# termx TUI Intent / Effect 规范

状态：Draft v1
日期：2026-03-23

这份文档面向 AI 编码实现。

目标：

1. 把输入统一翻译为显式 intent
2. 把 reducer 输出统一为 effect
3. 避免在 input handler 或 renderer 中直接改业务状态

---

## 1. 总数据流

统一数据流必须是：

1. 输入或事件进入
2. 适配为 `Intent`
3. `Reducer` 计算 next state
4. `Reducer` 输出 `Effects`
5. `Runtime` 执行 effects
6. runtime 结果再回流成 intent

禁止：

- 输入层直接改 state
- reducer 直接调 protocol client
- renderer 直接做业务副作用

---

## 2. Intent 总分类

建议定义：

```go
type Intent interface {
    intentName() string
}
```

按 8 类组织：

1. app lifecycle intents
2. navigation intents
3. pane intents
4. terminal connection intents
5. terminal control intents
6. overlay/prompt intents
7. workspace intents
8. runtime feedback intents

---

## 3. App Lifecycle Intents

```go
type AppInitIntent struct{}
type ScreenResizedIntent struct {
    Width  int
    Height int
}
type TickIntent struct {
    TimerID string
}
```

作用：

- 初始化
- 更新屏幕尺寸
- 处理 mode timeout、notice timeout

---

## 4. Navigation Intents

```go
type FocusPaneIntent struct {
    TargetPaneID PaneID
}

type MovePaneFocusIntent struct {
    Direction Direction
}

type FocusNextFloatingIntent struct{}
type FocusPreviousFloatingIntent struct{}
type RaiseFloatingIntent struct{}
type LowerFloatingIntent struct{}
```

约束：

- 这些 intent 只改焦点和层级
- 不直接 connect terminal

---

## 5. Pane Intents

```go
type SplitPaneIntent struct {
    SourcePaneID PaneID
    Direction    SplitDirection
}

type ClosePaneIntent struct {
    PaneID PaneID
}

type CreatePaneIntent struct {
    Kind PaneKind
}

type MoveFloatingPaneIntent struct {
    PaneID PaneID
    DX     int
    DY     int
}

type ResizeFloatingPaneIntent struct {
    PaneID PaneID
    DW     int
    DH     int
}

type CenterFloatingPaneIntent struct {
    PaneID PaneID
}
```

---

## 6. Terminal Connection Intents

这是当前主线最关键的一类。

```go
type ConnectTerminalIntent struct {
    PaneID     PaneID
    TerminalID TerminalID
    Source     ConnectSource
}

type DisconnectPaneIntent struct {
    PaneID PaneID
}

type ConnectTerminalInNewTabIntent struct {
    TerminalID TerminalID
}

type ConnectTerminalInFloatingPaneIntent struct {
    TerminalID TerminalID
}
```

```go
type ConnectSource string

const (
    ConnectSourcePicker          ConnectSource = "picker"
    ConnectSourceManagerHere     ConnectSource = "manager_here"
    ConnectSourceManagerNewTab   ConnectSource = "manager_new_tab"
    ConnectSourceManagerFloating ConnectSource = "manager_floating"
    ConnectSourceRestore         ConnectSource = "restore"
    ConnectSourceLayoutResolve   ConnectSource = "layout_resolve"
)
```

约束：

- 不再定义 `AttachTerminalIntent`
- 全部统一为 `ConnectTerminalIntent`

---

## 7. Terminal Control Intents

```go
type AcquireOwnerIntent struct {
    TerminalID TerminalID
    Requestor  OwnerRequestor
}

type ReleaseOwnerIntent struct {
    TerminalID TerminalID
    Requestor  OwnerRequestor
}

type ResizeTerminalIntent struct {
    TerminalID TerminalID
    Cols       int
    Rows       int
}

type UpdateTerminalMetadataIntent struct {
    TerminalID TerminalID
    Name       string
    Tags       map[string]string
}

type StopTerminalIntent struct {
    TerminalID TerminalID
}

type RestartProgramExitedTerminalIntent struct {
    PaneID PaneID
}
```

```go
type OwnerRequestor struct {
    PaneID    PaneID
    ClientID  string
    Reason    OwnerReason
}

type OwnerReason string

const (
    OwnerReasonResize   OwnerReason = "resize"
    OwnerReasonMetadata OwnerReason = "metadata"
    OwnerReasonManual   OwnerReason = "manual"
    OwnerReasonTabEnter OwnerReason = "tab_enter"
)
```

约束：

- `ResizeTerminalIntent` 必须先校验 owner
- `UpdateTerminalMetadataIntent` 也必须先校验 owner

