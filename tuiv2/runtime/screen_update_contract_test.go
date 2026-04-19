package runtime

import (
	"testing"

	"github.com/lozzow/termx/protocol"
)

func TestDecodeScreenUpdateContractPayloadNormalizesChangedRows(t *testing.T) {
	update := protocol.ScreenUpdate{
		Size: protocol.Size{Cols: 8, Rows: 2},
		ChangedRows: []protocol.ScreenRowUpdate{
			{Row: 0, Cells: []protocol.Cell{{Content: "old", Width: 1}}},
			{Row: 0, Cells: []protocol.Cell{{Content: "new", Width: 1}}},
		},
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	}

	payload, err := protocol.EncodeScreenUpdatePayload(update)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	contract, err := DecodeScreenUpdateContractPayload(payload)
	if err != nil {
		t.Fatalf("decode contract: %v", err)
	}
	if len(contract.Update.ChangedRows) != 1 {
		t.Fatalf("expected duplicate changed rows collapsed, got %#v", contract.Update.ChangedRows)
	}
	if got := contract.Update.ChangedRows[0].Cells[0].Content; got != "new" {
		t.Fatalf("expected normalized changed row to keep latest payload, got %#v", contract.Update.ChangedRows[0])
	}
	if len(contract.Summary.ChangedRows) != 1 || contract.Summary.ChangedRows[0] != 0 {
		t.Fatalf("expected summary changed rows deduped, got %#v", contract.Summary)
	}
}

func TestClassifyDecodedScreenUpdateMarksBootstrapPlaceholder(t *testing.T) {
	terminal := &TerminalRuntime{
		BootstrapPending: true,
		Snapshot:         &protocol.Snapshot{TerminalID: "term-1"},
	}
	contract := NewScreenUpdateContract(protocol.ScreenUpdate{
		FullReplace: true,
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			{{Content: " ", Width: 1}},
		}},
	})

	classified := classifyDecodedScreenUpdate(terminal, contract)
	if classified.Origin != screenUpdateOriginBootstrap {
		t.Fatalf("expected bootstrap origin, got %#v", classified)
	}
	if classified.Lifecycle != screenUpdateLifecyclePlaceholder {
		t.Fatalf("expected blank bootstrap full replace to classify as placeholder, got %#v", classified)
	}
	if classified.AdvanceBootstrap || classified.ClearRecovery {
		t.Fatalf("expected bootstrap placeholder to avoid state transitions, got %#v", classified)
	}
}

func TestClassifyDecodedScreenUpdateKeepsBootstrapPendingForTitleOnlyUpdate(t *testing.T) {
	terminal := &TerminalRuntime{
		BootstrapPending: true,
		Snapshot:         &protocol.Snapshot{TerminalID: "term-1"},
	}
	contract := NewScreenUpdateContract(protocol.ScreenUpdate{
		Title:  "demo",
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})

	classified := classifyDecodedScreenUpdate(terminal, contract)
	if classified.Origin != screenUpdateOriginBootstrap || classified.Lifecycle != screenUpdateLifecycleNoop {
		t.Fatalf("expected bootstrap noop classification, got %#v", classified)
	}
	if classified.AdvanceBootstrap || classified.ClearRecovery {
		t.Fatalf("expected title-only bootstrap update to avoid lifecycle boundary, got %#v", classified)
	}
}

func TestClassifyDecodedScreenUpdateMarksRecoveryFullReplaceBoundary(t *testing.T) {
	terminal := &TerminalRuntime{
		Recovery: RecoveryState{SyncLost: true, DroppedBytes: 17},
	}
	contract := NewScreenUpdateContract(protocol.ScreenUpdate{
		FullReplace: true,
		Size:        protocol.Size{Cols: 4, Rows: 1},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			{{Content: "o", Width: 1}, {Content: "k", Width: 1}},
		}},
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})

	classified := classifyDecodedScreenUpdate(terminal, contract)
	if classified.Origin != screenUpdateOriginRecovery || classified.Lifecycle != screenUpdateLifecycleFullReplace {
		t.Fatalf("expected recovery full-replace classification, got %#v", classified)
	}
	if classified.AdvanceBootstrap {
		t.Fatalf("expected recovery full replace to avoid bootstrap transition, got %#v", classified)
	}
	if !classified.ClearRecovery {
		t.Fatalf("expected recovery full replace to clear recovery, got %#v", classified)
	}
}
