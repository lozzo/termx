# 代理说明

## 最高工作基准

- 仓库根目录 `workflow.md` 是当前分支唯一有效的活动驱动文件。
- 本仓库内所有工作必须先读取 `workflow.md`，并以它作为范围、任务顺序、测试准入和提交规则的唯一基准。
- `termx-core-v2/docs/architecture.md` 是 core-v2 技术设计基准。
- `termx-tui-v3/docs/architecture.md` 是 tui-v3 技术设计基准。
- `AGENTS.md` 只规定代理执行方式和目录职责，不替代 `workflow.md` 的范围判断。
- 若 `workflow.md` 与旧说明、聊天记录、旧代码行为或局部假设冲突，默认以 `workflow.md` 为准。

## 自动执行模式

当用户启动 `/goal` 或要求自动推进时，按下面循环执行：

1. 读取 `workflow.md`。
2. 检查 `git status --short --branch`，确认是否存在未提交改动。
3. 按 `workflow.md` 任务队列表格顺序选择最早未完成切片。
4. 如果最早未完成切片是 `阻塞`，停止并向用户说明阻塞，不得跳到后续 `待开始` 切片。
5. 如果最早未完成切片是 `待开始`，先把它改为 `进行中`，并提交或与本切片首个实现提交同切片提交。
6. 只执行该切片，不跨切片扩展范围。
7. 需要技术细节时读取对应 architecture 文档。
8. 实现最小可验证改动，补齐该切片要求的 harness。
9. 运行该切片的测试准入命令。
10. 更新 `workflow.md` 中该切片状态和必要的当前状态说明。
11. 使用中文提交信息提交本切片。
12. 若 `/goal` 仍在继续，再进入下一切片。

如果没有明确阻塞，不要停下来要求用户确认普通实现细节。若范围、语义或目录权限不清，必须先更新 `workflow.md` 或向用户说明阻塞。

## 范围规则

- 允许主动工作目录只能来自 `workflow.md` 的“当前主线范围”和“受限联动范围”。
- 不允许因为“看起来有关”自行扩散到其他目录。
- 旧 `termx-core/` 与 `tuiv2/` 默认只读参考；不得继续原地修补旧 logical-line、copy mode、snapshot/grid viewport history path。
- 如果确实必须修改旧目录，先修改 `workflow.md` 的范围表并说明原因。
- 冻结目录不得触碰，除非 `workflow.md` 先明确解冻。

## 目录职责

- `termx-core-v2/`：新 core 主线目录，负责 logical-line-first 历史模型、`HistoryTrack`、`LiveSurfaceTrack`、`HistoryWindow`、storage/backend 与相关 harness。
- `termx-core-v2/docs/architecture.md`：core-v2 技术设计基准。
- `termx-tui-v3/`：新 TUI 主线目录，负责自有 `AppRuntime`、`TerminalHost`、`EffectRunner`、`FrameSink`、authoritative history store/source、copy mode、滚动、selection、render 与相关 harness。
- `termx-tui-v3/docs/architecture.md`：tui-v3 技术设计基准。
- `termx-core/`：旧 core 参考目录，只能读取、搜索、运行测试或摘取外部契约参考。
- `tuiv2/`：旧 TUI 参考目录，只能读取、搜索、运行测试或摘取外部契约参考。
- `termx-vterm/`：受限联动目录，只在新 core-v2/tui-v3 的 terminal 或 protocol 契约确实需要时最小化触及。
- `internal/protocol/` 与 `termx-proto/`：受限联动目录，只在 `history.window` contract 或 protocol adapter 切片需要时最小化触及。
- `termx-cli/`、`termx-shared/`、`termx-testkit/`、`scripts/`、`Makefile`、`go.work`、`go.work.sum`、必要顶层说明文档：受限联动范围，只在当前切片需要时最小化触及。

## 硬语义规则

- 历史 truth 的基本单位是 logical line，不是 visual row、wrapped row、snapshot scrollback 或 grid viewport。
- core-v2 的 `LogicalLineStore` 是唯一历史数据模型。
- `CommittedHistoryIndex`、`MutableFrontier`、`StorageBackend` 不能演变成第二份历史 truth。
- `persisted` 或落盘不表示不可修改。
- attach、reattach、bootstrap、recovery、full replace、clear screen、resize 不得凭空创建 committed history。
- resize 不得重写历史；grow resize 只能按完整 logical line reclaim committed suffix，shrink resize 表达 hidden mutable frontier。
- alt-screen 不写入 primary history；process exit 必须 force commit primary mutable frontier。
- tui-v3 不拥有 committed history truth，只消费 core-v2 authoritative `HistoryWindow`。
- tui-v3 copy mode 不得从本地 VTerm scrollback、snapshot totals、row ownership、LoadedRows、wrapped 拼接结果推断历史。
- tui-v3 不以 Bubble Tea 作为主运行时。
- 禁止在 tui-v3 主线引入 Bubble Tea `Program`、`standardRenderer`、`tea.Model`、`tea.Msg`、`tea.Cmd`、`tea.KeyMsg`、`tea.MouseMsg`、`bubbles` 或依赖这些 contract 的 UI 组件。
- 允许 `lipgloss/v2`、`x/ansi` 作为纯渲染/样式/ANSI 辅助；允许 `ultraviolet` 隔离在 `TerminalHost` 或 `FrameSink` 内作为终端 primitive。
- `hot/cold` 只能出现在旧模型问题说明或迁移记录中，不得作为新代码、测试 helper、内部 contract 或运行时状态命名。

## 实现纪律

- 先写 domain model 和小 harness，再接真实 protocol、terminal 或 CLI 入口。
- 不为兼容旧内部实现保留双路径、适配层或桥接代码，除非 `workflow.md` 明确要求。
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
- 一次切片尚未达到可提交状态时，先收敛切片，不要继续扩大改动面。
- 不得 amend commit，除非用户明确要求。

## 子代理使用

- 只有当用户明确要求子 Agent、审核或并行代理工作时才使用子代理。
- 子代理适合做只读审核、独立探索或互不重叠的实现切片。
- 子代理审核后的 findings 必须先本地判断并处理，再提交最终结果。
