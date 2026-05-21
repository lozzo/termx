package app

import (
	"testing"

	"github.com/lozzow/termx/internal/protocol"
	appruntime "github.com/lozzow/termx/tuiv2/runtime"
)

func TestTerminalHasKnownScrollbackBeyondIgnoresAuthoritativeHotOnlyRows(t *testing.T) {
	terminal := &appruntime.TerminalRuntime{
		Snapshot: &protocol.Snapshot{
			TerminalID:             "term-1",
			Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromText("hot0", 20), protocolRowFromText("hot1", 20)}),
			ScrollbackTotal:        2,
			ScrollbackLogicalTotal: 2,
			ScrollbackLoadedRows:   0,
			HistoryGeneration:      0,
		},
	}

	if terminalHasKnownScrollbackBeyond(terminal, 0) {
		t.Fatal("expected authoritative hot-only rows not to count as known committed scrollback")
	}
}

func TestTerminalHasKnownScrollbackBeyondKeepsLegacyVisualOnlyFallback(t *testing.T) {
	terminal := &appruntime.TerminalRuntime{
		Snapshot: &protocol.Snapshot{
			TerminalID:           "term-1",
			Scrollback:           protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromText("legacy0", 20), protocolRowFromText("legacy1", 20)}),
			ScrollbackLoadedRows: 0,
			HistoryGeneration:    0,
		},
	}

	if !terminalHasKnownScrollbackBeyond(terminal, 0) {
		t.Fatal("expected legacy visual-only rows to remain a compatible known scrollback fallback")
	}
}
