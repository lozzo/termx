package orchestrator

import (
	"context"

	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type Orchestrator struct {
	workbench *workbench.Workbench
	runtime   *runtime.Runtime
}

const (
	floatingMoveStep   = 2
	floatingResizeStep = 2
	floatingBoundsW    = 200
	floatingBoundsH    = 50
)

func New(wb *workbench.Workbench, rt *runtime.Runtime, _ ...any) *Orchestrator {
	return &Orchestrator{workbench: wb, runtime: rt}
}

func (o *Orchestrator) HandleSemanticAction(action input.SemanticAction) []Effect {
	switch action.Kind {
	case input.ActionOpenPicker, input.ActionOpenWorkspacePicker, input.ActionSubmitPrompt:
		return o.handlePickerAction(action)
	case input.ActionSplitPane, input.ActionSplitPaneHorizontal,
		input.ActionFocusPaneLeft, input.ActionFocusPaneRight, input.ActionFocusPaneUp, input.ActionFocusPaneDown,
		input.ActionClosePane, input.ActionDetachPane, input.ActionReconnectPane, input.ActionClosePaneKill,
		input.ActionZoomPane,
		input.ActionResizePaneLeft, input.ActionResizePaneRight, input.ActionResizePaneUp, input.ActionResizePaneDown,
		input.ActionResizePaneLargeLeft, input.ActionResizePaneLargeRight, input.ActionResizePaneLargeUp, input.ActionResizePaneLargeDown,
		input.ActionBalancePanes, input.ActionCycleLayout:
		return o.handlePaneAction(action)
	case input.ActionCreateTab, input.ActionNextTab, input.ActionPrevTab, input.ActionCloseTab:
		return o.handleTabAction(action)
	case input.ActionSwitchWorkspace, input.ActionCreateWorkspace, input.ActionDeleteWorkspace:
		return o.handleWorkspaceAction(action)
	case input.ActionCreateFloatingPane,
		input.ActionFocusPrevFloatingPane, input.ActionFocusNextFloatingPane,
		input.ActionMoveFloatingLeft, input.ActionMoveFloatingRight, input.ActionMoveFloatingUp, input.ActionMoveFloatingDown,
		input.ActionResizeFloatingLeft, input.ActionResizeFloatingRight, input.ActionResizeFloatingUp, input.ActionResizeFloatingDown,
		input.ActionCenterFloatingPane, input.ActionToggleFloatingVisibility, input.ActionCloseFloatingPane:
		return o.handleFloatingAction(action)
	case input.ActionKillTerminal:
		if action.TargetID == "" {
			return nil
		}
		return []Effect{KillTerminalEffect{TerminalID: action.TargetID}}
	default:
		return nil
	}
}

func (o *Orchestrator) AttachAndLoadSnapshot(ctx context.Context, paneID, terminalID, mode string, offset, limit int) ([]any, error) {
	terminal, err := o.runtime.AttachTerminal(ctx, paneID, terminalID, mode)
	if err != nil {
		return nil, err
	}
	o.bindWorkbenchPaneTerminal(paneID, terminalID)
	snapshot, err := o.runtime.LoadSnapshot(ctx, terminalID, offset, limit)
	if err != nil {
		return nil, err
	}
	msgs := []any{
		TerminalAttachedMsg{PaneID: paneID, TerminalID: terminalID, Channel: terminal.Channel},
		SnapshotLoadedMsg{PaneID: paneID, TerminalID: terminalID, Snapshot: snapshot},
	}
	return msgs, nil
}

func (o *Orchestrator) bindWorkbenchPaneTerminal(paneID, terminalID string) {
	if o == nil || o.workbench == nil || paneID == "" || terminalID == "" {
		return
	}
	workspace := o.workbench.CurrentWorkspace()
	if workspace == nil {
		return
	}
	for _, tab := range workspace.Tabs {
		if tab == nil || tab.Panes[paneID] == nil {
			continue
		}
		_ = o.workbench.BindPaneTerminal(tab.ID, paneID, terminalID)
		_ = o.workbench.FocusPane(tab.ID, paneID)
		return
	}
}
