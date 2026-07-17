# 工作流：客户端目录与 CONN003 连接运行时重构

## 当前真值

- 当前最早未完成切片是 `C3B`。目录 ownership 已完成收口；现在只实现 `client/endpoint` 的纯 planner，不得提前进入 runtime、TUI 接线或 CLI 接线。
- `CONN001` 已完成的 Endpoint/Route registry、strict parser/writer、EndpointAssembler 和 portable bootstrap/share contract 位于 `client/endpoint`；Android/TypeScript 未接线 registry/assembler 已在 C3R 删除。
- `CONN002` 已完成：`shared/remoteauth` 与 daemon-local AccessStore 拥有 DeviceIdentity、ClientAccessIdentity、PairingTicket、client-bound CapabilityGrant v2、channel binding auth、撤销和重启恢复。
- 当前真实代码状态：local Unix、SSH stdio、managed WebRTC 的 transport/protocol primitive 仍保留，但旧 `ResolveCurrentRoute`、TUI lazy bundle/session owner、CLI endpoint/local/SSH/managed dial 与 Hello 实现已删除。`cmd/termx` 当前保留指向未来共享 runtime 的明确未接线编译缺口；`RouteSelectionPlanner`、race、generation 和 stamped result 从 C3B 重新实现。
- 客户端连接 runtime 的目标 owner 是顶层 `client/runtime`，Endpoint 领域是 `client/endpoint`；不得放进语义含糊的 `shared/` 或 TUI。CONN003 先让 TUI/CLI 消费该 runtime；Android/iOS/桌面绑定与可选 WebAssembly WebRTC 只记录后续 contract，本轮不做真实跨平台编译。
- C3A 文档冲突已收口；C3X 又同步更新 `tui/docs/multi-endpoint-transport-plan.md`、`docs/development/cli-command-design.md`、`tui/docs/architecture.md`，明确旧 owner 已删除、当前正在重建共享 runtime。
- Cloud 单区域 direct/single Relay、Official Android、公网 HTTP staging、文件能力、CLI002-CLI008、KS012-KS017 已完成；这些是背景，不是当前可主动修改范围。
- `WEB003`、`CLOUD018`、`SI001` 暂停；`CONN004-CONN008`、GA、多区域、生产 TLS/OAuth、正式开源隔离全部待后续排序。
- 插件系统在独立分支；本分支不新增插件协议、代码或文档。

## 不变边界

