package vterm

import (
	"reflect"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	charmvt "github.com/lozzow/termx/termx-vterm/internal/vt"
)

func TestLoadSnapshotRestoresScreenAndCursor(t *testing.T) {
	vt := New(10, 4, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{
			{
				{Content: "h", Width: 1},
				{Content: "i", Width: 1},
			},
		},
	}, CursorState{Row: 0, Col: 2, Visible: true}, TerminalModes{AutoWrap: true})

	screen := vt.ScreenContent()
	if got := screen.Cells[0][0].Content + screen.Cells[0][1].Content; got != "hi" {
		t.Fatalf("expected restored content %q, got %q", "hi", got)
	}
	cursor := vt.CursorState()
	if cursor.Col != 2 || cursor.Row != 0 {
		t.Fatalf("expected restored cursor at (2,0), got (%d,%d)", cursor.Col, cursor.Row)
	}

	if _, err := vt.Write([]byte("!")); err != nil {
		t.Fatalf("write after snapshot failed: %v", err)
	}
	screen = vt.ScreenContent()
	if got := screen.Cells[0][0].Content + screen.Cells[0][1].Content + screen.Cells[0][2].Content; got != "hi!" {
		t.Fatalf("expected continued output %q, got %q", "hi!", got)
	}
}

func TestLoadSnapshotWithScrollbackRestoresHistory(t *testing.T) {
	vt := New(6, 3, 100, nil)
	vt.LoadSnapshotWithScrollback([][]Cell{
		{{Content: "o", Width: 1}, {Content: "l", Width: 1}, {Content: "d", Width: 1}},
	}, ScreenData{
		Cells: [][]Cell{
			{
				{Content: "n", Width: 1},
				{Content: "e", Width: 1},
				{Content: "w", Width: 1},
			},
		},
	}, CursorState{Row: 0, Col: 3, Visible: true}, TerminalModes{AutoWrap: true})

	scrollback := vt.ScrollbackContent()
	if len(scrollback) != 1 {
		t.Fatalf("expected 1 restored scrollback row, got %d", len(scrollback))
	}
	if got := scrollback[0][0].Content + scrollback[0][1].Content + scrollback[0][2].Content; got != "old" {
		t.Fatalf("expected restored scrollback %q, got %q", "old", got)
	}
}

func TestLoadSnapshotRestoresStyledBlankRows(t *testing.T) {
	const bg = "#222222"
	vt := New(12, 3, 100, nil)

	styledBlanks := make([]Cell, 12)
	for i := range styledBlanks {
		styledBlanks[i] = Cell{Content: " ", Width: 1, Style: CellStyle{BG: bg}}
	}
	promptRow := append([]Cell(nil), styledBlanks...)
	for i, r := range "> prompt" {
		promptRow[i] = Cell{Content: string(r), Width: 1, Style: CellStyle{BG: bg}}
	}

	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{
			styledBlanks,
			promptRow,
		},
		IsAlternateScreen: true,
	}, CursorState{Row: 1, Col: 8, Visible: true}, TerminalModes{AlternateScreen: true, AutoWrap: true})

	screen := vt.ScreenContent()
	if got := screen.Cells[0][0].Style.BG; got != bg {
		t.Fatalf("expected pure styled blank row to restore bg %q, got %#v", bg, screen.Cells[0][0])
	}
	if got := screen.Cells[0][11].Style.BG; got != bg {
		t.Fatalf("expected pure styled blank row tail to restore bg %q, got %#v", bg, screen.Cells[0][11])
	}
	if got := screen.Cells[1][0].Style.BG; got != bg {
		t.Fatalf("expected prompt row bg %q, got %#v", bg, screen.Cells[1][0])
	}
	if got := rowText(screen.Cells[1], 8); got != "> prompt" {
		t.Fatalf("expected prompt content restored, got %q", got)
	}
}

func TestVTermResizeReflowsSoftWrappedRows(t *testing.T) {
	vt := New(10, 4, 100, nil)
	if _, err := vt.Write([]byte("abcdefghijk")); err != nil {
		t.Fatalf("write wrapped content: %v", err)
	}
	if !vt.ScreenRowWrappedAt(0) {
		t.Fatalf("expected first row to be marked as soft-wrapped")
	}

	vt.Resize(20, 4)
	if got := rowText(vt.ScreenRowView(0), 11); got != "abcdefghijk" {
		t.Fatalf("expected widened resize to join soft-wrapped row, got %q", got)
	}
	if got := strings.TrimSpace(rowText(vt.ScreenRowView(1), 20)); got != "" {
		t.Fatalf("expected second row blank after widening, got %q", got)
	}
	if vt.ScreenRowWrappedAt(0) {
		t.Fatalf("expected joined row to no longer be marked as wrapped")
	}

	vt.Resize(5, 4)
	if got := rowText(vt.ScreenRowView(0), 5); got != "abcde" {
		t.Fatalf("expected row 0 after narrowing, got %q", got)
	}
	if got := rowText(vt.ScreenRowView(1), 5); got != "fghij" {
		t.Fatalf("expected row 1 after narrowing, got %q", got)
	}
	if got := strings.TrimSpace(rowText(vt.ScreenRowView(2), 5)); got != "k" {
		t.Fatalf("expected row 2 after narrowing, got %q", got)
	}
	if !vt.ScreenRowWrappedAt(0) || !vt.ScreenRowWrappedAt(1) || vt.ScreenRowWrappedAt(2) {
		t.Fatalf("unexpected wrapped markers after narrowing: %#v", vt.ScreenWrapped())
	}
}

func TestVTermResizeDoesNotJoinHardNewlineRows(t *testing.T) {
	vt := New(10, 4, 100, nil)
	if _, err := vt.Write([]byte("abc\r\nDEF")); err != nil {
		t.Fatalf("write hard newline content: %v", err)
	}

	vt.Resize(20, 4)
	if got := strings.TrimSpace(rowText(vt.ScreenRowView(0), 20)); got != "abc" {
		t.Fatalf("expected hard-newline row 0 preserved, got %q", got)
	}
	if got := strings.TrimSpace(rowText(vt.ScreenRowView(1), 20)); got != "DEF" {
		t.Fatalf("expected hard-newline row 1 preserved, got %q", got)
	}
	if vt.ScreenRowWrappedAt(0) {
		t.Fatalf("hard newline row must not be marked as wrapped")
	}
}

func TestVTermResizeDoesNotJoinExactWidthHardNewlineRows(t *testing.T) {
	vt := New(4, 4, 100, nil)
	if _, err := vt.Write([]byte("ABCD\r\nWXYZ")); err != nil {
		t.Fatalf("write exact-width hard newline content: %v", err)
	}
	if vt.ScreenRowWrappedAt(0) {
		t.Fatalf("exact-width newline row must not be marked as wrapped before resize")
	}

	vt.Resize(8, 4)
	if got := strings.TrimSpace(rowText(vt.ScreenRowView(0), 8)); got != "ABCD" {
		t.Fatalf("expected exact-width hard-newline row 0 preserved after resize, got %q", got)
	}
	if got := strings.TrimSpace(rowText(vt.ScreenRowView(1), 8)); got != "WXYZ" {
		t.Fatalf("expected exact-width hard-newline row 1 preserved after resize, got %q", got)
	}
	if vt.ScreenRowWrappedAt(0) {
		t.Fatalf("exact-width hard newline row must not be marked as wrapped after resize")
	}
}

func TestVTermResizeSplitsAndRejoinsHardNewlineArtRows(t *testing.T) {
	vt := New(8, 6, 100, nil)
	if _, err := vt.Write([]byte("AA  BB  \r\nCCDDCCDD\r\nuri: ok")); err != nil {
		t.Fatalf("write art content: %v", err)
	}

	vt.Resize(4, 6)
	if got := strings.TrimSpace(rowText(vt.ScreenRowView(0), 4)); got != "AA" {
		t.Fatalf("expected hard-newline art row 0 split after shrink, got %q", got)
	}
	if got := strings.TrimSpace(rowText(vt.ScreenRowView(1), 4)); got != "BB" {
		t.Fatalf("expected hard-newline art row 1 split after shrink, got %q", got)
	}
	if got := rowText(vt.ScreenRowView(2), 4); got != "CCDD" {
		t.Fatalf("expected hard-newline art row 2 split after shrink, got %q", got)
	}
	if got := rowText(vt.ScreenRowView(3), 4); got != "CCDD" {
		t.Fatalf("expected hard-newline art row 3 split after shrink, got %q", got)
	}
	if got := strings.TrimSpace(rowText(vt.ScreenRowView(4), 4)); got != "uri:" {
		t.Fatalf("expected following text row to stay after split rows, got %q", got)
	}
	if got := strings.TrimSpace(rowText(vt.ScreenRowView(5), 4)); got != "ok" {
		t.Fatalf("expected final text row to stay after split rows, got %q", got)
	}
	if wrapped := vt.ScreenWrapped(); !wrapped[0] || wrapped[1] || !wrapped[2] || wrapped[3] || !wrapped[4] || wrapped[5] {
		t.Fatalf("unexpected wrapped markers after shrink: %#v", wrapped)
	}

	vt.Resize(8, 6)
	if got := strings.TrimSpace(rowText(vt.ScreenRowView(0), 8)); got != "AA  BB" {
		t.Fatalf("expected hard-newline art row 0 rejoined after grow, got %q", got)
	}
	if got := rowText(vt.ScreenRowView(1), 8); got != "CCDDCCDD" {
		t.Fatalf("expected hard-newline art row 1 rejoined after grow, got %q", got)
	}
	if got := strings.TrimSpace(rowText(vt.ScreenRowView(2), 8)); got != "uri: ok" {
		t.Fatalf("expected following text row rejoined after grow, got %q", got)
	}
	if wrapped := vt.ScreenWrapped(); wrapped[0] || wrapped[1] || wrapped[2] {
		t.Fatalf("expected rejoined hard-newline rows to clear visible wrap markers, got %#v", wrapped)
	}
}

