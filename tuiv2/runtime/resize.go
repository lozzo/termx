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
	}
	if err := r.client.Resize(ctx, binding.Channel, cols, rows); err != nil {
		return shared.UserVisibleError{Op: "resize terminal", Err: err}
	}
	if terminal != nil {
		prevSnapshot := terminal.Snapshot
		oldCols, oldRows := 0, 0
		if terminal.VTerm != nil {
			oldCols, oldRows = terminal.VTerm.Size()
		} else if prevSnapshot != nil {
			oldCols = int(prevSnapshot.Size.Cols)
			oldRows = int(prevSnapshot.Size.Rows)
		}
		terminal.PendingOwnerResize = false
		if vt := r.ensureVTerm(terminal); vt != nil {
			provisionalSnapshot := (*protocol.Snapshot)(nil)
			if shouldEnterResizePreview(oldCols, oldRows, int(cols), int(rows)) {
				source := terminal.ResizePreviewSource
				if source == nil {
					source = captureResizePreviewSource(terminalID, prevSnapshot, vt)
					terminal.ResizePreviewSource = source
				}
				provisionalSnapshot = provisionalSnapshotForResizePreview(source, cols, rows)
			}
			vt.Resize(int(cols), int(rows))
			if provisionalSnapshot != nil {
				terminal.PreferSnapshot = true
				terminal.Snapshot = provisionalSnapshot
				r.bumpSurfaceVersion(terminal)
				terminal.SnapshotVersion = terminal.SurfaceVersion
				r.invalidate()
				return nil
			}
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

func captureResizePreviewSource(terminalID string, snapshot *protocol.Snapshot, vt VTermLike) *protocol.Snapshot {
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
	reflowedRows, reflowedTimes, reflowedKinds := reflowSnapshotRowsForPreview(snapshot, int(cols))
	screenRows := int(rows)
	screenStart := runtimeMaxInt(0, len(reflowedRows)-screenRows)
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

func reflowSnapshotRowsForPreview(snapshot *protocol.Snapshot, cols int) ([][]protocol.Cell, []time.Time, []string) {
	if snapshot == nil || cols <= 0 {
		return nil, nil, nil
	}
	sourceRows := make([][]protocol.Cell, 0, len(snapshot.Scrollback)+len(snapshot.Screen.Cells))
	sourceRows = append(sourceRows, snapshot.Scrollback...)
	sourceRows = append(sourceRows, snapshot.Screen.Cells...)
	sourceTimes := append([]time.Time(nil), snapshot.ScrollbackTimestamps...)
	sourceTimes = append(sourceTimes, snapshot.ScreenTimestamps...)
	sourceKinds := append([]string(nil), snapshot.ScrollbackRowKinds...)
	sourceKinds = append(sourceKinds, snapshot.ScreenRowKinds...)
	var rows [][]protocol.Cell
	var times []time.Time
	var kinds []string
	for i, row := range sourceRows {
		trimmed := trimPreviewSourceRow(row)
		if len(trimmed) == 0 {
			rows = append(rows, nil)
			times = append(times, previewSliceTimeAt(sourceTimes, i))
			kinds = append(kinds, previewSliceStringAt(sourceKinds, i))
			continue
		}
		for len(trimmed) > 0 {
			width := 0
			cut := 0
			for cut < len(trimmed) {
				cellWidth := trimmed[cut].Width
				if cellWidth <= 0 {
					cellWidth = 1
				}
				if width > 0 && width+cellWidth > cols {
					break
				}
				if width == 0 && cellWidth > cols {
					cut++
					width = cols
					break
				}
				width += cellWidth
				cut++
				if width >= cols {
					break
				}
			}
			if cut <= 0 {
				cut = 1
			}
			rows = append(rows, cloneProtocolCellRow(trimmed[:cut]))
			times = append(times, previewSliceTimeAt(sourceTimes, i))
			kinds = append(kinds, previewSliceStringAt(sourceKinds, i))
			trimmed = trimmed[cut:]
		}
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
