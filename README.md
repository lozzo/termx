# termx

`termx` 是一个以 Go 实现的终端服务端与客户端实验项目。

它的核心模型不是 `session -> window -> pane` 这一类层级结构，而是一个扁平的 terminal pool：服务端只负责 terminal 的生命周期、PTY、I/O、snapshot 和 attach；workspace、tab、pane、floating、快捷键和 UI 编排都由客户端自己定义。

当前默认入口是内置的 `tuiv2` TUI。项目同时提供：

- 一个可独立运行的 daemon
- 一个基于 Unix socket 的 CLI
- 一个最小 web bridge，用于 xterm.js 对比和调试
- 一套可直接嵌入 Go 程序的公开 API

## 当前状态

项目处于 active 开发阶段，但 `tuiv2` 已经是可工作的默认界面：

- 启动 `termx` 直接进入 TUI
- 可创建 terminal、attach、split、tab/workspace 切换
- 支持 terminal picker / terminal pool / floating pane / help overlay
- 支持 snapshot、observer / collaborator attach 模式
- 支持 host terminal theme 派生 UI，并允许通过 `termx.yaml` 做 chrome / theme 覆盖

更细的阶段说明见 [docs/tuiv2-current-status.md](docs/tuiv2-current-status.md)。

## 设计要点

- 服务端只有 terminal，没有 session / window / pane 树
- 一个 terminal 可以被多个客户端视图同时引用
- TUI 的 workspace / tab / pane / floating 是客户端投影，不是服务端事实
- snapshot / attach / event / stream 是服务端的稳定能力
- `tuiv2` 的 UI 配色默认从宿主终端主题推导，不引入固定主题基底

项目总览见 [docs/spec-overview.md](docs/spec-overview.md)。

## 环境要求

- Go `1.26.x`
- Unix-like 环境
  - 当前主路径依赖 PTY 和 Unix socket
- 一个可交互终端
  - 直接运行 `termx` 启动 TUI 时必须是 interactive terminal

## 构建

```bash
go build ./cmd/termx
```

如果你想把二进制放到当前目录：

```bash
go build -o termx ./cmd/termx
```

## 快速开始

### 1. 启动 TUI

```bash
./termx
```

直接运行根命令会进入 `tuiv2`。如果 daemon 还没启动，CLI 会自动尝试拉起它。

### 2. 创建一个 terminal

```bash
./termx new -- bash
```

输出会返回 terminal ID。

### 3. 查看当前 terminal

```bash
./termx ls
```

### 4. 直接 attach 到某个 terminal

```bash
./termx attach <terminal-id>
```

### 5. 启动最小 web bridge

```bash
./termx web -- bash
```

或者 attach 到已有 terminal：

```bash
./termx web --id <terminal-id>
```

## CLI 概览

根命令：

```text
termx
termx attach <id>
termx daemon
termx kill <id>
termx ls
termx new -- CMD [ARGS...]
termx web [--id TERMINAL_ID] [-- CMD [ARGS...]]
```

全局参数：

- `--socket`：指定 socket 路径
- `--log-file`：指定日志文件路径
- `--config`：指定配置文件路径

默认路径规则：

- socket：
  - `$XDG_RUNTIME_DIR/termx.sock`
  - 否则 `/tmp/termx-<uid>.sock`
- log：
  - `--log-file`
  - 否则 `$TERMX_LOG_FILE`
  - 否则 `$XDG_STATE_HOME/termx/termx.log`
  - 再否则 `~/.local/state/termx/termx.log`
- workspace state：
  - `$XDG_STATE_HOME/termx/workspace-state.json`
  - 或 `~/.local/state/termx/workspace-state.json`
- config：
  - `$XDG_CONFIG_HOME/termx/termx.yaml`
  - 或 `~/.config/termx/termx.yaml`

## 配置

首次启动 TUI 时，默认配置文件会自动创建为 `termx.yaml`。

完整说明见 [docs/termx-config.md](docs/termx-config.md)。

当前主要支持两类用户偏好配置：

- `chrome`：控制 pane / status / tab 的槽位顺序与显隐
- `theme`：覆盖 host-aware 主题推导出来的 token

示例：

```yaml
chrome:
  paneTop: [pane.title, pane.actions]
  statusLeft: [status.mode, status.hints]
  statusRight: []
  tabLeft: [tab.workspace, tab.tabs]

theme:
  accent: "#8b5cf6"
  panelBorder: "#4b5563"
  tabActiveBG: "#111827"
```

说明：

- 把某个数组设为 `[]` 表示显式隐藏该区域
- 调整数组顺序表示重排
- `theme` 留空时，继续使用从宿主终端前景、背景和 palette 推导出来的默认视觉

实现入口见 [tuiv2/shared/config_file.go](tuiv2/shared/config_file.go)。

## Go API

`termx` 也可以直接作为 Go 库嵌入：

```go
package main

import (
	"context"
	"log"

	"github.com/lozzow/termx"
)

func main() {
	srv := termx.NewServer()

	info, err := srv.Create(context.Background(), termx.CreateOptions{
		Command: []string{"bash"},
		Name:    "shell",
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("created terminal %s (%s)", info.ID, info.Name)
}
```

公开 API 说明见 [docs/spec-go-api.md](docs/spec-go-api.md)。

## 仓库结构

```text
cmd/termx/        CLI 入口
docs/             设计文档、协议文档、当前状态说明
protocol/         线协议与客户端/服务端协议对象
transport/        transport 抽象与 Unix transport
tuiv2/            默认 TUI 实现
vterm/            本地终端状态模型与相关实现
pty/              PTY 管理
third_party/      本地 fork 依赖
termx.go          Server 公开 API 主入口
terminal.go       Terminal 生命周期、attach、snapshot、stream
types.go          公开类型定义
```

## 开发与测试

常用测试命令：

```bash
go test ./cmd/termx ./tuiv2/app ./tuiv2/render ./tuiv2/shared
```

如果要跑完整回归：

```bash
go test ./...
```

`tuiv2` 相关改动建议至少覆盖：

- `go test ./tuiv2/render`
- `go test ./tuiv2/app`

## 重要文档

- [docs/spec-overview.md](docs/spec-overview.md)
- [docs/termx-config.md](docs/termx-config.md)
- [docs/spec-terminal.md](docs/spec-terminal.md)
- [docs/spec-go-api.md](docs/spec-go-api.md)
- [docs/spec-transport.md](docs/spec-transport.md)
- [docs/spec-protocol.md](docs/spec-protocol.md)
- [docs/spec-snapshot.md](docs/spec-snapshot.md)
- [docs/tuiv2-current-status.md](docs/tuiv2-current-status.md)
- [docs/tuiv2-keybinding-spec.md](docs/tuiv2-keybinding-spec.md)
- [docs/tuiv2-chrome-layout-spec.md](docs/tuiv2-chrome-layout-spec.md)
- [docs/tui-v2-migration-architecture-plan.md](docs/tui-v2-migration-architecture-plan.md)

## 说明

这个仓库现在更像一个正在持续收口的系统，而不是已经冻结接口的成品。

如果你要改 `tuiv2`：

- 优先读 `AGENTS.md`
- 再读 `docs/tuiv2-current-status.md`
- 然后按相关专题文档进入对应模块
