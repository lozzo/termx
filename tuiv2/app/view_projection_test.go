package app

import (
	"context"
	"testing"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/tuiv2/orchestrator"
)

func repeatedCompactRows(text string, count int, cols int) []protocol.CompactRow {
	if count <= 0 {
		return nil
	}
	rows := make([]protocol.CompactRow, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, protocol.CompactRowFromCells(protocolRowFromText(text, cols)))
	}
	return rows
}

func repeatedOwnership(ownership string, count int) []string {
	if count <= 0 || ownership == "" {
		return nil
	}
	values := make([]string, count)
	for i := range values {
		values[i] = ownership
	}
	return values
}

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
	msg := cmd()
	typed, ok := msg.(orchestrator.SnapshotLoadedMsg)
	if !ok {
		t.Fatalf("expected SnapshotLoadedMsg, got %#v", msg)
	}
	if typed.CopyModeRequest {
		t.Fatalf("expected active pane history request to stay on live/runtime path, got %#v", typed)
	}

	if got := len(client.viewportRequests); got != 1 {
		t.Fatalf("expected one pane history request, got %#v", client.viewportRequests)
	}
	if got := client.viewportRequests[0].cols; got != 120 {
		t.Fatalf("expected pane history request to use canonical cols 120, got %d", got)
	}
}

func TestPaneScrollbackPrefetchUsesLoadedDepthBeyondMaterializedWindow(t *testing.T) {
	model := setupModel(t, modelOpts{width: 50, height: 12})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID:           "term-1",
		Size:                 protocol.Size{Cols: 120, Rows: 24},
		Scrollback:           []protocol.CompactRow{protocol.CompactRowFromCells([]protocol.Cell{{Content: "seed", Width: 1}})},
		ScrollbackHasMore:    true,
		ScrollbackLoadedRows: 1500,
		ScrollbackOffset:     1200,
	}
	terminal.CommittedLoadedDepth = 1500
	_ = model.runtime.SetPaneViewportOffset("pane-1", 1400)

	client := model.runtime.Client().(*recordingBridgeClient)
	client.snapshotByTerminal["term-1"] = &protocol.Snapshot{
		TerminalID:           "term-1",
		Size:                 protocol.Size{Cols: 120, Rows: 24},
		Scrollback:           []protocol.CompactRow{protocol.CompactRowFromCells([]protocol.Cell{{Content: "older", Width: 1}})},
		ScrollbackHasMore:    true,
		ScrollbackTotal:      2001,
		ScrollbackOffset:     1500,
		ScrollbackLoadedRows: 2000,
	}

	cmd := model.ensureActivePaneScrollbackCmd()
	if cmd == nil {
		t.Fatal("expected pane scrollback prefetch command for materialized-window gap")
	}
	_ = cmd()

	if got := len(client.viewportRequests); got != 1 {
		t.Fatalf("expected one pane history request, got %#v", client.viewportRequests)
	}
	request := client.viewportRequests[0]
	if request.offset != 1500 {
		t.Fatalf("expected history request to continue from loaded depth 1500, got %#v", request)
	}
	if request.limit != 500 {
		t.Fatalf("expected next page size 500, got %#v", request)
	}
}

func TestPaneScrollbackPrefetchDoesNotTreatLiveTailOwnershipRowsAsCommittedHistory(t *testing.T) {
	model := setupModel(t, modelOpts{width: 50, height: 12})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 120, Rows: 24},
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromText("tail0", 120), protocolRowFromText("tail1", 120)}),
		ScrollbackTotal:        2,
		ScrollbackLogicalTotal: 2,
		ScrollbackOwnership:    repeatedOwnership(protocol.RowOwnershipLiveTailLive, 2),
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			protocolRowFromText("live0", 120),
		}},
	}
	terminal.CommittedHistoryExhausted = true
	_ = model.runtime.SetPaneViewportOffset("pane-1", 20)
	client := model.runtime.Client().(*recordingBridgeClient)

	cmd := model.ensureActivePaneScrollbackCmd()
	if cmd != nil {
		t.Fatal("expected no pane scrollback prefetch command for live-tail ownership rows")
	}
	if got := len(client.viewportRequests); got != 0 {
		t.Fatalf("expected no pane history request for live-tail ownership rows, got %#v", client.viewportRequests)
	}
}

