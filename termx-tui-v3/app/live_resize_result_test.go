package app

import (
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestDetachedViewResizeResultDoesNotOverrideCurrentSessionSize(t *testing.T) {
	reducer := NewLiveReducer(LiveDeps{})
	root := state.Root{
		Session: state.TerminalSessionStore{
			TerminalID: "term-1",
			Cols:       118,
			Rows:       38,
			Attached:   true,
		}.Resize(118, 38),
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-1",
			Cols:       118,
			Rows:       38,
		}.Resize(118, 38),
		TerminalViews: state.TerminalViewStore{}.BindPane(state.NewPaneTerminalView(
			state.DefaultPaneID,
			"term-1",
			8,
			118,
			38,
			state.TerminalResizeRoleOwner,
			"surface-main",
			state.TerminalPaneViewID(state.DefaultPaneID),
			true,
		)),
	}

	next, effects := reducer(root, LiveResizeResultMsg{
		ViewID: state.TerminalPaneViewID("pane-2"),
		Seq:    1,
		Cols:   64,
		Rows:   38,
		Result: services.TerminalResizeResult{
			TerminalID:   "term-1",
			Cols:         64,
			Rows:         38,
			Resized:      true,
			CanResize:    true,
			ResizePolicy: state.TerminalResizeRoleOwner,
			SurfaceID:    "surface-pane-2",
			ViewID:       state.TerminalPaneViewID("pane-2"),
		},
	})
	if len(effects) != 1 {
		t.Fatalf("detached stale resize result should resync latest owner size once, got %#v", effects)
	}
	msg, ok := effects[0].(FuncEffect).Run(nil).(LiveResizeMsg)
	if !ok {
		t.Fatalf("expected stale recovery LiveResizeMsg, got %#v", effects[0])
	}
	if msg.ViewID != state.TerminalPaneViewID(state.DefaultPaneID) || msg.Cols != 118 || msg.Rows != 38 {
		t.Fatalf("stale recovery must reassert latest owner size, got %#v", msg)
	}
	if next.Session.Cols != 118 || next.Session.Rows != 38 {
		t.Fatalf("detached view resize result must not override session size, got %#v", next.Session)
	}
	if next.Surface.Cols != 118 || next.Surface.Rows != 38 {
		t.Fatalf("detached view resize result must not override surface size, got %#v", next.Surface)
	}
	if _, ok := next.TerminalViews.Views[state.TerminalPaneViewID("pane-2")]; ok {
		t.Fatalf("detached view must stay absent, views=%#v", next.TerminalViews.Views)
	}
}

func TestDetachedViewResizeRequestDoesNotReuseCurrentSessionChannel(t *testing.T) {
	terminal := &services.FakeTerminalService{}
	reducer := NewLiveReducer(LiveDeps{Terminal: terminal})
	root := state.Root{
		Session: state.TerminalSessionStore{
			TerminalID: "term-1",
			Channel:    7,
			Attached:   true,
			SurfaceID:  "surface-main",
			ViewID:     state.TerminalPaneViewID(state.DefaultPaneID),
		}.Resize(118, 38),
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-1",
			Cols:       118,
			Rows:       38,
		}.Resize(118, 38),
		TerminalViews: state.TerminalViewStore{}.BindPane(state.NewPaneTerminalView(
			state.DefaultPaneID,
			"term-1",
			7,
			118,
			38,
			state.TerminalResizeRoleOwner,
			"surface-main",
			state.TerminalPaneViewID(state.DefaultPaneID),
			true,
		)),
	}

	next, effects := reducer(root, LiveResizeMsg{
		TerminalID: "term-1",
		ViewID:     state.TerminalPaneViewID("pane-2"),
		Cols:       64,
		Rows:       38,
		Seq:        3,
	})
	if len(effects) != 0 {
		t.Fatalf("detached view resize request should be dropped, got %#v", effects)
	}
	if len(terminal.Resizes) != 0 {
		t.Fatalf("detached view resize request must not reuse current session channel, got %#v", terminal.Resizes)
	}
	if next.Session.Cols != 118 || next.Session.Rows != 38 {
		t.Fatalf("detached view resize request must not change session, got %#v", next.Session)
	}
}
