# 工作流：远程平台产品与架构重构主线

## 当前目标

- 当前分支转入远程平台文档先行重构：先冻结产品版本、商业边界、公开/私有源码边界、领域归属、安全协议和迁移顺序，再开始任何 runtime 改造。
- `docs/remote-platform/` 是当前远程平台产品与技术设计的唯一活动基线；ME012 已实现代码只作为原型和迁移输入，不再反向定义新架构。
- TUI/client 侧已经完成的多 endpoint / 多 transport 模型继续有效，详细技术背景以 `termx-tui-v3/docs/multi-endpoint-transport-plan.md` 为准。
- 插件系统已经拆到独立分支，本分支不继续新增插件系统代码、协议或文档。
- 旧 screen app 无限历史清场与重建记录不再保留在本活动文件中；需要追溯时查看 git 历史和对应文档。

## 基准文档

- `docs/remote-platform/README.md`：当前远程平台文档索引、有效性和冻结决策。
- `docs/remote-platform/product-prd.md`：产品、版本、商业模式和收费边界基准。
- `docs/remote-platform/architecture-spec.md`：公开客户端/daemon 与私有 control plane/Hub/Relay 架构基准。
- `docs/remote-platform/network-topology.md`：local/SSH/WebRTC 全景、direct/Relay 网络拓扑和连接时序图。
- `docs/remote-platform/global-acceleration-spec.md`：SmartRoute、single-relay 选区、双 Edge Relay Mesh、质量测量和商业分阶段基准。
- `docs/remote-platform/security-protocol-spec.md`：设备身份、终端授权、云服务准入和 Relay 租约安全基准。
- `docs/remote-platform/source-boundary-and-migration-plan.md`：公开/私有仓库分拆、旧资产保留和实施切片基准。
- `termx-tui-v3/docs/multi-endpoint-transport-plan.md`：当前多 endpoint / 多 transport 技术规划。
- `termx-tui-v3/docs/architecture.md`：TUI v3 架构基准。
- `termx-core-v2/docs/architecture.md`：core-v2 架构基准。
- `internal/protocol/` 与 `termx-shared/transport/`：现有协议和 transport 抽象基线。

## 当前允许主动修改范围

- `workflow.md`
- `AGENTS.md`
- `docs/remote-platform/`
- `termx-tui-v3/`
- `termx-cli/`
- `termx-shared/`
- `internal/protocol/`
- `termx-proto/`
- `termx-core-v2/`，仅当 endpoint 能力、terminal 生命周期或 history contract 需要时最小化触及。
- `termx-remote-v2/`，仅在 RP002、RP003、RP006 对应公开 contract、端到端授权和客户端接入切片触及；不承载私有云服务实现。
- `termx-hub/`，仅在 RP005、RP007 私有服务迁移和公开仓库清场切片触及；现有代码作为待迁移的私有 Hub/Relay 历史实现资产保留，不再作为公开 client contract 的 owner。
- `scripts/`、`Makefile`、`go.work`、`go.work.sum`，仅当测试或入口联动需要时最小化触及。

## 保留但非当前主线主动改造范围

- `termx-remote/`
- `termx-app/`
- `remote-ui/`
- `web-control/`

以上目录是远程管理、移动端、共享 UI 和 Web 管理面历史产品资产，必须保留但不得作为新架构 fallback。`termx-app/` 与客户端 UI 后续只在 RP006 按公开 client contract 重构；`web-control/` 与 `termx-hub/` 的服务端实现目标为私有仓库，当前目录在迁移完成前只作为可追溯参考，不再承载公开产品契约。runtime 目录只能按任务队列对应 RP 切片解冻。

## 硬语义规则

