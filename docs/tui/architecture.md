# termx TUI 架构设计

状态：Draft v2
日期：2026-03-25

---

## 1. 目标

这份文档的目标不是再抽象讨论“理想架构”，而是把当前仓库已经做对的部分和后续必须重写的部分分开。

当前统一结论：

1. 数据驱动层继续保留
2. runtime 接线层继续保留
3. 旧版 `deprecated/tui-legacy/` 的 renderer/compositor 思路应该回收
4. 当前过渡 ASCII renderer 应该被替换，而不是继续扩展

### 1.1 总体架构图

```text
                                  termx TUI

┌──────────────────────────────────────────────────────────────────────────────┐
│  Input / Runtime Messages                                                   │
│  keyboard  mouse  resize  stream frame  terminal event  timer              │
└───────────────────────────────┬──────────────────────────────────────────────┘
                                │
                                v
┌──────────────────────────────────────────────────────────────────────────────┐
│  bt shell                                                                   │
│  tui/bt/model.go  +  tui/bt/intent_mapper.go                                │
│  负责把输入归一化并驱动 reducer / effect handler / renderer                 │
└───────────────────────────────┬──────────────────────────────────────────────┘
                                │
                    ┌───────────┴───────────┐
                    │                       │
                    v                       v
┌──────────────────────────────┐  ┌───────────────────────────────────────────┐
│  app/reducer                 │  │  runtime                                 │
│  纯状态迁移 + effect planning│  │  session / update / store / input / svc  │
└───────────────┬──────────────┘  └──────────────────────┬────────────────────┘
                │                                        │
                v                                        v
┌──────────────────────────────────────────────────────────────────────────────┐
│  domain state + runtime terminal store                                      │
│  AppState(workspace/tab/pane/connection/overlay/...) + RuntimeTerminalStore │
└───────────────────────────────┬──────────────────────────────────────────────┘
                                │
                                v
┌──────────────────────────────────────────────────────────────────────────────┐
│  new render layer                                                           │
│  projection -> pane surface -> canvas compositor -> overlay -> HUD          │
└───────────────────────────────┬──────────────────────────────────────────────┘
                                │
                                v
┌──────────────────────────────────────────────────────────────────────────────┐
│  ANSI frame                                                                 │
│  最终输出到 Bubble Tea / terminal                                           │
└──────────────────────────────────────────────────────────────────────────────┘
```

---

## 2. 当前仓库中已经成型的层

### 2.1 Domain / State

主要落点：

- `tui/domain/types`
- `tui/domain/layout`
- `tui/domain/connection`
- `tui/domain/workspace`
- `tui/domain/terminalpicker`
- `tui/domain/terminalmanager`
- `tui/domain/layoutresolve`
- `tui/domain/prompt`

职责：

- 定义 `workspace / tab / pane / terminal / connection / overlay` 等纯状态
- 承载 layout、共享 terminal、picker/manager/prompt 的纯规则

这一层是当前新主线的核心资产，不应该跟着 renderer 一起删除。

### 2.2 Intent / Bubble Tea Shell

主要落点：

- `tui/bt/intent_mapper.go`
- `tui/bt/model.go`

职责：

- 把键盘、鼠标、runtime message 映射到 intent
- 作为最薄的一层 UI 壳，连接 mapper / reducer / effect handler / renderer

当前判断：

- 这层整体方向正确
- 后续只需要让鼠标命中语义跟新 renderer 的真实盒模型保持一致

### 2.3 Reducer / Effect Planning

主要落点：

- `tui/app/reducer/reducer.go`

职责：

- 纯状态迁移
- effect 产出
- overlay / focus / mode 流程协调
- terminal picker / manager / layout resolve / floating / prompt 等业务动作收敛

当前判断：

- 这一层是正确的
- 后续继续坚持“reducer 不直接调 runtime service，不直接管 render dirty/cache”

### 2.4 Runtime / Session / Update

主要落点：

- `tui/runtime.go`
- `tui/runtime_updates.go`
- `tui/runtime_session.go`
- `tui/runtime_terminal_store.go`
- `tui/runtime_terminal_service.go`
- `tui/runtime_terminal_input.go`
- `tui/bt/effect_handler.go`

职责：

1. startup planner
2. startup task executor
3. runtime session bootstrap
4. terminal store
5. stream / event / snapshot update
6. terminal input passthrough
7. effect 执行

当前判断：

- 这一层已经是新主线真正的底盘
- 后续 renderer 重写必须建立在这层之上，而不是重新发明一套 runtime

---

## 3. 当前需要被替换的层

当前明确应该被替换的是：

- `tui/runtime_renderer.go`
- `tui/runtime_modern_renderer.go`
- 与它们强绑定的旧文本结构和旧渲染测试基线

