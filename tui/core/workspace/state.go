package workspace

import (
	"sort"

	"github.com/lozzow/termx/tui/core/layout"
	"github.com/lozzow/termx/tui/core/types"
)

type Workspace struct {
	ID          types.WorkspaceID
	ActiveTabID types.TabID
	Tabs        map[types.TabID]*TabState
}

type TabState struct {
	ID                types.TabID
	Title             string
	Layout            *layout.Node
	ActivePaneID      types.PaneID
	Panes             map[types.PaneID]PaneState
	FloatingPaneOrder []types.PaneID
}

type PaneState struct {
	ID         types.PaneID
	Kind       types.PaneKind
	SlotState  types.PaneSlotState
	TerminalID types.TerminalID
	Rect       types.Rect
}

func New(name string) *Workspace {
	if name == "" {
		name = "main"
	}
	initialPaneID := types.PaneID("pane-1")
	initialTabID := types.TabID("tab-1")
	tab := &TabState{
		ID:           initialTabID,
		Title:        "main",
		Layout:       layout.NewLeaf(initialPaneID),
		ActivePaneID: initialPaneID,
		Panes: map[types.PaneID]PaneState{
			initialPaneID: {
				ID:        initialPaneID,
				Kind:      types.PaneKindTiled,
				SlotState: types.PaneSlotUnconnected,
			},
		},
	}
	return &Workspace{
		ID:          types.WorkspaceID(name),
		ActiveTabID: initialTabID,
		Tabs: map[types.TabID]*TabState{
			initialTabID: tab,
		},
	}
}

func (w *Workspace) ActiveTab() *TabState {
	if w == nil {
		return nil
	}
	if tab, ok := w.Tabs[w.ActiveTabID]; ok {
		return tab
	}
	return nil
}

func (t *TabState) ActivePane() PaneState {
	if t == nil {
		return PaneState{}
	}
	if pane, ok := t.Panes[t.ActivePaneID]; ok {
		return pane
	}
	firstPaneID := t.FirstPaneID()
	if firstPaneID == "" {
		return PaneState{}
	}
	return t.Panes[firstPaneID]
}

func (t *TabState) FirstPaneID() types.PaneID {
	if t == nil || len(t.Panes) == 0 {
		return ""
	}
	ids := make([]string, 0, len(t.Panes))
	for paneID := range t.Panes {
		ids = append(ids, string(paneID))
	}
	sort.Strings(ids)
	return types.PaneID(ids[0])
}

// TrackPane 统一收口 pane 的默认值，避免上层 reducer/runtime 反复补齐空态。
func (t *TabState) TrackPane(pane PaneState) {
	if t == nil || pane.ID == "" {
		return
	}
	if t.Panes == nil {
		t.Panes = make(map[types.PaneID]PaneState)
	}
	if pane.Kind == "" {
		pane.Kind = types.PaneKindTiled
	}
	if pane.SlotState == "" {
		pane.SlotState = types.PaneSlotUnconnected
	}
	t.Panes[pane.ID] = pane
	if pane.Kind == types.PaneKindFloating && !containsPaneID(t.FloatingPaneOrder, pane.ID) {
		t.FloatingPaneOrder = append(t.FloatingPaneOrder, pane.ID)
	}
}

func containsPaneID(items []types.PaneID, target types.PaneID) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
