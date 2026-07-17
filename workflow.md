# 工作流：Proto API 与跨端 Go Client Engine 迁移

## 当前结论

- Go 端 Proto API 原子迁移 `PA005G` 已完成。下一切片是 `PA005E` 跨端 Go Client Engine 边界收口；Android 与 Web 都必须复用该引擎，不再按两套客户端网络实现分别打补丁。
- 当前迁移允许修改 `client/{runtime,port,adapter,binding}/`、`remote/`、`internal/protocol/`、`proto/`、`clients/mobile/`、`clients/ui/` 以及对应 tests、build scripts 和必要架构文档；每个切片仍只能触及任务表规定的最小范围。
- Android 目标是 Go Client Engine 编译为 native library，通过稳定 C ABI 与薄 JNI/Capacitor bridge 调用；Kotlin/Java 不拥有连接、认证、协议、session/resource 或重连真值。
- Web 目标是同一 Go Client Engine 编译为 WebAssembly。浏览器 WebRTC、WebCrypto/IndexedDB 和页面 lifecycle 由薄 JavaScript/TypeScript platform adapter 提供；TypeScript 不保留第二套认证、协议、Proto codec、session/resource 或多 DataChannel fallback 真值。
- `runtimepb` 和 `wirepb` 中仍被 App/Web 消费的迁移期 application schema，只能保留到对应 consumer 全部切到 `apipb + api.execute`；`PA005R` 必须原子删除这些旧 schema、codec、method string 和双路径。
- 当前探针已证明 `proto/apipb`、`internal/protocol`、`client/runtime`、remote auth 和 DataChannel transport 可以编译到 `js/wasm`，Go remote client/Pion 路径可以编译到 `android/arm64`。当前 native Pion WebRTC 不能直接编译到浏览器 WASM，因此 Web 必须通过浏览器 WebRTC platform port 接入。
- 本机当前未安装 Android NDK 和 `gomobile`；这不阻塞 `PA005E/PA005B`，但真实 Android native artifact 是 `PA005A1` 的完成条件。缺少工具链时必须把该切片标记阻塞，不得提交 fake `.so`、stub JNI 或跳到后续 Web consumer 迁移。
- 用户已明确允许实现阶段不保证旧测试通过。PA005G 的新 schema/API Mapping/API Layer tests 与 Go 生产包通过；旧 core/protocol/client-runtime/TUI tests 因引用已删除 DTO 暂缓到 `PA006T`，不得据此恢复旧类型。
- CLI 根包仍有 PA005G 之前已冻结的 endpoint runtime helper 缺口：`v3DialClient`、`probeEndpointProtocolClient`、`openEndpointProtocolClient`、`dialOrStartV3ClientContext` 及 attach runtime helpers。不得用 legacy/fallback 修补。
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

- 主动范围：`AGENTS.md`、`workflow.md`、`docs/development/`、`proto/`、`api_layer/`、`api_mapping/`、`internal/protocol/`、`client/{runtime,port,adapter,binding}/`、`remote/` 与对应 tests/guards。
- 平台切片范围：只有 `PA005A1/PA005A2` 可以主动修改 `clients/mobile/`、Android Gradle/JNI/build scripts；只有 `PA005W1/PA005W2` 可以主动修改 `clients/ui/`、WASM loader/worker 和 browser WebRTC adapter。
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
| PA005E | 待开始 | 跨端 Go Client Engine 收口 | 从 `remote/client.Dial` 和平台 consumer 中分离 portable orchestration；`client/runtime` 成为 endpoint/session generation、resource/cancel/reconnect 的唯一客户端真值，portable auth/protocol adapter 负责 remote auth、Hello、`api.execute` 与 Proto command/result/event；两者通过 WebRTC/DataChannel、signer/credential、Cloud signaling、clock/random、host lifecycle ports 组成共享引擎，runtime 不直接依赖 remoteauth/Pion；native Pion 进入 concrete adapter；不修改 App/Web 生产代码 |
| PA005B | 待开始 | 稳定跨语言 binding contract | 在 `client/binding` 建立 serialized Proto + opaque handle + async event/cancel/close/release 的窄边界；禁止 Go pointer、业务专用函数、JSON/base64 和无界队列；提供 fake host harness、ABI ownership 文档、Android 与 `js/wasm` 编译门禁，不产出 fake 平台库 |
| PA005A1 | 待开始 | Android native/JNI 纵向 spike | 安装并固定 NDK/Go binding 工具链；从 Go Client Engine 生成真实 Android native artifact，经薄 JNI/Capacitor bridge 完成 signaling、Pion WebRTC DataChannel、remote auth、Hello、一次 `api.execute`、一个 event、cancel/close；APK 进程内真实加载，Kotlin 不实现平行网络状态机 |
| PA005A2 | 待开始 | Android App 完整迁移 | App 所有 terminal/history/live/file/storage/access/remote consumer 切到 Go binding 与 `apipb`；Android Keystore signer、process/activity lifecycle、网络切换和 generation fence 接入；删除 Kotlin/TypeScript mobile 旧 codec、session/resource registry、多 DataChannel fallback 和旧原生 WebRTC manager |
| PA005W1 | 待开始 | Web WASM/WebRTC 安全纵向 spike | Go Client Engine 编译为 WASM；薄 browser adapter 提供 `RTCPeerConnection`、`RTCDataChannel`、signer/store、signaling 和 lifecycle；完成 remote auth、Hello、一次 `api.execute`、一个 event、cancel/close；独立 harness 证明 SDP fingerprint、浏览器 peer 与 Go auth transcript 的 channel binding，tab suspend/resume 产生新 generation |
| PA005W2 | 待开始 | Web client 完整迁移 | Web terminal/history/live/file/storage/access/remote consumer 切到 Go/WASM binding 与 generated `apipb`；收缩 `browserRtcSession` 为平台 primitive adapter，删除 TypeScript API codec、session/resource/reconnect truth、多 DataChannel fallback；typecheck/build 与真实浏览器 protocol harness 通过 |
| PA005R | 待开始 | 双端旧契约与路径原子删除 | Android/Web 都完成后删除 `runtimepb`、`wirepb` 中重复 application schema、generated artifacts、旧 method codec、旧 bridge 和 fallback；全仓扫描确认业务 API 只剩 `apipb + api.execute`，不得保留兼容 alias/wrapper/双路径 |
| PA006T | 待开始 | Proto API 测试迁移 | 把 core/protocol/client-runtime/TUI/CLI 旧 DTO tests 改为 generated Proto harness；补 event subscription correlation/release、machine-events-only、file active/resume token namespace 和跨 session upload resume 测试；不得恢复旧 alias/codec |
| PA007 | 待开始 | 跨端架构就绪双审 | import graph、schema coverage、重复 DTO、binding ownership、Android/Web lifecycle、channel binding、fallback、生成代码、文档和 tests 通过；架构 reviewer 与代码 reviewer 明确 PASS 后恢复 C3B |
| C3B | 暂停 | RouteSelectionPlanner | PA007 PASS 后恢复 |
| C3C | 暂停 | fresh daemon proof / ReadySession | PA007 PASS 后恢复 |
| C3D | 暂停 | shared runtime session owner | PA007 PASS 后恢复 |
| C3E | 暂停 | CLI 接入共享 runtime | PA007 PASS 后恢复 |
| C3F | 暂停 | operation generation stamp | PA007 PASS 后恢复 |
| C3G | 暂停 | local + SSH race E2E | PA007 PASS 后恢复 |
| C3H | 暂停 | 最终准入与双审 | PA007 PASS 后恢复 |

