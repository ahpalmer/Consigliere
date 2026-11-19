#!/bin/sh
# Sets up the dev box (docs/dev-box.md) and (re)applies its whole setup. deploy/dev.sh setup sends this through
# `az vm run-command`, which runs it as root with these variables exported ahead of it:
#
#   KEY_VAULT_NAME   the dev box's vault: tailscale-auth-key, github-token
#   AZURE_CLIENT_ID  the VM's managed identity
#   TS_HOSTNAME      the VM's name on the tailnet
#   DEV_PACK_REPO    HTTPS clone URL of the data pack holding code/repos.yaml, the list of repos to sync
#   GIT_USER_NAME    name and email on the dev user's commits
#   GIT_USER_EMAIL
#   DEV_FILES        deploy/dev/box as a base64 .tar.gz: helper scripts, the pre-push hook, systemd units
#
# Every step checks or overwrites, so it's safe to rerun. Output is kept short: run-command returns only the last 4 KiB.
set -eu
: "${KEY_VAULT_NAME:?}" "${AZURE_CLIENT_ID:?}" "${TS_HOSTNAME:?}" "${DEV_PACK_REPO:?}" "${GIT_USER_NAME:?}" "${GIT_USER_EMAIL:?}" "${DEV_FILES:?}"

# Pinned like every dependency; bump to releases at least two weeks old, with their published hashes.
TAILSCALE_VERSION=1.104.2
GH_VERSION=2.103.3
GO_VERSION=1.27.3
NODE_VERSION=22.23.4
DOTNET_SDK_VERSION=10.0.403
DOTNET8_RUNTIME_VERSION=8.0.33
PWSH_VERSION=7.6.9
case $(uname -m) in
aarch64)
	gh_file=gh_${GH_VERSION}_linux_arm64.tar.gz gh_sha=ee32df2323c8847f5fac6cb6b4b69ca75828e9329a8f22f677021f7b3306ce63
	go_file=go$GO_VERSION.linux-arm64.tar.gz go_sha=a06e99309dbcbdca1a8ccd2282c3ee146e55930886e26b4f1193269587165f49
	node_file=node-v$NODE_VERSION-linux-arm64.tar.xz node_sha=6287aa46b6c375d816e63ae66d024fbbfd70404825aabdfdf898a970871d2f4b
	sdk_file=dotnet-sdk-$DOTNET_SDK_VERSION-linux-arm64.tar.gz
	sdk_sha=0966aa2b70f4677b8319be9df447a78a0dcd6164836425b55a68fd714f4023feb87b4cac21b9a996b79472ad54487cd0adcd2a9dfd570e2b2a1d26423a2baa9b
	rt8_file=dotnet-runtime-$DOTNET8_RUNTIME_VERSION-linux-arm64.tar.gz
	rt8_sha=a9ba25eae72f98e25233ae202d6e4fb6663b2c8da23bcd9d797c89e8b1d01f51de90f1994dc7101a5fef56db401c05af184c5e251a9bce570adf044c4e3ae5a9
	pwsh_file=powershell-$PWSH_VERSION-linux-arm64.tar.gz pwsh_sha=02eb9991af447c7e7fe881acde195961afd397c40b8eb1c2b3856b84ec2d85f2
	;;
x86_64)
	gh_file=gh_${GH_VERSION}_linux_amd64.tar.gz gh_sha=4c806978ffce1cec70853302a56d70ac6da4f19bae7adca4e1a8ab00633dd91d
	go_file=go$GO_VERSION.linux-amd64.tar.gz go_sha=65efbc80fdc1fa2cd2631cad5b481777a68e8725f34e89f50fc6ac714e379b40
	node_file=node-v$NODE_VERSION-linux-x64.tar.xz node_sha=1805bcd35e6815559bd2eb97db438d3fdb0eb3a5e6a977b7c1badca76337e9ce
	sdk_file=dotnet-sdk-$DOTNET_SDK_VERSION-linux-x64.tar.gz
	sdk_sha=b7b3d8461cf98b07d2552ce9199702fa3ad010ac2671a5f8cd5136f7534e50f6855ba7bbb02b11494563f1d04156276a31cda1103b900e5eecd0d9473dc7b9c5
	rt8_file=dotnet-runtime-$DOTNET8_RUNTIME_VERSION-linux-x64.tar.gz
	rt8_sha=b314039d337a8c5c4be6a6f99712a2618cbeb5e1ab7cac7a5c1cbf1c97bd355f5d116b9932a8cfc604ac7849f74638896eba0534892c6518311908aba29b4f7b
	pwsh_file=powershell-$PWSH_VERSION-linux-x64.tar.gz pwsh_sha=1da6d6d6035d651f20e70ddee7c4c15ac6ab8b9fb40ee92240d91263f28a884b
	;;
