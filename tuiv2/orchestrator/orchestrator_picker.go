package orchestrator

import "github.com/lozzow/termx/tuiv2/input"

func (o *Orchestrator) handlePickerAction(action input.SemanticAction) []Effect {
	switch action.Kind {
	case input.ActionOpenPicker:
		return []Effect{
			SetInputModeEffect{Mode: input.ModeState{Kind: input.ModePicker, RequestID: action.TargetID}},
			OpenPickerEffect{RequestID: action.TargetID},
		}
	case input.ActionOpenWorkspacePicker:
		return []Effect{
			SetInputModeEffect{Mode: input.ModeState{Kind: input.ModeWorkspacePicker, RequestID: action.TargetID}},
			OpenWorkspacePickerEffect{RequestID: action.TargetID},
			LoadWorkspaceItemsEffect{},
		}
	case input.ActionSubmitPrompt:
		return []Effect{
			AttachTerminalEffect{
				PaneID:     action.PaneID,
				TerminalID: action.TargetID,
				Mode:       "collaborator",
			},
		}
	default:
		return nil
	}
}
