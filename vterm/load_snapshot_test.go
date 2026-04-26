package vterm

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/protocol"
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

func TestVTermWriteMarksAutoWrappedRows(t *testing.T) {
	vt := New(5, 3, 100, nil)

	if _, err := vt.Write([]byte("abcdef")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	rowKinds := vt.ScreenRowKinds()
	if len(rowKinds) < 2 {
		t.Fatalf("expected at least two screen row kinds, got %#v", rowKinds)
	}
	if rowKinds[0] == protocol.SnapshotRowKindWrapped {
		t.Fatalf("expected first physical row not to be wrapped, got %#v", rowKinds)
	}
	if rowKinds[1] != protocol.SnapshotRowKindWrapped {
		t.Fatalf("expected auto-wrap continuation row to be wrapped, got %#v", rowKinds)
	}
}

func TestVTermWriteWithDamagePreservesWideRuneContinuations(t *testing.T) {
	vt := New(8, 2, 100, nil)

	_, err, damage := vt.WriteWithDamage([]byte("你a"))
	if err != nil {
		t.Fatalf("write wide rune: %v", err)
	}
	if len(damage.ChangedScreenRows) != 1 {
		t.Fatalf("expected one changed row, got %#v", damage.ChangedScreenRows)
	}
	row := damage.ChangedScreenRows[0]
	if row.Row != 0 {
		t.Fatalf("expected first row damage, got row=%d", row.Row)
	}
	if got := row.Cells[0]; got.Content != "你" || got.Width != 2 {
		t.Fatalf("expected wide rune anchor cell, got %#v", got)
	}
	if got := row.Cells[1]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected wide rune continuation placeholder, got %#v", got)
	}
	if got := row.Cells[2]; got.Content != "a" || got.Width != 1 {
		t.Fatalf("expected trailing ascii cell, got %#v", got)
	}
}

