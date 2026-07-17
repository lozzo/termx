# Proto API Inventory

## 目的

本文冻结 PA002 对当前 API 资产的只读盘点。它不决定最终 field number，也不授权实现迁移；PA003 必须以本表为输入设计公共 application schema、兼容 envelope 和删除顺序。

所有跨边界 API 的最终真值必须在 `proto/`。表中的 Go domain model 可以保留在 owner 内部，但 Go-only request/result/event、protocol alias 和重复 projection 必须迁移或删除。

## 当前 Proto 集合

| Schema | 当前责任 | 主要消费者 | 结论 |
| --- | --- | --- | --- |
| `proto/wirepb/terminal.proto` | daemon wire framing，同时包含 terminal/history/live/file/storage/workbench/remote application message | Go `internal/protocol`、core、CLI/TUI protocol adapter | application 与 framing 混合；字段最完整，但不能继续同时作为 transport implementation 和公共 API package |
| `proto/runtimepb/runtime.proto` | Web/mobile shared UI 的公开 runtime request/response、terminal/file/storage projection | `clients/ui`、`clients/mobile` TypeScript | 已声明为公开 schema truth，但与 `wirepb` 大量重复，且 method/path/body 仍是弱类型 envelope |
| `proto/remoteauthpb/remote_auth.proto` | E2E auth、CapabilityGrant、client access、endpoint bootstrap/share | remoteauth、endpoint contract、daemon/client auth | ownership 基本正确；Go alias/domain copy 仍需通过 API Mapping 收口 |
| `proto/cloudpb/cloud_companion.proto` | Companion、login/enrollment、presence/signaling、relay lease、managed route | public companion client 与 private Cloud implementation | schema 边界相对完整；保持 Cloud control plane 与 terminal payload 分离 |

## 跨 Schema 冲突

### `wirepb` 与 `runtimepb`

两者都定义 terminal inventory、size、resize ownership、terminal create、file、storage。字段并不等价：

- `wirepb.TerminalInfo` 包含 tags、live cwd、created/exited metadata 和 attachment count；`runtimepb.TerminalInventoryItem` 包含 environment、size lock mode，但缺少多项 lifecycle metadata。
- 两者各自定义 `Size`、`ResizeOwnership`、file entry/list/preview/transfer 和 storage entry/request/result。
- `runtimepb.APIRequest/APIResponse` 以 string method/path + bytes body 分发，无法静态表达 command/result 对应关系、capability、typed error 或 resource handle。
- `wirepb.RequestEnvelope` 同样以 method string + bytes params 分发，且与 Hello/channel framing 同包。

PA003 必须选定单一公共 application package。建议以现有公开 `runtimepb` 的发布责任为迁移落点，重构为 typed command/result/event；`wirepb` 在兼容期只保留 framing 与旧连接 payload，不允许继续新增 application message。最终选择必须通过 compatibility 和 consumer inventory 验证后写入 schema 文档。

## 领域盘点

