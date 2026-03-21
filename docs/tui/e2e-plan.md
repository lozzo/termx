# TUI E2E 测试矩阵

本文件定义“先有场景，再有实现”的测试入口。

## 原则

1. 每个测试只服务一个用户目标
2. 测试名尽量使用场景语言，而不是内部实现语言
3. 后续重构 UI / 概念时，优先维护这些场景，而不是维护旧设计稿里的术语

## 第一批必须稳定的场景

### S1. 直接启动即可工作

目标：

- 用户直接运行 `termx`
- 进入一个可工作的 workspace
- 默认能立即操作 pane / shell

现有覆盖：

- 已有部分启动 e2e，但仍带旧概念痕迹

建议测试名：

- `TestE2ETUI_ScenarioLaunchIntoWorkingWorkspace`

## S2. 新建 split 并继续输入

目标：

- 用户 split 后，能选择新 terminal 或复用 terminal
- 新 pane 成功创建后，继续正常输入

现有覆盖：

- 有，但散落在旧流程测试里

建议测试名：

- `TestE2ETUI_ScenarioSplitAndContinueWorking`

## S3. 复用 terminal 到新 tab

目标：

- 同一个 terminal 能在两个 tab 中出现
- attach 后不崩溃、不闪退

建议测试名：

- `TestE2ETUI_ScenarioReuseTerminalInNewTab`

## S4. 复用 terminal 到 floating pane

目标：

- tiled 与 floating 同时观察同一 terminal
- 焦点切换、z-order、尺寸更新稳定

建议测试名：

- `TestE2ETUI_ScenarioReuseTerminalInFloatingPane`

## S5. 修改 terminal metadata

目标：

- 从 picker 或全局入口编辑 terminal `name/tags`
- 所有 attach pane 同步刷新

现有覆盖：

- `TestE2ETUI_ScenarioEditTerminalMetadata`

## S6. terminal 退出后恢复

目标：

- exited pane 上 restart
- 新 terminal 重新绑定到当前 pane

现有覆盖：

- 现有 e2e 已覆盖 exited 后 restart 并继续输入

建议测试名：

- `TestE2ETUI_ScenarioRestartExitedTerminal`

## S7. workspace 选择 / 切换

目标：

- 进入 workspace picker
- 创建 / 切换 workspace
- 切换后上下文清晰

现有覆盖：

- 已有 workspace picker / workspace switch e2e

建议测试名：

- `TestE2ETUI_ScenarioSwitchWorkspace`

## S8. 从 layout/workspace 文件启动

目标：

- 从指定文件启动 workspace
- 若有 floating / resolve，也能稳定进入

现有覆盖：

- startup layout / load-layout prompt / skip 已有 e2e
- 重复 `_hint_id` 在 startup create 与 prompt attach 路径上已有共享绑定覆盖

建议测试名：

- `TestE2ETUI_ScenarioLaunchFromLayoutFile`

## 第二批场景

- 多浮窗管理
- picker 大量 terminal 搜索
- attach 后 metadata 同步
- workspace 恢复失败降级
- render/backpressure 性能回归
- floating move/resize 时重叠 pane 不闪烁、不短暂消失
- floating drag 后，之前被遮挡的 pane body 必须完整恢复
- 共享 terminal 的 acquire resize / size lock
- 共享 terminal 的 pane close / terminal kill 联动

## 当前代码里的对应基线

当前仓库已经有大量 e2e / real e2e 测试，它们提供了能力基线；下一步不是全部推倒，而是：

- 逐步把旧测试重组为“场景化命名”
- 对新增重置设计的场景补充更清晰的断言

## 本轮重构后的优先动作

1. 新增 2~4 个“场景名”更清晰的 e2e 测试
2. 保持全量测试通过
3. 再开始重构 workspace 入口、概念文案和 UI 结构

## 第三批必须补齐的共享 terminal 场景

### S9. 同一 terminal 在 tiled 和 floating 间抢占尺寸

目标：

- resize 只有在显式 acquire 后才生效
- 其他 pane 仅观察，不隐式改写 terminal size

建议测试名：

- `TestE2ETUI_ScenarioSharedTerminalResizeRequiresExplicitAcquire`

### S10. 进入 tab 自动 acquire resize

