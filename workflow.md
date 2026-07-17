# 工作流：架构就绪与 CONN003 共享连接运行时

## 当前结论

- 当前最早未完成切片是 `AR003B1B`。在 `AR001-AR005` 全部完成前，暂停 `C3B` 以及后续连接功能开发。
- 目录迁移已经完成，但目录正确不等于架构就绪。当前必须先收口接口、依赖方向、消息边界和 composition root，再迁移真实实现。
- 禁止继续通过移动文件、修改 import、增加别名或补局部分支冒充架构重构。每次迁移必须先存在目标 interface/DTO、owner、truth source、消息链路和失败条件。
- 当前仓库是私有开发真值。插件系统、正式开源隔离、多区域 Cloud、完整 Web、Android/iOS/桌面 binding 均不是当前主动工作。

## 已完成基线

- Endpoint/Route registry、strict parser/writer、EndpointAssembler 和 portable bootstrap/share contract 位于 `client/endpoint`。
- DeviceIdentity、ClientAccessIdentity、PairingTicket、CapabilityGrant v2、channel binding auth 和撤销由 `shared/remoteauth` 与 daemon AccessStore 持有。
- 旧 `ResolveCurrentRoute`、TUI session owner、CLI 直接 route/dial/Hello owner 已删除，不得恢复。
- TUI 已拆为 `tui/port`、`tui/adapter/*`、`tui/testkit`；client/TUI 依赖守卫已建立。
- managed direct 与 single Relay 真实 E2E 使用显式单次 WebRTC/protocol session，不依赖旧 TUI session owner。
- `cmd/termx` 当前保留等待共享 runtime 的明确编译缺口；架构阶段不得用 stub、fallback 或复制旧逻辑维持假通过。

## 架构就绪标准

只有同时满足以下条件，才允许恢复 `C3B`：

1. 每个状态只能有一个 domain owner 和 truth source，不存在 TUI、CLI、adapter、storage 或 test helper 的第二份 session/route/history 真值。
2. `client/endpoint`、`client/runtime`、`client/port`、`client/adapter/*` 的接口和依赖方向可以由静态守卫验证。
3. TUI state/app/render 只消费 TUI-owned DTO 和 application port，不直接依赖 protocol、transport、credential、Cloud client 或 runtime concrete implementation。
4. CLI 只保留参数解析、dependency composition、调用 application/runtime port、格式化输出和退出码。
5. 真实实现迁移前，目标 interface、request/result/event DTO、取消语义、资源释放和失败分类已经存在并有小型 harness。
6. 生产代码中不得出现为了暂时编译而增加的 nil runtime、panic stub、旧 helper 同义替代、隐式 local fallback 或双路径兼容。
7. 活动架构文档、代码依赖守卫和实际 import graph 表达同一套边界。

## 不变领域边界

- Endpoint 表示 daemon 目标；Route 表示持久到达方式；Transport 表示一次 attempt 的运行时载体；WebRTC Path 只表示 `direct` / `single_relay`。
- `TerminalID` 只在 owning Endpoint 内唯一；跨 endpoint 必须使用 `TerminalRef{EndpointID, TerminalID}`。
- `connections.yaml` 只保存期望配置，不保存 winner、generation、phase、observed path、错误或 transport。
- `client/endpoint` 只拥有 Endpoint/Route 持久领域、assembler 和纯 planner；不 dial、不读 credential、不创建 protocol client。
- `client/runtime` 最终拥有 route plan 执行、race、winner/loser、ReadySession、generation、protocol session、授权状态和 lifecycle mailbox。
- `client/port` 只定义 runtime 所需的 host capability 和 application-facing contract；不得包含具体 transport、Cloud、UI 或测试实现。
- `client/adapter/*` 只把具体 primitive 映射到 port，不拥有 route/session truth，不选择 fallback。
- TUI 不拥有 terminal lifecycle、committed history、daemon 文件系统或 client session；service result 必须通过 message/effect 回到 reducer。
- renderer 只消费 view-model，不读 runtime、protocol client、history source 或 concrete adapter。
- CapabilityGrant 只由 owning daemon 签发和验证；Control Plane、Companion、Hub、Relay、planner 不接收 capability、private key 或 terminal payload。
- local、SSH、direct TLS、LAN discovery、share 和已就绪 DataChannel 不依赖账号或私有 Cloud。

## 接口优先迁移规则

