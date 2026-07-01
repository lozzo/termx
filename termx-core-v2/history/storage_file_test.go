package history

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestR341FileStorageBackendServesWindowPayloads(t *testing.T) {
	backend, err := NewFileStorageBackend(t.TempDir(), "term-r341-file")
	if err != nil {
		t.Fatalf("create file backend: %v", err)
	}
	store := NewBackendHistoryStore("term-r341-file", backend)
	for i := 1; i <= 20; i++ {
		applyStoreBatch(t, store, sealedLineMutations(LogicalLineID(i), HistoryRecordID(i), "file-line"))
	}
	raw := store.(*inMemoryHistoryStore)
	if got := len(raw.lines); got != 0 {
		t.Fatalf("file-backed store must spill sealed payloads out of hot map, got %d", got)
	}
	window, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-r341-file", Limit: 3, Cols: 80})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if got := strings.Join(rowTexts(window.Rows), "|"); got != "file-line|file-line|file-line" {
		t.Fatalf("file backend should load payloads for window, got %q", got)
	}
}

func TestR342FileStorageBackendDoesNotUseJSON(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "storage_file.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse storage file: %v", err)
	}
	for _, spec := range file.Imports {
		if spec.Path.Value == `"encoding/json"` {
			t.Fatalf("history file backend must stay binary; encoding/json import is not allowed")
		}
	}
	backend, err := NewFileStorageBackend(t.TempDir(), "term-r342-binary")
	if err != nil {
		t.Fatalf("create file backend: %v", err)
	}
	fileBackend := backend.(*fileStorageBackend)
	if strings.HasSuffix(fileBackend.path, ".jsonl") {
		t.Fatalf("history file backend must not use JSONL path: %s", fileBackend.path)
	}
}

func TestR346FileStorageBackendEscapesTerminalIDForPath(t *testing.T) {
	backend, err := NewFileStorageBackend(t.TempDir(), "term/with/slash")
	if err != nil {
		t.Fatalf("create file backend: %v", err)
	}
	fileBackend := backend.(*fileStorageBackend)
	if strings.Contains(fileBackend.path, "term/with/slash.history-lines.bin") {
		t.Fatalf("terminal id must not create nested history payload path: %s", fileBackend.path)
	}
	if !strings.Contains(fileBackend.path, "term%2Fwith%2Fslash.history-lines.bin") {
		t.Fatalf("history payload file should use escaped terminal id, got %s", fileBackend.path)
	}
}

func TestR407FileStorageBackendEncodesRunsWithoutTemporaryTextConcat(t *testing.T) {
	if _, err := parser.ParseFile(token.NewFileSet(), "storage_file.go", nil, 0); err != nil {
		t.Fatalf("parse storage source: %v", err)
	}
	text := sourceTextForGuard(t, "storage_file.go")
	for _, forbidden := range []string{"compactCellRuns", "current.Text +=", "Text +="} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("file backend must stream cell runs without temporary run text concatenation: %s", forbidden)
		}
	}
	if !strings.Contains(text, "writeCellRuns") || !strings.Contains(text, "writeCellRunText") {
		t.Fatal("file backend should encode cell runs directly into the binary payload")
	}
}

func TestR407BinaryLogicalLineRoundTripsMixedRuns(t *testing.T) {
	line := LogicalLine{
		ID:                42,
		Generation:        7,
		CreatedGeneration: 3,
		ContentGeneration: 6,
		Seal:              SealStateSealed,
		Kind:              string(LineKindOrdinary),
		ScreenCols:        120,
		Residency:         ResidencyFile,
		TailFill:          &RowTailFill{Style: CellStyle{BG: "default", Reverse: true}},
		Cells: []Cell{
			{Text: "a", Width: 1, Style: CellStyle{FG: "red"}},
			{Text: "b", Width: 1, Style: CellStyle{FG: "red"}},
			{Text: "你", Width: 2, Style: CellStyle{FG: "red"}},
			{Text: "c", Width: 1, Style: CellStyle{FG: "blue", Bold: true}, LinkURL: "https://example.test", LinkParams: "id=1"},
			{Text: "d", Width: 1, Style: CellStyle{FG: "blue", Bold: true}, LinkURL: "https://example.test", LinkParams: "id=1"},
		},
	}
	payload, err := encodeBinaryLogicalLine(line)
	if err != nil {
		t.Fatalf("encode logical line: %v", err)
	}
	got, err := decodeBinaryLogicalLine(payload)
	if err != nil {
		t.Fatalf("decode logical line: %v", err)
	}
	if got.ID != line.ID || got.Generation != line.Generation || got.Kind != line.Kind || got.ScreenCols != line.ScreenCols {
		t.Fatalf("decoded metadata mismatch: %#v", got)
	}
	if got.TailFill == nil || got.TailFill.Style != line.TailFill.Style {
		t.Fatalf("decoded tail fill mismatch: %#v", got.TailFill)
	}
	if len(got.Cells) != len(line.Cells) {
		t.Fatalf("decoded cell count mismatch: got %d want %d", len(got.Cells), len(line.Cells))
	}
	for index := range line.Cells {
		if got.Cells[index] != line.Cells[index] {
			t.Fatalf("decoded cell %d mismatch: got %#v want %#v", index, got.Cells[index], line.Cells[index])
		}
	}
	if countCellRuns(line.Cells) != 3 {
		t.Fatalf("expected width/style/link run split to produce 3 runs, got %d", countCellRuns(line.Cells))
	}
	if got := estimatedBinaryLogicalLinePayloadSize(line); got != len(payload) {
		t.Fatalf("estimated payload length must match encoded bytes for record offset accounting, got %d want %d", got, len(payload))
	}
}

