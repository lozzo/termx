package app

import (
	"testing"

	"github.com/lozzow/termx/internal/protocol"
)

func TestLocalViewProjectionPreservesPaneViewport(t *testing.T) {
	model := setupModel(t, modelOpts{})

	_ = model.runtime.SetPaneViewportOffset("pane-1", 4)
	_ = model.runtime.SetPaneContentOffset("pane-1", 3, 2)
	proj := model.captureLocalViewProjection()
	_ = model.runtime.SetPaneViewportOffset("pane-1", 0)
	_ = model.runtime.SetPaneContentOffset("pane-1", 0, 0)

	model.applyLocalViewProjection(proj)

	if got := model.runtime.PaneViewportOffset("pane-1"); got != 4 {
		t.Fatalf("expected local view projection to restore pane viewport 4, got %d", got)
	}
	if gotX, gotY := model.runtime.PaneContentOffset("pane-1"); gotX != 3 || gotY != 2 {
		t.Fatalf("expected local view projection to restore pane content offset 3,2 got %d,%d", gotX, gotY)
	}
}

func TestLocalViewProjectionNilModelCompatibility(t *testing.T) {
	var model *Model

	proj := model.captureLocalViewProjection()
	if proj.WorkspaceName != "" || proj.ActiveTabID != "" || proj.FocusedPaneID != "" {
		t.Fatalf("expected zero projection for nil model, got %#v", proj)
	}

	model.applyLocalViewProjection(localViewProjection{
		WorkspaceName: "main",
		ActiveTabID:   "tab-1",
		FocusedPaneID: "pane-1",
	})
}

func TestPaneScrollbackPrefetchUsesCanonicalCols(t *testing.T) {
	model := setupModel(t, modelOpts{width: 50, height: 12})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 120, Rows: 24},
		Scrollback: []protocol.CompactRow{
			protocol.CompactRowFromCells([]protocol.Cell{{Content: "seed", Width: 1}}),
		},
		ScrollbackHasMore: true,
	}
	_ = model.runtime.SetPaneViewportOffset("pane-1", 20)
	client := model.runtime.Client().(*recordingBridgeClient)
	client.snapshotByTerminal["term-1"] = &protocol.Snapshot{
		TerminalID:        "term-1",
		Size:              protocol.Size{Cols: 120, Rows: 24},
		Scrollback:        []protocol.CompactRow{protocol.CompactRowFromCells([]protocol.Cell{{Content: "older", Width: 1}})},
		ScrollbackHasMore: false,
		ScrollbackTotal:   1,
		ScrollbackOffset:  1,
	}

	cmd := model.ensureActivePaneScrollbackCmd()
	if cmd == nil {
		t.Fatal("expected pane scrollback prefetch command")
	}
	_ = cmd()

	if got := len(client.viewportRequests); got != 1 {
		t.Fatalf("expected one pane history request, got %#v", client.viewportRequests)
	}
	if got := client.viewportRequests[0].cols; got != 120 {
		t.Fatalf("expected pane history request to use canonical cols 120, got %d", got)
	}
}
