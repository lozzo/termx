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
- TUI-v3 不以 Bubble Tea 作为主运行时；消息循环、effect 调度、终端输入、终端模式和最终 frame 输出都由 v3 自己的 runtime/terminal host 管理。
- 可以使用 `lipgloss/v2`、`x/ansi` 这类纯渲染、样式、ANSI 辅助库，但不得引入绑定 Bubble Tea `Model/Msg/Cmd` contract 的 UI 组件作为主结构。
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
|  | AppRuntime     | ---> | AppShell       | ---> | EffectRunner| |
|  +----------------+      +----------------+      +-------------+ |
|          |                     |                         |        |
|          v                     v                         v        |
|  +----------------+    +---------------+        +----------------+ |
|  | TerminalHost   | -> | MessageRouter | <----- | Service Msgs   | |
|  +----------------+    +---------------+        +----------------+ |
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
|                        +---------------+                         |
|                        | Renderer      |                         |
|                        +---------------+                         |
|                                | frame                            |
|                                v                                  |
|                     +----------------------+                      |
|                     | TerminalHost FrameSink |                    |
|                     +----------------------+                      |
+------------------------------------------------------------------+
```

核心原则：

- `AppRuntime` 负责单线程消息循环、effect result 回投、timer、batch、cancel 和退出生命周期。
- `TerminalHost` 负责宿主 TTY raw mode、stdin event stream、terminal capability probe、alt-screen、mouse、bracketed paste 和 stdout frame sink。
- `AppShell` 只组合 runtime、message router、reducer、effect runner 和 render 调度，不持有所有业务细节。
- `StateRoot` 是唯一 UI state 容器。
- `Service` 只通过 message 返回结果，不直接改 `StateRoot`。
- `RenderVMBuilder` 从 `StateRoot` 生成不可变 view-model。
- `Renderer` 只画 view-model，不知道 core client、history source 或 terminal service；`FrameSink` 是 render 侧输出接口，由 `TerminalHost` 提供真实 TTY 实现。
- Bubble Tea 只能作为旧 `tuiv2/` 行为参考，不作为 TUI-v3 主线依赖。

## 4. 模块图

```text
app/
  runtime           v3 自有事件循环、message queue、timer、batch、cancel、quit
  shell             runtime、router、reducer、effect runner、render 调度组合边界
  messages          跨模块消息类型，不依赖 Bubble Tea Msg/Cmd
  effects           Effect 类型、EffectRunner、异步副作用包装

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
  terminal          外部终端进程/core service：attach、terminal input、resize、restart、ownership、surface/title event stream
  history           latest/older request IO，返回 response message，不做 stale 接纳
  session           load/save/restore
  clipboard         yank、clipboard history

terminalhost/
  input             raw stdin、UV/xterm event -> v3 InputEvent
  output            direct terminal enter/exit、FrameSink 实现、cursor/mouse/bracketed paste
  capability        host theme、emoji、width、palette probe

input/
  keymap            key binding catalog
  router            key -> semantic intent / terminal input
  mouse             mouse event -> semantic intent

render/
  viewmodel         StateRoot -> RenderVM
  renderer          RenderVM -> frame
  framesink         frame 输出接口；TerminalHost、非 TTY、测试实现该接口
  style             lipgloss/v2 纯样式 helper、主题 token、ANSI 宽度 helper
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
- `tuiv2/runtime` 的 terminal registry、pane binding、live surface adapter、terminal input/resize/attach 经验。v3 的 terminal process handle、event stream 和 IO 必须归外部 terminal service。
- `tuiv2/bootstrap`、`sessionstore`、`workbench`、`modal`、`uiinput`、clipboard 相关能力。
- tuiv2 中已经覆盖的行为 harness，特别是 input、render、historyview、copy selection、mouse wheel 和 resize 场景。

### 5.2 不直接沿用的结构

下面结构不迁移为 v3 主结构：

