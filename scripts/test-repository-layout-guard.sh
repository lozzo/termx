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

expect_pass "current repository" "$guard"

missing_document="$(create_fixture missing-document)"
rm -f "$missing_document/README.md"
expect_fail "missing required document" "required document is missing: README.md" \
  "$missing_document/scripts/repository-layout-guard.sh"

extra_markdown="$(create_fixture extra-markdown)"
printf '# Additional documentation\n' >"$extra_markdown/EXTRA.md"
git -C "$extra_markdown" add EXTRA.md
expect_pass "additional tracked Markdown" "$extra_markdown/scripts/repository-layout-guard.sh"

for artifact in app-debug.apk app-release.aab anytty; do
  artifact_fixture="$(create_fixture "root-${artifact//./-}")"
  printf 'fixture build output\n' >"$artifact_fixture/$artifact"
  expect_fail "root $artifact" "build output exists at repository root: $artifact" \
    "$artifact_fixture/scripts/repository-layout-guard.sh"
done

tracked_android="$(create_fixture tracked-android-output)"
tracked_apk="clients/mobile/android/app/build/outputs/apk/debug/app-debug.apk"
mkdir -p "$tracked_android/$(dirname "$tracked_apk")"
printf 'fixture build output\n' >"$tracked_android/$tracked_apk"
git -C "$tracked_android" add -f "$tracked_apk"
expect_fail "tracked Android build output" "Android build output must not be tracked" \
  "$tracked_android/scripts/repository-layout-guard.sh"

echo "repository layout guard fixtures passed"
