package app

import (
	"context"
	"reflect"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestCopyModePageUpLatestAndOlderE2E(t *testing.T) {
	core := &services.FakeCoreClient{
		LatestResponses: []services.HistoryResult{{Window: historyWindowForApp(
			state.HistoryWindowReplace,
			"term-1",
			"tok-1",
			78,
			7,
			[]state.HistoryRow{{Text: "new", LineID: 20}},
		)}},
		OlderResponses: []services.HistoryResult{{Window: historyWindowForApp(
			state.HistoryWindowPrepend,
			"term-1",
			"tok-1",
			78,
			7,
			[]state.HistoryRow{{Text: "old", LineID: 10}},
		)}},
	}
	core.LatestResponses[0].Window.Cursor = state.HistoryCursor{Valid: true, BeforeLineID: 20}
	core.OlderResponses[0].Window.Cursor = state.HistoryCursor{Valid: true, BeforeLineID: 10}
	core.OlderResponses[0].Window.Boundary = state.HistoryBoundary{FirstLineID: 10, LastLineID: 20}
	host := NewFakeTerminalHost(8)
	runtime := newCopyModeRuntime(host, core, nil)

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send page up: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain latest: %v", err)
	}
	if len(core.LatestRequests) != 1 || core.LatestRequests[0].TerminalID != "term-1" || core.LatestRequests[0].Cols != 78 {
		t.Fatalf("expected latest request, got %#v", core.LatestRequests)
	}
	if runtime.State().History.Token != "tok-1" || runtime.State().CopyMode.BoundToken != "tok-1" {
		t.Fatalf("state did not accept latest response %#v", runtime.State())
	}

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send older page up: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain older: %v", err)
	}
	if len(core.OlderRequests) != 1 {
		t.Fatalf("expected older request, got %#v", core.OlderRequests)
	}
	olderReq := core.OlderRequests[0]
	if olderReq.Token != "tok-1" || olderReq.Generation != 7 || olderReq.Boundary.LastLineID != 20 {
		t.Fatalf("unexpected older request %#v", olderReq)
	}
	if got := historyRowTexts(runtime.State().History.Rows); !reflect.DeepEqual(got, []string{"old", "new"}) {
		t.Fatalf("unexpected history rows %v", got)
	}
	if runtime.State().CopyMode.ViewportTop != 0 {
		t.Fatalf("viewport should clamp to top when visible rows cover loaded history, got %#v", runtime.State().CopyMode)
	}
	last := lastFrame(t, host.Frames())
	if len(last.Lines) == 0 || !frameContains(last, "old") || !frameContains(last, "new") {
		t.Fatalf("expected latest rendered copy frame to start with older row, got %#v", last.Lines)
	}
	if !frameContains(last, "● old") || !frameContains(last, "● new") {
		t.Fatalf("expected copy-history logical line markers in frame, got %#v", last.Lines)
	}
}

func TestCopyModeMouseWheelRequestsOlderAfterLatest(t *testing.T) {
	core := &services.FakeCoreClient{
		LatestResponses: []services.HistoryResult{{Window: historyWindowForApp(
			state.HistoryWindowReplace,
			"term-1",
			"tok-1",
			78,
			7,
			[]state.HistoryRow{{Text: "new", LineID: 20}},
		)}},
		OlderResponses: []services.HistoryResult{{Window: historyWindowForApp(
			state.HistoryWindowPrepend,
			"term-1",
			"tok-1",
			78,
			7,
			[]state.HistoryRow{{Text: "old", LineID: 10}},
		)}},
	}
	core.LatestResponses[0].Window.Cursor = state.HistoryCursor{Valid: true, BeforeLineID: 20}
	core.OlderResponses[0].Window.Cursor = state.HistoryCursor{Valid: true, BeforeLineID: 10}
	core.OlderResponses[0].Window.Boundary = state.HistoryBoundary{FirstLineID: 10, LastLineID: 20}
	host := NewFakeTerminalHost(8)
	runtime := newCopyModeRuntime(host, core, nil)

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindMouse, Mouse: input.MouseWheelUp}); err != nil {
		t.Fatalf("send wheel latest: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain latest: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindMouse, Mouse: input.MouseWheelUp}); err != nil {
		t.Fatalf("send wheel older: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain older: %v", err)
	}
	if len(core.LatestRequests) != 1 || len(core.OlderRequests) != 1 {
		t.Fatalf("unexpected history requests latest=%#v older=%#v", core.LatestRequests, core.OlderRequests)
	}
}

func TestCopyModeOlderBoundaryTokensAndExhaustedGuard(t *testing.T) {
	latest := historyWindowForApp(
		state.HistoryWindowReplace,
		"term-1",
		"tok-1",
		78,
		7,
		[]state.HistoryRow{{Text: "new", LineID: 20}},
	)
	latest.Cursor = state.HistoryCursor{Valid: true, BeforeLineID: 20}
	latest.HasMore = true
	exhausted := historyWindowForApp(state.HistoryWindowPrepend, "term-1", "tok-1", 78, 7, nil)
	exhausted.Cursor = latest.Cursor
	exhausted.HasMore = false
	core := &services.FakeCoreClient{
		LatestResponses: []services.HistoryResult{{Window: latest}},
		OlderResponses:  []services.HistoryResult{{Window: exhausted}},
	}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 8)
	runtime := newCopyModeRuntime(host, core, nil)

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send latest page up: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain latest: %v", err)
	}
	if frame := lastFrame(t, host.Frames()); !frameContains(frame, "↑ more") {
		t.Fatalf("latest window with cursor should show older-more token, got %#v", frame.Lines)
	}

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send exhausted older page up: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain exhausted older: %v", err)
	}
	if frame := lastFrame(t, host.Frames()); !frameContains(frame, "↑ top") {
		t.Fatalf("exhausted older window should show top token, got %#v", frame.Lines)
	}

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send redundant older page up: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain redundant older: %v", err)
	}
	if len(core.OlderRequests) != 1 {
		t.Fatalf("exhausted boundary must not request older again, got %#v", core.OlderRequests)
	}
}

