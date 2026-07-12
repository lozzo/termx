# 工作流：单区域公网 Cloud staging 纵向闭环

## 当前目标

- REC001 已完成仓库可控状态恢复：尚未提交的 RM004 目录与维护改动已经审计收口，误触发的发布期验证扩展已经清理。
- CLOUD001-CLOUD005 已完成单区域 Cloud 纵向闭环：开发云、桌面 managed direct、single Relay 与 Official Android 均已跨真实用户链路验收。
- CLOUD009-CLOUD010 已完成 managed direct 与 single Relay 的 Hub 本地授权热路径；连接阶段不再领取 managed admission 或从 Control Plane 获取 RelayLease。CLOUD011 已接通 desktop/Official Android 启动凭据、HubDirectory 缓存和 Hub-only 连接 contract，当前只剩 ADB 真机 Control Plane 中断验收。
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
- FILE001：`workflow.md`、`docs/remote-platform/`；只建立文件产品、权限、协议、流控、失败语义和迁移基线，不新增 runtime。
- FILE002：`proto/wirepb/`、`internal/protocol/`、`core/` 与必要 protocol/client harness；实现 daemon-owned 文件 metadata/read-preview 操作和显式 capability scope，不触及 Cloud 服务。
- FILE003：`proto/wirepb/`、`internal/protocol/`、`core/`、`remote/`、`shared/remoteauth/`、`cmd/termx/` 与必要 transport harness；实现同一 protocol session 内的流式上传下载、背压、取消、续传和完整性校验，并最小联动 pairing grant 的显式文件权限；不新增旧独立 DataChannel。
- FILE004：`clients/ui/`、`clients/mobile/`、必要公开 protocol adapter、Android 构建文件和手测文档；删除旧 `/files/*` 与 legacy file channel 依赖，完成 Official Android 真机和 direct/single Relay 验收。只有真实产品链路证明 contract 缺失时才最小联动 FILE002/FILE003 owner。
- VT001：`vterm/internal/vt/`、`vterm/vterm/`、`core/` restart harness 与 `workflow.md`；只修 Emulator close/read 生命周期竞态，不改变 terminal semantic transaction、screen/history truth 或 restart 产品语义。
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
| CLOUD011 | 进行中 | 客户端启动凭据与 Hub 目录刷新 | desktop/Official Android 启动时可访问 Control Plane 获取/刷新签名 edge token 与 HubDirectory，后续 direct/Relay 连接只访问 Hub |
| FILE001 | 完成 | 统一文件能力设计门禁 | 文件 owner、权限、方法、流控、失败语义和旧 API 迁移边界清晰 |
| FILE002 | 完成 | daemon 文件 metadata 与预览 | local protocol 可安全 list/stat/preview/mkdir/rename/delete/copy/move |
| FILE003 | 完成 | 文件上传下载数据流 | local 与 WebRTC 使用同一流协议完成背压、取消、续传和摘要校验 |
| FILE004 | 已完成 | 共享 UI 与 Official Android 闭环 | App 可浏览、预览、上传、下载并经 direct/single Relay 手测 |
| VT001 | 完成 | 收口 vterm restart 生命周期竞态 | 全量 core race 不再报告 Emulator close/read 竞态 |
| GA003 | 延后 | 双 Edge Relay Mesh corridor pilot | 仅在 CLOUD004 完成并有真实 corridor 数据后恢复 |
| GA004 | 延后 | 单 transit 受控加速 | 仅在 GA003 数据证明需要时恢复 |
| KS012 | 暂停 | 快捷键跨切片总契约守卫 | Cloud 单区域主线完成后重新排序 |
| KS013 | 暂停 | 快捷键文档与示例 | Cloud 单区域主线完成后重新排序 |
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
- FILE001 文档-only：`git diff --check`。
- FILE002：protocol/core 定向测试、文件系统 sandbox harness、`git diff --check`。
- FILE003：protocol/core/remote 定向测试、慢消费者/取消/续传/损坏数据 harness、`git diff --check`。
- FILE004：client workspace 测试、Community/Official Android 单测与 APK 构建边界、direct/single Relay 文件 E2E、ADB 手测、`git diff --check`。
- VT001：vterm 定向并发 harness、core restart 定向 race、全量 `go test -race ./core`、`git diff --check`。
- 只有切片真实跨越全仓 contract 时才运行 `make test-all`；当前开发阶段不运行 public snapshot 或 public-only release gate。

## 当前状态

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
- CLOUD011 进行中：Control Plane 登录/enrollment 返回的签名 edge credential 已绑定 Hub ID、URL、region 和 directory version；Companion v2 secret session 与 Official Android 安全会话缓存该目录并拒绝 Hub 变更或版本回滚。endpoint resolve 已从 Control Plane 移到 Hub，Android resolve、Relay lease 和 signaling 均只使用 bearer edge credential 访问 Hub；contract 测试在登录后关闭 Control Plane 仍完成上述三条链路。`114.66.58.243` 已部署当前代码，本机公网 TUI 实测 direct 显示 `connected/direct`、`relay_only` 显示 `connected/single`；Companion/Control Plane/devcloud race 与 `make test-android` 全绿。当前 `adb devices -l` 无设备，尚未执行 Official Android 真机 Control Plane 中断验收，因此切片保持进行中。
- 正式开源隔离、生产 OAuth/TLS、持久化数据库、计费、团队治理、Relay Mesh 和多区域运维全部延后。
