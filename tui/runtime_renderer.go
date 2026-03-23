package tui

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/tui/domain/types"
)

type runtimeRenderer struct{}

// Render 先提供一个稳定、可测试的文本视图，优先把生命周期打通。
// 这里不追求视觉完成度，只把当前 workspace / tab / pane / overlay 这些主语义明确展示出来。
func (runtimeRenderer) Render(state types.AppState) string {
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
	}
	return strings.Join(lines, "\n")
}
