# 工作流：统一 WebRTC DataChannel 远程连接

## 当前结论

- `RTC005` Go-owned Endpoint registry 已完成；当前活动主线从 `RTC006` 开始。
- TermX 只有一个面向用户的 App。Direct 与 SSH 是无需登录和订阅的基础能力；TermX Cloud 是同一 App 内可选的 managed Route，提供账号目录、托管信令、ICE-UDP、TURN Relay 和跨网络能力。
- 所有远程业务连接最终统一为可靠有序 WebRTC DataChannel：Direct 使用 daemon embedded signaling + ICE-TCP；SSH 使用 Go SSH client/direct-tcpip tunnel + daemon loopback ICE-TCP；Cloud 使用 TermX Cloud signaling + ICE-UDP 或 TURN Relay。
- Local Unix 仍是本机 CLI/TUI 到本机 daemon 的本地 transport，不要求为了形式统一改成 WebRTC。
- 所有官方客户端和仓库提供的外部客户端接入，其连接对象、Endpoint/Route 配置、pairing、remote auth、session generation、Hello、Proto command/event、资源和重连真值都属于 Go Client Engine。Go/native 直接调用，Android 使用 C ABI + JNI，未来 iOS/Desktop 使用 C ABI wrapper，未来浏览器使用 Go/WASM。
- Android 通过稳定 C ABI + 薄 JNI/Capacitor bridge 使用 Go；Kotlin/Java 只提供 lifecycle、Keystore、安全存储、权限和平台 primitive。
- Web/WASM 当前冻结。仓库维持现有编译与 contract 不回归，但不建设默认 Web 访问界面、不迁移浏览器 consumer，也不允许 Web 工作抢占 Android/native Go 主线。未来恢复时必须使用 Go/WASM；纯浏览器只支持 TermX Cloud managed WebRTC。
- `ICE001` 已证明 Pion 双端仅启用 TCP4 时，真实 selected candidate pair 为 TCP，并完成 DeviceIdentity/CapabilityGrant auth、Hello、Proto API、取消 teardown、race 和 100 次连续独立建连。
- 当前实现仍有 OpenSSH 子进程 + `stdio-proxy` 历史实现和旧 App flavor 构建语义等迁移债；不得围绕这些旧路径继续打补丁。
- 新 `ssh-webrtc-tcp` contract 已生效，但 Go SSH direct-tcpip connector 要到 `RTC006` 实现；此前 adapter 必须显式 unavailable，不得把新字段解释为旧 stdio proxy 参数。

## 产品要求

### 单一 App

- 只发布一个面向用户的 App；Cloud 是其中的可选托管能力，不是独立产品或 App flavor。
- Cloud 未登录、订阅失效或服务不可用时，只使 managed Route unavailable；不得隐藏、删除或阻断 Direct/SSH Endpoint。
- Cloud 服务端可以验证账号、订阅、信令和 Relay 租约，但不能看到 terminal payload，也不能签发或判断 terminal capability。

### Endpoint 与 Route

- 一个 daemon 只有一个 Endpoint，以经过验证的 DeviceIdentity/fingerprint 归并。
- 地址、域名、SSH alias、Cloud DeviceID 和 label 都不是身份真值。
- 目标 Route contract：

```text
EndpointConfig
  identity
  connect_mode
  selection_policy
  routes[]

RouteConfig oneof
  direct_webrtc_tcp
    signaling_addresses[]
    ice_tcp_addresses[]
    advertised_addresses[]
    server_name?

  ssh_webrtc_tcp
    host / port / user
    host_key_fingerprints[]
    proxy_jump?
    credential_descriptor
    remote_signaling_address
    remote_ice_tcp_address

  managed_webrtc
    target_device_id
    account_profile_ref
    relay_mode
```

- 上述字段、枚举、版本和 share/bootstrap contract 必须先进入 `proto/`；Go、JNI 和未来 WASM 只消费生成类型。
- Endpoint registry 与 Route policy 属于 Go truth。平台可以使用不同物理存储 backend，但不得在 Kotlin/TypeScript 复制一份连接配置真值。

### 配对与分享

- `termx pair create` 分享当前 daemon 的签名 identity、一次性 PairingTicket 和可达 Route hint，用于 App 添加并授权 daemon。
- `pair create` 必须支持用户显式覆盖 Direct 公网 IP、域名、signaling/ICE-TCP 端口和 server name，以支持 LAN、FRP 和其它 TCP 映射。
- 地址覆盖只改变 locator，不得覆盖 DeviceIdentity、fingerprint、DTLS binding 或授权 scope。
- `termx endpoint share <endpoint>` 迁移 TUI/CLI 已配置的 portable Route 和本地 selection policy；导入前 App 必须展示 diff。
- share 不传 SSH 私钥、密码、Cloud token、源 credential ref、源客户端 grant、runtime winner 或 session。

