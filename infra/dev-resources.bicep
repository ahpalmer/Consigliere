// The dev box's resource group contents. Deployed by dev.bicep; deploy/dev.sh reads this deployment's outputs.

param location string
param vmName string
param ownerObjectId string
param vmSize string
param arch string
param shutdownTime string
param monthlyBudget int
param budgetEmail string
param budgetStartDate string
param botPrincipalId string

var suffix = uniqueString(resourceGroup().id)

// Built-in role definitions: public IDs, the same in every tenant. gitleaks mistakes the "Secrets" ones for keys.
var roles = {
  keyVaultSecretsUser: '4633458b-17de-408a-b874-0445c86b69e6' // gitleaks:allow
  keyVaultSecretsOfficer: 'b86a8fe4-44ce-4948-aee5-eccb2c155cd7' // gitleaks:allow
}

// ---------- Network ----------

// Its own network, so it can't reach the bots' VMs on Azure's side. Nothing comes in from the internet: the NSG has
// no allow rules (Azure's defaults deny inbound). Outbound is open: Claude, GitHub, package registries, the web.
resource nsg 'Microsoft.Network/networkSecurityGroups@2024-05-01' = {
  name: 'nsg-${vmName}'
  location: location
  properties: {
    securityRules: []
  }
}

resource vnet 'Microsoft.Network/virtualNetworks@2024-05-01' = {
  name: 'vnet-${vmName}'
  location: location
  properties: {
    addressSpace: {
      addressPrefixes: ['10.41.0.0/24']
    }
    subnets: [
      {
        name: 'vm'
        properties: {
          addressPrefix: '10.41.0.0/27'
          networkSecurityGroup: {
            id: nsg.id
          }
        }
      }
    ]
  }
}

// For outbound traffic only (new subnets get no default outbound access, and a NAT gateway costs ~8x more).
resource publicIp 'Microsoft.Network/publicIPAddresses@2024-05-01' = {
  name: 'pip-${vmName}'
  location: location
  sku: {
    name: 'Standard'
  }
  properties: {
    publicIPAllocationMethod: 'Static'
  }
}

resource nic 'Microsoft.Network/networkInterfaces@2024-05-01' = {
  name: 'nic-${vmName}'
  location: location
  properties: {
    ipConfigurations: [
      {
        name: 'ipconfig'
        properties: {
          subnet: {
            id: vnet.properties.subnets[0].id
          }
          privateIPAllocationMethod: 'Dynamic'
          publicIPAddress: {
            id: publicIp.id
          }
        }
      }
    ]
  }
}

// ---------- VM ----------

// deploy/dev/setup.sh (run through `az vm run-command` by deploy/dev.sh setup) installs everything on it. The owner
// signs in with Tailscale SSH as the `dev` user; Azure requires an SSH key anyway, so it gets one whose private half
// was discarded, and nothing listens on a public port.
resource vm 'Microsoft.Compute/virtualMachines@2024-07-01' = {
  name: vmName
  location: location
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${devIdentity.id}': {}
    }
  }
  properties: {
    hardwareProfile: {
      vmSize: vmSize
    }
    storageProfile: {
      imageReference: {
        publisher: 'Canonical'
        offer: 'ubuntu-24_04-lts'
        sku: arch == 'arm64' ? 'server-arm64' : 'server'
        version: 'latest'
      }
      osDisk: {
        createOption: 'FromImage'
        diskSizeGB: 64 // repos, worktrees, Go and npm caches
        managedDisk: {
          storageAccountType: 'StandardSSD_LRS'
        }
        deleteOption: 'Delete'
      }
    }
    osProfile: {
      computerName: vmName
      #disable-next-line adminusername-should-not-be-literal // no one signs in as it (see above)
      adminUsername: 'azureuser'
      linuxConfiguration: {
        disablePasswordAuthentication: true
        ssh: {
          publicKeys: [
            {
              path: '/home/azureuser/.ssh/authorized_keys'
              keyData: loadTextContent('no-login.pub')
            }
          ]
        }
      }
    }
    networkProfile: {
      networkInterfaces: [
        {
          id: nic.id
          properties: {
            deleteOption: 'Delete'
          }
        }
      ]
    }
    diagnosticsProfile: {
      bootDiagnostics: {
        enabled: true
      }
    }
  }
}

