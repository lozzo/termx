package runtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/terminalmeta"
	"github.com/lozzow/termx/tuiv2/shared"
	localvterm "github.com/lozzow/termx/vterm"
)

type resizeFillStateSeeder interface {
	SeedResizeFillState(tailStartCol int, tailBG []localvterm.CellStyle, bottomBG string, bottomStartRow int)
}

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
		ownerPaneID := r.syncTerminalOwnership(terminal)
		if ownerPaneID == "" {
			if terminal.RequiresExplicitOwner {
				return nil
			}
		} else if ownerPaneID != paneID {
			return nil
		}
		forceResize := terminal.PendingOwnerResize
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
			vt.Resize(int(cols), int(rows))
			seedResizeFillFromSnapshot(vt, prevSnapshot, oldCols, oldRows, int(cols), int(rows))
		}
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

func seedResizeFillFromSnapshot(vt VTermLike, snapshot *protocol.Snapshot, oldCols, oldRows, newCols, newRows int) {
	if vt == nil || snapshot == nil || oldCols <= 0 || oldRows <= 0 || (newCols <= oldCols && newRows <= oldRows) {
		return
	}
	screen := vt.ScreenContent()
	if len(screen.Cells) == 0 {
		return
	}
	var payload strings.Builder
	tailStartCol := runtimeMaxInt(0, oldCols-1)
	tailBG := make([]localvterm.CellStyle, runtimeMinInt(oldRows, newRows))
	bottomBG := ""
	bottomStartRow := 0
	if newCols > oldCols {
		limitRows := runtimeMinInt(len(tailBG), len(screen.Cells))
		for y := 0; y < limitRows; y++ {
			bg := snapshotRowTailBackground(snapshot, y, oldCols)
			if bg == "" {
				continue
			}
			tailBG[y].BG = bg
			appendResizeFillSegmentsANSI(&payload, screen.Cells[y], y, tailStartCol, newCols, bg)
		}
	}
	if newRows > oldRows {
		bottomBG = snapshotBottomFillBackground(snapshot, oldRows, runtimeMaxInt(1, runtimeMinInt(oldCols, newCols)))
		bottomStartRow = runtimeMaxInt(0, oldRows)
		if bottomBG != "" {
			for y := runtimeMaxInt(0, oldRows); y < runtimeMinInt(newRows, len(screen.Cells)); y++ {
				appendResizeFillSegmentsANSI(&payload, screen.Cells[y], y, 0, newCols, bottomBG)
			}
		}
	}
	if payload.Len() == 0 {
		if seeder, ok := vt.(resizeFillStateSeeder); ok {
			seeder.SeedResizeFillState(tailStartCol, tailBG, bottomBG, bottomStartRow)
		}
		return
	}
	payload.WriteString("\x1b[0m")
	_, _ = vt.Write([]byte(payload.String()))
	if seeder, ok := vt.(resizeFillStateSeeder); ok {
		seeder.SeedResizeFillState(tailStartCol, tailBG, bottomBG, bottomStartRow)
	}
}

func appendResizeFillSegmentsANSI(out *strings.Builder, row []localvterm.Cell, rowIndex, startCol, endCol int, bg string) {
	if out == nil || rowIndex < 0 || startCol >= endCol || bg == "" {
		return
	}
	start := -1
	flush := func(col int) {
		if start < 0 || col <= start {
			start = -1
			return
		}
		out.WriteString(fmt.Sprintf("\x1b[%d;%dH", rowIndex+1, start+1))
		out.WriteString(backgroundANSI(bg))
		out.WriteString(strings.Repeat(" ", col-start))
		start = -1
	}
	for col := runtimeMaxInt(0, startCol); col < endCol; col++ {
		fillable := true
		if col < len(row) {
			fillable = runtimeCellNeedsResizeFill(row[col])
		}
		if fillable {
			if start < 0 {
				start = col
			}
			continue
		}
		flush(col)
	}
	flush(endCol)
}

func runtimeCellNeedsResizeFill(cell localvterm.Cell) bool {
	if cell.Style.BG != "" {
		return false
	}
	if cell.Width > 1 {
		return false
	}
	return strings.TrimSpace(cell.Content) == ""
}

func snapshotRowTailBackground(snapshot *protocol.Snapshot, rowIndex, width int) string {
	if snapshot == nil || rowIndex < 0 || rowIndex >= len(snapshot.Screen.Cells) || width <= 0 {
		return ""
	}
	row := snapshot.Screen.Cells[rowIndex]
	if width > len(row) {
		width = len(row)
	}
	for x := width - 1; x >= 0; x-- {
		if row[x].Style.BG != "" {
			return row[x].Style.BG
		}
	}
	return ""
}

func snapshotBottomFillBackground(snapshot *protocol.Snapshot, oldRows, width int) string {
	if snapshot == nil || oldRows <= 0 {
		return ""
	}
	limitRows := runtimeMinInt(oldRows, len(snapshot.Screen.Cells))
	for rowIndex := limitRows - 1; rowIndex >= 0; rowIndex-- {
		if bg := snapshotRowTailBackground(snapshot, rowIndex, width); bg != "" {
			return bg
		}
	}
	return ""
}

func backgroundANSI(hex string) string {
	if len(hex) != 7 || hex[0] != '#' {
		return ""
	}
	r, errR := strconv.ParseUint(hex[1:3], 16, 8)
	g, errG := strconv.ParseUint(hex[3:5], 16, 8)
	b, errB := strconv.ParseUint(hex[5:7], 16, 8)
	if errR != nil || errG != nil || errB != nil {
		return ""
	}
	return fmt.Sprintf("\x1b[0;48;2;%d;%d;%dm", r, g, b)
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
