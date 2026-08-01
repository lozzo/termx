# Cloud daemon 生命周期设计

## 目标

Cloud daemon 有三个持久状态：

- `ACTIVE`：允许注册到 Edge，允许新的 Cloud 客户端连接。
- `BLOCKED`：可恢复。拒绝新连接，断开现有 Cloud 客户端和 Relay，但保留 daemon 与 Edge 的 Agent 连接，以便立即恢复。
- `DELETED`：不可恢复。拒绝所有旧 Cloud 凭据，通知 daemon 删除 Cloud enrollment 和 Cloud issuer，并断开 Agent。Direct、SSH 和本地访问不受影响；再次使用 Cloud 必须重新注册并生成新的 daemon identity 和 binding。

`DELETED` 是终态。数据库保留墓碑用于拒绝旧凭据和审计，但普通用户设备列表不再展示它。

## 数据归属

### Web Controller 持久化

Controller 只持久化账号拥有关系和生命周期真值：

- daemon identity、账号、设备公钥和指纹；
- `state`、`state_revision`；
- 创建/更新时间和状态变更审计。

Controller **不持久化**：

- `binding_id`；
- owning Edge；
- Agent connection、boot ID、generation；
- Directory 中的实时 Client session、Presence 和 owning Edge 投影。

当前在线 Edge 只用于网页展示时，可以从 Controller Directory 的内存 Presence 投影获得；它不是持久归属关系。

Relay 计费是独立边界：Controller 会持久化 reservation 的 `edge_id`、`daemon_id`、`client_id`、`session_id` 和授权快照，用于限额、结算与审计。这些记录不是 daemon owning Edge 的权威映射，也不参与生命周期路由。

### Edge 内存

Edge 负责持有运行时事实：

- 已验签 binding 对应的 daemon 和目标 Edge；
- daemon 到当前 Agent generation 的映射；
- 当前 Client session、信令和 Relay；
- Controller 下发的 daemon 生命周期状态表。

Signed binding 仍然包含 `binding_id` 和目标 `edge_id`，因为它是 daemon 连接 Edge 时提交的凭据。这个信息由 Edge 验签和使用，不需要复制到 Controller 数据库。

### Daemon 本地

Daemon 只管理自己的 Cloud enrollment、Cloud issuer 和当前 Cloud peer。删除 Cloud enrollment 时不得删除 DeviceIdentity、Direct/SSH issuer、本地授权或 terminal 数据。

## 状态变更流程

1. 网页提交目标状态和当前 `state_revision`。
2. Controller 在数据库事务内校验账号拥有关系、合法状态转换和 revision，然后更新状态并记录审计。
3. 事务提交后，Controller 把完整的 `DaemonStateRecord` 广播给所有已认证且启用的在线 Edge，不查找也不持久化 owning Edge。
4. 每个 Edge 更新内存状态。没有该 daemon 的 Edge 只缓存状态；持有该 daemon Agent 的 Edge 执行收敛动作。
5. Edge 给 daemon 发送带 Agent generation 的生命周期命令。旧 generation 的结果不得影响新连接。

广播是幂等的完整替换，`state_revision` 用于丢弃旧消息和识别同 revision 冲突。

## Edge 收敛行为

### ACTIVE

- Agent 接入时先验签 binding，再取得 Controller 生命周期真值。
- Edge 先把 `ACTIVE` 命令发给 daemon；daemon 确认后才发布 Presence 和接受客户端。
- 从 `BLOCKED` 恢复时复用现有 Agent 连接，不要求 daemon 手动重连。

### BLOCKED

- 立即停止新 ClientGateway、信令和 Relay 准入；
- 从业务 Presence 中隐藏 daemon；
- 关闭现有 Cloud Client session 和 Relay；
- 通知 daemon 关闭 Cloud peer；
- 保留 Agent 连接，等待恢复。

### DELETED

- 先执行与 `BLOCKED` 相同的阻断和清理；
- 通知 daemon 删除本地 Cloud enrollment 和 Cloud issuer；
- daemon 确认后断开 Agent；未确认时 Edge 也必须在截止时间后强制断开；
- 旧 binding、RouteGrant 和 pairing 数据永久失效。

## 断线和重启

- Edge 建立 Controller control stream 时，先接收完整 daemon 状态快照，再开放 Agent/Client 准入。
- Controller 在发送快照前读取 Edge 的持久 `enabled` 状态；停用 Edge 不进入 ready 状态，已有 control generation 会立即失效。
- Edge control stream 断开后清空生命周期缓存并 fail closed；重连后以 Controller 快照重新建立真值。
- 如果 Agent 接入时 Edge 缺少该 daemon 状态，Edge 通过现有 control stream 向 Controller 定向查询；查询失败时拒绝接入。
- Daemon 断开后，Edge 清理该 generation 的 Presence、session 和信令。它再次连接时重新按 Controller 状态收敛。
- 状态已提交但广播目标 Edge 离线时不做额外持久队列；Edge 重连的完整快照负责收敛。
- 在线 Edge 的广播队列失步时关闭该 control generation，强制重连加载快照，避免继续使用已知过期状态。

## 安全边界

- 生命周期检查不能替代 binding、RouteGrant、client proof 或 pairing secret 的认证。
- 未通过客户端授权前，不返回精确的 `BLOCKED`/`DELETED` 状态，避免 daemon identity 枚举。
- 所有缺少状态、快照未就绪、Controller 不可查询的路径均拒绝新的 Cloud 准入。
- `state_revision` 和 Agent generation 分别防止持久状态乱序和运行时连接竞态。

## 明确不做

- 不在 Controller 建立 daemon 到 Edge 的持久映射；
- 不为广播增加每台 Edge 的持久消息队列；
- 不为旧生命周期协议增加兼容分支；
- 不把 Cloud 删除扩大为设备身份或 Direct/SSH 数据删除；
- 不提前引入分布式一致性组件。当前模型由数据库状态、广播、Edge 快照和 fail-closed 查询完成收敛。
