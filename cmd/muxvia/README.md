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
make build
```

`make build` 生成内置 Cloud Companion 的单文件产品二进制。只有验证公开源码边界时才运行
`make build-public`；直接 `go build ./cmd/muxvia` 等价于不嵌入私有 Cloud artifact 的源码构建，不能作为产品测试产物。
