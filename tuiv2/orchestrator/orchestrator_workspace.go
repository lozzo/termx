package orchestrator

import (
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/shared"
)

func (o *Orchestrator) handleWorkspaceAction(action input.SemanticAction) []Effect {
	switch action.Kind {
	case input.ActionSwitchWorkspace:
		if o.workbench != nil {
			_ = o.workbench.SwitchWorkspace(action.Text)
		}
		return []Effect{
			CloseModalEffect{Kind: input.ModeWorkspacePicker},
			SetInputModeEffect{Mode: input.ModeState{Kind: input.ModeNormal}},
			InvalidateRenderEffect{},
		}
	case input.ActionCreateWorkspace:
		if o.workbench == nil {
			return nil
		}
		name := "workspace-" + shared.GenerateShortID()
		_ = o.workbench.CreateWorkspace(name)
		_ = o.workbench.SwitchWorkspace(name)
		return []Effect{
			CloseModalEffect{Kind: input.ModeWorkspacePicker},
			SetInputModeEffect{Mode: input.ModeState{Kind: input.ModeNormal}},
			InvalidateRenderEffect{},
		}
	case input.ActionDeleteWorkspace:
		if o.workbench == nil {
			return nil
		}
		ws := o.workbench.CurrentWorkspace()
		if ws == nil {
			return nil
		}
		_ = o.workbench.DeleteWorkspace(ws.Name)
		return []Effect{
			SetInputModeEffect{Mode: input.ModeState{Kind: input.ModeNormal}},
			InvalidateRenderEffect{},
		}
	default:
		return nil
	}
}
