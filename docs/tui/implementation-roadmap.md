# termx TUI 实施路线图

状态：Draft v1

本文件把当前产品规格落到“先改哪些代码、为什么先改、哪些地方能复用、哪些地方要重构”。

---

## 1. 当前代码基线判断

结论：

- 旧设计文档应继续废弃
- 现有 TUI 代码不建议整体推倒重写
- 正确路线是：保留底层可复用资产，在现有代码上按 TDD 做分层重构

原因：

- 已有 server / protocol / vterm / TUI harness / e2e 基础设施
- 已有 attach、picker、floating、workspace state 的可运行基础
- 直接重写会把已经修好的链路重新做坏

---

## 2. 当前代码阅读结论

## 2.1 已有可复用资产

- `protocol/`
  - create / attach / list / snapshot / resize / set_metadata / set_tags 已有通路
- `termx.go`
  - server 侧 terminal create / attach / resize / kill 已成熟
- `vterm/`
  - 终端渲染底层已可复用
- `tui_e2e_test.go`
  - 已有稳定的屏幕级 e2e harness
- `tui/workspace_state.go`
  - 已有 workspace state 持久化基础

## 2.2 当前最需要改的逻辑

### resize 语义还是“几何变化即 resize”

当前 `tui/model.go` 里的 `resizeVisiblePanesCmd()` 会遍历所有可见 pane，并直接对 running pane 发送 `client.Resize(...)`。

这意味着：

- 只要布局变化
- 或 window size 变化
- 或 split/floating 几何变化

就会自动改写 terminal size。

这与新规格冲突，因为新规格要求：

- resize 必须显式 acquire
- pane 几何变化本身不应自动触发 PTY resize

### shared terminal resize 会同步到所有 pane runtime

当前 `handlePaneResize()` 会调用 `resizeTerminalPanes(terminalID, "", cols, rows)`，并且现有单测已经锁定了“同 terminal 的所有 pane 一起 resize”这一旧行为。

这部分需要重构成：

- terminal runtime size 更新
- pane display size 独立
- acquire/lock 驱动 terminal size 变更

### pane close 和 terminal kill 语义还未完全分层

当前：

- `closeActivePaneCmd()` 只是发 `paneDetachedMsg`
- `killActiveTerminalCmd()` 直接 kill terminal
- `removeTerminal()` 会把所有绑定 pane 都删掉

基础方向是对的，但还缺：

- 多客户端广播语义
- close/detach 的静默规则
- remove 的 notice/confirm

### 快捷键与 mode 仍然偏旧模型

当前 `tui/input.go` / `tui/model.go` 中：

- resize mode 仍然直接联动 `resizeVisiblePanesCmd()`
- 底层实现仍保留 `viewport` 命名，`fit/fixed/readonly/pin` 也还是通过 `Ctrl-v` 入口管理

这需要后续继续往：

- `workspace / tab / pane / terminal`

收口。

---

## 3. 文件级改造建议

## 3.1 优先保留

- `protocol/`
- `termx.go`
- `terminal.go`
- `vterm/`
- `tui/render.go`
- `tui/workspace_state.go`
- `tui_e2e_test.go`

## 3.2 优先重构

- `tui/model.go`
  - resize 触发路径
  - shared terminal runtime 同步逻辑
  - pane close / terminal remove / exited retained
  - notice / prompt / confirm
- `tui/input.go`
  - acquire resize 入口
  - tab auto-acquire 入口
  - kill/remove confirm 路径
- `tui/client.go`
  - 若协议需要新增事件/命令，从这里扩展
- `tui/workspace_state.go`
  - 持久化 `size_lock` / tab auto-acquire 等新状态

## 3.3 后续逐步清理

- 底层 `viewport` 命名与对外 `pane/display` 文案之间的实现层鸿沟
- 旧 resize mode 行为
- 更细的 help / status 精简与视觉分层

---

## 4. TDD 实施顺序

## 4.1 第一阶段：共享 terminal 生命周期

先补 e2e：

- `TestE2ETUI_ScenarioCloseSharedPaneKeepsTerminalAlive`
- `TestE2ETUI_ScenarioKillSharedTerminalClosesAllBoundPanes`
- `TestE2ETUI_ScenarioSharedExitedTerminalPropagatesToAllPanes`

原因：

- 这几项最接近当前实现
- 风险低
- 能先把 pane/terminal 语义锁住

## 4.2 第二阶段：共享 terminal resize acquire

再补 e2e：

- `TestE2ETUI_ScenarioSharedTerminalResizeRequiresExplicitAcquire`
- `TestE2ETUI_ScenarioTabAutoAcquireResizeOnEnter`
- `TestE2ETUI_ScenarioSharedTerminalResizeWarnsWhenSizeLockEnabled`

然后重构：

- `resizeVisiblePanesCmd()`
- `handlePaneResize()`
- shared terminal runtime 同步逻辑

## 4.3 第三阶段：多客户端协作通知

补 e2e / integration：

- close pane 不广播
- detach 不广播
- remove 广播 notice
- readonly/observer 禁止 remove

当前状态：

- 已完成：
  - `TestE2ETUI_ScenarioClosePaneDoesNotNotifyOtherClients`
  - `TestE2ETUI_ScenarioDetachDoesNotInterruptOtherClients`
  - `TestE2ETUI_ScenarioRemoteKillShowsNoticeAndClosesSharedPanes`
  - TUI 已订阅 `EventTerminalRemoved`，远端 remove 会关闭本地绑定 pane 并显示 notice
  - observer attachment 现在不能通过 protocol `kill/remove`
  - readonly pane 现在不能通过 TUI `kill/remove`
  - TUI chrome 已显示 `access:collab|observer` 与 `[obs]/[ro]` 轻量标记
