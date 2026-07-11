#!/usr/bin/env bash

set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 COMMUNITY_APK [OFFICIAL_APK]" >&2
  exit 2
fi

community_apk="$1"
official_apk="${2:-}"
for apk in "$community_apk" ${official_apk:+"$official_apk"}; do
  if [[ ! -s "$apk" ]]; then
    echo "Android APK is missing or empty: $apk" >&2
    exit 1
  fi
done

if ! command -v apkanalyzer >/dev/null 2>&1; then
  echo "apkanalyzer is required to verify Android class boundaries" >&2
  exit 1
fi

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/termx-apk-boundary.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

apkanalyzer dex packages --defined-only "$community_apk" >"$tmp_dir/community.txt"
if rg -q 'com\.termx\.cloud(?:\.|$)' "$tmp_dir/community.txt"; then
  echo "Community APK contains private cloud classes" >&2
  rg 'com\.termx\.cloud(?:\.|$)' "$tmp_dir/community.txt" >&2
  exit 1
fi

if [[ -n "$official_apk" ]]; then
  apkanalyzer dex packages --defined-only "$official_apk" >"$tmp_dir/official.txt"
  if ! rg -q 'com\.termx\.cloud\.OfficialManagedCloudFactory(?:[[:space:]]|$)' "$tmp_dir/official.txt"; then
    echo "Official APK does not define OfficialManagedCloudFactory" >&2
    exit 1
  fi
fi

echo "Android APK class boundary passed"
