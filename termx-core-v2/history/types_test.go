package history

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unsafe"
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

func TestMemoryStorageBackendCompactsCleanSealedLines(t *testing.T) {
	backend := NewMemoryStorageBackend()
	line := LogicalLine{
		ID:                7,
		Generation:        3,
		CreatedGeneration: 2,
		ContentGeneration: 3,
		Seal:              SealStateSealed,
		Cells: []Cell{
			{
				Text:       "styled",
				Width:      6,
				Style:      CellStyle{FG: "ansi:2", BG: "ansi:4", Bold: true, Underline: true},
				LinkURL:    "https://example.test",
				LinkParams: "id=7",
			},
			{
				Text:  "tail",
				Width: 4,
				Style: CellStyle{Italic: true, Reverse: true},
			},
		},
		TailFill:  &RowTailFill{Style: CellStyle{BG: "ansi:5"}},
		Residency: ResidencyMemory,
	}
	if err := backend.SaveLine(line); err != nil {
		t.Fatalf("save line: %v", err)
	}
	stored, ok := backend.compactLine(7)
	if !ok || len(stored.EncodedCells) == 0 {
		t.Fatalf("expected clean sealed line to use compact storage, got %#v", stored)
	}
	if got, want := len(backend.compactLines[compactDenseIndex(7)]), len(encodeCompactLogicalLine(stored)); got != want {
		t.Fatalf("dense compact slot should store only encoded payload, got len=%d want=%d", got, want)
	}
	if _, ok := backend.lines[7]; ok {
		t.Fatalf("compact line must not keep ordinary storage copy, got %#v", backend.lines[7])
	}

	loaded, ok := backend.LoadLine(7)
	if !ok {
		t.Fatal("expected compact line to load")
	}
	if !reflect.DeepEqual(loaded, line) {
		t.Fatalf("loaded compact line changed payload:\nwant %#v\ngot  %#v", line, loaded)
	}
	loaded.Cells[0].Text = "caller mutation"
	loaded.TailFill.Style.BG = "mutated"

	loadedAgain, ok := backend.LoadLine(7)
	if !ok {
		t.Fatal("expected compact line to load again")
	}
	if !reflect.DeepEqual(loadedAgain, line) {
		t.Fatalf("compact line leaked caller mutation:\nwant %#v\ngot  %#v", line, loadedAgain)
	}
	snapshotLine, ok := backend.LoadSnapshotLine(7)
	if !ok {
		t.Fatal("expected compact snapshot line")
	}
	if !reflect.DeepEqual(snapshotLine, line) {
		t.Fatalf("snapshot compact line changed payload:\nwant %#v\ngot  %#v", line, snapshotLine)
	}

	line.Dirty = true
	if err := backend.SaveLine(line); err != nil {
		t.Fatalf("save dirty line: %v", err)
	}
	if _, ok := backend.compactLine(7); ok {
		t.Fatalf("dirty line must stay mutable in ordinary storage, got %#v", stored)
	}
	storedLine, ok := backend.lines[7]
	if !ok || len(storedLine.Cells) == 0 {
		t.Fatalf("dirty line must stay mutable in ordinary storage, got %#v", backend.lines[7])
	}
}

func TestMemoryStorageBackendCompactsSparseHighIDWithoutDenseGap(t *testing.T) {
	backend := NewMemoryStorageBackend()
	line := LogicalLine{
		ID:        100_000,
		Seal:      SealStateSealed,
		Cells:     []Cell{{Text: "styled", Width: 6, Style: CellStyle{FG: "ansi:3"}}},
		Residency: ResidencyMemory,
	}
	if err := backend.SaveLine(line); err != nil {
		t.Fatalf("save high id compact line: %v", err)
	}
	if got, wantMax := len(backend.compactLines), maxCompactDenseGap+1; got > wantMax {
		t.Fatalf("high sparse id should not expand dense compact storage, len=%d want <= %d", got, wantMax)
	}
	if _, ok := backend.compactLine(line.ID); !ok {
		t.Fatal("expected high id compact line to load from sparse storage")
	}
	loaded, ok := backend.LoadLine(line.ID)
	if !ok {
		t.Fatal("expected high id compact line to load")
	}
	if !reflect.DeepEqual(loaded, line) {
		t.Fatalf("loaded high id compact line changed payload:\nwant %#v\ngot  %#v", line, loaded)
	}
	if got := backend.LineIDs(); !reflect.DeepEqual(got, []LogicalLineID{line.ID}) {
		t.Fatalf("unexpected line ids: %#v", got)
	}
}

