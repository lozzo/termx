package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	localvterm "github.com/lozzow/termx/termx-vterm/vterm"
	"github.com/lozzow/termx/tuiv2/shared"
)

const (
	alternateScrollbackLimit       = 10000
	materializedScrollbackRowLimit = 12000
)

type timestampedSnapshotLoader interface {
	LoadSnapshotWithTimestamps(scrollback [][]localvterm.Cell, scrollbackTimestamps []time.Time, screen localvterm.ScreenData, screenTimestamps []time.Time, cursor localvterm.CursorState, modes localvterm.TerminalModes)
}

type timestampedSnapshotSource interface {
	ScreenTimestamps() []time.Time
	ScrollbackTimestamps() []time.Time
}

type metadataSnapshotLoader interface {
	LoadSnapshotWithMetadata(scrollback [][]localvterm.Cell, scrollbackTimestamps []time.Time, scrollbackRowKinds []string, screen localvterm.ScreenData, screenTimestamps []time.Time, screenRowKinds []string, cursor localvterm.CursorState, modes localvterm.TerminalModes)
}

type extendedMetadataSnapshotLoader interface {
	LoadSnapshotWithExtendedMetadata(scrollback [][]localvterm.Cell, scrollbackTimestamps []time.Time, scrollbackRowKinds []string, scrollbackWrapped []bool, screen localvterm.ScreenData, screenTimestamps []time.Time, screenRowKinds []string, screenWrapped []bool, cursor localvterm.CursorState, modes localvterm.TerminalModes)
}

type sizedExtendedMetadataSnapshotLoader interface {
	LoadSizedSnapshotWithExtendedMetadata(cols, rows int, scrollback [][]localvterm.Cell, scrollbackTimestamps []time.Time, scrollbackRowKinds []string, scrollbackWrapped []bool, screen localvterm.ScreenData, screenTimestamps []time.Time, screenRowKinds []string, screenWrapped []bool, cursor localvterm.CursorState, modes localvterm.TerminalModes)
}

type ownershipSnapshotLoader interface {
	LoadSnapshotWithOwnership(scrollback [][]localvterm.Cell, scrollbackTimestamps []time.Time, scrollbackRowKinds []string, scrollbackWrapped []bool, scrollbackOwnership []string, screen localvterm.ScreenData, screenTimestamps []time.Time, screenRowKinds []string, screenWrapped []bool, screenOwnership []string, cursor localvterm.CursorState, modes localvterm.TerminalModes)
}

type sizedOwnershipSnapshotLoader interface {
	LoadSizedSnapshotWithOwnership(cols, rows int, scrollback [][]localvterm.Cell, scrollbackTimestamps []time.Time, scrollbackRowKinds []string, scrollbackWrapped []bool, scrollbackOwnership []string, screen localvterm.ScreenData, screenTimestamps []time.Time, screenRowKinds []string, screenWrapped []bool, screenOwnership []string, cursor localvterm.CursorState, modes localvterm.TerminalModes)
}

type metadataSnapshotSource interface {
	ScreenRowKinds() []string
	ScrollbackRowKinds() []string
}

type wrappedSnapshotSource interface {
	ScreenWrapped() []bool
	ScrollbackWrapped() []bool
}

type ownershipSnapshotSource interface {
	ScreenOwnership() []string
	ScrollbackOwnership() []string
}

type rowSnapshotSource interface {
	Size() (int, int)
	CursorState() localvterm.CursorState
	Modes() localvterm.TerminalModes
	IsAltScreen() bool
	ScreenRowCount() int
	ScrollbackRowCount() int
	ScreenRowView(row int) []localvterm.Cell
	ScrollbackRowView(row int) []localvterm.Cell
	ScreenRowTimestampAt(row int) time.Time
	ScrollbackRowTimestampAt(row int) time.Time
	ScreenRowKindAt(row int) string
	ScrollbackRowKindAt(row int) string
	ScreenRowWrappedAt(row int) bool
	ScrollbackRowWrappedAt(row int) bool
	ScreenRowOwnershipAt(row int) string
	ScrollbackRowOwnershipAt(row int) string
}

func (r *Runtime) LoadSnapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error) {
	if r == nil || r.client == nil {
		return nil, shared.UserVisibleError{Op: "snapshot terminal", Err: fmt.Errorf("runtime client is nil")}
	}
	snapshot, err := r.client.Snapshot(ctx, terminalID, offset, limit)
	if err != nil {
		return nil, shared.UserVisibleError{Op: "snapshot terminal", Err: err}
	}
	traceRuntimeSnapshot("runtime.load_snapshot.received", snapshot, "requested_offset", offset, "requested_limit", limit)
	if offset > 0 {
		return snapshot, nil
	}
	terminal := r.registry.GetOrCreate(terminalID)
	if terminal != nil {
		terminal.Snapshot = snapshot
		applyLatestSnapshotRuntimeState(terminal, snapshot)
		if snapshot == nil || !snapshotUsesAlternateScreen(snapshot) {
			terminal.AlternateScrollback = nil
		}
		terminal.PreferSnapshot = false
		if offset == 0 && snapshot != nil {
			if limit > 0 && protocol.HasExplicitRowOwnership(snapshot.ScrollbackOwnership, len(snapshot.Scrollback)) {
				terminal.CommittedHistoryExhausted = !snapshot.ScrollbackHasMore
			} else if !protocol.HasExplicitRowOwnership(snapshot.ScrollbackOwnership, len(snapshot.Scrollback)) {
				terminal.CommittedHistoryExhausted = false
			}
		}
		r.ensureVTerm(terminal)
		loadSnapshotIntoVTerm(terminal.VTerm, snapshot)
		if terminal.VTerm != nil {
			traceRuntimeVTermRows("runtime.load_snapshot.vterm_scrollback_after_load", terminalID, terminal.VTerm.ScrollbackContent())
			traceRuntimeVTermRows("runtime.load_snapshot.vterm_screen_after_load", terminalID, terminal.VTerm.ScreenContent().Cells)
		}
		r.bumpSurfaceVersion(terminal)
		terminal.SnapshotVersion = terminal.SurfaceVersion
		terminal.CommittedLoadingDepth = 0
		r.touch()
	}
	return snapshot, nil
}

func (r *Runtime) LoadGridViewport(ctx context.Context, terminalID string, offset, limit, cols int) (*protocol.Snapshot, error) {
	if r == nil || r.client == nil {
		return nil, shared.UserVisibleError{Op: "load terminal history", Err: fmt.Errorf("runtime client is nil")}
	}
	viewport, err := r.client.GridViewport(ctx, terminalID, offset, limit, cols)
	if err != nil {
		return nil, shared.UserVisibleError{Op: "load terminal history", Err: err}
	}
	traceRuntimeGridViewport("runtime.load_grid_viewport.received", terminalID, viewport, "requested_offset", offset, "requested_limit", limit, "requested_cols", cols)
	snapshot := snapshotFromGridViewport(terminalID, viewport)
	traceRuntimeSnapshot("runtime.load_grid_viewport.snapshot", snapshot, "requested_offset", offset, "requested_limit", limit, "requested_cols", cols)
	return snapshot, nil
}

