# Shared Workspace Session Design

Status: Accepted
Date: 2026-04-04

## 1. Problem

`termx` 现在的模型是：

- Terminal 是服务端实体
- workspace / tab / pane / layout 都是客户端本地状态
- `workspace-state.json` 只是本地持久化快照，不是共享真相

这意味着多个 TUI 实例同时操作同一个 workspace 时，天然会出现以下问题：

- pane-terminal 绑定关系在不同实例之间不一致
- `take terminal ownership` 只认本进程内 runtime cache，容易出现“pane X is not bound to terminal Y”这类本地失步错误
- 多个实例对同一个 state file 写入时是 last-write-wins，不具备协作语义
- 屏幕尺寸、active tab、focus、viewport、floating geometry 都没有正式定义共享边界

这不是一个“同步没补齐”的小 bug，而是当前产品抽象和目标行为不一致。

## 2. Why tmux Can Do It

tmux 能支持多客户端同时连接并保持状态一致，不是因为它“同步做得更细”，而是因为它有一个服务端 canonical session/window/pane 模型。

termx 当前没有这个层。

如果继续坚持“workspace 完全是客户端私有状态”，那么产品只能保证：

- 多客户端共享同一个 terminal pool
- 不能保证多客户端共享同一个 workspace 现场

如果想要 tmux 式“多客户端连接同一个工作现场”，就必须引入共享的 workspace session 真相。

## 3. Core Decision

不要尝试把 `workspace-state.json` 变成同步协议。

正确方向是把产品拆成两个层次：

- `Terminal`
  - 继续保持 termx 服务端唯一核心实体
- `WorkspaceSession`
  - 一个可共享的结构化工作现场
- `WorkspaceView`
  - 某个客户端附着到 `WorkspaceSession` 后形成的本地观察与交互状态

也就是说：

- `WorkspaceSession` 是共享真相
- `WorkspaceView` 是每个客户端自己的显示/交互状态

## 4. Shared Vs Local State

### 4.1 必须共享的状态

这些状态属于 `WorkspaceSession`：

- workspace / tab / pane 的存在关系
- pane 树与 split ratio
- tab 顺序
- pane 是否关闭
- pane 绑定的 `terminal_id`
- 显式的 attach / detach / rebind / close-pane / close-pane-and-kill-terminal 结果
- floating pane 是否存在
- terminal pool 中和该 session 直接相关的引用关系

这些变化必须由一个共享 store 统一提交，并向所有 attached views 广播增量事件。

### 4.2 必须按客户端隔离的状态

这些状态属于 `WorkspaceView`，不应该默认全局同步：

- 当前看到的 active tab
- 当前 focus pane
- modal / picker / prompt / help
- 鼠标 hover / drag 中间态
- pane 内部 viewport 偏移
- zoom / inspect / temporary display state
- query 输入状态
- terminal manager 页面选择项

原因很简单：

- 这些状态带有强烈“我正在看什么”的语义
- 一个客户端切 tab / 滚动 / 打开帮助，不应该强制打断另一个客户端

### 4.3 浮窗 geometry 的建议

`floating pane` 的“存在”应属于共享结构状态，但 geometry 不应该在第一阶段全局强同步。

第一阶段建议：

- 共享：某个 pane 是 floating pane
- 本地：floating rect、z-order、当前是否展开到什么位置

否则不同屏幕尺寸下会立刻遇到不可解释行为：

- 一个客户端的大浮窗在另一个小屏上根本放不下
- 一个客户端拖到边缘的位置在另一个客户端没有相同语义

等共享 session 稳定后，再决定是否引入可选的“共享浮窗布局”策略。

## 5. Ownership Must Stop Being Pane-Only

当前 `owner / follower` 模型默认把 owner 绑定到 pane。

这在单 TUI 实例里基本成立，但在多客户端共享 session 时不够用了。原因是：

- PTY size 是 terminal 全局状态
- 同一个 pane 可以同时被多个客户端看见
- 不同客户端看见该 pane 的可见尺寸可能不同

所以多客户端语义下，真正需要被抢占的不是“pane 身份”，而是：

- `terminal control lease`

建议模型：

- `pane-terminal binding` 仍是共享结构状态
- `terminal control lease` 是 terminal 级 runtime 状态
- lease holder 是某个 `WorkspaceView` / `client attachment`
- 只有 lease holder 的可见 rect 可以驱动 PTY resize
- 其他 view 看到同一个 terminal 时一律按 follower 处理，只做裁切/滚动，不改 PTY size

因此 `Take Terminal Ownership` 的真实含义应改成：

- 当前 view 获取该 terminal 的 control lease

而不是：

- 把一个 pane 永久写成 owner

## 6. Screen Size Policy

这是多客户端设计里最容易失控的点，必须先定规则。

### 6.1 Terminal size

`Terminal.Size` 是全局的。

它不能随着每个客户端视口各自变化而同时成立，所以必须只有一个来源。

第一阶段推荐唯一正式策略：

- `lease-owner size`

规则：

- 持有 lease 的 view 负责 PTY resize
- 其他 view 只消费输出并本地裁切
- lease holder 断开后，不自动迁移 ownership
- terminal 保持最后一次 size，直到新的 view 显式接管

这和当前产品文档中“owner 关闭/解绑后不自动迁移”的方向一致，只是 owner 的单位从 pane 提升到了 view。

### 6.2 Layout size

