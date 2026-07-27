package clientruntimeadapter

import (
	"context"
	"errors"
	"fmt"
	"sync"

	clientprotocol "github.com/muxvia/muxvia/client/adapter/protocol"
	clientendpoint "github.com/muxvia/muxvia/client/endpoint"
	clientruntime "github.com/muxvia/muxvia/client/runtime"
	protocoladapter "github.com/muxvia/muxvia/tui/adapter/protocol"
	"github.com/muxvia/muxvia/tui/port"
	"github.com/muxvia/muxvia/tui/state"
)

// EndpointApplicationRouter 把 TUI 的 endpoint-scoped services 路由到共享 ClientRuntime。
// protocol adapter 仍只绑定一条 ready session；本层负责按 EndpointID 获取和缓存 consumer lease。
type EndpointApplicationRouter struct {
	runtime clientruntime.ApplicationRuntime

	mu      sync.Mutex
	closed  bool
	clients map[state.EndpointID]routedApplicationClient
	opening map[state.EndpointID]*applicationOpen
}

type routedApplicationClient struct {
	client *clientprotocol.ApplicationClient
	owned  bool
}

type applicationOpen struct {
	done   chan struct{}
	client *clientprotocol.ApplicationClient
	err    error
}

// NewEndpointApplicationRouter 复用启动 TUI 时已经建立的 initial client，并通过其
// ClientRuntime 为其它 Endpoint 按需获取共享 application lease。
func NewEndpointApplicationRouter(initialEndpointID state.EndpointID, initial *clientprotocol.ApplicationClient) (*EndpointApplicationRouter, error) {
	initialEndpointID = state.NormalizeEndpointID(initialEndpointID)
	if initialEndpointID == "" || initial == nil {
		return nil, errors.New("endpoint application router requires an initial endpoint and client")
	}
	runtime, ok := initial.ConnectionRuntime().(clientruntime.ApplicationRuntime)
	if !ok || runtime == nil {
		return nil, errors.New("endpoint application router requires an application-capable client runtime")
	}
	return &EndpointApplicationRouter{
		runtime: runtime,
		clients: map[state.EndpointID]routedApplicationClient{initialEndpointID: {client: initial}},
		opening: make(map[state.EndpointID]*applicationOpen),
	}, nil
}

func (router *EndpointApplicationRouter) application(ctx context.Context, endpointID state.EndpointID) (*clientprotocol.ApplicationClient, error) {
	endpointID = state.NormalizeEndpointID(endpointID)
	if router == nil || endpointID == "" {
		return nil, fmt.Errorf("endpoint application request requires endpoint_id")
	}
	for {
		router.mu.Lock()
		if router.closed {
			router.mu.Unlock()
			return nil, fmt.Errorf("endpoint application router is closed")
		}
		if cached, ok := router.clients[endpointID]; ok && applicationClientReady(cached.client) {
			router.mu.Unlock()
			return cached.client, nil
		} else if ok {
			delete(router.clients, endpointID)
			if cached.owned && cached.client != nil {
				_ = cached.client.Close()
			}
		}
		if pending := router.opening[endpointID]; pending != nil {
			router.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-pending.done:
				if pending.err != nil {
					return nil, pending.err
				}
				return pending.client, nil
			}
		}
		pending := &applicationOpen{done: make(chan struct{})}
		router.opening[endpointID] = pending
		router.mu.Unlock()

		ready, err := router.runtime.AcquireSession(ctx, clientruntime.ConnectRequest{
			EndpointID: clientendpoint.EndpointID(endpointID),
			Intent:     clientruntime.ConnectIntentInteractive,
		})
		var client *clientprotocol.ApplicationClient
		if err == nil {
			client, err = clientprotocol.NewRuntimeApplicationClient(ready, router.runtime)
			if err != nil {
				_ = ready.Close()
			}
		}

		router.mu.Lock()
		delete(router.opening, endpointID)
		if err == nil && router.closed {
			err = fmt.Errorf("endpoint application router is closed")
			_ = client.Close()
			client = nil
		}
		if err == nil {
			router.clients[endpointID] = routedApplicationClient{client: client, owned: true}
		}
		pending.client, pending.err = client, err
		close(pending.done)
		router.mu.Unlock()
		return client, err
	}
}

func applicationClientReady(client *clientprotocol.ApplicationClient) bool {
	if client == nil || client.Done() == nil {
		return false
	}
	select {
	case <-client.Done():
		return false
	default:
		return true
	}
}

