package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lozzow/termx/protocol"
	btui "github.com/lozzow/termx/tui/bt"
	layoutresolvedomain "github.com/lozzow/termx/tui/domain/layoutresolve"
	promptdomain "github.com/lozzow/termx/tui/domain/prompt"
	terminalmanagerdomain "github.com/lozzow/termx/tui/domain/terminalmanager"
	terminalpickerdomain "github.com/lozzow/termx/tui/domain/terminalpicker"
	"github.com/lozzow/termx/tui/domain/types"
	workspacedomain "github.com/lozzow/termx/tui/domain/workspace"
)

type runtimeRenderer struct {
	Screens RuntimeTerminalStore
}

const runtimeScreenPreviewRows = 8
const runtimeOverlayPreviewRows = 8
const runtimeOverlayDetailPreviewRows = 4
const runtimeTerminalManagerPreviewRows = 4
const runtimeBarMaxWidth = 96
const runtimeSummaryMaxWidth = 240
const runtimeDetailMaxWidth = 240
const runtimeOutlinePreviewMaxWidth = 48
const runtimeWireframeWidth = 78
const runtimeWireframeOverlayWidth = 58
const runtimeWireframeSplitColumnWidth = 38
const runtimeWireframeMainPaneWidth = 52
const runtimeWireframeSidebarWidth = 24
const runtimeWireframePreviewRows = 4

// Render 先提供一个稳定、可测试的文本视图，优先把生命周期打通。
// 这里不追求视觉完成度，只把当前 workspace / tab / pane / overlay 这些主语义明确展示出来。
func (r runtimeRenderer) Render(state types.AppState, notices []btui.Notice) string {
	workspace, ok := state.Domain.Workspaces[state.Domain.ActiveWorkspaceID]
	if !ok {
		return "termx\nno workspace"
	}
	tab, ok := workspace.Tabs[workspace.ActiveTabID]
	if !ok {
		return fmt.Sprintf("termx\nworkspace: %s\nno tab", workspace.Name)
	}
	pane, ok := tab.Panes[tab.ActivePaneID]
	if !ok {
		return fmt.Sprintf("termx\nworkspace: %s\ntab: %s\nno pane", workspace.Name, tab.Name)
	}

	statusLines := renderStatusSection(workspace, tab, pane, state.UI)
	overlayActive := state.UI.Overlay.Kind != types.OverlayNone

	lines := []string{"termx"}
	lines = append(lines, r.renderWireframeView(state, workspace, tab, pane)...)
	lines = appendChrome(lines, "header", []string{
		renderHeaderBar(workspace, tab, pane, state.UI),
		renderWorkspaceBar(workspace),
		renderWorkspaceSummary(workspace),
		renderTabStrip(workspace),
		renderTabSummary(tab),
		renderTabPathBar(state, workspace, tab, pane),
		renderTabLayerBar(tab),
	}, func(lines []string) []string {
		return appendSection(lines, "status", statusLines)
	})
	lines = appendChrome(lines, "body", []string{r.renderBodyBar(state, pane, overlayActive)}, func(lines []string) []string {
		lines = append(lines, renderPaneBar(state, pane))
		lines = append(lines, renderTiledOutlineBar(tab)...)
		lines = append(lines, renderTiledLayout(tab)...)
		lines = append(lines, r.renderTiledTree(state, tab)...)
		lines = append(lines, r.renderTiledOutline(state, tab)...)
		lines = append(lines, renderFloatingOutlineBar(tab)...)
		lines = append(lines, r.renderFloatingOutline(state, tab)...)
		lines = appendSection(lines, "terminal", r.renderTerminalSection(state, pane, overlayActive))
		lines = appendSection(lines, "screen", r.renderScreenSection(pane, overlayActive))
		return appendSection(lines, "overlay", renderOverlayLines(state.UI.Overlay, state.UI.Focus))
	})
	lines = appendChrome(lines, "footer", []string{
		renderFooterBar(notices, state.UI.Overlay.Kind),
		renderFocusBar(state, pane),
		renderShortcutBar(state, pane),
	}, func(lines []string) []string {
		return appendSection(lines, "notices", renderNoticeLines(notices))
	})
	return strings.Join(lines, "\n")
}

func appendSection(lines []string, name string, body []string) []string {
	if len(body) == 0 {
		return lines
	}
	lines = append(lines, fmt.Sprintf("section_%s:", name))
	return append(lines, body...)
}

func appendChrome(lines []string, name string, body []string, fn func([]string) []string) []string {
	lines = append(lines, fmt.Sprintf("chrome_%s:", name))
	lines = append(lines, body...)
	if fn != nil {
		return fn(lines)
	}
	return lines
}

// renderWireframeView 在语义 renderer 之上补一层稳定的 ASCII 工作台，
// 让 `cmd/termx` 即使还没接入完整几何布局，也先具备“看起来像真正 TUI”的主视图。
func (r runtimeRenderer) renderWireframeView(state types.AppState, workspace types.WorkspaceState, tab types.TabState, pane types.PaneState) []string {
	lines := []string{"wireframe_view:"}
	layer := tab.ActiveLayer
	if layer == "" {
		layer = types.FocusLayerTiled
	}
	focus := state.UI.Focus.Layer
	if focus == "" {
		focus = types.FocusLayerTiled
	}
	lines = append(lines, renderASCIIBox(runtimeWireframeWidth, []string{
		fmt.Sprintf("WORKSPACE[%s] TAB[%s] LAYER[%s] FOCUS[%s] OVERLAY[%s]", safeWorkspaceLabel(workspace), safeTabLabel(tab), layer, focus, state.UI.Overlay.Kind),
		fmt.Sprintf("PATH[%s/%s/%s:%s]", safeWorkspaceLabel(workspace), safeTabLabel(tab), safePaneKind(pane.Kind), pane.ID),
	})...)
	lines = append(lines, r.renderWireframeWorkbench(state, tab, pane)...)
	if overlayLines := r.renderWireframeOverlayDialog(state); len(overlayLines) > 0 {
		lines = append(lines, overlayLines...)
	}
	return lines
}

func (r runtimeRenderer) renderWireframeWorkbench(state types.AppState, tab types.TabState, pane types.PaneState) []string {
	tiledPaneIDs := orderedTiledPaneIDs(tab)
	floatingPaneIDs := orderedFloatingPaneIDs(tab)
	switch {
	case len(tiledPaneIDs) > 1:
		return r.renderWireframeSplitWorkbench(state, tab, tiledPaneIDs, floatingPaneIDs)
	case len(floatingPaneIDs) > 0:
		return r.renderWireframeFloatingWorkbench(state, tab, pane, floatingPaneIDs)
	default:
		return renderASCIIBox(runtimeWireframeWidth, append([]string{"WORKBENCH single"}, r.renderWireframePaneCard(state, pane, true)...))
	}
}

func (r runtimeRenderer) renderWireframeSplitWorkbench(state types.AppState, tab types.TabState, tiledPaneIDs []types.PaneID, floatingPaneIDs []types.PaneID) []string {
	summary := summarizeTiledLayout(tab.RootSplit, len(tiledPaneIDs))
	lines := renderASCIIBox(runtimeWireframeWidth, []string{
		"WORKBENCH split",
		renderWireframeSplitSummary(summary),
	})
	overviewLines := make([]string, 0, 2)
	for i, paneID := range tiledPaneIDs {
		if i == 2 {
			break
		}
		pane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		overviewLines = append(overviewLines, renderWireframePaneStateSummary(state, pane, paneID == tab.ActivePaneID))
	}
	if len(overviewLines) > 0 {
		lines = append(lines, renderASCIIBox(runtimeWireframeWidth, overviewLines)...)
	}
	columnBoxes := make([][]string, 0, 2)
	for i, paneID := range tiledPaneIDs {
		if i == 2 {
			break
		}
		pane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		columnBoxes = append(columnBoxes, renderASCIIBox(runtimeWireframeSplitColumnWidth, r.renderWireframePaneCard(state, pane, paneID == tab.ActivePaneID)))
	}
	if len(columnBoxes) == 1 {
		columnBoxes = append(columnBoxes, renderASCIIBox(runtimeWireframeSplitColumnWidth, []string{"PANE[missing]", "slot[missing]"}))
	}
	lines = append(lines, joinASCIIBoxes(columnBoxes, 2)...)
	if len(tiledPaneIDs) > 2 {
		extraLines := []string{"EXTRA PANES"}
		for _, paneID := range tiledPaneIDs[2:] {
			pane, ok := tab.Panes[paneID]
			if !ok {
				continue
			}
			extraLines = append(extraLines, renderWireframePaneStateSummary(state, pane, paneID == tab.ActivePaneID))
		}
		lines = append(lines, renderASCIIBox(runtimeWireframeWidth, extraLines)...)
	}
	if len(floatingPaneIDs) > 0 {
		lines = append(lines, renderASCIIBox(runtimeWireframeWidth, r.renderWireframeFloatingStackBody(state, tab, floatingPaneIDs))...)
	}
	return lines
}

func renderWireframeSplitSummary(summary tiledLayoutSummary) string {
	parts := []string{
		fmt.Sprintf("SPLIT[%s]", summary.Root),
		fmt.Sprintf("LEAVES[%d]", summary.Leaves),
	}
	if summary.Root == "" {
		parts[0] = "SPLIT[implicit]"
	}
	if summary.HasRatio {
		parts = append([]string{fmt.Sprintf("SPLIT[%s]", summary.Root), fmt.Sprintf("RATIO[%.2f]", summary.Ratio)}, parts[1:]...)
	}
	if !summary.HasRatio {
		parts = append([]string{fmt.Sprintf("SPLIT[%s]", summary.Root), "RATIO[n/a]"}, parts[1:]...)
	}
	return strings.Join(parts, " ")
}

func (r runtimeRenderer) renderWireframeFloatingWorkbench(state types.AppState, tab types.TabState, pane types.PaneState, floatingPaneIDs []types.PaneID) []string {
	mainBox := renderASCIIBox(runtimeWireframeMainPaneWidth, append([]string{"WORKBENCH floating"}, r.renderWireframePaneCard(state, pane, true)...))
	sidebarBox := renderASCIIBox(runtimeWireframeSidebarWidth, r.renderWireframeFloatingStackBody(state, tab, floatingPaneIDs))
	lines := joinASCIIBoxes([][]string{mainBox, sidebarBox}, 2)
	summaryLines := r.renderWireframeFloatingStackSummary(state, tab, floatingPaneIDs)
	if len(summaryLines) > 0 {
		lines = append(lines, renderASCIIBox(runtimeWireframeWidth, summaryLines)...)
	}
	return lines
}

