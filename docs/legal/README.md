# TermX Legal Release Baseline

本目录保存 LIC001 的工程许可基线，不替代适用法域内的专业法律意见。

- `licensing-and-distribution-review.md`：当前 private monorepo、未来 public snapshot、Cloud Companion、Official App 和 Enterprise bundle 的许可与发布门禁。
- `third-party-inventory.md`：按实际发布入口盘点的 Go、npm、Android 和内嵌资产许可清单与异常项。
- `private-artifact-distribution-gates.md`：Companion、Official App、managed service 与 Enterprise bundle 的书面条款和发布审批门禁。
- `third-party/`：上游 artifact 缺少独立 license 文件时按精确 commit/hash 固定的许可文本。
- `public-snapshot/`：未来复制到全新公开仓库根目录的 Apache-2.0、NOTICE、DCO 和 CONTRIBUTING 模板。

当前 private monorepo 的根 `LICENSE` 保留全部权利。只有复制到独立 public repository 并在该仓库根放置 `public-snapshot/LICENSE` 后，选定公开文件才按 Apache-2.0 发布。

完整工程审计入口为 `scripts/license-audit.sh`。它检查 generated notice 是否与当前 Go/npm/Gradle graph、固定上游 hash 和 public/private import 边界一致；首次对外发布仍需要 `licensing-and-distribution-review.md` 规定的外部专业审批。
