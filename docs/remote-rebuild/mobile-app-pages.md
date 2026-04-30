# Mobile App 页面规划

状态：第一版产品信息架构。后续 app 实现按本文拆页面和状态，不复刻 `../tgent` 的 workspace/tab/pane 结构。

## 产品原则

1. App 是远程终端和文件管理客户端，不是 TUI shell。
2. 对象模型只有 `machine -> terminal`。
3. 首页优先展示可连接的机器和最近使用的 terminal。
4. 连接状态必须直观：online、offline、stale、connecting、relay、p2p、error。
5. 所有网络能力来自 transport interface，页面不直接调用 WebRTC/fetch/native bridge。
6. 未登录也允许扫码添加机器并尝试匿名 P2P。
7. P2P 成功时 terminal 和文件管理都可用。
8. TermX Relay 是订阅能力，P2P 失败时引导登录/订阅。
9. 文件管理作为 terminal 的辅助能力，不做完整网盘产品。

## 导航结构

```text
Auth Stack
  Welcome
  Login
  Register
  Forgot Password

Main Tabs
  Machines
  Activity
  Settings

Modal / Flow
  Pair Machine
  Machine Detail
  Terminal Session
  File Manager
  Connection Diagnostics
  Certificate Detail
```

第一版可以没有底部多 tab，直接用：

```text
Machines -> Machine Detail -> Terminal Session
                      \-> File Manager
Settings
Pair Machine
```

但数据模型和路由要预留 `Activity`。

## 页面清单

### 1. Welcome

用途：

- 未登录首次打开。
- 简短说明 app 用于连接自己的机器。

展示：

- TermX 标识。
- 登录按钮。
- 注册按钮。
- 扫码配对入口。未登录也允许保存本地 machine 和 app certificate。
- 匿名 P2P 说明：直连成功即可使用；需要 Relay 时再登录订阅。

操作：

- `Login`
- `Create account`
- `Scan pairing QR`

不展示：

- marketing 长文案。
- workspace/tab/pane 概念。

### 2. Login

用途：

- 用户登录 web control。

展示：

- email。
- password。
- server/control URL，高级设置里展示。
- 登录错误。

操作：

- 登录。
- 跳转注册。
- 忘记密码。
- OAuth 登录，如果 web control 已配置。

数据：

- `POST /api/v1/auth/login`
- 保存 access token / refresh token 到 secure storage。

状态：

- loading。
- invalid credentials。
- subscription warning，但不阻塞登录。
- network error。

### 3. Register

用途：

- 创建用户账号。

展示：

- email。
- username。
- password。
- referral/promo code，可选。

操作：

- 注册并登录。
- 返回登录。

数据：

- `POST /api/v1/auth/register`

### 4. Machines

用途：

- App 主屏。
- 查看所有可访问机器。
- 快速进入最近 terminal。

展示：

- 用户当前订阅摘要：plan、到期时间、relay/file 权限简短状态。
- 未登录时展示 `Anonymous P2P` 状态说明，不展示订阅摘要。
- 搜索框。
- machine list。
- 每个 machine card:
  - display name。
  - hostname。
  - platform。
  - online/offline/stale。
  - hub region。
  - terminal count。
  - last seen。
  - 最近 terminal 名称。
  - relay/file manager 能力小标识。
  - source: local anonymous / cloud。

操作：

- 点击 machine 进入 Machine Detail。
- 下拉刷新。
- 扫码配对。
- 长按或菜单：
  - rename machine。
  - revoke app certificate。
  - forget local machine record。

空状态：

- 未绑定机器：展示 `Pair Machine`。
- 网络失败：展示 retry。
- 订阅过期：展示续费入口，但仍可查看历史机器。
- 未登录：展示本地已配对机器和登录入口。

数据：

- 本地 machine store。
- 已登录时追加 `GET /api/v1/machines`。
- 已登录时追加 `GET /api/v1/billing/subscription`。

不展示：

- workspace。
- tab。
- pane。
- tmux session。

### 5. Machine Detail

用途：

- 查看一台机器详情和 terminal 列表。

展示：

- machine header:
  - display name。
  - machine id 短 ID。
  - online state。
  - hostname。
  - platform。
  - termx version。
  - hub region。
  - last seen。
  - connection policy。
  - anonymous P2P / managed P2P / paid relay availability。
- terminal list:
  - terminal name。
  - command。
  - state。
  - size cols x rows。
  - last active。
- quick actions:
  - Open Terminal。
  - Files。
  - Diagnostics。

