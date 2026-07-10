# TermX Platform Legacy Archive

本目录保存远程平台重构前的私有实现资产，只供代码考古和迁移决策参考。

- `termx-hub/` 从顶层同名目录按 RP005 原样移动。
- archive 不加入 `go.work`，不允许被 public/private runtime import、replace 或脚本执行。
- 旧 session token、agent token、terminal inventory、heartbeat kick、共享 TURN secret 和 24h credential 都不是兼容 contract。
- 需要复用算法或测试思路时，必须按 `docs/remote-platform/` 当前模型在 `private/termx-cloud/` 重新实现。
- 原始迁移来源 commit 可通过本文件加入仓库的 RP005 commit parent 追溯，不复制独立 Git 历史。
