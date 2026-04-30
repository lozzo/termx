# 连接模式与免费策略

状态：规划稿。本文定义免费、匿名、订阅 relay 的产品和技术边界。

## 产品结论

TermX 远程能力默认慷慨开放：

```text
不登录 / 不订阅:
  允许本地连接
  允许匿名 P2P 直连
  使用公共 STUN / ICE
  如果打洞成功，terminal 和文件管理都可用
  不使用 TermX TURN relay

订阅:
  允许使用 TermX TURN relay
  打洞失败也能连接
  relay 下 terminal 和文件传输按套餐限额执行
```

关键点：

- 公共 STUN 只辅助 NAT 打洞，不承载 terminal/file 数据流量。
- P2P 成功后，数据在 app 与 `termx daemon` 之间直接传输。
- TermX 服务器免费承载的只是轻量 rendezvous/signaling，不承载免费用户的数据面。
- 只有 TURN relay 和 relay 下的大流量能力需要订阅。

## 模式矩阵

| 模式 | 登录 | 服务器参与 | ICE | 数据路径 | Terminal | 文件管理 | 成本 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Local LAN | 不需要 | 不需要 | 可不用或 host-only | app/browser -> daemon | 可用 | 可用 | 用户本地网络 |
| Anonymous P2P | 不需要 | 匿名 rendezvous/signaling | 公共 STUN | app -> daemon P2P | 可用 | 可用 | 低，只是 signaling |
| Free Account P2P | 需要 | 控制面 + signaling | 公共 STUN | app -> daemon P2P | 可用 | 可用 | 低，只是控制面/signaling |
| Paid Relay | 需要订阅 | 控制面 + hub + TURN | TermX STUN/TURN | app -> TURN -> daemon | 可用 | 可用，按 policy | 高，relay 流量 |

## Anonymous P2P

匿名 P2P 的目标是：用户不登录也能远程试用，只要 NAT 能打通。

限制：

- 不进入云端 machine list。
- 不跨设备同步。
- 不提供 TermX TURN relay。
- 不提供长期云端审计。
- 不提供云端证书管理；证书只保存在 app 和 daemon 本地。

允许：

- 扫码配对。
- app certificate。
- 公共 STUN。
- 轻量 signaling。
- P2P 成功后的 terminal。
- P2P 成功后的 file manager。

## Anonymous Rendezvous

STUN 不能交换 WebRTC offer/answer，所以匿名 P2P 仍需要一个轻量 rendezvous 服务。

职责：

- 短期创建 anonymous channel。
- 转发 offer/answer/ICE candidates。
- 不存 terminal 数据。
- 不存文件。
- 不做 TURN relay。
- 不需要用户登录。

数据流：

```mermaid
sequenceDiagram
  participant D as termx daemon
  participant R as anonymous rendezvous
  participant A as mobile app

  D->>R: create/listen channel<br/>channel_id + channel_secret
  D-->>User: show QR
  A->>A: scan QR, create app keypair
  A->>R: claim channel<br/>channel_id + channel_secret
  A->>R: send WebRTC offer<br/>public STUN only
  R->>D: forward offer
  D->>D: verify app certificate/signature
  D->>R: answer
  R->>A: answer
  A<->>D: P2P DataChannels
```

二维码内容需要包含：

```json
{
  "type": "termx_pair_v1",
  "mode": "anonymous_p2p",
  "machine_id": "mach_...",
  "machine_name": "MacBook Pro",
  "machine_public_key_fingerprint": "sha256:...",
  "rendezvous_url": "https://rv.termx.dev",
  "channel_id": "rv_...",
  "channel_secret": "random_192bit_base64url",
  "public_stun_servers": [
    "stun:stun.l.google.com:19302",
    "stun:stun.cloudflare.com:3478"
  ],
  "expires_at": "2026-05-01T00:10:00Z"
}
```

安全要求：

- `channel_secret` 至少 128 bit，建议 192 bit。
- channel 短 TTL，默认 10 分钟。
- signaling payload 限制大小，建议 64KB。
- 每 IP、每 channel、每 machine fingerprint 限速。
- channel claim 后只允许同一 app certificate 或同一 app public key 继续使用。
- rendezvous 不发 TURN credentials。
- daemon 必须验证 app certificate 和 app signature，不能只信 rendezvous。

## 公共 STUN

默认公共 STUN 列表可以内置在 app 和 daemon 配置中。

候选：

```text
stun:stun.l.google.com:19302
stun:stun1.l.google.com:19302
stun:stun.cloudflare.com:3478
```

注意：

- 公共 STUN 可用性不保证。
- 公共 STUN 不等于 relay。
- 对称 NAT、企业网络、运营商严格 NAT 可能失败。
- 失败时产品提示应清楚说明：当前网络无法 P2P 直连，订阅后可使用 TermX Relay。

## Paid Relay

订阅后开放 TermX TURN relay。

要求：

- 用户登录。
- machine 绑定到用户。
- app certificate 注册到 web-control。
- web-control 签发 connect ticket。
- ticket policy 包含 `allow_relay: true`。
- hub/TURN 生成短期 credentials。

数据流：

```mermaid
sequenceDiagram
  participant A as mobile app
  participant W as web-control
  participant H as termx-hub/TURN
  participant D as termx daemon

  A->>W: request connect ticket
  W-->>A: ticket + relay policy
  A->>H: create signaling session
  H->>D: forward offer
  D-->>H: answer
  H-->>A: answer
  A<->>H: TURN relay data
  H<->>D: TURN relay data
```

Relay policy:

```json
{
  "allow_relay": true,
  "allow_relay_transfer": true,
  "relay_bandwidth_kbps": 1024,
  "monthly_relay_bytes": 10737418240
}
```

## 文件管理策略

文件管理是否可用取决于实际数据路径：

- P2P / local：可用，因为不消耗 TermX relay 流量。
- Relay：按订阅 policy 决定，可禁用或限速。

App 必须显示当前连接类型：

```text
Direct P2P: Files enabled
Relay: Files enabled by plan
Relay: File transfer not included in current plan
```

## 从匿名到登录

用户可以先匿名配对并使用 P2P。后续登录或订阅时执行 claim：

```text
app already has app certificate
user logs in
app requests machine claim
daemon confirms claim or signs claim challenge
web-control binds machine to user
```

Claim API 后续由 web-control 提供：

```text
POST /api/v1/machines/claim/start
POST /api/v1/machines/claim/confirm
```

## 服务器承载能力

免费匿名模式服务器主要压力来自：

- rendezvous channel 创建。
- WebRTC offer/answer/candidate JSON 转发。
- 短连接或 WebSocket 心跳。

不会承载：

- terminal 数据。
- 文件数据。
- TURN relay 流量。

必须做的保护：

- channel TTL。
- payload size limit。
- IP rate limit。
- channel message rate limit。
- captcha 或 proof-of-work，可后续加。
- abuse metrics。
- 不存长期匿名连接数据。
