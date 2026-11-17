#!/bin/sh
# Sets up the dev box (docs/dev-box.md) and (re)applies its whole setup. deploy/dev.sh setup sends this through
# `az vm run-command`, which runs it as root with these variables exported ahead of it:
#
#   KEY_VAULT_NAME   the dev box's vault: tailscale-auth-key
#   AZURE_CLIENT_ID  the VM's managed identity
#   TS_HOSTNAME      the VM's name on the tailnet
#
# Every step checks or overwrites, so it's safe to rerun. Output is kept short: run-command returns only the last 4 KiB.
set -eu
: "${KEY_VAULT_NAME:?}" "${AZURE_CLIENT_ID:?}" "${TS_HOSTNAME:?}"

# Pinned like every dependency; bump to releases at least two weeks old, with their published SHA-256s.
TAILSCALE_VERSION=1.104.2
GH_VERSION=2.103.3
GO_VERSION=1.27.3
NODE_VERSION=22.23.4
case $(uname -m) in
aarch64)
	go_file=go$GO_VERSION.linux-arm64.tar.gz go_sha=a06e99309dbcbdca1a8ccd2282c3ee146e55930886e26b4f1193269587165f49
	node_file=node-v$NODE_VERSION-linux-arm64.tar.xz node_sha=6287aa46b6c375d816e63ae66d024fbbfd70404825aabdfdf898a970871d2f4b
	;;
x86_64)
	go_file=go$GO_VERSION.linux-amd64.tar.gz go_sha=65efbc80fdc1fa2cd2631cad5b481777a68e8725f34e89f50fc6ac714e379b40
	node_file=node-v$NODE_VERSION-linux-x64.tar.xz node_sha=1805bcd35e6815559bd2eb97db438d3fdb0eb3a5e6a977b7c1badca76337e9ce
	;;
*) echo "unsupported machine $(uname -m)" && exit 1 ;;
esac
export DEBIAN_FRONTEND=noninteractive
log() { echo "==> $*"; }

# kv_get <name> prints a secret from the dev box's vault, signed in as the VM. Tokens go through stdin, not arguments.
kv_get() {
	curl -fsS -H Metadata:true \
		"http://169.254.169.254/metadata/identity/oauth2/token?api-version=2018-02-01&resource=https%3A%2F%2Fvault.azure.net&client_id=$AZURE_CLIENT_ID" |
		python3 -c 'import json, sys; print("Authorization: Bearer " + json.load(sys.stdin)["access_token"])' |
		curl -fsS -H @- "https://$KEY_VAULT_NAME.vault.azure.net/secrets/$1?api-version=7.4" |
		python3 -c 'import json, sys; sys.stdout.write(json.load(sys.stdin)["value"])'
}

# fetch <url> <sha256> <dir> [tar options] unpacks a checked download into a fresh <dir>.
fetch() {
	tmp=$(mktemp)
	curl -fsSL "$1" -o "$tmp"
	echo "$2  $tmp" | sha256sum -c --quiet
	rm -rf "$3" && mkdir -p "$3"
	dir=$3 && shift 3
	tar -xf "$tmp" -C "$dir" "$@"
	rm -f "$tmp"
}

# ---------- Packages ----------

# shellcheck disable=SC1091 # the VM's, not in this repo
. /etc/os-release
curl -fsSL "https://pkgs.tailscale.com/stable/ubuntu/$VERSION_CODENAME.noarmor.gpg" -o /usr/share/keyrings/tailscale-archive-keyring.gpg
curl -fsSL "https://pkgs.tailscale.com/stable/ubuntu/$VERSION_CODENAME.tailscale-keyring.list" -o /etc/apt/sources.list.d/tailscale.list
curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg -o /usr/share/keyrings/githubcli-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" >/etc/apt/sources.list.d/github-cli.list
log "installing packages"
apt-get -qq -o DPkg::Lock::Timeout=600 update >/dev/null
apt-get -qq -y -o DPkg::Lock::Timeout=600 install --allow-downgrades --allow-change-held-packages \
	git tmux build-essential jq ripgrep zstd xz-utils "tailscale=$TAILSCALE_VERSION" "gh=$GH_VERSION" >/dev/null
