package protocol

import (
	"encoding/json"
	"testing"
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
		"cursor": {"row": 1, "col": 2, "visible": true, "shape": "block"},
		"modes": {"alternate_screen": false, "mouse_tracking": false, "bracketed_paste": true, "application_cursor": false, "auto_wrap": true},
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
	if !snap.Modes.BracketedPaste || !snap.Cursor.Visible || snap.Cursor.Shape != "block" {
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
