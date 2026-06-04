# termx-tui-v3 Render Framework 架构

状态：已拍板
日期：2026-06-04

## 1. 文档目的

本文档定义 `termx-tui-v3` 的 render 架构方向。

本文档回答：

- 为什么 `termx-tui-v3` 需要一个内部 render framework。
- render framework 与 content renderer 的职责边界是什么。
- 产品级 UI 如何映射到 `StateRoot -> RenderVM -> RenderResult -> FrameSink`。
- panel、floating、overlay、toast、header/footer、content slot 如何组合。
- 哪些 `tuiv2` 产品行为可以保留，哪些实现结构不能迁入。
- 后续应如何分阶段落地，避免先写临时线框。

本文档不回答：

- 具体 Go 文件如何命名。
- 具体函数签名和字段名。
- canvas、cache、裁切、ANSI 输出的最终实现细节。
- 具体 PR 如何拆分。

本文档必须以 `termx-tui-v3/docs/ui-interaction-spec.md` 为产品基准。

## 2. 结论

`termx-tui-v3` 应把 render 做成内部 composition framework，而不是 terminal 专用 renderer。

该方向已经拍板，后续实现应以 `render framework + content renderer` 作为正式架构方向。

核心拆分：

- render framework：负责布局、panel、chrome、split、floating、overlay、toast、裁切、层级合成、hit region 和最终 frame。
- content renderer：负责在 framework 分配好的内容矩形里渲染具体内容。

terminal live surface 只是 content 类型之一。

未来新增非 terminal 内容时，应通过新增 content renderer 接入，而不是改造 panel、floating、overlay 或 frame sink。

## 3. 为什么需要 render framework

如果 renderer 只围绕 terminal 内容写，短期可以更快补出一个可见界面，但会产生这些问题：

- pane 边框、split、floating、overlay、toast、hit region 会和 terminal 渲染混在一起。
- Terminal Pool、Workbench Tree、Help、Prompt 等非 terminal 页面会被迫复用 terminal renderer 的假设。
- 后续新增文件查看器、任务列表、agent 状态、日志面板等非 terminal 内容时，需要反复改 renderer 主路径。
- copy mode 很容易再次退回从 live surface 或 snapshot fallback 的旧路径。

因此 render 的主抽象不应该是 terminal，而应该是：

- shell。
- layer。
- panel。
- content slot。
- overlay。
- toast。
- frame。

terminal 只是一种 content。

## 4. 总体数据流

目标数据流：

```text
StateRoot
  |
  v
RenderVMBuilder
  |
  v
ShellVM
  |
  +--> HeaderVM
  +--> FooterVM
  +--> LayoutVM
  |     |
  |     +--> PanelVM[]
  |     |     |
  |     |     +--> PanelChromeVM
  |     |     +--> ContentVM
  |     |
  |     +--> FloatingVM[]
  |
  +--> OverlayVM[]
  +--> ToastVM[]
  |
  v
Render Framework
  |
  +--> layout
  +--> panel chrome
  +--> content renderer dispatch
  +--> layer composition
  +--> hit region composition
  +--> cursor selection
  |
  v
RenderResult
  |
  v
FrameSink
```

输出侧只能有一个主结果类型：

```text
RenderResult
  Lines
  Cursor
  Blink
  HitRegions
  Metadata
```

字符串输出、测试输出、真实 TTY 输出都只是 `RenderResult` 的适配层。

不允许再次出现 `RenderFrame()` 和 `RenderFrameLines()` 两条独立主流程。

## 5. 分层职责

### 5.1 StateRoot

`StateRoot` 是 reducer-owned UI state。

它可以保存：

- workspace / tab / pane / floating pane 状态。
- active pane。
- panel mode。
- header/footer visibility。
- terminal live surface。
- authoritative history window。
- copy mode 交互态。
- overlay state。
- toast/message state。

它不负责：

- 计算最终屏幕矩形。
- 画线框。
- 合成 layer。
- 格式化最终 frame。

### 5.2 RenderVMBuilder

`RenderVMBuilder` 把 `StateRoot` 投影为 render 可消费的 VM。

它负责：

- 从产品状态选择当前 surface。
- 计算 workspace / tab / pane / floating 的 view-model。
- 给 panel 分配内容类型。
- 准备 header/footer/toast/overlay VM。
- 把业务状态转换成短 token、label、action descriptor。
- 判断 copy mode 是否绑定 authoritative history window。
- 计算 content 是否 pending、empty、error。