func TestVTermResizePreservesHardNewlineTrailingSpaceColumns(t *testing.T) {
	vt := New(4, 6, 100, nil)
	if _, err := vt.Write([]byte("AA  \r\nBB")); err != nil {
		t.Fatalf("write hard-newline trailing spaces: %v", err)
	}

	vt.Resize(2, 6)

	if got := rowText(vt.ScreenRowView(0), 2); got != "AA" {
		t.Fatalf("expected first split row to contain AA, got %q", got)
	}
	if !vt.ScreenRowWrappedAt(0) {
		t.Fatalf("expected trailing hard-newline spaces to keep row 0 wrapped after shrink")
	}
	if got := rowText(vt.ScreenRowView(1), 2); got != "  " {
		t.Fatalf("expected second split row to preserve trailing spaces, got %q", got)
	}
	if vt.ScreenRowWrappedAt(1) {
		t.Fatalf("expected trailing-space split row to terminate the hard-newline row")
	}
	if got := rowText(vt.ScreenRowView(2), 2); got != "BB" {
		t.Fatalf("expected following hard-newline row to stay below trailing spaces, got %q", got)
	}
}

func TestVTermScrollbackUsesStoredLineLengthAfterResize(t *testing.T) {
	vt := New(4, 2, 100, nil)
	if _, err := vt.Write([]byte("AA  \r\nBB\r\nCC")); err != nil {
		t.Fatalf("write scrollback trailing spaces: %v", err)
	}

	scrollback := vt.ScrollbackContent()
	if len(scrollback) == 0 {
		t.Fatal("expected scrollback row before resize")
	}
	if got := rowText(scrollback[0], 4); got != "AA  " {
		t.Fatalf("expected scrollback to preserve trailing spaces before resize, got %q row=%#v", got, scrollback[0])
	}

	vt.Resize(2, 2)
	scrollback = vt.ScrollbackContent()
	if len(scrollback) < 2 {
		t.Fatalf("expected resize to reflow scrollback rows, got %d", len(scrollback))
	}
	if got := rowText(scrollback[0], 2); got != "AA" {
		t.Fatalf("expected reflowed scrollback row 0, got %q row=%#v", got, scrollback[0])
	}
	if got := rowText(scrollback[1], 2); got != "  " {
		t.Fatalf("expected trailing-space scrollback segment after resize, got %q row=%#v", got, scrollback[1])
	}
}

func TestVTermResizeWithDamageAppendsRowsDisplacedByShrink(t *testing.T) {
	vt := New(12, 4, 0, nil)
	vt.DisableEmulatorScrollback()
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{
			cellsFromString("row 000098"),
			cellsFromString("row 000099"),
			cellsFromString("row 000100"),
			cellsFromString("prompt-tail"),
		},
	}, CursorState{Row: 3, Col: 10, Visible: true}, TerminalModes{AutoWrap: true})

	damage := vt.ResizeWithDamage(6, 4)
	if !damage.RequiresFullReplace || damage.FullReplaceReason != "resize" {
		t.Fatalf("expected resize full-replace damage, got %#v", damage)
	}
	gotRows := append(damageRowsText(damage.ScrollbackAppend), screenRowsText(vt.ScreenContent().Cells)...)
	joined := strings.Join(gotRows, "")
	for _, want := range []string{"row 000098", "row 000099", "row 000100", "prompt-tail"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected resize damage plus screen rows to contain %q, got %#v", want, gotRows)
		}
	}
}

func TestVTermResizeWithDamagePreservesWrappedBoundary(t *testing.T) {
	vt := New(5, 2, 0, nil)
	vt.DisableEmulatorScrollback()
	if _, err := vt.Write([]byte("abcdefghij")); err != nil {
		t.Fatalf("write wrapped line: %v", err)
	}
	if !vt.ScreenRowWrappedAt(0) {
		t.Fatal("expected first row wrapped before resize")
	}

	damage := vt.ResizeWithDamage(4, 2)
	gotRows := append(damageRowsText(damage.ScrollbackAppend), screenRowsText(vt.ScreenContent().Cells)...)
	if strings.Contains(strings.Join(gotRows, "|"), "ij|ij") {
		t.Fatalf("visible suffix duplicated in resize damage plus screen, got %#v", gotRows)
	}
	if len(damage.ScrollbackAppend) < 1 || !damage.ScrollbackAppend[0].Wrapped {
		t.Fatalf("expected displaced wrapped prefix in damage, got %#v", damage.ScrollbackAppend)
	}
}

func TestVTermResizeWithDamageDoesNotAppendVisibleSuffix(t *testing.T) {
	vt := New(5, 2, 0, nil)
	vt.DisableEmulatorScrollback()
	if _, err := vt.Write([]byte("abcdefghij")); err != nil {
		t.Fatalf("write wrapped line: %v", err)
	}

	damage := vt.ResizeWithDamage(4, 2)
	gotDamageRows := damageRowsText(damage.ScrollbackAppend)
	if !reflect.DeepEqual(gotDamageRows, []string{"abcd"}) {
		t.Fatalf("expected only displaced rows in damage, got %#v screen=%#v wrapped=%#v", gotDamageRows, screenRowsText(vt.ScreenContent().Cells), vt.ScreenWrapped())
	}
	if len(damage.ScrollbackAppend) < 1 || !damage.ScrollbackAppend[0].Wrapped {
		t.Fatalf("expected damage prefix to remain wrapped into visible screen, got %#v", damage.ScrollbackAppend)
	}
}

func TestVTermResizeWithDamageTailPlanDoesNotMatchEarlierDuplicateRows(t *testing.T) {
	vt := New(5, 3, 0, nil)
	vt.DisableEmulatorScrollback()
	base := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	screenTimes := []time.Time{
		base.Add(1 * time.Second),
		base.Add(2 * time.Second),
		base.Add(3 * time.Second),
	}
	vt.LoadSnapshotWithMetadata(nil, nil, nil, ScreenData{
		Cells: [][]Cell{
			cellsFromString("aaaaa"),
			cellsFromString("aaaaa"),
			cellsFromString("aaaaa"),
		},
	}, screenTimes, []string{"first", "second", "tail"}, CursorState{Row: 2, Col: 4, Visible: true}, TerminalModes{AutoWrap: true})

	damage := vt.ResizeWithDamage(3, 2)
	gotDamageRows := damageRowsText(damage.ScrollbackAppend)
	if !reflect.DeepEqual(gotDamageRows, []string{"aaa", "aa", "aaa", "aa"}) {
		t.Fatalf("expected resize plan to append positional prefix, got %#v screen=%#v", gotDamageRows, screenRowsText(vt.ScreenContent().Cells))
	}
	gotKinds := make([]string, 0, len(damage.ScrollbackAppend))
	for _, row := range damage.ScrollbackAppend {
		gotKinds = append(gotKinds, row.RowKind)
	}
	if !reflect.DeepEqual(gotKinds, []string{"first", "first", "second", "second"}) {
		t.Fatalf("expected explicit resize plan to append the older duplicate line first, got row kinds %#v", gotKinds)
	}
	if gotScreenRows := trimmedScreenRowsText(vt.ScreenContent().Cells); !reflect.DeepEqual(gotScreenRows, []string{"aaa", "aa"}) {
		t.Fatalf("expected resize plan to keep positional tail on screen, got %#v", gotScreenRows)
	}
}

func TestVTermResizeWithDamagePreservesWideRows(t *testing.T) {
	vt := New(8, 3, 0, nil)
	vt.DisableEmulatorScrollback()
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{
			{
				{Content: "你", Width: 2},
				{Content: "", Width: 0},
				{Content: "好", Width: 2},
				{Content: "", Width: 0},
				{Content: "A", Width: 1},
			},
			cellsFromString("qr-####"),
			cellsFromString("tail"),
		},
	}, CursorState{Row: 2, Col: 4, Visible: true}, TerminalModes{AutoWrap: true})

	damage := vt.ResizeWithDamage(4, 3)
	gotRows := append(damageRowsContentText(damage.ScrollbackAppend), screenRowsContentText(vt.ScreenContent().Cells)...)
	if !strings.Contains(strings.Join(gotRows, ""), "tail") {
		t.Fatalf("expected tail to remain visible after resize, got %#v", gotRows)
	}
	if len(damage.ScrollbackAppend) == 0 || len(damage.ScrollbackAppend[0].Cells) < 4 {
		t.Fatalf("expected wide row cells in damage, got %#v", damage.ScrollbackAppend)
	}
	if got := damage.ScrollbackAppend[0].Cells[0]; got.Content != "你" || got.Width != 2 {
		t.Fatalf("expected wide anchor preserved, got %#v", got)
	}
	if got := damage.ScrollbackAppend[0].Cells[1]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected wide continuation preserved, got %#v", got)
	}
}

func TestLoadSnapshotDefaultBlankRowsDoNotBecomeUsedRows(t *testing.T) {
	vt := New(4, 3, 100, nil)
	blank := []Cell{
		{Content: " ", Width: 1},
		{Content: " ", Width: 1},
		{Content: " ", Width: 1},
		{Content: " ", Width: 1},
	}

	vt.LoadSizedSnapshotWithExtendedMetadata(
		4,
		3,
		nil,
		nil,
		nil,
		nil,
		ScreenData{Cells: [][]Cell{blank}},
		nil,
		nil,
		nil,
		CursorState{Row: 0, Col: 0, Visible: true},
		TerminalModes{AutoWrap: true},
	)

	if got := len(vt.ScreenRowView(0)); got != 4 {
		t.Fatalf("expected public screen row to remain full width, got %d", got)
	}
	if got := len(vt.UsedScreenRow(0)); got != 4 {
		t.Fatalf("expected used screen row to keep blank row indexable for snapshots, got %d", got)
	}

	vt.Resize(2, 3)
	if got := strings.TrimSpace(rowText(vt.ScreenRowView(0), 2)); got != "" {
		t.Fatalf("expected blank row to stay blank after resize, got %q", got)
	}
	if got := strings.TrimSpace(rowText(vt.ScreenRowView(1), 2)); got != "" {
		t.Fatalf("expected blank row not to reflow into extra used row, got %q", got)
	}
}

