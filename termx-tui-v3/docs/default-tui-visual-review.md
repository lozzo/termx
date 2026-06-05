# 默认 TUI 视觉复核记录

## 1. 结论

切片 80-82 完成后，`termx-tui-v3` 已经具备 styled chrome renderer、连续 Unicode pane 线框、header/footer、toast、overlay、floating 和基础交互闭环，但真实视觉仍未达到用户提供的 `tuiv2` 截图目标。

切片 83 的结论是：复核未通过，不能把当时的默认 TUI 标记为截图级视觉完成。

切片 84-86 已完成三轮返工：shell bar 高密度重绘、pane chrome 槽位重绘、overlay/page/copy 内容层产品化 polish。

切片 87 的结论是：真实默认入口视觉复核仍未通过。当前 TUI 仍与用户提供的 `tuiv2` 目标截图不一致，不能宣称截图级视觉验收完成。切片 87 只保留固定 viewport smoke 证据扩展，作为后续重绘的回归基线。

切片 88 已完成第二轮 shell/pane 视觉重绘：默认 theme 改为更接近用户截图的紫色 accent + 深色 chrome，top bar、bottom bar、pane top chrome、toast 和 floating title 的视觉密度继续提高。但切片 88 仍不是最终截图级验收，是否真正像目标截图必须进入切片 89，在真实默认入口中对照复核。

切片 89 已完成默认入口真实 PTY 证据归档：在隔离 socket/log 和 `120x40` 真实 PTY 下运行 `go run ./termx-cli/cmd/termx`，确认默认入口进入 alternate screen，并输出 styled ANSI header、pane、footer、紫色 active border 和二轮 chrome token。该证据只证明真实 TTY 路径可绘制二轮 styled chrome，不能替代用户对目标截图风格的人工拍板。

切片 90 的结论是：用户明确反馈当前 TUI 样子仍与目标不一致，因此当前视觉对齐 goal 不能完成。后续必须进入切片 91，按目标截图重做整体 UI 构图和视觉层级，而不是继续用局部 token、smoke 文本或 PTY ANSI 捕获证明视觉完成。

## 2. 当前已经成立的工程事实

- 默认入口仍走 `termx-core-v2` 与 `termx-tui-v3`。
- renderer 主路径仍是 `render framework + content renderer`。
- `RenderResult` / `Frame` 保留 styled cell、styled line 和 ANSI 输出。
- tiled pane 默认不再使用 ASCII `+ - |`，而是 square Unicode box drawing。
- header/footer hide、pane split/focus/resize/zoom/close、floating、Terminal Pool、Workbench Tree、Prompt/Help、copy mode 都已有基本操作入口。
- terminal 内容、copy-history 和 overlay content 都被限制在自己的 content rect 内，不应冲破 UI chrome。

这些事实只说明“产品壳可运行”和“默认入口真实 PTY 路径可绘制”，不说明“视觉已经像目标截图”。切片 90 已经确认用户复核不通过，因此自动证据、真实 PTY 证据、Unicode 线框、ANSI 颜色和 chrome token 都只能作为回归基线，不能作为完成证据。

## 3. 切片 83 未通过原因

以下问题是切片 83 复核失败时的原因，已由切片 84-86 分别处理。

### 3.1 Shell Bar

当前 top/bottom bar 仍偏工程标签式信息条，例如 `ws:main`、`tab:[main]`、`active:pane-main`、`mode:live`。目标截图需要更接近 `tuiv2` 的高密度产品栏：

- tab/workspace strip 更像真实顶部栏，而不是 key-value 文本。
- active tab 和 inactive tab 需要更强的块状层级。
- 右侧 owner/status/action token 需要稳定对齐。
- bottom bar 需要彩色快捷键 taxonomy 和右侧 summary，而不是简单拼接提示。

### 3.2 Pane Chrome

当前 pane 线框已经连续，但仍不像目标截图里的 pane chrome：

- title/state/action 槽位密度不足。
- owner/follower/action token 没有形成目标截图里的视觉层级。
- active accent 和 inactive muted 的对比还不够像 `tuiv2`。
- card、split、floating 三种 chrome 的视觉语言还需要统一 polish。

### 3.3 Overlay / Toast / Floating

当前 toast 和 overlay 已经是实体 card，但仍只是第一轮 framework 视觉：

