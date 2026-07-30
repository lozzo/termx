#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
gate="$repo_root/scripts/verify-android-apk-boundary.sh"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/anytty-apk-boundary-test.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

fail() {
  printf '%s\n' "Android APK boundary test failed: $*" >&2
  exit 1
}

for tool in zip rg; do
  command -v "$tool" >/dev/null 2>&1 || fail "required test tool is unavailable: $tool"
done

add_library() {
  local root="$1"
  local abi="$2"
  local library="$3"
  local content="${4:-production-native}"
  mkdir -p "$root/lib/$abi"
  printf '%s\n' "$content" >"$root/lib/$abi/$library"
}

pack_apk() {
  local root="$1"
  local apk="$2"
  (cd "$root" && zip -q -r "$apk" lib)
}

add_complete_fixture() {
  local root="$1"
  for abi in arm64-v8a x86_64; do
    add_library "$root" "$abi" libanytty_client.so
    add_library "$root" "$abi" libanytty_client_jni.so
  done
}

expect_failure() {
  local label="$1"
  local expected="$2"
  shift 2
  local log="$tmp_dir/$label.log"
  if "$@" >"$log" 2>&1; then
    fail "$label unexpectedly passed"
  fi
  if ! rg -F -q "$expected" "$log"; then
    printf '%s\n' "unexpected $label output:" >&2
    sed -n '1,80p' "$log" >&2
    fail "$label did not report the expected failure"
  fi
}

complete_root="$tmp_dir/complete"
add_complete_fixture "$complete_root"
complete_apk="$tmp_dir/complete.apk"
pack_apk "$complete_root" "$complete_apk"
ANYTTY_ANDROID_EXPECTED_ABIS='arm64-v8a x86_64' "$gate" "$complete_apk"

missing_abi_root="$tmp_dir/missing-abi"
add_library "$missing_abi_root" arm64-v8a libanytty_client.so
add_library "$missing_abi_root" arm64-v8a libanytty_client_jni.so
missing_abi_apk="$tmp_dir/missing-abi.apk"
pack_apk "$missing_abi_root" "$missing_abi_apk"
expect_failure missing-abi 'missing required native library: lib/x86_64/libanytty_client.so' \
  env ANYTTY_ANDROID_EXPECTED_ABIS='arm64-v8a x86_64' "$gate" "$missing_abi_apk"
ANYTTY_ANDROID_EXPECTED_ABIS=arm64-v8a "$gate" "$missing_abi_apk"

missing_jni_root="$tmp_dir/missing-jni"
add_complete_fixture "$missing_jni_root"
rm "$missing_jni_root/lib/x86_64/libanytty_client_jni.so"
missing_jni_apk="$tmp_dir/missing-jni.apk"
pack_apk "$missing_jni_root" "$missing_jni_apk"
expect_failure missing-jni 'missing required native library: lib/x86_64/libanytty_client_jni.so' \
  env ANYTTY_ANDROID_EXPECTED_ABIS='arm64-v8a x86_64' "$gate" "$missing_jni_apk"

marker_root="$tmp_dir/marker"
add_complete_fixture "$marker_root"
printf '%s\n' 'android-spike-daemon' >>"$marker_root/lib/arm64-v8a/libanytty_client.so"
marker_apk="$tmp_dir/marker.apk"
pack_apk "$marker_root" "$marker_apk"
expect_failure marker 'forbidden Android dev/spike marker found in lib/arm64-v8a/libanytty_client.so' \
  env ANYTTY_ANDROID_EXPECTED_ABIS='arm64-v8a x86_64' "$gate" "$marker_apk"

empty_apk="$tmp_dir/empty.apk"
: >"$empty_apk"
expect_failure empty-apk 'APK is missing or empty' \
  env ANYTTY_ANDROID_EXPECTED_ABIS='arm64-v8a x86_64' "$gate" "$empty_apk"

printf '%s\n' 'Android APK boundary fixture tests passed'
