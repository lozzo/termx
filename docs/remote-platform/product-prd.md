# Muxvia 远程连接产品要求

状态：统一 WebRTC DataChannel 产品基线

活动切片、顺序和完成证据只记录在仓库根目录 `workflow.md`。本文定义稳定产品要求，不维护研发状态表。

Cloud 账号、套餐、交易、Subscription、Entitlement、managed P2P/Relay 准入、限额、用量和管理面的产品真值见 `cloud-product-spec.md`。本文只定义 Muxvia 整体产品形态和连接边界。

## 1. 产品形态

- Muxvia 只有一个面向用户的 App。
- App 原生支持 Direct、SSH 和 Muxvia Cloud managed Route。
- Direct 与 SSH 不要求登录或订阅。
- Cloud 是同一个 App 内的可选托管能力，不是独立 App flavor。
- 用户未登录、订阅失效或 Cloud 故障时，已保存的 Direct/SSH Endpoint 必须继续显示和工作。
- 当前不默认提供 Web 访问界面；Web/WASM 产品恢复由独立延后切片决定。

## 2. 用户管理对象

用户管理的是 daemon Endpoint，不是不同连接方式对应的多份机器记录。

```text
Endpoint
  stable client EndpointID
  verified daemon identity
  label
  connect policy
  Route[]
```

- 同一 daemon 通过 DeviceIdentity/fingerprint 归并。
- IP、域名、SSH host、Cloud DeviceID 和展示名称不能单独建立身份。
- 同一 Endpoint 可以同时拥有 Direct、SSH 和 Cloud Route。
- terminal 引用固定为 `EndpointID + daemon-local TerminalID`。

## 3. 连接方式

### 3.1 Local Unix

- 本机 CLI/TUI 到本机 daemon 使用 Unix socket。
- Local 不为了形式统一改成 WebRTC。
- Local 不进入移动 App 配对或 Endpoint share。

### 3.2 Direct WebRTC TCP

- daemon 提供 embedded signaling 和 ICE-TCP listener。
- embedded signaling 使用短期、一次性、versioned Proto request；daemon 在创建 peer 前校验 Endpoint pin、有效期和 request replay。
- answer 携带 daemon public identity，并由 DeviceIdentity 对 request correlation、SDP、ICE candidate 和有效期做 deterministic protobuf 签名；客户端验签后仍必须在实际 DTLS DataChannel 内完成 DeviceHello/CapabilityGrant auth。
- App/CLI/TUI 通过 Go Client Engine 建立 WebRTC DataChannel。
- 支持 LAN 自动地址和用户显式覆盖公网 IP、域名、端口及 server name。
- 地址覆盖用于 FRP 或其它 TCP 映射，只改变 locator，不改变 daemon identity 和授权。
- 不依赖 Muxvia Cloud。

### 3.3 SSH WebRTC TCP

- Go Client Engine 使用 Go SSH client 完成 host-key 校验和用户认证。
- SSH `direct-tcpip` tunnel 转发 daemon loopback signaling 与 ICE-TCP。
- tunnel 内继续建立与 Direct/Cloud 相同的 WebRTC DataChannel、remote auth、Hello 和 Proto API session。
- 用户可以使用 SSH agent、private key、password 和 ProxyJump；secret 只进入平台 secure store。
- 不依赖 Muxvia Cloud，也不使用旧远端 stdio proxy。

### 3.4 Muxvia Cloud

- Cloud 提供账号目录、托管 signaling、ICE-UDP、TURN Relay 和跨网络可达性。
- Muxvia Cloud 是唯一 managed WebRTC provider，不提供用户自建 Hub/Relay/signaling provider contract。
- Cloud 账号和订阅只控制 managed Route eligibility。
- Control Plane、Hub 和 Relay 不能读取 terminal payload、CapabilityGrant body 或判断 terminal scope。
- Cloud 退出登录或服务失败只影响 managed Route。
- 开发模式也必须完整执行账号、交易、Subscription、Entitlement、Hub policy、Relay lease、usage 和管理链路；只允许外部 provider 使用显式测试实现。
- 不同套餐可以分别控制 managed P2P、Relay、region、并发、速率和周期流量；套餐不得影响 Direct、SSH 或 daemon terminal capability。

## 4. 统一会话

所有远程连接最终必须进入可靠有序 WebRTC DataChannel：

```text
Route connector
  -> ReadyPeerSession
  -> daemon identity + channel binding
  -> CapabilityGrant / PairingTicket auth
  -> protocol Hello
  -> generated Proto command/result/event
```

