// Package routeplanner 把私有 SmartRoute 决策与公开 ManagedRoutePlan contract 隔离。
package routeplanner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/lozzow/termx/private/termx-cloud/route-planner/smartroute"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

const maxPlanTTL = 10 * time.Minute

// DecisionEngine 是 Service 使用的最小 SmartRoute 状态 owner。
// 生产实现是有界 smartroute.Planner；fake 只能用于确定性 contract harness。
type DecisionEngine interface {
	Select(smartroute.Request, time.Time) (smartroute.Decision, error)
}

// RequestSource 从私有 topology、Probe Aggregator、entitlement 和 cost policy 构造决策输入。
// 公开 PlanManagedRouteRequest 不携带候选质量、成本预算或内部权重。
type RequestSource interface {
	SmartRouteRequest(context.Context, *cloudpb.PlanManagedRouteRequest, time.Time) (smartroute.Request, error)
}

// RouteMaterial 是选中候选可执行的短期 ICE 配置。
// Relay credential 可以出现于 IceServers，但 signed lease、账号 token 和成本不得进入该对象。
type RouteMaterial struct {
	IceServers []*cloudpb.IceServer
	ValidUntil time.Time
}

// MaterialSource 只为最终选中候选生成 direct STUN 或 lease-bound TURN material。
// 它不能为被 cost guard、capacity 或 policy 拒绝的候选提前申请 RelayLease。
type MaterialSource interface {
	RouteMaterial(context.Context, *cloudpb.PlanManagedRouteRequest, smartroute.Candidate, time.Time) (RouteMaterial, error)
}

// Config 固定 Route Planner service 的依赖、时钟和 plan ID 生成器。
// 所有依赖都必须显式注入，避免测试 fake 或无 lease material 被生产路径静默采用。
type Config struct {
	Engine    DecisionEngine
	Requests  RequestSource
	Materials MaterialSource
	Now       func() time.Time
	PlanID    func() (string, error)
}

// Service 是 private candidate/cost truth 到 public route plan 的事务边界。
// 它不建立 PeerConnection、不执行 fallback，也不接触 terminal capability。
type Service struct {
	engine    DecisionEngine
	requests  RequestSource
	materials MaterialSource
	now       func() time.Time
	planID    func() (string, error)
}

