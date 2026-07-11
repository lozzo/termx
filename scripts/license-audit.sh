#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

required_files=(
  LICENSE
  THIRD_PARTY_NOTICES.md
  docs/legal/licensing-and-distribution-review.md
  docs/legal/private-artifact-distribution-gates.md
  docs/legal/public-snapshot/LICENSE
  docs/legal/public-snapshot/NOTICE
  docs/legal/public-snapshot/DCO
  docs/legal/public-snapshot/CONTRIBUTING.md
  docs/legal/public-snapshot/THIRD_PARTY_NOTICES.md
  termx-app/public/APACHE-2.0.txt
  termx-app/public/THIRD_PARTY_NOTICES.txt
)
for path in "${required_files[@]}"; do
  if [[ ! -s "$path" ]]; then
    echo "required legal release file is missing or empty: $path" >&2
    exit 1
  fi
done

scripts/fetch-pinned-third-party-notices.sh --check
GO_LICENSES_BIN="${GO_LICENSES_BIN:-}" scripts/generate-go-notices.sh --check
node scripts/generate-npm-notices.mjs --check
scripts/generate-android-notices.sh --check

public_go_modules=(
  internal
  termx-cli
  termx-core-v2
  termx-proto
  termx-remote-v2
  termx-shared
  termx-testkit
  termx-tui-v3
  termx-vterm
)
for module in "${public_go_modules[@]}"; do
  private_dependencies="$(cd "$repo_root/$module" && go list -deps -test ./... | grep '^github.com/lozzow/termx/private/' || true)"
  if [[ -n "$private_dependencies" ]]; then
    echo "public Go module $module imports private code:" >&2
    echo "$private_dependencies" >&2
    exit 1
  fi
done

echo "TermX license audit passed"
