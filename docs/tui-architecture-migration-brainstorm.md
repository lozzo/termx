# termx TUI 架构迁移脑暴记录

状态：Discussion Draft
日期：2026-03-27

> 本文不是实现计划，也不是重写方案。
> 它用于沉淀当前关于 TUI 目标架构与迁移主线的脑暴共识，作为后续继续讨论与收敛的基线。

---

## 1. 背景判断

当前 TUI 的核心问题不是某几个函数太长，而是：

- `tui/model.go` 同时承担了过多角色：Bubble Tea shell、应用根、主工作台根、runtime 协调点、局部领域逻辑承载点
- `pane`、terminal runtime、渲染调度、布局和交互边界没有被清晰建模
- 继续按“零散字段迁移”推进，收益会快速递减

因此，后续路线应从“字段迁移主导”切换为：

**以对象树为主轴做架构迁移。**

这个迁移必须满足：

- 只能迁移，不能完全重写
- 每一轮迁移后都保持主线可运行
- 每一轮迁移都让职责边界更清楚，而不是只做局部修补

---

## 2. 当前已确认的架构前提

### 2.1 TUI 的一级结构不是直接 `App -> Workspace`

结合产品定义，TUI 顶层应先区分为：

- `Workbench`
- `TerminalPoolPage`
- `Overlay`

因此顶层对象更合理的方向是：

```text
App
  ├── Workbench
  ├── TerminalPoolPage
  └── Overlay
```

其中：

- `Workbench` 是日常工作主界面
- `TerminalPoolPage` 是独立主页面，不是 overlay
- `Overlay` 包含 picker / prompt / help / dialog 等

### 2.2 Workbench 内部应采用对象树

主工作流对象树为：

```text
Workbench
  └── Workspace[*]
      └── Tab[*]
          ├── Pane[*] ---> *Terminal
          ├── LayoutTree
          └── Floating geometry / z-order
```

含义：

- `Workspace` 管理 tabs
- `Tab` 管理 panes 与布局
- `Pane` 是工作位 / 观察位
- `Pane` 直接引用 `*Terminal`

### 2.3 pane 与 terminal 解耦

已确认：

- `Pane != Terminal`
- `Pane` 直接引用 `*Terminal`
- 不引入 `PaneSession` 作为 pane 与 terminal 之间的中间层

原因：

- `Pane` 和 `Terminal` 的引用关系本身是自然的
- 如果 pane 确实需要很多属性，应优先通过组合来解耦，而不是引入语义不自然的中间对象

### 2.4 terminal pool 在 daemon 生命周期内，不在 TUI 生命周期内

已确认：

- 真实 terminal pool 由后台守护进程持有
- TUI 通过 socket 与 daemon 通信
- TUI 退出后，terminal 仍可继续运行

因此：

- `Workspace` 不拥有 terminal 生命周期
- `Tab` 不拥有 terminal 生命周期
- `Pane` 只引用 terminal，不拥有 terminal 本体

### 2.5 TUI 侧仍需要本地 `Terminal` 代理对象

已确认：

- `Pane` 不应只保存 `TerminalID`
- TUI 内部需要本地 `*Terminal` 对象供 `Pane`、`TerminalPoolPage`、渲染和同步逻辑共同引用

这个 `Terminal` 的语义是：

- daemon terminal 的本地代理 / 镜像
- 不是 terminal pool owner

### 2.6 `Terminal` 应是较重的代理对象

已确认：

TUI 侧 `Terminal` 应持有较完整的运行时镜像，例如：

- terminal identity / metadata / state
- stream / snapshot / vterm
- attach / channel 相关信息
- shared terminal 相关上下文

### 2.7 `Pane` 只保留 pane 自己的观察状态

已确认：

`Pane` 应尽量只保留：

- pane identity
- 当前绑定的 `*Terminal`
- viewport offset
- viewport move mode
- pane 级显示状态

而不应继续承担大块 terminal runtime 状态。

### 2.8 `Pane` 不保存布局几何真值

已确认：