### SSH

- SSH Route 使用 Go SSH client 完成 host-key 校验、用户认证和 `direct-tcpip` tunnel。
- Pion ICE-TCP 通过 SSH-backed dialer 到达 daemon loopback ICE-TCP listener。
- 最终 DataChannel、remote auth、Hello 和 Proto API 与 Direct/Cloud 完全共用。
- 新路径稳定后删除远端 `termx daemon stdio-proxy` 和进程型 OpenSSH transport，不保留 fallback。

### Cloud

- TermX Cloud 是唯一 managed WebRTC provider；不实现用户自建 Hub/Relay/signaling provider。
- managed Route 使用 Cloud signaling，优先 ICE-UDP，必要时使用 TURN Relay。
- 订阅只控制 managed Route eligibility，不进入 Endpoint、CapabilityGrant、terminal protocol 或 core truth。

### 弱网

- 当前不引入 KCP。可靠有序 DataChannel 已由 SCTP 提供重传、排序和拥塞控制；在 ICE-TCP/SSH TCP 上叠加 KCP 会形成重复可靠层。
- 弱网、文件吞吐和 head-of-line blocking 在纵向链路完成后、删除旧路径前统一验收；不得现在提前设计替代传输框架。

## 架构边界

```text
Android / TUI / CLI / future iOS/Desktop / future Web
                         |
                         v
                  Go Client Engine
  Endpoint registry / planner / pairing / PeerSession / generation
                         |
          +--------------+----------------+
          |              |                |
     Direct connector  SSH connector   Cloud connector
     embedded signal   SSH tunnel       Cloud signaling
       ICE-TCP          ICE-TCP         ICE-UDP/TURN
          +--------------+----------------+
                         |
               reliable ordered DataChannel
                         |
        remote auth -> Hello -> generated Proto API
                         |
            api_layer -> api_mapping -> core
```

- `client/endpoint`：Endpoint/Route/config/share contract、assembler 和 planner truth。
- `client/runtime`：attempt、ReadyPeerSession、winner、generation、lease、取消和 replacement truth。
- `client/adapter`：Direct/SSH/Cloud route connector；不能选择其它 Route 或持有第二份 session truth。
- `remote/`：daemon embedded signaling、ICE mux、Pion/DataChannel primitive 和 daemon auth 接线。
- `client/binding`：Proto bytes、opaque handle、异步 event/cancel/close/release；不拥有业务或连接真值。
- `clients/mobile`：Android UI/platform shell 与薄 JNI adapter。
- `clients/ui`：Android 当前使用的共享 UI；browser-specific runtime、entry 和 WASM consumer 当前冻结。
- `private/cloud`：Cloud 账号、信令、Hub、Relay 和订阅服务；只在 Cloud 切片主动修改。
- `cmd/termx`：命令参数、composition root 和输出；网络状态机必须位于 Go client/remote domain package，不得堆在 Cobra command 内。

## 当前允许范围

- 主动范围：`AGENTS.md`、`workflow.md`、`docs/remote-platform/`、`docs/development/`、`proto/`、`client/{endpoint,runtime,port,adapter,binding}/`、`remote/`、`cmd/termx/`、`clients/mobile/` 和当前切片对应 tests。
- Android 共享 UI 联动：`clients/ui/` 只允许为 Android 当前用户流程最小修改；browser entry、browser runtime、WASM loader/worker 和默认 Web 页面冻结。
- 受限联动：`internal/protocol/`、`api_layer/`、`api_mapping/`、`core/`、`shared/{transport,remoteauth}/`、`scripts/`、`Makefile`、`go.work*`，仅在当前切片真实消息链路需要时触及。
- Cloud 专属范围：`private/cloud/` 只有 `RTC009` 可以主动修改。
- `RTC001` 例外只允许对 `tui/state` 和 `private/cloud` 测试做 generated Route contract 引起的机械编译同步；不得改变 TUI 行为、Cloud eligibility、signaling、Relay 或私有服务逻辑。
- 禁止范围：插件系统、`private/archive/`、多区域 Cloud、计费平台扩张、iOS/Desktop 产物、默认 Web 访问产品、KCP/QUIC 替代层和开源发布工程。

## 任务队列

