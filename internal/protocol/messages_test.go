package protocol

import (
	"bytes"
	"reflect"
	"testing"
	"time"
	"unsafe"
)

func TestSnapshotPayloadRoundTripUsesBinaryRows(t *testing.T) {
	snap := &Snapshot{
		TerminalID: "term-binary",
		Size:       Size{Cols: 80, Rows: 24},
		Screen: ScreenData{
			Cells: [][]Cell{
				{{Content: "o", Width: 1}, {Content: "k", Width: 1}},
				{{Content: "重", Width: 2}, {Content: "", Width: 0}},
				{{Content: "A", Width: 1}, {Content: " ", Width: 1}, {Content: " ", Width: 1}},
			},
			IsAlternateScreen: true,
		},
		Scrollback: []CompactRow{
			CompactRowFromCells([]Cell{{Content: "h", Width: 1, Style: CellStyle{FG: "#ff0000"}}, {Content: "i", Width: 1, Style: CellStyle{FG: "#ff0000"}}}),
		},
		ScreenTimestamps:     []time.Time{time.Date(2026, 3, 18, 1, 0, 0, 0, time.UTC)},
		ScrollbackTimestamps: []time.Time{time.Date(2026, 3, 18, 0, 0, 0, 0, time.UTC)},
		ScreenRowKinds:       []string{SnapshotRowKindRestart},
		ScrollbackRowKinds:   []string{"log"},
		ScreenWrapped:        []bool{false, true, false},
		ScrollbackWrapped:    []bool{true},
		ScreenOwnership:      []string{RowOwnershipScreen, RowOwnershipScreen, RowOwnershipScreen},
		ScrollbackOwnership:  []string{RowOwnershipPersisted},
		Cursor:               CursorState{Row: 1, Col: 2, Visible: true, Shape: "bar", Blink: true},
		Modes:                TerminalModes{AlternateScreen: true, AlternateScroll: true, BracketedPaste: true, ApplicationCursor: true, AutoWrap: true},
		Timestamp:            time.Date(2026, 3, 18, 2, 0, 0, 0, time.UTC),
	}
	payload, err := EncodeSnapshotPayload(snap)
	if err != nil {
		t.Fatalf("encode snapshot payload failed: %v", err)
	}
	if bytes.Contains(payload, []byte(`"terminal_id"`)) || bytes.Contains(payload, []byte(`"cells"`)) {
		t.Fatalf("expected binary protobuf payload, got %q", payload)
	}
	decoded, err := DecodeSnapshotPayload(payload)
	if err != nil {
		t.Fatalf("decode snapshot payload failed: %v", err)
	}
	if decoded.TerminalID != snap.TerminalID || decoded.Size != snap.Size || !decoded.Screen.IsAlternateScreen {
		t.Fatalf("unexpected decoded header: %#v", decoded)
	}
	if got := rowToStringForTest(decoded.Screen.Cells[0]); got != "ok" {
		t.Fatalf("unexpected decoded screen row: %q", got)
	}
	if got := rowToStringForTest(decoded.Screen.Cells[1]); got != "重" {
		t.Fatalf("unexpected decoded wide row: %q %#v", got, decoded.Screen.Cells[1])
	}
	if got := rowToStringForTest(decoded.Screen.Cells[2]); got != "A  " {
		t.Fatalf("expected decoded screen row to preserve trailing spaces, got %q %#v", got, decoded.Screen.Cells[2])
	}
	if row := decoded.Scrollback[0].DecodeCells(); rowToStringForTest(row) != "hi" || row[0].Style.FG != "#ff0000" {
		t.Fatalf("unexpected decoded scrollback: %#v", row)
	}
	if len(decoded.ScreenWrapped) != 3 || !decoded.ScreenWrapped[1] || len(decoded.ScrollbackWrapped) != 1 || !decoded.ScrollbackWrapped[0] {
		t.Fatalf("unexpected decoded wrapped metadata: %#v %#v", decoded.ScreenWrapped, decoded.ScrollbackWrapped)
	}
	if len(decoded.ScreenOwnership) != 3 || decoded.ScreenOwnership[0] != RowOwnershipScreen || len(decoded.ScrollbackOwnership) != 1 || decoded.ScrollbackOwnership[0] != RowOwnershipPersisted {
		t.Fatalf("unexpected decoded ownership metadata: %#v %#v", decoded.ScreenOwnership, decoded.ScrollbackOwnership)
	}
	if decoded.Cursor.Shape != "bar" || !decoded.Modes.BracketedPaste || !decoded.Modes.AutoWrap {
		t.Fatalf("unexpected decoded cursor/modes: %#v %#v", decoded.Cursor, decoded.Modes)
	}
}

