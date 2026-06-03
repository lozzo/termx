# termx-tui-v3 架构设计

## 1. 背景

`tuiv2/` 已经包含大量可用能力：输入路由、runtime bridge、render pipeline、pane/workbench layout、modal、session restore、clipboard、terminal attach/resize 等。TUI-v3 不应该把这些能力全部推倒重写。

但 `tuiv2/app` 的核心问题是边界过度混合：单个 `Model` 同时持有 workspace、runtime、render、history store、copy mode、modal、clipboard、session、副作用队列、鼠标拖拽、terminal input dispatch 和各种 invalidate 状态。大量 `update_*` 文件通过共享 model 字段协作，导致历史、copy mode、live surface、render 和副作用互相穿透。

TUI-v3 的重构目标不是“功能全部重新发明”，而是：

- 沿用 tuiv2 中已经稳定的能力和行为经验。
- 重建模块边界，让 app shell、state、services、render、history、copy mode 各自有明确职责。
- 把唯一必须改的历史/copy mode 路径设计成 core-v2 authoritative history window 的消费者。
- 避免继续复制 tuiv2 的单体 app model 和 snapshot/grid viewport history fallback。

## 2. 设计目标

- `termx-tui-v3` 不拥有 committed history truth。
- copy mode、鼠标滚轮、page up/down、older prepend、latest replace、stale response guard 都围绕 core-v2 `HistoryWindow` 工作。
- 普通实时终端显示可以继续消费 live surface snapshot/grid viewport。
- TUI 内部状态和副作用分离：state reducer 不做 IO，service/effect 不直接绕过 message path 修改 UI state。
- renderer 只消费 render view-model，不读取 runtime、history source 或 protocol client。
- input 和 mouse 只输出 semantic intent，不直接修改 workspace/history/copy mode。
- 可以从 tuiv2 搬迁小而稳定的包，但迁入 v3 后必须去掉对 `tuiv2/` 的运行时依赖。

## 3. 总体架构图

```text
                              core-v2
              live surface / history.window / terminal control
                                  |
                                  v
                       +---------------------+
                       | CoreClient Adapter  |
                       +---------------------+
                                  |
+------------------------------------------------------------------+
| termx-tui-v3                                                     |
|                                                                  |
|  +----------------+      +----------------+      +-------------+ |
|  | BubbleTea Host | ---> | AppShell       | ---> | EffectRunner| |
|  +----------------+      +----------------+      +-------------+ |
|                                |                         |        |
|                                v                         v        |
|                        +---------------+        +----------------+ |
|                        | MessageRouter | <----- | Service Msgs   | |
|                        +---------------+        +----------------+ |
|                                |                                  |
|                                v                                  |
|  +-------------------------------------------------------------+ |
|  | StateRoot                                                   | |
|  |                                                             | |
|  | WorkspaceStore   PaneStore       ModalStore      Session    | |
|  | TerminalSurface HistoryStore    CopyModeStore   Clipboard   | |
|  +-------------------------------------------------------------+ |
|                                |                                  |
|                                v                                  |
|                        +---------------+                         |
|                        | RenderVMBuilder |                       |
|                        +---------------+                         |
|                                |                                  |
|                                v                                  |
|                          +----------+                             |
|                          | Renderer |                             |
|                          +----------+                             |
+------------------------------------------------------------------+
```

核心原则：

- `AppShell` 只负责 Bubble Tea 生命周期、消息分发和 effect 调度。
- `StateRoot` 是唯一 UI state 容器。
- `Service` 只通过 message 返回结果，不直接改 `StateRoot`。
- `RenderVMBuilder` 从 `StateRoot` 生成不可变 view-model。
- `Renderer` 只画 view-model，不知道 core client、history source 或 runtime service。

## 4. 模块图

