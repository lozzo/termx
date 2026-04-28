package runtime

import (
	"context"
	"time"

	"github.com/lozzow/termx/internal/clientapi"
	"github.com/lozzow/termx/protocol"
)

type TerminalAttachmentSnapshot struct {
	Exists     bool
	Channel    uint16
	AttachMode string
}

type TerminalLiveStateSnapshot struct {
	Exists                 bool
	TerminalID             string
	Name                   string
	Command                []string
	Tags                   map[string]string
	State                  string
	ExitCode               *int
	Title                  string
	Channel                uint16
	AttachMode             string
	Snapshot               *clientapi.SnapshotRef
	SnapshotVersion        uint64
	SurfaceVersion         uint64
	ScreenUpdate           VisibleScreenUpdateSummary
	VTerm                  VTermLike
	ScrollbackLoadedLimit  int
	ScrollbackLoadingLimit int
	ScrollbackExhausted    bool
	PreferSnapshot         bool
	BootstrapPending       bool
	Recovery               RecoveryState
	WasStreaming           bool
	StreamGeneration       uint64
}

func ClonePaneBinding(binding *PaneBinding) *PaneBinding {
	if binding == nil {
		return nil
	}
	cloned := *binding
	return &cloned
}

func (r *Runtime) RestorePaneBinding(paneID string, binding *PaneBinding) {
	if r == nil || paneID == "" {
		return
	}
	if binding == nil {
		delete(r.bindings, paneID)
		r.touch()
		return
	}
	cloned := *binding
	r.bindings[paneID] = &cloned
	r.touch()
}

func (r *Runtime) RestoreTerminalControlStatus(status TerminalControlStatus) {
	if r == nil || r.registry == nil || status.TerminalID == "" {
		return
	}
	terminal := r.registry.Get(status.TerminalID)
	if terminal == nil {
		return
	}
	terminal.OwnerPaneID = status.OwnerPaneID
	terminal.ControlPaneID = status.ControlPaneID
	terminal.RequiresExplicitOwner = status.RequiresExplicitOwner
	terminal.PendingOwnerResize = status.PendingOwnerResize
	terminal.BoundPaneIDs = append([]string(nil), status.BoundPaneIDs...)
	r.syncTerminalOwnership(terminal)
}

func (r *Runtime) TerminalAttachmentSnapshot(terminalID string) TerminalAttachmentSnapshot {
	if r == nil || r.registry == nil || terminalID == "" {
		return TerminalAttachmentSnapshot{}
	}
	terminal := r.registry.Get(terminalID)
	if terminal == nil {
		return TerminalAttachmentSnapshot{}
	}
	return TerminalAttachmentSnapshot{
		Exists:     true,
		Channel:    terminal.Channel,
		AttachMode: terminal.AttachMode,
	}
}

func (r *Runtime) RestoreTerminalAttachmentSnapshot(terminalID string, snapshot TerminalAttachmentSnapshot) {
	if r == nil || r.registry == nil || terminalID == "" {
		return
	}
	terminal := r.registry.Get(terminalID)
	if terminal == nil {
		return
	}
	if !snapshot.Exists {
		terminal.Channel = 0
		terminal.AttachMode = ""
		r.touch()
		return
	}
	if terminal.Channel == snapshot.Channel && terminal.AttachMode == snapshot.AttachMode {
		return
	}
	terminal.Channel = snapshot.Channel
	terminal.AttachMode = snapshot.AttachMode
	r.touch()
}

func (r *Runtime) TerminalLiveStateSnapshot(terminalID string) TerminalLiveStateSnapshot {
	if r == nil || r.registry == nil || terminalID == "" {
		return TerminalLiveStateSnapshot{}
	}
	terminal := r.registry.Get(terminalID)
	if terminal == nil {
		return TerminalLiveStateSnapshot{}
	}
	return TerminalLiveStateSnapshot{
		Exists:                 true,
		TerminalID:             terminal.TerminalID,
		Name:                   terminal.Name,
		Command:                append([]string(nil), terminal.Command...),
		Tags:                   cloneTags(terminal.Tags),
		State:                  terminal.State,
		ExitCode:               cloneExitCode(terminal.ExitCode),
		Title:                  terminal.Title,
		Channel:                terminal.Channel,
		AttachMode:             terminal.AttachMode,
		Snapshot:               cloneRuntimeSnapshot(terminal.Snapshot),
		SnapshotVersion:        terminal.SnapshotVersion,
		SurfaceVersion:         terminal.SurfaceVersion,
		ScreenUpdate:           cloneVisibleScreenUpdateSummary(terminal.ScreenUpdate),
		VTerm:                  terminal.VTerm,
		ScrollbackLoadedLimit:  terminal.ScrollbackLoadedLimit,
		ScrollbackLoadingLimit: terminal.ScrollbackLoadingLimit,
		ScrollbackExhausted:    terminal.ScrollbackExhausted,
		PreferSnapshot:         terminal.PreferSnapshot,
		BootstrapPending:       terminal.BootstrapPending,
		Recovery:               terminal.Recovery,
		WasStreaming:           terminal.Stream.Active,
		StreamGeneration:       terminal.Stream.Generation,
	}
}

