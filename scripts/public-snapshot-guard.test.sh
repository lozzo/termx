#!/usr/bin/env bash

set -euo pipefail

source_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_root="$(mktemp -d "${TMPDIR:-/tmp}/termx-public-guard-test.XXXXXX")"
trap 'rm -rf "$tmp_root"' EXIT
snapshot="$tmp_root/snapshot"

mkdir -p \
  "$snapshot/docs/development" \
  "$snapshot/docs/history" \
  "$snapshot/docs/legal/public-snapshot" \
  "$snapshot/docs/legal/third-party" \
  "$snapshot/docs/remote-platform" \
  "$snapshot/fixtures" \
  "$snapshot/internal" \
  "$snapshot/clients/ui" \
  "$snapshot/clients/mobile" \
  "$snapshot/scripts" \
  "$snapshot/cmd/termx" \
  "$snapshot/core" \
  "$snapshot/proto" \
  "$snapshot/remote" \
  "$snapshot/shared" \
  "$snapshot/testkit" \
  "$snapshot/tui" \
  "$snapshot/vterm"

for file in .gitignore CONTRIBUTING.md DCO LICENSE Makefile NOTICE README.md THIRD_PARTY_NOTICES.md go.sum go.work.sum package-lock.json package.json; do
  printf '%s\n' "fixture" >"$snapshot/$file"
done
cat >"$snapshot/go.mod" <<'EOF'
module github.com/lozzow/termx

go 1.26.0
EOF
for file in LICENSE NOTICE DCO CONTRIBUTING.md THIRD_PARTY_NOTICES.md THIRD_PARTY_INVENTORY.md; do
  printf '%s\n' "fixture" >"$snapshot/docs/legal/public-snapshot/$file"
done
printf '%s\n' "fixture" >"$snapshot/docs/legal/third-party-inventory.md"
printf '%s\n' "fixture" >"$snapshot/docs/legal/third-party/LICENSE.txt"
printf '%s\n' "fixture" >"$snapshot/docs/development/README.md"
printf '%s\n' "fixture" >"$snapshot/docs/history/README.md"
printf '%s\n' "fixture" >"$snapshot/docs/remote-platform/README.md"
printf '%s\n' 'VITE_CONTROL_URL=https://control.example.test' \
  >"$snapshot/clients/ui/.env.example"

cp "$source_root/scripts/public-snapshot-guard.sh" "$snapshot/scripts/public-snapshot-guard.sh"
for script in \
  android-resolved-dependencies.init.gradle \
  check-generated-code.sh \
  client-workspace-guard.mjs \
  doctor.sh \
  fetch-pinned-third-party-notices.sh \
  generate-android-notices.sh \
  generate-go-notices.sh \
  generate-npm-notices.mjs \
  license-audit.sh \
  public-snapshot-guard.test.sh \
  repository-layout-guard.sh \
  verify-android-apk-boundary.sh \
  with-clean-termx-env.sh; do
  printf '%s\n' "fixture" >"$snapshot/scripts/$script"
done
chmod +x "$snapshot/scripts/public-snapshot-guard.sh"

cat >"$snapshot/go.work" <<'EOF'
go 1.26.0

use .
EOF

guard="$snapshot/scripts/public-snapshot-guard.sh"
"$guard" >/dev/null

expect_rejected() {
  local expected="$1"
  if "$guard" >"$tmp_root/output" 2>&1; then
    echo "guard unexpectedly accepted fixture: $expected" >&2
    exit 1
  fi
  if ! grep -q "$expected" "$tmp_root/output"; then
    echo "guard rejected fixture for the wrong reason; expected: $expected" >&2
    cat "$tmp_root/output" >&2
    exit 1
  fi
}

mkdir "$snapshot/private"
expect_rejected "unexpected top-level entry: private"
rmdir "$snapshot/private"

printf '%s\n' "fixture" >"$snapshot/cmd/termx/AGENTS.md"
expect_rejected "agent instructions are present"
rm "$snapshot/cmd/termx/AGENTS.md"

printf '%s\n' "TOKEN=secret" >"$snapshot/clients/ui/.env"
expect_rejected "secret-like file"
rm "$snapshot/clients/ui/.env"

printf '%s\n%s\n%s\n' \
  '-----BEGIN PRIVATE KEY-----' \
  'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA' \
  '-----END PRIVATE KEY-----' >"$snapshot/fixtures/private-material.txt"
expect_rejected "private key material found"
rm "$snapshot/fixtures/private-material.txt"

outside="$tmp_root/outside"
printf '%s\n' "fixture" >"$outside"
ln -s "$outside" "$snapshot/fixtures/outside-link"
expect_rejected "symlink escapes"
rm "$snapshot/fixtures/outside-link"

printf '%s\n' "fixture" >"$snapshot/internal-only.txt"
expect_rejected "unexpected top-level entry: internal-only.txt"
rm "$snapshot/internal-only.txt"

cp "$snapshot/go.work" "$tmp_root/go.work"
printf '%s\n' 'go 1.26.0' >"$snapshot/go.work"
expect_rejected "public go.work modules differ"
cp "$tmp_root/go.work" "$snapshot/go.work"

mkdir -p "$snapshot/clients/ui/node_modules/example"
printf '%s%s\n' 'g' 'hp_abcdefghijklmnopqrstuvwxyz1234567890' \
  >"$snapshot/clients/ui/node_modules/example/generated.js"
"$guard" >/dev/null

printf '%s\n' 'TERMX_LOCAL_WEB_ORIGIN=http://127.0.0.1:18888' \
  >"$snapshot/clients/ui/.env.example"
expect_rejected "archived localweb path"
printf '%s\n' 'VITE_CONTROL_URL=https://control.example.test' \
  >"$snapshot/clients/ui/.env.example"

echo "public snapshot guard harness passed"