原因不是这些代码完全没价值，而是：

- 它们更像调试 shell，不像真正工作台
- 没有回到 `terminal-first / pane-first`
- 没有充分利用 legacy renderer 已验证过的 compositor 能力

所以这次重写的边界应当定义为：

- 保留 `domain + reducer + runtime + bt shell`
- 重写 `projection + pane surface + canvas compositor + overlay + HUD`

### 3.1 保留与替换边界图

```text
保留：

  domain
  reducer
  effect handler
  runtime session / update / terminal store / input
  bt shell

替换：

  runtime_renderer.go
  runtime_modern_renderer.go
  旧 screen_shell / chrome_* / section_* 输出结构
  旧 renderer 文本快照测试基线

边界：

  AppState + RuntimeTerminalStore
              │
              ├── 上游保留
              ▼
        New Renderer Boundary
              │
              ├── projection
              ├── pane surface
              ├── tiled/floating compositor
              ├── overlay compositor
              └── HUD
```

---

## 4. 最终目标分层

termx TUI 继续采用 6 层，但要明确哪些已存在、哪些待重写。

### 4.1 Intent Layer

职责：

- 输入事件 -> intent

当前状态：

- 已存在，保留

### 4.2 Application Layer

职责：

- reducer
- effect planning

当前状态：

- 已存在，保留

### 4.3 Domain Layer

职责：

- 纯状态
- 纯规则
- 纯布局规则

当前状态：

- 已存在，保留

### 4.4 Runtime Layer

职责：

- attach / snapshot / stream / resize / input
- effect 执行
- runtime update 回流

当前状态：

- 已存在，保留

### 4.5 Render Layer

职责：

- 把 `AppState + RuntimeTerminalStore` 变成真实可工作的 TUI 画面

当前状态：

- 待重写

### 4.6 Infrastructure Layer

职责：

- protocol client
- persistence
- benchmark
- e2e harness

当前状态：

- 保留并逐步增强

---

## 5. 新 Render Layer 的职责

新 renderer 不再只是“打印状态说明”，而应该完成下面 5 步。

### 5.1 Layout Projection

输入：

- `DomainState`
- 当前 active workspace / tab / pane

输出：

- tiled pane rect
- floating pane rect
- floating z-order
- active pane / active layer

这一层只负责几何投影，不直接处理 terminal 内容。

### 5.2 Terminal Surface Projection

输入：

- `RuntimeTerminalStore`
- pane rect
- pane 的 connect/owner/follower/slot 状态

输出：

- 某个 pane 内可显示的 terminal surface
- cursor
- waiting / unconnected / exited 的占位内容

这一层必须明确支持两条数据路径：

- live VTerm 路径
- snapshot 路径

### 5.3 Workbench Compositor

职责：

- 先画 tiled panes
- 再叠 floating panes
- 正确处理 overlap / clipping / z-order

这是真正的工作台主体。

### 5.4 Overlay Compositor

职责：

- mask / backdrop
- 居中 modal
- overlay focus target
- 关闭后无残影

overlay 只能盖在工作台上面，不能取代工作台。

### 5.5 HUD Chrome

职责：

- 最小 header
- 最小 footer
- status / notice / shortcut hint

chrome 只能承担导航和上下文表达，不能持续侵占 pane 主体面积。

---

## 6. 当前正确的数据流

后续必须沿这条链路推进：

```text
keyboard/mouse/runtime-msg
  -> IntentMapper
  -> Reducer
  -> Effects
  -> Runtime Executor / Runtime Update Handler
  -> AppState + RuntimeTerminalStore
  -> Projection Builder
  -> Workbench / Overlay / HUD Compositor
  -> ANSI Frame
```

强约束：

- reducer 不直接调 client
- renderer 不回写业务状态
- runtime update 不直接拼 UI
- dirty/cache 只留在 render/compositor 层

### 6.1 运行时数据流图

```text
keyboard / mouse
        │
        v
IntentMapper
        │
        v
Reducer ------------------------------┐
        │                             │
        ├── next AppState             │
        └── Effects                   │
                                       │
                                       v
                              RuntimeEffectHandler
                                       │
                                       v
                              TerminalService / Client
                                       │
                       ┌───────────────┴───────────────┐
                       │                               │
                       v                               v
               stream / snapshot                terminal events
                       │                               │
                       └───────────────┬───────────────┘
                                       v
                              RuntimeUpdateHandler
                                       │
                                       v
                              RuntimeTerminalStore
                                       │
                        ┌──────────────┴──────────────┐
                        │                             │
                        v                             v
                     AppState                  RuntimeTerminalStore
                        │                             │
                        └──────────────┬──────────────┘
                                       v
                                    Renderer
                                       │
                                       v
                                   ANSI Frame
```

---