// A nightly stop so a forgotten VM doesn't bill all month. The disk, repos and worktrees stay; running sessions end.
resource shutdown 'Microsoft.DevTestLab/schedules@2018-09-15' = if (!empty(shutdownTime)) {
  name: 'shutdown-computevm-${vmName}'
  location: location
  properties: {
    status: 'Enabled'
    taskType: 'ComputeVmShutdownTask'
    dailyRecurrence: {
      time: shutdownTime
    }
    timeZoneId: 'Eastern Standard Time'
    targetResourceId: vm.id
    notificationSettings: {
      status: 'Disabled'
    }
  }
}

// ---------- Secrets ----------

// The one-time Tailscale auth key now; the GitHub token for pushes later (M17). The VM reads them into memory.
resource vault 'Microsoft.KeyVault/vaults@2023-07-01' = {
  name: 'kv-consigliere-dev-${take(suffix, 4)}'
  location: location
  properties: {
    tenantId: tenant().tenantId
    sku: {
      family: 'A'
      name: 'standard'
    }
    enableRbacAuthorization: true
    enableSoftDelete: true
    softDeleteRetentionInDays: 7
  }
}

// ---------- Identity and roles ----------

// The VM's identity. It can read this vault and nothing else in Azure: no bot's resources, no subscription rights.
resource devIdentity 'Microsoft.ManagedIdentity/userAssignedIdentities@2023-01-31' = {
  name: 'id-consigliere-dev'
  location: location
}

resource devSecrets 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(vault.id, devIdentity.id, roles.keyVaultSecretsUser)
  scope: vault
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', roles.keyVaultSecretsUser)
    principalId: devIdentity.properties.principalId
    principalType: 'ServicePrincipal'
  }
}

resource ownerSecrets 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(vault.id, ownerObjectId, roles.keyVaultSecretsOfficer)
  scope: vault
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', roles.keyVaultSecretsOfficer)
    principalId: ownerObjectId
    principalType: 'User'
  }
}

// The owner's bot may see whether this VM is running, start it (the dashboard's Code page), and put it to sleep
// (deallocate) once it's been idle for a few hours, and nothing more: not change it, and not run commands on it
// (run-command would be root on the box, and so the GitHub token).
resource vmStarter 'Microsoft.Authorization/roleDefinitions@2022-04-01' = {
  name: guid(resourceGroup().id, 'vm-starter')
  properties: {
    roleName: 'Consigliere dev box starter (${resourceGroup().name})'
    description: 'Read the dev box VM and its power state, start it, and deallocate it.'
    type: 'CustomRole'
    permissions: [
      {
        actions: [
          'Microsoft.Compute/virtualMachines/read'
          'Microsoft.Compute/virtualMachines/instanceView/read'
          'Microsoft.Compute/virtualMachines/start/action'
          'Microsoft.Compute/virtualMachines/deallocate/action'
        ]
        notActions: []
      }
    ]
    assignableScopes: [resourceGroup().id]
  }
}

resource botStartsVm 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(vm.id, botPrincipalId, 'vm-starter')
  scope: vm
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', vmStarter.name) // role assignments take the subscription-level ID
    principalId: botPrincipalId
    principalType: 'ServicePrincipal'
  }
}

// ---------- Budget ----------

resource budget 'Microsoft.Consumption/budgets@2021-10-01' = {
  name: 'consigliere-dev-monthly'
  properties: {
    category: 'Cost'
    amount: monthlyBudget
    timeGrain: 'Monthly'
    timePeriod: {
      startDate: budgetStartDate
    }
    notifications: {
      actual80: {
        enabled: true
        operator: 'GreaterThan'
        threshold: 80
        thresholdType: 'Actual'
        contactEmails: [budgetEmail]
      }
      forecast100: {
        enabled: true
        operator: 'GreaterThan'
        threshold: 100
        thresholdType: 'Forecasted'
        contactEmails: [budgetEmail]
      }
    }
  }
}

output vmName string = vm.name
output keyVaultName string = vault.name
output devClientId string = devIdentity.properties.clientId
