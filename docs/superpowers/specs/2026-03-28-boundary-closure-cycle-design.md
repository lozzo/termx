# Boundary Closure Cycle Design

状态：Draft
日期：2026-03-28

## 1. 背景

截至当前迁移主线，TUI 已完成以下正式对象边界的引入与首轮迁移：

- Phase 1：`Workbench`
- Phase 2：`App`
- Phase 3：`TerminalStore + Terminal`
- Phase 4：`TerminalCoordinator + Resizer`
- Phase 5：`Renderer + RenderLoop`

这意味着主线对象图已经基本成形：

```text
Model
  -> App
    ├── Workbench
    ├── TerminalStore / Terminal
    ├── TerminalCoordinator
    ├── Resizer
    ├── Renderer
    └── RenderLoop
```

但“对象已经存在”不等于“边界已经彻底坐实”。当前代码大概率仍保留少量迁移过渡路径，包括：

- `Model` 中残留的旧主线路径或旁路入口
- 结构状态与运行时状态之间的补丁式同步逻辑
- runtime 协调与 render 协调中的兼容层或双入口
- 少量仅为迁移阶段保留的 helper / glue 代码

因此，下一周期不再新增新的架构相位对象，而是进入 **boundary closure** 周期：

> 不再扩张对象树，
> 而是把 Phase 1–5 已经迁出的职责彻底收口，
> 让现有对象边界成为唯一正式主线。

## 2. 本周期目标

本周期的目标是：

- 让 `Model` 更接近纯 Bubble Tea shell
- 让 `App` 成为唯一高层协调入口
- 让 `Workbench` 成为结构真相来源
- 让 `TerminalStore + Terminal` 成为 terminal 身份与镜像真相来源
- 让 `TerminalCoordinator + Resizer` 成为 runtime / resize 协调真相来源
- 让 `Renderer + RenderLoop` 成为渲染内容与渲染调度真相来源
- 清理迁移阶段遗留的旁路、重复路径与双重所有权
- 在不改变产品语义的前提下，完成正式边界收官

本周期明确**不是**：

- 新增 Phase 6 大对象
- 再发明一套新的对象图
- 重写 terminal runtime
- 重写 pane compositor / renderer 算法
- 做产品语义或交互语义重设计
- 借机做大规模无关重构

## 3. 核心策略

### 3.1 boundary-closure，而不是 phase-expansion

本周期采用的不是“继续发明新对象”的策略，而是：

- 保持已有对象图不变
- 识别哪些旧路径只是迁移过渡期留下来的兼容路径
- 逐步删除这些过渡路径
- 让每一类职责只保留一个正式归宿

因此，本周期完成后，代码结构不应“更大”，而应“更干净”。

### 3.2 收口优先级

优先级如下：

1. 清理 `Model` 中残留主线路径
2. 固化结构真相与运行时真相边界
3. 清理 runtime / render 协调残留旁路
4. 用最小必要测试与文档把收口结果钉住

### 3.3 删除重复路径，而不是新增抽象

本周期的主要动作应是：

- 删除重复入口
- 删除双重所有权
- 删除临时兼容逻辑
- 压缩已经失去必要性的 glue code

而不是：

- 再引入新的管理器/协调器/适配器
- 为一次性迁移问题设计长期抽象
- 继续把对象图层级做厚

## 4. 职责边界目标态

### 4.1 Model

本周期完成后，`Model` 主要负责：

- Bubble Tea `Init / Update / View`
- 少量 shell 级状态
- 消息接线与对象编排入口调用

`Model` 不应继续主导：

- 主工作流结构决策
- terminal runtime 主协调
- resize 主协调
- render scheduling 主协调
- 顶层 frame 生成主逻辑

它可以保留最薄的一层转调壳，但不应继续成为“最终真相归宿”。

### 4.2 App

`App` 继续作为唯一高层协调入口，负责：

- 承接 `Model` 发起的高层动作
- 组合 `Workbench`、runtime 对象与 render 对象完成动作编排
- 避免 `Model` 直接横穿多个对象边界

`App` 不是结构真相，也不是运行时真相；它是高层 orchestration 入口。

