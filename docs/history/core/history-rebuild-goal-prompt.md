# 历史重建 /goal prompt

把下面这段作为 `/goal` 的任务说明使用。它不是只做 R303，而是让 Agent 按
`workflow.md` 从最早未完成切片开始，逐片推进 R303-R310。

```text
继续当前仓库的 screen app 无限历史主线。你必须按 `workflow.md` 的任务队列自动推进：每轮只取最早未完成切片，完成该切片的 harness、实现、准入测试、`workflow.md` 状态更新和中文提交后，再继续下一切片；不要跳过阻塞切片，不要把多个切片混进一个提交。

开始前必须读取：
- `workflow.md`
- `AGENTS.md`
- `docs/history/core/screen-app-infinite-history-final-plan.md`
- `core/docs/architecture.md`
- `tui/docs/architecture.md`，仅在 R310 或 tui-v3 联动时读取相关部分
- `core/history/*.go`
- `vterm/vterm/semantic_source.go`

当前基础状态：
- 旧 history 实现已经清空，不能恢复旧 `HistoryTrack`、raw parser、snapshot/window 拼接、terminal history pipeline/queue 或 storage scrub 路径。
- `core/history/` 只保留 logical-line payload 定义和 domain contract 文件。
- `history.window` / `history.copy` 在真正完成 R310 接入前不能临时接 live surface、snapshot、TUI local rows 或旧 protocol fallback。
- 所有新增或修改的导出 type/interface/struct/方法/函数必须写详细中文注释；关键代码路径也要写清领域归属、真值来源、消息链路、失败条件或调用边界。

必须按下面切片顺序推进，不允许为了看起来快而跨切片：

R303 history projector domain harness：
- 在 `core/history` 内写 fake semantic transaction / fake classifier / fake projector harness。
- 覆盖普通输出 commit、progress bar CR 覆写、primary synchronized frame current-only、primary fullscreen repaint current-only、`/resume` alt transient、vim/htop alt no-commit、terminal exit force close、resize non-history boundary、frozen copy boundary这些 case 的最小断言。
- 如果需要实现，只实现纯内存 projector/store 最小骨架，且只能消费 `TerminalSemanticTransaction`、`ScreenAppDecision`、`HistoryEvent`、`HistoryMutation`。

R304 普通输出最小实现：
- 接 shell/stdout 普通输出 logical line commit。
- CR/BS/EL/CUP 等只能 mutate frontier，不能追加中间态。
- process exit 必须 force commit primary mutable frontier。
- 不创建 screen app session，不回退 raw parser。

R305 primary screen app session 最小实现：
- 支持 primary screen 上的 repaint/current frame。
- current frame 可进入 latest/frozen projection，`Committed=false`。
- repaint 不增长 ordinary committed depth，不按程序名特判。

R306 alt-screen transient 与 primary archive：
- alt-screen current frame 可选择，但默认不 ordinary commit。
- primary app 进入 alt 前 archive/hide 当前 primary frame，退出 alt 后新 primary publish 不能复活 pre-alt current frame。
- `/resume` picker、vim/htop 只能靠 terminal semantics 分类，不能写程序名分支。

R307 resize 与 final screen-frame harness：
- resize 不重写 committed history。
- final screen-frame 固定生成时宽度，进入 committed index 后仍是 `screen-frame` fixed-grid kind。
- terminal/process exit final commit 只发生一次。

R308 色彩属性与主题解析边界：
- default fg/bg 保存为语义默认，不提前烘焙查看端主题 RGB。
- 明确 RGB/256 色/16 色 token 的存储和渲染边界要有 harness。
- 如需 tui-v3 联动，只做最小渲染/解析接入。

R309 storage backend 无限历史接口：
- 文件/mmap/backend 只能承载 payload/index/journal/recovery，不能定义 mutability。
- append/update/recover/compact 需保持 `LogicalLineStore`、`CommittedHistoryIndex`、`MutableFrontier`、`ScreenFrameJournal` 的 domain truth。
- 不允许让文件格式变成第二份 history 模型。

R310 protocol/TUI history window 接入：
- `history.window`、`history.copy`、`history.release` 接 core-v2 authoritative `HistoryWindow`。
- cursor/token 必须表达 segment；不能让 TUI 从 `before_line_id`、LoadedRows、本地 row count、snapshot totals 或 local scrollback 推断分页。
- tui-v3 copy/history 只消费 authoritative window/copy，live display 与 history surface 分层。

全程硬约束：
- 不允许按进程名分支。
- 不允许从 `LiveSurfaceTrack`、snapshot、grid viewport、renderer rows、TUI local rows、raw PTY replay 推断 history truth。
- 不允许 storage scrub、定时刷新、重复 attach、局部 fallback 或桥接旧路径。
- 每个切片先补 harness，再接实现。
- 每个切片按 `workflow.md` 运行准入测试和 `git diff --check`，更新 `workflow.md`，中文提交。
- 如果某个切片阻塞，把该切片标为 `阻塞` 并说明原因；不得跳到后续切片。
```