func TestFileBackedMemoryStorageBackendCompactsCleanSealedLinesOffHeap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.compact")
	backend, err := NewFileBackedMemoryStorageBackend(path)
	if err != nil {
		t.Fatalf("create file backend: %v", err)
	}
	t.Cleanup(func() {
		_ = backend.Close()
	})
	line := LogicalLine{
		ID:                7,
		Generation:        3,
		CreatedGeneration: 2,
		ContentGeneration: 3,
		Seal:              SealStateSealed,
		Cells: []Cell{
			{Text: "styled", Width: 6, Style: CellStyle{FG: "ansi:2", BG: "ansi:4", Bold: true}},
			{Text: "tail", Width: 4, Style: CellStyle{Underline: true}},
		},
		TailFill:  &RowTailFill{Style: CellStyle{BG: "ansi:5"}},
		Residency: ResidencyMemory,
	}
	if err := backend.SaveLine(line); err != nil {
		t.Fatalf("save file compact line: %v", err)
	}
	if _, ok := backend.lines[line.ID]; ok {
		t.Fatalf("file compact line must not keep ordinary storage copy, got %#v", backend.lines[line.ID])
	}
	if len(backend.compactLines) != 0 || backend.compactSparse != nil {
		t.Fatalf("file compact backend must not keep encoded payload in memory, dense=%d sparse=%#v", len(backend.compactLines), backend.compactSparse)
	}
	slot := backend.compactFileLines[compactDenseIndex(line.ID)]
	if !slot.Present() || slot.Length() == 0 {
		t.Fatalf("expected file compact slot, got %#v", slot)
	}
	if backend.compactLineCount != 1 {
		t.Fatalf("unexpected compact count %d", backend.compactLineCount)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat compact file: %v", err)
	}
	if info.Size() != int64(slot.Length()) {
		t.Fatalf("compact file size mismatch got=%d want=%d", info.Size(), slot.Length())
	}

	loaded, ok := backend.LoadLine(line.ID)
	if !ok {
		t.Fatal("expected file compact line to load")
	}
	if !reflect.DeepEqual(loaded, line) {
		t.Fatalf("loaded file compact line changed payload:\nwant %#v\ngot  %#v", line, loaded)
	}
	loaded.Cells[0].Text = "caller mutation"
	loaded.TailFill.Style.BG = "mutated"
	loadedAgain, ok := backend.LoadLine(line.ID)
	if !ok {
		t.Fatal("expected file compact line to load again")
	}
	if !reflect.DeepEqual(loadedAgain, line) {
		t.Fatalf("file compact line leaked caller mutation:\nwant %#v\ngot  %#v", line, loadedAgain)
	}
	snapshotLine, ok := backend.LoadSnapshotLine(line.ID)
	if !ok {
		t.Fatal("expected file compact snapshot line")
	}
	if !reflect.DeepEqual(snapshotLine, line) {
		t.Fatalf("snapshot file compact line changed payload:\nwant %#v\ngot  %#v", line, snapshotLine)
	}
	if got := backend.LineIDs(); !reflect.DeepEqual(got, []LogicalLineID{line.ID}) {
		t.Fatalf("unexpected file compact line ids: %#v", got)
	}
	if !backend.HasLine(line.ID) {
		t.Fatal("expected file compact backend to report line exists")
	}

	line.Generation = 4
	line.Cells = []Cell{{Text: "replaced", Width: 8, Style: CellStyle{FG: "ansi:3"}}}
	if err := backend.SaveLine(line); err != nil {
		t.Fatalf("overwrite file compact line: %v", err)
	}
	updatedSlot := backend.compactFileLines[compactDenseIndex(line.ID)]
	if !updatedSlot.Present() || updatedSlot.Offset() <= slot.Offset() {
		t.Fatalf("expected overwritten file compact line to append new payload, old=%#v updated=%#v", slot, updatedSlot)
	}
	loaded, ok = backend.LoadLine(line.ID)
	if !ok || !reflect.DeepEqual(loaded, line) {
		t.Fatalf("unexpected overwritten file compact line ok=%v line=%#v", ok, loaded)
	}
	if !backend.DeleteLine(line.ID) {
		t.Fatal("expected file compact delete to report true")
	}
	if backend.HasLine(line.ID) {
		t.Fatal("expected deleted file compact line to be absent")
	}
	if got := backend.LineIDs(); len(got) != 0 {
		t.Fatalf("expected no line ids after delete, got %#v", got)
	}
	if backend.compactLineCount != 0 {
		t.Fatalf("expected compact count 0 after delete, got %d", backend.compactLineCount)
	}
}

