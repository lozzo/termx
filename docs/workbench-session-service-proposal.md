# Workbench Session Service Proposal

Status: Accepted
Date: 2026-04-05

## 1. Decision

termx 采用这条路线：

- 不再把 TUI 的 workspace/tab/pane/layout 真相留在本地文件
- 不单独引入一个只服务 TUI 的旁路 daemon
- 在现有 termx daemon 内新增一个一等的 `Workbench Session Service`

这意味着 termx daemon 从“只有 Terminal Pool”升级为“双服务模型”：

- `Terminal Pool Service`
  - 继续负责 PTY、I/O、resize、snapshot、events
- `Workbench Session Service`
  - 负责共享的 session/workspace/tab/pane/layout/binding/view state

重点是：

- `Terminal` 仍然是核心运行实体
- 但 termx daemon 不再只有 `Terminal` 这一类公开可见对象

## 2. Why This Route

不选“单独 TUI 守护进程”的原因：

- 那会制造第二个 server 和第二套生命周期
- 后续 GUI/Web/Mobile 无法自然复用
- TUI 会变成架构特权客户端，而不是第一方客户端

不选“继续本地文件同步”的原因：

- 没有 canonical shared truth
- 没有 revision / CAS / merge 语义
- 无法正式承载多客户端 attach / detach / follow / lease

因此正式产品方向应该是：

- 一个 daemon
- 两个服务
- 一套 transport / protocol / auth / lifecycle

## 3. Daemon Entities

### 3.1 `Terminal`

保持不变：

- PTY 进程
- VTerm / snapshot
- stream / attach / input / resize
- metadata / tags / lifecycle

`Terminal` 不直接拥有 pane、tab、workspace。

### 3.2 `WorkbenchSession`

新增共享实体。

它是多客户端 attach 的共享工作现场，包含：

- session metadata
- workspace tree
- tab tree
- pane tree
- floating membership
- pane-terminal binding
- layout ratios
- revision

建议结构：

```go
type WorkbenchSession struct {
    ID          string
    Name        string
    CreatedAt   time.Time
    UpdatedAt   time.Time
    Revision    uint64
    Workbench   WorkbenchDoc
}

type WorkbenchDoc struct {
    Workspaces      []WorkspaceDoc
    WorkspaceOrder  []string
}
```

### 3.3 `SessionView`

新增临时实体。

它表示某个客户端附着到某个 session 后的本地视图状态。

它不是共享布局真相，而是 per-client ephemeral state：

- `view_id`
- `client_id`
- `session_id`
- active workspace
- active tab
- focused pane
- viewport offsets
- window size
- follow target
- mode / overlay summary

建议结构：

```go
type SessionView struct {
    ViewID              string
    ClientID            string
    SessionID           string
    ActiveWorkspaceName string
    ActiveTabID         string
    FocusedPaneID       string
    WindowCols          uint16
    WindowRows          uint16
    FollowTargetViewID  string
    ViewportOffsets     map[string]PaneViewport
    AttachedAt          time.Time
    UpdatedAt           time.Time
}
```

### 3.4 `TerminalControlLease`

新增 runtime 实体。

这是一个 terminal 级的运行时控制权，不是结构树的一部分。

它用于解决 mixed-size 和 owner/follower 问题：

- 谁可以驱动 PTY resize
- 谁在 UI 中显示 owner
- lease 丢失时如何退化

建议结构：

```go
type TerminalControlLease struct {
    TerminalID string
    SessionID  string
    ViewID     string
    PaneID     string
    AcquiredAt time.Time
}
```

## 4. Shared Vs Local Truth

### 4.1 Session-level shared truth

以下状态属于 `WorkbenchSession`：

- workspace / tab / pane 是否存在
- workspace/tab 顺序
- tiled layout tree
- split ratio
- pane 与 terminal 的绑定关系
- floating pane 的存在性
- close / detach / rebind / move / split / create / delete 的结果

这些状态必须通过 session revision 提交。

### 4.2 View-level local truth

以下状态属于 `SessionView`：

- 当前 active workspace
- 当前 active tab
- 当前 focused pane
- pane viewport offset
- modal / prompt / picker / help
- hover / drag / temporary selection
- follow source
- 当前窗口尺寸

这些状态默认不广播成结构变更，不参与 session revision 冲突。

### 4.3 Deliberate non-shared state

第一阶段明确不做 session 级共享：

- floating rect
- floating z-order
- viewport offset
- input mode / modal stack

原因：

- 这些状态强依赖客户端尺寸
- 在多客户端 mixed-size 下没有稳定语义
- 先共享结构，后共享显示，风险更低

## 5. Behavior Rules

### 5.1 Default collaboration mode

默认是 `independent-view mode`：

- 所有结构编辑同步
- 每个 view 有自己的 active tab / focus / viewport
- 一个 view 的帮助层、滚动位置、焦点变化不会打断另一个 view

