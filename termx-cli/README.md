# termx-cli

`termx-cli` 是 monorepo 里的命令行产品壳。

职责：

- `termx` 根命令
- daemon / new / ls / attach / kill / rm 等本地 v2/v3 命令入口
- 把 `termx-core-v2` 与 `termx-tui-v3` 组装成最终 CLI 行为
- 默认入口必须走 `termx-core-v2` 与 `termx-tui-v3`
- 不装配 frozen `termx-remote` runtime，也不暴露旧 `termx remote ...` 命令

开发入口：

```bash
cd termx-cli
go test ./cmd/termx -count=1
go build ./cmd/termx
```
