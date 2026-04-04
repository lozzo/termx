package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (m *Model) applyEffects(effects []orchestrator.Effect) tea.Cmd {
	if len(effects) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(effects))
	for _, effect := range effects {
		if cmd := m.effectCmd(effect); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

func (m *Model) clampFloatingPanesToViewport() {
	if m == nil || m.workbench == nil || m.width <= 0 || m.height <= 0 {
		return
	}
	m.workbench.ClampFloatingPanesToBounds(workbench.Rect{W: maxInt(1, m.width), H: maxInt(1, m.height-2)})
}

func (m *Model) effectCmd(effect orchestrator.Effect) tea.Cmd {
	switch typed := effect.(type) {
	case orchestrator.InvalidateRenderEffect:
		m.clampFloatingPanesToViewport()
		m.render.Invalidate()
		return nil
	case orchestrator.ClosePaneEffect:
		m.clampFloatingPanesToViewport()
		m.render.Invalidate()
		return tea.Batch(m.resizeVisiblePanesCmd(), m.saveStateCmd())
	case orchestrator.CreateTabEffect:
		m.clampFloatingPanesToViewport()
		m.render.Invalidate()
		return m.saveStateCmd()
	case orchestrator.SwitchTabEffect:
		m.clampFloatingPanesToViewport()
		m.render.Invalidate()
		return m.resizeVisiblePanesCmd()
	case orchestrator.CloseTabEffect:
		m.clampFloatingPanesToViewport()
		m.render.Invalidate()
		return m.saveStateCmd()
	case orchestrator.KillTerminalEffect:
		return func() tea.Msg {
			client := m.runtime.Client()
			if client == nil {
				return nil
			}
			_ = client.Kill(context.Background(), typed.TerminalID)
			return nil
		}
	case orchestrator.SetInputModeEffect:
		return func() tea.Msg {
			m.input.SetMode(typed.Mode)
			m.render.Invalidate()
			return EffectAppliedMsg{Effect: typed}
		}
	case orchestrator.OpenPickerEffect:
		m.applyEffectSideState(typed)
		return func() tea.Msg {
			return EffectAppliedMsg{Effect: typed}
		}
	case orchestrator.OpenWorkspacePickerEffect:
		m.applyEffectSideState(typed)
		return func() tea.Msg {
			return EffectAppliedMsg{Effect: typed}
		}
	case orchestrator.CloseModalEffect:
		m.applyEffectSideState(typed)
		return func() tea.Msg {
			return EffectAppliedMsg{Effect: typed}
		}
	case orchestrator.LoadPickerItemsEffect:
		return func() tea.Msg {
			terminals, err := m.runtime.ListTerminals(context.Background())
			if err != nil && (m.runtime == nil || m.runtime.Registry() == nil || len(m.runtime.Registry().IDs()) == 0) {
				return err
			}
			if err != nil {
				terminals = nil
			}
			items := make([]modal.PickerItem, 0, len(terminals))
			for _, terminal := range terminals {
				if terminal.State == "exited" {
					continue
				}
				items = append(items, modal.PickerItem{
					TerminalID: terminal.ID,
					Name:       terminal.Name,
					State:      terminal.State,
				})
			}
			items = append(items, modal.PickerItem{
				CreateNew:   true,
				Name:        "new terminal",
				Description: "Create a new terminal",
			})
			return pickerItemsLoadedMsg{Items: items}
		}
	case orchestrator.LoadWorkspaceItemsEffect:
		return func() tea.Msg {
			if m.workbench == nil || m.modalHost == nil || m.modalHost.WorkspacePicker == nil {
				return nil
			}
			names := m.workbench.ListWorkspaces()
			items := make([]modal.WorkspacePickerItem, 0, len(names)+1)
			for _, name := range names {
				items = append(items, modal.WorkspacePickerItem{Name: name})
			}
			items = append(items, modal.WorkspacePickerItem{Name: "new workspace", CreateNew: true})
			m.modalHost.WorkspacePicker.Items = items
			m.modalHost.WorkspacePicker.ApplyFilter()
			requestID := ""
			if m.modalHost.Session != nil {
				requestID = m.modalHost.Session.RequestID
			}
			m.modalHost.MarkReady(input.ModeWorkspacePicker, requestID)
			m.render.Invalidate()
			return nil
		}
	case orchestrator.LoadSnapshotEffect:
		return func() tea.Msg {
			snapshot, err := m.runtime.LoadSnapshot(context.Background(), typed.TerminalID, typed.Offset, typed.Limit)
			if err != nil {
				return err
			}
			return orchestrator.SnapshotLoadedMsg{TerminalID: typed.TerminalID, Snapshot: snapshot}
		}
	case orchestrator.AttachTerminalEffect:
		return func() tea.Msg {
			msgs, err := m.orchestrator.AttachAndLoadSnapshot(context.Background(), typed.PaneID, typed.TerminalID, typed.Mode, 0, 200)
			if err != nil {
				return err
			}
			cmds := make([]tea.Cmd, 0, len(msgs))
			for _, msg := range msgs {
				value := msg
				cmds = append(cmds, func() tea.Msg { return value })
			}
			return tea.Batch(cmds...)()
		}
	default:
		return nil
	}
}

func (m *Model) enrichEffects(action input.SemanticAction, effects []orchestrator.Effect) []orchestrator.Effect {
	_ = action
	hasOpenPicker := false
	hasLoadPicker := false
	for _, effect := range effects {
		switch effect.(type) {
		case orchestrator.OpenPickerEffect:
			hasOpenPicker = true
		case orchestrator.LoadPickerItemsEffect:
			hasLoadPicker = true
		}
	}
	if hasOpenPicker && !hasLoadPicker {
		return append(effects, orchestrator.LoadPickerItemsEffect{})
	}
	return effects
}

func (m *Model) applyEffectSideState(effect orchestrator.Effect) {
	switch typed := effect.(type) {
	case orchestrator.OpenPickerEffect:
		if m.modalHost == nil {
			return
		}
		m.resetPickerState()
		m.modalHost.StartLoading(input.ModePicker, typed.RequestID)
		m.render.Invalidate()
	case orchestrator.OpenWorkspacePickerEffect:
		if m.modalHost == nil {
			return
		}
		if m.modalHost.WorkspacePicker == nil {
			m.modalHost.WorkspacePicker = &modal.WorkspacePickerState{}
		}
		m.modalHost.StartLoading(input.ModeWorkspacePicker, typed.RequestID)
		m.render.Invalidate()
	case orchestrator.CloseModalEffect:
		if m.modalHost == nil {
			return
		}
		requestID := ""
		if m.modalHost.Session != nil {
			requestID = m.modalHost.Session.RequestID
		}
		m.modalHost.Close(typed.Kind, requestID)
		m.render.Invalidate()
	case orchestrator.LoadPickerItemsEffect:
		if m.modalHost != nil && m.modalHost.Session != nil {
			m.modalHost.Session.Phase = modal.ModalPhaseLoading
			m.modalHost.Session.Loading = true
			m.render.Invalidate()
		}
	}
}

func (m *Model) resetPickerState() {
	if m == nil || m.modalHost == nil {
		return
	}
	if m.modalHost.Picker == nil {
		m.modalHost.Picker = &modal.PickerState{}
		return
	}
	m.modalHost.Picker.Title = ""
	m.modalHost.Picker.Footer = ""
	m.modalHost.Picker.Items = nil
	m.modalHost.Picker.Filtered = nil
	m.modalHost.Picker.Selected = 0
	m.modalHost.Picker.Query = ""
}