- `TerminalID` 只在单个 daemon/endpoint 内唯一；TUI/client 侧跨 endpoint 状态必须使用 `EndpointID + TerminalID` 的 `TerminalRef`。
- ME009 起，用户可见 terminal 名称是 first-party 新建 terminal 的默认 canonical key；协议字段仍暂名为 `TerminalID`，但 create 必须优先发送 terminal name，并在单 endpoint 内拒绝重名。
- 后续 identity 收敛切片才能重命名协议字段或删除旧 ID 兼容；当前不得把随机 ID 继续用于 first-party 新建 terminal 的 history/layout/storage key。
- Endpoint 表达“当前客户端要连接的 daemon 目标”；Transport 表达“到达该 endpoint 的方式”，例如 local unix socket、SSH、hub/P2P。
- WebRTC 表达一种到达 endpoint 的 transport；一次 WebRTC session 实际走 direct candidate 还是 managed Relay 是连接路径结果，不得建模成两种 endpoint 或两种终端协议。
- `direct`、`single_relay`、`relay_mesh` 只是 WebRTC `ObservedPath`；SmartRoute 必须基于质量、容量、成本和 entitlement 选路，不得按国家或城市硬编码固定 Relay 链。
- Relay Mesh 第一阶段只能有两个 Edge Relay 和一个逻辑 backbone segment；后续最多增加一个内部 transit，必须由真实 corridor 数据证明收益，禁止用户配置或系统生成任意 N 跳。
- Relay Mesh 全程保持 Client 到 daemon 的 DTLS 端到端加密；hop-level usage 可以分别采集，但账单必须按 route/session 聚合一次，不能机械重复计费同一字节。
- daemon 侧“有哪些客户端连接到我”与 TUI/client 侧“我连接了哪些 daemon endpoint”是两个不同管理器，不得混成一个模型。
- connection registry 是 CLI/TUI 共享的 endpoint 注册表，默认文件为 `$XDG_CONFIG_HOME/termx/connections.yaml` 或 `~/.config/termx/connections.yaml`；不得塞进 TUI-only 的 `tui-v3.yaml`。
- TUI 不拥有 terminal lifecycle、committed history 或 history truth；history/live/input/resize 必须路由到 owning endpoint 的 daemon。
- Workbench/layout storage 可以继续保存在本地客户端域，但其中的 terminal 连接意图必须持久化 `TerminalRef`，不能只持久化裸 `TerminalID`。
- endpoint label、transport address、hub identity、SSH host key 和 auth ref 不得互相替代；展示名不能作为安全身份。
- endpoint 离线、认证失败、transport 断开只能影响该 endpoint；不得清空其他 endpoint 的 terminal pool、layout、copy/history 状态。
- cancel token、live channel、surface/session key、input serial key、history token 和 copy token 都必须按 endpoint 作用域隔离。
- 现有 `internal/protocol` 仍以单 daemon 会话为边界；多 endpoint 路由应在 client/TUI 侧 manager 完成，不要求单个 protocol client 同时承载多个 daemon。
- SSH 第一阶段只作为连接远端 termx daemon 的 transport；不得悄悄 fallback 成原始 shell/PTY。
- hub/P2P endpoint 的展示名、endpoint id、hub URL、hub device id、device fingerprint、grant ref 和 relay 策略必须分离；`device_fingerprint` 才是远端设备安全身份，`hub_device_id` 只用于发现/路由，label 只用于 UI。
- hub/P2P 配对保持 daemon -> client 单向引导：daemon 生成 capability grant，客户端扫码或导入后保存到本地凭据存储；grant 只能在 WebRTC DTLS DataChannel 建立后的端到端握手中提交给 daemon，禁止进入 Hub/Web Controller 的 HTTP、gRPC、SDP、日志或持久化。
- hub/P2P relay 只能承载受限 protocol/datachannel，不能成为 terminal lifecycle、history、workbench storage 或设备信任 truth。
- hub/P2P 连接失败、授权失效、设备撤销或 relay 不可用时，只影响对应 endpoint，不得 fallback 到 local、SSH、旧 remote app 或原始 shell。
- `connections.yaml` 不保存 hub 原始 token、capability grant 或私钥；`grant_ref` 只能引用本地凭据存储、系统 keychain 或后续明确的 hub grant store。
- `AccountAccessToken`、`DeviceIdentity`、`CapabilityGrant`、`HubAdmissionTicket` 和 `RelayLease` 是五种不同凭据；不得复用字段、token 或验证责任。
- Hub/Relay 可以离线验证 control plane 签发的短期服务准入票据和 Relay 租约，但不得授予、扩大、撤销或解释 terminal capability；terminal 授权只属于 owning daemon。
- 免费本地连接、免费 SSH 和公开的多 endpoint 管理不得依赖 Web Controller、Hub、订阅或云账号。商业收费只建立在托管发现、托管 Relay、云同步、团队治理和运维 SLA 等持续服务成本上。
- 订阅失效只能拒绝新的付费 Relay/团队能力或按策略结束对应租约，不得踢掉 daemon 的免费本地/SSH 能力，也不得把 direct P2P 的 terminal capability 变成云端授权。
- `termx-hub/` 与 `web-control/` 的服务实现不进入公开源码发布；公开仓库必须保留足够的 wire contract、client SDK interface、错误语义和 fake harness，使 TUI、App 与 daemon 可独立开发和验证。

## 任务队列

