package app

import (
	"context"
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