*) echo "unsupported machine $(uname -m)" && exit 1 ;;
esac
box=/opt/consigliere-dev
export DEBIAN_FRONTEND=noninteractive
cd / # run-command starts in a root-only directory, where commands run as dev fail
log() { echo "==> $*"; }

# unpack <url> <sha256 or sha512> <dir> [tar options] downloads, checks the hash and unpacks into <dir>.
unpack() {
	tmp=$(mktemp)
	curl -fsSL "$1" -o "$tmp"
	if [ ${#2} -eq 128 ]; then sum=sha512sum; else sum=sha256sum; fi
	echo "$2  $tmp" | $sum -c --quiet
	dir=$3 && shift 3
	mkdir -p "$dir" && tar -xf "$tmp" -C "$dir" "$@"
	rm -f "$tmp"
}

# ---------- Packages ----------

# shellcheck disable=SC1091 # the VM's, not in this repo
. /etc/os-release
curl -fsSL "https://pkgs.tailscale.com/stable/ubuntu/$VERSION_CODENAME.noarmor.gpg" -o /usr/share/keyrings/tailscale-archive-keyring.gpg
curl -fsSL "https://pkgs.tailscale.com/stable/ubuntu/$VERSION_CODENAME.tailscale-keyring.list" -o /etc/apt/sources.list.d/tailscale.list
# GitHub's apt repo keeps only the newest gh, so gh comes from its release tarball like the tools below.
rm -f /etc/apt/sources.list.d/github-cli.list
log "installing packages"
apt-get -qq -o DPkg::Lock::Timeout=600 update >/dev/null
apt-get -qq -y -o DPkg::Lock::Timeout=600 install --allow-downgrades --allow-change-held-packages \
	git tmux build-essential jq ripgrep zstd xz-utils python3-yaml libicu74 "tailscale=$TAILSCALE_VERSION" >/dev/null
apt-mark hold tailscale >/dev/null

if ! /usr/local/gh/bin/gh --version 2>/dev/null | grep -q "^gh version $GH_VERSION "; then
	log "installing gh $GH_VERSION"
	rm -rf /usr/local/gh && unpack "https://github.com/cli/cli/releases/download/v$GH_VERSION/$gh_file" "$gh_sha" /usr/local/gh --strip-components=1
fi

if [ "$(/usr/local/go/bin/go version 2>/dev/null || true)" != "go version go$GO_VERSION linux/$(dpkg --print-architecture)" ]; then
	log "installing Go $GO_VERSION"
	rm -rf /usr/local/go && unpack "https://go.dev/dl/$go_file" "$go_sha" /usr/local/go --strip-components=1
fi
if [ "$(/usr/local/node/bin/node --version 2>/dev/null || true)" != "v$NODE_VERSION" ]; then
	log "installing Node $NODE_VERSION"
	rm -rf /usr/local/node && unpack "https://nodejs.org/dist/v$NODE_VERSION/$node_file" "$node_sha" /usr/local/node --strip-components=1
fi
# The .NET 10 SDK builds every repo (net8.0 targets too); the .NET 8 runtime runs net8.0 apps and tests.
if [ ! -d /usr/local/dotnet/sdk/$DOTNET_SDK_VERSION ] || [ ! -d /usr/local/dotnet/shared/Microsoft.NETCore.App/$DOTNET8_RUNTIME_VERSION ]; then
	log "installing .NET SDK $DOTNET_SDK_VERSION and runtime $DOTNET8_RUNTIME_VERSION"
	rm -rf /usr/local/dotnet
	unpack "https://builds.dotnet.microsoft.com/dotnet/Sdk/$DOTNET_SDK_VERSION/$sdk_file" "$sdk_sha" /usr/local/dotnet
	unpack "https://builds.dotnet.microsoft.com/dotnet/Runtime/$DOTNET8_RUNTIME_VERSION/$rt8_file" "$rt8_sha" /usr/local/dotnet
fi
if [ "$(/usr/local/powershell/pwsh --version 2>/dev/null || true)" != "PowerShell $PWSH_VERSION" ]; then
	log "installing PowerShell $PWSH_VERSION"
	rm -rf /usr/local/powershell
	unpack "https://github.com/PowerShell/PowerShell/releases/download/v$PWSH_VERSION/$pwsh_file" "$pwsh_sha" /usr/local/powershell
	chmod 755 /usr/local/powershell/pwsh
fi

# Login shells (SSH, tmux) find the tools; npm, go and dotnet tools install into the user's home.
cat >/etc/profile.d/dev-tools.sh <<'EOF'
export DOTNET_ROOT=/usr/local/dotnet DOTNET_CLI_TELEMETRY_OPTOUT=1 NPM_CONFIG_PREFIX="$HOME/.npm-global"
export PATH="$HOME/.local/bin:$HOME/go/bin:$HOME/.npm-global/bin:$HOME/.dotnet/tools:/usr/local/go/bin:/usr/local/node/bin:/usr/local/dotnet:/usr/local/powershell:$PATH"
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

# ---------- The dev box's own files ----------

# Names and URLs only; the secrets stay in Key Vault and are read into memory when used.
mkdir -p /etc/consigliere-dev
cat >/etc/consigliere-dev/env <<EOF
KEY_VAULT_NAME=$KEY_VAULT_NAME
AZURE_CLIENT_ID=$AZURE_CLIENT_ID
DEV_PACK_REPO=$DEV_PACK_REPO
EOF
rm -rf "$box.new" && mkdir -p "$box.new"
echo "$DEV_FILES" | base64 -d | tar -xz -C "$box.new" --strip-components=1
chown -R root:root "$box.new" && chmod -R go-w,a+rX "$box.new" && chmod 755 "$box.new"/bin/* "$box.new/pre-push"
rm -rf "$box" && mv "$box.new" "$box"
cp "$box"/bin/* /usr/local/bin/ # gh here wraps /usr/local/gh/bin/gh
cp "$box"/*.service "$box"/*.timer /etc/systemd/system/
systemctl daemon-reload

# ---------- The dev user ----------

# Where Claude Code and the repos live. No password and no sudo: sessions run as this user and can't change the
# machine. Admin goes through run-command (this script), as root.
id dev >/dev/null 2>&1 || useradd --create-home --shell /bin/bash dev
loginctl enable-linger dev # its tmux sessions and user services survive logging out

as_dev() { runuser -u dev -- "$@"; }
as_dev git config --global user.name "$GIT_USER_NAME"
as_dev git config --global user.email "$GIT_USER_EMAIL"
as_dev git config --global init.defaultBranch main
as_dev git config --global pull.ff only
as_dev git config --global push.autoSetupRemote true
# GitHub over HTTPS with the token from Key Vault (git-credential-keyvault), and nothing else stores credentials.
as_dev git config --global --replace-all credential.https://github.com.helper keyvault

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
	dev-kv-get tailscale-auth-key >"$key"
	tailscale up --auth-key="file:$key" --hostname="$TS_HOSTNAME" --ssh || { rm -f "$key"; exit 1; }
	rm -f "$key"
fi
# Tailscale SSH: tailscaled answers port 22 on the tailnet only, and the tailnet policy decides who may sign in as dev.
tailscale set --ssh --hostname="$TS_HOSTNAME"

# ---------- The repos ----------

systemctl enable --quiet --now dev-sync.timer
if dev-kv-get github-token >/dev/null 2>&1; then
	log "syncing repos"
	systemctl start dev-sync.service || journalctl -u dev-sync -n 10 --no-pager -o cat | cut -c1-200
else
	log "no github-token in $KEY_VAULT_NAME yet: repos sync once it's saved (deploy/dev.sh github-token)"
fi

# ---------- The agent: the dashboard's Code page ----------

# Restarting it leaves open sessions running (KillMode=process).
systemctl enable --quiet dev-agent.service
systemctl restart dev-agent.service

log "go $GO_VERSION, node $NODE_VERSION, .NET $DOTNET_SDK_VERSION, pwsh $PWSH_VERSION, gh $GH_VERSION, claude $(runuser -l dev -c 'claude --version' 2>/dev/null | head -1)"
# shellcheck disable=SC2012 # repo names from code/repos.yaml, checked by dev-sync
log "repos: $(ls /home/dev/src 2>/dev/null | tr '\n' ' ')"
log "ready: ssh dev@$(tailscale status --json | python3 -c 'import json, sys; print(json.load(sys.stdin)["Self"]["DNSName"].rstrip("."))')"
echo CONSIGLIERE_DEV_OK
