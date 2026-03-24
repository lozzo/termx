package bt

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tui/app/intent"
	layoutresolvedomain "github.com/lozzow/termx/tui/domain/layoutresolve"
	promptdomain "github.com/lozzow/termx/tui/domain/prompt"
	terminalmanagerdomain "github.com/lozzow/termx/tui/domain/terminalmanager"
	terminalpickerdomain "github.com/lozzow/termx/tui/domain/terminalpicker"
	"github.com/lozzow/termx/tui/domain/types"
	workspacedomain "github.com/lozzow/termx/tui/domain/workspace"
)

const overlayPreviewRowLimit = 8
const terminalManagerPreviewRowLimit = 4
const overlayDetailPreviewRowLimit = 4

func mapWorkspacePickerMouseClick(state types.AppState, msg tea.MouseMsg, view string) []intent.Intent {
	if !isLeftMousePress(msg) {
		return nil
	}
	picker, ok := state.UI.Overlay.Data.(*workspacedomain.PickerState)
	if !ok || picker == nil {
		return nil
	}
	rows := picker.VisibleRows()
	selected, hasSelection := picker.SelectedRow()
	selectedIndex := 0
	if hasSelection {
		for index, row := range rows {
			if row.Node.Key == selected.Node.Key {
				selectedIndex = index
				break
			}
		}
	}
	targetIndex, ok := overlayClickedRowIndex(view, "workspace_picker_rows:", msg.Y, len(rows), overlayPreviewRowLimit, selectedIndex)
	if !ok {
		return nil
	}
	delta := targetIndex - selectedIndex
	if delta == 0 {
		// 已选中行再次点击，等价于键盘 enter，先收口默认提交语义。
		return []intent.Intent{intent.WorkspacePickerSubmitIntent{}}
	}
	return []intent.Intent{intent.WorkspacePickerMoveIntent{Delta: delta}}
}

func mapTerminalPickerMouseClick(state types.AppState, msg tea.MouseMsg, view string) []intent.Intent {
	if !isLeftMousePress(msg) {
		return nil
	}
	picker, ok := state.UI.Overlay.Data.(*terminalpickerdomain.State)
	if !ok || picker == nil {
		return nil
	}
	rows := picker.VisibleRows()
	selected, hasSelection := picker.SelectedRow()
	selectedIndex := 0
	if hasSelection {
		for index, row := range rows {
			if row.Kind == selected.Kind && row.TerminalID == selected.TerminalID && row.Label == selected.Label {
				selectedIndex = index
				break
			}
		}
	}
	targetIndex, ok := overlayClickedRowIndex(view, "terminal_picker_rows:", msg.Y, len(rows), overlayPreviewRowLimit, selectedIndex)
	if !ok {
		return nil
	}
	delta := targetIndex - selectedIndex
	if delta == 0 {
		// picker 行已经处于选中态时，鼠标再次点击直接触发默认连接/创建动作。
		return []intent.Intent{intent.TerminalPickerSubmitIntent{}}
	}
	return []intent.Intent{intent.TerminalPickerMoveIntent{Delta: delta}}
}

func mapLayoutResolveMouseClick(state types.AppState, msg tea.MouseMsg, view string) []intent.Intent {
	if !isLeftMousePress(msg) {
		return nil
	}
	resolve, ok := state.UI.Overlay.Data.(*layoutresolvedomain.State)
	if !ok || resolve == nil {
		return nil
	}
	rows := resolve.Rows()
	selected, hasSelection := resolve.SelectedRow()
	selectedIndex := 0
	if hasSelection {
		for index, row := range rows {
			if row.Action == selected.Action && row.Label == selected.Label {
				selectedIndex = index
				break
			}
		}
	}
	targetIndex, ok := overlayClickedRowIndex(view, "layout_resolve_rows:", msg.Y, len(rows), overlayPreviewRowLimit, selectedIndex)
	if !ok {
		return nil
	}
	delta := targetIndex - selectedIndex
	if delta == 0 {
		// resolve overlay 先保持最小模型：再次点击已选中行就提交当前动作。
		return []intent.Intent{intent.LayoutResolveSubmitIntent{}}
	}
	return []intent.Intent{intent.LayoutResolveMoveIntent{Delta: delta}}
}

