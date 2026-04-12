# TUIv2 Phase 1.1 Inventory

## 范围

这份清单只盘点两类路径：

- `app` 内直接同时改 `workbench` 和 `runtime` 的事务路径
- 同一个 pane-terminal 事务被拆散在 `app` / `orchestrator` / `runtime` 多处完成的路径

不在这份清单里展开的内容：

- 纯 UI 状态
- 纯 render 投影
- 只读辅助函数

## 结论先行

当前最需要先收口的不是所有跨层调用，而是下面 6 条主事务：

1. 绑定已存在 terminal 到 pane
2. attach / create-and-attach / restart-and-attach
3. restore / reattach 失败补偿
4. floating pane direct close
5. session 文档导入后的 runtime reconcile
6. session lease acquire / release + resize

shared terminal owner / resize 的 flaky，不是独立问题，而是第 2、5、6 条事务边界不完整的症状。

## A. 主事务清单

### A1. Bind Existing Terminal To Pane

- 入口：
  - `splitPaneAndBindTerminalCmd`
  - `createTabAndBindTerminalCmd`
  - `createFloatingPaneAndBindTerminalCmd`
  - `bindTerminalSelectionCmd`
- 文件：
  - `tuiv2/app/update_runtime.go`
- 当前写入：
  - `workbench.BindPaneTerminal`
  - `workbench.FocusPane`
  - `workbench.SetPaneTitleByTerminalID`
  - `runtime.UnbindPane`
  - `runtime.Registry().GetOrCreate(...)` 后手工回填 `Name/Command/Tags/State/ExitCode/OwnerPaneID/BoundPaneIDs`
  - 额外 side state：`resetPaneScrollOffset`、`render.Invalidate`、`saveStateCmd`、必要时 `LoadSnapshotEffect`
- 问题：
  - 业务含义完整，但实现是手工双写加字段扇出。
  - owner/binding/metadata 的真相分散在 `workbench`、`runtime registry`、`runtime bindings` 三处。
  - exited terminal 额外补 snapshot load，说明同一事务的后置动作也散在 `app`。
- 建议归属：
  - 新的 pane-terminal transaction service
  - 候选方法：`BindExistingTerminal(tabID, paneID, pickerItem)`

### A2. Attach Existing Or Newly Created Terminal

- 入口：
  - `attachInitialTerminalCmd`
  - `attachPaneTerminalCmd`
  - `restartPaneTerminalCmd`
  - `submitCreateTerminal`
  - `splitPaneAndAttachTerminalCmd`
  - `createTabAndAttachTerminalCmd`
  - `createFloatingPaneAndAttachTerminalCmd`
- 文件：
  - `tuiv2/app/update_runtime.go`
  - `tuiv2/app/update_modal_prompt_submit_terminal.go`
  - `tuiv2/orchestrator/orchestrator_attach.go`
  - `tuiv2/orchestrator/orchestrator.go`
- 当前写入被拆成多段：
  - `Prepare*AttachTarget()` 先改 `workbench`
  - `submitCreateTerminal()` 先调 daemon create，再预热 `runtime.Registry()`
  - `AttachAndLoadSnapshot()` 在 `orchestrator` 里 attach runtime、回绑 `workbench` pane terminal、同步 pane title、启动 stream
  - `app` 自己维护 `pendingPaneAttaches`
  - `finalizeTerminalAttachCmd()` 再决定立刻 resize 还是挂起 `pendingPaneResizes`
  - attach 完成后是否 `saveState` 也还在 `app`
- 问题：
  - 同一个 attach 事务横跨 `app + orchestrator + runtime + pending maps`。
  - “workbench target 准备” 和 “runtime attach” 之间没有统一回滚/补偿边界。
  - create-terminal 场景还会提前写 `runtime.Registry()`，进一步放大分散写入。
- 建议归属：
  - 新的 pane-terminal transaction service，统一负责：
  - target prepare
  - daemon attach/create/restart
  - runtime/workbench 绑定落地
  - pending state
  - post-attach resize / stream / save-state
  - 候选方法：
  - `AttachTerminalToPane(...)`
  - `CreateAndAttachTerminal(...)`
  - `RestartAndReattachTerminal(...)`

