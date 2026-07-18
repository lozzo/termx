# 工作流：Proto API 与跨端 Go Client Engine 迁移

## 当前结论

- `CONN003` 的 C3B-C3H 已全部完成；当前没有后续活动切片。Android 与 Web 已复用同一 runtime/managed/auth/protocol/binding 引擎，不得重新引入平台自有客户端网络真值；后续开发必须先在本文件登记新切片。
- 当前迁移允许修改 `client/{runtime,port,adapter,binding}/`、`remote/`、`internal/protocol/`、`proto/`、`clients/mobile/`、`clients/ui/` 以及对应 tests、build scripts 和必要架构文档；每个切片仍只能触及任务表规定的最小范围。
- Android 目标是 Go Client Engine 编译为 native library，通过稳定 C ABI 与薄 JNI/Capacitor bridge 调用；Kotlin/Java 不拥有连接、认证、协议、session/resource 或重连真值。
- Android lifecycle 固定采用 generation teardown：锁屏广播或进程进入后台时关闭当前 Go engine、loopback bridge、全部 session/resource 和事件泵；WebView 恢复时先创建新 engine/generation，再让 TypeScript 重连。旧 handle、迟到 callback 和冻结期间未消费事件不得进入新 generation，事件队列必须有界。
- Web 目标是同一 Go Client Engine 编译为 WebAssembly。浏览器 WebRTC、WebCrypto/IndexedDB 和页面 lifecycle 由薄 JavaScript/TypeScript platform adapter 提供；TypeScript 不保留第二套认证、协议、Proto codec、session/resource 或多 DataChannel fallback 真值。
- 迁移期 `runtimepb` 与 `wirepb` 重复 application schema 已删除；`wirepb` 只保留 framing/file resource stream payload。后续不得恢复旧 schema、codec、method string、session token、bridge 或双路径。
- 当前探针已证明 `proto/apipb`、`internal/protocol`、`client/runtime`、remote auth 和 DataChannel transport 可以编译到 `js/wasm`，Go remote client/Pion 路径可以编译到 `android/arm64`。当前 native Pion WebRTC 不能直接编译到浏览器 WASM，因此 Web 必须通过浏览器 WebRTC platform port 接入。
- Android NDK 固定为 `27.2.12479018`，Go c-shared + 薄 JNI 构建脚本同时生成 arm64-v8a/x86_64 artifact；不引入 `gomobile`，稳定 C ABI 继续作为 Android、未来 iOS/Desktop 的公共边界。
- `PA005E` 已删除 `remote/client` 集中 owner；`client/runtime.SessionOwner` 持有 generation/current winner，`client/adapter/managed` 持有 portable signaling/auth/Hello/Proto session 编排，`client/adapter/managed/pion` 是 native concrete peer。不可导出 signer、unit/race、dependency guard、Android arm64/WASM 编译以及真实 direct/single-relay devcloud E2E 已通过。
- `PA005B` 已建立独立 `bindingpb` schema/descriptor、共享 Engine/Registry opaque handle owner、有界有序事件队列、cancel/close/release、C header 与 WASM export baseline。Proto unknown-field、ownership/backpressure/race、generated compare、C syntax、Android arm64 与 `js/wasm` 编译及禁止 Go pointer/JSON/base64/业务专用导出扫描已通过；未生成 placeholder 平台库。
- `PA005N1` 已生成真实 Android c-shared Go Client Engine 与薄 JNI debug harness；arm64 模拟器中的 APK 进程完成 Pion DataChannel、remote auth、Hello、`api.execute`、storage event、cancel、重复 close、buffer copy/free、跨 JVM thread 与 teardown 证明。release APK 不包含 in-process spike daemon/fake Cloud 装配。
- `PA005N2` 已把 Android App terminal/history/live/file/storage/access/remote consumer 迁到 generated `apipb` 与 Go binding；Kotlin 只保留 Keystore、Cloud primitive、loopback bridge 和 lifecycle adapter。锁屏、后台与网络 epoch 变化会销毁整代 engine/session/resource，恢复后创建更高 generation；冻结等待显式关闭，事件队列有界。旧 Kotlin/TypeScript codec、WebRTC manager、session/resource registry、fallback 与无用 `WAKE_LOCK` 权限已删除；generated check、Go tests、UI typecheck、mobile build/cap sync、instrumentation、Community/Official release APK 和 release native boundary 均通过。
- `PA005W1` 已产出真实 Go/WASM Client Engine、Promise status ABI、共享 managed host 与 Proto WebRTC platform request/event；Android 注入 Pion，Web 只注入 RTCPeerConnection/DataChannel、WebCrypto/IndexedDB 和 lifecycle primitive。Chrome Headless 对真实 Pion daemon 完成 actual remote certificate 与 SDP SHA-256 匹配、DTLS-bound remote auth、Hello、storage `api.execute`、application event、cancel/close，并通过 `pagehide/pageshow` teardown 后严格递增 generation；负向 certificate mismatch、opaque handle/event copy、generated code、WASM build、UI typecheck/test/build 和 Android debug 编译均通过。
- `PA005W2` 已建立 Android JNI/Web WASM 共用的 `ProtoBindingClient`、`ProtoBindingConnector`、session/resource/cancel/release owner；Web 生产入口改走 `BrowserBindingRuntime`，页面冻结销毁整代 WASM engine，恢复创建新 generation 后才通知 UI 重连。旧 `browserRtcSession`、`rtcApiChannel`、`runtimeProtocol`、Hub connector、connection orchestrator、reconnect store 及对应旧架构 tests 已删除约 8,400 行；Web generated code、UI/mobile typecheck/build、WASM build、binding guards 与真实 Chrome/Pion ProtoBindingClient E2E 均通过。浏览器 Cloud HTTP primitive 缺失 edge account/device identity 时 fail closed，不从 terminal capability 或 pairing grant 推导云准入。
- `PA005R` 已删除 `runtimepb`、`wirepb` 重复 application message、旧 TypeScript terminal/file/storage/history codec、旧 Hub API/pair panel、本地 session token/pairing payload store、旧 RTC session/channel interface 与对应失效 tests/mocks，共净删除约 16,600 行；Android/Web 生产 consumer 只接受 `ProtoClientSession`。generated-code check、UI typecheck/WASM build、mobile build、API Layer/API Mapping/binding tests、架构守卫与真实 Chrome/Pion E2E 通过。该切片当时暴露的 CLI composition 缺口已在 PA006T/PA007 收口，不再是当前债务。
- `PA006T` 已把 core/protocol/client-runtime/TUI/CLI 与共享 UI tests 迁到 generated Proto contract，删除依赖旧 DTO、method codec 和旧 browser session mock 的失效测试；补齐 event subscription correlation/release、machine-events-only、file active/resume token namespace 与跨 protocol session upload resume E2E。测试同时修正 unspecified storage scope 污染 event filter、无效 upload resume credential 错误分类和 CLI current API composition 缺口；全仓 Go tests/build、关键包 race、UI typecheck/test/build、mobile build 与 generated-code check 通过。
- PA006T 已完成旧 core/protocol/client-runtime/TUI/CLI tests 的 generated Proto 迁移；后续不得再以“旧测试暂缓”为理由保留或恢复 DTO、codec 或 fake session stamp。
- PA007 remediation 先把 CLI endpoint generation、route selection、Unix dial/Hello 收回 `client/runtime` 与 `client/adapter/local`，并让 TUI terminal/workbench/clipboard 共用同一 owner-fenced session；其后的 C3B-C3G 已完成 planner/race、SSH 与 managed 多 route winner、operation stamp 和真实 E2E，C3H 只负责最终准入与双审。
- PA007 remediation 已把跨语言 ABI 升到 v3：C/JNI/WASM 业务操作统一通过 serialized `bindingpb.EngineCommand`，不再暴露 pairing/credential 专用符号；Android 与 WASM host 共享 Go runtime-owned process generation authority。
- PA007 remediation 已把同 endpoint 的 binding `OpenSession` 改为 `client/runtime.SessionOwner.AcquireRoute` 共享底层 ready session、每个 consumer 独立 lease；`managedhost` 不保存第二份 current registry。list/inventory/workspace/file 的 lease close 不提升 generation。application event consumer 按完整 subscription `ResourceHandle` correlation，多个订阅不再共享 session 级隐式广播。
- PA007 最终准入已通过 public/private Go、关键包 race、clients 130/16 tests、typecheck/build/WASM、generated/layout doctor、Community/Official Android APK 与边界校验。架构 reviewer `019f73b0-8426-7022-816c-1390532ce14a` 与代码 reviewer `019f73b0-00ef-72a2-bb96-909ebbcfe4f2` 均明确 PASS；已处理 early stream terminal event exact-once release、managed production terminal-response 接线、active operation close barrier 与 close failure 可观察性。
- `C3B` 已在 `client/endpoint` 建立纯领域 `RouteSelectionPlanner`：输入 normalized Endpoint、ConnectIntent、route override、generation、平台 route capability 和可用 credential ref 快照，输出不可变 attempt groups、累计 hedge delay 与稳定过滤诊断。自动竞速只包含 local Unix/SSH，唯一 managed route 保留单路计划；planner 不 dial、不读取 secure store/Cloud、不选择 winner。Go harness、机器可读 fixture、race、全仓 Go tests 与 doctor 通过。
- `C3C` 已把 ReadySession 发布条件冻结为 route-specific authorization、fresh DeviceIdentity proof 与 protocol Hello。local Unix 和 OpenSSH 在 Hello 后通过 versioned Proto challenge/result 证明当前 daemon 持有 Endpoint DeviceIdentity 私钥，SSH 同时依赖 OpenSSH host-key/user auth 与远端 owner-only socket；managed 继续使用 channel-bound DeviceHello/CapabilityGrant。缺失 proof、授权、Hello、pin 匹配或生命周期 signal 的 attempt 均不能进入 SessionOwner winner；schema/generated、Mapping/API Layer/core service、adapter/runtime race、全仓 Go tests 与 doctor 通过。
- `C3D` 已让 `SessionOwner` 消费 C3B attempt groups：同 Endpoint 只有一个 in-flight planner race，不同 Endpoint 可独立推进；priority hedge 只使用 `client/port.Clock`，首个完整 ReadySession 线性化后取消并等待全部 loser，迟到成功资源 exact-close 后才发布 lease。`ClientRuntime` 实现公共 `Runtime` interface，按 config key 复用 winner，并提供进程内 sticky route override、独立 consumer lease、bounded latest-state lifecycle mailbox 和 generation-safe offline 投影。runtime/endpoint race、全仓 Go tests 与 doctor 通过。
- `C3E` 已把 CLI 与 TUI native composition 接到同一 `ClientRuntime/SessionOwner`：dialer registry 包含 local Unix、OpenSSH 与 lazy managed/Pion，managed credential 只从 owner-only store 解析且 Cloud Companion 仅在 managed attempt 真正启动时打开。TUI 通过 `tui/adapter/clientruntime.EndpointEventSource` 消费同一 owner mailbox，订阅时先收到 current winner；attachment channel 和 file resource stream 经 generation-fenced ready capability，不再要求 UI/command 持有 raw framing client。旧 `runtime.SelectRoute`、`local.Connect`、raw protocol adoption 与 `NewOwnedApplicationClient` 已删除并加入守卫；关键 race、全仓 Go tests 与 doctor 通过。
- `C3F` 已让 attach candidate/commit/cleanup、input/paste、resize 与 detach 携带同一 generated Proto `EndpointSessionStamp` 和唯一 operation ID。`ApplicationSession` 保留 caller operation identity 但强制覆盖 session stamp；runtime validator 与 protocol adapter 在 attachment lookup/具体调用前拒绝 stale generation，返回 `Attempted=false`。TUI pending candidate 不再覆盖 committed channel，replaced/迟到成功只精确 cleanup 自己的资源；非幂等 input/paste 失败后不再自动 reattach 或重放 payload。workbench storage 不持久化 runtime stamp，但同进程 reload 会保留 live stamp/operation/candidate。指定 race、全仓 Go tests 与 doctor 通过。
- `C3G` 已用测试内隔离的真实 OpenSSH 9.9 `sshd`、临时 host/client key、strict known_hosts、key-only auth、真实 `ssh` 子进程和真实 `termx daemon stdio-proxy` 验证 local/SSH full race、priority hedge、显式 SSH override、进程内 sticky reconnect、loser SSH process cleanup、跨 route `TerminalRef` 稳定和旧 generation `Attempted=false`。该 E2E 修正了同 config current winner 吞掉显式 override、SSH winner 仍绑定 race context、主动 Close 返回 `signal: killed` 三个生命周期错误；指定 race、真实 E2E、全仓 Go tests 与 doctor 通过。
- `C3H` 最终准入已通过关键包 race、全仓 Go tests、repository doctor、UI 130 项与 mobile 16 项 tests、TypeScript typecheck、Go/WASM 与 UI/mobile production build、旧 route owner/重复真值/fallback/cleanup 审计。架构 reviewer `019f75bc-d09d-7af1-a16a-4466843a9337` 与代码 reviewer `019f75bc-5056-7c81-b015-a9ceac924d5f` 在复审后均明确 PASS；已处理过时 planner/runtime 文档、迟到 input attach 重放、旧 generation input/resize/attach 错误污染、stale terminal pool 投影和同 route 显式 override 未写 sticky intent。
- 用户已确立仓库级强约定：所有插件、第三方客户端、官方客户端、跨进程和跨语言 API 的唯一 schema truth 必须位于 `proto/`。
- 完整运行链路固定为 `插件/客户端 -> transport/platform binding -> protocol framing -> generated proto -> api_layer -> api_mapping -> core`，返回方向相反。Proto 是 schema/message truth，不是 transport 或主动运行层；任何入口都不得绕过 API Layer 消费 core domain struct。
- `core/api` Go DTO 路线已判定错误，必须删除；此前 `AR003B1A/AR003B1B` 结论作废，不得继续迁移或补兼容层。
- 当前仓库仍是私有开发真值。插件系统实现仍在独立分支；本分支只建设插件和第三方客户端未来必须依赖的公共 Proto API 基础，不恢复插件 runtime。

