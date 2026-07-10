# `termx-remote-v2/` Agent Notes

## 定位

- 本目录是公开 WebRTC transport、DataChannel primitive 接线和端到端 remote auth runtime owner。
- cloud account、Hub/Relay adapter 和 SmartRoute 通过 `termx-shared/cloudcompanion` 的公开 contract 接入；本目录不得 import `termx-hub`、`web-control` 或 `private/`。
- terminal lifecycle、history truth 和 protocol method 仍属于 `termx-core-v2/`；endpoint 路由仍属于 TUI/client `EndpointManager`。

## 禁止

- 不得 import 或复制冻结 `termx-remote/` runtime。
- 不得恢复 session token、client public-key allowlist、localweb、remote UI 或原始 shell fallback。
- Companion/Hub 只能处理服务准入和 signaling，不能接收 CapabilityGrant、DeviceIdentity private key、DataChannel 或 terminal payload。
- 未经 DataChannel 内 DeviceIdentity proof、实际 DTLS certificate binding 与 capability challenge 的 session 不得进入 core-v2；任何新 adapter 都必须复用 `termx-shared/remoteauth` 状态机，不得注入“已授权”标记绕过。