### A3. Restore / Reattach With Failure Compensation

- 入口：
  - `reattachRestoredPanesCmd`
- 文件：
  - `tuiv2/app/update_runtime.go`
- 当前写入：
  - 复用 `attachPaneTerminalCmd`
  - attach 失败时，`app` 再手工 `workbench.BindPaneTerminal(tabID, paneID, "")`
- 问题：
  - 失败补偿还在 `app`。
  - attach transaction 的失败语义没有统一 owner，restore 场景只好自己兜底。
- 建议归属：
  - attach transaction service 的 restore/rehydrate 变体
  - 候选方法：`ReattachRestoredPane(...)`

### A4. Floating Pane Direct Close

- 入口：
  - `closeFloatingPaneDirect`
- 文件：
  - `tuiv2/app/floating_ui.go`
- 当前写入：
  - `workbench.ClosePane`
  - `runtime.UnbindPane`
  - 额外 side state：floating overview refresh、`render.Invalidate`、`saveStateCmd`
- 问题：
  - 这条路径绕开了 `orchestrator` 里已经存在的 pane close 流程。
  - pane close 的语义在 `app` 和 `orchestrator` 各有一份，后续很容易漂。
- 建议归属：
  - 统一复用 pane close transaction
  - 若 floating close 有额外 UI 行为，放在事务后处理，而不是自己重写关闭逻辑

### A5. Session Doc Import After Runtime Reconcile

- 入口：
  - `reconcileSessionRuntime`
  - 相邻的 session workbench export/import 流程
- 文件：
  - `tuiv2/app/session_sync.go`
- 当前写入：
  - `runtime.UnbindPane`
  - `runtime.AttachTerminal`
  - `runtime.StartStream`
  - 对应的 `workbench` 绑定变化来自 session 文档导入的其他步骤，不在同一个事务里
- 问题：
  - session sync 实际上是“先改 workbench 文档，再补 runtime 对齐”。
  - 这是一条拆裂事务，尤其容易留下 owner/connected/stream 状态错位。
- 建议归属：
  - 单独的 session reconcile service
  - 它应该拿 old/new binding diff，一次性做 workbench/runtime/session lease 对齐

### A6. Session Lease Acquire / Release And Resize

- 入口：
  - `acquireSessionLeaseAndResizeCmd`
  - `releaseSessionLeaseCmd`
  - 底层共用 `syncTerminalInteraction`
- 文件：
  - `tuiv2/app/session_sync.go`
  - `tuiv2/app/update_interaction_sync.go`
  - `tuiv2/app/update_actions_input.go`
- 当前写入：
  - `sessionLeases` map
  - `runtime.ApplySessionLeases`
  - `syncTerminalInteraction(...)` 触发 ownership / resize
  - 目标 pane / rect / terminal 仍然依赖 `workbench` 投影
- 问题：
  - 这条路径既改 session lease 状态，又通过 resize 影响 runtime live state，还依赖 workbench 布局。
  - 如果它继续留在 `app` 外围，新的 transaction service 仍然会被 owner/resize 时序绕开。
- 建议归属：
  - pane-terminal transaction service 的 session-aware 分支
  - 或者至少和 transaction service 共用同一个 ownership/lease/resize coordinator

## B. 次级跨层协调清单

这些路径不一定都要在第一刀完全收完，但它们和主事务强相关，Phase 1.2 设计 service 时必须一并考虑。

### B1. Ownership / Lease / Resize Coordination Cluster

- 入口：
  - `syncTerminalInteraction`
  - `prepareTerminalInput`
  - `ensurePaneTerminalSize`
  - `resizeVisiblePanes`
  - `syncZoomViewportCmd`
  - `syncActivePaneInteractiveOwnershipCmd`
  - `acquireSessionLeaseAndResizeCmd`
  - `ActionBecomeOwner`
- 文件：
  - `tuiv2/app/update_interaction_sync.go`
  - `tuiv2/app/update_actions_input.go`
  - `tuiv2/app/update_runtime.go`
  - `tuiv2/app/session_sync.go`
  - `tuiv2/app/update_actions_local.go`
