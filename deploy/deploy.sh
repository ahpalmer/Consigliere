#!/usr/bin/env bash
# Ships a release (deploy/package.sh) to a bot's VM: uploads it to the bot's releases container, then runs
# deploy/vm/install.sh on the VM, which installs it, applies the VM's setup and checks the app came up (rolling back
# if not). The deploy workflow runs this; so can you.
#
#   DATA_PACK_REPO=git@github.com:<you>/<data-pack>.git deploy/deploy.sh <release.tar.gz> [release name]
#
# CONSIGLIERE_RESOURCE_GROUP picks the bot (default rg-consigliere). RESTORE_BLOB=<backup blob> loads that backup if
# the VM has no database yet, for moving a bot to a new VM. Needs: az signed in, bash.
set -euo pipefail

file=$(realpath "${1:?usage: deploy/deploy.sh <release.tar.gz> [release name]}")
cd "$(dirname "$0")/.."
release=${2:-consigliere-$(git rev-parse --short=12 HEAD)}
: "${DATA_PACK_REPO:?set DATA_PACK_REPO to the SSH clone URL of the data pack}"
# shellcheck source=deploy/lib.sh
. deploy/lib.sh
bot_outputs

echo "Uploading $release to $storage"
az storage blob upload --auth-mode login --account-name "$storage" -c releases -n "$release.tar.gz" \
  -f "$(native "$file")" --overwrite --only-show-errors -o none

echo "Installing on $vm"
run_on_vm deploy/vm/install.sh CONSIGLIERE_INSTALL_OK \
  "RELEASE_URL=$releases/$release.tar.gz" "KEY_VAULT_NAME=$vault" "AZURE_CLIENT_ID=$client_id" \
  "BACKUP_CONTAINER_URL=$backups" "DATA_PACK_REPO=$DATA_PACK_REPO" "TS_HOSTNAME=$vm" "RESTORE_BLOB=${RESTORE_BLOB:-}"
