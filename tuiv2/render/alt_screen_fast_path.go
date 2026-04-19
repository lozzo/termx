package render

import (
	"strings"

	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func renderAltScreenFastPathVM(vm RenderVM, entries []paneRenderEntry, cursorOffsetY int) (renderedBody, bool) {
	if vm.Surface.Kind != VisibleSurfaceWorkbench || vm.Workbench == nil || vm.Runtime == nil {
		return renderedBody{}, false
	}
	if len(vm.Workbench.FloatingPanes) > 0 || len(entries) != 1 {
		return renderedBody{}, false
	}
	entry := entries[0]
	if !entry.Active || entry.TerminalID == "" || entry.Floating || entry.CopyModeActive || entry.ScrollOffset > 0 {
		return renderedBody{}, false
	}
	if !entry.Frameless {
		return renderedBody{}, false
	}
	if entry.EmptyActionSelected >= 0 || entry.ExitedActionSelected >= 0 {
		return renderedBody{}, false
	}
	if entry.Overflow.Right || entry.Overflow.Bottom {
		return renderedBody{}, false
	}
	resolved := resolvePaneContent(entry, vm.Runtime, false)
	if resolved.source == nil {
		return renderedBody{}, false
	}
	if !resolved.source.IsAlternateScreen() && !resolved.source.Modes().AlternateScreen {
		return renderedBody{}, false
	}

	perftrace.Count("render.body.alt_screen_fast_path", entry.Rect.W*entry.Rect.H)
	return renderAltScreenEntryFastPath(entry, resolved, vm.Runtime, cursorOffsetY), true
}

func renderAltScreenEntryFastPath(entry paneRenderEntry, resolved resolvedPaneContent, runtimeState *VisibleRuntimeStateProxy, cursorOffsetY int) renderedBody {
	lines := make([]string, 0, entry.Rect.H)
	if !entry.Frameless {
		lines = append(lines, renderAltScreenTopBorderLine(entry))
	}

	cursor := hideCursorANSI()
	cursorTarget, cursorOK := entryCursorRenderTarget(resolved.contentRect, resolved.source)
	cursorRow := -1
	cursorCol := -1
	cursorShape := ""
	if cursorOK {
		cursor = hostHiddenCursorANSI(cursorTarget.X, cursorTarget.Y+cursorOffsetY, cursorTarget.Shape, cursorTarget.Blink)
		if cursorTarget.Visible {
			cursorRow = cursorTarget.Y - resolved.contentRect.Y
			cursorCol = cursorTarget.X - resolved.contentRect.X
			cursorShape = cursorTarget.Shape
		}
	}

	emojiMode := emojiVariationSelectorModeForRuntime(runtimeState)
	base := resolved.source.ScrollbackRows()
	for row := 0; row < resolved.contentRect.H; row++ {
		var cells []protocol.Cell
		if row < resolved.source.ScreenRows() {
			cells = resolved.source.Row(base + row)
		}
		content := protocolViewportRowANSI(cells, resolved.contentRect.W, emojiMode, cursorCol, row == cursorRow, cursorShape)
		if entry.Frameless {
			lines = append(lines, wrapRenderedRowANSI(content))
			continue
		}
		lines = append(lines, renderAltScreenBorderedContentLine(entry, content))
	}

	if !entry.Frameless {
		lines = append(lines, renderAltScreenBottomBorderLine(entry))
	}

	return renderedBody{
		lines:  lines,
		cursor: cursor,
	}
}

func paneChromeStylesForEntry(entry paneRenderEntry) (drawStyle, paneChromeDrawStyles) {
	borderFG := entry.Theme.panelBorder2
	titleFG := entry.Theme.panelMuted
	metaFG := entry.Theme.panelMuted
	actionFG := entry.Theme.panelMuted
	stateFG := entry.Theme.panelMuted
	if entry.Active {
		borderFG = entry.Theme.chromeAccent
		titleFG = entry.Theme.panelText
		actionFG = entry.Theme.panelText
		switch entry.Border.StateTone {
		case "success":
			stateFG = entry.Theme.success
		case "warning":
			stateFG = entry.Theme.warning
		case "danger":
			stateFG = entry.Theme.danger
		default:
			stateFG = metaFG
		}
	}
	borderStyle := drawStyle{FG: borderFG}
	return borderStyle, paneChromeDrawStyles{
		Title:         drawStyle{FG: titleFG, Bold: true},
		Meta:          drawStyle{FG: metaFG},
		State:         drawStyle{FG: stateFG},
		Action:        drawStyle{FG: actionFG, Bold: entry.Active},
		EmphasizeRole: entry.Active,
	}
}

func renderAltScreenTopBorderLine(entry paneRenderEntry) string {
	if entry.Rect.W <= 0 {
		return ""
	}
	borderStyle, chromeStyles := paneChromeStylesForEntry(entry)
	canvas := newComposedCanvas(entry.Rect.W, 1)
	for x := 0; x < entry.Rect.W; x++ {
		glyph := "─"
		switch x {
		case 0:
			glyph = "┌"
		case entry.Rect.W - 1:
			glyph = "┐"
		}
		canvas.set(x, 0, drawCell{Content: glyph, Width: 1, Style: borderStyle})
	}
	localRect := workbench.Rect{X: 0, Y: 0, W: entry.Rect.W, H: entry.Rect.H}
	layout, ok := paneTopBorderLabelsLayout(localRect, entry.Title, entry.Border, paneChromeActionTokensForFrame(localRect, entry.Title, entry.Border, entry.Floating))
	if ok {
		for _, slot := range layout.actionSlots {
			drawBorderLabel(canvas, slot.X, 0, slot.Label, chromeStyles.Action)
		}
		if layout.titleLabel != "" {
			drawBorderLabel(canvas, layout.titleX, 0, layout.titleLabel, chromeStyles.Title)
		}
		if layout.stateLabel != "" {
			drawBorderLabel(canvas, layout.stateX, 0, layout.stateLabel, chromeStyles.State)
		}
		if layout.shareLabel != "" {
			drawBorderLabel(canvas, layout.shareX, 0, layout.shareLabel, chromeStyles.Meta)
		}
		if layout.roleLabel != "" {
			roleStyle := chromeStyles.Meta
			if chromeStyles.EmphasizeRole {
				roleStyle = chromeStyles.Action
			}
			drawBorderLabel(canvas, layout.roleX, 0, layout.roleLabel, roleStyle)
		}
		if layout.copyTimeLabel != "" {
			drawBorderLabel(canvas, layout.copyTimeX, 0, layout.copyTimeLabel, chromeStyles.Meta)
		}
		if layout.copyRowLabel != "" {
			drawBorderLabel(canvas, layout.copyRowX, 0, layout.copyRowLabel, chromeStyles.Meta)
		}
	}
	return canvas.cachedContentLines()[0]
}

func renderAltScreenBottomBorderLine(entry paneRenderEntry) string {
	if entry.Rect.W <= 0 {
		return ""
	}
	borderStyle, _ := paneChromeStylesForEntry(entry)
	canvas := newComposedCanvas(entry.Rect.W, 1)
	for x := 0; x < entry.Rect.W; x++ {
		glyph := "─"
		switch x {
		case 0:
			glyph = "└"
		case entry.Rect.W - 1:
			glyph = "┘"
		}
		canvas.set(x, 0, drawCell{Content: glyph, Width: 1, Style: borderStyle})
	}
	return canvas.cachedContentLines()[0]
}

func renderAltScreenBorderedContentLine(entry paneRenderEntry, content string) string {
	borderStyle, _ := paneChromeStylesForEntry(entry)
	var line strings.Builder
	line.Grow(len(content) + 32)
	line.WriteString(styleANSI(borderStyle))
	line.WriteString("│")
	line.WriteString("\x1b[0m")
	line.WriteString(content)
	line.WriteString(styleANSI(borderStyle))
	line.WriteString("│")
	return wrapRenderedRowANSI(line.String())
}

func protocolViewportRowANSI(row []protocol.Cell, width int, emojiMode shared.AmbiguousEmojiVariationSelectorMode, cursorCol int, cursorVisible bool, cursorShape string) string {
	return protocolRowANSIWithOptions(row, width, protocolRowANSIOptions{
		emojiMode:     emojiMode,
		cursorCol:     cursorCol,
		cursorVisible: cursorVisible,
		cursorShape:   cursorShape,
	})
}

func syntheticCursorDrawStyle(style drawStyle, shape string) drawStyle {
	style.Reverse = false
	style.FG = "#000000"
	style.BG = "#ffffff"
	switch shape {
	case "underline":
		style.Underline = true
	case "bar":
		style.Bold = true
	}
	return style
}

func wrapRenderedRowANSI(content string) string {
	return content + "\x1b[0m\x1b[K"
}