func TestLoadSnapshotWithExtendedMetadataRestoresWrappedRows(t *testing.T) {
	vt := New(5, 3, 100, nil)
	screen := ScreenData{Cells: [][]Cell{
		cellsFromString("abcde"),
		cellsFromString("fgh"),
	}}
	vt.LoadSnapshotWithExtendedMetadata(nil, nil, nil, nil, screen, nil, nil, []bool{true, false}, CursorState{Row: 1, Col: 3, Visible: true}, TerminalModes{AutoWrap: true})

	vt.Resize(20, 3)
	if got := strings.TrimSpace(rowText(vt.ScreenRowView(0), 20)); got != "abcdefgh" {
		t.Fatalf("expected resize to join snapshot soft-wrap rows, got %q", got)
	}
	if got := strings.TrimSpace(rowText(vt.ScreenRowView(1), 20)); got != "" {
		t.Fatalf("expected second row blank after join, got %q", got)
	}
}

func TestLoadSizedSnapshotWithExtendedMetadataPreservesSparseScreenGeometry(t *testing.T) {
	vt := New(10, 4, 100, nil)
	screen := ScreenData{Cells: [][]Cell{
		cellsFromString("100000"),
		cellsFromString("python total"),
		cellsFromString("# prompt"),
	}}

	vt.LoadSizedSnapshotWithExtendedMetadata(20, 5, nil, nil, nil, nil, screen, nil, nil, nil, CursorState{Row: 2, Col: 8, Visible: true}, TerminalModes{AutoWrap: true})

	cols, rows := vt.Size()
	if cols != 20 || rows != 5 {
		t.Fatalf("expected sized snapshot geometry 20x5, got %dx%d", cols, rows)
	}
	if got := strings.TrimSpace(rowText(vt.ScreenRowView(1), 20)); got != "python total" {
		t.Fatalf("expected sparse row to stay at row 1 without replay reflow, got %q", got)
	}
	if got := strings.TrimSpace(rowText(vt.ScreenRowView(2), 20)); got != "# prompt" {
		t.Fatalf("expected prompt row to stay at row 2, got %q", got)
	}
	if got := strings.TrimSpace(rowText(vt.ScreenRowView(3), 20)); got != "" {
		t.Fatalf("expected padded row 3 blank, got %q", got)
	}
}

func cellsFromString(value string) []Cell {
	cells := make([]Cell, 0, len(value))
	for _, r := range value {
		cells = append(cells, Cell{Content: string(r), Width: 1})
	}
	return cells
}

func rowText(row []Cell, limit int) string {
	var b strings.Builder
	for i, cell := range row {
		if i >= limit {
			break
		}
		if cell.Content == "" {
			b.WriteByte(' ')
			continue
		}
		b.WriteString(cell.Content)
	}
	return b.String()
}

func damageRowsText(rows []DamageOp) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, rowText(row.Cells, len(row.Cells)))
	}
	return out
}

func screenRowsText(rows [][]Cell) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, rowText(row, len(row)))
	}
	return out
}

func trimmedScreenRowsText(rows [][]Cell) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, strings.TrimRight(rowText(row, len(row)), " "))
	}
	return out
}

func damageRowsContentText(rows []DamageOp) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, rowContentText(row.Cells))
	}
	return out
}

func screenRowsContentText(rows [][]Cell) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, rowContentText(row))
	}
	return out
}

func rowContentText(row []Cell) string {
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(cell.Content)
	}
	return strings.TrimRight(b.String(), " ")
}

func TestLoadSnapshotPreservesWideCellContinuationsAcrossSubsequentWrites(t *testing.T) {
	vt := New(8, 2, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{
			{
				{Content: "你", Width: 2},
				{Content: "", Width: 0},
				{Content: "好", Width: 2},
				{Content: "", Width: 0},
				{Content: "A", Width: 1},
			},
		},
	}, CursorState{Row: 0, Col: 5, Visible: true}, TerminalModes{AutoWrap: true})

	screen := vt.ScreenContent()
	if got := screen.Cells[0][0]; got.Content != "你" || got.Width != 2 {
		t.Fatalf("expected first wide cell restored, got %#v", got)
	}
	if got := screen.Cells[0][1]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected wide-cell continuation placeholder at x=1, got %#v", got)
	}
	if got := screen.Cells[0][2]; got.Content != "好" || got.Width != 2 {
		t.Fatalf("expected second wide cell restored, got %#v", got)
	}
	if got := screen.Cells[0][3]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected wide-cell continuation placeholder at x=3, got %#v", got)
	}

	if _, err := vt.Write([]byte("!")); err != nil {
		t.Fatalf("write after wide-cell snapshot failed: %v", err)
	}

	screen = vt.ScreenContent()
	if got := screen.Cells[0][0]; got.Content != "你" || got.Width != 2 {
		t.Fatalf("expected first wide cell preserved after write, got %#v", got)
	}
	if got := screen.Cells[0][1]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected continuation placeholder preserved at x=1 after write, got %#v", got)
	}
	if got := screen.Cells[0][2]; got.Content != "好" || got.Width != 2 {
		t.Fatalf("expected second wide cell preserved after write, got %#v", got)
	}
	if got := screen.Cells[0][3]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected continuation placeholder preserved at x=3 after write, got %#v", got)
	}
	if got := screen.Cells[0][5]; got.Content != "!" || got.Width != 1 {
		t.Fatalf("expected trailing ASCII write after restored wide cells, got %#v", got)
	}
}

func TestVTermResizePreservesCurrentBackgroundForSubsequentErase(t *testing.T) {
	const bg = "#000000"
	vt := New(120, 4, 100, nil)

	seed := "\x1b[?1049h" +
		"\x1b[48;2;0;0;0m" +
		"\x1b[2;1Hline 001 some content" +
		"\x1b[K"
	if _, err := vt.Write([]byte(seed)); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	if got := vt.ScreenRowView(1)[60].Style.BG; got != bg {
		t.Fatalf("expected seeded tail bg %q before resize, got %#v", bg, vt.ScreenRowView(1)[60])
	}

	vt.Resize(96, 4)

	if _, err := vt.Write([]byte("\x1b[2;1Hline 001 some content\x1b[K")); err != nil {
		t.Fatalf("post-resize erase write: %v", err)
	}

	screen := vt.ScreenContent()
	if len(screen.Cells) <= 1 || len(screen.Cells[1]) < 61 {
		t.Fatalf("unexpected screen dimensions after resize+erase: %#v", screen.Cells)
	}
	if got := screen.Cells[1][60].Style.BG; got != bg {
		t.Fatalf("expected erase after resize to keep bg %q, got %#v", bg, screen.Cells[1][60])
	}
}

func TestLoadSnapshotWithTimestampsRestoresRowTimes(t *testing.T) {
	vt := New(6, 3, 100, nil)
	scrollbackTS := []time.Time{time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)}
	screenTS := []time.Time{time.Date(2026, 4, 7, 10, 0, 1, 0, time.UTC)}

	vt.LoadSnapshotWithTimestamps([][]Cell{
		{{Content: "o", Width: 1}, {Content: "l", Width: 1}, {Content: "d", Width: 1}},
	}, scrollbackTS, ScreenData{
		Cells: [][]Cell{
			{
				{Content: "n", Width: 1},
				{Content: "e", Width: 1},
				{Content: "w", Width: 1},
			},
		},
	}, screenTS, CursorState{Row: 0, Col: 3, Visible: true}, TerminalModes{AutoWrap: true})

	if got := vt.ScrollbackTimestamps(); len(got) != 1 || !got[0].Equal(scrollbackTS[0]) {
		t.Fatalf("unexpected restored scrollback timestamps: %#v", got)
	}
	if got := vt.ScreenTimestamps(); len(got) == 0 || !got[0].Equal(screenTS[0]) {
		t.Fatalf("unexpected restored screen timestamps: %#v", got)
	}
}

func TestLoadSnapshotWithMetadataRestoresRowKinds(t *testing.T) {
	vt := New(6, 3, 100, nil)

	vt.LoadSnapshotWithMetadata([][]Cell{{}}, []time.Time{time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)}, []string{"restart"}, ScreenData{
		Cells: [][]Cell{
			{
				{Content: "n", Width: 1},
				{Content: "e", Width: 1},
				{Content: "w", Width: 1},
			},
		},
	}, []time.Time{time.Date(2026, 4, 7, 10, 0, 1, 0, time.UTC)}, []string{""}, CursorState{Row: 0, Col: 3, Visible: true}, TerminalModes{AutoWrap: true})

	if got := vt.ScrollbackRowKinds(); len(got) != 1 || got[0] != "restart" {
		t.Fatalf("unexpected restored scrollback row kinds: %#v", got)
	}
}

func TestVTermWriteAssignsRowTimestamps(t *testing.T) {
	vt := New(6, 2, 100, nil)

	if _, err := vt.Write([]byte("one\ntwo\nthree\n")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	scrollbackTS := vt.ScrollbackTimestamps()
	if len(scrollbackTS) == 0 || scrollbackTS[0].IsZero() {
		t.Fatalf("expected scrollback timestamp after scroll, got %#v", scrollbackTS)
	}
	screenTS := vt.ScreenTimestamps()
	if len(screenTS) == 0 || screenTS[0].IsZero() {
		t.Fatalf("expected screen timestamps for visible rows, got %#v", screenTS)
	}
}

func TestVTermWriteWithDamagePreservesWideRuneContinuations(t *testing.T) {
	vt := New(8, 2, 100, nil)

	_, err, damage := vt.WriteWithDamage([]byte("你a"))
	if err != nil {
		t.Fatalf("write wide rune: %v", err)
	}
	op := firstWriteSpanOp(t, damage)
	if op.Row != 0 || op.Col != 0 {
		t.Fatalf("expected first-row write op, got %#v", op)
	}
	if got := op.Cells[0]; got.Content != "你" || got.Width != 2 {
		t.Fatalf("expected wide rune anchor cell, got %#v", got)
	}
	if got := op.Cells[1]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected wide rune continuation placeholder, got %#v", got)
	}
	if got := op.Cells[2]; got.Content != "a" || got.Width != 1 {
		t.Fatalf("expected trailing ascii cell, got %#v", got)
	}
}

