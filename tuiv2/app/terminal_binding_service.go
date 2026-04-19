package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/runtime"
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
	s.model.resetPaneViewport(result.paneID)
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

func (s *terminalBindingService) loadSnapshotCmd(terminalID string, offset, limit int) tea.Cmd {
	if s == nil || s.model == nil || s.model.runtime == nil || terminalID == "" {
		return nil
	}
	return func() tea.Msg {
		snapshot, err := s.model.runtime.LoadSnapshot(context.Background(), terminalID, offset, limit)
		if err != nil {
			return err
		}
		return orchestrator.SnapshotLoadedMsg{TerminalID: terminalID, Snapshot: snapshot}
	}
}

func (s *terminalBindingService) bindSelection(tabID, paneID string, item modal.PickerItem) (bindTerminalSelectionResult, error) {
	if s == nil || s.model == nil || s.model.workbench == nil || paneID == "" || item.TerminalID == "" {
		return bindTerminalSelectionResult{}, nil
	}
	service := s.model.paneBindingLifecycleService()
	if service == nil {
		return bindTerminalSelectionResult{}, nil
	}
	result, err := service.bindDetachedTerminal(bindDetachedTerminalRequest{
		TabID:      tabID,
		PaneID:     paneID,
		TerminalID: item.TerminalID,
		Binding: runtime.DetachedTerminalBinding{
			Name:     item.Name,
			Command:  append([]string(nil), item.CommandArgs...),
			Tags:     cloneStringMap(item.Tags),
			State:    terminalSelectionState(item),
			ExitCode: cloneIntPointer(item.ExitCode),
		},
	})
	if err != nil {
		return bindTerminalSelectionResult{}, err
	}
	return bindTerminalSelectionResult{
		tabID:             result.Target.TabID,
		paneID:            result.Target.PaneID,
		terminalID:        result.Target.TerminalID,
		loadSnapshotAfter: result.LoadSnapshotAfter,
	}, nil
}