## 已完成基线

- Endpoint/Route registry、assembler 与 portable bootstrap/share contract 位于 `client/endpoint`。
- `client/runtime` 已定义 connection/session generation、attempt、ReadySession、错误和 Clock 基础 contract。
- TUI 已拆为 `tui/port`、`tui/adapter/*`、`tui/testkit`，并有依赖守卫。
- CLI concrete dependency 债务已有冻结清单；当前明确编译缺口不得用 stub、fallback 或旧 helper 恢复。
- `proto/wirepb`、`proto/remoteauthpb`、`proto/cloudpb` 等已有 schema 是迁移输入，不代表当前 API ownership 已经清晰。

## Proto API 硬边界

```text
plugins / CLI / TUI / mobile / desktop / web / third-party clients
       |
       v
transport or platform binding
  Unix Socket / TCP+TLS / SSH / WebRTC DataChannel / JNI / Swift / WASM
       |
       v
protocol framing
  Hello / channel / correlation / payload transport
       |
       v
generated proto command / result / event
       |
       v
api_layer
  application dispatch / auth / session fence / cancel / lifecycle / stable errors
       |
       v
api_mapping
  generated proto <-> core domain; stateless and deterministic
       |
       v
core domain truth
```

- `proto/` 拥有 API message、enum、oneof、command/event envelope、capability、version 和兼容语义。
- `api_layer/` 只使用 proto 生成 request/result/event；不定义平行业务 DTO，不处理 transport framing。
- `api_mapping/` 是 generated proto 与 core domain 的唯一字段映射点；它不是 transport，不拥有连接、framing、状态、权限、route、fallback、重试或 session。
- `internal/protocol/` 只保留 framing、Hello、channel、correlation、payload transport 和连接级错误；不得重新拥有 proto 业务字段。
- `core/` 可以保留内部领域类型，但不得把内部类型作为客户端/插件 API。
- `client/runtime` 可以拥有 endpoint/session stamp、opaque handle 和 runtime lifecycle，但业务 command/result/event 必须来自 proto。
- `client/port` 定义 WebRTC/DataChannel、signer/credential、Cloud signaling、clock/random 和 host lifecycle 等平台能力；接口只表达能力和失败语义，不复制 Proto 业务字段。
- `client/adapter` 把 Pion、浏览器 adapter callback、protocol 和平台 credential/signaling primitive 接入 runtime；adapter 不拥有 route/session/API truth。
- `client/binding` 只暴露 serialized protobuf、opaque numeric handle、异步 event/cancel/release；不得暴露 Go pointer、core struct、平台 UI 类型或按业务 command 扩张的函数面。
- `tui/port` 与 UI state 可以拥有 UI-only view model；凡表达 daemon/client application API 的字段必须由 proto 经 API Mapping 投影，不得复制成另一份契约真值。
- 所有 schema 修改顺序固定为：proto -> generated code -> round-trip/compatibility harness -> API Layer -> API Mapping -> core adapter -> transport/consumer。