func (r *Runtime) RestoreTerminalLiveState(terminalID string, snapshot TerminalLiveStateSnapshot) {
	if r == nil || r.registry == nil || terminalID == "" {
		return
	}
	terminal := r.registry.Get(terminalID)
	if terminal == nil {
		return
	}
	currentGeneration := terminal.Stream.Generation
	if terminal.Stream.Stop != nil {
		terminal.Stream.Stop()
	}
	if !snapshot.Exists {
		terminal.Name = ""
		terminal.Command = nil
		terminal.Tags = nil
		terminal.State = ""
		terminal.ExitCode = nil
		terminal.Title = ""
		terminal.Channel = 0
		terminal.AttachMode = ""
		terminal.Snapshot = nil
		terminal.SnapshotVersion = 0
		terminal.SurfaceVersion = 0
		terminal.ScreenUpdate = VisibleScreenUpdateSummary{}
		terminal.VTerm = nil
		terminal.ScrollbackLoadedLimit = 0
		terminal.ScrollbackLoadingLimit = 0
		terminal.ScrollbackExhausted = false
		terminal.PreferSnapshot = false
		terminal.BootstrapPending = false
		terminal.Recovery = RecoveryState{}
		terminal.Stream = StreamState{Generation: currentGeneration}
		r.touch()
		return
	}
	terminal.Name = snapshot.Name
	terminal.Command = append([]string(nil), snapshot.Command...)
	terminal.Tags = cloneTags(snapshot.Tags)
	terminal.State = snapshot.State
	terminal.ExitCode = cloneExitCode(snapshot.ExitCode)
	terminal.Title = snapshot.Title
	terminal.Channel = snapshot.Channel
	terminal.AttachMode = snapshot.AttachMode
	terminal.Snapshot = cloneRuntimeSnapshot(snapshot.Snapshot)
	terminal.SnapshotVersion = snapshot.SnapshotVersion
	terminal.SurfaceVersion = snapshot.SurfaceVersion
	terminal.ScreenUpdate = cloneVisibleScreenUpdateSummary(snapshot.ScreenUpdate)
	terminal.VTerm = snapshot.VTerm
	terminal.ScrollbackLoadedLimit = snapshot.ScrollbackLoadedLimit
	terminal.ScrollbackLoadingLimit = snapshot.ScrollbackLoadingLimit
	terminal.ScrollbackExhausted = snapshot.ScrollbackExhausted
	terminal.PreferSnapshot = snapshot.PreferSnapshot
	terminal.BootstrapPending = snapshot.BootstrapPending
	terminal.Recovery = snapshot.Recovery
	if snapshot.StreamGeneration > currentGeneration {
		currentGeneration = snapshot.StreamGeneration
	}
	terminal.Stream = StreamState{Generation: currentGeneration}
	r.touch()
	if snapshot.WasStreaming && snapshot.Channel != 0 {
		_ = r.StartStream(context.Background(), terminalID)
	}
}

func cloneRuntimeSnapshot(snapshot *clientapi.SnapshotRef) *clientapi.SnapshotRef {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	cloned.Screen = protocol.ScreenData{
		Cells:             cloneProtocolCells2D(snapshot.Screen.Cells),
		IsAlternateScreen: snapshot.Screen.IsAlternateScreen,
	}
	cloned.Scrollback = cloneProtocolCells2D(snapshot.Scrollback)
	cloned.ScreenTimestamps = append([]time.Time(nil), snapshot.ScreenTimestamps...)
	cloned.ScrollbackTimestamps = append([]time.Time(nil), snapshot.ScrollbackTimestamps...)
	cloned.ScreenRowKinds = append([]string(nil), snapshot.ScreenRowKinds...)
	cloned.ScrollbackRowKinds = append([]string(nil), snapshot.ScrollbackRowKinds...)
	return &cloned
}

func cloneVisibleScreenUpdateSummary(summary VisibleScreenUpdateSummary) VisibleScreenUpdateSummary {
	summary.ChangedRows = append([]int(nil), summary.ChangedRows...)
	return summary
}
