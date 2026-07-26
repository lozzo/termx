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
if ! command -v protoc-gen-go-grpc >/dev/null 2>&1; then
  echo "protoc-gen-go-grpc is required to verify generated Cloud gRPC files" >&2
  exit 1
fi

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/muxvia-generated-check.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT
mkdir -p "$tmp_dir/go" "$tmp_dir/api" "$tmp_dir/runtime" "$tmp_dir/wire" "$tmp_dir/descriptor"

api_proto=(
  proto/apipb/common.proto
  proto/remoteauthpb/remote_auth.proto
  proto/apipb/access_remote.proto
  proto/apipb/storage.proto
  proto/apipb/terminal.proto
  proto/apipb/events.proto
  proto/apipb/file.proto
  proto/apipb/history.proto
  proto/apipb/runtime.proto
  proto/apipb/workbench.proto
  proto/apipb/application.proto
)
api_ts_proto=(
  proto/apipb/common.proto
  proto/apipb/terminal.proto
  proto/apipb/history.proto
  proto/apipb/file.proto
  proto/apipb/storage.proto
  proto/apipb/workbench.proto
  proto/apipb/runtime.proto
  proto/apipb/access_remote.proto
  proto/apipb/events.proto
  proto/apipb/application.proto
)
portable_ts_proto=(
  proto/remoteauthpb/remote_auth.proto
)
cloud_proto=(
  proto/cloud/v1/common.proto
  proto/cloud/v1/edge_config.proto
  proto/cloud/v1/runtime.proto
  proto/cloud/v1/edge_control.proto
)
# Go 与 TypeScript 都从 proto 源码生成到临时目录；检查过程不改工作树。
protoc -I proto \
  --go_out="$tmp_dir/go" \
  --go_opt=paths=source_relative \
  "${api_proto[@]}" \
  proto/wirepb/terminal.proto

protoc -I proto \
  --go_out="$tmp_dir/go" \
  --go_opt=paths=source_relative \
  --go-grpc_out="$tmp_dir/go" \
  --go-grpc_opt=paths=source_relative \
  "${cloud_proto[@]}"

protoc -I proto \
  --descriptor_set_out="$tmp_dir/descriptor/cloud-v1.pb" \
  "${cloud_proto[@]}"

protoc -I proto \
  --descriptor_set_out="$tmp_dir/descriptor/public-api-v1.pb" \
  "${api_proto[@]}"
PATH="$repo_root/node_modules/.bin:$PATH" NODE_NO_WARNINGS=1 protoc \
  -I proto \
  --es_out="$tmp_dir/api" \
  --es_opt=target=ts,import_extension=none \
  "${api_ts_proto[@]}" \
  "${portable_ts_proto[@]}"
PATH="$repo_root/node_modules/.bin:$PATH" NODE_NO_WARNINGS=1 protoc \
  -I proto \
  --es_out="$tmp_dir/api" \
  --es_opt=target=ts,import_extension=none \
  "${cloud_proto[@]}"
PATH="$repo_root/node_modules/.bin:$PATH" NODE_NO_WARNINGS=1 protoc \
  -I proto/wirepb \
  --es_out="$tmp_dir/wire" \
  --es_opt=target=ts,import_extension=none \
  proto/wirepb/terminal.proto
perl -0pi -e 's/\s*\z/\n/' "$tmp_dir"/api/apipb/*_pb.ts "$tmp_dir"/api/remoteauthpb/*_pb.ts "$tmp_dir"/api/cloud/v1/*_pb.ts "$tmp_dir/wire/terminal_pb.ts"

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

for source in "${api_ts_proto[@]}"; do
  name="$(basename "$source" .proto)"
  check_generated_file "$tmp_dir/go/apipb/${name}.pb.go" "proto/apipb/${name}.pb.go"
  check_generated_file "$tmp_dir/api/apipb/${name}_pb.ts" "clients/ui/src/generated/apipb/${name}_pb.ts"
done
check_generated_file "$tmp_dir/api/remoteauthpb/remote_auth_pb.ts" clients/ui/src/generated/remoteauthpb/remote_auth_pb.ts
check_generated_file "$tmp_dir/go/remoteauthpb/remote_auth.pb.go" proto/remoteauthpb/remote_auth.pb.go
check_generated_file "$tmp_dir/go/wirepb/terminal.pb.go" proto/wirepb/terminal.pb.go
check_generated_file "$tmp_dir/descriptor/public-api-v1.pb" proto/apipb/testdata/public-api-v1.pb
check_generated_file "$tmp_dir/wire/terminal_pb.ts" clients/ui/src/generated/wirepb/terminal_pb.ts
for source in "${cloud_proto[@]}"; do
  name="$(basename "$source" .proto)"
  check_generated_file "$tmp_dir/go/cloud/v1/${name}.pb.go" "proto/cloud/v1/${name}.pb.go"
  check_generated_file "$tmp_dir/api/cloud/v1/${name}_pb.ts" "clients/ui/src/generated/cloud/v1/${name}_pb.ts"
done
check_generated_file "$tmp_dir/go/cloud/v1/edge_control_grpc.pb.go" proto/cloud/v1/edge_control_grpc.pb.go
check_generated_file "$tmp_dir/descriptor/cloud-v1.pb" proto/cloud/v1/testdata/cloud-v1.pb
echo "generated code is current"
