# AnyTTY 项目综合审计报告

- 审计日期：2026-07-30
- 审计基线：`f0ce4dff`
- 审计范围：Go Core/CLI/TUI、协议层、Remote/Auth、Cloud Controller/Edge、React UI、Capacitor Android、构建发布、测试、依赖与部署配置
- 审计方式：源码静态审阅、并发与生命周期推演、依赖漏洞扫描、自动化测试/构建、桌面与移动视口动态检查

## 1. 执行摘要

当前代码库具备较完整的功能骨架和较强的单元测试基础，但**不建议按现状作为公网商业生产版本发布**。必须先关闭 2 项 P0：生产部署样例开启自助测试支付，以及 Relay 月度预算/撤销可被重复租约和缓存路由绕过。除此之外，P1 风险主要集中在：无界队列和 goroutine 导致的 DoS/OOM、历史与计费事件静默丢失、已建立会话不响应授权撤销、可达依赖漏洞、认证滥用防护、协议对恶意对端的资源约束，以及端到端产品承诺与当前移动端能力脱节。

UI 基础视觉、颜色对比、公开页面响应式和常规焦点样式总体合格；主要短板不是“看起来旧”，而是可用性与无障碍契约不完整：终端读屏、弹层焦点管理、移动触控目标、Android Back、错误恢复、账户找回以及移动管理表格。

代码层面的主要问题不是抽象不足，而是部分关键路径缺少明确的资源和错误语义，同时机械抽象、历史 helper、重复 clone 和基于 LOC/历史路径的守卫增加了维护成本。建议先修正确性和边界，再删除死代码和生成机械映射，暂不进行全仓架构重写。

### 发布判定

| 场景 | 结论 | 前置条件 |
| --- | --- | --- |
| 本地开发/受控演示 | 可继续 | 明确禁用真实计费承诺，限制网络暴露 |
| 小规模封闭 Beta | 有条件 | 至少完成全部 P0、依赖升级、会话撤销和资源上限 |
| 公网商业生产 | 阻断 | 完成全部 P0/P1，补齐 Linux/Android/安全质量门禁和灾难恢复验证 |

### 严重度定义

- **P0**：可直接造成计费/授权绕过、重大数据或业务损失；发布阻断。
- **P1**：高概率或高影响的安全、可靠性、数据完整性、核心体验问题；应在下一版本前修复。
- **P2**：中等风险、可扩展性或可访问性缺口；应进入近期计划。
- **P3**：局部体验、维护性、卫生或条件性风险；随模块改动清理。

## 2. 最高优先级问题

| ID | 级别 | 问题 | 主要证据 |
| --- | --- | --- | --- |
| SEC-01 | P0 | 公网部署样例无条件开启自助测试支付，普通用户可零成本获得付费权益 | `cloud/deploy/systemd/anytty-cloud-controller.service:2,13`；`cloud/controller/commerce/service.go:143-216` |
| SEC-02 | P0 | Relay 月度额度和撤销可被重复新租约、缓存 locator、长期 binding 绕过 | `cmd/anytty-cloud-controller/main.go:180-184`；`cloud/edge/runtime/relay_state.go:219-355` |
| SEC-03 | P1 | grant 撤销/到期只影响下一次握手，已建立 shell/file 会话继续有效 | `shared/remoteauth/handshake.go:352-383`；`core/server.go:938-955` |
| SEC-05 | P1 | `govulncheck` 确认 8 个可达漏洞，覆盖 Go、x/crypto、x/net、x/text | `go.mod:3-5,25-29,65` |
| REL-01 | P1 | 默认 PTY live queue 无界，高输出可持续推高堆内存直至 OOM | `core/server.go:157-169`；`core/terminal_live_queue.go:99-148` |
| REL-02 | P1 | history 写入失败被吞掉且完成位点前移，磁盘故障会静默丢历史 | `core/terminal_live_queue.go:181-229`；`core/terminal.go:694-715` |
| REL-03 | P1 | protocol server 每请求启动无上限 goroutine，认证客户端可拖垮服务 | `core/protocol_service.go:239-245,300-315` |
| REL-04 | P1 | protocol client 接受任意 channel/重复帧，可被恶意 peer 卡死或耗尽内存 | `internal/protocol/client.go:601-699`；`proto/wire/frame.go:10-16` |
| REL-05 | P1 | Relay 先删除状态/释放额度再写 durable outbox，崩溃可永久丢计费事实 | `cloud/edge/runtime/relay_state.go:318-342`；`cloud/edge/relay/server.go:297-309` |
| UX-01 | P1 | 官网承诺登录后自动发现设备，但当前移动 App 没有 Cloud 账号/设备发现闭环 | `cloud/web/src/pages/LandingPage.tsx:15-21`；`clients/mobile/src/AnyTTYApp.tsx:125-139` |
| UI-02 | P1 | XTerm 未开启 screen reader 模式，终端核心内容对读屏基本不可用 | `clients/ui/src/terminal/Terminal.tsx:909-917,2395-2405` |
| UI-03 | P1 | 多套弹层/抽屉缺 focus trap、Escape、inert 和焦点恢复 | `clients/ui/src/ui/ActionSheet.tsx:32-126`；`cloud/web/src/ui.tsx:39-54` |

## 3. 安全与隐私

### SEC-01｜P0｜生产部署样例开启 development payments

`cloud/deploy/systemd/anytty-cloud-controller.service:2,13` 明确以 online development 配置启动并传入 `--development-payments`，而 `cloud/deploy/nginx/cloud.anytty.com.conf:27-36` 将同一服务暴露到公网。普通登录用户可调用 `cloud/controller/apihttp/r7.go:192-199` 的订单创建和测试支付完成接口，最终进入 `cloud/controller/commerce/service.go:143-152,177-216` 获得 Pro/Team entitlement。

**影响**：零成本升级、资源滥用、计费与审计事实失真。

**建议**：拆分不可混用的 development/prod 单元；生产环境发现该 flag 时 fail-fast；关闭能力时不注册 handler；增加部署契约测试，确保 production 配置不包含测试支付能力。

### SEC-02｜P0｜Relay 周期预算与撤销不是强约束

Controller 给 daemon binding 固定 365 天有效期（`cmd/anytty-cloud-controller/main.go:180-184`），delegation 只包含单 lease 字节、速率、并发，没有周期、剩余预算、policy revision 或 hard expiry（`proto/cloud/v1/ticket.proto:16-22`）。客户端优先复用持久 locator（`client/adapter/cloud/dial.go:42-72`），Edge 对每次会话独立签发/续租并只维护活动并发和单租约字节（`cloud/edge/clientgateway/service.go:125-148`；`cloud/edge/runtime/relay_state.go:219-355`）。因此 Starter 用户可反复消耗新的 1GB lease，旧 binding 在 Controller 失联或撤销推送失败时仍可能工作。

