package app

import (
	"context"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestCopyHistoryOlderResultUsesIncrementalPatchWhenVisibleContentOnlyShifts(t *testing.T) {
	host := newCopyHistoryPerfIncrementalHost(16)
	host.SetSize(80, 24)
	root := copyHistoryPerfRoot(80, 24, 256)
	root.CopyMode.ViewportTop = 0
	root.CopyMode.Cursor = state.CopyPosition{}
	root.History.Pending = &state.HistoryPendingRequest{
		ID:                      1,
		Kind:                    state.HistoryRequestOlder,
		PaneID:                  root.History.PaneID,
		ViewID:                  root.History.ViewID,
		TerminalID:              root.History.TerminalID,
		Cols:                    root.History.Cols,
		Token:                   root.History.Token,
		Generation:              root.History.Generation,
		Cursor:                  root.History.Cursor,
		Boundary:                root.History.Boundary,
		ScrollDeltaAfterPrepend: 1,
	}
	runtime := NewAppRuntime(
		root,
		ComposeReducers(NewCopyModeReducer(CopyModeDeps{Core: &services.FakeCoreClient{}})),
		func(root state.Root) render.Frame {
			return render.NewRenderer(render.DefaultTheme()).RenderANSI(render.NewRenderVMBuilder().Build(root))
		},
		host,
		NewSyncEffectRunner(),
	)
	cache, ok := buildCopyHistoryPatchCache(root, render.DefaultTheme())
	if !ok {
		t.Fatal("expected copy history patch cache")
	}
	cache.Metadata = render.RenderMetadata{Width: 80, Height: 24}
	runtime.copyHistoryPatch = cache

	window := copyHistoryPerfOlderWindow(64, root.History)
	if err := runtime.Post(CopyModeHistoryResultMsg{Result: services.HistoryResult{RequestID: 1, Window: window}}); err != nil {
		t.Fatalf("post older result: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain older result: %v", err)
	}
	if host.sink.patchFrames != 1 {
		t.Fatalf("older result should use one incremental patch, frames=%d patches=%d", host.sink.frames, host.sink.patchFrames)
	}
	if copyHistoryPerfFrame.Patch == nil || !copyHistoryPerfFrame.Patch.Rewrite || len(copyHistoryPerfFrame.Patch.LinesANSI) != root.CopyMode.ViewRows {
		t.Fatalf("expected content-rect rewrite patch for pane history, got %#v", copyHistoryPerfFrame.Patch)
	}
	if copyHistoryPerfFrame.Patch.LineX != 1 || copyHistoryPerfFrame.Patch.LineWidth != root.History.Cols {
		t.Fatalf("rewrite patch must stay inside pane content rect, got %#v", copyHistoryPerfFrame.Patch)
	}
}

func TestCopyHistoryRewritePatchOffsetsANSIColumnAnchors(t *testing.T) {
	host := newCopyHistoryPerfIncrementalHost(16)
	host.SetSize(80, 24)
	root := copyHistoryPerfRoot(80, 24, 256)
	root.CopyMode.ViewportTop = 120
	root.CopyMode.Cursor = state.CopyPosition{Row: 120}
	root.History.Rows[119] = state.HistoryRow{
		Text:   "099975",
		LineID: 119,
		Cells: []state.HistoryCell{
			{Text: "0", Width: 1},
			{Text: "99975", Width: 5},
		},
	}
	root.History.Rows[120] = state.HistoryRow{Text: "current", LineID: 120}
	runtime := NewAppRuntime(
		root,
		copyHistoryPerfLineScrollReducer(),
		func(root state.Root) render.Frame {
			return render.NewRenderer(render.DefaultTheme()).RenderANSI(render.NewRenderVMBuilder().Build(root))
		},
		host,
		NewSyncEffectRunner(),
	)
	cache, ok := buildCopyHistoryPatchCache(root, render.DefaultTheme())
	if !ok {
		t.Fatal("expected copy history patch cache")
	}
	cache.Metadata = render.RenderMetadata{Width: 80, Height: 24}
	runtime.copyHistoryPatch = cache

	root.CopyMode = root.CopyMode.ScrollCursor(-1, len(root.History.Rows))
	runtime.state = root.Advance()
	if !runtime.tryRenderCopyHistoryPatch() {
		t.Fatal("expected rewrite patch")
	}
	patch := copyHistoryPerfFrame.Patch
	if patch == nil || !patch.Rewrite || len(patch.LinesANSI) == 0 {
		t.Fatalf("expected rewrite patch lines, got %#v", patch)
	}
	got := patch.LinesANSI[0]
	if !strings.Contains(got, "0\x1b[3G99975") || strings.Contains(got, "0\x1b[2G99975") {
		t.Fatalf("rewrite patch must offset content-local ANSI anchors, got %q", got)
	}
}

func TestCopyHistoryRewritePatchPreservesStyledBlankCells(t *testing.T) {
	host := newCopyHistoryPerfIncrementalHost(16)
	host.SetSize(80, 24)
	root := copyHistoryPerfRoot(80, 24, 256)
	root.CopyMode.ViewportTop = 120
	root.CopyMode.Cursor = state.CopyPosition{Row: 120}
	root.History.Rows[119] = state.HistoryRow{
		Text:   "BG    ",
		LineID: 119,
		Cells: []state.HistoryCell{
			{Text: "BG    ", Width: 6, Style: state.HistoryCellStyle{BG: "ansi:4"}},
		},
	}
	root.History.Rows[120] = state.HistoryRow{Text: "current", LineID: 120}
	runtime := NewAppRuntime(
		root,
		copyHistoryPerfLineScrollReducer(),
		func(root state.Root) render.Frame {
			return render.NewRenderer(render.DefaultTheme()).RenderANSI(render.NewRenderVMBuilder().Build(root))
		},
		host,
		NewSyncEffectRunner(),
	)
	cache, ok := buildCopyHistoryPatchCache(root, render.DefaultTheme())
	if !ok {
		t.Fatal("expected copy history patch cache")
	}
	cache.Metadata = render.RenderMetadata{Width: 80, Height: 24}
	runtime.copyHistoryPatch = cache

	root.CopyMode = root.CopyMode.ScrollCursor(-1, len(root.History.Rows))
	runtime.state = root.Advance()
	if !runtime.tryRenderCopyHistoryPatch() {
		t.Fatal("expected rewrite patch")
	}
	patch := copyHistoryPerfFrame.Patch
	if patch == nil || !patch.Rewrite || len(patch.LinesANSI) == 0 {
		t.Fatalf("expected rewrite patch lines, got %#v", patch)
	}
	got := patch.LinesANSI[0]
	if !strings.Contains(got, "\x1b[44mBG    ") {
		t.Fatalf("rewrite patch should preserve background over styled blanks, got %q", got)
	}
}

func TestCopyHistoryCursorOnlyMoveUsesCursorPatch(t *testing.T) {
	host := newCopyHistoryPerfIncrementalHost(16)
	host.SetSize(80, 24)
	root := copyHistoryPerfRoot(80, 24, 256)
	root.CopyMode.ViewportTop = 120
	root.CopyMode.Cursor = state.CopyPosition{Row: 121, Col: 2}
	runtime := NewAppRuntime(
		root,
		copyHistoryPerfLineScrollReducer(),
		func(root state.Root) render.Frame {
			return render.NewRenderer(render.DefaultTheme()).RenderANSI(render.NewRenderVMBuilder().Build(root))
		},
		host,
		NewSyncEffectRunner(),
	)
	cache, ok := buildCopyHistoryPatchCache(root, render.DefaultTheme())
	if !ok {
		t.Fatal("expected copy history patch cache")
	}
	cache.Metadata = render.RenderMetadata{Width: 80, Height: 24}
	runtime.copyHistoryPatch = cache

	root.CopyMode = root.CopyMode.ScrollCursor(-1, len(root.History.Rows))
	runtime.state = root.Advance()
	if !runtime.tryRenderCopyHistoryPatch() {
		t.Fatal("expected cursor-only patch")
	}
	patch := copyHistoryPerfFrame.Patch
	if patch == nil || !patch.CursorOnly || patch.Rewrite || patch.Dir != 0 || patch.LineANSI != "" || len(patch.LinesANSI) != 0 {
		t.Fatalf("cursor-only movement should not rewrite history content, got %#v", patch)
	}
	if !copyHistoryPerfFrame.Cursor.Visible || copyHistoryPerfFrame.CursorRect.W != 1 || copyHistoryPerfFrame.CursorRect.H != 1 {
		t.Fatalf("cursor-only patch should carry visible cursor metadata, frame=%#v", copyHistoryPerfFrame)
	}
}
