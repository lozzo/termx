# 工作流：termx TerminalView/Attachment 多 panel 连接主线

本文件是当前分支唯一有效的活动驱动文件。后续所有分析、实现、测试、提交都必须先读取本文件，并以本文件为准。

本文件只记录目标、范围、硬约束、任务队列、测试准入和提交规则。架构正文不写在本文件里，分别以 `termx-core-v2/docs/architecture.md`、`termx-tui-v3/docs/architecture.md`、`termx-tui-v3/docs/ui-interaction-spec.md`、`termx-tui-v3/docs/render-architecture.md` 和 `termx-cli/docs/v2-v3-switch-audit.md` 为准。

本文件必须保持全中文。若本文件与旧说明、聊天记录、旧代码行为或局部假设冲突，默认以本文件为准；若技术设计需要变化，必须先更新本文件或与实现同切片更新。

## 1. 当前唯一目标

在默认入口已经切到 `termx-core-v2/` 与 `termx-tui-v3/`，styled chrome、panel content、真实 PTY、workbench storage 和基础可用性切片已经完成的基础上，把“同一个 terminal 可以被同一屏幕、同一 tab 内多个不同 panel 连接”的 `TerminalView/Attachment` 语义补成当前主线，修正此前 `pane -> terminalID -> 全局 Session` 的一对一假设。

当前事实：

- `go run ./termx-cli/cmd/termx`、默认 `termx daemon`、`termx attach`、`termx new/ls/kill/rm` 已使用 `termx-core-v2` 与 `termx-tui-v3`。
- `termx-tui-v3` 已有自有 runtime、input、state、services、terminalhost、copy mode 和最小 render 骨架，且不依赖 Bubble Tea contract。
- `termx-tui-v3/render` 已建立 render framework / content renderer 分层，`RenderVMBuilder` 输出 `ShellVM`，`Renderer.RenderResult` 通过 workbench shell、panel、overlay 和 toast 合成 `RenderResult`；旧 `RenderVM{Lines, Status}` 兼容输入字段已删除，`Frame.Lines` 只作为 `RenderResult` 的 plain 输出适配保留。
- `termx-tui-v3/state` 已拥有 reducer-owned shell、workspace/tab/pane 最小树、panel presentation、header/footer visibility、toast/message 和 Terminal Picker overlay 状态模型。
- 外部 terminal emulator resize 已作为 runtime message 流进入 reducer-owned state；`RenderVMBuilder` 已把外部 viewport 作为 shell layout 尺寸 truth；renderer 已按已知 viewport 填满输出；active terminal resize 已从 panel content rect 计算；默认 UI chrome 已收敛到 Unicode box drawing。
- `termx-tui-v3/docs/ui-interaction-spec.md` 已明确 `terminal` 是全局运行实体、`pane` 是观察位/工作位，且一个 terminal 可以被多个 pane 复用；但当前实现仍大量依赖 `PaneState.TerminalID`、全局 `TerminalSessionStore`、全局 live stream token 和 active content rect resize，尚未形成 first-class `TerminalView/Attachment` 模型。
- `core-v2` protocol 已有 `SurfaceID`、`ViewID`、`ResizeOwnership` 和 channel 字段雏形，但当前 protocol session 主要保存 `channel -> terminalID`，没有 attachment registry、resize owner/follower 裁决、view identity 生命周期和多 view harness。
- `tui-v3` 当前 `TerminalSessionStore.InputChannels` 只是急修阶段的输入 channel 路由元数据，不是完整的 pane/floating view session truth；后续必须收敛为 reducer-owned view/attachment store。
- 当前 `Frame` 由 `RenderResult` 适配生成 plain、styled 和 ANSI 三种输出视图；真实 `FrameSink` 优先写 ANSI styled frame，plain `Frame.Lines` 只用于测试快照、smoke 输出和宽度断言。
- 当前默认 UI 只达到基础 styled chrome renderer 和产品壳可操作基线，尚未达到用户截图要求的 `tuiv2` 视觉等级；后续不能把“有 Unicode 线框、有颜色、有交互”误判为视觉对齐完成。
- 切片 80-82 完成后的真实视觉复核未通过：用户明确指出当前 TUI 样子仍与目标截图不一致，因此切片 83 只能作为复核失败和返工拆分归档，不能作为视觉完成验收。
- 当前主线不是回到旧 `tuiv2` 原地修补，也不是直接复制 `tuiv2/render`；`tuiv2` 只能作为只读视觉目标和经验参考，新实现必须服从 `termx-tui-v3` 的 render framework + content renderer 架构。

