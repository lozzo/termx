# termx Remote 完整架构重构 — Orchestrator Prompt

> **执行方式**：此文档供主 agent 读取。主 agent 负责按 Wave 调度 sub-agent，
> 同一 Wave 内的 sub-agent 在**同一次响应**中并发启动（单次 Agent 工具调用 × N），
> 等待全部完成后再启动下一 Wave。

---

## 项目信息

- 根目录：`/Users/lozzow/Documents/workdir/termx`
- Go 模块：`github.com/lozzow/termx`
- 任务文档目录：`/Users/lozzow/Documents/workdir/termx/codex-tasks/`
- 状态追踪文件：`/Users/lozzow/Documents/workdir/termx/workflow.md`

---

## 启动/恢复流程（主 agent 必须首先执行）

```
1. 读取 workflow.md
   - 若 overall_status == "done" → 任务已完成，报告状态后退出
   - 若有 in_progress 任务 → 检查其文件是否实际存在，决定重跑或继续
   - 若全部 pending → 从 Wave 0 开始

2. 从 workflow.md 的 current_wave 确定起点

3. 已标记 done 的任务跳过（不重新执行）
```

**workflow.md 更新规则（每步必做）**：
- 启动任何任务前：将该任务 Status 改为 `in_progress`，填写 Started 时间
- 任务成功后：改为 `done`，填写 Completed 时间和验证结果摘要
- 任务失败后：改为 `failed`，填写错误描述
- 每个 Wave 全部完成后：更新 `current_wave` 和对应 Wave 行的 Status
- 全部完成：将 `overall_status` 改为 `done`

---

## 系统架构图

```
┌────────────────────────────────────────────────────────────────────────────┐
│                            Web Controller                                   │
│                  账号 / 订阅 / hub 目录 / agent 列表                        │
└─────────┬──────────────────────────────────────────────┬────────────────────┘
          │ GET /api/v1/hubs  Bearer token                │ POST heartbeat
          ▼                                               ▼
┌─────────────────────────┐                  ┌───────────────────────────────┐
│      Online Hub          │                  │      Agent (termx 进程)       │
│   (cloud deployment)    │◀══ gRPC stream ══│  mode: online / both          │
│                          │   Bearer token   │  AccessToken from config      │
│  cmux 同端口分流:         │                  └───────────────────────────────┘
│  ├─ HTTP/2  → gRPC       │
│  ├─ HTTP/1  → REST API   │                  ┌───────────────────────────────┐
│  └─ Any     → ICE-TCP    │                  │      Agent (termx 进程)       │
└──────────┬───────────────┘                  │  mode: local / both           │
           │                                  │  ┌─────────────────────────┐ │
           │  HTTP POST /api/v1/sessions       │  │   Local Hub (embedded)  │ │
           │  offer + session_token            │  │  cmux: HTTP/2+HTTP/1    │ │
           │                                  │  │  + ICE-TCP              │ │
           │                                  │  └─────────────────────────┘ │
           │                                  └───────────────────────────────┘
           │
┌──────────┴─────────────────────────────────────────────────────────────────┐
│                           Browser (remote-ui)                               │
│                                                                             │
│  termx://pair?payload=base64({                                              │
│    machine: { id, name },                                                   │
│    addresses: { public: ["https://hub1.example.com",                        │
│                          "https://hub2.example.com"] },                     │
│    pairing: { session_id, secret, expires_at }                              │
│  })                                                                         │
│                                                                             │
│  连接策略:                                                                  │
│  ① Local Hub (2s 超时优先)                                                  │
│  ② 所有 hub_urls 并发 race → 最快成功者胜出                                  │
└──────────────────────────────┬──────────────────────────────────────────────┘
                               │ WebRTC DataChannels (P2P)
                               │ terminal:{id} / api / file:{id} / events
```

---

## 连接建立时序图

```
 Browser            Hub              Agent          Web Controller
    │                │                 │                  │
    │                │                 │─GET /api/v1/hubs─▶│
    │                │                 │◀─hub_urls─────────│
    │                │◀═gRPC stream════│ Bearer token      │
    │                │─RegisterAck────▶│ ICE servers       │
    │─POST /api/v1/sessions───────────▶│                   │
    │  offer SDP + session_token        │                   │
    │                │─gRPC push──────▶│                   │
    │                │  SignalingOffer  │─verifySessionToken│
    │                │                 │  HMAC 验证         │
    │                │◀─gRPC send─────│ createAnswer()     │
    │◀─HTTP 200 answer─────────────────│                   │
    │◀════════════════ ICE 协商 + DTLS ═════════════════════▶
    │◀════════════════ WebRTC DataChannels 建立 ════════════▶
```

---

## 鉴权流程图

