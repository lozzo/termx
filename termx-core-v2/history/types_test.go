package history

import (
	"errors"
	"reflect"
	"testing"
)

func TestMemoryLogicalLineStoreCreatesStableLines(t *testing.T) {
	store := NewMemoryLogicalLineStore(nil)
	cells := []Cell{{Text: "hello"}}

	first, err := store.CreateLine(CreateLineRequest{Cells: cells, Dirty: true})
	if err != nil {
		t.Fatalf("create first line: %v", err)
	}
	if first.ID != 1 {
		t.Fatalf("expected first id 1, got %d", first.ID)
	}
	if first.Generation != 1 {
		t.Fatalf("expected initial generation 1, got %d", first.Generation)
	}
	if first.Seal != SealStateOpen {
		t.Fatalf("expected default open seal, got %q", first.Seal)
	}
	if first.Residency != ResidencyMemory {
		t.Fatalf("expected default memory residency, got %q", first.Residency)
	}
	if !first.Dirty {
		t.Fatal("expected first line to keep dirty state")
	}

	cells[0].Text = "mutated caller slice"
	first.Cells[0].Text = "mutated returned line"
	stored, ok := store.Line(1)
	if !ok {
		t.Fatal("expected stored line")
	}
	if got := stored.Cells[0].Text; got != "hello" {
		t.Fatalf("store leaked mutable cells, got %q", got)
	}

	second, err := store.CreateLine(CreateLineRequest{
		Seal:      SealStateSealed,
		Cells:     []Cell{{Text: "world"}},
		Residency: ResidencyFile,
	})
	if err != nil {
		t.Fatalf("create second line: %v", err)
	}
	if second.ID != 2 {
		t.Fatalf("expected second id 2, got %d", second.ID)
	}
	if second.Seal != SealStateSealed {
		t.Fatalf("expected sealed line, got %q", second.Seal)
	}
	if second.Residency != ResidencyFile {
		t.Fatalf("expected file residency, got %q", second.Residency)
	}
	if got := store.LineIDs(); !reflect.DeepEqual(got, []LogicalLineID{1, 2}) {
		t.Fatalf("unexpected line ids %v", got)
	}
}

func TestMemoryLogicalLineStoreReplacesPayloadAndBumpsGeneration(t *testing.T) {
	store := NewMemoryLogicalLineStore(nil)
	line, err := store.CreateLine(CreateLineRequest{Cells: []Cell{{Text: "old"}}})
	if err != nil {
		t.Fatalf("create line: %v", err)
	}

	line.Seal = SealStateSealed
	line.Cells = []Cell{{Text: "new"}}
	line.Dirty = true
	line.Residency = ResidencyFile
	replaced, err := store.ReplaceLine(line)
	if err != nil {
		t.Fatalf("replace line: %v", err)
	}
	if replaced.Generation != 2 {
		t.Fatalf("expected generation bump to 2, got %d", replaced.Generation)
	}
	if replaced.Cells[0].Text != "new" {
		t.Fatalf("unexpected replaced payload %q", replaced.Cells[0].Text)
	}
	if replaced.Residency != ResidencyFile {
		t.Fatalf("expected residency update, got %q", replaced.Residency)
	}

	replaced.Cells[0].Text = "mutated returned line"
	stored, ok := store.Line(line.ID)
	if !ok {
		t.Fatal("expected stored line")
	}
	if stored.Cells[0].Text != "new" {
		t.Fatalf("replace leaked returned cells into store, got %q", stored.Cells[0].Text)
	}

	_, err = store.ReplaceLine(LogicalLine{ID: 999, Seal: SealStateOpen, Residency: ResidencyMemory})
	if !errors.Is(err, ErrUnknownLine) {
		t.Fatalf("expected ErrUnknownLine, got %v", err)
	}
	_, err = store.ReplaceLine(LogicalLine{Seal: SealStateOpen, Residency: ResidencyMemory})
	if !errors.Is(err, ErrInvalidLineID) {
		t.Fatalf("expected ErrInvalidLineID, got %v", err)
	}
}

