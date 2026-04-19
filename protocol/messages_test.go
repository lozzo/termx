package protocol

import (
	"bytes"
	"encoding/json"
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

func protocolSize(cols, rows uint16) Size {
	return Size{Cols: cols, Rows: rows}
}
