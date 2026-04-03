package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	case tea.MouseMsg:
		return m, m.handleMouseMsg(typed)
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
		if m.terminalPage == nil {
			m.terminalPage = &modal.TerminalManagerState{}
		}
		m.terminalPage.Items = typed.Items
		m.terminalPage.ApplyFilter()
		m.render.Invalidate()
		return m, nil
	case orchestrator.KillTerminalEffect:
		return m, m.effectCmd(typed)
	case EffectAppliedMsg:
		m.applyEffectSideState(typed.Effect)
		return m, nil
	case orchestrator.TerminalAttachedMsg:
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
		if typed.seq != m.errorSeq {
			return m, nil
		}
		m.err = nil
		m.render.Invalidate()
		return m, nil
	case terminalTitleMsg:
		if m.workbench != nil {
			m.workbench.SetPaneTitleByTerminalID(typed.TerminalID, typed.Title)
		}
		m.render.Invalidate()
		return m, nil
	case InvalidateMsg:
		m.render.Invalidate()
		return m, nil
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
		m.render.Invalidate()
		return m, m.resizeVisiblePanesCmd()
	case error:
		return m, m.showError(typed)
	default:
		return m, nil
	}
}

func (m *Model) showError(err error) tea.Cmd {
	if m == nil {
		return nil
	}
	m.errorSeq++
	m.err = err
	m.render.Invalidate()
	return clearErrorCmd(m.errorSeq)
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
	if m == nil {
		return false, nil
	}
	if m.input.Mode().Kind == input.ModeTerminalManager && m.terminalPage != nil {
		switch action.Kind {
		case input.ActionPickerUp:
			m.terminalPage.Move(-1)
			m.render.Invalidate()
			return true, nil
		case input.ActionPickerDown:
			m.terminalPage.Move(1)
			m.render.Invalidate()
			return true, nil
		case input.ActionCancelMode:
			m.closeTerminalManager()
			return true, nil
		case input.ActionSubmitPrompt:
			if selected := m.selectedAttachableTerminalPageItem(); selected != nil {
				m.closeTerminalManager()
				return true, m.attachPaneTerminalCmd("", m.currentOrActionPaneID(action.PaneID), selected.TerminalID)
			}
			if m.selectedTerminalManagerItem() != nil {
				return true, m.showError(shared.UserVisibleError{Op: "attach terminal", Err: fmt.Errorf("selected terminal is exited")})
			}
			return true, nil
		case input.ActionAttachTab:
			if selected := m.selectedAttachableTerminalPageItem(); selected != nil {
				m.closeTerminalManager()
				return true, m.createTabAndAttachTerminalCmd(selected.TerminalID)
			}
			if m.selectedTerminalManagerItem() != nil {
				return true, m.showError(shared.UserVisibleError{Op: "attach terminal", Err: fmt.Errorf("selected terminal is exited")})
			}
			return true, nil
		case input.ActionAttachFloating:
			if selected := m.selectedAttachableTerminalPageItem(); selected != nil {
				m.closeTerminalManager()
				return true, m.createFloatingPaneAndAttachTerminalCmd(selected.TerminalID)
			}
			if m.selectedTerminalManagerItem() != nil {
				return true, m.showError(shared.UserVisibleError{Op: "attach terminal", Err: fmt.Errorf("selected terminal is exited")})
			}
			return true, nil
		case input.ActionEditTerminal:
			if selected := m.selectedTerminalManagerItem(); selected != nil {
				m.openEditTerminalPrompt(selected)
				return true, nil
			}
			return true, nil
		case input.ActionKillTerminal:
			if selected := m.selectedTerminalManagerItem(); selected != nil {
				terminalID := selected.TerminalID
				items := m.terminalPage.Items
				filtered := items[:0]
				for _, item := range items {
					if item.TerminalID != terminalID {
						filtered = append(filtered, item)
					}
				}
				m.terminalPage.Items = filtered
				m.terminalPage.ApplyFilter()
				normalizeModalSelection(&m.terminalPage.Selected, len(m.terminalPage.VisibleItems()))
				m.render.Invalidate()
				return true, m.effectCmd(orchestrator.KillTerminalEffect{TerminalID: terminalID})
			}
			return true, nil
		default:
			return false, nil
		}
	}
	if m.modalHost == nil || m.modalHost.Session == nil {
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
				m.openCreateTerminalPrompt(action.PaneID, modal.CreateTargetReplace)
				return true, nil
			}
			if m.modalHost.Picker.SelectedItem() == nil {
				return true, nil
			}
			return false, nil
		case input.ActionPickerAttachSplit:
			selected := m.modalHost.Picker.SelectedItem()
			if selected == nil {
				return true, nil
			}
			if selected.CreateNew {
				m.openCreateTerminalPrompt(action.PaneID, modal.CreateTargetSplit)
				return true, nil
			}
			m.modalHost.Close(input.ModePicker, m.modalHost.Session.RequestID)
			m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
			m.render.Invalidate()
			return true, m.splitPaneAndAttachTerminalCmd(m.currentOrActionPaneID(action.PaneID), selected.TerminalID)
		case input.ActionEditTerminal:
			if selected := m.modalHost.Picker.SelectedItem(); selected != nil && !selected.CreateNew {
				m.openEditTerminalPrompt(selected)
			}
			return true, nil
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
				normalizeModalSelection(&m.modalHost.Picker.Selected, len(m.modalHost.Picker.VisibleItems()))
				m.render.Invalidate()
				return true, m.effectCmd(orchestrator.KillTerminalEffect{TerminalID: terminalID})
			}
			return true, nil
		default:
			return false, nil
		}
	case input.ModePrompt:
		switch action.Kind {
		case input.ActionCancelMode:
			m.modalHost.Close(input.ModePrompt, m.modalHost.Session.RequestID)
			m.restorePromptReturnMode(m.modalHost.Prompt)
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
					return true, func() tea.Msg {
						return input.SemanticAction{Kind: input.ActionCreateWorkspace}
					}
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
		return true, m.rearmPrefixTimeoutCmd()
	case input.ActionEnterFloatingMode:
		m.ensureFloatingModeTarget()
		m.input.SetMode(input.ModeState{Kind: input.ModeFloating})
		m.render.Invalidate()
		return true, m.rearmPrefixTimeoutCmd()
	case input.ActionEnterDisplayMode:
		m.input.SetMode(input.ModeState{Kind: input.ModeDisplay})
		m.render.Invalidate()
		return true, m.rearmPrefixTimeoutCmd()
	case input.ActionEnterGlobalMode:
		m.input.SetMode(input.ModeState{Kind: input.ModeGlobal})
		m.render.Invalidate()
		return true, m.rearmPrefixTimeoutCmd()
	case input.ActionCancelMode:
		if m.input.Mode().Kind == input.ModeTerminalManager && m.terminalPage != nil {
			return false, nil
		}
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
	case input.ActionCreateWorkspace:
		if m.input.Mode().Kind != input.ModeWorkspace || m.workbench == nil {
			return false, nil
		}
		name := "workspace-" + shared.GenerateShortID()
		if err := m.workbench.CreateWorkspace(name); err != nil {
			return true, m.showError(err)
		}
		_ = m.workbench.SwitchWorkspace(name)
		m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.saveStateCmd()
	case input.ActionDeleteWorkspace:
		if m.input.Mode().Kind != input.ModeWorkspace || m.workbench == nil {
			return false, nil
		}
		ws := m.workbench.CurrentWorkspace()
		if ws == nil {
			return true, nil
		}
		if err := m.workbench.DeleteWorkspace(ws.Name); err != nil {
			return true, m.showError(err)
		}
		m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.saveStateCmd()
	case input.ActionRenameWorkspace:
		if m.input.Mode().Kind != input.ModeWorkspace {
			return false, nil
		}
		m.openRenameWorkspacePrompt()
		return true, nil
	case input.ActionPrevWorkspace:
		if m.input.Mode().Kind != input.ModeWorkspace || m.workbench == nil {
			return false, nil
		}
		if err := m.workbench.SwitchWorkspaceByOffset(-1); err != nil {
			return true, m.showError(err)
		}
		m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.saveStateCmd()
	case input.ActionNextWorkspace:
		if m.input.Mode().Kind != input.ModeWorkspace || m.workbench == nil {
			return false, nil
		}
		if err := m.workbench.SwitchWorkspaceByOffset(1); err != nil {
			return true, m.showError(err)
		}
		m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, m.saveStateCmd()
	case input.ActionRenameTab:
		if m.input.Mode().Kind != input.ModeTab {
			return false, nil
		}
		m.openRenameTabPrompt()
		return true, nil
	case input.ActionJumpTab:
		if m.input.Mode().Kind != input.ModeTab || m.workbench == nil {
			return false, nil
		}
		index, err := strconv.Atoi(strings.TrimSpace(action.Text))
		if err != nil || index < 1 {
			return true, nil
		}
		ws := m.workbench.CurrentWorkspace()
		if ws == nil {
			return true, nil
		}
		if err := m.workbench.SwitchTab(ws.Name, index-1); err != nil {
			return true, m.showError(err)
		}
		m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, tea.Batch(m.resizeVisiblePanesCmd(), m.saveStateCmd())
	case input.ActionPrevTab:
		if m.input.Mode().Kind != input.ModeTab || m.workbench == nil {
			return false, nil
		}
		if err := m.switchCurrentTabByOffset(-1); err != nil {
			return true, m.showError(err)
		}
		m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, tea.Batch(m.resizeVisiblePanesCmd(), m.saveStateCmd())
	case input.ActionNextTab:
		if m.input.Mode().Kind != input.ModeTab || m.workbench == nil {
			return false, nil
		}
		if err := m.switchCurrentTabByOffset(1); err != nil {
			return true, m.showError(err)
		}
		m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
		m.render.Invalidate()
		return true, tea.Batch(m.resizeVisiblePanesCmd(), m.saveStateCmd())
	case input.ActionKillTab:
		if m.input.Mode().Kind != input.ModeTab {
			return false, nil
		}
		return true, m.killCurrentTabCmd()
	case input.ActionOpenTerminalManager:
		if m.input.Mode().Kind == input.ModeGlobal {
			m.terminalPage = &modal.TerminalManagerState{
				Title:  "Terminal Pool",
				Footer: input.FooterForMode(input.ModeTerminalManager, false),
			}
			m.input.SetMode(input.ModeState{Kind: input.ModeTerminalManager, RequestID: terminalPoolPageModeToken})
			m.render.Invalidate()
			return true, m.loadTerminalManagerItemsCmd()
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

const (
	prefixModeTimeout         = 3000 * time.Millisecond
	terminalPoolPageModeToken = "terminal-pool"
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

func (m *Model) selectedTerminalManagerItem() *modal.PickerItem {
	if m == nil || m.terminalPage == nil {
		return nil
	}
	selected := m.terminalPage.SelectedItem()
	if selected == nil || selected.CreateNew || selected.TerminalID == "" {
		return nil
	}
	return selected
}

func (m *Model) selectedAttachableTerminalPageItem() *modal.PickerItem {
	selected := m.selectedTerminalManagerItem()
	if selected == nil || selected.State == "exited" {
		return nil
	}
	return selected
}

func (m *Model) closeTerminalManager() {
	if m == nil {
		return
	}
	m.terminalPage = nil
	m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
	m.render.Invalidate()
}

func (m *Model) currentOrActionPaneID(paneID string) string {
	if strings.TrimSpace(paneID) != "" {
		return paneID
	}
	if m == nil || m.workbench == nil {
		return ""
	}
	if pane := m.workbench.ActivePane(); pane != nil {
		return pane.ID
	}
	return ""
}

func (m *Model) loadTerminalManagerItemsCmd() tea.Cmd {
	if m == nil || m.runtime == nil {
		return nil
	}
	return func() tea.Msg {
		terminals, err := m.runtime.ListTerminals(context.Background())
		if err != nil {
			return err
		}
		return terminalManagerItemsLoadedMsg{Items: m.buildTerminalManagerItems(terminals)}
	}
}

func (m *Model) buildTerminalManagerItems(terminals []protocol.TerminalInfo) []modal.PickerItem {
	if len(terminals) == 0 {
		return nil
	}
	bindings := map[string][]workbench.TerminalBindingLocation(nil)
	if m != nil && m.workbench != nil {
		bindings = m.workbench.TerminalBindings()
	}
	items := make([]modal.PickerItem, 0, len(terminals))
	for _, terminal := range terminals {
		locations := bindings[terminal.ID]
		visibleCount := 0
		for _, location := range locations {
			if location.Visible {
				visibleCount++
			}
		}
		group := "parked"
		switch {
		case terminal.State == "exited":
			group = "exited"
		case visibleCount > 0:
			group = "visible"
		}
		items = append(items, modal.PickerItem{
			TerminalID:  terminal.ID,
			Name:        terminal.Name,
			State:       group,
			Command:     strings.Join(terminal.Command, " "),
			CommandArgs: append([]string(nil), terminal.Command...),
			Tags:        cloneStringMap(terminal.Tags),
			Location:    formatTerminalManagerLocation(locations),
			Observed:    visibleCount > 0,
			Orphan:      len(locations) == 0,
			Description: formatTerminalManagerDescription(terminal, len(locations)),
			CreatedAt:   terminal.CreatedAt,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		leftGroup := terminalManagerGroupOrder(items[i].State)
		rightGroup := terminalManagerGroupOrder(items[j].State)
		if leftGroup != rightGroup {
			return leftGroup < rightGroup
		}
		leftName := strings.TrimSpace(items[i].Name)
		rightName := strings.TrimSpace(items[j].Name)
		if leftName == rightName {
			return items[i].TerminalID < items[j].TerminalID
		}
		if leftName == "" {
			return false
		}
		if rightName == "" {
			return true
		}
		return leftName < rightName
	})
	return items
}

func terminalManagerGroupOrder(state string) int {
	switch state {
	case "visible":
		return 0
	case "parked":
		return 1
	case "exited":
		return 2
	default:
		return 3
	}
}

func formatTerminalManagerLocation(locations []workbench.TerminalBindingLocation) string {
	if len(locations) == 0 {
		return "unbound"
	}
	index := 0
	for i, location := range locations {
		if location.Visible {
			index = i
			break
		}
	}
	location := locations[index]
	label := location.WorkspaceName + "/" + location.TabName + "/" + location.PaneID
	if len(locations) == 1 {
		return label
	}
	return label + " +" + strconv.Itoa(len(locations)-1)
}

func formatTerminalManagerDescription(terminal protocol.TerminalInfo, boundCount int) string {
	state := strings.TrimSpace(terminal.State)
	if state == "" {
		state = "unknown"
	}
	if terminal.ExitCode != nil {
		state += " (" + strconv.Itoa(*terminal.ExitCode) + ")"
	}
	paneWord := "panes"
	if boundCount == 1 {
		paneWord = "pane"
	}
	return state + " · " + strconv.Itoa(boundCount) + " " + paneWord + " bound"
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
	panes := make([]workbench.VisiblePane, 0, len(tab.Panes)+len(visible.FloatingPanes))
	panes = append(panes, tab.Panes...)
	panes = append(panes, visible.FloatingPanes...)

	cmds := make([]tea.Cmd, 0, len(panes))
	for _, pane := range panes {
		if pane.ID == "" || pane.TerminalID == "" {
			continue
		}
		cols := uint16(maxInt(2, pane.Rect.W-2))
		rows := uint16(maxInt(2, pane.Rect.H-2))
		paneID := pane.ID
		cmds = append(cmds, func() tea.Msg {
			if err := m.runtime.ResizeTerminal(context.Background(), paneID, pane.TerminalID, cols, rows); err != nil {
				return err
			}
			return nil
		})
	}
	return tea.Batch(cmds...)
}

func (m *Model) handleModalKeyMsg(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m == nil {
		return false, nil
	}
	if m.input.Mode().Kind == input.ModeTerminalManager && m.terminalPage != nil {
		return m.handleTerminalManagerQueryKeyMsg(msg)
	}
	if m.modalHost == nil || m.modalHost.Session == nil {
		return false, nil
	}
	switch m.modalHost.Session.Kind {
	case input.ModePrompt:
		if m.modalHost.Prompt == nil {
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
			if deleteLastRune(&m.modalHost.Prompt.Value) {
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
	case input.ModePicker:
		return m.handlePickerQueryKeyMsg(msg)
	case input.ModeWorkspacePicker:
		return m.handleWorkspacePickerQueryKeyMsg(msg)
	case input.ModeTerminalManager:
		return m.handleTerminalManagerQueryKeyMsg(msg)
	default:
		return false, nil
	}
}

func (m *Model) handlePickerQueryKeyMsg(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m.modalHost == nil || m.modalHost.Picker == nil {
		return false, nil
	}
	switch msg.Type {
	case tea.KeyRunes:
		if len(msg.Runes) == 0 {
			return true, nil
		}
		m.modalHost.Picker.Query += string(msg.Runes)
	case tea.KeyBackspace:
		if !deleteLastRune(&m.modalHost.Picker.Query) {
			return true, nil
		}
	default:
		return false, nil
	}
	m.modalHost.Picker.ApplyFilter()
	normalizeModalSelection(&m.modalHost.Picker.Selected, len(m.modalHost.Picker.VisibleItems()))
	m.render.Invalidate()
	return true, nil
}

func (m *Model) handleWorkspacePickerQueryKeyMsg(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m.modalHost == nil || m.modalHost.WorkspacePicker == nil {
		return false, nil
	}
	switch msg.Type {
	case tea.KeyRunes:
		if len(msg.Runes) == 0 {
			return true, nil
		}
		m.modalHost.WorkspacePicker.Query += string(msg.Runes)
	case tea.KeyBackspace:
		if !deleteLastRune(&m.modalHost.WorkspacePicker.Query) {
			return true, nil
		}
	default:
		return false, nil
	}
	m.modalHost.WorkspacePicker.ApplyFilter()
	normalizeModalSelection(&m.modalHost.WorkspacePicker.Selected, len(m.modalHost.WorkspacePicker.VisibleItems()))
	m.render.Invalidate()
	return true, nil
}

func (m *Model) handleTerminalManagerQueryKeyMsg(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m.terminalPage == nil {
		return false, nil
	}
	switch msg.Type {
	case tea.KeyRunes:
		if len(msg.Runes) == 0 {
			return true, nil
		}
		m.terminalPage.Query += string(msg.Runes)
	case tea.KeyBackspace:
		if !deleteLastRune(&m.terminalPage.Query) {
			return true, nil
		}
	default:
		return false, nil
	}
	m.terminalPage.ApplyFilter()
	normalizeModalSelection(&m.terminalPage.Selected, len(m.terminalPage.VisibleItems()))
	m.render.Invalidate()
	return true, nil
}

func deleteLastRune(value *string) bool {
	if value == nil || *value == "" {
		return false
	}
	_, size := utf8.DecodeLastRuneInString(*value)
	if size > 0 {
		*value = (*value)[:len(*value)-size]
	} else {
		*value = ""
	}
	return true
}

func normalizeModalSelection(selected *int, count int) {
	if selected == nil {
		return
	}
	if count <= 0 || *selected < 0 || *selected >= count {
		*selected = 0
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

func (m *Model) openEditTerminalPrompt(item *modal.PickerItem) {
	if m == nil || m.modalHost == nil || item == nil || item.TerminalID == "" {
		return
	}
	name := strings.TrimSpace(item.Name)
	requestID := "edit-terminal:" + item.TerminalID
	m.modalHost.Session = &modal.ModalSession{Kind: input.ModePrompt, Phase: modal.ModalPhaseReady, RequestID: requestID}
	m.modalHost.Prompt = &modal.PromptState{
		Kind:        "edit-terminal-name",
		Title:       "Edit Terminal",
		Hint:        "[Enter] continue  [Esc] cancel",
		Value:       name,
		Original:    name,
		DefaultName: name,
		TerminalID:  item.TerminalID,
		Command:     append([]string(nil), item.CommandArgs...),
		Name:        name,
		Tags:        cloneStringMap(item.Tags),
		ReturnMode:  m.promptReturnMode(),
	}
	m.input.SetMode(input.ModeState{Kind: input.ModePrompt, RequestID: requestID})
	m.render.Invalidate()
}

func (m *Model) openCreateTerminalPrompt(paneID string, target modal.CreateTargetKind) {
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
		Kind:         "create-terminal-name",
		Title:        "Create Terminal",
		Hint:         "[Enter] continue  [Esc] cancel",
		Original:     defaultName,
		DefaultName:  defaultName,
		PaneID:       paneID,
		Command:      []string{shell},
		CreateTarget: target,
	}
	m.input.SetMode(input.ModeState{Kind: input.ModePrompt, RequestID: requestID})
	m.render.Invalidate()
}

func (m *Model) openRenameWorkspacePrompt() {
	if m == nil || m.modalHost == nil || m.workbench == nil {
		return
	}
	workspace := m.workbench.CurrentWorkspace()
	if workspace == nil {
		return
	}
	requestID := "rename-workspace:" + workspace.Name
	m.modalHost.Session = &modal.ModalSession{Kind: input.ModePrompt, Phase: modal.ModalPhaseReady, RequestID: requestID}
	m.modalHost.Prompt = &modal.PromptState{
		Kind:       "rename-workspace",
		Title:      "rename workspace",
		Hint:       "[Enter] save  [Esc] cancel",
		Value:      workspace.Name,
		Original:   workspace.Name,
		AllowEmpty: false,
	}
	m.input.SetMode(input.ModeState{Kind: input.ModePrompt, RequestID: requestID})
	m.render.Invalidate()
}

func (m *Model) openRenameTabPrompt() {
	if m == nil || m.modalHost == nil || m.workbench == nil {
		return
	}
	tab := m.workbench.CurrentTab()
	if tab == nil {
		return
	}
	requestID := "rename-tab:" + tab.ID
	m.modalHost.Session = &modal.ModalSession{Kind: input.ModePrompt, Phase: modal.ModalPhaseReady, RequestID: requestID}
	m.modalHost.Prompt = &modal.PromptState{
		Kind:       "rename-tab",
		Title:      "rename tab",
		Hint:       "[Enter] save  [Esc] cancel",
		Value:      tab.Name,
		Original:   tab.Name,
		AllowEmpty: false,
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
	case "rename-tab":
		name := strings.TrimSpace(prompt.Value)
		if name == "" {
			name = strings.TrimSpace(prompt.Original)
		}
		if m.workbench == nil {
			return func() tea.Msg { return context.Canceled }
		}
		tab := m.workbench.CurrentTab()
		if tab == nil {
			return func() tea.Msg { return context.Canceled }
		}
		if err := m.workbench.RenameTab(tab.ID, name); err != nil {
			return func() tea.Msg { return err }
		}
		requestID := m.modalHost.Session.RequestID
		m.modalHost.Close(input.ModePrompt, requestID)
		m.restorePromptReturnMode(prompt)
		m.render.Invalidate()
		return m.saveStateCmd()
	case "rename-workspace":
		name := strings.TrimSpace(prompt.Value)
		if name == "" {
			name = strings.TrimSpace(prompt.Original)
		}
		original := strings.TrimSpace(prompt.Original)
		if m.workbench == nil {
			return func() tea.Msg { return context.Canceled }
		}
		if err := m.workbench.RenameWorkspace(original, name); err != nil {
			return func() tea.Msg { return err }
		}
		requestID := m.modalHost.Session.RequestID
		m.modalHost.Close(input.ModePrompt, requestID)
		m.restorePromptReturnMode(prompt)
		m.render.Invalidate()
		return m.saveStateCmd()
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
		prompt.Kind = "edit-terminal-tags"
		prompt.Title = "Edit Terminal"
		prompt.Hint = "[Enter] save  [Esc] cancel"
		prompt.AllowEmpty = true
		prompt.Name = name
		prompt.Value = formatPromptTags(prompt.Tags)
		m.render.Invalidate()
		return nil
	case "edit-terminal-tags":
		tags, err := parsePromptTags(prompt.Value)
		if err != nil {
			return func() tea.Msg { return err }
		}
		name := strings.TrimSpace(prompt.Name)
		if name == "" {
			name = strings.TrimSpace(prompt.DefaultName)
		}
		terminalID := prompt.TerminalID
		requestID := m.modalHost.Session.RequestID
		m.modalHost.Close(input.ModePrompt, requestID)
		m.restorePromptReturnMode(prompt)
		m.render.Invalidate()
		return func() tea.Msg {
			client := m.runtime.Client()
			if client == nil {
				return context.Canceled
			}
			if err := client.SetMetadata(context.Background(), terminalID, name, tags); err != nil {
				return err
			}
			if m.runtime != nil && m.runtime.Registry() != nil {
				m.runtime.Registry().SetMetadata(terminalID, name, tags)
			}
			if err := saveState(m.statePath, m.workbench, m.runtime); err != nil {
				return err
			}
			m.render.Invalidate()
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
		m.restorePromptReturnMode(prompt)
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
			switch prompt.CreateTarget {
			case modal.CreateTargetSplit:
				if cmd := m.splitPaneAndAttachTerminalCmd(pane, created.TerminalID); cmd != nil {
					return cmd()
				}
				return nil
			case modal.CreateTargetNewTab:
				if cmd := m.createTabAndAttachTerminalCmd(created.TerminalID); cmd != nil {
					return cmd()
				}
				return nil
			case modal.CreateTargetFloating:
				if cmd := m.createFloatingPaneAndAttachTerminalCmd(created.TerminalID); cmd != nil {
					return cmd()
				}
				return nil
			default:
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
		}
	default:
		return nil
	}
}

func (m *Model) promptReturnMode() input.ModeKind {
	if m == nil {
		return input.ModeNormal
	}
	if m.input.Mode().Kind == input.ModeTerminalManager && m.terminalPage != nil {
		return input.ModeTerminalManager
	}
	return input.ModeNormal
}

func (m *Model) restorePromptReturnMode(prompt *modal.PromptState) {
	if m == nil {
		return
	}
	mode := input.ModeNormal
	if prompt != nil && prompt.ReturnMode != "" {
		mode = prompt.ReturnMode
	}
	next := input.ModeState{Kind: mode}
	if mode == input.ModeTerminalManager {
		next.RequestID = terminalPoolPageModeToken
	}
	m.input.SetMode(next)
}

func (m *Model) switchCurrentTabByOffset(delta int) error {
	if m == nil || m.workbench == nil {
		return nil
	}
	ws := m.workbench.CurrentWorkspace()
	if ws == nil || len(ws.Tabs) == 0 {
		return nil
	}
	next := (ws.ActiveTab + delta + len(ws.Tabs)) % len(ws.Tabs)
	return m.workbench.SwitchTab(ws.Name, next)
}

func (m *Model) killCurrentTabCmd() tea.Cmd {
	if m == nil || m.workbench == nil {
		return nil
	}
	tab := m.workbench.CurrentTab()
	ws := m.workbench.CurrentWorkspace()
	if tab == nil || ws == nil {
		return nil
	}
	terminalIDs := make([]string, 0, len(tab.Panes))
	seen := make(map[string]struct{}, len(tab.Panes))
	for _, pane := range tab.Panes {
		if pane == nil || pane.TerminalID == "" {
			continue
		}
		if _, exists := seen[pane.TerminalID]; exists {
			continue
		}
		seen[pane.TerminalID] = struct{}{}
		terminalIDs = append(terminalIDs, pane.TerminalID)
	}
	if err := m.workbench.CloseTab(tab.ID); err != nil {
		return m.showError(err)
	}
	m.input.SetMode(input.ModeState{Kind: input.ModeNormal})
	m.render.Invalidate()

	cmds := make([]tea.Cmd, 0, len(terminalIDs)+2)
	for _, terminalID := range terminalIDs {
		cmds = append(cmds, m.effectCmd(orchestrator.KillTerminalEffect{TerminalID: terminalID}))
	}
	cmds = append(cmds, m.resizeVisiblePanesCmd(), m.saveStateCmd())
	return tea.Batch(cmds...)
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

func formatPromptTags(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+tags[key])
	}
	return strings.Join(parts, " ")
}

type inputError string

func (e inputError) Error() string { return string(e) }

func (m *Model) handleMouseMsg(msg tea.MouseMsg) tea.Cmd {
	// 只有当真正的覆盖层模态窗口打开时才阻止鼠标事件
	// 浮动窗口不是模态窗口，应该始终可以交互
	if m.modalHost != nil && m.modalHost.Session != nil {
		kind := m.modalHost.Session.Kind
		// 这些是真正的覆盖层模态，会遮挡整个界面
		if kind == input.ModePicker || kind == input.ModePrompt ||
			kind == input.ModeHelp || kind == input.ModeTerminalManager ||
			kind == input.ModeWorkspacePicker {
			return nil
		}
	}

	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button == tea.MouseButtonLeft {
			return m.handleMouseClick(msg.X, msg.Y)
		}
	case tea.MouseActionMotion:
		if msg.Button == tea.MouseButtonLeft && m.mouseDragPaneID != "" {
			return m.handleMouseDrag(msg.X, msg.Y)
		}
	case tea.MouseActionRelease:
		if msg.Button == tea.MouseButtonLeft {
			return m.handleMouseRelease()
		}
	}
	return nil
}

func (m *Model) handleMouseClickNonFloating(x, y int) tea.Cmd {
	// 处理非浮动窗口的点击（例如点击普通 pane 来聚焦）
	if m.workbench == nil {
		return nil
	}

	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}

	// 转换为内容区域坐标
	contentY := y - 1
	if contentY < 0 {
		return nil
	}

	// 获取可见的 pane 布局
	bodyRect := workbench.Rect{W: maxInt(1, m.width), H: maxInt(1, m.height-2)}
	visible := m.workbench.VisibleWithSize(bodyRect)
	if visible == nil || visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		return nil
	}

	visibleTab := visible.Tabs[visible.ActiveTab]

	// 检查是否点击了普通 pane
	for _, pane := range visibleTab.Panes {
		rect := pane.Rect
		if x >= rect.X && x < rect.X+rect.W && contentY >= rect.Y && contentY < rect.Y+rect.H {
			if pane.ID != tab.ActivePaneID {
				_ = m.workbench.FocusPane(tab.ID, pane.ID)
				m.render.Invalidate()
			}
			return nil
		}
	}

	return nil
}

func (m *Model) handleMouseClick(x, y int) tea.Cmd {
	if m.workbench == nil {
		return nil
	}

	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}

	// 转换为内容区域坐标（减去 tab bar 高度）
	contentY := y - 1
	if contentY < 0 {
		return nil
	}

	// 调试：记录浮动窗口数量
	floatingCount := len(tab.Floating)
	_ = floatingCount // 避免未使用变量警告

	// 检查是否点击了浮动窗口
	paneID, rect, isResize := m.findFloatingPaneAt(tab, x, contentY)
	if paneID != "" {
		// 激活该浮动窗口
		if tab.ActivePaneID != paneID {
			_ = m.workbench.FocusPane(tab.ID, paneID)
		}
		m.workbench.ReorderFloatingPane(tab.ID, paneID, true)

		// 开始拖动
		m.mouseDragPaneID = paneID
		if isResize {
			m.mouseDragMode = mouseDragResize
			m.mouseDragOffsetX = 0
			m.mouseDragOffsetY = 0
		} else {
			m.mouseDragMode = mouseDragMove
			m.mouseDragOffsetX = x - rect.X
			m.mouseDragOffsetY = contentY - rect.Y
		}
		m.render.Invalidate()
		return nil
	}

	// 如果没有点击浮动窗口，检查是否点击了普通 pane
	return m.handleMouseClickNonFloating(x, y)
}

func (m *Model) handleMouseDrag(x, y int) tea.Cmd {
	if m.workbench == nil || m.mouseDragPaneID == "" {
		return nil
	}

	tab := m.workbench.CurrentTab()
	if tab == nil {
		return nil
	}

	// 转换为内容区域坐标
	contentY := y - 1
	if contentY < 0 {
		contentY = 0
	}

	switch m.mouseDragMode {
	case mouseDragMove:
		newX := x - m.mouseDragOffsetX
		newY := contentY - m.mouseDragOffsetY
		m.workbench.MoveFloatingPane(tab.ID, m.mouseDragPaneID, newX, newY)
		m.render.Invalidate()
	case mouseDragResize:
		// 找到浮动窗口的当前位置
		for _, floating := range tab.Floating {
			if floating != nil && floating.PaneID == m.mouseDragPaneID {
				newW := x - floating.Rect.X + 1
				newH := contentY - floating.Rect.Y + 1
				m.workbench.ResizeFloatingPane(tab.ID, m.mouseDragPaneID, newW, newH)
				m.render.Invalidate()
				return m.resizeVisiblePanesCmd()
			}
		}
	}

	return nil
}

func (m *Model) handleMouseRelease() tea.Cmd {
	m.mouseDragPaneID = ""
	m.mouseDragOffsetX = 0
	m.mouseDragOffsetY = 0
	m.mouseDragMode = mouseDragNone
	return nil
}

// findFloatingPaneAt 查找指定坐标处的浮动窗口
// 返回 paneID, rect, isResize
func (m *Model) findFloatingPaneAt(tab *workbench.TabState, x, y int) (string, workbench.Rect, bool) {
	if tab == nil || len(tab.Floating) == 0 {
		return "", workbench.Rect{}, false
	}

	// 从后往前遍历（Z 序最高的在最后）
	for i := len(tab.Floating) - 1; i >= 0; i-- {
		floating := tab.Floating[i]
		if floating == nil {
			continue
		}

		rect := floating.Rect
		// 检查是否在窗口范围内
		if x >= rect.X && x < rect.X+rect.W && y >= rect.Y && y < rect.Y+rect.H {
			// 检查是否点击了 resize handle（右下角 2x2 区域）
			isResize := x >= rect.X+rect.W-2 && y >= rect.Y+rect.H-2
			return floating.PaneID, rect, isResize
		}
	}

	return "", workbench.Rect{}, false
}

func (m *Model) ensureFloatingModeTarget() {
	if m == nil || m.workbench == nil {
		return
	}
	tab := m.workbench.CurrentTab()
	if tab == nil || len(tab.Floating) == 0 {
		return
	}
	if active := activeFloatingPaneID(tab); active != "" {
		return
	}
	paneID := topmostFloatingPaneID(tab)
	if paneID == "" {
		return
	}
	_ = m.workbench.FocusPane(tab.ID, paneID)
	m.workbench.ReorderFloatingPane(tab.ID, paneID, true)
}

func activeFloatingPaneID(tab *workbench.TabState) string {
	if tab == nil || tab.ActivePaneID == "" {
		return ""
	}
	for _, floating := range tab.Floating {
		if floating != nil && floating.PaneID == tab.ActivePaneID {
			return tab.ActivePaneID
		}
	}
	return ""
}

func topmostFloatingPaneID(tab *workbench.TabState) string {
	if tab == nil || len(tab.Floating) == 0 {
		return ""
	}
	paneID := ""
	maxZ := 0
	for _, floating := range tab.Floating {
		if floating == nil || floating.PaneID == "" {
			continue
		}
		if paneID == "" || floating.Z >= maxZ {
			paneID = floating.PaneID
			maxZ = floating.Z
		}
	}
	return paneID
}
