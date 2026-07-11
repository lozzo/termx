# `remote-ui/` Agent Notes

## Boundary

- `remote-ui/` 是未来公开快照的一部分，负责 App 与浏览器客户端共享的 UI、状态编排和平台中立 runtime interface。
- `remote-ui/` 不拥有 terminal lifecycle、committed history、capability authorization、云账号、Hub presence 或 Relay entitlement 真值。
- `termx-app/` 只能通过明确的 TypeScript interface 注入 native WebRTC、credential、file transfer 和 lifecycle primitive；组件层不得直接依赖 Capacitor、Kotlin bridge 或浏览器全局对象。
- public source 不得 import、读取或通过 build script 引用 `private/` 和私有 archive。
- 当前处于开发周期，不保留旧 session-token、localweb、Web Controller 或 legacy remote fallback；破坏性调整直接按当前 contract 更新全部调用方。

## Runtime Schema

- `termx-proto/runtimepb/runtime.proto` 是 App/remote-ui runtime schema 的公开源码真值。
- `remote-ui/src/generated/runtimepb/` 只保存由该 schema 生成的 TypeScript projection，不得从 archive 或旧 `termx-remote/` 生成。
- wire terminal protocol 继续由 `termx-proto/wirepb/` 拥有；UI 不得用本地 DTO 建立第二套 terminal protocol。

## Transport And Security

- WebRTC 是 endpoint transport；direct、single relay 和 relay mesh 只是 observed path，不是不同 endpoint 或 terminal protocol。
- 原始 `CapabilityGrant` 只在 DTLS DataChannel 建立后的端到端握手中交给 owning daemon，不能进入 Hub/Control Plane HTTP、SDP、日志或 Web storage。
- AccountAccessToken、HubAdmissionTicket 和 RelayLease 只属于 managed cloud adapter；Community/adapter 缺失时必须 fail closed，并且只影响对应 managed endpoint。
- 组件层不得直接创建 `RTCPeerConnection`、`RTCDataChannel`，也不得直接调用 `fetch`、`localStorage` 或 native bridge；这些能力必须通过可替换 interface 注入。

## History Boundary

- live terminal 可以使用 xterm、snapshot、短 scrollback 和 render cache。
- copy、search、selection text 和无限历史窗口必须消费 core-v2 authoritative history/window contract。
- 不得从 xterm buffer、DOM/canvas rows、snapshot rows、native backlog 或本地 append log 拼出第二份 history truth。

## Verification

```bash
cd remote-ui
npm run proto
npm test
npm run typecheck
npm run build
```

涉及公开快照边界时，还必须从仓库根运行：

```bash
scripts/public-snapshot-guard.test.sh
```
