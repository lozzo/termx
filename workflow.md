# 工作流：单区域公网 Cloud staging 纵向闭环

## 当前目标

- REC001 已完成仓库可控状态恢复：尚未提交的 RM004 目录与维护改动已经审计收口，误触发的发布期验证扩展已经清理。
- CLOUD001-CLOUD005 已完成单区域 Cloud 纵向闭环：开发云、桌面 managed direct、single Relay 与 Official Android 均已跨真实用户链路验收。
- CLOUD009-CLOUD011 已完成 Control Plane 降载闭环：客户端启动/刷新取得 edge token 与 HubDirectory，后续 direct、single Relay 与跨进程恢复只访问 Hub。
- UI001 已完成：共享首页机器卡片明确 Local、Cloud、Local + Cloud 能力，并投影列表级可达性、连接阶段和实际路径。
- UI002 已完成：桌面首页已重构为紧凑产品栏和表格式机器清单，同时保持移动端卡片交互。
- UI003 已完成：Official Android 与共享移动 UI 已对齐到 Web Controller 的直角、细线、冷灰和低装饰视觉语言，同时保留移动端安全区、触控、返回、震动和全屏终端交互。
- WEB001 已完成并在 WEB003 收敛运行架构：React/Vite 公开订阅 Landing Page 由 Nginx 静态托管，Control Plane 直接提供同源浏览器 API 和套餐投影，不再运行 Next.js 或独立 Go BFF。
- WEB002 已完成：Web 登录、浏览器 Session、订阅账户、staging Checkout、签名 webhook、幂等订单和 Entitlement/Hub 投影已形成公网纵向链路。
- CLOUD012 已完成：Web、TUI 与 Official Android 使用统一账号设备码登录，账号名下 daemon 注册到 Hub；登录后的 direct 与 single Relay 热路径只访问 Hub，Control Plane 中断不影响有效缓存期内的新连接。
- CLI001 已完成：已审计当前扁平 CLI 与公开 `v3` 测试命令，建立以 endpoint/terminal 为真值的对象化命令树、稳定 target、JSON/format、退出码、tmux 能力映射和分期实现门禁；当前尚未改动 CLI 运行行为。
- CLI007 已完成：可选 Cloud Companion 的安装引导已区分未安装与源码构建缺少官方 release root，两种用户错误都有清晰下一步且不再重复 Usage。
- CLOUD013 已完成：公网 HTTP staging 的非秘密 runtime 配置已固化进显式 development Companion；同一份已验证 manifest 同时装配网络 adapter 与 HTTP 登录策略，用户无需额外配置即可执行状态和登录命令。
- 用户已把 TUI 快捷键完整收口提升为当前主线：KS012-KS017 将依次完成跨层契约、单一 action domain、输入/scene、全部默认 action、提示/点击同源和真实终端验收；CLI002-CLI006 暂停，待快捷键项目完成后重新排序。
- WEB003 已由用户重排为暂停：邮箱密码、用户中心、订阅和 AFF 已完成，GitHub/Google OIDC 留待统一账号客户端链路验收后恢复。
- FILE001-FILE004 与 CLOUD006-CLOUD008 已完成；Official Android 显式 development build 已通过公网 HTTP staging 在 5G 真机完成 direct、single Relay、terminal 与恢复链路。生产上线前必须另行切换 HTTPS/TLS，不得复用本切片的明文 profile。
- 当前仓库是唯一 private monorepo；当前不是正式开源或生产发布阶段。public snapshot、开源许可证模板替换、secret audit、第二仓和发布自动化全部延后。
- GA003 Relay Mesh、GA004 transit、多区域高可用、复杂计费、SSO 和 live reroute 继续保持延后；CLOUD005 完成不会自动启动这些事项，必须由用户基于真实数据重新排序。
- 插件系统位于独立分支，本分支不新增插件系统代码、协议或文档。

## 活动基线

- `AGENTS.md`：当前私有开发阶段、自动执行、领域边界和实现纪律。
- `docs/remote-platform/README.md`：现有远程平台产品、架构、安全和分发文档索引；这些文档是设计背景，不代表用户链路已经完成。
- `docs/remote-platform/product-prd.md`：免费/付费边界和商业方向。
- `docs/remote-platform/architecture-spec.md`：公开 terminal 数据面与私有云服务的逻辑 ownership。
- `docs/remote-platform/security-protocol-spec.md`：设备身份、CapabilityGrant、Hub admission 和 Relay lease 安全边界。
- `docs/remote-platform/global-acceleration-spec.md`：GA001/GA002 已有质量与 single-relay 算法背景；Relay Mesh 仅作延后设计输入。
- `tui/docs/multi-endpoint-transport-plan.md`、`tui/docs/architecture.md`：TUI endpoint/transport 与 runtime 基准。
- `tui/docs/shortcut-system-plan.md`：KS012-KS017 快捷键单一 action 真值、输入/展示闭环与删除边界基准。
- `tui/docs/shortcut-completion-goal-prompt.md`：快捷键项目自动推进和逐阶段双 Agent 审查 prompt。
- `core/docs/architecture.md`：core terminal lifecycle、live/history 与 storage 边界。
- `docs/remote-platform/cloud-staging-roadmap.md`：唯一活动实现真值，收敛当前代码缺口、四类 session 身份、真实消息链路和 CLOUD002-CLOUD005 用户 DoD。
- `docs/remote-platform/file-transfer-spec.md`：FILE001-FILE004 的产品边界、daemon ownership、授权、协议、流控和迁移真值。
- `docs/remote-platform/hub-edge-control-plan.md`：CLOUD009-CLOUD011 的 Control Plane 降载、Hub 授权投影、故障语义与迁移真值。

## 当前产品真值

- `core/` 拥有 terminal lifecycle、screen-backed history 和 daemon-local terminal identity。
- `tui/` 与 `clients/` 拥有客户端 endpoint manager、交互和展示，不拥有 terminal/history truth。
- `remote/` 与公开进程拥有 WebRTC、DTLS、DeviceIdentity、CapabilityGrant、DataChannel 和 termx protocol。
- daemon 所在机器的文件系统是文件 metadata 与内容真值；公开 termx protocol 只在已授权 session 内暴露文件操作，客户端只持有列表、预览和 transfer projection。
- `private/cloud/companion` 与 Official mobile adapter 只拥有账号 session、云 API、signaling、RelayLease、质量 summary 和 route plan。
- `private/cloud/devcloud` 已用两个独立 loopback HTTP listener 和一个 UDP TURN listener 装配真实序列化、认证、admission、短期 Relay lease、quota 与 signed usage 边界；它仍是显式内存 dev-local profile，不是生产部署模板。
- Companion 默认继续使用 `UnconfiguredAdapter`；development build 只有显式传入 dev manifest 才启用 HTTP adapter。Android 默认 Official 构建继续 `login_required`，只有显式 `termxOfficialDevCloud=true` APK 启用固定 loopback dev gateway；Community 仍 fail closed。
- 显式 `relay_only` 已经通过真实 Pion TURN 接入 desktop managed endpoint；自动 SmartRoute、Relay Mesh 和多区域仍未进入用户链路。

## 私有开发阶段边界

- public/private 目录只表达领域 ownership 与安全责任；当前不继续为未来开源移动文件、拆 module、复制源码或增加额外隔离层。
- 公开 package 不应依赖私有云实现，私有代码可以依赖公开 contract；该逻辑边界不得演变成当前阶段的发布工程主线。
- `private/archive` 只读，不进入 workspace、构建或 runtime fallback。
- public snapshot manifest、guard 和许可证模板作为未来发布资产保留，但不属于当前日常测试准入，也不得主动扩展。
- 显式 dev/staging harness 可以使用内存 store、固定测试账号和本地进程装配；默认产品路径必须 fail closed，禁止旧 session token、宽松 Bearer、grant-in-signaling 或 local/SSH fallback。
- 当前优先级是最小真实纵向闭环，不做 Kubernetes、数据库集群、多区域调度、Relay Mesh、复杂 billing、通用插件或假设性扩展抽象。

## 硬语义规则

