#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$repo_root"

fail() {
  echo "public snapshot guard failed: $*" >&2
  exit 1
}

required_top_level=(
  .gitignore
  CONTRIBUTING.md
  DCO
  LICENSE
  Makefile
  NOTICE
  README.md
  THIRD_PARTY_NOTICES.md
  clients
  docs
  fixtures
  go.mod
  go.sum
  go.work
  internal
  package-lock.json
  package.json
  scripts
  cmd
  core
  proto
  remote
  shared
  testkit
  tui
  vterm
)
for path in "${required_top_level[@]}"; do
  [[ -e "$path" ]] || fail "required top-level entry is missing: $path"
done

while IFS= read -r path; do
  name="${path#./}"
  case "$name" in
    .gitignore|CONTRIBUTING.md|DCO|LICENSE|Makefile|NOTICE|README.md|THIRD_PARTY_NOTICES.md|clients|cmd|core|docs|fixtures|go.mod|go.sum|go.work|go.work.sum|internal|package-lock.json|package.json|proto|remote|scripts|shared|testkit|tui|vterm)
      ;;
    *)
      fail "unexpected top-level entry: $name"
      ;;
  esac
done < <(find . -mindepth 1 -maxdepth 1 -print | LC_ALL=C sort)

while IFS= read -r path; do
  name="${path#clients/}"
  case "$name" in
    mobile|ui)
      ;;
    *)
      fail "unexpected public client entry: clients/$name"
      ;;
  esac
done < <(find clients -mindepth 1 -maxdepth 1 -print | LC_ALL=C sort)

while IFS= read -r path; do
  name="${path#docs/}"
  case "$name" in
    development|history|legal|remote-platform)
      ;;
    *)
      fail "unexpected public docs entry: docs/$name"
      ;;
  esac
done < <(find docs -mindepth 1 -maxdepth 1 -print | LC_ALL=C sort)
[[ -s docs/development/README.md ]] || fail "public development guide is missing"
[[ -d docs/history ]] || fail "public history directory is missing"

while IFS= read -r path; do
  name="${path#docs/legal/}"
  case "$name" in
    public-snapshot|third-party|third-party-inventory.md)
      ;;
    *)
      fail "unexpected public legal entry: docs/legal/$name"
      ;;
  esac
done < <(find docs/legal -mindepth 1 -maxdepth 1 -print | LC_ALL=C sort)

required_scripts=(
  android-resolved-dependencies.init.gradle
  check-generated-code.sh
  client-workspace-guard.mjs
  doctor.sh
  fetch-pinned-third-party-notices.sh
  generate-android-notices.sh
  generate-go-notices.sh
  generate-npm-notices.mjs
  license-audit.sh
  public-snapshot-guard.sh
  public-snapshot-guard.test.sh
  repository-layout-guard.sh
  verify-android-apk-boundary.sh
  with-clean-muxvia-env.sh
)
for script in "${required_scripts[@]}"; do
  [[ -s "scripts/$script" ]] || fail "required public release script is missing: scripts/$script"
done
while IFS= read -r path; do
  name="${path#scripts/}"
  case "$name" in
    android-resolved-dependencies.init.gradle|check-generated-code.sh|client-workspace-guard.mjs|doctor.sh|fetch-pinned-third-party-notices.sh|generate-android-notices.sh|generate-go-notices.sh|generate-npm-notices.mjs|license-audit.sh|public-snapshot-guard.sh|public-snapshot-guard.test.sh|repository-layout-guard.sh|verify-android-apk-boundary.sh|with-clean-muxvia-env.sh)
      ;;
    *)
      fail "unexpected public release script: scripts/$name"
      ;;
  esac
done < <(find scripts -mindepth 1 -maxdepth 1 -print | LC_ALL=C sort)

for forbidden in .git AGENTS.md workflow.md private termx-remote web-control; do
  [[ ! -e "$forbidden" ]] || fail "private or repository-internal entry is present: $forbidden"