- `tuiv2/app.Model` 的大状态对象。
- `tuiv2/app` 中以 Bubble Tea `Model/Msg/Cmd` 为中心的 host/runtime 结构。
- `tuiv2/input` 中对 `tea.KeyMsg`、`tea.MouseMsg` 的直接类型依赖；v3 必须改为自己的 `InputEvent`。
- 通过共享 model 字段互相耦合的 `update_*` 文件结构。
- 带 mutex、可被 service 直接调用修改的 UI store。
- copy mode 读取 snapshot/local scrollback 的历史路径。
- render 层从 snapshot/grid viewport 推断 history truth 的路径。
- app 层本地 committed history depth、local loading depth、local exhausted truth。
- mouse wheel/page up/page down 中任何 snapshot totals、LoadedRows、row count fallback。
- runtime/local VTerm scrollback 作为 history source 的路径。
- 任何把 Bubble Tea renderer、Bubble Tea `Cmd` 或 `bubbles` 组件作为 v3 主线 contract 的结构。

### 5.3 迁移方式

- 可以复制 tuiv2 中小而稳定的包到 v3，再按 v3 边界改名和裁剪依赖。
- 不允许 v3 运行时长期 import `tuiv2/` 作为内部依赖。
- 每个迁移包必须有自己的 v3 harness，不以 tuiv2 测试语义自动作为回归基准。
- 如果迁移代码携带旧 history 语义，必须先删除旧语义再进入 v3。
- store 迁移只能迁移数据结构和校验语义，不能迁移“外部对象持有指针并直接 Apply”的可变调用模式；v3 store 必须由 reducer 持有和更新。
- 如果迁移代码携带 `tea.Msg`、`tea.Cmd`、`tea.Model`、`tea.KeyMsg` 或 `tea.MouseMsg`，必须在迁入时替换为 v3 自有 message、effect 和 input event 类型。

## 6. 运行时与终端主机

TUI-v3 使用自有 `AppRuntime`，不使用 Bubble Tea `Program` 作为主运行时。

### 6.1 AppRuntime

`AppRuntime` 是轻量事件运行时，职责限定为：

- 维护单线程 message queue。
- 串行执行 `Reducer(StateRoot, Msg) -> StateRoot + Effects`。
- 接收 service、terminal host、timer 回投的 result message。
- 调度 batch、delay、interval、cancel token 和 quit。
- 在 state 或 host event 需要重绘时触发 render pass。
- 保证 reducer 同一时刻只有一个调用栈。

`AppRuntime` 不做业务决策，不读写 history，不直接访问 protocol client，不直接拼 frame。

### 6.2 EffectRunner

`EffectRunner` 替代 Bubble Tea `Cmd`。

规则：

- `Effect` 是 v3 自有类型，不是函数闭包随处捕获 `StateRoot`。
- effect 可以调用 service IO，但只能通过 result message 回到 `AppRuntime`。
- effect 必须支持 context/cancel，至少能取消 pending history request、terminal operation 和 timer。
- batch 只是多个 effect 的调度组合，不改变 reducer 纯同步边界。
- timer/interval 必须产出普通 message，不能直接改 state。

### 6.3 TerminalHost

`TerminalHost` 负责宿主 TTY 边界，不负责远端/core 终端进程的 attach、resize、restart 或 terminal input IO：

- raw mode enter/restore。
- alt-screen enter/exit。
- cursor hide/show。
- bracketed paste enable/disable。
- mouse cell/SGR mode enable/disable。
- stdin event stream。
- terminal capability/theme/emoji/palette probe。
- stdout `FrameSink` 实现。

输入侧必须把宿主 TTY 的 UV/xterm/其他 reader 事件转换成 v3 自有 `InputEvent`，再进入 `MessageRouter`；不得把 Bubble Tea key/mouse 类型泄漏进 `input`、`state` 或 `render`。

当前宿主 theme/palette probe 已按 reducer-owned capability 路径落地：真实 `TerminalHost` 进入 TUI 后发送 OSC 10/11/4 查询，`InputParser` 消费 OSC response 并产生 host theme event；`AppRuntime` 把它转换为 `HostThemeMsg`，由 reducer 更新 `StateRoot.HostTheme`。这些响应不得作为普通 terminal input 透传给 core-v2 terminal。

输出侧必须通过 `Renderer -> Frame -> FrameSink`，不得交给 Bubble Tea standard renderer。`FrameSink` contract 定义在 render 输出边界，真实 TTY 实现在 `TerminalHost`，非 TTY、测试和录制场景可以使用不同实现，但 contract 相同。

职责边界：

