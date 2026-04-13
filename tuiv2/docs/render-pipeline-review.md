# TUIv2 Render Pipeline Review

## 目的

这份文档用于给 `tuiv2` 当前渲染管线做一次结构化评审，内容包含：

- 当前实际渲染路径的流程图
- 当前管线的主要问题和风险
- 目标态渲染管线流程图
- 如果开始重构，建议的切入顺序和边界

本文基于当前代码路径整理，重点文件包括：

- `tuiv2/app/view.go`
- `tuiv2/app/render_state.go`
- `tuiv2/app/status_hints.go`
- `tuiv2/render/adapter.go`
- `tuiv2/render/coordinator.go`
- `tuiv2/render/pane_render_projection.go`
- `tuiv2/render/body_canvas_render.go`
- `tuiv2/render/body_render_cache.go`
- `tuiv2/render/overlay_surface.go`
- `tuiv2/render/overlays.go`
- `tuiv2/workbench/workbench.go`
- `tuiv2/runtime/runtime.go`

## 当前渲染管线

### 一句话总结

当前 `tuiv2` 的渲染不是简单的 `state -> render`，而是：

`domain state -> visible projection -> app 拼 UI 语义状态 -> coordinator 再做二次投影和缓存 -> body canvas -> overlay -> output writer`

优点是已经有纯读 projection 和 canvas cache 的雏形；问题是中间的 view-model 边界不干净，导致 `coordinator` 吞掉了过多职责。

### 当前流程图

```mermaid
flowchart TD
    A[Bubble Tea 调用 View()<br/>app/view.go] --> B[组装 visibleRenderState()<br/>app/render_state.go]
    B --> C[AdaptVisibleStateWithSize()<br/>render/adapter.go]
    C --> C1[Workbench.VisibleWithSize()<br/>workbench/workbench.go]
    C --> C2[Runtime.Visible()<br/>runtime/runtime.go]
    B --> B1[附加 UI 语义<br/>status hints / modal / terminal pool / copy mode]

    B1 --> D[Coordinator.RenderFrame / RenderFrameLines<br/>render/coordinator.go]

    D --> D0{frame cache 命中?}
    D0 -- 是 --> Z[复用 lastFrame / lastLines / lastCursor]
    D0 -- 否 --> E{surface 类型}

    E -- terminal pool --> E1[renderTerminalPoolPageWithCursor]
    E -- workbench --> F[选 active tab / 计算 bodyHeight / blink]

    F --> G[paneEntriesForTab()<br/>render/pane_render_projection.go]
    G --> H[buildPaneRenderEntry()<br/>标题 边框 snapshot surface copy-mode 等全部压平]

    H --> I[renderBodyCanvas()<br/>render/body_canvas_render.go]
    I --> I0{body cache 命中?}
    I0 -- 否 --> I1[rebuildBodyCanvas]
    I0 -- 是且无 overlap --> I2[按 pane frame/content key 增量刷新]
    I0 -- 是但有 overlap --> I3[整张 canvas 重建]

    I1 --> J[drawPaneFrame + drawPaneContent]
    I2 --> J
    I3 --> J

    J --> K[projectActiveEntryCursor]
    K --> L[body lines / body string]

    L --> M[render tab bar + status bar<br/>render/frame.go]
    M --> N[render overlay<br/>render/overlay_surface.go]
    N --> N0{overlay 存在?}
    N0 -- 否 --> O[拼 frame]
    N0 -- 是 --> N1[compositeOverlay()<br/>render/overlays.go]
    N1 --> O

    O --> P[Coordinator 缓存 frame/lines/cursor]
    P --> Q{输出路径}
    Q -- frameLinesWriter --> R[WriteFrameLines]
    Q -- frameWriter --> S[WriteFrame]
    Q -- 直接返回字符串 --> T[frame + cursor]

    R --> U[cursor_writer / output writer]
    S --> U
    T --> U
```

### 分层说明

#### 1. 领域只读投影层

- `workbench.VisibleWithSize()` 负责把 workspace/tab/pane/floating 状态投影成 render 可读结构。
- `runtime.Visible()` 负责把 terminal、binding、host palette 等 live state 投影成只读结构。

这一层总体方向是对的，已经接近纯读 projection。

