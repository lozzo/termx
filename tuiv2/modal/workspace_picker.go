package modal

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/lozzow/termx/tuiv2/uiinput"
)

type WorkspacePickerItemKind string

const (
	WorkspacePickerItemWorkspace WorkspacePickerItemKind = "workspace"
	WorkspacePickerItemTab       WorkspacePickerItemKind = "tab"
	WorkspacePickerItemPane      WorkspacePickerItemKind = "pane"
	WorkspacePickerItemCreate    WorkspacePickerItemKind = "create"
)

type WorkspacePickerItem struct {
	Kind           WorkspacePickerItemKind
	Name           string
	Description    string
	CreateNew      bool
	CreateName     string
	Depth          int
	WorkspaceName  string
	TabID          string
	TabIndex       int
	TabName        string
	PaneID         string
	Current        bool
	Active         bool
	Floating       bool
	TabCount       int
	PaneCount      int
	FloatingCount  int
	ActiveTabName  string
	ActivePaneName string
	TerminalID     string
	State          string
	Role           string

	searchTextLower string
	lineBody        string
	lineWidth       int
	lineNormal      string
	lineActive      string
}

type WorkspacePickerState struct {
	Title       string
	Query       string
	Cursor      int
	CursorSet   bool
	QueryInput  uiinput.State
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
	p.Filtered = workspacePickerVisibleItems(p.Items, p.QueryValue())
	if p.Selected >= len(p.Filtered) {
		p.Selected = 0
	}
}

func (p *WorkspacePickerState) VisibleItems() []WorkspacePickerItem {
	if p == nil {
		return nil
	}
	if len(p.Filtered) > 0 || strings.TrimSpace(p.QueryValue()) != "" {
		return p.Filtered
	}
	return workspacePickerVisibleItems(p.Items, "")
}

func (i *WorkspacePickerItem) RenderLine(width int, selected bool, normalStyle lipgloss.Style, activeStyle lipgloss.Style, createStyle lipgloss.Style) string {
	return i.RenderLineWithPrefix(width, selected, "  ", "▸ ", normalStyle, activeStyle, createStyle)
}

