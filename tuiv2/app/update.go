package app

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/bootstrap"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func (m *Model) Init() tea.Cmd {
	if err := m.bootstrapStartup(); err != nil {
		return func() tea.Msg { return err }
	}
	if m.cfg.AttachID != "" {
		return m.attachInitialTerminalCmd(m.cfg.AttachID)
	}
	if len(m.startup.PanesToReattach) > 0 {
		return m.reattachRestoredPanesCmd(m.startup.PanesToReattach)
	}
	// If startup opened a picker, immediately load the terminal list.
	if m.modalHost != nil && m.modalHost.Session != nil {
		return m.applyEffects([]orchestrator.Effect{orchestrator.LoadPickerItemsEffect{}})
	}
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		return m, m.handleKeyMsg(typed)
	case prefixTimeoutMsg:
		if typed.seq == m.prefixSeq && m.isStickyMode() {
			m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
		}
		return m, nil
	case SemanticActionMsg:
		if handled, cmd := m.handleLocalAction(typed.Action); handled {
			return m, cmd
		}
		if handled, cmd := m.handleModalAction(typed.Action); handled {
			return m, cmd
		}
		cmd := m.applyEffects(m.enrichEffects(typed.Action, m.orchestrator.HandleSemanticAction(typed.Action)))
		if m.isStickyMode() {
			cmd = tea.Batch(cmd, m.rearmPrefixTimeoutCmd())
		}
		return m, cmd
	case input.SemanticAction:
		if handled, cmd := m.handleLocalAction(typed); handled {
			return m, cmd
		}
		if handled, cmd := m.handleModalAction(typed); handled {
			return m, cmd
		}
		cmd := m.applyEffects(m.enrichEffects(typed, m.orchestrator.HandleSemanticAction(typed)))
		if m.isStickyMode() {
			cmd = tea.Batch(cmd, m.rearmPrefixTimeoutCmd())
		}
		return m, cmd
	case TerminalInputMsg:
		return m, m.handleTerminalInput(typed.Input)
	case input.TerminalInput:
		return m, m.handleTerminalInput(typed)
	case sequenceMsg:
		return m, m.nextSequenceCmd(typed)
	case pickerItemsLoadedMsg:
		if m.modalHost != nil {
			if m.modalHost.Picker == nil {
				m.modalHost.Picker = &modal.PickerState{}
			}
			m.modalHost.Picker.Items = typed.Items
			m.modalHost.Picker.ApplyFilter()
			if m.modalHost.Picker.Selected >= len(m.modalHost.Picker.VisibleItems()) {
				m.modalHost.Picker.Selected = 0
			}
			if m.modalHost.Session != nil {
				m.modalHost.MarkReady(m.modalHost.Session.Kind, m.modalHost.Session.RequestID)
			}
		}
		m.render.Invalidate()
		return m, nil
	case terminalManagerItemsLoadedMsg:
		if m.modalHost != nil {
			if m.modalHost.TerminalManager == nil {
				m.modalHost.TerminalManager = &modal.TerminalManagerState{}
			}
			m.modalHost.TerminalManager.Items = typed.Items
			m.modalHost.TerminalManager.ApplyFilter()
			if m.modalHost.Session != nil {
				m.modalHost.MarkReady(input.ModeTerminalManager, m.modalHost.Session.RequestID)
			}
		}
		m.render.Invalidate()
		return m, nil
	case EffectAppliedMsg:
		m.applyEffectSideState(typed.Effect)
		return m, nil
	case orchestrator.TerminalAttachedMsg:
		if m.workbench != nil {
			tabID := typed.TabID
			if tabID == "" {
				if tab := m.workbench.CurrentTab(); tab != nil {
					tabID = tab.ID
				}
			}
			if tabID != "" {
				_ = m.workbench.BindPaneTerminal(tabID, typed.PaneID, typed.TerminalID)
				_ = m.workbench.FocusPane(tabID, typed.PaneID)
			}
		}
		if m.modalHost != nil && m.modalHost.Session != nil && m.modalHost.Session.Kind == input.ModePicker {
			m.modalHost.Close(input.ModePicker, m.modalHost.Session.RequestID)
			m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
		}
		m.render.Invalidate()
		return m, tea.Batch(m.saveStateCmd(), m.resizeVisiblePanesCmd())
	case orchestrator.SnapshotLoadedMsg:
		m.render.Invalidate()
		return m, nil
	case hostDefaultColorsMsg:
		if m.runtime != nil {
			m.runtime.SetHostDefaultColors(typed.FG, typed.BG)
		}
		return m, nil
	case hostPaletteColorMsg:
		if m.runtime != nil {
			m.runtime.SetHostPaletteColor(typed.Index, typed.Color)
		}
		return m, nil
	case reattachFailedMsg:
		return m, m.openPickerIfUnattached(typed.paneID)
	case clearErrorMsg:
		m.err = nil
		m.render.Invalidate()
		return m, nil
	case InvalidateMsg:
		return m, nil
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
		m.render.Invalidate()
		return m, m.resizeVisiblePanesCmd()
	case error:
		m.err = typed
		m.render.Invalidate()
		return m, clearErrorCmd()
	default:
		return m, nil
	}
}

