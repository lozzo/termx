# Panel Content 深化计划

状态：已完成
日期：2026-06-10

本文档描述 panel content 深化已执行的目标、切片顺序和验收口径。

实现对应 `workflow.md` 切片 193-204；后续 VT EventRouter 是独立阶段，不属于本文档的 panel content 完成口径。

## 1. 目标

后续工作的目标不是继续调 pane 边框，而是把 pane 内部内容做成可信的 content renderer。

最终用户在一个 panel 中应该能看到：

- 实时模式下的 terminal live 内容。
- 历史/Copy 模式下来自 core-v2 authoritative `HistoryWindow` 的内容。
- resize 后按新 content rect 正确展示的 live 内容和 history 内容。
- 空白区域有轻量占位，不再看起来像透明或渲染缺失。
- 内容被裁切时有明确符号提示，例如右侧裁切用 `>`，下方裁切用 `v`。
- live 更新可以合并和丢中间帧，只保证 TUI 最终展示 core-v2 的最新 terminal 投影。
- history 内容要尽可能还原真实终端历史，不得长期停留在纯文本行；颜色、样式、宽度、链接等格式需要进入 core-v2、protocol 和 TUI 渲染链路。

## 2. 基本原则

### 2.1 content rect 是唯一绘制范围

panel content renderer 只能画在 framework 分配给它的 content rect 里。

例子：

```text
┌ shell ────────────────┐
│content starts here    │
│only this area is owned│
└───────────────────────┘
```

content renderer 不能覆盖 pane 顶边、右边框、底边，也不能自己猜测外部 terminal 的总 cols/rows。

### 2.2 live 和 history 共用 content 视觉规则

live 内容来自 `TerminalSurfaceStore`。

history 内容来自 core-v2 authoritative `HistoryWindow`。

二者数据来源不同，但在 panel 内应该共用这些视觉规则：

- 行宽按 content rect 裁切。
- 宽字符、emoji、combining mark 不能撑破边框。
- terminal extent 外区域使用统一占位策略。
- terminal 内部普通空白仍是空白 cell，不强行画成小圆点。
- 横向/纵向遮挡由 content renderer 输出 overflow hint，再由 pane/floating chrome 画 `>` / `v`。

### 2.3 小圆点只表示 terminal extent 外区域

这里要对齐 `tuiv2` 的语义：小圆点不是普通短行 padding，也不是把整个 content rect 的空白都点满。

只读参考里，`tuiv2/render/snapshot_render_helpers.go` 的 extent hint 会先确定 terminal 的实际 extent，再只在 extent 外画 `·`；`tuiv2/render/pane_render_projection.go` 的 overflow hint 则单独判断 `>` / `v`。

定义：

- `content rect`：pane/floating 分配给 terminal 内容的可绘制区域。
- `terminal extent`：live surface / snapshot 当前实际 terminal 尺寸或可见尺寸。
- `·`：content rect 内、terminal extent 外的弱占位。
- `>` / `v`：terminal extent 或 pane 自身被当前 viewport 裁切时的遮挡提示，属于 chrome marker，不属于普通 terminal cell。

默认情况下 terminal extent 锚在 content rect 左上角。

例子：pane content 是 `10x10`，terminal 实际是 `5x5`。

```text
content rect: 10x10
terminal extent: 5x5

render content:
  "abcde·····"
  "fghij·····"
  "klmno·····"
  "pqrst·····"
  "uvwxy·····"
  "··········"
  "··········"
  "··········"
  "··········"
  "··········"
```

这里右侧和底部是 terminal extent 之外，所以显示小圆点。

如果后续支持居中或偏移，点也应该跟随 terminal extent 的实际位置，而不是固定只在右下角。

```text
content rect: 4x3
terminal extent: 2x1
terminal offset: x=1,y=1

render content:
  "····"
  "·hi·"
  "····"
```

遮挡符号是另一件事。

```text
content rect: 10x5
terminal extent: 12x8

visible content:
  前 10 列、前 5 行 terminal cell

overflow hints:
  right=true
  bottom=true

chrome:
  右边界画 ">"
  底边画 "v"
```

