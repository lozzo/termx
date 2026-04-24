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
	StateLabel    string
	StateTone     string
	ShareLabel    string
	RoleLabel     string
	SizeLabel     string
	CopyTimeLabel string
	CopyRowLabel  string
}

// paneMeta generates the badge/meta string for a pane based on terminal state.
func paneMeta(pane workbench.VisiblePane, runtimeState *VisibleRuntimeStateProxy) string {
	return paneMetaWithLookup(pane, runtimeLookupForState(runtimeState), "")
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
		parts = append(parts, paneRunningIcon())
	case "exited":
		if terminal.ExitCode != nil {
			parts = append(parts, fmt.Sprintf("%s %d", paneExitedIcon(), *terminal.ExitCode))
		} else {
			parts = append(parts, paneExitedIcon())
		}
	case "waiting":
		parts = append(parts, paneWaitingIcon())
	case "killed":
		parts = append(parts, paneKilledIcon())
	}
	switch role := paneDisplayRole(pane.ID, terminal, lookup); role {
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
		StateTone:  paneBorderStateTone(terminal.State),
	}
	switch paneDisplayRole(pane.ID, terminal, lookup) {
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

func paneDisplayRole(paneID string, terminal *runtime.VisibleTerminal, lookup runtimeLookup) string {
	if paneID == "" || terminal == nil {
		return ""
	}
	if terminal.OwnerPaneID != "" {
		if terminal.OwnerPaneID == paneID {
			return "owner"
		}
		if containsPaneID(terminal.BoundPaneIDs, paneID) {
			return "follower"
		}
	}
	return lookup.paneRole(paneID)
}

func containsPaneID(ids []string, paneID string) bool {
	for _, existing := range ids {
		if existing == paneID {
			return true
		}
	}
	return false
}

func paneBorderStateLabel(state string, exitCode *int) string {
	switch state {
	case "running":
		return paneRunningIcon()
	case "exited":
		if exitCode != nil {
			return fmt.Sprintf("%s%d", paneExitedIcon(), *exitCode)
		}
		return paneExitedIcon()
	case "waiting":
		return paneWaitingIcon()
	case "killed":
		return paneKilledIcon()
	default:
		return ""
	}
}

func paneBorderStateTone(state string) string {
	switch state {
	case "running":
		return "success"
	case "waiting":
		return "warning"
	case "exited", "killed":
		return "danger"
	default:
		return ""
	}
}

func findVisibleTerminal(runtimeState *VisibleRuntimeStateProxy, terminalID string) *runtime.VisibleTerminal {
	return runtimeLookupForState(runtimeState).terminal(terminalID)
}

func runtimeLookupForState(runtimeState *VisibleRuntimeStateProxy) runtimeLookup {
	return newRuntimeLookup(runtimeState)
}
