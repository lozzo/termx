package terminal

import "github.com/lozzow/termx/tui/core/types"

type State string

const (
	StateRunning State = "running"
	StateExited  State = "exited"
)

type Metadata struct {
	ID              types.TerminalID
	Name            string
	Command         []string
	State           State
	Tags            map[string]string
	OwnerPaneID     types.PaneID
	AttachedPaneIDs []types.PaneID
}

func (m Metadata) HasPane(paneID types.PaneID) bool {
	for _, attachedPaneID := range m.AttachedPaneIDs {
		if attachedPaneID == paneID {
			return true
		}
	}
	return false
}

func (m Metadata) ResolvedOwnerPaneID() types.PaneID {
	if m.OwnerPaneID != "" {
		return m.OwnerPaneID
	}
	if len(m.AttachedPaneIDs) == 0 {
		return ""
	}
	return m.AttachedPaneIDs[0]
}

func (m Metadata) ConnectionRole(paneID types.PaneID) types.ConnectionRole {
	if !m.HasPane(paneID) {
		return ""
	}
	if m.ResolvedOwnerPaneID() == paneID {
		return types.ConnectionRoleOwner
	}
	return types.ConnectionRoleFollower
}
