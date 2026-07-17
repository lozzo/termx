# TermX 开发与维护

本文是仓库日常维护入口。产品与远程平台约束见 `docs/remote-platform/`，领域细节见 `core/docs/`、`tui/docs/`，已完成计划和一次性审计见 `docs/history/`。

目录 ownership 与依赖方向统一见 [`repository-layout.md`](repository-layout.md)，Proto API 强约定见 [`proto-api-architecture.md`](proto-api-architecture.md)，当前迁移盘点见 [`proto-api-inventory.md`](proto-api-inventory.md)；下表只提供入口索引，不另定义架构。

公开 CLI 的长期命令树、target、输出、退出码和 tmux 能力映射见 [`cli-command-design.md`](cli-command-design.md)。

## 仓库地图

| 路径 | 责任 |
| --- | --- |
| `cmd/termx/` | 公开 CLI、daemon 与 TUI 装配入口 |
| `client/endpoint/` | Endpoint/Route registry、assembler、planner 与 portable contract |
| `client/runtime/` | 跨端 route race、ReadySession、generation 与 session owner |
| `client/port/`、`client/adapter/` | host capability 接口和 local/SSH/managed/protocol adapter |
| `core/` | terminal lifecycle、live surface、history 与 daemon storage truth |
| `api_layer/`、`api_mapping/` | generated proto 驱动的 application API 与 core/proto 无状态字段映射；transport/framing 归独立 adapter 与 protocol |
| `tui/` | UI state、reducer/effect、terminal host、workbench/copy/history 投影、交互与渲染 |
| `remote/` | 公开 WebRTC/DataChannel transport 与端到端 remote auth 接线 |
| `shared/` | 尚未迁移的 transport、remote auth、Cloud Companion 和 infrastructure primitive；不得新增 domain owner |
| `proto/` | 插件、客户端、跨进程和跨语言 API 的唯一 schema truth 与生成代码 |
| `internal/protocol/` | framing、Hello、channel、correlation 与 proto payload transport |
| `clients/ui/` | 共享 React UI 与平台中立客户端 runtime interface |
| `clients/mobile/` | Capacitor/Android 壳、native bridge 与 Community App |
| `private/cloud/` | 闭源 Companion、Control Plane、Hub、Relay、Route Planner、Web Controller 与 Official App source set |
| `private/archive/`、`docs/history/` | 只读代码/文档考古资产，不进入活动 workflow、构建或 runtime fallback |
| `fixtures/`、`testkit/` | 跨语言 contract fixture 与测试辅助 |
| `scripts/` | 生成、诊断、license、public snapshot 和仓库结构门禁 |

公开 Go 代码只有根 `go.mod`。`private/cloud` 下六个部署单元保留独立 `go.mod`；它们可以依赖根公开 contract，公开 module 不得 import 私有实现。npm 只有根 `package-lock.json`，workspace 只包含 `clients/ui` 与 `clients/mobile`。

## 常用命令

```bash
make doctor
make build
make test
make test-private
make test-clients
make test-android
make test-all
make clean
```

- `make doctor` 是开始开发前的只读环境检查，验证 Go/Node/Java/Android/protobuf 工具、module/workspace 布局、生成代码和 Android 单一源码。
- `make build` 只把公开 `termx` 写入 `.artifacts/bin/termx`。
- `make test` 清理调用终端继承的全部 `TERMX_*`，并使用 `GOWORK=off` 测试根公开 module，保证结果不受当前远程会话污染且 public snapshot 不依赖私有 workspace。
- `make test-private` 逐个测试六个私有 Go module；public snapshot 没有 `private/cloud` 时明确跳过。
- `make test-clients` 依次生成 protobuf、运行共享 UI 测试与类型检查，并构建 UI/Mobile Web。
- `make test-android` 先同步 Capacitor，再 clean 构建 Community APK；私有 source set 存在时继续 clean 构建 Official APK，并验证 DEX class 边界。可交付 APK 副本写入 `.artifacts/android/`。
- `make test-all` 顺序组合全部测试，不吞掉子命令失败。
- `make clean` 只删除 `.artifacts`、旧根构建残留和标准 Go/Node/Gradle 输出，不删除依赖缓存或源码。

## 生成代码

Go protobuf 来自 `proto/{apipb,cloudpb,remoteauthpb,wirepb}`。当前 `PA005G` 只要求同步 Go generated code 与 public descriptor；客户端 TypeScript generated code 在后续 App/Web 切片同步。`scripts/check-generated-code.sh` 的全端比较门禁在客户端迁移完成后恢复。

```bash
npm run proto
scripts/check-generated-code.sh
```

Go 生成物需要当前 `protoc` 与 `protoc-gen-go`；TypeScript 生成器由根 npm workspace 固定。生成器版本变化必须与生成文件放在同一个切片审核。

## Android 边界

Android 自定义源码唯一真值是 `clients/mobile/android/app/src/main/`。`clients/mobile/native/android` 不得恢复；`cap sync` 后 `clients/mobile/scripts/verify-android-source.sh` 会检查源码、manifest、network config 和 Gradle 关键配置。

Community 构建不得引用 `private/cloud/mobile`。Official 构建只通过 `private/cloud/mobile/android/official-cloud.init.gradle` 注入固定 source set；`scripts/verify-android-apk-boundary.sh` 验证 Community 不含私有 cloud class，Official 必须包含 `OfficialManagedCloudFactory`。

## 产物与发布

仓库级可再生产物统一写入 `.artifacts/`，该目录整体忽略且不得跟踪。Node/Gradle 自身的标准 `dist`、`build`、`.gradle` 目录仍留在各自工程边界，并由 `make clean` 清理。

未来 public snapshot 必须从一个已提交 revision 按 `docs/remote-platform/public-snapshot-manifest.md` 复制到全新空目录，不复制 private Git 历史。发布前同时运行 private monorepo 与 public snapshot license audit、production npm audit、APK class boundary、secret scan 和最终 artifact SBOM。