func (router *EndpointApplicationRouter) terminal(ctx context.Context, endpointID state.EndpointID) (protocoladapter.ProtocolTerminalServiceAdapter, error) {
	client, err := router.application(ctx, endpointID)
	if err != nil {
		return protocoladapter.ProtocolTerminalServiceAdapter{}, err
	}
	return protocoladapter.ProtocolTerminalServiceAdapter{Client: client, Application: client.ApplicationSession}, nil
}

func (router *EndpointApplicationRouter) path(ctx context.Context, endpointID state.EndpointID) (protocoladapter.ProtocolPathServiceAdapter, error) {
	client, err := router.application(ctx, endpointID)
	if err != nil {
		return protocoladapter.ProtocolPathServiceAdapter{}, err
	}
	return protocoladapter.NewProtocolPathServiceAdapter(client.ApplicationSession)
}

func (router *EndpointApplicationRouter) core(ctx context.Context, endpointID state.EndpointID) (protocoladapter.ProtocolCoreClientAdapter, error) {
	client, err := router.application(ctx, endpointID)
	if err != nil {
		return protocoladapter.ProtocolCoreClientAdapter{}, err
	}
	return protocoladapter.ProtocolCoreClientAdapter{Application: client.ApplicationSession}, nil
}

func (router *EndpointApplicationRouter) Attach(ctx context.Context, req port.TerminalAttachRequest) (port.TerminalAttachResult, error) {
	adapter, err := router.terminal(ctx, req.EndpointID)
	if err != nil {
		return port.TerminalAttachResult{}, err
	}
	return adapter.Attach(ctx, req)
}

func (router *EndpointApplicationRouter) Detach(ctx context.Context, req port.TerminalDetachRequest) error {
	adapter, err := router.terminal(ctx, req.EndpointID)
	if err != nil {
		return err
	}
	return adapter.Detach(ctx, req)
}

func (router *EndpointApplicationRouter) List(ctx context.Context, req port.TerminalListRequest) (port.TerminalListResult, error) {
	adapter, err := router.terminal(ctx, req.EndpointID)
	if err != nil {
		return port.TerminalListResult{}, err
	}
	return adapter.List(ctx, req)
}

func (router *EndpointApplicationRouter) Create(ctx context.Context, req port.TerminalCreateRequest) (port.TerminalCreateResult, error) {
	adapter, err := router.terminal(ctx, req.EndpointID)
	if err != nil {
		return port.TerminalCreateResult{}, err
	}
	return adapter.Create(ctx, req)
}

func (router *EndpointApplicationRouter) Restart(ctx context.Context, req port.TerminalRestartRequest) error {
	adapter, err := router.terminal(ctx, req.EndpointID)
	if err != nil {
		return err
	}
	return adapter.Restart(ctx, req)
}

func (router *EndpointApplicationRouter) Reconnect(ctx context.Context, req port.TerminalReconnectRequest) (port.TerminalAttachResult, error) {
	adapter, err := router.terminal(ctx, req.EndpointID)
	if err != nil {
		return port.TerminalAttachResult{}, err
	}
	return adapter.Reconnect(ctx, req)
}

func (router *EndpointApplicationRouter) Kill(ctx context.Context, req port.TerminalKillRequest) error {
	adapter, err := router.terminal(ctx, req.EndpointID)
	if err != nil {
		return err
	}
	return adapter.Kill(ctx, req)
}

func (router *EndpointApplicationRouter) Remove(ctx context.Context, req port.TerminalRemoveRequest) error {
	adapter, err := router.terminal(ctx, req.EndpointID)
	if err != nil {
		return err
	}
	return adapter.Remove(ctx, req)
}

func (router *EndpointApplicationRouter) EditMetadata(ctx context.Context, req port.TerminalEditMetadataRequest) error {
	adapter, err := router.terminal(ctx, req.EndpointID)
	if err != nil {
		return err
	}
	return adapter.EditMetadata(ctx, req)
}

func (router *EndpointApplicationRouter) EditTags(ctx context.Context, req port.TerminalEditTagsRequest) error {
	adapter, err := router.terminal(ctx, req.EndpointID)
	if err != nil {
		return err
	}
	return adapter.EditTags(ctx, req)
}

func (router *EndpointApplicationRouter) SendInput(ctx context.Context, req port.TerminalInputRequest) error {
	adapter, err := router.terminal(ctx, req.EndpointID)
	if err != nil {
		return err
	}
	return adapter.SendInput(ctx, req)
}

func (router *EndpointApplicationRouter) Resize(ctx context.Context, req port.TerminalResizeRequest) (port.TerminalResizeResult, error) {
	adapter, err := router.terminal(ctx, req.EndpointID)
	if err != nil {
		return port.TerminalResizeResult{}, err
	}
	return adapter.Resize(ctx, req)
}

