package history

import (
	"strings"
	"testing"

	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestR321StreamReducerCRRewriteSealsOnlyFinalLine(t *testing.T) {
	reducer := NewStreamLineReducer()
	applyStreamOps(t, reducer,
		writeOp(0, 0, "abc"),
		controlOp("cr", 0, 0, 0),
		writeOp(0, 0, "X"),
	)

	mutations, err := reducer.ApplyOp(controlOp("lf", 0, 1, 0))
	if err != nil {
		t.Fatalf("apply LF: %v", err)
	}
	sealed := sealedMutationLines(mutations)
	if got := joinedLineTexts(sealed); got != "Xbc" {
		t.Fatalf("CR rewrite must seal only final line, got %q mutations=%#v", got, mutations)
	}
	if len(sealed) != 1 || sealed[0].Seal != SealStateSealed {
		t.Fatalf("expected one sealed logical line, got %#v", sealed)
	}
}

func TestR321StreamReducerEraseLineModes(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode int
		col  int
		want string
	}{
		{name: "to end", mode: 0, col: 2, want: "ab"},
		{name: "to start", mode: 1, col: 3, want: "    ef"},
		{name: "whole line", mode: 2, col: 3, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reducer := NewStreamLineReducer()
			applyStreamOps(t, reducer,
				writeOp(0, 0, "abcdef"),
				controlOp("el", 0, tc.col, tc.mode),
			)
			line := singleOpenLine(t, reducer)
			if got := lineText(line.Draft.Line); got != tc.want {
				t.Fatalf("EL mode %d at col %d got %q want %q line=%#v", tc.mode, tc.col, got, tc.want, line)
			}
		})
	}
}

func TestR321StreamReducerCursorAddressingWritesTargetRow(t *testing.T) {
	reducer := NewStreamLineReducer()
	applyStreamOps(t, reducer,
		writeOp(0, 0, "row0"),
		controlOp("cub", 0, 3, 1),
		writeOp(0, 2, "W"),
		controlOp("bs", 0, 3, 0),
		writeOp(0, 2, "!"),
		controlOp("cup", 2, 1, 0),
		writeOp(2, 1, "Z"),
		controlOp("cup", 0, 2, 0),
		writeOp(0, 2, "?"),
	)

	lines := openLinesByRow(t, reducer)
	if got := lineText(lines[0].Draft.Line); got != "ro?0" {
		t.Fatalf("row 0 must be updated after CUP back to row, got %q", got)
	}
	if got := lineText(lines[2].Draft.Line); got != " Z" {
		t.Fatalf("row 2 must receive CUP-targeted write with preserved col, got %q", got)
	}
}

func TestR332StreamReducerWideCellsDoNotLeaveContinuationSpaces(t *testing.T) {
	reducer := NewStreamLineReducer()
	applyStreamOps(t, reducer, TerminalSemanticOp{
		Code: vterm.ScreenOpWriteSpan,
		Row:  0,
		Col:  0,
		Cells: []TerminalSemanticCell{
			{Content: "中", Width: 2},
			{Content: "", Width: 0},
			{Content: "文", Width: 2},
			{Content: "", Width: 0},
		},
	})

	line := singleOpenLine(t, reducer).Draft.Line
	if got := lineText(line); got != "中文" {
		t.Fatalf("wide-cell continuation placeholders must not become spaces, got %q line=%#v", got, line)
	}
	if len(line.Cells) != 2 || line.Cells[0].Width != 2 || line.Cells[1].Width != 2 {
		t.Fatalf("wide cells should keep authoritative widths, got %#v", line.Cells)
	}
}

func TestR321StreamReducerEraseDisplayAndClearRect(t *testing.T) {
	reducer := NewStreamLineReducer()
	applyStreamOps(t, reducer,
		writeOp(0, 0, "top"),
		writeOp(1, 0, "middle"),
		writeOp(2, 0, "bottom"),
		controlOp("ed", 1, 3, 0),
	)
	lines := openLinesByRow(t, reducer)
	if got := lineText(lines[0].Draft.Line); got != "top" {
		t.Fatalf("ED0 must keep rows above cursor, got row0=%q", got)
	}
	if got := lineText(lines[1].Draft.Line); got != "mid" {
		t.Fatalf("ED0 must erase current row from cursor to end, got row1=%q", got)
	}
	if _, ok := lines[2]; ok {
		t.Fatalf("ED0 must clear rows below cursor, got row2=%#v", lines[2])
	}

	applyStreamOps(t, reducer,
		TerminalSemanticOp{Code: vterm.ScreenOpClearRect, Rect: vterm.DamageRect{X: 1, Y: 0, Width: 1, Height: 2}},
	)
	lines = openLinesByRow(t, reducer)
	if got := lineText(lines[0].Draft.Line); got != "t p" {
		t.Fatalf("ClearRect should clear addressed cells in row0, got %q", got)
	}
	if got := lineText(lines[1].Draft.Line); got != "m d" {
		t.Fatalf("ClearRect should clear addressed cells in row1, got %q", got)
	}
}

