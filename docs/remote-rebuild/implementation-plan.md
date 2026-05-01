# 分批次实施计划

状态：第四版。已调整为先做 `termx` 二进制内嵌本地 Web 和本地 WebRTC-over-TCP 调试闭环，再迁移同一套远程 UI 到 mobile app。

## 总原则

1. 安全身份底座先行，再做本地 embedded web 可运行闭环，再做匿名 P2P signaling，再迁移到 app，最后做订阅 relay。
2. 每一批都产出可运行、可测试的垂直切片。
3. 远程公开模型只允许 `machine -> terminal`。
4. 不把 `tuiv2` 的 workspace/tab/pane 模型带进远程体系。
5. Web/native 网络实现必须从第一天起走 interface。
6. 复用 `../tgent` 时优先保持页面代码、目录结构、组件边界和 adapter 分层可同步；只迁成熟行为，不迁 workspace/tab/pane 语义或混乱状态管理。
7. `termx` daemon 内置 agent，不发布独立 agent 二进制。
8. 远程 UI 先在本地 embedded web 中跑通，再复用到 mobile app；`Terminal.tsx`、`TerminalList.tsx`、`FileManager.tsx` 必须从同一套共享组件演进。
9. 本地 WebRTC 必须支持 TCP ICE mux / WebRTC-over-TCP fallback，避免本地网络或容器环境里 UDP 不可用时直接失败。
10. 对 tgent 中不够移动端原生的交互，TermX 要在消息处理层模拟 native app 行为：生命周期事件、恢复校验、用户 intent、错误提示和队列背压都由状态机统一处理，页面只消费稳定 snapshot。

## 依赖顺序

```text
P0 文档与边界
  -> P1 core remote runtime
  -> P2 identity + app certificate pairing
  -> P3 embedded local web + local WebRTC-over-TCP
  -> P4 anonymous P2P signaling
  -> P5 mobile app shell reusing shared remote UI
  -> P6 web-control auth/control plane
  -> P7 hub/TURN paid relay
  -> P8 end-to-end stack
  -> P9 hardening
```

关键原因：

- 没有 P2，就无法定义 app 与 agent 的端到端信任。
- 没有 P3，就无法在不引入 mobile/native 复杂度的情况下验证 terminal、file manager、local signaling、WebRTC DataChannel 和 agent bridge。
- 本地 embedded web 跑通后，mobile app 迁移只需要替换 app shell、storage 和 native transport adapter，而不重写 terminal/file manager 组件。
- 没有 P4，就无法提供免登录匿名远程试用。
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
- 本地 pair API 所需 core contract；HTTP endpoint 在 P3 embedded local web 中暴露。
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

## P3. Embedded Local Web + Local WebRTC-over-TCP

目标：

- `termx` 二进制内嵌本地 Web 静态资源，用户通过本机浏览器完成远程 UI 的第一轮调试。
- 本地 Web 和后续 mobile app 共享 `Terminal.tsx`、`TerminalList.tsx`、`FileManager.tsx` 等核心组件，避免双写。
- 本地 Web 只依赖 machine/terminal 模型，不复刻 workspace/tab/pane。
- 本地浏览器通过 local HTTP + WebRTC DataChannel 连接 daemon agent。
- 本地 WebRTC 支持 TCP ICE mux / WebRTC-over-TCP fallback，防止本地 UDP、容器网络或企业网络策略导致调试不可用。
- 本地模式先验证 terminal 和 file manager 的真实数据面；匿名 rendezvous 之后只替换 signaling 入口。

代码位置：

- `termx-core/internal/remote/localweb`
- `termx-core/internal/remote/rtc`
- `termx-core/internal/remote/bridge`
- 新建 `remote-ui/`，承载本地 Web 与 mobile app 共享的 React 组件、hooks、transport interfaces、browser adapters 和测试。
- `termx-cli/cmd/termx`，负责把 embedded web 和本地 remote runtime 暴露到 `termx daemon` / `termx remote` 命令。

可参考：

- `../tgent/tgent-go/internal/server/server.go` 中 HTTP 与 ICE TCP 端口复用 / cmux 思路。
- `../tgent/tgent-go/internal/agent/webrtc.go` 中 Pion WebRTC handler、TCP mux、DataChannel 命名和本地 offer 处理。
- `../tgent/tgent-app/src/api/webrtc.ts`
- `../tgent/tgent-app/src/api/terminalClient.ts`
- `../tgent/tgent-app/src/api/fileClient.ts`
- `../tgent/tgent-app/src/components/Terminal.tsx`
- `../tgent/tgent-app/src/components/SessionList.tsx`，迁改为 `TerminalList.tsx`。
- `../tgent/tgent-app/src/components/files/FileManager.tsx`

同步策略：

- 首次复制后保留与 tgent 可对照的文件拆分、组件边界和核心 props 命名，除非这些命名泄露 workspace/tab/pane。
- 必须建立 TermX 命名适配层：`server` -> `machine`，`pane` -> `terminal`，`session/window` 不作为 public model。
- 行为修复优先在与 tgent 同名/近似文件中落地，避免之后无法继续同步 upstream 变更。
- 不为同步而保留 tgent 的 web-only 交互状态；这些状态要收敛到 TermX 的 message reducer / event queue。

交付：

- embedded static web 文件系统和 build/embed 流程。
- local Web shell，展示 machine status、terminal list、terminal session、file manager。
- shared remote UI package:
  - `Terminal.tsx`
  - `TerminalList.tsx`
  - `FileManager.tsx`
  - `useTerminalSession`
  - `useFileManager`
  - `connectionMessageReducer`
  - `ConnectionEventQueue`
  - `RemoteTransport` / `PeerTransport` interface
  - browser local adapter