func (router *EndpointApplicationRouter) LiveSurface(ctx context.Context, req port.TerminalSurfaceRequest) (port.TerminalSurfaceResult, error) {
	adapter, err := router.terminal(ctx, req.EndpointID)
	if err != nil {
		return port.TerminalSurfaceResult{}, err
	}
	return adapter.LiveSurface(ctx, req)
}

func (router *EndpointApplicationRouter) ArmLiveInvalidation(ctx context.Context, req port.TerminalLiveEventRequest) (port.TerminalLiveEvent, error) {
	adapter, err := router.terminal(ctx, req.EndpointID)
	if err != nil {
		return port.TerminalLiveEvent{}, err
	}
	return adapter.ArmLiveInvalidation(ctx, req)
}

func (router *EndpointApplicationRouter) ListDirectories(ctx context.Context, req port.PathListDirectoriesRequest) (port.PathListDirectoriesResult, error) {
	adapter, err := router.path(ctx, req.EndpointID)
	if err != nil {
		return port.PathListDirectoriesResult{}, err
	}
	return adapter.ListDirectories(ctx, req)
}

func (router *EndpointApplicationRouter) Defaults(ctx context.Context, req port.PathDefaultsRequest) (port.PathDefaultsResult, error) {
	adapter, err := router.path(ctx, req.EndpointID)
	if err != nil {
		return port.PathDefaultsResult{}, err
	}
	return adapter.Defaults(ctx, req)
}

func (router *EndpointApplicationRouter) HistoryLatest(ctx context.Context, req port.HistoryLatestRequest) (port.HistoryResult, error) {
	adapter, err := router.core(ctx, req.EndpointID)
	if err != nil {
		return port.HistoryResult{RequestID: req.RequestID}, err
	}
	return adapter.HistoryLatest(ctx, req)
}

func (router *EndpointApplicationRouter) HistoryOlder(ctx context.Context, req port.HistoryOlderRequest) (port.HistoryResult, error) {
	adapter, err := router.core(ctx, req.EndpointID)
	if err != nil {
		return port.HistoryResult{RequestID: req.RequestID}, err
	}
	return adapter.HistoryOlder(ctx, req)
}

func (router *EndpointApplicationRouter) HistoryNewer(ctx context.Context, req port.HistoryNewerRequest) (port.HistoryResult, error) {
	adapter, err := router.core(ctx, req.EndpointID)
	if err != nil {
		return port.HistoryResult{RequestID: req.RequestID}, err
	}
	return adapter.HistoryNewer(ctx, req)
}

func (router *EndpointApplicationRouter) HistoryOldest(ctx context.Context, req port.HistoryOldestRequest) (port.HistoryResult, error) {
	adapter, err := router.core(ctx, req.EndpointID)
	if err != nil {
		return port.HistoryResult{RequestID: req.RequestID}, err
	}
	return adapter.HistoryOldest(ctx, req)
}

func (router *EndpointApplicationRouter) HistoryCopyRange(ctx context.Context, req port.HistoryCopyRangeRequest) (port.HistoryCopyRangeResult, error) {
	adapter, err := router.core(ctx, req.EndpointID)
	if err != nil {
		return port.HistoryCopyRangeResult{}, err
	}
	return adapter.HistoryCopyRange(ctx, req)
}

func (router *EndpointApplicationRouter) ReleaseHistory(ctx context.Context, req port.HistoryReleaseRequest) error {
	adapter, err := router.core(ctx, req.EndpointID)
	if err != nil {
		return err
	}
	return adapter.ReleaseHistory(ctx, req)
}

// Close 释放 router 自己获取的 consumer leases；initial client 仍由 composition root 关闭。
func (router *EndpointApplicationRouter) Close() error {
	if router == nil {
		return nil
	}
	router.mu.Lock()
	if router.closed {
		router.mu.Unlock()
		return nil
	}
	router.closed = true
	clients := make([]*clientprotocol.ApplicationClient, 0, len(router.clients))
	for _, cached := range router.clients {
		if cached.owned && cached.client != nil {
			clients = append(clients, cached.client)
		}
	}
	router.clients = nil
	router.mu.Unlock()
	var errs []error
	for _, client := range clients {
		errs = append(errs, client.Close())
	}
	return errors.Join(errs...)
}

var _ port.TerminalService = (*EndpointApplicationRouter)(nil)
var _ port.NativeScreenSource = (*EndpointApplicationRouter)(nil)
var _ port.LiveInvalidationSource = (*EndpointApplicationRouter)(nil)
var _ port.PathService = (*EndpointApplicationRouter)(nil)
var _ port.CoreClient = (*EndpointApplicationRouter)(nil)
