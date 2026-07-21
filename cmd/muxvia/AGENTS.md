# cmd/muxvia Agent Notes

## Boundary

- `cmd/muxvia` 是产品壳与 v2/v3 本地入口装配层，不是 core。
- 当前主线默认依赖 `core`、`tui`、`shared` 与必要协议包；不得恢复 `termx-remote` runtime、命令或 module replace。
- 不得恢复旧 `termx-core`、`tuiv2`、legacy daemon 或 remote fallback adapter。
- 不要把新的 shell-neutral 能力继续塞回 CLI。

## Frozen Remote CLI Rules

`cmd/muxvia` 的 frozen remote 入口已从默认主线清理。后续只能按根 `workflow.md` 的明确切片重新设计新控制面：

- 不得新增 remote app、remote-ui、web-control、旧 hub URL、QR payload 或旧 token 流程。
- 不得把 remote 失败 fallback 成 local daemon、原始 shell/PTY 或旧 app/web 控制面。
- 不得重新 import `github.com/muxvia/muxvia/termx-remote`；如确需新控制协议，先在 `workflow.md` 拆设计切片。

`cmd/muxvia` **不应实现**：Hub 逻辑、session token 验证、TURN relay、Web Controller、支付、quota。

CLI 输出不得泄漏 machine_secret、TURN secret 等敏感材料。
**不得输出**：AppCertificate、ed25519 key（已废弃）。

需要人工配置的外部事项（DNS、TLS、OAuth、云账号）不应阻塞主线；使用 mock/stub 并在根 `workflow.md` 记录 deferred external item。

触及 remote 清理的 CLI 改动必须遵守根 `workflow.md` 的切片范围、测试准入和提交规则。

## Workflow

- 遵守根 `AGENTS.md` 与根 `workflow.md`。
- 当前 master 整理阶段只允许承担：
  - v2/v3 默认入口和测试所需的最小维护。
  - 多 endpoint / 多 transport contract 变化导致的最小 glue 修改。
  - legacy remote 入口和依赖不得恢复的守卫说明。
- 不要借这轮整理继续扩展 remote CLI 产品行为。
