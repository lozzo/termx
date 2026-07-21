package httpapi

import (
	"context"
	"io"
	"sync"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

type streamSource struct {
	body io.ReadCloser
	mu   sync.Mutex
	once sync.Once
}

func newStreamSource(body io.ReadCloser) *streamSource {
	return &streamSource{body: body}
}

func (source *streamSource) receive(ctx context.Context, target proto.Message) error {
	if source == nil || source.body == nil || ctx == nil {
		return io.EOF
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	result := make(chan error, 1)
	go func() {
		result <- ReadFrame(source.body, target)
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		_ = source.close()
		<-result
		return ctx.Err()
	}
}

func (source *streamSource) close() error {
	if source == nil {
		return nil
	}
	var err error
	source.once.Do(func() {
		if source.body != nil {
			err = source.body.Close()
		}
	})
	return err
}

type presenceSource struct{ *streamSource }

// Receive 读取下一条 length-prefixed PresenceEvent，并在 caller cancel 时关闭当前 HTTP body。
func (source *presenceSource) Receive(ctx context.Context) (*cloudpb.PresenceEvent, error) {
	event := &cloudpb.PresenceEvent{}
	if err := source.receive(ctx, event); err != nil {
		return nil, err
	}
	return event, nil
}

// Close 幂等关闭当前 device presence HTTP stream。
func (source *presenceSource) Close() error { return source.close() }

type signalingSource struct{ *streamSource }

// Receive 读取下一条 length-prefixed SignalingEvent，并在 caller cancel 时关闭当前 HTTP body。
func (source *signalingSource) Receive(ctx context.Context) (*cloudpb.SignalingEvent, error) {
	event := &cloudpb.SignalingEvent{}
	if err := source.receive(ctx, event); err != nil {
		return nil, err
	}
	return event, nil
}

// Close 幂等关闭当前 managed signaling HTTP stream。
func (source *signalingSource) Close() error { return source.close() }
