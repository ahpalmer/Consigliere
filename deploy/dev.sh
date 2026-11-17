#!/usr/bin/env bash
# Runs the dev box (docs/dev-box.md):
#
#   deploy/dev.sh setup               installs or updates everything on it (deploy/dev/setup.sh); safe to rerun
#   deploy/dev.sh tailscale-auth-key  saves a one-time Tailscale auth key to its Key Vault
#   deploy/dev.sh github-token        checks a GitHub token and saves it, with its expiry, to its Key Vault
#   deploy/dev.sh start | stop        starts it, or stops and deallocates it so compute stops billing
#
# Keys are prompted for, not echoed, and never printed. setup needs DEV_PACK_REPO (HTTPS clone URL of the data pack
# with code/repos.yaml), GIT_USER_NAME and GIT_USER_EMAIL. Needs: az signed in, bash.
set -euo pipefail
cd "$(dirname "$0")/.."
# shellcheck source=deploy/lib.sh
. deploy/lib.sh
rg="rg-consigliere-dev"

outputs=$(az deployment group show -g "$rg" -n consigliere-dev --query \
  "[properties.outputs.vmName.value, properties.outputs.keyVaultName.value, properties.outputs.devClientId.value]" \
  -o tsv | tr '\r\n' '  ')
read -r vm vault client_id <<<"$outputs"
[[ -n ${client_id:-} ]] || { echo "no outputs in $rg: deploy infra/dev.bicep first (docs/dev-box.md)" >&2; exit 1; }

# save <name> <value> [az keyvault secret set options] goes through a temp file, never the command line.
save() {
  local tmp
  tmp=$(mktemp)
  printf %s "$2" >"$tmp"
  az keyvault secret set --vault-name "$vault" --name "$1" --file "$(native "$tmp")" --encoding utf-8 "${@:3}" -o none ||
    { rm -f "$tmp"; return 1; }
  rm -f "$tmp"
  echo "Saved $1 to $vault"
}

case ${1:-} in
setup)
  : "${DEV_PACK_REPO:?set DEV_PACK_REPO to the HTTPS clone URL of the data pack}"
  : "${GIT_USER_NAME:?set GIT_USER_NAME for commits made on the dev box}"
  : "${GIT_USER_EMAIL:?set GIT_USER_EMAIL (your GitHub noreply address) for commits made on the dev box}"
  files=$(tar -czf - -C deploy/dev box | base64 -w0)
  run_on_vm deploy/dev/setup.sh CONSIGLIERE_DEV_OK "KEY_VAULT_NAME=$vault" "AZURE_CLIENT_ID=$client_id" \
    "TS_HOSTNAME=$vm" "DEV_PACK_REPO=$DEV_PACK_REPO" "GIT_USER_NAME=$GIT_USER_NAME" "GIT_USER_EMAIL=$GIT_USER_EMAIL" \
    "DEV_FILES=$files"
  ;;
tailscale-auth-key)
  read -rsp "Paste the Tailscale auth key: " key && echo
  save tailscale-auth-key "$key"
  ;;
github-token)
  read -rsp "Paste the GitHub token: " key && echo
  # GitHub says whose token it is and when it expires; Key Vault keeps the expiry for the renewal reminder.
  headers=$(printf 'Authorization: Bearer %s\n' "$key" | curl -fsS -D - -o /dev/null -H @- https://api.github.com/user) ||
    { echo "GitHub didn't accept that token" >&2; exit 1; }
  expiry=$(sed -n 's/^github-authentication-token-expiration: *//Ip' <<<"$headers" | tr -d '\r')
  if [[ -n $expiry ]]; then
    save github-token "$key" --expires "$(date -u -d "$expiry" +%Y-%m-%dT%H:%M:%SZ)"
    echo "It expires $expiry."
  else
    save github-token "$key"
    echo "It has no expiry date: make one that expires (docs/dev-box.md)."
  fi
  ;;
start)
  az vm start -g "$rg" -n "$vm" -o none && echo "$vm is running"
  ;;
stop)
  az vm deallocate -g "$rg" -n "$vm" -o none && echo "$vm is stopped (deallocated)"
  ;;
*)
  sed -n '2,10p' "$0" >&2
  exit 2
  ;;
esac