func TestCopyModeOlderGuardSilentlyBlocksAnyPendingHistoryRequest(t *testing.T) {
	reducer := NewCopyModeReducer(CopyModeDeps{Core: &services.FakeCoreClient{}, Rows: 20})
	root := state.Root{
		Session: state.TerminalSessionStore{TerminalID: "term-1", Attached: true, Cols: 80, Rows: 24},
		Shell:   state.DefaultShell(),
		TerminalViews: state.TerminalViewStore{}.BindPane(state.NewPaneTerminalView(
			state.DefaultPaneID, "term-1", 4, 78, 20, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true,
		)),
		Viewport: state.ViewportStore{
			Valid: true,
			Cols:  80,
			Rows:  24,
		},
		History: state.HistoryStore{
			PaneID:     state.DefaultPaneID,
			ViewID:     state.TerminalPaneViewID(state.DefaultPaneID),
			TerminalID: "term-1",
			Token:      "tok-1",
			Cols:       78,
			Generation: 7,
			Cursor:     state.HistoryCursor{Valid: true, BeforeLineID: 20},
			Boundary:   state.HistoryBoundary{FirstLineID: 20, LastLineID: 20},
			Rows:       []state.HistoryRow{{Text: "new", LineID: 20}},
			Pending: &state.HistoryPendingRequest{
				ID:         9,
				Kind:       state.HistoryRequestLatest,
				TerminalID: "term-1",
				Cols:       78,
			},
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			TerminalID: "term-1",
			BoundToken: "tok-1",
			BoundCols:  78,
		},
	}

	next, effects := reducer(root, InputMsg{Event: input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}})
	if len(effects) != 1 {
		t.Fatalf("pending guard should only return handled effect, got %#v", effects)
	}
	if _, ok := effects[0].(handledEffect); !ok {
		t.Fatalf("pending guard should keep input handled, got %#v", effects[0])
	}
	if next.Session.LastError != "" || next.Surface.Err != "" {
		t.Fatalf("pending guard should stay silent, session=%q surface=%q", next.Session.LastError, next.Surface.Err)
	}
	if next.History.Pending == nil || next.History.Pending.Kind != state.HistoryRequestLatest {
		t.Fatalf("pending latest request should remain unchanged, got %#v", next.History.Pending)
	}
}

func TestCopyModeFooterOlderActionUsesAuthoritativeHistoryPath(t *testing.T) {
	core := &services.FakeCoreClient{
		LatestResponses: []services.HistoryResult{{Window: historyWindowForApp(
			state.HistoryWindowReplace,
			"term-1",
			"tok-1",
			78,
			7,
			[]state.HistoryRow{{Text: "new", LineID: 20}},
		)}},
		OlderResponses: []services.HistoryResult{{Window: historyWindowForApp(
			state.HistoryWindowPrepend,
			"term-1",
			"tok-1",
			78,
			7,
			[]state.HistoryRow{{Text: "old", LineID: 10}},
		)}},
	}
	core.LatestResponses[0].Window.Cursor = state.HistoryCursor{Valid: true, BeforeLineID: 20}
	core.OlderResponses[0].Window.Cursor = state.HistoryCursor{Valid: true, BeforeLineID: 10}
	core.OlderResponses[0].Window.Boundary = state.HistoryBoundary{FirstLineID: 10, LastLineID: 20}
	host := NewFakeTerminalHost(8)
	terminal := &services.FakeTerminalService{}
	runtime := NewInteractiveRuntime(
		state.Root{
			Shell: state.DefaultShell(),
			Session: state.TerminalSessionStore{
				TerminalID: "term-1",
				Attached:   true,
				Cols:       80,
				Rows:       24,
			},
			Surface: state.TerminalSurfaceStore{
				TerminalID: "term-1",
				Cols:       80,
				Rows:       24,
				Lines:      []string{"live"},
			},
			TerminalViews: state.TerminalViewStore{}.BindPane(state.NewPaneTerminalView(
				state.DefaultPaneID, "term-1", 4, 78, 20, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true,
			)),
		},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: core, Rows: 20},
	)
	host.SetSize(80, 24)

	if err := runtime.Post(ShellContentActionMsg{ActionID: render.ActionCopyOlder.String()}); err != nil {
		t.Fatalf("post copy footer latest action: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain copy footer latest action: %v", err)
	}
	if len(core.LatestRequests) != 1 || core.LatestRequests[0].TerminalID != "term-1" || core.LatestRequests[0].Cols != 78 {
		t.Fatalf("copy footer action should enter latest through copy reducer, got %#v", core.LatestRequests)
	}

	action := frameActionHitRegion(t, lastFrame(t, host.Frames()), render.ActionCopyOlder.String(), "")
	if err := host.SendInput(mouseEventAt(action.Rect)); err != nil {
		t.Fatalf("send copy footer older click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain copy footer older click: %v", err)
	}
	if len(core.OlderRequests) != 1 {
		t.Fatalf("expected copy footer older request, got %#v", core.OlderRequests)
	}
	olderReq := core.OlderRequests[0]
	if olderReq.Token != "tok-1" || olderReq.Generation != 7 || olderReq.Boundary.LastLineID != 20 {
		t.Fatalf("unexpected copy footer older request %#v", olderReq)
	}
	if got := historyRowTexts(runtime.State().History.Rows); !reflect.DeepEqual(got, []string{"old", "new"}) {
		t.Fatalf("copy footer older should prepend authoritative rows, got %v", got)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("copy footer older action must not leak to terminal input, got %#v", terminal.Inputs)
	}
}

