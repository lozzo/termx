package render

import (
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestCopyHistoryContentProjectsAuthoritativeRows(t *testing.T) {
	history := state.HistoryStore{
		Cols: 20,
		Rows: []state.HistoryRow{
			{
				Text:   "alpha",
				LineID: 1,
				Cells:  []state.HistoryCell{{Text: "alpha", Width: 5, Style: state.HistoryCellStyle{FG: "ansi:2", Bold: true}}},
			},
			{Text: "beta", LineID: 2},
		},
	}
	copyMode := state.CopyModeStore{
		Active:      true,
		ViewRows:    1,
		ViewportTop: 1,
		Cursor:      state.CopyPosition{Row: 1, Col: 2},
		BoundCols:   20,
	}
	content := buildCopyHistoryContent(history, copyMode)
	if content.Kind != ContentCopyHistory || len(content.Lines) != 1 {
		t.Fatalf("expected one copy history line, got %#v", content)
	}
	if got := content.Lines[0].PlainString(); got != "beta" {
		t.Fatalf("expected visible row beta, got %q", got)
	}
	if !content.Cursor.Visible || content.Cursor.Row != 0 || content.Cursor.Col != 2 {
		t.Fatalf("expected cursor in visible row, got %#v", content.Cursor)
	}
	if len(content.HitRegions) != 1 || content.HitRegions[0].Kind != HitRegionHistoryRow || content.HitRegions[0].Row != 1 {
		t.Fatalf("expected history row hit region, got %#v", content.HitRegions)
	}

	copyMode.ViewportTop = 0
	copyMode.Cursor = state.CopyPosition{Row: 0}
	content = buildCopyHistoryContent(history, copyMode)
	if got := content.Lines[0].Cells[0].ANSIStyle; got.FG != "ansi:2" || !got.Bold {
		t.Fatalf("expected terminal ANSI style from history cell, got %#v", got)
	}
}