- `workflow.md` 是本分支唯一活动驱动文件。若旧文档、聊天记录或旧代码行为与本文件冲突，以本文件为准。
- `docs/development/repository-layout.md` 是当前目录 ownership、依赖方向和迁移边界的唯一架构基准；其他活动文档只引用它，不复制另一套仓库地图。
- Endpoint 表示 daemon 目标；Route 表达到达该 Endpoint 的持久配置；Transport 表示一次 route attempt 的运行时载体；Path 只表达 managed WebRTC 内部 `direct` / `single_relay`。
- `TerminalID` 只在 owning Endpoint/daemon 内唯一；跨 endpoint 状态必须使用 `TerminalRef{EndpointID, TerminalID}`。
- `connections.yaml` 只保存 Endpoint/Route 期望配置；当前 winner、generation、dial phase、observed path、错误和 transport 不得写回 registry。
- TUI/App 不拥有 terminal lifecycle、committed history、history truth 或 daemon 文件系统 truth；live/input/resize/history/copy/file 全部路由到 owning endpoint daemon。
- CapabilityGrant 只由 owning daemon 签发和验证；Control Plane、Companion、Hub、Relay、Route Planner 不得接收 CapabilityGrant、DeviceIdentity private key、terminal payload、history、输入、文件路径、文件 metadata 或文件内容。
- local、SSH、direct TLS、LAN discovery、daemon bootstrap、share 和已就绪 DataChannel 不依赖账号、订阅、Hub 或 Relay。
- CONN003 只接 `local-unix` 与 `ssh-stdio` 的外层多 route race。`managed-webrtc` 保持单 route 可用但不参与共同竞速，等 `CONN005`；`direct-tls` 和 LAN discovery 等 `CONN004`；share 等 `CONN006`。
- `client/runtime` 是 route plan、race、winner/loser、session generation、protocol session、授权状态和稳定错误的跨端真值；TUI、CLI、Android、iOS、桌面与可选 Web 只能作为 host adapter、transport capability provider 和 projection consumer，不能再建立平行 session owner。
- `client/endpoint` 只拥有 Endpoint/Route 持久领域、assembler、planner 和 portable contract，不 dial、不读 credential、不 import TUI/Cobra/平台 UI。
- `tui/` 只拥有 UI model、update/effect、view、input、host、port 和 adapter；`tui/port` 定义 application-facing interface/DTO，`tui/adapter/*` 实现 protocol/client-runtime/system 边界。连接 bundle、route attempt 和 session owner 不得放回 TUI。
- `shared/` 在迁移期只容纳尚未归位的跨域 primitive/contract；不得新增任何 domain owner。后续独立切片再把 transport、remoteauth、cloudcompanion 和 diagnostics 移到明确顶层，不在 C3S2 扩散。
- 跨端边界使用 versioned protobuf command/event、opaque runtime handle 和显式资源释放语义；不得跨 C/JNI/WASM 边界暴露 Go pointer、Go struct、goroutine、`context.Context` 或平台对象。
- Android/iOS/桌面仍拥有系统生命周期、网络可达性通知、Keystore/Keychain、文件选择、通知和私有 Cloud 装配；这些能力以 host event 或异步 request/result 输入 Go runtime，不反向成为连接状态真值。
- 可选 Web 只支持浏览器原生 WebRTC/DataChannel transport，不支持 local Unix、SSH 或 direct TLS。Pion 的 `js/wasm` 路径只能作为浏览器 WebRTC API wrapper；浏览器 DTLS channel binding 未形成与现有安全语义等价的可验证 contract 前，不得接入 CapabilityGrant 生产链路或降低认证要求。
- 不恢复 legacy remote、旧 Hub/session-token、grant-in-signaling、原始 SSH shell fallback、通用插件或旧 `termx-core`/`tuiv2`。
- 可以使用 `/tmp/termx-conn003-ref` 这类仓库外临时目录保存旧代码参考；不得在仓库内新增旧实现快照、fallback 目录或第二份 runtime 真值。

## 当前允许修改范围

- 主动范围：`workflow.md`、`AGENTS.md`、`README.md`、活动架构文档、`client/`、`tui/{port,adapter,testkit,state,app}/`、`cmd/termx/`、CONN003 E2E 与必要 `testkit/`。
- 受限联动：`core/`、`internal/protocol/`、`shared/remoteauth/` 只允许为 fresh DeviceIdentity challenge proof、protocol Hello、channel-bound generation/stamp contract 做最小修改。
- C3S2 机械联动：允许只更新 `shared/cloudcompanion/`、`shared/remoteauth/`、`private/cloud/devcloud/`、现有测试中的 import，以及 `clients/mobile/android/app/build.gradle` 的 fixture 路径；不得借目录迁移修改行为。
- 禁止范围：`clients/mobile/`、`clients/ui/`、`remote/`、`proto/`、`private/archive/`，除非当前 CONN003 实现被真实编译 contract 阻塞且先更新本文件说明原因。

## CONN003 分阶段计划

