# termx TUI 渲染迁移设计

状态：Draft
日期：2026-03-25

## 1. 背景与目标

当前 TUI 主线已经具备继续建设最终界面的基础：

- `intent -> reducer -> effect -> runtime` 主链路已经存在
- `workspace / tab / pane / terminal / overlay` 语义已经稳定
- 键盘与鼠标入口已经统一到 `tui/bt/intent_mapper.go`
- startup / restore / terminal store / session bootstrap 已经具备

真正阻碍继续推进的不是状态层，而是渲染层仍混有大量过渡实现：

- [tui/runtime_renderer.go](/home/lozzow/workdir/termx/tui/runtime_renderer.go)
- [tui/runtime_modern_renderer.go](/home/lozzow/workdir/termx/tui/runtime_modern_renderer.go)
- [tui/bt/mouse_hit.go](/home/lozzow/workdir/termx/tui/bt/mouse_hit.go)

这些文件里同时存在：

- 最终产品工作台需要的渲染逻辑
- 过渡期文本壳与说明性 UI
- 依赖字符串内容的命中测试

旧版 TUI 虽然架构不可继续维护，但其渲染、裁切、命中、拖拽、浮层合成等问题已经踩坑较深：

- [deprecated/tui-legacy/pkg/render.go](/home/lozzow/workdir/termx/deprecated/tui-legacy/pkg/render.go)
- [deprecated/tui-legacy/pkg/layout.go](/home/lozzow/workdir/termx/deprecated/tui-legacy/pkg/layout.go)
- [deprecated/tui-legacy/pkg/input.go](/home/lozzow/workdir/termx/deprecated/tui-legacy/pkg/input.go)

本设计目标不是把旧版整套搬回，而是在保留现有新架构主干的前提下，迁回旧版里已经被验证过的“纯渲染能力”和“纯布局/几何能力”，尽快完成最终 TUI 工作台。

## 2. 结论

### 2.1 当前代码是否已经能支撑 TUI 界面开发

结论：能。

原因：

- 新架构的状态边界已经清晰，业务语义也基本齐备
- 输入归一化已经统一，不需要回退到旧版输入分发
- reducer 和 runtime 已可持续承载后续 UI 行为

当前不适合继续沿用的只有“过渡渲染层”。

### 2.2 迁移策略结论

采用以下策略：

- 保留新架构中的 `state / reducer / runtime / terminal store / input mapping`
- 删除或逐步替换过渡 renderer 与 workbench 文本命中逻辑
- 从旧版迁回纯渲染、纯布局、纯命中、纯几何处理
- 不回迁旧版的大一统 `Model`

## 3. 保留边界与替换边界

### 3.1 明确保留

以下部分继续保留，不重写：

- `tui/domain/**`
- `tui/app/reducer/**`
- `tui/runtime*.go` 中与 session / terminal store / updates / startup 相关部分
- `tui/bt/model.go`
- `tui/bt/intent_mapper.go`

### 3.2 明确替换

以下部分作为本轮主替换对象：

- [tui/runtime_renderer.go](/home/lozzow/workdir/termx/tui/runtime_renderer.go)
- [tui/runtime_modern_renderer.go](/home/lozzow/workdir/termx/tui/runtime_modern_renderer.go)
- [tui/bt/mouse_hit.go](/home/lozzow/workdir/termx/tui/bt/mouse_hit.go) 中 workbench 命中部分

### 3.3 明确不回迁

以下内容不回迁：

- 旧版大一统 `Model`
- 旧版事件入口总控
- 旧版把状态、输入、渲染缓存绑死在一个对象上的结构
- 旧版通过单个 `View()` 分支硬拼全部 overlay 的方式

## 4. 旧版可迁移能力清单

### 4.1 布局与几何能力

来源：

- [deprecated/tui-legacy/pkg/layout.go](/home/lozzow/workdir/termx/deprecated/tui-legacy/pkg/layout.go)

可迁内容：

