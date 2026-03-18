package termx

import (
	"encoding/json"
	"strings"
)

func (s Snapshot) MarshalJSON() ([]byte, error) {
	type jsonStyle struct {
		FG            string `json:"fg,omitempty"`
		BG            string `json:"bg,omitempty"`
		Bold          bool   `json:"b,omitempty"`
		Italic        bool   `json:"i,omitempty"`
		Underline     bool   `json:"u,omitempty"`
		Blink         bool   `json:"k,omitempty"`
		Reverse       bool   `json:"rv,omitempty"`
		Strikethrough bool   `json:"st,omitempty"`
	}
	type jsonCell struct {
		Content string     `json:"r,omitempty"`
		Width   int        `json:"w,omitempty"`
		Style   *jsonStyle `json:"s,omitempty"`
	}
	type jsonRow struct {
		Cells []jsonCell `json:"cells,omitempty"`
	}
	type jsonScreen struct {
		IsAlternate bool      `json:"is_alternate"`
		Rows        []jsonRow `json:"rows"`
	}
	type jsonSnapshot struct {
		TerminalID     string        `json:"terminal_id"`
		Size           Size          `json:"size"`
		Screen         jsonScreen    `json:"screen"`
		ScrollbackRows int           `json:"scrollback_rows"`
		Scrollback     []jsonRow     `json:"scrollback,omitempty"`
		Cursor         CursorState   `json:"cursor"`
		Modes          TerminalModes `json:"modes"`
		Timestamp      string        `json:"timestamp"`
	}

	encodeCell := func(cell Cell) jsonCell {
		out := jsonCell{Content: cell.Content}
		if cell.Width > 1 {
			out.Width = cell.Width
		}
		if !cell.Style.isZero() {
			out.Style = &jsonStyle{
				FG:            cell.Style.FG,
				BG:            cell.Style.BG,
				Bold:          cell.Style.Bold,
				Italic:        cell.Style.Italic,
				Underline:     cell.Style.Underline,
				Blink:         cell.Style.Blink,
				Reverse:       cell.Style.Reverse,
				Strikethrough: cell.Style.Strikethrough,
			}
		}
		return out
	}

	encodeRow := func(row []Cell) jsonRow {
		last := len(row)
		for last > 0 {
			cell := row[last-1]
			if cell.Content != "" && strings.TrimSpace(cell.Content) != "" {
				break
			}
			if !cell.Style.isZero() {
				break
			}
			last--
		}
		cells := make([]jsonCell, 0, last)
		for _, cell := range row[:last] {
			cells = append(cells, encodeCell(cell))
		}
		return jsonRow{Cells: cells}
	}

	screenRows := make([]jsonRow, 0, len(s.Screen.Cells))
	for _, row := range s.Screen.Cells {
		screenRows = append(screenRows, encodeRow(row))
	}
	scrollbackRows := make([]jsonRow, 0, len(s.Scrollback))
	for _, row := range s.Scrollback {
		scrollbackRows = append(scrollbackRows, encodeRow(row))
	}

	return json.Marshal(jsonSnapshot{
		TerminalID: s.TerminalID,
		Size:       s.Size,
		Screen: jsonScreen{
			IsAlternate: s.Screen.IsAlternateScreen,
			Rows:        screenRows,
		},
		ScrollbackRows: len(s.Scrollback),
		Scrollback:     scrollbackRows,
		Cursor:         s.Cursor,
		Modes:          s.Modes,
		Timestamp:      s.Timestamp.UTC().Format(timeLayout),
	})
}

const timeLayout = "2006-01-02T15:04:05Z07:00"

func (s CellStyle) isZero() bool {
	return s == CellStyle{}
}
