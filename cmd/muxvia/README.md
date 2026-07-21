# cmd/muxvia

`cmd/muxvia` 是 monorepo 里的命令行产品壳。

职责：

- `muxvia` 根命令
- daemon / new / ls / attach / kill / rm 等本地 v2/v3 命令入口
- 把 `core` 与 `tui` 组装成最终 CLI 行为
- 默认入口必须走 `core` 与 `tui`
- 不装配 frozen `termx-remote` runtime，也不暴露旧 `termx remote ...` 命令

开发入口：

```bash
go test ./cmd/muxvia -count=1
go build ./cmd/muxvia
```