- `TerminalID` 只在 owning daemon/endpoint 内唯一；跨 endpoint 状态使用 `TerminalRef{EndpointID, TerminalID}`。
- Endpoint 表达 daemon 目标，Transport 表达到达方式；local、SSH、WebRTC 不改变 terminal protocol、history owner 或 endpoint identity。
- TUI/App 不拥有 terminal lifecycle、committed history 或 history truth；live/input/resize/history/copy 路由到 owning endpoint。
- CapabilityGrant 只由 owning daemon 签发和验证，只能在 DTLS DataChannel 端到端握手中提交。
- Control Plane、Companion、Hub、Relay、Route Planner 不得接收 CapabilityGrant、DeviceIdentity private key、terminal payload、history 或输入。
- Control Plane、Companion、Hub、Relay、Route Planner 同样不得接收文件路径、目录列表、文件 metadata、文件内容、摘要或 transfer resume offset；Relay 只能转发并计量 DTLS 内的密文 bytes。
- Account token、DeviceIdentity、CapabilityGrant、HubAdmissionTicket 和 RelayLease 是不同凭据，不得复用字段、签名输入或验证责任。
- local、SSH 和 direct P2P 不依赖账号、订阅、Hub 或 Relay；云服务失败只影响 owning managed endpoint。
- 禁止 legacy remote、旧 Hub/session-token、grant-in-signaling、原始 shell fallback、通用插件恢复和按应用名特殊适配。
- 文档、接口、领域模型和 fake 测试不等于产品完成；活动切片必须证明当前阶段的真实跨组件消息链路或用户可观察行为。

## 允许范围

- REC001：`workflow.md`、`AGENTS.md`、当前 RM004 已识别改动、原有 tmux 冷启动测试夹具，以及必要的维护 guard；不得新增 Cloud runtime。
- CLOUD001：`workflow.md`、`AGENTS.md`、`docs/remote-platform/`；只建立纵向 roadmap 和验收链路，不新增实现。
- CLOUD002：`private/cloud/`、必要 `shared/`/`proto/` contract、受 contract 编译影响的最小 `remote/daemon` 联动、`Makefile` 和显式 dev launcher/harness；不触及 TUI/Android 产品接线或 Pion E2E。
- CLOUD003：`private/cloud/companion`、`remote/`、`shared/`、`proto/`、`tui/`、`cmd/termx/` 与必要 CLI/dev harness；只完成 desktop managed direct。
- CLOUD004：`private/cloud/{control-plane,hub,relay,route-planner,companion}`、`remote/`、`proto/cloudpb/`、`shared/cloudcompanion/`、`cmd/termx/`、`docs/remote-platform/cloud-staging-roadmap.md`、必要 `Makefile`/dev launcher 与 harness；只完成单区域 single Relay。`proto/cloudpb`/Hub 只允许传递 ManagedSession-bound route preference，使 daemon 能领取自己的 principal-specific TURN credential；不得扩展 terminal protocol 或 Mesh 字段。
- CLOUD005：`private/cloud/mobile`、`clients/mobile`、`docs/remote-platform/`、Android 构建文件，以及 `clients/ui` 中最小 shared terminal-protocol/pairing contract 与 harness；只完成 Official Android 接线与手测材料。允许联动 `clients/ui` 的原因是现有移动壳复用其 terminal client，而当前 daemon 只接受单一 `protocol` DataChannel；真机纵向验收若证明 Hub presence TTL 到期后 owning daemon 不再上线，允许最小联动 `remote/daemon` 与 `cmd/termx`，只补 fresh presence 续约生命周期和 harness。不得借此恢复旧 Web Controller/Hub runtime、扩展浏览器远程链路或重做 UI 架构。
- CLOUD006：`private/cloud/{devcloud,web-controller,infra}`、`private/cloud/companion` 的必要 staging contract、`docs/remote-platform/`、`workflow.md`、必要 `Makefile` 与部署/验证脚本；只完成 `114.66.58.243` 单机 staging 装配和从本机或 `ssh al` 发起的真实链路。不得复用服务器现存 legacy `termx-hub`/`termx-web-control`，不得把 loopback 明文 dev profile、固定账号或内存 store 描述为生产部署；公网凭据、端口、服务状态和回滚步骤必须落档，secret 不得提交。
- CLOUD007：`private/cloud/{companion,infra,web-controller}`、`docs/remote-platform/`、`workflow.md` 与必要 staging harness；只增加显式 `staging-public-http` development profile和 `114.66.58.243:41100-41102` 反向代理。固定测试账号、内存 store 和 account session 会经过明文网络，禁止真实用户凭据、生产数据、stable build、默认配置或隐式 fallback 使用；不得借此放宽 `dev-local`/`staging-ssh`，HTTPS/TLS 必须作为上线前独立门禁。
- CLOUD008：`private/cloud/mobile/android`、`clients/mobile/android` 的必要 development build 配置、`docs/remote-platform/` 与 `workflow.md`；只增加显式 Official 公网 HTTP staging 开关并在当前 ADB 真机验证。默认 Official、Community 与原 loopback dev build 必须保持原 fail-closed/loopback 边界；禁止真实账号/数据，不新增 Web Controller 授权旁路或 legacy fallback。
- CLOUD009：`private/cloud/{hub,devcloud,companion,control-plane}`、`shared/cloudcompanion/`、`proto/cloudpb/`、`docs/remote-platform/` 与 `workflow.md`；只把 managed direct 的 client admission、短期 EdgeManagedSession 和 daemon answer 绑定下沉到 Hub。允许显式 dev cloud 使用内存授权投影，但 cache miss 禁止同步回源；不得联动 Relay、生产数据库或多区域调度。
- CLOUD010：`private/cloud/{hub,relay,devcloud,control-plane}`、必要 private cloud contract、`docs/remote-platform/` 与 `workflow.md`；只实现单区域委派 Relay authority、预算快照和 durable usage outbox，Relay 租约热路径不得查询 Control Plane。
- CLOUD011：`private/cloud/{companion,mobile,devcloud}`、`clients/mobile/`、必要 `shared/cloudcompanion/`/`proto/cloudpb/` contract、`docs/remote-platform/` 与 `workflow.md`；只实现 desktop/Official Android 启动/刷新 edge token 与签名 HubDirectory，并完成 Control Plane 中断验收。
- CLOUD012：`private/cloud/{devcloud,web-controller,companion,mobile,infra}`、`proto/cloudpb/`、`shared/cloudcompanion/`、`cmd/termx/`、`clients/mobile/`、`clients/ui/`、`docs/remote-platform/` 与 `workflow.md`；只实现浏览器账号审批设备码、TUI/Official Android 账号 Session、账号名下 daemon enrollment 和现有 direct/single Relay 数据面接线。密码、OIDC secret 与浏览器 Session 只留在 Control Plane；客户端不得接收密码或浏览器 Cookie，Hub cache miss 不得同步回源，CapabilityGrant 仍只在端到端 DataChannel 内验证。显式公网 HTTP 仅限 staging，生产默认继续 fail closed。
- UI001：`clients/ui/`、`clients/mobile/` 中 Official pairing 非秘密类别投影、`workflow.md` 与对应测试；只重做机器列表 view-model、卡片和实时状态订阅，不修改 terminal truth、云授权或 transport 选择算法。
- UI002：`clients/ui/`、`clients/mobile/` 的共享首页响应式布局、`workflow.md` 与对应测试；只调整桌面信息架构和机器行投影，保持移动端交互、连接状态 owner 与 transport 语义不变。
- UI003：`clients/ui/`、`clients/mobile/`、必要 Android source sync、`scripts/client-workspace-guard.mjs` 与 `workflow.md`；只统一 App shell、首页、设备行、设置、Sheet、文件和工作区 chrome 的视觉 tokens 与移动交互，不修改 endpoint/transport 状态 owner、连接算法、terminal/history/file truth 或 native cloud contract。允许最小联动 workspace guard 的原因是 Web Controller 已进入根 workspace，而既有 client gate 未同步该事实。终端画布保持全屏内容面；所有移动主操作保持至少 44px 触控区域并尊重 safe area/reduced motion。
- WEB001：`private/cloud/web-controller/`、必要顶层 npm workspace/lockfile、`private/cloud/infra/staging/`、`workflow.md` 与对应测试；实现 React/Vite 用户订阅 Landing、可配置套餐目录和静态部署装配，不实现支付、生产价格或把订阅变成 terminal capability truth。
- WEB002：`private/cloud/{web-controller,devcloud,control-plane,hub}`、`private/cloud/infra/staging/`、必要 Web Controller React workspace、`workflow.md` 与对应测试；只实现浏览器账号 Session、订阅/订单、显式 staging payment provider、webhook 幂等和 entitlement snapshot 发布。生产 provider/价格未配置必须 fail closed，不修改 terminal capability。
- WEB003：`private/cloud/{web-controller,control-plane,devcloud,hub}`、Web Controller React workspace、staging 装配、`workflow.md` 与对应测试；实现 OIDC identity/account、邮箱密码身份、节点目录管理、账单订阅、账户设置、AFF 归因/奖励与审计投影。OAuth secret、Session bearer 和密码摘要只在 Control Plane；未配置 provider fail closed；节点、推荐奖励和订阅不得扩大 daemon terminal capability。
- FILE001：`workflow.md`、`docs/remote-platform/`；只建立文件产品、权限、协议、流控、失败语义和迁移基线，不新增 runtime。
- FILE002：`proto/wirepb/`、`internal/protocol/`、`core/` 与必要 protocol/client harness；实现 daemon-owned 文件 metadata/read-preview 操作和显式 capability scope，不触及 Cloud 服务。
- FILE003：`proto/wirepb/`、`internal/protocol/`、`core/`、`remote/`、`shared/remoteauth/`、`cmd/termx/` 与必要 transport harness；实现同一 protocol session 内的流式上传下载、背压、取消、续传和完整性校验，并最小联动 pairing grant 的显式文件权限；不新增旧独立 DataChannel。
- FILE004：`clients/ui/`、`clients/mobile/`、必要公开 protocol adapter、Android 构建文件和手测文档；删除旧 `/files/*` 与 legacy file channel 依赖，完成 Official Android 真机和 direct/single Relay 验收。只有真实产品链路证明 contract 缺失时才最小联动 FILE002/FILE003 owner。
- VT001：`vterm/internal/vt/`、`vterm/vterm/`、`core/` restart harness 与 `workflow.md`；只修 Emulator close/read 生命周期竞态，不改变 terminal semantic transaction、screen/history truth 或 restart 产品语义。
- CLI001：`docs/development/cli-command-design.md`、`docs/development/README.md` 与 `workflow.md`；只做 CLI 现状审计、目标命令树、寻址/输出/退出码 contract 和实施切片，不修改 runtime。
- CLI002：`cmd/termx/`、必要 `internal/protocol/`/`shared/` 查询 contract、CLI 黑盒 harness、`docs/development/cli-command-design.md` 与 `workflow.md`；只完成命令骨架、local terminal 生命周期、输出与错误 contract，并移除产品 `v3`/smoke 暴露，不扩展 endpoint transport。
- CLI003：`cmd/termx/`、必要 service/config helper、`shared/connection/`、对应文档与 `workflow.md`；只完成 daemon 当前用户生命周期和配置管理，不使用宽泛进程查杀，不触及 Cloud 服务端。
- CLI004：`cmd/termx/`、`shared/connection/`、TUI endpoint dialer 可复用边界、必要 `remote/`/Companion public contract、对应文档与 `workflow.md`；只完成 endpoint-aware CLI 和 local/SSH/Hub 真实路由，不新增 transport fallback。
- CLI005：`cmd/termx/`、必要 `internal/protocol/`/`core/`/`remote/` contract、对应文档与 `workflow.md`；只完成 send/capture/resize/wait/events 和稳定自动化输出，不读取 TUI renderer 或建立第二份 history truth。
- CLI006：`cmd/termx/`、`shared/connection/`、必要 file/workbench/pair public contract、`private/cloud/companion` 的安装装配、对应文档与 `workflow.md`；只收口 file/workspace/pair 和 Cloud login/enroll 用户体验，不把私有 Cloud 逻辑链接进公开 CLI。
- CLI007：`cmd/termx/`、`README.md`、`workflow.md` 与对应 CLI harness；只改进可选 Cloud Companion 未安装、源码构建无官方 release root 的错误指引和 Cloud runtime error 输出，不改变签名信任、安装来源、账号或 managed transport 行为。
- CLOUD013：`cmd/termx/`、`private/cloud/companion`、`scripts/`、`Makefile`、`workflow.md` 与对应 harness；只生成显式 development 测试套件，把 `staging-public-http` 非秘密 endpoint manifest 固化进 Companion，并让 termx 复验同目录 Companion 的固定名称、权限和构建期 SHA-256 后按需启动。不得静态链接 private Companion、读取运行时 manifest、放宽 stable/official 签名安装或把 enrollment/session secret 编进产物。
- KS012：`tui/{shortcut,input,app,render,config,state,terminalhost}`、`tui/docs/{shortcut-system-plan.md,shortcut-inventory.md,shortcut-contract-debt.json}`、`workflow.md` 与必要测试；只做最终现状审计、机器可读 debt manifest 和“无未分类/无新增 debt”守卫，可删除已被新模型替代的测试 helper，不要求提前修完 KS013-KS016 gap。
- KS013：新增 `tui/action/`，并允许修改 `tui/{shortcut,input,app,render,config,state}`、对应测试/文档与 `workflow.md`；只建立中立 action domain、shortcut 引用和 keyboard invocation -> handler contract，删除重复 identity/alias/scene 语义，不迁移 render surface 提示/点击。
- KS014：`tui/{terminalhost,input,shortcut,config,app,state}`、对应测试/文档与 `workflow.md`；只收口 raw key/mouse 到 InputEvent、catalog 编译、scene/lock/back navigation 和 PTY passthrough，不触及 renderer 视觉重做。
- KS015：`tui/{action,shortcut,input,app,state,services}`、对应测试/文档与 `workflow.md`；逐场景完成默认 action 的真实 reducer/effect/service 闭环；无真实功能的 placeholder 必须实现或从默认 catalog 删除，不得用 toast/刷新冒充成功。
- KS016：`tui/{action,shortcut,input,app,render,state}`、对应测试/文档与 `workflow.md`；逐 surface 收口 footer/help/overlay/chrome/content CTA 与 keyboard/click invocation 同源，可删除所有硬编码操作键位提示和重复 render action 声明。
- KS017：`tui/`、`cmd/termx/` 的必要黑盒 harness、`scripts/termx_shortcut_smoke.sh`、`README.md`、`tui/docs/`、`workflow.md` 与配置示例；只做完整配置/文档、全量守卫和真实 tmux/CSI-u 验收，删除本项目已替代旧代码，不扩展其他 TUI 产品功能。
- `core/` 只有 terminal lifecycle、history 或 scoped protocol contract 确实需要时才最小联动；`private/archive/` 始终禁止主动修改。

