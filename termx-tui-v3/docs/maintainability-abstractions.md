# termx-tui-v3 维护性抽象方案

状态：已落档
日期：2026-06-09

## 1. 文档目的

`termx-tui-v3` 当前已经形成可用的自有 runtime、state、input、services、render framework、styled chrome 和视觉验收 harness。后续主要风险不再是“功能不可见”，而是代码聚合点继续膨胀，导致小视觉或交互修改必须同时理解 state、projection、layout、chrome、hit region 和 app command adapter。

本文档定义下一阶段维护性重构的抽象方向。

本文档回答：

- 哪些抽象值得提炼。
- 哪些抽象不应该引入。
- 提炼顺序如何安排，才能保持行为不变。
- 每类抽象必须守住哪些边界和 harness。

本文档不回答：

- 每个切片的最终 Go 文件名。
- 具体字段命名和所有函数签名。
- 是否立即移动 package 边界。
- 视觉风格是否重新设计。

## 2. 结论

下一阶段不要把 `termx-tui-v3` 做成通用 UI 框架，也不要引入 widget tree、DSL、插件系统或 Bubble Tea contract。

应该提炼的是当前已经稳定、已经重复出现的产品语义边界：

- `ActionCatalog`：动作、快捷键、footer token、pane/floating chrome button、help 文案和 hit region 的单一语义来源。
- `LayoutPlan`：所有可见矩形、content rect、overlay/floating/panel 层级和 hit region 的单一布局 truth。
- `ChromePrimitive`：terminal chrome 的边框、slot、token、action cluster 和 style 合成原语。
- `Projection`：`state.Root -> render.ShellVM` 的产品投影层，隔离 reducer-owned truth 和 render-only VM。
- `ContentProjectorRegistry`：terminal live、copy history、empty/exited pane、Terminal Pool、Workbench Tree、Prompt、Help 等内容的 state-to-`ContentVM` 轻量投影边界。

这些抽象的共同目标是减少散落字符串、重复坐标计算和跨层知识泄漏，而不是制造新的运行时或组件框架。

## 3. 当前维护性问题

### 3.1 大文件聚合

当前热点文件已经承担多类职责：

- `state/shell.go` 同时承载 workspace、tab、pane tree、floating、overlay、toast、command 和默认值。
- `render/framework.go` 同时承载 canvas、shell bar、pane chrome、floating、overlay、toast、content dispatch 和 ANSI frame 组合。
- `render/vm.go` 同时承载 shell、panel、floating、overlay、footer、content 的 state-to-VM 投影。
- `app/runtime_test.go`、`app/ui_input_test.go` 和 `render/framework_test.go` 覆盖面很大，容易把行为 contract、视觉 contract 和实现细节混在同一个断言里。

这些文件不是立即错误，但它们会让后续维护变慢：新增一个按钮或调整一个 chrome 样式时，容易同时触碰 action id、render slot、hit region、footer、help、app adapter 和 visual harness。

### 3.2 语义重复

同一个产品动作现在会出现在多个位置：

- action id catalog。
- footer mode action。
- pane/floating chrome action。
- hit region action。
- Help 页面动作。
- app reducer 或 command adapter 分发。
- smoke/visual compare marker。

只要这些位置不是从同一语义来源派生，就会出现“视觉上有按钮但点击没接上”或“help 写了动作但 chrome 没显示”的漂移。

### 3.3 坐标重复

renderer 和 layout plan 已经有边界，但 chrome action rect、floating resize rect、pane content rect、overlay cursor 和 toast 层级仍然容易在不同 helper 中重复推导。

长期规则应是：所有可点击区域都必须从可见 layout/chrome plan 派生，不能由 input/app 再猜一次位置。

## 4. 不引入的抽象

以下方向暂不引入：

- 通用 widget tree。
- flex/grid 布局框架。
- CSS-like 样式系统。
- 插件式 UI runtime。
- 跨包事件总线。
- 泛型 UI DSL。
- Bubble Tea `Program`、`Model`、`Msg`、`Cmd`、`KeyMsg`、`MouseMsg` 或依赖这些 contract 的组件。

原因：