| ID | 状态 | 内容 | 完成条件 |
| --- | --- | --- | --- |
| C3A | 已完成 | 文档真值收口 | 三个冲突文档不再声称 CONN003 已实现；写清重构目标、非目标和删除边界 |
| C3R | 已完成 | CONN001/CONN002 平行模型去重 | 删除未接线 Android/TypeScript Endpoint registry/assembler；删除无消息链路的 protobuf runtime/session/assembler 类型；client-access 改为 typed protobuf；删除旧内存 Revocations；pairing 输入上限只引用 canonical contract |
| C3X | 已完成 | 旧连接 owner 前置拆除 | 删除 `Endpoint.ResolveCurrentRoute`、TUI `EndpointManager` lazy bundle/session owner、CLI endpoint/local/SSH/managed route/dial/Hello/cleanup owner 及只验证这些旧路径的 harness；保留 transport primitive、protocol adapter、service request/result、reducer、daemon host starter 和平台 host capability；旧实现只通过 Git 历史或仓库外临时资产参考 |
| C3S1 | 已完成 | 仓库目录文档真值 | 新增唯一 repository layout 基准；更新 workflow/AGENTS/README/TUI/CLI/remote 活动文档；删除已完成的一次性 goal prompt；明确 history/archive 非活动；所有后续路径统一为 `client/endpoint`、`client/runtime`、`tui/port`、`tui/adapter` |
| C3S2 | 已完成 | 客户端与 TUI 目录迁移 | Endpoint 领域迁入 `client/endpoint`；建立 `client/{runtime,port,adapter}` 边界；TUI 拆为 `port`、`adapter/protocol`、`adapter/system` 与 `testkit`；依赖守卫阻止 UI/CLI/private 反向依赖和 port 实现回流 |
| C3B | 待开始 | `client/endpoint` planner 领域层 | 新增纯 `RouteSelectionPlanner`、`RouteAttempt` plan、priority grouped hedge、manual override、unsupported route 失败；无网络 IO；单测覆盖 full race、hedge、manual-only、未绑定 identity 多 route 拒绝、managed 不入 CONN003 race |
| C3C | 待开始 | fresh daemon proof / ReadySession contract | local Unix 与 SSH route attempt 在 protocol Hello 前完成 fresh DeviceIdentity challenge proof；只有 transport + proof + authorization + Hello 全部成功才能产出 `ReadySession` |
| C3D | 待开始 | `client/runtime` session owner 与 TUI adapter | 新 client runtime 成为每 Endpoint 唯一 route race、winner、loser、generation、lifecycle mailbox owner；TUI 只通过 `tui/adapter/clientruntime` 消费 projection/port；service 调用取得 generation lease，迟到回包拒绝 |
| C3E | 待开始 | CLI 接入同一共享 runtime | terminal/file/workspace/root TUI/`endpoint test` 共用 planner、attempt dialer 和 session owner；`--route` 显式 override sticky 于当前 client runtime；pair/access 等 application service 也不得在 `cmd` 直接 Dial/NewClient/Hello/resolve credential；静态守卫覆盖 `cmd/termx`，错误码稳定，不 fallback local/raw shell |
| C3F | 待开始 | attach/input/resize generation 边界 | attach candidate、confirm、commit、cleanup、detach、input、paste、resize 都携带原始 `EndpointSessionStamp`；stale cleanup 只查已有 bundle，禁止 lazy dial |
| C3G | 待开始 | 真实 local + SSH race E2E | 新脚本使用真实 local daemon 与 OpenSSH host 注入延迟，验证 default full race、priority hedge、manual override、loser process 回收、TerminalRef 稳定和旧 generation 拒绝 |
| C3H | 待开始 | 审查、状态回填、提交 | 全部准入通过，双 Agent 架构/代码审查 PASS，仅机械回填本文件状态和审查结论后提交 |

## 目标架构草图

### planner 只做纯计划

```go
type RouteSelectionPlanner struct {
    KindPolicy map[RouteKind]RouteKindPolicy
}

func (p RouteSelectionPlanner) Plan(req RouteSelectionRequest) (RouteSelectionPlan, error) {
    endpoint := Normalize(req.Endpoint)
    routes := filterEnabledSupported(endpoint.Routes, req.RequestedRoute, p.KindPolicy)
    if req.RequestedRoute == "" && len(routes) > 1 {
        requireCompleteDaemonIdentity(endpoint)
    }
    groups := groupByPriority(routes, endpoint.SelectionPolicy.HedgeDelay)
    return immutableAttempts(endpoint.ID, endpoint.DaemonIdentity, req.Intent, req.Generation, groups)
}
```

约束：

- planner 不 dial、不读 credential store、不访问 Cloud、不创建 protocol client、不写 registry。
- 未配置 priority 时所有 automatic eligible route 在 `t=0` 同组启动。
- 配置 priority 时所有 automatic route 必须都有 priority；同 priority 同组，下一组按 hedge delay 启动。
- 显式 `--route` 可以选择 manual-only route；自动竞速必须排除 manual-only。
- CONN003 `CanAutoRace=true` 只给 `local-unix` 与 `ssh-stdio`；`managed-webrtc` 只能在单 route 或显式 override 下保持原能力。