func (r *Runtime) ApplyGridViewportPage(terminalID string, page *protocol.Snapshot, offset int) bool {
	if r == nil || r.registry == nil || terminalID == "" || page == nil || len(page.Scrollback) == 0 {
		return false
	}
	terminal := r.registry.Get(terminalID)
	if terminal == nil || terminal.Snapshot == nil {
		return false
	}
	current := terminal.Snapshot
	if offset < 0 {
		return false
	}
	traceRuntimeSnapshot("runtime.apply_grid_viewport.current_before", current, "page_offset", offset)
	traceRuntimeSnapshot("runtime.apply_grid_viewport.page", page, "page_offset", offset)
	if page.Size.Cols > 0 && current.Size.Cols > 0 && page.Size.Cols != current.Size.Cols {
		return false
	}
	if offset > 0 && offset != snapshotScrollbackLoadedDepth(current) {
		return false
	}
	if offset > 0 && !historyPageContinuesSnapshot(current, page) {
		return false
	}
	merged := cloneProtocolSnapshot(current)
	if offset == 0 {
		merged.Scrollback = protocol.CloneCompactRows(page.Scrollback)
		merged.ScrollbackTimestamps = append([]time.Time(nil), page.ScrollbackTimestamps...)
		merged.ScrollbackRowKinds = append([]string(nil), page.ScrollbackRowKinds...)
		merged.ScrollbackWrapped = append([]bool(nil), page.ScrollbackWrapped...)
		merged.ScrollbackOwnership = append([]string(nil), page.ScrollbackOwnership...)
		merged.ScrollbackOffset = page.ScrollbackOffset
		merged.ScrollbackTotal = page.ScrollbackTotal
		merged.ScrollbackLogicalTotal = page.ScrollbackLogicalTotal
		merged.ScrollbackHasMore = page.ScrollbackHasMore
		merged.ScrollbackLoadedRows = page.ScrollbackLoadedRows
		merged.HistoryGeneration = page.HistoryGeneration
		merged.ScrollbackFirstRowID = page.ScrollbackFirstRowID
		merged.ScrollbackLastRowID = page.ScrollbackLastRowID
		protocol.TrimSnapshotScrollbackScreenVisualOverlap(merged)
		trimSnapshotScrollbackWindow(merged, materializedScrollbackRowLimit, false)
	} else {
		mergedOffset := current.ScrollbackOffset
		merged.Scrollback = append(protocol.CloneCompactRows(page.Scrollback), merged.Scrollback...)
		merged.ScrollbackTimestamps = append(append([]time.Time(nil), page.ScrollbackTimestamps...), merged.ScrollbackTimestamps...)
		merged.ScrollbackRowKinds = append(append([]string(nil), page.ScrollbackRowKinds...), merged.ScrollbackRowKinds...)
		merged.ScrollbackWrapped = append(append([]bool(nil), page.ScrollbackWrapped...), merged.ScrollbackWrapped...)
		merged.ScrollbackOwnership = append(append([]string(nil), page.ScrollbackOwnership...), merged.ScrollbackOwnership...)
		merged.ScrollbackOffset = mergedOffset
		merged.ScrollbackTotal = maxInt(page.ScrollbackTotal, current.ScrollbackTotal)
		merged.ScrollbackLogicalTotal = maxInt(page.ScrollbackLogicalTotal, current.ScrollbackLogicalTotal)
		merged.ScrollbackHasMore = page.ScrollbackHasMore
		merged.ScrollbackLoadedRows = maxInt(page.ScrollbackLoadedRows, current.ScrollbackLoadedRows)
		merged.HistoryGeneration = page.HistoryGeneration
		merged.ScrollbackFirstRowID, merged.ScrollbackLastRowID = mergedCanonicalRowWindow(page, current)
		trimSnapshotScrollbackWindow(merged, materializedScrollbackRowLimit, true)
	}
	merged.Timestamp = time.Now()
	terminal.Snapshot = merged
	if offset == 0 {
		if terminal.VTerm != nil {
			loadSnapshotIntoVTerm(terminal.VTerm, merged)
		}
		applyLatestSnapshotRuntimeState(terminal, merged)
	} else if loadedDepth := snapshotScrollbackLoadedDepth(page); loadedDepth > terminal.CommittedLoadedDepth {
		terminal.CommittedLoadedDepth = loadedDepth
	}
	terminal.CommittedHistoryExhausted = !page.ScrollbackHasMore
	r.bumpSurfaceVersion(terminal)
	terminal.SnapshotVersion = terminal.SurfaceVersion
	terminal.PreferSnapshot = true
	r.touch()
	traceRuntimeSnapshot("runtime.apply_grid_viewport.merged_after", merged, "page_offset", offset)
	return true
}

func applyLatestSnapshotRuntimeState(terminal *TerminalRuntime, snapshot *protocol.Snapshot) {
	if terminal == nil {
		return
	}
	if snapshot == nil {
		terminal.CommittedLoadedDepth = 0
		return
	}
	terminal.CommittedLoadedDepth = snapshotScrollbackLoadedDepth(snapshot)
}

func snapshotScrollbackLoadedDepth(snapshot *protocol.Snapshot) int {
	return protocol.SnapshotCommittedLoadedDepth(snapshot)
}

func historyPageContinuesSnapshot(current, page *protocol.Snapshot) bool {
	if current == nil || page == nil {
		return false
	}
	currentCanonical := hasCanonicalHistoryWindow(current)
	pageCanonical := hasCanonicalHistoryWindow(page)
	if !currentCanonical || !pageCanonical {
		return false
	}
	if current.HistoryGeneration != page.HistoryGeneration {
		return false
	}
	if page.ScrollbackLastRowID+1 != current.ScrollbackFirstRowID {
		return false
	}
	return true
}

func hasCanonicalHistoryWindow(snapshot *protocol.Snapshot) bool {
	return protocol.SnapshotHasCanonicalCommittedWindow(snapshot)
}

func mergedCanonicalRowWindow(older, newer *protocol.Snapshot) (uint64, uint64) {
	olderOK := hasCanonicalHistoryWindow(older)
	newerOK := hasCanonicalHistoryWindow(newer)
	switch {
	case olderOK && newerOK:
		return older.ScrollbackFirstRowID, newer.ScrollbackLastRowID
	case olderOK:
		return older.ScrollbackFirstRowID, older.ScrollbackLastRowID
	case newerOK:
		return newer.ScrollbackFirstRowID, newer.ScrollbackLastRowID
	default:
		return 0, 0
	}
}

func trimSnapshotScrollbackWindow(snapshot *protocol.Snapshot, limit int, trimNewest bool) {
	if snapshot == nil || limit <= 0 || len(snapshot.Scrollback) <= limit {
		return
	}
	drop := len(snapshot.Scrollback) - limit
	// Canonical committed-row coordinates describe the loaded row window, not
	// the client-side materialized tail retained after bounded trimming.
	if trimNewest {
		keep := len(snapshot.Scrollback) - drop
		committedDrop := protocol.CountCommittedRowOwnershipRange(snapshot.ScrollbackOwnership, keep, len(snapshot.ScrollbackOwnership))
		snapshot.Scrollback = protocol.CloneCompactRows(snapshot.Scrollback[:keep])
		snapshot.ScrollbackTimestamps = cloneTimePrefix(snapshot.ScrollbackTimestamps, keep)
		snapshot.ScrollbackRowKinds = cloneStringPrefix(snapshot.ScrollbackRowKinds, keep)
		snapshot.ScrollbackWrapped = cloneBoolPrefix(snapshot.ScrollbackWrapped, keep)
		snapshot.ScrollbackOwnership = cloneStringPrefix(snapshot.ScrollbackOwnership, keep)
		snapshot.ScrollbackOffset += committedDrop
		return
	}
	snapshot.Scrollback = protocol.CloneCompactRows(snapshot.Scrollback[drop:])
	snapshot.ScrollbackTimestamps = cloneTimeSuffix(snapshot.ScrollbackTimestamps, drop)
	snapshot.ScrollbackRowKinds = cloneStringSuffix(snapshot.ScrollbackRowKinds, drop)
	snapshot.ScrollbackWrapped = cloneBoolSuffix(snapshot.ScrollbackWrapped, drop)
	snapshot.ScrollbackOwnership = cloneStringSuffix(snapshot.ScrollbackOwnership, drop)
	snapshot.ScrollbackHasMore = true
}

func cloneTimePrefix(values []time.Time, keep int) []time.Time {
	if keep <= 0 || len(values) < keep {
		return nil
	}
	return append([]time.Time(nil), values[:keep]...)
}

func repeatedOwnership(value string, count int) []string {
	if count <= 0 || value == "" {
		return nil
	}
	out := make([]string, count)
	for i := range out {
		out[i] = value
	}
	return out
}

func cloneStringPrefix(values []string, keep int) []string {
	if keep <= 0 || len(values) < keep {
		return nil
	}
	return append([]string(nil), values[:keep]...)
}

func cloneBoolPrefix(values []bool, keep int) []bool {
	if keep <= 0 || len(values) < keep {
		return nil
	}
	return append([]bool(nil), values[:keep]...)
}

func cloneTimeSuffix(values []time.Time, drop int) []time.Time {
	if drop < 0 || len(values) <= drop {
		return nil
	}
	return append([]time.Time(nil), values[drop:]...)
}

func cloneStringSuffix(values []string, drop int) []string {
	if drop < 0 || len(values) <= drop {
		return nil
	}
	return append([]string(nil), values[drop:]...)
}

func cloneBoolSuffix(values []bool, drop int) []bool {
	if drop < 0 || len(values) <= drop {
		return nil
	}
	return append([]bool(nil), values[drop:]...)
}

func snapshotFromGridViewport(terminalID string, viewport *protocol.GridViewport) *protocol.Snapshot {
	if viewport == nil {
		return nil
	}
	return &protocol.Snapshot{
		TerminalID:             terminalID,
		Size:                   viewport.Size,
		Scrollback:             protocol.CloneCompactRows(viewport.Rows),
		ScrollbackOffset:       viewport.ScrollbackOffset,
		ScrollbackTotal:        viewport.ScrollbackTotal,
		ScrollbackLogicalTotal: viewport.ScrollbackLogicalTotal,
		ScrollbackHasMore:      viewport.ScrollbackHasMore,
		ScrollbackLoadedRows:   viewport.LoadedRows,
		HistoryGeneration:      viewport.HistoryGeneration,
		ScrollbackFirstRowID:   viewport.FirstRowID,
		ScrollbackLastRowID:    viewport.LastRowID,
		ScrollbackTimestamps:   append([]time.Time(nil), viewport.ScrollbackTimestamps...),
		ScrollbackRowKinds:     append([]string(nil), viewport.ScrollbackRowKinds...),
		ScrollbackWrapped:      append([]bool(nil), viewport.ScrollbackWrapped...),
		ScrollbackOwnership:    append([]string(nil), viewport.RowOwnership...),
		Modes:                  protocol.TerminalModes{AutoWrap: true},
		Timestamp:              viewport.Timestamp,
	}
}

