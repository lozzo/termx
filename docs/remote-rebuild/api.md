# API 草案

状态：第三版草案。已纳入本地 embedded web 优先、匿名 P2P、公共 STUN 和订阅 relay。字段和路径后续实现前可以调整，但对象模型必须保持 `machine -> terminal`。

## 公共约定

### ID

- `user_id`: web control 用户 ID
- `machine_id`: 一台运行 `termx daemon` 的机器
- `terminal_id`: `termx-core` terminal ID
- `hub_id`: hub/TURN 边缘节点 ID
- `channel_id`: anonymous rendezvous channel ID
- `ticket_id`: 一次连接授权 ID
- `session_id`: 一次 signaling/WebRTC 会话 ID

### Auth

- App/Web 用户 API：`Authorization: Bearer <user_access_token>` 或 web cookie session
- TermX daemon 到 web control：`Authorization: Bearer <agent_access_token>`
- Hub 到 web control：`X-Hub-ID` + `X-Hub-Signature`
- App 到 hub signaling：`connect_ticket`
- App/Daemon 到 anonymous rendezvous：`channel_id + channel_secret`
- TURN：短期 HMAC 凭证

### Error

```json
{
  "error": {
    "code": "machine_offline",
    "message": "machine is offline",
    "request_id": "..."
  }
}
```

## Local Embedded Web API

这些 endpoint 由 `termx daemon` 本地监听地址提供，服务于内嵌本地 Web 和后续 LAN/local adapter。它们不需要 web-control 登录，不发 TermX TURN credentials，也不引入 workspace/tab/pane。

### Local Status

`GET /api/local/status`

Response:

```json
{
  "machine_id": "mach_...",
  "machine_name": "MacBook Pro",
  "machine_public_key_fingerprint": "sha256:...",
  "remote_enabled": true,
  "local_rtc": {
    "http_url": "http://127.0.0.1:7342",
    "ice_tcp_enabled": true,
    "ice_tcp_port": 7342
  }
}
```

### Local Terminal List

`GET /api/local/terminals`

Response:

```json
{
  "terminals": [
    {
      "terminal_id": "term_...",
      "name": "zsh",
      "command": ["zsh"],
      "cols": 120,
      "rows": 34,
      "state": "running",
      "last_active_at": "2026-05-01T10:00:00Z"
    }
  ]
}
```

### Local Pair

`POST /api/local/pair`

Request:

```json
{
  "pair_session_id": "pair_...",
  "pair_secret": "random_192bit_base64url",
  "app_device_id": "appdev_...",
  "app_name": "TermX Local Web",
  "app_public_key": "base64...",
  "requested_capabilities": ["terminal", "file_manager"]
}
```

Response:

```json
{
  "machine_id": "mach_...",
  "machine_name": "MacBook Pro",
  "machine_public_key": "base64...",
  "machine_public_key_fingerprint": "sha256:...",
  "app_certificate": {
    "payload": {},
    "signature": "base64..."
  },
  "expires_at": "2026-05-01T10:10:00Z"
}
```

### Local RTC Offer

`POST /api/local/rtc/offer`

Request:

```json
{
  "app_certificate": {
    "payload": {},
    "signature": "base64..."
  },
  "offer": {
    "session_id": "rtc_...",
    "machine_id": "mach_...",
    "terminal_id": "term_...",
    "sdp": "...",
    "ice_candidates": []
  },
  "signature": {
    "algorithm": "ed25519",
    "nonce": "...",
    "timestamp": 1770000000,
    "value": "base64..."
  },
  "client": {
    "type": "browser",
    "transport": "local"
  }
}
```

Response:

```json
{
  "answer": {
    "session_id": "rtc_...",
    "sdp": "...",
    "ice_candidates": []
  },
  "ice_tcp_enabled": true,
  "data_channels": ["api", "terminal:{terminal_id}", "file:{transfer_id}"]
}
```

Rules:

- local response must not contain TURN or TURNS URLs.
- browser adapter should prefer local host/TCP candidates when UDP candidates fail.
- daemon still verifies app certificate, app signature, nonce, timestamp, and requested terminal/file capability.