#### 2. app 侧 UI 语义拼装层

`visibleRenderState()` 当前不仅组装 `Workbench` / `Runtime` 可见状态，还把这些 UI 细节揉进同一个 state：

- status hints
- overlay/modal host
- terminal pool surface
- owner confirm
- empty pane / exited pane selection
- copy mode
- pane snapshot override

这一步实际上已经是 view-model builder，但目前没有被明确建模成独立层。

#### 3. render 二次投影层

进入 `coordinator` 后，并没有直接 render，而是又做了一轮 projection：

- 算 active tab
- 算 body height 和 immersive zoom
- 算 overlay cursor blink
- 把 `VisiblePane` 再压成 `paneRenderEntry`
- 把 snapshot/surface/copy-mode/theme/selection 全部展开成 frame key 和 content key

这说明 render 输入的抽象层级还不稳定。

#### 4. body canvas 层

`renderBodyCanvas()` 和 `bodyRenderCache` 是当前实现里相对健康的一层：

- 有 pane frame key 和 content key
- 有 overlap / non-overlap 分支
- 有 sprite cache
- 有 active cursor 投影

问题不在这层的基本方向，而在它前面的输入太脏，导致 cache 无法建立在更稳定的 view-model 上。

#### 5. overlay / output 层

overlay 目前在 body render 之后统一覆盖。

对于全遮罩 modal，这条路径的语义没有问题，但成本偏高，因为底层 body 往往已经完整 render 了一遍，而后续 `compositeOverlay()` 直接返回 overlay。

## 当前主要问题

### 1. `VisibleRenderState` 太胖，render 输入边界不清

当前 render 输入不是一个干净的、稳定的 VM，而是一个混合了：

- 领域可见状态
- UI 交互状态
- overlay/surface 状态
- copy-mode 瞬态
- snapshot override
- status 文案

的超级 struct。

结果：

- render cache key 难以语义化
- 一个小 UI 细节变更也可能强行穿透整条渲染路径
- `coordinator` 不得不承担二次投影职责

### 2. cache 建得太靠后

`View()` 里的缓存命中并没有绕开前面的状态整理成本。

即使最终命中 `lastFrame` 或 `lastLines`，前面通常仍然会发生：

- `visibleRenderState()` 构建
- `AdaptVisibleStateWithSize()`
- status hints 生成
- state key 计算

这让 frame cache 更像“最后一公里字符串缓存”，而不是稳定的渲染输入缓存。

### 3. `coordinator` 职责过重

`coordinator.go` 现在同时承担：

- render 入口
- cache key
- frame cache
- line cache
- cursor blink
- overlay compose
- body render orchestration
- status/tab bar cache
- body entry projection 调度

问题不是文件长，而是概念边界已经塌在一起。

这会让后续每加一个 overlay、mode、selection 状态，都可能同时影响：

- render key
- body projection
- cursor blink
- status bar
- overlay cursor
- output path

### 4. render 管线出现双轨实现

`RenderFrame()` 和 `RenderFrameLines()` 维护的是两条高度相似的流程。

它们都需要处理：

- active surface
- body render
- overlay
- blink
- tab/status chrome
- cache 落地

这类双轨路径短期看只是重复代码，长期看会变成行为漂移源头。

### 5. pane render entry 构造参数爆炸

`buildPaneRenderEntry(...)` 现在已经接近“位置参数拼正确性”。

传入内容包括：

- active pane
- scroll offset
- confirm state
- empty pane selection
- exited pane selection
- copy mode
- snapshot override
- theme

这意味着 pane projection 已经不再是局部职责，而是整个 UI 瞬态的大杂烩。

### 6. overlay 现在没有 fast path

当前 overlay 即使是全屏遮罩，也是在 body render 之后才覆盖。

这条路径的问题不是错，而是浪费：

- body 先完整 render
- overlay 再完整 render
- 某些分支里 body 结果甚至不会被真正使用

如果 modal/overlay 在大量交互里高频出现，这会让热路径做很多无效工作。

### 7. 样式入口方向正确，但 fallback 仍有固定品牌色

`uiThemeFromHostColors()` 作为统一入口是正确方向，符合“host terminal theme + semantic accent”的大原则。

但 palette 缺失时，当前 fallback 仍会落到固定 indigo/green/yellow/red/blue。