它不负责：

- 画字符。
- 合成 layer。
- 计算 ANSI 输出。
- 直接写 frame。
- 请求 core client。
- 修改 state。

`RenderVMBuilder` 不应退化成新的大 bag。

推荐按子域拆分：

- shell VM builder。
- body/layout VM builder。
- panel VM builder。
- content VM builder。
- overlay VM builder。
- toast VM builder。
- cursor VM builder。

如果某个 builder 的输入或输出同时覆盖 workspace、runtime、history、overlay、toast、cache 和 terminal lifecycle，应视为边界失效，必须继续拆分。

copy-history VM 的生成条件必须写实：

- `CopyModeStore` 的 terminal id 必须与 `HistoryStore` 当前 window 的 terminal id 一致。
- `CopyModeStore` 的 bound core window token 必须与 `HistoryStore` 当前 window token 一致。
- `CopyModeStore` 的 bound cols 必须与 `HistoryStore` 当前 window cols 一致。
- 任一条件不满足时，只能生成 pending、empty 或 error content VM。
- 不得从 `TerminalSurfaceStore`、snapshot、grid viewport 或 local VTerm scrollback 补齐 copy-history VM。

### 5.3 Render Framework

render framework 消费 VM，产出 `RenderResult`。

它负责：

- 屏幕矩形和安全裁切。
- header/footer 是否占用空间。
- card panel 与 split line panel 的 layout。
- floating panel 的 z-order、遮挡和裁切。
- overlay 和 toast 的层级合成。
- panel chrome。
- 全局 hit region 汇总。
- cursor 的最终归属。
- 宽窄屏退化。

它不负责：

- 判断业务动作是否可用。
- 读取 core client。
- 请求 history window。
- 从 live surface 推断 copy mode history。
- 解释 terminal 生命周期语义。
- 修改 `StateRoot`。

### 5.4 Content Renderer

content renderer 只负责在给定内容矩形内画内容。

输入是：

- content rect。
- content VM。
- theme / style token。
- content-local focus/cursor 信息。

输出是：

- content lines 或 cells。
- content-local cursor。
- content-local hit regions。
- content-local metadata。

content renderer 不知道：

- header/footer 是否存在。
- 当前 panel 是 floating 还是 tiled。
- 当前 panel 是否被 overlay 遮挡。
- 其他 panel 的位置。
- frame sink。
- core client。

## 6. Content 类型

架构上需要识别这些 content 类型，实际落地按第 18 节分阶段推进：

- `terminal-live`：实时 terminal surface。
- `copy-history`：authoritative HistoryWindow 投影。
- `empty-pane`：未连接 terminal 的 CTA。
- `exited-pane`：退出 terminal 的最后状态和 recovery CTA。
- `terminal-picker`：Terminal Picker overlay 内容。
- `terminal-pool`：terminal list/detail/preview。
- `workbench-tree`：workspace/tab/pane 树和 preview。
- `floating-overview`：floating pane overview 内容。
- `prompt`：输入表单。
- `help`：帮助内容。
- `placeholder`：尚未实现内容的明确占位。

后续可扩展：

- file viewer。
- log viewer。
- task list。
- agent status。
- metrics panel。
- remote/session inspector。

Content 类型是内部枚举和内部契约，不是第一阶段对外插件 API。

新增 content 类型不得修改 panel/floating/overlay 的基本合成规则。

## 7. Panel 模型

### 7.1 Tiled Panel

tiled panel 是 workbench 主体里的 pane。

必须支持两种视觉模式：

- card panel。
- split line。

card panel：

- 每个 panel 拥有独立完整边框。
- chrome 明确属于该 panel。
- 适合默认可读性和鼠标操作。

split line：

- 相邻 panel 共享分割线。
- 更接近 tmux。
- 提升 terminal 内容利用率。
- chrome 必须更克制。

两种模式共享同一组 panel/content 语义。

切换模式不能改变：

- pane id。
- terminal binding。
- active pane。
- copy mode 绑定。
- hit region 语义。

### 7.2 Floating Panel

floating panel 始终是独立完整边框。

它不跟随 tiled panel 的 card/split-line 模式变化。

floating panel 需要：

- rect。
- z-order。
- title。
- state token。
- action token。
- resize handle。
- drag affordance。
- content slot。

### 7.3 Panel Chrome

panel chrome 只表达 panel 局部状态。

