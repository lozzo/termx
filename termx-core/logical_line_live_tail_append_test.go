package termx

import (
	"reflect"
	"strings"
	"testing"

	localvterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestSplitDamageRowsByLiveTailHintExtendsTailAcrossWrappedBoundary(t *testing.T) {
	rows := []localvterm.DamageOp{
		{Cells: localVTermCellsFromString("hist"), WrappedSet: true, Wrapped: false},
		{Cells: localVTermCellsFromString("mid"), WrappedSet: true, Wrapped: true},
		{Cells: localVTermCellsFromString("tail"), WrappedSet: true, Wrapped: true},
	}

	persisted, liveTail := splitDamageRowsByLiveTailHint(rows, 1)
	if got := damageRowsToStrings(persisted); !reflect.DeepEqual(got, []string{"hist"}) {
		t.Fatalf("expected persisted flush boundary to stay before wrapped continuation, got persisted=%#v live_tail=%#v", got, damageRowsToStrings(liveTail))
	}
	if got := damageRowsToStrings(liveTail); !reflect.DeepEqual(got, []string{"mid", "tail"}) {
		t.Fatalf("expected trailing wrapped continuation to remain in live tail, got %#v", got)
	}
}

func TestTerminalOpenExactWidthLineStaysLiveTailAcrossOverflowedTop(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "open-line-overflow",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}

	writeVTermDamageToGrid(t, term, vt, "abcd")
	writeVTermDamageToGrid(t, term, vt, "efgh")
	writeVTermDamageToGrid(t, term, vt, "ij")

	if got := store.RowCount(); got != 0 {
		t.Fatalf("expected full-width open logical line to stay out of persisted store before terminator, got %d persisted rows", got)
	}
	if got := vtermRowsToStrings(term.primaryLiveTailRowsToRowsForTest()); !reflect.DeepEqual(got, []string{"abcd", "efgh"}) {
		t.Fatalf("expected wrapped prefixes above visible top to stay in one live-tail open line, got %#v raw=%#v", got, term.primaryLiveTail.rows())
	}

	snapshot := term.Snapshot(0, 10)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	var combinedRows []string
	for _, row := range snapshot.Scrollback {
		combinedRows = append(combinedRows, rowToString(row))
	}
	for _, row := range snapshot.Screen.Cells {
		combinedRows = append(combinedRows, rowToString(row))
	}
	if !reflect.DeepEqual(combinedRows, []string{"abcd", "efgh", "ij"}) {
		t.Fatalf("expected latest snapshot to project one open logical line across live-tail+screen, got %#v", combinedRows)
	}
	combinedWrapped := append(append([]bool(nil), snapshot.ScrollbackWrapped...), snapshot.ScreenWrapped...)
	if !reflect.DeepEqual(combinedWrapped, []bool{true, true, false}) {
		t.Fatalf("expected wrapped metadata to preserve open logical line continuity, got %#v", combinedWrapped)
	}
}

func TestTerminalHardNewlineSealsAccumulatedLiveTailOpenLineToPersistedHistory(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "sealed-open-line",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}

	writeVTermDamageToGrid(t, term, vt, "abcd")
	writeVTermDamageToGrid(t, term, vt, "efgh")
	writeVTermDamageToGrid(t, term, vt, "ij")
	if got := store.RowCount(); got != 0 {
		t.Fatalf("expected no persisted rows before hard newline seal, got %d", got)
	}

	writeVTermDamageToGrid(t, term, vt, "\r\n")

	if got := vtermRowsToStrings(term.primaryLiveTailRowsToRowsForTest()); len(got) != 0 {
		t.Fatalf("expected hard newline to seal and clear live-tail builder, got %#v", got)
	}

	viewport, err := store.Viewport(0, 10, 4)
	if err != nil {
		t.Fatalf("read persisted viewport: %v", err)
	}
	if got := vtermRowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"abcd", "efgh", "ij"}) {
		t.Fatalf("expected hard newline to seal the whole logical line to persisted store, got %#v", got)
	}
	if !reflect.DeepEqual(viewport.Wrapped, []bool{true, true, false}) {
		t.Fatalf("expected persisted rows to end only at an unwrapped terminator, got %#v", viewport.Wrapped)
	}
	if got := store.LogicalLineCount(); got != 1 {
		t.Fatalf("expected one sealed logical line after hard newline, got %d", got)
	}
}

