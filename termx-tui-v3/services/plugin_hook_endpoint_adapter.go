package services

import (
	"github.com/lozzow/termx/termx-shared/plugin"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

// EnrichDaemonHookForEndpoint 把 daemon-local terminal hook 补成当前 client 的 endpoint 视角。
// 它只填写 EndpointID/TerminalRef；SourceHost 必须保持 HostDaemon，避免 client 伪造 daemon 系统事件 owner。
func EnrichDaemonHookForEndpoint(event plugin.HookEvent, endpointID state.EndpointID) plugin.HookEvent {
	out := event.Clone()
	endpointID = state.NormalizeEndpointID(endpointID)
	if out.SourceHost != plugin.HostDaemon || out.DaemonTerminalID == "" {
		return out
	}
	ref := plugin.TerminalRef{
		EndpointID: plugin.EndpointID(endpointID),
		TerminalID: out.DaemonTerminalID,
	}
	out.EndpointID = ref.EndpointID
	out.TerminalRef = &ref
	return out
}