func (r *Runtime) refreshSnapshot(terminalID string) {
	if r == nil || r.registry == nil || terminalID == "" {
		return
	}
	terminal := r.registry.Get(terminalID)
	if terminal == nil {
		return
	}
	if terminal.PreferSnapshot && terminal.Snapshot != nil {
		terminal.Snapshot.Timestamp = time.Now()
		r.invalidate()
		return
	}
	if terminal.VTerm == nil {
		return
	}
	previous := terminal.Snapshot
	terminal.Snapshot = snapshotFromVTerm(terminalID, terminal.VTerm)
	preserveSnapshotHistoryMetadataFromProjection(previous, terminal.Snapshot)
	if terminal.Snapshot == nil || !snapshotUsesAlternateScreen(terminal.Snapshot) {
		terminal.AlternateScrollback = nil
	}
	terminal.PreferSnapshot = false
	terminal.SnapshotVersion = terminal.SurfaceVersion
	if terminal.Snapshot != nil {
		if loaded := snapshotScrollbackLoadedDepth(terminal.Snapshot); loaded > terminal.CommittedLoadedDepth {
			terminal.CommittedLoadedDepth = loaded
		}
		if terminal.CommittedLoadingDepth > 0 && len(terminal.Snapshot.Scrollback) >= terminal.CommittedLoadingDepth {
			terminal.CommittedLoadingDepth = 0
		}
	}
	r.invalidate()
}

func preserveSnapshotHistoryMetadataFromProjection(previous, next *protocol.Snapshot) {
	if previous == nil || next == nil || !protocol.HasExplicitRowOwnership(next.ScrollbackOwnership, len(next.Scrollback)) {
		return
	}
	next.ScrollbackLoadedRows = previous.ScrollbackLoadedRows
	next.HistoryGeneration = previous.HistoryGeneration
	next.ScrollbackFirstRowID = previous.ScrollbackFirstRowID
	next.ScrollbackLastRowID = previous.ScrollbackLastRowID
	next.ScrollbackOffset = previous.ScrollbackOffset
	next.ScrollbackTotal = previous.ScrollbackTotal
	next.ScrollbackLogicalTotal = previous.ScrollbackLogicalTotal
	next.ScrollbackHasMore = previous.ScrollbackHasMore
}

func (r *Runtime) RefreshSnapshotFromVTerm(terminalID string) bool {
	if r == nil || r.registry == nil || terminalID == "" {
		return false
	}
	terminal := r.registry.Get(terminalID)
	if terminal == nil || terminal.VTerm == nil {
		return false
	}
	r.refreshSnapshot(terminalID)
	return true
}

func loadSnapshotIntoVTerm(vt VTermLike, snap *protocol.Snapshot) {
	if vt == nil || snap == nil {
		return
	}
	traceRuntimeSnapshot("runtime.load_snapshot_into_vterm.input", snap)
	cols, rows := vt.Size()
	if snap.Size.Cols > 0 {
		cols = int(snap.Size.Cols)
	}
	if snap.Size.Rows > 0 {
		rows = int(snap.Size.Rows)
	}
	if loader, ok := vt.(sizedOwnershipSnapshotLoader); ok {
		loader.LoadSizedSnapshotWithOwnership(
			cols,
			rows,
			protocolCompactRowsToVTerm(snap.Scrollback),
			append([]time.Time(nil), snap.ScrollbackTimestamps...),
			append([]string(nil), snap.ScrollbackRowKinds...),
			append([]bool(nil), snap.ScrollbackWrapped...),
			append([]string(nil), snap.ScrollbackOwnership...),
			protocolScreenToVTerm(snap.Screen),
			append([]time.Time(nil), snap.ScreenTimestamps...),
			append([]string(nil), snap.ScreenRowKinds...),
			append([]bool(nil), snap.ScreenWrapped...),
			append([]string(nil), snap.ScreenOwnership...),
			protocolCursorToVTerm(snap.Cursor),
			protocolModesToVTerm(snap.Modes),
		)
	} else if loader, ok := vt.(sizedExtendedMetadataSnapshotLoader); ok {
		loader.LoadSizedSnapshotWithExtendedMetadata(
			cols,
			rows,
			protocolCompactRowsToVTerm(snap.Scrollback),
			append([]time.Time(nil), snap.ScrollbackTimestamps...),
			append([]string(nil), snap.ScrollbackRowKinds...),
			append([]bool(nil), snap.ScrollbackWrapped...),
			protocolScreenToVTerm(snap.Screen),
			append([]time.Time(nil), snap.ScreenTimestamps...),
			append([]string(nil), snap.ScreenRowKinds...),
			append([]bool(nil), snap.ScreenWrapped...),
			protocolCursorToVTerm(snap.Cursor),
			protocolModesToVTerm(snap.Modes),
		)
	} else if loader, ok := vt.(ownershipSnapshotLoader); ok {
		loader.LoadSnapshotWithOwnership(
			protocolCompactRowsToVTerm(snap.Scrollback),
			append([]time.Time(nil), snap.ScrollbackTimestamps...),
			append([]string(nil), snap.ScrollbackRowKinds...),
			append([]bool(nil), snap.ScrollbackWrapped...),
			append([]string(nil), snap.ScrollbackOwnership...),
			protocolScreenToVTerm(snap.Screen),
			append([]time.Time(nil), snap.ScreenTimestamps...),
			append([]string(nil), snap.ScreenRowKinds...),
			append([]bool(nil), snap.ScreenWrapped...),
			append([]string(nil), snap.ScreenOwnership...),
			protocolCursorToVTerm(snap.Cursor),
			protocolModesToVTerm(snap.Modes),
		)
	} else if loader, ok := vt.(extendedMetadataSnapshotLoader); ok {
		loader.LoadSnapshotWithExtendedMetadata(
			protocolCompactRowsToVTerm(snap.Scrollback),
			append([]time.Time(nil), snap.ScrollbackTimestamps...),
			append([]string(nil), snap.ScrollbackRowKinds...),
			append([]bool(nil), snap.ScrollbackWrapped...),
			protocolScreenToVTerm(snap.Screen),
			append([]time.Time(nil), snap.ScreenTimestamps...),
			append([]string(nil), snap.ScreenRowKinds...),
			append([]bool(nil), snap.ScreenWrapped...),
			protocolCursorToVTerm(snap.Cursor),
			protocolModesToVTerm(snap.Modes),
		)
	} else if loader, ok := vt.(metadataSnapshotLoader); ok {
		loader.LoadSnapshotWithMetadata(
			protocolCompactRowsToVTerm(snap.Scrollback),
			append([]time.Time(nil), snap.ScrollbackTimestamps...),
			append([]string(nil), snap.ScrollbackRowKinds...),
			protocolScreenToVTerm(snap.Screen),
			append([]time.Time(nil), snap.ScreenTimestamps...),
			append([]string(nil), snap.ScreenRowKinds...),
			protocolCursorToVTerm(snap.Cursor),
			protocolModesToVTerm(snap.Modes),
		)
	} else if loader, ok := vt.(timestampedSnapshotLoader); ok {
		loader.LoadSnapshotWithTimestamps(
			protocolCompactRowsToVTerm(snap.Scrollback),
			append([]time.Time(nil), snap.ScrollbackTimestamps...),
			protocolScreenToVTerm(snap.Screen),
			append([]time.Time(nil), snap.ScreenTimestamps...),
			protocolCursorToVTerm(snap.Cursor),
			protocolModesToVTerm(snap.Modes),
		)
	} else {
		vt.LoadSnapshotWithScrollback(protocolCompactRowsToVTerm(snap.Scrollback), protocolScreenToVTerm(snap.Screen), protocolCursorToVTerm(snap.Cursor), protocolModesToVTerm(snap.Modes))
	}
	if cols > 0 && rows > 0 {
		vt.Resize(cols, rows)
	}
}

