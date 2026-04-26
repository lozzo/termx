package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/terminalmeta"
	"github.com/lozzow/termx/tuiv2/shared"
)

func (r *Runtime) ResizeTerminal(ctx context.Context, paneID, terminalID string, cols, rows uint16) error {
	return r.ResizePane(ctx, paneID, terminalID, cols, rows)
}

func (r *Runtime) ResizePane(ctx context.Context, paneID, terminalID string, cols, rows uint16) error {
	if r == nil || r.client == nil {
		return shared.UserVisibleError{Op: "resize terminal", Err: fmt.Errorf("runtime client is nil")}
	}
	binding := r.Binding(paneID)
	if binding == nil || binding.Channel == 0 {
		return shared.UserVisibleError{Op: "resize terminal", Err: fmt.Errorf("pane %s is not attached", paneID)}
	}
	terminal := r.registry.Get(terminalID)
	prevSnapshot := (*protocol.Snapshot)(nil)
	oldCols, oldRows := 0, 0
	previewSource := (*protocol.Snapshot)(nil)
	previewActiveBeforeResize := false
	if terminal != nil {
		if terminalmeta.SizeLocked(terminal.Tags) {
			return nil
		}
		decision := r.ResizeDecision(paneID, terminalID)
		r.syncTerminalOwnership(terminal)
		if !decision.Allowed {
			return nil
		}
		forceResize := decision.Force
		if !forceResize && terminalAlreadySized(terminal, cols, rows) {
			return nil
		}
		prevSnapshot = terminal.Snapshot
		if vt := r.ensureVTerm(terminal); vt != nil {
			oldCols, oldRows = vt.Size()
			if shouldEnterResizePreview(oldCols, oldRows, int(cols), int(rows)) {
				previewActiveBeforeResize = true
				previewSource = terminal.ResizePreviewSource
				if previewSource == nil {
					previewSource = captureResizePreviewSource(terminalID, terminal, prevSnapshot, vt)
					terminal.ResizePreviewSource = previewSource
				}
			}
		} else if prevSnapshot != nil {
			oldCols = int(prevSnapshot.Size.Cols)
			oldRows = int(prevSnapshot.Size.Rows)
		}
	}
	if err := r.client.Resize(ctx, binding.Channel, cols, rows); err != nil {
		if terminal != nil && previewActiveBeforeResize && terminal.ResizePreviewSource == previewSource {
			terminal.ResizePreviewSource = nil
		}
		return shared.UserVisibleError{Op: "resize terminal", Err: err}
	}
	if terminal != nil {
		terminal.PendingOwnerResize = false
		if vt := r.ensureVTerm(terminal); vt != nil {
			provisionalSnapshot := (*protocol.Snapshot)(nil)
			if previewActiveBeforeResize {
				if previewSource == nil {
					previewSource = terminal.ResizePreviewSource
				}
				provisionalSnapshot = provisionalSnapshotForResizePreview(previewSource, cols, rows)
			}
			if provisionalSnapshot != nil {
				loadSnapshotIntoVTerm(vt, provisionalSnapshot)
				terminal.PreferSnapshot = true
				terminal.Snapshot = provisionalSnapshot
				r.bumpSurfaceVersion(terminal)
				terminal.SnapshotVersion = terminal.SurfaceVersion
				r.invalidate()
				return nil
			}
			vt.Resize(int(cols), int(rows))
		}
		terminal.PreferSnapshot = false
		terminal.ResizePreviewSource = nil
		if terminal.BootstrapPending {
			// Keep the local surface snapshot geometry in sync even before the
			// first bootstrap replay completes, otherwise owner handoff resizes
			// can stay stuck on the previous tab's size until the stream emits.
			r.bumpSurfaceVersion(terminal)
			r.refreshSnapshot(terminalID)
			return nil
		}
		r.bumpSurfaceVersion(terminal)
		r.refreshSnapshot(terminalID)
	}
	return nil
}

