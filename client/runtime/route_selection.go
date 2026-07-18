package runtime

import (
	"fmt"

	"github.com/lozzow/termx/client/endpoint"
)

// SelectRoute 按当前最小策略选择 endpoint route。
// 显式 requested route 必须 enabled；未显式指定时只接受唯一 enabled 且非 manual-only route，避免 CLI/UI 各自复制选择规则。
func SelectRoute(target endpoint.Endpoint, requested endpoint.RouteID) (endpoint.AccessRoute, error) {
	if requested != "" {
		route, ok := target.Route(requested)
		if !ok || !route.Enabled {
			return endpoint.AccessRoute{}, runtimeError(ErrorUnavailable, fmt.Sprintf("endpoint %s route %s is unavailable", target.ID, requested), nil)
		}
		return route, nil
	}
	eligible := make([]endpoint.AccessRoute, 0, len(target.Routes))
	for _, route := range target.RouteList() {
		if route.Enabled && !route.ManualOnly {
			eligible = append(eligible, route)
		}
	}
	if len(eligible) == 0 {
		return endpoint.AccessRoute{}, runtimeError(ErrorUnavailable, fmt.Sprintf("endpoint %s has no eligible route", target.ID), nil)
	}
	if len(eligible) != 1 {
		return endpoint.AccessRoute{}, runtimeError(ErrorInvalidRequest, fmt.Sprintf("endpoint %s requires explicit route selection", target.ID), nil)
	}
	return eligible[0], nil
}