## 停止条件

- 新增跨边界 Go DTO、interface request/result 或 event，但没有对应 proto schema。
- API Layer 或 API Mapping 依赖 TUI、Cobra、具体 transport、Cloud implementation 或插件实现。
- protocol package 同时拥有业务 DTO 和 wire codec，或 core/client/TUI 各维护一份同字段结构。
- API Mapping 建立连接、处理 framing，或持有缓存、goroutine、session、route winner、history truth、权限或 reducer state。
- 为维持编译增加 alias、兼容 wrapper、双编码、旧 method fallback、nil runtime 或 panic stub。
- Android/Kotlin 或 Web/TypeScript 在迁移后仍拥有认证、Hello、`api.execute` codec、session generation、resource registry、重连状态机或平行业务 DTO。
- Web 把 native Pion 编译失败当作保留第二套 TypeScript 客户端真值的理由，或让未经验证的 JavaScript fingerprint 直接成为 DTLS channel-binding 真值。
- JNI/WASM 暴露 Go pointer、阻塞式跨线程回调、无界事件队列、JSON/base64 业务 payload，或没有显式 cancel/close/release。
- proto schema 无法表达 endpoint/session stamp、错误、取消、资源释放或 capability/version 时继续写实现。

## 当前允许范围

- 主动范围：`AGENTS.md`、`workflow.md`、`docs/development/`、`proto/`、`api_layer/`、`api_mapping/`、`internal/protocol/`、`client/{runtime,port,adapter,binding}/`、`remote/`、`shared/remoteauth/` 与对应 tests/guards。`shared/remoteauth` 只允许为跨端不可导出 signer 接口最小触及，不迁移 daemon store 或其他 shared 目录债务。
- 平台切片范围：只有 `PA005N1/PA005N2` 可以主动修改 `clients/mobile/`、Android Gradle/JNI/build scripts；`PA005N2` 允许最小修改 `clients/ui` 的共享 Proto session contract 与 App 实际消费路径，但不得迁移 browser WebRTC adapter；`PA005W1/PA005W2` 负责 WASM loader/worker、browser WebRTC adapter 与剩余 Web consumer 收口。
- 受限联动：`core/`、`tui/{port,adapter,testkit}/`、`cmd/termx/`、`private/cloud/`、`Makefile`、`scripts/`、`go.work*`，只能为当前跨端迁移切片最小触及。
- 禁止范围：插件 runtime、`private/archive/`、多区域 Cloud、正式开源工程、真实 iOS/Desktop binding。当前 C ABI 必须为未来 iOS/Desktop 保留可复用边界，但不在本阶段生成 XCFramework 或桌面安装包。

