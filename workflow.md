# 工作流：termx-tui-v3 styled chrome renderer 视觉对齐

本文件是当前分支唯一有效的活动驱动文件。后续所有分析、实现、测试、提交都必须先读取本文件，并以本文件为准。

本文件只记录目标、范围、硬约束、任务队列、测试准入和提交规则。架构正文不写在本文件里，分别以 `termx-core-v2/docs/architecture.md`、`termx-tui-v3/docs/architecture.md`、`termx-tui-v3/docs/ui-interaction-spec.md`、`termx-tui-v3/docs/render-architecture.md` 和 `termx-cli/docs/v2-v3-switch-audit.md` 为准。

本文件必须保持全中文。若本文件与旧说明、聊天记录、旧代码行为或局部假设冲突，默认以本文件为准；若技术设计需要变化，必须先更新本文件或与实现同切片更新。

## 1. 当前唯一目标

在默认入口已经切到 `termx-core-v2/` 与 `termx-tui-v3/`，且外部 viewport / resize / Unicode 线框收口已完成的基础上，把 `termx-tui-v3` 推进到 styled chrome renderer 阶段，使默认 TUI 的 shell chrome、pane chrome、边框、状态栏、toast 和 overlay 达到 `tuiv2` 截图所示的视觉等级。

当前事实：

- `go run ./termx-cli/cmd/termx`、默认 `termx daemon`、`termx attach`、`termx new/ls/kill/rm` 已使用 `termx-core-v2` 与 `termx-tui-v3`。
- `termx-tui-v3` 已有自有 runtime、input、state、services、terminalhost、copy mode 和最小 render 骨架，且不依赖 Bubble Tea contract。
- `termx-tui-v3/render` 已建立最小 render framework / content renderer 分层，`RenderVMBuilder` 输出 `ShellVM`，`Renderer.RenderResult` 通过 workbench shell、panel、overlay 和 toast 合成 `RenderResult`；旧 `RenderVM{Lines, Status}` 字段只作为临时兼容投影存在。
- `termx-tui-v3/state` 已拥有 reducer-owned shell、workspace/tab/pane 最小树、panel presentation、header/footer visibility、toast/message 和 Terminal Picker overlay 状态模型。
- 外部 terminal emulator resize 已作为 runtime message 流进入 reducer-owned state；`RenderVMBuilder` 已把外部 viewport 作为 shell layout 尺寸 truth；renderer 已按已知 viewport 填满输出；active terminal resize 已从 panel content rect 计算；默认 UI chrome 已收敛到 Unicode box drawing。
- 当前 `Frame` 仍会把 `RenderResult` 压成纯文本 `[]string`，真实 `FrameSink` 不保留 ANSI SGR、active accent、inactive muted border、背景色或 bold 等样式；这不是 `tuiv2` 截图级别的 UI。
- 当前默认 UI 只满足“Unicode glyph 存在”，不满足 styled chrome renderer 要求；后续不能再把无样式纯文本线框当作视觉验收完成。
- 当前主线不是回到旧 `tuiv2` 原地修补，也不是直接复制 `tuiv2/render`；`tuiv2` 只能作为只读视觉目标和经验参考，新实现必须服从 `termx-tui-v3` 的 render framework + content renderer 架构。

完成定义：

- `termx-tui-v3` render 主路径使用 `render framework + content renderer` 作为正式结构。
- `RenderResult` 是唯一主输出，必须保留 styled cell / styled line / metadata；纯字符串、测试快照和真实 TTY ANSI 输出都只是适配层。
- 真实 `FrameSink` 必须能写出 ANSI styled frame，不能丢失边框色、active/inactive 状态、header/footer 背景、toast/overlay 样式和必要 reset。
- 默认 TUI 进入后不再只显示裸文本 `live surface pending` 或 `live: termx-main`，而是显示 workbench shell、header/footer、panel chrome 和 panel content。
- 默认 workbench chrome 必须达到 `tuiv2` 截图级别：整屏布局、稳定 top/bottom bar、pane 细线边框、active pane accent、inactive pane muted、pane 标题/状态/action 槽位、内容区明确裁切。
- 最小 render framework 阶段同时支持 card panel 与 split line 两种 tiled panel 呈现，并至少覆盖双 pane 横向和纵向分割。
- header/footer hide 必须真实影响 layout，隐藏后 body 回收空间，workspace、tab、mode、notice/error 仍可通过短标识、toast 或 Help 入口恢复识别。
- toast 支持真实渲染和基础生命周期：severity、pending/progress、auto dismiss、close current、clear all 和窄屏退化。
- pane 分屏、关闭、focus、zoom、resize/size change 必须作为统一 semantic command 落地，快捷键、鼠标 hit region、测试入口和后续 CLI mini command 都只能调用同一命令契约。
- pane 结构命令至少覆盖横向/纵向 split、close pane、close and kill terminal、focus/activate、zoom/unzoom、按方向和步长 resize、按比例或固定尺寸 set size、balance/equalize，以及 card/split 呈现切换；结构变化必须触发 layout measurement、active terminal content rect resize、copy mode rebind 和 toast 反馈。
- 所有 panel、split line、header/footer、toast、overlay 和 content slot 的布局、裁切、填充、对齐必须按 terminal cell display width 计算，emoji、CJK、combining mark、ANSI 样式和 host width ambiguous cluster 不得破坏边框或分割线。
- Terminal Picker 状态激活时有 overlay 或明确占位渲染路径；Terminal Pool 与 Workbench Tree 完整页面在 framework 成型后再接入。
- copy mode 仍只消费 core-v2 authoritative `HistoryWindow`，缺 window 或绑定不一致时显示 pending/empty/error，不得从 live surface、snapshot、grid viewport 或 local VTerm scrollback fallback。
- `go run ./termx-cli/cmd/termx` 默认路径继续使用 core-v2/tui-v3，不得重新引入旧 `termx-core` 或 `tuiv2` 默认依赖。
- 外部 terminal emulator 的 cols/rows 必须成为独立 reducer-owned viewport truth，不能混用 `Session.Cols/Rows` 或 `Surface.Cols/Rows` 充当 UI canvas truth。
- 真实 `TerminalHost` 必须在进入 TUI 后提供初始尺寸，并把外部窗口变化作为 resize event/message 进入 `AppRuntime`；fake host 必须能 deterministic 地注入 resize。
- renderer 输出必须以当前 viewport 填满整个上下文；当 viewport 已知时不得输出超过 viewport cols 的行，也不得因为默认 80 列导致宿主终端自动换行破坏边框。
- `RenderVMBuilder` 和 renderer 必须通过纯 layout measurement 得到 body、panel、content、overlay、toast 和 hit region rect；renderer 只消费 view-model/layout plan，不直接读取 host、service 或 core client。
- 发给 core-v2 terminal 的 resize 必须使用 active pane content rect 的 cols/rows，而不是外部 terminal emulator 总尺寸；card panel、split line、header/footer hide 和后续 floating/overlay 都必须能影响 content rect。
- host resize、header/footer hide、panel presentation 切换或 split 变化导致 copy content cols 改变时，copy mode 必须 invalid/rebind authoritative `HistoryWindow`，不得继续使用旧 bound cols，也不得从 live surface fallback。
- tiled pane 默认视觉目标使用 `tuiv2` 风格的 square Unicode box drawing：`┌┐└┘─│├┤┬┴┼`，并通过 style 区分 active/inactive；floating、modal、toast 可以使用 rounded card 风格，但也必须是 styled chrome。
- ASCII `+ - |` 只能出现在测试说明或兼容文档中，不得作为默认 UI chrome。

