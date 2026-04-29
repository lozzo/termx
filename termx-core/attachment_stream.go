package termx

import (
	"context"
	"sync"

	"github.com/lozzow/termx/termx-core/perftrace"
	"github.com/lozzow/termx/termx-core/protocol"
)

type attachmentStreamPump struct {
	ctx        context.Context
	cancel     context.CancelFunc
	terminalID string
	channel    uint16
	src        <-chan StreamMessage
	sendFrame  func(uint16, uint8, []byte) error

	mu          sync.Mutex
	cond        *sync.Cond
	queue       []StreamMessage
	queueBytes  int
	inputClosed bool
	suffixStart int
	suffixBase  *streamScreenState
	sentState   *streamScreenState
	queueState  *streamScreenState
}

func newAttachmentStreamPump(
	ctx context.Context,
	cancel context.CancelFunc,
	terminalID string,
	channel uint16,
	src <-chan StreamMessage,
	sendFrame func(uint16, uint8, []byte) error,
) *attachmentStreamPump {
	pump := &attachmentStreamPump{
		ctx:         ctx,
		cancel:      cancel,
		terminalID:  terminalID,
		channel:     channel,
		src:         src,
		sendFrame:   sendFrame,
		suffixStart: -1,
	}
	pump.cond = sync.NewCond(&pump.mu)
	return pump
}

func (p *attachmentStreamPump) run() {
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		p.readLoop()
	}()
	err := p.sendLoop()
	if err != nil && p.cancel != nil {
		p.cancel()
	}
	p.mu.Lock()
	p.inputClosed = true
	p.cond.Broadcast()
	p.mu.Unlock()
	<-readerDone
}

func (p *attachmentStreamPump) readLoop() {
	for {
		select {
		case <-p.ctx.Done():
			p.closeInput()
			return
		case msg, ok := <-p.src:
			if !ok {
				p.closeInput()
				return
			}
			p.enqueue(msg)
		}
	}
}

func (p *attachmentStreamPump) sendLoop() error {
	for {
		msg, ok := p.next()
		if !ok {
			return nil
		}
		typ, payload, ok := streamMessageFramePayload(msg)
		if !ok {
			continue
		}
		if err := p.sendFrame(p.channel, typ, payload); err != nil {
			return err
		}
	}
}

func (p *attachmentStreamPump) closeInput() {
	p.mu.Lock()
	p.inputClosed = true
	p.cond.Broadcast()
	p.mu.Unlock()
}

func (p *attachmentStreamPump) enqueue(msg StreamMessage) {
	p.mu.Lock()
	defer p.mu.Unlock()

	nextState := p.applyMessageToQueueStateLocked(msg)
	msgCountBefore := len(p.queue)
	if msg.Type == StreamScreenUpdate {
		if p.suffixStart < 0 || p.suffixStart > len(p.queue) {
			p.suffixStart = len(p.queue)
			p.suffixBase = cloneStreamScreenState(p.queueState)
		}
		p.queue = append(p.queue, msg)
		p.queueState = nextState
		if p.shouldCollapseSuffixLocked() {
			p.collapseSuffixLocked()
		}
	} else {
		p.queue = append(p.queue, msg)
		p.queueState = nextState
		p.suffixStart = -1
		p.suffixBase = nil
	}
	p.queueBytes = estimateStreamQueueWireBytes(p.queue)
	if len(p.queue) > msgCountBefore {
		perftrace.Count("transport.stream.backlog.enqueued_frames", len(p.queue)-msgCountBefore)
	}
	p.cond.Signal()
}

func (p *attachmentStreamPump) applyMessageToQueueStateLocked(msg StreamMessage) *streamScreenState {
	base := p.queueState
	switch msg.Type {
	case StreamScreenUpdate:
		update, err := protocol.DecodeScreenUpdatePayload(msg.Payload)
		if err != nil {
			return nil
		}
		return applyStreamScreenUpdateState(base, p.terminalID, update)
	case StreamResize:
		return resizeStreamScreenState(base, p.terminalID, msg.Cols, msg.Rows)
	case StreamOutput, StreamSyncLost:
		return nil
	default:
		return base
	}
}

func (p *attachmentStreamPump) shouldCollapseSuffixLocked() bool {
	if p.suffixStart < 0 || p.suffixStart >= len(p.queue) {
		return false
	}
	suffixLen := len(p.queue) - p.suffixStart
	if suffixLen < 2 || p.queueState == nil || p.queueState.snapshot == nil {
		return false
	}
	collapseFrames := backlogNormalScreenCollapseFrames
	collapseBytes := backlogNormalScreenCollapseBytes
	if p.queueState.snapshot.Modes.AlternateScreen {
		collapseFrames = backlogAlternateScreenCollapseFrames
		collapseBytes = backlogAlternateScreenCollapseBytes
	}
	return len(p.queue) >= collapseFrames || p.queueBytes >= collapseBytes
}