func TestGridViewportPayloadRoundTripUsesBinaryRows(t *testing.T) {
	viewport := &GridViewport{
		TerminalID:             "term-grid",
		Size:                   Size{Cols: 120, Rows: 40},
		Rows:                   []CompactRow{CompactRowFromCells([]Cell{{Content: "r", Width: 1}, {Content: "o", Width: 1}, {Content: "w", Width: 1}})},
		ScrollbackOffset:       10,
		ScrollbackLimit:        20,
		ScrollbackTotal:        100,
		ScrollbackLogicalTotal: 42,
		ScrollbackHasMore:      true,
		LoadedRows:             7,
		HistoryGeneration:      42,
		FirstRowID:             1000,
		LastRowID:              1006,
		RowOwnership:           []string{RowOwnershipLiveTailReclaimed},
		Timestamp:              time.Date(2026, 3, 18, 3, 0, 0, 0, time.UTC),
	}
	payload, err := EncodeGridViewportPayload(viewport)
	if err != nil {
		t.Fatalf("encode viewport payload failed: %v", err)
	}
	decoded, err := DecodeGridViewportPayload(payload)
	if err != nil {
		t.Fatalf("decode viewport payload failed: %v", err)
	}
	if decoded.TerminalID != viewport.TerminalID || decoded.Size != viewport.Size || decoded.ScrollbackTotal != 100 || decoded.ScrollbackLogicalTotal != 42 || !decoded.ScrollbackHasMore {
		t.Fatalf("unexpected decoded viewport header: %#v", decoded)
	}
	if decoded.LoadedRows != 7 || decoded.HistoryGeneration != 42 || decoded.FirstRowID != 1000 || decoded.LastRowID != 1006 {
		t.Fatalf("unexpected decoded viewport coordinates: %#v", decoded)
	}
	if len(decoded.RowOwnership) != 1 || decoded.RowOwnership[0] != RowOwnershipLiveTailReclaimed {
		t.Fatalf("unexpected decoded viewport ownership: %#v", decoded.RowOwnership)
	}
	if got := compactRowToStringForTest(decoded.Rows[0]); got != "row" {
		t.Fatalf("unexpected decoded viewport row: %q", got)
	}
}

func TestCompactRowsReuseASCIIStrings(t *testing.T) {
	rows := CompactRowsFromCells([][]Cell{
		{{Content: "a", Width: 1}, {Content: "a", Width: 1}, {Content: "a", Width: 1}, {Content: "a", Width: 1}},
	})
	decoded := rows[0].DecodeCells()
	if len(decoded) != 4 {
		t.Fatalf("unexpected compact row: %#v", decoded)
	}
	first := decoded[0].Content
	for i, cell := range decoded {
		if cell.Content != first {
			t.Fatalf("unexpected content at %d: %q want %q", i, cell.Content, first)
		}
		if unsafe.StringData(cell.Content) != unsafe.StringData(first) {
			t.Fatalf("expected ASCII cell content to reuse backing string at %d", i)
		}
	}
}

func compactRowToStringForTest(row CompactRow) string {
	return rowToStringForTest(row.DecodeCells())
}

func rowToStringForTest(row []Cell) string {
	var out string
	for _, cell := range row {
		out += cell.Content
	}
	return out
}

func TestChannelAllocatorReuseAndExhaustion(t *testing.T) {
	a := NewChannelAllocator()

	first, err := a.Alloc()
	if err != nil {
		t.Fatalf("alloc first failed: %v", err)
	}
	second, err := a.Alloc()
	if err != nil {
		t.Fatalf("alloc second failed: %v", err)
	}
	if first != 1 || second != 2 {
		t.Fatalf("unexpected allocated channels: %d %d", first, second)
	}

	a.Free(first)
	reused, err := a.Alloc()
	if err != nil {
		t.Fatalf("alloc reused failed: %v", err)
	}
	if reused != first {
		t.Fatalf("expected channel %d to be reused, got %d", first, reused)
	}

	a.next = ^uint16(0)
	if _, err := a.Alloc(); err == nil {
		t.Fatal("expected allocator exhaustion error")
	}
}

