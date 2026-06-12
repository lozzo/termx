# 工作流：termx TerminalView/Attachment 主线

本文件是当前分支唯一有效的活动驱动文件。后续分析、实现、测试、提交都先看这里；如果本文件与旧说明、聊天记录、旧代码行为冲突，以本文件为准。

本文件只保留 5 类东西：

- 当前目标
- 允许改哪里
- 不能改错的语义
- 当前任务顺序
- 测试和提交规则

架构细节不在这里展开，分别以 `termx-core-v2/docs/architecture.md`、`termx-tui-v3/docs/architecture.md`、`termx-tui-v3/docs/ui-interaction-spec.md`、`termx-tui-v3/docs/render-architecture.md`、`termx-cli/docs/v2-v3-switch-audit.md` 为准。

## 0. 怎么读这个文件

如果只是准备开始干活，先看这 4 段：

- `1. 当前目标`
- `4. 不可违反的语义`
- `5. 任务队列`
- `7. 测试准入`

如果你发现“用户想做的事”和“当前任务顺序”冲突，先改本文件，再改代码，不要靠口头约定跳过去。

例子：

- 如果 `215D1` 还是 `待开始`，那就先做 `215D1`，不能因为 `215E` 看起来更有意思就直接跳过去。
- 如果最早未完成切片是 `阻塞`，那就停下说明阻塞，不能继续做后面的 `待开始`。

## 1. 当前目标

### 1.1 一句话目标

把 `TerminalView/Attachment` 做成一等模型：同一个 terminal 可以同时被多个 pane 或 floating 连接，但 terminal process、live surface、authoritative history 仍然只有一份。

### 1.2 现在已经做到哪里

- 默认本地入口已经切到 `termx-core-v2/` 和 `termx-tui-v3/`。
- `termx-tui-v3` 已有自己的 runtime、input、state、service、terminal host、copy mode 和 styled render framework，不依赖 Bubble Tea 运行时。
- runtime 已经改成事件驱动；真实 CLI attach 不再走外层 `16ms` 轮询；resize 已经是 latest-wins。
- live/history 的边界已经收口：live 只负责“现在屏幕是什么”，history 只负责 authoritative `HistoryWindow`。
- core-v2 authoritative history stale guard 和 tui-v3 active-view history binding 已经补齐。

### 1.3 这轮还没做完的事

- floating group commands 还没做完。
- 非 history 快捷键的真实集成证据和 tmux 黑盒证据还没补齐。
- clipboard paste 和完整 clipboard history overlay 还没重启。
- 但 authoritative history 浏览与复制主链已经具备基础，可以先单独收口。

### 1.4 做完后应该是什么样

- 同一个 terminal 可以被多个 pane/floating 同时观察。
- 只有一个 view 能改 PTY size，别的 view 只能跟随，不会抢着改尺寸。
- close pane 只关闭当前工作位，不会顺手把 terminal kill 掉。
- kill terminal 会影响所有绑定它的 view。
- copy mode 永远只吃 core-v2 返回的 authoritative `HistoryWindow`，不会从 live surface、snapshot 或本地 scrollback 反推历史。

### 1.5 两个关键例子

例子 1：同 terminal 双 pane

- 左 pane 和右 pane 都连到同一个 terminal。
- 左 pane 是 resize owner，右 pane 是 follower。
- 左 pane 从 `80x24` 缩到 `56x24`，PTY size 应该跟着左 pane 走。
- 右 pane 只能显示这份 `56x24` 的 live projection，不能因为自己更宽就把 PTY 改回去。
- 关掉右 pane，不 kill terminal；左 pane 继续工作。

例子 2：同 terminal 双 view 的 copy mode

- pane A 和 pane B 都连到同一个 terminal。
- pane A 进入 copy mode，请求了一份 `80` 列的 `HistoryWindow`。
- pane B 后来 resize 到 `56` 列并重绑 history。
- pane B 的 response 不能把 pane A 眼前的 window 顶掉；每个 active copy view 只认自己的 request、token、cols 和 boundary。

## 2. 技术设计基准

- core-v2：`termx-core-v2/docs/architecture.md`
- tui-v3：`termx-tui-v3/docs/architecture.md`
- UI 交互：`termx-tui-v3/docs/ui-interaction-spec.md`
- render framework：`termx-tui-v3/docs/render-architecture.md`
- CLI 切换审计：`termx-cli/docs/v2-v3-switch-audit.md`

实现时如果发现这些设计文档需要变，就和当前切片一起改；不要代码先跑偏，文档以后再补。

