# TermX Platform Legacy Archive

本目录保存远程平台重构前的私有实现资产，只供代码考古和迁移决策参考。

- `termx-hub/` 从顶层同名目录按 RP005 原样移动。
- `termx-remote/` 从顶层同名目录按 RP007 原样移动；其中 `remote-ui-docs/`、`remote-ui-doc-tests/` 和 `remote-ui-localweb/` 保存已退出公开 runtime 的历史设计、测试与 localweb 入口。
- `web-control/` 从顶层同名目录按 RP007 原样移动；活动私有服务实现位于 `private/termx-cloud/`，不得从 archive 启动或复用旧 schema。
- archive 不加入 `go.work`，不允许被 public/private runtime import、replace 或脚本执行。
- 旧 session token、agent token、terminal inventory、heartbeat kick、共享 TURN secret 和 24h credential 都不是兼容 contract。
- 需要复用算法或测试思路时，必须按 `docs/remote-platform/` 当前模型在 `private/termx-cloud/` 重新实现。
- 原始迁移来源可通过 RP005/RP007 移动提交的 parent 追溯，不复制独立 Git 历史。
