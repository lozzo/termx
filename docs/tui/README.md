# TUI 客户端设计文档

termx TUI 是 termx 终端运行时的内置客户端。它通过 Unix socket 连接本地 termx daemon，提供分屏布局、多 tab、workspace 管理和浮动面板等功能。

本目录包含 TUI 客户端的完整设计文档，按主题拆分为独立文件。

## 文档索引

| 文档 | 内容 |
|------|------|
| [positioning.md](positioning.md) | 产品定位：termx 是什么、不是什么，与 tmux/zellij 的本质区别 |
| [model.md](model.md) | 核心模型：Terminal Pool、Viewport、fit/fixed 模式、多客户端仲裁 |
| [interaction.md](interaction.md) | 交互设计：模式、快捷键、terminal picker、关闭/detach 语义 |
| [layout.md](layout.md) | 声明式布局：YAML 配置、tag 匹配、workspace 管理 |
| [rendering.md](rendering.md) | 渲染架构：batching、dirty tracking、背压、viewport 裁剪 |
| [ai-scenarios.md](ai-scenarios.md) | AI 场景：Agent 宿主、人机协作、编排、Terminal 作为 I/O 通道 |
| [wireframes.md](wireframes.md) | TUI 线稿集：16 个 Frame 覆盖所有界面状态和组件拆解 |
| [verification.md](verification.md) | 待验证事项：设计中的假设和未验证方案，按优先级排序 |
| [roadmap.md](roadmap.md) | 路线图：技术验证（Spike）顺序 + 功能实现（Build）里程碑 |

## 阅读顺序

建议按以下顺序阅读：

1. **positioning** — 先理解 termx 的产品定位和核心差异
2. **model** — 理解 Viewport 统一模型，这是所有设计的基础
3. **interaction** — TUI 的交互逻辑和快捷键
4. **layout** — 声明式布局系统
5. **rendering** — 渲染管线的技术细节
6. **ai-scenarios** — AI 时代的扩展场景
