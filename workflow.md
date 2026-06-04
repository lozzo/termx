# 工作流：termx-core-v2 / termx-tui-v3 默认入口切换

本文件是当前分支唯一有效的活动驱动文件。后续所有分析、实现、测试、提交都必须先读取本文件，并以本文件为准。

本文件只记录目标、范围、硬约束、任务队列、测试准入和提交规则。架构正文不写在本文件里，分别以 `termx-core-v2/docs/architecture.md`、`termx-tui-v3/docs/architecture.md` 和 `termx-cli/docs/v2-v3-switch-audit.md` 为准。

本文件必须保持全中文。若本文件与旧说明、聊天记录、旧代码行为或局部假设冲突，默认以本文件为准；若技术设计需要变化，必须先更新本文件或与实现同切片更新。

## 1. 当前唯一目标

在已完成 `termx-core-v2/` 与 `termx-tui-v3/` 架构骨架的基础上，完成从旧 `termx-core/` + `tuiv2/` 到新 `termx-core-v2/` + `termx-tui-v3/` 的默认运行时切换。

当前事实：

- `go run ./termx-cli/cmd/termx` 当前仍使用旧 `termx-core` 和旧 `tuiv2`。
- `termx-core-v2` 已有 logical-line-first history domain、HistoryWindow 投影、独立 daemon/server 骨架、terminal lifecycle 与 protocol service，但尚未成为默认 CLI runtime。
- `termx-tui-v3` 已有自有 runtime/state/render/services 骨架、history/copy mode 状态模型和 smoke 入口，但尚未完整替代旧 TUI 的真实终端交互路径。
- 当前主线不是继续修补旧目录，而是按下面任务队列把新 core/tui 接到真实 CLI 默认路径。

完成定义：

- `go run ./termx-cli/cmd/termx`、`termx daemon`、`termx attach` 默认使用 `termx-core-v2` 与 `termx-tui-v3`。
- `termx-cli` 默认本地路径不再依赖旧 `termx-core` 或 `tuiv2` 作为 runtime backend。
- 若保留旧入口，必须显式命名为 legacy 或 fallback；不能作为默认路径，也不能被当成切换完成的依据。
- 本地单 session 主路径必须覆盖 create、attach、live render、input、resize、history window、copy mode、kill、remove。
- remote 兼容可以分阶段完成，但未完成差异必须写入任务队列或迁移审计，不能隐式宣称完整替换。

## 2. 技术设计基准

- core-v2 架构基准：`termx-core-v2/docs/architecture.md`。
- tui-v3 架构基准：`termx-tui-v3/docs/architecture.md`。
- CLI 切换审计和迁移矩阵：`termx-cli/docs/v2-v3-switch-audit.md`。
- 本文件不展开架构正文；实现遇到架构冲突时，先更新对应设计文档和本文件任务队列，再继续实现。

## 3. 工作范围

### 3.1 当前主线范围

允许主动新增、修改、删除、重写、测试：

- `termx-core-v2/`
- `termx-tui-v3/`
- `termx-cli/`
- `internal/protocol/`
- `termx-proto/`
- 根目录直接相关文件：`workflow.md`、`AGENTS.md`、`go.work`、`go.work.sum`、`Makefile`、必要顶层说明文档

### 3.2 受限联动范围

只有当 core-v2、tui-v3、vterm、协议契约、CLI 切换或 remote 兼容确实需要时，才允许最小化触及：

- `termx-vterm/`
- `termx-shared/`
- `termx-testkit/`
- `termx-remote/`
- `scripts/`

### 3.3 只读参考范围

默认不得修改：

- `termx-core/`
- `tuiv2/`

上述目录只能读取、搜索、运行测试或摘取已验证过的外部契约作为参考。不得继续在其中做 logical-line 原地重构、旧 copy mode 修补、旧 snapshot/grid viewport history path 修补、兼容桥接或 helper 收敛。