// renderWireframePaneCard 用统一卡片表达 pane 的控制关系、terminal 状态和最近屏幕内容，
// 这样 single/split/floating 三种工作台都能复用同一套最小可读块。
func (r runtimeRenderer) renderWireframePaneCard(state types.AppState, pane types.PaneState, active bool) []string {
	cardKind := "PANE"
	if active {
		cardKind = "ACTIVE"
	}
	role := renderTerminalRole(state.Domain.Connections[pane.TerminalID], pane.ID)
	if role == "" {
		role = string(pane.SlotState)
	}
	lines := []string{
		fmt.Sprintf("%s[%s] ROLE[%s] KIND[%s] SLOT[%s]", cardKind, renderPaneTitle(state, pane), role, safePaneKind(pane.Kind), pane.SlotState),
	}
	if pane.TerminalID != "" {
		terminalState := "unknown"
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.State != "" {
			terminalState = string(terminal.State)
		}
		lines = append(lines,
			fmt.Sprintf("%s[%s] ROLE[%s] STATE[%s]", cardKind, renderPaneTitle(state, pane), role, terminalState),
			fmt.Sprintf("TERM[%s] STATE[%s]", pane.TerminalID, terminalState),
		)
	} else {
		lines = append(lines, fmt.Sprintf("%s[%s] SLOT[%s]", cardKind, renderPaneTitle(state, pane), pane.SlotState))
	}
	if pane.Kind == types.PaneKindFloating && (pane.Rect.W > 0 || pane.Rect.H > 0) {
		lines = append(lines, fmt.Sprintf("RECT[%d,%d %dx%d]", pane.Rect.X, pane.Rect.Y, pane.Rect.W, pane.Rect.H))
	}
	lines = append(lines, r.renderWireframePanePreviewLines(state, pane)...)
	return lines
}

func (r runtimeRenderer) renderWireframePanePreviewLines(state types.AppState, pane types.PaneState) []string {
	if state.UI.Overlay.Kind != types.OverlayNone {
		return []string{"PREVIEW suppressed by overlay"}
	}
	if pane.TerminalID == "" || r.Screens == nil {
		return renderWireframePaneSlotPreview(state, pane)
	}
	snapshot, ok := r.Screens.Snapshot(pane.TerminalID)
	if !ok || snapshot == nil {
		return renderWireframePaneSlotPreview(state, pane)
	}
	rows, _, _ := renderSnapshotRows(snapshot)
	if len(rows) > runtimeWireframePreviewRows {
		rows = rows[len(rows)-runtimeWireframePreviewRows:]
	}
	if len(rows) == 0 {
		return []string{"PREVIEW[empty]"}
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, fmt.Sprintf("PREVIEW %s", row))
	}
	return lines
}

func renderWireframePaneStateSummary(state types.AppState, pane types.PaneState, active bool) string {
	cardKind := "PANE"
	if active {
		cardKind = "ACTIVE"
	}
	role := renderTerminalRole(state.Domain.Connections[pane.TerminalID], pane.ID)
	if role == "" {
		role = string(pane.SlotState)
	}
	if pane.TerminalID != "" {
		terminalState := "unknown"
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.State != "" {
			terminalState = string(terminal.State)
		}
		return fmt.Sprintf("%s[%s] ROLE[%s] STATE[%s]", cardKind, renderPaneTitle(state, pane), role, terminalState)
	}
	return fmt.Sprintf("%s[%s] SLOT[%s]", cardKind, renderPaneTitle(state, pane), pane.SlotState)
}

func renderWireframePaneSlotPreview(state types.AppState, pane types.PaneState) []string {
	switch pane.SlotState {
	case types.PaneSlotWaiting:
		return []string{"PREVIEW waiting for connect", "ACTION[n] new | ACTION[a] connect"}
	case types.PaneSlotExited:
		exitCode := ""
		if pane.LastExitCode != nil {
			exitCode = fmt.Sprintf(" EXIT[%d]", *pane.LastExitCode)
		} else if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.ExitCode != nil {
			exitCode = fmt.Sprintf(" EXIT[%d]", *terminal.ExitCode)
		}
		return []string{fmt.Sprintf("PREVIEW exited history retained%s", exitCode), "ACTION[r] restart | ACTION[a] connect"}
	case types.PaneSlotEmpty:
		return []string{"PREVIEW unconnected pane", "ACTION[n] new | ACTION[m] manager"}
	default:
		return []string{"PREVIEW unavailable"}
	}
}

func (r runtimeRenderer) renderWireframeFloatingStackBody(state types.AppState, tab types.TabState, floatingPaneIDs []types.PaneID) []string {
	lines := []string{"FLOATING STACK"}
	for _, paneID := range floatingPaneIDs {
		pane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		role := renderTerminalRole(state.Domain.Connections[pane.TerminalID], pane.ID)
		if role == "" {
			role = string(pane.SlotState)
		}
		lines = append(lines, fmt.Sprintf("FLOAT[%s] %s %s %d,%d %dx%d", paneID, renderPaneTitle(state, pane), role, pane.Rect.X, pane.Rect.Y, pane.Rect.W, pane.Rect.H))
		if preview := r.renderPanePreview(pane.TerminalID); preview != "" {
			lines = append(lines, fmt.Sprintf("PREVIEW %s", preview))
		}
	}
	if len(lines) == 1 {
		lines = append(lines, "FLOAT[none]")
	}
	return lines
}

func (r runtimeRenderer) renderWireframeFloatingStackSummary(state types.AppState, tab types.TabState, floatingPaneIDs []types.PaneID) []string {
	if len(floatingPaneIDs) == 0 {
		return nil
	}
	lines := []string{"FLOATING STACK"}
	for _, paneID := range floatingPaneIDs {
		pane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		role := renderTerminalRole(state.Domain.Connections[pane.TerminalID], pane.ID)
		if role == "" {
			role = string(pane.SlotState)
		}
		lines = append(lines, fmt.Sprintf("FLOAT[%s] %s %s %d,%d %dx%d", paneID, renderPaneTitle(state, pane), role, pane.Rect.X, pane.Rect.Y, pane.Rect.W, pane.Rect.H))
	}
	return lines
}

func (r runtimeRenderer) renderWireframeOverlayDialog(state types.AppState) []string {
	if state.UI.Overlay.Kind == types.OverlayNone {
		return nil
	}
	body := []string{fmt.Sprintf("OVERLAY[%s] FOCUS[%s]", state.UI.Overlay.Kind, state.UI.Focus.Layer)}
	body = append(body, renderWireframeOverlayBody(state.UI.Overlay)...)
	box := renderASCIIBox(runtimeWireframeOverlayWidth, body)
	padding := (runtimeWireframeWidth - runtimeWireframeOverlayWidth) / 2
	if padding < 0 {
		padding = 0
	}
	return indentLines(box, padding)
}

func renderWireframeOverlayBody(overlay types.OverlayState) []string {
	switch overlay.Kind {
	case types.OverlayTerminalManager:
		manager, ok := overlay.Data.(*terminalmanagerdomain.State)
		if !ok || manager == nil {
			return []string{"ROWS[0]"}
		}
		rows := manager.VisibleRows()
		selectedID := "none"
		selectedIndex := 0
		if row, ok := manager.SelectedRow(); ok {
			if row.TerminalID != "" {
				selectedID = string(row.TerminalID)
			} else {
				selectedID = row.Label
			}
			for idx, candidate := range rows {
				if candidate.Kind == row.Kind && candidate.TerminalID == row.TerminalID && candidate.Label == row.Label {
					selectedIndex = idx
					break
				}
			}
		}
		lines := []string{fmt.Sprintf("ROWS[%d] SELECTED[%s]", len(rows), selectedID)}
		previewRows, _ := overlayPreviewRowsAround(rows, runtimeOverlayDetailPreviewRows, selectedIndex)
		for _, row := range previewRows {
			prefix := "  "
			if selected, ok := manager.SelectedRow(); ok && selected.Kind == row.Kind && selected.TerminalID == row.TerminalID && selected.Label == row.Label {
				prefix = "> "
			}
			lines = append(lines, fmt.Sprintf("%s[%s] %s", prefix, row.Kind, row.Label))
		}
		if detail, ok := manager.SelectedDetail(); ok {
			lines = append(lines,
				fmt.Sprintf("DETAIL[%s] STATE[%s] VIS[%s]", detail.Name, detail.State, detail.VisibilityLabel),
				fmt.Sprintf("LOCATIONS[%d] OWNER[%s]", len(detail.Locations), detail.OwnerSlotLabel),
			)
		}
		lines = append(lines, "ACTIONS[jump connect_here new_tab floating]")
		return lines
	case types.OverlayWorkspacePicker:
		picker, ok := overlay.Data.(*workspacedomain.PickerState)
		if !ok || picker == nil {
			return []string{"ROWS[0]"}
		}
		rows := picker.VisibleRows()
		selectedIndex := 0
		selectedKey := "none"
		if selected, ok := picker.SelectedRow(); ok {
			selectedKey = selected.Node.Key
			for idx, row := range rows {
				if row.Node.Key == selected.Node.Key {
					selectedIndex = idx
					break
				}
			}
		}
		lines := []string{fmt.Sprintf("ROWS[%d] QUERY[%s] SELECTED[%s]", len(rows), picker.Query(), selectedKey)}
		if selected, ok := picker.SelectedRow(); ok {
			lines = append(lines, fmt.Sprintf("TARGET[%s] LABEL[%s] DEPTH[%d]", selected.Node.Kind, selected.Node.Label, selected.Depth))
		}
		previewRows, _ := overlayPreviewRowsAround(rows, 6, selectedIndex)
		for _, row := range previewRows {
			prefix := "  "
			if selected, ok := picker.SelectedRow(); ok && selected.Node.Key == row.Node.Key {
				prefix = "> "
			}
			lines = append(lines, fmt.Sprintf("%s%s[%s] %s", prefix, strings.Repeat("  ", row.Depth), row.Node.Kind, row.Node.Label))
		}
		return lines
	case types.OverlayTerminalPicker:
		picker, ok := overlay.Data.(*terminalpickerdomain.State)
		if !ok || picker == nil {
			return []string{"ROWS[0]"}
		}
		rows := picker.VisibleRows()
		lines := []string{fmt.Sprintf("ROWS[%d] QUERY[%s]", len(rows), picker.Query())}
		for _, row := range rows {
			prefix := "  "
			if selected, ok := picker.SelectedRow(); ok && selected.Kind == row.Kind && selected.TerminalID == row.TerminalID && selected.Label == row.Label {
				prefix = "> "
			}
			lines = append(lines, fmt.Sprintf("%s[%s] %s", prefix, row.Kind, row.Label))
			if len(lines) >= runtimeOverlayDetailPreviewRows+1 {
				break
			}
		}
		return lines
	case types.OverlayLayoutResolve:
		resolve, ok := overlay.Data.(*layoutresolvedomain.State)
		if !ok || resolve == nil {
			return []string{"ROWS[0]"}
		}
		rows := resolve.Rows()
		lines := []string{
			fmt.Sprintf("ROWS[%d] PANE[%s] ROLE[%s]", len(rows), resolve.PaneID, resolve.Role),
			fmt.Sprintf("HINT[%s]", resolve.Hint),
		}
		for _, row := range rows {
			prefix := "  "
			if selected, ok := resolve.SelectedRow(); ok && selected.Action == row.Action && selected.Label == row.Label {
				prefix = "> "
			}
			lines = append(lines, fmt.Sprintf("%s[%s] %s", prefix, row.Action, row.Label))
			if len(lines) >= runtimeOverlayDetailPreviewRows+1 {
				break
			}
		}
		return lines
	case types.OverlayPrompt:
		prompt, ok := overlay.Data.(*promptdomain.State)
		if !ok || prompt == nil {
			return []string{"PROMPT[draft]"}
		}
		lines := []string{
			fmt.Sprintf("PROMPT[%s]", prompt.Kind),
			fmt.Sprintf("TITLE[%s]", prompt.Title),
		}
		if len(prompt.Fields) == 0 {
			lines = append(lines, fmt.Sprintf("> [draft] %s", prompt.Draft))
			lines = append(lines, "ACTIONS[submit cancel]")
			return lines
		}
		active := prompt.Active
		if active < 0 || active >= len(prompt.Fields) {
			active = 0
		}
		lines = append(lines, fmt.Sprintf("ACTIVE[%s] VALUE[%s]", prompt.Fields[active].Key, prompt.Fields[active].Value))
		previewFields, _ := overlayPreviewRowsAround(prompt.Fields, runtimeOverlayDetailPreviewRows, active)
		for _, field := range previewFields {
			prefix := "  "
			if field.Key == prompt.Fields[active].Key && field.Label == prompt.Fields[active].Label {
				prefix = "> "
			}
			lines = append(lines, fmt.Sprintf("%s[%s] %s: %s", prefix, field.Key, field.Label, field.Value))
		}
		lines = append(lines, "ACTIONS[submit cancel]")
		return lines
	case types.OverlayHelp:
		return []string{
			"HELP[shortcuts]",
			"  Ctrl-p pane | Ctrl-t tab",
			"  Ctrl-w workspace | Ctrl-f picker",
		}
	default:
		return []string{fmt.Sprintf("OVERLAY[%s]", overlay.Kind)}
	}
}

