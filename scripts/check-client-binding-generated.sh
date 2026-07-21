#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if ! command -v protoc-gen-go >/dev/null 2>&1; then
  echo "protoc-gen-go is required to verify client binding generated code" >&2
  exit 1
fi

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/muxvia-binding-generated.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT
mkdir -p "$tmp_dir/go" "$tmp_dir/descriptor"

protoc -I proto \
  --go_out="$tmp_dir/go" \
  --go_opt=paths=source_relative \
  proto/bindingpb/client_binding.proto

protoc -I proto \
  --descriptor_set_out="$tmp_dir/descriptor/client-binding-v1.pb" \
  proto/bindingpb/client_binding.proto

cmp "$tmp_dir/go/bindingpb/client_binding.pb.go" proto/bindingpb/client_binding.pb.go
cmp "$tmp_dir/descriptor/client-binding-v1.pb" proto/bindingpb/testdata/client-binding-v1.pb

echo "client binding generated code is current"