普通 terminal 内部空白不画小圆点。例如 terminal extent 本身就是 `20x5`，第二行只有 `ok`，后面的 18 个 cell 仍然是普通空白，不是 `ok····`。

### 2.4 history truth 不能从 live 推断

Copy/History 模式仍然只消费 authoritative `HistoryWindow`。

错误例子：

```text
copy mode 缺 history.window
  -> 从 live surface 当前屏幕拼一份历史
```

这是禁止的。

正确例子：

```text
copy mode 缺 history.window
  -> 显示 pending
  -> 请求 core-v2 latest window
  -> response token/cols 匹配后再渲染
```

### 2.5 live 是 latest-wins 投影，不要求逐帧显示

live 内容本质上是 core-v2 当前 terminal surface 的投影。TUI 不需要展示每一个中间状态，只需要在可绘制时展示最新状态。

只读参考：

- 旧 `termx-core/attachment_stream.go` 已有 ready/ack、`forceLatestScreen`、替换 queued screen update 和按需拉最新 screen 的思路。
- 旧 `tuiv2/app/cursor_writer.go` 已有 pending frame 覆盖、batch flush 和必要 reset/resize frame 立即刷新的思路。

v3 后续实现要迁移这个产品语义，而不是复制旧 runtime：

- terminal live update 可以按 terminal id 合并，队列积压时保留最新 surface revision，丢弃旧的 live snapshot。
- render/frame sink 忙时不反压 core-v2 输出到不可控积压，只记录 dirty/latest，再在下一次可绘制机会渲染最新投影。
- resize、terminal switch、reset baseline、exit/error、attach/detach 这类语义边界不能被普通 live 帧覆盖掉；它们要么立即刷新，要么强制让下一帧以新 baseline 渲染。
- stale live revision 不能覆盖较新的 live surface。
- 最终帧必须对应当前 terminal id、当前 content rect 和最新已接受 revision。

例子：后端快速输出 1000 次。

```text
core-v2 live revisions:
  r1, r2, r3, ..., r1000

TUI 正在忙:
  可以丢弃 r1-r999 的中间绘制请求

下一次 render:
  使用 r1000 的 terminal surface
  frame 里能看到最新屏幕状态
```

### 2.6 history 必须保留终端格式

history 是长期数据，不是 live screen 的临时截图；越接近真实终端历史，copy/search/preview 才越可信。

当前明确缺口：

- `termx-core-v2/history.Cell` 目前只有 `Text`。
- `termx-core-v2/history.VisualRow` 目前只有 `Text`。
- `termx-core-v2/protocol_service.go` 当前把 history row 输出为 `protocol.CompactRow{Text: row.Text}`。

这说明现在的 history path 只能还原纯文本，不能还原真实格式。

后续目标：

- core-v2 logical line payload 要能表达结构化 cell 或 run：content、display width、foreground/background、bold/italic/underline/blink/reverse/strikethrough、link url/params。
- `HistoryWindow.Rows` 不能只携带 `Text`；需要携带可投影后的 styled cells/runs，同时保留 plain text 作为搜索、复制、测试快照的派生视图。
- protocol 已有 `CompactRow` / `CompactRowRun` / `CompactRowCell` 能表达 style、width 和 link；core-v2 history window 应优先输出这些结构，而不是降级成 `Text`。
- TUI copy/history renderer 要从 `CompactRow.DecodeCells()` 或等价结构生成 styled content cell；palette index、truecolor 和 SGR attrs 不能被误映射成 TermX theme token。
- plain text 可以继续存在，但只能作为 copy/search 辅助，不是渲染 truth。

例子：

```text
真实历史 logical line:
  cells:
    "ERR" fg=ansi:1 bold=true
    " "   default
    "build failed" fg=#ffcc00 underline=true

HistoryWindow:
  row keeps cells/runs + style metadata
  plain text derivative: "ERR build failed"

TUI render:
  "ERR" 仍是红色/粗体
  "build failed" 仍是黄色/下划线
```