func TestScreenUpdatePayloadTrimsTrailingBlankCellsButKeepsWideContinuation(t *testing.T) {
	update := ScreenUpdate{
		Size: protocolSize(10, 2),
		Ops: []ScreenOp{{
			Code: ScreenOpWriteSpan,
			Row:  0,
			Col:  0,
			Cells: []Cell{
				{Content: "A", Width: 1},
				{Content: "界", Width: 2},
				{Content: "", Width: 0},
				{Content: " ", Width: 1},
			},
		}},
		Cursor: CursorState{Row: 0, Col: 0, Visible: true},
		Modes:  TerminalModes{AutoWrap: true},
	}

	payload, err := EncodeScreenUpdatePayload(update)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	if !bytes.HasPrefix(payload, []byte(screenUpdatePayloadMagic)) {
		t.Fatalf("expected current binary screen update magic, got prefix %q", payload[:minInt(len(payload), 8)])
	}
	decoded, err := DecodeScreenUpdatePayload(payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(decoded.Ops) != 1 {
		t.Fatalf("expected one op, got %#v", decoded.Ops)
	}
	row := decoded.Ops[0].Cells
	if len(row) != 4 {
		t.Fatalf("expected write span cells to round-trip without trimming, got %#v", row)
	}
	if row[1].Content != "界" || row[1].Width != 2 {
		t.Fatalf("expected wide lead preserved, got %#v", row[1])
	}
	if row[2].Content != "" || row[2].Width != 0 {
		t.Fatalf("expected wide continuation preserved, got %#v", row[2])
	}
}

func TestScreenUpdatePayloadFullReplacePreservesTrailingBlankCells(t *testing.T) {
	update := ScreenUpdate{
		FullReplace: true,
		Size:        protocolSize(4, 1),
		Screen: ScreenData{Cells: [][]Cell{{
			{Content: "X", Width: 1},
			{Content: " ", Width: 1, Style: CellStyle{BG: "#112233"}},
			{Content: " ", Width: 1},
		}}},
		Cursor: CursorState{Row: 0, Col: 0, Visible: true},
		Modes:  TerminalModes{AutoWrap: true},
	}

	payload, err := EncodeScreenUpdatePayload(update)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	decoded, err := DecodeScreenUpdatePayload(payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	row := decoded.Screen.Cells[0]
	if len(row) != 3 {
		t.Fatalf("expected full replace trailing plain blank to preserve row geometry, got %#v", row)
	}
	if got := row[1].Style.BG; got != "#112233" {
		t.Fatalf("expected styled trailing blank cell preserved, got %#v", row[1])
	}
	if row[2].Content != " " || row[2].Width != 1 || row[2].Style != (CellStyle{}) {
		t.Fatalf("expected plain trailing blank cell preserved, got %#v", row[2])
	}
}

func TestCompactRowFromCellsCanPreserveTrailingBlankCells(t *testing.T) {
	row := []Cell{
		{Content: "A", Width: 1},
		{Content: "A", Width: 1},
		{Content: " ", Width: 1},
		{Content: " ", Width: 1},
	}

	trimmed := CompactRowFromCells(row).DecodeCells()
	if got := len(trimmed); got != 2 {
		t.Fatalf("expected default compact row to trim trailing blanks, got len=%d row=%#v", got, trimmed)
	}

	preserved := CompactRowFromCellsPreserveTrailingBlankCells(row, true).DecodeCells()
	if got := len(preserved); got != 4 {
		t.Fatalf("expected preserving compact row to keep trailing blanks, got len=%d row=%#v", got, preserved)
	}
	if preserved[2].Content != " " || preserved[3].Content != " " {
		t.Fatalf("expected trailing blanks preserved, got %#v", preserved)
	}
}

func TestScreenUpdatePayloadCurrentRoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 12, 1, 2, 3, 0, time.UTC)
	screenRows := [][]Cell{rowWithTextAt(10, 0, "abc"), rowWithTextAt(10, 0, "def")}
	screenRows[0][1].LinkURL = "https://example.test/screen"
	screenRows[0][1].LinkParams = "id=screen"
	writeCells := []Cell{{Content: "n", Width: 1}, {Content: "e", Width: 1, LinkURL: "https://example.test/op", LinkParams: "id=op"}, {Content: "w", Width: 1}}
	scrollbackRow := rowWithTextAt(10, 0, "old")
	scrollbackRow[0].LinkURL = "https://example.test/scrollback"
	scrollbackRow[0].LinkParams = "id=scrollback"
	update := ScreenUpdate{
		FullReplace:     true,
		ResetScrollback: true,
		Size:            protocolSize(10, 4),
		Title:           "ops-demo",
		Screen: ScreenData{
			Cells:             screenRows,
			IsAlternateScreen: true,
		},
		ScreenTimestamps: []time.Time{now, now.Add(time.Second)},
		ScreenRowKinds:   []string{"a", "b"},
		ScreenWrapped:    []bool{true, false},
		ScreenScroll:     1,
		Ops: []ScreenOp{
			{Code: ScreenOpScrollRect, Rect: ScreenRect{X: 0, Y: 0, Width: 10, Height: 4}, Dy: -1},
			{Code: ScreenOpWriteSpan, Row: 3, Col: 0, Cells: writeCells, Timestamp: now.Add(2 * time.Second), RowKind: "tail", Wrapped: true, WrappedSet: true},
			{Code: ScreenOpCopyRect, Src: ScreenRect{X: 0, Y: 0, Width: 4, Height: 1}, DstX: 5, DstY: 1},
			{Code: ScreenOpClearRect, Rect: ScreenRect{X: 7, Y: 0, Width: 3, Height: 2}, Timestamp: now.Add(3 * time.Second), RowKind: "clear", WrappedSet: true},
			{Code: ScreenOpClearToEOL, Row: 2, Col: 4, Timestamp: now.Add(4 * time.Second), RowKind: "eol", Wrapped: true, WrappedSet: true},
			{Code: ScreenOpCursor, Cursor: CursorState{Row: 3, Col: 3, Visible: true, Shape: "bar"}},
			{Code: ScreenOpModes, Modes: TerminalModes{AlternateScreen: true, AutoWrap: true, MouseTracking: true}},
			{Code: ScreenOpResize, Size: Size{Cols: 10, Rows: 4}},
			{Code: ScreenOpTitle, Title: "ops-demo"},
		},
		ScrollbackTrim: 2,
		ScrollbackAppend: []ScrollbackRowAppend{{
			Cells:      scrollbackRow,
			Timestamp:  now.Add(5 * time.Second),
			RowKind:    "old",
			Wrapped:    true,
			WrappedSet: true,
		}},
		Cursor: CursorState{Row: 3, Col: 3, Visible: true, Shape: "bar"},
		Modes:  TerminalModes{AlternateScreen: true, AutoWrap: true, MouseTracking: true},
	}

	payload, err := EncodeScreenUpdatePayload(update)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	if !bytes.HasPrefix(payload, []byte(screenUpdatePayloadMagic)) {
		t.Fatalf("expected current screen update magic, got %q", payload[:minInt(len(payload), 8)])
	}
	decoded, err := DecodeScreenUpdatePayload(payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if !decoded.FullReplace || !decoded.ResetScrollback || decoded.Title != update.Title || decoded.ScreenScroll != update.ScreenScroll {
		t.Fatalf("unexpected header round-trip: %#v", decoded)
	}
	if len(decoded.ScreenWrapped) != 2 || !decoded.ScreenWrapped[0] || decoded.ScreenWrapped[1] {
		t.Fatalf("unexpected decoded screen wrapped: %#v", decoded.ScreenWrapped)
	}
	if got := decoded.Screen.Cells[0][1]; got.LinkURL != "https://example.test/screen" || got.LinkParams != "id=screen" {
		t.Fatalf("expected screen hyperlink round-trip, got %#v", got)
	}
	if len(decoded.Ops) != len(update.Ops) {
		t.Fatalf("expected %d decoded ops, got %#v", len(update.Ops), decoded.Ops)
	}
	if decoded.Ops[1].Code != ScreenOpWriteSpan || !decoded.Ops[1].WrappedSet || !decoded.Ops[1].Wrapped {
		t.Fatalf("unexpected write op wrapped metadata: %#v", decoded.Ops[1])
	}
	if got := decoded.Ops[1].Cells[1]; got.LinkURL != "https://example.test/op" || got.LinkParams != "id=op" {
		t.Fatalf("expected write-span hyperlink round-trip, got %#v", got)
	}
	if decoded.Ops[2].Code != ScreenOpCopyRect || decoded.Ops[2].DstX != 5 || decoded.Ops[2].DstY != 1 {
		t.Fatalf("unexpected copyrect op: %#v", decoded.Ops[2])
	}
	if decoded.Ops[5].Cursor.Shape != "bar" || !decoded.Ops[6].Modes.MouseTracking {
		t.Fatalf("unexpected control op round-trip: %#v %#v", decoded.Ops[5], decoded.Ops[6])
	}
	if len(decoded.ScrollbackAppend) != 1 || !decoded.ScrollbackAppend[0].WrappedSet || !decoded.ScrollbackAppend[0].Wrapped {
		t.Fatalf("unexpected scrollback wrapped metadata: %#v", decoded.ScrollbackAppend)
	}
	if got := decoded.ScrollbackAppend[0].Cells[0]; got.LinkURL != "https://example.test/scrollback" || got.LinkParams != "id=scrollback" {
		t.Fatalf("expected scrollback hyperlink round-trip, got %#v", got)
	}
}

func TestDecodeScreenUpdatePayloadRejectsLegacyPayloads(t *testing.T) {
	for _, payload := range [][]byte{
		[]byte(`{"size":{"cols":4,"rows":1}}`),
		[]byte("TSU2\x00"),
		[]byte("TSU3\x00"),
		[]byte("TSU4\x00"),
		[]byte("TSU5\x00"),
		[]byte("TSU6\x00"),
	} {
		if _, err := DecodeScreenUpdatePayload(payload); err == nil {
			t.Fatalf("expected legacy payload %q to be rejected", payload[:minInt(len(payload), 4)])
		}
	}
}

func TestNormalizeScreenUpdateAlignsFullReplaceMetadataAndOps(t *testing.T) {
	now := time.Now().UTC()
	update := NormalizeScreenUpdate(ScreenUpdate{
		FullReplace: true,
		Screen: ScreenData{Cells: [][]Cell{
			{{Content: "a", Width: 1}},
			{{Content: "b", Width: 1}},
		}},
		ScreenTimestamps: []time.Time{now},
		ScreenRowKinds:   []string{"restart", "stale", "overflow"},
		ScreenWrapped:    []bool{true, false, true},
		Ops: []ScreenOp{
			{Code: ScreenOpWriteSpan, Row: 0, Col: 0, Cells: []Cell{{Content: "x", Width: 1}}, Wrapped: true, WrappedSet: true, Rect: ScreenRect{X: 1, Y: 2}},
			{Code: ScreenOpClearToEOL, Row: 0, Col: 1, Cells: []Cell{{Content: "ignored", Width: 1}}},
		},
	})

	if len(update.ScreenTimestamps) != 2 {
		t.Fatalf("expected screen timestamps normalized to screen height, got %#v", update.ScreenTimestamps)
	}
	if len(update.ScreenRowKinds) != 2 {
		t.Fatalf("expected screen row kinds normalized to screen height, got %#v", update.ScreenRowKinds)
	}
	if len(update.ScreenWrapped) != 2 || !update.ScreenWrapped[0] || update.ScreenWrapped[1] {
		t.Fatalf("expected screen wrapped normalized to screen height, got %#v", update.ScreenWrapped)
	}
	if update.Ops[0].Rect != (ScreenRect{}) || !update.Ops[0].Wrapped {
		t.Fatalf("expected write op normalized without dropping wrapped metadata, got %#v", update.Ops[0])
	}
	if len(update.Ops[1].Cells) != 0 {
		t.Fatalf("expected clear-to-eol op to drop cells, got %#v", update.Ops[1])
	}
}

func TestClassifyScreenUpdateDetectsBlankFullReplace(t *testing.T) {
	classification := ClassifyScreenUpdate(ScreenUpdate{
		FullReplace: true,
		Screen: ScreenData{Cells: [][]Cell{
			{{Content: " ", Width: 1}},
			{{Content: "", Width: 0}},
		}},
	})

	if !classification.BlankFullReplace || !classification.FullReplace {
		t.Fatalf("expected blank full replace classification, got %#v", classification)
	}
	if !classification.HasContentChange {
		t.Fatalf("expected blank full replace to still count as content change, got %#v", classification)
	}
	if classification.HasChangedRows || classification.HasScrollbackChange {
		t.Fatalf("expected blank full replace to stay delta-free, got %#v", classification)
	}
}

func TestClassifyScreenUpdateTreatsTitleOnlyUpdateAsNonContentChange(t *testing.T) {
	classification := ClassifyScreenUpdate(ScreenUpdate{
		Title:  "demo",
		Cursor: CursorState{Visible: true},
		Modes:  TerminalModes{AutoWrap: true},
	})

	if classification.HasContentChange {
		t.Fatalf("expected title-only update to avoid content-change boundary, got %#v", classification)
	}
	if !classification.HasTitle {
		t.Fatalf("expected title-only update to keep title bit, got %#v", classification)
	}
	if classification.FullReplace || classification.HasChangedRows || classification.HasScreenScroll || classification.HasScrollbackChange {
		t.Fatalf("expected title-only update to stay non-buffer-mutating, got %#v", classification)
	}
}

func TestClassifyScreenUpdateTreatsOpcodeControlOnlyUpdateAsNonContentChange(t *testing.T) {
	classification := ClassifyScreenUpdate(ScreenUpdate{
		Title:  "demo",
		Cursor: CursorState{Row: 1, Col: 2, Visible: true},
		Modes:  TerminalModes{AutoWrap: true},
		Ops: []ScreenOp{
			{Code: ScreenOpCursor, Cursor: CursorState{Row: 1, Col: 2, Visible: true}},
			{Code: ScreenOpModes, Modes: TerminalModes{AutoWrap: true}},
			{Code: ScreenOpTitle, Title: "demo"},
		},
	})

	if classification.HasContentChange {
		t.Fatalf("expected control-only opcode update to avoid content-change boundary, got %#v", classification)
	}
	if classification.HasChangedRows || classification.HasScreenScroll || classification.HasScrollbackChange || !classification.HasTitle {
		t.Fatalf("unexpected opcode classification bits: %#v", classification)
	}
}

func protocolSize(cols, rows uint16) Size {
	return Size{Cols: cols, Rows: rows}
}

func filledRow(width int) []Cell {
	row := make([]Cell, width)
	for i := range row {
		row[i] = Cell{Content: " ", Width: 1}
	}
	return row
}

func rowWithTextAt(width, col int, text string) []Cell {
	row := filledRow(width)
	writeText(row, col, text)
	return row
}

func writeText(row []Cell, col int, text string) {
	for _, r := range text {
		if col >= len(row) {
			return
		}
		row[col] = Cell{Content: string(r), Width: 1}
		col++
	}
}

func TestHistoryWindowPayloadRoundTrip(t *testing.T) {
	window := &HistoryWindow{
		TerminalID:    "term-hist",
		Token:         "g7:0-2:c80",
		Op:            HistoryWindowReplace,
		Size:          Size{Cols: 80, Rows: 24},
		Rows:          []CompactRow{CompactRowFromCells([]Cell{{Content: "a", Width: 1}, {Content: "b", Width: 1}})},
		RowKinds:      []string{"output"},
		RowWrapped:    []bool{false},
		RowOwnership:  []string{RowOwnershipPersisted},
		RowTimestamps: []time.Time{time.Date(2026, 6, 2, 1, 0, 0, 0, time.UTC)},
		Lines: []HistoryLineSpan{{
			StartRow:       0,
			EndRow:         0,
			RowKind:        "output",
			LogicalLineID:  42,
			TimestampStart: time.Date(2026, 6, 2, 0, 59, 0, 0, time.UTC),
			TimestampEnd:   time.Date(2026, 6, 2, 1, 1, 0, 0, time.UTC),
			ClippedBefore:  true,
			ClippedAfter:   true,
		}},
		BeforeOffset: 3,
		LoadedRows:   9,
		TotalRows:    12,
		LoadedLines:  2,
		LogicalTotal: 4,
		HasMore:      true,
		Generation:   7,
		FirstRowID:   0,
		LastRowID:    2,
		FirstLineID:  42,
		LastLineID:   43,
		CursorValid:  true,
		CursorLineID: 42,
		CursorRow:    1,
		RowLineIDs:   []uint64{42},
		RowInLine:    []int{1},
		Timestamp:    time.Date(2026, 6, 2, 2, 0, 0, 0, time.UTC),
	}
	payload, err := EncodeHistoryWindowPayload(window)
	if err != nil {
		t.Fatalf("encode history window payload failed: %v", err)
	}
	decoded, err := DecodeHistoryWindowPayload(payload)
	if err != nil {
		t.Fatalf("decode history window payload failed: %v", err)
	}
	if decoded.TerminalID != "term-hist" || decoded.Token != "g7:0-2:c80" || decoded.Op != HistoryWindowReplace || decoded.Size != window.Size {
		t.Fatalf("unexpected decoded history window header: %#v", decoded)
	}
	if decoded.BeforeOffset != 3 || decoded.LoadedRows != 9 || decoded.TotalRows != 12 || decoded.LoadedLines != 2 || decoded.LogicalTotal != 4 || !decoded.HasMore {
		t.Fatalf("unexpected decoded history window metadata: %#v", decoded)
	}
	if decoded.Generation != 7 || decoded.FirstRowID != 0 || decoded.LastRowID != 2 || decoded.FirstLineID != 42 || decoded.LastLineID != 43 {
		t.Fatalf("unexpected decoded history window boundary: %#v", decoded)
	}
	if !decoded.CursorValid || decoded.CursorLineID != 42 || decoded.CursorRow != 1 {
		t.Fatalf("unexpected decoded history cursor: %#v", decoded)
	}
	if !reflect.DeepEqual(decoded.RowLineIDs, []uint64{42}) || !reflect.DeepEqual(decoded.RowInLine, []int{1}) {
		t.Fatalf("unexpected decoded row logical mapping: line_ids=%v row_in_line=%v", decoded.RowLineIDs, decoded.RowInLine)
	}
	if len(decoded.Lines) != 1 || decoded.Lines[0] != window.Lines[0] {
		t.Fatalf("unexpected decoded history line spans: %#v", decoded.Lines)
	}
	if len(decoded.RowOwnership) != 1 || decoded.RowOwnership[0] != RowOwnershipPersisted {
		t.Fatalf("unexpected decoded history ownership: %#v", decoded.RowOwnership)
	}
	if got := compactRowToStringForTest(decoded.Rows[0]); got != "ab" {
		t.Fatalf("unexpected decoded history row: %q", got)
	}
}

func TestHistoryWindowParamsControlPayloadRoundTrip(t *testing.T) {
	encoded, err := EncodeMethodParams("history.window", HistoryWindowParams{
		TerminalID:          "term-hist",
		BeforeOffset:        5,
		Limit:               50,
		Cols:                100,
		Token:               "g7:c100:f42:l43",
		Generation:          7,
		CursorValid:         true,
		BeforeLineID:        42,
		BeforeRowInLine:     1,
		BoundaryFirstLineID: 42,
		BoundaryLastLineID:  43,
	})
	if err != nil {
		t.Fatalf("encode control params failed: %v", err)
	}
	decoded, err := DecodeMethodParams("history.window", encoded)
	if err != nil {
		t.Fatalf("decode control params failed: %v", err)
	}
	params, ok := decoded.(HistoryWindowParams)
	if !ok {
		t.Fatalf("expected HistoryWindowParams, got %T", decoded)
	}
	if params != (HistoryWindowParams{
		TerminalID:          "term-hist",
		BeforeOffset:        5,
		Limit:               50,
		Cols:                100,
		Token:               "g7:c100:f42:l43",
		Generation:          7,
		CursorValid:         true,
		BeforeLineID:        42,
		BeforeRowInLine:     1,
		BoundaryFirstLineID: 42,
		BoundaryLastLineID:  43,
	}) {
		t.Fatalf("unexpected decoded history window params: %#v", params)
	}
}