## 任务队列

| ID | 状态 | 范围 | 用户可观察验收 |
| --- | --- | --- | --- |
| REC001 | 完成 | 恢复仓库可控状态并收口未提交 RM004 | Git 工作树干净；维护入口有效；开源发布工作已明确延后 |
| CLOUD001 | 完成 | 建立唯一 Cloud staging roadmap | direct、single Relay、Android 的消息链路和完成条件清晰且不互相冒充 |
| CLOUD002 | 完成 | 最小单区域开发云服务 | 一个命令启动显式 dev cloud；账号、设备、resolve、admission、signaling 跨真实服务边界通过 |
| CLOUD003 | 完成 | Desktop managed direct 闭环 | TUI 经 Companion/Hub/WebRTC direct 列出、attach 并操作真实 daemon terminal |
| CLOUD004 | 完成 | 单区域 single Relay 闭环 | 显式 Relay 策略通过 lease-bound TURN 连接；quota、到期、usage 和局部失败可验证 |
| CLOUD005 | 完成 | Official Android 闭环 | Official APK 可扫码/导入、连接、列出/attach terminal、输入并完成后台恢复手测 |
| CLOUD006 | 完成 | 单区域公网 Cloud staging | 新主线 Web Controller 与 Hub/Relay 在指定服务器独立运行；本机经真实网络完成 managed endpoint 验收，失败边界和运维步骤可复现 |
| CLOUD007 | 完成 | 无隧道公网 HTTP staging | 外部开发客户端无需 SSH tunnel 可访问 Web Controller、登录、resolve 与 Hub signaling；默认和 production 路径仍 fail closed |
| CLOUD008 | 完成 | Official Android 公网 staging | ADB 真机无需 reverse 可经 Wi-Fi/移动网络连接、列出/attach terminal、输入输出并完成后台恢复；Community/default Official 仍 fail closed |
| CLOUD009 | 完成 | Hub managed direct 本地授权热路径 | Hub 从版本化授权投影离线验证 client，并本地创建 EdgeManagedSession；关闭 Control Plane 后有效快照内的新 direct 连接仍成功，撤销/过期/cache miss fail closed |
| CLOUD010 | 完成 | 单 Relay 委派授权与用量补报 | Hub/Relay 使用区域委派预算签发短期凭据；Control Plane 中断时有效预算内可连接，用量经幂等 durable outbox 补报 |
| CLOUD011 | 完成 | 客户端启动凭据与 Hub 目录刷新 | desktop/Official Android 启动时可访问 Control Plane 获取/刷新签名 edge token 与 HubDirectory，后续 direct/Relay 连接只访问 Hub |
| CLOUD012 | 完成 | 统一账号登录与账号节点归属 | 用户在 Web 注册后可审批 TUI/App 设备码；账号名下 daemon 可注册到 Hub；TUI/App 使用同一账号完成 direct 与显式 single Relay，连接热路径不访问 Control Plane |
| UI001 | 完成 | 首页机器类别与实时连接卡片 | 列表明确 Local、Cloud、Local + Cloud，并实时显示可达、连接阶段、实际 direct/Relay/local 路径与失败状态 |
| UI002 | 完成 | 桌面机器工作台 | 宽屏使用桌面导航、工具栏和稳定列机器清单，不再呈现放大的移动卡片；移动端布局不回归 |
| UI003 | 完成 | 移动端视觉系统与 Web Controller 对齐 | 375px、410px 与平板视口使用直角平面层级、清晰设备状态和移动触控交互；首页、设置、Sheet、文件与终端 chrome 风格一致且不改变连接语义 |
| WEB001 | 完成 | React 静态订阅 Landing 与 Control Plane Web API | 公开页面展示 Managed Free/Pro/Team 真实能力；价格未配置时不伪造金额；服务器不运行 Node Web 服务 |
| WEB002 | 完成 | 登录订阅付款纵向闭环 | 用户可登录、查看订阅、创建测试 Checkout，签名 webhook 幂等更新订单与 entitlement，Hub 收到新投影；生产支付未配置时拒绝 |
| WEB003 | 暂停 | 完整用户中心与联合登录 | GitHub/Google OIDC 或邮箱密码首次登录创建个人账号；用户可管理节点、账单订阅、账户设置和 AFF 奖励，首次有效付款幂等发放邀请人 +15 天、被邀请人 +7 天 |
| FILE001 | 完成 | 统一文件能力设计门禁 | 文件 owner、权限、方法、流控、失败语义和旧 API 迁移边界清晰 |
| FILE002 | 完成 | daemon 文件 metadata 与预览 | local protocol 可安全 list/stat/preview/mkdir/rename/delete/copy/move |
| FILE003 | 完成 | 文件上传下载数据流 | local 与 WebRTC 使用同一流协议完成背压、取消、续传和摘要校验 |
| FILE004 | 已完成 | 共享 UI 与 Official Android 闭环 | App 可浏览、预览、上传、下载并经 direct/single Relay 手测 |
| VT001 | 完成 | 收口 vterm restart 生命周期竞态 | 全量 core race 不再报告 Emulator close/read 竞态 |
| CLI001 | 完成 | CLI 命令体系设计门禁 | 当前命令缺口、对象化命令树、TerminalRef、输出/退出码与 tmux 能力映射形成唯一实现基线 |
| CLI002 | 暂停 | CLI 骨架与 local terminal 闭环 | 快捷键完整收口后重新排序 |
| CLI003 | 暂停 | daemon 与配置生命周期 | 快捷键完整收口后重新排序 |
| CLI004 | 暂停 | endpoint-aware CLI | 快捷键完整收口后重新排序 |
| CLI005 | 暂停 | CLI 自动化数据面 | 快捷键完整收口后重新排序 |
| CLI006 | 暂停 | file/workspace/pair/Cloud UX | 快捷键完整收口后重新排序 |
| CLI007 | 完成 | Cloud Companion 安装错误指引 | 未安装时明确 Cloud 为可选组件并提示安装命令；源码构建缺 release root 时提示换官方 termx；运行错误不重复 Usage |
| CLOUD013 | 完成 | 自包含 Cloud staging 测试套件 | `make build-cloud-test` 后无需 runtime.json、环境变量或 install，直接执行 `.artifacts/bin/termx cloud status` 和 `cloud login`；默认构建仍 fail closed |
| GA003 | 延后 | 双 Edge Relay Mesh corridor pilot | 仅在 CLOUD004 完成并有真实 corridor 数据后恢复 |
| GA004 | 延后 | 单 transit 受控加速 | 仅在 GA003 数据证明需要时恢复 |
| KS012 | 完成 | 快捷键最终审计与总契约 | 所有 binding/spec/handler/projection 被机器归类为符合或有 owner 的 debt，无未分类项且不能新增 debt |
| KS013 | 完成 | 中立 action domain 与分发 | tui/action 成为通用 action/invocation owner，shortcut 只拥有键位编译，keyboard 到 handler 无重复 identity/alias 表 |
| KS014 | 完成 | 输入协议与 scene 状态 | 传统 TTY/CSI-u/mouse 规范化、catalog 替换、sticky/copy/overlay/lock/Esc 优先级和 PTY 透传完整可验证 |
| KS015 | 完成 | 全部默认 action 真实闭环 | 每个默认 binding 都产生真实 reducer/effect/service 结果；placeholder 被实现或删除，失败不伪装成功 |
| KS016 | 完成 | 提示点击与键盘同源 | 所有 render surface 不含硬编码操作键位，keyboard/mouse/drag/click 使用 tui/action canonical invocation |
| KS017 | 完成 | 配置文档与真实终端验收 | 完整示例可加载，文档与 catalog 自动一致，全量 TUI/CLI/tmux/CSI-u 门禁通过且旧代码清理完成 |
| SI001 | 暂停 | TUI 同步输入组 | 恢复前重新确认范围 |
| OPEN001 | 延后 | 正式开源与发布隔离 | 用户明确进入发布阶段后再执行 public snapshot、许可证、secret audit 和新仓初始化 |

