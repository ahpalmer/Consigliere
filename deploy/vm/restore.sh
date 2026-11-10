#!/bin/sh
# Replaces the bot's database with a backup blob, keeping the old file as consigliere.db.before-restore. Runs as root
# on the VM: sent by deploy/restore-db.sh through `az vm run-command`, or called by install.sh on a new VM. Needs
# BACKUP_CONTAINER_URL, AZURE_CLIENT_ID and RESTORE_BLOB.
set -eu
: "${BACKUP_CONTAINER_URL:?}" "${AZURE_CLIENT_ID:?}" "${RESTORE_BLOB:?}"
db=/var/lib/consigliere/pack/db

token=$(curl -fsS -H Metadata:true \
	"http://169.254.169.254/metadata/identity/oauth2/token?api-version=2018-02-01&resource=https%3A%2F%2Fstorage.azure.com&client_id=$AZURE_CLIENT_ID" |
	python3 -c 'import json, sys; print(json.load(sys.stdin)["access_token"])')

was_active=$(systemctl is-active consigliere.service || true)
systemctl stop consigliere.service 2>/dev/null || true
install -d -o consigliere -g consigliere -m 700 /var/lib/consigliere /var/lib/consigliere/pack "$db"
curl -fsS -H "Authorization: Bearer $token" -H "x-ms-version: 2023-11-03" -o "$db/consigliere.db.new" "$BACKUP_CONTAINER_URL/$RESTORE_BLOB"
if [ -f "$db/consigliere.db" ]; then
	mv "$db/consigliere.db" "$db/consigliere.db.before-restore"
fi
rm -f "$db/consigliere.db-wal" "$db/consigliere.db-shm"
mv "$db/consigliere.db.new" "$db/consigliere.db"
chown consigliere:consigliere "$db"/consigliere.db* && chmod 600 "$db"/consigliere.db*
echo "==> restored $RESTORE_BLOB ($(stat -c %s "$db/consigliere.db") bytes)"
if [ "$was_active" = active ]; then
	systemctl start consigliere.service
fi
echo CONSIGLIERE_RESTORE_OK
