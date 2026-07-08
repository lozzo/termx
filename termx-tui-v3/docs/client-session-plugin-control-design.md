# 插件与 client session control 设计草案

## 背景

TermX 后续不会只有一个 TUI 客户端。TUI、App、Web、GUI 都会连接一个或多个 daemon endpoint；插件系统也不能只面向当前 TUI 的 panel/floating/window 模型设计。

当前比较清楚的方向是：插件不直接操作 UI 内部状态，而是通过 daemon 暴露的稳定协议入口发起请求。daemon 负责认证、路由、mailbox 和审计；目标 client session 本地裁决 UI action 是否支持、是否允许以及如何执行。这样可以避免把 daemon 做成第二份 TUI state truth，也避免未来 App/Web/GUI 接入时被 TUI 特例绑死。

本文只定义插件控制面的架构草案和第一阶段伪代码，不实现具体协议。

## 外部系统借鉴

- Zellij：插件是 WASM/WASI，由 host 加载，插件可以渲染自己的 pane，并通过 event/command API 影响 tab/pane/session；权限模型值得借鉴。TermX 不能照搬“插件直接深度改 layout/session”的模型，因为 TermX 有多 endpoint 和 client-local UI truth。
- tmux：server command API、control mode 和 hooks 证明了稳定控制协议的重要性。TermX 可以借鉴 command/control 面，但不应照搬 shell 插件体系，因为它缺少类型、权限和多客户端隔离。
- VS Code：manifest、activation events、contribution points、extension host placement 很适合 TermX。插件应该声明自己贡献的 action、keybinding、view/status 项和需要的 capability。
- Neovim / WezTerm：remote plugin、external UI、mux/gui 分离有参考价值，但 TermX 不应采用“插件可随意修改全局状态”的模型。

参考：

- https://zellij.dev/documentation/plugins
- https://github.com/tmux/tmux/blob/master/tmux.1
- https://code.visualstudio.com/api/advanced-topics/extension-host
- https://neovim.io/doc/user/api.html
- https://wezterm.org/config/lua/wezterm.plugin.html

## 术语

- `Plugin`：一个可安装扩展单元，由 manifest、贡献点、runner 和 capability 声明组成。
- `Host placement`：插件代码运行位置，分为 daemon、client、workspace 和 one-shot。
- `Client session`：一个正在运行的客户端实例，例如某个 TUI 进程、Web tab、GUI window 或 App 实例。
- `ClientKind`：client 类型，取值如 `tui`、`app`、`web`、`gui`。
- `Action`：可被快捷键、命令面板、插件或外部 control call 触发的 typed command。
- `Contribution`：插件向宿主声明的 UI 或行为扩展，例如 keybinding、action、status item、panel view。
- `Capability`：插件被授予的最小权限，例如 `terminal.kill`、`history.read`、`client.panel.close`。
- `TerminalRef`：跨 endpoint 的 terminal 引用，必须包含 `EndpointID + TerminalID`。裸 `TerminalID` 只允许在单 endpoint protocol adapter 内部出现。

## 核心原则

1. daemon 是 broker，不是 UI authority。
2. client session 本地拥有 UI 投影、focus、active panel、copy mode、clipboard prompt 和 action 解释权。
3. daemon/core 拥有 terminal lifecycle、history truth、terminal input/kill/restart 和 storage version/CAS。
4. 跨 endpoint 的 terminal target 必须使用 `TerminalRef`。
5. 插件 API 先 protocol-first，脚本语言只是 runner adapter。
6. 广播只用于 presence、capability、invalidated 这类通知；实际执行 action 默认 unicast 到明确 client session。
7. 快捷键绑定 action，不直接绑定内部函数。一个 action 可以有多个 key sequence。
8. 危险操作必须有 capability、precondition、deadline 和可审计 request id。

## 总体架构