### 4.3 Workbench

`Workbench` 作为结构对象树真相来源，负责：

- `Workspace -> Tab -> Pane` 结构拥有权
- 结构类入口与结构类只读视图
- 结构操作后的稳定输出

`Workbench` 不负责：

- terminal runtime 协调
- render scheduling
- 终端 attach/exit/kill 细节

### 4.4 TerminalStore / Terminal

`TerminalStore + Terminal` 负责：

- terminal 身份
- terminal 元数据
- terminal 快照镜像
- 与 pane 关联时的正式 terminal 对象归宿

它们不应再只是“辅助缓存”；本周期后应成为 terminal 相关读路径的正式来源。

### 4.5 TerminalCoordinator / Resizer

`TerminalCoordinator + Resizer` 负责：

- attach
- exit / kill 状态同步
- resize 主协调
- 相关 runtime 操作的一致入口

它们不负责：

- pane 结构裁决
- workbench 结构所有权
- 顶层渲染内容生成

### 4.6 Renderer / RenderLoop

`Renderer + RenderLoop` 负责：

- frame 内容生成
- tick / pending / flush / batching / backpressure 等调度主线
- render cache / dirty / pending 的正式主入口收口

它们不是业务工作流对象，也不应倒退成新的“巨型 Model”。

## 5. 需要收口的四个块

### 5.1 Block 1：`Model` 残留主线路径收口

目标：把 `Model` 中仍在直接触碰旧边界的逻辑继续压薄。

重点包括：

- 仍绕过 `App` 的高层动作分支
- 仍直接操作 workspace/tree/runtime/render 状态的代码
- 迁移阶段保留下来的过渡 helper
- 只为兼容旧主线而存在的旁路调用

成功标准：

- `Model` 中主要逻辑表现为“接消息 -> 转调对象 -> 回收结果”
- 大部分主线决策不再写死在 `Model`

### 5.2 Block 2：结构真相与运行时真相分离固化

目标：把结构边界与运行时边界进一步稳定下来，降低双重所有权。

重点包括：

- `Workbench` 结构树与 live pane/runtime identity 的关系
- pane 删除、切换 workspace/tab、attach 后的同步行为
- `Terminal` 镜像与 viewport/runtime 状态的边界
- 为保留 live identity 而引入的补丁式同步逻辑是否还能继续收缩

成功标准：

- 不再出现明显“结构真相一份、运行时真相一份、两边都能偷偷改”的主路径
- 结构同步逻辑和 runtime 保活逻辑边界可解释、可测试

### 5.3 Block 3：运行时协调与渲染协调残留清扫

目标：清掉 runtime / render 主线中的历史尾巴。

重点包括：

- attach / exit / kill / resize 是否仍有旁路
- `Renderer` / `RenderLoop` 之外是否仍有 render pending / flush / tick / cache / dirty 的第二入口
- `render_coordinator.go` 是否还能继续瘦身
- 旧 helper 是否仍在主路径承担关键职责

成功标准：

- runtime 主线由 `TerminalCoordinator + Resizer` 主导
- render 主线由 `Renderer + RenderLoop` 主导
- 不再有成规模的双入口并存

### 5.4 Block 4：最小必要验证与文档固化

目标：用有限但有效的验证，把收口结果固定下来。

包括：

- 针对收口点补 focused tests
- 跑 TUI 包测试
- 跑全仓测试
- 跑 CLI build
- 输出本周期设计与实现计划，形成正式收官文档

成功标准：

- 收口后的边界可以被文档准确描述
- 验证结果能证明迁移不是“看起来干净”，而是“真的稳定”

## 6. 主要文件落点

### 6.1 重点修改文件

- `tui/model.go`
  - 继续压薄 shell 边界
  - 收口残留直连路径与过渡 helper
- `tui/app.go`
  - 承接需要上提的高层动作编排
- `tui/workbench.go`
  - 收紧结构入口与只读视图边界
- `tui/picker.go`
  - 清理 terminal/workbench/runtime 混合判断的残留路径
- `tui/terminal_coordinator.go`
  - 收口 runtime 协调逻辑
- `tui/resizer.go`
  - 收口 resize 主线