func (i *WorkspacePickerItem) RenderLineWithPrefix(width int, selected bool, normalPrefix string, selectedPrefix string, normalStyle lipgloss.Style, activeStyle lipgloss.Style, createStyle lipgloss.Style) string {
	if i == nil || width <= 0 {
		return ""
	}
	if i.searchTextLower == "" {
		i.primeCaches()
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

func (i *WorkspacePickerItem) primeCaches() {
	if i == nil {
		return
	}
	kind := workspacePickerEffectiveKind(*i)
	if i.CreateNew || kind == WorkspacePickerItemCreate {
		label := strings.TrimSpace(i.Name)
		if label == "" {
			if strings.TrimSpace(i.CreateName) != "" {
				label = fmt.Sprintf("Create %q", strings.TrimSpace(i.CreateName))
			} else {
				label = "New workspace"
			}
		}
		desc := strings.TrimSpace(i.Description)
		if desc == "" {
			desc = "create workspace"
		}
		i.searchTextLower = strings.ToLower(strings.TrimSpace(label + " " + desc + " " + i.CreateName))
		i.lineBody = lipgloss.JoinHorizontal(lipgloss.Left, "+", " ", label, "  ", desc)
		i.lineWidth = 0
		i.lineNormal = ""
		i.lineActive = ""
		return
	}

	meta := make([]string, 0, 6)
	if kind == WorkspacePickerItemWorkspace {
		if i.Current {
			meta = append(meta, "current")
		}
		if i.TabCount > 0 {
			meta = append(meta, fmt.Sprintf("tabs:%d", i.TabCount))
		}
		if i.PaneCount > 0 {
			meta = append(meta, fmt.Sprintf("panes:%d", i.PaneCount))
		}
		if i.FloatingCount > 0 {
			meta = append(meta, fmt.Sprintf("float:%d", i.FloatingCount))
		}
		if activeTab := strings.TrimSpace(i.ActiveTabName); activeTab != "" {
			meta = append(meta, "tab:"+activeTab)
		}
	}
	if kind == WorkspacePickerItemTab {
		if i.Active {
			meta = append(meta, "active")
		}
		if i.PaneCount > 0 {
			meta = append(meta, fmt.Sprintf("panes:%d", i.PaneCount))
		}
		if i.FloatingCount > 0 {
			meta = append(meta, fmt.Sprintf("float:%d", i.FloatingCount))
		}
		if activePane := strings.TrimSpace(i.ActivePaneName); activePane != "" {
			meta = append(meta, "pane:"+activePane)
		}
	}
	if kind == WorkspacePickerItemPane {
		if i.Active {
			meta = append(meta, "active")
		}
		if strings.TrimSpace(i.State) != "" {
			meta = append(meta, i.State)
		}
		if strings.TrimSpace(i.Role) != "" {
			meta = append(meta, i.Role)
		}
		if i.Floating {
			meta = append(meta, "floating")
		}
	}
	if desc := strings.TrimSpace(i.Description); desc != "" {
		meta = append(meta, desc)
	}

	parts := []string{workspacePickerWorkspaceName(*i), strings.TrimSpace(i.TabName), strings.TrimSpace(i.Name), strings.TrimSpace(i.ActiveTabName), strings.TrimSpace(i.ActivePaneName), strings.TrimSpace(i.State), strings.TrimSpace(i.Role), strings.Join(meta, " ")}
	i.searchTextLower = strings.ToLower(strings.TrimSpace(strings.Join(parts, " ")))
	line := strings.TrimSpace(i.Name)
	if len(meta) > 0 {
		line = lipgloss.JoinHorizontal(lipgloss.Left, line, "  ", strings.Join(meta, "  "))
	}
	i.lineBody = line
	i.lineWidth = 0
	i.lineNormal = ""
	i.lineActive = ""
}

func workspacePickerVisibleItems(items []WorkspacePickerItem, query string) []WorkspacePickerItem {
	base := workspacePickerBaseItems(items)
	trimmed := strings.TrimSpace(query)
	if len(base) == 0 && trimmed == "" {
		return nil
	}
	if trimmed == "" {
		out := append([]WorkspacePickerItem(nil), base...)
		if create, ok := workspacePickerCreateItem(base, ""); ok {
			out = append(out, create)
		}
		return out
	}

	queryLower := strings.ToLower(trimmed)
	directWorkspaceMatch := make(map[string]bool)
	directTabMatch := make(map[string]bool)
	showWorkspace := make(map[string]bool)
	showTab := make(map[string]bool)
	showPane := make(map[string]bool)
	for _, item := range base {
		if item.searchTextLower == "" {
			item.primeCaches()
		}
		matched := strings.Contains(item.searchTextLower, queryLower)
		switch workspacePickerEffectiveKind(item) {
		case WorkspacePickerItemWorkspace:
			if matched {
				workspace := workspacePickerWorkspaceName(item)
				directWorkspaceMatch[workspace] = true
				showWorkspace[workspace] = true
			}
		case WorkspacePickerItemTab:
			workspaceName := workspacePickerWorkspaceName(item)
			key := workspacePickerTabKey(workspaceName, item.TabID)
			if matched {
				directTabMatch[key] = true
				showWorkspace[workspaceName] = true
				showTab[key] = true
			}
		case WorkspacePickerItemPane:
			workspaceName := workspacePickerWorkspaceName(item)
			tabKey := workspacePickerTabKey(workspaceName, item.TabID)
			paneKey := workspacePickerPaneKey(workspaceName, item.TabID, item.PaneID)
			if matched {
				showWorkspace[workspaceName] = true
				showTab[tabKey] = true
				showPane[paneKey] = true
			}
		}
	}

	filtered := make([]WorkspacePickerItem, 0, len(base)+1)
	for _, item := range base {
		switch workspacePickerEffectiveKind(item) {
		case WorkspacePickerItemWorkspace:
			if showWorkspace[workspacePickerWorkspaceName(item)] {
				filtered = append(filtered, item)
			}
		case WorkspacePickerItemTab:
			workspaceName := workspacePickerWorkspaceName(item)
			key := workspacePickerTabKey(workspaceName, item.TabID)
			if showWorkspace[workspaceName] {
				if directWorkspaceMatch[workspaceName] || showTab[key] {
					filtered = append(filtered, item)
				}
			}
		case WorkspacePickerItemPane:
			workspaceName := workspacePickerWorkspaceName(item)
			tabKey := workspacePickerTabKey(workspaceName, item.TabID)
			paneKey := workspacePickerPaneKey(workspaceName, item.TabID, item.PaneID)
			if showWorkspace[workspaceName] {
				if directWorkspaceMatch[workspaceName] || directTabMatch[tabKey] || showPane[paneKey] {
					filtered = append(filtered, item)
				}
			}
		}
	}
	if create, ok := workspacePickerCreateItem(base, query); ok {
		filtered = append(filtered, create)
	}
	return filtered
}

func workspacePickerBaseItems(items []WorkspacePickerItem) []WorkspacePickerItem {
	base := make([]WorkspacePickerItem, 0, len(items))
	for _, item := range items {
		if item.CreateNew || item.Kind == WorkspacePickerItemCreate {
			continue
		}
		base = append(base, item)
	}
	return base
}

func workspacePickerCreateItem(items []WorkspacePickerItem, query string) (WorkspacePickerItem, bool) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		if len(items) == 0 {
			return WorkspacePickerItem{}, false
		}
		return WorkspacePickerItem{
			Kind:        WorkspacePickerItemCreate,
			Name:        "New workspace",
			Description: "create workspace",
			CreateNew:   true,
		}, true
	}
	for _, item := range items {
		if workspacePickerEffectiveKind(item) != WorkspacePickerItemWorkspace {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(workspacePickerWorkspaceName(item)), trimmed) {
			return WorkspacePickerItem{}, false
		}
	}
	return WorkspacePickerItem{
		Kind:        WorkspacePickerItemCreate,
		Name:        fmt.Sprintf("Create %q", trimmed),
		Description: "new workspace",
		CreateNew:   true,
		CreateName:  trimmed,
	}, true
}

func workspacePickerTabKey(workspaceName, tabID string) string {
	return strings.TrimSpace(workspaceName) + "::" + strings.TrimSpace(tabID)
}

func workspacePickerWorkspaceName(item WorkspacePickerItem) string {
	name := strings.TrimSpace(item.WorkspaceName)
	if name != "" {
		return name
	}
	if workspacePickerEffectiveKind(item) == WorkspacePickerItemWorkspace {
		return strings.TrimSpace(item.Name)
	}
	return ""
}

func workspacePickerEffectiveKind(item WorkspacePickerItem) WorkspacePickerItemKind {
	switch {
	case item.CreateNew:
		return WorkspacePickerItemCreate
	case item.Kind != "":
		return item.Kind
	case strings.TrimSpace(item.PaneID) != "":
		return WorkspacePickerItemPane
	case strings.TrimSpace(item.TabID) != "":
		return WorkspacePickerItemTab
	default:
		return WorkspacePickerItemWorkspace
	}
}

func workspacePickerPaneKey(workspaceName, tabID, paneID string) string {
	return strings.TrimSpace(workspaceName) + "::" + strings.TrimSpace(tabID) + "::" + strings.TrimSpace(paneID)
}
