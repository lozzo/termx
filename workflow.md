# 工作流：termx-core-v2 / termx-tui-v3 自动迁移主线

本文件是当前分支唯一有效的活动驱动文件。后续所有分析、实现、测试、提交都必须先读取本文件，并以本文件为准。

本文件只记录当前目标、范围、硬约束、任务队列、测试准入和提交规则。技术设计正文不写在本文件里，分别以 `termx-core-v2/docs/architecture.md` 和 `termx-tui-v3/docs/architecture.md` 为准。

本文件必须保持全中文。若本文件与旧说明、聊天记录、旧代码行为、局部假设冲突，默认以本文件为准；若技术设计需要变化，必须先更新本文件或与实现同切片更新。

## 1. 当前唯一目标

停止继续在旧 `termx-core/` 与 `tuiv2/` 上原地修补，改为在新目录 `termx-core-v2/` 与 `termx-tui-v3/` 中重新落地 logical-line-first 的终端历史模型和 TUI 架构。

完成后的系统必须满足：

- `termx-core-v2` 拥有唯一 committed history truth。
- 历史基本单位是 logical line，不是 visual row、wrapped row、snapshot scrollback 或 grid viewport。
- logical line 选择的根因是支持可落盘、可分页、长期保留、接近无限的历史记录；不得因为 terminal size 改变而要求读回并重排全部历史。
- core-v2 使用单一 `LogicalLineStore` 作为历史 truth；`CommittedHistoryIndex`、`MutableFrontier`、`StorageBackend` 只是索引、可变边界和存储落点。
- `persisted` 或落盘不表示不可修改；clear scrollback、truncate、retention、reclaim、replace 都可以按完整 logical line 删除、撤回、替换或重新提交已提交历史。
- `termx-tui-v3` 不拥有 committed history truth，只消费 core-v2 返回的 authoritative `HistoryWindow`。
- copy mode、鼠标滚轮、page up/down、older prepend、latest replace、stale response guard 都围绕 authoritative `HistoryWindow` 工作。
- `termx-tui-v3` 不以 Bubble Tea 作为主运行时，必须使用自有 `AppRuntime`、`TerminalHost`、`EffectRunner`、`FrameSink` 边界。
- 允许使用 `lipgloss/v2`、`x/ansi`、隔离在 terminal host/frame sink 内的 `ultraviolet` 等纯渲染或终端 primitive；不得把 Bubble Tea `Model/Msg/Cmd` 或绑定该 contract 的 UI 组件作为 v3 主线结构。

## 2. 技术设计基准

- core-v2 架构基准：`termx-core-v2/docs/architecture.md`。
- tui-v3 架构基准：`termx-tui-v3/docs/architecture.md`。
- 若实现与设计文档冲突，先更新对应设计文档和本文件的任务队列，再继续实现。
- `workflow.md` 不展开架构正文，只记录自动执行时需要遵守的范围、顺序和准入条件。

## 3. 工作范围

### 3.1 当前主线范围

允许主动新增、修改、删除、重写、测试：

- `termx-core-v2/`
- `termx-tui-v3/`

### 3.2 受限联动范围

只有当 core-v2、tui-v3、vterm 或协议契约变化确实需要时，才允许最小化触及：

- `termx-vterm/`
- `internal/protocol/`
- `termx-proto/`
- `termx-cli/`
- `termx-shared/`
- `termx-testkit/`
- `scripts/`
- 根目录直接相关文件：`workflow.md`、`AGENTS.md`、`go.work`、`go.work.sum`、`Makefile`、必要顶层说明文档

### 3.3 只读参考范围

默认不得修改：

- `termx-core/`
- `tuiv2/`

上述目录只能读取、搜索、运行测试或摘取已验证过的外部契约作为参考。不得继续在其中做 logical-line 原地重构、旧 copy mode 修补、旧 snapshot/grid viewport history path 修补、兼容桥接或 helper 收敛。

如确实必须修改旧目录，必须先修改本文件，把该动作写入受限联动范围，并说明为什么不能在新目录完成。

### 3.4 冻结范围

不得主动触碰：

- `remote-ui/`
- `termx-app/`
- `web-control/`
- `termx-hub/`
- `termx-remote/`
- `bin/`
- `.claude/`
- 顶层可执行产物和测试产物
- 未在本文件列出的任何目录

