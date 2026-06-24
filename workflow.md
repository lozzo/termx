# 工作流：termx remote + app 端到端迁移主线

本文件是当前分支唯一有效的活动驱动文件。后续分析、实现、测试、提交都先看这里；如果本文件与旧说明、聊天记录、旧代码行为冲突，以本文件为准。

本文件已经从旧 history/copy 长队列压缩为 remote + app 端到端迁移队列。旧队列和完成记录不再在这里重复备份，按 git 历史追溯；本文件只保留当前阶段需要执行和约束的内容。

## 0. 怎么读

开始任何工作前只看这几段：

- `1. 当前目标`
- `3. 工作范围`
- `4. 不可违反的语义`
- `5. 扫描结论和任务队列`
- `6. 测试准入`

如果用户请求和任务队列冲突，先更新本文件，再改代码；不要靠口头约定跳过范围和顺序。

## 1. 当前目标

### 1.1 一句话目标

把 `termx remote ...`、core-v2 remote runtime 和真实 `termx-app/` 打通：CLI 启动和管理 remote，App 连接该 runtime 打开 terminal；App 可以持有自己的显示/滚动历史缓存，但复制模式和历史真值必须走 core-v2 authoritative logical-line history。

### 1.2 这轮只关心什么

当前阶段先迁 remote 的后端控制链路，再接真实 App：

1. 明确 remote 现有 method、service、storage、transport 和 CLI fallback 边界。
2. 把 `termx-proto` wire contract 与 `internal/protocol` Go contract 迁到 core-v2 domain 结构，而不是为旧 core 保留兼容层。
3. 在 core-v2 补齐 remote 所需的 typed protocol、transport scope、terminal create、storage/events 和 service hook。
4. 用 core-v2 adapter 接上 `termx-remote` public service，而不是把默认入口退回旧 core。
5. 分步迁移 `remote.status`、`remote.local.*`、`remote.pair.start` 和 remote terminal/storage routing。
6. 切换 CLI remote client/config 到 core-v2 默认 daemon，并清理旧 fallback 边界。
7. 保留 remote terminal/storage routing 的 core-v2 truth，不新增第二份 terminal truth。
8. 让 `termx-app/` 通过当前 CLI remote runtime 连接、列出、创建、进入 terminal。
9. 把 App 侧 live terminal display 和 infinite history surface 分层：live display 可以有本地短缓存，history/copy 必须通过 core-v2 logical-line window。
10. 后续无限历史回滚、窗口化渲染、选择和复制可以参考 `termx-app-history-ref/`，但不得把参考实现升级成新的历史真值。

### 1.3 不在当前阶段做什么

- 不迁 `web-control/`、`termx-hub/`。
- 不把 `termx-remote-v2/` 纳入实现范围；它当前只作为未跟踪实验目录存在。
- 不恢复旧 `termx-core/` 或 `tuiv2/`。
- 不对旧 core 协议、旧 storage 格式、旧 daemon method 或旧 remote fallback 做任何兼容；冲突时直接迁到 core-v2 contract。
- 不借 remote/App 迁移重开 TUI floating 的长尾问题；除非当前切片直接触发回归。
- 不把 App 本地显示缓存、xterm scrollback、DOM/canvas rows 或 native bridge backlog 当作 copy/history truth。

## 2. 技术设计基准

- core-v2：`termx-core-v2/docs/architecture.md`
- tui-v3：`termx-tui-v3/docs/architecture.md`
- CLI 切换审计：`termx-cli/docs/v2-v3-switch-audit.md`
- remote public service：`termx-remote/`
- wire protocol contract：`termx-proto/`
- protocol contract：`internal/protocol/`
- real app：`termx-app/`
- app shared UI/runtime package：`remote-ui/`
- infinite history UI reference：`termx-app-history-ref/`
- terminal live stream tradeoff memo：`terminal-live-stream-tradeoff.md`

如果实现发现设计文档过期，必须和当前切片一起更新；不要代码先跑偏，文档以后再补。

## 3. 工作范围

### 3.1 当前主线允许主动修改

- `workflow.md`
- `AGENTS.md`
- `termx-cli/cmd/termx/remote_*.go`
- `termx-cli/cmd/termx/default_dependency_guard_test.go`
- `termx-cli/docs/v2-v3-switch-audit.md`
- `termx-core-v2/`
- `termx-remote/`
- `termx-proto/`
- `internal/protocol/`
- `termx-app/`，仅当当前切片进入真实 App remote/runtime/history 集成阶段

### 3.2 受限联动范围

只有当前切片确实需要时，才允许最小化触及：

- `termx-cli/cmd/termx/` 内非 `remote_*.go` 的必要 glue、旧 fallback 删除或测试
- `termx-tui-v3/` 中受 protocol/service adapter 影响的 smoke 或 harness
- `remote-ui/`，仅当 `termx-app/` 的 terminal/runtime/history/copy contract 必须调整
- `termx-shared/`，仅当 transport/session contract 必须调整
- `termx-testkit/`
- `scripts/`
- `Makefile`
- `go.work`
- `go.work.sum`
- 必要顶层说明文档

### 3.3 已删除旧目录

默认不得恢复：

- `termx-core/`
- `tuiv2/`

### 3.4 只读参考范围

默认不得修改：

- `termx-remote-v2/`
- `termx-app-history-ref/`，当前作为未跟踪本地参考目录，只读参考无限历史回滚和 history surface 设计

### 3.5 冻结范围

不得主动触碰，除非本文件先明确解冻：

- `web-control/`
- `termx-hub/`
- `bin/`
- `.claude/`
- 顶层可执行产物和测试产物
- 未在本文件列出的目录

## 4. 不可违反的语义

### 4.1 默认本地入口不能回退

- 默认 `termx`、`daemon`、`attach`、`new`、`ls`、`kill`、`rm` 仍必须走 `termx-core-v2/` 和 `termx-tui-v3/`。
- 旧 `termx-core/` 和 `tuiv2/` 不得通过 `termx legacy ...`、remote fallback、test helper 或 go.mod replace 间接存在。
- 默认入口依赖守卫不能放松，任何 CLI 源文件都不得 import 旧 core/TUI。
- remote/protocol 迁移不得保留旧 core wire format、storage format、method adapter、双 handler、fallback 读写或兼容 shim。
- 跨进程、daemon client 或 remote service 需要共享的 payload 字段必须先进入 `termx-proto/wirepb/terminal.proto`，再由 `internal/protocol` 映射为 core-v2 Go domain；不能只在 CLI adapter 或反射 helper 里临时承接。

### 4.2 remote 不能拥有第二份 terminal truth

- terminal lifecycle、PTY size、attachment、events、history 和 storage 必须来自 core-v2 daemon/protocol。
- remote 只负责授权、配对、transport/session 和请求路由。
- remote storage 只能走 core-v2 storage API，不得把 TUI workbench、terminal lifecycle、copy/history 交互态写成 remote 私有 truth。

### 4.3 remote migration 必须分层

- 先审计 method/contract，再接 core-v2 extension hook，再接 service adapter，再启用 CLI flow。
- remote management request 必须通过清晰 adapter 路由到 core-v2 public/protocol 方法。
- 新 protocol structure 必须直接表达 core-v2 的 terminal、attachment、history、storage 和 event 模型；不得先模拟旧 core 协议再翻译到 core-v2。
- `.proto` 是 wire contract，不是旧 core contract；字段命名、optional/repeated 语义、时间单位、枚举和 scope 必须向 core-v2 domain 对齐。
- 不得直接读写 TUI reducer state、renderer、TerminalHost 或旧 core runtime。
- 不得用 storage scrub、定时刷新、重复 attach、局部 fallback 分支掩盖状态错乱。
- 禁止补丁式迁移：不能为了让某个 remote 命令暂时可用而叠加临时 if、重复同步、隐式状态修正或旧路径兜底；每个切片必须先明确 domain owner、truth source、消息链路和失败条件，再用契约测试或 harness 锁住语义。

### 4.4 tui-v3 和 history/copy 基线不能被破坏

- tui-v3 不以 Bubble Tea 作为主运行时。
- renderer 只消费 view-model，不读 core client、history source、runtime service 或 protocol client。
- copy/history 仍只消费 core-v2 authoritative history，不从 live/snapshot/VTerm scrollback fallback。
- panel/pane 只表达工作台槽位和连接意图，不表达 terminal running/exited 或 copy/history 交互态。

### 4.5 App remote/history/copy 边界