- terminal UI 的关键复杂度在 cell width、ANSI、layer、hit region、core-v2 authoritative history 和 reducer/effect 边界，不在通用 widget 表达能力。
- 当前产品 chrome 已经稳定，抽象应服务于去重和边界收紧，而不是重建一套运行时。
- 通用 UI 框架会让 `state.Root`、`RenderVM` 和 `RenderResult` 之外出现新的状态或布局 truth，违反现有架构约束。

## 5. ActionCatalog

### 5.1 目标

`ActionCatalog` 是所有 UI action 的声明式语义来源。

它不执行业务逻辑，只描述动作的稳定 id、适用范围、默认显示 token、危险等级、快捷键/鼠标可见性和帮助文案。

目标形态示意：

```go
type ActionSpec struct {
	ID        ActionID
	Scope     ActionScope
	Label     string
	Short     string
	Glyph     string
	Danger    bool
	Primary   bool
	Available AvailabilityPolicy
}
```

### 5.2 使用边界

`ActionCatalog` 可以被这些层消费：

- render：生成 footer action、pane/floating action cluster、overlay action row 和 Help 内容。
- layout：为可见 action slot 生成 hit region。
- app：把 action id 映射为已有 semantic command 或 shell message。
- tests：验证可见 action、hit region 和 app adapter 不漂移。

`ActionCatalog` 不允许：

- 直接修改 `state.Root`。
- 直接发送 effect。
- 读取 protocol client、terminal service 或 history source。
- 根据屏幕坐标决定业务行为。

### 5.3 渐进切片

第一阶段只把现有散落 action token 收口到 catalog，不改行为。

建议顺序：

1. 把 footer mode action 声明迁入 catalog。
2. 把 pane chrome action 和 floating chrome action 迁入 catalog。
3. 把 Help 页面动作来源改为 catalog。
4. 增加 guard：每个可见 action id 必须在 catalog 中存在，每个可点击 action 必须有 app adapter 或明确只读说明。

## 6. LayoutPlan

### 6.1 目标

`LayoutPlan` 是 renderer 和 input 共享的布局 truth。

所有可见矩形和 hit region 必须来自同一份 plan：

```go
type LayoutPlanner interface {
	Plan(vm ShellVM, viewport Rect) LayoutPlan
}

type LayoutPlan struct {
	Header     Rect
	Footer     Rect
	Body       Rect
	Panels     []PanelLayoutPlan
	Floatings  []FloatingLayoutPlan
	Overlays   []OverlayLayoutPlan
	Toasts     []ToastLayoutPlan
	HitRegions []HitRegion
}
```

### 6.2 规则

- renderer 只能按 plan 绘制，不再在绘制时发明新的 content rect。
- app/input 只能消费 plan 输出的 hit region，不再按视觉常识猜测坐标。
- terminal resize、copy mode rebind 和 overlay cursor ownership 必须使用 plan 中的 content rect。
- plan 是纯函数，同一 `ShellVM + viewport` 必须稳定输出同一布局结果。

### 6.3 渐进切片

1. 把 `layout_plan.go` 中的 rect helper、hit region helper 和 measure helper 拆成明确小文件。
2. 增加 plan invariant harness：panel content rect、action hit region、floating resize handle、overlay cursor、toast hit region 都必须和可见 frame 对齐。
3. 把 terminal resize 和 copy rebind 的 content rect 查找统一通过 plan helper，不从 renderer 或 app 局部重算。

## 7. ChromePrimitive

### 7.1 目标

`ChromePrimitive` 抽象 terminal chrome，不抽象通用组件。

它应该表达：

- square box border。
- split border connection。
- top/right/bottom slot。
- action cluster。
- style token。
- layer/owner。
- display width 安全裁切。

目标形态示意：

```go
type ChromeFrame struct {
	Rect   Rect
	Kind   ChromeKind
	Style  ChromeStyle
	Slots  ChromeSlots
	Owner  string
	Layer  LayerKind
}

type ChromeSlots struct {
	Left   []ChromeToken
	Right  []ChromeToken
	Bottom []ChromeToken
}
```

### 7.2 使用场景

同一 primitive 应服务：

