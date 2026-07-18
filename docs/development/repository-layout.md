# 仓库目录与依赖方向

## 文档职责

本文是当前仓库目录 ownership、依赖方向和迁移边界的唯一架构基准。活动任务顺序、允许修改范围和测试准入仍只看根目录 `workflow.md`。

目录名称必须表达 domain owner 或 adapter 角色，不得使用 `shared`、`services`、`common`、`manager` 这类无法判断真值归属的名称承载新领域状态。`docs/history/` 与 `private/archive/` 是只读背景，不参与当前目录判断。

## 顶层目标结构

```text
client/
  endpoint/          Endpoint/Route registry、assembler、planner、portable contract
  runtime/           PeerConnector、route race、ReadyPeerSession、pairing、generation、session owner、command/event
  port/              host capability、credential、cloud、clock、lifecycle 接口
  adapter/
    local/           local Unix attempt adapter
    ssh/             Go SSH direct-tcpip + ICE-TCP attempt adapter
    managed/         portable Cloud signaling、remote auth、Hello、Proto session attempt adapter
      pion/          native/Android Pion RTCPeerConnection primitive
    protocol/        ready transport 到 termx protocol service 的映射
  binding/           Proto bytes、opaque handle、异步事件、cancel/close/release 的稳定 C/JNI/WASM 核心

core/                daemon terminal lifecycle、history、live、storage truth
api_layer/           generated proto 驱动的 application dispatch、授权、session fence、取消与资源生命周期
api_mapping/         core domain 与 generated proto 的无状态双向字段映射；不是 transport
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
internal/protocol/   framing、Hello、channel、correlation 与 proto payload transport
proto/               所有插件/客户端/跨进程/跨语言 API 的唯一 schema truth 与生成代码
remote/              daemon WebRTC answerer、双方复用的 Pion/DataChannel primitive 与 remote auth 服务端接线
vterm/               terminal semantic interpreter
clients/             React/Capacitor/Android 等平台壳与 UI
private/             闭源云服务与只读历史 archive
testkit/             跨 package harness；不得成为生产 truth
```

## 依赖方向

允许的运行与依赖主方向：

```text
cmd / TUI / plugin / platform client
                  |
                  v
       client runtime / platform binding
                  |
                  v
 transport: Unix / WebRTC DataChannel / JNI / Swift / WASM
                  |
                  v
 protocol framing: Hello / channel / correlation / proto payload
                  |
                  v
          generated proto API
                  |
                  v
              api_layer
                  |
                  v
             api_mapping
                  |
                  v
              core domain
```

Proto 在图中表示 schema/message boundary，不表示网络 transport 或独立运行服务。返回结果和事件按相反方向流动。

硬规则：

- `client/endpoint` 不 import `client/runtime`、TUI、Cobra、platform UI、Cloud 私有实现或 protocol client。
- `client/runtime` 不 import `tui/`、`cmd/`、Android/JNI、Swift、DOM 或 `private/`。
- `client/runtime` 和 `client/port` 不 import concrete protocol、transport、remoteauth、Cloud Companion 或 WebRTC client；这些依赖只能出现在 `client/adapter/*`。
- `client/adapter/*` 可以依赖 runtime port 和具体 transport/protocol primitive，但不能持有第二份 route/session truth。
- `tui/state`、`tui/app` 和 `tui/render` 不 import concrete transport、credential store、Cloud Companion client 或 protocol client。
- `tui/port` 只定义 TUI 所需 interface/DTO；不得实现 IO，不得包含 fake。
- `tui/port` 按 history、terminal、live、path、endpoint event、clipboard 和 storage contract 分文件；不得重新合并为无 ownership 的总类型文件。
- TUI-owned endpoint phase/error/path projection 使用 `tui/state` 类型；`shared/cloudcompanion`、`cloudpb` 和 client runtime concrete event 只能由 `tui/adapter/clientruntime` 映射。
- `tui/adapter/*` 实现 port，并通过 message/effect 回投；不得直接修改 reducer-owned state。
- `cmd/termx` 不实现 Dial、Hello、authorization、credential resolution、route race、session cache 或 transport cleanup。
- `core/`、`remote/`、`private/` 不反向 import TUI 或 CLI。
- `proto/` 是所有跨边界 API 的唯一 schema truth；禁止在 `core/api`、client runtime、protocol 或 TUI port 复制业务 DTO。
- `api_layer/` 公开边界只使用 proto 生成类型，禁止依赖 UI、CLI、具体 transport、插件和 private Cloud implementation。
- `api_layer.RequestAdmission` 由当前 protocol connection 提供原子 lease，在同一准入边界校验连接存活、已协商 capability 和具体 command/resource authorization；daemon 不拥有 client runtime 的 endpoint alias、route 或 generation truth。
- `api_mapping/` 只做 core domain 与 proto 的确定性字段映射，不建立连接、不处理 framing，也不拥有状态、权限、session、route、fallback 或重试。
- `internal/protocol/` 只传输 proto payload，不拥有 application request/result/event 字段语义。
- 外部绑定只暴露 versioned protobuf command/event、opaque handle 和显式资源释放，不暴露 Go pointer 或内部 struct。

