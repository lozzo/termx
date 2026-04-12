package render

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func resolvePaneTitle(pane workbench.VisiblePane, runtimeState *VisibleRuntimeStateProxy) string {
	return resolvePaneTitleWithLookup(pane, newRuntimeLookup(runtimeState))
}

func resolvePaneTitleWithLookup(pane workbench.VisiblePane, lookup runtimeLookup) string {
	if strings.TrimSpace(pane.TerminalID) == "" {
		return "unconnected"
	}
	if terminal := lookup.terminal(pane.TerminalID); terminal != nil {
		if terminal.Name != "" {
			return terminal.Name
		}
	}
	return pane.Title
}

func displayPaneTitleWithLookup(pane workbench.VisiblePane, lookup runtimeLookup) string {
	title := resolvePaneTitleWithLookup(pane, lookup)
	buttonLabel := paneSizeLockButtonLabel(pane, lookup)
	if buttonLabel == "" {
		return title
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "terminal"
	}
	return buttonLabel + " " + title
}

// drawPaneFrame draws the border box with a title on the left and stable chrome slots on the right.
func drawPaneFrame(canvas *composedCanvas, rect workbench.Rect, sharedLeft, sharedTop bool, title string, border paneBorderInfo, theme uiTheme, overflow paneOverflowHints, active bool, floating bool) {
	_ = sharedLeft
	_ = sharedTop
	if rect.W < 2 || rect.H < 2 {
		return
	}
	borderFG := theme.panelBorder2
	titleFG := theme.panelMuted
	metaFG := theme.panelMuted
	actionFG := theme.panelMuted
	stateFG := theme.panelMuted
	if active {
		borderFG = theme.chromeAccent
		titleFG = theme.panelText
		metaFG = theme.panelMuted
		actionFG = theme.panelText
		switch border.StateTone {
		case "success":
			stateFG = theme.success
		case "warning":
			stateFG = theme.warning
		case "danger":
			stateFG = theme.danger
		default:
			stateFG = metaFG
		}
	}
	borderStyle := drawStyle{FG: borderFG}
	chromeStyles := paneChromeDrawStyles{
		Title:         drawStyle{FG: titleFG, Bold: true},
		Meta:          drawStyle{FG: metaFG},
		State:         drawStyle{FG: stateFG},
		Action:        drawStyle{FG: actionFG, Bold: active},
		EmphasizeRole: active,
	}
	if floating {
		// Floating panes are true overlays. If we merge their border connections
		// with whatever tiled border glyph is already underneath, the corner cell
		// turns into ├/┼ and the single resulting glyph inherits only one style.
		// That visually "activates" the underlying pane border at the junction.
		// Overwrite the floating frame directly so its corners stay real corners.
		drawDirectPaneBorder(canvas, rect, borderStyle)
		drawPaneOverflowMarkers(canvas, rect, theme, overflow, active)
		drawPaneTopBorderLabels(canvas, rect, chromeStyles, title, border, floating)
		return
	}
	// Framed split panes intentionally keep their own left/top borders instead
	// of merging into a single shared divider. Collapsing neighboring pane
	// frames saves a column, but it also changes the visual contract of split
	// layouts and makes the center separator disappear into one line.
	drawHorizontalBorder(canvas, rect.X, rect.X+rect.W-1, rect.Y, borderStyle, false, true, false)
	drawHorizontalBorder(canvas, rect.X, rect.X+rect.W-1, rect.Y+rect.H-1, borderStyle, false, false, true)
	drawVerticalBorder(canvas, rect.X, verticalBorderStart(rect.Y, false), rect.Y+rect.H-2, borderStyle, false)
	drawVerticalBorder(canvas, rect.X+rect.W-1, verticalBorderStart(rect.Y, false), rect.Y+rect.H-2, borderStyle, false)

	drawPaneOverflowMarkers(canvas, rect, theme, overflow, active)
	drawPaneTopBorderLabels(canvas, rect, chromeStyles, title, border, floating)
}

