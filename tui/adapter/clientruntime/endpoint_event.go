package clientruntimeadapter

import (
	clientruntime "github.com/lozzow/termx/client/runtime"
	"github.com/lozzow/termx/tui/port"
	"github.com/lozzow/termx/tui/state"
)

// ProjectEndpointEvent 把共享 runtime lifecycle event 映射为 TUI-owned endpoint 投影。
// 映射只转换稳定枚举和展示字段，不读取 registry，不创建连接，也不把 runtime stamp 写入 reducer state。
func ProjectEndpointEvent(event clientruntime.EndpointEvent) port.EndpointRuntimeEvent {
	phase := projectConnectionPhase(event.Phase)
	status := state.EndpointStatusConnecting
	switch event.Phase {
	case clientruntime.EndpointPhaseReady:
		status = state.EndpointStatusConnected
	case clientruntime.EndpointPhaseOffline:
		status = state.EndpointStatusOffline
	}
	return port.EndpointRuntimeEvent{
		EndpointID:           state.EndpointID(event.EndpointID),
		Status:               status,
		ErrorKind:            projectErrorKind(event.ErrorCode),
		Phase:                phase,
		ObservedPath:         event.ObservedPath,
		RouteSelectionReason: event.RouteSelectionReason,
		Message:              event.Message,
	}
}

func projectConnectionPhase(phase clientruntime.EndpointPhase) state.EndpointConnectionPhase {
	switch phase {
	case clientruntime.EndpointPhaseIdle:
		return state.EndpointConnectionIdle
	case clientruntime.EndpointPhasePlanning, clientruntime.EndpointPhaseResolving:
		return state.EndpointConnectionResolving
	case clientruntime.EndpointPhaseSignaling:
		return state.EndpointConnectionSignaling
	case clientruntime.EndpointPhaseConnecting:
		return state.EndpointConnectionConnecting
	case clientruntime.EndpointPhaseAuthorizing:
		return state.EndpointConnectionAuthorizing
	case clientruntime.EndpointPhaseReady:
		return state.EndpointConnectionConnected
	case clientruntime.EndpointPhaseOffline:
		return state.EndpointConnectionFailed
	default:
		return ""
	}
}

func projectErrorKind(code clientruntime.ErrorCode) state.EndpointErrorKind {
	switch code {
	case "":
		return state.EndpointErrorUnknown
	case clientruntime.ErrorInvalidRequest, clientruntime.ErrorUnsupportedRoute:
		return state.EndpointErrorConfig
	case clientruntime.ErrorIdentity, clientruntime.ErrorAuthorization:
		return state.EndpointErrorAuth
	case clientruntime.ErrorStaleSession:
		return state.EndpointErrorProtocol
	case clientruntime.ErrorCanceled, clientruntime.ErrorUnavailable:
		return state.EndpointErrorUnavailable
	default:
		return state.EndpointErrorUnknown
	}
}
