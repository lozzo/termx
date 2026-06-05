# TUI-v3 视觉差距审计与验收基线

## 1. 文档目的

本文档是切片 79 的视觉基线文档，用来纠正一个关键判断：当前 `termx-tui-v3` 已经具备 styled chrome renderer 和第一版可操作产品壳，但还没有达到用户要求的 `tuiv2` 截图级界面效果。

切片 83 已完成真实默认 TUI 复核，结论仍是未通过。切片 84-86 已完成对应返工，但切片 87 根据用户真实反馈再次确认当前 TUI 仍与目标截图不一致。切片 88 已完成 shell/pane 二轮视觉重绘，切片 89 已完成默认入口真实 PTY 证据归档；切片 90 根据用户反馈确认视觉仍不通过。后续必须进入切片 91 的整体 UI 构图三轮重绘。复核记录见 `termx-tui-v3/docs/default-tui-visual-review.md`。

视觉返工必须以本文档作为验收入口。实现仍必须遵守 `termx-tui-v3/docs/architecture.md` 与 `termx-tui-v3/docs/render-architecture.md`，不得因为追求视觉相似而复制旧 `tuiv2` runtime/model、Bubble Tea contract、snapshot/grid history fallback 或旧 renderer 大状态结构。

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
- 切片 86 后，Terminal Picker、Terminal Pool、Workbench Tree、Prompt、Help 和 copy-history 内容层已从工程表格/占位推进到第一轮产品视觉：统一 search affordance、selected row marker、detail/preview/context/input label、action row 和 copy search/match/scrollbar/status。
- 切片 87 后，`go run ./termx-cli/cmd/termx v3 smoke` 输出 12 个非交互视觉 case，包含 `terminal-pool-page`、`workbench-tree-page` 和 `visual-audit-current` review baseline。
- 切片 88 后，shell/pane 视觉完成二轮重绘：theme accent 改为紫色系，status bar 改为深色背景，top bar 使用 `×`、`[＋]` 和 compact summary，bottom bar 使用 `[Ctrl] · [P]` 类快捷键 taxonomy，pane top chrome 使用 `· ↔2`、`· ◆ owner`、`· 1/31` 与 action cluster。
- 切片 89 后，默认 `go run ./termx-cli/cmd/termx` 已在隔离 `120x40` 真实 PTY 中证明可进入 alternate screen 并输出二轮 styled ANSI frame。
- 切片 90 后，用户确认当前真实 TUI 样子仍与目标不一致，当前视觉 goal 不能完成。

当前仍不足：

- 用户真实复核已经暴露关键差距：当前 TUI 整体风格、密度、比例、层级和目标 `tuiv2` 截图仍不一致；切片 88 只是对应差距的二轮重绘，不是最终验收。
- 切片 91 前仍不能宣称当前视觉对齐 workflow 完成；切片 87、88、89、90 都不能作为完成结论。
- 有 Unicode 线框、ANSI 颜色、可操作命令和真实 PTY ANSI frame 仍不能单独作为未来视觉验收证据；必须由用户对照目标截图拍板。

## 4. 固定 viewport smoke 基线

切片 79 新增 `termx v3 smoke` case：

- case 名称：`visual-audit-current`
- 固定 viewport：`120x40`
- 覆盖元素：header、footer、split tiled pane、active/inactive pane、floating card、toast、emoji/CJK 宽度安全文本。
- 目的：记录视觉复核的稳定快照，供后续切片 84-87 对比。

切片 90 后，该 case 仍保持为 `visual review` 基线，并明确包含 `needs polish` 语义。只有切片 91 或后续真实截图复核通过后，才允许把它改为完成类基线。

## 5. 差距清单

### 5.1 Top Bar

当前问题：

- 切片 80 已修复稀疏拼接文本条问题，top bar 现在是分段产品栏。
- 切片 84 已把 top bar 推进到高密度产品栏：左侧 workspace + tab strip + `[⊕]`，右侧 active pane、`◆ owner`、terminal/floating summary 和 action token。
- 切片 88 已把 top bar 继续推进到紫色 accent + 深色背景方向：tab strip 改为 active tab、关闭 `×`、新增 `[＋]`，右侧使用 active pane、owner、compact terminal/floating summary 和 action cluster。
- 切片 90 已确认该 top bar 仍未达到目标截图级别；切片 91 必须按整体构图重新处理。

目标要求：

- 整行稳定 top bar，背景填满。
- 左侧显示 workspace 与 tab strip，active tab 与 inactive tab 清晰区分。
- 中间和右侧提供短状态 token，例如 active pane、terminal count、floating count、owner/status/action。
- 窄屏时按优先级退化，而不是随机裁掉关键上下文。

### 5.2 Bottom Bar

当前问题：