func TestVTermWriteWithDamageNormalizesCombiningRuneClusters(t *testing.T) {
	vt := New(8, 2, 100, nil)

	_, err, damage := vt.WriteWithDamage([]byte("e\u0301"))
	if err != nil {
		t.Fatalf("write combining rune: %v", err)
	}
	op := firstWriteSpanOp(t, damage)
	if got := op.Cells[0]; got.Content != "é" || got.Width != 1 {
		t.Fatalf("expected normalized grapheme cell, got %#v", got)
	}
	screen := vt.ScreenContent()
	if got := screen.Cells[0][0]; got.Content != "é" || got.Width != 1 {
		t.Fatalf("expected screen to keep normalized grapheme cell, got %#v", got)
	}
}

func TestVTermWriteWithDamageProducesSingleHighColumnSpan(t *testing.T) {
	vt := New(160, 1, 100, nil)

	_, err, damage := vt.WriteWithDamage([]byte("\x1b[1;138HZ"))
	if err != nil {
		t.Fatalf("write high-column edit: %v", err)
	}
	op := firstWriteSpanOp(t, damage)
	if op.Row != 0 || op.Col != 137 {
		t.Fatalf("unexpected high-column op metadata: %#v", op)
	}
	if len(op.Cells) != 1 || op.Cells[0].Content != "Z" {
		t.Fatalf("expected single-cell sparse op, got %#v", op.Cells)
	}
}

func TestVTermWriteWithDamageUsesClearToEOLForTailClear(t *testing.T) {
	vt := New(24, 1, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{{
			{Content: "p", Width: 1},
			{Content: "r", Width: 1},
			{Content: "e", Width: 1},
			{Content: "f", Width: 1},
			{Content: "i", Width: 1},
			{Content: "x", Width: 1},
			{Content: "X", Width: 1},
			{Content: "Y", Width: 1},
			{Content: "Z", Width: 1},
		}},
	}, CursorState{Row: 0, Col: 9, Visible: true}, TerminalModes{AutoWrap: true})

	_, err, damage := vt.WriteWithDamage([]byte("\x1b[1;7H\x1b[K"))
	if err != nil {
		t.Fatalf("clear to eol: %v", err)
	}
	op := firstOpWithCode(t, damage, ScreenOpClearToEOL)
	if op.Row != 0 || op.Col != 6 {
		t.Fatalf("unexpected clear-to-eol op: %#v", op)
	}
}

func TestVTermWriteWithDamagePreservesStyledEraseAsSpan(t *testing.T) {
	const bg = "#222222"
	vt := New(12, 2, 100, nil)

	_, err, damage := vt.WriteWithDamage([]byte("\x1b[48;2;34;34;34m\x1b[1;1H\x1b[K"))
	if err != nil {
		t.Fatalf("styled erase: %v", err)
	}
	op := firstWriteSpanOp(t, damage)
	if op.Row != 0 || op.Col != 0 || len(op.Cells) != 12 {
		t.Fatalf("expected full-row styled erase span, got %#v", op)
	}
	for i, cell := range op.Cells {
		if cell.Content != " " || cell.Width != 1 || cell.Style.BG != bg {
			t.Fatalf("expected styled blank cell %d with bg %q, got %#v", i, bg, cell)
		}
	}
	if got := vt.ScreenRowView(0)[11].Style.BG; got != bg {
		t.Fatalf("expected screen tail bg %q, got %#v", bg, vt.ScreenRowView(0)[11])
	}
}

func TestVTermWriteWithDamageCapturesMidRowStyleOnlySpan(t *testing.T) {
	vt := New(24, 1, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{{
			{Content: "p", Width: 1},
			{Content: "l", Width: 1},
			{Content: "a", Width: 1},
			{Content: "i", Width: 1},
			{Content: "n", Width: 1},
			{Content: "x", Width: 1},
			{Content: "t", Width: 1},
		}},
	}, CursorState{Row: 0, Col: 7, Visible: true}, TerminalModes{AutoWrap: true})

	_, err, damage := vt.WriteWithDamage([]byte("\x1b[1;6H\x1b[1mx\x1b[0m"))
	if err != nil {
		t.Fatalf("style-only write: %v", err)
	}
	op := firstWriteSpanOp(t, damage)
	if op.Col != 5 || len(op.Cells) != 1 {
		t.Fatalf("unexpected style-only op window: %#v", op)
	}
	if got := op.Cells[0]; got.Content != "x" || !got.Style.Bold {
		t.Fatalf("expected bold style-only cell, got %#v", got)
	}
}

func TestVTermWriteWithDamageKeepsWideCharSpanBoundaryStable(t *testing.T) {
	vt := New(8, 1, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{{
			{Content: "你", Width: 2},
			{Content: "", Width: 0},
			{Content: "a", Width: 1},
		}},
	}, CursorState{Row: 0, Col: 3, Visible: true}, TerminalModes{AutoWrap: true})

	_, err, damage := vt.WriteWithDamage([]byte("\x1b[1;1H界"))
	if err != nil {
		t.Fatalf("wide-boundary write: %v", err)
	}
	op := firstWriteSpanOp(t, damage)
	if op.Col != 0 || len(op.Cells) != 2 {
		t.Fatalf("expected op expanded to include wide continuation, got %#v", op)
	}
	if got := op.Cells[0]; got.Content != "界" || got.Width != 2 {
		t.Fatalf("expected wide anchor cell, got %#v", got)
	}
	if got := op.Cells[1]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected continuation placeholder preserved, got %#v", got)
	}
}

func TestVTermWriteSelectivelyInvalidatesOnlyChangedScreenRows(t *testing.T) {
	vt := New(6, 2, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{
			{
				{Content: "t", Width: 1},
				{Content: "o", Width: 1},
				{Content: "p", Width: 1},
			},
			{
				{Content: "b", Width: 1},
				{Content: "o", Width: 1},
				{Content: "t", Width: 1},
			},
		},
	}, CursorState{Row: 1, Col: 0, Visible: true}, TerminalModes{AutoWrap: true})

	topBefore := vt.ScreenRowView(0)
	bottomBefore := vt.ScreenRowView(1)
	if len(topBefore) == 0 || len(bottomBefore) == 0 {
		t.Fatalf("expected cached rows, got top=%#v bottom=%#v", topBefore, bottomBefore)
	}

	if _, err := vt.Write([]byte("\x1b[2;1Hnew")); err != nil {
		t.Fatalf("write updated row: %v", err)
	}

	topAfter := vt.ScreenRowView(0)
	bottomAfter := vt.ScreenRowView(1)
	if strings.TrimSpace(rowToString(topAfter)) != "top" {
		t.Fatalf("expected unchanged top row preserved, got %q", rowToString(topAfter))
	}
	if strings.TrimSpace(rowToString(bottomAfter)) != "new" {
		t.Fatalf("expected updated bottom row, got %q", rowToString(bottomAfter))
	}
	if &topAfter[0] != &topBefore[0] {
		t.Fatal("expected unchanged screen row cache to be reused")
	}
	if &bottomAfter[0] == &bottomBefore[0] {
		t.Fatal("expected changed screen row cache to be invalidated")
	}
}

func TestVTermWriteReusesScrolledCachesAcrossScreenAndScrollback(t *testing.T) {
	vt := New(6, 2, 100, nil)
	vt.LoadSnapshotWithScrollback([][]Cell{
		{
			{Content: "h", Width: 1},
			{Content: "i", Width: 1},
			{Content: "s", Width: 1},
			{Content: "t", Width: 1},
		},
	}, ScreenData{
		Cells: [][]Cell{
			{
				{Content: "t", Width: 1},
				{Content: "o", Width: 1},
				{Content: "p", Width: 1},
			},
			{
				{Content: "b", Width: 1},
				{Content: "o", Width: 1},
				{Content: "t", Width: 1},
			},
		},
	}, CursorState{Row: 1, Col: 0, Visible: true}, TerminalModes{AutoWrap: true})

	scrollbackBefore := vt.ScrollbackRowView(0)
	topBefore := vt.ScreenRowView(0)
	bottomBefore := vt.ScreenRowView(1)
	if len(scrollbackBefore) == 0 || len(topBefore) == 0 || len(bottomBefore) == 0 {
		t.Fatalf("expected primed caches, got scrollback=%#v top=%#v bottom=%#v", scrollbackBefore, topBefore, bottomBefore)
	}

	if _, err := vt.Write([]byte("\n")); err != nil {
		t.Fatalf("scroll write: %v", err)
	}

	scrollbackAfter0 := vt.ScrollbackRowView(0)
	scrollbackAfter1 := vt.ScrollbackRowView(1)
	screenAfter0 := vt.ScreenRowView(0)
	screenAfter1 := vt.ScreenRowView(1)
	if strings.TrimSpace(rowToString(scrollbackAfter0)) == "" {
		t.Fatalf("expected existing scrollback row preserved, got %q", rowToString(scrollbackAfter0))
	}
	if strings.TrimSpace(rowToString(scrollbackAfter1)) != "top" {
		t.Fatalf("expected scrolled-off top row in scrollback, got %q", rowToString(scrollbackAfter1))
	}
	if strings.TrimSpace(rowToString(screenAfter0)) != "bot" {
		t.Fatalf("expected bottom row to move into first screen row, got %q", rowToString(screenAfter0))
	}
	if &scrollbackAfter0[0] != &scrollbackBefore[0] {
		t.Fatal("expected retained scrollback cache to be reused")
	}
	if &scrollbackAfter1[0] != &topBefore[0] {
		t.Fatal("expected scrolled-off screen row cache to move into scrollback cache")
	}
	if &screenAfter0[0] != &bottomBefore[0] {
		t.Fatal("expected shifted screen row cache to be reused")
	}
	if len(screenAfter1) == 0 || &screenAfter1[0] == &topBefore[0] {
		t.Fatal("expected newly blank screen row to allocate a fresh cache")
	}
}

