# termx-core

`termx-core` 是 monorepo 里的 shell-neutral runtime 模块。

它提供：

- terminal pool server / daemon
- protocol / transport
- pty / vterm / snapshot
- events
- shared session / workbench service
- 可嵌入的 Go API

它不直接提供：

- `termx` CLI
- `tuiv2` TUI
- web/mobile 产品壳

这些壳层分别位于：

- `../termx-cli/`
- `../tuiv2/`

## 构建与测试

```bash
go test ./...
```

## 关键包

- `clientapi/`：shell-neutral client contract
- `protocol/`：线协议
- `transport/`：传输抽象与实现
- `workbenchdoc/` / `workbenchops/` / `workbenchsvc/`：共享 session / workbench contract

## 文档

核心文档见 [docs/README.md](docs/README.md)。

如果你要看 TUI 设计和交互文档，请去：

- [../tuiv2/docs/README.md](../tuiv2/docs/README.md)

如果你要看整仓 remote / tgent / 跨模块演进文档，请去：

- [../docs/README.md](../docs/README.md)
