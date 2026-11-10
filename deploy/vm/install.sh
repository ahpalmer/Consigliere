#!/bin/sh
# Installs a release on a bot's VM and (re)applies the VM's whole setup. deploy/deploy.sh sends this through
# `az vm run-command`, which runs it as root with these variables exported ahead of it:
#
#   RELEASE_URL           the build to install: a tarball in the bot's releases container (deploy/package.sh)
#   KEY_VAULT_NAME        the bot's vault: consigliere-secrets, datapack-deploy-key, tailscale-auth-key
#   AZURE_CLIENT_ID       the VM's managed identity
#   BACKUP_CONTAINER_URL  where nightly backups go
#   DATA_PACK_REPO        SSH clone URL of the bot's data pack
#   TS_HOSTNAME           the VM's name on the tailnet
#   RESTORE_BLOB          optional: a backup to load if the VM has no database yet (moving a bot to a new VM)
#
# Every step checks or overwrites, so it's safe to rerun and a new VM ends up the same as an old one. Output is kept
# short: run-command returns only the last 4 KiB.
set -eu
: "${RELEASE_URL:?}" "${KEY_VAULT_NAME:?}" "${AZURE_CLIENT_ID:?}" "${BACKUP_CONTAINER_URL:?}" "${DATA_PACK_REPO:?}" "${TS_HOSTNAME:?}"

TAILSCALE_VERSION=1.104.2 # pinned like every dependency; bump to a release at least two weeks old
app=/opt/consigliere
state=/var/lib/consigliere
export DEBIAN_FRONTEND=noninteractive
log() { echo "==> $*"; }

# ---------- The machine ----------

if [ "$(dpkg-query -W -f '${Version}' tailscale 2>/dev/null || true)" != "$TAILSCALE_VERSION" ]; then
	log "installing Tailscale $TAILSCALE_VERSION"
	# shellcheck disable=SC1091 # the VM's, not in this repo
	. /etc/os-release
	curl -fsSL "https://pkgs.tailscale.com/stable/ubuntu/$VERSION_CODENAME.noarmor.gpg" -o /usr/share/keyrings/tailscale-archive-keyring.gpg
	curl -fsSL "https://pkgs.tailscale.com/stable/ubuntu/$VERSION_CODENAME.tailscale-keyring.list" -o /etc/apt/sources.list.d/tailscale.list
	apt-get -qq -o DPkg::Lock::Timeout=600 update >/dev/null
	apt-get -qq -y -o DPkg::Lock::Timeout=600 install --allow-downgrades --allow-change-held-packages git "tailscale=$TAILSCALE_VERSION" >/dev/null
	apt-mark hold tailscale >/dev/null
fi

# Ubuntu installs security updates daily; this lets them reboot when one needs it, early morning US Eastern.
cat >/etc/apt/apt.conf.d/52consigliere-reboot <<'EOF'
Unattended-Upgrade::Automatic-Reboot "true";
Unattended-Upgrade::Automatic-Reboot-Time "08:00";
EOF

# 1 GiB of RAM is plenty for the app (~120 MiB), but a little swap keeps a spike from ending in the OOM killer.
if [ ! -f /swapfile ]; then
	fallocate -l 1G /swapfile && chmod 600 /swapfile && mkswap /swapfile >/dev/null && swapon /swapfile
	echo '/swapfile none swap sw 0 0' >>/etc/fstab
fi

id consigliere >/dev/null 2>&1 || useradd --system --home-dir "$state" --shell /usr/sbin/nologin consigliere

# ---------- The release ----------

# The VM downloads with its own identity; GitHub Actions only uploads.
storage_token() {
	curl -fsS -H Metadata:true \
		"http://169.254.169.254/metadata/identity/oauth2/token?api-version=2018-02-01&resource=https%3A%2F%2Fstorage.azure.com&client_id=$AZURE_CLIENT_ID" |
		python3 -c 'import json, sys; print(json.load(sys.stdin)["access_token"])'
}
release=$(basename "$RELEASE_URL" .tar.gz)
dir=$app/releases/$release
log "installing $release"
mkdir -p "$app/releases"
rm -rf "$dir.new" && mkdir "$dir.new"
curl -fsS -H "Authorization: Bearer $(storage_token)" -H "x-ms-version: 2023-11-03" "$RELEASE_URL" | tar -xz -C "$dir.new"
chown -R root:root "$dir.new" && chmod -R go-w "$dir.new"
chmod 755 "$dir.new/consigliere" "$dir.new/deploy/vm/datapack-sync.sh"
rm -rf "$dir" && mv "$dir.new" "$dir"

