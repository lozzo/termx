package app

import (
	corepool "github.com/lozzow/termx/tui/core/pool"
	coreterminal "github.com/lozzow/termx/tui/core/terminal"
	"github.com/lozzow/termx/tui/core/types"
)

// Reduce 只做纯状态变更，不直接发起任何 client 或 runtime 调用。
func Reduce(model Model, input any) (Model, []Effect) {
	switch typed := input.(type) {
	case Intent:
		switch typed {
		case IntentOpenTerminalPool:
			model.Screen = ScreenTerminalPool
			return model, []Effect{EffectLoadTerminalPool{}}
		case IntentCloseScreen:
			model.Screen = ScreenWorkbench
		case IntentOpenConnectOverlay:
			model.Overlay = model.Overlay.OpenConnectPicker()
		}
	case MessageTerminalDisconnected:
		model.Workbench.MarkPaneDisconnected(typed.PaneID)
	case MessageTerminalExited:
		model.Workbench.MarkTerminalExited(typed.TerminalID)
	case MessageTerminalRemoved:
		model.Workbench.MarkTerminalRemoved(typed.TerminalID)
	case MessageTerminalPoolLoaded:
		model.Pool.ApplyGroups(corepool.BuildGroups(indexTerminalMetadata(typed.Terminals), model.Workbench.VisibleTerminalIDs(), model.Pool.Query))
	}
	return model, nil
}

func indexTerminalMetadata(items []coreterminal.Metadata) map[types.TerminalID]coreterminal.Metadata {
	out := make(map[types.TerminalID]coreterminal.Metadata, len(items))
	for _, item := range items {
		out[item.ID] = item
	}
	return out
}
