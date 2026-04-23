package render

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type resolvedPaneContent struct {
	terminalKnown bool
	terminalName  string
	terminalState string
	snapshot      *protocol.Snapshot
	surface       runtime.TerminalSurface
	screenUpdate  terminalScreenUpdateHint
	source        terminalRenderSource
	metrics       renderTerminalMetrics
	contentRect   workbench.Rect
	renderOffset  int
}

type terminalSourceWindowState struct {
	rowIndices        []int
	exactRowHashes    []uint64
	rowContentHashes  []uint64
	rowIdentityHashes []uint64
	contentHash       uint64
	screenWindow      bool
}

func drawPaneContentWithKey(canvas *composedCanvas, rect workbench.Rect, entry paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) {
	resolved := resolvePaneContent(entry, runtimeState, false)
	contentRect := resolved.contentRect
	// Keep the cached redraw path on the same invariant as drawPaneContent():
	// every content repaint owns the whole framed interior.
	fillRect(canvas, interiorRectForEntry(entry), blankDrawCell())
	if entry.TerminalID == "" {
		drawEmptyPaneContent(canvas, contentRect, entry.PaneID, entry.TerminalID, entry.Theme, entry.EmptyActionSelected)
		return
	}
	if !resolved.terminalKnown {
		drawEmptyPaneContent(canvas, contentRect, entry.PaneID, entry.TerminalID, entry.Theme, -1)
		return
	}
	if resolved.source == nil || resolved.source.ScreenRows() == 0 {
		canvas.drawText(contentRect.X, contentRect.Y, resolved.terminalName+" ["+resolved.terminalState+"]", drawStyle{FG: entry.Theme.panelMuted})
		if resolved.terminalState == "exited" {
			drawExitedPaneRecoveryHints(canvas, contentRect, entry.Theme, entry.ExitedActionSelected, entry.ExitedActionPulse)
		}
		return
	}
	drawTerminalSourceWithOffsetAndMetrics(canvas, contentRect, resolved.source, resolved.renderOffset, entry.Theme, resolved.metrics)
	if entry.CopyModeActive {
		drawCopyModeOverlay(canvas, contentRect, resolved.snapshot, entry.Theme, entry.CopyModeCursorRow, entry.CopyModeCursorCol, entry.CopyModeViewTopRow, entry.CopyModeMarkSet, entry.CopyModeMarkRow, entry.CopyModeMarkCol)
	}
	if resolved.terminalState == "exited" {
		drawExitedPaneRecoveryHints(canvas, contentRect, entry.Theme, entry.ExitedActionSelected, entry.ExitedActionPulse)
	}
}

