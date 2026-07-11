# 第三方依赖与资产清单

状态：LIC001 审计基线

日期：2026-07-11

本清单记录当前 release entrypoint 的自动扫描结果、人工补充项和审计局限。生成文件才是随 artifact 分发的完整文本，本文件不替代任何上游许可证。

## 1. Go Artifacts

`scripts/generate-go-notices.sh` 使用固定 `google/go-licenses v2.0.1`，分别以 `darwin/arm64`、`linux/amd64`、`windows/amd64` 解析真实 binary package。

| Artifact | 入口 | 审计结果 | Bundle |
| --- | --- | --- | --- |
| public `termx` | `termx-cli/cmd/termx` | 54 条外部或 vendored license mapping；仅 MIT、BSD-3-Clause、Apache-2.0 | `termx-cli/cmd/termx/THIRD_PARTY_NOTICES.txt` |
| private `termx-cloud` | `private/termx-cloud/companion/cmd/termx-cloud` | 8 条外部 mapping；MIT、BSD-2-Clause、BSD-3-Clause | `private/termx-cloud/companion/cmd/termx-cloud/THIRD_PARTY_NOTICES.txt` |

Windows graph 额外包含 `go-winio`/`wincred`，Linux Companion graph 额外包含 `godbus/dbus`。`termx-vterm/internal/vt` 是仓库内 vendored MIT material，生成器保留其原始 license。当前 monorepo 的 first-party package 因根许可证不是公开 Apache-2.0 而被 `go-licenses` 标为 unknown；生成器只允许这个已知 first-party 状态，任何外部 unknown 或未批准 license 均失败。

`go-licenses` 明确不能继续分析部分 assembly/non-Go 文件；当前警告来自 `x/sys`、`x/net` 与 `klauspost/compress`。它们已有 module license，但 release review 仍必须把 assembly/native/wasm 作为人工 provenance 项。

## 2. npm/Web Bundle

`scripts/generate-npm-notices.mjs` 扫描 `termx-app/package-lock.json` 与 `remote-ui/package-lock.json` 的非 dev 条目，排除 first-party `@termx/*`，当前得到 117 个精确 package/version 和 72 份去重后的完整文本。

| License expression | Package count |
| --- | ---: |
| MIT | 88 |
| ISC | 12 |
| BlueOak-1.0.0 | 10 |
| Apache-2.0 | 3 |
| 0BSD | 1 |
| BSD-3-Clause | 1 |
| Unlicense | 1 |
| Apache-2.0 AND BSD-3-Clause | 1 |

当前没有 noncommercial、source-available、restricted 或 copyleft production entry。`bplist-parser@0.3.2` 只在 README 中提供完整 MIT 文本；`@bufbuild/protobuf@2.12.0` 的 npm tarball 未带根 Apache license，并且 `varint` implementation 另有 Google BSD-3-Clause header。两项均按上游精确 commit 和 SHA-256 固定在 `docs/legal/third-party/npm/`。

lockfile production 标记是保守的软件供应链范围，不等价于“全部代码都进入最终 Vite chunk”。发布时仍需用最终 bundle/SBOM 交叉确认，不能为了缩小 notice 而删除已随包或已链接的条款。

## 3. Android Runtime

`scripts/generate-android-notices.sh` 从 Gradle `releaseRuntimeClasspath` 读取 55 个 resolved Maven component，包含版本替换后的真实坐标，而不是只读取 `build.gradle` 的 requested version。

- AndroidX、Kotlin/Kotlinx、JetBrains annotations、Cordova、Gson、Guava listenablefuture 与 JSpecify：Apache-2.0。
- `Java-WebSocket:1.5.6` 与 `slf4j-api:2.0.6`：MIT，完整固定文本在 `docs/legal/third-party/android/`。
- Capacitor project modules：由 npm bundle 中对应精确 package/version 覆盖。
- `io.github.webrtc-sdk:android:125.6422.07`：单独按下面 native 规则处理。

任何新 Maven group 会让生成器失败；WebRTC、Java-WebSocket 或 SLF4J 的固定版本变化也会失败，必须先重新做来源和文本审查。

## 4. WebRTC Native Bundle

Android AAR `io.github.webrtc-sdk:android:125.6422.07` 本身没有内置 LICENSE/NOTICE，只包含 manifest、classes 和 native `.so`。其 Maven POM 声明 BSD-3-Clause，而 wrapper repository 根文件是 MIT；不能只信任 POM 的单一名称。

当前固定来源：

- repository tag：`v125.6422.07`
- commit：`878c5b093f8bbbd4955d1037316484aabe962d18`
- wrapper license SHA-256：`e6b282fe6c0fb353928923470457f31b44cbab203effd60c0cde4a5bb96c8aec`
- upstream raw `Licenses/WEBRTC.md` SHA-256：`d1f9382c6878ac024155fd6d44a5977329108bb8b0a01cea40e4a2f1d7de252e`（788,358 bytes，15,162 lines）
- deterministic distributed copy SHA-256：`63f7559e1510602581888b0b49231e9c626ec09505cff11ee9f7ea07e4f881ab`（只规范化 CRLF、行尾空白和 EOF 空行；788,235 bytes，15,161 lines）

App 必须同时分发 `WEBRTC_SDK_WRAPPER_LICENSE.txt` 与 `WEBRTC.md`。版本升级时禁止沿用旧 bundle 或用 Maven POM URL 替代。

## 5. Fonts And Other Assets

当前 App bundle 含 10 个 WOFF2 文件：Fira Code、JetBrains Mono、Iosevka、Cascadia/Caskaydia Cove 和 Hack 的 Nerd Font Mono regular/bold variants。`remote-ui/src/assets/fonts/LICENSE` 记录 attribution 与 OFL-1.1 文本，生成时按固定文件 hash 复制为 `termx-app/public/third-party/FONTS.txt`。

字体文件、Nerd Fonts patch set 或 attribution 发生变化时必须重新核对上游版本、reserved font names 和 Hack/Nerd Fonts 的组合条款；仅凭字体文件名不能推断许可证。

## 6. Audit Commands

```bash
scripts/fetch-pinned-third-party-notices.sh --check
GO_LICENSES_BIN=/path/to/go-licenses scripts/generate-go-notices.sh --check
node scripts/generate-npm-notices.mjs --check
scripts/generate-android-notices.sh --check
scripts/license-audit.sh
```

生成器失败是 release blocker，不允许通过忽略 unknown、删除依赖条目或移除 notice 文件绕过。
