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
	summaryLines := []string{
		fmt.Sprintf("summary: ws=%s tab=%s pane=%s overlay=%s focus=%s", workspace.Name, tab.Name, pane.ID, state.UI.Overlay.Kind, state.UI.Focus.Layer),
	}
	overlayActive := state.UI.Overlay.Kind != types.OverlayNone

	lines := []string{"termx"}
	lines = appendChrome(lines, "header", summaryLines, func(lines []string) []string {
		return appendSection(lines, "status", statusLines)
	})
	lines = appendChrome(lines, "body", nil, func(lines []string) []string {
		lines = appendSection(lines, "terminal", r.renderTerminalSection(state, pane, overlayActive))
		lines = appendSection(lines, "screen", r.renderScreenSection(pane, overlayActive))
		return appendSection(lines, "overlay", renderOverlayLines(state.UI.Overlay))
	})
	lines = appendChrome(lines, "footer", nil, func(lines []string) []string {
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

func renderStatusSection(workspace types.WorkspaceState, tab types.TabState, pane types.PaneState, ui types.UIState) []string {
	lines := []string{
		compactLine(
			fmt.Sprintf("workspace: %s", workspace.Name),
			fmt.Sprintf("tab: %s", tab.Name),
			fmt.Sprintf("pane: %s", pane.ID),
			fmt.Sprintf("slot: %s", pane.SlotState),
		),
		compactLine(
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
		return []string{"terminal: <disconnected>"}
	}

	lines := []string{fmt.Sprintf("terminal: %s", pane.TerminalID)}
	terminal, ok := state.Domain.Terminals[pane.TerminalID]
	if ok {
		label := terminal.Name
		if label == "" {
			label = string(terminal.ID)
		}
		lines[0] = compactLine(lines[0], fmt.Sprintf("title: %s", label))
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
	return lines
}

func (r runtimeRenderer) renderScreenSection(pane types.PaneState, compact bool) []string {
	if pane.TerminalID == "" || r.Screens == nil {
		return []string{"screen: <unavailable>"}
	}
	snapshot, ok := r.Screens.Snapshot(pane.TerminalID)
	if !ok || snapshot == nil {
		return []string{"screen: <unavailable>"}
	}
	rows, totalRows, truncated := renderSnapshotRows(snapshot)
	meta := []string{fmt.Sprintf("screen_rows: %d/%d", len(rows), totalRows)}
	if truncated {
		meta = append(meta, "screen_truncated: true")
	}
	if compact {
		return []string{compactLine(append([]string{"screen: <suppressed by overlay>"}, meta...)...)}
	}
	lines := []string{compactLine(append([]string{"screen:"}, meta...)...)}
	return append(lines, rows...)
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

func renderOverlayLines(overlay types.OverlayState) []string {
	switch overlay.Kind {
	case types.OverlayWorkspacePicker:
		picker, ok := overlay.Data.(*workspacedomain.PickerState)
		if !ok || picker == nil {
			return []string{fmt.Sprintf("overlay: %s", overlay.Kind)}
		}
		return renderWorkspacePickerLines(picker)
	case types.OverlayTerminalManager:
		manager, ok := overlay.Data.(*terminalmanagerdomain.State)
		if !ok || manager == nil {
			return []string{fmt.Sprintf("overlay: %s", overlay.Kind)}
		}
		return renderTerminalManagerLines(manager)
	case types.OverlayTerminalPicker:
		picker, ok := overlay.Data.(*terminalpickerdomain.State)
		if !ok || picker == nil {
			return []string{fmt.Sprintf("overlay: %s", overlay.Kind)}
		}
		return renderTerminalPickerLines(picker)
	case types.OverlayLayoutResolve:
		resolve, ok := overlay.Data.(*layoutresolvedomain.State)
		if !ok || resolve == nil {
			return []string{fmt.Sprintf("overlay: %s", overlay.Kind)}
		}
		return renderLayoutResolveLines(resolve)
	case types.OverlayPrompt:
		prompt, ok := overlay.Data.(*promptdomain.State)
		if !ok || prompt == nil {
			return []string{fmt.Sprintf("overlay: %s", overlay.Kind)}
		}
		return renderPromptLines(prompt)
	default:
		return []string{fmt.Sprintf("overlay: %s", overlay.Kind)}
	}
}

func renderWorkspacePickerLines(picker *workspacedomain.PickerState) []string {
	rows := picker.VisibleRows()
	lines := []string{compactLine(
		fmt.Sprintf("workspace_picker_query: %s", picker.Query()),
		fmt.Sprintf("workspace_picker_row_count: %d", len(rows)),
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

func renderTerminalManagerLines(manager *terminalmanagerdomain.State) []string {
	rows := manager.VisibleRows()
	lines := []string{compactLine(
		fmt.Sprintf("terminal_manager_query: %s", manager.Query()),
		fmt.Sprintf("terminal_manager_row_count: %d", len(rows)),
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
			compactLine(
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
			compactLine(
				fmt.Sprintf("detail_connected_panes: %d", detail.ConnectedPaneCount),
				fmt.Sprintf("detail_location_count: %d", len(detail.Locations)),
				fmt.Sprintf("detail_command: %s", detail.Command),
			),
			compactLine(fmt.Sprintf("detail_owner: %s", detail.OwnerSlotLabel), detailTags),
		)
		if locations := renderDetailLocations(detail.Locations); len(locations) > 0 {
			previewLocations, truncated := overlayPreviewStrings(locations, runtimeOverlayDetailPreviewRows)
			meta := []string{"detail_locations:", strings.Join(previewLocations, " ; ")}
			if truncated {
				meta = append(meta, "detail_locations_truncated: true")
			}
			lines = append(lines, compactLine(meta...))
		}
	}
	return lines
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
		lines = append(lines, fmt.Sprintf("- %s/%s/%s", location.WorkspaceName, location.TabName, location.SlotLabel))
	}
	return lines
}

func renderPromptLines(prompt *promptdomain.State) []string {
	lines := []string{compactLine(
		fmt.Sprintf("prompt_title: %s", prompt.Title),
		fmt.Sprintf("prompt_kind: %s", prompt.Kind),
	)}
	if prompt.TerminalID != "" {
		lines = append(lines, fmt.Sprintf("prompt_terminal: %s", prompt.TerminalID))
	}
	if len(prompt.Fields) == 0 {
		lines = append(lines,
			compactLine("prompt_active_field: draft", "prompt_active_label: draft", fmt.Sprintf("prompt_active_value: %s", prompt.Draft)),
			compactLine("prompt_active_index: 0", "prompt_field_count: 0"),
			"prompt_fields: | prompt_fields_rendered: 1",
			fmt.Sprintf("> [draft] %s", prompt.Draft),
		)
		return lines
	}
	active := prompt.Active
	if active < 0 || active >= len(prompt.Fields) {
		active = 0
	}
	lines = append(lines,
		compactLine(
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
	return lines
}

func renderTerminalPickerLines(picker *terminalpickerdomain.State) []string {
	rows := picker.VisibleRows()
	lines := []string{compactLine(
		fmt.Sprintf("terminal_picker_query: %s", picker.Query()),
		fmt.Sprintf("terminal_picker_row_count: %d", len(rows)),
	)}
	if row, ok := picker.SelectedRow(); ok && row.Kind == terminalpickerdomain.RowKindTerminal {
		lines = append(lines,
			compactLine(
				fmt.Sprintf("terminal_picker_selected: %s", row.TerminalID),
				fmt.Sprintf("terminal_picker_selected_label: %s", row.Label),
				fmt.Sprintf("terminal_picker_selected_kind: %s", row.Kind),
			),
			compactLine(
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

func renderLayoutResolveLines(resolve *layoutresolvedomain.State) []string {
	rows := resolve.Rows()
	lines := []string{
		compactLine(
			fmt.Sprintf("layout_resolve_pane: %s", resolve.PaneID),
			fmt.Sprintf("layout_resolve_role: %s", resolve.Role),
		),
		compactLine(
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

func renderNoticeLines(notices []btui.Notice) []string {
	if len(notices) == 0 {
		return []string{"notices: 0"}
	}
	lines := []string{"notices:"}
	start := 0
	if len(notices) > runtimeOverlayDetailPreviewRows {
		start = len(notices) - runtimeOverlayDetailPreviewRows
	}
	previewNotices := notices[start:]
	truncated := start > 0
	meta := []string{fmt.Sprintf("notices_rendered: %d", len(previewNotices))}
	if truncated {
		meta = append(meta, "notices_truncated: true")
	}
	lines = append(lines, compactLine(meta...))
	for _, notice := range previewNotices {
		if notice.Text == "" {
			continue
		}
		line := fmt.Sprintf("[%s] %s", notice.Level, notice.Text)
		if notice.Count > 1 {
			line = fmt.Sprintf("%s (x%d)", line, notice.Count)
		}
		lines = append(lines, line)
	}
	if len(lines) == 1 {
		return nil
	}
	return lines
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
