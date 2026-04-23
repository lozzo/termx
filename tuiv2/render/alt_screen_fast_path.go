package render

import (
	"strings"

	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
	localvterm "github.com/lozzow/termx/vterm"
)

// altScreenRowCache caches serialized ANSI strings for alt-screen rows keyed
// by row content hash. The cache is identity-based (hash → string) so it
// remains valid across scroll operations: a row that scrolled up by one
// position still has the same content hash and can be reused without
// re-serializing.
type altScreenRowCache struct {
	terminalID string
	width      int
	emojiMode  shared.AmbiguousEmojiVariationSelectorMode
	frameKey   paneFrameKey
	framed     bool
	// byHash maps row content hash → serialized ANSI (without cursor). The map
	// is rebuilt from scratch each frame so stale entries cannot accumulate.
	byHash map[uint64]string
	// lineByHash caches fully wrapped row lines for non-cursor rows when the
	// frame chrome is unchanged.
	lineByHash   map[uint64]string
	topBorder    string
	bottomBorder string
}

func (c *altScreenRowCache) valid(terminalID string, width int, emojiMode shared.AmbiguousEmojiVariationSelectorMode) bool {
	return c != nil && c.terminalID == terminalID && c.width == width && c.emojiMode == emojiMode
}

func renderAltScreenFastPathVM(coordinator *Coordinator, vm RenderVM, entries []paneRenderEntry, cursorOffsetY int) (renderedBody, bool) {
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
	return renderAltScreenEntryFastPath(coordinator, entry, resolved, vm.Runtime, cursorOffsetY), true
}

