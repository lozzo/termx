package runtime

import "github.com/lozzow/termx/protocol"

func (r *Runtime) ApplySessionLeases(viewID string, leases []protocol.LeaseInfo) {
	if r == nil || r.registry == nil {
		return
	}
	index := make(map[string]protocol.LeaseInfo, len(leases))
	for _, lease := range leases {
		if lease.TerminalID == "" {
			continue
		}
		index[lease.TerminalID] = lease
	}
	changed := false
	for _, terminalID := range r.registry.IDs() {
		terminal := r.registry.Get(terminalID)
		if terminal == nil {
			continue
		}
		prev := r.terminalControlStatus(terminal)
		lease, ok := index[terminalID]
		switch {
		case !ok:
			r.restoreLocalTerminalControl(terminal)
		case lease.ViewID != "":
			if lease.ViewID == viewID && containsPaneID(terminal.BoundPaneIDs, lease.PaneID) && r.connectedLocalBinding(lease.PaneID) != nil {
				r.promoteTerminalControlPane(terminal, lease.PaneID, true)
			} else {
				r.clearTerminalLocalControl(terminal, lease.PaneID, len(terminal.BoundPaneIDs) > 0)
			}
		default:
			r.clearTerminalLocalControl(terminal, "", len(terminal.BoundPaneIDs) > 0)
		}
		r.syncTerminalOwnership(terminal)
		if next := r.terminalControlStatus(terminal); prev.OwnerPaneID != next.OwnerPaneID || prev.ControlPaneID != next.ControlPaneID || prev.RequiresExplicitOwner != next.RequiresExplicitOwner || prev.PendingOwnerResize != next.PendingOwnerResize {
			changed = true
		}
	}
	if changed {
		r.touch()
	}
}
