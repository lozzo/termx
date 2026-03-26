package app

// Reduce 只做纯状态变更，不直接发起任何 client 或 runtime 调用。
func Reduce(model Model, input any) (Model, []Effect) {
	switch typed := input.(type) {
	case Intent:
		switch typed {
		case IntentOpenTerminalPool:
			model.Screen = ScreenTerminalPool
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
	}
	return model, nil
}
