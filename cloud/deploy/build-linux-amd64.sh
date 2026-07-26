#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd -P)"
artifact_dir="${MUXVIA_CLOUD_ARTIFACT_DIR:-$repo_root/.artifacts/cloud-linux-amd64}"

install -d -m 0755 "$artifact_dir"
(
  cd "$repo_root"
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off go build -trimpath \
    -o "$artifact_dir/muxvia-cloud-controller" ./cmd/muxvia-cloud-controller
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off go build -trimpath \
    -o "$artifact_dir/muxvia-cloud-edge" ./cmd/muxvia-cloud-edge
)

(
  cd "$artifact_dir"
  shasum -a 256 muxvia-cloud-controller muxvia-cloud-edge >SHA256SUMS
)
printf '%s\n' "$artifact_dir"