func contentRectForPane(rect workbench.Rect) workbench.Rect {
	content, _ := workbench.FramedPaneContentRect(rect, false, false)
	return content
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

func resolvePaneContent(entry paneRenderEntry, runtimeState *VisibleRuntimeStateProxy, local bool) resolvedPaneContent {
	resolved := resolvedPaneContent{}
	if local {
		resolved.contentRect = localContentRectForEntry(entry)
	} else {
		resolved.contentRect = contentRectForEntry(entry)
	}
	if entry.TerminalID == "" {
		return resolved
	}
	resolved.terminalKnown = entry.ContentKey.TerminalKnown
	if !resolved.terminalKnown {
		return resolved
	}
	resolved.terminalName = entry.ContentKey.Name
	resolved.terminalState = entry.ContentKey.State
	resolved.snapshot = entry.Snapshot
	resolved.surface = entry.Surface
	if terminal := findVisibleTerminal(runtimeState, entry.TerminalID); terminal != nil {
		resolved.screenUpdate = terminalScreenUpdateHintFromVisible(terminal.ScreenUpdate)
	}
	if resolved.terminalKnown && (resolved.snapshot == nil && resolved.surface == nil || resolved.terminalName == "" || resolved.terminalState == "") {
		if terminal := findVisibleTerminal(runtimeState, entry.TerminalID); terminal != nil {
			if resolved.snapshot == nil && resolved.surface == nil {
				resolved.surface = terminal.Surface
				if resolved.surface == nil {
					resolved.snapshot = terminal.Snapshot
				}
			}
			if resolved.terminalName == "" {
				resolved.terminalName = terminal.Name
			}
			if resolved.terminalState == "" {
				resolved.terminalState = terminal.State
			}
		}
	}
	resolved.source = renderSource(resolved.snapshot, resolved.surface)
	resolved.metrics = entry.Metrics
	if resolved.metrics == (renderTerminalMetrics{}) || resolved.snapshot != entry.Snapshot || resolved.surface != entry.Surface {
		resolved.metrics = terminalExtentProfileCached(resolved.snapshot, resolved.surface, entry.SurfaceVersion).Metrics
	}
	resolved.renderOffset = entry.ScrollOffset
	if entry.CopyModeActive {
		resolved.renderOffset = scrollOffsetForViewportTop(resolved.snapshot, resolved.contentRect.H, entry.CopyModeViewTopRow)
	}
	return resolved
}

func resolvedPaneContentNeedsConservativeRedraw(entry paneRenderEntry, resolved resolvedPaneContent) bool {
	if entry.ConservativeRedraw {
		return true
	}
	return terminalSourceWindowHasWidthSafety(resolved.source, resolved.contentRect.H, resolved.renderOffset)
}

func terminalSourceWindowHasWidthSafety(source terminalRenderSource, height, offset int) bool {
	if source == nil || height <= 0 {
		return false
	}
	for line := 0; line < height; line++ {
		rowIndex := terminalSourceWindowRowIndex(source, height, offset, line)
		if rowIndex < 0 {
			continue
		}
		if terminalSourceRowHasWidthSafety(source.Row(rowIndex)) {
			return true
		}
	}
	return false
}

func terminalSourceRowHasWidthSafety(row []protocol.Cell) bool {
	for _, cell := range row {
		decision := shared.WidthSafetyForTerminalCell(cell.Content, cell.Width)
		if decision.AmbiguousCompensation || decision.HostWidthStabilizer {
			return true
		}
	}
	return false
}

func terminalSourceWindowSignature(source terminalRenderSource, height, offset int) uint64 {
	finish := perftrace.Measure("render.window_signature")
	defer finish(height)
	if source == nil || height <= 0 {
		return 0
	}
	perftrace.Count("render.window_signature.rows", height)
	hash := fnvOffset64
	hash = fnvMixUint64(hash, uint64(source.Size().Cols))
	hash = fnvMixUint64(hash, uint64(source.Size().Rows))
	hash = fnvMixUint64(hash, uint64(source.ScreenRows()))
	hash = fnvMixUint64(hash, uint64(source.ScrollbackRows()))
	hash = fnvMixUint64(hash, uint64(source.TotalRows()))
	hash = fnvMixUint64(hash, uint64(offset))
	for line := 0; line < height; line++ {
		rowIndex := terminalSourceWindowRowIndex(source, height, offset, line)
		hash = fnvMixUint64(hash, terminalSourceRowHash(source, rowIndex))
	}
	return hash
}

func buildTerminalSourceWindowState(source terminalRenderSource, height, offset int) terminalSourceWindowState {
	if source == nil || height <= 0 {
		return terminalSourceWindowState{}
	}
	perftrace.Count("render.window_signature.rows", height)
	rowIndices := make([]int, height)
	for i := range rowIndices {
		rowIndices[i] = -1
	}
	for line := 0; line < height; line++ {
		rowIndices[line] = terminalSourceWindowRowIndex(source, height, offset, line)
	}

	rowHashes := make([]uint64, height)
	rowContentHashes := make([]uint64, height)
	rowIdentityHashes := make([]uint64, height)
	hash := fnvOffset64
	hash = fnvMixUint64(hash, uint64(source.Size().Cols))
	hash = fnvMixUint64(hash, uint64(source.Size().Rows))
	hash = fnvMixUint64(hash, uint64(source.ScreenRows()))
	hash = fnvMixUint64(hash, uint64(source.ScrollbackRows()))
	hash = fnvMixUint64(hash, uint64(source.TotalRows()))
	hash = fnvMixUint64(hash, uint64(offset))
	for i, rowIndex := range rowIndices {
		rowHash := terminalSourceRowHash(source, rowIndex)
		rowHashes[i] = rowHash
		rowContentHashes[i] = terminalSourceRowContentHash(source, rowIndex)
		rowIdentityHashes[i] = terminalSourceRowIdentityHash(source, rowIndex)
		hash = fnvMixUint64(hash, rowHash)
	}
	return terminalSourceWindowState{
		rowIndices:        rowIndices,
		exactRowHashes:    rowHashes,
		rowContentHashes:  rowContentHashes,
		rowIdentityHashes: rowIdentityHashes,
		contentHash:       hash,
		screenWindow:      offset <= 0,
	}
}

func terminalSourceWindowRowIndex(source terminalRenderSource, height, offset, line int) int {
	if source == nil || height <= 0 || line < 0 || line >= height {
		return -1
	}
	if offset <= 0 {
		base := source.ScrollbackRows()
		limit := minInt(height, source.ScreenRows())
		if line >= limit {
			return -1
		}
		return base + line
	}
	totalRows := source.TotalRows()
	end := totalRows - offset
	if end < 0 {
		end = 0
	}
	start := end - height
	if start < 0 {
		start = 0
	}
	rowIndex := start + line
	if rowIndex >= end {
		return -1
	}
	return rowIndex
}

func terminalSourceExtentHashWithMetrics(source terminalRenderSource, rect workbench.Rect, theme uiTheme, metrics renderTerminalMetrics) uint64 {
	if source == nil || rect.W <= 0 || rect.H <= 0 {
		return 0
	}
	hash := fnvOffset64
	hash = fnvMixUint64(hash, uint64(rect.W))
	hash = fnvMixUint64(hash, uint64(rect.H))
	hash = fnvMixUint64(hash, uint64(metrics.Cols))
	hash = fnvMixUint64(hash, uint64(metrics.Rows))
	hash = fnvMixString(hash, theme.panelBorder)
	return hash
}

func terminalSourceRowHash(source terminalRenderSource, rowIndex int) uint64 {
	perftrace.Count("render.row_hash", 1)
	if hashSource, ok := source.(terminalRowHashSource); ok {
		return hashSource.RowHash(rowIndex)
	}
	hash := fnvOffset64
	hash = fnvMixUint64(hash, uint64(rowIndex+1))
	if source == nil || rowIndex < 0 {
		return fnvMixUint64(hash, 0)
	}
	kind := source.RowKind(rowIndex)
	hash = fnvMixString(hash, kind)
	ts := source.RowTimestamp(rowIndex)
	hash = fnvMixInt64(hash, ts.UnixNano())
	if kind != "" || !ts.IsZero() {
		return hash
	}
	row := source.Row(rowIndex)
	hash = fnvMixUint64(hash, uint64(len(row)))
	for _, cell := range row {
		hash = fnvMixString(hash, cell.Content)
		hash = fnvMixInt64(hash, int64(cell.Width))
		hash = fnvMixString(hash, cell.Style.FG)
		hash = fnvMixString(hash, cell.Style.BG)
		hash = fnvMixBool(hash, cell.Style.Bold)
		hash = fnvMixBool(hash, cell.Style.Italic)
		hash = fnvMixBool(hash, cell.Style.Underline)
		hash = fnvMixBool(hash, cell.Style.Blink)
		hash = fnvMixBool(hash, cell.Style.Reverse)
		hash = fnvMixBool(hash, cell.Style.Strikethrough)
	}
	return hash
}

func terminalSourceRowIdentityHash(source terminalRenderSource, rowIndex int) uint64 {
	if hashSource, ok := source.(terminalRowIdentityHashSource); ok {
		return hashSource.RowIdentityHash(rowIndex)
	}
	hash := fnvOffset64
	if source == nil || rowIndex < 0 {
		return fnvMixUint64(hash, 0)
	}
	kind := source.RowKind(rowIndex)
	hash = fnvMixString(hash, kind)
	ts := source.RowTimestamp(rowIndex)
	hash = fnvMixInt64(hash, ts.UnixNano())
	if kind != "" || !ts.IsZero() {
		return hash
	}
	row := source.Row(rowIndex)
	hash = fnvMixUint64(hash, uint64(len(row)))
	for _, cell := range row {
		hash = fnvMixString(hash, cell.Content)
		hash = fnvMixInt64(hash, int64(cell.Width))
		hash = fnvMixString(hash, cell.Style.FG)
		hash = fnvMixString(hash, cell.Style.BG)
		hash = fnvMixBool(hash, cell.Style.Bold)
		hash = fnvMixBool(hash, cell.Style.Italic)
		hash = fnvMixBool(hash, cell.Style.Underline)
		hash = fnvMixBool(hash, cell.Style.Blink)
		hash = fnvMixBool(hash, cell.Style.Reverse)
		hash = fnvMixBool(hash, cell.Style.Strikethrough)
	}
	return hash
}

func terminalSourceRowContentHash(source terminalRenderSource, rowIndex int) uint64 {
	if hashSource, ok := source.(terminalRowContentHashSource); ok {
		return hashSource.RowContentHash(rowIndex)
	}
	hash := fnvOffset64
	if source == nil || rowIndex < 0 {
		return fnvMixUint64(hash, 0)
	}
	kind := source.RowKind(rowIndex)
	hash = fnvMixString(hash, kind)
	ts := source.RowTimestamp(rowIndex)
	hash = fnvMixInt64(hash, ts.UnixNano())
	row := source.Row(rowIndex)
	hash = fnvMixUint64(hash, uint64(len(row)))
	for _, cell := range row {
		hash = fnvMixString(hash, cell.Content)
		hash = fnvMixInt64(hash, int64(cell.Width))
		hash = fnvMixString(hash, cell.Style.FG)
		hash = fnvMixString(hash, cell.Style.BG)
		hash = fnvMixBool(hash, cell.Style.Bold)
		hash = fnvMixBool(hash, cell.Style.Italic)
		hash = fnvMixBool(hash, cell.Style.Underline)
		hash = fnvMixBool(hash, cell.Style.Blink)
		hash = fnvMixBool(hash, cell.Style.Reverse)
		hash = fnvMixBool(hash, cell.Style.Strikethrough)
	}
	return hash
}

const (
	fnvOffset64 = uint64(14695981039346656037)
	fnvPrime64  = uint64(1099511628211)
)

func fnvMixUint64(hash uint64, value uint64) uint64 {
	hash ^= value
	hash *= fnvPrime64
	return hash
}

func fnvMixInt64(hash uint64, value int64) uint64 {
	return fnvMixUint64(hash, uint64(value))
}

func fnvMixBool(hash uint64, value bool) uint64 {
	if value {
		return fnvMixUint64(hash, 1)
	}
	return fnvMixUint64(hash, 0)
}

func fnvMixString(hash uint64, value string) uint64 {
	hash = fnvMixUint64(hash, uint64(len(value)))
	for i := 0; i < len(value); i++ {
		hash ^= uint64(value[i])
		hash *= fnvPrime64
	}
	return hash
}

func drawPaneContentSpriteRowDiffWithMetrics(canvas, scratch *composedCanvas, rect workbench.Rect, source terminalRenderSource, rowIndex int, targetY int, theme uiTheme, metrics renderTerminalMetrics) {
	if canvas == nil || scratch == nil || rect.W <= 0 || targetY < rect.Y || targetY >= rect.Y+rect.H {
		return
	}
	scratch.withOwner(canvas.primaryOwner(), func() {
		scratch.resetToBlank()
		scratchRect := workbench.Rect{X: 0, Y: 0, W: rect.W, H: 1}
		if source != nil && rowIndex >= 0 {
			drawTerminalSourceRowInRectCleared(scratch, scratchRect, source, rowIndex, 0, theme)
		}
		drawTerminalExtentHintsRowWithMetrics(scratch, scratchRect, source, 0, theme, metrics)
	})

	dstRow := canvas.cells[targetY]
	srcRow := scratch.cells[0]
	start := -1
	flush := func(end int) {
		if start < 0 || end < start {
			return
		}
		copy(dstRow[rect.X+start:rect.X+end+1], srcRow[start:end+1])
		canvas.markRowDirtyRange(targetY, rect.X+start, rect.X+end)
		start = -1
	}
	for x := 0; x < rect.W; x++ {
		if dstRow[rect.X+x] == srcRow[x] {
			flush(x - 1)
			continue
		}
		if start < 0 {
			start = x
		}
	}
	flush(rect.W - 1)
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