func TestR407BinaryLogicalLineRoundTripsCompactRuns(t *testing.T) {
	line := LogicalLine{
		ID:        77,
		Seal:      SealStateSealed,
		Kind:      string(LineKindOrdinary),
		Residency: ResidencyFile,
		Runs: []CellRun{
			{Text: "alpha beta ", Style: CellStyle{FG: "ansi:2"}},
			{Text: "gamma", Style: CellStyle{FG: "ansi:3", Bold: true}},
		},
	}
	payload, err := encodeBinaryLogicalLine(line)
	if err != nil {
		t.Fatalf("encode compact run logical line: %v", err)
	}
	got, err := decodeBinaryLogicalLine(payload)
	if err != nil {
		t.Fatalf("decode compact run logical line: %v", err)
	}
	if len(got.Cells) != 0 || len(got.Runs) != len(line.Runs) {
		t.Fatalf("compact run payload should round-trip without cell expansion, got cells=%d runs=%#v", len(got.Cells), got.Runs)
	}
	if got.Runs[0] != line.Runs[0] || got.Runs[1] != line.Runs[1] {
		t.Fatalf("compact runs mismatch: got %#v want %#v", got.Runs, line.Runs)
	}
	if gotText := rowText(lineCellsForProjection(got)); gotText != "alpha beta gamma" {
		t.Fatalf("window projection should expand compact runs on demand, got %q", gotText)
	}
}

func TestR407FileStorageBackendStreamsRecordsWithCorrectOffsets(t *testing.T) {
	backend, err := NewFileStorageBackend(t.TempDir(), "term-r407-stream")
	if err != nil {
		t.Fatalf("create file backend: %v", err)
	}
	first := LogicalLine{
		ID:        1,
		Seal:      SealStateSealed,
		Kind:      string(LineKindOrdinary),
		Residency: ResidencyFile,
		Cells:     []Cell{{Text: "a", Width: 1}, {Text: "b", Width: 1}},
	}
	second := LogicalLine{
		ID:        2,
		Seal:      SealStateSealed,
		Kind:      string(LineKindOrdinary),
		Residency: ResidencyFile,
		Cells: []Cell{
			{Text: "你", Width: 2, Style: CellStyle{FG: "ansi:2"}},
			{Text: "c", Width: 1, Style: CellStyle{FG: "ansi:3"}},
		},
	}
	if err := backend.Apply(StorageTransaction{Lines: []LogicalLine{first, second}}); err != nil {
		t.Fatalf("apply streamed file records: %v", err)
	}
	fileBackend := backend.(*fileStorageBackend)
	if fileBackend.offsets[2] != int64(28+estimatedBinaryLogicalLinePayloadSize(first)) {
		t.Fatalf("second record offset should account for streamed first payload, offsets=%#v", fileBackend.offsets)
	}
	gotFirst, ok := fileBackend.GetLine(1)
	if !ok {
		t.Fatal("expected first streamed record to be readable")
	}
	gotSecond, ok := fileBackend.GetLine(2)
	if !ok {
		t.Fatal("expected second streamed record to be readable")
	}
	if len(gotFirst.Cells) != 2 || gotFirst.Cells[0].Text != "a" || len(gotSecond.Cells) != 2 || gotSecond.Cells[0].Text != "你" || gotSecond.Cells[1].Style.FG != "ansi:3" {
		t.Fatalf("streamed records decoded incorrectly: first=%#v second=%#v", gotFirst.Cells, gotSecond.Cells)
	}
}

func sourceTextForGuard(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read source guard file: %v", err)
	}
	return string(data)
}