**影响**：月度 5GB 等套餐限制无法形成账务强约束，撤销不及时，带宽成本失控。`CONNECTION_ARCHITECTURE.md:185` 也将其列为已知门禁。

**建议**：签名并下发包含 account、period ID、remaining、revision、hard not-after 的 Edge policy snapshot；Edge 原子扣减账户周期预算；续租不越过 hard expiry；撤销时主动关闭会话；覆盖连续多 lease、Controller 失联和撤销竞态测试。

### SEC-03｜P1｜已建立会话不响应 grant 撤销和到期

grant 默认 90 天、最长一年（`shared/remoteauth/pairing.go:22-30,98-107`）。revocation/expiry 只在握手时校验（`shared/remoteauth/handshake.go:352-383`），握手后变成不可变 scope（`remote/daemon/session.go:86-98`；`core/transport_scope.go:8-37`）。`AccessStore` 撤销只更新存储和通知 channel（`shared/remoteauth/access_store.go:532-568,620-624`），core 的连接注册表没有 grant ID（`core/server.go:938-955,1088-1123`）。

**影响**：设备丢失或授权误发后，即使用户撤销，现有 terminal/file stream 仍可持续到断网或进程重启。

**建议**：把 GrantID/ExpiresAt 带入 runtime admission，维护 grant 到连接 cancel 的索引；持久化撤销成功后关闭关联连接；按 expiry 设置 timer，并覆盖活跃 terminal/file stream 的撤销竞态。

### SEC-04｜P1｜注册和登录缺少滥用防护

公共注册/登录路由见 `cloud/controller/apihttp/server.go:158-162,400-417`。注册只做非常宽松的邮箱、密码长度和显示名检查，登录对已知用户每次执行 bcrypt（`cloud/controller/account/service.go:114-147`）；注册后立即附带 Starter subscription（`cloud/controller/postgres/account_store.go:15-31,210-213`）；Nginx 未配置 `limit_req`（`cloud/deploy/nginx/cloud.anytty.com.conf:27-63`）。

**影响**：批量假邮箱可获取免费额度并放大 bcrypt/DB 压力；已知账号面临 credential stuffing 和 CPU DoS。

**建议**：验证邮箱或邀请后再发 entitlement；限制并规范化所有字段；HTTP/gRPC 共用 per-IP、per-account 和 global token bucket，配合指数退避、监控与 Nginx 第二层限制。

### SEC-05｜P1｜8 个可达依赖漏洞

`govulncheck ./...` 确认 8 个 reachable 漏洞，包括：x/text 无效输入死循环、x/net 恶意 SVCB/HTTPS RR panic、Go ECH 隐私泄漏，以及 5 个 x/crypto SSH/RSA/DSA/FIDO 问题。可达路径进入 `vterm/vterm/vterm.go:5299`、`client/adapter/webrtc/pion/peer.go:184` 和 `client/adapter/ssh/dial.go:224`。

**影响**：不可信终端文本、SSH 服务端或网络响应可触发挂死、panic、DoS 或安全语义异常。

**建议**：最低升级到 Go 1.26.5、`golang.org/x/crypto` 0.52.0、`x/text` 0.39.0、`x/net` 0.56.0；`go mod tidy` 后跑全量、race 和 `govulncheck`。当前版本见 `go.mod:3-5,25-29,65`。

### SEC-06｜P1｜Cloud Hello 可被高权限观察者重放

Agent/Client Hello 的签名材料由发起方选择，Edge 没有 challenge 或 replay cache（`cloud/ticket/ticket.go:86-110,173-197,233-244`；`cloud/edge/agentgateway/service.go:146-161`；`cloud/edge/clientgateway/service.go:369-469`）。攻击前提是能获得 TLS 解密后的旧 Hello，例如被攻陷的终端、Edge/TLS 端点或本地插桩；普通被动窃听不足。

**影响**：可伪造 signaling generation、扰乱 presence、消耗 WebRTC/TURN/Edge 资源。最终 remoteauth 仍有 server nonce、session nonce 和 channel binding，因此不能夸大为直接 shell 接管。

**建议**：Edge 使用两阶段随机 nonce；proof 绑定 nonce、完整安全字段、Edge boot ID、短 expiry/TLS exporter，并原子标记 single-use；增加串行和并发重放测试。

### SEC-07｜P1｜Direct signaling 的预认证连接无总量上限

`remote/webrtc/direct_server.go:87-119` 在身份验证前为每个 TCP 连接启动 goroutine；静默 socket 可占用约 30 秒（`:150-160`），32 peer cap 只在完整解析后获取且会阻塞等待（`:161-181`）。

**影响**：公网或不可信 LAN 中，攻击者可用静默连接耗尽 FD/goroutine；知道 identity 的请求还能堆积 peer waiter。

**建议**：accept 后、spawn 前非阻塞获取全局/per-IP slot；缩短 pre-auth deadline；peer 满立即拒绝；压测 FD 和 goroutine 上界。现有 4MiB frame 上限是有效控制，应保留。

### SEC-08｜P2｜Android 全局允许明文和 mixed content

`clients/mobile/android/app/src/main/AndroidManifest.xml:4-5`、`clients/mobile/android/app/src/main/res/xml/network_security_config.xml:2-4` 和 `clients/mobile/capacitor.config.ts:19-21` 对 release 也全局放开明文/mixed content。本意是 loopback WebSocket，但页面没有 CSP（`clients/mobile/index.html:1-15`），WebView JS 又可读取 native bridge bearer token。

**影响**：当前尚未证实存在直接 MITM 路径，但未来引入 HTTP 资源、恶意导航或 WebView 注入时，攻击面会扩大到插件和 terminal 权限。

**建议**：release 禁止 mixed content；明文仅窄化到 loopback，或改用安全本地通道；增加导航 allowlist、严格 CSP，并检查 merged release manifest。

### SEC-09｜P2｜生产诊断默认全局采集，缺少递归脱敏

`clients/mobile/src/main.tsx:8-13` 启动全局诊断；`clients/mobile/src/nativeDebugLog.ts:11-113` monkeypatch console/window error 并直接 JSON stringify；原生侧常开 WebView console、logcat 和 ZIP 导出（`clients/mobile/android/app/src/main/java/com/anytty/app/AnyTTYDebugLog.kt:45-55,74-134,149-225,274-280`）。当前未看到直接记录 terminal 内容或 token，但会持久化机器、连接、路径等拓扑标识。