## 执行规则

1. 每轮先读取本文件、适用 `AGENTS.md` 并检查 `git status --short --branch`。
2. 只处理任务队列中最早的 `进行中` 或 `待开始` 切片；一次只做一个切片。
3. REC001 已按用户授权完成现有未提交改动审计；后续发现非本轮已识别改动时，仍不得覆盖未知用户工作。
4. 先补最小跨组件 harness，再接真实实现；不得用更多文档或抽象替代用户链路。
5. 切片完成后运行对应准入、更新本文件、使用中文提交信息提交，再进入下一切片。
6. 若发现 release-only、multi-region 或假设性优化工作，记录为 deferred，不得偏离当前纵向目标。
7. 外部 OAuth、生产 TLS、数据库和云资源缺失时，使用显式 dev/staging harness 推进；不得恢复旧 fallback。
8. KS012-KS017 是用户明确要求的双 Agent 审查切片；每个切片提交前必须按 `AGENTS.md` 同时完成架构 review 与代码 review，两个 reviewer 对阶段实现 diff 都明确 PASS 后，只允许机械回填 workflow 状态/审查证据并提交，才能进入下一切片。

## 测试准入

- REC001：`scripts/check_file_modes.sh`、`make doctor`、`make test-all`、`scripts/license-audit.sh`、`git diff --check`。不运行 public snapshot 独立构建或 public license audit。
- CLOUD001 文档-only：`git diff --check`。
- CLOUD002：受影响私有 module 测试、dev service 跨组件 harness、`git diff --check`。
- CLOUD003：Companion、remote、TUI 定向测试和 managed direct E2E harness、`git diff --check`。
- CLOUD004：Control Plane、Relay、Route Planner、remote 定向测试和真实 TURN E2E harness、`git diff --check`。
- CLOUD005：client workspace 测试、Community/Official Android 单测与 APK 构建边界、ADB 手测步骤审查、`git diff --check`。
- CLOUD006：受影响私有 module 测试、部署配置静态检查、远端 health/readiness、从本机或 `al` 发起的 managed direct/single Relay 定向 E2E、`git diff --check`。无法满足公网 TLS/UDP 前置条件时不得以 loopback 或 fake 冒充通过。
- CLOUD007：Companion manifest contract 测试、反向代理配置检查、从本机或 `al` 对公网地址执行 health/login/resolve/managed direct 定向验收、stable/default profile 拒绝测试、`git diff --check`。
- CLOUD008：Official public/loopback/default build contract 单测、Community/Official APK 构建边界、ADB 安装与 logcat、Wi-Fi/移动网络 direct、terminal List/Attach/Input/Output、后台恢复、`git diff --check`。
- CLOUD009：Hub/Companion/devcloud 定向测试、授权 revision/过期/cache miss harness、真实 HTTP direct E2E（Control Plane listener 关闭后新连接仍成功）、`git diff --check`。
- CLOUD010：Hub/Relay/Control Plane 定向测试、预算过期/并发/撤销、durable outbox 重启与幂等补报、真实 TURN E2E、`git diff --check`。
- CLOUD011：Companion/Official Android contract 测试、desktop direct/single Relay E2E、ADB 真机 Control Plane 中断验收、`git diff --check`。
- CLOUD012：Web 账号设备码审批、过期/重放/跨账号拒绝、账号 daemon enrollment、Companion CLI 轮询和 Official Android native contract 定向测试；React/Vite typecheck/build、staging 部署、Web 注册后 TUI direct/single Relay E2E、ADB 在线时 App 登录与 direct/single Relay 真机验收、`git diff --check`。ADB 不在线时不得把 App 真机链路标记完成。
- UI001：共享 UI machine/card 定向测试、client workspace 测试、Android source sync、移动 viewport 截图检查、`git diff --check`。
- UI002：共享 UI 首页定向测试、client workspace 测试、Android source sync、桌面与移动双 viewport 截图检查、`git diff --check`。
- UI003：共享 UI 首页/设置/Sheet/工作区定向测试、client workspace 测试、Android source sync、375x812、410x913、平板与宽屏截图检查、reduced-motion 静态检查、`git diff --check`；ADB 在线时补 Official Android WebView CDP 真机验收。
- WEB001：Web Controller Go module 测试、React/Vite typecheck/build、Control Plane HTTP harness、桌面与移动截图检查、staging 配置静态检查、`git diff --check`。
- WEB002：Web Controller/devcloud/Control Plane/Hub 定向测试、Session/CSRF/webhook 签名与幂等 harness、React/Vite typecheck/build、跨进程 login/checkout/account E2E、桌面与移动截图、`git diff --check`。
- WEB003：OIDC state/nonce/PKCE/callback、identity collision、密码摘要/修改、Session/CSRF、节点 ownership/revoke、AFF 单次归因、首次付款奖励幂等和订阅延期、账单/审计/SQLite 重启恢复定向测试；React/Vite typecheck/build、跨进程用户中心 E2E、桌面与移动截图、`git diff --check`。
- FILE001 文档-only：`git diff --check`。
- FILE002：protocol/core 定向测试、文件系统 sandbox harness、`git diff --check`。
- FILE003：protocol/core/remote 定向测试、慢消费者/取消/续传/损坏数据 harness、`git diff --check`。
- FILE004：client workspace 测试、Community/Official Android 单测与 APK 构建边界、direct/single Relay 文件 E2E、ADB 手测、`git diff --check`。
- VT001：vterm 定向并发 harness、core restart 定向 race、全量 `go test -race ./core`、`git diff --check`。
- CLI001 文档-only：`git diff --check`。
- CLI002：CLI command/help/golden/exit-code 黑盒测试、真实 local daemon terminal lifecycle harness、默认依赖守卫、`git diff --check`。
- CLI003：daemon service ownership/start-stop-status 黑盒测试、config parser/writer 定向测试、macOS/Linux 可用平台 harness、`git diff --check`。
- CLI004：TerminalRef/registry/dialer 定向测试、local/SSH/managed direct/single Relay CLI E2E、失败不 fallback harness、`git diff --check`。
- CLI005：input/live/history/events 定向测试、stdout/stderr/NDJSON/timeout 黑盒测试、local 与 WebRTC 自动化 E2E、`git diff --check`。
- CLI006：file/workbench/pair 定向测试、Companion 自动发现与 daemon enrollment IPC 测试、Cloud staging login/enroll E2E、secret scan、`git diff --check`。
- CLI007：Cloud 命令错误投影黑盒测试、release root/Companion missing 定向测试、`GOWORK=off go test ./cmd/termx -count=1`、重建二进制验证、`git diff --check`。
- CLOUD013：Companion embedded manifest 解析/默认 fail-closed、同目录路径/权限/SHA 复验、activation/status 定向测试；`GOWORK=off go test ./cmd/termx -count=1`；`cd private/cloud/companion && GOWORK=off go test ./... -count=1`；`make build-cloud-test`；隔离运行目录执行真实 `.artifacts/bin/termx cloud status --json`；`git diff --check`。
- KS012：debt manifest 分类完整性/不新增守卫、shortcut/domain/input/app/render/config 定向测试、`go test ./tui/... -count=1`、双 Agent PASS、`git diff --check`。
- KS013：`tui/action` canonical identity/invocation、shortcut 引用、keyboard handler 与 render metadata contract 定向测试、`go test ./tui/... -count=1`、双 Agent PASS、`git diff --check`。
- KS014：TerminalHost raw/CSI-u/mouse parser、key canonicalization、catalog replacement、scene/lock/back/PTY passthrough 定向测试、`go test ./tui/... -count=1`、双 Agent PASS、`git diff --check`。
- KS015：所有默认 scene/binding 的 reducer/effect/service 矩阵、endpoint-aware 失败 harness、`go test ./tui/... -count=1`、双 Agent PASS、`git diff --check`。
- KS016：footer/help/overlay/chrome/content projection、keyboard/click 等价、空 catalog/窄屏/capability 条件测试、`go test ./tui/... -count=1`、双 Agent PASS、`git diff --check`。
- KS017：配置示例加载与文档一致性守卫；`scripts/with-clean-termx-env.sh env GOWORK=off go test ./tui/... -count=1`；`scripts/with-clean-termx-env.sh env GOWORK=off go test -race ./tui/... -count=1`；`scripts/with-clean-termx-env.sh env GOWORK=off go test ./tui/terminalhost ./tui/input ./tui/app -run 'Test.*(CSIU|Kitty|Shortcut|BackNavigation|Footer|Overlay)' -count=20`；`scripts/with-clean-termx-env.sh env GOWORK=off go test ./cmd/termx -count=1`；`scripts/termx_shortcut_smoke.sh`；双 Agent PASS；`git diff --check`。
- 只有切片真实跨越全仓 contract 时才运行 `make test-all`；当前开发阶段不运行 public snapshot 或 public-only release gate。