func TestR328StreamReducerED2SealsOpenLineBeforeClearingScreen(t *testing.T) {
	reducer := NewStreamLineReducer()
	applyStreamOps(t, reducer,
		writeOp(0, 0, "old-a"),
		controlOp("lf", 0, 5, 0),
		writeOp(1, 0, "old-b"),
	)

	mutations, err := reducer.ApplyOp(controlOp("ed", 0, 0, 2))
	if err != nil {
		t.Fatalf("apply ED2: %v", err)
	}
	sealed := joinedLineTexts(sealedMutationLines(mutations))
	if !strings.Contains(sealed, "old-b") {
		t.Fatalf("ED2 must seal visible open line before clearing screen, sealed=%q mutations=%#v", sealed, mutations)
	}
	if lines := openLinesByRow(t, reducer); len(lines) != 0 {
		t.Fatalf("ED2 should remove cleared rows from ordinary open ownership, got %#v", lines)
	}
}

func TestR321StreamReducerEraseDeleteInsertCharacter(t *testing.T) {
	t.Run("ech", func(t *testing.T) {
		reducer := NewStreamLineReducer()
		applyStreamOps(t, reducer,
			writeOp(0, 0, "ABCDE"),
			controlOp("ech", 0, 1, 2),
		)
		if got := lineText(singleOpenLine(t, reducer).Draft.Line); got != "A  DE" {
			t.Fatalf("ECH should erase cells in place, got %q", got)
		}
	})
	t.Run("dch", func(t *testing.T) {
		reducer := NewStreamLineReducer()
		applyStreamOps(t, reducer,
			writeOp(0, 0, "ABCDE"),
			controlOp("dch", 0, 1, 2),
		)
		if got := lineText(singleOpenLine(t, reducer).Draft.Line); got != "ADE" {
			t.Fatalf("DCH should delete cells from cursor, got %q", got)
		}
	})
	t.Run("ich", func(t *testing.T) {
		reducer := NewStreamLineReducer()
		applyStreamOps(t, reducer,
			writeOp(0, 0, "ABCDE"),
			controlOp("ich", 0, 1, 2),
		)
		if got := lineText(singleOpenLine(t, reducer).Draft.Line); got != "A  BCDE" {
			t.Fatalf("ICH should insert blank cells at cursor, got %q", got)
		}
	})
}

func TestR321StreamReducerScrollOutProofSealsTerminalEvents(t *testing.T) {
	reducer := NewStreamLineReducer()
	applyStreamOps(t, reducer, writeOp(0, 0, "visible"))

	first, err := reducer.SealScrollOut(TerminalSemanticScrollOut{Runs: []TerminalSemanticCellRun{{Text: "gone"}}})
	if err != nil {
		t.Fatalf("seal scroll-out: %v", err)
	}
	second, err := reducer.SealScrollOut(TerminalSemanticScrollOut{Runs: []TerminalSemanticCellRun{{Text: "gone"}}})
	if err != nil {
		t.Fatalf("seal repeated scroll-out: %v", err)
	}
	if got := joinedLineTexts(sealedMutationLines(first)); got != "gone" {
		t.Fatalf("first proof must seal proof payload, got %q mutations=%#v", got, first)
	}
	if got := joinedLineTexts(sealedMutationLines(second)); got != "gone" {
		t.Fatalf("same text in a later terminal proof is still observable PTY history, got %q mutations=%#v", got, second)
	}
	if got := lineText(singleOpenLine(t, reducer).Draft.Line); got != "visible" {
		t.Fatalf("scroll-out proof must not rewrite active open line, got %q", got)
	}
}

