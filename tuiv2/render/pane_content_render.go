package render

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/workbench"
)

// drawPaneContent fills the interior of a pane with terminal snapshot content.
func drawPaneContent(canvas *composedCanvas, rect workbench.Rect, pane workbench.VisiblePane, lookup runtimeLookup, scrollOffset int, active bool) {
	if rect.W < 3 || rect.H < 3 {
		return
	}
	contentRect := contentRectForPaneEdges(rect, pane.SharedLeft, pane.SharedTop)
	// Clear the full framed interior, not just the terminal content rect. The
	// reserved right gutter intentionally sits outside contentRect so that pane
	// borders stay visually stable; if we only clear contentRect, stale border
	// glyphs can survive in that gutter and reappear as duplicate right edges.
	fillRect(canvas, interiorRectForPaneEdges(rect, pane.SharedLeft, pane.SharedTop), blankDrawCell())

	if pane.TerminalID == "" {
		drawEmptyPaneContent(canvas, contentRect, pane.ID, pane.TerminalID, defaultUITheme(), -1)
		return
	}

	terminal := lookup.terminal(pane.TerminalID)
	if terminal == nil {
		drawEmptyPaneContent(canvas, contentRect, pane.ID, pane.TerminalID, defaultUITheme(), -1)
		return
	}
	source := renderSource(terminal.Snapshot, terminal.Surface)
	if source == nil || source.ScreenRows() == 0 {
		canvas.drawText(contentRect.X, contentRect.Y, terminal.Name+" ["+terminal.State+"]", drawStyle{FG: defaultUITheme().panelMuted})
		if terminal.State == "exited" {
			drawExitedPaneRecoveryHints(canvas, contentRect, defaultUITheme(), -1, true)
		}
		return
	}
	drawTerminalSourceWithOffset(canvas, contentRect, source, scrollOffset, defaultUITheme())
	if active {
		projectPaneCursorSource(canvas, contentRect, source, scrollOffset)
	}
	if terminal.State == "exited" {
		drawExitedPaneRecoveryHints(canvas, contentRect, defaultUITheme(), -1, true)
	}
}

func drawPaneContentWithKey(canvas *composedCanvas, rect workbench.Rect, entry paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) {
	contentRect := contentRectForEntry(entry)
	// Keep the cached redraw path on the same invariant as drawPaneContent():
	// every content repaint owns the whole framed interior, including the
	// reserved gutter column.
	fillRect(canvas, interiorRectForEntry(entry), blankDrawCell())
	if entry.TerminalID == "" {
		drawEmptyPaneContent(canvas, contentRect, entry.PaneID, entry.TerminalID, entry.Theme, entry.EmptyActionSelected)
		return
	}
	terminal := findVisibleTerminal(runtimeState, entry.TerminalID)
	if terminal == nil {
		drawEmptyPaneContent(canvas, contentRect, entry.PaneID, entry.TerminalID, entry.Theme, -1)
		return
	}
	snapshot := entry.Snapshot
	surface := entry.Surface
	if snapshot == nil && surface == nil {
		surface = terminal.Surface
	}
	if snapshot == nil && surface == nil {
		snapshot = terminal.Snapshot
	}
	source := renderSource(snapshot, surface)
	if source == nil || source.ScreenRows() == 0 {
		canvas.drawText(contentRect.X, contentRect.Y, terminal.Name+" ["+terminal.State+"]", drawStyle{FG: entry.Theme.panelMuted})
		if terminal.State == "exited" {
			drawExitedPaneRecoveryHints(canvas, contentRect, entry.Theme, entry.ExitedActionSelected, entry.ExitedActionPulse)
		}
		return
	}
	renderOffset := entry.ScrollOffset
	if entry.CopyModeActive {
		renderOffset = scrollOffsetForViewportTop(snapshot, contentRect.H, entry.CopyModeViewTopRow)
	}
	drawTerminalSourceWithOffset(canvas, contentRect, source, renderOffset, entry.Theme)
	if entry.CopyModeActive {
		drawCopyModeOverlay(canvas, contentRect, snapshot, entry.Theme, entry.CopyModeCursorRow, entry.CopyModeCursorCol, entry.CopyModeViewTopRow, entry.CopyModeMarkSet, entry.CopyModeMarkRow, entry.CopyModeMarkCol)
	}
	if terminal.State == "exited" {
		drawExitedPaneRecoveryHints(canvas, contentRect, entry.Theme, entry.ExitedActionSelected, entry.ExitedActionPulse)
	}
}

func contentRectForPane(rect workbench.Rect) workbench.Rect {
	content, _ := workbench.FramedPaneContentRect(rect, false, false)
	return content
}

func interiorRectForPane(rect workbench.Rect) workbench.Rect {
	return interiorRectForPaneEdges(rect, false, false)
}

func interiorRectForPaneEdges(rect workbench.Rect, sharedLeft, sharedTop bool) workbench.Rect {
	_ = sharedLeft
	_ = sharedTop
	interior := workbench.Rect{X: rect.X + 1, Y: rect.Y + 1, W: rect.W - 2, H: rect.H - 2}
	return interior
}