**影响**：隐私数据长期留存；未来一次误打 token/SDP 会被自动捕获并可能随诊断包分享。

**建议**：生产诊断显式 opt-in；结构化字段 allowlist 加递归 redactor；停止全局 console/logcat 捕获；采用短环形缓冲、TTL 和导出预览；加入 sentinel-secret 测试。

### SEC-10｜P2｜Android loopback bridge 的认证前帧没有硬上限

`clients/mobile/android/app/src/main/java/com/anytty/app/goclient/GoClientBridgeServer.kt:21-29,47-55,84-98` 在校验 token 前接收并复制整个 WebSocket 二进制帧，没有 frame/message 大小、未认证连接数或 auth timeout 限制。

**影响**：本机恶意 App 或网页上下文扫描到临时端口后，可用超大帧触发库分配和二次 `ByteArray` copy，造成 OOM。

**建议**：decoder 在分配前设置硬上限；限制未认证连接和 deadline；验证 Origin/subprotocol；增加超大 pre-auth frame 测试。

### SEC-11｜P2｜Cloud JSON API 直接暴露内部错误

`cloud/controller/apihttp/server.go:454-458` 原样返回 `err.Error()`；账户 store 的 insert/commit 错误可沿 `cloud/controller/postgres/account_store.go:15-31` 和 `cloud/controller/apihttp/r7.go:49-57` 到达客户端。

**影响**：重复邮箱、数据库故障等可能暴露 SQLSTATE、约束名、表名或内部拓扑；对普通用户也不可理解。

**建议**：在 repository/service 边界映射稳定 public error code；响应只带本地化消息和 correlation ID，内部详情进受控日志；加入响应不得包含表名/约束/SQL 的测试。

### SEC-12｜P2｜供应链完整性门禁不足

Gradle wrapper 没有 `distributionSha256Sum`（`clients/mobile/android/gradle/wrapper/gradle-wrapper.properties:1-7`），未发现 Gradle dependency locking/verification metadata。GitHub Actions 使用 major tag 而不是 commit SHA（`.github/workflows/windows.yml:16,18,23,61`），CI 也未运行 `govulncheck`、npm audit、Android SCA/SBOM。

**建议**：补 wrapper SHA、strict verification metadata 和 lockfile；Actions 固定 commit SHA；CI 生成 SBOM、执行 SCA，并对发布产物做 provenance/签名。

### SEC-13｜P2｜Edge verification keyset 不持久化

Edge 只在 Welcome 后把 keyset 保存到内存（`cloud/edge/runtime/server.go:366-397,436-443`）。Controller/Edge 双重重启或失联后会 fail-closed，合法 binding 也无法接入；`ARCHITECTURE.md:169` 和 `CONNECTION_ARCHITECTURE.md:186` 已记录该缺口。

**建议**：以 `0600`、原子写方式持久化已验证 key bundle/revision/expiry，加入离线 TTL 和 anti-rollback，并覆盖轮换与双重重启。

### SEC-14｜P3｜HTTP body limit 存在 off-by-one

`cloud/controller/apihttp/server.go:429-439` 只读取恰好 limit 字节，不读取 limit+1 判断超限；Nginx 同时设置 unlimited body（`cloud/deploy/nginx/cloud.anytty.com.conf:36`）。内存仍有界，但“有效 JSON + 截断空白”可能被接受。

**建议**：使用 `http.MaxBytesReader`，或读取 limit+1 并明确返回 413。

### SEC-15｜P2｜Android backup 范围过宽

`clients/mobile/android/app/src/main/AndroidManifest.xml:4-10` 启用 `allowBackup=true` 且无明确排除；endpoint registry 使用 plaintext SharedPreferences，partial download 位于 app filesDir。

**建议**：终端类应用默认关闭 backup，或用 backup rules 精确排除凭据、设备拓扑和传输临时文件。

## 4. 可靠性、并发与数据完整性

### REL-01｜P1｜默认 PTY live queue 无界

`core/server.go:157-169` 选择低延迟默认队列；`core/terminal_live_queue.go:99-148` 对 pending chunk 不设字节或条数上限，输出同时进入 live/history（`core/terminal.go:531-540`）。测试甚至明确验证低延迟模式忽略 buffer 限制（`core/terminal_live_queue_test.go:225-245`）。

**影响**：慢消费者或持续高输出命令会无界占用 heap，最终 OOM。

**建议**：默认使用有界队列；按 terminal 和 server 设置 pending byte 总额；达到上限时采用 backpressure、断开慢消费者或显式丢弃策略，并导出可观测指标。

### REL-02｜P1｜history 持久化错误被吞掉

`core/terminal_live_queue.go:217-229` 丢弃 ingest error 并推进 completed；`Flush` 仍返回 nil（`:181-214`）。Resize/Seal/Close 也有忽略错误的路径（`core/terminal.go:238-243,263-280,1004-1010`）。

**影响**：磁盘满、I/O 或事务失败时，系统表面正常但历史永久缺失，无法通过调用方或监控发现。

**建议**：定义持久化失败语义：至少记录 terminal 级 fatal/degraded 状态、停止推进 durable watermark、让 Flush/Close 返回错误，并提供重试与告警。

### REL-03｜P1｜protocol server 无界请求并发

`core/protocol_service.go:300-315` 为每个请求启动 goroutine，无 semaphore、队列或每连接 in-flight 上限；断开时再等待全部 goroutine（`:239-245`）。

**影响**：已授权但恶意或故障客户端可用请求洪泛耗尽内存/goroutine，并拖住连接清理。

**建议**：每连接和全局并发预算；队列满返回资源耗尽；为请求绑定 deadline/cancel；断开不无限等待。

### REL-04｜P1｜protocol client 对恶意 peer 的 channel/frame 防护不足

`internal/protocol/client.go:601-699` 的重复 Hello/response 可阻塞已满 channel；任意 `uint16` channel ID 可创建 256-frame buffer，缺少 channel 数和总字节上限。单 frame 最大 4MiB（`proto/wire/frame.go:10-16`）。

**影响**：恶意服务端可冻结 read loop，或通过大量未打开 channel 快速耗尽内存。

**建议**：重复控制帧直接判协议错误；只接受已登记 channel；限制 channel 数、单 channel 和全连接 pending bytes；发送到 waiter 使用非阻塞/取消感知语义。

### REL-05｜P1｜Relay durable outbox 顺序违反架构约束