操作：

- 点击 terminal 进入 Terminal Session。
- 打开 File Manager。
- rename machine。
- manage certificates。
- disconnect/forget local cached connection。

数据：

- 本地 machine store。
- 匿名 P2P：rendezvous channel info from QR/local record。
- 已登录：`GET /api/v1/machines/{machine_id}`。
- 已登录 relay：`POST /api/v1/machines/{machine_id}/terminals/{terminal_id}/connect`。

错误状态：

- machine offline。
- terminal stale。
- terminal no longer exists。
- connect ticket denied。
- subscription does not allow relay/file transfer。
- anonymous rendezvous expired。
- P2P failed and relay not available。

### 6. Terminal Session

用途：

- 连接并操作一个 terminal。

展示：

- 顶部紧凑连接栏:
  - machine name。
  - terminal name。
  - p2p/relay/unknown。
  - latency。
  - connected/reconnecting/offline。
- terminal viewport。
- 可收起工具栏:
  - keyboard toggle。
  - paste。
  - resize/fit。
  - reconnect。
  - files。
  - diagnostics。

操作：

- 输入。
- 粘贴。
- 手动 reconnect。
- 打开 file manager。
- 复制选区。
- 发送常用组合键，后续版本：Ctrl/Cmd/Esc/Tab。

数据：

- 匿名 P2P：channel secret + anonymous rendezvous + public STUN。
- 登录 P2P/relay：connect ticket from web control。
- relay：signaling through hub。
- DataChannel `terminal:{terminal_id}`。
- DataChannel `api` for resize/status。

连接状态：

- preparing anonymous channel。
- preparing ticket。
- gathering ICE。
- signaling。
- opening data channel。
- connected。
- reconnecting。
- relay disabled。
- relay connected。
- p2p failed, relay requires subscription。
- failed。

不做：

- 多 terminal 分屏。
- tabs。
- workspace layout。
- pane 管理。

### 7. File Manager

用途：

- 管理当前 machine 文件。

展示：

- 当前路径 breadcrumb。
- 文件列表:
  - name。
  - type。
  - size。
  - modified time。
  - permissions，后续可选。
- 传输队列:
  - upload/download progress。
  - pause/retry/cancel。
- 当前连接策略:
  - relay transfer allowed/blocked。

操作：

- list directory。
- preview text/image，小文件优先。
- download。
- upload。
- mkdir。
- rename。
- delete。
- copy/move，后续可做。

数据：

- DataChannel `api`:
  - `GET /files/list`
  - `GET /files/stat`
  - `POST /files/mkdir`
  - `POST /files/delete`
  - `POST /files/rename`
  - `POST /files/download/init`
  - `POST /files/upload/init`
- DataChannel `file:{transfer_id}` for binary transfer。

限制：

- P2P/local 连接下文件上传下载可用。
- relay transfer 被 policy 禁用时，文件上传/下载按钮 disabled。
- 大文件传输要显示网络类型和风险。
- 不做云端存储。

### 8. Pair Machine

用途：

- 扫码绑定一台运行 `termx daemon` 的机器。
- 获取 app certificate。
- 未登录时创建 anonymous/local machine record。

展示：

- Camera scanner。
- 手动输入 pairing code。
- pairing progress。
- machine fingerprint 确认页。
- 成功后的 machine summary。
- 匿名 P2P 可用性说明。

操作：

- 扫描二维码。
- 手动输入。
- 确认 machine fingerprint。
- 保存 app certificate。
- 未登录：保存到本地 machine store。
- 已登录：注册 certificate metadata 到 web control。

数据：

- QR payload:
  - `machine_id`
  - `machine_public_key_fingerprint`
  - `pair_session_id`
  - `pair_secret`
  - `local_pair_url` 或 remote pairing URL
  - `rendezvous_url`
  - `channel_id`
  - `channel_secret`
  - `public_stun_servers`
- Local:
  - `POST /api/local/pair`
- Anonymous P2P:
  - `POST /api/v1/anonymous/channels/{id}/offer`
  - `POST /api/v1/anonymous/channels/{id}/answer`
- Cloud pairing after login:
  - `POST /api/v1/pairing/sessions/{id}/claim`

错误状态：

- QR expired。
- pair secret invalid。
- machine fingerprint mismatch。
- local machine unreachable。
- anonymous rendezvous expired。
- P2P direct failed。
- web control not logged in，仅影响云端同步和 Relay，不影响本地保存。

