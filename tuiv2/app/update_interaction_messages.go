package app

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/tuiv2/input"
	localvterm "github.com/lozzow/termx/vterm"
)

func (m *Model) handleInteractionMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch typed := msg.(type) {
	case interactionBatchMsg:
		return m.handleInteractionBatch(typed.Messages), true
	case mouseWheelBurstMsg:
		queueDelay := time.Since(typed.QueuedAt)
		appendMouseDebugLog(
			"mouse_process",
			"seq", typed.Seq,
			"kind", "wheel",
			"action", typed.Msg.Action,
			"button", typed.Msg.Button,
			"x", typed.Msg.X,
			"y", typed.Msg.Y,
			"queued_at", typed.QueuedAt.UTC().Format(time.RFC3339Nano),
			"queue_ms", queueDelay.Milliseconds(),
		)
		if typed.Seq > 0 && typed.Seq < latestQueuedWheelSeq() {
			appendMouseDebugLog("mouse_drop_stale", "seq", typed.Seq, "latest_seq", latestQueuedWheelSeq(), "kind", "wheel", "x", typed.Msg.X, "y", typed.Msg.Y)
			return nil, true
		}
		if !typed.QueuedAt.IsZero() && queueDelay > effectiveMouseWheelStaleThreshold() {
			appendMouseDebugLog("mouse_drop_lagged", "seq", typed.Seq, "queue_ms", queueDelay.Milliseconds(), "kind", "wheel", "x", typed.Msg.X, "y", typed.Msg.Y)
			return nil, true
		}
		return m.handleMouseWheelBurstMsg(typed), true
	case keyBurstMsg:
		m.beginBoundaryInteraction()
		return m.handleKeyBurstMsg(typed), true
	case queuedMouseMsg:
		queueDelay := time.Since(typed.QueuedAt)
		appendMouseDebugLog(
			"mouse_process",
			"seq", typed.Seq,
			"kind", typed.Kind,
			"action", typed.Msg.Action,
			"button", typed.Msg.Button,
			"x", typed.Msg.X,
			"y", typed.Msg.Y,
			"queued_at", typed.QueuedAt.UTC().Format(time.RFC3339Nano),
			"queue_ms", queueDelay.Milliseconds(),
		)
		if typed.Kind == "motion" && typed.Seq < latestQueuedMotionSeq() {
			appendMouseDebugLog("mouse_drop_stale", "seq", typed.Seq, "latest_seq", latestQueuedMotionSeq(), "x", typed.Msg.X, "y", typed.Msg.Y)
			return nil, true
		}
		if typed.Kind == "motion" && queueDelay > staleMouseMotionThreshold {
			appendMouseDebugLog("mouse_drop_lagged", "seq", typed.Seq, "queue_ms", queueDelay.Milliseconds(), "x", typed.Msg.X, "y", typed.Msg.Y)
			return nil, true
		}
		if typed.Kind == "motion" && typed.Msg.Button == tea.MouseButtonNone && (m.mouseDragMode != mouseDragNone || m.copyMode.MouseSelecting) {
			m.beginBoundaryInteraction()
			return m.handleMouseMsg(typed.Msg), true
		}
		if typed.Kind != "motion" {
			m.beginBoundaryInteraction()
		}
		if typed.Kind == "motion" && (m.mouseDragMode != mouseDragNone || m.copyMode.MouseSelecting) {
			return m.enqueueMouseMotionFlush(typed), true
		}
		return m.handleMouseMsg(typed.Msg), true
	case mouseMotionFlushMsg:
		return m.handleMouseMotionFlush(typed), true
	case tea.MouseMsg:
		if m.isBoundaryMouseMsg(typed) {
			m.beginBoundaryInteraction()
		}
		return m.handleMouseMsg(typed), true
	case tea.KeyMsg:
		m.beginBoundaryInteraction()
		return m.handleKeyMsg(typed), true
	case prefixTimeoutMsg:
		if typed.seq == m.prefixSeq && m.isStickyMode() {
			m.setMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
		}
		return nil, true
	case SemanticActionMsg:
		m.beginBoundaryInteraction()
		return m.dispatchSemanticActionCmd(typed.Action, true), true
	case input.SemanticAction:
		m.beginBoundaryInteraction()
		return m.dispatchSemanticActionCmd(typed, true), true
	case TerminalInputMsg:
		if !isContinuousTerminalInput(typed.Input) {
			m.beginBoundaryInteraction()
		}
		return m.handleTerminalInput(typed.Input), true
	case input.TerminalInput:
		if !isContinuousTerminalInput(typed) {
			m.beginBoundaryInteraction()
		}
		return m.handleTerminalInput(typed), true
	case terminalInputSentMsg:
		next := tea.Cmd(nil)
		if typed.continuous && len(m.terminalInputs.boundaryQueue) == 0 && m.terminalInputs.wheel != nil {
			m.terminalInputSending = false
			next = m.scheduleTerminalWheelContinuationDispatchCmd()
		} else {
			next = m.dequeueTerminalInputCmd()
		}
		if typed.err != nil {
			return tea.Batch(m.showError(typed.err), next), true
		}
		if next == nil {
			m.scheduleSharedTerminalSnapshotResync(typed.paneID, typed.terminalID)
			return nil, true
		}
		return next, true
	case terminalWheelDispatchMsg:
		if typed.seq != m.terminalWheelDispatchSeq {
			return nil, true
		}
		m.terminalWheelDispatchPending = false
		if m.terminalInputSending {
			return nil, true
		}
		return m.dequeueTerminalInputCmd(), true
	case sharedTerminalSnapshotResyncMsg:
		if typed.seq != m.terminalResyncSeq {
			return nil, true
		}
		if m.terminalInputSending || m.terminalInputs.HasPending() {
			return nil, true
		}
		return m.sharedTerminalSnapshotResyncCmd(typed.terminalID), true
	case sequenceMsg:
		return m.nextSequenceCmd(typed), true
	case copyModeAutoScrollMsg:
		return m.handleCopyModeAutoScroll(typed.seq), true
	default:
		return nil, false
	}
}