## 测试准入

- `PA001`：`git diff --check`；`GOWORK=off go test ./client/... ./tui/... -run '^$'`；可编译的 core/protocol contract packages；确认不存在 `core/api` import。
- `PA002`：`git diff --check`；inventory 中每项必须有 schema owner、consumer、迁移目标和删除条件。
- `PA003`：生成代码检查；proto round-trip、unknown-field/compatibility、enum/oneof/version harness；`git diff --check`。
- `PA004`：API Layer/API Mapping unit tests 与 dependency guards；取消、资源释放和错误映射 harness。
- `PA005A2R`：generated-code check；descriptor baseline；Proto/API Layer/API Mapping race tests；client-owned origin session、atomic admission lease、command authorization、resource ownership、unknown command correlation、typed error 和边界 validation harness；`git diff --check`。
- `PA005G`：Go 生产包编译；Go generated 临时重生成比较；`proto/apipb`、`api_mapping`、`api_layer` tests；Go 重复类型、旧 helper、旧 method dispatch 和旧 application DTO import 扫描。旧 DTO tests 按用户指令延期到 PA006T。
- `PA005E`：portable engine unit/race harness；`GOOS=android GOARCH=arm64` 与 `GOOS=js GOARCH=wasm` 编译 portable packages；dependency guard 确认 runtime 不依赖 Pion、DOM/JNI、UI、CLI、private 或 file-backed credential；native Pion adapter tests。
- `PA005B`：binding ownership/cancel/release/event backpressure harness；C ABI header/符号与 WASM export baseline；Android/`js/wasm` compile；Proto round-trip 与 unknown-field 保留；Go pointer、JSON/base64、业务专用导出扫描。
- `PA005A1`：真实 Android native artifact build；Gradle unit/instrumentation 或等效 APK load harness；真实 DataChannel/auth/Hello/`api.execute`/event/cancel/close 纵向证明；JNI thread、buffer ownership、重复 close 和 process teardown 测试。
- `PA005A2`：Android/App generated code、Kotlin/TypeScript compile、Community/Official APK build 与 native protocol harness；Keystore/lifecycle/generation tests；App 旧 schema/codec/session/resource/WebRTC manager/fallback 扫描。
- `PA005W1`：WASM build；browser WebRTC/auth/channel-binding E2E；Promise/cancel/close、event backpressure、binary copy、tab suspend/resume generation harness；不得以 mock peer 代替完成条件。
- `PA005W2`：Web generated code、TypeScript typecheck/build 与真实 browser protocol harness；旧 TS codec、session/resource/reconnect truth 和多 DataChannel fallback 扫描。
- `PA005R`：generated-code check；删除重复 runtime/wire application schema后的全仓 compile/typecheck/build；旧 schema、codec、method、bridge、fallback 和手写跨语言 DTO 扫描。
- `PA006T`：迁移后的 Go tests、race/E2E 与 CLI compile；失败不得通过恢复旧 DTO、method codec 或 fallback 解决。
- `PA007`：全量可运行测试、generated-code check、import graph、重复 schema/DTO 扫描和双 Agent 审查。

## 执行规则

1. 每轮先读取本文件和 `AGENTS.md`，再检查 worktree。
2. 只执行最早的进行中/待开始切片，不跨切片扩展；Android 与 Web 共用 engine，但仍按任务表先证明共同基础，再分别完成平台纵向闭环。
3. 先改 proto，再生成并补 compatibility harness，再写 API Layer/API Mapping，最后迁移 core、protocol transport 与 consumer；不得逆序。
4. 迁移完成必须删除旧 Go/Kotlin/TypeScript application API、codec、session/resource truth，不保留 alias、wrapper、双路径或 fallback。
5. 每个切片必须说明 schema owner、domain owner、truth source、转换点、消息链路、取消、释放和失败条件。
6. 每个有效切片运行准入并使用中文提交信息提交。
