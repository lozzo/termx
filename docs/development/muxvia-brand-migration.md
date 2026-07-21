# Muxvia 品牌与发布身份迁移

## 目标

本迁移在首次公开发布前，把活动产品从历史名称 `TermX` 完整迁移为 `Muxvia`。迁移只改变品牌、发布身份和由其派生的技术标识，不改变 Proto 字段编号、API 语义、领域 owner、连接模型、安全边界或产品能力。

## 标识矩阵

| 领域 | 历史标识 | 正式标识 |
| --- | --- | --- |
| 产品 | `TermX` | `Muxvia` |
| 托管服务 | `TermX Cloud` | `Muxvia Cloud` |
| 主域名 | 未冻结 | `muxvia.com` |
| Go module | `github.com/lozzow/termx` | `github.com/muxvia/muxvia` |
| CLI/主二进制 | `termx` | `muxvia` |
| Cloud Companion | `termx-cloud` | `muxvia-cloud` |
| Controller | `termx-cloud-controller` | `muxvia-cloud-controller` |
| Edge | `termx-cloud-edge` | `muxvia-cloud-edge` |
| URI scheme | `termx://` | `muxvia://` |
| Android applicationId/package | `com.termx.app` | `com.muxvia.app` |
| npm scope | `@termx/*` | `@muxvia/*` |
| Proto package | `termx.*` | `muxvia.*` |
| C ABI/native prefix | `termx_*` / `termx_client` | `muxvia_*` / `muxvia_client` |
| 环境变量 | `TERMX_*` | `MUXVIA_*` |
| 配置/状态目录 | `termx` | `muxvia` |
| 下载目录 | `Downloads/TermX` | `Downloads/Muxvia` |
| UI/CSS/event/storage key | `termx-*` / `termx:*` | `muxvia-*` / `muxvia:*` |

## 迁移顺序

1. Proto 源文件 package/go_package、Go modules/import、generated Go/TypeScript 和 descriptor fixture。
2. CLI 目录与二进制、C ABI/JNI/native library、URI、环境变量、配置/socket/log/state 路径。
3. Android package/applicationId、npm workspace/package/import、UI/CSS/event/storage key、Cloud composition 与发布资产。
4. 活动产品、架构、开发、法律和发布文档；历史目录保持原样。
5. 全仓残留扫描、构建、真实 APK smoke 和双 Agent 审查。

每一步必须从源码真值重新生成派生产物，不手改 generated 文件。路径重命名使用 Git 可追踪移动；不保留旧入口、旧 package、旧 URI、旧环境变量或运行时 fallback。

## 历史边界

以下位置保留历史品牌标识作为事实记录，不参与活动残留门禁：

- `.git/`
- `private/archive/`
- `docs/history/`
- 已有不可变测试证据和外部构建缓存；它们不得重新进入 runtime、workspace 或发布包。

除上述位置外，活动源码、Proto、生成代码、配置、测试、脚本、法律文本、发布资产和产品文档必须迁移为 Muxvia。测试数据若验证拒绝旧标识，可保留最小旧字符串，但必须在同一测试中明确标记为 legacy rejection fixture。

## 验收

- 活动范围残留扫描只允许明确 legacy rejection fixture。
- generated code 与 descriptor fixture 幂等。
- 全部 Go workspace、Node workspace、Cloud 双 Edge、standard/devcloud Android APK 通过。
- 最终 ARM64 APK 通过 Muxvia UI 添加 Endpoint、建立 Direct 连接、打开 terminal、输入并验证输出和 crash/secret 扫描。
- 架构 reviewer 确认改名未改变领域和安全模型；代码 reviewer 确认无旧入口、混合 package、漏改运行路径或发布身份。
