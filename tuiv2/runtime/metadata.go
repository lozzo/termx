package runtime

import "github.com/lozzow/termx/termx-core/protocol"

const externalResizeOwnerPaneID = "external"

func (r *Runtime) SetTerminalMetadata(terminalID, name string, tags map[string]string) {
	if r == nil || r.registry == nil || terminalID == "" {
		return
	}
	r.registry.SetMetadata(terminalID, name, tags)
	r.invalidate()
}

func (r *Runtime) ApplyTerminalList(terminals []protocol.TerminalInfo) {
	if r == nil || r.registry == nil {
		return
	}
	for _, info := range terminals {
		r.applyTerminalInfo(info)
	}
	r.invalidate()
}

func (r *Runtime) applyTerminalInfo(info protocol.TerminalInfo) {
	if r == nil || r.registry == nil || info.ID == "" {
		return
	}
	terminal := r.registry.GetOrCreate(info.ID)
	if terminal == nil {
		return
	}
	terminal.Name = info.Name
	terminal.Command = append([]string(nil), info.Command...)
	terminal.Tags = cloneTags(info.Tags)
	terminal.State = info.State
	terminal.ExitCode = cloneExitCode(info.ExitCode)
	terminal.ResizeOwnership = cloneProtocolResizeOwnership(info.ResizeOwnership)
	r.applyExternalResizeOwnershipInfo(terminal, info.ResizeOwnership, info.ResizeOwnerAttachmentCount)
}

func (r *Runtime) applyExternalResizeOwnerInfo(terminal *TerminalRuntime, resizeOwnerAttachmentCount int) {
	if terminal == nil {
		return
	}
	r.applyExternalResizeOwnershipInfo(terminal, terminal.ResizeOwnership, resizeOwnerAttachmentCount)
}

func (r *Runtime) applyExternalResizeOwnershipInfo(terminal *TerminalRuntime, ownership *protocol.ResizeOwnership, resizeOwnerAttachmentCount int) {
	if r == nil || terminal == nil {
		return
	}
	terminal.ResizeOwnerAttachmentCount = resizeOwnerAttachmentCount
	terminal.ResizeOwnership = cloneProtocolResizeOwnership(ownership)
	if ownership != nil && ownership.SizeLocked {
		r.clearTerminalLocalControl(terminal, externalResizeOwnerPaneID, true)
		r.syncTerminalOwnership(terminal)
		return
	}
	ownerSurfaceID := ""
	if ownership != nil {
		ownerSurfaceID = ownership.OwnerSurfaceID
	}
	if ownerSurfaceID != "" {
		if paneID := paneIDFromTerminalSurfaceID(ownerSurfaceID); paneID != "" && containsPaneID(terminal.BoundPaneIDs, paneID) {
			if r.connectedLocalBinding(paneID) != nil {
				r.promoteTerminalControlPane(terminal, paneID, false)
				r.syncTerminalOwnership(terminal)
				return
			}
		}
		r.clearTerminalLocalControl(terminal, externalResizeOwnerPaneID, true)
		r.syncTerminalOwnership(terminal)
		return
	}
	localBoundCount := len(terminal.BoundPaneIDs)
	if localBoundCount > 0 && resizeOwnerAttachmentCount > localBoundCount {
		if terminal.OwnerPaneID != "" && terminal.OwnerPaneID != externalResizeOwnerPaneID && !containsPaneID(terminal.BoundPaneIDs, terminal.OwnerPaneID) {
			terminal.ControlPaneID = ""
			terminal.RequiresExplicitOwner = true
			r.syncTerminalOwnership(terminal)
			return
		}
		r.clearTerminalLocalControl(terminal, externalResizeOwnerPaneID, true)
		r.syncTerminalOwnership(terminal)
		return
	}
	if terminal.OwnerPaneID == externalResizeOwnerPaneID {
		if paneID := r.firstConnectedBoundPaneID(terminal); paneID != "" {
			r.promoteTerminalControlPane(terminal, paneID, false)
		} else {
			r.restoreLocalTerminalControl(terminal)
		}
		r.syncTerminalOwnership(terminal)
	}
}

func cloneProtocolResizeOwnership(in *protocol.ResizeOwnership) *protocol.ResizeOwnership {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
