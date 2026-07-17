# 代理说明

## 最高工作基准

- 仓库根目录 `workflow.md` 是当前分支唯一有效的活动驱动文件。
- 本仓库内所有工作必须先读取 `workflow.md`，并以它作为范围、任务顺序、测试准入和提交规则的唯一基准。
- 当前活动主线只由 `workflow.md` 最早未完成切片决定；快捷键与单区域 Cloud 已是背景，当前优先完成客户端目录 ownership 收口和 CONN003 共享 runtime 重构。
- 插件系统已经拆到独立分支，本分支不新增插件系统代码、协议或文档。
- `docs/remote-platform/` 是当前远程平台产品、架构、安全和迁移基准。
- `tui/docs/multi-endpoint-transport-plan.md` 是当前多 endpoint / 多 transport 技术规划。
- `core/docs/architecture.md` 是 core-v2 技术设计基准。
- `tui/docs/architecture.md` 是 tui-v3 技术设计基准。
- `docs/development/repository-layout.md` 是目录 ownership、依赖方向和迁移边界的唯一架构基准。
- `AGENTS.md` 只规定代理执行方式和目录职责，不替代 `workflow.md` 的范围判断。
- 若 `workflow.md` 与旧说明、聊天记录、旧代码行为或局部假设冲突，默认以 `workflow.md` 为准。

## 当前私有开发阶段原则

- 当前仓库是唯一开发真值，并且整体仍是私有仓库；公开客户端、闭源云服务、内部文档和开发工具可以在同一 monorepo 中正常开发和提交。
- 当前优先级是尽快形成用户可运行、可观察、可验证的纵向产品闭环，不是提前完成未来开源发布工程。
- 除非用户明确启动“正式开源/发布准备”切片，否则不得主动建设或扩展 public mirror、第二仓、exporter、同步工具、clean-room 仓库、public-only CI、复制白名单、发布时源码剔除流程或额外的 private 隔离层。
- 已有 public/private 目录和依赖边界只用于表达领域 ownership 与安全责任；不得仅为了未来可能开源而反复移动文件、拆 module、增加 adapter、复制源码或把 public snapshot 独立构建设为日常开发门禁。
- `private/` 目录不是当前产品完成度指标。只要没有破坏 terminal truth、CapabilityGrant、DeviceIdentity private key、DataChannel payload 等安全边界，就应优先完成真实功能，而不是继续优化物理目录隔离。
- 正式开源时再从选定稳定提交执行一次性代码审查、许可证确认、secret audit、目录复制和新 Git 历史初始化；这些工作默认延后，不得阻塞当前功能开发。
- 若 `workflow.md` 把 public snapshot、开源许可证收口或 private 物理隔离排成当前活动切片，但用户没有明确要求进入发布阶段，必须先修正 `workflow.md`，不得机械执行该切片。
- 开发阶段禁止提前优化：单区域端到端链路完成前，不做 Relay Mesh、多 transit、全球多区域高可用、复杂计费平台、通用插件、分布式状态、无中断动态换路或为假设性扩展设计的大型抽象。
- 优先实现最小但真实的纵向闭环；允许在显式 dev/staging harness 中使用内存 store、固定测试身份和本地进程装配，但不得把 dev 凭据、宽松鉴权或 fallback 带入默认生产路径。
- “文档、接口、领域模型和 fake 测试完成”不等于产品完成。活动切片的完成条件必须包含当前阶段可观察的用户行为或真实跨组件消息链路。
- 不做与当前切片无关的仓库级整理、命名统一、性能优化、发布自动化或防御性抽象；发现这类工作时记录为 deferred item，继续完成当前纵向目标。

## 自动执行模式

当用户启动 `/goal` 或要求自动推进时，按下面循环执行：

