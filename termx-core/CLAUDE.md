# termx-core

当前项目根目录：`termx-core/`

## 定位

`termx-core` 是 PTY 托管 runtime：

- server / daemon
- terminal lifecycle
- protocol / transport
- pty / vterm / snapshot
- events
- shared session / workbench service
- shell-neutral Go API

它不是：

- `termx` CLI 产品壳
- `tuiv2` TUI 库
- web/mobile 端壳

## 依赖方向

- 允许：`tuiv2 -> termx-core`
- 允许：`termx-cli -> termx-core`
- 禁止：`termx-core -> tuiv2`
- 禁止：`termx-core -> termx-cli`

## 常用命令

```bash
go test ./...
```

如果只想看模块拆分边界，先读：

- [../ARCHITECTURE_SPLIT.md](../ARCHITECTURE_SPLIT.md)
- [docs/README.md](docs/README.md)
