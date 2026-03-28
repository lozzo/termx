# TUI V2 架构设计草图 v1

日期：2026-03-28
状态：Draft

---

## 1. 目标

termx 的 TUI V1 已经在渲染层、terminal 展示层、tiling / floating 合成层积累了大量成熟经验；当前主要问题不在“能不能渲染”，而在于上层架构过重：

- `Model` 过大，承担了过多职责
- 结构状态、运行时状态、渲染调度状态边界不够清晰
- 控制流分散，新增功能容易牵一发而动全身
- 一些新对象已经存在，但 ownership 还没有完全坐实

因此，V2 的目标不是重写全部 TUI 能力，而是：

> 用 **数据驱动 + reducer/effect** 的方式重建 TUI 上层架构，
> 同时尽可能复用 V1 已验证成熟的渲染与 terminal 相关能力。

V2 的核心收益应体现在：

1. **单一状态树**，减少隐式同步与双状态源
2. **Action 驱动**，让输入、事件、命令都经过统一入口
3. **Reducer 纯状态变化**，降低控制流复杂度
4. **Effect 显式副作用**，将 attach / resize / stream / persistence 等操作边界化
5. **View 是状态投影**，而不是在渲染时偷偷修状态
6. **底层渲染能力复用 V1**，不重踩 V1 已经踩过的坑

---

## 2. 总体原则

### 2.1 V2 不是第二套随机 TUI

V2 不应成为“和 V1 完全无关的重写版本”。

更准确地说：

- **V1 的底层成熟能力要复用**
- **V2 重做的是上层状态组织与控制流**

即：

- 重写：状态树、action、reducer、effect、view model、顶层 shell 组织
- 复用：render/canvas/compositor、terminal snapshot/vterm 接入、tiling/floating 合成逻辑、运行时低层对接经验

### 2.2 不追求一步到位迁移全部功能

V2 必须先形成一个**最小闭环**，证明架构可行，再逐步迁移剩余能力。

不建议第一阶段就覆盖：

- 全部 prompt / picker
- 全部 workspace persistence
- 全部 terminal manager 能力
- 全部 prefix mode 行为
- 全部 layout/import/export 周边

### 2.3 先定义状态和边界，再写 reducer 和 view

V2 的设计顺序应是：

1. 明确状态树结构
2. 定义 action 列表
3. 定义 reducer 责任边界
4. 定义 effect 类型和 runner 边界
5. 定义 view model / renderer adapter 边界
6. 最后实现 Bubble Tea shell

如果一开始没有明确状态树，V2 很容易再次长成一个新的 god object。

### 2.4 view 只能投影状态，不应偷偷修状态

V2 中：

- `View()` / renderer adapter 只能读取状态
- reducer 只能改状态
- effect 只能做副作用并返回事件

任何“渲染时顺手修状态”的路径，都应视为设计违规。

---

## 3. 目录建议

建议显式引入 V2 目录，而不是继续在 V1 上做大规模原地手术。

推荐目录：

```text
tui/
  v1/   # 现有稳定主线，冻结为参考和回退基线
  v2/   # 新的数据驱动架构
```

如果短期不想马上整体移动 V1 文件，也可以在第一阶段采用：

```text
tui/
  v2/
```

同时保留现有 V1 文件在 `tui/` 根目录。

但从长期维护性看，我更推荐最终形成：

- `tui/v1/`：旧架构
- `tui/v2/`：新架构

这样更容易：

- 并行验证
- 逐步迁移
- 做功能对照
- 在必要时快速回退

---

## 4. V2 的核心架构

### 4.1 顶层结构

建议采用如下结构：

```text
Bubble Tea Shell
  -> dispatch(Action)

Store
  - State
  - Reducer
  - Effect runner

State
  - WorkbenchState
  - InputState
  - OverlayState
  - RuntimeState
  - RenderState

Effects
  - AttachTerminal
  - ResizeTerminal
  - StartStream
  - StopStream
  - LoadSnapshot
  - PersistWorkspace
  - RequestRenderTick

ViewModel / Selectors
  - active workspace/tab/pane
  - visible tiling tree
  - floating stack
  - picker/prompt projection
  - render inputs

Renderer Adapter
  - 复用 V1 render/canvas/compositor 逻辑

Runtime Adapter
  - 复用 V1 terminal/runtime/stream 逻辑
```