架构要求先落 durable outbox（`ARCHITECTURE.md:129`），但 `cloud/edge/runtime/relay_state.go:318-332` 先删除 allocation、释放 quota 并生成随机事件，随后 `cloud/edge/relay/server.go:297-309` 才执行 `outbox.Put`。

**影响**：进程崩溃或 outbox 写失败时，usage/billing 事实不可恢复；还存在 ctx cancellation 与状态提交的竞态。

**建议**：把状态迁移和 durable event 写入同一原子边界，或先 durable append 再 apply/release；事件 ID 必须可重放、幂等；对 crash point 做故障注入测试。

### REL-06｜P1｜证书原子写因变量遮蔽忽略 Write/Sync 错误

`cloud/edge/certificate/manager.go:131-150` 在块内重新声明 `err`，导致 `Write`/`Sync` 结果没有传到后续判断，之后仍可能 Close+Rename。`staticcheck` 对此报 SA4006。

**影响**：证书或私钥状态文件可能被部分内容、未落盘内容替换，直到重启才暴露。

**建议**：使用普通赋值并逐步检查 Write、Sync、Close、Rename；注入短写和 Sync 失败测试。现有 `cloud/edge/certificate/manager_test.go:24-89` 只覆盖正常路径。

### REL-07｜P2｜ApplicationEvents 注册存在可构造死锁

`internal/protocol/client.go:218-258` 先注册容量 64 的 raw subscriber，再同步 replay pending，最后才启动 pump。与此同时 read loop 可填满 raw channel，从而阻塞注册流程。

**建议**：先启动 consumer 再注册/replay，或在同一 actor 中原子完成；增加大于 64 条 pending 加并发入站事件的测试。

### REL-08｜P2｜Edge actor 的“submit 成功”不等于状态已提交

`cloud/edge/runtime/state.go:621-647` 在调用方 ctx 取消后，已经排队的命令仍可能稍后执行；`BeginAgentSignal` 和 `CloseRelayAllocation` 因此可产生调用方看到失败但状态已经改变的 phantom commit（`cloud/edge/runtime/state.go:321-360`；`cloud/edge/runtime/relay_state.go:318-342`）。

**建议**：队列项携带 commit/result ack；执行前检查取消；对必须提交的操作使用独立 commit context，并清楚区分 queued/committed 状态。

### REL-09｜P2｜Shutdown deadline 形同虚设

Edge 在检查 ctx 前同步调用 `GracefulStop`（`cloud/edge/runtime/server.go:305-343`）；Core 直接忽略 ctx（`core/server.go:958-994`）并可能无限等待后台 flush/WG（`core/terminal.go:263-280`；`core/server.go:1108-1112`）。Controller 的 goroutine + timeout + Stop 模式（`cloud/controller/runtime/server.go:117-139`）更合理。

**建议**：统一 shutdown primitive；GracefulStop 与 ctx 并行，超时强制 Stop；所有 flush/join 接受 deadline，并在超时输出未退出组件。

### REL-10｜P2｜16 位 channel allocator 可回绕/碰撞

`core/protocol_service.go:435-476` 的 channel ID 分配缺少跨 attachment 全局占用检查；文件传输 allocator 只检查自己的 `fileChannels`（`core/file_transfer.go:422-434`）。

**建议**：统一连接级 channel registry，回绕时探测所有 active ID；限制 attachment/channel 总数，耗尽时显式拒绝。

### REL-11｜P2｜Edge State.Close 的循环嵌套错误

`cloud/edge/runtime/state.go:564-591` 把 sessionClosers 循环放在 agentWriters 循环内部：0 个 agent 时 session 不关闭，N 个 agent 时每个 session 关闭 N 次；相关 goroutine 也未等待。

**建议**：分离两个循环，用 `sync.Once`/幂等 closer 和 WaitGroup；覆盖 0、1、N agent/session 的关闭矩阵。

### REL-12｜P2｜文档宣称的资源硬限制未完整实现

`ARCHITECTURE.md:153-160` 声明资源上限；实际只有 `MaxSessions` 和 `MaxPendingSignals`（`cloud/edge/runtime/state.go:25-48,110-129`）。agent、relay allocation、transfer、generation map 等缺少完整上限或回收，`allocationNextGen`/`agentNextGen` 可持续增长。

**建议**：建立统一资源预算表，覆盖账户/连接/进程维度；所有 map 定义回收点和指标；把架构声明转成负载测试。

### REL-13｜P3｜长期运行的小型状态泄漏与时钟不一致

- `client/runtime/session_owner.go:123-131` 的 endpoint acquire lock map 不清理。
- Edge 注入了 `Now`，但 relay 仍直接调用 `time.Now`（`cloud/edge/runtime/state.go:25-31,121-126`；`cloud/edge/runtime/relay_state.go:42,48,75,254`）。

**建议**：引用计数/完成后清理 lock entry；统一使用注入时钟，保证可测的 expiry 和恢复语义。

## 5. 架构与扩展性

### EXT-01｜P2｜48 个 command 被多层手工镜像

`proto/apipb/application.proto:25-76` 有 48 个 command variant；`api_layer/core_adapter.go` 约 390 行、52 个 exported method；`client/runtime/application_session.go` 约 818 行、51 个 exported method；`api_mapping` 约 1912 行、139 个函数。新增 command 需要同步修改 capability、validation、dispatch、adapter、typed facade 和 result extraction。

**判断**：authorization、validation、transaction 边界有实际价值，不应删除；问题是 1:1 转发和 capability table 的机械手写。

**建议**：从 protobuf descriptor 生成 capability、dispatch 和 typed facade；保留手写 admission、attachment 和 file transaction 逻辑，并为生成物加一致性测试。

### EXT-02｜P2｜TUI 超大模块阻碍局部演进

生产热点包括 `tui/render/product_content.go` 3664 行、`tui/app/runtime.go` 2472 行、`tui/state/history.go` 2451 行、`tui/app/live.go` 2126 行、`tui/render/vm.go` 2024 行、`tui/app/copymode.go` 1746 行。

**影响**：状态、输入、渲染和布局规则交织，改动审查面大；现有按文件行数的测试守卫没有覆盖最严重热点。

**建议**：按用户行为/状态机拆分，不按“helper/util”拆分；先提取纯 projection、command handling 和 lifecycle，保留单一状态所有者；用圈复杂度、依赖方向和测试覆盖替代任意 LOC 门槛。

### EXT-03｜P2｜Cloud/客户端能力缺少明确产品契约

Cloud landing 宣称“同账号设备自动发现”，但当前移动端只注入 endpoint registry/runtime/pairing，首用仅能扫描服务 QR。测试还明确阻止旧 CloudAccountAdapter 回归（`clients/mobile/src/ProductShell.test.ts:7-24`）。仓库也没有可交付的桌面/iOS 客户端。

