# `remote/` Agent Notes

## 定位

- 本目录是公开 daemon WebRTC answerer、Pion/DataChannel primitive 和端到端 remote auth 服务端接线。
- cloud account、Hub/Relay adapter 和 SmartRoute 通过 `shared/cloudcompanion` 的公开 contract 接入；本目录不得 import `muxvia-hub`、`web-control` 或 `private/`。
- terminal lifecycle、history truth 和 protocol method 仍属于 `core/`；endpoint registry/planner 属于 `client/endpoint`，route race/session owner 属于 `client/runtime`，客户端 managed signaling/auth/Hello 编排属于 `client/adapter/managed`，native Pion 实现属于 `client/adapter/managed/pion`。`remote/webrtc` 只提供双方复用的底层 Pion primitive。

## 禁止

- 不得 import 或复制冻结 `muxvia-remote/` runtime。
- 不得恢复 session token、client public-key allowlist、localweb、remote UI 或原始 shell fallback。
- Companion/Hub 只能处理服务准入和 signaling，不能接收 CapabilityGrant、DeviceIdentity private key、DataChannel 或 terminal payload。
- 未经 DataChannel 内 DeviceIdentity proof、实际 DTLS certificate binding 与 capability challenge 的 session 不得进入 core-v2；任何新 adapter 都必须复用 `shared/remoteauth` 状态机，不得注入“已授权”标记绕过。
