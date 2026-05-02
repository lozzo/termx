# termx-core Agent Notes

当前项目根目录：`termx-core/`

## Boundary

- `termx-core` 是 shell-neutral runtime，不是 TUI、CLI 或 web/mobile 产品壳。
- `termx-core` 可以被 `tuiv2/`、`termx-cli/`、未来 `web/`、`mobile/`、TURN/WebRTC 服务复用。
- 禁止在 `termx-core` 中新增对 `tuiv2/*`、`termx-cli/*` 或其他壳层模块的反向依赖。

## Public Interfaces

- shell-neutral client contract：`clientapi/`
- wire contract：`protocol/`、`transport/`
- shared session/workbench contract：`workbenchdoc/`、`workbenchops/`、`workbenchsvc/`

## Rules

- screen update / snapshot / bootstrap 相关传输协议必须保持二进制编码；不要把线上链路改成 JSON。
- 共享能力优先进 public package，不要通过 `internal/*` 给外部壳层偷开入口。
- 改动 core 时，优先维护协议边界、服务模型和可复用性，不要引入 shell-specific 行为。
- 提交代码时，commit message 必须尽可能详细，准确写清动机、范围、关键实现与行为变化。

## Remote Agent Rules

- `termx-core` 可以承载 shell-neutral remote agent runtime、machine identity、app certificate、pairing、WebRTC offer/answer、DataChannel bridge、terminal/file/api/events 协议桥接。
- `termx-core` 不应承载 Web Control Plane 的用户、套餐、支付、订阅、订单、发票、OAuth 等产品业务逻辑。
- machine private key 必须只保存在本机；不要上传、导出到 app、写进 web/hub payload，或为了测试暴露明文。
- app certificate / connect ticket / nonce / timestamp / terminal ownership 校验必须是真行为，不能只在测试 mock 中成立。
- runtime 数据面必须保持 WebRTC DataChannel；HTTP 只能用于 local signaling、pairing、hub poll-answer、discovery 等建链前流程。
- core 侧不应引入浏览器 `RTCPeerConnection` / `RTCDataChannel` 类型；浏览器实现细节属于 `remote-ui` browser adapter。
- 遇到支付、邮件、OAuth、云服务账号等外部依赖时，不要塞进 core。应在 web/control-plane 的 provider interface 中 mock，并在 `docs/remote-rebuild/WORKFLOW.md` 记录 deferred external item。
- 触及 remote agent 的切片必须按根 `AGENTS.md` 的文件化 todo、TDD、subagent review 规则执行。