```
配对:
  Browser ──pair_secret──▶ Agent
                              │─ constant-time compare
                              │─ token.Issue(machineSecret, Claims)
                              │   = HMAC-SHA256(secret, payload)
                              └──▶ session_token ──▶ Browser (存储)

连接:
  Browser ──offer_sdp + session_token──▶ Hub ──gRPC──▶ Agent
                                                          │─ token.Verify(HMAC)
                                                          │─ claims.MachineID 对比
                                                          │─ claims.ExpiresAt 检查
                                                          └──▶ AnswerOffer()
                                                               DTLS 握手 (防重放)
```

---

## gRPC 协议消息图

```
Agent ══════════════════ gRPC 双向流 ══════════════════ Hub
  │──RegisterRequest──────────────────────────────────▶│
  │◀─RegisterResponse (session_id, ICE, heartbeat)─────│
  │──HeartbeatRequest (每 N 秒)────────────────────────▶│
  │◀─SignalingOffer (session_id, sdp, session_token)────│
  │──SignalingAnswer (session_id, sdp, candidates)─────▶│
  │◀─PairingClaim (claim_id, pair_secret, caps)─────────│
  │──PairingResult (claim_id, session_token)────────────▶│
  │◀─Kick (reason) → 断开，退避重连────────────────────│
```

---

## Hub 选择流程图

```
Agent 启动 (online 模式)
  │
  ▼
GET /api/v1/hubs ──▶ Web Controller
  │ hub_urls: [hub1, hub2, hub3]
  ▼
并发 HEAD /api/health（每个 hub 探测 3 次，取中位数 RTT）
  │
  ▼ 按延迟排序
hub1: 45ms ✓  hub2: 120ms ✓  hub3: timeout ✗
  │
  ▼
gRPC stream 连接 hub1
  │
断线? ──▶ 指数退避 (1s → 60s) ──▶ 重新探测选择
```

---

## 任务依赖 DAG

```
Wave 1（同时启动）
┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐
│  TASK_01   │ │  TASK_02   │ │  TASK_03   │ │  TASK_04   │
│ New Pkgs   │ │ Proto/gRPC │ │ Config/CLI │ │  Frontend  │
└─────┬──────┘ └─────┬──────┘ └────────────┘ └────────────┘
      │              │
Wave 2 ▼             │
┌────────────┐       │
│  TASK_05   │       │
│ Protocol+  │       │
│ Auth       │       │
└─────┬──────┘       │
      │              │
Wave 3└──────────────┤
                     ▼
             ┌────────────┐
             │  TASK_06   │
             │ Integration│
             └─────┬──────┘
                   │
Wave 4             ▼
             ┌────────────┐
             │  TASK_07   │
             │ Cleanup +  │
             │ Tests      │
             └────────────┘
```

---

## Step 0 — 一次性工具安装（主 agent 直接执行，不派发 sub-agent）

执行前先更新 workflow.md：`current_wave: 0`，Wave 0 Status 改为 `in_progress`。

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
# macOS: brew install protobuf  |  Linux: apt install protobuf-compiler
cd /Users/lozzow/Documents/workdir/termx/termx-remote
go get google.golang.org/grpc@v1.64.0
go get google.golang.org/protobuf@v1.34.0
```

完成后更新 workflow.md：Wave 0 Status 改为 `done`。

---

## Wave 1 — 同一响应内并发启动 4 个 sub-agent

> 主 agent：
> 1. 先更新 workflow.md：`current_wave: 1`，TASK_01~04 Status 改为 `in_progress`，填写 Started 时间
> 2. 在**同一次响应**中调用 Agent 工具 4 次，不要等待一个完成再启动下一个

### Sub-agent 1

```
description: "TASK_01: 新增独立包"
prompt: |
  项目根目录: /Users/lozzow/Documents/workdir/termx
  Go 模块: github.com/lozzow/termx

  读取文件 /Users/lozzow/Documents/workdir/termx/codex-tasks/TASK_01_NEW_PACKAGES.md
  并按其中所有步骤执行：创建 session/token 包、machine_secret、latency.go、middleware_lan.go。

  完成后运行验证命令：
    cd /Users/lozzow/Documents/workdir/termx/termx-remote && go test -race ./session/token/... ./identity/... && go build ./discovery/... ./hub/httpapi/...

  返回格式：
    STATUS: SUCCESS 或 FAILURE
    VERIFIED: <验证命令输出摘要>
    ERRORS: <如有错误，详细说明>
