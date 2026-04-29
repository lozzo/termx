# termx-core Agent Notes

当前项目根目录：`termx-core/`

## Boundary

- `termx-core` 是 shell-neutral runtime，不是 TUI、CLI 或 web/mobile 产品壳。
- `termx-core` 可以被 `tuiv2/`、`termx-cli/`、未来 `web/`、`mobile/`、TURN/WebRTC 服务复用。
- 禁止在 `termx-core` 中新增对 `tuiv2/*`、`termx-cli/*` 或其他壳层模块的反向依赖。

## Public Interfaces

- shell-neutral client contract：`clientapi/`
- wire contract：`protocol/`、`transport/`
- shared session/workbench contract：`workbenchdoc/`、`workbenchops/`、`workbenchsvc/`

## Rules

- screen update / snapshot / bootstrap 相关传输协议必须保持二进制编码；不要把线上链路改成 JSON。
- 共享能力优先进 public package，不要通过 `internal/*` 给外部壳层偷开入口。
- 改动 core 时，优先维护协议边界、服务模型和可复用性，不要引入 shell-specific 行为。
- 提交代码时，commit message 必须尽可能详细，准确写清动机、范围、关键实现与行为变化。
