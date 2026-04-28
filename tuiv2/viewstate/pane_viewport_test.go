package viewstate

import (
	"testing"

	"github.com/lozzow/termx/tuiv2/workbench"
)

func TestPaneViewportOffsetPrefersBindingAndClampsNegative(t *testing.T) {
	viewport := PaneViewport{
		Workbench: viewportWorkbench(5),
		BindingOffset: func(paneID string) (int, bool) {
			if paneID != "pane-1" {
				t.Fatalf("unexpected pane id %q", paneID)
			}
			return -3, true
		},
	}

	if got := viewport.Offset("pane-1"); got != 0 {
		t.Fatalf("expected negative binding offset clamped to 0, got %d", got)
	}
}

func TestPaneViewportOffsetFallsBackToActiveTabScroll(t *testing.T) {
	wb := viewportWorkbench(7)
	viewport := PaneViewport{Workbench: wb}

	if got := viewport.Offset("pane-1"); got != 7 {
		t.Fatalf("expected active tab scroll fallback, got %d", got)
	}
	if got := viewport.EffectiveTabOffset(wb.CurrentTab()); got != 7 {
		t.Fatalf("expected effective tab offset fallback, got %d", got)
	}
}

func TestPaneViewportAdjustMigratesLegacyOffsetToBinding(t *testing.T) {
	var setCalls []int
	var adjusted bool
	viewport := PaneViewport{
		Workbench: viewportWorkbench(4),
		BindingOffset: func(string) (int, bool) {
			return 0, false
		},
		SetBindingOffset: func(paneID string, offset int) bool {
			if paneID != "pane-1" {
				t.Fatalf("unexpected set pane id %q", paneID)
			}
			setCalls = append(setCalls, offset)
			return true
		},
		AdjustBindingOffset: func(paneID string, delta int) (int, bool) {
			if paneID != "pane-1" || delta != 1 {
				t.Fatalf("unexpected adjust pane=%q delta=%d", paneID, delta)
			}
			adjusted = true
			return 5, true
		},
	}

	next, changed := viewport.AdjustOffset("pane-1", 1)
	if !changed || next != 5 {
		t.Fatalf("expected binding adjust result next=5 changed=true, got next=%d changed=%v", next, changed)
	}
	if !adjusted {
		t.Fatal("expected binding adjust callback")
	}
	if len(setCalls) != 1 || setCalls[0] != 4 {
		t.Fatalf("expected legacy offset migrated before adjust, got %#v", setCalls)
	}
}

func TestPaneViewportResetClearsBindingAndTabScroll(t *testing.T) {
	wb := viewportWorkbench(6)
	var setCalls []int
	viewport := PaneViewport{
		Workbench: wb,
		SetBindingOffset: func(paneID string, offset int) bool {
			if paneID != "pane-1" {
				t.Fatalf("unexpected set pane id %q", paneID)
			}
			setCalls = append(setCalls, offset)
			return true
		},
	}

	if !viewport.Reset("pane-1") {
		t.Fatal("expected reset to report a change")
	}
	if len(setCalls) != 1 || setCalls[0] != 0 {
		t.Fatalf("expected binding reset to zero, got %#v", setCalls)
	}
	if got := wb.CurrentTab().ScrollOffset; got != 0 {
		t.Fatalf("expected tab scroll reset to zero, got %d", got)
	}
}

func viewportWorkbench(scrollOffset int) *workbench.Workbench {
	wb := workbench.NewWorkbench()
	wb.AddWorkspace("main", &workbench.WorkspaceState{
		Name:      "main",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-1",
			Name:         "1",
			ActivePaneID: "pane-1",
			ScrollOffset: scrollOffset,
			Panes: map[string]*workbench.PaneState{
				"pane-1": {ID: "pane-1", TerminalID: "term-1"},
			},
		}},
	})
	return wb
}
