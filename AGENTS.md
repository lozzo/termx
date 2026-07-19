# 代理说明

## 最高工作基准

- 仓库根目录 `workflow.md` 是当前分支唯一有效的活动驱动文件。
- 本仓库内所有工作必须先读取 `workflow.md`，并以它作为范围、任务顺序、测试准入和提交规则的唯一基准。
- 当前活动主线只由 `workflow.md` 最早未完成切片决定；Go 端 Proto API 与基础 Client Engine 已完成，当前主线是把 Direct、SSH、Cloud 三种远程 Route 统一到 Go-owned WebRTC DataChannel session，并先完成 Android JNI 纵向闭环。浏览器 Web/WASM 当前冻结，不得抢占 Android 与 native Go 主线。
- 插件系统已经拆到独立分支，本分支不新增插件系统代码、协议或文档。
- `docs/remote-platform/` 是远程平台产品、架构、安全和迁移背景文档；统一 WebRTC Route 的当前决策以 `workflow.md` 为准，并由对应活动切片同步更新该目录，旧文档不得覆盖活动工作流。
- `tui/docs/multi-endpoint-transport-plan.md` 是当前多 endpoint / 多 transport 技术规划。
- `core/docs/architecture.md` 是 core-v2 技术设计基准。
- `tui/docs/architecture.md` 是 tui-v3 技术设计基准。
- `docs/development/repository-layout.md` 是目录 ownership、依赖方向和迁移边界的唯一架构基准。
- `docs/development/proto-api-architecture.md` 是 Proto API、API Layer、API Mapping、transport、插件与客户端依赖关系的唯一架构基准。
- `AGENTS.md` 只规定代理执行方式和目录职责，不替代 `workflow.md` 的范围判断。
- 若 `workflow.md` 与旧说明、聊天记录、旧代码行为或局部假设冲突，默认以 `workflow.md` 为准。

## 当前私有开发阶段原则

- 当前仓库是唯一开发真值，并且整体仍是私有仓库；公开客户端、闭源云服务、内部文档和开发工具可以在同一 monorepo 中正常开发和提交。
- 当前优先级是尽快形成用户可运行、可观察、可验证的纵向产品闭环，不是提前完成未来开源发布工程。
- 除非用户明确启动“正式开源/发布准备”切片，否则不得主动建设或扩展 public mirror、第二仓、exporter、同步工具、clean-room 仓库、public-only CI、复制白名单、发布时源码剔除流程或额外的 private 隔离层。
- 已有 public/private 目录和依赖边界只用于表达领域 ownership 与安全责任；不得仅为了未来可能开源而反复移动文件、拆 module、增加 adapter、复制源码或把 public snapshot 独立构建设为日常开发门禁。
- `private/` 目录不是当前产品完成度指标。只要没有破坏 terminal truth、CapabilityGrant、DeviceIdentity private key、DataChannel payload 等安全边界，就应优先完成真实功能，而不是继续优化物理目录隔离。
- 正式开源时再从选定稳定提交执行一次性代码审查、许可证确认、secret audit、目录复制和新 Git 历史初始化；这些工作默认延后，不得阻塞当前功能开发。
- 若 `workflow.md` 把 public snapshot、开源许可证收口或 private 物理隔离排成当前活动切片，但用户没有明确要求进入发布阶段，必须先修正 `workflow.md`，不得机械执行该切片。
- 开发阶段禁止提前优化：单区域端到端链路完成前，不做 Relay Mesh、多 transit、全球多区域高可用、复杂计费平台、通用插件、分布式状态、无中断动态换路或为假设性扩展设计的大型抽象。
- 禁止提前优化和过度优化：只允许处理当前切片完成条件、现有契约直接要求、可复现失败或准入测试已经证明的问题；不得因为未来可能需要、理论上更通用、reviewer 的纯假设场景或“顺手更完整”而扩大模型、增加通用机制、跨层能力、状态 registry 或防御性框架。
- reviewer finding 必须先由主 Agent 判断是否属于当前切片并由代码链路、契约或最小 harness 证明。无法证明现实风险、只覆盖假设性扩展或需要扩大切片才能成立的 finding，应记录为 deferred item，不得为取得 PASS 机械实现。
- 双 Agent 审查只判断当前切片是否满足 `workflow.md` 已声明的范围、契约、完成条件和测试准入。reviewer 不得以未来平台、未来规模、可选 hardening、理论性能、未排期产品能力或“可以更通用”为由给出阻塞性 `FAIL`；这类观察只能记为 deferred item，且不影响当前切片 `PASS`。
- reviewer 只有在当前切片存在可由代码链路、契约冲突、可复现行为或准入测试证明的未解决问题时才能给出 `FAIL`。仅有命名偏好、抽象建议、未来扩展、理论竞态或未进入当前完成条件的增强项时，结论必须是 `PASS`，并把建议记录为不阻塞的 deferred observation。
- reviewer 对每个阻塞 finding 负有举证责任：必须指出当前切片内的具体文件/代码链路或契约条款、可触发条件、当前可观察影响，以及能够复现或证明问题的最小测试/命令。缺少上述证据的意见不得列为阻塞 finding，不得要求主 Agent 为证明纯假设不存在而增加抽象、fallback、registry、锁或防御性机制。
- 优先实现最小但真实的纵向闭环；允许在显式 dev/staging harness 中使用内存 store、固定测试身份和本地进程装配，但不得把 dev 凭据、宽松鉴权或 fallback 带入默认生产路径。
- “文档、接口、领域模型和 fake 测试完成”不等于产品完成。活动切片的完成条件必须包含当前阶段可观察的用户行为或真实跨组件消息链路。
- 不做与当前切片无关的仓库级整理、命名统一、性能优化、发布自动化或防御性抽象；发现这类工作时记录为 deferred item，继续完成当前纵向目标。

