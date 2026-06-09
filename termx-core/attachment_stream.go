package termx

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/lozzow/termx/termx-proto/wire"
	"github.com/lozzow/termx/termx-shared/perftrace"
)

type attachmentStreamPump struct {
	ctx        context.Context
	cancel     context.CancelFunc
	terminalID string
	channel    uint16
	remote     string
	src        <-chan StreamMessage
	latest     func() StreamMessage
	revision   func() uint64
	sendFrame  func(uint16, uint8, []byte) error
	logger     *slog.Logger

	mu                      sync.Mutex
	cond                    *sync.Cond
	queue                   []StreamMessage
	queueBytes              int
	inputClosed             bool
	readyMode               bool
	screenCredit            bool
	screenInFlight          bool
	screenDirty             bool
	forceLatestScreen       bool
	screenSequence          uint64
	lastAckedScreenSequence uint64
	latestAckedRevision     uint64
	screenStateRevision     uint64
	screenSequenceRevisions map[uint64]uint64
	stats                   attachmentPumpStats
}

const attachmentStreamAckRevisionWindow = 4096

func newAttachmentStreamPump(
	ctx context.Context,
	cancel context.CancelFunc,
	terminalID string,
	channel uint16,
	remote string,
	src <-chan StreamMessage,
	latest func() StreamMessage,
	revision func() uint64,
	sendFrame func(uint16, uint8, []byte) error,
	logger *slog.Logger,
) *attachmentStreamPump {
	pump := &attachmentStreamPump{
		ctx:                     ctx,
		cancel:                  cancel,
		terminalID:              terminalID,
		channel:                 channel,
		remote:                  remote,
		src:                     src,
		latest:                  latest,
		revision:                revision,
		sendFrame:               sendFrame,
		logger:                  logger,
		screenSequenceRevisions: make(map[uint64]uint64),
	}
	pump.stats.lastLog = time.Now()
	pump.cond = sync.NewCond(&pump.mu)
	return pump
}

func (p *attachmentStreamPump) screenReady(screenSequence uint64) {
	if p == nil {
		return
	}
	p.mu.Lock()
	if screenSequence == 0 && p.readyMode && p.screenInFlight {
		p.cond.Signal()
		p.mu.Unlock()
		return
	}
	if screenSequence > p.screenSequence {
		p.cond.Signal()
		p.mu.Unlock()
		return
	}
	if screenSequence > 0 && screenSequence <= p.lastAckedScreenSequence {
		p.readyMode = true
		p.cond.Signal()
		p.mu.Unlock()
		return
	}
	p.readyMode = true
	p.screenCredit = true
	p.screenInFlight = false
	if screenSequence > p.lastAckedScreenSequence {
		p.lastAckedScreenSequence = screenSequence
	}
	if ackRevision, ok := p.screenSequenceRevisions[screenSequence]; ok {
		if ackRevision > p.latestAckedRevision {
			p.latestAckedRevision = ackRevision
		}
		p.pruneScreenSequenceRevisionsLocked(screenSequence)
	}
	ackedRevision := p.latestAckedRevision
	wasDirty := p.screenDirty
	needsFreshScreen := p.screenDirty
	p.screenDirty = false
	queueFrames := len(p.queue)
	queueBytes := p.queueBytes
	readyMode := p.readyMode
	screenCredit := p.screenCredit
	screenInFlight := p.screenInFlight
	currentScreenSequence := p.screenSequence
	lastAckedScreenSequence := p.lastAckedScreenSequence
	p.cond.Signal()
	p.mu.Unlock()

	currentRevision := uint64(0)
	if screenSequence > 0 {
		currentRevision = p.currentRevision()
	}
	if !needsFreshScreen && screenSequence > 0 && currentRevision > ackedRevision {
		needsFreshScreen = true
	}
	if coreScreenUpdateDebugEnabled() && p.logger != nil {
		p.logger.Debug(
			"termx attachment stream ready",
			"terminal_id", p.terminalID,
			"remote", p.remote,
			"channel", p.channel,
			"acked_screen_sequence", screenSequence,
			"pump_screen_sequence", currentScreenSequence,
			"last_acked_screen_sequence", lastAckedScreenSequence,
			"acked_revision", ackedRevision,
			"current_revision", currentRevision,
			"needs_fresh_screen", needsFreshScreen,
			"was_dirty", wasDirty,
			"ready_mode", readyMode,
			"screen_credit", screenCredit,
			"screen_in_flight", screenInFlight,
			"queue_frames", queueFrames,
			"queue_bytes", queueBytes,
		)
	}
	if !needsFreshScreen {
		return
	}
	p.mu.Lock()
	if needsFreshScreen {
		p.forceLatestScreen = true
		if removed := p.dropQueuedScreenUpdatesLocked(); removed > 0 {
			p.stats.coalescedFrames += removed
			perftrace.Count("transport.stream.ready.replaced_queued_screen_frames", removed)
		}
	}
	if !p.hasQueuedScreenUpdateLocked() {
		msg := StreamMessage{Type: StreamScreenUpdate}
		p.appendQueueMessageLocked(msg)
		p.debugLogScreenQueueLocked("ready_enqueue_latest_placeholder", msg)
	}
	p.cond.Signal()
	p.mu.Unlock()
}

