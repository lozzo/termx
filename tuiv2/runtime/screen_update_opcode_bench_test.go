package runtime

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lozzow/termx/protocol"
)

func BenchmarkScreenUpdateOpcodeScenarios(b *testing.B) {
	for _, scenario := range opcodeBenchScenarios() {
		for _, variant := range []struct {
			name   string
			update protocol.ScreenUpdate
		}{
			{name: "legacy", update: scenario.legacy},
			{name: "opcode", update: scenario.opcode},
		} {
			b.Run(fmt.Sprintf("%s/%s", scenario.name, variant.name), func(b *testing.B) {
				payload, err := protocol.EncodeScreenUpdatePayload(variant.update)
				if err != nil {
					b.Fatalf("encode payload: %v", err)
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					decoded, err := protocol.DecodeScreenUpdatePayload(payload)
					if err != nil {
						b.Fatalf("decode payload: %v", err)
					}
					next := applyScreenUpdateSnapshot(scenario.base, "term-1", decoded)
					if next == nil {
						b.Fatal("expected snapshot update result")
					}
				}
			})
		}
	}
}

func TestScreenUpdateOpcodeScenarioWireSizes(t *testing.T) {
	for _, scenario := range opcodeBenchScenarios() {
		legacyPayload, err := protocol.EncodeScreenUpdatePayload(scenario.legacy)
		if err != nil {
			t.Fatalf("%s legacy encode: %v", scenario.name, err)
		}
		opcodePayload, err := protocol.EncodeScreenUpdatePayload(scenario.opcode)
		if err != nil {
			t.Fatalf("%s opcode encode: %v", scenario.name, err)
		}
		t.Logf("%s legacy_bytes=%d opcode_bytes=%d", scenario.name, len(legacyPayload), len(opcodePayload))
	}
}

