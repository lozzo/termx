package render

import (
	"strings"

	"github.com/lozzow/termx/internal/protocol"
	localvterm "github.com/lozzow/termx/termx-vterm/vterm"
	"github.com/lozzow/termx/tuiv2/shared"
)

type protocolRowANSIOptions struct {
	tight                bool
	compressStyledBlanks bool
	emojiMode            shared.AmbiguousEmojiVariationSelectorMode
	cursorCol            int
	cursorVisible        bool
	cursorShape          string
}

func protocolRowANSIWithOptions(row []protocol.Cell, width int, options protocolRowANSIOptions) string {
	if width <= 0 {
		return ""
	}
	if options.tight {
		row = trimProtocolRowTrailingBlankCells(row)
		if len(row) == 0 {
			return ""
		}
	}
	var builder strings.Builder
	current := drawStyle{}
	cols := 0
	cursorWritten := !options.cursorVisible || options.cursorCol < 0
	usedECH := false
	for index := 0; cols < width; {
		var cell drawCell
		cellIndex := index
		if index < len(row) {
			cell = drawCellFromProtocolCell(row[index])
			index++
			if cell.Continuation {
				continue
			}
		} else {
			cell = blankDrawCell()
		}
		if cols+cell.Width > width {
			break
		}
		if !cursorWritten && options.cursorCol >= cols && options.cursorCol < cols+cell.Width {
			cell.Style = syntheticCursorDrawStyle(cell.Style, options.cursorShape)
			cursorWritten = true
		}
		if options.compressStyledBlanks {
			if blankRun := protocolStyledBlankRun(row, cellIndex, cols, width, options); blankRun >= 5 {
				if current != cell.Style {
					builder.WriteString(styleDiffANSI(current, cell.Style))
					current = cell.Style
				}
				writeECHANSI(&builder, blankRun)
				usedECH = true
				cols += blankRun
				index = cellIndex + blankRun
				if cols < width {
					writeCHAANSI(&builder, cols+1)
				}
				continue
			}
		}
		content := cell.Content
		if content == "" {
			content = " "
		}
		if current != cell.Style {
			builder.WriteString(styleDiffANSI(current, cell.Style))
			current = cell.Style
		}
		nextCol := 0
		if cols+cell.Width < width {
			nextCol = cols + cell.Width + 1
		}
		builder.WriteString(serializeCellContentForDisplay(content, cell.Width, options.emojiMode, nextCol))
		cols += cell.Width
	}
	for !options.tight && cols < width {
		cellStyle := drawStyle{}
		if !cursorWritten && options.cursorCol == cols {
			cellStyle = syntheticCursorDrawStyle(drawStyle{}, options.cursorShape)
			cursorWritten = true
		}
		if current != cellStyle {
			builder.WriteString(styleDiffANSI(current, cellStyle))
			current = cellStyle
		}
		builder.WriteByte(' ')
		cols++
	}
	if current != (drawStyle{}) {
		builder.WriteString(styleDiffANSI(current, drawStyle{}))
	}
	if options.tight {
		return builder.String()
	}
	if usedECH {
		return builder.String()
	}
	return forceWidthANSIOverlay(builder.String(), width)
}

func vtermRowANSIWithOptions(row []localvterm.Cell, width int, options protocolRowANSIOptions) string {
	if width <= 0 {
		return ""
	}
	var builder strings.Builder
	current := drawStyle{}
	cols := 0
	cursorWritten := !options.cursorVisible || options.cursorCol < 0
	usedECH := false
	for index := 0; cols < width; {
		var cell drawCell
		cellIndex := index
		if index < len(row) {
			cell = drawCellFromVTermCell(row[index])
			index++
			if cell.Continuation {
				continue
			}
		} else {
			cell = blankDrawCell()
		}
		if cols+cell.Width > width {
			break
		}
		if !cursorWritten && options.cursorCol >= cols && options.cursorCol < cols+cell.Width {
			cell.Style = syntheticCursorDrawStyle(cell.Style, options.cursorShape)
			cursorWritten = true
		}
		if options.compressStyledBlanks {
			if blankRun := vtermStyledBlankRun(row, cellIndex, cols, width, options); blankRun >= 5 {
				if current != cell.Style {
					builder.WriteString(styleDiffANSI(current, cell.Style))
					current = cell.Style
				}
				writeECHANSI(&builder, blankRun)
				usedECH = true
				cols += blankRun
				index = cellIndex + blankRun
				if cols < width {
					writeCHAANSI(&builder, cols+1)
				}
				continue
			}
		}
		content := cell.Content
		if content == "" {
			content = " "
		}
		if current != cell.Style {
			builder.WriteString(styleDiffANSI(current, cell.Style))
			current = cell.Style
		}
		nextCol := 0
		if cols+cell.Width < width {
			nextCol = cols + cell.Width + 1
		}
		builder.WriteString(serializeCellContentForDisplay(content, cell.Width, options.emojiMode, nextCol))
		cols += cell.Width
	}
	for cols < width {
		cellStyle := drawStyle{}
		if !cursorWritten && options.cursorCol == cols {
			cellStyle = syntheticCursorDrawStyle(drawStyle{}, options.cursorShape)
			cursorWritten = true
		}
		if current != cellStyle {
			builder.WriteString(styleDiffANSI(current, cellStyle))
			current = cellStyle
		}
		builder.WriteByte(' ')
		cols++
	}
	if current != (drawStyle{}) {
		builder.WriteString(styleDiffANSI(current, drawStyle{}))
	}
	if usedECH {
		return builder.String()
	}
	return forceWidthANSIOverlay(builder.String(), width)
}

func protocolStyledBlankRun(row []protocol.Cell, index, cols, width int, options protocolRowANSIOptions) int {
	if index < 0 || index >= len(row) || cols < 0 || cols >= width {
		return 0
	}
	first := drawCellFromProtocolCell(row[index])
	if !terminalStyledBlankCell(first) {
		return 0
	}
	run := 0
	for scanIndex, scanCols := index, cols; scanIndex < len(row) && scanCols < width; scanIndex++ {
		if !options.cursorVisible || options.cursorCol < 0 {
			// No cursor can split the blank run.
		} else if options.cursorCol == scanCols {
			break
		}
		cell := drawCellFromProtocolCell(row[scanIndex])
		if cell.Continuation {
			continue
		}
		if !terminalStyledBlankCell(cell) || cell.Style != first.Style {
			break
		}
		run++
		scanCols++
	}
	return run
}

func vtermStyledBlankRun(row []localvterm.Cell, index, cols, width int, options protocolRowANSIOptions) int {
	if index < 0 || index >= len(row) || cols < 0 || cols >= width {
		return 0
	}
	first := drawCellFromVTermCell(row[index])
	if !terminalStyledBlankCell(first) {
		return 0
	}
	run := 0
	for scanIndex, scanCols := index, cols; scanIndex < len(row) && scanCols < width; scanIndex++ {
		if !options.cursorVisible || options.cursorCol < 0 {
			// No cursor can split the blank run.
		} else if options.cursorCol == scanCols {
			break
		}
		cell := drawCellFromVTermCell(row[scanIndex])
		if cell.Continuation {
			continue
		}
		if !terminalStyledBlankCell(cell) || cell.Style != first.Style {
			break
		}
		run++
		scanCols++
	}
	return run
}

func terminalStyledBlankCell(cell drawCell) bool {
	if cell.Continuation || cell.Width != 1 || cell.Style == (drawStyle{}) {
		return false
	}
	return cell.Content == "" || cell.Content == " "
}
