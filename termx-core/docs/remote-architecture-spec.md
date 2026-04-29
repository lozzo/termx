# termx 对接 tgent 远程体系规格

状态：方向已确认，进入兼容性与架构盘点阶段

## 1. 这次要纠正的方向

上一版规格的思路偏向“围绕 `termx` 新建一套更小的 control plane / mobile app”。
这不是当前最优路线。

你刚确认的真实目标是：

- 不是重写 `tgent` 产品面
- 而是**直接沿用 `tgent` 的产品路线和基础设施**
- 把 `tgent` 里原本依赖 `tmux + tgent agent` 的部分，替换成 `termx`
- 最终不再单独保留一个 `tgent` 常驻二进制，而是让 `termx` 原生持有这套远程能力

所以这次重构的核心不是“新做一套极简 app”，而是：

> **保留 tgent 的 web / app / dashboard / 登录 / 密码 / 定价 / 订阅 / hub / TURN / signaling 体系，
> 把底层终端运行时从 tmux 路线切换为 termx 路线。**

## 2. 总体策略

最快速的路线应当是：

1. **保留 `tgent-web` 的产品壳**
   - dashboard
   - 账号密码登录
   - profile / settings
   - pricing / subscription / billing
   - 设备/节点管理
   - hub / release / admin 等现有能力

2. **保留 `tgent` 现有的 hub / TURN / signaling / discovery 体系**
   - 尽量迁移，不重写
   - 先保持现有接口形状或最小改造

3. **保留 `tgent-app` 的整体壳与导航结构**
   - 不再另起一个全新 mobile app scaffold
   - 只对“session 列表 -> terminal 列表”相关页面和数据流做改造

4. **把 `termx` 变成新的设备端原生 runtime**
   - `termx daemon` 内嵌原本 `tgent agent` 该负责的远程能力
   - 设备侧最终只需要 `termx`

## 3. 这次不再追求的事情

为了走最快路线，这一轮**不以“重做产品形态”为目标**。

也就是说，这一轮不优先做：

- 重新设计一套更极简的 control plane
- 从头新写一套 mobile app
- 大规模重构 `tgent-web` 的信息架构
- 先把 pricing / dashboard / account 相关能力删掉

这次的优先级是**兼容现有产品壳、最快打通 termx 替换**。

## 3.1 这次必须顺手处理代码架构

虽然产品路线以“尽快接入 `tgent` 现有壳”为主，但当前 `termx` 仓库本身的代码结构也确实需要先收口，否则后面的接入工作只会继续把代码堆脏。

当前最明显的问题：

- 根目录 `package termx` 承担了过多职责：
  - 对外公开 API
  - terminal server 实现
  - transport session loop
  - attach/bootstrap/stream 编排
  - 事件和 session 协议处理
- `termx.go` 里同时处理：
  - terminal CRUD
  - attach/input/resize
  - `session.*`
  - transport permission checks
- `tuiv2` 已经非常大，后续不能再把远程对接逻辑也塞进去
- `mobile/` 和 `web/` 目录现在在这个分支里还不是清晰的受控产品壳落点
- `workbenchsvc`/`session.*` 是本地共享工作台语义，不该继续和远程 terminal 接入耦合增长

所以这轮 spec 不只是“决定产品路线”，还必须明确：

- 未来的目录怎么落
- 哪些包禁止继续长胖
- `tgent` 兼容层应该放在哪里

## 4. 不可动摇的核心约束

即便沿用 `tgent` 外壳，`termx` 自身的架构原则仍必须守住：

- `termx` 服务端仍然是 **flat terminal pool**
- 不把 tmux 的 `session/window/pane` 层级灌回 `termx` 服务端
- 远程终端数据面仍然只走 **WebRTC DataChannel**
- `termx` 现有 `attach / snapshot / input / resize / events / SyncLost / Closed / screen_update` 语义必须保留
- `screen_update / snapshot / bootstrap` 这条链继续保持二进制，不回退成 JSON 数据面
- 最终设备侧常驻进程是 `termx`，不是 `termx + tgent agent`

## 5. 新的目标架构

### 5.1 保留的远端组件

远端大体沿用 `tgent`：

- `tgent-web`
  - 用户登录
  - dashboard
  - pricing / billing / subscription
  - 设备/节点管理
  - hub 目录
  - connect ticket / auth / metadata