| 领域 | 现有 Proto | Go-only/重复类型 | 真实消费者 | 主要缺口 | PA003+ 目标与删除条件 |
| --- | --- | --- | --- | --- | --- |
| Terminal lifecycle | `wirepb.TerminalInfo/CreateParams/CreateResult/GetParams/ListResult`；`runtimepb.TerminalInventoryItem/TerminalCreateRequest/TerminalIDRequest` | `internal/protocol.TerminalInfo/CreateParams/CreateResult/ListResult/TerminalResourceUsage`；core 内部 `TerminalInfo` | core protocol service、CLI terminal commands、TUI terminal adapter、remote E2E | 两套 proto 字段漂移；state string；resource usage 使用 unknown fields；无 typed command envelope | 公共 proto 定义 terminal ref、create/list/get/mutation/metadata result；API Mapping 从 core domain 映射；删除 protocol DTO，core internal model 保留 |
| Attachment/input/resize | `wirepb.AttachParams/AttachResult/EnsureResizeParams/EnsureResizeResult/ResizeControl/ResizeOwnership` | protocol 同名 DTO、`DetachParams`、`InputParams`、`ResizeControlParams/Result` | TUI live/terminal adapter、CLI attach/automation、core attachment registry | 缺 operation ID、endpoint session generation、stale detail、input replay classification；detach 复用其它 wire message | proto 增加 attachment/session fence、operation ID、typed resize policy/control/error；迁移后删除 protocol DTO，禁止隐式重放 |
| Path/defaults | `wirepb.PathListDirs*`、`PathDefaultsResult` | protocol 同名 DTO | TUI path adapter、CLI create/file、core filesystem | runtimepb 无 path API；错误/空态未 typed | 进入公共 application proto；Missing/Truncated 保持业务结果；删除 protocol DTO |
| History | `wirepb.HistoryWindowParams/HistoryWindow` | protocol `HistoryWindowParams/HistoryWindow/HistoryLineSpan/HistoryBacklogStatus` | core history service、TUI history/copy、CLI dump/backlog | 大量正式字段只以 unknown field number 编码；token/generation/cursor/boundary 语义不完整；无 typed freeze/release handle | 先把全部 active unknown fields 正式写入公共 proto，再定义 window/copy/freeze/release command；删除 unknown-field fallback 和 protocol projection |
| Live screen | `wirepb.Snapshot/RowSet/CursorState/TerminalModes` | protocol `NativeScreenSnapshot/Cell/CellStyle/CompactRow*` | core live snapshot、TUI live surface | live revision 使用 unknown field；复用 snapshot message；缺 invalidation command/event 正式字段 | 定义 native screen/cell/cursor/modes/revision 与 invalidation event；history generation 与 live revision 分离；删除 snapshot unknown-field 编码 |
| File | `wirepb.File*`；`runtimepb.File*` | `internal/protocol/file.go` 与 `file_transfer_payload.go` DTO | CLI file、core file service、Web/mobile runtime | 双 proto schema；wire channel/ACK/frame 暴露；错误是 string；transfer handle/release 不统一 | 公共 proto 使用高层 list/stat/mutate/read/write/transfer handle；frame 留在 transport adapter；删除 protocol file DTO 和双 schema 字段漂移 |
| Storage | `wirepb.Storage*`；`runtimepb.Storage*` | protocol `StorageScope/Entry/*Params/*Result` | TUI workbench/clipboard storage、core daemon storage、Web/mobile | scope 一边 enum 一边 string；时间类型不同；CAS error 未 typed | 公共 proto 统一 enum、version/CAS error、opaque bytes；删除 protocol DTO；core storage 仍不理解 value schema |
| Workbench | `wirepb.Workbench*` | protocol 同名 snapshot/mutation DTO；TUI port/state projection | TUI workbench、core legacy workbench service | daemon 专用 workbench API 与“storage opaque、client-owned schema”冲突；pane kind 含 lifecycle/copy 状态 | 不扩展公共 daemon workbench API；确定迁移到 client-owned proto value schema + storage CAS 后删除 core/protocol 专用 API |
| Endpoint/route | `remoteauthpb.Endpoint*`、bootstrap/share messages | `client/endpoint` registry/domain structs；client runtime endpoint event/request | CLI endpoint、client planner/runtime、TUI endpoint store | runtime connect/disconnect/event/session stamp 没有公共 proto；domain registry 与 published bootstrap 的边界未列明 | registry domain 可保留；新增公共 runtime command/event/session stamp proto；API Mapping 负责 registry/bootstrap/runtime projection |
| Client access | `remoteauthpb.ClientAccess*` | `shared/remoteauth` domain structs、`internal/protocol` alias/params、`client/runtime/application_endpoint_access.go` DTO | core access service、CLI access、remote auth | 已有 proto 但 consumer 仍使用 Go alias/copy；stable error/capability envelope 不统一 | API Layer 直接使用 generated remoteauth messages 或由公共 command 包引用它们；删除 runtime Go-only Access DTO 和 protocol aliases |
| Endpoint probe | 无完整 application proto；Cloud 有 managed route/presence message | `client/runtime.EndpointProbeRequest/Result` | 目前只有 contract test | observed path/reason/session stamp 无 schema | PA003 定义 probe command/result；删除 Go-only contract 后再实现 consumer |
| Remote daemon control | `wirepb.RemoteStatus/PairStart/Local*` | protocol 同名 DTO | CLI remote、core RemoteService | 与 remoteauth/cloud responsibility 交叉；method string；错误不 typed | 明确 local daemon control 与 Cloud/remoteauth 边界，进入公共 application proto；删除 protocol DTO |
| Cloud companion | `cloudpb` 完整 request/response/event | shared wrapper/domain projection | CLI Cloud、Companion、private Cloud | shared wrapper 可能复制 enum/error；与 terminal API 不应合并 | 保持独立 Cloud schema；API Layer 只能组合 opaque managed route capability，不接 terminal payload |

