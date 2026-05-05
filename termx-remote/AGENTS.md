# termx-remote Agent Notes

## Boundary

- `termx-remote` 是 TermX remote 产品域唯一 owner。
- 它负责：hub、agent runtime、pairing、signaling、session orchestration、remote protocol、cert 验证。
- 它依赖 `termx-core/clientapi` 获取 shell-neutral daemon capability。
- 它不得依赖 `termx-core` 的具体 server internals。
- 它不得依赖 `termx-hub`、`termx-cli`、`remote-ui`（boundary test 强制执行）。

## Current Build Direction

主线目标：删除重复代码、统一 local/cloud 流程，完成 remote 产品本体。

核心工作：
- 删除 `localweb/` 包（自定义本地 HTTP API，与 hub 协议重复）
- 删除 `hub/controlclient/` 包（hub 不再调 Web Controller）
- 清理 `service.go` 中所有 localweb/localRTCAnswer/localWebAdapter 相关代码
- 实现 `service.go` LocalEnable()：嵌入启动 hub/httpapi + cmux（HTTP + ICE-TCP）
- 扩展 `agent/runtime.Manager` 支持 `[]hubURL`（both 模式双注册）
- 在 agent offer 接收路径上实现 app cert 验证（签名 + 有效期 + machine_id）

## Product Rules

### Hub（dumb relay）

- Hub 是纯信令中继，不做任何认证决策。
- Hub 不调用 Web Controller 验证 app certificate 或连接票据。
- Hub 只存储有 TTL 的短时内存状态：online agents、pending offers/answers、pairing claims。
- Hub 没有数据库，没有 durable state，没有 migration。
- `hub/controlclient/` 包应当删除；hub httpapi 不注入任何 control verifier。

### Web Controller（仅 agent 目录）

- Web Controller 只返回：该用户的 agent 列表 + 每个 agent 的 hub_url。
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
| `hub/heartbeat` | 保留 | 心跳管理 |
| `hub/sessionflow` | 保留（清理） | 删除 LocalPlan/AnswerLocal（若无引用） |
| `hub/cloud` | 保留 | 云存储 offer/answer 抽象 |
| `hub/controlclient` | **删除** | Hub 不再调 Web Controller |
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

- Hub httpapi 是否仍有任何 controlclient 或 Web Controller 调用
- Hub 是否引入 durable state / DB / migration
- local 与 cloud 流程是否重新分裂（代码路径应完全一致）
- Agent 侧 cert 验证是否覆盖签名、有效期、machine_id 三项
- Manager 多 hub 注册是否正确共享同一 agent identity
- DataChannel E2E 加密是否在 DTLS 之上独立实现（不替换 DTLS）
