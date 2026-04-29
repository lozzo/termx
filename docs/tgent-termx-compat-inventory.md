# termx 对接 tgent 兼容性盘点

状态：进行中

本文档记录 `termx` 对接 `tgent` 现有产品壳和基础设施时，当前已识别出的结构耦合点、可直接复用点、以及必须改造点。

## 1. 目标

这份盘点不是实现文档，而是为后续重构切片回答三个问题：

1. 哪些 `tgent` 能力可以直接保留
2. 哪些地方只是“换后端数据源”
3. 哪些地方必须从 `session/tmux` 语义改成 `terminal/termx` 语义

## 2. 当前 termx 仓库的结构压力点

### 2.1 根目录 `package termx` 过厚

当前根目录同时承载：

- 对外 API
- terminal server
- transport session loop
- attach / bootstrap / stream
- `session.*` RPC 处理

主要信号：

- `termx.go`
- `terminal.go`
- `snapshot.go`
- `attachment_stream.go`
- `stream_screen_state.go`
- `event.go`

结论：

- 如果不先收口，后续 `tgent` 对接逻辑会自然继续堆进根目录

### 2.2 `session.*` 和 workbench 已经耦进 server 主路径

当前 `termx.go` 里直接处理：

- terminal 请求
- `session.*` 请求
- `workbenchsvc` 接入

结论：

- `termx core`
- `local workbench`
- `remote terminal path`

这三条线必须先拆边界，否则后面越接越乱。

### 2.3 `tuiv2/runtime` 其实已经不只是 TUI 私有逻辑

当前最值得警惕的是：

- `tuiv2/runtime`
- `tuiv2/bridge`

它们已经承担了：

- attach / bootstrap
- 本地 terminal surface runtime
- stream 消费
- ownership / lease 应用

结论：

- 这层应逐步提升为 shell-neutral client runtime
- 不能继续挂在 `tuiv2/` 名下当作 TUI 私有包

### 2.4 session 真相重复

当前至少存在两层结构真相：

- daemon/workbench 侧：`workbenchdoc` / `workbenchsvc`
- TUI 侧：`tuiv2/workbench`

结论：

- 长期只能保留一份共享结构真相
- shell 层只能保留自己的 UI/viewstate

### 2.5 shell/product 逻辑已开始落入 `cmd/`

当前 `cmd/termx/web.go` 已经是 shell/product 逻辑，而不只是 CLI glue。

结论：

- `cmd/` 只能做入口与 wiring
- shell/product 逻辑必须迁回 shell 层

## 3. tgent-web 侧已识别情况

### 3.1 可以保留的部分

目前看来，这些能力与 tmux 本身没有强绑定，可以优先保留：

- 账号密码登录
- dashboard
- pricing / billing / subscription
- hub discover
- connect ticket 基础路径
- heartbeat / online state / inventory

### 3.2 明显带 tmux 产品表述的部分

目前已看到：

- 官网/营销文案直接写 `tmux`
  - `src/app/page.tsx`
  - `src/app/layout.tsx`

结论：

- 这些属于产品文案改造，不影响第一阶段技术打通

### 3.3 会被 termx 接入影响的数据与 API

目前已看到的压力点：

- `sessionBytesIn / sessionBytesOut / sessionStartedAt`
  - `src/lib/schema.ts`
  - `src/lib/queries.ts`
- hub agent report 里有关闭 session 的流量记录
  - `src/app/api/internal/hubs/agents/report/route.ts`
- `relay_traffic` 和 `agents` 这两张核心表，本身就是按 session 模型建的
  - `drizzle/0017_relay-traffic-session-model.sql`
  - `src/lib/schema.ts`
- landing / marketing 文案直接以 tmux 为产品定义
  - `src/app/page.tsx`

结论：

- 这里的 “session” 更像 relay/client 会话统计，不一定等于 tmux session
- 这部分需要区分：
  - 哪些是网络/relay session
  - 哪些是 tmux/session 产品语义
- 其中有一部分已经不是“文案问题”，而是 schema 级建模问题

### 3.4 schema / telemetry 级别的硬耦合

目前最重要的新发现：

- `relay_traffic` 是按 `session_id / session_type / connected_at / disconnected_at / duration` 建模的
- `agents` 表上还挂着单个在线 session 的 `sessionBytesIn/Out` 和 `sessionStartedAt`
- `/api/internal/hubs/agents/report` 的 wire contract 直接要求上传 `sessions[]`
- `/api/internal/hubs/heartbeat` 会把 agent 行上的 `sessionBytesIn/Out` 直接覆盖成“当前在线会话”

这说明：

- `tgent-web` 现在不只是“页面上有 sessions”
- 而是**控制面 schema 和内部 hub reporting API 就把 session 当成核心统计对象**

对 `termx` 的直接影响：

- 如果一个设备将来能同时暴露多个 terminals
- 或一个 terminal 有多个 attachments
- 那么这里不能只做字段改名，必须重新定义统计模型

当前判断：

- 这属于“必须设计的后端建模改造”
- 不是单纯的前端页面改词

## 4. tgent-app 侧已识别情况

### 4.1 可以优先保留的部分

大方向上可以保留：

- 登录壳
- Dashboard / Settings / Welcome 等产品壳
- terminal 连接页整体壳
- WebRTC / xterm.js 技术路线