func TestFileBackedMemoryStorageBackendCompactsSparseHighIDWithoutDenseGap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.compact")
	backend, err := NewFileBackedMemoryStorageBackend(path)
	if err != nil {
		t.Fatalf("create file backend: %v", err)
	}
	t.Cleanup(func() {
		_ = backend.Close()
	})
	line := LogicalLine{
		ID:        100_000,
		Seal:      SealStateSealed,
		Cells:     []Cell{{Text: "styled", Width: 6, Style: CellStyle{FG: "ansi:3"}}},
		Residency: ResidencyMemory,
	}
	if err := backend.SaveLine(line); err != nil {
		t.Fatalf("save high id file compact line: %v", err)
	}
	if got, wantMax := len(backend.compactFileLines), maxCompactDenseGap+1; got > wantMax {
		t.Fatalf("high sparse id should not expand dense file compact storage, len=%d want <= %d", got, wantMax)
	}
	if backend.compactFileSparse == nil || !backend.compactFileSparse[line.ID].Present() {
		t.Fatalf("expected high id file compact line to use sparse slot, got %#v", backend.compactFileSparse)
	}
	loaded, ok := backend.LoadLine(line.ID)
	if !ok {
		t.Fatal("expected high id file compact line to load")
	}
	if !reflect.DeepEqual(loaded, line) {
		t.Fatalf("loaded high id file compact line changed payload:\nwant %#v\ngot  %#v", line, loaded)
	}
	if got := backend.LineIDs(); !reflect.DeepEqual(got, []LogicalLineID{line.ID}) {
		t.Fatalf("unexpected high id file compact line ids: %#v", got)
	}
}

func TestCompactCellsEncodedCapacityMatchesEncodedLength(t *testing.T) {
	cells := []Cell{
		{Text: "plain", Width: 5},
		{Text: strings.Repeat("x", 180), Width: 180, Style: CellStyle{FG: "ansi:2", BG: "idx:24", Bold: true, Underline: true}},
		{Text: "link", Width: 4, Style: CellStyle{FG: "#ffcc00", Reverse: true}, LinkURL: "file://build.log", LinkParams: "line=7"},
	}

	encoded := encodeCompactCells(cells)
	if got, want := cap(encoded), len(encoded); got != want {
		t.Fatalf("compact cells should allocate exact backing capacity, cap=%d len=%d", got, want)
	}
	if got, want := compactCellsEncodedCapacity(cells), len(encoded); got != want {
		t.Fatalf("compact capacity estimate mismatch, got=%d want=%d", got, want)
	}
	if decoded := decodeCompactCells(encoded); !reflect.DeepEqual(decoded, cells) {
		t.Fatalf("compact round trip changed cells:\nwant %#v\ngot  %#v", cells, decoded)
	}
}

