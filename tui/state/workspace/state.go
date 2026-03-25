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

func containsPane(ids []types.PaneID, target types.PaneID) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}
