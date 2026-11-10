#!/usr/bin/env bash
# Shared by the deploy scripts (sourced, not run). CONSIGLIERE_RESOURCE_GROUP picks the bot: rg-consigliere (the
# default) or rg-consigliere-alison.

rg=${CONSIGLIERE_RESOURCE_GROUP:-rg-consigliere}

# bot_outputs sets vm, vault, client_id, storage, backups and releases from the bot's infra deployment.
bot_outputs() {
  local outputs
  outputs=$(az deployment group show -g "$rg" -n consigliere --query \
    "[properties.outputs.vmName.value, properties.outputs.keyVaultName.value, properties.outputs.appClientId.value, properties.outputs.storageAccountName.value, properties.outputs.backupContainerUrl.value, properties.outputs.releaseContainerUrl.value]" \
    -o tsv | tr '\r\n' '  ')
  # shellcheck disable=SC2034 # used by the scripts that source this
  read -r vm vault client_id storage backups releases <<<"$outputs"
  [[ -n ${releases:-} ]] || { echo "no VM outputs in $rg: deploy infra/ first (docs/deploy.md)" >&2; return 1; }
}

# native turns a path into one the Azure CLI can open (on Windows it's a Windows program).
native() {
  if command -v cygpath >/dev/null; then cygpath -w "$1"; else printf '%s\n' "$1"; fi
}

# run_on_vm <script> <marker> [NAME=value...] runs a deploy/vm script as root on the VM with the variables exported
# ahead of it, prints its output, and fails unless the output has the marker (run-command itself succeeds whatever
# the script's exit code). Values are single-quoted, so nothing in them is expanded on the VM.
run_on_vm() {
  local script=$1 marker=$2 payload out kv value
  shift 2
  payload=$(mktemp)
  {
    echo '#!/bin/sh'
    for kv in "$@"; do
      value=${kv#*=}
      printf "export %s='%s'\n" "${kv%%=*}" "${value//\'/\'\\\'\'}"
    done
    sed 1d "$script"
  } >"$payload"
  out=$(az vm run-command invoke -g "$rg" -n "$vm" --command-id RunShellScript --scripts @"$(native "$payload")" \
    --query 'value[0].message' -o tsv)
  rm -f "$payload"
  printf '%s\n' "$out"
  grep -q "^$marker" <<<"$out" || { echo "failed on $vm (output above)" >&2; return 1; }
}
