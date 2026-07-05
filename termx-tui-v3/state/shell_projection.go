package state

import (
	"strconv"
	"strings"
)

// TerminalPickerItems 从 reducer-owned root 推导 picker 列表；服务端 Terminal Pool 必须先回投到 TerminalPoolStore。
func TerminalPickerItems(root Root) []TerminalPickerItem {
	shell := root.Shell.ReadonlyDefaults()
	query := strings.ToLower(strings.TrimSpace(shell.Overlay.Query))
	items := []TerminalPickerItem{}
	createItem := TerminalPickerItem{Title: "new terminal", Kind: PaneTerminalLive, CreateNew: true}
	if matchesTerminalPickerQuery(createItem, query) {
		items = append(items, createItem)
	}
	seenTerminal := map[string]struct{}{}
	for _, poolItem := range root.TerminalPool.Items {
		if poolItem.TerminalID == "" {
			continue
		}
		item := TerminalPickerItem{
			Title:      terminalPoolTitle(poolItem),
			Kind:       PaneTerminalLive,
			TerminalID: poolItem.TerminalID,
			Active:     poolItem.Attached,
			FromPool:   true,
			PoolState:  poolItem.State,
			Cols:       poolItem.Cols,
			Rows:       poolItem.Rows,
		}
		if !matchesTerminalPickerQuery(item, query) {
			continue
		}
		items = append(items, item)
		seenTerminal[poolItem.TerminalID] = struct{}{}
	}
	for _, binding := range root.TerminalViews.Bindings() {
		if binding.TerminalID == "" {
			continue
		}
		if _, seen := seenTerminal[binding.TerminalID]; seen {
			continue
		}
		item := terminalPickerItemFromBinding(root, binding)
		if !matchesTerminalPickerQuery(item, query) {
			continue
		}
		items = append(items, item)
		seenTerminal[binding.TerminalID] = struct{}{}
	}
	if root.Session.TerminalID != "" {
		if _, seen := seenTerminal[root.Session.TerminalID]; !seen {
			item := terminalPickerItemFromSession(root)
			if matchesTerminalPickerQuery(item, query) {
				items = append(items, item)
			}
		}
	}
	if len(items) > 0 {
		selected := shell.Overlay.SelectedIndex
		if selected < 0 {
			selected = 0
		}
		if selected >= len(items) {
			selected = len(items) - 1
		}
		items[selected].Selected = true
	}
	return items
}

func terminalPickerItemFromBinding(root Root, binding TerminalViewBinding) TerminalPickerItem {
	surface := root.Surface.SurfaceForTerminal(binding.TerminalID)
	stateText := string(surface.State)
	if stateText == "" || stateText == string(TerminalLivePending) {
		stateText = string(root.Session.State)
	}
	cols := binding.DesiredCols
	rows := binding.DesiredRows
	if cols <= 0 {
		cols = surface.Cols
	}
	if rows <= 0 {
		rows = surface.Rows
	}
	return TerminalPickerItem{Title: binding.TerminalID, Kind: PaneTerminalLive, TerminalID: binding.TerminalID, Active: binding.Attached, PoolState: stateText, Cols: cols, Rows: rows}
}

func terminalPickerItemFromSession(root Root) TerminalPickerItem {
	surface := root.Surface.SurfaceForTerminal(root.Session.TerminalID)
	stateText := string(surface.State)
	if stateText == "" || stateText == string(TerminalLivePending) {
		stateText = string(root.Session.State)
	}
	return TerminalPickerItem{Title: root.Session.TerminalID, Kind: PaneTerminalLive, TerminalID: root.Session.TerminalID, Active: root.Session.Attached, PoolState: stateText, Cols: root.Session.Cols, Rows: root.Session.Rows}
}

