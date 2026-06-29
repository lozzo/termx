package app

import (
	"context"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestCopyModeCtrlVRequestsLatestHistoryAndConsumesTerminalInput(t *testing.T) {
	core := &services.FakeCoreClient{
		LatestResult: services.HistoryResult{
			Window: state.HistoryWindow{
				PaneID:     state.DefaultPaneID,
				ViewID:     state.TerminalPaneViewID(state.DefaultPaneID),
				TerminalID: "term-1",
				Token:      "tok-1",
				Op:         state.HistoryWindowReplace,
				Cols:       80,
				Rows: []state.HistoryRow{
					{Text: "alpha", LineID: 1},
					{Text: "beta", LineID: 2},
				},
				Cursor:     state.HistoryCursor{Valid: true, BeforeLineID: 1},
				Boundary:   state.HistoryBoundary{FirstLineID: 1, LastLineID: 2},
				Generation: 7,
			},
		},
	}
	terminal := &services.FakeTerminalService{}
	root := state.Root{
		Viewport: state.ViewportStore{Cols: 100, Rows: 30, Valid: true},
		Shell:    state.DefaultShell(),
	}
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 9, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true))
	root.Session = root.Session.AttachWithResizeOwner("term-1", 9, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID))
	reducer := ComposeReducers(
		NewCopyModeReducer(CopyModeDeps{Core: core}),
		NewTerminalInputRouterReducer(LiveDeps{Terminal: terminal}),
	)

	next, effects := reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x16", Ctrl: true}})
	if len(effects) != 1 {
		t.Fatalf("expected latest history effect only, got %#v", effects)
	}
	result, ok := effects[0].(FuncEffect).Run(context.Background()).(CopyModeHistoryResultMsg)
	if !ok {
		t.Fatalf("expected copy mode history result, got %#v", effects[0])
	}
	if len(core.LatestRequests) != 1 || core.LatestRequests[0].TerminalID != "term-1" {
		t.Fatalf("expected latest request for active terminal, got %#v", core.LatestRequests)
	}
	next, effects = reducer(next, result)
	if len(effects) != 0 {
		t.Fatalf("history result should not emit effects, got %#v", effects)
	}
	if !next.CopyMode.Active || len(next.History.Rows) != 2 {
		t.Fatalf("expected active copy history session, got copy=%#v history=%#v", next.CopyMode, next.History)
	}

	next, effects = reducer(next, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "x"}})
	if len(effects) != 0 {
		t.Fatalf("copy mode should consume ordinary terminal key without service effects, got %#v", effects)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("copy mode must not send ordinary key to terminal, got %#v", terminal.Inputs)
	}
}

func TestCopyModeCopyCurrentRowWritesClipboardStore(t *testing.T) {
	clipboard := &services.FakeClipboardService{}
	root := state.Root{
		Shell: state.DefaultShell(),
		History: state.HistoryStore{
			PaneID:     state.DefaultPaneID,
			ViewID:     state.TerminalPaneViewID(state.DefaultPaneID),
			TerminalID: "term-1",
			Token:      "tok-1",
			Rows: []state.HistoryRow{
				{Text: "alpha", LineID: 1},
				{Text: "beta", LineID: 2},
			},
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			PaneID:     state.DefaultPaneID,
			ViewID:     state.TerminalPaneViewID(state.DefaultPaneID),
			TerminalID: "term-1",
			BoundToken: "tok-1",
			Cursor:     state.CopyPosition{Row: 1},
			ViewRows:   10,
		},
	}
	root = root.WithCopyHistorySession(state.TerminalPaneViewID(state.DefaultPaneID), root.History, root.CopyMode)
	root.TerminalViews = root.TerminalViews.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 9, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true))
	reducer := NewCopyModeReducer(CopyModeDeps{Clipboard: clipboard})

	next, effects := reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "y"}})
	copyEffect, ok := firstFuncEffectForTest(effects)
	if !ok {
		t.Fatalf("expected clipboard write effect, got %#v", effects)
	}
	result := copyEffect.Run(context.Background()).(CopyModeCopyResultMsg)
	next, effects = reducer(next, result)
	if len(effects) != 1 {
		t.Fatalf("expected clipboard persist request effect, got %#v", effects)
	}
	if got := clipboard.LastCopy(); got != "beta" {
		t.Fatalf("expected system clipboard beta, got %q", got)
	}
	if len(next.Clipboard.Entries) == 0 || next.Clipboard.Entries[0].Text != "beta" {
		t.Fatalf("expected reducer clipboard history entry, got %#v", next.Clipboard)
	}
}

func firstFuncEffectForTest(effects []Effect) (FuncEffect, bool) {
	for _, effect := range effects {
		fn, ok := effect.(FuncEffect)
		if ok {
			return fn, true
		}
	}
	return FuncEffect{}, false
}
