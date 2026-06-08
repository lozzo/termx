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
- styled chrome renderer 如何在不复制 `tuiv2` 旧结构的前提下，达到 `tuiv2` 截图级视觉等级。

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
  Styled cells / styled lines
  Plain text adapter
  ANSI frame adapter
  Cursor
  Blink
  HitRegions
  Metadata
```

字符串输出、测试输出、真实 TTY 输出都只是 `RenderResult` 的适配层。

styled chrome renderer 阶段开始后，`RenderResult` 不能在主路径中过早压成纯 `[]string`。真实 TTY 输出必须通过 ANSI frame adapter 保留 foreground、background、bold、reset、cursor 和必要 metadata。

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
- styled chrome cell 合成。
- ANSI frame serialization 之前的 style 保真。

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

content renderer 可以产出 styled content，但只能在 framework 分配的 content rect 内生效。content renderer 不允许直接覆盖 pane border、shell chrome、overlay 或 toast chrome。

## 5.5 Styled Chrome Renderer

styled chrome renderer 是当前新阶段正式方向。

它的目标不是“出现 Unicode 线框”，而是让默认 TUI 的 shell chrome、pane chrome、边框、状态栏、toast 和 overlay 达到 `tuiv2` 截图级视觉等级。

必须具备：

- styled cell / styled line / styled layer primitive。
- foreground、background、bold、muted、accent、semantic severity 等 style token。
- active pane accent border。
- inactive pane muted border。
- top/bottom bar 背景填满整行。
- pane title、state、action 的稳定 chrome slot。
- toast/overlay/floating 独立 chrome style。
- ANSI frame serializer。
- plain text snapshot adapter。

不能继续依赖：

- renderer 主路径只维护 `[]string` canvas。
- `Frame` 只包含纯 `[]string`。
- `FrameSink` 直接写纯文本。
- 用标题里的 `active` 字样代替真实 focus style。
- 用“Unicode glyph 已存在”代替视觉验收。

`tuiv2/render` 中可参考的经验：

- `drawStyle` / styled cell 的分层思想。
- `composedCanvas` 的 cell matrix 思想。
- pane chrome slot 的产品结构。
- active/inactive border 的视觉语义。
- host-aware theme token 和 semantic color。
- ANSI serializer 的 style diff / reset / width-safety 思路。

当前视觉方向补充：

- shell top/bottom bar 需要保持 `tuiv2` 式高信息密度和整行 styled background，不应退回孤立文本 header/footer。
- tiled pane 默认使用 `tuiv2` 式 square 细线 panel chrome：连续边框、顶边 title/state/action 槽位、active accent、inactive muted；不能使用 ASCII 线框，也不能只输出无样式 Unicode glyph。
- 单 pane 的视觉重心是 styled chrome 和 content rect 裁切，terminal 内容只是 content renderer 的输入，不能覆盖 pane border、header/footer、toast 或 overlay。
- toast/message 使用实体卡片和 severity/accent token，适合右上角短反馈；它不改变 layout，不承担页面级状态展示。
- modal/overlay 使用实体前景卡片，允许标题、搜索、selected row、detail 和 action。默认不要求背景灰度遮罩，因为 dim 背景对中文、emoji、CJK、combining mark 或 ambiguous width 文本存在丢字风险；实现应优先保证文本可见和 cell width 正确。
- 后续 Prompt、Help、Workbench Tree 和 floating 都必须沿用 render framework 的 styled layer、layout plan、hit region 和 cursor ownership，不得各自写临时 frame。

不得迁入：

- `VisibleRenderState` 大 bag。
- 旧 Workbench / Runtime 状态模型。
- 旧 render cache key。
- 旧 cursor writer / incremental diff pipeline 的结构。
- render 层回看业务状态。
- 旧 snapshot/grid/copy fallback。

本阶段优先级：

1. shell/chrome/pane/frame 达到视觉等级。
2. 保证 content rect 裁切不破坏 chrome。
3. terminal-live 和 copy-history 内容 renderer 可以先保持最小内容展示，再进入后续深化。

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
- 当前实现使用共享外框加内部共享 divider，而不是只画内部竖线或横线；外框和 divider 必须通过 box connection 合成为 `┬`、`┴`、`├`、`┤`、`┼`。
- content rect 必须避开共享外框和 divider；terminal resize 与 copy-history rebind 使用 content rect 的 cols/rows，不使用 split leaf rect 的总宽高。
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

### 7.4 Pane 结构命令边界

pane split、close、focus、zoom、resize 和 size change 是 app/state 的结构命令，不是 render framework 的业务逻辑。

render framework 负责：

- 在 panel chrome、split divider、resize handle 和 content rect 周围产出稳定 hit region。
- 为 hit region 绑定稳定 action id 和目标 pane id。
- 在 layout plan 中表达 panel rect、content rect、divider rect 和 resize handle rect。
- 在 resize 或 split 预览需要时产出可见 affordance。
- 在命令执行后的新 VM 上重新绘制布局。

render framework 不负责：

- 修改 workspace/tab/pane tree。
- 判断 split 是否创建 terminal。
- kill terminal。
- 直接调整 terminal size。
- 请求或失效 copy mode history window。
- 解析 CLI mini command。

统一结构命令必须从入口 adapter 进入 app/reducer：

```text
keyboard / mouse hit region / test / CLI mini command
  |
  v
