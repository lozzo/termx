package render

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/uiinput"
	"github.com/lozzow/termx/tuiv2/workbench"
)

const (
	terminalPoolListLeftX       = 2
	terminalPoolFooterActionGap = 2
)

type terminalPoolPageLayout struct {
	width         int
	height        int
	contentHeight int
	cardX         int
	cardY         int
	cardWidth     int
	cardHeight    int
	innerWidth    int
	bodyRect      workbench.Rect
	listRect      workbench.Rect
	previewRect   workbench.Rect
	paneRect      workbench.Rect
	queryRect     workbench.Rect
	itemRows      []terminalPoolItemRowLayout
	footerLine    string
	footerActions []terminalPoolFooterActionLayout
}

type terminalPoolItemRowLayout struct {
	itemIndex int
	rect      workbench.Rect
}

type terminalPoolFooterActionLayout struct {
	label  string
	action input.SemanticAction
	rect   workbench.Rect
}

type terminalPoolListRow struct {
	itemIndex int
	groupText string
}

type terminalPoolFooterActionSpec struct {
	label  string
	action input.SemanticAction
}

func buildTerminalPoolPageLayout(pool *modal.TerminalManagerState, width, height int) terminalPoolPageLayout {
	width = maxInt(1, width)
	height = maxInt(1, height)
	contentHeight := maxInt(1, height)
	cardWidth := minInt(maxInt(96, width-8), maxInt(12, width-2))
	cardHeight := minInt(maxInt(20, height-4), maxInt(14, height-1))
	innerWidth := maxInt(10, cardWidth-2)
	cardX := maxInt(0, (width-cardWidth)/2)
	cardY := maxInt(0, (contentHeight-cardHeight)/2)
	bodyY := cardY + 2
	bodyHeight := maxInt(6, cardHeight-4)
	leftWidth := maxInt(26, minInt((innerWidth*38)/100, innerWidth-34))
	rightWidth := maxInt(24, innerWidth-leftWidth-1)
	rows := maxInt(1, bodyHeight-1)
	layout := terminalPoolPageLayout{
		width:         width,
		height:        height,
		contentHeight: contentHeight,
		cardX:         cardX,
		cardY:         cardY,
		cardWidth:     cardWidth,
		cardHeight:    cardHeight,
		innerWidth:    innerWidth,
		bodyRect:      workbench.Rect{X: cardX + 1, Y: bodyY, W: innerWidth, H: bodyHeight},
		listRect:      workbench.Rect{X: cardX + 1, Y: bodyY + 1, W: leftWidth, H: rows},
		previewRect:   workbench.Rect{X: cardX + 1 + leftWidth + 1, Y: bodyY + 1, W: rightWidth, H: rows},
		queryRect: workbench.Rect{
			X: cardX + 1 + uiinput.PromptWidth(overlaySearchPrompt()),
			Y: cardY + 1,
			W: maxInt(1, innerWidth-uiinput.PromptWidth(overlaySearchPrompt())),
			H: 1,
		},
	}
	layout.paneRect = terminalPoolPaneRect(layout.previewRect, 1)

	if pool != nil {
		visibleRows := terminalPoolListRows(pool.VisibleItems())
		layout.itemRows = make([]terminalPoolItemRowLayout, 0, len(visibleRows))
		y := layout.listRect.Y
		for _, row := range visibleRows {
			if y >= layout.listRect.Y+layout.listRect.H {
				break
			}
			if row.itemIndex >= 0 {
				layout.itemRows = append(layout.itemRows, terminalPoolItemRowLayout{
					itemIndex: row.itemIndex,
					rect: workbench.Rect{
						X: layout.listRect.X,
						Y: y,
						W: layout.listRect.W,
						H: 1,
					},
				})
			}
			y++
		}
	}

	layout.footerLine, layout.footerActions = layoutTerminalPoolFooterActionsWithTheme(defaultUITheme(), layout.innerWidth, cardY+cardHeight-2)
	for index := range layout.footerActions {
		layout.footerActions[index].rect.X += cardX + 1 + terminalPoolListLeftX
	}
	return layout
}

func terminalPoolPaneRect(previewRect workbench.Rect, detailRows int) workbench.Rect {
	if detailRows < 0 {
		detailRows = 0
	}
	detailBlockRows := 0
	if detailRows > 0 {
		detailBlockRows = minInt(detailRows+1, maxInt(1, previewRect.H-4))
	}
	y := previewRect.Y + detailBlockRows
	height := maxInt(3, previewRect.H-detailBlockRows)
	return workbench.Rect{X: previewRect.X, Y: y, W: previewRect.W, H: height}
}

func terminalPoolWindow(total, selected, rows int) (int, int) {
	return workbenchTreeWindow(total, selected, rows)
}

func terminalPoolListRows(items []modal.PickerItem) []terminalPoolListRow {
	if len(items) == 0 {
		return nil
	}
	rows := make([]terminalPoolListRow, 0, len(items))
	lastGroup := ""
	for index := range items {
		group := strings.ToUpper(strings.TrimSpace(items[index].State))
		if group != "" && group != lastGroup {
			rows = append(rows, terminalPoolListRow{itemIndex: -1, groupText: group})
			lastGroup = group
		}
		rows = append(rows, terminalPoolListRow{itemIndex: index})
	}
	return rows
}

func terminalPoolFooterActionSpecs() []terminalPoolFooterActionSpec {
	return []terminalPoolFooterActionSpec{
		{label: "here", action: input.SemanticAction{Kind: input.ActionSubmitPrompt}},
		{label: "tab", action: input.SemanticAction{Kind: input.ActionAttachTab}},
		{label: "float", action: input.SemanticAction{Kind: input.ActionAttachFloating}},
		{label: "edit", action: input.SemanticAction{Kind: input.ActionEditTerminal}},
		{label: "kill", action: input.SemanticAction{Kind: input.ActionKillTerminal}},
	}
}

func layoutTerminalPoolFooterActions(width, height int) (string, []terminalPoolFooterActionLayout) {
	return layoutTerminalPoolFooterActionsWithTheme(defaultUITheme(), width, height)
}

func layoutTerminalPoolFooterActionsWithTheme(theme uiTheme, width, height int) (string, []terminalPoolFooterActionLayout) {
	if width <= 0 || height <= 0 {
		return "", nil
	}
	y := height - 1
	specs := terminalPoolFooterActionSpecs()
	slots := make([]terminalPoolFooterActionLayout, 0, len(specs))
	parts := make([]string, 0, len(specs))
	x := terminalPoolListLeftX
	for _, spec := range specs {
		label := renderOverlayFooterActionLabel(theme, spec.label)
		labelW := xansi.StringWidth(label)
		if labelW <= 0 || x+labelW > width {
			break
		}
		slots = append(slots, terminalPoolFooterActionLayout{
			label:  label,
			action: spec.action,
			rect: workbench.Rect{
				X: x,
				Y: y,
				W: labelW,
				H: 1,
			},
		})
		parts = append(parts, label)
		x += labelW + terminalPoolFooterActionGap
	}
	line := ""
	for index, part := range parts {
		if index > 0 {
			line += renderOverlaySpan(overlayFooterPlainStyle(theme), "", terminalPoolFooterActionGap)
		}
		line += part
	}
	return renderOverlaySpan(pickerFooterStyle(theme), line, width), slots
}
