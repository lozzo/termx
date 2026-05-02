# termx-cli Agent Notes

当前项目根目录：`termx-cli/`

## Boundary

- `termx-cli` 是产品壳，不是 core。
- 可以依赖 `termx-core` 和 `tuiv2` 的 public package。
- 不要把新的 shell-neutral 能力继续塞回 CLI。

## Remote CLI Rules

- `termx-cli` 可以负责启动/配置 `termx daemon` 内置 remote runtime，暴露 remote status、pairing、dev/debug 等用户入口。
- `termx-cli` 不应实现 Web Control Plane、Hub、支付、订阅、quota、TURN relay 业务逻辑；这些应在独立 web/hub 服务或 `termx-core` shell-neutral runtime 中按边界实现。
- CLI 输出不得泄漏 machine private key、app private key、TURN secret、connect ticket signing secret 等敏感材料。
- 需要人工配置的外部事项，例如 DNS、TLS、支付、OAuth、云账号，不应阻塞 CLI 主线；使用 mock/stub 或跳过真实接入，并在 `docs/remote-rebuild/WORKFLOW.md` 记录 deferred external item。
- 触及 remote buildout 的 CLI 改动必须遵守根 `AGENTS.md` 的文件化 todo、TDD、subagent review 规则。
