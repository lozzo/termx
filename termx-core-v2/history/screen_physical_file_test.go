package history

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestR429FileScreenPhysicalRowBackendRangeAndRecovery(t *testing.T) {
	dir := t.TempDir()
	backend, err := NewFileScreenPhysicalRowBackend(dir, "term/r429")
	if err != nil {
		t.Fatalf("create screen physical backend: %v", err)
	}
	fileBackend := backend.(*fileScreenPhysicalRowBackend)
	if !strings.Contains(fileBackend.path, "term%2Fr429.screen-rows.bin") {
		t.Fatalf("screen physical backend must escape terminal id, path=%s", fileBackend.path)
	}
	rows := []PhysicalRow{
		screenPhysicalRowForTest(11, "one"),
		screenPhysicalRowForTest(12, "two"),
		screenPhysicalRowForTest(13, "three"),
	}
	if err := backend.AppendRows(rows); err != nil {
		t.Fatalf("append physical rows: %v", err)
	}
	if got := backend.RowCount(); got != 3 {
		t.Fatalf("backend row count mismatch: %d", got)
	}
	middle, err := backend.Rows(1, 3)
	if err != nil {
		t.Fatalf("read physical row range: %v", err)
	}
	if got := strings.Join(screenPhysicalRowTexts(middle), "|"); got != "two|three" {
		t.Fatalf("range should preserve append order and payload, got %q rows=%#v", got, middle)
	}

	reopened, err := NewFileScreenPhysicalRowBackend(dir, "term/r429")
	if err != nil {
		t.Fatalf("reopen screen physical backend: %v", err)
	}
	recovered, err := reopened.Recover()
	if err != nil {
		t.Fatalf("recover screen physical backend: %v", err)
	}
	if recovered.RowCount != 3 || recovered.NextRowID != 14 || recovered.AppliedSeq != 13 {
		t.Fatalf("recovered metadata mismatch: %#v", recovered)
	}
	head, err := reopened.Rows(0, 1)
	if err != nil {
		t.Fatalf("read recovered head: %v", err)
	}
	if got := strings.Join(screenPhysicalRowTexts(head), "|"); got != "one" {
		t.Fatalf("recovered backend should serve range without full materialization, got %q", got)
	}
	if _, err := NewFileScreenPhysicalRowBackend(filepath.Join(dir, "missing", "child"), "term-r429"); err != nil {
		t.Fatalf("nested backend directory should be created: %v", err)
	}
}

func screenPhysicalRowForTest(id uint64, text string) PhysicalRow {
	return PhysicalRow{
		ID:          id,
		Version:     1,
		OwnerSeq:    id,
		OwnerKind:   RowOwnerShellStream,
		Sealed:      true,
		ScreenCols:  40,
		SealSegment: HistorySegmentCommitted,
		Cells:       screenPhysicalCellsForTest(text),
	}
}

func screenPhysicalCellsForTest(text string) []Cell {
	cells := make([]Cell, 0, len([]rune(text)))
	for _, r := range text {
		cells = append(cells, Cell{Text: string(r), Width: 1})
	}
	return cells
}

func screenPhysicalRowTexts(rows []PhysicalRow) []string {
	texts := make([]string, 0, len(rows))
	for _, row := range rows {
		texts = append(texts, rowText(row.Cells))
	}
	return texts
}