func (m *Model) scheduleSharedTerminalSnapshotResync(paneID, terminalID string) {
	if !m.shouldResyncSharedTerminalSnapshot(paneID, terminalID) {
		return
	}
	m.terminalResyncSeq++
	seq := m.terminalResyncSeq
	msg := sharedTerminalSnapshotResyncMsg{
		seq:        seq,
		paneID:     paneID,
		terminalID: terminalID,
	}
	if sharedTerminalSnapshotResyncDelay <= 0 {
		m.sendAsync(msg)
		return
	}
	time.AfterFunc(sharedTerminalSnapshotResyncDelay, func() {
		if m == nil {
			return
		}
		m.sendAsync(msg)
	})
}

func (m *Model) shouldResyncSharedTerminalSnapshot(paneID, terminalID string) bool {
	if m == nil || m.runtime == nil || m.runtime.Client() == nil || terminalID == "" {
		return false
	}
	control := m.runtime.TerminalControlStatus(terminalID)
	if control.TerminalID == "" || len(control.BoundPaneIDs) < 2 {
		return false
	}
	terminal := m.runtime.Registry().Get(terminalID)
	if terminal == nil {
		return false
	}
	modes := localvterm.TerminalModes{}
	switch {
	case terminal.VTerm != nil:
		modes = terminal.VTerm.Modes()
	case terminal.Snapshot != nil:
		modes = localvterm.TerminalModes{
			AlternateScreen: terminal.Snapshot.Modes.AlternateScreen,
			MouseTracking:   terminal.Snapshot.Modes.MouseTracking,
			BracketedPaste:  terminal.Snapshot.Modes.BracketedPaste,
		}
	}
	if !modes.AlternateScreen && !modes.MouseTracking {
		return false
	}
	if paneID != "" {
		if pane, _, ok := m.visiblePaneForInput(paneID); !ok || pane == nil || pane.TerminalID != terminalID {
			return false
		}
	}
	return true
}

