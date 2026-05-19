# Terminal History Grid 决策文档

本文档记录 2026-05-19 之后 terminal 历史方向的最新结论。它取代之前的 `terminal-pool-pty-journal.md` 作为历史存储、copy mode、resize、remote/protobuf 合并工作的主设计依据。

## 当前结论

TermX 的 UI 历史主路径应回到 parsed grid/cell history，而不是 raw PTY journal replay。

建议基线 commit：

- `feac69ea Preserve terminal grid geometry across resize`

选择它的原因：

- 已经在 protobuf/remote channel 迁移之后，离当前 remote 代码更近。
- 保留 `terminalGridStore` / `terminalGridAppender` / `terminalGridCodec` 这套 parsed grid history。
- 已经有 compact row codec、grid viewport、screen update 合并、高压输出 latest-frame 优化。
- 比 `e20dedcd` 更接近我们需要继续维护的协议和 app 形态。

不建议以 `c9d35322` 之后的 raw event-log/history-line 路线作为主历史方向。那条路线里有一些修 bug 的思想可取，但它把 UI 历史主查询推向 PTY event replay，复杂度和内存/性能风险更高。

## 和 tmux 是否一致

结论：方向一致，但实现不完全一样。

一致点：

- 都不让客户端从 raw PTY bytes 中间开始解释历史。
- 都先由服务端 terminal emulator 把 PTY bytes 解析成带样式的 cell/grid。
- 历史展示读取的是已经解析好的 cell 内容、style、width、wrapped metadata。
- 更早的历史可以按保留策略丢弃；丢弃后 UI 明确展示“更早历史已清理”，不假装能还原。

差异点：

- tmux 的历史是 pane grid 的内存结构为主，按 history-limit 保留；TermX 的 `terminalGridStore` 是磁盘 page + fixed index 的 parsed row store。
- tmux 不追求用任意新宽度重放旧 PTY bytes；TermX 的 grid viewport 允许基于 stored rows + wrapped metadata 做 reflow。
- tmux 的 renderer 和 history grid 在同一个 core 内；TermX 还有 TUI、remote-ui、App 多个观察入口，所以必须通过 protocol/grid viewport/snapshot 传递结构化 rows。
- TermX 需要处理 owner/follower/floating pane 的 size ownership，tmux 的 pane 模型更直接。

所以准确说：TermX 应采用 tmux-like parsed grid history，而不是完全复刻 tmux 内部结构。

## 之前历史存储的演进

关键提交链：

- `48da1d35 Implement structured terminal history storage`
  - 引入 `terminalHistoryStore`。
  - 持久化已经解析后的 rows/cells/style。
  - TUI/copy mode 开始从 core 取结构化历史，而不是只依赖本地 snapshot scrollback。

- `f1c9aef1 Page copy-mode scrollback snapshots`
  - copy mode 开始分页加载 scrollback，避免一次性拿全量历史。

- `7d7d2ac9 Read live scrollback snapshots from history store`
  - live snapshot 的 scrollback 开始从 history store 读取。

- `27239e66 Rewrite terminal history rows as compact binary`
  - 从 JSON-like row payload 改为 compact binary row codec。
  - 降低磁盘和传输体积。

- `23a3df54 Refactor terminal history store into grid store`
  - `terminalHistoryStore` 重命名/重构为 `terminalGridStore`。
  - 概念从“history row”收敛到“terminal grid row”。

- `f8f88851 Read terminal grid index on demand`
  - 不再把完整 index 常驻内存。

- `daeba75e Route terminal history paging through grid viewport`
  - 引入 `GridViewport`。
  - 历史分页通过 grid viewport 返回结构化 rows。

- `e20dedcd Optimize terminal streaming and compact history`
  - 优化 terminal streaming、compact row、latest-frame 路径。
  - daemon vterm 禁用 emulator scrollback，历史通过 grid appender 捕获 damage rows。

- `b6e99c64 Use structured terminal history in app`
  - remote app 侧开始消费 structured terminal history。

- `feac69ea Preserve terminal grid geometry across resize`
  - 仍保留 grid store。
  - 加入 grid geometry / resize 相关修复。
  - 是当前建议测试和回归的 parsed grid 基线。

