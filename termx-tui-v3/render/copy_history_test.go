package render

import (
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestCopyHistoryContentANSILineAtOffsetsInternalColumnAnchors(t *testing.T) {
	history := state.HistoryStore{
		Rows: []state.HistoryRow{{
			Text:   "099975",
			LineID: 99,
			Cells: []state.HistoryCell{
				{Text: "0", Width: 1},
				{Text: "99975", Width: 5},
			},
		}},
	}

	got := CopyHistoryContentANSILineAt(history, state.CopyModeStore{}, 0, 10, 1, DefaultTheme())
	if !strings.Contains(got, "0\x1b[3G99975") || strings.Contains(got, "0\x1b[2G99975") {
		t.Fatalf("pane-local patch must offset ANSI column anchors, got %q", got)
	}
}

func TestCopyHistoryLinePadsWrappedTailBackgroundForDisplayOnly(t *testing.T) {
	history := state.HistoryStore{
		Cols: 10,
		Rows: []state.HistoryRow{{
			Text:      "tail",
			LineID:    99,
			RowInLine: 1,
			Cells: []state.HistoryCell{{
				Text:  "tail",
				Width: 4,
				Style: state.HistoryCellStyle{FG: "idx:203", BG: "idx:24", Bold: true},
			}},
		}},
	}

	lines := copyHistoryLines(history, state.CopyModeStore{BoundCols: 10})
	if len(lines) != 1 {
		t.Fatalf("expected one history line, got %d", len(lines))
	}
	if got := lines[0].PlainString(); got != "tail      " {
		t.Fatalf("wrapped history row should materialize display tail blanks, got %q", got)
	}
	if got := history.Rows[0].Text; got != "tail" {
		t.Fatalf("display-only tail padding must not mutate logical row text, got %q", got)
	}
	last := lines[0].Cells[len(lines[0].Cells)-1]
	if last.Width != 6 || last.ANSIStyle != (ANSICellStyle{BG: "idx:24"}) || last.TerminalContent != true {
		t.Fatalf("tail padding should keep only background footprint, got %#v", last)
	}

	ansi := CopyHistoryContentANSILineAt(history, state.CopyModeStore{BoundCols: 10}, 0, 10, 0, DefaultTheme())
	if !strings.Contains(ansi, "\x1b[48;5;24m      ") {
		t.Fatalf("ANSI history line should paint wrapped tail background, got %q", ansi)
	}
	if strings.Contains(ansi, "\x1b[38;5;203;48;5;24m      ") {
		t.Fatalf("tail blanks should not inherit foreground/bold payload style, got %q", ansi)
	}
}

func TestCopyHistoryLineDoesNotPadSingleRowBackgroundTail(t *testing.T) {
	history := state.HistoryStore{
		Cols: 10,
		Rows: []state.HistoryRow{{
			Text:   "short",
			LineID: 99,
			Cells: []state.HistoryCell{{
				Text:  "short",
				Width: 5,
				Style: state.HistoryCellStyle{BG: "idx:24"},
			}},
		}},
	}

	lines := copyHistoryLines(history, state.CopyModeStore{BoundCols: 10})
	if got := lines[0].PlainString(); got != "short" {
		t.Fatalf("single-row SGR background must not be invented to EOL, got %q", got)
	}
	if len(lines[0].Cells) != 1 {
		t.Fatalf("single-row history should not add display-only tail cell, got %#v", lines[0].Cells)
	}
}
