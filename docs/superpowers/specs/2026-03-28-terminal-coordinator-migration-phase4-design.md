# TerminalCoordinator Migration Phase 4 Design

状态：Draft
日期：2026-03-28

## 1. 背景

Phase 1 已完成 `Workbench` 落地，Phase 2 已完成 `App` 高层协调入口落地，Phase 3 已完成 `TerminalStore + Terminal` 初步落地。

当前 TUI 顶层关系已经开始收敛为：

- `Model` 逐步退化为 Bubble Tea shell 与 live shell state 承载体
- `App` 作为高层协调入口开始稳定
- `Workbench` 作为主工作流对象开始稳定
- `TerminalStore + Terminal` 已经开始承接 terminal 对象归属与部分读路径

但 terminal runtime 协调主线仍然分散：

- `Model` 上仍承载 attach / stream / snapshot / terminal 事件联动的一大部分逻辑
- resize 同步逻辑也仍然主要由 `Model` 驱动
- `TerminalStore + Terminal` 虽然已经落地，但还没有真正成为 runtime 协调的核心落点

这意味着：

- terminal 已经开始成为对象，但 runtime 协调仍没有正式归宿
- `Model` 仍然不是一个纯粹的 shell

因此下一阶段的目标不是继续在 `Model` 上搬一点点 helper，而是：

> 正式建立 `TerminalCoordinator` 与 `Resizer`，
> 把 terminal runtime 协调主线与 resize 同步主线从 `Model` 抽出去。

## 2. 本阶段目标

Phase 4 的主目标是：

- 正式建立 `TerminalCoordinator`
- 正式建立 `Resizer`
- 把 terminal runtime 协调主线从 `Model` 抽离
- 让 `TerminalStore + Terminal` 真正开始承接 runtime 状态更新
- 让布局变化链与 resize 同步链的职责边界清晰下来

本阶段不是 renderer 主线迁移，也不是 render loop 迁移。

## 3. 核心对象关系

本阶段完成后的推荐关系应为：

```text
Model
  -> App
    ├── Workbench
    ├── TerminalStore
    ├── TerminalCoordinator
    └── Resizer
```

进一步展开：

```text
App
  ├── Workbench
  ├── TerminalStore
  ├── TerminalCoordinator
  └── Resizer

Workbench
  └── Workspace -> Tab -> Pane

Pane
  └── *Terminal

TerminalStore
  └── *Terminal[*]

TerminalCoordinator
  └── terminal runtime coordination

Resizer
  └── resize synchronization
```

其中：

- `Model` 不再直接主导大部分 terminal runtime 协调
- `App` 继续作为唯一高层协调入口
- `Workbench` 继续负责主工作流对象树
- `TerminalStore + Terminal` 承接 runtime 状态镜像
- `TerminalCoordinator` 成为 runtime 协调主归宿
- `Resizer` 成为 resize 同步主归宿

## 4. 职责边界

### 4.1 Model

本阶段后，`Model` 主要负责：

- Bubble Tea `Init / Update / View`
- shell 级状态
- 将高层入口转发到 `App`
- 保留少量尚未迁完的边角 runtime glue（如果确有必要）

本阶段后，`Model` 不应继续承担：

- attach / snapshot / stream 主入口协调
- terminal removed / exited / killed 主联动逻辑
- resize 同步主逻辑

### 4.2 App

`App` 继续作为唯一高层协调入口，负责：

- 接收来自 `Model` 的高层消息 / action / command
- 调度：
  - `Workbench`
  - `TerminalStore`
  - `TerminalCoordinator`
  - `Resizer`

`App` 本身不应重新吞下底层 runtime 细节。

### 4.3 TerminalCoordinator

`TerminalCoordinator` 在本阶段统一负责两类事情：

#### A. 与 daemon / client 的通信与同步入口

- list
- attach
- snapshot
- stream
- terminal event subscription

#### B. pane / terminal runtime 协调

- bind / unbind terminal
- terminal removed / exited / killed 联动
- runtime 状态同步进 `TerminalStore`
- 多 pane / 同一 terminal 的 runtime 行为协调

`TerminalCoordinator` 在本阶段不负责：

- renderer
- render loop
- 最终布局裁决

它是 runtime 协调者，不是渲染或布局对象。

### 4.4 Resizer

`Resizer` 负责：

- 消费对象树已经结算好的尺寸变化
- 把尺寸变化同步到 terminal PTY
- 处理 owner / follower 相关的 resize 规则

`Resizer` 不负责：

- 计算布局
- 决定 pane 几何
- 决定 split / tab / floating 结构

### 4.5 TerminalStore + Terminal

本阶段里，`TerminalStore + Terminal` 不再只是对象容器，而要开始真正承接：

- terminal runtime 状态镜像
- attach / snapshot / stream / event 更新结果
- connection context

这意味着它们开始成为 runtime 协调的事实目标对象。

## 5. 迁移范围

本阶段迁移的是：

- `TerminalCoordinator` 类型本身
- `Resizer` 类型本身
- attach / stream / snapshot / terminal event 等主入口的协调归宿
- bind / unbind 与 terminal lifecycle 联动主线
- resize 同步链
- runtime 状态更新进 `TerminalStore + Terminal`

本阶段明确不迁：

- renderer 主线
- render loop
- 更大范围的 UI / 页面结构重写
- `TerminalPoolPage` 的完整页面建设

## 6. 文件结构建议

本阶段建议新增：

