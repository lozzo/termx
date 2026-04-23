package app

import (
	"bytes"
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

var terminalWheelDispatchDelay = 2 * time.Millisecond
var remoteTerminalWheelDispatchDelay = 1 * time.Millisecond
var terminalWheelTailDispatchDelay = 8 * time.Millisecond
var remoteTerminalWheelTailDispatchDelay = 6 * time.Millisecond
var continuousWheelStaleThreshold = 20 * time.Millisecond
var remoteContinuousWheelStaleThreshold = 12 * time.Millisecond

func (m *Model) isStickyMode() bool {
	kind := m.mode().Kind
	return kind == input.ModePane || kind == input.ModeResize || kind == input.ModeTab ||
		kind == input.ModeWorkspace || kind == input.ModeFloating ||
		kind == input.ModeGlobal
}

func (m *Model) rearmPrefixTimeoutCmd() tea.Cmd {
	m.prefixSeq++
	seq := m.prefixSeq
	return tea.Tick(prefixModeTimeout, func(time.Time) tea.Msg {
		return prefixTimeoutMsg{seq: seq}
	})
}

func (m *Model) handleTerminalInput(in input.TerminalInput) tea.Cmd {
	if normalized, cmd, ok := m.resolveTerminalInputDispatch(in); !ok {
		return cmd
	} else {
		in = normalized
	}
	m.enqueueTerminalInput(in)
	if m.interactionBatchActive {
		return nil
	}
	if m.terminalInputSending {
		return nil
	}
	if isContinuousTerminalInput(in) {
		if m.terminalWheelDispatchPending {
			return nil
		}
		if delay := effectiveTerminalWheelDispatchDelay(); delay > 0 {
			return m.scheduleTerminalWheelDispatchCmdWithDelay(delay)
		}
		return m.dequeueTerminalInputCmd()
	}
	return m.dequeueTerminalInputCmd()
}

func (m *Model) scheduleTerminalWheelDispatchCmd() tea.Cmd {
	return m.scheduleTerminalWheelDispatchCmdWithDelay(effectiveTerminalWheelDispatchDelay())
}

func (m *Model) scheduleTerminalWheelContinuationDispatchCmd() tea.Cmd {
	return m.scheduleTerminalWheelDispatchCmdWithDelay(effectiveTerminalWheelTailDispatchDelay())
}

func (m *Model) scheduleTerminalWheelDispatchCmdWithDelay(delay time.Duration) tea.Cmd {
	if m == nil {
		return nil
	}
	if m.terminalWheelDispatchPending {
		return nil
	}
	if delay <= 0 {
		return m.dequeueTerminalInputCmd()
	}
	m.terminalWheelDispatchSeq++
	seq := m.terminalWheelDispatchSeq
	m.terminalWheelDispatchPending = true
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return terminalWheelDispatchMsg{seq: seq}
	})
}

func effectiveTerminalWheelDispatchDelay() time.Duration {
	delay := terminalWheelDispatchDelay
	if shared.RemoteLatencyProfileEnabled() && (delay <= 0 || delay > remoteTerminalWheelDispatchDelay) {
		delay = remoteTerminalWheelDispatchDelay
	}
	return shared.DurationOverride("TERMX_TERMINAL_WHEEL_DISPATCH_DELAY", delay)
}

func effectiveTerminalWheelTailDispatchDelay() time.Duration {
	delay := terminalWheelTailDispatchDelay
	if shared.RemoteLatencyProfileEnabled() && (delay <= 0 || delay > remoteTerminalWheelTailDispatchDelay) {
		delay = remoteTerminalWheelTailDispatchDelay
	}
	return shared.DurationOverride("TERMX_TERMINAL_WHEEL_TAIL_DISPATCH_DELAY", delay)
}

func effectiveContinuousWheelStaleThreshold() time.Duration {
	delay := continuousWheelStaleThreshold
	if shared.RemoteLatencyProfileEnabled() && (delay <= 0 || delay > remoteContinuousWheelStaleThreshold) {
		delay = remoteContinuousWheelStaleThreshold
	}
	return shared.DurationOverride("TERMX_CONTINUOUS_WHEEL_STALE_THRESHOLD", delay)
}

