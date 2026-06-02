package render

import (
	"strconv"
	"time"

	"github.com/lozzow/termx/internal/protocol"
)

type copyModeProjectionRenderSource struct {
	projection *RenderCopyModeProjectionVM
	viewTopRow int
	height     int
}

func copyModeProjectionSource(copyMode RenderCopyModeVM, height int) terminalRenderSource {
	if copyMode.Projection == nil {
		return nil
	}
	return copyModeProjectionRenderSource{
		projection: copyMode.Projection,
		viewTopRow: projectionViewportTop(copyMode.Projection, height, copyMode.ViewTopRow),
		height:     maxInt(1, height),
	}
}

func (s copyModeProjectionRenderSource) Size() protocol.Size {
	if s.projection == nil {
		return protocol.Size{}
	}
	return s.projection.Size
}

func (s copyModeProjectionRenderSource) Cursor() protocol.CursorState { return protocol.CursorState{} }

func (s copyModeProjectionRenderSource) Modes() protocol.TerminalModes {
	return protocol.TerminalModes{AutoWrap: true}
}

func (s copyModeProjectionRenderSource) IsAlternateScreen() bool { return false }

func (s copyModeProjectionRenderSource) ScreenRows() int {
	if s.projection == nil {
		return 0
	}
	start := projectionViewportTop(s.projection, s.height, s.viewTopRow)
	return minInt(maxInt(1, s.height), len(s.projection.Rows)-start)
}

func (s copyModeProjectionRenderSource) ScrollbackRows() int {
	if s.projection == nil {
		return 0
	}
	return projectionViewportTop(s.projection, s.height, s.viewTopRow)
}

func (s copyModeProjectionRenderSource) TotalRows() int {
	if s.projection == nil {
		return 0
	}
	return len(s.projection.Rows)
}

func (s copyModeProjectionRenderSource) Row(rowIndex int) []protocol.Cell {
	if s.projection == nil || rowIndex < 0 || rowIndex >= len(s.projection.Rows) {
		return nil
	}
	return s.projection.Rows[rowIndex].Cells
}

func (s copyModeProjectionRenderSource) RowTimestamp(rowIndex int) time.Time {
	if s.projection == nil || rowIndex < 0 || rowIndex >= len(s.projection.Rows) {
		return time.Time{}
	}
	return s.projection.Rows[rowIndex].Timestamp
}

func (s copyModeProjectionRenderSource) RowKind(rowIndex int) string {
	if s.projection == nil || rowIndex < 0 || rowIndex >= len(s.projection.Rows) {
		return ""
	}
	return s.projection.Rows[rowIndex].Kind
}

func (s copyModeProjectionRenderSource) RowHash(rowIndex int) uint64 {
	hash := fnvOffset64
	hash = fnvMixUint64(hash, uint64(rowIndex+1))
	if s.projection == nil || rowIndex < 0 || rowIndex >= len(s.projection.Rows) {
		return fnvMixUint64(hash, 0)
	}
	row := s.projection.Rows[rowIndex]
	hash = fnvMixString(hash, row.Kind)
	hash = fnvMixBool(hash, row.Wrapped)
	hash = fnvMixInt64(hash, row.Timestamp.UnixNano())
	return hashProtocolRow(hash, row.Cells)
}

func (s copyModeProjectionRenderSource) RowContentHash(rowIndex int) uint64 {
	hash := fnvOffset64
	if s.projection == nil || rowIndex < 0 || rowIndex >= len(s.projection.Rows) {
		return fnvMixUint64(hash, 0)
	}
	row := s.projection.Rows[rowIndex]
	hash = fnvMixString(hash, row.Kind)
	hash = fnvMixBool(hash, row.Wrapped)
	hash = fnvMixInt64(hash, row.Timestamp.UnixNano())
	return hashProtocolRow(hash, row.Cells)
}

