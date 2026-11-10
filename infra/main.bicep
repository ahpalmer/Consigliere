// Everything one bot runs on in Azure: a resource group holding a small VM, Key Vault, storage for backups and releases,
// the VM's and GitHub Actions' identities, their role assignments, and a budget. Each bot gets its own resource group
// from its own parameters file, so bots share nothing. See docs/deploy.md.
//
//   az deployment sub create -n consigliere        -l eastus2 -f infra/main.bicep -p infra/main.bicepparam
//   az deployment sub create -n consigliere-alison -l eastus2 -f infra/main.bicep -p infra/alison.bicepparam
targetScope = 'subscription'

@description('Region for everything.')
param location string = 'eastus2'

param resourceGroupName string

@description('The VM\'s name, which is also its name on the tailnet: the dashboard is https://<botName>.<tailnet>.ts.net.')
param botName string

@description('Object ID of the owner (az ad signed-in-user show --query id -o tsv): sets Key Vault secrets, moves backups.')
param ownerObjectId string

@description('The GitHub repository allowed to deploy, as the GitHub sign-in token names it: owner@ownerId/name@repoId (the IDs come from https://api.github.com/repos/<owner>/<name>).')
param githubRepository string

@description('The GitHub environment whose deploys may sign in as this bot\'s GitHub identity.')
param githubEnvironment string

@description('2 vCPU, 1 GiB, Arm64: the app uses ~120 MiB.')
param vmSize string = 'Standard_B2pts_v2'

@description('Monthly budget for the resource group, in the billing currency. Alerts at 80% of actual and 100% of forecast spend.')
param monthlyBudget int = 20

@description('Where budget alerts are emailed.')
param budgetEmail string

@description('First month of the budget (yyyy-MM-01). Keep it fixed: a budget\'s start date can\'t move once created.')
param budgetStartDate string

resource rg 'Microsoft.Resources/resourceGroups@2024-03-01' = {
  name: resourceGroupName
  location: location
}

module resources 'resources.bicep' = {
  name: 'consigliere'
  scope: rg
  params: {
    location: location
    botName: botName
    ownerObjectId: ownerObjectId
    githubRepository: githubRepository
    githubEnvironment: githubEnvironment
    vmSize: vmSize
    monthlyBudget: monthlyBudget
    budgetEmail: budgetEmail
    budgetStartDate: budgetStartDate
  }
}

output resourceGroup string = rg.name
output vmName string = resources.outputs.vmName
output keyVaultName string = resources.outputs.keyVaultName
output storageAccountName string = resources.outputs.storageAccountName
// The GitHub environment secret AZURE_CLIENT_ID for the deploy workflow.
output githubClientId string = resources.outputs.githubClientId
output tenantId string = tenant().tenantId
output subscriptionId string = subscription().subscriptionId
