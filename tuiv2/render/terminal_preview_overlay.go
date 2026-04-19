package render

import (
	"strings"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func terminalPreviewBlockLinesANSI(snapshot *protocol.Snapshot, surface runtime.TerminalSurface, runtimeState *VisibleRuntimeStateProxy, width, height int, theme uiTheme) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	source := renderSource(snapshot, surface)
	if source == nil {
		lines := []string{forceWidthANSIOverlay("(no live preview)", width)}
		for len(lines) < height {
			lines = append(lines, "")
		}
		return lines
	}
	canvas := newComposedCanvas(width, height)
	if runtimeState != nil {
		canvas.hostEmojiVS16Mode = runtimeState.HostEmojiVS16Mode
	}
	drawTerminalSourceWithOffset(canvas, workbench.Rect{X: 0, Y: 0, W: width, H: height}, source, 0, theme)
	lines := canvas.embeddedContentLines()
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func terminalPreviewLinesANSI(snapshot *protocol.Snapshot, surface runtime.TerminalSurface, runtimeState *VisibleRuntimeStateProxy, width, maxLines int) []string {
	source := renderSource(snapshot, surface)
	if source == nil || width <= 0 || maxLines <= 0 || source.ScreenRows() == 0 {
		return []string{forceWidthANSIOverlay("(no live preview)", width)}
	}
	lines := make([]string, 0, minInt(source.ScreenRows(), maxLines))
	base := source.ScrollbackRows()
	emojiMode := shared.AmbiguousEmojiVariationSelectorRaw
	if runtimeState != nil {
		emojiMode = runtimeState.HostEmojiVS16Mode
	}
	for rowIndex := 0; rowIndex < source.ScreenRows() && len(lines) < maxLines; rowIndex++ {
		lines = append(lines, protocolPreviewRowANSI(source.Row(base+rowIndex), width, emojiMode))
	}
	if len(lines) == 0 {
		return []string{forceWidthANSIOverlay("(no live preview)", width)}
	}
	return lines
}

func protocolPreviewRowANSI(row []protocol.Cell, width int, emojiMode shared.AmbiguousEmojiVariationSelectorMode) string {
	return protocolRowANSIWithOptions(row, width, protocolRowANSIOptions{
		emojiMode: emojiMode,
	})
}

func trimProtocolRowTrailingBlankCells(row []protocol.Cell) []protocol.Cell {
	end := len(row)
	for end > 0 {
		cell := row[end-1]
		if cell.Content == "" && cell.Width == 0 {
			end--
			continue
		}
		if strings.TrimSpace(cell.Content) == "" && cell.Style == (protocol.CellStyle{}) {
			end--
			continue
		}
		break
	}
	return row[:end]
}
