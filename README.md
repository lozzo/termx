# termx Monorepo

这个仓库现在按多项目结构收口。

当前已拆出的 Go 项目：

- `termx-core/`：server / protocol / transport / snapshot / workbench session / Go API
- `tuiv2/`：TUI client / render / runtime / workbench projection
- `termx-cli/`：`termx` CLI / daemon wiring / web bridge shell

预留的后续项目落点：

- `web/`
- `mobile/`
- `turnserver/` 或其他独立服务目录

## 当前入口

如果你要开发 core：

```bash
cd termx-core
```

常用命令：

```bash
go test ./...
```

如果你要开发 TUI 库：

```bash
cd tuiv2
go test ./...
```

如果你要构建 `termx` CLI：

```bash
cd termx-cli
go build ./cmd/termx
```

## 说明文件

- 仓库级说明：`AGENTS.md`
- Core 子项目说明：`termx-core/AGENTS.md`
- Core 子项目背景文档：`termx-core/README.md`
- CLI 子项目说明：`termx-cli/AGENTS.md`
