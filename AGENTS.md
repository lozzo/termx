# TermX Remote Build Agent Notes

## Current Mission

完成可用的 remote 产品主链路：

- `termx-remote/hub`：dumb relay，无数据库，无认证决策，纯信令转发
- `termx-remote/agent`：连接 hub、验证 app certificate、控制谁能连入
- `termx-cli`：装配 local/cloud/both 三种运行模式
- `remote-ui`：browser 客户端，统一 hub/signaling/session 流程
- Web Controller：仅做 agent 目录服务，不做连接时认证

## Product Model

### Hub（dumb relay）

- Hub 是纯信令中继，不做任何认证决策。
- Hub 不调用 Web Controller 验证 app certificate 或连接票据。
- Hub 只存储有 TTL 的短时内存状态：online agents、pending offers/answers、pairing claims/results。
- Hub 没有数据库，没有 durable state。
- Hub 不区分 local 和 cloud 部署——代码完全相同，只是运行地址不同。

### Web Controller（agent 目录）

- Web Controller 只做：用户登录、列出该用户注册的 agent、返回每个 agent 所在的 hub_url。
- Web Controller **不做**：连接时 cert 验证、policy 决策、offer/answer 审核、runtime 代理。
- 用户登录 App 后，App 从 Web Controller 拿到 agent 列表和 hub_url，后续直接连 Hub，不再回调 Web Controller。

### Agent（认证决策方）

- Agent 在收到 offer 时验证 app certificate（签名、有效期、machine_id）。
- Agent 是唯一的认证决策方；Hub 只是转发 offer，不审核内容。
- Agent 用 app cert 公钥参与 DataChannel 密钥协商，在 DTLS 之上提供应用层 E2E 加密。

### 三种运行模式

| 模式 | 运行内容 | hub_url 来源 |
|------|----------|-------------|
| `local` | 进程内嵌入 hub（cmux: HTTP + ICE-TCP，LAN 暴露） | 本机 LAN IP:port |
| `cloud` | agent 连接云端 hub | 云端 hub 地址 |
| `both` | 并行：本地嵌入 hub + agent 同时注册云端 hub | 两个都有 |

- `both` 模式使用**一个** `runtime.Manager`，`hubURLs []string` 持两个地址。
- 认证逻辑在 agent 侧，与 hub 是 local 还是 cloud 无关，代码路径完全一致。

### Local Mode 技术细节

```
termx remote --mode local
  └─► 嵌入启动 hub/httpapi.NewHandler（LAN IP:port）
  └─► cmux 同端口复用：HTTP（hub API）+ ICE-TCP（WebRTC over TCP）
  └─► ICE config：只广播 TCP candidate，无 STUN/TURN
  └─► agent 用标准 register/heartbeat/poll 协议连本地 hub（127.0.0.1:port）
  └─► hub_url 返回 LAN IP:port
  └─► app certificate 验证逻辑与 cloud mode 完全相同（agent 侧）
```

## Architecture Principles

- `termx-core` 保持 shell-neutral daemon/core，不回流 remote 产品代码。
- `termx-remote` 是 remote 产品域唯一 owner。
- `termx-hub` 是云端独立部署单元，不归并入 `termx-cli`。
- Hub 不调 Web Controller；认证在 agent 侧。
- local 和 cloud 共用完全相同的 hub 代码、agent 代码、cert 验证代码。
- relay 不是第四种客户端 path；path 只允许 `local`、`public_p2p`、`managed`。
- runtime 数据面始终是 WebRTC DataChannel，不允许回退成 HTTP/WebSocket proxy。

## Module Structure（模块边界与职责速查）

| 模块 | 角色 | 部署位置 |
|------|------|----------|
| `termx-core` | shell-neutral daemon、协议、transport、PTY | 用户机器（in-process） |
| `termx-remote` | remote 产品域唯一 owner（hub/agent/pairing/protocol） | 用户机器（library） |
| `termx-hub` | Hub 可执行文件的命令行入口与配置适配器，产品逻辑 100% 在 `termx-remote/hub` | 云服务器（独立进程） |
| `termx-cli` | 产品壳，装配 core + remote，管理 local/cloud/both 模式 | 用户机器（CLI 工具） |
| `remote-ui` | Browser 侧 UI，HTTP/WebRTC 通信，不 import Go 代码 | 浏览器 |

### 待删除的死代码（不可违反）

- `termx-remote/localweb/` — 自定义本地 HTTP API，与 hub 协议重复，全部删除。
- `termx-remote/hub/controlclient/` — hub 不再调 Web Controller，整个包删除。
- `termx-remote/service.go` 中所有 localweb 相关代码（LocalWebHandler、localRTCAnswer、localWebAdapter 等）。
- `termx-cli/cmd/termx/web.go` — webshell cobra 入口，删除。
- `termx-cli/cmd/termx/main.go` 中 `cmd.AddCommand(webCommand(...))` 一行，删除。
- `termx-remote/hub/sessionflow` 中 `LocalPlan()`、`AnswerLocal()` — 若删除 localweb 后无引用，一并删除。

## Workflow Discipline

- 当前工作的唯一任务账本是仓库根目录 `workflow.md`。
- 所有新切片都必须从 `workflow.md` 认领任务并更新状态。

## TDD Rules

每个切片必须按下面顺序推进：

1. 定义目标行为
2. 写失败测试
3. 记录失败测试到 `workflow.md`
4. 写最小实现
5. 重构
6. 跑 focused tests
7. 跑 broader tests
8. 更新 `workflow.md`
9. 发起独立 subagent code review
10. 修复 review 发现
11. 再次更新 `workflow.md`

## Review Rules

每个切片完成后，必须用独立 subagent / review agent 审查，重点检查：

- 测试是否 fake / tautological
- Hub 是否偷偷引入了 Web Controller 调用或 cert 验证逻辑
- Hub 是否引入 durable state / DB / migration
- local 与 cloud 是否又分叉成两套流程
- agent 侧 cert 验证是否覆盖了签名、有效期、machine_id 三项
- 是否把 relay 重新抽象成第四种 path
- 是否遗漏 `workflow.md` 更新

## Documentation Rules

- 迁移文档可以保留为历史记录，但必须标注 archived，不得作为当前执行规则。
- 当前执行规则只写在：根 `AGENTS.md`、子目录 `AGENTS.md`、根 `workflow.md`。

## Directory Rule

- `termx-remote/`、`termx-hub/`、`termx-cli/`、`remote-ui/` 的后续改动都必须遵守本文件与根 `workflow.md`。