- 宿主 TTY 输入是用户按键、鼠标、粘贴、终端能力回报，归 `TerminalHost`。
- terminal input 是写给 core/terminal 进程的输入字节或控制请求，归 terminal service。
- 宿主 theme/palette 只推导 TermX chrome theme；PTY live 内容中的 `ansi:N`、`idx:N` 和 truecolor 仍作为 terminal 内容 SGR 语义直通，不重新映射为 semantic token。

## 7. UI 组件与第三方库边界

TUI-v3 可以使用纯渲染、纯样式、ANSI 辅助库；不得使用拥有事件循环、状态更新 contract 或 Bubble Tea `Model/Msg/Cmd` 绑定的 UI 组件作为主线结构。

允许：

- `charm.land/lipgloss/v2`：颜色、样式、border、padding、join、place、width、truncate 等纯字符串渲染能力。
- `github.com/charmbracelet/x/ansi`：ANSI strip、宽度、cursor/control sequence 辅助。
- `github.com/charmbracelet/ultraviolet`：作为 terminal input/output primitive 使用，必须隔离在 `TerminalHost` 或 `FrameSink` 内。
- tuiv2 中 picker、modal、prompt、workspace tree 的视觉设计、样式 token、render helper 思路。

禁止：

- 引入 Bubble Tea `Program` 作为 v3 主运行时。
- 引入 Bubble Tea `standardRenderer` 作为最终输出路径。
- 在 v3 主线类型中暴露 `tea.Model`、`tea.Msg`、`tea.Cmd`、`tea.KeyMsg`、`tea.MouseMsg`。
- 直接复用依赖 Bubble Tea contract 的 `bubbles` 组件。
- 让 UI 组件持有自己的业务 truth 并绕过 `StateRoot` 更新。

迁移组件时必须改成纯渲染/纯交互边界：

- 状态归 `StateRoot` 或对应 reducer-owned store。
- 输入只产出 semantic intent 或 v3 message。
- 渲染只消费 view-model，返回 frame lines、spans 或 hit region metadata。
- 组件内部不得发起 IO、history request、terminal input 或 session save。

## 8. 核心状态模型

### 8.1 StateRoot

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

`StateRoot` 只保存 terminal id、pane binding、surface snapshot、运行状态标记和请求状态。terminal process handle、event stream subscription、protocol client、resize/input IO 都属于 terminal service 或 core client adapter。

### 8.2 TerminalSurfaceStore

`TerminalSurfaceStore` 只保存实时显示所需状态。

- 可以保存 core-v2 live surface、snapshot、grid viewport 或 vterm surface。
- 可以服务普通 terminal renderer。
- 不得成为 copy mode/history path 的 committed history source。
- 不得向 `HistoryStore` 提供 rows 让其反推出 logical line。

### 8.3 HistoryStore

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

### 8.4 CopyModeStore

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

## 9. 消息和副作用架构

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

## 10. 历史和 copy mode 流程图

### 10.1 进入 copy mode

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

### 10.2 older prepend

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

### 10.3 selection

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

## 11. Render 架构

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

当前 render 主线已经从最小 frame 推进到 styled chrome renderer 和 render framework。后续实现必须保持下面的顺序：

- UI framework 交互产品化总验收已经完成。
- terminal-live、copy-history、empty/exited、Terminal Picker 内容 renderer 一期已经完成。
- Terminal Picker 数据源与 Terminal Pool service 接线一期已经完成。
- Terminal Pool 管理页一期已经完成。
- Workbench Tree overlay 一期已经完成。
- Floating pane 一期已经完成。
- Prompt/Help overlay 一期已经完成。
- Tab/Workspace 产品入口一期已经完成。
- TUI 产品壳总验收已经完成。
- terminal live 连接展示与交互前推已经完成：attach 后会通过 terminal service 拉取 core-v2 live snapshot 初始化 live surface，输入仍只经 terminal service 发送，画面更新只来自 live surface 回投。
- copy-history content renderer 深化已经完成：搜索、match navigation、viewport scroll、scrollbar/status、content-local mouse selection、selection/match 颜色层级和 position token 都继续建立在 authoritative `HistoryWindow` 之上。
- render cleanup/performance 已完成：`RenderVM` 不再暴露 `Mode`、`Lines`、`Status`、`HitRegions` 兼容输入字段，renderer 主路径只消费 `ShellVM`，并已建立 large output benchmark 基线。
- 后续再继续 terminal-live streaming/rich attributes、copy-history 最终 polish 和 remote/legacy 边界拆分等独立切片。