func TestTerminalExactWidthLineHardNewlineSealsToPersistedHistory(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "exact-width-hard-newline",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}

	writeVTermDamageToGrid(t, term, vt, "ABCD")
	if got := vtermRowsToStrings(term.primaryLiveTailRowsToRowsForTest()); len(got) != 0 {
		t.Fatalf("expected exact-width open line to avoid explicit live-tail rows before terminator, got %#v", got)
	}
	if !term.primaryLiveTail.wrapPending {
		t.Fatal("expected exact-width write to leave live-tail wrap pending before hard newline")
	}
	if got := store.RowCount(); got != 0 {
		t.Fatalf("expected no committed rows before hard newline, got %d", got)
	}

	writeVTermDamageToGrid(t, term, vt, "\r\n")

	if got := vtermRowsToStrings(term.primaryLiveTailRowsToRowsForTest()); len(got) != 0 {
		t.Fatalf("expected hard newline to clear exact-width live-tail builder, got %#v", got)
	}
	if term.primaryLiveTail.wrapPending {
		t.Fatal("expected hard newline to clear live-tail wrap pending")
	}
	if got := store.RowCount(); got != 1 {
		t.Fatalf("expected exact-width hard newline to seal one committed row, got %d", got)
	}
	if got := store.LogicalLineCount(); got != 1 {
		t.Fatalf("expected exact-width hard newline to seal one logical line, got %d", got)
	}

	latest := term.Snapshot(0, 10)
	if latest == nil {
		t.Fatal("expected latest snapshot")
	}
	if got := rowsToStrings(latest.Scrollback); !reflect.DeepEqual(got, []string{"ABCD"}) {
		t.Fatalf("expected latest snapshot to expose committed exact-width row, got %#v", got)
	}
	if latest.ScrollbackLoadedRows != 1 {
		t.Fatalf("expected latest snapshot committed depth 1, got %d", latest.ScrollbackLoadedRows)
	}
	if latest.HistoryGeneration == 0 || latest.ScrollbackFirstRowID != 0 || latest.ScrollbackLastRowID != 0 {
		t.Fatalf("expected latest snapshot to point at committed row window, gen=%d first=%d last=%d", latest.HistoryGeneration, latest.ScrollbackFirstRowID, latest.ScrollbackLastRowID)
	}
	if got := latest.ScrollbackWrapped; !reflect.DeepEqual(got, []bool{false}) {
		t.Fatalf("expected hard newline to seal exact-width row as unwrapped committed row, got %#v", got)
	}

	viewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 0, ScrollbackLimit: 10, Cols: 4})
	if viewport == nil {
		t.Fatal("expected latest viewport")
	}
	if got := rowsToStrings(viewport.Rows); !reflect.DeepEqual(got, []string{"ABCD"}) {
		t.Fatalf("expected latest viewport to expose committed exact-width row, got %#v", got)
	}
	if viewport.LoadedRows != 1 {
		t.Fatalf("expected latest viewport committed depth 1, got %d", viewport.LoadedRows)
	}
	if viewport.HistoryGeneration == 0 || viewport.FirstRowID != 0 || viewport.LastRowID != 0 {
		t.Fatalf("expected latest viewport to point at committed row window, gen=%d first=%d last=%d", viewport.HistoryGeneration, viewport.FirstRowID, viewport.LastRowID)
	}
	if got := viewport.ScrollbackWrapped; !reflect.DeepEqual(got, []bool{false}) {
		t.Fatalf("expected latest viewport to expose sealed exact-width row as unwrapped, got %#v", got)
	}
}