func TestCopyModeLatestUsesCopyContentRectCols(t *testing.T) {
	core := &services.FakeCoreClient{
		LatestResponses: []services.HistoryResult{{Window: historyWindowForApp(
			state.HistoryWindowReplace,
			"term-1",
			"tok-1",
			78,
			7,
			[]state.HistoryRow{{Text: "new", LineID: 20}},
		)}},
	}
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	runtime := newCopyModeRuntime(host, core, nil)

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send page up: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain latest: %v", err)
	}

	if len(core.LatestRequests) != 1 || core.LatestRequests[0].Cols != 78 {
		t.Fatalf("copy latest must use content rect cols, got %#v", core.LatestRequests)
	}
	if runtime.State().CopyMode.BoundCols != 78 || runtime.State().History.Cols != 78 {
		t.Fatalf("expected copy/history bound to content cols, got %#v", runtime.State())
	}
}

func TestCopyModeHostResizeRebindsLatestAndDoesNotRenderOldWindow(t *testing.T) {
	core := &services.FakeCoreClient{
		LatestResponses: []services.HistoryResult{
			{Window: historyWindowForApp(
				state.HistoryWindowReplace,
				"term-1",
				"tok-1",
				78,
				7,
				[]state.HistoryRow{{Text: "old-window", LineID: 20}},
			)},
			{Window: historyWindowForApp(
				state.HistoryWindowReplace,
				"term-1",
				"tok-2",
				98,
				8,
				[]state.HistoryRow{{Text: "new-window", LineID: 30}},
			)},
		},
	}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := newCopyModeRuntime(host, core, nil)

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send page up: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain latest: %v", err)
	}
	if err := runtime.Post(CopyModeSetMarkMsg{Position: state.CopyPosition{Row: 0, Col: 1}}); err != nil {
		t.Fatalf("post mark: %v", err)
	}
	if err := runtime.Post(CopyModeMoveCursorMsg{Position: state.CopyPosition{Row: 0, Col: 4}}); err != nil {
		t.Fatalf("post cursor: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain selection: %v", err)
	}

	if err := host.SendResize(100, 40); err != nil {
		t.Fatalf("send resize: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain rebind: %v", err)
	}

	if len(core.LatestRequests) != 2 || core.LatestRequests[1].Cols != 98 {
		t.Fatalf("expected second latest request at resized content cols, got %#v", core.LatestRequests)
	}
	if runtime.State().CopyMode.BoundToken != "tok-2" || runtime.State().CopyMode.BoundCols != 98 {
		t.Fatalf("expected rebound copy mode, got %#v", runtime.State().CopyMode)
	}
	if runtime.State().CopyMode.Mark != nil || runtime.State().CopyMode.Selection != nil || runtime.State().CopyMode.Cursor != (state.CopyPosition{}) {
		t.Fatalf("resize rebind must clear selection and cursor, got %#v", runtime.State().CopyMode)
	}
	last := lastFrame(t, host.Frames())
	if frameContains(last, "old-window") || !frameContains(last, "new-window") {
		t.Fatalf("resized copy mode must render new authoritative window only, got %#v", last.Lines)
	}
}

func TestCopyModeResizeRebindInvalidatesOldWindowBeforeLatestResponse(t *testing.T) {
	reducer := NewCopyModeResizeRebindReducer(CopyModeDeps{Core: &services.FakeCoreClient{}, Rows: 20})
	root := state.Root{
		Session: state.TerminalSessionStore{TerminalID: "term-1", Attached: true, Cols: 80, Rows: 24},
		Shell:   state.DefaultShell(),
		TerminalViews: state.TerminalViewStore{}.BindPane(state.NewPaneTerminalView(
			state.DefaultPaneID, "term-1", 4, 78, 20, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true,
		)),
		Viewport: state.ViewportStore{
			Valid: true,
			Cols:  100,
			Rows:  40,
		},
		History: state.HistoryStore{
			TerminalID: "term-1",
			Token:      "tok-old",
			Cols:       78,
			Rows:       []state.HistoryRow{{Text: "old-window", LineID: 20}},
			Lines:      []state.HistoryLineSpan{{LineID: 20, StartRow: 0, EndRow: 0}},
			Cursor:     state.HistoryCursor{Valid: true, BeforeLineID: 20},
			Generation: 7,
			Boundary:   state.HistoryBoundary{FirstLineID: 20, LastLineID: 20},
			HasMore:    true,
		},
		CopyMode: state.CopyModeStore{
			Active:      true,
			PaneID:      state.DefaultPaneID,
			ViewID:      state.TerminalPaneViewID(state.DefaultPaneID),
			TerminalID:  "term-1",
			BoundToken:  "tok-old",
			BoundCols:   78,
			ViewRows:    20,
			ViewportTop: 4,
			Cursor:      state.CopyPosition{Row: 0, Col: 3},
		},
	}
	mark := state.CopyPosition{Row: 0, Col: 1}
	root.CopyMode.Mark = &mark
	root.CopyMode.Selection = &state.CopySelection{Anchor: mark, Focus: root.CopyMode.Cursor}

	next, effects := reducer(root, HostResizeMsg{Cols: 100, Rows: 40})
	if len(effects) != 1 {
		t.Fatalf("expected one latest rebind effect, got %#v", effects)
	}
	if next.History.Token != "" || next.History.Cols != 0 || len(next.History.Rows) != 0 || len(next.History.Lines) != 0 {
		t.Fatalf("resize rebind must immediately invalidate old authoritative window, got %#v", next.History)
	}
	if next.History.Pending == nil || next.History.Pending.Kind != state.HistoryRequestLatest || next.History.Pending.Cols != 98 {
		t.Fatalf("resize rebind should wait for latest at new cols, got %#v", next.History.Pending)
	}
	if next.CopyMode.BoundToken != "" || next.CopyMode.BoundCols != 98 || next.CopyMode.ViewRows != 36 || next.CopyMode.ViewportTop != 0 {
		t.Fatalf("copy mode should enter explicit pending binding at new rect, got %#v", next.CopyMode)
	}
	if next.CopyMode.Cursor != (state.CopyPosition{}) || next.CopyMode.Mark != nil || next.CopyMode.Selection != nil {
		t.Fatalf("resize rebind must reset cursor and selection, got %#v", next.CopyMode)
	}
}