UI framework 交互产品化总验收包括：

- header/footer 产品信息层和 mode-specific hints。
- pane/resize/global mode 的键盘入口。
- 鼠标 hit region 到 pane focus、pane action、toast/overlay 的派发。
- active pane border/title/footer/toast 的实时反馈。
- split、close、resize、zoom、card/split、header/footer hide 后的 layout measurement、terminal content rect resize 和 copy rebind。

terminal-live content renderer 只能在上述交互闭环完成后深化。terminal live 内容只是 content renderer 的一种，不得为了接真实 terminal 内容绕过 render framework、命令 contract、layout plan 或 hit region。

当前 terminal-live content renderer 一期的架构边界：

- `TerminalSurfaceStore` 可以保存 live surface 是否已到达、实时行、基础 cursor metadata 和错误状态；这些只服务实时显示，不是 history truth。
- `TerminalSurfaceStore` 还表达 attached/exited/error lifecycle；退出态保留最后 live surface 行并在 panel/footer 中显示 exited 状态，但不写入 history。
- attach result 后可以通过 `TerminalSurfaceService.LiveSurface` 拉取一次 core-v2 live snapshot 作为 live surface 初始化；该 snapshot 只服务实时显示，不得作为 copy mode 或 committed history source。
- `RenderVMBuilder` 负责把 live 行投影为 `ContentVM`：基础 ANSI SGR 转成 semantic style token，pending/empty/exited 转成所属 pane 内的 content 状态，live cursor 转成 content-local cursor。
- `Renderer` 和 render framework 只按 content rect 裁切、合成和输出 styled frame，不解释 terminal lifecycle，也不从 live surface 推断 copy/history。
- `FrameSink` 只消费 `Frame` / `RenderResult` 的 ANSI styled frame；不得为了 live 内容绕过 `RenderResult` 直接写 TTY。
- 若未来协议提供 styled cell、精确 cursor、link、truecolor 或 terminal mode metadata，应作为 live content VM 的输入增强，而不是改变 render framework 与 history/copy mode 边界。

当前 copy-history content renderer 一期的架构边界：

- `HistoryStore + CopyModeStore` 是唯一输入；copy-history content 不读取 `TerminalSurfaceStore`、snapshot、grid viewport 或 local VTerm scrollback。
- `RenderVMBuilder` 只在 terminal id、bound token 和 bound cols 与 authoritative `HistoryWindow` 一致时生成历史内容；绑定不一致、缺 window 或 resize 后等待新 window 时只能生成 pending、empty 或 error。
- `RenderVMBuilder` 负责把 authoritative rows 投影为 `ContentVM`：logical-line / continuation / clipped marker、styled selection、content-local copy cursor 和 row/line/cols 位置摘要都在 VM 层表达。
- `Renderer` 和 render framework 只按 content rect 裁切、合成和输出 styled frame；renderer 不请求 history、不执行 selection 语义、不调用 clipboard，也不从 live content 补齐历史。
- copy/yank 成功反馈由 reducer 添加 shell toast；clipboard IO 仍通过 effect 和 result message 完成，不进入 renderer。
- `RenderVM` 不再保留 raw authoritative row text 兼容投影；logical-line marker、clipped marker、plain frame snapshot 都只是 UI/output adapter，不得污染历史 truth。

当前 copy-history content renderer 深化的架构边界：

- `CopyModeStore` 只新增交互态：可见行数、搜索 query、匹配列表、active match、viewport top 和 cursor clamp；这些字段不保存历史 truth。
- 高度变化只更新 copy view rows 并夹紧 viewport；content cols 变化仍必须失效旧 window 并重新请求 authoritative latest window。
- 搜索只在已接纳的 authoritative rows 上计算 match；输入字符、Backspace、Enter、方向键和 PageDown 只改变 copy mode 交互态，不触发 live surface fallback。
- match navigation 会把 copy cursor 移动到 authoritative row/col，并保证 cursor 进入当前 viewport。
- content-local mouse selection 使用 render hit region 给出的 authoritative row，回到 reducer 更新 cursor/selection；未命中 copy history row 的鼠标事件不得漏发为 terminal input。
- renderer 负责显示搜索栏、可见 rows、selection/match 样式层级、scrollbar/status 和 row/line/part/cols/span/search/older position token；这些都只属于 UI 投影，不写回 `HistoryStore` 或 core-v2。