- Direct、SSH 和 Cloud 不能形成三套 application session。
- terminal、history、input、resize、file 和 event 使用同一 Proto API。
- UI 不感知 Pion、SSH tunnel、Cloud signaling 或 Relay 的内部对象。
- session replacement 必须产生新 generation；旧 handle 和迟到 callback 失效。

## 5. 配对

`muxvia pair create` 用于把当前 daemon 添加并授权到 App：

- QR/手工码只包含 128-bit 一次性 claim、DeviceIdentity public key、有效期和建立首个 pairing DataChannel 必需的一个 Route seed；不包含 PairingTicket、scope 或 grant。
- daemon 默认监听所有 IPv4 interface，并把当前活动 RFC1918 IPv4 地址投影为可预览的 LAN signaling/ICE-TCP locator；没有可用 LAN 地址时必须由用户显式指定，不能发布 wildcard 地址。
- 用户可以同时覆盖 Direct signaling/ICE-TCP 的对外地址和端口，并可设置 server name；显式覆盖完全替代自动 LAN seed，用于 FRP 或其它 TCP 映射。
- App 从短码建立 Direct 或 Cloud managed pairing DataChannel，验证 DeviceHello 后提交 ClientAccessIdentity proof；owning daemon 才在端到端响应中返回完整签名 bundle 和 client-bound grant。
- PairingTicket 不能直接访问 terminal、history 或 file。
- QR 不包含长期 bearer grant、private key、Cloud token 或本地 credential ref。
- `muxvia pair create --text` 输出 `MXP1-...` portable claim code，默认二维码不高于 QR Version 10；`--qr-file FILE` 仍生成 owner-only 正方形 PNG。`--raw`/`--out` 只保留为本机 owner 脚本的完整 bundle 兼容入口，不得进入 App 扫码或 Web/Cloud。

## 6. Endpoint 分享

`muxvia endpoint share <endpoint>` 用于在客户端之间迁移已配置 Route 和 selection policy：

- 发送方通过一次性、短期、带 receiver proof 的 share session 发送 portable config。
- 接收方导入前展示 Route/policy diff。
- share 可以描述目标端所需 credential 类别，但不传 secret body。
- share 不传源 EndpointID、runtime winner、session、Cloud token、SSH secret 或源客户端 grant。
- 静态二维码只包含 TLS listener locator、临时证书 SHA-256 pin、一次性 session secret、transfer ID 和有效期；Endpoint 配置只能在 TLS pin 与 receiver proof 均通过后发送。
- 当前 share 固定为 config-only：导入后 Endpoint 保持未授权，接收端必须重新完成 daemon pairing，并在本地准备 SSH/Cloud credential。

## 7. Go Client Engine

- 所有官方客户端和仓库提供的外部客户端接入都复用 Go Client Engine。
- Go/native 直接调用。
- Android 使用稳定 C ABI + 薄 JNI/Capacitor bridge。
- 未来 iOS/Desktop 使用 C ABI wrapper。
- 未来浏览器使用 Go/WASM；当前 Web 产品冻结。
- Kotlin/JavaScript 只提供 UI、lifecycle、secure store、权限和必要平台 primitive，不复制连接状态机。

## 8. 安全与隐私

- DeviceIdentity 和 ClientAccessIdentity private key 不可导出。
- Android 使用 Keystore signer/store；平台层只返回签名结果，不长期暴露 key bytes。
- SSH host-key mismatch、daemon fingerprint mismatch、DTLS binding mismatch 和授权失败必须 fail closed。
- Hub/Relay 只能看到连接 metadata 和加密流量，不得看到 terminal 数据。
- 禁止用 fallback、旧 parser、重复授权状态或 storage scrub 掩盖身份和生命周期错误。

## 9. Android 用户验收

Android 用户可用性必须由 ARM64 模拟器上的真实 APK UI 证明，不以 Go/JNI 单测代替。

最终 APK 至少覆盖：

1. 扫码或导入 Endpoint。
2. 建立 Direct、SSH 和 Cloud 连接。
3. 查看 terminal 列表并打开 terminal。
4. 输入命令、验证输出并持续交互。
5. 上传和下载文件，校验长度与摘要。
6. 取消 operation 并确认资源释放。
7. 锁屏、后台和网络切换后建立新 generation 并恢复连接。
8. 弱网行为、logcat、AndroidRuntime 和 native crash 扫描。

具体切片和测试命令以 `workflow.md` 为准。

## 10. 非当前目标

- 默认 Web 访问界面。
- 用户自建 Muxvia managed Cloud provider。
- KCP、QUIC 或替代 WebRTC 传输框架。
- Relay Mesh、全球多区域高可用和无中断动态换路。
- 多区域计费平台、通用插件和未来发布工程。

这些事项不得作为当前连接纵向闭环或 reviewer PASS 的前置条件。