## 当前状态

- CLOUD013 已完成：`make build-cloud-test` 先生成内嵌 `staging-public-http` manifest 的 development `termx-cloud`，再把其固定文件名、版本、通道与 SHA-256 固化进同目录 `termx`；运行时复验真实 executable 目录、regular file、当前用户 ownership、不可 group/world writable、可执行位与摘要，失败不 fallback。Companion 对 embedded manifest 复用严格 schema/profile/address parser，并由唯一 runtime 装配同时产生 Control Plane/Hub adapter 与 HTTP 登录 URL 策略，禁止 runtime manifest 覆盖；installer smoke 始终使用 unconfigured adapter，默认无 embedded metadata 的 source/official build 继续走原签名 release root 门禁。CLI/Companion 全量测试、`go vet`、`make build-cloud-test`、隔离和默认 `cloud status` 均通过；真实 `.artifacts/bin/termx cloud login` 无额外配置即可返回 `http://114.66.58.243:41100/device` 验证地址和用户码并进入审批轮询。

- KS012 已完成：机器清单固定 203 个默认 shortcut entry、166 个 routed binding、146 个 canonical spec 和 123 个 render projection；203 个 entry 均按真实 routed/overlay 路径验证 handler。生产 `InputEvent`/`HitRegion` producer、全部 80 个 `withFooter` 键和显式 `Key`、非结构化 render 字符串均由 manifest、源码锚点、逐组 digest 与独立 SHA 闭集守卫覆盖。定向 `go test ./tui/{shortcut,input,app,render,config,state,terminalhost} -count=1` 和 clean-env `go test ./tui/... -count=1` 通过；架构 reviewer `ks012_arch_review` 与代码 reviewer `ks012_code_review` 最终均明确 PASS，`git diff --check` 通过。

- KS013 已完成：`tui/action` 以 159 个 canonical spec 统一 keyboard、mouse、drag 与 CTA identity/invocation，`tui/shortcut` 独占 15 个内置 scene 和 203 个默认 binding；全部默认 keyboard invocation 进入 app handler registry，overlay 不再经过 render fallback。render 的 123 个 `ProjectionID` 与 action identity 分离，并通过 `CanonicalActionID` 显式引用 canonical spec；遗留 footer/help metadata 与 surface bridge 继续由 debt manifest 锁定到 KS016。clean-env `go test ./tui/... -count=1` 通过；架构 reviewer `ks013_arch_review` 与代码 reviewer `ks013_code_review` 最终均明确 PASS，`git diff --check` 通过。

- KS014 已完成：`TerminalHost` 成为传统 TTY、Kitty CSI-u、SGR mouse、OSC 与 bracketed paste 的唯一 raw 分帧 owner；Esc 歧义由 25ms host 窗口收口，非法 host protocol、未建模 PUA 和不支持的 modifier 不进入 shortcut 或 PTY。catalog 按传统 canonical event 标记增强键盘依赖，scene/menu 只经 shortcut registry 和唯一 input mode 投影；overlay、copy、sticky、lock、双前缀与 back navigation 按 reducer-owned state 决定优先级。Paste 跨 chunk 原子化，Prompt 作为一次文本编辑消费；普通 terminal router 按 endpoint-aware owning surface 的 `BracketedPaste` mode 使用唯一 encoder。clean-env `go test ./tui/... -count=1`、`go test -race ./tui/terminalhost ./tui/input -count=1` 与 parser/Kitty/SGR/paste/prompt/shortcut/back-navigation 定向测试 `-count=20` 通过；架构 reviewer `ks014_arch_review` 与代码 reviewer `ks014_code_review` 最终均明确 PASS，`git diff --check` 通过。

