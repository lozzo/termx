package services

import (
	"context"

	"github.com/lozzow/termx/shared/connection"
	"github.com/lozzow/termx/tui/state"
)

// EndpointServiceBundle 是一个已经完成连接与协议握手的单 daemon service adapter 集合。
// C3X 只保留这个 leaf transport contract；route race、winner、generation 和 lifecycle owner
// 必须由后续 shared/clientruntime 提供，TUI 不得重新持有这些真值。
type EndpointServiceBundle struct {
	EndpointID           state.EndpointID
	RouteID              connection.RouteID
	Terminal             TerminalService
	Core                 CoreClient
	Surface              TerminalSurfaceService
	LiveEvents           TerminalLiveEventService
	Path                 PathService
	ObservedPath         string
	RouteSelectionReason string
	Lifecycle            EndpointLifecycle
}

// EndpointDialer 把共享 runtime 已选定的单次 route attempt 转成 ready service bundle。
// dialer 不选择其它 route、不缓存 session、不发布 reducer event，也不得 fallback 到其它 endpoint。
type EndpointDialer func(context.Context, connection.Endpoint, connection.AccessRoute) (EndpointServiceBundle, error)