## Web Control API

### Auth

`POST /api/v1/auth/login`

```json
{
  "email": "user@example.com",
  "password": "..."
}
```

Response:

```json
{
  "access_token": "...",
  "refresh_token": "...",
  "user": {
    "id": "usr_...",
    "email": "user@example.com",
    "role": "user"
  }
}
```

`POST /api/v1/auth/refresh`

`GET /api/v1/auth/me`

### Tokens

`GET /api/v1/tokens`

`POST /api/v1/tokens`

```json
{
  "name": "macbook daemon",
  "expires_at": "2026-12-31T00:00:00Z"
}
```

Response returns token only once.

### Machine Bootstrap

`POST /api/v1/agent/bootstrap`

Called by embedded TermX agent.

Request:

```json
{
  "version": "termx/0.1.0",
  "machine_id": "mach_...",
  "machine_public_key": "...",
  "display_name": "MacBook Pro",
  "hostname": "mbp.local",
  "platform": "darwin/arm64",
  "labels": ["personal"],
  "terminals": [
    {
      "terminal_id": "term_...",
      "name": "zsh",
      "command": ["zsh"],
      "cols": 120,
      "rows": 34,
      "state": "running"
    }
  ]
}
```

Response:

```json
{
  "machine_id": "mach_...",
  "policy": {
    "allow_remote_terminal": true,
    "allow_file_manager": true,
    "allow_relay": true,
    "allow_relay_transfer": false,
    "relay_bandwidth_kbps": 1024,
    "max_terminals": 32
  },
  "hub": {
    "hub_id": "hub_sgp_1",
    "http_url": "https://hub.example.com",
    "region": "sgp"
  },
  "agent_session_token": "..."
}
```

### Machine List

`GET /api/v1/machines`

Response:

```json
{
  "machines": [
    {
      "machine_id": "mach_...",
      "display_name": "MacBook Pro",
      "hostname": "mbp.local",
      "platform": "darwin/arm64",
      "online": true,
      "hub_id": "hub_sgp_1",
      "last_seen_at": "2026-05-01T10:00:00Z",
      "terminals": [
        {
          "terminal_id": "term_...",
          "name": "zsh",
          "state": "running",
          "cols": 120,
          "rows": 34
        }
      ]
    }
  ]
}
```

`GET /api/v1/machines/{machine_id}`

`PATCH /api/v1/machines/{machine_id}`

Allowed fields:

```json
{
  "display_name": "dev machine",
  "labels": ["dev"]
}
```

### Connect Ticket

`POST /api/v1/machines/{machine_id}/terminals/{terminal_id}/connect`

Request:

```json
{
  "client": {
    "type": "mobile",
    "platform": "android",
    "version": "0.1.0"
  },
  "capabilities": {
    "terminal": true,
    "file_manager": true
  }
}
```

Response:

```json
{
  "ticket_id": "tkt_...",
  "connect_ticket": "signed.jwt.or.paseto",
  "expires_at": "2026-05-01T10:05:00Z",
  "hub": {
    "hub_id": "hub_sgp_1",
    "http_url": "https://hub.example.com"
  },
  "machine_id": "mach_...",
  "terminal_id": "term_...",
  "policy": {
    "allow_relay": true,
    "allow_relay_transfer": false,
    "allow_public_stun": true,
    "allow_p2p_files": true
  }
}
```

## Anonymous Rendezvous API

该服务用于未登录或未订阅用户的 P2P signaling。它不提供 TURN，不转发 terminal/file 数据。

### Create Channel

`POST /api/v1/anonymous/channels`

Called by `termx daemon` when the user runs `termx pair --anonymous` or equivalent.

Request:

```json
{
  "machine_id": "mach_...",
  "machine_public_key_fingerprint": "sha256:...",
  "ttl_seconds": 600
}
```

Response:

```json
{
  "channel_id": "rv_...",
  "channel_secret": "random_192bit_base64url",
  "expires_at": "2026-05-01T10:10:00Z",
  "public_stun_servers": [
    "stun:stun.l.google.com:19302",
    "stun:stun.cloudflare.com:3478"
  ]
}
```

