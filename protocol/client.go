package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/lozzow/termx/transport"
)

type Client struct {
	transport transport.Transport
	nextID    atomic.Uint64

	mu      sync.Mutex
	sendMu  sync.Mutex
	waiters map[uint64]chan result
	streams map[uint16]*clientStream
	pending map[uint16][]StreamFrame
	reused  map[uint16][]StreamFrame
	dropped map[uint16]struct{}
	events  chan Event

	helloCh chan result
	done    chan struct{}
}

type result struct {
	payload json.RawMessage
	err     error
}

type StreamFrame struct {
	Type    uint8
	Payload []byte
}

type clientStream struct {
	mu sync.Mutex
	ch chan StreamFrame
}

func newClientStream() *clientStream {
	return &clientStream{ch: make(chan StreamFrame, 256)}
}

func (s *clientStream) channel() chan StreamFrame {
	return s.ch
}

func (s *clientStream) send(frame StreamFrame) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ch == nil {
		return
	}
	select {
	case s.ch <- frame:
	default:
	}
}

func (s *clientStream) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ch == nil {
		return
	}
	close(s.ch)
	s.ch = nil
}

func NewClient(t transport.Transport) *Client {
	c := &Client{
		transport: t,
		waiters:   make(map[uint64]chan result),
		streams:   make(map[uint16]*clientStream),
		pending:   make(map[uint16][]StreamFrame),
		reused:    make(map[uint16][]StreamFrame),
		dropped:   make(map[uint16]struct{}),
		helloCh:   make(chan result, 1),
		done:      make(chan struct{}),
	}
	go c.readLoop()
	return c
}

func (c *Client) Close() error {
	err := c.transport.Close()
	<-c.done
	return err
}

func (c *Client) Hello(ctx context.Context, hello Hello) error {
	payload, err := json.Marshal(hello)
	if err != nil {
		return err
	}
	frame, err := EncodeFrame(0, TypeHello, payload)
	if err != nil {
		return err
	}
	if err := c.send(frame); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case res := <-c.helloCh:
		return res.err
	}
}

