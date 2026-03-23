# termx TUI 测试策略

状态：Draft v1
日期：2026-03-23

---

## 1. 测试目标

TUI 测试不只是验证“能显示”，而是验证 4 类风险：

1. 产品语义回归
2. 共享 terminal 规则回归
3. 渲染回归
4. 交互路径回归

因此测试必须分层：

- 单元测试
- 组件/场景回归测试
- 端到端测试
- benchmark / 性能回归

---

## 2. 测试金字塔

### 2.1 单元测试

覆盖纯逻辑：

- layout tree
- workspace state import/export
- connection owner/follower policy
- intent mapping
- reducer
- slot transition
- prompt / overlay state transition

特点：

- 快
- 稳定
- 可高覆盖率

### 2.2 场景回归测试

覆盖中等粒度行为：

- split chooser
- picker 过滤与选择
- terminal manager 动作
  - connect here
  - new tab
  - floating
  - edit
  - stop
- unconnected/program-exited pane 动作
- floating z-order / center / hide-show
- status 文案与 help 内容

特点：

- 以场景命名
- 不强依赖最终像素级输出
- 验证一个完整用户动作链

### 2.3 端到端测试

覆盖真实工作流：

- 启动
- 输入
- connect
- shared terminal
- restore
- layout startup
- real program

特点：

- 与真实 server / protocol / stream 协作
- 防止纸面架构正确、真实行为错误

### 2.4 benchmark

覆盖性能和渲染退化：

- 大输出
- overlay 开关
- floating drag / resize
- shared alt-screen program
- 高频 render / backpressure

---

## 3. 单元测试计划

### 3.1 优先级最高的包

第一批必须先建立单测的包：

1. `tui/domain/layout`
2. `tui/domain/connection`
3. `tui/domain/workspace`
4. `tui/app/intent`
5. `tui/app/reducer`

### 3.2 核心断言

必须覆盖：

- owner 选举与迁移
- connect 默认 follower
- owner 获取
- close pane / stop terminal / exit terminal 的状态差异
- overlay 打开关闭的焦点回退
- prefix / mode 的非法输入忽略
- restore 失败降级

### 3.3 单元测试命名

建议统一场景式命名：

- `TestConnectionOwnerMigratesWhenOwnerPaneRemoved`
- `TestConnectionAnyClientCanAcquireOwner`
- `TestReducerStopTerminalKeepsUnconnectedPaneSlots`
- `TestIntentEscAlwaysClosesTransientOverlay`

---

## 4. 回归测试计划

### 4.1 回归测试关注点

回归测试主要盯下面几类已知高风险区：

1. floating 残影
2. shared terminal 串屏
3. overlay 关闭后脏 UI
4. mode/prefix 卡死
5. metadata 更新后标题不同步

### 4.2 回归用例池

建议建立固定回归池：

- `launch`
- `split`
- `picker`
- `manager`
- `workspace-switch`
- `floating-basic`
- `floating-overlap`
- `shared-terminal-basic`
- `shared-terminal-resize`
- `shared-terminal-exit`
- `restore`
- `layout-startup`

### 4.3 回归触发条件

下面改动必须触发对应回归：

- render 相关改动
- input / mode 相关改动
- connection policy 改动
- floating 改动
- restore / layout 改动

---

## 5. 端到端测试计划

### 5.1 第一批必须稳定的 E2E 场景

1. `TestE2ETUI_ScenarioLaunchIntoWorkingWorkspace`
2. `TestE2ETUI_ScenarioSplitAndContinueWorking`
3. `TestE2ETUI_ScenarioConnectTerminalInNewTab`
4. `TestE2ETUI_ScenarioConnectTerminalInFloatingPane`
5. `TestE2ETUI_ScenarioEditTerminalMetadata`
6. `TestE2ETUI_ScenarioTerminalManagerConnectsSelectedTerminalHere`
7. `TestE2ETUI_ScenarioSwitchWorkspace`
8. `TestE2ETUI_ScenarioLaunchFromLayoutFile`
9. `TestE2ETUI_ScenarioRestartProgramExitedTerminal`

### 5.2 第二批共享 terminal E2E

1. `TestE2ETUI_ScenarioSharedTerminalResizeRequiresExplicitAcquire`
2. `TestE2ETUI_ScenarioTabAutoAcquireResizeOnEnter`
3. `TestE2ETUI_ScenarioCloseSharedPaneKeepsTerminalAlive`
4. `TestE2ETUI_ScenarioKillSharedTerminalKeepsUnconnectedPaneSlots`
5. `TestE2ETUI_ScenarioSharedProgramExitedTerminalPropagatesToAllPanes`
6. `TestE2ETUI_ScenarioClosePaneDoesNotNotifyOtherClients`
7. `TestE2ETUI_ScenarioDetachDoesNotNotifyOtherClients`

### 5.3 第三批渲染与 real-program E2E

1. `python3` REPL
2. `vi` / `vim` alt-screen
3. `seq` 大输出
4. floating drag 恢复遮挡内容
5. overlay hide/show 无残影

---

## 6. 测试组织方式

### 6.1 场景优先

测试名优先描述用户目标，不优先描述内部实现。

### 6.2 一测一目标

- 一个测试只聚焦一个用户目标
- 复杂流程拆成多个场景

### 6.3 旧测试迁移策略

legacy 测试不做整包平移，按下面方式迁移：

1. 先提炼场景意图
2. 再按新结构重写 harness
3. 最后回迁断言

---

## 7. 质量门禁

### 7.1 本仓库统一验证命令

运行测试时使用仓库指定 Go 工具链：

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./... -count=1
```

### 7.2 TUI 改动最低门禁

任何 TUI 改动，至少需要：

1. 跑被修改包的定向 `go test`
2. 跑全量 `go test ./... -count=1`

### 7.3 文档要求

测试失败修复时，要同步更新：

- 场景名
- 验收口径
- 必要的主文档

---

## 8. benchmark 策略

建议至少保留下面基线 benchmark：

1. `BenchmarkRenderLargeOutput`
2. `BenchmarkRenderOverlayToggle`
3. `BenchmarkRenderFloatingDrag`
4. `BenchmarkRenderSharedAltScreen`
5. `BenchmarkReducerHighFrequencyEvents`

当 render、backpressure、cache 策略变化时，必须比较前后基线。

---

## 9. 完成标准

测试体系达到可持续状态，至少满足：

1. 纯逻辑有单测
2. 高风险交互有回归测试
3. 主路径有 E2E
4. 真实程序场景有样本测试
5. render 性能有 benchmark 基线