```

### Sub-agent 2

```
description: "TASK_02: Proto 定义与 gRPC 服务"
prompt: |
  项目根目录: /Users/lozzow/Documents/workdir/termx
  Go 模块: github.com/lozzow/termx

  读取文件 /Users/lozzow/Documents/workdir/termx/codex-tasks/TASK_02_PROTO_GRPC.md
  并按其中所有步骤执行：创建 proto 文件、运行 protoc 生成 Go 代码、
  实现 hub/grpcapi/server.go、discovery/grpc_hub_client.go。

  完成后运行验证命令：
    cd /Users/lozzow/Documents/workdir/termx/termx-remote && go build ./protocol/hubgrpc/... ./hub/grpcapi/... ./discovery/...

  返回格式：
    STATUS: SUCCESS 或 FAILURE
    VERIFIED: <验证命令输出摘要>
    ERRORS: <如有错误，详细说明>
```

### Sub-agent 3

```
description: "TASK_03: 配置字段与 CLI 命令"
prompt: |
  项目根目录: /Users/lozzow/Documents/workdir/termx
  Go 模块: github.com/lozzow/termx

  读取文件 /Users/lozzow/Documents/workdir/termx/codex-tasks/TASK_03_CONFIG_CLI.md
  并按其中所有步骤执行：为 Config struct 新增 Mode/AllowLAN/LANIPs 字段，
  修改 CLI --mode/--token 标志，修改 buildRemotePairPayload 支持 hub_urls 数组。

  完成后运行验证命令：
    cd /Users/lozzow/Documents/workdir/termx/termx-remote && go build ./config/...
    cd /Users/lozzow/Documents/workdir/termx/termx-cli && go build ./...

  返回格式：
    STATUS: SUCCESS 或 FAILURE
    VERIFIED: <验证命令输出摘要>
    ERRORS: <如有错误，详细说明>
```

### Sub-agent 4

```
description: "TASK_04: Frontend TypeScript 简化"
prompt: |
  项目根目录: /Users/lozzow/Documents/workdir/termx
  TypeScript 目录: /Users/lozzow/Documents/workdir/termx/remote-ui

  读取文件 /Users/lozzow/Documents/workdir/termx/codex-tasks/TASK_04_FRONTEND.md
  并按其中所有步骤执行：简化 localAppIdentity.ts（删除 ed25519，改为 MachineSessionStore），
  修改 managedHubApi.ts（session_token 替代 appCertificate），
  修改 managedHubRtcConnector.ts（删除 signOffer），
  修改 connectionOrchestrator.ts（hub_urls 竞速）。

  完成后运行验证命令：
    cd /Users/lozzow/Documents/workdir/termx/remote-ui && npm run build

  返回格式：
    STATUS: SUCCESS 或 FAILURE
    VERIFIED: <验证命令输出摘要>
    ERRORS: <如有错误，详细说明>
```

---

## Wave 1 结果处理（主 agent 执行）

收集 4 个 sub-agent 的返回结果后，更新 workflow.md：
- 成功的任务：Status 改为 `done`，填写 Completed 和验证结果
- 失败的任务：Status 改为 `failed`，填写错误描述到 Blockers 章节

决策：
- 若全部 `STATUS: SUCCESS` → 继续 Wave 2
- 若有 `STATUS: FAILURE`：
  - 仅 TASK_01 失败 → 修复后重试 TASK_01，TASK_05 无法启动
  - 仅 TASK_02 失败 → 修复后重试 TASK_02，TASK_06 无法启动
  - TASK_03 / TASK_04 失败 → 独立修复，不阻塞 Wave 2/3

---

## Wave 2 — TASK_01 完成后启动（单个 sub-agent）

> 主 agent：
> 1. 确认 TASK_01 SUCCESS
> 2. 更新 workflow.md：`current_wave: 2`，TASK_05 Status 改为 `in_progress`
> 3. 启动 sub-agent

### Sub-agent 5

```
description: "TASK_05: Protocol 类型与鉴权简化"
prompt: |
  项目根目录: /Users/lozzow/Documents/workdir/termx
  Go 模块: github.com/lozzow/termx
  前置条件: TASK_01 已完成（session/token 包和 identity/machine_secret.go 已存在）

  读取文件 /Users/lozzow/Documents/workdir/termx/codex-tasks/TASK_05_PROTOCOL_AUTH.md
  并按其中所有步骤执行（内部串行：05-A → 05-B/C 并行 → 05-D/E/F 串行 → 05-G）：
  修改 protocol/hubv1/hub.go，修改 hub/cloud/types.go 和 hub/registry/types.go，
  修改 hub/cloud/service.go 和 hub/httpapi/handler.go，
  修改 pairing/session.go，修改 identity/identity.go。

  完成后运行验证命令：
    cd /Users/lozzow/Documents/workdir/termx/termx-remote && go build ./protocol/... ./hub/... ./pairing/...

  返回格式：
    STATUS: SUCCESS 或 FAILURE
    VERIFIED: <验证命令输出摘要>
    ERRORS: <如有错误，详细说明>
