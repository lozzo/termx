package managed

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/muxvia/muxvia/client/endpoint"
	clientruntime "github.com/muxvia/muxvia/client/runtime"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
)

const managedRoutePlanMaxTTL = 10 * time.Minute

type dialRoute struct {
	iceServers      []*cloudpb.IceServer
	preference      cloudpb.RoutePreference
	relayOnly       bool
	expectedPath    endpoint.Path
	selectionReason cloudcompanion.RouteSelectionReason
	// relayEntitlementDenied 只记录 AUTO 在 Relay 准入被套餐拒绝后降级到 P2P 的事实。
	// 只有后续 ICE 也失败时才能据此提示升级套餐；P2P 成功时不得展示套餐错误。
	relayEntitlementDenied bool
}

// resolveDialRoute 在 STANDARD_RELAY 下通过 Companion 获取 principal-specific RelayLease，在 smart_route 下请求私有 Planner 的短期计划。
// AUTO 同时保留 P2P 与 TURN 候选，仅在产品契约明确拒绝 Relay 能力时继续 P2P；公开进程不自行签发或猜测 material。
func resolveDialRoute(
	ctx context.Context,
	cloud CloudClient,
	attempt clientruntime.AttemptRequest,
	resolved *cloudpb.ResolvedEndpoint,
	policy cloudcompanion.DialPolicy,
	transport cloudpb.RelayTransport,
	now time.Time,
) (dialRoute, error) {
	targetDeviceID := attempt.DaemonIdentity().DeviceID
	if policy.RelayOnly && policy.RoutePreference != cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY {
		return dialRoute{}, routePlanProtocolError("relay-only policy requires standard Relay service intent")
	}
	if policy.RoutePreference == cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY {
		request := &cloudpb.AcquireRelayLeaseRequest{
			ManagedSessionId: resolved.GetManagedSessionId(), TargetDeviceId: targetDeviceID,
			RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY,
		}
		lease, err := cloud.AcquireRelayLease(ctx, request)
		if err != nil {
			if !policy.RelayOnly && cloudcompanion.RelayLeaseUnavailableForAuto(err) {
				return constrainDialRouteTransport(dialRoute{
					iceServers: cloneRouteIceServers(resolved.GetIceServers()), preference: policy.RoutePreference,
					relayEntitlementDenied: cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ENTITLEMENT_DENIED),
				}, transport)
			}
			return dialRoute{}, err
		}
		if err := cloudcompanion.ValidateSingleRelayLease(request, lease, now); err != nil {
			return dialRoute{}, err
		}
		iceServers := cloneRouteIceServers(lease.GetIceServers())
		if !policy.RelayOnly {
			iceServers = append(cloneRouteIceServers(resolved.GetIceServers()), iceServers...)
		}
		expectedPath := endpoint.Path("")
		if policy.RelayOnly {
			expectedPath = endpoint.PathSingleRelay
		}
		return constrainDialRouteTransport(dialRoute{
			iceServers: iceServers, preference: cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY,
			relayOnly: policy.RelayOnly, expectedPath: expectedPath,
		}, transport)
	}
	if policy.RoutePreference != cloudpb.RoutePreference_ROUTE_PREFERENCE_SMART_ROUTE {
		return constrainDialRouteTransport(dialRoute{
			iceServers: resolved.GetIceServers(), preference: policy.RoutePreference, relayOnly: policy.RelayOnly,
		}, transport)
	}
	request := &cloudpb.PlanManagedRouteRequest{
		EndpointId: string(attempt.EndpointID()), ManagedSessionId: resolved.GetManagedSessionId(), TargetDeviceId: targetDeviceID,
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_SMART_ROUTE,
	}
	plan, err := cloud.PlanManagedRoute(ctx, request)
	if err != nil {
		return dialRoute{}, err
	}
	validated, err := validateManagedRoutePlan(request, plan, now)
	if err != nil {
		return dialRoute{}, err
	}
	return constrainDialRouteTransport(validated, transport)
}

func constrainDialRouteTransport(route dialRoute, transport cloudpb.RelayTransport) (dialRoute, error) {
	filtered, hasTURN, err := cloudcompanion.FilterRelayTransport(route.iceServers, transport)
	if err != nil {
		return dialRoute{}, err
	}
	if route.relayOnly && !hasTURN {
		return dialRoute{}, routePlanProtocolError("forced Relay transport is unavailable in the current lease")
	}
	route.iceServers = filtered
	return route, nil
}

func wireRelayTransport(value endpoint.RelayTransport) (cloudpb.RelayTransport, error) {
	switch value {
	case "", endpoint.RelayTransportAuto:
		return cloudpb.RelayTransport_RELAY_TRANSPORT_AUTO, nil
	case endpoint.RelayTransportUDP:
		return cloudpb.RelayTransport_RELAY_TRANSPORT_UDP, nil
	case endpoint.RelayTransportTCP:
		return cloudpb.RelayTransport_RELAY_TRANSPORT_TCP, nil
	default:
		return 0, routePlanProtocolError(fmt.Sprintf("unknown managed Relay transport %q", value))
	}
}