func TestCompactCellsRunEncodingPreservesStylesLinksAndWidths(t *testing.T) {
	cells := []Cell{
		{Text: "alpha ", Width: 6, Style: CellStyle{FG: "idx:42", BG: "ansi:4", Bold: true}, LinkURL: "file://build.log", LinkParams: "line=7"},
		{Text: "beta", Width: 4, Style: CellStyle{FG: "idx:42", BG: "ansi:4", Bold: true}, LinkURL: "file://build.log", LinkParams: "line=7"},
		{Text: "好", Width: 2, Style: CellStyle{FG: "#ffcc00", Underline: true}},
		{Text: "literal", Width: 7, Style: CellStyle{FG: "theme:accent", Reverse: true}},
	}

	encoded := encodeCompactCells(cells)
	if got, want := cap(encoded), len(encoded); got != want {
		t.Fatalf("compact cells should allocate exact backing capacity, cap=%d len=%d", got, want)
	}
	if got, want := compactCellsEncodedCapacity(cells), len(encoded); got != want {
		t.Fatalf("compact capacity estimate mismatch, got=%d want=%d", got, want)
	}
	if len(encoded) < 2 || encoded[0] != byte(compactCellsRunEncodingMarker) || encoded[1] != byte(compactCellsRunEncodingV1) {
		t.Fatalf("expected v1 run encoded cells, got %#v", encoded[:minInt(len(encoded), 2)])
	}
	if decoded := decodeCompactCells(encoded); !reflect.DeepEqual(decoded, cells) {
		t.Fatalf("run compact round trip changed cells:\nwant %#v\ngot  %#v", cells, decoded)
	}
}

func TestCompactCellsRunEncodingPreservesEmptySlice(t *testing.T) {
	decoded := decodeCompactCells(encodeCompactCells([]Cell{}))
	if decoded == nil || len(decoded) != 0 {
		t.Fatalf("empty compact cells should round-trip as empty slice, got %#v", decoded)
	}
}

func TestCompactLogicalLineDirectDenseEncodingMatchesCompactLineEncoding(t *testing.T) {
	line := LogicalLine{
		ID:                21,
		Generation:        5,
		CreatedGeneration: 5,
		ContentGeneration: 5,
		Seal:              SealStateSealed,
		Cells: []Cell{
			{Text: "plain", Width: 5},
			{Text: "styled", Width: 6, Style: CellStyle{FG: "idx:24", Bold: true}},
		},
		Residency: ResidencyMemory,
	}

	direct := encodeCompactLogicalLineFromLine(line)
	viaCompact := encodeCompactLogicalLine(compactLogicalLineFromLine(line))
	if !reflect.DeepEqual(direct, viaCompact) {
		t.Fatalf("direct dense compact encoding diverged from compact line encoding:\ndirect=%#v\nvia=%#v", direct, viaCompact)
	}
	loaded, ok := decodeCompactLine(line.ID, direct)
	if !ok {
		t.Fatal("expected direct encoded compact line to decode")
	}
	if !reflect.DeepEqual(loaded, line) {
		t.Fatalf("direct encoded line changed payload:\nwant %#v\ngot  %#v", line, loaded)
	}
}

func TestCompactLogicalLineHeaderOmitsDefaultMetadata(t *testing.T) {
	line := compactLogicalLine{
		ID:                11,
		Generation:        9,
		CreatedGeneration: 9,
		ContentGeneration: 9,
		Residency:         compactResidencyMemory,
		EncodedCells:      encodeCompactCells([]Cell{{Text: "payload", Width: 7}}),
	}

	encoded := encodeCompactLogicalLine(line)
	if got, want := cap(encoded), len(encoded); got != want {
		t.Fatalf("compact line should allocate exact backing capacity, cap=%d len=%d", got, want)
	}
	flags := compactLogicalLineHeaderFlags(line)
	if flags != 0 {
		t.Fatalf("default metadata should not set header flags, got %d", flags)
	}
	// 中文说明：默认 header 只保留 flags + generation，避免每条常规历史行重复存 created/content/residency/tail 标记。
	if got, want := len(encoded), 2+len(line.EncodedCells); got != want {
		t.Fatalf("default metadata header size changed, got len=%d want=%d", got, want)
	}

	loaded, ok := decodeCompactLine(line.ID, encoded)
	if !ok {
		t.Fatal("expected compact line to decode")
	}
	if loaded.Generation != 9 || loaded.CreatedGeneration != 9 || loaded.ContentGeneration != 9 || loaded.Residency != ResidencyMemory {
		t.Fatalf("default metadata round trip changed line: %#v", loaded)
	}
	if !reflect.DeepEqual(loaded.Cells, []Cell{{Text: "payload", Width: 7}}) {
		t.Fatalf("default metadata round trip changed cells: %#v", loaded.Cells)
	}
}

