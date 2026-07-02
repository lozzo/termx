package history

import (
	"testing"

	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestR421ScreenBufferCursorMovementAndEraseControls(t *testing.T) {
	buffer := newR421FilledScreenBuffer(12, "abcdefghijkl", "klmnopqrstuv", "uvwxyz")

	if err := buffer.ApplyTransaction(TerminalSemanticTransaction{
		Seq: 2,
		Ops: []TerminalSemanticOp{
			screenBufferControlAt("cup", 2, 5, 0),
			screenBufferControlAt("cuu", 0, 0, 1),
			screenBufferControlAt("cud", 0, 0, 1),
			screenBufferControlAt("cub", 0, 0, 2),
			screenBufferControlAt("cuf", 0, 0, 4),
			screenBufferControlAt("cha", 0, 1, 0),
			screenBufferControlAt("hpa", 0, 4, 0),
			screenBufferControlAt("vpa", 0, 0, 0),
			screenBufferControlAt("hvp", 1, 2, 0),
		},
	}); err != nil {
		t.Fatalf("apply cursor controls: %v", err)
	}
	if buffer.Cursor.Y != 1 || buffer.Cursor.X != 2 {
		t.Fatalf("unexpected cursor after movement controls: %#v", buffer.Cursor)
	}

	before := buffer.VisibleRows()[1]
	if err := buffer.ApplyTransaction(TerminalSemanticTransaction{
		Seq: 3,
		Ops: []TerminalSemanticOp{
			screenBufferControlAt("el", 1, 3, 0),
			screenBufferControlAt("ed", 1, 1, 0),
		},
	}); err != nil {
		t.Fatalf("apply erase controls: %v", err)
	}
	rows := buffer.VisibleRows()
	if rows[1].ID != before.ID {
		t.Fatalf("EL/ED must mutate physical row in place, before=%#v after=%#v", before, rows[1])
	}
	if rows[1].Version <= before.Version {
		t.Fatalf("EL/ED must advance row version, before=%#v after=%#v", before, rows[1])
	}
	if got := rows[1].Text(); got != "k" {
		t.Fatalf("unexpected ED0 row text %q row=%#v", got, rows[1])
	}
	if got := rows[2].Text(); got != "" {
		t.Fatalf("ED0 must clear rows below cursor, got row2=%q row=%#v", got, rows[2])
	}
	if len(buffer.CommittedRows()) != 0 {
		t.Fatalf("cursor/erase controls must not seal history by themselves, committed=%#v", buffer.CommittedRows())
	}
}

func TestR421ScreenBufferCharacterEditsPreserveRowIDAndWideCells(t *testing.T) {
	buffer := NewScreenHistoryBuffer(16, 3)
	wide := []TerminalSemanticCell{
		{Content: "A", Width: 1},
		{Content: "中", Width: 2},
		{Content: "", Width: 0},
		{Content: "B", Width: 1},
		{Content: "C", Width: 1},
	}
	if err := buffer.ApplyTransaction(TerminalSemanticTransaction{
		Seq: 1,
		Ops: []TerminalSemanticOp{
			screenBufferWriteCellsOp(0, 0, wide),
			screenBufferWriteCellsOp(1, 0, wide),
			screenBufferWriteOp(2, 0, "ABC"),
		},
	}); err != nil {
		t.Fatalf("apply wide rows: %v", err)
	}
	before := buffer.VisibleRows()
	if err := buffer.ApplyTransaction(TerminalSemanticTransaction{
		Seq: 2,
		Ops: []TerminalSemanticOp{
			screenBufferControlAt("ech", 0, 1, 2),
			screenBufferControlAt("dch", 1, 1, 2),
			screenBufferControlAt("ich", 2, 1, 2),
		},
	}); err != nil {
		t.Fatalf("apply character edits: %v", err)
	}
	rows := buffer.VisibleRows()
	if rows[0].ID != before[0].ID || rows[1].ID != before[1].ID || rows[2].ID != before[2].ID {
		t.Fatalf("character edits must preserve physical RowID, before=%#v after=%#v", before, rows)
	}
	if got := rows[0].Text(); got != "A  BC" {
		t.Fatalf("ECH must blank the wide-cell display columns without continuation text, got %q row=%#v", got, rows[0])
	}
	if got := rows[1].Text(); got != "ABC" {
		t.Fatalf("DCH must delete the wide cell as one screen cell span, got %q row=%#v", got, rows[1])
	}
	if got := rows[2].Text(); got != "A  BC" {
		t.Fatalf("ICH must insert blank display columns, got %q row=%#v", got, rows[2])
	}
}

func TestR421ScreenBufferLineOperationsMoveRowIdentity(t *testing.T) {
	tests := []struct {
		name      string
		op        TerminalSemanticOp
		wantTexts []string
		wantIDs   func([]PhysicalRow) []uint64
		sealsTop  bool
	}{
		{
			name:      "insert line shifts rows down without sealing",
			op:        screenBufferControlWithBottom("il", 1, 0, 1, 3),
			wantTexts: []string{"top", "", "mid"},
			wantIDs: func(before []PhysicalRow) []uint64 {
				return []uint64{before[0].ID, 0, before[1].ID}
			},
		},
		{
			name:      "delete line shifts rows up without sealing",
			op:        screenBufferControlWithBottom("dl", 1, 0, 1, 3),
			wantTexts: []string{"top", "bot", ""},
			wantIDs: func(before []PhysicalRow) []uint64 {
				return []uint64{before[0].ID, before[2].ID, 0}
			},
		},
		{
			name:      "scroll down shifts rows down without sealing",
			op:        screenBufferControlWithBottom("sd", 0, 0, 1, 3),
			wantTexts: []string{"", "top", "mid"},
			wantIDs: func(before []PhysicalRow) []uint64 {
				return []uint64{0, before[0].ID, before[1].ID}
			},
		},
		{
			name:      "scroll up full screen seals top row once",
			op:        screenBufferControlWithBottom("su", 0, 0, 1, 3),
			wantTexts: []string{"mid", "bot", ""},
			wantIDs: func(before []PhysicalRow) []uint64 {
				return []uint64{before[1].ID, before[2].ID, 0}
			},
			sealsTop: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buffer := newR421FilledScreenBuffer(12, "top", "mid", "bot")
			before := buffer.VisibleRows()
			if err := buffer.ApplyTransaction(TerminalSemanticTransaction{Seq: 2, Ops: []TerminalSemanticOp{tc.op}}); err != nil {
				t.Fatalf("apply line op: %v", err)
			}
			rows := buffer.VisibleRows()
			if got := screenBufferPhysicalTexts(rows); !equalStringSlices(got, tc.wantTexts) {
				t.Fatalf("unexpected row text after %s, got %v want %v rows=%#v", tc.name, got, tc.wantTexts, rows)
			}
			wantIDs := tc.wantIDs(before)
			for index, wantID := range wantIDs {
				if wantID == 0 {
					if rows[index].ID == before[0].ID || rows[index].ID == before[1].ID || rows[index].ID == before[2].ID {
						t.Fatalf("row %d should be a fresh physical row, rows=%#v before=%#v", index, rows, before)
					}
					continue
				}
				if rows[index].ID != wantID {
					t.Fatalf("row %d should preserve moved RowID %d, got %#v before=%#v", index, wantID, rows[index], before)
				}
			}
			committed := buffer.CommittedRows()
			if tc.sealsTop {
				if len(committed) != 1 || committed[0].ID != before[0].ID || committed[0].Text() != "top" {
					t.Fatalf("full-screen SU must seal original top RowID once, committed=%#v before=%#v", committed, before)
				}
			} else if len(committed) != 0 {
				t.Fatalf("%s must not seal primary history, committed=%#v", tc.name, committed)
			}
		})
	}
}

