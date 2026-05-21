package app

import (
	"testing"

	"github.com/lozzow/termx/internal/protocol"
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
