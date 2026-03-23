# termx TUI 产品规格

状态：Draft v1
日期：2026-03-23

---

## 1. 产品定义

termx TUI 是一个运行在本地终端里的多工作位终端工作台。

它一端连接 termx server 托管的 terminal 池，另一端提供类似 tmux / zellij 的界面组织能力。

一句话定义：

- `像终端工作台一样组织 workspace / tab / pane`
- `像运行时控制台一样管理、复用、恢复 terminal`

它不是：

- tmux 的兼容实现
- 只会 connect 单个 shell 的轻客户端
- 先做复杂 IDE 管理台的系统

---

## 2. 设计目标

### 2.1 核心目标

1. 用户执行 `termx` 后，立即进入可工作的界面
2. 用户先理解 `workspace / tab / pane / terminal` 四个概念即可开始工作
3. terminal 的复用、共享、恢复是自然能力，不是隐藏能力
4. 关闭 TUI 不会隐式结束 terminal
5. 在复杂共享场景下，terminal 的控制权、显示方式和生命周期都清楚可解释

### 2.2 非目标

1. 不追求 tmux 快捷键和 session 语义完全兼容
2. 不追求把 server/runtime 概念彻底藏起来
3. 当前阶段不先做批量运维控制台、规则编排器、复杂可视化面板
4. 不为追求炫技交互而牺牲稳定性和可解释性

---

## 3. 用户画像

### 3.1 开发者

- 日常开发、构建、测试、日志查看
- 需要快速开 pane / tab / floating
- 需要给 terminal 命名和打 tags

### 3.2 SRE / OPS

- 同时观察多个长期 terminal
- 希望快速 connect、切环境、快速恢复
- 对共享 terminal、浮窗观察和 metadata 检索更敏感

### 3.3 长会话用户

- 希望退出 TUI 后 terminal 继续运行
- 希望下次重新进入还能恢复现场
- 希望 layout / workspace file 可以描述“怎么进入工作状态”

---

## 4. 核心概念

### 4.1 workspace

- 最外层工作现场
- 对用户而言类似一个项目、任务域或环境域
- 包含多个 tab
- 有自己的名称、恢复状态、布局入口

### 4.2 tab

- workspace 内的页面单位
- 用来分离不同任务面，例如 `dev / logs / ops / build`
- 一个 tab 内同时包含 tiled pane 和 floating pane

### 4.3 pane

- 界面上的一个可见工作位
- 可以是 tiled pane，也可以是 floating pane
- 自身不等于 terminal
- 默认不是独立命名对象
- 当 pane 连接 terminal 时，标题默认显示 terminal 真实名称

### 4.4 terminal

- 由 termx server 托管的运行实体
- 有自己的 `id / name / tags / command / state`
- 可以被多个 pane 同时 connect
- 生命周期独立于单个 pane

### 4.5 floating pane

- tab 内位于浮层的 pane
- 用于临时观察、辅助操作、调试、巡检
- 可以与 tiled pane 并存
- 可移动、可缩放、可调 z-order

### 4.6 共享 terminal 的控制权关系

termx 对外只强调一组共享关系：

- `owner`
- `follower`

定义：

- `owner` 代表 terminal 的控制权持有者
- `follower` 代表当前只是在观察或附着该 terminal 的其他 pane / 客户端

`owner` 负责的是 terminal 的控制面权限，而不是“谁在运行程序”：

- resize terminal
- 更新 terminal metadata
- 执行 terminal 级管理动作

例如：

- `resize`
- `set name`
- `set tags`
- 其他 terminal control-plane 操作

强约束：

- 一个 terminal 任一时刻最多一个 owner
- 任意一个 pane 或客户端都可以主动获取 owner
- owner 迁移必须显式、稳定、可测试
- pane 只是观察窗口，不应该和 terminal 主体混淆

---

## 5. 产品原则

### 5.1 启动即工作

