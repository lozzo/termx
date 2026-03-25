package surface

import (
	"strings"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/domain/types"
	"github.com/lozzow/termx/tui/render/projection"
)

type Cursor struct {
	Visible bool
	Row     int
	Col     int
}

type Pane struct {
	Title  string
	Body   []string
	Cursor Cursor
}

// BuildPaneSurface 负责把 pane 的运行时槽位和终端快照压成可绘制 surface，
// 让 compositor 只关注 rect/frame，而不需要再理解 slot/snapshot 细节。
func BuildPaneSurface(state types.AppState, pane types.PaneState, screens projection.RuntimeTerminalStore, width, height int) Pane {
	title := paneTitle(state, pane)
	bodyWidth := max(1, width)
	bodyRows := max(1, height)

	switch pane.SlotState {
	case types.PaneSlotConnected:
		return buildConnectedSurface(pane, screens, title, bodyWidth, bodyRows)
	case types.PaneSlotWaiting:
		return Pane{Title: title, Body: clipBody([]string{"waiting slot"}, bodyWidth, bodyRows)}
	case types.PaneSlotExited:
		return Pane{Title: title, Body: clipBody([]string{"process exited"}, bodyWidth, bodyRows)}
	case types.PaneSlotEmpty:
		fallthrough
	default:
		return Pane{Title: title, Body: clipBody([]string{"empty pane"}, bodyWidth, bodyRows)}
	}
}

func buildConnectedSurface(pane types.PaneState, screens projection.RuntimeTerminalStore, title string, bodyWidth int, bodyRows int) Pane {
	if pane.TerminalID == "" || screens == nil {
		return Pane{Title: title, Body: clipBody([]string{"screen unavailable"}, bodyWidth, bodyRows)}
	}
	snapshot, ok := screens.Snapshot(pane.TerminalID)
	if !ok || snapshot == nil {
		return Pane{Title: title, Body: clipBody([]string{"screen unavailable"}, bodyWidth, bodyRows)}
	}

	body := snapshotRows(snapshot)
	if len(body) == 0 {
		body = []string{""}
	}
	if len(body) > bodyRows {
		body = body[len(body)-bodyRows:]
	}

	cursorRow := snapshot.Cursor.Row
	if len(body) < len(snapshot.Screen.Cells) {
		cursorRow -= len(snapshot.Screen.Cells) - len(body)
	}
	if cursorRow < 0 || cursorRow >= len(body) {
		cursorRow = 0
	}

	return Pane{
		Title: title,
		Body:  clipBody(body, bodyWidth, bodyRows),
		Cursor: Cursor{
			Visible: snapshot.Cursor.Visible,
			Row:     cursorRow,
			Col:     max(0, snapshot.Cursor.Col),
		},
	}
}

func snapshotRows(snapshot *protocol.Snapshot) []string {
	if snapshot == nil || len(snapshot.Screen.Cells) == 0 {
		return nil
	}

	rows := make([]string, 0, len(snapshot.Screen.Cells))
	for _, row := range snapshot.Screen.Cells {
		var line strings.Builder
		for _, cell := range row {
			line.WriteString(cell.Content)
		}
		rows = append(rows, strings.TrimRight(line.String(), " "))
	}
	return rows
}

func paneTitle(state types.AppState, pane types.PaneState) string {
	if pane.TitleHint != "" {
		return pane.TitleHint
	}
	if pane.TerminalID != "" {
		if terminal, ok := state.Domain.Terminals[pane.TerminalID]; ok && terminal.Name != "" {
			return terminal.Name
		}
		return string(pane.TerminalID)
	}
	switch pane.SlotState {
	case types.PaneSlotWaiting:
		return "waiting pane"
	case types.PaneSlotExited:
		return "exited pane"
	case types.PaneSlotEmpty:
		return "empty pane"
	}
	if pane.ID != "" {
		return string(pane.ID)
	}
	return "pane"
}

func clipBody(lines []string, width int, height int) []string {
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}
	if len(lines) == 0 {
		return []string{""}
	}

	clipped := make([]string, 0, min(len(lines), height))
	for _, line := range lines {
		clipped = append(clipped, clipLine(line, width))
		if len(clipped) == height {
			break
		}
	}
	return clipped
}

func clipLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(line)
	if len(runes) <= width {
		return line
	}
	return string(runes[:width])
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