1. 读取 `workflow.md`。
2. 检查 `git status --short --branch`，确认是否存在未提交改动。
3. 如果存在未提交改动，先判断来源和范围：若只有当前文档基线改动，先运行文档准入并提交；凡不是本轮 Agent 已识别的当前切片改动，一律停止说明，除非用户明确要求接管；不得把用户或其他代理改动混入本切片提交。
4. 按 `workflow.md` 任务队列表格顺序选择最早未完成切片。
5. 如果最早未完成切片是 `阻塞`，停止并向用户说明阻塞，不得跳到后续 `待开始` 切片。
6. 如果最早未完成切片是 `待开始`，先把它改为 `进行中`，并提交或与本切片首个实现提交同切片提交。
7. 只执行该切片，不跨切片扩展范围。
8. 需要技术细节时读取 `workflow.md` 指定的当前规划文档和对应 architecture 文档。
9. 实现最小可验证改动，先补齐该切片要求的 harness，再接真实实现。
10. 运行该切片的测试准入命令。
11. 若 `workflow.md` 把该切片标记为双 Agent 审查切片，按“阶段双审查门禁”完成架构审查与代码审查；两个 reviewer 都明确 PASS 前不得提交或进入下一切片。
12. 更新 `workflow.md` 中该切片状态和必要的当前状态说明。
13. 使用中文提交信息提交本切片。
14. 若 `/goal` 仍在继续，再进入下一切片。

如果没有明确阻塞，不要停下来要求用户确认普通实现细节。若范围、语义或目录权限不清，必须先更新 `workflow.md` 或向用户说明阻塞。

## 范围规则

- 允许主动工作目录只能来自 `workflow.md` 的“当前主线允许主动修改”和“受限联动范围”。
- 不允许因为“看起来有关”自行扩散到其他目录。
- 旧 `termx-core/` 与 `tuiv2/` 已退出本分支，不再作为只读参考、legacy fallback 或默认依赖存在。
- 当前默认本地 CLI 入口必须走 `core/` 与 `tui/`；不得重新引入 `termx legacy ...`、旧 daemon、旧 TUI 或 remote legacy/fallback。
- `cmd/termx/legacy_*.go` 不得重新出现；旧本地入口已经删除。
- `cmd/termx/default_dependency_guard_test.go` 是默认入口依赖守卫；默认源文件不得 import 旧 `termx-core` 或 `tuiv2`。
- `remote/`、`clients/mobile/` 与 `clients/ui/` 是活动远程客户端资产；只能按 `workflow.md` 对应纵向切片演进，不得恢复旧 fallback。
- 旧 `termx-hub/`、`termx-remote/`、`web-control/` 及 remote-ui 的历史 localweb/docs 已迁入 `private/archive/termx-platform-legacy/`，只能作为只读历史资产；archive 不进入 workspace、构建脚本或 runtime。
- Hub/Relay 服务端实现位于 `private/cloud/hub/` 与 `private/cloud/relay/`；当前只保持必要逻辑依赖边界，不为未来 public repo 继续增加物理隔离工作。
- `vterm/` 是受限联动目录，只能在 terminal semantic transaction 接口、事件或 harness 需要时最小化触及。
- `internal/protocol/` 与 `proto/` 是受限联动目录，只能在 endpoint routing、history window/copy 或 semantic history contract 需要跨进程时最小化触及。
- 如果确实必须恢复旧目录或解冻目录，先修改 `workflow.md` 的范围表并说明原因；默认不允许恢复。
- 关键代码需要写简短中文注释，说明 domain owner、truth source、消息链路或失败条件。

## 目录职责