- `termx` 启动后直接进入 workspace
- 默认给一个可输入 shell pane
- 不先落到“说明页”或“空壳页”

### 5.2 界面层和运行层分离

- pane 是界面层
- terminal 是运行层
- 界面可以关闭、重排、恢复
- terminal 可以持续运行、复用、共享

### 5.3 用户先用，再逐步理解

- 用户不必先理解所有 mode 才能工作
- 正常工作路径应该在 normal 状态下可达
- picker / help / manager 是增强能力，不是唯一入口

### 5.4 一切复杂行为都要能解释

termx 里所有复杂行为，最终都应该能用用户语言解释：

- 为什么 close pane 之后 terminal 还在
- 为什么当前不是 owner 时不能直接改 terminal 控制参数
- 为什么 stop terminal 后 pane 还在，但已经没有连接 terminal

如果一句话解释不清，设计就是有问题。

---

## 6. 生命周期定义

### 6.1 close pane

- 关闭的是 pane 这个展示入口
- 默认不结束 terminal
- 其他已连接 pane 不受影响

### 6.2 stop terminal

- stop 的是 terminal 实体
- 所有已连接 pane 都失去运行实体
- pane 保留在原位置，但变成“未连接 terminal 的 pane”
- stop 前必须确认

### 6.3 terminal exited

- pane 变成“terminal 中程序已经退出的 pane”
- 历史仍然可读
- 任一已连接 pane 可触发 restart

### 6.4 detach / quit TUI

- 结束当前 TUI 客户端
- 不结束 server 托管 terminal
- 重新进入后可以继续 connect / restore

---

## 7. 共享 terminal 规则

### 7.1 基本规则

- 一个 pane 同时最多连接一个 terminal
- 一个 terminal 可以被多个 pane 连接
- terminal 的 PTY size 只有一份
- pane 的几何只决定观察窗口，不定义 terminal 主体

### 7.2 owner / follower

- owner 持有 terminal 控制权
- follower 不持有 terminal 控制权
- 一个 terminal 任一时刻最多一个 owner
- owner 可执行 terminal 控制面动作
  - resize
  - set metadata
  - 其他 terminal-level control 操作
- 任意 pane 或客户端都可以请求获取 owner
- owner 消失时，系统要稳定迁移 owner

### 7.3 auto-acquire

- tab 可以配置 auto-acquire
- 进入 tab 时可自动争取 resize 控制权
- 该行为必须可关闭、可测试、可预测

---

## 8. 主界面职责分配

### 8.1 顶栏

- 当前 workspace
- tab strip
- workspace 级摘要
- 全局 notice / error / sync 状态

### 8.2 pane 标题栏

- 左侧优先显示 terminal 名称
- 当 pane 没有连接 terminal 时，显示槽位提示或引导动作
- 右侧只显示最关键的 terminal 关系信息
  - `owner / follower`

### 8.3 底栏

- 左侧放当前模式的最少必要快捷键
- 右侧放当前焦点对象的极简摘要
- 不把所有状态都堆到底栏

### 8.4 overlay

- help
- picker
- prompt
- terminal manager
- workspace picker

要求：

- 居中
- 有稳定遮罩和边框
- 关闭后不留残影

### 8.5 workspace picker

- 以树形结构展示 workspace 内部层级
- 至少可展开到 `workspace -> tab -> pane`
- 支持直接跳到某一个 pane
- 支持搜索 workspace 名称，也支持按路径定位目标 pane

---

## 9. 成功标准

termx TUI 第一阶段成功，不看实现细节，只看是否满足下面几点：

1. 用户启动后立即可工作
2. 用户能自然理解 `workspace / tab / pane / terminal`
3. close pane、stop terminal、detach TUI 三种行为不混淆
4. shared terminal 下 resize 规则稳定可解释
5. picker / manager / restore 是顺手入口，而不是补丁入口
6. 复杂交互发生时，界面不闪烁、不串屏、不留残影
