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

- 同一 daemon 通过 DeviceIdentity/fingerprint 归并；同一 Official App 安装通过稳定 `client_device_id` 归并。注册码、enrollment/activation flow 和 Cloud session 都只是事务，不得成为设备列表实体或下线目标。
- IP、域名、SSH host、Cloud DeviceID 和展示名称不能单独建立身份。
- 同一 Endpoint 可以同时拥有 Direct、SSH 和 Cloud Route。
- terminal 引用固定为 `EndpointID + daemon-local TerminalID`。
- Cloud device name、客户端 Endpoint label 和 Route display name 属于三个独立作用域；Cloud 只拥有账号内设备名称，客户端 USER label 最高且不被目录同步、再次 enrollment 或再次扫码覆盖，Route 名称只在所属 Endpoint 内显示。
- Route 使用 Endpoint 内稳定唯一的 `route_id` 参与选择、合并、优先级和诊断；修改名称、IP 或域名不能创建第二台机器或改变授权。

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
- 同一 Endpoint 已认证的 ReadyPeerSession/DataChannel 由 Go Client Engine 在当前客户端 generation 内持有；进入、退出或重新进入 terminal 只创建和释放 terminal resource/UI lease，不重新执行 signaling 或 ICE。
- 只有用户显式重连/改策略、Endpoint 配置变化、网络 generation 变化、App 后台冻结、鉴权撤销或底层 PeerSession 失效时，客户端才关闭旧 DataChannel 并建立递增的新 generation。
- UI 不感知 Pion、SSH tunnel、Cloud signaling 或 Relay 的内部对象。
- session replacement 必须产生新 generation；旧 handle 和迟到 callback 失效。

## 5. 配对

`muxvia pair create` 用于把当前 daemon 添加并授权到 App：

- QR/手工码只包含 128-bit 一次性 claim、DeviceIdentity public key、有效期和建立首个 pairing DataChannel 必需的有界 Route seeds；不包含 PairingTicket、scope、grant、SSH secret 或 Cloud token。
- daemon 默认监听所有 IPv4 interface，并把当前活动 RFC1918 IPv4 地址投影为可预览的 LAN signaling/ICE-TCP locator；没有可用 LAN 地址时必须由用户显式指定，不能发布 wildcard 地址。
- 零参数创建使用 Auto：Direct runtime 能发布可达 locator 时加入 Direct；只有当前 enrollment、owning Hub assignment 和 Presence session 均 READY 时才加入 Cloud；两者都可用时由 Go Client Engine 竞速，任一路径成功即完成配对。
- `pair create --route` 是唯一显式 Route 限定入口，可以重复指定 Direct LAN、Direct 显式映射、SSH 和 Cloud。常用 Direct/SSH 配置使用普通 flags，高级脚本和同 kind 多实例可使用严格 URI；FRP/公网 TCP 映射仍是 Direct Route，Cloud P2P/Relay 和 TURN UDP/TCP 仍是 managed Route 内部策略，不建立新的 Route kind。
- Cloud enabled 不等于 Cloud pairing eligible。明确 revoked/unauthenticated、旧 enrollment 或没有当前 Presence 时不得发布 Cloud seed，也不得阻断 Direct/SSH；显式 Cloud 不可用时必须在输出二维码前失败。
- 多 Hub 下 Controller 签发并保存在私有 device session 中的 `HubID/HubURL/HubRegion` 是该 enrollment 的 owning Hub 路由真值；Companion 的启动 manifest 只提供 bootstrap 目录，不能覆盖动态 assignment，否则 token audience 与请求 Hub 会不一致并被拒绝。
- Muxvia 自有分发构建把固定版本和 SHA-256 的 Cloud Companion artifact 内嵌进单个 `muxvia` 文件；首次使用时原子释放到当前用户私有的 versioned `cloud-companion/bundled/<sha256>/` 目录并再次复验 owner、权限、类型和摘要。Companion 仍作为独立进程通过专用 IPC 运行，不进入 terminal/DataChannel 进程内真值。
- App 从短码生成 Direct/SSH/Cloud pairing attempt plan，第一条完成 DeviceHello、DTLS channel binding 和 ClientAccessIdentity proof 的 DataChannel 获胜；单 Route 失败不能结束整体配对。owning daemon 只在端到端响应中返回完整签名 bundle 和 client-bound grant。
- 已配对客户端再次扫描同 DeviceIdentity 的二维码时必须展示 Route diff 并原子合并到现有 Endpoint；新 Route 添加、相同 Route 幂等、冲突 Route 确认更新，未出现的旧 Route 默认保留。不同 DeviceIdentity 即使名称和地址相同也不得合并。
- Route 更新不得扩大 CapabilityGrant scope；同一 ClientAccessIdentity 只能幂等复用或原子轮换授权，不能产生多个并行活跃 grant。
- PairingTicket 不能直接访问 terminal、history 或 file。
- QR 不包含长期 bearer grant、private key、Cloud token 或本地 credential ref。
- `muxvia pair create --text` 输出 `MXP1-...` portable claim code，默认二维码不高于 QR Version 10；`--qr-file FILE` 仍生成 owner-only 正方形 PNG。`--raw`/`--out` 只保留为本机 owner 脚本的完整 bundle 兼容入口，不得进入 App 扫码或 Web/Cloud。
- portable claim 使用有版本 marker，并仅在 raw-DEFLATE 确实缩短载荷时压缩；Go Client Engine 负责有界解压和 Proto 校验，平台 UI 不复制 codec。二维码使用 Low 纠错等级以降低 module 数，终端尺寸与 QR Version 门禁仍必须在输出前完成。
- 参数、Proto、合并和 App Route 管理的实现级规则见 `pairing-route-management-design.md`。

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