### 共享 client runtime 负责竞速和 generation

```go
func (m *ClientRuntime) ensureSession(ctx, endpointID, intent, routeOverride) (lease, error) {
    m.mu.Lock()
    if current winner matches sticky override {
        return lease(current.Generation, current.Bundle)
    }
    if inFlight exists {
        return waitForInFlight()
    }
    generation := m.nextGeneration(endpointID)
    plan := m.planner.Plan(endpoint, routeOverride, intent, generation)
    call := m.startRaceLocked(endpointID, generation, plan)
    m.mu.Unlock()
    return waitForWinner(call)
}

func race(plan) {
    for group in plan.Groups {
        sleep(group.Delay)
        start all attempts in group
    }
    first ReadySession wins CAS
    cancel loser contexts
    close loser bundles/transports/processes
    publish endpoint connected/offline event through mailbox
}
```

约束：

- `ReadySession` 必须表示 transport、fresh daemon proof、authorization 和 protocol Hello 全部完成。
- winner CAS 是唯一线性化点；静态 route 顺序只影响启动计划和失败诊断，不能让稍晚 Ready 反超。
- route switch 或 reconnect 先建立新 generation fence，再释放旧 winner；旧 generation 的 live/history/input/file 结果全部拒绝。
- lifecycle event mailbox 按 endpoint 合并，但不得丢失最终状态或相邻 `connected -> offline` 转换。
- loser cleanup 必须等待 SSH process、protocol transport、future TLS/WebRTC resources 释放；失败只作为诊断，不得复活 loser。
- `client/runtime` 不 import TUI、Cobra、Android/JNI、桌面 GUI、浏览器 DOM 或私有 Cloud 实现；平台 adapter 只能通过 capability/command/event contract 与它交互。
- `cmd/termx` 只是 composition root：允许解析参数、创建 host dependency、调用 runtime 和格式化结果；不得实现 Unix/SSH/WebRTC dial、credential resolution、DeviceIdentity proof、authorization、protocol Hello、route race、session cache 或 transport cleanup。

### channel-bound operation stamp

```go
type EndpointSessionStamp struct {
    EndpointID EndpointID
    RouteID RouteID
    Generation SessionGeneration
}

type AttachmentStamp struct {
    EndpointSessionStamp
    TerminalID string
    Channel uint16
    SurfaceID string
    ViewID string
    OperationID string
}

func SendInput(ctx, req) (Result, error) {
    bundle, ok := manager.bundleIfCurrent(req.Stamp)
    if !ok {
        return sessionStale(attempted=false)
    }
    err := bundle.Terminal.SendInput(stripEndpoint(req))
    return classifyAttempted(err, attempted=true)
}
```

约束：

- input、paste、resize、detach 必须携带创建 channel 的原始 stamp。
- stale 且未调用 adapter 时返回 `Attempted=false`，允许 reducer 发起 fresh recovery。
- adapter 已调用后的错误返回 `Attempted=true`，不得自动重放 input/paste bytes。
- cleanup 只查询当前已存在且 generation 精确匹配的 bundle，禁止因 cleanup 触发 lazy dial。

### 后续跨平台绑定

- Android 优先评估 `client/runtime` AAR binding；若使用 JNI/C ABI，接口仍必须复用同一 protobuf command/event contract，不另造 Kotlin 领域模型。
- iOS 使用 `gomobile bind -target=ios` 生成 XCFramework，由 Swift/Objective-C 薄 adapter 提供 Network.framework 可达性、Keychain、App lifecycle、文件和通知能力；Swift 不重建 endpoint/session 状态机。
- 桌面端通过同一 C ABI 或进程内 Go adapter 消费 runtime；私有 Cloud Companion 继续保持 out-of-process，不能因为共享 runtime 改回静态链接私有实现。
- Web 弱场景使用 `GOOS=js GOARCH=wasm`，Pion 只包装浏览器 `RTCPeerConnection`。Web host 提供 signaling、浏览器 credential custody 和生命周期事件，Go runtime 复用 planner、session、auth 与 terminal protocol 上层逻辑。
- CONN007 开始前必须先做独立 binding spike，验证 Android arm64 生命周期、异步事件、取消、资源释放和崩溃边界；Web spike 只有在产品恢复 Web 客户端时执行，至少验证 WASM package dependency、DataChannel、channel binding 和浏览器后台恢复。