这不是立即要改的最高优先级问题，但从架构约束看，它依旧是需要被显式 review 的一段。

## 我会保留什么

如果我来重构，不会先推倒这些层：

- `Workbench.VisibleWithSize()` 的只读 projection 基本方向
- `Runtime.Visible()` 的只读 projection 基本方向
- `renderBodyCanvas()` 的 canvas 组合思路
- `bodyRenderCache` / sprite cache 的总体模型
- host theme 驱动 `uiThemeFromHostColors()` 的入口位置

这些不是主要问题来源。

## 目标态渲染管线

### 目标原则

- `workbench` / `runtime` 只负责领域状态和 live state 的纯读投影
- `app` 或独立 `viewmodel` 层负责生产稳定的 `RenderVM`
- `render` 只消费 `RenderVM`，不再直接理解 `modal.*State` 或 binding catalog
- `coordinator` 只做调度、失效管理、blink、cache dispatch
- 输出路径只保留一条主渲染结果类型

### 目标流程图

```mermaid
flowchart TD
    A[Bubble Tea 调用 View()] --> B[Build RenderVM<br/>app/viewmodel or tuiv2/viewmodel]
    B --> B1[Workbench.VisibleWithSize]
    B --> B2[Runtime.Visible]
    B --> B3[UIState -> StatusVM / OverlayVM / SurfaceVM / CursorVM]

    B3 --> C[Coordinator.Render<br/>只负责调度和 cache]

    C --> C0{RenderVM key 命中?}
    C0 -- 是 --> Z[复用 RenderResult]
    C0 -- 否 --> D{Overlay 是否 opaque?}

    D -- 是 --> E[直接 render Overlay Surface]
    D -- 否 --> F[render Body Surface]

    F --> F1[BodyProjectionBuilder<br/>VisiblePane -> PaneBodyVM]
    F1 --> F2[BodyRenderer<br/>frame chrome + content sprite + cursor]
    F2 --> G[得到 BodyResult]

    E --> H[OverlayRenderer]
    G --> I{是否存在 overlay}
    I -- 否 --> J[ChromeComposer]
    I -- 是 --> K[OverlayRenderer + Composite]
    K --> J

    J --> L[RenderResult<br/>Lines + Cursor + Blink + Metrics]
    L --> M{输出适配}
    M --> N[WriteFrameLines]
    M --> O[WriteFrame]
    M --> P[Join lines for legacy path]
```

### 目标分层

#### 1. Projection layer

只保留纯读领域投影：

- `VisibleWorkbench`
- `VisibleRuntime`

不放 UI 瞬态修补逻辑。

#### 2. View-model layer

引入稳定的 `RenderVM`，建议最少拆成：

- `BodyVM`
- `StatusVM`
- `OverlayVM`
- `SurfaceVM`
- `CursorVM`

render 真正关心的不是 `modal.PromptState` 本身，而是：

- 当前 overlay kind
- overlay content model
- footer action model
- cursor target
- body pane list
- status hint tokens

#### 3. Render layer

render 只吃 VM，不再回看业务状态，不再理解输入绑定文档。

其中：

- body render 负责 pane surface 和 chrome
- overlay render 负责 modal 和 tree/picker surface
- chrome composer 负责 tab/status 外层壳

#### 4. Coordinator layer

`Coordinator` 只剩下这些职责：

- invalidate
- cache key
- cursor blink
- 调度 body / overlay / chrome renderer
- 产出统一 `RenderResult`

不再直接持有业务投影知识。

## 如果我来重构，我会怎么切

### Phase 0: 先补防回归测试

先固定这些行为：

- `RenderFrame()` 与 `RenderFrameLines()` 一致性
- overlay blink / cursor 行为
- copy mode pane chrome
- body cache hit / miss 条件
- overlap 与 non-overlap 的 body cache 行为
- status hints / overlay footer 的语义输出

否则后面每一步都只能靠肉眼对屏。

### Phase 1: 把 render 输入抽成 `RenderVM`

这是第一刀，也是最关键的一刀。

要做的不是拆文件，而是把现在的胖 `VisibleRenderState` 拆成显式 VM：

- `BuildRenderVM(workbenchVisible, runtimeVisible, uiState, transientState) RenderVM`

