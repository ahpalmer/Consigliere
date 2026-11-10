using 'main.bicep'

// A second bot with nothing shared: its own resource group, VM, Key Vault, storage and identities. Deployed from the
// alison/main branch by the "alison" GitHub environment.
param resourceGroupName = 'rg-consigliere-alison'
param botName = 'consigliere-alison'
param githubEnvironment = 'alison'
param budgetStartDate = '2026-11-01'

// The owner pays for and administers this one too.
param ownerObjectId = readEnvironmentVariable('CONSIGLIERE_OWNER_OBJECT_ID')
param budgetEmail = readEnvironmentVariable('CONSIGLIERE_BUDGET_EMAIL')
param githubRepository = 'ahpalmer@11614744/Consigliere@1434976398'