func (s copyModeProjectionRenderSource) RowIdentityHash(rowIndex int) uint64 {
	if s.projection == nil || rowIndex < 0 || rowIndex >= len(s.projection.Rows) {
		return fnvMixUint64(fnvOffset64, 0)
	}
	hash := fnvOffset64
	row := s.projection.Rows[rowIndex]
	hash = fnvMixUint64(hash, uint64(rowIndex+1))
	hash = fnvMixString(hash, s.projection.Token)
	hash = fnvMixUint64(hash, s.projection.Generation)
	hash = fnvMixUint64(hash, s.projection.FirstBoundaryID)
	hash = fnvMixUint64(hash, s.projection.LastBoundaryID)
	hash = fnvMixString(hash, row.Kind)
	hash = fnvMixBool(hash, row.Wrapped)
	hash = fnvMixInt64(hash, row.Timestamp.UnixNano())
	return hashProtocolRow(hash, row.Cells)
}

func (s copyModeProjectionRenderSource) RowVisualHash(rowIndex int) uint64 {
	if s.projection == nil || rowIndex < 0 || rowIndex >= len(s.projection.Rows) {
		return fnvMixUint64(fnvOffset64, 0)
	}
	return hashProtocolRow(fnvOffset64, s.projection.Rows[rowIndex].Cells)
}

func copyModeSource(copyMode RenderCopyModeVM, fallbackSnapshot *protocol.Snapshot, height int) terminalRenderSource {
	if source := copyModeProjectionSource(copyMode, height); source != nil {
		return source
	}
	return renderSource(fallbackSnapshot, nil)
}

func copyModeTotalRows(copyMode RenderCopyModeVM, snapshot *protocol.Snapshot) int {
	if copyMode.Projection != nil {
		return len(copyMode.Projection.Rows)
	}
	return snapshotTotalRows(snapshot)
}

func copyModeLogicalLineCount(copyMode RenderCopyModeVM, snapshot *protocol.Snapshot) int {
	if copyMode.Projection != nil {
		if len(copyMode.Projection.Lines) > 0 {
			return len(copyMode.Projection.Lines)
		}
		return projectionLogicalLineCount(copyMode.Projection)
	}
	return snapshotLogicalLineCount(snapshot)
}

func copyModeRowWrapped(copyMode RenderCopyModeVM, snapshot *protocol.Snapshot, row int) bool {
	if copyMode.Projection != nil {
		if row < 0 || row >= len(copyMode.Projection.Rows) {
			return false
		}
		return copyMode.Projection.Rows[row].Wrapped
	}
	return snapshotRowWrapped(snapshot, row)
}

func copyModeRowCells(copyMode RenderCopyModeVM, snapshot *protocol.Snapshot, row int) []protocol.Cell {
	if copyMode.Projection != nil {
		if row < 0 || row >= len(copyMode.Projection.Rows) {
			return nil
		}
		return copyMode.Projection.Rows[row].Cells
	}
	return snapshotRow(snapshot, row)
}

func copyModeTimestampLabelForVM(copyMode RenderCopyModeVM, snapshot *protocol.Snapshot, row int) string {
	ts := copyModeRowTimestamp(copyMode, snapshot, row)
	if ts.IsZero() {
		return ""
	}
	return formatSnapshotRowTimestamp(ts)
}

func copyModeRowTimestamp(copyMode RenderCopyModeVM, snapshot *protocol.Snapshot, row int) time.Time {
	if copyMode.Projection != nil {
		if row < 0 || row >= len(copyMode.Projection.Rows) {
			return time.Time{}
		}
		return copyMode.Projection.Rows[row].Timestamp
	}
	return snapshotRowTimestamp(snapshot, row)
}

func copyModeRowPositionLabelForVM(copyMode RenderCopyModeVM, snapshot *protocol.Snapshot, logicalLine, fallbackRow int) string {
	totalLines := copyModeLogicalLineCount(copyMode, snapshot)
	if totalLines <= 0 {
		return ""
	}
	if logicalLine < 0 {
		logicalLine = copyModeLogicalLineIndex(copyMode, snapshot, fallbackRow)
	}
	if logicalLine < 0 || logicalLine >= totalLines {
		return ""
	}
	return strconv.Itoa(logicalLine+1) + "/" + strconv.Itoa(totalLines)
}

