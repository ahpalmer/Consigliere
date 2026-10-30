#!/bin/sh
# Keeps a clone of the private data pack at $DEST, fetched with a read-only deploy key.
# Without arguments it syncs once (the init container); with --loop it re-syncs every $INTERVAL seconds (the sidecar).
# Files the pack's .gitignore covers, like the database in db/, are never touched.
set -eu
umask 0002 # group-writable: the app (same group, through fsGroup) creates db/ inside the clone

key=/tmp/deploy-key
export GIT_SSH_COMMAND="ssh -i $key -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/etc/datapack-sync/known_hosts"

# Steps are chained with && because set -e doesn't apply inside a function called from `||`.
sync_once() {
	# Copied each time: ssh wants a private key only its owner can read, and Key Vault rotation updates the mount.
	cp /mnt/datapack-key/deploy-key "$key" && chmod 600 "$key" &&
		{ [ -d "$DEST/.git" ] || git init -q -b "$BRANCH" "$DEST"; } &&
		git -C "$DEST" fetch -q --depth 1 "$REPO" "$BRANCH" &&
		git -C "$DEST" reset -q --hard FETCH_HEAD &&
		echo "data pack at $(git -C "$DEST" rev-parse --short HEAD)"
}

if [ "${1:-}" != "--loop" ]; then
	# If GitHub is unreachable, a copy from an earlier start still works.
	sync_once || {
		git -C "$DEST" rev-parse -q --verify HEAD >/dev/null 2>&1 || exit 1
		echo "sync failed; starting with the existing copy" >&2
	}
	exit 0
fi

while :; do
	sleep "$INTERVAL"
	sync_once || echo "sync failed; retrying in ${INTERVAL}s" >&2
done
