package protocol

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/lozzow/termx/termx-proto/wire"

	"github.com/lozzow/termx/termx-shared/perftrace"
	"github.com/lozzow/termx/termx-shared/transport"
)

type Client struct {
	transport transport.Transport
	nextID    atomic.Uint64

	mu               sync.Mutex
	sendMu           sync.Mutex
	waiters          map[uint64]chan result
	streams          map[uint16]*clientStream
	pending          map[uint16][]StreamFrame
	reused           map[uint16][]StreamFrame
	dropped          map[uint16]struct{}
	eventSubscribers map[uint64]eventSubscription
	nextEventSubID   uint64
	eventsStarted    bool
	eventStartDone   chan struct{}
	eventStartErr    error

	helloCh chan result
	done    chan struct{}
}

type result struct {
	payload []byte
	err     error
}

type StreamFrame struct {
	Type    uint8
	Payload []byte
}

type HistoryReplayPage struct {
	BeforeOffset int
	Limit        int
	Rows         int
	HasMore      bool
	Replay       string
}

type eventSubscription struct {
	params EventsParams
	ch     chan Event
}

type clientStream struct {
	mu         sync.Mutex
	cond       *sync.Cond
	ch         chan StreamFrame
	done       chan struct{}
	queue      []StreamFrame
	queueLimit int
	closed     bool
}

func newClientStream() *clientStream {
	return newClientStreamWithConfig(256, 1)
}

func newClientStreamWithConfig(queueLimit, channelCapacity int) *clientStream {
	if queueLimit <= 0 {
		queueLimit = 1
	}
	if channelCapacity < 0 {
		channelCapacity = 0
	}
	s := &clientStream{
		ch:         make(chan StreamFrame, channelCapacity),
		done:       make(chan struct{}),
		queueLimit: queueLimit,
		queue:      make([]StreamFrame, 0, min(queueLimit, 16)),
	}
	s.cond = sync.NewCond(&s.mu)
	go s.run()
	return s
}

func (s *clientStream) channel() chan StreamFrame {
	return s.ch
}

func (s *clientStream) send(frame StreamFrame) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.enqueueFrameLocked(frame)
	s.cond.Signal()
}

func (s *clientStream) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.done)
	s.queue = nil
	s.cond.Broadcast()
}

func (s *clientStream) run() {
	ch := s.channel()
	defer close(ch)
	for {
		frame, ok := s.nextFrame()
		if !ok {
			return
		}
		select {
		case ch <- frame:
		case <-s.done:
			return
		}
	}
}

func (s *clientStream) nextFrame() (StreamFrame, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		if len(s.queue) > 0 {
			frame := s.queue[0]
			copy(s.queue, s.queue[1:])
			last := len(s.queue) - 1
			s.queue[last] = StreamFrame{}
			s.queue = s.queue[:last]
			return frame, true
		}
		if s.closed {
			return StreamFrame{}, false
		}
		s.cond.Wait()
	}
}

func (s *clientStream) enqueueFrameLocked(frame StreamFrame) {
	if len(s.queue) >= s.queueLimit {
		return
	}
	payload := frame.Payload
	if len(payload) > 0 {
		payload = append([]byte(nil), payload...)
	}
	s.queue = append(s.queue, StreamFrame{
		Type:    frame.Type,
		Payload: payload,
	})
}

