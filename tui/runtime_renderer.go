package tui

import (
	"fmt"
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
		fmt.Sprintf("pane: %s", pane.ID),
		fmt.Sprintf("slot: %s", pane.SlotState),
		fmt.Sprintf("overlay: %s", state.UI.Overlay.Kind),
	}
	lines = append(lines, renderModeLines(state.UI.Mode)...)
	if pane.TerminalID != "" {
		lines = append(lines, fmt.Sprintf("terminal: %s", pane.TerminalID))
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok {
			label := terminal.Name
			if label == "" {
				label = string(terminal.ID)
			}
			lines = append(lines, fmt.Sprintf("title: %s", label))
		}
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
		"workspace_picker_rows:",
	}
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
		"terminal_manager_rows:",
	}
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
			fmt.Sprintf("detail_state: %s", detail.State),
			fmt.Sprintf("detail_visibility: %s", detail.VisibilityLabel),
			fmt.Sprintf("detail_command: %s", detail.Command),
		)
		if detail.OwnerSlotLabel != "" {
			lines = append(lines, fmt.Sprintf("detail_owner: %s", detail.OwnerSlotLabel))
		}
		if tags := renderDetailTags(detail.Tags); tags != "" {
			lines = append(lines, fmt.Sprintf("detail_tags: %s", tags))
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

func renderPromptLines(prompt *promptdomain.State) []string {
	lines := []string{
		fmt.Sprintf("prompt_title: %s", prompt.Title),
		fmt.Sprintf("prompt_kind: %s", prompt.Kind),
	}
	if len(prompt.Fields) == 0 {
		lines = append(lines,
			"prompt_fields:",
			fmt.Sprintf("> [draft] %s", prompt.Draft),
		)
		return lines
	}
	lines = append(lines, "prompt_fields:")
	active := prompt.Active
	if active < 0 || active >= len(prompt.Fields) {
		active = 0
	}
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
		"terminal_picker_rows:",
	}
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
		fmt.Sprintf("layout_resolve_role: %s", resolve.Role),
		fmt.Sprintf("layout_resolve_hint: %s", resolve.Hint),
		"layout_resolve_rows:",
	}
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
