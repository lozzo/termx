package clientruntimeadapter

import (
	"context"
	"fmt"

	"github.com/lozzow/termx/client/endpoint"
	clientruntime "github.com/lozzow/termx/client/runtime"
	"github.com/lozzow/termx/tui/port"
	"github.com/lozzow/termx/tui/state"
)

// EndpointEventSource 把共享 ClientRuntime 的 endpoint mailbox 投影为 TUI application event source。
// 它不建立连接、不保存 generation，也不解释 terminal lifecycle；关闭由调用 context 统一驱动。
type EndpointEventSource struct {
	Runtime    clientruntime.Runtime
	EndpointID state.EndpointID
}

// WatchEndpointEvents 订阅一个 Endpoint 的 runtime event，并在独立 bounded channel 中做纯枚举/展示映射。
func (source EndpointEventSource) WatchEndpointEvents(ctx context.Context) (<-chan port.EndpointRuntimeEvent, error) {
	if source.Runtime == nil || ctx == nil || state.NormalizeEndpointID(source.EndpointID) == "" {
		return nil, fmt.Errorf("endpoint event source requires runtime, context, and endpoint_id")
	}
	events, err := source.Runtime.WatchEndpoint(ctx, clientruntimeEndpointID(source.EndpointID))
	if err != nil {
		return nil, err
	}
	out := make(chan port.EndpointRuntimeEvent, 16)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				projected := ProjectEndpointEvent(event)
				select {
				case out <- projected:
				default:
					select {
					case <-out:
					default:
					}
					select {
					case out <- projected:
					default:
					}
				}
			}
		}
	}()
	return out, nil
}

func clientruntimeEndpointID(endpointID state.EndpointID) endpoint.EndpointID {
	return endpoint.EndpointID(state.NormalizeEndpointID(endpointID))
}

var _ port.EndpointEventSource = EndpointEventSource{}
