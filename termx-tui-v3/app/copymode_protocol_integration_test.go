package app

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-proto/wire"
	"github.com/lozzow/termx/termx-shared/transport/memory"
	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestCopyModeUsesProtocolHistoryWindowClient(t *testing.T) {
	clientTransport, serverTransport := memory.NewPair()
	errCh := make(chan error, 1)
	go func() {
		defer func() { _ = serverTransport.Close() }()
		errCh <- runCopyModeHistoryProtocolServer(serverTransport)
	}()
	client := protocol.NewClient(clientTransport)
	defer func() { _ = client.Close() }()
	if err := client.Hello(context.Background(), protocol.Hello{Version: wire.Version, Client: "tui-v3-copy"}); err != nil {
		t.Fatalf("hello: %v", err)
	}

	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())
	runtime := NewAppRuntime(
		state.Root{
			Shell:         state.DefaultShell(),
			Session:       state.TerminalSessionStore{TerminalID: "term-1", Attached: true, Cols: 80, Rows: 24},
			Surface:       state.TerminalSurfaceStore{TerminalID: "term-1", Cols: 80, Rows: 24, Lines: []string{"live"}},
			TerminalViews: state.TerminalViewStore{}.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 4, 78, 20, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true)),
		},
		ComposeReducers(
			NewCopyModeReducer(CopyModeDeps{Core: services.ProtocolCoreClientAdapter{Client: client}, Rows: 20}),
			NewCopyModeResizeRebindReducer(CopyModeDeps{Core: services.ProtocolCoreClientAdapter{Client: client}, Rows: 20}),
		),
		func(root state.Root) render.Frame {
			return renderer.Render(builder.Build(root))
		},
		host,
		NewSyncEffectRunner(),
	)

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send latest page up: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain latest: %v", err)
	}
	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send older page up: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain older: %v", err)
	}
	if got := historyRowTexts(runtime.State().History.Rows); len(got) != 2 || got[0] != "old" || got[1] != "new" {
		t.Fatalf("unexpected protocol history rows %v", got)
	}
	_ = clientTransport.Close()
	select {
	case err := <-errCh:
		if err != nil && err != io.EOF {
			t.Fatalf("server returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop")
	}
}

func TestCopyModeProtocolCodexLatestDoesNotUseLiveRevisionBoundary(t *testing.T) {
	clientTransport, serverTransport := memory.NewPair()
	errCh := make(chan error, 1)
	go func() {
		defer func() { _ = serverTransport.Close() }()
		errCh <- runCopyModeCodexLatestProtocolServer(serverTransport)
	}()
	client := protocol.NewClient(clientTransport)
	defer func() { _ = client.Close() }()
	if err := client.Hello(context.Background(), protocol.Hello{Version: wire.Version, Client: "tui-v3-copy-codex"}); err != nil {
		t.Fatalf("hello: %v", err)
	}

	host := NewFakeTerminalHost(16)
	host.SetSize(80, 24)
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())
	runtime := NewAppRuntime(
		state.Root{
			Shell:   state.DefaultShell(),
			Session: state.TerminalSessionStore{TerminalID: "term-1", Attached: true, Cols: 80, Rows: 24},
			Surface: state.TerminalSurfaceStore{
				TerminalID: "term-1",
				Revision:   1,
				Cols:       80,
				Rows:       24,
				Lines:      []string{"lozzow@RedmiBook: ~/Documents/workdir/termx", "codex --yolo", "OpenAI Codex"},
				State:      state.TerminalLiveAttached,
			},
			TerminalViews: state.TerminalViewStore{}.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 4, 78, 20, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true)),
		},
		ComposeReducers(
			NewCopyModeReducer(CopyModeDeps{Core: services.ProtocolCoreClientAdapter{Client: client}, Rows: 20}),
			NewCopyModeResizeRebindReducer(CopyModeDeps{Core: services.ProtocolCoreClientAdapter{Client: client}, Rows: 20}),
		),
		func(root state.Root) render.Frame {
			return renderer.Render(builder.Build(root))
		},
		host,
		NewSyncEffectRunner(),
	)

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send latest page up: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain latest: %v", err)
	}
	got := historyRowTexts(runtime.State().History.Rows)
	if len(got) < 25 || got[0] != "lozzow@RedmiBook: ~/Documents/workdir/termx" || got[1] != "codex --yolo" || got[4] != "Update available! 0.141.0 -> 0.142.0" {
		t.Fatalf("expected Codex update card at live-tail history boundary, got %v", got)
	}
	if runtime.State().CopyMode.Cursor.Row != len(got)-1 || runtime.State().CopyMode.ViewportTop != 5 {
		t.Fatalf("copy latest should start at Codex newest tail, got copy=%#v rows=%v", runtime.State().CopyMode, got)
	}

	_ = clientTransport.Close()
	select {
	case err := <-errCh:
		if err != nil && err != io.EOF {
			t.Fatalf("server returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop")
	}
}

