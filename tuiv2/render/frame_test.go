package render

import (
	"fmt"
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/runtime"
	rtpkg "github.com/lozzow/termx/tuiv2/runtime"
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
	if strings.Contains(line, "[d] DETACH") || strings.Contains(line, "[a] OWNER") || strings.Contains(line, "[X] CLOSE+KILL") {
		t.Fatalf("expected unavailable pane actions to be hidden for unconnected pane:\n%s", line)
	}
	if !strings.Contains(line, "[r] RECONNECT") || !strings.Contains(line, "[z] ZOOM") {
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
	if !strings.Contains(line, "[a] OWNER") || !strings.Contains(line, "[d] DETACH") {
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
	if !strings.Contains(line, "[N] NEW FLOAT") {
		t.Fatalf("expected floating mode to preserve create action:\n%s", line)
	}
	for _, hidden := range []string{"[h/j/k/l] MOVE", "[H/J/K/L] RESIZE", "[x] CLOSE", "[v] TOGGLE", "[a] OWNER"} {
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
	line := renderTabBar(state)
	// The filler uses styleANSI with the active tab BG — verify
	// the rendered ANSI contains the matching 48;2;R;G;B sequence.
	r, g, b, ok := parseHexColor(theme.tabActiveBG)
	if !ok {
		t.Fatalf("could not parse tabActiveBG %q", theme.tabActiveBG)
	}
	wantBG := fmt.Sprintf("48;2;%d;%d;%d", r, g, b)
	if !strings.Contains(line, wantBG) {
		t.Fatalf("expected tab bar filler to contain BG %s for active bg %q, got:\n%q", wantBG, theme.tabActiveBG, line)
	}
}

func TestFillLineKeepsRightSegmentSeparatedWhenTight(t *testing.T) {
	line := xansi.Strip(fillLine("[V] COPY [F] PICKER [G] GLOBAL", "ws:main terminals:1", 40, "#000000"))
	if !strings.Contains(line, " ws:main terminals:1") {
		t.Fatalf("expected right segment to stay separated, got %q", line)
	}
	if strings.Contains(line, "GLOBALws:main") {
		t.Fatalf("expected left and right segments not to merge, got %q", line)
	}
}

func TestStatusBarHintTokensUseForegroundColor(t *testing.T) {
	theme := defaultUITheme()
	rootColors := rootStatusHintColors(theme)
	labels := []string{"P PANE", "R RESIZE", "T TAB", "W WORKSPACE", "O FLOAT", "V COPY", "F PICKER", "G GLOBAL"}
	for i, color := range rootColors {
		if i >= len(labels) {
			break
		}
		hint := renderDesktopHint(theme, labels[i], color)
		// The key bracket should use the semantic color as FG
		r, g, b, ok := parseHexColor(color)
		if !ok {
			t.Fatalf("cannot parse color %q", color)
		}
		wantFG := fmt.Sprintf("38;2;%d;%d;%d", r, g, b)
		if !strings.Contains(hint, wantFG) {
			t.Errorf("%s: expected FG %s in hint output %q", labels[i], wantFG, hint)
		}
	}
}

func TestStatusBarRenderIsDeterministic(t *testing.T) {
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
	rt := runtime.New(nil)
	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.State = "running"

	state := WithTermSize(AdaptVisibleStateWithSize(wb, rt, 120, 18), 120, 20)
	state = WithStatus(state, "", "", string(input.ModeNormal))

	first := renderStatusBar(state)
	for i := 0; i < 5; i++ {
		got := renderStatusBar(state)
		if got != first {
			t.Fatalf("status bar render %d differs from first:\nfirst=%q\ngot=%q", i, first, got)
		}
	}
}

func TestFillLineStartsWithCHAAnchor(t *testing.T) {
	line := fillLine("left", "right", 40, "#000000")
	if !strings.HasPrefix(line, "\x1b[1G") {
		t.Fatalf("expected fillLine to start with CHA(1), got prefix %q", line[:minInt(10, len(line))])
	}
}

func TestStatusBarTruncationPreservesColors(t *testing.T) {
	theme := defaultUITheme()
	hint := renderDesktopHint(theme, "V COPY", theme.hintKeyFG)
	sep := renderStatusSep(theme)
	hint2 := renderDesktopHint(theme, "G GLOBAL", theme.hintKeyFG)
	long := hint + sep + hint2
	truncated := xansi.Truncate(long, 15, "")
	if !strings.Contains(truncated, "48;2;") {
		t.Errorf("truncated hint lost BG color: %q", truncated)
	}
}

func TestModeAccentColorUsesBaseHintAccentForDisplayAndGlobal(t *testing.T) {
	theme := defaultUITheme()
	for _, mode := range []input.ModeKind{input.ModeDisplay, input.ModeGlobal, input.ModeTerminalManager} {
		if got := modeAccentColor(theme, mode); got != theme.hintKeyFG {
			t.Fatalf("mode %q accent=%q, want base hint accent %q", mode, got, theme.hintKeyFG)
		}
	}
}
