# v2/v3 默认入口切换审计与迁移矩阵

本文档是切片 11 的产物，用于约束后续把 `termx-cli` 默认运行时从旧 `termx-core` + `tuiv2` 切到 `termx-core-v2` + `termx-tui-v3` 的执行顺序、能力边界和验收口径。

## 1. 当前结论

当前 `go run ./termx-cli/cmd/termx` 仍使用旧链路：

| 入口 | 当前实现 | 证据 | 迁移目标 |
| --- | --- | --- | --- |
| `termx` root | 调用 `runTUIv2` | `termx-cli/cmd/termx/main.go` | 切片 25 后默认调用 tui-v3 |
| `termx attach <id>` | 调用 `runTUIv2` | `termx-cli/cmd/termx/tui_launcher.go` | 切片 26 后默认调用 tui-v3 attach |
| `termx daemon` | 调用旧 `termx-core.NewServer` | `termx-cli/cmd/termx/daemon_command.go` | 切片 26 后默认调用 core-v2 server |
| `termx new/ls/kill/rm` | 通过 `internal/protocol.Client` 自动连接或启动旧 daemon | `termx-cli/cmd/termx/terminal_commands.go`、`termx-cli/cmd/termx/daemon_client.go` | 切片 20 后在 v3 实验入口下等价可用，切片 26 后默认可用 |
| `termx remote ...` | 依赖 CLI remote glue、旧配置 helper 和 daemon protocol extension | `termx-cli/cmd/termx/remote_commands.go`、`termx-cli/cmd/termx/remote_login.go`、`termx-cli/cmd/termx/remote_runtime.go` | 切片 24 收口，默认切换不能隐式丢 remote 或旧 `tuiv2/shared` 配置依赖 |

当前新模块状态：

| 模块 | 已有能力 | 尚缺能力 |
| --- | --- | --- |
| `termx-core-v2` | logical line history domain、HistoryWindow 投影、独立 server/daemon API、PTY lifecycle、protocol service、event stream、history.window 实服务 | CLI v3 daemon 装配、自动启动路径、remote extension/config 收口、默认入口切换后的回归验收 |
| `termx-tui-v3` | 自有 AppRuntime、state、render、input、真实 TerminalHost/FrameSink、live app、copy mode 真实 protocol path、services adapters | CLI attach/root 装配、本地端到端 smoke、默认入口切换后的回归验收 |

## 2. 实验入口策略

默认入口不得直接切换。后续必须先提供显式实验入口：

| 阶段 | 命令形态 | 约束 |
| --- | --- | --- |
| 切片 18-24 | `termx v3 ...` | 明确标识为 v3 实验入口；默认 `termx` 仍走旧链路 |
| 切片 25-27 | `termx ...` | 默认 root、attach、daemon、new、ls、kill、rm 切到 v2/v3，并完成回归验收 |
| 切片 25 之后可选 | `termx legacy ...` | 如必须保留旧链路，只能显式命名；不得用隐藏环境变量作为默认回退 |

推荐实验命令矩阵：

| 实验命令 | 对应默认命令 | 目标 backend |
| --- | --- | --- |
| `termx v3 daemon` | `termx daemon` | core-v2 server |
| `termx v3 new -- CMD` | `termx new -- CMD` | core-v2 protocol create |
| `termx v3 ls` | `termx ls` | core-v2 protocol list |
| `termx v3 attach <id>` | `termx attach <id>` | tui-v3 + core-v2 attach/history/input |
| `termx v3 kill <id>` | `termx kill <id>` | core-v2 protocol kill |
| `termx v3 rm <id>` | `termx rm <id>` | core-v2 protocol remove |

## 3. 当前 CLI 命令迁移矩阵

