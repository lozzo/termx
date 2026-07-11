# TermX 仓库可维护性收口计划

状态：RM001 冻结基线

日期：2026-07-11

## 1. 目标

当前仓库已经完成 core-v2、TUI-v3、多 endpoint、公开远程 contract、私有云服务和 SmartRoute 的主要模型重建，但目录仍保留迁移期命名和构建方式。维护者需要同时理解多个 Go module、重复 Android 源码、公开/私有发布白名单和大量历史文档，才能判断一处改动应该落在哪里。

本计划只收口仓库组织和维护入口，不改变 terminal、endpoint、remote auth、WebRTC、SmartRoute 或云服务业务语义。完成后必须满足：

1. 公开 Go 代码使用一个根 module 和一份依赖图。
2. 目录名表达领域，不再携带已经失去区分意义的 `v2`、`v3` 后缀。
3. Web UI 与移动 App 位于统一 `clients/` 边界。
4. Android 自定义源码只有一份 truth source。
5. 私有云服务继续保持独立部署 module，但统一位于 `private/cloud/`。
6. 根目录只保留仓库入口、法律文件和 workspace 配置；构建产物统一进入忽略目录。
7. `workflow.md` 只驱动当前活动切片，完成历史由 Git 和历史文档承担。

## 2. 当前维护成本

RM001 盘点得到：

- 公开 Go 代码分布在 9 个 module，依靠约 28 条本地 `replace` 连接。
- 私有云包含 7 个独立 Go module；这些 module 对应部署边界，应继续保留。
- `termx-app/native/android/` 与 Android Gradle source tree 有 21 组相同 Kotlin 文件。
- `private/archive/` 包含 443 个历史文件，只供代码考古，不属于活动依赖图。
- 根目录存在约 250 MiB 的忽略测试二进制和 `bin/` 产物，源码状态虽干净但工作区难以辨认。
- 活动和历史设计文档合计超过 1.7 万行；`workflow.md` 同时保存任务队列和大量完成记录。

这些问题的根因不是领域边界缺失，而是迁移期的物理目录、module 边界和日常命令没有跟随已经稳定的领域模型收口。

## 3. 目标目录

```text
.
├── cmd/
│   └── termx/                 公开 CLI、daemon 与 TUI 装配入口
├── core/                      terminal lifecycle、history、live truth
├── tui/                       TUI runtime、EndpointManager、state、render
├── remote/                    WebRTC、DataChannel 与端到端 remote auth 接线
├── shared/                    endpoint、transport、remote auth、Companion contract
├── proto/                     versioned public protobuf 与生成代码
├── vterm/                     terminal semantic interpreter
├── testkit/                   跨领域公开测试辅助
├── internal/                  仓库内 protocol 与 frame audit
├── clients/
│   ├── ui/                    共享 React UI 与平台中立 runtime interface
│   └── mobile/                Capacitor/Android App 与 native bridge
├── private/
│   ├── cloud/
│   │   ├── companion/         用户侧闭源 Cloud Companion
│   │   ├── control-plane/     账号、设备、entitlement、lease、usage
│   │   ├── hub/               presence 与 signaling
│   │   ├── relay/             TURN、lease enforcement、usage meter
│   │   ├── route-planner/     quality、SmartRoute、后续 Relay Mesh
│   │   ├── web-controller/    私有管理面
│   │   └── mobile/            Official App 私有 source set
│   └── archive/               只读历史资产，不进入依赖图
├── fixtures/                  terminal/protocol contract fixtures
├── docs/                      产品、架构、开发和历史文档
├── scripts/                   构建、生成、审计和发布门禁
├── .artifacts/                统一忽略的本地构建与测试产物
├── go.mod                     唯一公开 Go module
├── go.sum
├── go.work                    根公开 module + 私有部署 module
├── package.json               clients npm workspace 入口
├── package-lock.json
├── Makefile
└── README.md
```

不增加 `public/` 包装目录。规则保持为：除 `private/`、内部工作流文件和明确发布排除项外，根目录活动源码默认属于未来公开快照。这样私有 monorepo 与未来 public repo 使用同一套领域目录，不需要在复制时重写路径。

## 4. Go Module 决策

### 4.1 公开代码

公开 Go 代码合并为根 module：

```text
module github.com/lozzow/termx
```

目标 import path：

```text
github.com/lozzow/termx/core
github.com/lozzow/termx/tui
github.com/lozzow/termx/remote
github.com/lozzow/termx/shared/...
github.com/lozzow/termx/proto/...
github.com/lozzow/termx/vterm/...
github.com/lozzow/termx/testkit
github.com/lozzow/termx/internal/...
```

公开 package 不独立发版，也没有真实的跨仓 semantic version 需求。继续维护 9 份 `go.mod`、`go.sum` 和本地 `replace` 只会制造伪边界。领域边界改由 package 方向、dependency guard 和测试守卫保证。

### 4.2 私有代码

以下私有部署单元继续保持独立 module：

- `private/cloud/companion`
- `private/cloud/control-plane`
- `private/cloud/hub`
- `private/cloud/relay`
- `private/cloud/route-planner`
- `private/cloud/web-controller`

它们可以依赖根公开 module；公开 module 不得 import `private/`。私有服务之间只按显式 contract 或签名凭据依赖，不因为目录整理合并数据库模型或部署生命周期。

`private/cloud/mobile` 是 Android Official 私有 source set，不是 Go module。