func opcodeBenchScenarios() []struct {
	name   string
	base   *protocol.Snapshot
	legacy protocol.ScreenUpdate
	opcode protocol.ScreenUpdate
} {
	return []struct {
		name   string
		base   *protocol.Snapshot
		legacy protocol.ScreenUpdate
		opcode protocol.ScreenUpdate
	}{
		{
			name: "less_scroll",
			base: opcodeBenchSnapshot("less", 80, 24),
			legacy: protocol.ScreenUpdate{
				Size:         protocol.Size{Cols: 80, Rows: 24},
				ScreenScroll: 1,
				ChangedRows: []protocol.ScreenRowUpdate{{
					Row:   23,
					Cells: opcodeBenchRow(80, "less-24"),
				}},
				Cursor: protocol.CursorState{Row: 23, Col: 0, Visible: true},
				Modes:  protocol.TerminalModes{AutoWrap: true},
			},
			opcode: protocol.ScreenUpdate{
				Size:         protocol.Size{Cols: 80, Rows: 24},
				ScreenScroll: 1,
				ChangedRows: []protocol.ScreenRowUpdate{{
					Row:   23,
					Cells: opcodeBenchRow(80, "less-24"),
				}},
				Ops: []protocol.ScreenOp{
					{Code: protocol.ScreenOpScrollRect, Rect: protocol.ScreenRect{X: 0, Y: 0, Width: 80, Height: 24}, Dy: -1},
					{Code: protocol.ScreenOpWriteSpan, Row: 23, Col: 0, Cells: opcodeBenchRow(80, "less-24")},
				},
				Cursor: protocol.CursorState{Row: 23, Col: 0, Visible: true},
				Modes:  protocol.TerminalModes{AutoWrap: true},
			},
		},
		{
			name: "vim_scroll_region",
			base: opcodeBenchSnapshot("vim", 120, 40),
			legacy: protocol.ScreenUpdate{
				Size: protocol.Size{Cols: 120, Rows: 40},
				ChangedRows: opcodeBenchChangedRowsFromLines(5, []string{
					benchLine("vim", 6, 120),
					benchLine("vim", 7, 120),
					benchLine("vim", 8, 120),
					benchLine("vim", 9, 120),
					benchLine("vim", 10, 120),
					benchLine("vim", 11, 120),
					benchLine("vim", 12, 120),
					benchLine("vim", 13, 120),
					benchLine("vim", 14, 120),
					benchLine("vim", 15, 120),
					benchLine("vim", 16, 120),
					benchLine("vim", 17, 120),
					benchLine("vim", 18, 120),
					benchLine("vim", 19, 120),
					benchLine("vim", 20, 120),
					benchLine("vim", 21, 120),
					benchLine("vim", 22, 120),
					benchLine("vim", 23, 120),
					benchLine("vim", 24, 120),
					benchLine("vim", 25, 120),
					benchLine("vim", 26, 120),
				}),
				Cursor: protocol.CursorState{Row: 25, Col: 0, Visible: true},
				Modes:  protocol.TerminalModes{AlternateScreen: true, AutoWrap: true},
			},
			opcode: protocol.ScreenUpdate{
				Size: protocol.Size{Cols: 120, Rows: 40},
				ChangedRows: opcodeBenchChangedRowsFromLines(5, []string{
					benchLine("vim", 6, 120),
					benchLine("vim", 7, 120),
					benchLine("vim", 8, 120),
					benchLine("vim", 9, 120),
					benchLine("vim", 10, 120),
					benchLine("vim", 11, 120),
					benchLine("vim", 12, 120),
					benchLine("vim", 13, 120),
					benchLine("vim", 14, 120),
					benchLine("vim", 15, 120),
					benchLine("vim", 16, 120),
					benchLine("vim", 17, 120),
					benchLine("vim", 18, 120),
					benchLine("vim", 19, 120),
					benchLine("vim", 20, 120),
					benchLine("vim", 21, 120),
					benchLine("vim", 22, 120),
					benchLine("vim", 23, 120),
					benchLine("vim", 24, 120),
					benchLine("vim", 25, 120),
					benchLine("vim", 26, 120),
				}),
				Ops: []protocol.ScreenOp{
					{Code: protocol.ScreenOpScrollRect, Rect: protocol.ScreenRect{X: 0, Y: 5, Width: 120, Height: 21}, Dy: -1},
					{Code: protocol.ScreenOpWriteSpan, Row: 25, Col: 0, Cells: opcodeBenchRow(120, benchLine("vim", 26, 120))},
				},
				Cursor: protocol.CursorState{Row: 25, Col: 0, Visible: true},
				Modes:  protocol.TerminalModes{AlternateScreen: true, AutoWrap: true},
			},
		},
		{
			name: "top_scroll",
			base: opcodeBenchSnapshot("top", 80, 24),
			legacy: protocol.ScreenUpdate{
				Size:         protocol.Size{Cols: 80, Rows: 24},
				ScreenScroll: 1,
				ChangedRows: []protocol.ScreenRowUpdate{
					{Row: 0, Cells: opcodeBenchRow(80, "top header load=0.42 users=2")},
					{Row: 1, Cells: opcodeBenchRow(80, "tasks 97 total 1 running")},
					{Row: 23, Cells: opcodeBenchRow(80, "proc-24 cpu=4.2 mem=128m")},
				},
				Cursor: protocol.CursorState{Row: 23, Col: 0, Visible: true},
				Modes:  protocol.TerminalModes{AutoWrap: true},
			},
			opcode: protocol.ScreenUpdate{
				Size:         protocol.Size{Cols: 80, Rows: 24},
				ScreenScroll: 1,
				ChangedRows: []protocol.ScreenRowUpdate{
					{Row: 0, Cells: opcodeBenchRow(80, "top header load=0.42 users=2")},
					{Row: 1, Cells: opcodeBenchRow(80, "tasks 97 total 1 running")},
					{Row: 23, Cells: opcodeBenchRow(80, "proc-24 cpu=4.2 mem=128m")},
				},
				Ops: []protocol.ScreenOp{
					{Code: protocol.ScreenOpScrollRect, Rect: protocol.ScreenRect{X: 0, Y: 0, Width: 80, Height: 24}, Dy: -1},
					{Code: protocol.ScreenOpWriteSpan, Row: 0, Col: 0, Cells: opcodeBenchRow(80, "top header load=0.42 users=2")},
					{Code: protocol.ScreenOpWriteSpan, Row: 1, Col: 0, Cells: opcodeBenchRow(80, "tasks 97 total 1 running")},
					{Code: protocol.ScreenOpWriteSpan, Row: 23, Col: 0, Cells: opcodeBenchRow(80, "proc-24 cpu=4.2 mem=128m")},
				},
				Cursor: protocol.CursorState{Row: 23, Col: 0, Visible: true},
				Modes:  protocol.TerminalModes{AutoWrap: true},
			},
		},
		{
			name: "block_move",
			base: opcodeBenchSnapshot("move", 120, 40),
			legacy: protocol.ScreenUpdate{
				Size: protocol.Size{Cols: 120, Rows: 40},
				ChangedRows: opcodeBenchChangedRowsFromLines(20, []string{
					benchLine("move", 5, 120),
					benchLine("move", 6, 120),
					benchLine("move", 7, 120),
					benchLine("move", 8, 120),
					benchLine("move", 9, 120),
					benchLine("move", 10, 120),
					benchLine("move", 11, 120),
					benchLine("move", 12, 120),
					benchLine("move", 13, 120),
					benchLine("move", 14, 120),
				}),
				Cursor: protocol.CursorState{Row: 20, Col: 0, Visible: true},
				Modes:  protocol.TerminalModes{AlternateScreen: true, AutoWrap: true},
			},
			opcode: protocol.ScreenUpdate{
				Size: protocol.Size{Cols: 120, Rows: 40},
				ChangedRows: opcodeBenchChangedRowsFromLines(20, []string{
					benchLine("move", 5, 120),
					benchLine("move", 6, 120),
					benchLine("move", 7, 120),
					benchLine("move", 8, 120),
					benchLine("move", 9, 120),
					benchLine("move", 10, 120),
					benchLine("move", 11, 120),
					benchLine("move", 12, 120),
					benchLine("move", 13, 120),
					benchLine("move", 14, 120),
				}),
				Ops: []protocol.ScreenOp{
					{Code: protocol.ScreenOpCopyRect, Src: protocol.ScreenRect{X: 0, Y: 5, Width: 120, Height: 10}, DstX: 0, DstY: 20},
				},
				Cursor: protocol.CursorState{Row: 20, Col: 0, Visible: true},
				Modes:  protocol.TerminalModes{AlternateScreen: true, AutoWrap: true},
			},
		},
		{
			name: "sparse_point",
			base: opcodeBenchSnapshot("seed", 120, 40),
			legacy: protocol.ScreenUpdate{
				Size: protocol.Size{Cols: 120, Rows: 40},
				ChangedSpans: []protocol.ScreenSpanUpdate{{
					Row:      12,
					ColStart: 37,
					Cells:    []protocol.Cell{{Content: "X", Width: 1}},
					Op:       protocol.ScreenSpanOpWrite,
				}},
				Cursor: protocol.CursorState{Row: 12, Col: 38, Visible: true},
				Modes:  protocol.TerminalModes{AutoWrap: true},
			},
			opcode: protocol.ScreenUpdate{
				Size: protocol.Size{Cols: 120, Rows: 40},
				ChangedSpans: []protocol.ScreenSpanUpdate{{
					Row:      12,
					ColStart: 37,
					Cells:    []protocol.Cell{{Content: "X", Width: 1}},
					Op:       protocol.ScreenSpanOpWrite,
				}},
				Ops: []protocol.ScreenOp{
					{Code: protocol.ScreenOpWriteSpan, Row: 12, Col: 37, Cells: []protocol.Cell{{Content: "X", Width: 1}}},
				},
				Cursor: protocol.CursorState{Row: 12, Col: 38, Visible: true},
				Modes:  protocol.TerminalModes{AutoWrap: true},
			},
		},
	}
}