如需扩展范围，必须先修改本文件的范围表，再开展对应工作。

## 4. 不可违反的语义约束

### 4.1 core-v2 约束

- primary history 的基本单位必须是 logical line。
- logical line 必须有稳定身份，不能只靠当前窗口内 row index 表达。
- visual rows 只能是某个 cols 下的投影结果。
- wrapped metadata 可以作为投影辅助信息，但不能作为最终历史 truth。
- snapshot、grid viewport、TUI runtime scrollback 都不能作为 committed history truth。
- `LogicalLineStore` 是唯一历史数据模型。
- `CommittedHistoryIndex` 只表达当前计入 authoritative committed history 的 logical line 顺序。
- `MutableFrontier` 只表达当前仍可被终端语义修改的 logical line 范围。
- `StorageBackend` 只是内存、文件、mmap 等存储落点，不定义 mutability。
- `open/sealed`、`dirty/clean`、`committed/uncommitted`、`mutable`、`residency` 是正交属性，不得混成一个状态。
- attach、reattach、bootstrap、recovery、full replace、clear screen、resize 不得凭空创造 committed history。
- resize 不是历史创建事件，也不是历史重写事件；grow resize 只能按完整 logical line reclaim committed suffix，shrink resize 必须表达 `screen -> hidden mutable frontier`。
- alt-screen 不写入 primary history；process exit 是显式 mutability 边界，退出时 primary `MutableFrontier` 必须 force commit。

### 4.2 tui-v3 约束

- `termx-tui-v3` 不得用本地 VTerm scrollback、snapshot totals、row ownership 数量、LoadedRows、hasMore、wrapped 拼接结果推断历史真相。
- `HistoryStore` 只保存 core-v2 返回的 authoritative window、请求状态和 exhausted marker。
- `CopyModeStore` 只保存交互态：active pane、terminal id、viewport top、cursor、selection、bound token、bound cols。
- latest window 使用 replace。
- older window 使用 prepend。
- stale response guard 使用 core 返回的 token、generation、cursor、logical line boundary 和 cols，不使用本地深度计数。
- `TerminalSurfaceStore` 只服务实时显示，不得向 `HistoryStore` 提供 rows 让其反推出 logical line。
- TUI 主线不得引入 Bubble Tea `Program`、`standardRenderer`、`tea.Model`、`tea.Msg`、`tea.Cmd`、`tea.KeyMsg`、`tea.MouseMsg`、`bubbles` 或依赖这些 contract 的 UI 组件。

### 4.3 命名约束

- 新实现命名收敛到 `LogicalLineStore`、`CommittedHistoryIndex`、`MutableFrontier`、`StorageBackend`、`HistoryWindow`、`AppRuntime`、`TerminalHost`、`EffectRunner`、`FrameSink`。
- `hot/cold` 只能出现在旧模型问题说明或迁移记录中，不得继续作为代码、测试 helper、内部 contract 或运行时状态的主语义命名。
- 若从旧实现迁移概念，迁入新目录时必须按新语义重命名，不能把旧语义带进 v2/v3。

## 5. 任务队列

状态只能使用：`待开始`、`进行中`、`完成`、`阻塞`。同一时间只能有一个切片处于 `进行中`。

自动执行必须按表格顺序处理最早未完成切片：

- 如果最早未完成切片是 `阻塞`，必须停止并向用户说明阻塞，不得跳到后续 `待开始` 切片。
- 如果最早未完成切片是 `进行中`，继续该切片。
- 如果最早未完成切片是 `待开始`，先把它改为 `进行中` 并提交或与本切片首个实现提交同切片提交，然后只执行该切片。

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
| 8. tui-v3 input/render/UI 边界 | 待开始 | `termx-tui-v3/` | 自有 `InputEvent`、semantic intent、RenderVMBuilder、Renderer、hit regions、lipgloss style helper；无 Bubble Tea contract |
| 9. services 与集成 | 待开始 | `termx-tui-v3/`、受限联动范围 | core client、terminal service、session、clipboard、真实 adapter 接入；fake 与最小 e2e 通过 |
| 10. 收口与迁移入口 | 待开始 | 受限联动范围 | 新路径可运行；必要 CLI/adapter 入口接入；旧 helper/fixture 只在明确不再需要时删除 |

当前下一步：执行切片 8。

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

### 6.3 tui-v3 harness