### Listen Channel

`GET /api/v1/anonymous/channels/{channel_id}/events`

Auth:

```text
Authorization: Rendezvous channel_id:channel_secret
```

Server-sent events, WebSocket, or long-poll are acceptable. Message contract:

```json
{
  "type": "offer",
  "from": "appdev_...",
  "payload": {}
}
```

### Send Offer

`POST /api/v1/anonymous/channels/{channel_id}/offer`

Request:

```json
{
  "channel_secret": "...",
  "app_certificate": {
    "payload": {},
    "signature": "base64..."
  },
  "offer": {
    "sdp": "...",
    "ice_candidates": []
  },
  "signature": {
    "algorithm": "ed25519",
    "nonce": "...",
    "timestamp": 1770000000,
    "value": "base64..."
  }
}
```

### Send Answer

`POST /api/v1/anonymous/channels/{channel_id}/answer`

Request:

```json
{
  "channel_secret": "...",
  "answer": {
    "sdp": "...",
    "ice_candidates": []
  }
}
```

Limits:

- payload max 64KB。
- channel TTL 默认 10 分钟。
- no TURN credentials。
- no terminal/file data。
- per-IP / per-channel rate limit。

## Hub API

### Hub Heartbeat To Web Control

`POST /api/internal/hubs/heartbeat`

Request:

```json
{
  "hub_id": "hub_sgp_1",
  "region": "sgp",
  "http_url": "https://hub.example.com",
  "status": "online",
  "agent_count": 123,
  "bandwidth_mbps": 1000,
  "cpu_cores": 8,
  "memory_gb": 16,
  "max_agents": 500
}
```

### Agent Register To Hub

`POST /api/v1/agents/register`

Request:

```json
{
  "version": "termx/0.1.0",
  "machine_id": "mach_...",
  "agent_session_token": "...",
  "display_name": "MacBook Pro",
  "hostname": "mbp.local",
  "platform": "darwin/arm64",
  "terminals": [
    {
      "terminal_id": "term_...",
      "name": "zsh",
      "state": "running",
      "cols": 120,
      "rows": 34
    }
  ]
}
```

Response:

```json
{
  "hub_id": "hub_sgp_1",
  "agent_session_id": "ags_...",
  "heartbeat_interval_seconds": 15,
  "rtc_config": {
    "ice_servers": [
      { "urls": ["stun:hub.example.com:3478"] },
      {
        "urls": ["turn:hub.example.com:3478?transport=udp"],
        "username": "...",
        "credential": "..."
      }
    ]
  },
  "policy": {
    "allow_relay": true,
    "allow_relay_transfer": false
  }
}
```

### Agent Heartbeat To Hub

`POST /api/v1/agents/heartbeat`

```json
{
  "agent_session_id": "ags_...",
  "machine_id": "mach_...",
  "terminals": [
    {
      "terminal_id": "term_...",
      "name": "zsh",
      "state": "running",
      "cols": 120,
      "rows": 34
    }
  ]
}
```

### ICE Config

`GET /api/v1/rtc/config`

Anonymous/free P2P 不调用该 endpoint。匿名路径使用 rendezvous 返回的公共 STUN。该 endpoint 主要用于登录或订阅路径。

Response:

```json
{
  "mode": "relay_or_managed_p2p",
  "ice_servers": [
    { "urls": ["stun:hub.example.com:3478"] },
    {
      "urls": [
        "turn:hub.example.com:3478?transport=udp",
        "turn:hub.example.com:3478?transport=tcp"
      ],
      "username": "...",
      "credential": "..."
    }
  ]
}
```

### Signaling

`POST /api/v1/sessions`

Request from app/browser:

```json
{
  "connect_ticket": "...",
  "app_certificate": {
    "payload": {},
    "signature": "base64..."
  },
  "offer": {
    "sdp": "...",
    "ice_candidates": []
  },
  "signature": {
    "algorithm": "ed25519",
    "nonce": "...",
    "timestamp": 1770000000,
    "value": "base64..."
  }
}
```

Response:

