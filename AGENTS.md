# TermX Remote Build Agent Notes

以下为不变的架构约束：

完成可用的 remote 产品主链路：

- `termx-remote/hub`：dumb relay，无数据库，无认证决策，纯信令转发；云端部署时允许管理面 heartbeat
- `termx-remote/agent`：连接 hub、验证 app certificate、控制谁能连入
- `termx-cli`：装配 local/cloud/both 三种运行模式
- `remote-ui`：browser 客户端，统一 hub/signaling/session 流程
- Web Controller：做 hub/agent 目录、账号/订阅/踢下线控制面，不做连接时认证或 runtime 代理

## Product Model

### Hub（dumb relay）

- Hub 是纯信令中继，不做任何认证决策。
- Hub 不调用 Web Controller 验证 app certificate 或连接票据。
- Hub 允许周期性向 Web Controller 做**管理面 heartbeat**：注册自身在线状态、汇报当前在线 machine/agent 列表、汇报 relay/TURN 流量、接收 `kick_agents` 指令。
- Hub 管理面 heartbeat 不得参与单次连接认证、offer/answer 审核、app certificate 验证或连接票据验证。
- Hub 只存储有 TTL 的短时内存状态：online agents、pending offers/answers、pairing claims/results。
- Hub 没有数据库，没有 durable state。
- Hub 不区分 local 和 cloud 部署——代码完全相同，只是运行地址不同。

### Web Controller（agent 目录）

- Web Controller 只做：用户登录、hub 目录、列出该用户注册的 agent、返回每个 agent 所在的 hub_url、订阅/配额/踢下线控制面。
- Web Controller **不做**：连接时 cert 验证、policy 决策、offer/answer 审核、runtime 代理。
- 用户登录 App 后，App 从 Web Controller 拿到 agent 列表和 hub_url，后续直接连 Hub，不再回调 Web Controller。

### Agent（认证决策方）

- Agent 在收到 offer 时验证 app certificate（签名、有效期、machine_id）。
- Agent 是唯一的认证决策方；Hub 只是转发 offer，不审核内容。
- Agent 用 app cert 公钥参与 DataChannel 密钥协商，在 DTLS 之上提供应用层 E2E 加密。

### 三种运行模式

| 模式 | 运行内容 | hub_url 来源 |
|------|----------|-------------|
| `local` | 进程内嵌入 hub（cmux: HTTP + ICE-TCP，LAN 暴露） | 本机 LAN IP:port |
| `cloud` | agent 连接云端 hub | 云端 hub 地址 |
| `both` | 并行：本地嵌入 hub + agent 同时注册云端 hub | 两个都有 |

- `both` 模式使用**一个** `runtime.Manager`，`hubURLs []string` 持两个地址。
- 认证逻辑在 agent 侧，与 hub 是 local 还是 cloud 无关，代码路径完全一致。