func renderASCIIBox(width int, body []string) []string {
	if width < 4 {
		width = 4
	}
	innerWidth := width - 2
	lines := []string{"+" + strings.Repeat("-", innerWidth) + "+"}
	for _, line := range body {
		lines = append(lines, "|"+padRight(truncateLine(line, innerWidth), innerWidth)+"|")
	}
	lines = append(lines, "+"+strings.Repeat("-", innerWidth)+"+")
	return lines
}

func joinASCIIBoxes(boxes [][]string, gap int) []string {
	if len(boxes) == 0 {
		return nil
	}
	if len(boxes) == 1 {
		return boxes[0]
	}
	height := 0
	widths := make([]int, len(boxes))
	for index, box := range boxes {
		if len(box) > height {
			height = len(box)
		}
		if len(box) > 0 {
			widths[index] = len(box[0])
		}
	}
	spacer := strings.Repeat(" ", gap)
	lines := make([]string, 0, height)
	for row := 0; row < height; row++ {
		var builder strings.Builder
		for index, box := range boxes {
			if index > 0 {
				builder.WriteString(spacer)
			}
			if row < len(box) {
				builder.WriteString(box[row])
				continue
			}
			builder.WriteString(strings.Repeat(" ", widths[index]))
		}
		lines = append(lines, builder.String())
	}
	return lines
}

func indentLines(lines []string, padding int) []string {
	if padding <= 0 {
		return lines
	}
	prefix := strings.Repeat(" ", padding)
	indented := make([]string, 0, len(lines))
	for _, line := range lines {
		indented = append(indented, prefix+line)
	}
	return indented
}

func padRight(line string, width int) string {
	if len(line) >= width {
		return line
	}
	return line + strings.Repeat(" ", width-len(line))
}

func safeWorkspaceLabel(workspace types.WorkspaceState) string {
	if workspace.Name != "" {
		return workspace.Name
	}
	return string(workspace.ID)
}

func safeTabLabel(tab types.TabState) string {
	if tab.Name != "" {
		return tab.Name
	}
	return string(tab.ID)
}

func safePaneKind(kind types.PaneKind) types.PaneKind {
	if kind != "" {
		return kind
	}
	return types.PaneKindTiled
}

// renderHeaderBar 把主工作区、pane、overlay、focus、mode 汇总成顶栏语义，
// 这样主视图即使正文被压缩，顶栏仍然能稳定表达当前上下文。
func renderHeaderBar(workspace types.WorkspaceState, tab types.TabState, pane types.PaneState, ui types.UIState) string {
	parts := []string{
		fmt.Sprintf("header_bar: ws=%s", workspace.Name),
		fmt.Sprintf("tab=%s", tab.Name),
		fmt.Sprintf("pane=%s", pane.ID),
		fmt.Sprintf("slot=%s", pane.SlotState),
		fmt.Sprintf("overlay=%s", ui.Overlay.Kind),
		fmt.Sprintf("focus=%s", ui.Focus.Layer),
	}
	if ui.Mode.Active != types.ModeNone {
		parts = append(parts, fmt.Sprintf("mode=%s", ui.Mode.Active))
	}
	return compactBarLine(parts...)
}

// renderFooterBar 把 notice 聚合状态和当前 overlay 汇总到底栏，
// 底栏优先回答“现在有没有问题、当前是不是处在特殊交互里”。
func renderFooterBar(notices []btui.Notice, overlay types.OverlayKind) string {
	parts := []string{fmt.Sprintf("footer_bar: notices=%d", countVisibleNotices(notices))}
	if last, ok := lastVisibleNotice(notices); ok {
		parts = append(parts, fmt.Sprintf("last=%s", last.Level))
	}
	parts = append(parts, fmt.Sprintf("overlay=%s", overlay))
	return compactBarLine(parts...)
}

func renderWorkspaceBar(workspace types.WorkspaceState) string {
	label := workspace.Name
	if label == "" {
		label = string(workspace.ID)
	}
	return compactSummaryLine(fmt.Sprintf("workspace_bar: [%s]", label))
}

func renderWorkspaceSummary(workspace types.WorkspaceState) string {
	tabs := len(workspace.TabOrder)
	panes := 0
	terminals := map[types.TerminalID]struct{}{}
	floating := 0
	for _, tabID := range workspace.TabOrder {
		tab, ok := workspace.Tabs[tabID]
		if !ok {
			continue
		}
		for _, pane := range tab.Panes {
			panes++
			if pane.Kind == types.PaneKindFloating {
				floating++
			}
			if pane.TerminalID != "" {
				terminals[pane.TerminalID] = struct{}{}
			}
		}
	}
	return compactSummaryLine(
		fmt.Sprintf("workspace_summary: tabs=%d", tabs),
		fmt.Sprintf("panes=%d", panes),
		fmt.Sprintf("terminals=%d", len(terminals)),
		fmt.Sprintf("floating=%d", floating),
	)
}

func renderTabStrip(workspace types.WorkspaceState) string {
	parts := make([]string, 0, len(workspace.TabOrder))
	for _, tabID := range workspace.TabOrder {
		tab, ok := workspace.Tabs[tabID]
		if !ok {
			continue
		}
		label := tab.Name
		if label == "" {
			label = string(tab.ID)
		}
		if tabID == workspace.ActiveTabID {
			parts = append(parts, fmt.Sprintf("[%s]", label))
			continue
		}
		parts = append(parts, label)
	}
	if len(parts) == 0 {
		return "tab_strip: <none>"
	}
	return compactSummaryLine(fmt.Sprintf("tab_strip: %s", strings.Join(parts, " | ")))
}

func renderTabSummary(tab types.TabState) string {
	tiled := 0
	floating := 0
	connected := 0
	waiting := 0
	exited := 0
	empty := 0
	for _, pane := range tab.Panes {
		switch pane.Kind {
		case types.PaneKindFloating:
			floating++
		default:
			tiled++
		}
		switch pane.SlotState {
		case types.PaneSlotConnected:
			connected++
		case types.PaneSlotWaiting:
			waiting++
		case types.PaneSlotExited:
			exited++
		case types.PaneSlotEmpty:
			empty++
		}
	}
	return compactSummaryLine(
		fmt.Sprintf("tab_summary: tiled=%d", tiled),
		fmt.Sprintf("floating=%d", floating),
		fmt.Sprintf("connected=%d", connected),
		fmt.Sprintf("waiting=%d", waiting),
		fmt.Sprintf("exited=%d", exited),
		fmt.Sprintf("empty=%d", empty),
		fmt.Sprintf("active_layer=%s", tab.ActiveLayer),
	)
}

func renderTabPathBar(state types.AppState, workspace types.WorkspaceState, tab types.TabState, pane types.PaneState) string {
	workspaceLabel := workspace.Name
	if workspaceLabel == "" {
		workspaceLabel = string(workspace.ID)
	}
	tabLabel := tab.Name
	if tabLabel == "" {
		tabLabel = string(tab.ID)
	}
	layer := pane.Kind
	if layer == "" {
		layer = types.PaneKindTiled
	}
	return compactSummaryLine(
		fmt.Sprintf("tab_path_bar: path=%s/%s/%s:%s", workspaceLabel, tabLabel, layer, pane.ID),
		fmt.Sprintf("target=%s", renderPaneTitle(state, pane)),
	)
}

func renderTabLayerBar(tab types.TabState) string {
	tiledRoot := "<none>"
	if paneIDs := orderedTiledPaneIDs(tab); len(paneIDs) > 0 {
		tiledRoot = string(paneIDs[0])
	}
	floatingTop := "<none>"
	if paneIDs := orderedFloatingPaneIDs(tab); len(paneIDs) > 0 {
		floatingTop = string(paneIDs[len(paneIDs)-1])
	}
	return compactSummaryLine(
		fmt.Sprintf("tab_layer_bar: tiled_root=%s", tiledRoot),
		fmt.Sprintf("floating_top=%s", floatingTop),
		fmt.Sprintf("floating_total=%d", len(orderedFloatingPaneIDs(tab))),
	)
}

func renderPaneBar(state types.AppState, pane types.PaneState) string {
	parts := []string{fmt.Sprintf("pane_bar: title=%s", renderPaneTitle(state, pane))}
	if role := renderTerminalRole(state.Domain.Connections[pane.TerminalID], pane.ID); role != "" {
		parts = append(parts, fmt.Sprintf("role=%s", role))
	} else if pane.TerminalID == "" {
		parts = append(parts, fmt.Sprintf("slot=%s", pane.SlotState))
	}
	parts = append(parts, fmt.Sprintf("kind=%s", pane.Kind))
	return compactSummaryLine(parts...)
}

func renderPaneTitle(state types.AppState, pane types.PaneState) string {
	if pane.TerminalID != "" {
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok {
			if terminal.Name != "" {
				return terminal.Name
			}
		}
		return string(pane.TerminalID)
	}
	switch pane.SlotState {
	case types.PaneSlotExited:
		return "exited pane"
	case types.PaneSlotWaiting:
		return "waiting pane"
	default:
		return "unconnected pane"
	}
}

