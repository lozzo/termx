package projection

import (
	"sort"
	"strings"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	"github.com/lozzow/termx/tui/core/types"
	coreworkspace "github.com/lozzow/termx/tui/core/workspace"
	featureoverlay "github.com/lozzow/termx/tui/features/overlay"
	featureterminalpool "github.com/lozzow/termx/tui/features/terminalpool"
)

type Screen struct {
	Screen        app.Screen
	WorkspaceName string
	Overlay       Overlay
	Panes         []Pane
	Pool          TerminalPool
}

type Overlay struct {
	Kind     featureoverlay.Kind
	Title    string
	Selected types.TerminalID
	Items    []PoolItem
}

type Pane struct {
	ID     types.PaneID
	Title  string
	Status string
	Body   string
}

type TerminalPool struct {
	SelectedTerminalID types.TerminalID
	Visible            []PoolItem
	Parked             []PoolItem
	Exited             []PoolItem
}

type PoolItem struct {
	ID    types.TerminalID
	Name  string
	State string
}

func Project(model app.Model, width, height int) Screen {
	screen := Screen{
		Screen:        model.Screen,
		WorkspaceName: model.WorkspaceName,
		Overlay: Overlay{
			Kind:     model.Overlay.Active.Kind,
			Title:    model.Overlay.Active.Title,
			Selected: model.Overlay.Active.Selected,
			Items:    projectPoolItems(model.Overlay.Active.Items),
		},
	}
	if model.Screen == app.ScreenTerminalPool {
		screen.Pool = projectTerminalPool(model)
		return screen
	}
	tab := model.Workbench.Workspace.ActiveTab()
	if tab == nil {
		return screen
	}

	panes := make([]Pane, 0, len(tab.Panes))
	for _, paneID := range orderedPaneIDs(tab.Panes, tab.ActivePaneID) {
		paneState := tab.Panes[paneID]
		pane := Pane{
			ID:     paneID,
			Title:  projectPaneTitle(model, paneState),
			Status: string(paneState.SlotState),
			Body:   projectPaneBody(model, paneState),
		}
		panes = append(panes, pane)
	}
	screen.Panes = panes
	return screen
}

func projectTerminalPool(model app.Model) TerminalPool {
	return TerminalPool{
		SelectedTerminalID: model.Pool.SelectedTerminalID,
		Visible:            projectPoolItems(model.Pool.Visible),
		Parked:             projectPoolItems(model.Pool.Parked),
		Exited:             projectPoolItems(model.Pool.Exited),
	}
}

func projectPoolItems(items []featureterminalpool.Item) []PoolItem {
	out := make([]PoolItem, 0, len(items))
	for _, item := range items {
		out = append(out, PoolItem{ID: item.ID, Name: item.Name, State: string(item.State)})
	}
	return out
}

func orderedPaneIDs(panes map[types.PaneID]coreworkspace.PaneState, active types.PaneID) []types.PaneID {
	ids := make([]types.PaneID, 0, len(panes))
	for paneID := range panes {
		ids = append(ids, paneID)
	}
	sort.Slice(ids, func(i, j int) bool {
		left := ids[i]
		right := ids[j]
		if left == active {
			return true
		}
		if right == active {
			return false
		}
		leftState := panes[left].SlotState
		rightState := panes[right].SlotState
		if paneStatePriority(leftState) != paneStatePriority(rightState) {
			return paneStatePriority(leftState) < paneStatePriority(rightState)
		}
		return left < right
	})
	return ids
}

func paneStatePriority(state types.PaneSlotState) int {
	switch state {
	case types.PaneSlotLive:
		return 0
	case types.PaneSlotExited:
		return 1
	default:
		return 2
	}
}

func projectPaneTitle(model app.Model, paneState coreworkspace.PaneState) string {
	if paneState.TerminalID != "" {
		if meta, ok := model.Workbench.Terminals[paneState.TerminalID]; ok && strings.TrimSpace(meta.Name) != "" {
			return meta.Name
		}
		return string(paneState.TerminalID)
	}
	return "unconnected"
}

func projectPaneBody(model app.Model, paneState coreworkspace.PaneState) string {
	switch paneState.SlotState {
	case types.PaneSlotUnconnected:
		return "connect existing\ncreate terminal\nopen pool"
	default:
		if session, ok := model.Workbench.Sessions[paneState.TerminalID]; ok {
			if body := snapshotBody(session.Snapshot); body != "" {
				return body
			}
		}
		if paneState.SlotState == types.PaneSlotExited {
			return "terminal exited"
		}
		return ""
	}
}

func snapshotBody(snapshot *protocol.Snapshot) string {
	if snapshot == nil {
		return ""
	}
	lines := make([]string, 0, len(snapshot.Screen.Cells))
	for _, row := range snapshot.Screen.Cells {
		var builder strings.Builder
		for _, cell := range row {
			builder.WriteString(cell.Content)
		}
		line := strings.TrimRight(builder.String(), " ")
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}
