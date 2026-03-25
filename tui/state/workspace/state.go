package workspace

import (
	"github.com/lozzow/termx/tui/state/layout"
	"github.com/lozzow/termx/tui/state/types"
)

type PaneState struct {
	ID         types.PaneID
	Kind       types.PaneKind
	SlotState  types.PaneSlotState
	TerminalID types.TerminalID
	Rect       types.Rect
}

type TabState struct {
	ID            types.TabID
	Title         string
	Layout        *layout.Node
	ActivePaneID  types.PaneID
	Panes         map[types.PaneID]PaneState
	FloatingOrder []types.PaneID
}

type WorkspaceState struct {
	ID          types.WorkspaceID
	ActiveTabID types.TabID
	Tabs        map[types.TabID]*TabState
}

func NewTemporary(name string) *WorkspaceState {
	tabID := types.TabID("tab-1")
	paneID := types.PaneID("pane-1")

	tab := &TabState{
		ID:           tabID,
		Title:        "shell",
		Layout:       layout.NewLeaf(paneID),
		ActivePaneID: paneID,
		Panes: map[types.PaneID]PaneState{
			paneID: {
				ID:        paneID,
				Kind:      types.PaneKindTiled,
				SlotState: types.PaneSlotUnconnected,
			},
		},
	}

	return &WorkspaceState{
		ID:          types.WorkspaceID(name),
		ActiveTabID: tabID,
		Tabs: map[types.TabID]*TabState{
			tabID: tab,
		},
	}
}

func (ws *WorkspaceState) ActiveTab() *TabState {
	if ws == nil {
		return nil
	}
	return ws.Tabs[ws.ActiveTabID]
}

func (t *TabState) ActivePane() (PaneState, bool) {
	if t == nil {
		return PaneState{}, false
	}
	return t.Pane(t.ActivePaneID)
}

func (t *TabState) Pane(id types.PaneID) (PaneState, bool) {
	if t == nil {
		return PaneState{}, false
	}
	pane, ok := t.Panes[id]
	return pane, ok
}

// TrackPane 统一接入 tab 内 pane 状态，后续 split、float、restore 都走这里补充索引。
func (t *TabState) TrackPane(pane PaneState) {
	if t.Panes == nil {
		t.Panes = make(map[types.PaneID]PaneState)
	}
	t.Panes[pane.ID] = pane
	if pane.Kind == types.PaneKindFloating && !containsPane(t.FloatingOrder, pane.ID) {
		t.FloatingOrder = append(t.FloatingOrder, pane.ID)
	}
	if t.ActivePaneID == "" {
		t.ActivePaneID = pane.ID
	}
}

func (ws *WorkspaceState) Clone() *WorkspaceState {
	if ws == nil {
		return nil
	}
	out := &WorkspaceState{
		ID:          ws.ID,
		ActiveTabID: ws.ActiveTabID,
		Tabs:        make(map[types.TabID]*TabState, len(ws.Tabs)),
	}
	for id, tab := range ws.Tabs {
		out.Tabs[id] = tab.Clone()
	}
	return out
}

func (ws *WorkspaceState) HasPane(id types.PaneID) bool {
	if ws == nil {
		return false
	}
	for _, tab := range ws.Tabs {
		if _, ok := tab.Pane(id); ok {
			return true
		}
	}
	return false
}

func (t *TabState) Clone() *TabState {
	if t == nil {
		return nil
	}
	out := &TabState{
		ID:            t.ID,
		Title:         t.Title,
		Layout:        t.Layout.Clone(),
		ActivePaneID:  t.ActivePaneID,
		Panes:         make(map[types.PaneID]PaneState, len(t.Panes)),
		FloatingOrder: append([]types.PaneID(nil), t.FloatingOrder...),
	}
	for id, pane := range t.Panes {
		out.Panes[id] = pane
	}
	return out
}

func (t *TabState) PaneCount() int {
	if t == nil {
		return 0
	}
	return len(t.Panes)
}

func (t *TabState) FirstPaneID() types.PaneID {
	if t == nil {
		return ""
	}
	for _, floatingID := range t.FloatingOrder {
		if _, ok := t.Panes[floatingID]; ok {
			return floatingID
		}
	}
	for paneID := range t.Panes {
		return paneID
	}
	return ""
}

func (t *TabState) RaiseFloatingPane(paneID types.PaneID) {
	if t == nil {
		return
	}
	next := make([]types.PaneID, 0, len(t.FloatingOrder))
	for _, existing := range t.FloatingOrder {
		if existing != paneID {
			next = append(next, existing)
		}
	}
	t.FloatingOrder = append(next, paneID)
}

func (t *TabState) RemoveFloatingPane(paneID types.PaneID) {
	if t == nil {
		return
	}
	next := make([]types.PaneID, 0, len(t.FloatingOrder))
	for _, existing := range t.FloatingOrder {
		if existing != paneID {
			next = append(next, existing)
		}
	}
	t.FloatingOrder = next
}

func (p PaneState) IsUnconnected() bool {
	return p.SlotState == types.PaneSlotUnconnected
}

func (p PaneState) AnchorVisible(bounds types.Rect) bool {
	return p.Rect.X >= bounds.X && p.Rect.Y >= bounds.Y &&
		p.Rect.X < bounds.X+bounds.W && p.Rect.Y < bounds.Y+bounds.H
}

func (p PaneState) IsCentered(bounds types.Rect) bool {
	return p.Rect.X == bounds.X+(bounds.W-p.Rect.W)/2 &&
		p.Rect.Y == bounds.Y+(bounds.H-p.Rect.H)/2
}

func containsPane(ids []types.PaneID, target types.PaneID) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}