- local `POST /api/local/rtc/offer`。
- local `GET /api/local/status`。
- local `GET /api/local/terminals`。
- local `POST /api/local/pair`。
- local file manager API over DataChannel `api`。
- local WebRTC ICE TCP mux / WebRTC-over-TCP fallback。
- DataChannel:
  - `terminal:{terminal_id}`
  - `api`
  - `file:{transfer_id}`
- 本地浏览器 smoke test 页面和 mock transport tests。

验收：

- embedded web asset test。
- local pair API test。
- local RTC offer/answer test。
- ICE TCP mux enabled test or adapter-level contract test。
- local terminal binary protocol roundtrip。
- local file manager list/download/upload smoke。
- TS tests for shared remote UI hooks/adapters。
- message reducer / event queue tests covering reconnect, app resume, duplicate transport events, terminal/file channel lifecycle, and user-visible error routing。
- Browser smoke: `termx daemon` serves local Web and opens a terminal using local WebRTC.
- `cd termx-core && go test ./...`
- `cd termx-cli && go test ./...`

不做：

- 不接公网 rendezvous。
- 不接 TermX TURN relay。
- 不让 anonymous/free flow 获得 TURN credentials。
- 不把 workspace/tab/pane 作为 UI 或 API 概念。

## P4. Anonymous P2P Signaling

目标：

- 不登录、不订阅也能通过公共 STUN 尝试 P2P。
- P2P 成功后 terminal 和 file manager 都可用，因为 P3 已验证同一套 DataChannel/agent bridge。
- 免费路径不使用 TermX TURN relay。
- rendezvous 只转发 signaling，不转发 terminal/file 数据。
- 本地 Web 和 mobile app 都复用 P3 的 shared remote UI，只切换 transport/signaling adapter。

代码位置：

- `termx-core/internal/remote/rendezvous`
- `termx-core/internal/remote/rtc`
- `remote-ui/` anonymous adapter
- 新建 `termx-rendezvous/` 或先在 `termx-hub` 前独立建轻量服务

交付：

- anonymous rendezvous HTTP adapter/service:
  - `POST /api/v1/anonymous/channels`
  - `GET /api/v1/anonymous/channels/{id}/events`
  - `POST /api/v1/anonymous/channels/{id}/offer`
  - `POST /api/v1/anonymous/channels/{id}/answer`
- QR 中包含 channel id、channel secret、public STUN list。
- app/browser/daemon 只使用公共 STUN。
- P2P 失败时返回明确错误，不降级免费 relay。

验收：

- anonymous fake rendezvous signaling test。
- public STUN config 不包含 TermX TURN。
- file manager 在 anonymous P2P 连接下可用。
- payload size limit / TTL / rate limit tests。
- app certificate + app signature checks around WebRTC offer。

## P5. Mobile App Shell Reusing Shared Remote UI

目标：

- 重新创建 `mobile-app/`。
- 以 P3 的 `remote-ui/` 为源，迁移同一套 terminal list、terminal session 和 file manager 组件到 mobile app。
- mobile app 只新增 app shell、navigation、native storage、camera/QR、secure key storage 和 native transport adapter。
- browser adapter 和 native adapter 分离，React 业务层不直接碰 fetch/WebRTC/native plugin。
- mobile app 使用 `remote-ui` 的消息处理层，native plugin 只上报规范化事件；页面不直接处理 WebRTC callback、foreground/background callback 或网络切换 callback。
- app keypair 和 certificate storage 纳入 transport/auth 边界。
- 第一版必须支持本地配对、anonymous P2P、terminal、file manager。
- 支持 `local`、`anonymous_p2p`、`managed_p2p`、`paid_relay` 四种连接模式的数据模型，但 managed/relay 可先 stub。
- 不复刻 workspace/tab/pane。

可参考：

- `../tgent/tgent-app/src/api/webrtc.ts`
- `../tgent/tgent-app/src/api/fileClient.ts`
- `../tgent/tgent-app/src/api/terminalClient.ts`
- `../tgent/tgent-app/native/android/NativeConnectionPlugin.kt`
- `../tgent/tgent-app/native/android/transport/WebRTCTransport.kt`
- `../tgent/tgent-app/native/android/transfer/FileTransferManager.kt`
- `../tgent/tgent-app/native/android/crypto/*`

第一版页面：

- Welcome。
- Pair Machine。
- Machines，本地/匿名机器也展示。
- Machine Detail。
- Terminal Session，使用 `remote-ui/Terminal.tsx`。
- File Manager，使用 `remote-ui/FileManager.tsx`。
- Settings。
- Diagnostics，先做轻量 modal。
- Login 可存在，但不作为匿名 P2P 前置。

交付：

- mobile app shell 和 routing。
- `ControlApi`
- `RendezvousApi`
- `SignalingApi`
- `PeerTransport`
- `CertificateStore`
- `AppKeyStore`
- browser implementation 复用 P3，mobile 只接入 native implementation。
- Android native implementation skeleton。
- native-like message adapter: app resume/suspend、network change、transport reconnect、connection verify 和 error routing。
- mock implementation for UI tests。
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
- TS unit tests。
- app key/cert mock tests。
- anonymous P2P adapter tests。
- native-like message handling tests for foreground/background, network switching, retries, stale channel cleanup, and non-blocking user feedback。
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
