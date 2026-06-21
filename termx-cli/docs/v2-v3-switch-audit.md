# v2/v3 默认入口与 remote 迁移审计

本文档只保留当前分支仍有效的边界。旧切片细节按 git 历史追溯；如与根目录 `workflow.md` 冲突，以 `workflow.md` 为准。

## 当前结论

- 默认本地入口已经切到 `termx-core-v2` + `termx-tui-v3`。
- 旧 `termx-core/` 与 `tuiv2/` 已删除，不再作为只读参考、legacy 命令、remote fallback 或 go.mod replace 存在。
- `termx legacy ...`、旧 daemon auto-start、`remote_runtime.go` 和 `remote_protocol_codec.go` 已删除。
- `termx remote ...` 只能通过 `resolveV3Socket` + `dialOrStartV3Client` 连接 core-v2 daemon；core-v2 remote hook 未迁移完成前，应由 core-v2 protocol 明确失败，不能启动旧 daemon 或回退旧 adapter。
- remote config/auth 路径由 CLI helper 处理，不再依赖 `tuiv2/shared`。

## 命令矩阵

| 命令 | 当前路径 | 关键约束 |
| --- | --- | --- |
| `termx` | core-v2 daemon + tui-v3 root runtime | 不读取旧 TUI config，不恢复 `runTUIv2` |
| `termx attach <id>` | tui-v3 attach runtime | 只绑定 core-v2 terminal |
| `termx daemon` | core-v2 `NewServer` | 不构造旧 core server |
| `termx new/ls/kill/rm` | core-v2 protocol client | 默认 socket 使用 v3 路径 |
| `termx remote login` | CLI HTTP login + CLI config/auth helper | 不依赖旧 TUI shared helper |
| `termx remote status/local/pair` | core-v2 protocol client | 等待后续 core-v2 remote service hook 和 adapter |

## 守卫规则

- `termx-cli/cmd/termx/default_dependency_guard_test.go` 扫描所有非测试 CLI 源文件，包括 `remote_*.go`。
- 默认和 remote CLI 源文件不得 import `github.com/lozzow/termx/termx-core` 或 `github.com/lozzow/termx/tuiv2`。
- `legacy_*.go` 不得重新出现。
- `go.work`、`termx-cli/go.mod`、`termx-testkit/go.mod` 和相关 module 文件不得恢复旧 `termx-core` / `tuiv2` replace。

## 后续 remote 迁移

后续切片必须继续按 core-v2 contract 迁移，不允许补旧 fallback：

1. 先审计并对齐 `termx-proto` / `internal/protocol` remote wire 和 typed domain contract。
2. 在 core-v2 增加 remote service hook、transport scope API、terminal create/process 字段和 storage/events routing。
3. 用 core-v2 daemon/domain adapter 装配 `termx-remote.Service`。
4. 用 smoke 证明 `remote.status`、`remote.local.*`、`remote.pair.start` 和 remote terminal/storage/transport routing 都经过 core-v2 truth。

## 验收命令

当前删除旧 fallback 切片的基线验收：

| 范围 | 命令 |
| --- | --- |
| CLI | `cd termx-cli && go test ./cmd/termx -count=1` |
| 默认依赖守卫 | `cd termx-cli && go test ./cmd/termx -run TestDefaultRuntimeSourceDoesNotImportLegacyCoreOrTUI -count=1` |
| remote | `cd termx-remote && go test ./... -count=1` |
| testkit | `cd termx-testkit && go test ./... -count=1` |
| hub module boundary | `cd termx-hub && go test ./... -count=1` |
| diff | `git diff --check` |