如确实必须修改旧目录，必须先修改本文件，把该动作写入受限联动范围，并说明为什么不能在新目录完成。默认入口切换不得通过把新目录包在旧 core/旧 TUI 外层来伪完成。

### 3.4 冻结范围

不得主动触碰：

- `remote-ui/`
- `termx-app/`
- `web-control/`
- `termx-hub/`
- `bin/`
- `.claude/`
- 顶层可执行产物和测试产物
- 未在本文件列出的任何目录

如需扩展范围，必须先修改本文件的范围表，再开展对应工作。

## 4. 不可违反的语义约束

### 4.1 为什么必须使用 logical line

- 目标是支持可落盘、可分页、长期保留、接近无限的历史记录。
- 历史 truth 不能依赖当前 terminal size，因为窗口大小随时会变。
- 如果历史只存在内存 grid、snapshot scrollback 或 visual row 中，resize 后就必须读回大量历史并按新列宽重排；这不适合无限历史，也不适合稳定分页。
- logical line 是 shell/程序输出语义下的稳定历史单位；visual row 只是某个 `cols` 下的投影结果。
- 因此 core-v2 只能按 logical line 存储和分页，再按当前列宽生成 HistoryWindow。

### 4.2 core-v2 约束

- primary history 的基本单位必须是 logical line。
- logical line 必须有稳定身份，不能只靠当前窗口内 row index 表达。
- visual rows 只能是某个 cols 下的投影结果。
- wrapped metadata 可以作为投影辅助信息，但不能作为最终历史 truth。
- snapshot、grid viewport、TUI runtime scrollback 都不能作为 committed history truth。
- `LogicalLineStore` 是唯一历史数据模型。
- `CommittedHistoryIndex` 只表达当前计入 authoritative committed history 的 logical line 顺序。
- `MutableFrontier` 只表达当前仍可被终端语义修改的 logical line 范围。
- `StorageBackend` 只是内存、文件、mmap 等存储落点，不定义 mutability。
- `persisted`、落盘或 committed 不表示永远不可修改；clear scrollback、truncate、retention、reclaim、replace 都可以按完整 logical line 删除、撤回、替换或重新提交已提交历史。
- `open/sealed`、`dirty/clean`、`committed/uncommitted`、`mutable`、`residency` 是正交属性，不得混成一个状态。
- attach、reattach、bootstrap、recovery、full replace、clear screen、resize 不得凭空创造 committed history。
- resize 不是历史创建事件，也不是历史重写事件；grow resize 只能按完整 logical line reclaim committed suffix，shrink resize 必须表达 `screen -> hidden mutable frontier`。
- alt-screen 不写入 primary history；process exit 是显式 mutability 边界，退出时 primary `MutableFrontier` 必须 force commit。

### 4.3 tui-v3 约束

- `termx-tui-v3` 不拥有 committed history truth，只消费 core-v2 返回的 authoritative `HistoryWindow`。
- `HistoryStore` 只保存 core-v2 返回的 authoritative window、请求状态和 exhausted marker。
- `CopyModeStore` 只保存交互态：active pane、terminal id、viewport top、cursor、selection、bound token、bound cols。
- latest window 使用 replace。
- older window 使用 prepend。
- stale response guard 使用 core 返回的 token、generation、cursor、logical line boundary 和 cols，不使用本地深度计数。
- `TerminalSurfaceStore` 只服务实时显示，不得向 `HistoryStore` 提供 rows 让其反推出 logical line。
- copy mode、鼠标滚轮、page up/down、selection、copy 必须围绕 authoritative `HistoryWindow` 工作。
- copy mode 缺 authoritative window 时不得从 live surface、snapshot 或 VTerm scrollback fallback。
- TUI 主线不得引入 Bubble Tea `Program`、`standardRenderer`、`tea.Model`、`tea.Msg`、`tea.Cmd`、`tea.KeyMsg`、`tea.MouseMsg`、`bubbles` 或依赖这些 contract 的 UI 组件。
- 允许使用 `lipgloss/v2`、`x/ansi`、隔离在 terminal host/frame sink 内的 `ultraviolet` 等纯渲染或终端 primitive。

