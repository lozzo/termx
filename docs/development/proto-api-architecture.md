# Proto API 架构

## 强约定

termx 的所有插件、第三方客户端、官方客户端、跨进程服务和跨语言 binding API，必须先定义在 `proto/`。Proto schema 是字段、枚举、command/event、错误 detail、capability、版本和兼容语义的唯一真值。

禁止先定义 Go request/result/event struct，再把它映射成 proto。禁止把 core domain struct、TUI state 或 protocol package DTO 当作外部 API。

## 完整运行链路

```text
plugins and clients
  CLI / TUI / mobile / desktop / web / third-party
    |
    v
Go Client Engine
  Endpoint/Route / pairing / PeerSession / generation / resource
    |
    v
transport or platform binding
  Unix Socket / WebRTC DataChannel / JNI / Swift / WASM
    |
    v
protocol framing
  Hello / channel / correlation / payload framing
    |
    v
generated proto command / result / event
    |
    v
api_layer
  dispatch / authorization / session fence / cancellation / resource lifecycle
    |
    v
api_mapping
  generated proto <-> core domain
    |
    v
core
  domain truth / state machine / terminal lifecycle / history
```

返回消息沿相反方向传递。Proto 是 schema、生成类型与序列化消息的真值，不是连接管理器或主动运行层。

### Transport And Protocol Framing

- Local Unix 与 Direct/SSH/Cloud Route connector 负责建立连接；所有远程 Route 最终传递可靠有序 WebRTC DataChannel bytes。JNI、Swift 和 WASM binding 只负责平台调用边界。
- `internal/protocol/` 负责 Hello、channel、correlation 和 payload framing，不拥有 terminal/history 等 application API 字段语义。
- Direct embedded signaling 的 request/answer/error 与 protocol `SessionClose` 也必须先定义在 Proto；length-prefix、TCP listener 和 Pion TCPMux 只实现 framing/lifecycle，不能复制字段或把签名 answer 当作 DataChannel authorization。
- transport 和 protocol framing 不执行 application authorization，不把 core domain struct 暴露给客户端，也不在失败时切换未授权 fallback。

### Cross-platform Go Client Engine

- `client/runtime.SessionOwner` 是 endpoint session generation、当前 ready winner、stale operation 拒绝和 reconnect replacement 的唯一客户端真值；平台 UI 不缓存 protocol client。
- Direct、SSH 和 Cloud connector 必须返回同一种 Go-owned ReadyPeerSession；Route 差异只存在于 signaling、ICE 和 SSH tunnel 建立阶段。
- `client/adapter/direct` 只消费当前 `direct-webrtc-tcp` attempt；它验证 daemon-signed answer 后仍复用 `client/adapter/peer` 的 DTLS-bound capability transaction，并通过 versioned `SessionClose` 让 daemon 先释放 protocol/peer/TCPMux 资源。
- native/Android 使用 Go/Pion；Android 通过 C ABI + JNI。浏览器 Web 当前冻结，未来恢复时由 Go/WASM 使用浏览器 WebRTC/WebCrypto primitive，不允许 TypeScript 建立第二套 session truth。
- remote-auth 通过异步 `ClientAccessSigner` 使用平台 secure key。native 文件 store 可以适配内存 Ed25519 signer；Android Keystore/WebCrypto 只提供 public projection 与不可导出 signer，Go 在发送 proof 前必须用绑定 public key 重新验签。
- `client/endpoint.RouteSelectionPlanner` 只根据不可变 endpoint、intent、override、generation 和平台能力生成 attempt groups；`client/runtime.SessionOwner` 执行 race、线性化唯一 ReadyPeerSession winner 并精确清理 loser。planner 不进入 platform binding 或 managed adapter，adapter 也不得选择其它 route。
- `client/runtime.PeerConnector` 是 Local/Direct/SSH/Cloud 的统一 attempt 边界；远程 connector 必须返回同一种 `ReadyPeerSession`。`PairingService` 统一拥有 Endpoint pin、实际 DTLS binding、PairingTicket handshake 和 pairing peer exact-close，Cloud adapter 只拥有 Cloud signaling/ICE。
- 任一远程 attempt 成功结果必须已经完成 remote auth、Hello，并只通过 generated `apipb` 执行业务 command/event。attachment/file 的内部 framing channel 不属于公共 API，跨语言 binding 必须在 opaque resource 边界重新封装。
- `client/binding` 只接收 serialized `bindingpb.OpenSessionRequest`、`bindingpb.EngineCommand`、`apipb.CommandEnvelope` 和 `uint64` opaque handle；异步输出统一为 `bindingpb.EventEnvelope`。C/JNI/WASM 共享同一 engine/operation/session registry 与有界事件队列，平台 wrapper 不能建立第二份 handle truth，也不能为 pairing、credential 或其它业务 command 增加专用导出符号。
- `OpenSessionRequest` 只携带 EndpointID、可选 Route override 和 intent；当前 `EndpointConfigV1` 必须由 Go Client Engine registry 解析。registry get/upsert/delete、pairing import 与 credential lifecycle 都通过同一个 `EngineCommand` 入口，Android/Web 平台只持久化 opaque `EndpointRegistryV1` bytes。

