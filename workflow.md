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

## 1. 当前目标

### 1.1 一句话目标

把 `TerminalView/Attachment` 做成一等模型：同一个 terminal 可以同时被多个 pane 或 floating 连接，但 terminal process、live surface、authoritative history 仍然只有一份。

### 1.2 这轮只关心什么

这轮只收口一件事：**让 history / copy 主链尽快达到可用状态**。

这里的“可用”只看这一条主验收链：

1. 用户 attach 到一个真实 terminal。
2. 进入 copy mode。
3. 连续 `PageUp` 能拿到 older 历史。
4. pane resize 后，当前 frozen history 只做本地重排，不回 core 重新取一份 latest。
5. 搜索能在当前 frozen history 上命中。
6. 选中和复制得到的文本仍按 logical line 正确组装。

### 1.3 主验收链的通过标准

- 进入 copy mode 后，看到的是 authoritative history，不是 live fallback。
- `PageUp` 会继续拉 older，而不是只在本地假滚。
- resize 后，当前历史内容不丢、不跳回 pending、不被 live 内容替换。
- 搜索、cursor、selection 在 older prepend 和本地 reflow 后仍指向原来的内容。
- copy 出来的文本不因为 visual wrap / local reflow 被错误插入换行。

### 1.4 当前已经直接可用的能力

- core-v2 的 history truth 已经从 live surface 剥离，主线是 logical line。
- copy mode 已经走 frozen snapshot，不再跟着后续 live 输出漂移。
- latest / older 分页主链已经打通，older 继续带 token / boundary 回 core。
- TUI 已经能对 frozen logical lines 做本地 reflow，resize 不再默认回 core 重拿 latest。
- 搜索已经按 logical-line reflow 语义工作，不再只限于单个 visual row。
- copy / yank 已经按 logical line 组装，不再把同一条 reflow 后的 logical line 错拆成多行。
- boundary overlap / clipped partial / same-line merge 这些会直接影响 copy 正确性的基础语义已经补到主链。
- 大部分 stale response guard 已经补上，旧 latest/older 不会轻易覆盖当前 frozen history。

### 1.5 当前已满足的“可用”状态

- 已有统一的高层 runtime 主验收链，覆盖 `attach -> copy mode -> older -> resize -> search -> selection -> copy`。
- `215E1` 的退出标准已经收口：主验收链通过，且 core-v2 / protocol / tui-v3 的模块守卫测试通过。
- 当前 history / copy 主链不再以“继续补长尾黑盒”为默认目标；后续只在真实回归或新范围需要时再打开新切片。

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

### 4.2 tui-v3 只消费 authoritative history

- `termx-tui-v3` 不拥有 committed history truth。
- `HistoryStore` 只保存 core-v2 返回的 authoritative logical-line 历史快照、pending 状态和 exhausted 信息。
- `CopyModeStore` 只保存交互态：active view、terminal id、cursor、selection、frozen token 和当前本地投影 cols/rows。
- copy mode 缺冻结快照时，只能显示 pending、empty 或 error，不能从 live surface、snapshot 或本地 VTerm scrollback fallback。

### 4.3 resize 必须有 owner/follower 语义

- 同一 terminal 同时只能有一个有效 resize owner 修改 PTY size。
- follower 或 observer view 只能显示当前 terminal projection，不能因为自己 content rect 变化就覆盖 PTY size。
- owner transfer 必须走协议、effect、message 和 reducer 路径，不能在 UI 层本地偷偷改 truth。

### 4.4 render 和 runtime 的边界不能乱

- `RenderResult` 是唯一主输出；plain 文本、测试快照和 ANSI frame 都只是适配层。
- renderer 只消费 view-model 和 layout plan，不直接读 service、host 或 core client。
- `service` 不得直接修改 reducer-owned state；所有状态变化都必须走 message/effect 回主循环。
- `termx-tui-v3` 主线不得引入 Bubble Tea `Program`、`tea.Model`、`tea.Msg`、`bubbles` 等 contract。

### 4.5 实现纪律

- 先写 domain model 和最小 harness，再接真实 protocol、terminal 或 CLI。
- 大目标必须拆成可独立验证的小阶段，每个小阶段单独用 `SK:` 中文提交。
- 如果做着做着发现边界错了，先收口成文档/架构修正，再继续实现。
- 关键代码要加简短中文注释，说明真正不自明的逻辑。