| 命令 | 当前依赖 | v2/v3 目标 | 最早切片 | 验收证据 |
| --- | --- | --- | --- | --- |
| `termx` | `tuiv2/app.RunWithClient` | tui-v3 root app，默认 session `main/main` | 25 | 非交互 `--help` 可编译；交互 smoke 走 tui-v3 |
| `termx attach <id>` | `tuiv2/app.RunWithClient` | tui-v3 attach app，绑定指定 terminal | 21、26 | attach 后能渲染 live surface、输入、resize、copy mode |
| `termx daemon` | 旧 `termx-core.NewServer` | core-v2 `NewServer` 或等价 constructor | 18、26 | socket listen、shutdown、events、protocol smoke 通过 |
| `termx new -- CMD` | `protocol.Client.Create` + 旧 daemon | core-v2 protocol `create` | 20、26 | 返回 terminal id；`ls` 可见；PTY 有输出 |
| `termx ls` | `protocol.Client.List` + 旧 daemon | core-v2 protocol `list` | 20、26 | 输出 id/name/command/state/size |
| `termx kill <id>` | `protocol.Client.Kill` + 旧 daemon | core-v2 protocol `kill` | 20、26 | running 进程退出；事件发出 |
| `termx rm <id>` | `protocol.Client.Remove` + 旧 daemon | core-v2 protocol `remove` | 20、26 | inventory 删除；attach channel 清理 |
| `termx remote login` | CLI HTTP login + 旧 `tuiv2/shared` 配置 helper | CLI 配置 helper 从 tuiv2 解耦，认证存储行为保持 | 23、24、28 | 登录、保存 token、配置路径和敏感信息保护测试通过 |
| `termx remote status/info/open` | daemon remote protocol methods | core-v2 daemon 暴露 remote extension handler，或显式 legacy/fallback 隔离 | 24 | status/local status 与旧输出兼容或延期记录明确 |
| `termx remote enable/disable/pair` | CLI remote glue + daemon extension | core-v2 daemon 支撑 local runtime 和 pair，或显式 legacy/fallback 隔离 | 24 | remote CLI 测试通过或延期记录明确；敏感信息不泄漏 |

## 4. Protocol 方法迁移矩阵

core-v2 必须先实现本地默认路径所需方法，再收口 remote。

| 方法或 frame | 当前旧 server 行为 | core-v2 目标 | 最早切片 |
| --- | --- | --- | --- |
| `hello` | protocol 握手 | 保持 wire version 兼容 | 12 |
| `create` | 创建 PTY terminal，返回 id/state | core-v2 registry + PTY lifecycle + history ingest | 13、14 |
| `list` | 返回 terminal inventory | core-v2 registry 排序输出 | 12、14 |
| `get` | 返回 terminal info | core-v2 terminal info | 14 |
| `kill` | 结束 running terminal 或移除 exited terminal | core-v2 process control | 13、14 |
| `restart` | 重启 terminal | core-v2 process restart；可在实验入口先标注受限 | 13、14 |
| `remove` | 关闭 terminal、清理持久状态、发事件 | core-v2 lifecycle + storage cleanup | 13、14 |
| `set_tags` | 修改 tags | core-v2 metadata | 14 |
| `set_metadata` | 修改 name/tags 并发 inventory 变化 | core-v2 metadata + duplicate guard | 14 |
| `resize` | 按 terminal id resize | core-v2 resize event，不能凭空创建 history | 13、14 |
| `ensure_resize` | attach channel resize ownership guard | core-v2 resize ownership 或等价 owner/follower 规则 | 14 |
| `attach` | 分配 channel，订阅 stream，返回 resize control | core-v2 attach channel + stream pump | 14 |
| `detach` | 清理 terminal attachment | core-v2 attachment cleanup | 14 |
| `events` | 订阅 terminal/storage events | core-v2 event broker | 12、14 |
| `snapshot` | legacy realtime projection | 仅作为实时兼容投影，不是 history truth | 14 |
| `grid.viewport` | legacy realtime projection | 仅作为实时兼容投影，不是 copy mode truth | 14 |
| `history.window` | 当前旧 core 已扩展 token/generation/logical cursor 字段，但实现仍在旧 core | core-v2 authoritative HistoryWindow，来自 logical line truth | 14 |
| `storage.get` | app storage | core-v2 storage service 或迁移兼容层 | 23、24 |
| `storage.put` | app storage | core-v2 storage service 或迁移兼容层 | 23、24 |
| `storage.delete` | app storage | core-v2 storage service 或迁移兼容层 | 23、24 |
| `storage.list` | app storage | core-v2 storage service 或迁移兼容层 | 23、24 |
| `remote.status` | remote extension handler | core-v2 daemon 装配 remote handler，或显式 legacy/fallback 隔离 | 24 |
| `remote.pair.start` | remote extension handler | core-v2 daemon 装配 remote handler，或显式 legacy/fallback 隔离 | 24 |
| `remote.local.enable` | remote extension handler | core-v2 daemon 装配 remote local runtime，或显式 legacy/fallback 隔离 | 24 |
| `remote.local.status` | remote extension handler | core-v2 daemon 装配 remote local runtime，或显式 legacy/fallback 隔离 | 24 |
| `remote.local.disable` | remote extension handler | core-v2 daemon 装配 remote local runtime，或显式 legacy/fallback 隔离 | 24 |
| `wire.TypeInput` | attach channel 输入写入 PTY | tui-v3 input -> protocol input -> core-v2 PTY | 13、16 |
| `wire.TypeResize` | attach channel resize | tui-v3 resize -> protocol resize frame -> core-v2 resize | 13、16 |
| `wire.TypeStreamReady` | stream backpressure/recovery hint | core-v2 stream pump 支持或定义等价语义 | 14、16 |
| output stream frames | screen update、resize、closed、sync lost | core-v2 live surface stream；tui-v3 不从 stream 推断 history truth | 14、16 |

