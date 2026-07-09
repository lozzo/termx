# termx-cli Agent Notes

## Boundary

- `termx-cli` 是产品壳与 v2/v3 本地入口装配层，不是 core。
- 当前主线默认依赖 `termx-core-v2`、`termx-tui-v3`、`termx-shared` 与必要协议包；现存 `termx-remote` 耦合属于 legacy/frozen 清理债务，不得继续扩展。
- 不得恢复旧 `termx-core`、`tuiv2`、legacy daemon 或 remote fallback adapter。
- 不要把新的 shell-neutral 能力继续塞回 CLI。

## Frozen Remote CLI Rules

`termx-cli` 中仍然存在的 remote 入口和 `termx-remote` import 只能按根 `workflow.md` 的明确整理切片处理：

- 可以删除、隔离或标记 frozen 行为。
- 不得新增 remote app、remote-ui、web-control、旧 hub URL、QR payload 或旧 token 流程。
- 不得把 remote 失败 fallback 成 local daemon、原始 shell/PTY 或旧 app/web 控制面。
- 如果某个当前测试仍依赖 legacy remote package，先把依赖关系和删除顺序写回 `workflow.md`，再拆独立提交处理。

`termx-cli` **不应实现**：Hub 逻辑、session token 验证、TURN relay、Web Controller、支付、quota。

CLI 输出不得泄漏 machine_secret、TURN secret 等敏感材料。
**不得输出**：AppCertificate、ed25519 key（已废弃）。

需要人工配置的外部事项（DNS、TLS、OAuth、云账号）不应阻塞主线；使用 mock/stub 并在根 `workflow.md` 记录 deferred external item。

触及 remote 清理的 CLI 改动必须遵守根 `workflow.md` 的切片范围、测试准入和提交规则。

## Workflow

- 遵守根 `AGENTS.md` 与根 `workflow.md`。
- 当前 master 整理阶段只允许承担：
  - v2/v3 默认入口和测试所需的最小维护。
  - 多 endpoint / 多 transport contract 变化导致的最小 glue 修改。
  - legacy remote 入口和依赖的删除、隔离或冻结说明。
- 不要借这轮整理继续扩展 remote CLI 产品行为。