func drawDirectPaneBorder(canvas *composedCanvas, rect workbench.Rect, style drawStyle) {
	if canvas == nil || rect.W < 2 || rect.H < 2 {
		return
	}
	for x := rect.X; x < rect.X+rect.W; x++ {
		canvas.set(x, rect.Y, drawCell{Content: "─", Width: 1, Style: style})
		canvas.set(x, rect.Y+rect.H-1, drawCell{Content: "─", Width: 1, Style: style})
	}
	for y := rect.Y; y < rect.Y+rect.H; y++ {
		canvas.set(rect.X, y, drawCell{Content: "│", Width: 1, Style: style})
		canvas.set(rect.X+rect.W-1, y, drawCell{Content: "│", Width: 1, Style: style})
	}
	canvas.set(rect.X, rect.Y, drawCell{Content: "┌", Width: 1, Style: style})
	canvas.set(rect.X+rect.W-1, rect.Y, drawCell{Content: "┐", Width: 1, Style: style})
	canvas.set(rect.X, rect.Y+rect.H-1, drawCell{Content: "└", Width: 1, Style: style})
	canvas.set(rect.X+rect.W-1, rect.Y+rect.H-1, drawCell{Content: "┘", Width: 1, Style: style})
}

func drawPaneOverflowMarkers(canvas *composedCanvas, rect workbench.Rect, theme uiTheme, overflow paneOverflowHints, active bool) {
	if canvas == nil || rect.W < 3 || rect.H < 3 {
		return
	}
	if overflow.Right {
		markerFG := theme.panelMuted
		if active {
			markerFG = ensureContrast(mixHex(theme.chromeAccent, theme.panelText, 0.35), theme.hostBG, 4.2)
		}
		markerStyle := drawStyle{FG: markerFG, Bold: active}
		canvas.set(rect.X+rect.W-1, rect.Y+rect.H-2, drawCell{Content: ">", Width: 1, Style: markerStyle})
	}
	if overflow.Bottom {
		markerFG := theme.panelMuted
		if active {
			markerFG = ensureContrast(theme.warning, theme.hostBG, 4.2)
		}
		markerStyle := drawStyle{FG: markerFG, Bold: active}
		canvas.set(rect.X+rect.W-2, rect.Y+rect.H-1, drawCell{Content: "v", Width: 1, Style: markerStyle})
	}
}

const (
	borderConnUp = 1 << iota
	borderConnDown
	borderConnLeft
	borderConnRight
)

var borderGlyphConnections = map[string]uint8{
	"│": borderConnUp | borderConnDown,
	"─": borderConnLeft | borderConnRight,
	"┌": borderConnDown | borderConnRight,
	"┐": borderConnDown | borderConnLeft,
	"└": borderConnUp | borderConnRight,
	"┘": borderConnUp | borderConnLeft,
	"├": borderConnUp | borderConnDown | borderConnRight,
	"┤": borderConnUp | borderConnDown | borderConnLeft,
	"┬": borderConnDown | borderConnLeft | borderConnRight,
	"┴": borderConnUp | borderConnLeft | borderConnRight,
	"┼": borderConnUp | borderConnDown | borderConnLeft | borderConnRight,
}

var borderConnectionGlyph = map[uint8]string{
	borderConnUp | borderConnDown:                                    "│",
	borderConnLeft | borderConnRight:                                 "─",
	borderConnDown | borderConnRight:                                 "┌",
	borderConnDown | borderConnLeft:                                  "┐",
	borderConnUp | borderConnRight:                                   "└",
	borderConnUp | borderConnLeft:                                    "┘",
	borderConnUp | borderConnDown | borderConnRight:                  "├",
	borderConnUp | borderConnDown | borderConnLeft:                   "┤",
	borderConnDown | borderConnLeft | borderConnRight:                "┬",
	borderConnUp | borderConnLeft | borderConnRight:                  "┴",
	borderConnUp | borderConnDown | borderConnLeft | borderConnRight: "┼",
}

