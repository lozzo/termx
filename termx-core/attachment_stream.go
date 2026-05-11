package termx

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-core/perftrace"
	"github.com/lozzow/termx/termx-core/protocol"
)

type attachmentStreamPump struct {
	ctx        context.Context
	cancel     context.CancelFunc
	terminalID string
	channel    uint16
	remote     string
	src        <-chan StreamMessage
	sendFrame  func(uint16, uint8, []byte) error
	logger     *slog.Logger

	mu          sync.Mutex
	cond        *sync.Cond
	queue       []StreamMessage
	queueBytes  int
	inputClosed bool
	suffixStart int
	suffixBase  *streamScreenState
	sentState   *streamScreenState
	queueState  *streamScreenState
	stats       attachmentPumpStats
}

func newAttachmentStreamPump(
	ctx context.Context,
	cancel context.CancelFunc,
	terminalID string,
	channel uint16,
	remote string,
	src <-chan StreamMessage,
	sendFrame func(uint16, uint8, []byte) error,
	logger *slog.Logger,
) *attachmentStreamPump {
	pump := &attachmentStreamPump{
		ctx:         ctx,
		cancel:      cancel,
		terminalID:  terminalID,
		channel:     channel,
		remote:      remote,
		src:         src,
		sendFrame:   sendFrame,
		logger:      logger,
		suffixStart: -1,
	}
	pump.stats.lastLog = time.Now()
	pump.cond = sync.NewCond(&pump.mu)
	return pump
}

type attachmentPumpStats struct {
	lastLog         time.Time
	enqueuedFrames  int
	enqueuedBytes   int
	sentFrames      int
	sentBytes       int
	outputFrames    int
	screenFrames    int
	syncLostFrames  int
	closedFrames    int
	coalescedFrames int
	maxPayloadBytes int
	maxQueueFrames  int
	maxQueueBytes   int
}

func (p *attachmentStreamPump) run() error {
	if p.logger != nil {
		p.logger.Info("termx attachment stream pump started", "terminal_id", p.terminalID, "remote", p.remote, "channel", p.channel)
	}
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
	p.flushStatsLocked("closed")
	p.mu.Unlock()
	<-readerDone
	return err
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
		if err := p.sendMessageFrame(msg); err != nil {
			return err
		}
	}
}

func (p *attachmentStreamPump) sendMessageFrame(msg StreamMessage) error {
	if msg.Type == StreamOutput {
		for len(msg.Output) > 0 {
			n := minInt(len(msg.Output), maxLiveOutputFrameBytes)
			if err := p.sendFrame(p.channel, protocol.TypeOutput, msg.Output[:n]); err != nil {
				return err
			}
			p.recordSentFrame(protocol.TypeOutput, n)
			msg.Output = msg.Output[n:]
		}
		return nil
	}
	typ, payload, ok := streamMessageFramePayload(msg)
	if !ok {
		return nil
	}
	if len(payload) > protocol.MaxFrameSize {
		syncLostPayload := protocol.EncodeSyncLostPayload(uint64(len(payload)))
		if p.logger != nil {
			p.logger.Warn(
				"termx attachment stream payload exceeded frame cap",
				"terminal_id", p.terminalID,
				"remote", p.remote,
				"channel", p.channel,
				"type", streamMessageTypeName(msg.Type),
				"payload_bytes", len(payload),
				"max_frame_bytes", protocol.MaxFrameSize,
			)
		}
		if err := p.sendFrame(p.channel, protocol.TypeSyncLost, syncLostPayload); err != nil {
			return err
		}
		p.recordSentFrame(protocol.TypeSyncLost, len(syncLostPayload))
		return nil
	}
	if err := p.sendFrame(p.channel, typ, payload); err != nil {
		return err
	}
	p.recordSentFrame(typ, len(payload))
	return nil
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
	p.recordEnqueuedLocked(msg)
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
	p.stats.coalescedFrames += suffixLen - 1
	if p.logger != nil {
		p.logger.Warn(
			"termx attachment stream backlog collapsed",
			"terminal_id", p.terminalID,
			"remote", p.remote,
			"channel", p.channel,
			"suffix_frames", suffixLen,
			"before_bytes", beforeBytes,
			"after_bytes", afterBytes,
			"alternate_screen", p.queueState.snapshot.Modes.AlternateScreen,
		)
	}
	p.suffixStart = len(p.queue) - 1
}

