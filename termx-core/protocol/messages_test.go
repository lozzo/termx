package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
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
	if len(snap.Scrollback) != 1 || snap.Scrollback[0][0].Content != "o" {
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
		ChangedRows: []ScreenRowUpdate{{
			Row: 0,
			Cells: []Cell{
				{Content: "A", Width: 1},
				{Content: "界", Width: 2},
				{Content: "", Width: 0},
				{Content: " ", Width: 1},
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
		t.Fatalf("expected binary screen update magic, got prefix %q", payload[:minInt(len(payload), 8)])
	}
	decoded, err := DecodeScreenUpdatePayload(payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(decoded.ChangedRows) != 1 {
		t.Fatalf("expected one changed row, got %#v", decoded.ChangedRows)
	}
	row := decoded.ChangedRows[0].Cells
	if len(row) != 3 {
		t.Fatalf("expected trailing plain blanks to be trimmed while keeping wide continuation, got %#v", row)
	}
	if row[1].Content != "界" || row[1].Width != 2 {
		t.Fatalf("expected wide lead preserved, got %#v", row[1])
	}
	if row[2].Content != "" || row[2].Width != 0 {
		t.Fatalf("expected wide continuation preserved, got %#v", row[2])
	}
}

func TestScreenUpdatePayloadKeepsStyledTrailingBlankCell(t *testing.T) {
	update := ScreenUpdate{
		Size:         protocolSize(4, 1),
		ScreenScroll: 1,
		ChangedRows: []ScreenRowUpdate{{
			Row: 0,
			Cells: []Cell{
				{Content: "X", Width: 1},
				{Content: " ", Width: 1, Style: CellStyle{BG: "#112233"}},
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
	decoded, err := DecodeScreenUpdatePayload(payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	row := decoded.ChangedRows[0].Cells
	if len(row) != 2 {
		t.Fatalf("expected styled trailing blank to remain on the wire, got %#v", row)
	}
	if got := row[1].Style.BG; got != "#112233" {
		t.Fatalf("expected styled trailing blank cell preserved, got %#v", row[1])
	}
	if decoded.ScreenScroll != 1 {
		t.Fatalf("expected screen scroll round-trip, got %#v", decoded)
	}
}

func TestDecodeScreenUpdatePayloadRejectsLegacyJSON(t *testing.T) {
	raw := []byte(`{
		"size": {"cols": 4, "rows": 1},
		"changed_rows": [{
			"row": 0,
			"cells": [{"r":"o","w":1},{"r":"k","w":1}],
			"timestamp": "2026-04-18T00:00:01Z",
			"row_kind": "legacy"
		}],
		"cursor": {"row": 0, "col": 2, "visible": true, "shape": "block"},
		"modes": {"alternate_screen": false, "mouse_tracking": false, "bracketed_paste": false, "application_cursor": false, "auto_wrap": true}
	}`)

	if _, err := DecodeScreenUpdatePayload(raw); err == nil {
		t.Fatal("expected legacy json screen update payload to be rejected")
	}
}

func TestNormalizeScreenUpdateDeduplicatesChangedRowsAndAlignsMetadata(t *testing.T) {
	now := time.Now().UTC()
	later := now.Add(time.Second)
	update := NormalizeScreenUpdate(ScreenUpdate{
		FullReplace: true,
		Screen: ScreenData{Cells: [][]Cell{
			{{Content: "a", Width: 1}},
			{{Content: "b", Width: 1}},
		}},
		ScreenTimestamps: []time.Time{now},
		ScreenRowKinds:   []string{"restart", "stale", "overflow"},
		ChangedRows: []ScreenRowUpdate{
			{Row: 1, Cells: []Cell{{Content: "old", Width: 1}}, Timestamp: now},
			{Row: 1, Cells: []Cell{{Content: "new", Width: 1}}, Timestamp: later},
		},
	})

	if len(update.ScreenTimestamps) != 2 {
		t.Fatalf("expected screen timestamps normalized to screen height, got %#v", update.ScreenTimestamps)
	}
	if len(update.ScreenRowKinds) != 2 {
		t.Fatalf("expected screen row kinds normalized to screen height, got %#v", update.ScreenRowKinds)
	}
	if len(update.ChangedRows) != 1 {
		t.Fatalf("expected duplicate changed rows collapsed, got %#v", update.ChangedRows)
	}
	if got := update.ChangedRows[0].Cells[0].Content; got != "new" {
		t.Fatalf("expected last changed row to win, got %#v", update.ChangedRows[0])
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

func TestDecodeScreenUpdatePayloadKeepsTSU2Compatibility(t *testing.T) {
	update := ScreenUpdate{
		Size: protocolSize(12, 2),
		ChangedRows: []ScreenRowUpdate{{
			Row: 1,
			Cells: []Cell{
				{Content: "o", Width: 1},
				{Content: "k", Width: 1},
			},
			Timestamp: time.Date(2026, 4, 20, 1, 0, 0, 0, time.UTC),
			RowKind:   "legacy-row",
		}},
		Cursor: CursorState{Row: 1, Col: 2, Visible: true},
		Modes:  TerminalModes{AutoWrap: true},
	}

	payload, err := encodeScreenUpdatePayloadBinaryV2(update)
	if err != nil {
		t.Fatalf("encode tsu2 payload: %v", err)
	}
	decoded, err := DecodeScreenUpdatePayload(payload)
	if err != nil {
		t.Fatalf("decode tsu2 payload: %v", err)
	}
	if len(decoded.ChangedRows) != 1 {
		t.Fatalf("expected legacy changed rows preserved, got %#v", decoded.ChangedRows)
	}
	if len(decoded.ChangedSpans) != 1 {
		t.Fatalf("expected tsu2 decode to synthesize one replace-row span, got %#v", decoded.ChangedSpans)
	}
	if decoded.ChangedSpans[0].Op != ScreenSpanOpReplaceRow || decoded.ChangedSpans[0].Row != 1 {
		t.Fatalf("unexpected synthesized replace-row span: %#v", decoded.ChangedSpans[0])
	}
}

func TestScreenUpdatePayloadTSU4RoundTrip(t *testing.T) {
	update := ScreenUpdate{
		Size:         protocolSize(10, 4),
		ScreenScroll: 1,
		Title:        "ops-demo",
		Ops: []ScreenOp{
			{Code: ScreenOpScrollRect, Rect: ScreenRect{X: 0, Y: 0, Width: 10, Height: 4}, Dy: -1},
			{Code: ScreenOpWriteSpan, Row: 3, Col: 0, Cells: []Cell{{Content: "n", Width: 1}, {Content: "e", Width: 1}, {Content: "w", Width: 1}}, Timestamp: time.Date(2026, 4, 20, 2, 0, 0, 0, time.UTC), RowKind: "tail"},
			{Code: ScreenOpCopyRect, Src: ScreenRect{X: 0, Y: 0, Width: 4, Height: 1}, DstX: 5, DstY: 1},
			{Code: ScreenOpClearRect, Rect: ScreenRect{X: 7, Y: 0, Width: 3, Height: 2}, Timestamp: time.Date(2026, 4, 20, 2, 0, 1, 0, time.UTC), RowKind: "clear"},
			{Code: ScreenOpClearToEOL, Row: 2, Col: 4, Timestamp: time.Date(2026, 4, 20, 2, 0, 2, 0, time.UTC), RowKind: "eol"},
			{Code: ScreenOpCursor, Cursor: CursorState{Row: 3, Col: 3, Visible: true, Shape: "bar"}},
			{Code: ScreenOpModes, Modes: TerminalModes{AlternateScreen: true, AutoWrap: true, MouseTracking: true}},
			{Code: ScreenOpResize, Size: Size{Cols: 10, Rows: 4}},
			{Code: ScreenOpTitle, Title: "ops-demo"},
		},
		Cursor: CursorState{Row: 3, Col: 3, Visible: true, Shape: "bar"},
		Modes:  TerminalModes{AlternateScreen: true, AutoWrap: true, MouseTracking: true},
	}

	payload, err := encodeScreenUpdatePayloadBinaryV4(update)
	if err != nil {
		t.Fatalf("encode tsu4 payload: %v", err)
	}
	if !bytes.HasPrefix(payload, []byte(screenUpdatePayloadMagicV4)) {
		t.Fatalf("expected tsu4 magic prefix, got %q", payload[:minInt(len(payload), 8)])
	}
	decoded, err := DecodeScreenUpdatePayload(payload)
	if err != nil {
		t.Fatalf("decode tsu4 payload: %v", err)
	}
	if decoded.Title != update.Title || decoded.ScreenScroll != update.ScreenScroll {
		t.Fatalf("unexpected tsu4 header round-trip: %#v", decoded)
	}
	if len(decoded.Ops) != len(update.Ops) {
		t.Fatalf("expected %d decoded ops, got %#v", len(update.Ops), decoded.Ops)
	}
	if decoded.Ops[0].Code != ScreenOpScrollRect || decoded.Ops[0].Dy != -1 {
		t.Fatalf("unexpected scrollrect op: %#v", decoded.Ops[0])
	}
	if decoded.Ops[2].Code != ScreenOpCopyRect || decoded.Ops[2].DstX != 5 || decoded.Ops[2].DstY != 1 {
		t.Fatalf("unexpected copyrect op: %#v", decoded.Ops[2])
	}
	if decoded.Ops[5].Cursor.Shape != "bar" || !decoded.Ops[6].Modes.MouseTracking {
		t.Fatalf("unexpected control op round-trip: %#v %#v", decoded.Ops[5], decoded.Ops[6])
	}
}

func TestEncodeScreenUpdatePayloadPrefersTSU3WhenSmaller(t *testing.T) {
	update := ScreenUpdate{
		Size:         protocolSize(80, 24),
		ScreenScroll: 1,
		ChangedSpans: []ScreenSpanUpdate{{
			Row:      23,
			ColStart: 0,
			Cells: []Cell{
				{Content: "l", Width: 1},
				{Content: "e", Width: 1},
				{Content: "s", Width: 1},
				{Content: "s", Width: 1},
				{Content: "-", Width: 1},
				{Content: "2", Width: 1},
				{Content: "4", Width: 1},
			},
			Op: ScreenSpanOpWrite,
		}},
		Ops: []ScreenOp{{
			Code: ScreenOpScrollRect,
			Rect: ScreenRect{X: 0, Y: 0, Width: 80, Height: 24},
			Dy:   -1,
		}, {
			Code: ScreenOpWriteSpan,
			Row:  23,
			Col:  0,
			Cells: []Cell{
				{Content: "l", Width: 1},
				{Content: "e", Width: 1},
				{Content: "s", Width: 1},
				{Content: "s", Width: 1},
				{Content: "-", Width: 1},
				{Content: "2", Width: 1},
				{Content: "4", Width: 1},
			},
		}},
		Cursor: CursorState{Row: 23, Col: 0, Visible: true},
		Modes:  TerminalModes{AutoWrap: true},
	}

	payload, err := EncodeScreenUpdatePayload(update)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	if !bytes.HasPrefix(payload, []byte(screenUpdatePayloadMagicV3)) {
		t.Fatalf("expected TSU3 fallback for smaller payload, got prefix %q", payload[:minInt(len(payload), 8)])
	}
}

func TestEncodeScreenUpdatePayloadPrefersTSU4WhenSmaller(t *testing.T) {
	rows := make([]ScreenRowUpdate, 0, 10)
	for i := 0; i < 10; i++ {
		rows = append(rows, ScreenRowUpdate{
			Row:   20 + i,
			Cells: rowWithTextAt(120, 0, strings.Repeat(string(rune('A'+i)), 120)),
		})
	}
	update := ScreenUpdate{
		Size:        protocolSize(120, 40),
		ChangedRows: rows,
		Ops: []ScreenOp{{
			Code: ScreenOpCopyRect,
			Src:  ScreenRect{X: 0, Y: 5, Width: 120, Height: 10},
			DstX: 0,
			DstY: 20,
		}},
		Cursor: CursorState{Row: 20, Col: 0, Visible: true},
		Modes:  TerminalModes{AlternateScreen: true, AutoWrap: true},
	}

	payload, err := EncodeScreenUpdatePayload(update)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	if !bytes.HasPrefix(payload, []byte(screenUpdatePayloadMagicV4)) {
		t.Fatalf("expected TSU4 when opcode payload is smaller, got prefix %q", payload[:minInt(len(payload), 8)])
	}
}

func TestEncodeScreenUpdatePayloadKeepsTSU4WhenV3WouldDropOpcodeOnlyContent(t *testing.T) {
	update := ScreenUpdate{
		Size:         protocolSize(80, 24),
		ScreenScroll: 1,
		Ops: []ScreenOp{
			{Code: ScreenOpScrollRect, Rect: ScreenRect{X: 0, Y: 0, Width: 80, Height: 24}, Dy: -1},
			{Code: ScreenOpWriteSpan, Row: 23, Col: 0, Cells: []Cell{{Content: "x", Width: 1}}},
		},
		Cursor: CursorState{Row: 23, Col: 1, Visible: true},
		Modes:  TerminalModes{AutoWrap: true},
	}

	payload, err := EncodeScreenUpdatePayload(update)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	if !bytes.HasPrefix(payload, []byte(screenUpdatePayloadMagicV4)) {
		t.Fatalf("expected TSU4 to preserve opcode-only content, got prefix %q", payload[:minInt(len(payload), 8)])
	}
}

func TestScreenUpdatePayloadTSU3WireCases(t *testing.T) {
	type wireCase struct {
		name     string
		legacy   ScreenUpdate
		modern   ScreenUpdate
		wantTSU2 int
		wantTSU3 int
	}

	styleOnlyRow := filledRow(32)
	writeText(styleOnlyRow, 0, "plain-text")
	styleOnlyRow[5] = Cell{Content: "x", Width: 1, Style: CellStyle{FG: "#112233", Bold: true}}

	wideRow := filledRow(24)
	writeText(wideRow, 0, "A")
	writeWideCell(wideRow, 1, Cell{Content: "界", Width: 2})
	writeText(wideRow, 3, "B")

	resizeRow := filledRow(96)
	writeText(resizeRow, 72, "ok")

	cases := []wireCase{
		{
			name: "high_col_single_char_change",
			legacy: ScreenUpdate{
				Size: protocolSize(160, 1),
				ChangedRows: []ScreenRowUpdate{{
					Row:   0,
					Cells: rowWithTextAt(160, 137, "Z"),
				}},
				Cursor: CursorState{Row: 0, Col: 138, Visible: true},
				Modes:  TerminalModes{AutoWrap: true},
			},
			modern: ScreenUpdate{
				Size: protocolSize(160, 1),
				ChangedSpans: []ScreenSpanUpdate{{
					Row:      0,
					ColStart: 137,
					Cells:    []Cell{{Content: "Z", Width: 1}},
					Op:       ScreenSpanOpWrite,
				}},
				Cursor: CursorState{Row: 0, Col: 138, Visible: true},
				Modes:  TerminalModes{AutoWrap: true},
			},
			wantTSU2: 590,
			wantTSU3: 44,
		},
		{
			name: "clear_to_eol",
			legacy: ScreenUpdate{
				Size: protocolSize(120, 1),
				ChangedRows: []ScreenRowUpdate{{
					Row:   0,
					Cells: rowWithTextAt(120, 0, "prefix"),
				}},
				Cursor: CursorState{Row: 0, Col: 6, Visible: true},
				Modes:  TerminalModes{AutoWrap: true},
			},
			modern: ScreenUpdate{
				Size: protocolSize(120, 1),
				ChangedSpans: []ScreenSpanUpdate{{
					Row:      0,
					ColStart: 6,
					Op:       ScreenSpanOpClearToEOL,
				}},
				Cursor: CursorState{Row: 0, Col: 6, Visible: true},
				Modes:  TerminalModes{AutoWrap: true},
			},
			wantTSU2: 61,
			wantTSU3: 38,
		},
		{
			name: "mid_row_style_change",
			legacy: ScreenUpdate{
				Size: protocolSize(32, 1),
				ChangedRows: []ScreenRowUpdate{{
					Row:   0,
					Cells: styleOnlyRow,
				}},
				Cursor: CursorState{Row: 0, Col: 6, Visible: true},
				Modes:  TerminalModes{AutoWrap: true},
			},
			modern: ScreenUpdate{
				Size: protocolSize(32, 1),
				ChangedSpans: []ScreenSpanUpdate{{
					Row:      0,
					ColStart: 5,
					Cells:    []Cell{{Content: "x", Width: 1, Style: CellStyle{FG: "#112233", Bold: true}}},
					Op:       ScreenSpanOpWrite,
				}},
				Cursor: CursorState{Row: 0, Col: 6, Visible: true},
				Modes:  TerminalModes{AutoWrap: true},
			},
			wantTSU2: 87,
			wantTSU3: 53,
		},
		{
			name: "wide_char_boundary",
			legacy: ScreenUpdate{
				Size: protocolSize(24, 1),
				ChangedRows: []ScreenRowUpdate{{
					Row:   0,
					Cells: wideRow,
				}},
				Cursor: CursorState{Row: 0, Col: 4, Visible: true},
				Modes:  TerminalModes{AutoWrap: true},
			},
			modern: ScreenUpdate{
				Size: protocolSize(24, 1),
				ChangedSpans: []ScreenSpanUpdate{{
					Row:      0,
					ColStart: 1,
					Cells: []Cell{
						{Content: "界", Width: 2},
						{Content: "", Width: 0},
					},
					Op: ScreenSpanOpWrite,
				}},
				Cursor: CursorState{Row: 0, Col: 4, Visible: true},
				Modes:  TerminalModes{AutoWrap: true},
			},
			wantTSU2: 54,
			wantTSU3: 48,
		},
		{
			name: "resize_then_incremental_span",
			legacy: ScreenUpdate{
				Size: protocolSize(96, 4),
				ChangedRows: []ScreenRowUpdate{{
					Row:   3,
					Cells: resizeRow,
				}},
				Cursor: CursorState{Row: 3, Col: 74, Visible: true},
				Modes:  TerminalModes{AlternateScreen: true, AutoWrap: true},
			},
			modern: ScreenUpdate{
				Size: protocolSize(96, 4),
				ChangedSpans: []ScreenSpanUpdate{{
					Row:      3,
					ColStart: 72,
					Cells: []Cell{
						{Content: "o", Width: 1},
						{Content: "k", Width: 1},
					},
					Op: ScreenSpanOpWrite,
				}},
				Cursor: CursorState{Row: 3, Col: 74, Visible: true},
				Modes:  TerminalModes{AlternateScreen: true, AutoWrap: true},
			},
			wantTSU2: 333,
			wantTSU3: 47,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v2Payload, err := encodeScreenUpdatePayloadBinaryV2(tc.legacy)
			if err != nil {
				t.Fatalf("encode tsu2 payload: %v", err)
			}
			v3Payload, err := EncodeScreenUpdatePayload(tc.modern)
			if err != nil {
				t.Fatalf("encode tsu3 payload: %v", err)
			}
			if len(v2Payload) != tc.wantTSU2 {
				t.Fatalf("unexpected tsu2 payload size: got %d want %d", len(v2Payload), tc.wantTSU2)
			}
			if len(v3Payload) != tc.wantTSU3 {
				t.Fatalf("unexpected tsu3 payload size: got %d want %d", len(v3Payload), tc.wantTSU3)
			}
			decoded, err := DecodeScreenUpdatePayload(v3Payload)
			if err != nil {
				t.Fatalf("decode tsu3 payload: %v", err)
			}
			if len(decoded.ChangedSpans) != len(NormalizeScreenUpdate(tc.modern).ChangedSpans) {
				t.Fatalf("unexpected decoded spans: %#v", decoded.ChangedSpans)
			}
			if !bytes.HasPrefix(v3Payload, []byte(screenUpdatePayloadMagicV3)) {
				t.Fatalf("expected tsu3 magic prefix, got %q", v3Payload[:minInt(len(v3Payload), 8)])
			}
			if len(v3Payload) >= len(v2Payload) {
				t.Fatalf("expected tsu3 payload smaller than tsu2 for %s, got tsu3=%d tsu2=%d", tc.name, len(v3Payload), len(v2Payload))
			}
		})
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

func writeWideCell(row []Cell, col int, cell Cell) {
	if col < 0 || col >= len(row) {
		return
	}
	row[col] = cell
	for offset := 1; offset < cell.Width && col+offset < len(row); offset++ {
		row[col+offset] = Cell{Content: "", Width: 0, Style: cell.Style}
	}
}
