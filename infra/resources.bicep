// The resource group's contents for one bot. Deployed by main.bicep; deploy/deploy.sh reads this deployment's outputs.

param location string
param botName string
param ownerObjectId string
param githubRepository string
param githubEnvironment string
param vmSize string
param monthlyBudget int
param budgetEmail string
param budgetStartDate string

var suffix = uniqueString(resourceGroup().id)

// Built-in role definitions: public IDs, the same in every tenant. gitleaks mistakes the "Secrets" ones for keys.
var roles = {
  reader: 'acdd72a7-3385-48ef-bd42-f606fba81ae7'
  vmContributor: '9980e02c-c2be-4d73-94e8-173b1dc7cf3c'
  keyVaultSecretsUser: '4633458b-17de-408a-b874-0445c86b69e6' // gitleaks:allow
  keyVaultSecretsOfficer: 'b86a8fe4-44ce-4948-aee5-eccb2c155cd7' // gitleaks:allow
  blobDataContributor: 'ba92f5b4-2d11-453d-a403-e96b0029c9fe'
  blobDataReader: '2a2b9908-6ea1-4ae2-8e65-a410df84e7d1'
}

// ---------- Network ----------

// Each bot has its own network, so two bots' VMs can't reach each other. Nothing comes in from the internet: the
// NSG has no allow rules (Azure's defaults deny inbound), and the app, Tailscale and the deploys all connect out.
resource nsg 'Microsoft.Network/networkSecurityGroups@2024-05-01' = {
  name: 'nsg-${botName}'
  location: location
  properties: {
    securityRules: []
  }
}

resource vnet 'Microsoft.Network/virtualNetworks@2024-05-01' = {
  name: 'vnet-${botName}'
  location: location
  properties: {
    addressSpace: {
      addressPrefixes: ['10.40.0.0/24']
    }
    subnets: [
      {
        name: 'vm'
        properties: {
          addressPrefix: '10.40.0.0/27'
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
  name: 'pip-${botName}'
  location: location
  sku: {
    name: 'Standard'
  }
  properties: {
    publicIPAllocationMethod: 'Static'
  }
}

resource nic 'Microsoft.Network/networkInterfaces@2024-05-01' = {
  name: 'nic-${botName}'
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

// One small Arm64 VM. deploy/vm/install.sh (run through `az vm run-command`, from the deploy workflow) sets up
// everything on it: the app's systemd service, the data pack sync and Tailscale, which serves the dashboard on the
// owner's tailnet. Nobody signs in over SSH: Azure requires a key, so it gets one whose private half was discarded.
resource vm 'Microsoft.Compute/virtualMachines@2024-07-01' = {
  name: botName
  location: location
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${appIdentity.id}': {}
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
        sku: 'server-arm64'
        version: 'latest'
      }
      osDisk: {
        createOption: 'FromImage'
        diskSizeGB: 32
        managedDisk: {
          storageAccountType: 'StandardSSD_LRS'
        }
        deleteOption: 'Delete'
      }
    }
    osProfile: {
      computerName: botName
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
        enabled: true // managed storage; the serial log helps if the VM ever doesn't come up
      }
    }
  }
}

// ---------- Secrets ----------

// Holds the app's secrets file, the data pack deploy key and the one-time Tailscale auth key (docs/deploy.md). Access
// is by RBAC role only, and the VM reads them into memory: nothing secret is written to its disk.
resource vault 'Microsoft.KeyVault/vaults@2023-07-01' = {
  name: 'kv-consigliere-${take(suffix, 6)}'
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

// ---------- Storage: backups and releases ----------

resource storage 'Microsoft.Storage/storageAccounts@2023-05-01' = {
  name: 'stconsigliere${take(suffix, 8)}'
  location: location
  kind: 'StorageV2'
  sku: {
    name: 'Standard_LRS'
  }
  properties: {
    accessTier: 'Hot'
    allowBlobPublicAccess: false
    allowSharedKeyAccess: false // Entra ID only; nothing can use an account key
    minimumTlsVersion: 'TLS1_2'
    supportsHttpsTrafficOnly: true
  }
}

resource blobService 'Microsoft.Storage/storageAccounts/blobServices@2023-05-01' = {
  parent: storage
  name: 'default'
  properties: {
    deleteRetentionPolicy: {
      enabled: true
      days: 7
    }
  }
}

resource backupContainer 'Microsoft.Storage/storageAccounts/blobServices/containers@2023-05-01' = {
  parent: blobService
  name: 'backups'
}

// Each deploy uploads the build here, and the VM downloads it with its own identity.
resource releaseContainer 'Microsoft.Storage/storageAccounts/blobServices/containers@2023-05-01' = {
  parent: blobService
  name: 'releases'
}

resource retention 'Microsoft.Storage/storageAccounts/managementPolicies@2023-05-01' = {
  parent: storage
  name: 'default'
  properties: {
    policy: {
      rules: [
        {
          name: 'expire-backups'
          enabled: true
          type: 'Lifecycle'
          definition: {
            filters: {
              blobTypes: ['blockBlob']
              prefixMatch: ['backups/consigliere-']
            }
            actions: {
              baseBlob: {
                delete: {
                  daysAfterModificationGreaterThan: 30
                }
              }
            }
          }
        }
        {
          name: 'expire-releases'
          enabled: true
          type: 'Lifecycle'
          definition: {
            filters: {
              blobTypes: ['blockBlob']
              prefixMatch: ['releases/']
            }
            actions: {
              baseBlob: {
                delete: {
                  daysAfterModificationGreaterThan: 30
                }
              }
            }
          }
        }
      ]
    }
  }
}

// ---------- Identities ----------

// The VM's identity: the app and its scripts read Key Vault, write backups and download releases as this.
resource appIdentity 'Microsoft.ManagedIdentity/userAssignedIdentities@2023-01-31' = {
  name: 'id-consigliere-app'
  location: location
}

// GitHub Actions signs in as this identity over OIDC, only from this bot's GitHub environment.
resource githubIdentity 'Microsoft.ManagedIdentity/userAssignedIdentities@2023-01-31' = {
  name: 'id-consigliere-github'
  location: location
}

resource githubFederation 'Microsoft.ManagedIdentity/userAssignedIdentities/federatedIdentityCredentials@2023-01-31' = {
  parent: githubIdentity
  name: 'github-${githubEnvironment}'
  properties: {
    issuer: 'https://token.actions.githubusercontent.com'
    subject: 'repo:${githubRepository}:environment:${githubEnvironment}'
    audiences: ['api://AzureADTokenExchange']
  }
}

// ---------- Role assignments ----------

// The VM reads its secrets, writes (and, for a restore, reads) backups, and downloads releases.
resource appSecrets 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(vault.id, appIdentity.id, roles.keyVaultSecretsUser)
  scope: vault
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', roles.keyVaultSecretsUser)
    principalId: appIdentity.properties.principalId
    principalType: 'ServicePrincipal'
  }
}

