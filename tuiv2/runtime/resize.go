package runtime

import (
	"context"
	"fmt"
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
			if shouldPreferSnapshotDuringLocalShrink(oldCols, oldRows, int(cols), int(rows)) {
				source := prevSnapshot
				if source == nil {
					source = snapshotFromVTerm(terminalID, vt)
				}
				provisionalSnapshot = provisionalSnapshotForLocalShrink(source, cols, rows)
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
	if snapshot == nil || cols == 0 || rows == 0 {
		return nil
	}
	cloned := *snapshot
	cloned.Size = protocol.Size{Cols: cols, Rows: rows}
	cloned.Screen = protocol.ScreenData{
		Cells:             cloneProtocolCells2D(snapshot.Screen.Cells),
		IsAlternateScreen: snapshot.Screen.IsAlternateScreen,
	}
	cloned.Scrollback = cloneProtocolCells2D(snapshot.Scrollback)
	cloned.ScreenTimestamps = append([]time.Time(nil), snapshot.ScreenTimestamps...)
	cloned.ScrollbackTimestamps = append([]time.Time(nil), snapshot.ScrollbackTimestamps...)
	cloned.ScreenRowKinds = append([]string(nil), snapshot.ScreenRowKinds...)
	cloned.ScrollbackRowKinds = append([]string(nil), snapshot.ScrollbackRowKinds...)
	if cloned.Cursor.Row >= int(rows) || cloned.Cursor.Col >= int(cols) {
		cloned.Cursor.Visible = false
		cloned.Cursor.Row = runtimeMinInt(cloned.Cursor.Row, int(rows)-1)
		cloned.Cursor.Col = runtimeMinInt(cloned.Cursor.Col, int(cols)-1)
	}
	return &cloned
}

func cloneProtocolCells2D(rows [][]protocol.Cell) [][]protocol.Cell {
	if len(rows) == 0 {
		return nil
	}
	cloned := make([][]protocol.Cell, len(rows))
	for i, row := range rows {
		cloned[i] = append([]protocol.Cell(nil), row...)
	}
	return cloned
}

func terminalAlreadySized(terminal *TerminalRuntime, cols, rows uint16) bool {
	if terminal == nil || cols == 0 || rows == 0 {
		return false
	}
	if terminal.Snapshot != nil && terminal.Snapshot.Size.Cols == cols && terminal.Snapshot.Size.Rows == rows {
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
