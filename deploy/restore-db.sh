#!/usr/bin/env bash
# Replaces a bot's database with a backup: a blob already in its backups container, or a local file (uploaded there
# first, e.g. a laptop's database). The VM stops the agent, swaps the file (keeping the old one as
# consigliere.db.before-restore) and starts it again.
#
#   deploy/restore-db.sh <backup blob name | file.db>
#
# CONSIGLIERE_RESOURCE_GROUP picks the bot (default rg-consigliere). Needs: az signed in, bash.
set -euo pipefail

source=${1:?usage: deploy/restore-db.sh <backup blob name | file.db>}
[[ -f $source ]] && source=$(realpath "$source")
cd "$(dirname "$0")/.."
# shellcheck source=deploy/lib.sh
. deploy/lib.sh
bot_outputs

blob=$source
if [[ -f $source ]]; then
  blob=restore-$(date -u +%Y-%m-%dT%H%MZ).db # outside the backups/consigliere- prefix, so retention leaves it alone
  echo "Uploading $source as $blob"
  az storage blob upload --auth-mode login --account-name "$storage" -c backups -n "$blob" \
    -f "$(native "$source")" --only-show-errors -o none
fi

run_on_vm deploy/vm/restore.sh CONSIGLIERE_RESTORE_OK \
  "BACKUP_CONTAINER_URL=$backups" "AZURE_CLIENT_ID=$client_id" "RESTORE_BLOB=$blob"