### Core

- 拥有 terminal、attachment、history、live、file/storage 等领域真值。
- 可以定义内部 domain struct 和 value object。
- 不向插件或客户端暴露内部 Go 类型。
- 不依赖 UI、Cobra、具体 transport、插件或平台 binding。

### API Layer

- 对外参数、结果、事件和稳定错误 detail 只使用 proto 生成类型。
- 负责 application method dispatch、connection-bound admission、capability/authorization、operation/resource origin fence、取消、资源分配与释放、stream 生命周期和错误分类。
- 不负责 protobuf 字段转换、socket framing、WebRTC、SSH、UI projection 或 route fallback。
- 通过 API Mapping 把 generated proto 转为 core domain value，再调用 core 的窄 domain interface；不得把 core struct 泄露到公开方法。

### API Mapping

- `api_mapping/` 是 core domain 与 generated proto 之间唯一允许的字段映射点。
- 映射必须无状态、确定性、可单测并显式失败。
- 它不是 transport，不建立 Unix Socket/TCP/SSH/WebRTC 连接，也不处理 Hello、channel、correlation 或 framing。
- 不持有 goroutine、session、route winner、history cache、attachment registry 或 reducer state。
- 平台确有专用 view model 时，只能从 proto 投影；平台模型不能反向成为 API truth。

### Plugins And Clients

- 只依赖发布的 proto schema、生成 SDK、capability/version contract 和公开 binding。
- 不依赖内部 Go package、core struct、TUI state、private Cloud type 或 protocol implementation。
- CLI/TUI 作为 first-party client 也必须遵守同一边界，不能拥有特权旁路。

## Proto 分层

`proto/` 后续必须明确区分：

- application API：terminal、attachment、history、live、path、file、storage、endpoint、access 等 command/result/event。
- transport framing：Hello、request correlation、channel frame、payload envelope。
- private Cloud API：Control Plane、Companion、Hub/Relay 管理面；不得混入 terminal payload 或 CapabilityGrant 判断。

PA003 已建立 `proto/apipb/`，package 为 `termx.api.v1`，它是新的公共 application API 唯一落点。后续 terminal/history/file 等领域 command/result/event 必须进入该 package 或其同版本子 schema。

`runtimepb` 迁移期 Web/mobile application schema 已删除。`wirepb` 只保留 Hello、request/response correlation、错误 envelope 与 file resource stream payload；它不再包含 terminal/history/storage 等 application message。

## API 设计要求

- 使用 versioned command/event envelope；未知 command、enum 和 field 必须有明确兼容行为。
- `RequestContext` 只能位于 command envelope 顶层，不能复制到每个 oneof command；这样旧服务收到未来 command 时仍能保留 request ID、版本和 session correlation。
- request/result/event 必须携带消息起源 endpoint session 的 stamp；结果只能回显请求 origin generation，不能用新 generation 包装旧请求结果。client runtime 按 generation 拒绝旧 session 迟到消息。
- endpoint generation 是 client runtime truth，daemon 不查询或复制“当前 generation”。capability 与具体 application scope 由 protocol connection 在 Hello/authorization 后持有，并通过覆盖 controller 执行期的原子 admission lease 交给 API Layer；每条请求不得自声明 capability。
- API Layer 在深拷贝 in-process/JNI/WASM Proto 前必须执行 envelope 总量门禁；通过后 admission、validation 与 dispatch 只能使用同一私有快照。
- 非幂等 input、paste、resize、detach 必须携带 operation/session fence，失败后不得隐式重放。
- 长生命周期活动资源使用 session-bound opaque token 与稳定 `ResourceKind` enum，并有显式 release/cancel；owning registry 必须验证 token 真伪、kind、generation 和 session ownership，不得暴露 Go pointer、channel 或 goroutine。
- upload resume 不是活动 stream resource：使用独立 `FileUploadResumeHandle`，由 verified principal、path、size 和 TTL 约束，可跨 session 用于续传或专用 `FileTransferCancel` 销毁未完成上传，但不能进入 stream 或通用 resource release。
- 创建长期资源的 controller 必须返回 pending transaction；API Layer 校验公开 projection 后才 commit，校验或提交失败必须通过不继承请求取消且有超时的 cleanup context rollback。
- 稳定错误使用 code + typed detail，不依赖字符串解析。
- `EndpointID + TerminalID` 是跨 endpoint terminal identity；裸 `TerminalID` 只在 owning daemon 内有效。
- history token/generation、live revision、runtime session generation 是不同版本空间，proto 中不得复用同一字段表达。
- file API 使用高层 read/write/stream contract，不能把 wire channel、ACK window 或 frame type 暴露给插件/客户端。

