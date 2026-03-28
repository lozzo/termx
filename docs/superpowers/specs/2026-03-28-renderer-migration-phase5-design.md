# Renderer Migration Phase 5 Design

状态：Draft
日期：2026-03-28

## 1. 背景

Phase 1 已完成 `Workbench` 落地，Phase 2 已完成 `App` 高层协调入口落地，Phase 3 已完成 `TerminalStore + Terminal` 初步落地，Phase 4 正在/已把 terminal runtime 协调与 resize 同步逐步迁入正式横向对象。

当前对象关系已经趋于清晰：

- `App` 作为高层协调入口
- `Workbench` 作为主工作流对象树宿主
- `TerminalStore + Terminal` 作为 terminal 对象归宿
- `TerminalCoordinator` 作为 terminal runtime 协调归宿
- `Resizer` 作为 resize 同步归宿

但渲染主线仍然主要散在：

- `Model.View()`
- `render.go`
- `render_coordinator.go`
- 各种 render cache / render dirty / render pending / flush / batching 辅助逻辑

这意味着：

- 渲染系统虽然已有实现，但正式归宿尚不清晰
- `Model` 仍然不是一个足够干净的 shell

因此最后一个大阶段的目标是：

> 正式建立 `Renderer` 与 `RenderLoop`，
> 把 frame 生成主线与渲染调度主线从 `Model` 抽出去。

## 2. 本阶段目标

Phase 5 的主目标是：

- 正式建立 `Renderer`
- 正式建立 `RenderLoop`
- 把渲染主线从 `Model` 抽离
- 让 `Model` 最终更接近纯 Bubble Tea shell
- 让渲染系统建立在已经成形的对象树与 terminal 镜像之上

本阶段的激进点在于：

- 不只是增加两个对象
- 而是让它们真正成为渲染内容与渲染时序的正式归宿

## 3. 核心对象关系

本阶段完成后的推荐关系应为：

```text
Model
  -> App
    ├── Workbench
    ├── TerminalStore
    ├── TerminalCoordinator
    ├── Resizer
    ├── Renderer
    └── RenderLoop
```

进一步展开：

```text
App
  ├── Workbench
  ├── TerminalStore
  ├── TerminalCoordinator
  ├── Resizer
  ├── Renderer
  └── RenderLoop
```

其中：

- `Model` 继续只负责 Bubble Tea shell
- `Renderer` 成为 frame 生成归宿
- `RenderLoop` 成为 tick / batching / flush / backpressure 归宿
- `Renderer + RenderLoop` 建立在前面阶段已经稳定的对象边界之上

## 4. 职责边界

### 4.1 Model

本阶段后，`Model` 主要负责：

- Bubble Tea `Init / Update / View`
- 持有少量 shell 级状态
- 调 `App`
- 接回渲染输出

本阶段后，`Model` 不应继续承担：

- 主要 frame 拼装逻辑
- batching / flush / backpressure 主逻辑
- render dirty / pending / interactive render 主调度逻辑

### 4.2 Renderer

`Renderer` 在本阶段负责：

- 读取：
  - `Workbench`
  - `TerminalStore`
  - terminal mirror
  - shell state
- 产出最终 frame / view 字符串
- 做极少量局部渲染 bookkeeping
  - 局部 cache key
  - 局部缓存适配
  - 轻量 dirty bookkeeping

`Renderer` 不负责：

- terminal runtime 协调
- terminal 状态修复
- 事件分发
- 布局裁决

它默认应尽量纯读，不成为状态裁决器。

### 4.3 RenderLoop

`RenderLoop` 在本阶段负责：

- tick 驱动渲染
- batching
- flush
- backpressure
- interactive render window / render 节流策略

`RenderLoop` 不负责：

- 生成 frame 内容
- 决定 pane 显示语义
- 直接处理 terminal runtime 细节

它是调度器，不是 renderer，也不是业务对象。

## 5. 迁移范围

本阶段迁移的是：

- `Renderer` 类型本身
- `RenderLoop` 类型本身
- frame 生成主入口
- batching / flush / backpressure / tick 驱动主链
- render cache / render pending / render dirty 主调度链收口

本阶段明确不迁：

- terminal runtime 主线
- resize 主线
- 更大范围的 UI 语义重设计
- layout 系统重构
- 重新发明 pane compositor 算法

## 6. 文件结构建议

本阶段建议新增：

- `tui/renderer.go`
  - `Renderer`
  - 顶层 frame 生成入口
- `tui/render_loop.go`
  - `RenderLoop`
  - tick / batching / flush / backpressure
- `tui/renderer_test.go`
- `tui/render_loop_test.go`

本阶段可能修改：

- `tui/model.go`
- `tui/render.go`
- `tui/render_coordinator.go`
- `tui/render_benchmark_test.go`
- `tui/model_test.go`

原则：

- 可以复用 `render.go` 已有底层实现
- 但“谁来生成 frame、谁来驱动渲染节奏”要迁到新对象
- 不强迫一开始就把所有底层 helper 拆得一干二净

