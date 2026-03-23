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

	lines := []string{
		fmt.Sprintf("workspace: %s", workspace.Name),
		fmt.Sprintf("tab: %s", tab.Name),
		fmt.Sprintf("tab_layer: %s", tab.ActiveLayer),
		fmt.Sprintf("pane: %s", pane.ID),
		fmt.Sprintf("slot: %s", pane.SlotState),
		fmt.Sprintf("overlay: %s", state.UI.Overlay.Kind),
	}
	lines = append(lines, renderFocusLines(state.UI.Focus)...)
	lines = append(lines, renderModeLines(state.UI.Mode)...)
	lines = append(lines, renderPaneStateLines(pane)...)
	if pane.TerminalID != "" {
		lines = append(lines, fmt.Sprintf("terminal: %s", pane.TerminalID))
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok {
			label := terminal.Name
			if label == "" {
				label = string(terminal.ID)
			}
			lines = append(lines, fmt.Sprintf("title: %s", label))
			lines = append(lines, renderTerminalStateLines(terminal)...)
		}
		lines = append(lines, renderConnectionLines(state.Domain.Connections[pane.TerminalID], pane.ID)...)
		if r.Screens != nil {
			if snapshot, ok := r.Screens.Snapshot(pane.TerminalID); ok && snapshot != nil {
				lines = append(lines, "screen:")
				lines = append(lines, renderSnapshotRows(snapshot)...)
			}
			if status, ok := r.Screens.Status(pane.TerminalID); ok {
				lines = append(lines, renderRuntimeStatusLines(status)...)
			}
		}
	}
	lines = append(lines, renderOverlayLines(state.UI.Overlay)...)
	lines = append(lines, renderNoticeLines(notices)...)
	return strings.Join(lines, "\n")
}

func renderPaneStateLines(pane types.PaneState) []string {
	lines := []string{fmt.Sprintf("pane_kind: %s", pane.Kind)}
	if pane.LastExitCode != nil {
		lines = append(lines, fmt.Sprintf("pane_exit_code: %d", *pane.LastExitCode))
	}
	return lines
}