- `tgent hub`
  - agent/device 注册
  - signaling
  - TURN / relay
  - discovery / heartbeat / liveness

- `tgent-app`
  - 现有 app 壳、路由、登录、设置、Dashboard
  - 终端页和列表页做最小必要改造

### 5.2 替换的设备端组件

设备端不再保留独立 `tgent` 常驻二进制。

改为：

- `termx daemon`
  - 本地 terminal pool 真相源
  - 本地 PTY / attach / snapshot / stream
  - 内嵌远程注册、signaling、WebRTC bridge
  - 对接 `tgent-web` / `tgent hub`

可以把它理解成：

> **“用 termx 替掉 tmux + tgent agent 的组合。”**

## 5.3 目标代码架构分层

为避免后续继续糊成一团，代码层次建议收成 5 层：

### 第 1 层：termx core

这是 `termx` 作为终端运行时的核心，负责：

- terminal pool
- PTY
- snapshot
- attach/stream
- screen update
- event bus

要求：

- 不依赖 `tgent`
- 不依赖 dashboard / billing / account
- 不依赖 mobile/web 壳

### 第 2 层：local client / workbench

这是现有 `tuiv2` 和 `workbench` 相关逻辑，负责：

- 本地 TUI
- workspace / tab / pane / floating
- `session.*` 相关共享工作台能力

要求：

- 仍可继续存在
- 但要明确它是“本地/工作台侧能力”
- 不能成为远程 terminal 产品模型的基础

### 第 3 层：shell-neutral terminal client runtime

这一层不是设备端远程注册 runtime，而是给多个 shell 复用的客户端运行时层。

它负责：

- attach / bootstrap
- stream 消费
- 本地 terminal cache / vterm cache
- terminal 级 resize 协调
- shell-neutral client bridge 抽象

当前最接近这一层的，其实是：

- `tuiv2/runtime`
- `tuiv2/bridge`

这是当前结构里最容易误导后续扩张的地方，因为它们实际上已经不再只是 TUI 私有逻辑。

要求：

- 后续应逐步从 `tuiv2/` 名下移出
- 成为 shell-neutral 层
- 供 `tuiv2`、未来的 `web/app` shell 共用
- 只处理 terminal client 语义，不承载 workbench/session 结构真相

明确不属于这一层的东西：

- `session.*` 文档结构真相
- workbench 视图树
- lease / ownership 的工作台级编排
- modal / viewport / shell-specific UI state

### 第 4 层：embedded remote runtime

这是新的设备端远程层，负责：

- 基于抽象接口驱动设备端远程注册与长连
- WebRTC bridge
- terminal list 暴露
- termx protocol 到远程通道的桥接
- 设备端远程生命周期管理

要求：

- 放在 `termx` 仓库内
- 跟 `tuiv2` 解耦
- 不直接承载产品壳逻辑
- 不直接知道 `tgent` 的 DTO、HTTP 路径或 hub 私有 wire contract
- 应依赖 discovery / signaling / control 的抽象接口，而不是依赖某个具体产品面的契约

### 第 5 层：tgent compatibility adapters

这是“termx 对接 tgent 现有体系”的适配层，负责：

- 对接 `tgent-web` 的控制面 API
- 对接 `tgent hub` 的注册/发现/signaling 契约
- 把 `termx` 的 terminal 语义映射到 `tgent` 现有产品入口

要求：

- 必须集中
- 不要把 `tgent` 兼容逻辑散落到 `protocol/`、`tuiv2/`、core server 各处
- 这样以后如果再脱离 `tgent`，爆炸半径是局部的
- 这是唯一允许理解 `tgent-web` / `tgent hub` 具体 contract、字段名、路径形状、reporting schema 的地方

进一步约束：

- embedded remote runtime 依赖 compat 暴露的抽象接口
- compat 依赖 `tgent` 具体协议
- 不反过来让 runtime 直接 import `tgent` contract

### 第 6 层：product shells

这是沿用或迁移来的产品壳：

- `web/control` 对应 `tgent-web`
- `mobile/app` 对应 `tgent-app`

要求：

- 它们是产品前端/控制面应用
- 不和 Go runtime 包互相污染
- 只通过明确定义的 control/hub 接口对接

## 5.2 建议的目标目录结构

这不是一次性全搬迁计划，而是后续重构应逐步收敛到的目录图。