func cloneRouteIceServers(servers []*cloudpb.IceServer) []*cloudpb.IceServer {
	cloned := make([]*cloudpb.IceServer, 0, len(servers))
	for _, server := range servers {
		if server == nil {
			continue
		}
		cloned = append(cloned, &cloudpb.IceServer{
			Urls: append([]string(nil), server.GetUrls()...), Username: server.GetUsername(), Credential: server.GetCredential(),
		})
	}
	return cloned
}

// validateManagedRoutePlan 在公开进程再次校验 Companion 输出。
// Companion 即使被替换或返回损坏数据，也不能把未选中的 TURN、mesh 或跨 session material 注入 Pion。
func validateManagedRoutePlan(request *cloudpb.PlanManagedRouteRequest, plan *cloudpb.ManagedRoutePlan, now time.Time) (dialRoute, error) {
	if request == nil || plan == nil || !canonicalRouteValue(plan.GetPlanId()) ||
		plan.GetManagedSessionId() != request.GetManagedSessionId() || plan.GetTargetDeviceId() != request.GetTargetDeviceId() {
		return dialRoute{}, routePlanProtocolError("Cloud Companion returned an invalid managed route plan identity")
	}
	reason := cloudcompanion.RouteSelectionReasonFromWire(plan.GetSelectionReason())
	if reason == "" {
		return dialRoute{}, routePlanProtocolError("Cloud Companion returned an unknown managed route selection reason")
	}
	nowUnix := now.Unix()
	if nowUnix < 0 || plan.GetValidUntilUnix() <= uint64(nowUnix) || plan.GetValidUntilUnix() > uint64(now.Add(managedRoutePlanMaxTTL).Unix()) {
		return dialRoute{}, routePlanProtocolError("Cloud Companion returned an expired or overlong managed route plan")
	}
	hasTURN, err := validateRouteIceServers(plan.GetIceServers())
	if err != nil {
		return dialRoute{}, err
	}

	route := dialRoute{iceServers: plan.GetIceServers(), expectedPath: endpoint.Path(cloudcompanion.PathFromWire(plan.GetSelectedPath())), selectionReason: reason}
	switch plan.GetSelectedPath() {
	case cloudpb.ObservedPath_OBSERVED_PATH_DIRECT:
		if plan.GetRelayOnly() || plan.GetRelayRegion() != "" || hasTURN {
			return dialRoute{}, routePlanProtocolError("direct managed route plan contains Relay material")
		}
		route.preference = cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY
	case cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY:
		if !plan.GetRelayOnly() || !hasTURN || !canonicalRouteTag(plan.GetRelayRegion()) {
			return dialRoute{}, routePlanProtocolError("single-relay managed route plan is incomplete")
		}
		route.preference = cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY
		route.relayOnly = true
	default:
		return dialRoute{}, routePlanProtocolError("managed route plan path is unavailable in GA002")
	}
	return route, nil
}

func validateRouteIceServers(servers []*cloudpb.IceServer) (bool, error) {
	hasTURN := false
	for _, server := range servers {
		if server == nil || len(server.GetUrls()) == 0 {
			return false, routePlanProtocolError("managed route plan contains an invalid ICE server")
		}
		for _, rawURL := range server.GetUrls() {
			if rawURL == "" || rawURL != strings.TrimSpace(rawURL) {
				return false, routePlanProtocolError("managed route plan contains a non-canonical ICE URL")
			}
			lowerURL := strings.ToLower(rawURL)
			isTURN := strings.HasPrefix(lowerURL, "turn:") || strings.HasPrefix(lowerURL, "turns:")
			if !isTURN && !strings.HasPrefix(lowerURL, "stun:") && !strings.HasPrefix(lowerURL, "stuns:") {
				return false, routePlanProtocolError("managed route plan contains an unsupported ICE URL")
			}
			if isTURN {
				hasTURN = true
				if strings.TrimSpace(server.GetUsername()) == "" || strings.TrimSpace(server.GetCredential()) == "" {
					return false, routePlanProtocolError("managed route plan TURN server has no short credential")
				}
			}
		}
	}
	return hasTURN, nil
}

func canonicalRouteValue(value string) bool {
	return value != "" && len(value) <= 128 && value == strings.TrimSpace(value) && !strings.ContainsFunc(value, unicode.IsSpace)
}

func canonicalRouteTag(value string) bool {
	if value == "" || len(value) > 64 || value != strings.TrimSpace(value) || value != strings.ToLower(value) {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}

func routePlanProtocolError(message string) error {
	return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, message)
}