| ID | 状态 | 范围 | 验收 |
| --- | --- | --- | --- |
| ME001 | 完成 | 清理 `workflow.md`，新增多 endpoint / transport 规划文档 | 文档说明术语、边界、阶段、风险和测试准入 |
| ME002 | 完成 | 引入 `EndpointID` / `TerminalRef` 状态模型，默认 endpoint 为 `local` | 本地单 endpoint 行为不变；同名 terminal 在不同 endpoint 不冲突 |
| ME003 | 完成 | 设计并实现 client 侧 connection registry 基础结构 | 可列出 local endpoint；配置缺失时有稳定默认；endpoint 名称和连接策略来自 `connections.yaml` |
| ME004 | 完成 | Terminal picker / Terminal Pool 支持 endpoint 聚合和局部失败 | picker 展示机器名称、endpoint 状态和 terminal；单 endpoint 失败不影响其他 endpoint |
| ME005 | 完成 | live/input/resize/owner/copy/history 路由按 `TerminalRef` 隔离 | owner 转移、输入、history token 不跨 endpoint 串扰 |
| ME006 | 完成 | workbench storage 持久化 endpoint-aware terminal binding | 旧 snapshot 默认映射到 `local`；缺失 endpoint 保留 unresolved binding |
| ME007 | 完成 | local unix socket 作为标准 endpoint transport | 当前本地 attach 路径迁移到 endpoint manager 后行为不变 |
| ME008 | 完成 | SSH transport 连接远端 termx daemon | 明确认证、host key、远端 socket 发现和失败展示 |
| ME009 | 完成 | Terminal picker 单一 create 入口与 terminal name identity 第一阶段 | picker 只展示一个 create 行；create prompt 记住上次 endpoint/command/workdir；新建 terminal 在单 endpoint 内按名称唯一，默认以名称作为 daemon-local key |
| ME010 | 完成 | hub/P2P 身份、安全、中继策略与 connection registry contract | `connections.yaml` 可表达 hub endpoint；label 不作为安全身份；hub 发现目标/relay 变化触发 reconnect；无真实 dialer 时不 fallback |
| ME011 | 完成 | hub/P2P 单向配对与 capability grant contract | `hub_device_id` 只做发现；`device_fingerprint` 是远端安全身份；`grant_ref` 指向 remote-issued grant；fingerprint/grant 变化触发 reconnect |
| CL001 | 完成 | 项目代码整理基线 | `workflow.md`、根 `AGENTS.md`、局部代理说明与当前 master 主线一致；明确 frozen legacy 目录只读边界和后续入口/依赖清理顺序 |
| CL002 | 完成 | 顶层 Makefile 入口清理 | 默认 help/phony/target 不再暴露 frozen `remote-ui`、localweb、旧 remote daemon/dev/pair/status 入口；保留 v2/v3 build 与测试入口 |
| CL003 | 完成 | CLI frozen remote 依赖清理 | `termx-cli` 默认命令和 daemon 启动不再装配 frozen `termx-remote` runtime；移除 CLI 对 `termx-remote` module 的 import/replace，保留 core/protocol typed hook 作为后续新控制面契约 |
| CL004 | 完成 | 恢复远程产品目录与 hub 归位边界修正 | `termx-remote/`、`termx-app/`、`remote-ui/`、`web-control/` 作为有效产品资产保留；`termx-hub` 产品逻辑已归位到 `termx-hub/internal` 且不再依赖旧 `termx-remote` module |
| CL005 | 完成 | `termx-shared` transport 与 terminal metadata 边界整理 | unix/memory transport 明确读包上限、关闭语义和 listener 生命周期；terminal metadata 只保留共享语义，不承载 TUI 图标/按钮文案 |
| SI001 | 暂停 | TUI 同步输入组交互与 input 多播 | 用户切换到项目代码整理后暂停；恢复时继续 `Ctrl-P i/v/u` 管理当前 TUI 本地同步输入组 |
| KS001 | 完成 | TUI 快捷键现状盘点与 shortcuts 设计文档 | `shortcut-inventory.md` 覆盖实际按键、展示提示、不一致项；`shortcut-system-plan.md` 固化 `tui.shortcuts` 单一配置入口、提示同源和旧 `tui.keymap` 删除策略 |
| KS002 | 完成 | shortcuts config schema/parser/validation | 新增 `TUIShortcutConfig`；支持 action label、场景短写/长写；删除旧 `tui.keymap` 配置字段、默认值、parser、validation、示例和测试，不保留 deprecated/fallback |
| KS003 | 完成 | 内置 action registry 与输入路由接入 | 默认 shortcuts 生成当前实际行为；自定义 shortcuts 是唯一输入真值；旧硬编码只能作为默认 catalog 来源，不保留第二套 route fallback |
| KS004 | 完成 | footer/help/overlay 提示同源接入与旧提示清理 | footer、help、overlay/menu 提示全部从 shortcut catalog 生成；用户删除 shortcut 后提示消失；修复当前提示与真实按键不一致问题 |
| KS005 | 完成 | 统一 action invocation、domain spec 与 dispatcher contract | 先补 registry/invocation 完备性 harness；底层 shortcut domain spec 统一 action id、参数 schema、allowed scenes、默认文案和展示策略；input 保留 invocation，app dispatcher 只拥有执行 handler |
| KS006 | 完成 | 键盘与点击统一 action 执行链路 | 先补键盘/点击 invocation 等价 harness；footer/content 点击与键盘派发同一 invocation；方向、focus、`tab.jump.N`、`floating.summon.N` 不丢语义或参数，歧义聚合提示只允许 hint-only |
| KS007 | 完成 | 删除快捷键硬编码 fallback 并补齐提示 | 先补显式空 catalog 无 fallback harness；sticky/copy Esc、空 footer `Ctrl-G`、Help close、Prompt suggestion 回到 catalog；每个 action 在 domain spec 明确 footer/help/click 策略 |
| KS008 | 完成 | shortcut key、scene、action 和参数校验收敛 | 先补 canonical key、scene-action 和参数矩阵；config 基于 KS005 domain spec 校验 routed/overlay scene，拒绝等价冲突、理论不可表达 token、错误 scene-action 组合及越界参数 |
| KS009 | 完成 | action 文案覆盖与 catalog 替换语义 | 先补默认、action-only、scene-only、显式空 scene 和 actions+scenes 矩阵；action-only 只覆盖默认文案，出现 scene 后用户 scene catalog 是完整按键真值 |
| KS010 | 完成 | 组合 action 与 terminal lifecycle 执行语义 | 先补成功、失败、TerminalRef 路由 harness；只实现 `panel.kill` / `panel.kill_and_close` handler、步骤和关闭时机，不再调整 registry 基础结构或开放脚本系统 |
| KS011 | 完成 | 增强键盘协议与 Ctrl+数字 | Kitty CSI-u raw bytes 可生成 Ctrl+数字 invocation；TerminalHost 成对 push/query/pop disambiguate 协议，capability 控制提示 availability，宿主序列不泄漏 PTY |
| KS011A | 完成 | shortcut 单键展示策略与数字范围表达式 | binding 长写支持 `show`；`[1...9]` / `ctrl+[1...5]` 在配置加载期配合 `{key}` 展开；footer 只展示显式可见项且不产生裸按键，Help 保留全部有效绑定 |
| KS011B | 完成 | shortcut bracket 字面量与范围识别回归修复 | `[`、`]` 及带修饰符 bracket 继续作为合法具体按键；仅范围表达式进入展开；真实用户配置可加载 |
| KS011C | 完成 | README 快捷键与增强键盘使用说明 | 主 README 覆盖 `show`、数字范围、Ctrl+数字 capability、iTerm2 条件、fallback 与诊断入口 |
| KS011D | 完成 | 全局返回导航与 CSI-u 控制键回归修复 | Esc 从 shortcut catalog 移出并按 suggestion、overlay、copy、interaction 层级统一返回；CSI-u 控制键还原为标准命名 Key且保留修饰语义 |
| CX001 | 完成 | endpoint 连接中与断线 pane UI 收敛 | 复用 reducer-owned `AttachPending` 和 endpoint runtime status；重连请求立即显示连接中，断线保留最后画面并结构化展示 endpoint、transport、原因和局部操作；不新增自动重试或 transport fallback |
| CX002 | 完成 | pane 未连接、重连中与异常断开统一状态面板 | 三态使用一致的信息层级与操作布局；只展示 reducer 已知状态，不伪造连接进度；异常断开按错误类别给出可执行提示并保留最后画面 |
| ME012A | 完成 | hub/P2P protocol transport primitive 与 scope harness | 新 DataChannel transport 只承载 termx protocol frame；daemon 必须按 capability scope 接入 core-v2，不依赖冻结 `termx-remote` runtime |
| ME012B | 完成 | remote-issued capability grant 与设备身份 | 定义 grant scope/expiry/revoke、凭据解析和 device fingerprint proof；Hub 不参与 terminal capability 授权，旧 session token 不作为 fallback |
| ME012C1 | 完成 | daemon 授权 DataChannel session acceptor | 已协商 DataChannel 只有在 grant/fingerprint/expiry/revoke 校验成功后才能进入 core-v2 scoped transport |
| ME012C2 | 完成 | daemon hub agent 与 offer/answer | 注册/发现/offer-answer/NAT traversal/relay 建立 DataChannel；原型 Hub 不验证 terminal capability，失败不创建 protocol session |
| ME012D | 完成 | TUI/CLI hub endpoint dialer | 注册 `hub-p2p` dialer，建立 protocol client bundle；失败只影响 owning endpoint，不 fallback 到 local/SSH/旧 remote |
| RP001 | 完成 | 远程平台产品、架构、安全与源码边界文档基线 | 完成 PRD、架构 spec、安全协议 spec、公开/私有仓库分拆与旧资产迁移计划；冻结实现门禁，文档之间术语和责任一致 |
| RP001A | 完成 | 远程平台网络拓扑与连接时序图 | Mermaid 图覆盖 local/SSH 云旁路、managed WebRTC direct、Relay fallback、凭据可见性和 E2E capability 握手 |
| RP001B | 完成 | 全球网络加速产品与 Relay Mesh 预研 | 明确是否建设、阶段边界、质量选路、双边 Edge Relay、计量、安全和网络图；不把任意 N 跳加入首版 Relay |
| RP002 | 待开始 | 公开 remote contract 与私有服务边界抽取 | 公开仓库只保留 endpoint/transport、信令 wire contract、client interface 与 fake harness；Hub/Web Controller runtime 不再被 client 直接 import |
| RP003 | 待开始 | DataChannel 端到端设备证明与 capability handshake | Hub/Control Plane 全链路看不到 capability grant；daemon 在 DTLS DataChannel 内完成设备证明、challenge 和 scope 映射后才接 core-v2 |
| RP004 | 待开始 | 私有 control plane 服务票据与 Relay entitlement | 账号、设备目录、Hub admission、Relay lease、套餐 entitlement 和 usage event 形成独立领域；订阅不参与 terminal authorization |
| RP005 | 待开始 | 私有 Hub/Relay 重建与旧实现迁移 | Hub 只做 presence/rendezvous/signaling，Relay 按短租约和会话计量；旧 session token、terminal inventory 和 bearer grant 信令全部删除 |
| RP006 | 待开始 | TUI 与 App 统一远程 endpoint contract | TUI/App 共用 endpoint、配对、凭据和错误模型；平台只各自实现 WebRTC primitive，不复制业务协议 |
| RP007 | 待开始 | 私有仓库分拆与公开仓库清场 | 保留 git 历史和归档 tag 后迁出 Hub/Web Controller 服务实现；公开构建、测试和文档不依赖私有源码 |
| GA001 | 待开始 | direct/Relay 网络质量观测基线 | 只采集 RTT、丢包、抖动、吞吐、断线和成本 summary；不含 terminal/grant 数据，不自动改路 |
| GA002 | 待开始 | SmartRoute single-relay 智能选区 | direct 与受限 single-relay 候选按质量和成本竞争；具备 hysteresis、cost guard、选择原因和局部失败 |
| GA003 | 待开始 | 双 Edge Relay Mesh corridor pilot | 两端就近 TURN、单逻辑 backbone、route-bound RelayLease、内部服务身份和 session-level usage reconciliation 完整通过 |
| GA004 | 待开始 | 单 transit 受控加速试点 | 仅当 GA003 数据证明特定 corridor 需要时启用，`max_internal_transit=1`；任意 N 跳保持禁止 |
| KS012 | 待开始 | 快捷键跨切片总契约守卫 | 汇总默认 catalog 完备性、键盘/点击等价、空 catalog、overlay、组合 action 和 capability 回归守卫，不在本切片首次补关键 harness |
| KS013 | 待开始 | 快捷键文档与示例收尾 | 更新现状统计、配置示例、支持键位和限制；删除错误可用性声明，确保可加载示例不会意外禁用未展示的必要入口 |

