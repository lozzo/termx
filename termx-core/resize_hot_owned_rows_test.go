package termx

import (
	"reflect"
	"testing"

	localvterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestTerminalResizeFullReplaceUsesExplicitResizeHotOwnedRows(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	vt.LoadSnapshot(localvterm.ScreenData{Cells: [][]localvterm.Cell{localVTermCellsFromString("live")}}, localvterm.CursorState{Row: 0, Col: 4, Visible: true}, localvterm.TerminalModes{AutoWrap: true})
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "resize-explicit-hot",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}

	damage := localvterm.WriteDamage{
		ScrollbackAppend: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("cold"), WrappedSet: true, Wrapped: false},
			{Cells: localVTermCellsFromString("tail"), WrappedSet: true, Wrapped: true},
		},
		RequiresFullReplace: true,
		FullReplaceReason:   "resize",
	}
	setResizeHotOwnedRowsForTest(t, &damage, 1)
	term.appendGridFromDamageLocked(damage)

	viewport, err := store.Viewport(0, 10, 4)
	if err != nil {
		t.Fatalf("read cold viewport: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"cold"}) {
		t.Fatalf("expected only cold committed rows persisted, got %#v", got)
	}
	if got := vtermRowsToStrings(term.hotAppendRowsToRowsForTest()); !reflect.DeepEqual(got, []string{"tail"}) {
		t.Fatalf("expected explicit resize hot rows to stay display-only, got %#v", got)
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
	if !reflect.DeepEqual(combined, []string{"cold", "tail", "live"}) {
		t.Fatalf("expected latest snapshot projection cold+hot+screen, got %#v", combined)
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
		t.Fatalf("expected older viewport to exclude hot resize tail, got %#v", gotRows)
	}
}

func TestTerminalResizeFullReplacePrefersExplicitResizeHotOwnedRowsOverWrappedFallback(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:            "resize-explicit-priority",
		grid:          store,
		hotAppendRows: []localvterm.DamageOp{{Cells: localVTermCellsFromString("oldhot"), WrappedSet: true, Wrapped: true}},
	}

	damage := localvterm.WriteDamage{
		ScrollbackAppend: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("cold"), WrappedSet: true, Wrapped: false},
			{Cells: localVTermCellsFromString("tail"), WrappedSet: true, Wrapped: true},
		},
		RequiresFullReplace: true,
		FullReplaceReason:   "resize",
	}
	setResizeHotOwnedRowsForTest(t, &damage, 0)
	term.appendGridFromDamageLocked(damage)

	viewport, err := store.Viewport(0, 10, 4)
	if err != nil {
		t.Fatalf("read viewport: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"cold", "tail"}) {
		t.Fatalf("expected explicit resize field to override wrapped fallback and persist all rows, got %#v", got)
	}
	if got := vtermRowsToStrings(term.hotAppendRowsToRowsForTest()); !reflect.DeepEqual(got, []string{"oldhot"}) {
		t.Fatalf("expected explicit resize field to leave existing hot prefix intact while keeping new rows cold, got %#v", got)
	}
}

func TestTerminalResizeFullReplaceFallsBackWhenExplicitResizeHotOwnedRowsMissing(t *testing.T) {
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:            "resize-fallback-missing",
		grid:          store,
		hotAppendRows: []localvterm.DamageOp{{Cells: localVTermCellsFromString("oldhot"), WrappedSet: true, Wrapped: true}},
	}

	damage := localvterm.WriteDamage{
		ScrollbackAppend: []localvterm.DamageOp{
			{Cells: localVTermCellsFromString("cold"), WrappedSet: true, Wrapped: false},
			{Cells: localVTermCellsFromString("tail"), WrappedSet: true, Wrapped: true},
		},
		RequiresFullReplace: true,
		FullReplaceReason:   "resize",
	}
	clearResizeHotOwnedRowsForTest(t, &damage)
	term.appendGridFromDamageLocked(damage)

	viewport, err := store.Viewport(0, 10, 4)
	if err != nil {
		t.Fatalf("read viewport: %v", err)
	}
	if len(viewport.Rows) != 0 {
		t.Fatalf("expected legacy resize fallback to avoid committing rows when explicit field is absent, got %#v", vtermRowsToStrings(viewport.Rows))
	}
	if got := vtermRowsToStrings(term.hotAppendRowsToRowsForTest()); !reflect.DeepEqual(got, []string{"oldhot", "cold", "tail"}) {
		t.Fatalf("expected legacy resize fallback to keep full append display-only, got %#v", got)
	}
}

func setResizeHotOwnedRowsForTest(t *testing.T, damage *localvterm.WriteDamage, value int) {
	t.Helper()
	elem := reflect.ValueOf(damage).Elem()
	field := elem.FieldByName("ResizeHotOwnedRows")
	if !field.IsValid() {
		t.Fatal("expected WriteDamage.ResizeHotOwnedRows field")
	}
	setField := elem.FieldByName("ResizeHotOwnedRowsSet")
	if !setField.IsValid() {
		t.Fatal("expected WriteDamage.ResizeHotOwnedRowsSet field")
	}
	field.SetInt(int64(value))
	setField.SetBool(true)
}

func clearResizeHotOwnedRowsForTest(t *testing.T, damage *localvterm.WriteDamage) {
	t.Helper()
	elem := reflect.ValueOf(damage).Elem()
	field := elem.FieldByName("ResizeHotOwnedRows")
	if !field.IsValid() {
		t.Fatal("expected WriteDamage.ResizeHotOwnedRows field")
	}
	setField := elem.FieldByName("ResizeHotOwnedRowsSet")
	if !setField.IsValid() {
		t.Fatal("expected WriteDamage.ResizeHotOwnedRowsSet field")
	}
	field.SetInt(0)
	setField.SetBool(false)
}