### 4.2 `Model` 在 V2 中的角色

V2 里的 `Model` 应非常薄，只承担：

- Bubble Tea shell
- 将 `tea.Msg` 转成 `Action` 或 `Event`
- 调 store/reducer/effect runner
- 触发 `View()` 的顶层投影

它不应继续承担：

- 结构真相
- runtime 真相
- render scheduling 真相
- terminal 生命周期主逻辑

换句话说：

> V2 的 `Model` 应是 shell，不应是 domain owner。

---

## 5. 状态树设计

V2 的关键在于状态树（single state tree）。

### 5.1 顶层 State

建议：

```go
type State struct {
    Workbench WorkbenchState
    Input     InputState
    Overlay   OverlayState
    Runtime   RuntimeState
    Render    RenderState
    UI        UIState
}
```

这里的 `State` 是唯一正式状态树。

### 5.2 `WorkbenchState`

负责结构真相：

```go
type WorkbenchState struct {
    Workspaces      map[string]WorkspaceState
    Order           []string
    ActiveWorkspace int
}

type WorkspaceState struct {
    Name      string
    Tabs      []TabState
    ActiveTab int
}

type TabState struct {
    Name            string
    Root            *LayoutNodeState
    Panes           map[string]PaneState
    Floating        []FloatingPaneState
    FloatingVisible bool
    ActivePaneID    string
    ZoomedPaneID    string
    LayoutPreset    int
}

type PaneState struct {
    ID         string
    Title      string
    TerminalID string
    Viewport   ViewportState
}
```

这部分是唯一结构真相。

### 5.3 `InputState`

负责输入状态机：

```go
type InputState struct {
    PrefixActive  bool
    DirectMode    bool
    PrefixSeq     int
    PrefixMode    PrefixMode
    PrefixTimeout time.Duration
    RawPending    []byte
    InputBlocked  bool
}
```

V2 中建议单独把它视为状态机子域。

### 5.4 `OverlayState`

负责 UI 叠加层：

```go
type OverlayState struct {
    ShowHelp        bool
    Prompt          *PromptState
    TerminalPicker  *TerminalPickerState
    WorkspacePicker *WorkspacePickerState
    TerminalManager *TerminalManagerState
    Notice          string
    Error           *ErrorState
}
```

这部分只代表当前 UI 叠加层显示状态。

### 5.5 `RuntimeState`

负责 runtime 协调态：

```go
type RuntimeState struct {
    Terminals       map[string]TerminalRuntimeState
    PaneSessions    map[string]PaneSessionState
    PendingAttaches map[string]AttachRequestState
    ResizeOwners    map[string]ResizeOwnerState
}
```

注意：
- runtime coordination state 和 structure state 分离
- terminal identity / metadata 也可在这里或单独 terminal state 中托管

### 5.6 `RenderState`

负责渲染调度与缓存元信息：

```go
type RenderState struct {
    Dirty             bool
    Pending           bool
    CacheKey          string
    InteractiveUntil  time.Time
    LastFlush         time.Time
    Interval          time.Duration
    FastInterval      time.Duration
    InteractiveWindow time.Duration
}
```

建议 V2 中不要把所有绘制细节塞进这里；这里只放调度和缓存元状态。

### 5.7 `UIState`

负责真正 Bubble Tea 壳层相关状态：

```go
type UIState struct {
    Width     int
    Height    int
    Quitting  bool
    HostTheme HostThemeState
}
```

---

## 6. Action / Event / Effect 边界

### 6.1 Action

Action 代表：

- 用户输入意图
- 内部状态操作意图
- 顶层交互动作

例子：

```go
type Action interface{}

type ActivateTabAction struct { Index int }
type FocusPaneAction struct { PaneID string }
type OpenTerminalPickerAction struct{}
type OpenWorkspacePickerAction struct{}
type SplitPaneAction struct { Direction SplitDirection }
type AttachTerminalAction struct { PaneID string; TerminalID string }
type ResizePaneAction struct { PaneID string; Cols, Rows int }
type ToggleHelpAction struct{}
type ShowPromptAction struct { Kind PromptKind }
```

原则：
- action 不直接带副作用
- action 先进入 reducer

