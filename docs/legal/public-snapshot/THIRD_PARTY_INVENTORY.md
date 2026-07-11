# Third-Party Dependency And Asset Inventory

Status: RP007 public snapshot release baseline

Date: 2026-07-11

This inventory describes the public source snapshot and its Community artifacts. Generated artifact notices remain the distributable source of complete license text; this document does not replace upstream terms.

## Go CLI

`scripts/generate-go-notices.sh --public-only` uses pinned `google/go-licenses v2.0.1` against the real `termx-cli/cmd/termx` binary graph for `darwin/arm64`, `linux/amd64`, and `windows/amd64`.

The reviewed LIC001 graph contains 54 external or vendored license mappings under MIT, BSD-3-Clause, or Apache-2.0. The generated notice is committed at `termx-cli/cmd/termx/THIRD_PARTY_NOTICES.txt` and is printed by `termx licenses`.

`termx-vterm/internal/vt` is vendored MIT material whose upstream license remains adjacent to the source. Assembly, native, and WebAssembly files still require provenance review even when their Go module has an approved license.

## npm And Web Bundles

`scripts/generate-npm-notices.mjs` scans production entries in `termx-app/package-lock.json` and `remote-ui/package-lock.json`, excluding first-party `@termx/*` packages. The LIC001 baseline contains 117 exact package/version entries and 72 deduplicated license texts.

Reviewed expressions are MIT, ISC, BlueOak-1.0.0, Apache-2.0, 0BSD, BSD-3-Clause, Unlicense, and the combined Apache-2.0/BSD-3-Clause expression used by `@bufbuild/protobuf`. Pinned upstream overrides are stored under `docs/legal/third-party/npm/`.

The generated bundle is committed at `termx-app/public/third-party/NPM_NOTICES.txt`.

## Android Community Runtime

`scripts/generate-android-notices.sh` resolves the Community App `releaseRuntimeClasspath` instead of trusting requested Gradle versions. The LIC001 baseline contains 55 Maven components.

AndroidX, Kotlin/Kotlinx, JetBrains annotations, Cordova, Gson, Guava listenablefuture, and JSpecify are Apache-2.0. `Java-WebSocket:1.5.6` and `slf4j-api:2.0.6` are MIT. Their pinned texts are stored under `docs/legal/third-party/android/`.

The generated bundle is committed at `termx-app/public/third-party/ANDROID_NOTICES.txt`.

## WebRTC And Fonts

The Android WebRTC bundle is pinned to `io.github.webrtc-sdk:android:125.6422.07`, repository tag `v125.6422.07`, and commit `878c5b093f8bbbd4955d1037316484aabe962d18`. The App distributes both `WEBRTC_SDK_WRAPPER_LICENSE.txt` and the complete upstream `WEBRTC.md` notice bundle.

The App includes ten Nerd Font Mono WOFF2 assets. `remote-ui/src/assets/fonts/LICENSE` is the reviewed attribution truth and is copied to `termx-app/public/third-party/FONTS.txt` by the pinned notice script.

## Audit

Run these commands from the copied public snapshot after `npm ci` in both npm projects:

```bash
scripts/public-snapshot-guard.test.sh
scripts/public-snapshot-guard.sh
scripts/license-audit.sh --public-snapshot
```

Any new dependency, version, license expression, Maven group, native bundle, font, or missing license text is a release blocker until reviewed and regenerated.