func renderShortcutBar(state types.AppState, pane types.PaneState) string {
	parts := renderShortcutParts(state, pane)
	return compactSummaryLine(fmt.Sprintf("shortcut_bar: %s", strings.Join(parts, " | ")))
}

func renderFocusBar(state types.AppState, pane types.PaneState) string {
	target := renderFocusTarget(state, pane)
	layer := state.UI.Focus.Layer
	if layer == "" {
		layer = types.FocusLayerTiled
	}
	parts := []string{
		fmt.Sprintf("focus_bar: target=%s", target),
		fmt.Sprintf("layer=%s", layer),
	}
	if role := renderTerminalRole(state.Domain.Connections[pane.TerminalID], pane.ID); role != "" {
		parts = append(parts, fmt.Sprintf("role=%s", role))
	}
	return compactSummaryLine(parts...)
}

func renderFocusTarget(state types.AppState, pane types.PaneState) string {
	if pane.TerminalID != "" {
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok {
			if terminal.Name != "" {
				return terminal.Name
			}
		}
		return string(pane.TerminalID)
	}
	switch pane.SlotState {
	case types.PaneSlotExited:
		return "exited-pane"
	case types.PaneSlotWaiting:
		return "waiting-pane"
	case types.PaneSlotEmpty:
		return "empty-pane"
	default:
		return string(pane.ID)
	}
}

func renderShortcutParts(state types.AppState, pane types.PaneState) []string {
	switch state.UI.Overlay.Kind {
	case types.OverlayHelp:
		return []string{"Esc close", "? help"}
	case types.OverlayTerminalPicker, types.OverlayWorkspacePicker, types.OverlayLayoutResolve:
		return []string{"Enter confirm", "Esc close", "? help"}
	case types.OverlayTerminalManager:
		return []string{"Enter here", "t new-tab", "o float", "e edit", "k stop", "Esc close", "? help"}
	case types.OverlayPrompt:
		return []string{"Enter submit", "Esc close", "? help"}
	}
	if state.UI.Mode.Active == types.ModeFloating {
		return []string{"h/l focus", "j/k move", "H/J/K/L size", "[/] z", "c center", "x close", "Esc exit", "? help"}
	}
	switch pane.SlotState {
	case types.PaneSlotEmpty, types.PaneSlotWaiting:
		return []string{"n new", "a connect", "m manager", "x close", "? help"}
	case types.PaneSlotExited:
		return []string{"r restart", "a connect", "x close", "? help"}
	default:
		return []string{"Ctrl-p pane", "Ctrl-t tab", "Ctrl-w ws", "Ctrl-o float", "Ctrl-f pick", "Ctrl-g global", "? help"}
	}
}

// renderBodyBar 汇总 body 主体的 terminal/screen/overlay 状态，
// 让用户先看到“主体现在展示的是什么”，再往下看具体 section 细节。
func (r runtimeRenderer) renderBodyBar(state types.AppState, pane types.PaneState, overlayActive bool) string {
	return compactBarLine(
		fmt.Sprintf("body_bar: terminal=%s", r.renderBodyTerminalSummary(state, pane)),
		fmt.Sprintf("screen=%s", r.renderBodyScreenSummary(pane, overlayActive)),
		fmt.Sprintf("overlay=%s", state.UI.Overlay.Kind),
	)
}

func (r runtimeRenderer) renderBodyTerminalSummary(state types.AppState, pane types.PaneState) string {
	if pane.TerminalID == "" {
		return "disconnected"
	}
	if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.State != "" {
		return fmt.Sprintf("%s:%s", pane.TerminalID, terminal.State)
	}
	return string(pane.TerminalID)
}

func (r runtimeRenderer) renderBodyScreenSummary(pane types.PaneState, overlayActive bool) string {
	if pane.TerminalID == "" || r.Screens == nil {
		return "unavailable"
	}
	snapshot, ok := r.Screens.Snapshot(pane.TerminalID)
	if !ok || snapshot == nil {
		return "unavailable"
	}
	rows, totalRows, _ := renderSnapshotRows(snapshot)
	if overlayActive {
		return "suppressed"
	}
	return fmt.Sprintf("preview:%d/%d", len(rows), totalRows)
}

func renderStatusSection(workspace types.WorkspaceState, tab types.TabState, pane types.PaneState, ui types.UIState) []string {
	lines := []string{
		compactSummaryLine(
			fmt.Sprintf("workspace: %s", workspace.Name),
			fmt.Sprintf("tab: %s", tab.Name),
			fmt.Sprintf("pane: %s", pane.ID),
			fmt.Sprintf("slot: %s", pane.SlotState),
		),
		compactSummaryLine(
			fmt.Sprintf("tab_layer: %s", tab.ActiveLayer),
			fmt.Sprintf("overlay: %s", ui.Overlay.Kind),
			fmt.Sprintf("focus_layer: %s", ui.Focus.Layer),
			fmt.Sprintf("pane_kind: %s", pane.Kind),
		),
	}
	if ui.Focus.OverlayTarget != "" {
		lines = append(lines, fmt.Sprintf("focus_overlay_target: %s", ui.Focus.OverlayTarget))
	}
	if ui.Mode.Active != types.ModeNone {
		lines = append(lines, compactLine(
			fmt.Sprintf("mode: %s", ui.Mode.Active),
			fmt.Sprintf("mode_sticky: %t", ui.Mode.Sticky),
		))
	}
	if stack := renderFloatingStack(tab); stack != "" {
		lines = append(lines, fmt.Sprintf("floating_stack: %s", stack))
	}
	if pane.LastExitCode != nil {
		lines = append(lines, fmt.Sprintf("pane_exit_code: %d", *pane.LastExitCode))
	}
	if pane.Kind == types.PaneKindFloating {
		lines = append(lines, compactLine(
			fmt.Sprintf("pane_rect: x=%d", pane.Rect.X),
			fmt.Sprintf("y=%d", pane.Rect.Y),
			fmt.Sprintf("w=%d", pane.Rect.W),
			fmt.Sprintf("h=%d", pane.Rect.H),
		))
	}
	return lines
}

func renderFloatingStack(tab types.TabState) string {
	parts := make([]string, 0, len(tab.FloatingOrder))
	for _, paneID := range tab.FloatingOrder {
		pane, ok := tab.Panes[paneID]
		if !ok || pane.Kind != types.PaneKindFloating {
			continue
		}
		parts = append(parts, string(paneID))
	}
	return strings.Join(parts, " > ")
}

func renderTiledOutlineBar(tab types.TabState) []string {
	paneIDs := orderedTiledPaneIDs(tab)
	if len(paneIDs) <= 1 {
		return nil
	}
	return []string{compactSummaryLine(
		fmt.Sprintf("tiled_outline_bar: active=%s", tab.ActivePaneID),
		fmt.Sprintf("total=%d", len(paneIDs)),
	)}
}

func renderTiledLayout(tab types.TabState) []string {
	paneIDs := orderedTiledPaneIDs(tab)
	if len(paneIDs) <= 1 {
		return nil
	}
	summary := summarizeTiledLayout(tab.RootSplit, len(paneIDs))
	parts := []string{
		fmt.Sprintf("tiled_layout: root=%s", summary.Root),
		fmt.Sprintf("depth=%d", summary.Depth),
		fmt.Sprintf("leaves=%d", summary.Leaves),
	}
	if summary.HasRatio {
		parts = append(parts, fmt.Sprintf("ratio=%.2f", summary.Ratio))
	}
	return []string{compactSummaryLine(parts...)}
}

func renderFloatingOutlineBar(tab types.TabState) []string {
	paneIDs := orderedFloatingPaneIDs(tab)
	if len(paneIDs) == 0 {
		return nil
	}
	return []string{compactSummaryLine(
		fmt.Sprintf("floating_outline_bar: active=%s", tab.ActivePaneID),
		fmt.Sprintf("total=%d", len(paneIDs)),
		fmt.Sprintf("top=%s", paneIDs[len(paneIDs)-1]),
	)}
}

type tiledLayoutSummary struct {
	Root     string
	Depth    int
	Leaves   int
	Ratio    float64
	HasRatio bool
}

func (r runtimeRenderer) renderTiledTree(state types.AppState, tab types.TabState) []string {
	paneIDs := orderedTiledPaneIDs(tab)
	if len(paneIDs) <= 1 {
		return nil
	}
	lines := []string{"tiled_tree:"}
	if tab.RootSplit == nil {
		for i, paneID := range paneIDs {
			prefix := "|- "
			if i == len(paneIDs)-1 {
				prefix = "\\- "
			}
			lines = append(lines, prefix+r.renderTiledTreePaneLine(state, tab, paneID))
		}
		return lines
	}
	return append(lines, r.renderTiledTreeNode(state, tab, tab.RootSplit, "", true)...)
}

// renderTiledOutline 把当前 tab 下的 tiled pane 顺序稳定地投影成概览，
// 并补上每个 pane 的最小运行摘要，让 split 工作台在文本视图里也能读出结构和状态。
func (r runtimeRenderer) renderTiledOutline(state types.AppState, tab types.TabState) []string {
	paneIDs := orderedTiledPaneIDs(tab)
	if len(paneIDs) <= 1 {
		return nil
	}
	lines := []string{"tiled_outline:"}
	for _, paneID := range paneIDs {
		pane, ok := tab.Panes[paneID]
		if !ok || pane.Kind != types.PaneKindTiled {
			continue
		}
		prefix := "  "
		if paneID == tab.ActivePaneID {
			prefix = "> "
		}
		parts := []string{fmt.Sprintf("%s[tiled] %s", prefix, renderPaneTitle(state, pane))}
		parts = append(parts, renderPaneOverviewStateParts(state, pane)...)
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.State != "" {
			parts = append(parts, fmt.Sprintf("state=%s", terminal.State))
		}
		if preview := r.renderPanePreview(pane.TerminalID); preview != "" {
			parts = append(parts, fmt.Sprintf("preview=%s", preview))
		}
		lines = append(lines, compactSummaryLine(parts...))
	}
	return lines
}

func (r runtimeRenderer) renderFloatingOutline(state types.AppState, tab types.TabState) []string {
	paneIDs := orderedFloatingPaneIDs(tab)
	if len(paneIDs) == 0 {
		return nil
	}
	lines := []string{"floating_outline:"}
	for _, paneID := range paneIDs {
		pane, ok := tab.Panes[paneID]
		if !ok || pane.Kind != types.PaneKindFloating {
			continue
		}
		prefix := "  "
		if paneID == tab.ActivePaneID {
			prefix = "> "
		}
		parts := []string{fmt.Sprintf("%s[floating] %s", prefix, renderPaneTitle(state, pane))}
		parts = append(parts, renderPaneOverviewStateParts(state, pane)...)
		if pane.Rect.W > 0 || pane.Rect.H > 0 {
			parts = append(parts, fmt.Sprintf("rect=%d,%d %dx%d", pane.Rect.X, pane.Rect.Y, pane.Rect.W, pane.Rect.H))
		}
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.State != "" {
			parts = append(parts, fmt.Sprintf("state=%s", terminal.State))
		}
		if preview := r.renderPanePreview(pane.TerminalID); preview != "" {
			parts = append(parts, fmt.Sprintf("preview=%s", preview))
		}
		lines = append(lines, compactSummaryLine(parts...))
	}
	return lines
}