```text
cmd/
  termx/                 用户设备侧主二进制
  termx-hub/             服务器侧 hub / TURN 二进制

docs/

protocol/                termx 原生线协议，只放 termx 自己的协议类型
transport/               termx 原生 transport 抽象
pty/
vterm/
fanout/
perftrace/

internal/
  core/
    server/              terminal server 实现
    terminal/            terminal 生命周期、snapshot、attach、stream
    stream/              bootstrap / screen update / slow-consumer 策略
    events/              event bus
  workbench/
    session/             session.* 服务端实现
    doc/
    ops/
    svc/
  remote/
    runtime/             设备端 embedded remote runtime 主协调
    identity/            设备身份与本地密钥材料
    discovery/           hub 发现与选择
    signaling/           signaling client / server 对接
    rtc/                 WebRTC / DataChannel bridge
    bridge/              termx attach/protocol 到远程 runtime 的桥接
  client/
    runtime/             shell-neutral terminal client runtime
    api/                 shell-neutral client 抽象（当前 tuiv2/bridge 的归宿）
  compat/
    tgent/
      control/           对接 tgent-web 的 API client / contract
      hub/               对接 tgent hub 的契约适配
      model/             session->terminal 等兼容映射模型
      telemetry/         tgent session telemetry 与 termx terminal/attachment telemetry 的翻译层

tuiv2/                   本地 TUI，只做本地/workbench 客户端

web/
  control/               迁入或改造后的 tgent-web

mobile/
  app/                   迁入或改造后的 tgent-app
```

## 5.3 目录边界规则

### root `package termx`

根包后续应该尽量只保留：

- 对外公开类型
- `NewServer`
- 少量稳定公开 API façade

不应继续扩张成：

- 远程 runtime 大杂烩
- `tgent` 兼容层
- workbench session 实现细节

### `internal/core`

把现在散在根目录的实现逐步收进去，例如：

- `terminal.go`
- `snapshot.go`
- `event.go`
- `attachment_stream.go`
- `stream_screen_state.go`
- `live_output.go`

目标是让根目录不再同时堆一批 server internals。

### `internal/workbench`

现在的 `workbenchdoc` / `workbenchops` / `workbenchsvc` 和 `session.*` 处理逻辑，应统一归到“本地工作台能力”边界。

要求：

- `workbenchdoc` / `workbenchsvc` 是本地共享 session/workbench 的真相源
- 远程 terminal 接入不要直接依赖 shell-local workbench 状态
- `session.*` 继续存在也可以，但要被隔离，而不是继续长进 core server 主路径
- `tuiv2/workbench` 长期目标应收缩成 shell-local `viewstate`，而不是再维护第二份结构真相

### `internal/client/runtime`

当前 `tuiv2/runtime` 和 `tuiv2/bridge` 里已经有一层可复用的 shell-neutral 客户端运行时。

要求：

- 这层不能继续挂在 `tuiv2` 名下
- `tuiv2`、未来 app/web shell 都应该依赖它
- 它可以依赖 `protocol/transport` 和共享 session/document types
- 它不能反向依赖 `tuiv2/*`
- 它只做 terminal client runtime，不做本地 workbench/session state 编排

### `internal/remote`

所有设备端远程能力都收在这里：

- 不放进 `cmd/termx`
- 不放进 `tuiv2`
- 不散落在根包

这样后续“termx 原生持有 agent 能力”才会结构清晰。

### `internal/compat/tgent`

所有 `tgent` 特有的契约、字段、接口形状都集中放这里。

目的：

- 避免 `tgent` 兼容代码污染 `termx` 原生协议
- 避免未来改 control plane 时全仓到处改

### `internal/compat/tgent/telemetry`

`tgent-web` 当前的 `sessionBytesIn / sessionBytesOut / sessionStartedAt / relay_traffic(session_id, session_type, duration...)` 本质上是控制面 telemetry schema。

要求：

- 这类 schema/telemetry 翻译必须集中在 compat 层
- 不把这套 `session_*` telemetry 语义直接扩散到 termx core 或 embedded remote runtime
- 后续如果要支持“一个设备多个 terminals / attachments”，映射逻辑只在这里改

### `web/` 和 `mobile/`

这两块如果决定正式迁入本仓，就要明确把它们当作：

- **产品壳工程**

而不是“零散实验目录”。

也就是说：

