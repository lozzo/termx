# termx TUI 架构设计

状态：Draft v3
日期：2026-03-25

## 1. 核心原则

当前架构只做一件事：

- 保留当前数据驱动层
- 重写当前 renderer

## 2. 总体架构图

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
│  AppState + RuntimeTerminalStore                                            │
└───────────────────────────────┬──────────────────────────────────────────────┘
                                │
                                v
┌──────────────────────────────────────────────────────────────────────────────┐
│  new render layer                                                           │
│  projection -> surface -> compositor -> overlay -> HUD                      │
└───────────────────────────────┬──────────────────────────────────────────────┘
                                │
                                v
┌──────────────────────────────────────────────────────────────────────────────┐
│  ANSI Frame                                                                 │
└──────────────────────────────────────────────────────────────────────────────┘
```

## 3. 当前仓库中保留的层

### Domain / State

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

- 纯状态
- 纯规则
- 纯布局规则

### Reducer / Effect Planning

主要落点：

- `tui/app/reducer/reducer.go`

职责：

- 纯状态迁移
- effect 产出
- overlay / focus / mode 协调

### Bubble Tea 壳

主要落点：

- `tui/bt/model.go`
- `tui/bt/intent_mapper.go`

职责：

- 输入归一化
- 驱动 reducer / effect handler / renderer

### Runtime

主要落点：

- `tui/runtime.go`
- `tui/runtime_updates.go`
- `tui/runtime_session.go`
- `tui/runtime_terminal_store.go`
- `tui/runtime_terminal_service.go`
- `tui/runtime_terminal_input.go`
- `tui/bt/effect_handler.go`

职责：

- startup
- attach
- snapshot / stream / event update
- terminal input/output/resize

## 4. 当前替换边界

当前要替换的是：

- `tui/runtime_renderer.go`
- `tui/runtime_modern_renderer.go`
- 与它们强绑定的旧文本结构

```text
保留：

  domain
  reducer
  bt shell
  runtime session / update / store / input

替换：

  old renderer
  old textual shell structure
  old renderer snapshot assumptions
```

## 5. 新 render layer 的内部结构

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

## 6. 运行时数据流

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
```

## 7. legacy renderer 里值得回收的骨架

旧版真正可回收的不是大一统 `Model`，而是这条骨架：

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

## 8. legacy -> 新主线映射

```text
legacy 可回收骨架                         新主线对应层
────────────────────────────────────────────────────────────────
renderTabComposite()                 -> Canvas / Compositor
paneRenderEntries()                  -> Projection
LayoutNode.Rects()                   -> Projection
drawPaneBody()/drawCursor()          -> Pane Surface
composedCanvas                       -> Canvas
damage redraw / dirty rows / cols    -> incremental redraw
convertVTermViewport()               -> live surface projection
convertProtocolViewport()            -> snapshot surface projection
```

## 9. 明确不回收的部分

- legacy 的大一统 `Model`
- 状态、输入、渲染缓存强耦合
- 所有 overlay 从单一 `View()` 分支硬拼

## 10. 当前正确的实现顺序

1. 删/换当前过渡 renderer
2. 建立 `projection -> surface -> canvas compositor`
3. 恢复 floating
4. 恢复 overlay
5. 回补新 E2E
6. 最后再做性能、颜色、残影治理