```text
app/
  shell             Bubble Tea Model、Init/Update/View 边界
  messages          跨模块消息类型
  effects           Effect 类型、EffectRunner、异步命令包装

state/
  root              StateRoot 与 reducer 协调
  workspace         tabs、panes、layout、active pane
  terminalsurface   当前实时 terminal surface，非历史 truth
  historyview       core authoritative HistoryWindow state
  copymode          copy mode cursor、viewport、selection
  modal             picker、prompt、terminal manager state
  session           session restore/save 所需 UI 状态

services/
  coreclient        protocol/core-v2 adapter
  terminal          外部 runtime service：attach、input、resize、restart、ownership、event stream
  history           latest/older request IO，返回 response message，不做 stale 接纳
  session           load/save/restore
  clipboard         yank、clipboard history
  host              emoji probe、theme probe、terminal capability

input/
  keymap            key binding catalog
  router            key -> semantic intent / terminal input
  mouse             mouse event -> semantic intent

render/
  viewmodel         StateRoot -> RenderVM
  renderer          RenderVM -> frame
  hitregions        frame hit region metadata

bridge/
  protocol          termx protocol mapping
  fake              harness fake source/client
```

## 5. tuiv2 可复用策略

### 5.1 可迁移能力

这些能力可以作为 v3 的迁移来源，但迁入后必须放进新的边界：

- `tuiv2/input` 的 keymap、mode、router、terminal input translation。
- `tuiv2/render` 的 canvas、compositor、pane chrome、hit regions、theme、glyph、render cache 思路。
- `tuiv2/historyview` 的 authoritative window contract、source adapter 思路和 stale guard 测试。v3 不照搬其带 mutex 的可变 store。
- `tuiv2/runtime` 的 terminal registry、pane binding、live surface adapter、terminal input/resize/attach 经验。v3 的 terminal process handle、event stream 和 IO 必须归外部 runtime service。
- `tuiv2/bootstrap`、`sessionstore`、`workbench`、`modal`、`uiinput`、clipboard 相关能力。
- tuiv2 中已经覆盖的行为 harness，特别是 input、render、historyview、copy selection、mouse wheel 和 resize 场景。

### 5.2 不直接沿用的结构

下面结构不迁移为 v3 主结构：

- `tuiv2/app.Model` 的大状态对象。
- 通过共享 model 字段互相耦合的 `update_*` 文件结构。
- 带 mutex、可被 service 直接调用修改的 UI store。
- copy mode 读取 snapshot/local scrollback 的历史路径。
- render 层从 snapshot/grid viewport 推断 history truth 的路径。
- app 层本地 committed history depth、local loading depth、local exhausted truth。
- mouse wheel/page up/page down 中任何 snapshot totals、LoadedRows、row count fallback。
- runtime/local VTerm scrollback 作为 history source 的路径。

### 5.3 迁移方式

- 可以复制 tuiv2 中小而稳定的包到 v3，再按 v3 边界改名和裁剪依赖。
- 不允许 v3 运行时长期 import `tuiv2/` 作为内部依赖。
- 每个迁移包必须有自己的 v3 harness，不以 tuiv2 测试语义自动作为回归基准。
- 如果迁移代码携带旧 history 语义，必须先删除旧语义再进入 v3。
- store 迁移只能迁移数据结构和校验语义，不能迁移“外部对象持有指针并直接 Apply”的可变调用模式；v3 store 必须由 reducer 持有和更新。

## 6. 核心状态模型

### 6.1 StateRoot

`StateRoot` 是 TUI-v3 的唯一 UI state。

至少包含：

- `WorkspaceStore`
- `PaneStore`
- `TerminalSurfaceStore`
- `HistoryStore`
- `CopyModeStore`
- `ModalStore`
- `SessionState`
- `ClipboardState`
- `HostState`

`StateRoot` 不保存 protocol client、goroutine handle、terminal process handle 或 renderer cache。

`StateRoot` 只保存 terminal id、pane binding、surface snapshot、运行状态标记和请求状态。terminal process handle、event stream subscription、protocol client、resize/input IO 都属于 runtime service 或 core client adapter。

### 6.2 TerminalSurfaceStore

`TerminalSurfaceStore` 只保存实时显示所需状态。

- 可以保存 core-v2 live surface、snapshot、grid viewport 或 vterm surface。
- 可以服务普通 terminal renderer。
- 不得成为 copy mode/history path 的 committed history source。
- 不得向 `HistoryStore` 提供 rows 让其反推出 logical line。

### 6.3 HistoryStore

`HistoryStore` 是 reducer-owned 的纯状态，保存 core-v2 返回的 authoritative history window、请求状态和 exhausted marker。它不保存 copy mode 交互态。

至少包含：

