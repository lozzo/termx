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
  - `local`：嵌入 hub（cmux: HTTP/2+HTTP/1+ICE-TCP），LAN 暴露
  - `hub`：agent gRPC 长连接业务 hub
  - `both`：同时启动两者，Manager 持多个 hub URL
- 输出统一格式的 hub_urls（**数组**）/ QR payload（schema_version: 4）
- `termx remote enable --mode both` 使用固定 Web Control；自建/测试场景通过配置文件或 `TERMX_REMOTE_CONTROL_URL` 覆盖。`--token` 只用于自动化，无 token 时走浏览器授权。

`termx-cli` **不应实现**：Hub 逻辑、session token 验证、TURN relay、Web Controller、支付、quota。

CLI 输出不得泄漏 machine_secret、TURN secret 等敏感材料。
**不得输出**：AppCertificate、ed25519 key（已废弃）。

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
- 当前主线只关注 core+tui。`termx-cli` 在这轮只允许承担：
  - tmux/local smoke 所需的最小入口维护
  - core/tui contract 变化导致的最小 glue 修改
- 不要借这轮开发继续扩展 remote CLI 产品行为。
