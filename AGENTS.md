# 代理说明

## 最高工作基准

- 仓库根目录 `workflow.md` 是当前分支唯一有效的活动驱动文件。
- 本仓库内所有工作必须先读取 `workflow.md`，并以它作为范围、任务顺序、测试准入和提交规则的唯一基准。
- 当前主线是 screen app 无限历史清场与重建；旧 remote + app 迁移队列已经退出当前活动范围，只能按 git 历史追溯。
- `termx-core-v2/docs/screen-app-infinite-history-final-plan.md` 是当前无限历史技术定案。
- `termx-core-v2/docs/architecture.md` 是 core-v2 技术设计基准。
- `termx-tui-v3/docs/architecture.md` 是 tui-v3 技术设计基准。
- `AGENTS.md` 只规定代理执行方式和目录职责，不替代 `workflow.md` 的范围判断。
- 若 `workflow.md` 与旧说明、聊天记录、旧代码行为或局部假设冲突，默认以 `workflow.md` 为准。

## 自动执行模式

当用户启动 `/goal` 或要求自动推进时，按下面循环执行：

1. 读取 `workflow.md`。
2. 检查 `git status --short --branch`，确认是否存在未提交改动。
3. 如果存在未提交改动，先判断来源和范围：若只有当前文档基线改动，先运行文档准入并提交；凡不是本轮 Agent 已识别的当前切片改动，一律停止说明，除非用户明确要求接管；不得把用户或其他代理改动混入本切片提交。
4. 按 `workflow.md` 任务队列表格顺序选择最早未完成切片。
5. 如果最早未完成切片是 `阻塞`，停止并向用户说明阻塞，不得跳到后续 `待开始` 切片。
6. 如果最早未完成切片是 `待开始`，先把它改为 `进行中`，并提交或与本切片首个实现提交同切片提交。
7. 只执行该切片，不跨切片扩展范围。
8. 需要技术细节时读取对应 architecture 文档和 `termx-core-v2/docs/screen-app-infinite-history-final-plan.md`。
9. 实现最小可验证改动，先补齐该切片要求的 harness，再接真实实现。
10. 运行该切片的测试准入命令。
11. 更新 `workflow.md` 中该切片状态和必要的当前状态说明。
12. 使用中文提交信息提交本切片。
13. 若 `/goal` 仍在继续，再进入下一切片。

如果没有明确阻塞，不要停下来要求用户确认普通实现细节。若范围、语义或目录权限不清，必须先更新 `workflow.md` 或向用户说明阻塞。

## 范围规则

- 允许主动工作目录只能来自 `workflow.md` 的“当前主线允许主动修改”和“受限联动范围”。
- 不允许因为“看起来有关”自行扩散到其他目录。
- 旧 `termx-core/` 与 `tuiv2/` 已退出本分支，不再作为只读参考、legacy fallback 或默认依赖存在。
- 当前默认本地 CLI 入口必须走 `termx-core-v2/` 与 `termx-tui-v3/`；不得重新引入 `termx legacy ...`、旧 daemon、旧 TUI 或 remote legacy/fallback。
- `termx-cli/cmd/termx/legacy_*.go` 不得重新出现；旧本地入口已经删除。
- `termx-cli/cmd/termx/default_dependency_guard_test.go` 是默认入口依赖守卫；默认源文件不得 import 旧 `termx-core` 或 `tuiv2`。
- `termx-remote/`、`termx-remote-v2/`、`termx-app/`、`remote-ui/`、`web-control/`、`termx-hub/` 当前冻结，除非 `workflow.md` 当前切片明确解冻。
- `termx-vterm/` 是受限联动目录，只能在 terminal semantic transaction 接口、事件或 harness 需要时最小化触及。
- `internal/protocol/` 与 `termx-proto/` 是受限联动目录，只能在 `history.window`、history copy 或 semantic history contract 需要跨进程时最小化触及。
- 如果确实必须恢复旧目录或解冻目录，先修改 `workflow.md` 的范围表并说明原因；默认不允许恢复。
- 关键代码需要写简短中文注释，说明 domain owner、truth source、消息链路或失败条件。