### 4.4 CLI / daemon 切换约束

- 默认入口切换前必须存在显式实验入口，并通过本地单 session 主路径 smoke。
- 实验入口可以暂时与旧入口并存，但必须显式命名；不能让用户误以为默认 `termx` 已完成切换。
- `termx-cli` 默认路径不得通过旧 `termx-core.NewServer` 启动 core-v2。
- `termx-cli` 默认路径不得通过 `tuiv2/app.RunWithClient` 启动 tui-v3。
- core-v2 daemon 必须自己拥有 terminal lifecycle、protocol service、event stream、history window service 和 shutdown 语义；不能把旧 daemon 当作 runtime backend。
- tui-v3 必须自己拥有 terminal raw mode、input loop、frame sink、effect loop 和 copy mode 交互；不能把旧 tuiv2 当作 runtime backend。
- 默认本地路径完成后，remote 相关差异必须在任务队列中显式保留，不得隐式宣称完整替换。

### 4.5 命名约束

- 新实现命名收敛到 `LogicalLineStore`、`CommittedHistoryIndex`、`MutableFrontier`、`StorageBackend`、`HistoryWindow`、`AppRuntime`、`TerminalHost`、`EffectRunner`、`FrameSink`。
- `hot/cold` 只能出现在旧模型问题说明或迁移记录中，不得继续作为代码、测试 helper、内部 contract 或运行时状态的主语义命名。
- 若从旧实现迁移概念，迁入新目录时必须按新语义重命名，不能把旧语义带进 v2/v3。

## 5. 任务队列

状态只能使用：`待开始`、`进行中`、`完成`、`阻塞`。同一时间只能有一个切片处于 `进行中`。

自动执行必须按表格顺序处理最早未完成切片：

- 如果最早未完成切片是 `阻塞`，必须停止并向用户说明阻塞，不得跳到后续 `待开始` 切片。
- 如果最早未完成切片是 `进行中`，继续该切片。
- 如果最早未完成切片是 `待开始`，先把它改为 `进行中` 并提交，或与本切片首个实现提交同切片提交，然后只执行该切片。

