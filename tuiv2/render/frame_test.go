package render

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
	rtpkg "github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestRenderStatusBarHidesUnavailablePaneActionsForUnconnectedPane(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 80, 18), 80, 20)
	state = WithStatus(state, "", "", string(input.ModePane))

	line := xansi.Strip(renderStatusBar(state))
	if strings.Contains(line, "d DETACH") || strings.Contains(line, "a OWNER") || strings.Contains(line, "X CLOSE+KILL") {
		t.Fatalf("expected unavailable pane actions to be hidden for unconnected pane:\n%s", line)
	}
	if !strings.Contains(line, "r RECONNECT") || !strings.Contains(line, "z ZOOM") {
		t.Fatalf("expected still-available pane actions to remain visible:\n%s", line)
	}
}

func TestRenderStatusBarShowsOwnerActionForSharedFollower(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-2",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "owner", TerminalID: "term-1"},
				"pane-2": {ID: "pane-2", Title: "follower", TerminalID: "term-1"},
			},
			Root: &workbench.LayoutNode{
				Direction: workbench.SplitVertical,
				Ratio:     0.5,
				First:     workbench.NewLeaf("pane-1"),
				Second:    workbench.NewLeaf("pane-2"),
			},
		}},
	})
	rt := runtime.New(nil)
	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.State = "running"
	terminal.OwnerPaneID = "pane-1"
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	ownerBinding := rt.BindPane("pane-1")
	ownerBinding.Role = runtime.BindingRoleOwner
	ownerBinding.Connected = true
	followerBinding := rt.BindPane("pane-2")
	followerBinding.Role = runtime.BindingRoleFollower
	followerBinding.Connected = true

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 120, 18), 120, 20)
	state = WithStatus(state, "", "", string(input.ModePane))

	line := xansi.Strip(renderStatusBar(state))
	if !strings.Contains(line, "a OWNER") || !strings.Contains(line, "d DETACH") {
		t.Fatalf("expected shared follower shortcuts to remain visible:\n%s", line)
	}
}

func TestRenderStatusBarFloatingModeShowsOnlyActiveFloatingActions(t *testing.T) {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "tab 1",
			ActivePaneID: "pane-1",
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", Title: "shell", TerminalID: "term-1"},
			},
			Root: workbench.NewLeaf("pane-1"),
		}},
	})

	state := WithTermSize(AdaptVisibleStateWithSize(wb, runtime.New(nil), 80, 18), 80, 20)
	state = WithStatus(state, "", "", string(input.ModeFloating))

	line := xansi.Strip(renderStatusBar(state))
	if !strings.Contains(line, "N NEW FLOAT") {
		t.Fatalf("expected floating mode to preserve create action:\n%s", line)
	}
	for _, hidden := range []string{"h/j/k/l MOVE", "H/J/K/L RESIZE", "x CLOSE", "v TOGGLE", "a OWNER"} {
		if strings.Contains(line, hidden) {
			t.Fatalf("expected %q to be hidden without an active floating pane:\n%s", hidden, line)
		}
	}
}

func TestPadPaneBorderSlotCentersText(t *testing.T) {
	if got := padPaneBorderSlot("x2", 4); got != " x2 " {
		t.Fatalf("expected centered slot padding, got %q", got)
	}
}

func TestRenderTabBarFillerUsesActiveTabBackground(t *testing.T) {
	state := VisibleRenderState{
		Workbench: &workbench.VisibleWorkbench{
			WorkspaceName: "main",
			ActiveTab:     0,
			Tabs: []workbench.VisibleTab{
				{ID: "tab-1", Name: "build"},
			},
		},
		Runtime: &rtpkg.VisibleRuntime{
			HostDefaultBG: "#f5f5f5",
			HostDefaultFG: "#111111",
		},
		TermSize: TermSize{Width: 60, Height: 20},
	}

	theme := uiThemeForState(state)
	layout := buildTabBarLayout(state)
	left := renderTabBarLeft(layout)
	fillerWidth := state.TermSize.Width - xansi.StringWidth(left)
	if fillerWidth <= 0 {
		t.Fatalf("expected positive filler width, got %d", fillerWidth)
	}

	line := renderTabBar(state)
	expectedFiller := backgroundStyle(theme.tabActiveBG).Width(fillerWidth).Render("")
	if !strings.HasSuffix(line, expectedFiller) {
		t.Fatalf("expected tab bar filler to use active bg %q", theme.tabActiveBG)
	}
}
