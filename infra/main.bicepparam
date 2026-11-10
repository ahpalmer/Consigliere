using 'main.bicep'

// The owner's bot, deployed from main by the "production" GitHub environment.
param resourceGroupName = 'rg-consigliere'
param botName = 'consigliere'
param githubEnvironment = 'production'
param budgetStartDate = '2026-10-01'

// Personal values come from the environment, so they never land in this public repo (see docs/deploy.md).
param ownerObjectId = readEnvironmentVariable('CONSIGLIERE_OWNER_OBJECT_ID')
param budgetEmail = readEnvironmentVariable('CONSIGLIERE_BUDGET_EMAIL')
param githubRepository = 'ahpalmer@11614744/Consigliere@1434976398'