func TestPaneScrollbackPrefetchUsesZeroOffsetAfterLiveTailOwnershipLatestReplace(t *testing.T) {
	model := setupModel(t, modelOpts{width: 50, height: 12})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID:           "term-1",
		Size:                 protocol.Size{Cols: 120, Rows: 24},
		Scrollback:           []protocol.CompactRow{protocol.CompactRowFromCells([]protocol.Cell{{Content: "canon", Width: 1}})},
		ScrollbackHasMore:    true,
		ScrollbackLoadedRows: 12000,
		ScrollbackOffset:     0,
		ScrollbackTotal:      12000,
		HistoryGeneration:    10,
	}
	terminal.CommittedLoadedDepth = 12000
	_ = model.runtime.SetPaneViewportOffset("pane-1", 20)

	client := model.runtime.Client().(*recordingBridgeClient)
	client.snapshotByTerminal["term-1"] = &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 120, Rows: 24},
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromText("tail0", 120), protocolRowFromText("tail1", 120)}),
		ScrollbackOffset:       0,
		ScrollbackTotal:        4,
		ScrollbackLogicalTotal: 4,
		ScrollbackHasMore:      true,
		ScrollbackOwnership:    repeatedOwnership(protocol.RowOwnershipLiveTailLive, 2),
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			protocolRowFromText("live0", 120),
		}},
	}

	if _, err := model.runtime.LoadSnapshot(context.Background(), "term-1", 0, 0); err != nil {
		t.Fatalf("load live-tail ownership latest snapshot: %v", err)
	}
	terminal = model.runtime.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected runtime terminal")
	}
	if got := terminal.CommittedLoadedDepth; got != 0 {
		t.Fatalf("expected latest replace to clear known committed depth, got %d", got)
	}

	cmd := model.ensureActivePaneScrollbackCmd()
	if cmd == nil {
		t.Fatal("expected pane scrollback prefetch command after live-tail ownership latest replace")
	}
	_ = cmd()

	if got := len(client.viewportRequests); got != 1 {
		t.Fatalf("expected one pane history request, got %#v", client.viewportRequests)
	}
	request := client.viewportRequests[0]
	if request.offset != 0 {
		t.Fatalf("expected pane history request to restart from committed depth 0, got %#v", request)
	}
	if request.limit != 500 {
		t.Fatalf("expected default page size 500 from committed depth 0, got %#v", request)
	}
}

func TestPaneScrollbackPrefetchUsesZeroOffsetAfterLiveTailOwnershipDisplayTail(t *testing.T) {
	model := setupModel(t, modelOpts{width: 50, height: 12})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 120, Rows: 24},
		Scrollback: protocol.CompactRowsFromCells([][]protocol.Cell{
			protocolRowFromText("tail0", 120),
		}),
		ScrollbackLoadedRows:   0,
		ScrollbackOffset:       0,
		ScrollbackTotal:        0,
		ScrollbackLogicalTotal: 0,
		ScrollbackHasMore:      false,
		HistoryGeneration:      0,
		ScrollbackOwnership:    repeatedOwnership(protocol.RowOwnershipLiveTailLive, 1),
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			protocolRowFromText("fresh", 120),
		}},
	}
	terminal.CommittedLoadedDepth = 0
	terminal.CommittedHistoryExhausted = false
	_ = model.runtime.SetPaneViewportOffset("pane-1", 20)

	client := model.runtime.Client().(*recordingBridgeClient)
	client.snapshotByTerminal["term-1"] = &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 120, Rows: 24},
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromText("canon0", 120)}),
		ScrollbackOffset:       0,
		ScrollbackTotal:        1,
		ScrollbackLogicalTotal: 1,
		ScrollbackHasMore:      false,
		ScrollbackLoadedRows:   1,
		HistoryGeneration:      11,
		ScrollbackFirstRowID:   0,
		ScrollbackLastRowID:    0,
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			protocolRowFromText("fresh", 120),
		}},
	}

	cmd := model.ensureActivePaneScrollbackCmd()
	if cmd == nil {
		t.Fatal("expected pane scrollback prefetch command after full-replace boundary reset")
	}
	_ = cmd()

	if got := len(client.viewportRequests); got != 1 {
		t.Fatalf("expected one pane history request, got %#v", client.viewportRequests)
	}
	request := client.viewportRequests[0]
	if request.offset != 0 {
		t.Fatalf("expected pane history request to restart from committed depth 0 after full-replace boundary reset, got %#v", request)
	}
	if request.limit != 500 {
		t.Fatalf("expected default page size 500 from committed depth 0 after full-replace boundary reset, got %#v", request)
	}
}