type attachmentPumpStats struct {
	lastLog         time.Time
	enqueuedFrames  int
	enqueuedBytes   int
	sentFrames      int
	sentBytes       int
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
		sentScreenFrame, err := p.sendMessageFrame(msg)
		if err != nil {
			return err
		}
		if p.readyMode && msg.Type == StreamScreenUpdate && !sentScreenFrame {
			p.releaseUnsentScreenFrame()
		}
	}
}

func (p *attachmentStreamPump) sendMessageFrame(msg StreamMessage) (bool, error) {
	resolvedLatest := false
	if msg.Type == StreamScreenUpdate && len(msg.Payload) > 0 && p.shouldSendLatestScreenFrame(msg) {
		msg = StreamMessage{Type: StreamScreenUpdate, Revision: msg.Revision}
	}
	if msg.Type == StreamScreenUpdate && len(msg.Payload) == 0 {
		switch {
		case p.latest != nil:
			msg = p.latest()
			resolvedLatest = true
		case msg.Latest != nil:
			msg = msg.Latest()
			resolvedLatest = true
		default:
			return false, nil
		}
		if msg.Type != StreamScreenUpdate || len(msg.Payload) == 0 {
			return false, nil
		}
	}
	typ, payload, ok := streamMessageFramePayload(msg)
	if !ok {
		return false, nil
	}
	if len(payload) > wire.MaxFrameSize {
		syncLostPayload := wire.EncodeSyncLostPayload(uint64(len(payload)))
		if p.logger != nil {
			p.logger.Warn(
				"termx attachment stream payload exceeded frame cap",
				"terminal_id", p.terminalID,
				"remote", p.remote,
				"channel", p.channel,
				"type", streamMessageTypeName(msg.Type),
				"payload_bytes", len(payload),
				"max_frame_bytes", wire.MaxFrameSize,
			)
		}
		if err := p.sendFrame(p.channel, wire.TypeSyncLost, syncLostPayload); err != nil {
			return false, err
		}
		p.recordSentFrame(wire.TypeSyncLost, len(syncLostPayload))
		return false, nil
	}
	screenSequence := uint64(0)
	if typ == wire.TypeScreenUpdate {
		screenSequence = p.nextScreenSequence()
		p.recordSentScreenFrameAckState(screenSequence, msg.Revision)
		if resolvedLatest {
			p.markLatestScreenFrameStarted()
		}
	}
	if err := p.sendFrame(p.channel, typ, payload); err != nil {
		return false, err
	}
	switch typ {
	case wire.TypeScreenUpdate:
		p.recordSentScreenStateRevision(msg.Revision)
	case wire.TypeSyncLost:
		p.recordSentScreenStateRevision(0)
	}
	p.recordSentFrame(typ, len(payload))
	if typ == wire.TypeScreenUpdate && coreScreenUpdateDebugEnabled() && p.logger != nil {
		p.mu.Lock()
		readyMode := p.readyMode
		screenCredit := p.screenCredit
		screenInFlight := p.screenInFlight
		screenDirty := p.screenDirty
		forceLatestScreen := p.forceLatestScreen
		queueFrames := len(p.queue)
		queueBytes := p.queueBytes
		stateRevision := p.screenStateRevision
		p.mu.Unlock()
		p.logger.Debug(
			"termx attachment stream send screen",
			"terminal_id", p.terminalID,
			"remote", p.remote,
			"channel", p.channel,
			"screen_sequence", screenSequence,
			"revision", msg.Revision,
			"payload_bytes", len(payload),
			"resolved_latest", resolvedLatest,
			"ready_mode", readyMode,
			"screen_credit", screenCredit,
			"screen_in_flight", screenInFlight,
			"screen_dirty", screenDirty,
			"force_latest_screen", forceLatestScreen,
			"screen_state_revision", stateRevision,
			"queue_frames", queueFrames,
			"queue_bytes", queueBytes,
		)
	}
	return typ == wire.TypeScreenUpdate, nil
}

