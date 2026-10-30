#!/usr/bin/env bash
# Deploys an image that's already in the registry: fills in deploy/k8s with the Azure names from the infra
# deployment's outputs, applies it, and waits for the new pod. The deploy workflow runs this; so can you.
#
#   DATA_PACK_REPO=git@github.com:<you>/<data-pack>.git deploy/deploy.sh <image tag>
#
# Needs: az signed in, kubectl pointed at the cluster (az aks get-credentials), bash.
set -euo pipefail
cd "$(dirname "$0")/.."

tag=${1:?usage: deploy/deploy.sh <image tag>}
: "${DATA_PACK_REPO:?set DATA_PACK_REPO to the SSH clone URL of the data pack}"
rg=${CONSIGLIERE_RESOURCE_GROUP:-rg-consigliere}

outputs=$(az deployment group show -g "$rg" -n consigliere --query \
  "[properties.outputs.acrLoginServer.value, properties.outputs.appClientId.value, properties.outputs.keyVaultName.value, properties.outputs.backupContainerUrl.value]" \
  -o tsv | tr '\r\n' '  ')
read -r registry client_id vault backups <<<"$outputs"
tenant_id=$(az account show --query tenantId -o tsv | tr -d '\r')

# Gitignored; only ever holds names and IDs, never secrets.
cat >deploy/k8s/azure.env <<EOF
IMAGE=$registry/consigliere:$tag
APP_CLIENT_ID=$client_id
TENANT_ID=$tenant_id
KEY_VAULT_NAME=$vault
DATA_PACK_REPO=$DATA_PACK_REPO
Backup__ContainerUrl=$backups
EOF

kubectl apply -k deploy/k8s
kubectl -n consigliere rollout status deployment/consigliere --timeout=5m