func copyModeLogicalLineIndex(copyMode RenderCopyModeVM, snapshot *protocol.Snapshot, row int) int {
	if copyMode.Projection != nil {
		return projectionLogicalLineIndex(copyMode.Projection, row)
	}
	return snapshotLogicalLineIndex(snapshot, row)
}

func copyModeLogicalLineBounds(copyMode RenderCopyModeVM, snapshot *protocol.Snapshot, logicalLine int) (int, int, bool) {
	if copyMode.Projection != nil {
		return projectionLogicalLineBounds(copyMode.Projection, logicalLine)
	}
	return snapshotLogicalLineBounds(snapshot, logicalLine)
}

func copyModePointForLogicalPos(copyMode RenderCopyModeVM, snapshot *protocol.Snapshot, logicalLine, logicalCol int) (int, int, bool) {
	start, end, ok := copyModeLogicalLineBounds(copyMode, snapshot, logicalLine)
	if !ok {
		return 0, 0, false
	}
	offset := 0
	lastRow, lastCol := start, 0
	for row := start; row <= end; row++ {
		cells := copyModeRowCells(copyMode, snapshot, row)
		for col, cell := range cells {
			if cell.Content == "" && cell.Width == 0 {
				continue
			}
			lastRow, lastCol = row, col
			if offset >= logicalCol {
				return row, col, true
			}
			offset++
		}
	}
	row, col := clampCopyPointForMode(copyMode, snapshot, lastRow, lastCol)
	return row, col, true
}

func projectionLogicalLineCount(projection *RenderCopyModeProjectionVM) int {
	if projection == nil || len(projection.Rows) == 0 {
		return 0
	}
	count := 0
	for row := 0; row < len(projection.Rows); row++ {
		if row == 0 || !projection.Rows[row-1].Wrapped {
			count++
		}
	}
	return count
}

func projectionLogicalLineIndex(projection *RenderCopyModeProjectionVM, row int) int {
	if projection == nil || row < 0 || row >= len(projection.Rows) {
		return -1
	}
	if len(projection.Lines) > 0 {
		for index, line := range projection.Lines {
			if row >= line.StartRow && row <= line.EndRow {
				return index
			}
		}
		return -1
	}
	index := -1
	for current := 0; current <= row; current++ {
		if current == 0 || !projection.Rows[current-1].Wrapped {
			index++
		}
	}
	return index
}

func projectionLogicalLineBounds(projection *RenderCopyModeProjectionVM, logicalLine int) (int, int, bool) {
	if projection == nil || len(projection.Rows) == 0 || logicalLine < 0 {
		return 0, 0, false
	}
	if len(projection.Lines) > 0 {
		if logicalLine >= len(projection.Lines) {
			return 0, 0, false
		}
		line := projection.Lines[logicalLine]
		start := clampInt(line.StartRow, 0, len(projection.Rows)-1)
		end := clampInt(line.EndRow, start, len(projection.Rows)-1)
		return start, end, true
	}
	index := -1
	for row := 0; row < len(projection.Rows); row++ {
		if row == 0 || !projection.Rows[row-1].Wrapped {
			index++
		}
		if index != logicalLine {
			continue
		}
		end := row
		for end < len(projection.Rows)-1 && projection.Rows[end].Wrapped {
			end++
		}
		return row, end, true
	}
	return 0, 0, false
}

func projectionViewportTop(projection *RenderCopyModeProjectionVM, height, viewTopRow int) int {
	if projection == nil || len(projection.Rows) == 0 {
		return 0
	}
	if height <= 0 {
		height = len(projection.Rows)
	}
	maxTop := maxInt(0, len(projection.Rows)-maxInt(1, height))
	if viewTopRow < 0 {
		return 0
	}
	if viewTopRow > maxTop {
		return maxTop
	}
	return viewTopRow
}