## 执行规则

1. 每轮先读取 `workflow.md`。
2. 开始实现前检查 `git status --short --branch`，不得混入用户或其他代理的未提交改动。
3. 按任务队列顺序推进最早的 `待开始` 或 `进行中` 切片；遇到 `阻塞` 不得跳过。
4. 切片开始时先把状态改为 `进行中`，可与首个实现提交同提交。
5. 只执行当前切片，不跨切片扩展范围。
6. 先补模型和 harness，再接真实 protocol、transport 或 CLI 入口。
7. 完成后更新任务状态、当前说明和必要文档。
8. 每个有效变动都必须提交，提交信息使用中文；用户明确要求不提交时除外。

## 测试准入

- 文档-only 改动：`git diff --check`
- `termx-tui-v3/` 改动：`cd termx-tui-v3 && go test ./... -count=1`
- `termx-cli/` 改动：`cd termx-cli && go test ./cmd/termx -count=1`
- `internal/protocol/` 改动：`go test ./internal/protocol/... -count=1`
- `termx-shared/connection/` 改动：`cd termx-shared && go test ./connection -count=1`
- `termx-shared/transport/` 改动：运行对应 package 的 `go test ... -count=1`
- 任意提交前都必须运行 `git diff --check`

## 当前状态

- RP001B 已完成：全球网络加速被定义为 Relay 之上的可选付费能力；先做质量观测和 single-relay SmartRoute，再按 corridor 数据门禁试点双 Edge Relay Mesh，最多允许一个内部 transit；固定地区链、任意 N 跳和中国特例不进入公开协议。Relay Mesh 图已用 Mermaid CLI 实际渲染并完成视觉检查，本切片未修改 runtime。
- RP001A 已完成：新增五张可直接在 Markdown/GitHub 渲染的 Mermaid 图，覆盖全部 transport、managed WebRTC 网络、direct 时序、Relay fallback 和五类凭据边界；图形已使用 Mermaid CLI 实际渲染验证，本切片未修改 runtime。
- RP001 已完成：远程平台 PRD、架构 spec、安全协议 spec 与源码边界/迁移计划已建立；Hub/Web Controller 服务端目标为私有仓库，公开仓库保留 client/daemon contract 与 fake harness，旧实现保留私有可追溯 archive；本切片未修改 runtime。
- ME012A-D 保留为已验证的技术原型，不再作为目标安全架构：当前 opaque grant 经 Hub 信令、Hub agent token、terminal inventory 注册和订阅 kick 等行为必须在 RP002-RP005 中按新 contract 删除，不保留兼容 fallback。
- ME001 已完成：工作流已收敛为多 endpoint / 多 transport 主线，详细规划落到 `termx-tui-v3/docs/multi-endpoint-transport-plan.md`。
- ME002 已完成：TUI state 已有 `EndpointID` / `TerminalRef` 基础模型，默认 `local` endpoint 保持现有本地行为，同名 terminal 可在不同 endpoint 下共存。
- ME003 已完成：`termx-shared/connection` 提供独立 `connections.yaml` registry loader，缺省返回稳定 `local` endpoint，并定义 label 热更新与 dial identity 变更需要 reconnect 的基础判断。
- ME004 已完成：TUI state 增加 reducer-owned endpoint 展示投影，Terminal picker / Terminal Manager 可按 endpoint 分组展示机器名称、transport、connect mode、状态和局部错误；endpoint-scoped list 失败不会清空其他 endpoint 的 terminal rows。
- ME005 已完成：live surface/session/input channel、Terminal Pool 操作、resize owner、copy/history pending/window/release 均带 `TerminalRef` 路由；同名 terminal 的 input serial、live refresh、history window 和 copy session 不跨 endpoint 串扰。
- ME006 已完成：workbench snapshot 持久化 `EndpointID + TerminalID` 连接意图，旧 binding 缺 endpoint 时默认恢复为 `local`；缺失、disabled 或 manual endpoint 的 binding 保留在 pane/floating layout 中并标记 unresolved，不自动 attach。
- ME007 已完成：TUI runtime 启动时加载 `connections.yaml` endpoint registry，local unix socket 作为 `EndpointManager` 的标准 local transport bundle 接入；terminal/core/live 请求进入 per-endpoint adapter 前剥离 `EndpointID`，回包后补回 `EndpointID`，当前本地 attach、list、live、history 行为保持不变；显式 `--socket` 仍覆盖 registry local socket。
- ME008 已完成：新增 OpenSSH stdio-proxy transport，`auth_ref=ssh:<alias>` 使用本机 SSH config，host key 由 known_hosts 严格校验，`remote_socket=auto` 在远端解析默认 socket；EndpointManager 可 lazy dial SSH endpoint，Terminal Manager 对 auto/on_demand endpoint 执行聚合刷新，失败只标记对应 endpoint offline 且不 fallback 成 shell 或 local endpoint。
- ME009 已完成：picker 只保留单一 create 行，create prompt 用 server 下拉选择 endpoint，并记住上次 endpoint/command/workdir；TUI/CLI/protocol first-party create 优先以 terminal name 作为 daemon-local key，core-v2 在 create 与 rename 时拒绝同 daemon 重名。
- ME010 已完成：`connections.yaml` 可表达 hub/P2P endpoint，hub URL、`hub_device_id` 与 relay 策略分离；label 只影响展示，hub 发现目标/relay 变化触发 reconnect；无真实 hub dialer 时 EndpointManager 只返回该 endpoint 的未连接错误，不 fallback。
- ME011 已完成：按用户确认的单向配对模型收敛 hub 安全 contract，remote 生成 capability grant 给客户端；`hub_device_id` 只做发现/路由，`device_fingerprint` 作为远端设备安全身份，`grant_ref` 指向本地保存的 grant，真实 `termx-hub/` transport dialer 和跨设备发现进入 ME012。
- ME012A 已完成：ME012 已拆为 transport primitive、grant/identity、daemon agent 和 client dialer 四个连续切片；`termx-shared/transport/datachannel` 以可靠有序消息抽象承载完整 termx protocol frame，明确背压、关闭和复制语义，不依赖 Pion 或冻结 `termx-remote`；core-v2 harness 证明 DataChannel session 必须经 `ServeScopedTransport` 接入，跨 capability terminal 的请求在 protocol method 入口被拒绝。
- ME012B 已完成：`termx-shared/remoteauth` 新增 daemon-local Ed25519 设备身份、稳定 `device_fingerprint`、remote-issued capability grant、受限 terminal/machine-events scope、expiry/signature/fingerprint/revoke 校验、客户端 `grant_ref` 文件凭据存储和 daemon 持久化撤销 store；Hub 不持有 daemon 私钥、不做 terminal capability 授权判断，旧 session token 不能被解析为 grant，配置文件仍只保存 `grant_ref`。
- ME012C1 已完成：`termx-remote-v2/` 按新模型解冻为 hub/P2P daemon/client transport owner；Pion adapter 只实现共享 DataChannel primitive，`daemon.SessionAcceptor` 使用 daemon-owned fingerprint 校验 grant 的签名、expiry 和 revoke 后才映射 core-v2 `TransportScope`。core-v2 scope 已改为显式 `AllowDaemon`/single-terminal/machine-events 三选一，零值 scope 拒绝，避免远程装配遗漏字段时意外获得 daemon 全权。
- ME012C2 已完成：`termx-hub/client` 以公开 gRPC agent stream 封装 Hub internal wire，remote-v2 只看到 opaque `CapabilityGrant`；daemon agent 注册、heartbeat、offer/answer 和 kick 均按单 Hub session 驱动，offer 授权失败只回该 session error。Pion answerer 先验 grant，再协商可靠有序 `termx-protocol` DataChannel；真实 WebRTC harness 已通过 protocol Hello/List 到 core-v2。CLI daemon 仅在 `TERMX_HUB_URL`、`TERMX_REMOTE_DEVICE_ID`、`TERMX_HUB_AGENT_TOKEN` 显式配置时启动 agent，默认本地 daemon 不变，远程 agent 后续运行失败只记录错误、不停止本地 listener。Hub/remote-v2 全量与 CLI remote/daemon 定向准入通过；CLI 全量仍有本切片前已存在的两个快捷键视觉基线失败（旧 `[Ctrl+E] RENAME` 标记和 footer 顺序/`PgUp` 差异），未在 remote 切片修改 UI。
- ME012D 已完成：`termx-remote-v2/client` 在访问 Hub 前先校验 grant 签名/fingerprint/expiry 和 `hub_device_id`，使用 Hub session HTTP 做 opaque grant 信令、按 `relay_mode` 约束 ICE/TURN，再建立可靠有序 `termx-protocol` DataChannel；真实 fake-Hub + Pion + daemon answerer + core-v2 harness 已通过 Hello/List。CLI `EndpointManager` 已注册 `hub-p2p` dialer，从 `$XDG_STATE_HOME/termx/remote-v2/credentials`（或默认 state dir）按 `grant_ref` 解析 secret，并复用现有 terminal/core/live/path protocol adapters；凭据缺失和 dial 失败只返回 owning endpoint 错误，不调用 local/SSH。remote-v2 与 tui-v3 全量通过，CLI remote/daemon/dialer 定向通过；CLI 全量仍只有已记录的两项快捷键视觉基线失败。
- CL001 已完成：master 项目整理基线已对齐，`workflow.md`、根 `AGENTS.md` 和 `termx-cli/AGENTS.md` 均明确当前整理主线、插件分支隔离、frozen legacy 边界与 remote CLI 清理债务。
- CL002 已完成：顶层 Makefile 已移除 frozen `remote-ui`、localweb、旧 remote daemon/dev/pair/status/test 入口，只保留当前 v2/v3 build 与测试入口。
- CL003 已完成：`termx-cli` 默认命令、daemon 启动、测试、README、脚本和 module 文件已移除 frozen `termx-remote` runtime/命令依赖；core-v2/protocol 的 typed remote hook 暂不在本切片删除。
- CL004 已完成：撤回“旧 remote/app/web 目录无效可删”的判断，`termx-remote/`、`termx-app/`、`remote-ui/`、`web-control/` 已恢复并保留为远程管理、移动端、共享 UI 和 Web 管理面产品资产；`termx-hub` 产品逻辑仍归位到 `termx-hub/internal`，且不再 require/replace 旧 `termx-remote` module。
- CL005 已完成：`termx-shared` unix transport 已增加 packet 读包上限、Close 解除阻塞语义和无 goroutine 泄漏的 Accept；memory transport 不再用 channel close panic/recover 表达生命周期；terminal metadata 删除 TUI 图标/按钮文案，只保留共享 tag/mode 语义。本切片未迁移 `perftrace/gridtrace`，诊断包重组后续单独处理。
- SI001 暂停：按用户确认的交互设计实现 TUI 本地同步输入组；同步状态属于当前 TUI reducer-owned 输入路由状态，不写入 daemon terminal lifecycle、history truth 或 workbench storage。
- KS001 已完成：已新增 TUI 快捷键现状盘点与 `tui.shortcuts` 设计文档；本切片未修改运行时输入路由。后续 KS002-KS004 必须删除旧 `tui.keymap`，不保留 deprecated 双路径、兼容 fallback 或第二份快捷键真值；每个阶段提交前必须运行准入并使用子 Agent 只读审核。
- KS002 已完成：`tui.shortcuts` config schema/parser/validation 已落地，支持 action label、场景短写/长写和内置 scene 校验；旧 `tui.keymap` 配置入口、默认值、parser、validation、示例和测试已删除，本切片未接入运行时输入路由。
- KS003 已完成：已建立内置 action registry 与 shortcut catalog 输入路由；默认 catalog 覆盖现有硬编码行为，自定义 `tui.shortcuts` 是唯一输入真值，显式空 `tui.shortcuts` 不回退默认，未知 action 和 runtime key 冲突在配置期拒绝。KS004 接提示/overlay 同源前需要统一 overlay `menu.*` registry。
- KS004 已完成：footer、help 和 overlay/menu 提示统一从 shortcut catalog 生成，旧 `tui.footer.actions` / `footer.modes.*.actions` 配置入口和 `FooterVM.Actions` 字符串 fallback 已删除；overlay Esc close 等主要动作通过 catalog 映射，用户显式删除 shortcut 后不再展示也不再触发对应动作。
- KS005 已完成：新增无 app/render/config 依赖的 shortcut domain registry，统一 canonical action id、typed 参数、allowed scenes、默认文案和显式展示策略；input binding 只产出 `ActionInvocation`，app dispatcher 负责投影到 reducer intent；旧 input action switch 和 shell-to-render adapter 已删除，默认 catalog/spec/dispatcher 完备性 harness 已落地。
- KS006 已完成：footer 与受 shortcut catalog 控制的 overlay/content 命中区携带 canonical `ActionInvocation`；点击消息保留 pane/floating/row 目标上下文，copy 与 overlay 分别由 owning reducer 消费；零值 click policy 不可点击，不同方向/参数的聚合提示只保留 hint-only；`floating.summon.N` 使用 typed index，overview 行使用 `floating_overview.open + Row`，不再混淆两种语义。
- KS007 已完成：该阶段曾将 sticky/copy Esc、Prompt suggestion Esc、Help close 收进 scene catalog；KS011D 已以全局返回导航替换 Esc 相关路径，并删除 `interaction.exit`、`copy.exit`、`prompt.suggestion_exit` 配置 action及 `prompt_suggestion` scene。空 footer 不虚构 `Ctrl-G`，Help 内容关闭文案与 hit region 仍随可配置 action catalog 同步。
- KS008 已完成：config 以 shortcut domain `ParseInvocation`、`AllowedScenes` 和 `Routable` 为 action/scene/参数真值；routed 与 overlay 共用 canonical key signature，拒绝 panel/pane、Esc/Return、modifier 顺序、Ctrl 字符大小写及 `Ctrl-@`/`Ctrl-Space` 等价冲突；quoted `.` 的短写和长写均通过真实 YAML parser；增强键盘 token 只做协议理论校验，实际 capability/available 状态仍由 KS011 负责。
- KS009 已完成：仅配置 `actions` 时保留全部默认 bindings 并覆盖文案；任意显式 scene（包括空 scene）声明完整用户 catalog，不继承其他默认 scene；`shortcuts: {}` 保留默认，`shortcuts:`、`actions:`、`scene:` null 明确拒绝；action alias 在 parser 中 canonicalize 且重复声明报错；按键 label > action label > shortcut domain `DefaultLabel`，footer 聚合只合并键和 invocation，不再覆盖文案。
- KS010 已完成：`panel.kill` 与 `panel.kill_and_close` 直接进入独立 pane command 链路；两者都只从 pane binding 读取 owning `TerminalRef` 发 kill，不 fallback 裸 `TerminalID`；kill-only 成功保留 pane，kill-and-close 仅在成功且 result/close 消费两次确认 pane 仍绑定同一 `TerminalRef` 后进入标准 workbench pane-close/persist 路径，失败、重绑定和 last-pane close 拒绝都保留 pane；配置仍只引用 action id，不开放脚本。
- KS011 已完成：TerminalHost 分步完整写入 Kitty keyboard protocol disambiguate push、capability query，并只在 push 成功后 pop；`CSI ? flags u` 回投 reducer-owned `HostCapabilities`，增强 binding 仅在 flag 1 确认后进入 footer/Help；CSI-u Ctrl+数字经 raw parser、标准 InputEvent 和 shortcut catalog 生成参数化 invocation，普通数字不误判，release/拒绝结构不触发 action，所有宿主 CSI-u 都不会原样泄漏到 PTY；不支持宿主继续使用 `Ctrl-T` 后数字 fallback。
- CX001 已完成：reconnect 请求先以目标 view 的 `AttachPending` 投影连接中，并把 owning endpoint 标为 connecting；成功回包收敛为 connected，失败清除 pending 并按错误分类回投 offline。断线 pane 保留最后 live surface，结构化显示 endpoint label/id、transport、terminal、reason 和局部 Reconnect/Disconnect 操作；未引入定时刷新、自动重试或 fallback。
- CX002 已完成：未连接态展示清晰的起始说明和主次操作；连接/重连态保留最后画面并说明输入暂停、新 endpoint session 正在建立；异常断开态把错误分类投影为用户可读 issue，原始错误降为 detail，并按 auth、host-key、transport、remote daemon、protocol、config 给出对应 next step。三态未伪造百分比、重试次数或 transport 阶段。
- KS011A 已完成：shortcut binding 长写新增三态 `show`，显式 false 只隐藏 footer、键盘与 Help 保留，显式 true 可覆盖 domain 默认 footer hidden；升序单数字 `[1...9]` 与 `ctrl+[1...5]` 在配置加载期用 `{key}` 展开并继续走 canonical key、scene/action、参数与冲突校验；footer 宽度不足时按键和文案整体裁剪，不再保留裸按键旧路径。
- KS011B 已完成：范围 parser 先识别合法具体 key，`[`、`]` 与 `ctrl-[` 不再被误判为范围；只有包含 `...` 的非法 token 才返回范围专用错误，完整用户配置已通过真实 CLI 加载验证。
- KS011C 已完成：主 README 新增快捷键单一配置入口、`show` 展示策略、数字范围表达式、Ctrl+数字增强键盘 capability、iTerm2 条件、`Ctrl-T` fallback 和输入诊断说明；示例明确是合并到完整 catalog 的片段，避免意外替换其他 scene。
- KS011D 已完成：`Root.CurrentBackNavigationLayer` 与 `NewBackNavigationReducer` 成为 Esc 的唯一 owner，固定按 prompt suggestion、overlay、当前 view copy/history、sticky interaction 返回一层；配置拒绝未修饰 esc/escape，footer 从同一层级自动补 `Esc BACK`，普通 live Esc 继续透传 PTY。Kitty CSI-u Esc/Enter/Tab/Backspace 归一为标准命名 Key，Shift-Tab 与 Alt 控制键修饰语义得到保留。`termx-tui-v3` 全量测试、真实用户配置 CLI 加载和 `git diff --check` 已通过；`termx-cli` 全包测试仍有两个既有 visual smoke 基线失败，已在未修改的 `HEAD 99eeb6e8` 独立复现，本切片未修改对应 CLI/visual target。
