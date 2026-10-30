using 'main.bicep'

// Personal values come from the environment, so they never land in this public repo (see docs/deploy.md).
param ownerObjectId = readEnvironmentVariable('CONSIGLIERE_OWNER_OBJECT_ID')
param budgetEmail = readEnvironmentVariable('CONSIGLIERE_BUDGET_EMAIL')
param githubRepository = 'ahpalmer/Consigliere'