安全要求：

- app 生成自己的 app keypair。
- app 不下载 agent private key。
- app 保存 app certificate。
- 配对成功后清除 pair secret。
- 不向 anonymous rendezvous 发送 terminal/file 数据。

### 9. Certificate Detail

用途：

- 查看当前手机对某台 machine 的证书。
- 撤销或重新配对。

展示：

- app device name。
- cert id。
- machine name。
- machine fingerprint。
- issued at。
- expires at。
- capabilities。
- revoked state。

操作：

- revoke certificate。
- rotate app key / re-pair。
- copy fingerprint。

数据：

- `GET /api/v1/machines/{machine_id}/app-certificates`
- `DELETE /api/v1/machines/{machine_id}/app-certificates/{cert_id}`

### 10. Activity

用途：

- 查看最近连接和传输。

第一版可延后。

展示：

- recent terminal sessions。
- file transfers。
- failed connection attempts。
- security events:
  - certificate issued。
  - certificate revoked。
  - login refreshed。

数据：

- 本地 app event store。
- 后续可合并 web-control audit events。

### 11. Connection Diagnostics

用途：

- 排查连接失败。

展示：

- control URL。
- rendezvous URL。
- hub URL。
- machine online state。
- ticket expiry。
- ICE gathering state。
- connection type p2p/relay。
- local/remote candidate type。
- RTT。
- relay policy。
- public STUN server list。
- last error code。

操作：

- retry。
- copy diagnostics。
- open support log。

数据：

- `PeerTransport.getConnectionInfo()`
- local transport logs。
- local machine store。
- 已登录时 `GET /api/v1/machines/{machine_id}`。

### 12. Settings

用途：

- 用户、订阅、网络、安全、本地缓存设置。

展示：

- account:
  - 未登录状态。
  - email，已登录时。
  - plan，已登录时。
  - subscription expiry，已登录时。
- network:
  - control URL。
  - preferred region，后续。
  - relay usage policy，后续。
- security:
  - paired certificates。
  - biometric lock，后续。
  - clear local keys。
- app:
  - version。
  - logs。

操作：

- login / subscribe。
- logout。
- refresh subscription。
- manage paired machines。
- clear cache。
- export diagnostics。

## 全局状态模型

```ts
type MachineState = 'online' | 'offline' | 'stale'
type TerminalState = 'running' | 'exited' | 'unknown'
type ConnectionState =
  | 'idle'
  | 'preparing_anonymous_channel'
  | 'requesting_ticket'
  | 'gathering_ice'
  | 'signaling'
  | 'opening_channel'
  | 'connected'
  | 'reconnecting'
  | 'failed'
  | 'closed'
type ConnectionType = 'p2p' | 'relay' | 'unknown'
type MachineSource = 'local_anonymous' | 'cloud'
```

## 第一版最小可交付

P5 app shell 第一版只做：

1. Welcome。
2. Pair Machine。
3. Machines，本地匿名机器优先。
4. Machine Detail。
5. Terminal Session，支持 anonymous P2P。
6. File Manager，支持 anonymous P2P。
7. Settings 里的 login/logout、clear local keys 和 diagnostics。
8. Login，可用但不作为匿名 P2P 前置。

延期：

- Activity。
- Certificate Detail 独立页面，可先放 Machine Detail modal。
- 复杂快捷键配置。
- 多 terminal 并发 UI。
- 文件 copy/move 高级队列。
- 订阅购买完整流程，先跳 web control。

## 页面到接口映射

| 页面 | Web Control | Hub | Agent DataChannel | Local |
| --- | --- | --- | --- | --- |
| Login | auth login/refresh | - | - | - |
| Machines | optional machines list, subscription | - | - | local machine store |
| Machine Detail | optional machine detail, connect ticket | anonymous rendezvous or hub sessions | - | local machine store |
| Terminal Session | optional connect ticket | anonymous rendezvous or hub sessions | terminal, api resize/status | local rtc offer |
| File Manager | optional connect ticket/policy | anonymous rendezvous or hub sessions | api files, file channel | local rtc offer |
| Pair Machine | optional pairing sessions, cert metadata | anonymous rendezvous | - | local pair |
| Settings | auth me, subscription, certificates | - | - | local cache |

## 明确不做

- 不做 workspace 页面。
- 不做 tab 管理。
- 不做 pane 管理。
- 不做 remote TUI layout 编辑。
- 不把文件页做成云盘。
- 不让页面直接 import browser/native networking implementation。
