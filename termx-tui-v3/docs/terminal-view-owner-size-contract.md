# Terminal View Owner / Size Contract

本文只整理 `tuiv2` 里已经跑通过的交互语义，并对照 `termx-tui-v3` 当前状态列出待确认合同。后续实现应按本文确认后的语义收敛，不在 `tuiv2` 原地修改。

## 结论

`◆ owner` 不是一个单纯 UI 标签，而是当前 view 可以驱动 terminal PTY resize 的控制权。`◇ follow`/`observer` 可以展示同一个 terminal，但不能因为自身 pane 尺寸变化改写共享 PTY size。

小白点 `·` 不是 owner/follower 状态提示。它只表示当前 pane/floating 的 content rect 里，有一部分落在 terminal 可见内容 extent 之外。owner 且没有 terminal size lock 时，terminal 应该跟随 owner content rect resize；如果 resize 已经稳定后 owner pane 仍长期出现大片小白点，优先判断为 owner resize 没有收敛、size lock 状态误判，或 view-local layout 被错误当成 terminal size lock。

## tuiv2 参考语义

| 领域 | tuiv2 状态/函数 | 语义 |
| --- | --- | --- |
| terminal 全局 owner | `TerminalRuntime.OwnerPaneID` | 全局共享展示里的 owner pane 标识，可能来自本地，也可能被 core/外部 owner 覆盖。 |
| 本地 resize 控制者 | `TerminalRuntime.ControlPaneID` | 本 TUI 内真正允许发 PTY resize 的 pane。binding role 由它派生。 |
| 连接集合 | `TerminalRuntime.BoundPaneIDs` / `PaneBinding.Connected` | 同一个 terminal 可以被多个 pane/floating 连接；这是展示/输入连接，不等于每个连接都有 resize 权。 |
| owner 冻结 | `RequiresExplicitOwner` | owner 释放或外部 owner 介入后，不再自动 resize，直到显式 takeover。 |
| takeover 后强制 resize | `PendingOwnerResize` | owner handoff 后即使 cached geometry 一样，也要强制下一次 resize，避免新 owner 卡在旧尺寸。 |
| terminal size lock | metadata tag `termx.size_lock` | terminal 级锁；锁住后 owner 仍可存在，但不能驱动 PTY resize。 |
| pane 本地投影 | `PaneBinding.ContentOffset` | 只影响当前 pane 的内容对齐/平移，不改变 terminal process、history truth 或 PTY size。 |
| role 展示 | `syncBindingRolesForTerminal` | 默认所有 bound pane 是 follower，只有 `ControlPaneID` 对应 binding 是 owner。 |

### Resize 流程

| 场景 | tuiv2 行为 |
| --- | --- |
| owner pane content rect 改变，terminal 未 size lock | `ResizePaneForView` 调 `ResizeDecision`，允许后发 `EnsureResize(... ResizePolicyOwner, SurfaceID, ViewID)`。 |
| follower/observer content rect 改变 | 不发 owner resize；它只按共享 terminal size 做本地投影。 |
| follower 被显式 `Become Owner` | 先 `AcquireTerminalOwnership`，更新 `ControlPaneID` 和 binding roles，再强制下一次 resize。 |
| zoom / tab switch / 需要几何变化的本地交互 | 如果当前 pane 需要成为 resize owner，先 takeover，再按新 content rect resize。 |
| terminal size lock 开启 | layout/zoom/resize 等几何动作被拦截或降级，不把 view-local lock 伪装成 terminal lock。 |
| 外部 owner 或 core 返回 `CanResize=false` | 本地 control 清掉或冻结，等待用户显式接管或 core 新结果。 |

## 小白点与 extent

tuiv2 的点状填充来自 `drawTerminalExtentHintsWithMetricsAndPlacement`，输入是 terminal 可见内容 metrics 与 pane content rect。

| 规则 | 说明 |
| --- | --- |
| 点只画在 extent 外 | terminal 内容 extent 内的空白仍是正常空格，不画 `·`。 |
| primary screen metrics 会收缩 | primary screen 以实际渲染行数和最大行宽作为 visible metrics；alt-screen 使用 terminal full size。 |
| content offset 同时作用到内容 | 居中/平移改变 terminal 内容在 content rect 中的位置；边界之外才画点。 |
| overflow 与点分开 | 内容比 viewport 大时用 overflow hint；内容比 viewport 小或被平移留空时用点。 |
| owner 不直接决定点 | 点只看 extent 和 placement；但 owner 未锁时应 resize 到 content rect，所以正常稳定态不应长期 underfill。 |

因此，截图里 `◆ owner` 旁边仍有大片小白点，只在这些情况下合理：

1. terminal 真实 size 被 core terminal size lock 锁住；
2. owner attach/resize 还没拿到 `CanResize=true` 或 resize 结果还没回来；
3. 当前 view 明确选择了本地布局模式，让 terminal 以较小 extent 居中/平移展示；
4. terminal 内容 metrics 按 primary screen 收缩到实际可见内容，且产品确认要继续显示 extent hint。

