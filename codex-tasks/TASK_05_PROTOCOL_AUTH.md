# TASK_05 — Protocol 类型与鉴权简化

**Wave**: 2（依赖 TASK_01 完成，TASK_01 验证通过后立即启动）  
**验证**: `cd termx-remote && go build ./protocol/... ./hub/... ./pairing/...`

---

## 执行顺序（内部串行）

```
05-A: protocol/hubv1/hub.go
05-B: hub/cloud/types.go      （依赖 05-A）
05-C: hub/registry/types.go   （依赖 05-A，可与 05-B 同时）
05-D: hub/cloud/service.go    （依赖 05-B + 05-C）
05-E: hub/httpapi/handler.go  （依赖 05-B + 05-C）
05-F: pairing/session.go      （依赖 TASK_01 的 session/token 包）
05-G: identity/identity.go    （删除 MachineKey）
```

---

## 05-A: 修改 `termx-remote/protocol/hubv1/hub.go`

### 删除

- `OfferSignature` struct
- `AgentRegistrationSignature` struct
- `Signature` 字段（从 `AgentRegistrationRequest`）
- `CanonicalAgentRegistrationSignatureMessage` 函数（如在此文件）

### 修改 `SignalingOffer`

```go
// 旧
type SignalingOffer struct {
    SessionID      string          `json:"session_id"`
    MachineID      string          `json:"machine_id"`
    TerminalID     string          `json:"terminal_id,omitempty"`
    SDP            string          `json:"sdp"`
    Candidates     []string        `json:"ice_candidates,omitempty"`
    AppCertificate json.RawMessage `json:"app_certificate,omitempty"`
    Signature      OfferSignature  `json:"signature,omitempty"`
}

// 新
type SignalingOffer struct {
    SessionID    string   `json:"session_id"`
    MachineID    string   `json:"machine_id"`
    TerminalID   string   `json:"terminal_id,omitempty"`
    SDP          string   `json:"sdp"`
    Candidates   []string `json:"ice_candidates,omitempty"`
    SessionToken string   `json:"session_token"`
}
```

### 其他含 AppCertificate 的 struct

在 `hubv1` 包中全局搜索 `AppCertificate json.RawMessage`，每处替换为 `SessionToken string \`json:"session_token"\``。
删除对应的 `Signature OfferSignature` 字段。

---

## 05-B: 修改 `termx-remote/hub/cloud/types.go`

全文搜索以下模式并替换：

| 旧内容 | 新内容 |
|--------|--------|
| `AppCertificate json.RawMessage` | `SessionToken string` |
| `Signature      OfferSignature`  | （删除整行） |
| `OfferSignature` struct 定义 | （删除整个 struct） |

删除与 `OfferSignature` 相关的所有字段和方法。

---

## 05-C: 修改 `termx-remote/hub/registry/types.go`

全文搜索以下模式并替换：

| 旧内容 | 新内容 |
|--------|--------|
| `AppCertificate json.RawMessage` | `SessionToken string` |
| `Signature      OfferSignature`  | （删除整行） |
| `SignatureAlgorithm string`      | （删除） |
| `SignatureNonce     string`      | （删除） |
| `SignatureTimestamp int64`       | （删除） |
| `SignatureValue     string`      | （删除） |
| `OfferSignature` struct 定义 | （删除整个 struct） |

---

## 05-D: 修改 `termx-remote/hub/cloud/service.go`

1. 删除 `registrySignature()` 函数
2. 删除 `cloudSignature()` 函数
3. 在所有 offer 类型转换处（旧代码拷贝 `AppCertificate` 和 `Signature`），替换为：

```go
// 旧
AppCertificate: cloneBytes(in.AppCertificate),
Signature:      registrySignature(in.Signature),

// 新
SessionToken: in.SessionToken,
```

---

## 05-E: 修改 `termx-remote/hub/httpapi/handler.go`

### 删除

- `offerSignatureRequest` struct（约 L873）
- `validateSessionRequestEnvelope()` 函数（约 L880）

### 修改 session 创建 request struct（约 L480 附近匿名 struct）

找到含 `AppCertificate json.RawMessage` 和 `Signature offerSignatureRequest` 的 struct，替换为：

