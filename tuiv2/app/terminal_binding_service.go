package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
)

type terminalBindingService struct {
	model *Model
}

type bindTerminalSelectionResult struct {
	tabID             string
	paneID            string
	terminalID        string
	loadSnapshotAfter bool
}

func (m *Model) terminalBindingService() *terminalBindingService {
	if m == nil {
		return nil
	}
	return &terminalBindingService{model: m}
}

func (s *terminalBindingService) splitAndBindCmd(paneID string, item modal.PickerItem) tea.Cmd {
	if s == nil || s.model == nil || s.model.orchestrator == nil || paneID == "" || item.TerminalID == "" {
		return nil
	}
	tabID, newPaneID, err := s.model.orchestrator.PrepareSplitAttachTarget(paneID)
	if err != nil {
		return func() tea.Msg { return err }
	}
	return s.bindSelectionCmd(tabID, newPaneID, item)
}

func (s *terminalBindingService) createTabAndBindCmd(item modal.PickerItem) tea.Cmd {
	if s == nil || s.model == nil || s.model.orchestrator == nil || item.TerminalID == "" {
		return nil
	}
	tabID, paneID, err := s.model.orchestrator.PrepareTabAttachTarget()
	if err != nil {
		return func() tea.Msg { return err }
	}
	return s.bindSelectionCmd(tabID, paneID, item)
}

func (s *terminalBindingService) createFloatingAndBindCmd(item modal.PickerItem) tea.Cmd {
	if s == nil || s.model == nil || s.model.orchestrator == nil || item.TerminalID == "" {
		return nil
	}
	tabID, paneID, err := s.model.orchestrator.PrepareFloatingAttachTarget()
	if err != nil {
		return func() tea.Msg { return err }
	}
	return s.bindSelectionCmd(tabID, paneID, item)
}

func (s *terminalBindingService) bindSelectionCmd(tabID, paneID string, item modal.PickerItem) tea.Cmd {
	if s == nil || s.model == nil || s.model.workbench == nil || paneID == "" || item.TerminalID == "" {
		return nil
	}
	result, err := s.bindSelection(tabID, paneID, item)
	if err != nil {
		return func() tea.Msg { return err }
	}
	s.model.resetPaneScrollOffset(result.tabID, result.paneID)
	s.model.render.Invalidate()
	cmds := []tea.Cmd{s.model.saveStateCmd()}
	if result.loadSnapshotAfter {
		cmds = append(cmds, s.model.effectCmd(orchestrator.LoadSnapshotEffect{
			TerminalID: result.terminalID,
			Offset:     0,
			Limit:      defaultTerminalSnapshotScrollbackLimit,
		}))
	}
	return batchCmds(cmds...)
}

func (s *terminalBindingService) bindSelection(tabID, paneID string, item modal.PickerItem) (bindTerminalSelectionResult, error) {
	if s == nil || s.model == nil || s.model.workbench == nil || paneID == "" || item.TerminalID == "" {
		return bindTerminalSelectionResult{}, nil
	}
	resolvedTabID, oldTerminalID, err := s.resolveTarget(tabID, paneID)
	if err != nil {
		return bindTerminalSelectionResult{}, err
	}
	if err := s.model.workbench.BindPaneTerminal(resolvedTabID, paneID, item.TerminalID); err != nil {
		return bindTerminalSelectionResult{}, err
	}
	_ = s.model.workbench.FocusPane(resolvedTabID, paneID)
	if name := strings.TrimSpace(item.Name); name != "" {
		s.model.workbench.SetPaneTitleByTerminalID(item.TerminalID, name)
	}
	if s.model.runtime != nil {
		s.model.runtime.UnbindPane(paneID, oldTerminalID)
		terminal := s.model.runtime.Registry().GetOrCreate(item.TerminalID)
		if terminal != nil {
			terminal.Name = strings.TrimSpace(item.Name)
			if len(item.CommandArgs) > 0 {
				terminal.Command = append([]string(nil), item.CommandArgs...)
			}
			terminal.Tags = cloneStringMap(item.Tags)
			terminal.State = terminalSelectionState(item)
			terminal.ExitCode = cloneIntPointer(item.ExitCode)
			terminal.Channel = 0
			terminal.AttachMode = ""
			terminal.OwnerPaneID = paneID
			terminal.ControlPaneID = ""
			terminal.RequiresExplicitOwner = false
			terminal.BoundPaneIDs = appendUniqueValue(terminal.BoundPaneIDs, paneID)
		}
	}
	return bindTerminalSelectionResult{
		tabID:             resolvedTabID,
		paneID:            paneID,
		terminalID:        item.TerminalID,
		loadSnapshotAfter: s.model.runtime != nil && terminalSelectionState(item) == "exited",
	}, nil
}

func (s *terminalBindingService) resolveTarget(tabID, paneID string) (string, string, error) {
	if s == nil || s.model == nil || s.model.workbench == nil || paneID == "" {
		return "", "", nil
	}
	workspace := s.model.workbench.CurrentWorkspace()
	if workspace == nil {
		return "", "", teaErr("select terminal: no current workspace")
	}
	for _, tab := range workspace.Tabs {
		if tab == nil || tab.Panes[paneID] == nil {
			continue
		}
		if tabID != "" && tab.ID != tabID {
			continue
		}
		return tab.ID, tab.Panes[paneID].TerminalID, nil
	}
	if tabID != "" {
		return "", "", teaErr("select terminal: pane " + paneID + " not found in tab " + tabID)
	}
	return "", "", teaErr("select terminal: pane " + paneID + " not found")
}