完成定义：

- `termx-tui-v3` render 主路径使用 `render framework + content renderer` 作为正式结构。
- `RenderResult` 是唯一主输出，必须保留 styled cell / styled line / metadata；纯字符串、测试快照和真实 TTY ANSI 输出都只是适配层。
- 真实 `FrameSink` 必须能写出 ANSI styled frame，不能丢失边框色、active/inactive 状态、header/footer 背景、toast/overlay 样式和必要 reset。
- 默认 TUI 进入后不再只显示裸文本 `live surface pending` 或 `live: termx-main`，而是显示 workbench shell、header/footer、panel chrome 和 panel content。
- 默认 workbench chrome 必须达到 `tuiv2` 截图级别：整屏布局、稳定 top/bottom bar、pane 细线边框、active pane accent、inactive pane muted、pane 标题/状态/action 槽位、内容区明确裁切。
- pane/floating 不再以裸 `TerminalID` 作为唯一连接身份；最终必须通过 `TerminalView/Attachment` 绑定 terminal、protocol channel、view id、surface id、resize role 和 view-local request/error/desired size。
- 同一个 terminal 可以同时被多个 tiled pane、floating pane 或后续 tab/workspace view 连接；terminal process、lifecycle、input sink 和 authoritative history truth 仍只有一份。
- close pane / detach pane 只移除当前工作位和 view binding，不 kill terminal；kill terminal 是 destructive terminal lifecycle 操作，会影响所有绑定该 terminal 的 view。
- resize 必须有 ownership 语义：同一 terminal 同时只能有一个有效 resize owner 修改 PTY size，follower/observer view 不得因为自己的 content rect 变化覆盖 terminal process size；focus 或显式操作如需转移 owner，必须走协议和 reducer/effect/message 路径。
- copy/history 仍按 terminal authoritative `HistoryWindow` 工作，但交互态、content cols、pending request 和 rebind 必须绑定到发起 copy 的 pane/view，不能被同 terminal 的其它 view 覆盖。
- 视觉完成不能只看 smoke 文本是否有线框；必须以真实 TTY ANSI frame、截图/录制、固定 viewport smoke snapshot 和人工对照 `tuiv2` 目标风格共同验收。
- 当前切片 75-78 只完成产品壳、基础 styled renderer、terminal/copy 前推和 render 输入清理，不等于上述视觉完成定义已经满足。
- 本轮 UI 产品壳完成后，除 terminal-live/copy-history 的深层内容渲染仍可继续深化外，header/footer、pane、floating、Terminal Pool、Workbench Tree、Prompt/Help、Tab/Workspace、toast、overlay、快捷键和鼠标入口必须形成可基本操作的产品闭环。
- 如果 UI 产品壳完成后仍有余力，优先把 terminal live 的连接展示和输入交互继续前推，但不得以牺牲 UI 产品壳完备验收为代价。
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
- 默认视觉目标以 `tuiv2` 实际样式为准：shell header/footer 是单行 tab/status bar，不绘制整屏外框；pane、floating、overlay、toast 等对象 chrome 才使用边框，并通过 style 区分 active/inactive。后续若 `tuiv2` 对某类 overlay 使用圆角或特定卡片样式，应迁移为 v3 render primitive，而不是继续按旧 Unicode 线稿硬化。
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
- `Ctrl-p` pane mode、`Ctrl-r` resize mode、`Ctrl-g` global mode 是当前第一版结构操作入口；card/split 切换、header/footer hide、toast close current、toast clear all 已通过 mode 快捷键进入 semantic action，不得再绕过 reducer 临时实现第二套逻辑。
- 鼠标和 hit region 必须复用同一 semantic action；render 可以只产出稳定 action token，但 app/input 必须把真实鼠标坐标派发到最新 hit region，UI chrome 区域不得漏发到 terminal。
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

