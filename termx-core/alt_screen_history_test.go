package termx

import (
	"reflect"
	"strings"
	"testing"

	localvterm "github.com/lozzow/termx/termx-vterm/vterm"
)

func TestTerminalAltScreenDoesNotCreatePrimaryHistoryAndRestoresPrimarySurface(t *testing.T) {
	vt := localvterm.New(8, 2, 0, nil)
	vt.DisableEmulatorScrollback()
	store := newMemoryTerminalGridStoreForTest(t)
	defer store.Close()
	term := &Terminal{
		id:    "alt-screen-primary-history-freeze",
		size:  Size{Cols: 8, Rows: 2},
		vterm: vt,
		grid:  store,
	}

	appendExplicitTerminalGridRowsForTest(t, store, []terminalGridRow{
		{cells: localVTermCellsFromString("base")},
	})
	vt.LoadSnapshot(
		localvterm.ScreenData{Cells: [][]localvterm.Cell{
			localVTermRowForTest("base", 8),
			localVTermRowForTest("", 8),
		}},
		localvterm.CursorState{Row: 0, Col: 4, Visible: true},
		localvterm.TerminalModes{AutoWrap: true},
	)
	beforeRows := store.RowCount()
	beforeLines := store.LogicalLineCount()
	if beforeRows == 0 || beforeLines != 1 {
		t.Fatalf("expected primary setup to seal one logical line, rows=%d lines=%d", beforeRows, beforeLines)
	}

	writeVTermDamageToGridAndAlternate(t, term, vt, "\x1b[?1049h")
	writeVTermDamageToGridAndAlternate(t, term, vt, "ALT0\r\nALT1\r\nALT2")
	if !vt.Modes().AlternateScreen {
		t.Fatal("expected vterm to enter alt-screen")
	}
	if got := store.RowCount(); got != beforeRows {
		t.Fatalf("expected alt-screen writes not to append primary persisted rows, before=%d after=%d", beforeRows, got)
	}
	if got := store.LogicalLineCount(); got != beforeLines {
		t.Fatalf("expected alt-screen writes not to create primary logical lines, before=%d after=%d", beforeLines, got)
	}

	altCoreViewport, err := term.combinedGridViewport(0, 10, 8, term.primaryLiveTail.clone())
	if err != nil {
		t.Fatalf("alt-screen history window viewport: %v", err)
	}
	altWindow := historyWindowFromCoreGridViewport(term.id, 0, altCoreViewport)
	if got := historyWindowRowTexts(altWindow); !reflect.DeepEqual(got, []string{"base"}) {
		t.Fatalf("expected alt-screen history window to keep frozen primary history only, got %#v", got)
	}
	if altWindow.LoadedLines != beforeLines || altWindow.LogicalTotal != beforeLines {
		t.Fatalf("expected alt-screen history window logical counts unchanged, loaded=%d total=%d", altWindow.LoadedLines, altWindow.LogicalTotal)
	}
	if historyWindowContainsText(altWindow, "ALT") {
		t.Fatalf("expected alt-screen history window not to contain alternate content, got %#v", altWindow.Rows)
	}

	altSnapshot := term.Snapshot(0, 10)
	if altSnapshot == nil {
		t.Fatal("expected alt-screen snapshot")
	}
	if !altSnapshot.Modes.AlternateScreen || !altSnapshot.Screen.IsAlternateScreen {
		t.Fatalf("expected snapshot to report alt-screen mode, got modes=%#v screen=%#v", altSnapshot.Modes, altSnapshot.Screen)
	}
	if !snapshotContains(altSnapshot, "ALT1") || !snapshotContains(altSnapshot, "ALT2") || snapshotContains(altSnapshot, "base") {
		t.Fatalf("expected alt snapshot to expose alternate surface without primary history, got %#v", altSnapshot)
	}

	altViewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 0, ScrollbackLimit: 10, Cols: 8})
	if altViewport == nil {
		t.Fatal("expected alt-screen viewport")
	}
	if got := rowsToStrings(altViewport.Rows); containsString(got, "base") {
		t.Fatalf("expected alt viewport not to expose primary history, got %#v", got)
	}
	if altViewport.ScrollbackTotal == 0 {
		t.Fatalf("expected alt viewport to stay on alternate scrollback namespace, got %#v", altViewport)
	}
	altReplay := term.HistoryReplay(HistoryReplayOptions{Alternate: true, Limit: 10})
	if altReplay.Rows == 0 {
		t.Fatalf("expected alt replay to stay separate from primary history, got %#v", altReplay)
	}
	if strings.Contains(altReplay.Replay, "base") {
		t.Fatalf("expected alt replay not to contain primary content, got %#v", altReplay)
	}

	writeVTermDamageToGridAndAlternate(t, term, vt, "\x1b[?1049l")
	if vt.Modes().AlternateScreen {
		t.Fatal("expected vterm to leave alt-screen")
	}
	if got := store.RowCount(); got != beforeRows {
		t.Fatalf("expected leaving alt-screen not to append primary persisted rows, before=%d after=%d", beforeRows, got)
	}
	if got := store.LogicalLineCount(); got != beforeLines {
		t.Fatalf("expected leaving alt-screen not to create primary logical lines, before=%d after=%d", beforeLines, got)
	}

	primarySnapshot := term.Snapshot(0, 10)
	if primarySnapshot == nil {
		t.Fatal("expected primary snapshot")
	}
	if primarySnapshot.Modes.AlternateScreen || primarySnapshot.Screen.IsAlternateScreen {
		t.Fatalf("expected primary snapshot after alt exit, got modes=%#v screen=%#v", primarySnapshot.Modes, primarySnapshot.Screen)
	}
	if !snapshotContains(primarySnapshot, "base") || snapshotContains(primarySnapshot, "ALT0") || snapshotContains(primarySnapshot, "ALT1") || snapshotContains(primarySnapshot, "ALT2") {
		t.Fatalf("expected primary surface/history restored without alt content, got %#v", primarySnapshot)
	}

	primaryViewport := term.GridViewportWithOptions(GridViewportOptions{ScrollbackOffset: 0, ScrollbackLimit: 10, Cols: 8})
	if primaryViewport == nil {
		t.Fatal("expected primary viewport")
	}
	if got := rowsToStrings(primaryViewport.Rows); !reflect.DeepEqual(got, []string{"base"}) {
		t.Fatalf("expected primary viewport to remain primary persisted history only, got %#v", got)
	}
	if primaryViewport.LoadedRows != beforeRows || primaryViewport.HistoryGeneration == 0 {
		t.Fatalf("expected primary metadata unchanged after alt-screen, loaded=%d gen=%d", primaryViewport.LoadedRows, primaryViewport.HistoryGeneration)
	}
	primaryReplay := term.HistoryReplay(HistoryReplayOptions{Limit: 10})
	if primaryReplay.Rows != beforeRows || primaryReplay.Replay == "" {
		t.Fatalf("expected primary replay to remain separate from alt history, got %#v", primaryReplay)
	}
	if strings.Contains(primaryReplay.Replay, "ALT") {
		t.Fatalf("expected primary replay not to contain alt content, got %#v", primaryReplay)
	}
}

func writeVTermDamageToGridAndAlternate(t *testing.T, term *Terminal, vt *localvterm.VTerm, text string) {
	t.Helper()
	_, err, damage := vt.WriteWithDamage([]byte(text))
	if err != nil {
		t.Fatalf("write vterm damage %q: %v", text, err)
	}
	term.captureAlternateDamageLocked(damage)
	term.appendGridFromDamageLocked(damage)
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
