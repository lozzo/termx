package app

import (
	"context"
	"sort"
	"strings"

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
	m.workbench.ClampFloatingPanesToBounds(m.bodyRect())
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
		return nil
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
		return func() tea.Msg {
			snapshot, err := m.runtime.LoadSnapshot(context.Background(), typed.TerminalID, typed.Offset, typed.Limit)
			if err != nil {
				return err
			}
			return orchestrator.SnapshotLoadedMsg{TerminalID: typed.TerminalID, Snapshot: snapshot}
		}
	case orchestrator.AttachTerminalEffect:
		return func() tea.Msg {
			msgs, err := m.orchestrator.AttachAndLoadSnapshot(context.Background(), typed.PaneID, typed.TerminalID, typed.Mode, 0, defaultTerminalSnapshotScrollbackLimit)
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
	m.modalHost.Picker.Footer = ""
	m.modalHost.Picker.Items = nil
	m.modalHost.Picker.Filtered = nil
	m.modalHost.Picker.Selected = 0
	m.modalHost.Picker.Query = ""
	m.modalHost.Picker.Cursor = 0
	m.modalHost.Picker.CursorSet = false
}

func (m *Model) workspacePickerItems() []modal.WorkspacePickerItem {
	if m == nil || m.workbench == nil {
		return nil
	}
	names := m.workbench.ListWorkspaces()
	items := make([]modal.WorkspacePickerItem, 0, len(names)*4)
	current := m.workbench.CurrentWorkspaceName()
	for _, name := range names {
		ws := m.workbench.WorkspaceByName(name)
		if ws == nil {
			continue
		}
		tabCount := 0
		paneCount := 0
		floatingCount := 0
		activeTabName := ""
		activePaneName := ""
		previewTerminalID := ""
		for index, tab := range ws.Tabs {
			if tab == nil {
				continue
			}
			tabCount++
			paneCount += len(tab.Panes)
			floatingCount += len(tab.Floating)
			if index == ws.ActiveTab {
				activeTabName = strings.TrimSpace(tab.Name)
				if pane := tab.Panes[tab.ActivePaneID]; pane != nil {
					activePaneName = strings.TrimSpace(pane.Title)
					previewTerminalID = strings.TrimSpace(pane.TerminalID)
					if activePaneName == "" {
						activePaneName = pane.ID
					}
				}
			}
		}
		items = append(items, modal.WorkspacePickerItem{
			Kind:           modal.WorkspacePickerItemWorkspace,
			Name:           name,
			WorkspaceName:  name,
			Current:        name == current,
			Active:         name == current,
			TabCount:       tabCount,
			PaneCount:      paneCount,
			FloatingCount:  floatingCount,
			ActiveTabName:  activeTabName,
			ActivePaneName: activePaneName,
			TerminalID:     previewTerminalID,
		})
		for index, tab := range ws.Tabs {
			if tab == nil {
				continue
			}
			activePaneName := ""
			terminalID := ""
			if pane := tab.Panes[tab.ActivePaneID]; pane != nil {
				activePaneName = strings.TrimSpace(pane.Title)
				terminalID = strings.TrimSpace(pane.TerminalID)
				if activePaneName == "" {
					activePaneName = pane.ID
				}
			}
			items = append(items, modal.WorkspacePickerItem{
				Kind:           modal.WorkspacePickerItemTab,
				Name:           strings.TrimSpace(tab.Name),
				WorkspaceName:  name,
				TabID:          tab.ID,
				TabIndex:       index,
				Depth:          1,
				Active:         name == current && index == ws.ActiveTab,
				PaneCount:      len(tab.Panes),
				FloatingCount:  len(tab.Floating),
				ActivePaneName: activePaneName,
				TerminalID:     terminalID,
			})
			for _, paneID := range workspacePickerPaneIDs(tab) {
				pane := tab.Panes[paneID]
				if pane == nil {
					continue
				}
				terminalState := "unconnected"
				title := strings.TrimSpace(pane.Title)
				role := ""
				floating := workspacePickerTabFloating(tab, paneID)
				if m.runtime != nil {
					if binding := m.runtime.Binding(paneID); binding != nil && strings.TrimSpace(string(binding.Role)) != "" {
						role = strings.TrimSpace(string(binding.Role))
					}
					if terminal := m.runtime.Registry().Get(strings.TrimSpace(pane.TerminalID)); terminal != nil {
						if strings.TrimSpace(terminal.Name) != "" && title == "" {
							title = strings.TrimSpace(terminal.Name)
						}
						if strings.TrimSpace(terminal.State) != "" {
							terminalState = strings.TrimSpace(terminal.State)
						}
					}
				} else if strings.TrimSpace(pane.TerminalID) != "" {
					terminalState = "attached"
				}
				if title == "" {
					title = pane.ID
				}
				items = append(items, modal.WorkspacePickerItem{
					Kind:          modal.WorkspacePickerItemPane,
					Name:          title,
					WorkspaceName: name,
					TabID:         tab.ID,
					TabIndex:      index,
					TabName:       strings.TrimSpace(tab.Name),
					PaneID:        pane.ID,
					Depth:         2,
					Active:        name == current && index == ws.ActiveTab && pane.ID == tab.ActivePaneID,
					Floating:      floating,
					TerminalID:    strings.TrimSpace(pane.TerminalID),
					State:         terminalState,
					Role:          role,
				})
			}
		}
	}
	return items
}

func workspacePickerPaneIDs(tab *workbench.TabState) []string {
	if tab == nil {
		return nil
	}
	order := make([]string, 0, len(tab.Panes))
	seen := make(map[string]struct{}, len(tab.Panes))
	if tab.Root != nil {
		for _, paneID := range tab.Root.LeafIDs() {
			if paneID == "" || tab.Panes[paneID] == nil {
				continue
			}
			order = append(order, paneID)
			seen[paneID] = struct{}{}
		}
	}
	if len(tab.Floating) > 0 {
		for _, floating := range tab.Floating {
			if floating == nil || floating.PaneID == "" || tab.Panes[floating.PaneID] == nil {
				continue
			}
			if _, ok := seen[floating.PaneID]; ok {
				continue
			}
			order = append(order, floating.PaneID)
			seen[floating.PaneID] = struct{}{}
		}
	}
	extras := make([]string, 0, len(tab.Panes))
	for paneID := range tab.Panes {
		if _, ok := seen[paneID]; ok {
			continue
		}
		extras = append(extras, paneID)
	}
	sort.Strings(extras)
	order = append(order, extras...)
	return order
}

func workspacePickerTabFloating(tab *workbench.TabState, paneID string) bool {
	if tab == nil || paneID == "" {
		return false
	}
	for _, floating := range tab.Floating {
		if floating != nil && floating.PaneID == paneID {
			return true
		}
	}
	return false
}