它可以显示：

- title。
- lifecycle token。
- share count。
- owner/follower/follow action。
- copy mode token。
- pane action。

它不能显示：

- 全局 workspace 摘要。
- 全局 notice。
- Terminal Pool 页面状态。
- 长帮助文案。

## 8. Shell Chrome

shell chrome 是 workbench 外层。

它包含：

- header。
- footer。
- toast anchor。

header：

- workspace。
- tab strip。
- create tab token。
- 短 notice/error 的轻量入口。

footer：

- mode。
- mode hints。
- workspace / terminal / floating 短摘要。

header/footer 可以隐藏。

隐藏时：

- body 占用更多空间。
- 状态不能完全丢失。
- notice/error 主要进入 toast。
- mode 必须能通过短暂反馈或 Help 被识别。

## 9. Overlay

overlay 是临时层。

架构上需要覆盖这些 overlay 类型，实际落地按第 18 节分阶段推进：

- Terminal Picker。
- Workbench Tree。
- Prompt。
- Help。
- Floating Overview。

overlay 可以是：

- 透明覆盖。
- 半透明/遮罩语义覆盖。
- opaque 全屏覆盖。

opaque overlay 可以跳过 body render，但必须独立产出 cursor。

不能依赖上一帧 body cursor。

overlay 内不显示快捷键字符串。

overlay 输出：

- overlay layer。
- overlay hit regions。
- overlay cursor。
- overlay metadata。

## 10. Toast / Message

toast 是右上角现代消息系统。

toast 属于 shell 层，不属于 panel 内容。

toast 必须：

- 浮在主界面之上。
- 不永久改变 layout。
- 支持 info/success/warning/error。
- 支持 pending/progress 语义。
- 支持自动消失。
- 支持关闭当前消息或清空消息。
- 在窄屏退化为单行短提示。

toast 不替代：

- pane 内 loading。
- copy mode 状态。
- Terminal Pool 列表状态。
- Help。

## 11. Hit Region

hit region 是 render framework 的一等输出。

来源：

- shell chrome。
- panel chrome。
- content renderer。
- floating panel。
- overlay。
- toast。

规则：

- region 必须绑定稳定语义，不绑定临时文案。
- UI chrome region 优先于 terminal mouse forwarding。
- overlay region 优先于 body region。
- toast region 优先于被遮挡的 body region。
- floating panel region 优先于 tiled panel region。
- region 必须经过 layer composition 裁切。

content renderer 可以产出局部 region，但 framework 负责把它们转换到全局坐标。

## 12. Cursor

cursor 的最终归属由 render framework 决定。

优先级：

1. active opaque overlay cursor。
2. active prompt/input cursor。
3. active content cursor。
4. hidden cursor。

body 被 opaque overlay 跳过时，overlay 必须自己提供 cursor。

copy mode cursor 不等同于 terminal live cursor。

terminal live cursor 只来自 live surface content。

copy mode cursor 只来自 authoritative history content。

## 13. Layer 合成

推荐概念层级：

```text
base background
tiled panels
floating panels
shell chrome
overlay
toast
cursor metadata
```

合成规则：

- 后层覆盖前层。
- opaque layer 可以阻止下层 render。
- 半透明语义可以通过 style 表达，但最终仍输出确定字符/cell。
- 每一层必须能裁切到 viewport。
- 每一层产出的 hit region 必须随层级一起裁切和覆盖。

## 14. 宽度安全与字符单元

render framework 必须以 terminal cell width 为布局单位。

所有会影响边框、split line、toast、overlay、header/footer、panel chrome 和 content rect 的计算，都不得使用 byte length 或 rune count。

必须使用 ANSI-aware / grapheme-aware 的宽度、裁切、填充和对齐 helper。

至少要覆盖：

- CJK 宽字符。
- emoji。
- variation selector emoji。
- zero-width combining mark。
- ANSI SGR 样式序列。
- 控制序列或 erase 序列不会污染布局宽度。

框架内部推荐使用 cell / segment primitive 表达内容：

- `Text`：原始显示文本或 ANSI 片段。
- `Width`：该片段占用的 terminal cell 数。
- `Style`：可选样式 token。
- `Safe`：是否可安全参与线性拼接、裁切和 diff。

旧 `tuiv2` 中有两类经验可以迁入新边界：