- `termx-app/` 是真实 App 入口，必须连接 CLI 启动的 remote/core-v2 runtime；不得另起一套 App 私有 terminal lifecycle、PTY、history 或 storage truth。
- `remote-ui/` 可以承载 App/Web 共享 UI、运行时接口、terminal live surface、history surface 和缓存，但不能反向定义 core-v2 protocol truth。
- App 允许自己持有显示历史、短 scrollback、render window、离线滚动缓存和 native bridge 状态；这些只能是 projection/cache，不能作为 copy mode、search、selection text 或历史窗口的权威来源。
- App 的 copy mode 必须通过 core-v2 logical-line history/window contract 取数；不得从 xterm buffer、snapshot scrollback、DOM/canvas rows、native bridge backlog 或 App 本地 append log 拼接最终文本。
- 无限历史回滚使用 logical-line cursor/token/generation 这类后端窗口语义；不得以 visual row、wrapped row、像素 offset 或 xterm scrollback index 作为后端 contract。
- `LiveSurface` 和 `HistorySurface` 必须分层：live surface 展示实时终端和短上下文，history surface 显式进入后按 frozen/tokenized history window 渲染、选择、复制、搜索。
- `termx-app-history-ref/` 只作为无限历史回滚、窗口化渲染、overscan、WebGL/Canvas/DOM renderer 的参考；参考实现里的 mock source、visual row 逻辑和 demo 数据不得成为新协议或历史真值。

### 4.6 实现纪律

- 先写 domain model、小 harness 或契约测试，再接真实 protocol、terminal 或 CLI。
- 代码必须按正确模型写完整；如果方案依赖“再刷一次状态”“失败就 fallback”“先 scrub storage”“兼容旧内部格式”才能成立，默认不合格，需要回到状态归属和 contract 重做。
- 关键代码写简短中文注释，只解释不自明的边界或约束。
- 手工编辑文件必须使用 `apply_patch`。
- 不得覆盖用户或其他代理的未提交改动。
- 不得 amend commit，除非用户明确要求。

## 5. 扫描结论和任务队列

### 5.1 代码扫描结论

这次扫描到的当前边界如下，后续切片必须按这些边界拆，不得用补丁式兼容绕过去：

- R175 后旧 `remote_runtime.go`、旧 CLI 私有 `remote_protocol_codec.go`、legacy daemon auto-start 和旧 `termx-core/tuiv2` fallback 已删除；`termx-cli/cmd/termx/remote_client.go` 现在只保留 `remote.status`、`remote.local.*`、`remote.pair.start` client 调用骨架，等待 core-v2 daemon 承接这些 method。
- `termx-cli/cmd/termx/remote_config.go` 已不再依赖旧 TUI，但 remote config path 与 v3 config policy 仍需要单独锁定，避免 App/CLI/local runtime 后续形成多套配置入口。
- `termx-remote.Service` 的边界是合理的：它只需要 daemon 提供 terminal management、storage、events、transport/session，不应拥有 terminal lifecycle/history/storage truth。
- `termx-remote` 的 `runtimepb` 是 remote runtime/localweb/WebRTC API，可保留；不能把它当成 core daemon protocol truth，也不能让 core-v2 模拟旧 core protocol 后再翻译。
- `internal/protocol` 已有 remote wire protobuf，但当前用反射/getter/wirepb 指针承接 remote params/results；迁移目标是新增显式 `protocol.Remote*` domain structs，移除反射和 legacy getter 兼容。
- `termx-core-v2` 已有 storage/events 和 protocol dispatch，但没有 remote service hook，也没有 public `ServeTransport/ServeScopedTransport`；remote WebRTC datachannel 需要 core-v2 自己的 transport scope API。
- `termx-core-v2` 的 `ProcessSpec` 当前只包含 `TerminalID/Command/Size`，而 `protocol.CreateParams` 和 remote terminal management 会传 `Dir/Env/Scrollback*`；这些字段必须进入 core-v2 create/process contract，不能在 remote adapter 里丢弃。
- `termx-proto/wirepb/terminal.proto` 是 remote/core-v2 跨进程 wire contract 的入口。当前已包含 `CreateParams` 的 `dir/env/scrollback_*` 和 `RemoteStatus/RemotePairStart*/RemoteLocal*`，但后续必须先审计这些字段是否完整表达 core-v2 domain；需要共享的新字段必须写入 `.proto` 并同步生成 `terminal.pb.go`，不能只补在 Go 侧反射/adapter。
- `termx-app/` 是 Capacitor React 真实 App：`TermxApp.tsx` 挂载 `RemoteControlApp`，注入 Native HTTP/storage/QR、`createNativeMachineRuntime` 和 native file transfer context。
- `termx-app/src/NativeConnectionProxy.ts` 通过 Capacitor `NativeConnection` 插件做控制面，通过本地 WebSocket bridge 承载 WebRTC/datachannel 二进制帧，并把 `terminal:${machineId}:${terminalId}` channel 映射成 `RtcSession.openTerminal`；它不是 terminal/history truth。
- `termx-app/src/plugins/nativeConnection.ts` 的 native contract 是 machine/session/bridge/transfer 维度，当前没有 logical-line history/copy contract；后续如需 App history 必须通过 remote/core-v2 runtime API 补齐，而不是塞进 native bridge 私有状态。
- `remote-ui/` 是 `termx-app` 当前依赖的共享 UI/runtime 包。它现在的 terminal 路径仍包含 snapshot/scrollback、xterm scrollback、`loadScrollback` 和本地 text prefix 拼接逻辑；这些可以作为 live display/cache 参考，但 copy/history 迁移时必须改成 core-v2 logical-line source。
- `termx-app-history-ref/` 当前是未跟踪本地参考目录，包含 `MockHistorySource`、`HistoryCacheWindow`、`HistorySurfaceApp` 与 WebGL/Canvas/DOM renderer demo；只能参考其 window/cache/overscan/renderer 结构，不能照搬 mock source 或 visual-row truth。
- R189 已补 `remote-ui/docs/app-core-v2-contract.md`：`termx-app` 注入 native runtime adapter，`remote-ui` 保持 TypeScript interface 边界；runtime API、terminal live datachannel、logical-line `CoreV2HistorySource`/`HistorySurface` 分层；现有 snapshot、`loadScrollback`、xterm buffer、native bridge backlog 和 App 本地 append/cache 明确只能作为 live display/cache，不得作为 App copy/search/selection truth。
- R190 已在 `remote-ui/src/terminal/coreV2TerminalProtocol.ts` 建立 core-v2 terminal/history TypeScript domain contract：`history.window`、`history.copy`、`history.release` 使用 logical-line cursor/token/generation/range 参数；`HistoryWindow` payload 归一为 logical-line render rows/line spans；旧 snapshot、visual scrollback、xterm buffer 和 `loadScrollback` 明确只能作为 live display cache。当前 `termx-proto/wirepb/terminal.proto` 已包含 `HistoryWindow*`，但 `remote-ui/src/generated/wirepb/terminal_pb.ts` 仍缺这些生成类型，本机缺 `protoc` 且先不手写生成文件；后续如需 wirepb TS codec，必须先同步生成物。
- R191 已让真实 App/native runtime 只通过 CLI remote 暴露的 local/hub Hub API 建立连接：`remote-ui` 删除旧 `createLocalAgentApi` 与 `/api/local/status` caller，legacy cleanup harness 锁定不得恢复 `/api/local/status`、`/api/local/rtc/offer`、`/api/local/pair`、`/api/local/terminals`；Android native local connector 使用标准 `/api/v1/sessions/ice` 探测本地 Hub 并复用返回的 ICE 配置进入 `WebRTCTransport.connectHub`，不再先访问 App 私有 local status。R191 已通过 `remote-ui` typecheck/test/build、`termx-app` build、`termx-app` cap:sync、旧 local API 静态搜索和 `git diff --check`；已用 Homebrew 安装 OpenJDK 21 并重试 Android Kotlin 编译，当前阻塞于本机缺失 Android SDK：`termx-app/android/local.properties` 指向的 `/Users/lozzow/Library/Android/sdk` 不存在。
- R192 已补 `remote-ui/src/integration/appTerminalRuntimeContract.test.ts`，用同一个 core-v2 App session 锁定 terminal inventory/list、create/restart/remove 管理 API 与 live terminal attach/input/resize 都经由 session API channel 和 terminal datachannel；App live surface 继续允许 xterm/snapshot/短 scrollback 作为显示缓存，但 terminal lifecycle、resize owner、input 和管理操作由 core-v2 runtime session 承接。R192 已通过 `remote-ui` focused test/typecheck/test/build、`termx-app` build、旧 local/fallback 静态搜索和 `git diff --check`；已用 OpenJDK 21 重试 Android/Kotlin 编译确认，当前阻塞于本机缺失 Android SDK。
- R193 已补 `remote-ui/src/terminal/coreV2HistorySource.ts`，让 App/shared UI 通过 machine-scoped `RtcSession.openApi()` 调用 core-v2 `history.window`，并归一化为 logical-line `CoreV2HistoryWindow`；focused harness 证明 latest/older window 使用 token、generation、logical cursor、line boundary，不打开 terminal datachannel、不调用 `loadScrollback`、不从 snapshot/xterm/live cache 取历史。R193 已通过 `remote-ui` focused test/typecheck/test/build、`termx-app` build、旧 local/fallback 静态搜索和 `git diff --check`。
- R194 已补 `remote-ui/src/terminal/coreV2HistorySurface.ts`，在 `CoreV2HistorySource` 之上建立 App infinite history surface/cache：latest window 建立 frozen token/generation，older/newer 只能通过 logical cursor 与 line boundary 继续分页，render window/overscan/cache trim 只是 App 投影，token/generation 变化会让 surface stale 并清空本地 cache。focused harness 证明 window 合并、overscan render window、cache trim 后的方向重载和 stale 失效语义；R194 已通过 `remote-ui` focused test/typecheck/test/build 与 `termx-app` build。
- R195 已补 `remote-ui/src/terminal/coreV2HistoryInteraction.ts`，把 App history selection/range/search/copy 都绑定到 `CoreV2HistorySurfaceSnapshot` 的 logical line/cell 坐标；`CoreV2HistorySource.copy()` 通过 machine-scoped API 调 `history.copy`，最终文本由 core-v2 frozen logical-line snapshot 生成。focused harness 证明 copy 不调用 xterm selection、visual scrollback、DOM/canvas renderer text 或 App append log，search 只返回 logical-line range；R195 已通过 `remote-ui` focused test/typecheck/test/build 与 `termx-app` build。
- R196 已补 `remote-ui/src/integration/appCoreV2EndToEndSmoke.test.ts`，用同一个 core-v2 App session 串起 terminal create、terminal datachannel attach/input/resize、logical-line history latest/older rollback 和 `history.copy`；smoke 断言 rollback/copy 不走旧 terminal history replay/live scrollback。R196 已通过 `remote-ui` focused test/typecheck/test/build、`termx-app` build、`termx-cli` remote/default focused Go tests、`termx-core-v2` scoped transport/history copy focused Go tests 和 `termx-remote` 全量 Go tests；Android/Kotlin 编译仍因本机缺 Android SDK 未运行。
- R197 已更新 `termx-cli/docs/v2-v3-switch-audit.md` 与 `remote-ui/docs/app-core-v2-contract.md`，把 remote + App 迁移从“后续 App 集成”收口为当前完成状态：记录 App 连接 CLI remote runtime 的方式、terminal/live/history/copy truth 边界、`CoreV2HistorySource/Surface/Interaction` 分层、`termx-app-history-ref/` 只读参考取舍、完整测试证据和 Android SDK 缺失说明。R197 为 docs-only，准入 `git diff --check`。
- R198 已补 `terminal-live-stream-tradeoff.md`：记录完整连续客户端 PTY bytes、慢客户端不丢、不反压程序三者不能同时成立；后续默认回到 core-v2 维护 latest screen 和 logical-line history，App/TUI 本地 scrollback 只能作为显示缓存，copy/search/history truth 必须走 core history。