func TestTerminalWideAndCombiningExactWidthLineSealsAsOneLogicalLine(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "wide-combining-seal",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}

	writeVTermDamageToGrid(t, term, vt, "你e\u0301a")
	if got := store.RowCount(); got != 0 {
		t.Fatalf("expected wide/combining open line to stay out of persisted store before terminator, got %d rows", got)
	}
	if !term.primaryLiveTail.wrapPending {
		t.Fatal("expected exact-width wide/combining line to leave live-tail wrap pending before hard newline")
	}

	writeVTermDamageToGrid(t, term, vt, "\r\n")

	if got := store.RowCount(); got != 1 {
		t.Fatalf("expected hard newline to seal one persisted row, got %d", got)
	}
	if got := store.LogicalLineCount(); got != 1 {
		t.Fatalf("expected hard newline to seal one logical line, got %d", got)
	}
	if term.primaryLiveTail.wrapPending {
		t.Fatal("expected hard newline to clear wide/combining wrap pending")
	}

	wide, err := store.Viewport(0, 10, 4)
	if err != nil {
		t.Fatalf("wide viewport: %v", err)
	}
	if got := vtermRowsToStrings(wide.Rows); !reflect.DeepEqual(got, []string{"你éa"}) {
		t.Fatalf("expected normalized wide/combining logical line, got %#v", got)
	}
	if got := wide.Wrapped; !reflect.DeepEqual(got, []bool{false}) {
		t.Fatalf("expected sealed wide/combining row to terminate the logical line, got %#v", got)
	}
	if len(wide.Rows) != 1 || len(wide.Rows[0]) < 4 {
		t.Fatalf("expected wide viewport cells with continuation placeholder, got %#v", wide.Rows)
	}
	if got := wide.Rows[0][0]; got.Content != "你" || got.Width != 2 {
		t.Fatalf("expected wide anchor cell to persist, got %#v", got)
	}
	if got := wide.Rows[0][1]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected wide continuation placeholder to persist, got %#v", got)
	}
	if !vtermRowContainsCell(wide.Rows[0], "é", 1) {
		t.Fatalf("expected combining cluster to normalize before persistence, got %#v", wide.Rows[0])
	}
	if !vtermRowContainsCell(wide.Rows[0], "a", 1) {
		t.Fatalf("expected trailing ascii cell to persist after normalized grapheme, got %#v", wide.Rows[0])
	}

	narrow, err := store.Viewport(0, 10, 3)
	if err != nil {
		t.Fatalf("narrow viewport: %v", err)
	}
	if got := vtermRowsToStrings(narrow.Rows); !reflect.DeepEqual(got, []string{"你é", "a"}) {
		t.Fatalf("expected narrow projection to reflow by cell width, got %#v", got)
	}
	if got := narrow.Wrapped; !reflect.DeepEqual(got, []bool{true, false}) {
		t.Fatalf("expected narrow projection to preserve logical line boundary, got %#v", got)
	}
	if len(narrow.Rows) != 2 || len(narrow.Rows[0]) < 2 {
		t.Fatalf("expected narrow projection to keep wide continuation in first segment, got %#v", narrow.Rows)
	}
	if got := narrow.Rows[0][0]; got.Content != "你" || got.Width != 2 {
		t.Fatalf("expected narrow wide anchor cell to persist, got %#v", got)
	}
	if got := narrow.Rows[0][1]; got.Content != "" || got.Width != 0 {
		t.Fatalf("expected narrow wide continuation placeholder to persist, got %#v", got)
	}
	if !vtermRowContainsCell(narrow.Rows[0], "é", 1) {
		t.Fatalf("expected combining cluster to normalize before persistence, got %#v", narrow.Rows[0])
	}
	if !vtermRowContainsCell(narrow.Rows[1], "a", 1) {
		t.Fatalf("expected trailing ascii cell to remain in the final projected segment, got %#v", narrow.Rows[1])
	}
}

