# 统一 Endpoint、多 Route 与 App Share 迁移 `/goal` Prompt

把下面内容作为新的 `/goal` 请求发送给 Codex。该 Goal 只应在当前更早的 `workflow.md` 活动切片已经完成、相关改动已经提交后启动。

```text
/goal

目标：把 docs/remote-platform/unified-endpoint-route-refactor-plan.md 中已经审核的统一连接方案完整实现为真实产品链路。最终一个 daemon 在所有客户端中只对应一个 Endpoint；local Unix、SSH、direct TLS 和 managed WebRTC 是该 Endpoint 的多条 Route。TUI、CLI 和 Official App 使用同一 endpoint identity、route selection、默认竞速、priority、错误和 session generation 语义。Official App 不登录 Cloud 也能通过本地 direct/SSH 使用 daemon；Cloud 只叠加 managed route。TUI/CLI 可以通过 termx endpoint share 把 portable route、SSH 参数和用户确认的 selection policy 一次迁移到 App。

本 Goal 是实现任务，不以新增文档、接口、fake 或局部 demo 代替真实纵向链路。不要恢复旧 Hub/session-token、legacy remote、local/cloud/local_cloud 机器分类、Cloud-only endpoint owner、插件系统或任何 fallback。

启动门禁：

1. 每轮首先完整读取仓库根 AGENTS.md、workflow.md、docs/remote-platform/unified-endpoint-route-refactor-plan.md、tui/docs/multi-endpoint-transport-plan.md、core/docs/architecture.md、tui/docs/architecture.md、docs/remote-platform/architecture-spec.md、docs/remote-platform/security-protocol-spec.md，以及当前切片直接涉及的目录说明。
2. 检查 git status --short --branch、最近提交和当前最早未完成 workflow 切片。不得覆盖、回退、接管或混入用户/其他 Agent 的未提交改动。
3. 如果 CLOUD018 或任何排在本 Goal 之前的切片仍是进行中/阻塞/待开始，或者工作树存在未识别改动，停止并明确说明阻塞；不得修改 workflow 排序、不得跳过当前切片、不得把现有改动吸收到 CONN 提交。
4. 只有更早切片完成且工作树可控时，审计当前代码与目标方案的差异，把下面 CONN001-CONN008 以相同顺序写入 workflow.md。补齐每个切片的允许目录、受限联动范围、用户 DoD、精确测试命令和双 Agent 门禁。在同一个 Goal 基线提交中，最小修正 AGENTS.md/workflow.md/security 文档里已经被本审核方案替代的 “CapabilityGrant 只能在 DTLS DataChannel” 表述：新真值是 Grant 只能提交给 owning daemon，并且只能位于完成 channel binding 的 direct TLS 或 DTLS DataChannel 端到端认证握手内；Control Plane、Companion、Hub、Relay 和 signaling 仍永远不得接收 Grant。不得借此放宽其他安全边界。完成后使用中文提交 Goal 基线。
5. 若当前代码已经实现某项，不凭文件名或旧测试直接标记完成；必须用本 Goal 规定的模型、失败语义和真实链路重新验收。

全程硬边界：

6. Endpoint 表示 daemon 目标；Route 表示到达方式；Transport 表示一次运行时载体；Path 只表示 managed WebRTC 内部 direct/single_relay 结果。不得再次把这四个概念合并。
7. DeviceFingerprint 是跨来源、跨 transport 合并 daemon 的安全锚点。label、hostname、IP、SSH alias、Hub URL、Cloud account 和裸 DeviceID 都不能单独触发合并或换 pin。
8. 跨 endpoint 继续使用 TerminalRef{EndpointID, TerminalID}。route 变化不得改变 EndpointID、TerminalRef、terminal lifecycle、history truth 或 core-v2 owner。
9. core-v2 继续拥有 terminal lifecycle/history/file truth；TUI/App 只拥有 endpoint registry、route/session 和 UI projection。renderer 不读取网络 service，service 不直接修改 reducer-owned state。
10. 未配置 priority 时，所有 eligible route 在同一轮默认并发竞速。配置 priority 时按分组和有界 hedge delay 启动。只有完成 transport、daemon identity、authorization 和 protocol Hello 的 ReadySession 才能胜出。
11. winner 产生后必须取消并释放所有 loser：SSH process、TLS socket、PeerConnection、signaling stream、Relay reservation、protocol transport 和 pending effect。旧 SessionGeneration 的 live/history/input/file 回包全部拒绝。
12. Cloud Companion 只实现一条可取消的 managed route attempt，不拥有 SavedEndpointRegistry、SSH/direct 配置、外层竞速、CapabilityGrant 或 termx protocol session。
13. local、SSH、direct TLS、LAN discovery、daemon bootstrap、客户端 share 和已就绪 DataChannel 不依赖 Control Plane、Hub、Relay 或 Cloud 登录。Cloud 故障只能降低 managed route 可用性。
14. 不手写 SSH 协议或密码学。Go 侧复用现有 OpenSSH/成熟库边界；Android SSH 必须选用经过验证、持续维护且支持 host-key pin、取消和流式 stdio 的库，并在 workflow 记录选型依据。
15. 不保留旧内部 schema、旧 pairing payload、旧 session token、旧 machineStore 合并、旧 Cloud-only ConnectionStore 或兼容读取分支。当前仍是私有开发阶段，按新模型做破坏性迁移并删除旧路径。
16. 所有 secret 只进入明确安全边界。Cloud token、browser Cookie、Hub/Relay credential、SSH private key/password、CapabilityGrant 和 client private key 不得进入日志、指标、普通 registry、shell argv、静态二维码或 Cloud signaling。
17. daemon bootstrap、Cloud discovery 和 LAN discovery 不得覆盖客户端 priority/disabled/manual-only/label。只有用户明确确认的客户端 share import 可以修改本地 selection policy。
18. 关键导出类型、接口、方法和安全/状态路径写详细中文注释，说明 domain owner、truth source、消息链路、失败条件和禁止 fallback 的边界。

实施切片：

CONN001：统一领域模型、registry 与跨语言 contract

- 冻结 Endpoint、AccessRoute、RouteKind、SelectionPolicy、RouteAttempt、ReadySession、EndpointSession、Path、ConnectIntent、CredentialDescriptor、EndpointAssembler input/result 和 LocalDiscoveryCandidate。
- 把 shared/connection 当前一个 Config 绑定一个 Transport 的模型替换为 versioned endpoint registry；移动端使用同一 domain schema，不要求使用相同存储格式。
- 定义 EndpointBootstrapBundle v2、ClientEndpointShareBundle v1、ShareSessionOffer、Cloud ManagedDevice.device_fingerprint 和稳定错误码。
- 建立 Go/Kotlin/TypeScript 共享 fixture、strict parser、round-trip、unknown field/size limit、identity conflict 和导入交换律 harness。
- 本切片必须至少完成真实 registry 读写和现有配置入口消费新 schema；不能只增加 type。

CONN002：全局 Daemon Identity、客户端绑定授权与配对

- daemon 启动统一加载一份 DeviceIdentity，local、SSH、direct TLS 和 managed WebRTC 都证明同一 identity。
- 实现每 Endpoint ClientAccessIdentity、短期一次性 PairingTicket、带 SubjectKeyFingerprint 的 CapabilityGrant v2、ManageClientAccess 和 daemon-local PairingExchange。
- capability handshake 必须验证 daemon channel binding、grant issuer/subject、client private-key possession、expiry、scope 和 revoke；复制 grant 文本不能建立 session。
- ticket consume、client key 绑定、grant 签发和幂等 delivery receipt 是一个原子事务；普通连接/重连验证不写数据库。
- local owner 可以管理客户端授权；远程 terminal/file scope 不隐式拥有 ManageClientAccess。
- 删除新配对继续签发 bearer-only grant 的路径，补重放、并发兑换、响应丢失、错误 key、错误 fingerprint、撤销和重启恢复 harness。

CONN003：EndpointAssembler、RouteSelectionPlanner 与 TUI/CLI session owner

- 所有 Saved registry、Cloud projection、bootstrap import、manual draft 和 LAN candidate 进入统一 EndpointAssembler；相同 fingerprint + DeviceID 合并，相同 ID/不同 fingerprint 或相同 fingerprint/不同 ID 进入安全冲突。
- 实现 default full race、priority grouped hedge、manual route override、eligibility、稳定 tie-breaker、attempt cancellation 和 SessionGeneration。
- TUI EndpointManager 拆为 registry projection、route selection/session owner 和 endpoint-aware service router；不让 reducer/render/service 形成重复真值。
- CLI/TUI 先完成 local Unix 与真实 SSH 多 route 连接、切换、重连、TerminalRef 稳定和 history/live/input/file 路由。
- 同一 endpoint 的 route 配置更新不得复制 workbench、terminal 或授权状态。

CONN004：Direct TLS 与 LAN discovery

- daemon 增加显式启用、默认安全关闭公网监听的 TLS 1.3 ingress；TLS certificate 轮换不改变 DeviceIdentity pin。
- 实现 direct TLS frame transport、identity/certificate binding、client-bound capability handshake 和取消。
- 使用平台 mDNS/Bonjour primitive 提供 LAN candidate；地址只在内存 TTL cache 中参与 direct address race，最终仍验证 daemon fingerprint。
- DHCP、Wi-Fi/VPN/蜂窝切换只刷新 candidate，不改变 EndpointID、grant ref、priority 或 label；可选 last-success seed 只能低频 debounce 写入。
- 完成真实 LAN direct、错误公告、错误证书、错误 identity、地址变化和 Cloud 完全关闭的 E2E。

CONN005：Managed Cloud 收缩为普通 Route adapter

- Cloud ManagedDevice projection 返回 DeviceFingerprint；Cloud directory 只向同 fingerprint Endpoint 叠加 managed-webrtc route，不创建 Cloud machine truth。
- Companion IPC 使用短期 AttemptID + TargetDeviceID；删除客户端本地 EndpointID 和外层 selection ownership。
- managed adapter 内部继续负责 WebRTC direct/single Relay path，外层 planner 只看到一条 managed route attempt。
- loser、用户取消和 deadline 必须立即关闭 signaling/PeerConnection 并释放 Relay reservation；已就绪 DataChannel 不与后续 Companion/Hub/Control Plane 可用性绑定。
- logout/revoke 只禁用 managed route；已保存 direct/SSH route、本地 registry 和 daemon-local grant 保持不变。
- 完成 Control Plane 关闭、Hub/Relay 故障、managed loser 和 local/SSH/direct winner 的真实链路。

CONN006：`termx endpoint share` 与 TUI share action

- 实现 canonical `termx endpoint share <endpoint>`、`--routes`、`--without-policy`、`--config-only`，TUI endpoint action 复用同一个 share service。
- 默认启动短期、单次消费的 LAN TLS share listener。QR 只包含 TransferID、listener address、ephemeral certificate pin、one-time session secret 和 expiry。
- App receiver 发送 ClientAccessIdentity public key/proof；TUI 展示接收方短 fingerprint 并等待用户确认后才传输 bundle。
- share 可以迁移 direct/SSH/managed portable route、host-key pins、ProxyJump、remote socket、label 和用户确认的 priority/disabled/manual-only；不迁移 local-unix、源 EndpointID、runtime 状态或源 credential ref。
- 有 ManageClientAccess 时向 daemon 请求绑定 App key 的新 grant；不得复制源客户端 grant。daemon 不可达或无权限时 config-only 仍成功，App 明确显示 authorization_required。
- 首期默认不迁移 SSH credential body。若实现 `--include-ssh-credential`，只允许双方确认后的实时 TLS channel、逐凭据确认和目标 secure store 原子落盘；agent/hardware/Keystore key 永不导出。Cloud token 在任何 share 模式都禁止转移。
- 完成错误 TLS pin、第二次消费、过期、错误 receiver proof、用户拒绝、daemon 离线、无 ManageClientAccess 和 secret scan harness。

CONN007：Official App 统一 endpoint runtime

- Android native 成为 SavedEndpointRegistry、EndpointAssembler、bootstrap/share parser、credential resolution、route dialer/race、session lifecycle、LAN discovery 和前后台恢复 owner。
- 接入真实 direct TLS、SSH 和 managed WebRTC connector；共享 TypeScript UI 只消费脱敏 projection 和发起 intent。
- App 冷启动先显示本地 registry，再异步启动 LAN discovery 与可选 Cloud account overlay；未登录/无法访问 Cloud 时 direct/SSH endpoint 可连接。
- 首页只展示 daemon Endpoint；route 状态显示 LAN direct、SSH、Cloud、当前 winner 和 managed path。不得恢复 Local/Cloud/Local+Cloud 持久机器类型。
- 一个 scanner 按显式 intent 分发 endpoint bootstrap、endpoint share 和 Cloud activation；禁止 parser fallback。
- App 支持 share 导入 diff、route/policy 选择、credential_required、authorization_required，以及手工新增/编辑 direct/SSH 的补充入口。
- 完成 Community/Official 构建边界、Android source sync、单元测试、APK 构建和 ADB 真机 local direct、SSH、managed direct、single Relay、后台恢复与 Cloud 离线验收。

CONN008：旧路径删除、迁移守卫与全链路验收

- 删除 PairingBundle v1“导入即 Cloud”、UI schema v4 termx_pair、旧 Hub pairing claim/session token、local/hub URL race、MachineAccessClass/local_cloud、Cloud-only ConnectionStore/ManagedPairingImporter 和重复 TypeScript/native session owner。
- 删除按 label、address、裸 DeviceID 或来源类型合并的代码；添加静态守卫防止旧 symbol、旧 endpoint kind、grant-in-signaling 和 fallback 回归。
- 同一真实 daemon 同时开启 local、SSH、direct TLS 和 managed Cloud。TUI 与 App 未配置 priority 时选择同一延迟 fixture winner；配置 priority 时按同一 grouped hedge contract 执行。
- 证明 App 先扫二维码后登录 Cloud、先登录后扫码、先 share 后登录的最终 Endpoint identity/route 集合一致。
- 证明 route 切换后 TerminalRef、terminal lifecycle、history/file truth 不变，旧 generation 回包被拒绝。
- 证明 Cloud/Companion/Hub/Control Plane/Relay 分别故障时，只影响对应 managed 新连接，local/SSH/direct 和已就绪 DataChannel 按设计继续工作。
- 完成真实 SSH、LAN direct、managed direct、single Relay、share、App 手工编辑、Cloud logout、identity conflict、授权撤销、重启恢复和 secret scan 总验收。
- 所有旧路径删除、全部规定测试和真机链路通过前不得宣称迁移完成。

每个切片的执行循环：

19. 只选择 workflow.md 中最早未完成 CONN 切片。待开始先标为进行中；一次只执行一个切片，不跨阶段预做后续实现。
20. 编码前在更新中明确本切片 domain owner、truth source、消息链路、持久化边界、取消链路和失败条件。
21. 先写能证明模型和失败条件的最小 harness，再接真实实现。禁止以 storage scrub、重复刷新、隐式 fallback、旧格式兼容或 case-specific if 让测试通过。
22. 运行 workflow.md 为当前切片记录的全部测试准入。所有外部进程和长任务必须等待完成，不得留下后台测试/session。
23. 更新相关 architecture/security/plan 文档只为同步已经实现的真值，不得用文档替代缺失行为。

每阶段强制双 Agent 门禁：

24. 实现和测试完成后、提交前，同时启动两个相互独立的只读 reviewer。若 reviewer 工具不可用，将当前切片标记阻塞，不能降级为自审。
25. 架构 reviewer 提示词：
“只读审核当前 CONN 切片的阶段实现 diff、相关设计和测试；排除 reviewer PASS 后机械回填的 workflow 状态/审查证据。重点检查 Endpoint/Route/Transport/Path owner、DeviceFingerprint 真值、TerminalRef、EndpointAssembler、route race、SessionGeneration、授权 subject binding、Cloud/Companion 边界、App native/TS 边界、持久化与取消链路、重复真值、fallback、旧路径删除，以及是否为局部 case 堆补丁。按严重度给 findings；无 finding 时明确输出 PASS。不要修改文件。”
26. 代码 reviewer 提示词：
“只读审核当前 CONN 切片的阶段实现 diff 和测试；排除 reviewer PASS 后机械回填的 workflow 状态/审查证据。重点检查行为 bug、状态竞态、identity/pin 冲突、grant/ticket 重放、secret 泄漏、SSH host-key、TLS binding、竞速 winner/loser 资源释放、deadline/cancel、网络切换、App 前后台、导入覆盖、数据库写放大、错误恢复、真实测试有效性和用户可观察回归。按严重度给 findings；无 finding 时明确输出 PASS。不要修改文件。”
27. 主 Agent 独立判断 findings，修复所有有效问题，重跑受影响测试，并把更新后的实现 diff 交给原 reviewer 复审。两个 reviewer 都明确 PASS 前不得完成或提交。
28. PASS 后只允许机械更新 workflow 状态、review 证据和已处理 finding 摘要；运行 git diff --check，使用中文提交信息提交，不得 amend。若机械记录之外又修改实现、测试或其他文档，必须重新复审。
29. 提交后确认本切片文件均已提交，未混入其他切片或用户改动。若 /goal 继续，重新从启动读取步骤进入下一个切片。

最终完成标准：

- TUI、CLI 和 Official App 对一个 daemon 只显示一个 Endpoint，并可同时持有 local、SSH、direct TLS、managed WebRTC route。
- 无 priority 默认竞速；有 priority 按共享 contract 分组 hedge；首个 ReadySession 胜出且所有 loser 完整释放。
- App 不登录 Cloud、无互联网或 Control Plane/Hub/Relay 故障时，仍能通过实际可达的 LAN direct/SSH 使用已保存 daemon。
- Cloud 登录只增加 managed route；扫码、Cloud directory、LAN discovery、手工配置和 share 的不同顺序产生相同 Endpoint 聚合结果。
- `termx endpoint share` 能让 App 一次导入 TUI/CLI 已配置的 portable route 和 priority；静态 QR、日志和 Cloud 均看不到 SSH credential、Cloud token 或 CapabilityGrant。
- CapabilityGrant 绑定目标 ClientAccessIdentity；PairingTicket 一次性、可幂等交付；普通 terminal/file grant 不能转授权。
- route 切换、断线重竞速和 App 前后台恢复不改变 TerminalRef、terminal/history/file truth。
- 旧 pairing、旧 Hub/session-token、旧 machine categories、重复 connection owner 和所有 fallback 已删除并有守卫。
- workflow 规定的 Go/Kotlin/TypeScript、TUI/CLI、private cloud、Android APK、ADB 真机和真实 multi-route E2E 全部通过；无法完成真实设备或外部链路时保持 Goal 未完成并明确阻塞。
```
