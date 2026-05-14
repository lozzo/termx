package runtime

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core/protocol"
	localvterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func BenchmarkScreenUpdateOpcodeScenarios(b *testing.B) {
	for _, scenario := range opcodeBenchScenarios() {
		for _, variant := range []struct {
			name   string
			update protocol.ScreenUpdate
		}{
			{name: "ops", update: scenario.update},
			{name: "full_replace", update: opcodeBenchFullReplaceUpdate(scenario.base, scenario.update)},
		} {
			payload, err := protocol.EncodeScreenUpdatePayload(variant.update)
			if err != nil {
				b.Fatalf("%s/%s encode payload: %v", scenario.name, variant.name, err)
			}
			b.Run(fmt.Sprintf("%s/%s/decode_only", scenario.name, variant.name), func(b *testing.B) {
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					decoded, err := protocol.DecodeScreenUpdatePayload(payload)
					if err != nil {
						b.Fatalf("decode payload: %v", err)
					}
					if decoded.Size != variant.update.Size {
						b.Fatalf("unexpected decoded size: %#v", decoded.Size)
					}
				}
			})
			b.Run(fmt.Sprintf("%s/%s/decode_snapshot_apply", scenario.name, variant.name), func(b *testing.B) {
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
			if variant.update.FullReplace {
				b.Run(fmt.Sprintf("%s/%s/decode_contract_full", scenario.name, variant.name), func(b *testing.B) {
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						b.StopTimer()
						vt := opcodeBenchVTerm(scenario.base)
						b.StartTimer()
						decoded, err := protocol.DecodeScreenUpdatePayload(payload)
						if err != nil {
							b.Fatalf("decode payload: %v", err)
						}
						next := applyScreenUpdateSnapshot(scenario.base, "term-1", decoded)
						if next == nil {
							b.Fatal("expected snapshot update result")
						}
						loadSnapshotIntoVTerm(vt, next)
					}
				})
				continue
			}
			b.Run(fmt.Sprintf("%s/%s/decode_contract_partial", scenario.name, variant.name), func(b *testing.B) {
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					vt := opcodeBenchVTerm(scenario.base)
					applier, ok := any(vt).(screenUpdateApplier)
					if !ok {
						b.Fatal("expected vterm screen update applier")
					}
					b.StartTimer()
					decoded, err := protocol.DecodeScreenUpdatePayload(payload)
					if err != nil {
						b.Fatalf("decode payload: %v", err)
					}
					next := applyScreenUpdateSnapshot(scenario.base, "term-1", decoded)
					if next == nil {
						b.Fatal("expected snapshot update result")
					}
					if !applier.ApplyScreenUpdate(vtermScreenUpdateFromProtocol(decoded)) {
						b.Fatal("expected partial apply success")
					}
				}
			})
		}
	}
}

func TestScreenUpdateOpcodeScenarioWireSizes(t *testing.T) {
	for _, scenario := range opcodeBenchScenarios() {
		opcodePayload, err := protocol.EncodeScreenUpdatePayload(scenario.update)
		if err != nil {
			t.Fatalf("%s opcode encode: %v", scenario.name, err)
		}
		fullPayload, err := protocol.EncodeScreenUpdatePayload(opcodeBenchFullReplaceUpdate(scenario.base, scenario.update))
		if err != nil {
			t.Fatalf("%s full replace encode: %v", scenario.name, err)
		}
		t.Logf("%s ops_bytes=%d full_replace_bytes=%d", scenario.name, len(opcodePayload), len(fullPayload))
	}
}

