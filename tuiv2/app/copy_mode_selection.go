package app

import (
	"encoding/base64"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/tuiv2/input"
)

func (m *Model) beginCopySelection() {
	if !m.ensureCopyMode() {
		return
	}
	point := m.copyMode.Cursor
	m.copyMode.Mark = &copyModePoint{Row: point.Row, Col: point.Col}
	m.render.Invalidate()
}

func normalizeCopySelection(a, b copyModePoint) (copyModePoint, copyModePoint) {
	if a.Row > b.Row || (a.Row == b.Row && a.Col > b.Col) {
		return b, a
	}
	return a, b
}

func (m *Model) copyModeSelectedText() (string, bool) {
	if !m.ensureCopyMode() || m.copyMode.Mark == nil {
		return "", false
	}
	buffer, ok := m.activeCopyModeBuffer()
	if !ok || buffer.totalRows() == 0 {
		return "", false
	}
	start, end := normalizeCopySelection(buffer.clampPoint(*m.copyMode.Mark), buffer.clampPoint(m.copyMode.Cursor))
	var out strings.Builder
	for row := start.Row; row <= end.Row; row++ {
		cells := buffer.row(row)
		firstCol := 0
		lastCol := buffer.rowMaxCol(row)
		if row == start.Row {
			firstCol = start.Col
		}
		if row == end.Row {
			lastCol = end.Col
		}
		if lastCol < firstCol {
			lastCol = firstCol
		}
		firstCol = buffer.normalizeCol(row, firstCol)
		lastCol = buffer.normalizeCol(row, lastCol)
		for col := firstCol; col <= lastCol; col++ {
			if col >= 0 && col < len(cells) && cells[col].Content == "" && cells[col].Width == 0 {
				continue
			}
			if col >= 0 && col < len(cells) && cells[col].Content != "" {
				out.WriteString(cells[col].Content)
				continue
			}
			out.WriteByte(' ')
		}
		if row < end.Row {
			out.WriteByte('\n')
		}
	}
	return out.String(), true
}

func osc52ClipboardSequence(text string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	return "\x1b]52;c;" + encoded + "\x07"
}

func (m *Model) copySelectionToClipboard(exit bool) tea.Cmd {
	text, ok := m.copyModeSelectedText()
	if !ok || text == "" {
		return m.showError(fmt.Errorf("copy mode selection is empty"))
	}
	m.yankBuffer = text
	m.pushClipboardHistory(text, m.copyMode.PaneID)
	clipboardErr := error(nil)
	if systemClipboardWriter != nil {
		clipboardErr = systemClipboardWriter(text)
	}
	if m.cursorOut != nil {
		if err := m.cursorOut.WriteControlSequence(osc52ClipboardSequence(text)); err != nil && clipboardErr == nil {
			clipboardErr = err
		}
	}
	if exit {
		m.leaveCopyMode()
		m.setMode(input.ModeState{Kind: input.ModeNormal})
	} else {
		m.clearCopySelection()
	}
	m.render.Invalidate()
	if clipboardErr != nil && m.yankBuffer == "" {
		return m.showError(clipboardErr)
	}
	return m.showNotice(fmt.Sprintf("copied %d bytes", len([]byte(text))))
}
