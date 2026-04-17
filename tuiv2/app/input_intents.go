package app

import (
	"bytes"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
)

// interactionContinuousAccumulator keeps only the live continuous intent.
// Boundary inputs invalidate these slots instead of flushing historical tails.
type interactionContinuousAccumulator struct {
	latestMotion *queuedMouseMsg
	wheel        *mouseWheelBurstMsg
}

// highFrequencyMouseAccumulator is kept as an alias for the input forwarder
// tests, but the behavior is now shared with interaction batch compaction.
type highFrequencyMouseAccumulator = interactionContinuousAccumulator

func (a *interactionContinuousAccumulator) Empty() bool {
	return a == nil || (a.latestMotion == nil && a.wheel == nil)
}

func (a *interactionContinuousAccumulator) Reset() {
	if a == nil {
		return
	}
	a.latestMotion = nil
	a.wheel = nil
}

func (a *interactionContinuousAccumulator) Queue(msg tea.Msg) bool {
	if a == nil {
		return false
	}
	switch typed := msg.(type) {
	case queuedMouseMsg:
		if typed.Kind != "motion" {
			return false
		}
		a.QueueMotion(typed)
		return true
	case mouseWheelBurstMsg:
		a.QueueWheelBurst(typed)
		return true
	default:
		return false
	}
}

func (a *interactionContinuousAccumulator) QueueMotion(msg queuedMouseMsg) {
	if a == nil {
		return
	}
	copyMsg := msg
	a.latestMotion = &copyMsg
}

func (a *interactionContinuousAccumulator) QueueWheel(msg tea.MouseMsg) {
	a.QueueWheelBurst(mouseWheelBurstMsg{Msg: msg, Repeat: 1})
}

func (a *interactionContinuousAccumulator) QueueWheelBurst(msg mouseWheelBurstMsg) {
	if a == nil {
		return
	}
	normalized, ok := normalizeMouseWheelBurst(msg)
	if !ok {
		return
	}
	if a.wheel == nil {
		copyMsg := normalized
		a.wheel = &copyMsg
		return
	}
	current := *a.wheel
	currentStep := mouseWheelButtonStep(current.Msg.Button)
	nextStep := mouseWheelButtonStep(normalized.Msg.Button)
	switch {
	case currentStep == nextStep:
		merged := normalized
		merged.Repeat = current.Repeat + normalized.Repeat
		a.wheel = &merged
	case current.Repeat > normalized.Repeat:
		current.Repeat -= normalized.Repeat
		a.wheel = &current
	case current.Repeat == normalized.Repeat:
		a.wheel = nil
	default:
		normalized.Repeat -= current.Repeat
		a.wheel = &normalized
	}
}

func (a *interactionContinuousAccumulator) Flush() []tea.Msg {
	if a == nil {
		return nil
	}
	msgs := make([]tea.Msg, 0, 2)
	if a.latestMotion != nil {
		msgs = append(msgs, *a.latestMotion)
		a.latestMotion = nil
	}
	if a.wheel != nil {
		msgs = append(msgs, *a.wheel)
		a.wheel = nil
	}
	return msgs
}

func normalizeMouseWheelBurst(msg mouseWheelBurstMsg) (mouseWheelBurstMsg, bool) {
	if mouseWheelButtonStep(msg.Msg.Button) == 0 {
		return mouseWheelBurstMsg{}, false
	}
	msg.Repeat = maxInt(1, msg.Repeat)
	return msg, true
}

func mouseWheelButtonStep(button tea.MouseButton) int {
	switch button {
	case tea.MouseButtonWheelUp:
		return 1
	case tea.MouseButtonWheelDown:
		return -1
	default:
		return 0
	}
}

type terminalInputDispatchQueue struct {
	boundaryQueue []input.TerminalInput
	wheel         *input.TerminalInput
}

func (q *terminalInputDispatchQueue) HasPending() bool {
	return q != nil && (len(q.boundaryQueue) > 0 || q.wheel != nil)
}

func (q *terminalInputDispatchQueue) ResetContinuous() {
	if q == nil {
		return
	}
	q.wheel = nil
}

