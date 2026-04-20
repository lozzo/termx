package app

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
)

type effectApplyOptions struct {
	deferInvalidate bool
}

func (m *Model) applyEffects(effects []orchestrator.Effect) tea.Cmd {
	return m.applyEffectsWithOptions(effects, effectApplyOptions{})
}

func (m *Model) applyEffectsWithOptions(effects []orchestrator.Effect, options effectApplyOptions) tea.Cmd {
	if len(effects) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(effects))
	for _, effect := range effects {
		if cmd := m.effectCmdWithOptions(effect, options); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

func (m *Model) clampFloatingPanesToViewport() {
	if m == nil || m.workbench == nil || m.width <= 0 || m.height <= 0 {
		return
	}
	m.workbench.ClampFloatingPanesToBounds(m.bodyRect())
}

func (m *Model) effectCmd(effect orchestrator.Effect) tea.Cmd {
	return m.effectCmdWithOptions(effect, effectApplyOptions{})
}

func (m *Model) effectCmdWithOptions(effect orchestrator.Effect, options effectApplyOptions) tea.Cmd {
	switch typed := effect.(type) {
	case orchestrator.InvalidateRenderEffect:
		m.clampFloatingPanesToViewport()
		if !options.deferInvalidate {
			m.render.Invalidate()
		}
		return nil
	case orchestrator.ClosePaneEffect:
		if service := m.paneBindingLifecycleService(); service != nil {
			if _, err := service.close(typed.PaneID); err != nil {
				return m.showError(err)
			}
		}
		m.clampFloatingPanesToViewport()
		if m.modalHost != nil && m.modalHost.Session != nil && m.modalHost.Session.Kind == input.ModeFloatingOverview {
			m.refreshFloatingOverview("")
			if m.modalHost.FloatingOverview != nil && len(m.modalHost.FloatingOverview.Items) == 0 {
				m.closeFloatingOverview()
			}
		}
		m.render.Invalidate()
		return tea.Batch(m.resizeVisiblePanesCmd(), m.saveStateCmd())
	case orchestrator.DetachPaneEffect:
		if service := m.paneBindingLifecycleService(); service != nil {
			if _, err := service.detach(typed.PaneID); err != nil {
				return m.showError(err)
			}
		}
		m.render.Invalidate()
		return nil
	case orchestrator.ReconnectPaneEffect:
		if service := m.paneBindingLifecycleService(); service != nil {
			if _, err := service.reconnect(typed.PaneID); err != nil {
				return m.showError(err)
			}
		}
		m.render.Invalidate()
		return nil
	case orchestrator.ResizePaneLayoutEffect:
		if service := m.layoutResizeService(); service != nil {
			_, cmd := service.adjustPaneRatioAction(input.SemanticAction{Kind: typed.Kind, PaneID: typed.PaneID}, typed.Delta)
			return cmd
		}
		return nil
	case orchestrator.BalancePanesEffect:
		if service := m.layoutResizeService(); service != nil {
			_, cmd := service.balancePanesAction()
			return cmd
		}
		return nil
	case orchestrator.CycleLayoutEffect:
		if service := m.layoutResizeService(); service != nil {
			_, cmd := service.cycleLayoutAction()
			return cmd
		}
		return nil
	case orchestrator.MoveFloatingPaneEffect:
		if service := m.layoutResizeService(); service != nil {
			_, cmd := service.moveFloatingAction(input.SemanticAction{Kind: typed.Kind, PaneID: typed.PaneID})
			return cmd
		}
		return nil
	case orchestrator.ResizeFloatingPaneEffect:
		if service := m.layoutResizeService(); service != nil {
			_, cmd := service.resizeFloatingAction(input.SemanticAction{Kind: typed.Kind, PaneID: typed.PaneID})
			return cmd
		}
		return nil
	case orchestrator.CenterFloatingPaneEffect:
		if service := m.layoutResizeService(); service != nil {
			_, cmd := service.centerFloatingAction(input.SemanticAction{Kind: input.ActionCenterFloatingPane, PaneID: typed.PaneID})
			return cmd
		}
		return nil
	case orchestrator.CreateTabEffect:
		m.clampFloatingPanesToViewport()
		m.render.Invalidate()
		return m.saveStateCmd()
	case orchestrator.SwitchTabEffect:
		m.clampFloatingPanesToViewport()
		m.render.Invalidate()
		return nil
	case orchestrator.CloseTabEffect:
		if service := m.tabLifecycleService(); service != nil {
			return service.closeAndSaveCmd(typed.TabID, false)
		}
		return nil
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
			m.setMode(typed.Mode)
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
				items = append(items, modal.PickerItem{
					TerminalID:    terminal.ID,
					Name:          terminal.Name,
					State:         terminal.State,
					TerminalState: terminal.State,
					ExitCode:      cloneIntPointer(terminal.ExitCode),
					Command:       strings.Join(terminal.Command, " "),
					CommandArgs:   append([]string(nil), terminal.Command...),
					Tags:          cloneStringMap(terminal.Tags),
					CreatedAt:     terminal.CreatedAt,
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
			m.modalHost.WorkspacePicker.Items = m.workspacePickerItems()
			m.modalHost.WorkspacePicker.ApplyFilter()
			requestID := ""
			if m.modalHost.Session != nil {
				requestID = m.modalHost.Session.RequestID
			}
			m.markModalReady(input.ModeWorkspacePicker, requestID)
			m.render.Invalidate()
			return nil
		}
	case orchestrator.LoadSnapshotEffect:
		if service := m.terminalBindingService(); service != nil {
			return service.loadSnapshotCmd(typed.TerminalID, typed.Offset, typed.Limit)
		}
		return nil
	case orchestrator.AttachTerminalEffect:
		if service := m.terminalAttachService(); service != nil {
			return service.attachWithModeCmd("", typed.PaneID, typed.TerminalID, typed.Mode)
		}
		return nil
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
		m.startLoadingModal(input.ModePicker, typed.RequestID)
		m.render.Invalidate()
	case orchestrator.OpenWorkspacePickerEffect:
		if m.modalHost == nil {
			return
		}
		if m.modalHost.WorkspacePicker == nil {
			m.modalHost.WorkspacePicker = &modal.WorkspacePickerState{}
		}
		m.startLoadingModal(input.ModeWorkspacePicker, typed.RequestID)
		m.render.Invalidate()
	case orchestrator.CloseModalEffect:
		if m.modalHost == nil {
			return
		}
		requestID := ""
		if m.modalHost.Session != nil {
			requestID = m.modalHost.Session.RequestID
		}
		m.closeModal(typed.Kind, requestID, input.ModeState{Kind: input.ModeNormal})
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
	m.modalHost.Picker.Items = nil
	m.modalHost.Picker.Filtered = nil
	m.modalHost.Picker.Selected = 0
	m.modalHost.Picker.Query = ""
	m.modalHost.Picker.Cursor = 0
	m.modalHost.Picker.CursorSet = false
	m.modalHost.Picker.QueryInput.Clear()
}
