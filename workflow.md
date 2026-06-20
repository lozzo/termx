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
- `termx-remote-v2/`
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
- attach、reattach、bootstrap、recovery、full replace、resize 都不能凭空创造 committed history；clear screen 只能把 core 已持有的 primary frontier 封口提交，不能从 live snapshot 反推历史。
- alt-screen 运行期间不持续写入 primary history；退出 alt-screen 时如果开启保留策略，最后一帧必须作为 authoritative history page 追加；process exit 必须 force commit primary mutable frontier。

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
| 215E1-R30. SK copy history 背景延伸行尾回归 | 完成 | `scripts/`、`termx-core-v2/`、`termx-tui-v3/`、`workflow.md` | 已补最小 ANSI 输出脚本和 tmux dump harness：普通 SGR 背景不会自然铺满行尾，`CSI K` erase-to-EOL 会把当前背景写到行尾；core history 现在只对 `CSI K` 记录 styled blank footprint，protocol/TUI/copy render 保持这些行尾背景 |
| 215E1-R31. SK copy history 真实链路语义 harness | 完成 | `scripts/`、`termx-core-v2/`、`termx-tui-v3/`、`workflow.md` | 已复用完整 tmux history smoke 增加受控 ANSI 语义场景，真实跑 daemon -> terminal -> attach -> live capture -> copy latest -> copy oldest；同时补 parser 语义单测，固定目前支持的行编辑/erase/背景/提交边界，不把 history parser 改成第二个 terminal emulator |
| 215E1-R32. SK copy history 背景现场取证 | 完成 | `scripts/`、`workflow.md` | 已补 `bg-forensics` 取证场景：抓 `generate_terminal_stress.py` 真实 raw 输入、live raw dump、copy/history tail raw dump，并解析背景色区间对比；1000/10000 行固定 seed 取证未复现 live/copy 背景丢失 |
| 215E1-R33. SK copy history live shell 背景复现 | 完成 | `scripts/`、`workflow.md` | 已把背景取证改成截图一致的真实链路：先 attach 到交互 shell，再在 live pane 里发送 stress 命令，抓 live/copy raw 对比；10000 行固定 seed 下未复现未选中状态的 live/copy 背景 cell 差异 |
| 215E1-R34. SK copy history selection 空白覆盖 | 完成 | `termx-tui-v3/render/`、`workflow.md` | 已修复截图里的 selection overlay：history 渲染投影不会再丢弃 `Text="" Width>0` 的空白 footprint；多行选区跨过 history row 时，黄色选区会覆盖文本后的空白 cell |
| 215E1-R35. SK copy history selection 行尾填充 | 完成 | `termx-tui-v3/render/`、`workflow.md` | 已撤回上一版 selection 行尾填充症状补丁；这类空白背景必须来自真实 history cell，不应该靠 copy selection overlay 临时补 |
| 215E1-R36. SK copy history 背景空格真实保真 | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`scripts/`、`workflow.md` | 已修复 core history projection/mutation 丢真实空白 footprint：`Text="" Width>0` 和 `Width > text width` 的带背景空格会物化成 history 空格 cell，再经 protocol/TUI/copy render 保真；不靠 selection overlay，也不把普通 SGR 背景凭空铺满整行 |
| 215E1-R37. SK copy history 真实 tmux raw 背景复现 | 完成 | `scripts/`、`workflow.md` | 已修正背景 harness：raw-preserve 重新抓当前 tmux pane 的 `capture-pane -epN` 并记录 capture mode；新增确定性 `bg-footprint` 场景，只输出“无背景尾部”和“真实背景空格尾部”两行，live/copy raw 都证明真实背景空格保真 |
| 215E1-R38. SK copy history shell 执行背景复现 | 完成 | `scripts/`、`termx-tui-v3/render/`、`workflow.md` | 已按真实手工路径改成双客户端 harness：direct PTY 里的 termx 客户端执行 stress，tmux 里的第二个 termx 客户端抓 live/copy raw；修复 history 渲染 wrapped continuation row 尾部背景空白丢失，报告收敛到 `reproduced=no` / `screen_diff_candidates=0` |
| 215E1-R39. SK copy history core 背景 footprint 回归 | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`scripts/`、`workflow.md` | 已撤掉 R38 的 TUI display-only 补丁；core history parser 现在只在 terminal 滚屏新建物理行继承背景时，把尾部空白作为 authoritative `Text="" Width=N Style=BG` footprint 写进 history；`bg-forensics` raw harness 收敛到 `screen_lost_bg_cells=0` |
| 215E1-R40. SK copy history 行尾背景语义修正 | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`internal/protocol/`、`termx-vterm/`、`workflow.md` | 已把滚屏继承背景改成 row tail fill metadata，不再写成 logical line 里的 N 个空格；copy/history 本地重排只在最后一个 visual row 展示行尾背景，真实空格/CSI K 仍保持 cell 宽度语义；live surface resize/reflow 会保留 used 宽度之后的行尾背景 |
| 215E1-R41. SK history screen ownership 回写语义 | 完成 | `termx-core-v2/`、`termx-core-v2/docs/architecture.md`、`termx-tui-v3/`、`workflow.md` | 已处理 Codex 这类 primary-screen UI 反复回写已有行的问题：core history 自己维护当前屏幕行到 logical line 的 ownership；光标上移/绝对定位回写时修改原 line，不把 `Working Working Working` 这类中间态追加成新历史；真实空白行作为 logical line 保留；copy frozen older 会先翻冻结屏幕里的上一行，不跳过 frozen live-tail |
| 215E1-R42. SK clear screen 保留当前屏幕页 | 完成 | `termx-core-v2/`、`termx-core-v2/docs/architecture.md`、`workflow.md` | 已处理 Codex / tmux 对比里的清屏问题：`CSI 2J` 不再直接删除当前 primary screen 上尚未 committed 的 logical lines，而是先把 HistoryTrack 已持有的 frontier 页面封口提交，再清空 screen ownership，让新全屏 UI 从新页面开始 |
| 215E1-R43. SK alt screen 进入前保留 primary 页 | 完成 | `termx-core-v2/`、`termx-core-v2/docs/architecture.md`、`workflow.md` | 已处理 Codex 进入 alt-screen 后只能看到 000058 的问题：进入 alt-screen 前先把 primary 当前 frontier 页封口提交；进入 alt-screen 后的清屏和绘制不再影响 primary history |
| 215E1-R44. SK alt screen 退出保留最后一帧 | 完成 | `termx-core-v2/live/`、`termx-core-v2/`、`termx-core-v2/docs/architecture.md`、`workflow.md` | 已处理全屏程序退出后看不到最后 UI 的问题：live surface 会保留退出前最后一帧；这个最后一帧只用于实时显示，不写入 primary history |
| 215E1-R45. SK alt screen 压力历史尾部回归 | 完成 | `termx-core-v2/`、`termx-core-v2/live/`、`termx-tui-v3/app/`、`workflow.md` | 已处理真实现场“压力日志后进入 alt-screen，看起来只能看到 000058 一类旧尾部”：补 100 行以上长行压力 harness，证明 authoritative history/copy latest 不丢 primary 尾部；同时把 live alt 退出策略改成先恢复 primary 再追加 alt 最后一帧，避免普通 live pane 被 alt 最后一帧直接盖住 |
| 215E1-R46. SK fullscreen home clear 保留 primary 页 | 完成 | `termx-core-v2/`、`workflow.md` | 已处理真实现场“100 行 stress 后进入 Codex/全屏，退出后 copy/history 只剩到 000058”：全屏入口常见 `CSI H` + `CSI J/0J` 现在按 page-break 处理，先提交 primary frontier，再让新 UI 从新页开始，不再删除屏幕上的 primary logical line |
| 215E1-R47. SK alt screen 退出最后一帧样式保真开关 | 完成 | `termx-core-v2/live/`、`termx-core-v2/docs/architecture.md`、`workflow.md` | 已处理真实现场“htop 退出后最后 UI 还在但颜色布局丢失”：live surface 退出 alt-screen 时按 styled cell replay 保留最后一帧，并提供默认开启、后续可迁移到配置的开关 |
| 215E1-R48. SK alt screen 退出最后一帧进入 history | 完成 | `termx-core-v2/`、`termx-core-v2/docs/architecture.md`、`workflow.md` | 已处理真实现场“alt-screen 最后一帧只在 live 里保留，history/copy 里看不到”：退出 alt-screen 时按同一保留开关把最后一帧追加成 authoritative history page |
| 215E1-R49. SK terminal view owner size 语义回归 | 完成 | `termx-tui-v3/`、`workflow.md` | 已修复 terminal view owner 已恢复但 resize 请求仍携带 follower policy 的错位；terminal size lock 展示与 view-local layout lock 已拆开，locked owner 仍显示 owner 但不发 resize |
| 215E1-R50. SK terminal size lock 交互迁移 | 完成 | `termx-tui-v3/`、`workflow.md` | 已把 tuiv2 的 terminal Size lock 搬到 tui-v3：`s LOCK` 通过 terminal service 写 `termx.size_lock` tags，成功后同步投影到同 terminal 的所有 view |
| 215E1-R51. SK terminal size lock chrome action | 完成 | `termx-tui-v3/render/`、`termx-tui-v3/app/`、`workflow.md` | 已在左上 terminal 名称后展示 size lock 图标；pane/floating 图标点击都会复用 terminal Size lock toggle 主链并锁定正确 terminal |
| 215E1-R52. SK terminal size lock 权限与 resize guard | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`workflow.md` | 已修复 Size lock 最高优先级语义：只有 owner 可点击切换，follow 只展示；terminal 锁定后 split/layout/owner 变化都不能重新发 PTY resize，除非主动解锁 |
| 215E1-R53. SK locked terminal attach resize guard | 完成 | `termx-tui-v3/app/`、`workflow.md` | 已覆盖并修复先 split 空 pane、再 attach 到同一 locked terminal 时，新 pane 或 owner attach result 误改 terminal size 的回归 |
| 215E1-R54. SK terminal size lock unlock sync | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/render/`、`termx-tui-v3/services/`、相关文档、`workflow.md` | 已修复 owner 解锁后按当前 owner panel 尺寸重新 resize、lock metadata 事件广播投影、锁图标点击解锁的交互闭环 |
| 215E1-R55. SK floating window 交互审计 | 完成 | `termx-tui-v3/docs/`、`termx-tui-v3/app/`、`termx-tui-v3/state/`、`termx-tui-v3/render/`、`workflow.md` | 已整理 floating window 在 attach/create、hit region、owner/size lock、resize、collapsed input、storage restore、workbench tree 等路径上的问题清单和建议修复顺序 |
| 215E1-R56. SK floating hit region panel identity | 完成 | `termx-tui-v3/render/`、`termx-tui-v3/app/`、`termx-tui-v3/state/`、`workflow.md` | 已将 floating hit region 收口为普通 panel id + floating 标记，runtime/app 在边界上按 panel 身份分发，只在下发 floating command 时解析内部 floating id |
| 215E1-R57. SK floating empty input and resize hit target | 完成 | `termx-tui-v3/input/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`workflow.md` | 已修复 active floating empty panel 键盘上下/回车仍打到 tiled pane 的问题，floating empty 内容会显示 reducer-owned CTA selection，并把 floating resize 命中区扩到右下角 3 格 |
| 215E1-R58. SK floating memory diagnostics | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/terminalhost/`、`termx-cli/cmd/termx/`、`workflow.md` | 已补 TUI runtime/latest frame sink 内存诊断、heap profile 文件落盘开关，以及 runtime/sink 队列引用清理回归测试；用于现场复现 floating 打开后的内存增长来源 |
| 215E1-R59. SK floating memory growth fix | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/render/`、`termx-tui-v3/terminalhost/`、`workflow.md` | 已基于 R58 现场日志/pprof 修复 floating 打开后的真实内存增长点：workbench storage conflict/load/reattach 风暴不再反复拉同一 terminal live surface |
| 215E1-R60. SK panel terminal binding truth refactor | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/render/`、`termx-tui-v3/state/`、相关文档、`workflow.md` | 已重构 panel 到 terminal 的连接 truth：pane/floating 的真实 terminal/channel/resize role 只来自 TerminalViewStore，Shell 裸 terminal id 只保留为展示/storage 兼容字段，不再污染输入、attach target、render 或 restore |
| 215E1-R61. SK tiled floating input focus regression | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/state/`、`workflow.md` | 该切片曾用 focus/mode 补丁处理输入错位；后续现场证明根因是 per-view attachment channel 失效，相关输入修复已在 R62 撤回 |
| 215E1-R62. SK panel input view channel refresh | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/state/`、`workflow.md` | 已撤回 R61 的 focus/mode 输入补丁；键盘输入按 active TerminalView binding 路由，channel 缺失或发送失败时只为该 view 重新 attach 并重放输入；split 继承 terminal 时不再复制 sibling channel |
| 215E1-R63. SK active panel input activation | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/state/`、`workflow.md` | 已修复 panel/floating 内容点击后键盘输入仍被 UI mode 吞掉的问题：点击 terminal 内容会激活对应 panel 并退出交互 mode，后续输入只按内存 active TerminalView binding 直达 terminal；restore snapshot 只保留连接意图，不复用旧 channel |
| 215E1-R64. SK active view input routing root cause | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/services/`、`termx-tui-v3/state/`、`workflow.md` | 已把普通 terminal 输入从 live reducer 隐式兜底重构为独立 `TerminalInputRouter`：UI/overlay/copy 先消费，未消费的 key 与 mouse passthrough 统一在同一处解析 active TerminalView binding、记录输入路由日志、发送或按当前 view 重新 attach，不依赖 storage/snapshot/global session fallback |
| 215E1-R65. SK acked terminal input protocol | 完成 | `internal/protocol/`、`termx-core-v2/`、`termx-tui-v3/services/`、`workflow.md` | 已把 tui-v3 active view 普通输入从无 ack 的 stream input 切到 request-response `input` 方法：daemon 按 terminal/channel/surface/view 校验后写入 process，失效 channel 不再静默丢弃，而是返回错误触发当前 view reattach/replay |
| 215E1-R66. SK exited terminal recovery panel | 完成 | `termx-core-v2/`、`internal/protocol/`、`termx-proto/`、`termx-tui-v3/`、`workflow.md` | 已给 tui-v3 panel 连接到已退出 terminal 的场景补完整业务：core/protocol 传递退出时间、退出码和命令，pane/floating 显示退出信息；restart 保留当前 view 绑定意图但清旧 channel，并逐 view 重新 attach |
| 215E1-R67. SK root restart and interactive exit CTA | 完成 | `termx-cli/`、`termx-tui-v3/render/`、`termx-tui-v3/app/`、`termx-tui-v3/state/`、`workflow.md` | 已修复默认 root TUI 退出后重进遇到固定 root terminal duplicate：固定 root 已退出时先 restart 再 attach；live exited 内容升级为居中的正式 exited CTA，键盘上下/Enter 与鼠标点击 restart/picker 都会进入真实 action 链路 |
| 215E1-R68. SK exited lifecycle after live output | 完成 | `termx-tui-v3/render/`、`workflow.md` | 已把 live exited 信息和 CTA 放到 terminal 内容尾部；内容超过视口时默认显示最后历史行和退出提示，并让鼠标 hit region 跟随尾部对齐后的实际行 |
| 215E1-R69. SK picker attach exited lifecycle | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/state/`、`workflow.md` | 已修复从退出态/picker 连接到另一个已退出 terminal 时被 attach result 临时清成 running：pool 明确为 exited 的 terminal 会保留 lifecycle 元数据并立即显示退出 CTA，不等用户再输入 |
| 215E1-R70. SK restart 保留 terminal 数据 | 完成 | `termx-core-v2/`、`termx-tui-v3/app/`、`termx-tui-v3/state/`、`workflow.md` | 已修复 restart 把同一 terminal 的 live surface 和 authoritative history 清空的问题：restart 只重启 process，保留 terminal identity/history/live tail，同时重置新进程不应继承的 parser、alt-screen、mouse/bracketed-paste 状态 |
| 215E1-R71. SK 退出 marker 进入 terminal 数据 | 完成 | `termx-core-v2/`、`termx-tui-v3/render/`、`workflow.md` | 已把 process exit marker 作为 core 生成的 terminal 系统输出写入 live surface 和 authoritative history，包含 terminal id、退出码、退出时间和命令；TUI render 只在 marker 已存在时补 restart/picker action，不再重复伪造退出信息 |
| 215E1-R72. SK restart 后 live cursor 投影修正 | 完成 | `termx-tui-v3/render/`、`termx-tui-v3/app/`、`workflow.md` | 已修复 restart 保留旧 live tail 时 TUI 从最后一行文本尾部合成可见 cursor 的问题：live terminal cursor 只信 core/protocol surface cursor；旧进程 cursor 被清掉后不会显示到 panel 最后一格，新进程首帧 cursor 回来后再展示 |
| 215E1-R73. SK restart 后 core live cursor 可见 | 完成 | `termx-core-v2/`、`termx-tui-v3/render/`、`workflow.md` | 已修正 restart 保留旧 live tail 后 core seed cursor 被置为 hidden 的问题：core 现在把 cursor 映射到保留 tail 后的 append row 并保持 visible；TUI render 继续只按 core/protocol surface cursor 坐标投影，不从文本尾部合成 |
| 215E1-R74. SK restart 后 lifecycle 恢复 | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/app/`、`workflow.md` | 已修复 restart ack 后旧 `exited` 展示态继续污染 reattach 的问题；当前 R83 已收紧为：pool/list 不再把 running/exited 写回 live/session，restart result 只来自 core ack，后续展示以 core live surface/event 为准 |
| 215E1-R75. SK core lifecycle 权威边界 | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/services/`、`termx-tui-v3/app/`、`termx-tui-v3/docs/`、`workflow.md` | 已把重进 TUI 后 lifecycle 判断收口到 core terminal lifecycle；当前 R83 已进一步明确：TUI 不把 lifecycle 权威性落进 snapshot/store，restart 入口必须按需查询 core，workbench storage 只保留连接意图 |
| 215E1-R76. SK restore 清理旧 exited pane | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/app/`、`workflow.md` | 已修复 R75 前旧 workbench storage 中残留 `PaneExited` 导致重进 TUI 仍显示 restart 的问题：restore 入口也会把 `PaneExited`/copy-history scrub 成 terminal-live 连接意图，再由 core running/exited lifecycle 决定最终 UI；同时补 lifecycle trace 日志定位 restore/list/surface/restart |
| 215E1-R77. SK panel lifecycle 边界收口 | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/render/`、`termx-tui-v3/app/`、`termx-tui-v3/docs/`、`AGENTS.md`、`workflow.md` | 已把 pane kind 收窄为 empty/terminal-live：`exited` 和 `copy-history` 不再是当前 panel 状态，只能作为旧 snapshot 字符串在 restore 边界迁移；render exited 只从 TerminalView 绑定 terminal 的 lifecycle 投影，旧 snapshot 缺 TerminalViews 时会从连接意图补 detached binding；AGENTS 已强调禁止症状补丁 |
| 215E1-R78. SK live lifecycle 队列边界 | 完成 | `termx-tui-v3/app/`、`workflow.md` | 已修复真正现场断点：`LifecycleKnown=true` 的 live surface/event 消息是 core terminal lifecycle 边界，不能被 runtime 的 ordinary live frame coalesce 丢掉；当前 R83 已把该标记移到消息/result/event 层，不写入 TUI snapshot/store |
| 215E1-R79. SK terminal lifecycle 链路文档 | 完成 | `termx-tui-v3/docs/`、`workflow.md` | 已新增 `terminal-lifecycle-debug-chain.md`，按 workbench restore、TerminalView binding、terminal pool list、live surface/session、runtime queue、render exited CTA、restart action 分段记录关键代码、状态 owner、不变量和排查顺序 |
| 215E1-R80. SK TUI/core 状态归属文档 | 完成 | `termx-tui-v3/docs/`、`workflow.md` | 已新增 `state-ownership-map.md`，用 JSON 总图和 owner 表梳理 core terminal、attachment、history、live、storage，以及 TUI shell、TerminalView、surface/session、history/copy、pool、runtime/host/render 缓存的状态归属和禁止跨边界使用规则 |
| 215E1-R81. SK 状态归属文档中文化 | 完成 | `termx-tui-v3/docs/`、`workflow.md` | 已把 `state-ownership-map.md` 的标题、章节、JSON key、表头和语义说明改成中文；代码结构名、字段名和文件路径保留原名，方便按文档回查代码 |
| 215E1-R82. SK resize owner 接任主动校验 | 完成 | `termx-core-v2/`、`termx-tui-v3/state/`、`termx-tui-v3/app/`、`workflow.md` | 已修复 owner 主动/被动转移后不立即 resize 的回归：TerminalView owner 变化会标记一次 pending owner resize，layout reducer 立即发 view-scoped `ensure_resize`；core-v2 `ensure_resize` 在尺寸相同情况下只刷新 ownership/control，不实际 resize PTY |
| 215E1-R83. SK terminal lifecycle reentry authority | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`workflow.md` | 已收口重进 TUI 后 restart CTA 误判：TUI 不把 terminal pool/list 的 running/exited 写入 pane live 状态，restart 入口先查询 core terminal 当前状态；lifecycle-known 只作为消息/result/event 的一次性边界，不落进 TUI snapshot/store |
| 215E1-R84. SK bound terminal lifecycle restore query | 完成 | `termx-tui-v3/app/`、`workflow.md` | 已处理重进/restore 后已绑定 panel 没有主动查询 core lifecycle 的回归：workbench restore 完成后对 preserve 的 already-live TerminalView binding 发起一次 core surface/lifecycle 查询，不在 TUI 缓存 terminal running/exited |
| 215E1-R85. SK root attach no auto restart | 完成 | `termx-cli/`、`termx-tui-v3/app/`、`workflow.md` | 已处理重进 TUI 时全屏程序被自动 restart/HUP 的回归：root/attach 入口不再携带自动 restart 意图，只 attach/query core lifecycle；用户明确按 R 时才 restart |
| 215E1-R86. SK restart lifecycle truth guard | 完成 | `termx-core-v2/`、`termx-cli/`、`termx-tui-v3/app/`、`workflow.md` | 已修复 restart 后重进 TUI 仍显示 exited 的根因：core restart 新进程不能绑定到本次 protocol request/session ctx；TUI 关闭 socket 不再 cancel 刚重启出的 PTY |
| 215E1-R87. SK copy history 滚动 perf 和下滑回归 | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/state/`、`workflow.md` | 已修复新 history/copy 模式 raw mouse wheel down 被 runtime 吞掉的问题；同时给 text-only ASCII logical line reflow 加 fast path，常见日志 older prepend 从约 `0.9ms / 1.6MB / 828 allocs` 降到约 `0.09ms / 1.4MB / 135 allocs` |
| 215E1-R88. SK copy latest 不阻塞 sibling view 输入 | 完成 | `termx-core-v2/`、`termx-tui-v3/app/`、`workflow.md` | 已处理真实现场：protocol session request 并发处理，copy latest 的 history barrier 不再挡住同一 client 后续 input ack；copy-mode 输入拦截按 active TerminalView 归属判断，绑定旧 pane 的 copy/history 不再吞掉 sibling active pane 输入 |
| 215E1-R89. SK copy 入口即时反馈与 buffer 内存收敛 | 完成 | `termx-core-v2/`、`termx-tui-v3/app/`、`termx-tui-v3/terminalhost/`、`workflow.md` | 已处理真实现场：高压 live 输出期间鼠标上滑或 Ctrl-V 先进入 view-scoped copy entering 可见态，不等历史 latest；runtime 输入优先于普通 live 帧；core/TUI/FrameSink 消费 buffer 后清尾释放 payload 引用；protocol frozen pin 限量保留并避免 pinned latest 全量 payload 重拷贝 |
| 215E1-R90. SK copy 无 pending 闪屏与轻量 frozen pin | 完成 | `termx-core-v2/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`workflow.md` | 已处理真实现场：进入 copy mode 时 latest 异步期间只拦截输入并保持 live 画面，不显示 pending 文本或闪屏；退出/取消 copy 释放本地 history window；core frozen token 只保留 committed 边界和必要 frontier，按页从 store 加载，不 pin 整份历史 lines |
| 215E1-R91. SK copy entering 冻结 live 反馈 | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`workflow.md` | 已处理真实现场：连续 live 输出时按 Ctrl-V 后立即冻结当前 view 的可见 live 画面并拦截输入，不显示 pending 文本，也不继续被后续 live 帧滚动覆盖；latest 回来后再切 authoritative copy history |
| 215E1-R92. SK copy entering 滚动意图接续 | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`workflow.md` | 已处理真实现场：copy entering 冻结 live 后，滚轮/PageUp/PageDown 会累计成 copy cursor 滚动意图；latest 回来后立即从尾部应用，超出 latest 页的上滚会继续发 older 并带上未消费行数，不再等一会儿跳到非预期位置 |
| 215E1-R93. SK history observer 延迟删除和版本保留 | 完成 | `termx-core-v2/history/`、`termx-core-v2/protocol_service.go`、`internal/protocol/`、`termx-tui-v3/services/`、`termx-tui-v3/app/`、`workflow.md` | 已按 observer epoch 收口 copy/history 冻结语义：进入历史模式不拷贝整份历史；删除先按 epoch 标记，新进入的 observer 不再看到，已进入的 observer 仍按进入时刻可见；修改已被旧 observer 观察的 line 时写时保留旧版本，TUI 退出 copy 时主动释放 frozen token，所有 observer 离开后再物理清理 |
| 215E1-R94. SK copy latest 冻结边界回归 | 完成 | `termx-core-v2/`、`termx-tui-v3/app/`、`workflow.md` | 已处理真实现场：进入 copy mode 后画面能立刻停住，但继续滚动要等 latest 追平；冻结窗口还会包含用户进入 copy 之后的未来日志。latest 现在不等待 history ingest backlog，TUI 会把进入时观察到的 history generation 带给 core，core 按该边界冻结 authoritative history，不把进入后的 future line 混进旧 copy 会话 |
| 215E1-R95. SK live snapshot generation 非阻塞 | 完成 | `termx-core-v2/`、`workflow.md` | 已处理真实现场：R94 为了给 copy latest 带 history generation，让 live snapshot 读取 history pipeline 锁，导致高压输出下 live surface 刷新被 history ingest 节奏拖慢。history pipeline 现在维护原子 last-completed generation；live snapshot 只读这个非阻塞投影，不再等待 history parser |
| 215E1-R96. SK sticky shortcut mode 超时退出 | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/app/`、`workflow.md` | 已对齐 tuiv2：pane/resize/tab/workspace/floating/global 这类前缀快捷键 mode 在 3 秒无输入后自动退出，并在有效快捷动作后续期；terminal picker、terminal pool、prompt、help、clipboard history、copy/history 等真实 overlay 或交互页面不自动退出 |
| 215E1-R97. SK clipboard history 手工新增 | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`workflow.md` | 已复刻 tuiv2 clipboard history 的 New entry 能力：v3 overlay 提供新建入口，提交后写入 reducer-owned clipboard store，并通过 core-v2 daemon storage 持久化 |
| 215E1-R98. SK clipboard history 快捷入口 | 完成 | `termx-tui-v3/input/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`workflow.md` | 已修复 `Ctrl-V` 后 `H` 入口：copy entering 阶段也能打开 v3 clipboard history overlay，copy footer 明确展示 `H CLIPBOARD` |
| 215E1-R99. SK clipboard history modal 线框与模糊搜索 | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/render/`、`workflow.md` | 已按确认稿重画 clipboard history：标题嵌入 v3 细边框，内部只保留搜索区和左窄右宽 T 字分栏；footer 展示全局快捷键；搜索复用 picker 子序列匹配并高亮命中字母 |
| 215E1-R100. SK copy selection 显示与最近粘贴回归 | 完成 | `termx-tui-v3/render/`、`termx-tui-v3/app/`、`workflow.md` | 已修复 copy/history 选区行尾空白不染色的问题：选区显示会像 tmux 一样把选中 visual row 的 display-only 空白补成黄底，但不进入复制文本；`p` 粘贴最近复制时若 clipboard service 缓存为空，会回退到 reducer-owned clipboard history 最新项 |
| 215E1-R101. SK clipboard history viewport 预览回归 | 完成 | `termx-tui-v3/render/`、`workflow.md` | 已处理 clipboard history modal 仍按旧固定宽度展示、右侧预览过窄的问题：弹窗尺寸来自外部 terminal viewport，右侧展示选中项多行正文预览 |
| 215E1-R102. SK clipboard history 列宽样式回归 | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`workflow.md` | 已处理 clipboard history modal 细节：预览正文用前景色，搜索命中保持黄色，搜索标签改成 `Search:`，弹窗高度收窄，名称列默认加宽并支持鼠标拖拽分隔符调整 |
| 215E1-R103. SK workbench navigator 快照复刻 | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/render/`、`workflow.md` | 已把 v3 workbench tree 改成 tuiv2 风格 Workbench Navigator：左侧展示 workspace/tab/pane 树和状态，右侧复用 v3 panel/live renderer 展示选中 pane 的实时 snapshot，并默认选中当前 active pane |
| 215E1-R104. SK workbench navigator 真实 dump 校准 | 完成 | `termx-tui-v3/render/`、`termx-tui-v3/`、`termx-cli/`、`workflow.md` | 已按子代理审核和 tmux `capture-pane -epN` 真实 dump 校准 Workbench Navigator：搜索光标回到 search 行，内部布局改按弹窗 content rect 分配，右侧 snapshot 放大并展示 live 内容，action 改到底部语义行；CLI 支持 `visual-snapshot --case workbench-tree-page` 直接复抓 |
| 215E1-R105. SK workbench navigator tree 交互回归 | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`workflow.md` | 已修复 Workbench Navigator 搜索删除、floating 节点展示、tab 多 pane snapshot 预览和 tree 图标/文本/状态着色 |
| 215E1-R106. SK workbench navigator 布局微调 | 完成 | `termx-tui-v3/render/`、`workflow.md` | 已把 Workbench Navigator 左侧 tree 列在可用空间内加宽约 10 字符，并在搜索行和内容区之间补横线分隔 |
| 215E1-R107. SK workbench navigator T 框贴边回归 | 完成 | `termx-tui-v3/render/`、`workflow.md` | 已把 Workbench Navigator 内部分割框改成和外框合并的 T 字框，去掉 search 上方空白，并让 tab 多 pane 预览 frame 标题使用连接的 terminal 名称 |
| 215E1-R108. SK workbench navigator tab terminal 展示 | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/render/`、`workflow.md` | 已把 Workbench Navigator tab 下的 terminal-live 子节点显示为连接的 terminal 名称；搜索同时保留 pane 原标题匹配；选中 tab 时右侧只预览该 tab 下已连接的 terminal |
| 215E1-R109. SK floating tab-scoped model 重构 | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/render/`、`termx-tui-v3/app/`、`termx-tui-v3/services/`、`workflow.md` | 已把 floating window 从 Shell 全局状态重构为 tab 子集；不做旧全局 floating storage 迁移兼容，Workbench Navigator 按 tab 子节点展示和预览 floating terminal |
| 215E1-R110. SK workbench navigator workspace 操作回归 | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/render/`、`termx-tui-v3/app/`、`workflow.md` | 已修正 Workbench Navigator：展示所有 workspace，选中节点名称带下划线，底部/快捷入口支持创建 workspace，floating 子节点按 terminal 图标和状态颜色展示 |
| 215E1-R111. SK workspace tab CRUD 快捷键闭环 | 完成 | `termx-tui-v3/input/`、`termx-tui-v3/state/`、`termx-tui-v3/app/`、`termx-tui-v3/render/`、`workflow.md` | 已补齐 workspace/tab CRUD 快捷键闭环：footer 键位和 input binding 对齐，Workbench Navigator 对非当前 workspace 的 tab/pane/floating 目标路由正确 |
| 215E1-R112. SK workspace 空槽位语义 | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/render/`、`workflow.md` | 已让新建 workspace 默认创建 main tab 和全屏 empty pane，不自动创建 terminal；无 tab workspace 只展示居中快捷键提示 |
| 215E1-R113. SK 顶部栏鼠标交互 | 完成 | `termx-tui-v3/render/`、`termx-tui-v3/app/`、`workflow.md` | 已让顶部 workspace 名称点击打开 Workbench Navigator；tab/close/create 点击区域保持可用，加号改成带间距的 Nerd Font 图标 |
| 215E1-R114. SK root 空终端启动 picker | 完成 | `termx-cli/`、`termx-tui-v3/app/`、`workflow.md` | 已让 root 入口无 core terminal 时不再自动创建 main terminal，直接启动空 workbench 并打开 terminal picker；Esc 后保留 unconnected panel，并跳过启动时旧 workbench storage load |
| 215E1-R115. SK 空 tab 顶栏光标残留回归 | 完成 | `termx-tui-v3/render/`、`workflow.md` | 已修复空 tab 提示页暴露可见/隐藏 cursor，避免顶栏附近出现多余竖线 |
| 215E1-R116. SK tab create unconnected pane 与空 workspace header | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/render/`、`workflow.md` | 已对齐 tuiv2 新 tab 创建 unconnected pane 的结构，并修复无 tab 时 header 仍伪造 main tab |
| 215E1-R117. SK TUI 配置管理文档 | 完成 | `termx-tui-v3/docs/`、`workflow.md` | 已梳理 v3 TUI 配置管理边界、主题 token、配置项和加载优先级，先落文档再进入实现 |
| 215E1-R118. SK TUI 标准配置样例 | 完成 | `termx-tui-v3/docs/`、`workflow.md` | 已补充 v3 标准配置样例，每个配置项都写中文注释、默认含义和可选示例 |
| 215E1-R119. SK TUI 配置代码适配 | 完成 | `termx-tui-v3/config/`、`termx-tui-v3/render/`、`termx-cli/`、`workflow.md` | 已接入 v3 独立配置模型、标准模板解析、env 覆盖、theme resolver 和 CLI 启动读取，并补最小单测 |
| 215E1-R120. SK daemon/TUI/history 内存 profile 优化 | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`termx-vterm/`、`workflow.md` | 已基于 pprof 收掉 daemon live/history surface 的固定常驻内存：vterm ANSI parser data buffer 从每个 surface 4MB 降到 64KB；单个 history pipeline inuse profile 从约 7.0MB 降到约 3.0MB，core ingest alloc/op 从约 30.7MB 降到约 26.6MB，copy/history 滚动 CPU benchmark 未回退 |
| 215E1-R121. SK daemon/TUI/history 真实 RSS 测量 | 完成 | `termx-cli/`、`termx-core-v2/`、`termx-tui-v3/`、`termx-shared/`、`scripts/`、`workflow.md` | 已补真实 daemon/attach/copy RSS smoke 与 daemon/TUI heap profile；收掉 transport zstd 固定大窗口、history sealed clean line 内存形态和 commit 热路径 clone。1000 行 daemon 保持 50MB 内；30000 行 daemon RSS 仍约 124MB，heap 约 37-46MB，证据指向 live/vterm 写入临时分配造成 Go RSS 高水位，需要后续单独优化 |
| 215E1-R122. SK live/vterm 写入 RSS 高水位优化 | 完成 | `termx-core-v2/`、`termx-vterm/`、`workflow.md` | 已把 vterm fast SGR 从整屏 clone 改成原地滚动复用行，只复制离屏 scrollback；history pipeline 内部 owned cells 去掉 parser batch clone 和 store/backend 二次 clone。30k stress daemon RSS 从约 124MB 降到约 110MB，TUI copy-oldest 约 50.8MB；单个 daemon heap profile 约 41MB，剩余 daemon RSS 主要是 Go allocator 高水位，继续压到 50MB 需要 runtime/memory-limit 或架构性落盘/分段策略，不能靠 scrub/fallback 补丁完成 |
| 215E1-R123. SK daemon RSS runtime limit 与 history churn 继续压榨 | 完成 | `termx-core-v2/`、`termx-vterm/`、`termx-cli/`、`scripts/`、`workflow.md` | 已加显式 `TERMX_DAEMON_MEMORY_LIMIT_MB` daemon GC pacing 开关，并继续收掉 history owned append/内部 replace clone、core CSI 参数 parse 分配、vterm fast SGR 参数 scratch 与 scrollback damage 容器扩容。30k smoke 在 48MB limit 下 daemon RSS 从 R122 约 110MB 降到约 79.7MB，TUI copy-oldest 约 49.7MB；40MB/56MB limit 不更优。history ingest bench：plain 约 `4.64MB / 20902 allocs`，line-edit 约 `30.42MB / 33425 allocs`；剩余 daemon RSS 仍受 Go allocator 高水位与真实 compact history payload 约 36-39MB 约束，未用 scrub/fallback 降 truth |
| 215E1-R124. SK 真实 TUI 100k stress 双侧内存优化 | 完成 | `termx-core-v2/`、`termx-tui-v3/`、`termx-vterm/`、`termx-cli/`、`scripts/`、`workflow.md` | 已补真实 TUI pane 执行 100000 行 stress 的双侧 RSS/CPU/heap harness，并接入 TUI 显式 memory limit；core compact history backend 改成顺序 ID dense 存储、精确编码容量，parser 批次应用后清临时引用。最终 100k 真实链路：daemon copy-oldest 约 117.7MB、TUI copy-oldest 约 84.0MB、脚本 `real 4.69 user 2.58 sys 0.31`，copy oldest 可见 `000000`；剩余 daemon heap 主要是真实 compact history payload 与 dense slot，继续大幅下降需要分段/压缩/落盘 history backend 级策略，不得靠丢 truth 或 scrub |
| 215E1-R125. SK 100k history payload 与 TUI RSS 继续压榨 | 完成 | `termx-core-v2/`、`scripts/`、`workflow.md` | 已验证 compact line header/slot 这类存储结构微调只能带来约 1-3MB 级收益，不能支撑 50% 量级改善；负收益 arena 分段方案已回退。100k 真实链路当前 daemon copy-oldest 约 116.6MB、TUI copy-oldest 约 84.2MB、脚本 `real 4.84 user 2.60 sys 0.32`，oldest 页可见 `000000`；全局 pprof 产物在 `/tmp/termx-r125-global-profile-current-100k/analysis`，指向 daemon GB 级 `cloneCells` churn 和 heap 内真实 history payload 才是下一步主线 |
| 215E1-R126. SK daemon/TUI stress harness 与量级优化入口 | 完成 | `termx-tui-v3/`、`scripts/`、`workflow.md` | 已把真实 100k harness 校准成默认干净采样：RSS/CPU 采样不再每点触发 heap profile，最终再抓 daemon/TUI heap；产出 baseline time、stress time、pprof top 和 DOT 热点图源，Graphviz 缺失时记录 SVG 失败原因。TUI live snapshot 有 screen cells 时不再同时保留重复 Lines；100k clean 当前 daemon copy-oldest 约 117.3MB、TUI copy-oldest 约 79.6MB、脚本 `real 4.21 user 2.43 sys 0.31`、oldest 可见 `000000`。profile 指向 daemon alloc 约 7.0GB，其中 `history.cloneCells` 约 4.0GB、`MemoryStorageBackend.LoadLine` 约 3.5GB，是下一步量级优化入口 |
| 215E1-R127. SK daemon history LoadLine clone churn 优化 | 完成 | `termx-core-v2/`、`workflow.md` | 已把 history store 内部可写/只读判定路径从 public `Line()` 防御性 clone 切到锁内 owned read、专用写入和 commit-state helper；public `Line()` 仍返回 detached copy，observer/frozen snapshot 继续 COW 保留旧版本。ingest benchmark：plain 从约 `4.64MB / 20902 allocs / 3.0ms` 降到约 `3.34MB / 7399 allocs / 2.7ms`，line-edit 从约 `30.42MB / 33425 allocs / 6.7ms` 降到约 `9.23MB / 18356 allocs / 4.4ms`。100k clean 当前 daemon copy-oldest 约 116.5MB、daemon 累计 CPU 约 23.3s、脚本 `real 3.61 user 2.32 sys 0.29`，oldest 可见 `000000`；R126 的 GB 级 `LoadLine/LogicalLine.Clone/cloneCells` 主热区已退下 |
| 215E1-R128. SK daemon compact clean/reencode 与 live/vterm alloc 热点 | 完成 | `termx-core-v2/`、`termx-vterm/`、`workflow.md` | 已把 compact clean 存储改成 dense 直写与 run/color 编码，并让 history pipeline 只在 alt-screen 相关输出时维护 alt capture，普通 primary stress 不再并行跑第二套 live/vterm。100k final artifact 在 `/tmp/termx-r128-final2-100k`：daemon copy-oldest 约 121.6MB、TUI copy-oldest 约 120.0MB、daemon CPU 约 6.2s、脚本 `real 2.57 user 2.27 sys 0.23`、oldest 可见 `000000`。daemon alloc_space 从 R127 约 3.1GB 降到约 2.1GB，但 RSS/TUI 高水位未达到 50%/100% 量级，profile 指向下一步应转向 TUI live snapshot clone、protocol row decode 和 render/canvas alloc churn |
| 215E1-R129. SK stress 脚本双侧 RSS/CPU 真实基准与 TUI/daemon 联合优化 | 完成 | `scripts/`、`termx-tui-v3/`、`workflow.md` | 已按同一份 100k 真实链路 pprof/hotspot 收口 TUI live refresh 压力：protocol ordinary changed 只发 refresh invalidation，不再在 service 层提前拉 snapshot；runtime 按事件循环 coalescing 和 in-flight dirty 背压只保留最新 surface 请求，不做固定帧率限制；普通 refresh 不触发 render 和 layout resize 测量。100k final artifact 在 `/tmp/termx-r129-refresh-layout-backpressure-100k`：daemon copy-oldest 约 121.5MB、TUI copy-oldest 约 48.1MB、脚本 `real 2.54 user 2.24 sys 0.22`、oldest 可见 `000000`；TUI `alloc_space` 从 R128 约 1.9GB 降到约 1.34GB，`terminalLiveLineFromCells` 从约 984MB 降到约 109MB。daemon inuse 仍主要是真实 compact history payload，下一步若要继续 50% 量级下降应做 history backend 分段/落盘/压缩设计，而不是继续抠 header/slot 几 MB |
| 215E1-R130. SK 100k daemon RSS 回收与 backend 策略基准 | 完成 | `scripts/`、`termx-core-v2/`、`termx-cli/`、`termx-tui-v3/`、`workflow.md` | 已用 runtime memstats/heap/RSS 证明 daemon 100k RSS 大头是真实 compact history payload 常驻 heap，而不是固定帧率或 TUI 帧处理；新增显式 file-backed compact history backend，clean sealed logical line payload 落到文件，Go heap 只留 offset/length，CommittedHistoryIndex 顺序场景不再保留重复 map。100k final artifact `/tmp/termx-r130-filebackend-indexslot-100k`：daemon copy-oldest 46.6MB、TUI copy-oldest 49.5MB、stress `real 2.60 user 2.23 sys 0.24`、outside baseline `real 1.33 user 1.30 sys 0.02`、oldest 可见 `000000`；backend 文件约 31MB，daemon forced heap inuse 约 14.6MB。32MB daemon memory limit 组合只小降到 45.4MB 且不是主收益，默认不靠 GC limit 硬压 |
| 215E1-R131. SK 100k 默认 daemon history backend 收口 | 完成 | `scripts/`、`termx-core-v2/`、`termx-cli/`、`workflow.md` | 已把默认本地 daemon history backend 切到 file-backed compact storage：默认落在 `$XDG_STATE_HOME/termx/core-v2-history` 或用户 state dir，`TERMX_DAEMON_HISTORY_FILE_BACKEND_DIR` 可覆盖，`TERMX_DAEMON_HISTORY_BACKEND=memory` 可诊断退回内存。harness 不带 `--daemon-history-file-backend` 时会隔离 `XDG_STATE_HOME` 并验证默认目录产物。100k default artifact `/tmp/termx-r131-default-filebackend-100k`：daemon copy-oldest 45.4MB、TUI copy-oldest 49.8MB、stress `real 2.59 user 2.22 sys 0.24`、outside baseline `real 1.33 user 1.29 sys 0.02`、oldest 可见 `000000`；daemon default history file 约 31MB，daemon forced heap inuse 约 14.6MB |
| 215E1-R132. SK TUI live 调度去固定帧率审计 | 完成 | `termx-tui-v3/app/`、`termx-tui-v3/services/`、`workflow.md` | 已按用户约束锁定 live/render 调度：没有新增固定 FPS 或固定 interval；ordinary live surface 返回时如果同 terminal 已有 dirty/queued successor，会只更新 reducer state 并跳过中间帧渲染，后续最新 surface 再画；lifecycle/resize/attach/exit 等边界仍保序且不被丢弃 |
| 215E1-R133. SK TUI render/canvas 热点压缩 | 完成 | `termx-tui-v3/render/`、`termx-tui-v3/services/`、`internal/protocol/`、`workflow.md` | 已按全局 heap/hotspot 收口 TUI render/canvas 热点：真实 TTY `RenderANSI` 直接从 pooled canvas 输出 ANSI，不再先构造完整 `RenderResult.Content`；canvas 只保留 2 个复用实例；content viewport 增加 extent 覆盖/整行内外 fast path；live snapshot wire 裁掉无样式行尾空白，TUI service 把同样式 ASCII live cells 合并成 run。没有引入固定 FPS / fixed interval；ordinary live 仍由事件队列、dirty 背压和可丢中间帧策略驱动。100k artifact `/tmp/termx-r133-live-run-direct-ansi-100k`：daemon copy-oldest 44.8MB、TUI stress 峰值 36.5MB、TUI copy-oldest 48.6MB、stress `real 2.55 user 2.23 sys 0.22`、outside baseline `real 1.31 user 1.27 sys 0.02`、oldest 可见 `000000`；TUI `alloc_space` 从 R132 约 1.357GB 降到约 572MB，`canvas.lines/newCanvas/renderContentViewportRow/contentViewportLineWindow` 退出真实 TTY 主热点，`terminalLiveLineFromCells` 从约 100MB 降到约 4MB |
| 215E1-R134. SK daemon latest-only vterm damage 压缩 | 完成 | `termx-core-v2/`、`termx-vterm/`、`workflow.md` | 已把 core live surface 的 latest-only VTerm 写入从 `WriteWithScrollbackDamage` 改成普通 emulator write：live surface 仍按事件写入并保留最新 screen/cursor，但不再为调用方不用的 scrollback damage 构造 run/cell payload；`WriteWithDamage` 增量路径仍保留 scrollback append。100k artifact `/tmp/termx-r134-latest-only-vterm-100k`：daemon copy-oldest 45.9MB、TUI copy-oldest 48.2MB、stress `real 2.55 user 2.23 sys 0.23`、outside baseline `real 1.33 user 1.29 sys 0.02`、oldest 可见 `000000`；daemon `alloc_space` 从 R133 约 2.10GB 降到约 1.53GB，`compactASCIIStyleRuns/scrollbackAppendOpsFromCharmVTDamages/scrollbackRunsToVTermRuns` 退出 daemon top 热点；未引入固定 FPS / fixed interval，也不丢 authoritative history truth |
| 215E1-R135. SK history cell/write 与 live snapshot 转换继续压缩 | 完成 | `termx-core-v2/`、`internal/protocol/`、`termx-tui-v3/`、`workflow.md` | 已按 R134 100k pprof 压缩 daemon history 写入和 live snapshot 转换：parser 同一批次连续 text cells 合并后再写入 HistoryTrack，store 内部 owned-create 避免新行返回 clone，seal active line 走内部 dirty seal，parser 复用已知显示宽度，live snapshot 转协议前裁掉无样式空白尾部。100k artifact `/tmp/termx-r135-batched-history-100k`：daemon copy-oldest 44.9MB、TUI copy-oldest 48.8MB、stress `real 2.54 user 2.24 sys 0.22`、outside baseline `real 1.33 user 1.30 sys 0.02`、oldest 可见 `000000`；daemon `alloc_space` 从 R134 约 1.53GB 降到约 1.35GB，`writePrimaryCellsOwned` 从约 207MB 降到约 2.5MB，`sealActiveLine` 从约 193MB 降到约 3MB，`vtermCellsToProtocol` 从约 88MB 降到约 82MB。未引入固定 FPS / fixed interval，仍由事件循环和 dirty 背压允许丢中间 live 帧，不丢 authoritative history truth |
| 215E1-R136. SK stress harness 校准与双侧剩余热点 | 完成 | `scripts/`、`termx-core-v2/`、`termx-vterm/`、`termx-tui-v3/`、`internal/protocol/`、`workflow.md` | 已继续按 100k stress 全局 pprof/hotspot 收口双侧剩余热点：core live snapshot 改成保留行位置但只克隆非默认空白前缀，TUI live protocol cells 合并成 run 时按小容量窗口和精确 builder 构造，harness 增加 copy-oldest idle 与 heap profile 后 GC/RSS 诊断阶段并修正 profile stage 标记。100k artifact `/tmp/termx-r136-final-100k`：daemon stress 峰值 42.1MB、daemon copy-oldest 44.0MB、TUI stress 峰值 35.8MB、TUI copy-oldest 48.5MB、stress `real 2.52 user 2.23 sys 0.22`、outside baseline `real 1.33 user 1.30 sys 0.02`、oldest 可见 `000000`；daemon `alloc_space` 从 R135 约 1.35GB 降到约 1.13GB，TUI `alloc_space` 从约 552MB 降到约 481MB，`liveSurfaceCellsFromProtocol` 从约 73MB 降到约 11MB。未引入固定 FPS / fixed interval，仍由事件循环、dirty 背压和可丢中间 live 帧驱动，不丢 authoritative history truth |
| 215E1-R137. SK copy/history 虚拟滚动窗口 | 完成 | `termx-tui-v3/state/`、`termx-tui-v3/app/`、`workflow.md` | 已先收口用户实测“进入历史模式持续向前滚动时 TUI 内存累加”的 TUI 本地窗口主因：older result 接纳后按可见区/预取区裁剪本地 `HistoryStore.SourceLines/Rows/Lines`，释放已经滚过的新尾部 backing array；copy cursor/mark/selection/query matches 随窗口裁剪重定位，frozen token/generation 和 tail boundary stale guard 保持不变。连续 older harness 证明 TUI rows/source lines 不再无界增长；当前协议只有 latest/older/oldest，所以本切片不伪造 newer/around contract，按 index/range 从 backend 拉取复制和双向虚拟窗口拆到下一切片 |
| 215E1-R138. SK history newer backend fetch | 完成 | `termx-core-v2/`、`internal/protocol/`、`termx-proto/`、`termx-tui-v3/services/`、`termx-tui-v3/state/`、`termx-tui-v3/app/`、`workflow.md` | 已补 R137 未扩协议的双向虚拟窗口后半段：`history.window` 新增 `mode=newer` 和 after cursor 字段，core-v2 在 frozen snapshot 内按 local tail 向较新方向返回 `append` window；TUI `HistoryStore` 支持 append/newer pending，local trim 改成围绕当前 viewport 保留窗口，trim 后 older cursor 绑定本地第一行，向下回到被回收的新尾部时从 backend 拉回。协议 stale guard 仍用 token/generation/boundary，不用 local scrollback 或固定帧率掩盖 |
| 215E1-R139. SK history range copy backend fetch | 完成 | `termx-core-v2/`、`internal/protocol/`、`termx-proto/`、`termx-tui-v3/services/`、`termx-tui-v3/state/`、`termx-tui-v3/app/`、`workflow.md` | 已补用户要求的 copy mode “记录 index，然后从后端取”：protocol 新增 `history.copy`，沿用 frozen token/generation/boundary stale guard，并带 logical range endpoint；core-v2 在 frozen snapshot 内按 logical line/cell display column 组装复制文本；TUI selection 保存 logical anchor/focus，虚拟滚动裁剪后不再为了超大选区常驻所有 rows，被裁掉的范围复制时从 core/file backend 拉 authoritative logical line |
| 215E1-R140. SK repeated stress RSS 高水位回收 | 完成 | `scripts/`、`termx-core-v2/`、`termx-tui-v3/`、`workflow.md` | 已处理用户实测“同一 terminal 连续执行 100k stress 脚本约 10 次后 RSS 升到 120MB+ 且不回落”：harness 新增 `--repeat`，逐轮记录 daemon/TUI RSS、CPU、memstats、heap、history file 大小和 live surface footprint；定位到 1M 顺序 committed id slice、file-backed compact slot padding，以及 copy/latest 当前 generation 冻结时仍全量解码/构造 ID 的高水位。实现后 committed index 用连续 range 保存，file compact slot 压成 64-bit offset/length，frozen snapshot 当前 generation/连续 committed range 不再扫描落盘 payload 或构造 100 万 ID slice，并在 terminal/request 边界做受阈值限制的 runtime page 归还；不靠定时 scrub、丢 history truth、清 live fallback 或固定 FPS。对比 artifact：R140 前 `/tmp/termx-r140-repeat10-current-100k` daemon run10 约 103.3MB、copy-oldest 约 111.7MB；最终 `/tmp/termx-r140-repeat10-range-final-100k` daemon run10 约 61.9MB、copy-latest 约 61.9MB、copy-oldest 约 62.0MB，TUI run10 约 37.4MB、copy-oldest 约 50.3MB，history file 约 307.8MB，10 轮 stress `real 2.55-2.63s`，oldest 可见 `000000`；daemon forced heap inuse 约 15.3MB，top 常驻只剩 1M line packed file slot 约 8.1MB |
| 215E1-R141. SK 单次真实 stress 双侧内存与 CPU 逼近 tmux | 完成 | `scripts/`、`termx-core-v2/`、`termx-tui-v3/`、`termx-vterm/`、`internal/protocol/`、`workflow.md` | 已按用户当前基准重新校准 harness：默认真实执行 `python3 scripts/generate_terminal_stress.py --lines 100000`，不再默认追加 seed/width；报告记录 command、baseline time、每轮 time、daemon/TUI RSS/CPU/memstats/heap、history file 大小和 live/copy latest/copy oldest marker trace，并补 `--use-real-state` 复现真实用户 state。TUI render terminal ANSI style 热路径改成有界缓存 + 直接拼 SGR 参数，避免 stress 有限 palette 每格重建 `[]string`/Join；不改成固定 FPS，也不丢 authoritative history。最终 clean 100k artifact `/tmp/termx-r141-final-100k`：daemon idle 17.1MB、stress 39.2MB、copy-oldest 39.2MB，TUI idle 29.1MB、stress 36.4MB、copy-latest 44.8MB、copy-oldest 50.0MB，history file 30.7MB，stress `real 2.64 user 2.28 sys 0.21`，outside baseline `real 1.33 user 1.30 sys 0.02`，copy-oldest 可见 `000000`/最新行/done marker；同代码 clean 路径未复现 daemon 200MB+ / TUI 110MB 或单次落盘后 120MB+ 常驻，若用户默认 state 仍复现需用 `--use-real-state` artifact 对照定位 |

当前下一步：

- `215E1-R141 单次真实 stress 双侧内存与 CPU 逼近 tmux` 已完成：最终 clean artifact `/tmp/termx-r141-final-100k` 执行用户同款命令并记录双侧 RSS/CPU/heap/history trace；daemon stress/copy-oldest 约 39.2MB，TUI stress 约 36.4MB、copy-oldest 约 50.0MB，history file 约 30.7MB，oldest/latest/done marker 都可追溯。当前 clean 路径没有复现 daemon 200MB+、TUI 110MB 或单次落盘后 120MB+ 常驻；如用户真实默认 state 仍复现，后续直接用本 harness 的 `--use-real-state` 对同一命令采样。
- `215E1-R140 repeated stress RSS 高水位回收` 已完成：连续 10 次 100k stress artifact `/tmp/termx-r140-repeat10-range-final-100k` 显示 daemon 从 R140 前 run10 约 103.3MB / copy-oldest 约 111.7MB 降到 run10 约 61.9MB / copy-oldest 约 62.0MB；copy/latest 不再因为冻结当前 generation 全量解码落盘历史而抬高 RSS，TUI 仍稳定在 stress 约 37.4MB、copy-oldest 约 50.3MB。
- `215E1-R139 history range copy backend fetch` 已完成：copy selection 保存 logical endpoint，`history.copy` 用 frozen token/generation/boundary + logical range 从 core/backend 拉 authoritative 文本，TUI trim 不再为了超大选区保留所有滚过 rows。
- `215E1-R138 history newer backend fetch` 已完成：`history.window mode=newer` + append 合并让 R137/R138 已回收的本地窗口能从 frozen backend 拉回较新内容；TUI trim 现在围绕 viewport 虚拟化，不靠 fixed FPS 或 local scrollback。
- `215E1-R136 stress harness 校准与双侧剩余热点` 已完成：core live snapshot 不再克隆默认空白尾部，TUI live cell run 合并避免按 cell 数预分配，harness 增加 copy-oldest idle 与 profile 后 RSS/heap 诊断。100k artifact `/tmp/termx-r136-final-100k`：daemon copy-oldest 44.0MB、TUI copy-oldest 48.5MB、stress `real 2.52 user 2.23 sys 0.22`、oldest 可见 `000000`；daemon `alloc_space` 约 1.13GB，TUI `alloc_space` 约 481MB，调度仍无固定 FPS / fixed interval。
- `215E1-R135 history cell/write 与 live snapshot 转换继续压缩` 已完成：parser 连续 text runs 聚合后一次写入 HistoryTrack，store 内部 owned-create/dirty-seal 避免新行和 seal clone，parser flush 使用已知显示宽度，live snapshot 转 protocol 前裁无样式空白尾部。100k artifact `/tmp/termx-r135-batched-history-100k`：daemon idle 17.3MB、daemon after stress 42.9MB、daemon copy-oldest 44.9MB、TUI stress 峰值 36.1MB、TUI copy-oldest 48.8MB、stress `real 2.54 user 2.24 sys 0.22`、outside baseline `real 1.33 user 1.30 sys 0.02`、oldest 可见 `000000`。daemon `alloc_space` 从 R134 约 1.53GB 降到约 1.35GB，`writePrimaryCellsOwned` 从约 207MB 降到约 2.5MB，`sealActiveLine` 从约 193MB 降到约 3MB，`vtermCellsToProtocol` 从约 88MB 降到约 82MB；RSS 仍只小幅变化，后续 50% 级别收益必须继续看 compact file decode/window projection、live snapshot screen clone 和 TUI protocol decode/render，不应转向固定帧率限制。
- `215E1-R134 daemon latest-only vterm damage 压缩` 已完成：core live surface 只消费 VTerm latest screen，不消费增量 damage；因此 latest-only 写入改成普通 emulator write，跳过 scrollback damage recorder 和 `DamageOp` 构造。`WriteWithDamage` 增量路径仍保留 scrollback append，history truth 不通过这条 live surface damage。100k artifact `/tmp/termx-r134-latest-only-vterm-100k`：daemon idle 17.2MB、daemon after stress 43.4MB、daemon copy-oldest 45.9MB、TUI stress 峰值 36.2MB、TUI copy-oldest 48.2MB、stress `real 2.55 user 2.23 sys 0.23`、outside baseline `real 1.33 user 1.29 sys 0.02`、oldest 可见 `000000`。daemon `alloc_space` 约 1.53GB，相比 R133 约 2.10GB 下降约 27%；R133 daemon top 中的 `compactASCIIStyleRuns/scrollbackAppendOpsFromCharmVTDamages/scrollbackRunsToVTermRuns` 已退出本轮 top list。RSS/脚本 wall time 基本不变，说明当前主剩余仍在 history clone/write、live snapshot protocol 转换和 file backend decode，而不是固定帧率或 latest damage 构造。
- `215E1-R133 TUI render/canvas 热点压缩` 已完成：真实 TTY `RenderANSI` 直接从 pooled canvas 输出 ANSI，完整 `RenderResult` 只留给测试/调试；content viewport 增加 extent 覆盖和整行 fast path；live snapshot wire 不再传输无样式行尾空白，TUI service 合并同样式 ASCII live cell run。没有引入固定 FPS / fixed interval。100k artifact `/tmp/termx-r133-live-run-direct-ansi-100k`：daemon copy-oldest 44.8MB、TUI stress 峰值 36.5MB、TUI copy-oldest 48.6MB、stress `real 2.55 user 2.23 sys 0.22`、outside baseline `real 1.31 user 1.27 sys 0.02`、oldest 可见 `000000`；TUI `alloc_space` 约 572MB，相比 R132 约 1.357GB 下降约 58%，真实 TTY 路径不再以 `canvas.lines/newCanvas/renderContentViewportRow/contentViewportLineWindow` 为主热点。
- `215E1-R130 100k daemon RSS 回收与 backend 策略基准` 已完成：harness 增加 daemon/TUI runtime memstats 和 file backend artifact 统计，先证明 no-limit 100k 下 daemon copy-oldest RSS 约 119.8MB、HeapAlloc 约 78.7MB、HeapSys 约 103.8MB；SIGUSR1 forced heap 后 inuse 约 34MB 但 macOS RSS 仍不回落，单纯 `FreeOSMemory`/`madvdontneed` 不足以达成用户可见下降。随后改为 file-backed compact history backend：clean sealed logical line payload 落到文件，Go heap 只保留 offset/length；CommittedHistoryIndex 顺序场景去掉重复 `present` map，file slot 压成 packed offset/length。100k final artifact `/tmp/termx-r130-filebackend-indexslot-100k`：daemon idle 17.6MB、daemon after stress 45.0MB、daemon copy-oldest 46.6MB、TUI copy-oldest 49.5MB、stress `real 2.60 user 2.23 sys 0.24`、outside baseline `real 1.33 user 1.30 sys 0.02`、oldest 可见 `000000`；daemon history file 约 31MB，forced heap inuse 约 14.6MB。32MB daemon memory limit 组合 artifact `/tmp/termx-r130-filebackend-limit32-100k` 只把 daemon copy-oldest 小降到 45.4MB，CPU 基本不变但收益很小，因此主收益来自 backend 策略，不靠定时 scrub/fallback 或强制丢 history truth。
- `215E1-R131 100k 默认 daemon history backend 收口` 已完成：默认本地 daemon 现在使用 file-backed compact history backend，默认目录为 `$XDG_STATE_HOME/termx/core-v2-history` 或用户 state dir，保留 `TERMX_DAEMON_HISTORY_FILE_BACKEND_DIR` 覆盖和 `TERMX_DAEMON_HISTORY_BACKEND=memory` 诊断退回。harness 改成默认隔离 `XDG_STATE_HOME=$ROOT/state`，不带 `--daemon-history-file-backend` 时也会走默认 file backend 并记录默认目录。100k artifact `/tmp/termx-r131-default-filebackend-100k`：daemon idle 17.1MB、daemon after stress 42.6MB、daemon copy-oldest 45.4MB、TUI copy-oldest 49.8MB、stress `real 2.59 user 2.22 sys 0.24`、outside baseline `real 1.33 user 1.29 sys 0.02`、oldest 可见 `000000`；daemon default history file 约 31MB，forced heap inuse 约 14.6MB。至此 R130 的收益进入默认真实路径，不再依赖专用 harness 开关。
- `215E1-R129 stress 脚本双侧 RSS/CPU 真实基准与 TUI/daemon 联合优化` 已完成：harness 增加 stress 期间 daemon/TUI RSS/CPU 采样和峰值报告；TUI ordinary live event 改成 refresh invalidation，由 AppRuntime 队列合并、skip-render 和 TerminalSurfaceStore in-flight dirty 背压驱动拉取最新 surface，不再使用 33ms 固定帧率/固定 interval 限流，也不让普通 refresh 进入布局测量。100k final artifact 在 `/tmp/termx-r129-refresh-layout-backpressure-100k`：daemon idle 16.7MB、daemon copy-oldest 121.5MB、TUI idle 32.3MB、TUI copy-oldest 48.1MB、stress 峰值 TUI 39.0MB、脚本 `real 2.54 user 2.24 sys 0.22`、baseline `real 1.33 user 1.29 sys 0.02`，oldest 页可见 `000000`。TUI copy-oldest 相比 R128 约 120.0MB 降到 48.1MB，达到 50% 量级；daemon 仍约 121MB，pprof 指向真实 compact history payload 和 backend 策略问题，不能靠丢 history truth 或 storage scrub 处理。
- `215E1-R128 daemon compact clean/reencode 与 live/vterm alloc 热点` 已完成：storage compact payload 改为 v1 run/color encoding，dense line 直接从 logical line 编码，去掉旧格式读取兼容；history pipeline lazy alt capture 避免普通 primary 输出维护第二套 live/vterm。100k clean artifact 在 `/tmp/termx-r128-final2-100k`：daemon idle 17.0MB、daemon copy-oldest 121.6MB、TUI idle 34.6MB、TUI copy-oldest 120.0MB、daemon cumulative CPU 约 6.2s、脚本 `real 2.57 user 2.27 sys 0.23`、baseline `real 1.33 user 1.30 sys 0.02`，oldest 页可见 `000000`。daemon `inuse_space` 约 43.9MB、`alloc_space` 约 2.1GB，剩余主要是 live/vterm write latest、protocol live snapshot 和 history clone；TUI `inuse_space` 约 11.9MB、`alloc_space` 约 1.9GB，主要是 `cloneLiveCellRows`、`terminalLiveLineFromCells`、`newCanvas/canvas.lines`、protocol row decode。结论：本切片改善 CPU/alloc，但 RSS 未达 50%/100% 量级，下一步必须转向 TUI live/render allocator 高水位，不能继续在 compact header/slot 上抠个位数 MB。
- `215E1-R124 真实 TUI 100k stress 双侧内存优化` 已完成：真实 TUI pane 执行 100000 行 stress 的最终 harness 数据为 daemon idle 19.7MB、daemon copy-oldest 117.7MB、TUI idle 35.0MB、TUI copy-oldest 84.0MB，脚本 `real 4.69 user 2.58 sys 0.31`，oldest 页可见 `000000`。profile 显示剩余 daemon heap 主要是真实 compact history payload 与 dense slot；继续大幅下降要进入 history backend 分段/压缩/落盘设计，不能靠清理 authoritative history 或定时 scrub。
- `215E1-R127 daemon history LoadLine clone churn 优化` 已完成：store 内部 mutation/commit 判定不再通过 public `Line()` 克隆整行，改为 owned read + 专用写入/commit-state helper。100k clean artifact 在 `/tmp/termx-r127-ownedline-100k`：daemon copy-oldest 116.5MB、daemon cumulative CPU 23.27s、TUI copy-oldest 81.9MB、脚本 `real 3.61 user 2.32 sys 0.29`、baseline `real 1.32 user 1.28 sys 0.02`，oldest 页可见 `000000`。daemon alloc_space 从 R126 约 7.0GB 降到约 3.1GB，`cloneCells` 从约 4.0GB 降到约 244MB；下一步 R128 应看 dirty clean 时的 compact reencode 和 live/vterm alloc 热点。
- `215E1-R126 daemon/TUI stress harness 与量级优化入口` 已完成：真实 harness 默认 `profile-mode=final`，RSS/CPU 表不再被每点 heap dump 污染，并额外生成 daemon/TUI pprof top 与 DOT 热点图源；本机缺 Graphviz，SVG 失败原因会写入 `profile-graphs/README.txt`。100k clean artifact 在 `/tmp/termx-r126-clean-100k`：daemon idle 17.0MB、daemon copy-oldest 117.3MB、TUI idle 34.4MB、TUI copy-oldest 79.6MB，脚本 `real 4.21 user 2.43 sys 0.31`，outside-termx baseline `real 1.32 user 1.29 sys 0.02`，oldest 页可见 `000000`。TUI 去掉 screen+Lines 双副本后 RSS 比 R125 约降 4-5MB；daemon profile 明确剩余主线是 `LoadLine/LogicalLine.Clone/cloneCells` GB 级 churn，下一步进入 R127，不能继续死磕 compact header。
- `215E1-R125 100k history payload 与 TUI RSS 继续压榨` 已完成：compact line header 默认值压缩和 dense slot 只保留 encoded payload 后，100k 真实链路当前 daemon copy-oldest 约 116.6MB、TUI copy-oldest 约 84.2MB、脚本 `real 4.84 user 2.60 sys 0.32`，oldest 页可见 `000000`。全局 pprof 产物在 `/tmp/termx-r125-global-profile-current-100k/analysis`：daemon `inuse_space` 约 53.3MB，其中 `encodeCompactLogicalLine` 约 27.0MB、dense slot 约 2.6MB；daemon `alloc_space` 约 8.0GB，其中 `cloneCells` 约 4.33GB，主要来自 `MemoryStorageBackend.LoadLine` 与 `HistoryTrack.commitFrontier/writePrimaryCells`；TUI 当前 inuse heap 约 10.6MB，但 alloc_space 约 1.2GB，主要来自 live snapshot clone 与 render canvas。结论是 storage header 微调只能省个位数 MB，下一步应转向 R126/R127 的 clone churn / 分段或文件 backend 量级优化。
- `215E1-R123 daemon RSS runtime limit 与 history churn 继续压榨` 已完成：daemon runtime memory limit 作为显式可关闭开关接入；真实 30000 行 smoke 验证 48MB limit 最优，daemon stress/copy RSS 约 79.7MB，TUI copy-oldest 约 49.7MB。代码侧继续减少 owned append/replace clone、core CSI 参数 parse 分配、vterm fast SGR 参数 scratch 和 scrollback damage 容器扩容；benchmark 未显示 CPU 回退。剩余 daemon RSS 未压到 50MB，profile 显示 heap 约 36-39MB 且 RSS 仍有 allocator 高水位，继续下降需要更大架构策略（例如 history backend 分段/落盘），不能靠清理 truth 或定时 scrub 完成。
- `215E1-R122 live/vterm 写入 RSS 高水位优化` 已完成：fast SGR live 写入不再按 batch 克隆整屏，history pipeline 内部 owned cells 去掉 parser batch clone 和 store/backend 二次 clone。最终 30000 行真实 smoke：daemon idle 19.5MB、stress 后 110.0MB、copy oldest 110.2MB；TUI attached 38.3MB、copy latest 44.2MB、copy oldest 50.8MB。单个 daemon heap profile 约 41MB，证明剩余 daemon RSS 不是 history/copy truth 常驻，而是 Go runtime/allocator 高水位；在不引入定时 scrub、丢 history/live truth 或 fallback 的前提下，本轮不能证明 30000 行 daemon 达到 50MB。
- `215E1-R121 daemon/TUI/history 真实 RSS 测量` 已完成：新增真实 `scripts/termx_memory_smoke.sh`，记录 daemon idle / stress 后 / copy latest / copy oldest 与 TUI attach/copy RSS，并落 daemon/TUI heap profile。1000 行场景 daemon 约 46MB；30000 行场景 daemon 约 124MB、TUI copy-oldest 约 51MB，不能证明大场景已达 50MB。
- `215E1-R120 daemon/TUI/history 内存 profile 优化` 已完成：pprof 证明主要固定常驻来自 core terminal 的 live surface 与 history alt-capture surface 内部 vterm parser data buffer。已把每个 vterm 固定 data buffer 从 4MB 收到 64KB，并补 32KB OSC title 回归测试；常规 copy/history benchmark 保持稳定。继续把历史模式本地已加载 older 缓存做有界窗口，需要单独切片设计 cursor/search/selection 坐标迁移，不能用丢 slice 的症状补丁直接处理。
- `215E1-R119 TUI 配置代码适配` 已完成：新增 v3 独立 config package，支持解析 `tui-v3.yaml`、缺省默认、env 覆盖、unknown field/坏颜色报错和 keymap 冲突检测；`state.Root.Config` 会进入 render theme resolver，CLI 默认 root/attach 启动会读取 `$XDG_CONFIG_HOME/termx/tui-v3.yaml`，不存在则用内置默认。
- `215E1-R118 TUI 标准配置样例` 已完成：新增完整 `tui-v3.example.yaml` 标准模板，字段齐全，中文注释说明用途、默认行为和示例值，并挂回配置管理设计文档作为后续 loader/schema 对齐基准。
- `215E1-R117 TUI 配置管理文档` 已完成：新增 v3 独立 TUI 配置管理基准，不复用 tuiv2 shared config 作为运行时依赖；明确主题、chrome、interaction、keymap 等配置项、加载优先级、host palette 与用户覆盖的合成规则，以及 renderer/input 只能消费 resolved token 的边界。
- `215E1-R116 tab create unconnected pane 与空 workspace header` 已完成：v3 `tab.create` 现在会生成 active `unconnected` pane 和 root split；无 tab workspace 的 header 不再 fallback 出虚假的 `main` tab，只保留 workspace 名和新建 tab 入口。
- `215E1-R115 空 tab 顶栏光标残留回归` 已完成：空 tab 提示页不再生成可见 bar cursor；无 panel 的空 body 提示页也不会 fallback 成隐藏 IME anchor，避免真实终端光标停在 tab/header 附近形成多余竖线。
- `215E1-R114 root 空终端启动 picker` 已完成：root 入口只复用现存 terminal；没有 terminal 时启动空 workbench、打开 Terminal Picker，由用户显式创建或 Esc 回到 `unconnected` pane；该空启动会跳过旧 workbench storage 初始恢复，避免没有 core terminal 时被旧连接意图覆盖。
- `215E1-R112 workspace 空槽位语义` 已完成：新建 workspace 会得到 main tab 和全屏 `unconnected` empty pane，不自动绑定 terminal；关闭所有 tab 后只展示居中快捷键提示。
- `215E1-R113 顶部栏鼠标交互` 已完成：workspace 名称点击打开 Workbench Navigator；tab switch/close/create 鼠标路径有 runtime 回归；创建按钮改为带间距的 `󰐕`。
- `215E1-R111 workspace tab CRUD 快捷键闭环` 已完成：tab/workspace 模式使用 `c` 新建、`n` 下一个、`p` 上一个、`r` 重命名、`x` 关闭/删除；footer 展示和 input binding 已用测试锁定；Workbench Navigator 选中非当前 workspace 下的 tab/pane/floating 时，open/new/rename/delete/zoom 会先路由到目标 workspace。
- `215E1-R110 workbench navigator workspace 操作回归` 已完成：Workbench Navigator tree 现在遍历所有 workspace；选中 item 的名称带下划线；`Ctrl-N`/底部 New 在 workspace 节点上创建新 workspace；floating 子节点使用 terminal 图标并按 pane/terminal 状态着色；右侧 preview 按选中 workspace/tab 查找 pane/floating。
- `215E1-R109 floating tab-scoped model 重构` 已完成：floating window 只挂在 tab 下，命令、渲染、输入、storage、Workbench Navigator 和浮窗 overview 都消费当前 tab 的 floating；旧顶层 floating storage 已移除，不做迁移兼容。选中 tab 时右侧会同时预览该 tab 下的 tiled pane 与 floating terminal，floating 行显示连接的 terminal 名称并保留 `floating` 状态标签。
- `215E1-R108 workbench navigator tab terminal 展示` 已完成：Workbench tree item 增加 display title，tab 下子节点显示 terminal pool 名称或 terminal id；pane 原标题仍参与搜索；tab 右侧 preview 过滤未连接空槽位，展示该 tab 下所有已连接 terminal。
- `215E1-R107 workbench navigator T 框贴边回归` 已完成：Workbench Navigator content rect 贴外框内侧绘制，内部横线/竖线由 overlay chrome 合并到左右和底部边框；search 直接位于标题边框下一行；tab 预览里的每个 pane frame 使用 terminal pool 名称或 terminal id 展示。
- `215E1-R106 workbench navigator 布局微调` 已完成：左侧 tree 列比 R105 更宽，搜索行下方有横线分隔，snapshot/hit region 坐标同步下移。
- `215E1-R105 workbench navigator tree 交互回归` 已完成：Workbench Navigator 搜索支持 Backspace/Delete 删除；floating 会作为实际节点展示并可打开；选中 tab 时右侧按 pane 列表分配多个 live snapshot；tree 行的图标、标题和状态 token 按 active/running/floating/owner 等状态着色。
- `215E1-R104 workbench navigator 真实 dump 校准` 已完成：新增可选择 smoke case 的 visual snapshot 入口，并用 tmux `capture-pane -epN` 产出 Workbench Navigator plain/ANSI dump；Workbench Navigator 现在按弹窗内部空间放大右侧 snapshot，搜索光标在 search 行，action 留在底部语义行。
- `215E1-R103 workbench navigator 快照复刻` 已完成：Workbench Navigator 使用左树右快照布局；左侧展示 workspace/tab/pane 状态，右侧复用现有 panel/live renderer 展示当前 pane snapshot，打开时默认落到 active pane。
- `215E1-R102 clipboard history 列宽样式回归` 已完成：clipboard history 预览正文改为前景色，搜索命中字母保持黄色；modal 高度改为内容驱动，名称列默认加宽并支持鼠标拖拽分隔符调整。
- `215E1-R101 clipboard history viewport 预览回归` 已完成：clipboard history modal 现在按外部 terminal viewport 留边展开，左侧保持窄名称列，右侧展示选中项多行正文预览。
- `215E1-R100 copy selection 显示与最近粘贴回归` 已完成：copy/history 选区现在会把选中行的行尾空白显示成黄底；Enter 复制仍写入 clipboard history，`Ctrl-V` 后 `p` 会从最近复制或 clipboard history 最新项粘贴，不再误报 copy buffer empty。
- `215E1-R99 clipboard history modal 线框与模糊搜索` 已完成：clipboard history modal 现在是 `Clipboard History` 顶边标题 + 搜索区 + 左窄右宽 T 字分栏；快捷键留在全局 footer；输入 `gft` 这类子序列能匹配 `git commit ... fix terminal` 并高亮命中字母。
- `215E1-R98 clipboard history 快捷入口` 已完成：`Ctrl-V` 进入 copy/history 后按 `H` 会打开 v3 clipboard history overlay；latest 仍在飞的 entering 阶段也不再吞掉 `H`，copy footer 会显示 `H CLIPBOARD` 可点击入口。
- `215E1-R97 clipboard history 手工新增` 已完成：v3 clipboard history overlay 现在有 New entry 入口，支持 `Ctrl-N` 和 content action 打开 v3 prompt，提交后写入 reducer-owned clipboard store 并保存到 core-v2 daemon storage。
- `215E1-R96 sticky shortcut mode 超时退出` 已完成：v3 现在只让 pane/resize/tab/workspace/floating/global 这类 sticky prefix mode 空闲 3 秒后退出；有效快捷动作会续期，overlay/copy 页面仍保持显式关闭语义。
- 当前有效输入模型：TerminalHost 的 key/mouse 入口同源；runtime 先做 mouse hit-test 激活输入态，普通 key 与 terminal mouse passthrough 统一进入 `TerminalInputRouter`，只按 active TerminalView binding 发起带 ack 的 protocol `input` 请求；protocol/core 按 view-scoped attachment 校验，失败只重连当前 view，不覆盖 sibling binding
- 当前 sibling view 输入隔离：同一 TUI client 的 protocol control request 会并发处理；一个 view 进入 copy/history 触发的 history latest 不再等待 history ingest backlog 追平，也不能 head-of-line blocking 后续 sibling view `input` 请求。TUI copy-mode 输入拦截只在当前 active TerminalView 属于该 copy session 时生效。
- 当前 resize owner 语义：owner 主动获取、attach result 投影成 owner、关闭旧 owner 后 sibling 自动接任，都会触发一次 view-scoped `ensure_resize`；如果目标尺寸等于 core 当前 PTY size，core 只返回最新 ownership/control，不调用实际 PTY resize。
- 当前 restart 语义：terminal lifecycle 和 terminal data 分离；process 退出/重启不会清空 core-v2 authoritative history，也不会清空 live tail。restart 会让所有 view channel 失效并逐 view reattach，但 TUI 等待 reattach 时继续显示旧 live tail。
- 当前 exit marker 语义：process 退出时 core-v2 先 force commit primary frontier，再把 `terminal exited`、`exited at`、`command` 三类 marker 作为显式系统输出追加到 live surface 和 authoritative history；这不是 storage/snapshot/TUI overlay 推导出来的当前进程内状态。
- 当前 lifecycle 权威边界：当前 terminal 是否 exited/running 只看当次 core terminal 查询或 core lifecycle event/surface；pane/floating 只有空槽位或连接到 TerminalView 的状态，workbench storage 只保存布局和连接意图，不参与当前 lifecycle 或输入路由判断。
- 当前 live 队列边界：`LifecycleKnown=true` 只存在于 `LiveSurfaceMsg` / `LiveEventMsg` / service result 这类一次性消息边界，不属于 ordinary live frame；runtime 不允许用后续普通 live 帧合并替换它，且 TUI snapshot/store 不保存这个权威性标志。
- 当前旧 storage 兼容：R75 前已经写入 opaque storage 的 `"exited"` / `"copy-history"` pane kind 会在 restore 时迁移成 terminal-live 连接意图；如果旧 snapshot 缺 TerminalViews，会从 pane/floating 的 terminal intent 补 detached view binding。
- 当前 live cursor 语义：terminal live cursor 只来自 core/protocol surface cursor；restart 后 core 把保留 tail 之后的 append row 作为 visible cursor seed，TUI 只把该 surface Row/Col 映射到当前 view layout，不按文本尾部合成。

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
- `215E1-R66` 已完成：terminal 退出后 core/protocol/tui-v3 会保留退出时间、退出码和 command；pane/floating 的退出态渲染显示这些信息；restart 不复用旧 channel，而是保留 TerminalView 绑定意图并逐 view 重新 attach，避免 sibling view 被覆盖或失效。
- `215E1-R68` 已完成：live exited 信息不再压在历史前面，而是追加到 terminal 内容尾部；视口默认显示尾部，使最后历史行、退出时间、命令和 restart/picker CTA 一起可见。
- `215E1-R69` 已完成：picker/attach 选择 pool 中已退出 terminal 时不会再把 exited surface 临时清成 attached；UI 会立即显示目标 terminal 的退出时间、命令和 CTA。
- `215E1-R71` 已完成：terminal process exit marker 现在进入 core-v2 live surface 和 authoritative history，重启后仍能在 live tail/history 中看到退出发生的时间、退出码和 command；TUI 已补去重，避免 core marker 与 render overlay 重复。
- `215E1-R72` 已完成：restart 后旧 live tail 没有 surface cursor metadata 时，render 不再把光标放到旧内容最后一列；等新进程 live surface 带回真实 cursor 后再显示。
- `215E1-R73` 已完成：restart 保留 live tail 时 core 不再把 VTerm cursor seed 成 hidden，而是映射到保留 tail 后的 append row 并保持 visible；TUI render 已补测试，证明 live cursor 使用 core surface Row/Col。
- 已知环境缺口：本机当前没有 `protoc` 与 `protoc-gen-go`；只有在需要重新生成 proto 时才构成阻塞。