目标：

- tab 配置开启 auto-acquire
- 用户切回 tab 时自动获取 resize 控制
- 未开启的 tab 不自动改写 terminal size

建议测试名：

- `TestE2ETUI_ScenarioTabAutoAcquireResizeOnEnter`

### S11. size lock 为 warn 时 resize 需要提示

目标：

- terminal 带 `termx.size_lock=warn`
- acquire/resize 前弹出提示
- 用户确认后才提交 resize

建议测试名：

- `TestE2ETUI_ScenarioSharedTerminalResizeWarnsWhenSizeLockEnabled`

### S12. 关闭共享 pane 不销毁 terminal

目标：

- close 当前 pane
- 其他共享 pane 继续可用
- terminal 不被误杀

建议测试名：

- `TestE2ETUI_ScenarioCloseSharedPaneKeepsTerminalAlive`

### S13. 销毁 terminal 后所有共享 pane 一并关闭

目标：

- kill/remove terminal
- 所有绑定该 terminal 的 pane 自动消失
- tab 自动重排，不留空壳 pane

建议测试名：

- `TestE2ETUI_ScenarioKillSharedTerminalClosesAllBoundPanes`

### S14. 共享 terminal exited retained 后多 pane 一致进入 exited

目标：

- terminal 自然退出
- 所有共享 pane 一致显示 exited
- 任一 pane 可触发 restart

建议测试名：

- `TestE2ETUI_ScenarioSharedExitedTerminalPropagatesToAllPanes`

### S15. 多客户端 close pane 保持静默

目标：

- A 客户端关闭自己的 pane
- B 客户端不收到误导性的 removed notice
- shared terminal 继续可用

现有覆盖：

- `TestE2ETUI_ScenarioClosePaneDoesNotNotifyOtherClients`

### S16. 多客户端 detach 保持静默

目标：

- A 客户端 detach
- B 客户端继续工作
- 不出现 remote removed notice

现有覆盖：

- `TestE2ETUI_ScenarioDetachDoesNotInterruptOtherClients`

### S17. 多客户端 remote remove 广播 notice

目标：

- A 客户端 kill/remove terminal
- B 客户端收到 notice
- B 客户端对应 shared pane 自动关闭

现有覆盖：

- `TestE2ETUI_ScenarioRemoteKillShowsNoticeAndClosesSharedPanes`

### S18. startup workspace restore 恢复 shared terminal 绑定

目标：

- 启动时读取 workspace state
- 多个 tab 重新绑定到同一个 terminal
- active tab 的 auto-acquire resize 立即恢复生效

现有覆盖：

- `TestE2ETUI_ScenarioStartupRestoresWorkspaceStateWithSharedTerminalBinding`

### S19. startup layout 通过显式 hint 复用 shared terminal

目标：

- layout 中多个 pane 显式写同一个 `_hint_id`
- startup layout 后多个 pane 绑定同一个 terminal
- 不误创建重复 terminal

现有覆盖：

- `TestE2ETUI_StartupLayoutCanReuseExplicitHintAcrossTiledAndFloatingPanes`

### S20. layout skip 降级后仍可继续操作

目标：

- `load-layout ... skip` 不弹 picker
- 未匹配 pane 保持 waiting
- layout 进入后仍可继续后续操作

现有覆盖：

- `TestE2ETUI_CommandLoadLayoutSkipLeavesWaitingPaneAndKeepsLayoutUsable`

### S21. 全屏 terminal 复用后保留 alt-screen 基线

目标：

- 一个处于 full-screen / alternate-screen 的 terminal 被复用到新 tab / split / floating
- 新 pane 首帧先完整恢复 snapshot，而不是只显示后续增量输出
- 后续增量输出不会把既有 alt-screen 内容整块抹掉

现有覆盖：

- `TestE2ETUI_NewTabReusePreservesAltScreenSnapshotBeforeIncrementalUpdates`
- `TestE2ETUI_SplitReusePreservesAltScreenSnapshotBeforeIncrementalUpdates`
- `TestE2ETUI_FloatingReusePreservesAltScreenSnapshotBeforeIncrementalUpdates`
- 相关 attach 单测已覆盖 tab / split / floating 三条路径
