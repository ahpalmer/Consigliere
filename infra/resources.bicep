// The resource group's contents. Deployed by main.bicep; the deploy script reads this deployment's outputs.

param location string
param ownerObjectId string
param githubRepository string
param nodeVmSize string
param monthlyBudget int
param budgetEmail string
param budgetStartDate string

// The app's Kubernetes identity: namespace and service account in deploy/k8s.
var appServiceAccount = 'system:serviceaccount:consigliere:consigliere'
var suffix = uniqueString(resourceGroup().id)

// Built-in role definitions.
var roles = {
  reader: 'acdd72a7-3385-48ef-bd42-f606fba81ae7'
  acrPull: '7f951dda-4ed3-4680-a7ca-43fe172d538d'
  acrPush: '8311e382-0749-4cb8-b61a-304f252e45ec'
  aksClusterUser: '4abbcc35-e782-43d8-92c5-2d3f1bd2253f'
  aksRbacClusterAdmin: 'b1ff04bb-8a4e-4dc4-8eb5-8693973ce19b'
  keyVaultSecretsUser: '4633458b-17de-408a-b874-0445c86b69e6'
  keyVaultSecretsOfficer: 'b86a8fe4-44ce-4948-aee5-eccb2c155cd7'
  blobDataContributor: 'ba92f5b4-2d11-453d-a403-e96b0029c9fe'
  blobDataReader: '2a2b9908-6ea1-4ae2-8e65-a410df84e7d1'
}

// ---------- Cluster ----------

// Free tier, one node, no ingress: the app only makes outbound calls (Telegram long-polling, APIs), and the
// dashboard is reached with kubectl port-forward. Sign-in is Entra ID only (no local admin kubeconfig).
resource aks 'Microsoft.ContainerService/managedClusters@2024-09-01' = {
  name: 'aks-consigliere'
  location: location
  sku: {
    name: 'Base'
    tier: 'Free'
  }
  identity: {
    type: 'SystemAssigned'
  }
  properties: {
    dnsPrefix: 'consigliere'
    nodeResourceGroup: '${resourceGroup().name}-nodes'
    disableLocalAccounts: true
    aadProfile: {
      managed: true
      enableAzureRBAC: true
    }
    oidcIssuerProfile: {
      enabled: true
    }
    securityProfile: {
      workloadIdentity: {
        enabled: true
      }
    }
    addonProfiles: {
      // Mounts Key Vault secrets into pods as files.
      azureKeyvaultSecretsProvider: {
        enabled: true
        config: {
          enableSecretRotation: 'true'
          rotationPollInterval: '2m'
        }
      }
    }
    autoUpgradeProfile: {
      upgradeChannel: 'patch'
      nodeOSUpgradeChannel: 'NodeImage'
    }
    networkProfile: {
      networkPlugin: 'azure'
      networkPluginMode: 'overlay'
      loadBalancerSku: 'standard'
      outboundType: 'loadBalancer'
    }
    agentPoolProfiles: [
      {
        name: 'system'
        mode: 'System'
        count: 1
        vmSize: nodeVmSize
        osType: 'Linux'
        osSKU: 'AzureLinux'
        osDiskType: 'Managed'
        osDiskSizeGB: 32
        type: 'VirtualMachineScaleSets'
      }
    ]
  }
}

// Upgrades and node image updates restart the only node, so keep them to early Sunday morning (US Eastern).
resource upgradeWindow 'Microsoft.ContainerService/managedClusters/maintenanceConfigurations@2024-09-01' = {
  parent: aks
  name: 'aksManagedAutoUpgradeSchedule'
  properties: {
    maintenanceWindow: {
      schedule: {
        weekly: {
          dayOfWeek: 'Sunday'
          intervalWeeks: 1
        }
      }
      durationHours: 4
      startTime: '07:00'
      utcOffset: '+00:00'
    }
  }
}

resource nodeOsWindow 'Microsoft.ContainerService/managedClusters/maintenanceConfigurations@2024-09-01' = {
  parent: aks
  name: 'aksManagedNodeOSUpgradeSchedule'
  dependsOn: [upgradeWindow] // the cluster takes one change at a time
  properties: {
    maintenanceWindow: {
      schedule: {
        weekly: {
          dayOfWeek: 'Sunday'
          intervalWeeks: 1
        }
      }
      durationHours: 4
      startTime: '07:00'
      utcOffset: '+00:00'
    }
  }
}

// ---------- Images ----------

