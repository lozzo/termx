package modal

import (
	"strings"

	"github.com/lozzow/termx/tuiv2/uiinput"
)

// TerminalManagerState 保存 terminal manager modal 的 UI 状态。
type TerminalManagerState struct {
	Title      string
	Items      []PickerItem
	Filtered   []PickerItem
	Selected   int
	Query      string
	Cursor     int
	CursorSet  bool
	QueryInput uiinput.State
}

func (m *TerminalManagerState) SelectedItem() *PickerItem {
	items := m.VisibleItems()
	if m == nil || m.Selected < 0 || m.Selected >= len(items) {
		return nil
	}
	return &items[m.Selected]
}

func (m *TerminalManagerState) Move(delta int) {
	items := m.VisibleItems()
	if m == nil || len(items) == 0 || delta == 0 {
		return
	}
	next := m.Selected + delta
	if next < 0 {
		next = 0
	}
	if next >= len(items) {
		next = len(items) - 1
	}
	m.Selected = next
}

func (m *TerminalManagerState) ApplyFilter() {
	if m == nil {
		return
	}
	query := strings.ToLower(strings.TrimSpace(m.QueryValue()))
	if query == "" {
		m.Filtered = append([]PickerItem(nil), m.Items...)
		if m.Selected >= len(m.Filtered) {
			m.Selected = 0
		}
		return
	}
	filtered := make([]PickerItem, 0, len(m.Items))
	for _, item := range m.Items {
		search := strings.ToLower(strings.Join([]string{item.TerminalID, item.Name, item.Command, item.Location, item.State, item.TerminalState, item.Description}, " "))
		if strings.Contains(search, query) {
			filtered = append(filtered, item)
		}
	}
	m.Filtered = filtered
	if m.Selected >= len(m.Filtered) {
		m.Selected = 0
	}
}

func (m *TerminalManagerState) VisibleItems() []PickerItem {
	if m == nil {
		return nil
	}
	if len(m.Filtered) > 0 || strings.TrimSpace(m.QueryValue()) != "" {
		return m.Filtered
	}
	return m.Items
}
