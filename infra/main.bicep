// Everything Consigliere runs on in Azure: one resource group holding AKS, the container registry, Key Vault, backup
// storage, the app's and GitHub Actions' identities, their role assignments, and a budget. See docs/deploy.md.
//
//   az deployment sub create -n consigliere -l eastus2 -f infra/main.bicep -p infra/main.bicepparam
targetScope = 'subscription'

@description('Region for everything.')
param location string = 'eastus2'

param resourceGroupName string = 'rg-consigliere'

@description('Object ID of the owner (az ad signed-in-user show --query id -o tsv): cluster admin, sets Key Vault secrets, reads backups.')
param ownerObjectId string

@description('The GitHub repository (owner/name) whose "production" environment may deploy.')
param githubRepository string

@description('One small node runs the app and the system pods. Needs at least 2 vCPUs and 4 GiB.')
param nodeVmSize string = 'Standard_B2als_v2'

@description('Monthly budget for the resource group, in the billing currency. Alerts at 80% of actual and 100% of forecast spend.')
param monthlyBudget int = 75

@description('Where budget alerts are emailed.')
param budgetEmail string

@description('First month of the budget (yyyy-MM-01). Keep it fixed: a budget\'s start date can\'t move once created.')
param budgetStartDate string = '2026-10-01'

resource rg 'Microsoft.Resources/resourceGroups@2024-03-01' = {
  name: resourceGroupName
  location: location
}

module resources 'resources.bicep' = {
  name: 'consigliere'
  scope: rg
  params: {
    location: location
    ownerObjectId: ownerObjectId
    githubRepository: githubRepository
    nodeVmSize: nodeVmSize
    monthlyBudget: monthlyBudget
    budgetEmail: budgetEmail
    budgetStartDate: budgetStartDate
  }
}

output resourceGroup string = rg.name
output aksName string = resources.outputs.aksName
output keyVaultName string = resources.outputs.keyVaultName
output storageAccountName string = resources.outputs.storageAccountName
// The production environment secret AZURE_CLIENT_ID for the deploy workflow.
output githubClientId string = resources.outputs.githubClientId
output tenantId string = tenant().tenantId
output subscriptionId string = subscription().subscriptionId
