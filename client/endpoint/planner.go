package endpoint

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// RouteSelectionRequest 是 RouteSelectionPlanner 的完整纯领域输入。
// SupportedRouteKinds 与 AvailableCredentialRefs 必须由调用方从当前平台能力和 secure-store 索引生成；planner 不读取平台状态、credential body、Cloud 或 registry。
type RouteSelectionRequest struct {
	Endpoint                Endpoint
	Intent                  ConnectIntent
	RouteOverride           RouteID
	Generation              SessionGeneration
	SupportedRouteKinds     []RouteKind
	AvailableCredentialRefs []string
}

// RouteSelectionPlan 是单个 Endpoint generation 的不可变 route attempt 分组。
// Groups 只能通过 getter 取得深拷贝；runtime 后续可以按 StartDelay 启动 attempt，但 winner、取消和 cleanup 不属于 planner。
type RouteSelectionPlan struct {
	endpointID  EndpointID
	generation  SessionGeneration
	groups      []RouteAttemptGroup
	diagnostics []RoutePlanDiagnostic
}

// Diagnostics 返回按 RouteID 稳定排序的过滤诊断深拷贝。
// 诊断只解释 route 为什么没有进入本次计划，不包含 credential body、网络错误或后续 dial 状态。
func (plan RouteSelectionPlan) Diagnostics() []RoutePlanDiagnostic {
	return append([]RoutePlanDiagnostic(nil), plan.diagnostics...)
}

// RoutePlanDiagnostic 是 planner 对未进入计划的持久 route 给出的稳定、脱敏原因。
type RoutePlanDiagnostic struct {
	RouteID RouteID
	Code    ErrorCode
	Reason  RoutePlanDiagnosticReason
}

// RoutePlanDiagnosticReason 是 C3B 可产生的 route 过滤原因，不表示 dial 失败。
type RoutePlanDiagnosticReason string

const (
	// RoutePlanRouteDisabled 表示 route 被用户或配置禁用。
	RoutePlanRouteDisabled RoutePlanDiagnosticReason = "route_disabled"
	// RoutePlanManualOnly 表示自动计划跳过只允许显式选择的 route。
	RoutePlanManualOnly RoutePlanDiagnosticReason = "manual_only"
	// RoutePlanPlatformUnsupported 表示当前平台没有该 route primitive。
	RoutePlanPlatformUnsupported RoutePlanDiagnosticReason = "platform_unsupported"
	// RoutePlanCredentialUnavailable 表示 route 引用的 credential 当前不可用。
	RoutePlanCredentialUnavailable RoutePlanDiagnosticReason = "credential_unavailable"
	// RoutePlanAutomaticRaceUnsupported 表示 route 可单独使用，但当前阶段不参加 local/SSH 自动竞速。
	RoutePlanAutomaticRaceUnsupported RoutePlanDiagnosticReason = "automatic_race_unsupported"
)

// EndpointID 返回本计划唯一对应的 Endpoint。
func (plan RouteSelectionPlan) EndpointID() EndpointID { return plan.endpointID }

// Generation 返回调用方分配且由全部 attempt 共享的 session generation。
func (plan RouteSelectionPlan) Generation() SessionGeneration { return plan.generation }

// Groups 返回按启动时间、priority、RouteID 稳定排序的 attempt group 深拷贝。
func (plan RouteSelectionPlan) Groups() []RouteAttemptGroup {
	groups := make([]RouteAttemptGroup, 0, len(plan.groups))
	for _, group := range plan.groups {
		groups = append(groups, cloneRouteAttemptGroup(group))
	}
	return groups
}

// RouteAttemptGroup 表示同一时刻启动的一组 route attempt。
// priority 只决定分组和启动计划，不能覆盖后续第一个完成完整 ReadySession 校验的 winner。
type RouteAttemptGroup struct {
	priority   *int
	startDelay time.Duration
	attempts   []RouteAttempt
}

// Priority 返回该组显式 priority；无 priority 的 full-race group 返回 nil。
func (group RouteAttemptGroup) Priority() *int { return clonePriority(group.priority) }

// StartDelay 返回相对本次 race 起点的启动延迟。
func (group RouteAttemptGroup) StartDelay() time.Duration { return group.startDelay }

// Attempts 返回按 RouteID 稳定排序的 attempt 深拷贝。
func (group RouteAttemptGroup) Attempts() []RouteAttempt {
	attempts := make([]RouteAttempt, 0, len(group.attempts))
	for _, attempt := range group.attempts {
		attempts = append(attempts, cloneRouteAttempt(attempt))
	}
	return attempts
}

