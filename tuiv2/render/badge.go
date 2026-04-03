package render

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

// paneMeta generates the badge/meta string for a pane based on terminal state.
func paneMeta(pane workbench.VisiblePane, runtimeState *VisibleRuntimeStateProxy) string {
	return paneMetaWithLookup(pane, newRuntimeLookup(runtimeState))
}

func paneMetaWithLookup(pane workbench.VisiblePane, lookup runtimeLookup) string {
	if pane.TerminalID == "" {
		return "unconnected"
	}
	terminal := lookup.terminal(pane.TerminalID)
	if terminal == nil {
		return ""
	}
	parts := make([]string, 0, 4)
	switch terminal.State {
	case "running":
		parts = append(parts, "●")
	case "exited":
		if terminal.ExitCode != nil {
			parts = append(parts, fmt.Sprintf("○ %d", *terminal.ExitCode))
		} else {
			parts = append(parts, "○")
		}
	case "waiting":
		parts = append(parts, "…")
	case "killed":
		parts = append(parts, "✕")
	}
	switch role := lookup.paneRole(pane.ID); role {
	case "owner":
		parts = append(parts, "◆")
	case "follower":
		parts = append(parts, "◇")
	}
	if len(terminal.BoundPaneIDs) > 1 {
		parts = append(parts, fmt.Sprintf("⧉ %d", len(terminal.BoundPaneIDs)))
	}
	return strings.Join(parts, " ")
}

func findVisibleTerminal(runtimeState *VisibleRuntimeStateProxy, terminalID string) *runtime.VisibleTerminal {
	return newRuntimeLookup(runtimeState).terminal(terminalID)
}

func paneBindingRole(runtimeState *VisibleRuntimeStateProxy, paneID string) string {
	return newRuntimeLookup(runtimeState).paneRole(paneID)
}
