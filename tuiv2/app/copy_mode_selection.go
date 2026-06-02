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
	buffer, ok := m.activeCopyModeBuffer()
	if !ok {
		return
	}
	point := m.copyMode.Cursor
	m.copyMode.Mark = &copyModePoint{Row: point.Row, Col: point.Col}
	if logical, ok := buffer.logicalPosForPoint(point); ok {
		m.copyMode.MarkLogical = &logical
	}
	m.saveCurrentCopyModeState()
	m.render.Invalidate()
}

func normalizeCopySelection(a, b copyModeLogicalPos) (copyModeLogicalPos, copyModeLogicalPos) {
	if a.Line > b.Line || (a.Line == b.Line && a.Offset > b.Offset) {
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
	start, ok := buffer.logicalPosForPoint(*m.copyMode.Mark)
	if !ok {
		return "", false
	}
	end, ok := buffer.logicalPosForPoint(m.copyMode.Cursor)
	if !ok {
		return "", false
	}
	start, end = normalizeCopySelection(start, end)
	var out strings.Builder
	var previousLine copyModeLogicalLine
	havePreviousLine := false
	for lineIndex := start.Line; lineIndex <= end.Line; lineIndex++ {
		line, ok := buffer.logicalLineByIndex(lineIndex)
		if !ok {
			return "", false
		}
		if havePreviousLine && !copyModeSelectionLinesContinue(previousLine, line) {
			out.WriteByte('\n')
		}
		firstOffset := 0
		lastOffset := len(line.Text)
		if lineIndex == start.Line {
			firstOffset = start.Offset
		}
		if lineIndex == end.Line {
			lastOffset = end.Offset + 1
		}
		if firstOffset < 0 {
			firstOffset = 0
		}
		if firstOffset > len(line.Text) {
			firstOffset = len(line.Text)
		}
		if lastOffset < firstOffset {
			lastOffset = firstOffset
		}
		if lastOffset > len(line.Text) {
			lastOffset = len(line.Text)
		}
		out.WriteString(line.Text[firstOffset:lastOffset])
		previousLine = line
		havePreviousLine = true
	}
	return out.String(), true
}

func copyModeSelectionLinesContinue(previous, next copyModeLogicalLine) bool {
	return previous.LogicalLineID != 0 &&
		previous.LogicalLineID == next.LogicalLineID &&
		previous.ClippedAfter &&
		next.ClippedBefore
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
	paneID := m.copyMode.PaneID
	storeCmd := m.pushClipboardHistory(text, paneID)
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
	return batchCmds(storeCmd, m.showNotice(fmt.Sprintf("copied %d bytes", len([]byte(text)))))
}