## 7. 数据流设计

### 7.1 frame 生成链

推荐数据流：

```text
Model.View()
  -> App / Renderer
  -> read Workbench / TerminalStore / terminal mirror / shell state
  -> final frame
```

解释：

- `Model.View()` 逐步退化为转调入口
- `Renderer` 读取稳定对象边界，产出最终显示结果

### 7.2 渲染调度链

推荐数据流：

```text
Update / runtime changes / tick
  -> RenderLoop
  -> batching / flush / backpressure / interactive window
  -> Renderer
  -> frame output
```

解释：

- 渲染调度与 frame 生成分离
- `RenderLoop` 决定“何时渲染、何时 flush”
- `Renderer` 决定“渲染出什么”

## 8. 迁移切刀

### 8.1 切刀 A：先立 Renderer

目标：

- 新增 `Renderer`
- 先把渲染内容入口对象立起来
- 让 `View()` 开始能转调 `Renderer`

结果：

- frame 生成归宿开始成立

### 8.2 切刀 B：迁主要渲染入口

优先迁移：

- tab bar / status / content body 的顶层拼装
- workbench-facing read boundary 消费
- pane frame title/meta 这类高层读取入口

结果：

- `Model` 不再直接拼大部分顶层 frame

### 8.3 切刀 C：再立 RenderLoop

目标：

- 把 batching / flush / backpressure / render tick 迁入 `RenderLoop`
- 让 `Model` 不再主导渲染节奏

结果：

- 渲染时序主链归宿成立

### 8.4 切刀 D：收口缓存与 dirty 主链

目标：

- 让 render cache / render dirty / render pending 的主调度逻辑归入 `Renderer + RenderLoop`
- 尽量复用现有 dirty redraw 算法与底层实现

结果：

- 渲染主链真正从 `Model` 收口出去

## 9. 测试策略

### 9.1 Renderer 单元测试

验证：

- 能从对象树与 terminal mirror 生成正确 frame
- workbench 读边界使用正确
- pane title/meta / tab bar / status / content body 主路径正确

### 9.2 RenderLoop 单元测试

验证：

- tick 驱动
- batching
- flush
- backpressure
- interactive rendering 时序

### 9.3 集成回归测试

重点盯住：

- `Model.View()` 主路径
- alt-screen / snapshot 显示
- dirty row / dirty region
- floating overlap redraw
- render cache 命中与回退
- startup / attach / picker / workspace 切换后的 frame 稳定性

本阶段测试重点不是“有两个新对象”，而是：

- 渲染主线是否真的离开 `Model`
- 现有显示语义是否稳定
- 渲染缓存与调度链是否仍然正确

## 10. 风险与约束

### 风险 1：Renderer 越界做 runtime 修复

风险：

- `Renderer` 变成新的状态裁决器
- 一边读一边偷偷修 runtime 状态

应对：

- `Renderer` 默认尽量纯读
- 只允许极少量局部 bookkeeping

### 风险 2：RenderLoop 越界做业务路由

风险：

- 调度对象开始处理业务逻辑
- 最终变成新的 `Model`

应对：

- `RenderLoop` 只管渲染时序
- 不碰 terminal/runtime/布局业务决策

### 风险 3：一口气改掉底层 compositor 行为

风险：

- 边界迁移演变成算法重写
- 回归面极大

应对：

- 优先复用现有 `render.go` 内底层实现
- 先迁入口和主链，不急着大改内部算法

### 风险 4：render cache / dirty 链路被破坏

风险：

- 看起来能渲染，但性能或局部重绘退化
- 出现奇怪闪烁、漏刷、过刷

应对：

- 保留 benchmark / dirty redraw 测试
- 重点回归缓存命中和局部刷新行为

## 11. 成功标准

本阶段成功的判断标准是：

- `Renderer` 是正式渲染入口
- `RenderLoop` 是正式调度入口
- `Model` 不再主导渲染主线
- render cache / pending / dirty / batching 主链已收口
- 现有显示语义不明显回归
- 到这里，整个迁移主线完成“对象树 + 高层协调 + terminal runtime + 渲染”四大主链收口

## 12. 结论

Phase 5 的正确方向是：

> 正式建立 `Renderer + RenderLoop`，
> 把渲染内容主线与渲染调度主线从 `Model` 抽出去，
> 但激进地收边界，不激进地重写渲染语义与底层算法。

这样可以在保持现有显示行为稳定的前提下，完成整个架构迁移主线的最后收口：

- `App` 作为高层协调入口
- `Workbench` 作为主工作流对象
- `TerminalStore + Terminal` 作为 terminal 对象归宿
- `TerminalCoordinator + Resizer` 作为 runtime / resize 归宿
- `Renderer + RenderLoop` 作为渲染主链归宿

到此，`Model` 将真正接近一个纯粹的 Bubble Tea shell。
