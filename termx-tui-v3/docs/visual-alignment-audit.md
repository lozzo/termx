# TUI-v3 视觉差距审计与验收基线

## 1. 文档目的

本文档是切片 79 的视觉基线文档，用来纠正一个关键判断：当前 `termx-tui-v3` 已经具备 styled chrome renderer 和第一版可操作产品壳，但还没有达到用户要求的 `tuiv2` 截图级界面效果。

后续切片 80-83 必须以本文档作为视觉验收入口。实现仍必须遵守 `termx-tui-v3/docs/architecture.md` 与 `termx-tui-v3/docs/render-architecture.md`，不得因为追求视觉相似而复制旧 `tuiv2` runtime/model、Bubble Tea contract、snapshot/grid history fallback 或旧 renderer 大状态结构。

## 2. 视觉目标来源

目标来自用户提供的 `tuiv2` 截图和补充描述：

- 默认界面应有类似 `tuiv2` 的整屏 header/footer，而不是稀疏文字条。
- pane 应使用 square Unicode 细线 chrome，边框连续，顶部 title/state/action 槽位完整。
- active pane 应有明显 accent，inactive pane 应 muted，不应只靠标题文字表达焦点。
- panel 支持 card panel 和 tmux-like split line 两种视觉呈现。
- header/footer 可隐藏，隐藏后 body 真实回收空间。
- floating window 保持独立带边框 card。
- toast/message 应像现代 CLI/TUI 的右上角实体弹出消息，不只是简单文本。
- modal/overlay 使用实体前景卡片，不要求灰度遮罩；中文、emoji、CJK、combining mark 优先保证可见和宽度正确。
- UI chrome 必须能承载真实 terminal 内容、copy-history、Terminal Pool、Workbench Tree、Prompt/Help，而不被内容冲破。

## 3. 当前实现事实

当前已经具备：

- `RenderResult` 单一路径、styled cells、ANSI frame adapter 和 width-safe helper。
- reducer-owned viewport，固定 viewport 下 frame 行数和每行 display width 可以稳定。
- card/split panel、header/footer hide、toast lifecycle、overlay、floating、Terminal Pool、Workbench Tree、Prompt/Help、Tab/Workspace 的第一版产品壳。
- `go run ./termx-cli/cmd/termx v3 smoke` 可输出多个非交互 smoke case。

当前仍不足：

- 当前 smoke 和默认入口的视觉密度、槽位、状态表达和整体层级仍不像用户给出的 `tuiv2` 截图。
- 有 Unicode 线框、ANSI 颜色和可操作命令，并不等于视觉对齐完成。
- 文档中所有“styled chrome 已达到 tuiv2 截图级视觉等级”的历史表述都应按“基础结构已落地，但视觉仍需返工”理解。

## 4. 固定 viewport smoke 基线

切片 79 新增 `termx v3 smoke` case：

- case 名称：`visual-audit-current`
- 固定 viewport：`120x40`
- 覆盖元素：header、footer、split tiled pane、active/inactive pane、floating card、toast、emoji/CJK 宽度安全文本。
- 目的：记录当前 visual gap 的稳定现状基线，供切片 80-83 对比。

该 case 是“当前不足的审计快照”，不是目标完成快照。后续实现如果让该 case 更接近目标，应同步更新本文档或相关验收说明。

## 5. 差距清单

### 5.1 Top Bar

当前问题：

- 信息密度不足，和 `tuiv2` 的 top bar / tab strip 感受不同。
- workspace、tab、active pane、terminal/floating summary、mode/status token 的槽位不够稳定。
- 背景虽有 style，但视觉上仍像拼出来的文本段，不像完整产品栏。

目标要求：

- 整行稳定 top bar，背景填满。
- 左侧显示 workspace 与 tab strip，active tab 与 inactive tab 清晰区分。
- 中间和右侧提供短状态 token，例如 active pane、terminal count、floating count、owner/status/action。
- 窄屏时按优先级退化，而不是随机裁掉关键上下文。

### 5.2 Bottom Bar

当前问题：

- footer hints 已可用，但视觉层级和 `tuiv2` 底栏差距明显。
- mode token、快捷键 token、active target、右侧 summary 没有形成稳定产品栏。

目标要求：

- 整行稳定 bottom bar，背景填满。
- 左侧突出当前 mode，中间显示 mode-specific shortcut token，右侧显示 workspace/pane/terminal summary。
- copy mode、overlay、Prompt/Help、Terminal Pool、Workbench Tree 都要有专属 footer hints。
- header/footer hide 后 body 回收空间，必要上下文通过 toast、短标识或 Help 入口可恢复识别。

### 5.3 Pane Chrome

当前问题：

- pane chrome 已有 square glyph 和 ANSI accent，但视觉密度、title/state/action 槽位仍不足。
- active/inactive 的差异在 smoke 中存在，但真实观感不够接近 `tuiv2` 截图。
- split line 与 card panel 需要统一 slot 语义，避免“线是连续的，但不像产品面板”。

目标要求：

- square 细线连续边框：`┌┐└┘─│├┤┬┴┼`。
- 顶边 title、state、action 槽位稳定，title/action 之间必须保留线段。
- active pane 使用 accent / strong style，inactive pane 使用 muted style。
- card panel 与 split line 都必须保留 content rect、hit region 和 active/focus 语义。
- emoji、CJK、combining mark、ANSI styled text 不得覆盖或推开边框。