// renderTiledTreeNode 递归投影 split 树，优先把结构关系表达清楚，
// 这样文本视图也能读出“哪个 pane 在哪一支、当前 active 在哪里”。
func (r runtimeRenderer) renderTiledTreeNode(state types.AppState, tab types.TabState, node *types.SplitNode, prefix string, isLast bool) []string {
	if node == nil {
		return nil
	}
	branch := "|- "
	nextPrefix := prefix + "|  "
	if isLast {
		branch = "\\- "
		nextPrefix = prefix + "   "
	}
	if node.First == nil && node.Second == nil {
		return []string{prefix + branch + r.renderTiledTreePaneLine(state, tab, node.PaneID)}
	}

	line := fmt.Sprintf("split %s", node.Direction)
	if node.Ratio > 0 {
		line = fmt.Sprintf("%s ratio=%.2f", line, node.Ratio)
	}
	lines := []string{prefix + branch + line}
	children := []*types.SplitNode{node.First, node.Second}
	filtered := make([]*types.SplitNode, 0, len(children))
	for _, child := range children {
		if child != nil {
			filtered = append(filtered, child)
		}
	}
	for i, child := range filtered {
		lines = append(lines, r.renderTiledTreeNode(state, tab, child, nextPrefix, i == len(filtered)-1)...)
	}
	return lines
}

func (r runtimeRenderer) renderTiledTreePaneLine(state types.AppState, tab types.TabState, paneID types.PaneID) string {
	pane, ok := tab.Panes[paneID]
	if !ok {
		return fmt.Sprintf("[missing] %s", paneID)
	}
	parts := []string{fmt.Sprintf("[tiled] %s", renderPaneTitle(state, pane))}
	if paneID == tab.ActivePaneID {
		parts[0] = "> " + parts[0]
	}
	parts = append(parts, renderPaneOverviewStateParts(state, pane)...)
	if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.State != "" {
		parts = append(parts, fmt.Sprintf("state=%s", terminal.State))
	}
	if preview := r.renderPanePreview(pane.TerminalID); preview != "" {
		parts = append(parts, fmt.Sprintf("preview=%s", preview))
	}
	return compactSummaryLine(parts...)
}

// renderPaneOverviewStateParts 把 pane 在概览中的最小生命周期语义统一起来，
// 让 tiled/floating/tree 三种投影都能稳定表达 waiting/exited/empty 差异。
func renderPaneOverviewStateParts(state types.AppState, pane types.PaneState) []string {
	if pane.SlotState != "" && pane.SlotState != types.PaneSlotConnected {
		parts := []string{fmt.Sprintf("slot=%s", pane.SlotState)}
		switch pane.SlotState {
		case types.PaneSlotWaiting:
			parts = append(parts, "detail=layout pending")
		case types.PaneSlotEmpty:
			parts = append(parts, "detail=terminal missing")
		case types.PaneSlotExited:
			if pane.LastExitCode != nil {
				parts = append(parts, fmt.Sprintf("exit=%d", *pane.LastExitCode))
			} else if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.ExitCode != nil {
				parts = append(parts, fmt.Sprintf("exit=%d", *terminal.ExitCode))
			}
			parts = append(parts, "detail=history retained")
		}
		return parts
	}
	if role := renderTerminalRole(state.Domain.Connections[pane.TerminalID], pane.ID); role != "" {
		return []string{fmt.Sprintf("role=%s", role)}
	}
	parts := []string{fmt.Sprintf("slot=%s", pane.SlotState)}
	return parts
}

func summarizeTiledLayout(root *types.SplitNode, fallbackLeaves int) tiledLayoutSummary {
	if root == nil {
		return tiledLayoutSummary{
			Root:   "<implicit>",
			Depth:  1,
			Leaves: fallbackLeaves,
		}
	}
	depth, leaves := splitTreeMetrics(root)
	summary := tiledLayoutSummary{
		Root:   string(root.Direction),
		Depth:  depth,
		Leaves: leaves,
	}
	if summary.Root == "" {
		summary.Root = "<implicit>"
	}
	if root.First != nil || root.Second != nil {
		summary.Ratio = root.Ratio
		summary.HasRatio = true
	}
	if summary.Leaves < fallbackLeaves {
		summary.Root = "<implicit>"
		summary.Depth = 1
		summary.Leaves = fallbackLeaves
		summary.Ratio = 0
		summary.HasRatio = false
	}
	return summary
}

func splitTreeMetrics(node *types.SplitNode) (depth int, leaves int) {
	if node == nil {
		return 0, 0
	}
	if node.First == nil && node.Second == nil {
		return 1, 1
	}
	firstDepth, firstLeaves := splitTreeMetrics(node.First)
	secondDepth, secondLeaves := splitTreeMetrics(node.Second)
	maxDepth := firstDepth
	if secondDepth > maxDepth {
		maxDepth = secondDepth
	}
	return maxDepth + 1, firstLeaves + secondLeaves
}

func orderedTiledPaneIDs(tab types.TabState) []types.PaneID {
	if len(tab.Panes) == 0 {
		return nil
	}
	ordered := make([]types.PaneID, 0, len(tab.Panes))
	seen := map[types.PaneID]struct{}{}
	collectSplitPaneIDs(tab.RootSplit, tab, seen, &ordered)

	remaining := make([]types.PaneID, 0, len(tab.Panes))
	for paneID, pane := range tab.Panes {
		if pane.Kind != types.PaneKindTiled {
			continue
		}
		if _, ok := seen[paneID]; ok {
			continue
		}
		remaining = append(remaining, paneID)
	}
	sort.Slice(remaining, func(i, j int) bool {
		return remaining[i] < remaining[j]
	})
	return append(ordered, remaining...)
}

func orderedFloatingPaneIDs(tab types.TabState) []types.PaneID {
	ordered := make([]types.PaneID, 0, len(tab.FloatingOrder))
	seen := map[types.PaneID]struct{}{}
	for _, paneID := range tab.FloatingOrder {
		pane, ok := tab.Panes[paneID]
		if !ok || pane.Kind != types.PaneKindFloating {
			continue
		}
		if _, ok := seen[paneID]; ok {
			continue
		}
		seen[paneID] = struct{}{}
		ordered = append(ordered, paneID)
	}
	remaining := make([]types.PaneID, 0, len(tab.Panes))
	for paneID, pane := range tab.Panes {
		if pane.Kind != types.PaneKindFloating {
			continue
		}
		if _, ok := seen[paneID]; ok {
			continue
		}
		remaining = append(remaining, paneID)
	}
	sort.Slice(remaining, func(i, j int) bool {
		return remaining[i] < remaining[j]
	})
	return append(ordered, remaining...)
}

func collectSplitPaneIDs(node *types.SplitNode, tab types.TabState, seen map[types.PaneID]struct{}, ordered *[]types.PaneID) {
	if node == nil {
		return
	}
	if node.First == nil && node.Second == nil {
		pane, ok := tab.Panes[node.PaneID]
		if !ok || pane.Kind != types.PaneKindTiled {
			return
		}
		if _, ok := seen[node.PaneID]; ok {
			return
		}
		seen[node.PaneID] = struct{}{}
		*ordered = append(*ordered, node.PaneID)
		return
	}
	collectSplitPaneIDs(node.First, tab, seen, ordered)
	collectSplitPaneIDs(node.Second, tab, seen, ordered)
}

func (r runtimeRenderer) renderPanePreview(terminalID types.TerminalID) string {
	if terminalID == "" || r.Screens == nil {
		return ""
	}
	snapshot, ok := r.Screens.Snapshot(terminalID)
	if !ok || snapshot == nil {
		return ""
	}
	rows, _, _ := renderSnapshotRows(snapshot)
	for i := len(rows) - 1; i >= 0; i-- {
		line := strings.TrimSpace(rows[i])
		if line == "" || line == "<empty>" {
			continue
		}
		return truncateLine(line, runtimeOutlinePreviewMaxWidth)
	}
	if len(rows) == 0 {
		return ""
	}
	return truncateLine(rows[len(rows)-1], runtimeOutlinePreviewMaxWidth)
}

func (r runtimeRenderer) renderTerminalSection(state types.AppState, pane types.PaneState, compact bool) []string {
	if pane.TerminalID == "" {
		lines := []string{compactSummaryLine("terminal_bar: disconnected", "terminal: <disconnected>")}
		lines = append(lines, renderPaneSlotLines(pane)...)
		return lines
	}

	// section 首行需要同时保留 bar 和正文主语义，这里只做普通拼接，
	// 避免 bar 的裁剪策略误伤正文字段可见性。
	lines := []string{compactSummaryLine(renderTerminalBar(state, pane), fmt.Sprintf("terminal: %s", pane.TerminalID))}
	terminal, ok := state.Domain.Terminals[pane.TerminalID]
	if ok {
		label := terminal.Name
		if label == "" {
			label = string(terminal.ID)
		}
		lines[0] = compactSummaryLine(lines[0], fmt.Sprintf("title: %s", label))
		stateParts := make([]string, 0, 6)
		if terminal.State != "" {
			stateParts = append(stateParts, fmt.Sprintf("terminal_state: %s", terminal.State))
		}
		if terminal.ExitCode != nil {
			stateParts = append(stateParts, fmt.Sprintf("terminal_exit_code: %d", *terminal.ExitCode))
		}
		stateParts = append(stateParts, fmt.Sprintf("terminal_visibility: %t", terminal.Visible))
		stateParts = append(stateParts, renderConnectionLines(state.Domain.Connections[pane.TerminalID], pane.ID)...)
		if len(terminal.Command) > 0 {
			stateParts = append(stateParts, fmt.Sprintf("terminal_command: %s", strings.Join(terminal.Command, " ")))
		}
		perLine := 4
		if compact {
			perLine = len(stateParts)
			if perLine == 0 {
				perLine = 1
			}
		}
		lines = appendCompactParts(lines, perLine, stateParts)
		if !compact {
			if tags := renderTerminalTags(terminal.Tags); tags != "" {
				lines = append(lines, fmt.Sprintf("terminal_tags: %s", tags))
			}
		}
	} else {
		connectionParts := renderConnectionLines(state.Domain.Connections[pane.TerminalID], pane.ID)
		if len(connectionParts) > 0 {
			lines = append(lines, compactLine(connectionParts...))
		}
	}

	if r.Screens != nil {
		if status, ok := r.Screens.Status(pane.TerminalID); ok {
			lines = appendRuntimeStatusLines(lines, status)
		}
	}
	lines = append(lines, renderPaneSlotLines(pane)...)
	return lines
}

