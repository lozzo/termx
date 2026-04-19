package render

func renderActiveOverlayVMWithCursor(vm RenderVM, termSize TermSize, cursorOffsetY int, cursorVisible bool) renderedBody {
	theme := uiThemeForRuntime(vm.Runtime)
	result := renderedBody{cursor: hideCursorANSI()}
	setLines := func(lines []string, blink bool) {
		result.lines = lines
		result.content = ""
		result.blink = blink
		result.meta = solidPresentMetadata(termSize.Width, len(lines), renderOwnerOpaqueOverlay)
	}
	switch vm.Overlay.Kind {
	case VisibleOverlayPrompt:
		setLines(renderPromptOverlayLinesWithThemeAndCursor(vm.Overlay.Prompt, termSize, theme, cursorVisible), true)
		if cursorVisible {
			if x, y, ok := promptOverlayCursorTarget(vm.Overlay.Prompt, termSize); ok {
				result.cursor = hostCursorANSI(x, y+cursorOffsetY, "bar", false)
			}
		}
	case VisibleOverlayPicker:
		setLines(renderPickerOverlayLinesWithThemeAndCursor(vm.Overlay.Picker, termSize, theme, cursorVisible), true)
		if cursorVisible {
			if x, y, ok := pickerOverlayCursorTarget(vm.Overlay.Picker, termSize); ok {
				result.cursor = hostCursorANSI(x, y+cursorOffsetY, "bar", false)
			}
		}
	case VisibleOverlayWorkspacePicker:
		setLines(renderWorkspacePickerOverlayLinesWithThemeAndCursor(vm.Overlay.WorkspacePicker, vm.Runtime, termSize, theme, cursorVisible), true)
		if cursorVisible {
			if x, y, ok := workspacePickerOverlayCursorTarget(vm.Overlay.WorkspacePicker, termSize); ok {
				result.cursor = hostCursorANSI(x, y+cursorOffsetY, "bar", false)
			}
		}
	case VisibleOverlayHelp:
		setLines(renderHelpOverlayLinesWithTheme(vm.Overlay.Help, termSize, theme), false)
	case VisibleOverlayFloatingOverview:
		setLines(renderFloatingOverviewOverlayLinesWithTheme(vm.Overlay.FloatingOverview, termSize, theme), false)
	default:
		return renderedBody{}
	}
	return result
}