如果以上都不成立，就是 bug。

## Content Offset / Center 合同

tuiv2 的 `paneContentOffsetRange(viewportSize, contentSize)` 用 `min(0, viewport-content)` 到 `max(0, viewport-content)` 作为范围。内容比 viewport 小时可以正向居中；内容比 viewport 大时可以负向平移。

| 操作 | 合同 |
| --- | --- |
| Center / Align | 只改变当前 view 的视觉投影，不抢 terminal owner，不改变 terminal size lock。 |
| Pan | 内容、selection、hit region、live cursor 必须一起做同一个相对位移。 |
| Cursor clipping | cursor 经过同一 extent 变换后，如果落在 content rect 外，必须隐藏，不停在旧位置。 |
| Copy mode | copy mode 使用 authoritative history window，不能复用 live surface extent 或 VTerm scrollback。 |

## tui-v3 当前对照

| 主题 | 当前 v3 状态 | 与 tuiv2/目标合同的差异 |
| --- | --- | --- |
| owner restore | storage restore 已按保存的 `ResizeRole` attach，owner 优先 attach。 | 仍需确认 restore 后 owner content rect 是否必然触发一次 PTY resize，尤其是退出前 pane 尺寸与重进后 viewport 不一致时。 |
| resize role | `TerminalViewBinding.ResizeRole/CanResize/OwnerViewID` 已接收 core resize control。 | `OwnerBinding` 只认 `owner && CanResize`，但 UI 文案也需要明确区分 `owner but size_locked` 与 `follow`。 |
| terminal size lock | `TerminalViewBinding.SizeLocked` 保存 core 返回的 terminal lock。 | `TerminalViewLayout.SizeLocked` 也是 `SizeLocked`，但它是 view-local layout lock。两个名字相同，容易把 terminal lock 与本地投影锁混用。 |
| view-local layout | `TerminalViewLayout.Mode/Pan/Align` 会投影到 renderer。 | 当前 `s LOCK` 文档和测试走 view-local layout lock；这与 tuiv2 `s` 是 terminal metadata size lock 不一致，需要产品确认保留哪个快捷键语义。 |
| dot rendering | `RenderContentViewport` 已只对 live terminal known extent 画 `·`，cursor 也走同一 extent 变换并越界隐藏。 | 需要补验收：owner 未 size lock 且 resize 稳定后，content extent 应等于 owner content rect，不应因为 layout/metrics 残留出现大片点。 |
| replacement owner | `promoteReplacementOwnerLocked` 会在 owner view 删除后自动提升另一个绑定为 owner。 | 旧文档曾指出这偏离 shared terminal 产品语义；需确认 owner 关闭后是冻结等待显式接管，还是自动接任。 |
| follower input | v3 输入按 active terminal binding channel 发送。 | 输入不应隐式 resize 或抢 owner；几何动作需要显式 takeover 合同。 |

## 建议确认后的修复顺序

1. 先命名拆清：terminal size lock 只来自 core resize control/metadata；view-local layout lock 改名或在 UI 上避免叫 size lock。
2. owner attach/restore/host resize/split/floating resize 后，都以 owner content rect 发 authoritative resize；结果以 core `CanResize/SizeLocked/OwnerViewID` 为准。
3. follower/observer 的 pane/floating 几何变化不得发 PTY resize；需要 fullscreen/zoom/fit 时先走显式 owner takeover。
4. dot hint 只表达 extent 外区域；owner 未锁的稳定态必须让 extent 收敛到 content rect，除非用户显式选择本地 projection 模式。
5. center/pan/align 必须统一变换 content、cursor、selection/hit region；cursor 超出 content rect 就隐藏。
6. owner 删除或断开后的 replacement owner 策略单独确认：若采用 tuiv2 产品修正方向，应冻结为 follower/needs explicit owner，而不是自动提升。

## 验收用例草案

| 用例 | 期望 |
| --- | --- |
| owner pane 重进后 viewport 改变 | restore attach 仍申请 owner，收到 `CanResize=true` 后按当前 content rect resize。 |
| owner 未 size lock，split 改变高度 | owner terminal extent 最终等于新 content rect，不出现稳定大片 `·`。 |
| follower pane 变大 | PTY size 不变；follower 可以显示点或 overflow，但 chrome 不显示 `◆ owner`。 |
| terminal size lock 开启 | chrome 显示 terminal locked 状态；owner 可以仍是 owner，但 `CanResize=false`，几何动作不发 resize。 |
| view-local center | 内容和 live cursor 同步移动；cursor 被移出 content rect 时隐藏。 |
| owner view 关闭 | 按确认策略：冻结等待显式接管，或自动提升并强制 resize；不能悄悄展示错误 owner。 |