- `c9d35322 Fix terminal history preservation on resize`
  - 修复 resize 导致历史丢失的问题，引入 `ResizeWithDamage` 和 resize event log。
  - 但同时把 `terminal_grid_*` 重命名/推进到 `terminal_history_line_*` 和 PTY event log 方向。
  - 只建议提取其中 resize damage 写入历史的思想，不建议直接继承 event-log 主线。

## feac69ea 的历史存储怎么做

核心组件：

- `termx-core/terminal_grid_store.go`
- `termx-core/terminal_grid_appender.go`
- `termx-core/terminal_grid_codec.go`

数据模型：

```text
PTY bytes
  -> core vterm
  -> WriteWithDamage / WriteForLatestFrame
  -> damage.ScrollbackAppend
  -> terminalGridAppender
  -> terminalGridStore pages + grid.index
  -> GridViewport / Snapshot / HistoryReplay
```

存储内容：

- cell content
- cell width
- style: fg/bg/bold/italic/underline/blink/reverse/strikethrough
- link metadata
- timestamp
- row kind
- wrapped flag

存储结构：

```text
grid.meta.pb
grid.index
grid-000000.txg
grid-000001.txg
...
```

`grid.index` 是固定 20 bytes record：

```text
seq, offset, length, flags
```

`flags` 里包含 wrapped 标记。查询历史时根据 offset/limit 从 index 定位 rows，再从 page 文件 `ReadAt` 读取 compact row payload。

## 为什么这个方向比 raw PTY journal 更适合

raw PTY journal 的理论好处是可以从最原始 bytes 重新构造状态。但它的问题是：

- 不能从任意 byte offset 开始 replay，必须有完整 emulator checkpoint。
- checkpoint 必须包含 parser state、cursor、mode、palette、screen、tab stop、charset 等大量状态。
- 要做 row anchor、event index、checkpoint retention，复杂度很高。
- 用“当前宽度”重放旧 PTY bytes 也不一定等价于程序当时真实输出，因为程序当时看到的是旧 PTY size。
- 高频输出时，TUI 不应该逐 byte replay，它只需要最新 screen 和结构化历史页。

parsed grid history 直接保存“已经正确解析出来的结果”。颜色、prompt、二维码、宽字符这些问题不需要依赖第一个 byte 的上下文，因为每个 cell 已经带着自己的 style 和 width。

## 当前 feac69ea 仍然存在的问题

### 1. Resize 后最新行丢失

在 `feac69ea` 中，`Terminal.Resize` 的关键路径是：

```text
pty.Resize
vterm.Resize
broadcast resize
```

`vterm.Resize` 会更新 emulator 和 metadata，但没有返回 resize damage，也没有把 resize 过程中从 visible screen 挤出去的 rows 写入 `terminalGridStore`。

结果：

- owner resize shrink 后，原本在 screen 底部/边界的最新行可能被挤出 visible screen。
- 这些行没有进入 grid store。
- copy mode/history viewport 再去读历史时，看到的就是缺尾部行。

后续 `c9d35322` 的思路是正确的：

```text
damage := vterm.ResizeWithDamage(cols, rows)
appendHistoryLinesFromDamageLocked(damage)
```

但我们应该把这个思想移植回 `terminalGridStore`：

```text
damage := vterm.ResizeWithDamage(cols, rows)
appendGridFromDamageLocked(damage)
```

而不是切换到 PTY event replay 主线。

### 2. 某些 shell prompt 看历史时文本消失

可能原因有几类：

- prompt 行停留在 visible screen 时没有 scroll 出去，所以还没进入 grid store；snapshot/copy-mode 合并 screen 和 history 时边界处理不稳。
- resize shrink 时 prompt 或最新输出被挤出 screen，但没有通过 resize damage 写入 grid store。
- `GridViewport(cols)` 使用了观察者 pane cols，而不是 terminal canonical cols，导致 wrapped group 被错误 reflow/crop。
- wrapped metadata 或 hard-break 判断不完整，logical line 重组时把一部分行当成 continuation 丢掉。

优先修复顺序：

1. resize damage 必须写入 grid store。
2. history/copy-mode 请求必须使用 terminal canonical cols。
3. snapshot/current screen 和 grid history 的拼接边界必须有明确规则。
4. 为 zsh/p10k/starship 等 prompt 增加真实 TUI/tmux harness 回归。

