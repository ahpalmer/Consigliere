using 'dev.bicep'

// The owner's dev box (M16). Sizes and names are dev.bicep's defaults.
param budgetStartDate = '2026-11-01'

// Personal values come from the environment, so they never land in this public repo (see docs/deploy.md).
param ownerObjectId = readEnvironmentVariable('CONSIGLIERE_OWNER_OBJECT_ID')
param budgetEmail = readEnvironmentVariable('CONSIGLIERE_BUDGET_EMAIL')