已完成的 empty/exited/Terminal Picker content renderer 一期的架构边界：

- empty pane 与 exited pane content 只消费对应 `PaneState`，由 `RenderVMBuilder` 投影为 CTA 行和 content action hit region；renderer 只负责裁切、合成和输出。
- Terminal Picker 一期最初只消费 reducer-owned `ShellStore`、当前 workspace panes、active session/surface/history terminal id 和 overlay query；切片 69 后 item source 已扩展为 reducer-owned `TerminalPoolStore`，但 renderer 仍不得直接读取 service 或伪装完整管理页。
- Terminal Picker search cursor 是 overlay content-local cursor；layout measurement 必须让 Terminal Picker overlay 拥有 cursor，而不是让 cursor 落回 pane 内容。
- content action hit region 由 render framework 转换为全局坐标；runtime 可以把已可落地的 picker row focus/overlay close、empty/exited close 反馈回 reducer。
- create、restart、reconnect 和 pool row attach 已在切片 69 接入 service/effect/result message；尚未接入的 manager、edit、kill 等动作只能显示明确反馈或进入后续 Terminal Pool 页面切片，不得直接修改 service state 或伪造 terminal lifecycle。
- content action 不得漏发为 terminal input；未命中 content action 的鼠标事件仍按现有 hit region / terminal forwarding 边界处理。

当前 Terminal Picker 真实交互深化的架构边界：

- `OverlayState.Query` 与 `OverlayState.SelectedIndex` 是 reducer-owned picker 交互状态；query 更新、过滤后重置 selection、上下移动 selection 都必须由 `ShellStore` 方法完成。
- `state.TerminalPickerItems(root)` 是 app 与 render 共享的当前 picker item 推导入口；它只从 reducer-owned root、当前 workspace panes、当前 active session/surface/history terminal id 和 reducer-owned `TerminalPoolStore` 推导列表，不直接读取服务端 Terminal Pool。
- UI input reducer 在 Terminal Picker overlay 打开时优先消费字符、Backspace、上下方向键和 Enter；这些输入不得进入 terminal input path。
- Enter 和 picker row click 必须复用同一 selected item 语义：pane row focus/close overlay，pool row 走 service attach，create row 走 service create；不得按键盘和鼠标分叉实现第二套路由。
- `picker.new` 只能通过 terminal service create effect/result 接线，result 到达后显示反馈 toast；不得直接修改 service state 或在 result 前伪造 terminal lifecycle。
- Terminal Picker 只投影轻量 selected hint，不显示 Terminal Pool 式 detail/preview 管理块；renderer 不读取 service、core client 或 Terminal Pool。
- overlay cursor、row action 和 selected hint 继续由 render framework 按 content rect 裁切合成，不得覆盖 pane chrome、toast 或 shell chrome。

当前 Terminal Pool 数据源与 Picker 服务接线一期的架构边界：

- `TerminalPoolStore` 是 reducer-owned list/source 状态，只保存 list 请求状态、当前 items、错误、stale guard 序号和最近 create/attach 结果；它不是完整 Terminal Pool 管理页状态。
- `TerminalService` 已扩展最小 Terminal Pool contract：`List`、`Create`、`Restart`、`Reconnect`、`Attach` 均只能通过 effect/result message 回到 reducer；service 不得直接修改 `StateRoot`。
- Terminal Picker 打开时可以触发 `TerminalPoolListRequestMsg`，服务端 list result 只能先写入 `TerminalPoolStore`，再由 `state.TerminalPickerItems(root)` 合并当前 workspace panes 与 pool items。
- picker attach 对当前 pane row 仍只执行 pane focus/close overlay；对 pool row 才通过 terminal service attach/reconnect result 更新 session/surface，并显示 toast。
- `picker.new` 通过 terminal service create result 显示反馈并触发 list refresh；不得在 result 到达前伪造 terminal lifecycle。
- list/create/attach/restart/reconnect 失败必须写入 reducer-owned error/toast；stale list result 必须被拒绝。
- 本阶段不实现 Terminal Pool 管理页、跨 workspace 管理、跨 remote 管理、metadata edit、kill/remove UI 或 Workbench Tree。

当前 Terminal Pool 管理页一期的架构边界：