- `web/control` = 迁入或裁剪后的 `tgent-web`
- `mobile/app` = 迁入或裁剪后的 `tgent-app`

不能让它们长期保持“目录存在，但边界和归属不清”的状态。

## 5.4 当前代码压力点

下面这些点是这轮重构里应优先收口的：

### 压力点 1：根目录文件过厚

当前根目录还保留着大量 server internals，例如：

- `termx.go`
- `terminal.go`
- `snapshot.go`
- `attachment_stream.go`
- `stream_screen_state.go`

这会让后续 remote 接入自然地继续往根目录堆。

### 压力点 2：session/workbench 耦合进 server 主路径

当前 `termx.go` 直接承载 `session.*` 请求处理和 `workbenchsvc` 接入。

这对本地共享工作台是可以理解的，但如果不先隔离，后续：

- `termx core`
- `local workbench`
- `remote terminal path`

三条线会继续在同一个 server 文件里缠死。

### 压力点 3：tuiv2 过大

`tuiv2` 目前已经是仓库最大体量区域。

规则必须写清楚：

- `tuiv2` 继续只做本地客户端/TUI/workbench
- 不把 `tgent` 对接逻辑、设备端 remote runtime 逻辑塞进去
- 其中已经具备通用价值的 `runtime/bridge` 还要逐步从 `tuiv2` 名下抽离

### 压力点 4：session 真相重复

当前 session 相关结构真相至少有两层：

- daemon/workbench 侧的 canonical `workbenchdoc`
- `tuiv2/workbench` 侧的本地可变结构

如果这个问题不先定边界，后续多 shell 共享时会越来越难收。

原则必须明确：

- 共享结构真相只保留一份
- shell 自己只保留 focus、viewport、modal、floating rect、host theme 这类 UI/viewstate
- 不再让 shell 持有第二份 authoritative workbench 结构

### 压力点 5：terminal runtime 与 workbench runtime 混层

当前 `tuiv2/runtime` 里同时带有：

- terminal attach/bootstrap/stream 语义
- ownership / lease / pane binding 等工作台级语义

这两类东西后续必须继续拆：

- terminal client runtime
- 本地 workbench/session client orchestration

### 压力点 6：shell 逻辑落在 `cmd/`

当前 `cmd/termx/web.go` 本质上已经是 shell/product 逻辑，而不是单纯 CLI glue。

这个边界要尽早收正：

- `cmd/` 只做二进制入口与 wiring
- shell/product 代码放到自己的 shell 层

### 压力点 7：tgent session telemetry 与 termx terminal telemetry 未隔离

当前 `tgent-web` 中有一层不是产品 session，也不是 tmux session，而是 relay/client telemetry session。

这层如果不先隔离，后续会出现两种错误：

- 把它误当成 tmux/product session 一起删掉
- 或者把它错误扩散进 termx runtime 核心模型

所以必须单独建立 compat telemetry 边界。

### 压力点 8：product shells 尚未正式收编

当前分支里 `web/`、`mobile/` 还没有形成清晰受控结构。

如果后面确认要把 `tgent-web` / `tgent-app` 迁进本仓，那就要在目录层面正式承认它们，而不是继续放成半迁移状态。

## 6. 组件职责

### 6.1 termx daemon

`termx daemon` 负责：

- terminal 生命周期
- PTY 管理
- 本地 attach / snapshot / stream
- 本地 Unix socket / CLI 能力
- 嵌入设备端远程 runtime
- 向 `tgent` hub 注册
- 处理来自 hub 的 WebRTC/signaling/connect 请求

它不负责：

- dashboard / account / billing
- hub 目录真相
- 用户登录态数据库

### 6.2 tgent-web

`tgent-web` 继续负责：

- 登录 / 注册 / 密码 / session
- dashboard
- pricing / subscription / billing
- agent/device 归属
- hub 列表与在线态目录
- connect ticket
- release / admin / override 等已有管理能力

但它需要做的改造是：

- 从“tmux/session 视角”调整为“termx/terminal 视角”
- 至少新增或改造 terminal list / terminal connect 相关 API

### 6.3 tgent hub

`tgent hub` 继续负责：

- 设备端注册
- 在线状态维护
- offer / answer 转发
- TURN / relay
- heartbeat
- discovery

优先原则：

- 能迁移就迁移
- 能兼容就兼容
- 不从零重写 TURN