## 5. core-v2 daemon 能力矩阵

| 能力 | 旧 core 现状 | core-v2 目标 | 关键约束 |
| --- | --- | --- | --- |
| server constructor/options | `NewServer` + socket/default size/grid root/logger | 独立 `NewServer` 或等价 API | `termx-cli` 不得通过旧 `termx-core.NewServer` 启动 |
| socket listen/shutdown | Unix socket listener + graceful shutdown | core-v2 自有 listener/shutdown | 自动启动 daemon 行为必须保持 |
| terminal registry | map terminal id -> Terminal | core-v2 registry | `list/get/remove/events` 共用同一 truth |
| PTY lifecycle | create/read/write/resize/kill/restart | core-v2 lifecycle package | 输出进入 live surface 和 history ingest 边界 |
| live surface | VTerm/screen stream | core-v2 live projection | 只服务实时显示，不是 committed history truth |
| history truth | 旧 mixed grid/persisted/live-tail path | `LogicalLineStore` + committed index + mutable frontier | logical line 是唯一 history truth |
| HistoryWindow | 旧 core authoritative window 过渡实现 | core-v2 authoritative window service | 支持 token/generation/cursor/boundary/cols |
| stream attachment | channel allocator + stream pump | core-v2 attachment manager | 支持 input/resize/closed/sync lost |
| resize ownership | owner/follower/observer | core-v2 resize control | resize 不能创建 history |
| events | terminal/storage event fanout | core-v2 event broker | remote/local CLI 都消费同一事件流 |
| storage | public/private app storage | core-v2 storage 或兼容 service | remote/session 配置不能丢 |
| remote extension | `ProtocolMethodHandler` | core-v2 server extension hook，或显式 legacy/fallback 隔离 | 切片 24 前不能宣称 remote 完成 |
| remote login/config | CLI HTTP login + `tuiv2/shared` 配置 helper | CLI 自有或共享配置 helper，不依赖 tuiv2 默认 runtime | 切片 28 前必须清理默认旧依赖 |
| persistence/recovery | grid root + terminal store | logical line store + runtime recovery | recovery/full replace 不得凭空创建 committed history |
| logging/diagnostics | slow op、socket、daemon logs | core-v2 logging hooks | CLI `--log-file` 行为保持 |

## 6. tui-v3 能力矩阵

| 能力 | tuiv2 现状 | tui-v3 目标 | 最早切片 |
| --- | --- | --- | --- |
| runtime | Bubble Tea program；TTY 上部分绕过 standardRenderer | 自有 `AppRuntime`、`Msg`、`Effect`、`EffectRunner` | 15 |
| terminal host | Bubble Tea input + 自定义 cursor writer | 自有 raw mode、input loop、FrameSink | 15 |
| protocol services | `tuiv2/bridge` + runtime managers | tui-v3 services + protocol adapters | 16 |
| live render | 本地 VTerm/snapshot/runtime registry | core-v2 live surface projection -> RenderVM | 16 |
| input | tea key/mouse -> action -> runtime input | `InputEvent` -> semantic intent -> protocol input | 16 |
| resize | pane/layout/owner resize managers | TerminalHost size + protocol resize/ensure_resize | 16 |
| copy mode | 已接入 authoritative history.window，但仍在 tuiv2 state/render | `HistoryStore` + `CopyModeStore` 只消费 core-v2 window | 17 |
| session/workspace | tuiv2 persist/workbench/session store | v3 最小 session state，后续再扩展复杂布局 | 16、23 |
| render styling | tuiv2 render + lipgloss v1/自定义 writer | tui-v3 RenderVM + Renderer + lipgloss/v2/x/ansi | 15、16 |
| Bubble Tea | 主 runtime contract | 禁止进入 v3 主线 | 全阶段 |