- Terminal Pool Page 是独立 surface/content，不是 Terminal Picker overlay 的字段扩展。
- 页面状态必须 reducer-owned，至少表达 open/closed、query、selected index、loading/empty/error、last action status 和必要的 detail/preview VM 输入。
- TerminalPoolStore 仍是 terminal list/source 状态；页面可以复用该 store，但不得让 renderer 或 content renderer 直接读取 service。
- 页面打开必须触发 list request；list result 先写入 reducer-owned TerminalPoolStore，再由页面 VM 推导 list/detail/preview。
- 页面 query 和 selected index 属于 Shell/Page state，不能塞进 TerminalPoolStore 变成服务端列表状态。
- 页面打开、关闭、query 更新、selection 移动、row click、action click 都必须进入 app message / reducer 路径；普通输入不得漏发到底层 terminal。
- TerminalService 需要覆盖 Terminal Pool 页面动作的 effect/result 边界：attach、kill、edit metadata 至少要有明确 request/result message；service 不得直接修改 StateRoot 或伪造 terminal lifecycle。
- attach 成功可以更新当前 active pane/session/surface，并通过 toast 或页面状态反馈；attach 失败只能反馈错误，不得改写本地 terminal truth。
- kill 成功前不得从本地 list 中预删 terminal；kill result 到达后可以触发 list refresh 或标记 action 状态，但 lifecycle truth 仍以后续 service/list/event 为准。
- edit metadata 一期可以只做最小 service 边界和反馈；如果需要 Prompt，应通过后续 Prompt overlay 切片完善，不得在 Terminal Pool 页面内临时实现第二套表单状态机。
- RenderVMBuilder 负责把 Terminal Pool page state 投影为 terminal-pool ContentVM：list rows、selected row、detail、preview、footer action 和 cursor；renderer 只按 content rect 裁切、合成和输出。
- layout measurement 必须给 Terminal Pool Page 足够的可见内容空间；在常规 80x24 / e2e viewport 下不得裁掉 detail、preview 摘要或 Attach/Edit/Kill action。
- 窄高退化必须由页面 VM 或 content renderer 明确压缩内容优先级，不能靠 renderer 最后裁切把关键 action 隐藏掉。
- Terminal Pool 页面不得混入 Workbench Tree、floating drag/resize、remote 管理、tab/workspace 结构操作或 copy-history 逻辑。
- 页面 action hit region 必须由 render framework 转换到全局坐标，并由 runtime 按最新 hit region 派发；content action 命中不得转发成 terminal input。
- 页面内所有文本继续通过 cell-width helper 裁切，emoji、CJK、combining mark 和 ANSI styled text 不得破坏 overlay/page chrome。

## 12. 与 core-v2 的接口

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

## 13. 包边界硬约束

- `historyview` 不 import `render`、`runtime`、`app`。
- `copymode` 不 import protocol client、runtime 或 renderer。
- `render` 不 import services/coreclient。
- `services` 不 import renderer。
- `input` 不 import app state。
- `app` 可以组合各包，但不得持有所有业务细节字段。
- `app/runtime` 不 import render implementation、core protocol client 或 terminal process handle。
- `terminalhost` 不 import state reducer、historyview、copymode 或 services/coreclient。
- `render/style` 可以 import `lipgloss/v2` 和 `x/ansi`，但不得 import Bubble Tea。
- `TerminalSurfaceStore` 与 `HistoryStore` 不能互相反推数据。
- terminal service 不持有或修改 `StateRoot`；它只通过 event/result message 反馈 attach、resize、restart、surface update 和 title/metadata 变化。
- history service 不持有 `HistoryStore`；它只返回 response message。

## 14. 测试策略

优先用小 harness 固定边界，再做 e2e。

- input harness：key/mouse -> semantic intent。
- reducer harness：message -> state + effects。
- historyview harness：latest replace、older prepend、empty older exhausted、stale response、boundary overlap、local request id 与 core window token 分离、cols mismatch 拒绝。
- copymode harness：cursor、viewport、selection、clipped span、multi logical line copy。
- render VM harness：live mode 与 copy mode projection 分流、copy mode 缺 window 时不从 live surface fallback。
- service fake harness：core response 映射、local request id、error cleanup。
- terminal service harness：attach、resize、restart、event stream 只回 message，不直接改 state。
- app runtime harness：message 顺序、effect result 回投、timer、batch、cancel、quit。
- terminal host harness：input event 转换、direct terminal enter/exit、FrameSink 输出 contract。
- UI render helper harness：lipgloss/v2 样式 helper 宽度、裁剪、ANSI 安全性，不依赖 Bubble Tea。
- visual alignment harness：固定 viewport smoke snapshot、真实 TTY ANSI frame、截图/录制和人工对照 `tuiv2` 视觉目标；不得只凭 Unicode 线框存在判定完成。
- integration harness：wheel up 进入 copy mode、page up 请求 older、resize 后 latest replace、旧 cols response 被拒绝。

