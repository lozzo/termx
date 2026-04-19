package render

import (
	"strconv"
	"strings"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
)

func renderTerminalPoolPageWithCursor(pool *modal.TerminalManagerState, runtimeState *VisibleRuntimeStateProxy, termSize TermSize, cursorOffsetY int, cursorVisible bool) renderedBody {
	if pool == nil {
		return renderedBody{}
	}
	theme := uiThemeForRuntime(runtimeState)
	width := maxInt(1, termSize.Width)
	height := maxInt(1, termSize.Height)
	layout := buildTerminalPoolPageLayout(pool, width, height)
	innerWidth := layout.innerWidth
	headerLines := make([]string, 0, 3)

	title := terminalPickerTitleStyle(theme).Width(width).Render(forceWidthANSIOverlay(coalesce(strings.TrimSpace(pool.Title), "Terminal Pool"), width))
	headerLines = append(headerLines, title)
	headerLines = append(headerLines, forceWidthANSIOverlay(renderOverlaySearchLineWithCursor(theme, pool.Query, pool.Cursor, pool.CursorSet, width, cursorVisible), width))
	headerLines = append(headerLines, overlayCardFillStyle(theme).Width(width).Render(""))

	contentLines := make([]string, 0, height)
	lookup := newRuntimeLookup(runtimeState)

	items := pool.VisibleItems()
	for _, row := range terminalPoolListRows(items) {
		if row.itemIndex < 0 {
			contentLines = append(contentLines, renderOverlaySpan(overlayCardFillStyle(theme), "  "+overlaySectionTitleStyle(theme).Render(row.groupText), width))
			continue
		}
		line := items[row.itemIndex].RenderLine(innerWidth, row.itemIndex == pool.Selected, pickerLineStyle(theme), pickerSelectedLineStyle(theme), pickerCreateRowStyle(theme))
		contentLines = append(contentLines, renderOverlaySpan(overlayCardFillStyle(theme), "  "+line, width))
	}
	if detailLines := renderTerminalPoolDetailsWithLookup(pool.SelectedItem(), lookup, innerWidth); len(detailLines) > 0 {
		contentLines = append(contentLines, overlayCardFillStyle(theme).Width(width).Render(""))
		for _, line := range detailLines {
			contentLines = append(contentLines, renderOverlaySpan(overlayCardFillStyle(theme), "  "+line, width))
		}
	}

	footerLine, _ := layoutTerminalPoolFooterActionsWithTheme(theme, width, height)
	result := renderedBody{
		lines:  renderPageLinesWithPinnedFooter(headerLines, contentLines, footerLine, width, height),
		cursor: hideCursorANSI(),
		meta:   solidPresentMetadata(width, height, renderOwnerTerminalPool),
	}
	if cursorVisible {
		cursorX := layout.queryRect.X + valueCursorCellOffset(pool.Query, queryCursorIndex(pool.Query, pool.Cursor, pool.CursorSet), layout.queryRect.W)
		result.cursor = hostCursorANSI(cursorX, layout.queryRect.Y+cursorOffsetY, "bar", false)
	}
	return result
}

func renderTerminalPoolDetailsWithLookup(item *modal.PickerItem, lookup runtimeLookup, innerWidth int) []string {
	if item == nil {
		return nil
	}
	lines := []string{forceWidthANSIOverlay("PREVIEW", innerWidth)}
	if terminal := lookup.terminal(item.TerminalID); terminal != nil {
		lines = append(lines, terminalPoolPreviewLines(terminal.Snapshot, terminal.Surface, innerWidth, 4)...)
		if strings.TrimSpace(terminal.OwnerPaneID) != "" {
			lines = append(lines, forceWidthANSIOverlay("owner pane: "+terminal.OwnerPaneID, innerWidth))
		}
		lines = append(lines, forceWidthANSIOverlay("bound panes: "+strconv.Itoa(len(terminal.BoundPaneIDs)), innerWidth))
	} else {
		lines = append(lines, forceWidthANSIOverlay("(no live preview)", innerWidth))
	}
	if strings.TrimSpace(item.Command) != "" {
		if len(lines) > 0 && !strings.Contains(lines[len(lines)-1], "DETAIL") {
			lines = append(lines, forceWidthANSIOverlay("DETAIL", innerWidth))
		}
		lines = append(lines, forceWidthANSIOverlay("command: "+item.Command, innerWidth))
	}
	if strings.TrimSpace(item.Location) != "" {
		lines = append(lines, forceWidthANSIOverlay("location: "+item.Location, innerWidth))
	}
	if strings.TrimSpace(item.Description) != "" {
		lines = append(lines, forceWidthANSIOverlay("status: "+item.Description, innerWidth))
	}
	return lines
}

func terminalPoolPreviewLines(snapshot *protocol.Snapshot, surface runtime.TerminalSurface, innerWidth int, maxLines int) []string {
	if maxLines <= 0 {
		maxLines = 4
	}
	lines := terminalPreviewLinesANSI(snapshot, surface, nil, innerWidth, maxLines)
	if len(lines) == 0 {
		lines = []string{forceWidthANSIOverlay("(no live preview)", innerWidth)}
	}
	return lines
}
