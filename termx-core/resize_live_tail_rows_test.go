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
	setResizeLiveTailRowsForTest(t, &damage, 1)
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
	setResizeLiveTailRowsForTest(t, &damage, 0)
	term.appendGridFromDamageLocked(damage)

	if got := store.RowCount(); got != 0 {
		t.Fatalf("expected resize full-replace not to create persisted history, got %d rows", got)
	}
	if got := vtermRowsToStrings(term.primaryLiveTailRowsToRowsForTest()); !reflect.DeepEqual(got, []string{"oldtail", "hist", "tail"}) {
		t.Fatalf("expected explicit resize rows to append to existing open live-tail prefix, got %#v", got)
	}
}

func TestTerminalResizeFullReplaceFallsBackWhenExplicitResizeLiveTailRowsMissing(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:   "resize-fallback-missing",
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
	clearResizeLiveTailRowsForTest(t, &damage)
	term.appendGridFromDamageLocked(damage)

	viewport, err := store.Viewport(0, 10, 4)
	if err != nil {
		t.Fatalf("read viewport: %v", err)
	}
	if len(viewport.Rows) != 0 {
		t.Fatalf("expected legacy resize fallback to avoid committing rows when explicit field is absent, got %#v", vtermRowsToStrings(viewport.Rows))
	}
	if got := vtermRowsToStrings(term.primaryLiveTailRowsToRowsForTest()); !reflect.DeepEqual(got, []string{"oldtail", "hist", "tail"}) {
		t.Fatalf("expected legacy resize fallback to preserve ordinary open live-tail prefix, got %#v", got)
	}
}

func TestTerminalResizeFullReplaceRepeatedFallbackReplacesLiveTail(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:   "resize-repeated-fallback-replaces",
		grid: store,
	}
	term.primaryLiveTail.replaceResizeRows([]localvterm.DamageOp{
		{Cells: localVTermCellsFromString("old0"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("old1"), WrappedSet: true, Wrapped: true},
	}, false)

	damage := localvterm.WriteDamage{
		ScrollbackAppend: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("new0"), WrappedSet: true, Wrapped: true},
			{Cells: localVTermCellsFromString("new1"), WrappedSet: true, Wrapped: true},
		},
		RequiresFullReplace: true,
		FullReplaceReason:   "resize",
	}
	clearResizeLiveTailRowsForTest(t, &damage)
	term.appendGridFromDamageLocked(damage)

	if got := vtermRowsToStrings(term.primaryLiveTailRowsToRowsForTest()); !reflect.DeepEqual(got, []string{"new0", "new1"}) {
		t.Fatalf("expected repeated resize fallback to replace stale live tail, got %#v", got)
	}
}

func setResizeLiveTailRowsForTest(t *testing.T, damage *localvterm.WriteDamage, value int) {
	t.Helper()
	elem := reflect.ValueOf(damage).Elem()
	field := elem.FieldByName("ResizeLiveTailRows")
	if !field.IsValid() {
		t.Fatal("expected WriteDamage.ResizeLiveTailRows field")
	}
	setField := elem.FieldByName("ResizeLiveTailRowsSet")
	if !setField.IsValid() {
		t.Fatal("expected WriteDamage.ResizeLiveTailRowsSet field")
	}
	field.SetInt(int64(value))
	setField.SetBool(true)
}

func clearResizeLiveTailRowsForTest(t *testing.T, damage *localvterm.WriteDamage) {
	t.Helper()
	elem := reflect.ValueOf(damage).Elem()
	field := elem.FieldByName("ResizeLiveTailRows")
	if !field.IsValid() {
		t.Fatal("expected WriteDamage.ResizeLiveTailRows field")
	}
	setField := elem.FieldByName("ResizeLiveTailRowsSet")
	if !setField.IsValid() {
		t.Fatal("expected WriteDamage.ResizeLiveTailRowsSet field")
	}
	field.SetInt(0)
	setField.SetBool(false)
}