func TestCopyModeResizeRebindPendingFrameDoesNotShowOldRowsOrLiveFallback(t *testing.T) {
	root := state.Root{
		Session: state.TerminalSessionStore{TerminalID: "term-1", Attached: true, Cols: 80, Rows: 24},
		Shell:   state.DefaultShell(),
		TerminalViews: state.TerminalViewStore{}.BindPane(state.NewPaneTerminalView(
			state.DefaultPaneID, "term-1", 4, 78, 20, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true,
		)),
		Surface: state.TerminalSurfaceStore{
			TerminalID: "term-1",
			Lines:      []string{"live-fallback"},
		},
		Viewport: state.ViewportStore{
			Valid: true,
			Cols:  100,
			Rows:  40,
		},
		History: state.HistoryStore{
			PaneID:     state.DefaultPaneID,
			ViewID:     state.TerminalPaneViewID(state.DefaultPaneID),
			TerminalID: "term-1",
			Token:      "tok-old",
			Cols:       78,
			Rows:       []state.HistoryRow{{Text: "old-window", LineID: 20}},
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			PaneID:     state.DefaultPaneID,
			ViewID:     state.TerminalPaneViewID(state.DefaultPaneID),
			TerminalID: "term-1",
			BoundToken: "tok-old",
			BoundCols:  78,
			ViewRows:   20,
		},
	}
	reducer := NewCopyModeResizeRebindReducer(CopyModeDeps{Core: &services.FakeCoreClient{}, Rows: 20})
	root, _ = reducer(root, HostResizeMsg{Cols: 100, Rows: 40})
	frame := render.NewRenderer(render.DefaultTheme()).Render(render.NewRenderVMBuilder().Build(root))

	if !frameContains(frame, "copy history pending: authoritative history window pending") {
		t.Fatalf("resize rebind should render pending history state, got %#v", frame.Lines)
	}
	if frameContains(frame, "old-window") || frameContains(frame, "live-fallback") {
		t.Fatalf("resize rebind pending frame must not show old history or live fallback, got %#v", frame.Lines)
	}
}

func TestCopyModeResizeRebindRuntimeRendersPendingBeforeLatestResponse(t *testing.T) {
	runner := &recordingEffectRunner{}
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	runtime := newCopyModeRuntimeWithRunner(host, &services.FakeCoreClient{}, nil, runner)
	runtime.state.Surface.Lines = []string{"live-fallback"}
	runtime.state.History = state.HistoryStore{
		PaneID:     state.DefaultPaneID,
		ViewID:     state.TerminalPaneViewID(state.DefaultPaneID),
		TerminalID: "term-1",
		Token:      "tok-old",
		Cols:       78,
		Rows:       []state.HistoryRow{{Text: "old-window", LineID: 20}},
	}
	runtime.state.CopyMode = state.CopyModeStore{
		Active:     true,
		PaneID:     state.DefaultPaneID,
		ViewID:     state.TerminalPaneViewID(state.DefaultPaneID),
		TerminalID: "term-1",
		BoundToken: "tok-old",
		BoundCols:  78,
		ViewRows:   20,
	}

	if err := host.SendResize(100, 40); err != nil {
		t.Fatalf("send resize: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain resize: %v", err)
	}

	if len(runner.Effects) != 1 {
		t.Fatalf("resize rebind should schedule latest request without executing it, got %#v", runner.Effects)
	}
	pendingFrame := lastFrame(t, host.Frames())
	if !frameContains(pendingFrame, "copy history pending: authoritative history window pending") {
		t.Fatalf("runtime should render pending frame before latest response, got %#v", pendingFrame.Lines)
	}
	if frameContains(pendingFrame, "old-window") || frameContains(pendingFrame, "live-fallback") {
		t.Fatalf("pending runtime frame must not show old history or live fallback, got %#v", pendingFrame.Lines)
	}
	if runtime.State().History.Pending == nil || runtime.State().History.Pending.Cols != 98 {
		t.Fatalf("runtime should keep pending latest request at resized cols, got %#v", runtime.State().History.Pending)
	}
}

func TestCopyModeResizeRowsOnlyKeepsWindowAndDoesNotRequestLatest(t *testing.T) {
	reducer := NewCopyModeResizeRebindReducer(CopyModeDeps{Core: &services.FakeCoreClient{}, Rows: 20})
	root := state.Root{
		Session: state.TerminalSessionStore{TerminalID: "term-1", Attached: true, Cols: 80, Rows: 24},
		Shell:   state.DefaultShell(),
		TerminalViews: state.TerminalViewStore{}.BindPane(state.NewPaneTerminalView(
			state.DefaultPaneID, "term-1", 4, 78, 20, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true,
		)),
		Viewport: state.ViewportStore{
			Valid: true,
			Cols:  80,
			Rows:  18,
		},
		History: state.HistoryStore{
			TerminalID: "term-1",
			Token:      "tok-1",
			Cols:       78,
			Rows: []state.HistoryRow{
				{Text: "one", LineID: 1},
				{Text: "two", LineID: 2},
				{Text: "three", LineID: 3},
				{Text: "four", LineID: 4},
			},
		},
		CopyMode: state.CopyModeStore{
			Active:      true,
			TerminalID:  "term-1",
			BoundToken:  "tok-1",
			BoundCols:   78,
			ViewRows:    20,
			ViewportTop: 3,
			Cursor:      state.CopyPosition{Row: 2, Col: 1},
		},
	}

	next, effects := reducer(root, HostResizeMsg{Cols: 80, Rows: 18})
	if len(effects) != 0 {
		t.Fatalf("rows-only resize must not request latest, got %#v", effects)
	}
	if next.History.Token != "tok-1" || next.History.Cols != 78 || len(next.History.Rows) != 4 {
		t.Fatalf("rows-only resize should preserve authoritative window, got %#v", next.History)
	}
	if next.CopyMode.BoundToken != "tok-1" || next.CopyMode.BoundCols != 78 || next.CopyMode.ViewRows != 14 {
		t.Fatalf("rows-only resize should only update view rows, got %#v", next.CopyMode)
	}
	if next.CopyMode.Cursor != (state.CopyPosition{Row: 2, Col: 1}) {
		t.Fatalf("rows-only resize should preserve cursor, got %#v", next.CopyMode.Cursor)
	}
	if next.CopyMode.ViewportTop != 0 {
		t.Fatalf("rows-only resize should clamp viewport to the loaded top when all rows fit, got %#v", next.CopyMode)
	}
}