func TestR321StreamReducerScrollAndCopyRectMoveRowOwnership(t *testing.T) {
	reducer := NewStreamLineReducer()
	applyStreamOps(t, reducer,
		writeOp(0, 0, "row0"),
		writeOp(1, 0, "row1"),
		writeOp(2, 0, "row2"),
		TerminalSemanticOp{Code: vterm.ScreenOpScrollRect, Rect: vterm.DamageRect{X: 0, Y: 0, Width: 10, Height: 3}, Dy: -1},
		TerminalSemanticOp{Code: vterm.ScreenOpCopyRect, Src: vterm.DamageRect{X: 0, Y: 1, Width: 4, Height: 1}, DstX: 0, DstY: 2},
	)
	lines := openLinesByRow(t, reducer)
	if got := lineText(lines[0].Draft.Line); got != "row1" {
		t.Fatalf("scroll rect should move row1 ownership to row0, got %q", got)
	}
	if got := lineText(lines[1].Draft.Line); got != "row2" {
		t.Fatalf("scroll rect should move row2 ownership to row1, got %q", got)
	}
	if got := lineText(lines[2].Draft.Line); got != "row2" {
		t.Fatalf("copy rect should copy row1 payload into row2 ownership, got %q", got)
	}
}

func TestR329StreamReducerConsumesVTermLineControls(t *testing.T) {
	for _, tc := range []struct {
		name    string
		control TerminalSemanticOp
		trailer *TerminalSemanticOp
		want    map[int]string
	}{
		{
			name:    "insert line moves owned rows down once",
			control: controlOpWithBottom("il", 1, 0, 1, 3),
			trailer: &TerminalSemanticOp{Code: vterm.ScreenOpScrollRect, Rect: vterm.DamageRect{X: 0, Y: 1, Width: 12, Height: 2}, Dy: 1},
			want:    map[int]string{0: "top", 2: "mid"},
		},
		{
			name:    "delete line moves owned rows up once",
			control: controlOpWithBottom("dl", 1, 0, 1, 3),
			trailer: &TerminalSemanticOp{Code: vterm.ScreenOpScrollRect, Rect: vterm.DamageRect{X: 0, Y: 1, Width: 12, Height: 2}, Dy: -1},
			want:    map[int]string{0: "top", 1: "bot"},
		},
		{
			name:    "scroll up uses scroll region bottom",
			control: controlOpWithBottom("su", 0, 0, 1, 3),
			trailer: &TerminalSemanticOp{Code: vterm.ScreenOpScrollRect, Rect: vterm.DamageRect{X: 0, Y: 0, Width: 12, Height: 3}, Dy: -1},
			want:    map[int]string{0: "mid", 1: "bot"},
		},
		{
			name:    "scroll down uses scroll region bottom",
			control: controlOpWithBottom("sd", 0, 0, 1, 3),
			trailer: &TerminalSemanticOp{Code: vterm.ScreenOpScrollRect, Rect: vterm.DamageRect{X: 0, Y: 0, Width: 12, Height: 3}, Dy: 1},
			want:    map[int]string{1: "top", 2: "mid"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reducer := NewStreamLineReducer()
			applyStreamOps(t, reducer,
				writeOp(0, 0, "top"),
				writeOp(1, 0, "mid"),
				writeOp(2, 0, "bot"),
				tc.control,
			)
			if tc.trailer != nil {
				applyStreamOps(t, reducer, *tc.trailer)
			}
			lines := openLinesByRow(t, reducer)
			assertOpenLineTexts(t, lines, tc.want)
		})
	}
}

func TestR329StreamReducerReverseIndexUsesScrollRegion(t *testing.T) {
	reducer := NewStreamLineReducer()
	applyStreamOps(t, reducer,
		writeOp(0, 0, "top"),
		writeOp(1, 0, "region-a"),
		writeOp(2, 0, "region-b"),
		writeOp(3, 0, "region-c"),
		controlOpWithBottom("decstbm", 0, 0, 2, 4),
		controlOp("cup", 1, 0, 0),
		controlOp("ri", 1, 0, 0),
		TerminalSemanticOp{Code: vterm.ScreenOpScrollRect, Rect: vterm.DamageRect{X: 0, Y: 1, Width: 12, Height: 3}, Dy: 1},
	)

	lines := openLinesByRow(t, reducer)
	assertOpenLineTexts(t, lines, map[int]string{
		0: "top",
		2: "region-a",
		3: "region-b",
	})
}

func TestR329StreamReducerRISSealsAndClearsMutableRows(t *testing.T) {
	reducer := NewStreamLineReducer()
	applyStreamOps(t, reducer, writeOp(0, 0, "before"))

	mutations, err := reducer.ApplyOp(controlOp("ris", 0, 6, 0))
	if err != nil {
		t.Fatalf("apply RIS: %v", err)
	}
	if got := joinedLineTexts(sealedMutationLines(mutations)); got != "before" {
		t.Fatalf("RIS must seal current observable line before reset, got %q mutations=%#v", got, mutations)
	}
	if lines := openLinesByRow(t, reducer); len(lines) != 0 {
		t.Fatalf("RIS must clear ordinary row ownership, got %#v", lines)
	}
}

