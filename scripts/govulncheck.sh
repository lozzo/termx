#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$repo_root"

if (( $# == 0 )); then
  set -- ./...
fi

exec env GOWORK=off go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 "$@"