这一步完成后：

- `app/render_state.go` 会明显变薄
- `render/adapter.go` 不再承担超大 bag struct 的角色
- `coordinator` 不再需要理解这么多原始状态

### Phase 2: 把 status / overlay 文案建模从 render 热路径里拿掉

目前 `status hints` 虽然已经不在 render 包里生成，但它仍然跟 render state 耦合太深。

我会把这部分收敛成：

- `StatusVM`
- `OverlayActionVM`

render 只接收整理好的 token，不再理解：

- 当前 mode 下哪些 action 可见
- workspace picker 选中项是什么语义
- 哪个状态该不该显示 owner action

这些判断应该留在 view-model builder 层。

### Phase 3: 合并 `RenderFrame()` / `RenderFrameLines()` 双轨

统一只保留一个主输出：

```text
type RenderResult struct {
    Lines  []string
    Cursor string
    Blink  bool
}
```

字符串版输出只是适配层：

- `frame := strings.Join(result.Lines, "\n")`

这样可以消掉两条近似流程的漂移风险。

### Phase 4: 给 overlay 加 opaque fast path

如果 overlay 本身就是全遮罩，就不要先 render body 再覆盖。

这一步会直接改善：

- modal 高频交互的热路径成本
- current pipeline 中 overlay 白干活的问题

### Phase 5: 再拆 `coordinator.go`

只有在 VM 边界立住以后，拆文件才有意义。

我会按职责拆成这些文件：

- `coordinator.go`
- `coordinator_cache.go`
- `coordinator_cursor.go`
- `body_projection.go`
- `body_renderer.go`
- `overlay_renderer.go`
- `chrome_renderer.go`

而不是现在就把一团耦合平均分摊到多个文件里。

### Phase 6: 最后再 review 主题 fallback

等前面的结构稳定后，再处理：

- `uiThemeFromHostColors()` 的 fallback 策略
- modal/tree/preview 背景一致性
- terminal preview 空白背景回归 host default background

这属于视觉收口，不该抢在结构收口之前做。

## 建议的评审重点

如果要找人 review，这份文档建议 reviewer 重点看下面 5 件事：

1. 现在的 render 输入边界是否过胖，是否应该显式引入 `RenderVM`
2. `coordinator` 是否应该继续承担 projection 语义
3. overlay 是否应该增加 opaque fast path
4. `RenderFrame()` / `RenderFrameLines()` 是否应该合并成单一主路径
5. body cache 当前的 key 和失效边界是不是已经足够清晰

## 建议 reviewer 回答的问题

- 当前最值得优先动的第一刀是什么
- 哪些状态必须留在 app/viewmodel 层，不能继续漏进 render
- 当前 body cache 是否已经值得保留，还是应该重做
- overlay fast path 会不会引入新的 cursor / blink / hit-testing 风险
- `RenderVM` 的最小切分应该到什么粒度

---

## Code Review 补充（基于实际代码验证）

### 诊断确认

以下问题在代码里有直接证据：

**renderStateKey 是整个 app 瞬态的指纹**

`coordinator.go` 的 `renderStateKey` struct（约 80-111 行）直接把 `*modal.PromptState`、`*modal.PickerState`、copy mode 的所有字段全部平铺进 cache key。这意味着任何 UI 细节变动都会让 frame cache 失效，cache 实际上退化成了”最后一公里字符串缓冲”，而不是稳定的渲染输入缓存。

**statusBarCacheKey 知道的太多**

`coordinator.go` 的 `statusBarCacheKey` struct 里包含 `FloatingTotal`、`FloatingCollapsed`、`FloatingHidden`、`SelectedTreeSig`。这些不是 cache key 应该知道的东西，说明 coordinator 已经在”读懂业务”，而不是”调度渲染”。

**paneEntriesForTab 参数爆炸已经发生**

`pane_render_projection.go:64` 的函数签名已经有 13 个参数，其中最后传入了完整的 `state VisibleRenderState` 兜底，说明函数自己无法从局部参数拿到足够信息。

### Phase 顺序调整建议

**Phase 3（合并双轨）应该提前到 Phase 1 之前或同时进行。**

原因：合并 `RenderFrame` / `RenderFrameLines` 不依赖 VM 层完成。只需要引入：