// RouteSelectionPlanner 把持久 Endpoint route 配置转换为确定性 attempt groups。
// planner 不 dial、不解析 credential、不选择 winner、不修改输入，也不为 unsupported route 建立 fallback。
type RouteSelectionPlanner struct{}

// Plan 生成当前 C3B 支持的 local/SSH 自动竞速计划。
// managed WebRTC 只在它是唯一 eligible 自动 route 或被显式 override 时保留单路能力；direct TLS/LAN 与 managed 共同竞速留给后续切片。
func (RouteSelectionPlanner) Plan(request RouteSelectionRequest) (RouteSelectionPlan, error) {
	target := cloneEndpoint(request.Endpoint)
	if err := target.Validate(); err != nil {
		return RouteSelectionPlan{}, err
	}
	if !target.Enabled {
		return RouteSelectionPlan{}, connectionError(ErrorRouteUnavailable, "endpoint %q is disabled", target.ID)
	}
	if request.Generation == 0 {
		return RouteSelectionPlan{}, connectionError(ErrorConfig, "endpoint %q route plan requires a generation", target.ID)
	}
	if strings.TrimSpace(request.Intent.Kind) == "" {
		return RouteSelectionPlan{}, connectionError(ErrorConfig, "endpoint %q route plan requires a connect intent", target.ID)
	}

	supported, err := routeKindSet(request.SupportedRouteKinds)
	if err != nil {
		return RouteSelectionPlan{}, err
	}
	credentials, err := credentialRefSet(request.AvailableCredentialRefs)
	if err != nil {
		return RouteSelectionPlan{}, err
	}

	if request.RouteOverride != "" {
		route, ok := target.Route(request.RouteOverride)
		if !ok || !route.Enabled {
			return RouteSelectionPlan{}, connectionError(ErrorRouteUnavailable, "endpoint %q route %q is unavailable", target.ID, request.RouteOverride)
		}
		if err := validatePlannedRoute(route, supported, credentials); err != nil {
			return RouteSelectionPlan{}, err
		}
		return newRouteSelectionPlan(target, request.Intent, request.Generation, []routeGroup{{priority: clonePriority(route.Priority), routes: []AccessRoute{route}}}, 0, nil), nil
	}

	automatic := make([]AccessRoute, 0, len(target.Routes))
	managed := make([]AccessRoute, 0, 1)
	diagnostics := make([]RoutePlanDiagnostic, 0, len(target.Routes))
	credentialBlocked := false
	for _, route := range target.RouteList() {
		if !route.Enabled {
			diagnostics = append(diagnostics, routePlanDiagnostic(route, ErrorRouteUnavailable, RoutePlanRouteDisabled))
			continue
		}
		if route.ManualOnly {
			diagnostics = append(diagnostics, routePlanDiagnostic(route, ErrorRouteUnavailable, RoutePlanManualOnly))
			continue
		}
		if _, ok := supported[route.Kind]; !ok {
			diagnostics = append(diagnostics, routePlanDiagnostic(route, ErrorRouteUnavailable, RoutePlanPlatformUnsupported))
			continue
		}
		if route.CredentialRef != "" {
			if _, ok := credentials[route.CredentialRef]; !ok {
				credentialBlocked = true
				diagnostics = append(diagnostics, routePlanDiagnostic(route, ErrorCredentialRequired, RoutePlanCredentialUnavailable))
				continue
			}
		}
		switch route.Kind {
		case RouteLocalUnix, RouteSSHStdio:
			automatic = append(automatic, route)
		case RouteManagedWebRTC:
			managed = append(managed, route)
		default:
			diagnostics = append(diagnostics, routePlanDiagnostic(route, ErrorRouteUnavailable, RoutePlanAutomaticRaceUnsupported))
		}
	}
	if len(automatic) == 0 {
		if len(managed) == 1 {
			automatic = managed
		} else if credentialBlocked {
			return RouteSelectionPlan{}, connectionError(ErrorCredentialRequired, "endpoint %q has no eligible route with an available credential", target.ID)
		} else {
			return RouteSelectionPlan{}, connectionError(ErrorRouteUnavailable, "endpoint %q has no eligible route for the current platform", target.ID)
		}
	} else {
		for _, route := range managed {
			diagnostics = append(diagnostics, routePlanDiagnostic(route, ErrorRouteUnavailable, RoutePlanAutomaticRaceUnsupported))
		}
	}

	groups := groupAutomaticRoutes(automatic)
	sort.Slice(diagnostics, func(i, j int) bool { return diagnostics[i].RouteID < diagnostics[j].RouteID })
	return newRouteSelectionPlan(target, request.Intent, request.Generation, groups, target.SelectionPolicy.HedgeDelay, diagnostics), nil
}

