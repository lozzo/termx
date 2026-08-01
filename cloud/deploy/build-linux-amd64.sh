#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd -P)"
artifact_dir="${ANYTTY_CLOUD_ARTIFACT_DIR:-$repo_root/.artifacts/cloud-linux-amd64}"
if [ -n "$(git -C "$repo_root" status --porcelain --untracked-files=all)" ]; then
  echo "refusing to build release artifacts from a dirty worktree" >&2
  exit 1
fi
cloud_version="$(git -C "$repo_root" rev-parse HEAD)"

install -d -m 0755 "$artifact_dir"
(
  cd "$repo_root"
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off go build -buildvcs=false -trimpath -ldflags "-X main.softwareVersion=$cloud_version" \
    -o "$artifact_dir/anytty-cloud-controller" ./cmd/anytty-cloud-controller
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off go build -buildvcs=false -trimpath -ldflags "-X main.defaultSoftwareVersion=$cloud_version" \
    -o "$artifact_dir/anytty-cloud-edge" ./cmd/anytty-cloud-edge
)
install -m 0644 "$repo_root/cloud/deploy/systemd/anytty-cloud-controller.service" \
  "$artifact_dir/anytty-cloud-controller.service"
install -m 0644 "$repo_root/cloud/deploy/systemd/anytty-cloud-edge.service" \
  "$artifact_dir/anytty-cloud-edge.service"

(
  cd "$artifact_dir"
  shasum -a 256 \
    anytty-cloud-controller \
    anytty-cloud-edge \
    anytty-cloud-controller.service \
    anytty-cloud-edge.service >SHA256SUMS
)
printf '%s\n' "$artifact_dir ($cloud_version)"
