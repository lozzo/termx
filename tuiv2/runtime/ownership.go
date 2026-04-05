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
	controlPaneID := terminal.ControlPaneID
	if controlPaneID == "" {
		controlPaneID = r.inferControlPaneID(terminal, ownerPaneID)
	}
	if controlPaneID != "" {
		if controlPaneID != ownerPaneID || !containsPaneID(terminal.BoundPaneIDs, controlPaneID) || r.bindings[controlPaneID] == nil {
			controlPaneID = ""
		}
	}
	changed := terminal.OwnerPaneID != ownerPaneID || terminal.ControlPaneID != controlPaneID
	if terminal.OwnerPaneID != "" && ownerPaneID == "" {
		terminal.RequiresExplicitOwner = true
	}
	if controlPaneID != "" {
		terminal.RequiresExplicitOwner = false
	}
	terminal.OwnerPaneID = ownerPaneID
	terminal.ControlPaneID = controlPaneID
	if r.syncBindingRolesForTerminal(terminal) {
		changed = true
	}
	if changed {
		r.touch()
	}
	return controlPaneID
}

func (r *Runtime) inferControlPaneID(terminal *TerminalRuntime, ownerPaneID string) string {
	if r == nil || terminal == nil || ownerPaneID == "" || !containsPaneID(terminal.BoundPaneIDs, ownerPaneID) {
		return ""
	}
	binding := r.bindings[ownerPaneID]
	if binding == nil || !binding.Connected {
		return ""
	}
	if binding.Role == BindingRoleOwner {
		return ownerPaneID
	}
	return ""
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
		if paneID == terminal.ControlPaneID {
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
	terminal.ControlPaneID = paneID
	terminal.RequiresExplicitOwner = false
	terminal.PendingOwnerResize = true
	r.syncTerminalOwnership(terminal)
	return nil
}