**建议**：在产品层先选择一个真实闭环：恢复 Cloud account discovery，或修改官网/套餐/引导，只承诺 QR/局域 endpoint 工作流；为官网承诺建立 capability acceptance test。

### EXT-04｜P2｜质量平台只有 Windows CI

仓库仅有 `.github/workflows/windows.yml:1-65`，没有 Linux/macOS/Android workflow，也没有 race、vet、staticcheck、govulncheck、npm audit、a11y 或发布安全扫描。

**建议**：拆成 fast PR gate 和 nightly/deep gate；至少覆盖 Linux Go、Windows packaging、Android build、Cloud E2E、静态检查、SCA；macOS 可先做定时构建。

### EXT-05｜P3｜TUI port 泄漏 DTO，部分接口没有消费方

`tui/port/history.go`、`tui/port/live.go`、`tui/port/terminal.go` 直接暴露 state/input DTO；`tui/port/live.go:23-30` 的两个 service interface 和 `tui/port/session.go:8-16` 仅嵌入单一接口且全仓没有使用方。

**建议**：删除没有消费方的接口套接口；只有在 DTO 高频变化或出现第二实现时再引入 port-owned DTO。Clock/Timer、WebRTC capability 等已有真实替换点的接口应保留。

### EXT-06｜P3｜文档策略与仓库状态漂移

仓库缺少 README、CONTRIBUTING、SECURITY 和 CHANGELOG；同时 `scripts/repository-layout-guard.sh:20-22` 把 tracked Markdown 精确锁死为 `ARCHITECTURE.md` 与 `workflow.md`，但仓库已经有 4 个 tracked Markdown，导致 `make doctor` 必然失败。

**建议**：守卫检查必需文档和禁止路径，不要禁止新增文档；补最小 README（构建/运行/架构入口）、SECURITY（报告方式与支持版本）、贡献和发布流程。将本报告纳入版本控制时需同步修正该 guard。

## 6. 过度设计、过度防御与死代码

### OVR-01｜P2｜死代码规模已经影响可维护性

本次 `staticcheck` 共报告 155 个 U1000；复核得到约 133 个生产未使用 symbol，其中 TUI 100 个、Core 18 个、CLI 10 个。示例包括整个未使用的 `core/process.go:303-388` scripted process、`core/protocol_service.go:1014-1051` 的旧 helper，以及 `tui/render/product_content.go:1229-1349,3371-3609` 的多组未使用 projection。

**判断**：这是“先抽象/保留旧路径”的实际成本，不应通过为死代码补测试来保留。

**建议**：按 package 小批删除未导出 U1000，每批跑全量测试；公开 API 可能有仓库外消费者，不能仅凭静态工具删除 exported symbol。

### OVR-02｜P3｜同一路径重复 protobuf 深拷贝

ABI 异步边界在 `client/binding/engine.go:312-339` clone 是合理的；随后 runtime、service、terminal dispatch、cancel/resource 又多次 clone（`client/runtime/application_session.go:165-177`；`api_layer/service.go:71-83,126-153`；`api_layer/terminal.go:55-100`）。

**影响**：增加分配、代码量和“谁拥有消息”的不确定性。

**建议**：写明 protobuf ownership contract，只在跨 goroutine、异步缓存和不可信调用者边界 clone；同步只读链路传 const-by-convention。

### OVR-03｜P3｜LOC guard 和历史删除断言属于脆弱防御

`tui/render/file_size_guard_test.go:9-26`、`tui/state/file_size_guard_test.go:9-31` 使用任意 LOC 阈值，却没有覆盖当前最大热点；`client/cmd_dependency_debt_test.go:14-39`、`client/dependency_guard_test.go:55-65`、`api_layer/dependency_guard_test.go:38-44` 还长期断言历史路径/符号“必须不存在”。

**影响**：鼓励移动代码而不是降低复杂度，并让测试绑定已完成迁移的历史细节。

**建议**：保留 import direction/cycle/forbidden dependency 守卫；迁移完成后删除历史 absence 断言；以复杂度、稳定接口和行为测试替代 LOC。

### OVR-04｜P3｜连接状态维护了不展示的 message

`clients/ui/src/app/MachineBrowserShell.tsx:50-75` 构造详细且硬编码英文的 `connection.message`，但 `ConnectionFlowView` 在 `:127-131` 只按 phase 映射通用文案。

**建议**：要么改成结构化、本地化且实际展示的 detail，要么从 snapshot 删除该字段，保持单一信息源。

### OVR-05｜P3｜应保留的复杂度边界

下列机制虽然复杂，但解决了真实竞态或协议约束，不应作为“过度设计”删除：

- generation fence：`client/runtime/session_owner.go:163-197,246-278,425-444`。
- Edge 单写 actor 的总体方向；需要修 commit/cancel 语义，而不是改回共享锁状态。
- WebRTC ordered/reliable validation 与 lifecycle：`remote/webrtc/channel.go:78-105`、`remote/webrtc/direct_server.go:173-190`。
- terminal live/history 双消费者模型。
- vterm 大文件中的终端语义复杂度；应按领域拆分，但不能用通用 renderer 替代。

## 7. 产品体验与交互

### UX-01｜P1｜官网承诺与移动端实际能力断裂

官网和 User Overview 表述登录 AnyTTY App 后可发现同账号设备（`cloud/web/src/pages/LandingPage.tsx:15-21`），但移动 App 当前没有账号登录/Cloud device discovery，只提供 endpoint registry、runtime 和 pairing（`clients/mobile/src/AnyTTYApp.tsx:125-139`；`clients/ui/src/app/RemoteControlApp.tsx:1110-1129`）。

**影响**：用户完成注册和付费后无法按文案闭环，是比视觉微调更严重的信任问题。

**建议**：立即统一产品承诺和现有能力；将“注册→登录 App→发现设备→连接”做成端到端 acceptance test。

### UX-02｜P1｜TUI 默认快捷键劫持常见终端按键

默认占用 Ctrl-P/R/G/O/T/W/F/V/PageUp（`tui/shortcut/defaults.go:11-20`）；1 秒双击/隐藏 KEYLOCK 逻辑（`tui/app/ui_input.go:186-218`）使首次 Ctrl-W 等不会发送给 shell，测试也固化了该行为（`tui/app/ui_input_test.go:371-410`）。

**影响**：与 shell、编辑器、tmux 的肌肉记忆冲突，用户很难判断输入被终端还是 AnyTTY 消费。