```text
                 +------------------------------+
                 | daemon-scoped plugin runner  |
                 +--------------+---------------+
                                |
                                v
+-------------------------------+-------------------------------+
| daemon                                                        |
|                                                               |
|  plugin registry / grant verifier / session registry          |
|  client control mailbox / terminal API / storage API          |
|                                                               |
+-------------------------------+-------------------------------+
                                |
        client.control.call     |     terminal/storage/history
                                v
+-------------------------------+-------------------------------+
| client session: TUI / App / Web / GUI                         |
|                                                               |
|  action registry -> reducer/message path -> effects           |
|  EndpointManager -> owning endpoint daemon                    |
|                                                               |
+-------------------------------+-------------------------------+
                                ^
                                |
                 +--------------+---------------+
                 | client-scoped plugin runner  |
                 +------------------------------+
```

daemon 可以知道“哪些 client session 在线”和“这些 session 声明支持哪些 action/capability”。daemon 不解释 active panel、current tab、floating state、copy mode 或 UI focus。

client 可以知道“当前 UI 上下文”和“自己连接了哪些 endpoint”。因此涉及 `active panel`、`current tab`、`current focused terminal` 的 action 必须由目标 client 本地解释。

## 插件运行层级

| 层级 | 启动方 | 适合场景 | 边界 |
| --- | --- | --- | --- |
| `daemon-scoped` | daemon | terminal 监控、history 索引、storage 自动化、后台任务 | 不能直接拥有 UI state，只能发起 client action intent |
| `client-scoped` | TUI/App/Web/GUI | 快捷键、状态栏、panel view、clipboard/prompt/focus、UI action | 只操作当前 client 的 UI 投影 |
| `workspace-scoped` | workspace trust 后由 daemon 或 client 启动 | 项目工作流、共享 action、layout intent | 共享状态走 daemon opaque storage + CAS |
| `one-shot` | action/keybinding 按需启动 | 小脚本、命令、临时自动化 | 执行完退出，结果通过 typed response 返回 |

第一阶段推荐先支持内建 action 和 one-shot/external runner。第三方长期运行插件和 WASM sandbox 放到后续阶段。

## Manifest 草案

```yaml
id: termx.builtin.workspace
name: TermX Workspace Tools
api: 1

activation:
  - onClientKind:tui
  - onAction:termx.client.panel.close_and_kill_terminal
  - onCommand:workspace.rebuild

hosts:
  client:
    kinds: ["tui", "gui"]
    runner:
      type: builtin
    capabilities:
      - client.panel.read
      - client.panel.close
      - terminal.kill
    contributes:
      actions:
        - id: termx.client.panel.close_and_kill_terminal
          title: Close Panel and Kill Terminal
          scope: client
          danger: destructive
          params_schema: builtin:empty
      keybindings:
        - keys: ["ctrl+w x"]
          action: termx.client.panel.close_and_kill_terminal

  daemon:
    runner:
      type: external
      command: ["termx-plugin-workspace-daemon"]
    activation:
      - onDaemonStart
    capabilities:
      - terminal.list
      - storage.read:termx.workspace/*
      - storage.write:termx.workspace/*
```

关键点：

- `hosts.client.kinds` 决定该插件能在哪类 client 中加载。
- `contributes.actions` 只注册 action 元数据，不绕过 client reducer。
- `capabilities` 是最小权限声明，安装时形成 grant，运行时逐次校验。
- `danger: destructive` 的 action 需要默认 confirmation 或 trusted grant。

## Action registry

快捷键、命令面板、插件按钮和外部请求都应该统一触发 action：

```go
type ActionSpec struct {
    ID                   string
    OwnerPluginID        string
    Scope                ActionScope // client | daemon | workspace
    SupportedClientKinds []ClientKind
    RequiredCaps         []Capability
    Danger               DangerLevel
    ParamsSchema         SchemaRef
    Idempotent           bool
}

type ActionInvocation struct {
    RequestID      string
    ActionID       string
    Params         json.RawMessage
    Source         InvocationSource
    Target         ActionTarget
    Deadline       time.Time
    IdempotencyKey string
}
```

