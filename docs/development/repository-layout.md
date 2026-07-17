# 仓库目录与依赖方向

## 文档职责

本文是当前仓库目录 ownership、依赖方向和迁移边界的唯一架构基准。活动任务顺序、允许修改范围和测试准入仍只看根目录 `workflow.md`。

目录名称必须表达 domain owner 或 adapter 角色，不得使用 `shared`、`services`、`common`、`manager` 这类无法判断真值归属的名称承载新领域状态。`docs/history/` 与 `private/archive/` 是只读背景，不参与当前目录判断。

## 顶层目标结构

```text
client/
  endpoint/          Endpoint/Route registry、assembler、planner、portable contract
  runtime/           route race、ReadySession、generation、session owner、command/event
  port/              host capability、credential、cloud、clock、lifecycle 接口
  adapter/
    local/           local Unix attempt adapter
    ssh/             SSH stdio attempt adapter
    managed/         managed WebRTC attempt adapter
    protocol/        ready transport 到 termx protocol service 的映射
  binding/           后续 AAR、XCFramework、C ABI、WASM 的稳定外部边界

core/                daemon terminal lifecycle、history、live、storage truth
tui/                 纯 TUI 产品与平台适配
  state/             当前 reducer-owned UI model；后续独立切片再评估改名 model
  app/               当前 reducer/effect/workflow；后续独立切片再拆 update/runtime
  render/            view-model 与 frame 生成
  input/             raw input 到 semantic intent
  shortcut/          scene + key binding
  action/            UI action identity/invocation
  config/            TUI 配置加载与验证
  terminalhost/      TTY input/output primitive；后续独立切片再评估改名 host
  port/              TUI application-facing interface 与 DTO
  adapter/
    clientruntime/   client runtime command/event 到 TUI port/message 的映射
    protocol/        protocol projection 到 TUI DTO；迁移期保留直接 adapter
    system/          clipboard 等系统能力

cmd/termx/           Cobra、参数/target 解析、composition root、输出与退出码
internal/protocol/   仓库内部 daemon/client wire 实现
proto/               versioned wire schema 与生成代码
remote/              managed WebRTC/DataChannel primitive 与公开 remote auth 接线
vterm/               terminal semantic interpreter
clients/             React/Capacitor/Android 等平台壳与 UI
private/             闭源云服务与只读历史 archive
testkit/             跨 package harness；不得成为生产 truth
```

## 依赖方向

允许的主方向：

```text
cmd / platform binding / tui adapter
                  |
                  v
            client/runtime
              |       |
              v       v
 client/endpoint    client/port
              |
              v
 protocol / transport / remoteauth primitive
              |
              v
             core daemon
```

硬规则：

- `client/endpoint` 不 import `client/runtime`、TUI、Cobra、platform UI、Cloud 私有实现或 protocol client。
- `client/runtime` 不 import `tui/`、`cmd/`、Android/JNI、Swift、DOM 或 `private/`。
- `client/adapter/*` 可以依赖 runtime port 和具体 transport/protocol primitive，但不能持有第二份 route/session truth。
- `tui/state`、`tui/app` 和 `tui/render` 不 import concrete transport、credential store、Cloud Companion client 或 protocol client。
- `tui/port` 只定义 TUI 所需 interface/DTO；不得实现 IO，不得包含 fake。
- `tui/adapter/*` 实现 port，并通过 message/effect 回投；不得直接修改 reducer-owned state。
- `cmd/termx` 不实现 Dial、Hello、authorization、credential resolution、route race、session cache 或 transport cleanup。
- `core/`、`remote/`、`private/` 不反向 import TUI 或 CLI。
- 外部绑定只暴露 versioned protobuf command/event、opaque handle 和显式资源释放，不暴露 Go pointer 或内部 struct。

## C3S2 迁移范围

C3S2 只完成当前连接主线必须先纠正的结构：

1. `shared/connection/` 迁到 `client/endpoint/`，package 名改为 `endpoint`。
2. 建立 `client/runtime/`、`client/port/` 与 `client/adapter/` 目录骨架；不提前实现 C3B-C3F 行为。
3. `tui/services/` 拆成 `tui/port/` 与 `tui/adapter/protocol/`、`tui/adapter/system/`。
4. fake 移入 `tui/testkit/` 或测试文件，不留在生产 port package。
5. `EndpointServiceBundle`、`EndpointDialer` 不留在 TUI；C3D 所需 ready bundle contract 归 `client/runtime` 或 `client/port`。
6. 更新 import、文档链接和静态依赖守卫，不改变协议、registry 格式或用户行为。

C3S2 不移动 `shared/transport`、`shared/remoteauth`、`shared/cloudcompanion`、`perftrace`、`filelock`，也不机械重命名整个 `tui/app`、`tui/state`、`tui/render`。这些目录确有命名与职责债务，但必须按后续独立切片处理，避免把连接重写淹没在十万行纯路径 diff 中。

## 后续目录债务

- `shared/transport` 目标为顶层 `transport/`，因为 daemon 与 client 共同消费。
- `shared/remoteauth` 目标为顶层 `remoteauth/`，同时服务 owning daemon 与公开 client。
- `shared/cloudcompanion` 目标为 `cloud/companion/` 或 client cloud adapter contract。
- `shared/perftrace`、`shared/gridtrace`、`shared/filelock` 应进入明确的 `internal/diagnostics`、`internal/filelock` 等 infrastructure 目录。
- `tui/app` 后续拆为 reducer/update 与 UI runtime；`tui/state` 是否改名 `model`、`terminalhost` 是否改名 `host`，等连接 runtime 稳定后再决定。

这些债务不得阻塞 C3B-C3H，也不得继续向现有错误目录新增领域 owner。