func drawHorizontalBorder(canvas *composedCanvas, startX, endX, y int, style drawStyle, sharedStart bool, downAtEnd bool, upAtEnd bool) {
	if canvas == nil || startX > endX {
		return
	}
	for x := startX; x <= endX; x++ {
		connections := uint8(0)
		if x == startX {
			if sharedStart {
				connections |= borderConnRight
			} else if upAtEnd {
				connections |= borderConnRight | borderConnUp
			} else {
				connections |= borderConnRight | borderConnDown
			}
		} else if x == endX {
			if upAtEnd {
				connections |= borderConnLeft | borderConnUp
			} else if downAtEnd {
				connections |= borderConnLeft | borderConnDown
			} else {
				connections |= borderConnLeft
			}
		} else {
			connections |= borderConnLeft | borderConnRight
		}
		mergeBorderCell(canvas, x, y, connections, style)
	}
}

func drawVerticalBorder(canvas *composedCanvas, x, startY, endY int, style drawStyle, sharedStart bool) {
	if canvas == nil || startY > endY {
		return
	}
	for y := startY; y <= endY; y++ {
		connections := uint8(0)
		if y == startY {
			if sharedStart {
				connections |= borderConnDown
			} else {
				connections |= borderConnUp | borderConnDown
			}
		} else {
			connections |= borderConnUp | borderConnDown
		}
		mergeBorderCell(canvas, x, y, connections, style)
	}
}

func verticalBorderStart(y int, sharedTop bool) int {
	if sharedTop {
		return y - 1
	}
	return y + 1
}

func mergeBorderCell(canvas *composedCanvas, x, y int, connections uint8, style drawStyle) {
	if canvas == nil || x < 0 || y < 0 || x >= canvas.width || y >= canvas.height {
		return
	}
	if existing, ok := borderGlyphConnections[canvas.cells[y][x].Content]; ok {
		connections |= existing
	}
	glyph, ok := borderConnectionGlyph[connections]
	if !ok {
		return
	}
	canvas.set(x, y, drawCell{Content: glyph, Width: 1, Style: style})
}

type paneChromeDrawStyles struct {
	Title         drawStyle
	Meta          drawStyle
	State         drawStyle
	Action        drawStyle
	EmphasizeRole bool
}

func drawPaneTopBorderLabels(canvas *composedCanvas, rect workbench.Rect, styles paneChromeDrawStyles, title string, border paneBorderInfo, floating bool) {
	layout, ok := paneTopBorderLabelsLayout(rect, title, border, paneChromeActionTokensForFrame(rect, title, border, floating))
	if canvas == nil || !ok {
		return
	}
	for _, slot := range layout.actionSlots {
		drawBorderLabel(canvas, slot.X, rect.Y, slot.Label, styles.Action)
	}
	if layout.titleLabel != "" {
		drawBorderLabel(canvas, layout.titleX, rect.Y, layout.titleLabel, styles.Title)
	}
	if layout.stateLabel != "" {
		drawBorderLabel(canvas, layout.stateX, rect.Y, layout.stateLabel, styles.State)
	}
	if layout.shareLabel != "" {
		drawBorderLabel(canvas, layout.shareX, rect.Y, layout.shareLabel, styles.Meta)
	}
	if layout.roleLabel != "" {
		roleStyle := styles.Meta
		if styles.EmphasizeRole {
			roleStyle = styles.Action
		}
		drawBorderLabel(canvas, layout.roleX, rect.Y, layout.roleLabel, roleStyle)
	}
	if layout.copyTimeLabel != "" {
		drawBorderLabel(canvas, layout.copyTimeX, rect.Y, layout.copyTimeLabel, styles.Meta)
	}
	if layout.copyRowLabel != "" {
		drawBorderLabel(canvas, layout.copyRowX, rect.Y, layout.copyRowLabel, styles.Meta)
	}
}

type paneBorderLabelsLayout struct {
	actionSlots   []paneChromeActionSlot
	titleX        int
	titleLabel    string
	stateX        int
	stateLabel    string
	shareX        int
	shareLabel    string
	roleX         int
	roleLabel     string
	copyTimeX     int
	copyTimeLabel string
	copyRowX      int
	copyRowLabel  string
}

type paneBorderSlot struct {
	label string
	kind  string
}

