# 代理说明

## 最高工作基准

- `AGENTS.md` 是本仓库代理执行方式、范围判断、测试和提交纪律的默认基准。
- 后续工作不再由 `workflow.md` 驱动；除非用户明确要求，不必读取、更新或按它的任务队列执行。
- 用户当前请求是活动驱动来源；范围必须结合本文件的目录职责、冻结规则和仓库实际状态判断。
- `termx-core-v2/docs/architecture.md` 是 core-v2 技术设计基准。
- `termx-tui-v3/docs/architecture.md` 是 tui-v3 技术设计基准。
- 若 `workflow.md`、旧说明、聊天记录、旧代码行为或局部假设与本文件或用户当前请求冲突，默认以后者为准。

## 自动执行模式

当用户启动 `/goal` 或要求自动推进时，按下面循环执行：

1. 读取 `AGENTS.md`，需要技术细节时读取对应 architecture 文档。
2. 检查 `git status --short --branch`，确认是否存在未提交改动。
3. 根据用户当前目标、仓库状态和本文件约束拆出最早且最小可验证任务；如果用户给了明确清单，按清单顺序推进。
4. 如果当前任务阻塞，停止并向用户说明阻塞原因，不得自行跳到无关任务。
5. 只执行当前任务，不跨任务扩展范围。
6. 实现最小可验证改动，补齐必要 harness。
7. 运行与改动范围匹配的测试准入命令；文档-only 改动至少运行 `git diff --check`。
8. 只更新当前任务必要的文档和状态说明，不维护 `workflow.md` 状态。
9. 使用中文提交信息提交当前任务。
10. 若 `/goal` 仍在继续且存在明确下一任务，再进入下一轮。

如果没有明确阻塞，不要停下来要求用户确认普通实现细节。若范围、语义或目录权限不清，必须先向用户说明阻塞。

## 范围规则

- 允许主动工作目录由用户当前请求、本文件目录职责和必要联动决定，默认只触碰最小相关目录。
- 不允许因为“看起来有关”自行扩散到其他目录。
- 旧 `termx-core/` 与 `tuiv2/` 已退出本分支，不再作为只读参考、legacy fallback 或默认依赖存在。
- 当前默认本地 CLI 入口必须走 `termx-core-v2/` 与 `termx-tui-v3/`；不得重新引入 `termx legacy ...`、旧 daemon、旧 TUI 或 remote legacy/fallback。
- `termx-cli/cmd/termx/legacy_*.go` 不得重新出现；旧本地入口已经删除。
- `termx-cli/cmd/termx/remote_*.go` 只能通过 core-v2 daemon/protocol/service extension 接入，不得 import 旧 `termx-core` 或 `tuiv2`。
- `termx-cli/cmd/termx/default_dependency_guard_test.go` 是默认入口依赖守卫；默认源文件不得 import 旧 `termx-core` 或 `tuiv2`。
- 当前进入 remote 迁移阶段时，仍必须保持默认本地入口走 `termx-core-v2/` 与 `termx-tui-v3/`；remote 迁移只能通过 core-v2 protocol/service extension、`termx-remote` public package 和 CLI glue 接入，不能把默认路径退回旧 daemon 或旧 TUI。
- `termx remote ...` 从 legacy/fallback 迁出必须按用户给定计划或明确任务逐步完成：先审计和契约，再 core-v2 extension hook，再 CLI 装配，再启用 local/pair flow。不得一次性大搬旧实现。
- 协议迁移必须以 core-v2 domain contract 为唯一目标；不为旧 `termx-core/` 保留 wire format、storage format、method adapter、双 handler、fallback 读写或兼容 shim。
- remote 迁移期间允许触碰 `termx-remote/` 和必要的 `termx-cli/cmd/termx/remote_*.go`，但只能在当前任务直接需要时修改；`remote-ui/`、`web-control/`、`termx-hub/` 仍默认冻结。
- 如果确实必须恢复旧目录，先向用户说明原因并获得明确同意；默认不允许恢复。
- 冻结目录不得触碰，除非用户明确解冻或当前任务直接要求。
- 关键代码需要写上注释,使用中文
## 目录职责