## 7. 旧版 renderer 的可复用骨架

旧版真正可回收的不是大一统 `Model`，而是渲染骨架：

```text
View()
  -> renderTabBar()
  -> renderContentBody()
       -> renderTabComposite()
            -> paneRenderEntries()
                 -> LayoutNode.Rects()
                 -> visibleFloatingPanes()
            -> composedCanvas
                 -> drawPaneFrameWithTitle()
                 -> drawPaneBody()
                 -> drawCursor()
  -> renderStatus()
```

核心参考文件：

- `deprecated/tui-legacy/pkg/model.go`
- `deprecated/tui-legacy/pkg/render.go`
- `deprecated/tui-legacy/pkg/layout.go`

### 7.1 值得直接回收的函数职责

#### `renderTabComposite`

旧版真正的 compositor 入口。

它负责：

1. 收敛 tiled/floating 的统一渲染输入
2. 把 frame/body/cursor 合成到同一个 canvas
3. 尝试局部重绘

这条主链值得直接迁回新主线。

#### `paneRenderEntries`

旧版最有价值的抽象之一。

它把：

- layout projection
- floating order
- pane title/meta

统一变成渲染层输入。

新主线建议明确恢复类似结构，例如：

- `WorkbenchProjection`
- `ProjectedPane`
- `ProjectedOverlay`

#### `composedCanvas`

旧版基于二维 `drawCell` 做合成。

这带来几个直接收益：

- 支持局部覆盖
- 支持宽字符 continuation cell
- 支持行缓存和 ANSI 输出缓存
- 支持 overlap/damage redraw

这比当前过渡 renderer 直接输出说明文本更接近产品级 TUI。

#### `drawPaneBodyDirtyRows`

旧版已经支持 pane 级 dirty row / dirty col 增量重绘。

这个优化值得保留，但必须放在 render layer，而不是塞回领域模型。

#### `convertVTermViewport` / `convertProtocolViewport`

旧版 fixed viewport 有一个很好的优化：

- 直接从 live source / snapshot source 生成当前 viewport
- 避免先物化整张 full grid 再裁切

这对 floating、小窗口、共享 terminal 特别重要。

### 7.2 明确不回收的旧版部分

下面这些不要搬回新主线：

- 大一统 `Model`
- 业务状态、输入状态、渲染缓存混在一个对象里
- overlay / picker / manager / help 全部从一个 `View()` 分支硬拼
- 渲染缓存直接反向污染业务对象边界

---

## 8. 旧版做过的关键优化

旧版 renderer 不是单纯“能画出来”，它还做了不少值得复用的优化。

### 8.1 整页与行级缓存

旧版有：

- `renderCache`
- `tab.renderCache`
- `rowCache`
- `fullCache`

目的：

- overlay/picker/help 打开时优先复用已稳定画面
- row dirty 时避免整页重新编码 ANSI

### 8.2 Damage Redraw

旧版通过：

- rect 对比
- damage rect
- overlap 重画

来避免每次 floating 移动都全量重绘整个工作台。

### 8.3 Dirty Rows / Dirty Cols

旧版能追踪：

- `dirtyRowStart / dirtyRowEnd`
- `dirtyColStart / dirtyColEnd`

只刷 pane 内容真正变化的那部分区域。

### 8.4 Fixed Viewport Direct Render

旧版 fixed 模式不是先生成 full grid，而是直接生成目标 viewport。

这是新主线很值得回收的一个点。

### 8.5 ANSI / Title / Blank Row Cache

旧版还做了：

- style ANSI cache
- border title cell cache
- blank fill row cache

这些都属于后续性能优化时可直接借鉴的技术点。

### 8.6 宽字符与 continuation cell

旧版通过：

- `uniseg`
- continuation cell
- crop 逻辑

专门处理宽字符、组合字符和裁切边界。

这一点必须在新 renderer 一开始就保留，不然后面会反复踩坑。

---

## 9. 推荐的 render 子分层

这次 renderer 重写，建议内部至少拆成下面 4 层。

### 9.1 Projection

职责：

- `AppState -> WorkbenchProjection`

包含：

- tiled rect projection
- floating rect projection
- active/focus projection
- overlay projection

### 9.2 Surface

职责：

- `pane + runtime source -> PaneSurface`

包含：

- viewport crop
- cursor projection
- waiting/unconnected/exited body
- owner/follower/readonly/pin 元信息

### 9.3 Canvas / Compositor

职责：

- 把 projected panes / overlay / HUD 画进统一 canvas

建议包含：

- `drawCell`
- `Canvas`
- `DamageTracker`
- `ANSIEncoder`

### 9.4 Presenter

职责：

- 负责最终产品布局语言
- 负责 header/footer/modal 框架

它只决定“怎么呈现”，不应该承载业务状态。