func TestR421ScreenBufferScrollRegionDoesNotSealLocalScroll(t *testing.T) {
	buffer := newR421FilledScreenBuffer(12, "keep", "r1", "r2", "r3")
	before := buffer.VisibleRows()
	if err := buffer.ApplyTransaction(TerminalSemanticTransaction{
		Seq: 2,
		Ops: []TerminalSemanticOp{
			screenBufferControlWithBottom("decstbm", 0, 0, 2, 4),
			screenBufferControlWithBottom("su", 1, 0, 1, 4),
		},
	}); err != nil {
		t.Fatalf("apply scroll region op: %v", err)
	}
	rows := buffer.VisibleRows()
	if buffer.Margins.Top != 1 || buffer.Margins.Bottom != 4 {
		t.Fatalf("unexpected margins: %#v", buffer.Margins)
	}
	if rows[0].ID != before[0].ID || rows[0].Text() != "keep" {
		t.Fatalf("scroll region must not touch row above margin, before=%#v after=%#v", before[0], rows[0])
	}
	if rows[1].ID != before[2].ID || rows[1].Text() != "r2" {
		t.Fatalf("row 2 should move into scroll-region top, rows=%#v before=%#v", rows, before)
	}
	if len(buffer.CommittedRows()) != 0 {
		t.Fatalf("local scroll region must not seal primary history, committed=%#v", buffer.CommittedRows())
	}
}