共享的是布局关系，不是最终 cell rect。

建议：

- split ratio 共享
- 每个 view 用自己的 viewport 尺寸重新解算 pane rect

这和网页的响应式布局类似：

- 共享的是结构约束
- 每个客户端自行渲染最终几何结果

### 6.3 Viewport behavior

pane 小于 terminal 时：

- 默认从左上角裁切
- 每个 view 维护自己的 viewport offset

不要把 viewport offset 设为 session 级共享状态；它更像“我此刻在看哪里”，不是“大家都必须看这里”。

## 7. Two Collaboration Modes

产品上不应该只有一种“多客户端”。

建议明确拆成两个模式：

### 7.1 Independent View Mode

默认模式。

特点：

- 所有结构编辑同步
- 每个客户端保留自己的 active tab / focus / viewport / overlay
- 适合日常多端同时打开同一 workspace session

这是最稳的默认值。

### 7.2 Follow View Mode

显式进入的协作模式。

特点：

- 一个 view 可以跟随另一个 view 的 active tab / focus / viewport
- 适合教学、演示、远程协作
- 不承诺像素级完全相同

这样可以满足“像 tmux 一样一起看同一个现场”的需求，但不把所有客户端默认绑死在一起。

## 8. What To Do Right Now

在共享 session 还没实现前，当前产品必须明确一条限制：

- 多个 TUI 实例同时读写同一个 `workspace-state.json` 不受支持

否则用户会自然把它理解成“多客户端共享 workspace”，然后踩到现在这类失步问题。

短期建议：

1. 启动时为 workspace state path 做实例占用检测；发现多个实例时直接告警。
2. 错误文案改成 `not locally bound`，避免把本地 runtime cache 说成全局真相。
3. 不要尝试通过 watch 本地 JSON 文件来补共享同步。

第 3 条尤其重要，因为 file watch 只能得到“最后一次序列化结果”，不能提供可靠的意图合并、冲突解决和 runtime lease 语义。

## 9. Architecture Direction

推荐新增一个共享状态层，而不是继续把同步塞进现有 runtime cache：

### 9.1 Shared Session Store

职责：

- 持有 `WorkspaceSession` canonical document
- 支持 compare-and-swap / revision
- 广播结构变更事件
- 为 attached views 分配 `view_id`

### 9.2 Session Event Stream

事件至少包括：

- tab created / closed / reordered
- pane created / closed / moved
- pane attached / detached / rebound
- floating promoted / demoted
- session snapshot replaced
- control lease acquired / released

### 9.3 Per-View Ephemeral State

只存在于连接生命周期里：

- active tab
- focused pane
- viewport offset
- overlay state
- follow target
- window size

## 10. Current Product Contract

在 2026-04-05 这次收口后，termx 对多客户端 shared session 的产品定义是：

- 多个 TUI 可以 attach 同一个 `WorkspaceSession`
- workspace/tab/pane/layout/binding 的结构变化是共享的
- active tab / focus / viewport / modal / zoom / scroll 是 per-view 的
- session 结构同步由 push events 驱动，不使用周期性 polling
- event stream 断开后，client 应重连并在恢复后做一次 `session.get` 全量校准
- mixed-size 下共享的是 layout ratio，不是最终 cell rect
- PTY size 只能由当前控制方驱动；其他 view 只能本地裁切，不得改写 PTY size
- 控制方失效后不自动迁移；terminal 保持最后一次 size，直到新的 view 显式接管

这意味着 termx 当前默认的正式协作模式是：

- 共享结构
- 隔离观察
- 显式接管控制权

## 11. Phase Boundary

本次收口完成的是：

- shared session truth
- multi-client structural sync
- per-view local overlay preservation
- push-based session sync with reconnect resync

本次收口明确没有完成的是：

- daemon 真正发放和回收 terminal control lease
- follow mode
- shared floating geometry
- render model 与 view model 的彻底拆分

## 12. Runtime

`runtime` 继续负责：

- terminal attach / stream / snapshot / recovery
- terminal registry
- local render invalidation

但它不应该继续承担“共享 workspace 绑定真相”。

## 13. Suggested Delivery Plan

### Phase 0

先把当前产品说清楚并降低误导：

- 增加单实例占用检测
- 调整 ownership 错误文案
- 在文档中明确“当前只有共享 terminal pool，没有共享 workspace session”

### Phase 1

引入 `WorkspaceSession` store 和 revision/event 模型，但先只同步：

- tab / pane / split
- pane-terminal binding
- close / detach / reattach

暂时不做共享 floating geometry。

### Phase 2

引入 `view_id` 和 `control lease`：

- `Take Terminal Ownership` 改为抢占 terminal control lease
- resize 只接受 lease holder
- render 层按本 view 是否持有 lease 显示 owner/follower

### Phase 3

引入可选 `follow view mode`：

- 跟随 active tab
- 跟随 focus pane
- 跟随 viewport

把“强同步观看”做成显式模式，而不是默认副作用。

## 14. Product Definition Summary

一句话总结：

- 共享 terminal pool != 共享 workspace 现场
- 想要 tmux 式多客户端 workspace，就必须有 `WorkspaceSession` 这个共享真相层
- 屏幕大小、focus、viewport 这类状态不能一股脑全局同步，必须拆成 shared session state 和 per-view local state

如果不先做这个拆分，后面所有 ownership、resize、pane binding 的问题都会反复出现，只是换一种报错方式而已。
