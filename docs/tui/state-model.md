# termx TUI 状态模型规范

状态：Draft v1
日期：2026-03-23

这份文档面向 AI 编码实现。

目标：

1. 把领域状态、UI 状态、运行时状态、渲染状态拆清
2. 把字段、枚举、约束写死
3. 避免 AI 在实现时自行发明新的状态语义

---

## 1. 总原则

### 1.1 状态分层

TUI 状态必须拆成 4 层：

1. `DomainState`
2. `UIState`
3. `RuntimeState`
4. `RenderState`

### 1.2 严格约束

- `DomainState` 可持久化、可单测
- `UIState` 不参与 workspace/layout 持久化
- `RuntimeState` 不得进入 reducer 的持久化输出
- `RenderState` 只服务渲染，不得反向驱动业务

### 1.3 命名约束

- pane 和 terminal 的关系统一叫 `connection`
- 动作统一用 `connect / disconnect`
- 不使用 `attach / bind / binding`

---

## 2. 顶层状态

```go
type AppState struct {
    Domain  DomainState
    UI      UIState
    Runtime RuntimeState
    Render  RenderState
}
```

---

## 3. DomainState

```go
type DomainState struct {
    ActiveWorkspaceID WorkspaceID
    Workspaces        map[WorkspaceID]WorkspaceState
    WorkspaceOrder    []WorkspaceID
    Terminals         map[TerminalID]TerminalRef
    Connections       map[TerminalID]ConnectionState
}
```

约束：

- `Workspaces` 是领域主数据
- `Terminals` 只存 terminal 元信息快照，不存流式数据
- `Connections` 只表达连接关系和 owner 控制权

---

## 4. 标识类型

建议使用显式类型，避免 string 混用：

```go
type WorkspaceID string
type TabID string
type PaneID string
type TerminalID string
type OverlayID string
```

约束：

- 禁止直接用裸 `string` 互传不同 ID

---

## 5. WorkspaceState

```go
type WorkspaceState struct {
    ID          WorkspaceID
    Name        string
    Tabs        map[TabID]TabState
    TabOrder    []TabID
    ActiveTabID TabID
}
```

约束：

- `TabOrder` 决定 tab strip 顺序
- `ActiveTabID` 必须存在于 `Tabs`

---

## 6. TabState

```go
type TabState struct {
    ID                TabID
    Name              string
    RootSplit         *SplitNode
    Panes             map[PaneID]PaneState
    FloatingOrder     []PaneID
    ActivePaneID      PaneID
    ActiveLayer       FocusLayer
    AutoAcquireOwner  bool
}
```

约束：

- `RootSplit` 只包含 tiled pane
- `FloatingOrder` 只包含 floating pane
- `ActivePaneID` 可以落在 tiled 或 floating
- `ActiveLayer` 只能是 `tiled` 或 `floating`

---

## 7. PaneState

```go
type PaneState struct {
    ID           PaneID
    Kind         PaneKind
    Rect         Rect
    TitleHint    string
    TerminalID   TerminalID
    SlotState    PaneSlotState
    LastExitCode *int
}
```

### 7.1 PaneKind

```go
type PaneKind string

const (
    PaneKindTiled    PaneKind = "tiled"
    PaneKindFloating PaneKind = "floating"
)
```

### 7.2 PaneSlotState

```go
type PaneSlotState string

const (
    PaneSlotConnected PaneSlotState = "connected"
    PaneSlotEmpty     PaneSlotState = "empty"
    PaneSlotExited    PaneSlotState = "exited"
    PaneSlotWaiting   PaneSlotState = "waiting"
)
```

定义：

- `connected`
  - 当前 pane 已连接一个 terminal
- `empty`
  - 当前 pane 没有连接 terminal
- `exited`
  - 当前 pane 连接过的 terminal 中程序已退出，历史仍可见
- `waiting`
  - 当前 pane 是 layout/restore 的待解析槽位

约束：

- `connected` 时，`TerminalID` 必须非空
- `empty` 时，`TerminalID` 必须为空
- `exited` 时，`TerminalID` 可以保留
- `waiting` 时，`TerminalID` 一般为空

---

## 8. TerminalRef

```go
type TerminalRef struct {
    ID          TerminalID
    Name        string
    Command     []string
    Tags        map[string]string
    State       TerminalRunState
    ExitCode    *int
    Visible     bool
}
```

### 8.1 TerminalRunState

```go
type TerminalRunState string

const (
    TerminalRunStateRunning TerminalRunState = "running"
    TerminalRunStateExited  TerminalRunState = "exited"
    TerminalRunStateStopped TerminalRunState = "stopped"
)
```

约束：

- `TerminalRef` 是 terminal 的资源视图
- 不存 stream channel、snapshot buffer、renderer cache

---

## 9. ConnectionState

```go
type ConnectionState struct {
    TerminalID        TerminalID
    ConnectedPaneIDs  []PaneID
    OwnerPaneID       PaneID
    AutoAcquirePolicy AutoAcquirePolicy
}
```

### 9.1 AutoAcquirePolicy

```go
type AutoAcquirePolicy string

const (
    AutoAcquireDisabled AutoAcquirePolicy = "disabled"
    AutoAcquireTabEnter AutoAcquirePolicy = "tab_enter"
)
```