func (q *terminalInputDispatchQueue) Enqueue(in input.TerminalInput) {
	if q == nil {
		return
	}
	in = normalizeTerminalInput(in)
	if isContinuousTerminalInput(in) {
		q.enqueueWheel(in)
		return
	}
	q.boundaryQueue = append(q.boundaryQueue, in)
	q.wheel = nil
}

func (q *terminalInputDispatchQueue) enqueueWheel(in input.TerminalInput) {
	if q == nil {
		return
	}
	if q.wheel == nil {
		copyIn := in
		q.wheel = &copyIn
		return
	}
	current := *q.wheel
	currentRepeat := maxInt(1, current.Repeat)
	inRepeat := maxInt(1, in.Repeat)
	switch {
	case current.PaneID == in.PaneID && current.WheelDirection == in.WheelDirection:
		merged := in
		merged.Repeat = currentRepeat + inRepeat
		q.wheel = &merged
	case current.PaneID == in.PaneID && current.WheelDirection == -in.WheelDirection:
		switch {
		case currentRepeat > inRepeat:
			current.Repeat = currentRepeat - inRepeat
			q.wheel = &current
		case currentRepeat == inRepeat:
			q.wheel = nil
		default:
			in.Repeat = inRepeat - currentRepeat
			copyIn := in
			q.wheel = &copyIn
		}
	default:
		copyIn := in
		q.wheel = &copyIn
	}
}

func (q *terminalInputDispatchQueue) Dequeue(isPaneAttachPending func(string) bool) (input.TerminalInput, bool) {
	if q == nil {
		return input.TerminalInput{}, false
	}
	if len(q.boundaryQueue) > 0 {
		first := q.boundaryQueue[0]
		if isPaneAttachPending != nil && isPaneAttachPending(first.PaneID) {
			return input.TerminalInput{}, false
		}
		batch := input.TerminalInput{Kind: first.Kind, PaneID: first.PaneID}
		data := make([]byte, 0, len(first.Data))
		consumed := 0
		for consumed < len(q.boundaryQueue) {
			next := q.boundaryQueue[consumed]
			if next.PaneID != batch.PaneID || next.Kind != batch.Kind {
				break
			}
			if next.Repeat > 1 {
				data = append(data, bytes.Repeat(next.Data, next.Repeat)...)
			} else {
				data = append(data, next.Data...)
			}
			consumed++
		}
		q.boundaryQueue = q.boundaryQueue[consumed:]
		batch.Data = data
		return batch, true
	}
	if q.wheel == nil {
		return input.TerminalInput{}, false
	}
	wheel := *q.wheel
	if isPaneAttachPending != nil && isPaneAttachPending(wheel.PaneID) {
		return input.TerminalInput{}, false
	}
	q.wheel = nil
	if wheel.Repeat > 1 {
		wheel.Data = bytes.Repeat(wheel.Data, wheel.Repeat)
	}
	wheel.Repeat = 1
	return wheel, true
}

func normalizeTerminalInput(in input.TerminalInput) input.TerminalInput {
	in.Repeat = maxInt(1, in.Repeat)
	return in
}

func isContinuousTerminalInput(in input.TerminalInput) bool {
	return in.Kind == input.TerminalInputWheel && in.PaneID != "" && in.WheelDirection != 0
}

func (m *Model) beginBoundaryInteraction() {
	if m == nil {
		return
	}
	m.interactionBoundaryEpoch++
	m.terminalWheelDispatchPending = false
	m.pendingMouseMotion = nil
	m.mouseMotionFlushPending = false
	m.terminalInputs.ResetContinuous()
	m.cancelCopyModeContinuous()
}

func (m *Model) cancelCopyModeContinuous() {
	if m == nil {
		return
	}
	if !m.copyMode.MouseSelecting && m.copyMode.AutoScrollDir == 0 {
		return
	}
	m.copyMode.MouseSelecting = false
	m.copyMode.AutoScrollDir = 0
	m.copyMode.AutoScrollSeq = m.noteCopyModeMouseActivity()
	if m.render != nil {
		m.render.Invalidate()
	}
}
