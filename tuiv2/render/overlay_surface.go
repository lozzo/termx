package render

func renderActiveOverlay(state VisibleRenderState, termSize TermSize) string {
	return renderActiveOverlayWithCursor(state, termSize, 0, true).content
}

func renderActiveOverlayWithCursor(state VisibleRenderState, termSize TermSize, cursorOffsetY int, cursorVisible bool) renderedBody {
	return renderActiveOverlayVMWithCursor(RenderVMFromVisibleState(state), termSize, cursorOffsetY, cursorVisible)
}

func renderActiveOverlayVM(vm RenderVM, termSize TermSize) string {
	return renderActiveOverlayVMWithCursor(vm, termSize, 0, true).content
}

func renderActiveOverlayVMWithCursor(vm RenderVM, termSize TermSize, cursorOffsetY int, cursorVisible bool) renderedBody {
	theme := uiThemeForRuntime(vm.Runtime)
	result := renderedBody{cursor: hideCursorANSI()}
	switch vm.Overlay.Kind {
	case VisibleOverlayPrompt:
		result.content = renderPromptOverlayWithThemeAndCursor(vm.Overlay.Prompt, termSize, theme, cursorVisible)
		result.blink = true
		if cursorVisible {
			if x, y, ok := promptOverlayCursorTarget(vm.Overlay.Prompt, termSize); ok {
				result.cursor = hostCursorANSI(x, y+cursorOffsetY, "bar", false)
			}
		}
	case VisibleOverlayPicker:
		result.content = renderPickerOverlayWithThemeAndCursor(vm.Overlay.Picker, termSize, theme, cursorVisible)
		result.blink = true
		if cursorVisible {
			if x, y, ok := pickerOverlayCursorTarget(vm.Overlay.Picker, termSize); ok {
				result.cursor = hostCursorANSI(x, y+cursorOffsetY, "bar", false)
			}
		}
	case VisibleOverlayWorkspacePicker:
		result.content = renderWorkspacePickerOverlayWithThemeAndCursor(vm.Overlay.WorkspacePicker, vm.Runtime, termSize, theme, cursorVisible)
		result.blink = true
		if cursorVisible {
			if x, y, ok := workspacePickerOverlayCursorTarget(vm.Overlay.WorkspacePicker, termSize); ok {
				result.cursor = hostCursorANSI(x, y+cursorOffsetY, "bar", false)
			}
		}
	case VisibleOverlayTerminalManager:
		result.content = renderTerminalManagerOverlayWithThemeAndCursor(vm.Overlay.TerminalManager, termSize, theme, cursorVisible)
		result.blink = true
		if cursorVisible {
			if x, y, ok := terminalManagerOverlayCursorTarget(vm.Overlay.TerminalManager, termSize); ok {
				result.cursor = hostCursorANSI(x, y+cursorOffsetY, "bar", false)
			}
		}
	case VisibleOverlayHelp:
		result.content = renderHelpOverlayWithTheme(vm.Overlay.Help, termSize, theme)
	case VisibleOverlayFloatingOverview:
		result.content = renderFloatingOverviewOverlayWithTheme(vm.Overlay.FloatingOverview, termSize, theme)
	default:
		return renderedBody{}
	}
	return result
}
