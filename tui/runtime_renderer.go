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
	lines = appendChrome(lines, "header", []string{renderHeaderBar(workspace, tab, pane, state.UI)}, func(lines []string) []string {
		return appendSection(lines, "status", statusLines)
	})
	lines = appendChrome(lines, "body", []string{r.renderBodyBar(state, pane, overlayActive)}, func(lines []string) []string {
		lines = appendSection(lines, "terminal", r.renderTerminalSection(state, pane, overlayActive))
		lines = appendSection(lines, "screen", r.renderScreenSection(pane, overlayActive))
		return appendSection(lines, "overlay", renderOverlayLines(state.UI.Overlay, state.UI.Focus))
	})
	lines = appendChrome(lines, "footer", []string{renderFooterBar(notices, state.UI.Overlay.Kind)}, func(lines []string) []string {
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
	if pane.LastExitCode != nil {
		lines = append(lines, fmt.Sprintf("pane_exit_code: %d", *pane.LastExitCode))
	}
	return lines
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