func TestCopyModeProtocolHistorySearchMatchesAcrossReflowRows(t *testing.T) {
	clientTransport, serverTransport := memory.NewPair()
	errCh := make(chan error, 1)
	go func() {
		defer func() { _ = serverTransport.Close() }()
		errCh <- runCopyModeCrossRowSearchProtocolServer(serverTransport)
	}()
	client := protocol.NewClient(clientTransport)
	defer func() { _ = client.Close() }()
	if err := client.Hello(context.Background(), protocol.Hello{Version: wire.Version, Client: "tui-v3-copy-cross-row"}); err != nil {
		t.Fatalf("hello: %v", err)
	}

	host := NewFakeTerminalHost(16)
	host.SetSize(10, 12)
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())
	runtime := NewAppRuntime(
		state.Root{
			Shell:         state.DefaultShell(),
			Session:       state.TerminalSessionStore{TerminalID: "term-1", Attached: true, Cols: 10, Rows: 12},
			Surface:       state.TerminalSurfaceStore{TerminalID: "term-1", Cols: 10, Rows: 12, Lines: []string{"live"}},
			TerminalViews: state.TerminalViewStore{}.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 4, 8, 8, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true)),
		},
		ComposeReducers(
			NewCopyModeReducer(CopyModeDeps{Core: services.ProtocolCoreClientAdapter{Client: client}, Rows: 20}),
			NewCopyModeResizeRebindReducer(CopyModeDeps{Core: services.ProtocolCoreClientAdapter{Client: client}, Rows: 20}),
		),
		func(root state.Root) render.Frame {
			return renderer.Render(builder.Build(root))
		},
		host,
		NewSyncEffectRunner(),
	)

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send latest page up: %v", err)
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
	if runtime.State().CopyMode.Query != "beta" || len(runtime.State().CopyMode.Matches) != 1 {
		t.Fatalf("expected one protocol cross-row match, got %#v", runtime.State().CopyMode)
	}
	if runtime.State().CopyMode.Cursor != (state.CopyPosition{Row: 0, Col: 5}) {
		t.Fatalf("expected cursor on protocol cross-row match start, got %#v", runtime.State().CopyMode.Cursor)
	}

	_ = clientTransport.Close()
	select {
	case err := <-errCh:
		if err != nil && err != io.EOF {
			t.Fatalf("server returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop")
	}
}