## 3. 建议切片顺序

### 3.1 切片 A：ContentViewport 合同

先定义内容渲染的输入合同。

要做的事：

- 明确 `ContentVM + Rect` 如何生成 `ContentRenderRequest`。
- 让 content renderer 知道当前可用 `cols` 和 `rows`。
- 定义 terminal extent、普通空白、extent 外小圆点和 overflow hint 如何表达。
- 让 live、copy、empty、exited、placeholder 都能用同一套辅助函数。

例子 1：普通短行不画小圆点。

```text
content rect: 20x5
terminal extent: 20x5
input lines:
  "123456789012345678901234"
  "ok"

render content:
  row 1: "12345678901234567890"
  row 2: "ok" + 18 个普通空白 cell
  row 3: 20 个普通空白 cell
  row 4: 20 个普通空白 cell
  row 5: 20 个普通空白 cell

overflow hints:
  right=true
```

这里 `>` 不写进 content cell，而是交给 pane/floating chrome 在右边界画。

例子 2：terminal extent 小于 content rect 时，才画小圆点。

```text
content rect: 10x6
terminal extent: 5x3

render content:
  "abcde·····"
  "fghij·····"
  "klmno·····"
  "··········"
  "··········"
  "··········"
```

验收：

- 单行或 terminal extent 超宽会返回 right overflow hint，并在 chrome 右边界显示 `>`。
- terminal extent 超高会返回 bottom overflow hint，并在 chrome 底边显示 `v`。
- terminal extent 小于 content rect 时，右侧和底部的 extent 外区域显示小圆点。
- 普通短行、普通空行和 terminal 内部 blank cell 不被点满。
- 宽字符被裁切时不会留下半个字符 footprint。
- ANSI styled cell 被裁切后仍有 reset，不污染边框。

### 3.2 切片 B：Terminal Live 内容深化

在 ContentViewport 合同稳定后，先做 live 内容。

要做的事：

- 使用 `TerminalSurfaceStore.Screen` 优先渲染 live cells。
- 没有 cell screen 时，再使用现有 live lines fallback。
- 保留 ANSI foreground/background/bold 等样式。
- cursor 只落在 content rect 内。
- pending/empty/exited/error 状态仍然显示明确内容。
- 如果 live surface / terminal extent 比 content rect 宽，返回 right overflow hint，右侧 chrome 显示 `>`。
- 如果 live surface / terminal extent 比 content rect 高，返回 bottom overflow hint，底部 chrome 显示 `v`。
- 如果 live surface / terminal extent 比 content rect 小，extent 外区域显示小圆点。
- live update 按 terminal id 和 surface revision 合并，render 忙时可以丢弃中间 live 帧。
- 最终渲染以最新 accepted live surface 为准，旧 revision 不得覆盖新 revision。
- resize/reset/exit/attach 等语义边界不被普通 live coalescing 吞掉。

例子 1：右侧裁切

```text
content rect: 10x3
terminal extent: 14x3
live row:
  "lozzow@RedmiBook ~/Documents/workdir/termx"

render content:
  "lozzow@Re"
  10 个普通空白 cell
  10 个普通空白 cell

overflow hints:
  right=true
```

例子 2：terminal 比 pane 小

```text
content rect: 10x6
terminal extent: 5x3
live rows:
  "abcde"
  "fghij"
  "klmno"

render content:
  "abcde·····"
  "fghij·····"
  "klmno·····"
  "··········"
  "··········"
  "··········"
```

例子 3：底部裁切

```text
content rect: 12x3
terminal extent: 12x4
live rows:
  "row 1"
  "row 2"
  "row 3"
  "row 4"

render content:
  "row 1" + 7 个普通空白 cell
  "row 2" + 7 个普通空白 cell
  "row 3" + 7 个普通空白 cell

overflow hints:
  bottom=true
```

例子 4：宽字符

```text
content rect: 6x2
live row:
  "你好world"

render content:
  "你好wo"
  6 个普通空白 cell

overflow hints:
  right=true
```

例子 5：live update 合并