resource appBackups 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(backupContainer.id, appIdentity.id, roles.blobDataContributor)
  scope: backupContainer
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', roles.blobDataContributor)
    principalId: appIdentity.properties.principalId
    principalType: 'ServicePrincipal'
  }
}

resource appReleases 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(releaseContainer.id, appIdentity.id, roles.blobDataReader)
  scope: releaseContainer
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', roles.blobDataReader)
    principalId: appIdentity.properties.principalId
    principalType: 'ServicePrincipal'
  }
}

// GitHub Actions reads this deployment's outputs, uploads a release and runs the install script on the VM. Running
// scripts there is about what shipping code to it already allows, and only this bot's GitHub environment can do it.
resource githubReader 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(resourceGroup().id, githubIdentity.id, roles.reader)
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', roles.reader)
    principalId: githubIdentity.properties.principalId
    principalType: 'ServicePrincipal'
  }
}

resource githubReleases 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(releaseContainer.id, githubIdentity.id, roles.blobDataContributor)
  scope: releaseContainer
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', roles.blobDataContributor)
    principalId: githubIdentity.properties.principalId
    principalType: 'ServicePrincipal'
  }
}

resource githubVm 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(vm.id, githubIdentity.id, roles.vmContributor)
  scope: vm
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', roles.vmContributor)
    principalId: githubIdentity.properties.principalId
    principalType: 'ServicePrincipal'
  }
}

// The owner sets secrets and moves databases in and out of backups. (Subscription Owner covers the management
// plane, but not these data-plane permissions.)
resource ownerSecrets 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(vault.id, ownerObjectId, roles.keyVaultSecretsOfficer)
  scope: vault
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', roles.keyVaultSecretsOfficer)
    principalId: ownerObjectId
    principalType: 'User'
  }
}

resource ownerBackups 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(backupContainer.id, ownerObjectId, roles.blobDataContributor)
  scope: backupContainer
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', roles.blobDataContributor)
    principalId: ownerObjectId
    principalType: 'User'
  }
}

resource ownerReleases 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(releaseContainer.id, ownerObjectId, roles.blobDataContributor)
  scope: releaseContainer
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', roles.blobDataContributor)
    principalId: ownerObjectId
    principalType: 'User'
  }
}

// ---------- Budget ----------

resource budget 'Microsoft.Consumption/budgets@2021-10-01' = {
  name: 'consigliere-monthly'
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
output storageAccountName string = storage.name
output backupContainerUrl string = '${storage.properties.primaryEndpoints.blob}${backupContainer.name}'
output releaseContainerUrl string = '${storage.properties.primaryEndpoints.blob}${releaseContainer.name}'
output appClientId string = appIdentity.properties.clientId
output githubClientId string = githubIdentity.properties.clientId