| 切片 | 状态 | 范围 | 完成标准 |
| --- | --- | --- | --- |
| 0. 设计基线 | 完成 | `termx-core-v2/docs/`、`termx-tui-v3/docs/`、`workflow.md`、`AGENTS.md` | core-v2 与 tui-v3 架构文档存在；本文件和 `AGENTS.md` 指向新主线；旧实现只作为参考 |
| 1. 新模块骨架 | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`go.work` | 两个新 Go module 加入 workspace；建立最小包结构、空实现和 smoke tests；不依赖旧内部实现 |
| 2. core-v2 domain 骨架 | 完成 | `termx-core-v2/` | 建立 `LogicalLine`、`LogicalLineStore`、`CommittedHistoryIndex`、`MutableFrontier`、`StorageBackend` 内存实现和基础 harness |
| 3. core-v2 历史事件语义 | 完成 | `termx-core-v2/` | 覆盖 write/seal/mutate/reset/commit/reclaim/hide/truncate/alt-screen/process-exit/resize 事件；非历史事件不创建 committed history |
| 4. core-v2 HistoryWindow 投影 | 完成 | `termx-core-v2/` | 从 logical lines 生成 visual rows、line spans、clipping、token、generation、cursor、latest replace、older prepend |
| 5. protocol 契约联动 | 完成 | `termx-proto/`、`internal/protocol/`、`termx-core-v2/` | 按需扩展 `history.window` contract；legacy snapshot/grid viewport 明确只作为实时兼容投影 |
| 6. tui-v3 runtime 骨架 | 完成 | `termx-tui-v3/` | 建立自有 `Msg`、`Effect`、`AppRuntime`、`EffectRunner`、`TerminalHost` fake、`FrameSink` contract 和 harness |
| 7. tui-v3 history/copymode 状态 | 完成 | `termx-tui-v3/` | reducer-owned `HistoryStore`、`CopyModeStore`、latest/older/stale/resize/selection harness 完成 |
| 8. tui-v3 input/render/UI 边界 | 完成 | `termx-tui-v3/` | 自有 `InputEvent`、semantic intent、RenderVMBuilder、Renderer、hit regions、lipgloss style helper；无 Bubble Tea contract |
| 9. services 与集成 | 完成 | `termx-tui-v3/`、受限联动范围 | core client、terminal service、session、clipboard contract、fake、protocol history adapter 和最小 runtime e2e harness |
| 10. 收口与迁移入口 | 完成 | 受限联动范围 | 新路径可运行；必要 CLI/adapter 入口接入；旧 helper/fixture 只在明确不再需要时删除 |
| 11. 切换审计与迁移矩阵 | 完成 | `termx-cli/docs/`、`termx-core-v2/docs/`、`termx-tui-v3/docs/`、只读参考旧目录 | `termx-cli/docs/v2-v3-switch-audit.md` 已明确当前 CLI 命令到旧依赖的映射、目标 v2/v3 命令矩阵、协议方法矩阵、daemon 能力矩阵、TUI 能力矩阵和分阶段验收口径 |
| 12. core-v2 server API 与 daemon 骨架 | 完成 | `termx-core-v2/`、`internal/protocol/`、`termx-cli/` 按需 | core-v2 已提供独立 server/daemon API、options、listen/shutdown、terminal registry、事件订阅 fake harness；静态 harness 确认不调用旧 `termx-core`/`tuiv2` |
| 13. core-v2 terminal lifecycle 与 PTY 管线 | 完成 | `termx-core-v2/`、`termx-vterm/`、`termx-shared/` 按需 | core-v2 已建立 `TerminalProcess`/`ProcessFactory`、terminal lifecycle、create/input/resize/exit/restart/remove harness；输出进入 live surface 与 `HistoryTrack` ingest；exit force commit、late output guard、resize grow/shrink 和 shutdown lifecycle harness 通过 |
| 14. core-v2 protocol service 与 HistoryWindow 实服务 | 完成 | `termx-core-v2/`、`internal/protocol/`、`termx-proto/` | core-v2 protocol session 已服务 create/get/list/set metadata/restart/remove/events/input/resize/history.window；HistoryWindow 来自 `HistoryTrack` logical line truth，包含 token/generation/cursor/boundary 与 stale guard；协议测试通过 |
| 15. tui-v3 真实 TerminalHost 与 FrameSink | 完成 | `termx-tui-v3/` | 建立真实 raw mode、输入读取、窗口尺寸、帧输出、恢复终端状态和取消/退出 harness；不得引入 Bubble Tea runtime |
| 16. tui-v3 本地 live app 主路径 | 完成 | `termx-tui-v3/`、`termx-cli/` 按需 | attach 本地 session 后可渲染 live surface、发送键盘输入、处理 resize、显示基础状态与错误；fake 和最小真实协议 harness 通过 |
| 17. tui-v3 copy mode 主路径 | 完成 | `termx-tui-v3/`、`internal/protocol/` 按需 | page up/down、鼠标滚轮、older prepend、latest replace、selection、copy、stale response guard 在真实 core client 路径通过 |
| 18. CLI v3 命令组与 daemon 骨架 | 进行中 | `termx-cli/`、`Makefile` | 增加显式 `termx v3` 实验命令组；`termx v3 daemon` 以前台方式启动 core-v2 server；提供非交互 v3 smoke 入口验证 tui-v3 可被 CLI 装配运行；默认 root、`daemon`、`attach` 仍保持旧入口；CLI 测试和迁移 smoke 通过 |
| 19. CLI v3 daemon 连接与自动启动基础 | 待开始 | `termx-cli/`、`termx-core-v2/` 按需 | v3 实验入口能连接已存在 core-v2 daemon；需要 daemon 时只能自动启动 core-v2 daemon，不能复用旧 `termx-core` 自动启动路径；socket、日志路径、启动失败和关闭行为有测试 |
| 20. CLI v3 本地控制命令 | 待开始 | `termx-cli/`、`termx-core-v2/`、`internal/protocol/` 按需 | `termx v3 new`、`termx v3 ls`、`termx v3 kill`、`termx v3 rm` 通过 core-v2 protocol service 工作；命令输出和错误语义与旧默认入口可对照；CLI tests 覆盖本地单 session |
| 21. CLI v3 attach/TUI 装配 | 待开始 | `termx-cli/`、`termx-tui-v3/` 按需 | `termx v3 attach <id>` 使用 tui-v3 `TerminalHost`、protocol adapters 和 `NewInteractiveRuntime` 装配真实交互路径；非交互环境给出明确错误或运行专用 harness；不得调用 `tuiv2/app.RunWithClient` |
| 22. v3 本地端到端 smoke | 待开始 | `termx-cli/`、`termx-core-v2/`、`termx-tui-v3/`、`internal/protocol/`、`Makefile` 按需 | 建立可重复的本地单 session smoke：启动 core-v2 daemon、创建 PTY、读取 live surface、发送 input、处理 resize、请求 `history.window`、触发 copy mode 主路径；默认入口仍未切换 |
| 23. 配置、日志与状态路径收口 | 待开始 | `termx-cli/`、`termx-shared/` 按需 | v3 实验入口的 socket、log、config、state 路径语义明确且有测试；v3 路径不得为了配置加载依赖 `tuiv2/shared`；与旧默认入口的差异写入迁移审计 |
| 24. remote 兼容与隔离结论 | 待开始 | `termx-cli/`、`termx-core-v2/`、`termx-remote/`、`termx-shared/` 按需 | remote 命令在默认切换前有明确结论：已迁移到 core-v2 extension，或显式保留 legacy/fallback 边界；不能把未完成 remote 能力伪装成默认已兼容 |
| 25. 默认 root 入口切换 | 待开始 | `termx-cli/`、`Makefile`、`go.work` 按需 | `go run ./termx-cli/cmd/termx` 默认 root TUI 使用 tui-v3；默认 root 不再调用旧 `runTUIv2`；旧 root 若保留必须移动到显式 legacy/fallback 入口；`--help` 可编译运行 |
| 26. 默认 daemon/attach/control 切换 | 待开始 | `termx-cli/`、`termx-core-v2/`、`termx-tui-v3/`、`internal/protocol/`、`Makefile` 按需 | `termx daemon`、`termx attach`、`termx new`、`termx ls`、`termx kill`、`termx rm` 默认使用 core-v2/tui-v3；旧路径只允许显式 legacy/fallback；本地单 session 主路径回归通过 |
| 27. 默认入口回归验收 | 待开始 | `termx-cli/`、`termx-core-v2/`、`termx-tui-v3/`、`internal/protocol/`、`termx-proto/`、`Makefile` 按需 | 默认入口切换后运行 CLI tests、core-v2 tests、tui-v3 tests、protocol tests、`make test-v2-migration` 和默认入口非交互 smoke；remote 未完成项必须在文档和任务结论中显式保留 |
| 28. 旧默认依赖清理与冻结 | 待开始 | `termx-cli/`、`workflow.md`、`AGENTS.md`、必要顶层说明文档 | `termx-cli` 默认路径不再 import 旧 `termx-core`/`tuiv2`；旧目录冻结状态明确；依赖守卫、测试入口和文档完成；切片 28 完成后本轮默认入口切换目标才算完成 |

