# Workbench Session Phase 1 Design

Status: Implemented
Date: 2026-04-05

## 1. Goal

Phase 1 的目标不是一次把完整“终端复用器能力”做完，而是先把共享真相立起来。

完成后必须满足：

- termx daemon 能创建、持久化、恢复 `WorkbenchSession`
- 多个 TUI 可以 attach 同一个 session
- 结构性编辑走 daemon，而不是本地 `workspace-state.json`
- 每个 client 的 active tab / focus / viewport 仍保留为 per-view 状态
- TUI 能在不重做全部 UI 的前提下切到新服务

Phase 1 明确不要求：

- 完整 follow mode
- 终端 lease 驱动的 resize 接管
- floating rect 跨客户端同步

## 1.1 Close-out

2026-04-05 的 Phase 1 收口标准定义为：

- daemon 成为 session/workspace/tab/pane/layout/binding 的共享真相
- TUI startup 通过 `session.attach` 恢复现场，不再把本地 state file 当共享真相
- 多客户端结构编辑通过 `session.replace` / `session.apply` 同步
- active tab / focus / viewport 保持 per-view，本地叠加回放到 shared snapshot 之上
- session 更新改为 push event 驱动，不再依赖定时 polling
- event stream 断开后重连，并在重连成功后做一次 `session.get` 校准

Phase 1 明确仍然没有完成：

- daemon-backed terminal control lease
- mixed-size 下真正的 lease-driven resize authority
- 完整的 render model / view model 拆分

## 2. Implementation Strategy

选择：

- daemon 内内存真相 + 本地磁盘快照持久化

不选择：

- 外部数据库
- CRDT
- 分布式多主同步

理由：

- 当前 termx 是单机 daemon
- session 共享只需要 daemon 级 canonical truth
- 先把产品语义跑通，比先上复杂存储更重要

## 3. New Packages

建议新增以下 root-level 包，避免 daemon 依赖 `tuiv2`：

### 3.1 `workbenchdoc`

职责：

- 纯数据结构
- 不含 TUI/UI/runtime 语义
- 作为 session snapshot 与 apply 的核心文档类型

建议来源：

- 参考当前 [types.go](../../tuiv2/workbench/types.go)
- 但迁到 root-level 公共包

核心结构：

```go
type Doc struct {
    CurrentWorkspace string
    WorkspaceOrder   []string
    Workspaces       map[string]*Workspace
}

type Workspace struct {
    Name string
    Tabs []*Tab
}

type Tab struct {
    ID              string
    Name            string
    Root            *LayoutNode
    Panes           map[string]*Pane
    Floating        map[string]*FloatingPane
    FloatingVisible bool
}

type Pane struct {
    ID         string
    Title      string
    TerminalID string
}
```

注意：

- `CurrentWorkspace` 在 Phase 1 仍保留在 shared doc 中，只作为默认入口
- 但 TUI attach 后会立即用自己的 `SessionView.ActiveWorkspaceName` 覆盖

### 3.2 `workbenchops`

职责：

- 定义 `ApplyRequest`
- 定义 op schema
- 在 doc 上执行原子变更

### 3.3 `workbenchsvc`

职责：

- session store
- view store
- lease store
- session event bus
- persistence

## 4. Internal Data Structures

### 4.1 Service root

```go
type WorkbenchService struct {
    mu       sync.RWMutex
    sessions map[string]*SessionState
    views    map[string]*ViewState
    events   *WorkbenchEventBus
    persist  Persistence
    ids      IDSource
    clock    Clock
}
```

### 4.2 Session state