- 已落地测试：
  - `TestHandleRequestKillDeniedForObserverAttachment`
  - `TestHandleRequestKillAllowedForCollaboratorAttachment`
  - `TestE2E_ObserverCannotKill`
  - `TestReadonlyViewportBlocksKillTerminal`
  - `TestRenderStatusShowsAccessModeInRuntimeSummary`
  - `TestPaneTitleAddsObserverAndReadonlyBadges`
  - `TestE2ETUI_PaneChromeShowsReadonlyAndAccessStatus`
- 待完成：
  - 若未来协议补充操作者身份，可把 notice 升级为“被谁移除”

## 4.4 第四阶段：workspace/layout 恢复

补 e2e：

- layout -> workspace instance
- workspace restore
- shared terminal binding restore
- tab auto-acquire restore

当前状态：

- 已完成：
  - workspace state 恢复后，active tab 的 `auto-acquire resize` 会立即生效
  - workspace 切换到带 `auto-acquire resize` 的 tab 时会立即生效
  - startup workspace restore 已覆盖 shared terminal binding 场景
  - startup layout 现在允许通过重复 `_hint_id` 显式复用同一个 terminal 到多个 pane
  - 当重复 `_hint_id` 当前未命中 terminal 时：
    - `create` 现在只创建一次并复用到所有同 hint pane
    - `prompt` 现在只提示一次并把 attach 结果传播到所有同 hint pane
  - layout `skip` 降级路径已有真实 e2e 覆盖
- 已落地测试：
  - `TestLoadWorkspaceStateCmdRestoresActiveTabAutoAcquireResize`
  - `TestWorkspaceSwitchRestoresAutoAcquireResizeOnActivatedWorkspace`
  - `TestE2ETUI_ScenarioStartupRestoresWorkspaceStateWithSharedTerminalBinding`
  - `TestBuildWorkspaceFromLayoutSpecAllowsExplicitHintReuseAcrossPanes`
  - `TestE2ETUI_StartupLayoutCanReuseExplicitHintAcrossTiledAndFloatingPanes`
  - `TestLoadLayoutSpecCmdPromptPolicyAttachReusesExplicitHintAcrossPanes`
  - `TestLoadLayoutSpecCmdCreatePolicyReusesExplicitHintAcrossPanes`
  - `TestE2ETUI_CommandLoadLayoutPromptReusesExplicitHintAcrossPanes`
  - `TestE2ETUI_CommandLoadLayoutSkipLeavesWaitingPaneAndKeepsLayoutUsable`
- 待完成：
  - layout `skip` 对重复 `_hint_id` 的整组跳过语义，可再补一条更直接的场景测试

## 4.5 第五阶段：用户可见术语与帮助文案收口

已完成：

- picker / prompt / notice 中面向用户的 `viewport` 文案已收口为 `pane`
- 状态栏与 help 中 `Ctrl-v` 已改为 `display` 语义，不再把 `view` 当成一等用户概念
- help 中共享 terminal 文案已改成“先 acquire resize control，再改 PTY size”
- 相关单测、e2e、real e2e 已随文案一起锁定

待完成：

- 若后续继续重构输入模型，可把内部 `prefixModeViewport` / `ViewportMode*` 与外部文案进一步解耦
- help/status 仍可继续压缩，给后续视觉重做留空间

## 4.6 第六阶段：full-screen / alt-screen 复用渲染回归基线

已完成：

- attach 现有 terminal 到新 tab 时，已锁定“snapshot 先落地，再接后续增量输出”
- 现在额外补上了 split / floating 两条回归覆盖
- 针对 full-screen / alternate-screen terminal 的复用，tab / split / floating 三条 attach 路径都有 e2e 基线
- 对 floating 几何变化的增量重绘，已补上“旧遮挡区域必须整 pane 恢复”的单测基线
- 当前实现策略是：floating move/resize 直接重建该 tab 的合成画布；同时所有 full rebuild 路径都强制整 pane 绘制，避免 `htop`/`vim` 类按 dirty-row 增量刷新的 pane 在重叠区出现闪烁、短暂消失或残影

待完成：

- 若后续继续出现 `htop` / `vim` / `less` 类真实程序复用异常，可再补 real e2e 回归

---

## 5. 第一批实际编码切入点

## Step 1

先在测试层写出 3 个共享 terminal 生命周期 e2e：

- close shared pane
- kill shared terminal
- shared exited retained

## Step 2

最小实现修改：

- 校正 pane close 后的布局与 active pane 选择
- 校正 kill terminal 后 notice 和批量 pane 移除
- 校正 exited retained 在多个 pane 的一致传播

## Step 3

跑：

- `go test ./... -count=1`

## Step 4

再进入 resize acquire 重构。

---

## 6. 明确暂不做的事

以下内容先不在第一批编码中处理：

- 完整重做全部快捷键
- 最终 observer/collaborator 权限系统 UI
- layout 文件格式最终定版
- 底层 `viewport` 实现命名整体改名

这些留到主线稳定后再继续。

---

## 7. 里程碑

### M1

- shared terminal close / kill / exited 语义稳定

### M2

- resize acquire / size_lock / tab auto-acquire 稳定

### M3

- 多客户端通知与权限语义稳定

### M4

- workspace/layout 恢复链路稳定

### M5

- 用户可见术语已基本收口到 `workspace / tab / pane / terminal`
- 当前代码具备继续做 help/status/UI 重绘的稳定基线