### 3. TUI/runtime 仍会把历史页合并成大 snapshot

`feac69ea` 的 `tuiv2/runtime/snapshot.go` 里，`ApplyGridViewportPage` 会把 page prepend/replace 到 `terminal.Snapshot.Scrollback`，并更新 `ScrollbackLoadedLimit`。

这会导致：

- 用户滚得越多，TUI runtime 的 snapshot 越大。
- copy mode 可能持有完整 frozen snapshot。
- 内存还是会随着历史加载线性增长。

这个问题和 core grid store 方向无关，是 TUI/runtime materialized view 管理方式还没收敛。

目标应改为：

- TUI/runtime 只持有当前 screen + 有限 history window。
- copy mode 持有 viewport cursor/offset/page refs，不持有完整 scrollback clone。
- history page cache 有全局内存预算和 LRU。
- 已加载页面可丢弃，重新从 core `GridViewport` 拉取。

## daemon vterm 轻量化

这件事仍然需要做，而且和 parsed grid history 不冲突。

daemon 的 `core canonical vterm` 应保留，因为它负责：

- 和真实 PTY process 交互。
- 维护当前 screen/cursor/mode/title/cwd。
- 产生 screen update / latest frame。
- 产生 scrollback damage，写入 grid store。
- 处理 resize 后的真实 screen 状态。

但它不能承担无限历史：

- emulator scrollback 应禁用或限制到很小。
- normal history 进入 `terminalGridStore`。
- alternate screen 可保留单独 bounded history。
- snapshot 不能把完整历史拉回 daemon vterm。

`feac69ea` 里已经有正确方向：

```go
vt := vterm.New(cols, rows, liveScrollbackRows(...), ...)
vt.DisableEmulatorScrollback()
```

后续应保持：

```text
daemon vterm = 当前热窗口 + damage producer
terminalGridStore = 历史权威存储
TUI runtime = 观察者缓存，不是历史存储
```

## owner/follower 与历史宽度规则

真实 PTY size 是 terminal 的全局唯一状态。panel、floating window、mobile App、remote-ui 都不是另一份 terminal size，它们只是连接到同一个 terminal 的观察视窗。

历史 materialization 的宽度必须来自 terminal 当前真实 PTY size，也就是 owner 驱动后的 canonical terminal content size，而不是当前观察者 pane/App viewport 的宽度。

规则：

- terminal 全局只有一个真实 PTY size。
- owner resize 成功后，core 更新 terminal size 和 geometry revision。
- grid viewport 的普通 TUI/App 调用必须传 terminal canonical cols。
- panel、floating window、mobile App、remote-ui 如果绘制窗口和真实 PTY size 不一致，只做同一套投影：裁剪、滚动窗口、overflow arrows 和 blank-dot hints。
- 所有观察者的处理方式应该一致；差异只来自本地 viewport 能看到 canonical terminal content 的哪一块。
- follower pane 进入 copy mode 或滚动历史时，不改变 PTY size，也不按自己的 pane/App viewport cols 重新解释历史。
- owner size 变化时，已经打开的 copy mode/history materialized pages 需要失效重取。

`GridViewport(cols)` 能支持任意 cols 是工具能力；普通 terminal workspace 不应把 follower pane width、floating window width 或 App viewport width 传进去。

## remote/protobuf 后续代码怎么合回来

目标不是直接回退到 `feac69ea` 并丢掉 remote 工作，而是：

```text
历史存储基线：feac69ea 的 terminalGridStore 方向
remote/protobuf/app：保留 feac69ea 之后已经需要的后续改动
不要带回：c9d35322/2a611b94 中 raw PTY journal 作为 UI 历史主路径的改动
```

建议合并策略：

1. 从 `feac69ea` 建新工作分支。
2. 保留 `terminal_grid_*` 文件和 `GridViewport` 协议。
3. cherry-pick 或手工重放 remote/protobuf 相关提交：
   - `dd2f0e61 Accept copied v4 pairing payloads`
   - `57692c45 Rewrite remote pairing flow`
   - 其他 remote-ui / termx-app / termx-remote / web-control 纯业务提交。