### 5.2 Optional follow mode

`follow mode` 是显式开启的 view-level 行为。

规则：

- 一个 view 可以设置 `FollowTargetViewID`
- 跟随目标 view 的 active workspace / active tab / focused pane / viewport offset
- 不承诺像素级一致
- 只影响跟随 view，不反向写回结构真相

### 5.3 Structural edit impact

如果某个结构编辑影响到别的 view 当前目标：

- active pane 被删除：
  - 受影响 view 自动 rebased 到最近可聚焦 pane
- active tab 被删除：
  - 受影响 view 自动 rebased 到同 workspace 最近 tab
- active workspace 被删除：
  - 受影响 view 自动 rebased 到最近 workspace

这种 rebasing 通过 `view_rebased` 事件通知客户端。

## 6. Resize And Ownership Policy

这部分必须定死，不允许模糊。

### 6.1 Chosen policy

选择：

- `view-scoped terminal control lease`

不选择：

- 所有客户端最小尺寸驱动 PTY

原因：

- termx 不是纯 session-first 复用器，terminal pool 仍是一等对象
- 同一个 terminal 可以被多个 pane 甚至多个 workspace 复用
- 当前产品已经有 owner/follower 语义，`follower` 不应改写 PTY size

### 6.2 Exact rule

规则如下：

- 一个 terminal 同时最多一个 control lease holder
- 只有 lease holder 对应的 view 可以触发 PTY resize
- 其他 view 看到该 terminal 时一律按 follower 渲染
- follower 的几何变化只改本地裁切，不改 PTY size
- lease holder 断开、解绑、pane 删除后，lease 释放
- lease 释放后不自动迁移
- terminal 保持最后一次 size，直到新的 view 显式获取 lease

### 6.3 Input rule

lease 只控制几何，不控制输入权限。

第一阶段保持：

- 任意 live pane 都可以发送输入
- lease 只影响 resize / owner badge / ownership actions

这样可以保留 termx 原本的多 collaborator 输入语义。

### 6.4 First attach behavior

当某个 pane 首次绑定一个 terminal 且当前没有 lease 时：

- 触发该绑定动作的 view 自动获得 lease

当另一个 view 想接管时：

- 必须显式调用 `AcquireTerminalLease`

## 7. Mixed-size Policy

### 7.1 Layout geometry

共享的是 layout structure，不是最终 cell rect。

规则：

- session 保存 split ratio
- 每个 view 用自己的 `WindowCols/WindowRows` 解算可见 pane rect

### 7.2 PTY size

`Terminal.Size` 是 terminal-global state。

它只接受 lease holder 的可见 pane rect 计算结果。

即：

- session shared
- render rect per-view
- PTY size single-source

### 7.3 Hidden / tiny pane behavior

如果 lease holder 的 pane：

- 被切到不可见 tab
- 被隐藏
- 被缩到极小

则第一阶段不自动切 lease。

行为：

- terminal 继续保持 lease holder 最近一次提交的 size
- 其他 view 若要修正，必须显式接管 lease

这是刻意选择的强规则，优先保证语义稳定。

## 8. Go API Shape

不建议在 `Server` 上散落大量平铺方法。

建议新增一个 service handle：

```go
type Server struct {
    // Terminal Pool
}

func (s *Server) Workbench() WorkbenchService

type WorkbenchService interface {
    CreateSession(ctx context.Context, opts CreateSessionOptions) (*SessionInfo, error)
    ListSessions(ctx context.Context, opts ...ListSessionOptions) ([]*SessionInfo, error)
    GetSession(ctx context.Context, id string) (*SessionSnapshot, error)
    DeleteSession(ctx context.Context, id string) error

    AttachSession(ctx context.Context, id string, opts AttachSessionOptions) (*AttachSessionResult, error)
    DetachSession(ctx context.Context, sessionID, viewID string) error

    Apply(ctx context.Context, sessionID string, req ApplyRequest) (*ApplyResult, error)
    UpdateView(ctx context.Context, sessionID, viewID string, req UpdateViewRequest) (*ViewInfo, error)

    AcquireTerminalLease(ctx context.Context, sessionID, viewID, paneID, terminalID string) (*LeaseInfo, error)
    ReleaseTerminalLease(ctx context.Context, sessionID, viewID, terminalID string) error

    Events(ctx context.Context, opts ...WorkbenchEventOption) <-chan WorkbenchEvent
}
```

### 8.1 Why `Apply`

结构编辑统一走：

- `Apply(session_id, base_revision, ops...)`

而不是暴露几十个只给第一方 TUI 用的零散 RPC。

原因：

- 需要 revision / CAS
- 需要冲突处理
- 需要把多步编辑压成一个原子提交

## 9. Protocol Shape

沿用现有控制通道 JSON RPC，不新增第二套 transport。

建议新增 method namespace：

