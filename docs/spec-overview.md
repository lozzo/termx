# termx — 项目总览

## 设计哲学

termx 的核心设计思想可以用一句话概括：

> **服务端是一个扁平的 Terminal 池，没有任何组织结构。**

传统终端复用器（tmux、zellij、screen）都在服务端强制了层级结构：

```
tmux:     server → session → window → pane
zellij:   server → session → tab    → pane
screen:   server → session → window
termx:    server → terminal (扁平)
```

termx 选择了一条不同的路：服务端只管 Terminal 的生命周期（创建、销毁、I/O、resize），所有组织概念（workspace、标签页、分组、布局）都由客户端自行实现。

这就像 S3 vs 文件系统的关系：S3 只有 object（扁平），但客户端可以用 key 前缀模拟任意目录结构。termx 只有 Terminal，但客户端可以自由定义任意组织方式。

## 为什么这样设计

1. **客户端自由度最大化**：TUI 客户端可以实现 workspace 概念，移动 App 可以用标签页，Web 客户端可以用浮动窗口 —— 同一个服务端，不同的组织方式
2. **引用语义**：同一个 Terminal 可以同时出现在多个"分组"中，因为组织结构是客户端的视图，不是服务端的所有权
3. **代码极简**：服务端不需要管理树结构、不需要处理节点增删时的布局调整、不需要协调多个客户端对树的修改冲突
4. **Bug 表面积最小**：没有层级意味着没有 "删除 window 时 pane 怎么办"、"移动 pane 到另一个 session" 这类复杂问题

## 架构

```
┌─────────────────────────────────────────────┐
│                  termx Server               │
│                                             │
│  ┌──────────────────────────────────────┐   │
│  │          Terminal Pool (flat)         │   │
│  │                                      │   │
│  │  ┌─────────┐ ┌─────────┐ ┌────────┐ │   │
│  │  │Terminal 1│ │Terminal 2│ │Terminal│ │   │
│  │  │ PTY     │ │ PTY     │ │ PTY    │ │   │
│  │  │ VTerm   │ │ VTerm   │ │ VTerm  │ │   │
│  │  │ State   │ │ State   │ │ State  │ │   │
│  │  └────┬────┘ └────┬────┘ └───┬────┘ │   │
│  │       │            │          │      │   │
│  └───────┼────────────┼──────────┼──────┘   │
│          │            │          │           │
│  ┌───────┴────────────┴──────────┴──────┐   │
│  │              Fan-out                  │   │
│  └───────┬────────────┬──────────┬──────┘   │
│          │            │          │           │
│  ┌───────┴────────────┴──────────┴──────┐   │
│  │           Transport Layer             │   │
│  │  Unix Socket │ WebSocket │ Memory     │   │
│  └───────┬────────────┬──────────┬──────┘   │
└──────────┼────────────┼──────────┼──────────┘
           │            │          │
     ┌─────┴─────┐ ┌────┴────┐ ┌──┴───┐
     │TUI Client │ │Web/App  │ │Go API│
     │(bubbletea)│ │Client   │ │(嵌入) │
     │           │ │         │ │      │
     │ workspace │ │ tabs    │ │ any  │
     │ keybinds  │ │ float   │ │      │
     └───────────┘ └─────────┘ └──────┘
```

## Terminal

Terminal 是 termx 的**唯一服务端实体**，包含：

- **PTY 进程**：实际运行的 shell 或命令
- **虚拟终端（VTerm）**：解析 ANSI 转义序列，维护屏幕缓冲区
- **稳定 terminal ID**：客户端只需持有 terminal ID，就能在自己的布局或业务存储中引用该 Terminal

详见 [spec-terminal.md](spec-terminal.md)。

## 客户端类型

### TUI 客户端（内置）

类似 tmux 的终端界面，但组织方式完全在客户端：

- 列表视图：显示所有 Terminal，支持按名称、命令或本地布局分组
- 全屏 attach：raw passthrough，直连 PTY I/O
- 快捷键驱动