- 切片 80 已修复 mode、快捷键、active target、右侧 summary 缺少稳定槽位的问题。
- 切片 84 已把 bottom bar 推进到 `MODE • [KEY] ACTION` 快捷键 taxonomy，并在窄屏退化时保留尾部关键动作。
- 切片 88 已把 bottom bar 继续收敛到 `[Ctrl] · [P] PANE` 类快捷键 taxonomy，并把 ready token 简化为 `termx`。
- 切片 90 已确认该 bottom bar 仍未达到目标截图级别；切片 91 必须按整体构图重新处理。

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
- 切片 88 已把 pane top chrome 元信息改为更高密度的 `· ↔2`、`· ◆ owner`、`· 1/31`，并让 active accent 改为紫色系；floating active state 改为 `● float`。
- 切片 90 已确认该 pane chrome 仍未达到目标截图级别；切片 91 必须按整体构图重新处理。

目标要求：

- square 细线连续边框：`┌┐└┘─│├┤┬┴┼`。
- 顶边 title、state、action 槽位稳定，title/action 之间必须保留线段。
- active pane 使用 accent / strong style，inactive pane 使用 muted style。
- card panel 与 split line 都必须保留 content rect、hit region 和 active/focus 语义。
- emoji、CJK、combining mark、ANSI styled text 不得覆盖或推开边框。

### 5.4 Toast / Message

当前问题：

- 切片 82 已把 toast 从功能占位推进到右上角实体消息：rounded card、severity accent 竖条、title/body 合并裁切、close action、右侧/顶部留白和 ANSI style 已落地。
- 切片 88 已把 toast title/body 分隔改为 `·` 分层，并沿用深色实体 card 与 severity 侧边。
- 切片 90 已确认整体视觉仍未通过，toast 不能单独视为参考图级别；不得回退成简单文本。
- 切片 92 按用户截图反馈把 toast 收敛为深色直角实体矩形、左右紫色竖线和居中文案；toast 不再绘制 close token，点击 toast 本体只负责遮挡命中，不穿透到底层 UI。

目标要求：

- 右上角实体弹出消息，短标题优先；复制成功等短反馈使用居中文案，不显示多行 title/body 表格感。
- severity、pending/progress、close current、clear all、auto dismiss 都保留。
- 不改变 pane layout，不遮挡 header/footer 关键 token。
- 宽字符内容优先保证可见和行宽正确。

### 5.5 Overlay / Modal

当前问题：

- 切片 82 已把 Terminal Picker、Terminal Pool、Workbench Tree、Prompt/Help 的 framework chrome 推进为带 padding 的实体 card：title/state/action 槽位、content padding、ANSI reset 和 cursor/hit region 语义已同步。
- 切片 86 已完成 overlay 内部 content 第一轮产品化 polish：Terminal Picker 使用统一搜索行、选中 marker、preview 和 new action；Terminal Pool / Workbench Tree 使用页面标题、搜索行、selected row、detail / preview / action 槽位；Prompt / Help 使用标题、context/input 或分类 topic 行。
- 切片 87 已把 Terminal Pool Page 和 Workbench Tree Page 加入 `termx v3 smoke` 固定快照，但这只是回归证据，不是截图级视觉完成证据。

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

- copy-history 已有 authoritative window、search、selection、scrollbar/status。
- 切片 86 已把 search row 推进为 `⌕ search` 形态，把 match state 和 `SCROLL` status 从工程占位推进到更清晰的产品层级。
- 切片 87 已把 copy-history 的 search row、authoritative row 和 `SCROLL` status 纳入 smoke 断言。logical-line marker、continuation、clipped marker、selection 和 active match 的更细视觉仍可在后续切片继续增强。

目标要求：

- 继续只消费 core-v2 authoritative `HistoryWindow`。
- 每个 visible row 的 logical-line / continuation / clipped 状态必须清晰可见。
- selection、active match、cursor 和 scrollbar/status 层级不同。
- resize 后宽度变化必须重新绑定 authoritative window，不显示旧 cols rows。

### 5.8 Terminal Pool / Workbench Tree

当前问题：

- Terminal Pool 和 Workbench Tree 已有页面内容。
- 切片 86 已统一 search、selected row、detail、preview、action 的产品层级，不再允许回退到 `> row`、`detail xxx`、`preview xxx` 这类工程表格文本。
- 切片 87 已在 `100x30` 固定 viewport 下把 Terminal Pool Page 和 Workbench Tree Page 纳入 smoke 输出，常规 viewport 下关键 action 不被裁切；视觉密度和品牌层级仍以后续真实复核为准。

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
- 后续继续通过切片 84-91 处理 shell bar、pane chrome、overlay/page/copy polish、二轮重绘、真实 PTY 证据、用户不通过归档和整体 UI 构图三轮重绘。

切片 84 负责：

- 已完成 shell bar 高密度重绘第一轮。
- 已把 fixed smoke 和 e2e visual guard 更新为新的 top/bottom bar 视觉合同。
- 截图级总体验收仍未通过，后续留到切片 91 或之后。

