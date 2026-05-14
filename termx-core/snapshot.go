package termx

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lozzow/termx/termx-core/protocol"
)

func trimSnapshotResultToFrameBudget(snapshot *Snapshot, encoded []byte, budget int) (*Snapshot, []byte) {
	if snapshot == nil || budget <= 0 || len(encoded) <= budget || len(snapshot.Scrollback) == 0 {
		return snapshot, encoded
	}
	low, high := 0, len(snapshot.Scrollback)
	var best *Snapshot
	var bestEncoded []byte
	for low <= high {
		keep := (low + high) / 2
		candidate := snapshotWithScrollbackTail(snapshot, keep)
		data, err := json.Marshal(candidate)
		if err != nil {
			break
		}
		if len(data) <= budget {
			best = candidate
			bestEncoded = data
			low = keep + 1
			continue
		}
		high = keep - 1
	}
	if best != nil {
		return best, bestEncoded
	}
	trimmed := snapshotWithScrollbackTail(snapshot, 0)
	data, err := json.Marshal(trimmed)
	if err != nil || len(data) > len(encoded) {
		return snapshot, encoded
	}
	return trimmed, data
}

func snapshotWithScrollbackTail(snapshot *Snapshot, keep int) *Snapshot {
	if snapshot == nil {
		return nil
	}
	out := *snapshot
	rowCount := len(snapshot.Scrollback)
	if keep < 0 {
		keep = 0
	}
	if keep > rowCount {
		keep = rowCount
	}
	trim := rowCount - keep
	if trim <= 0 {
		return &out
	}
	out.ScrollbackHasMore = out.ScrollbackHasMore || rowCount > 0
	if trim >= rowCount {
		out.Scrollback = nil
		out.ScrollbackTimestamps = nil
		out.ScrollbackRowKinds = nil
		out.ScrollbackWrapped = nil
		return &out
	}
	out.Scrollback = snapshot.Scrollback[trim:]
	out.ScrollbackTimestamps = trimTimeMetadataHead(snapshot.ScrollbackTimestamps, trim)
	out.ScrollbackRowKinds = trimStringMetadataHead(snapshot.ScrollbackRowKinds, trim)
	out.ScrollbackWrapped = trimBoolMetadataHead(snapshot.ScrollbackWrapped, trim)
	return &out
}

func trimTimeMetadataHead(values []time.Time, trim int) []time.Time {
	if len(values) == 0 {
		return nil
	}
	if trim >= len(values) {
		return nil
	}
	return values[trim:]
}

func trimStringMetadataHead(values []string, trim int) []string {
	if len(values) == 0 {
		return nil
	}
	if trim >= len(values) {
		return nil
	}
	return values[trim:]
}

func trimBoolMetadataHead(values []bool, trim int) []bool {
	if len(values) == 0 {
		return nil
	}
	if trim >= len(values) {
		return nil
	}
	return values[trim:]
}

func trimGridViewportResultToFrameBudget(viewport *protocol.GridViewport, encoded []byte, budget int) (*protocol.GridViewport, []byte) {
	if viewport == nil || budget <= 0 || len(encoded) <= budget || len(viewport.Rows) == 0 {
		return viewport, encoded
	}
	low, high := 0, len(viewport.Rows)
	var best *protocol.GridViewport
	var bestEncoded []byte
	for low <= high {
		keep := (low + high) / 2
		candidate := protocolGridViewportWithRowTail(viewport, keep)
		data, err := json.Marshal(candidate)
		if err != nil {
			break
		}
		if len(data) <= budget {
			best = candidate
			bestEncoded = data
			low = keep + 1
			continue
		}
		high = keep - 1
	}
	if best != nil {
		return best, bestEncoded
	}
	trimmed := protocolGridViewportWithRowTail(viewport, 0)
	data, err := json.Marshal(trimmed)
	if err != nil || len(data) > len(encoded) {
		return viewport, encoded
	}
	return trimmed, data
}