### 6.4 tgent-app

`tgent-app` 继续负责：

- 用户登录
- 设备列表
- Dashboard / Settings 等现有产品壳
- terminal 连接页

主要改造点：

- 把原来围绕 `session` 的终端入口，改成围绕 `terminal`
- session 列表页改为 terminal 列表页
- terminal 连接数据流改接 `termx`

### 6.5 shell-neutral client runtime

这一层是多个 shell 共用的客户端运行时，不是产品壳。

它负责：

- 跟 `termx` protocol 对接
- attach/bootstrap/stream 生命周期
- 本地 terminal surface 状态
- 供 TUI、web、app 复用的 shell-neutral client 能力

它不负责：

- TUI modal / input / render
- dashboard / settings / billing
- hub/control-plane 设备端注册
- workbench/session 文档真相
- `tgent` 特有 telemetry/schema 翻译

### 6.6 workbench/session client orchestration

这是本地共享工作台侧的客户端编排层。

它负责：

- `session.*` 客户端交互
- 本地 viewstate 与 canonical workbench 文档之间的同步
- pane binding / lease / ownership 等工作台级协同

它不负责：

- 设备端远程注册
- `tgent` compatibility
- 产品壳逻辑

## 7. 数据模型如何收口

这里是最关键的部分。

### 7.1 termx 服务端内核不变

`termx` 仍然只有：

- `terminal`

它没有：

- tmux session
- tmux window
- tmux pane

### 7.2 tgent 产品层允许保留“设备/节点”概念

`tgent` 现有的：

- agent
- device
- dashboard 上的节点概念

这些都可以保留。

但它们在 `termx` 语境里应理解为：

- 一个设备上运行着一个 `termx daemon`
- 这个 daemon 暴露一个扁平 terminal pool

### 7.3 session 页面改 terminal 页面

原 `tgent` 产品里如果存在“session 列表页”或以 session 为主入口的 terminal 页面，这次的目标是：

- **把 session 列表改成 terminal 列表**
- 每个 terminal 直接以 `terminal_id` 作为连接目标

也就是说，对外产品主路径从：

- `device -> session -> terminal-ish view`

改成：

- `device -> terminal -> terminal view`

### 7.4 本地布局不进入 termx 服务端

如果未来 app/web 还想保留某些分组、收藏、最近使用、标签等 UI 组织概念，这些都只能是客户端投影，不能重新塞回 `termx` 服务端模型里。

## 8. 对接方式

### 8.1 优先保留 tgent 的控制面和发现机制

优先保留：

- hub discover API
- heartbeat 上报
- online/offline agent report
- connect ticket
- user/session auth
- subscription / quota / dashboard 相关逻辑

理由很简单：

- 这些不是 tmux 专属逻辑
- 重写它们只会拖慢 termx 接入

但要加一条边界：

- `tgent-web` 的 session telemetry/reporting schema 属于 compat/telemetry 层，不自动等于 termx 的 terminal 模型

### 8.2 设备端从 tgent agent 改成 termx native runtime

原本由 `tgent agent` 做的事情，迁入 `termx`：

- 登录/注册到 control plane
- 选择 hub
- 建立 hub 长连
- 处理 signaling
- 建立 WebRTC
- 把 DataChannel 和本地 termx attach/protocol 桥接
- 返回 terminal 列表

这里的具体 `tgent` discover/register/signaling/ticket contract 不应该直接散进 runtime；应由 compat 适配层实现，再给 runtime 提供抽象接口。

### 8.3 WebRTC 数据面保持 termx 语义

虽然外层沿用 `tgent` 的 signaling / hub / TURN，
但终端数据面承载的是 `termx` 语义：

- `attach`
- `input`
- `resize`
- `snapshot`
- `screen_update`
- `SyncLost`
- `Closed`

不要因为要兼容 `tgent`，就在终端数据面再发明一套 tmux 风格 payload。

## 9. 协议与接口原则

### 9.1 可以兼容 tgent 的控制面接口

为了最快落地，下面这些可以优先兼容 `tgent` 现有接口形状：

- 设备注册
- hub discover
- connect ticket
- 登录鉴权
- hub heartbeat

### 9.2 需要新增或改造 terminal 视角接口

至少需要有一组围绕 `terminal` 的接口，例如：

- 列出某设备的 terminals
- 对某 terminal 申请 connect ticket
- 获取 terminal metadata