func TestCopyModePaneSizeCommandRebindsLatestAtContentCols(t *testing.T) {
	core := &services.FakeCoreClient{
		LatestResponses: []services.HistoryResult{
			{Window: historyWindowForApp(state.HistoryWindowReplace, "term-1", "tok-1", 39, 7, []state.HistoryRow{{Text: "old-window", LineID: 20}})},
			{Window: historyWindowForApp(state.HistoryWindowReplace, "term-1", "tok-2", 22, 8, []state.HistoryRow{{Text: "sized-window", LineID: 30}})},
		},
	}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	initialShell := state.DefaultShell().
		SetPanelPresentation(state.PanelPresentationSplitLine).
		SplitActivePane(state.PaneState{ID: "pane-2", Kind: state.PaneTerminalLive}, state.SplitDirectionVertical)
	runtime := NewInteractiveRuntime(
		state.Root{Shell: initialShell},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: &services.FakeTerminalService{AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 4}}},
		CopyModeDeps{Core: core, Rows: 20},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send page up: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain latest: %v", err)
	}
	if err := runtime.Post(ShellPaneCommandMsg{Command: state.PaneCommand{
		Action:   state.PaneCommandSetSize,
		Target:   state.PaneCommandTarget{PaneID: "pane-2"},
		SizeMode: state.PaneSizeCells,
		Cols:     24,
	}}); err != nil {
		t.Fatalf("post pane size command: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain pane size rebind: %v", err)
	}

	if len(core.LatestRequests) != 2 || core.LatestRequests[1].Cols != 22 {
		t.Fatalf("pane size command must rebind copy mode at active content cols, got %#v", core.LatestRequests)
	}
	if runtime.State().CopyMode.BoundToken != "tok-2" || runtime.State().CopyMode.BoundCols != 22 {
		t.Fatalf("expected copy mode rebound to sized window, got %#v", runtime.State().CopyMode)
	}
	if runtime.State().History.Token != "tok-2" || runtime.State().History.Cols != 22 || len(runtime.State().History.Rows) != 1 || runtime.State().History.Rows[0].Text != "sized-window" {
		t.Fatalf("pane size rebind must replace authoritative history window, got %#v", runtime.State().History)
	}
}

func TestInteractiveRuntimeHostResizeKeepsReboundCopyWindowAfterTerminalResizeResult(t *testing.T) {
	terminal := &services.FakeTerminalService{AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 4, Cols: 78, Rows: 20}}
	core := &services.FakeCoreClient{
		LatestResponses: []services.HistoryResult{
			{Window: historyWindowForApp(state.HistoryWindowReplace, "term-1", "tok-1", 78, 7, []state.HistoryRow{{Text: "old-window", LineID: 20}})},
			{Window: historyWindowForApp(state.HistoryWindowReplace, "term-1", "tok-2", 98, 8, []state.HistoryRow{{Text: "new-window", LineID: 30}})},
		},
	}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: core, Rows: 20},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send page up: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain latest: %v", err)
	}
	if err := host.SendResize(100, 40); err != nil {
		t.Fatalf("send resize: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain resize/rebind: %v", err)
	}

	if len(terminal.Resizes) != 1 || terminal.Resizes[0].Cols != 98 || terminal.Resizes[0].Rows != 36 {
		t.Fatalf("expected terminal resize to content rect, got %#v", terminal.Resizes)
	}
	if runtime.State().CopyMode.BoundToken != "tok-2" || runtime.State().History.Token != "tok-2" {
		t.Fatalf("terminal resize result must not clear rebound copy window, got %#v", runtime.State())
	}
}

func TestCopyModeResizeRejectsOldColsResponseAsStale(t *testing.T) {
	core := &services.FakeCoreClient{}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := newCopyModeRuntime(host, core, nil)
	runtime.state.CopyMode = state.CopyModeStore{Active: true, PaneID: state.DefaultPaneID, ViewID: state.TerminalPaneViewID(state.DefaultPaneID), TerminalID: "term-1", BoundToken: "tok-1", BoundCols: 78}
	runtime.state.History = state.HistoryStore{
		PaneID:     state.DefaultPaneID,
		ViewID:     state.TerminalPaneViewID(state.DefaultPaneID),
		TerminalID: "term-1",
		Token:      "tok-1",
		Cols:       78,
		Rows:       []state.HistoryRow{{Text: "old-window", LineID: 20}},
	}

	if err := host.SendResize(100, 40); err != nil {
		t.Fatalf("send resize: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain resize: %v", err)
	}
	pending := runtime.State().History.Pending
	if pending == nil || pending.Cols != 98 {
		t.Fatalf("expected pending rebind request at new cols, got %#v", runtime.State().History)
	}
	if err := runtime.Post(CopyModeHistoryResultMsg{Result: services.HistoryResult{
		RequestID: services.RequestID(pending.ID),
		Window:    historyWindowForApp(state.HistoryWindowReplace, "term-1", "tok-old", 78, 7, []state.HistoryRow{{Text: "stale", LineID: 1}}),
	}}); err != nil {
		t.Fatalf("post stale response: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain stale response: %v", err)
	}

	if runtime.State().History.Token != "" || len(runtime.State().History.Rows) != 0 {
		t.Fatalf("old cols response must not restore stale window, got %#v", runtime.State().History)
	}
	if runtime.State().Session.LastError == "" {
		t.Fatalf("expected stale cols error, got %#v", runtime.State())
	}
	last := lastFrame(t, host.Frames())
	if frameContains(last, "old-window") || frameContains(last, "│stale") {
		t.Fatalf("stale resize response must not render old rows, got %#v", last.Lines)
	}
}

