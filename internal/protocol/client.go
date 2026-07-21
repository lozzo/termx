package protocol

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/muxvia/muxvia/proto/apipb"
	"github.com/muxvia/muxvia/proto/wire"

	"github.com/muxvia/muxvia/shared/perftrace"
	"github.com/muxvia/muxvia/shared/transport"
	"google.golang.org/protobuf/proto"
)

type Client struct {
	transport transport.Transport
	nextID    atomic.Uint64

	doneErrMu sync.Mutex
	doneErr   error
	closeOnce sync.Once
	closeErr  error

	mu                          sync.Mutex
	sendMu                      sync.Mutex
	waiters                     map[uint64]chan result
	streams                     map[uint16]*clientStream
	pending                     map[uint16][]StreamFrame
	reused                      map[uint16][]StreamFrame
	dropped                     map[uint16]struct{}
	applicationEventSubscribers map[uint64]chan *apipb.EventEnvelope
	pendingApplicationEvents    []*apipb.EventEnvelope
	applicationAttachments      map[uint16]*apipb.ResourceHandle
	nextEventSubID              uint64

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
		transport:                   t,
		waiters:                     make(map[uint64]chan result),
		streams:                     make(map[uint16]*clientStream),
		pending:                     make(map[uint16][]StreamFrame),
		reused:                      make(map[uint16][]StreamFrame),
		dropped:                     make(map[uint16]struct{}),
		applicationEventSubscribers: make(map[uint64]chan *apipb.EventEnvelope),
		applicationAttachments:      make(map[uint16]*apipb.ResourceHandle),
		helloCh:                     make(chan result, 1),
		done:                        make(chan struct{}),
	}
	go c.readLoop()
	return c
}

// ApplicationEvents 注册 generated Proto application event 消费者。
// 事件 frame 只包含 EventEnvelope；filter 与 subscription lifecycle 由 daemon resource 管理。
func (c *Client) ApplicationEvents(ctx context.Context) (<-chan *apipb.EventEnvelope, error) {
	if c == nil || c.doneClosed() {
		return nil, io.EOF
	}
	raw := make(chan *apipb.EventEnvelope, 64)
	out := make(chan *apipb.EventEnvelope, 64)
	c.mu.Lock()
	c.nextEventSubID++
	id := c.nextEventSubID
	c.applicationEventSubscribers[id] = raw
	pending := c.pendingApplicationEvents
	c.pendingApplicationEvents = nil
	c.mu.Unlock()
	for _, event := range pending {
		raw <- event
	}
	go func() {
		defer close(out)
		defer func() {
			c.mu.Lock()
			delete(c.applicationEventSubscribers, id)
			c.mu.Unlock()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.done:
				return
			case event := <-raw:
				select {
				case out <- event:
				case <-ctx.Done():
					return
				case <-c.done:
					return
				}
			}
		}
	}()
	return out, nil
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		select {
		case <-c.done:
		default:
			if drainer, ok := c.transport.(interface{ Drain(context.Context) error }); ok {
				if payload, err := EncodeSessionClosePayload(); err == nil {
					if frame, err := wire.EncodeFrame(0, wire.TypeSessionClose, payload); err == nil && c.send(frame) == nil {
						drainCtx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
						_ = drainer.Drain(drainCtx)
						cancel()
						select {
						case <-c.done:
						case <-time.After(250 * time.Millisecond):
						}
					}
				}
			}
		}
		c.closeErr = c.transport.Close()
	})
	<-c.done
	return c.closeErr
}

// Done 返回 protocol client read loop 的关闭信号。
// 调用方只能把它作为连接生命周期事件源，不能从这里推断 terminal lifecycle 或 history truth。
func (c *Client) Done() <-chan struct{} {
	if c == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return c.done
}