// NewService 创建严格依赖注入的 Route Planner service。
// 缺少候选源、决策 owner 或 material issuer 时直接失败，不能返回猜测的 direct/Relay 计划。
func NewService(config Config) (*Service, error) {
	if config.Engine == nil || config.Requests == nil || config.Materials == nil {
		return nil, fmt.Errorf("route planner service dependencies are required")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.PlanID == nil {
		config.PlanID = randomPlanID
	}
	return &Service{
		engine: config.Engine, requests: config.Requests, materials: config.Materials,
		now: config.Now, planID: config.PlanID,
	}, nil
}

// PlanManagedRoute 选择一个 direct/single-relay 候选并返回短期可执行 ICE 计划。
// 任一失败只结束本次 managed route 请求；调用方不得隐式使用 resolution 中未被选择的 TURN server。
func (service *Service) PlanManagedRoute(ctx context.Context, request *cloudpb.PlanManagedRouteRequest) (*cloudpb.ManagedRoutePlan, error) {
	if service == nil || ctx == nil || request == nil {
		return nil, fmt.Errorf("invalid managed route plan request")
	}
	if !canonicalIdentifier(request.GetEndpointId()) || !canonicalIdentifier(request.GetManagedSessionId()) ||
		!canonicalIdentifier(request.GetTargetDeviceId()) || request.GetRoutePreference() != cloudpb.RoutePreference_ROUTE_PREFERENCE_SMART_ROUTE {
		return nil, fmt.Errorf("invalid managed route plan binding")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := service.now().UTC()
	input, err := service.requests.SmartRouteRequest(ctx, proto.Clone(request).(*cloudpb.PlanManagedRouteRequest), now)
	if err != nil {
		return nil, fmt.Errorf("load SmartRoute candidates: %w", err)
	}
	if input.ManagedSessionID != request.GetManagedSessionId() {
		return nil, fmt.Errorf("SmartRoute candidate source returned a different managed session")
	}
	decision, err := service.engine.Select(input, now)
	if err != nil {
		return nil, fmt.Errorf("select SmartRoute candidate: %w", err)
	}
	selectionReason := reasonToWire(decision.Reason)
	if selectionReason == cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_UNSPECIFIED {
		return nil, fmt.Errorf("select SmartRoute candidate: unknown public selection reason")
	}
	material, err := service.materials.RouteMaterial(ctx, proto.Clone(request).(*cloudpb.PlanManagedRouteRequest), decision.Selected, now)
	if err != nil {
		return nil, fmt.Errorf("issue selected route material: %w", err)
	}
	if err := validateMaterial(decision.Selected, material, now); err != nil {
		return nil, err
	}
	planID, err := service.planID()
	if err != nil || !canonicalIdentifier(planID) {
		return nil, fmt.Errorf("generate managed route plan id")
	}
	return &cloudpb.ManagedRoutePlan{
		PlanId: planID, ManagedSessionId: request.GetManagedSessionId(), TargetDeviceId: request.GetTargetDeviceId(),
		SelectedPath: decision.Selected.Path, SelectionReason: selectionReason,
		ValidUntilUnix: uint64(material.ValidUntil.Unix()), IceServers: cloneIceServers(material.IceServers),
		RelayOnly: decision.Selected.Path == cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY,
		RelayRegion: func() string {
			if decision.Selected.Path == cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY {
				return strings.ToLower(strings.TrimSpace(decision.Selected.Region))
			}
			return ""
		}(),
	}, nil
}

func validateMaterial(candidate smartroute.Candidate, material RouteMaterial, now time.Time) error {
	if !now.Before(material.ValidUntil) || material.ValidUntil.After(now.Add(maxPlanTTL)) {
		return fmt.Errorf("selected route material lifetime is invalid")
	}
	hasTURN := false
	for _, server := range material.IceServers {
		if server == nil || len(server.GetUrls()) == 0 {
			return fmt.Errorf("selected route material contains an invalid ICE server")
		}
		for _, rawURL := range server.GetUrls() {
			if rawURL == "" || rawURL != strings.TrimSpace(rawURL) {
				return fmt.Errorf("selected route material contains a non-canonical ICE URL")
			}
			url := strings.ToLower(rawURL)
			isTURN := strings.HasPrefix(url, "turn:") || strings.HasPrefix(url, "turns:")
			if !isTURN && !strings.HasPrefix(url, "stun:") && !strings.HasPrefix(url, "stuns:") {
				return fmt.Errorf("selected route material contains an unsupported ICE URL")
			}
			if isTURN {
				hasTURN = true
				if strings.TrimSpace(server.GetUsername()) == "" || strings.TrimSpace(server.GetCredential()) == "" {
					return fmt.Errorf("selected route material TURN server has no short credential")
				}
			}
		}
	}
	switch candidate.Path {
	case cloudpb.ObservedPath_OBSERVED_PATH_DIRECT:
		if hasTURN {
			return fmt.Errorf("direct route material contains TURN")
		}
	case cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY:
		if !hasTURN {
			return fmt.Errorf("single-relay route material has no TURN server")
		}
	default:
		return fmt.Errorf("unsupported selected route path %s", candidate.Path)
	}
	return nil
}

func reasonToWire(reason smartroute.Reason) cloudpb.RouteSelectionReason {
	switch reason {
	case smartroute.ReasonInitialBest:
		return cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_INITIAL_BEST
	case smartroute.ReasonOnlyViable:
		return cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_ONLY_VIABLE
	case smartroute.ReasonLowerLoss:
		return cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_LOWER_LOSS
	case smartroute.ReasonDirectUnstable:
		return cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_DIRECT_UNSTABLE
	case smartroute.ReasonLowerLatency:
		return cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_LOWER_LATENCY
	case smartroute.ReasonLowerScore:
		return cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_LOWER_SCORE
	case smartroute.ReasonCostGuard:
		return cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_COST_GUARD
	case smartroute.ReasonMinimumHold:
		return cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_MINIMUM_HOLD
	case smartroute.ReasonCooldown:
		return cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_COOLDOWN
	case smartroute.ReasonHysteresisHold:
		return cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_HYSTERESIS_HOLD
	case smartroute.ReasonInsufficientImprovement:
		return cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_INSUFFICIENT_IMPROVEMENT
	case smartroute.ReasonCurrentUnavailable:
		return cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_CURRENT_UNAVAILABLE
	case smartroute.ReasonCurrentBest:
		return cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_CURRENT_BEST
	default:
		return cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_UNSPECIFIED
	}
}

func cloneIceServers(servers []*cloudpb.IceServer) []*cloudpb.IceServer {
	cloned := make([]*cloudpb.IceServer, 0, len(servers))
	for _, server := range servers {
		if server != nil {
			cloned = append(cloned, proto.Clone(server).(*cloudpb.IceServer))
		}
	}
	return cloned
}

func canonicalIdentifier(value string) bool {
	return value != "" && len(value) <= 128 && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\r\n\t ")
}

func randomPlanID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return "plan-" + hex.EncodeToString(buffer), nil
}
