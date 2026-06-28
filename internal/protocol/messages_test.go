package protocol

import (
	"bytes"
	"reflect"
	"testing"
	"time"
	"unsafe"
)

func TestNativeScreenSnapshotPayloadRoundTripUsesLiveRevision(t *testing.T) {
	snap := &NativeScreenSnapshot{
		TerminalID: "term-live",
		Revision:   42,
		Size:       Size{Cols: 20, Rows: 3},
		Rows:       []CompactRow{CompactRowFromCells([]Cell{{Content: "l", Width: 1}, {Content: "v", Width: 1}})},
		AltScreen:  true,
		Cursor:     CursorState{Row: 0, Col: 2, Visible: true},
		Modes:      TerminalModes{AlternateScreen: true, BracketedPaste: true, AutoWrap: true},
		Timestamp:  time.Date(2026, 6, 28, 1, 2, 3, 0, time.UTC),
	}
	payload, err := EncodeNativeScreenSnapshotPayload(snap)
	if err != nil {
		t.Fatalf("encode native screen: %v", err)
	}
	decoded, err := DecodeNativeScreenSnapshotPayload(payload)
	if err != nil {
		t.Fatalf("decode native screen: %v", err)
	}
	if decoded.TerminalID != snap.TerminalID || decoded.Revision != 42 || decoded.Size != snap.Size || !decoded.AltScreen {
		t.Fatalf("unexpected native screen metadata %#v", decoded)
	}
	if decoded.Rows[0].Text != "lv" || !decoded.Modes.BracketedPaste || decoded.Cursor.Col != 2 {
		t.Fatalf("native screen lost rows/modes/cursor: %#v", decoded)
	}
}