func (p *attachmentStreamPump) collapseSuffixLocked() {
	if p.suffixStart < 0 || p.suffixStart >= len(p.queue) || p.queueState == nil || p.queueState.snapshot == nil {
		return
	}
	suffixLen := len(p.queue) - p.suffixStart
	if suffixLen < 2 {
		return
	}
	payload, ok := encodeMergedScreenStatePayload(p.suffixBase, p.queueState, false)
	if !ok {
		return
	}
	merged := StreamMessage{Type: StreamScreenUpdate, Payload: payload}
	beforeBytes := estimateStreamQueueWireBytes(p.queue[p.suffixStart:])
	p.queue = append(append([]StreamMessage(nil), p.queue[:p.suffixStart]...), merged)
	afterBytes := estimateStreamQueueWireBytes(p.queue[p.suffixStart:])
	perftrace.Count("transport.stream.backlog.coalesced_frames", suffixLen-1)
	if p.queueState.snapshot.Modes.AlternateScreen {
		perftrace.Count("transport.stream.backlog.collapse.alternate", suffixLen)
	} else {
		perftrace.Count("transport.stream.backlog.collapse.normal", suffixLen)
	}
	if beforeBytes > afterBytes {
		perftrace.Count("transport.stream.backlog.saved_bytes", beforeBytes-afterBytes)
	}
	p.suffixStart = len(p.queue) - 1
}

func (p *attachmentStreamPump) next() (StreamMessage, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for {
		if len(p.queue) > 0 {
			msg := p.queue[0]
			p.queue = append([]StreamMessage(nil), p.queue[1:]...)
			p.advanceSentStateLocked(msg)
			if p.suffixStart >= 0 {
				switch {
				case p.suffixStart > 0:
					p.suffixStart--
				case p.suffixStart == 0:
					if len(p.queue) > 0 && p.queue[0].Type == StreamScreenUpdate {
						p.suffixStart = 0
						p.suffixBase = cloneStreamScreenState(p.sentState)
					} else {
						p.suffixStart = -1
						p.suffixBase = nil
					}
				}
			}
			if len(p.queue) == 0 {
				p.queueState = cloneStreamScreenState(p.sentState)
			}
			p.queueBytes = estimateStreamQueueWireBytes(p.queue)
			return msg, true
		}
		if p.inputClosed || p.ctx.Err() != nil {
			return StreamMessage{}, false
		}
		p.cond.Wait()
	}
}

func (p *attachmentStreamPump) advanceSentStateLocked(msg StreamMessage) {
	switch msg.Type {
	case StreamScreenUpdate:
		update, err := protocol.DecodeScreenUpdatePayload(msg.Payload)
		if err != nil {
			p.sentState = nil
			return
		}
		p.sentState = applyStreamScreenUpdateState(p.sentState, p.terminalID, update)
	case StreamResize:
		p.sentState = resizeStreamScreenState(p.sentState, p.terminalID, msg.Cols, msg.Rows)
	case StreamOutput, StreamSyncLost:
		p.sentState = nil
	}
}

func estimateStreamQueueWireBytes(queue []StreamMessage) int {
	total := 0
	for _, msg := range queue {
		total += estimateStreamMessageWireBytes(msg)
	}
	return total
}

func estimateStreamMessageWireBytes(msg StreamMessage) int {
	_, payload, ok := streamMessageFramePayload(msg)
	if !ok {
		return 0
	}
	return 7 + len(payload)
}

func streamMessageFramePayload(msg StreamMessage) (uint8, []byte, bool) {
	switch msg.Type {
	case StreamOutput:
		return protocol.TypeOutput, msg.Output, true
	case StreamSyncLost:
		return protocol.TypeSyncLost, protocol.EncodeSyncLostPayload(msg.DroppedBytes), true
	case StreamResize:
		return protocol.TypeResize, protocol.EncodeResizePayload(msg.Cols, msg.Rows), true
	case StreamBootstrapDone:
		return protocol.TypeBootstrapDone, nil, true
	case StreamScreenUpdate:
		return protocol.TypeScreenUpdate, msg.Payload, true
	case StreamClosed:
		code := 0
		if msg.ExitCode != nil {
			code = *msg.ExitCode
		}
		return protocol.TypeClosed, protocol.EncodeClosedPayload(code), true
	default:
		return 0, nil, false
	}
}
