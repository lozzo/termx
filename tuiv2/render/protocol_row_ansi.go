package render

import (
	"strings"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/shared"
	localvterm "github.com/lozzow/termx/vterm"
)

type protocolRowANSIOptions struct {
	tight         bool
	emojiMode     shared.AmbiguousEmojiVariationSelectorMode
	cursorCol     int
	cursorVisible bool
	cursorShape   string
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
	for index := 0; cols < width; {
		var cell drawCell
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
		builder.WriteString(styleANSI(drawStyle{}))
	}
	if options.tight {
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
	for index := 0; cols < width; {
		var cell drawCell
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
		builder.WriteString(styleANSI(drawStyle{}))
	}
	return forceWidthANSIOverlay(builder.String(), width)
}
