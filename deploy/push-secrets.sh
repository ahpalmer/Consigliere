#!/usr/bin/env bash
# Copies secrets into a bot's Key Vault, where the VM reads them into memory:
#   - the app's secrets file ("Section:Key" JSON, the same file `consigliere secrets set` writes), as one secret
#   - optionally the data pack's deploy key (a private SSH key whose public half is a read-only deploy key on GitHub)
#   - with --tailscale-auth-key, a one-time Tailscale auth key (prompted for, not echoed) the VM joins the tailnet with
# Values go straight from file (or prompt) to Key Vault and are never printed. The agent reads its secrets when it
# starts, so restart it afterwards: deploy/restart.sh.
#
#   deploy/push-secrets.sh <secrets.json> [deploy key file]
#   deploy/push-secrets.sh --tailscale-auth-key
#
# CONSIGLIERE_RESOURCE_GROUP picks the bot (default rg-consigliere).
set -euo pipefail

arg=${1:?usage: deploy/push-secrets.sh <secrets.json> [deploy key file] | --tailscale-auth-key}
[[ -f $arg ]] && arg=$(realpath "$arg")
deploy_key=${2:-}
[[ -n $deploy_key ]] && deploy_key=$(realpath "$deploy_key")
cd "$(dirname "$0")/.."
# shellcheck source=deploy/lib.sh
. deploy/lib.sh
bot_outputs

save() { # save <name> <file>
  az keyvault secret set --vault-name "$vault" --name "$1" --file "$(native "$2")" --encoding utf-8 -o none
  echo "Saved $1 to $vault"
}

if [[ $arg == --tailscale-auth-key ]]; then
  read -rsp "Paste the Tailscale auth key: " key && echo
  tmp=$(mktemp) && trap 'rm -f "$tmp"' EXIT
  printf %s "$key" >"$tmp"
  save tailscale-auth-key "$tmp"
  exit 0
fi

# The agent won't start without these (keys only; values aren't read here).
missing=()
for key in Anthropic:ApiKey Telegram:BotToken Telegram:AllowedChatIds:0 Web:Token; do
  grep -q "\"$key\"" "$arg" || missing+=("$key")
done
if ((${#missing[@]})); then
  echo "missing from $arg: ${missing[*]} (add with: go run ./cmd/consigliere --secrets $arg secrets set KEY VALUE)" >&2
  exit 1
fi

save consigliere-secrets "$arg"
[[ -z $deploy_key ]] || save datapack-deploy-key "$deploy_key"
