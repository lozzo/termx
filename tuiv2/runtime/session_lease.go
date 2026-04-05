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
		prevOwner := terminal.OwnerPaneID
		prevControl := terminal.ControlPaneID
		lease, ok := index[terminalID]
		switch {
		case !ok:
			terminal.OwnerPaneID = ""
			terminal.ControlPaneID = ""
			terminal.RequiresExplicitOwner = len(terminal.BoundPaneIDs) > 0
		case lease.ViewID != "":
			terminal.OwnerPaneID = lease.PaneID
			if lease.ViewID == viewID && containsPaneID(terminal.BoundPaneIDs, lease.PaneID) && r.bindings[lease.PaneID] != nil {
				terminal.ControlPaneID = lease.PaneID
				terminal.RequiresExplicitOwner = false
			} else {
				terminal.ControlPaneID = ""
				terminal.RequiresExplicitOwner = len(terminal.BoundPaneIDs) > 0
			}
			if prevControl != terminal.ControlPaneID && terminal.ControlPaneID != "" {
				terminal.PendingOwnerResize = true
			}
		default:
			terminal.OwnerPaneID = ""
			terminal.ControlPaneID = ""
			terminal.RequiresExplicitOwner = len(terminal.BoundPaneIDs) > 0
		}
		if r.syncBindingRolesForTerminal(terminal) {
			changed = true
		}
		if prevOwner != terminal.OwnerPaneID || prevControl != terminal.ControlPaneID {
			changed = true
		}
	}
	if changed {
		r.touch()
	}
}
