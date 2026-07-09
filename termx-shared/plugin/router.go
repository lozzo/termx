package plugin

import "fmt"

const (
	defaultMaxRouterDepth     = 8
	defaultMaxTraceDeliveries = 64
)

// HookRouterConfig 描述 hook router 的循环和投递预算。
// 这些限制由 host 执行，插件不能通过 manifest 或运行期请求放大。
type HookRouterConfig struct {
	MaxDepth           int
	MaxTraceDeliveries int
	EventCatalog       []EventSpec
}

// HookRouter 是 host-local 的 hook 调度器纯模型。
// 它只决定哪些订阅应收到事件，不直接执行插件、不做 IO、不修改宿主状态。
type HookRouter struct {
	maxDepth           int
	maxTraceDeliveries int
	eventsByType       map[EventType]EventSpec
	seen               map[TraceDedupeKey]struct{}
	deliveriesByTrace  map[string]int
}

// TraceDedupeKey 是同一 trace 内的 hook 去重键。
// 它必须带 source/session/daemon/endpoint 作用域，避免不同 client 或 endpoint 的同名对象互相吞事件。
type TraceDedupeKey struct {
	TraceID          string
	SourceHost       HostPlacement
	SourceSession    string
	DaemonID         string
	DaemonTerminalID TerminalID
	EndpointID       EndpointID
	TerminalID       TerminalID
	PluginID         PluginID
	EventType        EventType
	ObjectKind       string
	ObjectID         string
}

// DispatchResult 描述一次 hook dispatch 的纯结果。
// Deliveries 是应投递给插件的事件副本，Drops 是被 policy 拒绝的诊断记录。
type DispatchResult struct {
	Deliveries []HookDeliveryAttempt
	Drops      []HookDrop
}

// HookDeliveryAttempt 描述一次计划中的插件投递。
// Event 是已递增 depth/actor path 的副本，runner 可据此执行 handler。
type HookDeliveryAttempt struct {
	PluginID PluginID
	Event    HookEvent
	Delivery HookDelivery
}

// HookDrop 描述 hook 被 router policy 拒绝的原因。
// host 可把它聚合成低频 hook.dropped 诊断事件。
type HookDrop struct {
	PluginID PluginID
	EventID  string
	Reason   HookDropReason
}

// HookDropReason 是 hook router 拒绝投递的稳定原因。
// 它用于 harness 和后续审计，不应包含动态错误文本。
type HookDropReason string

const (
	// DropMaxDepth 表示 trace depth 已达到上限。
	DropMaxDepth HookDropReason = "max_depth"
	// DropTraceBudget 表示同一 trace 已超过 host 允许的投递预算。
	DropTraceBudget HookDropReason = "trace_budget"
	// DropSelfCaused 表示订阅默认不接收自己因果链造成的事件。
	DropSelfCaused HookDropReason = "self_caused"
	// DropDuplicate 表示同一 trace/source/object/plugin 已经投递过。
	DropDuplicate HookDropReason = "duplicate"
	// DropScope 表示事件不在订阅授权 scope 内。
	DropScope HookDropReason = "scope"
	// DropEventType 表示事件类型不在订阅 event catalog 中。
	DropEventType HookDropReason = "event_type"
	// DropSourceHost 表示系统事件的 SourceHost 与 host 事件目录中的 truth owner 不一致。
	DropSourceHost HookDropReason = "source_host"
)

// NewHookRouter 创建 host-local hook router。
// 它保存 trace 内去重和预算状态，调用方应按 host/session 生命周期创建和清理。
func NewHookRouter(config HookRouterConfig) *HookRouter {
	maxDepth := config.MaxDepth
	if maxDepth <= 0 {
		maxDepth = defaultMaxRouterDepth
	}
	maxTraceDeliveries := config.MaxTraceDeliveries
	if maxTraceDeliveries <= 0 {
		maxTraceDeliveries = defaultMaxTraceDeliveries
	}
	eventsByType := make(map[EventType]EventSpec, len(config.EventCatalog))
	for _, event := range config.EventCatalog {
		if event.Type == "" {
			continue
		}
		eventsByType[event.Type] = cloneEventSpec(event)
	}
	return &HookRouter{
		maxDepth:           maxDepth,
		maxTraceDeliveries: maxTraceDeliveries,
		eventsByType:       eventsByType,
		seen:               make(map[TraceDedupeKey]struct{}),
		deliveriesByTrace:  make(map[string]int),
	}
}

