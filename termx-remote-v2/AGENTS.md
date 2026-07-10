# `termx-remote-v2/` Agent Notes

## 定位

- 本目录是 ME012+ 新 hub/P2P transport runtime owner。
- 只负责设备身份、capability grant 验证、Hub 信令、WebRTC/DataChannel 适配和 daemon/client transport 装配。
- terminal lifecycle、history truth 和 protocol method 仍属于 `termx-core-v2/`；endpoint 路由仍属于 TUI/client `EndpointManager`。

## 禁止

- 不得 import 或复制冻结 `termx-remote/` runtime。
- 不得恢复 session token、client public-key allowlist、localweb、remote UI 或原始 shell fallback。
- Hub 不做授权决策；未经 remote-issued grant 校验的 DataChannel 不得进入 core-v2。