func NewClient(t transport.Transport) *Client {
	c := &Client{
		transport:        t,
		waiters:          make(map[uint64]chan result),
		streams:          make(map[uint16]*clientStream),
		pending:          make(map[uint16][]StreamFrame),
		reused:           make(map[uint16][]StreamFrame),
		dropped:          make(map[uint16]struct{}),
		eventSubscribers: make(map[uint64]eventSubscription),
		helloCh:          make(chan result, 1),
		done:             make(chan struct{}),
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
	payload, err := EncodeHelloPayload(hello)
	if err != nil {
		return err
	}
	frame, err := wire.EncodeFrame(0, wire.TypeHello, payload)
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

func (c *Client) Call(ctx context.Context, method string, params any, out any) error {
	return c.doRequest(ctx, method, params, out)
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
	return c.AttachWithOptions(ctx, AttachParams{TerminalID: terminalID, Mode: mode})
}

func (c *Client) AttachWithOptions(ctx context.Context, params AttachParams) (*AttachResult, error) {
	var out AttachResult
	if err := c.doRequest(ctx, "attach", params, &out); err != nil {
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

func (c *Client) Detach(ctx context.Context, params DetachParams) error {
	return c.doRequest(ctx, "detach", params, nil)
}

func (c *Client) EnsureResize(ctx context.Context, params EnsureResizeParams) (*EnsureResizeResult, error) {
	var out EnsureResizeResult
	if err := c.doRequest(ctx, "ensure_resize", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) LockResize(ctx context.Context, params ResizeControlParams) (*ResizeControlResult, error) {
	var out ResizeControlResult
	if err := c.doRequest(ctx, "resize.lock", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UnlockResize(ctx context.Context, params ResizeControlParams) (*ResizeControlResult, error) {
	var out ResizeControlResult
	if err := c.doRequest(ctx, "resize.unlock", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// LiveScreen 返回 core 当前 latest native screen。
// 它是 v3 live display 的专用路径，不读取 scrollback，也不使用 HistoryGeneration 承载 revision。
func (c *Client) LiveScreen(ctx context.Context, terminalID string) (*NativeScreenSnapshot, error) {
	payload, err := c.doRequestPayload(ctx, "live.screen.get", LiveScreenParams{TerminalID: terminalID})
	if err != nil {
		return nil, err
	}
	return DecodeNativeScreenSnapshotPayload(payload)
}

// NextLiveInvalidation 阻塞等待指定 terminal 的下一次 live screen 失效通知。
// observedRevision 是客户端已观察到的 latest native screen revision，不是渲染进度；
// core 只用它补 one-shot arm 间隙丢失的 wake，不维护客户端 frame 队列。
func (c *Client) NextLiveInvalidation(ctx context.Context, terminalID string, observedRevision uint64) (*Event, error) {
	payload, err := c.doRequestPayload(ctx, "live.invalidation.next", LiveInvalidationNextParams{
		TerminalID:       terminalID,
		ObservedRevision: observedRevision,
	})
	if err != nil {
		return nil, err
	}
	event, err := DecodeEventPayload(payload)
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (c *Client) HistoryWindow(ctx context.Context, params HistoryWindowParams) (*HistoryWindow, error) {
	payload, err := c.doRequestPayload(ctx, "history.window", params)
	if err != nil {
		return nil, err
	}
	return DecodeHistoryWindowPayload(payload)
}

func (c *Client) HistoryCopy(ctx context.Context, params HistoryWindowParams) (string, error) {
	payload, err := c.doRequestPayload(ctx, "history.copy", params)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func (c *Client) ReleaseHistory(ctx context.Context, params HistoryWindowParams) error {
	return c.doRequest(ctx, "history.release", params, nil)
}

// HistoryBacklogStatus 返回 core-v2 history consumer 的只读 backlog 诊断。
// 它不触发 history flush，也不拉取 history.window；调用方只能用它观测
// pending bytes 和背压统计，不能把它作为 authoritative history payload。
func (c *Client) HistoryBacklogStatus(ctx context.Context, terminalID string) (*HistoryBacklogStatus, error) {
	var out HistoryBacklogStatus
	if err := c.doRequest(ctx, "history.backlog.status", GetParams{TerminalID: terminalID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) StorageGet(ctx context.Context, params StorageGetParams) (*StorageEntry, error) {
	var out StorageEntry
	if err := c.doRequest(ctx, "storage.get", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) StoragePut(ctx context.Context, params StoragePutParams) (*StorageEntry, error) {
	var out StorageEntry
	if err := c.doRequest(ctx, "storage.put", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) StorageDelete(ctx context.Context, params StorageDeleteParams) (*StorageDeleteResult, error) {
	var out StorageDeleteResult
	if err := c.doRequest(ctx, "storage.delete", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) StorageList(ctx context.Context, params StorageListParams) (*StorageListResult, error) {
	var out StorageListResult
	if err := c.doRequest(ctx, "storage.list", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) WorkbenchGet(ctx context.Context, params WorkbenchGetParams) (*WorkbenchSnapshot, error) {
	var out WorkbenchSnapshot
	if err := c.doRequest(ctx, "workbench.get", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) WorkbenchApply(ctx context.Context, params WorkbenchMutateParams) (*WorkbenchMutateResult, error) {
	var out WorkbenchMutateResult
	if err := c.doRequest(ctx, "workbench.apply", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Events(ctx context.Context, params EventsParams) (<-chan Event, error) {
	ch := make(chan Event, 64)
	c.mu.Lock()
	c.nextEventSubID++
	id := c.nextEventSubID
	c.eventSubscribers[id] = eventSubscription{params: params, ch: ch}
	c.mu.Unlock()

	if err := c.ensureEventsStarted(ctx); err != nil {
		c.removeEventSubscriber(id)
		return nil, err
	}
	if c.doneClosed() {
		c.removeEventSubscriber(id)
		return nil, io.EOF
	}
	go func() {
		<-ctx.Done()
		c.removeEventSubscriber(id)
	}()
	return ch, nil
}

func (c *Client) ensureEventsStarted(ctx context.Context) error {
	c.mu.Lock()
	if c.eventsStarted {
		c.mu.Unlock()
		return nil
	}
	if c.eventStartDone != nil {
		done := c.eventStartDone
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
		}
		c.mu.Lock()
		started := c.eventsStarted
		err := c.eventStartErr
		c.mu.Unlock()
		if started {
			return nil
		}
		if err != nil {
			return err
		}
		return io.EOF
	}
	done := make(chan struct{})
	c.eventStartDone = done
	c.eventStartErr = nil
	c.mu.Unlock()

	// 中文说明：同一个 protocol client 只向 daemon 打开一条宽事件流；
	// 多个 TUI view/storage watcher 在客户端内 fan-out，避免互相取消或重复刷新。
	err := c.doRequest(ctx, "events", EventsParams{}, nil)
	c.mu.Lock()
	if err == nil {
		c.eventsStarted = true
	}
	c.eventStartErr = err
	if c.eventStartDone == done {
		close(done)
		c.eventStartDone = nil
	}
	c.mu.Unlock()
	return err
}

func (c *Client) Input(ctx context.Context, channel uint16, data []byte) error {
	finish := perftrace.Measure("protocol.input.send")
	defer func() {
		finish(len(data))
	}()
	frame, err := wire.EncodeFrame(channel, wire.TypeInput, data)
	if err != nil {
		return err
	}
	return c.send(frame)
}

func (c *Client) InputWithOptions(ctx context.Context, params InputParams) error {
	finish := perftrace.Measure("protocol.request.input")
	defer func() {
		finish(len(params.Data))
	}()
	return c.doRequest(ctx, "input", params, nil)
}

func (c *Client) Resize(ctx context.Context, channel uint16, cols, rows uint16) error {
	frame, err := wire.EncodeFrame(channel, wire.TypeResize, wire.EncodeResizePayload(cols, rows))
	if err != nil {
		return err
	}
	return c.send(frame)
}

func (c *Client) ResizeRequest(ctx context.Context, terminalID string, cols, rows uint16) error {
	return c.doRequest(ctx, "resize", ResizeParams{
		TerminalID: terminalID,
		Cols:       cols,
		Rows:       rows,
	}, nil)
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

func (c *Client) HistoryReplay(ctx context.Context, channel uint16, beforeOffset, limit int) (*HistoryReplayPage, error) {
	stream, stop := c.Stream(channel)
	defer stop()

	frame, err := wire.EncodeFrame(channel, wire.TypeHistoryRequest, wire.EncodeHistoryRequestPayload(beforeOffset, limit))
	if err != nil {
		return nil, err
	}
	if err := c.send(frame); err != nil {
		return nil, err
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case msg, ok := <-stream:
			if !ok {
				return nil, io.EOF
			}
			switch msg.Type {
			case wire.TypeHistoryReplay:
				rows, hasMore, replay, err := wire.DecodeHistoryReplayPayload(msg.Payload)
				if err != nil {
					return nil, err
				}
				return &HistoryReplayPage{
					BeforeOffset: beforeOffset,
					Limit:        limit,
					Rows:         rows,
					HasMore:      hasMore,
					Replay:       string(replay),
				}, nil
			case wire.TypeError:
				msgErr, err := DecodeErrorPayload(msg.Payload)
				if err != nil {
					return nil, err
				}
				return nil, fmt.Errorf("protocol error %d: %s", msgErr.Error.Code, msgErr.Error.Message)
			case wire.TypeClosed:
				return nil, io.EOF
			}
		}
	}
}

func (c *Client) doRequest(ctx context.Context, method string, params any, out any) error {
	payload, err := c.doRequestPayload(ctx, method, params)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return DecodeMethodResult(method, payload, out)
}

func (c *Client) doRequestPayload(ctx context.Context, method string, params any) ([]byte, error) {
	finish := perftrace.Measure("protocol.request." + method)
	payload, err := EncodeMethodParams(method, params)
	if err != nil {
		finish(0)
		return nil, err
	}
	id := c.nextID.Add(1)
	reqPayload, err := EncodeRequestPayload(Request{
		ID:     id,
		Method: method,
		Params: payload,
	})
	if err != nil {
		finish(len(payload))
		return nil, err
	}
	frame, err := wire.EncodeFrame(0, wire.TypeRequest, reqPayload)
	if err != nil {
		finish(len(payload))
		return nil, err
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
		finish(len(frame))
		return nil, err
	}

	select {
	case <-ctx.Done():
		finish(len(frame))
		return nil, ctx.Err()
	case res := <-resCh:
		if res.err != nil {
			finish(len(frame))
			return nil, res.err
		}
		finish(len(res.payload))
		return res.payload, nil
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
		channel, typ, payload, err := wire.DecodeFrame(frame)
		if err != nil {
			c.failAll(err)
			return
		}
		if channel == 0 {
			switch typ {
			case wire.TypeHello:
				c.helloCh <- result{}
			case wire.TypeEvent:
				evt, err := DecodeEventPayload(payload)
				if err != nil {
					c.failAll(err)
					return
				}
				c.publishEvent(evt)
			case wire.TypeResponse:
				resp, err := DecodeResponsePayload(payload)
				if err != nil {
					c.failAll(err)
					return
				}
				c.mu.Lock()
				ch := c.waiters[resp.ID]
				c.mu.Unlock()
				if ch != nil {
					ch <- result{payload: resp.Result}
				}
			case wire.TypeResponseBinary:
				id, resultPayload, err := DecodeBinaryResponsePayload(payload)
				if err != nil {
					c.failAll(err)
					return
				}
				c.mu.Lock()
				ch := c.waiters[id]
				c.mu.Unlock()
				if ch != nil {
					ch <- result{payload: resultPayload}
				}
			case wire.TypeError:
				msg, err := DecodeErrorPayload(payload)
				if err != nil {
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

		streamFrame := StreamFrame{Type: typ, Payload: append([]byte(nil), payload...)}
		c.mu.Lock()
		stream := c.streams[channel]
		if stream == nil {
			if _, dropped := c.dropped[channel]; dropped {
				queue := c.reused[channel]
				if len(queue) < 256 {
					c.reused[channel] = append(queue, streamFrame)
				}
				c.mu.Unlock()
				continue
			}
			queue := c.pending[channel]
			if len(queue) < 256 {
				c.pending[channel] = append(queue, streamFrame)
			}
			c.mu.Unlock()
			continue
		}
		c.mu.Unlock()
		stream.send(streamFrame)
	}
}

func (c *Client) publishEvent(event Event) {
	c.mu.Lock()
	subscribers := make([]eventSubscription, 0, len(c.eventSubscribers))
	for _, sub := range c.eventSubscribers {
		if !eventMatchesParams(event, sub.params) {
			continue
		}
		subscribers = append(subscribers, sub)
	}
	c.mu.Unlock()
	for _, sub := range subscribers {
		select {
		case sub.ch <- event:
		default:
		}
	}
}

func (c *Client) doneClosed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

func (c *Client) removeEventSubscriber(id uint64) {
	c.mu.Lock()
	sub, ok := c.eventSubscribers[id]
	if ok {
		delete(c.eventSubscribers, id)
	}
	c.mu.Unlock()
	if ok {
		close(sub.ch)
	}
}

func eventMatchesParams(event Event, params EventsParams) bool {
	if params.TerminalID != "" && params.TerminalID != event.TerminalID {
		return false
	}
	if (event.Storage != nil || hasStorageEventParams(params)) && !storageEventMatchesParams(event.Storage, params) {
		return false
	}
	if (event.Workbench != nil || params.WorkbenchID != "") && !workbenchEventMatchesParams(event.Workbench, params) {
		return false
	}
	if len(params.Types) == 0 {
		return true
	}
	for _, typ := range params.Types {
		if typ == event.Type {
			return true
		}
	}
	return false
}

func hasStorageEventParams(params EventsParams) bool {
	return params.StorageAppID != "" || params.StorageScope != "" || params.StorageOwnerID != "" || params.StorageKeyPrefix != ""
}

func storageEventMatchesParams(storage *StorageChangedData, params EventsParams) bool {
	if storage == nil {
		return params.StorageAppID == "" && params.StorageScope == "" && params.StorageOwnerID == "" && params.StorageKeyPrefix == ""
	}
	if params.StorageAppID != "" && params.StorageAppID != storage.AppID {
		return false
	}
	if params.StorageScope != "" && params.StorageScope != storage.Scope {
		return false
	}
	if params.StorageOwnerID != "" && params.StorageOwnerID != storage.OwnerID {
		return false
	}
	if params.StorageKeyPrefix != "" && !strings.HasPrefix(storage.Key, params.StorageKeyPrefix) {
		return false
	}
	return true
}

func workbenchEventMatchesParams(workbench *WorkbenchChangedData, params EventsParams) bool {
	if workbench == nil {
		return params.WorkbenchID == ""
	}
	return params.WorkbenchID == "" || params.WorkbenchID == workbench.WorkspaceID || params.WorkbenchID == workbench.ResourceID
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
	for id, sub := range c.eventSubscribers {
		close(sub.ch)
		delete(c.eventSubscribers, id)
	}
}
