# termx-cli

`termx-cli` 是 monorepo 里的命令行产品壳。

职责：

- `termx` 根命令
- daemon / new / ls / attach / kill / web 等命令入口
- 把 `termx-core` 与 `tuiv2` 组装成最终 CLI 行为

开发入口：

```bash
cd termx-cli
go test ./...
go build ./cmd/termx
```
