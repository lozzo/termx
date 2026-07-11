# TermX Public Snapshot Manifest

Status: RP007 release procedure

Date: 2026-07-11

## 1. Purpose

This manifest defines the one-time, reviewable copy from the private authoritative monorepo into a new public Git repository. It is not a mirror, exporter, synchronization service, or second development workflow.

The snapshot source must be one committed revision that already passed the private monorepo gates. The destination must not exist before copying, and the source `.git/` directory is never copied.

## 2. Public Path Allowlist

Copy these root files from the selected commit:

```text
.gitignore
Makefile
README.md
go.mod
go.sum
```

Copy these complete public source directories:

```text
fixtures/
internal/
remote-ui/
termx-app/
cmd/
core/
proto/
remote/
shared/
testkit/
tui/
vterm/
```

Copy only these documentation and release-support paths:

```text
docs/legal/public-snapshot/
docs/legal/third-party/
docs/remote-platform/
scripts/android-resolved-dependencies.init.gradle
scripts/fetch-pinned-third-party-notices.sh
scripts/generate-android-notices.sh
scripts/generate-go-notices.sh
scripts/generate-npm-notices.mjs
scripts/license-audit.sh
scripts/public-snapshot-guard.sh
scripts/public-snapshot-guard.test.sh
```

Do not copy root `LICENSE`, root `THIRD_PARTY_NOTICES.md`, `go.work`, `go.work.sum`, `workflow.md`, any `AGENTS.md`, `private/`, legacy top-level remote directories, ignored build outputs, local configuration, or the source repository history. The reviewed public templates replace the root legal and workspace files. `go work sync` may create a new public-only `go.work.sum`; its absence is valid when the root module sum already closes the public dependency graph.

## 3. Manual Copy Procedure

Run this procedure from a shell with Git, Go, Node.js, npm, Android/Java tooling, `protoc`, ripgrep, and Perl available. npm installs the pinned `protoc-gen-es` plugin. `ANDROID_HOME` must point to a valid Android SDK; do not copy a developer's ignored `android/local.properties`. `SOURCE_COMMIT` must resolve to an immutable commit, not a dirty working tree.

```bash
set -euo pipefail

SOURCE_REPO=/absolute/path/to/private/termx
SOURCE_COMMIT=$(git -C "$SOURCE_REPO" rev-parse --verify '<release-commit>^{commit}')
DEST=/absolute/path/to/new-public-termx
: "${ANDROID_HOME:?set ANDROID_HOME to the Android SDK root}"
test -d "$ANDROID_HOME"

test ! -e "$DEST"
mkdir -m 0755 "$DEST"

PUBLIC_PATHS=(
  .gitignore
  Makefile
  README.md
  go.mod
  go.sum
  fixtures
  internal
  remote-ui
  termx-app
  cmd
  core
  proto
  remote
  shared
  testkit
  tui
  vterm
  docs/legal/public-snapshot
  docs/legal/third-party
  docs/remote-platform
  scripts/android-resolved-dependencies.init.gradle
  scripts/fetch-pinned-third-party-notices.sh
  scripts/generate-android-notices.sh
  scripts/generate-go-notices.sh
  scripts/generate-npm-notices.mjs
  scripts/license-audit.sh
  scripts/public-snapshot-guard.sh
  scripts/public-snapshot-guard.test.sh
)

git -C "$SOURCE_REPO" archive "$SOURCE_COMMIT" -- "${PUBLIC_PATHS[@]}" \
  | tar -x -C "$DEST"

find "$DEST" -name AGENTS.md -type f -delete

cp "$DEST/docs/legal/public-snapshot/LICENSE" "$DEST/LICENSE"
cp "$DEST/docs/legal/public-snapshot/NOTICE" "$DEST/NOTICE"
cp "$DEST/docs/legal/public-snapshot/DCO" "$DEST/DCO"
cp "$DEST/docs/legal/public-snapshot/CONTRIBUTING.md" "$DEST/CONTRIBUTING.md"
cp "$DEST/docs/legal/public-snapshot/THIRD_PARTY_NOTICES.md" "$DEST/THIRD_PARTY_NOTICES.md"
cp "$DEST/docs/legal/public-snapshot/THIRD_PARTY_INVENTORY.md" \
  "$DEST/docs/legal/third-party-inventory.md"
cp "$DEST/docs/remote-platform/public-snapshot/go.work" "$DEST/go.work"
rm -f "$DEST/go.work.sum"

(
  cd "$DEST"
  go work sync
  scripts/public-snapshot-guard.test.sh
  scripts/public-snapshot-guard.sh
)
```

The destination remains outside Git until every gate below passes. This makes accidental source-history reuse structurally impossible during review.

## 4. Independent Build And Test Gates

Install exact npm dependencies before license generation:

```bash
(cd "$DEST/remote-ui" && npm ci)
(cd "$DEST/termx-app" && npm ci)
(cd "$DEST/remote-ui" && npm audit --omit=dev)
(cd "$DEST/termx-app" && npm audit --omit=dev)
```

Run all public Go module tests, the CLI build, shared UI tests/build, Community App build, Android Community tests, and release audit:

```bash
(cd "$DEST" && GOWORK=off go test ./... -count=1 && GOWORK=off go build ./cmd/termx)
(cd "$DEST/remote-ui" && npm run proto && npm test && npm run typecheck && npm run build)
(cd "$DEST/termx-app" && npm run cap:build)
(cd "$DEST/termx-app/android" && ./gradlew testDebugUnitTest assembleDebug)
(cd "$DEST" && scripts/license-audit.sh --public-snapshot)
```

Before a production release, also run `npm audit` in both npm projects, resolve or formally review every remaining development-tool advisory, generate an SBOM from final binaries/APKs with the release team's pinned SBOM tool, and run the organization-approved secret scanner. RP007 production dependency audits are clean after updating `tar` to `7.5.19`; current Vite/Babel development-tool advisories remain a release blocker outside this repository-boundary slice. `public-snapshot-guard.sh` is the repository-local fail-closed baseline; it does not claim to replace an independently maintained scanner.

## 5. New Public History

Only after all gates pass:

```bash
cd "$DEST"
git init --initial-branch=main
git add --all
git commit -s -m 'Initial public TermX snapshot'
```

Review the first commit tree and generated SBOM before adding a public remote. Never add the private repository as a remote and never graft, filter, or merge its history into the public repository.
