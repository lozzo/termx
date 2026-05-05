# termx-cli Agent Notes

当前项目根目录：`termx-cli/`

## Boundary

- `termx-cli` 是产品壳与唯一 remote 集成层，不是 core。
- 可以依赖 `termx-core` 和 `termx-remote` 的 public package。
- 不要把新的 shell-neutral 能力继续塞回 CLI。

## Remote CLI Rules

- 当前迁移目标下，`termx-cli` 必须成为 remote 的唯一装配与集成入口。
- `termx-cli` 负责：
  - 启动/连接 daemon
  - 选择 `termx-core/clientapi` 的具体实现（in-process 或 protocol/RPC adapter）
  - 装配 `termx-remote/agent`
  - local 模式时嵌入启动 local hub
  - managed 模式时获取/注入外网 hub endpoint
  - 输出统一格式的 pairing / QR payload
- `termx-cli` 不应实现 Hub、TURN relay、Web Control、支付、订阅、quota 等业务逻辑；这些必须下沉到 `termx-remote` 或外部服务。
- CLI 输出不得泄漏 machine private key、app private key、TURN secret、connect ticket signing secret 等敏感材料。
- 需要人工配置的外部事项，例如 DNS、TLS、支付、OAuth、云账号，不应阻塞主线；使用 mock/stub 或跳过真实接入，并在根 `workflow.md` 记录 deferred external item。
- 触及 remote buildout 的 CLI 改动必须遵守根 `AGENTS.md` 的 `workflow.md` 驱动、TDD、subagent review 规则。