tuiv2 测试可以作为行为参考，但不得把旧 snapshot/local scrollback 语义带进 v3。

## 15. 推荐落地顺序

1. 建立 `termx-tui-v3/` Go module、包目录和基础测试框架。
2. 建立 v3 自有 `Msg`、`Effect`、`AppRuntime`、`EffectRunner` 骨架和 harness。
3. 建立 `TerminalHost` fake 与 `FrameSink` contract，先不接真实 TTY。
4. 迁入或重写 `input`，替换 Bubble Tea key/mouse 类型，只输出 semantic intent / terminal input。
5. 建立 `StateRoot`、message、effect、reducer 骨架。
6. 实现 reducer-owned `historyview` 状态与 fake source harness。
7. 实现 `copymode` 状态机和 selection harness。
8. 建立 `RenderVMBuilder`，先用 fake state 渲染 live/copy 两种 projection。
9. 迁移 render primitives、pane chrome、hit regions、lipgloss/v2 style helper 和 render cache。
10. 建立 `CoreClient` fake adapter，接入 history latest/older。
11. 建立 terminal service fake，接入 terminal surface/update/resize/restart message。
12. 接入 workspace/pane layout、modal、clipboard、session restore。
13. 接入真实 protocol adapter、真实 `TerminalHost`、terminal input、resize、attach/restart。
14. 补最小端到端 harness。
15. 完成 styled chrome renderer、cell matrix、theme token、ANSI FrameSink、header/footer、toast/overlay、pane chrome 和 pane command 基础。
16. 完成 UI framework 交互产品化总验收：header/footer 信息层、pane/resize/global mode、鼠标命中、active pane 反馈、toast 操作、layout/effect 同步和基本手工测试入口。
17. 完成 terminal-live content renderer 一期：live 行、基础 style、cursor、pending/empty/exited、宽字符裁切和 no chrome leak。
18. 完成 copy-history content renderer 一期：authoritative rows、logical-line marker、selection、cursor、position token、copy/yank feedback、宽字符裁切和 no chrome leak。
19. 完成 empty/exited/Terminal Picker content renderer 与 Terminal Picker 真实交互。
20. 完成 Terminal Pool 数据源与 Picker 服务接线。
21. 实现 Terminal Pool 管理页一期。
22. 实现 Workbench Tree overlay 一期。
23. 实现 floating pane 一期。
24. 实现 Prompt / Help overlay 一期。
25. 实现 Tab / Workspace 产品入口一期。
26. 完成 TUI 产品壳总验收。
27. 前推 terminal live 连接展示与交互。
28. 深化 copy-history 内容 renderer。
29. 清理 render 兼容投影并建立性能基线。
30. 做视觉差距审计与固定 viewport smoke 基线。
31. 重绘 `tuiv2` 风格 shell header/footer。
32. 重绘 `tuiv2` 风格 pane chrome 与 split 视觉。
33. 对齐 overlay、toast 和 floating 视觉。
34. 做真实默认 TUI 视觉验收。

每个切片都必须避免引入 local scrollback history fallback。

## 16. 第一阶段范围

第一阶段只做：

- v3 module skeleton
- package boundaries
- v3 自有 `Msg` / `Effect` 基础类型
- `AppRuntime` fake harness
- `TerminalHost` fake 与 `FrameSink` contract
- reducer-owned `historyview` state
- fake `historyview.Source`
- `copymode` state skeleton
- reducer/effect harness
- latest replace harness
- older prepend harness
- stale response harness
- selection clipped span harness

第一阶段不做：

- 真实 AppRuntime/TerminalHost 完整接入
- 真实 protocol adapter
- 旧 `tuiv2/` 原地修补
- local VTerm scrollback 迁移
- render 全量迁移
