package modal

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type WorkspacePickerItem struct {
	Name        string
	Description string
	CreateNew   bool

	searchTextLower string
	lineBody        string
	lineWidth       int
	lineNormal      string
	lineActive      string
}

type WorkspacePickerState struct {
	Title       string
	Footer      string
	Query       string
	Items       []WorkspacePickerItem
	Filtered    []WorkspacePickerItem
	Selected    int
	RenderWidth int
}

func (p *WorkspacePickerState) SelectedItem() *WorkspacePickerItem {
	items := p.VisibleItems()
	if p == nil || p.Selected < 0 || p.Selected >= len(items) {
		return nil
	}
	return &items[p.Selected]
}

func (p *WorkspacePickerState) Move(delta int) {
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

func (p *WorkspacePickerState) ApplyFilter() {
	if p == nil {
		return
	}
	query := strings.TrimSpace(strings.ToLower(p.Query))
	if query == "" {
		p.Filtered = append([]WorkspacePickerItem(nil), p.Items...)
		if p.Selected >= len(p.Filtered) {
			p.Selected = 0
		}
		return
	}

	filtered := make([]WorkspacePickerItem, 0, len(p.Items))
	for _, item := range p.Items {
		if item.searchTextLower == "" {
			item.primeCaches()
		}
		if strings.Contains(item.searchTextLower, query) {
			filtered = append(filtered, item)
		}
	}
	p.Filtered = filtered
	if p.Selected >= len(p.Filtered) {
		p.Selected = 0
	}
}

func (p *WorkspacePickerState) VisibleItems() []WorkspacePickerItem {
	if p == nil {
		return nil
	}
	if len(p.Filtered) > 0 || strings.TrimSpace(p.Query) != "" {
		return p.Filtered
	}
	return p.Items
}

func (i *WorkspacePickerItem) RenderLine(width int, selected bool, normalStyle lipgloss.Style, activeStyle lipgloss.Style, createStyle lipgloss.Style) string {
	if i == nil || width <= 0 {
		return ""
	}
	if i.searchTextLower == "" {
		i.primeCaches()
	}
	if i.lineWidth != width {
		i.lineWidth = width
		plain := forceWidthANSI(" "+i.lineBody+" ", width)
		if i.CreateNew {
			i.lineNormal = createStyle.Render(plain)
		} else {
			i.lineNormal = normalStyle.Render(plain)
		}
		i.lineActive = activeStyle.Render(plain)
	}
	if selected {
		return i.lineActive
	}
	return i.lineNormal
}

func (i *WorkspacePickerItem) primeCaches() {
	if i == nil {
		return
	}
	i.searchTextLower = strings.ToLower(strings.TrimSpace(i.Name + " " + i.Description))
	i.lineBody = strings.TrimSpace(i.Name + "  " + i.Description)
	i.lineWidth = 0
	i.lineNormal = ""
	i.lineActive = ""
}