func opcodeBenchSnapshot(prefix string, cols, rows int) *protocol.Snapshot {
	lines := make([]string, rows)
	for row := 0; row < rows; row++ {
		lines[row] = benchLine(prefix, row, cols)
	}
	return snapshotWithLines("term-1", uint16(cols), uint16(rows), lines)
}

func opcodeBenchRow(cols int, text string) []protocol.Cell {
	row := make([]protocol.Cell, cols)
	for col := 0; col < cols; col++ {
		row[col] = protocol.Cell{Content: " ", Width: 1}
	}
	for col := 0; col < len(text) && col < cols; col++ {
		row[col] = protocol.Cell{Content: string(text[col]), Width: 1}
	}
	return row
}

func opcodeBenchChangedRowsFromLines(startRow int, lines []string) []protocol.ScreenRowUpdate {
	rows := make([]protocol.ScreenRowUpdate, 0, len(lines))
	for i, line := range lines {
		rows = append(rows, protocol.ScreenRowUpdate{
			Row:   startRow + i,
			Cells: opcodeBenchRow(len(line), line),
		})
	}
	return rows
}

func benchLine(prefix string, row, cols int) string {
	base := fmt.Sprintf("%s-%02d ", prefix, row)
	if len(base) >= cols {
		return base[:cols]
	}
	return base + strings.Repeat(".", cols-len(base))
}