### 9.5 Render Layer 内部结构图

```text
AppState + RuntimeTerminalStore
              │
              v
┌────────────────────────────────────────────────────────────────────┐
│ Projection                                                        │
│  tiled rects / floating rects / z-order / active focus / overlay │
└──────────────────────────────┬─────────────────────────────────────┘
                               │
                               v
┌────────────────────────────────────────────────────────────────────┐
│ Pane Surface                                                      │
│  live vterm / snapshot / cursor / waiting / unconnected / exited │
└──────────────────────────────┬─────────────────────────────────────┘
                               │
                               v
┌────────────────────────────────────────────────────────────────────┐
│ Canvas / Compositor                                               │
│  tiled first -> floating second -> overlap/clipping -> damage     │
└──────────────────────────────┬─────────────────────────────────────┘
                               │
                               v
┌────────────────────────────────────────────────────────────────────┐
│ Overlay Compositor                                                │
│  backdrop / modal / focus return / no residue                     │
└──────────────────────────────┬─────────────────────────────────────┘
                               │
                               v
┌────────────────────────────────────────────────────────────────────┐
│ HUD / Presenter                                                   │
│  header / footer / notices / shortcuts                            │
└──────────────────────────────┬─────────────────────────────────────┘
                               │
                               v
                           ANSI Encoder
                               │
                               v
                            Final Frame
```

---

## 10. 接口建议

为了不再次把 renderer、runtime、业务状态缠在一起，建议先固定接口。

### 10.1 Runtime 相关

- `TerminalSnapshotSource`
- `TerminalSessionSource`
- `TerminalControlService`

### 10.2 Render 相关

- `WorkbenchProjector`
- `PaneSurfaceProjector`
- `OverlayProjector`
- `CanvasComposer`
- `FrameRenderer`

### 10.3 当前仓库已存在的壳接口

- `bt.Renderer`
- `bt.MessageHandler`
- `bt.UnmappedKeyHandler`
- `reducer.StateReducer`

这些不需要推翻，继续沿用即可。

---

## 11. 目录建议

当前不强制立刻搬目录，但新的 renderer 至少应遵守下面职责边界：

- `tui/domain`
  - 纯领域状态和规则
- `tui/app`
  - reducer / intent / effect planning
- `tui/bt`
  - Bubble Tea 壳
- `tui/runtime_*`
  - runtime session / store / input / update / service
- `tui/render/*`
  - projection / surface / canvas / overlay / hud / ansi encoder

如果本轮不立即创建 `tui/render/`，也至少要按这些职责拆文件，不能再把新 renderer 全堆进 `runtime_renderer.go`。

---

## 12. 性能与稳定性顺序

性能重要，但必须后置在正确结构之后。

顺序固定为：

1. 先建立正确的工作台渲染结构
2. 再补 damage / row cache / style cache
3. 最后补 benchmark、backpressure、残影治理

可直接参考旧版、但应后置的优化包括：

- 行缓存
- full frame cache
- dirty rows / dirty cols
- fixed viewport direct render
- ANSI style cache
- blank row cache
- overlap damage redraw

这些优化都应该进入新的 render layer，不要渗漏回 reducer/domain。

---

## 13. 明确禁止

后续实现明确禁止：

- 再把 summary/card/rail 做成主工作台
- 用大量状态字段说明替代真实 pane 布局
- 让 overlay 长期主导首屏空间
- 为了守住旧测试而继续维护当前过渡 ASCII renderer
- 把旧版大一统 `Model` 直接搬回当前仓库

---

## 14. 当前正确的重写顺序

基于当前仓库状态，下一阶段顺序应固定为：

1. 文档继续收口到“保数据层、换 renderer”
2. 删除/替换当前过渡 renderer 实现
3. 建立 `projection -> composed canvas -> tiled/floating compositor`
4. 恢复 overlay 叠层
5. 按新 renderer 重建 E2E
6. 最后做颜色、性能、残影、backpressure

这就是当前 termx TUI 的正确技术主线。

### 14.1 legacy renderer 回收映射图

```text
legacy 可回收骨架                         新主线对应层
────────────────────────────────────────────────────────────────
renderTabComposite()                 -> Canvas / Compositor
paneRenderEntries()                  -> Projection
LayoutNode.Rects()                   -> Projection
drawPaneBody()/drawCursor()          -> Pane Surface
composedCanvas                       -> Canvas
damage redraw / dirty rows / cols    -> DamageTracker / incremental redraw
convertVTermViewport()               -> live surface projection
convertProtocolViewport()            -> snapshot surface projection
tab bar / status 的产品语言          -> HUD / Presenter

legacy 不回收：

  大一统 Model
  状态/输入/渲染缓存混在一起
  所有 overlay 都从单一 View() 硬分支
```