func (m *Model) handleKeyMsg(msg tea.KeyMsg) tea.Cmd {
	if handled, cmd := m.handleModalKeyMsg(msg); handled {
		return cmd
	}
	result := m.input.RouteKeyMsg(msg)
	if result.Action != nil {
		action := *result.Action
		if m.modalHost != nil && m.modalHost.Session != nil && m.modalHost.Session.Kind == input.ModePicker && m.modalHost.Picker != nil {
			if selected := m.modalHost.Picker.SelectedItem(); selected != nil && action.Kind == input.ActionSubmitPrompt {
				action.TargetID = selected.TerminalID
			}
		}
		if action.PaneID == "" && m.workbench != nil {
			if pane := m.workbench.ActivePane(); pane != nil {
				action.PaneID = pane.ID
			}
		}
		return func() tea.Msg { return action }
	}
	if result.TerminalInput != nil {
		inputMsg := *result.TerminalInput
		if inputMsg.PaneID == "" && m.workbench != nil {
			if pane := m.workbench.ActivePane(); pane != nil {
				inputMsg.PaneID = pane.ID
			}
		}
		return func() tea.Msg { return inputMsg }
	}
	return nil
}

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

func (m *Model) effectCmd(effect orchestrator.Effect) tea.Cmd {
	switch typed := effect.(type) {
	case orchestrator.InvalidateRenderEffect:
		m.render.Invalidate()
		return nil
	case orchestrator.ClosePaneEffect:
		m.render.Invalidate()
		return tea.Batch(m.resizeVisiblePanesCmd(), m.saveStateCmd())
	case orchestrator.CreateTabEffect:
		m.render.Invalidate()
		return m.saveStateCmd()
	case orchestrator.SwitchTabEffect:
		m.render.Invalidate()
		return m.resizeVisiblePanesCmd()
	case orchestrator.CloseTabEffect:
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
			return EffectAppliedMsg{Effect: typed}
		}
	case orchestrator.OpenPickerEffect:
		if m.modalHost != nil && m.modalHost.Picker == nil {
			m.modalHost.Picker = &modal.PickerState{}
		}
		return func() tea.Msg {
			return EffectAppliedMsg{Effect: typed}
		}
	case orchestrator.OpenWorkspacePickerEffect:
		if m.modalHost != nil && m.modalHost.WorkspacePicker == nil {
			m.modalHost.WorkspacePicker = &modal.WorkspacePickerState{}
		}
		return func() tea.Msg {
			return EffectAppliedMsg{Effect: typed}
		}
	case orchestrator.LoadPickerItemsEffect:
		return func() tea.Msg {
			terminals, err := m.runtime.ListTerminals(context.Background())
			if err != nil {
				return err
			}
			items := make([]modal.PickerItem, 0, len(terminals))
			for _, terminal := range terminals {
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
	if action.Kind != input.ActionOpenPicker {
		return effects
	}
	return append(effects, orchestrator.LoadPickerItemsEffect{})
}

func (m *Model) applyEffectSideState(effect orchestrator.Effect) {
	switch typed := effect.(type) {
	case orchestrator.OpenPickerEffect:
		if m.modalHost == nil {
			return
		}
		if m.modalHost.Picker == nil {
			m.modalHost.Picker = &modal.PickerState{}
		}
		m.modalHost.StartLoading(input.ModePicker, typed.RequestID)
	case orchestrator.OpenWorkspacePickerEffect:
		if m.modalHost == nil {
			return
		}
		if m.modalHost.WorkspacePicker == nil {
			m.modalHost.WorkspacePicker = &modal.WorkspacePickerState{}
		}
		m.modalHost.StartLoading(input.ModeWorkspacePicker, typed.RequestID)
	case orchestrator.LoadPickerItemsEffect:
		if m.modalHost != nil && m.modalHost.Session != nil {
			m.modalHost.Session.Phase = modal.ModalPhaseLoading
			m.modalHost.Session.Loading = true
		}
	}
}

func (m *Model) handleModalAction(action input.SemanticAction) (bool, tea.Cmd) {
	if m == nil || m.modalHost == nil || m.modalHost.Session == nil {
		return false, nil
	}
	switch m.modalHost.Session.Kind {
	case input.ModePicker:
		if m.modalHost.Picker == nil {
			return false, nil
		}
		switch action.Kind {
		case input.ActionPickerUp:
			m.modalHost.Picker.Move(-1)
			m.render.Invalidate()
			return true, nil
		case input.ActionPickerDown:
			m.modalHost.Picker.Move(1)
			m.render.Invalidate()
			return true, nil
		case input.ActionCancelMode:
			m.modalHost.Close(input.ModePicker, m.modalHost.Session.RequestID)
			m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
			return true, nil
		case input.ActionSubmitPrompt:
			if selected := m.modalHost.Picker.SelectedItem(); selected != nil && selected.CreateNew {
				m.openCreateTerminalPrompt(action.PaneID)
				return true, nil
			}
			return false, nil
		case input.ActionKillTerminal:
			if selected := m.modalHost.Picker.SelectedItem(); selected != nil && !selected.CreateNew {
				terminalID := selected.TerminalID
				items := m.modalHost.Picker.Items
				filtered := items[:0]
				for _, item := range items {
					if item.TerminalID != terminalID {
						filtered = append(filtered, item)
					}
				}
				m.modalHost.Picker.Items = filtered
				m.modalHost.Picker.ApplyFilter()
				m.render.Invalidate()
				return true, func() tea.Msg {
					return orchestrator.KillTerminalEffect{TerminalID: terminalID}
				}
			}
			return true, nil
		default:
			return false, nil
		}
	case input.ModePrompt:
		switch action.Kind {
		case input.ActionCancelMode:
			m.modalHost.Close(input.ModePrompt, m.modalHost.Session.RequestID)
			m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
			return true, nil
		case input.ActionSubmitPrompt:
			return true, m.submitPromptCmd(action.PaneID)
		default:
			return false, nil
		}
	case input.ModeWorkspacePicker:
		if m.modalHost.WorkspacePicker == nil {
			return false, nil
		}
		switch action.Kind {
		case input.ActionPickerUp:
			m.modalHost.WorkspacePicker.Move(-1)
			m.render.Invalidate()
			return true, nil
		case input.ActionPickerDown:
			m.modalHost.WorkspacePicker.Move(1)
			m.render.Invalidate()
			return true, nil
		case input.ActionCancelMode:
			m.modalHost.Close(input.ModeWorkspacePicker, m.modalHost.Session.RequestID)
			m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
			return true, nil
		case input.ActionSubmitPrompt:
			if selected := m.modalHost.WorkspacePicker.SelectedItem(); selected != nil {
				if selected.CreateNew {
					return true, nil
				}
				return true, func() tea.Msg {
					return input.SemanticAction{Kind: input.ActionSwitchWorkspace, Text: selected.Name}
				}
			}
			return true, nil
		default:
			return false, nil
		}
	case input.ModeHelp:
		switch action.Kind {
		case input.ActionCancelMode:
			m.modalHost.Close(input.ModeHelp, m.modalHost.Session.RequestID)
			m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
			return true, nil
		default:
			return false, nil
		}
	case input.ModeTerminalManager:
		if m.modalHost.TerminalManager == nil {
			return false, nil
		}
		switch action.Kind {
		case input.ActionPickerUp:
			m.modalHost.TerminalManager.Move(-1)
			m.render.Invalidate()
			return true, nil
		case input.ActionPickerDown:
			m.modalHost.TerminalManager.Move(1)
			m.render.Invalidate()
			return true, nil
		case input.ActionCancelMode:
			m.modalHost.Close(input.ModeTerminalManager, m.modalHost.Session.RequestID)
			m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
			return true, nil
		case input.ActionSubmitPrompt:
			if selected := m.modalHost.TerminalManager.SelectedItem(); selected != nil && !selected.CreateNew {
				return true, func() tea.Msg {
					return input.SemanticAction{Kind: input.ActionOpenPicker, TargetID: selected.TerminalID, PaneID: action.PaneID}
				}
			}
			return true, nil
		case input.ActionKillTerminal:
			if selected := m.modalHost.TerminalManager.SelectedItem(); selected != nil && !selected.CreateNew {
				terminalID := selected.TerminalID
				items := m.modalHost.TerminalManager.Items
				filtered := items[:0]
				for _, item := range items {
					if item.TerminalID != terminalID {
						filtered = append(filtered, item)
					}
				}
				m.modalHost.TerminalManager.Items = filtered
				m.modalHost.TerminalManager.ApplyFilter()
				m.render.Invalidate()
				return true, func() tea.Msg {
					return orchestrator.KillTerminalEffect{TerminalID: terminalID}
				}
			}
			return true, nil
		default:
			return false, nil
		}
	default:
		if action.Kind == input.ActionCancelMode {
			m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
			return true, nil
		}
		return false, nil
	}
}

func (m *Model) handleLocalAction(action input.SemanticAction) (bool, tea.Cmd) {
	if m == nil || m.modalHost == nil {
		return false, nil
	}
	switch action.Kind {
	case input.ActionEnterPaneMode:
		m.input.SetMode(input.ModeState{Kind: input.ModePane})
		m.render.Invalidate()
		return true, m.rearmPrefixTimeoutCmd()
	case input.ActionEnterResizeMode:
		m.input.SetMode(input.ModeState{Kind: input.ModeResize})
		m.render.Invalidate()
		return true, m.rearmPrefixTimeoutCmd()
	case input.ActionEnterTabMode:
		m.input.SetMode(input.ModeState{Kind: input.ModeTab})
		m.render.Invalidate()
		return true, m.rearmPrefixTimeoutCmd()
	case input.ActionEnterWorkspaceMode:
		m.input.SetMode(input.ModeState{Kind: input.ModeWorkspace})
		m.render.Invalidate()
		return true, nil
	case input.ActionEnterFloatingMode:
		m.input.SetMode(input.ModeState{Kind: input.ModeFloating})
		m.render.Invalidate()
		return true, nil
	case input.ActionEnterDisplayMode:
		m.input.SetMode(input.ModeState{Kind: input.ModeDisplay})
		m.render.Invalidate()
		return true, m.rearmPrefixTimeoutCmd()
	case input.ActionEnterGlobalMode:
		m.input.SetMode(input.ModeState{Kind: input.ModeGlobal})
		m.render.Invalidate()
		return true, nil
	case input.ActionCancelMode:
		if m.modalHost == nil || m.modalHost.Session == nil {
			m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
			return true, nil
		}
		return false, nil
	case input.ActionOpenHelp:
		m.modalHost.Open(input.ModeHelp, "help")
		m.modalHost.Help = modal.DefaultHelp()
		m.modalHost.MarkReady(input.ModeHelp, "help")
		m.input.SetMode(input.ModeState{Kind: input.ModeHelp, RequestID: "help"})
		m.render.Invalidate()
		return true, nil
	case input.ActionFocusPane:
		if m.input.Mode().Kind == input.ModeNormal {
			m.input.SetMode(input.ModeState{Kind: input.ModePane})
			m.render.Invalidate()
			return true, nil
		}
		return false, nil
	case input.ActionOpenPrompt:
		if m.input.Mode().Kind == input.ModeNormal {
			m.input.SetMode(input.ModeState{Kind: input.ModeResize})
			m.render.Invalidate()
			return true, nil
		}
		return false, nil
	case input.ActionCreateTab:
		if m.input.Mode().Kind == input.ModeNormal {
			m.input.SetMode(input.ModeState{Kind: input.ModeTab})
			m.render.Invalidate()
			return true, nil
		}
		return false, nil
	case input.ActionOpenWorkspacePicker:
		if m.input.Mode().Kind == input.ModeNormal {
			m.input.SetMode(input.ModeState{Kind: input.ModeWorkspace})
			m.render.Invalidate()
			return true, nil
		}
		return false, nil
	case input.ActionOpenTerminalManager:
		if m.input.Mode().Kind == input.ModeGlobal {
			requestID := "terminal-manager"
			m.modalHost.Open(input.ModeTerminalManager, requestID)
			if m.modalHost.TerminalManager == nil {
				m.modalHost.TerminalManager = &modal.TerminalManagerState{}
			}
			m.modalHost.TerminalManager.Title = "Terminal Manager"
			m.modalHost.TerminalManager.Footer = "[Enter] attach  [Ctrl-K] kill  [Esc] close"
			m.input.SetMode(input.ModeState{Kind: input.ModeTerminalManager, RequestID: requestID})
			m.render.Invalidate()
			return true, func() tea.Msg {
				terminals, err := m.runtime.ListTerminals(context.Background())
				if err != nil {
					return err
				}
				items := make([]modal.PickerItem, 0, len(terminals))
				for _, terminal := range terminals {
					items = append(items, modal.PickerItem{TerminalID: terminal.ID, Name: terminal.Name, State: terminal.State})
				}
				return terminalManagerItemsLoadedMsg{Items: items}
			}
		}
		return false, nil
	case input.ActionZoomPane:
		if m.input.Mode().Kind == input.ModeNormal {
			m.input.SetMode(input.ModeState{Kind: input.ModeDisplay})
			m.render.Invalidate()
			return true, nil
		}
		if m.workbench != nil {
			if tab := m.workbench.CurrentTab(); tab != nil {
				paneID := action.PaneID
				if paneID == "" {
					paneID = tab.ActivePaneID
				}
				if tab.ZoomedPaneID == paneID {
					tab.ZoomedPaneID = ""
				} else {
					tab.ZoomedPaneID = paneID
				}
				m.render.Invalidate()
			}
		}
		return true, nil
	case input.ActionScrollUp:
		if tab := m.workbench.CurrentTab(); tab != nil {
			tab.ScrollOffset += 1
			m.render.Invalidate()
		}
		return true, nil
	case input.ActionScrollDown:
		if tab := m.workbench.CurrentTab(); tab != nil {
			if tab.ScrollOffset > 0 {
				tab.ScrollOffset -= 1
			}
			m.render.Invalidate()
		}
		return true, nil
	case input.ActionQuit:
		if m.input.Mode().Kind == input.ModeNormal {
			m.input.SetMode(input.ModeState{Kind: input.ModeGlobal})
			m.render.Invalidate()
			return true, nil
		}
		m.quitting = true
		return true, tea.Batch(m.saveStateCmd(), tea.Quit)
	default:
		return false, nil
	}
}

const prefixModeTimeout = 1500 * time.Millisecond

func (m *Model) isStickyMode() bool {
	kind := m.input.Mode().Kind
	return kind == input.ModePane || kind == input.ModeResize || kind == input.ModeTab || kind == input.ModeDisplay
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
	// If the active pane has no terminal bound, open the picker instead of
	// sending input (which would produce a confusing "not attached" error).
	if m.workbench != nil {
		if pane := m.workbench.ActivePane(); pane != nil && pane.TerminalID == "" {
			return m.openPickerIfUnattached(pane.ID)
		}
	}
	return func() tea.Msg {
		if err := m.runtime.SendInput(context.Background(), in.PaneID, in.Data); err != nil {
			return err
		}
		return nil
	}
}

// openPickerIfUnattached opens the terminal picker for paneID when that pane
// is the current active pane and has no terminal bound. Safe to call redundantly.
func (m *Model) openPickerIfUnattached(paneID string) tea.Cmd {
	if m == nil || m.workbench == nil || m.modalHost == nil {
		return nil
	}
	// Only act if the pane is the current active pane and still unbound.
	pane := m.workbench.ActivePane()
	if pane == nil || pane.ID != paneID || pane.TerminalID != "" {
		return nil
	}
	// Don't open a second picker if one is already active.
	if m.modalHost.Session != nil {
		return nil
	}
	m.modalHost.Open(input.ModePicker, paneID)
	if m.modalHost.Picker == nil {
		m.modalHost.Picker = &modal.PickerState{}
	}
	m.input.SetMode(input.ModeState{Kind: input.ModePicker, RequestID: paneID})
	m.render.Invalidate()
	return m.applyEffects([]orchestrator.Effect{orchestrator.LoadPickerItemsEffect{}})
}

func (m *Model) splitPaneAndAttachTerminalCmd(paneID, terminalID string) tea.Cmd {
	if m == nil || m.workbench == nil || paneID == "" || terminalID == "" {
		return nil
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}
	newPaneID := "pane-" + shared.GenerateShortID()
	if err := m.workbench.SplitPane(tab.ID, paneID, newPaneID, workbench.SplitVertical); err != nil {
		return func() tea.Msg { return err }
	}
	_ = m.workbench.FocusPane(tab.ID, newPaneID)
	m.render.Invalidate()
	return tea.Batch(m.attachPaneTerminalCmd("", newPaneID, terminalID), m.saveStateCmd())
}

func (m *Model) createTabAndAttachTerminalCmd(terminalID string) tea.Cmd {
	if m == nil || m.workbench == nil || terminalID == "" {
		return nil
	}
	ws := m.workbench.CurrentWorkspace()
	if ws == nil {
		return nil
	}
	tabID := "tab-" + shared.GenerateShortID()
	paneID := "pane-" + shared.GenerateShortID()
	name := strconv.Itoa(len(ws.Tabs) + 1)
	if err := m.workbench.CreateTab(ws.Name, tabID, name); err != nil {
		return func() tea.Msg { return err }
	}
	if err := m.workbench.CreateFirstPane(tabID, paneID); err != nil {
		return func() tea.Msg { return err }
	}
	_ = m.workbench.SwitchTab(ws.Name, len(ws.Tabs)-1)
	m.render.Invalidate()
	return tea.Batch(m.attachPaneTerminalCmd("", paneID, terminalID), m.saveStateCmd())
}

func (m *Model) createFloatingPaneAndAttachTerminalCmd(terminalID string) tea.Cmd {
	if m == nil || m.workbench == nil || terminalID == "" {
		return nil
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}
	paneID := "pane-" + shared.GenerateShortID()
	if err := m.workbench.CreateFloatingPane(tab.ID, paneID, workbench.Rect{X: 10, Y: 5, W: 80, H: 24}); err != nil {
		return func() tea.Msg { return err }
	}
	_ = m.workbench.FocusPane(tab.ID, paneID)
	m.render.Invalidate()
	return tea.Batch(m.attachPaneTerminalCmd("", paneID, terminalID), m.saveStateCmd())
}

func (m *Model) nextSequenceCmd(seq sequenceMsg) tea.Cmd {
	if len(seq) == 0 {
		return nil
	}
	return func() tea.Msg {
		return seq[0]
	}
}

func (m *Model) attachInitialTerminalCmd(terminalID string) tea.Cmd {
	if m == nil || m.workbench == nil || m.orchestrator == nil || terminalID == "" {
		return nil
	}
	pane := m.workbench.ActivePane()
	if pane == nil || pane.ID == "" {
		return nil
	}
	if m.modalHost != nil && m.modalHost.Session != nil && m.modalHost.Session.Kind == input.ModePicker {
		m.modalHost.Close(input.ModePicker, m.modalHost.Session.RequestID)
		m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
	}
	paneID := pane.ID
	return m.attachPaneTerminalCmd("", paneID, terminalID)
}

func (m *Model) attachPaneTerminalCmd(tabID, paneID, terminalID string) tea.Cmd {
	if m == nil || m.orchestrator == nil || paneID == "" || terminalID == "" {
		return nil
	}
	return func() tea.Msg {
		msgs, err := m.orchestrator.AttachAndLoadSnapshot(context.Background(), paneID, terminalID, "collaborator", 0, 200)
		if err != nil {
			return err
		}
		for index := range msgs {
			if attached, ok := msgs[index].(orchestrator.TerminalAttachedMsg); ok {
				attached.TabID = tabID
				msgs[index] = attached
			}
		}
		cmds := make([]tea.Cmd, 0, len(msgs))
		for _, msg := range msgs {
			value := msg
			cmds = append(cmds, func() tea.Msg { return value })
		}
		return tea.Batch(cmds...)()
	}
}

func (m *Model) reattachRestoredPanesCmd(hints []bootstrap.PaneReattachHint) tea.Cmd {
	if m == nil || len(hints) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(hints))
	for _, hint := range hints {
		h := hint
		cmds = append(cmds, func() tea.Msg {
			cmd := m.attachPaneTerminalCmd(h.TabID, h.PaneID, h.TerminalID)
			if cmd == nil {
				return reattachFailedMsg{tabID: h.TabID, paneID: h.PaneID}
			}
			msg := cmd()
			if _, ok := msg.(error); ok {
				if m.workbench != nil && h.TabID != "" {
					_ = m.workbench.BindPaneTerminal(h.TabID, h.PaneID, "")
				}
				return reattachFailedMsg{tabID: h.TabID, paneID: h.PaneID}
			}
			return msg
		})
	}
	return tea.Batch(cmds...)
}

type sequenceMsg []any

func (m *Model) resizeVisiblePanesCmd() tea.Cmd {
	if m == nil || m.runtime == nil || m.workbench == nil {
		return nil
	}
	bodyRect := workbench.Rect{W: maxInt(1, m.width), H: maxInt(1, m.height-2)}
	visible := m.workbench.VisibleWithSize(bodyRect)
	if visible == nil || visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		return nil
	}
	tab := visible.Tabs[visible.ActiveTab]
	cmds := make([]tea.Cmd, 0, len(tab.Panes))
	for _, pane := range tab.Panes {
		if pane.ID == "" || pane.TerminalID == "" {
			continue
		}
		cols := uint16(maxInt(2, pane.Rect.W-2))
		rows := uint16(maxInt(2, pane.Rect.H-2))
		paneID := pane.ID
		cmds = append(cmds, func() tea.Msg {
			if err := m.runtime.ResizeTerminal(context.Background(), paneID, cols, rows); err != nil {
				return err
			}
			return nil
		})
	}
	return tea.Batch(cmds...)
}

