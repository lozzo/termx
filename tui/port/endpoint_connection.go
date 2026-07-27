package port

import (
	"context"

	"github.com/anytty/anytty/tui/state"
)

// EndpointConnectionService 是 TUI effect 读取共享连接策略和原子修改 Route priority 的 application port。
// 实现必须委托 Go Endpoint registry/planner；不得在 reducer 中复制 priority 校验、credential 或 route 可用性规则。
type EndpointConnectionService interface {
	LoadConnections(context.Context) (state.EndpointStore, error)
	ApplyRoutePriorities(context.Context, state.EndpointID, map[string]*int) (state.EndpointStore, error)
}