如果现有 `tgent` 页面/接口是按 `session` 写的，就在这一层做最小改造，而不是把 `session` 强行映射回 `termx` 服务端。

### 9.3 终端 streaming 语义必须保留

必须保留这些现有行为：

- `attach` 是 I/O 入口
- `observer` / `collaborator` 语义保留
- bootstrap 顺序保留：`Resize -> full ScreenUpdate -> BootstrapDone`
- 退出后的 `Closed` 保留
- `SyncLost` 仍然是快照恢复边界
- remote client 要保住 attach 后 stream 延迟开启时的首批 frame 缓冲/重放语义

## 10. 身份和密钥策略

这部分分“本轮最快落地”和“后续理想方向”两层看。

### 10.1 本轮最快落地原则

既然本轮目标是直接沿用 `tgent` 路线，那么：

- 优先复用现有账号登录体系
- 优先复用现有 connect ticket / hub token 路线
- 设备端密钥由 `termx` 原生持有
- 不再额外需要独立 `tgent` agent 二进制来保管或使用它

### 10.2 仍需避免的坏方向

即便为了快，也不应该继续扩大这些坏方向：

- 让 `termx` 服务端被迫接受 tmux session tree
- 让设备端长期依赖独立 `tgent` 常驻进程
- 在新集成里继续扩散“数据库布尔标记就是信任锚”这种做法

### 10.3 后续可继续优化

你前面提到的“更像 SSH 的用户自持证书”方向仍然是合理的，
但从实现顺序看，它更适合作为：

- **termx 接入 tgent 成功后的下一阶段安全重构**

而不是当前最快替换路径的前置条件。

也就是说：

- **第一阶段优先完成 termx 替换**
- **第二阶段再把密钥模型做得更干净**

## 11. 这次最重要的产品取舍

### 保留

- dashboard
- 账号密码登录
- pricing / billing / subscription
- settings
- hub / relay / turn 基础设施
- 现有 app/web 壳

### 改造

- session 列表页
- session -> terminal 的连接模型
- 设备端 runtime
- 终端桥接与 streaming

### 删除或弱化

- 独立 `tgent` 常驻二进制
- tmux 作为设备端终端真相源

## 12. 推荐实施顺序

### 第一阶段：代码架构和接口边界盘点

- 盘点 `tgent-web` / `tgent hub` / `tgent-app` 当前哪些接口和页面强依赖 tmux/session 模型
- 盘点当前 `termx` 仓库哪些目录边界已经会阻碍后续远程接入
- 明确 core / workbench / terminal client runtime / remote / compat / product shells 的落点
- 单独识别 `tgent` session telemetry 和 tmux/product session 的差别
- 明确哪些地方只需“换数据源”，哪些地方必须“改页面语义”

### 第二阶段：先做结构性收口

- 先把最容易继续长胖的代码边界收口
- 至少把 remote runtime 落点、compat 层落点、web/mobile 产品壳落点定下来
- 避免一边做功能一边继续堆脏

### 第三阶段：termx 原生持有 agent 能力

- 把原 `tgent agent` 的核心远程能力嵌进 `termx daemon`
- 先让 `termx` 能原生注册 hub、响应 signaling、返回 terminal list

### 第四阶段：termx 对接 tgent 控制面

- 复用 `tgent-web` 登录、discover、ticket、dashboard 路线
- 打通设备列表和 terminal 列表

### 第五阶段：改 app/web 的 session 页面

- session 列表页改 terminal 列表页
- connect 流程从 session target 改成 terminal target

### 第六阶段：打通 live terminal

- WebRTC DataChannel
- termx protocol bridge
- xterm.js / app terminal view
- reconnect / bootstrap / SyncLost recovery

## 13. 当前规格结论

这轮重构的正确方向应当是：

> **保留 tgent 的产品壳和远端基础设施，
> 把设备端从 tmux+tgent agent 替换成 termx 原生 runtime，
> 并把 session 主入口改造成 terminal 主入口。**

同时，在开始实现前，必须先把当前仓库的代码边界重新梳理清楚，至少明确：

- `termx core`
- `workbench / session`
- `shell-neutral client runtime`
- `embedded remote runtime`
- `tgent compatibility`
- `web/mobile product shells`

这比“新起一套 termx control/mobile 体系”更贴近你的真实目标，也更快落地。