- `x/ansi.StringWidth` / `x/ansi.Truncate` / `lipgloss.Width` 一类 ANSI-aware helper，用于普通 UI 文本宽度、裁切和补齐。
- presented row / width safety 一类 cell-level 思路，用于处理 wide cell、emoji variation selector、host width ambiguous cluster、erase 和 reanchor。

迁入时不能复制旧 `tuiv2` 的 runtime/model 结构，但必须保留“最终每一行显示宽度等于目标 viewport 宽度”这个 contract。

禁止：

- 用 `len(string)` 决定可见宽度。
- 用 `len([]rune(string))` 决定可见宽度。
- 先按 rune 裁切再补边框。
- 允许 content renderer 输出超过 content rect 的可见宽度并破坏右边框。
- 允许宽字符覆盖 split line、toast 边框或 panel 边框。

第一阶段 harness 必须加入：

- panel 标题包含 emoji 时边框宽度稳定。
- content 包含 CJK / emoji / combining mark 时右边框不漂移。
- toast 文本包含 emoji 时 toast 宽度稳定。
- split line 两侧 content 包含宽字符时分割线不被覆盖。
- ANSI styled text 裁切后显示宽度等于目标宽度，且不留下未闭合样式。

## 15. 与 core-v2 的边界

render 不直接访问 core-v2。

render 只消费 state 中已经接纳的数据。

terminal live content：

- 消费 `TerminalSurfaceStore`。
- 只用于实时显示。

copy history content：

- 消费 `HistoryStore + CopyModeStore`。
- 只使用 core-v2 authoritative `HistoryWindow`。

禁止：

- copy mode 从 live surface fallback。
- copy mode 从 snapshot/grid viewport fallback。
- render 请求 `history.window`。
- render 触发 terminal input/resize。

## 16. 与 tuiv2 的关系

可以保留的产品行为：

- top bar / bottom bar 信息分区。
- pane chrome 稳定槽位。
- empty pane CTA。
- exited pane recovery。
- Terminal Picker。
- Terminal Pool。
- Workbench Tree。
- floating pane 带边框、z-order、drag/resize。
- hit region 驱动鼠标语义。
- UI chrome 优先于 terminal mouse forwarding。

不能迁入的实现结构：

- `VisibleRenderState` 大 bag。
- `Workbench` / `Runtime` 旧状态模型。
- render 层回看业务状态。
- render cache key 直接平铺业务状态。
- pane render entry 参数爆炸。
- snapshot/grid/scrollback copy mode fallback。
- Bubble Tea `Program`。
- Bubble Tea `standardRenderer`。
- Bubble Tea `tea.Model` / `tea.Msg` / `tea.Cmd`。
- Bubble Tea `tea.KeyMsg` / `tea.MouseMsg`。
- `bubbles` 或任何依赖 Bubble Tea contract 的 UI 组件。
- `RenderFrame` / `RenderFrameLines` 双主路径。

v2 的经验应该转化为边界和 harness，而不是复制旧结构。

## 17. Cache 与性能

Phase 1 / 最小 framework 阶段不以复杂 cache 为目标。

先保证：

- 单一路径输出。
- 正确裁切。
- 正确层级。
- 正确 hit region。
- 正确 cursor。

后续再引入 cache。

cache 原则：

- cache key 只基于 VM。
- cache 不读取业务 state。
- body cache 不理解 overlay 业务。
- status/header cache 不理解 modal 业务。
- content cache 只归 content renderer 所有。
- overlap / non-overlap 等几何信息应由 VM 或 layout 层显式表达，不靠 renderer 临时猜。

## 18. 分阶段计划

### 18.1 Phase 0：文档和防回归基线

目标：

- 本文档定稿。
- UI 交互规格和 render 架构一致。
- 明确不再用裸文本 frame 作为默认界面完成标准。

建议 harness：

- `RenderResult` 单一路径。
- frame lines 与 string adapter 一致。
- copy mode 缺 authoritative history 不 fallback。
- pending 状态显示在所属 panel 内。
- card panel 和 split line 都能产出稳定 content rect。
- header/footer hide 不导致 workspace、tab、mode、notice/error 完全不可达。
- hit region 层级优先级覆盖 shell chrome、overlay、toast、floating 和 terminal mouse forwarding。
- opaque overlay 自己产出 cursor，不复用 body cursor。
- toast 不改变 body layout。
- CJK、emoji、combining mark 和 ANSI styled text 不破坏 panel 边框、split line、toast 边框或 row width。

