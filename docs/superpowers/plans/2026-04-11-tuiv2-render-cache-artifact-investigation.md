# tuiv2 Render Cache Artifact Investigation (2026-04-11)

## Context

用户反馈两类现象，怀疑都与此前为了提升浮动窗口移动性能引入的渲染缓存有关：

1. pane 顶栏的 size lock 按钮点击后，metadata 已切换，但左侧图标不变化。
2. 页面偶发渲染错位、脏屏、纯黑背景覆盖文字区域；另一个相关现象是在外部终端窗口 resize 时，`neovim` 的灰色背景会在文本末尾断掉，行尾背景丢失。

## Confirmed Findings

### A. Lock button stale icon was a real cache invalidation bug

根因：

- `client.SetMetadata(...)` 已经把 `termx.size_lock` 写回 server。
- `TerminalRegistry.SetMetadata(...)` 只更新本地 registry 字段。
- 但 `runtime.Visible()` 依赖的 visible cache 没有失效。
- render 继续吃旧的 `VisibleRuntime.Terminals[i].SizeLocked`，导致按钮图标不刷新。

修复：

- 新增 `Runtime.SetTerminalMetadata(...)`
- 统一走 runtime 层 metadata 更新，并调用 `runtime.invalidate()`
- size lock toggle 和 terminal metadata 编辑都改为使用这个入口

相关文件：

- `tuiv2/runtime/metadata.go`
- `tuiv2/app/terminal_size_lock.go`
- `tuiv2/app/update_modal_prompt_submit_terminal.go`

回归测试：

- `TestFeatureToggleTerminalSizeLockWithKeyboardSavesMetadata`
- `TestMouseClickPaneChromeSizeLockTogglesTerminalMetadata`

### B. Broader render artifact bug is still open

目前还没有一个“手动必现”的稳定复现步骤。现象更像是另一类缓存/重绘边界问题，而不是 metadata cache 这条线本身：

- 浮动窗口移动后偶发错位/脏屏
- 某些本应是文字的区域被纯黑背景覆盖
- `neovim` 在外部终端 resize 后，行尾背景没有延展到当前宽度

这些现象更接近以下几类问题之一：

1. pane content sprite / body render cache 的宽高或 rect key 没及时失效
2. resize 后行尾 blank fill / background fill 没覆盖到新宽度
3. ANSI row serialization 只输出到文本末尾，没有正确补齐背景
4. surface/snapshot version 与 render cache key 的联动仍有缺口
5. wide cell / continuation cell footprint 在 resize 或 partial redraw 后没被彻底清理

## Investigation Plan

### Phase 1: Build a stable reproducer

目标：

- 先把“偶发”变成 deterministic test 或最小可复现 harness。

优先方向：

1. 浮动窗口拖动 + render cache 复用
2. host terminal resize + `neovim`/alternate-screen background tail
3. content rect 变化但 cached sprite 未重新生成

候选入口：

- `tuiv2/render/coordinator_test.go`
- `tuiv2/render/compositor_test.go`
- `tuiv2/app/mouse_test.go`
- `tuiv2/app/cursor_writer_test.go`
- `tuiv2/app/e2e_test.go`

### Phase 2: Narrow the failing layer

对每个 reproducer 分开判断问题发生在：

1. runtime surface / snapshot 输入是否已经错误
2. render cache key 是否错误地命中
3. composed canvas 是否残留旧 cell footprint
4. cursor writer 是否没有清掉 host 端旧区域

### Phase 3: Fix and freeze with tests

要求：

- 修复必须附带至少一个 deterministic test
- 文档补充“触发条件 / 根因 / 为什么之前没失效 / 新的回归保护”

## Current Status

- size lock 图标不刷新的子问题：已修复
- 广义 render artifact / black background / `neovim` 背景断尾：已拿到一个 deterministic reproducer

## Stable Reproducer

### VTerm-level reproducer

测试：

- `TestVTermResizeRoundTripPreservesBackgroundStyleAcrossExpandedTail`

位置：

- `vterm/load_snapshot_test.go`

步骤：

1. 构造一个带整行背景色的 snapshot
2. `LoadSnapshot(...)`
3. 执行 `Resize(窄)`
4. 再执行 `Resize(宽)`
5. 检查重新扩展出来的行尾空白列

未修复前的稳定现象：

- 扩展出来的尾部空白列会丢失原先的背景色
- 这与用户描述的 “`neovim` 文本末尾之后的灰色背景消失” 一致

结论：

- 这条 bug 至少有一部分根因在 `VTerm.Resize()` 之后的本地 screen view / snapshot 行尾样式保持，而不是单纯 render cache 命中错误

### Higher-level exploratory reproducer

在 `outputCursorWriter` / `frame lines path` 上，宽度往返后还能观察到更高层的最终画面分歧；但这条链路里混杂了 pane 布局、overflow hint、cursor writer diff 等多个因素，目前不作为主线稳定 reproducer。
