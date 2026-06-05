# TUI-v3 视觉差距审计与验收基线

## 1. 文档目的

本文档是切片 79 的视觉基线文档，用来纠正一个关键判断：当前 `termx-tui-v3` 已经具备 styled chrome renderer 和第一版可操作产品壳，但还没有达到用户要求的 `tuiv2` 截图级界面效果。

切片 83 已完成真实默认 TUI 复核，结论仍是未通过。复核记录见 `termx-tui-v3/docs/default-tui-visual-review.md`。后续不得把切片 80-82 的工程进展误标为截图级视觉完成。

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
- 切片 80 后，header/footer 已从拼接文本条改为分段产品栏：workspace/tab/mode/action/active/summary 使用稳定 token，active token 使用 accent，次级 summary 使用 muted，notice/error/exited 使用 warning，行内使用 Unicode `│` 分隔。
- 切片 81 后，pane chrome 已从基础线框推进到 `tuiv2` 风格的 shared chrome：card panel 与 split line 都使用 square 细线、顶边 title/state/action 槽位、active accent、inactive muted、连续外框和宽字符安全 content rect。
- `go run ./termx-cli/cmd/termx v3 smoke` 可输出多个非交互 smoke case。

当前仍不足：

- 切片 83 复核确认：当前 smoke 和默认入口的视觉密度、槽位、状态表达和整体层级仍不像用户给出的 `tuiv2` 截图。
- 有 Unicode 线框、ANSI 颜色和可操作命令，并不等于视觉对齐完成。
- 文档中所有“styled chrome 已达到 tuiv2 截图级视觉等级”的历史表述都应按“基础结构已落地，但视觉仍需返工”理解。

## 4. 固定 viewport smoke 基线

切片 79 新增 `termx v3 smoke` case：

- case 名称：`visual-audit-current`
- 固定 viewport：`120x40`
- 覆盖元素：header、footer、split tiled pane、active/inactive pane、floating card、toast、emoji/CJK 宽度安全文本。
- 目的：记录视觉复核的稳定快照，供后续切片 84-87 对比。

该 case 是“复核快照”，不是目标完成快照。切片 83 后该 case 的文案已经从 `visual gap / not tuiv2` 改成 `visual review / needs polish`，用于提醒当前仍需截图级返工。后续实现如果让该 case 更接近目标，应同步更新本文档或相关验收说明。

## 5. 差距清单

### 5.1 Top Bar

当前问题：

- 切片 80 已修复稀疏拼接文本条问题，top bar 现在是分段产品栏。
- 切片 84 已把 top bar 推进到高密度产品栏：左侧 workspace + tab strip + `[⊕]`，右侧 active pane、`◆ owner`、terminal/floating summary 和 action token。
- 仍可继续 polish inactive tab 视觉和最终品牌密度，但不再允许回退到 `ws:/tab:/active:` 工程标签。

目标要求：

- 整行稳定 top bar，背景填满。
- 左侧显示 workspace 与 tab strip，active tab 与 inactive tab 清晰区分。
- 中间和右侧提供短状态 token，例如 active pane、terminal count、floating count、owner/status/action。
- 窄屏时按优先级退化，而不是随机裁掉关键上下文。

### 5.2 Bottom Bar

当前问题：

- 切片 80 已修复 mode、快捷键、active target、右侧 summary 缺少稳定槽位的问题。
- 切片 84 已把 bottom bar 推进到 `MODE • [KEY] ACTION` 快捷键 taxonomy，并在窄屏退化时保留尾部关键动作。
- 右侧细粒度状态 summary 和最终品牌视觉仍可在切片 87 前继续 polish，但不再允许回退到 `mode:/keys:` 工程标签。

目标要求：

- 整行稳定 bottom bar，背景填满。
- 左侧突出当前 mode，中间显示 mode-specific shortcut token，右侧显示 workspace/pane/terminal summary。
- copy mode、overlay、Prompt/Help、Terminal Pool、Workbench Tree 都要有专属 footer hints。
- header/footer hide 后 body 回收空间，必要上下文通过 toast、短标识或 Help 入口可恢复识别。

### 5.3 Pane Chrome

当前问题：

- 切片 81 已完成第一轮 pane chrome 视觉重绘，card panel 与 split line 都具备稳定 title/state/action 槽位、连续 square 外框和 active/inactive style。
- split line 现在使用共享外框与共享分割线，content rect 会避开外边框和 divider；terminal resize 与 copy rebind 也按新的 content rect cols/rows 计算。
- 切片 85 已把 pane 顶边推进到目标截图式槽位：title、状态点、`↔0`、`◆ owner`、宽 pane full action cluster `[o]─[_]─[Z]─[x]`、窄分屏 compact action cluster `[Z]─[x]`，并让 action hit region 与可见 cluster 同宽。
- 仍未完全达到 `tuiv2` 截图中真实 owner/follower 数据、tab strip 侧信息和更高密度 pane 状态的完整细节。

