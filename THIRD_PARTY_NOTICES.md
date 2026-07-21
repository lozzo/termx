# Muxvia Third-Party Notices

本文件是当前 private monorepo 的第三方材料索引。根 `LICENSE` 对 Muxvia 自有材料保留全部权利，但不改变任何第三方材料的原始许可。

## Artifact Notice Bundles

| Artifact | 随包 notice | 用户入口 |
| --- | --- | --- |
| public `muxvia` CLI/daemon | `cmd/muxvia/THIRD_PARTY_NOTICES.txt` | `muxvia licenses` |
| private `muxvia-cloud` Companion | `private/cloud/companion/cmd/muxvia-cloud/THIRD_PARTY_NOTICES.txt` | `muxvia-cloud licenses` |
| Community/Official Android App | `clients/mobile/public/THIRD_PARTY_NOTICES.txt` 与 `clients/mobile/public/third-party/` | App web assets 与 APK `assets/public/` |
| future public source snapshot | `docs/legal/public-snapshot/THIRD_PARTY_NOTICES.md` | 新公开仓库根目录 |

这些 bundle 按实际 Go build graph、npm lockfile、Gradle runtime graph 和内嵌资产分别生成。单独列出 SPDX 名称或本索引不能替代 artifact 内的完整版权、许可和 NOTICE 文本。

## Vendored And Embedded Material

- `vterm/internal/vt/LICENSE`：Charmbracelet terminal implementation 的 MIT 文本。
- `clients/ui/src/assets/fonts/LICENSE`：当前 App 内嵌 Nerd Font Mono 字体的 attribution 与 OFL-1.1 文本。
- `clients/mobile/public/third-party/WEBRTC.md`：Android WebRTC native build 及其第三方组件的固定上游 notice bundle。
- `docs/legal/third-party/`：npm/Maven artifact 没有随包提供独立 license 文件时使用的固定上游文本。

## Reproducible Audit

```bash
scripts/license-audit.sh
```

该命令验证 pinned source hash、Go 三平台依赖、两个 production npm lockfile、Android release runtime graph、App 静态 notice 和 public/private Go 依赖方向。实际发布仍需同时生成 SBOM、签名和 provenance，并按 `docs/legal/licensing-and-distribution-review.md` 完成人工审批。
