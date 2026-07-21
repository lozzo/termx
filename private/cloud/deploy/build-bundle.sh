#!/bin/sh
set -eu

root="$(CDPATH= cd -- "$(dirname -- "$0")/../../.." && pwd)"
artifact_dir="${MUXVIA_CLOUD_ARTIFACT_DIR:-$root/.artifacts/cloud-deploy/bundle}"

rm -rf "$artifact_dir"
mkdir -p "$artifact_dir/bin" "$artifact_dir/web" "$artifact_dir/config"

(cd "$root/private/cloud/web-controller/web" && npm run build)

for command in controller edge bootstrap; do
  case "$command" in
    controller) package="./private/cloud/controller/cmd/muxvia-cloud-controller" ;;
    edge) package="./private/cloud/edge/cmd/muxvia-cloud-edge" ;;
    bootstrap) package="./private/cloud/devcloud/cmd/muxvia-cloud-bootstrap" ;;
  esac
  output="$artifact_dir/bin/muxvia-cloud-$command"
  (cd "$root" && env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK="$root/go.work" go build -trimpath -o "$output" "$package")
done

cp -R "$root/private/cloud/web-controller/web/dist/." "$artifact_dir/web/"
cp "$root/private/cloud/web-controller/config/plans.json" "$artifact_dir/config/plans.json"
COPYFILE_DISABLE=1 tar -C "$artifact_dir" -czf "$artifact_dir/muxvia-cloud-linux-amd64.tar.gz" bin web config
printf '%s\n' "$artifact_dir/muxvia-cloud-linux-amd64.tar.gz"

