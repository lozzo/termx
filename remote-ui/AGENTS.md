# `remote-ui/` Agent Notes

当前项目根目录：`remote-ui/`

## Boundary

- `remote-ui` 是 Web / embedded Web UI 的产品壳，不是 `termx-core`。
- `remote-ui` 负责连接建立、运行时 WebRTC session、前端 terminal/file/events/api 消费层，以及 UI 状态编排。
- `remote-ui` 不应反向定义或污染 `termx-core` 的 shell-neutral runtime 边界。
- 当前产品方向是 APP-first，但当前阶段**只实现 browser runtime adapter**；native adapter 只保留接口与工厂边界，不落实现。

## Transport Architecture

- `remote-ui` 的运行时 transport 统一基于 WebRTC DataChannel。
- 平台中立公共接口层必须以 `RtcSession` 作为唯一运行时连接对象，并以 `RtcBinaryChannel` / `RtcJsonRpcChannel` / `ConnectionInfo` / `ConnectionCapabilities` 等中性类型对外暴露能力。
- HTTP 只允许承担 signaling / discovery / pairing / rendezvous / hub poll-answer 等建链前职责；HTTP 不是运行时 transport。
- 所有网络相关能力必须先定义 TypeScript `interface`，再提供 browser implementation。
- 当前阶段不得实现 native 运行时代码；但必须为未来 native 适配器保留清晰的 interface / factory 边界。
- 前端实现必须先抽象平台中立的公共接口，再提供浏览器 WebRTC 实现；不要把浏览器 `RTCPeerConnection` / `RTCDataChannel` / `fetch` / `localStorage` 直接扩散到高层业务代码。
- 客户端可见的连接路径只分 3 类：`local`、`public_p2p`、`managed`。
- APP 连接策略必须按阶段升级：
  - 优先 local/LAN
  - 再 `public_p2p`
  - 最后 `managed`
- relay 不是客户端单独选择的 transport 类型。`managed` 路径下是否走 relay，必须由 Hub / TURN / ICE 侧策略决定。

## Current Migration Task

- 当前任务不再是局部 WebRTC rewrite，而是配合 remote 域迁移做 `remote-ui` 网络边界重构。
- 目标是让 `remote-ui` 只依赖稳定接口层，并与未来 `termx-remote` 的统一 hub/signaling 流程对齐。
- 当前阶段只完成 browser implementation：
  - `browserRtcSession`
  - browser connector/signaling adapters
  - browser API/storage/crypto adapters
- native 相关文件只允许出现接口或工厂，不允许落真实实现。

## Workflow

- 当前任务必须采用 TDD：
  - 先定义目标行为
  - 先写失败测试
  - 再写最小实现
  - 再重构
  - 再跑验证
  - 再更新根 `workflow.md`
- 当前任务必须做切片级 code review：
  - 每个切片完成后必须发起一次独立审查。
  - 审查重点必须包括：
    - 测试是否只是迎合实现
    - 是否存在 fake test / tautological test / 只验证 mock 交互的测试
    - 是否残留错误抽象
    - 是否把浏览器类型泄漏进公共接口或业务层
    - 是否为 future-native 保留了清晰边界而又没有过早实现 native
    - 是否遗漏 `workflow.md` 更新

## Validation

- 与本任务相关的改动，至少要运行：
  - `npm test`
  - `npm run typecheck`
  - `npm run build`
- 如果改动影响本地 embedded Web UI 资产同步，还必须运行：
  - `npm run build:localweb`

## Integration Rules

- `remote-ui` 后续应对接统一的 hub/signaling/session 流程，不要保留 local 与 managed 的两套产品逻辑。
- Web Control / Hub / local agent 的 HTTP API 只负责建链前 signaling/discovery/pairing/policy；terminal/file/api/events 运行时必须继续走 WebRTC DataChannel。
- 触及 remote buildout 的 `remote-ui` 改动必须遵守根 `AGENTS.md` 的 `workflow.md` 驱动、TDD、subagent review 规则。