```json
{
  "session_id": "rtc_...",
  "answer": {
    "sdp": "...",
    "ice_candidates": []
  }
}
```

Hub forwards to agent through long-poll, WebSocket, or gRPC stream. Transport can change, message contract stays stable:

```json
{
  "type": "webrtc_offer",
  "session_id": "rtc_...",
  "machine_id": "mach_...",
  "terminal_id": "term_...",
  "offer": {
    "sdp": "...",
    "ice_candidates": []
  },
  "app_certificate": {
    "payload": {},
    "signature": "base64..."
  },
  "signature": {
    "algorithm": "ed25519",
    "nonce": "...",
    "timestamp": 1770000000,
    "value": "base64..."
  },
  "policy": {
    "allow_relay": true,
    "allow_relay_transfer": false
  }
}
```

## DataChannel Protocol

### Channel Names

- `terminal:{terminal_id}`: TermX terminal binary protocol
- `api`: JSON request/response control channel
- `events`: optional event channel
- `file:{transfer_id}`: binary file transfer channel

### API Channel Request

```json
{
  "id": "req_...",
  "method": "GET",
  "path": "/files/list?path=/tmp",
  "body": null
}
```

Response:

```json
{
  "id": "req_...",
  "status": 200,
  "body": {}
}
```

### Agent API Paths

`GET /status`

`GET /terminals`

`POST /terminals/{terminal_id}/resize`

```json
{
  "cols": 120,
  "rows": 34
}
```

`GET /files/list?path=/`

`GET /files/stat?path=/tmp/a.txt`

`POST /files/mkdir`

`POST /files/rename`

`POST /files/delete`

`POST /files/copy`

`POST /files/move`

`POST /files/download/init`

```json
{
  "path": "/tmp/a.bin",
  "offset": 0
}
```

`POST /files/upload/init`

```json
{
  "path": "/tmp/a.bin",
  "size": 12345,
  "overwrite": false,
  "offset": 0
}
```

## Native/Web Transport Interface

```ts
export interface ControlApi {
  login(input: LoginInput): Promise<LoginResult>
  refresh(): Promise<AuthTokens>
  listMachines(): Promise<Machine[]>
  createConnectTicket(machineId: string, terminalId: string): Promise<ConnectTicket>
}

export interface LocalAgentApi {
  getStatus(): Promise<LocalStatus>
  listTerminals(): Promise<Terminal[]>
  pair(input: LocalPairInput): Promise<LocalPairResult>
  createRTCAnswer(input: LocalRTCOffer): Promise<LocalRTCAnswer>
}

export interface RendezvousApi {
  createChannel(input: CreateRendezvousChannelInput): Promise<RendezvousChannel>
  listen(channel: RendezvousChannel): AsyncIterable<RendezvousMessage>
  sendOffer(channel: RendezvousChannel, offer: SignedOffer): Promise<void>
  sendAnswer(channel: RendezvousChannel, answer: RTCSessionAnswer): Promise<void>
}

export interface SignalingApi {
  getRTCConfig(hubUrl: string): Promise<RTCConfig>
  createSession(hubUrl: string, ticket: string, offer: RTCSessionDescriptionInit): Promise<RTCSessionAnswer>
}

export interface PeerTransport {
  connect(input: PeerConnectInput): Promise<void>
  disconnect(): Promise<void>
  openTerminal(terminalId: string): Promise<BinaryChannel>
  openApi(): Promise<JsonRpcChannel>
  openFileTransfer(transferId: string): Promise<BinaryChannel>
  getConnectionInfo(): Promise<ConnectionInfo>
}
```

Connection modes:

```ts
type ConnectionMode = 'local' | 'anonymous_p2p' | 'managed_p2p' | 'paid_relay'
```

Rules:

- React components use `ControlApi` and `PeerTransport`, not `fetch` or `RTCPeerConnection` directly.
- Native implementation may expose a local bridge, but bridge protocol is private to native adapter.
- Browser implementation may use WebRTC directly, but only inside adapter package.
- `remote-ui` components may depend on `LocalAgentApi` for local embedded web metadata, but concrete browser/native implementations stay outside components.
