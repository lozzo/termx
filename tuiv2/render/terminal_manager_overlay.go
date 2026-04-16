package render

import (
	"strings"

	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func renderTerminalManagerOverlay(manager *modal.TerminalManagerState, termSize TermSize) string {
	return strings.Join(renderTerminalManagerOverlayLinesWithThemeAndCursor(manager, termSize, defaultUITheme(), true), "\n")
}

func renderTerminalManagerOverlayWithTheme(manager *modal.TerminalManagerState, termSize TermSize, theme uiTheme) string {
	return strings.Join(renderTerminalManagerOverlayLinesWithThemeAndCursor(manager, termSize, theme, true), "\n")
}

func renderTerminalManagerOverlayWithThemeAndCursor(manager *modal.TerminalManagerState, termSize TermSize, theme uiTheme, cursorVisible bool) string {
	return strings.Join(renderTerminalManagerOverlayLinesWithThemeAndCursor(manager, termSize, theme, cursorVisible), "\n")
}

func renderTerminalManagerOverlayLinesWithThemeAndCursor(manager *modal.TerminalManagerState, termSize TermSize, theme uiTheme, cursorVisible bool) []string {
	if manager == nil {
		return nil
	}
	width, height := overlayViewport(termSize)
	innerWidth := pickerInnerWidth(width)
	items := manager.VisibleItems()
	itemLines := make([]string, 0, len(items))
	for index := range items {
		item := items[index]
		itemLines = append(itemLines, item.RenderLine(innerWidth, index == manager.Selected, pickerLineStyle(theme), pickerSelectedLineStyle(theme), pickerCreateRowStyle(theme)))
	}
	if detailLines := renderTerminalManagerDetails(manager.SelectedItem(), innerWidth); len(detailLines) > 0 {
		itemLines = append(itemLines, "")
		itemLines = append(itemLines, detailLines...)
	}
	footerLine, _ := layoutOverlayFooterActionsWithTheme(theme, terminalManagerFooterActionSpecs(), workbench.Rect{W: innerWidth, H: 1})
	return renderPickerCardLinesWithTheme(
		theme,
		coalesce(manager.Title, "Terminal Manager"),
		renderOverlaySearchLineWithCursor(theme, manager.Query, manager.Cursor, manager.CursorSet, innerWidth, cursorVisible),
		itemLines,
		footerLine,
		width,
		height,
	)
}

func renderTerminalManagerDetails(item *modal.PickerItem, innerWidth int) []string {
	if item == nil {
		return nil
	}
	lines := []string{
		forceWidthANSIOverlay("selected: "+coalesce(item.Name, item.TerminalID), innerWidth),
	}
	if strings.TrimSpace(item.Command) != "" {
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
