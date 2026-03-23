package tui

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/protocol"
	btui "github.com/lozzow/termx/tui/bt"
	"github.com/lozzow/termx/tui/domain/types"
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
	lines = append(lines, renderNoticeLines(notices)...)
	return strings.Join(lines, "\n")
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