## 删除/替换清单

- C3X 直接删除 `Endpoint.ResolveCurrentRoute`，不保留测试 helper、single-route compatibility guard 或同义替代函数。
- C3X 删除 TUI `EndpointManager` 的 registry、bundle cache、lazy dial、lifecycle watcher、event subscriber owner 以及 `EndpointServiceBundle`、`EndpointDialer`；C3D 的 ready bundle contract 必须直接定义在共享 runtime 边界。
- C3X 删除 CLI `openEndpointProtocolClient`、`openEndpointRouteProtocolClient`、`probeEndpointProtocolClient` 和可变 dial hook；调用方在 C3E 前允许形成明确的未接线编译缺口，不得增加 stub、fallback 或旧逻辑复制来维持假通过。
- C3X 删除 `cmd/termx` 中的 local/SSH/managed WebRTC dialer、credential resolution、Hello 和 transport cleanup 实现及其专属测试；后续分别由 `shared/transport`、`remote/client` adapter 与 `client/runtime` 组合，`cmd` 只注入依赖。
- `cmd/termx` 不再直接选择 route 或保存 session state；只负责 Cobra 参数、target resolution、输出和错误码。
- route 选择、dial、event publish、bundle cache 和 session owner 全部进入共享 Go client runtime；TUI 只保留 service adapter、mailbox 投影和 reducer 消息桥接。
- 旧文档中“CONN003 已实现基线”字样必须删除或改为“CONN003 目标基线”。
- 不新增仓库内 `legacy/`、`tmp/`、`archive/` 作为旧代码参考。

## 测试准入

- C3A 文档-only：`git diff --check`。
- C3C：`scripts/with-clean-termx-env.sh env GOWORK=off go test ./shared/transport/unix/... ./shared/transport/ssh/... ./internal/protocol/... ./core -count=1`；必要 race；`git diff --check`。
- C3S2：`scripts/with-clean-termx-env.sh env GOWORK=off go test ./client/endpoint/... ./tui/port ./tui/adapter/... -count=1`；运行目录依赖守卫和 `git diff --check`。`cmd/termx` 可继续因 C3D/C3E 未接线失败，但不得因旧 import、包循环或丢失非连接类型失败。
- C3B：`scripts/with-clean-termx-env.sh env GOWORK=off go test ./client/endpoint/... -count=1`；`git diff --check`。
- C3D：`scripts/with-clean-termx-env.sh env GOWORK=off go test ./client/runtime/... ./tui/port ./tui/adapter/... ./tui/state ./tui/app -count=1`；`scripts/with-clean-termx-env.sh env GOWORK=off go test -race ./client/runtime/... ./tui/adapter/... -count=1`；`git diff --check`。
- C3E：`scripts/with-clean-termx-env.sh env GOWORK=off go test ./cmd/termx -count=1`；必要 `go test -race ./cmd/termx -count=1`；`git diff --check`。
- C3F：`scripts/with-clean-termx-env.sh env GOWORK=off go test ./tui/... ./cmd/termx -count=1`；`git diff --check`。
- C3G：`scripts/conn003_local_ssh_race_e2e.sh` 必须用真实 local daemon 与 OpenSSH host 注入延迟，覆盖 default full race、priority hedge、manual override、loser process 回收、TerminalRef 稳定和旧 generation 拒绝。
- CONN003 最终准入：`scripts/with-clean-termx-env.sh env GOWORK=off go test ./client/... ./shared/transport/unix/... ./shared/transport/ssh/... ./core ./tui/... ./cmd/termx -count=1`；`scripts/with-clean-termx-env.sh env GOWORK=off go test -race ./client/... ./shared/transport/ssh/... ./core ./tui/adapter/... ./cmd/termx -count=1`；`scripts/conn003_local_ssh_race_e2e.sh`；双 Agent PASS；`git diff --check`。

## 执行规则

