// Package plugin 定义 TermX 插件 action / hook / message trace 的共享纯模型。
package plugin

import (
	"encoding/json"
	"time"
)

// PluginID 是已安装插件的稳定身份。
// 它由 manifest/安装源决定，不能由外部 runner 在请求体里自报。
type PluginID string

// Capability 描述插件被授予的最小权限。
// capability 必须由 host 根据 action/hook catalog 和 grant 推导，不能由插件请求体声明。
type Capability string

// ActionID 是可被快捷键、命令面板、插件或外部 control call 触发的 typed command。
// 内建 action 使用 termx.* 命名空间，第三方 action 必须挂在插件自己的 publisher/plugin 命名空间下。
type ActionID string

// EventType 是系统 hook 或插件私有 event 的类型名。
// termx.* 系统事件只能由拥有 truth 的 host 发布，插件只能发布自己命名空间下的私有事件。
type EventType string

// HostPlacement 描述插件代码或 hook 事件所在的 host 边界。
// daemon/client/workspace 的 truth owner 不同，hook 不能跨边界解释对方状态。
type HostPlacement string

const (
	// HostDaemon 表示事件或 action side effect 属于 daemon/core 边界。
	HostDaemon HostPlacement = "daemon"
	// HostClient 表示事件或 action side effect 属于当前 TUI/App/Web/GUI client session 边界。
	HostClient HostPlacement = "client"
	// HostWorkspace 表示事件或 action side effect 属于 workspace 信任域。
	HostWorkspace HostPlacement = "workspace"
	// HostOneShot 表示插件代码由 host 按需启动，执行完即退出。
	HostOneShot HostPlacement = "one_shot"
)

// ClientKind 描述正在运行的客户端类型。
// 它用于 action/hook capability 协商，不能被 daemon 用来解释具体 UI state。
type ClientKind string

const (
	// ClientKindTUI 表示 terminal UI client。
	ClientKindTUI ClientKind = "tui"
	// ClientKindApp 表示移动或桌面 App client。
	ClientKindApp ClientKind = "app"
	// ClientKindWeb 表示 Web client。
	ClientKindWeb ClientKind = "web"
	// ClientKindGUI 表示未来 GUI client。
	ClientKindGUI ClientKind = "gui"
)

// EndpointID 是 client 本地 connection registry 中 endpoint 的稳定 ID。
// daemon/core hook 源头不得生成 EndpointID；它只能由 client 侧 EndpointManager 补充。
type EndpointID string

// TerminalID 是单个 daemon/endpoint 内唯一的 terminal identity。
// 跨 endpoint 场景必须和 EndpointID 组合成 TerminalRef。
type TerminalID string

// TerminalRef 是 client 侧跨 endpoint terminal 引用。
// daemon/core 只拥有 daemon-local TerminalID，不能凭空生成 TerminalRef。
type TerminalRef struct {
	EndpointID EndpointID `json:"endpoint_id"`
	TerminalID TerminalID `json:"terminal_id"`
}

// Equal 判断两个 TerminalRef 是否指向同一 client 视角 terminal。
// 它同时比较 EndpointID 和 TerminalID，避免跨 endpoint 同名 terminal 被误合并。
func (ref TerminalRef) Equal(other TerminalRef) bool {
	return ref.EndpointID == other.EndpointID && ref.TerminalID == other.TerminalID
}

// Empty 判断 TerminalRef 是否缺少 endpoint 或 terminal identity。
// 空引用不能用于跨 endpoint side effect。
func (ref TerminalRef) Empty() bool {
	return ref.EndpointID == "" || ref.TerminalID == ""
}

// ActionScope 描述 action 的执行 authority 所在边界。
// client action 由目标 client session 裁决，daemon action 由 owning daemon 裁决。
type ActionScope string

const (
	// ActionScopeClient 表示 action 的 UI 语义由目标 client session 裁决。
	ActionScopeClient ActionScope = "client"
	// ActionScopeDaemon 表示 action 的 side effect 由 daemon/core 裁决。
	ActionScopeDaemon ActionScope = "daemon"
	// ActionScopeWorkspace 表示 action 属于 workspace 信任域。
	ActionScopeWorkspace ActionScope = "workspace"
)

// DangerLevel 描述 action 的风险级别。
// destructive action 默认需要确认或显式 trusted grant。
type DangerLevel string

const (
	// DangerNone 表示普通 action。
	DangerNone DangerLevel = "none"
	// DangerDestructive 表示会删除、kill、覆盖或造成不可逆影响的 action。
	DangerDestructive DangerLevel = "destructive"
)

// ActionSpec 是 action registry 中的宿主侧事实。
// RequiredCaps 由 host/catalog 使用，不允许外部 runner 在调用请求中自报 capability。
type ActionSpec struct {
	ID                   ActionID
	OwnerPluginID        PluginID
	Scope                ActionScope
	SupportedClientKinds []ClientKind
	RequiredCaps         []Capability
	ClientRequiredCaps   []Capability
	DaemonRequiredCaps   []Capability
	Danger               DangerLevel
	ParamsSchema         string
	Idempotent           bool
}

// ClientCaps 返回 client session 必须本地校验的 capability 列表。
// 这些权限只覆盖 UI 投影和 client-owned state，不替代 daemon side effect grant。
func (spec ActionSpec) ClientCaps() []Capability {
	return append([]Capability(nil), spec.ClientRequiredCaps...)
}

// DaemonCaps 返回 daemon/core side effect 必须校验的 capability 列表。
// TUI 解析出 TerminalRef 后，仍必须回到 owning endpoint 使用这些 capability 校验。
func (spec ActionSpec) DaemonCaps() []Capability {
	return append([]Capability(nil), spec.DaemonRequiredCaps...)
}