func TestR329StreamReducerCarriesVTermTailFill(t *testing.T) {
	reducer := NewStreamLineReducer()
	applyStreamOps(t, reducer, writeOp(0, 0, "styled"))

	lf := controlOp("lf", 0, 6, 0)
	lf.TailFill = &TerminalSemanticStyle{BG: "idx:24"}
	mutations, err := reducer.ApplyOp(lf)
	if err != nil {
		t.Fatalf("apply LF with tail fill: %v", err)
	}
	sealed := sealedMutationLines(mutations)
	if len(sealed) != 1 || sealed[0].TailFill == nil || sealed[0].TailFill.Style.BG != "idx:24" {
		t.Fatalf("vterm TailFill must be copied into logical history line, got %#v mutations=%#v", sealed, mutations)
	}
}

func TestR321StreamReducerSoftWrapAndLFBoundaries(t *testing.T) {
	reducer := NewStreamLineReducer()
	applyStreamOps(t, reducer,
		writeOp(0, 0, "abc"),
		controlOp("soft-wrap", 0, 3, 0),
		writeOp(1, 0, "def"),
	)
	mutations, err := reducer.ApplyOp(controlOp("lf", 1, 3, 0))
	if err != nil {
		t.Fatalf("apply LF: %v", err)
	}
	if got := joinedLineTexts(sealedMutationLines(mutations)); got != "abcdef" {
		t.Fatalf("soft wrapped physical rows must stay one logical line, got %q", got)
	}
}

func applyStreamOps(t *testing.T, reducer StreamLineReducer, ops ...TerminalSemanticOp) {
	t.Helper()
	for _, op := range ops {
		if _, err := reducer.ApplyOp(op); err != nil {
			t.Fatalf("apply op %#v: %v", op, err)
		}
	}
}

func singleOpenLine(t *testing.T, reducer StreamLineReducer) OpenLine {
	t.Helper()
	lines := openLinesByRow(t, reducer)
	if len(lines) != 1 {
		t.Fatalf("expected one open line, got %#v", lines)
	}
	for _, line := range lines {
		return line
	}
	return OpenLine{}
}

func openLinesByRow(t *testing.T, reducer StreamLineReducer) map[int]OpenLine {
	t.Helper()
	debug, ok := reducer.(interface {
		debugOpenLinesByRow() map[int]OpenLine
	})
	if !ok {
		t.Fatalf("reducer does not expose test debug row ownership")
	}
	return debug.debugOpenLinesByRow()
}

func sealedMutationLines(mutations []HistoryMutation) []LogicalLine {
	var lines []LogicalLine
	for _, mutation := range mutations {
		if mutation.Kind == HistoryMutationSealLine && mutation.Line != nil {
			lines = append(lines, *mutation.Line)
		}
	}
	return lines
}

func joinedLineTexts(lines []LogicalLine) string {
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		parts = append(parts, lineText(line))
	}
	return strings.Join(parts, "\n")
}

func lineText(line LogicalLine) string {
	var out string
	for _, cell := range line.Cells {
		out += cell.Text
	}
	return out
}

func writeOp(row int, col int, text string) TerminalSemanticOp {
	cells := make([]TerminalSemanticCell, 0, len(text))
	for _, r := range text {
		cells = append(cells, TerminalSemanticCell{Content: string(r), Width: 1})
	}
	return TerminalSemanticOp{Code: vterm.ScreenOpWriteSpan, Row: row, Col: col, Cells: cells}
}

func controlOp(kind string, row int, col int, mode int) TerminalSemanticOp {
	return TerminalSemanticOp{Code: vterm.ScreenOpControl, Control: kind, Row: row, Col: col, Mode: mode}
}

func controlOpWithBottom(kind string, row int, col int, mode int, bottom int) TerminalSemanticOp {
	op := controlOp(kind, row, col, mode)
	op.Bottom = bottom
	return op
}

func assertOpenLineTexts(t *testing.T, lines map[int]OpenLine, want map[int]string) {
	t.Helper()
	if len(lines) != len(want) {
		t.Fatalf("unexpected row ownership count, got %#v want %#v", lines, want)
	}
	for row, text := range want {
		line, ok := lines[row]
		if !ok {
			t.Fatalf("missing row %d in %#v", row, lines)
		}
		if got := lineText(line.Draft.Line); got != text {
			t.Fatalf("row %d got %q want %q lines=%#v", row, got, text, lines)
		}
	}
}
