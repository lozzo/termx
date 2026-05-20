# termx-remote Agent Rules

> 长上下文注意：本文件规则始终有效。执行前确认未违反 M1-M7（见根 AGENTS.md）。

---

## 0. 当前任务

正在执行 Remote 架构重构。状态见根目录 `workflow.md`。

---

## 1. 模块绝对禁止

| # | 禁止行为 |
|---|---------|
| R1 | 复活 `cert/` 包（AppCertificate、ed25519 machine key） |
| R2 | 复活 `session/rtc/offer_signature.go` |
| R3 | 复活 `identity.MachineKey` |
| R4 | Hub 做任何认证决策（hub/httpapi、hub/grpcapi 均不验证 session token 内容） |
| R5 | local 与 cloud/online 使用不同的 offer 处理代码路径 |

---

## 2. 包状态速查

| 包 | 状态 | 说明 |
|----|------|------|
| `session/token` | ✅ 新增 | HMAC-SHA256 Issue/Verify |
| `identity` | ✅ 已改 | 只保留 MachineSecret（32字节）和 DeviceIdentity |
| `cert/` | ❌ 已删除 | 不得复活 |
| `session/rtc/offer_signature` | ❌ 已删除 | 不得复活 |
| `protocol/hubgrpc` | ✅ 新增 | protobuf 生成，gRPC agent-hub 协议 |
| `hub/grpcapi` | ✅ 新增 | Hub gRPC server |
| `hub/httpapi` | ✅ 保留 | browser HTTP 信令，session_token 替代 AppCertificate |
| `hub/cloud` | ✅ 保留 | SessionToken 替代 AppCertificate/Signature |
| `hub/registry` | ✅ 保留 | SessionToken 替代 AppCertificate/Signature |
| `discovery` | ✅ 扩展 | 新增 latency.go + grpc_hub_client.go |
| `pairing` | ✅ 已改 | token.Issue 替代 cert.SignAppCertificate |
| `config` | ✅ 已改 | 新增 Mode/AllowLAN/LANIPs |
| `hub/controlclient` | ❌ 已删除 | 不得复活 |
| `localweb` | ❌ 已删除 | 不得复活 |

---

## 3. 鉴权规则

```
session_token 格式:
  payload_b64 = base64url(JSON{sid, mid, cap[], iat, exp})
  mac_b64     = base64url(HMAC-SHA256(machineSecret, "termx-session-v1:" + payload_b64))
  token       = payload_b64 + "." + mac_b64

Agent 验证:
  1. token.Verify(tok, machineSecret, now)  ← hmac.Equal()，常量时间
  2. claims.MachineID == m.cfg.MachineID
  3. claims.ExpiresAt > now.Unix()
  4. DTLS 防重放（Pion 自动，无需代码）
```

---

## 4. gRPC 协议规则

- Agent→Hub 首条消息必须是 `RegisterRequest`
- Hub 回 `RegisterResponse`（session_id, ICE servers, heartbeat interval）
- 心跳间隔来自 RegisterResponse，默认 30s
- 断线：指数退避重连（1s → 60s 上限）
- online 模式走 gRPC；local 模式 agent 注册走 HTTP（保留）

---

## 5. Service.go cmux 分流顺序

```go
grpcListener := mux.Match(cmux.HTTP2())      // 必须在 HTTP1 之前
httpListener := mux.Match(cmux.HTTP1Fast())
iceListener  := mux.Match(cmux.Any())
```

---

## 6. Review Checklist

每次 PR/切片完成后检查：

- [ ] `go test -race ./...` 全通过
- [ ] `cert/` 目录不存在
- [ ] `session/rtc/offer_signature.go` 不存在
- [ ] `hub/httpapi/handler.go` 无 AppCertificate / OfferSignature 字段
- [ ] `hub/grpcapi/server.go` 只做格式验证（Bearer 非空），不做 HMAC 验证
- [ ] `pairing/session.go` 使用 `token.Issue`，不使用 `cert.SignAppCertificate`
- [ ] `agent/runtime/manager.go` 使用 `verifySessionToken`，无 `authorizeCloudOffer`
- [ ] Hub heartbeat 是异步 goroutine，不阻塞 offer/answer