func TestCopyModeProtocolHistoryReflowsWrappedLogicalLineLocally(t *testing.T) {
	clientTransport, serverTransport := memory.NewPair()
	errCh := make(chan error, 1)
	go func() {
		defer func() { _ = serverTransport.Close() }()
		errCh <- runCopyModeWrappedLogicalLineProtocolServer(serverTransport)
	}()
	client := protocol.NewClient(clientTransport)
	defer func() { _ = client.Close() }()
	if err := client.Hello(context.Background(), protocol.Hello{Version: wire.Version, Client: "tui-v3-copy-wrap"}); err != nil {
		t.Fatalf("hello: %v", err)
	}

	host := NewFakeTerminalHost(16)
	host.SetSize(10, 12)
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())
	runtime := NewAppRuntime(
		state.Root{
			Shell:         state.DefaultShell(),
			Session:       state.TerminalSessionStore{TerminalID: "term-1", Attached: true, Cols: 10, Rows: 12},
			Surface:       state.TerminalSurfaceStore{TerminalID: "term-1", Cols: 10, Rows: 12, Lines: []string{"live"}},
			TerminalViews: state.TerminalViewStore{}.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 4, 8, 8, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true)),
		},
		ComposeReducers(
			NewCopyModeReducer(CopyModeDeps{Core: services.ProtocolCoreClientAdapter{Client: client}, Rows: 20}),
			NewCopyModeResizeRebindReducer(CopyModeDeps{Core: services.ProtocolCoreClientAdapter{Client: client}, Rows: 20}),
		),
		func(root state.Root) render.Frame {
			return renderer.Render(builder.Build(root))
		},
		host,
		NewSyncEffectRunner(),
	)

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send latest page up: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain latest: %v", err)
	}

	if len(runtime.State().History.SourceLines) != 1 || runtime.State().History.SourceLines[0].Text != "abcdef" {
		t.Fatalf("expected one merged frozen source line, got %#v", runtime.State().History.SourceLines)
	}
	if got := historyRowTexts(runtime.State().History.Rows); len(got) != 1 || got[0] != "abcdef" {
		t.Fatalf("expected local reflow to widen wrapped logical line into one row, got %v", got)
	}

	_ = clientTransport.Close()
	select {
	case err := <-errCh:
		if err != nil && err != io.EOF {
			t.Fatalf("server returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop")
	}
}

func TestCopyModeProtocolHistoryReflowsWideStyledCellLocally(t *testing.T) {
	clientTransport, serverTransport := memory.NewPair()
	errCh := make(chan error, 1)
	go func() {
		defer func() { _ = serverTransport.Close() }()
		errCh <- runCopyModeWideStyledLogicalLineProtocolServer(serverTransport)
	}()
	client := protocol.NewClient(clientTransport)
	defer func() { _ = client.Close() }()
	if err := client.Hello(context.Background(), protocol.Hello{Version: wire.Version, Client: "tui-v3-copy-wide-cell"}); err != nil {
		t.Fatalf("hello: %v", err)
	}

	host := NewFakeTerminalHost(16)
	host.SetSize(7, 12)
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())
	runtime := NewAppRuntime(
		state.Root{
			Shell:         state.DefaultShell(),
			Session:       state.TerminalSessionStore{TerminalID: "term-1", Attached: true, Cols: 7, Rows: 12},
			Surface:       state.TerminalSurfaceStore{TerminalID: "term-1", Cols: 7, Rows: 12, Lines: []string{"live"}},
			TerminalViews: state.TerminalViewStore{}.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 4, 5, 8, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true)),
		},
		ComposeReducers(
			NewCopyModeReducer(CopyModeDeps{Core: services.ProtocolCoreClientAdapter{Client: client}, Rows: 20}),
			NewCopyModeResizeRebindReducer(CopyModeDeps{Core: services.ProtocolCoreClientAdapter{Client: client}, Rows: 20}),
		),
		func(root state.Root) render.Frame {
			return renderer.Render(builder.Build(root))
		},
		host,
		NewSyncEffectRunner(),
	)

	if err := host.SendInput(input.InputEvent{Kind: input.EventKindKey, Key: input.KeyPageUp}); err != nil {
		t.Fatalf("send latest page up: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain latest: %v", err)
	}

	if len(runtime.State().History.SourceLines) != 1 || runtime.State().History.SourceLines[0].Text != "abcdef" {
		t.Fatalf("expected one frozen source line for wide styled cell, got %#v", runtime.State().History.SourceLines)
	}
	if got := historyRowTexts(runtime.State().History.Rows); len(got) != 2 || got[0] != "abcde" || got[1] != "f" {
		t.Fatalf("expected local reflow to split wide styled cell by display cols, got %v", got)
	}

	_ = clientTransport.Close()
	select {
	case err := <-errCh:
		if err != nil && err != io.EOF {
			t.Fatalf("server returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop")
	}
}