resource acr 'Microsoft.ContainerRegistry/registries@2023-07-01' = {
  name: 'acrconsigliere${suffix}'
  location: location
  sku: {
    name: 'Basic'
  }
  properties: {
    adminUserEnabled: false
    anonymousPullEnabled: false
  }
}

// ---------- Secrets ----------

// Holds the app's secrets file and the data pack deploy key (docs/deploy.md). Access is by RBAC role only.
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

// ---------- Backups ----------

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

resource backupRetention 'Microsoft.Storage/storageAccounts/managementPolicies@2023-05-01' = {
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
      ]
    }
  }
}

// ---------- Identities ----------

// The app's pod signs in as this identity (workload identity): it reads Key Vault and writes backups.
resource appIdentity 'Microsoft.ManagedIdentity/userAssignedIdentities@2023-01-31' = {
  name: 'id-consigliere-app'
  location: location
}

resource appFederation 'Microsoft.ManagedIdentity/userAssignedIdentities/federatedIdentityCredentials@2023-01-31' = {
  parent: appIdentity
  name: 'aks-service-account'
  properties: {
    issuer: aks.properties.oidcIssuerProfile.issuerURL
    subject: appServiceAccount
    audiences: ['api://AzureADTokenExchange']
  }
}

// GitHub Actions signs in as this identity over OIDC, only from the repository's "production" environment.
resource githubIdentity 'Microsoft.ManagedIdentity/userAssignedIdentities@2023-01-31' = {
  name: 'id-consigliere-github'
  location: location
}

resource githubFederation 'Microsoft.ManagedIdentity/userAssignedIdentities/federatedIdentityCredentials@2023-01-31' = {
  parent: githubIdentity
  name: 'github-production'
  properties: {
    issuer: 'https://token.actions.githubusercontent.com'
    subject: 'repo:${githubRepository}:environment:production'
    audiences: ['api://AzureADTokenExchange']
  }
}

// ---------- Role assignments ----------

// Nodes pull images.
resource kubeletAcrPull 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(acr.id, aks.id, roles.acrPull)
  scope: acr
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', roles.acrPull)
    principalId: aks.properties.identityProfile.kubeletidentity.objectId
    principalType: 'ServicePrincipal'
  }
}

// The app reads its secrets and writes backups (to the backups container only).
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

// GitHub Actions reads this deployment's outputs, pushes images, and applies manifests. Cluster admin isn't much
// beyond what pushing an image already allows here (the image runs with the app's secrets), and it's limited to
// the protected "production" environment.
resource githubReader 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(resourceGroup().id, githubIdentity.id, roles.reader)
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', roles.reader)
    principalId: githubIdentity.properties.principalId
    principalType: 'ServicePrincipal'
  }
}

resource githubAcrPush 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(acr.id, githubIdentity.id, roles.acrPush)
  scope: acr
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', roles.acrPush)
    principalId: githubIdentity.properties.principalId
    principalType: 'ServicePrincipal'
  }
}

resource githubClusterUser 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(aks.id, githubIdentity.id, roles.aksClusterUser)
  scope: aks
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', roles.aksClusterUser)
    principalId: githubIdentity.properties.principalId
    principalType: 'ServicePrincipal'
  }
}

resource githubClusterAdmin 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(aks.id, githubIdentity.id, roles.aksRbacClusterAdmin)
  scope: aks
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', roles.aksRbacClusterAdmin)
    principalId: githubIdentity.properties.principalId
    principalType: 'ServicePrincipal'
  }
}

// The owner administers the cluster, sets secrets and can download backups. (Subscription Owner covers the
// management plane, but not these data-plane permissions.)
resource ownerClusterAdmin 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(aks.id, ownerObjectId, roles.aksRbacClusterAdmin)
  scope: aks
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', roles.aksRbacClusterAdmin)
    principalId: ownerObjectId
    principalType: 'User'
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

resource ownerBackups 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(backupContainer.id, ownerObjectId, roles.blobDataReader)
  scope: backupContainer
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', roles.blobDataReader)
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

output aksName string = aks.name
output acrName string = acr.name
output acrLoginServer string = acr.properties.loginServer
output keyVaultName string = vault.name
output storageAccountName string = storage.name
output backupContainerUrl string = '${storage.properties.primaryEndpoints.blob}${backupContainer.name}'
output appClientId string = appIdentity.properties.clientId
output githubClientId string = githubIdentity.properties.clientId