- `Rects`
- `Adjacent`
- `ContainsPane`
- `LeafIDs`
- `SwapWithNeighbor`
- `AdjustPaneBoundary`

处理原则：

- 已存在于新 domain 中的保留现状
- 新 domain 缺失的几何能力补齐到新 domain 或 projection 辅助层
- 不从 renderer 反向发明布局算法

### 4.2 画布与绘制原语

来源：

- [deprecated/tui-legacy/pkg/render.go](/home/lozzow/workdir/termx/deprecated/tui-legacy/pkg/render.go)

可迁内容：

- `drawCell`
- `composedCanvas`
- `set / fill / redrawDamage`
- pane frame / title / body 绘制
- cursor 绘制
- clipping / overlap / z-order 的实际处理
- row cache / dirty row 的增量刷新思路

处理原则：

- 迁的是画布能力，不是旧版页面结构
- 将绘制能力拆到新的 `canvas / compositor / surface` 中

### 4.3 命中、拖拽、缩放能力

来源：

- [deprecated/tui-legacy/pkg/input.go](/home/lozzow/workdir/termx/deprecated/tui-legacy/pkg/input.go)

可迁内容：

- `paneAtPoint` 思路
- floating resize handle hit test
- drag move / drag resize 的几何更新
- z-order 调整思路

处理原则：

- 保留新架构输入入口
- 只迁“纯命中”和“纯几何状态推进”
- 不回迁旧版事件分发结构

### 4.4 layout 声明能力

来源：

- [deprecated/tui-legacy/pkg/layout_decl.go](/home/lozzow/workdir/termx/deprecated/tui-legacy/pkg/layout_decl.go)

可借鉴内容：

- layout spec 校验
- export/import 结构
- arrange/build 思路

优先级结论：

- 当前优先级低于 render/workbench/floating
- 暂不作为迁移主线阻塞项

## 5. 当前应删除的过渡代码

### 5.1 应停止扩展的代码

- [tui/runtime_renderer.go](/home/lozzow/workdir/termx/tui/runtime_renderer.go) 中 `screen_shell`、`section_*`、`chrome_*`、`wireframe` 相关输出
- [tui/runtime_modern_renderer.go](/home/lozzow/workdir/termx/tui/runtime_modern_renderer.go) 中仅用于过渡卡片式展示的工作台拼装

结论：

- 这些代码只适合作为临时 fallback
- 不应继续往里面补最终功能

### 5.2 应逐步移除的 workbench 文本命中

- [tui/bt/mouse_hit.go](/home/lozzow/workdir/termx/tui/bt/mouse_hit.go) 中基于渲染字符串内容解析 pane 点击目标的逻辑

处理原则：

- overlay 行列表点击可暂时保留文本命中
- workbench 主体必须改成几何命中

## 6. 新 renderer 分层设计

新 renderer 需要显式拆层，并通过接口解耦：

```text
AppState + RuntimeTerminalStore
              │
              v
Projection
  ├─ tiled pane rects
  ├─ floating pane rects
  ├─ active focus / z-order
  └─ overlay geometry
              │
              v
Pane Surface
  ├─ live vterm surface
  ├─ snapshot surface
  ├─ empty/waiting/exited surface
  └─ cursor/meta/status surface
              │
              v
Canvas
  ├─ drawCell
  ├─ frame/title/body
  ├─ clipping
  └─ dirty rows
              │
              v
Workbench Compositor
  ├─ tiled draw
  ├─ floating draw
  └─ overlap/z-order
              │
              v
Overlay Compositor
  ├─ backdrop
  ├─ modal placement
  └─ cleanup/focus return
              │
              v
Hit Test
  ├─ paneAtPoint
  ├─ resize handle hit
  └─ overlay geometry hit
```

建议目录形态：

```text
tui/render/
  projection/
  surface/
  canvas/
  compositor/
  overlay/
  hittest/
```

## 7. 实施顺序

### 阶段 1：渲染层切割