### 6.2 Event

Event 代表副作用的结果或外部输入：

```go
type Event interface{}

type PaneOutputReceivedEvent struct { PaneID string; Data []byte }
type TerminalExitedEvent struct { TerminalID string; ExitCode int }
type SnapshotLoadedEvent struct { TerminalID string; Snapshot *protocol.Snapshot }
type AttachCompletedEvent struct { PaneID string; Session PaneSessionState }
```

原则：
- event 也进入 reducer
- reducer 再决定状态如何变化

### 6.3 Effect

Effect 代表副作用计划：

```go
type Effect interface{}

type AttachTerminalEffect struct { PaneID string; TerminalID string }
type ResizeTerminalEffect struct { PaneID string; Cols, Rows int }
type StartStreamEffect struct { PaneID string; TerminalID string }
type StopStreamEffect struct { PaneID string }
type LoadSnapshotEffect struct { TerminalID string }
type PersistWorkspaceEffect struct{}
type RequestRenderTickEffect struct{}
```

原则：
- reducer 产生 effect plan
- effect runner 执行 effect
- effect runner 执行结果回流为 event

---

## 7. Reducer 设计

### 7.1 总 reducer

顶层 reducer 应只负责分发：

```go
func Reduce(state State, action Action) (State, []Effect)
```

它应把 action 分发到：

- workbench reducer
- input reducer
- overlay reducer
- runtime reducer
- render reducer

### 7.2 子 reducer

建议按子域拆：

- `ReduceWorkbench(...)`
- `ReduceInput(...)`
- `ReduceOverlay(...)`
- `ReduceRuntime(...)`
- `ReduceRender(...)`

每个 reducer 只做纯状态变化，不直接操作外部 client/program。

### 7.3 关键约束

- reducer 不应直接访问 Bubble Tea `Program`
- reducer 不应直接做 RPC / attach / resize
- reducer 不应直接读写终端 runtime 对象
- reducer 只能返回新的 state 与 effect plan

---

## 8. Effect Runner 设计

V2 的 effect runner 是整个数据驱动架构的核心执行边界。

### 8.1 负责内容

- attach terminal
- resize terminal
- start/stop stream
- load snapshot
- persist workspace
- request render tick

### 8.2 建议分层

建议不要让 shell 直接执行所有 effect，而是通过 adapter：

```text
EffectRunner
  ├── RuntimeAdapter
  ├── RenderAdapter
  ├── PersistenceAdapter
  └── ProgramAdapter
```

### 8.3 优点

这样做的好处是：

- effect 不直接依赖 V1 的内部对象布局
- 可以更容易替换 / mock / 测试
- 可以把 V1 复用边界收得更稳

---

## 9. V1 能力复用策略

这是 V2 是否值得做的关键。

### 9.1 可以直接复用的 V1 能力

建议优先复用：

#### 渲染层
- `render.go` 中成熟的 pane/compositor/rendering 逻辑
- canvas/composed canvas
- floating overlay 合成与遮罩策略
- dirty region / partial redraw 的成熟经验

#### terminal 展示层
- snapshot -> vterm 装载逻辑
- vterm / terminal mirror 相关渲染逻辑
- pane 内容裁切、跟随、viewport 投影逻辑

#### runtime 底层经验
- attach / snapshot / stream / resize 等调用路径中的已验证边界
- 现有 terminal client 对接经验

### 9.2 不建议直接复用的 V1 上层组织

不要直接复用：

- V1 的大 `Model`
- V1 的 workspace 双向同步逻辑
- V1 的 render scheduling 与 runtime glue 组织方式
- V1 的隐式 state mutation 风格

### 9.3 复用方式：adapter，而不是复制一份

V2 不应该把 V1 的 render/runtime 代码复制一份进 `v2/`，而应该优先：

- 抽公共底层能力
- 通过 adapter 调用
- 在必要时对 V1 底层做小幅重构以便复用

也就是说：

> 复用能力，不复制架构。

---

## 10. Renderer Adapter 设计

V2 的 renderer adapter 应该做到：

### 输入
- 当前 workbench 的结构投影
- 当前 pane 的 terminal render 输入
- floating pane visible state
- overlay/prompt/picker 状态
- UI 宽高与主题