## 任务队列

| ID | 状态 | 内容 | 完成条件 |
| --- | --- | --- | --- |
| PA001 | 已完成 | 固化 Proto API 强约定并清理错误基线 | 更新 AGENTS/workflow/架构文档；删除 `core/api` 和未接线的重复 application DTO；恢复 clean compile baseline；文档和守卫不再宣称 Go DTO 是 API truth |
| PA002 | 已完成 | Proto API inventory 与缺口表 | 列出 terminal/attachment/path/history/live/file/storage/workbench/endpoint/access/cloud 的现有 proto、Go-only DTO、consumer、兼容字段和缺失 command/event/error；不写实现 |
| PA003 | 已完成 | 公共 API schema 与 envelope | 在 `proto/` 定义 versioned command/result/event/error/capability/resource handle；区分公共 application API 与内部 wire framing；生成代码和 compatibility harness 通过 |
| PA004 | 已完成 | API Layer 与 API Mapping 骨架 | 建立 `api_layer/` 与 `api_mapping/`；只依赖 core domain interface 与生成 proto；静态守卫禁止 UI/transport/private/state owner；fake harness 覆盖取消、释放和错误 |
| PA005A1 | 已完成 | Terminal/attachment/path Proto schema | 定义 TerminalRef、lifecycle、attachment/input/resize、path typed command/result/event；加入 application envelope、生成代码和 compatibility harness |
| PA005A2 | 已完成 | Terminal API Layer 与 API Mapping | proto validation、core domain mapping、typed error/cancel/release；不接 protocol transport 或 UI |
| PA005A2R | 已完成 | Proto API 基础契约审查修正 | envelope 顶层 context 保留未知 command correlation；result/event 回显 origin session；API Layer 使用 connection-bound atomic admission lease；resource handle 绑定 origin session 且事务式发布；稳定错误、enum/数值边界、response clone、descriptor baseline 和双 reviewer 通过 |
| PA005A3 | 已完成 | Terminal Proto API 原子迁移 | `api.execute` framing 把 Proto envelope 交给 API Layer；core 提供 connection-bound admission、terminal adapter 与 attachment transaction；protocol client、CLI/TUI/remote terminal/path consumer 同步切到 `apipb`；删除 terminal/attachment/path protocol DTO、旧 method codec 和 wire schema，不使用 alias、wrapper 或双路径；依赖守卫与测试通过 |
| PA005A4 | 已完成 | Application 装配方向收口 | connection-bound API 装配移出 `core/`；`api_mapping` 恢复 generated Proto 与 core domain 的唯一字段转换；`core` 不再 import `api_layer/api_mapping`，protocol framing 不直接持有 core adapter；补 import guard 和同等 terminal/path E2E 后删除 `core/application_api.go` |
| PA005G | 已完成 | Go 全领域 Proto API 原子迁移 | history/live/file/storage/workbench value/endpoint runtime/access/remote daemon control/application events 已进入 `apipb + api.execute`；Go-only DTO、generic `Call`/method codec、daemon workbench mutation/store 与 Go 双路径已删除；file stream framing 私有，upload resume 与 active resource token 分域，event subscription 有资源 correlation；生产包、Go generated compare、schema/Mapping/API Layer tests 与旧 DTO 扫描通过 |
| PA005E | 已完成 | 跨端 Go Client Engine 收口 | 从 `remote/client.Dial` 和平台 consumer 中分离 portable orchestration；`client/runtime` 成为 endpoint/session generation、resource/cancel/reconnect 的唯一客户端真值，portable auth/protocol adapter 负责 remote auth、Hello、`api.execute` 与 Proto command/result/event；两者通过 WebRTC/DataChannel、signer/credential、Cloud signaling、clock/random、host lifecycle ports 组成共享引擎，runtime 不直接依赖 remoteauth/Pion；native Pion 进入 concrete adapter；不修改 App/Web 生产代码 |
| PA005B | 已完成 | 稳定跨语言 binding contract | 在 `client/binding` 建立 serialized Proto + opaque handle + async event/cancel/close/release 的窄边界；禁止 Go pointer、业务专用函数、JSON/base64 和无界队列；提供 fake host harness、ABI ownership 文档、Android 与 `js/wasm` 编译门禁，不产出 fake 平台库 |
| PA005N1 | 已完成 | Android native/JNI 纵向 spike | 安装并固定 NDK/Go binding 工具链；从 Go Client Engine 生成真实 Android native artifact，经薄 JNI/Capacitor bridge 完成 signaling、Pion WebRTC DataChannel、remote auth、Hello、一次 `api.execute`、一个 event、cancel/close；APK 进程内真实加载，Kotlin 不实现平行网络状态机 |
| PA005N2 | 已完成 | Android App 完整迁移 | App 所有 terminal/history/live/file/storage/access/remote consumer 切到 Go binding 与 `apipb`；Android Keystore signer、process/activity lifecycle、网络切换和 generation fence 接入；锁屏/后台销毁旧 engine/bridge/session/resource，WebView 恢复前创建严格递增的新 generation，冻结等待显式失败且事件队列有界；删除 Kotlin/TypeScript mobile 旧 codec、session/resource registry、多 DataChannel fallback 和旧原生 WebRTC manager |
| PA005W1 | 已完成 | Web WASM/WebRTC 安全纵向 spike | Go Client Engine 编译为 WASM；薄 browser adapter 提供 `RTCPeerConnection`、`RTCDataChannel`、signer/store、signaling 和 lifecycle；完成 remote auth、Hello、一次 `api.execute`、一个 event、cancel/close；独立 harness 证明 SDP fingerprint、浏览器 peer 与 Go auth transcript 的 channel binding，tab suspend/resume 产生新 generation |
| PA005W2 | 已完成 | Web client 完整迁移 | Web terminal/history/live/file/storage/access/remote consumer 切到 Go/WASM binding 与 generated `apipb`；收缩 `browserRtcSession` 为平台 primitive adapter，删除 TypeScript API codec、session/resource/reconnect truth、多 DataChannel fallback；typecheck/build 与真实浏览器 protocol harness 通过 |
| PA005R | 已完成 | 双端旧契约与路径原子删除 | Android/Web 都完成后删除 `runtimepb`、`wirepb` 中重复 application schema、generated artifacts、旧 method codec、旧 bridge 和 fallback；全仓扫描确认业务 API 只剩 `apipb + api.execute`，不得保留兼容 alias/wrapper/双路径 |
| PA006T | 已完成 | Proto API 测试迁移 | 把 core/protocol/client-runtime/TUI/CLI 旧 DTO tests 改为 generated Proto harness；补 event subscription correlation/release、machine-events-only、file active/resume token namespace 和跨 session upload resume 测试；不得恢复旧 alias/codec |
| PA007 | 已完成 | 跨端架构就绪双审 | import graph、schema coverage、重复 DTO、binding ownership、Android/Web lifecycle、channel binding、fallback、生成代码、文档和 tests 通过；架构 reviewer 与代码 reviewer 明确 PASS 后恢复 C3B |
| C3B | 已完成 | RouteSelectionPlanner | 纯 planner 覆盖平台/credential eligibility、manual override、local/SSH full race、priority hedge、唯一 managed 单路、不可变 attempt groups 和稳定过滤诊断；机器可读 fixture 与 race 通过 |
| C3C | 已完成 | fresh daemon proof / ReadySession | local/SSH fresh challenge proof、managed channel-bound auth、ReadySession evidence/pin/Hello/lifecycle gate 与失败清理通过 |
| C3D | 已完成 | shared runtime session owner | planner-driven attempt groups、per-endpoint singleflight、唯一 winner 线性化、loser cancel/wait/cleanup、shared lease、sticky override 与 bounded lifecycle mailbox harness 通过 |
| C3E | 已完成 | CLI 接入共享 runtime | CLI/TUI composition 共用 ClientRuntime/SessionOwner、local/SSH/lazy-managed registry、system Clock 与 lifecycle source；旧单 route owner/raw adoption 已删除 |
| C3F | 已完成 | operation generation stamp | attach candidate/commit/cleanup、input/paste/resize/detach 共用 Proto session stamp 与 operation identity；stale 副作用前失败且 input 不重放 |
| C3G | 已完成 | local + SSH race E2E | 隔离真实 sshd/OpenSSH client 覆盖 full race、priority hedge、override/sticky、loser process cleanup、TerminalRef 稳定和 stale operation |
| C3H | 已完成 | 最终准入与双审 | 全量测试与架构/重复真值/fallback/cleanup 审计通过；架构 reviewer 与代码 reviewer 复审后均明确 PASS |