```go
type SessionState struct {
    Meta    SessionMeta
    Doc     *workbenchdoc.Doc
    Revision uint64

    mu sync.RWMutex
}

type SessionMeta struct {
    ID        string
    Name      string
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

### 4.3 View state

```go
type ViewState struct {
    ViewID              string
    SessionID           string
    ClientID            string
    ActiveWorkspaceName string
    ActiveTabID         string
    FocusedPaneID       string
    Viewports           map[string]PaneViewport
    WindowCols          uint16
    WindowRows          uint16
    FollowTargetViewID  string
    AttachedAt          time.Time
    UpdatedAt           time.Time
}
```

### 4.4 Lease state

Phase 1 先把 store 立起来，但只提供 attach/create 时自动分配和 disconnect 时释放。

```go
type LeaseState struct {
    TerminalID string
    SessionID  string
    ViewID     string
    PaneID     string
    AcquiredAt time.Time
}
```

## 5. Persistence

### 5.1 What is persisted

持久化：

- session meta
- session revision
- workbench doc

不持久化：

- views
- follow target
- leases

原因：

- views 是连接期状态
- leases 是 runtime ownership，不应跨 daemon 重启隐式恢复

### 5.2 File layout

建议路径：

- `$XDG_STATE_HOME/termx/workbench/sessions/<session-id>.json`
- 无 `XDG_STATE_HOME` 时回退到现有 state 目录策略

文件结构：

```json
{
  "version": 1,
  "session": {
    "id": "main",
    "name": "main",
    "created_at": "...",
    "updated_at": "...",
    "revision": 12
  },
  "workbench": {}
}
```

### 5.3 Write strategy

采用：

- 每次成功 `Apply` 后全量覆写单 session 文件
- 写入临时文件后原子 rename

不做：

- append-only WAL

原因：

- Phase 1 写流量很低
- 全量 JSON 更容易调试和回滚

## 6. Session Lifecycle

### 6.1 Create

`CreateSession`：

- 创建空 session
- 自动写入一个默认 workspace `main`
- 自动写入一个默认 tab `1`
- 自动写入一个默认 pane `1`
- revision 从 `1` 开始

这和当前 TUI startup 行为一致，迁移成本最低。

### 6.2 Attach

`AttachSession`：

- 校验 session 存在
- 分配 `view_id`
- 创建 `ViewState`
- 若调用方未指定默认焦点：
  - active workspace = session doc current workspace
  - active tab = workspace 第一个 tab
  - focused pane = tab active pane / fallback pane
- 返回：
  - session snapshot
  - view snapshot
  - current leases

### 6.3 Detach

`DetachSession`：

- 删除 `ViewState`
- 释放该 view 持有的 leases
- 广播 `session.view_detached`

### 6.4 Restore on daemon startup

daemon 启动时：

- 扫描 session 目录
- 读取每个 session 文件
- 恢复到内存 `sessions map`
- `views` 和 `leases` 为空

## 7. Apply Model

### 7.1 Request

```go
type ApplyRequest struct {
    ViewID        string
    BaseRevision  uint64
    Ops           []Op
}
```

### 7.2 Execution

执行流程：

1. 读取 session 当前 revision
2. 不匹配则返回 `409 conflict`
3. 深拷贝 doc
4. 在副本上顺序执行所有 ops
5. 全部成功后替换原 doc
6. revision++
7. 持久化
8. 广播 `session.updated`

### 7.3 Atomicity

一组 ops 必须原子生效。

这意味着：

- 一个 split + bind-terminal 可以作为一次提交
- 一个 close-pane + fallback-focus rebase 可以由服务端一次完成

## 8. Phase 1 Op Set

Phase 1 不求全，只覆盖 TUI 最常用结构路径。

### 8.1 Workspace ops

- `create-workspace`
- `rename-workspace`
- `delete-workspace`

不包括：

- workspace reorder

### 8.2 Tab ops

- `create-tab`
- `rename-tab`
- `delete-tab`

不包括：

- tab reorder

### 8.3 Pane ops

- `create-first-pane`
- `split-pane`
- `close-pane`
- `focus-pane`

说明：

- `focus-pane` 在长期应属于 view-level
- 但 Phase 1 为了兼容现有 `workbench` 结构，可以允许 doc 内 tab 的 `ActivePaneID` 继续存在
- 真正 attach 后的焦点仍以 `ViewState.FocusedPaneID` 为准

### 8.4 Binding ops

- `bind-terminal`
- `detach-terminal`
- `replace-terminal`

这组 op 是 Phase 1 最重要的一组，因为它直接替代当前 TUI 本地绑定真相。

### 8.5 Floating ops

只支持：

- `promote-floating`
- `demote-floating`
- `toggle-floating-visibility`

不支持：

- `move-floating`
- `resize-floating`
- `raise-floating`

因为这些都依赖 per-view geometry。

## 9. Op Mapping To Existing TUI Code

为了降低迁移成本，Phase 1 的 server 端实现应尽量复用当前 TUI 的结构操作语义。

可直接参考：

- [mutate.go](../../tuiv2/workbench/mutate.go)

迁移原则：

- 先把 `tuiv2/workbench` 里的纯结构操作迁到 `workbenchops`
- 不把 TUI runtime/render 依赖搬进 daemon

## 10. Event Model

### 10.1 Event types

Phase 1 只需要这些：

- `session.created`
- `session.deleted`
- `session.updated`
- `session.view_attached`
- `session.view_detached`
- `session.view_updated`
- `session.lease_changed`

### 10.2 Event payload shape

推荐 payload 一律包含：

- `session_id`
- `revision` if structural
- `view_id` if view-scoped
- `kind`
- `summary`

`session.updated` 示例：

```json
{
  "type": "session.updated",
  "session_id": "main",
  "revision": 13,
  "summary": {
    "ops": ["split-pane", "bind-terminal"]
  }
}
```

### 10.3 Best-effort semantics

和 terminal events 一样：

- best-effort
- 可以丢
- 客户端若怀疑漏事件，调用 `session.get`

## 11. Server Integration

### 11.1 `Server` shape

在 [termx.go](../termx.go) 的 `Server` 中新增：

```go
type Server struct {
    // existing terminal fields
    workbench *workbenchsvc.Service
}
```

`NewServer()` 时初始化。

### 11.2 Request routing

在现有 `handleRequest()` 中增加 namespace dispatch：

```go
switch {
case strings.HasPrefix(req.Method, "session."):
    return s.handleSessionRequest(...)
default:
    return s.handleTerminalRequest(...)
}
```

不要把 terminal 和 session methods 混成一个巨大 `switch`。

### 11.3 Client identity

现有 transport session 已经有 `remote string`。

Phase 1 先用：

- `client_id = remote`

如果后续需要区分同一 remote 的多个 logical client，再扩展 hello capability。

## 12. Protocol Additions

### 12.1 `protocol/messages.go`

新增：

- `CreateSessionParams`
- `SessionInfo`
- `SessionSnapshot`
- `AttachSessionParams`
- `AttachSessionResult`
- `ApplyParams`
- `ApplyResult`
- `UpdateViewParams`
- `AcquireLeaseParams`
- `WorkbenchEvent`

### 12.2 No new frame types

Phase 1 不新增新的二进制帧类型。

原因：

- session service 完全属于控制面
- 现有 control channel JSON RPC 足够

## 13. TUI Migration

### 13.1 Phase 1 TUI target

TUI 不需要一次性重构。

第一步只做：

- startup 不再读本地 `workspace-state.json` 作为真相
- 改为：
  - `session.attach`
  - 把返回 snapshot hydrate 到本地 `workbench`
- 结构编辑后不再 `saveStateCmd()`
  - 改为 `session.apply`
- active tab / focus / window size 变化
  - 改为 `session.view_update`

### 13.2 Keep local workbench model

Phase 1 TUI 仍保留本地 `workbench.Workbench` 作为 render model。

流程：

- daemon snapshot -> hydrate local workbench
- local action -> 发 `session.apply`
- 收到成功响应 / session.updated -> refresh local workbench

这样能最大化复用现有渲染与编排代码。

当前实现额外约束：

- session 更新通过事件流触发 `session.get`
- 不保留周期性拉取 fallback
- reconnect 后立即做一次快照校准，避免错过断线期间的结构变化

### 13.3 State file downgrade

`workspace-state.json` 在 Phase 1 降级为：

- import/export 格式
- 非共享离线备份

不再作为日常运行真相。

## 14. Phase 1 Testing

必须新增三层测试：

### 14.1 Service unit tests

- create/list/get/delete session
- attach/detach view
- apply with revision success/conflict
- bind/detach terminal ops
- persistence restore

### 14.2 Protocol contract tests

- `session.create`
- `session.attach`
- `session.apply`
- `session.view_update`
- conflict payload correctness

### 14.3 TUI integration tests

- startup attach existing session
- split pane through `session.apply`
- attach existing terminal through `session.apply`
- second client attach same session and observe structural change
- second client reconnect after disconnect and continue receiving later session updates
- close-pane / close-pane-and-kill-terminal persists back into shared session

## 15. Deferred To Phase 2

以下能力明确延期：

- `AcquireTerminalLease` 真正接入 runtime resize
- follow mode
- shared floating geometry
- session ACL / permissions
- tab reorder / workspace reorder
- optimistic incremental patch events

## 16. Exact Next Implementation Order

建议按这个顺序写代码：

1. 新建 `workbenchdoc`
2. 新建 `workbenchops`，迁移最小结构变更逻辑
3. 新建 `workbenchsvc`，先做内存 store + JSON persistence
4. 在 `Server` 上挂 `workbenchsvc`
5. 在 `protocol/messages.go` 加 session schema
6. 在 `handleRequest()` 里新增 `session.*` routing
7. 写 protocol contract tests
8. TUI startup 改为 `session.attach`
9. TUI 结构编辑改为 `session.apply`
10. 本地 `workspace-state.json` 改成 import/export

这套顺序的目的是：

- 先把 daemon 真相立住
- 再让 TUI 切过去
- 避免又出现“双写真相”长期共存