## 目录职责

- `termx-core-v2/`：新 core 主线目录，负责 logical-line-first 历史模型、terminal semantic transaction 消费、screen app session、segment cursor、`HistoryWindow`、storage/backend 与相关 harness。
- `termx-core-v2/docs/screen-app-infinite-history-final-plan.md`：当前无限历史定案。
- `termx-core-v2/docs/architecture.md`：core-v2 技术设计基准。
- `termx-vterm/`：终端语义解释来源；负责把 PTY bytes 解释成 terminal 语义事件或 transaction，不负责持有无限历史 truth。
- `termx-tui-v3/`：新 TUI 主线目录，负责自有 `AppRuntime`、`TerminalHost`、`EffectRunner`、`FrameSink`、authoritative history source、copy mode、滚动、selection、render 与相关 harness；不拥有 committed history truth。
- `termx-tui-v3/docs/architecture.md`：tui-v3 技术设计基准。
- `termx-core/`：已删除旧 core 目录；不得作为 fallback 恢复。
- `tuiv2/`：已删除旧 TUI 目录；不得作为 fallback 恢复。
- `internal/protocol/` 与 `termx-proto/`：受限联动目录，只在 history window/copy 或 semantic history contract 需要时最小化触及。
- `termx-cli/`、`termx-shared/`、`termx-testkit/`、`scripts/`、`Makefile`、`go.work`、`go.work.sum`、必要顶层说明文档：受限联动范围，只在当前切片需要时最小化触及。

## 硬语义规则

- 禁止症状补丁：遇到状态错乱、输入错路由、生命周期误判或恢复异常时，必须先定位权威状态边界和消息链路，再修改模型或契约；不得用 storage scrub、fallback、定时刷新、重复 attach、局部 if 分支等方式掩盖根因。
- 禁止补丁式实现：不得为了让当前 case 通过而堆叠临时分支、局部兜底、重复同步、隐式状态修正或旧路径兼容；每次修复都必须先说清 domain owner、truth source、消息链路和失败条件，再按模型/契约补 harness 后实现。
- 当前无限历史主线允许先删后写：旧补丁式历史代码、raw parser fallback、程序名特殊分支、snapshot/history 拼接 fallback、重复同步和隐式状态修正可以按 `workflow.md` 当前切片删除。
- 历史 truth 的基本单位是 logical line，不是 visual row、wrapped row、snapshot scrollback、grid viewport、xterm buffer row 或 DOM/canvas row。
- core-v2 的 logical-line history 是唯一历史数据模型。
- `CommittedHistoryIndex`、`MutableFrontier`、segment cursor、storage backend、cache、adapter、TUI/App projection 不能演变成第二份历史 truth。
- `persisted` 或落盘不表示不可修改；是否可修改由 session/segment/finalization 语义决定。
- raw PTY bytes parser 不能作为 terminal 语义 owner，也不能 fallback 出第二套历史。
- core-v2 应消费 termx-vterm 解释过程中的 semantic transaction，而不是消费最终屏幕快照。
- vterm 当前屏幕不是无限历史来源；它只能提供终端语义解释后的可记录事件。
- tmux 等价目标只覆盖真实经过 PTY 的内容；程序没有输出到 PTY 的内部状态不在目标内。
- attach、reattach、bootstrap、recovery、full replace、clear screen、resize 不得凭空创建 committed history。
- resize 不得重写 committed history；普通 logical line 只能在展示层重新 wrap，final screen-frame 必须固定生成时宽度。
- alt-screen 不写入 primary history；纯 alt-screen transient 退出时不 commit 屏幕内容。
- primary screen app 临时进入 alt-screen 前必须 archive/hide 当前 primary frame；退出 alt 后如果出现新的 primary 输出，必须作为新的 primary frame publish，可以接回同一 session journal，但不得复活 pre-alt current frame，也不得凭空 commit alt 屏幕。
- process exit 必须 force commit primary mutable frontier，并按分类决定是否生成 final screen-frame。
- default fg/bg 应保存为语义属性，由查看历史时的主题解析；明确 RGB 颜色属于内容属性，不能被后续主题替换。
- 不得为 Codex、Claude Code、htop、vim 等程序名写特殊适配；只能按终端语义和屏幕行为分类。
- panel/pane 只表达工作台槽位和连接意图：空或连接到 terminal view。terminal 是否 running/exited、退出码、退出时间、命令、restart 判断都属于 core terminal lifecycle，不得写入 workbench storage 或 pane kind。
- copy/history 是当前 TUI 的交互态，属于 `CopyModeStore`/`HistoryStore` 投影，不得作为 pane kind 或 workbench storage 状态持久化。
- tui-v3 不拥有 committed history truth，只消费 core-v2 authoritative `HistoryWindow`。
- tui-v3 copy mode 不得从本地 VTerm scrollback、snapshot totals、row ownership、LoadedRows、wrapped 拼接结果推断历史。
- tui-v3 不以 Bubble Tea 作为主运行时。
- 禁止在 tui-v3 主线引入 Bubble Tea `Program`、`standardRenderer`、`tea.Model`、`tea.Msg`、`tea.Cmd`、`tea.KeyMsg`、`tea.MouseMsg`、`bubbles` 或依赖这些 contract 的 UI 组件。
- 允许 `lipgloss/v2`、`x/ansi` 作为纯渲染/样式/ANSI 辅助；允许 `ultraviolet` 隔离在 `TerminalHost` 或 `FrameSink` 内作为终端 primitive。
- `hot/cold` 只能出现在旧模型问题说明或迁移记录中，不得作为新代码、测试 helper、内部 contract 或运行时状态命名。

