# `termx-hub/` Agent Notes

## 定位

- `termx-hub` 是云端独立部署单元（systemd service），**不归并入 termx-cli**。
- 云服务器部署；termx-cli 在用户机器运行——部署模型、配置、生命周期完全独立。
- 所有 hub 产品逻辑在 `termx-remote/hub/`；`termx-hub/cmd` 只做环境解析与组装。

## Boundary

- `termx-hub` is the standalone Hub executable and deployment configuration wrapper.
- Hub product logic (registry, signaling, ICE, heartbeat) belongs in `termx-remote/hub/`.
- `termx-hub/cmd` may only: read environment variables, construct `termx-remote/hub` services, start HTTP server and cleanup loops.
- Hub must NOT call Web Controller to verify app certificates or connection tickets.
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

**Hub 不做的：**
- 验证 app certificate（这是 agent 的职责）
- 调用 Web Controller（hub 与 Web Controller 无运行时依赖）
- 保存任何 durable state
- 做任何 policy 或 quota 决策

## Configuration（云端部署）

Hub 启动只需要：
- 监听地址（TERMX_HUB_ADDR）
- ICE 配置（STUN/TURN server URLs、credentials）
- 可选：region 标识、heartbeat 间隔、registry TTL、rate limit 参数

**不需要**：TERMX_HUB_CONTROL_URL、TERMX_HUB_CONTROL_SECRET（已删除）

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
- Hub が Web Controller をランタイムで呼び出していないか
- Registry/signaling map に unbounded な状態がなく TTL cleanup があるか
- Rate limit と backpressure の動作がテストされているか