### 4.10 SK 小阶段与反补丁式实现约束

- 后续 `/goal` 自动推进时，任何较大目标必须先拆成可独立验证的小阶段；每个小阶段完成后必须单独提交一个 `SK:` 前缀的中文提交，作为可回退、可审计的中间过程。
- `SK:` 提交必须对应一个真实小阶段：有清晰范围、状态更新、必要 harness 和准入命令结果；不得把多个小阶段堆成一个大提交，也不得把半成品标成 `SK:`。
- 如果一个小阶段做到一半发现边界不对，必须先收敛为架构/设计修正阶段，更新 `workflow.md` 和对应 architecture 文档，再继续实现；不得为了赶进度绕过现有架构。
- 禁止补丁式代码：不得新增临时旁路、重复 truth、第二套状态、字符串 action 分叉、renderer 读 service/runtime、service 直改 reducer-owned state、fake-only 成功路径、仅为当前 case 服务的硬编码 adapter，或长期保留新旧双路径。
- 如果新增功能要求改变现有架构，默认选择重构或重写对应边界，把 domain model、protocol contract、projector、renderer、state/effect/service 边界做干净，再接真实路径和测试。
- 允许大改，但必须小步提交：可以在一个 `/goal` 中连续完成多个 `SK:` 小阶段；每个阶段都必须保持仓库可编译、测试准入清楚、行为边界可解释。
- 旧实现只能只读参考。迁移旧经验时必须迁入 v2/v3 的新边界和命名，不能复制旧 runtime/model 大 bag，也不能为了兼容旧内部实现保留桥接层，除非本文件先明确批准。

### 4.11 TerminalView / Attachment 语义约束

- `Terminal` 是 core-v2 管理的全局运行实体，拥有 process、PTY size、terminal lifecycle、live surface truth 和 authoritative logical-line history truth。
- `TerminalView` 或 `Attachment` 是某个 TUI panel/floating/tab 对 terminal 的连接视图，拥有 pane/floating identity、protocol channel、view id、surface id、resize role、desired content size、request seq、error state 和 event stream subscription。
- pane/floating/workbench 结构状态不得继续把裸 `TerminalID` 当作完整连接 truth；可以保留 `TerminalID` 作为 terminal identity，但必须通过 view binding 表达具体连接。
- 同一 terminal 可以同时绑定多个 view；多个 view 共享 terminal process、input sink、history truth 和 terminal lifecycle，不共享 view-local focus、copy mode、content rect、resize request seq 或 UI error state。
- terminal input 必须路由到当前 active view 的 attachment channel；不得 fallback 到全局 latest session terminal。
- terminal mouse passthrough 必须按当前命中 content 所属 view 的 live modes 判断；UI chrome、overlay、toast、footer/header hit region 继续优先，不得漏发到底层 terminal。
- resize 必须按 attachment role 处理：owner view 可以发起 PTY resize，follower/observer view 只能显示当前 terminal projection 或请求显式 ownership transfer；不得让 inactive 或非 owner view 的 content rect 变化覆盖 terminal process size。
- attach/reconnect/duplicate view 必须创建或复用 view binding；close/detach pane 只移除 view binding，不 kill terminal；kill terminal 必须广播并让所有绑定 view 显示 exited/removed/error。
- copy mode 绑定 active view 的 pane/floating 与 terminal id、content cols、view rows 和 request id；history window 仍只按 terminal truth 返回，不因多个 view 存在而复制 history truth。
- live surface cache 可以按 terminal id 保存 authoritative latest projection，但 resize boundary、desired size、stream token、input channel、view error 和 stale guard 必须按 view/attachment 或明确的 terminal generation 建模，不能依赖单全局 `TerminalSessionStore`。
- workbench storage schema 必须表达 pane/floating 的 view binding；schema 变化必须版本化并有 decode/encode harness，不得悄悄复用旧 v1 字段造成语义混淆。
- core-v2 daemon 仍不理解 workspace/tab/pane truth；它只管理 terminal pool、attachment/channel registry、resize ownership、event stream 和 opaque storage。

