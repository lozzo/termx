# 分批次实施计划

状态：第三版。已纳入免登录匿名 P2P、公共 STUN、订阅 relay、app certificate 配对方案和 mobile 页面规划。

## 总原则

1. 安全身份底座先行，再做匿名 P2P signaling，再做订阅 relay，再做 UI。
2. 每一批都产出可运行、可测试的垂直切片。
3. 远程公开模型只允许 `machine -> terminal`。
4. 不把 `tuiv2` 的 workspace/tab/pane 模型带进远程体系。
5. Web/native 网络实现必须从第一天起走 interface。
6. 复用 `../tgent` 时只迁行为和成熟模块，不迁混乱状态管理。
7. `termx` daemon 内置 agent，不发布独立 agent 二进制。

## 依赖顺序

```text
P0 文档与边界
  -> P1 core remote runtime
  -> P2 identity + app certificate pairing
  -> P3 local + anonymous P2P signaling
  -> P4 app transport abstraction
  -> P5 app product shell with anonymous P2P
  -> P6 web-control auth/control plane
  -> P7 hub/TURN paid relay
  -> P8 end-to-end stack
  -> P9 hardening
```

关键原因：

- 没有 P2，就无法定义 app 与 agent 的端到端信任。
- 没有 P3，就无法提供免登录匿名远程试用。
- Web-control 和 Hub/TURN 是登录、订阅和 relay 的前置，不应阻塞匿名 P2P MVP。
- Hub/TURN 只应承载订阅 relay 或登录后的 managed signaling，不承载免费数据面。

## P0. 文档与边界冻结

目标：

- 顶层 remote 目录重新从空开始。
- 明确 `machine -> terminal` 对象模型。
- 明确 `termx` daemon 内置 agent。
- 明确本地 embedded web。
- 明确 transport interface。
- 明确 app certificate 配对方案。
- 明确匿名 P2P + 公共 STUN + 订阅 relay 策略。
- 明确 app 页面与第一版范围。

交付：

- `docs/remote-rebuild/README.md`
- `docs/remote-rebuild/architecture.md`
- `docs/remote-rebuild/api.md`
- `docs/remote-rebuild/auth-and-pairing.md`
- `docs/remote-rebuild/mobile-app-pages.md`
- `docs/remote-rebuild/implementation-plan.md`

验收：

- 文档能指导下一批建目录和接口。
- 没有新代码引用 workspace/tab/pane 作为远程公开模型。

## P1. Core Remote Runtime 骨架

目标：

- `termx daemon` 能启动 remote runtime。
- 支持 remote config。
- 持久化 machine identity。
- 提供 terminal inventory。
- 暴露 local status。

代码位置：

- `termx-core/remote.go`
- `termx-core/internal/remote/config`
- `termx-core/internal/remote/identity`
- `termx-core/internal/remote/runtime`
- `termx-cli/cmd/termx`

交付：

- `termx daemon` 可通过 env/config 开启 remote。
- `termx remote status` 可查看状态。
- runtime 能读取 terminal inventory。
- runtime 不依赖 `tuiv2`。

验收：

- `cd termx-core && go test ./...`
- `cd termx-cli && go test ./...`

当前状态：

- 已有一版 baseline，需要后续按新 P2/P3 鉴权模型调整。

## P2. Identity 与 App Certificate 配对

目标：

- machine keypair 只在 `termx daemon` 本机生成和保存。
- app keypair 在手机本地生成和保存。
- agent 用 machine private key 签发 app certificate。
- app 不下载、不解密、不持有 agent/machine private key。

代码位置：

- `termx-core/internal/remote/identity`
- `termx-core/internal/remote/cert`
- `termx-core/internal/remote/pairing`
- `termx-cli/cmd/termx`
- 后续 mobile native secure storage adapter

交付：

- machine key 生成、加载、fingerprint。
- app certificate payload、canonical encoding、sign、verify。
- nonce/timestamp 防重放 helper。
- `termx pair` 命令。
- 本地短期 pair session。
- `POST /api/local/pair`。
- cert revoke cache 数据结构。
- 匿名模式下本地 cert revoke 命令。

验收：