- Terminal Pool、Workbench Tree、Prompt/Help 的内容区仍偏功能表格。
- selected row、search row、detail/preview、action row 的视觉层级需要统一。
- copy-history 的 search、selection、match、scrollbar/status 仍偏工程占位。
- toast 可以继续保留右上角实体 card，不要求灰度遮罩背景。

### 3.4 验收口径错误风险

不能再用下面这些条件判定通过：

- 有 Unicode 边框。
- 有 ANSI 颜色。
- smoke 行宽恒等。
- pane 命令可操作。
- overlay/toast 可见。

这些是工程准入，不是目标截图级视觉验收。

## 4. 后续返工切片

切片 83 后按 `workflow.md` 增加并完成了以下返工切片：

- 切片 84：`tuiv2` 风格 shell bar 高密度重绘。
- 切片 85：pane chrome 目标截图级槽位重绘。
- 切片 86：overlay/page/copy 视觉产品化 polish。
- 切片 87：默认入口截图级视觉复核未通过归档。
- 切片 88：目标截图级 shell/pane 视觉重绘二轮。
- 切片 89：真实默认入口截图级验收证据归档。
- 切片 90：用户真实视觉复核不通过归档。
- 切片 91：目标截图级整体 UI 构图三轮重绘。

切片 84 已完成第一轮 shell bar 重绘：顶部不再使用 `ws:/tab:/active:` 这类工程标签，而是使用 workspace、tab strip、`[⊕]`、active pane、`◆ owner`、terminal/floating 和 action token；底部不再使用 `mode:/keys:`，而是使用 `MODE • [KEY] ACTION` 快捷键 taxonomy、active target 和 summary。切片 90 已确认整体视觉仍不通过。

切片 85 已完成第一轮 pane chrome 槽位重绘：tiled pane 顶边不再只显示单一 close token，而是显示 title、状态点、`↔0` 元信息、`◆ owner` 和 action cluster；宽 pane 使用 `[o]─[_]─[Z]─[x]`，窄分屏 pane 使用 `[Z]─[x]` 退化，title 与 action 之间继续保留连续横线。切片 90 已确认整体视觉仍不通过。

切片 86 已完成第一轮 overlay/page/copy 内容层视觉产品化 polish：Terminal Picker、Terminal Pool、Workbench Tree、Prompt、Help 和 copy-history 不再使用工程表格式文本作为主要视觉语言，而是统一 search affordance、selected row marker、detail/preview/context/input label、action row 和 copy search/match/scrollbar/status。切片 90 已确认整体视觉仍不通过。

切片 87 已完成默认入口截图级视觉复核未通过归档和自动证据扩展：

- `go run ./termx-cli/cmd/termx v3 smoke` 现在输出 12 个固定视觉 case，新增 `terminal-pool-page` 和 `workbench-tree-page` 页面级快照。
- `visual-audit-current` 保留为 `visual review` 基线，固定 `120x40` viewport，覆盖 split line、active/inactive pane、toast、floating、header/footer、emoji/CJK 宽度安全，并明确当前仍需 screenshot polish。
- `TestSmokeRunDetailedCoversUIFramework` 固化 header/footer、pane chrome、toast、floating、Terminal Picker、Terminal Pool、Workbench Tree、Prompt/Help、copy-history 的视觉 marker、ANSI style 和行宽恒等。
- `TestV3SmokeCommandIncludesVisualReviewCases` 固化 CLI 可执行 `termx v3 smoke` 输出必须包含视觉复核 case，避免只在包内测试通过。
- `termx v3 e2e-smoke` 继续覆盖默认 attach 装配、host viewport、resize、content rect terminal resize、copy rebind 和 pane command。
- 默认 `go run ./termx-cli/cmd/termx` 在非交互环境会按设计拒绝启动；可验证证据链是 `TestRootCmdRoutesToTUIv3ByDefault` 证明默认 root 路由到 v3 root runner，`v3 e2e-smoke` 证明同一 v3 TUI render/frame 路径可渲染和交互。
- 用户真实复核指出当前 TUI 仍不像目标截图，因此切片 88 必须继续做 shell/pane 视觉重绘，切片 89 再做真实默认入口截图级验收。

切片 88 已完成第二轮 shell/pane 视觉重绘：