## 3. 工作范围

### 3.1 当前主线允许主动修改

- `termx-core-v2/`
- `termx-tui-v3/`
- `termx-cli/`
- `internal/protocol/`
- `termx-proto/`
- 根目录直接相关文件：`workflow.md`、`AGENTS.md`、`go.work`、`go.work.sum`、`Makefile`、必要顶层说明文档

### 3.2 受限联动范围

只有当前切片确实需要时，才允许最小化触及：

- `termx-vterm/`
- `termx-shared/`
- `termx-testkit/`
- `termx-remote/`
- `scripts/`

### 3.3 只读参考范围

默认不得修改：

- `termx-core/`
- `tuiv2/`

它们只能拿来读、搜、跑测试、找外部契约参考，不能继续在里面修旧逻辑，也不能把新实现包在旧实现外面假装迁移完成。

### 3.4 冻结范围

不得主动触碰：

- `remote-ui/`
- `termx-app/`
- `web-control/`
- `termx-hub/`
- `bin/`
- `.claude/`
- 顶层可执行产物和测试产物
- 未在本文件列出的目录

## 4. 不可违反的语义

### 4.1 历史 truth 只能是 logical line

- core-v2 的历史基本单位必须是 logical line，不是 visual row，不是 snapshot scrollback，不是 grid viewport。
- `LogicalLineStore` 是唯一历史 truth。
- `HistoryWindow` 只是按当前 `cols` 投影出来的窗口，不是新的历史来源。
- attach、reattach、bootstrap、recovery、full replace、clear screen、resize 都不能凭空创造 committed history。
- alt-screen 不写入 primary history；process exit 必须 force commit primary mutable frontier。

例子：

- 一个很长的 shell 输出在 `80` 列下显示成 3 行，在 `56` 列下显示成 5 行。变的是投影，不是历史本身。

### 4.2 tui-v3 只消费 authoritative history

- `termx-tui-v3` 不拥有 committed history truth。
- `HistoryStore` 只保存 core-v2 返回的 authoritative logical-line 历史快照、pending 状态和 exhausted 信息。
- `CopyModeStore` 只保存交互态：active view、terminal id、cursor、selection、frozen token 和当前本地投影 cols/rows。
- copy mode 缺冻结快照时，只能显示 pending、empty 或 error，不能从 live surface、snapshot 或本地 VTerm scrollback fallback。

例子：

- copy mode 正在等 `history.window` 响应时，屏幕上 live surface 已经有内容，也不能拿那几行 live 内容凑一个“临时历史”出来。
- copy mode 一旦进入并拿到冻结快照，后续 pane 变窄时可以在 TUI 本地按新宽度重新排版；但这批可排版的数据仍然必须来自 core-v2 的 authoritative logical-line 快照。

### 4.3 terminal 和 terminal view 不是一回事

- `Terminal` 是全局运行实体，拥有 process、PTY size、live surface 和 authoritative history。
- `TerminalView/Attachment` 是某个 pane/floating 对 terminal 的连接视图，拥有 view id、surface id、resize role、desired size、request seq、view-local error 等信息。
- 可以保留 `TerminalID` 作为 terminal identity，但不能再把裸 `TerminalID` 当成完整连接 truth。
- 输入必须进 active view 对应的 attachment channel，不能偷懒走“全局最新 session terminal”。
- close/detach pane 只移除 view binding，不 kill terminal；kill terminal 才是破坏性动作。

例子：

- 同一个 terminal 同时出现在一个 tiled pane 和一个 floating pane 里。它们共享同一个进程和同一份历史，但不共享各自的 focus、copy 状态、content rect 和错误提示。

### 4.4 resize 必须有 owner/follower 语义

- 同一 terminal 同时只能有一个有效 resize owner 修改 PTY size。
- follower 或 observer view 只能显示当前 terminal projection，不能因为自己 content rect 变化就覆盖 PTY size。
- owner transfer 必须走协议、effect、message 和 reducer 路径，不能在 UI 层本地偷偷改 truth。
- 发给 core-v2 terminal 的 resize 必须使用 active owner pane 的 content rect，而不是整个外部 terminal emulator 的尺寸。

例子：

- 左 pane 是 owner，右 pane 是 follower。右 pane 就算更宽，也只能把内容裁切得更舒服，不能把 PTY 改大。

### 4.5 render 和 runtime 的边界不能乱

