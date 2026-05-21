package runtime

import (
	"testing"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/tuiv2/bridge"
)

func TestTransactionRestorePreservesViewportAndHistoryLoadState(t *testing.T) {
	rt := New(nil)
	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.Snapshot = &bridge.SnapshotRef{
		TerminalID:           "term-1",
		Size:                 protocol.Size{Cols: 80, Rows: 24},
		Scrollback:           protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromString("seed")}),
		ScrollbackHasMore:    true,
		ScrollbackLoadedRows: 37,
		Screen:               protocol.ScreenData{Cells: [][]protocol.Cell{protocolRowFromString("live")}},
		Cursor:               protocol.CursorState{Visible: true},
		Modes:                protocol.TerminalModes{AutoWrap: true},
	}
	terminal.ScrollbackLoadedLimit = 37
	terminal.ScrollbackLoadingLimit = 64
	terminal.ScrollbackExhausted = false

	binding := rt.BindPane("pane-1")
	binding.Viewport.Offset = 9
	binding.ContentOffset = PaneContentOffsetState{X: 4, Y: -3}

	live := rt.TerminalLiveStateSnapshot("term-1")
	bindingSnapshot := ClonePaneBinding(binding)

	terminal.ScrollbackLoadedLimit = 0
	terminal.ScrollbackLoadingLimit = 0
	terminal.ScrollbackExhausted = true
	_ = rt.SetPaneViewportOffset("pane-1", 0)
	_ = rt.SetPaneContentOffset("pane-1", 0, 0)

	rt.RestoreTerminalLiveState("term-1", live)
	rt.RestorePaneBinding("pane-1", bindingSnapshot)

	if got := terminal.ScrollbackLoadedLimit; got != 37 {
		t.Fatalf("expected restore to preserve loaded rows limit 37, got %d", got)
	}
	if got := terminal.ScrollbackLoadingLimit; got != 64 {
		t.Fatalf("expected restore to preserve in-flight loading limit 64, got %d", got)
	}
	if terminal.ScrollbackExhausted {
		t.Fatal("expected restore to preserve non-exhausted history state")
	}
	if got := rt.PaneViewportOffset("pane-1"); got != 9 {
		t.Fatalf("expected restore to preserve pane viewport 9, got %d", got)
	}
	if gotX, gotY := rt.PaneContentOffset("pane-1"); gotX != 4 || gotY != -3 {
		t.Fatalf("expected restore to preserve pane content offset 4,-3, got %d,%d", gotX, gotY)
	}

	terminal.ScrollbackLoadedLimit = 1
	terminal.ScrollbackLoadingLimit = 0
	terminal.ScrollbackExhausted = true
	exhausted := rt.TerminalLiveStateSnapshot("term-1")

	terminal.ScrollbackLoadedLimit = 200
	terminal.ScrollbackLoadingLimit = 500
	terminal.ScrollbackExhausted = false

	rt.RestoreTerminalLiveState("term-1", exhausted)

	if got := terminal.ScrollbackLoadedLimit; got != 1 {
		t.Fatalf("expected exhausted restore to preserve loaded rows limit 1, got %d", got)
	}
	if got := terminal.ScrollbackLoadingLimit; got != 0 {
		t.Fatalf("expected exhausted restore to preserve cleared loading limit 0, got %d", got)
	}
	if !terminal.ScrollbackExhausted {
		t.Fatal("expected exhausted restore to keep exhausted=true")
	}
}
