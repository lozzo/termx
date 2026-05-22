package app

import (
	"testing"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	appruntime "github.com/lozzow/termx/tuiv2/runtime"
)

func TestTerminalHasKnownScrollbackBeyondIgnoresLiveTailOwnershipRows(t *testing.T) {
	terminal := &appruntime.TerminalRuntime{
		Snapshot: &protocol.Snapshot{
			TerminalID:             "term-1",
			Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromText("tail0", 20), protocolRowFromText("tail1", 20)}),
			ScrollbackTotal:        2,
			ScrollbackLogicalTotal: 2,
			ScrollbackOwnership:    []string{protocol.RowOwnershipLiveTailLive, protocol.RowOwnershipLiveTailLive},
		},
	}

	if terminalHasKnownScrollbackBeyond(terminal, 0) {
		t.Fatal("expected live-tail ownership rows not to count as known committed scrollback")
	}
}

func TestTerminalHasKnownScrollbackBeyondRequiresExplicitCommittedOwnership(t *testing.T) {
	terminal := &appruntime.TerminalRuntime{
		Snapshot: &protocol.Snapshot{
			TerminalID:          "term-1",
			Scrollback:          protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromText("committed0", 20), protocolRowFromText("committed1", 20)}),
			ScrollbackOwnership: []string{protocol.RowOwnershipPersisted, protocol.RowOwnershipPersisted},
		},
	}

	if !terminalHasKnownScrollbackBeyond(terminal, 0) {
		t.Fatal("expected committed ownership rows to count as known scrollback")
	}
}

func TestTerminalHasKnownScrollbackBeyondIgnoresTotalsAndHasMoreAsHistoryTruth(t *testing.T) {
	terminal := &appruntime.TerminalRuntime{
		Snapshot: &protocol.Snapshot{
			TerminalID:          "term-1",
			Scrollback:          protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromText("committed0", 20)}),
			ScrollbackTotal:     2000,
			ScrollbackHasMore:   true,
			ScrollbackOwnership: []string{protocol.RowOwnershipPersisted},
		},
	}

	if terminalHasKnownScrollbackBeyond(terminal, 1) {
		t.Fatal("expected totals/hasMore not to imply committed scrollback beyond ownership depth")
	}
}

func TestLoadTerminalHistoryViewportCmdIgnoresUnownedResponseState(t *testing.T) {
	model := setupModel(t, modelOpts{width: 50, height: 12})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.ScrollbackLoadedLimit = 0
	terminal.ScrollbackLoadingLimit = 500
	terminal.ScrollbackExhausted = false
	model.setHistoryLoadingOwner("term-1", 500, historyLoadingOwnerLive)

	client := model.runtime.Client().(*recordingBridgeClient)
	client.snapshotByTerminal["term-1"] = &protocol.Snapshot{
		TerminalID:        "term-1",
		Size:              protocol.Size{Cols: 120, Rows: 24},
		Scrollback:        repeatedCompactRows("old", 500, 120),
		ScrollbackTotal:   500,
		ScrollbackHasMore: false,
	}

	cmd := model.loadTerminalHistoryViewportCmd("term-1", 0, 500, 120, false)
	if cmd == nil {
		t.Fatal("expected history viewport command")
	}
	msg := cmd()
	if _, ok := msg.(orchestrator.SnapshotLoadedMsg); !ok {
		t.Fatalf("expected SnapshotLoadedMsg, got %#v", msg)
	}

	if got := terminal.ScrollbackLoadedLimit; got != 0 {
		t.Fatalf("expected unowned response not to advance committed depth, got %d", got)
	}
	if terminal.ScrollbackExhausted {
		t.Fatal("expected unowned response not to mark live history exhausted from hasMore")
	}
	if got := terminal.ScrollbackLoadingLimit; got != 0 {
		t.Fatalf("expected matching loading slot to clear after response, got %d", got)
	}
}