## 测试准入

- `PA001`：`git diff --check`；`GOWORK=off go test ./client/... ./tui/... -run '^$'`；可编译的 core/protocol contract packages；确认不存在 `core/api` import。
- `PA002`：`git diff --check`；inventory 中每项必须有 schema owner、consumer、迁移目标和删除条件。
- `PA003`：生成代码检查；proto round-trip、unknown-field/compatibility、enum/oneof/version harness；`git diff --check`。
- `PA004`：API Layer/API Mapping unit tests 与 dependency guards；取消、资源释放和错误映射 harness。
- `PA005A2R`：generated-code check；descriptor baseline；Proto/API Layer/API Mapping race tests；client-owned origin session、atomic admission lease、command authorization、resource ownership、unknown command correlation、typed error 和边界 validation harness；`git diff --check`。
- `PA005G`：Go 生产包编译；Go generated 临时重生成比较；`proto/apipb`、`api_mapping`、`api_layer` tests；Go 重复类型、旧 helper、旧 method dispatch 和旧 application DTO import 扫描。旧 DTO tests 按用户指令延期到 PA006T。
- `PA005E`：portable engine unit/race harness；`GOOS=android GOARCH=arm64` 与 `GOOS=js GOARCH=wasm` 编译 portable packages；dependency guard 确认 runtime 不依赖 Pion、DOM/JNI、UI、CLI、private 或 file-backed credential；native Pion adapter tests。
- `PA005B`：binding ownership/cancel/release/event backpressure harness；C ABI header/符号与 WASM export baseline；Android/`js/wasm` compile；Proto round-trip 与 unknown-field 保留；Go pointer、JSON/base64、业务专用导出扫描。
- `PA005N1`：真实 Android native artifact build；Gradle unit/instrumentation 或等效 APK load harness；真实 DataChannel/auth/Hello/`api.execute`/event/cancel/close 纵向证明；JNI thread、buffer ownership、重复 close 和 process teardown 测试。
- `PA005N2`：Android/App generated code、Kotlin/TypeScript compile、Community/Official APK build 与 native protocol harness；Keystore/lifecycle/generation tests；instrumentation 必须证明锁屏等价 teardown 后旧 engine handle 失效且新 session generation 严格递增；resource 等冻结等待收到显式关闭而非永久悬挂；App 旧 schema/codec/session/resource/WebRTC manager/fallback 扫描；release native library 不包含 PA005N1 spike daemon/fake Cloud 装配。
- `PA005W1`：WASM build；browser WebRTC/auth/channel-binding E2E；Promise/cancel/close、event backpressure、binary copy、tab suspend/resume generation harness；不得以 mock peer 代替完成条件。
- `PA005W2`：Web generated code、TypeScript typecheck/build 与真实 browser protocol harness；旧 TS codec、session/resource/reconnect truth 和多 DataChannel fallback 扫描。
- `PA005R`：generated-code check；删除重复 runtime/wire application schema后的全仓 compile/typecheck/build；旧 schema、codec、method、bridge、fallback 和手写跨语言 DTO 扫描。
- `PA006T`：迁移后的 Go tests、race/E2E 与 CLI compile；失败不得通过恢复旧 DTO、method codec 或 fallback 解决。
- `PA007`：全量可运行测试、generated-code check、import graph、重复 schema/DTO 扫描和双 Agent 审查。
- `C3B`：`go test ./client/endpoint -count=1`；`go test -race ./client/endpoint -count=1`；机器可读 route plan fixture；`make test`；`make doctor`；`git diff --check`。
- `C3C`：generated-code check；fresh challenge/proof、pin mismatch、缺失 authorization/Hello/lifecycle、managed auth 与 local/SSH adapter harness；`go test -race ./shared/remoteauth ./api_mapping ./api_layer ./client/runtime ./client/adapter/protocol ./client/adapter/managed ./client/adapter/ssh -count=1`；`make test`；`make doctor`；`git diff --check`。
- `C3D`：`go test -race ./client/runtime ./client/endpoint -count=1`；winner/loser exact cleanup、hedge delay、cancel、同 endpoint shared lease、sticky override 与 lifecycle mailbox harness；`make test`；`make doctor`；`git diff --check`。
- `C3E`：CLI/TUI local/SSH/managed composition 与 dependency guards；默认/显式 route、shared owner reuse、错误/取消投影 harness；旧 `SelectRoute`、`local.Connect`、raw protocol adoption 扫描；`go test -race ./client/runtime ./client/adapter/... ./tui/adapter/clientruntime ./cmd/termx -count=1`；`make test`；`make doctor`；`git diff --check`。
- `C3F`：attach candidate/confirm/commit/cleanup、detach/input/paste/resize stamp harness；旧 generation 和 replaced operation 在 adapter 调用前失败，`Attempted=false`，已调用非幂等 input 不自动重放；`go test -race ./tui/app ./tui/adapter/clientruntime ./tui/adapter/protocol ./client/runtime ./cmd/termx -count=1`；`make test`；`make doctor`；`git diff --check`。
- `C3G`：测试内隔离真实 `sshd`、OpenSSH client 与远端 `termx daemon stdio-proxy`；full race、priority hedge、explicit override/sticky、loser SSH PID cleanup、TerminalRef 与 stale operation harness；`go test -race ./client/runtime ./client/adapter/ssh ./shared/transport/ssh -count=1`；`go test -race ./cmd/termx -run 'TestC3GRealLocalAndOpenSSHRoutes' -count=1`；`make test`；`make doctor`；`git diff --check`。
- `C3H`：`go test -race ./client/endpoint ./client/runtime ./client/adapter/... ./shared/transport/ssh ./tui/app ./tui/adapter/clientruntime ./tui/adapter/protocol ./cmd/termx -count=1`；`make test`；`make doctor`；`npm test`；`npm run typecheck`；`npm run build`；旧 route owner/raw protocol adoption/平台网络第二真值、重复 application DTO、fallback 与资源 cleanup 守卫扫描；架构 reviewer 与代码 reviewer 对修复后 diff 均明确 PASS；`git diff --check`。

## 执行规则

1. 每轮先读取本文件和 `AGENTS.md`，再检查 worktree。
2. 只执行最早的进行中/待开始切片，不跨切片扩展；Android 与 Web 共用 engine，但仍按任务表先证明共同基础，再分别完成平台纵向闭环。
3. 先改 proto，再生成并补 compatibility harness，再写 API Layer/API Mapping，最后迁移 core、protocol transport 与 consumer；不得逆序。
4. 迁移完成必须删除旧 Go/Kotlin/TypeScript application API、codec、session/resource truth，不保留 alias、wrapper、双路径或 fallback。
5. 每个切片必须说明 schema owner、domain owner、truth source、转换点、消息链路、取消、释放和失败条件。
6. 每个有效切片运行准入并使用中文提交信息提交。