## 2. 技术设计基准

- core-v2 架构基准：`termx-core-v2/docs/architecture.md`。
- tui-v3 架构基准：`termx-tui-v3/docs/architecture.md`。
- tui-v3 UI 交互基准：`termx-tui-v3/docs/ui-interaction-spec.md`。
- tui-v3 render framework 基准：`termx-tui-v3/docs/render-architecture.md`。
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

### 4.6 render framework 最小阶段约束

- `render framework + content renderer` 是正式方向，不得继续把 terminal 内容写成 renderer 主抽象。
- `RenderResult` 是唯一 render 主输出；字符串、测试和真实 TTY 输出都只能作为适配层。
- 最小阶段必须同时处理 card panel 与 split line，不能只做 card panel。
- 最小阶段必须处理 header/footer hide 的真实 layout 效果，不能只保留 VM 字段。
- 最小阶段必须处理 toast 基础生命周期，不能只做静态文本。
- Terminal Pool 与 Workbench Tree 完整页面不作为最小阶段阻塞项，但 Terminal Picker 状态必须有 overlay 或明确占位渲染路径。
- `Ctrl-f` 进入 Terminal Picker、`Ctrl-v` 进入 Display / Copy 是已定产品基准。
- card/split 切换、header/footer hide、toast close current、toast clear all 的具体快捷键尚未拍板；实现可以先提供 semantic action、reducer message、hit region 和测试入口，但不得临时发明新的产品快捷键并写死。
- 鼠标和 hit region 语义可以先按稳定 action token 落地，具体视觉文案可以后续细化。
- 所有 UI chrome 和 content slot 的宽度计算必须使用 ANSI-aware / grapheme-aware / cell-aware helper，不得用 byte length 或 rune count 作为可见宽度。
- 旧 `tuiv2` 的 width safety 经验只能迁入为 v3 render primitive 和 harness，不能迁入旧 runtime/model/cursor writer 结构。
- 最小阶段不得引入通用 widget/plugin UI 框架，也不得引入 Bubble Tea contract。

### 4.7 外部 viewport 与 resize 约束

- `ViewportStore` 或等价 reducer-owned state 必须表达外部 terminal emulator 当前可绘制区域；它是 UI canvas truth。
- `TerminalSessionStore` 和 `TerminalSurfaceStore` 表达 PTY/session/live surface 状态；它们不是 UI canvas truth。
- `HostResizeMsg` 或等价 message 是外部尺寸进入 app 的唯一写状态入口；service、renderer、terminalhost 不得直接修改 reducer-owned state。
- 初始 render 必须使用 host 初始尺寸；若真实 host 无法获得尺寸，必须显式 fallback 并在测试中覆盖，而不是无条件默认 `80x24`。
- layout measurement 必须是纯函数；同一 `ShellVM + viewport` 必须稳定产出相同 body/panel/content/overlay/toast rect 和 hit region plan。
- renderer 必须严格按 plan 绘制；content renderer 只能在分配给它的 content rect 内绘制和裁切。
- active terminal resize 必须由 app 根据 layout plan 触发 effect，并做尺寸去重，避免每帧重复向 core-v2 发送相同 resize。
- copy mode 绑定的 cols 必须等于 copy content rect width；宽度变化必须让旧 window 失效并重新请求 authoritative window。
- Unicode box drawing glyph 统一按 cell width 1 处理；emoji、CJK、combining mark、ANSI 样式和 ambiguous width 不得覆盖、推开或截断边框。

### 4.8 styled chrome renderer 约束

- styled chrome renderer 是当前新阶段正式方向；不得继续把纯文本 `[]string` frame 作为真实 TTY 主输出。
- `RenderResult` 必须保留 styled cell、styled line 或等价结构，直到最终 TTY serializer；不得在 renderer 主路径中过早丢弃 style。
- `FrameSink` 必须支持 ANSI styled frame，包含 SGR foreground/background/bold/reset 和 cursor metadata；纯文本 frame 只能作为测试快照或兼容适配层。
- renderer 内部必须有 cell matrix 或等价 compositor，能表达 text、width、style、owner/layer、continuation、safe flag；不能继续以 `[]string` 作为 canvas truth。
- theme 必须表达 host-aware palette、accent、success、warning、danger、info、panel border、muted border、chrome bg/fg、active/inactive pane chrome 等 token。
- active pane border 必须使用 accent/strong style，inactive pane border 必须使用 muted style；active/inactive 状态不能只靠标题文字表达。
- tiled pane 默认视觉必须对齐 `tuiv2` 截图级别：square Unicode 边框、细线分割、pane top chrome 槽位、top/bottom bar token、状态/动作短 token、可见焦点态。
- card panel、split line、floating/modal/toast 都必须使用 styled chrome；只出现 Unicode glyph 但无颜色/样式不算完成。
- styled chrome 的宽度计算必须 ANSI-aware；带 ANSI 的标题、token、toast、overlay 和内容裁切后 display width 必须仍等于目标 rect。
- `tuiv2/render` 只能作为只读参考：可参考 `drawStyle`、`composedCanvas`、theme token、pane chrome slot、ANSI serializer 和宽度安全思路；不得复制旧 runtime/model、VisibleRenderState 大 bag、cache key 或业务状态耦合结构。
- 当前阶段优先让 shell/chrome/pane/frame 达到视觉等级；terminal live/copy history 内容 renderer 深化可以分阶段推进，但内容必须被 content rect 裁切，不能破坏 styled chrome。

### 4.9 pane 结构命令约束

