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
