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

## 快速启动

如果你在调本地 embedded web / 前端，不想每次手动先构建 `remote-ui` 再构建 Go：

```bash
make remote-dev
```

常用目标：

```bash
make localweb-build   # 生产构建前端并同步到 termx-core 内嵌静态资源
make termx-build      # 只编译 Go，输出到 ./bin/termx
make remote-dev       # 编译 Go、启用/复用本地 remote、启动 Vite dev server 热更新
make remote-daemon    # 前台启动带内嵌静态资源的 local remote daemon
make remote-open      # 启用/复用本地 remote 并打印内嵌页面 URL
```

`make remote-dev` 现在会让前端走 `npm run dev`。本地 daemon / API 继续跑在 `127.0.0.1:18888`，Vite dev server 默认跑在 `127.0.0.1:5173`，`/api/*` 会自动代理到 daemon，所以只改前端时不需要重新做 embedded build。启动时终端里还会直接打印一组 `Pair ID` / `Pair secret`，方便你马上在 dev 页面里配对。

默认监听地址：

- web: `127.0.0.1:18888`
- ICE TCP: `127.0.0.1:18889`
- Vite dev: `127.0.0.1:5173`

可以按需覆盖：

```bash
make remote-dev LOCAL_WEB_ADDR=127.0.0.1:19988 ICE_TCP_ADDR=127.0.0.1:19989 REMOTE_UI_DEV_PORT=5174
```

注意：`http://127.0.0.1:5173` 和内嵌页 `http://127.0.0.1:18888` 是两个不同 origin，第一次切到 dev server 时通常需要重新 pair 一次，因为浏览器本地存储不共享。

停止运行中的 Vite dev server 直接 `Ctrl+C`。需要重新生成内嵌静态资源时，再跑 `make localweb-build` 或 `make remote-daemon`。

## 说明文件

- 仓库级说明：`AGENTS.md`
- Core 子项目说明：`termx-core/AGENTS.md`
- Core 子项目背景文档：`termx-core/README.md`
- CLI 子项目说明：`termx-cli/AGENTS.md`