func renderPaneSlotLines(pane types.PaneState) []string {
	switch pane.SlotState {
	case types.PaneSlotEmpty:
		return []string{
			"pane_slot_detail: terminal removed or not connected",
			"pane_actions:",
			"  [n] start new terminal",
			"  [a] connect existing terminal",
			"  [m] open terminal manager",
			"  [x] close pane",
		}
	case types.PaneSlotWaiting:
		return []string{
			"pane_slot_detail: waiting for layout or restore resolution",
			"pane_actions:",
			"  [n] start new terminal",
			"  [a] connect existing terminal",
			"  [m] open terminal manager",
			"  [x] close pane",
		}
	case types.PaneSlotExited:
		lines := []string{
			"pane_slot_detail: terminal program exited",
			"pane_history: retained",
			"pane_actions:",
			"  [r] restart terminal",
			"  [a] connect another terminal",
			"  [x] close pane",
		}
		return lines
	default:
		return nil
	}
}

func (r runtimeRenderer) renderScreenSection(pane types.PaneState, compact bool) []string {
	if pane.TerminalID == "" || r.Screens == nil {
		return []string{compactSummaryLine("screen_bar: state=unavailable", "screen: <unavailable>")}
	}
	snapshot, ok := r.Screens.Snapshot(pane.TerminalID)
	if !ok || snapshot == nil {
		return []string{compactSummaryLine("screen_bar: state=unavailable", "screen: <unavailable>")}
	}
	rows, totalRows, truncated := renderSnapshotRows(snapshot)
	meta := []string{fmt.Sprintf("screen_rows: %d/%d", len(rows), totalRows)}
	if truncated {
		meta = append(meta, "screen_truncated: true")
	}
	if compact {
		return []string{
			compactSummaryLine(
				fmt.Sprintf("screen_bar: state=suppressed | rows=%d/%d", len(rows), totalRows),
				compactLine(append([]string{"screen: <suppressed by overlay>"}, meta...)...),
			),
		}
	}
	lines := []string{
		compactSummaryLine(
			fmt.Sprintf("screen_bar: state=preview | rows=%d/%d", len(rows), totalRows),
			compactLine(append([]string{"screen:"}, meta...)...),
		),
	}
	return append(lines, rows...)
}

func renderTerminalBar(state types.AppState, pane types.PaneState) string {
	if pane.TerminalID == "" {
		return "terminal_bar: disconnected"
	}
	parts := []string{fmt.Sprintf("terminal_bar: id=%s", pane.TerminalID)}
	if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok {
		label := terminal.Name
		if label == "" {
			label = string(terminal.ID)
		}
		parts = append(parts, fmt.Sprintf("title=%s", label))
		if terminal.State != "" {
			parts = append(parts, fmt.Sprintf("state=%s", terminal.State))
		}
	}
	if role := renderTerminalRole(state.Domain.Connections[pane.TerminalID], pane.ID); role != "" {
		parts = append(parts, fmt.Sprintf("role=%s", role))
	}
	return compactBarLine(parts...)
}

func renderTerminalRole(conn types.ConnectionState, paneID types.PaneID) string {
	if conn.TerminalID == "" || !containsPaneID(conn.ConnectedPaneIDs, paneID) {
		return ""
	}
	if conn.OwnerPaneID == paneID {
		return "owner"
	}
	return "follower"
}

func compactLine(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, " | ")
}

func compactBarLine(parts ...string) string {
	return truncateLine(compactLine(parts...), runtimeBarMaxWidth)
}

// compactSummaryLine 用于 section/status 这类“摘要 + 正文首行”。
// 这里给比 bar 更宽的预算，既避免长字段把视图横向撑爆，也尽量保留更多正文语义。
func compactSummaryLine(parts ...string) string {
	return truncateLine(compactLine(parts...), runtimeSummaryMaxWidth)
}

// compactDetailLine 用于 overlay/detail 元数据行。
// 这些行需要比 bar 更宽的预算，但也不能让超长 command/tag/hint 把主视图横向撑爆。
func compactDetailLine(parts ...string) string {
	return truncateLine(compactLine(parts...), runtimeDetailMaxWidth)
}

func truncateLine(line string, maxWidth int) string {
	if maxWidth <= 0 || len(line) <= maxWidth {
		return line
	}
	if maxWidth <= 3 {
		return line[:maxWidth]
	}
	// 语义栏位通常前后都带关键上下文，中间裁剪能同时保留头部主语与尾部状态。
	visible := maxWidth - 3
	head := visible / 2
	tail := visible - head
	return line[:head] + "..." + line[len(line)-tail:]
}

func appendCompactParts(lines []string, perLine int, parts []string) []string {
	if perLine <= 0 {
		perLine = len(parts)
	}
	current := make([]string, 0, perLine)
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		current = append(current, part)
		if len(current) == perLine {
			lines = append(lines, compactLine(current...))
			current = current[:0]
		}
	}
	if len(current) > 0 {
		lines = append(lines, compactLine(current...))
	}
	return lines
}

func renderConnectionLines(conn types.ConnectionState, paneID types.PaneID) []string {
	if conn.TerminalID == "" || !containsPaneID(conn.ConnectedPaneIDs, paneID) {
		return nil
	}
	role := "follower"
	if conn.OwnerPaneID == paneID {
		role = "owner"
	}
	return []string{
		fmt.Sprintf("connection_role: %s", role),
		fmt.Sprintf("connected_panes: %d", len(conn.ConnectedPaneIDs)),
	}
}

func containsPaneID(ids []types.PaneID, target types.PaneID) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func renderTerminalTags(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", key, tags[key]))
	}
	return strings.Join(parts, ",")
}

func appendRuntimeStatusLines(lines []string, status RuntimeTerminalStatus) []string {
	lines = appendCompactParts(lines, 3, []string{
		runtimeStatePart(status),
		runtimeExitCodePart(status),
		runtimeSizePart(status),
	})
	lines = appendCompactParts(lines, 2, []string{
		runtimeAccessPart(status),
		runtimeSyncLostPart(status),
	})
	lines = appendCompactParts(lines, 1, []string{
		runtimeRemovedPart(status),
		runtimeReadErrorPart(status),
	})
	return lines
}

func runtimeStatePart(status RuntimeTerminalStatus) string {
	if status.State == "" {
		return ""
	}
	return fmt.Sprintf("runtime_state: %s", status.State)
}

func runtimeExitCodePart(status RuntimeTerminalStatus) string {
	if !status.Closed || status.ExitCode == nil {
		return ""
	}
	return fmt.Sprintf("runtime_exit_code: %d", *status.ExitCode)
}

func runtimeSizePart(status RuntimeTerminalStatus) string {
	if status.Size.Cols == 0 && status.Size.Rows == 0 {
		return ""
	}
	return fmt.Sprintf("runtime_size: %dx%d", status.Size.Cols, status.Size.Rows)
}

func runtimeAccessPart(status RuntimeTerminalStatus) string {
	if !status.ObserverOnly {
		return ""
	}
	return "runtime_access: observer_only"
}

func runtimeSyncLostPart(status RuntimeTerminalStatus) string {
	if !status.SyncLost {
		return ""
	}
	return fmt.Sprintf("runtime_sync_lost: %d", status.SyncLostDroppedBytes)
}

func runtimeRemovedPart(status RuntimeTerminalStatus) string {
	if status.RemovedReason == "" {
		return ""
	}
	return fmt.Sprintf("runtime_removed: %s", status.RemovedReason)
}

func runtimeReadErrorPart(status RuntimeTerminalStatus) string {
	if status.ReadError == "" {
		return ""
	}
	return fmt.Sprintf("runtime_read_error: %s", status.ReadError)
}

func renderSnapshotRows(snapshot *protocol.Snapshot) ([]string, int, bool) {
	if snapshot == nil || len(snapshot.Screen.Cells) == 0 {
		return []string{"<empty>"}, 0, false
	}
	start := 0
	if len(snapshot.Screen.Cells) > runtimeScreenPreviewRows {
		start = len(snapshot.Screen.Cells) - runtimeScreenPreviewRows
	}
	lines := make([]string, 0, len(snapshot.Screen.Cells)-start)
	for _, row := range snapshot.Screen.Cells[start:] {
		lines = append(lines, renderSnapshotRow(row))
	}
	return lines, len(snapshot.Screen.Cells), start > 0
}

func renderSnapshotRow(row []protocol.Cell) string {
	if len(row) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, cell := range row {
		builder.WriteString(cell.Content)
	}
	return strings.TrimRight(builder.String(), " ")
}

func renderOverlayLines(overlay types.OverlayState, focus types.FocusState) []string {
	bar := renderOverlayBar(overlay.Kind, focus.Layer)
	switch overlay.Kind {
	case types.OverlayWorkspacePicker:
		picker, ok := overlay.Data.(*workspacedomain.PickerState)
		if !ok || picker == nil {
			return mergeSectionBar(bar, []string{fmt.Sprintf("overlay: %s", overlay.Kind)})
		}
		return mergeSectionBar(bar, renderWorkspacePickerLines(picker))
	case types.OverlayHelp:
		return mergeSectionBar(bar, renderHelpLines(overlay.ReturnFocus))
	case types.OverlayTerminalManager:
		manager, ok := overlay.Data.(*terminalmanagerdomain.State)
		if !ok || manager == nil {
			return mergeSectionBar(bar, []string{fmt.Sprintf("overlay: %s", overlay.Kind)})
		}
		return mergeSectionBar(bar, renderTerminalManagerLines(manager))
	case types.OverlayTerminalPicker:
		picker, ok := overlay.Data.(*terminalpickerdomain.State)
		if !ok || picker == nil {
			return mergeSectionBar(bar, []string{fmt.Sprintf("overlay: %s", overlay.Kind)})
		}
		return mergeSectionBar(bar, renderTerminalPickerLines(picker))
	case types.OverlayLayoutResolve:
		resolve, ok := overlay.Data.(*layoutresolvedomain.State)
		if !ok || resolve == nil {
			return mergeSectionBar(bar, []string{fmt.Sprintf("overlay: %s", overlay.Kind)})
		}
		return mergeSectionBar(bar, renderLayoutResolveLines(resolve))
	case types.OverlayPrompt:
		prompt, ok := overlay.Data.(*promptdomain.State)
		if !ok || prompt == nil {
			return mergeSectionBar(bar, []string{fmt.Sprintf("overlay: %s", overlay.Kind)})
		}
		return mergeSectionBar(bar, renderPromptLines(prompt))
	default:
		return mergeSectionBar(bar, []string{fmt.Sprintf("overlay: %s", overlay.Kind)})
	}
}

func renderHelpLines(returnFocus types.FocusState) []string {
	layer := returnFocus.Layer
	if layer == "" {
		layer = types.FocusLayerTiled
	}
	paneID := returnFocus.PaneID
	if paneID == "" {
		paneID = types.PaneID("none")
	}
	return []string{
		compactLine(
			fmt.Sprintf("help_bar: layer=%s", layer),
			fmt.Sprintf("pane=%s", paneID),
		),
		"help_most_used: Ctrl-p pane | Ctrl-t tab | Ctrl-w workspace | Ctrl-f picker | Ctrl-o floating | Ctrl-g global",
		"help_concepts: pane = work slot | terminal = running entity",
		"help_shared: owner controls terminal-level operations | follower observes without control",
		"help_exit: close pane != stop terminal != detach TUI",
	}
}

