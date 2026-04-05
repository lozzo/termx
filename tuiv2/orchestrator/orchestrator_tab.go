package orchestrator

import (
	"fmt"

	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/shared"
)

func (o *Orchestrator) handleTabAction(action input.SemanticAction) []Effect {
	switch action.Kind {
	case input.ActionCreateTab:
		ws := o.workbench.CurrentWorkspace()
		if ws == nil {
			return nil
		}
		tabID := shared.NextTabID()
		tabName := fmt.Sprintf("%d", len(ws.Tabs)+1)
		paneID := shared.NextPaneID()
		_ = o.workbench.CreateTab(ws.Name, tabID, tabName)
		_ = o.workbench.CreateFirstPane(tabID, paneID)
		_ = o.workbench.SwitchTab(ws.Name, len(ws.Tabs)-1)
		return []Effect{
			InvalidateRenderEffect{},
			OpenPickerEffect{RequestID: paneID},
			SetInputModeEffect{Mode: input.ModeState{Kind: input.ModePicker, RequestID: paneID}},
		}
	case input.ActionNextTab, input.ActionPrevTab:
		ws := o.workbench.CurrentWorkspace()
		if ws == nil || len(ws.Tabs) == 0 {
			return nil
		}
		delta := 1
		if action.Kind == input.ActionPrevTab {
			delta = -1
		}
		next := (ws.ActiveTab + delta + len(ws.Tabs)) % len(ws.Tabs)
		_ = o.workbench.SwitchTab(ws.Name, next)
		return []Effect{SwitchTabEffect{Delta: delta}}
	case input.ActionCloseTab:
		tabID := action.TabID
		if tabID == "" {
			tab := o.workbench.CurrentTab()
			if tab == nil {
				return nil
			}
			tabID = tab.ID
		}
		_ = o.workbench.CloseTab(tabID)
		return []Effect{CloseTabEffect{TabID: tabID}}
	default:
		return nil
	}
}