- `client/endpoint/`：客户端 Endpoint/Route 持久领域、assembler、planner 与 portable contract；不负责网络 IO、credential、protocol session 或 UI。
- `client/runtime/`：跨端客户端 route race、ReadySession、generation、session owner 和稳定 command/event 真值；不得依赖 TUI、CLI、平台 UI 或私有 Cloud 实现。
- `client/port/` 与 `client/adapter/`：host capability 接口与 local/SSH/managed/protocol adapter；adapter 不得创建第二份 route/session truth。
- `core/`：新 core 主线目录，负责 terminal lifecycle、daemon-local terminal identity、screen-backed history 模型、terminal semantic transaction 消费、`HistoryWindow`、storage/backend 与相关 harness。
- `docs/history/core/screen-app-infinite-history-final-plan.md`：旧无限历史定案，当前只在触及 history truth 时作为背景基准读取。
- `core/docs/architecture.md`：core-v2 技术设计基准。
- `vterm/`：终端语义解释来源；负责把 PTY bytes 解释成 terminal 语义事件或 transaction，不负责持有无限历史 truth。
- `tui/`：TUI 产品目录，负责 UI state、reducer/effect、AppRuntime、TerminalHost、FrameSink、workbench/layout、copy/history 投影、输入和 render；只通过 `tui/port` 与 `tui/adapter` 消费 client/core projection，不拥有 endpoint route/session、committed history 或 daemon terminal lifecycle。
- `tui/docs/architecture.md`：tui-v3 技术设计基准。
- `termx-core/`：已删除旧 core 目录；不得作为 fallback 恢复。
- `tuiv2/`：已删除旧 TUI 目录；不得作为 fallback 恢复。
- `internal/protocol/` 与 `proto/`：受限联动目录，只在 endpoint-aware routing、history window/copy 或 semantic history contract 需要时最小化触及。
- `remote/`：公开 managed WebRTC client/daemon orchestration、DataChannel E2E auth、平台 primitive interface 与 fake harness；不承载 Hub/Relay server 或账号业务。
- `clients/ui/` 与 `clients/mobile/`：公开共享 UI 和移动客户端；消费公开 endpoint/history/cloud contract，不拥有 daemon terminal truth 或私有云服务状态。
- `private/cloud/`：闭源 Control Plane、Companion、Hub、Relay、Web Controller 与官方移动装配；可以依赖 public contract，public namespace 不得反向依赖。
- `cmd/termx/`：Cobra、参数/target 解析、composition root、输出和退出码；不得实现网络连接、credential resolution、Hello、授权、session cache 或 cleanup。
- `shared/`：迁移期遗留 primitive/contract 容器，不得新增领域 owner；目标去向和当前允许迁移范围以 repository layout 文档和 `workflow.md` 为准。
- `testkit/`、`scripts/`、`Makefile`、`go.work`、`go.work.sum`、必要顶层说明文档：受限联动范围，只在当前切片需要时最小化触及。

## 硬语义规则

- 禁止症状补丁：遇到状态错乱、输入错路由、生命周期误判或恢复异常时，必须先定位权威状态边界和消息链路，再修改模型或契约；不得用 storage scrub、fallback、定时刷新、重复 attach、局部 if 分支等方式掩盖根因。
- 禁止补丁式实现：不得为了让当前 case 通过而堆叠临时分支、局部兜底、重复同步、隐式状态修正或旧路径兼容；每次修复都必须先说清 domain owner、truth source、消息链路和失败条件，再按模型/契约补 harness 后实现。
- 多 endpoint / 多 transport 主线必须保持 endpoint 边界清晰：跨 endpoint 状态使用 `EndpointID + TerminalID` 的 `TerminalRef`，不得把裸 `TerminalID` 当成全局唯一真值。
- Endpoint 表达“当前客户端要连接的 daemon 目标”，Transport 表达“到达该 endpoint 的方式”；daemon 侧客户端连接管理与 `client/runtime` 侧 endpoint session 管理不得混成一个模型。
- TUI 不拥有 terminal lifecycle、committed history 或 history truth；history/live/input/resize 必须路由到 owning endpoint 的 daemon。
- 远程产品目录只能按 `workflow.md` 明确切片重新设计；不得通过 fallback、桥接或旧入口把 archive 中的 remote/localweb/Web Controller 路径重新引回当前 TUI/core 主线。
- Hub/Relay 可以验证云服务准入和 Relay 租约，但不能看到或判断 terminal capability；CapabilityGrant 只能在完成 channel binding 的 direct TLS 或 DTLS DataChannel 端到端认证握手内由 owning daemon 验证。
- 免费 local、SSH、多 endpoint 和 terminal protocol 不得依赖私有服务、账号订阅或 Relay；收费边界只建立在托管云服务能力上。
- 桌面 closed cloud client 使用专用 out-of-process Cloud Companion 和 versioned local IPC，不得恢复通用插件系统或把私有模块链接进公开 `termx`；移动端使用同一 contract 的官方私有构建模块。
- direct TLS、WebRTC、TLS/DTLS channel binding、CapabilityGrant 与 terminal protocol 必须留在公开进程；Cloud Companion 失败只影响 managed Cloud route。
- 当前开发阶段只维护这个 private monorepo 并正常提交；闭源代码统一进入 `private/`。正式开源时复制审核后的公开目录到全新空 Git 仓库，不复制当前私有历史，当前不建设 exporter 或双仓同步。
- R419 后，history ingest truth 的基本单位是 core-v2 authoritative physical row/cell，不是 append-only logical line、visual row、wrapped row、snapshot scrollback、grid viewport、xterm buffer row 或 DOM/canvas row。
- core-v2 `ScreenHistoryBuffer` 是 main/alt screen、physical rows、cells、cursor、scroll region、RowID、Version 和 seal-once 的 domain owner；logical line 只是 query/copy/history 阶段的 projection。
- physical row store、sealed row index、logical projection、segment cursor、storage backend、cache、adapter、TUI/App projection 不能演变成第二份历史 truth。
- `persisted` 或落盘不表示不可修改；是否可修改由 terminal/session/row lifecycle 语义决定。
- raw PTY bytes parser 不能作为 terminal 语义 owner，也不能 fallback 出第二套历史。
- core-v2 应消费 vterm 解释过程中的 semantic transaction，而不是消费最终屏幕快照。
- vterm 当前屏幕不是无限历史来源；它只能提供终端语义解释后的可记录事件和 side proof，history truth 必须由 core-v2 screen buffer 消费语义后维护。
- tmux 等价目标只覆盖真实经过 PTY 的内容；程序没有输出到 PTY 的内部状态不在目标内。
- attach、reattach、bootstrap、recovery、full replace、clear screen、resize 不得凭空创建 committed history。
- resize 不得重写 sealed physical history；普通 logical line projection 可以在展示层重新 wrap，final screen-frame 必须固定生成时宽度。
- alt-screen 不写入 primary history；纯 alt-screen transient 退出时不 commit 屏幕内容。
- primary screen app 临时进入 alt-screen 前必须 archive/hide 当前 primary frame；退出 alt 后如果出现新的 primary 输出，必须作为新的 primary frame publish，可以接回同一 session journal，但不得复活 pre-alt current frame，也不得凭空 commit alt 屏幕。
- process exit 必须按 terminal lifecycle seal 当前 primary mutable physical rows/current frame，并按分类决定是否生成 final screen-frame projection。
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
- 所有新增或修改的导出 `type`、`interface`、`struct`、导出方法和导出函数都必须写清晰、详细的中文注释；注释要说明用途、领域归属、真值来源、消息链路、失败条件或调用边界中的至少相关部分，不能只复述名字。
- 关键代码路径必须写必要中文注释，尤其是状态归属、事务边界、跨模块消息传递、历史 truth 边界、失败分支和禁止 fallback 的位置；不要用空泛注释替代模型说明。
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