```text
terminal id: term-main
incoming live revisions:
  41: "build 1%"
  42: "build 2%"
  ...
  99: "build done"

TUI frame sink 忙:
  pending live projection 被替换为 revision 99

render:
  只需要显示 "build done"
```

验收：

- `go test ./termx-tui-v3/render` 覆盖裁切、占位、ANSI、宽字符。
- `go test ./termx-tui-v3/...` 覆盖 live projection、cursor、surface revision stale guard 和 live coalescing。
- burst live update harness 证明大量连续更新后队列有界、最终帧展示最新 revision。
- resize/reset/exit/attach 语义边界不会被 live coalescing 丢掉。
- tmux ANSI smoke 仍通过。

### 3.3 切片 C：Live resize 行为验收

live 内容做好后，再做 resize 验收。

要做的事：

- 外部 viewport resize 后重新测量 layout。
- active pane content rect 改变时，只向 core-v2 发送 content rect 尺寸。
- 重复尺寸不重复发送 resize。
- live surface 更新回来后按新 rect 展示。

例子：

```text
外部窗口: 120x40
header/footer/pane chrome 扣除后
active content rect: 118x36

发送给 core-v2:
  resize cols=118 rows=36
```

缩小窗口后：

```text
外部窗口: 80x24
active content rect: 78x20

发送给 core-v2:
  resize cols=78 rows=20
```

验收：

- fake runtime 测试证明 content rect 变化触发 resize。
- tmux resize smoke 证明 PTY 内 `stty size` 等于 content rect。
- frame 每一行宽度仍等于 viewport width。

### 3.4 切片 D：History styled payload 合同

live 稳定后，先补 history 格式合同，再做 copy/history UI。

要做的事：

- 将 core-v2 logical line payload 从纯 `Text` cell 扩展为能表达 terminal cell/run 的结构。
- 让 projection 在 wrap/reflow 后保留 style、width、link 和 logical line metadata。
- 让 `HistoryWindow.Rows` 携带 styled cells/runs，并保留 plain text derivative。
- 让 protocol adapter 使用 `CompactRowFromCells` 或等价结构输出 history rows，不再只写 `CompactRow{Text: row.Text}`。
- 保持 token、generation、cursor、logical line boundary 和 stale guard 语义不变。

例子：

```text
core-v2 logical line:
  cells:
    {text:"OK", fg:"ansi:2", bold:true}
    {text:" ", width:1}
    {text:"deploy", fg:"#80ffea", link:"file://deploy.log"}

protocol history row:
  CompactRow.Runs or CompactRow.Cells contains style/link metadata

TUI ContentVM:
  Line.Cells includes ANSIStyle/link metadata
```

验收：

- core-v2 history window 测试覆盖 styled logical line reflow 后 style 不丢。
- protocol binary encode/decode 覆盖 history `CompactRow` style、width、link round trip。
- tui-v3 copy/history projector 覆盖 styled history row 渲染，不把 `ansi:N` 重映射为 theme token。
- plain text copy/search 仍可从 styled row 派生。

### 3.5 切片 E：Copy/History 内容深化

styled history payload 合同稳定后，再做 history UI。

要做的事：

- 继续只渲染 `HistoryStore.Rows`，但 row 内容必须是 authoritative styled cells/runs 的投影结果。
- 使用 logical line marker 表达同一 logical line 的不同 visual row。
- selection/search/cursor 都按 authoritative row 工作。
- 历史行超宽时返回 right overflow hint，chrome 右边界显示 `>`。
- history window 未覆盖上下边界时显示 `v` 或状态 token。
- history 普通短行不画小圆点；只有存在 terminal-like extent 外区域时才复用 live 的小圆点策略。
- 渲染时保留 history row 的 ANSI palette index、truecolor、bold/italic/underline/reverse/blink/strikethrough 和 link metadata。

例子：

```text
HistoryWindow rows:
  line 100 row 0: "git status"
  line 101 row 0: "modified: a" fg=ansi:3
  line 101 row 1: "modified: b" fg=ansi:3 underline=true

render:
  "● git status"
  "● modified: a"
  "╎ modified: b"
```