### 4.2 必须重点盘点的部分

需要重点确认：

- 设备进入后的 session 列表页
- terminal 页的数据加载入口
- 本地状态里哪些字段直接绑定 tmux session / pane / window 语义

当前盘点结论：

- app 层大概率不是整壳重写
- 重点改的是“session 入口 -> terminal 入口”

已识别出的明确耦合点：

- `src/pages/Dashboard.tsx`
  - Dashboard tab 明确有 `sessions`
  - 主数据来源是 `useAgentSessions(...)`
- `src/state/AgentDataStore.ts`
  - 核心状态直接保存 `_sessions`
  - 状态节点带 `session_id / window_id / pane_id`
  - 各种刷新逻辑直接围绕 session/window/pane 写
- `src/pages/TerminalPage.tsx`
  - 事件处理直接监听 `session_closed / window_closed`
- `src/pages/WebRTCTestPage.tsx`
  - 直接请求 `/sessions`
- `src/pages/WelcomePage.tsx`
  - 文案仍直接写 tmux

结论：

- `Dashboard.tsx`
- `AgentDataStore.ts`
- `TerminalPage.tsx`

这三块会是 app 侧最先要改的热点。

## 5. tgent-go / hub / agent 侧已识别情况

### 5.1 可直接复用的远程基础设施

优先保留：

- hub heartbeat
- online/offline agent report
- TURN / relay
- signaling offer / answer
- hub discover

这些部分更多是远程基础设施，不必因为替换 tmux 就推倒重来。

### 5.2 明显强绑定 tmux/session 的设备端能力

已识别出的重耦合区域包括：

- `internal/daemon/daemon.go`
- `internal/daemon/pipemanager.go`
- `internal/daemon/window_resize_coordinator.go`
- `internal/daemon/pane_resizer.go`
- `internal/tmux/*`

这些能力直接绑定：

- tmux session
- tmux window
- tmux pane
- pipe-pane capture
- tmux layout/resize 协调

结论：

- 这是后续被 `termx daemon` 原生 runtime 替换的核心区域
- 尤其 `internal/daemon/daemon.go` 暴露出来的公开动作几乎全是 tmux 资源动作：
  - `CreateSession`
  - `KillSession`
  - `CreateWindow`
  - `KillWindow`
  - `MoveWindow`
  - `JoinPane`
  - `SplitPane`
  - `KillPane`
  - `CapturePaneContent`
  - `SendKeys`
  - `ResizePane`
  - `ZoomPane`
  - `UnzoomPane`

### 5.3 agent/hub 协议里仍残留 session/pane 形状

已识别：

- `api/proto/tgent/v1/agent.proto`
- 事件里仍有 `session_id` / `pane_id`
- HTTP proxy path 仍以 `/sessions` 等资源路径表达
- hub traffic/reporting 里仍记录关闭的 `session_id`
  - `internal/hub/agent_reporter.go`
  - `src/app/api/internal/hubs/agents/report/route.ts`

结论：

- 这里必须分层看：
  - signaling / relay 机制可保留
  - session/pane 资源形状不能继续作为 termx 的终端模型

进一步区分：

- `session_id` 如果只是 relay / signaling / websocket 连接 ID，可以保留，但应改名或在语义上与 tmux session 彻底脱钩
- `pane_id / window_id` 这种直接映射 tmux 结构的字段，后续不能继续作为终端产品模型主轴

## 6.5 不是简单改名的问题

当前已经能明确排除一种误判：

- 这次接入**不是**“把 session 改名为 terminal 就行”

因为至少有三类东西是实质性结构问题：

1. `tgent-go/internal/daemon` 直接以 tmux session/window/pane 为设备端真相源
2. `tgent-app` 的状态树直接围绕 `session -> window -> pane`
3. `tgent-web` 的 schema / telemetry / hub reporting 直接以 session 为统计对象

所以后续必须分别处理：

- 设备端 runtime 替换
- app 页面/状态模型改造
- control plane schema / telemetry 映射设计

## 6. 当前分类

### 6.1 直接复用

- `tgent-web` 登录、账号、dashboard、pricing、subscription 基础能力
- hub discover / heartbeat / relay / TURN / signaling
- `tgent-app` 产品壳与 WebRTC+xterm 技术路线

### 6.2 最小改造

- connect ticket 返回结构
- 设备详情页入口
- app/web 中“session 列表页 -> terminal 列表页”
- 文案中 tmux 表述

### 6.3 必须替换

- `tgent` 设备端 tmux daemon / pipe-pane / pane resize 协调路径
- 所有把 tmux `session/window/pane` 当终端真相源的地方
- 直接依赖 tmux layout 的页面和接口

## 7. 对后续实现的直接影响

### 第一优先级

- 先把 `termx` 仓库自身结构边界收口
- 否则后续 `tgent` 兼容层会把 `termx` 继续污染

### 第二优先级

- 先让 `termx daemon` 原生具备：
  - 注册 hub
  - 响应 signaling
  - 返回 terminal list

### 第三优先级

- 再去改 `tgent-web` / `tgent-app` 的 session 页面和 terminal 页面

## 8. 下一步

这份盘点完成后，下一份文档应明确：

- 第一段真正的结构性重构切片是什么
- 哪些文件先动
- 哪些行为先用 characterization tests 锁住
