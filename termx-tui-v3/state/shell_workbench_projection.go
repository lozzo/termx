package state

import (
	"fmt"
	"strings"
)

func WorkbenchTreeItems(root Root) []WorkbenchTreeItem {
	shell := root.Shell.EnsureDefaults()
	query := strings.ToLower(strings.TrimSpace(shell.Overlay.Query))
	workspaces := shell.Workspaces
	if len(workspaces) == 0 {
		workspaces = []WorkspaceState{shell.Workspace}
	}
	items := make([]WorkbenchTreeItem, 0, len(workspaces)*4)
	appendItem := func(item WorkbenchTreeItem) {
		if matchesWorkbenchTreeQuery(item, query) {
			items = append(items, item)
		}
	}

	for _, workspace := range workspaces {
		workspace = workspace.ensureDefaults()
		workspaceActive := workspace.ID == shell.Workspace.ID
		appendItem(WorkbenchTreeItem{
			Kind:          WorkbenchTreeKindWorkspace,
			WorkspaceID:   workspace.ID,
			WorkspaceName: workspace.Name,
			Depth:         0,
			Active:        workspaceActive,
			Summary:       workbenchWorkspaceSummary(workspace),
		})
		for _, tab := range workspace.Tabs {
			tabActive := workspaceActive && tab.ID == workspace.ActiveTabID
			appendItem(WorkbenchTreeItem{
				Kind:          WorkbenchTreeKindTab,
				WorkspaceID:   workspace.ID,
				WorkspaceName: workspace.Name,
				TabID:         tab.ID,
				TabTitle:      tab.Title,
				PaneID:        tab.ActivePaneID,
				Depth:         1,
				Active:        tabActive,
				Summary:       workbenchTabSummary(tab),
			})
			for _, pane := range tab.Panes {
				terminalID := workbenchPaneTerminalID(root, pane)
				displayTitle := ""
				if pane.Kind == PaneTerminalLive && terminalID != "" {
					displayTitle = workbenchTerminalTitle(root.TerminalPool, terminalID)
				}
				appendItem(WorkbenchTreeItem{
					Kind:          WorkbenchTreeKindPane,
					WorkspaceID:   workspace.ID,
					WorkspaceName: workspace.Name,
					TabID:         tab.ID,
					TabTitle:      tab.Title,
					PaneID:        pane.ID,
					PaneTitle:     paneTitle(pane),
					DisplayTitle:  displayTitle,
					PaneKind:      pane.Kind,
					TerminalID:    terminalID,
					Depth:         2,
					Active:        tabActive && pane.ID == shell.ActivePaneID,
					Summary:       workbenchPaneSummary(pane, terminalID),
				})
			}
			for _, floating := range tab.Floatings {
				pane := floating.Pane
				terminalID := workbenchFloatingTerminalID(root, floating)
				displayTitle := ""
				if terminalID != "" {
					displayTitle = workbenchTerminalTitle(root.TerminalPool, terminalID)
				}
				appendItem(WorkbenchTreeItem{
					Kind:          WorkbenchTreeKindFloating,
					WorkspaceID:   workspace.ID,
					WorkspaceName: workspace.Name,
					TabID:         tab.ID,
					TabTitle:      tab.Title,
					FloatingID:    floating.ID,
					FloatingTitle: floating.Title,
					PaneID:        pane.ID,
					PaneTitle:     paneTitle(pane),
					DisplayTitle:  displayTitle,
					PaneKind:      pane.Kind,
					TerminalID:    terminalID,
					Depth:         2,
					Active:        tabActive && floating.Active,
					Summary:       workbenchFloatingSummary(floating, terminalID),
				})
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

func workbenchFloatingTerminalID(root Root, floating FloatingPaneState) string {
	if binding, ok := root.TerminalViews.FloatingBinding(floating.ID); ok && binding.TerminalID != "" {
		return binding.TerminalID
	}
	return workbenchPaneTerminalID(root, floating.Pane)
}

func workbenchPaneTerminalID(root Root, pane PaneState) string {
	if binding, ok := root.TerminalViews.PaneBinding(pane.ID); ok && binding.TerminalID != "" {
		return binding.TerminalID
	}
	if pane.TerminalID != "" {
		return pane.TerminalID
	}
	return ""
}

func pickerTerminalID(root Root, pane PaneState) string {
	if pane.TerminalID != "" {
		return pane.TerminalID
	}
	if pane.Active && root.Session.TerminalID != "" {
		return root.Session.TerminalID
	}
	if pane.Active && root.Surface.TerminalID != "" {
		return root.Surface.TerminalID
	}
	if pane.Active && root.History.TerminalID != "" {
		return root.History.TerminalID
	}
	return ""
}

func workbenchTerminalTitle(pool TerminalPoolStore, terminalID string) string {
	for _, item := range pool.Items {
		if item.TerminalID != terminalID {
			continue
		}
		return terminalPoolTitle(item)
	}
	return terminalID
}

func matchesWorkbenchTreeQuery(item WorkbenchTreeItem, query string) bool {
	if query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(item.Kind), query) ||
		strings.Contains(strings.ToLower(item.WorkspaceName), query) ||
		strings.Contains(strings.ToLower(item.WorkspaceID), query) ||
		strings.Contains(strings.ToLower(item.TabTitle), query) ||
		strings.Contains(strings.ToLower(item.TabID), query) ||
		strings.Contains(strings.ToLower(item.FloatingTitle), query) ||
		strings.Contains(strings.ToLower(item.FloatingID), query) ||
		strings.Contains(strings.ToLower(item.PaneTitle), query) ||
		strings.Contains(strings.ToLower(item.DisplayTitle), query) ||
		strings.Contains(strings.ToLower(item.PaneID), query) ||
		strings.Contains(strings.ToLower(string(item.PaneKind)), query) ||
		strings.Contains(strings.ToLower(item.TerminalID), query) ||
		strings.Contains(strings.ToLower(item.Summary), query)
}

func workbenchWorkspaceSummary(workspace WorkspaceState) string {
	return fmt.Sprintf("tabs:%d panes:%d", len(workspace.Tabs), workspacePaneCount(workspace))
}

func workbenchTabSummary(tab TabState) string {
	if len(tab.Floatings) == 0 {
		return fmt.Sprintf("panes:%d active:%s", len(tab.Panes), tab.ActivePaneID)
	}
	return fmt.Sprintf("panes:%d floating:%d active:%s", len(tab.Panes), len(tab.Floatings), tab.ActivePaneID)
}

func workbenchPaneSummary(pane PaneState, terminalID string) string {
	summary := string(pane.Kind)
	if terminalID != "" {
		summary += " term:" + terminalID
	}
	return summary
}

func workbenchFloatingSummary(floating FloatingPaneState, terminalID string) string {
	tags := []string{"floating"}
	if floating.Pane.Kind != "" {
		tags = append(tags, string(floating.Pane.Kind))
	}
	if floating.Collapsed {
		tags = append(tags, "collapsed")
	} else {
		tags = append(tags, "open")
	}
	if floating.FitMode == FloatingFitAuto {
		tags = append(tags, "auto-fit")
	} else {
		tags = append(tags, "manual")
	}
	if terminalID != "" {
		tags = append(tags, "term:"+terminalID)
	}
	return strings.Join(tags, " ")
}

func workspacePaneCount(workspace WorkspaceState) int {
	count := 0
	for _, tab := range workspace.Tabs {
		count += len(tab.Panes)
	}
	return count
}
