package protocol

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
	"unsafe"
)

func TestSnapshotUnmarshalJSON(t *testing.T) {
	raw := []byte(`{
		"terminal_id": "term-1",
		"size": {"cols": 80, "rows": 24},
		"screen": {
			"is_alternate": false,
			"rows": [
				{"cells": [{"r": "h"}, {"r": "i", "s": {"fg": "#ff0000", "b": true}}]}
			]
		},
		"scrollback": [
			{"cells": [{"r": "o"}, {"r": "k"}]}
		],
		"screen_timestamps": ["2026-03-18T00:00:02Z"],
		"scrollback_timestamps": ["2026-03-18T00:00:01Z"],
		"screen_row_kinds": ["restart"],
		"scrollback_row_kinds": ["restart"],
		"cursor": {"row": 1, "col": 2, "visible": true, "shape": "block"},
		"modes": {"alternate_screen": false, "alternate_scroll": true, "mouse_tracking": false, "bracketed_paste": true, "application_cursor": false, "auto_wrap": true},
		"timestamp": "2026-03-18T00:00:00Z"
	}`)

	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("unmarshal snapshot failed: %v", err)
	}

	if snap.TerminalID != "term-1" || snap.Size.Cols != 80 || snap.Size.Rows != 24 {
		t.Fatalf("unexpected snapshot header: %#v", snap)
	}
	if len(snap.Screen.Cells) != 1 || len(snap.Screen.Cells[0]) != 2 {
		t.Fatalf("unexpected screen cells: %#v", snap.Screen.Cells)
	}
	if snap.Screen.Cells[0][1].Style.FG != "#ff0000" || !snap.Screen.Cells[0][1].Style.Bold {
		t.Fatalf("unexpected styled cell: %#v", snap.Screen.Cells[0][1])
	}
	if row := snap.Scrollback[0].DecodeCells(); len(snap.Scrollback) != 1 || len(row) == 0 || row[0].Content != "o" {
		t.Fatalf("unexpected scrollback: %#v", snap.Scrollback)
	}
	if len(snap.ScreenTimestamps) != 1 || !snap.ScreenTimestamps[0].Equal(time.Date(2026, 3, 18, 0, 0, 2, 0, time.UTC)) {
		t.Fatalf("unexpected screen timestamps: %#v", snap.ScreenTimestamps)
	}
	if len(snap.ScrollbackTimestamps) != 1 || !snap.ScrollbackTimestamps[0].Equal(time.Date(2026, 3, 18, 0, 0, 1, 0, time.UTC)) {
		t.Fatalf("unexpected scrollback timestamps: %#v", snap.ScrollbackTimestamps)
	}
	if len(snap.ScreenRowKinds) != 1 || snap.ScreenRowKinds[0] != SnapshotRowKindRestart {
		t.Fatalf("unexpected screen row kinds: %#v", snap.ScreenRowKinds)
	}
	if len(snap.ScrollbackRowKinds) != 1 || snap.ScrollbackRowKinds[0] != SnapshotRowKindRestart {
		t.Fatalf("unexpected scrollback row kinds: %#v", snap.ScrollbackRowKinds)
	}
	if !snap.Modes.BracketedPaste || !snap.Modes.AlternateScroll || !snap.Cursor.Visible || snap.Cursor.Shape != "block" {
		t.Fatalf("unexpected cursor or modes: %#v %#v", snap.Cursor, snap.Modes)
	}
}

func TestSnapshotUnmarshalCompactRows(t *testing.T) {
	raw := []byte(`{
		"terminal_id": "term-compact",
		"size": {"cols": 80, "rows": 24},
		"screen": {
			"is_alternate": false,
			"rows": [
				{"t": "plain"},
				{"runs": [{"t": "red", "s": {"fg": "#ff0000"}}, {"t": "bold", "s": {"b": true}}]}
			]
		},
		"scrollback": [
			{"t": "history"}
		],
		"cursor": {"row": 1, "col": 2, "visible": true, "shape": "block"},
		"modes": {"auto_wrap": true},
		"timestamp": "2026-03-18T00:00:00Z"
	}`)

	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("unmarshal compact snapshot failed: %v", err)
	}
	if got := rowToStringForTest(snap.Screen.Cells[0]); got != "plain" {
		t.Fatalf("unexpected compact text row: %q", got)
	}
	if got := rowToStringForTest(snap.Screen.Cells[1]); got != "redbold" {
		t.Fatalf("unexpected compact run row: %q", got)
	}
	if snap.Screen.Cells[1][0].Style.FG != "#ff0000" || !snap.Screen.Cells[1][3].Style.Bold {
		t.Fatalf("unexpected run styles: %#v", snap.Screen.Cells[1])
	}
	if got := compactRowToStringForTest(snap.Scrollback[0]); got != "history" {
		t.Fatalf("unexpected compact scrollback row: %q", got)
	}
}