func vtermRowContainsCell(row []localvterm.Cell, content string, width int) bool {
	for _, cell := range row {
		if cell.Content == content && cell.Width == width {
			return true
		}
	}
	return false
}

func TestTerminalResizeFullReplacePreservesExistingLiveTailOpenLinePrefix(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "resize-live-tail-open-line-prefix",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}

	writeVTermDamageToGrid(t, term, vt, "abcd")
	writeVTermDamageToGrid(t, term, vt, "efgh")
	writeVTermDamageToGrid(t, term, vt, "ij")
	if got := vtermRowsToStrings(term.primaryLiveTailRowsToRowsForTest()); !reflect.DeepEqual(got, []string{"abcd", "efgh"}) {
		t.Fatalf("expected live-tail open-line prefix before resize, got %#v", got)
	}
	if got := store.RowCount(); got != 0 {
		t.Fatalf("expected no committed rows before resize, got %d", got)
	}

	damage := vt.ResizeWithDamage(1, 1)
	if !damage.RequiresFullReplace || damage.FullReplaceReason != "resize" {
		t.Fatalf("expected resize full-replace damage, got %#v", damage)
	}
	term.size = Size{Cols: 1, Rows: 1}
	term.appendGridFromDamageLocked(damage)

	if got := store.RowCount(); got != 0 {
		t.Fatalf("expected resize live tail to stay out of committed store, got %d committed rows", got)
	}

	liveTailRows := vtermRowsToStrings(term.primaryLiveTailRowsToRowsForTest())
	screenRows := vtermRowsToStrings(vt.ScreenContent().Cells)
	if got := strings.Join(append(append([]string(nil), liveTailRows...), screenRows...), ""); got != "abcdefghij" {
		t.Fatalf("expected resize to preserve full open logical line across live tail+screen, got live_tail=%#v screen=%#v", liveTailRows, screenRows)
	}

	snapshot := term.Snapshot(0, 10)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if got := strings.Join(rowsToStrings(snapshot.Scrollback), ""); got != strings.Join(liveTailRows, "") {
		t.Fatalf("expected latest snapshot scrollback to project live tail, got %#v", rowsToStrings(snapshot.Scrollback))
	}
	if snapshot.ScrollbackLoadedRows != 0 || snapshot.HistoryGeneration != 0 || snapshot.ScrollbackFirstRowID != 0 || snapshot.ScrollbackLastRowID != 0 {
		t.Fatalf("expected snapshot to keep zero committed-depth metadata for live tail, loaded=%d gen=%d first=%d last=%d", snapshot.ScrollbackLoadedRows, snapshot.HistoryGeneration, snapshot.ScrollbackFirstRowID, snapshot.ScrollbackLastRowID)
	}

	viewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 0, ScrollbackLimit: 10, Cols: 1})
	if viewport == nil {
		t.Fatal("expected viewport")
	}
	if got := strings.Join(rowsToStrings(viewport.Rows), ""); got != strings.Join(liveTailRows, "") {
		t.Fatalf("expected latest viewport rows to stay live-tail-only after resize, got %#v", rowsToStrings(viewport.Rows))
	}
	if viewport.LoadedRows != 0 || viewport.HistoryGeneration != 0 || viewport.FirstRowID != 0 || viewport.LastRowID != 0 {
		t.Fatalf("expected viewport to keep zero committed-depth metadata for live tail, loaded=%d gen=%d first=%d last=%d", viewport.LoadedRows, viewport.HistoryGeneration, viewport.FirstRowID, viewport.LastRowID)
	}

	olderViewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 1, ScrollbackLimit: 10, Cols: 1})
	if olderViewport == nil {
		t.Fatal("expected older viewport")
	}
	if got := rowsToStrings(olderViewport.Rows); len(got) != 0 {
		t.Fatalf("expected shrink-hidden live tail not to become older committed history, got %#v", got)
	}
	if olderViewport.LoadedRows != 0 || olderViewport.HistoryGeneration != 0 || olderViewport.FirstRowID != 0 || olderViewport.LastRowID != 0 {
		t.Fatalf("expected older viewport metadata to stay persisted-empty for shrink-hidden live tail, loaded=%d gen=%d first=%d last=%d", olderViewport.LoadedRows, olderViewport.HistoryGeneration, olderViewport.FirstRowID, olderViewport.LastRowID)
	}

	olderSnapshot := term.Snapshot(1, 10)
	if olderSnapshot == nil {
		t.Fatal("expected older snapshot")
	}
	if got := rowsToStrings(olderSnapshot.Scrollback); len(got) != 0 {
		t.Fatalf("expected shrink-hidden live tail not to appear in older snapshot history, got %#v", got)
	}
	if olderSnapshot.ScrollbackLoadedRows != 0 || olderSnapshot.HistoryGeneration != 0 || olderSnapshot.ScrollbackFirstRowID != 0 || olderSnapshot.ScrollbackLastRowID != 0 {
		t.Fatalf("expected older snapshot metadata to stay persisted-empty for shrink-hidden live tail, loaded=%d gen=%d first=%d last=%d", olderSnapshot.ScrollbackLoadedRows, olderSnapshot.HistoryGeneration, olderSnapshot.ScrollbackFirstRowID, olderSnapshot.ScrollbackLastRowID)
	}
}

