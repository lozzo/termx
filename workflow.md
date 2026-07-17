# 工作流：CONN003 连接运行时重构

## 当前真值

- 当前最早未完成切片是 `CONN003`。本轮只处理统一 Endpoint / 多 Route 的 route planner、CLI/TUI session owner 与文档真值修正。
- `CONN001` 已完成：`shared/connection` 拥有 `connections.yaml` v2、Endpoint/Route registry、strict parser/writer、EndpointAssembler、portable bootstrap/share contract 和跨 Go/Kotlin/TypeScript fixture。
- `CONN002` 已完成：`shared/remoteauth` 与 daemon-local AccessStore 拥有 DeviceIdentity、ClientAccessIdentity、PairingTicket、client-bound CapabilityGrant v2、channel binding auth、撤销和重启恢复。
- 当前真实代码状态：local Unix、SSH stdio、managed WebRTC 各自已有可用接线，但 `RouteSelectionPlanner`、default full race、priority hedge、winner/loser cleanup、`SessionGeneration` guard 和 stamped service result 尚未完整实现。生产路径仍存在 `ResolveCurrentRoute` 过渡调用。
- 当前文档冲突：`tui/docs/multi-endpoint-transport-plan.md`、`docs/development/cli-command-design.md`、`tui/docs/architecture.md` 把 CONN003 部分能力写成“已实现”。后续必须先把这些内容改成“目标/缺口/待实现”，完成真实实现后才能再改回“已实现”。
- Cloud 单区域 direct/single Relay、Official Android、公网 HTTP staging、文件能力、CLI002-CLI008、KS012-KS017 已完成；这些是背景，不是当前可主动修改范围。
- `WEB003`、`CLOUD018`、`SI001` 暂停；`CONN004-CONN008`、GA、多区域、生产 TLS/OAuth、正式开源隔离全部待后续排序。
- 插件系统在独立分支；本分支不新增插件协议、代码或文档。

## 不变边界

- `workflow.md` 是本分支唯一活动驱动文件。若旧文档、聊天记录或旧代码行为与本文件冲突，以本文件为准。
- Endpoint 表示 daemon 目标；Route 表达到达该 Endpoint 的持久配置；Transport 表示一次 route attempt 的运行时载体；Path 只表达 managed WebRTC 内部 `direct` / `single_relay`。
- `TerminalID` 只在 owning Endpoint/daemon 内唯一；跨 endpoint 状态必须使用 `TerminalRef{EndpointID, TerminalID}`。
- `connections.yaml` 只保存 Endpoint/Route 期望配置；当前 winner、generation、dial phase、observed path、错误和 transport 不得写回 registry。
- TUI/App 不拥有 terminal lifecycle、committed history、history truth 或 daemon 文件系统 truth；live/input/resize/history/copy/file 全部路由到 owning endpoint daemon。
- CapabilityGrant 只由 owning daemon 签发和验证；Control Plane、Companion、Hub、Relay、Route Planner 不得接收 CapabilityGrant、DeviceIdentity private key、terminal payload、history、输入、文件路径、文件 metadata 或文件内容。
- local、SSH、direct TLS、LAN discovery、daemon bootstrap、share 和已就绪 DataChannel 不依赖账号、订阅、Hub 或 Relay。
- CONN003 只接 `local-unix` 与 `ssh-stdio` 的外层多 route race。`managed-webrtc` 保持单 route 可用但不参与共同竞速，等 `CONN005`；`direct-tls` 和 LAN discovery 等 `CONN004`；share 等 `CONN006`。
- 不恢复 legacy remote、旧 Hub/session-token、grant-in-signaling、原始 SSH shell fallback、通用插件或旧 `termx-core`/`tuiv2`。
- 可以使用 `/tmp/termx-conn003-ref` 这类仓库外临时目录保存旧代码参考；不得在仓库内新增旧实现快照、fallback 目录或第二份 runtime 真值。

## 当前允许修改范围

- 主动范围：`workflow.md`、`docs/remote-platform/unified-endpoint-route-refactor-plan.md`、`docs/development/cli-command-design.md`、`tui/docs/multi-endpoint-transport-plan.md`、`tui/docs/architecture.md`、`tui/docs/state-ownership-map.md`、`shared/connection/`、`shared/transport/{unix,ssh}/`、`tui/{services,state,app}/`、`cmd/termx/`、`scripts/conn003_local_ssh_race_e2e.sh`、必要 `testkit/`。
- 受限联动：`core/`、`internal/protocol/`、`shared/remoteauth/` 只允许为 fresh DeviceIdentity challenge proof、protocol Hello、channel-bound generation/stamp contract 做最小修改。
- 禁止范围：`private/cloud/`、`clients/mobile/`、`clients/ui/`、`remote/`、`proto/`、`private/archive/`，除非当前 CONN003 实现被真实编译 contract 阻塞且先更新本文件说明原因。

## CONN003 分阶段计划

