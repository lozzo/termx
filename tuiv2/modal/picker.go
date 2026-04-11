package modal

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// PickerItem 是 picker 列表中的一条终端记录。
type PickerItem struct {
	TerminalID    string
	Name          string
	State         string
	TerminalState string
	ExitCode      *int
	Command       string
	CommandArgs   []string
	Tags          map[string]string
	Location      string
	Observed      bool
	Orphan        bool
	CreateNew     bool
	Description   string
	CreatedAt     time.Time

	lineBody   string
	lineWidth  int
	lineNormal string
	lineActive string
}

// PickerState 保存 picker modal 的全部 UI 状态。
type PickerState struct {
	Title     string
	Footer    string
	Items     []PickerItem
	Filtered  []PickerItem
	Selected  int
	Query     string
	Cursor    int
	CursorSet bool
}

func (p *PickerState) SelectedItem() *PickerItem {
	items := p.VisibleItems()
	if p == nil || p.Selected < 0 || p.Selected >= len(items) {
		return nil
	}
	return &items[p.Selected]
}

func (p *PickerState) Move(delta int) {
	items := p.VisibleItems()
	if p == nil || len(items) == 0 || delta == 0 {
		return
	}
	next := p.Selected + delta
	if next < 0 {
		next = 0
	}
	if next >= len(items) {
		next = len(items) - 1
	}
	p.Selected = next
}

func (p *PickerState) ApplyFilter() {
	if p == nil {
		return
	}
	query := strings.ToLower(strings.TrimSpace(p.Query))
	if query == "" {
		p.Filtered = pickerItemsCreateFirst(append([]PickerItem(nil), p.Items...))
		if p.Selected >= len(p.Filtered) {
			p.Selected = 0
		}
		return
	}
	filtered := make([]PickerItem, 0, len(p.Items))
	for _, item := range p.Items {
		search := strings.ToLower(strings.Join([]string{item.TerminalID, item.Name, item.Command, item.Location, item.State, item.TerminalState, item.Description}, " "))
		if strings.Contains(search, query) {
			filtered = append(filtered, item)
		}
	}
	p.Filtered = pickerItemsCreateFirst(filtered)
	if p.Selected >= len(p.Filtered) {
		p.Selected = 0
	}
}

func (p *PickerState) VisibleItems() []PickerItem {
	if p == nil {
		return nil
	}
	if len(p.Filtered) > 0 || strings.TrimSpace(p.Query) != "" {
		return p.Filtered
	}
	return pickerItemsCreateFirst(p.Items)
}

func (i *PickerItem) RenderLine(width int, selected bool, normalStyle lipgloss.Style, activeStyle lipgloss.Style, createStyle lipgloss.Style) string {
	return i.RenderLineWithPrefix(width, selected, " ", " ", normalStyle, activeStyle, createStyle)
}

func (i *PickerItem) RenderLineWithPrefix(width int, selected bool, normalPrefix string, selectedPrefix string, normalStyle lipgloss.Style, activeStyle lipgloss.Style, createStyle lipgloss.Style) string {
	if i == nil || width <= 0 {
		return ""
	}
	if i.lineBody == "" {
		i.lineBody = i.renderBody()
	}
	if normalPrefix == " " && selectedPrefix == " " && i.lineWidth != width {
		i.lineWidth = width
		plain := lipgloss.JoinHorizontal(lipgloss.Left, " ", i.lineBody, " ")
		if i.CreateNew {
			i.lineNormal = renderPickerLine(createStyle, plain, width)
		} else {
			i.lineNormal = renderPickerLine(normalStyle, plain, width)
		}
		i.lineActive = renderPickerLine(activeStyle, plain, width)
	}
	if normalPrefix != " " || selectedPrefix != " " {
		prefix := normalPrefix
		if selected {
			prefix = selectedPrefix
		}
		plain := lipgloss.JoinHorizontal(lipgloss.Left, prefix, i.lineBody, " ")
		if selected {
			return renderPickerLine(activeStyle, plain, width)
		}
		if i.CreateNew {
			return renderPickerLine(createStyle, plain, width)
		}
		return renderPickerLine(normalStyle, plain, width)
	}
	if selected {
		return i.lineActive
	}
	return i.lineNormal
}

func renderPickerLine(style lipgloss.Style, plain string, width int) string {
	return style.Render(forceWidthANSI(plain, width))
}

func pickerItemsCreateFirst(items []PickerItem) []PickerItem {
	if len(items) == 0 {
		return items
	}
	createIndex := -1
	for index := range items {
		if items[index].CreateNew {
			createIndex = index
			break
		}
	}
	if createIndex <= 0 {
		return items
	}
	ordered := make([]PickerItem, 0, len(items))
	ordered = append(ordered, items[createIndex])
	ordered = append(ordered, items[:createIndex]...)
	ordered = append(ordered, items[createIndex+1:]...)
	return ordered
}

func (i *PickerItem) renderBody() string {
	if i == nil {
		return ""
	}
	if i.CreateNew {
		return lipgloss.JoinHorizontal(
			lipgloss.Left,
			"+",
			" ",
			coalesce(i.Name, "new terminal"),
			"  ",
			coalesce(i.Description, "Create a new terminal"),
		)
	}
	marker := "○"
	if i.Observed {
		marker = "●"
	}
	label := coalesce(strings.TrimSpace(i.Name), coalesce(strings.TrimSpace(i.Command), "terminal"))
	state := coalesce(strings.TrimSpace(i.State), "running")
	idPart := strings.TrimSpace(i.TerminalID)
	if idPart != "" {
		idPart += " "
	}
	location := ""
	if strings.TrimSpace(i.Location) != "" {
		location = " @" + i.Location
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, marker, " ", idPart, label, "  ", state, location)
}

func forceWidthANSI(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) >= width {
		return lipgloss.NewStyle().MaxWidth(width).Render(s)
	}
	return s + strings.Repeat(" ", width-lipgloss.Width(s))
}

func coalesce(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