## 5. 任务队列

状态只能使用：`待开始`、`进行中`、`完成`、`阻塞`。同一时间只能有一个切片处于 `进行中`。

自动执行时只看下面这张表，按顺序取最早未完成切片：

| 切片 | 状态 | 范围 | 白话说明 |
| --- | --- | --- | --- |
| 背景里程碑：0-215H3 | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`termx-cli/`、`internal/protocol/`、`termx-proto/`、相关文档 | 默认入口、runtime、styled render framework、TerminalView/Attachment 基线、resize ownership、history MVP H1-H3 都已经收口；更细的历史细节需要时去看 git 提交和架构文档 |
| 215D1. SK floating group commands | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/input/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`termx-tui-v3/docs/` | 已补齐 floating group commands |
| 215F. SK shortcut integration and tmux harness | 完成 | `termx-cli/`、`termx-tui-v3/`、`termx-core-v2/`、`internal/protocol/`、`Makefile` 按需 | 已补 runtime 黑盒证据与 tmux smoke |
| 215E1-A. SK history copy 可用性主链验收 | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`internal/protocol/`、相关文档 | 已补高层 runtime 黑盒，证明 attach -> copy mode -> older -> resize 本地重排 -> search -> selection -> copy 主链成立 |
| 215E1-B. SK history copy 主链缺口修补 | 完成 | 同上 | 已收掉 attach / resize / 本地 reflow 这类真实会卡主链可用性的缺口，不再继续扩到长尾黑盒 |
| 215E1-C. SK history copy 收口与回归 | 完成 | 同上 | 已用高层 runtime 黑盒和 core/protocol/tui-v3 联合模块测试证明主链可用，停止继续补长尾语义 |
| 215E2. SK clipboard paste 主链 | 完成 | `termx-tui-v3/input/`、`termx-tui-v3/app/`、`termx-tui-v3/services/`、`termx-cli/`、相关文档 | 已把显示态 `p/P PASTE` 接上真实主链：`p` paste 上一次 copy，`P` paste system clipboard，都会退出 copy mode 并发到 active terminal |
| 215E3. SK clipboard history overlay | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`termx-tui-v3/docs/` | 已补 clipboard history overlay：`H` 打开本地 clipboard history，支持 filter、paste、edit、delete，并复用 copy-mode paste 主链 |
| 215E1-R1. SK history copy 主链卡顿回归 | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/input/`、`termx-tui-v3/render/`、相关文档 | 已把 copy history latest/older 改成异步 effect；慢 history 请求下 `Ctrl-V` 会立刻进入 copy mode pending，`PageUp / wheel` 不再同步卡住 runtime 主循环 |
| 215E1-R2. SK history window 真分页性能回归 | 完成 | `termx-core-v2/history/`、`termx-core-v2/protocol_service.go`、`termx-core-v2/*test.go`、相关文档 | 已把 `history.window` latest/older 从全量投影后切页改成按页倒序收集；frozen snapshot 入口去掉重复 clone，协议 snapshot 投影也不再全量 reflow 后切页 |
| 215E1-R3. SK history older loading 性能回归 | 完成 | `termx-core-v2/protocol_service.go`、`termx-core-v2/*test.go`、`termx-tui-v3/app/`、`workflow.md` | 已处理真实现场的 `↑ loading`：当前 copy 会话自己的 stale older 响应会清 pending，frozen snapshot older 改成二分定位 cursor/boundary，并用 10 万行 benchmark 验证单页 older 是微秒级 |
| 215E1-R4. SK copy mode 上滑可见更新回归 | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/state/`、`workflow.md` | 已处理真实现场的“上滑 older 返回了但画面没动”：copy mode 上滑先滚已加载历史，拉到 older 后把视口放到新 prepend 的历史页 |
| 215E1-R5. SK 连续 older 不推进回归 | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`internal/protocol/`、`workflow.md` | 已修复真实现场“1000 行输出只能看到 953，再上滑 loading/more 切换但内容不变”：core older response 保持 frozen tail boundary，TUI 能连续接纳并 prepend/显示更老页 |
| 215E1-R6. SK live 压力输出最新帧合并 | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`termx-cli/cmd/termx/`、`workflow.md` | 已处理 `generate_terminal_stress.py --lines 100000` 下的 live pending：core live/history 分轨并按批 ingest，live surface 只维护 latest screen；TUI 合并普通 live changed，真实 TTY 写帧只保留最新待写帧；CLI 首次 attach 不再因为预填 session 被 stale guard 丢掉 |
| 215E1-R7. SK copy-top 翻到最老页回归 | 完成 | `termx-core-v2/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`termx-cli/`、`scripts/`、`workflow.md` | 已处理 copy-top 到不了最老页：copy mode 里按 `g` 会在当前 frozen snapshot 上直接请求 oldest page，不再靠重复 older 分页；100000 行 tmux baseline 的 copy-top / resize / reattach 都能看到 `000000`，copy history 正文也不再混入 search/status/line marker |
| 215E1-R8. SK copy history 按行滚动预加载 | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/state/`、`workflow.md` | 已把滚轮上滑改成按行移动；接近顶部会按页预取 older，但 older 返回只填本地缓存并保持当前内容锚点；真正到顶部继续上滑时，只按用户多滚的行数露出旧内容；`g` 直达最老页不变 |
| 215E1-R9. SK copy history 滚动跟手优化 | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/state/`、`termx-tui-v3/terminalhost/`、`workflow.md` | 已处理滚轮按行但不跟手：history 请求量和 older 预取阈值按当前 panel 尺寸动态计算，不再写死；FrameSink 能识别一行滚动并用终端 scroll region 只补新行，避免每次滚轮整块重写 |
| 215E1-R10. SK copy history 滚动 perf 定位 | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/render/`、`termx-tui-v3/terminalhost/`、`workflow.md` | 已做 copy history 连续滚动专项 benchmark/profile：主要瓶颈是 render 每帧大量分配，不是 FrameSink 写出；content viewport 满屏快路径减少临时 cell，真实 TTY 走 ANSI-only frame，测试仍保留完整 frame |
| 215E1-R11. SK copy history 滚动增量渲染 | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/render/`、`termx-tui-v3/terminalhost/`、`workflow.md` | 已把真实 TTY copy history 已加载区滚动改成增量 patch：一行/多行滚动不再重建整屏 frame，只用 scroll region 补新露出的行；latest-only sink 对 patch 保序，对完整帧仍保留 latest-only |
| 215E1-R12. SK copy history older 加载路径 perf | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/state/`、`termx-tui-v3/render/`、`workflow.md` | 已把 older 接收路径从全量重排/深拷贝改成只重排新 older 页，并让 older 返回后可见内容只移动少量行时继续走增量 patch；8192 loaded older 接收约 `0.89ms / 1.48MB / 831 allocs`，older result runtime patch 约 `1.11ms / 1.51MB / 910 allocs` |
| 215E1-R13. SK copy history latest 尾部定位回归 | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/state/`、`workflow.md` | 已修复进入 copy/history 后看不到最新日志：latest window 内部按旧到新排列，TUI 现在默认把 cursor/viewport 放到 latest 页尾部；oldest 跳转仍从最老页头部显示；scroll clamp 与实际 history 正文可见行数对齐 |
| 215E1-R14. SK copy history 增量滚动边框回归 | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/render/`、`termx-tui-v3/terminalhost/`、`workflow.md` | 已修复 pane 内滚动时宽度差一格、边框消失：copy history 内容不是全屏宽时不再用终端 scroll region 滚整行，改为只重写 pane 内容矩形；全宽内容仍保留 scroll region 快路径 |
| 215E1-R15. SK copy history 矩形 patch 列锚点回归 | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/render/`、`workflow.md` | 已修复历史滚动时行首前导 `0` 被遮挡、点击 pane 后恢复：矩形 patch 从 pane 内容区写出时，内部 ANSI 绝对列锚点会加上 content X 偏移 |
| 215E1-R16. SK copy history cell 宽度填充回归 | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/render/`、`termx-tui-v3/services/`、`workflow.md` | 已修复 `ls` 多列输出在 history 里挤成一坨：history cell 的 authoritative 宽度大于文本宽度时，展示/复制/搜索都会把右侧填充列当作空格；同时保留 protocol trailing blank cells |
| 215E1-R17. SK copy history latest 追平和空格端到端回归 | 完成 | `termx-core-v2/`、`termx-tui-v3/state/`、`termx-tui-v3/services/`、`termx-tui-v3/render/`、`workflow.md` | 已处理真实现场两个回归：进入 history/copy 前等待已入队的 async history 输出追平 live，避免 10 万行只冻结到 9 万多；`ls`/tabular 输出通过 tab 展开、protocol -> TUI 本地 reflow harness 证明空格不丢 |
| 215E1-R18. SK copy history 行编辑语义回归 | 完成 | `termx-core-v2/`、`workflow.md` | 已处理 latest tail 上的真实输出语义污染：core parser 现在把 `CSI C/D/G` 和 backspace 路由成 mutable frontier 光标 mutation；shell autosuggestion / 补全灰字被 erase 删除后不会继续作为最终 logical line 存进 history，光标右移超过行尾也会保留空白列 |
| 215E1-R19. SK copy history 行编辑性能回归审计 | 完成 | `termx-core-v2/`、`workflow.md` | 已检查并优化 R18 行编辑 parser / ingest 热路径：补 plain log batch 与 autosuggestion line-edit batch benchmark；发现普通日志曾从 R17 约 `8.58ms` 退化到 `13.3ms`，已通过去掉 append 后整行重复宽度扫描恢复到约 `8.65ms`；行编辑 batch 从优化前约 `19.7ms` 收回到约 `16.4ms` |
| 215E1-R20. SK copy history cursor scroll 回归 | 完成 | `termx-tui-v3/`、`workflow.md` | 已把 copy/history 滚轮、PageUp/PageDown、半页滚动改成 cursor-first：输入先移动 copy cursor，viewport 只负责保持 cursor 屏幕锚点；older 仍按页预取和填充，等待 older 时只消费未满足的 cursor 行移动；滚动 benchmark 通过，loaded line scroll 约 `68-69us / 55KB / 339 allocs`，older result patch 约 `1.22ms / 1.57MB / 1241 allocs` |
| 215E1-R21. SK copy history pane focus 保持回归 | 完成 | `termx-tui-v3/`、`workflow.md` | 已修复真实现场：copy/history 模式中鼠标点击其他 panel 只切换 active pane，不退出原 pane 的历史模式；关闭/删除绑定 pane、切 tab/workspace、floating deactivate 仍会清掉不可见 frozen history；已补 runtime 鼠标点击其他 pane 的回归测试 |
| 215E1-R22. SK copy history cursor wheel 与非 active pane 保持回归 | 完成 | `termx-tui-v3/`、`workflow.md` | 已修正 R20/R21 真实语义：滚轮先移动 copy cursor，cursor 留在可见区内时只复投光标、不重画内容；cursor 到可见区边缘后才推动 viewport；copy/history 按绑定 pane 展示，点击其他 panel 不退出，重新点回来能续上 |
| 215E1-R23. SK copy history selection 颜色回归 | 完成 | `termx-tui-v3/render/`、`termx-tui-v3/app/`、`workflow.md` | 已把 copy/history 被选中文本改成灰色字体、黄色背景；只改显示样式，不改 selection/copy 文本语义，搜索命中仍保留原 warning 高亮 |
| 215E1-R24. SK clipboard history core storage | 完成 | `termx-tui-v3/`、`termx-cli/`、`workflow.md` | 已把 `H` 打开的 copy list 从本地内存升级为 core-v2 daemon storage 托管：复制后写入 storage，打开弹窗时读取，编辑/删除后回写；UI 仍复用现有 clipboard history overlay |
| 215E1-R25. SK global frame presenter 防闪烁 | 完成 | `termx-tui-v3/terminalhost/`、`workflow.md` | 已给真实 TTY 输出补一层 V3 全局 frame presenter：同尺寸帧只按变化行绝对定位增量写，不再对每个变化行先清整行；首帧/resize 仍全量校准，降低 Ctrl-V 进入 copy mode 和远程 SSH 下的大面积闪烁 |
| 215E1-R26. SK copy mode 进入无 pending 闪屏 | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/state/`、`workflow.md` | 已把 Ctrl-V 的“发起 latest 请求”和“真正进入 copy/history”拆开：latest 没回来前只拦截输入、保留当前 live pane 画面，不再把内容区替换成 `copy history pending`；latest 回来后才激活 authoritative copy history，不使用 live/snapshot 当 history fallback |
| 215E1-R27. SK copy history 背景空白保真回归 | 完成 | `termx-tui-v3/render/`、`workflow.md` | 已修复 history/copy 内容裁剪时，authoritative cell 宽度里已有的带背景空白被补成无样式空格；裁剪落在 cell padding 区域时会继承原 terminal cell 的 ANSI 背景 |
| 215E1-R28. SK copy history 空文本背景 cell 保真回归 | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/render/`、`workflow.md` | 已修复 copy/history 本地重排和 canvas 写行时把 `Text="" Width>0 Style=BG` 的 terminal cell 当成空内容跳过；空 footprint 现在按带样式空格参与本地 reflow 和最终 ANSI frame |
| 215E1-R29. SK copy history 行尾背景误修回滚 | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`workflow.md` | 已撤回上一版基于 `CSI K` / BCE 在 core 里按 terminal cols 主动补满行尾背景空白的窄修复；保留 TUI 对已经存在的 styled blank cell 的展示保真，后续重新从 parser/live surface 背景语义定位根因 |

当前下一步：

- `215E1-R29 copy history 行尾背景误修回滚` 已完成
- 准入：`cd termx-core-v2 && go test ./... -count=1`、`cd termx-tui-v3 && go test ./... -count=1` 已通过

## 6. 必做证据

### 6.1 `215E1-A` 的唯一主验收链

必须证明这条链真实成立：

1. attach 到一个真实 terminal。
2. 进入 copy mode，看到 authoritative history pending -> latest。
3. 连续 `PageUp`，older 真正从 core 返回并 prepend。
4. resize 后，当前 frozen history 仍在，本地 reflow 生效，不回 core 重拿 latest。
5. 搜索能命中当前 frozen history。
6. 选中和复制得到的文本仍按 logical line 正确组装。

### 6.2 模块级最低守卫

在主验收链之外，当前阶段只保留这些最低守卫：

- core-v2：`\n`、`\r`、erase、auto-wrap、alt-screen、process exit、resize 的 history 语义不能回退。
- protocol：latest / older、token / generation / cursor / boundary stale guard 不能回退。
- tui-v3：copy mode 不得 fallback 到 live；本地 reflow、search、selection、copy 不能回退。

### 6.3 非阻塞长尾

下面这些不再作为当前阶段继续主动扩张的目标；除非它们直接阻塞主验收链，否则先停：

- same-terminal reconnect / terminal-pool reconnect / floating reconnect 的对称黑盒再细化
- delayed stale latest / older / error 的继续分叉覆盖
- workbench reload / delayed attach / lifecycle 组合路径的更多审计
- boundary overlap / clipped marker / grapheme 投影的再细化证明
- 更多“第 N 层关键语义”式的追加审计

### 6.4 `215E1-R1` 的唯一回归标准

- `Ctrl-V` 进入 copy mode 时，UI 主循环不得因为 latest history 请求而卡住。
- 第一个可见反馈必须先进入 `authoritative history window pending`，而不是长时间停在 live 内容或无响应。
- copy mode 下 `PageUp` / mouse wheel up 请求 older 时，也不得同步阻塞整个 runtime。
- 本切片只处理“history 请求必须异步”的真实交互回归，不顺手扩新语义。

### 6.5 `215E1-R2` 的唯一回归标准

- `history.window latest` 不能为了返回一页历史而投影全部 committed / frozen logical lines。
- `history.window older` 不能为了返回上一页而从头投影到 cursor。
- copy mode 进入时的 frozen snapshot 不能全量克隆整份历史；只能 pin 住本次会话的 logical boundary，并按页取 line payload。
- 行为上仍要保持 frozen token、older boundary、logical-line source、本地 reflow 和 copy 语义不变。

## 7. 测试准入

每个有效切片提交前，至少跑和改动范围相符的测试：

- `215E1-A`：
  - 1 组高层 runtime / protocol 黑盒，覆盖主验收链
  - 必要的模块守卫测试
- core-v2 改动：`cd termx-core-v2 && go test ./... -count=1`
- tui-v3 改动：`cd termx-tui-v3 && go test ./... -count=1`
- protocol 改动：`cd internal && go test ./protocol/... -count=1`；如果动到 `termx-proto/`，再跑 `cd termx-proto && go test ./... -count=1`
- CLI 改动：`cd termx-cli && go test ./... -count=1`
- 默认入口、跨模块或迁移相关改动：按需加跑 `make test-v2-migration`
- 默认入口相关改动：还要确认 `go run ./termx-cli/cmd/termx --help` 能编译运行
- 文档-only 改动：至少跑 `git diff --check`

## 8. 自动推进和提交规则

- 每次开始工作先读本文件，再跑 `git status --short --branch`。
- 只执行任务队列里最早未完成的切片。
- 如果最早未完成切片是 `阻塞`，必须停下说明原因，不能跳到后面。
- 如果最早未完成切片是 `待开始`，先把它标成 `进行中`，再开始做。
- 一个小阶段收口后，立刻更新本文件状态、跑准入、提交一个 `SK:` 中文提交。
- 不要长期堆未提交改动，也不要把多个小阶段硬塞进一个提交。
- 不得 amend commit，除非用户明确要求。
- 不得覆盖用户或其他代理的未提交改动；如果冲突，先停下说明。

## 9. 当前状态

- 默认本地入口已经切到 `termx-core-v2/` 和 `termx-tui-v3/`；旧本地路径只允许走 `termx legacy ...`。
- `AppRuntime` 已是事件驱动批处理循环；真实 CLI attach 不再有外层 `16ms` 轮询；resize latest-wins 和 owner 转移链路已经收口。
- `215H1`、`215H2`、`215H3` 已完成，live/history 边界、core-v2 authoritative history stale guard、tui-v3 active-view history binding 都已经落地。
- 非 history 快捷键主线 `215D1`、`215F` 已完成。
- `215E1` 代码主线已经具备：
  - frozen snapshot
  - latest / older 分页
  - TUI 本地 reflow
  - logical-line search / selection / copy
  - 关键 stale guard
- `215E1-A` 已补 runtime 高层验收：真实 attach、进入 copy mode、older prepend、resize 本地重排、search、selection、copy 已经能在同一条黑盒链里通过。
- `215E1-B` 已收掉一个真实 runtime 缺口：手工 `LiveResizeMsg` 现在会同步当前 owner view 的 desired size，不再被 attach correction 的 stale recovery 拉回旧 content rect。
- `215E1-B` 本轮还收回了一个 attach guard 回归，并把两条过时断言对齐到当前 frozen snapshot 语义：显式 `ViewID` 的首次 attach 不再被误丢；pane size 后 copy mode 继续本地 reflow 当前 frozen history，不再期待第二个 latest。
- `215E1-C` 已完成：当前已有高层 runtime 主验收链，且 `go test ./termx-core-v2/... ./internal/protocol/... ./termx-tui-v3/... -count=1` 已通过，说明从 core 到 protocol 到 tui-v3 的 history/copy 主链当前处于可用状态。
- `215E2` 已完成：display / copy mode 下的 `p/P` 已接上 paste 主链；`p` 走最近一次 copy buffer，`P` 走 system clipboard read，都会退出 copy mode 并把文本发到 active terminal；如果 live surface 开着 bracketed paste，会按 `\x1b[200~...\x1b[201~` 包装。
- `215E3` 已完成：copy mode 下的 `H` 会打开 reducer-owned clipboard history overlay，支持 filter、paste、edit、delete；paste 复用 active terminal 输入主链，并在 interactive runtime 下通过键盘和 content action 闭环验收。
- 已修复一条真实主链回归：copy mode 的 latest / older 现在走异步 effect；真实 protocol 一慢时，`Ctrl-V` 会立刻进入 `authoritative history window pending`，`PageUp`、mouse wheel up 不再同步卡住 runtime 主循环。
- 已修复一条真实性能回归：`history.window` 协议虽然有 `Limit`，但 core 内部之前仍会先全量冻结或全量投影历史，再切最后一页；现在 latest / older 会从目标位置按页倒序收集，copy mode frozen snapshot 也不再对整份 snapshot 全量 reflow 后切页。
- 已知环境缺口：本机当前没有 `protoc` 与 `protoc-gen-go`；只有在需要重新生成 proto 时才构成阻塞。