func shouldEnterResizePreview(oldCols, oldRows, newCols, newRows int) bool {
	if oldCols <= 0 || oldRows <= 0 || newCols <= 0 || newRows <= 0 {
		return false
	}
	return newCols != oldCols || newRows != oldRows
}

func shouldPreferSnapshotDuringLocalShrink(oldCols, oldRows, newCols, newRows int) bool {
	if oldCols <= 0 || oldRows <= 0 || newCols <= 0 || newRows <= 0 {
		return false
	}
	if newCols > oldCols || newRows > oldRows {
		return false
	}
	return newCols < oldCols || newRows < oldRows
}

func provisionalSnapshotForLocalShrink(snapshot *protocol.Snapshot, cols, rows uint16) *protocol.Snapshot {
	return provisionalSnapshotForResizePreview(snapshot, cols, rows)
}

func captureResizePreviewSource(terminalID string, terminal *TerminalRuntime, snapshot *protocol.Snapshot, vt VTermLike) *protocol.Snapshot {
	if terminal != nil && !terminal.PreferSnapshot && terminal.SurfaceVersion > terminal.SnapshotVersion {
		return cloneProtocolSnapshot(snapshotFromVTerm(terminalID, vt))
	}
	if snapshot != nil {
		return cloneProtocolSnapshot(snapshot)
	}
	return cloneProtocolSnapshot(snapshotFromVTerm(terminalID, vt))
}

func provisionalSnapshotForResizePreview(snapshot *protocol.Snapshot, cols, rows uint16) *protocol.Snapshot {
	if snapshot == nil || cols == 0 || rows == 0 {
		return nil
	}
	if snapshot.Screen.IsAlternateScreen || snapshot.Modes.AlternateScreen {
		return provisionalAltScreenSnapshotForResizePreview(snapshot, cols, rows)
	}
	return provisionalNonAltSnapshotForResizePreview(snapshot, cols, rows)
}

func provisionalAltScreenSnapshotForResizePreview(snapshot *protocol.Snapshot, cols, rows uint16) *protocol.Snapshot {
	cloned := cloneProtocolSnapshot(snapshot)
	if cloned == nil {
		return nil
	}
	cloned.Size = protocol.Size{Cols: cols, Rows: rows}
	cloned.Screen.Cells = cropProtocolRowsForViewport(snapshot.Screen.Cells, int(cols), int(rows))
	cloned.ScreenTimestamps = resizeTimeSlice(snapshot.ScreenTimestamps, int(rows))
	cloned.ScreenRowKinds = resizeStringSlice(snapshot.ScreenRowKinds, int(rows))
	cloned.Timestamp = time.Now()
	clampSnapshotCursorToSize(cloned, cols, rows)
	return cloned
}

func provisionalNonAltSnapshotForResizePreview(snapshot *protocol.Snapshot, cols, rows uint16) *protocol.Snapshot {
	cloned := cloneProtocolSnapshot(snapshot)
	if cloned == nil {
		return nil
	}
	cloned.Size = protocol.Size{Cols: cols, Rows: rows}
	reflowedRows, reflowedTimes, reflowedKinds, visibleTopRow := reflowSnapshotRowsForPreview(snapshot, int(cols))
	screenRows := int(rows)
	screenStart := previewScreenStartForNonAltResize(snapshot, reflowedRows, screenRows, visibleTopRow)
	cloned.Scrollback = cloneProtocolRows(reflowedRows[:screenStart])
	cloned.ScrollbackTimestamps = append([]time.Time(nil), reflowedTimes[:screenStart]...)
	cloned.ScrollbackRowKinds = append([]string(nil), reflowedKinds[:screenStart]...)
	cloned.Screen = protocol.ScreenData{
		Cells:             cloneAndPadProtocolRowsRect(reflowedRows, screenStart, screenRows, int(cols)),
		IsAlternateScreen: false,
	}
	cloned.ScreenTimestamps = resizeTimeSlice(reflowedTimes[screenStart:], screenRows)
	cloned.ScreenRowKinds = resizeStringSlice(reflowedKinds[screenStart:], screenRows)
	cloned.Timestamp = time.Now()
	clampSnapshotCursorToSize(cloned, cols, rows)
	return cloned
}