func TestInteractiveRuntimeRoutesTerminalInputAndCopyModeInput(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 4, Cols: 80, Rows: 24},
	}
	core := &services.FakeCoreClient{
		LatestResponses: []services.HistoryResult{{Window: historyWindowForApp(
			state.HistoryWindowReplace,
			"term-1",
			"tok-1",
			78,
			7,
			[]state.HistoryRow{{Text: "copy-row", LineID: 20}},
		)}},
	}
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: core, Rows: 20},
	)

	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "x"}); err != nil {
		t.Fatalf("send terminal char: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain terminal input: %v", err)
	}
	if len(terminal.Inputs) != 1 || string(terminal.Inputs[0].Bytes) != "x" {
		t.Fatalf("expected terminal input, got %#v", terminal.Inputs)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send page up: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain copy mode: %v", err)
	}
	if len(core.LatestRequests) != 1 {
		t.Fatalf("expected history latest request, got %#v", core.LatestRequests)
	}
	if len(terminal.Inputs) != 1 {
		t.Fatalf("copy mode page up should not be terminal input %#v", terminal.Inputs)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEsc}); err != nil {
		t.Fatalf("send esc: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain esc: %v", err)
	}
	if runtime.State().CopyMode.Active {
		t.Fatalf("expected copy mode to exit %#v", runtime.State().CopyMode)
	}
	if len(terminal.Inputs) != 1 {
		t.Fatalf("copy mode esc should not be terminal input %#v", terminal.Inputs)
	}
}

func TestInteractiveRuntimeCopyModeSwallowsUnboundRawKeys(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 4, Cols: 80, Rows: 24},
	}
	core := &services.FakeCoreClient{
		LatestResponses: []services.HistoryResult{{Window: historyWindowForApp(
			state.HistoryWindowReplace,
			"term-1",
			"tok-1",
			78,
			7,
			[]state.HistoryRow{{Text: "copy-row", LineID: 20}},
		)}},
	}
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: core, Rows: 20},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("enter copy mode: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain copy mode: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyF5, RawSeq: "\x1b[15~"}); err != nil {
		t.Fatalf("send copy unbound raw key: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain copy unbound raw key: %v", err)
	}
	if len(terminal.Inputs) != 0 {
		t.Fatalf("copy mode unbound raw key must not leak to terminal, got %#v", terminal.Inputs)
	}
}

func TestInteractiveRuntimePassesRawSpecialKeysAndSwallowsUIModeUnboundKeys(t *testing.T) {
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{TerminalID: "term-1", Channel: 4, Cols: 80, Rows: 24},
	}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	runtime := NewInteractiveRuntime(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
	)
	if err := runtime.Post(LiveAttachMsg{Config: LiveConfig{TerminalID: "term-1", Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("post attach: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain attach: %v", err)
	}
	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyLeft, RawSeq: "\x1b[D"},
		{Kind: input.EventKindKey, Key: input.KeyDelete, RawSeq: "\x1b[3~"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x03", Ctrl: true, RawSeq: "\x03"},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "x", Alt: true, RawSeq: "\x1bx"},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send raw key %#v: %v", event, err)
		}
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain raw keys: %v", err)
	}
	got := make([]string, 0, len(terminal.Inputs))
	for _, req := range terminal.Inputs {
		got = append(got, string(req.Bytes))
	}
	want := []string{"\x1b[D", "\x1b[3~", "\x03", "\x1bx"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected raw key passthrough got=%q want=%q", got, want)
	}

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: "\x10", Ctrl: true}); err != nil {
		t.Fatalf("enter pane mode: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyDelete, RawSeq: "\x1b[3~"}); err != nil {
		t.Fatalf("send ui unbound key: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain ui unbound: %v", err)
	}
	if len(terminal.Inputs) != len(want) {
		t.Fatalf("ui mode unbound key must not leak to terminal, got %#v", terminal.Inputs)
	}
}

func TestCopyModeSelectionCopiesAuthoritativeRows(t *testing.T) {
	clipboard := &services.FakeClipboardService{}
	host := NewFakeTerminalHost(4)
	runtime := newCopyModeRuntime(host, &services.FakeCoreClient{}, clipboard)
	runtime.state.History = historyStoreForCopySelection()
	runtime.state.CopyMode = state.CopyModeStore{
		Active:     true,
		TerminalID: "term-1",
		BoundToken: "tok-1",
		BoundCols:  78,
	}

	if err := runtime.Post(CopyModeSetMarkMsg{Position: state.CopyPosition{Row: 0, Col: 1}}); err != nil {
		t.Fatalf("post mark: %v", err)
	}
	if err := runtime.Post(CopyModeMoveCursorMsg{Position: state.CopyPosition{Row: 1, Col: 2}}); err != nil {
		t.Fatalf("post move: %v", err)
	}
	if err := runtime.Post(CopyModeCopySelectionMsg{}); err != nil {
		t.Fatalf("post copy: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(clipboard.Writes) != 1 || clipboard.Writes[0].Text != "lpha\nbe" {
		t.Fatalf("unexpected clipboard writes %#v", clipboard.Writes)
	}
	last := lastFrame(t, host.Frames())
	assertPaneVisualState(t, last, "lpha", render.StyleAccent)
	assertPaneVisualState(t, last, "be", render.StyleAccent)
	if !frameContains(last, "Copied to clipboard") || !frameContains(last, "│") || frameContains(last, "selection yanked") {
		t.Fatalf("copy feedback toast should be visible, got %#v", last.Lines)
	}
}

func TestCopyModeCanonicalKeysMoveSelectAndCopy(t *testing.T) {
	clipboard := &services.FakeClipboardService{}
	host := NewFakeTerminalHost(16)
	runtime := newCopyModeRuntime(host, &services.FakeCoreClient{}, clipboard)
	runtime.state.History = historyStoreForCopySelection()
	runtime.state.CopyMode = state.CopyModeStore{
		Active:     true,
		TerminalID: "term-1",
		BoundToken: "tok-1",
		BoundCols:  78,
		ViewRows:   4,
	}
	for _, event := range []input.InputEvent{
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "G"},
		{Kind: input.EventKindKey, Key: input.KeyHome},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: " "},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "l"},
		{Kind: input.EventKindKey, Key: input.KeyEnd},
		{Kind: input.EventKindKey, Key: input.KeyChar, Char: "y"},
	} {
		if err := host.SendInput(event); err != nil {
			t.Fatalf("send copy key %#v: %v", event, err)
		}
		if err := runtime.Drain(context.Background()); err != nil {
			t.Fatalf("drain copy key %#v: %v", event, err)
		}
	}
	if len(clipboard.Writes) != 1 || clipboard.Writes[0].Text != "beta" {
		t.Fatalf("expected y to copy selected authoritative row, got %#v", clipboard.Writes)
	}
	if runtime.State().CopyMode.Cursor != (state.CopyPosition{Row: 1, Col: 4}) {
		t.Fatalf("expected copy cursor at end of beta, got %#v", runtime.State().CopyMode.Cursor)
	}
}

