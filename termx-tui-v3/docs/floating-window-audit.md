# termx-tui-v3 Floating Window 问题审计

状态：审计完成，待拆修复切片
日期：2026-06-17
范围：`termx-tui-v3/app/`、`termx-tui-v3/state/`、`termx-tui-v3/render/`、`termx-tui-v3/docs/`

## 1. 结论

当前 floating window 最大的问题不是单个按钮失效，而是同一套代码里混用了三种身份：

- `paneID`：tiled pane 的身份。
- `floatingID`：floating pane 的外层窗口身份。
- `ActiveFloatingID`：当前视觉焦点，不应该被当作命令目标 fallback。

这导致 floating 的点击、attach/create、owner/size lock、resize、storage restore 都有“看起来能跑，但目标可能错”的风险。修复时应该先把 target 模型收口，再补具体交互。

## 2. 问题清单

| ID | 严重度 | 问题 | 影响 |
| --- | --- | --- | --- |
| FW-01 | 高 | terminal attach/create/reconnect 的目标和尺寸依赖 `ActiveFloatingID` | pane 操作可能误 attach 到 floating；非 active floating 可能用错尺寸 |
| FW-02 | 高 | hit region 用 `PaneID` 承载 floating id，runtime 只做了局部特判 | floating content/chrome action 路由不一致，点击目标不稳定 |
| FW-03 | 高 | floating terminal chrome 的 take-owner 点击会走 pane owner 路径 | follower floating 上点击 owner 槽可能无法正确申请 owner |
| FW-04 | 高 | floating resize 触发条件按 active floating 判断，不按命令目标判断 | 非 active floating 的 auto-fit/resize owner 场景会漏 resize 或命中错 terminal |
| FW-05 | 中 | collapsed floating 仍可能作为 terminal input target | 内容隐藏后键盘仍可能送到这个 terminal |
| FW-06 | 中 | Workbench Tree 里 floating 节点不可打开/管理 | 结构化导航显示了 floating，但实际操作断路 |
| FW-07 | 中 | storage restore 没有校验 floating/view 交叉引用，attach 使用旧 desired size | 可能恢复出不可见 orphan attachment，或启动时尺寸先错再补 |
| FW-08 | 中 | close floating 后本地随机提升 replacement owner | owner truth 没完全走 core，且 map 遍历选择不稳定 |
| FW-09 | 低 | footer/active target 仍以 pane 为中心 | floating 获得视觉焦点后，底栏目标提示可能误导 |
| FW-10 | 低 | 旧 view-local layout lock 残留 | 容易和 terminal 级 size lock 混淆 |

## 3. 详细问题

### FW-01：attach/create/reconnect 目标和尺寸依赖 active floating

代码证据：

- `app/terminal_pool.go:229-237`：attach 先调用 `terminalPoolAttachSize(root)`，再解析 `TargetFloatingID`；空目标会 fallback 到 `root.Shell.ActiveFloatingID`。
- `app/terminal_pool.go:310-333`：create 同样先按当前 active view 算尺寸，`TargetFloatingID` 只被原样带到 result。
- `app/terminal_pool.go:384-392`：reconnect 也重复了 active floating fallback。
- `app/terminal_pool.go:651-664`：`terminalPoolAttachSize` 直接读 `activeFloatingContentRect`，没有 target 参数。
- `app/shell.go:782`：prompt submit 创建 terminal 时把 `TargetFloatingID` 写成当前 active floating，而不是 prompt/open action 的真实 target。
- `state/shell_overlay.go:111-137`：picker/pool overlay 有 `TargetID`，但后续 attach 路径没有稳定使用它来决定 pane/floating 类型。

结果：

- 如果用户在 pane 上发起 attach/create，但之前有 active floating，可能误 attach 到 floating。
- 如果用户点击一个非 active floating 里的 empty attach/create，命令可能落到旧 active floating 或 active pane。
- 创建/连接 terminal 时用的 cols/rows 可能来自 active floating，而不是真实目标 floating/pane。

修复方向：

- 引入显式 target：`TerminalAttachTarget{Kind: pane|floating, PaneID, FloatingID}`。
- overlay/prompt 创建时保存 target kind，不再只保存裸 `TargetID`。
- `terminalPoolAttachSize` 改成按 target 解析 content rect；解析 target 必须发生在发 service request 之前。
- terminal pool reducer 不允许再把空 `TargetFloatingID` 自动解释成 active floating。

