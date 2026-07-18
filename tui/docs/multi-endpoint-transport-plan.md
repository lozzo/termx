# 多 Endpoint / 多 Transport 技术规划

## 文档职责

本文只定义 TUI/CLI 客户端侧的多 Endpoint、多 Route 运行时边界，以及当前活动切片 `CONN003` 的实现目标。

- 当前任务顺序、允许修改范围、准入命令和完成状态只看仓库根目录 `workflow.md`。
- Endpoint/Route/Transport/Path、授权、Cloud 和后续迁移的完整产品架构以 [`docs/remote-platform/unified-endpoint-route-refactor-plan.md`](../../docs/remote-platform/unified-endpoint-route-refactor-plan.md) 为准。
- TUI reducer、service、render 和 history ownership 以 [`tui/docs/architecture.md`](architecture.md) 为准。
- 仓库目录与依赖方向以 [`docs/development/repository-layout.md`](../../docs/development/repository-layout.md) 为准。
- 本文不重复保存 CLI 命令树、Cloud 专项设计或历史实施记录，避免形成第二份活动计划。

## 当前真实状态

`CONN001` 和 `CONN002` 已完成：

- `connections.yaml` v2 已能保存一个 Endpoint 下的多条 Route。
- `EndpointID + TerminalID` 已是跨 Endpoint 的 `TerminalRef`。
- DeviceIdentity、ClientAccessIdentity、PairingTicket、client-bound CapabilityGrant v2 和 channel binding auth 已有领域实现。
- local Unix、SSH stdio、managed WebRTC 各自已有可用接线。

`CONN003` 尚未完成：

- C3X 已删除 `Endpoint.ResolveCurrentRoute`、TUI lazy bundle/session owner 和 CLI 直接 route/dial owner；CLI/TUI 当前保留明确待接 `client/runtime` 的调用缺口，不再有可继续修补的旧运行时。
- C3B 已在 `client/endpoint` 完成纯领域 `RouteSelectionPlanner`、local/SSH full-race 分组、priority hedge、manual override、唯一 managed 单路计划和机器可读 fixture；它只产出不可变 attempt groups，不 dial，也不拥有 winner。
- C3C 已把 local/SSH fresh DeviceIdentity challenge proof、managed channel-bound auth、route authorization、Hello 与 lifecycle signal 固化为统一 `ReadySession` 发布门禁；默认全量竞速的真实启动和 winner/loser cleanup 尚未成为 CLI/TUI 共同运行时。
- C3E 已让 CLI/TUI native composition 共用 per-endpoint `ClientRuntime/SessionOwner`、local/SSH/lazy-managed dialer registry、system Clock 和 lifecycle mailbox；TUI 不再从 raw protocol client 推断 endpoint 状态。operation/channel stamp 的完整副作用前校验仍由 C3F 完成。
- `SessionGeneration` 和 channel-bound operation stamp 尚未覆盖 attach、input、paste、resize、detach 与迟到回包。

因此，下文所有 planner、race、generation 和 stamp 语义都是 `CONN003` 的目标契约，不是当前已交付能力。

## 当前非目标

- 不在 CONN003 接入 direct TLS、LAN discovery、managed WebRTC 共同竞速或 endpoint share。
- 不实现长期多活动 session、热备、无中断动态换路或跨 Endpoint fallback。
- 不把 planner 扩展成插件系统，也不恢复旧 remote、旧 Hub/session-token、旧 core/TUI 路径。
- 不让 TUI/CLI、Cloud Companion、Hub 或 Relay 成为 terminal lifecycle、history、CapabilityGrant 或 daemon identity private key 的第二真值。

## 领域边界

```text
Endpoint  一个客户端要连接的 daemon 目标
  Route   到达该 Endpoint 的持久配置
    Transport  某次 route attempt 的运行时载体
      Path     managed WebRTC 内部 direct / single_relay 路径
```

- 一个 daemon 对应一个 Endpoint；local Unix、SSH、direct TLS 和 managed WebRTC 是该 Endpoint 下的 Route。
- `connections.yaml` 只保存期望配置，不保存 winner、generation、dial phase、observed path、运行时错误或 transport 句柄。
- `TerminalID` 只在 owning daemon 内唯一；跨 Endpoint 数据必须使用 `TerminalRef{EndpointID, TerminalID}`。
- client 侧 `client/runtime` 管理“我如何连接 daemon”；daemon 侧 attachment/client manager 管理“哪些客户端连接我”。两者不是同一个领域模型。
- TUI 不拥有 terminal lifecycle、committed history、daemon 文件系统或授权真值。

## CONN003 目标运行时

### RouteSelectionPlanner

planner 是 `client/endpoint` 的纯领域逻辑：

- 输入已规范化 Endpoint、连接意图、可选 route override 和 generation。
- 过滤 disabled、manual-only、当前阶段不支持自动竞速或缺少必要 identity/credential 的 route。
- 无 priority 时，所有 eligible local Unix/SSH route 在 `t=0` 启动。
- 有 priority 时，同 priority 同组，后续组按 `hedge_delay` 启动。
- 显式 `--route` 只计划指定 route，并允许选择 manual-only route。
- 输出不可变 attempt groups；不 dial、不读 credential store、不访问 Cloud、不写 registry。

