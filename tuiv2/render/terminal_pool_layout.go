package render

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/workbench"
)

const (
	terminalPoolListLeftX       = 2
	terminalPoolListStartY      = 3
	terminalPoolFooterActionGap = 2
)

type terminalPoolPageLayout struct {
	innerWidth    int
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
	layout := terminalPoolPageLayout{
		innerWidth: maxInt(24, width-4),
		queryRect: workbench.Rect{
			X: 2 + xansi.StringWidth("search: "),
			Y: 1,
			W: maxInt(1, width-2-xansi.StringWidth("search: ")),
			H: 1,
		},
	}

	if pool != nil {
		rows := terminalPoolListRows(pool.VisibleItems())
		maxRows := maxInt(0, height-1) // last row is reserved for footer actions.
		layout.itemRows = make([]terminalPoolItemRowLayout, 0, len(rows))
		y := terminalPoolListStartY
		for _, row := range rows {
			if y >= maxRows {
				break
			}
			if row.itemIndex >= 0 {
				layout.itemRows = append(layout.itemRows, terminalPoolItemRowLayout{
					itemIndex: row.itemIndex,
					rect: workbench.Rect{
						X: terminalPoolListLeftX,
						Y: y,
						W: layout.innerWidth,
						H: 1,
					},
				})
			}
			y++
		}
	}

	layout.footerLine, layout.footerActions = layoutTerminalPoolFooterActions(width, height)
	return layout
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
	if len(parts) > 0 {
		line = renderOverlaySpan(overlayFooterPlainStyle(theme), "", terminalPoolListLeftX)
		for index, part := range parts {
			if index > 0 {
				line += renderOverlaySpan(overlayFooterPlainStyle(theme), "", terminalPoolFooterActionGap)
			}
			line += part
		}
	}
	return renderOverlaySpan(pickerFooterStyle(theme), line, width), slots
}
