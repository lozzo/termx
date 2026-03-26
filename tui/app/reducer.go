package app

import (
	"maps"

	corepool "github.com/lozzow/termx/tui/core/pool"
	coreterminal "github.com/lozzow/termx/tui/core/terminal"
	featureterminalpool "github.com/lozzow/termx/tui/features/terminalpool"
	"github.com/lozzow/termx/tui/core/types"
)

// Reduce 只做纯状态变更，不直接发起任何 client 或 runtime 调用。
func Reduce(model Model, input any) (Model, []Effect) {
	switch typed := input.(type) {
	case SimpleIntent:
		switch typed {
		case IntentOpenTerminalPool:
			model.Screen = ScreenTerminalPool
			return model, []Effect{EffectLoadTerminalPool{}}
		case IntentCloseScreen:
			model.Screen = ScreenWorkbench
			model.Overlay = model.Overlay.Clear()
		case IntentOpenConnectOverlay:
			model.Overlay = model.Overlay.OpenConnectPicker(connectOverlayItems(model), connectOverlaySelected(model))
		case IntentOpenHelpOverlay:
			model.Overlay = model.Overlay.OpenHelp()
		case IntentDisconnectActivePane:
			pane := model.Workbench.ActivePane()
			if pane.ID != "" && pane.TerminalID != "" {
				return model, []Effect{EffectDisconnectPane{PaneID: pane.ID}, EffectLoadTerminalPool{}}
			}
		case IntentReconnectActivePane:
			pane := model.Workbench.ActivePane()
			if pane.TerminalID != "" {
				return model, []Effect{EffectReconnectTerminal{TerminalID: pane.TerminalID}, EffectLoadTerminalPool{}}
			}
		case IntentPoolSelectNext:
			model.Pool.SelectNext()
		case IntentPoolSelectPrev:
			model.Pool.SelectPrev()
		case IntentOverlaySelectNext:
			model.Overlay = model.Overlay.SelectNext()
		case IntentOverlaySelectPrev:
			model.Overlay = model.Overlay.SelectPrev()
		case IntentOverlayConfirmConnect:
			if model.Overlay.Active.Kind == "connect-picker" && model.Overlay.Active.Selected != "" {
				return model, []Effect{EffectConnectTerminal{TerminalID: model.Overlay.Active.Selected}, EffectLoadTerminalPool{}}
			}
		}
	case IntentConnectTerminal:
		return model, []Effect{EffectConnectTerminal{TerminalID: typed.TerminalID}, EffectLoadTerminalPool{}}
	case IntentKillSelectedTerminal:
		terminalID := typed.TerminalID
		if terminalID == "" {
			terminalID = model.Pool.SelectedTerminalID
		}
		if terminalID == "" {
			terminalID = model.Workbench.ActiveTerminalID()
		}
		if terminalID != "" {
			return model, []Effect{EffectKillTerminal{TerminalID: terminalID}, EffectLoadTerminalPool{}}
		}
	case IntentRemoveSelectedTerminal:
		terminalID := typed.TerminalID
		if terminalID == "" {
			terminalID = model.Pool.SelectedTerminalID
		}
		if terminalID == "" {
			terminalID = model.Workbench.ActiveTerminalID()
		}
		if terminalID != "" {
			return model, []Effect{EffectRemoveTerminal{TerminalID: terminalID}, EffectLoadTerminalPool{}}
		}
	case MessageTerminalDisconnected:
		model.Workbench.MarkPaneDisconnected(typed.PaneID)
		model.Pool.ApplyGroups(corepool.BuildGroups(indexTerminalMetadataFromWorkbench(model.Workbench.Terminals), model.Workbench.VisibleTerminalIDs(), model.Pool.Query))
	case MessageTerminalConnected:
		model.Workbench.BindActivePane(typed.Terminal)
		model.Workbench.SetSessionSnapshot(typed.Terminal.ID, typed.Snapshot)
		model.Overlay = model.Overlay.Clear()
		model.Pool.ApplyGroups(corepool.BuildGroups(indexTerminalMetadataFromWorkbench(model.Workbench.Terminals), model.Workbench.VisibleTerminalIDs(), model.Pool.Query))
	case MessageTerminalExited:
		model.Workbench.MarkTerminalExited(typed.TerminalID)
		model.Pool.ApplyGroups(corepool.BuildGroups(indexTerminalMetadataFromWorkbench(model.Workbench.Terminals), model.Workbench.VisibleTerminalIDs(), model.Pool.Query))
	case MessageTerminalRemoved:
		model.Workbench.MarkTerminalRemoved(typed.TerminalID)
		model.Pool.ApplyGroups(corepool.BuildGroups(indexTerminalMetadataFromWorkbench(model.Workbench.Terminals), model.Workbench.VisibleTerminalIDs(), model.Pool.Query))
	case MessageTerminalPoolLoaded:
		merged := mergeTerminalMetadata(indexTerminalMetadata(typed.Terminals), model.Workbench.Terminals)
		model.Pool.ApplyGroups(corepool.BuildGroups(merged, model.Workbench.VisibleTerminalIDs(), model.Pool.Query))
		if model.Overlay.Active.Kind == "connect-picker" {
			model.Overlay = model.Overlay.OpenConnectPicker(connectOverlayItems(model), connectOverlaySelected(model))
		}
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

func indexTerminalMetadataFromWorkbench(items map[types.TerminalID]coreterminal.Metadata) map[types.TerminalID]coreterminal.Metadata {
	out := make(map[types.TerminalID]coreterminal.Metadata, len(items))
	maps.Copy(out, items)
	return out
}

func mergeTerminalMetadata(base map[types.TerminalID]coreterminal.Metadata, overlay map[types.TerminalID]coreterminal.Metadata) map[types.TerminalID]coreterminal.Metadata {
	maps.Copy(base, overlay)
	return base
}

func connectOverlayItems(model Model) []featureterminalpool.Item {
	return append(append(append([]featureterminalpool.Item{}, model.Pool.Visible...), model.Pool.Parked...), model.Pool.Exited...)
}

func connectOverlaySelected(model Model) types.TerminalID {
	if model.Overlay.Active.Selected != "" {
		return model.Overlay.Active.Selected
	}
	if model.Pool.SelectedTerminalID != "" {
		return model.Pool.SelectedTerminalID
	}
	items := connectOverlayItems(model)
	if len(items) > 0 {
		return items[0].ID
	}
	return ""
}
