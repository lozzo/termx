package bt

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
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

// mapWorkbenchMouseClick 为默认 modern 工作台补最小鼠标命中：
// 先不做复杂几何系统，而是复用用户第一眼能点击到的 pane 标题文本。
// 这样 split 侧栏、floating deck、pane 卡片标题都能共用一套“点击即切焦点”的路径。
func mapWorkbenchMouseClick(state types.AppState, msg tea.MouseMsg, view string) []intent.Intent {
	if !isLeftMousePress(msg) {
		return nil
	}
	workspace, tab, ok := currentWorkbench(state)
	if !ok {
		return nil
	}
	line, ok := lineAtIndexStripped(view, msg.Y)
	if !ok {
		return nil
	}
	if targetTabID, targetPaneID, ok := clickedWorkbenchTabJumpTarget(workspace, line, msg.X); ok {
		return []intent.Intent{intent.WorkspaceTreeJumpIntent{
			WorkspaceID: workspace.ID,
			TabID:       targetTabID,
			PaneID:      targetPaneID,
		}}
	}
	paneID, ok := clickedWorkbenchPaneID(state, tab, line, msg.X)
	if !ok {
		return nil
	}
	return []intent.Intent{intent.WorkspaceTreeJumpIntent{
		WorkspaceID: workspace.ID,
		TabID:       tab.ID,
		PaneID:      paneID,
	}}
}

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
	targetIndex, ok := overlayClickedWorkspacePickerRowIndex(view, msg.Y, rows, selectedIndex)
	if !ok {
		return nil
	}
	delta := targetIndex - selectedIndex
	if delta == 0 {
		// 已选中行再次点击，等价于键盘 enter，先收口默认提交语义。
		return []intent.Intent{intent.WorkspacePickerSubmitIntent{}}
	}
	// workspace picker 更像导航选择器，点击目标行时直接完成“切到该行并提交”。
	return []intent.Intent{
		intent.WorkspacePickerMoveIntent{Delta: delta},
		intent.WorkspacePickerSubmitIntent{},
	}
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
	targetIndex, ok := overlayClickedTerminalPickerRowIndex(view, msg.Y, rows, selectedIndex)
	if !ok {
		return nil
	}
	delta := targetIndex - selectedIndex
	if delta == 0 {
		// picker 行已经处于选中态时，鼠标再次点击直接触发默认连接/创建动作。
		return []intent.Intent{intent.TerminalPickerSubmitIntent{}}
	}
	// terminal picker 没有独立详情区，点击某一行就直接执行 connect/create 默认动作。
	return []intent.Intent{
		intent.TerminalPickerMoveIntent{Delta: delta},
		intent.TerminalPickerSubmitIntent{},
	}
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
	targetIndex, ok := overlayClickedLayoutResolveRowIndex(view, msg.Y, rows, selectedIndex)
	if !ok {
		return nil
	}
	delta := targetIndex - selectedIndex
	if delta == 0 {
		// resolve overlay 先保持最小模型：再次点击已选中行就提交当前动作。
		return []intent.Intent{intent.LayoutResolveSubmitIntent{}}
	}
	// layout resolve 本身就是动作列表，点击非当前项时直接切换并立即提交。
	return []intent.Intent{
		intent.LayoutResolveMoveIntent{Delta: delta},
		intent.LayoutResolveSubmitIntent{},
	}
}