定义：

- `ConnectedPaneIDs`
  - 当前连接该 terminal 的 pane 集合
- `OwnerPaneID`
  - 当前 terminal 控制权所有者

约束：

- `OwnerPaneID` 为空时，表示当前还未选出 owner
- `OwnerPaneID` 非空时，必须存在于 `ConnectedPaneIDs`
- 同一 terminal 任一时刻最多一个 owner
- 任意 pane 或客户端都可以请求 owner

---

## 10. SplitNode

```go
type SplitNode struct {
    PaneID     PaneID
    Direction  SplitDirection
    Ratio      float64
    First      *SplitNode
    Second     *SplitNode
}
```

### 10.1 SplitDirection

```go
type SplitDirection string

const (
    SplitDirectionHorizontal SplitDirection = "horizontal"
    SplitDirectionVertical   SplitDirection = "vertical"
)
```

约束：

- 叶子节点仅有 `PaneID`
- 非叶子节点必须有 `First` 和 `Second`

---

## 11. UIState

```go
type UIState struct {
    Focus   FocusState
    Overlay OverlayState
    Mode    ModeState
    Notice  NoticeState
}
```

### 11.1 FocusState

```go
type FocusState struct {
    Layer         FocusLayer
    WorkspaceID   WorkspaceID
    TabID         TabID
    PaneID        PaneID
    OverlayTarget OverlayKind
}
```

### 11.2 FocusLayer

```go
type FocusLayer string

const (
    FocusLayerTiled    FocusLayer = "tiled"
    FocusLayerFloating FocusLayer = "floating"
    FocusLayerOverlay  FocusLayer = "overlay"
    FocusLayerPrompt   FocusLayer = "prompt"
)
```

### 11.3 OverlayState

```go
type OverlayState struct {
    Kind OverlayKind
    Data OverlayData
}
```

```go
type OverlayKind string

const (
    OverlayNone            OverlayKind = "none"
    OverlayTerminalPicker  OverlayKind = "terminal_picker"
    OverlayTerminalManager OverlayKind = "terminal_manager"
    OverlayWorkspacePicker OverlayKind = "workspace_picker"
    OverlayHelp            OverlayKind = "help"
    OverlayPrompt          OverlayKind = "prompt"
    OverlayConfirm         OverlayKind = "confirm"
)
```

### 11.4 ModeState

```go
type ModeState struct {
    Active    ModeKind
    Sticky    bool
    DeadlineAt *time.Time
}
```

```go
type ModeKind string

const (
    ModeNone      ModeKind = "none"
    ModePane      ModeKind = "pane"
    ModeResize    ModeKind = "resize"
    ModeTab       ModeKind = "tab"
    ModeWorkspace ModeKind = "workspace"
    ModeFloating  ModeKind = "floating"
    ModePicker    ModeKind = "picker"
    ModeGlobal    ModeKind = "global"
)
```

---

## 12. RuntimeState

```go
type RuntimeState struct {
    TerminalSessions map[TerminalID]TerminalRuntime
    PendingRequests  map[string]PendingRequest
    Timers           map[string]TimerState
}
```

```go
type TerminalRuntime struct {
    TerminalID     TerminalID
    Connected      bool
    SnapshotReady  bool
    StreamAttached bool
}
```

约束：

- `RuntimeState` 不持久化
- `RuntimeState` 不参与纯 reducer 的断言

---

## 13. RenderState

```go
type RenderState struct {
    ScreenWidth   int
    ScreenHeight  int
    Dirty         bool
    DirtyRegions  []Rect
    OverlayCache  map[string]string
    PaneFrameCache map[PaneID]string
    PaneBodyCache  map[PaneID]string
}
```

约束：

- 业务逻辑不能直接改 cache 内容
- 只允许 renderer 自己维护 render cache

---

## 14. ScreenModel

Render 层只接收稳定投影，不直接读完整 AppState。

```go
type ScreenModel struct {
    Workspace WorkspaceViewModel
    Focus     FocusViewModel
    Overlay   OverlayViewModel
    Notices   []NoticeViewModel
}
```

AI 实现要求：

- 先做 `AppState -> ScreenModel` 投影
- 再做 `ScreenModel -> string`
- 不允许 render 直接读 runtime service

---

## 15. 不变量

下面这些不变量必须有单测：

1. `connected` pane 必须有 `TerminalID`
2. `empty` pane 不得有 `TerminalID`
3. `OwnerPaneID` 必须在 `ConnectedPaneIDs` 中
4. `ActiveTabID` 必须存在
5. `ActivePaneID` 必须存在于当前 tab
6. `OverlayKind != none` 时，`Focus.Layer` 必须是 `overlay` 或 `prompt`
7. 同一 terminal 只能有一个 owner

---

## 16. AI 编码顺序

实现状态模型时，AI 应按下面顺序提交：

1. `id.go`
2. `rect.go`
3. `pane_state.go`
4. `workspace_state.go`
5. `connection_state.go`
6. `ui_state.go`
7. `runtime_state.go`
8. `render_state.go`

每一步都先写类型和不变量单测，再写辅助方法。
