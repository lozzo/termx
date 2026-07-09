# `termx-hub/` Agent Notes

## 定位

- `termx-hub` 是云端独立部署单元（systemd service），**不归并入 termx-cli**。
- 云服务器部署；termx-cli 在用户机器运行——部署模型、配置、生命周期完全独立。
- 所有 hub 产品逻辑在 `termx-hub/internal/hub/`；`termx-hub/cmd` 只做环境解析与组装。

## Boundary

- `termx-hub` is the standalone Hub executable and deployment configuration wrapper.
- Hub product logic (registry, signaling, ICE, heartbeat) belongs in `termx-hub/internal/hub/`.
- `termx-hub/cmd` may only: read environment variables, construct `termx-hub/internal/hub` services, start HTTP server and cleanup loops.
- Hub must NOT call Web Controller/control-plane service to verify app certificates, session tokens, connection tickets, offers, or answers.
- Hub may call Web Controller/control-plane service only through periodic management-plane heartbeat for hub registration, online agent list, relay traffic reporting, and forced disconnect commands.
- Hub must NOT make any authentication decisions. It is a dumb relay.
- Hub must NOT be a terminal/file/api/events HTTP or WebSocket runtime proxy.
- Runtime data path is always WebRTC DataChannel (never HTTP/WebSocket).

## Hub Role（dumb relay）

Hub 是纯信令中继，职责边界：

**Hub 做的：**
- 接受 agent 注册（register/heartbeat）
- 存储 pending offers/answers（有 TTL，内存）
- 转发 offer 给对应 agent（poll/answer）
- 处理 pairing claims（中转）
- 提供 ICE 配置（STUN/TURN URLs）
- 云端部署时周期性向 Web Controller/control-plane 服务汇报管理面状态：hub 在线、online agents、relay/TURN 流量
- 接收 Web Controller/control-plane 服务返回的 `kick_agents`，并在本地 registry 标记 forced offline

**Hub 不做的：**
- 验证 app certificate（这是 agent 的职责）
- 连接时调用 Web Controller/control-plane 服务验证 app certificate、session token、ticket、offer/answer 或做 per-session policy 决策
- 保存任何 durable state
- 在 Hub 内做任何用户级 policy 或 quota 决策（只能执行 Web Controller/control-plane 管理面返回的 forced disconnect / rate metadata）

## Current P0 Task（WF-503）

更新 `termx-hub/deploy/termx-hub.env.example`，补充缺失字段（带注释）：

```
# Web Controller / control-plane URL（web-control 管理面服务地址）
TERMX_HUB_CONTROL_URL=http://localhost:3000

# Hub 与 Web Controller/control-plane 服务之间的共享密钥（必须与 web-control 的 HUB_SECRET 一致）
TERMX_HUB_CONTROL_SECRET=termx-development-hub-secret-change-me
```

验证：`go run ./cmd/termx-hub` 启动后 `/api/health` 返回 ok，heartbeat log 不报 401/403。

## Configuration（云端部署）

Hub 启动只需要：
- 监听地址（TERMX_HUB_ADDR）
- ICE 配置（STUN/TURN server URLs、credentials）
- 可选：region 标识、heartbeat 间隔、registry TTL、rate limit 参数
- 云端管理面 heartbeat 配置：TERMX_HUB_CONTROL_URL、TERMX_HUB_CONTROL_SECRET、TERMX_HUB_PUBLIC_HTTP_URL、TERMX_HUB_NAME、TERMX_HUB_REGION、TERMX_HUB_MAX_AGENTS

**env var 完整说明**（`deploy/termx-hub.env.example` 必须包含所有字段）：

| 变量 | 必需 | 说明 |
|------|------|------|
| `TERMX_HUB_ADDR` | 否 | 监听地址，默认 `127.0.0.1:8447` |
| `TERMX_HUB_ID` | 否 | Hub 唯一 ID，默认 hostname；同一 Web Controller/control-plane 下多 hub 需不同 ID |
| `TERMX_HUB_NAME` | 否 | 展示名 |
| `TERMX_HUB_REGION` | 否 | 区域标识（如 `cn`、`us-west`） |
| `TERMX_HUB_PUBLIC_HTTP_URL` | heartbeat 需要 | hub 对外 HTTP 地址，heartbeat 上报给 Web Controller/control-plane 后，browser 凭此直连 hub |
| `TERMX_HUB_CONTROL_URL` | heartbeat 需要 | Web Controller/control-plane 地址 |
| `TERMX_HUB_CONTROL_SECRET` | heartbeat 需要 | 与 web-control `HUB_SECRET` 一致 |
| `TERMX_HUB_STUN_SERVERS` | 否 | 逗号分隔 STUN URL，如 `stun:stun.l.google.com:19302` |
| `TERMX_HUB_TURN_SECRET` | 否 | 启用内嵌 TURN 的密钥 |
| `TERMX_HUB_TURN_ADDR` | 否 | 内嵌 TURN 监听地址，默认 `0.0.0.0:3478` |
| `TERMX_HUB_TURN_PUBLIC_IP` | TURN 需要 | TURN 服务器公网 IP（使用 0.0.0.0 时必填） |
| `TERMX_HUB_HEARTBEAT_INTERVAL` | 否 | 心跳间隔，默认 `1m` |
| `TERMX_HUB_MAX_AGENTS` | 否 | 最大在线 agent 数 |

**禁止**：任何用于连接时 cert/ticket/offer 验证的 Web Controller/control-plane 配置或 control verifier。

## Build Rules

- Follow root `AGENTS.md` and root `workflow.md` for TDD, subagent review, and workflow tracking.
- Every slice must update `workflow.md` before and after implementation.
- Tests first, then minimal implementation.

## Transport Policy

- Client-visible connection paths: `local`, `public_p2p`, `managed` only.
- Relay is not a fourth path. It appears only as capability/policy/accounting metadata.
- `public_p2p`: STUN/rendezvous only, no TURN credentials.
- `managed`: may include TURN credentials in ICE config.

## Review Focus

- Hub httpapi に controlclient の呼び出しが残っていないか（残ってはいけない）
- Hub に durable state / DB / migration がないか
- Hub が接続時認証のために Web Controller/control-plane サービスを呼び出していないか（管理面 heartbeat は許可）
- 管理面 heartbeat が hub/httpapi の offer/answer request path に入っていないか
- Registry/signaling map に unbounded な状態がなく TTL cleanup があるか
- Rate limit と backpressure の動作がテストされているか