func TestVTermWriteAssignsTimestampsToBlankRows(t *testing.T) {
	vt := New(6, 3, 100, nil)

	if _, err := vt.Write([]byte("\n")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	screenTS := vt.ScreenTimestamps()
	if len(screenTS) < 2 || screenTS[0].IsZero() || screenTS[1].IsZero() {
		t.Fatalf("expected blank rows created by newline to receive timestamps, got %#v", screenTS)
	}
}

func TestVTermWriteAltScreenScrollDoesNotInvalidateWholeScreen(t *testing.T) {
	vt := New(5, 4, 100, nil)
	vt.LoadSnapshot(ScreenData{
		IsAlternateScreen: true,
		Cells: [][]Cell{
			{{Content: "1", Width: 1}},
			{{Content: "2", Width: 1}},
			{{Content: "3", Width: 1}},
			{{Content: "4", Width: 1}},
		},
	}, CursorState{Row: 3, Col: 0, Visible: true}, TerminalModes{AlternateScreen: true, AutoWrap: true})

	_, err, damage := vt.WriteWithDamage([]byte("\n"))
	if err != nil {
		t.Fatalf("alt-screen scroll write: %v", err)
	}
	if damage.RequiresFullReplace {
		t.Fatalf("expected direct scroll ops instead of full replace, got %#v", damage)
	}
	cols, rows := vt.Size()
	foundScroll := false
	for _, op := range damage.Ops {
		if op.Code != ScreenOpScrollRect {
			continue
		}
		foundScroll = true
		if op.Rect.Width != cols || op.Rect.Height != rows || op.Dy != -1 {
			t.Fatalf("unexpected direct scroll op: %#v", op)
		}
	}
	if !foundScroll {
		t.Fatalf("expected direct scroll op from local x/vt damage stream, got %#v", damage.Ops)
	}
}

func TestVTermWriteWithDamageUsesDirectSpanOps(t *testing.T) {
	vt := New(8, 2, 100, nil)

	_, err, damage := vt.WriteWithDamage([]byte("abc"))
	if err != nil {
		t.Fatalf("write with damage: %v", err)
	}
	if len(damage.Ops) == 0 {
		t.Fatalf("expected direct span ops, got %#v", damage)
	}
	span := damage.Ops[0]
	if span.Code != ScreenOpWriteSpan || span.Row != 0 || span.Col != 0 {
		t.Fatalf("unexpected first direct op: %#v", span)
	}
	if got := span.Cells[0].Content + span.Cells[1].Content + span.Cells[2].Content; got != "abc" {
		t.Fatalf("unexpected direct span contents: %#v", span.Cells)
	}
}

func TestVTermWriteWithDamageBroadDirectSpanUsesFullReplace(t *testing.T) {
	vt := New(80, 24, 100, nil)
	prevWithDamage := safeEmulatorWriteWithDamage
	safeEmulatorWriteWithDamage = func(emu *charmvt.SafeEmulator, data []byte) (int, error, []charmvt.Damage, bool) {
		n, err := emu.Write(data)
		cells := make([]uv.Cell, emu.Width())
		for col := range cells {
			cells[col] = uv.Cell{Content: "x", Width: 1}
		}
		damages := make([]charmvt.Damage, emu.Height())
		for row := 0; row < emu.Height(); row++ {
			damages[row] = charmvt.SpanDamage{X: 0, Y: row, Cells: cells}
		}
		return n, err, damages, true
	}
	t.Cleanup(func() {
		safeEmulatorWriteWithDamage = prevWithDamage
	})

	_, err, damage := vt.WriteWithDamage([]byte("x"))
	if err != nil {
		t.Fatalf("write with damage: %v", err)
	}
	if !damage.RequiresFullReplace {
		t.Fatalf("expected broad direct span damage to use full replace, got %#v", damage)
	}
	if damage.FullReplaceReason != "broad_direct_cell_damage" {
		t.Fatalf("unexpected full replace reason %q", damage.FullReplaceReason)
	}
	if len(damage.Ops) != 0 {
		t.Fatalf("expected no expanded span ops for broad damage, got %#v", damage.Ops)
	}
	if damage.DirectDamageItems != 24 || damage.DirectDamageRows != 24 || damage.DirectDamageCells != 1920 {
		t.Fatalf("unexpected direct damage stats: items=%d rows=%d cells=%d", damage.DirectDamageItems, damage.DirectDamageRows, damage.DirectDamageCells)
	}
}

func TestVTermWriteWithDamageRepeatedDirectSpanUsesFullReplace(t *testing.T) {
	vt := New(146, 73, 100, nil)
	prevWithDamage := safeEmulatorWriteWithDamage
	safeEmulatorWriteWithDamage = func(emu *charmvt.SafeEmulator, data []byte) (int, error, []charmvt.Damage, bool) {
		n, err := emu.Write(data)
		cells := []uv.Cell{{Content: "x", Width: 1}}
		damages := make([]charmvt.Damage, 0, 6000)
		for col := 0; col < 6000; col++ {
			damages = append(damages, charmvt.SpanDamage{X: col % emu.Width(), Y: 2, Cells: cells})
		}
		return n, err, damages, true
	}
	t.Cleanup(func() {
		safeEmulatorWriteWithDamage = prevWithDamage
	})

	_, err, damage := vt.WriteWithDamage([]byte("x"))
	if err != nil {
		t.Fatalf("write with damage: %v", err)
	}
	if !damage.RequiresFullReplace {
		t.Fatalf("expected repeated direct span damage to use full replace, got %#v", damage)
	}
	if damage.FullReplaceReason != "repeated_direct_damage" {
		t.Fatalf("unexpected full replace reason %q", damage.FullReplaceReason)
	}
	if len(damage.Ops) != 0 {
		t.Fatalf("expected no expanded span ops for repeated damage, got %#v", damage.Ops)
	}
	if damage.DirectDamageItems != 6000 || damage.DirectDamageRows != 1 || damage.DirectDamageCells != 6000 {
		t.Fatalf("unexpected direct damage stats: items=%d rows=%d cells=%d", damage.DirectDamageItems, damage.DirectDamageRows, damage.DirectDamageCells)
	}
}

func TestVTermWriteWithDamageKeepsScrollDirectDamageIncremental(t *testing.T) {
	vt := New(10, 4, 100, nil)
	prevWithDamage := safeEmulatorWriteWithDamage
	safeEmulatorWriteWithDamage = func(emu *charmvt.SafeEmulator, data []byte) (int, error, []charmvt.Damage, bool) {
		n, err := emu.Write(data)
		cells := make([]uv.Cell, emu.Width())
		for col := range cells {
			cells[col] = uv.Cell{Content: "x", Width: 1}
		}
		damages := []charmvt.Damage{
			charmvt.ScrollDamage{Rectangle: uv.Rect(0, 0, emu.Width(), emu.Height()), Dy: -1},
			charmvt.SpanDamage{X: 0, Y: emu.Height() - 1, Cells: cells},
		}
		return n, err, damages, true
	}
	t.Cleanup(func() {
		safeEmulatorWriteWithDamage = prevWithDamage
	})

	_, err, damage := vt.WriteWithDamage([]byte("\n"))
	if err != nil {
		t.Fatalf("write with damage: %v", err)
	}
	if damage.RequiresFullReplace {
		t.Fatalf("expected scroll damage to remain incremental, got %#v", damage)
	}
	if damage.DirectDamageItems != 2 || damage.DirectDamageRows != 1 || damage.DirectDamageCells != 10 {
		t.Fatalf("unexpected direct damage stats: items=%d rows=%d cells=%d", damage.DirectDamageItems, damage.DirectDamageRows, damage.DirectDamageCells)
	}
	foundScroll := false
	for _, op := range damage.Ops {
		if op.Code == ScreenOpScrollRect {
			foundScroll = true
		}
	}
	if !foundScroll {
		t.Fatalf("expected scroll op to be preserved, got %#v", damage.Ops)
	}
}

func TestVTermWriteAltScreenSwitchKeepsDamageCorrect(t *testing.T) {
	vt := New(5, 3, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{
			{{Content: "a", Width: 1}},
			{{Content: "b", Width: 1}},
			{{Content: "c", Width: 1}},
		},
	}, CursorState{Row: 2, Col: 1, Visible: true}, TerminalModes{AutoWrap: true})

	_, err, damage := vt.WriteWithDamage([]byte("\x1b[?1049h"))
	if err != nil {
		t.Fatalf("enter alt-screen: %v", err)
	}
	if !vt.IsAltScreen() {
		t.Fatal("expected alt-screen to be enabled")
	}
	if !damage.RequiresFullReplace {
		t.Fatalf("expected full replace damage on alt-screen switch, got %#v", damage)
	}
	screen := vt.ScreenContent()
	for row := range screen.Cells {
		if strings.TrimSpace(rowToString(screen.Cells[row])) != "" {
			t.Fatalf("expected blank alt-screen row %d, got %q", row, rowToString(screen.Cells[row]))
		}
	}

	_, err, damage = vt.WriteWithDamage([]byte("\x1b[?1049l"))
	if err != nil {
		t.Fatalf("leave alt-screen: %v", err)
	}
	if vt.IsAltScreen() {
		t.Fatal("expected alt-screen to be disabled")
	}
	if !damage.RequiresFullReplace {
		t.Fatalf("expected full replace damage when restoring main screen, got %#v", damage)
	}
	screen = vt.ScreenContent()
	if got := strings.TrimSpace(rowToString(screen.Cells[0])) + strings.TrimSpace(rowToString(screen.Cells[1])) + strings.TrimSpace(rowToString(screen.Cells[2])); got != "abc" {
		t.Fatalf("expected main-screen content restored, got %q", got)
	}
}