必须逐步覆盖：

- input key/mouse -> semantic intent。
- reducer message -> state + effects。
- effect result 回到 message path。
- AppRuntime message 顺序、timer、batch、cancel、quit。
- TerminalHost input event 转换和 FrameSink contract。
- HistoryStore latest replace、older prepend、empty exhausted、stale response、cols mismatch。
- CopyMode cursor、viewport、selection、clipped span、multi logical line copy。
- RenderVM live mode 与 copy mode projection 分流。
- copy mode 缺 authoritative window 时不得从 live surface fallback。
- lipgloss/v2 style helper 宽度、裁剪、ANSI 安全性。

## 7. 测试准入

每个有效切片提交前必须运行与切片相关的测试。新 module 建立后优先使用模块内命令：

- core-v2 改动：在 `termx-core-v2/` 运行 `go test ./... -count=1`。
- tui-v3 改动：在 `termx-tui-v3/` 运行 `go test ./... -count=1`。
- protocol 改动：在 `internal/` 运行 `go test ./protocol/... -count=1`，在 `termx-proto/` 运行 `go test ./... -count=1`。
- workspace 或受限联动改动：运行所有相关模块测试。
- 文档-only 改动：至少运行 `git diff --check`。

如果测试无法运行，必须在最终说明和必要时在提交前记录原因。不能把真实语义失败当作偶发失败。

## 8. 自动执行规则

- 每次开始工作先检查 `git status --short --branch`。
- 按任务队列表格顺序选择最早未完成切片；遇到 `阻塞` 必须停止，遇到 `进行中` 继续，遇到 `待开始` 必须先标为 `进行中`。
- 切片范围不清时，先更新本文件，不要自行扩大范围。
- 每个切片尽量保持小而可提交；不要跨多个切片堆叠未提交改动。
- 完成切片后更新本文件中对应状态和必要的下一步说明，与实现同提交。
- 如果发现设计文档需要变化，必须与实现同切片更新，或先提交设计更新。
- 如果遇到阻塞，必须把对应切片状态改为 `阻塞` 并说明阻塞条件；不要继续扩散到其他目录。

## 9. 提交规则

- 当前工作区未提交改动必须先整理并提交，再继续后续开发。
- 每个有效变动必须形成中文提交。
- 不允许长期堆积未提交改动。
- 不得 amend commit，除非用户明确要求。
- 不得 revert 用户或其他代理的未提交改动；若冲突，先停下说明。
- 删除旧代码必须和对应新语义或 harness 同切片提交。

## 10. 当前状态

- 当前分支主线已切换到 `termx-core-v2/` 与 `termx-tui-v3/`。
- 切片 1 已完成：`termx-core-v2/` 与 `termx-tui-v3/` 已建立 Go module、最小包结构、空实现和 smoke tests，并已加入 `go.work`。
- 切片 2 已完成：`termx-core-v2/history` 已建立 logical line、单一 store、committed index、mutable frontier、内存 storage backend 和基础 harness。
- 切片 3 已完成：`termx-core-v2/history` 已建立显式历史事件入口，覆盖 write、seal、mutate、reset、commit、reclaim、hide、truncate、alt-screen、process-exit、resize 与 non-history boundary harness。
- 切片 4 已完成：`termx-core-v2/history` 已建立 HistoryWindow authoritative projection，覆盖 latest replace、older prepend、cursor、token、generation、clipping、logical line span 和 mutable frontier 混合投影 harness。
- 切片 5 已完成：`history.window` protocol contract 已扩展 token/generation/logical cursor/boundary 与 row-to-line 映射字段，legacy snapshot/grid viewport 在代码注释中明确为实时兼容投影。
- 切片 6 已完成：`termx-tui-v3/app` 已建立自有 `AppRuntime`、`Msg`、`Effect`、`EffectRunner`、`TerminalHost` fake、`FrameSink` contract 和 runtime harness。
- 切片 7 已完成：`termx-tui-v3/state` 已建立 reducer-owned `HistoryStore` 与 `CopyModeStore`，覆盖 latest、older、stale、resize、selection harness。
- 旧 `termx-core/` 与 `tuiv2/` 的历史修补进度不再作为当前主线状态；如需查阅只能通过 git 历史或只读参考。
- 下一步执行切片 8：建立 tui-v3 input/render/UI 边界。