| ID | 状态 | 内容 | 完成条件 |
| --- | --- | --- | --- |
| BASE001 | 已完成 | 产品与工作流基线 | 单一 App、Go-owned clients、统一 DataChannel、Cloud 边界、Web 延后和后续切片写入 AGENTS/workflow；删除旧活动噪声 |
| RTC001 | 已完成 | Proto Route/config contract | `EndpointRouteConfigV1` oneof、`EndpointConfigV1`、`EndpointRegistryV1`、portable credential descriptor 和地址覆盖已生成；Go/YAML v3/parser/assembler/planner/CLI 已迁移；旧 Route enum/name 无 alias；descriptor、round-trip、unknown-field、生成代码、架构文档与 doctor 通过 |
| RTC002 | 已完成 | 通用 PeerSession 与 pairing | Local/Direct/SSH/Cloud 统一使用 `PeerConnector.Connect -> ReadyPeerSession`；planner 支持平台声明的四类 connector 同组竞速；`PairingService` 统一 Endpoint pin、实际 DTLS binding、PairingTicket handshake 和 exact-close；三类远程 fake connector、managed Pion E2E、cancel、stale generation、唯一 winner 与 race 通过 |
| RTC003 | 已完成 | daemon embedded signaling + ICE-TCP | versioned Direct signaling Proto、一次性 expiry/replay/pin admission、DeviceIdentity 签名 answer、共享 Pion TCPMux、Go Direct connector、DTLS-bound auth、Hello、Proto API、SessionClose 和有界 peer admission 已完成；真实 TCP candidate、篡改拒绝、取消、100 次连续建连/listener cleanup 与 race 通过；未接 App |
| RTC004 | 已完成 | Android Direct 纵向闭环 | `pair create` QR -> Android JNI -> Go Direct connector -> PairingTicket -> client-bound grant -> terminal list/attach；无 Cloud 登录可用；锁屏/恢复 generation 正确；ARM64 模拟器真实 APK 已完成扫码导入、terminal 输入输出、持续交互与 crash scan |
| RTC005 | 已完成 | Go-owned Endpoint registry | `enginehost` 已拥有 registry load/get/upsert/delete、identity pin、pairing credential 补偿和 unreferenced credential cleanup；Android/Web 只保存 opaque Proto bytes，UI 只缓存 projection；race、失败事务、5 个 instrumentation、模拟器冷启动恢复和重复真值扫描通过 |
| RTC006 | 待开始 | SSH WebRTC TCP | Go SSH client、host-key/credential port、direct-tcpip backed ICE dialer 和真实 sshd E2E；完成后删除 OpenSSH 子进程与远端 `stdio-proxy` |
| RTC007 | 待开始 | 地址覆盖、LAN 与 FRP | `pair create` 支持 signaling/ICE 地址与端口覆盖、自动 LAN seed 和安全预览；真实 LAN 与 TCP 映射 E2E 证明 locator 变化不改变 identity pin |
| RTC008 | 待开始 | Endpoint share | 实现 CLI/TUI 同源 `endpoint share`、一次性 TLS share session、receiver proof、Route/policy diff、config-only 和 App 原子导入 |
| RTC009 | 待开始 | Cloud Route 收口 | 现有 managed WebRTC 接到统一 PeerSession；单一 App 内按账号/订阅决定 eligibility；Cloud logout/failure 不影响 Direct/SSH |
| RTC010 | 待开始 | 删除旧路径与最终验收 | 删除 managed-only pairing、旧 Route、旧 App flavor 分支、重复平台真值和旧 proxy；在 Android 模拟器完成最终 APK 的 Direct/SSH/Cloud、LAN/TCP mapping、terminal 交互、文件传输、弱网、lifecycle 与双 Agent 审查 |
| WEB001 | 延后 | Web/WASM 产品恢复 | 仅用户明确恢复 Web 后启动；Go/WASM + Cloud managed WebRTC，纯浏览器不支持 Direct/SSH |

## 测试准入

### Android APK 端到端基线

- Android 纵向切片和 `RTC010` 必须构建并安装真实 APK 到仓库指定的 ARM64 Android 模拟器；不能只运行 Go、JNI、TypeScript 或 Gradle 的隔离测试。
- 自动化流程必须从真实 App UI 驱动：添加或扫码导入 Endpoint -> 建立 Direct/SSH/Cloud 对应连接 -> 查看 terminal 列表 -> 打开 terminal -> 输入命令并等待可识别输出 -> 继续发送输入并验证交互状态。
- 最终验收还必须从 App UI 完成文件上传与下载，并按文件长度和摘要校验内容；必须覆盖取消或失败后资源释放，不能只证明文件 API 可调用。
- 必须覆盖锁屏、后台或等价 Activity/WebView freeze -> 旧 generation/handle 失效 -> 恢复后新 generation 重建 -> terminal 重新连接，并扫描 logcat、`AndroidRuntime`、native crash 和无界等待。
- 测试夹具可以准备 daemon、terminal command 和校验文件，但不得绕过 APK UI 直接调用 Go/JNI 来冒充用户端到端结果。物理设备只作为补充，不替代模拟器门禁。