- `RenderResult` 是唯一主输出；plain 文本、测试快照和 ANSI frame 都只是适配层。
- renderer 只消费 view-model 和 layout plan，不直接读 service、host 或 core client。
- `service` 不得直接修改 reducer-owned state；所有状态变化都必须走 message/effect 回主循环。
- `termx-tui-v3` 主线不得引入 Bubble Tea `Program`、`tea.Model`、`tea.Msg`、`bubbles` 等 contract。
- 默认视觉目标继续对齐 `tuiv2` 风格，不能退化成 ASCII `+ - |` 线框。

例子：

- 一个 pane split 完成后，正确路径应该是：semantic command -> reducer 改 pane 结构 -> layout remeasure -> effect 触发 resize -> renderer 重画，而不是 render 过程里顺手改 terminal size。

### 4.6 实现纪律

- 先写 domain model 和最小 harness，再接真实 protocol、terminal 或 CLI。
- 不为兼容旧内部实现保留长期双路径、桥接层或补丁式旁路，除非本文件明确批准。
- 大目标必须拆成可独立验证的小阶段，每个小阶段单独用 `SK:` 中文提交。
- 如果做着做着发现边界错了，先收口成文档/架构修正，再继续实现。
- 关键代码要加简短中文注释，说明真正不自明的逻辑。

## 5. 任务队列

状态只能使用：`待开始`、`进行中`、`完成`、`阻塞`。同一时间只能有一个切片处于 `进行中`。

自动执行时只看下面这张表，按顺序取最早未完成切片：

