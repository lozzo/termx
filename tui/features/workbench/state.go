package workbench

import (
	"github.com/lozzow/termx/protocol"
	coreterminal "github.com/lozzow/termx/tui/core/terminal"
	"github.com/lozzow/termx/tui/core/types"
	coreworkspace "github.com/lozzow/termx/tui/core/workspace"
)

type State struct {
	Workspace *coreworkspace.Workspace
	Terminals map[types.TerminalID]coreterminal.Metadata
	Sessions  map[types.TerminalID]SessionState
}

type SessionState struct {
	Snapshot *protocol.Snapshot
}

func New(workspaceName string) State {
	return State{
		Workspace: coreworkspace.New(workspaceName),
		Terminals: make(map[types.TerminalID]coreterminal.Metadata),
		Sessions:  make(map[types.TerminalID]SessionState),
	}
}

func (s State) ActivePane() coreworkspace.PaneState {
	if s.Workspace == nil {
		return coreworkspace.PaneState{}
	}
	tab := s.Workspace.ActiveTab()
	if tab == nil {
		return coreworkspace.PaneState{}
	}
	return tab.ActivePane()
}

func (s State) ActiveTerminalID() types.TerminalID {
	return s.ActivePane().TerminalID
}

// BindActivePane 把当前活跃 pane 绑定到 terminal，并同步 owner/attached 摘要。
func (s *State) BindActivePane(meta coreterminal.Metadata) bool {
	if s == nil || s.Workspace == nil {
		return false
	}
	tab := s.Workspace.ActiveTab()
	if tab == nil {
		return false
	}
	pane := tab.ActivePane()
	if pane.ID == "" {
		return false
	}
	pane.TerminalID = meta.ID
	if meta.State == coreterminal.StateExited {
		pane.SlotState = types.PaneSlotExited
	} else {
		pane.SlotState = types.PaneSlotLive
	}
	tab.TrackPane(pane)
	if meta.OwnerPaneID == "" {
		meta.OwnerPaneID = pane.ID
	}
	if !meta.HasPane(pane.ID) {
		meta.AttachedPaneIDs = append(meta.AttachedPaneIDs, pane.ID)
	}
	s.Terminals[meta.ID] = meta
	return true
}

// MarkPaneDisconnected 只解除单个 pane 和 terminal 的绑定，不影响其他共享 pane。
func (s *State) MarkPaneDisconnected(paneID types.PaneID) bool {
	if s == nil || s.Workspace == nil {
		return false
	}
	tab := s.Workspace.ActiveTab()
	if tab == nil {
		return false
	}
	pane, ok := tab.Panes[paneID]
	if !ok {
		return false
	}
	terminalID := pane.TerminalID
	pane.TerminalID = ""
	pane.SlotState = types.PaneSlotUnconnected
	tab.TrackPane(pane)
	if terminalID == "" {
		return true
	}
	meta, ok := s.Terminals[terminalID]
	if !ok {
		return true
	}
	meta.AttachedPaneIDs = removePaneID(meta.AttachedPaneIDs, paneID)
	if meta.OwnerPaneID == paneID {
		meta.OwnerPaneID = meta.ResolvedOwnerPaneID()
	}
	s.Terminals[terminalID] = meta
	return true
}

// MarkTerminalExited 保留 pane 绑定关系，只把绑定到该 terminal 的 pane 统一切到 exited。
func (s *State) MarkTerminalExited(terminalID types.TerminalID) int {
	if s == nil || s.Workspace == nil {
		return 0
	}
	meta, ok := s.Terminals[terminalID]
	if !ok {
		return 0
	}
	meta.State = coreterminal.StateExited
	s.Terminals[terminalID] = meta
	return s.updatePanesForTerminal(terminalID, func(pane coreworkspace.PaneState) coreworkspace.PaneState {
		pane.SlotState = types.PaneSlotExited
		return pane
	})
}

// MarkTerminalRemoved 清除 terminal 记录，并把原 pane 恢复成 unconnected。
func (s *State) MarkTerminalRemoved(terminalID types.TerminalID) int {
	if s == nil || s.Workspace == nil {
		return 0
	}
	delete(s.Terminals, terminalID)
	delete(s.Sessions, terminalID)
	return s.updatePanesForTerminal(terminalID, func(pane coreworkspace.PaneState) coreworkspace.PaneState {
		pane.TerminalID = ""
		pane.SlotState = types.PaneSlotUnconnected
		return pane
	})
}

func (s *State) SetSessionSnapshot(terminalID types.TerminalID, snapshot *protocol.Snapshot) {
	if s == nil {
		return
	}
	if s.Sessions == nil {
		s.Sessions = make(map[types.TerminalID]SessionState)
	}
	session := s.Sessions[terminalID]
	session.Snapshot = snapshot
	s.Sessions[terminalID] = session
}

func (s *State) updatePanesForTerminal(terminalID types.TerminalID, update func(coreworkspace.PaneState) coreworkspace.PaneState) int {
	tab := s.Workspace.ActiveTab()
	if tab == nil {
		return 0
	}
	updated := 0
	for paneID, pane := range tab.Panes {
		if pane.TerminalID != terminalID {
			continue
		}
		tab.TrackPane(update(pane))
		updated++
		if paneID == tab.ActivePaneID {
			tab.ActivePaneID = paneID
		}
	}
	return updated
}

func (s State) VisibleTerminalIDs() map[types.TerminalID]bool {
	visible := make(map[types.TerminalID]bool)
	if s.Workspace == nil {
		return visible
	}
	tab := s.Workspace.ActiveTab()
	if tab == nil {
		return visible
	}
	for _, pane := range tab.Panes {
		if pane.TerminalID != "" {
			visible[pane.TerminalID] = true
		}
	}
	return visible
}

func removePaneID(items []types.PaneID, target types.PaneID) []types.PaneID {
	if len(items) == 0 {
		return nil
	}
	out := make([]types.PaneID, 0, len(items))
	for _, item := range items {
		if item != target {
			out = append(out, item)
		}
	}
	return out
}
