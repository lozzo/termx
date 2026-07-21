package companion

import (
	"context"
	"io"
	"sync"

	"github.com/muxvia/muxvia/private/cloud/companion/cloudservice"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
	"google.golang.org/protobuf/proto"
)

type ownedStream interface {
	Close() error
}

type boundedStream[T proto.Message] struct {
	queue       chan T
	done        chan struct{}
	cancel      context.CancelFunc
	sourceClose func() error
	deregister  func()
	onFinish    func()

	finishOnce sync.Once
	mu         sync.Mutex
	terminal   error
}

func newBoundedStream[T proto.Message](parent context.Context, capacity int, receive func(context.Context) (T, error), sourceClose func() error, register func(ownedStream) func(), onFinish func(), wrap func(*boundedStream[T]) ownedStream) *boundedStream[T] {
	ctx, cancel := context.WithCancel(parent)
	stream := &boundedStream[T]{queue: make(chan T, capacity), done: make(chan struct{}), cancel: cancel, sourceClose: sourceClose, onFinish: onFinish}
	owner := wrap(stream)
	stream.deregister = register(owner)
	go stream.pump(ctx, receive)
	return stream
}

func (stream *boundedStream[T]) pump(ctx context.Context, receive func(context.Context) (T, error)) {
	for {
		message, err := receive(ctx)
		if err != nil {
			if err == context.Canceled {
				err = io.EOF
			} else if err != io.EOF {
				err = sanitizeAdapterError(err)
			}
			stream.finish(err)
			return
		}
		if any(message) == nil {
			stream.finish(protocolError("cloud stream returned an empty event"))
			return
		}
		message = cloneMessage(message)
		select {
		case stream.queue <- message:
		case <-ctx.Done():
			stream.finish(io.EOF)
			return
		default:
			stream.finish(cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_BACKPRESSURE, "cloud companion stream queue is full"))
			return
		}
	}
}

func (stream *boundedStream[T]) receive() (T, error) {
	select {
	case message := <-stream.queue:
		return cloneMessage(message), nil
	default:
	}
	select {
	case message := <-stream.queue:
		return cloneMessage(message), nil
	case <-stream.done:
		select {
		case message := <-stream.queue:
			return cloneMessage(message), nil
		default:
		}
		stream.mu.Lock()
		err := stream.terminal
		stream.mu.Unlock()
		var zero T
		if err == nil {
			err = io.EOF
		}
		return zero, err
	}
}

func (stream *boundedStream[T]) close() error {
	stream.finish(io.EOF)
	return nil
}

func (stream *boundedStream[T]) finish(err error) {
	stream.finishOnce.Do(func() {
		stream.mu.Lock()
		stream.terminal = err
		stream.mu.Unlock()
		stream.cancel()
		_ = stream.sourceClose()
		close(stream.done)
		if stream.deregister != nil {
			stream.deregister()
		}
		if stream.onFinish != nil {
			stream.onFinish()
		}
	})
}

type presenceStream struct {
	stream     *boundedStream[*cloudpb.PresenceEvent]
	trackOffer func(string, string)
}

func newPresenceStream(ctx context.Context, source cloudservice.PresenceSource, presenceSessionID, targetDeviceID string, capacity int, register func(ownedStream) func(), trackOffer func(string, string), onFinish func()) *presenceStream {
	owner := &presenceStream{trackOffer: trackOffer}
	receive := func(ctx context.Context) (*cloudpb.PresenceEvent, error) {
		event, err := source.Receive(ctx)
		if err != nil {
			return nil, err
		}
		return sanitizePresenceEvent(event, presenceSessionID, targetDeviceID)
	}
	owner.stream = newBoundedStream(ctx, capacity, receive, source.Close, register, onFinish, func(stream *boundedStream[*cloudpb.PresenceEvent]) ownedStream {
		owner.stream = stream
		return owner
	})
	return owner
}

// Receive 返回下一条经过 session/target 校验和错误脱敏的 daemon presence 事件。
// 只有实际交付给 caller 的 offer 才会登记为可 CompleteSignalingOffer 的 connection-owned ID。
func (stream *presenceStream) Receive() (*cloudpb.PresenceEvent, error) {
	event, err := stream.stream.receive()
	if err == nil && event.GetOffer() != nil {
		stream.trackOffer(event.GetOffer().GetSignalingSessionId(), event.GetOffer().GetManagedSessionId())
	}
	return event, err
}

// Close 幂等关闭当前 daemon presence stream，并解除阻塞 Receive。
// 关闭只清理 owning connection 的 offer ownership，不影响其他 daemon connection。
func (stream *presenceStream) Close() error {
	return stream.stream.close()
}

type signalingStream struct {
	stream *boundedStream[*cloudpb.SignalingEvent]
}

func newSignalingStream(ctx context.Context, source cloudservice.SignalingSource, capacity int, register func(ownedStream) func()) *signalingStream {
	owner := &signalingStream{}
	receive := func(ctx context.Context) (*cloudpb.SignalingEvent, error) {
		event, err := source.Receive(ctx)
		if err != nil {
			return nil, err
		}
		return sanitizeSignalingEvent(event)
	}
	owner.stream = newBoundedStream(ctx, capacity, receive, source.Close, register, nil, func(stream *boundedStream[*cloudpb.SignalingEvent]) ownedStream {
		owner.stream = stream
		return owner
	})
	return owner
}

// Receive 返回下一条经过 schema 校验和错误脱敏的 client signaling 事件。
func (stream *signalingStream) Receive() (*cloudpb.SignalingEvent, error) {
	return stream.stream.receive()
}

// Close 幂等关闭当前 client signaling stream，并解除阻塞 Receive。
// 关闭不会停止同一进程中的 daemon presence 或其他 managed session。
func (stream *signalingStream) Close() error {
	return stream.stream.close()
}
