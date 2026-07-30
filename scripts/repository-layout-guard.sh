#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$repo_root"

fail() {
  echo "repository layout guard failed: $*" >&2
  exit 1
}

for path in \
  termx-app remote-ui termx-core termx-core-v2 termx-tui-v3 termx-remote termx-remote-v2 \
  tuiv2 termx-hub web-control private private/termx-cloud proto/cloudpb shared/cloudcompanion \
  client/adapter/managed; do
  [[ ! -e "$path" ]] || fail "legacy path still exists: $path"
done

required_documents=(
  README.md
  CONTRIBUTING.md
  SECURITY.md
  CHANGELOG.md
  ARCHITECTURE.md
  CONNECTION_ARCHITECTURE.md
  workflow.md
)
for path in "${required_documents[@]}"; do
  [[ -f "$path" ]] || fail "required document is missing: $path"
done

for path in bin .build; do
  [[ ! -e "$path" ]] || fail "legacy build output still exists: $path (run make clean)"
done
root_build_output="$(find . -mindepth 1 -maxdepth 1 -type f \( \
  -name '*.apk' -o -name '*.aab' -o -name '*.test' -o -name '*.cover' -o -name 'cover.out' \
  -o -name 'anytty' -o -name 'anytty.exe' -o -name 'muxvia' -o -name 'muxvia.exe' \
  -o -name 'anytty-cloud-controller' -o -name 'anytty-cloud-controller.exe' \
  -o -name 'anytty-cloud-edge' -o -name 'anytty-cloud-edge.exe' \
  -o -name '*.bin' -o -name '*.dll' -o -name '*.dylib' -o -name '*.so' -o -name '*.exe' \
  \) -print -quit)"
if [[ -n "$root_build_output" ]]; then
  fail "build output exists at repository root: ${root_build_output#./} (use .artifacts or make clean)"
fi
if [[ -d clients/mobile/native/android ]]; then
  fail "duplicate Android source mirror exists: clients/mobile/native/android"
fi
if ! rg -Fxq '/.artifacts/' .gitignore; then
  fail ".artifacts must be the ignored repository artifact root"
fi
if [[ -n "$(git ls-files .artifacts)" ]]; then
  fail ".artifacts must not contain tracked files"
fi
tracked_android_outputs="$({
  git ls-files -- '*.apk' '*.aab'
  git ls-files clients/mobile/android | rg '(^|/)(build|\.gradle|\.cxx)/' || true
} | LC_ALL=C sort -u)"
if [[ -n "$tracked_android_outputs" ]]; then
  fail "Android build output must not be tracked: $tracked_android_outputs"
fi

lockfiles=()
while IFS= read -r path; do
  lockfiles+=("$path")
done < <(find . \
  \( -type d \( -name node_modules -o -name build -o -name dist -o -name .gradle \) -prune \) \
  -o \( -type f -name package-lock.json -print \) | LC_ALL=C sort)
if [[ "${lockfiles[*]}" != "./package-lock.json" ]]; then
  fail "npm lockfiles differ: ${lockfiles[*]:-none}"
fi

expected_modules=(go.mod)
actual_modules=()
while IFS= read -r path; do
  actual_modules+=("$path")
done < <(find . \
  \( -type d \( -name node_modules -o -name build -o -name dist -o -name .gradle \) -prune \) \
  -o \( -type f -name go.mod -print \) | sed 's#^\./##' | LC_ALL=C sort)
expected_modules_sorted=()
while IFS= read -r path; do
  expected_modules_sorted+=("$path")
done < <(printf '%s\n' "${expected_modules[@]}" | LC_ALL=C sort)
if [[ "${actual_modules[*]}" != "${expected_modules_sorted[*]}" ]]; then
  fail "Go module roots differ: ${actual_modules[*]}"
fi

scan_candidates=(
  .gitignore README.md CONTRIBUTING.md SECURITY.md CHANGELOG.md ARCHITECTURE.md
  CONNECTION_ARCHITECTURE.md workflow.md Makefile THIRD_PARTY_NOTICES.txt go.mod go.work package.json
  clients cmd core docs internal proto remote scripts shared testkit tui vterm
)
scan_paths=()
for path in "${scan_candidates[@]}"; do
  [[ -e "$path" ]] && scan_paths+=("$path")
done
old_path_matches="$(rg -n --hidden 'private/termx-cloud' "${scan_paths[@]}" \
  --glob '!**/node_modules/**' \
  --glob '!scripts/repository-layout-guard.sh' \
  --glob '!**/dist/**' --glob '!**/build/**' --glob '!**/.gradle/**' || true)"
[[ -z "$old_path_matches" ]] || fail "active files reference private/termx-cloud:\n$old_path_matches"

old_import_matches="$(rg -n --hidden \
  'github\.com/lozzow/termx/(termx-core|termx-core-v2|termx-tui-v3|termx-remote|termx-remote-v2|tuiv2)(/|\"|$)' \
  --glob '*.go' --glob '!**/*_test.go' --glob 'go.mod' --glob 'go.work' \
  --glob '!**/node_modules/**' . || true)"
[[ -z "$old_import_matches" ]] || fail "production files reference a legacy module path:\n$old_import_matches"

if rg -n '^(termx-build|test-core|test-tui|test-repository|test-cli-v3[^:]*):' Makefile >/dev/null; then
  fail "Makefile still exposes a legacy build or test target"
fi

echo "repository layout passed"
