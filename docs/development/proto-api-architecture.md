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
transport or platform binding
  Unix Socket / TCP+TLS / SSH / WebRTC DataChannel / JNI / Swift / WASM
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

- Unix Socket、TCP/TLS、SSH 和 WebRTC DataChannel 负责建立连接与传递 bytes；JNI、Swift 和 WASM binding 负责平台调用边界。
- `internal/protocol/` 负责 Hello、channel、correlation 和 payload framing，不拥有 terminal/history 等 application API 字段语义。
- transport 和 protocol framing 不执行 application authorization，不把 core domain struct 暴露给客户端，也不在失败时切换未授权 fallback。

### Cross-platform Go Client Engine

- `client/runtime.SessionOwner` 是 endpoint session generation、当前 ready winner、stale operation 拒绝和 reconnect replacement 的唯一客户端真值；平台 UI 不缓存 protocol client。
- `client/adapter/managed` 是 native/Web 共用的 managed single-route attempt 编排，固定执行 Cloud resolution/route material -> platform peer -> signaling -> DTLS-bound remote auth -> protocol Hello -> Proto application session。
- `client/port.ManagedPeer` 只描述 RTCPeerConnection/DataChannel 平台原语。native/Android 由 `client/adapter/managed/pion` 实现；Web 后续由浏览器 adapter 实现，portable managed adapter 不 import Pion、DOM、JNI 或 `syscall/js`。
- remote-auth 通过异步 `ClientAccessSigner` 使用平台 secure key。native 文件 store 可以适配内存 Ed25519 signer；Android Keystore/WebCrypto 只提供 public projection 与不可导出 signer，Go 在发送 proof 前必须用绑定 public key 重新验签。
- `SessionOwner.ConnectRoute` 只接收已经选定的 route，不提前实现 route planner/race。planner 只能在后续切片生成单个 immutable `AttemptRequest`，不能进入 platform binding 或 managed adapter。
- managed attempt 成功结果必须已经完成 remote auth、Hello，并只通过 generated `apipb` 执行业务 command/event。attachment/file 的内部 framing channel 不属于公共 API，跨语言 binding 必须在 opaque resource 边界重新封装。

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

已有 `runtimepb` 是迁移期 Web/mobile API，已有 `wirepb` 同时包含 application message 与 framing。当前 Go 端不得使用其中的 application message；App/Web consumer 后续切到 `apipb` 后删除重复 schema。暂留只表达迁移顺序，不是长期兼容承诺。

## API 设计要求

- 使用 versioned command/event envelope；未知 command、enum 和 field 必须有明确兼容行为。
- `RequestContext` 只能位于 command envelope 顶层，不能复制到每个 oneof command；这样旧服务收到未来 command 时仍能保留 request ID、版本和 session correlation。
- request/result/event 必须携带消息起源 endpoint session 的 stamp；结果只能回显请求 origin generation，不能用新 generation 包装旧请求结果。client runtime 按 generation 拒绝旧 session 迟到消息。
- endpoint generation 是 client runtime truth，daemon 不查询或复制“当前 generation”。capability 与具体 application scope 由 protocol connection 在 Hello/authorization 后持有，并通过覆盖 controller 执行期的原子 admission lease 交给 API Layer；每条请求不得自声明 capability。
- API Layer 在深拷贝 in-process/JNI/WASM Proto 前必须执行 envelope 总量门禁；通过后 admission、validation 与 dispatch 只能使用同一私有快照。
- 非幂等 input、paste、resize、detach 必须携带 operation/session fence，失败后不得隐式重放。
- 长生命周期活动资源使用 session-bound opaque token 与稳定 `ResourceKind` enum，并有显式 release/cancel；owning registry 必须验证 token 真伪、kind、generation 和 session ownership，不得暴露 Go pointer、channel 或 goroutine。
- upload resume 不是活动 stream resource：使用独立 `FileUploadResumeHandle`，由 verified principal、path、size 和 TTL 约束，可跨 session 使用，但不能进入通用 release/cancel。
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
- managed client signaling/auth/Hello/session 编排已从已删除的 `remote/client` 收口到 `client/adapter/managed`；native Pion 只位于 concrete adapter，portable engine 已有 Android arm64 与 `js/wasm` 编译门禁。
- App/Web consumer 尚未迁移，`runtimepb/wirepb` 重复 schema 暂留；当前阶段不得修改其生产源码。
- Go 旧测试仍需改为 generated Proto harness；实现收口不以旧 DTO 测试继续编译为条件。
- CLI 的共享 endpoint runtime helper 仍有已冻结编译缺口；不得用自造 generation、裸 protocol client 或 local fallback 填补。