func runCopyModeHistoryProtocolServer(tr *memory.Transport) error {
	if err := expectCopyModeProtocolHello(tr); err != nil {
		return err
	}
	if err := sendCopyModeProtocolHello(tr); err != nil {
		return err
	}
	if err := serveCopyModeCopyEntryProjection(tr, "term-1", 78, 120); err != nil {
		return err
	}
	req, err := expectCopyModeProtocolRequest(tr, "history.window")
	if err != nil {
		return err
	}
	params, err := copyModeProtocolRequestParams[protocol.HistoryWindowParams](req)
	if err != nil {
		return err
	}
	if params.TerminalID != "term-1" || params.Token != "" || params.Cols != 78 || params.Limit != 120 {
		return fmt.Errorf("unexpected latest params %#v", params)
	}
	if err := sendCopyModeHistoryWindow(tr, req, protocol.HistoryWindow{
		TerminalID:   "term-1",
		Token:        "tok-1",
		Op:           protocol.HistoryWindowReplace,
		Size:         protocol.Size{Cols: 78, Rows: 20},
		Rows:         []protocol.CompactRow{protocol.CompactRowFromCells([]protocol.Cell{{Content: "new"}})},
		Lines:        []protocol.HistoryLineSpan{{LogicalLineID: 20, StartRow: 0, EndRow: 0}},
		RowLineIDs:   []uint64{20},
		RowInLine:    []int{0},
		CursorValid:  true,
		CursorLineID: 20,
		Generation:   7,
		FirstLineID:  20,
		LastLineID:   20,
	}); err != nil {
		return err
	}
	req, err = expectCopyModeProtocolRequest(tr, "history.window")
	if err != nil {
		return err
	}
	params, err = copyModeProtocolRequestParams[protocol.HistoryWindowParams](req)
	if err != nil {
		return err
	}
	if params.Token != "tok-1" || params.Generation != 7 || !params.CursorValid || params.BeforeLineID != 20 || params.BoundaryLastLineID != 20 {
		return fmt.Errorf("unexpected older params %#v", params)
	}
	return sendCopyModeHistoryWindow(tr, req, protocol.HistoryWindow{
		TerminalID:   "term-1",
		Token:        "tok-1",
		Op:           protocol.HistoryWindowPrepend,
		Size:         protocol.Size{Cols: 78, Rows: 20},
		Rows:         []protocol.CompactRow{protocol.CompactRowFromCells([]protocol.Cell{{Content: "old"}})},
		Lines:        []protocol.HistoryLineSpan{{LogicalLineID: 10, StartRow: 0, EndRow: 0}},
		RowLineIDs:   []uint64{10},
		RowInLine:    []int{0},
		CursorValid:  true,
		CursorLineID: 10,
		Generation:   7,
		FirstLineID:  10,
		LastLineID:   20,
	})
}