每个架构迁移按以下顺序执行：

```text
owner/truth source
    -> interface + immutable DTO
    -> fake/harness 验证消息和失败边界
    -> adapter 实现
    -> consumer 切换
    -> 删除旧调用路径
    -> 依赖守卫
```

- interface 按调用方需要定义，不能照搬现有 concrete method 集合。
- DTO 不跨层暴露 Go 内部状态、transport 对象、goroutine、`context.Context`、平台对象或 private Cloud 类型。
- adapter 不直接修改 reducer-owned state；runtime event 必须经 TUI message path 投影。
- 迁移完成后必须删除旧路径，不保留 compatibility wrapper、type alias 或双写。
- 若新接口只能靠大量特殊分支适配旧实现，停止迁移并重新判断 owner，而不是继续补丁。

## 当前允许范围

- 主动范围：`workflow.md`、`AGENTS.md`、`docs/development/repository-layout.md`、相关活动架构文档、`client/`、`tui/{port,adapter,testkit,state,app,render}/`、`cmd/termx/`。
- 受限联动：`core/`、`internal/protocol/`、`shared/{transport,remoteauth,cloudcompanion}/`、`remote/`，只能为已定义 interface 的 adapter 或 contract 做最小修改。
- 禁止范围：`clients/mobile/`、`clients/ui/`、`proto/`、`private/archive/`、插件系统、开源发布工程，除非先修改本文件说明真实阻塞。

## 任务队列

| ID | 状态 | 内容 | 完成条件 |
| --- | --- | --- | --- |
| AR001 | 已完成 | 压缩活动 workflow，建立架构就绪门禁 | 删除过时任务流水账；明确接口优先顺序、允许范围、停止条件和后续队列；`git diff --check` |
| AR002 | 已完成 | client contract 与依赖图 | 定义 runtime control interface、session/attempt/lifecycle DTO、ReadySession 校验、稳定错误与 Clock capability；harness 覆盖不可变 route、stamp、取消和 lifecycle 边界；无网络实现 |
| AR003 | 已完成 | TUI application boundary 清理 | port 按 history/terminal/live/path/endpoint event/clipboard/storage 拆分；TUI state/port 不依赖 Cloud/protocol/transport concrete type；clientruntime adapter 负责纯事件映射并由守卫固化 |
| AR003B1A | 已完成 | Core terminal/attachment/path contract | 定义 wire/UI-independent daemon-local DTO；client runtime application request/result 只包装 EndpointID 和 session/attachment stamp |
| AR003B1B | 进行中 | Core/protocol terminal contract 迁移 | core 实现与 protocol adapter 真实改用 `core/api`；更新调用方后删除 protocol/core 重复 DTO，不使用 type alias |
| AR003B2 | 待开始 | Core history/live API | 定义 authoritative history window 与 native screen DTO；core implementation 产出 API，protocol 只编码，TUI 只做 UI projection |
| AR003B3 | 待开始 | Core file/workbench API | file 隐藏 protocol transfer frame/channel；workbench 只表达 layout/binding intent；迁移 core/protocol owner |
| AR003B4 | 待开始 | Core API 依赖守卫与重复类型删除 | 禁止 API 依赖 implementation/wire/UI/client；删除 core/protocol/client 的重复 daemon-local DTO 并完成 import graph 审计 |
| AR004A | 已完成 | CLI concrete dependency 清单与冻结守卫 | concrete import 与 direct helper 已按文件精确列入可递减债务清单；新增依赖立即失败，移除债务必须同步删除守卫条目 |
| AR004B1 | 已完成 | Endpoint probe 与 access contract | probe 只返回 stamp/path/reason；access 只返回公开 identity 与脱敏 record/scope；contract harness 禁止 transport/protocol/credential owner 泄露 |
| AR004B2A | 已完成 | Terminal lifecycle contract | client-owned TerminalRef、defaults/create/list/mutation/metadata DTO 与窄接口完成；裸 TerminalID 不能跨 endpoint |
| AR004B2B | 已完成 | Terminal attachment contract | attachment stamp 固定 endpoint/route/generation/terminal/channel/surface/view/operation；错误显式区分 adapter 是否已尝试，input 不得隐式重放 |
| AR004B2C | 待开始 | Terminal history/live/event contract | 消费 `core/api` authoritative history/live DTO，并增加 endpoint/session stamp；不复制 core 或 protocol 模型 |
| AR004B3 | 待开始 | File 与 workspace application contract | file 使用高层 stream/read/write contract 隐藏 frame/channel；workspace 使用 client-owned snapshot/mutation DTO |
| AR004C | 待开始 | CLI consumer 迁移 | terminal/file/workspace/endpoint test/pair/access/root TUI 改为依赖 application contract；删除 direct dial/Hello/credential helper 调用意图，不增加 stub |
| AR004D | 待开始 | CLI composition 与守卫收口 | concrete adapter 只允许出现在明确 composition 文件；daemon host 与 client runtime 装配分离；债务清单归零或仅保留文档批准的 daemon-host primitive |
| AR005 | 待开始 | 架构就绪审查 | import graph、重复 owner、旧 helper、fallback、文档和测试门禁全部通过；独立架构 reviewer 与代码 reviewer PASS 后才恢复 C3B |
| C3B | 暂停 | `client/endpoint` 纯 RouteSelectionPlanner | AR005 PASS 后恢复；无网络 IO，覆盖 full race、priority hedge、manual-only、identity 与 managed route 限制 |
| C3C | 暂停 | fresh daemon proof / ReadySession | local/SSH attempt 完成 transport、fresh proof、authorization、Hello 后才产生 ReadySession |
| C3D | 暂停 | `client/runtime` session owner 与 TUI adapter | runtime 成为唯一 race/winner/generation/lifecycle owner，TUI 只消费 port/event projection |
| C3E | 暂停 | CLI 接入共享 runtime | terminal/file/workspace/root TUI/endpoint test 共用 runtime；`cmd` 不处理网络和 credential |
| C3F | 暂停 | operation generation stamp | attach/input/paste/resize/detach 使用原始 session stamp，拒绝 stale result 和隐式重放 |
| C3G | 暂停 | 真实 local + SSH race E2E | 验证 full race、hedge、override、loser cleanup、TerminalRef 和 stale generation |
| C3H | 暂停 | 最终准入与双审 | 全部测试、race、真实 E2E 和双 Agent 审查通过 |

