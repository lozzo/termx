package termx

import (
	"reflect"
	"testing"

	localvterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestTerminalResizeFullReplaceUsesExplicitResizeLiveTailRows(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	vt.LoadSnapshot(localvterm.ScreenData{Cells: [][]localvterm.Cell{localVTermCellsFromString("live")}}, localvterm.CursorState{Row: 0, Col: 4, Visible: true}, localvterm.TerminalModes{AutoWrap: true})
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "resize-explicit-live-tail",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}

	damage := localvterm.WriteDamage{
		ScrollbackAppend: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("hist"), WrappedSet: true, Wrapped: false},
			{Cells: localVTermCellsFromString("tail"), WrappedSet: true, Wrapped: true},
		},
		RequiresFullReplace: true,
		FullReplaceReason:   "resize",
	}
	damage.ResizeLiveTailRows = 1
	term.appendGridFromDamageLocked(damage)

	if got := store.RowCount(); got != 0 {
		t.Fatalf("expected resize full-replace not to create persisted history, got %d rows", got)
	}
	if got := vtermRowsToStrings(term.primaryLiveTailRowsToRowsForTest()); !reflect.DeepEqual(got, []string{"hist", "tail"}) {
		t.Fatalf("expected explicit resize rows to stay display-only live-tail projection, got %#v", got)
	}

	snapshot := term.Snapshot(0, 10)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	var combined []string
	for _, row := range snapshot.Scrollback {
		combined = append(combined, rowToString(row))
	}
	for _, row := range snapshot.Screen.Cells {
		combined = append(combined, rowToString(row))
	}
	if !reflect.DeepEqual(combined, []string{"hist", "tail", "live"}) {
		t.Fatalf("expected latest snapshot projection persisted+live-tail+screen, got %#v", combined)
	}

	olderViewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 1, ScrollbackLimit: 10, Cols: 4})
	if olderViewport == nil {
		t.Fatal("expected older viewport")
	}
	gotRows := make([]string, 0, len(olderViewport.Rows))
	for _, row := range olderViewport.Rows {
		gotRows = append(gotRows, rowToString(row))
	}
	if len(gotRows) != 0 {
		t.Fatalf("expected older viewport to exclude live-tail resize tail, got %#v", gotRows)
	}
}

func TestTerminalResizeFullReplacePrefersExplicitResizeLiveTailRowsOverWrappedFallback(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:   "resize-explicit-priority",
		grid: store,
	}
	term.primaryLiveTail.replaceRows([]localvterm.DamageOp{{Cells: localVTermCellsFromString("oldtail"), WrappedSet: true, Wrapped: true}}, terminalLiveTailOriginLive, true)

	damage := localvterm.WriteDamage{
		ScrollbackAppend: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("hist"), WrappedSet: true, Wrapped: false},
			{Cells: localVTermCellsFromString("tail"), WrappedSet: true, Wrapped: true},
		},
		RequiresFullReplace: true,
		FullReplaceReason:   "resize",
	}
	damage.ResizeLiveTailRows = 0
	term.appendGridFromDamageLocked(damage)

	if got := store.RowCount(); got != 0 {
		t.Fatalf("expected resize full-replace not to create persisted history, got %d rows", got)
	}
	if got := vtermRowsToStrings(term.primaryLiveTailRowsToRowsForTest()); !reflect.DeepEqual(got, []string{"oldtail", "hist", "tail"}) {
		t.Fatalf("expected explicit resize rows to append to existing open live-tail prefix, got %#v", got)
	}
}

func TestTerminalResizeFullReplacePreservesHiddenLiveTailAcrossRepeatedResize(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:   "resize-repeated-preserves-hidden-live-tail",
		grid: store,
	}
	term.primaryLiveTail.replaceLiveRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("h0"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("h1"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("h2"), WrappedSet: true, Wrapped: true},
	}, false)

	damage1 := localvterm.WriteDamage{
		ScrollbackAppend: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("n0"), WrappedSet: true, Wrapped: true},
			{Cells: localVTermCellsFromString("n1"), WrappedSet: true, Wrapped: true},
		},
		RequiresFullReplace: true,
		FullReplaceReason:   "resize",
		ResizeLiveTailRows:  0,
	}
	term.appendGridFromDamageLocked(damage1)

	damage2 := localvterm.WriteDamage{
		ScrollbackAppend: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("n2"), WrappedSet: true, Wrapped: true},
			{Cells: localVTermCellsFromString("n3"), WrappedSet: true, Wrapped: true},
		},
		RequiresFullReplace: true,
		FullReplaceReason:   "resize",
		ResizeLiveTailRows:  0,
	}
	term.appendGridFromDamageLocked(damage2)

	if got := vtermRowsToStrings(term.primaryLiveTailRowsToRowsForTest()); !reflect.DeepEqual(got, []string{"h0", "h1", "h2", "n0", "n1", "n2", "n3"}) {
		t.Fatalf("expected repeated resize full-replace to preserve previously hidden live-tail rows, got %#v", got)
	}
}