- `tui/terminal_coordinator.go`
  - `TerminalCoordinator`
  - runtime 协调主入口
- `tui/resizer.go`
  - `Resizer`
  - resize 同步逻辑
- `tui/terminal_coordinator_test.go`
- `tui/resizer_test.go`

本阶段可能修改：

- `tui/model.go`
- `tui/app.go`
- `tui/picker.go`
- `tui/workbench.go`
- `tui/model_test.go`

如果 attach / stream / terminal event 的逻辑还分散在别的辅助文件，也可以一起接入，但原则是：

- 先立正式归宿对象
- 再迁入口
- 不顺手做 renderer 架构重写

## 7. 数据流设计

### 7.1 terminal runtime 协调链

推荐数据流：

```text
Model / App high-level intent
  -> TerminalCoordinator
  -> tui.Client / daemon
  -> TerminalStore / Terminal runtime mirror
  -> Pane / Workbench / render reads
```

解释：

- 高层入口仍由 `Model -> App` 发起
- 真正的 terminal runtime 协调由 `TerminalCoordinator` 承担
- 协调结果更新到 `TerminalStore + Terminal`
- 上层对象和渲染读取这些 runtime mirror

### 7.2 resize 链

推荐数据流：

```text
window/layout change
  -> Workbench / Workspace / Tab layout settlement
  -> Resizer
  -> TerminalCoordinator / client
  -> daemon PTY resize
```

解释：

- 布局由对象树结算
- `Resizer` 只负责同步
- `TerminalCoordinator` / client 负责和 daemon 通信

## 8. 迁移切刀

### 8.1 切刀 A：先立 TerminalCoordinator

目标：

- 新增 `TerminalCoordinator`
- 建立 `App -> TerminalCoordinator`
- 先接一批 runtime 入口

结果：

- runtime 协调主归宿开始成立

### 8.2 切刀 B：迁 terminal lifecycle 联动

目标：

- 把 bind / unbind
- removed / exited / killed 联动
- 一部分 terminal 状态更新

迁到 `TerminalCoordinator`

结果：

- `Model` 不再主导 terminal lifecycle 协调

### 8.3 切刀 C：迁 attach / stream / snapshot 主入口

目标：

- attach / stream / snapshot 入口迁入 `TerminalCoordinator`
- `TerminalStore + Terminal` 成为更新目标

结果：

- runtime 主线开始收口

### 8.4 切刀 D：再立 Resizer

目标：

- 把 resize 同步逻辑迁到 `Resizer`
- 让 `Model` 不再主导 resize runtime 协调

结果：

- layout settlement 与 resize synchronization 链路分离

## 9. 测试策略

### 9.1 TerminalCoordinator 单元测试

验证：

- attach / snapshot / stream 入口转发正确
- bind / unbind 正确更新 terminal / pane 关系
- removed / exited / killed 联动正确
- runtime 更新正确进入 `TerminalStore`

### 9.2 Resizer 单元测试

验证：

- 只消费布局结算结果，不裁决布局
- owner / follower resize 规则正确
- resize 调用只作用于应作用的 terminal

### 9.3 集成回归测试

重点盯住：

- attach terminal
- split / new tab / floating 之后的 attach
- shared terminal 的 stream owner / resize owner 行为
- removed / exited / restart / close pane 行为
- layout change 之后的 resize 行为

本阶段的测试重点不是“多了两个对象”，而是：

- runtime 协调是否真的离开 `Model`
- `TerminalStore + Terminal` 是否真正接到 runtime 更新
- shared terminal / resize 行为是否不回归

## 10. 风险与约束

### 风险 1：TerminalCoordinator 变成新的大泥球

风险：

- 只是把 `Model` 的 runtime 逻辑整体搬过去
- 形成新的“超级对象”

应对：

- `TerminalCoordinator` 只做 terminal runtime 协调
- 不吞布局裁决
- 不吞 renderer

### 风险 2：Resizer 越界做布局裁决

风险：

- resize 对象反过来干预对象树布局
- 破坏 `Workbench / Tab` 的结构所有权

应对：

- 明确 `Resizer` 只同步，不裁决
- 几何真值继续留在对象树

### 风险 3：shared terminal 行为回归

风险：

- shared terminal stream owner / resize owner 的细节行为被破坏

应对：

- 用现有 shared terminal 测试做主回归
- 每次只迁一批主入口，不一口气抽空所有逻辑

## 11. 成功标准

本阶段成功的判断标准是：

- `TerminalCoordinator` 成为 terminal runtime 协调主归宿
- `Resizer` 成为 resize 同步主归宿
- `Model` 不再直接主导大部分 terminal runtime 协调
- `TerminalStore + Terminal` 真正开始接 runtime 更新
- shared terminal / resize owner / terminal lifecycle 行为不回归
- renderer 主线仍保持稳定

## 12. 结论

Phase 4 的正确方向是：

> 正式建立 `TerminalCoordinator + Resizer`，
> 把 terminal runtime 协调链与 resize 同步链从 `Model` 抽出去，
> 但不提前卷入 renderer 主线。

这样可以继续完成架构主线收敛：

- `App` 作为高层协调入口
- `Workbench` 作为主工作流对象
- `TerminalStore + Terminal` 作为 terminal 对象归宿
- `TerminalCoordinator` 作为 runtime 协调归宿
- `Resizer` 作为 resize 同步归宿

这将为最后的 renderer / render loop 迁移提供稳定前提。