---

## 8. Overlay / Prompt Intents

```go
type OpenTerminalPickerIntent struct{}
type OpenTerminalManagerIntent struct{}
type OpenWorkspacePickerIntent struct{}
type OpenHelpIntent struct{}
type CloseOverlayIntent struct{}

type SubmitPromptIntent struct {
    Value string
}

type CancelPromptIntent struct{}
```

---

## 9. Workspace Intents

```go
type CreateWorkspaceIntent struct {
    Name string
}

type SwitchWorkspaceIntent struct {
    WorkspaceID WorkspaceID
}

type SwitchTabIntent struct {
    TabID TabID
}

type CreateTabIntent struct {
    Name string
}

type WorkspaceTreeJumpIntent struct {
    WorkspaceID WorkspaceID
    TabID       TabID
    PaneID      PaneID
}
```

约束：

- `WorkspaceTreeJumpIntent` 是 workspace picker 的关键 intent
- 它必须同时完成 workspace/tab/focus 的一致跳转

---

## 10. Runtime Feedback Intents

```go
type TerminalConnectedIntent struct {
    PaneID     PaneID
    TerminalID TerminalID
}

type TerminalConnectionFailedIntent struct {
    PaneID     PaneID
    TerminalID TerminalID
    Err        error
}

type TerminalStoppedIntent struct {
    TerminalID TerminalID
}

type TerminalProgramExitedIntent struct {
    TerminalID TerminalID
    ExitCode   int
}

type TerminalMetadataUpdatedIntent struct {
    TerminalID TerminalID
    Name       string
    Tags       map[string]string
}
```

---

## 11. Effect 总分类

```go
type Effect interface {
    effectName() string
}
```

建议按 6 类组织：

1. terminal effects
2. overlay effects
3. timer effects
4. persistence effects
5. notice effects
6. render effects

---

## 12. Terminal Effects

```go
type ConnectTerminalEffect struct {
    PaneID     PaneID
    TerminalID TerminalID
}

type CreateTerminalEffect struct {
    PaneID  PaneID
    Command []string
    Name    string
}

type StopTerminalEffect struct {
    TerminalID TerminalID
}

type ResizeTerminalEffect struct {
    TerminalID TerminalID
    Cols       int
    Rows       int
}

type UpdateTerminalMetadataEffect struct {
    TerminalID TerminalID
    Name       string
    Tags       map[string]string
}
```

---

## 13. Overlay Effects

```go
type OpenOverlayEffect struct {
    Kind OverlayKind
}

type CloseOverlayEffect struct{}
type OpenPromptEffect struct {
    PromptKind string
}
```

---

## 14. Timer / Persistence / Notice Effects

```go
type ArmModeTimerEffect struct {
    TimerID   string
    Duration  time.Duration
}

type CancelTimerEffect struct {
    TimerID string
}

type SaveWorkspaceStateEffect struct {
    WorkspaceID WorkspaceID
}

type ShowNoticeEffect struct {
    Message string
}
```

---

## 15. Reducer Contract

建议统一函数签名：

```go
type ReduceResult struct {
    State   DomainState
    UI      UIState
    Effects []Effect
}

type Reducer interface {
    Reduce(state AppState, intent Intent) ReduceResult
}
```

约束：

- reducer 只返回结果，不做副作用
- runtime feedback 也必须重新进入 reducer

---

## 16. 输入映射优先表

第一阶段 AI 实现时，至少把这些输入映射清楚：

1. `Ctrl-f` -> `OpenTerminalPickerIntent`
2. `Ctrl-g` + `t` -> `OpenTerminalManagerIntent`
3. `Ctrl-w` -> `OpenWorkspacePickerIntent`
4. `Esc` -> `CloseOverlayIntent`
5. `Enter` on workspace tree leaf -> `WorkspaceTreeJumpIntent`
6. `Enter` in terminal manager -> `ConnectTerminalIntent{Source: manager_here}`

---

## 17. 第一批必须实现的 reducer 行为

1. `ConnectTerminalIntent`
2. `ClosePaneIntent`
3. `AcquireOwnerIntent`
4. `UpdateTerminalMetadataIntent`
5. `WorkspaceTreeJumpIntent`
6. `TerminalProgramExitedIntent`

这些是当前最容易反复返工的核心链路。

---

## 18. AI 编码注意事项

- 任何旧代码里出现的 `attach` 都应迁成 `connect`
- 不要把 owner 判断散写到 UI handler
- 不要把 connect 成功逻辑写在 renderer 里
- 不要把 workspace picker 的 jump 逻辑直接写死在 overlay view 代码里
