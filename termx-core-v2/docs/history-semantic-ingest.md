# core-v2 history semantic ingest 设计说明

状态：R430 后本文是 pre-screen-buffer 语义 ingest 背景。当前 production ingest
truth 已切到 `ScreenHistoryBuffer` physical rows/cells；logical line 不再是
默认写入阶段的正文 truth，只在 HistoryWindow/Copy/Search 阶段由 physical rows
投影，或作为显式 legacy harness 的 mutation-backed payload 使用。

## 1. 目标

本文原目标是把 PTY 输出里的终端语义转换成 `HistoryEvent`，再写入
`HistoryTrack` / `LogicalLineStore`。R430 后这条链路已降级为旧
mutation-backed 迁移背景；当前默认目标是把同一 vterm semantic transaction
写入 `ScreenHistoryBuffer`，由 physical row/cell state 和 sealed row backend
持有 authoritative history truth。

本设计替代继续扩展手写 `historyANSIParser` 的方向。`historyANSIParser` 可以在迁移期保留为局部文本/style parser 或 fallback harness 对照，但不再作为终端语义来源继续叠补丁。

需要由 vterm 理解的语义包括：

- CSI/OSC 和 private mode。
- cursor movement、absolute cursor、tab、CR/LF、auto-wrap。
- scroll region、index/reverse index、insert/delete line、SU/SD。
- ED/EL、styled blank、SGR、OSC8 link。
- mouse/focus/synchronized output mode。
- alt-screen enter/exit。

## 2. 不变边界

- 默认 production history truth 的基本单位是 physical row/cell；logical line 是查询/复制/search 阶段的 projection。
- vterm scrollback 不能直接成为 history truth。
- live snapshot、vterm screen、damage rect 不能被 `HistoryTrack` 反向读取来推导 committed history。
- semantic ingest 使用的 cols/rows 必须等于真实 PTY size，不能为了“保存更多内容”使用更高的假终端。
- copy/history window 只能来自 core-v2 authoritative history window；默认由 screen-backed physical rows 投影生成。
- resize 不重写 committed history，只使投影/window token 失效，并按既有模型 reclaim 或 hide mutable frontier。

## 3. vterm 归属

目标实现不需要单独再起一个 history 专用 vterm。更合理的方向是把 core 里已有的 terminal vterm 提升为 EventRouter 的唯一 semantic source：

1. PTY bytes 进入 core-v2 后，只写入一次 vterm。
2. vterm 在同一个 write transaction 内产出 semantic damage batch。
3. EventRouter 按同一个 input sequence 把 batch 分发给 history projector 和 live projector。
4. history projector 只消费 batch 中的终端语义，转换成 `HistoryEvent`。
5. live projector 用同一批语义更新 latest screen / snapshot。

这里的关键限制是：共用 vterm 不等于 history 可以从 live surface 读数据。`HistoryTrack` 不能调用 live snapshot、screen rows、vterm scrollback 或 grid viewport。允许共享的是“同一批 PTY bytes 解码后的有序语义事件”，不是 live 投影结果。

如果迁移期暂时保留现有 `live.SurfaceTrack` 包装，必须先把 vterm write/damage 从 live-only 路径抽到 EventRouter 边界，再让 live 和 history 同源消费。不能让 history 在 live 更新完成后再从 `SurfaceTrack.Snapshot()` 反推历史。

## 4. semantic batch

vterm-backed ingest 每次处理一批 PTY bytes 后，应产出一个只描述终端语义的 batch。batch 可以引用 vterm damage API，但不能直接暴露为 history storage contract。

建议 batch 至少包含：

- input sequence。
- PTY cols/rows。
- before/after modes：尤其是 alt-screen、mouse tracking、focus tracking、synchronized output、autowrap。
- before/after cursor。
- primary scroll-out rows：真实 primary screen 滚出可见区的 rows，带 wrapped、style、link、tail fill 信息。
- primary screen ops：write span、clear、clear-to-EOL、scroll rect、copy rect、cursor、modes。
- alternate screen ops 和 alternate append。
- full replace/stale 标记。

`RequiresFullReplace` 只能表达 live surface 需要重建或客户端投影 stale，不能等价为“把当前屏幕全写入 history”。

## 5. projector 规则

history projector 是 vterm semantic batch 到 `HistoryEvent` 的唯一转换层。它不保存第二份历史 truth。

### 5.1 普通 primary 输出

- 文本写入当前 mutable logical line。
- LF seal 当前 logical line，但不必立即 committed。
- logical line 只有离开 primary screen ownership 后才 committed。
- primary scroll-out row 只能驱动“该 terminal 行已经离开 screen ownership”的事实，最终仍要合并成 logical line 再提交。
- wrapped row 只用于重建当前 PTY size 下的 logical line 边界，不能作为存储主键或独立 history truth。
- CR、backspace、absolute cursor、EL、局部 clear 只 mutate mutable frontier，不直接生成 committed history。

### 5.2 primary-screen TUI

Codex 这类程序不能简单归类成“全屏只保最后一帧”。正确边界是 primary scrollback semantics 与 current-frame repaint semantics：