快捷键配置只指向 action：

```yaml
keybindings:
  - keys: ["ctrl+t 1", "ctrl+1"]
    action: termx.client.tab.activate
    args:
      index: 1

  - keys: ["ctrl+w x"]
    action: termx.client.panel.close_and_kill_terminal
    confirm: dangerous
```

这允许同一个操作绑定多个键，也允许插件贡献 action 后再由用户配置 keybinding。

## 命名空间

命名空间必须能同时区分归属、执行边界和兼容版本。建议规则：

| 类型 | 命名规则 | 示例 |
| --- | --- | --- |
| 内建 client action | `termx.client.<domain>.<verb>` | `termx.client.panel.close_and_kill_terminal` |
| 内建 daemon action | `termx.daemon.<domain>.<verb>` | `termx.daemon.terminal.kill` |
| 内建 workspace action | `termx.workspace.<domain>.<verb>` | `termx.workspace.layout.apply` |
| 第三方 action | `<publisher>.<plugin>.<domain>.<verb>` | `acme.deploy.terminal.run_task` |
| capability | `<owner>.<resource>.<verb>` | `client.panel.close`、`terminal.kill` |
| storage prefix | `plugin/<plugin_id>/...` 或 `termx/<domain>/...` | `plugin/acme.deploy/cache` |
| protocol method | `<area>.<resource>.<verb>` | `client.control.call` |

内建 action 的 `termx.client.*` 不表示只能由 TUI 执行，而是表示 action 的 authority 在目标 client session。本 action 可以由 TUI、App、Web 或 GUI 各自实现；是否支持由 session 注册的 `ActionSpec` 决定。

插件 ID 推荐使用 marketplace/publisher 级命名，例如 `acme.deploy`。插件自己的 action、storage 和 event 名称必须挂在该 ID 下，避免污染内建命名空间。

## Client session control 协议草案

### 注册 session

```go
type ClientSessionRegisterParams struct {
    SessionID    string
    ClientKind   ClientKind
    WorkspaceID  string
    InstanceID   string
    PID          int
    TTY          string
    Capabilities []ClientCapability
    Actions      []ActionSpec
    Epoch        uint64
}

type ClientCapability struct {
    Name       string
    Version    int
    Attributes map[string]string
}
```

建议方法：

```text
client.session.register
client.session.unregister
client.session.list
client.capabilities.list
```

session registry 是 daemon 的运行时路由表。它不是 workbench storage，也不持久化 UI state。

### 发起 control call

```go
type ClientControlCallParams struct {
    RequestID      string
    SourcePluginID string
    Target         ClientTargetSelector
    ActionID       string
    Params         json.RawMessage
    RequiredCaps   []Capability
    DeadlineUnixNS int64
    IdempotencyKey string
    SchemaVersion  int
    ReplyMode      ReplyMode // none | accepted | completed
}

type ClientTargetSelector struct {
    SessionID   string
    ClientKind  ClientKind
    WorkspaceID string

    // 只能由目标 client 本地解释。
    ActiveClient bool
    ActivePanel  bool

    // 如果 action 与 terminal 有关，必须显式传 TerminalRef。
    TerminalRef *TerminalRef

    // broadcast 只允许 presence/capability/invalidation 类 action；
    // destructive action 必须显式 multicast 并逐目标授权。
    Broadcast bool
}

type ClientControlDelivery struct {
    RequestID string
    Targets   []ClientDeliveryTarget
}

type ClientDeliveryTarget struct {
    SessionID string
    Status    DeliveryStatus // queued | denied | offline | unsupported | timeout
    Reason    string
}
```

建议方法：

```text
client.control.call
client.control.watch
client.control.respond
```

`client.control.watch` 应该是显式 session filter 的 control mailbox，不应该进入普通 broad events。daemon 返回 `queued` 只代表请求进入目标 session mailbox，不代表 UI 已执行。