func previewScreenStartForNonAltResize(snapshot *protocol.Snapshot, reflowedRows [][]protocol.Cell, screenRows int, visibleTopRow int) int {
	if screenRows <= 0 || len(reflowedRows) <= screenRows {
		return 0
	}
	if visibleTopRow < 0 {
		return 0
	}
	maxStart := len(reflowedRows) - screenRows
	if visibleTopRow > maxStart {
		return maxStart
	}
	return visibleTopRow
}

func reflowSnapshotRowsForPreview(snapshot *protocol.Snapshot, cols int) ([][]protocol.Cell, []time.Time, []string, int) {
	if snapshot == nil || cols <= 0 {
		return nil, nil, nil, 0
	}
	scrollbackRows := len(snapshot.Scrollback)
	sourceRows := make([][]protocol.Cell, 0, len(snapshot.Scrollback)+len(snapshot.Screen.Cells))
	sourceRows = append(sourceRows, snapshot.Scrollback...)
	sourceRows = append(sourceRows, snapshot.Screen.Cells...)
	sourceTimes := append([]time.Time(nil), snapshot.ScrollbackTimestamps...)
	sourceTimes = append(sourceTimes, snapshot.ScreenTimestamps...)
	sourceKinds := append([]string(nil), snapshot.ScrollbackRowKinds...)
	sourceKinds = append(sourceKinds, snapshot.ScreenRowKinds...)
	sourceRows, sourceTimes, sourceKinds = trimTrailingBlankPreviewRows(sourceRows, sourceTimes, sourceKinds)
	var rows [][]protocol.Cell
	var times []time.Time
	var kinds []string
	visibleTopRow := 0
	for i := 0; i < len(sourceRows); i++ {
		if i == scrollbackRows {
			visibleTopRow = len(rows)
		}
		logicalRow := trimPreviewSourceRow(sourceRows[i])
		rowTime := previewSliceTimeAt(sourceTimes, i)
		rowKind := previewSliceStringAt(sourceKinds, i)
		for i+1 < len(sourceRows) && previewSliceStringAt(sourceKinds, i+1) == protocol.SnapshotRowKindWrapped {
			i++
			logicalRow = append(logicalRow, trimPreviewSourceRow(sourceRows[i])...)
			if rowTime.IsZero() {
				rowTime = previewSliceTimeAt(sourceTimes, i)
			}
		}
		if len(logicalRow) == 0 {
			rows = append(rows, nil)
			times = append(times, rowTime)
			kinds = append(kinds, rowKind)
			continue
		}
		firstSegment := true
		for len(logicalRow) > 0 {
			cut := previewReflowCut(logicalRow, cols)
			segment := cloneProtocolCellRow(logicalRow[:cut])
			rows = append(rows, segment)
			times = append(times, rowTime)
			if firstSegment {
				kinds = append(kinds, rowKind)
				firstSegment = false
			} else {
				kinds = append(kinds, protocol.SnapshotRowKindWrapped)
			}
			logicalRow = logicalRow[cut:]
		}
	}
	return rows, times, kinds, visibleTopRow
}

func previewReflowCut(row []protocol.Cell, cols int) int {
	if len(row) == 0 || cols <= 0 {
		return 0
	}
	width := 0
	cut := 0
	for cut < len(row) {
		cellWidth := row[cut].Width
		if cellWidth <= 0 {
			cellWidth = 1
		}
		if width > 0 && width+cellWidth > cols {
			break
		}
		if width == 0 && cellWidth > cols {
			return cut + 1
		}
		width += cellWidth
		cut++
		if width >= cols {
			break
		}
	}
	if cut >= len(row) {
		return cut
	}
	if cut <= 0 {
		return 1
	}
	return cut
}

