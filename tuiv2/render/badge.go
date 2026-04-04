package render

import (
	"fmt"
	"strings"

	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

const ownerConfirmLabel = "◆ owner?"

const (
	paneBorderStateSlotWidth = 3
	paneBorderShareSlotWidth = 4
	paneBorderRoleSlotWidth  = 10
)

type paneBorderInfo struct {
	StateLabel string
	ShareLabel string
	RoleLabel  string
}

// paneMeta generates the badge/meta string for a pane based on terminal state.
func paneMeta(pane workbench.VisiblePane, runtimeState *VisibleRuntimeStateProxy) string {
	return paneMetaWithLookup(pane, newRuntimeLookup(runtimeState), "")
}

func paneMetaWithLookup(pane workbench.VisiblePane, lookup runtimeLookup, confirmPaneID string) string {
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
		parts = append(parts, "owner")
	case "follower":
		if confirmPaneID == pane.ID {
			parts = append(parts, ownerConfirmLabel)
		} else if terminal.OwnerPaneID != "" && terminal.OwnerPaneID != pane.ID {
			parts = append(parts, "follow:"+terminal.OwnerPaneID)
		} else {
			parts = append(parts, "follower")
		}
	}
	if len(terminal.BoundPaneIDs) > 1 {
		parts = append(parts, fmt.Sprintf("⧉ %d", len(terminal.BoundPaneIDs)))
	}
	return strings.Join(parts, " ")
}

func paneBorderInfoWithLookup(pane workbench.VisiblePane, lookup runtimeLookup, confirmPaneID string) paneBorderInfo {
	if pane.TerminalID == "" {
		return paneBorderInfo{}
	}
	terminal := lookup.terminal(pane.TerminalID)
	if terminal == nil {
		return paneBorderInfo{}
	}
	info := paneBorderInfo{
		StateLabel: paneBorderStateLabel(terminal.State, terminal.ExitCode),
	}
	switch lookup.paneRole(pane.ID) {
	case "owner":
		info.RoleLabel = "◆ owner"
	case "follower":
		if confirmPaneID == pane.ID {
			info.RoleLabel = ownerConfirmLabel
		} else {
			info.RoleLabel = "◇ follow"
		}
	}
	if len(terminal.BoundPaneIDs) > 1 {
		info.ShareLabel = fmt.Sprintf("⇄%d", len(terminal.BoundPaneIDs))
	}
	return info
}

func paneBorderStateLabel(state string, exitCode *int) string {
	switch state {
	case "running":
		return "●"
	case "exited":
		if exitCode != nil {
			return fmt.Sprintf("○%d", *exitCode)
		}
		return "○"
	case "waiting":
		return "…"
	case "killed":
		return "✕"
	default:
		return ""
	}
}

func findVisibleTerminal(runtimeState *VisibleRuntimeStateProxy, terminalID string) *runtime.VisibleTerminal {
	return newRuntimeLookup(runtimeState).terminal(terminalID)
}

func paneBindingRole(runtimeState *VisibleRuntimeStateProxy, paneID string) string {
	return newRuntimeLookup(runtimeState).paneRole(paneID)
}