## 修改顺序

```text
proto schema
  -> generated code
  -> round-trip and compatibility harness
  -> API Layer
  -> API Mapping
  -> core adapter
  -> protocol transport and client/plugin consumer
  -> delete old Go-only API
```

任何一步需要 alias、双路径、fallback 或复制 DTO 才能继续时，必须停止并重新判断 schema 与 owner。

## 依赖守卫

- `api_layer/` 禁止依赖 `tui/`、`cmd/`、具体 transport、private Cloud implementation 或插件实现。
- `api_mapping/` 禁止依赖 runtime manager、storage implementation、transport、protocol framing、UI 和 private Cloud implementation。
- `internal/protocol/` 禁止定义与 application proto 同字段的业务 DTO。
- `client/runtime`、`tui/port` 和平台 binding 禁止复制 application proto 业务字段。
- 生成代码必须通过仓库 generated-code check，禁止手工编辑。
- `proto/apipb/testdata/public-api-v1.pb` 是完整 descriptor baseline；字段、enum、oneof、reserved 和 message 变更必须显式更新 baseline，并接受 schema compatibility review。

## 当前迁移债务

- Go application domain 已统一进入 `apipb + api.execute`；旧 Go protocol DTO、generic method codec、daemon workbench mutation/store 已删除。
- `core` 只暴露 native `ApplicationSessionPort`，不 import `api_layer/api_mapping`；generated Proto 与 core 的字段转换只位于 `api_mapping/`。
- Go application API 与基础 Client Engine 已形成；当前活动迁移按 `workflow.md` 把 Direct、SSH 和 Cloud 收口为统一 WebRTC DataChannel PeerSession。
- Android 是当前平台纵向主线；Web/WASM 只维持 contract/编译不回归，不建设默认 Web 访问入口或迁移 browser consumer。
- `runtimepb`、`wirepb` 重复 application schema、旧 method codec、本地 session token store 与旧 Hub/RTC bridge 已删除；`wirepb` 的剩余消息仅属于 framing-private contract。
- Go 旧测试已经迁为 generated Proto harness；旧 DTO 测试例外已结束。
- CLI/TUI 必须通过同一 `ClientRuntime/SessionOwner` 使用 local、Direct、SSH 与 managed connector；任何 consumer 不得自行选择 route、缓存 protocol client 或增加 fallback。
- Android engine 重建通过 Go `SessionGenerationAuthority` 保持进程内 endpoint generation 单调递增；未来 WASM 沿用同一 contract，平台只持有 authority 引用，不生成或缓存 generation 数值。
- 同一 engine 内，`client/runtime.SessionOwner.AcquireRoute` 让同 endpoint 且连接配置相同的 `OpenSession` 共享一条 Go-owned ready session；binding handle 只持有 consumer lease，`enginehost` 不保存第二份 current-session map。关闭 list/inventory/file/workspace 任一 lease 不关闭底层 session，只有配置变化、显式 generation replacement、lifecycle teardown 或 engine close 才替换底层连接。
- `enginehost` 是跨端 Endpoint registry 事务 owner：加载后先做 Proto/领域校验，upsert 禁止替换既有 identity pin，pairing 在 credential bind 后提交 registry 且失败时补偿 credential，delete 只清理新 registry 中不再引用的 credential ref。TypeScript/Kotlin 只能缓存只读 projection，不得持久化 `configProto` 或建立 Route 索引。
- application event 必须按完整 subscription `ResourceHandle`（token、kind、session、generation）关联；session 级 event pump 只是 transport fan-out，consumer 不得把它当作 subscription filter。