### 5.4 Toast / Message

当前问题：

- toast 已有卡片和 lifecycle，但现代感、padding、severity/accent 侧边和右上角定位仍需 polish。
- 当前 smoke 中 toast 更像功能占位，不像用户第一张参考图的消息系统。

目标要求：

- 右上角实体弹出消息，短标题优先，正文按宽度退化。
- severity、pending/progress、close current、clear all、auto dismiss 都保留。
- 不改变 pane layout，不遮挡 header/footer 关键 token。
- 宽字符内容优先保证可见和行宽正确。

### 5.5 Overlay / Modal

当前问题：

- Terminal Picker、Terminal Pool、Workbench Tree、Prompt/Help 已能显示，但视觉仍偏基础。
- modal padding、title/action 槽位、selected row、search row 和实体卡片感需要统一。

目标要求：

- 实体前景卡片，有明确 title、搜索或 input 行、selected row、detail/preview 和 action row。
- 不要求背景灰度遮罩；如果 dim 背景会影响中文或 emoji，可不做。
- overlay 打开时拥有自己的 cursor 和 hit region，不漏发输入到底层 terminal。
- 窄屏/窄高要主动压缩内容，而不是让 footer action 或 selected row 被 framework 裁掉。

### 5.6 Floating

当前问题：

- floating 已有独立边框和基本操作，但视觉还没有达到目标截图里的 pane/card 层级。
- active/focus、title/action、遮挡、裁切和底层 pane 关系需要 polish。

目标要求：

- floating 始终是独立带边框 card，不受 tiled pane 的 card/split 呈现影响。
- title/state/action 槽位稳定，active floating 有明确 accent。
- content rect 裁切和 terminal resize 仍按 floating content rect 计算。
- floating 遮挡 pane 时不得破坏底层 pane 的 frame 宽度和 ANSI reset。

### 5.7 Copy History

当前问题：

- copy-history 已有 authoritative window、search、selection、scrollbar/status，但视觉层级仍偏工程占位。
- logical-line marker、continuation、clipped marker、selection 和 active match 还需要最终 polish。

目标要求：

- 继续只消费 core-v2 authoritative `HistoryWindow`。
- 每个 visible row 的 logical-line / continuation / clipped 状态必须清晰可见。
- selection、active match、cursor 和 scrollbar/status 层级不同。
- resize 后宽度变化必须重新绑定 authoritative window，不显示旧 cols rows。

### 5.8 Terminal Pool / Workbench Tree

当前问题：

- Terminal Pool 和 Workbench Tree 已有页面内容，但视觉更像功能表格，不像产品级 overlay。
- detail、preview、action、selected row 和 search 的层级需要统一。

目标要求：

- 页面级实体 overlay，search、list、selected row、detail、preview、action 槽位稳定。
- Terminal Pool 的 Attach/Edit/Kill action 在常规 viewport 下必须可见。
- Workbench Tree 的 workspace/tab/pane/floating row 层级必须清楚。
- 鼠标 row/action hit region 与可见内容同步，隐藏内容不得保留可点击区域。

## 6. 后续切片验收映射

切片 80 负责：

- top bar 和 bottom bar 重绘。
- header/footer hide 后的视觉退化。
- smoke 和真实 TTY 中 ANSI styled bar 可见。

切片 81 负责：

- pane chrome 与 split line 重绘。
- active/inactive accent/muted 视觉对齐。
- card/split 两种 tiled panel 的 border、slot、content rect 和 hit region 对齐。

切片 82 负责：

- toast、Terminal Picker、Terminal Pool、Workbench Tree、Prompt/Help 和 floating 视觉对齐。
- modal/card padding、title/action、selected row、severity/accent、ANSI reset 和宽字符安全。

切片 83 负责：

- 真实默认入口视觉验收。
- `go run ./termx-cli/cmd/termx` 在常用 viewport 下人工对照目标截图。
- 分屏、focus、resize、floating、Terminal Pool/Tree、Prompt/Help、copy mode 的视觉检查。

## 7. 验收方式

自动验收：

- `cd termx-tui-v3 && go test ./... -count=1`
- `go run ./termx-cli/cmd/termx v3 smoke`
- `go run ./termx-cli/cmd/termx v3 e2e-smoke`
- `git diff --check`

人工验收：

- 在真实 terminal 中运行 `go run ./termx-cli/cmd/termx`。
- 使用常见尺寸检查，例如 `80x24`、`100x32`、`120x40`。
- 对照用户给出的 `tuiv2` 截图检查 top bar、bottom bar、pane chrome、toast、overlay 和 floating。
- 不允许只凭 smoke 文本存在 Unicode 线框就判定完成。

## 8. 非目标

切片 79 不做：

- 直接重画 header/footer。
- 直接重画 pane chrome。
- 直接重画 toast/overlay/floating。
- terminal live rich attributes、streaming event loop、link/underline/reverse/truecolor 完整化。
- copy-history 最终 polish。
- remote 迁移。
- 拆分 legacy binary 或清理 module 级旧依赖。