func mapTerminalManagerMouseClick(state types.AppState, msg tea.MouseMsg, view string) []intent.Intent {
	if !isLeftMousePress(msg) {
		return nil
	}
	manager, ok := state.UI.Overlay.Data.(*terminalmanagerdomain.State)
	if !ok || manager == nil {
		return nil
	}
	if intents := mapTerminalManagerLocationClick(manager, msg, view); len(intents) > 0 {
		return intents
	}
	if intents := mapTerminalManagerActionClick(msg, view); len(intents) > 0 {
		return intents
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
	targetVisibleIndex, ok := overlayClickedTerminalManagerRowIndex(view, msg.Y, rows, selectedVisibleIndex)
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
	if targetRow.Kind == terminalmanagerdomain.RowKindCreate {
		if delta == 0 {
			// create row 没有详情态，单击直接对齐 enter 触发创建更符合直觉。
			return []intent.Intent{intent.TerminalManagerConnectHereIntent{}}
		}
		return []intent.Intent{
			intent.TerminalManagerMoveIntent{Delta: delta},
			intent.TerminalManagerConnectHereIntent{},
		}
	}
	if delta == 0 {
		// terminal 行维持 inspect-first：再次点击当前选中项才执行 connect-here。
		return []intent.Intent{intent.TerminalManagerConnectHereIntent{}}
	}
	return []intent.Intent{intent.TerminalManagerMoveIntent{Delta: delta}}
}

func mapTerminalManagerLocationClick(manager *terminalmanagerdomain.State, msg tea.MouseMsg, view string) []intent.Intent {
	detail, ok := manager.SelectedDetail()
	if !ok || len(detail.Locations) == 0 {
		return nil
	}
	targetIndex, ok := overlayClickedStringRowIndex(view, "detail_locations:", msg.Y, len(detail.Locations), overlayDetailPreviewRowLimit, 0, func(index int) string {
		location := detail.Locations[index]
		return "[location] " + location.WorkspaceName + "/" + location.TabName + "/" + location.SlotLabel
	})
	if !ok {
		return nil
	}
	location := detail.Locations[targetIndex]
	return []intent.Intent{intent.TerminalManagerJumpToLocationIntent{
		WorkspaceID: location.WorkspaceID,
		TabID:       location.TabID,
		PaneID:      location.PaneID,
	}}
}

func mapTerminalManagerActionClick(msg tea.MouseMsg, view string) []intent.Intent {
	actionRows := terminalmanagerdomain.ActionRows()
	targetIndex, ok := overlayClickedStringRowIndex(view, "terminal_manager_actions:", msg.Y, len(actionRows), len(actionRows), 0, func(index int) string {
		action := actionRows[index]
		return "[" + string(action.ID) + "] " + action.Label
	})
	if !ok {
		return nil
	}
	switch actionRows[targetIndex].ID {
	case terminalmanagerdomain.ActionJumpToConnectedPane:
		return []intent.Intent{intent.TerminalManagerJumpToConnectedPaneIntent{}}
	case terminalmanagerdomain.ActionConnectHere:
		return []intent.Intent{intent.TerminalManagerConnectHereIntent{}}
	case terminalmanagerdomain.ActionNewTab:
		return []intent.Intent{intent.TerminalManagerConnectInNewTabIntent{}}
	case terminalmanagerdomain.ActionFloatingPane:
		return []intent.Intent{intent.TerminalManagerConnectInFloatingPaneIntent{}}
	case terminalmanagerdomain.ActionEditMetadata:
		return []intent.Intent{intent.TerminalManagerEditMetadataIntent{}}
	case terminalmanagerdomain.ActionAcquireOwner:
		return []intent.Intent{intent.TerminalManagerAcquireOwnerIntent{}}
	case terminalmanagerdomain.ActionStop:
		return []intent.Intent{intent.TerminalManagerStopIntent{}}
	default:
		return nil
	}
}

func mapPromptMouseClick(state types.AppState, msg tea.MouseMsg, view string) []intent.Intent {
	if !isLeftMousePress(msg) {
		return nil
	}
	prompt, ok := state.UI.Overlay.Data.(*promptdomain.State)
	if !ok || prompt == nil {
		return nil
	}
	if intents := mapPromptActionClick(msg, view); len(intents) > 0 {
		return intents
	}
	if len(prompt.Fields) == 0 {
		return nil
	}
	active := prompt.Active
	if active < 0 || active >= len(prompt.Fields) {
		active = 0
	}
	targetIndex, ok := overlayClickedPromptFieldIndex(view, msg.Y, prompt.Fields, active)
	if !ok || targetIndex == active {
		return nil
	}
	return []intent.Intent{intent.PromptSelectFieldIntent{Index: targetIndex}}
}

func mapPromptActionClick(msg tea.MouseMsg, view string) []intent.Intent {
	actionRows := promptdomain.ActionRows()
	targetIndex, ok := overlayClickedStringRowIndex(view, "prompt_actions:", msg.Y, len(actionRows), len(actionRows), 0, func(index int) string {
		action := actionRows[index]
		return "[" + string(action.ID) + "] " + action.Label
	})
	if !ok {
		return nil
	}
	switch actionRows[targetIndex].ID {
	case promptdomain.ActionSubmit:
		return []intent.Intent{intent.SubmitPromptIntent{}}
	case promptdomain.ActionCancel:
		return []intent.Intent{intent.CancelPromptIntent{}}
	default:
		return nil
	}
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

// overlayClickedStringRowIndex 先尝试直接命中 screen shell 里真正可见的行文本，
// 再回退到 chrome metadata 区的相对行号映射，保证真实对话框和调试层都能点。
func overlayClickedStringRowIndex(view string, prefix string, y int, rowCount int, previewLimit int, selectedIndex int, rowText func(index int) string) (int, bool) {
	if y < 0 || rowCount == 0 || rowText == nil {
		return 0, false
	}
	if line, ok := lineAtIndex(view, y); ok {
		start, end := overlayPreviewWindow(rowCount, previewLimit, selectedIndex)
		for index := start; index < end; index++ {
			text := rowText(index)
			if text != "" && strings.Contains(line, text) {
				return index, true
			}
		}
	}
	return overlayClickedRowIndex(view, prefix, y, rowCount, previewLimit, selectedIndex)
}

func overlayClickedWorkspacePickerRowIndex(view string, y int, rows []workspacedomain.TreeRow, selectedIndex int) (int, bool) {
	return overlayClickedStringRowIndex(view, "workspace_picker_rows:", y, len(rows), overlayPreviewRowLimit, selectedIndex, func(index int) string {
		row := rows[index]
		return strings.Repeat("  ", row.Depth) + "[" + string(row.Node.Kind) + "] " + row.Node.Label
	})
}

func overlayClickedTerminalPickerRowIndex(view string, y int, rows []terminalpickerdomain.Row, selectedIndex int) (int, bool) {
	return overlayClickedStringRowIndex(view, "terminal_picker_rows:", y, len(rows), overlayPreviewRowLimit, selectedIndex, func(index int) string {
		row := rows[index]
		return "[" + string(row.Kind) + "] " + row.Label
	})
}

func overlayClickedLayoutResolveRowIndex(view string, y int, rows []layoutresolvedomain.Row, selectedIndex int) (int, bool) {
	return overlayClickedStringRowIndex(view, "layout_resolve_rows:", y, len(rows), overlayPreviewRowLimit, selectedIndex, func(index int) string {
		row := rows[index]
		return "[" + string(row.Action) + "] " + row.Label
	})
}

func overlayClickedTerminalManagerRowIndex(view string, y int, rows []terminalmanagerdomain.Row, selectedIndex int) (int, bool) {
	return overlayClickedStringRowIndex(view, "terminal_manager_rows:", y, len(rows), terminalManagerPreviewRowLimit, selectedIndex, func(index int) string {
		row := rows[index]
		return "[" + string(row.Kind) + "] " + row.Label
	})
}

func overlayClickedPromptFieldIndex(view string, y int, fields []promptdomain.Field, active int) (int, bool) {
	return overlayClickedStringRowIndex(view, "prompt_fields:", y, len(fields), overlayDetailPreviewRowLimit, active, func(index int) string {
		field := fields[index]
		return "[" + field.Key + "] " + field.Label + ": " + field.Value
	})
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

func lineAtIndex(view string, index int) (string, bool) {
	if index < 0 {
		return "", false
	}
	lines := strings.Split(view, "\n")
	if index >= len(lines) {
		return "", false
	}
	return lines[index], true
}

func lineAtIndexStripped(view string, index int) (string, bool) {
	line, ok := lineAtIndex(view, index)
	if !ok {
		return "", false
	}
	return xansi.Strip(line), true
}

func currentWorkbench(state types.AppState) (types.WorkspaceState, types.TabState, bool) {
	workspaceID := state.UI.Focus.WorkspaceID
	if workspaceID == "" {
		workspaceID = state.Domain.ActiveWorkspaceID
	}
	workspace, ok := state.Domain.Workspaces[workspaceID]
	if !ok {
		return types.WorkspaceState{}, types.TabState{}, false
	}
	tabID := state.UI.Focus.TabID
	if tabID == "" {
		tabID = workspace.ActiveTabID
	}
	tab, ok := workspace.Tabs[tabID]
	if !ok {
		return types.WorkspaceState{}, types.TabState{}, false
	}
	return workspace, tab, true
}

func clickedWorkbenchPaneID(state types.AppState, tab types.TabState, line string, mouseX int) (types.PaneID, bool) {
	if strings.TrimSpace(line) == "" {
		return "", false
	}
	type paneHit struct {
		paneID types.PaneID
		start  int
		end    int
	}
	hits := make([]paneHit, 0, len(tab.Panes))
	searchOffset := 0
	for _, paneID := range orderedWorkbenchPaneIDs(tab) {
		pane, ok := tab.Panes[paneID]
		if !ok {
			continue
		}
		title := workbenchPaneDisplayTitle(state, pane)
		if title == "" {
			continue
		}
		start, end, ok := clickedWorkbenchPaneTitleRange(line, searchOffset, title)
		if !ok {
			continue
		}
		searchOffset = end
		hits = append(hits, paneHit{paneID: paneID, start: start, end: end})
	}

	// split/floating 标题现在可能出现在同一行，仅靠“这一行只出现一个标题”已经不够。
	// 这里优先结合鼠标 X 坐标命中具体标题区间，保证多 pane 同行时仍然能精确切焦点。
	if mouseX >= 0 {
		for _, hit := range hits {
			startRune := utf8.RuneCountInString(line[:hit.start])
			endRune := utf8.RuneCountInString(line[:hit.end])
			startRune, endRune = expandWorkbenchPaneClickRange(line, startRune, endRune)
			if mouseX >= startRune && mouseX < endRune {
				return hit.paneID, true
			}
		}
	}

	// 回退到旧语义：当一行里只有一个 pane 标题时，即使没有精确 X 也允许点击成功。
	candidates := make([]types.PaneID, 0, len(hits))
	for _, hit := range hits {
		candidates = append(candidates, hit.paneID)
	}
	if len(candidates) == 1 {
		return candidates[0], true
	}
	return "", false
}

// clickedWorkbenchTabJumpTarget 优先处理顶部 tab strip 的点击。
// 这里不引入完整盒模型，而是直接复用 renderer 已经稳定输出的 tab label 文本，
// 再结合鼠标 X 坐标把点击归属到具体 tab，避免整行点击时总是落到第一个匹配项。
func clickedWorkbenchTabJumpTarget(workspace types.WorkspaceState, line string, mouseX int) (types.TabID, types.PaneID, bool) {
	if strings.TrimSpace(line) == "" || len(workspace.TabOrder) == 0 {
		return "", "", false
	}
	searchOffset := 0
	for index, tabID := range workspace.TabOrder {
		tab, ok := workspace.Tabs[tabID]
		if !ok {
			continue
		}
		start, end, ok := clickedWorkbenchTabRange(line, searchOffset, index, tab)
		if !ok {
			continue
		}
		searchOffset = end
		startRune := utf8.RuneCountInString(line[:start])
		endRune := utf8.RuneCountInString(line[:end])
		startRune, endRune = expandWorkbenchTabClickRange(line, startRune, endRune)
		if mouseX < startRune || mouseX >= endRune {
			continue
		}
		paneID, ok := activeWorkbenchTabPaneID(tab)
		if !ok {
			return "", "", false
		}
		return tabID, paneID, true
	}
	return "", "", false
}

func workbenchTabDisplayLabel(index int, tab types.TabState) string {
	return fmt.Sprintf("%d:%s", index+1, workbenchTabLabel(tab))
}

func workbenchTabShortLabel(index int, tab types.TabState) string {
	return fmt.Sprintf("%d:%s", index+1, workbenchTabIdentityLabel(tab))
}

func clickedWorkbenchTabRange(line string, searchOffset int, index int, tab types.TabState) (int, int, bool) {
	labels := []string{
		workbenchTabShortLabel(index, tab),
		workbenchTabDisplayLabel(index, tab),
	}
	for _, label := range labels {
		byteStart := strings.Index(line[searchOffset:], label)
		if byteStart < 0 {
			continue
		}
		byteStart += searchOffset
		return byteStart, byteStart + len(label), true
	}
	return 0, 0, false
}

func clickedWorkbenchPaneTitleRange(line string, searchOffset int, title string) (int, int, bool) {
	byteStart := strings.Index(line[searchOffset:], title)
	if byteStart < 0 {
		return 0, 0, false
	}
	byteStart += searchOffset
	return byteStart, byteStart + len(title), true
}

func workbenchTabLabel(tab types.TabState) string {
	label := workbenchTabIdentityLabel(tab)
	switch paneCount := len(tab.Panes); paneCount {
	case 0:
		return label + " • empty"
	case 1:
		return label + " • 1 pane"
	default:
		return fmt.Sprintf("%s • %d panes", label, paneCount)
	}
}

func workbenchTabIdentityLabel(tab types.TabState) string {
	if strings.TrimSpace(tab.Name) != "" {
		return tab.Name
	}
	return string(tab.ID)
}

func expandWorkbenchTabClickRange(line string, start int, end int) (int, int) {
	runes := []rune(line)
	if start > 0 && runes[start-1] == '[' {
		start--
	}
	if end < len(runes) && runes[end] == ']' {
		end++
	}
	return start, end
}

func expandWorkbenchPaneClickRange(line string, start int, end int) (int, int) {
	runes := []rune(line)
	if start > 0 && runes[start-1] == ' ' {
		start--
	}
	if end < len(runes) && runes[end] == ' ' {
		end++
	}
	return start, end
}

func activeWorkbenchTabPaneID(tab types.TabState) (types.PaneID, bool) {
	if tab.ActivePaneID != "" {
		if _, ok := tab.Panes[tab.ActivePaneID]; ok {
			return tab.ActivePaneID, true
		}
	}
	ordered := orderedWorkbenchPaneIDs(tab)
	if len(ordered) == 0 {
		return "", false
	}
	return ordered[0], true
}

func orderedWorkbenchPaneIDs(tab types.TabState) []types.PaneID {
	seen := map[types.PaneID]struct{}{}
	ordered := make([]types.PaneID, 0, len(tab.Panes))
	if tab.ActivePaneID != "" {
		if _, ok := tab.Panes[tab.ActivePaneID]; ok {
			ordered = append(ordered, tab.ActivePaneID)
			seen[tab.ActivePaneID] = struct{}{}
		}
	}
	if tab.RootSplit != nil {
		appendWorkbenchPaneIDsFromSplit(tab.RootSplit, seen, &ordered)
	}
	for _, paneID := range tab.FloatingOrder {
		if _, ok := seen[paneID]; ok {
			continue
		}
		if _, ok := tab.Panes[paneID]; !ok {
			continue
		}
		ordered = append(ordered, paneID)
		seen[paneID] = struct{}{}
	}
	for paneID := range tab.Panes {
		if _, ok := seen[paneID]; ok {
			continue
		}
		ordered = append(ordered, paneID)
	}
	return ordered
}

func appendWorkbenchPaneIDsFromSplit(node *types.SplitNode, seen map[types.PaneID]struct{}, ordered *[]types.PaneID) {
	if node == nil {
		return
	}
	if node.First == nil && node.Second == nil {
		if node.PaneID == "" {
			return
		}
		if _, ok := seen[node.PaneID]; ok {
			return
		}
		*ordered = append(*ordered, node.PaneID)
		seen[node.PaneID] = struct{}{}
		return
	}
	appendWorkbenchPaneIDsFromSplit(node.First, seen, ordered)
	appendWorkbenchPaneIDsFromSplit(node.Second, seen, ordered)
}

func workbenchPaneDisplayTitle(state types.AppState, pane types.PaneState) string {
	switch pane.SlotState {
	case types.PaneSlotWaiting:
		return "waiting pane"
	case types.PaneSlotEmpty:
		return "unconnected pane"
	}
	if pane.TerminalID != "" {
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && strings.TrimSpace(terminal.Name) != "" {
			return terminal.Name
		}
	}
	if pane.SlotState == types.PaneSlotExited {
		return "exited pane"
	}
	if pane.ID != "" {
		return string(pane.ID)
	}
	return "pane"
}
