# termx TUI 产品定义设计

状态：Draft
日期：2026-03-25

## 1. 目标

为 termx 重新定义 TUI 的产品形态，先明确产品边界、用户可见对象、一级信息架构和关键交互，再进入后续实现计划。

这份文档只定义产品，不讨论具体渲染架构、状态机拆分和代码模块实现。

## 2. 产品定位

termx 对外的产品定义是：

> `termx` 是一个更现代的终端复用器。

它在使用体验上应尽量接近 tmux / zellij，用户进入后应该马上能工作，而不是先面对一个资源管理后台。

但它的底层机制不同：

- termx 的本体是一个全局 `terminal pool`
- TUI 只是 terminal pool 的第一方界面
- `pane` 不是 terminal 本体，只是 terminal 的一个工作位/观察位
- 一个 terminal 可以被多个 pane 连接
- 后续 GUI / Mobile 也应能复用同一个 terminal pool

因此，termx TUI 的正确定位不是“纯 tmux 复刻”，也不是“只有管理功能的 terminal 控制台”，而是：

- 对用户来说：一个更现代的终端复用器
- 对机制来说：一个建立在 terminal pool 之上的第一方工作台

## 3. 设计原则

### 3.1 主体验优先接近 tmux / zellij

- 默认进入可工作的 workbench
- 主体工作流围绕 `workspace / tab / pane / floating pane`
- 不在默认主界面长期暴露重量级管理面板

### 3.2 pane 与 terminal 必须解耦

- 用户操作的是 pane
- terminal 是持续运行、可复用的实体
- close pane 默认不等于 kill terminal
- pane 可以断开、换绑、复用已有 terminal

### 3.3 terminal pool 不能被隐藏

- 日常工作流不应被 terminal pool 管理页打扰
- 但复用已有 terminal 必须是自然路径，而不是隐藏能力
- 既要有轻量 picker，也要有完整 terminal pool 管理页

### 3.4 workspace 是组织视图，不是 session 所有权模型

- workspace 是全局工作现场
- 它不天然绑定项目目录
- 它不拥有 terminal 生命周期
- 项目目录可以声明快速启动入口，但不改变 workspace 的全局属性

## 4. 核心对象

### 4.1 terminal

- server 托管的真实运行实体
- 有自己的名称、命令、状态、标签、连接关系
- 可以被多个 pane 复用
- 是未来多端共享的核心对象

### 4.2 pane

- 一个可见工作位
- 默认不作为独立命名对象存在
- 左侧标题默认显示当前绑定 terminal 的名称
- 若未连接 terminal，则显示空态名称
- pane 的生命周期独立于 terminal

### 4.3 floating pane

- 是 pane 的另一种摆放方式，不是弱化版观察窗
- 可进行正常 terminal 交互、连接、换绑、关闭
- 但 floating pane 内部不再继续分裂

### 4.4 tab

- 第一阶段等同传统终端复用器中的 window
- 主要职责是组织 pane
- 先不赋予过重的策略语义
- 后续可以扩展 tab 级偏好与行为

### 4.5 workspace

- 最顶层工作现场
- 感知应偏轻，用户日常主要活动仍在 tab / pane
- 默认保存完整工作现场，而不仅是布局骨架
- workspace 不是 session，不拥有 terminal 本体

## 5. 一级信息架构

第一阶段只保留三个层级：

### 5.1 Workbench

默认入口，也是日常工作的主界面。

职责：

- 分屏与 tab 工作流
- floating pane 工作流
- terminal 连接、断开、换绑
- workspace 的日常使用

### 5.2 Terminal Pool

一个独立主页面，用来完整查看和管理 terminal 本体。

它不是 picker 放大版，而是资源管理页。

### 5.3 Overlay 层

包括：

- picker
- dialog
- prompt
- help

职责是承接高频局部动作，而不是替代主页面。

## 6. Workbench 定义

### 6.1 顶栏

采用中等信息密度，默认包含：

- 当前 workspace
- tab strip
- 少量工作台摘要

第一阶段先固定内容，不做高度可配置；后续可通过配置文件开放更高自由度。

### 6.2 pane 标题栏

标题栏规则如下：

- pane 不需要独立名字
- 左侧优先显示 terminal 名称
- 未连接 terminal 时显示空态名称，例如 `unconnected`
- 右侧展示连接与显示状态

右侧状态可包括：

- owner / follower
- readonly
- fit / fixed
- share 数量
- pin 等局部标记

### 6.3 底栏

- 左侧展示当前 mode 的快捷键提示
- 右侧展示当前焦点对象的短摘要

第一阶段固定，后续允许通过配置文件自定义样式与内容分配。

### 6.4 unconnected pane

未连接 terminal 的 pane 不应显示成死空白。

它应表现为可操作空态，至少提供：

