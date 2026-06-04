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
	if runtime.State().CopyMode.ViewportTop != 1 {
		t.Fatalf("expected viewport shifted by older prepend, got %#v", runtime.State().CopyMode)
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

func TestCopyModePaneSizeCommandRebindsLatestAtContentCols(t *testing.T) {
	core := &services.FakeCoreClient{
		LatestResponses: []services.HistoryResult{
			{Window: historyWindowForApp(state.HistoryWindowReplace, "term-1", "tok-1", 39, 7, []state.HistoryRow{{Text: "old-window", LineID: 20}})},
			{Window: historyWindowForApp(state.HistoryWindowReplace, "term-1", "tok-2", 23, 8, []state.HistoryRow{{Text: "sized-window", LineID: 30}})},
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

	if len(core.LatestRequests) != 2 || core.LatestRequests[1].Cols != 23 {
		t.Fatalf("pane size command must rebind copy mode at active content cols, got %#v", core.LatestRequests)
	}
	if runtime.State().CopyMode.BoundToken != "tok-2" || runtime.State().CopyMode.BoundCols != 23 {
		t.Fatalf("expected copy mode rebound to sized window, got %#v", runtime.State().CopyMode)
	}
	if runtime.State().History.Token != "tok-2" || runtime.State().History.Cols != 23 || len(runtime.State().History.Rows) != 1 || runtime.State().History.Rows[0].Text != "sized-window" {
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
	runtime.state.CopyMode = state.CopyModeStore{Active: true, TerminalID: "term-1", BoundToken: "tok-1", BoundCols: 78}
	runtime.state.History = state.HistoryStore{
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
	if frameContains(last, "old-window") || frameContains(last, "stale") {
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
	if !frameContains(last, "selection yanked") {
		t.Fatalf("copy feedback toast should be visible, got %#v", last.Lines)
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

func TestSelectedTextUsesRuneColumns(t *testing.T) {
	history := state.HistoryStore{
		Rows: []state.HistoryRow{{Text: "你好abc", LineID: 1}},
	}
	copyMode := state.CopyModeStore{
		Active: true,
		Selection: &state.CopySelection{
			Anchor: state.CopyPosition{Row: 0, Col: 1},
			Focus:  state.CopyPosition{Row: 0, Col: 4},
		},
	}
	if got := SelectedText(history, copyMode); got != "好ab" {
		t.Fatalf("unexpected selected text %q", got)
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
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())
	if cols, rows, _ := host.Size(); cols <= 0 || rows <= 0 {
		host.SetSize(80, 24)
	}
	return NewAppRuntime(
		state.Root{
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
		},
		ComposeReducers(
			NewCopyModeReducer(CopyModeDeps{Core: core, Clipboard: clipboard, Rows: 20}),
			NewCopyModeResizeRebindReducer(CopyModeDeps{Core: core, Clipboard: clipboard, Rows: 20}),
		),
		func(root state.Root) render.Frame {
			return renderer.Render(builder.Build(root))
		},
		host,
		NewSyncEffectRunner(),
	)
}

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