目标：

- 把过渡 renderer 从最终 renderer 主线中剥离
- 建立新 renderer pipeline 骨架

工作：

- 新建 render 分层目录
- 定义 projection / surface / compositor / hittest 接口
- 让 runtime renderer 只负责组装，不再承载大量 UI 文本拼接
- 保留过渡 renderer 作为短期 fallback，但停止继续扩展

验证：

- 新旧入口同时可编译
- renderer 骨架测试可跑

### 阶段 2：tiled workbench

目标：

- 恢复 single pane / split pane 的真实工作台

工作：

- 用新 layout domain 输出 tiled rects
- 从旧版补齐 boundary adjust / swap / leaf 工具
- 建立 pane projection
- 建立 live / snapshot / empty / waiting / exited surface
- 完成 pane frame/title/body/cursor 绘制

验证：

- tiled layout 几何测试
- pane surface 渲染测试
- split 后 terminal 内容正确贴入 pane

### 阶段 3：floating compositor

目标：

- 恢复真实浮窗

工作：

- 引入 floating entries / z-order
- 迁回 overlap / clipping / compositor 顺序
- 补几何命中
- 实现 move / resize / bring-to-front

验证：

- floating overlap 测试
- z-order 测试
- drag / resize 几何测试

### 阶段 4：overlay compositor

目标：

- 恢复 overlay 作为真正盖板层

工作：

- overlay 单独走 backdrop + modal 合成
- 关闭 overlay 后做画面清理
- 维护 return focus
- overlay 内部列表点击短期保留文本命中

验证：

- overlay open/close cleanup
- return focus 测试
- picker / manager / prompt 基本展示可用

### 阶段 5：业务流回接

目标：

- 让工作台不是只“能画”，而是能工作

工作：

- terminal picker
- terminal manager
- prompt
- layout resolve
- restore / startup / session bootstrap

验证：

- reducer + runtime + renderer 联动测试
- 关键业务流定向测试

### 阶段 6：性能与正确性修补

目标：

- 迁回旧版已经踩过的性能与边界处理

工作：

- dirty rows / damage redraw
- 宽字符与 cursor 处理
- clipping 残影修复
- backpressure
- benchmark

验证：

- render benchmark
- 高频更新稳定性测试

## 8. 每轮交付建议

建议按以下工作块提交：

1. renderer 骨架切割 + projection 接口
2. tiled canvas + pane surface
3. floating compositor + hit-test + drag/resize
4. overlay compositor + focus return
5. 业务流回接 + 清理过渡代码 + 全量验证

每轮都应同时包含：

- 功能代码
- 对应测试
- 中文提交信息

## 9. 测试策略

最低验证要求：

- 被修改包的定向 `go test`
- `PATH=\"/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH\" go test ./... -count=1`

测试重点：

- layout rect 投影
- adjacent / boundary adjust / swap
- pane surface 渲染
- floating overlap / clipping / z-order
- drag / resize 命中与状态推进
- overlay cleanup / focus return
- renderer frame 级断言

## 10. 风险与处理

风险 1：直接在现有 renderer 文件上继续堆代码，导致迁移无限拖延

处理：

- 先切渲染分层，再做功能

风险 2：把旧版实现整块搬回，重新引入旧耦合

处理：

- 只迁纯算法、纯几何、纯画布能力

风险 3：过早追求 dirty redraw 与性能，主线再次被细节拖住

处理：

- 先恢复正确工作台，再补增量刷新

风险 4：workbench 继续依赖文本命中，导致拖拽与浮窗行为不可靠

处理：

- workbench 必须切到几何命中

## 11. 本设计的实施判断

本设计主张的不是“慢慢优化当前 renderer”，而是：

- 现在的新架构已经够用
- 旧版踩坑成果必须回收
- 先把 renderer 层拆对
- 再按 tiled -> floating -> overlay -> 业务回接 的顺序推进

这是当前最短路径，也是风险最低的路径。