目标要求：

- square 细线连续边框：`┌┐└┘─│├┤┬┴┼`。
- 顶边 title、state、action 槽位稳定，title/action 之间必须保留线段。
- active pane 使用 accent / strong style，inactive pane 使用 muted style。
- card panel 与 split line 都必须保留 content rect、hit region 和 active/focus 语义。
- emoji、CJK、combining mark、ANSI styled text 不得覆盖或推开边框。

### 5.4 Toast / Message

当前问题：

- 切片 82 已把 toast 从功能占位推进到右上角实体消息：rounded card、severity accent 竖条、title/body 合并裁切、close action、右侧/顶部留白和 ANSI style 已落地。
- toast 仍不是最终品牌视觉；后续可继续 polish 动画、progress 和更细的 severity icon，但不得回退成简单文本。

目标要求：

- 右上角实体弹出消息，短标题优先，正文按宽度退化。
- severity、pending/progress、close current、clear all、auto dismiss 都保留。
- 不改变 pane layout，不遮挡 header/footer 关键 token。
- 宽字符内容优先保证可见和行宽正确。

### 5.5 Overlay / Modal

当前问题：

- 切片 82 已把 Terminal Picker、Terminal Pool、Workbench Tree、Prompt/Help 的 framework chrome 推进为带 padding 的实体 card：title/state/action 槽位、content padding、ANSI reset 和 cursor/hit region 语义已同步。
- overlay 内部 content 的 selected row、detail、preview 仍可继续做最终产品视觉 polish。

目标要求：

- 实体前景卡片，有明确 title、搜索或 input 行、selected row、detail/preview 和 action row。
- 不要求背景灰度遮罩；如果 dim 背景会影响中文或 emoji，可不做。
- overlay 打开时拥有自己的 cursor 和 hit region，不漏发输入到底层 terminal。
- 窄屏/窄高要主动压缩内容，而不是让 footer action 或 selected row 被 framework 裁掉。

### 5.6 Floating

当前问题：

- 切片 82 已把 floating chrome 推进为独立实体 pane card：title/state/action 槽位、active accent、resize affordance、content rect 裁切和遮挡层级已对齐。
- floating 的 drag affordance、attach as floating 和更细的 z-order 视觉仍可继续深化。

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

- 已完成 top bar 和 bottom bar 重绘。
- 已保留 header/footer hide 后的 body 回收语义。
- 已通过 smoke 和 ANSI styled frame 验证：status background、accent/muted/warning token、Unicode 分隔、关键状态优先保留和窄屏快捷键压缩。

切片 81 负责：

- 已完成 pane chrome 与 split line 重绘。
- 已完成 active/inactive accent/muted 视觉对齐。
- 已完成 card/split 两种 tiled panel 的 border、slot、content rect 和 hit region 对齐。
- 已明确 split line 使用共享外框语义，不能再用裸内部 divider 造成边界感弱或纵线不连续。

切片 82 负责：

- 已完成 toast、Terminal Picker、Terminal Pool、Workbench Tree、Prompt/Help 和 floating 的第一轮视觉对齐。
- 已完成 modal/card padding、title/action、severity/accent、ANSI reset 和宽字符安全的 framework 层验收。
- selected row、detail/preview 和最终品牌视觉仍允许在切片 83 后继续 polish。

切片 83 负责：

- 已完成真实默认入口视觉复核。
- 复核结论是未通过，不能标记为截图级视觉完成。
- 已把失败原因、手工复核入口和后续切片写入 `termx-tui-v3/docs/default-tui-visual-review.md`。
- 后续继续通过切片 84-87 处理 shell bar、pane chrome、overlay/page/copy polish 和最终截图级验收。

切片 84 负责：

- 已完成 shell bar 高密度重绘第一轮。
- 已把 fixed smoke 和 e2e visual guard 更新为新的 top/bottom bar 视觉合同。
- 截图级总体验收仍留到切片 87。

切片 85 负责：

- 已完成 pane chrome 目标截图级槽位重绘第一轮。
- 已把 fixed smoke、render harness 和 e2e visual guard 更新为新的 pane 顶边槽位合同。
- 截图级总体验收仍留到切片 87。

## 7. 验收方式

自动验收：

- `cd termx-tui-v3 && go test ./... -count=1`
- `cd termx-cli && go test ./... -count=1`
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
