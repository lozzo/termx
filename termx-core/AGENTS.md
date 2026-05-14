# termx-core Agent Notes

当前项目根目录：`termx-core/`

## Boundary

- `termx-core` 是 shell-neutral runtime，不是 TUI、CLI、web/mobile 或 remote 产品壳。
- `termx-core` 可以被 `tuiv2/`、`termx-cli/`、未来 `web/`、`mobile/`、`termx-remote/` 复用。
- 禁止在 `termx-core` 中新增对 `tuiv2/*`、`termx-cli/*`、`termx-remote/*` 或其他壳层模块的反向依赖。

## Public Interfaces

- shell-neutral daemon client contract：`clientapi/`
- wire contract：`protocol/`、`transport/`
- generic client state contract：core storage APIs

## Rules

- screen update / snapshot / bootstrap 相关传输协议必须保持二进制编码；不要把线上链路改成 JSON。
- 共享能力优先进 public package，不要通过 `internal/*` 给外部壳层偷开入口。
- 改动 core 时，优先维护协议边界、服务模型和可复用性，不要引入 shell-specific 行为。
- 提交代码时，commit message 必须尽可能详细，准确写清动机、范围、关键实现与行为变化。

## Remote Migration Rules

- 当前迁移目标是让 `termx-core` 不再承载任何 remote 产品域代码。
- `termx-core` 可以保留 daemon/core 能力与对应 public interface，但不得继续承载：
  - remote runtime
  - machine identity / app certificate / pairing 产品编排
  - WebRTC offer/answer 产品编排
  - localweb remote API
  - local hub / hub discovery / signaling / QR payload
  - remote.* RPC 暴露面
- `termx-core` 与 remote 域的正确关系应为：
  - `termx-core/clientapi` 定义 shell-neutral daemon capability interface
  - `termx-core/protocol/client` 只是该 interface 的一个 RPC adapter
  - `termx-remote` 依赖这些 interface 来实现 remote 产品逻辑
- 遇到支付、邮件、OAuth、云服务账号等外部依赖，不要塞进 core；记录到根 `workflow.md` 的 deferred item。
- 触及本次迁移的切片必须按根 `AGENTS.md` 的 `workflow.md` 驱动、TDD、subagent review 规则执行。