1. 每轮先读取本文件和适用 `AGENTS.md`，再检查 `git status --short --branch`。
2. 只执行任务队列中最早的 `进行中` 或 `待开始` 子项；不得跨到 CONN004+。
3. 若发现非本轮 agent 改动，先判断是否影响当前子项；未知或冲突改动不得覆盖。
4. 每个子项先补 harness，再改实现；不得用文档、fake 测试或 fallback 冒充用户链路。
5. CONN003 工作更新必须明确 domain owner、truth source、消息链路、持久化边界、取消链路和失败条件。
6. 手工编辑必须用 `apply_patch`；不得用 destructive git 命令。
7. 有效变动必须提交，提交信息用中文；用户明确要求不提交时除外。
8. CONN003 最终提交前必须完成双 Agent 架构审查与代码审查，两个 reviewer 明确 PASS 后才能提交。若 reviewer 工具不可用，标记阻塞，不得自审替代。
9. reviewer PASS 后只允许机械回填本文件状态、审查结论和已处理 finding 摘要；若再改实现、测试或其它文档，必须复审。

## 任务队列

| ID | 状态 | 说明 |
| --- | --- | --- |
| C3A | 已完成 | 修正文档真值与 CONN003 重构说明 |
| C3R | 已完成 | 删除 CONN001/CONN002 未接线平行模型与重复 wire/schema |
| C3X | 已完成 | 删除旧 route/session owner、`cmd` 网络实现与专属 harness |
| C3S1 | 已完成 | 收口仓库目录与依赖方向文档 |
| C3S2 | 已完成 | 迁移 `client/*` 并拆分 TUI port/adapter/testkit |
| C3B | 待开始 | 纯 RouteSelectionPlanner 领域层 |
| C3C | 待开始 | local/SSH fresh proof 与 ReadySession contract |
| C3D | 待开始 | `client/runtime` session owner 与 TUI adapter |
| C3E | 待开始 | CLI 共用共享 runtime 与 route override |
| C3F | 待开始 | attach/input/resize generation stamp |
| C3G | 待开始 | 真实 local + SSH race E2E |
| C3H | 待开始 | 最终准入、双审、状态回填和提交 |
| CONN004 | 待开始 | Direct TLS 与 LAN discovery，等 CONN003 完成后再恢复 |
| CONN005 | 待开始 | Managed Cloud 普通 Route adapter，等 CONN004 或用户重排 |
| CONN006 | 待开始 | endpoint share 与 TUI share action |
| CONN007 | 待开始 | Android 首先绑定共享 Go endpoint runtime；桌面复用同一 ABI；Web WASM 保持可选弱场景 |
| CONN008 | 待开始 | 旧路径删除与全链路总验收 |
| IOS001 | 延后 | iOS XCFramework + Swift host adapter，复用同一 Go runtime 与 protobuf contract |
| WEB003 | 暂停 | 完整用户中心与联合登录 |
| CLOUD018 | 暂停 | Hub 自主 Presence 与持久 session P0 |
| SI001 | 暂停 | TUI 同步输入组 |
| OPEN001 | 延后 | 正式开源与发布隔离 |

## 当前状态记录

- 2026-07-17：C3S2 完成。Endpoint 领域位于 `client/endpoint`；客户端边界为 `client/runtime`、`client/port` 与 local/SSH/managed/protocol adapter；TUI 边界为 `tui/port`、`tui/adapter/protocol`、`tui/adapter/system` 与 `tui/testkit`。client/TUI 静态依赖守卫已启用，相关准入通过；`cmd/termx` 仅保留 C3D/C3E 计划内未接线缺口。managed direct/single Relay 真实 E2E 已改为显式建立单次 WebRTC/protocol session，不再依赖已删除的 TUI session owner。
- 2026-07-17：跨端 runtime 决策写入当前真值。CONN003 不再把 session owner 固化在 TUI；共享 Go client runtime 负责 planner/race/generation/protocol/auth，TUI/CLI 先接 adapter，Android 通过 AAR、iOS 通过 XCFramework、桌面通过 C ABI 或进程内 adapter 接入。Web 只考虑浏览器原生 WebRTC 的 Pion WASM wrapper；未解决等价 DTLS channel binding 前不进入生产 CapabilityGrant 链路，本轮不做真实跨平台编译。