## 5. Client 与 Android 决策

根 npm workspace 只包含：

```text
clients/ui
clients/mobile
```

`clients/ui` 是共享 React package；`clients/mobile` 依赖 workspace 中的 UI package，并拥有 Capacitor/Android 壳。两者仍可独立执行自己的 test/build script，但依赖安装和 lockfile 由根 workspace 统一。

Android truth source 固定为：

```text
clients/mobile/android/app/src/main/java/com/termx/app/
```

删除 `native/android` 镜像和把 Kotlin/Java 文件反复复制到 Gradle source tree 的脚本逻辑。`npx cap sync` 不得删除或重写自定义 source；若 Capacitor 升级破坏该保证，应让同步命令明确失败，而不是恢复第二份源码真值。

Official 私有 Kotlin 继续位于 `private/cloud/mobile/android`，只由官方 init script 作为额外 source set 注入；Community build 不引用该目录。

## 6. 文档与工作流决策

- `README.md` 只解释产品、快速开始、当前能力和目标仓库地图。
- `docs/remote-platform/` 继续保存远程产品、安全、分发和加速规范。
- `docs/development/` 保存维护入口、测试矩阵和领域 ownership。
- `core/docs/`、`tui/docs/` 只保留仍约束当前实现的领域设计。
- 已完成迁移计划、旧问题分析和一次性验收记录移入 `docs/history/`；它们不能继续作为当前实现 truth source。
- 根目录孤立设计文档必须归入对应领域或 `docs/history/`。
- `workflow.md` 保留当前目标、允许范围、未完成/阻塞队列、准入命令和最近一个切片摘要。完成切片的详细记录由 Git commit、架构文档和 `docs/history/` 承担。

## 7. 构建与维护入口

最终根 Makefile 至少提供：

```text
make build           构建 termx 到 .artifacts/bin/
make test            测试公开 Go module
make test-private    测试所有私有 Go module
make test-clients    测试/类型检查共享 UI 与移动 Web 壳
make test-android    测试 Community Android
make test-all        组合上述准入，不隐藏失败
make doctor          检查 Go/Node/Java/Android/protoc 环境和生成物一致性
make clean           只删除已知 .artifacts 与标准构建输出
```

禁止测试命令在仓库根生成 `*.test`，禁止发布构建写入顶层 `bin/`。`.artifacts/` 只保存可再生本地产物并整体忽略，不进入 public snapshot。

## 8. 迁移切片

### RM001：冻结目录与门禁

- 建立本计划并更新活动工作流。
- 冻结目标目录、module、client、Android 和文档 truth source。
- 不移动 runtime 文件，不改变构建行为。

准入：`git diff --check`。

### RM002：公开 Go module 与领域目录收口

- 建立根 `go.mod/go.sum`。
- 将 CLI、core、TUI、remote、shared、proto、vterm、testkit 移入目标路径。
- 删除公开子 module 与本地 `replace`，更新全部公开/私有 import。
- 更新 Go workspace、Go 构建脚本、dependency guard 和 notice 生成入口。
- 不改变业务 contract，不保留旧 import path adapter。

准入：根公开 Go 全量、私有 Go 全量、相关 race/vet、CLI build、dependency guard 和 `git diff --check`。

### RM003：Client workspace 与 Android 单一源码

- `remote-ui` 迁入 `clients/ui`，`termx-app` 迁入 `clients/mobile`。
- 建立根 npm workspace 与单一 lockfile。
- 删除 Android 镜像源码和复制逻辑，以 Gradle source tree 为唯一 truth。
- 更新 Community/Official 构建和 class boundary harness。
- 不改变 endpoint、WebRTC、credential 或 UI 业务语义。

准入：UI test/typecheck/build、mobile Web build、Community/Official clean Android unit/APK、DEX 私有类边界和 `git diff --check`。

### RM004：私有目录、维护入口与文档收尾

- `private/termx-cloud` 迁入 `private/cloud`，保持服务 module/deploy 边界。
- 更新 Makefile、scripts、public snapshot、license audit 和全部活动文档路径。
- 将根孤立文档和完成迁移材料归档，压缩 `workflow.md` 为活动驱动文件。
- 构建产物统一到 `.artifacts/`，提供 `clean` 与 `doctor`。
- 删除所有旧目录名、旧 module path 和旧构建入口，不保留兼容 fallback。

准入：`make test-all`、public/private license audit、同一提交树 public snapshot 独立构建、路径残留 guard 和 `git diff --check`。

## 9. 迁移不变量

目录重组期间始终保持：

- terminal lifecycle/history truth 只属于 core。
- endpoint 路由只使用 `TerminalRef`，不退回裸 `TerminalID` 全局真值。
- TUI/App 不拥有 committed history 或 terminal lifecycle。
- WebRTC 仍只是 transport；direct/single-relay/mesh 仍只是 observed path。
- CapabilityGrant 和 terminal payload 不进入 Companion、Hub、Relay 或 route plan。
- public package 不 import `private/`。
- local/SSH 不依赖云账号、Companion、Hub 或订阅。
- Community App 和缺少 Companion 的公开构建继续 fail closed，但保持 local/SSH 可用。
- archive 不进入 go.work、npm workspace、构建脚本或 runtime fallback。

任何切片若只能通过旧路径 adapter、双 module、双源码、临时 replace 或发布时删除 import 才能通过，必须回到目标结构修正，不能把迁移债务留给后续功能开发。