func opcodeBenchScenarios() []struct {
	name   string
	base   *protocol.Snapshot
	update protocol.ScreenUpdate
} {
	return []struct {
		name   string
		base   *protocol.Snapshot
		update protocol.ScreenUpdate
	}{
		{
			name: "less_scroll",
			base: opcodeBenchSnapshot("less", 80, 24),
			update: protocol.ScreenUpdate{
				Size:         protocol.Size{Cols: 80, Rows: 24},
				ScreenScroll: 1,
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
			base: opcodeBenchSnapshotWithModes("vim", 120, 40, protocol.TerminalModes{AlternateScreen: true, AutoWrap: true}),
			update: protocol.ScreenUpdate{
				Size: protocol.Size{Cols: 120, Rows: 40},
				Ops: []protocol.ScreenOp{
					{Code: protocol.ScreenOpScrollRect, Rect: protocol.ScreenRect{X: 0, Y: 5, Width: 120, Height: 21}, Dy: -1},
					{Code: protocol.ScreenOpWriteSpan, Row: 25, Col: 0, Cells: opcodeBenchRow(120, benchLine("vim", 26, 120))},
				},
				Cursor: protocol.CursorState{Row: 25, Col: 0, Visible: true},
				Modes:  protocol.TerminalModes{AlternateScreen: true, AutoWrap: true},
			},
		},
		{
			name: "nvim_alt_fullwidth_scroll_3_rows",
			base: opcodeBenchSnapshotWithModes("nvim", 120, 40, protocol.TerminalModes{AlternateScreen: true, AutoWrap: true}),
			update: protocol.ScreenUpdate{
				Size: protocol.Size{Cols: 120, Rows: 40},
				Ops: []protocol.ScreenOp{
					{Code: protocol.ScreenOpScrollRect, Rect: protocol.ScreenRect{X: 0, Y: 0, Width: 120, Height: 40}, Dy: -3},
					{Code: protocol.ScreenOpWriteSpan, Row: 37, Col: 0, Cells: opcodeBenchRow(120, benchLine("nvim", 137, 120))},
					{Code: protocol.ScreenOpWriteSpan, Row: 38, Col: 0, Cells: opcodeBenchRow(120, benchLine("nvim", 138, 120))},
					{Code: protocol.ScreenOpWriteSpan, Row: 39, Col: 0, Cells: opcodeBenchRow(120, benchLine("nvim", 139, 120))},
				},
				Cursor: protocol.CursorState{Row: 39, Col: 8, Visible: true},
				Modes:  protocol.TerminalModes{AlternateScreen: true, AutoWrap: true},
			},
		},
		{
			name: "top_scroll",
			base: opcodeBenchSnapshot("top", 80, 24),
			update: protocol.ScreenUpdate{
				Size:         protocol.Size{Cols: 80, Rows: 24},
				ScreenScroll: 1,
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
			base: opcodeBenchSnapshotWithModes("move", 120, 40, protocol.TerminalModes{AlternateScreen: true, AutoWrap: true}),
			update: protocol.ScreenUpdate{
				Size: protocol.Size{Cols: 120, Rows: 40},
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
			update: protocol.ScreenUpdate{
				Size: protocol.Size{Cols: 120, Rows: 40},
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
	return opcodeBenchSnapshotWithModes(prefix, cols, rows, protocol.TerminalModes{})
}

func opcodeBenchSnapshotWithModes(prefix string, cols, rows int, modes protocol.TerminalModes) *protocol.Snapshot {
	lines := make([]string, rows)
	for row := 0; row < rows; row++ {
		lines[row] = benchLine(prefix, row, cols)
	}
	snapshot := snapshotWithLines("term-1", uint16(cols), uint16(rows), lines)
	if snapshot != nil {
		snapshot.Modes = modes
		snapshot.Screen.IsAlternateScreen = modes.AlternateScreen
	}
	return snapshot
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

func benchLine(prefix string, row, cols int) string {
	base := fmt.Sprintf("%s-%02d ", prefix, row)
	if len(base) >= cols {
		return base[:cols]
	}
	return base + strings.Repeat(".", cols-len(base))
}

func opcodeBenchVTerm(base *protocol.Snapshot) VTermLike {
	cols := 80
	rows := 24
	if base != nil {
		if base.Size.Cols > 0 {
			cols = int(base.Size.Cols)
		}
		if base.Size.Rows > 0 {
			rows = int(base.Size.Rows)
		}
	}
	vt := localvterm.New(cols, rows, 1024, nil)
	loadSnapshotIntoVTerm(vt, base)
	return vt
}

func opcodeBenchFullReplaceUpdate(base *protocol.Snapshot, update protocol.ScreenUpdate) protocol.ScreenUpdate {
	next := applyScreenUpdateSnapshot(base, "term-1", update)
	if next == nil {
		return protocol.ScreenUpdate{}
	}
	full := protocol.ScreenUpdate{
		FullReplace:      true,
		ResetScrollback:  !next.Modes.AlternateScreen,
		Size:             next.Size,
		Screen:           cloneProtocolScreenData(next.Screen),
		ScreenTimestamps: append([]time.Time(nil), next.ScreenTimestamps...),
		ScreenRowKinds:   append([]string(nil), next.ScreenRowKinds...),
		Cursor:           next.Cursor,
		Modes:            next.Modes,
	}
	if next.Modes.AlternateScreen {
		full.ResetScrollback = false
	}
	return full
}