4. 对 `c9d35322` 只摘取以下思想：
   - `VTerm.ResizeWithDamage`
   - resize 造成的 scrollback append 必须写入历史 store
   - screen update latest/fresh recovery 相关修复
   - 不直接继承 `terminal_event_log` 作为 UI 历史主查询。
5. 更新协议时保留 protobuf/binary transport，但历史消息仍表达 structured grid viewport，而不是 raw PTY replay。

需要特别小心的目录：

- `termx-core/terminal.go`
- `termx-core/stream_screen_state.go`
- `termx-core/snapshot.go`
- `termx-core/terminal_grid_*`
- `termx-proto/wirepb/terminal.proto`
- `internal/protocol/*`
- `tuiv2/runtime/snapshot.go`
- `tuiv2/runtime/resize.go`
- `remote-ui/src/terminal/*`

## 后续修复计划

第一阶段：固定历史主线

- 以 `feac69ea` 的 grid store 为基线。
- 恢复/保留后续 remote/protobuf 必要代码。
- 删除 raw PTY journal 作为 UI 历史主路径的设计和实现。
- 保留 raw PTY log 只作为 debug/audit 可选能力时，不能参与普通 history viewport。

第二阶段：修 resize 吞历史

- 给 `vterm.Resize` 增加或恢复 `ResizeWithDamage`。
- resize shrink 前捕获 visible screen rows。
- resize 后把被挤出的 rows 作为 `damage.ScrollbackAppend`。
- `Terminal.Resize` 调用 `appendGridFromDamageLocked(damage)`。
- 为 `generate_terminal_stress.py --lines 100/1000` + owner resize 增加 TUI/tmux harness。

第三阶段：修 prompt/history 消失

- 增加 zsh prompt、starship/p10k、彩色 SGR、wide char、QR code 的真实 TUI 回归。
- 验证 prompt 从 visible screen 到 grid store 的边界。
- 验证 wrapped/hard-break metadata。
- 验证 copy mode 取 terminal canonical cols。

第四阶段：TUI/runtime 内存收敛

- `ApplyGridViewportPage` 不再把无限 scrollback 合并进 `terminal.Snapshot.Scrollback`。
- 引入 bounded history window/page cache。
- copy mode 状态保存 offset/cursor/page refs，不 clone 全量 snapshot。
- `ScrollbackLoadedLimit` 不再作为“已经永久加载了多少历史”的单调状态。

第五阶段：remote/app 对齐

- remote-ui 继续消费 structured terminal snapshot/grid viewport。
- App/browser 不接收 raw PTY control bytes 作为历史。
- 所有 terminal/file/preview 数据仍走 WebRTC transport。

## 验收场景

必须用真实 TUI 或 tmux harness 验收：

- owner 运行 `python scripts/generate_terminal_stress.py --lines 100` 后 resize，最新 `000100` 行不丢。
- owner 运行 `python scripts/generate_terminal_stress.py --lines 1000` 后 resize，copy mode 能看到尾部历史。
- 彩色 zsh prompt 被推出可视窗口后，copy mode 滚回去颜色和文本都存在。
- `go run ./termx-cli/cmd/termx remote pair` 的 QR code 被推出窗口后，copy mode 滚回去仍完整。
- 大平铺 pane 和小 floating pane 连接同一 terminal，floating takeover owner 后，两个 pane 的历史都按 owner/canonical size 展示。
- follower pane 进入 copy mode 不改变 PTY size，不按 follower pane width 重排历史。
- TUI 退出再进入，owner/follower 恢复后历史宽度仍按 terminal canonical size。
- 连续 resize 80x24 -> 120x30 -> 60x20，不丢 resize 前后边界行。
- remote-ui/app 通过 WebRTC 查看同一 terminal，历史和 TUI 看到的 structured rows 一致。

## 当前建议

下一步不要继续在 `2a611b94` 的 raw journal WIP 上扩展。

建议新建分支：

```bash
git switch -c grid-history-rebase feac69ea
```

然后按模块合回 remote/protobuf 代码，并优先实现 resize damage 写入 `terminalGridStore`。

如果需要临时保留当前 WIP，已经有：

```text
2a611b94 WIP terminal history direction checkpoint
```

它可以作为参考和回退点，但不应作为历史主线继续开发。
