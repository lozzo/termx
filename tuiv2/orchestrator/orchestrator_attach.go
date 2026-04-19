package orchestrator

import (
	"fmt"

	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type TerminalAttachPlan struct {
	TabID      string
	PaneID     string
	TerminalID string
	Mode       string
}

func (o *Orchestrator) PlanAttachTerminal(tabID, paneID, terminalID, mode string) (TerminalAttachPlan, error) {
	if o == nil || o.workbench == nil {
		return TerminalAttachPlan{}, fmt.Errorf("orchestrator: attach target requires an active workbench")
	}
	if paneID == "" || terminalID == "" {
		return TerminalAttachPlan{}, fmt.Errorf("orchestrator: attach target requires pane and terminal ids")
	}
	resolvedTabID, err := o.workbench.ResolvePaneTab(tabID, paneID)
	if err != nil {
		return TerminalAttachPlan{}, err
	}
	return TerminalAttachPlan{
		TabID:      resolvedTabID,
		PaneID:     paneID,
		TerminalID: terminalID,
		Mode:       mode,
	}, nil
}

func (o *Orchestrator) PrepareSplitAttachTarget(sourcePaneID string) (tabID, paneID string, err error) {
	if o == nil || o.workbench == nil || sourcePaneID == "" {
		return "", "", fmt.Errorf("orchestrator: split attach target requires an active workbench and source pane")
	}
	tab := o.workbench.CurrentTab()
	if tab == nil {
		return "", "", fmt.Errorf("orchestrator: no current tab")
	}
	paneID = shared.NextPaneID()
	if err := o.workbench.SplitPane(tab.ID, sourcePaneID, paneID, workbench.SplitVertical); err != nil {
		return "", "", err
	}
	if err := o.workbench.FocusPane(tab.ID, paneID); err != nil {
		return "", "", err
	}
	return tab.ID, paneID, nil
}

func (o *Orchestrator) PrepareTabAttachTarget() (tabID, paneID string, err error) {
	if o == nil || o.workbench == nil {
		return "", "", fmt.Errorf("orchestrator: tab attach target requires an active workbench")
	}
	ws := o.workbench.CurrentWorkspace()
	if ws == nil {
		return "", "", fmt.Errorf("orchestrator: no current workspace")
	}
	tabID = shared.NextTabID()
	paneID = shared.NextPaneID()
	tabName := ws.NextAvailableTabName()
	if err := o.workbench.CreateTab(ws.Name, tabID, tabName); err != nil {
		return "", "", err
	}
	if err := o.workbench.CreateFirstPane(tabID, paneID); err != nil {
		return "", "", err
	}
	if err := o.workbench.SwitchTab(ws.Name, len(ws.Tabs)-1); err != nil {
		return "", "", err
	}
	return tabID, paneID, nil
}

func (o *Orchestrator) PrepareFloatingAttachTarget() (tabID, paneID string, err error) {
	if o == nil || o.workbench == nil {
		return "", "", fmt.Errorf("orchestrator: floating attach target requires an active workbench")
	}
	tab := o.workbench.CurrentTab()
	if tab == nil {
		return "", "", fmt.Errorf("orchestrator: no current tab")
	}
	paneID = shared.NextPaneID()
	if err := o.workbench.CreateFloatingPane(tab.ID, paneID, workbench.Rect{X: 10, Y: 5, W: 80, H: 24}); err != nil {
		return "", "", err
	}
	if err := o.workbench.FocusPane(tab.ID, paneID); err != nil {
		return "", "", err
	}
	return tab.ID, paneID, nil
}