func mapTerminalManagerMouseClick(state types.AppState, msg tea.MouseMsg, view string) []intent.Intent {
	if !isLeftMousePress(msg) {
		return nil
	}
	manager, ok := state.UI.Overlay.Data.(*terminalmanagerdomain.State)
	if !ok || manager == nil {
		return nil
	}
	rows := manager.VisibleRows()
	selected, hasSelection := manager.SelectedRow()
	if !hasSelection {
		return nil
	}
	selectedVisibleIndex := terminalManagerVisibleIndex(rows, selected)
	if selectedVisibleIndex < 0 {
		return nil
	}
	targetVisibleIndex, ok := overlayClickedRowIndex(view, "terminal_manager_rows:", msg.Y, len(rows), terminalManagerPreviewRowLimit, selectedVisibleIndex)
	if !ok {
		return nil
	}
	targetRow := rows[targetVisibleIndex]
	if targetRow.Kind == terminalmanagerdomain.RowKindHeader {
		return nil
	}

	selectableRows := terminalManagerSelectableRows(rows)
	selectedSelectableIndex := terminalManagerSelectableIndex(selectableRows, selected)
	targetSelectableIndex := terminalManagerSelectableIndex(selectableRows, targetRow)
	if selectedSelectableIndex < 0 || targetSelectableIndex < 0 {
		return nil
	}
	delta := targetSelectableIndex - selectedSelectableIndex
	if delta == 0 {
		// terminal manager 的默认鼠标提交先对齐 enter，也就是 connect-here/create。
		return []intent.Intent{intent.TerminalManagerConnectHereIntent{}}
	}
	return []intent.Intent{intent.TerminalManagerMoveIntent{Delta: delta}}
}

func mapPromptMouseClick(state types.AppState, msg tea.MouseMsg, view string) []intent.Intent {
	if !isLeftMousePress(msg) {
		return nil
	}
	prompt, ok := state.UI.Overlay.Data.(*promptdomain.State)
	if !ok || prompt == nil || len(prompt.Fields) == 0 {
		return nil
	}
	active := prompt.Active
	if active < 0 || active >= len(prompt.Fields) {
		active = 0
	}
	targetIndex, ok := overlayClickedRowIndex(view, "prompt_fields:", msg.Y, len(prompt.Fields), overlayDetailPreviewRowLimit, active)
	if !ok || targetIndex == active {
		return nil
	}
	return []intent.Intent{intent.PromptSelectFieldIntent{Index: targetIndex}}
}

// overlayClickedRowIndex 根据当前渲染文本里的 rows 区域起点，反推出点击命中的真实 row 索引。
// 这里不解析每一行具体文本，而是复用 state 中的真实 row 投影，避免展示文本反向驱动状态。
func overlayClickedRowIndex(view string, prefix string, y int, rowCount int, previewLimit int, selectedIndex int) (int, bool) {
	if y < 0 || strings.TrimSpace(view) == "" || rowCount == 0 {
		return 0, false
	}
	metaIndex := findLineIndexWithPrefix(view, prefix)
	if metaIndex < 0 {
		return 0, false
	}
	start, end := overlayPreviewWindow(rowCount, previewLimit, selectedIndex)
	clickedPreviewIndex := y - metaIndex - 1
	if clickedPreviewIndex < 0 || clickedPreviewIndex >= end-start {
		return 0, false
	}
	return start + clickedPreviewIndex, true
}

func isLeftMousePress(msg tea.MouseMsg) bool {
	event := tea.MouseEvent(msg)
	return event.Button == tea.MouseButtonLeft && event.Action == tea.MouseActionPress
}

func overlayPreviewWindow(rowCount int, previewLimit int, selectedIndex int) (int, int) {
	if previewLimit <= 0 || rowCount <= previewLimit {
		return 0, rowCount
	}
	if selectedIndex < 0 {
		selectedIndex = 0
	}
	if selectedIndex >= rowCount {
		selectedIndex = rowCount - 1
	}
	start := selectedIndex - previewLimit + 1
	if start < 0 {
		start = 0
	}
	end := start + previewLimit
	if end > rowCount {
		end = rowCount
		start = end - previewLimit
	}
	return start, end
}

func terminalManagerVisibleIndex(rows []terminalmanagerdomain.Row, target terminalmanagerdomain.Row) int {
	for index, row := range rows {
		if row.Kind == target.Kind && row.TerminalID == target.TerminalID && row.Label == target.Label {
			return index
		}
	}
	return -1
}

func terminalManagerSelectableRows(rows []terminalmanagerdomain.Row) []terminalmanagerdomain.Row {
	out := make([]terminalmanagerdomain.Row, 0, len(rows))
	for _, row := range rows {
		if row.Kind == terminalmanagerdomain.RowKindHeader {
			continue
		}
		out = append(out, row)
	}
	return out
}

func terminalManagerSelectableIndex(rows []terminalmanagerdomain.Row, target terminalmanagerdomain.Row) int {
	for index, row := range rows {
		if row.Kind == target.Kind && row.TerminalID == target.TerminalID && row.Label == target.Label {
			return index
		}
	}
	return -1
}

func findLineIndexWithPrefix(view string, prefix string) int {
	for index, line := range strings.Split(view, "\n") {
		if strings.HasPrefix(line, prefix) {
			return index
		}
	}
	return -1
}