- theme accent 从青绿色改为紫色系，status bar 背景改为更接近目标截图的深色。
- top bar 使用 workspace、tab、关闭 `×`、新增 `[＋]`、active pane、`◆ owner`、terminal/floating compact summary 和 `[o]─[_]─[Z]─[x]` action cluster。
- bottom bar 从 `MODE • [KEY] ACTION` 继续收敛为 `[Ctrl] · [P] PANE` 这类彩色快捷键 taxonomy，右侧 ready token 简化为 `termx`。
- pane top chrome 使用 `· ↔2`、`· ◆ owner`、`· 1/31` 和 action cluster 提高密度，active pane 继续用 accent，inactive pane 用 muted。
- toast、floating 和 overlay title 使用 `·` 分层，避免回到 ASCII 分隔；floating active state 改为 `● float`。
- 自动 harness 已覆盖紫色 ANSI、暗色 status 背景、无默认 ASCII chrome、宽字符安全、固定 smoke 12 case 和 CLI smoke 输出。

切片 88 的结论只能是“二轮视觉重绘完成”。

切片 89 已完成默认入口真实 PTY 证据归档：

- 使用隔离 socket/log 启动默认 `go run ./termx-cli/cmd/termx`，避免污染用户已有 daemon。
- 固定 PTY viewport 为 `120x40`。
- 观测到真实 TUI 进入 alternate screen，并输出 styled ANSI header、pane、footer。
- 首屏包含紫色 active border、深色 status background、`×`、`[＋]`、`[Ctrl] · [P]`、`· ↔2`、`· ◆ owner`、`· 1/31` 等二轮 chrome token。
- 隔离验收进程已清理。
- 发现当前默认 TUI 没有全局 quit 快捷键；`Ctrl-C` 会进入底层 shell，这是后续交互 polish 项，不作为本次视觉绘制通过或失败的依据。

切片 89 不能作为“截图级视觉通过”结论。

切片 90 已完成用户真实视觉复核不通过归档：

- 用户明确反馈当前 TUI 样子仍与目标不一致。
- 当前视觉对齐 goal 不能标记完成。
- 自动 smoke、e2e、PTY ANSI 捕获、Unicode 线框、ANSI 颜色和 chrome token 都不能解除该不通过结论。
- 后续切片 91 必须回到整体 UI 构图和视觉层级，重点处理 top/bottom bar 整屏感、pane/floating/toast 比例、留白、层级、active/inactive 强弱、默认首屏占位和真实截图级对照。
- 切片 91 可以使用 Nerd Font、emoji 和其他 UTF-8 glyph 作为正式 chrome token；关键要求是通过 width helper 和 harness 保证显示宽度、裁切、命中区和 ANSI styled frame 一致，而不是退回纯 ASCII action 文案。

切片 91 已完成三轮整体 UI 构图重绘，并通过自动准入；该结论仍不替代用户对目标截图风格的真实终端复核。

## 5. 手工复核入口

切片 91 完成后必须手工复核：

- 启动：`go run ./termx-cli/cmd/termx`。
- viewport：至少检查 `80x24`、`100x32`、`120x40`。
- pane：`Ctrl-p` 后检查 `v/s/n/N/z/x/c/p` 的 split、focus、zoom、close、card/split 视觉反馈。
- resize：`Ctrl-r` 后检查方向键和 `h/j/k/l` 调整大小时边框、footer、content rect 是否同步。
- floating：`Ctrl-o` 后检查 create、move、resize、center、collapse、close 和 active chrome。
- overlay：`Ctrl-g p`、`Ctrl-g w`、`Ctrl-g :`、`Ctrl-g ?` 检查 Terminal Pool、Workbench Tree、Prompt、Help。
- copy：`Ctrl-v` 检查 copy-history search、selection、scrollbar/status 和 no terminal input leak。
- toast/header/footer：`Ctrl-g h/f/T/t` 检查隐藏、恢复、关闭和清空消息。

## 6. 自动准入

切片 90 自动准入：

- `git diff --check`

切片 90 是文档归档切片，只要求 `git diff --check`。切片 91 已恢复并通过完整准入：`cd termx-tui-v3 && go test ./... -count=1`、`cd termx-cli && go test ./... -count=1`、`go run ./termx-cli/cmd/termx v3 smoke`、`go run ./termx-cli/cmd/termx v3 e2e-smoke`、`git diff --check`。