func snapshotFromVTerm(terminalID string, vt VTermLike) *protocol.Snapshot {
	if vt == nil {
		return nil
	}
	if source, ok := vt.(rowSnapshotSource); ok {
		return snapshotFromRowSource(terminalID, source)
	}
	screenTimestamps := []time.Time(nil)
	scrollbackTimestamps := []time.Time(nil)
	if source, ok := vt.(timestampedSnapshotSource); ok {
		screenTimestamps = source.ScreenTimestamps()
		scrollbackTimestamps = source.ScrollbackTimestamps()
	}
	screenRowKinds := []string(nil)
	scrollbackRowKinds := []string(nil)
	if source, ok := vt.(metadataSnapshotSource); ok {
		screenRowKinds = source.ScreenRowKinds()
		scrollbackRowKinds = source.ScrollbackRowKinds()
	}
	screenWrapped := []bool(nil)
	scrollbackWrapped := []bool(nil)
	if source, ok := vt.(wrappedSnapshotSource); ok {
		screenWrapped = source.ScreenWrapped()
		scrollbackWrapped = source.ScrollbackWrapped()
	}
	screenOwnership := []string(nil)
	scrollbackOwnership := []string(nil)
	if source, ok := vt.(ownershipSnapshotSource); ok {
		screenOwnership = source.ScreenOwnership()
		scrollbackOwnership = source.ScrollbackOwnership()
	}
	cols, rows := vt.Size()
	outRows := make([][]protocol.Cell, 0)
	backlog := make([]protocol.CompactRow, 0)
	isAlternateScreen := false
	if source, ok := vt.(rowSnapshotSource); ok {
		isAlternateScreen = source.IsAltScreen()
		backlog = make([]protocol.CompactRow, source.ScrollbackRowCount())
		for i := 0; i < len(backlog); i++ {
			backlog[i] = protocol.CompactRowFromCellsPreserveTrailingBlankCells(protocolCellsFromVTermRow(source.ScrollbackRowView(i)), true)
		}
		outRows = make([][]protocol.Cell, source.ScreenRowCount())
		for i := 0; i < len(outRows); i++ {
			outRows[i] = protocolCellsFromVTermRow(source.ScreenRowView(i))
		}
	} else {
		screen := vt.ScreenContent()
		isAlternateScreen = screen.IsAlternateScreen
		outRows = make([][]protocol.Cell, 0, len(screen.Cells))
		for _, row := range screen.Cells {
			out := make([]protocol.Cell, 0, len(row))
			for _, cell := range row {
				out = append(out, protocolCellFromVTermCell(cell))
			}
			outRows = append(outRows, out)
		}
		scrollback := vt.ScrollbackContent()
		backlog = make([]protocol.CompactRow, 0, len(scrollback))
		for _, row := range scrollback {
			backlog = append(backlog, protocol.CompactRowFromCellsPreserveTrailingBlankCells(protocolCellsFromVTermRow(row), true))
		}
	}
	snapshot := &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: uint16(cols), Rows: uint16(rows)},
		Screen: protocol.ScreenData{
			Cells:             outRows,
			IsAlternateScreen: isAlternateScreen,
		},
		Scrollback:           backlog,
		ScreenTimestamps:     append([]time.Time(nil), screenTimestamps...),
		ScrollbackTimestamps: append([]time.Time(nil), scrollbackTimestamps...),
		ScreenRowKinds:       append([]string(nil), screenRowKinds...),
		ScrollbackRowKinds:   append([]string(nil), scrollbackRowKinds...),
		ScreenWrapped:        append([]bool(nil), screenWrapped...),
		ScrollbackWrapped:    append([]bool(nil), scrollbackWrapped...),
		ScreenOwnership:      repeatedOwnership(protocol.RowOwnershipScreen, len(outRows)),
		ScrollbackOwnership:  append([]string(nil), scrollbackOwnership...),
		Cursor:               protocolCursorFromVTerm(vt.CursorState()),
		Modes:                protocolModesFromVTerm(vt.Modes()),
		Timestamp:            time.Now(),
	}
	if protocol.HasExplicitRowOwnership(screenOwnership, len(outRows)) {
		snapshot.ScreenOwnership = append([]string(nil), screenOwnership...)
	}
	return snapshot
}

func snapshotUsesAlternateScreen(snapshot *protocol.Snapshot) bool {
	return snapshot != nil && (snapshot.Modes.AlternateScreen || snapshot.Screen.IsAlternateScreen)
}

func (r *Runtime) AlternateScrollbackSnapshot(terminalID string, snapshot *protocol.Snapshot) *protocol.Snapshot {
	if snapshot == nil {
		return nil
	}
	if !snapshotUsesAlternateScreen(snapshot) {
		return cloneProtocolSnapshot(snapshot)
	}
	cloned := cloneProtocolSnapshot(snapshot)
	var scrollback []protocol.CompactRow
	if r == nil || r.registry == nil || terminalID == "" {
		clearSnapshotScrollback(cloned)
		return cloned
	}
	terminal := r.registry.Get(terminalID)
	if terminal != nil {
		scrollback = terminal.AlternateScrollback
	}
	cloned.Scrollback = protocol.CloneCompactRows(scrollback)
	cloned.ScrollbackTimestamps = nil
	cloned.ScrollbackRowKinds = nil
	cloned.ScrollbackWrapped = nil
	cloned.ScrollbackOwnership = nil
	cloned.ScrollbackOffset = 0
	cloned.ScrollbackTotal = len(cloned.Scrollback)
	cloned.ScrollbackHasMore = len(scrollback) >= alternateScrollbackLimit
	return cloned
}

func clearSnapshotScrollback(snapshot *protocol.Snapshot) {
	if snapshot == nil {
		return
	}
	snapshot.Scrollback = nil
	snapshot.ScrollbackTimestamps = nil
	snapshot.ScrollbackRowKinds = nil
	snapshot.ScrollbackWrapped = nil
	snapshot.ScrollbackOwnership = nil
	snapshot.ScrollbackOffset = 0
	snapshot.ScrollbackTotal = 0
	snapshot.ScrollbackHasMore = false
}

func (r *Runtime) captureAlternateScrollback(terminal *TerminalRuntime, update protocol.ScreenUpdate) {
	if terminal == nil {
		return
	}
	previous := terminal.Snapshot
	if previous == nil || !snapshotUsesAlternateScreen(previous) {
		if update.Modes.AlternateScreen {
			terminal.AlternateScrollback = nil
		}
		return
	}
	if !update.Modes.AlternateScreen {
		terminal.AlternateScrollback = nil
		return
	}
	if update.FullReplace {
		terminal.AlternateScrollback = nil
		return
	}
	rows := alternateRowsScrolledOut(previous, update)
	if len(rows) == 0 {
		return
	}
	terminal.AlternateScrollback = append(terminal.AlternateScrollback, rows...)
	if overflow := len(terminal.AlternateScrollback) - alternateScrollbackLimit; overflow > 0 {
		terminal.AlternateScrollback = protocol.CloneCompactRows(terminal.AlternateScrollback[overflow:])
	}
}

func alternateRowsScrolledOut(previous *protocol.Snapshot, update protocol.ScreenUpdate) []protocol.CompactRow {
	if previous == nil {
		return nil
	}
	rows := make([]protocol.CompactRow, 0)
	for _, op := range update.Ops {
		if op.Code != protocol.ScreenOpScrollRect || op.Dx != 0 || op.Dy >= 0 {
			continue
		}
		if op.Rect.X != 0 || int(update.Size.Cols) > 0 && op.Rect.Width < int(update.Size.Cols) {
			continue
		}
		rows = append(rows, snapshotScreenRows(previous, op.Rect.Y, -op.Dy)...)
	}
	if len(rows) > 0 {
		return rows
	}
	if update.ScreenScroll > 0 {
		return snapshotScreenRows(previous, 0, update.ScreenScroll)
	}
	return nil
}

func snapshotScreenRows(snapshot *protocol.Snapshot, start int, count int) []protocol.CompactRow {
	if snapshot == nil || count <= 0 {
		return nil
	}
	rows := make([]protocol.CompactRow, 0, count)
	for row := start; row < start+count; row++ {
		if row < 0 || row >= len(snapshot.Screen.Cells) {
			continue
		}
		rows = append(rows, protocol.CompactRowFromCellsPreserveTrailingBlankCells(snapshot.Screen.Cells[row], true))
	}
	return rows
}

func snapshotFromRowSource(terminalID string, source rowSnapshotSource) *protocol.Snapshot {
	if source == nil {
		return nil
	}
	cols, rows := source.Size()
	screenRows := source.ScreenRowCount()
	scrollbackRows := source.ScrollbackRowCount()
	screen := make([][]protocol.Cell, screenRows)
	screenTimestamps := make([]time.Time, screenRows)
	screenRowKinds := make([]string, screenRows)
	screenWrapped := make([]bool, screenRows)
	screenOwnership := make([]string, screenRows)
	for row := 0; row < screenRows; row++ {
		screen[row] = protocolCellsFromVTermRow(source.ScreenRowView(row))
		screenTimestamps[row] = source.ScreenRowTimestampAt(row)
		screenRowKinds[row] = source.ScreenRowKindAt(row)
		screenWrapped[row] = source.ScreenRowWrappedAt(row)
		screenOwnership[row] = source.ScreenRowOwnershipAt(row)
	}
	scrollback := make([]protocol.CompactRow, scrollbackRows)
	scrollbackTimestamps := make([]time.Time, scrollbackRows)
	scrollbackRowKinds := make([]string, scrollbackRows)
	scrollbackWrapped := make([]bool, scrollbackRows)
	scrollbackOwnership := make([]string, scrollbackRows)
	for row := 0; row < scrollbackRows; row++ {
		scrollback[row] = protocol.CompactRowFromCellsPreserveTrailingBlankCells(protocolCellsFromVTermRow(source.ScrollbackRowView(row)), true)
		scrollbackTimestamps[row] = source.ScrollbackRowTimestampAt(row)
		scrollbackRowKinds[row] = source.ScrollbackRowKindAt(row)
		scrollbackWrapped[row] = source.ScrollbackRowWrappedAt(row)
		scrollbackOwnership[row] = source.ScrollbackRowOwnershipAt(row)
	}
	snapshot := &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: uint16(cols), Rows: uint16(rows)},
		Screen: protocol.ScreenData{
			Cells:             screen,
			IsAlternateScreen: source.IsAltScreen(),
		},
		Scrollback:           scrollback,
		ScreenTimestamps:     screenTimestamps,
		ScrollbackTimestamps: scrollbackTimestamps,
		ScreenRowKinds:       screenRowKinds,
		ScrollbackRowKinds:   scrollbackRowKinds,
		ScreenWrapped:        screenWrapped,
		ScrollbackWrapped:    scrollbackWrapped,
		ScreenOwnership:      repeatedOwnership(protocol.RowOwnershipScreen, len(screen)),
		ScrollbackOwnership:  scrollbackOwnership,
		Cursor:               protocolCursorFromVTerm(source.CursorState()),
		Modes:                protocolModesFromVTerm(source.Modes()),
		Timestamp:            time.Now(),
	}
	if protocol.HasExplicitRowOwnership(screenOwnership, len(screen)) {
		snapshot.ScreenOwnership = screenOwnership
	}
	return snapshot
}