## 7. 分阶段验收口径

| 阶段 | 验收重点 | 必跑检查 |
| --- | --- | --- |
| 切片 11 | 本文档存在，矩阵覆盖 CLI、protocol、daemon、TUI、验收口径 | `git diff --check` |
| 切片 12 | core-v2 server API 和 daemon 骨架 | `cd termx-core-v2 && go test ./... -count=1` |
| 切片 13 | PTY lifecycle 和输出 ingest | core-v2 tests；必要时 termx-vterm tests |
| 切片 14 | core-v2 protocol service + history.window | core-v2 tests、`cd internal && go test ./protocol/... -count=1`、`cd termx-proto && go test ./... -count=1` |
| 切片 15 | tui-v3 真实 TerminalHost/FrameSink | `cd termx-tui-v3 && go test ./... -count=1` |
| 切片 16 | tui-v3 attach/live/input/resize 主路径 | tui-v3 tests + 最小 fake/真实协议 harness |
| 切片 17 | copy mode 真实 core client path | tui-v3 tests，覆盖 latest/older/stale/selection/copy |
| 切片 18 | `termx v3` 命令组、core-v2 daemon 骨架、tui-v3 smoke 装配 | `cd termx-cli && go test ./... -count=1`、`make test-v2-migration` |
| 切片 19 | v3 daemon 连接和自动启动基础 | `cd termx-cli && go test ./... -count=1`、`make test-v2-migration` |
| 切片 20 | v3 本地控制命令 | CLI tests + v3 local command smoke |
| 切片 21 | v3 attach/TUI 装配 | CLI tests + tui-v3 tests + attach harness |
| 切片 22 | v3 本地端到端 smoke | `make test-v2-migration` + v3 local e2e smoke |
| 切片 23 | 配置、日志与状态路径收口 | CLI tests；路径差异必须显式记录 |
| 切片 24 | remote 兼容与隔离结论 | CLI remote tests；未完成项必须显式记录 |
| 切片 25 | 默认 root 入口切换 | `go run ./termx-cli/cmd/termx --help`、CLI tests、迁移 smoke |
| 切片 26 | 默认 daemon/attach/control 切换 | CLI tests、迁移 smoke、默认本地单 session smoke |
| 切片 27 | 默认入口回归验收 | CLI/core-v2/tui-v3/protocol 相关 tests、`make test-v2-migration`、默认入口 smoke |
| 切片 28 | 旧默认依赖清理与冻结 | grep 守卫：默认路径不 import `termx-core`/`tuiv2`；文档-only 部分至少 `git diff --check` |

## 8. 非目标与延期项

- 切片 11 不写 runtime 代码，只建立迁移审计基准。
- `snapshot` 与 `grid.viewport` 后续只能作为实时兼容投影，不得重新变成 history truth。
- 复杂多 pane/workspace/floating UI 不作为 v3 实验入口首个验收门槛；首个门槛是本地单 session create/attach/render/input/resize/history/copy mode。
- remote 可以晚于本地实验入口完成，但默认入口切换前必须有明确兼容结论或显式延期记录。
- 外部账号、DNS、TLS、OAuth 等人工配置事项不阻塞本地默认切换；必须用 mock/stub 或文档记录。

## 9. 后续切片关系

切片 12-14 负责让 core-v2 具备 daemon/server/protocol 能力；切片 15-17 负责让 tui-v3 具备真实 TTY 和交互能力；切片 18-24 在 CLI 内用 `termx v3 ...` 跑通实验入口、本地命令、attach/TUI、端到端 smoke、配置和 remote 收口；切片 25-27 切换默认 root、daemon、attach 和本地控制命令并完成回归验收；切片 28 清理默认旧依赖并冻结旧目录。
