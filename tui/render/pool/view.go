package pool

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	overlayview "github.com/lozzow/termx/tui/render/overlay"
	statepool "github.com/lozzow/termx/tui/state/pool"
	stateterminal "github.com/lozzow/termx/tui/state/terminal"
	"github.com/lozzow/termx/tui/state/types"
)

type TerminalGroup struct {
	Visible []TerminalGroupItem
	Parked  []TerminalGroupItem
	Exited  []TerminalGroupItem
}

type TerminalGroupItem struct {
	TerminalID types.TerminalID
	Metadata   stateterminal.Metadata
}

func Render(model app.Model, width, height int) string {
	_ = width
	_ = height

	grouped := GroupTerminals(model)
	lines := []string{
		renderHeader(model),
		renderColumns(model, grouped),
		" <enter> OPEN HERE  <t> NEW TAB  <o> FLOAT  <e> EDIT  <k> KILL  <d> REMOVE  <esc> BACK",
	}
	if model.Overlay.HasActive() {
		lines = append(lines, overlayview.Render(model, width, height))
	}
	return strings.Join(lines, "\n")
}

func GroupTerminals(model app.Model) TerminalGroup {
	grouped := TerminalGroup{}
	items := statepool.BuildConnectItems(filterPoolTerminals(model))
	for _, item := range items {
		meta, ok := model.Terminals[item.TerminalID]
		if !ok {
			continue
		}
		groupItem := TerminalGroupItem{TerminalID: item.TerminalID, Metadata: meta}
		switch {
		case meta.State == stateterminal.StateExited:
			grouped.Exited = append(grouped.Exited, groupItem)
		case terminalVisibleInActiveTab(model, item.TerminalID):
			grouped.Visible = append(grouped.Visible, groupItem)
		default:
			grouped.Parked = append(grouped.Parked, groupItem)
		}
	}
	return grouped
}

func renderHeader(model app.Model) string {
	workspaceName := "main"
	if model.Workspace != nil && model.Workspace.ID != "" {
		workspaceName = string(model.Workspace.ID)
	}
	return fmt.Sprintf("termx  [%s]  [Pool]", workspaceName)
}

func renderColumns(model app.Model, grouped TerminalGroup) string {
	left := []string{"TERMINALS"}
	if model.Pool.Query != "" {
		left = append(left, fmt.Sprintf("query: %s", model.Pool.Query))
	}
	left = append(left, renderGroup("VISIBLE", grouped.Visible, model)...)
	left = append(left, renderGroup("PARKED", grouped.Parked, model)...)
	left = append(left, renderGroup("EXITED", grouped.Exited, model)...)
	left = append(left, "filter by name tags command")

	middle := []string{"LIVE PREVIEW"}
	if previewID := model.Pool.PreviewTerminalID; previewID != "" {
		middle = append(middle, terminalName(model, previewID))
		middle = append(middle, snapshotLines(model, previewID)...)
	}
	middle = append(middle, "read-only live observe")

	right := []string{"DETAILS", "metadata first"}
	if meta, ok := model.Terminals[model.Pool.SelectedTerminalID]; ok {
		right = append(right,
			fmt.Sprintf("name: %s", meta.Name),
			fmt.Sprintf("state: %s", meta.State),
			fmt.Sprintf("owner: %s", ownerSummary(meta)),
			fmt.Sprintf("tags: %s", tagsSummary(meta.Tags)),
			fmt.Sprintf("cmd: %s", strings.Join(meta.Command, " ")),
			"",
			"connections",
		)
		for _, paneID := range meta.AttachedPaneIDs {
			right = append(right, fmt.Sprintf("- %s", paneID))
		}
	}

	return strings.Join([]string{
		strings.Join(left, "\n"),
		strings.Join(middle, "\n"),
		strings.Join(right, "\n"),
	}, "\n---\n")
}

func renderGroup(title string, items []TerminalGroupItem, model app.Model) []string {
	lines := []string{title}
	for _, item := range items {
		prefix := "  "
		if model.Pool.SelectedTerminalID == item.TerminalID {
			prefix = "> "
		}
		lines = append(lines, prefix+renderItem(item))
	}
	return lines
}

func renderItem(item TerminalGroupItem) string {
	icon := "●"
	if item.Metadata.State == stateterminal.StateExited {
		icon = "○"
	}
	return fmt.Sprintf("%s %s  %s  shown:%d", icon, item.Metadata.Name, tagsInline(item.Metadata.Tags), len(item.Metadata.AttachedPaneIDs))
}

func filterPoolTerminals(model app.Model) map[types.TerminalID]stateterminal.Metadata {
	filtered := make(map[types.TerminalID]stateterminal.Metadata)
	needle := strings.ToLower(strings.TrimSpace(model.Pool.Query))
	for id, meta := range model.Terminals {
		if needle == "" || matches(meta, needle) {
			filtered[id] = meta
		}
	}
	return filtered
}

func matches(meta stateterminal.Metadata, needle string) bool {
	if strings.Contains(strings.ToLower(meta.Name), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(strings.Join(meta.Command, " ")), needle) {
		return true
	}
	for key, value := range meta.Tags {
		if strings.Contains(strings.ToLower(key), needle) || strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}
	return false
}

func terminalVisibleInActiveTab(model app.Model, terminalID types.TerminalID) bool {
	if model.Workspace == nil || model.Workspace.ActiveTab() == nil {
		return false
	}
	for _, pane := range model.Workspace.ActiveTab().Panes {
		if pane.TerminalID == terminalID {
			return true
		}
	}
	return false
}

func terminalName(model app.Model, terminalID types.TerminalID) string {
	meta, ok := model.Terminals[terminalID]
	if !ok || meta.Name == "" {
		return "shell"
	}
	return meta.Name
}

func snapshotLines(model app.Model, terminalID types.TerminalID) []string {
	session, ok := model.Sessions[terminalID]
	if !ok || session.Snapshot == nil {
		return []string{"$"}
	}
	return flattenScreen(session.Snapshot)
}

func flattenScreen(snapshot *protocol.Snapshot) []string {
	if snapshot == nil || len(snapshot.Screen.Cells) == 0 {
		return []string{"$"}
	}
	lines := make([]string, 0, len(snapshot.Screen.Cells))
	for _, row := range snapshot.Screen.Cells {
		var b strings.Builder
		for _, cell := range row {
			if cell.Content == "" {
				continue
			}
			b.WriteString(cell.Content)
		}
		lines = append(lines, b.String())
	}
	if len(lines) == 0 {
		return []string{"$"}
	}
	return lines
}

func ownerSummary(meta stateterminal.Metadata) string {
	if meta.OwnerPaneID == "" {
		return "none"
	}
	return string(meta.OwnerPaneID)
}

func tagsSummary(tags map[string]string) string {
	values := collectTagValues(tags)
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ",")
}

func tagsInline(tags map[string]string) string {
	values := collectTagValues(tags)
	if len(values) == 0 {
		return ""
	}
	prefixed := make([]string, 0, len(values))
	for _, value := range values {
		prefixed = append(prefixed, "#"+value)
	}
	return strings.Join(prefixed, " ")
}

func collectTagValues(tags map[string]string) []string {
	values := make([]string, 0, len(tags))
	for _, value := range tags {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}