建议 harness：

- active floating 存在时，从 tiled pane 打开 picker 并 attach，必须落到 pane。
- 点击非 active empty floating 的 attach/create，必须落到被点击 floating，并使用该 floating content rect。
- `ActionPoolAttachFloat` 创建的新 floating 必须用新 floating rect，而不是旧 active floating rect。

### FW-02：hit region owner 语义混用

代码证据：

- `render/layout_hit_regions.go:138-148`：floating 的 chrome、content、drag hit region 都把 `HitRegion.PaneID` 填成 floating id。
- `render/layout_hit_regions.go:153-166`：floating chrome action slot 也写入 `PaneID: paneID`，这里的 `paneID` 实际是 floating id。
- `app/runtime.go:860-865`：runtime 只对 `ActionResizeLayoutLock` 做 floating 特判，其它 content action 仍走 `ShellContentActionMsg`。
- `app/shell.go:309-312`：`ShellContentActionMsg` 一开始会 `FocusPane(PaneID)`；当这个值其实是 floating id 时，pane focus 不成立。
- `app/copymode.go:158-168` 和 `render/vm.go:880-882` 已经在兼容“PaneID 可能是 floating id”的特殊情况。

结果：

- floating action 有的靠 `TargetID: msg.PaneID` 正好工作，有的会先尝试 focus pane，再依赖旧 active floating。
- copy/history 鼠标路径为了兼容 floating，不得不把 `PaneID` 解释成 floating id。
- 后续新增 floating action 时很容易接错到 pane 路径。

修复方向：

- `HitRegion` 增加明确 owner 字段，例如 `OwnerKind`、`OwnerID`，或至少增加 `FloatingID`。
- runtime 根据 owner kind 分发到 `ShellPaneContentActionMsg` / `ShellFloatingContentActionMsg`。
- 不允许再把 floating id 写进名为 `PaneID` 的字段后让 app 层猜。

建议 harness：

- 点击 floating 内容文本、empty attach、chrome close、terminal size lock、terminal take-owner 都必须先聚焦/raise 被点击 floating。
- 同一屏里有 tiled pane 和两个 floating 时，所有 floating hit region 的目标都不能受旧 active floating 影响。

### FW-03：floating terminal chrome 的 take-owner 点击走错路径

代码证据：

- `render/panel_terminal_chrome.go:101-105`：owner 槽在 follower 时携带 `ActionTerminalTakeResizeOwner`。
- `render/chrome_primitive.go:61-90`：floating terminal label slot 会进入 floating primitive 的 `ActionSlots`。
- `render/layout_hit_regions.go:153-166`：这些 action slot 被转成 `HitRegionContentAction`，`PaneID` 实际是 floating id。
- `app/runtime.go:813-818`：take-owner 的确认点击只处理 `HitRegionPaneAction`，不处理 floating 的 `HitRegionContentAction`。
- `app/shell.go:453-455`：`ActionTerminalTakeResizeOwner` 在普通 content action 中调用 `requestPaneResizeOwner(root, msg.PaneID)`。

结果：

floating follower 上点击 terminal chrome 的 owner 槽时，代码会把 floating id 当 pane id 去申请 owner，目标不对。footer 的 `floating take-owner` 走 `requestFloatingResizeOwner`，但 chrome 点击路径没有对齐。

修复方向：

- 在 runtime 对 floating owner slot 进入 `ShellFloatingContentActionMsg`，并保留 double-click confirm 语义。
- `ActionTerminalTakeResizeOwner` 应按 owner kind 调用 pane 或 floating owner request。
- owner slot 的 hit region 应携带 view id，避免再从 pane/floating id 反查。

建议 harness：

- follower floating terminal chrome 第一次点击显示 `owner?`。
- 第二次点击同一槽位调用 attach owner request，`ViewID` 必须是 `floating:<id>`。

### FW-04：floating resize 触发条件按 active floating 判断

代码证据：