func TerminalPoolPageItems(root Root) []TerminalPoolPageItem {
	shell := root.Shell.ReadonlyDefaults()
	query := strings.ToLower(strings.TrimSpace(shell.Overlay.Query))
	items := make([]TerminalPoolPageItem, 0, len(root.TerminalPool.Items))
	for _, poolItem := range root.TerminalPool.Items {
		if poolItem.TerminalID == "" {
			continue
		}
		item := TerminalPoolPageItem{
			TerminalID:      poolItem.TerminalID,
			Title:           terminalPoolTitle(poolItem),
			State:           poolItem.State,
			CWD:             poolItem.CWD,
			Command:         append([]string(nil), poolItem.Command...),
			Tags:            cloneStringMap(poolItem.Tags),
			ExitCode:        cloneIntPointer(poolItem.ExitCode),
			ExitedAt:        poolItem.ExitedAt,
			Cols:            poolItem.Cols,
			Rows:            poolItem.Rows,
			AttachmentCount: poolItem.AttachmentCount,
			Attached:        poolItem.Attached || poolItem.AttachmentCount > 0,
		}
		if !matchesTerminalPoolPageQuery(item, query) {
			continue
		}
		items = append(items, item)
	}
	if len(items) > 0 {
		selected := shell.Overlay.SelectedIndex
		if selected < 0 {
			selected = 0
		}
		if selected >= len(items) {
			selected = len(items) - 1
		}
		items[selected].Selected = true
	}
	return items
}

func terminalPoolPickerLocation() string {
	return "pool"
}

func matchesTerminalPickerQuery(item TerminalPickerItem, query string) bool {
	if query == "" {
		return true
	}
	if item.CreateNew {
		return TerminalPickerQueryMatchIndexes(item.Title, query) != nil ||
			TerminalPickerQueryMatchIndexes("create terminal", query) != nil ||
			TerminalPickerQueryMatchIndexes("new terminal", query) != nil
	}
	return TerminalPickerQueryMatchIndexes(item.Title, query) != nil ||
		TerminalPickerQueryMatchIndexes(item.TerminalID, query) != nil ||
		TerminalPickerQueryMatchIndexes(item.PoolState, query) != nil ||
		TerminalPickerQueryMatchIndexes(terminalPickerSizeText(item), query) != nil
}

func terminalPickerSizeText(item TerminalPickerItem) string {
	if item.Cols <= 0 || item.Rows <= 0 {
		return ""
	}
	return strconv.Itoa(item.Cols) + "x" + strconv.Itoa(item.Rows)
}

func TerminalPickerQueryMatchIndexes(value string, query string) []int {
	query = strings.TrimSpace(query)
	if query == "" {
		return []int{}
	}
	valueRunes := []rune(value)
	queryRunes := []rune(query)
	matches := make([]int, 0, len(queryRunes))
	valueAt := 0
	for _, queryRune := range queryRunes {
		queryLower := []rune(strings.ToLower(string(queryRune)))
		if len(queryLower) == 0 {
			continue
		}
		found := false
		for valueAt < len(valueRunes) {
			valueLower := []rune(strings.ToLower(string(valueRunes[valueAt])))
			if len(valueLower) > 0 && valueLower[0] == queryLower[0] {
				matches = append(matches, valueAt)
				valueAt++
				found = true
				break
			}
			valueAt++
		}
		if !found {
			return nil
		}
	}
	return matches
}

func matchesTerminalPoolPageQuery(item TerminalPoolPageItem, query string) bool {
	if query == "" {
		return true
	}
	if strings.Contains(strings.ToLower(item.Title), query) ||
		strings.Contains(strings.ToLower(item.TerminalID), query) ||
		strings.Contains(strings.ToLower(item.State), query) ||
		strings.Contains(strings.ToLower(item.CWD), query) ||
		strings.Contains(strings.ToLower(strings.Join(item.Command, " ")), query) {
		return true
	}
	for key, value := range item.Tags {
		if strings.Contains(strings.ToLower(key), query) || strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}