func (m *Model) handleModalKeyMsg(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m == nil || m.modalHost == nil || m.modalHost.Session == nil {
		return false, nil
	}
	if m.modalHost.Session.Kind != input.ModePrompt || m.modalHost.Prompt == nil {
		return false, nil
	}
	switch msg.Type {
	case tea.KeyRunes:
		if len(msg.Runes) > 0 {
			m.modalHost.Prompt.Value += string(msg.Runes)
			m.render.Invalidate()
		}
		return true, nil
	case tea.KeyBackspace:
		if value := m.modalHost.Prompt.Value; value != "" {
			_, size := utf8.DecodeLastRuneInString(value)
			if size > 0 {
				m.modalHost.Prompt.Value = value[:len(value)-size]
			} else {
				m.modalHost.Prompt.Value = ""
			}
			m.render.Invalidate()
		}
		return true, nil
	case tea.KeyEnter:
		return true, func() tea.Msg {
			return input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: m.modalHost.Prompt.PaneID}
		}
	case tea.KeyEsc:
		return true, func() tea.Msg { return input.SemanticAction{Kind: input.ActionCancelMode} }
	default:
		return false, nil
	}
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func (m *Model) openEditTerminalPrompt(terminalID string, currentName string) {
	if m == nil || m.modalHost == nil || terminalID == "" {
		return
	}
	requestID := "edit-terminal:" + terminalID
	m.modalHost.Session = &modal.ModalSession{Kind: input.ModePrompt, Phase: modal.ModalPhaseReady, RequestID: requestID}
	m.modalHost.Prompt = &modal.PromptState{
		Kind:        "edit-terminal-name",
		Title:       "Edit Terminal",
		Hint:        "[Enter] save  [Esc] cancel",
		Value:       currentName,
		Original:    currentName,
		DefaultName: currentName,
		PaneID:      terminalID,
	}
	m.input.SetMode(input.ModeState{Kind: input.ModePrompt, RequestID: requestID})
	m.render.Invalidate()
}

