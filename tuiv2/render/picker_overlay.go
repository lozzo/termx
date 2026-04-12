package render

import "github.com/lozzow/termx/tuiv2/modal"

func renderPickerOverlay(picker *modal.PickerState, termSize TermSize) string {
	return renderPickerOverlayWithThemeAndCursor(picker, termSize, defaultUITheme(), true)
}

func renderPickerOverlayWithTheme(picker *modal.PickerState, termSize TermSize, theme uiTheme) string {
	return renderPickerOverlayWithThemeAndCursor(picker, termSize, theme, true)
}

func renderPickerOverlayWithThemeAndCursor(picker *modal.PickerState, termSize TermSize, theme uiTheme, cursorVisible bool) string {
	if picker == nil {
		return ""
	}
	width, height := overlayViewport(termSize)
	innerWidth := pickerInnerWidth(width)
	items := picker.VisibleItems()
	itemLines := make([]string, 0, len(items))
	for index := range items {
		item := items[index]
		itemLines = append(itemLines, item.RenderLineWithPrefix(innerWidth, index == picker.Selected, "  ", "▸ ", pickerLineStyle(theme), pickerSelectedLineStyle(theme), pickerCreateRowStyle(theme)))
	}
	header := renderOverlaySearchLineWithCursor(theme, picker.Query, picker.Cursor, picker.CursorSet, innerWidth, cursorVisible)
	return renderPickerCardWithTheme(theme, coalesce(picker.Title, "Terminal Picker"), header, itemLines, "", width, height)
}