func (p *attachmentStreamPump) shouldSendLatestScreenFrame(msg StreamMessage) bool {
	if p == nil || msg.Type != StreamScreenUpdate {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.forceLatestScreen {
		return true
	}
	if !p.readyMode || msg.Revision == 0 {
		return false
	}
	expectedRevision := p.screenStateRevision + 1
	if msg.Revision <= expectedRevision {
		return false
	}
	p.forceLatestScreen = true
	p.stats.coalescedFrames++
	perftrace.Count("transport.stream.ready.revision_gap_frames", 1)
	return true
}

func (p *attachmentStreamPump) markLatestScreenFrameStarted() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.forceLatestScreen = false
	p.mu.Unlock()
}

func (p *attachmentStreamPump) releaseUnsentScreenFrame() {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.screenInFlight {
		p.screenInFlight = false
		p.screenCredit = true
		if p.screenDirty && !p.hasQueuedScreenUpdateLocked() {
			p.appendQueueMessageLocked(StreamMessage{Type: StreamScreenUpdate})
		}
	}
	p.screenDirty = false
	p.cond.Signal()
	p.mu.Unlock()
}

func (p *attachmentStreamPump) currentRevision() uint64 {
	if p == nil || p.revision == nil {
		return 0
	}
	return p.revision()
}

func (p *attachmentStreamPump) nextScreenSequence() uint64 {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.screenSequence++
	return p.screenSequence
}

func (p *attachmentStreamPump) recordSentScreenFrameAckState(screenSequence, screenRevision uint64) {
	if p == nil || screenSequence == 0 {
		return
	}
	p.mu.Lock()
	if p.screenSequenceRevisions == nil {
		p.screenSequenceRevisions = make(map[uint64]uint64)
	}
	p.screenSequenceRevisions[screenSequence] = screenRevision
	p.pruneOldScreenSequenceRevisionsLocked()
	p.mu.Unlock()
}

func (p *attachmentStreamPump) pruneOldScreenSequenceRevisionsLocked() {
	if p == nil || attachmentStreamAckRevisionWindow <= 0 || len(p.screenSequenceRevisions) <= attachmentStreamAckRevisionWindow {
		return
	}
	minSequence := uint64(0)
	if p.screenSequence > attachmentStreamAckRevisionWindow {
		minSequence = p.screenSequence - attachmentStreamAckRevisionWindow
	}
	for sequence := range p.screenSequenceRevisions {
		if sequence < minSequence {
			delete(p.screenSequenceRevisions, sequence)
		}
	}
}

func (p *attachmentStreamPump) pruneScreenSequenceRevisionsLocked(ackedSequence uint64) {
	if p == nil || ackedSequence == 0 {
		return
	}
	for sequence := range p.screenSequenceRevisions {
		if sequence <= ackedSequence {
			delete(p.screenSequenceRevisions, sequence)
		}
	}
}

func (p *attachmentStreamPump) recordSentScreenStateRevision(revision uint64) {
	if p == nil {
		return
	}
	if revision == 0 {
		p.mu.Lock()
		p.screenStateRevision = 0
		p.mu.Unlock()
		return
	}
	p.mu.Lock()
	if revision > 0 {
		p.screenStateRevision = revision
	}
	p.mu.Unlock()
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

	msgCountBefore := len(p.queue)
	if msg.Type == StreamScreenUpdate {
		if p.readyMode && (p.screenInFlight || !p.screenCredit || p.hasQueuedScreenUpdateLocked()) {
			p.screenDirty = true
			p.forceLatestScreen = true
			p.stats.coalescedFrames++
			perftrace.Count("transport.stream.ready.coalesced_frames", 1)
			p.debugLogScreenQueueLocked("coalesce_ready_wait", msg)
			return
		}
		if p.forceLatestScreen {
			msg = StreamMessage{Type: StreamScreenUpdate}
		}
		if removed := p.dropQueuedScreenUpdatesLocked(); removed > 0 {
			p.stats.coalescedFrames += removed
			perftrace.Count("transport.stream.backlog.coalesced_frames", removed)
			p.forceLatestScreen = true
			msg = StreamMessage{Type: StreamScreenUpdate}
		}
		p.appendQueueMessageLocked(msg)
		p.debugLogScreenQueueLocked("enqueue", msg)
	} else {
		p.appendQueueMessageLocked(msg)
	}
	p.queueBytes = estimateStreamQueueWireBytes(p.queue)
	if len(p.queue) > msgCountBefore {
		perftrace.Count("transport.stream.backlog.enqueued_frames", len(p.queue)-msgCountBefore)
	}
	p.cond.Signal()
}