apt-mark hold tailscale gh >/dev/null

if [ "$(/usr/local/go/bin/go version 2>/dev/null || true)" != "go version go$GO_VERSION linux/$(dpkg --print-architecture)" ]; then
	log "installing Go $GO_VERSION"
	fetch "https://go.dev/dl/$go_file" "$go_sha" /usr/local/go --strip-components=1
fi
if [ "$(/usr/local/node/bin/node --version 2>/dev/null || true)" != "v$NODE_VERSION" ]; then
	log "installing Node $NODE_VERSION"
	fetch "https://nodejs.org/dist/v$NODE_VERSION/$node_file" "$node_sha" /usr/local/node --strip-components=1
fi

# Login shells (SSH, tmux) find the tools; npm and go install into the user's home, not the root-owned trees.
cat >/etc/profile.d/dev-tools.sh <<'EOF'
export NPM_CONFIG_PREFIX="$HOME/.npm-global"
export PATH="$HOME/.local/bin:$HOME/go/bin:$HOME/.npm-global/bin:/usr/local/go/bin:/usr/local/node/bin:$PATH"
EOF

# Ubuntu installs security updates daily; this lets them reboot when one needs it, early morning US Eastern.
cat >/etc/apt/apt.conf.d/52consigliere-reboot <<'EOF'
Unattended-Upgrade::Automatic-Reboot "true";
Unattended-Upgrade::Automatic-Reboot-Time "08:00";
EOF

# Builds can spike past 8 GiB; swap turns that into slowness instead of the OOM killer.
if [ ! -f /swapfile ]; then
	fallocate -l 4G /swapfile && chmod 600 /swapfile && mkswap /swapfile >/dev/null && swapon /swapfile
	echo '/swapfile none swap sw 0 0' >>/etc/fstab
fi

# ---------- The dev user ----------

# Where Claude Code and the repos live. No password and no sudo: sessions run as this user and can't change the
# machine. Admin goes through run-command (this script), as root.
id dev >/dev/null 2>&1 || useradd --create-home --shell /bin/bash dev
loginctl enable-linger dev # its tmux sessions and user services survive logging out

# Anthropic's native installer, stable channel; it updates itself after that.
if [ ! -x /home/dev/.local/bin/claude ]; then
	log "installing Claude Code"
	runuser -l dev -c 'curl -fsSL https://claude.ai/install.sh | bash -s stable' >/dev/null
fi

# ---------- Tailscale: SSH from the owner's devices ----------

systemctl enable --quiet --now tailscaled
ts_state() { tailscale status --json 2>/dev/null | python3 -c 'import json, sys; print(json.load(sys.stdin).get("BackendState", ""))' || true; }
if [ "$(ts_state)" != Running ]; then
	log "joining the tailnet as $TS_HOSTNAME"
	# A one-time key, kept in memory (/run) only as long as `tailscale up` needs it.
	key=$(mktemp -p /run)
	kv_get tailscale-auth-key >"$key"
	tailscale up --auth-key="file:$key" --hostname="$TS_HOSTNAME" --ssh || { rm -f "$key"; exit 1; }
	rm -f "$key"
fi
# Tailscale SSH: tailscaled answers port 22 on the tailnet only, and the tailnet policy decides who may sign in as dev.
tailscale set --ssh --hostname="$TS_HOSTNAME"

log "go $GO_VERSION, node $NODE_VERSION, gh $GH_VERSION, claude $(runuser -l dev -c 'claude --version' 2>/dev/null | head -1)"
log "ready: ssh dev@$(tailscale status --json | python3 -c 'import json, sys; print(json.load(sys.stdin)["Self"]["DNSName"].rstrip("."))')"
echo CONSIGLIERE_DEV_OK