- pane split、close、resize、zoom、focus 等结构操作必须 command-first，不得只作为快捷键 handler 的局部逻辑。
- 命令来源可以是 pane mode、resize mode、鼠标 hit region、测试 harness、后续 CLI mini command 或 command palette，但最终必须进入同一组 semantic command。
- semantic command 必须携带稳定 action id、target workspace/tab/pane、orientation、direction、delta、ratio、absolute size、confirm policy 等必要参数；不得把显示文案或具体按键当作业务语义。
- reducer 负责 workspace/tab/pane/floating 结构状态变化；service 不得直接改 reducer-owned state；需要创建、kill、resize terminal 时必须通过 effect/message 回到主循环。
- split、close、resize、balance、zoom 和 panel presentation 切换后，必须重新测量 layout plan，并以 active content rect 触发 terminal resize 去重。
- active copy pane 的 content width 变化后，必须 invalid/rebind authoritative `HistoryWindow`，不得沿用旧 cols，也不得 fallback 到 live surface。
- close pane 与 close and kill terminal 必须区分：关闭 pane 只移除当前工作位；kill terminal 是破坏性 terminal lifecycle 操作，必须能走确认或明确 danger 语义。
- 后续 CLI mini command 只能作为 command adapter，不能绕过 TUI reducer、layout measurement、terminal resize、copy rebind 或 toast feedback。

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
| 18. CLI v3 命令组与 daemon 骨架 | 完成 | `termx-cli/`、`Makefile` | 增加显式 `termx v3` 实验命令组；`termx v3 daemon` 以前台方式启动 core-v2 server；提供非交互 v3 smoke 入口验证 tui-v3 可被 CLI 装配运行；默认 root、`daemon`、`attach` 仍保持旧入口；CLI 测试和迁移 smoke 通过 |
| 19. CLI v3 daemon 连接与自动启动基础 | 完成 | `termx-cli/`、`termx-core-v2/` 按需 | v3 实验入口能连接已存在 core-v2 daemon；需要 daemon 时只能自动启动 core-v2 daemon，不能复用旧 `termx-core` 自动启动路径；socket、日志路径、启动失败和关闭行为有测试 |
| 20. CLI v3 本地控制命令 | 完成 | `termx-cli/`、`termx-core-v2/`、`internal/protocol/` 按需 | `termx v3 new`、`termx v3 ls`、`termx v3 kill`、`termx v3 rm` 通过 core-v2 protocol service 工作；命令输出和错误语义与旧默认入口可对照；CLI tests 覆盖本地单 session |
| 21. CLI v3 attach/TUI 装配 | 完成 | `termx-cli/`、`termx-tui-v3/` 按需 | `termx v3 attach <id>` 使用 tui-v3 `TerminalHost`、protocol adapters 和 `NewInteractiveRuntime` 装配真实交互路径；非交互环境给出明确错误或运行专用 harness；不得调用 `tuiv2/app.RunWithClient` |
| 22. v3 本地端到端 smoke | 完成 | `termx-cli/`、`termx-core-v2/`、`termx-tui-v3/`、`internal/protocol/`、`Makefile` 按需 | 建立可重复的本地单 session smoke：启动 core-v2 daemon、创建 PTY、读取 live surface、发送 input、处理 resize、请求 `history.window`、触发 copy mode 主路径；默认入口仍未切换 |
| 23. 配置、日志与状态路径收口 | 完成 | `termx-cli/`、`termx-shared/` 按需 | v3 实验入口的 socket、log、config、state 路径语义明确且有测试；v3 路径不得为了配置加载依赖 `tuiv2/shared`；与旧默认入口的差异写入迁移审计 |
| 24. remote 兼容与隔离结论 | 完成 | `termx-cli/`、`termx-core-v2/`、`termx-remote/`、`termx-shared/` 按需 | remote 命令在默认切换前有明确结论：已迁移到 core-v2 extension，或显式保留 legacy/fallback 边界；不能把未完成 remote 能力伪装成默认已兼容 |
| 25. 默认 root 入口切换 | 完成 | `termx-cli/`、`Makefile`、`go.work` 按需 | `go run ./termx-cli/cmd/termx` 默认 root TUI 使用 tui-v3；默认 root 不再调用旧 `runTUIv2`；旧 root 若保留必须移动到显式 legacy/fallback 入口；`--help` 可编译运行 |
| 26. 默认 daemon/attach/control 切换 | 完成 | `termx-cli/`、`termx-core-v2/`、`termx-tui-v3/`、`internal/protocol/`、`Makefile` 按需 | `termx daemon`、`termx attach`、`termx new`、`termx ls`、`termx kill`、`termx rm` 默认使用 core-v2/tui-v3；旧路径只允许显式 legacy/fallback；本地单 session 主路径回归通过 |
| 27. 默认入口回归验收 | 完成 | `termx-cli/`、`termx-core-v2/`、`termx-tui-v3/`、`internal/protocol/`、`termx-proto/`、`Makefile` 按需 | 默认入口切换后运行 CLI tests、core-v2 tests、tui-v3 tests、protocol tests、`make test-v2-migration` 和默认入口非交互 smoke；remote 未完成项必须在文档和任务结论中显式保留 |
| 28. 旧默认依赖清理与冻结 | 完成 | `termx-cli/`、`workflow.md`、`AGENTS.md`、必要顶层说明文档 | `termx-cli` 默认路径不再 import 旧 `termx-core`/`tuiv2`；旧目录冻结状态明确；依赖守卫、测试入口和文档完成；切片 28 完成后本轮默认入口切换目标才算完成 |
| 29. tui-v3 UI 交互规格文档 | 完成 | `termx-tui-v3/docs/`、`workflow.md`、只读参考 `tuiv2/docs/` 和 `tuiv2/` | 新增独立中文文档定义 tui-v3 的产品级 UI 交互、界面结构、功能清单、页面线稿、快捷键与鼠标交互、宽窄屏退化和硬约束；可参考 tuiv2 的页面设计，但不得写实现方案、渲染算法、Go 包拆分或从旧实现迁移代码的技术步骤 |
| 30. tui-v3 UI 交互规格增量 | 完成 | `termx-tui-v3/docs/`、`workflow.md` | 在 UI 交互规格中补充 pane chrome 模式：card panel 与 tmux-like split line 两种 tiled pane 呈现；支持隐藏全局 header/footer 以提升终端内容利用率；floating pane 保持独立带边框；补充右上角现代化弹出消息系统；不得写实现方案 |
| 31. tui-v3 render framework 架构文档 | 完成 | `termx-tui-v3/docs/`、`workflow.md`、只读参考 `tuiv2/docs/` 和 `tuiv2/render/` | 新增独立中文文档定义 tui-v3 render framework 与 content renderer 的职责边界、数据流、层级合成、panel/overlay/floating/toast/content 分类、禁止事项和分阶段落地计划；文档完成后必须由子 Agent 审核，审核结论需纳入最终交付说明 |
| 32. tui-v3 render framework 拍板结论落档 | 完成 | `termx-tui-v3/docs/`、`workflow.md` | 把用户拍板结论写入 render 架构文档：`render framework + content renderer` 是正式方向；最小 render framework 阶段必须同时处理 card panel 与 split line、header/footer hide、toast 基础生命周期；Terminal Pool 与 Workbench Tree 在 framework 成型后再接入 |
| 33. render framework 契约与 harness 基线 | 完成 | `termx-tui-v3/render/`、`termx-tui-v3/docs/`、`workflow.md` | 建立 `RenderResult`、cursor、blink、metadata、rect/layer/cell 或 line primitive、content renderer contract、content kind、panel/floating/overlay/toast/header/footer VM 基础类型；建立 width-safe helper / primitive，覆盖 ANSI-aware、grapheme-aware、cell-aware 的宽度、裁切、填充和对齐；保留兼容适配层让现有 runtime 可编译；harness 覆盖 `RenderResult` 单一路径、string/frame adapter 一致、copy mode 无 authoritative history 不 fallback、emoji/CJK/combining mark/ANSI styled text 不破坏 row width、Bubble Tea contract 不进入 render |
| 34. tui-v3 shell/panel/toast 状态模型 | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/app/`、`termx-tui-v3/render/` 按需 | 在 reducer-owned state 中加入 shell、workspace/tab/pane 最小树、panel presentation、active pane、header/footer visibility、toast/message、Terminal Picker overlay 占位状态；提供 action/reducer harness 覆盖 card/split 切换、header/footer hide、toast add/auto-dismiss/close/clear、Terminal Picker open/close；未拍板快捷键只能通过 semantic action 或测试消息进入 |
| 35. RenderVMBuilder 分层重建 | 完成 | `termx-tui-v3/render/`、`termx-tui-v3/state/` | 把当前大 `RenderVM{Lines, Status}` 替换为 shell/body/layout/panel/content/overlay/toast/cursor 子 VM；builder 不得退化成大 bag；copy-history VM 必须校验 terminal id、bound token 和 cols；缺 authoritative window 时只生成 pending/empty/error content；live content 只消费 `TerminalSurfaceStore` |
| 36. render framework 最小渲染器 | 完成 | `termx-tui-v3/render/`、`termx-tui-v3/terminalhost/` 按需 | 实现最小 render framework：viewport layout、header/footer 占位与隐藏、card panel、split line、最小双 pane 横向/纵向分割、panel chrome、content renderer dispatch、toast 层、Terminal Picker overlay/placeholder、hit region 合成、cursor 归属、最终 `RenderResult -> FrameSink` 适配；harness 覆盖宽窄屏、裁切、层级优先级、toast 不改变 body layout、opaque overlay cursor，以及 panel 标题/content/toast 中的 emoji、CJK、combining mark、ANSI styled text 不破坏边框、split line 或 row width |
| 37. app/input 接线与交互入口 | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/input/`、`termx-tui-v3/state/`、`termx-tui-v3/render/` 按需 | 默认 runtime 使用新的 render framework 主路径；`Ctrl-f` 打开 Terminal Picker overlay/placeholder，`Ctrl-v` 进入 Display/Copy 并在缺 authoritative history 时显示 panel 内 pending/empty；card/split、header/footer hide、toast close/clear 先通过 semantic action、hit region 或测试消息接入，不临时发明未拍板快捷键；live input、resize、copy mode 原有主路径不回退 |
| 38. 默认入口 UI smoke 与回归验收 | 完成 | `termx-tui-v3/`、`termx-cli/`、`Makefile` 按需 | 默认 `go run ./termx-cli/cmd/termx` 和非交互 smoke 不再把裸文本 frame 当作可用界面；smoke 覆盖 workbench shell、header/footer、card/split、header/footer hide、toast、Terminal Picker placeholder、copy pending/empty、live surface panel content、emoji/CJK/ANSI 宽度安全；运行 `cd termx-tui-v3 && go test ./... -count=1`、`cd termx-cli && go test ./... -count=1` 和按需 `make test-v2-migration` |
| 39. render framework 收口与文档同步 | 完成 | `termx-tui-v3/docs/`、`workflow.md`、`termx-tui-v3/` | 同步实现结果到 render 架构和 UI 交互文档，记录已落地、未落地和后续 Terminal Pool / Workbench Tree / floating / overlay 深化切片；删除或重命名过时的裸文本 render helper/test 语义；确认旧 `tuiv2` 仍只读参考，默认路径不引入旧依赖 |
| 40. viewport state 与 host resize contract | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/app/`、`termx-tui-v3/terminalhost/`、`termx-tui-v3/docs/` 按需 | 建立 reducer-owned 外部 viewport 状态；扩展 `TerminalHost` contract 以提供初始尺寸和 resize event 流；真实 host 监听外部 terminal emulator resize，fake host 可注入 resize；`HostResizeMsg` 或等价 message 是尺寸入 state 的唯一入口；harness 覆盖初始尺寸、重复尺寸去重、fake resize、真实 host size 查询和 runtime resize 后重绘 |
| 41. layout measurement 与 viewport 驱动 renderer | 完成 | `termx-tui-v3/render/`、`termx-tui-v3/state/`、`termx-tui-v3/app/` 按需 | 建立纯 layout measurement/plan：输入 `ShellVM + viewport`，输出 body、panel、content、overlay、toast、hit region 和 cursor rect；`RenderVMBuilder` 使用外部 viewport 作为 `Layout.Viewport` truth；renderer 严格按 plan 绘制并填满 viewport；已知 viewport 下不得默认 80 列输出或输出超过 viewport cols；harness 覆盖窄屏、宽屏、header/footer hide、card/split 双 pane、overlay/toast 不改变 body layout 和所有行 display width 等于 viewport cols |
| 42. active content rect 驱动 terminal resize | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/render/`、`termx-tui-v3/services/`、`termx-cli/` 按需 | app 根据 layout plan 计算 active terminal content rect，并只用 content rect cols/rows 向 core-v2 terminal 发送 resize；外部 viewport、header/footer hide、card/split 切换和 split 变化导致 content rect 变化时触发 resize effect，重复尺寸不重复发送；attach 初始尺寸也以 content rect 为准；harness 覆盖 card 扣边框、split 分割、header/footer 隐藏回收空间、resize effect 去重和真实 protocol adapter 参数 |
| 43. copy mode resize rebind 与 history window cols | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/state/`、`termx-tui-v3/render/`、`internal/protocol/` 按需 | copy mode 绑定 cols 使用 copy content rect width；host resize、layout chrome 变化或 active copy panel content width 变化时 invalid 旧 bound token/window 并重新请求 authoritative latest window，期间显示 pending/empty/error，不从 live surface fallback；older request 和 stale guard 使用新 cols；harness 覆盖 resize 后旧 window 不再渲染、latest replace 重新绑定、新旧 response stale、selection/cursor 清理和 `HistoryWindow` 请求 cols |
| 44. Unicode 线框与 cell-width 硬化 | 完成 | `termx-tui-v3/render/`、`termx-tui-v3/docs/`、只读参考 `tuiv2/render/` | 默认 UI chrome 去除 ASCII `+ - |` 线框；card/modal/floating 使用 `╭╮╰╯─│`，pane/split 使用 `┌┐└┘─│├┤┬┴┼` 同类连续 box drawing glyph；split 连接点按邻接线合成；标题、content、toast、overlay 中 emoji、CJK、combining mark、ANSI 样式不得破坏边框；harness 覆盖 Unicode glyph、连接点、窄屏裁切、右边框不丢失、无默认 ASCII chrome、行宽恒等和旧 `tuiv2` 只读参考结论 |
| 45. 默认入口 resize/UI 验收与文档收口 | 完成 | `termx-tui-v3/`、`termx-cli/`、`Makefile`、`workflow.md`、`termx-tui-v3/docs/` 按需 | 默认 `go run ./termx-cli/cmd/termx`、`termx attach` 和非交互 smoke 覆盖外部 viewport 初始尺寸、resize 重绘、content rect terminal resize、copy mode rebind、Unicode 线框、card/split/header/footer/toast/overlay 宽度安全；更新 render/UI 文档记录本轮已落地和后续 Terminal Pool / Workbench Tree / floating 深化边界；运行 `cd termx-tui-v3 && go test ./... -count=1`、`cd termx-cli && go test ./... -count=1`，并按需运行 `make test-v2-migration` |
| 46. styled chrome renderer 设计落档 | 完成 | `termx-tui-v3/docs/`、`workflow.md`、只读参考 `tuiv2/render/` | 把 styled chrome renderer 作为新阶段正式目标写入 render/UI 文档和 workflow；明确 `tuiv2` 截图级视觉目标、不得复制旧实现、优先 shell/chrome/pane/frame，内容 renderer 后续分阶段深化；文档-only 运行 `git diff --check` |
| 47. styled RenderResult / Frame contract | 完成 | `termx-tui-v3/render/`、`termx-tui-v3/terminalhost/`、`termx-tui-v3/docs/` 按需 | `RenderResult`、`Frame`、`FrameSink` 建立可保留 styled cell / styled line / ANSI line 的 contract；保留纯文本快照适配但不作为真实 TTY 主输出；真实 `FrameSink` 可写 ANSI styled frame 并正确 reset；harness 覆盖 ANSI SGR 保留、plain snapshot 等宽、cursor metadata 不丢、旧 smoke 可适配 |
| 48. cell matrix compositor 与 ANSI serializer | 完成 | `termx-tui-v3/render/`、`termx-tui-v3/terminalhost/` 按需 | renderer canvas 从纯字符串升级为 cell matrix 或等价 compositor，支持 text、width、style、owner/layer、continuation/safe flag、wide-cell footprint 清理和 ANSI serializer；harness 覆盖 styled glyph、wide char/emoji/combining/ANSI 裁切后行宽恒等、style reset、右边框不丢、无默认 ASCII chrome |
| 49. theme token 与 host-aware palette | 完成 | `termx-tui-v3/render/`、`termx-tui-v3/docs/` 按需 | 建立 v3 theme token：host fg/bg、chrome bg/fg、accent、success/warning/danger/info、panel border、muted border、active/inactive pane、toast/overlay token；可参考 tuiv2 theme 思路但不迁入旧大状态；harness 覆盖 active/inactive border 色不同、semantic token 输出 ANSI、主题 fallback 稳定 |
| 50. pane chrome 视觉对齐 | 完成 | `termx-tui-v3/render/`、`termx-tui-v3/state/` 按需 | tiled pane 默认达到 tuiv2 截图级 styled chrome：square Unicode 细线边框、active pane accent、inactive pane muted、标题/状态/action 槽位、pane top chrome token、split/card 两种模式均保持 content rect 和 hit region；harness 覆盖单 pane、多 pane 横纵 split、active 切换、标题含宽字符、ANSI 样式不破坏边框 |
| 51. shell chrome / top bar / bottom bar 视觉对齐 | 完成 | `termx-tui-v3/render/`、`termx-tui-v3/state/`、`termx-cli/` 按需 | header/footer 从占位文本升级为 styled top/bottom bar：workspace/tab/mode/hint/status token、active tab/创建 token、右侧摘要、隐藏 header/footer 后状态仍可达；harness 覆盖 ANSI 背景填满整行、token 宽度安全、hide 后 body 回收、默认入口 smoke 包含 styled top/bottom bar |
| 52. toast / overlay styled chrome | 完成 | `termx-tui-v3/render/`、`termx-tui-v3/state/` 按需 | toast 和 Terminal Picker overlay 使用 styled chrome：border/background/severity token/pending token/close action region/opaque cursor 归属；floating/modal 仍可 rounded card 但必须有独立样式；harness 覆盖 toast 不改 body layout、overlay 遮挡优先级、窄屏退化、ANSI reset 和宽字符安全 |
| 53. pane 结构命令 contract | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/app/`、`termx-tui-v3/input/`、`termx-tui-v3/docs/` 按需 | 建立 pane structural command/semantic action contract，覆盖 split、close、close and kill、focus、zoom、resize、set size、balance、panel presentation；快捷键、鼠标、测试和后续 CLI mini command 都只能作为 adapter 调用该 contract；harness 覆盖 command 参数、target pane、无效目标、confirm policy 和 reducer/effect 边界 |
| 54. pane split / close / zoom 状态语义 | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/app/`、`termx-tui-v3/render/` 按需 | reducer-owned pane tree 支持横向/纵向 split、close pane、close and kill terminal effect、focus/activate、zoom/unzoom；结构变化保持 terminal binding、active pane、tab invariants、toast 反馈和 render VM 投影；不得直接修改 service state |
| 55. pane resize / size change geometry | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`termx-tui-v3/services/` 按需 | pane resize 支持按方向/步长调整、按比例或固定尺寸 set size、balance/equalize；layout measurement 产出稳定 panel/content rect，外部 viewport、header/footer hide、card/split、resize 变化都触发 active terminal content rect resize 去重和 copy mode authoritative window rebind |
| 56. pane command adapters：快捷键、鼠标、CLI mini | 进行中 | `termx-tui-v3/input/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`termx-cli/` 按需 | pane mode、resize mode、鼠标 hit region 和 CLI mini command adapter 调用同一 semantic command；不临时发明未拍板产品快捷键，但保留稳定 command id、测试入口和后续 CLI mini 解析边界；UI chrome region 优先于 terminal mouse forwarding |
| 57. 默认入口 styled UI + pane command smoke 与视觉验收 | 待开始 | `termx-tui-v3/`、`termx-cli/`、`Makefile` 按需 | `termx v3 smoke`、`termx v3 e2e-smoke` 和默认入口非交互 smoke 覆盖 styled frame 与 pane command：ANSI SGR 存在、active/inactive pane 样式不同、top/bottom bar styled、pane border continuous、split/close/resize/zoom 后 frame 行宽恒等、content rect terminal resize、copy rebind；运行 `cd termx-tui-v3 && go test ./... -count=1`、`cd termx-cli && go test ./... -count=1`、`make test-v2-migration` |
| 58. styled chrome / pane command 文档收口与后续内容 renderer 边界 | 待开始 | `termx-tui-v3/docs/`、`workflow.md` | 同步 styled chrome 与 pane command 已落地、未落地和后续 terminal-live/copy-history/Terminal Pool/Workbench Tree/floating 内容 renderer 深化边界；明确当前阶段完成 chrome/frame 视觉等级和 pane 结构命令基础，不宣称内容 renderer 完整；文档-only 至少运行 `git diff --check`，若同切片有代码则运行相关测试 |