# Names and URLs only; the secrets stay in Key Vault and are read into memory.
mkdir -p /etc/consigliere
cat >/etc/consigliere/env <<EOF
KEY_VAULT_NAME=$KEY_VAULT_NAME
AZURE_CLIENT_ID=$AZURE_CLIENT_ID
DATA_PACK_REPO=$DATA_PACK_REPO
DataPack__Path=$state/pack
Web__Listen=127.0.0.1:8080
Backup__ContainerUrl=$BACKUP_CONTAINER_URL
EOF

previous=$(readlink "$app/current" || true)
ln -sfn "$dir" "$app/current.new" && mv -T "$app/current.new" "$app/current"
cp "$dir"/deploy/vm/*.service "$dir"/deploy/vm/*.timer /etc/systemd/system/
systemctl daemon-reload
systemctl enable --quiet consigliere.service consigliere-sync.timer
systemctl start consigliere-sync.timer

# ---------- Tailscale: the dashboard on the owner's tailnet ----------

systemctl enable --quiet --now tailscaled
ts_state() { tailscale status --json 2>/dev/null | python3 -c 'import json, sys; print(json.load(sys.stdin).get("BackendState", ""))' || true; }
if [ "$(ts_state)" != Running ]; then
	log "joining the tailnet as $TS_HOSTNAME"
	# A one-time key, kept in memory (/run) only as long as `tailscale up` needs it.
	key=$(mktemp -p /run)
	"$dir/consigliere" keyvault get "$KEY_VAULT_NAME" tailscale-auth-key >"$key"
	tailscale up --auth-key="file:$key" --hostname="$TS_HOSTNAME" || { rm -f "$key"; exit 1; }
	rm -f "$key"
fi
# https://<TS_HOSTNAME>.<tailnet>.ts.net → the dashboard on loopback, with a certificate from Tailscale. Not Funnel:
# only the tailnet can reach it, and the dashboard's own sign-in still applies. Without HTTPS certificates turned on
# in the tailnet, `tailscale serve` waits forever for someone to enable them, so it gets a time limit.
if ! timeout 60 tailscale serve --bg --https=443 http://127.0.0.1:8080 >/dev/null; then
	echo "tailscale serve failed: turn on MagicDNS and HTTPS Certificates in the tailnet's DNS settings, then deploy again"
	exit 1
fi

# ---------- The data pack, the database, the app ----------

systemctl start consigliere-sync.service || [ -d "$state/pack/.git" ] # an old copy still works if GitHub is down
if [ -n "${RESTORE_BLOB:-}" ] && [ ! -f "$state/pack/db/consigliere.db" ]; then
	sh "$dir/deploy/vm/restore.sh"
fi
systemctl restart consigliere.service

healthy() {
	i=0
	while [ $i -lt 45 ]; do
		curl -fsS -o /dev/null http://127.0.0.1:8080/ && return 0
		i=$((i + 1))
		sleep 2
	done
	return 1
}
if ! healthy; then
	journalctl -u consigliere -n 25 --no-pager -o cat | cut -c1-200
	if [ -n "$previous" ] && [ "$previous" != "$dir" ]; then
		ln -sfn "$previous" "$app/current.new" && mv -T "$app/current.new" "$app/current"
		systemctl restart consigliere.service
		echo "rolled back to $(basename "$previous")"
	fi
	exit 1
fi

# Keep the three newest releases (their names are commit hashes, so ls is safe here).
# shellcheck disable=SC2012
ls -1dt "$app"/releases/*/ | tail -n +4 | xargs -r rm -rf
log "$release is running; tailnet: $(tailscale status --json | python3 -c 'import json, sys; print(json.load(sys.stdin)["Self"]["DNSName"].rstrip("."))')"
echo CONSIGLIERE_INSTALL_OK