当前下一步：继续切片 18“CLI v3 命令组与 daemon 骨架”。

## 6. 必做 harness

### 6.1 core-v2 harness

必须逐步覆盖：

- 普通输出无换行。
- 普通输出带换行。
- 自动折行。
- 宽字符与组合字符。
- 光标移动后覆写。
- clear screen 与 clear scrollback。
- alt-screen 进入与退出。
- grow resize reclaim committed suffix。
- shrink resize hidden frontier。
- attach/bootstrap/recovery/full replace 不创建 committed history。
- process exit force commit。
- committed suffix reclaim 后修改再提交。
- truncate/retention 按完整 logical line 删除。
- logical line id、seal、dirty、generation、residency、committed index、mutable frontier、projection 内容。

### 6.2 HistoryWindow harness

必须逐步覆盖：

- latest replace。
- older prepend。
- 空 older exhausted。
- token/generation/cursor/boundary stale guard。
- 不同 cols 下重投影。
- clipped before / clipped after。
- logical line id 与 row 到 line 映射。
- mutable frontier 和 committed tail 混合投影。
- resize 后旧 window/response 失效。
- 真实 PTY 输出进入 logical line 后，通过 protocol history.window 返回 authoritative window。

### 6.3 tui-v3 harness

必须逐步覆盖：