CONN003 自动竞速只包含 `local-unix` 和 `ssh-stdio`。单条 `managed-webrtc` 保留已有能力，但不参与共同竞速；direct TLS/LAN、managed 共同竞速和 share 分别属于 CONN004、CONN005、CONN006。

### ReadySession

transport 建立不等于 Endpoint 已连接。一次 attempt 只有依次完成以下边界后才能产生 `ReadySession`：

1. 建立 transport。
2. 完成 fresh DeviceIdentity challenge proof，并匹配 Endpoint pin；local/SSH 使用 versioned Proto challenge/result，managed 使用 channel-bound DeviceHello。
3. 完成当前连接意图要求的授权。
4. 完成 termx protocol Hello。

首个产生 `ReadySession` 的 attempt 在唯一线性化点成为 winner。静态 route 顺序只稳定启动计划和错误诊断，不能让稍晚 Ready 的 attempt 反超。

### ClientRuntime

`client/runtime` 的目标职责是每个 Endpoint 唯一的跨端 session owner：

- 分配递增 `SessionGeneration`。
- 同一 Endpoint 只维护一个 in-flight race 和一个活动 winner。
- 取消并等待 loser transport、protocol client 和 SSH 子进程退出。
- 持有 sticky route override，但不写回 registry。
- 通过 mailbox 发布 lifecycle event，不在 manager 锁内等待 UI 消费。
- 为 service 调用签发当前 generation lease，并在结果提交前再次校验。

CLI 不另建一套选路状态机。root TUI、terminal/file/workspace 命令和 `endpoint test` 共用 planner、attempt dialer 与 session owner；`cmd/termx` 只负责参数、target resolution、输出和退出码。

## 消息与取消链路

```text
CLI/TUI intent
  -> ClientRuntime.ensureSession
  -> RouteSelectionPlanner.Plan
  -> local/SSH attempts
  -> identity proof + authorization + Hello
  -> ReadySession winner CAS
  -> service adapter
  -> stamped result message
  -> reducer generation guard
```

取消顺序必须可追踪：

```text
new generation fence
  -> cancel in-flight attempts / old session
  -> close protocol transports
  -> wait SSH child / future route resources
  -> publish final lifecycle state
```

- winner 产生后必须取消并回收全部 loser；cleanup 失败只进入诊断，不能复活 loser。
- route switch 或 reconnect 先建立新 generation fence，再释放旧 winner。
- stale cleanup 只能查询已有且 stamp 精确匹配的 bundle，禁止 cleanup 触发 lazy dial。
- adapter 尚未调用时返回 `Attempted=false`；adapter 已调用后返回 `Attempted=true`。输入和 paste 在结果不确定时不得自动重放。

## Channel-Bound Stamp

attach candidate、confirm、commit、cleanup、detach、input、paste 和 resize 必须携带创建 channel 时的原始身份：

```text
EndpointSessionStamp = EndpointID + RouteID + SessionGeneration
AttachmentStamp      = EndpointSessionStamp + TerminalID + Channel
                       + SurfaceID + ViewID + OperationID
```

- 旧 generation 的迟到结果可以进入消息队列，但 reducer 不能把它提交为当前状态。
- candidate 失败或 stale finalize 只清理 candidate，保留旧 committed binding。
- candidate 成功并通过 operation/generation guard 后，先原子提交新 binding，再精确 detach previous binding。
- route 切换保持 `EndpointID` 和 `TerminalRef`，不能把旧 session 的错误投影到新 session。

## 持久化边界

允许持久化：

- EndpointID、label、DaemonIdentity pin、connect/selection policy。
- RouteID、kind、地址、credential ref、enabled/manual-only/priority 等期望配置。
- workbench 中的 `TerminalRef` 连接意图。

禁止持久化：

- 当前 winner、SessionGeneration、AttemptID、ReadySession、dial phase、错误和 observed path。
- transport/protocol client/SSH process 句柄。
- raw CapabilityGrant、DeviceIdentity private key、SSH 密码或私钥。
- runtime attachment channel、live/history/copy 投影。

## 删除与替换边界

CONN003 直接删除旧职责，不保留双路径：

- C3X 已删除 `Endpoint.ResolveCurrentRoute`、TUI lazy bundle/session owner 和 CLI 直接 route/dial owner。
- `cmd/termx` 不再直接选择 route 或保存 session state。
- `tui/adapter/clientruntime` 不得重新承担 route 选择、dial、event publish、bundle cache 或 session owner。
- 不新增 local fallback、raw SSH shell fallback、legacy remote bridge 或仓库内旧代码快照。
- 不为 managed WebRTC、direct TLS、LAN discovery 或 share 提前扩展通用抽象。

## 验收边界

本阶段必须由真实 local daemon 与 OpenSSH host 验证：

- 无 priority 的 local/SSH full race。
- priority grouped hedge。
- 显式 route override 及当前 TUI session 内 sticky 行为。
- loser protocol transport 和 SSH 子进程回收。
- route 切换后 `TerminalRef` 稳定。
- 旧 generation 的 attach/input/resize/result 被拒绝且不触发 lazy dial 或输入重放。

具体子任务、测试命令和双 Agent 审查门禁只维护在 `workflow.md`。
