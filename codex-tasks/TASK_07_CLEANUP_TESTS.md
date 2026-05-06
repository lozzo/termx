# TASK_07 — 清理与测试

**Wave**: 4（所有其他任务完成后执行）  
**验证**: `cd termx-remote && go test -race ./... && cd ../termx-cli && go build ./... && cd ../remote-ui && npm run build`

---

## 07-A: 删除废弃文件

```bash
rm -rf termx-remote/cert/
rm -f  termx-remote/session/rtc/offer_signature.go
rm -f  termx-remote/session/rtc/offer_signature_test.go
```

---

## 07-B: 修复因删除导致的编译错误

执行 `cd termx-remote && go build ./...`，找到所有因以下类型被删除而产生的编译错误：

- `cert.AppCertificateEnvelope`
- `cert.AppCertificatePayload`
- `cert.SignAppCertificate`
- `cert.VerifyAppCertificate`
- `cert.ReplayWindow`
- `identity.MachineKey`
- `identity.LoadOrCreateMachineKey`
- `identity.MachinePublicKeyFingerprint`
- `identity.PublicKeyString`
- `rtc.OfferSignature`（session/rtc 包中）
- `rtc.VerifyOfferSignature`
- `rtc.CanonicalOfferSignatureMessage`
- `hubv1.OfferSignature`
- `hubv1.AgentRegistrationSignature`

**处理规则**：
- 若编译错误在**测试文件**中：更新测试，删除对应断言，改为测试新行为
- 若编译错误在**实现文件**中：说明 TASK_05/06 有遗漏，补充完成对应步骤

---

## 07-C: 更新测试文件

### `termx-remote/agent/runtime/cert_verify_test.go`

若此文件主要内容是测试 AppCertificate 验证流程，**整个文件删除**。

新建 `termx-remote/agent/runtime/session_token_verify_test.go`，测试新的 `verifySessionToken` 方法：

```go
package runtime_test

// 测试场景：
// 1. 正常 token → 验证通过
// 2. 过期 token → 返回错误
// 3. 错误 machine_id → 返回错误
// 4. 篡改 token → 返回错误
// 5. 空 token → 返回错误
```

### `termx-remote/agent/runtime/hub_registration_test.go`

删除以下断言：
- 检查 `Signature.Algorithm`、`Signature.Nonce`、`Signature.Timestamp`、`Signature.Value` 的断言
- 检查 `MachinePublicKey` 的断言

保留：其余注册字段（AgentID、DeviceID、MachineID 等）的正常测试。

### `termx-remote/hub/httpapi/sessions_test.go`

删除：`app_certificate`、`signature` 请求/响应字段断言

新增：
- `session_token` 为空时 → 400 Bad Request
- `session_token` 非空时 → 正常处理（若有 mock）

### `termx-remote/hub/httpapi/agents_test.go`

删除：注册请求中 `signature` 字段相关断言。

### `termx-remote/pairing/session_test.go`

删除：`ClaimResponse.AppCertificate`、`ClaimResponse.MachinePublicKey` 断言

新增：
```go
// 验证 session_token 非空且可被 token.Verify 验证
resp, err := session.Claim(req)
if err != nil { t.Fatal(err) }
if resp.SessionToken == "" { t.Fatal("session_token is empty") }
claims, err := token.Verify(resp.SessionToken, machineSecret, time.Now())
if err != nil { t.Fatal(err) }
if claims.MachineID != cfg.MachineID { t.Fatalf("machine_id mismatch") }
```

### `termx-remote/identity/identity_test.go`

删除：`LoadOrCreateMachineKey`、`MachineKey.Sign`、`MachinePublicKeyFingerprint` 相关测试用例。

保留：`LoadOrCreate`（DeviceIdentity）相关测试。

### `termx-remote/hub_boundary_test.go` 和 `service_local_hub_test.go`

删除：`AppCertificate`、`OfferSignature`、`MachineKey` 相关引用，替换为 `SessionToken`。

---

## 07-D: 全量验证

按顺序执行，每条命令必须零错误：

```bash
# 1. Go 全量编译
cd termx-remote && go build ./...

# 2. Go 全量测试（race detector）
cd termx-remote && go test -race ./...

# 3. CLI 编译
cd ../termx-cli && go build ./...

# 4. Frontend 类型检查
cd ../remote-ui && npm run build
```

若有失败：
- 编译失败 → 找到根因，修复，重新执行步骤 1
- 测试失败 → 检查是测试逻辑错误还是实现错误，修复后重新执行步骤 2
- Frontend 类型错误 → 检查 TASK_04 的接口是否完整实现，修复后执行步骤 4

---

## 07-E: 最终清单确认

完成后检查以下文件/目录**不存在**：

```bash
# 应当不存在
test -e termx-remote/cert/                         && echo "FAIL: cert/ still exists"
test -e termx-remote/session/rtc/offer_signature.go && echo "FAIL: offer_signature.go still exists"

# 应当存在
test -f termx-remote/session/token/token.go         || echo "FAIL: token.go missing"
test -f termx-remote/identity/machine_secret.go     || echo "FAIL: machine_secret.go missing"
test -f termx-remote/protocol/hubgrpc/hub.pb.go     || echo "FAIL: hub.pb.go missing"
test -f termx-remote/hub/grpcapi/server.go          || echo "FAIL: grpc server missing"
test -f termx-remote/discovery/grpc_hub_client.go   || echo "FAIL: grpc client missing"
test -f termx-remote/discovery/latency.go           || echo "FAIL: latency.go missing"
test -f termx-remote/hub/httpapi/middleware_lan.go  || echo "FAIL: lan middleware missing"
```

所有检查输出应为空（无 FAIL 行）。
