#!/usr/bin/env bash

set -euo pipefail

fail() {
  printf '%s\n' "Android APK boundary failed: $*" >&2
  exit 1
}

if [[ $# -ne 1 ]]; then
  printf '%s\n' "usage: $0 APP_APK" >&2
  exit 2
fi

for tool in unzip strings rg; do
  command -v "$tool" >/dev/null 2>&1 || fail "required tool is unavailable: $tool"
done

app_apk="$1"
[[ -s "$app_apk" ]] || fail "APK is missing or empty: $app_apk"

if ! apk_entries="$(unzip -Z1 "$app_apk")"; then
  fail "APK is not a readable ZIP archive: $app_apk"
fi

expected_abis_value="${ANYTTY_ANDROID_EXPECTED_ABIS:-arm64-v8a x86_64}"
expected_abis_value="${expected_abis_value//,/ }"
read -r -a expected_abis <<<"$expected_abis_value"
(( ${#expected_abis[@]} > 0 )) || fail 'ANYTTY_ANDROID_EXPECTED_ABIS must name at least one ABI'

for abi in "${expected_abis[@]}"; do
  [[ "$abi" =~ ^[A-Za-z0-9_.-]+$ ]] || fail "invalid expected ABI: $abi"
  for library in libanytty_client.so libanytty_client_jni.so; do
    native_path="lib/$abi/$library"
    if ! printf '%s\n' "$apk_entries" | rg -F -x "$native_path" >/dev/null; then
      fail "missing required native library: $native_path"
    fi
  done
done

native_libraries="$(printf '%s\n' "$apk_entries" | rg '^lib/[^/]+/[^/]+\.so$')"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/anytty-apk-boundary.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

while IFS= read -r native_path; do
  [[ -n "$native_path" ]] || continue
  native_file="$tmp_dir/native.so"
  native_strings="$tmp_dir/native.strings"
  if ! unzip -p "$app_apk" "$native_path" >"$native_file"; then
    fail "could not extract native library: $native_path"
  fi
  if ! strings "$native_file" >"$native_strings"; then
    fail "could not inspect native library: $native_path"
  fi
  if rg -F \
    -e 'ANYTTY_ANDROID_GO_TAGS' \
    -e 'anytty_android_spike' \
    -e 'createSpike' \
    -e 'android-spike-daemon' \
    -e 'android-managed-1' \
    -e 'anytty-go-client-%d' "$native_strings" >/dev/null; then
    fail "forbidden Android dev/spike marker found in $native_path"
  fi
done <<<"$native_libraries"

printf '%s\n' "Android APK boundary passed: $app_apk"