// renderFocusLines 把当前焦点层和 overlay 目标显式投影到文本视图里，
// 这样 runtime E2E 可以直接验证交互是否切到了预期焦点。
func renderFocusLines(focus types.FocusState) []string {
	lines := []string{fmt.Sprintf("focus_layer: %s", focus.Layer)}
	if focus.OverlayTarget != "" {
		lines = append(lines, fmt.Sprintf("focus_overlay_target: %s", focus.OverlayTarget))
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

func renderTerminalStateLines(terminal types.TerminalRef) []string {
	var lines []string
	if terminal.State != "" {
		lines = append(lines, fmt.Sprintf("terminal_state: %s", terminal.State))
	}
	if terminal.ExitCode != nil {
		lines = append(lines, fmt.Sprintf("terminal_exit_code: %d", *terminal.ExitCode))
	}
	if len(terminal.Command) > 0 {
		lines = append(lines, fmt.Sprintf("terminal_command: %s", strings.Join(terminal.Command, " ")))
	}
	if tags := renderTerminalTags(terminal.Tags); tags != "" {
		lines = append(lines, fmt.Sprintf("terminal_tags: %s", tags))
	}
	return lines
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

func renderModeLines(mode types.ModeState) []string {
	if mode.Active == types.ModeNone {
		return nil
	}
	return []string{
		fmt.Sprintf("mode: %s", mode.Active),
		fmt.Sprintf("mode_sticky: %t", mode.Sticky),
	}
}

func renderRuntimeStatusLines(status RuntimeTerminalStatus) []string {
	var lines []string
	if status.State != "" {
		lines = append(lines, fmt.Sprintf("runtime_state: %s", status.State))
	}
	if status.Closed && status.ExitCode != nil {
		lines = append(lines, fmt.Sprintf("runtime_exit_code: %d", *status.ExitCode))
	}
	if status.Size.Cols > 0 || status.Size.Rows > 0 {
		lines = append(lines, fmt.Sprintf("runtime_size: %dx%d", status.Size.Cols, status.Size.Rows))
	}
	if status.ObserverOnly {
		lines = append(lines, "runtime_access: observer_only")
	}
	if status.SyncLost {
		lines = append(lines, fmt.Sprintf("runtime_sync_lost: %d", status.SyncLostDroppedBytes))
	}
	if status.RemovedReason != "" {
		lines = append(lines, fmt.Sprintf("runtime_removed: %s", status.RemovedReason))
	}
	if status.ReadError != "" {
		lines = append(lines, fmt.Sprintf("runtime_read_error: %s", status.ReadError))
	}
	return lines
}

func renderSnapshotRows(snapshot *protocol.Snapshot) []string {
	if snapshot == nil || len(snapshot.Screen.Cells) == 0 {
		return []string{"<empty>"}
	}
	lines := make([]string, 0, len(snapshot.Screen.Cells))
	for _, row := range snapshot.Screen.Cells {
		lines = append(lines, renderSnapshotRow(row))
	}
	return lines
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
			return nil
		}
		return renderWorkspacePickerLines(picker)
	case types.OverlayTerminalManager:
		manager, ok := overlay.Data.(*terminalmanagerdomain.State)
		if !ok || manager == nil {
			return nil
		}
		return renderTerminalManagerLines(manager)
	case types.OverlayTerminalPicker:
		picker, ok := overlay.Data.(*terminalpickerdomain.State)
		if !ok || picker == nil {
			return nil
		}
		return renderTerminalPickerLines(picker)
	case types.OverlayLayoutResolve:
		resolve, ok := overlay.Data.(*layoutresolvedomain.State)
		if !ok || resolve == nil {
			return nil
		}
		return renderLayoutResolveLines(resolve)
	case types.OverlayPrompt:
		prompt, ok := overlay.Data.(*promptdomain.State)
		if !ok || prompt == nil {
			return nil
		}
		return renderPromptLines(prompt)
	default:
		return nil
	}
}

func renderWorkspacePickerLines(picker *workspacedomain.PickerState) []string {
	lines := []string{
		fmt.Sprintf("workspace_picker_query: %s", picker.Query()),
	}
	if node, ok := picker.SelectedNode(); ok {
		lines = append(lines,
			fmt.Sprintf("workspace_picker_selected: %s", node.Key),
			fmt.Sprintf("workspace_picker_selected_kind: %s", node.Kind),
			fmt.Sprintf("workspace_picker_selected_label: %s", node.Label),
		)
	}
	if row, ok := picker.SelectedRow(); ok {
		lines = append(lines, fmt.Sprintf("workspace_picker_selected_expanded: %t", row.Expanded))
	}
	lines = append(lines, "workspace_picker_rows:")
	selected, hasSelection := picker.SelectedRow()
	for _, row := range picker.VisibleRows() {
		prefix := "  "
		if hasSelection && row.Node.Key == selected.Node.Key {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%s[%s] %s", prefix, strings.Repeat("  ", row.Depth), row.Node.Kind, row.Node.Label))
	}
	return lines
}

func renderTerminalManagerLines(manager *terminalmanagerdomain.State) []string {
	lines := []string{
		fmt.Sprintf("terminal_manager_query: %s", manager.Query()),
	}
	if row, ok := manager.SelectedRow(); ok && row.Kind == terminalmanagerdomain.RowKindTerminal {
		lines = append(lines,
			fmt.Sprintf("terminal_manager_selected: %s", row.TerminalID),
			fmt.Sprintf("terminal_manager_selected_label: %s", row.Label),
			fmt.Sprintf("terminal_manager_selected_kind: %s", row.Kind),
			fmt.Sprintf("terminal_manager_selected_section: %s", row.Section),
			fmt.Sprintf("terminal_manager_selected_state: %s", row.State),
			fmt.Sprintf("terminal_manager_selected_visible: %t", row.Visible),
			fmt.Sprintf("terminal_manager_selected_connected_panes: %d", row.ConnectedPaneCount),
			fmt.Sprintf("terminal_manager_selected_command: %s", row.Command),
		)
	} else if terminalID, ok := manager.SelectedTerminalID(); ok {
		lines = append(lines, fmt.Sprintf("terminal_manager_selected: %s", terminalID))
	}
	lines = append(lines, "terminal_manager_rows:")
	selected, hasSelection := manager.SelectedRow()
	for _, row := range manager.VisibleRows() {
		prefix := "  "
		if hasSelection && row.Kind != terminalmanagerdomain.RowKindHeader && row.Kind == selected.Kind && row.TerminalID == selected.TerminalID && row.Label == selected.Label {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s[%s] %s", prefix, row.Kind, row.Label))
	}
	if detail, ok := manager.SelectedDetail(); ok {
		lines = append(lines,
			fmt.Sprintf("terminal_manager_detail: %s", detail.Name),
			fmt.Sprintf("detail_terminal: %s", detail.TerminalID),
			fmt.Sprintf("detail_state: %s", detail.State),
			fmt.Sprintf("detail_visibility: %s", detail.VisibilityLabel),
			fmt.Sprintf("detail_connected_panes: %d", detail.ConnectedPaneCount),
			fmt.Sprintf("detail_command: %s", detail.Command),
		)
		if detail.OwnerSlotLabel != "" {
			lines = append(lines, fmt.Sprintf("detail_owner: %s", detail.OwnerSlotLabel))
		}
		if tags := renderDetailTags(detail.Tags); tags != "" {
			lines = append(lines, fmt.Sprintf("detail_tags: %s", tags))
		}
		if locations := renderDetailLocations(detail.Locations); len(locations) > 0 {
			lines = append(lines, "detail_locations:")
			lines = append(lines, locations...)
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
	lines := []string{
		fmt.Sprintf("prompt_title: %s", prompt.Title),
		fmt.Sprintf("prompt_kind: %s", prompt.Kind),
	}
	if prompt.TerminalID != "" {
		lines = append(lines, fmt.Sprintf("prompt_terminal: %s", prompt.TerminalID))
	}
	if len(prompt.Fields) == 0 {
		lines = append(lines,
			"prompt_active_field: draft",
			"prompt_active_label: draft",
			fmt.Sprintf("prompt_active_value: %s", prompt.Draft),
			"prompt_fields:",
			fmt.Sprintf("> [draft] %s", prompt.Draft),
		)
		return lines
	}
	active := prompt.Active
	if active < 0 || active >= len(prompt.Fields) {
		active = 0
	}
	lines = append(lines,
		fmt.Sprintf("prompt_active_field: %s", prompt.Fields[active].Key),
		fmt.Sprintf("prompt_active_label: %s", prompt.Fields[active].Label),
		fmt.Sprintf("prompt_active_value: %s", prompt.Fields[active].Value),
		"prompt_fields:",
	)
	for idx, field := range prompt.Fields {
		prefix := "  "
		if idx == active {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s[%s] %s: %s", prefix, field.Key, field.Label, field.Value))
	}
	return lines
}

func renderTerminalPickerLines(picker *terminalpickerdomain.State) []string {
	lines := []string{
		fmt.Sprintf("terminal_picker_query: %s", picker.Query()),
	}
	if row, ok := picker.SelectedRow(); ok && row.Kind == terminalpickerdomain.RowKindTerminal {
		lines = append(lines,
			fmt.Sprintf("terminal_picker_selected: %s", row.TerminalID),
			fmt.Sprintf("terminal_picker_selected_label: %s", row.Label),
			fmt.Sprintf("terminal_picker_selected_kind: %s", row.Kind),
			fmt.Sprintf("terminal_picker_selected_state: %s", row.State),
		)
	} else if terminalID, ok := picker.SelectedTerminalID(); ok {
		lines = append(lines, fmt.Sprintf("terminal_picker_selected: %s", terminalID))
	}
	lines = append(lines, "terminal_picker_rows:")
	selected, hasSelection := picker.SelectedRow()
	for _, row := range picker.VisibleRows() {
		prefix := "  "
		if hasSelection && row.Kind == selected.Kind && row.TerminalID == selected.TerminalID && row.Label == selected.Label {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s[%s] %s", prefix, row.Kind, row.Label))
	}
	return lines
}

func renderLayoutResolveLines(resolve *layoutresolvedomain.State) []string {
	lines := []string{
		fmt.Sprintf("layout_resolve_pane: %s", resolve.PaneID),
		fmt.Sprintf("layout_resolve_role: %s", resolve.Role),
		fmt.Sprintf("layout_resolve_hint: %s", resolve.Hint),
	}
	if row, ok := resolve.SelectedRow(); ok {
		lines = append(lines,
			fmt.Sprintf("layout_resolve_selected: %s", row.Action),
			fmt.Sprintf("layout_resolve_selected_label: %s", row.Label),
		)
	}
	lines = append(lines, "layout_resolve_rows:")
	selected, hasSelection := resolve.SelectedRow()
	for _, row := range resolve.Rows() {
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
		return nil
	}
	lines := []string{"notices:"}
	for _, notice := range notices {
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