func runCopyModeCodexLatestProtocolServer(tr *memory.Transport) error {
	if err := expectCopyModeProtocolHello(tr); err != nil {
		return err
	}
	if err := sendCopyModeProtocolHello(tr); err != nil {
		return err
	}
	if err := serveCopyModeCopyEntryProjection(tr, "term-1", 78, 120); err != nil {
		return err
	}
	req, err := expectCopyModeProtocolRequest(tr, "history.window")
	if err != nil {
		return err
	}
	params, err := copyModeProtocolRequestParams[protocol.HistoryWindowParams](req)
	if err != nil {
		return err
	}
	if params.TerminalID != "term-1" || params.Token != "" || params.Cols != 78 || params.Limit != 120 || params.Generation != 0 {
		return fmt.Errorf("unexpected Codex latest params %#v", params)
	}
	rows := []protocol.CompactRow{
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "lozzow@RedmiBook: ~/Documents/workdir/termx"}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "codex --yolo"}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "lozzow@RedmiBook: ~/Documents/workdir/termx"}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "codex --yolo"}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "Update available! 0.141.0 -> 0.142.0"}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "Run brew upgrade --cask codex to update."}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "See full release notes:"}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "https://github.com/openai/codex/releases/latest"}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: ""}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "OpenAI Codex"}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: ""}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "model: gpt-5.5 xhigh"}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "directory: ~/Documents/workdir/termx"}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "permissions: YOLO mode"}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: ""}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "Tip: Use /compact when the conversation gets long."}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: ""}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "> Improve documentation in @filename"}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "gpt-5.5 xhigh . ~/Documents/workdir/termx"}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: ""}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "status: running"}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "tokens: 123"}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "owner: default"}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "footer: ready"}}),
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "input cursor"}}),
	}
	latchedLines := make([]protocol.HistoryLineSpan, 0, len(rows))
	rowLineIDs := make([]uint64, 0, len(rows))
	rowInLine := make([]int, 0, len(rows))
	rowOwnership := make([]string, 0, len(rows))
	for index := range rows {
		lineID := uint64(10 + index)
		latchedLines = append(latchedLines, protocol.HistoryLineSpan{LogicalLineID: lineID, StartRow: index, EndRow: index})
		rowLineIDs = append(rowLineIDs, lineID)
		rowInLine = append(rowInLine, 0)
		ownership := protocol.RowOwnershipPersisted
		if index >= 4 {
			ownership = protocol.RowOwnershipLiveTailLive
		}
		rowOwnership = append(rowOwnership, ownership)
	}
	return sendCopyModeHistoryWindow(tr, req, protocol.HistoryWindow{
		TerminalID: "term-1",
		Token:      "tok-codex",
		Op:         protocol.HistoryWindowReplace,
		Size:       protocol.Size{Cols: 78, Rows: 20},
		Rows:       rows,
		Lines:      latchedLines,
		RowLineIDs: rowLineIDs,
		RowInLine:  rowInLine,
		// 中文说明：Codex 全屏当前帧从 update card 开始，TUI 必须用这个 ownership
		// 锚定 copy/history 入口，不能落到 current frame 尾部。
		RowOwnership: rowOwnership,
		CursorValid:  true,
		CursorLineID: 14,
		Generation:   7,
		FirstLineID:  10,
		LastLineID:   34,
		LoadedLines:  len(rows),
		LogicalTotal: 2,
	})
}

func runCopyModeWideStyledLogicalLineProtocolServer(tr *memory.Transport) error {
	if err := expectCopyModeProtocolHello(tr); err != nil {
		return err
	}
	if err := sendCopyModeProtocolHello(tr); err != nil {
		return err
	}
	if err := serveCopyModeCopyEntryProjection(tr, "term-1", 5, 64); err != nil {
		return err
	}
	req, err := expectCopyModeProtocolRequest(tr, "history.window")
	if err != nil {
		return err
	}
	params, err := copyModeProtocolRequestParams[protocol.HistoryWindowParams](req)
	if err != nil {
		return err
	}
	if params.TerminalID != "term-1" || params.Token != "" || params.Cols != 5 || params.Limit != 64 {
		return fmt.Errorf("unexpected latest params %#v", params)
	}
	if err := sendCopyModeHistoryWindow(tr, req, protocol.HistoryWindow{
		TerminalID: "term-1",
		Token:      "tok-wide",
		Op:         protocol.HistoryWindowReplace,
		Size:       protocol.Size{Cols: 3, Rows: 20},
		Rows: []protocol.CompactRow{
			protocol.CompactRowFromCells([]protocol.Cell{{
				Content: "abcdef",
				Width:   6,
				Style: protocol.CellStyle{
					FG:        "ansi:4",
					Underline: true,
				},
				LinkURL: "file://wide.txt",
			}}),
		},
		Lines:        []protocol.HistoryLineSpan{{LogicalLineID: 10, StartRow: 0, EndRow: 0}},
		RowLineIDs:   []uint64{10},
		RowInLine:    []int{0},
		Generation:   7,
		FirstLineID:  10,
		LastLineID:   10,
		LoadedLines:  1,
		LogicalTotal: 1,
	}); err != nil {
		return err
	}
	return nil
}