func TestCopyModeSearchScrollAndMouseSelection(t *testing.T) {
	core := &services.FakeCoreClient{
		LatestResponses: []services.HistoryResult{{Window: historyWindowForApp(
			state.HistoryWindowReplace,
			"term-1",
			"tok-1",
			78,
			7,
			[]state.HistoryRow{
				{Text: "alpha", LineID: 10},
				{Text: "beta", LineID: 11},
				{Text: "gamma beta", LineID: 12},
				{Text: "delta", LineID: 13},
			},
		)}},
	}
	host := NewFakeTerminalHost(32)
	host.SetSize(80, 8)
	runtime := newCopyModeRuntime(host, core, nil)

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send copy enter: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain latest: %v", err)
	}
	for _, ch := range []string{"b", "e", "t", "a"} {
		if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyChar, Char: ch}); err != nil {
			t.Fatalf("send query %q: %v", ch, err)
		}
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain query: %v", err)
	}
	if runtime.State().CopyMode.Query != "beta" || len(runtime.State().CopyMode.Matches) != 2 || runtime.State().CopyMode.Cursor.Row != 1 {
		t.Fatalf("expected search matches and cursor on first beta, got %#v", runtime.State().CopyMode)
	}
	queryFrame := lastFrame(t, host.Frames())
	if !frameContains(queryFrame, "⌕ search beta") || !frameContains(queryFrame, "match:1/2") || !frameContains(queryFrame, "SCROLL") {
		t.Fatalf("expected search row and scrollbar, got %#v", queryFrame.Lines)
	}
	assertPaneVisualState(t, queryFrame, "beta", render.StyleWarning)

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyEnter}); err != nil {
		t.Fatalf("send next match: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain next match: %v", err)
	}
	if runtime.State().CopyMode.ActiveMatch != 1 || runtime.State().CopyMode.Cursor.Row != 2 {
		t.Fatalf("expected next search match, got %#v", runtime.State().CopyMode)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageDn}); err != nil {
		t.Fatalf("send page down: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain page down: %v", err)
	}
	if runtime.State().CopyMode.ViewportTop == 0 {
		t.Fatalf("expected copy viewport to scroll down, got %#v", runtime.State().CopyMode)
	}

	frame := lastFrame(t, host.Frames())
	var target render.HitRegion
	for _, region := range frame.HitRegions {
		if region.Kind == render.HitRegionHistoryRow && region.Row == 2 {
			target = region
			break
		}
	}
	if target.Kind == "" {
		t.Fatalf("expected visible history row hit region, got %#v", frame.HitRegions)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindMouse, Mouse: input.MouseLeft, Row: target.Rect.Y + 1, Col: target.Rect.X + 6}); err != nil {
		t.Fatalf("send row click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain row click: %v", err)
	}
	if runtime.State().CopyMode.Mark == nil || runtime.State().CopyMode.Cursor.Row != 2 {
		t.Fatalf("expected content-local mouse selection on history row, got %#v", runtime.State().CopyMode)
	}
}

func TestCopyModeMouseSelectionUsesHistoryDisplayColumns(t *testing.T) {
	core := &services.FakeCoreClient{
		LatestResponses: []services.HistoryResult{{Window: historyWindowForApp(
			state.HistoryWindowReplace,
			"term-1",
			"tok-1",
			78,
			7,
			[]state.HistoryRow{{
				Text:   "a好bc",
				LineID: 10,
				Cells: []state.HistoryCell{
					{Text: "a", Width: 1},
					{Text: "好", Width: 2},
					{Text: "bc", Width: 2},
				},
			}},
		)}},
	}
	host := NewFakeTerminalHost(16)
	host.SetSize(80, 8)
	runtime := newCopyModeRuntime(host, core, nil)

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send copy enter: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain latest: %v", err)
	}
	frame := lastFrame(t, host.Frames())
	var target render.HitRegion
	for _, region := range frame.HitRegions {
		if region.Kind == render.HitRegionHistoryRow && region.Row == 0 {
			target = region
			break
		}
	}
	if target.Kind == "" || target.Rect.W != 5 {
		t.Fatalf("expected text-only history row region with display width, got %#v", frame.HitRegions)
	}

	if err := host.SendInput(input.InputEvent{
		Kind:  input.EventKindMouse,
		Mouse: input.MouseLeft,
		Row:   target.Rect.Y + 1,
		Col:   target.Rect.X + 1 + 2,
	}); err != nil {
		t.Fatalf("send row click: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain row click: %v", err)
	}
	if got := runtime.State().CopyMode.Cursor; got != (state.CopyPosition{Row: 0, Col: 2}) {
		t.Fatalf("mouse selection should use display columns, got %#v", got)
	}
	if runtime.State().CopyMode.Mark == nil || *runtime.State().CopyMode.Mark != (state.CopyPosition{Row: 0, Col: 2}) {
		t.Fatalf("mouse selection should set mark at display column, got %#v", runtime.State().CopyMode.Mark)
	}
}

