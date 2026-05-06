# termx-remote Agent Notes

## Boundary

- `termx-remote` 是 TermX remote 产品域唯一 owner。
- 它负责：hub、agent runtime、pairing、signaling、session orchestration、remote protocol、cert 验证。
- 它依赖 `termx-core/clientapi` 获取 shell-neutral daemon capability。
- 它不得依赖 `termx-core` 的具体 server internals。
- 它不得依赖 `termx-hub`、`termx-cli`、`remote-ui`（boundary test 强制执行）。

## Current Build Direction

**主线迁移已完成**（WF-201~305 全部 done）。当前重心转向部署就绪与端到端验证。

`termx-remote` 本身无新 P0 代码任务。`cd termx-remote && go test ./...` 必须保持全通过。

当前 P0 任务在 `remote-ui`、`web-control`、`termx-hub` 侧（见根 `workflow.md` WF-501~505）。

如发现 `termx-remote` 侧问题需修复，遵守以下规则并在 `workflow.md` 认领切片再动手。

已知 TODO（安全，暂不处理）：
- `hub/httpapi/handler.go:162` — agent 注册未调用 web-control verify-registration，标有 TODO 注释，后续处理。

## Product Rules

### Hub（dumb relay）

- Hub 是纯信令中继，不做任何认证决策。
- Hub 不调用 Web Controller 验证 app certificate 或连接票据。
- Hub 允许通过管理面 heartbeat 周期性向 Web Controller 汇报 hub 在线状态、online machine/agent 列表、relay/TURN 流量，并接收 forced disconnect 指令。
- 管理面 heartbeat 不得阻塞或审核单次 offer/answer，不得验证 app certificate 或连接票据。
- Hub 只存储有 TTL 的短时内存状态：online agents、pending offers/answers、pairing claims。
- Hub 没有数据库，没有 durable state，没有 migration。
- `hub/controlclient/` 包应当删除；hub httpapi 不注入任何连接时 control verifier。

### Web Controller（目录与管理面）

- Web Controller 返回：hub 目录、该用户的 agent 列表 + 每个 agent 的 hub_url。
- Web Controller 接收云端 Hub 管理面 heartbeat，用于维护 hub 在线状态、agent 在线状态、relay 流量、订阅/踢下线控制。
- Web Controller 不做连接时认证、policy 决策、offer 审核。
- Agent 连上 hub 后，认证由 agent 自己完成，不回调 Web Controller。

### Agent（认证决策方）

- Agent 在收到 offer 时验证 app certificate（Ed25519 签名、有效期、machine_id 匹配）。
- 验证通过后建立 WebRTC 连接，并用 app cert 公钥参与 DataChannel 密钥协商。
- DataChannel 在 DTLS 之上再加一层应用层 E2E 加密（app cert key 派生）。

### 运行模式

- `local`：service.go 嵌入启动 hub/httpapi（cmux: HTTP + ICE-TCP，LAN 暴露）
- `cloud`：agent 连接云端 hub
- `both`：一个 Manager，`hubURLs []string` 持 [localHubURL, cloudHubURL]

local 和 cloud 认证逻辑完全一致（agent 侧 cert 验证），hub 不区分两者。

## 包结构说明

| 包 | 状态 | 职责 |
|----|------|------|
| `hub/httpapi` | 保留 | Hub HTTP handler（dumb relay） |
| `hub/registry` | 保留 | Agent 注册表（纯内存 + TTL） |
| `hub/ice` | 保留 | ICE 配置（cloud: STUN+TURN；local: TCP only） |
| `hub/heartbeat` | 保留 | 云端 Hub 管理面 heartbeat（hub 状态、agent 列表、relay 流量、kick 指令） |
| `hub/sessionflow` | 保留（清理） | 删除 LocalPlan/AnswerLocal（若无引用） |
| `hub/cloud` | 保留 | 云存储 offer/answer 抽象 |
| `hub/controlclient` | **删除** | 旧包混合连接时认证与管理面职责；不得复活 |
| `agent/runtime` | 保留（扩展） | Manager 支持 []hubURL |
| `localweb` | **删除** | 与 hub 协议重复的自定义本地 HTTP API |
| `pairing` | 保留 | Pairing claim/response 类型 |
| `protocol` | 保留 | hub/app/agent 消息契约 |
| `cert` | 保留 | App certificate 类型与验证逻辑 |
| `bridge` | 保留 | WebRTC DataChannel bridging |
| `session/rtc` | 保留 | RTC offer/answer |

## Workflow

- 遵守根 `AGENTS.md` 与根 `workflow.md`。
- 每个切片都必须 TDD 推进，并在切片完成后做独立 subagent review。
- 新发现问题必须同步写入根 `workflow.md`。

## Review Focus

- Hub httpapi 是否仍有任何连接时 controlclient / Web Controller 调用
- Hub 管理面 heartbeat 是否保持周期性异步，不参与连接认证、offer 审核或 cert 验证
- Hub 是否引入 durable state / DB / migration
- local 与 cloud 流程是否重新分裂（代码路径应完全一致）
- Agent 侧 cert 验证是否覆盖签名、有效期、machine_id 三项
- Manager 多 hub 注册是否正确共享同一 agent identity
- DataChannel E2E 加密是否在 DTLS 之上独立实现（不替换 DTLS）