func protocolCellFromVTermCell(cell localvterm.Cell) protocol.Cell {
	return protocol.Cell{
		Content: cell.Content,
		Width:   cell.Width,
		Style: protocol.CellStyle{
			FG:            cell.Style.FG,
			BG:            cell.Style.BG,
			Bold:          cell.Style.Bold,
			Italic:        cell.Style.Italic,
			Underline:     cell.Style.Underline,
			Blink:         cell.Style.Blink,
			Reverse:       cell.Style.Reverse,
			Strikethrough: cell.Style.Strikethrough,
		},
	}
}

func boolAtProtocol(values []bool, index int) bool {
	return index >= 0 && index < len(values) && values[index]
}

func applyScreenUpdateSnapshot(current *protocol.Snapshot, terminalID string, update protocol.ScreenUpdate) *protocol.Snapshot {
	update = protocol.NormalizeScreenUpdate(update)
	if update.FullReplace {
		snapshot := &protocol.Snapshot{TerminalID: terminalID}
		if snapshot.TerminalID == "" {
			snapshot.TerminalID = terminalID
		}
		if update.Size.Cols > 0 || update.Size.Rows > 0 {
			snapshot.Size = update.Size
		}
		if update.ResetScrollback {
			snapshot.Scrollback = nil
			snapshot.ScrollbackTimestamps = nil
			snapshot.ScrollbackRowKinds = nil
			snapshot.ScrollbackWrapped = nil
			snapshot.ScrollbackOwnership = nil
		}
		snapshot.Screen = cloneProtocolScreenData(update.Screen)
		snapshot.ScreenTimestamps = append([]time.Time(nil), update.ScreenTimestamps...)
		snapshot.ScreenRowKinds = append([]string(nil), update.ScreenRowKinds...)
		snapshot.ScreenWrapped = append([]bool(nil), update.ScreenWrapped...)
		snapshot.ScreenOwnership = repeatedOwnership(protocol.RowOwnershipScreen, len(snapshot.Screen.Cells))
		for _, row := range update.ScrollbackAppend {
			snapshot.Scrollback = append(snapshot.Scrollback, compactScreenUpdateScrollbackAppendRow(row))
			snapshot.ScrollbackTimestamps = append(snapshot.ScrollbackTimestamps, row.Timestamp)
			snapshot.ScrollbackRowKinds = append(snapshot.ScrollbackRowKinds, row.RowKind)
			snapshot.ScrollbackWrapped = append(snapshot.ScrollbackWrapped, row.WrappedSet && row.Wrapped)
			snapshot.ScrollbackOwnership = append(snapshot.ScrollbackOwnership, protocol.RowOwnershipLiveTailLive)
		}
		snapshot.Screen.IsAlternateScreen = update.Modes.AlternateScreen
		snapshot.Cursor = update.Cursor
		snapshot.Modes = update.Modes
		snapshot.Timestamp = time.Now()
		return snapshot
	}

	snapshot := &protocol.Snapshot{TerminalID: terminalID}
	if current != nil {
		cloned := *current
		snapshot = &cloned
	}
	if snapshot.TerminalID == "" {
		snapshot.TerminalID = terminalID
	}
	if update.Size.Cols > 0 || update.Size.Rows > 0 {
		snapshot.Size = update.Size
	}
	screenCellsOwned := false
	screenTimestampsOwned := false
	screenRowKindsOwned := false
	screenWrappedOwned := false
	scrollbackOwned := false
	scrollbackTimestampsOwned := false
	scrollbackRowKindsOwned := false
	scrollbackWrappedOwned := false
	scrollbackOwnershipOwned := false
	if update.ResetScrollback {
		snapshot.Scrollback = nil
		snapshot.ScrollbackTimestamps = nil
		snapshot.ScrollbackRowKinds = nil
		snapshot.ScrollbackWrapped = nil
		snapshot.ScrollbackOwnership = nil
		scrollbackOwned = true
		scrollbackTimestampsOwned = true
		scrollbackRowKindsOwned = true
		scrollbackWrappedOwned = true
		scrollbackOwnershipOwned = true
	}
	requiredRows := int(maxUint16(snapshot.Size.Rows, uint16(maxChangedScreenRow(update)+1)))
	if requiredRows > len(snapshot.Screen.Cells) {
		ensureSnapshotScreenRowsCOW(snapshot, requiredRows, &screenCellsOwned, &screenTimestampsOwned, &screenRowKindsOwned, &screenWrappedOwned)
	}
	if len(update.Ops) == 0 && update.ScreenScroll != 0 {
		shiftSnapshotScreenRows(snapshot, update.ScreenScroll, &screenCellsOwned, &screenTimestampsOwned, &screenRowKindsOwned, &screenWrappedOwned)
	}
	if update.ScrollbackTrim > 0 {
		trimSnapshotScrollbackFront(snapshot, update.ScrollbackTrim)
		scrollbackOwned = true
		scrollbackTimestampsOwned = true
		scrollbackRowKindsOwned = true
		scrollbackWrappedOwned = true
		scrollbackOwnershipOwned = true
	}
	screenRowCellsOwned := make(map[int]bool)
	if len(update.Ops) > 0 {
		applySnapshotScreenOps(snapshot, update, &screenCellsOwned, &screenTimestampsOwned, &screenRowKindsOwned, &screenWrappedOwned, screenRowCellsOwned)
	}
	if appendCount := len(update.ScrollbackAppend); appendCount > 0 {
		baseRows := len(snapshot.Scrollback)
		snapshot.Scrollback = cowProtocolCompactRows(snapshot.Scrollback, baseRows+appendCount, &scrollbackOwned)
		snapshot.ScrollbackTimestamps = cowTimeSlice(snapshot.ScrollbackTimestamps, baseRows+appendCount, &scrollbackTimestampsOwned)
		snapshot.ScrollbackRowKinds = cowStringSlice(snapshot.ScrollbackRowKinds, baseRows+appendCount, &scrollbackRowKindsOwned)
		snapshot.ScrollbackWrapped = cowBoolSlice(snapshot.ScrollbackWrapped, baseRows+appendCount, &scrollbackWrappedOwned)
		snapshot.ScrollbackOwnership = cowStringSlice(snapshot.ScrollbackOwnership, baseRows+appendCount, &scrollbackOwnershipOwned)
		for i, row := range update.ScrollbackAppend {
			index := baseRows + i
			snapshot.Scrollback[index] = compactScreenUpdateScrollbackAppendRow(row)
			snapshot.ScrollbackTimestamps[index] = row.Timestamp
			snapshot.ScrollbackRowKinds[index] = row.RowKind
			snapshot.ScrollbackWrapped[index] = row.WrappedSet && row.Wrapped
			snapshot.ScrollbackOwnership[index] = protocol.RowOwnershipLiveTailLive
		}
	}
	snapshot.Screen.IsAlternateScreen = update.Modes.AlternateScreen
	snapshot.ScreenOwnership = repeatedOwnership(protocol.RowOwnershipScreen, len(snapshot.Screen.Cells))
	snapshot.Cursor = update.Cursor
	snapshot.Modes = update.Modes
	snapshot.Timestamp = time.Now()
	return snapshot
}

func compactScreenUpdateScrollbackAppendRow(row protocol.ScrollbackRowAppend) protocol.CompactRow {
	return protocol.CompactRowFromCellsPreserveTrailingBlankCells(row.Cells, row.WrappedSet && row.Wrapped)
}

func cloneProtocolSnapshot(snapshot *protocol.Snapshot) *protocol.Snapshot {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	cloned.Screen = cloneProtocolScreenData(snapshot.Screen)
	cloned.Scrollback = protocol.CloneCompactRows(snapshot.Scrollback)
	cloned.ScreenTimestamps = append([]time.Time(nil), snapshot.ScreenTimestamps...)
	cloned.ScrollbackTimestamps = append([]time.Time(nil), snapshot.ScrollbackTimestamps...)
	cloned.ScreenRowKinds = append([]string(nil), snapshot.ScreenRowKinds...)
	cloned.ScrollbackRowKinds = append([]string(nil), snapshot.ScrollbackRowKinds...)
	cloned.ScreenWrapped = append([]bool(nil), snapshot.ScreenWrapped...)
	cloned.ScrollbackWrapped = append([]bool(nil), snapshot.ScrollbackWrapped...)
	cloned.ScreenOwnership = append([]string(nil), snapshot.ScreenOwnership...)
	cloned.ScrollbackOwnership = append([]string(nil), snapshot.ScrollbackOwnership...)
	return &cloned
}

func cloneProtocolScreenData(screen protocol.ScreenData) protocol.ScreenData {
	return protocol.ScreenData{
		Cells:             cloneProtocolRows(screen.Cells),
		IsAlternateScreen: screen.IsAlternateScreen,
	}
}

func cloneProtocolRows(rows [][]protocol.Cell) [][]protocol.Cell {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]protocol.Cell, len(rows))
	for i, row := range rows {
		out[i] = cloneProtocolCellRow(row)
	}
	return out
}