- `BASE001`：文本守卫确认 AGENTS/workflow 只定义单一 App、Web/WASM 延后且 Direct/SSH/Cloud 统一使用 WebRTC DataChannel；`git diff --check`。
- `RTC001`：generated-code check；Proto round-trip、unknown-field、descriptor compatibility；Endpoint parser/assembler/planner tests；旧 Route enum/name 扫描；`make doctor`；`git diff --check`。
- `RTC002`：PeerSession/pairing unit + race；Direct/SSH/Cloud fake connector exact-close、cancel、stale generation 和唯一 winner harness；managed-only import owner 扫描；`make doctor`；`git diff --check`。
- `RTC003`：ICE-TCP candidate protocol assertion；auth/Hello/Proto API；signaling expiry/replay/pin mismatch；100 次建连、取消和 listener cleanup；相关 race；`make doctor`；`git diff --check`。
- `RTC004`：mobile build、`:app:assembleDebug`、`:app:connectedDebugAndroidTest`；ARM64 Android 模拟器安装真实 APK，并从 App UI 完成无 Cloud 扫码、连接、terminal 列表、打开 terminal、输入命令、验证输出、持续交互、锁屏恢复和 crash scan；`make doctor`；`git diff --check`。
- `RTC005`：registry transaction/race、identity conflict、credential atomicity、Android process recreation；平台源码扫描禁止第二份 Endpoint/Route/session registry；mobile tests/build；`git diff --check`。
- `RTC006`：隔离真实 sshd、host-key pin、key/password credential、SSH-backed ICE selected TCP pair、auth/Hello/API、tunnel cancel/cleanup；扫描确认新路径不启动 `stdio-proxy`；`make doctor`；`git diff --check`。
- `RTC007`：LAN 与 TCP mapping harness；二维码 route override round-trip；错误 advertised address、identity mismatch、过期 ticket 和端口不可达 fail closed；Android 模拟器经真实 TCP 映射完成 App E2E；`git diff --check`。
- `RTC008`：ShareSessionOffer/Bundle compatibility、一次消费、过期、receiver proof、config diff、config-only、secret 扫描；CLI/TUI/Android E2E；`git diff --check`。
- `RTC009`：managed direct/Relay E2E；未登录/无订阅/Cloud 故障只过滤 managed Route；Direct/SSH regression；Cloud 私有边界扫描；`git diff --check`。
- `RTC010`：全仓 Go/race、Android tests/build/instrumentation；ARM64 Android 模拟器安装最终 APK，并从真实 App UI 完成 Direct、SSH、Cloud、LAN/TCP mapping、terminal 列表、terminal 打开、命令输入/输出、持续交互、文件上传/下载内容校验、取消、锁屏/后台恢复和网络切换；执行弱网与 crash 扫描；旧路径/重复真值/fallback 扫描；双 reviewer 只按当前切片既定完成条件审查，不得以提前优化或未来能力阻塞，架构与代码 reviewer 均 PASS；`make doctor`；`git diff --check`。

## 执行规则

1. 每轮先读取 `AGENTS.md` 和本文件，再检查 `git status --short --branch`。
2. 只执行任务队列中最早的 `进行中` 或 `待开始` 切片；`延后` 不属于活动队列。
3. 待开始切片先标记 `进行中`；没有明确阻塞时不请求普通实现确认。
4. 新增或修改跨边界字段时固定执行 `proto -> generated -> compatibility harness -> runtime/adapter -> binding/platform consumer`。
5. 先做最小真实纵向 harness，再接生产入口；不得只以 fake、接口或文档宣布完成。
6. 不为旧内部 Route、storage、proxy、App flavor 或 Web runtime 保留双路径、alias、wrapper 和 fallback。
7. 每个切片运行对应准入、更新状态并使用中文提交信息提交。
8. `RTC010` 执行双 Agent 审查；其它切片只有用户或本文件明确要求时使用子 Agent。
9. reviewer 只能用当前切片的范围、契约、完成条件、可复现行为和测试证据判定 `PASS/FAIL`；未来优化建议记录为 deferred，不得扩大切片或阻塞提交。
