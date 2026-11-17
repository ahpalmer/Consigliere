#!/usr/bin/env bash
# Runs the dev box (docs/dev-box.md):
#
#   deploy/dev.sh setup               installs or updates everything on it (deploy/dev/setup.sh); safe to rerun
#   deploy/dev.sh tailscale-auth-key  saves a one-time Tailscale auth key (prompted for, not echoed) to its Key Vault
#   deploy/dev.sh start | stop        starts it, or stops and deallocates it so compute stops billing
#
# Needs: az signed in, bash.
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

case ${1:-} in
setup)
  run_on_vm deploy/dev/setup.sh CONSIGLIERE_DEV_OK "KEY_VAULT_NAME=$vault" "AZURE_CLIENT_ID=$client_id" "TS_HOSTNAME=$vm"
  ;;
tailscale-auth-key)
  read -rsp "Paste the Tailscale auth key: " key && echo
  tmp=$(mktemp) && trap 'rm -f "$tmp"' EXIT
  printf %s "$key" >"$tmp"
  az keyvault secret set --vault-name "$vault" --name tailscale-auth-key --file "$(native "$tmp")" --encoding utf-8 -o none
  echo "Saved tailscale-auth-key to $vault"
  ;;
start)
  az vm start -g "$rg" -n "$vm" -o none && echo "$vm is running"
  ;;
stop)
  az vm deallocate -g "$rg" -n "$vm" -o none && echo "$vm is stopped (deallocated)"
  ;;
*)
  sed -n '2,8p' "$0" >&2
  exit 2
  ;;
esac
