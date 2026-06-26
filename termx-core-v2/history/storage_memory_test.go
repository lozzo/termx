package history

import "testing"

func TestR309MemoryStorageRecoverKeepsDomainStructuresSeparate(t *testing.T) {
	backend := NewMemoryStorageBackend()
	err := backend.Apply(StorageTransaction{
		Generation: 7,
		Lines: []LogicalLine{
			{ID: 1, Kind: string(LineKindOrdinary), Cells: []Cell{{Text: "a", Width: 1}}},
			{ID: 2, Kind: string(LineKindScreenFrame), Cells: []Cell{{Text: "f", Width: 1}}, ScreenCols: 80},
		},
		Committed: []LogicalLineID{1},
		Frontier:  []LogicalLineID{2},
		Frames: []FrameRecord{{
			SessionID: 1,
			FrameID:   2,
			LineIDs:   []LogicalLineID{2},
			ScreenSize: TerminalSemanticSize{
				Cols: 80,
				Rows: 24,
			},
		}},
	})
	if err != nil {
		t.Fatalf("apply storage transaction: %v", err)
	}

	state, err := backend.Recover()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if state.Generation != 7 || len(state.Lines) != 2 || len(state.Committed) != 1 || len(state.Frontier) != 1 || len(state.Frames) != 1 {
		t.Fatalf("recovered state lost domain structures: %#v", state)
	}
	if state.Lines[1].ScreenCols != 80 {
		t.Fatalf("screen frame payload metadata must recover from payload truth, got %#v", state.Lines[1])
	}
}

func TestR309MemoryStoragePersistedLineCanBeUpdated(t *testing.T) {
	backend := NewMemoryStorageBackend()
	if err := backend.Apply(StorageTransaction{
		Generation: 1,
		Lines:      []LogicalLine{{ID: 1, Cells: []Cell{{Text: "old", Width: 3}}, Residency: ResidencyMemory}},
		Committed:  []LogicalLineID{1},
	}); err != nil {
		t.Fatalf("apply initial line: %v", err)
	}
	if err := backend.Apply(StorageTransaction{
		Generation: 2,
		Lines:      []LogicalLine{{ID: 1, Cells: []Cell{{Text: "new", Width: 3}}, Residency: ResidencyFile}},
		Committed:  []LogicalLineID{1},
	}); err != nil {
		t.Fatalf("update persisted line: %v", err)
	}
	state, err := backend.Recover()
	if err != nil {
		t.Fatalf("recover updated line: %v", err)
	}
	if len(state.Lines) != 1 || plainText(state.Lines[0].Cells) != "new" {
		t.Fatalf("backend must not treat persisted line as immutable, got %#v", state.Lines)
	}
}

func TestR309MemoryStorageCompactKeepsReferencedPayloadOnly(t *testing.T) {
	backend := NewMemoryStorageBackend()
	if err := backend.Apply(StorageTransaction{
		Generation: 1,
		Lines: []LogicalLine{
			{ID: 1, Cells: []Cell{{Text: "committed", Width: 9}}},
			{ID: 2, Cells: []Cell{{Text: "frontier", Width: 8}}},
			{ID: 3, Cells: []Cell{{Text: "orphan", Width: 6}}},
		},
		Committed: []LogicalLineID{1},
		Frontier:  []LogicalLineID{2},
	}); err != nil {
		t.Fatalf("apply lines: %v", err)
	}
	if err := backend.Compact(StorageCompactionPolicy{MaxFrames: 0}); err != nil {
		t.Fatalf("compact: %v", err)
	}
	state, err := backend.Recover()
	if err != nil {
		t.Fatalf("recover compacted state: %v", err)
	}
	if len(state.Lines) != 2 || state.Lines[0].ID != 1 || state.Lines[1].ID != 2 {
		t.Fatalf("compact should delete only unreferenced payload, got %#v", state.Lines)
	}
}