- tiled pane chrome。
- floating chrome。
- overlay chrome。
- toast chrome。

差异只来自 spec：

- pane：title/state/meta + `[zoom split-v split-h close]`。
- floating：无 split，只有可用动作 `[raise close]`，底边可有 `v`。
- overlay：title/state/action，如 `esc`。
- toast：severity、close、progress。

### 7.3 规则

- Chrome primitive 不读取 state。
- Chrome primitive 不决定 action 是否可用；只消费投影后的 token。
- Chrome primitive 必须输出可见 slot metadata，供 layout hit region 复用。
- 所有裁切必须使用 display cell width。

### 7.4 渐进切片

1. 从现有 pane/floating action cluster 抽出 `ChromeToken` 和 `ChromeSlots`。
2. 用 primitive 替换 pane/floating 顶边 action overlay。
3. 再迁移 overlay/toast chrome。
4. 保留现有 visual compare 作为行为不变验收。

## 8. Projection 层

### 8.1 目标

`Projection` 负责把 reducer-owned `state.Root` 转换为 render-only `ShellVM`。

当前 `RenderVMBuilder` 已承担这个职责。维护性重构的目标不是改变职责，而是把它从 renderer 绘制逻辑中分离出来，避免 render package 同时成为 state 投影器和绘制器。

目标形态示意：

```go
type ShellProjector struct {
	Actions ActionCatalog
	Content ContentProjectorRegistry
}

func (p ShellProjector) Project(root state.Root) render.ShellVM
```

### 8.2 规则

- Projection 可以读取 `state.Root`。
- Projection 可以调用 content projector registry 生成 `ContentVM`。
- Projection 可以应用产品展示规则，例如 active floating 存在时 tiled pane visual active 降级。
- Projection 不绘制字符。
- Projection 不计算最终屏幕 rect。
- Projection 不请求服务、不发送 effect、不修改 reducer-owned state。

### 8.3 package 策略

短期可以继续留在 `render` package 内做文件级拆分，降低 import churn。

中期再考虑拆出 `termx-tui-v3/viewmodel` 或 `termx-tui-v3/projection` package。只有当下面条件满足时才移动 package：

- `RenderVMBuilder` 已经不依赖 renderer 私有 helper。
- content projector registry 的输入输出稳定。
- action catalog 不造成 app/render 循环依赖。
- 全量 tui-v3 测试和 visual compare 均通过。

## 9. ContentProjectorRegistry 与 ContentRenderer

### 9.1 目标

`ContentProjectorRegistry` 负责把 `state.Root` 中的产品状态投影成 `ContentVM`。它属于 projection 层，不属于绘制层。

真正的 content renderer 继续只负责在 framework 分配好的 content rect 内绘制 `ContentVM`，不读取 `state.Root`，不访问 service，也不负责外部 chrome。

建议轻量 projector registry：

```go
type ContentProjector interface {
	ProjectContent(ctx ContentProjectorContext) ContentVM
}

type ContentProjectorContext struct {
	Root      state.Root
	Pane      state.PaneState
	Mode      ContentMode
	Actions   ActionCatalog
}
```

真正内容绘制边界保持为：

```go
type ContentRenderer interface {
	RenderContent(vm ContentVM, rect Rect) []Line
}
```

### 9.2 内容类型

首批可拆：

- terminal live。
- copy history。
- empty pane。
- exited pane。
- Terminal Picker。
- Terminal Pool。
- Workbench Tree。
- Prompt。
- Help。

### 9.3 规则

- content projector 可以读取 `state.Root`，但不能修改 state、请求服务或发送 effect。
- content projector 只输出 `ContentVM`，不画 pane/floating/overlay 外边框。
- content renderer 只消费 `ContentVM + Rect`，不读取 `state.Root`、terminal service 或 protocol client。
- copy history projector 只能消费 reducer 已保存的 authoritative `HistoryWindow`，不得从 live surface fallback。
- content renderer 输出的 hit region 必须是相对 content rect 的局部坐标，由 layout plan 统一平移。

## 10. 测试准入

维护性抽象切片必须以“行为不变”为默认目标。

每个非文档切片至少运行：