```go
var req struct {
    ConnectTicket string `json:"connect_ticket"`
    MachineID     string `json:"machine_id"`
    TerminalID    string `json:"terminal_id,omitempty"`
    Offer         struct {
        SessionID  string   `json:"session_id"`
        SDP        string   `json:"sdp"`
        Candidates []string `json:"ice_candidates,omitempty"`
    } `json:"offer"`
    SessionToken string `json:"session_token"`
}
```

删除调用 `validateSessionRequestEnvelope()` 的代码，替换为：

```go
if strings.TrimSpace(req.SessionToken) == "" {
    writeError(w, http.StatusBadRequest, "session_token_required", "session_token is required")
    return
}
```

构建 cloud submit input 时：`SessionToken: req.SessionToken`（删除 `AppCertificate` 和 `Signature` 字段）

### 修改 pairing result 响应（约 L460）

```go
// 旧
"app_certificate": result.AppCertificate,
// 新
"session_token": result.SessionToken,
```

### 修改 agent 注册处理（约 L187）

删除读取 `req.Signature` 字段并写入 registry 的代码块：

```go
// 删除类似如下代码：
SignatureAlgorithm: req.Signature.Algorithm,
SignatureNonce:     req.Signature.Nonce,
SignatureTimestamp: req.Signature.Timestamp,
SignatureValue:     req.Signature.Value,
```

删除相关 `// TODO: verify agent registration signature` 注释。

---

## 05-F: 修改 `termx-remote/pairing/session.go`

### 删除 imports

```go
"github.com/lozzow/termx/termx-remote/cert"
"github.com/lozzow/termx/termx-remote/identity"  // 若只用于 MachineKey
```

### 新增 import

```go
"github.com/lozzow/termx/termx-remote/session/token"
```

### 修改 `Config` struct

```go
// 旧
type Config struct {
    MachineID    string
    MachineName  string
    MachineKey   identity.MachineKey   // 删除
    LocalPairURL string
    Now          func() time.Time
}

// 新
type Config struct {
    MachineID       string
    MachineName     string
    MachineSecret   []byte            // 新增
    DefaultTokenTTL time.Duration     // 新增，默认 365*24*time.Hour
    LocalPairURL    string
    Now             func() time.Time
}
```

### 修改 `ClaimRequest` struct

删除：`AppPublicKey string`、`AppName string`

### 修改 `ClaimResponse` struct

```go
// 旧
type ClaimResponse struct {
    MachineID        string
    MachineName      string
    MachinePublicKey string                       // 删除
    AppCertificate   cert.AppCertificateEnvelope  // 删除
}

// 新
type ClaimResponse struct {
    MachineID    string    `json:"machine_id"`
    MachineName  string    `json:"machine_name"`
    SessionToken string    `json:"session_token"`
    ExpiresAt    time.Time `json:"expires_at"`
}
```

### 修改 `Claim()` 方法内部

找到调用 `cert.SignAppCertificate(...)` 的代码块，替换为：

```go
now := s.cfg.Now()
if now.IsZero() { now = time.Now().UTC() }
ttl := s.cfg.DefaultTokenTTL
if ttl <= 0 { ttl = 365 * 24 * time.Hour }
expiresAt := now.Add(ttl)

tok, err := token.Issue(s.cfg.MachineSecret, token.Claims{
    SessionID:    s.session.PairSessionID,
    MachineID:    s.cfg.MachineID,
    Capabilities: req.RequestedCapabilities,
    IssuedAt:     now.Unix(),
    ExpiresAt:    expiresAt.Unix(),
})
if err != nil { return ClaimResponse{}, fmt.Errorf("issue token: %w", err) }

return ClaimResponse{
    MachineID:    s.cfg.MachineID,
    MachineName:  s.cfg.MachineName,
    SessionToken: tok,
    ExpiresAt:    expiresAt,
}, nil
```

---

## 05-G: 修改 `termx-remote/identity/identity.go`

删除以下所有内容（不影响 DeviceIdentity 和 LoadOrCreate）：

- `MachineKey` struct 和所有方法（Sign、String、GoString）
- `machineKeyFile` struct
- `const MachineKeyFilename = "machine_key"`
- `LoadOrCreateMachineKey()` 函数
- `MachinePublicKeyFingerprint()` 函数
- `PublicKeyString()` 函数
- `decodeMachineKey()` 函数
- `persistMachineKey()` 函数
- 仅供上述函数使用的 imports：`crypto/ed25519`、`crypto/sha256`、`encoding/hex`