### 5.2 任务队列

状态只能使用：`待开始`、`进行中`、`完成`、`阻塞`。同一时间只能有一个切片处于 `进行中`。

自动执行时只看下面这张表，按顺序取最早未完成切片：

| 切片 | 状态 | 范围 | 白话说明 |
| --- | --- | --- | --- |
| 背景里程碑：local v2/v3 切换与 history/copy/floating 收口 | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`termx-cli/`、`internal/protocol/`、相关文档 | 默认本地入口、TerminalView/Attachment、authoritative history、copy/history、floating、owner/attachment 计数已经收口；细节按 git 历史追溯 |
| R173. SK remote 迁移工作流重置 | 完成 | `AGENTS.md`、`workflow.md` | 已收紧 AGENTS 的 remote 迁移边界，压缩 workflow 为 remote 主线，并保留测试/提交准入 |
| R173B. SK 禁止补丁式实现准入 | 完成 | `AGENTS.md`、`workflow.md` | 已把禁止补丁式实现写成硬语义：先定位 truth/source/message chain，再按模型和 harness 实现 |
| R173C. SK core-v2 protocol-only 准入 | 完成 | `AGENTS.md`、`workflow.md` | 已明确 remote/protocol 迁移不对旧 core 做任何兼容，协议结构必须迁到 core-v2 domain contract |
| R174. SK remote 迁移代码扫描与队列设计 | 完成 | `workflow.md`、只读扫描 `termx-cli/`、`termx-remote/`、`termx-core-v2/`、`internal/protocol/`、`termx-proto/`、旧 `termx-core/` | 已定位 CLI legacy daemon 入口、旧 core adapter、remote service 边界、core-v2 缺口和后续切片顺序 |
| R175. SK 删除旧 core/tuiv2 fallback | 完成 | `termx-core/`、`tuiv2/`、`AGENTS.md`、`workflow.md`、`go.work`、`go.work.sum`、`termx-cli/`、`termx-testkit/`、`termx-remote/`、按需 `termx-hub/go.mod` | 已删除 `termx-core/` 与 `tuiv2/`，移除 legacy command、旧 daemon auto-start、remote fallback adapter 和所有构建引用；remote 命令只连 core-v2 daemon，未迁 hook 时由 core-v2 contract 明确失败 |
| R175B. SK 真实 App 和历史参考并入工作流 | 完成 | `workflow.md`、只读扫描 `termx-app/`、`remote-ui/`、`termx-app-history-ref/` | 已把目标扩展为 CLI remote + core-v2 + 真实 App 端到端；`termx-app` 解冻到后续 App 切片，`remote-ui` 作为受限联动，`termx-app-history-ref` 作为只读无限历史参考；写明 App 本地历史只能是显示缓存，copy/history truth 必须走 core-v2 logical-line |
| R176. SK termx-proto core-v2 wire contract | 完成 | `termx-proto/`、`internal/protocol/` 只读参照、`termx-core-v2/` 只读参照、`termx-remote/` 只读参照 | 已用本机 `buf` + `protoc-gen-go` 重新生成 `terminal.pb.go`，让 `HistoryWindowParams` 的 `mode/after_cursor/range` 等 core-v2 logical-line history 字段进入生成类型；新增 wirepb descriptor contract test 锁定 create、terminal info、history window、remote status/local/pair、storage 字段号 |
| R177. SK remote protocol typed contract | 完成 | `internal/protocol/`、按需 `termx-proto/` | 已新增显式 `protocol.RemoteStatus`、`RemotePairStartParams/Result`、`RemoteLocalEnableParams`、`RemoteLocalStatus`；`Encode/DecodeMethod*` 不再用反射/getter/wirepb 指针作为 remote domain，并补 protocol tests |
| R178. SK core-v2 create/process remote contract | 完成 | `termx-core-v2/`、`internal/protocol/`、按需 `termx-proto/` | 已让 core-v2 terminal create/process contract 承载 `Dir`、`Env`、CWD metadata 和 remote create 需要的 scrollback 参数；process spawn 与 restart 复用同一 create options，不在 remote adapter 里静默丢字段 |
| R179. SK core-v2 transport scope API | 完成 | `termx-core-v2/`、`internal/protocol/`、`termx-proto/` 按需、`termx-shared/` 按需 | 已给 core-v2 提供 public `ServeTransport` / `ServeScopedTransport` / `TransportScope` 能力；scope 在 protocol session 边界约束 terminal method、stream channel 和事件订阅，不创建第二份 terminal truth |
| R180. SK core-v2 remote method hook | 完成 | `termx-core-v2/`、`internal/protocol/` | 已在 core-v2 protocol dispatch 中接入 typed `RemoteService` hook，用 fake service 证明 `remote.status`、`remote.pair.start`、`remote.local.*` 经过 core-v2；未恢复旧 `ProtocolMethodHandler` wire bytes contract |
| R181. SK core-v2 remote daemon adapter | 完成 | `termx-cli/cmd/termx/remote_*.go`、`termx-core-v2/`、`termx-remote/` | 已用 core-v2 server/domain 实现 `termx-remote.Service` 需要的 Daemon/StorageDaemon/ScopedDaemon adapter；CLI remote client 已切到 `protocol.Remote*` typed contract，remote terminal datachannel 优先进入 scoped daemon，不恢复旧 `remote_runtime.go` |
| R182. SK core-v2 daemon remote lifecycle | 完成 | `termx-cli/cmd/termx/`、`termx-core-v2/`、`termx-remote/` | 默认 `termx daemon` 已装配 remote config/service/start/close/local auto-enable；remote runtime 生命周期跟随 core-v2 daemon，未回退 legacy daemon |
| R183. SK remote config path 独立策略 | 完成 | `termx-cli/cmd/termx/remote_config.go`、按需共享 config helper | 已让默认 `daemon`、`v3 daemon`、remote CLI auto-start 和 remote config bootstrap 共用同一 remote config path 策略：显式 `--config` 优先，否则走当前 v3 config 默认路径；remote 文件未引入旧 TUI |
| R184. SK remote status/local/pair core-v2 smoke | 完成 | `termx-cli/`、`termx-core-v2/`、`termx-remote/` | 已补真实 CLI/core-v2 smoke：`remote.status`、`remote.local.enable/status/disable`、`remote.pair.start` 经由 core-v2 daemon socket 和真实 `termx-remote.Service`，不经过旧 core |
| R185. SK remote terminal management/storage/events routing | 完成 | `termx-remote/`、`termx-core-v2/`、`termx-cli/`、`termx-testkit/` 按需 | 已验证 remote runtime API 的 terminal list/create/get_directory/set_metadata/restart/remove、storage get/put/delete/list、events subscription 都经 `termx-remote.Service` 路由到 core-v2 truth |
| R186. SK remote transport session core-v2 routing | 完成 | `termx-core-v2/`、`termx-remote/`、`termx-shared/`、`termx-testkit/` 按需 | 已验证 remote WebRTC/datachannel transport 通过 `termx-remote.Service` 进入 core-v2 `ServeScopedTransport` protocol session，terminal scope 与 machine-events-only scope 行为正确 |
| R187. SK remote backend contract smoke | 完成 | `termx-cli/`、`termx-core-v2/`、`termx-remote/`、`termx-testkit/`、必要文档 | 已用后端 contract smoke 验证 remote status/local/pair、terminal/storage/events、transport session 全部经过 core-v2 truth，并守卫旧 fallback 目录/文件不得恢复 |
| R188. SK remote backend docs checkpoint | 完成 | `workflow.md`、`termx-cli/docs/v2-v3-switch-audit.md`、必要顶层文档 | 已更新审计文档和当前状态，记录 remote 后端已迁 core-v2 contract、旧 fallback 已删除边界和 backend smoke 证据；后续继续 App 集成 |
| R189. SK App/remote-ui contract 准入设计 | 完成 | `termx-app/`、`remote-ui/`、`workflow.md`、按需 `remote-ui/AGENTS.md` | 已基于现有 `TermxApp`、Native bridge、`RemoteControlApp` 和 terminal client 明确 App 连接 CLI remote runtime 的 TypeScript runtime/history contract；`remote-ui/AGENTS.md` 已允许 workflow 切片内维护 App/native runtime adapter contract，同时锁定 copy/history 必须走 core-v2 logical-line |
| R190. SK remote-ui core-v2 terminal protocol contract | 完成 | `remote-ui/`、按需 `termx-proto/` 生成产物只读参照、`internal/protocol/` 只读参照 | 已在 shared UI/runtime 层建立 core-v2 remote terminal/history method/event contract；旧 snapshot/scrollback API 被限定为 live display/cache，不再作为 copy/history 数据源 |
| R191. SK termx-app 连接 CLI remote runtime | 完成 | `termx-app/`、`remote-ui/`、按需 `termx-cli/`、`termx-remote/` | 已删除 App/remote-ui 旧 local status 私有 API caller，native 本地连接探测改走 CLI remote local/hub 的 `/api/v1/sessions/ice`，真实 App 继续复用 session token、native bridge、API/events/terminal datachannel 连接当前 remote runtime |
| R192. SK App terminal 管理与 live surface | 完成 | `termx-app/`、`remote-ui/`、按需 `termx-remote/`、`termx-core-v2/` | 已用 focused contract 证明 App terminal list/create/attach/input/resize/restart/remove 都经同一个 core-v2 runtime session 的 API/datachannel；live surface 只保留 xterm/snapshot/短 scrollback 显示缓存 |
| R193. SK App logical-line HistorySource | 完成 | `remote-ui/`、`termx-app/`、按需 `termx-proto/`、`internal/protocol/` | 已新增 core-v2 `HistorySource` adapter，经 `history.window` 返回 logical-line window、cursor/token/generation 和 cell/style footprint；不返回 xterm scrollback truth |
| R194. SK App infinite history surface/cache | 完成 | `remote-ui/`、`termx-app/`、只读参考 `termx-app-history-ref/` | 基于 `termx-app-history-ref` 的窗口化渲染、overscan、缓存和 renderer 思路实现 App history surface；App 可持有 render/cache window，但必须用 core-v2 cursor/token/generation 校验和失效 |
| R195. SK App copy/search/selection logical-line 化 | 完成 | `remote-ui/`、`termx-app/`、按需 `termx-testkit/` | App 复制模式、搜索和选择都从 logical-line history surface 组装文本；测试必须证明不会从 xterm selection、snapshot rows、DOM/canvas rows 或 App 本地 append log 返回最终 copy 文本 |
| R196. SK App 端到端 smoke | 完成 | `termx-app/`、`remote-ui/`、`termx-cli/`、`termx-core-v2/`、`termx-remote/`、`termx-testkit/` 按需 | 验证 CLI daemon/remote local enable -> App 配对/连接 -> terminal 创建/附着/输入输出 -> history rollback -> logical-line copy 的端到端路径 |
| R197. SK remote + App migration docs finalization | 完成 | `workflow.md`、`termx-cli/docs/v2-v3-switch-audit.md`、`remote-ui/docs/` 或必要顶层文档 | 更新最终迁移记录、App 连接方式、history/copy truth 边界、无限历史参考取舍和完整测试证据 |
| R198. SK 终端慢流不可能三角备忘 | 完成 | `workflow.md`、`terminal-live-stream-tradeoff.md` | 记录完整 PTY、本地不丢和不反压程序三者不能同时成立；后续实时展示回到 core latest screen，完整历史走 core logical-line history |
| R199. SK App local Hub JSON 响应诊断修复 | 完成 | `remote-ui/`、`termx-app/`、`termx-remote/localweb/static`、按需 `workflow.md` | 已修复 local/hub 连接中 `pair_...` pairing id 或损坏 runtime token 暴露 JSON.parse/invalid token 原始错误的问题；按真实 protobuf session token 解析 answer proof session id，缓存 token 失效会自动重新配对，配对后刷新 terminal inventory，并已用 Chrome 跑通 localweb 配对和打开 terminal |
| R200. SK localweb 终端打开链路收口 | 完成 | `remote-ui/`、`termx-app/`、`termx-remote/localweb/static`、`workflow.md` | 浏览器 localweb 打开 terminal 链路已收口：Web/Native session 生命周期统一为 `ManagedRtcSession`，浏览器 API datachannel lease close 不再关闭共享 session，raw API channel close 可恢复重建；桌面端 terminal body 固定在 grid 1fr 行避免 xterm 0 高，初始 untrusted fit 不会把 core PTY resize 成 1 行；Chrome 验收已看到输出并能输入回显 |
| R201A. SK TUI 前台程序鼠标滚轮透传 | 完成 | `termx-tui-v3/`、`workflow.md` | 已修复 Codex、Claude Code、opencode 等前台 TUI 已启用 terminal mouse tracking 时，raw 鼠标滚轮被 TermX 抢先进入无限历史的问题；前台 terminal 内容区优先透传 raw mouse，未启用 tracking 时才进入 TermX copy/history |
| R201B. SK core primary fullscreen 帧历史收口 | 完成 | `termx-core-v2/`、`workflow.md` | 已修复 Codex 这类不进 alt-screen、但在 primary screen 上反复 `CSI H/J` 全屏刷新的程序被当成普通滚动历史累计的问题；进入 fullscreen 时保留前一页，后续 fullscreen 帧只替换 mutable frame，copy/history 不展示每一帧刷新日志 |
| R201C. SK core primary fullscreen 运行帧排除 | 完成 | `termx-core-v2/`、`workflow.md` | 已修复 Codex 运行中的 primary fullscreen UI frame 被当成 committed 滚动历史重复累计的问题；运行帧不提交 committed history，后续由 R201F 收口为 mutable current frame 可见 |
| R201D. SK TUI history styled 宽字符渲染收口 | 完成 | `termx-tui-v3/`、`workflow.md` | 已修复 TUI copy/history 渲染 Codex 日志时 styled compact run 内中文宽度被当成 1 列，导致 ANSI 列锚点回退、背景块覆盖、底部文本缺字和宽度错乱的问题；history 仍只消费 core-v2 logical-line window |
| R201E. SK Codex history TTY 帧回归收口 | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`workflow.md` | 已修复 Codex primary-screen TUI 在运行中输入框/运行帧进入 copy/history，以及真实 TTY/tmux 下 copy/history 增量绘制背景块和行尾清理不等价的问题；运行帧只在 force commit/进程退出时提交，TUI FrameSink 对带绝对列定位的 ANSI 行先清整行再写 |
| R201F. SK primary fullscreen 当前帧历史可见 | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`workflow.md` | 已修复 Codex primary-screen TUI 在 copy/history 模式中底部当前帧行不可见的问题；running fullscreen frame 作为 mutable current frame 出现在 latest/frozen window，但不计入 committed history depth，重复 repaint 仍只保留最新帧 |
| R201G. SK daemon remote local 启动失败降级 | 完成 | `termx-cli/`、`workflow.md` | 已修复 remote local 配置绑定到当前机器不存在地址时，默认 core-v2 daemon auto-start 直接退出，CLI 只看到等待 socket 超时的问题；remote local auto-enable 失败会降级记录日志，core daemon socket 继续启动 |
| R201H. SK Codex tmux raw 输出历史回放 | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`workflow.md` | 已基于 tmux pipe-pane/capture-pane dump 复现 Codex primary-screen 输出真实序列；core-v2 现在识别同步输出/focus tracking 作为 primary TUI intent，intent 下 ED0 可在非首行启动或替换 mutable fullscreen frame |
| R201I. SK core-v2 history semantic ingest 设计 | 完成 | `termx-core-v2/docs/`、`workflow.md` | 已新增 history semantic ingest 设计文档，明确后续不再给 `historyANSIParser` 叠终端语义补丁；core 里已有 vterm 应作为 EventRouter 唯一 semantic source，共用同一批 PTY 解码 damage，但 history 只能消费语义事件并写 `HistoryTrack` logical-line truth |
| R201J. SK core-v2 shared vterm semantic batch 边界 | 完成 | `termx-core-v2/`、`workflow.md` | 已把 `Terminal` 输出入口改成 live surface 持有的同一个 vterm write transaction 产出 semantic batch，history queue 消费该 batch；真实 process 路径不再把 PTY bytes 同时送进 live vterm 和独立 history vterm/altCap 捕获，`FlushHistory` 先等 live batch 生成再等 history 落库；本切片只建立 EventRouter/shared batch 边界，`historyANSIParser` 的终端语义 projector 收缩留给 R201K |
| R201K. SK core-v2 vterm damage history projector | 完成 | `termx-core-v2/`、`termx-vterm/`、按需 `termx-tui-v3/`、`workflow.md` | 已接入 shared vterm damage/control/mode projector：primary scroll-out 只通过 screen ownership 提交 logical line，damage-only batch 可转换 write/clear/scroll/control/mode `HistoryEvent`；raw parser 收缩为文本/style/OSC8 和迁移期控制辅助，覆盖 rows=1/2 普通输出、Codex raw repaint signal、styled blank、alt-screen final frame |
| R201L. SK core-v2 raw 控制语义 vterm 化 | 完成 | `termx-core-v2/`、`termx-vterm/`、`workflow.md` | 已补齐 vterm 基础 cursor control damage，并让 core damage-only projector 消费 CUU/CUD/CUF/CUB/CHA/CUP/VPA；真实 vterm damage harness 证明 cursor overwrite 可不经 parser 投影，parser 仍保留文本/style/OSC8 与缺少 byte-offset ordered event 的迁移期辅助 |
| R201M. SK vterm ordered semantic ops 边界 | 完成 | `termx-core-v2/`、`termx-vterm/`、`workflow.md` | 已从 `WriteDamage.Ops` 的 screen-diff 职责中拆出 `SemanticOps`，vterm 同一 write transaction 明确携带文本写入、控制、mode 的有序语义序列；core history projector 优先消费 semantic ops，避免用 screen diff 顺序猜 history 事件 |
| R201N. SK vterm semantic clear 边界 | 完成 | `termx-core-v2/`、`termx-vterm/`、`workflow.md` | 已修正 `SemanticOps` 只承载真实终端语义 clear/control/mode/text，不把 scroll 后屏幕填充、diff clear 或 full-replace screen cleanup 伪装成 history semantic clear |
| R201O. SK raw shared-vterm history projector 收口 | 完成 | `termx-core-v2/`、`termx-vterm/`、`workflow.md` | 已把 raw shared-vterm inline 编辑类 batch 切到同批 ordered `SemanticOps`：backspace/tab/cursor/EL 与文本按 vterm 顺序投影，不再让 parser 重放这些终端控制；纯文本/SGR/OSC、scroll-out、alt-screen 和 full-replace 仍保留 parser/现有 projector 迁移辅助 |
| R201P. SK vterm 文本语义 damage 边界 | 完成 | `termx-vterm/`、`termx-core-v2/`、`workflow.md` | 已让 vterm 在真实 print 路径输出独立 `TextDamage`，`SemanticOps` 文本只来自 print path，不再从 screen diff span 派生；screen diff span、clear fill、scroll fill 仍只服务 live/damage，不作为 history 文本语义 |
| R201Q. SK raw 文本语义 shadow parser 边界 | 完成 | `termx-core-v2/`、按需 `termx-vterm/`、`workflow.md` | 已允许安全纯文本/SGR/OSC raw batch 使用 vterm `TextDamage` projector，同时让 parser 只 shadow 更新 pending/style/OSC 状态，不再向 HistoryTrack 重放文本或终端控制 |
| R201R. SK vterm styled erase 语义边界 | 完成 | `termx-vterm/`、`termx-core-v2/`、`workflow.md` | 已让 vterm 在真实 EL clear 语义里携带当前 SGR 背景 footprint，core-v2 projector 直接消费 plain/styled EL，不再为了 styled blank 回退 raw parser；ED/scroll/alt/full-replace 仍留给后续语义切片 |
| R201S. SK raw mode 语义 vterm 化 | 完成 | `termx-core-v2/`、`termx-vterm/`、`workflow.md` | 已让 raw shared batch 中的 vterm `ScreenOpModes` 直接驱动 primary fullscreen intent 和 mode-only alt-screen 边界，parser 只 shadow pending/style/OSC 状态，不再重放 private mode CSI；带 alt 内容/final-frame、ED、scroll-out 仍留给后续语义切片 |
| R201T. SK raw ED 语义 vterm 化 | 完成 | `termx-core-v2/`、`termx-vterm/`、`workflow.md` | 已让 raw shared batch 中的 vterm `ed` control 直接驱动 `EventEraseInDisplay`，Codex-style mode/CUP/ED0 repaint 不再回退 parser；scroll-out、alt final-frame 和 full-replace 仍留给后续切片 |
| R201U. SK raw primary scroll-out vterm 化 | 完成 | `termx-core-v2/`、`termx-vterm/`、`workflow.md` | 已让 rows=1/2 raw shared batch 中的 primary `ScrollbackAppend`、向上 `ScrollRect` 和 LF/IND control 直接驱动 HistoryTrack screen ownership 提交；大高度多 scroll、RI、alt final-frame 和 full-replace 仍留给后续切片 |
| R201V. SK scroll-region/RI 语义 vterm 化 | 完成 | `termx-core-v2/`、`termx-vterm/`、`workflow.md` | 已让 vterm 暴露 DECSTBM scroll-region semantic control，并让 raw shared batch 中的 scroll-region、RI down-scroll 和 absolute cursor 由 vterm semantic ops 驱动；Codex-style 局部滚动不会破坏已 committed pre-existing lines，alt final-frame 和 full-replace 仍留给后续切片 |
| R201W. SK alt final-frame shared vterm 化 | 完成 | `termx-core-v2/`、`workflow.md` | 已删除 history pipeline 的 altCap 第二套 vterm，alt final-frame 只从真实 shared live vterm `WriteWithResult` semantic batch 进入 history，alt-screen 运行中不写 primary history、退出 final frame 只追加一次；full-replace 和 parser 职责收缩仍留给后续切片 |
| R201X. SK full-replace history 边界 vterm 化 | 完成 | `termx-core-v2/`、`workflow.md` | 已让 shared vterm `RequiresFullReplace` 只作为 live/stale 边界，不让 full-replace-only raw batch 触发 parser history append；带 raw 的 full-replace text semantic ops 仍需后续证明 ownership/alt 边界后再放开 |
| R201Y. SK 低高度 multi-scroll vterm 化 | 完成 | `termx-core-v2/`、`termx-vterm/`、`workflow.md` | 已让 rows=1/2 普通 primary raw batch 中的多次 LF/IND、自动换行 soft-wrap、多个 primary scrollback append 和向上 scroll rect 直接走 vterm semantic projector；vterm 明确区分自动换行 soft-wrap 与显式 IND，core-v2 不再回退 parser 且不丢不重复 |
| R201Z. SK 大高度 primary scroll-out vterm 化 | 完成 | `termx-core-v2/`、`workflow.md` | 已让 rows>2 普通 primary raw batch 中的 plain LF/IND/soft-wrap、primary scrollback append 和向上 scroll rect 直接走 shared vterm semantic projector；样式/link/宽字符 footprint 仍留给后续专门切片，避免把未建模语义抢进 projector |
| R201AA. SK ASCII SGR/OSC8 文本语义 vterm 化 | 完成 | `termx-core-v2/`、`workflow.md` | 已让 rows>2 的单批 ASCII SGR 与 OSC8 link 文本加 hard newline 直接由 shared vterm semantic text cells 投影；parser 只 shadow pending 状态，不再重放这些文本/样式语义 |
| R201AB. SK 宽字符/combining 文本语义 vterm 化 | 完成 | `termx-core-v2/`、`workflow.md` | 已让 rows>2 的单批宽字符和 combining grapheme 文本加 hard newline 直接由 shared vterm semantic text cells 投影；保持 logical cell width/style/link，不回退 parser |
| R202. SK Web 桌面 terminal 可视区修复 | 待开始 | `remote-ui/`、`termx-remote/localweb/static`、`workflow.md` | 修复 Web 桌面状态右侧 terminal 内容不可见、移动端可见的问题；桌面断点 terminal body 必须占据唯一 1fr 行，Chrome 验收需证明桌面宽度可见并可输入回显 |

