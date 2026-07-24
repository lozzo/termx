#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
artifact_dir="${MUXVIA_CLOUD_TEST_ARTIFACT_DIR:-${root}/.artifacts/bin}"
muxvia_path="${artifact_dir}/muxvia"
manifest_path="${root}/private/cloud/companion/config/staging-public-https.json"
bundle_stage="${root}/cmd/muxvia/cloud_bundle"
companion_path="${bundle_stage}/muxvia-cloud"

mkdir -p "${artifact_dir}"
mkdir -p "${bundle_stage}"
trap 'rm -rf "${bundle_stage}"' EXIT

manifest_base64="$(base64 <"${manifest_path}" | tr -d '\r\n')"

(
	cd "${root}/private/cloud/companion"
	GOWORK=off go build \
		-ldflags "-X main.companionVersion=v0.0.0-dev -X main.buildChannel=development -X main.embeddedDevelopmentManifestBase64=${manifest_base64}" \
		-o "${companion_path}" ./cmd/muxvia-cloud
)
chmod 0755 "${companion_path}"

if command -v sha256sum >/dev/null 2>&1; then
	companion_sha256="$(sha256sum "${companion_path}" | awk '{print $1}')"
else
	companion_sha256="$(shasum -a 256 "${companion_path}" | awk '{print $1}')"
fi

(
	cd "${root}"
	GOWORK=off go build -tags muxvia_cloud_bundled \
		-ldflags "-X main.muxviaBuildVersion=v0.0.0-dev -X main.cloudDevelopmentCompanionSHA256=${companion_sha256} -X main.cloudDevelopmentCompanionVersion=v0.0.0-dev -X main.cloudDevelopmentCompanionChannel=development" \
		-o "${muxvia_path}" ./cmd/muxvia
)

printf 'Built default single-file Cloud-enabled muxvia:\n  %s\n' "${muxvia_path}"
