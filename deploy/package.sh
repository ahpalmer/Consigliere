#!/usr/bin/env bash
# Builds a release: the Arm64 Linux binary (dashboard embedded) plus the config and the files the VM needs, as one
# tarball for deploy/deploy.sh. Build the dashboard first: cd web && npm ci && npm run build.
#
#   deploy/package.sh <out.tar.gz>
set -euo pipefail

out=$(realpath -m "${1:?usage: deploy/package.sh <out.tar.gz>}")
cd "$(dirname "$0")/.."
[[ -f internal/web/dist/index.html ]] || { echo "build the dashboard first: cd web && npm ci && npm run build" >&2; exit 1; }

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o "$stage/consigliere" ./cmd/consigliere
mkdir -p "$stage/config" "$stage/deploy/vm" "$(dirname "$out")"
cp config/consigliere.json "$stage/config/"
cp deploy/vm/* "$stage/deploy/vm/"
tar -czf "$out" -C "$stage" .
echo "$out"
