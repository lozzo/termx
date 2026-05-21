# termx-core Agent Notes

当前项目根目录：`termx-core/`

## Boundary

- `termx-core` 是 shell-neutral runtime/history 内核，不是 TUI、CLI、web/mobile 或 remote 产品壳。
- 可以被 `tuiv2/`、`termx-cli/` 和未来其他壳层复用，但禁止新增对 `tuiv2/*`、`termx-cli/*`、`termx-remote/*` 或其他壳层模块的反向依赖。
- 共享能力优先进 public package（如 `clientapi/`、`protocol/`、`transport/`、core storage APIs），不要通过 `internal/*` 给壳层偷开入口。
- `screen update` / `snapshot` / `bootstrap` 相关线上传输继续使用二进制编码，不要改成 JSON。

## Current Line Focus

- 当前主动开发线只关注 `termx-core` / `termx-vterm` / `tuiv2`。
- `termx-core` 在这轮优先服务：
  - canonical row identity / generation
  - persisted history / mutable live tail / screen projection ownership 边界
  - resize ownership metadata
  - retention / paging / stale-page 语义
- `persisted history store` 只承载已提交的完整 logical lines；`mutable live tail` 可以暂存 still-open latest tails，也可以包含从 persisted history reclaim 回来的 sealed suffix。
- 当前 resize authority 只在 `termx-vterm` -> `termx-core` 的内部 contract 上显式化：`ResizeLiveTailRows` 与 `ResizeLiveTailRowsSet` 只定义 resize append suffix 的 live-tail / persisted-history 切分；不要在 wire/runtime/app 路径上假定更宽语义，除非根 `workflow.md` 明确打开。
- 只有 core/tui contract 必须变化时，才允许最小化触及 `internal/protocol`、`termx-proto`、`termx-cli` glue。

## Workflow

- 唯一有效驱动文件是 repository-root `workflow.md`。
- 每完成一个 module-sized slice，立即压缩更新根 `workflow.md`，只保留当前决策、最新证据、当前风险、下一步。
- core 侧 contract 变化先写进根 `workflow.md`，再进入实现或和实现同切片落地。

## Keep Out Of Core

- 不要把 remote 产品域代码继续塞进 `termx-core`。例如 WebRTC pairing、hub discovery、signaling、QR payload、localweb API、remote runtime 编排都不属于这里。
- 改动 core 时优先维护协议边界、服务模型和可复用性，不要引入 shell-specific 行为。