func cloneProtocolCellRow(row []protocol.Cell) []protocol.Cell {
	if len(row) == 0 {
		return nil
	}
	return append([]protocol.Cell(nil), row...)
}

func ensureSnapshotScreenRowCellsCOW(snapshot *protocol.Snapshot, row int, screenCellsOwned *bool, ownedRows map[int]bool) {
	if snapshot == nil || row < 0 {
		return
	}
	snapshot.Screen.Cells = cowProtocolRows(snapshot.Screen.Cells, row+1, screenCellsOwned)
	if ownedRows != nil {
		if ownedRows[row] {
			return
		}
		ownedRows[row] = true
	}
	snapshot.Screen.Cells[row] = cloneProtocolCellRow(snapshot.Screen.Cells[row])
}

func applyProtocolCellSpan(row []protocol.Cell, colStart int, cells []protocol.Cell) []protocol.Cell {
	if colStart < 0 {
		colStart = 0
	}
	if len(cells) == 0 {
		return trimProtocolCellRow(row)
	}
	row = padProtocolCellRow(row, colStart+len(cells))
	copy(row[colStart:], cells)
	return trimProtocolCellRow(row)
}

func clearProtocolCellRowFrom(row []protocol.Cell, colStart int) []protocol.Cell {
	if colStart <= 0 {
		return nil
	}
	if colStart >= len(row) {
		return trimProtocolCellRow(row)
	}
	return trimProtocolCellRow(cloneProtocolCellRow(row[:colStart]))
}

func applySnapshotScreenOps(snapshot *protocol.Snapshot, update protocol.ScreenUpdate, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned, screenWrappedOwned *bool, screenRowCellsOwned map[int]bool) {
	if snapshot == nil {
		return
	}
	for _, op := range update.Ops {
		switch op.Code {
		case protocol.ScreenOpWriteSpan:
			if op.Row < 0 {
				continue
			}
			ensureSnapshotScreenRowsCOW(snapshot, op.Row+1, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned, screenWrappedOwned)
			ensureSnapshotScreenRowCellsCOW(snapshot, op.Row, screenCellsOwned, screenRowCellsOwned)
			snapshot.Screen.Cells[op.Row] = applyProtocolCellSpan(snapshot.Screen.Cells[op.Row], op.Col, op.Cells)
			snapshot.ScreenTimestamps[op.Row] = op.Timestamp
			snapshot.ScreenRowKinds[op.Row] = op.RowKind
			if op.WrappedSet {
				snapshot.ScreenWrapped[op.Row] = op.Wrapped
			}
		case protocol.ScreenOpClearToEOL:
			if op.Row < 0 {
				continue
			}
			ensureSnapshotScreenRowsCOW(snapshot, op.Row+1, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned, screenWrappedOwned)
			ensureSnapshotScreenRowCellsCOW(snapshot, op.Row, screenCellsOwned, screenRowCellsOwned)
			snapshot.Screen.Cells[op.Row] = clearProtocolCellRowFrom(snapshot.Screen.Cells[op.Row], op.Col)
			snapshot.ScreenTimestamps[op.Row] = op.Timestamp
			snapshot.ScreenRowKinds[op.Row] = op.RowKind
			if op.WrappedSet {
				snapshot.ScreenWrapped[op.Row] = op.Wrapped
			}
		case protocol.ScreenOpClearRect:
			applySnapshotClearRect(snapshot, op, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned, screenWrappedOwned, screenRowCellsOwned)
		case protocol.ScreenOpScrollRect:
			applySnapshotScrollRect(snapshot, op, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned, screenWrappedOwned, screenRowCellsOwned)
		case protocol.ScreenOpCopyRect:
			applySnapshotCopyRect(snapshot, op, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned, screenWrappedOwned, screenRowCellsOwned)
		case protocol.ScreenOpResize:
			rows := int(op.Size.Rows)
			if rows > 0 {
				ensureSnapshotScreenRowsCOW(snapshot, rows, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned, screenWrappedOwned)
			}
			if op.Size.Cols > 0 {
				snapshot.Size.Cols = op.Size.Cols
			}
			if op.Size.Rows > 0 {
				snapshot.Size.Rows = op.Size.Rows
			}
		}
	}
}

func applySnapshotClearRect(snapshot *protocol.Snapshot, op protocol.ScreenOp, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned, screenWrappedOwned *bool, screenRowCellsOwned map[int]bool) {
	rect := op.Rect
	if snapshot == nil || rect.Width <= 0 || rect.Height <= 0 || rect.Y < 0 {
		return
	}
	ensureSnapshotScreenRowsCOW(snapshot, rect.Y+rect.Height, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned, screenWrappedOwned)
	cols := snapshotScreenWidth(snapshot, rect.X+rect.Width)
	for row := rect.Y; row < rect.Y+rect.Height; row++ {
		ensureSnapshotScreenRowCellsCOW(snapshot, row, screenCellsOwned, screenRowCellsOwned)
		dense := padProtocolCellRow(snapshot.Screen.Cells[row], cols)
		for col := maxInt(rect.X, 0); col < minInt(rect.X+rect.Width, cols); col++ {
			dense[col] = protocolBlankCell()
		}
		snapshot.Screen.Cells[row] = trimProtocolCellRow(dense)
		snapshot.ScreenTimestamps[row] = op.Timestamp
		snapshot.ScreenRowKinds[row] = op.RowKind
		if op.WrappedSet {
			snapshot.ScreenWrapped[row] = op.Wrapped
		} else if rect.X <= 0 && rect.Width >= cols {
			snapshot.ScreenWrapped[row] = false
		}
	}
}

func applySnapshotScrollRect(snapshot *protocol.Snapshot, op protocol.ScreenOp, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned, screenWrappedOwned *bool, screenRowCellsOwned map[int]bool) {
	rect := op.Rect
	if snapshot == nil || rect.Width <= 0 || rect.Height <= 0 || rect.Y < 0 {
		return
	}
	ensureSnapshotScreenRowsCOW(snapshot, rect.Y+rect.Height, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned, screenWrappedOwned)
	cols := snapshotScreenWidth(snapshot, rect.X+rect.Width)
	fullWidth := op.Dx == 0 && rect.X == 0 && rect.Width >= cols
	if fullWidth {
		beforeRows := cloneProtocolRowsRect(snapshot.Screen.Cells, rect.Y, rect.Height)
		beforeTimes := append([]time.Time(nil), snapshot.ScreenTimestamps[rect.Y:rect.Y+rect.Height]...)
		beforeKinds := append([]string(nil), snapshot.ScreenRowKinds[rect.Y:rect.Y+rect.Height]...)
		beforeWrapped := append([]bool(nil), snapshot.ScreenWrapped[rect.Y:rect.Y+rect.Height]...)
		for row := rect.Y; row < rect.Y+rect.Height; row++ {
			srcY := row - op.Dy
			if srcY >= rect.Y && srcY < rect.Y+rect.Height {
				snapshot.Screen.Cells[row] = beforeRows[srcY-rect.Y]
				snapshot.ScreenTimestamps[row] = beforeTimes[srcY-rect.Y]
				snapshot.ScreenRowKinds[row] = beforeKinds[srcY-rect.Y]
				snapshot.ScreenWrapped[row] = boolAtProtocol(beforeWrapped, srcY-rect.Y)
				markSnapshotScreenRowOwned(screenRowCellsOwned, row)
				continue
			}
			snapshot.Screen.Cells[row] = nil
			snapshot.ScreenTimestamps[row] = time.Time{}
			snapshot.ScreenRowKinds[row] = ""
			snapshot.ScreenWrapped[row] = false
			markSnapshotScreenRowOwned(screenRowCellsOwned, row)
		}
		return
	}
	beforeRows := cloneAndPadProtocolRowsRect(snapshot.Screen.Cells, rect.Y, rect.Height, cols)
	beforeTimes := append([]time.Time(nil), snapshot.ScreenTimestamps[rect.Y:rect.Y+rect.Height]...)
	beforeKinds := append([]string(nil), snapshot.ScreenRowKinds[rect.Y:rect.Y+rect.Height]...)
	beforeWrapped := append([]bool(nil), snapshot.ScreenWrapped[rect.Y:rect.Y+rect.Height]...)
	for row := rect.Y; row < rect.Y+rect.Height; row++ {
		ensureSnapshotScreenRowCellsCOW(snapshot, row, screenCellsOwned, screenRowCellsOwned)
		dense := padProtocolCellRow(snapshot.Screen.Cells[row], cols)
		for col := maxInt(rect.X, 0); col < minInt(rect.X+rect.Width, cols); col++ {
			srcX := col - op.Dx
			srcY := row - op.Dy
			if srcX >= rect.X && srcX < rect.X+rect.Width && srcY >= rect.Y && srcY < rect.Y+rect.Height {
				dense[col] = beforeRows[srcY-rect.Y][srcX]
				continue
			}
			dense[col] = protocolBlankCell()
		}
		snapshot.Screen.Cells[row] = trimProtocolCellRow(dense)
	}
	for row := rect.Y; row < rect.Y+rect.Height; row++ {
		srcY := row - op.Dy
		if srcY >= rect.Y && srcY < rect.Y+rect.Height {
			snapshot.ScreenTimestamps[row] = beforeTimes[srcY-rect.Y]
			snapshot.ScreenRowKinds[row] = beforeKinds[srcY-rect.Y]
			if op.Dx == 0 && rect.X == 0 && rect.Width >= cols {
				snapshot.ScreenWrapped[row] = boolAtProtocol(beforeWrapped, srcY-rect.Y)
			}
			continue
		}
		snapshot.ScreenTimestamps[row] = time.Time{}
		snapshot.ScreenRowKinds[row] = ""
		if op.Dx == 0 && rect.X == 0 && rect.Width >= cols {
			snapshot.ScreenWrapped[row] = false
		}
	}
}