func TestTerminalResizeFullReplacePreservesExactWidthLiveTailWrapPendingLine(t *testing.T) {
	vt := localvterm.New(4, 1, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "resize-live-tail-wrap-pending-exact-width",
		size:  Size{Cols: 4, Rows: 1},
		vterm: vt,
		grid:  store,
	}

	writeVTermDamageToGrid(t, term, vt, "abcd")
	if got := vtermRowsToStrings(term.primaryLiveTailRowsToRowsForTest()); len(got) != 0 {
		t.Fatalf("expected no explicit live-tail append rows before resize for exact-width open line, got %#v", got)
	}
	if !term.primaryLiveTail.wrapPending {
		t.Fatal("expected exact-width write to leave live-tail wrap pending before resize")
	}
	if got := store.RowCount(); got != 0 {
		t.Fatalf("expected no committed rows before resize, got %d", got)
	}

	damage := vt.ResizeWithDamage(1, 1)
	if !damage.RequiresFullReplace || damage.FullReplaceReason != "resize" {
		t.Fatalf("expected resize full-replace damage, got %#v", damage)
	}
	if got := damage.LiveTailAppendRows; got != 3 {
		t.Fatalf("expected exact-width resize append rows to expose three displaced rows, got %d", got)
	}
	if got := damageRowsToStrings(damage.ScrollbackAppend); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("expected resize to displace wrapped prefix rows only, got %#v", got)
	}

	term.size = Size{Cols: 1, Rows: 1}
	term.appendGridFromDamageLocked(damage)

	if got := store.RowCount(); got != 0 {
		t.Fatalf("expected exact-width open line to stay out of committed store after resize, got %d committed rows", got)
	}
	if got := term.primaryLiveTail.wrapPending; !got {
		t.Fatal("expected resize to preserve live-tail wrap pending for exact-width open line")
	}

	liveTailRows := vtermRowsToStrings(term.primaryLiveTailRowsToRowsForTest())
	screenRows := vtermRowsToStrings(vt.ScreenContent().Cells)
	if !reflect.DeepEqual(liveTailRows, []string{"a", "b", "c"}) {
		t.Fatalf("expected displaced prefix rows to stay in live tail after resize, got %#v", liveTailRows)
	}
	if !reflect.DeepEqual(screenRows, []string{"d"}) {
		t.Fatalf("expected final exact-width cell to remain on screen after resize, got %#v", screenRows)
	}
	if got := strings.Join(append(append([]string(nil), liveTailRows...), screenRows...), ""); got != "abcd" {
		t.Fatalf("expected resize to preserve full exact-width open line across live tail+screen, got live_tail=%#v screen=%#v", liveTailRows, screenRows)
	}

	snapshot := term.Snapshot(0, 10)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if got := rowsToStrings(snapshot.Scrollback); !reflect.DeepEqual(got, liveTailRows) {
		t.Fatalf("expected latest snapshot scrollback to project live-tail prefix rows only, got %#v", got)
	}
	if snapshot.ScrollbackLoadedRows != 0 || snapshot.HistoryGeneration != 0 || snapshot.ScrollbackFirstRowID != 0 || snapshot.ScrollbackLastRowID != 0 {
		t.Fatalf("expected snapshot to keep zero committed-depth metadata for exact-width live-tail line, loaded=%d gen=%d first=%d last=%d", snapshot.ScrollbackLoadedRows, snapshot.HistoryGeneration, snapshot.ScrollbackFirstRowID, snapshot.ScrollbackLastRowID)
	}
	if got := append(append([]bool(nil), snapshot.ScrollbackWrapped...), snapshot.ScreenWrapped...); !reflect.DeepEqual(got, []bool{true, true, true, false}) {
		t.Fatalf("expected snapshot wrapped metadata to preserve open exact-width line continuity, got %#v", got)
	}

	metadataOnly := term.Snapshot(1, 10)
	if metadataOnly == nil {
		t.Fatal("expected older snapshot")
	}
	if got := rowsToStrings(metadataOnly.Scrollback); len(got) != 0 {
		t.Fatalf("expected older snapshot to stay persisted-only and exclude live-tail open line, got %#v", got)
	}
	if metadataOnly.ScrollbackLoadedRows != 0 || metadataOnly.HistoryGeneration != 0 || metadataOnly.ScrollbackFirstRowID != 0 || metadataOnly.ScrollbackLastRowID != 0 {
		t.Fatalf("expected older snapshot metadata to keep committed depth at zero, loaded=%d gen=%d first=%d last=%d", metadataOnly.ScrollbackLoadedRows, metadataOnly.HistoryGeneration, metadataOnly.ScrollbackFirstRowID, metadataOnly.ScrollbackLastRowID)
	}

	viewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 0, ScrollbackLimit: 10, Cols: 1})
	if viewport == nil {
		t.Fatal("expected viewport")
	}
	if got := rowsToStrings(viewport.Rows); !reflect.DeepEqual(got, liveTailRows) {
		t.Fatalf("expected latest viewport rows to stay live-tail-only after exact-width resize, got %#v", got)
	}
	if viewport.LoadedRows != 0 || viewport.HistoryGeneration != 0 || viewport.FirstRowID != 0 || viewport.LastRowID != 0 {
		t.Fatalf("expected viewport to keep zero committed-depth metadata for exact-width live-tail line, loaded=%d gen=%d first=%d last=%d", viewport.LoadedRows, viewport.HistoryGeneration, viewport.FirstRowID, viewport.LastRowID)
	}

	olderViewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 1, ScrollbackLimit: 10, Cols: 1})
	if olderViewport == nil {
		t.Fatal("expected older viewport")
	}
	if got := rowsToStrings(olderViewport.Rows); len(got) != 0 {
		t.Fatalf("expected older viewport to exclude live-tail-only exact-width line, got %#v", got)
	}
	if olderViewport.LoadedRows != 0 || olderViewport.HistoryGeneration != 0 || olderViewport.FirstRowID != 0 || olderViewport.LastRowID != 0 {
		t.Fatalf("expected older viewport metadata to keep committed depth at zero, loaded=%d gen=%d first=%d last=%d", olderViewport.LoadedRows, olderViewport.HistoryGeneration, olderViewport.FirstRowID, olderViewport.LastRowID)
	}
}

func damageRowsToStrings(rows []localvterm.DamageOp) []string {
	if len(rows) == 0 {
		return nil
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, vtermRowToString(row.Cells))
	}
	return out
}