- 当前行为：
  - 从 `workbench` 投影出 pane/rect
  - 决定是否 acquire session lease
  - 决定是否 acquire local ownership
  - 再决定是否 resize
- 问题：
  - 这组逻辑虽然主要写 `runtime`，但事务触发条件依赖 `workbench` 和 session state。
  - shared terminal owner/resize 的 flaky 就出在这里和 attach/restore 边界没完全收口。
- 建议：
  - Phase 1.2 要优先处理它，不要等主事务清理快结束时再回头补
  - 至少要和 attach transaction service 共享同一套 target resolution、ownership policy、lease policy、resize policy

### B2. Floating Auto-Fit Uses Runtime Snapshot To Mutate Workbench Layout

- 入口：
  - `fitFloatingPaneToContent`
  - `maybeAutoFitFloatingPanesCmd`
- 文件：
  - `tuiv2/app/floating_ui.go`
- 当前行为：
  - 读 `runtime` snapshot / surface
  - 改 `workbench` floating rect / fit mode / auto-fit size
  - 然后再触发 `resizePaneIfNeededCmd`
- 问题：
  - 这是另一条“读 runtime 决定 workbench，再反过来改 runtime size”的跨层闭环。
  - 不属于第一批 pane-terminal attach 事务，但架构上同样需要收口。
- 建议：
  - 第一轮先记账，不立即并入 attach service
  - 等主事务收口后，再决定是否抽成 floating layout coordinator

### B3. Terminal Metadata Edits Still Bypass Any Domain Service

- 入口：
  - `submitEditTerminalTagsPrompt`
  - `toggleTerminalSizeLockCmd`
- 文件：
  - `tuiv2/app/update_modal_prompt_submit_terminal.go`
  - `tuiv2/app/terminal_size_lock.go`
- 当前行为：
  - 直接调 daemon `SetMetadata`
  - 然后直接写 `runtime.SetTerminalMetadata`
  - 再落本地 state
- 问题：
  - 这不是 pane binding 事务，但依然是 metadata transaction，当前没有统一 owner。
- 建议：
  - 作为 Phase 1 后半段的次级整理项
  - 先不要抢在 pane-terminal 主事务之前处理

## C. 已经部分集中，但不要回退的路径

下面这些路径已经不该再回退到 `app` 手写双写：

- `orchestrator.handlePaneAction()` 里的：
  - `ActionClosePane`
  - `ActionDetachPane`
  - `ActionReconnectPane`
  - `ActionClosePaneKill`
- 相关文件：
  - `tuiv2/orchestrator/orchestrator_pane.go`

当前问题不是它们不存在，而是 `app` 里还残留着平行实现，比如 `closeFloatingPaneDirect`。

## D. Phase 1.2 建议切入顺序

按风险和回报排序，建议这样做：

1. `bindTerminalSelectionCmd`
2. `syncTerminalInteraction` 的 ownership/lease/resize policy 收口
3. `closeFloatingPaneDirect`
4. `attachPaneTerminalCmd` / `submitCreateTerminal` / `restartPaneTerminalCmd`
5. `reattachRestoredPanesCmd`
6. `reconcileSessionRuntime` + session lease/release

原因：

- 第 1 条是最明显的手工双写，改完收益立刻可见。
- 第 2 条必须前置，不然新的 transaction service 仍会被旧的 ownership/resize 路径绕开。
- 第 3 条能顺手消掉 `app` 与 `orchestrator` 的平行关闭路径。
- 第 4-6 条是一组，直接关联 shared terminal owner/resize flaky。
- session reconcile 仍然放最后，但不能漏掉 session lease/release 这一半事务。

## E. Phase 1.2 的验收口径

第一轮事务收口完成后，至少要满足：

- `app` 不再手工同时调用 `workbench.BindPaneTerminal` 和 `runtime.UnbindPane/Registry().GetOrCreate(...)`
- `attach/create/restart/restore/session-lease-resize` 共享同一个事务 owner
- `closeFloatingPaneDirect` 不再保留平行关闭逻辑
- shared terminal owner/resize 的 flaky 用例稳定通过
