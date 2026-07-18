# Proto API Inventory

## 当前结论

- `proto/apipb/` 是公共 application API 的唯一 schema truth。
- Go 运行链路固定为 `client -> protocol framing -> apipb -> api_layer -> api_mapping -> core`。
- `internal/protocol/` 的 Go 生产代码只保留 Hello、request/response correlation、`api.execute` payload transport、stream channel 和 file frame/ACK/finish。
- `core/` 保留领域模型；这些类型不是跨进程 API，也不能被客户端直接依赖。
- Go、Android 与 Web application consumer 均已迁移到 `apipb + api.execute`；Android JNI 与 Web WASM 共用 Go Client Engine 和同一 binding contract。

## Schema Ownership

| Schema | Owner | 当前状态 |
| --- | --- | --- |
| `proto/apipb/` | terminal、attachment、path、history、live、file、storage、workbench value、endpoint runtime、client access、remote daemon control、application events | Go 端唯一公共 application schema |
| `proto/wirepb/` | Hello、correlation、error envelope 与 file resource stream payload | 仅 framing-private；不得承载 application API |
| `proto/runtimepb/` | 已删除 | application schema 已统一到 `apipb` |
| `proto/remoteauthpb/` | DeviceIdentity、CapabilityGrant、pairing/bootstrap | 保持独立安全 contract |
| `proto/cloudpb/` | Companion 与 Cloud control plane | 保持独立，不承载 terminal payload |

## Go 领域状态

| 领域 | 公共 Proto | Core owner | Go consumer 状态 |
| --- | --- | --- | --- |
| Terminal lifecycle/attachment/path | `terminal.proto` | terminal registry、attachment registry、filesystem query | CLI/TUI/runtime 已走 `api.execute` |
| History/live | `history.proto` | authoritative history、native live screen | TUI/CLI 已消费 generated Proto；旧 protocol DTO 已删 |
| File | `file.proto` | daemon filesystem、session file transfer registry | control plane 已迁移；Data/ACK/Finish 仍是 framing-private |
| Storage/workbench value | `storage.proto`、`workbench.proto` | opaque storage；workbench value 由 client owner | daemon workbench mutation API 与 core store 已删 |
| Endpoint runtime | `runtime.proto` | client runtime | pre-connection probe/session message 使用 generated Proto |
| Client access/remote control | `access_remote.proto` | access store、daemon remote service | CLI/core adapter 已迁移；旧 protocol alias 已删 |
| Events | `events.proto`、`application.proto` | core event broker、session subscription registry | 当前只发布 lifecycle、live invalidated、storage changed |

## Resource Semantics

- Attachment、event subscription 和 active file transfer 使用 current-session `ResourceHandle`；cancel/release 必须由 owning session registry 验证。
- Upload resume 使用独立 `FileUploadResumeHandle`。它由 verified principal、目标 path、size 和 TTL 约束，可跨 protocol session 用于续传或专用 transfer cancel，但不能用于 stream 或通用 resource release。
- File stream channel、window、chunk 和 frame type 不进入公共 Proto。protocol binding 可建立 resource 到 channel 的私有映射。
- Storage value 始终是 opaque bytes；core 不解释 workbench 或客户端 value schema。

## 暂留债务

- App/Web 生产 consumer 已完成迁移；后续不得恢复 TypeScript/Kotlin application codec、平台自有 session/resource registry 或旧 Hub/RTC bridge。
- Go 旧 DTO tests 已在 PA006T 迁到 generated Proto harness；仓库不再保留“测试后续再迁”的例外。
- CLI only-viable local route 已通过 `client/runtime.SessionOwner` 与 `client/adapter/local` 接线；完整 planner/race、SSH/managed winner 和 stamped operation 仍属于 C3B-C3H，不能在 command 内补 route fallback。
- 跨语言 ABI v3 只保留通用 `EngineCommand` Proto envelope；新增 pairing、credential 或未来业务动作时修改 schema，不新增 C/JNI/WASM 业务符号。

## 删除门禁

1. 全端生产代码扫描不得引用 `runtimepb` 或 `wirepb` application message。
2. Go protocol client 不暴露 generic `Call(method, ...)`，server 只 dispatch `api.execute`。
3. Android/Web 不得恢复旧 codec、method string、session token、multi-channel fallback 或平台自有 reconnect truth。
4. `wirepb` 只允许 framing/file stream payload；application schema 只能位于 `apipb`。
5. PA007 再执行全仓 generated code、测试、重复 schema 和双 Agent 审查。
