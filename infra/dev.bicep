// The dev box: a VM where Claude Code works on the owner's repositories, driven from the Claude app (docs/dev-box.md).
// Its own resource group, network, Key Vault and identity, apart from every bot: nothing on it can reach a bot's
// secrets, and the bots' VMs stay small and always on while this one can sleep.
//
//   az deployment sub create -n consigliere-dev -l eastus2 -f infra/dev.bicep -p infra/dev.bicepparam
targetScope = 'subscription'

@description('Region for everything.')
param location string = 'eastus2'

param resourceGroupName string = 'rg-consigliere-dev'

@description('The VM\'s name, which is also its name on the tailnet: ssh dev@<name>.')
param vmName string = 'consigliere-dev'

@description('Object ID of the owner (az ad signed-in-user show --query id -o tsv): sets Key Vault secrets.')
param ownerObjectId string

@description('2 vCPU, 8 GiB, Arm64: Claude Code plus Go and Node builds. Use an x64 size (and arch) if a repo\'s tools need it.')
param vmSize string = 'Standard_B2ps_v2'

@allowed(['arm64', 'x64'])
@description('Must match vmSize: Standard_B*p* sizes are Arm64.')
param arch string = 'arm64'

@description('Shuts the VM down (deallocated, so compute stops billing) every day at this time, Eastern; empty for never. Start it again from the Azure app or deploy/dev.sh start.')
param shutdownTime string = '0300'

@description('Monthly budget for the resource group, in the billing currency. Alerts at 80% of actual and 100% of forecast spend.')
param monthlyBudget int = 60

@description('Where budget alerts are emailed.')
param budgetEmail string

@description('First month of the budget (yyyy-MM-01). Keep it fixed: a budget\'s start date can\'t move once created.')
param budgetStartDate string

resource rg 'Microsoft.Resources/resourceGroups@2024-03-01' = {
  name: resourceGroupName
  location: location
}

module resources 'dev-resources.bicep' = {
  name: 'consigliere-dev'
  scope: rg
  params: {
    location: location
    vmName: vmName
    ownerObjectId: ownerObjectId
    vmSize: vmSize
    arch: arch
    shutdownTime: shutdownTime
    monthlyBudget: monthlyBudget
    budgetEmail: budgetEmail
    budgetStartDate: budgetStartDate
  }
}

output resourceGroup string = rg.name
output vmName string = resources.outputs.vmName
output keyVaultName string = resources.outputs.keyVaultName