```go
type RenderResult struct {
    Lines  []string
    Cursor string
    Blink  bool
}
```

字符串版只是适配层：

```go
frame := strings.Join(result.Lines, “\n”)
```

这一步改动最小、边界最清晰，同时为 VM 层提供**输出侧的稳定锚点**。VM 层的抽象从输入侧切，输出侧先稳住，可以降低 Phase 1 期间的双轨维护成本。

### VM Builder 签名警告

文档建议的签名：

```go
BuildRenderVM(workbenchVisible, runtimeVisible, uiState, transientState) RenderVM
```

`uiState` 和 `transientState` 这两个参数本身边界不清，实现时如果直接对应 app 侧的两个 bag struct，会把当前 `visibleRenderState()` 的胖逻辑平移到新位置，不是真正的分层。

更稳的做法：让每个 VM builder 只接受具体类型，边界由参数类型决定，不依靠 bag struct 兜底：

```go
BuildBodyVM(workbenchVisible VisibleWorkbench, copyMode CopyModeState, snapshotOverride SnapshotOverride) BodyVM
BuildOverlayVM(modalHost ModalHost) OverlayVM
BuildStatusVM(runtimeVisible VisibleRuntime, hints []StatusHint, inputMode string) StatusVM
BuildCursorVM(bodyVM BodyVM, overlayVM OverlayVM) CursorVM
```

这样如果某个 VM builder 的参数列表开始复胖，会立刻可见，不会悄悄退回大杂烩。

### Overlay Fast Path 的 Cursor 隐患

文档在 Phase 4 建议全遮罩 overlay 跳过 body render。逻辑正确，但有一个当前文档没有覆盖的边界：

**`projectActiveEntryCursor` 在 body render 之后才运行。**

如果 overlay opaque 时直接跳过 body render，cursor 的坐标来源需要有独立的 fast path。否则 overlay 内的输入框光标位置将来自上一帧 body 的残留值，在 overlay 初次出现时可能定位错误。

实现 Phase 4 时需要显式处理：overlay render 自己负责产出 `CursorVM`，不复用 body 的光标投影。

### Body Cache 的 Overlap 预计算边界

当前 `rebuildBodyCanvas` vs 增量刷新的 overlap/non-overlap 分支，在 render 层根据 pane rect 实时判断。

如果 `BodyVM` 里的 `PaneBodyVM` 不携带预计算的 `HasOverlap bool`，迁移后 `BodyRenderer` 仍然需要做 geometry 判断，职责会再次偏重。

建议：overlap 标记由 VM builder 负责计算，`BodyRenderer` 只消费，不自己判断几何关系。

### Phase 0 测试优先级补充

文档建议先固定 `RenderFrame` 与 `RenderFrameLines` 一致性。这是对的，但有一个更关键的先决条件：

**body cache 的 hit/miss 条件需要先有行为测试覆盖。**

原因：Phase 1 抽 VM 后，cache key 的结构会变化。如果 hit/miss 行为没有测试覆盖，你无法区分”cache key 变了但行为等价”和”cache key 变了且行为漂移”这两种情况，只能靠肉眼对屏。这比渲染一致性测试更优先。

---

## 结论

当前 `tuiv2` 渲染管线的问题，不是”没有 renderer”，而是”缺一层干净的 render view-model”。

底层 canvas/cache 思路并不差，真正拖累可维护性的，是中间状态边界塌陷，导致：

- `coordinator` 过胖，同时承担 projection 语义和调度职责
- cache key 等价于整个 app 瞬态指纹，cache 命中率极低
- overlay 和 body 互相拖累，全遮罩 modal 仍触发完整 body render
- 小 UI 状态变化容易穿透整条热路径

所以重构顺序应该是：

1. **先合并双轨输出**（`RenderFrame` / `RenderFrameLines` → `RenderResult`），稳住输出侧锚点
2. **再补 body cache hit/miss 行为测试**，在 key 结构变化前固定基准
3. **再立 render 输入边界**（`RenderVM` 拆成具体类型，不用 bag struct）
4. **再补 overlay fast path**，同时解决 cursor 独立投影问题
5. **最后拆 coordinator 和样式收口**

不要反过来先拆文件，否则大概率只是把耦合搬家。
