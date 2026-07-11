# 工作流：统一文件能力纵向闭环

## 当前目标

- REC001 已完成仓库可控状态恢复：尚未提交的 RM004 目录与维护改动已经审计收口，误触发的发布期验证扩展已经清理。
- CLOUD001-CLOUD005 已完成单区域 Cloud 纵向闭环：开发云、桌面 managed direct、single Relay 与 Official Android 均已跨真实用户链路验收。
- 当前主线转为 FILE001-FILE004：把仍依赖旧 runtime API 的共享文件 UI 迁移到公开 termx protocol，并在 local、SSH、WebRTC direct/single Relay 与 Official Android 上保持同一文件语义。
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
- FILE001：`workflow.md`、`docs/remote-platform/`；只建立文件产品、权限、协议、流控、失败语义和迁移基线，不新增 runtime。
- FILE002：`proto/wirepb/`、`internal/protocol/`、`core/` 与必要 protocol/client harness；实现 daemon-owned 文件 metadata/read-preview 操作和显式 capability scope，不触及 Cloud 服务。
- FILE003：`proto/wirepb/`、`internal/protocol/`、`core/`、`remote/` 与必要 transport harness；实现同一 protocol session 内的流式上传下载、背压、取消、续传和完整性校验，不新增旧独立 DataChannel。
- FILE004：`clients/ui/`、`clients/mobile/`、必要公开 protocol adapter、Android 构建文件和手测文档；删除旧 `/files/*` 与 legacy file channel 依赖，完成 Official Android 真机和 direct/single Relay 验收。只有真实产品链路证明 contract 缺失时才最小联动 FILE002/FILE003 owner。
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
| FILE001 | 完成 | 统一文件能力设计门禁 | 文件 owner、权限、方法、流控、失败语义和旧 API 迁移边界清晰 |
| FILE002 | 待开始 | daemon 文件 metadata 与预览 | local protocol 可安全 list/stat/preview/mkdir/rename/delete/copy/move |
| FILE003 | 待开始 | 文件上传下载数据流 | local 与 WebRTC 使用同一流协议完成背压、取消、续传和摘要校验 |
| FILE004 | 待开始 | 共享 UI 与 Official Android 闭环 | App 可浏览、预览、上传、下载并经 direct/single Relay 手测 |
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
- FILE001 文档-only：`git diff --check`。
- FILE002：protocol/core 定向测试、文件系统 sandbox harness、`git diff --check`。
- FILE003：protocol/core/remote 定向测试、慢消费者/取消/续传/损坏数据 harness、`git diff --check`。
- FILE004：client workspace 测试、Community/Official Android 单测与 APK 构建边界、direct/single Relay 文件 E2E、ADB 手测、`git diff --check`。
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
- 正式开源隔离、生产 OAuth/TLS、持久化数据库、计费、团队治理、Relay Mesh 和多区域运维全部延后。