### 阶段双审查门禁

- 用户或 `workflow.md` 明确要求阶段双审查时，每个切片在实现和测试准入完成后、提交前，必须同时启动两个相互独立的只读 reviewer：一个负责架构审查，一个负责代码审查。
- 架构 reviewer 必须检查 domain owner、truth source、消息链路、失败条件、模块边界、重复真值、fallback、旧代码删除是否彻底，以及实现是否为了局部 case 引入补丁分支。
- 代码 reviewer 必须检查行为 bug、状态竞态、输入边界、错误处理、安全/隐私、性能退化、测试有效性和用户可观察回归；不得只做格式或命名检查。
- reviewer 必须基于当前阶段实现 diff、相关实现和测试给出 `PASS` 或 `FAIL`。审查范围不包含 reviewer PASS 后机械写入的 `workflow.md` 状态/审查证据；没有明确结论、只给摘要或仍有未解决 finding，都视为 `FAIL`。
- 主 Agent 必须独立判断并处理 findings，不能机械接受或忽略。修复任何实质 finding 后必须重新运行受影响测试，并把更新后的阶段实现 diff 交给原 reviewer 复审；架构与代码 reviewer 都明确 `PASS` 才满足门禁。
- reviewer 只读，不得直接改文件、提交或替主 Agent扩大切片。若双审查所需子 Agent 不可用，该切片标记阻塞，不得降低为单 Agent、自审或跳过。
- 两个 reviewer PASS 后只允许机械更新 `workflow.md` 的切片状态、reviewer 结论和已处理 finding 摘要，再运行 `git diff --check` 后提交；若同时修改任何实现、测试、其他文档或非审查元数据，必须重新交原 reviewer 复审。该终止规则避免“记录 PASS 本身又制造待审 diff”的无限循环。
