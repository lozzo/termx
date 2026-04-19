package runtime

type TerminalControlStatus struct {
	TerminalID            string
	OwnerPaneID           string
	ControlPaneID         string
	BoundPaneIDs          []string
	RequiresExplicitOwner bool
	PendingOwnerResize    bool
}

type TerminalOwnershipRequest struct {
	PaneID                   string
	ExplicitTakeover         bool
	ImplicitInteractiveOwner bool
}

type TerminalResizeDecision struct {
	Status  TerminalControlStatus
	Allowed bool
	Force   bool
}

func (r *Runtime) TerminalControlStatus(terminalID string) TerminalControlStatus {
	if r == nil || r.registry == nil || terminalID == "" {
		return TerminalControlStatus{}
	}
	return r.terminalControlStatus(r.registry.Get(terminalID))
}

func (r *Runtime) terminalControlStatus(terminal *TerminalRuntime) TerminalControlStatus {
	if terminal == nil {
		return TerminalControlStatus{}
	}
	status := TerminalControlStatus{
		TerminalID:            terminal.TerminalID,
		OwnerPaneID:           terminal.OwnerPaneID,
		ControlPaneID:         terminal.ControlPaneID,
		BoundPaneIDs:          append([]string(nil), terminal.BoundPaneIDs...),
		RequiresExplicitOwner: terminal.RequiresExplicitOwner,
		PendingOwnerResize:    terminal.PendingOwnerResize,
	}
	if status.ControlPaneID == "" && !status.RequiresExplicitOwner {
		status.ControlPaneID = r.inferControlPaneID(terminal, status.OwnerPaneID)
	}
	if status.ControlPaneID != "" {
		if status.ControlPaneID != status.OwnerPaneID || !containsPaneID(status.BoundPaneIDs, status.ControlPaneID) || r.bindings[status.ControlPaneID] == nil {
			status.ControlPaneID = ""
		}
	}
	if status.ControlPaneID != "" {
		status.RequiresExplicitOwner = false
	}
	return status
}

func (r *Runtime) ShouldAcquireTerminalOwnership(terminalID string, req TerminalOwnershipRequest) bool {
	if r == nil || r.registry == nil || terminalID == "" || req.PaneID == "" {
		return false
	}
	terminal := r.registry.Get(terminalID)
	if terminal == nil || r.connectedLocalBinding(req.PaneID) == nil || !containsPaneID(terminal.BoundPaneIDs, req.PaneID) {
		return false
	}
	status := r.terminalControlStatus(terminal)
	if req.ExplicitTakeover {
		return status.OwnerPaneID != req.PaneID || status.ControlPaneID != req.PaneID || status.RequiresExplicitOwner
	}
	if !req.ImplicitInteractiveOwner {
		return false
	}
	if len(status.BoundPaneIDs) < 2 || !terminalHasVisibleCursor(terminal) {
		return false
	}
	return status.OwnerPaneID != req.PaneID
}

func (r *Runtime) ResizeDecision(paneID, terminalID string) TerminalResizeDecision {
	decision := TerminalResizeDecision{}
	if r == nil || r.registry == nil || paneID == "" || terminalID == "" {
		return decision
	}
	terminal := r.registry.Get(terminalID)
	if terminal == nil {
		return decision
	}
	decision.Status = r.terminalControlStatus(terminal)
	decision.Force = terminal.PendingOwnerResize
	switch {
	case decision.Status.ControlPaneID == "":
		decision.Allowed = !decision.Status.RequiresExplicitOwner
	default:
		decision.Allowed = decision.Status.ControlPaneID == paneID
	}
	return decision
}

func terminalHasVisibleCursor(terminal *TerminalRuntime) bool {
	if terminal == nil {
		return false
	}
	switch {
	case terminal.VTerm != nil:
		return terminal.VTerm.CursorState().Visible
	case terminal.Snapshot != nil:
		return terminal.Snapshot.Cursor.Visible
	default:
		return false
	}
}

func (r *Runtime) connectedLocalBinding(paneID string) *PaneBinding {
	if r == nil || paneID == "" {
		return nil
	}
	binding := r.bindings[paneID]
	if binding == nil || !binding.Connected {
		return nil
	}
	return binding
}

func (r *Runtime) promoteTerminalControlPane(terminal *TerminalRuntime, paneID string, forceNextResize bool) {
	if terminal == nil || paneID == "" {
		return
	}
	prev := r.terminalControlStatus(terminal)
	terminal.OwnerPaneID = paneID
	terminal.ControlPaneID = paneID
	terminal.RequiresExplicitOwner = false
	if forceNextResize && prev.ControlPaneID != paneID {
		terminal.PendingOwnerResize = true
	}
}

func (r *Runtime) clearTerminalLocalControl(terminal *TerminalRuntime, ownerPaneID string, requiresExplicitOwner bool) {
	if terminal == nil {
		return
	}
	terminal.OwnerPaneID = ownerPaneID
	terminal.ControlPaneID = ""
	terminal.RequiresExplicitOwner = requiresExplicitOwner
}

func (r *Runtime) restoreLocalTerminalControl(terminal *TerminalRuntime) {
	if terminal == nil {
		return
	}
	if localOwnerPaneID := r.localConnectedOwnerPaneID(terminal); localOwnerPaneID != "" {
		r.promoteTerminalControlPane(terminal, localOwnerPaneID, false)
		return
	}
	r.clearTerminalLocalControl(terminal, "", len(terminal.BoundPaneIDs) > 0)
}