## 自动执行模式

当用户启动 `/goal` 或要求自动推进时，按下面循环执行：

1. 读取 `workflow.md`。
2. 检查 `git status --short --branch`，确认是否存在未提交改动。
3. 如果存在未提交改动，先判断来源和范围：若只有当前文档基线改动，先运行文档准入并提交；凡不是本轮 Agent 已识别的当前切片改动，一律停止说明，除非用户明确要求接管；不得把用户或其他代理改动混入本切片提交。
4. 按 `workflow.md` 任务队列表格顺序选择最早未完成切片。
5. 如果最早未完成切片是 `阻塞`，停止并向用户说明阻塞，不得跳到后续 `待开始` 切片。
6. 如果最早未完成切片是 `待开始`，先把它改为 `进行中`，并提交或与本切片首个实现提交同切片提交。
7. 只执行该切片，不跨切片扩展范围。
8. 需要技术细节时读取 `workflow.md` 指定的当前规划文档和对应 architecture 文档。
9. 实现最小可验证改动，先补齐该切片要求的 harness，再接真实实现。
10. 运行该切片的测试准入命令。
11. 若 `workflow.md` 把该切片标记为双 Agent 审查切片，按“阶段双审查门禁”完成架构审查与代码审查；两个 reviewer 都明确 PASS 前不得提交或进入下一切片。
12. 更新 `workflow.md` 中该切片状态和必要的当前状态说明。
13. 使用中文提交信息提交本切片。
14. 若 `/goal` 仍在继续，再进入下一切片。

如果没有明确阻塞，不要停下来要求用户确认普通实现细节。若范围、语义或目录权限不清，必须先更新 `workflow.md` 或向用户说明阻塞。

## 范围规则

- 允许主动工作目录只能来自 `workflow.md` 的“当前主线允许主动修改”和“受限联动范围”。
- 不允许因为“看起来有关”自行扩散到其他目录。
- 旧 `termx-core/` 与 `tuiv2/` 已退出本分支，不再作为只读参考、legacy fallback 或默认依赖存在。
- 当前默认本地 CLI 入口必须走 `core/` 与 `tui/`；不得重新引入 `termx legacy ...`、旧 daemon、旧 TUI 或 remote legacy/fallback。
- `cmd/termx/legacy_*.go` 不得重新出现；旧本地入口已经删除。
- `cmd/termx/default_dependency_guard_test.go` 是默认入口依赖守卫；默认源文件不得 import 旧 `termx-core` 或 `tuiv2`。
- `remote/`、`clients/mobile/` 与 `clients/ui/` 是活动远程客户端资产；只能按 `workflow.md` 对应纵向切片演进，不得恢复旧 fallback。
- `client/binding/` 是 Android、未来 iOS/Desktop 的 C ABI 与预留 WebAssembly 外部边界；只能暴露序列化 Proto、opaque handle、异步事件和显式释放，不得暴露 Go pointer、core domain struct 或平台 UI 类型。WASM 当前只维持不回归，不主动开发 Web 产品入口。
- 旧 `termx-hub/`、`termx-remote/`、`web-control/` 及 remote-ui 的历史 localweb/docs 已迁入 `private/archive/termx-platform-legacy/`，只能作为只读历史资产；archive 不进入 workspace、构建脚本或 runtime。
- Hub/Relay 服务端实现位于 `private/cloud/hub/` 与 `private/cloud/relay/`；当前只保持必要逻辑依赖边界，不为未来 public repo 继续增加物理隔离工作。
- `vterm/` 是受限联动目录，只能在 terminal semantic transaction 接口、事件或 harness 需要时最小化触及。
- `internal/protocol/` 与 `proto/` 是受限联动目录，只能在 endpoint routing、history window/copy 或 semantic history contract 需要跨进程时最小化触及。
- 如果确实必须恢复旧目录或解冻目录，先修改 `workflow.md` 的范围表并说明原因；默认不允许恢复。
- 关键代码需要写简短中文注释，说明 domain owner、truth source、消息链路或失败条件。

