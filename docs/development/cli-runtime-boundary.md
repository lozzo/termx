# CLI 与共享 Runtime 边界

## 文档职责

本文记录 `cmd/termx` 在架构清理阶段的 concrete dependency 债务、目标 application contract 和迁移顺序。活动状态与准入只看根目录 `workflow.md`。

## 当前问题

`cmd/termx` 目前同时承担了四类职责：

1. Cobra 参数、target 解析、输出和退出码。
2. terminal/file/workspace/access 等 client application workflow。
3. local daemon host 的启动、status、stop 与 client-access listener 装配。
4. Cloud Companion、managed daemon、WebRTC 和 transport concrete composition。

这些职责处于同一个 Go package，导致 command 文件可以直接 import protocol、transport、remoteauth、Cloud Companion 和 WebRTC。旧 dial owner 已删除后，部分调用方又保留未接线 helper 名称，使“composition root”和“application implementation”边界不可验证。

## 目标结构

```text
cmd/termx command
    -> client/runtime application interface
        -> client runtime owner
            -> client/adapter/{local,ssh,managed,protocol}
                -> protocol / transport / remoteauth / cloud primitive

cmd/termx daemon command
    -> daemon host application interface
        -> daemon host composition
            -> core / listener / managed ingress primitive
```

- command 文件只能解析参数、构造 application request、调用 interface、格式化 result/error。
- client application contract 位于 `client/runtime`，按 endpoint、terminal、file、workspace、access 分组；不得形成单个总接口。
- daemon host contract 与 client runtime contract 分离，daemon listener/ingress 不得伪装成 client route adapter。
- concrete import 最终只能出现在少量命名明确的 composition 文件；普通 `*_command.go` 不得 import concrete protocol/transport/Cloud。
- 当前未接线必须保持编译期可见，不能用 nil implementation、panic、local fallback 或旧 helper 同义替代隐藏。

## 迁移顺序

1. 冻结当前 concrete import 和 direct helper 债务，任何新增立即失败。
2. 在 `client/runtime` 先定义窄 application interface/DTO 和 harness。
3. 逐组迁移 endpoint test、terminal、file、workspace、pair/access 和 root TUI consumer。
4. 把 local/SSH/managed/protocol 实现接入 `client/adapter/*`，command 只注入 interface。
5. 分离 daemon host composition 与 client runtime composition。
6. 删除 direct helper 债务清单，静态守卫只允许批准的 composition 文件依赖 concrete package。

## 停止条件

- 新 interface 需要 import Cobra、TUI state、private Cloud 或 `internal/protocol`。
- 为保持命令编译而返回假 session、空 client 或默认 local route。
- terminal/file/workspace 分别建立自己的 endpoint session cache。
- command 根据错误文本决定 fallback、授权或 route 选择。
- daemon host 和 client session 被合并为同一个 manager/runtime。