```sh
cd termx-tui-v3 && go test ./... -count=1
git diff --check
```

触碰 render、layout、hit region、chrome、projection 或 CLI visual snapshot 时，还必须运行：

```sh
cd termx-cli && go test ./cmd/termx -run 'TestV3VisualSnapshot|TestV3SmokeCommandIncludesVisualReviewCases|TestV3TmuxVisualCompareCapturesTargetAndDiffArtifacts' -count=1
make test-cli-v3-tmux-visual-compare
```

触碰 runtime、input、mouse 或 app action adapter 时，还必须运行相关 app/CLI interaction tests，至少覆盖：

- no terminal input leak。
- hit region action dispatch。
- pane/floating/footer action adapter。
- terminal content rect resize。
- copy mode rebind。

文档-only 切片至少运行：

```sh
git diff --check
```

## 11. 推荐切片顺序

### 11.1 权限噪音收口

目标：把普通源码、文档、JSON、图片等文件的 mode 噪音从维护性重构中剥离。脚本文件是否可执行按实际用途保留。

注意：该切片默认只能处理 `workflow.md` 当前允许范围内的文件。已删除的 legacy 目录不得为了维护性重构恢复；若必须重新设计外部 App/Web/control 面，必须先更新 workflow 范围表并单独说明原因。该切片必须单独提交，不和抽象代码变更混在一起。

### 11.2 机械拆大文件

目标：不改行为，只按职责拆现有大文件。

建议：

- `state/shell.go` 拆为 workspace、pane tree、floating、overlay、toast、defaults。
- `render/framework.go` 拆为 canvas、shell bar、pane chrome、floating chrome、overlay chrome、toast、content dispatch。
- `render/layout_plan.go` 拆为 measure、rects、hit regions。

### 11.3 ActionCatalog 收口

目标：让 footer、pane/floating chrome、help、hit region 和 app adapter 共享 action spec。

验收：

- action id catalog guard 覆盖所有可见 action。
- app adapter guard 覆盖所有会触发业务的 action。
- footer/help/chrome 不再各自手写同一 action label。

### 11.4 ChromePrimitive 收口

目标：pane/floating/overlay/toast 的边框和 slot 使用同一 chrome primitive。

验收：

- visual snapshot 不变。
- tmux visual compare 0 mismatch。
- hit region 与可见 action slot 对齐。

### 11.5 Projection 拆层

目标：把 state-to-VM 投影从绘制逻辑中分离。

验收：

- renderer 不读取 `state.Root`。
- projection 不绘制字符、不计算最终 rect。
- content renderer registry 输入输出稳定。

### 11.6 ContentProjectorRegistry

目标：每类内容独立投影和测试，chrome 不再混入 content projector 或 content renderer。

验收：

- terminal live、copy history、empty/exited、pool/tree/help/prompt 都有独立 content projector harness。
- content renderer 继续只消费 `ContentVM + Rect`。
- copy history 不引入 live surface fallback。

## 12. 风险与止损

维护性重构必须避免“大重构长时间不可运行”。

止损规则：

- 任一切片不能同时移动 package、改行为、改视觉 target。
- 若 visual compare 出现 mismatch，先判断是否本切片目标允许；不允许时必须回到行为不变。
- 不用抽象覆盖尚未重复三次的代码路径。
- 抽象不能让简单的 chrome token 变成多层间接查找。
- 若新抽象需要 mock 大量内部对象才能测试，说明边界过重，应退回小 helper。

## 13. 完成定义

维护性抽象阶段不是以文件行数最小为目标，而是以稳定边界为目标。

完成后应满足：

- 新增 UI action 只需改 catalog、adapter 和必要 content/visual harness，不需要在 footer、help、hit region、chrome 多处手写。
- 新增 pane/floating/overlay/toast chrome 样式只需调整 chrome primitive/spec，不需要复制边框和 action slot 逻辑。
- 新增 content 页面只需实现 content projector 和必要 content renderer，不改 shell layout、pane chrome 或 FrameSink。
- renderer 继续只消费 VM 和 layout plan，不读取 service、runtime 或 protocol client。
- copy mode 继续只消费 core-v2 authoritative `HistoryWindow`。