## 目录职责

- `api_layer/`：core application API 的执行边界，负责调用编排、授权、取消、错误分类和资源生命周期；所有跨边界 request/result/event 必须使用 `proto/` 生成类型，不得定义第二套业务 DTO。
- `api_mapping/`：`generated proto <-> core domain` 以及必要的平台 projection 映射；只做确定性字段映射和结构 validation，不建立连接，不处理 framing，也不拥有 lifecycle、session、history、route 或 UI state。
- `client/endpoint/`：客户端 Endpoint/Route 持久领域、assembler、planner 与 portable contract；不负责网络 IO、credential、protocol session 或 UI。
- `client/runtime/`：跨端客户端 route race、ReadyPeerSession、generation、session owner 和 proto command/event 执行生命周期；不得自定义 application DTO，不得依赖 TUI、CLI、平台 UI 或私有 Cloud 实现。
- `client/port/` 与 `client/adapter/`：host capability 接口与 local/SSH/managed/protocol adapter；adapter 不得创建第二份 route/session truth。
- `client/binding/`：跨语言调用边界。C ABI 供 Android JNI 以及未来 iOS/Desktop wrapper 使用；WASM binding 是未来浏览器预留边界。binding 只做参数所有权、异步调度、handle/event/release 和 Proto bytes 转交，不拥有 endpoint、route、session、credential、API 或重连真值。
- `core/`：新 core 主线目录，负责 terminal lifecycle、daemon-local terminal identity、screen-backed history 模型、terminal semantic transaction 消费、`HistoryWindow`、storage/backend 与相关 harness。
- `docs/history/core/screen-app-infinite-history-final-plan.md`：旧无限历史定案，当前只在触及 history truth 时作为背景基准读取。
- `core/docs/architecture.md`：core-v2 技术设计基准。
- `vterm/`：终端语义解释来源；负责把 PTY bytes 解释成 terminal 语义事件或 transaction，不负责持有无限历史 truth。
- `tui/`：TUI 产品目录，负责 UI state、reducer/effect、AppRuntime、TerminalHost、FrameSink、workbench/layout、copy/history 投影、输入和 render；只通过 `tui/port` 与 `tui/adapter` 消费 client/core projection，不拥有 endpoint route/session、committed history 或 daemon terminal lifecycle。
- `tui/docs/architecture.md`：tui-v3 技术设计基准。
- `termx-core/`：已删除旧 core 目录；不得作为 fallback 恢复。
- `tuiv2/`：已删除旧 TUI 目录；不得作为 fallback 恢复。
- `proto/`：所有跨 core API Layer、插件、第三方客户端、官方客户端、进程和语言边界 API 的唯一 schema truth；生成代码不得手改。
- `internal/protocol/`：连接 framing、握手、channel、request correlation 和 proto payload 传输实现；不得重新定义 proto 已表达的业务 request/result/event DTO。
- `remote/`：公开 daemon WebRTC endpoint、ICE-TCP/ICE-UDP primitive、DataChannel E2E auth 与 session 接线；不承载 Cloud 账号、订阅、Hub/Relay server 或计费业务。
- `clients/ui/` 与 `clients/mobile/`：共享 UI 和移动平台壳；消费 Go Client Engine 投影，不拥有 daemon terminal truth、Endpoint/Route/session/credential 真值。Android/Kotlin 只保留 lifecycle、Keystore、安全存储、权限和薄 JNI/Capacitor adapter；TypeScript 只保留 UI，不得复制认证、重连、Proto codec、resource/session、SSH 或 WebRTC 状态机。
- `private/cloud/`：闭源 Control Plane、Companion、Hub、Relay、Web Controller 与 Cloud 移动装配；它是同一个 App 的可选 managed Route 能力，不是第二个面向用户的 App 版本。可以依赖 public contract，public namespace 不得反向依赖。
- `cmd/termx/`：Cobra、参数/target 解析、composition root、输出和退出码；不得实现网络连接、credential resolution、Hello、授权、session cache 或 cleanup。
- `shared/`：迁移期遗留 primitive/contract 容器，不得新增领域 owner；目标去向和当前允许迁移范围以 repository layout 文档和 `workflow.md` 为准。
- `testkit/`、`scripts/`、`Makefile`、`go.work`、`go.work.sum`、必要顶层说明文档：受限联动范围，只在当前切片需要时最小化触及。

