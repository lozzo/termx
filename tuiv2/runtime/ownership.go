package runtime

import (
	"fmt"

	"github.com/lozzow/termx/tuiv2/shared"
)

func (r *Runtime) syncTerminalOwnership(terminal *TerminalRuntime) string {
	if r == nil || terminal == nil {
		return ""
	}
	ownerPaneID := terminal.OwnerPaneID
	if ownerPaneID != "" {
		if !containsPaneID(terminal.BoundPaneIDs, ownerPaneID) || r.bindings[ownerPaneID] == nil {
			ownerPaneID = ""
		}
	}
	changed := terminal.OwnerPaneID != ownerPaneID
	if terminal.OwnerPaneID != "" && ownerPaneID == "" {
		terminal.RequiresExplicitOwner = true
	}
	if ownerPaneID != "" {
		terminal.RequiresExplicitOwner = false
	}
	terminal.OwnerPaneID = ownerPaneID
	if r.syncBindingRolesForTerminal(terminal) {
		changed = true
	}
	if changed {
		r.touch()
	}
	return ownerPaneID
}

func (r *Runtime) syncBindingRolesForTerminal(terminal *TerminalRuntime) bool {
	if r == nil || terminal == nil {
		return false
	}
	changed := false
	for _, paneID := range terminal.BoundPaneIDs {
		binding := r.bindings[paneID]
		if binding == nil {
			continue
		}
		nextRole := BindingRoleFollower
		if paneID == terminal.OwnerPaneID {
			nextRole = BindingRoleOwner
		}
		if binding.Role == nextRole {
			continue
		}
		binding.Role = nextRole
		changed = true
	}
	return changed
}

func containsPaneID(ids []string, paneID string) bool {
	for _, existing := range ids {
		if existing == paneID {
			return true
		}
	}
	return false
}

func (r *Runtime) AcquireTerminalOwnership(paneID, terminalID string) error {
	if r == nil || r.registry == nil {
		return shared.UserVisibleError{Op: "take terminal ownership", Err: fmt.Errorf("runtime unavailable")}
	}
	if paneID == "" {
		return shared.UserVisibleError{Op: "take terminal ownership", Err: fmt.Errorf("pane is required")}
	}
	if terminalID == "" {
		return shared.UserVisibleError{Op: "take terminal ownership", Err: fmt.Errorf("pane %s is not attached to a terminal", paneID)}
	}
	terminal := r.registry.Get(terminalID)
	if terminal == nil {
		return shared.UserVisibleError{Op: "take terminal ownership", Err: fmt.Errorf("terminal %s not found", terminalID)}
	}
	binding := r.bindings[paneID]
	if binding == nil || !binding.Connected {
		return shared.UserVisibleError{Op: "take terminal ownership", Err: fmt.Errorf("pane %s is not attached", paneID)}
	}
	if !containsPaneID(terminal.BoundPaneIDs, paneID) {
		return shared.UserVisibleError{Op: "take terminal ownership", Err: fmt.Errorf("pane %s is not locally bound to terminal %s", paneID, terminalID)}
	}
	if terminal.OwnerPaneID == paneID {
		return nil
	}
	terminal.OwnerPaneID = paneID
	terminal.RequiresExplicitOwner = false
	terminal.PendingOwnerResize = true
	r.syncTerminalOwnership(terminal)
	return nil
}