## 当前已落实边界

当前连接主线已经完成以下结构收口：

1. Endpoint/Route 持久领域位于 `client/endpoint/`，package 名为 `endpoint`。
2. `client/runtime/`、`client/port/` 与 `client/adapter/` 已建立明确边界；runtime 行为按 workflow 后续切片实现。
3. TUI application contract 位于 `tui/port/`，client runtime、protocol 与系统实现分别位于 `tui/adapter/clientruntime/`、`tui/adapter/protocol/`、`tui/adapter/system/`。
4. TUI fake 位于 `tui/testkit/`，生产 port 不包含测试状态或宿主 IO。
5. `EndpointServiceBundle`、`EndpointDialer` 已退出 TUI；后续 ready bundle contract 只能归 `client/runtime` 或 `client/port`。
6. 静态依赖守卫禁止 client 依赖 UI/CLI/private，也禁止 TUI port 重新依赖 protocol、adapter、testkit 或 `os/exec`。
7. managed client 编排已从 `remote/client` 收口到 `client/adapter/managed`；portable adapter 不 import Pion，native concrete peer 位于 `client/adapter/managed/pion`，成功 attempt 已完成 remote auth、Hello 和 Proto application session。

## 接口优先约束

- `client/runtime.Runtime` 是 TUI、CLI 和未来平台 binding 使用的连接控制面接口，只返回不可变 `SessionLease` 与 endpoint event，不暴露 transport 或 protocol client。
- `client/runtime.PeerConnector` 是 runtime 到单 route adapter 的边界；adapter 只能执行指定 attempt，不能选择其它 route 或 fallback。
- `client/runtime.ReadyPeerSession` 只在 transport、identity、authorization 和 protocol Hello 全部完成后成立，并拥有明确的 Done/Err/Close 生命周期。
- `client/port.Clock` 等 host capability 必须先定义接口和取消/释放语义，再接系统实现。
- TUI/CLI consumer 迁移必须发生在上述 contract 和 harness 之后；不得根据当前 concrete method 集合反向生成宽接口。

本轮没有移动 `shared/transport`、`shared/remoteauth`、`shared/cloudcompanion`、`perftrace`、`filelock`，也没有机械重命名整个 `tui/app`、`tui/state`、`tui/render`。这些目录确有命名与职责债务，但只能按后续独立切片处理，避免干扰连接运行时主线。

## 后续目录债务

- `shared/transport` 目标为顶层 `transport/`，因为 daemon 与 client 共同消费。
- `shared/remoteauth` 目标为顶层 `remoteauth/`，同时服务 owning daemon 与公开 client。
- `shared/cloudcompanion` 目标为 `cloud/companion/` 或 client cloud adapter contract。
- `shared/perftrace`、`shared/gridtrace`、`shared/filelock` 应进入明确的 `internal/diagnostics`、`internal/filelock` 等 infrastructure 目录。
- `tui/app` 后续拆为 reducer/update 与 UI runtime；`tui/state` 是否改名 `model`、`terminalhost` 是否改名 `host`，等连接 runtime 稳定后再决定。

这些债务不得阻塞 C3B-C3H，也不得继续向现有错误目录新增领域 owner。