- `termx-core-v2/`：新 core 主线目录，负责 logical-line-first 历史模型、`HistoryTrack`、`LiveSurfaceTrack`、`HistoryWindow`、storage/backend 与相关 harness。
- `termx-core-v2/docs/architecture.md`：core-v2 技术设计基准。
- `termx-tui-v3/`：新 TUI 主线目录，负责自有 `AppRuntime`、`TerminalHost`、`EffectRunner`、`FrameSink`、authoritative history store/source、copy mode、滚动、selection、render 与相关 harness。
- `termx-tui-v3/docs/architecture.md`：tui-v3 技术设计基准。
- `termx-core/`：已删除旧 core 目录；不得作为 fallback 恢复。
- `tuiv2/`：已删除旧 TUI 目录；不得作为 fallback 恢复。
- `termx-vterm/`：受限联动目录，只在新 core-v2/tui-v3 的 terminal 或 protocol 契约确实需要时最小化触及。
- `internal/protocol/` 与 `termx-proto/`：受限联动目录，只在相关 contract 或 protocol adapter 任务需要时最小化触及。
- `termx-remote/`：remote runtime/service 主线目录，只在 remote 迁移任务中修改；它不能直接拥有 core-v2 terminal/history truth，只能通过 core-v2 daemon/protocol adapter 访问。
- `termx-remote-v2/`：remote v2 设计/实验目录；默认不触碰，除非用户明确把它纳入当前任务。
- `termx-cli/`、`termx-shared/`、`termx-testkit/`、`scripts/`、`Makefile`、`go.work`、`go.work.sum`、必要顶层说明文档：受限联动范围，只在当前任务需要时最小化触及。

## 硬语义规则

- 禁止症状补丁：遇到状态错乱、输入错路由、生命周期误判或恢复异常时，必须先定位权威状态边界和消息链路，再修改模型或契约；不得用 storage scrub、fallback、定时刷新、重复 attach、局部 if 分支等方式掩盖根因。
- 禁止补丁式实现：不得为了让当前 case 通过而堆叠临时分支、局部兜底、重复同步、隐式状态修正或旧路径兼容；每次修复都必须先说清 domain owner、truth source、消息链路和失败条件，再按模型/契约补 harness 后实现。
- panel/pane 只表达工作台槽位和连接意图：空或连接到 terminal view。terminal 是否 running/exited、退出码、退出时间、命令、restart 判断都属于 core terminal lifecycle，不得写入 workbench storage 或 pane kind。
- copy/history 是当前 TUI 的交互态，属于 `CopyModeStore`/`HistoryStore` 投影，不得作为 pane kind 或 workbench storage 状态持久化。
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
- remote 不能拥有第二份 terminal truth：terminal lifecycle、PTY size、attachment、events、history 和 storage 必须来自 core-v2 daemon/protocol；remote 只负责授权、配对、transport/session 和请求路由。
- remote storage 只能走 core-v2 storage API；不得把 TUI workbench、terminal lifecycle 或 copy/history 交互态写成 remote 私有 truth。
- remote management request 必须通过清晰 adapter 路由到 core-v2 public/protocol 方法；不得直接读写 TUI reducer state、renderer、TerminalHost 或旧 core runtime。
- 新协议结构必须直接表达 core-v2 的 terminal、attachment、history、storage 和 event 模型；不得先模拟旧 core 协议再翻译到 core-v2。

## 实现纪律

- 先写 domain model 和小 harness，再接真实 protocol、terminal 或 CLI 入口。
- 代码必须按正确模型写完整：如果只能靠“再补一个判断”“再刷一次状态”“失败就 fallback”“先 scrub storage”才能成立，默认方案不合格，需要回到状态归属和契约设计重新做。
- 当前处于开发周期，不做旧内部实现、旧 storage/协议格式、旧 snapshot/workbench schema 或旧运行时行为的兼容；需要破坏性调整时直接按新模型改，删除旧路径。
- 不为兼容旧内部实现保留双路径、适配层、桥接代码、旧格式读取分支或迁移兜底，除非用户当前请求或新设计文档明确要求。
- remote/protocol 迁移时发现旧 core contract 与 core-v2 contract 冲突，必须改向 core-v2；不得为了旧客户端或旧 daemon 继续工作而保留兼容代码。
- 从旧实现迁移代码时，迁入新目录后必须按新边界重命名、裁剪依赖并补 v2/v3 harness。
- service 不得直接修改 reducer-owned state；必须通过 message/effect 回到主循环。
- renderer 只消费 view-model，不读 core client、history source、runtime service 或 protocol client。
- 手工编辑文件必须使用 `apply_patch`。
- 不得使用 destructive git 命令。
- 不得覆盖用户或其他代理的未提交改动；发现冲突时停下说明。

## 测试和提交

- 每个有效变动提交前必须运行与改动范围匹配的测试准入命令。
- 文档-only 改动至少运行 `git diff --check`。
- 如果测试无法运行，最终说明必须写清原因。
- 每个有效变动必须提交，提交信息必须使用中文。
- 一次任务尚未达到可提交状态时，先收敛任务，不要继续扩大改动面。
- 不得 amend commit，除非用户明确要求。

## 子代理使用

- 只有当用户明确要求子 Agent、审核或并行代理工作时才使用子代理。
- 子代理适合做只读审核、独立探索或互不重叠的实现任务。
- 子代理审核后的 findings 必须先本地判断并处理，再提交最终结果。