### 输出
- 最终 frame 字符串
- 必要的 cache metadata

### 关键原则
- 上层 reducer/store 不参与底层绘制细节
- 底层 renderer adapter 不拥有业务状态真相
- 只消费 selector/view model 结果

这意味着 V2 可以复用 V1 的成熟 render 逻辑，但把“谁来组织 frame 输入”这件事放到新架构上层。

---

## 11. Runtime Adapter 设计

### 输入
- attach / resize / start stream / stop stream 等 effect

### 输出
- event
  - attach completed
  - pane output received
  - terminal exited
  - snapshot loaded

### 关键原则
- runtime adapter 不直接改 store state
- 一切状态变化都通过 event 回 reducer

这样才能维持真正的数据驱动闭环。

---

## 12. V2 最小闭环范围（第一期）

第一期不要追求覆盖全部 V1 能力。

建议最小闭环只覆盖：

1. 单 workspace
2. tab/pane 基础切换
3. attach terminal 并显示内容
4. tiling 渲染
5. floating 渲染
6. 基础 input dispatch
7. render loop / basic batching
8. terminal output 流显示

第一期明确不要求覆盖：

- 所有 prompt
- 所有 picker
- 所有 workspace persistence
- 所有 terminal manager 功能
- 所有 prefix mode 行为
- 所有 layout persistence/import/export 细节

### 目标
最小闭环只证明一件事：

> 数据驱动 + reducer/effect 架构在 termx TUI 上可行，且能复用 V1 成熟渲染层。

---

## 13. 推荐实施阶段

### Phase A：设计与复用边界确认
输出：
- State schema
- Action / Event / Effect 列表
- V1 可复用能力清单
- Renderer/Runtime adapter 边界

### Phase B：Store + Shell + 最小 reducer/effect 骨架
输出：
- Bubble Tea shell
- store
- reducer dispatch
- effect runner
- 最小状态树

### Phase C：最小闭环打通
输出：
- attach terminal
- 基础 tiling render
- floating render
- basic input
- output streaming
- render tick

### Phase D：逐步迁移功能
输出：
- picker/prompt
- workspace 操作
- persistence
- terminal manager
- 高级 prefix mode

### Phase E：主线切换 / V1 退役准备
输出：
- 功能覆盖比较
- 回退路径
- 主线切换决策

---

## 14. 风险

### 风险 1：误把 V2 当成“只是换个文件夹”

如果只是把 V1 逻辑挪进 `tui/v2`，却不改变 state/action/effect 边界，那么 V2 会变成 V1 的复制品。

### 风险 2：误以为“这只是展示层”

termx TUI 不只是渲染，它还包含：
- streaming
- attach
- resize ownership
- pane lifecycle
- render timing

这些 effect 边界如果前期没设计好，后面会再次长成复杂总线。

### 风险 3：第一期范围过大

如果一开始就试图迁完全部 picker/prompt/persistence/manager 功能，V2 很容易失控。

### 风险 4：V1 底层复用边界不清，导致复制代码

如果 renderer/runtime 能力不是通过 adapter/抽取共享层复用，而是直接 copy，后续维护会变成双倍成本。

---

## 15. 成功标准

V2 第一阶段成功，不是指它比 V1 功能更多，而是指：

1. 存在清晰的 single state tree
2. 用户输入、terminal 事件、render tick 都通过 action/event 统一流转
3. reducer 只做状态变化，effect 只做副作用
4. view 是状态投影，不在渲染时修状态
5. V1 渲染层关键能力成功复用
6. 最小闭环可运行
7. 新增一个小功能时，不需要在巨大的 god object 上四处打补丁

---

## 16. 结论

V2 的正确方向不是：

- 再写一套全新 TUI
- 再踩一遍 V1 在渲染层已经踩过的坑

而是：

> 在保留 V1 底层成熟能力的前提下，
> 用数据驱动、action/reducer/effect 的方式重建 TUI 上层架构，
> 让状态边界、控制流边界和副作用边界真正清晰下来。

如果这条路线走通，V2 的价值会非常明确：

- 新功能更容易加
- 组件更容易测试
- 状态更容易追踪
- 不再一牵一发而动全身

这是一个比继续在 V1 上做局部 surgery 更值得投入的方向。