- terminal id
- core window token
- generation
- rows
- logical line spans
- stable logical line ids
- current older cursor / before cursor
- first/last logical boundary
- has more
- pending local request id
- pending request kind：latest 或 older
- pending request cursor / boundary / cols
- exhausted older marker

`HistoryStore` 不保存 viewport top、copy cursor、selection anchor/focus 或 auto-scroll 状态；这些都属于 `CopyModeStore`。

必须区分两个 token：

- local request id：TUI 为每次 history 请求生成，只用于把 async response 关联回当前 pending request。
- core window token：core-v2 返回的 authoritative window token，用于后续 older request 和 stale guard。

history service 只能发起请求并把 response 映射成 message，不能在 service 层决定 stale 接纳。latest/older response 的接纳必须回到 reducer，由 `HistoryStore` 使用 local request id、core window token、generation、current older cursor、first/last logical boundary、cols 校验。

`exhausted older marker` 只能绑定 core response、local request id、请求 cursor、core window token 和 cols；它不是本地推断的 history exhausted truth。

### 6.4 CopyModeStore

`CopyModeStore` 只保存 copy mode 的交互态。

- active pane id
- terminal id
- cursor
- mark
- viewport top
- selection anchor/focus
- auto-scroll state
- bound core window token
- bound cols
- pending/empty display state

Copy mode 不读取 local VTerm scrollback，不用 wrapped rows 拼 logical line，不维护本地 committed depth。

## 7. 消息和副作用架构

```text
input/mouse/protocol msg
        |
        v
 MessageRouter
        |
        v
 Reducer(StateRoot, Msg) -> StateRoot + Effects
        |
        v
 EffectRunner -> services/coreclient/clipboard/session/host
        |
        v
 result Msg
```

规则：

- reducer 只做同步 state transition。
- service IO 必须通过 effect 描述。
- effect result 必须回到 message path。
- renderer invalidate 是 effect 或 shell 级调度，不允许 service 直接调用 renderer。
- terminal input、history request、session save、clipboard IO 都属于 effect。

## 8. 历史和 copy mode 流程图

### 8.1 进入 copy mode

```text
wheel up / page up
        |
        v
InputRouter -> IntentEnterCopyMode
        |
        v
Reducer: set CopyMode pending, create local request id
        |
        v
HistoryService.Latest(terminal, local request id, cols, limit)
        |
        v
core-v2 HistoryWindow replace
        |
        v
Reducer receives response; HistoryStore validates request id / op / generation / cols
        |
        v
HistoryStore stores core window token; CopyModeStore binds core token + cols + viewport/cursor
        |
        v
RenderVMBuilder uses HistoryStore + CopyModeStore projection
```

### 8.2 older prepend

```text
copy mode viewport at top
        |
        v
IntentRequestOlder
        |
        v
HistoryService.Older(local request id, core window token, cursor, boundary, cols)
        |
        v
core-v2 HistoryWindow prepend
        |
        v
Reducer receives response; HistoryStore validates request id/token/generation/cursor/boundary/cols
        |
        v
prepend rows; CopyModeStore viewport top adjusted by inserted row count
```

### 8.3 selection

```text
cursor/mark visual position
        |
        v
HistoryStore row -> authoritative line span
        |
        v
CopyMode selection model
        |
        v
copy text assembled by logical line spans
```

clipped span 不能被当作完整 logical line。相邻片段只有在 stable logical line id 和 clipping 关系连续时才能拼接。

## 9. Render 架构

Renderer 分两层：

- `RenderVMBuilder`：从 `StateRoot` 构建纯 view-model。
- `Renderer`：把 view-model 渲染成 frame 和 hit region metadata。

RenderVM 至少包含：

- workspace layout
- pane chrome
- active pane
- terminal live surface projection
- copy mode history projection
- modal overlay
- status bar
- cursor state
- hit regions

copy mode VM 只能由 `HistoryStore + CopyModeStore` 生成。只有 `CopyModeStore` 的 terminal id、bound core window token 与 bound cols 同 `HistoryStore` 当前窗口一致时，RenderVMBuilder 才能生成 copy mode history VM。缺少 authoritative history window 或绑定不一致时，只能渲染 pending、empty 或 error 状态，不得从 `TerminalSurfaceStore`、snapshot、grid viewport、local VTerm scrollback fallback 生成 copy mode 内容。

