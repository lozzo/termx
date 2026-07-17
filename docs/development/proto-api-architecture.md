# Proto API 架构

## 强约定

termx 的所有插件、第三方客户端、官方客户端、跨进程服务和跨语言 binding API，必须先定义在 `proto/`。Proto schema 是字段、枚举、command/event、错误 detail、capability、版本和兼容语义的唯一真值。

禁止先定义 Go request/result/event struct，再把它映射成 proto。禁止把 core domain struct、TUI state 或 protocol package DTO 当作外部 API。

## 完整链路

```text
core
  domain truth / state machine / terminal lifecycle / history
    <->
api_layer
  application dispatch / authorization / cancellation / resource lifecycle
    <->
transformer
  core domain <-> generated proto / platform projection
    <->
plugins and clients
  CLI / TUI / mobile / desktop / web / third-party
```

### Core

- 拥有 terminal、attachment、history、live、file/storage 等领域真值。
- 可以定义内部 domain struct 和 value object。
- 不向插件或客户端暴露内部 Go 类型。
- 不依赖 UI、Cobra、具体 transport、插件或平台 binding。

### API Layer

- 对外参数、结果、事件和稳定错误 detail 只使用 proto 生成类型。
- 负责 application method dispatch、授权、取消、资源分配与释放、stream 生命周期和错误分类。
- 不负责 protobuf 字段转换、socket framing、WebRTC、SSH、UI projection 或 route fallback。
- 允许依赖 core 的窄 domain interface，但不得把 core struct 泄露到公开方法。

### Transformer

- 是 core domain 与 generated proto 之间唯一允许的字段转换点。
- 转换必须无状态、确定性、可单测并显式失败。
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

已有 `wirepb` 同时包含 application message 与 framing，是迁移输入，不是最终目录结论。拆分前先做 inventory 和 compatibility 设计，禁止直接移动 schema 或改 field number。

## API 设计要求

- 使用 versioned command/event envelope；未知 command、enum 和 field 必须有明确兼容行为。
- 非幂等 input、paste、resize、detach 必须携带 operation/session fence，失败后不得隐式重放。
- 长生命周期资源使用 opaque handle，并有显式 release/cancel；不得暴露 Go pointer 或 goroutine。
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
  -> transformer
  -> core adapter
  -> client/plugin consumer
  -> delete old Go-only API
```

任何一步需要 alias、双路径、fallback 或复制 DTO 才能继续时，必须停止并重新判断 schema 与 owner。

## 依赖守卫

- `api_layer/` 禁止依赖 `tui/`、`cmd/`、具体 transport、private Cloud implementation 或插件实现。
- `transformer/` 禁止依赖 runtime manager、storage implementation、transport、UI 和 private Cloud implementation。
- `internal/protocol/` 禁止定义与 application proto 同字段的业务 DTO。
- `client/runtime`、`tui/port` 和平台 binding 禁止复制 application proto 业务字段。
- 生成代码必须通过仓库 generated-code check，禁止手工编辑。

## 当前迁移债务

- `core/api` 是错误的平行 Go API，必须删除。
- `internal/protocol/messages.go` 仍拥有大量 application DTO，需按领域迁移到 proto 生成类型。
- `internal/protocol/control_payload.go` 同时承担 dispatch、业务转换和 wire codec，需拆到 API Layer、Transformer 和 protocol framing。
- TUI/CLI 当前直接依赖 protocol client，需在共享 runtime 和 API Layer 稳定后迁移。
