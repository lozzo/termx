# 默认 TUI 视觉复核记录

## 1. 结论

切片 80-82 完成后，`termx-tui-v3` 已经具备 styled chrome renderer、连续 Unicode pane 线框、header/footer、toast、overlay、floating 和基础交互闭环，但真实视觉仍未达到用户提供的 `tuiv2` 截图目标。

因此切片 83 的结论是：复核未通过，不能把当前默认 TUI 标记为截图级视觉完成。

## 2. 当前已经成立的工程事实

- 默认入口仍走 `termx-core-v2` 与 `termx-tui-v3`。
- renderer 主路径仍是 `render framework + content renderer`。
- `RenderResult` / `Frame` 保留 styled cell、styled line 和 ANSI 输出。
- tiled pane 默认不再使用 ASCII `+ - |`，而是 square Unicode box drawing。
- header/footer hide、pane split/focus/resize/zoom/close、floating、Terminal Pool、Workbench Tree、Prompt/Help、copy mode 都已有基本操作入口。
- terminal 内容、copy-history 和 overlay content 都被限制在自己的 content rect 内，不应冲破 UI chrome。

这些事实只说明“产品壳可运行”，不说明“视觉已经像目标截图”。

## 3. 未通过原因

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

后续必须按 `workflow.md` 新增切片继续：

- 切片 84：`tuiv2` 风格 shell bar 高密度重绘。
- 切片 85：pane chrome 目标截图级槽位重绘。
- 切片 86：overlay/page/copy 视觉产品化 polish。
- 切片 87：默认入口截图级视觉验收。

切片 84 已完成第一轮 shell bar 重绘：顶部不再使用 `ws:/tab:/active:` 这类工程标签，而是使用 workspace、tab strip、`[⊕]`、active pane、`◆ owner`、terminal/floating 和 action token；底部不再使用 `mode:/keys:`，而是使用 `MODE • [KEY] ACTION` 快捷键 taxonomy、active target 和 summary。最终是否达到截图级视觉仍以后续切片 87 的真实默认入口验收为准。

## 5. 手工复核入口

每次视觉返工后都必须手工复核：

- 启动：`go run ./termx-cli/cmd/termx`。
- viewport：至少检查 `80x24`、`100x32`、`120x40`。
- pane：`Ctrl-p` 后检查 `v/s/n/N/z/x/c/p` 的 split、focus、zoom、close、card/split 视觉反馈。
- resize：`Ctrl-r` 后检查方向键和 `h/j/k/l` 调整大小时边框、footer、content rect 是否同步。
- floating：`Ctrl-o` 后检查 create、move、resize、center、collapse、close 和 active chrome。
- overlay：`Ctrl-g p`、`Ctrl-g w`、`Ctrl-g :`、`Ctrl-g ?` 检查 Terminal Pool、Workbench Tree、Prompt、Help。
- copy：`Ctrl-v` 检查 copy-history search、selection、scrollbar/status 和 no terminal input leak。
- toast/header/footer：`Ctrl-g h/f/T/t` 检查隐藏、恢复、关闭和清空消息。

## 6. 自动准入

视觉返工仍必须保留自动准入：

- `cd termx-tui-v3 && go test ./... -count=1`
- `cd termx-cli && go test ./... -count=1`
- `go run ./termx-cli/cmd/termx v3 smoke`
- `go run ./termx-cli/cmd/termx v3 e2e-smoke`
- `git diff --check`

自动准入只能证明没有破坏 contract。最终视觉是否通过，仍以用户目标截图和真实 terminal 人工复核为准。
