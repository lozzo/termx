# TUI 客户端

termx 内置一个全功能 TUI 客户端，基于 [bubbletea](https://github.com/charmbracelet/bubbletea) 框架。它通过 Unix socket 连接本地 termx daemon，提供分屏布局、多 tab、workspace 管理和浮动面板等功能。

与 tmux/zellij 的关键差异：termx 是终端运行时（Terminal Runtime），不是终端复用器。Terminal 独立存活，客户端只是视图。布局完全在客户端侧实现，服务端只有扁平的 Terminal 池。

## 设计文档

TUI 客户端的完整设计已拆分为独立文档，位于 [docs/tui/](tui/) 目录下。

| 文档 | 内容 |
|------|------|
| [tui/positioning.md](tui/positioning.md) | 产品定位：termx 是什么、不是什么，与 tmux/zellij 的本质区别，核心用户价值 |
| [tui/model.md](tui/model.md) | 核心模型：Terminal Pool、Viewport（fit/fixed 模式）、平铺+浮动统一布局、多客户端 resize 仲裁、外层窗口 resize 行为 |
| [tui/interaction.md](tui/interaction.md) | 交互设计：模式、快捷键、Terminal Picker、关闭 vs kill 语义、程序退出后 Viewport 保留与恢复 |
| [tui/layout.md](tui/layout.md) | 声明式布局：YAML 配置格式、tag 匹配机制、Workspace 管理、布局保存与恢复 |
| [tui/rendering.md](tui/rendering.md) | 渲染架构：batching、dirty tracking（Viewport 级 + 行级）、背压退化、viewport 裁剪、浮窗 z-order |
| [tui/ai-scenarios.md](tui/ai-scenarios.md) | AI 场景：Agent 宿主、人机协作、Agent 编排、Terminal 作为 I/O 通道 |

建议按以上顺序阅读：先理解产品定位，再理解核心模型，然后是交互和布局，最后是渲染细节和 AI 场景。

## 相关文档

- [Terminal 模型](spec-terminal.md) — TUI 显示的数据
- [传输层](spec-transport.md) — TUI 通过 Unix socket 连接
- [线协议](spec-protocol.md) — TUI 使用的消息格式
- [快照](spec-snapshot.md) — Scrollback/Copy 模式的数据来源
- [事件系统](spec-events.md) — TUI 订阅的 server 事件
