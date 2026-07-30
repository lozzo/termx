#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
guard="$repo_root/scripts/repository-layout-guard.sh"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/anytty-layout-guard.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

expect_pass() {
  local label="$1"
  shift
  local output
  if ! output="$("$@" 2>&1)"; then
    echo "FAIL: $label should pass" >&2
    echo "$output" >&2
    exit 1
  fi
  echo "PASS: $label"
}

expect_fail() {
  local label="$1"
  local expected="$2"
  shift 2
  local output
  if output="$("$@" 2>&1)"; then
    echo "FAIL: $label should fail" >&2
    exit 1
  fi
  if ! grep -Fq "$expected" <<<"$output"; then
    echo "FAIL: $label reported an unexpected error" >&2
    echo "$output" >&2
    exit 1
  fi
  echo "PASS: $label"
}

create_fixture() {
  local name="$1"
  local fixture="$tmp_dir/$name"
  mkdir -p "$fixture/scripts"
  cp "$guard" "$fixture/scripts/repository-layout-guard.sh"
  chmod +x "$fixture/scripts/repository-layout-guard.sh"
  for document in \
    README.md CONTRIBUTING.md SECURITY.md CHANGELOG.md \
    ARCHITECTURE.md CONNECTION_ARCHITECTURE.md workflow.md; do
    : >"$fixture/$document"
  done
  printf '/.artifacts/\n*.apk\n*.aab\n' >"$fixture/.gitignore"
  printf '{}\n' >"$fixture/package-lock.json"
  printf 'module example.com/layout-fixture\n\ngo 1.26.5\n' >"$fixture/go.mod"
  printf '# fixture\n' >"$fixture/Makefile"
  git -C "$fixture" init -q
  git -C "$fixture" add .
  printf '%s\n' "$fixture"
}

create_android_ignore_fixture() {
  local name="${1:-android-ignore}"
  local fixture="$tmp_dir/$name"
  mkdir -p "$fixture/clients/mobile/android/app"
  cp "$repo_root/.gitignore" "$fixture/.gitignore"
  cp "$repo_root/clients/mobile/.gitignore" "$fixture/clients/mobile/.gitignore"
  cp "$repo_root/clients/mobile/android/.gitignore" "$fixture/clients/mobile/android/.gitignore"
  cp "$repo_root/clients/mobile/android/app/.gitignore" "$fixture/clients/mobile/android/app/.gitignore"
  git -C "$fixture" init -q
  printf '%s\n' "$fixture"
}

expect_ignored() {
  local fixture="$1"
  local path="$2"
  local output
  if ! output="$(git -C "$fixture" check-ignore --no-index -v "$path" 2>&1)"; then
    echo "FAIL: expected Android build path to be ignored: $path" >&2
    echo "$output" >&2
    exit 1
  fi
  echo "PASS: ignored $path"
}

expect_visible() {
  local fixture="$1"
  local path="$2"
  local output
  local status
  if output="$(git -C "$fixture" check-ignore --no-index -v "$path" 2>&1)"; then
    echo "FAIL: source or fixture path was unexpectedly ignored: $path" >&2
    echo "$output" >&2
    exit 1
  else
    status=$?
  fi
  if [[ "$status" -ne 1 ]]; then
    echo "FAIL: git check-ignore failed for $path" >&2
    echo "$output" >&2
    exit 1
  fi
  echo "PASS: visible $path"
}

expect_recursive_build_mutation_detected() {
  local label="$1"
  local ignore_file="$2"
  local recursive_rule="$3"
  local fixture
  local path="clients/mobile/android/app/src/test/resources/build/fixture.json"
  local output
  fixture="$(create_android_ignore_fixture "android-ignore-mutation-$label")"
  printf '%s\n' "$recursive_rule" >>"$fixture/$ignore_file"
  mkdir -p "$fixture/$(dirname "$path")"
  printf 'fixture\n' >"$fixture/$path"
  if ! output="$(git -C "$fixture" check-ignore --no-index -v "$path" 2>&1)"; then
    echo "FAIL: recursive build mutation was not detected in $ignore_file" >&2
    exit 1
  fi
  if ! grep -Fq -- "$recursive_rule" <<<"$output"; then
    echo "FAIL: $ignore_file mutation did not cause the visibility failure" >&2
    echo "$output" >&2
    exit 1
  fi
  echo "PASS: recursive build mutation detected in $ignore_file"
}