func paneTopBorderLabelsLayout(rect workbench.Rect, title string, border paneBorderInfo, actionTokens []paneChromeActionToken) (paneBorderLabelsLayout, bool) {
	if rect.W <= 4 {
		return paneBorderLabelsLayout{}, false
	}
	innerX := rect.X + 2
	innerW := rect.W - 4
	if innerW <= 0 {
		return paneBorderLabelsLayout{}, false
	}

	fullTitleLabel := normalizePaneBorderLabel(title)
	allSlots := make([]paneBorderSlot, 0, 5)
	if label := padPaneBorderSlot(border.StateLabel, paneBorderStateSlotWidth); label != "" {
		allSlots = append(allSlots, paneBorderSlot{kind: "state", label: label})
	}
	if label := padPaneBorderSlot(border.ShareLabel, paneBorderShareSlotWidth); label != "" {
		allSlots = append(allSlots, paneBorderSlot{kind: "share", label: label})
	}
	if label := paneBorderRoleSlot(border.RoleLabel); label != "" {
		allSlots = append(allSlots, paneBorderSlot{kind: "role", label: label})
	}
	if label := normalizePaneBorderLabel(border.CopyTimeLabel); label != "" {
		allSlots = append(allSlots, paneBorderSlot{kind: "copy-time", label: label})
	}
	if label := normalizePaneBorderLabel(border.CopyRowLabel); label != "" {
		allSlots = append(allSlots, paneBorderSlot{kind: "copy-row", label: label})
	}
	titleFullWidth := xansi.StringWidth(fullTitleLabel)
	active := paneBorderSlotsForWidth(allSlots, maxInt(0, innerW-titleFullWidth))
	if fullTitleLabel == "" && len(active) == 0 {
		return paneBorderLabelsLayout{}, false
	}

	actionCount := len(actionTokens)
	reservedStatuses := paneBorderSlotsWidth(active)
	preferActionCluster := len(actionTokens) > 0 && actionTokens[0].Kind == HitRegionPaneCenterFloating
	for {
		reservedRight := reservedStatuses + visiblePaneChromeActionClusterWidth(actionTokens, actionCount, preferActionCluster)
		titleBudget := innerW - reservedRight
		titleFits := titleFullWidth <= titleBudget
		if preferActionCluster {
			titleFits = titleBudget >= 1
		}
		if titleFits || (fullTitleLabel == "" && reservedRight <= innerW) {
			break
		}
		if actionCount > 0 {
			actionCount--
			continue
		}
		removeIdx := paneBorderSlotRemovalIndex(active)
		if removeIdx >= 0 {
			active = append(active[:removeIdx], active[removeIdx+1:]...)
			reservedStatuses = paneBorderSlotsWidth(active)
			continue
		}
		break
	}
	titleLabel := xansi.Truncate(fullTitleLabel, maxInt(0, innerW-reservedStatuses-visiblePaneChromeActionClusterWidth(actionTokens, actionCount, preferActionCluster)), "")
	if titleLabel == "" && len(active) == 0 && actionCount == 0 {
		return paneBorderLabelsLayout{}, false
	}

	layout := paneBorderLabelsLayout{
		actionSlots: make([]paneChromeActionSlot, actionCount),
		titleX:      innerX,
		titleLabel:  titleLabel,
	}
	visibleActionTokens := visiblePaneChromeActionTokens(actionTokens, actionCount, preferActionCluster)
	right := innerX + innerW
	actionXs := make([]int, actionCount)
	for i := actionCount - 1; i >= 0; i-- {
		labelW := xansi.StringWidth(visibleActionTokens[i].Label)
		right -= labelW
		actionXs[i] = right
		if i > 0 {
			right -= paneChromeActionGap
		}
	}
	if len(active) > 0 && actionCount > 0 {
		right--
	}
	for i := len(active) - 1; i >= 0; i-- {
		slot := active[i]
		slotW := xansi.StringWidth(slot.label)
		x := right - slotW
		switch slot.kind {
		case "state":
			layout.stateX = x
			layout.stateLabel = slot.label
		case "share":
			layout.shareX = x
			layout.shareLabel = slot.label
		case "role":
			layout.roleX = x
			layout.roleLabel = slot.label
		case "copy-time":
			layout.copyTimeX = x
			layout.copyTimeLabel = slot.label
		case "copy-row":
			layout.copyRowX = x
			layout.copyRowLabel = slot.label
		}
		right = x - 1
	}
	for i := 0; i < actionCount; i++ {
		token := visibleActionTokens[i]
		layout.actionSlots[i] = paneChromeActionSlot{
			Kind:  token.Kind,
			Label: token.Label,
			X:     actionXs[i],
		}
	}
	return layout, true
}