| ID | 状态 | 内容 | 完成条件 |
| --- | --- | --- | --- |
| C3A | 已完成 | 文档真值收口 | 三个冲突文档不再声称 CONN003 已实现；明确当前代码仍用 `ResolveCurrentRoute` 过渡；写清重构目标、非目标和删除边界 |
| C3B | 待开始 | `shared/connection` planner 领域层 | 新增纯 `RouteSelectionPlanner`、`RouteAttempt` plan、priority grouped hedge、manual override、unsupported route 失败；无网络 IO；单测覆盖 full race、hedge、manual-only、未绑定 identity 多 route 拒绝、managed 不入 CONN003 race |
| C3C | 待开始 | fresh daemon proof / ReadySession contract | local Unix 与 SSH route attempt 在 protocol Hello 前完成 fresh DeviceIdentity challenge proof；只有 transport + proof + authorization + Hello 全部成功才能产出 `ReadySession` |
| C3D | 待开始 | TUI `EndpointManager` session owner 重写 | manager 成为每 Endpoint 唯一 route race、winner、loser、generation、lifecycle mailbox owner；删除生产路径 `ResolveCurrentRoute` 依赖；service 调用取得 generation lease，迟到回包拒绝 |
| C3E | 待开始 | CLI runtime 接入同一 session owner | terminal/file/workspace/root TUI/`endpoint test` 共用 planner 与 attempt dialer；`--route` 显式 override sticky 于当前 TUI session；错误码稳定，不 fallback local/raw shell |
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

### session owner 负责竞速和 generation

```go
func (m *EndpointManager) ensureSession(ctx, endpointID, intent, routeOverride) (lease, error) {
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

## 删除/替换清单

- 删除或降级 `Endpoint.ResolveCurrentRoute` 在生产路径的使用；保留时只能作为测试 helper 或单 route 兼容 guard，并加静态守卫防止 CLI/TUI runtime 调用。
- `cmd/termx` 不再直接选择 route 或保存 session state；只负责 Cobra 参数、target resolution、输出和错误码。
- TUI `EndpointManager.bundle()` 不再同时承担 route 选择、dial、event publish 和 bundle cache；拆为 planner adapter、session owner、service router、mailbox。
- 旧文档中“CONN003 已实现基线”字样必须删除或改为“CONN003 目标基线”。
- 不新增仓库内 `legacy/`、`tmp/`、`archive/` 作为旧代码参考。

## 测试准入

- C3A 文档-only：`git diff --check`。
- C3B：`scripts/with-clean-termx-env.sh env GOWORK=off go test ./shared/connection/... -count=1`；`git diff --check`。
- C3C：`scripts/with-clean-termx-env.sh env GOWORK=off go test ./shared/transport/unix/... ./shared/transport/ssh/... ./internal/protocol/... ./core -count=1`；必要 race；`git diff --check`。
- C3D：`scripts/with-clean-termx-env.sh env GOWORK=off go test ./tui/services ./tui/state ./tui/app -count=1`；`scripts/with-clean-termx-env.sh env GOWORK=off go test -race ./tui/services -count=1`；`git diff --check`。
- C3E：`scripts/with-clean-termx-env.sh env GOWORK=off go test ./cmd/termx -count=1`；必要 `go test -race ./cmd/termx -count=1`；`git diff --check`。
- C3F：`scripts/with-clean-termx-env.sh env GOWORK=off go test ./tui/... ./cmd/termx -count=1`；`git diff --check`。
- C3G：`scripts/conn003_local_ssh_race_e2e.sh` 必须用真实 local daemon 与 OpenSSH host 注入延迟，覆盖 default full race、priority hedge、manual override、loser process 回收、TerminalRef 稳定和旧 generation 拒绝。
- CONN003 最终准入：`scripts/with-clean-termx-env.sh env GOWORK=off go test ./shared/connection/... ./shared/transport/unix/... ./shared/transport/ssh/... ./core ./tui/... ./cmd/termx -count=1`；`scripts/with-clean-termx-env.sh env GOWORK=off go test -race ./shared/connection/... ./shared/transport/ssh/... ./core ./tui/services ./cmd/termx -count=1`；`scripts/conn003_local_ssh_race_e2e.sh`；双 Agent PASS；`git diff --check`。

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
| C3B | 待开始 | 纯 RouteSelectionPlanner 领域层 |
| C3C | 待开始 | local/SSH fresh proof 与 ReadySession contract |
| C3D | 待开始 | TUI EndpointManager session owner |
| C3E | 待开始 | CLI 共用 session owner 与 route override |
| C3F | 待开始 | attach/input/resize generation stamp |
| C3G | 待开始 | 真实 local + SSH race E2E |
| C3H | 待开始 | 最终准入、双审、状态回填和提交 |
| CONN004 | 待开始 | Direct TLS 与 LAN discovery，等 CONN003 完成后再恢复 |
| CONN005 | 待开始 | Managed Cloud 普通 Route adapter，等 CONN004 或用户重排 |
| CONN006 | 待开始 | endpoint share 与 TUI share action |
| CONN007 | 待开始 | Official App 统一 endpoint runtime |
| CONN008 | 待开始 | 旧路径删除与全链路总验收 |
| WEB003 | 暂停 | 完整用户中心与联合登录 |
| CLOUD018 | 暂停 | Hub 自主 Presence 与持久 session P0 |
| SI001 | 暂停 | TUI 同步输入组 |
| OPEN001 | 延后 | 正式开源与发布隔离 |

## 当前状态记录

- 2026-07-17：C3A 完成。多 transport 文档已删除旧 ME 路线图并收敛为 CONN003 技术边界；CLI 文档不再维护易漂移的命令快照；TUI 架构删除已退出目录迁移说明和旧落地顺序。三份文档均明确当前仍处于 `ResolveCurrentRoute` 过渡态，planner/session owner/generation 是待实现目标。
- 2026-07-17：因文档把 CONN003 写成已实现而源码仍处于 `ResolveCurrentRoute` 过渡态，本文件已压缩为当前活动控制面；真实实现从 C3B 的纯 planner 领域层开始。
