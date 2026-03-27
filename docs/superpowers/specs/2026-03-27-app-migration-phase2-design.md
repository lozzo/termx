# App Migration Phase 2 Design

状态：Draft
日期：2026-03-27

## 1. 背景

Phase 1 已完成 `Workbench` 落地：

- `Workbench` 已成为真实对象，而不是概念容器
- `Workbench` 已接住 workspace 树所有权
- `Workbench` 已接住一部分主工作流入口
- `Workbench` 已提供主工作台只读视图入口

但当前 TUI 顶层仍存在一个核心问题：

- `Model` 仍同时承担 Bubble Tea shell、应用高层入口、部分主工作流协调、以及 runtime 承载职责

这意味着当前结构虽然已经不再完全由 `Model` 直接拥有主工作流对象树，但它仍然是高层协调入口与多类职责的混合体。

因此下一阶段的目标不是继续堆积 `Model` 辅助方法，而是：

> 正式建立 `App`，让它成为唯一高层协调入口。

## 2. 本阶段目标

Phase 2 的唯一主目标是：

- 让 `App` 成为唯一高层协调入口
- 让 `Model` 进一步退化为 Bubble Tea shell + runtime/live state 承载体
- 让 `Workbench` 继续作为主工作流正式对象，由 `App` 调度

这一阶段不是 terminal runtime 架构迁移，也不是 renderer 迁移。

## 3. 顶层关系

本阶段完成后，推荐的顶层关系应为：

```text
Bubble Tea Msg
  -> Model
  -> App
  -> Workbench / 其他高层对象
```

对象职责关系：

```text
Model
  ├── app *App
  ├── runtime / live state
  └── Bubble Tea shell

App
  ├── workbench *Workbench
  └── high-level actions / message routing

Workbench
  └── Workspace -> Tab -> Pane
```

其中：

- `Model` 不再是“应用根”
- `App` 才是“当前 TUI 进程中的高层应用入口”
- `Workbench` 继续是主工作流对象，而不是被 `App` 取代

## 4. 职责边界

### 4.1 Model

本阶段后，`Model` 主要负责：

- Bubble Tea `Init / Update / View`
- 宿主终端尺寸、render cache、少量 shell 级状态
- 当前阶段仍未迁出的 runtime/live state
- 将高层入口转发给 `App`
- 输出 view 所需内容

本阶段后，`Model` 不应继续承担：

- 高层应用根语义
- 高层主工作流路由中心
- 与 `Workbench` 并列的高层协调职责

### 4.2 App

`App` 是本阶段新增的正式对象，其职责是：

- 作为唯一高层协调入口
- 接受来自 `Model` 的高层消息 / action / command 入口
- 把动作路由到：
  - `Workbench`
  - 当前已存在的高层 helper / 高层工作流
  - 必要时调用 `Model` 暂时保留的 runtime 能力

`App` 在本阶段不负责：

- stream / attach / snapshot / recovery 主线
- terminal runtime 内部细节
- renderer / render loop
- terminal store / terminal proxy 正式落地

### 4.3 Workbench

`Workbench` 在本阶段继续承担：

- 主工作流对象树宿主
- workspace / tab / pane 的 lookup / action / read boundary
- 主工作台语义边界

它不需要在本阶段直接接管所有 runtime 细节。

## 5. 迁移范围

本阶段迁移的是：

- `App` 类型本身
- `Model -> App` 初始化关系
- 一部分已经清晰的高层入口
- 一部分 `Update` 的高层消息分发骨架

优先迁入 `App` 的入口包括：

- workspace / tab / pane 的高层 action 入口
- picker / prompt 的高层打开入口
- 部分 prefix / command 结果分发
- 部分“只是做应用层决策与路由”的消息处理逻辑

本阶段明确不迁：

- `TerminalStore`
- TUI 本地 `Terminal` 代理正式落地
- `TerminalCoordinator`
- `Resizer`
- renderer / render loop 重构
- terminal runtime 主线迁移

## 6. 文件结构建议

本阶段建议先新增：

- `tui/app.go`
  - `App` 类型
  - `NewApp(...)`
  - 核心依赖持有关系
- `tui/app_actions.go`
  - 高层 action / command 入口

可选但默认暂缓：

- `tui/app_update.go`

原因：

- 这一轮目标是先建立高层对象边界，而不是立即按文件层面做大拆分
- 如果一开始就把 `App` 再细拆过多文件，会增加迁移摩擦，降低本阶段收益

因此，推荐先两文件起步；如果实际代码体量明显膨胀，再拆 `app_update.go`。

## 7. 数据流设计

本阶段推荐的数据流：