func TestLiveInvalidatedEventRoundTripCarriesRevision(t *testing.T) {
	payload, err := EncodeEventPayload(Event{
		Type:            EventTerminalLiveInvalidated,
		TerminalID:      "term-live",
		LiveInvalidated: &LiveScreenInvalidatedData{Revision: 99},
		Timestamp:       time.Date(2026, 6, 28, 1, 2, 3, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("encode event: %v", err)
	}
	event, err := DecodeEventPayload(payload)
	if err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if event.Type != EventTerminalLiveInvalidated || event.TerminalID != "term-live" || event.LiveInvalidated == nil || event.LiveInvalidated.Revision != 99 {
		t.Fatalf("live invalidation event did not round trip: %#v", event)
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

func TestBuildCompactRowMatchesSliceBuilder(t *testing.T) {
	row := []Cell{
		{Content: "A", Width: 1, Style: CellStyle{FG: "#ff0000"}},
		{Content: "B", Width: 1, Style: CellStyle{FG: "#ff0000"}},
		{Content: " ", Width: 1},
	}
	streamed := BuildCompactRowPreserveTrailingBlankCells(len(row), func(index int) Cell { return row[index] })
	sliced := CompactRowFromCellsPreserveTrailingBlankCells(row, true)
	if !reflect.DeepEqual(streamed, sliced) {
		t.Fatalf("streamed compact row mismatch\nwant %#v\ngot  %#v", sliced, streamed)
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

func TestTerminalExitMetadataCodecRoundTrip(t *testing.T) {
	exitedAt := time.Unix(1712345678, 987654321).UTC()
	exitCode := 23
	result, err := EncodeMethodResult("list", ListResult{Terminals: []TerminalInfo{{
		ID:        "term-1",
		Name:      "job",
		Command:   []string{"bash", "-lc", "make test"},
		State:     "exited",
		ExitCode:  &exitCode,
		ExitedAt:  exitedAt,
		CreatedAt: exitedAt.Add(-time.Hour),
	}}})
	if err != nil {
		t.Fatalf("encode list result: %v", err)
	}
	var decoded ListResult
	if err := DecodeMethodResult("list", result, &decoded); err != nil {
		t.Fatalf("decode list result: %v", err)
	}
	if len(decoded.Terminals) != 1 || decoded.Terminals[0].ExitCode == nil || *decoded.Terminals[0].ExitCode != exitCode || !decoded.Terminals[0].ExitedAt.Equal(exitedAt) {
		t.Fatalf("terminal exit metadata did not round trip: %#v", decoded)
	}

	payload, err := EncodeEventPayload(Event{Type: EventTerminalStateChanged, TerminalID: "term-1", StateChanged: &TerminalStateChangedData{
		NewState: "exited",
		ExitCode: &exitCode,
		ExitedAt: exitedAt,
	}})
	if err != nil {
		t.Fatalf("encode event: %v", err)
	}
	event, err := DecodeEventPayload(payload)
	if err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if event.StateChanged == nil || event.StateChanged.ExitCode == nil || *event.StateChanged.ExitCode != exitCode || !event.StateChanged.ExitedAt.Equal(exitedAt) {
		t.Fatalf("event exit metadata did not round trip: %#v", event)
	}
}

func TestHistoryWindowPayloadRoundTrip(t *testing.T) {
	window := &HistoryWindow{
		TerminalID: "term-hist",
		Token:      "g7:0-2:c80",
		Op:         HistoryWindowReplace,
		Size:       Size{Cols: 80, Rows: 24},
		Rows: []CompactRow{CompactRowFromCellsPreserveTrailingBlankCells([]Cell{
			{Content: "ERR", Width: 3, Style: CellStyle{FG: "ansi:1", Bold: true}},
			{Content: " ", Width: 1},
			{Content: "好", Width: 2, Style: CellStyle{FG: "#ffcc00", Underline: true}, LinkURL: "file://build.log", LinkParams: "line=7"},
			{Content: " ", Width: 1},
		}, true)},
		RowKinds:      []string{"output"},
		RowWrapped:    []bool{false},
		RowOwnership:  []string{RowOwnershipPersisted},
		RowSegments:   []string{HistoryCursorSegmentArchivedPrimaryFrame},
		RowSessionIDs: []uint64{9},
		RowFrameIDs:   []uint64{11},
		RowFixedGrid:  []bool{true},
		RowScreenCols: []int{120},
		RowIndexes:    []int{200},
		RowTimestamps: []time.Time{time.Date(2026, 6, 2, 1, 0, 0, 0, time.UTC)},
		Lines: []HistoryLineSpan{{
			StartRow:       0,
			EndRow:         0,
			RowKind:        "output",
			LogicalLineID:  42,
			SessionID:      9,
			FrameID:        11,
			FixedGrid:      true,
			ScreenCols:     120,
			TimestampStart: time.Date(2026, 6, 2, 0, 59, 0, 0, time.UTC),
			TimestampEnd:   time.Date(2026, 6, 2, 1, 1, 0, 0, time.UTC),
			ClippedBefore:  true,
			ClippedAfter:   true,
		}},
		BeforeOffset:   3,
		LoadedRows:     9,
		TotalRows:      12,
		LoadedLines:    2,
		LogicalTotal:   4,
		HasMore:        true,
		Generation:     7,
		FirstRowID:     0,
		LastRowID:      2,
		FirstLineID:    42,
		LastLineID:     43,
		CursorValid:    true,
		CursorLineID:   42,
		CursorRow:      1,
		CursorRowIndex: 200,
		CursorSegment:  HistoryCursorSegmentArchivedPrimaryFrame,
		RowLineIDs:     []uint64{42},
		RowInLine:      []int{1},
		Timestamp:      time.Date(2026, 6, 2, 2, 0, 0, 0, time.UTC),
	}
	window.Rows[0].TailFill = &CompactRowStyle{BG: "idx:24"}
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
	if !decoded.CursorValid || decoded.CursorLineID != 42 || decoded.CursorRow != 1 || decoded.CursorRowIndex != 200 || decoded.CursorSegment != HistoryCursorSegmentArchivedPrimaryFrame {
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
	if !reflect.DeepEqual(decoded.RowSegments, []string{HistoryCursorSegmentArchivedPrimaryFrame}) {
		t.Fatalf("unexpected decoded row segments: %#v", decoded.RowSegments)
	}
	if !reflect.DeepEqual(decoded.RowSessionIDs, []uint64{9}) || !reflect.DeepEqual(decoded.RowFrameIDs, []uint64{11}) || !reflect.DeepEqual(decoded.RowFixedGrid, []bool{true}) || !reflect.DeepEqual(decoded.RowScreenCols, []int{120}) || !reflect.DeepEqual(decoded.RowIndexes, []int{200}) {
		t.Fatalf("unexpected decoded row source identity: sessions=%v frames=%v fixed=%v cols=%v indexes=%v", decoded.RowSessionIDs, decoded.RowFrameIDs, decoded.RowFixedGrid, decoded.RowScreenCols, decoded.RowIndexes)
	}
	if got := compactRowToStringForTest(decoded.Rows[0]); got != "ERR 好 " {
		t.Fatalf("unexpected decoded history row: %q", got)
	}
	if decoded.Rows[0].TailFill == nil || decoded.Rows[0].TailFill.BG != "idx:24" {
		t.Fatalf("lost tail fill after payload round trip: %#v", decoded.Rows[0])
	}
	cells := decoded.Rows[0].DecodeCells()
	if len(cells) != 4 {
		t.Fatalf("expected styled history cells after payload round trip, got %#v", cells)
	}
	if cells[0].Content != "ERR" || cells[0].Width != 3 || cells[0].Style.FG != "ansi:1" || !cells[0].Style.Bold {
		t.Fatalf("lost first styled cell after payload round trip %#v", cells[0])
	}
	if cells[2].Content != "好" || cells[2].Width != 2 || cells[2].Style.FG != "#ffcc00" || !cells[2].Style.Underline || cells[2].LinkURL == "" || cells[2].LinkParams == "" {
		t.Fatalf("lost wide linked cell after payload round trip %#v", cells[2])
	}
	if cells[3].Content != " " {
		t.Fatalf("lost trailing blank cell after payload round trip %#v", cells[3])
	}
}

func TestHistoryWindowParamsControlPayloadRoundTrip(t *testing.T) {
	encoded, err := EncodeMethodParams("history.window", HistoryWindowParams{
		TerminalID:          "term-hist",
		BeforeOffset:        5,
		Limit:               50,
		Cols:                100,
		Mode:                "newer",
		Token:               "g7:c100:f42:l43",
		Generation:          7,
		CursorValid:         true,
		BeforeLineID:        42,
		BeforeRowInLine:     1,
		BeforeRowIndex:      200,
		CursorSegment:       HistoryCursorSegmentArchivedPrimaryFrame,
		AfterCursorValid:    true,
		AfterLineID:         43,
		AfterRowInLine:      2,
		AfterRowIndex:       240,
		AfterCursorSegment:  HistoryCursorSegmentCurrentAltFrame,
		BoundaryFirstLineID: 42,
		BoundaryLastLineID:  43,
		RangeValid:          true,
		RangeStartLineID:    40,
		RangeStartCol:       3,
		RangeEndLineID:      44,
		RangeEndCol:         9,
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
		Mode:                "newer",
		Token:               "g7:c100:f42:l43",
		Generation:          7,
		CursorValid:         true,
		BeforeLineID:        42,
		BeforeRowInLine:     1,
		BeforeRowIndex:      200,
		CursorSegment:       HistoryCursorSegmentArchivedPrimaryFrame,
		AfterCursorValid:    true,
		AfterLineID:         43,
		AfterRowInLine:      2,
		AfterRowIndex:       240,
		AfterCursorSegment:  HistoryCursorSegmentCurrentAltFrame,
		BoundaryFirstLineID: 42,
		BoundaryLastLineID:  43,
		RangeValid:          true,
		RangeStartLineID:    40,
		RangeStartCol:       3,
		RangeEndLineID:      44,
		RangeEndCol:         9,
	}) {
		t.Fatalf("unexpected decoded history window params: %#v", params)
	}
}

func TestHistoryReleaseParamsControlPayloadRoundTrip(t *testing.T) {
	encoded, err := EncodeMethodParams("history.release", HistoryWindowParams{
		TerminalID: "term-hist",
		Token:      "snap-token",
	})
	if err != nil {
		t.Fatalf("encode history release params failed: %v", err)
	}
	decoded, err := DecodeMethodParams("history.release", encoded)
	if err != nil {
		t.Fatalf("decode history release params failed: %v", err)
	}
	params, ok := decoded.(HistoryWindowParams)
	if !ok {
		t.Fatalf("expected HistoryWindowParams, got %T", decoded)
	}
	if params.TerminalID != "term-hist" || params.Token != "snap-token" {
		t.Fatalf("unexpected history release params: %#v", params)
	}
}

func TestDetachParamsControlPayloadRoundTripKeepsAttachmentIdentity(t *testing.T) {
	params := DetachParams{
		TerminalID: "term-1",
		Channel:    7,
		SurfaceID:  "surface-1",
		ViewID:     "view-1",
	}
	encoded, err := EncodeMethodParams("detach", params)
	if err != nil {
		t.Fatalf("encode detach params failed: %v", err)
	}
	decoded, err := DecodeMethodParams("detach", encoded)
	if err != nil {
		t.Fatalf("decode detach params failed: %v", err)
	}
	got, ok := decoded.(DetachParams)
	if !ok {
		t.Fatalf("expected DetachParams, got %T", decoded)
	}
	if got != params {
		t.Fatalf("unexpected decoded detach params: %#v", got)
	}
}

func TestInputParamsControlPayloadRoundTripKeepsViewIdentityAndBytes(t *testing.T) {
	params := InputParams{
		TerminalID: "term-1",
		Channel:    7,
		SurfaceID:  "surface-1",
		ViewID:     "view-1",
		Data:       []byte("ls\n\x00raw"),
	}
	encoded, err := EncodeMethodParams("input", params)
	if err != nil {
		t.Fatalf("encode input params failed: %v", err)
	}
	decoded, err := DecodeMethodParams("input", encoded)
	if err != nil {
		t.Fatalf("decode input params failed: %v", err)
	}
	got, ok := decoded.(InputParams)
	if !ok {
		t.Fatalf("expected InputParams, got %T", decoded)
	}
	if got.TerminalID != params.TerminalID || got.Channel != params.Channel || got.SurfaceID != params.SurfaceID || got.ViewID != params.ViewID || !bytes.Equal(got.Data, params.Data) {
		t.Fatalf("unexpected decoded input params: %#v", got)
	}
}