func TestVTermTracksMouseModesFromEscapeSequences(t *testing.T) {
	vt := New(20, 5, 100, nil)

	if vt.Modes().MouseTracking {
		t.Fatal("expected mouse tracking disabled by default")
	}
	if _, err := vt.Write([]byte("\x1b[?1002h\x1b[?1006h")); err != nil {
		t.Fatalf("enable mouse tracking failed: %v", err)
	}
	if !vt.Modes().MouseTracking {
		t.Fatal("expected mouse tracking after enabling button-event mode")
	}
	if !vt.Modes().MouseButtonEvent || !vt.Modes().MouseSGR {
		t.Fatalf("expected button-event+SGR mouse mode flags, got %#v", vt.Modes())
	}
	if _, err := vt.Write([]byte("\x1b[?1006l")); err != nil {
		t.Fatalf("disable sgr mode failed: %v", err)
	}
	if !vt.Modes().MouseTracking {
		t.Fatal("expected mouse tracking to remain enabled after disabling sgr encoding only")
	}
	if vt.Modes().MouseSGR {
		t.Fatalf("expected SGR flag cleared after disabling 1006, got %#v", vt.Modes())
	}
	if _, err := vt.Write([]byte("\x1b[?1002l")); err != nil {
		t.Fatalf("disable mouse tracking failed: %v", err)
	}
	if vt.Modes().MouseTracking {
		t.Fatal("expected mouse tracking disabled after reset")
	}
}

func TestLoadSnapshotRestoresMouseTrackingMode(t *testing.T) {
	vt := New(10, 4, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{{{Content: "x", Width: 1}}},
	}, CursorState{Row: 0, Col: 1, Visible: true}, TerminalModes{AutoWrap: true, MouseTracking: true, MouseNormal: true})

	if !vt.Modes().MouseTracking {
		t.Fatal("expected snapshot restore to preserve mouse tracking")
	}
	if !vt.Modes().MouseNormal || vt.Modes().MouseSGR {
		t.Fatalf("expected snapshot restore to preserve legacy mouse encoding, got %#v", vt.Modes())
	}
}

func TestLoadSnapshotRestoresSGRMouseEncodingMode(t *testing.T) {
	vt := New(10, 4, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{{{Content: "x", Width: 1}}},
	}, CursorState{Row: 0, Col: 1, Visible: true}, TerminalModes{AutoWrap: true, MouseTracking: true, MouseButtonEvent: true, MouseSGR: true})

	if !vt.Modes().MouseTracking || !vt.Modes().MouseButtonEvent || !vt.Modes().MouseSGR {
		t.Fatalf("expected snapshot restore to preserve SGR mouse encoding, got %#v", vt.Modes())
	}
}

func TestVTermTracksAlternateScrollModeFromEscapeSequences(t *testing.T) {
	vt := New(20, 5, 100, nil)

	if vt.Modes().AlternateScroll {
		t.Fatal("expected alternate scroll disabled by default")
	}
	if _, err := vt.Write([]byte("\x1b[?1007h")); err != nil {
		t.Fatalf("enable alternate scroll failed: %v", err)
	}
	if !vt.Modes().AlternateScroll {
		t.Fatal("expected alternate scroll enabled after escape sequence")
	}
	if _, err := vt.Write([]byte("\x1b[?1007l")); err != nil {
		t.Fatalf("disable alternate scroll failed: %v", err)
	}
	if vt.Modes().AlternateScroll {
		t.Fatal("expected alternate scroll disabled after reset")
	}
}

func TestLoadSnapshotRestoresAlternateScrollMode(t *testing.T) {
	vt := New(10, 4, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{{{Content: "x", Width: 1}}},
	}, CursorState{Row: 0, Col: 1, Visible: true}, TerminalModes{AutoWrap: true, AlternateScroll: true})

	if !vt.Modes().AlternateScroll {
		t.Fatal("expected snapshot restore to preserve alternate scroll")
	}
}

func TestApplyScreenUpdateAppliesWriteSpanInPlace(t *testing.T) {
	vt := New(6, 3, 100, nil)
	now := time.Date(2026, 4, 18, 8, 0, 0, 0, time.UTC)
	vt.LoadSnapshotWithMetadata(nil, nil, nil, ScreenData{
		Cells: [][]Cell{
			{
				{Content: "o", Width: 1},
				{Content: "l", Width: 1},
				{Content: "d", Width: 1},
				{Content: " ", Width: 1},
			},
			{
				{Content: "r", Width: 1},
				{Content: "o", Width: 1},
				{Content: "w", Width: 1},
				{Content: " ", Width: 1},
			},
		},
		IsAlternateScreen: true,
	}, []time.Time{now, now}, []string{"old-0", "old-1"}, CursorState{Row: 1, Col: 3, Visible: true}, TerminalModes{AlternateScreen: true, AutoWrap: true})

	oldEmu := vt.emu
	if !vt.ApplyScreenUpdate(ScreenUpdate{
		Size: Size{Cols: 4, Rows: 2},
		Ops: []DamageOp{{
			Code: ScreenOpWriteSpan,
			Row:  1,
			Col:  0,
			Cells: []Cell{
				{Content: "n", Width: 1},
				{Content: "e", Width: 1},
				{Content: "w", Width: 1},
				{Content: "!", Width: 1},
			},
			Timestamp: now.Add(time.Second),
			RowKind:   "new-1",
		}},
		Cursor: CursorState{Row: 1, Col: 4, Visible: true, Shape: "bar"},
		Modes:  TerminalModes{AlternateScreen: true, AutoWrap: true},
	}) {
		t.Fatal("expected incremental screen update to apply")
	}

	if vt.emu != oldEmu {
		t.Fatal("expected incremental apply to keep the existing emulator instance")
	}
	screen := vt.ScreenContent()
	if got := screen.Cells[1][0].Content + screen.Cells[1][1].Content + screen.Cells[1][2].Content + screen.Cells[1][3].Content; got != "new!" {
		t.Fatalf("expected updated row content, got %q", got)
	}
	if got := vt.ScreenRowTimestampAt(1); !got.Equal(now.Add(time.Second)) {
		t.Fatalf("expected updated row timestamp, got %v", got)
	}
	if got := vt.ScreenRowKindAt(1); got != "new-1" {
		t.Fatalf("expected updated row kind, got %q", got)
	}
	if cursor := vt.CursorState(); cursor.Row != 1 || cursor.Col != 4 || cursor.Shape != CursorBar {
		t.Fatalf("expected updated cursor, got %#v", cursor)
	}
}

func TestApplyScreenUpdateAppliesClearToEOLOp(t *testing.T) {
	vt := New(8, 1, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{{
			{Content: "p", Width: 1},
			{Content: "r", Width: 1},
			{Content: "e", Width: 1},
			{Content: "f", Width: 1},
			{Content: "i", Width: 1},
			{Content: "x", Width: 1},
			{Content: "X", Width: 1},
			{Content: "Y", Width: 1},
		}},
		IsAlternateScreen: true,
	}, CursorState{Row: 0, Col: 8, Visible: true}, TerminalModes{AlternateScreen: true, AutoWrap: true})

	if !vt.ApplyScreenUpdate(ScreenUpdate{
		Size: Size{Cols: 8, Rows: 1},
		Ops: []DamageOp{{
			Code: ScreenOpClearToEOL,
			Row:  0,
			Col:  6,
		}},
		Cursor: CursorState{Row: 0, Col: 6, Visible: true},
		Modes:  TerminalModes{AlternateScreen: true, AutoWrap: true},
	}) {
		t.Fatal("expected clear-to-eol span to apply incrementally")
	}

	row := vt.ScreenRowView(0)
	if got := strings.TrimRight(rowToString(row), " "); got != "prefix" {
		t.Fatalf("expected row tail cleared, got %q", rowToString(row))
	}
}

func TestApplyScreenUpdateAppliesStyleOnlyWriteOp(t *testing.T) {
	vt := New(8, 1, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{{
			{Content: "p", Width: 1},
			{Content: "l", Width: 1},
			{Content: "a", Width: 1},
			{Content: "i", Width: 1},
			{Content: "n", Width: 1},
			{Content: "x", Width: 1},
		}},
		IsAlternateScreen: true,
	}, CursorState{Row: 0, Col: 6, Visible: true}, TerminalModes{AlternateScreen: true, AutoWrap: true})

	if !vt.ApplyScreenUpdate(ScreenUpdate{
		Size: Size{Cols: 8, Rows: 1},
		Ops: []DamageOp{{
			Code: ScreenOpWriteSpan,
			Row:  0,
			Col:  5,
			Cells: []Cell{{
				Content: "x",
				Width:   1,
				Style:   CellStyle{Bold: true},
			}},
			Timestamp: time.Date(2026, 4, 18, 8, 0, 2, 0, time.UTC),
			RowKind:   "style-only",
		}},
		Cursor: CursorState{Row: 0, Col: 6, Visible: true},
		Modes:  TerminalModes{AlternateScreen: true, AutoWrap: true},
	}) {
		t.Fatal("expected style-only span to apply incrementally")
	}

	cell := vt.ScreenRowView(0)[5]
	if cell.Content != "x" || !cell.Style.Bold {
		t.Fatalf("expected bold cell after style-only span, got %#v", cell)
	}
	if got := vt.ScreenRowKindAt(0); got != "style-only" {
		t.Fatalf("expected row kind updated by style-only span, got %q", got)
	}
}