## 硬语义规则

- **Proto API 强约定**：所有对插件、第三方客户端、官方客户端、CLI/TUI client runtime、跨进程服务或跨语言 binding 暴露的 API，都必须先定义在 `proto/`；任何 Go interface、dispatcher、adapter 或 binding 只能消费生成类型，不得先写 Go struct 再补 proto。
- 唯一允许的完整运行链路是 `插件/客户端 -> Go Client Engine -> transport/platform binding -> protocol framing -> generated proto -> api_layer -> api_mapping -> core`，返回方向相反。Unix Socket、WebRTC DataChannel、JNI、Swift 和预留 WASM binding 都属于 transport 或平台接入，不属于 API Mapping；任何入口不得绕过 API Layer 直接消费 core domain struct。
- 所有官方客户端和仓库提供的外部客户端接入必须复用同一套 Go Client Engine：Endpoint/Route 配置、route planning、embedded signaling、SSH tunnel、remote auth、session generation、protocol Hello、`api.execute`、Proto command/result/event、resource lifecycle、取消和重连策略属于 Go truth。Go/native 客户端直接调用，Android 通过 C ABI + JNI，未来 iOS/Desktop 通过 C ABI wrapper，未来浏览器通过 Go/WASM；平台层只能提供 secure signer/store、host lifecycle、系统权限和浏览器未来必需的 WebRTC/WebCrypto primitive。
- 所有远程业务连接最终必须进入可靠有序 WebRTC DataChannel。Direct Route 使用 daemon embedded signaling + ICE-TCP；SSH Route 使用 Go SSH client/direct-tcpip tunnel + daemon loopback ICE-TCP；Cloud Route 使用 TermX Cloud signaling + ICE-UDP 或 TURN Relay。三种 Route 成功后必须返回同一 Go-owned ReadyPeerSession，不得维护三套 application session。
- WebRTC Cloud 是唯一允许的 managed WebRTC 服务；不得建设用户自建 Hub/Relay/signaling provider。Direct embedded signaling 是 daemon 自带的近端连接能力，不是可替换的 Cloud provider。
- 用户未登录、未订阅或 Cloud 不可用时，Android/iOS/Desktop 的 Direct 与 SSH 必须继续可用；Cloud 只决定 managed Route eligibility，不得隐藏、删除或阻断本地 Endpoint。
- Android 默认不得继续维护原生 Kotlin/Java 网络连接管理器；Go 代码通过稳定 C ABI 编译为 Android native library，再由薄 JNI/Capacitor bridge 调用。除平台 API 必需适配外，不得在 Kotlin/Java 重写 Go 连接状态机。
- 浏览器 Web 当前不是默认产品入口，不得为 Web 页面、WASM consumer、浏览器 Direct/SSH 或 Web UI 可用性扩展当前切片。未来恢复 Web 时仍必须使用 Go/WASM Client Engine；纯浏览器只支持 TermX Cloud managed WebRTC，不支持 Direct ICE-TCP 或 SSH。
- Android JNI 与未来 WASM 必须保持同一窄 binding contract：共享 UI 只消费 `ProtoClientSession`、`ProtoResourceStream`、generated `apipb`、cancel/close/release、错误和 generation 失效语义。UI 不得按 JNI/WASM 或 Direct/SSH/Cloud 分叉业务 session 接口，也不得直接持有 peer/channel。
- 跨 JNI/WASM 边界的业务 payload 只能是 versioned protobuf bytes；平台可以从同一份 schema 生成语言类型，但不得手写镜像 DTO。外部资源只能以数值 opaque handle 标识，禁止跨边界传递 Go pointer、channel、interface 或内部 struct。
- binding 调用必须是可取消的异步模型。Android process/activity 重建和网络切换后必须由 Go runtime 建立新的 session generation；不得复用 stale DataChannel、SSH tunnel、resource handle 或旧授权状态。未来浏览器恢复时沿用同一规则。
- Android 锁屏或进程进入后台时，即使 JNI/Go 线程仍可运行，WebView 也视为不可消费事件；native owner 必须关闭当前 Go engine、loopback bridge、session/resource 与事件泵，禁止后台维持第二套重连或积累无界事件。WebView 恢复时必须先创建进程内严格递增的新 generation，再通知 UI 重建 binding；冻结前的 socket、operation/session/resource handle 和迟到 callback 一律失效，不得复活或重放到新 generation。
- Android 用户可用性不能只由 Go unit test、binding harness、Gradle build 或单个 API instrumentation 证明。凡 `workflow.md` 要求 Android 纵向验收，必须在仓库指定的 ARM64 Android 模拟器安装真实 APK，通过真实 App UI 完成该切片声明的用户流程；连接纵向至少覆盖添加 Endpoint、建立连接、查看 terminal 列表、打开 terminal、输入命令并验证输出、持续交互和 crash 扫描。最终 APK 验收还必须覆盖 Direct/SSH/Cloud、上传与下载文件并校验内容、取消、锁屏/后台恢复后重新连接、网络切换及 logcat/native crash 扫描。物理设备测试可以补充，但不得替代默认可复现的模拟器门禁。
- Android 最终 E2E 必须由 App UI 发起用户动作。测试夹具、daemon capture、文件系统检查、摘要计算和日志可以作为结果 oracle，但不得直接调用 Go/JNI/binding、绕过 UI 发起连接、terminal 输入、文件上传/下载或取消操作后再把结果记作 App E2E。每个流程都要记录可复现步骤、预期结果和实际证据。
- 最终 APK E2E 是发布候选产物门禁，不得用较早切片的 APK、单元测试、instrumentation、WebView/DOM 状态检查、直接 JNI/Go 调用、手工修改客户端状态或“代码路径看起来可达”替代。自动化可以通过 Android UIAutomator、ADB 输入、WebView DevTools/CDP 等方式操作真实 App UI，但连接、terminal 输入、文件传输、取消和恢复动作必须由已安装 APK 的 UI 发起。
- 最终 APK E2E 必须形成逐项证据矩阵，至少记录 APK 路径与 SHA-256、模拟器 AVD/ABI/API、Route 与网络条件、App UI 操作、结果 oracle、关键日志/产物位置、实际结果和通过/失败结论。缺少 `workflow.md` 要求的任一强制流程或证据时，切片不得标记完成，双 reviewer 也不得给出 `PASS`。
- DeviceIdentity、ClientAccessIdentity 和 SSH 私钥不得以裸字节长期暴露给 Kotlin/JavaScript。Go Client Engine 必须通过 signer/credential port 使用 Android Keystore、未来 Keychain/keychain 或 WebCrypto 等平台实现，并明确不可导出 key、签名失败和用户/系统取消语义。
- `core/` 可以拥有内部领域 struct、value object 和状态机，但这些类型不得成为插件/客户端契约，也不得为了复用而移动到所谓 shared API DTO 目录。
- `api_layer/` 的公开方法参数、返回值、command、event、stream item 和稳定错误 detail 必须来自 proto 生成类型；允许的非 proto 参数仅限 `context.Context`、内部依赖接口和不越过调用边界的资源句柄实现。
- `EndpointSessionStamp.generation` 属于 client runtime correlation truth，daemon/API Layer 不得建立第二份“当前 generation”状态。API Layer 必须使用 protocol connection 提供的原子 admission lease 校验连接存活、已协商 capability 和具体 command/resource authorization；请求中的 stamp 只用于 operation/resource fence 与结果 origin correlation。
- `api_mapping/` 是 core domain 与 proto API 之间唯一允许的字段映射位置；API Mapping 必须无状态、可测试、失败显式，不得建立连接、处理 framing、选择 route/fallback、判断权限、执行重试或修改 reducer/core-owned state。
- `proto/` schema 是 API 字段、枚举、oneof、版本和兼容语义的唯一真值。Proto 是 schema 与消息契约，不是 transport、连接管理器或主动运行层。修改 API 必须先改 proto、重新生成、补兼容/round-trip harness，再修改 API Layer、API Mapping 和 consumer。
- 禁止在 `core/api`、`client/runtime`、`internal/protocol`、`tui/port`、插件 SDK 或平台 binding 中复制 proto 业务字段形成平行 DTO；UI-only view model 和 core-only domain model 除外，但必须通过 API Mapping 显式转换。
- `internal/protocol` 只能负责 wire transport，不拥有 API 语义；method string 到 proto command 的分发属于迁移债，最终必须由 versioned proto command/event envelope 取代。
- 插件和第三方客户端只以发布的 proto schema、生成 SDK 和 API capability/version contract 为基础，不依赖仓库内部 Go package、core struct、TUI state 或私有 Cloud 类型。
- 禁止症状补丁：遇到状态错乱、输入错路由、生命周期误判或恢复异常时，必须先定位权威状态边界和消息链路，再修改模型或契约；不得用 storage scrub、fallback、定时刷新、重复 attach、局部 if 分支等方式掩盖根因。
- 禁止补丁式实现：不得为了让当前 case 通过而堆叠临时分支、局部兜底、重复同步、隐式状态修正或旧路径兼容；每次修复都必须先说清 domain owner、truth source、消息链路和失败条件，再按模型/契约补 harness 后实现。
- 多 endpoint / 多 transport 主线必须保持 endpoint 边界清晰：跨 endpoint 状态使用 `EndpointID + TerminalID` 的 `TerminalRef`，不得把裸 `TerminalID` 当成全局唯一真值。
- Endpoint 表达“当前客户端要连接的 daemon 目标”，Transport 表达“到达该 endpoint 的方式”；daemon 侧客户端连接管理与 `client/runtime` 侧 endpoint session 管理不得混成一个模型。
- TUI 不拥有 terminal lifecycle、committed history 或 history truth；history/live/input/resize 必须路由到 owning endpoint 的 daemon。
- 远程产品目录只能按 `workflow.md` 明确切片重新设计；不得通过 fallback、桥接或旧入口把 archive 中的 remote/localweb/Web Controller 路径重新引回当前 TUI/core 主线。
- Hub/Relay 可以验证云服务准入、订阅和 Relay 租约，但不能看到或判断 terminal capability；CapabilityGrant 只能在完成 DTLS DataChannel channel binding 的端到端认证握手内由 owning daemon 验证。
- 免费 local、SSH、多 endpoint 和 terminal protocol 不得依赖私有服务、账号订阅或 Relay；收费边界只建立在托管云服务能力上。
- Cloud Companion 使用专用 versioned IPC，不得恢复通用插件系统；移动端 Cloud 装配也只能提供账号、信令和 Relay primitive，不得拥有 Endpoint、PeerSession 或 terminal protocol。
- Direct/SSH/Cloud 的 WebRTC、DTLS channel binding、CapabilityGrant 与 terminal protocol 必须留在 Go Client Engine/daemon；Cloud Companion 失败只影响 managed Cloud Route。
- 当前开发阶段只维护这个 private monorepo 并正常提交；闭源代码统一进入 `private/`。正式开源时复制审核后的公开目录到全新空 Git 仓库，不复制当前私有历史，当前不建设 exporter 或双仓同步。
- R419 后，history ingest truth 的基本单位是 core-v2 authoritative physical row/cell，不是 append-only logical line、visual row、wrapped row、snapshot scrollback、grid viewport、xterm buffer row 或 DOM/canvas row。
- core-v2 `ScreenHistoryBuffer` 是 main/alt screen、physical rows、cells、cursor、scroll region、RowID、Version 和 seal-once 的 domain owner；logical line 只是 query/copy/history 阶段的 projection。
- physical row store、sealed row index、logical projection、segment cursor、storage backend、cache、adapter、TUI/App projection 不能演变成第二份历史 truth。
- `persisted` 或落盘不表示不可修改；是否可修改由 terminal/session/row lifecycle 语义决定。
- raw PTY bytes parser 不能作为 terminal 语义 owner，也不能 fallback 出第二套历史。
- core-v2 应消费 vterm 解释过程中的 semantic transaction，而不是消费最终屏幕快照。
- vterm 当前屏幕不是无限历史来源；它只能提供终端语义解释后的可记录事件和 side proof，history truth 必须由 core-v2 screen buffer 消费语义后维护。
- tmux 等价目标只覆盖真实经过 PTY 的内容；程序没有输出到 PTY 的内部状态不在目标内。
- attach、reattach、bootstrap、recovery、full replace、clear screen、resize 不得凭空创建 committed history。
- resize 不得重写 sealed physical history；普通 logical line projection 可以在展示层重新 wrap，final screen-frame 必须固定生成时宽度。
- alt-screen 不写入 primary history；纯 alt-screen transient 退出时不 commit 屏幕内容。
- primary screen app 临时进入 alt-screen 前必须 archive/hide 当前 primary frame；退出 alt 后如果出现新的 primary 输出，必须作为新的 primary frame publish，可以接回同一 session journal，但不得复活 pre-alt current frame，也不得凭空 commit alt 屏幕。
- process exit 必须按 terminal lifecycle seal 当前 primary mutable physical rows/current frame，并按分类决定是否生成 final screen-frame projection。
- default fg/bg 应保存为语义属性，由查看历史时的主题解析；明确 RGB 颜色属于内容属性，不能被后续主题替换。
- 不得为 Codex、Claude Code、htop、vim 等程序名写特殊适配；只能按终端语义和屏幕行为分类。
- panel/pane 只表达工作台槽位和连接意图：空或连接到 terminal view。terminal 是否 running/exited、退出码、退出时间、命令、restart 判断都属于 core terminal lifecycle，不得写入 workbench storage 或 pane kind。
- copy/history 是当前 TUI 的交互态，属于 `CopyModeStore`/`HistoryStore` 投影，不得作为 pane kind 或 workbench storage 状态持久化。
- tui-v3 不拥有 committed history truth，只消费 core-v2 authoritative `HistoryWindow`。
- tui-v3 copy mode 不得从本地 VTerm scrollback、snapshot totals、row ownership、LoadedRows、wrapped 拼接结果推断历史。
- tui-v3 不以 Bubble Tea 作为主运行时。
- 禁止在 tui-v3 主线引入 Bubble Tea `Program`、`standardRenderer`、`tea.Model`、`tea.Msg`、`tea.Cmd`、`tea.KeyMsg`、`tea.MouseMsg`、`bubbles` 或依赖这些 contract 的 UI 组件。
- 允许 `lipgloss/v2`、`x/ansi` 作为纯渲染/样式/ANSI 辅助；允许 `ultraviolet` 隔离在 `TerminalHost` 或 `FrameSink` 内作为终端 primitive。
- `hot/cold` 只能出现在旧模型问题说明或迁移记录中，不得作为新代码、测试 helper、内部 contract 或运行时状态命名。