详见 [spec-tui-client.md](spec-tui-client.md)。

### Go API 客户端（嵌入）

作为 Go 库直接嵌入到其他程序中（如 tgent-go）：

```go
srv := termx.NewServer()
term, _ := srv.Create(ctx, termx.CreateOptions{Command: []string{"bash"}})
// 直接调用 Go API，无需 Transport
```

详见 [spec-go-api.md](spec-go-api.md)。

### 远程客户端（tgent-app、Web）

通过 Transport（WebSocket / Unix socket）连接，使用线协议通信。组织方式完全由客户端 UI 定义：

- tgent-app 可以用标签页 + 分屏
- Web 客户端可以用浮动窗口
- 都通过 Terminal ID 引用同一组 Terminal

详见 [spec-transport.md](spec-transport.md) 和 [spec-protocol.md](spec-protocol.md)。

## 数据流

### 终端输出（PTY → 客户端）

```
PTY stdout → Fan-out → [Subscriber 1, Subscriber 2, ...]
                ↓
              VTerm（异步解析，维护屏幕缓冲区，仅服务 Snapshot）
```

- PTY 原始字节先经过 Fan-out 分发给所有订阅者，再异步送入 VTerm 解析
- VTerm 不在数据分发的关键路径上，仅为 Snapshot 维护屏幕状态
- Fan-out 处理背压：慢消费者被丢弃（不阻塞其他订阅者）

### 控制面 vs 数据面

termx 在协议和架构上明确区分两条路径：

- **控制面（control plane）**：create/list/get/kill/attach/snapshot/events 等管理操作，频率低，使用 JSON
- **数据面（data plane）**：Terminal 输入输出字节流，频率高，使用二进制帧透传 PTY 原始字节

这一区分是刻意设计，而不是过渡实现。性能评审后的结论是：

- JSON 主要存在于低频控制面，不应成为终端渲染卡顿的首要怀疑对象
- 高负载场景（`htop`、`vim`、大量日志、频繁 resize）的主要风险，通常在**客户端 VT 解析、本地屏幕模型、分屏合成和最终渲染输出**
- 因此 termx 的优化重点应放在 TUI 渲染链路，而不是把控制面 JSON 过早替换成二进制协议

### TUI 性能关注点

termx 的服务端保持扁平 Terminal 池，这一点不会妨碍高性能；真正影响交互手感的是客户端如何把多个 Terminal 组织成可视界面。

参考 tmux 和 zellij 的实现模式，TUI 性能的关键不在于“是否支持分屏/浮窗”，而在于是否具备以下能力：

- 有稳定的 screen/grid 模型，而不是每次都从头拼整屏字符串
- 以 dirty pane / dirty line / dirty rect 为单位增量更新
- 在 resize、全屏程序和高频输出下做 redraw batching
- 对慢终端或积压输出做背压控制，而不是无限追帧
- 尽量利用终端原生能力（scroll、insert/delete、synchronized updates），减少无意义重绘

termx 的后续 TUI 设计应遵循这些原则。

### 终端输入（客户端 → PTY）

```
Client → Transport → Server.WriteInput() → PTY stdin
```

- 输入直接写入 PTY，不经过 VTerm
- 多个客户端可以同时向同一个 Terminal 输入（最后写入的生效）

### 重连恢复

```
Client reconnect → Server.Snapshot() → VTerm 屏幕快照 → Client 恢复显示
```

详见 [spec-snapshot.md](spec-snapshot.md)。

## 相关文档

- [Terminal 模型](spec-terminal.md)
- [PTY 管理](spec-pty-manager.md)
- [虚拟终端](spec-vterm.md)
- [快照](spec-snapshot.md)
- [事件系统](spec-events.md)
- [传输层](spec-transport.md)
- [线协议](spec-protocol.md)
- [TUI 客户端](spec-tui-client.md)
- [Go API](spec-go-api.md)
- [tgent 集成](integration-tgent.md)
