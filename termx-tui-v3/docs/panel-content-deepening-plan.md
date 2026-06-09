# Panel Content 深化计划

状态：待用户审核
日期：2026-06-10

本文档只描述后续要做什么、为什么这样排顺序、验收时看什么。

本文档不表示已经开始实现。用户审核通过前，不进入下面任何实现切片。

## 1. 目标

后续工作的目标不是继续调 pane 边框，而是把 pane 内部内容做成可信的 content renderer。

最终用户在一个 panel 中应该能看到：

- 实时模式下的 terminal live 内容。
- 历史/Copy 模式下来自 core-v2 authoritative `HistoryWindow` 的内容。
- resize 后按新 content rect 正确展示的 live 内容和 history 内容。
- 空白区域有轻量占位，不再看起来像透明或渲染缺失。
- 内容被裁切时有明确符号提示，例如右侧裁切用 `>`，下方裁切用 `v`。

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

验收：

- `go test ./termx-tui-v3/render` 覆盖裁切、占位、ANSI、宽字符。
- `go test ./termx-tui-v3/...` 覆盖 live projection 和 cursor。
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

### 3.4 切片 D：Copy/History 内容深化

live 稳定后再做 history。

要做的事：

- 继续只渲染 `HistoryStore.Rows`。
- 使用 logical line marker 表达同一 logical line 的不同 visual row。
- selection/search/cursor 都按 authoritative row 工作。
- 历史行超宽时返回 right overflow hint，chrome 右边界显示 `>`。
- history window 未覆盖上下边界时显示 `v` 或状态 token。
- history 普通短行不画小圆点；只有存在 terminal-like extent 外区域时才复用 live 的小圆点策略。

例子：

```text
HistoryWindow rows:
  line 100 row 0: "git status"
  line 101 row 0: "modified: a"
  line 101 row 1: "modified: b"

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
- selection/search 不被占位符和遮挡符号破坏。
- mouse hit region 仍指向 authoritative row。

### 3.5 切片 E：History resize rebind

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

### 3.6 切片 F：Panel content 总验收

最后做一次总验收。

要覆盖：

- 单 pane live。
- 双 pane live + inactive placeholder。
- split resize 后 live 裁切。
- copy mode pending。
- copy mode loaded rows。
- floating pane content。
- exited pane。
- error pane。

例子：

```text
左 pane live:
  content: "top output" + 普通空白
  chrome: right overflow marker ">"

右 pane copy:
  "⌕ search rows:40"
  "● git log"

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

## 5. 推荐夜间执行顺序

如果用户审核通过，建议按这个顺序自动推进：

1. ContentViewport 合同。
2. Terminal Live 内容深化。
3. Live resize 行为验收。

这三步完成后，live panel 先具备稳定内容面板能力。

第二轮再做：

1. Copy/History 内容深化。
2. History resize rebind。
3. Panel content 总验收。

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
