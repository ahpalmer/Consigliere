#!/usr/bin/env bash
# Copies secrets into Key Vault, where the pod mounts them:
#   - the app's secrets file ("Section:Key" JSON, the same file `consigliere secrets set` writes), as one secret
#   - optionally the data pack's deploy key (a private SSH key whose public half is a read-only deploy key on GitHub)
# Values go straight from file to Key Vault and are never printed. Restart the pod afterwards to pick them up:
#   kubectl -n consigliere rollout restart deployment/consigliere
#
#   deploy/push-secrets.sh <secrets.json> [deploy key file]
set -euo pipefail

secrets=${1:?usage: deploy/push-secrets.sh <secrets.json> [deploy key file]}
deploy_key=${2:-}
rg=${CONSIGLIERE_RESOURCE_GROUP:-rg-consigliere}
vault=$(az deployment group show -g "$rg" -n consigliere --query properties.outputs.keyVaultName.value -o tsv | tr -d '\r')

# The pod won't start without these (keys only; values aren't read here).
missing=()
for key in Anthropic:ApiKey Telegram:BotToken Telegram:AllowedChatIds:0 Web:Token; do
  grep -q "\"$key\"" "$secrets" || missing+=("$key")
done
if ((${#missing[@]})); then
  echo "missing from $secrets: ${missing[*]} (add with: go run ./cmd/consigliere --secrets $secrets secrets set KEY VALUE)" >&2
  exit 1
fi

az keyvault secret set --vault-name "$vault" --name consigliere-secrets --file "$secrets" --encoding utf-8 -o none
echo "Saved $secrets to $vault as consigliere-secrets"
if [[ -n $deploy_key ]]; then
  az keyvault secret set --vault-name "$vault" --name datapack-deploy-key --file "$deploy_key" --encoding utf-8 -o none
  echo "Saved $deploy_key to $vault as datapack-deploy-key"
fi