- `session.create`
- `session.list`
- `session.get`
- `session.delete`
- `session.attach`
- `session.detach`
- `session.apply`
- `session.view_update`
- `session.acquire_lease`
- `session.release_lease`
- `session.events`

### 9.1 `session.attach`

请求：

```json
{
  "id": 7,
  "method": "session.attach",
  "params": {
    "session_id": "ws-main",
    "view": {
      "window_cols": 184,
      "window_rows": 52,
      "client_name": "termx-tui/0.2.0"
    }
  }
}
```

响应：

```json
{
  "id": 7,
  "result": {
    "session_id": "ws-main",
    "view_id": "view-18",
    "revision": 41,
    "snapshot": {
      "workbench": {},
      "view": {},
      "leases": []
    }
  }
}
```

### 9.2 `session.apply`

请求：

```json
{
  "id": 8,
  "method": "session.apply",
  "params": {
    "session_id": "ws-main",
    "view_id": "view-18",
    "base_revision": 41,
    "ops": [
      {"op": "split-pane", "pane_id": "12", "direction": "vertical"},
      {"op": "bind-terminal", "pane_id": "44", "terminal_id": "a7k2m9x1"}
    ]
  }
}
```

响应：

```json
{
  "id": 8,
  "result": {
    "applied_revision": 42,
    "snapshot": null
  }
}
```

冲突时：

```json
{
  "id": 8,
  "error": {
    "code": 409,
    "message": "session revision conflict",
    "data": {
      "expected_revision": 41,
      "actual_revision": 42
    }
  }
}
```

### 9.3 `session.view_update`

只更新 per-view 状态，不 bump session structural revision。

请求：

```json
{
  "id": 9,
  "method": "session.view_update",
  "params": {
    "session_id": "ws-main",
    "view_id": "view-18",
    "view": {
      "active_tab_id": "tab-2",
      "focused_pane_id": "44",
      "window_cols": 184,
      "window_rows": 52
    }
  }
}
```

### 9.4 Events

建议新增 event family：

- `session.created`
- `session.deleted`
- `session.updated`
- `session.view_updated`
- `session.view_detached`
- `session.view_rebased`
- `session.lease_changed`

事件语义保持和 terminal events 一样：

- best-effort
- 通知而不是真相
- 客户端可用 `session.get` 重建

## 10. Snapshot Contract

`session.get` 和 `session.attach` 返回的 snapshot 建议统一：

```json
{
  "session": {
    "id": "ws-main",
    "name": "main",
    "revision": 42,
    "workbench": {}
  },
  "view": {
    "view_id": "view-18",
    "active_workspace_name": "main",
    "active_tab_id": "tab-2",
    "focused_pane_id": "44",
    "window_cols": 184,
    "window_rows": 52
  },
  "leases": [
    {
      "terminal_id": "a7k2m9x1",
      "view_id": "view-18",
      "pane_id": "44"
    }
  ]
}
```

## 11. Operation Set

第一阶段 `session.apply` 只需要有限 op 集，不要设计成通用 CRDT。

建议首批：

- `create-workspace`
- `delete-workspace`
- `rename-workspace`
- `create-tab`
- `delete-tab`
- `rename-tab`
- `move-tab`
- `create-pane`
- `delete-pane`
- `split-pane`
- `move-pane`
- `bind-terminal`
- `detach-terminal`
- `replace-terminal`
- `promote-floating`
- `demote-floating`
- `set-layout-ratio`

### 11.1 Atomicity

一次 `Apply` 中的所有 ops：

- 要么全部成功
- 要么全部失败

这对 TUI 很重要，因为很多交互是复合动作。

## 12. Migration Plan

### Phase 1

落地 `Workbench Session Service` 最小骨架：

- create/list/get/delete session
- attach/detach session
- session snapshot
- apply with revision
- per-view local state

先不接管所有旧 TUI 路径。

### Phase 2

把 TUI 从本地 `workspace-state.json` 切到 daemon session：

- startup attach session
- local save/restore 只保留为 export/import
- structural actions 改走 `session.apply`
- local focus/viewport 改走 `session.view_update`

### Phase 3

接入 terminal leases：

- owner/follower 从 pane-local runtime cache 升级为 daemon truth
- resize 只接受 lease holder
- takeover action 改为 `session.acquire_lease`

### Phase 4

补 follow mode：

- follow target view
- rebase handling
- session view events

## 13. Explicit Non-goals

第一阶段不做：

- session-level floating rect synchronization
- generic distributed merge / CRDT
- offline-first session editing
- fully mirrored input/mode stacks
- external auth / ACL model

## 14. Final Product Position

termx daemon 的正式定位应更新为：

- 底层是 `Terminal Pool`
- 上层是可选的 `Workbench Session`
- TUI、GUI、Web 都可以 attach 到同一个 session
- session 共享结构真相
- view 保留本地观察状态

这条路线既保住 termx 的 terminal-pool 特性，也让它在体验上具备真正的终端复用器能力。