### 响应 control call

```go
type ClientControlRespondParams struct {
    RequestID string
    SessionID string
    Status    ControlStatus // accepted | denied | unsupported | busy | executed | failed | timeout
    Error     string
    Result    json.RawMessage
    Epoch     uint64
}
```

目标 client 必须检查：

- action 是否存在。
- client kind 是否支持。
- request 是否过期。
- request epoch/session 是否仍有效。
- 插件 grant 是否包含该 action 所需 capability。
- danger action 是否需要用户确认。

## Daemon broker 伪代码

```go
type ClientSessionRegistry struct {
    sessions map[string]*ClientSession
    grants   GrantVerifier
}

func (r *ClientSessionRegistry) Register(ctx context.Context, p ClientSessionRegisterParams) error {
    if p.SessionID == "" || p.ClientKind == "" {
        return ErrInvalidSession
    }

    session := &ClientSession{
        ID:           p.SessionID,
        Kind:         p.ClientKind,
        WorkspaceID:  p.WorkspaceID,
        Actions:      indexActions(p.Actions),
        Capabilities: p.Capabilities,
        Epoch:        p.Epoch,
        Inbox:        NewBoundedControlInbox(),
    }

    r.sessions[p.SessionID] = session
    publishPresenceChanged(session)
    return nil
}

func (r *ClientSessionRegistry) ControlCall(ctx context.Context, p ClientControlCallParams) (ClientControlDelivery, error) {
    if time.Now().UnixNano() > p.DeadlineUnixNS {
        return ClientControlDelivery{}, ErrExpired
    }

    if err := r.grants.Verify(p.SourcePluginID, p.RequiredCaps); err != nil {
        return ClientControlDelivery{}, err
    }

    targets := r.resolveTargets(p.Target)
    if len(targets) == 0 {
        return ClientControlDelivery{RequestID: p.RequestID}, nil
    }

    delivery := ClientControlDelivery{RequestID: p.RequestID}
    for _, target := range targets {
        if p.Target.Broadcast && isExecutableOrDangerous(p.ActionID) {
            delivery.Targets = append(delivery.Targets, denied(target, "broadcast execution is not allowed"))
            continue
        }

        if !target.SupportsAction(p.ActionID) {
            delivery.Targets = append(delivery.Targets, unsupported(target))
            continue
        }

        req := ClientControlRequest{
            RequestID:      p.RequestID,
            SourcePluginID: p.SourcePluginID,
            ActionID:       p.ActionID,
            Params:         p.Params,
            Target:         p.Target,
            DeadlineUnixNS: p.DeadlineUnixNS,
            IdempotencyKey: p.IdempotencyKey,
            TargetEpoch:    target.Epoch,
        }

        if ok := target.Inbox.TryEnqueue(req); !ok {
            delivery.Targets = append(delivery.Targets, busy(target))
            continue
        }
        delivery.Targets = append(delivery.Targets, queued(target))
    }
    return delivery, nil
}

func (r *ClientSessionRegistry) WatchControl(ctx context.Context, sessionID string) (<-chan ClientControlRequest, error) {
    session := r.sessions[sessionID]
    if session == nil {
        return nil, ErrUnknownSession
    }
    return session.Inbox.Subscribe(ctx), nil
}
```

daemon broker 的禁止事项：

- 不解释 active panel/current focus。
- 不把 request 写入 workbench storage。
- 不把 `TerminalID` 当跨 endpoint target。
- 不把 `queued` 当作执行成功。
- 不用普通 event broadcast 传 destructive action。

## TUI client 启动伪代码