**建议**：采用单一可配置 prefix（类似 tmux）进入命令层；默认只占用极少数明确组合，并在状态栏显示当前 prefix/lock 状态。

### UX-03｜P1｜移动端冷启动失败是死路

registry 初始化错误在 `clients/mobile/src/AnyTTYApp.tsx:72-91` 被压成通用状态，`:110-123` 只显示“操作失败，请重试”，没有重试、设置、诊断或退出路径。普通浏览器预览会因原生插件不可用直接落入该状态。

**建议**：提供 retry、打开诊断、重置本地配置和返回入口；区分“非原生环境”“插件缺失”“存储损坏”；开发 Web preview 提供明确 mock adapter 或受控说明页。

### UX-04｜P2｜移动键盘用隐藏手势表达三态

Ctrl/Alt 和键盘锁依赖 300ms 双击或 400ms 长按切换 once/locked（`clients/ui/src/terminal/MobileTerminalKeybar.tsx:25-68,256-304`）；一个 `aria-pressed` 无法表达三态。swipe-across 只移动 popup 高亮，松手不发送目标键（`clients/ui/src/terminal/MobileTerminalKeybar.tsx:147-177`）。

**建议**：使用显式 off/once/locked 控件和锁图标；删除无动作的滑动反馈；为方向、符号、PgUp/PgDn 提供本地化 aria-label。

### UX-05｜P2｜Android Back 依赖英文 aria-label

`clients/mobile/src/AnyTTYApp.tsx:405-424` 通过 DOM 查询精确匹配 `Back to machines`、`Close pairing`、`Close`。

**影响**：国际化或文案改动会静默破坏原生返回键，嵌套 overlay 的关闭顺序也不可靠。

**建议**：实现显式 overlay/navigation stack，或用 context 注册 native back action；增加多语言和嵌套 sheet 集成测试。

### UX-06｜P2｜账户生命周期不完整

登录页无找回密码入口，路由也没有 reset/recovery；注册只有一个密码字段并立即登录（`cloud/web/src/pages/LoginPage.tsx:24-30`；`cloud/web/src/pages/RegisterPage.tsx:24-30`；`cloud/web/src/App.tsx:27-59`）。

**建议**：上线前实现统一响应、限频、短期一次性 token 的密码重置，并使旧会话失效；注册至少增加密码确认或清晰 show/validation。

### UX-07｜P2｜Cloud 错误恢复粗糙且暴露技术消息

`cloud/web/src/ui.tsx:56` 原样渲染 message，恢复按钮统一 `window.location.reload()`；多个 mutation 页面也直接显示 `.error.message`。

**建议**：按稳定 error code 映射用户文案；query/mutation 原地 retry；只有管理员可展开关联码和技术详情，不丢失表单上下文。

### UX-08｜P2｜品牌与国际化债务可见

移动 monogram 仍显示旧 `MV`（`clients/ui/src/app/RemoteControlApp.tsx:974-975`），桌面为 AnyTTY；多处用户和 a11y 文案仍硬编码英文，例如 `clients/ui/src/app/RemoteControlApp.tsx:805,820,1404,1417,1669`、`clients/ui/src/terminal/Terminal.tsx:2411`、`clients/ui/src/ui/ActionSheet.tsx:59`。

**建议**：统一 AnyTTY 资产；所有可见和 a11y 文案进入 locale；增加 raw JSX string 检查，技术 token 除外。

### UX-09｜P2｜Cloud 管理信息架构在移动端不可扫描

CloudShell 平铺用户 6 个、管理员 12 个入口（`cloud/web/src/shell/CloudShell.tsx:12-34`）；所有表格默认最小宽 760px，只有用户表转换成卡片（`cloud/web/src/styles.css:217-220,427-437`）。

**建议**：管理员入口按 Infrastructure、Commerce、Governance 分组；移动表格按字段优先级转为 drilldown/card，或至少 sticky 首列/操作列并提供横向滚动提示。

## 8. UI 设计与无障碍

### UI-01｜P1｜Android 禁止页面缩放

`clients/mobile/index.html:5` 设置 `maximum-scale=1.0, user-scalable=no`。

**影响**：低视力用户无法使用系统缩放，不符合现代移动无障碍预期。

**建议**：移除这两个限制，并以 200% zoom/系统大字体测试终端 chrome、sheet 和表单。

### UI-02｜P1｜终端缺少 screen reader 模式

XTerm 初始化（`clients/ui/src/terminal/Terminal.tsx:909-917`）未启用 `screenReaderMode`；外层 `role="application"`（`:2395-2405`）不能替代终端内容的可访问表示。

**建议**：提供可切换或随 assistive technology 启用的 screenReaderMode；验证焦点、行更新、输入回显和性能；不要仅依赖 `role=application`。

### UI-03｜P1｜弹层焦点生命周期碎片化

ActionSheet、PasteConfirmDialog、FilePreview、FileTransferPanel、RemoteControlApp overlay、MobileSheetPanel 和 Cloud drawer/modal 各自实现，多个版本缺少 dialog 语义、Escape、focus trap、背景 inert 和焦点恢复。一个相对完整实现已存在于 `clients/ui/src/app/MachineWorkspace.tsx:3057-3107,3330-3349`。

**建议**：收敛成共享 accessible dialog/sheet/drawer primitive；统一 overlay stack、body scroll lock 和 focus return；组件契约测试覆盖 Tab/Shift+Tab/Escape/嵌套弹层。

### UI-04｜P1｜移动终端键盘触控目标过小

`clients/ui/src/terminal/MobileTerminalKeybar.tsx:188` 的按键高 32px，单行 9 键；Fn tabs 约 28px、action 40px（`clients/ui/src/terminal/TerminalFnPanel.tsx:20-36,50-65`）。

**影响**：终端高频输入容易误触，尤其在单手、移动和辅助触控场景。

**建议**：主要触控目标至少 44x44px；通过分页、横滑分组或自定义布局解决密度，而不是继续压缩高度。

### UI-05｜P2｜全局禁止文本选择和系统 callout

`clients/mobile/src/index.css:12-20` 对 `html/body/#root` 设置 `user-select:none` 和 `touch-callout:none`。

**影响**：配对码、设备 ID、错误详情和文件预览文本无法原生选择复制。

**建议**：只限制终端手势区域和装饰 chrome；为 input/textarea/code/pre/details/preview 与 `.select-text` 恢复选择。

### UI-06｜P2｜Cloud 搜索框缺名称和可见焦点

Accounts、Audit、Orders 搜索框只有 placeholder（`cloud/web/src/pages/AccountsPage.tsx:24`、`cloud/web/src/pages/AuditPage.tsx:17`、`cloud/web/src/pages/OrdersPage.tsx:31`）；`cloud/web/src/styles.css:315-317` 去掉 outline，容器没有 `:focus-within`。

