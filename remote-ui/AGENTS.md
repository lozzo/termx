# `remote-ui/` Agent Notes

## Boundary

- `remote-ui` 是 Web / embedded Web UI，不是 `termx-core` 或 `termx-remote`。
- `remote-ui` 负责：连接建立、运行时 WebRTC session、terminal/file/events 消费、UI 状态编排。
- `remote-ui` 不反向定义或污染 `termx-core` / `termx-remote` 的产品边界。
- 当前阶段只实现 browser adapter；native adapter 只保留 future boundary，不落实现。
- 当前仍是开发阶段。重构时不要保留兼容别名、旧导出、wrapper 文件或旧模块名；直接改成新的命名和边界，并同步更新所有调用方。

## Current Build Direction

**主线 WF-203 已完成**。当前 P0 任务：**local + hub 远程连接模型收敛**。

### WF-501 实现说明

**目标**：`VITE_CONTROL_URL=http://my-ctrl.example.com npm run dev` 启动后，打开 `/` 时 Control URL 已预填，无需手动改 Settings。

**修改文件**：`remote-ui/src/entries/mountRemoteControlApp.tsx`

当前入口应保留 `defaultControlUrl` 注入：
```tsx
<RemoteControlApp
  hubRtcSessionFactory={...}
  networkRuntime={networkRuntime}
  pairCrypto={pairCrypto}
/>
```

**新增文件**：`remote-ui/.env.example`
```
# Web Controller URL — 设置后 npm run dev 首页自动预填控制地址
# VITE_CONTROL_URL=http://localhost:3000

# 本地 hub 代理目标（localweb 模式，访问 /localweb.html 时生效）
# TERMX_LOCAL_WEB_ORIGIN=http://127.0.0.1:18888
```

**验证命令**：
```
cd remote-ui && npm run typecheck && npm run test
```

**不需要改**：`RemoteControlApp.tsx` 中的 `defaultWebControlUrl` 常量（作为最终 fallback 保留，不删除）。

### npm run dev 使用说明（给运行者）

```bash
# Hub 模式（访问 /）
VITE_CONTROL_URL=http://localhost:3000 npm run dev
# 打开 http://localhost:5173/ → 登录 web-control → 看到机器列表 → 点连接

# Local 模式（访问 /localweb.html）
TERMX_LOCAL_WEB_ORIGIN=http://192.168.x.x:18888 npm run dev
# 打开 http://localhost:5173/localweb.html → 扫描或输入本地 hub URL → 连接
```

## 连接架构（当前）

所有连接路径都通过 Hub 协议，只是 hub URL 不同：

```
local:  hubRtcConnector → http://LAN_IP:18888  (remote 二进制内嵌本地 hub)
hub:    hubRtcConnector → 公网 Hub URL（可 P2P，也可 relay）
both:   先 local，再 race hub URLs
```

termx:// URI payload（schema_version: 4）：
```json
{
  "type": "termx_pair",
  "schema_version": 4,
  "preferred_path": "local",
  "machine": { "id": "machine-id", "name": "Machine" },
  "local": { "hub_urls": ["http://192.168.1.10:18888"] },
  "hub": { "hub_urls": ["https://hub.example.com"], "web_control": "https://control.example.com" },
  "pairing": { "session_id": "pair-session", "secret": "pair-secret" },
  "bootstrap": {}
}
```

**已删除的旧路径**：
- `localRtcConnector.ts` → 调用 `/api/local/rtc/offer`（localweb 端点，已删除）
- `localAgentApi.ts` 中的 offer/pair/terminal 调用（localweb 端点，已删除）

**保留的当前代码**：
- `hubRtcConnector.ts` — 主连接路径，local 和 hub 共用
- `hubApi.ts` — Hub 标准 API（sessions、pairing/claims、answer）
- `browserRtcSession.ts` — WebRTC core（offer、answer、DataChannel）
- `connectionOrchestrator.ts` — local + hub 连接路径编排

## 连接流程（本地 hub）

```
1. 用户提供本地 hub URL（QR 扫描 / 手动输入）
2. LocalHubUrlProvider.getLocalHubUrl() → "http://192.168.x.x:18888"
3. 检查是否已配对 → hubApi.createPairingClaim(localHubUrl)
4. 连接 → hubRtcConnector.connect(localHubUrl, session_token)
5. WebRTC DataChannel 建立
6. terminal/file/events（与 hub mode 完全一致）
```

## App 认证（已简化）

- `remote-ui` 持有 **session_token**（pairing 时从 hub 获取，HMAC-SHA256，由 agent 签发）。
- session_token 按 machineId 存储：`termx.session.{machineId}.token`
- 每次发送 offer 时，session_token 作为 offer payload 的 `session_token` 字段随 offer 发出。
- **不再有**：app ed25519 key pair 生成、AppCertificate 存储、per-offer ed25519 签名。
- `remote-ui` 不调 Web Controller 做 offer 前验证——验证由 agent 在收到 offer 时 HMAC 验证。
- DTLS（WebRTC 内置）提供传输层防重放，无需额外签名。

## Transport Architecture

- 运行时 transport 统一基于 WebRTC DataChannel。
- 所有网络能力必须先定义 TypeScript `interface`，再提供 browser implementation。
- 组件层不得直接依赖：`RTCPeerConnection`、`RTCDataChannel`、`fetch`、`localStorage`。
- 客户端 path 只允许：`local`、`hub`。
- relay 只能表现为 capability/policy/connection info，不能变成第四种 transport。
- Hub 是 dumb relay，`remote-ui` 不假设 Hub 会做任何 cert 验证或 policy 决策。

## Workflow

- 遵守根 `AGENTS.md` 与根 `workflow.md`。
- 每个切片 TDD 推进，切片完成后独立 subagent review。
- 新发现问题同步写入根 `workflow.md`。

## Review Focus

- 是否还有任何对 `/api/local/rtc/offer`、`/api/local/pair`、`/api/local/terminals`、`/api/local/status` 的调用（不得有）
- local 连接路径是否已改为 `hubRtcConnector`（不得保留旧 localRtcConnector）
- 组件层是否泄漏了浏览器网络对象（RTCPeerConnection、fetch 等）
- local 和 hub 连接代码路径是否真正统一（不得有双轨）
- 测试是否覆盖了 local path 使用 hubRtcConnector 的行为
