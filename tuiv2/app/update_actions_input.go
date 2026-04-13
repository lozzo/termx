package app

import (
	"bytes"
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/workbench"
)

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
	if len(in.Data) == 0 && in.Kind == input.TerminalInputPaste && in.Text != "" {
		if encoded := m.encodeActiveTerminalPaste(in.Text, in.PaneID); len(encoded) > 0 {
			in.Data = encoded
		}
	}
	if len(in.Data) == 0 {
		return nil
	}
	if m.isPaneAttachPending(in.PaneID) {
		m.enqueueTerminalInput(in)
		return nil
	}
	if m.workbench != nil {
		if pane := m.workbench.ActivePane(); pane != nil && pane.TerminalID == "" {
			return m.openPickerIfUnattached(pane.ID)
		}
	}
	m.enqueueTerminalInput(in)
	if m.interactionBatchActive {
		return nil
	}
	if m.terminalInputSending {
		return nil
	}
	return m.dequeueTerminalInputCmd()
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
	in.Repeat = maxInt(1, in.Repeat)
	if in.Kind != input.TerminalInputWheel || in.PaneID == "" || in.WheelDirection == 0 {
		m.pendingTerminalInputs = append(m.pendingTerminalInputs, in)
		return
	}
	for len(m.pendingTerminalInputs) > 0 {
		lastIdx := len(m.pendingTerminalInputs) - 1
		last := &m.pendingTerminalInputs[lastIdx]
		if last.Kind != input.TerminalInputWheel || last.PaneID != in.PaneID || last.WheelDirection == 0 {
			break
		}
		lastRepeat := maxInt(1, last.Repeat)
		inRepeat := maxInt(1, in.Repeat)
		if last.WheelDirection == in.WheelDirection && bytes.Equal(last.Data, in.Data) {
			last.Repeat = lastRepeat + inRepeat
			return
		}
		if last.WheelDirection != -in.WheelDirection {
			break
		}
		switch {
		case lastRepeat > inRepeat:
			last.Repeat = lastRepeat - inRepeat
			return
		case lastRepeat == inRepeat:
			m.pendingTerminalInputs = m.pendingTerminalInputs[:lastIdx]
			return
		default:
			m.pendingTerminalInputs = m.pendingTerminalInputs[:lastIdx]
			in.Repeat = inRepeat - lastRepeat
		}
	}
	m.pendingTerminalInputs = append(m.pendingTerminalInputs, in)
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
	target, _ := m.resolveTerminalInteractionTarget(terminalInteractionRequest{PaneID: next.PaneID})
	terminalID := target.terminalID
	return func() tea.Msg {
		prepareFinish := perftrace.Measure("app.input.prepare")
		err := m.prepareTerminalInput(context.Background(), next.PaneID)
		prepareFinish(len(next.Data))
		if err != nil {
			return terminalInputSentMsg{err: err, paneID: next.PaneID, terminalID: terminalID}
		}
		sendFinish := perftrace.Measure("app.input.send")
		err = m.runtime.SendInput(context.Background(), next.PaneID, next.Data)
		sendFinish(len(next.Data))
		return terminalInputSentMsg{
			err:        err,
			paneID:     next.PaneID,
			terminalID: terminalID,
		}
	}
}

func (m *Model) dequeueTerminalInputBatch() (input.TerminalInput, bool) {
	if m == nil || len(m.pendingTerminalInputs) == 0 {
		return input.TerminalInput{}, false
	}
	first := m.pendingTerminalInputs[0]
	if m.isPaneAttachPending(first.PaneID) {
		return input.TerminalInput{}, false
	}
	batch := input.TerminalInput{
		Kind:   first.Kind,
		PaneID: first.PaneID,
	}
	data := make([]byte, 0, len(first.Data))
	consumed := 0
	for consumed < len(m.pendingTerminalInputs) {
		next := m.pendingTerminalInputs[consumed]
		if next.PaneID != batch.PaneID {
			break
		}
		if next.Repeat > 1 {
			data = append(data, bytes.Repeat(next.Data, next.Repeat)...)
		} else {
			data = append(data, next.Data...)
		}
		consumed++
	}
	m.pendingTerminalInputs = m.pendingTerminalInputs[consumed:]
	batch.Data = data
	return batch, true
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
	return m.syncTerminalInteraction(ctx, terminalInteractionRequest{
		PaneID:         paneID,
		TerminalID:     terminalID,
		Rect:           rect,
		ResizeIfNeeded: true,
	}, terminalInteractionTarget{
		paneID:     paneID,
		terminalID: terminalID,
		rect:       rect,
	})
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

func (m *Model) terminalAlreadySized(terminalID string, cols, rows uint16) bool {
	if m == nil || m.runtime == nil || terminalID == "" || cols == 0 || rows == 0 {
		return false
	}
	terminal := m.runtime.Registry().Get(terminalID)
	if terminal == nil {
		return false
	}
	if terminal.Snapshot != nil && terminal.Snapshot.Size.Cols == cols && terminal.Snapshot.Size.Rows == rows {
		return true
	}
	if terminal.VTerm == nil {
		return false
	}
	currentCols, currentRows := terminal.VTerm.Size()
	return currentCols == int(cols) && currentRows == int(rows)
}

func (m *Model) visiblePaneForInput(paneID string) (*workbench.PaneState, workbench.Rect, bool) {
	if m == nil || m.workbench == nil {
		return nil, workbench.Rect{}, false
	}
	tabState := m.workbench.CurrentTab()
	if tabState == nil {
		return nil, workbench.Rect{}, false
	}
	if paneID == "" {
		if pane := m.workbench.ActivePane(); pane != nil {
			paneID = pane.ID
		}
	}
	if paneID == "" {
		return nil, workbench.Rect{}, false
	}
	pane := tabState.Panes[paneID]
	if pane == nil {
		return nil, workbench.Rect{}, false
	}
	visible := m.workbench.VisibleWithSize(m.bodyRect())
	if visible == nil || visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		return nil, workbench.Rect{}, false
	}
	tab := visible.Tabs[visible.ActiveTab]
	for i := range visible.FloatingPanes {
		if visible.FloatingPanes[i].ID == paneID {
			return pane, visible.FloatingPanes[i].Rect, true
		}
	}
	for i := range tab.Panes {
		if tab.Panes[i].ID == paneID {
			return pane, tab.Panes[i].Rect, true
		}
	}
	return nil, workbench.Rect{}, false
}

func sharedInputLeaseUnsupportedError() error {
	return teaErr("connected termx daemon is too old for shared resize control; restart the daemon and reconnect")
}

type teaErr string

func (e teaErr) Error() string { return string(e) }

func (m *Model) markPendingPaneAttach(paneID, terminalID string) {
	if m == nil || paneID == "" {
		return
	}
	if m.pendingPaneAttaches == nil {
		m.pendingPaneAttaches = make(map[string]string)
	}
	m.pendingPaneAttaches[paneID] = terminalID
}

func (m *Model) clearPendingPaneAttach(paneID, terminalID string) {
	if m == nil || len(m.pendingPaneAttaches) == 0 || paneID == "" {
		return
	}
	current, ok := m.pendingPaneAttaches[paneID]
	if !ok {
		return
	}
	if terminalID != "" && current != "" && current != terminalID {
		return
	}
	delete(m.pendingPaneAttaches, paneID)
}

func (m *Model) isPaneAttachPending(paneID string) bool {
	if m == nil || paneID == "" || len(m.pendingPaneAttaches) == 0 {
		return false
	}
	_, ok := m.pendingPaneAttaches[paneID]
	return ok
}
