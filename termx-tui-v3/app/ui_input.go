package app

import (
	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

func NewUIInputReducer() Reducer {
	return func(root state.Root, msg Msg) (state.Root, []Effect) {
		inputMsg, ok := msg.(InputMsg)
		if !ok {
			return root, nil
		}
		intent := input.Route(inputMsg.Event, root.CopyMode.Active)
		switch intent.Kind {
		case input.IntentOpenTerminalPicker:
			root.Shell = root.Shell.OpenTerminalPicker()
			return root.Advance(), []Effect{handledEffect{}}
		case input.IntentPaneCommand:
			command, ok := PaneCommandFromIntent(intent)
			if !ok {
				return root, nil
			}
			return reducePaneCommand(root, command)
		default:
			return root, nil
		}
	}
}
