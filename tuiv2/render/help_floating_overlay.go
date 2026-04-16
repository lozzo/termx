package render

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func renderHelpOverlay(help *modal.HelpState, termSize TermSize) string {
	return strings.Join(renderHelpOverlayLinesWithTheme(help, termSize, defaultUITheme()), "\n")
}

func renderHelpOverlayWithTheme(help *modal.HelpState, termSize TermSize, theme uiTheme) string {
	return strings.Join(renderHelpOverlayLinesWithTheme(help, termSize, theme), "\n")
}

func renderHelpOverlayLinesWithTheme(help *modal.HelpState, termSize TermSize, theme uiTheme) []string {
	if help == nil {
		return nil
	}
	width, height := overlayViewport(termSize)
	innerWidth := pickerInnerWidth(width)
	lines := helpOverlayLines(theme, help, innerWidth)
	return renderPickerCardLinesWithTheme(theme, "Help", "", lines, "", width, height)
}

func renderFloatingOverviewOverlayWithTheme(overview *modal.FloatingOverviewState, termSize TermSize, theme uiTheme) string {
	return strings.Join(renderFloatingOverviewOverlayLinesWithTheme(overview, termSize, theme), "\n")
}

func renderFloatingOverviewOverlayLinesWithTheme(overview *modal.FloatingOverviewState, termSize TermSize, theme uiTheme) []string {
	if overview == nil {
		return nil
	}
	width, height := overlayViewport(termSize)
	innerWidth := pickerInnerWidth(width)
	itemLines := make([]string, 0, len(overview.Items))
	for index := range overview.Items {
		itemLines = append(itemLines, renderFloatingOverviewItemLine(overview.Items[index], index == overview.Selected, innerWidth, theme))
	}
	footerLine, _ := layoutOverlayFooterActionsWithTheme(theme, floatingOverviewFooterActionSpecs(), workbench.Rect{W: innerWidth, H: 1})
	return renderPickerCardLinesWithTheme(
		theme,
		"Floating Windows",
		"Restore, collapse, close, or summon floating panes",
		itemLines,
		footerLine,
		width,
		height,
	)
}

func renderFloatingOverviewItemLine(item modal.FloatingOverviewItem, selected bool, width int, theme uiTheme) string {
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = item.PaneID
	}
	display := string(item.Display)
	if display == "" {
		display = string(workbench.FloatingDisplayExpanded)
	}
	fit := "manual"
	if item.FitMode == workbench.FloatingFitAuto {
		fit = "auto"
	}
	slot := " "
	if item.ShortcutSlot > 0 {
		slot = fmt.Sprintf("%d", item.ShortcutSlot)
	}
	body := fmt.Sprintf("[%s] %s  %s  %s  %dx%d", slot, title, display, fit, item.Rect.W, item.Rect.H)
	style := pickerLineStyle(theme)
	if selected {
		style = pickerSelectedLineStyle(theme)
	} else if item.Display != workbench.FloatingDisplayExpanded {
		style = pickerLineStyle(theme).Foreground(lipgloss.Color(theme.panelMuted))
	}
	return style.Render(forceWidthANSIOverlay(body, width))
}

func helpOverlayLines(theme uiTheme, help *modal.HelpState, innerWidth int) []string {
	if help == nil {
		return nil
	}
	lines := make([]string, 0)
	for _, section := range help.Sections {
		lines = append(lines, renderOverlaySpan(overlaySectionTitleStyle(theme), "▍ "+section.Title, innerWidth))
		for _, binding := range section.Bindings {
			line := overlayHelpKeyStyle(theme).Render(binding.Key) +
				renderOverlaySpan(overlayHelpActionStyle(theme), "", 2) +
				overlayHelpActionStyle(theme).Render(binding.Action)
			lines = append(lines, renderOverlaySpan(overlayHelpActionStyle(theme), line, innerWidth))
		}
		lines = append(lines, "")
	}
	return lines
}
