# `clients/ui/` Agent Notes

## Boundary

- `clients/ui/` 是未来公开快照的一部分，负责 App 与浏览器客户端共享的 UI、状态编排和平台中立 runtime interface。
- `clients/ui/` 不拥有 terminal lifecycle、committed history、capability authorization、云账号、Hub presence 或 Relay entitlement 真值。
- `clients/mobile/` 只能通过明确的 TypeScript interface 注入 native WebRTC、credential、file transfer 和 lifecycle primitive；组件层不得直接依赖 Capacitor、Kotlin bridge 或浏览器全局对象。
- public source 不得 import、读取或通过 build script 引用 `private/` 和私有 archive。
- 当前处于开发周期，不保留旧 session-token、localweb、Web Controller 或 legacy remote fallback；破坏性调整直接按当前 contract 更新全部调用方。

## Runtime Schema

- `proto/apipb/` 是插件、App/shared UI、官方客户端和第三方客户端公共 application API 的唯一源码真值。
- `clients/ui/src/generated/apipb/` 只保存由公共 API schema 生成的 TypeScript projection；组件和 runtime 不得复制 proto 业务字段建立第二套 API DTO。
- 迁移期 `proto/runtimepb/runtime.proto` 已删除，不得恢复兼容 schema、alias 或 generated artifact。
- `proto/wirepb/` 只拥有 framing 与 file resource stream payload；UI 不得把 wire message 当作公共 application API。
- 所有客户端 API 修改顺序固定为 proto -> generated code -> compatibility harness -> API Layer/API Mapping -> transport/client consumer。

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
cd clients/ui
npm run proto
npm test
npm run typecheck
npm run build
```

涉及公开快照边界时，还必须从仓库根运行：

```bash
scripts/public-snapshot-guard.test.sh
```