这里：

- `●` 表示 logical line 起始 visual row。
- `╎` 表示同一 logical line 的 continuation row。

例子：顶部/底部还有更多历史

```text
render:
  "⌕ search rows:500"
  "⇡ older content"
  "● current row"
  "● current row 2"
  "SCROLL █ 120-140/500"
```

验收：

- 缺 authoritative window 时显示 pending，不从 live fallback。
- token/cols/terminal mismatch 时不渲染旧 history。
- styled row、ANSI palette、truecolor、宽字符 width 和 link metadata 不丢。
- selection/search 不被占位符和遮挡符号破坏。
- mouse hit region 仍指向 authoritative row。

### 3.6 切片 F：History resize rebind

history renderer 做好后，再处理 resize rebind。

要做的事：

- copy mode 打开时，如果 content cols 变化，旧 `HistoryWindow` 立即 invalid。
- 请求 core-v2 latest replace，使用新 cols。
- 等待期间显示 pending。
- 旧 cols response 到达时拒绝。
- 新 response 到达后重新渲染。

例子：

```text
copy mode bound cols=100
resize 后 content cols=80

state:
  old window invalid
  bound token cleared
  request latest cols=80

UI:
  "copy history pending: history cols changed"
```

验收：

- resize 后旧 rows 不再继续显示。
- stale response 不会覆盖新 window。
- cursor/selection 的策略明确：要么重置，要么按 logical boundary 重新定位；不能半隐式保留。

### 3.7 切片 G：Panel content 总验收

最后做一次总验收。

要覆盖：

- 单 pane live。
- 双 pane live + inactive placeholder。
- split resize 后 live 裁切。
- copy mode pending。
- copy mode loaded styled rows。
- floating pane content。
- exited pane。
- error pane。
- live burst update latest-wins。

例子：

```text
左 pane live:
  content: "top output" + 普通空白
  chrome: right overflow marker ">"

右 pane copy:
  "⌕ search rows:40"
  "● git log" keeps original history style cells

floating:
  "terminal exited code:0"
```

验收：

- `go test ./termx-tui-v3/... -count=1`
- `go test ./termx-cli/cmd/termx -run 'TestV3VisualSnapshot|TestV3SmokeCommandIncludesVisualReviewCases|TestV3TmuxVisualCompareCapturesTargetAndDiffArtifacts' -count=1`
- `make test-cli-v3-tmux-visual-compare`
- 必要时新增 tmux content snapshot 场景。

## 4. 明确不在第一晚做的事

第一晚不建议做这些：

- 不改 core-v2 的 logical line truth。
- 不把 history 从 live surface 拼出来。
- 不一次性重写所有 copy mode 交互。
- 不重做 pane chrome、header/footer 或 Terminal Picker。
- 不把 renderer 改成直接读 runtime、service 或 protocol client。
- 不在缺 authoritative history window 时伪造历史内容。
- 不把 history 格式保真做成 TUI 侧猜测；格式必须从 core-v2 authoritative history payload 传下来。
- 不为了性能丢掉 resize/reset/exit/attach 这类语义边界；只允许丢普通 live 中间帧。

## 5. 推荐夜间执行顺序

如果用户审核通过，建议按这个顺序自动推进：

1. ContentViewport 合同。
2. Terminal Live 内容深化。
3. Live resize 行为验收。

这三步完成后，live panel 先具备稳定内容面板能力，并且具备 latest-wins 的性能语义。

第二轮再做：

1. History styled payload 合同。
2. Copy/History 内容深化。
3. History resize rebind。
4. Panel content 总验收。

## 6. 回滚点

进入上述实现前，应该在计划文档提交后打 tag。

建议 tag：

```text
pre-panel-content-20260610
```

该 tag 表示：

- Terminal Picker 与 Create Terminal 表单已完成当前复核状态。
- workdir 候选 popup 已完成最高层、tree 风格和左右键导航。
- panel content 深化尚未开始。

如果夜间实现出现方向问题，可以回到这个 tag 重新拆分。