func TestCompactLogicalLineHeaderPreservesNonDefaultMetadata(t *testing.T) {
	line := compactLogicalLine{
		ID:                12,
		Generation:        9,
		CreatedGeneration: 7,
		ContentGeneration: 8,
		TailFill:          &RowTailFill{Style: CellStyle{BG: "ansi:4", Reverse: true}},
		Residency:         compactResidencyFile,
		EncodedCells:      encodeCompactCells([]Cell{{Text: "payload", Width: 7, Style: CellStyle{FG: "ansi:2"}}}),
	}

	encoded := encodeCompactLogicalLine(line)
	if got, want := cap(encoded), len(encoded); got != want {
		t.Fatalf("compact line should allocate exact backing capacity, cap=%d len=%d", got, want)
	}
	flags := compactLogicalLineHeaderFlags(line)
	wantFlags := compactLineHeaderCreatedDifferent | compactLineHeaderContentDifferent | compactLineHeaderResidencyDifferent | compactLineHeaderTailFill
	if flags != wantFlags {
		t.Fatalf("unexpected non-default metadata flags, got=%d want=%d", flags, wantFlags)
	}

	loaded, ok := decodeCompactLine(line.ID, encoded)
	if !ok {
		t.Fatal("expected compact line to decode")
	}
	want := LogicalLine{
		ID:                line.ID,
		Generation:        line.Generation,
		CreatedGeneration: line.CreatedGeneration,
		ContentGeneration: line.ContentGeneration,
		Seal:              SealStateSealed,
		Cells:             []Cell{{Text: "payload", Width: 7, Style: CellStyle{FG: "ansi:2"}}},
		TailFill:          &RowTailFill{Style: CellStyle{BG: "ansi:4", Reverse: true}},
		Residency:         ResidencyFile,
	}
	if !reflect.DeepEqual(loaded, want) {
		t.Fatalf("non-default metadata round trip changed line:\nwant %#v\ngot  %#v", want, loaded)
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

func TestCommittedHistoryIndexKeepsSequentialIDsAsRange(t *testing.T) {
	index := NewCommittedHistoryIndex()
	for id := LogicalLineID(1); id <= 1000; id++ {
		if err := index.Append(id); err != nil {
			t.Fatalf("append %d: %v", id, err)
		}
	}
	if got := index.Len(); got != 1000 {
		t.Fatalf("unexpected len %d", got)
	}
	if !index.Contains(1) || !index.Contains(1000) || index.Contains(1001) {
		t.Fatal("unexpected range membership")
	}
	if len(index.ids) != 0 || index.present != nil {
		t.Fatalf("sequential index should stay compact, ids=%d present=%#v", len(index.ids), index.present)
	}
	if !index.Remove(1) || !index.Contains(2) || index.Contains(1) {
		t.Fatal("range head remove did not keep membership")
	}
	if len(index.ids) != 0 {
		t.Fatalf("head remove should keep compact range, ids=%d", len(index.ids))
	}
	if !index.Remove(500) {
		t.Fatal("expected middle remove")
	}
	if len(index.ids) == 0 {
		t.Fatal("middle remove must materialize non-contiguous membership")
	}
	if index.Contains(500) {
		t.Fatal("expected removed middle id to be absent")
	}
}

func TestCompactFileSlotStaysPacked(t *testing.T) {
	if got := unsafe.Sizeof(compactFileSlot(0)); got != 8 {
		t.Fatalf("compact file slot must stay 8 bytes, got %d", got)
	}
	slot := makeCompactFileSlot(7, 11)
	if !slot.Present() || slot.Offset() != 7 || slot.Length() != 11 {
		t.Fatalf("unexpected packed slot %#v offset=%d length=%d", slot, slot.Offset(), slot.Length())
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