// Dispatch 根据订阅和 router policy 计算 hook 投递计划。
// 系统事件必须由 host 产生；router 不接受插件直接 emit termx.* 的入口。
func (router *HookRouter) Dispatch(event HookEvent, subscriptions []HookSubscription) DispatchResult {
	var result DispatchResult
	for _, sub := range subscriptions {
		if !router.sourceHostMatchesCatalog(event) {
			result.Drops = append(result.Drops, dropFor(sub, event, DropSourceHost))
			continue
		}
		if !subscriptionMatchesEventType(sub, event.Type) {
			result.Drops = append(result.Drops, dropFor(sub, event, DropEventType))
			continue
		}
		if !scopeMatches(sub.Scope, event) {
			result.Drops = append(result.Drops, dropFor(sub, event, DropScope))
			continue
		}
		if event.Trace.Depth >= router.maxDepth {
			result.Drops = append(result.Drops, dropFor(sub, event, DropMaxDepth))
			continue
		}
		if !sub.ReceiveSelfCaused && event.Trace.ContainsActor(sub.PluginID) {
			result.Drops = append(result.Drops, dropFor(sub, event, DropSelfCaused))
			continue
		}
		key := DedupeKey(sub.PluginID, event)
		if _, ok := router.seen[key]; ok {
			result.Drops = append(result.Drops, dropFor(sub, event, DropDuplicate))
			continue
		}
		if router.traceBudgetExceeded(event.Trace.TraceID) {
			result.Drops = append(result.Drops, dropFor(sub, event, DropTraceBudget))
			continue
		}
		next := event.Clone()
		next.Trace.Depth++
		next.Trace.LastPluginID = sub.PluginID
		next.Trace.ActorPath = append(next.Trace.ActorPath, sub.PluginID)
		router.seen[key] = struct{}{}
		router.deliveriesByTrace[event.Trace.TraceID]++
		result.Deliveries = append(result.Deliveries, HookDeliveryAttempt{
			PluginID: sub.PluginID,
			Event:    next,
			Delivery: sub.Delivery,
		})
	}
	return result
}

func (router *HookRouter) sourceHostMatchesCatalog(event HookEvent) bool {
	if len(router.eventsByType) == 0 {
		return true
	}
	spec, ok := router.eventsByType[event.Type]
	if !ok {
		return true
	}
	return spec.SourceHost == event.SourceHost
}

// DedupeKey 生成同一 trace 内用于防循环和防重复投递的键。
// key 包含 source/session/daemon/endpoint identity，避免同名 terminal 或 panel 互相影响。
func DedupeKey(pluginID PluginID, event HookEvent) TraceDedupeKey {
	terminalID := TerminalID("")
	endpointID := event.EndpointID
	if event.TerminalRef != nil {
		terminalID = event.TerminalRef.TerminalID
		if endpointID == "" {
			endpointID = event.TerminalRef.EndpointID
		}
	}
	return TraceDedupeKey{
		TraceID:          event.Trace.TraceID,
		SourceHost:       event.SourceHost,
		SourceSession:    event.SourceSession,
		DaemonID:         event.DaemonID,
		DaemonTerminalID: event.DaemonTerminalID,
		EndpointID:       endpointID,
		TerminalID:       terminalID,
		PluginID:         pluginID,
		EventType:        event.Type,
		ObjectKind:       event.ObjectKind,
		ObjectID:         event.ObjectID,
	}
}

// ValidateHookSubscription 执行 host-side subscription 基础校验。
// 它只校验 router 必需字段；scope 粒度和 wildcard 授权由 host policy/catalog 裁决。
func ValidateHookSubscription(sub HookSubscription) error {
	if sub.PluginID == "" {
		return fmt.Errorf("plugin id is required")
	}
	if sub.Host == "" {
		return fmt.Errorf("host placement is required")
	}
	if len(sub.EventTypes) == 0 {
		return fmt.Errorf("event types are required")
	}
	if sub.Delivery.Mode == "" {
		return fmt.Errorf("delivery mode is required")
	}
	return nil
}

func (router *HookRouter) traceBudgetExceeded(traceID string) bool {
	if traceID == "" {
		return false
	}
	return router.deliveriesByTrace[traceID] >= router.maxTraceDeliveries
}

func dropFor(sub HookSubscription, event HookEvent, reason HookDropReason) HookDrop {
	return HookDrop{PluginID: sub.PluginID, EventID: event.EventID, Reason: reason}
}

func subscriptionMatchesEventType(sub HookSubscription, eventType EventType) bool {
	for _, candidate := range sub.EventTypes {
		if candidate == eventType {
			return true
		}
	}
	return false
}

func scopeMatches(scope HookScope, event HookEvent) bool {
	if scope.WorkspaceID != "" && scope.WorkspaceID != event.WorkspaceID {
		return false
	}
	if scope.ClientSessionID != "" && scope.ClientSessionID != event.SourceSession {
		return false
	}
	if scope.EndpointID != "" {
		endpointID := event.EndpointID
		if endpointID == "" && event.TerminalRef != nil {
			endpointID = event.TerminalRef.EndpointID
		}
		if scope.EndpointID != endpointID {
			return false
		}
	}
	if scope.TerminalRef != nil {
		if event.TerminalRef == nil || !scope.TerminalRef.Equal(*event.TerminalRef) {
			return false
		}
	}
	if scope.DaemonID != "" && scope.DaemonID != event.DaemonID {
		return false
	}
	if scope.DaemonTerminalID != "" && scope.DaemonTerminalID != event.DaemonTerminalID {
		return false
	}
	return true
}