## 架构阶段测试准入

- `AR001`：`git diff --check`。
- `AR002`：`scripts/with-clean-termx-env.sh env GOWORK=off go test ./client/... -count=1`；依赖守卫；`git diff --check`。
- `AR003`：`scripts/with-clean-termx-env.sh env GOWORK=off go test ./tui/... -count=1`；必要 race；`git diff --check`。
- `AR004`：运行可编译的 CLI contract/harness；用静态守卫确认 `cmd/termx` 不 import concrete transport/Cloud credential/protocol dial owner；计划内未接线必须有唯一明确来源。
- `AR005`：`go list` import graph 审计；client/TUI/相关 Cloud E2E；旧 owner/fallback 扫描；架构与代码两个独立 reviewer 明确 PASS。

## 后续跨平台约束

- Android/iOS/桌面最终复用同一 Go runtime contract，平台只提供生命周期、网络状态、Keystore/Keychain、文件和通知能力。
- 外部 binding 只暴露 versioned protobuf command/event、opaque handle 和显式释放，不暴露 Go pointer、Go struct 或 goroutine。
- Android 可评估 AAR/JNI/C ABI，iOS 可生成 XCFramework，桌面可用 C ABI 或进程内 adapter。
- Web 是可选弱场景，只支持浏览器原生 WebRTC/DataChannel；等价 DTLS channel binding 未验证前不得接入生产 CapabilityGrant 链路。
- 真实跨平台编译只在后续独立 binding spike 执行，不得反向影响当前接口 ownership。

## 执行规则

1. 每轮先读取本文件和适用 `AGENTS.md`，再检查 `git status --short --branch`。
2. 只执行最早的 `进行中` 或 `待开始` 切片，不跨切片实现。
3. 发现未知未提交改动不得覆盖；与当前切片冲突时停止说明。
4. 先写 interface/DTO 和 harness，再写 adapter，再迁移 consumer，最后删除旧路径。
5. 每个变动必须说明 owner、truth source、消息链路、持久化边界、取消链路和失败条件。
6. 禁止症状补丁、panic stub、fallback、双路径、隐式状态修正和旧代码复制。
7. 手工编辑使用 `apply_patch`，不使用 destructive git 命令。
8. 每个有效切片运行对应准入并使用中文提交信息提交。
9. reviewer finding 必须本地判断并修复；实质修复后重新测试和复审。