func contentRectForPaneEdges(rect workbench.Rect, sharedLeft, sharedTop bool) workbench.Rect {
	content, _ := workbench.FramedPaneContentRect(rect, sharedLeft, sharedTop)
	return content
}

func interiorRectForEntry(entry paneRenderEntry) workbench.Rect {
	if entry.Frameless {
		return entry.Rect
	}
	return interiorRectForPaneEdges(entry.Rect, entry.SharedLeft, entry.SharedTop)
}

func contentRectForEntry(entry paneRenderEntry) workbench.Rect {
	if entry.Frameless {
		return entry.Rect
	}
	return contentRectForPaneEdges(entry.Rect, entry.SharedLeft, entry.SharedTop)
}

func localContentRectForEntry(entry paneRenderEntry) workbench.Rect {
	interior := interiorRectForEntry(entry)
	content := contentRectForEntry(entry)
	return workbench.Rect{
		X: content.X - interior.X,
		Y: content.Y - interior.Y,
		W: content.W,
		H: content.H,
	}
}

func drawEmptyPaneContent(canvas *composedCanvas, rect workbench.Rect, paneID, terminalID string, theme uiTheme, selectedIndex int) {
	if canvas == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	actions := layoutEmptyPaneActions(rect, paneID)
	if len(actions) == 0 {
		return
	}

	headline := "No terminal attached"
	if strings.TrimSpace(terminalID) != "" {
		headline = "Terminal unavailable"
	}
	firstActionY := actions[0].rowRect.Y
	headlineY := firstActionY - 1
	if headlineY >= rect.Y {
		headlineStyle := drawStyle{FG: theme.panelText}
		canvas.drawText(rect.X, headlineY, centerText(xansi.Truncate(headline, rect.W, ""), rect.W), headlineStyle)
	}

	for index, item := range actions {
		style := emptyPaneActionDrawStyle(theme, item.spec.Kind, index == selectedIndex)
		lineText := centerText(xansi.Truncate(wrapEmptyPaneActionLabel(item.spec, index == selectedIndex), rect.W, ""), rect.W)
		canvas.drawText(item.rowRect.X, item.rowRect.Y, lineText, style)
	}

	if strings.TrimSpace(terminalID) != "" {
		lastActionY := actions[len(actions)-1].rowRect.Y
		terminalLineY := lastActionY + 1
		if terminalLineY < rect.Y+rect.H {
			line := centerText(xansi.Truncate("terminal="+terminalID, rect.W, ""), rect.W)
			canvas.drawText(rect.X, terminalLineY, line, drawStyle{FG: theme.panelMuted})
		}
	}
}

func drawExitedPaneRecoveryHints(canvas *composedCanvas, rect workbench.Rect, theme uiTheme, selectedIndex int, pulse bool) {
	if canvas == nil || rect.W <= 0 || rect.H < 2 {
		return
	}
	actions := layoutExitedPaneRecoveryActions(rect, "pane")
	if len(actions) == 0 {
		return
	}
	if rect.H >= len(actions)+1 {
		headlineY := actions[0].rowRect.Y - 1
		headline := centerText(xansi.Truncate("last output", rect.W, ""), rect.W)
		canvas.drawText(rect.X, headlineY, headline, drawStyle{FG: theme.panelText, Bold: true})
	}
	for index, item := range actions {
		style := exitedPaneActionDrawStyle(theme, item.spec.Kind, index == selectedIndex)
		text := centerText(xansi.Truncate(wrapExitedPaneActionLabel(item.spec, index == selectedIndex, pulse), rect.W, ""), rect.W)
		canvas.drawText(item.rowRect.X, item.rowRect.Y, text, style)
	}
}

func exitedPaneActionDrawStyle(theme uiTheme, kind HitRegionKind, selected bool) drawStyle {
	accent := theme.panelText
	switch kind {
	case HitRegionExitedPaneRestart:
		accent = theme.success
	case HitRegionExitedPaneChoose:
		accent = theme.chromeAccent
	}
	if selected {
		return drawStyle{FG: ensureContrast(mixHex(accent, theme.panelText, 0.15), theme.hostBG, 4.0), Bold: true}
	}
	return drawStyle{FG: ensureContrast(accent, theme.hostBG, 3.8), Bold: true}
}

func emptyPaneActionDrawStyle(theme uiTheme, kind HitRegionKind, selected bool) drawStyle {
	accent := theme.panelText
	switch kind {
	case HitRegionEmptyPaneAttach:
		accent = theme.chromeAccent
	case HitRegionEmptyPaneCreate:
		accent = theme.success
	case HitRegionEmptyPaneManager:
		accent = theme.panelText
	case HitRegionEmptyPaneClose:
		accent = theme.danger
	}
	if selected {
		return drawStyle{FG: ensureContrast(mixHex(accent, theme.panelText, 0.2), theme.hostBG, 4.0), Bold: true}
	}
	return drawStyle{FG: ensureContrast(accent, theme.hostBG, 3.8), Bold: kind != HitRegionEmptyPaneManager}
}