func renderOverlayBar(kind types.OverlayKind, focusLayer types.FocusLayer) string {
	return compactBarLine(
		fmt.Sprintf("overlay_bar: kind=%s", kind),
		fmt.Sprintf("focus=%s", focusLayer),
	)
}

func mergeSectionBar(bar string, body []string) []string {
	if len(body) == 0 {
		return []string{bar}
	}
	lines := append([]string{}, body...)
	lines[0] = compactSummaryLine(bar, lines[0])
	return lines
}

func renderWorkspacePickerLines(picker *workspacedomain.PickerState) []string {
	rows := picker.VisibleRows()
	lines := []string{compactLine(
		renderWorkspacePickerBar(picker),
		compactLine(
			fmt.Sprintf("workspace_picker_query: %s", picker.Query()),
			fmt.Sprintf("workspace_picker_row_count: %d", len(rows)),
		),
	)}
	if node, ok := picker.SelectedNode(); ok {
		lines = append(lines, compactLine(
			fmt.Sprintf("workspace_picker_selected: %s", node.Key),
			fmt.Sprintf("workspace_picker_selected_kind: %s", node.Kind),
			fmt.Sprintf("workspace_picker_selected_label: %s", node.Label),
		))
	}
	if row, ok := picker.SelectedRow(); ok {
		lines = append(lines, compactLine(
			fmt.Sprintf("workspace_picker_selected_depth: %d", row.Depth),
			fmt.Sprintf("workspace_picker_selected_expanded: %t", row.Expanded),
			fmt.Sprintf("workspace_picker_selected_match: %t", row.Match),
		))
	}
	selected, hasSelection := picker.SelectedRow()
	selectedIndex := 0
	if hasSelection {
		for idx, row := range rows {
			if row.Node.Key == selected.Node.Key {
				selectedIndex = idx
				break
			}
		}
	}
	previewRows, truncated := overlayPreviewRowsAround(rows, runtimeOverlayPreviewRows, selectedIndex)
	rowMeta := []string{"workspace_picker_rows:", fmt.Sprintf("workspace_picker_rows_rendered: %d", len(previewRows))}
	if truncated {
		rowMeta = append(rowMeta, "workspace_picker_rows_truncated: true")
	}
	lines = append(lines, compactLine(rowMeta...))
	for _, row := range previewRows {
		prefix := "  "
		if hasSelection && row.Node.Key == selected.Node.Key {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%s[%s] %s", prefix, strings.Repeat("  ", row.Depth), row.Node.Kind, row.Node.Label))
	}
	return lines
}

func renderWorkspacePickerBar(picker *workspacedomain.PickerState) string {
	if node, ok := picker.SelectedNode(); ok {
		parts := []string{
			fmt.Sprintf("workspace_picker_bar: selected=%s", node.Key),
			fmt.Sprintf("kind=%s", node.Kind),
		}
		if row, ok := picker.SelectedRow(); ok {
			parts = append(parts, fmt.Sprintf("depth=%d", row.Depth))
		}
		return compactBarLine(parts...)
	}
	return "workspace_picker_bar: none"
}

func renderTerminalManagerLines(manager *terminalmanagerdomain.State) []string {
	rows := manager.VisibleRows()
	lines := []string{compactLine(
		renderTerminalManagerBar(manager),
		compactLine(
			fmt.Sprintf("terminal_manager_query: %s", manager.Query()),
			fmt.Sprintf("terminal_manager_row_count: %d", len(rows)),
		),
	)}
	if row, ok := manager.SelectedRow(); ok && row.Kind == terminalmanagerdomain.RowKindTerminal {
		selectedTags := ""
		if tags := renderTerminalTags(row.Tags); tags != "" {
			selectedTags = fmt.Sprintf("terminal_manager_selected_tags: %s", tags)
		}
		lines = append(lines,
			compactLine(
				fmt.Sprintf("terminal_manager_selected: %s", row.TerminalID),
				fmt.Sprintf("terminal_manager_selected_label: %s", row.Label),
				fmt.Sprintf("terminal_manager_selected_kind: %s", row.Kind),
				fmt.Sprintf("terminal_manager_selected_section: %s", row.Section),
			),
			compactLine(
				fmt.Sprintf("terminal_manager_selected_state: %s", row.State),
				fmt.Sprintf("terminal_manager_selected_visible: %t", row.Visible),
				fmt.Sprintf("terminal_manager_selected_visibility: %s", row.VisibilityLabel),
				fmt.Sprintf("terminal_manager_selected_connected_panes: %d", row.ConnectedPaneCount),
				fmt.Sprintf("terminal_manager_selected_location_count: %d", row.LocationCount),
			),
			compactDetailLine(
				fmt.Sprintf("terminal_manager_selected_command: %s", row.Command),
				fmt.Sprintf("terminal_manager_selected_owner: %s", row.OwnerSlotLabel),
				selectedTags,
			),
		)
	} else if terminalID, ok := manager.SelectedTerminalID(); ok {
		lines = append(lines, fmt.Sprintf("terminal_manager_selected: %s", terminalID))
	}
	selected, hasSelection := manager.SelectedRow()
	selectedIndex := 0
	if hasSelection {
		for idx, row := range rows {
			if row.Kind == selected.Kind && row.TerminalID == selected.TerminalID && row.Label == selected.Label {
				selectedIndex = idx
				break
			}
		}
	}
	previewRows, truncated := overlayPreviewRowsAround(rows, runtimeTerminalManagerPreviewRows, selectedIndex)
	rowMeta := []string{"terminal_manager_rows:", fmt.Sprintf("terminal_manager_rows_rendered: %d", len(previewRows))}
	if truncated {
		rowMeta = append(rowMeta, "terminal_manager_rows_truncated: true")
	}
	lines = append(lines, compactLine(rowMeta...))
	for _, row := range previewRows {
		prefix := "  "
		if hasSelection && row.Kind != terminalmanagerdomain.RowKindHeader && row.Kind == selected.Kind && row.TerminalID == selected.TerminalID && row.Label == selected.Label {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s[%s] %s", prefix, row.Kind, row.Label))
	}
	if detail, ok := manager.SelectedDetail(); ok {
		detailTags := ""
		if tags := renderDetailTags(detail.Tags); tags != "" {
			detailTags = fmt.Sprintf("detail_tags: %s", tags)
		}
		lines = append(lines,
			compactLine(
				fmt.Sprintf("terminal_manager_detail: %s", detail.Name),
				fmt.Sprintf("detail_terminal: %s", detail.TerminalID),
				fmt.Sprintf("detail_state: %s", detail.State),
				fmt.Sprintf("detail_visible: %t", detail.Visible),
				fmt.Sprintf("detail_visibility: %s", detail.VisibilityLabel),
			),
			compactDetailLine(
				fmt.Sprintf("detail_connected_panes: %d", detail.ConnectedPaneCount),
				fmt.Sprintf("detail_location_count: %d", len(detail.Locations)),
				fmt.Sprintf("detail_command: %s", detail.Command),
			),
			compactDetailLine(fmt.Sprintf("detail_owner: %s", detail.OwnerSlotLabel), detailTags),
		)
		if locations := renderDetailLocations(detail.Locations); len(locations) > 0 {
			previewLocations, truncated := overlayPreviewStrings(locations, runtimeOverlayDetailPreviewRows)
			meta := []string{"detail_locations:", fmt.Sprintf("detail_locations_rendered: %d", len(previewLocations))}
			if truncated {
				meta = append(meta, "detail_locations_truncated: true")
			}
			lines = append(lines, compactDetailLine(meta...))
			lines = append(lines, previewLocations...)
		}
		actionRows := terminalmanagerdomain.ActionRows()
		lines = append(lines, compactLine(
			"terminal_manager_actions:",
			fmt.Sprintf("terminal_manager_actions_rendered: %d", len(actionRows)),
		))
		for _, action := range actionRows {
			lines = append(lines, fmt.Sprintf("  [%s] %s", action.ID, action.Label))
		}
	}
	return lines
}

func renderTerminalManagerBar(manager *terminalmanagerdomain.State) string {
	if row, ok := manager.SelectedRow(); ok {
		selected := row.Label
		if row.TerminalID != "" {
			selected = string(row.TerminalID)
		}
		parts := []string{fmt.Sprintf("terminal_manager_bar: selected=%s", selected)}
		if row.Section != "" {
			parts = append(parts, fmt.Sprintf("section=%s", row.Section))
		}
		parts = append(parts, fmt.Sprintf("kind=%s", row.Kind))
		return compactBarLine(parts...)
	} else if terminalID, ok := manager.SelectedTerminalID(); ok {
		return compactBarLine(fmt.Sprintf("terminal_manager_bar: selected=%s", terminalID))
	}
	return "terminal_manager_bar: none"
}

func renderDetailTags(tags []terminalmanagerdomain.Tag) string {
	if len(tags) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tags))
	for _, tag := range tags {
		parts = append(parts, fmt.Sprintf("%s=%s", tag.Key, tag.Value))
	}
	return strings.Join(parts, ",")
}

func renderDetailLocations(locations []terminalmanagerdomain.Location) []string {
	if len(locations) == 0 {
		return nil
	}
	lines := make([]string, 0, len(locations))
	for _, location := range locations {
		lines = append(lines, fmt.Sprintf("  [location] %s/%s/%s", location.WorkspaceName, location.TabName, location.SlotLabel))
	}
	return lines
}