- pane 的 rect 不属于 `Pane`
- pane 的几何真值应由 `Tab` 持有和计算
- 这同时适用于 tiled pane 和 floating pane

也就是说，`Tab` 是 pane 布局与几何关系的拥有者。

### 2.9 viewport offset 属于 `Pane`

已确认：

由于同一个 terminal 可以被多个 pane 观察，而不同 pane 的观察位置可能不同，因此：

- viewport offset / move mode 属于 `Pane`
- 不属于 `Terminal`

### 2.10 owner / follower / connection context 属于 `Terminal`

已确认：

- 连接 terminal 的上下文信息应由 `Terminal` 自己维护
- 不只是记录 pane ID，而是记录连接它的客户端与对应对象 ID
- 这些客户端未来可能来自：
  - TUI
  - Web
  - Desktop
  - Mobile

因此 `Terminal` 上应存在某种连接上下文，例如：

- 谁连接了它
- 各自对应的对象 ID
- owner / follower 等连接角色信息

### 2.11 `TerminalStore` 统一持有 `*Terminal`

已确认：

TUI 内部应有：

```text
TerminalStore
  └── *Terminal[*]
```

作用：

- 统一持有 terminal 代理对象
- 供 `Pane` 和 `TerminalPoolPage` 共享引用
- 避免多处维护多份 terminal 本地状态

### 2.12 `TerminalStore` 不直接负责 socket 同步

已确认：

- `TerminalStore` 是 registry / cache
- 不直接承担与 daemon 通信的职责

和 daemon 通信、同步状态的职责，应落到单独的横向对象中。

### 2.13 `Pane` 绑定/解绑 terminal 时应显式维护 connection context

已确认：

当 `Pane` 绑定或解绑 terminal 时：

- 不应只改自己的指针
- 还应显式更新 `Terminal` 上的 connection context

这样 connection 关系是对象层的显式事实，而不是靠全局扫描推导出来。

### 2.14 `TerminalPoolPage` 直接读取 `TerminalStore`

已确认：

- `TerminalPoolPage` 不应维护另一套 terminal 数据副本
- 页面只保留：筛选 / 排序 / 选中 / 局部 UI 状态
- terminal 数据本身直接来自 `TerminalStore`

---

## 3. 横向对象的定位

除纵向对象树外，还需要一组横向协作对象。

当前已确认的方向是：

```text
App
  ├── InputRouter
  ├── TerminalCoordinator
  ├── Resizer
  ├── Renderer
  └── RenderLoop
```

### 3.1 `InputRouter`

已确认：

- `InputRouter` 只做输入翻译
- 把输入转换成对象方法调用
- 不直接处理复杂 runtime 细节
- 不直接承载复杂布局逻辑

### 3.2 `TerminalCoordinator`

已确认：

先不拆成 `Syncer + Coordinator` 两个对象，而是先合并成一个横向对象。

它统一负责两类事：

1. 与 `tui.Client` / daemon 的通信与同步
2. pane / terminal runtime 协调

例如：

- list / events / attach / snapshot / stream
- bind / unbind terminal
- terminal removed / exited / killed 联动
- runtime 状态同步进 `TerminalStore`

### 3.3 `Resizer`

已确认：

`Resizer` 的职责是：

- 把布局变化同步成 terminal PTY resize
- 处理与 owner/follower 相关的 terminal resize 行为

### 3.4 `Renderer`

已确认：

`Renderer` 应遵循：

- 默认尽量纯读取对象树和 terminal 镜像来产出 frame
- 可以做极少量 bookkeeping
- 不能成为状态修复器或 runtime 裁决器

可接受的 bookkeeping 仅限非常局部的渲染记账和缓存维护。

### 3.5 `RenderLoop`

已确认：

`RenderLoop` 负责：

- tick 驱动渲染
- batching
- flush
- backpressure

---

## 4. 高层协调入口

### 4.1 `App` 应成为唯一高层协调入口

已确认：

- `Model` 不应与 `App` 各处理一半高层逻辑
- `App` 应成为唯一高层协调入口