done
agent_file="$(find . \
  \( -type d \( -name node_modules -o -name dist -o -name build -o -name .gradle \) -prune \) \
  -o \( -type f -name AGENTS.md -print -quit \))"
[[ -z "$agent_file" ]] || fail "repository-internal agent instructions are present: ${agent_file#./}"

while IFS= read -r path; do
  case "$path" in
    */.env.example)
      ;;
    *)
      fail "secret-like file must not enter the public snapshot: ${path#./}"
      ;;
  esac
done < <(find . \
  \( -type d \( -name node_modules -o -name dist -o -name build -o -name .gradle \) -prune \) \
  -o \( -type f \( \
    -name '.env' -o -name '.env.*' -o -name '*.pem' -o -name '*.key' \
    -o -name '*.p12' -o -name '*.pfx' -o -name 'id_rsa*' \
    -o -name 'credentials*' -o -name '.npmrc' -o -name '.pypirc' \
  \) -print \) | LC_ALL=C sort)

token_pattern='A[K]IA[0-9A-Z]{16}|A[S]IA[0-9A-Z]{16}|g[h]p_[A-Za-z0-9]{30,}|github_''pat_[A-Za-z0-9_]{40,}'
rg_excludes=(
  --glob '!**/node_modules/**'
  --glob '!**/dist/**'
  --glob '!**/build/**'
  --glob '!**/.gradle/**'
)
token_matches="$(rg -n --hidden --pcre2 "${rg_excludes[@]}" "$token_pattern" . || true)"
[[ -z "$token_matches" ]] || fail "credential-shaped token found:\n$token_matches"

pem_pattern='-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----\r?\n[A-Za-z0-9+/=]{32,}'
pem_matches="$(rg -n -l -U --hidden --pcre2 "${rg_excludes[@]}" -- "$pem_pattern" . || true)"
[[ -z "$pem_matches" ]] || fail "private key material found:\n$pem_matches"

while IFS= read -r -d '' link; do
  resolved="$(perl -MCwd=abs_path -e '$path = abs_path($ARGV[0]); defined $path or exit 1; print $path' "$link")" \
    || fail "broken symlink is not allowed: ${link#./}"
  case "$resolved" in
    "$repo_root"|"$repo_root"/*)
      ;;
    *)
      fail "symlink escapes the public snapshot: ${link#./} -> $resolved"
      ;;
  esac
done < <(find . \
  \( -type d \( -name node_modules -o -name dist -o -name build -o -name .gradle \) -prune \) \
  -o \( -type l -print0 \))

WORKSPACE_JSON="$(go work edit -json)" node <<'NODE'
const workspace = JSON.parse(process.env.WORKSPACE_JSON)
const expected = ['.']
const actual = (workspace.Use ?? []).map((entry) => entry.DiskPath).sort()
if (JSON.stringify(actual) !== JSON.stringify(expected)) {
  console.error(`public go.work modules differ: ${JSON.stringify(actual)}`)
  process.exit(1)
}
if ((workspace.Replace ?? []).length !== 0) {
  console.error('public go.work must not contain replace directives')
  process.exit(1)
}
NODE

if rg -n --hidden "${rg_excludes[@]}" --glob 'package.json' --glob 'package-lock.json' --glob '*.gradle' --glob '*.gradle.kts' \
  '(file:[^"[:space:]]*private/|\.\./private/)' package.json package-lock.json clients >/dev/null; then
  fail "App or shared UI build metadata references private source"
fi
[[ -s clients/ui/.env.example ]] || fail "shared UI public environment template is missing"
if rg -n '(MUXVIA_LOCAL_WEB_ORIGIN|localweb)' clients/ui/.env.example >/dev/null; then
  fail "shared UI environment template references the archived localweb path"
fi

echo "Muxvia public snapshot structure passed"