```go
func StartTUI(ctx context.Context, runtime *AppRuntime, daemon protocol.Client) error {
    sessionID := randomSessionID()
    actions := BuildTUIActionCatalog()

    err := daemon.ClientSessionRegister(ctx, ClientSessionRegisterParams{
        SessionID:   sessionID,
        ClientKind:  "tui",
        WorkspaceID: runtime.State().Workspace.ID,
        InstanceID:  runtime.InstanceID(),
        PID:         os.Getpid(),
        TTY:         detectTTY(),
        Actions:     actions.ExportSpecs(),
        Capabilities: []ClientCapability{
            {Name: "client.panel", Version: 1},
            {Name: "client.tab", Version: 1},
            {Name: "terminal-ref-routing", Version: 1},
        },
        Epoch: runtime.SessionEpoch(),
    })
    if err != nil {
        return err
    }

    controlEvents, err := daemon.ClientControlWatch(ctx, sessionID)
    if err != nil {
        return err
    }

    go func() {
        for req := range controlEvents {
            runtime.Post(ClientControlRequestMsg{Request: req})
        }
    }()

    return nil
}
```

TUI 不允许 protocol watcher 直接改 `StateRoot`。它只能 post message，让 reducer/effect path 处理。

## TUI action dispatch 伪代码

```go
func ReduceClientControl(state *StateRoot, msg ClientControlRequestMsg) ([]Effect, StateMutation) {
    req := msg.Request
    if req.IsExpired() {
        return respond(req, "timeout", "request expired"), NoMutation
    }

    spec, ok := state.ActionRegistry.Lookup(req.ActionID)
    if !ok {
        return respond(req, "unsupported", "unknown action"), NoMutation
    }

    if !state.CapabilityStore.Allows(req.SourcePluginID, spec.RequiredCaps) {
        return respond(req, "denied", "capability denied"), NoMutation
    }

    if spec.Danger == DangerDestructive && !state.TrustStore.IsTrusted(req.SourcePluginID, spec.ID) {
        return []Effect{
            ShowConfirmationPromptEffect(req),
        }, NoMutation
    }

    return InvokeLocalAction(state, req, spec)
}

func InvokeLocalAction(state *StateRoot, req ClientControlRequest, spec ActionSpec) ([]Effect, StateMutation) {
    switch req.ActionID {
    case "termx.client.panel.close_and_kill_terminal":
        return reduceClosePanelAndKillTerminal(state, req)
    case "termx.client.panel.create_terminal_bind":
        return reduceCreatePanelTerminalBind(state, req)
    case "termx.client.tab.activate":
        return reduceActivateTab(state, req)
    default:
        return respond(req, "unsupported", "action not implemented by this client"), NoMutation
    }
}
```

## 示例：关闭 panel 并 kill terminal

这个动作由 client 本地解释 active panel。daemon 不知道 active panel 是谁，也不应该猜。

```go
func reduceClosePanelAndKillTerminal(state *StateRoot, req ClientControlRequest) ([]Effect, StateMutation) {
    panel := state.Workbench.ActivePanel()
    if panel == nil {
        return respond(req, "failed", "no active panel"), NoMutation
    }

    ref, ok := panel.TerminalRef()
    if !ok {
        mutation := state.Workbench.ClosePanel(panel.ID)
        return []Effect{
            SaveWorkbenchEffect(mutation.Version),
            RespondControlEffect(req, "executed", nil),
        }, mutation
    }

    // reducer 只产生命令意图；terminal kill 通过 EndpointManager 找 owning endpoint。
    mutation := state.Workbench.ClosePanel(panel.ID)
    effects := []Effect{
        SaveWorkbenchEffect(mutation.Version),
        KillTerminalEffect{
            RequestID: req.RequestID,
            Ref:       ref,
            Reason:    "close_panel_and_kill_terminal",
        },
    }
    return effects, mutation
}

func RunKillTerminalEffect(ctx context.Context, endpointManager EndpointManager, e KillTerminalEffect) Msg {
    svc, err := endpointManager.TerminalService(e.Ref.EndpointID)
    if err != nil {
        return ClientActionFailedMsg{RequestID: e.RequestID, Error: err}
    }
    if err := svc.Kill(ctx, e.Ref.TerminalID); err != nil {
        return ClientActionFailedMsg{RequestID: e.RequestID, Error: err}
    }
    return ClientActionExecutedMsg{RequestID: e.RequestID}
}
```

