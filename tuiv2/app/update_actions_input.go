package app

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/orchestrator"
)

func (m *Model) isStickyMode() bool {
	kind := m.input.Mode().Kind
	return kind == input.ModePane || kind == input.ModeResize || kind == input.ModeTab ||
		kind == input.ModeWorkspace || kind == input.ModeFloating || kind == input.ModeDisplay ||
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
	if len(in.Data) == 0 {
		return nil
	}
	if m.workbench != nil {
		if pane := m.workbench.ActivePane(); pane != nil && pane.TerminalID == "" {
			return m.openPickerIfUnattached(pane.ID)
		}
	}
	m.pendingTerminalInputs = append(m.pendingTerminalInputs, in)
	if m.terminalInputSending {
		return nil
	}
	return m.dequeueTerminalInputCmd()
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
	m.modalHost.Open(input.ModePicker, paneID)
	m.resetPickerState()
	m.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: paneID})
	m.render.Invalidate()
	return m.applyEffects([]orchestrator.Effect{orchestrator.LoadPickerItemsEffect{}})
}

func (m *Model) dequeueTerminalInputCmd() tea.Cmd {
	if m == nil {
		return nil
	}
	if len(m.pendingTerminalInputs) == 0 {
		m.terminalInputSending = false
		return nil
	}
	next := m.pendingTerminalInputs[0]
	m.pendingTerminalInputs = m.pendingTerminalInputs[1:]
	m.terminalInputSending = true
	return func() tea.Msg {
		return terminalInputSentMsg{
			err: m.runtime.SendInput(context.Background(), next.PaneID, next.Data),
		}
	}
}