- machine key file 权限测试。
- certificate sign/verify 测试。
- pair secret 过期/单次使用测试。
- app signature message 验证测试。

不做：

- 不上传 machine private key。
- 不实现 tgent 的 encrypted agent private key 主路径。

## P3. Local + Anonymous P2P Signaling

目标：

- 不登录、不订阅也能通过公共 STUN 尝试 P2P。
- P2P 成功后 terminal 和 file manager 都可用。
- 免费路径不使用 TermX TURN relay。
- rendezvous 只转发 signaling，不转发 terminal/file 数据。

代码位置：

- `termx-core/internal/remote/localweb`
- `termx-core/internal/remote/rtc`
- `termx-core/internal/remote/bridge`
- `termx-core/internal/remote/rendezvous`
- 新建 `termx-rendezvous/` 或先在 `termx-hub` 前独立建轻量服务

交付：

- local `POST /api/local/rtc/offer`。
- anonymous rendezvous:
  - `POST /api/v1/anonymous/channels`
  - `GET /api/v1/anonymous/channels/{id}/events`
  - `POST /api/v1/anonymous/channels/{id}/offer`
  - `POST /api/v1/anonymous/channels/{id}/answer`
- QR 中包含 channel id、channel secret、public STUN list。
- app/daemon 只使用公共 STUN。
- P2P 成功后 DataChannel:
  - `terminal:{terminal_id}`
  - `api`
  - `file:{transfer_id}`
- P2P 失败时返回明确错误，不降级免费 relay。

验收：

- local terminal binary protocol roundtrip。
- anonymous fake rendezvous signaling test。
- public STUN config 不包含 TermX TURN。
- file manager 在 P2P 连接下可用。
- payload size limit / TTL / rate limit tests。

## P4. App Transport Abstraction

目标：

- 重新创建 `mobile-app/`。
- 先建网络接口和 adapters，不急着做完整 UI。
- browser adapter 和 native adapter 分离。
- React 业务层不直接碰 fetch/WebRTC/native plugin。
- app keypair 和 certificate storage 纳入 transport/auth 边界。
- 支持 `local`、`anonymous_p2p`、`managed_p2p`、`paid_relay` 四种连接模式。

可参考：

- `../tgent/tgent-app/src/api/webrtc.ts`
- `../tgent/tgent-app/src/api/fileClient.ts`
- `../tgent/tgent-app/src/api/terminalClient.ts`
- `../tgent/tgent-app/native/android/NativeConnectionPlugin.kt`
- `../tgent/tgent-app/native/android/transport/WebRTCTransport.kt`
- `../tgent/tgent-app/native/android/transfer/FileTransferManager.kt`
- `../tgent/tgent-app/native/android/crypto/*`

交付：

- `ControlApi`
- `RendezvousApi`
- `SignalingApi`
- `PeerTransport`
- `CertificateStore`
- `AppKeyStore`
- browser implementation。
- Android native implementation skeleton。
- mock implementation for UI tests。

验收：

- TS unit tests。
- app key/cert mock tests。
- anonymous P2P adapter tests。
- browser adapter signaling smoke。
- native bridge compile test。

## P5. App Product Shell With Anonymous P2P

目标：

- 手机 app 重做 UI/状态。
- 按 `mobile-app-pages.md` 实现第一版。
- 第一版必须支持未登录扫码配对和 anonymous P2P 连接。
- 只提供 machine list、terminal list、terminal attach、file manager、pairing、settings。
- 不复刻 workspace/tab/pane。

第一版页面：

- Welcome。
- Pair Machine。
- Machines，本地/匿名机器也展示。
- Machine Detail。
- Terminal Session。
- File Manager。
- Settings。
- Diagnostics，先做轻量 modal。
- Login 可存在，但不作为匿名 P2P 前置。

交付：

- 未登录扫码配对。
- 本地保存 machine + app certificate。
- anonymous P2P terminal。
- anonymous P2P file manager。
- P2P 失败时引导登录/订阅 relay。
- 登录和 token 保存。
- cloud machine list 可先 stub。
- settings logout / clear local keys。

验收：

- rendered UI smoke。
- mocked transport tests。
- Android debug build。
- anonymous P2P smoke。
- text/layout mobile viewport check。

