# termx TUI 测试落地计划

状态：Draft v1
日期：2026-03-23

这份文档面向 AI 编码实现。

目标：

1. 把测试从“策略”落到“先写什么文件”
2. 规定测试包结构和命名
3. 让 AI 可以按顺序交付而不是东补西补

---

## 1. 测试目录建议

建议按下面方式组织：

- `tui/domain/layout/*_test.go`
- `tui/domain/connection/*_test.go`
- `tui/domain/workspace/*_test.go`
- `tui/app/intent/*_test.go`
- `tui/app/reducer/*_test.go`
- `tui/testkit/*`
- `tui_e2e_test.go`
- `e2e_real_test.go`

---

## 2. 第一批必须先写的单测

### 2.1 layout

文件建议：

- `tui/domain/layout/layout_test.go`

先写：

1. split tree 构建
2. remove pane
3. adjacent pane 查找
4. rect 计算

### 2.2 connection

文件建议：

- `tui/domain/connection/state_test.go`

先写：

1. connect 默认 follower
2. owner 选举
3. owner 迁移
4. 任意请求方可 acquire owner
5. owner 必须在 connected panes 中

### 2.3 workspace

文件建议：

- `tui/domain/workspace/state_test.go`
- `tui/domain/workspace/picker_tree_test.go`

先写：

1. workspace switch
2. active tab/pane 不变量
3. workspace picker tree build
4. workspace tree jump

### 2.4 reducer

文件建议：

- `tui/app/reducer/reducer_test.go`

先写：

1. `ConnectTerminalIntent`
2. `ClosePaneIntent`
3. `StopTerminalIntent`
4. `TerminalProgramExitedIntent`
5. `WorkspaceTreeJumpIntent`

---

## 3. 第一批必须先写的回归测试

文件建议：

- `tui/app/reducer/overlay_regression_test.go`
- `tui/render/render_regression_test.go`

先写：

1. overlay close 后焦点恢复
2. owner 迁移后焦点不乱
3. unconnected pane 动作提示正确
4. program-exited pane 动作提示正确

---

## 4. 第一批 E2E 顺序

强制顺序：

1. `TestE2ETUI_ScenarioLaunchIntoWorkingWorkspace`
2. `TestE2ETUI_ScenarioSplitAndContinueWorking`
3. `TestE2ETUI_ScenarioConnectTerminalInNewTab`
4. `TestE2ETUI_ScenarioTerminalManagerConnectsSelectedTerminalHere`
5. `TestE2ETUI_ScenarioWorkspacePickerJumpsDirectlyToPane`

原因：

- 这 5 个场景刚好覆盖：
  - 启动
  - split
  - connect
  - manager
  - workspace tree jump

---

## 5. 第二批 E2E 顺序

1. `TestE2ETUI_ScenarioConnectTerminalInFloatingPane`
2. `TestE2ETUI_ScenarioEditTerminalMetadata`
3. `TestE2ETUI_ScenarioRestartProgramExitedTerminal`
4. `TestE2ETUI_ScenarioSwitchWorkspace`
5. `TestE2ETUI_ScenarioLaunchFromLayoutFile`

---

## 6. 第三批共享 terminal E2E

1. `TestE2ETUI_ScenarioSharedTerminalResizeRequiresExplicitAcquire`
2. `TestE2ETUI_ScenarioTabAutoAcquireResizeOnEnter`
3. `TestE2ETUI_ScenarioCloseSharedPaneKeepsTerminalAlive`
4. `TestE2ETUI_ScenarioKillSharedTerminalKeepsUnconnectedPaneSlots`
5. `TestE2ETUI_ScenarioSharedProgramExitedTerminalPropagatesToAllPanes`

---

## 7. E2E Harness 要求

AI 编码时，必须把 harness 抽到 `tui/testkit`，不要在每个 E2E 里重复造。

至少提供：

```go
type TUIScreenHarness interface {
    OpenTerminalPicker()
    OpenTerminalManager()
    OpenWorkspacePicker()
    SendText(text string)
    PressEnter()
    PressEsc()
    WaitScreenContains(s string)
    WaitFocusPane(id PaneID)
}
```

要求：

- 命令和断言分开
- 统一等待策略
- 屏幕稳定判断抽公共 helper

---

## 8. Real Program 测试顺序

在普通 E2E 稳定前，不要先恢复 real-program 测试。

顺序：

1. `python3`
2. `seq`
3. `vi` / `vim`

原因：

- `vi` 最容易放大 alt-screen 和 render 问题

---

## 9. 每次提交的最低验证

任何 TUI 代码提交至少执行：

```bash
PATH="$PWD/.toolchain/go/bin:$PATH" go test ./... -count=1
```

以及：

- 被改包的定向 `go test`

---

## 10. AI 执行顺序建议

AI 实现时按下面节奏最稳：

1. 先补单测
2. 再补 reducer
3. 再补 runtime adapter
4. 再补 renderer
5. 最后补 E2E

不要直接先写完整 UI。
