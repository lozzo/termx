# termx TUI 产品定义

状态：Draft v3
日期：2026-03-25

## 产品目标

termx TUI 是一个 terminal-first 的工作台。

它的目标不是展示状态说明，而是让用户直接在里面工作：

- 启动后立刻可输入
- split 后两个 pane 都可工作
- floating 是真实浮层，不是摘要卡
- overlay 是辅助层，不是主界面主体

## 核心对象

### workspace

- 顶层工作现场
- 承载多个 tab

### tab

- 一个任务面的工作区
- 同时包含 tiled 和 floating pane

### pane

- terminal 的观察和操作窗口
- 只是视图和交互表面，不是运行主体

### terminal

- server 托管的运行实体
- 可以被多个 pane 同时连接

## 连接语义

- `connect`：pane 连接某个 terminal
- `disconnect`：pane 不再连接 terminal，但 pane 位置仍可保留
- `close pane`：关闭窗口，不等于停止 terminal
- `stop terminal`：终止运行实体，不等于删除 pane

## pane slot 状态

### connected pane

- 当前 pane 已连接 terminal

### empty pane

- 当前 pane 还没有连接 terminal

### exited pane

- terminal 中的程序退出了，但历史仍可见

### waiting pane

- 常见于 layout / restore，表示预留槽位

## 共享 terminal

共享 terminal 只保留两种关系：

### owner

- 可以做 terminal 控制面动作
- 例如 resize、acquire owner、metadata 操作

### follower

- 能看、能切焦点
- 但默认不拥有 terminal 控制面权限

任意已连接的 pane 或客户端，都可以请求获取 owner。

## 用户主路径

### 启动

- 进入默认 workspace
- 默认有一个 shell pane
- 用户可以立即输入

### split

- 用户拆分当前 pane
- 新 pane 可以创建 terminal，也可以连接已有 terminal

### floating

- 用户临时开一个浮窗做观察或辅助操作
- 浮窗可切焦点、移动、缩放、调 z-order、居中呼回

### overlay

- 用户打开 picker / manager / help / prompt
- overlay 盖在 workbench 上
- 关闭后回到原来的 pane

### restore / layout

- 用户可从保存的 workspace 或 layout 进入已有工作现场
- waiting pane / layout resolve 负责补齐未连接的槽位