func (m *Model) openCreateTerminalPrompt(paneID string) {
	if m == nil || m.modalHost == nil {
		return
	}
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		shell = "/bin/sh"
	}
	defaultName := filepath.Base(shell)
	requestID := "create-terminal:" + paneID
	m.modalHost.Session = &modal.ModalSession{Kind: input.ModePrompt, Phase: modal.ModalPhaseReady, RequestID: requestID}
	m.modalHost.Prompt = &modal.PromptState{
		Kind:        "create-terminal-name",
		Title:       "Create Terminal",
		Hint:        "[Enter] continue  [Esc] cancel",
		Original:    defaultName,
		DefaultName: defaultName,
		PaneID:      paneID,
		Command:     []string{shell},
	}
	m.input.SetMode(input.ModeState{Kind: input.ModePrompt, RequestID: requestID})
	m.render.Invalidate()
}

func (m *Model) submitPromptCmd(paneID string) tea.Cmd {
	if m == nil || m.modalHost == nil || m.modalHost.Prompt == nil {
		return nil
	}
	prompt := m.modalHost.Prompt
	switch prompt.Kind {
	case "create-terminal-name":
		name := strings.TrimSpace(prompt.Value)
		if name == "" {
			name = strings.TrimSpace(prompt.Original)
		}
		prompt.Kind = "create-terminal-tags"
		prompt.Title = "Create Terminal"
		prompt.Hint = "[Enter] create  [Esc] cancel"
		prompt.AllowEmpty = true
		prompt.Name = name
		prompt.Value = ""
		m.render.Invalidate()
		return nil
	case "edit-terminal-name":
		name := strings.TrimSpace(prompt.Value)
		if name == "" {
			name = strings.TrimSpace(prompt.Original)
		}
		terminalID := prompt.PaneID
		requestID := m.modalHost.Session.RequestID
		m.modalHost.Close(input.ModePrompt, requestID)
		m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return func() tea.Msg {
			client := m.runtime.Client()
			if client == nil {
				return context.Canceled
			}
			registry := m.runtime.Registry()
			var tags map[string]string
			if registry != nil {
				if terminal := registry.Get(terminalID); terminal != nil {
					tags = cloneStringMap(terminal.Tags)
				}
			}
			if err := client.SetMetadata(context.Background(), terminalID, name, tags); err != nil {
				return err
			}
			if registry != nil {
				if terminal := registry.Get(terminalID); terminal != nil {
					terminal.Name = name
				}
			}
			return nil
		}
	case "create-terminal-tags":
		tags, err := parsePromptTags(prompt.Value)
		if err != nil {
			return func() tea.Msg { return err }
		}
		name := strings.TrimSpace(prompt.Name)
		if name == "" {
			name = strings.TrimSpace(prompt.DefaultName)
		}
		pane := paneID
		if pane == "" {
			pane = prompt.PaneID
		}
		command := append([]string(nil), prompt.Command...)
		if len(command) == 0 {
			command = []string{"/bin/sh"}
		}
		requestID := m.modalHost.Session.RequestID
		m.modalHost.Close(input.ModePrompt, requestID)
		m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return func() tea.Msg {
			client := m.runtime.Client()
			if client == nil {
				return context.Canceled
			}
			created, err := client.Create(context.Background(), command, name, protocol.Size{Cols: 80, Rows: 24})
			if err != nil {
				return err
			}
			if len(tags) > 0 {
				if err := client.SetTags(context.Background(), created.TerminalID, tags); err != nil {
					return err
				}
			}
			msgs, err := m.orchestrator.AttachAndLoadSnapshot(context.Background(), pane, created.TerminalID, "collaborator", 0, 200)
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

func parsePromptTags(value string) (map[string]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\t' || r == ' '
	})
	tags := make(map[string]string, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		key, rawValue, ok := strings.Cut(part, "=")
		key = strings.TrimSpace(key)
		rawValue = strings.TrimSpace(rawValue)
		if !ok || key == "" {
			return nil, inputError("invalid tag syntax: " + part)
		}
		tags[key] = rawValue
	}
	return tags, nil
}

type inputError string

func (e inputError) Error() string { return string(e) }
