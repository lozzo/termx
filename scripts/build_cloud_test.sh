#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
artifact_dir="${TERMX_CLOUD_TEST_ARTIFACT_DIR:-${root}/.artifacts/bin}"
companion_path="${artifact_dir}/termx-cloud"
termx_path="${artifact_dir}/termx"
manifest_path="${root}/private/cloud/companion/config/staging-public-http.json"

mkdir -p "${artifact_dir}"

manifest_base64="$(base64 <"${manifest_path}" | tr -d '\r\n')"

(
	cd "${root}/private/cloud/companion"
	GOWORK=off go build \
		-ldflags "-X main.companionVersion=v0.0.0-dev -X main.buildChannel=development -X main.embeddedDevelopmentManifestBase64=${manifest_base64}" \
		-o "${companion_path}" ./cmd/termx-cloud
)
chmod 0755 "${companion_path}"

if command -v sha256sum >/dev/null 2>&1; then
	companion_sha256="$(sha256sum "${companion_path}" | awk '{print $1}')"
else
	companion_sha256="$(shasum -a 256 "${companion_path}" | awk '{print $1}')"
fi

(
	cd "${root}"
	GOWORK=off go build \
		-ldflags "-X main.termxBuildVersion=v0.0.0-dev -X main.cloudDevelopmentCompanionName=termx-cloud -X main.cloudDevelopmentCompanionSHA256=${companion_sha256} -X main.cloudDevelopmentCompanionVersion=v0.0.0-dev -X main.cloudDevelopmentCompanionChannel=development" \
		-o "${termx_path}" ./cmd/termx
)

printf 'Built Cloud test suite:\n  %s\n  %s\n' "${termx_path}" "${companion_path}"
