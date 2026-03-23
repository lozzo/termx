# termx TUI E2E 计划

状态：Draft v1
日期：2026-03-23

本文件定义 TUI 端到端测试矩阵。

---

## 1. 原则

1. 先定义用户场景，再写测试
2. 测试名优先描述用户目标
3. 一个测试只验证一个主目标
4. 新主线按场景回迁 legacy E2E，不做整包平移

---

## 2. 第一批必须稳定的场景

### E1 启动即进入可工作 workspace

建议测试名：

- `TestE2ETUI_ScenarioLaunchIntoWorkingWorkspace`

断言：

- 默认进入 workspace
- 默认有可输入 shell pane
- 启动后可立即继续输入

### E2 split 并继续工作

建议测试名：

- `TestE2ETUI_ScenarioSplitAndContinueWorking`

断言：

- split chooser 正常出现
- 可以新建 terminal 或 connect existing
- split 后新 pane 可继续输入

### E3 复用 terminal 到新 tab

建议测试名：

- `TestE2ETUI_ScenarioConnectTerminalInNewTab`

断言：

- 同一 terminal 在两个 tab 中可见
- connect 后无崩溃、无串屏

### E4 复用 terminal 到 floating pane

建议测试名：

- `TestE2ETUI_ScenarioConnectTerminalInFloatingPane`

断言：

- tiled 和 floating 同时观察同一 terminal
- 焦点切换和 z-order 正常

### E5 修改 terminal metadata

建议测试名：

- `TestE2ETUI_ScenarioEditTerminalMetadata`

断言：

- 修改 name / tags 成功
- 所有已连接 pane 标题同步刷新

### E5.1 terminal manager 直接 connect 当前 pane

建议测试名：

- `TestE2ETUI_ScenarioTerminalManagerConnectsSelectedTerminalHere`

断言：

- terminal manager 可打开
- 选中 terminal 后可直接 connect 到当前 pane
- connect 后当前 pane 正常显示目标 terminal

### E6 workspace 切换

建议测试名：

- `TestE2ETUI_ScenarioSwitchWorkspace`

断言：

- workspace picker 可搜索
- workspace picker 可展示 `workspace -> tab -> pane` 树形结构
- create / switch 可用
- 可直接跳到目标 pane
- 切换后上下文清晰

### E7 从 layout 文件启动

建议测试名：

- `TestE2ETUI_ScenarioLaunchFromLayoutFile`

断言：

- layout 可进入工作现场
- resolve 流程可完成
- 失败时可降级

### E8 program-exited terminal restart

建议测试名：

- `TestE2ETUI_ScenarioRestartProgramExitedTerminal`

断言：

- terminal 中程序退出后的 pane 保留历史
- restart 后重新工作

---

## 3. 第二批共享 terminal 场景

### E9 resize 需要显式 acquire

- `TestE2ETUI_ScenarioSharedTerminalResizeRequiresExplicitAcquire`

### E10 tab auto-acquire

- `TestE2ETUI_ScenarioTabAutoAcquireResizeOnEnter`

### E11 close shared pane 不杀 terminal

- `TestE2ETUI_ScenarioCloseSharedPaneKeepsTerminalAlive`

### E12 kill terminal 后保留未连接 pane slots

- `TestE2ETUI_ScenarioKillSharedTerminalKeepsUnconnectedPaneSlots`

### E13 shared program-exited 状态同步

- `TestE2ETUI_ScenarioSharedProgramExitedTerminalPropagatesToAllPanes`

### E14 close pane 不通知其他客户端

- `TestE2ETUI_ScenarioClosePaneDoesNotNotifyOtherClients`

### E15 detach 不通知其他客户端

- `TestE2ETUI_ScenarioDetachDoesNotNotifyOtherClients`

---

## 4. 第三批浮窗与渲染场景

### E16 floating hide/show

- `TestE2ETUI_ScenarioFloatingHideShowKeepsScreenClean`

### E17 floating z-order

- `TestE2ETUI_ScenarioFloatingZOrderMatchesVisibleTopWindow`

### E18 floating center recall

- `TestE2ETUI_ScenarioFloatingCenterShortcutRecentersPane`

### E19 floating drag 恢复被遮挡内容

- `TestE2ETUI_ScenarioFloatingDragRestoresOccludedBody`

### E20 overlay 关闭无残影

- `TestE2ETUI_ScenarioOverlayClosesWithoutArtifacts`

---

## 5. 第四批 real-program 场景

### E21 python3 REPL

- `TestE2EReal_PythonREPL`

### E22 vi / vim alt-screen

- `TestE2EReal_ViFullscreen`

### E23 大输出 scrollback

- `TestE2EReal_LargeOutputSeq`

---

## 6. 执行节奏

建议按下面顺序恢复：

1. 第一批主路径
2. 第二批 shared terminal
3. 第三批浮窗和渲染
4. 第四批 real-program

---

## 7. 质量门禁

运行测试时统一使用仓库内 Go 工具链：

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./... -count=1
```

每次 TUI 改动至少执行：

1. 被改包的定向 `go test`
2. 全量 `go test ./... -count=1`
