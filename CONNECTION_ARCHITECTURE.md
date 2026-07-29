# AnyTTY Cloud 连接架构与审查

## 1. 目标

连接路径按两个优先级设计：第一是安全，第二是用户快速连接。

已确认的产品要求：

- daemon 只在首次注册时直接访问 Controller。
- 已授权 daemon 和客户端在 Controller 不可达时，仍能连接原 Edge。
- pairing 由 daemon 提供自己的 Edge locator 和通信凭据，客户端直接访问 Edge。
- 客户端只在没有可信 locator、locator 明确失效或 Edge 传输不可达时回源 Controller。
- Relay 的实时连接状态和短租约由 Edge 处理，Controller 不进入单次 session 热路径。
- 开发期协议一次性升级，不保留旧 RPC、消息、记录兼容代码或旧测试。

## 2. 当前结果

| 能力 | 状态 | 实现边界 |
| --- | --- | --- |
| enrollment 一次性访问 Controller | 已实现 | 完成后返回 daemon binding 与 Edge locator |
| daemon 启动直连 Edge | 已实现 | runtime 只读取 version 2 record，不创建 Controller client |
| Controller 离线时 daemon 重连原 Edge | 已实现 | 前提是 Edge 进程仍持有 verification keys |
| pairing offer 直连 Edge | 已实现 | offer 内 grant 和 locator 由 daemon identity 绑定 |
| 已授权客户端 cache-first | 已实现 | credential locator 命中时 Controller RPC 为零 |
| 精确 Controller fallback | 已实现 | 仅本地 typed transport error 或明确位置不存在触发 |
| Relay 创建和续租 Edge-local | 已实现 | 每次租约最多五分钟，不能扩大 binding 委托 |
| Controller 离线 usage | 已实现 | Edge durable outbox，恢复后幂等补报 |
| binding 覆盖 locator 完整性 | 已实现 | Controller 签名 claims 内含 locator SHA-256 |
| 私有 CA 隔离 | 已实现 | locator CA 使用独立 root pool，不继承系统根 |
| 旧开发协议删除 | 已实现 | proto、服务方法、生成代码、实现和旧测试一并删除 |

## 3. 信任链

```text
Controller binding signing key
  -> DaemonBindingClaims
       -> daemon/account/device public key
       -> owning edge_id
       -> EdgeLocator SHA-256
       -> revision / validity / Relay delegation

daemon DeviceIdentity
  -> AgentGateway proof of possession
  -> CloudRouteGrant for one ClientAccessIdentity
  -> PairingRouteGrant for one claim digest

ClientAccessIdentity
  -> ClientGateway stream proof
  -> DataChannel capability authentication
```

`DaemonBindingClaims` 不是 bearer token。复制 enrollment record 但没有 DeviceIdentity 私钥，不能生成有效 AgentGateway proof。

`CloudRouteGrant` 只允许 discovery/signaling，不包含 terminal scope。最终 terminal 权限来自 daemon AccessStore 签发的 CapabilityGrant，并在 DTLS DataChannel 内再次验证。

## 4. enrollment 时序

```text
daemon                 Controller                    Edge Directory
  | enrollment code        |                               |
  |----------------------->|                               |
  |<---- random challenge -|                               |
  | device proof ---------->|                               |
  |                         | select available Edge ------>|
  |                         | project EdgeLocator <--------|
  |                         | hash locator                  |
  |                         | sign DaemonBindingClaims      |
  |<-- binding + locator ---|                               |
  | atomically save v2 record                              |
```

record 加载时先做严格 JSON 和 protobuf 校验，再检查 daemon/account/Edge 一致性及 locator SHA-256。旧版本和未知字段直接失败，用户需要重新授权。

## 5. daemon 快路径

```text
load record
  -> decode binding + locator
  -> TLS to locator endpoint
  -> AgentHello(binding, DeviceIdentity proof)
  -> Edge verifies Controller signature and target
  -> Edge attaches authenticated generation
  -> heartbeat / signaling
```

该路径没有 Controller DNS、TCP、TLS 或 RPC。AgentGateway 断开只对同一 Edge 退避重连。