func renderAltScreenEntryFastPath(coordinator *Coordinator, entry paneRenderEntry, resolved resolvedPaneContent, runtimeState *VisibleRuntimeStateProxy, cursorOffsetY int) renderedBody {
	lines := make([]string, 0, entry.Rect.H)

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
	contentW := resolved.contentRect.W

	// Look up the row cache from the coordinator. Invalidate if terminal, width,
	// or emoji mode changed (e.g. after resize or terminal switch).
	var cache *altScreenRowCache
	if coordinator != nil {
		if coordinator.altScreenCache.valid(entry.TerminalID, contentW, emojiMode) {
			cache = coordinator.altScreenCache
		} else {
			coordinator.altScreenCache = &altScreenRowCache{
				terminalID: entry.TerminalID,
				width:      contentW,
				emojiMode:  emojiMode,
				byHash:     make(map[uint64]string, resolved.contentRect.H),
				lineByHash: make(map[uint64]string, resolved.contentRect.H),
			}
			cache = coordinator.altScreenCache
		}
	}
	if cache != nil {
		framed := !entry.Frameless
		if cache.frameKey != entry.FrameKey || cache.framed != framed {
			cache.frameKey = entry.FrameKey
			cache.framed = framed
			cache.lineByHash = make(map[uint64]string, resolved.contentRect.H)
			cache.topBorder = ""
			cache.bottomBorder = ""
		}
	}

	if !entry.Frameless {
		if cache != nil && cache.topBorder != "" {
			lines = append(lines, cache.topBorder)
		} else {
			top := renderAltScreenTopBorderLine(entry)
			lines = append(lines, top)
			if cache != nil {
				cache.topBorder = top
			}
		}
	}

	// Build the new cache for this frame. The identity-based lookup allows rows
	// that shifted position due to a scroll to still be found by content.
	var newByHash map[uint64]string
	var newLineByHash map[uint64]string
	if cache != nil {
		newByHash = make(map[uint64]string, resolved.contentRect.H)
		if !entry.Frameless {
			newLineByHash = make(map[uint64]string, resolved.contentRect.H)
		}
	}

	base := resolved.source.ScrollbackRows()
	screenRows := resolved.source.ScreenRows()
	for row := 0; row < resolved.contentRect.H; row++ {
		rowIndex := base + row
		isCursorRow := row == cursorRow

		var content string
		if !isCursorRow && cache != nil && row < screenRows {
			// For non-cursor rows, try to reuse a cached ANSI string. Use the
			// row content hash instead of the positional row hash so a row that
			// scrolled to a new viewport position still hits the cache.
			rowHash := terminalSourceRowVisualHash(resolved.source, rowIndex)
			if !entry.Frameless {
				if cached, ok := cache.lineByHash[rowHash]; ok {
					lines = append(lines, cached)
					newLineByHash[rowHash] = cached
					if content, ok := cache.byHash[rowHash]; ok {
						newByHash[rowHash] = content
					}
					perftrace.Count("render.body.alt_screen_row_cache.hit", 1)
					continue
				}
			}
			if cached, ok := cache.byHash[rowHash]; ok {
				content = cached
				perftrace.Count("render.body.alt_screen_row_cache.hit", 1)
			} else {
				content = terminalSourceViewportRowANSI(resolved.source, rowIndex, contentW, emojiMode, -1, false, "")
				perftrace.Count("render.body.alt_screen_row_cache.miss", 1)
			}
			newByHash[rowHash] = content
		} else {
			// Cursor row, out-of-bounds row, or no cache: always re-serialize.
			content = terminalSourceViewportRowANSI(resolved.source, rowIndex, contentW, emojiMode, cursorCol, isCursorRow, cursorShape)
		}

		if entry.Frameless {
			lines = append(lines, wrapRenderedRowANSI(content))
		} else {
			line := renderAltScreenBorderedContentLine(entry, content)
			lines = append(lines, line)
			if !isCursorRow && newLineByHash != nil && row < screenRows {
				rowHash := terminalSourceRowVisualHash(resolved.source, rowIndex)
				newLineByHash[rowHash] = line
			}
		}
	}

	// Replace the coordinator's cache map with the current frame's entries.
	// This purges stale entries from rows that are no longer visible.
	if cache != nil {
		cache.byHash = newByHash
		if newLineByHash != nil {
			cache.lineByHash = newLineByHash
		} else {
			cache.lineByHash = nil
		}
	}

	if !entry.Frameless {
		if cache != nil && cache.bottomBorder != "" {
			lines = append(lines, cache.bottomBorder)
		} else {
			bottom := renderAltScreenBottomBorderLine(entry)
			lines = append(lines, bottom)
			if cache != nil {
				cache.bottomBorder = bottom
			}
		}
	}

	return renderedBody{
		lines:  lines,
		cursor: cursor,
		meta:   solidPresentMetadata(entry.Rect.W, len(lines), entry.OwnerID),
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
	layout, ok := paneTopBorderLabelsLayout(localRect, resolvePaneChromeConfig(entry.Chrome, entry.Title, entry.Border, paneChromeActionTokensForFrame(localRect, entry.Title, entry.Border, entry.Floating)))
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
	line.Grow(len(content) + 48)
	line.WriteString(styleANSI(borderStyle))
	line.WriteString("│")
	line.WriteString("\x1b[0m")
	line.WriteString(content)
	// Re-anchor the right border to the final pane column so width-unsafe
	// content stays aligned with the regular compositor path.
	writeCHAANSI(&line, entry.Rect.W)
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

func terminalSourceViewportRowANSI(source terminalRenderSource, rowIndex, width int, emojiMode shared.AmbiguousEmojiVariationSelectorMode, cursorCol int, cursorVisible bool, cursorShape string) string {
	if source == nil || rowIndex < 0 {
		return protocolViewportRowANSI(nil, width, emojiMode, cursorCol, cursorVisible, cursorShape)
	}
	if rowSource, ok := source.(terminalCellRowSource); ok {
		if row := rowSource.RowView(rowIndex); row != nil {
			return vtermViewportRowANSI(row, width, emojiMode, cursorCol, cursorVisible, cursorShape)
		}
	}
	return protocolViewportRowANSI(source.Row(rowIndex), width, emojiMode, cursorCol, cursorVisible, cursorShape)
}

func vtermViewportRowANSI(row []localvterm.Cell, width int, emojiMode shared.AmbiguousEmojiVariationSelectorMode, cursorCol int, cursorVisible bool, cursorShape string) string {
	return vtermRowANSIWithOptions(row, width, protocolRowANSIOptions{
		emojiMode:     emojiMode,
		cursorCol:     cursorCol,
		cursorVisible: cursorVisible,
		cursorShape:   cursorShape,
	})
}

func terminalSourceRowVisualHash(source terminalRenderSource, rowIndex int) uint64 {
	if source == nil || rowIndex < 0 {
		return fnvMixUint64(fnvOffset64, 0)
	}
	if hashSource, ok := source.(terminalRowVisualHashSource); ok {
		return hashSource.RowVisualHash(rowIndex)
	}
	if rowSource, ok := source.(terminalCellRowSource); ok {
		if row := rowSource.RowView(rowIndex); row != nil {
			return hashVTermRow(fnvOffset64, row)
		}
	}
	return hashProtocolRow(fnvOffset64, source.Row(rowIndex))
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