## Go-only API 债务

### 必须优先删除或迁移

- `client/runtime/application_endpoint_access.go`：Endpoint probe 与 access application request/result 全是 Go-only 跨边界 DTO；access 已有 `remoteauthpb`，probe 缺 proto。
- `internal/protocol/messages.go`：除 framing/connection primitive 外，大量 terminal/history/live/storage/workbench/remote DTO 与 proto 重复。
- `internal/protocol/file.go`、`file_transfer_payload.go`：与两个 proto package 重复，并泄露 wire transfer 细节。
- `internal/protocol/client_access.go`：使用 alias 暂时掩盖 domain/proto 双 owner，违反迁移完成后不得 alias 的规则。

### 可以作为内部 domain 保留

- `core.TerminalInfo`、terminal state、history storage/index、attachment registry 等 core-only model。
- `client/endpoint` registry、route planner 和 credential descriptor 的内部 domain model；发布/导入必须经 API Mapping 与 proto contract。
- TUI reducer/view model、selection/layout/render DTO；仅 UI 字段可以保留，daemon/API 字段必须从 proto 投影。

## 隐式协议债务

以下语义已在 production 使用，但未正式存在于 proto schema，而是通过 protobuf unknown fields 写入：

- terminal exited/resource usage 字段。
- terminal state event exited time、live invalidation revision、observed live revision。
- native screen live revision。
- history mode、before/after cursor、range、segment、row/session/frame/fixed-grid/screen-size metadata。
- history backlog status 全部诊断字段。

PA003 必须为仍有真实 consumer 的字段分配正式 schema；没有 consumer 或违反当前 truth model 的字段直接删除。禁止把 unknown-field helper 当成长期兼容机制。

## Envelope 与错误缺口

当前存在三套不一致错误模型：

- `wirepb.ProtocolError {int32 code, string message}`。
- `runtimepb.APIResponse {int32 status, string error, bytes body}`。
- `cloudpb.CloudError` 与 `remoteauthpb.AuthErrorCode` 的 typed 模型。

公共 application API 需要统一：

- versioned command/result/event envelope。
- request ID、operation ID、endpoint/session generation、capability/version。
- typed error code + oneof detail + attempted/retryable 标记。
- explicit cancel/release 与 opaque resource handle。
- unknown command/version/capability 的 fail-closed 行为。

## Consumer 迁移顺序

1. 先稳定 public application proto 与 compatibility harness。
2. 建立 API Layer 和 core/proto API Mapping，不接 UI 或 transport。
3. 迁移 terminal/attachment/path，删除对应 protocol DTO。
4. 迁移 history/live，先消灭 unknown-field 正式语义。
5. 迁移 file/storage，并决定 workbench 退出 daemon 专用 API 的方式。
6. 迁移 endpoint/access/runtime command/event。
7. CLI/TUI/remote/Cloud first-party consumer 全部通过同一 API；最后才开放插件 SDK 或平台 binding。

## PA002 完成检查

- 每个活动领域都有 proto owner、Go-only debt、consumer、缺口和删除条件。
- 明确记录 `wirepb/runtimepb` 双 schema 和 unknown-field 债务。
- 没有新增 schema、generated code、API Layer 或 API Mapping 实现。
