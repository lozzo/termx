# Proto API Inventory

## 当前结论

- `proto/apipb/` 是公共 application API 的唯一 schema truth。
- Go 运行链路固定为 `client -> protocol framing -> apipb -> api_layer -> api_mapping -> core`。
- `internal/protocol/` 的 Go 生产代码只保留 Hello、request/response correlation、`api.execute` payload transport、stream channel 和 file frame/ACK/finish。
- `core/` 保留领域模型；这些类型不是跨进程 API，也不能被客户端直接依赖。
- 当前只完成 Go 端迁移。App/Web 尚未迁移，因此 `runtimepb` 与 `wirepb` 中被它们消费的旧 application schema 暂留；Go 生产代码不得使用它们。

## Schema Ownership

| Schema | Owner | 当前状态 |
| --- | --- | --- |
| `proto/apipb/` | terminal、attachment、path、history、live、file、storage、workbench value、endpoint runtime、client access、remote daemon control、application events | Go 端唯一公共 application schema |
| `proto/wirepb/` | framing 与 App/Web 迁移期旧 schema | Go 端只使用 framing/file stream message；重复 application message 在 PA005W 后删除 |
| `proto/runtimepb/` | App/Web 迁移期 runtime schema | 不得新增；App/Web 切到 `apipb` 后删除 |
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
- Upload resume 使用独立 `FileUploadResumeHandle`。它由 verified principal、目标 path、size 和 TTL 约束，可跨 protocol session 使用，但不能用于 stream、cancel 或通用 resource release。
- File stream channel、window、chunk 和 frame type 不进入公共 Proto。protocol binding 可建立 resource 到 channel 的私有映射。
- Storage value 始终是 opaque bytes；core 不解释 workbench 或客户端 value schema。

## 暂留债务

- App：Android/native bridge 与 shared client 仍有旧 method/string codec，进入 `PA005A` 后迁移。
- Web：browser RTC 仍有旧 runtime/multi-channel adapter，进入 `PA005W` 后迁移或删除。
- `wirepb/runtimepb` 重复 application schema 只因上述 consumer 暂留，不是兼容承诺。
- Go 旧测试仍引用已删除 DTO；用户已明确把测试迁移放到实现收口之后。
- CLI composition root 仍缺既有 endpoint runtime helper；不得用 legacy client、假 generation 或 fallback 修补。

## 删除门禁

1. Go 生产代码扫描不得引用 `runtimepb` 或 `wirepb` application message。
2. Go protocol client 不暴露 generic `Call(method, ...)`，server 只 dispatch `api.execute`。
3. App 迁移完成后删除其旧 codec 和 method string。
4. Web 迁移完成后删除 `runtimepb`、`wirepb` 重复 application schema及旧 TS generated code。
5. PA007 再执行全仓 generated code、测试、重复 schema 和双 Agent 审查。
