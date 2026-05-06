# TermX Agent Rules

> 长上下文注意：本文件的规则在任何时候都有效。每次工具调用前先确认未违反下列约束。

---

## 0. 当前任务状态

**正在执行**：Remote 架构重构（见 `workflow.md` 当前状态）  
**任务文档**：`CODEX_REMOTE_REARCHITECTURE.md`（orchestrator）+ `codex-tasks/TASK_0[1-7]_*.md`  
**恢复入口**：session 断开后，读 `workflow.md` → 从 `current_wave` 和未完成任务继续

---

## 1. 绝对禁止（MUST NOT）

| # | 禁止行为 |
|---|---------|
| M1 | Hub 验证 app certificate 或 session token——这是 Agent 的职责 |
| M2 | Hub 在连接时调用 Web Controller 做认证/policy 决策 |
| M3 | Hub 引入 durable state / 数据库 / migration |
| M4 | Runtime 数据（terminal/file/api/events）走 HTTP 或 WebSocket——必须走 WebRTC DataChannel |
| M5 | termx-remote 依赖 termx-hub、termx-cli 或 remote-ui |
| M6 | 生成或打印 machine_secret、TURN secret、私钥等敏感材料 |
| M7 | 复活已删除的 `cert/`、`offer_signature.go`、`identity.MachineKey` |

---

## 2. 架构约束（快速参考）

### Hub（dumb relay）
- 纯信令中继：转发 offer/answer、pairing claim，不审核内容
- 内存状态，有 TTL，无持久化
- 管理面 heartbeat（异步，周期性）：向 Web Controller 上报 hub 在线、agent 列表、relay 流量，接收 kick 指令
- heartbeat 不得阻塞或参与连接认证

### Agent（认证决策方）
- 验证 HMAC session_token：`token.Verify(tok, machineSecret, now)` → 检查 MAC + 过期 + machine_id
- **不再有** AppCertificate、ed25519 per-offer 签名、ReplayWindow
- DTLS（Pion 自动）提供传输层防重放

### 三种模式

| mode | 本地 hub | online hub |
|------|---------|-----------|
| `local` | 内嵌启动（cmux: HTTP/2+HTTP/1+ICE-TCP） | 否 |
| `online` | 否 | gRPC stream 长连接 |
| `both` | 是 | 是 |

### LAN 过滤
- `allow_lan=false` → 仅 loopback
- `allow_lan=true, lan_ips 空` → 所有私有 IP
- `allow_lan=true, lan_ips 非空` → CIDR 白名单

### Web Controller
- 职责：用户登录、hub 目录、agent 列表 + hub_urls
- 不做：连接时认证、offer 审核、runtime 代理

---

## 3. 配置模型

```yaml
remote:
  enable: true
  mode: both          # local | online | both
  token: "xxxxxx"     # online 模式必须
  allow_lan: true
  lan_ips: ["192.168.0.0/16"]
```

CLI：`termx remote enable --mode both --token xxxxxx`

---

## 4. TDD 规则

- 每个切片 TDD 推进
- 切片完成后独立 subagent review
- `go test ./...` 必须保持全通过
- 新发现问题写入 `workflow.md` 的 Blockers 章节

---

## 5. 包边界

```
termx-remote  ← 唯一 remote owner
  依赖: termx-core/clientapi（只 public API）
  禁止依赖: termx-hub、termx-cli、remote-ui

termx-hub     ← 云端部署单元
  hub 产品逻辑在 termx-remote/hub/
  cmd/ 只做环境解析和组装

termx-cli     ← 产品壳
  依赖: termx-core + termx-remote public package
  不在 CLI 内实现 hub 逻辑、cert 验证、TURN relay

remote-ui     ← Web UI
  不反向定义 termx-core / termx-remote 边界
```