## 6. 测试准入

每个有效切片提交前，至少跑和改动范围相符的测试：

- 文档-only 改动：`git diff --check`
- core-v2 改动：`cd termx-core-v2 && go test ./... -count=1`
- remote 改动：`cd termx-remote && go test ./... -count=1`
- CLI remote 改动：`cd termx-cli && go test ./cmd/termx -count=1`
- protocol 改动：`cd internal && go test ./protocol/... -count=1`
- proto 改动：`cd termx-proto && go test ./... -count=1`
- `.proto` 改动：必须同步更新对应生成文件；如果本机缺少 `protoc` 或 `protoc-gen-go`，不得手写生成文件，当前切片应标 `阻塞` 并写清缺口。
- remote-ui 改动：`cd remote-ui && npm run typecheck && npm run test`；涉及 public entry、terminal renderer、protocol 或打包行为时加跑 `npm run build`
- termx-app 改动：`cd termx-app && npm run build`
- termx-app native/Capacitor 改动：在 App build 外按需跑 `cd termx-app && npm run cap:sync`；若本机缺 Android/Capacitor 环境，最终说明必须写清
- App history/copy 改动：必须有 focused harness 证明 copy/search/selection 使用 core-v2 logical-line source，不从 xterm/snapshot/DOM/cache 拼最终文本
- 默认入口或依赖边界改动：`cd termx-cli && go test ./cmd/termx -run TestDefaultRuntimeSourceDoesNotImportLegacyCoreOrTUI -count=1`
- remote fallback/旧依赖清理改动：除 CLI 测试外，必须确认 `termx-cli/cmd/termx/remote_*.go` 不再 import `termx-core` 或 `tuiv2`，且默认依赖守卫不再把 remote 文件作为旧依赖豁免。
- 跨模块迁移改动：按需加跑 `make test-v2-migration`
- CLI 编译入口相关改动：按需确认 `go run ./termx-cli/cmd/termx --help` 能编译运行