- KS015 已完成：203 条默认 binding canonicalize 为 146 个 invocation，并经正式 reducer 组合执行同步 effect/message 链直到静止；排除 `Generation` 和普通 toast 后，每项必须产生真实状态变化或抵达 terminal/core/clipboard/workbench storage owner。terminal mutation 以完整 operation vector 和精确 `TerminalRef` 列表验收，能够拒绝同 endpoint 错 terminal、重复/额外 mutation 与 fallback；owner/resize/size-lock、history latest/older/newer/oldest、clipboard/storage、split/tab/floating attach 的局部失败均有 harness。`action.command` 使用 canonical parser/dispatcher，overlay-only action fail closed；picker edit 使用 unfiltered reducer-owned pool metadata。定向 KS015 测试 `-count=20`、clean-env `go test ./tui/... -count=1` 和 `git diff --check` 通过；架构 reviewer `ks015_arch_review` 与代码 reviewer `ks015_code_review` 最终均明确 PASS。额外的 `go test -race ./tui/app -count=1` 暴露既有 live invalidation 测试夹具对 `FakeTerminalService.LiveInvalidationRequests` 的并发读写，未经过 KS015 变更链路，保留给已强制要求全量 race 的 KS017 收口。

- KS016 已完成：footer/help/overlay/header/chrome/content CTA 与 drag 全部携带 `tui/action` canonical Invocation，app 不再解析 render `ProjectionID`/`ActionID` 执行业务；旧 `ShellContentActionMsg` dispatcher、固定 footer/help 元数据和 87 个无生产消费者 projection 已删除，render 只保留 34 个真实视觉/几何投影。每个 actionable HitRegion producer 显式声明 active/explicit `HitTargetMode`，row target 另用 `HasRow` 表达存在性并按当前 reducer-owned projection 验证范围和 picker row kind；缺失、越界、错类 target 在 specialized/generic 分流前 fail closed，footer active-target 与 empty-tab no-pane 入口保持合法。硬编码 `Ctrl-F`/`Ctrl-T`/`R restart` 文案和隐式 `r/R` 已删除；clean-env `go test ./tui/... -count=1` 与 `git diff --check` 通过，架构 reviewer `ks016_arch_review` 和代码 reviewer `ks016_code_review` 最终均明确 PASS。

- KS017 已完成：两个可直接加载的配置示例明确 empty map、action-only 与显式 scene 完整替换语义，README 补齐支持键位、组合修饰键、canonical 冲突、增强键盘前置条件与诊断；运行 catalog 统计、关键文档 contract 和旧执行符号均有自动守卫。真实 smoke 使用隔离 daemon/config/state/log 和独立 tmux socket，完整验证默认 root/sticky/overlay/copy/quit 以及 CSI-u capability、`Ctrl-1` 和正常退出链路；异步 live invalidation fake 改为 mutex-owned snapshot 后全量 race 通过。最终 completion audit 进一步删除 inventory 中遗留的 KS001 手写逐键表，保留机器 manifest、truth owner、消息链路和自动统计，并新增禁止 Markdown 逐键表回归的守卫。clean-env 全量 TUI、race、shortcut/CSI-u 定向 `-count=20`、CLI、`scripts/termx_shortcut_smoke.sh` 与 `git diff --check` 全部通过，架构 reviewer `ks017_arch_review` 和代码 reviewer `ks017_code_review` 对原实现及 completion audit 修正最终均明确 PASS。

- CLI007 已完成：Cloud 命令运行错误统一由子命令边界投影，参数错误仍保留 Cobra Usage，运行错误只返回一次稳定错误。官方构建缺少 Companion 时返回 `COMPANION_MISSING`，明确它是默认不捆绑的可选组件并提示 `termx cloud install`；源码构建缺少官方 release root 时返回 `COMPANION_UNTRUSTED`，明确必须先换官方 `termx`，不再给出无法执行的直接安装建议。`GOWORK=off go test ./cmd/termx -count=1`、`go vet ./cmd/termx`、`make build`、真实 `.artifacts/bin/termx cloud status --json` 单行错误/退出码/无 Usage 验证和 `git diff --check` 均通过。

- 快捷键最终收口已规划为 KS012-KS017，目标不是补齐零散按键，而是删除第二真值并证明完整消息链路。当前审计已确认 `tui/shortcut` 之外仍有 `render.ActionSpecCatalog` 等 action 描述面、内容区硬编码 `Ctrl-F`/`Ctrl-T`/`R restart` 操作提示，以及 `system.open_prompt` 落到 placeholder 的风险。中立 `tui/action` 将拥有 keyboard/mouse/drag/CTA 共用 action identity 与 invocation，`tui/shortcut` 只拥有 scene+key 编译和快捷键展示覆盖；每个默认 shortcut 后续必须真实工作或从 catalog 删除。KS012 先用 debt manifest 保证无未分类/无新增 gap，KS013-KS016 再按 owner 消除。允许在切片范围内大规模删除被替代旧代码，不以改动行数为约束。所有阶段执行架构 reviewer + 代码 reviewer 双门禁，规划用 `/goal` prompt 位于 `tui/docs/shortcut-completion-goal-prompt.md`。

- 快捷键规划阶段审查证据：架构 reviewer `shortcut_plan_arch_review` 与代码 reviewer `shortcut_plan_code_review` 已对最终规划明确 PASS。已处理 findings 包括 KS012 审计/全绿矛盾、中立 action owner、KS013/KS016 surface 边界、KS015 scope、CTA/shortcut label ownership、双审查元数据终止规则和 KS017 可复现命令。clean-env `go test ./tui/... -count=1` 当前九个 package 全绿，`git diff --check` 通过。

- CLI001 已完成：当前顶层 `new/ls/attach/kill/rm` 缺少对象层级与 help，`--socket` 只覆盖 local，裸 TerminalID 无法表达跨 endpoint 真值，公开 `v3` 同时泄漏重复命令和 smoke harness，查询输出也没有 JSON/format/退出码 contract。新基线以 `terminal`、`endpoint`、`daemon`、`workspace`、`file`、`pair`、`cloud`、`config` 为 canonical namespace，保留五个高频短别名但共享 handler；target 使用 `EndpointID:TerminalID`，自动化分 CLI002-CLI006 纵向实现。tmux 只映射 send/capture/wait/format 等成熟控制能力，不把 session/window/pane 模型覆盖到 TermX terminal/workspace ownership。

- `114.66.58.243` 当前按用户要求只承担服务端 Cloud 角色：`termx-staging-cloud` 与 Nginx active，Control Plane/Hub/Relay 和 Web Controller 公网健康；`termx-staging-daemon`、`termx-staging-daemon-companion` 已停止并 disabled，root 自动启动的 local daemon 也已停止。账号数据库和既有服务文件保留，本地或其他机器作为 daemon/client 继续测试。

- CLOUD012 启动审计确认：此前 Web 邮箱账号虽持久化在 SQLite，但 Control Plane `/v1/login/*`、daemon enrollment 与 Official Android gateway 固定签发 `account-dev-local`，共享 App 设置页仍使用 archive 前的 `/api/v1/auth/*` bearer 假设，因此“Web 注册”和“TUI/App 云连接”并非同一账号真值。本切片据此选择浏览器审批设备码、账号绑定 enrollment 和 Hub 本地授权投影，不把密码、Cookie 或 terminal capability 下放到客户端。

- CLOUD012 已完成：Control Plane 提供浏览器 Session 保护的设备码检查/审批和账号专属一次性 daemon enrollment code；TUI 按服务端 interval 轮询审批，Official Android 通过 native 私有 gateway 打开系统浏览器登录，edge token 只进入 Keystore，旧 WebView `/api/v1/auth/*` 登录在 Official 装配中不再使用。`114.66.58.243` 已替换 Cloud、Companion、termx 与 React 静态产物，验收时三个 systemd unit 均为 active；公网现场验证新邮箱注册后审批 TUI 登录返回同一 AccountID，注册账号签发的一次性 enrollment code 把 daemon、Hub device projection 和 Web 节点投影绑定到同一账号，重放被拒绝。Official Android 真机 `24129PN74C` 已通过系统浏览器审批设备码、Keystore Session 恢复、账号名下 pairing、`connected/direct` 和显式 `connected/single_relay`；只阻断手机到 Control Plane `41101/tcp` 后，direct 与 single Relay 均能重新建连，窗口内只访问 Hub resolve/signaling/lease 路径。强停并重建 App 进程后账号 Session 与 pairing 仍恢复，在 Control Plane 继续中断时 direct 再次成功；重复强停造成的短时 Relay allocation 最终触发 Managed Free `quota_exhausted`，按委派预算 fail closed，没有伪造 Relay 成功或回退其他路径。

- WEB003 运行架构已收敛：React/Vite 只生成静态文件，Nginx 在 `41100` 直接托管并将 `/api/*` 同源代理到 Control Plane `41001`；Control Plane 直接拥有 HttpOnly Session Cookie、Origin/CSRF、账号数据库、订阅和 AFF API。Next App Router、Route Handler、Node runtime unit、独立 Go Web Controller BFF binary/unit 与 `41000/41004` listener 已删除，不保留 fallback。