```

---

## Wave 3 — TASK_02 + TASK_05 均完成后启动（单个 sub-agent）

> 主 agent：
> 1. 确认 TASK_02 和 TASK_05 都 SUCCESS
> 2. 更新 workflow.md：`current_wave: 3`，TASK_06 Status 改为 `in_progress`
> 3. 启动 sub-agent

### Sub-agent 6

```
description: "TASK_06: 集成（Manager + Service.go）"
prompt: |
  项目根目录: /Users/lozzow/Documents/workdir/termx
  Go 模块: github.com/lozzow/termx
  前置条件:
    - TASK_02 已完成（protocol/hubgrpc/ 和 hub/grpcapi/ 已存在）
    - TASK_05 已完成（session/token 验证、protocol 类型已更新）

  读取文件 /Users/lozzow/Documents/workdir/termx/codex-tasks/TASK_06_INTEGRATION.md
  并按其中所有步骤执行（06-A → 06-B → 06-C → 06-D）：
  修改 manager.go（verifySessionToken + gRPC 连接循环 + hub 延迟选择），
  修改 service.go（cmux 增加 HTTP/2 分流 + gRPC server + LAN filter）。

  完成后运行验证命令：
    cd /Users/lozzow/Documents/workdir/termx/termx-remote && go build ./...

  返回格式：
    STATUS: SUCCESS 或 FAILURE
    VERIFIED: <验证命令输出摘要>
    ERRORS: <如有错误，详细说明>
```

---

## Wave 4 — 所有 sub-agent 完成后（单个 sub-agent）

> 主 agent：
> 1. 确认 TASK_01 ~ TASK_06 全部 SUCCESS
> 2. 更新 workflow.md：`current_wave: 4`，TASK_07 Status 改为 `in_progress`
> 3. 启动 sub-agent

### Sub-agent 7

```
description: "TASK_07: 清理旧代码与全量测试"
prompt: |
  项目根目录: /Users/lozzow/Documents/workdir/termx
  Go 模块: github.com/lozzow/termx
  前置条件: TASK_01 ~ TASK_06 全部完成

  读取文件 /Users/lozzow/Documents/workdir/termx/codex-tasks/TASK_07_CLEANUP_TESTS.md
  并按其中所有步骤执行：
  删除 termx-remote/cert/ 目录和 offer_signature.go，
  修复所有因删除导致的编译错误，
  更新受影响的测试文件，
  运行全量测试。

  完成后运行最终验证命令：
    cd /Users/lozzow/Documents/workdir/termx/termx-remote && go test -race ./...
    cd /Users/lozzow/Documents/workdir/termx/termx-cli && go build ./...
    cd /Users/lozzow/Documents/workdir/termx/remote-ui && npm run build

  返回格式：
    STATUS: SUCCESS 或 FAILURE
    TEST_RESULTS: <go test 输出摘要>
    ERRORS: <如有错误，详细说明>
```

---

## 最终验证（主 agent 执行）

收到 TASK_07 返回后：
1. 更新 workflow.md：TASK_07 Status 改为 `done`，Wave 4 Status 改为 `done`
2. 执行以下检查：

```bash
# 文件存在性检查
test -e /Users/lozzow/Documents/workdir/termx/termx-remote/cert/ \
  && echo "FAIL: cert/ still exists"
test -e /Users/lozzow/Documents/workdir/termx/termx-remote/session/rtc/offer_signature.go \
  && echo "FAIL: offer_signature.go still exists"
test -f /Users/lozzow/Documents/workdir/termx/termx-remote/session/token/token.go \
  || echo "FAIL: token.go missing"
test -f /Users/lozzow/Documents/workdir/termx/termx-remote/protocol/hubgrpc/hub.pb.go \
  || echo "FAIL: hub.pb.go missing"
test -f /Users/lozzow/Documents/workdir/termx/termx-remote/hub/grpcapi/server.go \
  || echo "FAIL: grpc server missing"
```

所有检查输出为空（无 FAIL 行）且 TASK_07 返回 `STATUS: SUCCESS` → 更新 workflow.md：`overall_status: done`。任务完成。

---

## 错误处理规则

| 情况 | 处理方式 |
|------|---------|
| Sub-agent 编译失败 | 主 agent 读取错误信息，指示该 sub-agent 修复后重试 |
| Sub-agent 3 次仍失败 | 记录到主 agent 输出，跳过该任务，继续后续可执行的 Wave |
| Wave N sub-agent 失败 | 不启动依赖其结果的下游任务，独立任务继续 |
| 全量测试（TASK_07）失败 | 主 agent 读取失败用例，针对性派发修复 sub-agent |