func isPreviewSpaceCell(cell protocol.Cell) bool {
	return cell.Width <= 1 && cell.Style == (protocol.CellStyle{}) && strings.TrimSpace(cell.Content) == ""
}

func trimTrailingBlankPreviewRows(rows [][]protocol.Cell, times []time.Time, kinds []string) ([][]protocol.Cell, []time.Time, []string) {
	last := len(rows) - 1
	for last >= 0 && len(trimProtocolCellRow(rows[last])) == 0 {
		last--
	}
	if last < 0 {
		return nil, nil, nil
	}
	rows = rows[:last+1]
	if len(times) > len(rows) {
		times = times[:len(rows)]
	}
	if len(kinds) > len(rows) {
		kinds = kinds[:len(rows)]
	}
	return rows, times, kinds
}

func trimPreviewSourceRow(row []protocol.Cell) []protocol.Cell {
	trimmed := trimProtocolCellRow(row)
	if len(trimmed) == 0 {
		return nil
	}
	return trimmed
}

func cropProtocolRowsForViewport(rows [][]protocol.Cell, cols, height int) [][]protocol.Cell {
	if height <= 0 {
		return nil
	}
	out := make([][]protocol.Cell, height)
	for rowIndex := 0; rowIndex < height; rowIndex++ {
		out[rowIndex] = make([]protocol.Cell, cols)
		for col := range out[rowIndex] {
			out[rowIndex][col] = protocolBlankCell()
		}
		if rowIndex >= len(rows) {
			continue
		}
		source := rows[rowIndex]
		copy(out[rowIndex], source[:runtimeMinInt(len(source), cols)])
	}
	return out
}

func clampSnapshotCursorToSize(cloned *protocol.Snapshot, cols, rows uint16) {
	if cloned == nil || cols == 0 || rows == 0 {
		return
	}
	if cloned.Cursor.Row >= int(rows) || cloned.Cursor.Col >= int(cols) {
		cloned.Cursor.Visible = false
		cloned.Cursor.Row = runtimeMinInt(cloned.Cursor.Row, int(rows)-1)
		cloned.Cursor.Col = runtimeMinInt(cloned.Cursor.Col, int(cols)-1)
	}
}

func resizeTimeSlice(values []time.Time, size int) []time.Time {
	if size <= 0 {
		return nil
	}
	out := make([]time.Time, size)
	copy(out, values)
	return out
}

func resizeStringSlice(values []string, size int) []string {
	if size <= 0 {
		return nil
	}
	out := make([]string, size)
	copy(out, values)
	return out
}

func previewSliceTimeAt(values []time.Time, index int) time.Time {
	if index < 0 || index >= len(values) {
		return time.Time{}
	}
	return values[index]
}

func previewSliceStringAt(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return strings.TrimSpace(values[index])
}

func terminalAlreadySized(terminal *TerminalRuntime, cols, rows uint16) bool {
	if terminal == nil || cols == 0 || rows == 0 {
		return false
	}
	if terminal.ResizePreviewSource == nil && terminal.Snapshot != nil && terminal.Snapshot.Size.Cols == cols && terminal.Snapshot.Size.Rows == rows {
		return true
	}
	if terminal.VTerm == nil {
		return false
	}
	currentCols, currentRows := terminal.VTerm.Size()
	return currentCols == int(cols) && currentRows == int(rows)
}

func (r *Runtime) TerminalAlreadySized(terminalID string, cols, rows uint16) bool {
	if r == nil || r.registry == nil || terminalID == "" {
		return false
	}
	return terminalAlreadySized(r.registry.Get(terminalID), cols, rows)
}

func runtimeMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func runtimeMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
