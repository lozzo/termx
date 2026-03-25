package connection

import "github.com/lozzow/termx/tui/domain/types"

type State struct {
	terminalID types.TerminalID
	panes      []types.PaneID
	ownerPane  types.PaneID
}

func NewState(terminalID types.TerminalID) *State {
	return &State{terminalID: terminalID}
}

func FromSnapshot(snapshot types.ConnectionState) *State {
	state := &State{
		terminalID: snapshot.TerminalID,
		panes:      append([]types.PaneID(nil), snapshot.ConnectedPaneIDs...),
		ownerPane:  snapshot.OwnerPaneID,
	}
	state.normalizeOwner()
	return state
}

func (s *State) Snapshot() types.ConnectionState {
	return types.ConnectionState{
		TerminalID:       s.terminalID,
		ConnectedPaneIDs: append([]types.PaneID(nil), s.panes...),
		OwnerPaneID:      s.ownerPane,
	}
}

func (s *State) Connect(paneID types.PaneID) {
	if paneID == "" || s.hasPane(paneID) {
		return
	}
	s.panes = append(s.panes, paneID)
	if s.ownerPane == "" {
		s.ownerPane = paneID
	}
}

func (s *State) Disconnect(paneID types.PaneID) {
	if paneID == "" {
		return
	}
	dst := s.panes[:0]
	for _, id := range s.panes {
		if id != paneID {
			dst = append(dst, id)
		}
	}
	s.panes = dst
	s.normalizeOwner()
}

func (s *State) Acquire(paneID types.PaneID) bool {
	if !s.hasPane(paneID) {
		return false
	}
	s.ownerPane = paneID
	return true
}

func (s *State) Owner() types.PaneID {
	s.normalizeOwner()
	return s.ownerPane
}

func (s *State) HasControl(paneID types.PaneID) bool {
	return s.Owner() == paneID
}

func (s *State) hasPane(paneID types.PaneID) bool {
	for _, id := range s.panes {
		if id == paneID {
			return true
		}
	}
	return false
}

// normalizeOwner 保证 owner 永远落在已连接 pane 集合中。
// 这是共享 terminal 最核心的不变量，后续 reducer 会依赖它做控制权迁移。
func (s *State) normalizeOwner() {
	if len(s.panes) == 0 {
		s.ownerPane = ""
		return
	}
	if s.hasPane(s.ownerPane) {
		return
	}
	s.ownerPane = s.panes[0]
}