- `app/layout_resize.go:145-158`：floating command 是否可能触发 terminal resize，最终只看 `activeFloatingHasTerminal(root)`。
- `app/layout_resize.go:164-175`：`activeFloatingHasTerminal` 只检查 `shell.ActiveFloatingID`。
- `app/live.go:530-548`：auto-fit refresh 遍历 floating 时，找到第一个匹配 terminal 就 return；多个 auto-fit floating 只刷新第一个。
- `state/shell_floating_group.go:91-110`：`RefreshAutoFit` 更新目标 floating rect，但不会 focus/raise 该 floating。

结果：

- 对非 active floating 的 background auto-fit refresh，layout resize 判断可能漏掉目标。
- 如果 active floating 是另一个 terminal，后续 owner rect 解析可能围绕 active view 展开。
- 同 terminal 多个 auto-fit floating 时，只有第一个会响应 live surface size 变化。

修复方向：

- `floatingCommandMayResizeTerminal` 应按 command target/result id 判断，不能读全局 active。
- `maybeRefreshFloatingAutoFit` 应批量刷新所有匹配 floating，或明确选择 owner floating。
- layout resize reducer 最好接收“本次变动影响哪个 view/terminal”的 hint，再解析 owner binding。

建议 harness：

- 两个 floating 连接不同 terminal，非 active floating auto-fit refresh 后只能影响自己的 geometry。
- 同 terminal 两个 auto-fit floating，live surface size 变化后两个 geometry 都刷新，且只有 owner view 可以 resize PTY。

### FW-05：collapsed floating 仍可能接收 terminal input

代码证据：

- `state/shell_floating.go:161-168`：单个 floating toggle collapse 后仍调用 `focusRaiseFloating`，active floating 不清空。
- `render/layout_plan.go:197-200`：collapsed floating 的 `ContentRect` 会清空。
- `render/layout_cursor.go:7-17`：active floating 内容 rect 为空时不会展示 cursor。
- `app/live.go:641-654`：`liveInputTarget` 只看 active floating 和 terminal id，不检查 `Collapsed`。

结果：

floating 内容已经不可见，cursor 也不会展示，但键盘输入仍可能发送到这个 hidden terminal。

修复方向：

- 产品语义需要明确：collapsed floating 是否能拥有 terminal input。
- 建议默认 collapse 后不作为 live input target；可以保留 titlebar active 样式，但键盘 terminal input 应回到 tiled active pane，或需要显式 summon 后才恢复。

建议 harness：

- active floating collapse 后发送普通字符，不应进入 collapsed floating terminal。
- summon/show floating 后，terminal input 才回到该 floating。

### FW-06：Workbench Tree floating 节点不可操作

代码证据：

- `render/product_content.go` 已有 floating/workbench tree 展示路径。
- `app/shell.go:1043-1044`：Workbench Tree open 到 floating 时直接 toast `not implemented`。
- `app/shell.go:1051-1139`：rename/delete/detach/zoom 等 tree action 只支持 workspace/tab/pane。

结果：

Workbench Tree 作为结构化导航显示了 floating，但用户不能从这里打开/raise/关闭/重命名 floating。

修复方向：

- Workbench Tree 对 floating 至少支持 open/raise、collapse、close。
- 如果不准备支持完整管理，应从 tree action 中显式降级，而不是展示一个不可操作节点。

### FW-07：storage restore 缺少 floating/view 交叉校验

代码证据：

- `state/workbench_storage.go:131-162`：snapshot validation 只校验 terminal view 自身字段，不校验 `FloatingID` 是否存在于 `Floatings`。
- `state/workbench_storage.go:116-128`：`ToTerminalViewStore` 会直接 bind floating view。
- `app/workbench_storage.go:177-180`：load 后会对所有 restored binding 发 attach effect。
- `app/workbench_storage.go:185-221`：restore attach 使用 binding 里的旧 `DesiredCols/DesiredRows`，不是当前 measured content rect。

结果：

- storage 中如果出现 orphan floating binding，启动后可能 attach 一个不可见 view。
- floating rect/header/footer/viewport 变化后，restore 首次 attach 的 size 可能是旧值；后续依赖 layout resize 补救。
- locked terminal、owner/follower restore、collapsed floating restore 的组合缺少明确语义。

修复方向：