- WEB003 当前进展：Web Controller 已以 SQLite 持久化账号、bcrypt 密码身份、浏览器 Session、订单、payment event 幂等、节点、AFF 单次归因、双边奖励和审计；邮箱注册可消费 `?aff=`，首次有效 Pro 付款由签名 webhook 唯一触发邀请人 +15 天、被邀请人 +7 天，并把奖励计入订阅有效期与 Control Plane entitlement 发布。Account 可按凭据状态设置或修改密码，原团队成员 Invitations UI/API 已删除并替换为 Referrals。React/Vite 表现层已统一为 TypeScript、Tailwind CSS v4 与本地 shadcn/ui 源码组件，Light/Dark 主题保持直角、细线和中性配色，旧 `globals.css`/`controller.css` 页面级样式已删除；Landing、登录和完整用户中心通过 `i18next`/`react-i18next` 提供英文、简体中文和俄语，语言选择在浏览器持久化，动态日期、价格、订单状态和无障碍标签跟随当前语言。数据库关闭重开后的登录、Session、订单和奖励恢复已有 harness；GitHub/Google OIDC callback 仍未实现，因此 WEB003 保持进行中。

- WEB002 迁移前验收记录：Go BFF 使用随机 bearer 摘要保存 8 小时浏览器 session，Next 只在 HttpOnly/SameSite=Strict Cookie 中持有 bearer，写请求同时要求精确 Origin 与 CSRF double submit token。Checkout 只创建 pending order；显式 staging provider 生成 HMAC `payment.succeeded`，EventID 幂等且 account/plan/order 必须绑定，只有 Control Plane internal entitlement update 成功并递增 edge revision、重新签发并应用 Hub snapshot 后订单才提交 paid。Pro snapshot 把 staging Relay budget 从 64 MiB/2 concurrency 更新到 256 MiB/4，terminal grant 不参与。`/login`、`/account`、订单列表和测试付款 UI 已完成桌面/Pixel 7 验收。Go Web Controller/devcloud/Hub test、vet、Next typecheck/build、本地跨进程和 `114.66.58.243:41100` 公网 E2E 均通过；公网从 Managed Free 完成 Pro paid，缺 Origin checkout 为 403，Control Plane/Hub ready 且五个 unit active。生产 OAuth、价格、持久订单数据库和真实 payment provider 仍 fail closed，不以 staging provider 冒充生产付款。
- WEB001 迁移前验收记录：`private/cloud/web-controller/web` 使用 Next.js 16 App Router 提供公开订阅 Landing，真实 TermX 机器工作台截图作为首屏产品信号，Managed Free/Pro/Team 只展示 PRD 已确认能力。价格目录由独立 `plans.json` 配置，Go BFF 严格拒绝 contact/included 套餐携带金额；Next `/` 与 `/api/catalog` 运行时只经 loopback BFF 读取同一真值，未配置价格显示用户态 Preview/Contact 文案。standalone 构建装配已修复重复构建静态目录污染，并以真实 Go `42104` + Next `42100` 双进程验证 Landing、catalog 和上游失败 503；1440x900 与 Pixel 7 截图无重叠。`114.66.58.243` 已安装校验过的私有 Node 24 LTS、Linux/amd64 BFF 与 Next standalone，staging unit 将 Next `41000` 与 BFF `41004` 分离，Nginx 仍只公开 `41100`；公网 Landing、catalog、status 均为 200，Control Plane/Hub ready 且五个 unit active。Go test/vet、Next typecheck/build、artifact 检查与 `git diff --check` 通过；生产 npm audit 无 high/critical，Next 16.2.10 内嵌 PostCSS 保留 2 个暂无同代升级修复的 moderate。仓库既有 `repository-layout-guard.sh` expected module 列表遗漏已存在的 `private/cloud/devcloud/go.mod`，因此该非 WEB001 guard 仍失败，未在本切片扩散修复。
- UI002 已完成：共享首页在 `lg` 宽屏下使用 64px 产品栏、带文字的 Add machine 主操作和 Machine/Access/Connection 稳定列表格，机器行收敛为 72px、去除移动卡片阴影和大圆角；移动端继续使用三行卡片和 40px 触控操作。1440x900 桌面截图验证长名称、Local/Cloud/Local + Cloud、可达性与操作列无重叠，Pixel 7 截图验证移动布局未回归；`make test-clients`（63 个文件、452 条测试）、Android source sync 与 `git diff --check` 全绿。
- UI003 已完成：共享 App shell、首页机器列表、设置、配对/操作 Sheet、文件管理/预览、传输中心、终端列表和移动终端 chrome 已统一为直角、细边框、冷灰背景与蓝色主操作；机器网络遮罩、终端 attach、文件会话和预览 loading 统一使用缺边方形 spinner，内部终端单屏/分屏与工具栏不再使用悬浮圆角画布。移动终端顶栏把分屏、resize、连接信息、文件和终端工具收进 44px 入口的底部工具面板，终端画布、连接状态、endpoint/transport owner 与 terminal/file truth 未改变。375x812、410x913、768x1024 和 1440x900 截图无重叠，reduced-motion 可关闭动画；`make test-clients`（63 个文件、453 条测试）、Android source sync 与 `git diff --check` 通过。`24129PN74C` 上 WebView CDP 验证 CSS viewport 为 410x913、横向溢出为 0、首页/设置面板圆角均为 0px，真机截图确认状态栏安全区和触控布局正常；视觉验收中一度误装默认 Official APK，导致保留的 Public staging 记录按预期 `login_required`，随后已用 `termxOfficialPublicHTTPStaging=true` 重新构建并覆盖安装，设备拔出前未冒充完成 P2P/Relay 复测。额外执行 `make test-android` 时 Community 构建/单测成功，Official APK 构建并通过 class boundary，但既有 `ManagedPathQualityTest.reporterOnlySubmitsQualityWindows` 在 `runBlocking` 永久等待，线程栈确认后终止；该非 UI003 测试问题保持 deferred，不冒充全量 Android 门禁通过。
- UI001 已完成：机器 store 持久化 Local、Cloud、Local + Cloud 接入类别，账号同步/退出按能力正确合并和降级；首页卡片显示真实 health 可达性、授权状态、终端数，以及已存在会话的连接阶段和 local/P2P direct/single Relay 路径，列表不会为了取状态主动建连。共享 UI 定向测试、`make test-clients`（63 个文件、451 条测试）、Android source sync、Pixel 7 viewport 截图与 `git diff --check` 通过。设备重连后已覆盖安装 Official public HTTP staging APK，并用 WebView CDP 验证 410x913 viewport 无横向溢出；真机暴露旧 Official `source=manual` 记录缺少新类别字段会误标 Local，已按 pairing ownership 迁移为 Cloud 并补回归测试，CDP 最终显示 `Cloud` 与 `Cloud available`。
- RM001-RM003 已提交：公开 Go module、npm workspace 和 Android 单一源码已经收口。
- RM004 原未提交改动已由 REC001 审计接管：`private/cloud` 路径迁移、canonical Make 入口、`.artifacts`、doctor/layout/generated guard、文档归档和原有 tmux 冷启动诊断已经收口并通过 REC001 全部准入。
- RP002-RP007、GA001/GA001A/GA002 已建立 contract、领域组件和 harness；这些成果是 CLOUD002-CLOUD005 的输入，不代表 managed cloud 已可用。
- CLOUD001 已完成：活动 roadmap 明确 direct、single Relay 与 Android 的顺序和用户 DoD。
- CLOUD002 已完成：PresenceSession/ManagedSession 已分离；fresh proof、账号/设备 session、resolve、Hub admission、answer/failure signaling、局部失败和 backpressure 已通过真实 Control Plane/Hub listener 纵向 harness；`make cloud-dev` 可生成显式 dev-local manifest。
- CLOUD003 已完成：`termx daemon --cloud` 使用 fresh proof 建立 presence；public pairing create/import 分离 raw grant 与 endpoint registry；TUI 经真实 Companion IPC、Control Plane/Hub listener、Pion DTLS DataChannel、capability handshake 和 core-v2 protocol 完成 List/Attach/Input/Resize/Live/History，并投影连接 phase 与实际 `direct` path。race E2E 证明云边界看不到 grant、设备私钥或 terminal payload，远端 daemon 关闭不影响 local endpoint。
- CLOUD004 已完成：`make cloud-dev` 装配一个 lease-bound Pion UDP TURN；client/daemon 通过同一 ManagedSession 获取不同短期凭据，TUI 在真实 `single_relay` path 完成 List/Attach/Input/Live/History；Authority/Control Plane 验证并发、quota、到期与 signed idempotent usage，race E2E 证明 Relay 停止后不回退 direct 且 local endpoint 仍可用。
- CLOUD005 已完成：Official dev gateway、真实 DTLS/capability auth、Keystore pairing、单一 `protocol` DataChannel、core-v2 live screen 和 fresh-proof presence 续约已接通；真机 List/Attach/Input/Output、2 秒/10 秒恢复、Hub 局部失败和 Community `companion_missing` 均已通过，准入全绿。Community 验收后设备物理断开，重连后只需恢复安装 Official dev APK，不影响切片完成度。
- FILE001 已完成：统一规范明确 daemon 文件系统 truth、显式四类文件权限、metadata 方法、单 protocol DataChannel 流、背压/续传/摘要失败语义和旧 `/files/*`/独立 file channel 删除路线；UI/schema 存量不再冒充可用能力。
- FILE002 已完成：公开 wire/typed client 已提供 `file.list/stat/preview/mkdir/rename/delete/copy/move`；core 以 daemon OS 文件系统为 truth，使用绝对路径、lstat symlink 语义、有界预览、opaque stale cursor、显式 overwrite 和逐项 mutation 结果。local listener 显式拥有文件权限，terminal-scoped/缺权限 session fail closed；protocol/core harness 与 generated-code gate 全绿，未接旧 `/files/*` UI。
- FILE003 已完成：protocol v4 在单一 transport 内由 control method 分配 session-local transfer channel，64 KiB chunk 与 256 KiB ACK window 提供显式背压；下载固定 size/mtime identity 并返回全文件 SHA-256，上传使用 daemon-owned temp、连续 offset、finish digest 和原子 rename。上传可跨 protocol session 续传 15 分钟并绑定 local principal 或 signed GrantID，其他 grant 不能 resume/cancel；cancel 幂等清理。local 慢消费者/control 隔离、断线续传、损坏摘要、stale source、principal isolation 和 Pion direct 文件下载 harness 全绿；文件专属 core/remote race 全绿。全量 core race 仍被既有 vterm restart/drain race 阻断，栈不经过 FILE003。
- FILE004 已完成：共享 UI 与 Official Android 已统一到 typed `file.*` 和 protocol v4 stream；旧 `/files/*`、`openFileTransfer`、独立 file DataChannel 与旧 task id 已删除。Android native 保留 picker、MediaStore、SQLite 与后台线程，在单一 authenticated protocol DataChannel 内完成上传、下载、取消、后台与进程中断恢复；core 允许同 principal 的新 session 串行接管旧上传 channel，仍拒绝不同 principal。公网真机 direct 完成浏览、预览、2 MiB 下载、3 MiB 上传、双向取消、64 MiB 双向续传和两端 SHA-256；single Relay 真实 TURN/DataChannel harness 通过，Android dev Relay 因远端 loopback/ADB 无 UDP 转发未冒充真机 Relay。`make test-clients`、`make test-android`、generated guard、文件协议 Go tests、FILE004 race 与 `git diff --check` 全绿；全量 core race 仍存在既有 vterm restart 竞态，记录为非 FILE004 剩余风险。
- VT001 已完成：`Emulator.closed` 改为原子生命周期真值，response drain 可与 restart `Close` 并发且由 pipe close 正常唤醒；并发 Close/Read harness 连续 race、vterm 全量 race、core restart 定向 race 与全量 `go test -race ./core` 均通过，未改变 screen/history 或 restart 产品语义。
- CLOUD006 已完成：用户授权清除服务器原有 legacy TermX 与 FILE004 devstack；新 `termx-staging-cloud`、`termx-staging-web-controller`、`termx-staging-daemon-companion`、`termx-staging-daemon` 四个 unit 已在 `114.66.58.243` 独立运行。Control Plane/Hub/Web Controller 仅绑定 loopback 并经 SSH tunnel 访问，lease-bound TURN 独占 `41003/udp`；Companion 使用 headless GNOME Keyring 与 systemd credential。本机真实 TUI 的 direct 与显式 `relay_only` 均完成 resolving/signaling/connecting/authorizing/connected，运维、bootstrap 与清理步骤已落档。当前 picker 未投影 observed `single_relay` 文本，packet/usage/path 自动化证据留作后续观测切片，不能据此启动 GA003。
- CLOUD007 已完成：按用户明确授权，Nginx 在 `41100/41101/41102` 将 Web Controller、Control Plane 与 Hub loopback owner 暴露为无隧道公网 HTTP staging；`41100/runtime.json` 公开显式 `staging-public-http` development manifest，但不公开有效 enrollment code 或 pairing grant。本机 development Companion 直接经公网完成 login、resolve、Hub signaling 和真实 TUI resolving/signaling/connecting/authorizing/connected，SSH 未参与运行链路。该 profile 只允许固定测试账号、短期 session 和内存 store；默认 `dev-local`/`staging-ssh`、stable build 仍拒绝公网明文。上线前 HTTPS/TLS 仍是独立强制门禁。
- CLOUD008 已完成：Official Android 增加互斥、显式的 `termxOfficialPublicHTTPStaging` debug build，默认 Official、Community 和原 loopback profile 边界不变。`24129PN74C` 真机在无 ADB reverse、5G 网络下完成公网 pairing、managed direct、List/Attach/Input/Output 和 8 秒后台恢复；direct 为 `prflx / host`，RTT 约 51-64 ms。真机同时暴露并修复 `Use relay` 未把 `forceRelay` 下沉 native，以及 daemon/同机 TURN 双端 relay-only 导致 answer 无 candidate 的问题；Answerer 验证 offer 只能含 relay candidate 后显式发布 daemon host candidate，最终观测 `Mode=Relay`、`Path=single_relay`、`Candidates=relay / host`、RTT 49 ms。服务器 `41003/udp` 双向正常，先前安全组阻断判断已撤销。
- CLOUD009 已完成：Control Plane 登录/enrollment 签发带 client/daemon principal、Hub audience、auth epoch 和 expiry 的 edge credential；Hub 以签名完整 policy snapshot、严格 revision、内存 projection 和原子文件 store 为授权真值，重启恢复时重新验签。Companion managed offer/answer contract 已删除逐连接 `AcquireClientAdmission`/`AcquireDaemonAnswerAdmission` 和对应 HTTP 路由；Hub 本地创建 direct EdgeManagedSession，daemon answer 绑定 active target presence。真实 HTTP harness 关闭 Control Plane listener 后仍新建并完成 direct，Hub/Companion/devcloud 全量测试、Hub race、direct/vertical race 与文档准入全绿。`relay_only` 在 CLOUD010 完成前仍显式携带原 Control Plane lease correlation ID，不影响 direct 热路径，也不得成为最终 Relay ownership。
- CLOUD010 已完成：`AcquireRelayLease` 从 ControlPlaneAdapter/Control Plane HTTP 路由迁到 HubAdapter/Hub edge endpoint；client/daemon edge principal 与本地 target policy 决定准入。签名 policy snapshot 携带 single Relay enable、TTL、bytes、bitrate 和 concurrency 预算，Hub 使用独立 regional key 在预算内签 lease，Relay 只信任该 key。真实 TURN E2E 在 Control Plane server 关闭后完成 List/Attach/Input/History 和 `single_relay`，预算耗尽/过期释放有 harness。signed usage event 连同原始 signed lease 先写入原子 durable outbox，重启后无需内存 session map 即可重新验 lease、幂等结算并 ack；devcloud/Relay 全量 race、Control Plane/Companion 全量测试与文档准入全绿。
- CLOUD011 已完成：Control Plane 登录/enrollment 返回的签名 edge credential 绑定 Hub ID、URL、region 和 directory version；Companion v2 secret session 与 Official Android 独立 Android Keystore AES-GCM store 缓存该目录并拒绝 Hub 变更或版本回滚。endpoint resolve 已从 Control Plane 移到 Hub，Android resolve、Relay lease 和 signaling 均只使用 bearer edge credential 访问 Hub；contract 测试覆盖进程重建后关闭 Control Plane。`114.66.58.243` 公网 desktop direct 为 `connected/direct`、`relay_only` 为 `connected/single`。ADB 真机 `24129PN74C` 在仅阻断自身到 `41101/tcp` 后强停并重建 App 进程，新 direct 为 `connected/direct`、`prflx/host`；随后 single Relay 为 `connected/single_relay`、`relay/host`、RTT 62 ms。中断窗口 Nginx 只有 Hub resolve/lease/signaling，无 login 或 admission；临时防火墙规则已删除。Companion/Control Plane/devcloud race、`make test-android` 与文档准入全绿。
- 正式开源隔离、生产 OAuth/TLS、持久化数据库、计费、团队治理、Relay Mesh 和多区域运维全部延后。
