#!/usr/bin/env bash
# Restarts a bot's agent (it reads its secrets when it starts) and shows its last log lines.
#
#   deploy/restart.sh
#
# CONSIGLIERE_RESOURCE_GROUP picks the bot (default rg-consigliere). Needs: az signed in, bash.
set -euo pipefail
cd "$(dirname "$0")/.."
# shellcheck source=deploy/lib.sh
. deploy/lib.sh
bot_outputs

script=$(mktemp) && trap 'rm -f "$script"' EXIT
cat >"$script" <<'EOF'
#!/bin/sh
systemctl restart consigliere.service && sleep 5
journalctl -u consigliere -n 15 --no-pager -o cat | cut -c1-200
systemctl is-active consigliere.service && echo CONSIGLIERE_RESTART_OK
EOF
run_on_vm "$script" CONSIGLIERE_RESTART_OK