func runCopyModeWrappedLogicalLineProtocolServer(tr *memory.Transport) error {
	if err := expectCopyModeProtocolHello(tr); err != nil {
		return err
	}
	if err := sendCopyModeProtocolHello(tr); err != nil {
		return err
	}
	if err := serveCopyModeCopyEntryProjection(tr, "term-1", 8, 64); err != nil {
		return err
	}
	req, err := expectCopyModeProtocolRequest(tr, "history.window")
	if err != nil {
		return err
	}
	params, err := copyModeProtocolRequestParams[protocol.HistoryWindowParams](req)
	if err != nil {
		return err
	}
	if params.TerminalID != "term-1" || params.Token != "" || params.Cols != 8 || params.Limit != 64 {
		return fmt.Errorf("unexpected latest params %#v", params)
	}
	return sendCopyModeHistoryWindow(tr, req, protocol.HistoryWindow{
		TerminalID: "term-1",
		Token:      "tok-1",
		Op:         protocol.HistoryWindowReplace,
		Size:       protocol.Size{Cols: 3, Rows: 8},
		Rows: []protocol.CompactRow{
			protocol.CompactRowFromCells([]protocol.Cell{{Content: "abc", Width: 3}}),
			protocol.CompactRowFromCells([]protocol.Cell{{Content: "def", Width: 3}}),
		},
		Lines:       []protocol.HistoryLineSpan{{LogicalLineID: 10, StartRow: 0, EndRow: 1}},
		RowLineIDs:  []uint64{10, 10},
		RowInLine:   []int{0, 1},
		Generation:  7,
		FirstLineID: 10,
		LastLineID:  10,
	})
}

func runCopyModeCrossRowSearchProtocolServer(tr *memory.Transport) error {
	if err := expectCopyModeProtocolHello(tr); err != nil {
		return err
	}
	if err := sendCopyModeProtocolHello(tr); err != nil {
		return err
	}
	if err := serveCopyModeCopyEntryProjection(tr, "term-1", 8, 64); err != nil {
		return err
	}
	req, err := expectCopyModeProtocolRequest(tr, "history.window")
	if err != nil {
		return err
	}
	params, err := copyModeProtocolRequestParams[protocol.HistoryWindowParams](req)
	if err != nil {
		return err
	}
	if params.TerminalID != "term-1" || params.Token != "" || params.Cols != 8 || params.Limit != 64 {
		return fmt.Errorf("unexpected latest params %#v", params)
	}
	return sendCopyModeHistoryWindow(tr, req, protocol.HistoryWindow{
		TerminalID: "term-1",
		Token:      "tok-1",
		Op:         protocol.HistoryWindowReplace,
		Size:       protocol.Size{Cols: 8, Rows: 8},
		Rows: []protocol.CompactRow{
			protocol.CompactRowFromCells([]protocol.Cell{{Content: "alphabe"}}),
			protocol.CompactRowFromCells([]protocol.Cell{{Content: "tagamma"}}),
		},
		Lines:       []protocol.HistoryLineSpan{{LogicalLineID: 10, StartRow: 0, EndRow: 1}},
		RowLineIDs:  []uint64{10, 10},
		RowInLine:   []int{0, 1},
		Generation:  7,
		FirstLineID: 10,
		LastLineID:  10,
	})
}