## 实现纪律

- 先写 domain model 和小 harness，再接真实 protocol、terminal 或 CLI 入口。
- 代码必须按正确模型写完整：如果只能靠“再补一个判断”“再刷一次状态”“失败就 fallback”“先 scrub storage”才能成立，默认方案不合格，需要回到状态归属和契约设计重新做。
- 当前处于开发周期，不做旧内部实现、旧 storage/协议格式、旧 snapshot/workbench schema 或旧运行时行为的兼容；需要破坏性调整时直接按新模型改，删除旧路径。
- 不为兼容旧内部实现保留双路径、适配层、桥接代码、旧格式读取分支或迁移兜底，除非 `workflow.md` 明确要求。
- 从旧实现迁移代码时，迁入新目录后必须按新边界重命名、裁剪依赖并补 v2/v3 harness。
- service 不得直接修改 reducer-owned state；必须通过 message/effect 回到主循环。
- renderer 只消费 view-model，不读 core client、history source、runtime service 或 protocol client。
- 手工编辑文件必须使用 `apply_patch`。
- 不得使用 destructive git 命令。
- 不得覆盖用户或其他代理的未提交改动；发现冲突时停下说明。

## 测试和提交

- 每个有效切片提交前必须运行 `workflow.md` 规定的测试准入命令。
- 文档-only 改动至少运行 `git diff --check`。
- 如果测试无法运行，最终说明必须写清原因。
- 每个有效变动必须提交，提交信息必须使用中文。
- 用户明确要求不要提交时，按用户最新指令执行，并在最终说明未提交。
- 一次切片尚未达到可提交状态时，先收敛切片，不要继续扩大改动面。
- 不得 amend commit，除非用户明确要求。

## 子代理使用

- 只有当用户明确要求子 Agent、审核或并行代理工作时才使用子代理。
- 子代理适合做只读审核、独立探索或互不重叠的实现切片。
- 子代理审核后的 findings 必须先本地判断并处理，再提交最终结果。