func TestPaneScrollbackStaleLiveResponseDoesNotRestoreBoundarySideStateAfterFullReplaceReset(t *testing.T) {
	model := setupModel(t, modelOpts{width: 50, height: 12})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 120, Rows: 24},
		Scrollback: protocol.CompactRowsFromCells([][]protocol.Cell{
			protocolRowFromText("tail0", 120),
		}),
		ScrollbackLoadedRows:   0,
		ScrollbackOffset:       0,
		ScrollbackTotal:        0,
		ScrollbackLogicalTotal: 0,
		ScrollbackHasMore:      false,
		HistoryGeneration:      0,
		ScrollbackOwnership:    repeatedOwnership(protocol.RowOwnershipLiveTailLive, 1),
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			protocolRowFromText("fresh", 120),
		}},
	}
	terminal.CommittedLoadedDepth = 0
	terminal.CommittedLoadingDepth = 0
	terminal.CommittedHistoryExhausted = false

	model.setHistoryLoadingOwner("term-1", 500, historyLoadingOwnerLive)
	terminal.CommittedLoadingDepth = 500

	client := model.runtime.Client().(*recordingBridgeClient)
	client.snapshotByTerminal["term-1"] = &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 120, Rows: 24},
		Scrollback:             repeatedCompactRows("old", 12500, 120),
		ScrollbackOffset:       0,
		ScrollbackTotal:        12500,
		ScrollbackLogicalTotal: 12500,
		ScrollbackHasMore:      false,
		ScrollbackLoadedRows:   12500,
		HistoryGeneration:      10,
		ScrollbackFirstRowID:   0,
		ScrollbackLastRowID:    12499,
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			protocolRowFromText("fresh", 120),
		}},
	}

	staleLiveCmd := model.loadTerminalHistoryViewportCmd("term-1", 12000, 500, 120, false)
	if staleLiveCmd == nil {
		t.Fatal("expected stale live history viewport command")
	}
	msg := staleLiveCmd()
	typed, ok := msg.(orchestrator.SnapshotLoadedMsg)
	if !ok {
		t.Fatalf("expected SnapshotLoadedMsg, got %#v", msg)
	}
	if typed.CopyModeRequest {
		t.Fatalf("expected stale live request to stay on live/runtime path, got %#v", typed)
	}

	if got := len(client.viewportRequests); got != 1 {
		t.Fatalf("expected one stale live history request, got %#v", client.viewportRequests)
	}
	if request := client.viewportRequests[0]; request.offset != 12000 || request.limit != 500 {
		t.Fatalf("expected stale live history request to keep original offset/limit, got %#v", request)
	}
	if got := terminal.CommittedLoadedDepth; got != 0 {
		t.Fatalf("expected stale live response not to restore committed depth after full-replace reset, got %d", got)
	}
	if terminal.CommittedHistoryExhausted {
		t.Fatal("expected stale live response not to mark live exhausted after full-replace reset")
	}
	if got, want := terminal.CommittedLoadingDepth, 500; got != want {
		t.Fatalf("expected stale live response not to clear current reset-owned loading limit %d, got %d", want, got)
	}
	if state, ok := model.historyLoading["term-1"]; !ok || state.Owner != historyLoadingOwnerLive || state.Limit != 500 {
		t.Fatalf("expected stale live response not to replace current reset-owned loading slot, got %#v ok=%v", state, ok)
	}
}
