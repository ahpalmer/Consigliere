#!/bin/sh
# Keeps a clone of the private data pack at $DEST, fetched with a read-only deploy key. consigliere-sync.service runs
# it at boot and every 5 minutes. The key is read from Key Vault into the unit's RuntimeDirectory (in memory, removed
# when the run ends), so it never reaches the disk. Files the pack's .gitignore covers, like the database in db/, are
# never touched.
set -eu
here=$(dirname "$0")
key=$RUNTIME_DIRECTORY/deploy-key
umask 077

"$here/../../consigliere" keyvault get "$KEY_VAULT_NAME" datapack-deploy-key >"$key"
export GIT_SSH_COMMAND="ssh -i $key -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=$here/known_hosts"
[ -d "$DEST/.git" ] || git init -q -b main "$DEST"
git -C "$DEST" fetch -q --depth 1 "$DATA_PACK_REPO" main
git -C "$DEST" reset -q --hard FETCH_HEAD
echo "data pack at $(git -C "$DEST" rev-parse --short HEAD)"