func protocolGridViewportWithRowTail(viewport *protocol.GridViewport, keep int) *protocol.GridViewport {
	if viewport == nil {
		return nil
	}
	out := *viewport
	rowCount := len(viewport.Rows)
	if keep < 0 {
		keep = 0
	}
	if keep > rowCount {
		keep = rowCount
	}
	trim := rowCount - keep
	if trim <= 0 {
		return &out
	}
	out.ScrollbackHasMore = out.ScrollbackHasMore || rowCount > 0
	if trim >= rowCount {
		out.Rows = nil
		out.ScrollbackTimestamps = nil
		out.ScrollbackRowKinds = nil
		out.ScrollbackWrapped = nil
		return &out
	}
	out.Rows = viewport.Rows[trim:]
	out.ScrollbackTimestamps = trimTimeMetadataHead(viewport.ScrollbackTimestamps, trim)
	out.ScrollbackRowKinds = trimStringMetadataHead(viewport.ScrollbackRowKinds, trim)
	out.ScrollbackWrapped = trimBoolMetadataHead(viewport.ScrollbackWrapped, trim)
	return &out
}

type snapshotJSONStyle struct {
	FG            string `json:"fg,omitempty"`
	BG            string `json:"bg,omitempty"`
	Bold          bool   `json:"b,omitempty"`
	Italic        bool   `json:"i,omitempty"`
	Underline     bool   `json:"u,omitempty"`
	Blink         bool   `json:"k,omitempty"`
	Reverse       bool   `json:"rv,omitempty"`
	Strikethrough bool   `json:"st,omitempty"`
}

type snapshotJSONCell struct {
	Content string             `json:"r,omitempty"`
	Width   int                `json:"w,omitempty"`
	Style   *snapshotJSONStyle `json:"s,omitempty"`
}

type snapshotJSONRun struct {
	Text  string             `json:"t,omitempty"`
	Style *snapshotJSONStyle `json:"s,omitempty"`
}

type snapshotJSONRow struct {
	Text  string             `json:"t,omitempty"`
	Runs  []snapshotJSONRun  `json:"runs,omitempty"`
	Cells []snapshotJSONCell `json:"cells,omitempty"`
}

func (s Snapshot) MarshalJSON() ([]byte, error) {
	type jsonScreen struct {
		IsAlternate bool              `json:"is_alternate"`
		Rows        []snapshotJSONRow `json:"rows"`
	}
	type jsonSnapshot struct {
		TerminalID           string            `json:"terminal_id"`
		Size                 Size              `json:"size"`
		Screen               jsonScreen        `json:"screen"`
		ScrollbackRows       int               `json:"scrollback_rows"`
		ScrollbackOffset     int               `json:"scrollback_offset,omitempty"`
		ScrollbackTotal      int               `json:"scrollback_total,omitempty"`
		ScrollbackHasMore    bool              `json:"scrollback_has_more,omitempty"`
		Scrollback           []snapshotJSONRow `json:"scrollback,omitempty"`
		ScreenTimestamps     []string          `json:"screen_timestamps,omitempty"`
		ScrollbackTimestamps []string          `json:"scrollback_timestamps,omitempty"`
		ScreenRowKinds       []string          `json:"screen_row_kinds,omitempty"`
		ScrollbackRowKinds   []string          `json:"scrollback_row_kinds,omitempty"`
		ScreenWrapped        []bool            `json:"screen_wrapped,omitempty"`
		ScrollbackWrapped    []bool            `json:"scrollback_wrapped,omitempty"`
		Cursor               CursorState       `json:"cursor"`
		Modes                TerminalModes     `json:"modes"`
		Timestamp            string            `json:"timestamp"`
	}

	encodeRowTimestamps := func(values []time.Time) []string {
		if len(values) == 0 {
			return nil
		}
		out := make([]string, len(values))
		nonEmpty := false
		for i, value := range values {
			if value.IsZero() {
				continue
			}
			out[i] = value.UTC().Format(time.RFC3339Nano)
			nonEmpty = true
		}
		if !nonEmpty {
			return nil
		}
		return out
	}
	encodeStringSlice := func(values []string) []string {
		if len(values) == 0 {
			return nil
		}
		out := make([]string, len(values))
		nonEmpty := false
		for i, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			out[i] = value
			nonEmpty = true
		}
		if !nonEmpty {
			return nil
		}
		return out
	}
	encodeBoolSlice := func(values []bool) []bool {
		if len(values) == 0 {
			return nil
		}
		out := make([]bool, len(values))
		nonEmpty := false
		for i, value := range values {
			if value {
				out[i] = true
				nonEmpty = true
			}
		}
		if !nonEmpty {
			return nil
		}
		return out
	}

	return json.Marshal(jsonSnapshot{
		TerminalID: s.TerminalID,
		Size:       s.Size,
		Screen: jsonScreen{
			IsAlternate: s.Screen.IsAlternateScreen,
			Rows:        encodeSnapshotJSONRows(s.Screen.Cells),
		},
		ScrollbackRows:       len(s.Scrollback),
		ScrollbackOffset:     s.ScrollbackOffset,
		ScrollbackTotal:      s.ScrollbackTotal,
		ScrollbackHasMore:    s.ScrollbackHasMore,
		Scrollback:           encodeSnapshotJSONRows(s.Scrollback),
		ScreenTimestamps:     encodeRowTimestamps(s.ScreenTimestamps),
		ScrollbackTimestamps: encodeRowTimestamps(s.ScrollbackTimestamps),
		ScreenRowKinds:       encodeStringSlice(s.ScreenRowKinds),
		ScrollbackRowKinds:   encodeStringSlice(s.ScrollbackRowKinds),
		ScreenWrapped:        encodeBoolSlice(s.ScreenWrapped),
		ScrollbackWrapped:    encodeBoolSlice(s.ScrollbackWrapped),
		Cursor:               s.Cursor,
		Modes:                s.Modes,
		Timestamp:            s.Timestamp.UTC().Format(timeLayout),
	})
}