- 连接已有 terminal
- 创建新 terminal
- 打开 terminal pool

### 6.5 floating pane

- 与 tiled pane 属于同级展示形态
- 能力上是完整 pane
- 仅在布局能力上受限，不支持内部继续 split

## 7. Terminal Pool 页面定义

这是一个独立主页面，而不是 overlay。

### 7.1 页面结构

采用三栏式结构：

- 左栏：terminal 列表
- 中栏：当前选中 terminal 的实时内容
- 右栏：terminal 信息与连接关系

### 7.2 左栏分组

第一阶段采用轻分组：

- `visible`
- `parked`
- `exited`

定义：

- `visible`：当前至少被一个 pane 显示的 terminal
- `parked`：terminal 仍活着，但当前没有 pane 在显示
- `exited`：terminal 已退出，但记录仍保留

### 7.3 中栏

默认展示实时 attach 视图，而不是静态 snapshot。

这意味着 Terminal Pool 不只是管理页，也是观察页。

第一阶段默认以只读观察为主，不直接把它当作日常输入位。

如果用户需要接管输入，应通过显式动作切换，而不是在管理页里隐式抢占输入焦点。

### 7.4 右栏

信息展示优先级：

1. terminal 元数据
2. terminal 连接关系

即先回答“它是什么”，再回答“谁在看它、怎么被连接”。

### 7.5 操作重心

Terminal Pool 页的主操作重心偏向 terminal 本体管理，例如：

- 重命名
- 标签/元数据编辑
- stop / kill
- 查看连接关系

从这里“打开到当前 pane / 新 tab / floating”仍然需要提供，但不是主叙事。

## 8. 统一创建与连接流程

第一阶段，`split / new tab / new float` 统一遵循同一模式：

1. 先创建目标 pane slot
2. 立即弹出轻量 dialog
3. 用户选择：
   - 创建新 terminal
   - 连接已有 terminal
4. 如果取消，则保留为 `unconnected pane`

这样可以保证：

- 用户操作对象始终是 pane
- terminal 作为后续连接实体出现
- 所有创建动作共享同一套交互模型

## 9. 连接已有 terminal 的规则

### 9.1 默认范围

- 默认展示全部 terminal
- 不按 workspace / tab 隐式裁剪范围

原因：

- terminal pool 是全局池
- 如果默认只看局部范围，会削弱产品的核心差异点

### 9.2 默认排序

默认排序按“最近用户交互”而不是“最近纯输出”。

最近用户交互可包括：

- 最近输入
- 最近 attach / connect
- 最近显式打开

不把持续高频输出作为主排序依据，避免 `htop`、日志流、watch 类 terminal 长期霸榜。

终端是否持续输出可以作为辅助状态展示，但不主导排序。

## 10. pane 关闭与 terminal 生命周期

产品必须显式区分 pane 操作和 terminal 操作。

第一阶段明确支持三类动作：

- 关闭 pane
- 断开 pane 与当前 terminal 的连接
- 将 pane 重新连接到其他 terminal
- 关闭 pane 并 kill terminal

默认行为定义为：

- `close pane` 默认只关闭 pane，不 kill terminal

这是 termx 与传统复用器最重要的差异点之一，必须在界面与交互中持续体现。

## 11. workspace 与项目启动

- workspace 本身是全局对象
- 不和项目目录天然绑定
- 项目目录中可以声明一种快速启动方式
- 快速启动可以恢复已有 workspace，或按模板创建 workspace
- 但这不改变 workspace 的全局本质

第一阶段的实现计划不应把“项目目录快速启动”扩展成独立大子系统。

它可以保留为后续能力入口，但不应阻塞 workbench、terminal pool 和核心连接复用闭环。

## 12. 第一阶段不做的东西

为了避免重新落回“什么都想做，结果主形态迟迟不稳定”的问题，以下内容明确不在第一阶段主产品定义里：

- 独立 Settings 页面
- 高度可配置的顶栏/底栏 UI 编排
- 复杂 IDE 式管理后台
- 把 tab/workspace 一开始就做成强策略对象

这些能力后续可以通过配置文件或产品迭代逐步补上。

## 13. 产品定义结论

第一阶段的 termx TUI 应被定义为：

> 一个更现代的终端复用器。

它的主界面应像 tmux / zellij 一样用于日常工作；它的底层则建立在一个可复用的 terminal pool 之上，使 pane、workspace、未来 GUI / Mobile 界面都可以复用同一批 terminal。

因此，termx TUI 不是“terminal pool 的壳”，也不是“传统终端复用器的换皮版”，而是：

- 一个以 workbench 为主、以 terminal pool 为核心机制的终端复用器
- 一个把 pane 与 terminal 解耦的工作台
- 一个为未来多端复用预留了正确产品边界的第一方界面
