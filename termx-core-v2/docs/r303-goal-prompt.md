# R303 /goal prompt

把下面这段作为 `/goal` 的任务说明使用。

```text
继续当前仓库的 screen app 无限历史主线，完成 `workflow.md` 中的 R303：history projector domain harness。

开始前必须读取：
- `workflow.md`
- `termx-core-v2/docs/screen-app-infinite-history-final-plan.md`
- `termx-core-v2/docs/architecture.md`
- `termx-core-v2/history/*.go`
- `termx-vterm/vterm/semantic_source.go`

当前基础状态：
- 旧 history 实现已经清空，不能恢复旧 `HistoryTrack`、raw parser、snapshot/window 拼接、terminal history pipeline/queue 或 storage scrub 路径。
- `termx-core-v2/history/` 只保留 logical-line payload 定义和本轮新增的 domain contract 文件。
- `history.window` / `history.copy` 仍应在 R303 完成前返回 `ErrHistoryNotRebuilt`，不要临时接 live surface 或旧 protocol fallback。

R303 只做 domain harness 和最小 domain 骨架，不接真实 PTY、protocol、TUI 或文件 storage：
1. 在 `termx-core-v2/history` 内写 fake semantic transaction / fake classifier / fake projector harness。
2. 覆盖普通输出 commit、progress bar CR 覆写、primary synchronized frame current-only、primary fullscreen repaint current-only、`/resume` alt transient、vim/htop alt no-commit、terminal exit force close、resize non-history boundary、frozen copy boundary 这些 case 的最小断言。
3. harness 必须先表达 domain owner、truth source、消息链路和失败条件，再补实现。
4. 如果需要实现，只实现纯内存 projector/store 最小骨架，且只能消费 `TerminalSemanticTransaction`、`ScreenAppDecision`、`HistoryEvent`、`HistoryMutation`。
5. 不允许按进程名写分支；不允许从 `LiveSurfaceTrack`、snapshot、grid viewport、renderer rows、TUI local rows、raw PTY replay 推断 history truth。
6. 不允许把 segment cursor 偷塞回旧 `before_line_id` 语义；window/copy 相关 harness 必须使用 `HistoryCursor.Segment` 或 opaque token。
7. R303 完成后更新 `workflow.md`，运行 `cd termx-core-v2 && go test ./... -count=1` 和 `git diff --check`，用中文提交信息提交。

完成标准：
- R303 的 harness 能阻止旧补丁式路径复活。
- projector/store 的基础行为只在 domain 包内可验证。
- R304 仍是普通输出真实接入，不要跨切片实现。
```