func TestSnapshotUnmarshalCompactRowsReusesASCIIStrings(t *testing.T) {
	raw := []byte(`{
		"terminal_id": "term-compact-ascii",
		"size": {"cols": 80, "rows": 24},
		"screen": {"rows": [{"t": "aaaa"}]},
		"scrollback": [{"t": "aaaa"}],
		"cursor": {"visible": true},
		"modes": {"auto_wrap": true},
		"timestamp": "2026-03-18T00:00:00Z"
	}`)
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("unmarshal compact snapshot failed: %v", err)
	}
	if len(snap.Screen.Cells) != 1 || len(snap.Screen.Cells[0]) != 4 {
		t.Fatalf("unexpected compact row: %#v", snap.Screen.Cells)
	}
	first := snap.Screen.Cells[0][0].Content
	for i, cell := range snap.Screen.Cells[0] {
		if cell.Content != first {
			t.Fatalf("unexpected content at %d: %q want %q", i, cell.Content, first)
		}
		if unsafe.StringData(cell.Content) != unsafe.StringData(first) {
			t.Fatalf("expected ASCII cell content to reuse backing string at %d", i)
		}
	}
}

func TestGridViewportMarshalUsesCompactRows(t *testing.T) {
	viewport := GridViewport{
		TerminalID: "term-compact",
		Size:       Size{Cols: 80, Rows: 24},
		Rows: CompactRowsFromCells([][]Cell{
			{{Content: "o", Width: 1}, {Content: "k", Width: 1}},
			{{Content: "r", Width: 1, Style: CellStyle{Bold: true}}, {Content: "u", Width: 1, Style: CellStyle{Bold: true}}},
		}),
		Timestamp: time.Date(2026, 3, 18, 0, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(viewport)
	if err != nil {
		t.Fatalf("marshal compact viewport failed: %v", err)
	}
	if bytes.Contains(data, []byte(`"cells"`)) {
		t.Fatalf("expected compact viewport rows without cells fallback, got %s", data)
	}
	if !bytes.Contains(data, []byte(`"t":"ok"`)) || !bytes.Contains(data, []byte(`"runs"`)) {
		t.Fatalf("expected compact text and runs in viewport JSON, got %s", data)
	}
	var decoded GridViewport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal compact viewport failed: %v", err)
	}
	if got := compactRowToStringForTest(decoded.Rows[0]); got != "ok" {
		t.Fatalf("unexpected decoded compact text row: %q", got)
	}
	decodedRun := decoded.Rows[1].DecodeCells()
	if got := rowToStringForTest(decodedRun); got != "ru" || len(decodedRun) == 0 || !decodedRun[0].Style.Bold {
		t.Fatalf("unexpected decoded compact run row: %q %#v", got, decoded.Rows[1])
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

func TestScreenUpdatePayloadFullReplaceTrimsTrailingBlankCells(t *testing.T) {
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
	if len(row) != 2 {
		t.Fatalf("expected full replace trailing plain blank to be trimmed, got %#v", row)
	}
	if got := row[1].Style.BG; got != "#112233" {
		t.Fatalf("expected styled trailing blank cell preserved, got %#v", row[1])
	}
}

func TestScreenUpdatePayloadCurrentRoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 12, 1, 2, 3, 0, time.UTC)
	update := ScreenUpdate{
		FullReplace:     true,
		ResetScrollback: true,
		Size:            protocolSize(10, 4),
		Title:           "ops-demo",
		Screen: ScreenData{
			Cells:             [][]Cell{rowWithTextAt(10, 0, "abc"), rowWithTextAt(10, 0, "def")},
			IsAlternateScreen: true,
		},
		ScreenTimestamps: []time.Time{now, now.Add(time.Second)},
		ScreenRowKinds:   []string{"a", "b"},
		ScreenWrapped:    []bool{true, false},
		ScreenScroll:     1,
		Ops: []ScreenOp{
			{Code: ScreenOpScrollRect, Rect: ScreenRect{X: 0, Y: 0, Width: 10, Height: 4}, Dy: -1},
			{Code: ScreenOpWriteSpan, Row: 3, Col: 0, Cells: []Cell{{Content: "n", Width: 1}, {Content: "e", Width: 1}, {Content: "w", Width: 1}}, Timestamp: now.Add(2 * time.Second), RowKind: "tail", Wrapped: true, WrappedSet: true},
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
			Cells:      rowWithTextAt(10, 0, "old"),
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
	if len(decoded.Ops) != len(update.Ops) {
		t.Fatalf("expected %d decoded ops, got %#v", len(update.Ops), decoded.Ops)
	}
	if decoded.Ops[1].Code != ScreenOpWriteSpan || !decoded.Ops[1].WrappedSet || !decoded.Ops[1].Wrapped {
		t.Fatalf("unexpected write op wrapped metadata: %#v", decoded.Ops[1])
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
}

func TestDecodeScreenUpdatePayloadRejectsLegacyPayloads(t *testing.T) {
	for _, payload := range [][]byte{
		[]byte(`{"size":{"cols":4,"rows":1}}`),
		[]byte("TSU2\x00"),
		[]byte("TSU3\x00"),
		[]byte("TSU4\x00"),
		[]byte("TSU5\x00"),
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