- input key/mouse -> semantic intent。
- reducer message -> state + effects。
- effect result 回到 message path。
- AppRuntime message 顺序、timer、batch、cancel、quit。
- TerminalHost input event 转换和 FrameSink contract。
- 真实 raw mode 进入、恢复、异常退出清理。
- HistoryStore latest replace、older prepend、empty exhausted、stale response、cols mismatch。
- CopyMode cursor、viewport、selection、clipped span、multi logical line copy。
- RenderVM live mode 与 copy mode projection 分流。
- copy mode 缺 authoritative window 时不得从 live surface fallback。
- lipgloss/v2 style helper 宽度、裁剪、ANSI 安全性。

### 6.4 CLI / 集成 harness

必须逐步覆盖：

- CLI root、attach、daemon 当前旧链路识别测试或审计记录。
- `termx v3` 实验命令组存在，且不会改变默认 root、`daemon`、`attach` 的旧入口行为。
- `termx v3 daemon` 启动 core-v2 daemon。
- v3 实验入口能通过非交互 smoke 装配 tui-v3。
- v3 实验入口 attach tui-v3。
- 自动启动 daemon、连接已有 daemon、socket 路径、日志路径、配置路径。
- `termx v3 new`、`termx v3 ls`、`termx v3 kill`、`termx v3 rm` 命令通过 core-v2 protocol service。
- PTY 输出 -> core-v2 live surface -> tui-v3 render。
- tui-v3 input -> protocol input -> PTY。
- resize -> core-v2 live/history boundary -> tui-v3 frame。
- copy mode -> core-v2 HistoryWindow -> selection/copy。
- 默认入口切换后，`termx-cli` 默认路径不再 import 旧 `termx-core` 或 `tuiv2`。
- Bubble Tea contract 不进入 tui-v3 主线。

## 7. 测试准入

每个有效切片提交前必须运行与切片相关的测试。新 module 建立后优先使用模块内命令：

- core-v2 改动：在 `termx-core-v2/` 运行 `go test ./... -count=1`。
- tui-v3 改动：在 `termx-tui-v3/` 运行 `go test ./... -count=1`。
- protocol 改动：在 `internal/` 运行 `go test ./protocol/... -count=1`，在 `termx-proto/` 运行 `go test ./... -count=1`。
- CLI 改动：在 `termx-cli/` 运行 `go test ./... -count=1`。
- workspace 或受限联动改动：运行所有相关模块测试，并按需运行 `make test-v2-migration`。
- 切片 18 改动：至少运行 `cd termx-cli && go test ./... -count=1`、`make test-v2-migration`。
- 切片 19-24 改动：至少运行 `cd termx-cli && go test ./... -count=1`，涉及 core-v2、tui-v3、protocol、remote 或共享模块时同步运行对应模块测试，并按需运行 `make test-v2-migration`。
- 默认入口切换相关改动：至少运行 `make test-v2-migration`、`cd termx-cli && go test ./... -count=1`，并用非交互命令验证 `go run ./termx-cli/cmd/termx --help` 可编译运行。
- 文档-only 改动：至少运行 `git diff --check`。