| 切片 | 状态 | 范围 | 白话说明 |
| --- | --- | --- | --- |
| 背景里程碑：0-215H3 | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`termx-cli/`、`internal/protocol/`、`termx-proto/`、相关文档 | 默认入口、runtime、styled render framework、TerminalView/Attachment 基线、resize ownership、history MVP H1-H3 都已经收口；更细的历史细节需要时去看 git 提交和架构文档 |
| 215D1. SK floating group commands | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/input/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`termx-tui-v3/docs/` | 已补齐 floating `v ALL`、`= FIT`、`s AUTO-FIT` 与 overview `s SHOW ALL`、`c COLLAPSE ALL`、`x CLOSE`；全部进入 reducer-owned floating state 与统一 `FloatingCommand`，`FIT/AUTO-FIT` 基于 terminal live/session 尺寸工作，auto-fit 在后续 live 尺寸变化时会刷新 floating rect；相关 reducer/render/storage harness 已通过 |
| 215F. SK shortcut integration and tmux harness | 完成 | `termx-cli/`、`termx-tui-v3/`、`termx-core-v2/`、`internal/protocol/`、`Makefile` 按需 | 已补 runtime 黑盒证据：floating overview/summon/show-all/collapse-all/close、terminal pool delete、同 terminal owner/follower resize 与 pane close 恢复都走真实 reducer/effect/message；并补 tmux owner/follower emoji-dots smoke 与 CLI close-pane resize 稳定性回归 |
| 215E1. SK history copy 主链收口 | 进行中 | `termx-core-v2/`、`termx-tui-v3/`、`internal/protocol/`、相关文档 | 改成完整 copy mode 冻结快照模型：先补清楚 line 何时 seal、何时 committable、何时 committed，再由 core-v2 返回 authoritative logical-line snapshot，TUI 本地负责展示、重排、搜索、选择、复制；older 分页继续回 core 按 snapshot token/boundary 拉更早 logical lines。例子：同一份冻结历史在窄 pane 里可排成 5 行，在宽 pane 里可排成 3 行；变的是客户端投影，不是历史 truth |
| 215E2. SK clipboard paste 主链 | 待开始 | `termx-tui-v3/input/`、`termx-tui-v3/app/`、`termx-tui-v3/services/`、`termx-cli/`、相关文档 | 把显示态 `p/P PASTE` 接上真实主链：从宿主 paste/clipboard 输入拿文本，按 active terminal view 路由到 terminal input，处理 bracketed paste 和错误反馈。例子：焦点在 pane A 时 paste，只能发到 pane A 对应 terminal，不能误发到别的 pane |
| 215E3. SK clipboard history overlay | 待开始 | `termx-tui-v3/state/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`termx-tui-v3/docs/` | 再补完整的 clipboard history overlay，包括列表、过滤、选择、粘贴/新建/删除等产品壳。这个切片不负责 authoritative terminal history，本质上是独立的 clipboard UI |

当前下一步：

- `215D1 floating group commands` 已完成
- `215F shortcut integration and tmux harness` 已完成
- `215E1 history copy 主链收口` 现在是当前切片，先把 commit/seal 语义、ownership ledger、snapshot contract、TUI 本地 reflow 与 older 分页全链路写清楚，再开始改实现
- `215E2 clipboard paste 主链`、`215E3 clipboard history overlay` 继续后排

## 6. 必做证据

这里不再展开成长清单，只保留“每类模块至少要证明什么”。

### 6.1 core-v2

至少要证明：

- 普通输出、换行、自动折行、宽字符、组合字符是稳定的。
- clear screen、clear scrollback、alt-screen、process exit 的边界是对的。
- resize 不会凭空造历史；grow/shrink 只影响投影和 frontier 语义。

### 6.2 HistoryWindow

至少要证明：

- latest 用 replace，older 用 prepend。
- exhausted、token/generation/cursor/boundary stale guard 是对的。
- 同一份冻结 logical-line 历史可以被不同客户端或同一客户端在不同 `cols` 下本地重投影。
- 分页边界按 stable logical-line boundary 走，不按 visual row 数量猜。
- 客户端只需要持有“当前已加载的 logical-line 切片”，不需要一次把整份历史全搬到本地。

例子：

- copy mode 在 `80` 列进入后拿到冻结快照，之后 pane 缩到 `56` 列时，TUI 可以用同一份冻结 logical-line 数据本地重排；但如果用户继续请求 older，仍然必须带着冻结 token/boundary 回 core 拉更早的 logical lines。
- 如果当前 TUI 只拿了 `line 920-1000` 这一段 logical lines，那么屏幕上只能展示和重排这段；继续往上翻时，再拿 `token + boundary` 去要 `line 880-919`，而不是一次把 `1-1000` 全拿回来。

### 6.3 history copy 全链路

至少要证明：

- `\n`、`\r`、覆写、erase、auto-wrap、scroll、resize、exit 这些终端语义，会把 line 明确推进到 `open / sealed / committable / committed / reclaimed` 等状态，而不是只停留在模糊的 frontier 抽象。
- core 拥有 primary screen ownership ledger，并且只有在 line 已 sealed 且 screen 不再持有它时，才允许它进入 committed history。
- 进入 copy mode 后，core 返回 frozen snapshot token、committed upper bound、frozen frontier 和 older boundary；后续新 live 输出不会污染这个 snapshot。
- TUI 只对 frozen logical-line snapshot 做本地重排和展示，不从 live surface 或旧 rows 反推历史。
- TUI resize 后只做本地 reflow；older 不足时继续带 snapshot token / boundary 回 core 拉更多 logical lines。

例子：

- `hello\rworld` 不应因为出现了 `\r` 就提前 committed；它仍属于可变区域，直到明确 seal + 脱离 screen ownership 才能沉淀。
- copy mode 打开后，terminal 又打印了 50 行新输出；live pane 能看到它们，但 frozen snapshot 继续只看到进入 copy mode 那一刻的上界。
- 如果屏幕高度是 `24` 行，而某条已经 `\n` 封口的 line 还在这 `24` 行里可见，它就只是 `sealed`，还不能算 `committable`；只有后续继续输出把它真正滚出 primary screen ownership，它才允许进入 committed history。

白话计划：

1. core 先把“什么时候只是 seal，什么时候才允许 commit”写死。
2. `\n` 只负责封口，不直接等于 committed；是否真正沉淀，要看 primary screen ownership ledger 里是否还持有这条 line。
3. 用户进入 copy mode 时，core 返回一份冻结快照：
   - `snapshot_token`
   - `committed_upper_bound`
   - 当前仍可变但要一起冻结的 `frozen_frontier_lines`
   - 第一批 logical-line payload
   - `older_boundary`
4. 这份冻结快照不是“整份历史全量复制一份”。
   - 已经 committed 的历史可以共享。
   - 当前仍可变、但这次 copy 需要看见的 frontier line 单独冻结。
   - 如果后续 live 还要修改这些被 snapshot 引用的 line，就按 line 做 copy-on-write。
5. TUI 只缓存当前这份 snapshot 已经加载到的 logical-line 切片，再按 pane 宽度本地重排。
6. pane resize 时，只重排本地 rows，不回 core 要一份新的 history truth。
7. 如果用户继续往上翻，而当前已加载切片不够，TUI 才带着 `snapshot_token + older_boundary` 回 core 拉更早的 logical lines。

再举一个完整例子：

1. 用户在 `80` 列 pane 进入 copy mode。
2. core 返回 `snapshot_token=S1`，以及 `line 920-1000` 这段 logical lines。
3. TUI 用这段数据在本地排成 `80` 列 rows。
4. pane 之后缩到 `56` 列，TUI 直接把同一批 logical lines 重新排成 `56` 列 rows，不回 core。
5. 用户继续 `PageUp`，发现 `line 920-1000` 不够了；这时才带着 `S1 + boundary(920)` 去拉 `line 880-919`。
6. 这期间 terminal live 又输出了 `1001-1050`。live pane 能看到这些新内容，但 `S1` 这份 frozen snapshot 不会被它们污染。

### 6.3 tui-v3

至少要证明：

- input key/mouse 会先变成 semantic intent，再进入 reducer/effect。
- live mode 和 copy mode 的 projection 严格分流。
- host viewport、host resize、layout measurement、content rect resize 都走 message path。
- styled render 的宽度安全、边框安全、ANSI 安全成立。
- pane/floating 的 split、close、zoom、resize、group command 真正改的是 reducer-owned state，不是临时渲染假象。

### 6.4 CLI / 集成

至少要证明：

- `termx` 默认本地入口继续走 core-v2/tui-v3。
- `new/ls/kill/rm`、attach、PTY live、input、resize、多 view attach 都能打通。
- tmux 或等价黑盒 smoke 能证明真实路径成立，而不是只有 fake harness 通过。

## 7. 测试准入

每个有效切片提交前，至少跑和改动范围相符的测试：

- core-v2 改动：`cd termx-core-v2 && go test ./... -count=1`
- tui-v3 改动：`cd termx-tui-v3 && go test ./... -count=1`
- protocol 改动：`cd internal && go test ./protocol/... -count=1`；如果动到 `termx-proto/`，再跑 `cd termx-proto && go test ./... -count=1`
- CLI 改动：`cd termx-cli && go test ./... -count=1`
- 默认入口、跨模块或迁移相关改动：按需加跑 `make test-v2-migration`
- 默认入口相关改动：还要确认 `go run ./termx-cli/cmd/termx --help` 能编译运行
- 文档-only 改动：至少跑 `git diff --check`

如果测试跑不了，最终说明里必须写清原因，不能把真实失败说成“偶发”。

## 8. 自动推进和提交规则

- 每次开始工作先读本文件，再跑 `git status --short --branch`。
- 只执行任务队列里最早未完成的切片。
- 如果最早未完成切片是 `阻塞`，必须停下说明原因，不能跳到后面。
- 如果最早未完成切片是 `待开始`，先把它标成 `进行中`，再开始做。
- 一个小阶段收口后，立刻更新本文件状态、跑准入、提交一个 `SK:` 中文提交。
- 不要长期堆未提交改动，也不要把多个小阶段硬塞进一个提交。
- 不得 amend commit，除非用户明确要求。
- 不得覆盖用户或其他代理的未提交改动；如果冲突，先停下说明。

例子：

- 如果 `215D1` 进行到一半才发现需要先改设计，那这一小段先收口成“设计修正”提交，再继续实现；不要一边改架构一边改功能最后混成一个大补丁。

## 9. 当前状态

- 默认本地入口已经切到 `termx-core-v2/` 和 `termx-tui-v3/`；旧本地路径只允许走 `termx legacy ...`，remote 仍按 legacy/fallback 隔离。
- `AppRuntime` 已是事件驱动批处理循环；真实 CLI attach 不再有外层 `16ms` 轮询；resize latest-wins 和 owner 转移链路已经收口。
- `215H1`、`215H2`、`215H3` 已完成，live/history 边界、core-v2 authoritative history stale guard、tui-v3 active-view history binding 都已经落地。
- 非 history 快捷键主线 `215D1`、`215F` 已完成。
- `215E1 history copy 主链收口` 已推进到实现阶段；当前已经落下 protocol session 冻结 snapshot、TUI 本地 reflow 和 resize 后不再回 core 重投影的主链，并补上四十四层关键语义：一是 primary screen ownership 判定，`newline` 现在只 seal，不再自动 committed；core-v2 会按 terminal 当前 rows 判断哪些 sealed line 已经滚出 primary screen、何时才变成 committable/committed；二是 `\r` / 覆写主链，history parser 不再吞掉孤立 `\r`，而是显式把它路由成“回到列 0 后覆写当前 mutable line”；三是 `CSI K` erase-in-line 主链，当前行擦除会进入 mutable frontier mutation，而不是只改 live surface，并且 `EL 0/1/2` 都已有 core/protocol 回归；四是 `ED 2/3` 的最小 destructive 语义，clear screen 现在会显式 reset 当前 frontier，clear scrollback 会显式 truncate committed history；五是 `ED 0/1` 现在也有最小 history 语义：只作用于当前 mutable frontier，`ED 0` 清 active cursor 之下的 mutable tail，`ED 1` 清 cursor 之上的 mutable prefix 和 active line 前缀，都不得误造或误删 committed history；六是 frozen snapshot 隔离已覆盖 `\r`、`EL 0/1/2`、`ED 0/1/3`，证明这些后续 live-tail 或 destructive 变化不会 retroactively 污染进入 copy mode 时已经冻结的历史快照；七是 protocol session 不再只为每个 terminal 保留一份 snapshot，而是按 frozen token 并存多份 pin，同一 terminal 连续拿到新 latest 后，旧 token 的 older 分页仍然可用，不会被后来的 latest 覆盖；八是 TUI 真实 protocol adapter 不再把每个 returned row 当成独立 logical line，而是会先按 stable `RowLineIDs` 合回 frozen `SourceLines`，再做本地 reflow，所以跨多 row 的同一 logical line 在真实 core -> protocol -> TUI 路径下也能按新 cols 正确重排；九是 protocol older stale guard 现在接受“客户端当前 frozen latest/prepend 视图”的 logical boundary，而不是错误地强制要求 boundary 永远等于单行尾部 latest，所以多行 latest boundary、prepend 后扩展的 first-boundary 以及本地 reflow 后继续 older 都不会再被误判成 stale；十是 protocol session 现在会把 frozen snapshot token 唯一化到会话级别，避免不同 terminal 在相同 history generation 上撞 token 并互相覆盖 pin，所以跨 terminal 并行 copy/history 时，older 仍然会命中各自的 frozen snapshot；十一是 copy/yank 文本现在按 logical line span 组装，同一 logical line 因本地 reflow 拆成多 row 时不会再错误插入换行，只有真正跨到下一条 logical line 时才保留 `\n`；十二是 protocol adapter 现在会把 authoritative `HistoryLineSpan` 的 `ClippedBefore/After` 一并带进 frozen `SourceLines`，本地 reflow 后这些 clipped 标记仍然保留，不会把被裁断的 logical-line partial 片段误当成完整 source line；十三是 TUI `HistoryStore.prepend` 现在会在 older boundary 处合并同一 logical line 的 clipped partial source，避免 prepend 后把同一条逻辑行的前后片段重复保存成两条 frozen source line；十四是 `ApplyWindow` 对 prepend 返回的 inserted row 数现在按本地 reflow 前后真实行数差计算，而不是盲用协议 older rows 数，所以遇到 boundary overlap 合并或本地重排后，copy viewport 不会再被错误顶动；十五是 `HistoryStore` 在只有 `Rows + Lines` 的 fallback/rebind 路径里，恢复 frozen `SourceLines` 时也会保留 authoritative clipped span 标记，所以本地 resize 重排不会再把 partial logical-line 片段误洗成完整 source line；十六是 copy mode search 现在已提升到 logical-line reflow 语义：同一 frozen logical line 被本地重排成多条 visual row 后，query 仍能跨 row 命中、导航到 match 起点，并在 renderer 中正确高亮跨行片段，而不再只限于单个 visual row 内搜索；十七是这条跨行 reflow search 现在也已经补到真实 protocol runtime 黑盒：`ProtocolCoreClientAdapter` 返回同一 logical line 的多 row authoritative window 时，copy mode 仍会在真实 core -> protocol -> TUI 路径下得到跨行 match，并把 cursor 放到正确起点，不再只在 fake core harness 里成立；十八是 restart/recovery 边界现在也有 protocol frozen snapshot 证据：terminal restart 之后，旧 frozen token 仍只服务 restart 前那份快照的 older 分页，而新的 latest snapshot 已经看到“live/history 都被重置后的空历史”，不会把 restart 当成凭空创造 committed history 的来源；十九是真实 VT alt-screen 切换现在已经接进 core history 主链：`CSI ?1049h/l` 会被 parser/ingest 路由成 `switch-alt-screen`，因此 alt-screen 期间输出不会进入 primary history，退出后新的 primary latest/protocol history window 也只会看到切屏前后的 primary 内容，而不会把 alt 内容混成 committed 或 mutable primary history；二十是 process exit 现在也有 protocol frozen snapshot 证据：terminal 退出时会 force-commit primary mutable frontier，旧 frozen token 继续只服务退出前那份快照的 older 分页，而新的 latest snapshot 会看到退出后已沉淀成 persisted history 的 tail，不会把 exit 后的边界变化 retroactively 污染旧 snapshot；二十一是 frozen snapshot latest 现在真正按本地 pane cols 接纳和重排：core 返回的 `window.Cols` 只表示 authoritative source 投影列宽，不再被 TUI 当成本地展示列宽硬绑定，因此同一条被 core 按窄 cols 投成多 row 的 logical line，进入 copy mode 后仍会先合回单条 frozen source，再按当前 pane 宽度本地 reflow，而不会被误认为是多条历史行；二十二是 older exhausted guard 现在也和 frozen 本地重排语义对齐：一旦同一 token/cursor/boundary 已经证明到顶，后续 pane resize 只会改变 TUI 本地 `Cols` 投影，不会清掉 exhausted marker，因此 copy mode 在本地 reflow 后仍保持 `↑ top`，也不会重复向 core 发送同一份 frozen snapshot 的 older 请求；二十三是 delayed older 响应现在也保持本地列宽绑定：如果 older 以旧 cols 发出、期间 pane 已本地 resize 并重排，响应回来后 `HistoryStore` 会继续按当前本地 cols 接纳 frozen source，`CopyModeStore` 也不会再被旧 `window.Cols` 拉回 source 投影宽度，因此 copy history 不会误退回 `history cols changed` pending 态；二十四是 pane/view 被重新 attach 或 reconnect 到新的 terminal 时，旧 frozen copy 绑定现在会被立即失效：`HistoryWindow` 会清空为“新 terminal 的 pending authoritative history”，`CopyModeStore` 也会丢掉旧 token/selection/query，避免同一个 pane 在 terminal 已切换后还继续渲染旧 terminal 的冻结历史；二十五是 kill/remove terminal 现在也会同步清掉当前 copy/history 绑定：如果被删除的 terminal 正在提供 frozen history，`HistoryStore` 会失效旧窗口、`CopyModeStore` 会直接退出，避免 pane 在 terminal 已被删掉后还继续显示旧快照；二十六是 pane/workbench close 或 detach 当前 copy pane 时，旧 frozen 历史现在也会立即失效：如果被关闭的是 active copy 所在 pane，`HistoryStore` 会清空旧 window、`CopyModeStore` 会退出，避免 pane 已经从 workspace 消失后屏幕上还留着它的 frozen history内容；二十七是 tab/workspace 切换以及其他会改变 active pane 的命令现在也会同步失效旧 copy 绑定：如果当前激活 pane 已不再是发起 copy 的 pane，`HistoryStore` 会清空旧 window、`CopyModeStore` 会退出，避免旧 frozen 历史被错误画进新的 active pane；二十八是 floating close 现在也会同步清掉绑定到该 floating view 的 copy/history：如果关闭的是当前承载 frozen history 的 floating，`HistoryStore` 会失效旧 window、`CopyModeStore` 会退出，避免 floating 已经关闭后旧历史还继续残留在 UI 状态里；二十九是 renderer 现在也补上了 copy-history view binding 兜底守卫：只有 `CopyModeStore` 的 pane/view 绑定仍然有效时才允许真正渲染 frozen history，否则只显示 pending/error，不再因为 state 漏失效而把旧历史错误画出来；三十是 Terminal Pool attach/reconnect 现在也会走和 live attach 一致的 copy-history rebind 失效逻辑：无论目标是 active pane 还是 floating view，只要 terminal 被重新绑定到新的 terminal id，旧 `HistoryWindow` 都会立即失效、`CopyModeStore` 也会转成新 terminal 的 pending 状态，避免 terminal pool attach/reconnect 后继续渲染旧 terminal 的冻结历史；三十一是 workbench storage load/reload 现在也会主动清理旧 frozen copy/history：外部 snapshot 一旦整体替换 `Shell/TerminalViews`，旧 `HistoryWindow` 和 `CopyModeStore` 会先失效，再应用新的 workbench snapshot 和 restore attach，避免 storage reload 之后屏幕还保留旧 terminal 的 frozen history，或者输入仍被旧 copy mode 吃掉；三十二是 TUI 本地 reflow 现在不再依赖 core 预先把 frozen source 拆成细 cell：无论 `SourceLine` 只有纯 `Text`，还是 protocol 返回了一个 width 大于当前 pane cols 的单个 styled cell，本地 reflow 都会先按 grapheme/display cell 拆开，再按当前 pane 宽度重排，所以 frozen logical-line snapshot 在真实 protocol 路径下也能独立完成宽窄列重投影；三十三是文档层现在把这条链的白话计划也写死了：冻结快照不是整份历史全量复制，而是“committed 历史共享 + frozen frontier 单独冻结 + 后续 live 修改按 line 做 copy-on-write”；TUI 只持有当前已加载的 logical-line 切片，本地 resize 只重排 rows，继续 older 时再带 `snapshot_token + boundary` 回 core 拉更早 logical lines；三十四是 floating deactivate 现在也补到了 runtime 黑盒：当 copy mode 绑定在某个 floating view 上时，只要该 floating 被取消激活，真实 runtime 帧就不再继续渲染它的 frozen history，`CopyModeStore` 也会同步退出，避免旧浮层历史在焦点已经回到 tiled pane 后继续残留在屏幕上；三十五是 restart 这类 non-history-boundary 现在也补到了 TUI runtime 黑盒：即使后来又来了一个“restart 后空历史”的 latest replace，它也不能把当前 frozen copy 里的旧 rows 顶掉，屏幕仍然继续显示进入 copy mode 时绑定的 token/source lines，直到用户主动退出 copy mode 或重新请求新的 latest snapshot；三十六是 core/protocol frozen snapshot 投影现在也和 authoritative `HistoryWindow` 对齐了 grapheme/display-cell 语义：当单个 measured styled cell 只有在当前列宽下放不下时，protocol 路径才会按 grapheme 拆分它，因此组合字符 `é` 会继续保持一个 display cell、宽字符 `好` 会继续保持两个 display cells，而普通可整行放下的 styled run 仍然保留原有 run/cell 结构，不会被协议层无谓打碎；三十七是 copy mode 明确退出后，后续迟到的 latest replace 既不会偷偷回填当前 active copy，也不会在下一次重新进入 copy mode 的 pending 阶段复用上一次残留的 frozen rows，UI 只允许显示“authoritative history pending”，不允许把旧 window 当成临时历史继续挂出来；三十八是 protocol frozen snapshot 现在也有了 resize 黑盒：同一 token 的 older 分页继续服务 resize 前 pin 住的冻结快照，不会被后续 grow/shrink 触发的新 latest frontier 重投影 retroactively 污染；只有重新请求新的 latest snapshot，才会看到 resize 后暴露出来的更大 live tail 或新的 frontier 投影；三十九是 copy mode 在 latest pending 阶段退出时，现在会同步清掉本地 `History.Pending`，因此同一个 requestID 的迟到 matching latest 也不能再在 copy mode 已关闭后回填 `HistoryStore` 或把 delayed rows 画回屏幕；四十是同一场景下的迟到 matching history 错误现在也会被按 stale 直接丢掉，不再把 `Surface.Err` / `Session.LastError` 或 copy 错误状态错误抬成用户可见 UI 错误；四十一是如果当前已经有更新的 pending request 在飞，被它取代的旧 request 再迟到返回 history 错误，也会被同一条 stale guard 静默丢掉，不会中断新的 pending latest/older，更不会把 superseded error 错误顶成当前 copy UI 的报错；四十二是同一类 superseded stale guard 现在也补到了 delayed success window：当旧 latest/older request 已经被新的 pending request 取代时，旧 request 迟到返回的 authoritative window 也会被按 stale 直接丢掉，既不会回填当前 `HistoryStore`，也不会覆盖新的 pending/latest 绑定，更不会把 superseded rows 渲染进当前 copy frame；四十三是 `workbench storage load/reload` 这类外部状态整体替换路径现在也补到了 runtime 黑盒：一旦旧 pane/view 结构和旧 copy binding 已被新 snapshot 替换，旧 pending history 的迟到 latest 或 error 都会继续按 stale 直接丢掉，既不会把旧 terminal 的 authoritative history 回填进新工作区，也不会把 delayed old-history rows / error 重新渲染回当前 frame；四十四是 attach/reconnect 作为 non-history-boundary 现在也补上了“同 terminal 不误伤 frozen snapshot”的黑盒：如果 pane/view 只是重新 attach 到同一个 terminal，而不是切换到新的 terminal id，当前 copy mode 的 frozen `HistoryWindow`、bound token 和本地 reflow rows 都会继续保留，不会被错误清空成 pending，也不会因为一次 same-terminal reattach 就把旧历史从屏幕上抹掉。
- `215E2 clipboard paste 主链` 和 `215E3 clipboard history overlay` 还没开始，等 `215E1` 收口后继续。
- 已知环境缺口：本机当前没有 `protoc` 与 `protoc-gen-go`；只有在需要重新生成 proto 时才构成阻塞。