### 18.2 Phase 1：最小 framework primitives

目标：

- 定义 rect / layer / panel / content / hit region / render result 概念。
- 定义 width-safe cell / line / segment primitive 和宽度 helper。
- 建立 workbench shell。
- 支持 card panel 与 split line 两种 tiled panel 呈现，不得把 split line 延后到后续阶段才处理。
- 支持最小多 pane layout，至少覆盖双 pane 横向和纵向分割，用于验证 split line 的真实合成。
- 支持 active pane、panel chrome、content rect 和 hit region 在两种 panel 模式下语义一致。
- 支持 header/footer 显示与隐藏，隐藏时 body 必须回收空间，workspace、tab、mode、notice/error 不能彻底不可达。
- 支持 toast 的真实渲染和基础生命周期，包括 severity、pending/progress、auto dismiss、close current、clear all 和窄屏退化。
- 支持 terminal-live、copy-history pending/empty、Terminal Picker placeholder 的 content/overlay 接入路径。
- 所有 chrome、panel、toast、overlay 和 content slot 都必须通过 width-safe helper 裁切、填充和对齐。

非目标：

- floating。
- Terminal Pool 完整页面。
- Workbench Tree 完整页面。
- cache。

### 18.3 Phase 2：tiled panel layout refinement

目标：

- 复杂多 pane layout。
- card panel。
- split line。
- active pane。
- panel content rect 分配。
- panel resize / split geometry 的边界测试。
- card/split 模式切换的状态保持测试。

### 18.4 Phase 3：content renderer 完整分流

目标：

- terminal-live content。
- empty-pane content。
- exited-pane content。
- copy-history content。
- terminal-picker content。

约束：

- copy-history 只消费 authoritative HistoryWindow。
- terminal-live 不参与 history truth。

### 18.5 Phase 4：floating / overlay

目标：

- floating panel z-order。
- floating 裁切和遮挡。
- overlay 合成。
- opaque overlay fast path。
- cursor 归属。

### 18.6 Phase 5：Terminal Pool / Workbench Tree / Prompt / Help

目标：

- Terminal Pool page content。
- Workbench Tree overlay。
- Prompt overlay。
- Help overlay。
- overlay hit region。

### 18.7 Phase 6：性能和增量渲染

目标：

- content-level cache。
- layer-level dirty region。
- floating overlap 优化。
- large terminal output 性能验证。

## 19. 最小 render framework 阶段完成标准

最小 render framework 阶段完成后，render 主路径至少必须支撑：

- 进入 TUI 后有 workbench shell。
- 有 header/footer，且能真正隐藏并回收 body 空间。
- card panel 与 split line 两种 tiled panel 呈现都可用。
- 两种 panel 模式下 active pane、panel chrome、content rect 和 hit region 语义一致。
- live surface pending 在 panel 内显示。
- terminal live 内容在 panel content rect 内显示。
- Terminal Picker 状态激活时有 overlay 或明确占位渲染路径。
- Display / Copy 状态激活时进入 copy-history content 路径。
- copy mode 缺 authoritative history 时显示 panel 内 pending/empty。
- toast 支持 severity、pending/progress、auto dismiss、close current、clear all 和窄屏退化。
- CJK、emoji、combining mark 和 ANSI styled text 在标题、content、toast 中不会破坏边框、split line 或 row width。
- `RenderResult` 是唯一主输出。
- renderer 不读取 core client。
- renderer 不修改 state。

## 20. 不做什么

Phase 1 / 最小 framework 阶段不做：

- 通用 widget 系统。
- 插件 UI 框架。
- 复杂主题系统。
- 高级动画。
- 对外 API 稳定承诺。
- 复刻完整 tuiv2 renderer。
- Terminal Pool 完整页面。
- Workbench Tree 完整页面。
- 完整复刻 `tuiv2` 的 cursor writer 和增量 diff pipeline。

render framework 是 `termx-tui-v3` 内部架构，不是独立产品。

## 21. 已拍板决策

以下结论已经拍板。后续实现不得重新打开这些问题，除非先修改本文档：

- `render framework + content renderer` 是正式方向。
- 最小 render framework 阶段不能只做 card panel；card panel 与 split line 都必须处理。
- header/footer hide 不能只保留 VM 字段和测试；最小 render framework 阶段必须处理实际隐藏、空间回收和状态可达性。
- toast 不能只做静态渲染；最小 render framework 阶段必须处理基础生命周期和自动消失。
- Terminal Pool 与 Workbench Tree 在 framework 成型后再接入，不作为最小 render framework 阶段的阻塞项。

