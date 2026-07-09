package services

import (
	"context"
	"fmt"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-proto/wire"
)

// ProtocolClientControlClient 是 TUI client control adapter 依赖的 protocol 边界。
// Call 必须发到当前 endpoint 的 daemon；Stream 只消费 daemon 为 mailbox 分配的 channel，
// adapter 不从这里推断 panel、tab、float 或 terminal lifecycle truth。
type ProtocolClientControlClient interface {
	Call(context.Context, string, any, any) error
	Stream(uint16) (<-chan protocol.StreamFrame, func())
}

// ProtocolClientControlAdapter 把 daemon client.control.* protocol 映射成 TUI service 能力。
// 它只负责注册 client session、打开 mailbox stream、解码 invocation 和提交 response；
// 真正的 action 执行仍属于 TUI reducer/runtime 或后续插件 runner。
type ProtocolClientControlAdapter struct {
	Client ProtocolClientControlClient
	Buffer int
}

// Register 向当前 endpoint daemon 声明本 TUI session 的 client action catalog。
// 该方法只透传 client-owned action 投影；daemon 返回的时间戳表示 broker presence，不是 UI state。
func (adapter ProtocolClientControlAdapter) Register(ctx context.Context, params protocol.ClientSessionRegisterParams) (protocol.ClientSessionRegisterResult, error) {
	if adapter.Client == nil {
		return protocol.ClientSessionRegisterResult{}, fmt.Errorf("missing client control protocol client")
	}
	var out protocol.ClientSessionRegisterResult
	if err := adapter.Client.Call(ctx, protocol.MethodClientSessionRegister, params, &out); err != nil {
		return protocol.ClientSessionRegisterResult{}, err
	}
	return out, nil
}

// List 查询当前 endpoint daemon 已知的 client session 目录。
// 目录仅用于路由和诊断，不代表这些 session 的 active panel、tab 或 terminal 绑定真值。
func (adapter ProtocolClientControlAdapter) List(ctx context.Context, params protocol.ClientSessionListParams) (protocol.ClientSessionListResult, error) {
	if adapter.Client == nil {
		return protocol.ClientSessionListResult{}, fmt.Errorf("missing client control protocol client")
	}
	var out protocol.ClientSessionListResult
	if err := adapter.Client.Call(ctx, protocol.MethodClientSessionList, params, &out); err != nil {
		return protocol.ClientSessionListResult{}, err
	}
	return out, nil
}

// Watch 打开本 TUI session 的 client control mailbox。
// 返回 channel 只产出 daemon 已经校验并派生 Source 的 invocation；ctx 取消或 daemon 关闭 stream 时 channel 会关闭。
func (adapter ProtocolClientControlAdapter) Watch(ctx context.Context, params protocol.ClientControlWatchParams) (<-chan protocol.ClientControlInvocation, error) {
	if adapter.Client == nil {
		return nil, fmt.Errorf("missing client control protocol client")
	}
	var watch protocol.ClientControlWatchResult
	if err := adapter.Client.Call(ctx, protocol.MethodClientControlWatch, params, &watch); err != nil {
		return nil, err
	}
	frames, stop := adapter.Client.Stream(watch.Channel)
	out := make(chan protocol.ClientControlInvocation, adapter.bufferSize())
	go adapter.forwardClientControlFrames(ctx, watch.SessionID, watch.Channel, frames, out, stop)
	return out, nil
}

// Call 向当前 endpoint daemon broker 提交 client action 调用。
// Source 不由调用方填写；daemon 会根据已认证的 protocol/runner 边界派生到 mailbox invocation。
func (adapter ProtocolClientControlAdapter) Call(ctx context.Context, params protocol.ClientControlCallParams) (protocol.ClientControlCallResult, error) {
	if adapter.Client == nil {
		return protocol.ClientControlCallResult{}, fmt.Errorf("missing client control protocol client")
	}
	var out protocol.ClientControlCallResult
	if err := adapter.Client.Call(ctx, protocol.MethodClientControlCall, params, &out); err != nil {
		return protocol.ClientControlCallResult{}, err
	}
	return out, nil
}

// Respond 把 TUI action 执行结果回写给 daemon broker。
// response 必须携带原 invocation 的 SessionID、RequestID 和 TraceParent；adapter 不伪造或替换 trace。
func (adapter ProtocolClientControlAdapter) Respond(ctx context.Context, params protocol.ClientControlResponseParams) (protocol.ClientControlResponseResult, error) {
	if adapter.Client == nil {
		return protocol.ClientControlResponseResult{}, fmt.Errorf("missing client control protocol client")
	}
	var out protocol.ClientControlResponseResult
	if err := adapter.Client.Call(ctx, protocol.MethodClientControlRespond, params, &out); err != nil {
		return protocol.ClientControlResponseResult{}, err
	}
	return out, nil
}

// Unwatch 主动关闭当前 endpoint daemon 上的 mailbox watcher。
// 调用方取消 Watch ctx 时 adapter 会自动调用它；显式调用主要用于测试或未来 runtime 精准释放。
func (adapter ProtocolClientControlAdapter) Unwatch(ctx context.Context, params protocol.ClientControlUnwatchParams) (protocol.ClientControlUnwatchResult, error) {
	if adapter.Client == nil {
		return protocol.ClientControlUnwatchResult{}, fmt.Errorf("missing client control protocol client")
	}
	var out protocol.ClientControlUnwatchResult
	if err := adapter.Client.Call(ctx, protocol.MethodClientControlUnwatch, params, &out); err != nil {
		return protocol.ClientControlUnwatchResult{}, err
	}
	return out, nil
}

func (adapter ProtocolClientControlAdapter) forwardClientControlFrames(ctx context.Context, sessionID string, channel uint16, frames <-chan protocol.StreamFrame, out chan<- protocol.ClientControlInvocation, stop func()) {
	defer close(out)
	defer stop()
	defer adapter.unwatchClientControl(sessionID, channel)
	for {
		var frame protocol.StreamFrame
		var ok bool
		select {
		case <-ctx.Done():
			return
		case frame, ok = <-frames:
			if !ok {
				return
			}
		}
		switch frame.Type {
		case wire.TypeClosed:
			return
		case wire.TypeClientControl:
			invocation, err := protocol.DecodeClientControlInvocationPayload(frame.Payload)
			if err != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case out <- invocation:
			}
		}
	}
}

func (adapter ProtocolClientControlAdapter) unwatchClientControl(sessionID string, channel uint16) {
	if adapter.Client == nil || sessionID == "" || channel == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var out protocol.ClientControlUnwatchResult
	_ = adapter.Client.Call(ctx, protocol.MethodClientControlUnwatch, protocol.ClientControlUnwatchParams{
		SessionID: sessionID,
		Channel:   channel,
	}, &out)
}

func (adapter ProtocolClientControlAdapter) bufferSize() int {
	if adapter.Buffer > 0 {
		return adapter.Buffer
	}
	return 64
}