func sendCopyModeHistoryWindow(tr *memory.Transport, req protocol.Request, window protocol.HistoryWindow) error {
	payload, err := protocol.EncodeHistoryWindowPayload(&window)
	if err != nil {
		return err
	}
	binaryPayload, err := protocol.EncodeBinaryResponsePayload(req.ID, payload)
	if err != nil {
		return err
	}
	return sendCopyModeProtocolFrame(tr, 0, wire.TypeResponseBinary, binaryPayload)
}

func serveCopyModeCopyEntryProjection(tr *memory.Transport, terminalID string, cols int, limit int) error {
	req, err := expectCopyModeProtocolRequest(tr, "history.copy_entry")
	if err != nil {
		return err
	}
	params, err := copyModeProtocolRequestParams[protocol.CopyEntryProjectionParams](req)
	if err != nil {
		return err
	}
	if params.TerminalID != terminalID || params.Cols != cols || params.Limit != limit {
		return fmt.Errorf("unexpected copy-entry params %#v", params)
	}
	projection := protocol.CopyEntryProjection{
		TerminalID: terminalID,
		NativeCols: cols,
		Window: protocol.HistoryWindow{
			TerminalID: terminalID,
			Op:         protocol.HistoryWindowReplace,
			Size:       protocol.Size{Cols: uint16(cols), Rows: 20},
		},
		Capabilities: protocol.CopyEntryCapabilityBits{Selectable: true},
	}
	payload, err := protocol.EncodeCopyEntryProjectionPayload(&projection)
	if err != nil {
		return err
	}
	binaryPayload, err := protocol.EncodeBinaryResponsePayload(req.ID, payload)
	if err != nil {
		return err
	}
	return sendCopyModeProtocolFrame(tr, 0, wire.TypeResponseBinary, binaryPayload)
}

func expectCopyModeProtocolHello(tr *memory.Transport) error {
	channel, typ, payload, err := recvCopyModeProtocolFrame(tr)
	if err != nil {
		return err
	}
	if channel != 0 || typ != wire.TypeHello {
		return fmt.Errorf("unexpected hello frame channel=%d type=%d", channel, typ)
	}
	_, err = protocol.DecodeHelloPayload(payload)
	return err
}

func sendCopyModeProtocolHello(tr *memory.Transport) error {
	payload, err := protocol.EncodeHelloPayload(protocol.Hello{Version: wire.Version, Server: "fake"})
	if err != nil {
		return err
	}
	return sendCopyModeProtocolFrame(tr, 0, wire.TypeHello, payload)
}

func expectCopyModeProtocolRequest(tr *memory.Transport, method string) (protocol.Request, error) {
	channel, typ, payload, err := recvCopyModeProtocolFrame(tr)
	if err != nil {
		return protocol.Request{}, err
	}
	if channel != 0 || typ != wire.TypeRequest {
		return protocol.Request{}, fmt.Errorf("unexpected request frame channel=%d type=%d", channel, typ)
	}
	req, err := protocol.DecodeRequestPayload(payload)
	if err != nil {
		return protocol.Request{}, err
	}
	if req.Method != method {
		return protocol.Request{}, fmt.Errorf("unexpected method %s", req.Method)
	}
	return req, nil
}

func copyModeProtocolRequestParams[T any](req protocol.Request) (T, error) {
	var zero T
	decoded, err := protocol.DecodeMethodParams(req.Method, req.Params)
	if err != nil {
		return zero, err
	}
	params, ok := decoded.(T)
	if !ok {
		return zero, fmt.Errorf("unexpected params type %T", decoded)
	}
	return params, nil
}

func sendCopyModeProtocolFrame(tr *memory.Transport, channel uint16, typ uint8, payload []byte) error {
	frame, err := wire.EncodeFrame(channel, typ, payload)
	if err != nil {
		return err
	}
	return tr.Send(frame)
}

func recvCopyModeProtocolFrame(tr *memory.Transport) (uint16, uint8, []byte, error) {
	frame, err := tr.Recv()
	if err != nil {
		return 0, 0, nil, err
	}
	return wire.DecodeFrame(frame)
}