切片 85 负责：

- 已完成 pane chrome 目标截图级槽位重绘第一轮。
- 已把 fixed smoke、render harness 和 e2e visual guard 更新为新的 pane 顶边槽位合同。
- 截图级总体验收仍未通过，后续留到切片 91 或之后。

切片 86 负责：

- 已完成 overlay/page/copy 内容层视觉产品化 polish 第一轮。
- 已把 Terminal Picker、Terminal Pool、Workbench Tree、Prompt、Help 和 copy-history 的 render harness 与 smoke 断言更新为新的内容层视觉合同。
- 截图级总体验收仍未通过，后续留到切片 91 或之后；切片 86 不能单独作为默认 TUI 已达到目标截图的证据。

切片 87 负责：

- 已完成默认入口截图级视觉复核未通过归档。
- 已把 `termx v3 smoke` 扩展到 12 个 case，覆盖 workbench live、split hidden toast、Terminal Picker、Terminal Pool Page、Workbench Tree Page、copy empty、copy history、Prompt、Help、Tab/Workspace、pane command flow 和 `120x40` visual review baseline。
- 已在 `termx-cli` 测试中固化 CLI smoke 输出必须包含 Terminal Pool / Workbench Tree / visual review / copy-history status marker，并禁止出现 `visual acceptance` 完成声明。
- 已保留 `termx v3 e2e-smoke` 对默认 attach 装配、host viewport、resize、content rect terminal resize、copy rebind 和 pane command 的证明。
- 默认 root 在非交互环境拒绝启动；其可验证证据是默认 root 路由到 v3 root runner，v3 smoke/e2e 证明同一 TUI render/frame path。
- 切片 87 不能作为视觉完成证据；切片 88 必须继续 shell/pane 视觉重绘，切片 89 再做真实默认入口截图级验收。

切片 88 负责：

- 已完成目标截图级 shell/pane 视觉重绘二轮。
- 已把默认 theme 切到紫色 accent、深色 host/chrome/status 背景和 muted 边框层级。
- 已更新 top bar、bottom bar、pane top chrome、toast、floating 和 overlay title 分隔符，使默认视觉更接近用户给出的 `tuiv2` 紫色边框与高密度 chrome 风格。
- 已把 render harness、tui smoke 和 CLI smoke 更新到二轮视觉合同，覆盖紫色 ANSI、暗色 status 背景、无默认 ASCII chrome、宽字符安全和固定 12 case。
- 切片 88 不能作为最终视觉完成证据；切片 89 必须做真实默认入口 PTY 证据归档，切片 90 必须归档用户不通过结论，切片 91 必须做整体 UI 构图三轮重绘。

切片 89 负责：

- 已在隔离 socket/log 和真实 PTY 中运行默认 `go run ./termx-cli/cmd/termx`。
- 已确认默认入口进入 alternate screen，并输出 styled ANSI header、pane、footer、紫色 active border 和二轮 chrome token。
- 已确认隔离进程清理完成。
- 发现默认 TUI 没有全局 quit 快捷键，`Ctrl-C` 会进入底层 shell；这是后续交互 polish 项。
- 切片 89 不能把当前 goal 标记完成，因为 PTY ANSI frame 仍不是用户截图级视觉拍板。

切片 90 负责：

- 已根据用户反馈确认当前真实 TUI 样子仍与目标不一致。
- 已明确当前视觉对齐 goal 不能完成。
- 已把不通过结论作为切片 91 的返工入口。

切片 91 已完成：

- 已按目标截图方向重新处理默认 TUI 整体构图，而不是只替换局部 token。
- 已覆盖 top bar、bottom bar、pane card/split、floating、toast、modal/overlay、默认首屏占位、active/inactive 层级、留白比例和窄屏退化的自动 smoke 合同。
- 已把 pane/floating chrome 的动作和状态 glyph 当作正式视觉 token 处理；默认允许使用 Nerd Font PUA 字符，也允许后续按字体环境替换为 emoji 或其他 UTF-8 符号，命中区和裁切继续按 display cell width 计算。
- 实现继续使用 render framework + content renderer，未复制 `tuiv2` runtime/model，未引入 Bubble Tea。
- 自动验收已通过；是否真正达到用户截图级风格仍需真实终端人工复核。

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
- 不允许只凭 smoke 文本、PTY ANSI 捕获或 Unicode 线框就判定完成。

## 8. 非目标

本文档初始切片 79 不做：

- 直接重画 header/footer。
- 直接重画 pane chrome。
- 直接重画 toast/overlay/floating。
- terminal live rich attributes、streaming event loop、link/underline/reverse/truecolor 完整化。
- copy-history 最终 polish。
- remote 迁移。
- 拆分 legacy binary 或清理 module 级旧依赖。