- storage load 时按 shell 当前 panes/floatings 过滤 terminal views。
- restored owner view 如果可见且未 size lock，应以当前 measured content rect 作为 attach/resize 目标。
- collapsed/不可见 floating 默认不应发会改变 PTY size 的 owner resize。

建议 harness：

- snapshot 有 orphan floating binding 时，不发 terminal attach。
- restored owner floating 的 rect 与 saved desired size 不一致时，解锁状态下最终 resize 到当前 rect。
- restored collapsed owner floating 不应因为不可见 content rect 把 terminal size 改成默认值。

### FW-08：close floating 后本地 replacement owner 不稳定

代码证据：

- `state/terminal_view.go:204-218`：`DetachFloating` 删除 view 后立刻调用 `promoteReplacementOwnerLocked`。
- `state/terminal_view.go:312-336`：replacement owner 通过遍历 map 选第一个同 terminal binding。
- `workflow.md` 的 resize 语义要求 owner transfer 走协议、effect、message 路径，不能在 UI 层偷偷改 truth。

结果：

关闭一个 owner floating 后，TUI 本地可能立刻把另一个 view 标成 owner；选择哪个 view 取决于 map 遍历顺序，不稳定。core-v2 的真实 owner/control result 还没确认时，UI 会先展示一个本地结论。

修复方向：

- detach owner 后应进入 pending/unknown，等待 core resize-control/attach result 确认。
- 如果必须本地选择 fallback，也要使用稳定规则，例如 active visible owner candidate 优先、pane 顺序优先、floating z-order 优先，并明确标记 pending。

### FW-09：footer active target 仍以 pane 为中心

代码证据：

- `render/vm.go:422-433`：`activeTargetSummary` 始终返回 `pane:<title>`，没有 active floating 分支。
- `render/vm.go:537-555`：active floating 存在时 tiled pane 会降级为非 active，这是正确的视觉语义。

结果：

屏幕视觉焦点在 floating 上时，footer 仍可能提示 pane target。用户看到的 active target 和后续 footer action 的真实目标不完全一致。

修复方向：

- active floating 存在时 footer summary 应显示 `float:<title>` 和 terminal role。
- footer action handler 应尽量使用显式 active owner target，而不是各自读取 pane/floating 全局状态。

### FW-10：view-local layout lock 残留

代码证据：

- `state/terminal_view.go:50-57`：`TerminalViewLayout` 仍有 `SizeLocked`。
- `state/terminal_view.go:534-560`：layout command 仍支持 `toggle-lock`。
- `app/ui_input.go:735-776`：仍有 `"terminal layout lock"` 和 `terminalViewLayoutToast` 的 locked/unlocked 文案。
- 当前 terminal size lock 已通过 `terminalmeta.SizeLockTag` 作为 terminal 级语义实现。

结果：

代码里同时存在“terminal size lock”和“view layout lock”两套 lock 词汇。对 floating 来说这会继续污染 owner/follower/size lock 的理解。

修复方向：

- 如果 view-local lock 已无产品语义，应删除或改名为明确的 `layoutFreeze`。
- `ActionResizeLayoutLock` 和 footer `s LOCK` 只保留 terminal 级 size lock 语义。

## 4. 看起来已经正确的点

- cursor 相对位移和裁剪隐藏方向是正确的：`render/layout_cursor.go:47-67` 会把 content-local cursor 加上 content rect origin，且 `cursorWasClipped` 会隐藏被裁剪的真实 terminal cursor。
- floating z-order 的 hit region 顺序是正确方向：`render/layout_hit_regions.go:22-24` 从最上层 floating 开始追加 foreground hit region。
- terminal 级 size lock 的 metadata broadcast 和 owner 解锁后 resize 补偿已经在 R54 收口，本审计不再重复列为当前 floating 问题。

## 5. 建议修复顺序

1. 先修 FW-01/FW-02：引入显式 target/owner kind，统一 attach/create/picker/prompt/runtime hit region 分发。
2. 再修 FW-03/FW-04：把 owner/take-owner/resize/auto-fit 全部改成 target view 驱动。
3. 然后修 FW-05/FW-07/FW-08：收口 collapsed、restore、close owner handoff 这些生命周期语义。
4. 最后修 FW-06/FW-09/FW-10：补 Workbench Tree、footer target 和旧 layout lock 清理。