## 22. 当前实现落地记录

状态：最小 render framework 阶段已落地。

已落地：

- `StateRoot -> RenderVMBuilder -> ShellVM -> Renderer.RenderResult -> FrameSink` 主路径已建立。
- `RenderResult` 是 renderer 主输出；`Frame` 只作为 `FrameSink`、测试和 CLI smoke 的线性输出适配。
- `RenderVMBuilder` 已输出 header/footer、layout/panel、content、overlay、toast 和 cursor 子 VM。
- `state.Root` 已拥有 reducer-owned shell、workspace/tab/pane 最小树、panel presentation、header/footer visibility、toast/message 和 Terminal Picker overlay 状态。
- tiled panel 已支持 card panel 与 split line 两种呈现。
- split line 已覆盖最小双 pane 横向和纵向分割。
- header/footer hide 已真实影响 body layout，隐藏后 panel body 回收空间。
- toast 已支持 severity、pending、auto dismiss、close current、clear all；renderer 中 toast 不改变 body layout。
- Terminal Picker 已有 overlay/placeholder 渲染路径。
- `Ctrl-f` 已接入 Terminal Picker intent。
- `Ctrl-v` 已接入 Display / Copy intent，并进入 authoritative history request 路径。
- copy-history content 只在 terminal id、bound token、cols 与 authoritative `HistoryStore` 一致时渲染历史内容。
- copy mode 缺 authoritative window 或绑定不一致时显示 panel 内 pending、empty 或 error，不从 live surface fallback。
- 宽度相关渲染通过 `DisplayWidth`、`FitText`、`SliceCells` 等 helper 处理 emoji、CJK、combining mark 和 ANSI styled text。
- `termx v3 smoke` 已输出多 case UI frame，覆盖 workbench shell、card/split、header/footer hide、toast、Terminal Picker、copy empty、copy history 和 live surface content。
- `make test-v2-migration` 已纳入并通过当前最小 render framework 回归。

当前仍是最小实现，不应误读为完整产品态：

- split layout 只覆盖最小双 pane 横向/纵向，不覆盖复杂多层 split、resize handle 或动态 pane resize。
- content renderer 目前是最小分流，terminal-live、copy-history、empty/exited/placeholder 已有基础路径，但 Terminal Pool、Workbench Tree、Prompt、Help 的完整内容未落地。
- floating panel 仍未落地，当前只有架构类型和后续边界。
- overlay 只落地 Terminal Picker placeholder 与基础 opaque cursor 归属，未完成 Workbench Tree、Prompt、Help、Floating Overview。
- toast 具备基础生命周期和渲染，不代表最终视觉 polish、动画或完整消息队列策略。
- hit region 已有内容、overlay、toast 合成基础，不代表完整鼠标交互产品语义。
- `RenderVM{Lines, Status}` 字段仍保留为兼容投影，后续可以在默认 runtime 和测试全部迁到 `ShellVM/RenderResult` 后继续删除或重命名。

## 23. 后续深化切片建议

后续任务必须继续遵守本文档，不得回退到裸文本 frame 或 terminal-only renderer。

建议后续切片：

- tiled layout refinement：支持多层 split、pane resize、复杂 content rect 分配、active pane hit region 和 card/split 模式切换保持。
- content renderer 分流：把 terminal-live、copy-history、empty/exited、terminal-picker、help、prompt 独立成更小 content renderer。
- floating / overlay：实现 floating pane z-order、裁切、遮挡、置顶、drag/resize affordance，以及 Workbench Tree、Prompt、Help、Floating Overview overlay。
- Terminal Pool：实现 Terminal Pool page content、列表、搜索、detail、preview 和 attach/kill/edit action。
- Workbench Tree：实现 workspace/tab/pane/floating 结构导航 overlay。
- input / hit region：把 card/split、header/footer hide、toast close/clear、overlay close 等未拍板快捷键继续保持 semantic action，在产品拍板后再落具体快捷键。
- cleanup：删除或重命名剩余旧 `RenderVM{Lines, Status}` 兼容字段和相关测试命名，前提是 runtime、CLI smoke 和 harness 已全部迁到 `ShellVM/RenderResult` 语义。
- performance：引入 content-level cache、layer dirty region 和 large terminal output 性能验证。
