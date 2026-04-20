package render

import "strings"

import "github.com/lozzow/termx/tuiv2/modal"

func renderPickerOverlay(picker *modal.PickerState, termSize TermSize) string {
	return strings.Join(renderPickerOverlayLinesWithThemeAndCursor(picker, termSize, defaultUITheme(), true), "\n")
}

func renderPickerOverlayLinesWithThemeAndCursor(picker *modal.PickerState, termSize TermSize, theme uiTheme, cursorVisible bool) []string {
	_ = cursorVisible
	if picker == nil {
		return nil
	}
	width, height := overlayViewport(termSize)
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