**建议**：添加真实 label 或 aria-label，并为容器提供至少 3px 的 `:focus-within` 指示。

### UI-07｜P2｜SPA 路由不更新标题和主内容焦点

`cloud/web/src/shell/CloudShell.tsx:71` 已计算 route title，`:95` 的 main 也有 `tabIndex=-1`，但没有 location change effect；只有登录/注册页设置 `document.title`。

**建议**：路径变化时更新 title，并把焦点移到 main/h1；query-only 变化避免重复打断，必要时用 live region 宣布。

### UI-08｜P2｜Cloud 移动抽屉缺导航/模态语义

`cloud/web/src/shell/CloudShell.tsx:82-91` 的菜单按钮没有 `aria-expanded`/`aria-controls`；抽屉缺 Escape、focus trap、inert 和焦点恢复。

**建议**：纳入共享 drawer primitive，移动端逐项做键盘和读屏测试。

### UI-09｜P2｜官网和认证页次要链接热区过小

360x800 动态测量中，header 登录/开始使用只有约 40px 高；品牌约 34px；登录页次要链接约 16.5px 高；footer GitHub 约 17px。

**建议**：视觉字号可保持，但用 padding 或伪元素将 hit area 扩到至少 44px。

### UI-10｜P2｜TUI 默认依赖 Nerd Font，无安全 fallback

`tui/render/glyphs.go:3-4,60-83` 用私有区字符表达关键动作；配置只允许用户逐个改 glyph（`tui/docs/config.example.yaml:144-193`）。

**建议**：提供 `glyph_preset: auto|nerd|unicode|ascii`，默认 auto 或安全 Unicode；验证字符宽度并为关键动作保留文字 fallback。

### UI-11｜P2｜缺少系统化无障碍质量门禁

未发现 axe/pa11y/Lighthouse；Cloud 缺少 dialog/drawer/search/route announce 的 a11y contract test。

**建议**：Playwright + axe 覆盖 landing、auth、user、admin 的 320/360/768/desktop；补 200% zoom、系统大字体、TalkBack/VoiceOver 冒烟测试。

### UI-12｜P3｜移动官网首屏过高

360x800 动态检查中，hero 从 y=64 延伸到约 y=1006，首屏完全看不到下一段；相关规则在 `cloud/web/src/styles.css:382-395`。桌面 1440x900 可露出下一段。

**建议**：减少移动纵向 padding/min-height，保证 AnyTTY 仍是第一视口信号，同时露出下一 section 的视觉线索。

### UI-13｜P3｜Skeleton 未正确宣布加载状态

`cloud/web/src/ui.tsx:37` 只有 `aria-label='正在加载'`，没有 `role=status`/live，视觉骨架也未 aria-hidden。

**建议**：容器使用 `role=status`、`aria-live=polite` 和隐藏文本；内容区域设 `aria-busy`，装饰骨架设 `aria-hidden`。

### UI-14｜P2｜共享 UI 的不可用占位在默认深色主题下近乎不可见

`clients/ui/src/entries/mountRemoteControlApp.tsx:23-33` 的不可用占位使用固定 `text-zinc-950`/`text-zinc-600`，默认页面却是 `#030712` 深色（`clients/ui/src/terminal/terminalSettings.ts:531-542`）。动态检查中标题几乎不可读，且没有 retry/诊断动作。

**建议**：只使用语义色 token；提供重试和错误详情入口；增加 dark/light 主题的视觉回归测试。

## 9. 性能与体积

### PERF-01｜P1｜无界 PTY 队列是首要性能风险

见 REL-01。这里不应先做渲染微优化；必须先建立内存和 pending-byte 硬边界。

### PERF-02｜P2｜移动 Web 包体过大

`npm run build` 产出的 `clients/mobile/dist` 约 18MB：主 JS 约 1.88MB（gzip 约 525KB），Three.js 约 733KB（gzip约 188KB），10 个 Nerd Font 资源单个约 1.0-2.3MB，字体总量占主要部分。多个 3D loader 已拆 chunk，但主路径和全部字体仍偏重。

**影响**：安装包、首次加载、WebView 启动和低端机内存压力增大。

**建议**：只预装一个默认字体，其余按需下载/缓存；按功能路由懒加载 terminal settings、文件预览和 Three.js；分析主 chunk 依赖并设置体积预算。

### PERF-03｜P2｜Cloud 首包超过单 chunk 警戒线

Cloud `cloud/controller/apihttp/web/app.js` 约 518KB（gzip约 150KB），Vite 报告 chunk 超过 500KB。

**建议**：按 public/auth/user/admin 路由拆分；管理员页面和重型表格延迟加载；在 CI 记录 gzip/brotli budget，避免回归。

## 10. 代码质量、测试与发布工程

### QLT-01｜P1｜`make doctor` 在干净基线上必然失败

`scripts/repository-layout-guard.sh:20-22` 只允许两个 Markdown，但当前 tracked 文件已有 `ARCHITECTURE.md`、`CONNECTION_ARCHITECTURE.md`、`PAIRING_QR_OPTIMIZATION.md`、`workflow.md`。`scripts/doctor.sh:50-55` 在其后才执行依赖和生成代码检查，因此 doctor 被过时 allowlist 提前阻断。

**建议**：立即修正守卫语义；CI 必须运行 doctor，并增加“当前 master 自洽”的测试。

### QLT-02｜P1｜静态 correctness 门禁缺失且当前失败

- `go vet ./...` 失败：4 个 protobuf lock-copy 测试问题、`tui/app/runtime.go:433` unreachable、`tui/state/history.go:707` self-assignment。
- `staticcheck ./...` 输出 340 条：155 U1000、146 条 ST 风格项和 39 条其他诊断。高价值项包括证书写错误遮蔽、Cloud verification key parse error 被覆盖、无意义循环、self-assignment 和潜在 nil 检查顺序。
- `Makefile:36-37` 的 Go gate 只有 `go test`，CI 也未补这些检查。

**建议**：先引入 correctness 集合并建立小规模 baseline；修完后再开启 unused；ST1005/ST1008 可单独渐进处理，避免风格噪声淹没真实问题。

### QLT-03｜P3｜前端根级脚本语义不一致

`package.json:32` 的 `typecheck` 只检查 UI 并以 Cloud build 代替 Cloud 独立 typecheck，单独运行时遗漏 Mobile；`build` 又遗漏 Cloud。完整的 `make test-clients` 会依次执行两者，因此当前总门禁实际上会通过 Mobile 的 `tsc` 和 Cloud 的 `tsc --noEmit`，这里的问题是脚本名称和独立使用语义不一致，而不是总门禁完全漏检。此外仍没有 ESLint/React hooks/a11y lint。