func TestMemoryStorageBackendOverwritesAndDeletesPersistedLines(t *testing.T) {
	backend := NewMemoryStorageBackend()
	line := LogicalLine{
		ID:         42,
		Generation: 3,
		Seal:       SealStateSealed,
		Cells:      []Cell{{Text: "persisted"}},
		Residency:  ResidencyFile,
	}
	if err := backend.SaveLine(line); err != nil {
		t.Fatalf("save line: %v", err)
	}

	loaded, ok := backend.LoadLine(42)
	if !ok {
		t.Fatal("expected loaded line")
	}
	loaded.Cells[0].Text = "caller mutation"
	loadedAgain, ok := backend.LoadLine(42)
	if !ok {
		t.Fatal("expected loaded line again")
	}
	if loadedAgain.Cells[0].Text != "persisted" {
		t.Fatalf("backend leaked loaded cells, got %q", loadedAgain.Cells[0].Text)
	}

	line.Generation = 4
	line.Cells = []Cell{{Text: "replaced after persist"}}
	if err := backend.SaveLine(line); err != nil {
		t.Fatalf("overwrite line: %v", err)
	}
	loaded, ok = backend.LoadLine(42)
	if !ok {
		t.Fatal("expected overwritten line")
	}
	if loaded.Generation != 4 || loaded.Cells[0].Text != "replaced after persist" {
		t.Fatalf("unexpected overwritten line: %#v", loaded)
	}
	if !backend.DeleteLine(42) {
		t.Fatal("expected delete to report true")
	}
	if _, ok := backend.LoadLine(42); ok {
		t.Fatal("expected deleted line to be absent")
	}
}

func TestCommittedHistoryIndexStoresMembershipOnly(t *testing.T) {
	index := NewCommittedHistoryIndex()
	if index.Generation() != 0 {
		t.Fatalf("unexpected initial generation %d", index.Generation())
	}
	if err := index.Append(1); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if err := index.Append(2); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	if err := index.Append(2); !errors.Is(err, ErrDuplicateLineID) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
	if got := index.IDs(); !reflect.DeepEqual(got, []LogicalLineID{1, 2}) {
		t.Fatalf("unexpected committed ids %v", got)
	}
	if !index.Contains(1) || !index.Contains(2) {
		t.Fatal("expected committed ids to be present")
	}
	if index.Generation() != 2 {
		t.Fatalf("expected generation 2 after two appends, got %d", index.Generation())
	}

	ids := index.IDs()
	ids[0] = 99
	if got := index.IDs(); !reflect.DeepEqual(got, []LogicalLineID{1, 2}) {
		t.Fatalf("index leaked ids slice, got %v", got)
	}
	if !index.Remove(1) {
		t.Fatal("expected remove to report true")
	}
	if index.Contains(1) {
		t.Fatal("expected removed id to be absent")
	}
	if got := index.IDs(); !reflect.DeepEqual(got, []LogicalLineID{2}) {
		t.Fatalf("unexpected committed ids after remove %v", got)
	}
}

func TestMutableFrontierIsIndependentFromCommittedIndex(t *testing.T) {
	index := NewCommittedHistoryIndex()
	frontier := NewMutableFrontier()

	if err := index.Append(7); err != nil {
		t.Fatalf("append committed id: %v", err)
	}
	if err := frontier.Add(7); err != nil {
		t.Fatalf("add frontier id: %v", err)
	}
	if !index.Contains(7) || !frontier.Contains(7) {
		t.Fatal("expected the same logical line to be committed and mutable")
	}

	if !index.Remove(7) {
		t.Fatal("expected committed remove to report true")
	}
	if frontier.Contains(7) != true {
		t.Fatal("committed remove must not remove mutable frontier membership")
	}
	if !frontier.Remove(7) {
		t.Fatal("expected frontier remove to report true")
	}
	if frontier.Contains(7) {
		t.Fatal("expected frontier id to be absent")
	}
}

func TestLogicalLineStateDimensionsAreOrthogonal(t *testing.T) {
	store := NewMemoryLogicalLineStore(nil)
	index := NewCommittedHistoryIndex()
	frontier := NewMutableFrontier()

	line, err := store.CreateLine(CreateLineRequest{
		Seal:      SealStateSealed,
		Cells:     []Cell{{Text: "tail"}},
		Dirty:     true,
		Residency: ResidencyFile,
	})
	if err != nil {
		t.Fatalf("create line: %v", err)
	}
	if err := index.Append(line.ID); err != nil {
		t.Fatalf("append committed id: %v", err)
	}
	if err := frontier.Add(line.ID); err != nil {
		t.Fatalf("add frontier id: %v", err)
	}

	stored, ok := store.Line(line.ID)
	if !ok {
		t.Fatal("expected stored line")
	}
	if stored.Seal != SealStateSealed {
		t.Fatalf("expected sealed line, got %q", stored.Seal)
	}
	if !stored.Dirty {
		t.Fatal("expected dirty line")
	}
	if stored.Residency != ResidencyFile {
		t.Fatalf("expected file residency, got %q", stored.Residency)
	}
	if !index.Contains(line.ID) || !frontier.Contains(line.ID) {
		t.Fatal("committed and mutable membership should remain orthogonal")
	}
}