## 5. 任务队列

状态只能使用：`待开始`、`进行中`、`完成`、`阻塞`。同一时间只能有一个切片处于 `进行中`。

自动执行必须按表格顺序处理最早未完成切片：

- 如果最早未完成切片是 `阻塞`，必须停止并向用户说明阻塞，不得跳到后续 `待开始` 切片。
- 如果最早未完成切片是 `进行中`，继续该切片。
- 如果最早未完成切片是 `待开始`，先把它改为 `进行中` 并提交，或与本切片首个实现提交同切片提交，然后只执行该切片。

| 切片 | 状态 | 范围 | 完成标准 |
| --- | --- | --- | --- |
| 历史已完成：0-212F | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`termx-cli/`、`internal/protocol/`、`termx-proto/`、`termx-vterm/`、`termx-shared/`、`scripts/`、`Makefile`、`go.work`、相关文档 | 默认入口已切到 core-v2/tui-v3；logical-line history、HistoryWindow、protocol、runtime、styled render framework、UI 产品壳、真实 PTY/live/copy、tmux harness、storage sync、Terminal Picker/Create Terminal、panel content 深化与 TerminalView/Attachment 设计到 212F 的已完成细节不再在本队列展开，必要时从 git 历史与对应架构/产品文档追溯 |
| 213. SK view-aware resize 与 ownership transfer | 完成 | `workflow.md`、`termx-tui-v3/app/`、`termx-tui-v3/state/`、`termx-tui-v3/render/` | resize owner truth 已收口到 `TerminalViewStore` view binding；owner lookup/transfer、view-local desired size、request seq 与 stale guard 均按 view 记录；follower/observer 不覆盖共享 PTY size；pane chrome 展示 owner/follower 并提供 owner transfer action；准入已通过 |
| 213A-213F. SK terminal header 与 empty pane 收口 | 完成 | `workflow.md`、`termx-tui-v3/state/`、`termx-tui-v3/input/`、`termx-tui-v3/app/`、`termx-tui-v3/render/` | pane/floating terminal header 已按 tuiv2 风格使用结构化 `TerminalChromeVM`；unconnected pane chrome 与 empty state 已对齐；empty pane 不显示输入光标，CTA 居中并支持鼠标命中、上下键选择和 Enter 执行；global quit/pane detach 反馈已接入；准入已通过 |
| 214. SK copy/history view binding 收口 | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/app/`、`termx-tui-v3/render/` | copy mode 已绑定 active pane/view、terminal id、content cols、view rows、request id 和 bound token；同 terminal 不同 view 的 copy cols/rebind 不互相覆盖；history truth 仍来自 core-v2 terminal authoritative `HistoryWindow`；pending/older/stale guard 保持 no live fallback；准入已通过 |
| 215. SK 快捷键可见入口与核查基线 | 完成 | `termx-tui-v3/render/`、`termx-tui-v3/app/`、`termx-tui-v3/input/`、`termx-tui-v3/docs/`、`workflow.md` | 已建立 `termx-tui-v3/docs/keybindings.md` 核查基线；已按 tuiv2 迁移基准补齐当前已有真实语义的 pane、resize、global、floating、tab、workspace footer/help 可见声明、快捷键入口和 reducer harness；未实现项已拆入后续 SK 小阶段，禁止继续用临时按键 handler 补洞 |
| 215A. SK copy/display 快捷键语义核验 | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/docs/`、`workflow.md` | 已核验 display/copy 的 `Home/End`、`g/G`、`u/d` 均走 authoritative `HistoryWindow` 上的 copy reducer；`Enter` 复制 selection 后退出 copy mode；补充 integration harness；`p/P PASTE`、`H HISTORY` 保持后置，等待 history MVP 后再恢复；准入已通过 |
| 215B. SK view-local terminal layout state | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/app/`、`termx-tui-v3/input/`、`termx-tui-v3/render/`、`termx-tui-v3/docs/` | 已建立 `TerminalViewBinding.Layout` 作为 view-local terminal layout domain，用于 size lock、content pan、content align、center/reset 和 resize mode layout toggle；`s LOCK`、`Space LAYOUT`、`Shift+WASD/Shift+Arrow PAN`、`0/$/^/B ALIGN`、`m/|/_ CENTER`、`r RESET` 已走统一 semantic command、render projector 和 harness；状态不写入全局 terminal truth；准入已通过 |
| 215C. SK overlay keyboard command router | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/input/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`termx-tui-v3/docs/` | 已收敛 Terminal Picker、Terminal Pool、Workbench Tree 的键盘动作路由：picker `Tab SPLIT`、`Ctrl-E EDIT`、`Ctrl-K KILL`、`Ctrl-X DELETE`；pool `Ctrl-T TAB`、`Ctrl-O FLOAT`、`Ctrl-E EDIT`、`Ctrl-K KILL`、`Ctrl-X DELETE`；workbench tree `Ctrl-N/R/X/D/Z`；均复用 overlay item selection、ActionSpec 和 reducer/effect/message；terminal inventory delete 在 tui-v3 service/reducer 尚未接线，`Ctrl-X` 保留独立 delete 语义并显示 unsupported toast；准入已通过 |
| 215C1. SK terminal inventory delete 接线 | 完成 | `termx-tui-v3/services/`、`termx-tui-v3/app/`、`termx-tui-v3/state/`、`termx-tui-v3/docs/`、`workflow.md` | 已接入 tui-v3 terminal remove service adapter、fake service、Terminal Pool remove message/reducer、Picker/Pool `Ctrl-X DELETE` 真实 effect/result path；delete 成功后清理 TerminalView、pane/floating binding、session/live surface，并保持 terminal inventory remove 不伪装成 kill |
| 215C2. SK pane lock 快捷键对齐 | 完成 | `termx-tui-v3/input/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`termx-tui-v3/docs/` | 已对齐 tuiv2 pane mode `s LOCK`：pane footer、keyboard binding、action catalog 和 reducer path 复用 215B 的 view-local layout command；无 active terminal view 时给 reducer-owned toast，不透传 terminal |
| 215D. SK floating overview 与 summon | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/input/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`termx-tui-v3/docs/` | 已建立 floating overview overlay state、selection 和 summon domain；floating `o OVERVIEW`、`1-9 SUMMON` 以及 overview `Up/Down`、`Enter OPEN`、`1-9 SUMMON`、`Esc BACK` 已走 reducer-owned state、ActionSpec、input/app/render harness；overview 不再是 placeholder |
| 215D1. SK floating group commands | 待开始 | `termx-tui-v3/state/`、`termx-tui-v3/input/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`termx-tui-v3/docs/` | 补齐 floating group 操作，覆盖 floating `v ALL`、`= FIT`、`s AUTO-FIT` 与 overview `s SHOW ALL`、`c COLLAPSE ALL`、`x CLOSE`；collapse、fit、auto-fit 必须进入 reducer-owned floating state，不得只作为 render 标志 |
| 215E. SK clipboard paste 与 history overlay | 阻塞 | `termx-tui-v3/state/`、`termx-tui-v3/input/`、`termx-tui-v3/app/`、`termx-tui-v3/services/`、`termx-tui-v3/render/`、`termx-tui-v3/docs/` | 暂不执行：clipboard paste/history overlay 依赖历史记录技术达到可验收 MVP；当前 history 仍只是非常早期的演示能力，不作为本轮连续自动推进目标。重新启用前必须先新增 history MVP 切片，明确 authoritative history contract、storage、pagination、copy binding 和真实 harness |
| 215F. SK shortcut integration and tmux harness | 待开始 | `termx-cli/`、`termx-tui-v3/`、`termx-core-v2/`、`internal/protocol/`、`Makefile` 按需 | 建立非 history 快捷键真实/黑盒证据：同一 terminal 连接到同 tab 多 pane 或 floating，pane lock、terminal remove、floating overview/summon/group command 均走真实 reducer/effect/message；输入只进 active view channel，owner resize 触达 PTY，follower 不覆盖 size，kill/remove terminal 更新所有 view；运行相关模块测试、tmux/e2e smoke 和 `git diff --check` |

当前下一步：执行切片 215D1 floating group commands；随后执行 215F shortcut integration/tmux harness。215E clipboard paste/history overlay 因依赖 history 技术暂时阻塞，history MVP 今天不重启、不排进当前自动执行队列。继续保持 copy/history 不从 live surface、snapshot 或 local VTerm scrollback 推断历史，也不得把 styled payload parser 扩张成补丁式完整 VT emulator。

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
- 自动执行不能因为局部测试暂时通过就跳过后续切片；当前 UI 框架完善阶段必须从切片 61 开始按顺序推进，不得跳过 header/footer、鼠标 hit region 或 active pane 反馈直接做 Terminal Pool、Workbench Tree、floating 或大内容 renderer。
- `/goal` 自动推进期间，较大目标必须拆成多个小阶段；每完成一个小阶段就运行该阶段准入并提交一个 `SK:` 前缀中文提交，再进入下一小阶段。
- 如果实现过程中发现现有架构无法干净承载目标，不得临时补丁绕过；必须暂停当前实现小阶段，先把 architecture/workflow 和边界重构小阶段做成新的 `SK:` 提交。

## 9. 提交规则

- 当前工作区未提交改动必须先整理并提交，再继续后续开发。
- 每个有效变动必须形成中文提交。
- 后续 `/goal` 自动推进的小阶段提交必须使用 `SK:` 前缀，例如 `SK: 完成 live latest-wins 合流`；文档-only、小重构和 harness 阶段也必须按同样规则形成可回退提交。
- 不允许长期堆积未提交改动。
- 不得 amend commit，除非用户明确要求。
- 不得 revert 用户或其他代理的未提交改动；若冲突，先停下说明。
- 删除旧代码必须和对应新语义或 harness 同切片提交。

## 10. 当前状态

- 当前分支主线已切换到 `termx-core-v2/` 与 `termx-tui-v3/`，默认 root、daemon、attach、new/ls/kill/rm 已走 core-v2/tui-v3；旧本地路径只允许 `termx legacy ...`，remote 仍按 legacy/fallback 隔离。
- 已完成历史能力、协议服务、真实 PTY/live surface、styled render framework、pane/floating/overlay/product shell、Terminal Pool/Picker、Workbench storage sync、tmux 黑盒 harness、panel content 深化和 TerminalView/Attachment 基线切片；详细完成记录不再保留在本文件长表中。
- 当前 TerminalView/Attachment 主线已完成到 214：view-aware resize ownership、pane/floating terminal header、unconnected/empty pane 视觉与键盘 CTA、copy/history view binding 均已收口。
- 当前快捷键迁移已完成到 215D：floating overview/summon 已复用 reducer-owned floating state；下一步按 215D1、215F 连续收敛非 history 快捷键。215E clipboard paste/history overlay 已阻塞后置，history MVP 今天不重启。
- 已知环境缺口：本机当前没有 `protoc` 与 `protoc-gen-go`；仅在需要重新生成 proto 时构成阻塞，当前文档压缩不受影响。
