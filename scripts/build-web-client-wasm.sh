#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output_root="${1:-${repo_root}/clients/ui/public/termx-wasm}"
wasm_exec="$(go env GOROOT)/lib/wasm/wasm_exec.js"

if [[ ! -f "$wasm_exec" ]]; then
  echo "Go wasm_exec.js is unavailable at $wasm_exec" >&2
  exit 1
fi

mkdir -p "$output_root"
(
  cd "$repo_root"
  GOOS=js GOARCH=wasm go build -trimpath -o "$output_root/termx-client.wasm" ./client/binding/wasmlib
)
cp "$wasm_exec" "$output_root/wasm_exec.js"

echo "Web Client Engine artifacts written to $output_root"