func applySnapshotCopyRect(snapshot *protocol.Snapshot, op protocol.ScreenOp, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned, screenWrappedOwned *bool, screenRowCellsOwned map[int]bool) {
	src := op.Src
	if snapshot == nil || src.Width <= 0 || src.Height <= 0 || src.Y < 0 || op.DstY < 0 {
		return
	}
	rowsNeeded := maxInt(src.Y+src.Height, op.DstY+src.Height)
	ensureSnapshotScreenRowsCOW(snapshot, rowsNeeded, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned, screenWrappedOwned)
	cols := snapshotScreenWidth(snapshot, maxInt(src.X+src.Width, op.DstX+src.Width))
	fullWidth := src.X == 0 && op.DstX == 0 && src.Width >= cols
	if fullWidth {
		beforeRows := cloneProtocolRowsRect(snapshot.Screen.Cells, src.Y, src.Height)
		beforeTimes := append([]time.Time(nil), snapshot.ScreenTimestamps[src.Y:src.Y+src.Height]...)
		beforeKinds := append([]string(nil), snapshot.ScreenRowKinds[src.Y:src.Y+src.Height]...)
		beforeWrapped := append([]bool(nil), snapshot.ScreenWrapped[src.Y:src.Y+src.Height]...)
		for row := 0; row < src.Height; row++ {
			dstRow := op.DstY + row
			if dstRow < 0 || dstRow >= len(snapshot.Screen.Cells) {
				continue
			}
			snapshot.Screen.Cells[dstRow] = beforeRows[row]
			snapshot.ScreenTimestamps[dstRow] = beforeTimes[row]
			snapshot.ScreenRowKinds[dstRow] = beforeKinds[row]
			snapshot.ScreenWrapped[dstRow] = boolAtProtocol(beforeWrapped, row)
			markSnapshotScreenRowOwned(screenRowCellsOwned, dstRow)
		}
		return
	}
	beforeRows := cloneAndPadProtocolRowsRect(snapshot.Screen.Cells, src.Y, src.Height, cols)
	for row := 0; row < src.Height; row++ {
		dstRow := op.DstY + row
		if dstRow < 0 || dstRow >= len(snapshot.Screen.Cells) {
			continue
		}
		ensureSnapshotScreenRowCellsCOW(snapshot, dstRow, screenCellsOwned, screenRowCellsOwned)
		dense := padProtocolCellRow(snapshot.Screen.Cells[dstRow], cols)
		for col := 0; col < src.Width; col++ {
			dstCol := op.DstX + col
			srcCol := src.X + col
			if dstCol < 0 || dstCol >= cols || srcCol < 0 || srcCol >= cols || row >= len(beforeRows) {
				continue
			}
			dense[dstCol] = beforeRows[row][srcCol]
		}
		snapshot.Screen.Cells[dstRow] = trimProtocolCellRow(dense)
	}
}

func padProtocolCellRow(row []protocol.Cell, cols int) []protocol.Cell {
	if cols <= len(row) {
		return row
	}
	if row == nil {
		row = make([]protocol.Cell, 0, cols)
	}
	for len(row) < cols {
		row = append(row, protocolBlankCell())
	}
	return row
}

func trimProtocolCellRow(row []protocol.Cell) []protocol.Cell {
	if len(row) == 0 {
		return nil
	}
	last := -1
	for i, cell := range row {
		if protocolCellNeedsSnapshotRow(cell) {
			last = i
			if cell.Width > 1 {
				last = maxInt(last, minInt(len(row)-1, i+cell.Width-1))
			}
		}
	}
	if last < 0 {
		return nil
	}
	return cloneProtocolCellRow(row[:last+1])
}

func protocolCellNeedsSnapshotRow(cell protocol.Cell) bool {
	if cell.Style != (protocol.CellStyle{}) {
		return true
	}
	if cell.Width > 1 {
		return true
	}
	if cell.Content == "" {
		return false
	}
	return strings.TrimSpace(cell.Content) != ""
}

func protocolBlankCell() protocol.Cell {
	return protocol.Cell{Content: " ", Width: 1}
}

func cloneProtocolRowsRect(rows [][]protocol.Cell, start, height int) [][]protocol.Cell {
	if height <= 0 {
		return nil
	}
	out := make([][]protocol.Cell, height)
	for i := 0; i < height; i++ {
		row := start + i
		if row < 0 || row >= len(rows) {
			continue
		}
		out[i] = cloneProtocolCellRow(rows[row])
	}
	return out
}

func cloneAndPadProtocolRowsRect(rows [][]protocol.Cell, start, height, cols int) [][]protocol.Cell {
	if height <= 0 {
		return nil
	}
	out := make([][]protocol.Cell, height)
	for i := 0; i < height; i++ {
		row := start + i
		if row < 0 || row >= len(rows) {
			out[i] = make([]protocol.Cell, cols)
			for j := range out[i] {
				out[i][j] = protocolBlankCell()
			}
			continue
		}
		out[i] = padProtocolCellRow(cloneProtocolCellRow(rows[row]), cols)
	}
	return out
}

func markSnapshotScreenRowOwned(ownedRows map[int]bool, row int) {
	if ownedRows == nil {
		return
	}
	ownedRows[row] = true
}

func snapshotScreenWidth(snapshot *protocol.Snapshot, minWidth int) int {
	width := minWidth
	if snapshot != nil && int(snapshot.Size.Cols) > width {
		width = int(snapshot.Size.Cols)
	}
	if snapshot != nil {
		for _, row := range snapshot.Screen.Cells {
			if len(row) > width {
				width = len(row)
			}
		}
	}
	if width < 1 {
		return 1
	}
	return width
}

func cowProtocolRows(rows [][]protocol.Cell, size int, owned *bool) [][]protocol.Cell {
	size = maxInt(size, len(rows))
	if size <= 0 {
		return nil
	}
	if owned != nil && *owned {
		if len(rows) >= size {
			return rows
		}
		return append(rows, make([][]protocol.Cell, size-len(rows))...)
	}
	out := make([][]protocol.Cell, size)
	copy(out, rows)
	if owned != nil {
		*owned = true
	}
	return out
}

func cowProtocolCompactRows(rows []protocol.CompactRow, size int, owned *bool) []protocol.CompactRow {
	size = maxInt(size, len(rows))
	if size <= 0 {
		return nil
	}
	if owned != nil && *owned {
		if len(rows) >= size {
			return rows
		}
		return append(rows, make([]protocol.CompactRow, size-len(rows))...)
	}
	out := make([]protocol.CompactRow, size)
	copy(out, rows)
	if owned != nil {
		*owned = true
	}
	return out
}

func cowTimeSlice(values []time.Time, size int, owned *bool) []time.Time {
	size = maxInt(size, len(values))
	if size <= 0 {
		return nil
	}
	if owned != nil && *owned {
		if len(values) >= size {
			return values
		}
		return append(values, make([]time.Time, size-len(values))...)
	}
	out := make([]time.Time, size)
	copy(out, values)
	if owned != nil {
		*owned = true
	}
	return out
}

func cowStringSlice(values []string, size int, owned *bool) []string {
	size = maxInt(size, len(values))
	if size <= 0 {
		return nil
	}
	if owned != nil && *owned {
		if len(values) >= size {
			return values
		}
		return append(values, make([]string, size-len(values))...)
	}
	out := make([]string, size)
	copy(out, values)
	if owned != nil {
		*owned = true
	}
	return out
}

func cowBoolSlice(values []bool, size int, owned *bool) []bool {
	size = maxInt(size, len(values))
	if size <= 0 {
		return nil
	}
	if owned != nil && *owned {
		if len(values) >= size {
			return values
		}
		return append(values, make([]bool, size-len(values))...)
	}
	out := make([]bool, size)
	copy(out, values)
	if owned != nil {
		*owned = true
	}
	return out
}

func ensureSnapshotScreenRowsCOW(snapshot *protocol.Snapshot, rows int, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned, screenWrappedOwned *bool) {
	if snapshot == nil || rows <= 0 {
		return
	}
	snapshot.Screen.Cells = cowProtocolRows(snapshot.Screen.Cells, rows, screenCellsOwned)
	snapshot.ScreenTimestamps = cowTimeSlice(snapshot.ScreenTimestamps, rows, screenTimestampsOwned)
	snapshot.ScreenRowKinds = cowStringSlice(snapshot.ScreenRowKinds, rows, screenRowKindsOwned)
	snapshot.ScreenWrapped = cowBoolSlice(snapshot.ScreenWrapped, rows, screenWrappedOwned)
	if snapshot.Size.Rows < uint16(rows) {
		snapshot.Size.Rows = uint16(rows)
	}
}

