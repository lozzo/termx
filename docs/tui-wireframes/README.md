# TUI 线框图场景包

这个目录用于沉淀 termx TUI 的线框图、关键场景和状态流转。

目标：

- 用纯 TUI 线框图表达产品形态
- 把复杂状态拆成可单独讨论的场景文件
- 把主路径、异常路径、共享 terminal、floating、overlay 等场景拆开讨论
- 作为后续实现、测试和 UI 讨论的稳定参考

约定：

- 每个场景单独一个文件
- 每个场景只回答一个主问题，不混多个主题
- 线框图优先使用纯终端字符表达，不追求视觉花哨
- 先把状态、信息层级、交互入口表达清楚，再讨论渲染细节

推荐结构：

- `00-index.md`
  场景索引与阅读顺序
- `01-*.md ~ 99-*.md`
  单场景线框图
- `flows/`
  跨场景流转与状态变化

单个场景文件的固定结构：

- `目标`
- `状态前提`
- `线框图`
- `关键规则`
- `流转`

当前状态：

- 目录骨架已建立
- 主场景、状态场景、共享 terminal、floating、overlay、flow 已补入首版 ASCII 线框图
- 第二轮细化场景已补入，包括多 pane、tab 切换、connect 多来源、Terminal Pool `kill/remove` 结果态、floating 锚点极限、宽字符裁切边界
- 当前进入“继续扩面”阶段，重点是补 workspace 切换、overlay 叠层和 alt-screen 对照图
- 如需在上下文压缩后快速恢复，请优先阅读：
  - [00-index.md](/home/lozzow/workdir/termx/docs/tui-wireframes/00-index.md)
  - [99-session-handoff.md](/home/lozzow/workdir/termx/docs/tui-wireframes/99-session-handoff.md)
