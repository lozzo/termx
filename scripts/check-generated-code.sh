#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if [[ ! -x node_modules/.bin/protoc-gen-es ]]; then
  echo "protoc-gen-es is missing; run npm ci from the repository root" >&2
  exit 1
fi
if ! command -v protoc-gen-go >/dev/null 2>&1; then
  echo "protoc-gen-go is required to verify generated Go protobuf files" >&2
  exit 1
fi

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/termx-generated-check.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT
mkdir -p "$tmp_dir/go" "$tmp_dir/runtime" "$tmp_dir/wire"

# Go 与 TypeScript 都从 proto 源码生成到临时目录；检查过程不改工作树。
protoc -I proto \
  --go_out="$tmp_dir/go" \
  --go_opt=paths=source_relative \
  proto/cloudpb/cloud_companion.proto \
  proto/remoteauthpb/remote_auth.proto \
  proto/wirepb/terminal.proto

PATH="$repo_root/node_modules/.bin:$PATH" NODE_NO_WARNINGS=1 protoc \
  -I proto/runtimepb \
  --es_out="$tmp_dir/runtime" \
  --es_opt=target=ts,import_extension=none \
  proto/runtimepb/runtime.proto
PATH="$repo_root/node_modules/.bin:$PATH" NODE_NO_WARNINGS=1 protoc \
  -I proto/wirepb \
  --es_out="$tmp_dir/wire" \
  --es_opt=target=ts,import_extension=none \
  proto/wirepb/terminal.proto
perl -0pi -e 's/\s*\z/\n/' "$tmp_dir/runtime/runtime_pb.ts" "$tmp_dir/wire/terminal_pb.ts"

check_generated_file() {
  local generated="$1"
  local committed="$2"
  if cmp -s "$generated" "$committed"; then
    return
  fi
  echo "generated file is stale: $committed" >&2
  diff -u "$committed" "$generated" | sed -n '1,120p' >&2 || true
  exit 1
}

check_generated_file "$tmp_dir/go/cloudpb/cloud_companion.pb.go" proto/cloudpb/cloud_companion.pb.go
check_generated_file "$tmp_dir/go/remoteauthpb/remote_auth.pb.go" proto/remoteauthpb/remote_auth.pb.go
check_generated_file "$tmp_dir/go/wirepb/terminal.pb.go" proto/wirepb/terminal.pb.go
check_generated_file "$tmp_dir/runtime/runtime_pb.ts" clients/ui/src/generated/runtimepb/runtime_pb.ts
check_generated_file "$tmp_dir/wire/terminal_pb.ts" clients/ui/src/generated/wirepb/terminal_pb.ts
echo "generated code is current"