func shiftSnapshotScreenRows(snapshot *protocol.Snapshot, delta int, screenCellsOwned, screenTimestampsOwned, screenRowKindsOwned, screenWrappedOwned *bool) {
	if snapshot == nil || delta == 0 {
		return
	}
	rows := len(snapshot.Screen.Cells)
	if rows == 0 {
		return
	}
	if delta >= rows || delta <= -rows {
		snapshot.Screen.Cells = cowProtocolRows(snapshot.Screen.Cells, rows, screenCellsOwned)
		snapshot.ScreenTimestamps = cowTimeSlice(snapshot.ScreenTimestamps, rows, screenTimestampsOwned)
		snapshot.ScreenRowKinds = cowStringSlice(snapshot.ScreenRowKinds, rows, screenRowKindsOwned)
		snapshot.ScreenWrapped = cowBoolSlice(snapshot.ScreenWrapped, rows, screenWrappedOwned)
		clear(snapshot.Screen.Cells)
		clear(snapshot.ScreenTimestamps)
		clear(snapshot.ScreenRowKinds)
		clear(snapshot.ScreenWrapped)
		return
	}
	snapshot.Screen.Cells = cowProtocolRows(snapshot.Screen.Cells, rows, screenCellsOwned)
	snapshot.ScreenTimestamps = cowTimeSlice(snapshot.ScreenTimestamps, rows, screenTimestampsOwned)
	snapshot.ScreenRowKinds = cowStringSlice(snapshot.ScreenRowKinds, rows, screenRowKindsOwned)
	snapshot.ScreenWrapped = cowBoolSlice(snapshot.ScreenWrapped, rows, screenWrappedOwned)
	if delta > 0 {
		for row := 0; row < rows-delta; row++ {
			snapshot.Screen.Cells[row] = snapshot.Screen.Cells[row+delta]
			snapshot.ScreenTimestamps[row] = snapshot.ScreenTimestamps[row+delta]
			snapshot.ScreenRowKinds[row] = snapshot.ScreenRowKinds[row+delta]
			snapshot.ScreenWrapped[row] = snapshot.ScreenWrapped[row+delta]
		}
		for row := rows - delta; row < rows; row++ {
			snapshot.Screen.Cells[row] = nil
			snapshot.ScreenTimestamps[row] = time.Time{}
			snapshot.ScreenRowKinds[row] = ""
			snapshot.ScreenWrapped[row] = false
		}
		return
	}
	shift := -delta
	for row := rows - 1; row >= shift; row-- {
		snapshot.Screen.Cells[row] = snapshot.Screen.Cells[row-shift]
		snapshot.ScreenTimestamps[row] = snapshot.ScreenTimestamps[row-shift]
		snapshot.ScreenRowKinds[row] = snapshot.ScreenRowKinds[row-shift]
		snapshot.ScreenWrapped[row] = snapshot.ScreenWrapped[row-shift]
	}
	for row := 0; row < shift; row++ {
		snapshot.Screen.Cells[row] = nil
		snapshot.ScreenTimestamps[row] = time.Time{}
		snapshot.ScreenRowKinds[row] = ""
		snapshot.ScreenWrapped[row] = false
	}
}

func trimSnapshotScrollbackFront(snapshot *protocol.Snapshot, trim int) {
	if snapshot == nil || trim <= 0 {
		return
	}
	if trim >= len(snapshot.Scrollback) {
		snapshot.Scrollback = nil
		snapshot.ScrollbackTimestamps = nil
		snapshot.ScrollbackRowKinds = nil
		snapshot.ScrollbackWrapped = nil
		snapshot.ScrollbackOwnership = nil
		return
	}
	snapshot.Scrollback = cloneProtocolCompactRowsWindow(snapshot.Scrollback, trim)
	snapshot.ScrollbackTimestamps = append([]time.Time(nil), snapshot.ScrollbackTimestamps[minInt(trim, len(snapshot.ScrollbackTimestamps)):]...)
	snapshot.ScrollbackRowKinds = append([]string(nil), snapshot.ScrollbackRowKinds[minInt(trim, len(snapshot.ScrollbackRowKinds)):]...)
	snapshot.ScrollbackWrapped = append([]bool(nil), snapshot.ScrollbackWrapped[minInt(trim, len(snapshot.ScrollbackWrapped)):]...)
	snapshot.ScrollbackOwnership = append([]string(nil), snapshot.ScrollbackOwnership[minInt(trim, len(snapshot.ScrollbackOwnership)):]...)
}


func cloneProtocolCompactRowsWindow(rows []protocol.CompactRow, start int) []protocol.CompactRow {
	start = minInt(maxInt(start, 0), len(rows))
	if start >= len(rows) {
		return nil
	}
	out := make([]protocol.CompactRow, len(rows)-start)
	copy(out, rows[start:])
	return out
}

func maxChangedScreenRow(update protocol.ScreenUpdate) int {
	maxRow := -1
	for _, op := range update.Ops {
		switch op.Code {
		case protocol.ScreenOpWriteSpan, protocol.ScreenOpClearToEOL:
			if op.Row > maxRow {
				maxRow = op.Row
			}
		case protocol.ScreenOpScrollRect, protocol.ScreenOpClearRect:
			if row := op.Rect.Y + op.Rect.Height - 1; row > maxRow {
				maxRow = row
			}
		case protocol.ScreenOpCopyRect:
			if row := op.DstY + op.Src.Height - 1; row > maxRow {
				maxRow = row
			}
		case protocol.ScreenOpResize:
			if row := int(op.Size.Rows) - 1; row > maxRow {
				maxRow = row
			}
		}
	}
	if update.FullReplace && len(update.Screen.Cells) > 0 {
		maxRow = len(update.Screen.Cells) - 1
	}
	return maxRow
}

func maxUint16(a, b uint16) uint16 {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func protocolRowsToVTerm(rows [][]protocol.Cell) [][]localvterm.Cell {
	out := make([][]localvterm.Cell, len(rows))
	for y, row := range rows {
		out[y] = protocolCellRowToVTerm(row)
	}
	return out
}

func protocolCompactRowsToVTerm(rows []protocol.CompactRow) [][]localvterm.Cell {
	out := make([][]localvterm.Cell, len(rows))
	for y, row := range rows {
		out[y] = protocolCellRowToVTerm(row.DecodeCells())
	}
	return out
}

func protocolCellRowToVTerm(row []protocol.Cell) []localvterm.Cell {
	if len(row) == 0 {
		return nil
	}
	out := make([]localvterm.Cell, len(row))
	for x, cell := range row {
		out[x] = localvterm.Cell{
			Content: cell.Content,
			Width:   cell.Width,
			Style: localvterm.CellStyle{
				FG:            cell.Style.FG,
				BG:            cell.Style.BG,
				Bold:          cell.Style.Bold,
				Italic:        cell.Style.Italic,
				Underline:     cell.Style.Underline,
				Blink:         cell.Style.Blink,
				Reverse:       cell.Style.Reverse,
				Strikethrough: cell.Style.Strikethrough,
			},
		}
	}
	return out
}

func protocolScreenToVTerm(screen protocol.ScreenData) localvterm.ScreenData {
	return localvterm.ScreenData{
		Cells:             protocolRowsToVTerm(screen.Cells),
		IsAlternateScreen: screen.IsAlternateScreen,
	}
}

func protocolCursorToVTerm(cursor protocol.CursorState) localvterm.CursorState {
	return localvterm.CursorState{
		Row:     cursor.Row,
		Col:     cursor.Col,
		Visible: cursor.Visible,
		Shape:   localvterm.CursorShape(cursor.Shape),
		Blink:   cursor.Blink,
	}
}

func protocolModesToVTerm(modes protocol.TerminalModes) localvterm.TerminalModes {
	return localvterm.TerminalModes{
		AlternateScreen:   modes.AlternateScreen,
		AlternateScroll:   modes.AlternateScroll,
		MouseTracking:     modes.MouseTracking,
		MouseX10:          modes.MouseX10,
		MouseNormal:       modes.MouseNormal,
		MouseButtonEvent:  modes.MouseButtonEvent,
		MouseAnyEvent:     modes.MouseAnyEvent,
		MouseSGR:          modes.MouseSGR,
		BracketedPaste:    modes.BracketedPaste,
		ApplicationCursor: modes.ApplicationCursor,
		AutoWrap:          modes.AutoWrap,
	}
}

func protocolCursorFromVTerm(cursor localvterm.CursorState) protocol.CursorState {
	return protocol.CursorState{
		Row:     cursor.Row,
		Col:     cursor.Col,
		Visible: cursor.Visible,
		Shape:   string(cursor.Shape),
		Blink:   cursor.Blink,
	}
}

func protocolModesFromVTerm(modes localvterm.TerminalModes) protocol.TerminalModes {
	return protocol.TerminalModes{
		AlternateScreen:   modes.AlternateScreen,
		AlternateScroll:   modes.AlternateScroll,
		MouseTracking:     modes.MouseTracking,
		MouseX10:          modes.MouseX10,
		MouseNormal:       modes.MouseNormal,
		MouseButtonEvent:  modes.MouseButtonEvent,
		MouseAnyEvent:     modes.MouseAnyEvent,
		MouseSGR:          modes.MouseSGR,
		BracketedPaste:    modes.BracketedPaste,
		ApplicationCursor: modes.ApplicationCursor,
		AutoWrap:          modes.AutoWrap,
	}
}
