# `remote-ui/` Agent Notes

## Boundary

- `remote-ui` 是 Web / embedded Web UI，不是 `termx-core` 或 `termx-remote`。
- `remote-ui` 负责：连接建立、运行时 WebRTC session、terminal/file/events 消费、UI 状态编排。
- `remote-ui` 不反向定义或污染 `termx-core` / `termx-remote` 的产品边界。
- 当前阶段只实现 browser adapter；native adapter 只保留 future boundary，不落实现。

## Current Build Direction

**主线 WF-203 已完成**。当前 P0 任务：**WF-501 支持 `VITE_CONTROL_URL` env var**。

### WF-501 实现说明

**目标**：`VITE_CONTROL_URL=http://my-ctrl.example.com npm run dev` 启动后，打开 `/` 时 Control URL 已预填，无需手动改 Settings。

**修改文件**：`remote-ui/src/remoteAppMount.tsx`

当前代码（无 defaultControlUrl）：
```tsx
<WebControlRemoteApp
  managedRtcSessionFactory={...}
  networkRuntime={networkRuntime}
  pairCrypto={pairCrypto}
/>
```

修改为：
```tsx
<WebControlRemoteApp
  defaultControlUrl={import.meta.env.VITE_CONTROL_URL || undefined}
  managedRtcSessionFactory={...}
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

**不需要改**：`WebControlRemoteApp.tsx` 中的 `defaultWebControlUrl` 常量（作为最终 fallback 保留，不删除）。

### npm run dev 使用说明（给运行者）

```bash
# Cloud 模式（访问 /）
VITE_CONTROL_URL=http://localhost:3000 npm run dev
# 打开 http://localhost:5173/ → 登录 web-control → 看到机器列表 → 点连接

# Local 模式（访问 /localweb.html）
TERMX_LOCAL_WEB_ORIGIN=http://192.168.x.x:18888 npm run dev
# 打开 http://localhost:5173/localweb.html → 扫描或输入本地 hub URL → 连接
```

## 连接架构（新）

所有连接路径都通过 Hub 协议，只是 hub URL 不同：

```
local mode:   managedHubRtcConnector → http://LAN_IP:18888   (嵌入式本地 hub)
cloud mode:   managedHubRtcConnector → https://hub.termx.io  (云端 hub)
both mode:    connectionOrchestrator 先尝试 local，失败再用 cloud
```

**已删除的旧路径**：
- `localRtcConnector.ts` → 调用 `/api/local/rtc/offer`（localweb 端点，已删除）
- `localAgentApi.ts` 中的 offer/pair/terminal 调用（localweb 端点，已删除）

**保留的旧代码**（仍有效）：
- `managedHubRtcConnector.ts` — 主连接路径，local 和 cloud 共用
- `managedHubApi.ts` — Hub 标准 API（sessions、pairing/claims、answer）
- `browserRtcSession.ts` — WebRTC core（offer、answer、DataChannel）
- `connectionOrchestrator.ts` — 连接路径编排（需更新 local 路径实现）

## 连接流程（本地 hub）

```
1. 用户提供本地 hub URL（QR 扫描 / 手动输入）
2. LocalHubUrlProvider.getLocalHubUrl() → "http://192.168.x.x:18888"
3. 检查是否已配对 → managedHubApi.createPairingClaim(localHubUrl)
4. 连接 → managedHubRtcConnector.connect(localHubUrl, appCert)
5. WebRTC DataChannel 建立
6. terminal/file/events（与 cloud mode 完全一致）
```

## App 认证与 Cert

- `remote-ui` 持有 app certificate（pairing 时通过 hub pairing/claims 获取，machine key 签名）。
- 每次发送 offer 时，app cert 作为 offer payload 的一部分随 offer 一起发出。
- DataChannel 建立后，用 app cert 公钥参与密钥协商（未来 E2E 加密使用）。
- `remote-ui` 不调 Web Controller 做 offer 前的 cert 验证——验证由 agent 在收到 offer 时完成。

## Transport Architecture

- 运行时 transport 统一基于 WebRTC DataChannel。
- 所有网络能力必须先定义 TypeScript `interface`，再提供 browser implementation。
- 组件层不得直接依赖：`RTCPeerConnection`、`RTCDataChannel`、`fetch`、`localStorage`。
- 客户端 path 只允许：`local`、`public_p2p`、`managed`。
- relay 只能表现为 capability/policy/connection info，不能变成第四种 transport。
- Hub 是 dumb relay，`remote-ui` 不假设 Hub 会做任何 cert 验证或 policy 决策。

## Workflow

- 遵守根 `AGENTS.md` 与根 `workflow.md`。
- 每个切片 TDD 推进，切片完成后独立 subagent review。
- 新发现问题同步写入根 `workflow.md`。

## Review Focus

- 是否还有任何对 `/api/local/rtc/offer`、`/api/local/pair`、`/api/local/terminals`、`/api/local/status` 的调用（不得有）
- local 连接路径是否已改为 `managedHubRtcConnector`（不得保留旧 localRtcConnector）
- 组件层是否泄漏了浏览器网络对象（RTCPeerConnection、fetch 等）
- local 和 cloud 连接代码路径是否真正统一（不得有双轨）
- 测试是否覆盖了 local path 使用 managedHubRtcConnector 的行为