func (p *attachmentStreamPump) recordEnqueuedLocked(msg StreamMessage) {
	if p == nil {
		return
	}
	payloadBytes := streamMessagePayloadBytes(msg)
	p.stats.enqueuedFrames++
	p.stats.enqueuedBytes += payloadBytes
	if msg.Type == StreamOutput {
		p.stats.outputFrames++
	}
	if msg.Type == StreamScreenUpdate {
		p.stats.screenFrames++
	}
	if msg.Type == StreamSyncLost {
		p.stats.syncLostFrames++
	}
	if msg.Type == StreamClosed {
		p.stats.closedFrames++
	}
	if payloadBytes > p.stats.maxPayloadBytes {
		p.stats.maxPayloadBytes = payloadBytes
	}
	if len(p.queue) > p.stats.maxQueueFrames {
		p.stats.maxQueueFrames = len(p.queue)
	}
	if p.queueBytes > p.stats.maxQueueBytes {
		p.stats.maxQueueBytes = p.queueBytes
	}
	if payloadBytes >= coreDiagnosticsLargePayloadBytes && p.logger != nil {
		p.logger.Warn(
			"termx attachment stream large enqueue",
			"terminal_id", p.terminalID,
			"remote", p.remote,
			"channel", p.channel,
			"type", streamMessageTypeName(msg.Type),
			"payload_bytes", payloadBytes,
			"queue_frames", len(p.queue),
			"queue_bytes", p.queueBytes,
		)
	}
	if time.Since(p.stats.lastLog) >= coreDiagnosticsInterval {
		p.flushStatsLocked("interval")
	}
}

func (p *attachmentStreamPump) recordSentFrame(typ uint8, payloadBytes int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats.sentFrames++
	p.stats.sentBytes += payloadBytes
	if payloadBytes > p.stats.maxPayloadBytes {
		p.stats.maxPayloadBytes = payloadBytes
	}
	if payloadBytes >= coreDiagnosticsLargePayloadBytes && p.logger != nil {
		p.logger.Warn(
			"termx attachment stream large send",
			"terminal_id", p.terminalID,
			"remote", p.remote,
			"channel", p.channel,
			"type", protocolFrameTypeName(typ),
			"payload_bytes", payloadBytes,
			"queue_frames", len(p.queue),
			"queue_bytes", p.queueBytes,
		)
	}
	if time.Since(p.stats.lastLog) >= coreDiagnosticsInterval {
		p.flushStatsLocked("interval")
	}
}

func (p *attachmentStreamPump) flushStatsLocked(reason string) {
	if p == nil {
		return
	}
	if p.logger == nil || (p.stats.enqueuedFrames == 0 && p.stats.sentFrames == 0 && p.stats.coalescedFrames == 0) {
		p.stats.lastLog = time.Now()
		return
	}
	p.logger.Info(
		"termx attachment stream stats",
		"terminal_id", p.terminalID,
		"remote", p.remote,
		"channel", p.channel,
		"reason", reason,
		"enqueued_frames", p.stats.enqueuedFrames,
		"enqueued_bytes", p.stats.enqueuedBytes,
		"sent_frames", p.stats.sentFrames,
		"sent_bytes", p.stats.sentBytes,
		"output_frames", p.stats.outputFrames,
		"screen_update_frames", p.stats.screenFrames,
		"sync_lost_frames", p.stats.syncLostFrames,
		"closed_frames", p.stats.closedFrames,
		"coalesced_frames", p.stats.coalescedFrames,
		"max_payload_bytes", p.stats.maxPayloadBytes,
		"max_queue_frames", p.stats.maxQueueFrames,
		"max_queue_bytes", p.stats.maxQueueBytes,
		"current_queue_frames", len(p.queue),
		"current_queue_bytes", p.queueBytes,
	)
	p.stats.lastLog = time.Now()
	p.stats.enqueuedFrames = 0
	p.stats.enqueuedBytes = 0
	p.stats.sentFrames = 0
	p.stats.sentBytes = 0
	p.stats.outputFrames = 0
	p.stats.screenFrames = 0
	p.stats.syncLostFrames = 0
	p.stats.closedFrames = 0
	p.stats.coalescedFrames = 0
	p.stats.maxPayloadBytes = 0
	p.stats.maxQueueFrames = len(p.queue)
	p.stats.maxQueueBytes = p.queueBytes
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

func streamMessagePayloadBytes(msg StreamMessage) int {
	_, payload, ok := streamMessageFramePayload(msg)
	if !ok {
		return 0
	}
	return len(payload)
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