**建议**：每个 workspace 提供 `typecheck`、`lint`、`test`、`build`；根脚本统一遍历三者；开启 React hooks 和 jsx-a11y，避免以 build 隐式代替类型契约。

### QLT-04｜P2｜Android 构建依赖旧配置

Gradle 构建成功，但报告 `flatDir` 无 metadata，且使用的 deprecated features 将与 Gradle 9 不兼容。

**建议**：用正式 Maven/local project dependency 替代 flatDir；运行 `--warning-mode all` 建清单并在 Gradle 9 升级前消除。

### QLT-05｜P3｜根目录 APK 是未跟踪发布卫生风险

`app-debug.apk` 未被 git 跟踪，未构成已泄漏，但根 `.gitignore` 未覆盖它。

**建议**：构建产物统一进入 `.artifacts/android/`，根级 ignore APK，并在提交检查中禁止二进制构建物。

## 11. 自动化与动态验证结果

| 检查 | 结果 | 备注 |
| --- | --- | --- |
| `go test ./... -count=1` | 通过 | 全部 Go package |
| 选定核心路径 `go test -race` | 通过 | Core/协议/Edge 重点并发路径；不是全仓 race |
| `npm test` | 通过 | UI 224、Mobile 53、Cloud Web 3 tests |
| `npm run typecheck` | 通过 | 但根脚本遗漏 Mobile，见 QLT-03 |
| `npm run build` | 通过 | Mobile 有 >500KB chunk 警告 |
| `npm run test:i18n` | 通过 | 569 个 locale key |
| Cloud Playwright E2E | 24 通过、18 预期跳过 | 3 个 viewport；本地未连接后端 |
| Android `testDebugUnitTest assembleDebug` | 通过 | 285 tasks；有 Gradle 弃用警告 |
| Android APK boundary | 通过 | 原生边界验证脚本通过 |
| `scripts/check-generated-code.sh` | 通过 | 生成物一致 |
| `npm audit`（完整及 production） | 通过 | 472 dependencies，0 vulnerability |
| tracked secret 基础扫描 | 通过 | 只命中测试假 PEM，未发现真实凭据 |
| `govulncheck ./...` | 失败 | 8 个 reachable vulnerability，见 SEC-05 |
| `go vet ./...` | 失败 | 6 条诊断，见 QLT-02 |
| `staticcheck ./...` | 失败 | 340 条诊断，见 QLT-02 |
| `make doctor` | 失败 | Markdown allowlist 与仓库现状不一致 |

### 动态 UI 结论

- Cloud Landing 在 1440x900、360x800 均无横向溢出；登录页移动视口也无溢出。
- 常用颜色 token 的实测对比度达到 4.5:1 正文基线；公开页大多数 focus ring 清晰，搜索框例外。
- 全局 reduced-motion 规则存在；登录表单字段、密码显示、主提交按钮达到 44px。
- 移动 App 在普通浏览器因原生插件不可用进入通用失败页，无法作为有效 Web preview；真机/模拟器交互未在本次审计覆盖。

## 12. 正向结论与现有有效控制

- Cloud cookie 使用 Secure、HttpOnly、SameSite Strict，并存在 CSRF 检查（`cloud/controller/apihttp/r7.go:484-505`；`cloud/controller/apihttp/server.go:382-395`）。
- SSH host key 有 pin；两处 `InsecureSkipVerify` 均配有严格自定义证书/hostname/EKU/chain 验证，不是裸跳过 TLS。
- remoteauth 最终握手绑定 server nonce、session nonce、channel 和 capability；Cloud Hello replay 不能直接等同 shell 接管。
- frame、WebRTC ordered/reliable、generation fence、单写 actor 等防护方向正确；问题在局部上限和提交语义，不应推倒重写。
- Go、UI、Mobile 和 Cloud 已有相当数量的行为测试；Cloud E2E 覆盖多 viewport，生成代码和 Android APK boundary 均有专门守卫。
- Cloud 公开页视觉层级、颜色、响应式基础和 reduced-motion 总体合格；CLI help 信息结构清楚。

## 13. 分阶段整改路线图

### 0-7 天：发布止血

1. 关闭生产 development payments，拆分并验证 prod deployment。
2. 设计并落地 Relay 周期预算/hard expiry/revision 的最小强约束。
3. 升级 Go 与 x/crypto、x/net、x/text，清零 reachable vulnerability。
4. 修复无界 PTY queue、protocol 请求/channel 资源上限。
5. 让 grant 撤销/到期主动终止现有连接。
6. 修复 history 错误传播、Relay durable outbox 顺序和证书原子写。
7. 修复 `make doctor`，把 vet/staticcheck correctness/govulncheck 纳入 PR gate。

### 2-4 周：可靠 Beta

1. 补注册/登录限流、邮箱验证、错误码和密码找回。
2. 修 ApplicationEvents 死锁、actor phantom commit、shutdown deadline、State.Close 和 channel allocator。
3. 收紧 Android mixed content、loopback bridge、诊断日志、backup 与 CSP/navigation。
4. 对齐 Cloud 官网承诺与移动端真实闭环。
5. 收敛 dialog/sheet/drawer primitive，补终端读屏、zoom、44px 触控和 Android Back。
6. 建 Linux/Android CI、Cloud a11y E2E 和 bundle budget。

### 1-2 个季度：降低扩展成本

1. 从 protobuf descriptor 生成机械 command surface。
2. 分批删除生产 U1000 和无消费方接口，减少重复 clone。
3. 按行为拆分 TUI 热点模块，移除 LOC/历史路径式守卫。
4. 持久化 Edge keyset，补离线轮换、双重重启和灾难恢复测试。
5. 完善 README、SECURITY、CONTRIBUTING、CHANGELOG 和发布/SBOM/provenance 流程。
6. 对移动字体、Three.js 和 Cloud 路由做按需加载与体积治理。

## 14. 审计边界

本次没有连接真实 PostgreSQL、Controller/Edge/TURN 生产拓扑，没有进行主动公网攻击、长时间 soak、故障注入、全仓 race、Android 真机 TalkBack/性能、iOS 或 macOS 手工验证。Cloud 登录后页面的动态检查受本地后端未启动限制，部分判断来自源码和现有 E2E。以上边界不会改变已确认 P0/P1 的结论，但生产发布前仍需要独立渗透测试、容量测试和恢复演练。