func visiblePaneChromeActionTokens(tokens []paneChromeActionToken, count int, preferSuffix bool) []paneChromeActionToken {
	if count <= 0 || len(tokens) == 0 {
		return nil
	}
	if count >= len(tokens) {
		return tokens
	}
	if preferSuffix {
		return tokens[len(tokens)-count:]
	}
	return tokens[:count]
}

func visiblePaneChromeActionClusterWidth(tokens []paneChromeActionToken, count int, preferSuffix bool) int {
	return paneChromeActionClusterWidth(visiblePaneChromeActionTokens(tokens, count, preferSuffix), count)
}

func paneBorderSlotsForWidth(slots []paneBorderSlot, width int) []paneBorderSlot {
	if len(slots) == 0 || width <= 0 {
		return nil
	}
	active := append([]paneBorderSlot(nil), slots...)
	for paneBorderSlotsWidth(active) > width {
		removeIdx := paneBorderSlotRemovalIndex(active)
		if removeIdx < 0 {
			break
		}
		active = append(active[:removeIdx], active[removeIdx+1:]...)
	}
	if paneBorderSlotsWidth(active) > width {
		return nil
	}
	return active
}

func paneBorderSlotRemovalIndex(slots []paneBorderSlot) int {
	for _, kind := range []string{"share", "state", "role", "copy-time", "copy-row"} {
		for i := len(slots) - 1; i >= 0; i-- {
			if slots[i].kind == kind {
				return i
			}
		}
	}
	return -1
}

func normalizePaneBorderLabel(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return " " + text + " "
}

func padPaneBorderSlot(text string, width int) string {
	if strings.TrimSpace(text) == "" || width <= 0 {
		return ""
	}
	text = xansi.Truncate(strings.TrimSpace(text), width, "")
	pad := maxInt(0, width-xansi.StringWidth(text))
	left := pad / 2
	right := pad - left
	return strings.Repeat(" ", left) + text + strings.Repeat(" ", right)
}

func paneBorderRoleSlot(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return padPaneBorderSlot(text, paneBorderRoleSlotWidth)
}

func paneBorderSlotsWidth(slots []paneBorderSlot) int {
	total := 0
	for i, slot := range slots {
		total += xansi.StringWidth(slot.label)
		if i > 0 {
			total++
		}
	}
	return total
}

func drawBorderLabel(canvas *composedCanvas, x, y int, text string, style drawStyle) {
	if canvas == nil || strings.TrimSpace(text) == "" {
		return
	}
	canvas.drawText(x, y, text, style)
}

func PaneOwnerButtonRect(pane workbench.VisiblePane, runtimeState *VisibleRuntimeStateProxy, confirmPaneID string) (workbench.Rect, bool) {
	lookup := newRuntimeLookup(runtimeState)
	title := displayPaneTitleWithLookup(pane, lookup)
	border := paneBorderInfoWithLookup(pane, lookup, confirmPaneID)
	layout, ok := paneTopBorderLabelsLayout(
		pane.Rect,
		title,
		border,
		paneChromeActionTokensForPane(pane, title, border),
	)
	if !ok || layout.roleLabel == "" {
		return workbench.Rect{}, false
	}
	actionLabel := paneOwnerActionLabel(pane, lookup, confirmPaneID)
	if actionLabel == "" {
		return workbench.Rect{}, false
	}
	return workbench.Rect{
		X: layout.roleX,
		Y: pane.Rect.Y,
		W: xansi.StringWidth(layout.roleLabel),
		H: 1,
	}, true
}

func paneOwnerActionLabel(pane workbench.VisiblePane, lookup runtimeLookup, confirmPaneID string) string {
	if pane.TerminalID == "" || lookup.paneRole(pane.ID) != "follower" {
		return ""
	}
	if confirmPaneID == pane.ID {
		return ownerConfirmLabel
	}
	return "follow"
}