失败语义：

- panel 已关闭但 kill 失败时，action 应返回 `failed` 并带具体错误；后续可考虑 compensation UI，例如 toast 或 undo close，但不能把 daemon 状态伪装成已 kill。
- 如果同一个 request 因重试重复到达，TUI 需要用 `IdempotencyKey` 避免重复关闭其他 panel。

## 示例：新建 panel、创建 terminal 并绑定

这个动作要跨 client UI 和 daemon terminal lifecycle。推荐分阶段消息化，不在一个 service 回调里直接改 state。

```go
type CreatePanelTerminalBindParams struct {
    EndpointID EndpointID
    Command    []string
    Workdir    string
    PanelSide  SplitSide
}

func reduceCreatePanelTerminalBind(state *StateRoot, req ClientControlRequest) ([]Effect, StateMutation) {
    params := decodeCreatePanelParams(req.Params)
    if !state.EndpointStore.Has(params.EndpointID) {
        return respond(req, "failed", "unknown endpoint"), NoMutation
    }

    panelID := state.Workbench.ReservePanel(params.PanelSide)
    mutation := state.Workbench.MarkPanelPending(panelID, "creating-terminal")

    return []Effect{
        SaveWorkbenchEffect(mutation.Version),
        CreateTerminalEffect{
            RequestID:  req.RequestID,
            PanelID:    panelID,
            EndpointID: params.EndpointID,
            Command:    params.Command,
            Workdir:    params.Workdir,
        },
    }, mutation
}

func RunCreateTerminalEffect(ctx context.Context, endpointManager EndpointManager, e CreateTerminalEffect) Msg {
    svc, err := endpointManager.TerminalService(e.EndpointID)
    if err != nil {
        return CreateTerminalFailedMsg{RequestID: e.RequestID, PanelID: e.PanelID, Error: err}
    }

    terminal, err := svc.Create(ctx, CreateTerminalRequest{
        Command: e.Command,
        Workdir: e.Workdir,
    })
    if err != nil {
        return CreateTerminalFailedMsg{RequestID: e.RequestID, PanelID: e.PanelID, Error: err}
    }

    return TerminalCreatedForPanelMsg{
        RequestID: e.RequestID,
        PanelID:   e.PanelID,
        Ref: TerminalRef{
            EndpointID: e.EndpointID,
            TerminalID: terminal.ID,
        },
    }
}

func reduceTerminalCreatedForPanel(state *StateRoot, msg TerminalCreatedForPanelMsg) ([]Effect, StateMutation) {
    mutation := state.Workbench.BindPanelToTerminal(msg.PanelID, msg.Ref)
    return []Effect{
        SaveWorkbenchEffect(mutation.Version),
        AttachLiveSurfaceEffect{PanelID: msg.PanelID, Ref: msg.Ref},
        RespondControlEffectByRequestID(msg.RequestID, "executed", nil),
    }, mutation
}
```

失败语义：

- create terminal 失败时，reserved panel 应恢复为空槽位或被删除，具体策略由 client UI 决定。
- terminal 创建成功但 bind 失败时，不能 kill terminal 作为隐式补偿，除非 action 明确声明 compensation policy。
- 所有 storage/workbench 写入都要走版本/CAS，不能把 plugin action 写成绕过 reducer 的状态修正。

## 插件 runner 策略

第一阶段不建议把主线押在某一种嵌入式语言上。推荐顺序：