- `tui/renderer.go`
  - 收紧 frame 生成职责
- `tui/render_loop.go`
  - 收紧调度职责
- `tui/render_coordinator.go`
  - 能继续下沉的旧协调逻辑继续下沉

### 6.2 重点测试文件

- `tui/model_test.go`
- `tui/app_test.go`
- `tui/renderer_test.go`
- `tui/render_loop_test.go`
- `tui/terminal_coordinator_test.go`
- `tui/resizer_test.go`

### 6.3 新文件策略

本周期默认**尽量不新增新对象文件**。

如果需要新增文件，也应只限于：

- focused regression test file
- 很小的 boundary helper file

原则是收口，而不是扩张。

## 7. 错误处理策略

本周期不追求引入新的复杂错误模型，而追求“错误归属更清楚”。

原则如下：

- 结构类失败由结构边界返回
- runtime 协调失败由 `TerminalCoordinator` / `Resizer` 返回
- render / scheduling 失败由 `Renderer` / `RenderLoop` 处理或向上返回最小必要结果
- `Model` 只负责接线，不负责重新发明底层错误语义

这样调试时可以更快判断问题属于：

- 结构边界
- runtime 边界
- render/scheduling 边界

## 8. 测试策略

### 8.1 focused regression 优先

本周期仍采用 focused regression tests 优先策略。

重点测试：

- `Model` 是否仍通过 `App` / `Renderer` / `RenderLoop` 转调
- attach / workspace 切换 / pane 删除后的边界是否保持一致
- render pending / flush / dirty / cache 是否仍由新边界主导
- runtime 退出与 resize 路径是否仍由正式对象主导

### 8.2 package 级验证

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -count=1
```

### 8.3 repository 级验证

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./... -count=1
```

### 8.4 build 验证

```bash
PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go build ./cmd/termx
```

## 9. 风险与约束

### 风险 1：误删兼容路径导致隐性回归

风险：

- 某些过渡路径虽然看起来重复，但实际仍承接关键兼容语义

应对：

- 先识别主路径与旁路
- 优先删除最重复、最容易解释的兼容层
- 每一块都配 focused tests

### 风险 2：为了收口而再次新增抽象

风险：

- 原本应该删除重复路径，结果反而又包一层新抽象

应对：

- 默认先删路径，再考虑抽象
- 没有重复需求就不新增对象

### 风险 3：结构真相与运行时真相再次缠绕

风险：

- 为了图省事，把 runtime 状态又塞回结构对象

应对：

- 保持结构边界与 runtime 边界分离
- 允许最薄同步，但不允许边界倒退

### 风险 4：render 收口演变成算法重写

风险：

- 收口任务滑向 compositor / renderer 算法级重构

应对：

- 本周期只收入口与职责
- 不重写已有底层渲染语义

## 10. 成功标准

本周期结束时，应满足：

- `Model` 进一步接近纯 Bubble Tea shell
- `App` 作为唯一高层协调入口更加坐实
- `Workbench`、`TerminalStore`、`TerminalCoordinator`、`Resizer`、`Renderer`、`RenderLoop` 都成为各自职责的唯一正式主线归宿
- 不再存在明显的双重所有权主路径
- runtime / render 没有成规模旁路
- 当前正式架构可以被文档准确描述
- `go test ./tui`、`go test ./...`、`go build ./cmd/termx` 全部通过

## 11. 结论

下一周期的本质不是“再新增一个迁移 phase”，而是：

> 把已经迁出的 7 条主线彻底打磨成正式边界，
> 让 `Model` 变薄，让对象边界固定，
> 让整个 TUI 迁移从“基本完成”走到“正式闭环”。

本周期完成后，termx TUI 的架构主线应稳定为：

- `Model`：Bubble Tea shell
- `App`：高层协调入口
- `Workbench`：结构对象树
- `TerminalStore + Terminal`：terminal 对象归宿
- `TerminalCoordinator + Resizer`：runtime / resize 协调归宿
- `Renderer + RenderLoop`：渲染内容与调度归宿

到这里，后续演进就应建立在正式对象边界之上，而不再继续进行架构迁移本身。