当前限制：Edge 只从活跃 EdgeControl Welcome 获取 binding verification keys，尚未把最后验证的 key bundle 持久化。因此“Controller 已停止但 Edge 未重启”支持直连；“Edge 和 Controller 同时重启失败”会 fail closed。

## 6. pairing 快路径

pairing offer 包含 claim、daemon public identity、短期 pairing route grant 和 Edge locator。grant 同时绑定 claim digest 与 locator digest。

```text
scan offer
  -> verify daemon public identity
  -> verify grant signature and expiry
  -> verify claim and locator digests
  -> TLS to Edge
  -> pairing-only ClientGateway
  -> daemon live precheck
  -> DataChannel claim exchange
  -> save capability + route grant + locator
```

Controller 从头到尾不参与。集成测试在停止 public Controller 后执行完整 pairing 和 terminal I/O。

## 7. 已授权客户端快路径

credential 同时持有 `CloudRouteGrant` 和 `CloudEdgeLocator`。客户端分别校验 locator 结构和 grant envelope，再尝试 Edge。长期 grant 不绑定固定 locator；否则 Controller 认证 fallback 返回新 Edge 后，旧摘要会让每次连接永久回源。locator 只允许从已验证 pairing offer 或 Controller TLS Directory 响应写入 secure credential。

成功路径：

```text
credential -> cached route -> Edge TLS/HTTP2 -> ClientGateway
           -> daemon precheck -> WebRTC/DTLS -> capability auth -> Ready
```

只有完整 Ready 后才能提交新 locator。缓存公开位置写入失败只记录诊断，不改变已认证 session 的成功结果。

## 8. fallback 裁决

`ShouldRefreshEdgeLocator` 只接受两类错误：

1. Edge dial/TLS/HTTP2 ready 之前产生的内部 typed locator-unreachable error。
2. gRPC `NotFound`，表示目标位置在当前 Edge 不存在。

以下错误直接返回，禁止回源掩盖：

- unauthenticated / permission denied。
- daemon 实时拒绝。
- grant、proof、协议或 generation 错误。
- quota 和 Relay 拒绝。
- offer/answer、ICE、DTLS、DataChannel 或 terminal auth 错误。

客户端在 gRPC dial 后显式等待最多三秒 transport ready，只有该阶段失败才标成 locator 网络问题。建立 stream 后的 `Unavailable` 不自动解释为位置失效。

## 9. Relay 与“为什么续租”

Edge 知道 TCP/gRPC/TURN 连接是否活着，但“连接活着”不等于“仍有权消耗 Relay”。短租约用于约束：

- 订阅和账号策略的最大陈旧窗口。
- 最大字节数、速率和并发 allocation。
- 一个凭据只属于指定 account、Edge、daemon、client 和 session。
- 异常断连后资源最迟何时自动回收。

当前实现中，Edge 从 Controller 签名的 daemon binding 委托收缩生成五分钟租约。续租复用 lease ID，不访问 Controller，也不触发 ICE restart。

主动关闭 session 会立即关闭对应 allocation 并撤销租约，不等待五分钟。非正常断开则由 TURN 生命周期和租约到期共同兜底。binding 到期后不能续出更晚的租约。

## 10. Controller 离线矩阵

| 场景 | 能否工作 | 原因 |
| --- | --- | --- |
| 已连接 P2P DataChannel | 是 | 数据不经过 Edge/Controller |
| 已连接 Relay session | 是，至当前有效租约和可续委托边界 | Edge 本地转发和续租 |
| 已授权客户端新建 P2P | 是 | credential 有 locator 与 route grant |
| 已授权客户端新建 Relay | 是 | Edge 使用当前 daemon binding 委托 |
| 新 pairing | 是 | offer 自带 daemon 签名 locator 路由材料 |
| daemon 重连同一未重启 Edge | 是 | record 和 Edge 内存 key bundle 足够 |
| 无 locator 的客户端 | 否 | 没有可信入口，只能 Directory fallback |
| Edge 重启后 daemon admission | 否 | key bundle 未持久化，安全地拒绝 |
| 跨 Edge 自动迁移 | 否 | 当前没有签名 redirect/recovery 协议 |