// Err 返回 read loop 结束时记录的 transport/protocol 错误。
// 该错误用于 client/TUI 展示 endpoint 断线原因；如果连接尚未关闭或正常关闭则返回 nil。
func (c *Client) Err() error {
	if c == nil {
		return io.EOF
	}
	select {
	case <-c.done:
	default:
		return nil
	}
	c.doneErrMu.Lock()
	defer c.doneErrMu.Unlock()
	return c.doneErr
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

// ExecuteApplication 通过唯一 api.execute framing 发送公共 Proto API command。
// protocol client 只负责 correlation 与 payload transport，不解释 terminal application 字段。
func (c *Client) ExecuteApplication(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	payload, err := c.doApplicationRequest(ctx, command, false)
	if err != nil {
		return nil, err
	}
	result, err := DecodeApplicationResult(payload)
	if err != nil {
		return nil, err
	}
	c.updateApplicationAttachmentBinding(command, result)
	return result, nil
}

// ExecuteApplicationTerminal 使用通用 correlation 选项，在调用 context 取消后继续等待一个有界 terminal response。
// 是否选择该能力由上层 resource owner 决定；protocol 不检查 application oneof，也不负责业务清理。
func (c *Client) ExecuteApplicationTerminal(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	payload, err := c.doApplicationRequest(ctx, command, true)
	if err != nil {
		return nil, err
	}
	result, err := DecodeApplicationResult(payload)
	if err != nil {
		return nil, err
	}
	c.updateApplicationAttachmentBinding(command, result)
	return result, nil
}

// ApplicationAttachmentChannel 返回当前 protocol connection 为 attachment resource 分配的 stream channel。
// opaque token 的内部格式和 channel registry 只属于 framing binding，不得暴露为公共 API schema。
func (c *Client) ApplicationAttachmentChannel(resource *apipb.ResourceHandle) (uint16, bool) {
	if resource.GetKind() != apipb.ResourceKind_RESOURCE_KIND_TERMINAL_ATTACHMENT {
		return 0, false
	}
	return c.ApplicationResourceChannel(resource)
}

// ApplicationResourceChannel 返回当前 connection 为 stream resource 绑定的内部 channel。
// channel 只供 framing adapter 使用，不进入公共 Proto result。
func (c *Client) ApplicationResourceChannel(resource *apipb.ResourceHandle) (uint16, bool) {
	if resource == nil || len(resource.GetOpaqueToken()) < 2 {
		return 0, false
	}
	channel := binary.BigEndian.Uint16(resource.GetOpaqueToken()[:2])
	if channel == 0 {
		return 0, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	bound := c.applicationAttachments[channel]
	return channel, bound != nil && string(bound.GetOpaqueToken()) == string(resource.GetOpaqueToken())
}

// ApplicationAttachment 返回 stream channel 当前绑定的 Proto resource handle 副本。
func (c *Client) ApplicationAttachment(channel uint16) (*apipb.ResourceHandle, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	resource := c.applicationAttachments[channel]
	if resource == nil {
		return nil, false
	}
	return proto.Clone(resource).(*apipb.ResourceHandle), true
}

func (c *Client) updateApplicationAttachmentBinding(command *apipb.CommandEnvelope, result *apipb.ResultEnvelope) {
	if command == nil || result == nil || result.GetError() != nil {
		return
	}
	boundResource := result.GetTerminalAttach().GetAttachment().GetResource()
	if transfer := result.GetFileTransferOpen().GetTransfer(); transfer != nil {
		boundResource = transfer.GetResource()
	}
	if boundResource != nil && len(boundResource.GetOpaqueToken()) >= 2 {
		channel := binary.BigEndian.Uint16(boundResource.GetOpaqueToken()[:2])
		if channel != 0 {
			c.bindApplicationAttachment(channel, boundResource)
		}
		return
	}
	var released *apipb.ResourceHandle
	switch value := command.GetCommand().(type) {
	case *apipb.CommandEnvelope_TerminalDetach:
		released = value.TerminalDetach.GetAttachment()
	case *apipb.CommandEnvelope_ReleaseResource:
		released = value.ReleaseResource.GetResource()
	case *apipb.CommandEnvelope_FileTransferCancel:
		released = value.FileTransferCancel.GetTransfer()
	}
	if channel, ok := c.ApplicationResourceChannel(released); ok {
		c.mu.Lock()
		delete(c.applicationAttachments, channel)
		c.mu.Unlock()
	}
}

func (c *Client) bindApplicationAttachment(channel uint16, resource *apipb.ResourceHandle) {
	c.mu.Lock()
	stream := c.streams[channel]
	if stream == nil {
		stream = newClientStream()
		c.streams[channel] = stream
	}
	c.applicationAttachments[channel] = proto.Clone(resource).(*apipb.ResourceHandle)
	delete(c.dropped, channel)
	pending := c.pending[channel]
	delete(c.pending, channel)
	reused := c.reused[channel]
	delete(c.reused, channel)
	c.mu.Unlock()
	for _, frame := range pending {
		stream.send(frame)
	}
	for _, frame := range reused {
		stream.send(frame)
	}
}

// SendFileFrame 在已由 daemon 分配的 transfer channel 上发送流控 frame。
// typ 只允许 file data/ack/finish，避免调用方借该入口写 terminal channel。
func (c *Client) SendFileFrame(channel uint16, typ uint8, payload []byte) error {
	switch typ {
	case wire.TypeFileData, wire.TypeFileAck, wire.TypeFileFinish:
	default:
		return fmt.Errorf("unsupported client file frame type %d", typ)
	}
	frame, err := wire.EncodeFrame(channel, typ, payload)
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

func (c *Client) doApplicationRequest(ctx context.Context, command *apipb.CommandEnvelope, waitTerminalOnCancel bool) ([]byte, error) {
	const method = "api.execute"
	finish := perftrace.Measure("protocol.request." + method)
	payload, err := EncodeApplicationCommand(command)
	if err != nil {
		finish(0)
		return nil, err
	}
	id := c.nextID.Add(1)
	requestPayload, err := EncodeRequestPayload(Request{ID: id, Method: method, Params: payload})
	if err != nil {
		finish(len(payload))
		return nil, err
	}
	frame, err := wire.EncodeFrame(0, wire.TypeRequest, requestPayload)
	if err != nil {
		finish(len(payload))
		return nil, err
	}

	response := make(chan result, 1)
	c.mu.Lock()
	c.waiters[id] = response
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
		if waitTerminalOnCancel {
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			select {
			case result := <-response:
				if result.err != nil {
					finish(len(frame))
					return nil, result.err
				}
				finish(len(result.payload))
				return result.payload, nil
			case <-c.done:
				finish(len(frame))
				return nil, c.Err()
			case <-timer.C:
				_ = c.Close()
				finish(len(frame))
				return nil, fmt.Errorf("cancelled file resource open did not reach a terminal response")
			}
		}
		finish(len(frame))
		return nil, ctx.Err()
	case result := <-response:
		if result.err != nil {
			finish(len(frame))
			return nil, result.err
		}
		finish(len(result.payload))
		return result.payload, nil
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
			c.setDoneErr(err)
			c.failAll(err)
			return
		}
		channel, typ, payload, err := wire.DecodeFrame(frame)
		if err != nil {
			c.setDoneErr(err)
			c.failAll(err)
			return
		}
		if channel == 0 {
			switch typ {
			case wire.TypeHello:
				c.helloCh <- result{}
			case wire.TypeEvent:
				var applicationEvent apipb.EventEnvelope
				if err := proto.Unmarshal(payload, &applicationEvent); err != nil {
					c.setDoneErr(err)
					c.failAll(err)
					return
				}
				if applicationEvent.GetEvent() == nil {
					err := fmt.Errorf("application event payload has no event")
					c.setDoneErr(err)
					c.failAll(err)
					return
				}
				c.publishApplicationEvent(&applicationEvent)
			case wire.TypeResponse:
				resp, err := DecodeResponsePayload(payload)
				if err != nil {
					c.setDoneErr(err)
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
					c.setDoneErr(err)
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
					c.setDoneErr(err)
					c.failAll(err)
					return
				}
				c.mu.Lock()
				ch := c.waiters[msg.ID]
				c.mu.Unlock()
				if ch != nil {
					ch <- result{err: &RequestError{Code: msg.Error.Code, Message: msg.Error.Message}}
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

func (c *Client) setDoneErr(err error) {
	if err == nil {
		return
	}
	c.doneErrMu.Lock()
	defer c.doneErrMu.Unlock()
	if c.doneErr == nil {
		c.doneErr = err
	}
}

func (c *Client) publishApplicationEvent(event *apipb.EventEnvelope) {
	if event == nil {
		return
	}
	snapshot := proto.Clone(event).(*apipb.EventEnvelope)
	c.mu.Lock()
	if len(c.applicationEventSubscribers) == 0 {
		if len(c.pendingApplicationEvents) < 64 {
			c.pendingApplicationEvents = append(c.pendingApplicationEvents, snapshot)
		}
		c.mu.Unlock()
		return
	}
	subscribers := make([]chan *apipb.EventEnvelope, 0, len(c.applicationEventSubscribers))
	for _, subscriber := range c.applicationEventSubscribers {
		subscribers = append(subscribers, subscriber)
	}
	c.mu.Unlock()
	for _, subscriber := range subscribers {
		select {
		case subscriber <- proto.Clone(snapshot).(*apipb.EventEnvelope):
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

func (c *Client) failAll(err error) {
	if err == nil {
		err = io.EOF
	}
	c.mu.Lock()
	waiters := make([]chan result, 0, len(c.waiters))
	for id, ch := range c.waiters {
		waiters = append(waiters, ch)
		delete(c.waiters, id)
	}
	streams := make([]*clientStream, 0, len(c.streams))
	for id, stream := range c.streams {
		streams = append(streams, stream)
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
	for id := range c.applicationEventSubscribers {
		delete(c.applicationEventSubscribers, id)
	}
	c.pendingApplicationEvents = nil
	c.mu.Unlock()

	// waiter 可能已经收到 terminal result 并正等待 c.mu 删除自身；通知与 stream close 必须在 registry 锁外执行。
	for _, ch := range waiters {
		select {
		case ch <- result{err: err}:
		default:
		}
	}
	select {
	case c.helloCh <- result{err: err}:
	default:
	}
	for _, stream := range streams {
		stream.close()
	}
}