```text
Bubble Tea Msg
  -> Model.Update()
  -> App
  -> Workbench / 高层 helper / 必要的 runtime callback
```

解释：

1. Bubble Tea 消息进入 `Model.Update()`
2. `Model` 识别当前消息属于：
   - 高层应用协调
   - runtime / 低层细节
3. 高层协调消息转给 `App`
4. `App` 再调度：
   - `Workbench`
   - 高层 command / picker / prompt 入口
   - 必要时通过窄接口回到 `Model` 的 runtime 能力

这样可以保证：

- `App` 真正承担高层协调职责
- `Model` 不被一次性掏空
- runtime-heavy 路径不被误迁

## 8. 依赖策略

本阶段的过渡依赖策略是：

- 允许 `App` 持有对 `Model` runtime 能力的过渡性访问面
- 但不把 `App` 设计成新的“万能 Model 包装器”

推荐做法：

- 可以先让 `App` 持有 `model *Model` 的过渡依赖
- 但只通过少量高层方法使用它
- 在实现中尽量把 `App` 写成“协调器”，而不是“复制一份 Model 方法集合”

这属于过渡性边界，而不是最终形态。

## 9. 迁移切刀

### 9.1 切刀 A：先立 App

目标：

- 新增 `App`
- 在 `NewModel()` 中完成 `app` 初始化
- 让 `workbench` 正式挂到 `app`

结果：

- `App` 从概念变成真实对象
- 后续迁移有合法宿主

### 9.2 切刀 B：迁高层 action / command 入口

优先迁移那些更明显属于高层应用协调的入口：

- workspace / tab / pane 的高层 command
- picker / prompt 打开入口
- 一部分 prefix 结果分发

要求：

- runtime-heavy 内部逻辑不一并迁入
- `App` 只负责“决定做什么、交给谁做”

### 9.3 切刀 C：迁 Update 高层分发骨架

目标：

- 让 `Model.Update()` 对部分高层消息转调 `App`
- 但不在这一轮一口气重写完整 `Update`

要求：

- 只迁高层分发骨架
- 保持现有 runtime 分支稳定

## 10. 测试策略

### 10.1 App 单元测试

验证：

- `App` 是否正确转发高层 action 到 `Workbench`
- `App` 是否正确处理高层消息分发
- `App` 是否没有越界承担 runtime 细节

### 10.2 Model 集成测试

验证：

- `Model -> App -> Workbench` 接线是否成立
- 当前已有用户工作流是否不回归

重点盯住：

- workspace / tab / pane 主流程
- prefix 入口
- picker / prompt 打开路径
- attach / split / bootstrap / render 等现有主路径

### 10.3 回归原则

本阶段测试重点不是“App 文件存在”，而是：

- 高层入口是否真的统一到 `App`
- `Model` 是否真的少承担一部分高层职责
- 现有 runtime 行为是否稳定

## 11. 风险与约束

### 风险 1：App 变成新的大泥球

风险：

- 只是把 `Model` 的高层逻辑复制到 `App`
- 结果从一个大对象变成两个大对象

应对：

- `App` 只承担高层协调，不吞 runtime 内部实现
- 只迁明显属于应用路由层的逻辑

### 风险 2：误伤 runtime 主线

风险：

- 过早迁移 attach / stream / resize / recovery
- 导致大面积回归

应对：

- 明确本阶段不碰 terminal runtime 主线
- 遇到 runtime-heavy 路径时保守处理

### 风险 3：Model 名义变薄、实质不变

风险：

- 表面引入 `App`
- 实际高层入口仍主要留在 `Model`

应对：

- 在 plan 中明确哪些入口必须迁到 `App`
- 用测试覆盖高层分发接线，而不是只测试对象存在

## 12. 成功标准

本阶段成功的判断标准是：

- `App` 是真实高层对象，而不是空壳
- `App` 成为唯一高层协调入口
- `Model` 明显开始退化成 shell + runtime 承载体
- `Workbench` 继续作为主工作流宿主
- terminal runtime / renderer 主线没有被提前卷入
- 现有主路径没有明显回归

## 13. 结论

Phase 2 的正确方向是：

> 先立 `App`，但只迁高层协调，不迁 runtime 主线。

这能在不打断现有 TUI 主线的前提下，进一步完成顶层对象关系的收敛：

- `Model` 退化为 shell
- `App` 成为唯一高层协调入口
- `Workbench` 继续作为主工作流正式对象

这也是后续 `TerminalStore + Terminal`、`TerminalCoordinator + Resizer`、`Renderer + RenderLoop` 能顺序迁移的必要前提。
