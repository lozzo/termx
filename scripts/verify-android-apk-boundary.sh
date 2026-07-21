#!/usr/bin/env bash

set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 APP_APK [DEV_CLOUD_APK]" >&2
  exit 2
fi

app_apk="$1"
dev_cloud_apk="${2:-}"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/muxvia-apk-boundary.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

for apk in "$app_apk" ${dev_cloud_apk:+"$dev_cloud_apk"}; do
  if [[ ! -s "$apk" ]]; then
    echo "Android APK is missing or empty: $apk" >&2
    exit 1
  fi
  while IFS= read -r native_path; do
    [[ -n "$native_path" ]] || continue
    unzip -p "$apk" "$native_path" >"$tmp_dir/native.so"
    if strings "$tmp_dir/native.so" | rg -q 'android-spike-daemon|android-managed-1|muxvia-go-client-%d'; then
      echo "Release APK contains the PA005N1 spike daemon: $apk ($native_path)" >&2
      exit 1
    fi
  done < <(unzip -Z1 "$apk" | rg '^lib/[^/]+/libmuxvia_client\.so$' || true)
done

if ! command -v apkanalyzer >/dev/null 2>&1; then
  echo "apkanalyzer is required to verify Android class boundaries" >&2
  exit 1
fi

for apk in "$app_apk" ${dev_cloud_apk:+"$dev_cloud_apk"}; do
  apkanalyzer dex packages --defined-only "$apk" >"$tmp_dir/classes.txt"
  if ! rg -q 'com\.muxvia\.cloud\.OfficialManagedCloudFactory(?:[[:space:]]|$)' "$tmp_dir/classes.txt"; then
    echo "Muxvia APK does not define its managed Cloud factory: $apk" >&2
    exit 1
  fi
done

echo "Android single-App Cloud assembly passed"
