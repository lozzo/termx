package app

// Reduce 只做纯状态变更，不直接发起任何 client 或 runtime 调用。
func Reduce(model Model, intent Intent) (Model, []Effect) {
	switch intent {
	case IntentOpenTerminalPool:
		model.Screen = ScreenTerminalPool
	case IntentCloseScreen:
		model.Screen = ScreenWorkbench
	}
	return model, nil
}