func TestVTermWriteWithDamageNormalizesCombiningRuneClusters(t *testing.T) {
	vt := New(8, 2, 100, nil)

	_, err, damage := vt.WriteWithDamage([]byte("e\u0301"))
	if err != nil {
		t.Fatalf("write combining rune: %v", err)
	}
	if len(damage.ChangedScreenRows) != 1 {
		t.Fatalf("expected one changed row, got %#v", damage.ChangedScreenRows)
	}
	row := damage.ChangedScreenRows[0]
	if got := row.Cells[0]; got.Content != "é" || got.Width != 1 {
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
	if len(damage.ChangedScreenSpans) != 1 {
		t.Fatalf("expected one changed span, got %#v", damage.ChangedScreenSpans)
	}
	span := damage.ChangedScreenSpans[0]
	if span.Row != 0 || span.ColStart != 137 || span.Op != protocol.ScreenSpanOpWrite {
		t.Fatalf("unexpected high-column span metadata: %#v", span)
	}
	if len(span.Cells) != 1 || span.Cells[0].Content != "Z" {
		t.Fatalf("expected single-cell sparse span, got %#v", span.Cells)
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
	if len(damage.ChangedScreenSpans) != 1 {
		t.Fatalf("expected one clear-to-eol span, got %#v", damage.ChangedScreenSpans)
	}
	span := damage.ChangedScreenSpans[0]
	if span.Op != protocol.ScreenSpanOpClearToEOL || span.ColStart != 6 {
		t.Fatalf("unexpected clear-to-eol span: %#v", span)
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
	if len(damage.ChangedScreenSpans) != 1 {
		t.Fatalf("expected one style-only span, got %#v", damage.ChangedScreenSpans)
	}
	span := damage.ChangedScreenSpans[0]
	if span.ColStart != 5 || len(span.Cells) != 1 {
		t.Fatalf("unexpected style-only span window: %#v", span)
	}
	if got := span.Cells[0]; got.Content != "x" || !got.Style.Bold {
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
	if len(damage.ChangedScreenSpans) != 1 {
		t.Fatalf("expected one wide-boundary span, got %#v", damage.ChangedScreenSpans)
	}
	span := damage.ChangedScreenSpans[0]
	if span.ColStart != 0 || len(span.Cells) != 2 {
		t.Fatalf("expected span expanded to include wide continuation, got %#v", span)
	}
	if got := span.Cells[0]; got.Content != "界" || got.Width != 2 {
		t.Fatalf("expected wide anchor cell, got %#v", got)
	}
	if got := span.Cells[1]; got.Content != "" || got.Width != 0 {
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
	if len(damage.ChangedScreenRows) >= 4 {
		t.Fatalf("expected alt-screen scroll to avoid full-screen invalidation, got %#v", damage.ChangedScreenRows)
	}
	if len(damage.ChangedScreenRows) == 0 {
		t.Fatal("expected at least the edge row to be redrawn after alt-screen scroll")
	}
	cols, rows := vt.Size()
	foundScroll := false
	for _, op := range damage.Ops {
		if op.Code != protocol.ScreenOpScrollRect {
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
	if span.Code != protocol.ScreenOpWriteSpan || span.Row != 0 || span.Col != 0 {
		t.Fatalf("unexpected first direct op: %#v", span)
	}
	if got := span.Cells[0].Content + span.Cells[1].Content + span.Cells[2].Content; got != "abc" {
		t.Fatalf("unexpected direct span contents: %#v", span.Cells)
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
	if len(damage.ChangedScreenRows) != 3 {
		t.Fatalf("expected full-screen damage on alt-screen switch, got %#v", damage.ChangedScreenRows)
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
	if len(damage.ChangedScreenRows) != 3 {
		t.Fatalf("expected full-screen damage when restoring main screen, got %#v", damage.ChangedScreenRows)
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

func TestApplyScreenUpdateUpdatesChangedRowsInPlace(t *testing.T) {
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
	if !vt.ApplyScreenUpdate(protocol.ScreenUpdate{
		Size: protocol.Size{Cols: 4, Rows: 2},
		ChangedRows: []protocol.ScreenRowUpdate{{
			Row: 1,
			Cells: []protocol.Cell{
				{Content: "n", Width: 1},
				{Content: "e", Width: 1},
				{Content: "w", Width: 1},
				{Content: "!", Width: 1},
			},
			Timestamp: now.Add(time.Second),
			RowKind:   "new-1",
		}},
		Cursor: protocol.CursorState{Row: 1, Col: 4, Visible: true, Shape: "bar"},
		Modes:  protocol.TerminalModes{AlternateScreen: true, AutoWrap: true},
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

func TestApplyScreenUpdateAppliesClearToEOLSpan(t *testing.T) {
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

	if !vt.ApplyScreenUpdate(protocol.ScreenUpdate{
		Size: protocol.Size{Cols: 8, Rows: 1},
		ChangedSpans: []protocol.ScreenSpanUpdate{{
			Row:      0,
			ColStart: 6,
			Op:       protocol.ScreenSpanOpClearToEOL,
		}},
		Cursor: protocol.CursorState{Row: 0, Col: 6, Visible: true},
		Modes:  protocol.TerminalModes{AlternateScreen: true, AutoWrap: true},
	}) {
		t.Fatal("expected clear-to-eol span to apply incrementally")
	}

	row := vt.ScreenRowView(0)
	if got := strings.TrimRight(rowToString(row), " "); got != "prefix" {
		t.Fatalf("expected row tail cleared, got %q", rowToString(row))
	}
}

func TestApplyScreenUpdateAppliesStyleOnlySpan(t *testing.T) {
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

	if !vt.ApplyScreenUpdate(protocol.ScreenUpdate{
		Size: protocol.Size{Cols: 8, Rows: 1},
		ChangedSpans: []protocol.ScreenSpanUpdate{{
			Row:      0,
			ColStart: 5,
			Cells: []protocol.Cell{{
				Content: "x",
				Width:   1,
				Style:   protocol.CellStyle{Bold: true},
			}},
			Op:        protocol.ScreenSpanOpWrite,
			Timestamp: time.Date(2026, 4, 18, 8, 0, 2, 0, time.UTC),
			RowKind:   "style-only",
		}},
		Cursor: protocol.CursorState{Row: 0, Col: 6, Visible: true},
		Modes:  protocol.TerminalModes{AlternateScreen: true, AutoWrap: true},
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

func TestApplyScreenUpdateAppliesWideCharBoundarySpan(t *testing.T) {
	vt := New(8, 1, 100, nil)
	vt.LoadSnapshot(ScreenData{
		Cells: [][]Cell{{
			{Content: "你", Width: 2},
			{Content: "", Width: 0},
			{Content: "a", Width: 1},
		}},
		IsAlternateScreen: true,
	}, CursorState{Row: 0, Col: 3, Visible: true}, TerminalModes{AlternateScreen: true, AutoWrap: true})

	if !vt.ApplyScreenUpdate(protocol.ScreenUpdate{
		Size: protocol.Size{Cols: 8, Rows: 1},
		ChangedSpans: []protocol.ScreenSpanUpdate{{
			Row:      0,
			ColStart: 0,
			Cells: []protocol.Cell{
				{Content: "界", Width: 2},
				{Content: "", Width: 0},
			},
			Op: protocol.ScreenSpanOpWrite,
		}},
		Cursor: protocol.CursorState{Row: 0, Col: 3, Visible: true},
		Modes:  protocol.TerminalModes{AlternateScreen: true, AutoWrap: true},
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
	if !vt.ApplyScreenUpdate(protocol.ScreenUpdate{
		Size:         protocol.Size{Cols: 4, Rows: 4},
		ScreenScroll: 1,
		Ops: []protocol.ScreenOp{
			{Code: protocol.ScreenOpScrollRect, Rect: protocol.ScreenRect{X: 0, Y: 0, Width: 4, Height: 4}, Dy: -1},
			{Code: protocol.ScreenOpWriteSpan, Row: 3, Col: 0, Cells: []protocol.Cell{{Content: "r", Width: 1}, {Content: "o", Width: 1}, {Content: "w", Width: 1}, {Content: "5", Width: 1}}, Timestamp: now.Add(4 * time.Second), RowKind: "e"},
		},
		Cursor: protocol.CursorState{Row: 3, Col: 0, Visible: true},
		Modes:  protocol.TerminalModes{AlternateScreen: true, AutoWrap: true},
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

	if !vt.ApplyScreenUpdate(protocol.ScreenUpdate{
		Size: protocol.Size{Cols: 4, Rows: 3},
		Ops: []protocol.ScreenOp{
			{Code: protocol.ScreenOpCopyRect, Src: protocol.ScreenRect{X: 0, Y: 0, Width: 4, Height: 1}, DstX: 0, DstY: 2},
		},
		Cursor: protocol.CursorState{Row: 2, Col: 0, Visible: true},
		Modes:  protocol.TerminalModes{AlternateScreen: true, AutoWrap: true},
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

func TestApplyScreenUpdateAppliesResizeThenSparseSpan(t *testing.T) {
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

	if !vt.ApplyScreenUpdate(protocol.ScreenUpdate{
		Size: protocol.Size{Cols: 8, Rows: 3},
		ChangedSpans: []protocol.ScreenSpanUpdate{{
			Row:      2,
			ColStart: 5,
			Cells: []protocol.Cell{
				{Content: "o", Width: 1},
				{Content: "k", Width: 1},
			},
			Op: protocol.ScreenSpanOpWrite,
		}},
		Cursor: protocol.CursorState{Row: 2, Col: 7, Visible: true},
		Modes:  protocol.TerminalModes{AlternateScreen: true, AutoWrap: true},
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
	if vt.ApplyScreenUpdate(protocol.ScreenUpdate{
		Size:            protocol.Size{Cols: 1, Rows: 1},
		ResetScrollback: true,
		Cursor:          protocol.CursorState{Row: 0, Col: 1, Visible: true},
		Modes:           protocol.TerminalModes{AutoWrap: true},
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
	if !vt.ApplyScreenUpdate(protocol.ScreenUpdate{
		Size: protocol.Size{Cols: 6, Rows: 3},
		ChangedRows: []protocol.ScreenRowUpdate{{
			Row: 2,
			Cells: []protocol.Cell{
				{Content: "x", Width: 1},
				{Content: "y", Width: 1},
			},
			Timestamp: now,
		}},
		Cursor: protocol.CursorState{Row: 2, Col: 2, Visible: true},
		Modes:  protocol.TerminalModes{AlternateScreen: true, AutoWrap: true},
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
	if !vt.ApplyScreenUpdate(protocol.ScreenUpdate{
		Size:           protocol.Size{Cols: 2, Rows: 1},
		ScrollbackTrim: 1,
		ScrollbackAppend: []protocol.ScrollbackRowAppend{{
			Cells:     []protocol.Cell{{Content: "d", Width: 1}},
			Timestamp: now.Add(3 * time.Second),
			RowKind:   "d",
		}},
		Cursor: protocol.CursorState{Row: 0, Col: 2, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
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