1. `builtin runner`：内建 action，先跑通 action registry 和 client control 协议。
2. `external runner`：插件作为独立进程，通过 stdio JSON-RPC 或 daemon socket 访问受限 TermX API。语言无关，最容易调试。
3. `wasm runner`：第三方插件的默认安全 sandbox，适合借鉴 Zellij 的 WASM/WASI 权限模型。
4. `expr runner`：仅用于 keybinding condition、manifest activation condition、filter 表达式，不作为通用插件语言。
5. `goja/risor/lua runner`：可作为后续可选 runner，不应成为第一版插件 contract。

external runner 启动环境示例：

```text
TERMX_PLUGIN_ID=example.close-panel
TERMX_PLUGIN_HOST=client
TERMX_CLIENT_KIND=tui
TERMX_CLIENT_SESSION_ID=tui-01H...
TERMX_WORKSPACE_ID=default
TERMX_DAEMON_ENDPOINT=local
TERMX_PLUGIN_GRANT_REF=grant:...
```

如果插件从 terminal 进程内部启动，它通常只能天然知道当前 daemon endpoint 和 terminal identity；它不一定知道哪个 TUI session 正在显示它。需要控制 UI 时，应通过 `client.session.list` 查找 session，或由 TUI 启动时注入 `TERMX_CLIENT_SESSION_ID`。

## 权限与安全

权限分两段校验：

1. daemon 校验插件是否可以访问 daemon-owned capability，例如 `terminal.kill`、`history.read`、`storage.write:prefix`。
2. client 校验插件是否可以访问 client-owned capability，例如 `client.panel.close`、`client.clipboard.write`、`client.prompt.show`。

Grant 至少绑定：

```text
plugin_id
daemon/device fingerprint
client/workspace audience
capability list
expiry
revocation id
api version
```

危险能力示例：

- `terminal.kill`
- `terminal.input.write`
- `client.clipboard.read`
- `client.panel.close`
- `workspace.storage.write`
- `network.open`
- `file.read` / `file.write`

默认策略：

- 第三方插件没有隐式 full access。
- destructive action 默认需要确认。
- broadcast destructive action 默认禁止。
- request 必须有 deadline。
- client reconnect 或 session epoch 变化后，过期 request 不得补执行。

## 生命周期

daemon 启动：

```text
load daemon manifests
verify grants
start daemon-scoped plugins with onDaemonStart
subscribe daemon events for onTerminalCreated/onTerminalExited/onStorageChanged
```

client 启动：

```text
load client-compatible manifests
register client session and supported actions
start eager client plugins
lazy activate plugins on action/view/status contribution
watch client.control mailbox
```

workspace 打开：

```text
check workspace trust
load workspace manifest
register workspace actions/storage prefix
start workspace-scoped daemon/client parts if allowed
```

关闭或 reload：

```text
send deactivate to plugin runner
cancel pending requests by generation
unregister contributions
unregister client session on normal exit
daemon cleans session on connection close
```

## 第一阶段落地顺序

1. 先把现有快捷键整理成 `ActionSpec` catalog，footer/help/command palette 都引用 action 元数据。
2. 增加内建 action registry，支持一个 action 多个 key sequence，例如 `ctrl+t 1` 和 `ctrl+1` 都触发 tab activate。
3. 增加 `client.session.register/list` 和 `client.control.call/watch/respond` 的协议草案与 harness。
4. TUI 启动时注册 session，watch control mailbox，把请求投递到 AppRuntime message。
5. 先以内建 action 跑通 `close panel + kill terminal` 和 `create panel + create terminal + bind`。
6. 增加 manifest parser，但只支持 builtin/external runner。
7. 再引入 capability grant 和安装信任 UX。
8. 最后评估 WASM 或具体脚本 runner。

## 非目标

- 第一阶段不实现插件市场。
- 第一阶段不让插件直接渲染复杂 WebView。
- 第一阶段不实现 tmux 式 shell plugin。
- 第一阶段不让 daemon 解释 TUI layout、focus、copy mode 或 floating state。
- 第一阶段不做 App/Web/GUI 的真实 UI 插件实现，只把 client session control 协议设计成可扩展到它们。