如果测试无法运行，最终说明必须写清原因。

## 7. 自动推进和提交规则

- 每次开始工作先读本文件，再跑 `git status --short --branch`。
- 只执行任务队列里最早未完成的切片。
- 如果最早未完成切片是 `阻塞`，必须停下说明原因，不能跳到后面。
- 如果最早未完成切片是 `待开始`，先把它标成 `进行中`，再开始做。
- 一个小阶段收口后，更新本文件状态和当前状态说明，跑准入，提交一个 `SK:` 中文提交。
- 不要长期堆未提交改动，也不要把多个小阶段硬塞进一个提交。
- 不得 amend commit，除非用户明确要求。
- 不得覆盖用户或其他代理的未提交改动；如果冲突，先停下说明。

## 8. 当前状态

- 当前分支已经完成本地默认入口 v2/v3 切换；最近收口提交为 `c9d133d4 SK: 修复启动重复附件计数`。
- `R173` 已完成：`AGENTS.md` 明确 remote 迁移不能退回旧 daemon/TUI，`workflow.md` 已压缩为 remote 迁移队列。
- `R173B` 已完成：禁止补丁式实现成为 AGENTS 与 workflow 的硬准入，后续 remote 切片必须先讲清状态归属和 contract。
- `R173C` 已完成：remote/protocol 迁移只面向 core-v2 contract，不对旧 core 保留兼容层或 fallback。
- `R174` 已完成：已扫描 CLI remote、`termx-remote.Service`、core-v2 protocol/server/storage/events、旧 core extension/transport 源边界，并把迁移队列细化到 R175-R188。
- `R175` 已完成：已按用户确认删除旧 `termx-core/` 与 `tuiv2/`，清理 legacy/fallback 入口和构建引用，压缩 CLI 切换审计文档。
- `R175B` 已完成：已扫描 `termx-app/`、`remote-ui/`、`termx-app-history-ref/`，并把工作流扩展为 remote backend + 真实 App 端到端；App 本地历史只允许作为显示/cache，copy/history truth 必须走 core-v2 logical-line。
- `termx remote ...` 后端已迁到 core-v2 remote service hook、runtime API 和 scoped transport；旧 fallback 已删除。
- `R176` 已完成：`termx-proto/wirepb/terminal.pb.go` 已与 `terminal.proto` 的 core-v2 logical-line history/window 字段对齐，并新增 descriptor contract test 防止生成文件再次落后。
- `R177` 已完成：`internal/protocol` remote codec 已切到显式 `protocol.Remote*` domain contract，参数/结果不再接受 getter、任意 struct 反射或 `wirepb` 指针作为业务类型；准入 `cd internal && go test ./protocol/... -count=1` 已通过。
- `R178` 已完成：`TerminalRecord`/`ProcessSpec` 已承接 `Dir`、`Env`、`Scrollback*`，protocol create 不再丢这些字段；core-v2 list/get 暴露创建 CWD，PTY spawn 使用 create dir/env，restart 复用原 create options；准入 `cd termx-core-v2 && go test ./... -count=1` 与 `cd internal && go test ./protocol/... -count=1` 已通过。
- `R179` 已完成：core-v2 已公开 `ServeTransport` / `ServeScopedTransport` / `TransportScope`；terminal scope 会约束 terminal-bound method、stream channel 并把空事件订阅收窄到目标 terminal，machine-events-only scope 只允许 terminal 事件流；focused scope harness 已通过，完整 core-v2/protocol 准入在本切片提交前运行。
- `R180` 已完成：core-v2 新增 typed `RemoteService` hook 与 `WithRemoteService` 注入点，protocol dispatch 已承接 `remote.status`、`remote.pair.start`、`remote.local.enable/status/disable`；未配置 remote service 时返回明确 unavailable 错误，fake harness 证明请求参数和结果都走 `protocol.Remote*` domain 类型。
- `R181` 已完成：新增 core-v2 remote daemon adapter，覆盖 terminal create/list/get/metadata/restart/remove、storage get/put/delete/list、events 和 `ServeScopedTransport`；core-v2 支持构造后 `SetRemoteService` 注入真实 remote hook，CLI remote client 只向 daemon 发送 `internal/protocol.Remote*` typed payload；remote create 的 env/retention 字段完整传入 core-v2 create params；准入 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-remote && go test ./... -count=1`、`cd termx-cli && go test ./cmd/termx -count=1`、默认依赖守卫与 `git diff --check` 已通过。
- `R182` 已完成：默认 `termx daemon` 会加载 remote config，基于同一个 core-v2 server 构造 `termx-remote.Service`，注入 typed remote hook，随 daemon context start/close，并在 local/both 模式下按 config/env 自动 `LocalEnable`；focused harness 证明 hook 注入、local auto-enable、close 清理和 daemon env 配置装配。准入 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-remote && go test ./... -count=1`、`cd termx-cli && go test ./cmd/termx -count=1`、默认依赖守卫与 `git diff --check` 已通过。
- `R183` 已完成：新增 remote config path helper，默认 `daemon` 与 `v3 daemon` 都读取 root `--config` 指定的 remote 配置；`remote status/local/pair/open` 等 remote CLI 命令通过 context 把显式 config path 传给 core-v2 daemon auto-start；config bootstrap 也统一走 remote path helper。准入 `cd termx-cli && go test ./cmd/termx -count=1`、默认依赖守卫与 `git diff --check` 已通过。
- `R184` 已完成：新增真实 CLI/core-v2 remote smoke，启动 core-v2 server，注入真实 `termx-remote.Service`，通过 Cobra `termx remote status`、`remote enable --mode local`、`remote pair --json`、`remote disable --json` 验证 status/local/pair 请求经 daemon socket 到达 remote service，并确认 disable 后 core-v2 hook 的 `remote.local.status` 已关闭。准入 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-remote && go test ./... -count=1`、`cd termx-cli && go test ./cmd/termx -count=1`、默认依赖守卫与 `git diff --check` 已通过。
- `R185` 已完成：`termx-remote.Service` 新增 runtime API 路由入口，terminal management、storage 和 events subscription 都通过 core-v2 daemon adapter 访问 truth；focused harness 覆盖 terminal create/list/get_directory/set_metadata/restart/remove、storage put/get/list/delete 和 terminal created/metadata/removed events，remote create 的 ID 生成也收敛到 core-v2 adapter 边界。准入 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-remote && go test ./... -count=1`、`cd termx-cli && go test ./cmd/termx -count=1`、默认依赖守卫、remote 旧 import 检查与 `git diff --check` 已通过。
- `R186` 已完成：`termx-remote.Service` 成为真实 runtime manager 的 transport sink，terminal datachannel 和新的 `machine-events` protocol datachannel 都经 Service 路由到 core-v2 scoped protocol session；focused harness 覆盖 `webrtc:terminal:<machine>:<terminal>` 只允许目标 terminal、`webrtc:machine-events` 拒绝 terminal/storage 方法但允许 terminal lifecycle/metadata events，RTC offer handler 也验证 `machine-events` datachannel 会进入 transport sink。准入 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-remote && go test ./... -count=1`、`cd termx-cli && go test ./cmd/termx -count=1`、默认依赖守卫、remote 旧 import 检查与 `git diff --check` 已通过。
- `R187` 已完成：新增 remote backend contract smoke，启动真实 core-v2 daemon socket 并注入真实 `termx-remote.Service` hook，串联验证 `remote.status`、`remote.local.status/enable/disable`、`remote.pair.start`、runtime API terminal create/storage put/events、remote service scoped transport 都落到 core-v2 truth；同时守卫旧 `termx-core/`、`tuiv2/`、`remote_runtime.go`、`remote_protocol_codec.go`、`legacy_commands.go` 不得恢复。准入 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-remote && go test ./... -count=1`、`cd termx-cli && go test ./cmd/termx -count=1`、默认依赖守卫、remote 旧 import 检查与 `git diff --check` 已通过。
- `R188` 已完成：`termx-cli/docs/v2-v3-switch-audit.md` 已更新为 remote 后端迁移完成 checkpoint，记录 `termx remote ...` 只连 core-v2 daemon、status/local/pair 经 typed hook、runtime API terminal/storage/events 与 WebRTC/datachannel transport 经 `termx-remote.Service` 路由到 core-v2 truth、旧 fallback 不得恢复，并把 App/remote-ui history/copy 边界写为当时的后续阶段。准入 `git diff --check` 已通过。
- `R189-R197` 已完成：真实 `termx-app/` 与 `remote-ui/` 已建立 CLI remote runtime 连接方式、terminal management/live surface、logical-line `CoreV2HistorySource`、infinite history surface/cache、logical-line copy/search/selection 和 App end-to-end smoke；迁移文档已记录最终边界和测试证据。
- `R198` 已完成：`terminal-live-stream-tradeoff.md` 已记录终端慢消费者不可能三角，明确最新屏用于实时展示，完整历史必须走 core-v2 logical-line history，客户端本地 scrollback 只允许作为缓存。
- `R199` 已完成：App/local web 会把缓存里的 `pair_...` pairing session id 视为错误 runtime token 并清理；损坏或服务端拒绝的 runtime token 会走 auth failure，清缓存并打开重新配对；Hub 成功响应若不是 JSON 会带 endpoint/status/body preview；answer-proof 验证按 Go `tokenpb.Claims` protobuf payload 读取 `session_id`，不再把合法 `session_token` 当 JSON 解析失败；配对成功后会刷新 terminal inventory。准入已通过 `remote-ui` focused tests/typecheck/test/build:localweb、`termx-app` build、`termx-remote` 全量 Go tests、CLI remote focused tests、`git diff --check`，并用 Chrome DevTools Protocol 跑通隔离 local runtime：坏 token -> 重新配对 -> terminal inventory 刷新 -> 打开 `browser-smoke-final`，页面无 `Unexpected token` 或 `invalid session token`。
- `R200` 已完成：`remote-ui`/`termx-app` 现在共享 `ManagedRtcSession` 生命周期接口，Browser/Native 两套实现都必须提供 `subscribeConnectionState`、`onDisconnect`、`isAlive`、`handleAppResume`、`waitUntilConnected` 和 `closeTerminalDataChannel`；浏览器 API datachannel lease close 不再关闭底层共享 channel，raw API channel close 只清缓存并按需重建，不再误判为整条 WebRTC session 失败。localweb 桌面端 terminal header/body/keybar 使用显式 grid row，body 固定在 1fr 行并保持 `h-full`，解决 `md:hidden` header 让 xterm 落入 auto 行导致 0 高的问题；初始 1-row fit 仍被拦截，避免 core PTY 被压扁。准入已通过 `remote-ui` typecheck/test/build:localweb、`termx-app` build、`git diff --check`；Chrome DevTools Protocol 隔离验证 `http://127.0.0.1:58955/localweb.html`：配对 -> 打开 `r200-final`，桌面截图可见 `R200_FINAL_READY` 和 `R200_FINAL_ECHO:keyevent...`，terminal rect 约 `886x912`，无 `session_failed`、`connect_failed`、`Unexpected token` 或 `browser WebRTC api channel closed`。
- `R201A` 已完成：TUI 鼠标路由现在在 runtime 命中测试确认 active terminal 内容区且前台程序启用 mouse tracking 时，为 raw mouse 标记 terminal passthrough；UI/copy reducer 避让，terminal input router 发送 raw seq 给子进程。未启用 tracking 的滚轮仍进入 authoritative logical-line copy/history，已通过 `cd termx-tui-v3 && go test ./... -count=1`。
- `R201B` 已完成：core-v2 history 现在把 primary-screen 前台 TUI 的控制序列拆成 fullscreen intent 与可替换 mutable frame；第一次 home-clear 仍按 page-break 提交进入前的 shell 页面，后续 repeated home-clear 只 reset 当前 fullscreen frame，不再把 Codex repaint 帧累计到 committed history。准入已通过 focused history/terminal ingest tests、`cd termx-core-v2 && go test ./... -count=1`、`git diff --check -- ...`。
- `R201C` 已完成：运行中的 primary fullscreen frame 不再作为 committed 滚动历史重复累计，因此 Codex 输入框、footer、局部 repaint UI 不会被滚进 TermX 无限历史；进入 fullscreen 前的 shell/logical-line 历史仍保留，process exit/force commit 仍会把最终帧写入历史。R201F 已把最终展示语义调整为 mutable current frame 可见但不计入 committed depth。准入已通过 focused history/protocol/terminal 回归、`cd termx-core-v2 && go test ./... -count=1`、`git diff --check -- ...`。
- `R201D` 已完成：TUI protocol history adapter 现在按 grapheme display width 解析 styled compact run，copy/history renderer 增加 styled 宽字符 ANSI 锚点回归，避免中文日志行按 1 列推进造成背景块覆盖、缺字和底部宽度错乱。准入已通过 `cd termx-tui-v3 && go test ./... -count=1`、`git diff --check -- ...`；已有 remote-ui/localweb 未提交改动未纳入本切片。
- `R201E` 已完成：core-v2 在 primary fullscreen frame 运行期间不会因为 cursor/mouse mode 退出或 frame 内换行把 Codex 输入框、footer、运行帧暴露到 latest/copy history；普通 commit 对运行帧 no-op，process exit/force commit 仍会提交最终帧。tui-v3 FrameSink 在真实 TTY patch 擦除前先 `ANSIReset`，对含 `CSI G/H/f/C/D/X` 的 ANSI addressed 行改为 reset+clear-line+rewrite，避免 StringWidth 补空格导致黑块、旧尾巴和底部缺字。准入已通过 focused core/FrameSink 回归、`cd termx-core-v2 && go test ./... -count=1`、`cd termx-tui-v3 && go test ./... -count=1`、tmux 最小 ANSI capture 验证和 `git diff --check -- ...`；已有 remote-ui/localweb 未提交改动未纳入本切片。
- `R201F` 已完成：core-v2 latest/frozen snapshot 重新投影 running primary fullscreen mutable frontier，让 Codex 当前输入/状态底行进入 copy/history 时仍可见；这些行保持 `Committed=false`，`TotalLines/LogicalTotal` 仍只计算 committed history，重复 repaint 只保留最新 frame，process exit/force commit 仍提交最终帧。tui-v3 同步夹紧 copy/history 全量和 patch cursor 的可见行，避免 cursor row/rect 越过 content viewport。准入已通过 focused core/protocol/TUI cursor 回归、`cd termx-core-v2 && go test ./... -count=1`、`cd termx-tui-v3 && go test ./... -count=1`、`git diff --check -- ...`。
- `R201G` 已完成：`go run ./termx-cli/cmd/termx ls` 的 daemon auto-start timeout 根因是 remote local 配置尝试监听当前机器不存在的 `192.168.0.103:18888`，daemon 启动时把 LocalEnable 错误当硬失败并退出。现在 remote service `Start` 仍是硬失败，但 daemon startup 内的 local auto-enable 失败只写 warning，remote hook 保持安装，core daemon socket 继续启动；真实坏地址验证已看到 `ls` 返回成功、日志记录 auto-enable warning 和 daemon ready。准入已通过 focused runtime harness、`cd termx-cli && go test ./cmd/termx -count=1`、`git diff --check -- termx-cli workflow.md`。
- `R201H` 已完成：tmux raw dump 显示真实 Codex 0.141.0 没有进入 alt-screen，也不是单纯 `CSI H/J` repaint；它使用 `CSI ?2026h/l` 同步输出、`CSI ?1004h` focus tracking、`CSI 5;48r`/`CSI 1;11r` 滚动区域、连续 `ESC M` reverse-index，以及局部绝对定位绘制输入框和底部状态行。core-v2 replay harness 已固化该序列：进入 Codex 前的 shell 行会 page-break 提交，当前输入/状态底行作为 mutable frame 可见，第二帧 repaint 会替换第一帧输入框且不增加 committed history depth。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-tui-v3 && go test ./... -count=1`、`git diff --check -- termx-core-v2 workflow.md`。
- `R201I` 已完成：`termx-core-v2/docs/history-semantic-ingest.md` 记录后续 history ingest 的设计基准：不再按进程名特判或继续扩写手写 parser；真实 PTY bytes 只由 core 已持有的 vterm 解码一次，EventRouter 分发同一批 semantic damage 给 live/history；vterm scrollback、live snapshot、damage rows 不能成为 history truth，最终必须转换成 `HistoryEvent` 写入 `HistoryTrack` logical-line 模型。文档同时明确 Codex primary scrollback/context 与 current-frame repaint 要分开处理，opencode/alt-screen 运行中不写 primary history。准入 `git diff --check` 已通过。
- `R201J` 已完成：`live.SurfaceTrack.WriteWithResult` 现在返回同一次 vterm `WriteWithDamage` 的 semantic damage，`Terminal.IngestOutput` 和真实 process output 都把 shared vterm batch 投给 history pipeline；history queue 支持 semantic batch，`FlushHistory` 先 flush live queue 再 flush history queue，避免 copy/history 冻结漏掉已读但尚未转成 batch 的 PTY 输出。shared batch 路径禁止旧 altCap 二次捕获 final frame，alt-screen final frame 只追加一次。准入已通过 `cd termx-core-v2 && go test ./... -count=1`。
- `R201K` 已完成：history queue 现在接收含 damage 的 shared vterm batch，projector 统计并消费 vterm write/clear/scroll/control/mode 语义；新增 `EventPrimaryScrollOut`，只用 vterm primary scrollback append 证明“有行离开 screen ownership”，最终提交仍由 `HistoryTrack` 按 logical line ownership 完成，不能把 vterm scrollback row 或 screen diff 当 history truth。`termx-vterm` 同批补出 CR/LF/IND/RI、ED/EL 和 mode change damage，full-replace 分支也保留这些语义 ops；raw parser 仍作为文本/style/OSC8 与迁移期控制辅助，不再扩大成新的终端语义补丁。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-vterm && go test ./... -count=1`。
- `R201L` 已完成：`termx-vterm` 现在为 CUU/CUD/CUF/CUB/CNL/CPL/CHA/HPA/VPA/CUP/HVP 输出 ordered control damage，core-v2 damage projector 将这些 control ops 映射到已有 cursor `HistoryEvent`；focused harness 使用真实 vterm `WriteWithDamage` 证明 `abcdef CSI 3D XYZ` 可只靠 damage projector 得到 `abcXYZ`，不依赖 raw parser 控制语义。当前仍未把 raw shared-vterm path 全量切到 vterm projector，因为 vterm damage 尚未携带 PTY byte offset/完整 ordered semantic text event；不能用 screen diff 猜测 parser 文本顺序。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-vterm && go test ./... -count=1`、`git diff --check`。
- `R201M` 已完成：`termx-vterm.WriteDamage` 新增 `SemanticOps`，direct write/clear/scroll/control/mode 会作为有序语义序列输出，broad/repeated full-replace screen diff 不会伪装成 semantic text；full-replace 中的 control/mode 语义保留在 `SemanticOps`。core-v2 live surface 不会丢弃 semantic-only damage，history projector 优先消费 `SemanticOps`，没有时才回退旧 `Ops`。focused harness 证明 screen diff `Ops` 与 semantic ops 冲突时 history 只消费 semantic ops。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-vterm && go test ./... -count=1`、`git diff --check`。
- `R201N` 已完成：`SemanticOps` 不再从 screen diff clear 派生，真实 EL/ED 只能通过 vterm `ControlDamage` 的 `el/ed` 进入 semantic stream；focused harness 证明 screen diff clear 仍保留给 live/screen update，但 history semantic projector 只消费 `el` control，不把 scroll/fill clear 当成 erase。Codex raw damage 统计同步改为不要求 clear diff 进入 semantic ops。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-vterm && go test ./... -count=1`、`git diff --check`。
- `R201O` 已完成：vterm 现在为裸 backspace 与 tab 输出 ordered control damage，`SemanticOps` 可覆盖 `write -> bs/ht/cursor/EL -> write` 这类 raw inline 编辑事务；core-v2 raw shared batch 在确认只包含安全 inline semantic ops 且无 scrollback/alt/full-replace 时直接走 vterm projector，避免 parser 重新执行终端控制。纯文本/SGR/OSC8 跨 chunk 样式、scroll-out、alt-screen final frame、fullscreen/full-replace 仍按现有迁移辅助路径处理，后续不能用 screen diff 猜 history truth。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-vterm && go test ./... -count=1`、`git diff --check`。
- `R201P` 已完成：vterm 内部新增 `TextDamage`，只由真实 ASCII/grapheme print path 产生；screen `SpanDamage` 继续服务 live diff，styled erase/fill 不会被伪装成文本语义。`WriteDamage.SemanticOps` 的 text span 现在来自 `TextDamage`，full-replace 分支也不会从 screen diff 推导文本；focused harness 覆盖 print text 样式、ordered write/control 语义、broad/repeated full-replace 不产生 semantic text、wide rune screen span 不回归。core raw shortcut 仍不放开纯文本/SGR/OSC 跨 chunk 和 styled EL，等待后续切片处理 pending/style/control 完整边界。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-vterm && go test ./... -count=1`、`git diff --check`。
- `R201Q` 已完成：core-v2 raw shared batch 在仅包含 vterm text semantic ops 与允许的 inline cursor/backspace/tab/control 时，先 shadow parse raw 以维护 parser pending/style/OSC 辅助状态，再只把 vterm `SemanticOps` 写入 `HistoryTrack`；纯文本/SGR 跨 chunk 已可由 vterm text cells 投影，parser 不再重放文本。scroll-out、alt-screen、full-replace 和 styled EL 仍走现有迁移辅助，避免把 screen diff/fill 当 history truth。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-vterm && go test ./... -count=1`、`git diff --check`。
- `R201Y` 已完成：vterm semantic ops 明确区分自动换行 `soft-wrap` 与显式 `IND`，core-v2 `HistoryTrack` 用 `EventSoftWrapLine` 表达 visual row ownership 前进但 logical line 仍 open；rows=1/2 同一 raw batch 内多次 LF/soft-wrap/scrollback append 可直接走 vterm semantic projector，不再回退 parser，且普通 primary 输出不丢不重复。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`cd termx-vterm && go test ./... -count=1`、`git diff --check`。
- `R201Z` 已完成：rows>2 的普通 plain primary scroll-out raw batch 现在可直接使用 shared vterm semantic projector，vterm 的 primary scrollback append 只证明 screen ownership 离开，最终仍由 `HistoryTrack` 合并 logical-line history；focused harness 证明 semantic projector 命中、`RawFallbacks == 0`、plain 行不丢不重复。样式、OSC8 link、宽字符/combining 和 styled tail footprint 仍由后续专门语义切片收口，避免在本切片误把未建模 footprint 交给 projector。准入已通过 `cd termx-core-v2 && go test ./... -count=1`。
- `R201AA` 已完成：vterm semantic text cells 已承接 rows>2 单批 ASCII SGR/OSC8 hard-newline 输出，focused harness 证明 styled/link raw batch 使用 semantic projector、`RawFallbacks == 0`，红色 SGR、OSC8 link metadata 与后续 plain line 都来自 shared vterm cells；HistoryTrack append 路径会合并相邻同 style/link ASCII run，避免 vterm 逐 cell text damage 改变 logical cell run 形态，overwrite 路径仍保持精确列替换。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `R201AB` 已完成：vterm semantic text cells 已承接 rows>2 单批 combining grapheme 和宽字符 hard-newline 输出，projector 会在同一 damage 内聚合连续 write span，并把跨 span 的零宽 combining mark 合并到前一个 logical cell；focused harness 证明 `é好` 使用 semantic projector、`RawFallbacks == 0`，窄列投影仍保持 combining grapheme 与宽字符边界。准入已通过 `cd termx-core-v2 && go test ./... -count=1`、`git diff --check`。
- `R202` 待开始：Web 桌面 terminal 可视区修复保留在后续切片；已有 remote-ui/localweb 未提交改动不纳入当前 core-v2 semantic ingest 切片。
- `termx-remote-v2/` 当前是未跟踪目录，本工作流默认不触碰。
- `termx-app-history-ref/` 当前是未跟踪本地参考目录，本工作流只读参考，不纳入提交内容，除非后续切片明确要求。
- 当前已知环境缺口：本机没有 Android SDK，`termx-app/android/local.properties` 指向的 `/Users/lozzow/Library/Android/sdk` 不存在；OpenJDK 21 已通过 Homebrew 安装。Android/Kotlin 编译未作为当前 checkpoint 的通过条件。
- 额外检查记录：R177 已把 `Client.Detach` 纳入 protocol public boundary 守卫，避免现有公开方法继续造成 protocol 准入失败。
