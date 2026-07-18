# TUI 多 Endpoint / 多 Route 接入基线

状态：RTC001 后技术边界

活动任务与顺序只以仓库根目录 `workflow.md` 为准。本文只约束 TUI 如何消费 Go Client Engine，不维护独立迁移队列。

## 1. 核心模型

- Endpoint 表示当前客户端要连接的 daemon。
- Route 表示到达该 Endpoint 的方式。
- 跨 Endpoint terminal 引用固定为 `TerminalRef = EndpointID + TerminalID`。
- TUI 不拥有 Endpoint identity、Route config、session generation、winner、credential、terminal lifecycle 或 committed history truth。
- TUI 只消费 Go Client Engine 和 daemon API 的 projection。

当前 Route：

```text
local-unix
direct-webrtc-tcp
ssh-webrtc-tcp
managed-webrtc
```

所有远程 Route 成功后都必须表现为同一种 Go-owned ReadyPeerSession 和 Proto API session。TUI 不按 Direct/SSH/Cloud 分叉 terminal、history、input、resize 或 file workflow。

## 2. 依赖方向

```text
tui reducer/view
      |
      v
tui/port
      |
      v
tui/adapter/clientruntime
      |
      v
client/runtime -> client/endpoint -> generated Endpoint Proto
```

- renderer 只读取 view-model。
- reducer 只通过 message/effect 修改 UI state。
- adapter 不能缓存第二份 protocol client 或自行选择 Route。
- CLI 与 TUI composition 必须复用同一个 registry、planner 和 SessionOwner。

## 3. Endpoint 投影

TUI 可保存和展示：

- EndpointID、label、连接状态和稳定诊断。
- 当前 ReadyPeerSession 的 RouteID、generation 和 observed managed path。
- `TerminalRef`、terminal lifecycle projection 和用户工作台绑定。

TUI 不得保存：

- Route kind-specific 配置副本。
- credential body、DeviceIdentity private key 或 CapabilityGrant body。
- runtime winner、stale resource handle 或 reconnect state machine。
- daemon terminal running/exited 真值或 committed history。

## 4. 连接动作

TUI 连接动作只能提交：

```text
EndpointID
ConnectIntent
optional RouteID override
```

Go Client Engine 负责：

1. 加载并验证 Endpoint/Route config。
2. 解析当前平台能力和 credential availability。
3. 生成 attempt plan。
4. 建立 Route connector。
5. 完成 remote auth、Hello 和 Proto API session。
6. 发布 generation-bound ReadyPeerSession。
7. 取消 loser 并释放资源。

TUI 不得为连接失败增加 local fallback、旧 SSH proxy、重复 attach、定时刷新或隐式 Route 改写。

## 5. Terminal 操作

- list/create/attach/detach/input/paste/resize/history/file 都必须带 owning Endpoint correlation。
- 非幂等 input 不自动重放。
- operation/resource 必须受 session generation fence 约束。
- session replacement 后，旧 generation 的 callback、resource 和 UI effect 必须失败，不得写入新 terminal view。
- endpoint session 关闭不等于 daemon terminal 退出。

## 6. 配置与分享

- TUI endpoint 命令和 UI 编辑器只能通过 Go Endpoint domain 修改 registry。
- kind-specific 字段来自 generated Proto contract；不得在 TUI state 定义第二份业务 DTO。
- `pair create` 是 daemon bootstrap/authorization 流程。
- `endpoint share` 是客户端之间迁移 portable Route/policy 的流程。
- TUI 只展示导入 diff 和用户确认，不解析 credential secret 或自行签发 grant。

## 7. 生命周期

- TUI 进程退出时由 SessionOwner 关闭 session、operation 和 resource。
- registry reload 只在 dial identity 变化时要求后续 reconnect；label/priority 等变化不能热改已建立 session。
- Cloud failure 只影响 managed Route projection。
- Direct/SSH connector 只有在平台 primitive 或所需 credential 缺失时显示稳定 unavailable，不得回退旧 transport。

## 8. 当前与后续

- RTC001：完成 versioned Proto Route/config、strict parser、assembler 和 planner contract。
- RTC002-RTC006：Go runtime 已把 Direct 与 SSH 收口为统一 PeerSession；Cloud 的最终统一装配按 `workflow.md` 推进。
- Android、Cloud 和最终验收顺序以 `workflow.md` 为准。
- Web/WASM 当前冻结，TUI 不为未来 Web 抽象额外 UI 或 runtime contract。