延期：

- Activity。
- Certificate Detail 独立页面。
- 复杂快捷键配置。
- 多 terminal 并发 UI。
- 订阅购买完整流程。

## P6. Web Control Auth 与控制面

目标：

- 重新创建 `web-control/`。
- 迁改 tgent 用户、订阅、token、hub 管理能力。
- 把 `agent/server` 命名改为 `machine`。
- 实现 machine bootstrap。
- 实现 app certificate metadata 和 revoke。
- 实现 connect ticket 签发。
- 支持匿名 machine 后续 claim 到用户账号。

可参考：

- `../tgent/tgent-web/src/lib/schema.ts`
- `../tgent/tgent-web/src/app/api/auth`
- `../tgent/tgent-web/src/app/api/admin`
- `../tgent/tgent-web/src/app/api/tokens`
- `../tgent/tgent-web/src/app/api/hubs`
- `../tgent/tgent-web/src/app/api/internal`

交付：

- auth login/refresh/me。
- token/setup token API。
- machine bootstrap API。
- machine list/detail API。
- app certificate list/register/revoke API。
- pairing relay API，占位即可。
- hub heartbeat API。
- connect ticket API。
- machine claim API。
- subscription policy 输出。

验收：

- API tests。
- database migration tests。
- ticket sign/verify tests。
- certificate revoke policy tests。

## P7. Hub/TURN Paid Relay

目标：

- 重新创建 `termx-hub/`。
- 迁改 tgent hub/TURN 基础能力。
- 支持 agent register/heartbeat/signaling。
- 支持 STUN/TURN 短期凭证。
- 支持流量统计和 relay policy。
- hub 不持久化用户业务数据。
- hub/TURN 作为订阅功能，不承载匿名免费数据面。

可参考：

- `../tgent/tgent-go/internal/hub`
- `../tgent/tgent-go/internal/hubserver`
- `../tgent/tgent-go/internal/hubgrpc`

交付：

- `termx-hub` 可运行 HTTP + TURN。
- `GET /api/v1/rtc/config`。
- `POST /api/v1/agents/register`。
- `POST /api/v1/agents/heartbeat`。
- `POST /api/v1/sessions`。
- hub heartbeat to web control。
- signaling 透传 app certificate 和 app signature。
- connect ticket 校验。
- relay/file policy enforcement。

验收：

- hub unit tests。
- fake agent + fake app signaling test。
- TURN credential generation test。
- expired ticket / revoked cert / relay denied tests。

## P8. End-to-End Remote Slice

目标：

- web control + hub/TURN + termx daemon + app/browser 全链路跑通。

交付：

- local docker/devstack。
- anonymous P2P without login。
- single machine single terminal connect。
- app certificate 配对。
- connect ticket。
- relay-disabled direct connect path。
- relay-enabled path。
- file list/download/upload。
- traffic stats。

验收：

- automated e2e。
- manual Android test。
- failure mode tests:
  - offline machine。
  - stale terminal。
  - expired ticket。
  - revoked app certificate。
  - invalid app signature。
  - anonymous rendezvous expired。
  - P2P failed with no relay entitlement。
  - TURN denied。
  - relay transfer denied。

## P9. Hardening

目标：

- 安全、限额、可观测、发布。

交付：

- signed connect ticket 完整 key rotation。
- anonymous rendezvous abuse controls。
- short-lived TURN credentials。
- policy enforcement at web/hub/app/agent。
- app certificate revoke propagation。
- audit events。
- metrics/logging。
- rate limit。
- retry/backoff。
- release update metadata。
- deploy runbooks。
- security review checklist。

验收：

- security review。
- load test。
- upgrade/rollback test。
- key/cert rotation test。

## 不做事项

- 不在远程 API 中暴露 workspace/tab/pane。
- 不让 app 管理 TUI layout。
- 不把 native WebRTC 和 browser WebRTC 混在同一个业务类里。
- 不把 hub 变成业务数据库。
- 不发布独立 termx-agent 二进制。
- 不让 app 持有 machine private key。
- 不给匿名免费用户发 TermX TURN relay credentials。
