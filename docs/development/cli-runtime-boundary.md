# CLI 与共享 Runtime 边界

## 文档职责

本文记录 `cmd/muxvia` 在架构清理阶段的 concrete dependency 债务、目标 application contract 和迁移顺序。活动状态与准入只看根目录 `workflow.md`。

## 当前问题

`cmd/muxvia` 目前同时承担了四类职责：

1. Cobra 参数、target 解析、输出和退出码。
2. terminal/file/workspace/access 等 client application workflow。
3. local daemon host 的启动、status、stop 与 client-access listener 装配。
4. Cloud Companion、managed daemon、WebRTC 和 transport concrete composition。

这些职责处于同一个 Go package，导致部分 command 文件仍直接 import protocol、transport、remoteauth、Cloud Companion 和 WebRTC。PA007 已把 CLI endpoint generation、route policy、Unix dial 与 Hello 收回 `client/runtime` 和 `client/adapter/local`；当前冻结清单约束的是剩余 daemon/Cloud concrete composition，不表示 client session ownership 仍在 CLI。

## 目标结构

```text
cmd/muxvia command
    -> client/runtime application interface
        -> client runtime owner
            -> client/adapter/{local,ssh,managed,protocol}
                -> protocol / transport / remoteauth / cloud primitive

cmd/muxvia daemon command
    -> daemon host application interface
        -> daemon host composition
            -> core / listener / managed ingress primitive
```

- command 文件只能解析参数、构造 application request、调用 interface、格式化 result/error。
- client application contract 位于 `client/runtime`，按 endpoint、terminal、file、workspace、access 分组；不得形成单个总接口。
- daemon host contract 与 client runtime contract 分离，daemon listener/ingress 不得伪装成 client route adapter。
- concrete import 最终只能出现在少量命名明确的 composition 文件；普通 `*_command.go` 不得新增 concrete protocol/transport/Cloud 依赖。
- local、Direct、SSH 与 managed Route 必须由共享 planner/runtime 接线；command 只能提交 endpoint、intent 和可选显式 override，不能自行竞速、缓存 session 或用 local fallback 隐藏 route 失败。
- TUI terminal、workbench 与 clipboard 是同一 endpoint session 上的 consumer，共用一条 ready connection；隔离由 Proto operation/subscription resource 提供，不为 consumer 数量创建平行 generation。

## 迁移顺序

1. 已完成：冻结 concrete import/direct helper 债务，守卫现在扫描全部 command 源文件，不再整文件排除 composition helper。
2. 已完成：local Unix route 使用 `ClientRuntime/SessionOwner`、`RouteSelectionPlanner` 与 `client/adapter/local`，不再由 CLI 生成 stamp、选择 route 或执行 Hello；旧 `SelectRoute` 已删除。
3. 已完成：planner、fresh ReadyPeerSession proof、per-endpoint race/session owner 与 local/Direct/SSH/lazy-managed native composition；CLI/TUI command helper 已收缩为共享 `ClientRuntime` 的 composition injection，旧单 route selector/connect helper 和 raw protocol adoption 已删除。
4. 已完成：operation generation stamp、Direct ICE-TCP 与 Go SSH `direct-tcpip` 真实 E2E、winner/loser cleanup；进程型 OpenSSH transport 和远端 `stdio-proxy` 已删除。
5. 当前剩余 Cloud 最终装配、产品 E2E 与全仓审计按 `workflow.md` 推进；daemon/Cloud concrete composition 债务不在普通 command 中扩散。

## 停止条件

- 新 interface 需要 import Cobra、TUI state、private Cloud 或 `internal/protocol`。
- 为保持命令编译而返回假 session、空 client 或默认 local route。
- terminal/file/workspace 分别建立自己的 endpoint session cache。
- command 根据错误文本决定 fallback、授权或 route 选择。
- daemon host 和 client session 被合并为同一个 manager/runtime。