func renderPromptLines(prompt *promptdomain.State) []string {
	lines := []string{compactLine(
		renderPromptBar(prompt),
		compactLine(
			fmt.Sprintf("prompt_title: %s", prompt.Title),
			fmt.Sprintf("prompt_kind: %s", prompt.Kind),
		),
	)}
	if prompt.TerminalID != "" {
		lines = append(lines, fmt.Sprintf("prompt_terminal: %s", prompt.TerminalID))
	}
	if len(prompt.Fields) == 0 {
		lines = append(lines,
			compactDetailLine("prompt_active_field: draft", "prompt_active_label: draft", fmt.Sprintf("prompt_active_value: %s", prompt.Draft)),
			compactLine("prompt_active_index: 0", "prompt_field_count: 0"),
			"prompt_fields: | prompt_fields_rendered: 1",
			fmt.Sprintf("> [draft] %s", prompt.Draft),
		)
		return append(lines, renderPromptActionLines()...)
	}
	active := prompt.Active
	if active < 0 || active >= len(prompt.Fields) {
		active = 0
	}
	lines = append(lines,
		compactDetailLine(
			fmt.Sprintf("prompt_active_field: %s", prompt.Fields[active].Key),
			fmt.Sprintf("prompt_active_label: %s", prompt.Fields[active].Label),
			fmt.Sprintf("prompt_active_value: %s", prompt.Fields[active].Value),
		),
		compactLine(
			fmt.Sprintf("prompt_active_index: %d", active),
			fmt.Sprintf("prompt_field_count: %d", len(prompt.Fields)),
		),
	)
	previewFields, truncated := overlayPreviewRowsAround(prompt.Fields, runtimeOverlayDetailPreviewRows, active)
	fieldMeta := []string{"prompt_fields:", fmt.Sprintf("prompt_fields_rendered: %d", len(previewFields))}
	if truncated {
		fieldMeta = append(fieldMeta, "prompt_fields_truncated: true")
	}
	lines = append(lines, compactLine(fieldMeta...))
	for _, field := range previewFields {
		prefix := "  "
		if field.Key == prompt.Fields[active].Key && field.Label == prompt.Fields[active].Label {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s[%s] %s: %s", prefix, field.Key, field.Label, field.Value))
	}
	return append(lines, renderPromptActionLines()...)
}

func renderPromptActionLines() []string {
	actionRows := promptdomain.ActionRows()
	lines := []string{compactLine(
		"prompt_actions:",
		fmt.Sprintf("prompt_actions_rendered: %d", len(actionRows)),
	)}
	for _, action := range actionRows {
		lines = append(lines, fmt.Sprintf("  [%s] %s", action.ID, action.Label))
	}
	return lines
}

func renderPromptBar(prompt *promptdomain.State) string {
	activeField := "draft"
	if len(prompt.Fields) > 0 {
		active := prompt.Active
		if active < 0 || active >= len(prompt.Fields) {
			active = 0
		}
		activeField = prompt.Fields[active].Key
	}
	parts := []string{fmt.Sprintf("prompt_bar: kind=%s", prompt.Kind)}
	if prompt.TerminalID != "" {
		parts = append(parts, fmt.Sprintf("terminal=%s", prompt.TerminalID))
	}
	parts = append(parts, fmt.Sprintf("active=%s", activeField))
	return compactBarLine(parts...)
}

func renderTerminalPickerLines(picker *terminalpickerdomain.State) []string {
	rows := picker.VisibleRows()
	lines := []string{compactLine(
		renderTerminalPickerBar(picker),
		compactLine(
			fmt.Sprintf("terminal_picker_query: %s", picker.Query()),
			fmt.Sprintf("terminal_picker_row_count: %d", len(rows)),
		),
	)}
	if row, ok := picker.SelectedRow(); ok && row.Kind == terminalpickerdomain.RowKindTerminal {
		lines = append(lines,
			compactLine(
				fmt.Sprintf("terminal_picker_selected: %s", row.TerminalID),
				fmt.Sprintf("terminal_picker_selected_label: %s", row.Label),
				fmt.Sprintf("terminal_picker_selected_kind: %s", row.Kind),
			),
			compactDetailLine(
				fmt.Sprintf("terminal_picker_selected_state: %s", row.State),
				fmt.Sprintf("terminal_picker_selected_command: %s", row.Command),
			),
			compactLine(
				fmt.Sprintf("terminal_picker_selected_visible: %t", row.Visible),
				fmt.Sprintf("terminal_picker_selected_connected_panes: %d", row.ConnectedPaneCount),
			),
		)
		if tags := renderTerminalTags(row.Tags); tags != "" {
			lines = append(lines, fmt.Sprintf("terminal_picker_selected_tags: %s", tags))
		}
	} else if terminalID, ok := picker.SelectedTerminalID(); ok {
		lines = append(lines, fmt.Sprintf("terminal_picker_selected: %s", terminalID))
	}
	selected, hasSelection := picker.SelectedRow()
	selectedIndex := 0
	if hasSelection {
		for idx, row := range rows {
			if row.Kind == selected.Kind && row.TerminalID == selected.TerminalID && row.Label == selected.Label {
				selectedIndex = idx
				break
			}
		}
	}
	previewRows, truncated := overlayPreviewRowsAround(rows, runtimeOverlayPreviewRows, selectedIndex)
	rowMeta := []string{"terminal_picker_rows:", fmt.Sprintf("terminal_picker_rows_rendered: %d", len(previewRows))}
	if truncated {
		rowMeta = append(rowMeta, "terminal_picker_rows_truncated: true")
	}
	lines = append(lines, compactLine(rowMeta...))
	for _, row := range previewRows {
		prefix := "  "
		if hasSelection && row.Kind == selected.Kind && row.TerminalID == selected.TerminalID && row.Label == selected.Label {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s[%s] %s", prefix, row.Kind, row.Label))
	}
	return lines
}

func renderTerminalPickerBar(picker *terminalpickerdomain.State) string {
	parts := []string{fmt.Sprintf("terminal_picker_bar: query=%s", picker.Query())}
	if row, ok := picker.SelectedRow(); ok {
		if row.TerminalID != "" {
			parts = append(parts, fmt.Sprintf("selected=%s", row.TerminalID))
		} else {
			parts = append(parts, fmt.Sprintf("selected=%s", row.Label))
		}
		parts = append(parts, fmt.Sprintf("kind=%s", row.Kind))
	} else if terminalID, ok := picker.SelectedTerminalID(); ok {
		parts = append(parts, fmt.Sprintf("selected=%s", terminalID))
	}
	return compactBarLine(parts...)
}

func renderLayoutResolveLines(resolve *layoutresolvedomain.State) []string {
	rows := resolve.Rows()
	lines := []string{
		compactLine(
			renderLayoutResolveBar(resolve),
			fmt.Sprintf("layout_resolve_pane: %s", resolve.PaneID),
			fmt.Sprintf("layout_resolve_role: %s", resolve.Role),
		),
		compactDetailLine(
			fmt.Sprintf("layout_resolve_hint: %s", resolve.Hint),
			fmt.Sprintf("layout_resolve_row_count: %d", len(rows)),
		),
	}
	if row, ok := resolve.SelectedRow(); ok {
		lines = append(lines, compactLine(
			fmt.Sprintf("layout_resolve_selected: %s", row.Action),
			fmt.Sprintf("layout_resolve_selected_label: %s", row.Label),
		))
	}
	selected, hasSelection := resolve.SelectedRow()
	selectedIndex := 0
	if hasSelection {
		for idx, row := range rows {
			if row.Action == selected.Action && row.Label == selected.Label {
				selectedIndex = idx
				break
			}
		}
	}
	previewRows, truncated := overlayPreviewRowsAround(rows, runtimeOverlayPreviewRows, selectedIndex)
	rowMeta := []string{"layout_resolve_rows:", fmt.Sprintf("layout_resolve_rows_rendered: %d", len(previewRows))}
	if truncated {
		rowMeta = append(rowMeta, "layout_resolve_rows_truncated: true")
	}
	lines = append(lines, compactLine(rowMeta...))
	for _, row := range previewRows {
		prefix := "  "
		if hasSelection && row.Action == selected.Action && row.Label == selected.Label {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s[%s] %s", prefix, row.Action, row.Label))
	}
	return lines
}

func renderLayoutResolveBar(resolve *layoutresolvedomain.State) string {
	parts := []string{
		fmt.Sprintf("layout_resolve_bar: pane=%s", resolve.PaneID),
		fmt.Sprintf("role=%s", resolve.Role),
	}
	if row, ok := resolve.SelectedRow(); ok {
		parts = append(parts, fmt.Sprintf("selected=%s", row.Action))
	}
	return compactBarLine(parts...)
}

func renderNoticeLines(notices []btui.Notice) []string {
	total := countVisibleNotices(notices)
	if total == 0 {
		return []string{"notice_bar: total=0 | showing=0 | notices: 0"}
	}
	visible := visibleNotices(notices)
	start := 0
	if len(visible) > runtimeOverlayDetailPreviewRows {
		start = len(visible) - runtimeOverlayDetailPreviewRows
	}
	previewNotices := visible[start:]
	truncated := start > 0
	lines := []string{renderNoticeBar(visible, previewNotices)}
	if groupBar := renderNoticeGroupBar(visible); groupBar != "" {
		lines = append(lines, groupBar)
	}
	meta := []string{fmt.Sprintf("notices_rendered: %d", len(previewNotices))}
	if truncated {
		meta = append(meta, "notices_truncated: true")
	}
	lines = append(lines, compactLine(meta...))
	for _, notice := range previewNotices {
		line := fmt.Sprintf("[%s] %s", notice.Level, notice.Text)
		if notice.Count > 1 {
			line = fmt.Sprintf("%s (x%d)", line, notice.Count)
		}
		lines = append(lines, line)
	}
	return lines
}

func countVisibleNotices(notices []btui.Notice) int {
	return len(visibleNotices(notices))
}

func lastVisibleNotice(notices []btui.Notice) (btui.Notice, bool) {
	visible := visibleNotices(notices)
	if len(visible) == 0 {
		return btui.Notice{}, false
	}
	return visible[len(visible)-1], true
}

func visibleNotices(notices []btui.Notice) []btui.Notice {
	visible := make([]btui.Notice, 0, len(notices))
	for _, notice := range notices {
		if strings.TrimSpace(notice.Text) == "" {
			continue
		}
		visible = append(visible, notice)
	}
	return visible
}

func renderNoticeBar(visible []btui.Notice, preview []btui.Notice) string {
	parts := []string{
		fmt.Sprintf("notice_bar: total=%d", len(visible)),
		fmt.Sprintf("showing=%d", len(preview)),
	}
	if len(visible) > 0 {
		parts = append(parts, fmt.Sprintf("last=%s", visible[len(visible)-1].Level))
	}
	parts = append(parts, "notices:")
	return compactBarLine(parts...)
}

func renderNoticeGroupBar(visible []btui.Notice) string {
	errorCount := 0
	infoCount := 0
	for _, notice := range visible {
		switch notice.Level {
		case btui.NoticeLevelError:
			errorCount++
		case btui.NoticeLevelInfo:
			infoCount++
		}
	}
	parts := make([]string, 0, 2)
	if errorCount > 0 {
		parts = append(parts, fmt.Sprintf("error=%d", errorCount))
	}
	if infoCount > 0 {
		parts = append(parts, fmt.Sprintf("info=%d", infoCount))
	}
	if len(parts) == 0 {
		return ""
	}
	parts[0] = fmt.Sprintf("notice_group_bar: %s", parts[0])
	return compactBarLine(parts...)
}

func overlayPreviewRowsAround[T any](rows []T, limit int, focusIndex int) ([]T, bool) {
	if limit <= 0 || len(rows) <= limit {
		return rows, false
	}
	if focusIndex < 0 {
		focusIndex = 0
	}
	if focusIndex >= len(rows) {
		focusIndex = len(rows) - 1
	}
	start := focusIndex - limit + 1
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > len(rows) {
		end = len(rows)
		start = end - limit
	}
	return rows[start:end], true
}

func overlayPreviewStrings(rows []string, limit int) ([]string, bool) {
	if limit <= 0 || len(rows) <= limit {
		return rows, false
	}
	return rows[:limit], true
}