## 实现纪律

- 新增或修改跨边界 API 时，顺序固定为：proto schema -> generated code -> compatibility harness -> API Layer interface/dispatcher -> API Mapping harness -> core adapter -> transport/consumer；不得颠倒顺序。
- 当前远程迁移顺序固定为：Proto Route/config contract -> portable PeerSession/pairing harness -> daemon embedded signaling + ICE-TCP -> Android JNI Direct 纵向闭环 -> Go-owned Endpoint registry -> SSH-backed ICE dialer -> endpoint share -> Cloud Route 收口 -> 删除旧路径 -> 弱网与真机准入。Web/WASM 只维持现有门禁，不进入当前顺序。
- binding 导出面必须保持窄且稳定：创建/关闭 engine 或 session、提交 protobuf command、取消 operation、轮询或订阅 protobuf event、释放 opaque resource。不得按每个业务 command 导出一组 JNI/WASM 函数。
- 如果发现现有 API 只存在 Go struct/interface 而没有 proto 定义，必须先在 `workflow.md` 登记并迁移到 proto；不得继续扩大该 Go-only API。
- 先写 domain model 和小 harness，再接真实 protocol、terminal 或 CLI 入口。
- 所有新增或修改的导出 `type`、`interface`、`struct`、导出方法和导出函数都必须写清晰、详细的中文注释；注释要说明用途、领域归属、真值来源、消息链路、失败条件或调用边界中的至少相关部分，不能只复述名字。
- 关键代码路径必须写必要中文注释，尤其是状态归属、事务边界、跨模块消息传递、历史 truth 边界、失败分支和禁止 fallback 的位置；不要用空泛注释替代模型说明。
- 代码必须按正确模型写完整：如果只能靠“再补一个判断”“再刷一次状态”“失败就 fallback”“先 scrub storage”才能成立，默认方案不合格，需要回到状态归属和契约设计重新做。
- 当前问题存在局部、直接且可验证的修复时，不得先抽取通用框架或建设未来能力；新增 abstraction 必须同时减少当前切片的真实复杂度，并且其唯一使用者不能只是为假设性复用预留。
- 当前处于开发周期，不做旧内部实现、旧 storage/协议格式、旧 snapshot/workbench schema 或旧运行时行为的兼容；需要破坏性调整时直接按新模型改，删除旧路径。
- 不为兼容旧内部实现保留双路径、适配层、桥接代码、旧格式读取分支或迁移兜底，除非 `workflow.md` 明确要求。
- 从旧实现迁移代码时，迁入新目录后必须按新边界重命名、裁剪依赖并补 v2/v3 harness。
- service 不得直接修改 reducer-owned state；必须通过 message/effect 回到主循环。
- renderer 只消费 view-model，不读 core client、history source、runtime service 或 protocol client。
- 手工编辑文件必须使用 `apply_patch`。
- 不得使用 destructive git 命令。
- 不得覆盖用户或其他代理的未提交改动；发现冲突时停下说明。

