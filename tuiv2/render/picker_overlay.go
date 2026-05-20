package render

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/modal"
)

func renderPickerOverlay(picker *modal.PickerState, termSize TermSize) string {
	return strings.Join(renderPickerOverlayLinesWithThemeAndCursor(picker, termSize, defaultUITheme(), true), "\n")
}

func renderPickerOverlayLinesWithThemeAndCursor(picker *modal.PickerState, termSize TermSize, theme uiTheme, cursorVisible bool) []string {
	_ = cursorVisible
	if picker == nil {
		return nil
	}
	width, height := overlayViewport(termSize)
	if picker.Layout == modal.PickerLayoutClipboardHistory {
		return renderClipboardHistoryPickerOverlayLines(picker, width, height, theme)
	}
	innerWidth := pickerInnerWidth(width)
	items := picker.VisibleItems()
	itemLines := make([]string, 0, len(items))
	for index := range items {
		item := items[index]
		itemLines = append(itemLines, item.RenderLineWithPrefix(innerWidth, index == picker.Selected, "  ", "▸ ", pickerLineStyle(theme), pickerSelectedLineStyle(theme), pickerCreateRowStyle(theme)))
	}
	header := renderOverlaySearchInput(theme, picker.QueryState(), innerWidth)
	return renderPickerCardLinesWithTheme(theme, coalesce(picker.Title, "Terminal Picker"), header, itemLines, "", width, height)
}

func renderClipboardHistoryPickerOverlayLines(picker *modal.PickerState, width, height int, theme uiTheme) []string {
	items := picker.VisibleItems()
	layout := buildClipboardHistoryPickerLayout(width, height, len(items))
	lines := make([]string, 0, layout.cardHeight-2)
	lines = append(lines, renderCardContentRow(theme, "", layout.innerWidth))
	lines = append(lines, renderCardHeaderRow(theme, renderOverlaySearchInput(theme, picker.QueryState(), layout.innerWidth), layout.innerWidth))
	lines = append(lines, renderClipboardHistoryPickerColumnHeader(theme, layout.innerWidth, layout.leftRect.W, layout.rightRect.W))

	start, end := pickerVisibleWindow(len(items), picker.Selected, layout.listHeight)
	selected := picker.SelectedItem()
	for row := 0; row < layout.listHeight; row++ {
		itemIndex := start + row
		left := renderOverlaySpan(overlayCardFillStyle(theme), "", layout.leftRect.W)
		if itemIndex >= end {
			right := renderClipboardHistoryPreviewLine(theme, selected, row, layout.rightRect.W)
			lines = append(lines, renderCardContentRow(theme, left+"  "+right, layout.innerWidth))
			continue
		}
		left = renderClipboardHistoryListLine(theme, items[itemIndex], itemIndex == picker.Selected, layout.leftRect.W)
		right := renderClipboardHistoryPreviewLine(theme, selected, row, layout.rightRect.W)
		lines = append(lines, renderCardContentRow(theme, left+"  "+right, layout.innerWidth))
	}
	lines = append(lines, renderCardContentRow(theme, "", layout.innerWidth))

	cardLines := make([]string, 0, len(lines)+2)
	cardLines = append(cardLines, renderModalTopBorder(theme, coalesce(picker.Title, "Clipboard History"), layout.innerWidth))
	for _, line := range lines {
		cardLines = append(cardLines, renderModalFramedRow(theme, line, layout.innerWidth))
	}
	cardLines = append(cardLines, renderModalBottomBorder(theme, layout.innerWidth))
	return placeOverlayCardLines(theme, layout.width, layout.contentHeight, layout.cardX, layout.cardY, layout.cardWidth, cardLines)
}

func renderClipboardHistoryPickerColumnHeader(theme uiTheme, innerWidth, leftWidth, rightWidth int) string {
	left := renderOverlaySpan(clipboardHistoryPickerHeaderStyle(theme), " history", leftWidth)
	right := renderOverlaySpan(clipboardHistoryPickerHeaderStyle(theme), " preview", rightWidth)
	return forceWidthANSIOverlay(left+"  "+right, innerWidth)
}

func renderClipboardHistoryListLine(theme uiTheme, item modal.PickerItem, selected bool, width int) string {
	if width <= 0 {
		return ""
	}
	if item.CreateNew {
		style := pickerCreateRowStyle(theme)
		if selected {
			style = pickerSelectedLineStyle(theme)
		}
		return renderOverlaySpan(style, " + "+coalesce(item.Name, "New clipboard entry"), width)
	}
	prefix := "   "
	style := pickerLineStyle(theme)
	if selected {
		prefix = "▸ "
		style = pickerSelectedLineStyle(theme)
	}
	meta := clipboardHistoryMetaLine(item)
	if meta != "" {
		meta = "  " + meta
	}
	textWidth := maxInt(8, width-lipgloss.Width(prefix)-lipgloss.Width(meta)-1)
	text := xansi.Truncate(strings.TrimSpace(item.Name), textWidth, "…")
	if text == "" {
		text = "(empty)"
	}
	line := prefix + text + meta
	return renderOverlaySpan(style, line, width)
}

