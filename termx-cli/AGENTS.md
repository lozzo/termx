# termx-cli Agent Notes

## Boundary

- `termx-cli` 是产品壳与唯一 remote 集成层，不是 core。
- 可以依赖 `termx-core` 和 `termx-remote` 的 public package。
- 不要把新的 shell-neutral 能力继续塞回 CLI。

## Remote CLI Rules

`termx-cli` 负责：

- 启动/连接 daemon（in-process 或 socket）
- 绑定 `termx-core/clientapi`
- 装配 `termx-remote.Service`
- 根据 `--mode` 启动对应运行时：
  - `local`：调用 `service.LocalEnable()`，嵌入 hub，LAN 暴露，ICE-TCP
  - `cloud`：调用 `service.CloudEnable()`，agent 连接云端 hub
  - `both`：同时启动两者，Manager 持两个 hub URL
- 输出统一格式的 hub_url / QR payload

`termx-cli` **不应实现**：Hub 逻辑、cert 验证、TURN relay、Web Controller、支付、quota。

CLI 输出不得泄漏 machine private key、app private key、TURN secret 等敏感材料。

需要人工配置的外部事项（DNS、TLS、OAuth、云账号）不应阻塞主线；使用 mock/stub 并在根 `workflow.md` 记录 deferred external item。

触及 remote buildout 的 CLI 改动必须遵守根 `AGENTS.md` 的 TDD、subagent review 规则。

## 待清理的死代码

以下代码存在残留，**必须删除**，否则构建失败：

- `cmd/termx/web.go` — import 已删除的 `internal/webshell` 包，整个文件删除。
- `cmd/termx/main.go` 第 181 行：`cmd.AddCommand(webCommand(&socket, &logFile))` 删除此行。
- `internal/webshell/` — 实现文件已从 git 删除，目录应已为空。

## Workflow

- 遵守根 `AGENTS.md` 与根 `workflow.md`。
- 每个切片 TDD 推进，切片完成后独立 subagent review。