func (m *Model) handleKeyBurstMsg(msg keyBurstMsg) tea.Cmd {
	repeat := maxInt(1, msg.Repeat)
	if repeat == 1 {
		return m.handleKeyMsg(msg.Msg)
	}
	if inputMsg, ok := m.repeatedTerminalInputForKeyMsg(msg.Msg, repeat); ok {
		return m.handleTerminalInput(inputMsg)
	}
	cmds := make([]tea.Cmd, 0, repeat)
	for i := 0; i < repeat; i++ {
		if cmd := m.handleKeyMsg(msg.Msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return batchCmds(cmds...)
}

func (m *Model) repeatedTerminalInputForKeyMsg(msg tea.KeyMsg, repeat int) (input.TerminalInput, bool) {
	if m == nil || m.input == nil || repeat <= 1 {
		return input.TerminalInput{}, false
	}
	if m.modalHost != nil && m.modalHost.Session != nil {
		return input.TerminalInput{}, false
	}
	if m.emptyPaneSelectionPaneID != "" || m.exitedPaneSelectionPaneID != "" {
		return input.TerminalInput{}, false
	}
	if _, ok := m.restartActionForKeyMsg(msg); ok {
		return input.TerminalInput{}, false
	}
	result := m.input.RouteKeyMsg(msg)
	if result.Action != nil || result.TerminalInput == nil {
		return input.TerminalInput{}, false
	}
	inputMsg := *result.TerminalInput
	if inputMsg.PaneID == "" && m.workbench != nil {
		if pane := m.workbench.ActivePane(); pane != nil {
			inputMsg.PaneID = pane.ID
		}
	}
	if encoded := m.encodeActiveTerminalInput(msg, inputMsg.PaneID); len(encoded) > 0 {
		inputMsg.Data = encoded
	}
	if len(inputMsg.Data) == 0 {
		return input.TerminalInput{}, false
	}
	inputMsg.Repeat = repeat
	return inputMsg, true
}

func (m *Model) enqueueTerminalInput(in input.TerminalInput) {
	if m == nil {
		return
	}
	m.terminalInputs.Enqueue(in)
}

func (m *Model) openPickerIfUnattached(paneID string) tea.Cmd {
	if m == nil || m.workbench == nil || m.modalHost == nil {
		return nil
	}
	pane := m.workbench.ActivePane()
	if pane == nil || pane.ID != paneID || pane.TerminalID != "" {
		return nil
	}
	if m.modalHost.Session != nil {
		return nil
	}
	m.openModal(input.ModePicker, paneID)
	m.resetPickerState()
	m.render.Invalidate()
	return m.applyEffects([]orchestrator.Effect{orchestrator.LoadPickerItemsEffect{}})
}

func (m *Model) dequeueTerminalInputCmd() tea.Cmd {
	if m == nil {
		return nil
	}
	next, ok := m.dequeueTerminalInputBatch()
	if !ok {
		m.terminalInputSending = false
		return nil
	}
	m.terminalInputSending = true
	if m.canDirectSendForwardedWheelInput(next) {
		perftrace.Count("app.input.wheel.direct_dequeue", len(next.Data))
		return m.terminalInputDirectSendCmd(next)
	}
	return m.terminalInputSendCmd(next)
}

func (m *Model) terminalInputExpanded(in input.TerminalInput) input.TerminalInput {
	in.Repeat = maxInt(1, in.Repeat)
	if in.Repeat > 1 && len(in.Data) > 0 {
		in.Data = bytes.Repeat(in.Data, in.Repeat)
		in.Repeat = 1
	}
	return in
}

func (m *Model) terminalInputSendCmd(next input.TerminalInput) tea.Cmd {
	if m == nil {
		return nil
	}
	continuous := isContinuousTerminalInput(next)
	next = m.terminalInputExpanded(next)
	target, _ := m.resolveTerminalInteractionTarget(terminalInteractionRequest{PaneID: next.PaneID})
	terminalID := target.terminalID
	return func() tea.Msg {
		prepareFinish := perftrace.Measure("app.input.prepare")
		err := m.prepareTerminalInput(context.Background(), next.PaneID)
		prepareFinish(len(next.Data))
		if err != nil {
			return terminalInputSentMsg{err: err, paneID: next.PaneID, terminalID: terminalID, continuous: continuous}
		}
		sendFinish := perftrace.Measure("app.input.send")
		err = m.runtime.SendInput(context.Background(), next.PaneID, next.Data)
		sendFinish(len(next.Data))
		return terminalInputSentMsg{
			err:        err,
			paneID:     next.PaneID,
			terminalID: terminalID,
			continuous: continuous,
		}
	}
}

func (m *Model) terminalInputDirectSendCmd(next input.TerminalInput) tea.Cmd {
	if m == nil {
		return nil
	}
	continuous := isContinuousTerminalInput(next)
	next = m.terminalInputExpanded(next)
	target, _ := m.resolveTerminalInteractionTarget(terminalInteractionRequest{PaneID: next.PaneID})
	terminalID := target.terminalID
	return func() tea.Msg {
		sendFinish := perftrace.Measure("app.input.send")
		err := m.runtime.SendInput(context.Background(), next.PaneID, next.Data)
		sendFinish(len(next.Data))
		return terminalInputSentMsg{
			err:        err,
			paneID:     next.PaneID,
			terminalID: terminalID,
			continuous: continuous,
		}
	}
}

func (m *Model) dequeueTerminalInputBatch() (input.TerminalInput, bool) {
	if m == nil {
		return input.TerminalInput{}, false
	}
	return m.terminalInputs.Dequeue(m.isPaneAttachPending, time.Now(), effectiveContinuousWheelStaleThreshold())
}

func (m *Model) prepareTerminalInput(ctx context.Context, paneID string) error {
	target, ok := m.resolveTerminalInteractionTarget(terminalInteractionRequest{PaneID: paneID})
	if !ok {
		return nil
	}
	return m.syncTerminalInteraction(ctx, terminalInteractionRequest{
		PaneID:               target.paneID,
		TerminalID:           target.terminalID,
		Rect:                 target.rect,
		ResizeIfNeeded:       true,
		ImplicitSessionLease: true,
	}, target)
}

func (m *Model) ensurePaneTerminalSize(ctx context.Context, paneID, terminalID string, rect workbench.Rect) error {
	service := m.layoutResizeService()
	if service == nil {
		return nil
	}
	return service.ensurePaneTerminalSize(ctx, paneID, terminalID, rect)
}

func (m *Model) implicitSessionLeaseNeedsAcquire(terminalID, paneID string) bool {
	if m == nil || terminalID == "" || paneID == "" || m.sessionViewID == "" {
		return false
	}
	lease, ok := m.sessionLeases[terminalID]
	if !ok {
		return false
	}
	return lease.PaneID == paneID && lease.ViewID != "" && lease.ViewID != m.sessionViewID
}