// InvocationSource 描述 action 调用来源。
// 外部 runner 不能伪造该字段；host 必须从 runner session / grant 派生最终 source identity。
type InvocationSource struct {
	PluginID PluginID
	Kind     string
}

// ActionTarget 描述 action 目标选择器。
// active/current 这类 selector 只能由目标 client 本地解释，不能由 daemon 推断。
type ActionTarget struct {
	SessionID   string       `json:"session_id,omitempty"`
	ClientKind  ClientKind   `json:"client_kind,omitempty"`
	WorkspaceID string       `json:"workspace_id,omitempty"`
	ActivePanel bool         `json:"active_panel,omitempty"`
	TerminalRef *TerminalRef `json:"terminal_ref,omitempty"`
	Broadcast   bool         `json:"broadcast,omitempty"`
}

// ActionInvocation 是插件或快捷键发起的 action 请求模型。
// Source 和完整 MessageTrace 由 host 派生；外部 runner 只能携带 opaque TraceParent。
type ActionInvocation struct {
	RequestID      string
	ActionID       ActionID
	Params         []byte
	Source         InvocationSource
	Target         ActionTarget
	TraceParent    TraceParent
	Deadline       time.Time
	IdempotencyKey string
}

// HookDeliveryMode 描述 hook 投递队列的背压语义。
// strict_queued 不能静默丢事件；latest/coalesced 才是 lossy 模式。
type HookDeliveryMode string

const (
	// DeliveryStrictQueued 表示事件不能静默 drop，满队列时必须断开、busy 或 gap marker。
	DeliveryStrictQueued HookDeliveryMode = "strict_queued"
	// DeliveryQueued 表示普通队列投递，可按 host policy 对慢订阅降级或断开。
	DeliveryQueued HookDeliveryMode = "queued"
	// DeliveryLatest 表示只保留最新事件，适合 resize/mode 等 latest-only 事实。
	DeliveryLatest HookDeliveryMode = "latest"
	// DeliveryCoalesced 表示按对象合并事件，适合 PTY activity 或 resize storm。
	DeliveryCoalesced HookDeliveryMode = "coalesced"
)

// Lossy 判断该 delivery mode 是否允许 latest/coalesce/drop。
// lifecycle/audit 这类严格事件不能依赖 lossy 投递。
func (mode HookDeliveryMode) Lossy() bool {
	return mode == DeliveryLatest || mode == DeliveryCoalesced
}

// HookDelivery 描述单个订阅的投递策略。
// QueueLimit、Debounce 和 Throttle 是 host-enforced policy，不由插件运行期随意放大。
type HookDelivery struct {
	Mode       HookDeliveryMode
	QueueLimit int
	Debounce   time.Duration
	Throttle   time.Duration
}

// HookScope 描述 hook 订阅的授权和过滤范围。
// PTY activity 即使没有 raw output，也必须受 workspace/client/endpoint/terminal 范围约束。
type HookScope struct {
	WorkspaceID      string
	ClientSessionID  string
	EndpointID       EndpointID
	TerminalRef      *TerminalRef
	DaemonID         string
	DaemonTerminalID TerminalID
}

// HookFilter 是预留给 manifest 条件表达式和 host policy 的结构化过滤入口。
// 第一阶段测试只覆盖 scope 过滤，不执行任意脚本表达式。
type HookFilter struct {
	Key   string
	Value string
}

// HookSubscription 是 host 解析 manifest、event catalog 和 grant 后得到的内部订阅。
// ResolvedCaps 由 host 推导，外部 runner 不能在 subscribe 请求里自报它。
type HookSubscription struct {
	PluginID          PluginID
	Host              HostPlacement
	EventTypes        []EventType
	Scope             HookScope
	Filters           []HookFilter
	Delivery          HookDelivery
	ResolvedCaps      []Capability
	ReceiveSelfCaused bool
}

// HookEvent 是系统 hook 投递给插件前的统一 envelope。
// daemon/core 只能填写 daemon-local identity；client 可在 EndpointManager 后补 TerminalRef。
type HookEvent struct {
	EventID       string        `json:"event_id"`
	Type          EventType     `json:"type"`
	SourceHost    HostPlacement `json:"source_host"`
	SourceSession string        `json:"source_session,omitempty"`
	ClientKind    ClientKind    `json:"client_kind,omitempty"`
	WorkspaceID   string        `json:"workspace_id,omitempty"`

	DaemonID         string     `json:"daemon_id,omitempty"`
	DaemonTerminalID TerminalID `json:"daemon_terminal_id,omitempty"`

	EndpointID  EndpointID   `json:"endpoint_id,omitempty"`
	TerminalRef *TerminalRef `json:"terminal_ref,omitempty"`
	ObjectKind  string       `json:"object_kind,omitempty"`
	ObjectID    string       `json:"object_id,omitempty"`

	Sequence uint64          `json:"sequence,omitempty"`
	Time     time.Time       `json:"time"`
	Trace    MessageTrace    `json:"trace"`
	Payload  json.RawMessage `json:"payload,omitempty"`
	Lossy    bool            `json:"lossy,omitempty"`
}

// Clone 返回 hook event 的深拷贝。
// router 修改 Trace/ActorPath 时必须复制，避免污染调用方持有的事件。
func (event HookEvent) Clone() HookEvent {
	out := event
	out.Payload = append(json.RawMessage(nil), event.Payload...)
	out.Trace = event.Trace.Clone()
	if event.TerminalRef != nil {
		ref := *event.TerminalRef
		out.TerminalRef = &ref
	}
	return out
}