expect_pass "current repository" "$guard"

missing_document="$(create_fixture missing-document)"
rm -f "$missing_document/README.md"
expect_fail "missing required document" "required document is missing: README.md" \
  "$missing_document/scripts/repository-layout-guard.sh"

extra_markdown="$(create_fixture extra-markdown)"
printf '# Additional documentation\n' >"$extra_markdown/EXTRA.md"
git -C "$extra_markdown" add EXTRA.md
expect_pass "additional tracked Markdown" "$extra_markdown/scripts/repository-layout-guard.sh"

legacy_path="$(create_fixture legacy-path)"
mkdir -p "$legacy_path/termx-app"
expect_fail "legacy path" "legacy path still exists: termx-app" \
  "$legacy_path/scripts/repository-layout-guard.sh"

for artifact in app-debug.apk app-release.aab anytty anytty.exe fixture-library.dll; do
  artifact_fixture="$(create_fixture "root-${artifact//./-}")"
  printf 'fixture build output\n' >"$artifact_fixture/$artifact"
  expect_fail "root $artifact" "build output exists at repository root: $artifact" \
    "$artifact_fixture/scripts/repository-layout-guard.sh"
done

tracked_case=0
for tracked_output in \
  clients/mobile/android/app/build/outputs/apk/debug/app-debug.apk \
  clients/mobile/android/app/build/intermediates/state.bin \
  clients/mobile/android/.gradle/cache/state \
  clients/mobile/android/app/.cxx/state; do
  tracked_case=$((tracked_case + 1))
  tracked_android="$(create_fixture "tracked-android-output-$tracked_case")"
  mkdir -p "$tracked_android/$(dirname "$tracked_output")"
  printf 'fixture build output\n' >"$tracked_android/$tracked_output"
  git -C "$tracked_android" add -f "$tracked_output"
  expect_fail "tracked Android build output: $tracked_output" \
    "Android build output must not be tracked: $tracked_output" \
    "$tracked_android/scripts/repository-layout-guard.sh"
done

android_ignore="$(create_android_ignore_fixture)"
ignored_paths=(
  clients/mobile/android/build/state.bin
  clients/mobile/android/app/build/state.bin
  clients/mobile/android/.gradle/cache/state
  clients/mobile/android/app/.gradle/cache/state
  clients/mobile/android/.cxx/state
  clients/mobile/android/app/.cxx/state
  clients/mobile/android/app-debug.apk
  clients/mobile/android/app-release.aab
)
visible_paths=(
  clients/mobile/android/app/src/test/resources/build/fixture.json
  clients/mobile/android/app/src/main/resources/build/fixture.json
  clients/mobile/android/scripts/fixtures/build/fixture.json
)
for path in "${ignored_paths[@]}" "${visible_paths[@]}"; do
  mkdir -p "$android_ignore/$(dirname "$path")"
  printf 'fixture\n' >"$android_ignore/$path"
done
for path in "${ignored_paths[@]}"; do
  expect_ignored "$android_ignore" "$path"
done
for path in "${visible_paths[@]}"; do
  expect_visible "$android_ignore" "$path"
done
expect_recursive_build_mutation_detected root .gitignore '/clients/mobile/android/**/build/'
expect_recursive_build_mutation_detected mobile clients/mobile/.gitignore '/android/**/build/'
expect_recursive_build_mutation_detected android clients/mobile/android/.gitignore '**/build/'

echo "repository layout guard fixtures passed"
