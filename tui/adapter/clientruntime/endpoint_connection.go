package clientruntimeadapter

import (
	"context"
	"fmt"

	"github.com/anytty/anytty/client/endpoint"
	clientruntime "github.com/anytty/anytty/client/runtime"
	"github.com/anytty/anytty/tui/state"
)

// EndpointConnectionControl 把共享 Endpoint registry 与 runtime planner 环境投影为 TUI EndpointStore。
// RegistryPath 为空时使用 Endpoint 默认路径；Runtime 只提供只读 plan snapshot，不把 session owner 下放给 UI。
type EndpointConnectionControl struct {
	RegistryPath string
	Runtime      EndpointPlanSnapshotSource
}

// EndpointPlanSnapshotSource 是 adapter 读取 Go runtime planner policy 的窄只读能力。
// 它不包含 EnsureSession/Disconnect，因此 Connections 页面不能借此拥有连接 lifecycle。
type EndpointPlanSnapshotSource interface {
	PlanSnapshot(context.Context, endpoint.EndpointID) (clientruntime.EndpointPlanSnapshot, error)
}

// LoadConnections 读取最新 registry，并用同一个 Go planner 环境标记每条 Route 的当前可用性。
func (control EndpointConnectionControl) LoadConnections(ctx context.Context) (state.EndpointStore, error) {
	registry, err := endpoint.Load(control.RegistryPath)
	if err != nil {
		return state.EndpointStore{}, err
	}
	return control.project(ctx, registry)
}

// ApplyRoutePriorities 在跨进程 registry 锁内提交完整 priority 集合，并返回提交后的 Go-owned 投影。
// mutation 失败时文件与当前 runtime session 均保持不变；成功也不会隐式触发重连。
func (control EndpointConnectionControl) ApplyRoutePriorities(ctx context.Context, endpointID state.EndpointID, priorities map[string]*int) (state.EndpointStore, error) {
	domainPriorities := make(map[endpoint.RouteID]*int, len(priorities))
	for routeID, priority := range priorities {
		domainPriorities[endpoint.RouteID(routeID)] = priority
	}
	registry, err := endpoint.UpdateContext(ctx, control.RegistryPath, false, func(current endpoint.Registry) (endpoint.Registry, error) {
		return endpoint.SetAutomaticRoutePriorities(current, endpoint.EndpointID(endpointID), domainPriorities)
	})
	if err != nil {
		return state.EndpointStore{}, err
	}
	return control.project(ctx, registry)
}

func (control EndpointConnectionControl) project(ctx context.Context, registry endpoint.Registry) (state.EndpointStore, error) {
	store := (state.EndpointStore{}).ApplyConnectionRegistry(registry)
	if control.Runtime == nil {
		return store, nil
	}
	for _, target := range registry.List() {
		snapshot, err := control.Runtime.PlanSnapshot(ctx, target.ID)
		if err != nil {
			return state.EndpointStore{}, fmt.Errorf("load endpoint %q planner policy: %w", target.ID, err)
		}
		availability, err := endpoint.EvaluateRouteAvailability(endpoint.RouteAvailabilityRequest{
			Endpoint: target, PlanningEndpoint: snapshot.Endpoint,
			SupportedRouteKinds: snapshot.Environment.SupportedRouteKinds, AvailableCredentialRefs: snapshot.Environment.AvailableCredentialRefs,
		})
		if err != nil {
			return state.EndpointStore{}, fmt.Errorf("evaluate endpoint %q route availability: %w", target.ID, err)
		}
		store = store.ApplyRouteAvailability(state.EndpointID(target.ID), availability)
	}
	return store, nil
}