renderer 禁止：

- 读取 core client。
- 请求 history window。
- 从 snapshot/grid viewport 推断 copy mode history。
- 修改 `StateRoot`。

## 10. 与 core-v2 的接口

TUI-v3 只通过 `CoreClient` 访问 core-v2。

`CoreClient` 至少提供：

- live surface latest
- history latest window
- history older window
- terminal input
- terminal resize
- terminal attach / detach / restart
- title / metadata event stream

history response 的接纳规则：

- latest 必须是 replace。
- older 必须是 prepend。
- op 由 core-v2 决定。
- stale guard 只能使用 core window token、generation、cursor、logical boundary。
- 空 older exhausted 必须绑定 local request id、core window token、请求 cursor 和 cols。

resize 接纳规则：

- TUI 必须记录 history request 使用的 cols。
- terminal cols 改变后，当前 history window、core window token、copy selection 和 pending older request 都必须失效。
- resize 后 TUI 不允许本地 reflow 旧 history rows。
- resize 后 copy mode 如仍保持打开，只能进入 pending/empty 状态并请求 core-v2 latest replace。
- 旧 cols 的 latest/older response 必须拒绝。
- page up/down 在等待新 latest replace 时不得从 live surface 或旧 window 推断历史。

## 11. 包边界硬约束

- `historyview` 不 import `render`、`runtime`、`app`。
- `copymode` 不 import protocol client、runtime 或 renderer。
- `render` 不 import services/coreclient。
- `services` 不 import renderer。
- `input` 不 import app state。
- `app` 可以组合各包，但不得持有所有业务细节字段。
- `TerminalSurfaceStore` 与 `HistoryStore` 不能互相反推数据。
- runtime service 不持有或修改 `StateRoot`；它只通过 event/result message 反馈 attach、resize、restart、surface update 和 title/metadata 变化。
- history service 不持有 `HistoryStore`；它只返回 response message。

## 12. 测试策略

优先用小 harness 固定边界，再做 e2e。

- input harness：key/mouse -> semantic intent。
- reducer harness：message -> state + effects。
- historyview harness：latest replace、older prepend、empty older exhausted、stale response、boundary overlap、local request id 与 core window token 分离、cols mismatch 拒绝。
- copymode harness：cursor、viewport、selection、clipped span、multi logical line copy。
- render VM harness：live mode 与 copy mode projection 分流、copy mode 缺 window 时不从 live surface fallback。
- service fake harness：core response 映射、local request id、error cleanup。
- runtime service harness：attach、resize、restart、event stream 只回 message，不直接改 state。
- integration harness：wheel up 进入 copy mode、page up 请求 older、resize 后 latest replace、旧 cols response 被拒绝。

tuiv2 测试可以作为行为参考，但不得把旧 snapshot/local scrollback 语义带进 v3。

## 13. 推荐落地顺序

1. 建立 `termx-tui-v3/` Go module、包目录和基础测试框架。
2. 迁入或重写 `input`，只输出 semantic intent / terminal input。
3. 建立 `StateRoot`、message、effect、reducer、EffectRunner 骨架。
4. 实现 reducer-owned `historyview` 状态与 fake source harness。
5. 实现 `copymode` 状态机和 selection harness。
6. 建立 `RenderVMBuilder`，先用 fake state 渲染 live/copy 两种 projection。
7. 迁移 render primitives、pane chrome、hit regions 和 render cache。
8. 建立 `CoreClient` fake adapter，接入 history latest/older。
9. 建立 runtime service fake，接入 terminal surface/update/resize/restart message。
10. 接入 workspace/pane layout、modal、clipboard、session restore。
11. 接入真实 protocol adapter、terminal input、resize、attach/restart。
12. 补最小端到端 harness。

每个切片都必须避免引入 local scrollback history fallback。

## 14. 第一阶段范围

第一阶段只做：

- v3 module skeleton
- package boundaries
- reducer-owned `historyview` state
- fake `historyview.Source`
- `copymode` state skeleton
- reducer/effect harness
- latest replace harness
- older prepend harness
- stale response harness
- selection clipped span harness

第一阶段不做：

- Bubble Tea app 完整接入
- 真实 protocol adapter
- 旧 `tuiv2/` 原地修补
- local VTerm scrollback 迁移
- render 全量迁移