如果测试无法运行，必须在最终说明和必要时在提交前记录原因。不能把真实语义失败当作偶发失败。

## 8. 自动执行规则

- 每次开始工作先读取本文件，再检查 `git status --short --branch`。
- 按任务队列表格顺序选择最早未完成切片；遇到 `阻塞` 必须停止，遇到 `进行中` 继续，遇到 `待开始` 必须先标为 `进行中`。
- 切片范围不清时，先更新本文件，不要自行扩大范围。
- 每个切片尽量保持小而可提交；不要跨多个切片堆叠未提交改动。
- 完成切片后更新本文件中对应状态和必要的下一步说明，与实现同提交。
- 如果发现设计文档需要变化，必须与实现同切片更新，或先提交设计更新。
- 如果遇到阻塞，必须把对应切片状态改为 `阻塞` 并说明阻塞条件；不要继续扩散到其他目录。
- 自动执行不能因为局部测试暂时通过就跳过后续切片；只有切片 28 完成且测试准入通过，当前目标才算完成。

## 9. 提交规则

- 当前工作区未提交改动必须先整理并提交，再继续后续开发。
- 每个有效变动必须形成中文提交。
- 不允许长期堆积未提交改动。
- 不得 amend commit，除非用户明确要求。
- 不得 revert 用户或其他代理的未提交改动；若冲突，先停下说明。
- 删除旧代码必须和对应新语义或 harness 同切片提交。

## 10. 当前状态

- 当前分支主线已切换到 `termx-core-v2/` 与 `termx-tui-v3/` 的重构方向。
- 切片 0-10 已完成：新 core/tui module、logical line history domain、HistoryWindow projection、protocol 字段、tui-v3 runtime/state/render/services 骨架、smoke 入口和 `make test-v2-migration` 均已建立。
- 切片 11 已完成：`termx-cli/docs/v2-v3-switch-audit.md` 已建立默认入口切换审计、迁移矩阵和分阶段验收口径。
- 切片 12 已完成：`termx-core-v2` 已建立独立 server/daemon API、listener factory、listen/shutdown、terminal registry、event broker 和 fake harness，不依赖旧 `termx-core`/`tuiv2`。
- 切片 13 已完成：`termx-core-v2` 已建立 terminal lifecycle、PTY 管线、输出 ingest、live surface、history track、exit force commit、late output guard、resize grow/shrink 和 shutdown lifecycle harness。
- 切片 14 已完成：`termx-core-v2` protocol session 已服务控制面、输入、resize、events 和 `history.window`，并从 `HistoryTrack` logical line truth 返回 authoritative window。
- 切片 15 已完成：`termx-tui-v3/terminalhost` 已建立真实 raw mode、输入读取、窗口尺寸、frame sink 输出、终端状态恢复和取消退出 harness，且未引入 Bubble Tea runtime。
- 切片 16 已完成：`termx-tui-v3` 已建立 reducer-owned live surface/session、`NewLiveRuntime`、attach/render/input/resize/error 主路径、terminal service fake，以及真实 protocol client 的 attach/input/ensure_resize harness。
- 切片 17 已完成：`termx-tui-v3` 已建立 copy mode app reducer、page up/鼠标滚轮 latest/older 主路径、older prepend、selection/copy、stale response guard、真实 protocol history.window client harness，以及 live/copy reducer 组合入口。
- 当前正在执行切片 18：`CLI v3 命令组与 daemon 骨架`。
- 当前 CLI 默认运行时尚未切换：`go run ./termx-cli/cmd/termx` 仍通过旧 `termx-core` daemon 和旧 `tuiv2` TUI 工作。
- 下一阶段目标按切片 18-28 顺序执行：先完成显式 `termx v3` 实验入口，再完成 v3 daemon 连接、本地控制命令、attach/TUI 装配、本地端到端 smoke、配置和 remote 收口，然后切换默认 root/daemon/attach/control，最后清理并冻结旧默认依赖。