- 进入 primary-screen TUI 前的 shell 页面按 page-break/ownership 规则提交。
- TUI 运行期间，如果 vterm 在 primary 语义上产生真实 scroll-out，这些内容应进入 logical-line history，且只能提交一次。
- TUI 运行期间，底部 input/status、局部 UI、spinner、按钮等 repaint 内容属于 current mutable frame。
- 重复 repaint 必须替换 current frame，不得把每一帧追加到 committed history。
- latest/frozen window 必须能看到 current mutable frame，包括底部 input/status 行，但这些行 `Committed=false`，不计入 committed history depth。
- process exit 或 force commit 时，最后一帧才按策略进入 history。

因此实现不能按进程名硬编码 Codex/opencode，而应按终端语义分类：

- primary scrollback append -> logical-line history。
- primary screen direct damage / repaint -> mutable current frame。
- alt-screen damage -> 不写 committed primary history；当前可见 frame 可作为 transient latest frame。

### 5.3 alt-screen / 当前屏幕应用

opencode 或其他 alt-screen/近似 alt-screen 应用运行中不写 primary committed history。

- enter alt-screen 前必须先提交 primary page。
- alt-screen 内部 scroll、direct damage、alternate append 不进入 committed primary history。
- running/exit alt-screen 的当前可见 frame 作为 transient latest/frozen frame 暴露，`Committed=false`，不增加 committed depth，后续 primary 输出会替换它。
- 进程死在 alt-screen 时，不凭空合成未输出的内部历史。

## 6. 小高度终端

当真实 PTY 高度只有 1-2 行时，semantic ingest 仍必须使用真实高度。

- 普通 primary 输出：真实滚出的 logical lines 进入 committed history。
- Codex primary-screen TUI：真实 primary scroll-out 的 transcript/context 进入 history；当前 input/status frame 只作为 mutable current frame。
- opencode/alt-screen TUI：展示当前屏幕语义，运行中不写 committed primary history；
  运行中或退出时的当前 frame 可以作为 transient latest/frozen frame，退出时 live 默认恢复 primary。

不能为了让 history “看起来更多”把 semantic vterm rows 设大。假高度会改变 scroll region、reverse index、wrap、cursor position 和 mouse hit testing，产生假的历史。

## 7. styled blank 和背景 footprint

styled blank 是终端语义，不是普通文本空格的简单变体。projector 必须区分：

- 行尾背景延伸：用 tail fill 表达，不能把整行默认空格写成 logical payload。
- EL/ED 产生的带背景空白：只在有可见 style footprint 时保留。
- 后续真实文本写入同一 logical line 时，必须清掉 stale tail fill。
- default blank 不应生成历史内容。
- full replace 或 clear 不能把整屏 blank 作为 committed history 写入。

这个边界用于避免 copy/history 中出现异常黑块、空白行膨胀、底部文本缺失或 resize 后多出背景块。

## 8. 迁移步骤

推荐按下面顺序推进：

1. 增加 core-v2 harness，使用 tmux/Codex raw dump 或最小等价序列锁住语义。
2. 在 EventRouter 边界引入 shared vterm semantic batch，让 live/history 消费同一批解码结果。
3. 为 primary scroll-out 建立 row-to-logical-line projector。
4. 为 primary-screen TUI 建立 current mutable frame projector。
5. 收口 alt-screen 当前帧边界：running/exit frame 只进 transient latest/frozen，不进 committed primary history；live replay 仍由显式调试策略控制。
6. 用新 projector 替换 `historyANSIParser` 的 terminal semantic 职责。
7. 删除或收缩 `historyANSIParser`，只保留仍有必要的文本/style 辅助逻辑。

## 9. 必备 harness 不变量

后续实现必须至少覆盖这些不变量：

- rows=1/2 的普通 primary 输出：sealed line 只有离开 screen ownership 后才 committed；open tail/latest 不重复出现在 older。
- Codex primary raw：进入 Codex 前的 shell/context 保留；运行 frame 在 latest/frozen 可见但不增加 committed depth。
- Codex 重复 repaint：旧输入框不会进入 older，也不会连续出现在 latest。
- primary scroll region + reverse index + absolute cursor：不会破坏已 committed 的 pre-existing lines。
- primary scroll-out：vterm primary scrollback append 只转成 logical-line history 一次，wrapped rows 合并为同一 logical line。
- styled blank：行尾背景 footprint 保留，但 default blank 不写成历史；后续真实写入清掉 stale tail fill。
- alt-screen：alternate append/screen ops 不进 committed primary history；running/exit 当前 frame 只保留一份 transient latest/frozen；进程死在 alt-screen 不合成额外历史。
- latest/history window：running primary-screen current frame 的底部 input/status 行可见，且 `Committed=false`。

## 10. 禁止事项

- 不按进程名硬编码 Codex、Claude Code、opencode。
- 不用更高的 fake rows 保存隐藏内容。
- 不从 live snapshot、vterm scrollback 或 TUI 本地 cache 反推 copy/history truth。
- 不把 `RequiresFullReplace` 当作 history append。
- 不把每次 fullscreen repaint append 到 committed history。
- 不在 `HistoryTrack` 里引入第二套 visual-row truth。