func (c *Client) Create(ctx context.Context, params CreateParams) (*CreateResult, error) {
	var out CreateResult
	if err := c.doRequest(ctx, "create", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) List(ctx context.Context) (*ListResult, error) {
	var out ListResult
	if err := c.doRequest(ctx, "list", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Kill(ctx context.Context, terminalID string) error {
	return c.doRequest(ctx, "kill", GetParams{TerminalID: terminalID}, nil)
}

func (c *Client) Restart(ctx context.Context, terminalID string) error {
	return c.doRequest(ctx, "restart", GetParams{TerminalID: terminalID}, nil)
}

func (c *Client) Remove(ctx context.Context, terminalID string) error {
	return c.doRequest(ctx, "remove", GetParams{TerminalID: terminalID}, nil)
}

func (c *Client) SetTags(ctx context.Context, terminalID string, tags map[string]string) error {
	return c.doRequest(ctx, "set_tags", SetTagsParams{
		TerminalID: terminalID,
		Tags:       tags,
	}, nil)
}

func (c *Client) SetMetadata(ctx context.Context, terminalID string, name string, tags map[string]string) error {
	return c.doRequest(ctx, "set_metadata", SetMetadataParams{
		TerminalID: terminalID,
		Name:       name,
		Tags:       tags,
	}, nil)
}

func (c *Client) Attach(ctx context.Context, terminalID string, mode string) (*AttachResult, error) {
	var out AttachResult
	if err := c.doRequest(ctx, "attach", AttachParams{TerminalID: terminalID, Mode: mode}, &out); err != nil {
		return nil, err
	}
	c.mu.Lock()
	stream := c.streams[out.Channel]
	if stream == nil {
		stream = newClientStream()
		c.streams[out.Channel] = stream
	}
	delete(c.dropped, out.Channel)
	pending := c.pending[out.Channel]
	delete(c.pending, out.Channel)
	reused := c.reused[out.Channel]
	delete(c.reused, out.Channel)
	c.mu.Unlock()
	for _, frame := range pending {
		stream.send(frame)
	}
	for _, frame := range reused {
		stream.send(frame)
	}
	return &out, nil
}

func (c *Client) Snapshot(ctx context.Context, terminalID string, offset, limit int) (*Snapshot, error) {
	var out Snapshot
	if err := c.doRequest(ctx, "snapshot", SnapshotParams{
		TerminalID:       terminalID,
		ScrollbackOffset: offset,
		ScrollbackLimit:  limit,
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateSession(ctx context.Context, params CreateSessionParams) (*SessionSnapshot, error) {
	var out SessionSnapshot
	if err := c.doRequest(ctx, "session.create", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListSessions(ctx context.Context) (*ListSessionsResult, error) {
	var out ListSessionsResult
	if err := c.doRequest(ctx, "session.list", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetSession(ctx context.Context, sessionID string) (*SessionSnapshot, error) {
	var out SessionSnapshot
	if err := c.doRequest(ctx, "session.get", GetSessionParams{SessionID: sessionID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) AttachSession(ctx context.Context, params AttachSessionParams) (*SessionSnapshot, error) {
	var out SessionSnapshot
	if err := c.doRequest(ctx, "session.attach", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DetachSession(ctx context.Context, sessionID, viewID string) error {
	return c.doRequest(ctx, "session.detach", DetachSessionParams{
		SessionID: sessionID,
		ViewID:    viewID,
	}, nil)
}

func (c *Client) ApplySession(ctx context.Context, params ApplySessionParams) (*SessionSnapshot, error) {
	var out SessionSnapshot
	if err := c.doRequest(ctx, "session.apply", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ReplaceSession(ctx context.Context, params ReplaceSessionParams) (*SessionSnapshot, error) {
	var out SessionSnapshot
	if err := c.doRequest(ctx, "session.replace", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateSessionView(ctx context.Context, params UpdateSessionViewParams) (*ViewInfo, error) {
	var out ViewInfo
	if err := c.doRequest(ctx, "session.view_update", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) AcquireSessionLease(ctx context.Context, params AcquireSessionLeaseParams) (*LeaseInfo, error) {
	var out LeaseInfo
	if err := c.doRequest(ctx, "session.acquire_lease", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ReleaseSessionLease(ctx context.Context, params ReleaseSessionLeaseParams) error {
	return c.doRequest(ctx, "session.release_lease", params, nil)
}

func (c *Client) Events(ctx context.Context, params EventsParams) (<-chan Event, error) {
	c.mu.Lock()
	if c.events == nil {
		c.events = make(chan Event, 64)
	}
	events := c.events
	c.mu.Unlock()

	if err := c.doRequest(ctx, "events", params, nil); err != nil {
		return nil, err
	}
	return events, nil
}

func (c *Client) Input(ctx context.Context, channel uint16, data []byte) error {
	frame, err := EncodeFrame(channel, TypeInput, data)
	if err != nil {
		return err
	}
	return c.send(frame)
}

func (c *Client) Resize(ctx context.Context, channel uint16, cols, rows uint16) error {
	frame, err := EncodeFrame(channel, TypeResize, EncodeResizePayload(cols, rows))
	if err != nil {
		return err
	}
	return c.send(frame)
}

func (c *Client) Stream(channel uint16) (<-chan StreamFrame, func()) {
	c.mu.Lock()
	stream := c.streams[channel]
	if stream == nil {
		if _, dropped := c.dropped[channel]; dropped {
			c.mu.Unlock()
			idle := make(chan StreamFrame)
			return idle, func() {}
		}
		stream = newClientStream()
		c.streams[channel] = stream
	}
	pending := c.pending[channel]
	delete(c.pending, channel)
	c.mu.Unlock()
	for _, frame := range pending {
		stream.send(frame)
	}

	return stream.channel(), func() {
		c.mu.Lock()
		if current, ok := c.streams[channel]; ok {
			delete(c.streams, channel)
			c.dropped[channel] = struct{}{}
			current.close()
		}
		delete(c.pending, channel)
		c.mu.Unlock()
	}
}

func (c *Client) doRequest(ctx context.Context, method string, params any, out any) error {
	payload, err := json.Marshal(params)
	if err != nil {
		return err
	}
	id := c.nextID.Add(1)
	reqPayload, err := json.Marshal(Request{
		ID:     id,
		Method: method,
		Params: payload,
	})
	if err != nil {
		return err
	}
	frame, err := EncodeFrame(0, TypeRequest, reqPayload)
	if err != nil {
		return err
	}

	resCh := make(chan result, 1)
	c.mu.Lock()
	c.waiters[id] = resCh
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.waiters, id)
		c.mu.Unlock()
	}()

	if err := c.send(frame); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case res := <-resCh:
		if res.err != nil {
			return res.err
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(res.payload, out)
	}
}

func (c *Client) send(frame []byte) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.transport.Send(frame)
}

func (c *Client) readLoop() {
	defer close(c.done)
	for {
		frame, err := c.transport.Recv()
		if err != nil {
			c.failAll(err)
			return
		}
		channel, typ, payload, err := DecodeFrame(frame)
		if err != nil {
			c.failAll(err)
			return
		}
		if channel == 0 {
			switch typ {
			case TypeHello:
				c.helloCh <- result{}
			case TypeEvent:
				var evt Event
				if err := json.Unmarshal(payload, &evt); err != nil {
					c.failAll(err)
					return
				}
				c.mu.Lock()
				ch := c.events
				c.mu.Unlock()
				if ch != nil {
					select {
					case ch <- evt:
					default:
					}
				}
			case TypeResponse:
				var resp Response
				if err := json.Unmarshal(payload, &resp); err != nil {
					c.failAll(err)
					return
				}
				c.mu.Lock()
				ch := c.waiters[resp.ID]
				c.mu.Unlock()
				if ch != nil {
					ch <- result{payload: resp.Result}
				}
			case TypeError:
				var msg ErrorMessage
				if err := json.Unmarshal(payload, &msg); err != nil {
					c.failAll(err)
					return
				}
				c.mu.Lock()
				ch := c.waiters[msg.ID]
				c.mu.Unlock()
				if ch != nil {
					ch <- result{err: fmt.Errorf("protocol error %d: %s", msg.Error.Code, msg.Error.Message)}
				}
			}
			continue
		}

		c.mu.Lock()
		stream := c.streams[channel]
		if stream == nil {
			if _, dropped := c.dropped[channel]; dropped {
				queue := c.reused[channel]
				if len(queue) < 256 {
					c.reused[channel] = append(queue, StreamFrame{Type: typ, Payload: append([]byte(nil), payload...)})
				}
				c.mu.Unlock()
				continue
			}
			queue := c.pending[channel]
			if len(queue) < 256 {
				c.pending[channel] = append(queue, StreamFrame{Type: typ, Payload: append([]byte(nil), payload...)})
			}
			c.mu.Unlock()
			continue
		}
		c.mu.Unlock()
		stream.send(StreamFrame{Type: typ, Payload: append([]byte(nil), payload...)})
	}
}

func (c *Client) failAll(err error) {
	if err == nil {
		err = io.EOF
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, ch := range c.waiters {
		ch <- result{err: err}
		delete(c.waiters, id)
	}
	select {
	case c.helloCh <- result{err: err}:
	default:
	}
	for id, stream := range c.streams {
		stream.close()
		delete(c.streams, id)
	}
	for id := range c.pending {
		delete(c.pending, id)
	}
	for id := range c.reused {
		delete(c.reused, id)
	}
	for id := range c.dropped {
		delete(c.dropped, id)
	}
	if c.events != nil {
		close(c.events)
		c.events = nil
	}
}