PaneStructuralCommand
  |
  v
reducer-owned workspace/tab/pane state
  |
  +--> effects: terminal resize / terminal kill / history rebind / toast
  |
  v
RenderVMBuilder -> MeasureLayout -> RenderResult
```

第一阶段必须覆盖的命令语义：

- split horizontal / split vertical。
- close pane。
- close pane and kill terminal。
- focus / activate pane。
- zoom / unzoom pane。
- resize by direction and delta。
- set size by ratio or fixed cell size。
- balance / equalize split group。
- switch card panel / split line presentation。

这些命令必须能被快捷键、鼠标 hit region、测试入口和后续 CLI mini command 复用。CLI mini command 只能作为 adapter，不得绕过 reducer、layout measurement、terminal resize、copy history rebind 或 toast feedback。

结构命令执行后必须重新测量 layout。若 active terminal content rect 改变，app 必须通过 effect 去重发送 terminal resize。若 active copy pane content width 改变，必须 invalid/rebind authoritative `HistoryWindow`，不得沿用旧 cols 或从 live surface fallback。

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
- 自动消失必须由真实 runtime timer 派发普通 tick message 进入 reducer，不得只停留在测试 harness 手动 tick。
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

### 18.7 Phase 6：styled chrome renderer

目标：

- `RenderResult` 保留 styled cell / styled line / metadata。
- `Frame` 和 `FrameSink` 支持 ANSI styled frame。
- canvas 升级为 cell matrix 或等价 compositor。
- theme token 覆盖 host-aware palette、accent、semantic severity、active/inactive pane、chrome bg/fg。
- tiled pane 默认达到 `tuiv2` 截图级 styled chrome：square Unicode 细线边框、active accent、inactive muted、pane top chrome slot。
- header/footer 达到 styled top/bottom bar 级别。
- toast 和 overlay 使用 styled chrome。
- smoke 和 harness 检查 ANSI SGR、active/inactive 差异和行宽恒等。

当前状态：

- 上述目标中的 renderer 结构、styled `RenderResult`、ANSI `FrameSink`、cell matrix、theme token、基础 pane/header/footer/toast/overlay 路径已经作为工程基线落地。
- 根据 `termx-tui-v3/docs/visual-alignment-audit.md` 与 `termx-tui-v3/docs/default-tui-visual-review.md`，当前默认 TUI 仍未达到用户截图要求的完整 `tuiv2` 视觉等级；后续必须继续按视觉返工切片推进。
- 切片 80 已把 shell header/footer 从稀疏文本条推进到分段产品栏。
- 切片 84 已把 shell header/footer 从工程 key-value 标签继续推进到高密度产品栏：top bar 使用 workspace、tab strip、`[⊕]`、active pane、`◆ owner`、terminal/floating 和 action token；bottom bar 使用 `MODE • [KEY] ACTION`、active target 和 summary。
- 切片 81 已把 pane chrome / split line 从基础线框推进到 shared chrome：card panel 与 split line 都使用 square 细线、title/state/action 槽位、active accent、inactive muted、共享外框、连接点合成和 content rect 宽度安全。
- 切片 85 已把 pane chrome 继续推进到目标截图式槽位：title、状态点、`↔0`、`◆ owner`、宽 pane full action cluster、窄分屏 compact action cluster 和同宽 hit region，顶边剩余空间继续由横线承接。
- 切片 88 已完成 shell/pane 二轮视觉重绘：theme accent 改为紫色系，status bar 改为深色背景，top bar 使用关闭 `×`、新增 `[＋]`、compact summary 和 action cluster，bottom bar 使用 `[Ctrl] · [P]` 类快捷键 taxonomy，pane top chrome 使用 `· ↔2`、`· ◆ owner`、`· 1/31` 等高密度槽位。
- 切片 82 已把 overlay/toast/floating 从基础可见 chrome 推进到实体 card：toast 具备 severity accent 竖条、title/body 合并裁切、close action 和右上角留白；overlay 与 floating 具备 title/state/action 槽位、content padding、active/focus token、ANSI reset 和宽字符安全。
- 切片 83 已复核失败：当前默认 TUI 是可运行的 styled chrome 产品壳，但不是截图级视觉完成。
- 后续不得再把无样式纯文本线框、基础 ANSI 色或 smoke 行宽恒等当作默认 UI 完成标准。
- 后续深化应继续按切片 89 的真实默认入口截图级验收推进；若仍不一致，再新增明确的视觉重绘切片，不得回退到 terminal-only renderer。

非目标：

- 复制 `tuiv2` renderer。
- 复制 cursor writer / incremental diff pipeline。
- 一次性完成 Terminal Pool、Workbench Tree、floating 全量内容。

### 18.8 Phase 7：UI framework 交互产品化

目标：

- 把 styled chrome framework 从静态视觉基线推进到可基本操作的产品壳。
- 键盘、鼠标、测试入口和后续 CLI mini command 都复用同一 semantic command 或 shell message。
- active pane、footer active target、toast feedback、content rect resize 和 copy rebind 在结构操作后同步更新。
- header/footer hide、card/split presentation、toast close/clear、overlay close、terminal mouse forwarding 边界具备端到端 harness。
- 用户能在不依赖真实 terminal 内容 renderer 完整化的情况下测试 split、close、focus、resize、zoom、card/split、header/footer hide 和 toast 操作。

必须覆盖：

- pane mode：split right/down、close、focus next/previous、zoom/unzoom、balance、card/split presentation。
- resize mode：方向 resize、balance、退出。
- global mode：header/footer hide、toast close current、toast clear all。
- 鼠标：pane content/chrome focus、pane action、resize handle 或 split divider、toast/overlay 优先级、未命中 fallback。
- 视觉反馈：active/inactive border style、pane title/state、footer active target、toast command feedback。
- layout/effect：每次结构变化都重新 measurement，active terminal content rect resize 去重，copy mode cols 变化 invalid/rebind。

非目标：

- terminal-live styled cells 完整化。
- copy-history selection/scrollbar 完整化。
- Terminal Pool、Workbench Tree、floating 完整页面。

该阶段必须排在 terminal-live content renderer 深化之前。原因是 terminal live 内容只是一种 content；如果 framework 的 hit region、active feedback、layout/effect 同步和 chrome 操作还不稳定，先接真实 terminal 内容会把边界问题和内容问题混在一起。

### 18.9 Phase 8：terminal-live content renderer

目标：

- terminal live content renderer 只消费 pane content rect 和 live surface / terminal session 投影。
- terminal 内容只能绘制在 content rect 内，不能覆盖 pane border、split line、header/footer、toast 或 overlay。
- 表达 basic style、cursor、pending、empty、exited、resize 后裁切和宽字符安全。
- 一期允许把 raw live 行中的基础 ANSI SGR 投影为 semantic style token；更完整的 styled cell stream 可以作为后续 protocol/content renderer 增强。
- 后续可继续深化 selection/search、content-local hit region、status metadata、truecolor、link、reverse/underline 和 richer terminal style。

非目标：

- 把 live surface 变成 history truth。
- 从 terminal-live content renderer 反推 copy mode history。
- 绕过 render framework 直接写 FrameSink。

### 18.10 Phase 9：性能和增量渲染

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

状态：最小 render framework、外部 viewport / resize / Unicode 线框、styled chrome renderer 工程基线、pane command 基础、UI framework 第一版产品交互、terminal-live / copy-history / empty / exited 内容 renderer 一期、Terminal Picker 真实交互深化、Terminal Pool 数据源与 Picker 服务接线一期、Terminal Pool 管理页一期、Workbench Tree overlay 一期、Floating pane 一期、Prompt / Help overlay 一期、Tab / Workspace 产品入口一期、TUI 产品壳总验收、terminal live 连接展示与交互前推、copy-history content renderer 深化和 render 兼容投影清理与性能基线已落地。当前用户确认的视觉方向已经补充落档：TUI-v3 必须延续 `tuiv2` 风格的 header/footer、square pane chrome、active accent、toast 卡片和实体 overlay，且不要求灰度遮罩背景。切片 79 已重新打开视觉差距审计与验收基线，切片 80-88 已推进多轮真实视觉对齐；不得把现有 styled chrome、二轮重绘或 smoke 证据误判为最终截图级视觉完成。

已落地：

- `StateRoot -> RenderVMBuilder -> ShellVM -> Renderer.RenderResult -> FrameSink` 主路径已建立。
- `RenderResult` 是 renderer 主输出；`Frame` 只作为 `FrameSink`、测试和 CLI smoke 的线性输出适配，并保留 plain、styled line、ANSI line、cursor 和 metadata。
- 真实 `FrameSink` 优先写 ANSI styled frame，每行使用绝对定位输出，避免满宽行依赖换行推进导致宿主自动换行破坏竖向边框。
- renderer canvas 已升级为 cell matrix / compositor，cell 记录 text、width、style、owner、layer、continuation 和 safe flag，并处理 wide-cell footprint。
- theme token 已覆盖 host fg/bg、chrome fg/bg、accent、muted、success/warning/danger/info、active/inactive pane、toast/overlay 和 status bar。
- host-aware theme 已从静态 fallback 推进到 reducer-owned capability：`StateRoot.HostTheme` 保存 OSC 10/11/4 probe 得到的 default fg/bg 与 16 色 palette，`RenderVMBuilder` 把它推导为 `RenderVM.Theme`，renderer 优先使用 VM theme 输出 TermX chrome；该路径只影响 chrome semantic token，terminal live cell 的 ANSI palette / 256 色 / truecolor 继续直通宿主。
- `RenderVMBuilder` 已输出 header/footer、layout/panel、content、overlay、toast 和 cursor 子 VM。
- `state.Root` 已拥有 reducer-owned shell、workspace/tab/pane 最小树、panel presentation、header/footer visibility、toast/message、interaction mode 和 Terminal Picker overlay 状态。
- `state.Root` 已拥有 reducer-owned 外部 viewport state；真实 `TerminalHost` 查询初始尺寸并监听宿主 resize，fake host 可 deterministic 注入 resize。
- 外部尺寸只通过 `HostResizeMsg` 进入 reducer-owned state；service、renderer、terminalhost 不直接改 state。
- `RenderVMBuilder` 已把外部 viewport 投影为 `ShellVM.Layout.Viewport` truth，不再把 session size 或 live surface size 当 UI canvas truth。
- `render.MeasureLayout` 已作为纯函数产出 viewport、body、panel、content、overlay、toast、hit region、cursor 和 cursor rect。
- renderer 已严格按 layout plan 绘制；已知 viewport 下 frame 行数等于 viewport rows，每行 display width 等于 viewport cols，不再因默认 80 列破坏宿主自动换行。
- tiled panel 已支持 card panel 与 split line 两种呈现。
- split line 已覆盖最小双 pane 横向和纵向分割。
- header/footer hide 已真实影响 body layout，隐藏后 panel body 回收空间。
- toast 已支持 severity、pending、auto dismiss、close current、clear all；新增 toast 默认带明确 TTL，真实 runtime 定期派发 toast tick 并触发重绘；renderer 中 toast 不改变 body layout。
- Terminal Picker 已有 styled overlay 渲染路径，并已接入 query/filter/selection/preview 交互。
- `Ctrl-f` 已接入 Terminal Picker intent。
- `Ctrl-v` 已接入 Display / Copy intent，并进入 authoritative history request 路径。
- active terminal resize 已由 app 通过 layout plan 计算 active pane content rect，并只把 content rect cols/rows 发给 core-v2 terminal service；attach 初始尺寸、host resize、header/footer hide、card/split 切换和 split 变化都会触发去重后的 resize。
- copy mode latest/older request 使用 copy content rect cols/rows；host resize 或 layout chrome 变化导致 content width 改变时会失效旧 `HistoryWindow`、清理 selection/cursor、重新请求 authoritative latest window。
- copy-history content 只在 terminal id、bound token、cols 与 authoritative `HistoryStore` 一致时渲染历史内容。
- copy mode 缺 authoritative window 或绑定不一致时显示 panel 内 pending、empty 或 error，不从 live surface fallback。
- copy-history content renderer 深化已支持搜索栏、query/match state、match navigation、PageDown/滚轮滚动、selection/match 颜色层级、content-local mouse selection、scrollbar/status 和 position token；所有内容仍只来自 authoritative `HistoryWindow`。
- copy mode 高度变化只更新 view rows 并夹紧 viewport；宽度变化仍 invalid/rebind authoritative `HistoryWindow`，不得显示旧 cols rows。
- terminal live attach 后会通过 terminal service 可选 `LiveSurface` 能力拉取一次 core-v2 live snapshot 初始化 `TerminalSurfaceStore`；该 snapshot 只用于实时显示，不进入 copy-history authoritative path。
- terminal live 输入只通过 `TerminalService.SendInput` effect 发送；TUI 不做本地假回显，输入显示必须来自后续 live surface 回投。
- terminal live lifecycle 已区分 attached、exited 和 error；exited 会保留最后 live 行并在 panel/footer 中显示 exited 状态，error 仍走 notice/error status。
- 宽度相关渲染通过 `DisplayWidth`、`FitText`、`SliceCells` 等 helper 处理 emoji、CJK、combining mark 和 ANSI styled text。
- 默认 UI chrome 已使用 Unicode box drawing；pane、floating、overlay、toast 默认使用直角 `┌┐└┘─│`，split line 使用连接感知 `┌┐└┘─│├┤┬┴┼`，ASCII `+ - |` 不作为默认 UI chrome。
- tiled card pane 已使用 square Unicode 细线 pane chrome 基础；active pane 使用 accent style，inactive pane 使用 muted style，pane 顶边包含 title、state 和 action slot，但仍需按视觉审计继续对齐 `tuiv2` 截图级密度、槽位和层级。
- v3 pane border 只迁入 `tuiv2/render` 的 cell 级连接位合成经验，不迁入旧 runtime/model、VisibleRenderState、cursor writer 或 snapshot/grid/copy fallback。
- header/footer 已按切片 80 从基础信息条重绘为 `tuiv2` 风格分段产品栏：status background 填满整行，workspace/tab/mode/action/active/summary 使用稳定 token，active token 使用 accent，次级 summary 使用 muted，notice/error/exited 使用 warning，行内使用 Unicode `│` 分隔；窄屏下快捷键 token 按优先级压缩，error/exited 关键状态优先保留。
- pane chrome 已按切片 95 恢复真实可用的 split-down、split-right 和 close 可见入口；每个可见 token 都有独立 hit region 并回投同一 `PaneCommandSplit` / `PaneCommandClose` contract。zoom 等尚未恢复为真实可点击入口的按钮继续隐藏，窄 pane 会退化为 close-only 或隐藏 action slot。
- shell/pane 已按切片 88 完成二轮视觉重绘：默认 palette 使用紫色 accent 和深色 chrome/status 背景；toast/floating/overlay title 使用 `·` 分层。切片 166 后按用户要求回到 `tuiv2` 实际样式方向：header/footer 是单行 tab/status bar，不绘制整屏 shell 外框，也不使用 `┌┐` / `└┘` / `├┤` / `┬┴` 作为 shell bar；footer key 显示为 `[Ctrl] • [P] pane` 这类分段状态栏语义；pane/floating/overlay 等对象 chrome 继续使用各自边框与 active/inactive 样式。
- 切片 124 已按用户反馈回收 tiled pane 顶边未设计 token；切片 125 后默认 pane chrome 只恢复真实接通的 split-down、split-right、close 可见 action，按钮文本与 hit region 来自同一 action item 列表，pane action 命中优先于 shared divider resize；仍不提前画状态 Nerd Font、`⇄2`、`◆ owner`、`1/31`、zoom 或 owner/follower token。Nerd Font 字符体系必须后续独立定义语义、字形、fallback 和命中区后再接入。
- toast 与 Terminal Picker overlay 已升级为 styled chrome，覆盖直角 border/background、toast 双侧 accent 竖线、ANSI reset 和宽字符安全；toast 本体只作为遮挡命中，不再显示 close token。
- `state.PaneCommand` 已成为 pane split、close、close and kill、focus、zoom、resize、set size、balance 和 panel presentation 的统一 semantic command contract。
- `Ctrl-p` pane mode、`Ctrl-r` resize mode、`Ctrl-g` global mode 和 `Ctrl-o` floating mode 已作为第一版键盘入口落地；`Esc` 退出 mode/overlay 且不漏发 terminal。
- pane mode、resize mode、鼠标 hit region 和 CLI mini command adapter 已接入同一 pane command contract；后续入口不得绕过 reducer 或另建局部 command path。
- pane command 后会重新测量 layout plan；active terminal content rect 变化会触发 terminal resize 去重；active copy pane content width 变化会 invalid/rebind authoritative `HistoryWindow`。
- `TerminalSessionStore` 已记录 terminal resize 目标尺寸和序号，连续 split/resize/zoom 等 pane command 下旧 resize result 不会覆盖最新 content rect。
- app/runtime 已缓存最新 render hit regions，把真实鼠标坐标派发到 pane content、pane chrome、pane action、pane 边框/分隔线 resize drag、toast 和 overlay content action；UI chrome 命中优先于 terminal forwarding。
- active pane border/title/footer/toast 会跟随键盘和鼠标 focus、split、close、resize、zoom、card/split 切换更新。
- Terminal Picker query、过滤、selected row、上下键移动、Enter attach/focus、row click attach/focus、new action feedback、preview/detail 行和 no terminal input leak 已落地。
- Terminal Pool 数据源与 Picker 服务接线一期已落地：`TerminalPoolStore` 保存 list loading/empty/error/items/stale guard，Terminal Picker 打开可触发 terminal list request，picker rows 合并当前 workspace panes 与 pool items 并去重，pool row attach、create、restart、reconnect 通过 service/effect/result message 回到 reducer。
- Terminal Pool 管理页一期已落地：独立 terminal-pool content/page 已接入，支持全局入口和 empty manager action 打开、list loading、搜索、selected row、键盘选择、Enter attach、鼠标 row/action 命中、detail、metadata、preview 摘要、Attach/Edit/Kill action、可见 action 不被裁切、service/effect/result 反馈和 no terminal input leak。
- Workbench Tree overlay 一期已落地：页面级结构导航 overlay、workspace/tab/pane/floating row、搜索、selected row、detail/preview、Open/Focus action、键盘/鼠标选择和 no terminal input leak 已接入。
- Floating pane 已落地到当前切片 96：reducer-owned floating state、z-order、active/collapsed、styled bordered chrome、keyboard create/move/resize/center/collapse/close、mouse raise/close、标题栏连续拖动移动、右下 resize handle 连续拖动 resize、content rect 裁切、viewport clamp 和 width-safe harness 已接入。切片 126 后，点击未遮挡的 tiled pane 会通过 reducer 语义清空 active floating，使 tiled pane 恢复 active chrome，floating 保持可见但以 inactive/muted chrome 渲染。
- Prompt / Help overlay 一期已落地：Prompt 作为 reducer-owned 短输入 overlay 支持 title/context/input/submit/cancel/destructive confirm 边界，Help 按 Most used、Pane、Tab、Workspace、Floating、Terminal Pool、Display/Copy 展示概念和动作；键盘、鼠标 close、overlay cursor、footer hint、宽字符安全和 no terminal input leak 已接入。
- overlay/page/copy 内容层已按切片 86 完成第一轮产品化 polish：Terminal Picker、Terminal Pool、Workbench Tree、Prompt、Help 和 copy-history 的 content renderer 统一输出 search affordance、selected row marker、detail / preview / context / input label、action row、copy match 和 scrollbar/status；这些内容仍只在 framework 分配的 content rect 内绘制。
- Tab / Workspace 产品入口一期已落地：`Ctrl-t` / `Ctrl-w` 已作为 reducer-owned interaction mode 接入，tab create/switch/rename/close、workspace create/switch/rename、Prompt rename 回投、header tab strip、footer mode hints、smoke case 和 no terminal input leak 已接入。
- TUI 产品壳总验收已落地：`TestInteractiveRuntimeTUIProductShellAcceptanceFlow` 串联 pane split/focus/resize/presentation、floating create/move/resize/close、Terminal Pool 搜索/attach、Workbench Tree open/focus、Prompt/Help、Tab/Workspace rename、toast/header/footer 操作，并统一验证 no terminal input leak、viewport 行宽恒等和 layout 操作触发 content rect resize。
- `termx v3 smoke` 已输出 12 个固定视觉 case，覆盖 workbench shell、card/split、header/footer hide、toast、Terminal Picker、Terminal Pool Page、Workbench Tree Page、Prompt/Help、copy empty、copy history、live surface content、Unicode 线框和宽字符宽度安全。
- `termx v3 smoke` 已覆盖 `pane-command-flow`，验证 pane command feedback、styled active pane ANSI、无默认 ASCII chrome 和行宽恒等。
- `termx v3 smoke` 已覆盖 `visual-audit-current` 的 `120x40` visual review baseline，验证 split line、active/inactive pane、toast、floating、header/footer、emoji/CJK 宽度安全，并明确当前仍需 screenshot polish。
- `termx v3 e2e-smoke` 已覆盖 core-v2 daemon、默认 attach 装配、fake host 初始 viewport、host resize 重绘、content rect terminal resize、copy mode authoritative history、resized copy cols、split/resize/zoom/unzoom/close pane command，以及最终 panes/active/zoom 状态。
- `make test-v2-migration` 已纳入当前默认入口、v3 smoke、e2e smoke 和默认依赖守卫回归。

当前仍是阶段性实现，不应误读为完整最终产品态：

- 当前完成的是 chrome/frame 工程基线、pane 结构命令基础、UI framework 第一版可操作闭环、三类内容 renderer 一期、Terminal Pool 管理页一期、Workbench Tree 一期、Floating pane 一期、Prompt/Help 一期、Tab/Workspace 一期和 TUI 产品壳总验收，不代表已经达到用户截图要求的最终视觉，也不代表跨 workspace/remote Terminal Pool 深化或最终 terminal/copy 内容体验完整。
- header/footer 已有切片 80 的分段产品栏；复杂 notice/error 汇总、更多 workspace/tab 管理信息和更精细的 `tuiv2` tab strip 密度仍可继续 polish。
- render 已能合成 hit region，app/runtime 已把真实鼠标坐标派发到最新 hit region；pane resize 和 floating move/resize 的连续拖拽已通过 runtime transient drag state 回投 reducer command。复杂 overlay、Prompt/Help 和 terminal mouse forwarding 的完整产品边界仍需继续深化。
- active pane 视觉反馈已有端到端验收，但最终视觉 polish、更多状态 token 和复杂多层 split 的 focus affordance 仍可继续增强。
- terminal-live content renderer 与连接展示前推已完成当前阶段：raw live 行会在 VM 层转换为 styled `Line`，基础 ANSI SGR 映射为 semantic style token，live cursor 输出为 content-local cursor，pending/empty/exited 状态在所属 pane 内表达，attach 后会初始化真实 core-v2 live rows，content 仍由 framework 裁切到 content rect；但 streaming event loop、selection、search、content-local hit region、clipped markers、truecolor/link/reverse/underline 和 rich terminal metadata 尚未完整产品化。
- copy-history content renderer 深化已完成当前阶段：只在 authoritative window 绑定一致时渲染，历史 row 投影为带 logical-line、continuation、clipped marker 的 styled `Line`，selection 和 active match 用 styled cells 表达，copy cursor 按 marker offset 投影为 content-local cursor，顶部 search row、底部 scrollbar/status、PageDown/滚轮滚动、match navigation、content-local mouse selection 和 row/line/part/cols/span/search/older 位置摘要已接入；logical-line 拼接提示和最终视觉 polish 仍可后续继续增强。
- 切片 86 后，copy-history 的 search row 和 scrollbar/status 已从工程占位推进到产品化视觉语言；仍不得把它作为 committed history truth 或从 live surface 反推历史。
- empty/exited/Terminal Picker content renderer 一期已进入当前阶段：empty pane 与 exited pane 已由单行 placeholder 升级为 CTA 内容，Terminal Picker overlay 已输出 search row、当前 workspace terminal list、selected row、new terminal row、overlay cursor 和 content action hit region。
- Terminal Picker 真实交互深化已进入当前阶段：query、过滤、selected row、上下键移动、Enter focus/close overlay、picker row click、new action feedback、最小 preview/detail 行和 no terminal input leak 已通过 reducer-owned state 与 content renderer 路径表达。
- Terminal Pool 数据源与 Picker 服务接线一期已进入当前阶段：Terminal Picker 打开可请求 terminal list，`TerminalPoolStore` 保存 loading/empty/error/items/stale guard，picker rows 会合并当前 workspace panes 与 pool items 并去重，pool row attach、create、restart、reconnect 均通过 terminal service effect/result message 回到 reducer。
- Terminal Pool 管理页一期已进入当前阶段：页面作为独立 terminal-pool content/page 实现，常规 viewport 下可见 list、selected detail、preview 摘要和 Attach/Edit/Kill action，窄高时按明确优先级压缩内容；跨 workspace/remote 管理、metadata Prompt、kill confirm、attach as tab/floating 和完整 terminal emulator preview 仍属后续深化。
- Prompt/Help 已有一期产品内容；命令面板、Help 搜索/分页、多字段表单、input click 光标定位和真实业务命令执行仍待后续深化。
- Tab/Workspace 已有一期产品入口；workspace delete、tab reorder、鼠标 tab strip、跨 workspace terminal attach 和 session persist/restore 完整树仍待后续深化。
- floating panel 已支持连续拖拽移动和 resize；attach as floating 和 Floating Overview 尚未完成。
- overlay 已落地 Terminal Picker、Terminal Pool Page、Workbench Tree、Prompt、Help 和基础 opaque cursor 归属，未完成 Floating Overview 的产品内容。
- toast 具备基础生命周期和 styled 渲染，不代表最终视觉 polish、动画或完整消息队列策略。
- hit region 已有内容、overlay、toast 合成基础，不代表完整鼠标交互产品语义。
- 切片 87 已根据真实复核反馈确认当前 TUI 仍未达到目标截图级视觉，只能作为复核未通过归档和 smoke 证据扩展。后续不得回退到旧 `tuiv2` 或恢复基础线框路径。
- 切片 88 已完成二轮 shell/pane 视觉重绘，但不能替代真实默认入口截图级复核；切片 89 前不得宣称当前 TUI 已达到用户截图目标。
- `RenderVM{Lines, Status}` 兼容输入字段已删除，`RenderVM` 只承载 `ShellVM`；`Frame.Lines`、`Frame.StyledLines`、`Frame.ANSILines` 只作为 `RenderResult` 的 plain/styled/ANSI 输出适配保留。
- large terminal output 性能基线已建立：`go test ./render -run '^$' -bench BenchmarkRendererLargeTerminalOutput -benchtime=1x` 当前单次基线约 6.91ms/op、6.75MB/op、12648 allocs/op。

## 23. 后续深化切片建议

后续任务必须继续遵守本文档，不得回退到裸文本 frame 或 terminal-only renderer。

当前优先级：

- 当前优先完成切片 89：真实默认入口截图级验收。
- 不得再把基础 styled chrome、Unicode glyph、ANSI 颜色、二轮重绘或 smoke 文本线框单独当作视觉完成证据。

建议后续切片：

- terminal-live content renderer 深化：在一期基础上完善 richer terminal styled cell、selection/search、clipped markers、status metadata、content-local hit region、truecolor/link/reverse/underline 和更完整的 terminal mode token。
- render performance 优化：在已有 large output benchmark 基线上继续做 content-level cache、layer dirty region、allocation 降低和 diff output；不得恢复旧 `RenderVM` 裸行输入。
- copy-history polish：在已完成搜索、滚动、scrollbar、鼠标选择和 position token 的基础上继续增强 logical-line 拼接提示、最终视觉层级、跨 logical-line 选择 affordance 和窄屏退化。

Terminal Pool 管理页的 render 边界：

- RenderVMBuilder 负责把 reducer-owned page state 投影为 `terminal-pool` ContentVM。
- render framework 只负责页面矩形、边框、裁切、层级、cursor 和 hit region 坐标转换。
- terminal-pool content renderer 只在分配的 content rect 内绘制 list/detail/preview/footer action。
- attach/edit/kill action 只产出稳定 content action hit region，不在 renderer 内执行业务逻辑。
- loading/empty/error 是页面内容状态，不是 toast 的替代；toast 只用于操作结果和全局反馈。
- preview 一期可以是 summary 或 last known live preview，但必须被 content rect 裁切，不能覆盖 chrome。
- 宽字符、emoji、combining mark 和 ANSI styled text 必须继续经过 cell-width helper，不得破坏页面边框或整行宽度。
- Terminal Pool Page layout 必须区别于小型 Terminal Picker：Terminal Picker 可以保持紧凑 overlay，Terminal Pool 需要更大的 page/overlay rect。
- 如果 viewport 高度不足，content renderer 必须主动压缩 preview/detail，而不是让 framework 裁掉 footer action 或 selected row。
- content action hit region 必须随 content rect 裁切同步收敛；不可见 action 不应保留可点击 region，可见 action 不得缺失 region。
- 页面 cursor 归属于 search field；overlay/page 打开期间不得复用底层 pane cursor。