func TestR421ScreenBufferAltScreenAndSyncDoNotSealPrimary(t *testing.T) {
	buffer := NewScreenHistoryBuffer(12, 3)
	if err := buffer.ApplyTransaction(TerminalSemanticTransaction{
		Seq: 1,
		Ops: []TerminalSemanticOp{
			screenBufferWriteOp(0, 0, "primary"),
			screenBufferModeOp(1048, true),
			screenBufferControlAt("cup", 2, 5, 0),
			screenBufferModeOp(1048, false),
			screenBufferModeOp(1049, true),
			screenBufferWriteOp(0, 0, "alt"),
			screenBufferModeOp(2026, true),
			screenBufferModeOp(2026, false),
			screenBufferModeOp(1049, false),
		},
	}); err != nil {
		t.Fatalf("apply alt/sync sequence: %v", err)
	}
	if buffer.Cursor.Y != 0 || buffer.Cursor.X != len("primary") {
		t.Fatalf("1048/1049 save-restore should restore primary cursor, cursor=%#v", buffer.Cursor)
	}
	if err := buffer.ApplyTransaction(TerminalSemanticTransaction{
		Seq: 2,
		Ops: []TerminalSemanticOp{
			screenBufferModeOp(1047, true),
			screenBufferWriteOp(0, 0, "tmp"),
			screenBufferModeOp(1047, false),
		},
	}); err != nil {
		t.Fatalf("apply 1047 alt sequence: %v", err)
	}
	if buffer.InAlt {
		t.Fatalf("buffer should leave alt-screen after mode reset")
	}
	if got := buffer.VisibleRows()[0].Text(); got != "primary" {
		t.Fatalf("alt-screen writes must not mutate primary screen, got %q rows=%#v", got, buffer.VisibleRows())
	}
	if got := buffer.Alt.Rows[0].Text(); got != "tmp" {
		t.Fatalf("alt grid should hold transient alt content, got %q row=%#v", got, buffer.Alt.Rows[0])
	}
	if len(buffer.CommittedRows()) != 0 {
		t.Fatalf("alt/sync modes must not seal primary history, committed=%#v", buffer.CommittedRows())
	}
}

func TestR421ScreenBufferED2ScrollOutProofSealsRowsOnce(t *testing.T) {
	buffer := newR421FilledScreenBuffer(12, "one", "two")
	before := buffer.VisibleRows()
	tx := TerminalSemanticTransaction{
		Seq: 2,
		Ops: []TerminalSemanticOp{{
			Code:    vterm.ScreenOpControl,
			Control: "ed",
			Mode:    2,
			ScrollOut: []vterm.ScrollbackRowAppend{
				{Row: 0, RowSet: true},
				{Row: 1, RowSet: true},
			},
		}},
	}
	if err := buffer.ApplyTransaction(tx); err != nil {
		t.Fatalf("apply ED2 proof: %v", err)
	}
	if err := buffer.ApplyTransaction(tx); err != nil {
		t.Fatalf("duplicate seq must be ignored instead of sealing rows twice: %v", err)
	}
	committed := buffer.CommittedRows()
	if len(committed) != 2 {
		t.Fatalf("ED2 proof should seal two physical rows once, committed=%#v", committed)
	}
	for index, row := range committed {
		if row.ID != before[index].ID || row.Text() != before[index].Text() || !row.Sealed {
			t.Fatalf("committed row %d must preserve original RowID/text, committed=%#v before=%#v", index, committed, before)
		}
	}
	for _, row := range buffer.VisibleRows() {
		if row.ID == before[0].ID || row.ID == before[1].ID {
			t.Fatalf("current screen must not retain a sealed RowID, visible=%#v before=%#v", buffer.VisibleRows(), before)
		}
	}
}

func newR421FilledScreenBuffer(cols int, rows ...string) *ScreenHistoryBuffer {
	buffer := NewScreenHistoryBuffer(cols, len(rows))
	ops := make([]TerminalSemanticOp, 0, len(rows))
	for row, text := range rows {
		ops = append(ops, screenBufferWriteOp(row, 0, text))
	}
	if err := buffer.ApplyTransaction(TerminalSemanticTransaction{Seq: 1, Ops: ops}); err != nil {
		panic(err)
	}
	return buffer
}

func screenBufferControlAt(kind string, row int, col int, mode int) TerminalSemanticOp {
	return TerminalSemanticOp{Code: vterm.ScreenOpControl, Control: kind, Row: row, Col: col, RowSet: true, Mode: mode}
}

func screenBufferControlWithBottom(kind string, row int, col int, mode int, bottom int) TerminalSemanticOp {
	op := screenBufferControlAt(kind, row, col, mode)
	op.Bottom = bottom
	return op
}

func screenBufferModeOp(mode int, enabled bool) TerminalSemanticOp {
	return TerminalSemanticOp{Code: vterm.ScreenOpModes, Private: true, Mode: mode, Enabled: enabled}
}

func screenBufferWriteCellsOp(row int, col int, cells []TerminalSemanticCell) TerminalSemanticOp {
	return TerminalSemanticOp{Code: vterm.ScreenOpWriteSpan, Row: row, Col: col, Cells: cells}
}

func screenBufferPhysicalTexts(rows []PhysicalRow) []string {
	out := make([]string, len(rows))
	for index, row := range rows {
		out[index] = row.Text()
	}
	return out
}

func equalStringSlices(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
