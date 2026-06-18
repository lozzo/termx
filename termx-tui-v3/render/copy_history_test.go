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

func TestCopyHistoryContentANSILineUsesTailFillDisplayOnly(t *testing.T) {
	tail := state.HistoryCellStyle{BG: "idx:24"}
	row := state.HistoryRow{
		Text:     "ij",
		LineID:   99,
		TailFill: &tail,
		Cells: []state.HistoryCell{
			{Text: "i", Width: 1, Style: tail},
			{Text: "j", Width: 1, Style: tail},
		},
	}
	if got := state.HistoryRowDisplayWidth(row); got != 2 {
		t.Fatalf("tail fill must not affect logical row width, got %d", got)
	}
	ansi := CopyHistoryContentANSILineAt(state.HistoryStore{Cols: 8, Rows: []state.HistoryRow{row}}, state.CopyModeStore{BoundCols: 8}, 0, 8, 0, DefaultTheme())
	if !strings.Contains(ansi, "\x1b[48;5;24m      ") {
		t.Fatalf("tail fill should render display-only background to EOL, got %q", ansi)
	}
}

func TestCopyHistoryContentANSILineSelectionFillsRowTail(t *testing.T) {
	history := state.HistoryStore{
		Cols: 8,
		Rows: []state.HistoryRow{{
			Text:   "uv",
			LineID: 1,
			Cells: []state.HistoryCell{
				{Text: "u", Width: 1},
				{Text: "v", Width: 1},
			},
		}},
	}
	copyMode := state.CopyModeStore{
		Active:    true,
		BoundCols: 8,
		Selection: &state.CopySelection{
			Anchor: state.CopyPosition{Row: 0, Col: 0},
			Focus:  state.CopyPosition{Row: 0, Col: 8},
		},
	}

	line := copyHistoryLine(history.Rows[0], 0, normalizedCopySelection(copyMode), copySearchRange{}, 8)
	if len(line.Cells) != 3 || line.Cells[2].Width != 6 || line.Cells[2].ANSIStyle != copyHistorySelectionANSIStyle {
		t.Fatalf("selection should paint display-only row tail like tmux, got %#v", line.Cells)
	}
	if got := line.Width(); got != 8 {
		t.Fatalf("selection line should fill to viewport width, got %d cells=%#v", got, line.Cells)
	}
	if got := state.HistoryRowDisplayWidth(history.Rows[0]); got != 2 {
		t.Fatalf("selection fill must not mutate logical row width, got %d", got)
	}
}
