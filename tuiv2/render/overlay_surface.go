package render

func renderActiveOverlay(state VisibleRenderState, termSize TermSize) string {
	return renderActiveOverlayWithCursor(state, termSize, 0, true).content
}

func renderActiveOverlayWithCursor(state VisibleRenderState, termSize TermSize, cursorOffsetY int, cursorVisible bool) renderedBody {
	theme := uiThemeForState(state)
	result := renderedBody{cursor: hideCursorANSI()}
	switch state.Overlay.Kind {
	case VisibleOverlayPrompt:
		result.content = renderPromptOverlayWithThemeAndCursor(state.Overlay.Prompt, termSize, theme, cursorVisible)
		result.blink = true
		if cursorVisible {
			if x, y, ok := promptOverlayCursorTarget(state.Overlay.Prompt, termSize); ok {
				result.cursor = hostCursorANSI(x, y+cursorOffsetY, "bar", false)
			}
		}
	case VisibleOverlayPicker:
		result.content = renderPickerOverlayWithThemeAndCursor(state.Overlay.Picker, termSize, theme, cursorVisible)
		result.blink = true
		if cursorVisible {
			if x, y, ok := pickerOverlayCursorTarget(state.Overlay.Picker, termSize); ok {
				result.cursor = hostCursorANSI(x, y+cursorOffsetY, "bar", false)
			}
		}
	case VisibleOverlayWorkspacePicker:
		result.content = renderWorkspacePickerOverlayWithThemeAndCursor(state.Overlay.WorkspacePicker, state.Runtime, termSize, theme, cursorVisible)
		result.blink = true
		if cursorVisible {
			if x, y, ok := workspacePickerOverlayCursorTarget(state.Overlay.WorkspacePicker, termSize); ok {
				result.cursor = hostCursorANSI(x, y+cursorOffsetY, "bar", false)
			}
		}
	case VisibleOverlayTerminalManager:
		result.content = renderTerminalManagerOverlayWithThemeAndCursor(state.Overlay.TerminalManager, termSize, theme, cursorVisible)
		result.blink = true
		if cursorVisible {
			if x, y, ok := terminalManagerOverlayCursorTarget(state.Overlay.TerminalManager, termSize); ok {
				result.cursor = hostCursorANSI(x, y+cursorOffsetY, "bar", false)
			}
		}
	case VisibleOverlayHelp:
		result.content = renderHelpOverlayWithTheme(state.Overlay.Help, termSize, theme)
	case VisibleOverlayFloatingOverview:
		result.content = renderFloatingOverviewOverlayWithTheme(state.Overlay.FloatingOverview, termSize, theme)
	default:
		return renderedBody{}
	}
	return result
}