func (m *Model) sharedTerminalSnapshotResyncCmd(terminalID string) tea.Cmd {
	if m == nil || m.runtime == nil || m.runtime.Client() == nil || terminalID == "" {
		return nil
	}
	return func() tea.Msg {
		if _, err := m.runtime.LoadSnapshot(context.Background(), terminalID, 0, defaultTerminalSnapshotScrollbackLimit); err != nil {
			return err
		}
		return InvalidateMsg{}
	}
}

func (m *Model) handleInteractionBatch(messages []tea.Msg) tea.Cmd {
	if len(messages) == 0 {
		return nil
	}
	messages = compactInteractionMessages(messages)
	perftrace.Count("app.interaction.batch", len(messages))
	wasSending := m.terminalInputSending
	prevBatchState := m.interactionBatchActive
	m.interactionBatchActive = true
	defer func() {
		m.interactionBatchActive = prevBatchState
	}()
	cmds := make([]tea.Cmd, 0, len(messages))
	for _, msg := range messages {
		cmd, ok := m.handleInteractionMessage(msg)
		if !ok || cmd == nil {
			continue
		}
		cmds = append(cmds, cmd)
	}
	if !wasSending {
		if cmd := m.dequeueTerminalInputCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return batchCmds(cmds...)
}

func compactInteractionMessages(messages []tea.Msg) []tea.Msg {
	if len(messages) <= 1 {
		return messages
	}
	compacted := make([]tea.Msg, 0, len(messages))
	var continuous interactionContinuousAccumulator
	for _, msg := range messages {
		if continuous.Queue(msg) {
			continue
		}
		continuous.Reset()
		compacted = append(compacted, msg)
	}
	compacted = append(compacted, continuous.Flush()...)
	return compacted
}

func (m *Model) enqueueMouseMotionFlush(msg queuedMouseMsg) tea.Cmd {
	if m == nil {
		return nil
	}
	copyMsg := msg
	m.pendingMouseMotion = &copyMsg
	epoch := m.interactionBoundaryEpoch
	if m.mouseMotionFlushPending {
		return nil
	}
	m.mouseMotionFlushPending = true
	return func() tea.Msg {
		return mouseMotionFlushMsg{epoch: epoch}
	}
}

func (m *Model) handleMouseMotionFlush(msg mouseMotionFlushMsg) tea.Cmd {
	if m == nil {
		return nil
	}
	m.mouseMotionFlushPending = false
	if msg.epoch != m.interactionBoundaryEpoch {
		m.pendingMouseMotion = nil
		return nil
	}
	if m.pendingMouseMotion == nil {
		return nil
	}
	pending := *m.pendingMouseMotion
	m.pendingMouseMotion = nil
	queueDelay := time.Since(pending.QueuedAt)
	appendMouseDebugLog(
		"mouse_motion_flush",
		"seq", pending.Seq,
		"x", pending.Msg.X,
		"y", pending.Msg.Y,
		"queue_ms", queueDelay.Milliseconds(),
	)
	if pending.Seq < latestQueuedMotionSeq() {
		appendMouseDebugLog("mouse_drop_stale", "seq", pending.Seq, "latest_seq", latestQueuedMotionSeq(), "x", pending.Msg.X, "y", pending.Msg.Y)
		return nil
	}
	if queueDelay > staleMouseMotionThreshold {
		appendMouseDebugLog("mouse_drop_lagged", "seq", pending.Seq, "queue_ms", queueDelay.Milliseconds(), "x", pending.Msg.X, "y", pending.Msg.Y)
		return nil
	}
	return m.handleMouseMsg(pending.Msg)
}

func isRawMouseWheelContinuous(msg tea.MouseMsg) bool {
	return msg.Action == tea.MouseActionPress && mouseWheelButtonStep(msg.Button) != 0
}

func (m *Model) isBoundaryMouseMsg(msg tea.MouseMsg) bool {
	if isRawMouseWheelContinuous(msg) {
		return false
	}
	switch msg.Action {
	case tea.MouseActionPress, tea.MouseActionRelease:
		return true
	case tea.MouseActionMotion:
		return msg.Button == tea.MouseButtonNone && (m.mouseDragMode != mouseDragNone || m.copyMode.MouseSelecting)
	default:
		return false
	}
}