## 11. 已完成的安全收口

- binding 和 route grant 全部使用确定性 protobuf、Ed25519 与独立签名 domain。
- binding 验 target Edge、validity、revision、device key、capability 和 locator digest。
- AgentGateway 与 ClientGateway 保存精确 generation，迟到 detach/close 不能影响新连接。
- binding 过期后，即使 writer 仍连接，`AuthenticatedAgentClaims` 也拒绝新 session。
- Edge state 对 Agent、session、pending signaling、Relay、mailbox 和 claim 设置硬上限。
- gRPC 单消息 1 MiB、并发 stream 256；SDP 和 candidate 在昂贵资源前校验。
- locator 必须是合法 host:port、SNI 和非空 CA；私有 CA 不混入系统信任根。
- Directory 先验 route grant 和 client proof，再返回实时 locator。
- claim 本体、CapabilityGrant、TURN credential、SDP/ICE 和 terminal/file payload 不进入 Controller 数据库或日志。

## 12. 上线前剩余门禁

这些不是旧协议兼容项，而是当前协议仍需补齐的安全和性能能力：

| 优先级 | 项目 | 风险/收益 |
| --- | --- | --- |
| P0 | Edge 先发单次 nonce，Agent/Client proof 覆盖完整安全相关 Hello | 当前 proof 的 session ID 由发起方选择，捕获包的重放保护不够强 |
| P0 | 把 Relay entitlement 从长 binding 拆为有硬到期的 Edge policy snapshot | Controller 离线时订阅撤销的收敛窗口目前等于 binding 剩余时间 |
| P0 | Edge 持久化已验证 verification keys 和 policy revision | Edge/Controller 同时重启时无法保持有限离线服务 |
| P1 | 签名 locator 增加 issued/refresh/expiry 和 revision 防回滚 | 当前签名覆盖完整 locator，但没有独立刷新窗口 |
| P1 | 结构化 Cloud error detail | 当前已避免宽泛 `Unavailable` fallback，但 `NotFound` 仍不够细 |
| P1 | Edge HTTP/2 connection pool | 当前每个 signaling session 建一个 ClientConn，增加 TLS RTT |
| P1 | fallback singleflight、负缓存和 Controller circuit breaker | Edge 故障时多个并发连接可能同时回源 |
| P1 | Happy Eyeballs 与多 endpoint locator | 改善 IPv6 黑洞和单入口故障时延 |

任何门禁实现都直接升级当前 schema 和测试，不增加旧版本分支。

## 13. 性能预算

| 场景 | P50 | P95 | 硬上限 |
| --- | ---: | ---: | ---: |
| 国内缓存 Edge，P2P | 700 ms | 2 s | 5 s |
| 国内缓存 Edge，Relay | 1 s | 3 s | 6 s |
| locator fallback，Controller 可达 | 2.5 s | 5 s | 8 s |
| Controller 被墙但缓存 Edge 可用 | 与正常路径一致 | 与正常路径一致 | 不受 Controller 影响 |
| Edge 不可用且 Controller 被墙 | 2 s 内开始明确失败 | 4 s | 5 s |

当前最直接的性能缺口是 ClientConn 复用和 fallback 合并。安全 nonce 会增加一个 Edge RTT，但该 RTT 在国内 Edge 上可控，并可与 ICE gathering 并行。

## 14. 验收

当前回归基线：

- `go test ./...` 全仓通过。
- `./scripts/check-generated-code.sh` 通过。
- public Controller 停止后，pairing、CLI/TUI P2P terminal I/O 通过。
- Controller 与 Directory 停止后，已缓存客户端可建立新的 UDP/TCP Relay session。
- Controller 恢复后，Edge usage outbox 清空且事件去重提交。
- binding/locator 篡改、错误 Edge、过期 claims 和 binding 缺少 locator digest 均被拒绝。
- proto descriptor contract 不包含已删除的开发期 RPC 和 payload。

上线门禁还必须补充：Hello 抓包重放、Edge/Controller 双重启、policy 硬到期、locator revision 回滚、连接池上限和跨境故障注入测试。