理想关系应为：

```text
Bubble Tea Msg
  -> Model
  -> App
  -> 其他对象协作
```

### 4.2 `Model` 的最终角色

已确认：

`Model` 的最终角色应退化为：

- Bubble Tea shell
- 接收消息
- 转调 `App`
- 产出 `View`

不再承载：

- 主工作台根对象职责
- 复杂 terminal runtime 协调
- 复杂布局裁决
- 复杂领域行为

### 4.3 `Workbench` 应是正式对象

已确认：

`Workbench` 不是概念层或字段集合，而应是一个真实对象。

它应承接：

- workspace 树
- pane/tab/workspace 主工作流
- 主工作台相关状态与语义

---

## 5. 已确认的协作原则

### 5.1 主体采用：纵向对象树 + 横向服务对象

已确认：

- 纵向对象树表达结构与领域所有权
- 横向对象表达协作、同步、渲染、输入翻译等服务能力

### 5.2 布局变化链

已确认：

所有布局尺寸变化应先在纵向对象树中结算，再由 `Resizer` 同步到 terminal：

```text
外部窗口变化 / 用户布局操作
  -> App / Workbench / Workspace / Tab 结算布局
  -> Resizer
  -> TerminalCoordinator / client
  -> daemon PTY resize
```

这意味着：

- `Pane` 不是布局推动者
- `Tab` 才是 pane 布局拥有者

### 5.3 Terminal 数据链

已确认：

```text
daemon
  -> protocol/socket
  -> tui.Client
  -> TerminalCoordinator
  -> TerminalStore
  -> *Terminal
  -> Pane / TerminalPoolPage / Renderer
```

---

## 6. 迁移主线总览

当前讨论的迁移主线不是“继续拆字段”，而是：

### Phase 1：先立 `Workbench`

目的：

- 把主工作流从 `Model` 身上剥出第一个真实大对象
- 让 `Workspace / Tab / Pane` 有合法上层宿主

这一轮不碰：

- `TerminalStore + Terminal` 正式落地
- `Renderer` 重构
- terminal runtime 主线
- 大规模改名

### Phase 2：再立 `App`

目的：

- 让 `App` 成为唯一高层协调入口
- 让 `Model` 真正开始退化成 shell

这一轮不碰：

- terminal runtime 体系重构
- renderer/render loop 主线

### Phase 3：再立 `TerminalStore + Terminal`

目的：

- 把 terminal 语义从 `Pane/ViewPort/Model` 中抽出
- 让 `Pane -> *Terminal` 关系真正成立
- 让 `TerminalPoolPage` 有统一数据源

这一轮不碰：

- 一口气收完全部 runtime 协调
- 顺手重做渲染

### Phase 4：再迁 `TerminalCoordinator + Resizer`

目的：

- 把 terminal runtime 协调从 `Model` 抽离
- 让 bind/unbind/stream/attach/remove/exited/resize 有自然归宿

### Phase 5：最后迁 `Renderer + RenderLoop`

目的：

- 让渲染体系建立在已经稳定的对象树和 terminal 镜像上
- 分清 renderer 与 render loop 的边界

---

## 7. 当前推荐的第一刀

当前脑暴共识下，最推荐的第一刀是：

**先立 `Workbench`。**

原因：

- 它是新架构里的真实对象
- 最贴近当前 TUI 主工作流
- 能最大化复用现有 `Workspace/Tab` 对象化成果
- 风险低于直接碰 terminal runtime

第一轮的真正目标不是“做完 Workbench”，而是：

> 让 `Workbench` 成为主工作流职责的合法宿主，
> 并让 `Model` 开始把主工作台相关逻辑委托给它。

---

## 8. 下一步讨论建议

在当前记录基础上，下一步最适合继续讨论的是：

- `Phase 1: Workbench` 再细分成 2~3 个更稳的子阶段
- 每个子阶段：
  - 先迁什么
  - 不要碰什么
  - 它给后续哪一轮腾位置

这一步完成后，才适合进入正式设计文档与实施计划。