func TestTerminalResizeFullReplaceRecoverableHiddenLiveTailUsesResizeOrigin(t *testing.T) {
	root := t.TempDir()
	store, err := newTerminalGridStore(root, "resize-recoverable-hidden-live-tail")
	if err != nil {
		t.Fatalf("new grid store: %v", err)
	}
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	vt.LoadSnapshot(
		localvterm.ScreenData{Cells: [][]localvterm.Cell{localVTermCellsFromString("live")}},
		localvterm.CursorState{Row: 0, Col: 4, Visible: true},
		localvterm.TerminalModes{AutoWrap: true},
	)
	term := &Terminal{
		id:    "resize-recoverable-hidden-live-tail",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}

	damage := localvterm.WriteDamage{
		ScrollbackAppend: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("h0"), WrappedSet: true, Wrapped: true},
			{Cells: localVTermCellsFromString("h1"), WrappedSet: true, Wrapped: true},
		},
		RequiresFullReplace: true,
		FullReplaceReason:   "resize",
		ResizeLiveTailRows:  0,
	}
	term.appendGridFromDamageLocked(damage)
	dir := store.dir
	if err := store.Close(); err != nil {
		t.Fatalf("close grid store: %v", err)
	}

	metadata, err := readTerminalGridLineMetadata(dir)
	if err != nil {
		t.Fatalf("read live tail metadata: %v", err)
	}
	if len(metadata.LiveRecords) != 1 {
		t.Fatalf("expected one recoverable live-tail record, got %#v", metadata.LiveRecords)
	}
	runtimeID := metadata.LiveRecords[0].ID
	if metadata.LiveRecords[0].Origin != terminalLiveTailOriginResize || !metadata.LiveRecords[0].Dirty || metadata.LiveRecords[0].Generation != 0 || !terminalRuntimeLogicalLineID(runtimeID) {
		t.Fatalf("expected dirty resize runtime live-tail record, got %#v", metadata.LiveRecords[0])
	}

	reopened, err := openTerminalGridStoreForReplay(root, "resize-recoverable-hidden-live-tail")
	if err != nil {
		t.Fatalf("reopen grid store: %v", err)
	}
	defer reopened.Close()
	tail, ok := reopened.recoveredLiveTailFromMetadata()
	if !ok {
		t.Fatal("expected resize hidden live tail to recover from metadata")
	}
	if len(tail.segments) != 1 {
		t.Fatalf("expected one recovered resize segment, got %#v", tail.segments)
	}
	segment := tail.segments[0]
	if segment.origin != terminalLiveTailOriginResize || segment.sealState != terminalLiveTailOpen || !segment.wrapPending {
		t.Fatalf("expected recovered hidden segment to stay open resize tail, got %#v", segment)
	}
	if got := damageRowsToStrings(segment.rows); !reflect.DeepEqual(got, []string{"h0", "h1"}) {
		t.Fatalf("expected recovered resize rows, got %#v", got)
	}
	if got := segment.logicalLineIDs; !reflect.DeepEqual(got, []uint64{runtimeID, runtimeID}) {
		t.Fatalf("expected recovered resize rows to keep runtime logical line id, got %#v", got)
	}

	viewport, err := historyCombinedGridViewportFromStore(reopened, 0, 10, 4, tail)
	if err != nil {
		t.Fatalf("history viewport with recovered resize live tail: %v", err)
	}
	window := historyWindowFromCoreGridViewport("resize-recoverable-hidden-live-tail", 0, viewport)
	if got := historyWindowTrimmedRowTexts(window); !reflect.DeepEqual(got, []string{"h0", "h1"}) {
		t.Fatalf("expected recovered resize live tail in latest history projection, got %#v", got)
	}
	if window.LoadedRows != 0 || window.LoadedLines != 1 || window.LogicalTotal != 1 || window.Generation != 0 {
		t.Fatalf("expected recovered resize live tail to stay mutable without committed depth, loaded_rows=%d loaded_lines=%d total=%d gen=%d", window.LoadedRows, window.LoadedLines, window.LogicalTotal, window.Generation)
	}
	if len(window.Lines) != 1 || window.Lines[0].LogicalLineID != runtimeID || !window.Lines[0].ClippedAfter {
		t.Fatalf("expected recovered resize runtime logical line span, got %#v", window.Lines)
	}
}