func encodeSnapshotJSONRows(rows [][]Cell) []snapshotJSONRow {
	out := make([]snapshotJSONRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, encodeSnapshotJSONRow(row))
	}
	return out
}

func encodeSnapshotJSONRow(row []Cell) snapshotJSONRow {
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
	row = row[:last]
	if len(row) == 0 {
		return snapshotJSONRow{}
	}
	var text strings.Builder
	allSimple := true
	allPlain := true
	for _, cell := range row {
		cellText, ok := snapshotJSONCellText(cell)
		if !ok {
			allSimple = false
			break
		}
		if !cell.Style.isZero() {
			allPlain = false
		}
		text.WriteString(cellText)
	}
	if allSimple && allPlain {
		return snapshotJSONRow{Text: text.String()}
	}
	if allSimple {
		runs := make([]snapshotJSONRun, 0, 4)
		var runText strings.Builder
		runStyle := row[0].Style
		flushRun := func() {
			if runText.Len() == 0 {
				return
			}
			runs = append(runs, snapshotJSONRun{
				Text:  runText.String(),
				Style: snapshotJSONStyleFromCellStyle(runStyle),
			})
			runText.Reset()
		}
		for _, cell := range row {
			if cell.Style != runStyle {
				flushRun()
				runStyle = cell.Style
			}
			cellText, _ := snapshotJSONCellText(cell)
			runText.WriteString(cellText)
		}
		flushRun()
		return snapshotJSONRow{Runs: runs}
	}
	cells := make([]snapshotJSONCell, 0, len(row))
	for _, cell := range row {
		cells = append(cells, encodeSnapshotJSONCell(cell))
	}
	return snapshotJSONRow{Cells: cells}
}

func snapshotJSONCellText(cell Cell) (string, bool) {
	if cell.Width > 1 {
		return "", false
	}
	if cell.Content == "" {
		return " ", true
	}
	if utf8.RuneCountInString(cell.Content) != 1 {
		return "", false
	}
	return cell.Content, true
}

func encodeSnapshotJSONCell(cell Cell) snapshotJSONCell {
	out := snapshotJSONCell{Content: cell.Content}
	if cell.Width > 1 {
		out.Width = cell.Width
	}
	out.Style = snapshotJSONStyleFromCellStyle(cell.Style)
	return out
}

func snapshotJSONStyleFromCellStyle(style CellStyle) *snapshotJSONStyle {
	if style.isZero() {
		return nil
	}
	return &snapshotJSONStyle{
		FG:            style.FG,
		BG:            style.BG,
		Bold:          style.Bold,
		Italic:        style.Italic,
		Underline:     style.Underline,
		Blink:         style.Blink,
		Reverse:       style.Reverse,
		Strikethrough: style.Strikethrough,
	}
}

const timeLayout = "2006-01-02T15:04:05Z07:00"

func (s CellStyle) isZero() bool {
	return s == CellStyle{}
}