func TestVTermPreservesHyperlinkAcrossSnapshotAndScreenUpdate(t *testing.T) {
	vt := New(8, 1, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{{
			{Content: "l", Width: 1, LinkURL: "https://example.test/snapshot", LinkParams: "id=snapshot"},
			{Content: "i", Width: 1, LinkURL: "https://example.test/snapshot", LinkParams: "id=snapshot"},
			{Content: "n", Width: 1},
		}},
	}, CursorState{Row: 0, Col: 3, Visible: true}, TerminalModes{AutoWrap: true})

	if got := vt.ScreenRowView(0)[0]; got.LinkURL != "https://example.test/snapshot" || got.LinkParams != "id=snapshot" {
		t.Fatalf("expected snapshot hyperlink restored, got %#v", got)
	}

	if !vt.ApplyScreenUpdate(ScreenUpdate{
		Size: Size{Cols: 8, Rows: 1},
		Ops: []DamageOp{{
			Code: ScreenOpWriteSpan,
			Row:  0,
			Col:  2,
			Cells: []Cell{{
				Content:    "k",
				Width:      1,
				LinkURL:    "https://example.test/update",
				LinkParams: "id=update",
			}},
		}},
		Cursor: CursorState{Row: 0, Col: 3, Visible: true},
		Modes:  TerminalModes{AutoWrap: true},
	}) {
		t.Fatal("expected hyperlink span to apply incrementally")
	}

	cell := vt.ScreenRowView(0)[2]
	if cell.Content != "k" || cell.LinkURL != "https://example.test/update" || cell.LinkParams != "id=update" {
		t.Fatalf("expected updated hyperlink cell, got %#v", cell)
	}
}

func TestVTermReplayResetsHyperlinkBeforePlainText(t *testing.T) {
	var b strings.Builder
	writeSequentialRows(&b, [][]Cell{{
		{Content: "l", Width: 1, LinkURL: "https://example.test"},
		{Content: "p", Width: 1},
	}})

	got := b.String()
	if !strings.Contains(got, "\x1b]8;;https://example.test\x07") {
		t.Fatalf("expected replay to set hyperlink, got %q", got)
	}
	plainIndex := strings.Index(got, "p")
	resetIndex := strings.LastIndex(got[:plainIndex], "\x1b]8;;\x07")
	if resetIndex < 0 {
		t.Fatalf("expected hyperlink reset before plain cell, got %q", got)
	}

	b.Reset()
	writeSequentialRows(&b, [][]Cell{{
		{Content: "l", Width: 1, LinkURL: "https://example.test"},
	}})
	got = b.String()
	if !strings.HasSuffix(got, "\x1b]8;;\x07\x1b[0m") {
		t.Fatalf("expected final replay reset to close hyperlink state, got %q", got)
	}
}

func TestVTermReplayPreservesTrailingStyledAndLinkedBlanks(t *testing.T) {
	var b strings.Builder
	writeSequentialRows(&b, [][]Cell{{
		{Content: " ", Width: 1, Style: CellStyle{BG: "#222222"}},
		{Content: " ", Width: 1, LinkURL: "https://example.test/tail"},
	}})

	got := b.String()
	if !strings.Contains(got, "48;2;34;34;34") {
		t.Fatalf("expected trailing styled blank to be replayed, got %q", got)
	}
	if !strings.Contains(got, "\x1b]8;;https://example.test/tail\x07") {
		t.Fatalf("expected trailing linked blank to be replayed, got %q", got)
	}
}

func TestApplyScreenUpdateAppliesWideCharBoundaryWriteOp(t *testing.T) {
	vt := New(8, 1, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{{
			{Content: "你", Width: 2},
			{Content: "", Width: 0},
			{Content: "a", Width: 1},
		}},
		IsAlternateScreen: true,
	}, CursorState{Row: 0, Col: 3, Visible: true}, TerminalModes{AlternateScreen: true, AutoWrap: true})

	if !vt.ApplyScreenUpdate(ScreenUpdate{
		Size: Size{Cols: 8, Rows: 1},
		Ops: []DamageOp{{
			Code: ScreenOpWriteSpan,
			Row:  0,
			Col:  0,
			Cells: []Cell{
				{Content: "界", Width: 2},
				{Content: "", Width: 0},
			},
		}},
		Cursor: CursorState{Row: 0, Col: 3, Visible: true},
		Modes:  TerminalModes{AlternateScreen: true, AutoWrap: true},
	}) {
		t.Fatal("expected wide-boundary span to apply incrementally")
	}

	row := vt.ScreenRowView(0)
	if got := row[0]; got.Content != "界" || got.Width != 2 {
		t.Fatalf("expected wide anchor updated, got %#v", got)
	}
	if got := row[1]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected wide continuation preserved, got %#v", got)
	}
}

func TestApplyScreenUpdateAppliesOpcodeScrollRect(t *testing.T) {
	vt := New(4, 4, 100, nil)
	now := time.Date(2026, 4, 18, 8, 0, 4, 0, time.UTC)
	vt.LoadSnapshotWithMetadata(nil, nil, nil, ScreenData{
		Cells: [][]Cell{
			{{Content: "r", Width: 1}, {Content: "o", Width: 1}, {Content: "w", Width: 1}, {Content: "1", Width: 1}},
			{{Content: "r", Width: 1}, {Content: "o", Width: 1}, {Content: "w", Width: 1}, {Content: "2", Width: 1}},
			{{Content: "r", Width: 1}, {Content: "o", Width: 1}, {Content: "w", Width: 1}, {Content: "3", Width: 1}},
			{{Content: "r", Width: 1}, {Content: "o", Width: 1}, {Content: "w", Width: 1}, {Content: "4", Width: 1}},
		},
		IsAlternateScreen: true,
	}, []time.Time{now, now.Add(time.Second), now.Add(2 * time.Second), now.Add(3 * time.Second)}, []string{"a", "b", "c", "d"}, CursorState{Row: 3, Col: 0, Visible: true}, TerminalModes{AlternateScreen: true, AutoWrap: true})

	oldEmu := vt.emu
	if !vt.ApplyScreenUpdate(ScreenUpdate{
		Size:         Size{Cols: 4, Rows: 4},
		ScreenScroll: 1,
		Ops: []DamageOp{
			{Code: ScreenOpScrollRect, Rect: DamageRect{X: 0, Y: 0, Width: 4, Height: 4}, Dy: -1},
			{Code: ScreenOpWriteSpan, Row: 3, Col: 0, Cells: []Cell{{Content: "r", Width: 1}, {Content: "o", Width: 1}, {Content: "w", Width: 1}, {Content: "5", Width: 1}}, Timestamp: now.Add(4 * time.Second), RowKind: "e"},
		},
		Cursor: CursorState{Row: 3, Col: 0, Visible: true},
		Modes:  TerminalModes{AlternateScreen: true, AutoWrap: true},
	}) {
		t.Fatal("expected opcode scrollrect update to apply incrementally")
	}
	if vt.emu != oldEmu {
		t.Fatal("expected opcode incremental apply to keep emulator instance")
	}
	screen := vt.ScreenContent()
	got := []string{
		rowToString(screen.Cells[0]),
		rowToString(screen.Cells[1]),
		rowToString(screen.Cells[2]),
		rowToString(screen.Cells[3]),
	}
	if !reflect.DeepEqual(got, []string{"row2", "row3", "row4", "row5"}) {
		t.Fatalf("unexpected opcode scrollrect rows: %#v", got)
	}
	if rowKind := vt.ScreenRowKindAt(0); rowKind != "b" {
		t.Fatalf("expected shifted row kind on row 0, got %q", rowKind)
	}
	if rowKind := vt.ScreenRowKindAt(3); rowKind != "e" {
		t.Fatalf("expected tail write row kind on row 3, got %q", rowKind)
	}
}

func TestApplyScreenUpdateAppliesOpcodeCopyRect(t *testing.T) {
	vt := New(4, 3, 100, nil)
	now := time.Date(2026, 4, 18, 8, 0, 5, 0, time.UTC)
	vt.LoadSnapshotWithMetadata(nil, nil, nil, ScreenData{
		Cells: [][]Cell{
			{{Content: "A", Width: 1}, {Content: "B", Width: 1}, {Content: "C", Width: 1}, {Content: "D", Width: 1}},
			{{Content: "r", Width: 1}, {Content: "o", Width: 1}, {Content: "w", Width: 1}, {Content: "2", Width: 1}},
			{{Content: "r", Width: 1}, {Content: "o", Width: 1}, {Content: "w", Width: 1}, {Content: "3", Width: 1}},
		},
		IsAlternateScreen: true,
	}, []time.Time{now, now.Add(time.Second), now.Add(2 * time.Second)}, []string{"a", "b", "c"}, CursorState{Row: 2, Col: 0, Visible: true}, TerminalModes{AlternateScreen: true, AutoWrap: true})

	if !vt.ApplyScreenUpdate(ScreenUpdate{
		Size: Size{Cols: 4, Rows: 3},
		Ops: []DamageOp{
			{Code: ScreenOpCopyRect, Src: DamageRect{X: 0, Y: 0, Width: 4, Height: 1}, DstX: 0, DstY: 2},
		},
		Cursor: CursorState{Row: 2, Col: 0, Visible: true},
		Modes:  TerminalModes{AlternateScreen: true, AutoWrap: true},
	}) {
		t.Fatal("expected opcode copyrect update to apply incrementally")
	}

	screen := vt.ScreenContent()
	got := []string{
		rowToString(screen.Cells[0]),
		rowToString(screen.Cells[1]),
		rowToString(screen.Cells[2]),
	}
	if !reflect.DeepEqual(got, []string{"ABCD", "row2", "ABCD"}) {
		t.Fatalf("unexpected opcode copyrect rows: %#v", got)
	}
	if rowKind := vt.ScreenRowKindAt(2); rowKind != "a" {
		t.Fatalf("expected copied row kind on destination row, got %q", rowKind)
	}
}