func (p *attachmentStreamPump) appendQueueMessageLocked(msg StreamMessage) {
	p.queue = append(p.queue, msg)
	p.queueBytes = estimateStreamQueueWireBytes(p.queue)
	p.recordEnqueuedLocked(msg)
}

func (p *attachmentStreamPump) debugLogScreenQueueLocked(action string, msg StreamMessage, extra ...any) {
	if p == nil || p.logger == nil || !coreScreenUpdateDebugEnabled() || msg.Type != StreamScreenUpdate {
		return
	}
	fields := []any{
		"terminal_id", p.terminalID,
		"remote", p.remote,
		"channel", p.channel,
		"action", action,
		"revision", msg.Revision,
		"payload_bytes", len(msg.Payload),
		"placeholder", len(msg.Payload) == 0,
		"ready_mode", p.readyMode,
		"screen_credit", p.screenCredit,
		"screen_in_flight", p.screenInFlight,
		"screen_dirty", p.screenDirty,
		"force_latest_screen", p.forceLatestScreen,
		"screen_sequence", p.screenSequence,
		"last_acked_screen_sequence", p.lastAckedScreenSequence,
		"latest_acked_revision", p.latestAckedRevision,
		"screen_state_revision", p.screenStateRevision,
		"queue_frames", len(p.queue),
		"queue_bytes", p.queueBytes,
	}
	fields = append(fields, extra...)
	p.logger.Debug("termx attachment stream screen queue", fields...)
}

func (p *attachmentStreamPump) hasQueuedScreenUpdateLocked() bool {
	for _, msg := range p.queue {
		if msg.Type == StreamScreenUpdate {
			return true
		}
	}
	return false
}

func (p *attachmentStreamPump) dropQueuedScreenUpdatesLocked() int {
	if p == nil || len(p.queue) == 0 {
		return 0
	}
	removed := 0
	dst := p.queue[:0]
	for _, msg := range p.queue {
		if msg.Type == StreamScreenUpdate {
			removed++
			continue
		}
		dst = append(dst, msg)
	}
	for i := len(dst); i < len(p.queue); i++ {
		p.queue[i] = StreamMessage{}
	}
	p.queue = dst
	return removed
}

func (p *attachmentStreamPump) recordEnqueuedLocked(msg StreamMessage) {
	if p == nil {
		return
	}
	payloadBytes := streamMessagePayloadBytes(msg)
	p.stats.enqueuedFrames++
	p.stats.enqueuedBytes += payloadBytes
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
			index := p.sendableQueueIndexLocked()
			if index < 0 {
				if p.inputClosed || p.ctx.Err() != nil {
					return StreamMessage{}, false
				}
				p.cond.Wait()
				continue
			}
			msg := p.removeQueueMessageLocked(index)
			if p.readyMode && msg.Type == StreamScreenUpdate {
				p.screenCredit = false
				p.screenInFlight = true
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

func (p *attachmentStreamPump) sendableQueueIndexLocked() int {
	if len(p.queue) == 0 {
		return -1
	}
	if !p.readyMode || p.queue[0].Type != StreamScreenUpdate || p.screenCredit {
		return 0
	}
	for i := 1; i < len(p.queue); i++ {
		if p.queue[i].Type != StreamScreenUpdate {
			return i
		}
	}
	return -1
}

func (p *attachmentStreamPump) removeQueueMessageLocked(index int) StreamMessage {
	msg := p.queue[index]
	copy(p.queue[index:], p.queue[index+1:])
	last := len(p.queue) - 1
	p.queue[last] = StreamMessage{}
	p.queue = p.queue[:last]
	return msg
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
	case StreamSyncLost:
		return wire.TypeSyncLost, wire.EncodeSyncLostPayload(msg.DroppedBytes), true
	case StreamResize:
		return wire.TypeResize, wire.EncodeResizePayload(msg.Cols, msg.Rows), true
	case StreamBootstrapDone:
		return wire.TypeBootstrapDone, nil, true
	case StreamScreenUpdate:
		return wire.TypeScreenUpdate, msg.Payload, true
	case StreamClosed:
		code := 0
		if msg.ExitCode != nil {
			code = *msg.ExitCode
		}
		return wire.TypeClosed, wire.EncodeClosedPayload(code), true
	default:
		return 0, nil, false
	}
}