func renderClipboardHistoryPreviewLine(theme uiTheme, item *modal.PickerItem, row, width int) string {
	if width <= 0 {
		return ""
	}
	style := overlayCardFillStyle(theme)
	if item == nil {
		return renderOverlaySpan(style, "", width)
	}
	if item.CreateNew {
		lines := []string{
			"Create a shared clipboard entry.",
			"It will be stored in daemon public storage.",
		}
		if row >= len(lines) {
			return renderOverlaySpan(style, "", width)
		}
		return renderOverlaySpan(style, lines[row], width)
	}
	lines := clipboardHistoryPreviewLines(*item, width)
	if row >= len(lines) {
		return renderOverlaySpan(style, "", width)
	}
	return renderOverlaySpan(style, lines[row], width)
}

func clipboardHistoryPreviewLines(item modal.PickerItem, width int) []string {
	out := []string(nil)
	if created := clipboardHistoryCreatedLabel(item.CreatedAt); created != "" {
		out = append(out, "time: "+created)
	}
	if source := strings.TrimSpace(item.SourceApp); source != "" {
		out = append(out, "source: "+source)
	}
	if pane := strings.TrimSpace(item.Location); pane != "" {
		out = append(out, "from: "+pane)
	}
	if len(out) > 0 {
		out = append(out, "")
	}
	text := item.Description
	if strings.TrimSpace(text) == "" {
		text = item.Name
	}
	wrapped := wrapPlainTextForOverlay(text, width)
	if len(wrapped) == 0 {
		wrapped = []string{"(empty)"}
	}
	return append(out, wrapped...)
}

func clipboardHistoryMetaLine(item modal.PickerItem) string {
	parts := []string(nil)
	if label := clipboardHistoryShortTimeLabel(item.CreatedAt); label != "" {
		parts = append(parts, label)
	}
	if source := strings.TrimSpace(item.SourceApp); source != "" {
		parts = append(parts, source)
	}
	if pane := strings.TrimSpace(item.Location); pane != "" {
		parts = append(parts, pane)
	}
	if len(parts) == 0 && strings.TrimSpace(item.State) != "" {
		parts = append(parts, strings.TrimSpace(item.State))
	}
	return strings.Join(parts, " · ")
}

func clipboardHistoryShortTimeLabel(createdAt time.Time) string {
	if createdAt.IsZero() {
		return ""
	}
	now := time.Now()
	local := createdAt.Local()
	if now.Year() == local.Year() && now.YearDay() == local.YearDay() {
		return local.Format("15:04")
	}
	if now.Year() == local.Year() {
		return local.Format("01-02")
	}
	return local.Format("2006-01-02")
}

func clipboardHistoryCreatedLabel(createdAt time.Time) string {
	if createdAt.IsZero() {
		return ""
	}
	return createdAt.Local().Format("2006-01-02 15:04:05")
}

func wrapPlainTextForOverlay(text string, width int) []string {
	if width <= 0 {
		return nil
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	out := []string(nil)
	for _, part := range strings.Split(text, "\n") {
		if part == "" {
			out = append(out, "")
			continue
		}
		for lipgloss.Width(part) > width {
			cut := overlayWrapCut(part, width)
			if cut <= 0 || cut >= len(part) {
				break
			}
			out = append(out, strings.TrimRight(part[:cut], " "))
			part = strings.TrimLeft(part[cut:], " ")
		}
		out = append(out, part)
	}
	return out
}

func overlayWrapCut(text string, width int) int {
	if width <= 0 || text == "" {
		return 0
	}
	best := 0
	lastSpace := -1
	used := 0
	for index, r := range text {
		rw := xansi.StringWidth(string(r))
		if rw <= 0 {
			rw = 1
		}
		if used+rw > width {
			if lastSpace > 0 {
				return lastSpace
			}
			return best
		}
		used += rw
		best = index + len(string(r))
		if r == ' ' || r == '\t' {
			lastSpace = best
		}
	}
	return len(text)
}

func pickerVisibleWindow(total, selected, visibleRows int) (int, int) {
	if total <= 0 || visibleRows <= 0 {
		return 0, 0
	}
	selected = clampInt(selected, 0, total-1)
	if total <= visibleRows {
		return 0, total
	}
	start := selected - visibleRows/2
	if start < 0 {
		start = 0
	}
	if start+visibleRows > total {
		start = total - visibleRows
	}
	return start, start + visibleRows
}

func clipboardHistoryPickerHeaderStyle(theme uiTheme) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.panelMuted)).
		Background(lipgloss.Color(overlayCardBG(theme))).
		Bold(true)
}