## 测试和提交

- 每个有效切片提交前必须运行 `workflow.md` 规定的测试准入命令。
- 文档-only 改动至少运行 `git diff --check`。
- 如果测试无法运行，最终说明必须写清原因。
- 每个有效变动必须提交，提交信息必须使用中文。
- 用户明确要求不要提交时，按用户最新指令执行，并在最终说明未提交。
- 一次切片尚未达到可提交状态时，先收敛切片，不要继续扩大改动面。
- 不得 amend commit，除非用户明确要求。

## 子代理使用

- 只有当用户明确要求子 Agent、审核或并行代理工作时才使用子代理。
- 子代理适合做只读审核、独立探索或互不重叠的实现切片。
- 子代理审核后的 findings 必须先本地判断并处理，再提交最终结果。

### 阶段双审查门禁

- 用户或 `workflow.md` 明确要求阶段双审查时，每个切片在实现和测试准入完成后、提交前，必须同时启动两个相互独立的只读 reviewer：一个负责架构审查，一个负责代码审查。
- 架构 reviewer 必须在当前切片范围内检查 domain owner、truth source、消息链路、失败条件、模块边界、重复真值、fallback、本切片要求删除的旧代码，以及实现是否为了局部 case 引入补丁分支。
- 代码 reviewer 必须在当前切片范围内检查行为 bug、状态竞态、输入边界、错误处理、安全/隐私、已由代码或测试证明的性能退化、测试有效性和用户可观察回归；不得只做格式或命名检查，也不得把假设性优化列为阻塞项。
- reviewer 必须基于当前阶段实现 diff、相关实现和测试给出 `PASS` 或 `FAIL`，并把结论分为“当前切片阻塞 finding”和“不阻塞的 deferred observation”。审查范围不包含 reviewer PASS 后机械写入的 `workflow.md` 状态/审查证据；没有明确结论或仍有未解决的当前切片阻塞 finding，视为 `FAIL`。仅存在 deferred observation 时必须允许 `PASS`。
- 每个阻塞 finding 必须包含具体证据、触发条件、当前影响和最小复现方式；reviewer 不得把“可能”“理论上”“未来规模下”或无法在当前契约和准入中证明的风险改写成阻塞项。主 Agent 不承担证明任意假设绝不发生的责任。
- reviewer 不得要求主 Agent 为获得 `PASS` 实现未排期能力、通用化当前单一实现、增加假设性 fallback/hardening、改造无关目录或提前处理未来 Web/iOS/Desktop/多区域能力。此类要求违反审查范围，主 Agent 应拒绝扩大切片并记录为 deferred observation。
- 主 Agent 必须独立判断并处理 findings，不能机械接受或忽略。修复任何实质 finding 后必须重新运行受影响测试，并把更新后的阶段实现 diff 交给原 reviewer 复审；架构与代码 reviewer 都明确 `PASS` 才满足门禁。
- reviewer 只读，不得直接改文件、提交或替主 Agent扩大切片。若双审查所需子 Agent 不可用，该切片标记阻塞，不得降低为单 Agent、自审或跳过。
- 两个 reviewer PASS 后只允许机械更新 `workflow.md` 的切片状态、reviewer 结论和已处理 finding 摘要，再运行 `git diff --check` 后提交；若同时修改任何实现、测试、其他文档或非审查元数据，必须重新交原 reviewer 复审。该终止规则避免“记录 PASS 本身又制造待审 diff”的无限循环。
