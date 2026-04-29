package render

import (
	"strings"

	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
)

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