当前下一步：切片 46 已完成。自动执行必须从切片 47 开始推进 styled RenderResult / Frame contract，不得跳过到内容 renderer 或 Terminal Pool。

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
- 外部 viewport 初始尺寸进入 reducer-owned state。
- host resize event 通过 message path 触发重绘。
- fake host deterministic 注入 resize。
- `RenderVMBuilder` 使用外部 viewport，不以 `Session` 或 `Surface` 尺寸作为 UI canvas truth。
- layout measurement 纯函数输出 panel/content/overlay/toast rect。
- renderer 输出行数和每行 display width 严格等于 viewport。
- active terminal resize 使用 content rect cols/rows，且重复尺寸去重。
- copy mode resize 后旧 bound cols/window 失效并重新请求 authoritative window。
- Unicode box drawing glyph、split 连接点、右边框和宽字符内容同时保持 cell-width 安全。
- styled `RenderResult` / `Frame` / `FrameSink` contract 不丢 style。
- ANSI styled frame serializer 输出 SGR、reset、cursor metadata，plain snapshot 仍可用于测试。
- active pane border 与 inactive pane border 有不同 style token。
- top/bottom bar 背景填满整行，token 裁切后 display width 恒等。
- pane chrome 标题、状态、action 槽位带 ANSI 样式时不破坏边框。
- toast/overlay styled chrome 不改变 body layout，且窄屏退化仍宽度安全。
- pane structural command 覆盖 split、close、close and kill、focus、zoom、resize、set size、balance 和 panel presentation。
- pane split/close/resize/zoom 后 active pane、terminal binding、layout plan、content rect、terminal resize、copy rebind 和 toast feedback 保持一致。
- keyboard adapter、mouse hit region、测试入口和后续 CLI mini command adapter 调用同一 semantic command contract。
- `termx v3 smoke` 输出 styled frame，不能退化为纯文本线框。

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
- 外部 terminal emulator resize -> tui-v3 viewport -> render frame。
- 外部 terminal emulator resize -> active content rect -> core-v2 terminal resize。
- pane split/close/resize/zoom -> tui-v3 semantic command -> reducer/effect -> render frame。
- CLI mini command -> tui-v3 semantic command adapter -> reducer/effect，不绕过 TUI state 和 layout measurement。
- copy mode -> core-v2 HistoryWindow -> selection/copy。
- copy mode viewport/content cols 变化 -> authoritative HistoryWindow 重新绑定。
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
- 切片 40-44 改动：至少运行 `cd termx-tui-v3 && go test ./... -count=1`；涉及 CLI 装配、protocol adapter 或默认入口时同步运行 `cd termx-cli && go test ./... -count=1`。
- 切片 45 改动：至少运行 `cd termx-tui-v3 && go test ./... -count=1`、`cd termx-cli && go test ./... -count=1`，并按需运行 `make test-v2-migration`。
- 切片 47-52 改动：至少运行 `cd termx-tui-v3 && go test ./... -count=1`；涉及真实 `FrameSink`、CLI smoke 或默认入口时同步运行 `cd termx-cli && go test ./... -count=1`。
- 切片 53-56 改动：至少运行 `cd termx-tui-v3 && go test ./... -count=1`；涉及 CLI mini command adapter、默认入口或 protocol 装配时同步运行 `cd termx-cli && go test ./... -count=1`。
- 切片 57 改动：必须运行 `cd termx-tui-v3 && go test ./... -count=1`、`cd termx-cli && go test ./... -count=1`、`make test-v2-migration`。
- 切片 58 改动：文档-only 至少运行 `git diff --check`；若同切片含代码，按代码范围运行相关测试。
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
- 自动执行不能因为局部测试暂时通过就跳过后续切片；当前 styled chrome renderer 阶段只有切片 58 完成且测试准入通过后才算完成。

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
- 切片 18 已完成：`termx-cli` 已建立显式 `termx v3` 实验命令组、`termx v3 daemon` core-v2 前台 daemon 入口、`termx v3 smoke` tui-v3 非交互 smoke 入口，并把 v3 smoke 纳入 `make test-v2-migration`；默认 root、`daemon`、`attach` 仍保持旧入口。
- 切片 19 已完成：`termx-cli` 已建立 v3 专用 daemon client、`termx v3 ping` probe、core-v2 socket 默认解析、连接已有 core-v2 daemon、自动启动 `termx v3 daemon`、启动失败传播和旧 daemon 自动启动隔离测试。
- 切片 20 已完成：`termx-cli` 已建立 `termx v3 new/ls/kill/rm` 本地控制命令，全部通过 core-v2 protocol service，单 session create/list/kill/remove CLI harness 覆盖真实 core-v2 daemon 路径。
- 切片 21 已完成：`termx-cli` 已建立 `termx v3 attach <id>`，交互路径使用 tui-v3 `terminalhost.Host`、protocol terminal/history adapters 和 `NewInteractiveRuntime`，非交互环境返回明确错误，CLI harness 覆盖禁止调用旧 `tuiv2` 与真实 protocol attach 装配。
- 切片 22 已完成：`termx-cli` 已建立 `termx v3 e2e-smoke` 非交互本地端到端 smoke，并纳入 `make test-v2-migration`；smoke 覆盖 core-v2 daemon、create、live attach、terminal input、resize、`history.window` 与 tui-v3 copy mode 主路径，默认入口仍未切换。
- 切片 23 已完成：`termx-cli` 已明确 v3 实验入口 socket、log、config、state 路径策略并补 tests；v3 本地 attach 不读取或创建 `termx.yaml`，配置差异已写入迁移审计。
- 切片 24 已完成：remote 暂不迁移到 `termx v3` 实验命令组，审计文档明确 legacy/fallback 隔离结论；`termx v3 remote ...` 不挂载，旧 `termx remote ...` 保留，默认切换不得把 remote 伪称为 core-v2 已兼容。
- 切片 25 已完成：`go run ./termx-cli/cmd/termx` 默认 root 已调用 tui-v3 root runner，自动连接/启动 core-v2 daemon 并进入 tui-v3 attach runtime；旧 root 仅保留为显式 `termx legacy`。
- 切片 26 已完成：`termx daemon`、`termx attach`、`termx new/ls/kill/rm` 默认入口已切到 core-v2/tui-v3；旧本地 root/daemon/attach/control 仅保留在显式 `termx legacy ...` 下，旧 remote 仍按切片 24 结论保持 legacy/fallback 隔离。
- 切片 27 已完成：CLI、core-v2、tui-v3、protocol、proto 测试均通过；`make test-v2-migration` 已纳入并通过默认入口非交互 smoke，覆盖默认 `termx daemon/new/ls/kill/rm` 和 `termx --help`。
- 切片 28 已完成：旧本地实现文件已收敛为 `legacy_*.go` 显式隔离入口，remote 仍按 `remote_*.go` legacy/fallback 隔离；`TestDefaultRuntimeSourceDoesNotImportLegacyCoreOrTUI` 和 `make test-cli-default-deps` 守卫默认源文件不得 import 旧 `termx-core`/`tuiv2`。
- 当前本轮默认入口切换目标已完成：`go run ./termx-cli/cmd/termx`、默认 `termx daemon`、`termx attach`、`termx new/ls/kill/rm` 已使用 core-v2/tui-v3；旧本地路径只能通过 `termx legacy ...` 显式调用；remote 未迁移项保持文档化 legacy/fallback 边界。
- 切片 29 已完成：`termx-tui-v3/docs/ui-interaction-spec.md` 已新增，定义 tui-v3 的产品级 UI 交互、界面结构、功能清单、页面线稿、快捷键与鼠标交互、宽窄屏退化和硬约束；该文档不写实现方案，后续 render 架构和默认界面补齐必须以它作为产品基准。
- 切片 30 已完成：`termx-tui-v3/docs/ui-interaction-spec.md` 已补充 card panel / tmux-like split line 两种 tiled pane 呈现、全局 header/footer 可隐藏、floating pane 保持带边框、右上角现代消息弹层；该切片只写产品需求，不写实现方案。
- 切片 31 已完成：`termx-tui-v3/docs/render-architecture.md` 已新增，定义 render framework 与 content renderer 的边界、数据流、层级合成、panel/overlay/floating/toast/content 分类、禁止事项和分阶段落地计划；子 Agent 审核结论为无严重问题，小修后可进入用户拍板。
- 切片 32 已完成：用户拍板结论已写入 `termx-tui-v3/docs/render-architecture.md`；render framework + content renderer 成为正式方向，最小 render framework 阶段必须处理 card/split 两种 panel、header/footer hide 和 toast 基础生命周期，Terminal Pool 与 Workbench Tree 等 framework 成型后再接入。
- 切片 33 已完成：`termx-tui-v3/render` 已建立 `RenderResult`、layer、line/cell、content renderer、shell/panel/floating/overlay/toast/header/footer VM 基础类型、width-safe helper 和 Bubble Tea contract import guard；现有裸文本 `RenderVM{Lines, Status}` 仅作为兼容输入，经 `RenderResult` 单一路径适配到 `Frame`。
- 切片 34 已完成：`state.Root` 已拥有 reducer-owned `ShellStore`，覆盖 workspace/tab/pane 最小树、active pane、card/split panel presentation、header/footer visibility、toast/message 生命周期和 Terminal Picker overlay 占位状态；`app.NewShellReducer` 已提供 semantic message 入口并组合进 live/interactive runtime，但尚未接具体产品快捷键。
- 切片 35 已完成：`RenderVMBuilder` 已把 `state.Root` 投影为 `ShellVM`、header/footer、layout/panel、content、overlay、toast 和 cursor 子 VM；旧 `RenderVM{Lines, Status}` 字段只保留为临时兼容投影，copy-history VM 已校验 terminal id、bound token 和 cols，缺 authoritative window 时只生成 pending/empty/error content，live content 只消费 `TerminalSurfaceStore`。
- 切片 36 已完成：`Renderer.RenderResult` 已走最小 render framework，产出 workbench shell、header/footer、card panel、split line、双 pane 横向/纵向分割、panel chrome、content slot、toast、Terminal Picker overlay、hit region 合成和 cursor 归属；宽字符、emoji、combining mark 和 ANSI 样式通过 width-safe helper 裁切填充，不再把 active content 裸输出为 frame。
- 切片 37 已完成：input router 已把 `Ctrl-f` 映射为 Terminal Picker intent、`Ctrl-v` 映射为 Display/Copy intent；runtime 已接入 UI input reducer，`Ctrl-f` 打开 Terminal Picker overlay/placeholder 且不发送到 terminal，`Ctrl-v` 进入 copy-history authoritative request 路径；card/split、header/footer hide、toast close/clear 已通过 semantic message 进入 runtime，不发明未拍板产品快捷键。
- 切片 38 已完成：`termx-tui-v3` detailed smoke 已覆盖 workbench shell、card panel、split line、header/footer hide、toast、Terminal Picker placeholder、copy empty、copy history、live surface panel content、emoji/CJK/ANSI 宽度安全；`termx v3 smoke` 输出多 case UI frame，`make test-v2-migration` 已通过默认 CLI 与 v2/v3 迁移回归。
- 切片 39 已完成：`termx-tui-v3/docs/render-architecture.md` 和 `termx-tui-v3/docs/ui-interaction-spec.md` 已同步最小 render framework 已落地、未落地边界和后续 Terminal Pool / Workbench Tree / floating / overlay 深化切片；过时的 no-fallback 测试命名已收敛；旧 `tuiv2` 仍只读参考，默认路径未引入旧依赖。
- 当前 render framework 最小阶段目标已完成：默认入口继续使用 core-v2/tui-v3；render 主路径已走 `render framework + content renderer`；默认 TUI 和非交互 smoke 不再把裸文本 frame 当作可用界面。
- 当前新阶段目标已完成：外部 terminal emulator viewport 驱动重绘、layout measurement、active terminal content rect resize、copy mode resize rebind 和 Unicode box drawing 线框硬化均已落地到默认入口验收范围。
- 当前本轮运行缺口已收敛：默认 TUI 已按 reducer-owned viewport 绘制整屏 frame，已知 viewport 下不再按默认 80 列输出；默认 UI chrome 已改为 Unicode box drawing；外部 host resize 已进入 reducer-owned state 并触发重绘。
- 切片 40 已完成：`state.Root` 已拥有 reducer-owned 外部 viewport state；`TerminalHost` contract 已暴露初始尺寸和 resize event 流；真实 host 监听 `SIGWINCH` 并发布 resize event；fake host 可注入 resize；runtime 通过 `HostResizeMsg` 去重更新 viewport 并重绘，同时把有效 resize message 交给 reducer 链供后续切片使用。
- 切片 41 已完成：`RenderVMBuilder` 已把 reducer-owned 外部 viewport 投影为 `Layout.Viewport` truth；`render.MeasureLayout` 已作为纯函数产出 viewport、body、panel、content、overlay、toast、全局 hit region、最终 cursor 和绝对 cursor rect；renderer 已严格按 layout plan 绘制并填满已知 viewport，已知窄屏下不再扩展到默认 80 列。
- 切片 42 已完成：app 已通过 `render.MeasureLayout` 计算 active terminal content rect，并把 attach 初始尺寸、host resize、header/footer hide、panel presentation 和 split 变化转换为去重后的 `LiveResizeMsg`；terminal resize service 只收到 content rect cols/rows，CLI 真实 protocol 装配已覆盖 attach 后 content rect resize correction。
- 切片 43 已完成：copy mode latest/older 已使用 copy content rect cols/rows 请求 authoritative `HistoryWindow`；host resize、layout chrome 变化和 active copy content width 变化会失效旧 history window、清理 selection/cursor、重新 latest rebind，并在 pending 期间不从 live surface fallback；旧 cols response 会被 stale guard 拒绝。
- 切片 44 已完成：render 默认 UI chrome 已改为 Unicode box drawing；card、overlay、toast 使用 `╭╮╰╯─│`，split line 使用连接感知 `┌┐└┘─│├┤┬┴┼` primitive；split content rect 避开 divider cell，emoji/CJK/combining/ANSI 内容继续通过 cell-width helper 裁切填充；旧 `tuiv2/render` 仅只读参考了连接位合成思路，未迁入旧 runtime/model。
- 切片 45 已完成：`termx v3 e2e-smoke` 已通过 fake host 初始尺寸和 host resize event 覆盖默认 attach 装配、外部 viewport 初始尺寸、resize 重绘、content rect terminal resize、copy mode resized cols 绑定和 frame 行宽恒等；`termx-tui-v3/docs/render-architecture.md` 与 `termx-tui-v3/docs/ui-interaction-spec.md` 已同步本轮已落地内容和后续 Terminal Pool / Workbench Tree / floating 深化边界；测试准入 `cd termx-tui-v3 && go test ./... -count=1`、`cd termx-cli && go test ./... -count=1`、`make test-v2-migration` 已通过。
- 当前新阶段目标已拍板：把 v3 从“Unicode glyph 可见”推进到 `tuiv2` 截图级 styled chrome renderer。当前 v3 的 `Frame` 仍会丢弃 style，真实 `FrameSink` 仍写纯文本，因此不能把现状视作视觉完成。
- 切片 46 已完成：`workflow.md`、`termx-tui-v3/docs/render-architecture.md`、`termx-tui-v3/docs/ui-interaction-spec.md` 已把 styled chrome renderer 目标落档；明确 `tuiv2/render` 只读参考，优先实现 shell/chrome/pane/frame 的 styled visual parity，terminal-live/copy-history/Terminal Pool/Workbench Tree/floating 内容 renderer 后续分阶段深化。
- 当前补充拍板：pane split、close、resize/size change、focus、zoom 必须纳入当前阶段，并作为 command-first 的统一 semantic action contract；分屏创建、分屏关闭、按方向调整大小、固定尺寸/比例尺寸、等分/balance 不得只做成快捷键局部逻辑；快捷键、鼠标、测试入口和后续 CLI mini command 只能作为 adapter 调用该 contract。
- 当前未拍板但不阻塞编码的点：card/split 切换、header/footer hide、toast close current、toast clear all 以及 pane command 的具体产品快捷键；实现只能先提供 semantic action、reducer message、hit region、CLI mini command adapter 边界和测试入口，不得临时发明产品快捷键。
- 切片 47 已完成：`RenderResult` 可生成 plain snapshot、styled lines 和 ANSI lines；`Frame` 保留 plain/styled/ANSI lines、cursor、blink 和 metadata；真实 `FrameSink` 优先写 ANSI styled frame 并在行尾和帧尾 reset；旧 smoke 和测试仍可读取 plain `Frame.Lines`；测试准入 `cd termx-tui-v3 && go test ./... -count=1` 已通过。
- 切片 48 已完成：renderer canvas 已从 `[]string` 升级为 cell matrix compositor，内部 cell 记录 text、width、style、owner、layer、continuation 和 safe flag；写入宽字符、emoji 或覆盖 continuation cell 时会清理 wide-cell footprint；raw ANSI 输入进入 matrix 前会 strip 控制序列，最终仍通过 `Frame.ANSILines` serializer 输出 styled frame；测试准入 `cd termx-tui-v3 && go test ./... -count=1` 已通过。
- 切片 49 已完成：`Theme` 已扩展 host fg/bg、chrome fg/bg、accent、muted、success/warning/danger/info、panel border、muted border、active/inactive pane、toast/overlay 和 status token；`RenderResult` 与 `Frame` 保留 theme fallback；ANSI serializer 按 theme token 输出 24-bit SGR；harness 覆盖 semantic token 输出、自定义 palette、active/inactive token 区分和 fallback；测试准入 `cd termx-tui-v3 && go test ./... -count=1` 已通过。
- 切片 50 已完成：tiled card pane 已从 rounded chrome 切到 square Unicode pane chrome；pane chrome primitive 支持 per-pane style/owner/layer；active pane 使用 accent style，inactive pane 使用 muted style；pane 顶边包含标题、状态和 action slot；split/card 两种模式保留 content rect 与行宽恒等；smoke 已验证 active pane accent ANSI；测试准入 `cd termx-tui-v3 && go test ./... -count=1` 已通过。
- 切片 51 已完成：header/footer 已从占位文本升级为 styled top/bottom bar；top bar 输出 workspace、tab、create 和 notice token，bottom bar 输出 mode、hint 和 status token；bar cell 使用 `StyleStatus` 并输出 status background ANSI，行宽填满 viewport；header/footer hide 继续由 layout plan 回收 body；默认 smoke 已验证 styled shell bar token 和背景 ANSI；测试准入 `cd termx-tui-v3 && go test ./... -count=1` 已通过。
- 切片 52 已完成：toast 与 Terminal Picker overlay 已升级为 styled chrome，覆盖 overlay/toast 背景、severity token、close action region、hit region 优先级、ANSI 输出和宽字符安全；测试准入 `cd termx-tui-v3 && go test ./... -count=1` 已通过。
- 切片 53 已完成：`state.PaneCommand` 已成为 pane 结构操作统一 contract，覆盖 split、close、close and kill、focus、zoom、resize、set size、balance、panel presentation、target、source、confirm、ratio/cell size 和 CLI mini source；`ShellPaneCommandMsg` 已接入 app reducer，旧 split/presentation 消息也转入同一 contract；无效目标和 destructive confirm 只产生反馈 effect，不绕过 reducer；layout resize 与 copy mode rebind 已识别统一命令消息；测试准入 `cd termx-tui-v3 && go test ./... -count=1` 已通过。
- 切片 54 已完成：reducer-owned pane tree 已支持横向/纵向 split 插入、focus/activate、close pane、close and kill terminal effect 边界、zoom/unzoom/toggle zoom；close 会同步裁剪 split tree、保留 terminal binding 供 kill effect 使用、维护 active pane 与 tab invariants；pane command 成功、无效和待确认状态均有 toast 反馈；RenderVMBuilder 在 zoom 状态只投影 zoomed pane，closed pane 不再进入 render VM；测试准入 `cd termx-tui-v3 && go test ./... -count=1` 已通过。
- 切片 55 已完成：pane tree 已拥有 reducer-owned split geometry hint，支持按方向/步长 resize、按比例 set size、按固定 cell size set size 和 balance/equalize；RenderVMBuilder 会把 ratio/bias/fixed size hint 投影到 SplitVM，render layout measurement 按 hint 计算 panel/content rect；active terminal content rect resize 去重和 copy mode authoritative window rebind 已通过 `ShellPaneCommandMsg` 跟随几何变化；测试准入 `cd termx-tui-v3 && go test ./... -count=1` 已通过。
- 当前下一步是切片 56：pane command adapters，把 pane mode、resize mode、鼠标 hit region 和 CLI mini command adapter 接到同一 semantic command，继续避免临时发明未拍板产品快捷键。
- 后续如继续推进 Terminal Pool、Workbench Tree、floating 深化、remote 迁移、彻底移除 `termx-cli` module 级旧依赖或拆分 legacy binary，必须在 styled chrome 阶段完成或明确调整本文件任务队列后再开始。