func TestApplyScreenUpdateAppliesResizeThenSparseWriteOp(t *testing.T) {
	vt := New(4, 2, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{
			{
				{Content: "a", Width: 1},
				{Content: "b", Width: 1},
				{Content: "c", Width: 1},
				{Content: "d", Width: 1},
			},
		},
		IsAlternateScreen: true,
	}, CursorState{Row: 0, Col: 4, Visible: true}, TerminalModes{AlternateScreen: true, AutoWrap: true})

	if !vt.ApplyScreenUpdate(ScreenUpdate{
		Size: Size{Cols: 8, Rows: 3},
		Ops: []DamageOp{{
			Code: ScreenOpWriteSpan,
			Row:  2,
			Col:  5,
			Cells: []Cell{
				{Content: "o", Width: 1},
				{Content: "k", Width: 1},
			},
		}},
		Cursor: CursorState{Row: 2, Col: 7, Visible: true},
		Modes:  TerminalModes{AlternateScreen: true, AutoWrap: true},
	}) {
		t.Fatal("expected resize + sparse span update to apply incrementally")
	}

	if cols, rows := vt.Size(); cols != 8 || rows != 3 {
		t.Fatalf("expected resized terminal 8x3, got %dx%d", cols, rows)
	}
	row := vt.ScreenRowView(2)
	if got := row[5].Content + row[6].Content; got != "ok" {
		t.Fatalf("expected sparse span applied after resize, got %#v", row)
	}
}

func TestApplyScreenUpdateRejectsUnsupportedResetScrollback(t *testing.T) {
	vt := New(6, 3, 100, nil)
	vt.LoadSnapshotWithScrollback([][]Cell{{{Content: "o", Width: 1}}}, ScreenData{
		Cells: [][]Cell{{
			{Content: "n", Width: 1},
		}},
	}, CursorState{Row: 0, Col: 1, Visible: true}, TerminalModes{AutoWrap: true})

	oldEmu := vt.emu
	if vt.ApplyScreenUpdate(ScreenUpdate{
		Size:            Size{Cols: 1, Rows: 1},
		ResetScrollback: true,
		Cursor:          CursorState{Row: 0, Col: 1, Visible: true},
		Modes:           TerminalModes{AutoWrap: true},
	}) {
		t.Fatal("expected reset scrollback to fall back instead of partial apply")
	}
	if vt.emu != oldEmu {
		t.Fatal("expected rejected partial apply to leave emulator untouched")
	}
}

func TestApplyScreenUpdateAllowsSafeResizeWithoutRecreatingEmulator(t *testing.T) {
	vt := New(4, 2, 100, nil)
	now := time.Date(2026, 4, 18, 8, 30, 0, 0, time.UTC)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{
			{
				{Content: "a", Width: 1},
				{Content: "b", Width: 1},
				{Content: "c", Width: 1},
				{Content: "d", Width: 1},
			},
			{
				{Content: "1", Width: 1},
				{Content: "2", Width: 1},
				{Content: "3", Width: 1},
				{Content: "4", Width: 1},
			},
		},
		IsAlternateScreen: true,
	}, CursorState{Row: 1, Col: 3, Visible: true}, TerminalModes{AlternateScreen: true, AutoWrap: true})

	oldEmu := vt.emu
	if !vt.ApplyScreenUpdate(ScreenUpdate{
		Size: Size{Cols: 6, Rows: 3},
		Ops: []DamageOp{{
			Code: ScreenOpWriteSpan,
			Row:  2,
			Col:  0,
			Cells: []Cell{
				{Content: "x", Width: 1},
				{Content: "y", Width: 1},
			},
			Timestamp: now,
		}},
		Cursor: CursorState{Row: 2, Col: 2, Visible: true},
		Modes:  TerminalModes{AlternateScreen: true, AutoWrap: true},
	}) {
		t.Fatal("expected resize + changed rows update to apply incrementally")
	}
	if vt.emu != oldEmu {
		t.Fatal("expected resize path to keep the existing emulator instance")
	}
	if cols, rows := vt.Size(); cols != 6 || rows != 3 {
		t.Fatalf("expected resized terminal 6x3, got %dx%d", cols, rows)
	}
	screen := vt.ScreenContent()
	if got := screen.Cells[2][0].Content + screen.Cells[2][1].Content; got != "xy" {
		t.Fatalf("expected appended resized row content, got %q", got)
	}
	if cursor := vt.CursorState(); cursor.Row != 2 || cursor.Col != 2 {
		t.Fatalf("expected cursor moved after resize update, got %#v", cursor)
	}
}

func TestApplyScreenUpdateUpdatesScrollbackWithoutRecreatingEmulator(t *testing.T) {
	vt := New(4, 2, 100, nil)
	now := time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC)
	vt.LoadSnapshotWithMetadata([][]Cell{
		{{Content: "a", Width: 1}},
		{{Content: "b", Width: 1}},
		{{Content: "c", Width: 1}},
	}, []time.Time{now, now.Add(time.Second), now.Add(2 * time.Second)}, []string{"a", "b", "c"}, ScreenData{
		Cells: [][]Cell{
			{
				{Content: "x", Width: 1},
				{Content: "y", Width: 1},
			},
		},
	}, []time.Time{now}, []string{"screen"}, CursorState{Row: 0, Col: 2, Visible: true}, TerminalModes{AutoWrap: true})

	oldEmu := vt.emu
	if !vt.ApplyScreenUpdate(ScreenUpdate{
		Size:           Size{Cols: 2, Rows: 1},
		ScrollbackTrim: 1,
		ScrollbackAppend: []ScrollbackRowAppend{{
			Cells:     []Cell{{Content: "d", Width: 1}},
			Timestamp: now.Add(3 * time.Second),
			RowKind:   "d",
		}},
		Cursor: CursorState{Row: 0, Col: 2, Visible: true},
		Modes:  TerminalModes{AutoWrap: true},
	}) {
		t.Fatal("expected scrollback update to apply incrementally")
	}
	if vt.emu != oldEmu {
		t.Fatal("expected scrollback update to keep the existing emulator instance")
	}
	scrollback := vt.ScrollbackContent()
	if len(scrollback) != 3 {
		t.Fatalf("expected trimmed+appended scrollback length 3, got %d", len(scrollback))
	}
	if got := scrollback[0][0].Content + scrollback[1][0].Content + scrollback[2][0].Content; got != "bcd" {
		t.Fatalf("expected scrollback to become bcd, got %q", got)
	}
	if got := vt.ScrollbackRowTimestampAt(0); !got.Equal(now.Add(time.Second)) {
		t.Fatalf("expected retained timestamp after trim, got %v", got)
	}
	if got := vt.ScrollbackRowKindAt(2); got != "d" {
		t.Fatalf("expected appended row kind, got %q", got)
	}
}

func TestApplyScreenUpdateScrollbackAppendKeepsMetadataTailWhenCapped(t *testing.T) {
	vt := New(4, 2, 3, nil)
	now := time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC)
	vt.LoadSnapshotWithMetadata([][]Cell{
		{{Content: "a", Width: 1}},
		{{Content: "b", Width: 1}},
		{{Content: "c", Width: 1}},
	}, []time.Time{now, now.Add(time.Second), now.Add(2 * time.Second)}, []string{"a", "b", "c"}, ScreenData{
		Cells: [][]Cell{{{Content: "x", Width: 1}}},
	}, []time.Time{now}, []string{"screen"}, CursorState{Row: 0, Col: 1, Visible: true}, TerminalModes{AutoWrap: true})

	if !vt.ApplyScreenUpdate(ScreenUpdate{
		Size: Size{Cols: 1, Rows: 1},
		ScrollbackAppend: []ScrollbackRowAppend{{
			Cells:     []Cell{{Content: "d", Width: 1}},
			Timestamp: now.Add(3 * time.Second),
			RowKind:   "d",
		}},
		Cursor: CursorState{Row: 0, Col: 1, Visible: true},
		Modes:  TerminalModes{AutoWrap: true},
	}) {
		t.Fatal("expected capped scrollback append to apply incrementally")
	}

	scrollback := vt.ScrollbackContent()
	if len(scrollback) != 3 {
		t.Fatalf("expected capped scrollback length 3, got %d", len(scrollback))
	}
	if got := scrollback[0][0].Content + scrollback[1][0].Content + scrollback[2][0].Content; got != "bcd" {
		t.Fatalf("expected capped scrollback to keep bcd, got %q", got)
	}
	if got := vt.ScrollbackRowTimestampAt(0); !got.Equal(now.Add(time.Second)) {
		t.Fatalf("expected metadata to drop capped prefix, got %v", got)
	}
	if got := vt.ScrollbackRowKindAt(0); got != "b" {
		t.Fatalf("expected metadata row 0 kind b after cap, got %q", got)
	}
	if got := vt.ScrollbackRowKindAt(2); got != "d" {
		t.Fatalf("expected appended metadata kind d, got %q", got)
	}
}

func TestApplyScreenUpdateWriteSpanPreservesTrailingBlankUsedWidthAfterResize(t *testing.T) {
	vt := New(4, 3, 100, nil)
	if !vt.ApplyScreenUpdate(ScreenUpdate{
		Size: Size{Cols: 4, Rows: 3},
		Ops: []DamageOp{{
			Code: ScreenOpWriteSpan,
			Row:  0,
			Col:  0,
			Cells: []Cell{
				{Content: "A", Width: 1},
				{Content: "A", Width: 1},
				{Content: " ", Width: 1},
				{Content: " ", Width: 1},
			},
		}},
		Cursor: CursorState{Row: 0, Col: 4, Visible: true},
		Modes:  TerminalModes{AutoWrap: true},
	}) {
		t.Fatal("expected trailing-space write span to apply incrementally")
	}

	vt.Resize(2, 3)
	if got := rowText(vt.ScreenRowView(0), 2); got != "AA" {
		t.Fatalf("expected first resized row from write span, got %q", got)
	}
	if got := rowText(vt.ScreenRowView(1), 2); got != "  " {
		t.Fatalf("expected trailing blank cells from write span to survive resize, got %q row=%#v", got, vt.ScreenRowView(1))
	}
}

func firstWriteSpanOp(t *testing.T, damage WriteDamage) DamageOp {
	t.Helper()
	return firstOpWithCode(t, damage, ScreenOpWriteSpan)
}

func firstOpWithCode(t *testing.T, damage WriteDamage, code ScreenOpCode) DamageOp {
	t.Helper()
	for _, op := range damage.Ops {
		if op.Code == code {
			return op
		}
	}
	t.Fatalf("expected op %v in damage, got %#v", code, damage)
	return DamageOp{}
}
