# termx — PTY 托管服务

## 项目定位

termx 是一个 Go 编写的 **PTY 托管服务**（不是终端复用器）。它管理一个扁平的 Terminal 池，提供创建、销毁、读写、resize 操作，不强加任何组织结构。

**核心哲学：服务端只有 Terminal，没有 session/window/pane/tab。** 所有组织概念（workspace、标签页、分组）都由客户端自行实现。

```
tmux:     session → window → pane     (服务端强制 3 层树)
zellij:   session → tab → pane        (服务端强制 3 层树)
termx:    Terminal                     (服务端扁平，客户端自由组织)
```

## Go 包结构

```
termx/
├── cmd/termx/          # CLI（daemon、attach、ls、new、kill）
├── termx.go            # Server 类型和高级 API
├── terminal.go         # Terminal 类型和状态机
├── event.go            # 事件类型和 EventBus
├── snapshot.go         # 快照类型和序列化
├── pty/                # PTY 管理（creack/pty 封装）
├── vterm/              # 虚拟终端（屏幕缓冲区）
├── fanout/             # 输出多播
├── transport/          # 传输层接口和实现
│   ├── unix/           # Unix domain socket
│   ├── ws/             # WebSocket
│   └── memory/         # Go 进程内（测试/嵌入）
├── protocol/           # 线协议编解码
└── tui/                # TUI 客户端（bubbletea）
```

## 核心概念

- **Terminal**：唯一实体。包含 PTY 进程、虚拟终端缓冲区、元数据 tags
- **Server**：管理 Terminal 池，暴露 Go API，接受 Transport 连接
- **Transport**：传输层抽象（Unix socket / WebSocket / 内存）
- **Fan-out**：一个 Terminal 的输出可以同时发送给多个订阅者
- **Snapshot**：客户端重连时恢复屏幕状态（scrollback + 屏幕 + 光标 + 模式）
- **Tags**：Terminal 上的 key-value 元数据，服务端只存储不解释，客户端用于分组/过滤

## 核心接口速览

```go
// Server — 入口
srv := termx.NewServer(termx.WithSocketPath("/tmp/termx.sock"))
srv.ListenAndServe(ctx)

// Terminal CRUD
term, _ := srv.Create(ctx, termx.CreateOptions{Command: []string{"bash"}})
terms := srv.List(ctx, termx.ListOptions{Tags: map[string]string{"group": "dev"}})
srv.Kill(ctx, termID)

// Terminal I/O
srv.WriteInput(ctx, termID, []byte("ls\n"))
sub := srv.Subscribe(ctx, termID) // 返回 <-chan []byte
snap := srv.Snapshot(ctx, termID)

// Resize
srv.Resize(ctx, termID, 120, 40)

// Tags
srv.SetTags(ctx, termID, map[string]string{"group": "dev"})

// Events
events := srv.Events(ctx) // 返回 <-chan Event
```

注意：

- `Get()` / `List()` 返回的 `TerminalInfo` 及其中的 `Command` / `Tags` 采用只读快照语义
- 如果客户端需要修改这些字段，应先自行拷贝再修改
- 这样服务端可以复用 metadata 快照，显著降低高频读取路径的分配成本

## 协作方式

- 默认连续执行整批后续计划步骤，直到任务全部完成后再统一向人类同步结果
- 不要在每一轮结束后停下来等待确认
- 只有遇到真实 blocker、需求冲突、危险/不可逆操作，或用户明确要求暂停时，才需要确认
- 需要提交代码时，提交信息保持中文

## 构建和测试

```bash
# 构建
PATH="$PWD/.toolchain/go/bin:$PATH" go build ./cmd/termx

# 测试
PATH="$PWD/.toolchain/go/bin:$PATH" go test ./...

# 运行 daemon
./termx daemon

# 创建终端
./termx new -- bash
./termx new --tag group=dev -- zsh

# 列出终端
./termx ls
./termx ls --tag group=dev

# 附加到终端
./termx attach <terminal-id>

# TUI 界面
./termx
```

## 当前实现进度

- 已完成服务端 MVP：PTY、VTerm、快照、事件、协议、Unix socket、CLI
- 已有基础 TUI：tab/pane、attach/create、快捷键、自动拉起 daemon
- TUI 当前使用 cell-based compositor 渲染 pane，优先保证 `htop`/`vim`/`less` 等全屏程序在 resize 时的正确性

## Spec 文档

详细设计文档在 `docs/` 目录：

| 文档 | 内容 |
|------|------|
| [spec-overview.md](docs/spec-overview.md) | 项目总览和架构 |
| [spec-terminal.md](docs/spec-terminal.md) | Terminal 模型（核心） |
| [spec-pty-manager.md](docs/spec-pty-manager.md) | PTY 管理 |
| [spec-vterm.md](docs/spec-vterm.md) | 虚拟终端 |
| [spec-snapshot.md](docs/spec-snapshot.md) | 快照和重连恢复 |
| [spec-events.md](docs/spec-events.md) | 事件系统 |
| [spec-transport.md](docs/spec-transport.md) | 传输层 |
| [spec-protocol.md](docs/spec-protocol.md) | 线协议 |
| [spec-tui-client.md](docs/spec-tui-client.md) | TUI 客户端 |
| [spec-go-api.md](docs/spec-go-api.md) | Go 公开 API |