type routeGroup struct {
	priority *int
	routes   []AccessRoute
}

func groupAutomaticRoutes(routes []AccessRoute) []routeGroup {
	if len(routes) == 0 {
		return nil
	}
	if routes[0].Priority == nil {
		return []routeGroup{{routes: routes}}
	}
	byPriority := make(map[int][]AccessRoute)
	priorities := make([]int, 0)
	for _, route := range routes {
		priority := *route.Priority
		if _, ok := byPriority[priority]; !ok {
			priorities = append(priorities, priority)
		}
		byPriority[priority] = append(byPriority[priority], route)
	}
	sort.Ints(priorities)
	groups := make([]routeGroup, 0, len(priorities))
	for _, priority := range priorities {
		value := priority
		groups = append(groups, routeGroup{priority: &value, routes: byPriority[priority]})
	}
	return groups
}

func newRouteSelectionPlan(target Endpoint, intent ConnectIntent, generation SessionGeneration, groups []routeGroup, hedgeDelay time.Duration, diagnostics []RoutePlanDiagnostic) RouteSelectionPlan {
	plan := RouteSelectionPlan{endpointID: target.ID, generation: generation, groups: make([]RouteAttemptGroup, 0, len(groups))}
	plan.diagnostics = append([]RoutePlanDiagnostic(nil), diagnostics...)
	for index, group := range groups {
		attempts := make([]RouteAttempt, 0, len(group.routes))
		for _, route := range group.routes {
			attempts = append(attempts, RouteAttempt{
				AttemptID:        AttemptID(fmt.Sprintf("%s:%d:%s", target.ID, generation, route.ID)),
				EndpointID:       target.ID,
				ExpectedIdentity: target.DaemonIdentity,
				Route:            cloneRoute(route),
				Intent:           cloneConnectIntent(intent),
				Generation:       generation,
			})
		}
		plan.groups = append(plan.groups, RouteAttemptGroup{
			priority: clonePriority(group.priority), startDelay: time.Duration(index) * hedgeDelay, attempts: attempts,
		})
	}
	return plan
}

func routePlanDiagnostic(route AccessRoute, code ErrorCode, reason RoutePlanDiagnosticReason) RoutePlanDiagnostic {
	return RoutePlanDiagnostic{RouteID: route.ID, Code: code, Reason: reason}
}

func validatePlannedRoute(route AccessRoute, supported map[RouteKind]struct{}, credentials map[string]struct{}) error {
	if _, ok := supported[route.Kind]; !ok {
		return connectionError(ErrorRouteUnavailable, "route %q kind %q is not supported by the current platform", route.ID, route.Kind)
	}
	switch route.Kind {
	case RouteLocalUnix, RouteSSHStdio, RouteManagedWebRTC:
	default:
		return connectionError(ErrorRouteUnavailable, "route %q kind %q is not available in C3B", route.ID, route.Kind)
	}
	if route.CredentialRef != "" {
		if _, ok := credentials[route.CredentialRef]; !ok {
			return connectionError(ErrorCredentialRequired, "route %q credential %q is unavailable", route.ID, route.CredentialRef)
		}
	}
	return nil
}

func routeKindSet(values []RouteKind) (map[RouteKind]struct{}, error) {
	result := make(map[RouteKind]struct{}, len(values))
	for _, value := range values {
		switch value {
		case RouteLocalUnix, RouteSSHStdio, RouteManagedWebRTC, RouteDirectTLS:
		default:
			return nil, connectionError(ErrorConfig, "route planner received unknown platform route kind %q", value)
		}
		result[value] = struct{}{}
	}
	return result, nil
}

func credentialRefSet(values []string) (map[string]struct{}, error) {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || trimmed != value || strings.ContainsAny(value, "\r\n") {
			return nil, connectionError(ErrorConfig, "route planner received an invalid credential reference")
		}
		result[value] = struct{}{}
	}
	return result, nil
}

func cloneConnectIntent(intent ConnectIntent) ConnectIntent {
	return ConnectIntent{Kind: intent.Kind, TerminalID: intent.TerminalID, RequiredScopes: append([]string(nil), intent.RequiredScopes...)}
}

func cloneRouteAttempt(attempt RouteAttempt) RouteAttempt {
	attempt.Route = cloneRoute(attempt.Route)
	attempt.Intent = cloneConnectIntent(attempt.Intent)
	return attempt
}

func cloneRouteAttemptGroup(group RouteAttemptGroup) RouteAttemptGroup {
	return RouteAttemptGroup{priority: clonePriority(group.priority), startDelay: group.startDelay, attempts: group.Attempts()}
}
