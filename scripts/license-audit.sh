#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

audit_scope="private-monorepo"
if [[ "${1:-}" == "--public-snapshot" ]]; then
  audit_scope="public-snapshot"
  shift
fi
if [[ $# -ne 0 ]]; then
  echo "usage: $0 [--public-snapshot]" >&2
  exit 2
fi

required_files=(
  LICENSE
  THIRD_PARTY_NOTICES.md
  docs/legal/public-snapshot/LICENSE
  docs/legal/public-snapshot/NOTICE
  docs/legal/public-snapshot/DCO
  docs/legal/public-snapshot/CONTRIBUTING.md
  docs/legal/public-snapshot/THIRD_PARTY_NOTICES.md
  docs/legal/public-snapshot/THIRD_PARTY_INVENTORY.md
  docs/legal/third-party-inventory.md
  termx-app/public/APACHE-2.0.txt
  termx-app/public/THIRD_PARTY_NOTICES.txt
)
if [[ "$audit_scope" == "private-monorepo" ]]; then
  required_files+=(
    docs/legal/licensing-and-distribution-review.md
    docs/legal/private-artifact-distribution-gates.md
  )
else
  required_files+=(
    NOTICE
    DCO
    CONTRIBUTING.md
  )
fi
for path in "${required_files[@]}"; do
  if [[ ! -s "$path" ]]; then
    echo "required legal release file is missing or empty: $path" >&2
    exit 1
  fi
done

if [[ "$audit_scope" == "public-snapshot" ]]; then
  scripts/public-snapshot-guard.sh
  public_template_pairs=(
    "LICENSE:docs/legal/public-snapshot/LICENSE"
    "NOTICE:docs/legal/public-snapshot/NOTICE"
    "DCO:docs/legal/public-snapshot/DCO"
    "CONTRIBUTING.md:docs/legal/public-snapshot/CONTRIBUTING.md"
    "THIRD_PARTY_NOTICES.md:docs/legal/public-snapshot/THIRD_PARTY_NOTICES.md"
    "docs/legal/third-party-inventory.md:docs/legal/public-snapshot/THIRD_PARTY_INVENTORY.md"
  )
  for pair in "${public_template_pairs[@]}"; do
    target="${pair%%:*}"
    template="${pair#*:}"
    if ! cmp -s "$target" "$template"; then
      echo "public snapshot legal file differs from its reviewed template: $target" >&2
      exit 1
    fi
  done
fi

scripts/fetch-pinned-third-party-notices.sh --check
go_notice_args=(--check)
if [[ "$audit_scope" == "public-snapshot" ]]; then
  go_notice_args+=(--public-only)
fi
GO_LICENSES_BIN="${GO_LICENSES_BIN:-}" scripts/generate-go-notices.sh "${go_notice_args[@]}"
node scripts/generate-npm-notices.mjs --check
scripts/generate-android-notices.sh --check

private_dependencies="$(GOWORK=off go list -deps -test ./... | grep '^github.com/lozzow/termx/private/' || true)"
if [[ -n "$private_dependencies" ]]; then
  echo "public root Go module imports private code:" >&2
  echo "$private_dependencies" >&2
  exit 1
fi

echo "TermX $audit_scope license audit passed"