func TestSelectedTextSupportsReversedMultiRowSelection(t *testing.T) {
	history := historyStoreForCopySelection()
	copyMode := state.CopyModeStore{
		Active: true,
		Selection: &state.CopySelection{
			Anchor: state.CopyPosition{Row: 1, Col: 2},
			Focus:  state.CopyPosition{Row: 0, Col: 1},
		},
	}
	if got := SelectedText(history, copyMode); got != "lpha\nbe" {
		t.Fatalf("unexpected selected text %q", got)
	}
}

func TestSelectedTextUsesDisplayColumns(t *testing.T) {
	history := state.HistoryStore{
		Rows: []state.HistoryRow{{Text: "你好abc", LineID: 1}},
	}
	copyMode := state.CopyModeStore{
		Active: true,
		Selection: &state.CopySelection{
			Anchor: state.CopyPosition{Row: 0, Col: 2},
			Focus:  state.CopyPosition{Row: 0, Col: 5},
		},
	}
	if got := SelectedText(history, copyMode); got != "好a" {
		t.Fatalf("unexpected selected text %q", got)
	}
}

func TestCopyModeLineEndAndClampUseDisplayColumns(t *testing.T) {
	history := state.HistoryStore{
		Rows: []state.HistoryRow{{Text: "a好", LineID: 1}},
	}
	if got := copyModeLineEndPosition(history, 0); got != (state.CopyPosition{Row: 0, Col: 3}) {
		t.Fatalf("line end should use display width, got %#v", got)
	}
	copyMode := state.CopyModeStore{Cursor: state.CopyPosition{Row: 0, Col: 99}}
	copyMode = clampCopyCursor(copyMode, history)
	if copyMode.Cursor != (state.CopyPosition{Row: 0, Col: 3}) {
		t.Fatalf("clamp should use display width, got %#v", copyMode.Cursor)
	}
}

func TestCopyModeRejectsStaleHistoryResult(t *testing.T) {
	host := NewFakeTerminalHost(4)
	runtime := newCopyModeRuntime(host, &services.FakeCoreClient{}, nil)
	runtime.state.History = state.HistoryStore{
		Pending: &state.HistoryPendingRequest{ID: 2, Kind: state.HistoryRequestOlder, TerminalID: "term-1", Cols: 80, Token: "tok-1", Generation: 7},
	}

	if err := runtime.Post(CopyModeHistoryResultMsg{Result: services.HistoryResult{
		RequestID: 2,
		Window:    historyWindowForApp(state.HistoryWindowPrepend, "term-1", "stale", 80, 7, []state.HistoryRow{{Text: "old", LineID: 1}}),
	}}); err != nil {
		t.Fatalf("post stale result: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if runtime.State().History.Token != "" {
		t.Fatalf("stale response should not mutate history %#v", runtime.State().History)
	}
	if runtime.State().Session.LastError == "" {
		t.Fatalf("expected stale error in state %#v", runtime.State())
	}
}

func newCopyModeRuntime(host *FakeTerminalHost, core services.CoreClient, clipboard services.ClipboardService) *AppRuntime {
	return newCopyModeRuntimeWithRunner(host, core, clipboard, NewSyncEffectRunner())
}

func newCopyModeRuntimeWithRunner(host *FakeTerminalHost, core services.CoreClient, clipboard services.ClipboardService, runner EffectRunner) *AppRuntime {
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())
	if cols, rows, _ := host.Size(); cols <= 0 || rows <= 0 {
		host.SetSize(80, 24)
	}
	return NewAppRuntime(
		state.Root{
			Shell: state.DefaultShell(),
			Session: state.TerminalSessionStore{
				TerminalID: "term-1",
				Attached:   true,
				Cols:       80,
				Rows:       24,
			},
			Surface: state.TerminalSurfaceStore{
				TerminalID: "term-1",
				Cols:       80,
				Rows:       24,
				Lines:      []string{"live"},
			},
			TerminalViews: state.TerminalViewStore{}.BindPane(state.NewPaneTerminalView(
				state.DefaultPaneID, "term-1", 4, 78, 20, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true,
			)),
		},
		ComposeReducers(
			NewCopyModeReducer(CopyModeDeps{Core: core, Clipboard: clipboard, Rows: 20}),
			NewCopyModeResizeRebindReducer(CopyModeDeps{Core: core, Clipboard: clipboard, Rows: 20}),
		),
		func(root state.Root) render.Frame {
			return renderer.Render(builder.Build(root))
		},
		host,
		runner,
	)
}

type recordingEffectRunner struct {
	Effects []Effect
}

func (runner *recordingEffectRunner) Run(_ context.Context, effect Effect, _ func(Msg)) {
	runner.Effects = append(runner.Effects, effect)
}

func (runner *recordingEffectRunner) Cancel(CancelToken) {}

func historyWindowForApp(
	op state.HistoryWindowOp,
	terminalID string,
	token string,
	cols int,
	generation uint64,
	rows []state.HistoryRow,
) state.HistoryWindow {
	firstLine := uint64(0)
	lastLine := uint64(0)
	if len(rows) > 0 {
		firstLine = rows[0].LineID
		lastLine = rows[len(rows)-1].LineID
	}
	return state.HistoryWindow{
		TerminalID: terminalID,
		Token:      token,
		Op:         op,
		Cols:       cols,
		Rows:       rows,
		Lines:      []state.HistoryLineSpan{{LineID: firstLine, StartRow: 0, EndRow: len(rows) - 1}},
		Generation: generation,
		Boundary:   state.HistoryBoundary{FirstLineID: firstLine, LastLineID: lastLine},
	}
}

func historyStoreForCopySelection() state.HistoryStore {
	return state.HistoryStore{
		TerminalID: "term-1",
		Token:      "tok-1",
		Cols:       78,
		Rows: []state.HistoryRow{
			{Text: "alpha", LineID: 10},
			{Text: "beta", LineID: 11},
		},
	}
}

func historyRowTexts(rows []state.HistoryRow) []string {
	texts := make([]string, len(rows))
	for i, row := range rows {
		texts[i] = row.Text
	}
	return texts
}
