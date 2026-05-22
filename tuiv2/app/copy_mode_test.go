package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/orchestrator"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type recordingControlWriter struct {
	cursor   string
	controls []string
}

func (w *recordingControlWriter) SetCursorSequence(seq string) {
	w.cursor = seq
}

func (w *recordingControlWriter) WriteControlSequence(seq string) error {
	w.controls = append(w.controls, seq)
	return nil
}

func (w *recordingControlWriter) QueueControlSequenceAfterWrite(seq string) {}

func protocolRowFromText(text string, cols int) []protocol.Cell {
	if cols <= 0 {
		cols = len(text)
	}
	row := make([]protocol.Cell, cols)
	runes := []rune(text)
	for i := 0; i < cols; i++ {
		content := " "
		if i < len(runes) {
			content = string(runes[i])
		}
		row[i] = protocol.Cell{Content: content, Width: 1}
	}
	return row
}

func copyModeTestSnapshot(scrollback, screen []string) *protocol.Snapshot {
	sbRows := make([][]protocol.Cell, 0, len(scrollback))
	maxCols := 1
	for _, line := range scrollback {
		if len([]rune(line)) > maxCols {
			maxCols = len([]rune(line))
		}
	}
	screenRows := make([][]protocol.Cell, 0, len(screen))
	for _, line := range screen {
		if len([]rune(line)) > maxCols {
			maxCols = len([]rune(line))
		}
	}
	for _, line := range scrollback {
		sbRows = append(sbRows, protocolRowFromText(line, maxCols))
	}
	for _, line := range screen {
		screenRows = append(screenRows, protocolRowFromText(line, maxCols))
	}
	var firstRowID uint64
	var lastRowID uint64
	historyGeneration := uint64(0)
	if len(sbRows) > 0 {
		firstRowID = 1000
		lastRowID = firstRowID + uint64(len(sbRows)-1)
		historyGeneration = 1
	}
	return &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: uint16(maxCols), Rows: uint16(len(screenRows))},
		Scrollback:             protocol.CompactRowsFromCells(sbRows),
		ScrollbackOwnership:    repeatedOwnership(protocol.RowOwnershipPersisted, len(sbRows)),
		ScrollbackTotal:        len(sbRows),
		ScrollbackLogicalTotal: len(sbRows),
		ScrollbackLoadedRows:   len(sbRows),
		HistoryGeneration:      historyGeneration,
		ScrollbackFirstRowID:   firstRowID,
		ScrollbackLastRowID:    lastRowID,
		Screen:                 protocol.ScreenData{Cells: screenRows},
		Cursor:                 protocol.CursorState{Row: maxInt(0, len(screenRows)-1), Col: 0, Visible: true},
		Modes:                  protocol.TerminalModes{AutoWrap: true},
	}
}

func copyModeSnapshotScreenText(snapshot *protocol.Snapshot) string {
	if snapshot == nil {
		return ""
	}
	var b strings.Builder
	for _, row := range snapshot.Screen.Cells {
		b.WriteString(rowTextFromProtocolCells(row))
		b.WriteByte('\n')
	}
	return b.String()
}

func rowTextFromProtocolCells(row []protocol.Cell) string {
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(cell.Content)
	}
	return strings.TrimRight(b.String(), " ")
}

func rowTextFromCompactRow(row protocol.CompactRow) string {
	return rowTextFromProtocolCells(row.DecodeCells())
}

func seedCopyModeSnapshot(t *testing.T, m *Model, scrollback, screen []string) {
	t.Helper()
	seedCopyModeSnapshotForTerminal(t, m, "term-1", scrollback, screen)
}

func seedCopyModeSnapshotForTerminal(t *testing.T, m *Model, terminalID string, scrollback, screen []string) {
	t.Helper()
	terminal := m.runtime.Registry().GetOrCreate(terminalID)
	snapshot := copyModeTestSnapshot(scrollback, screen)
	snapshot.TerminalID = terminalID
	terminal.Snapshot = snapshot
	if client, ok := m.runtime.Client().(*recordingBridgeClient); ok {
		if client.snapshotByTerminal == nil {
			client.snapshotByTerminal = make(map[string]*protocol.Snapshot)
		}
		client.snapshotByTerminal[terminalID] = snapshot
	}
}

func snapshotWindow(snapshot *protocol.Snapshot, offset int, limit int) *protocol.Snapshot {
	if snapshot == nil {
		return nil
	}
	cloned := cloneSnapshot(snapshot)
	if cloned == nil || limit <= 0 {
		return cloned
	}
	if offset < 0 {
		offset = 0
	}
	total := len(snapshot.Scrollback)
	if offset > total {
		offset = total
	}
	end := total - offset
	if end < 0 {
		end = 0
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	cloned.Scrollback = protocol.CloneCompactRows(snapshot.Scrollback[start:end])
	if len(snapshot.ScrollbackTimestamps) >= end {
		cloned.ScrollbackTimestamps = append([]time.Time(nil), snapshot.ScrollbackTimestamps[start:end]...)
	} else {
		cloned.ScrollbackTimestamps = nil
	}
	if len(snapshot.ScrollbackRowKinds) >= end {
		cloned.ScrollbackRowKinds = append([]string(nil), snapshot.ScrollbackRowKinds[start:end]...)
	} else {
		cloned.ScrollbackRowKinds = nil
	}
	if len(snapshot.ScrollbackWrapped) >= end {
		cloned.ScrollbackWrapped = append([]bool(nil), snapshot.ScrollbackWrapped[start:end]...)
	} else {
		cloned.ScrollbackWrapped = nil
	}
	if len(snapshot.ScrollbackOwnership) >= end {
		cloned.ScrollbackOwnership = append([]string(nil), snapshot.ScrollbackOwnership[start:end]...)
	} else {
		cloned.ScrollbackOwnership = nil
	}
	cloned.ScrollbackOffset = offset
	cloned.ScrollbackTotal = total
	if snapshot.ScrollbackLogicalTotal > 0 {
		cloned.ScrollbackLogicalTotal = snapshot.ScrollbackLogicalTotal
	} else {
		cloned.ScrollbackLogicalTotal = total
	}
	cloned.ScrollbackHasMore = start > 0
	cloned.ScrollbackLoadedRows = offset + len(cloned.Scrollback)
	if total > 0 {
		cloned.HistoryGeneration = snapshot.HistoryGeneration
	}
	if baseRowID, ok := snapshotRowIDBase(snapshot, total); ok && len(cloned.Scrollback) > 0 {
		cloned.ScrollbackFirstRowID = baseRowID + uint64(start)
		cloned.ScrollbackLastRowID = baseRowID + uint64(end-1)
	} else {
		cloned.ScrollbackFirstRowID = 0
		cloned.ScrollbackLastRowID = 0
	}
	return cloned
}

func snapshotRowIDBase(snapshot *protocol.Snapshot, total int) (uint64, bool) {
	if snapshot == nil || total <= 0 {
		return 0, false
	}
	if snapshot.HistoryGeneration != 0 && snapshot.ScrollbackLastRowID >= snapshot.ScrollbackFirstRowID {
		return snapshot.ScrollbackFirstRowID, true
	}
	if snapshot.ScrollbackLastRowID == 0 {
		return 0, false
	}
	base := snapshot.ScrollbackLastRowID + 1 - uint64(total)
	return base, true
}

func setupSplitCopyModeModel(t *testing.T) *Model {
	t.Helper()
	root := &workbench.LayoutNode{
		Direction: workbench.SplitVertical,
		Ratio:     0.5,
		First:     workbench.NewLeaf("pane-1"),
		Second:    workbench.NewLeaf("pane-2"),
	}
	model := setupModel(t, modelOpts{
		width:  80,
		height: 12,
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{{
					ID:           "tab-1",
					Name:         "tab 1",
					ActivePaneID: "pane-1",
					Panes: map[string]*workbench.PaneState{
						"pane-1": {ID: "pane-1", Title: "left", TerminalID: "term-1"},
						"pane-2": {ID: "pane-2", Title: "right", TerminalID: "term-2"},
					},
					Root: root,
				}},
			},
		},
	})
	for _, item := range []struct {
		paneID     string
		terminalID string
		channel    uint16
		name       string
	}{
		{paneID: "pane-1", terminalID: "term-1", channel: 1, name: "left"},
		{paneID: "pane-2", terminalID: "term-2", channel: 2, name: "right"},
	} {
		terminal := model.runtime.Registry().GetOrCreate(item.terminalID)
		terminal.Name = item.name
		terminal.State = "running"
		terminal.Channel = item.channel
		binding := model.runtime.BindPane(item.paneID)
		binding.Channel = item.channel
		binding.Connected = true
	}
	return model
}

func setupSharedTerminalCopyModeModel(t *testing.T) *Model {
	t.Helper()
	root := &workbench.LayoutNode{
		Direction: workbench.SplitVertical,
		Ratio:     0.5,
		First:     workbench.NewLeaf("pane-1"),
		Second:    workbench.NewLeaf("pane-2"),
	}
	model := setupModel(t, modelOpts{
		width:  80,
		height: 12,
		workspaces: map[string]*workbench.WorkspaceState{
			"main": {
				Name:      "main",
				ActiveTab: 0,
				Tabs: []*workbench.TabState{{
					ID:           "tab-1",
					Name:         "tab 1",
					ActivePaneID: "pane-1",
					Panes: map[string]*workbench.PaneState{
						"pane-1": {ID: "pane-1", Title: "left", TerminalID: "term-1"},
						"pane-2": {ID: "pane-2", Title: "right", TerminalID: "term-1"},
					},
					Root: root,
				}},
			},
		},
	})
	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.Name = "shared"
	terminal.State = "running"
	terminal.Channel = 1
	terminal.BoundPaneIDs = []string{"pane-1", "pane-2"}
	terminal.OwnerPaneID = "pane-1"
	for _, paneID := range []string{"pane-1", "pane-2"} {
		binding := model.runtime.BindPane(paneID)
		binding.Channel = 1
		binding.Connected = true
	}
	return model
}

func TestPagedSnapshotLoadedFallsBackToRuntimeMergeWhenFrozenCopyModeDoesNotConsumePage(t *testing.T) {
	model := setupSharedTerminalCopyModeModel(t)
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot = copyModeTestSnapshot([]string{"canon001"}, []string{"live0"})
	terminal.Snapshot.HistoryGeneration = 10
	terminal.Snapshot.ScrollbackFirstRowID = 1
	terminal.Snapshot.ScrollbackLastRowID = 1
	terminal.Snapshot.ScrollbackLoadedRows = 1
	terminal.Snapshot.ScrollbackTotal = 1
	terminal.Snapshot.ScrollbackLogicalTotal = 1

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	if model.copyMode.Snapshot == nil {
		t.Fatal("expected pane-1 to enter frozen copy mode")
	}
	if err := model.workbench.FocusPane("tab-1", "pane-2"); err != nil {
		t.Fatalf("focus pane-2: %v", err)
	}

	model.copyMode.Snapshot.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{
		protocolRowFromText("tail0", 8),
		protocolRowFromText("tail1", 8),
	})
	model.copyMode.Snapshot.ScrollbackOffset = 0
	model.copyMode.Snapshot.ScrollbackTotal = 2
	model.copyMode.Snapshot.ScrollbackLogicalTotal = 2
	model.copyMode.Snapshot.ScrollbackHasMore = false
	model.copyMode.Snapshot.ScrollbackLoadedRows = 0
	model.copyMode.Snapshot.HistoryGeneration = 0
	model.copyMode.Snapshot.ScrollbackFirstRowID = 0
	model.copyMode.Snapshot.ScrollbackLastRowID = 0
	model.copyMode.Snapshot.ScrollbackOwnership = []string{protocol.RowOwnershipLiveTailLive, protocol.RowOwnershipLiveTailLive}
	model.copyMode.CommittedLoadedRows = 0
	model.saveCurrentCopyModeState()

	olderPage := copyModeTestSnapshot([]string{"canon000"}, []string{"live0"})
	olderPage.TerminalID = "term-1"
	olderPage.ScrollbackOffset = 1
	olderPage.ScrollbackTotal = 2
	olderPage.ScrollbackLogicalTotal = 2
	olderPage.ScrollbackLoadedRows = 2
	olderPage.HistoryGeneration = 10
	olderPage.ScrollbackFirstRowID = 0
	olderPage.ScrollbackLastRowID = 0

	_, cmd := model.Update(orchestrator.SnapshotLoadedMsg{
		TerminalID:      "term-1",
		Snapshot:        olderPage,
		Offset:          1,
		Limit:           1,
		Paged:           true,
		CopyModeRequest: false,
	})
	drainCmd(t, model, cmd, 20)

	terminal = model.runtime.Registry().Get("term-1")
	if terminal == nil || terminal.Snapshot == nil {
		t.Fatalf("expected runtime terminal snapshot after paged merge, got %#v", terminal)
	}
	if got, want := snapshotScrollbackLoadedDepth(terminal.Snapshot), 2; got != want {
		t.Fatalf("expected runtime merge to raise loaded depth to %d, got %d", want, got)
	}
	if got, want := len(terminal.Snapshot.Scrollback), 2; got != want {
		t.Fatalf("expected runtime merge to prepend older page, got %d rows want %d", got, want)
	}
	if got := rowTextFromCompactRow(terminal.Snapshot.Scrollback[0]); got != "canon000" {
		t.Fatalf("expected merged runtime snapshot to start with canon000, got %q", got)
	}
	if got := rowTextFromCompactRow(terminal.Snapshot.Scrollback[1]); got != "canon001" {
		t.Fatalf("expected merged runtime snapshot to keep canon001 second, got %q", got)
	}
	if got, want := terminal.Snapshot.HistoryGeneration, uint64(10); got != want {
		t.Fatalf("expected runtime merge to preserve canonical generation, got %d want %d", got, want)
	}
	if got, want := terminal.Snapshot.ScrollbackFirstRowID, uint64(0); got != want {
		t.Fatalf("expected runtime merge to update first row id, got %d want %d", got, want)
	}
	if got, want := terminal.Snapshot.ScrollbackLastRowID, uint64(1); got != want {
		t.Fatalf("expected runtime merge to keep last row id, got %d want %d", got, want)
	}

	frozen, ok := model.copyModeStateForPane("pane-1")
	if !ok || frozen.Snapshot == nil {
		t.Fatalf("expected pane-1 frozen copy-mode state after live merge, got %#v ok=%v", frozen, ok)
	}
	if got := snapshotScrollbackLoadedDepth(frozen.Snapshot); got != 0 {
		t.Fatalf("expected unmatched frozen snapshot to keep committed depth 0, got %d", got)
	}
	if got, want := len(frozen.Snapshot.Scrollback), 2; got != want {
		t.Fatalf("expected unmatched frozen snapshot to stay unchanged, got %d rows want %d", got, want)
	}
	if got := rowTextFromCompactRow(frozen.Snapshot.Scrollback[0]); got != "tail0" {
		t.Fatalf("expected frozen snapshot not to consume live page, got %q", got)
	}
}

func TestPagedSnapshotLoadedSkipsRuntimeMergeWhenFrozenCopyModeConsumesPage(t *testing.T) {
	model := setupSharedTerminalCopyModeModel(t)
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot = copyModeTestSnapshot([]string{"canon001"}, []string{"live0"})
	terminal.Snapshot.HistoryGeneration = 10
	terminal.Snapshot.ScrollbackFirstRowID = 1
	terminal.Snapshot.ScrollbackLastRowID = 1
	terminal.Snapshot.ScrollbackLoadedRows = 1
	terminal.Snapshot.ScrollbackTotal = 1
	terminal.Snapshot.ScrollbackLogicalTotal = 1

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	if model.copyMode.Snapshot == nil {
		t.Fatal("expected pane-1 to enter frozen copy mode")
	}
	if err := model.workbench.FocusPane("tab-1", "pane-2"); err != nil {
		t.Fatalf("focus pane-2: %v", err)
	}

	olderPage := copyModeTestSnapshot([]string{"canon000"}, []string{"live0"})
	olderPage.TerminalID = "term-1"
	olderPage.ScrollbackOffset = 1
	olderPage.ScrollbackTotal = 2
	olderPage.ScrollbackLogicalTotal = 2
	olderPage.ScrollbackLoadedRows = 2
	olderPage.HistoryGeneration = 10
	olderPage.ScrollbackFirstRowID = 0
	olderPage.ScrollbackLastRowID = 0

	_, cmd := model.Update(orchestrator.SnapshotLoadedMsg{
		TerminalID:      "term-1",
		Snapshot:        olderPage,
		Offset:          1,
		Limit:           1,
		Paged:           true,
		CopyModeRequest: true,
	})
	drainCmd(t, model, cmd, 20)

	frozen, ok := model.copyModeStateForPane("pane-1")
	if !ok || frozen.Snapshot == nil {
		t.Fatalf("expected pane-1 frozen copy-mode state after paged load, got %#v ok=%v", frozen, ok)
	}
	if got, want := snapshotScrollbackLoadedDepth(frozen.Snapshot), 2; got != want {
		t.Fatalf("expected frozen snapshot to consume matching page to depth %d, got %d", want, got)
	}
	if got, want := len(frozen.Snapshot.Scrollback), 2; got != want {
		t.Fatalf("expected frozen snapshot to prepend matching page, got %d rows want %d", got, want)
	}
	if got := rowTextFromCompactRow(frozen.Snapshot.Scrollback[0]); got != "canon000" {
		t.Fatalf("expected consumed frozen snapshot to start with canon000, got %q", got)
	}
	if got := rowTextFromCompactRow(frozen.Snapshot.Scrollback[1]); got != "canon001" {
		t.Fatalf("expected consumed frozen snapshot to keep canon001 second, got %q", got)
	}

	terminal = model.runtime.Registry().Get("term-1")
	if terminal == nil || terminal.Snapshot == nil {
		t.Fatalf("expected runtime terminal snapshot to remain available, got %#v", terminal)
	}
	if got, want := snapshotScrollbackLoadedDepth(terminal.Snapshot), 1; got != want {
		t.Fatalf("expected runtime snapshot to keep live committed depth %d, got %d", want, got)
	}
	if got, want := len(terminal.Snapshot.Scrollback), 1; got != want {
		t.Fatalf("expected runtime snapshot to stay unmerged while frozen pane consumes page, got %d rows want %d", got, want)
	}
	if got := rowTextFromCompactRow(terminal.Snapshot.Scrollback[0]); got != "canon001" {
		t.Fatalf("expected runtime live snapshot not to be polluted by frozen page, got %q", got)
	}
	if got, want := terminal.Snapshot.ScrollbackFirstRowID, uint64(1); got != want {
		t.Fatalf("expected runtime first row id unchanged, got %d want %d", got, want)
	}
	if got, want := terminal.Snapshot.ScrollbackLastRowID, uint64(1); got != want {
		t.Fatalf("expected runtime last row id unchanged, got %d want %d", got, want)
	}
}

func TestPagedSnapshotLoadedCopyModeRequestDoesNotPolluteRuntimeAfterCopyModeExit(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"canon001"}, []string{"live0"})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot.HistoryGeneration = 10
	terminal.Snapshot.ScrollbackFirstRowID = 1
	terminal.Snapshot.ScrollbackLastRowID = 1
	terminal.Snapshot.ScrollbackLoadedRows = 1
	terminal.Snapshot.ScrollbackTotal = 1
	terminal.Snapshot.ScrollbackLogicalTotal = 1
	terminal.CommittedLoadedDepth = 1
	terminal.CommittedHistoryExhausted = false
	client := model.runtime.Client().(*recordingBridgeClient)
	client.snapshotByTerminal["term-1"] = &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   terminal.Snapshot.Size,
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromText("canon000", 8)}),
		ScrollbackOffset:       1,
		ScrollbackTotal:        2,
		ScrollbackLogicalTotal: 2,
		ScrollbackLoadedRows:   2,
		HistoryGeneration:      10,
		ScrollbackFirstRowID:   0,
		ScrollbackLastRowID:    0,
		ScrollbackHasMore:      false,
		Screen:                 protocol.ScreenData{Cells: cloneProtocolRows(terminal.Snapshot.Screen.Cells)},
		Cursor:                 terminal.Snapshot.Cursor,
		Modes:                  terminal.Snapshot.Modes,
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	if model.copyMode.Snapshot == nil {
		t.Fatal("expected frozen copy-mode snapshot before exit")
	}
	buffer, ok := model.activeCopyModeBuffer()
	if !ok {
		t.Fatal("expected active copy-mode buffer before exit")
	}
	staleCopyModeCmd := model.ensureCopyModeScrollbackCmd(buffer)
	if staleCopyModeCmd == nil {
		t.Fatal("expected copy-mode history request command before exit")
	}
	if got, want := terminal.CommittedLoadingDepth, 501; got != want {
		t.Fatalf("expected copy-mode request to mark loading limit %d before exit, got %d", want, got)
	}
	if state, ok := model.historyLoading["term-1"]; !ok || state.Owner != historyLoadingOwnerCopyMode || state.Limit != 501 {
		t.Fatalf("expected copy-mode request to own loading slot 501 before exit, got %#v ok=%v", state, ok)
	}
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCancelMode})
	if model.activePaneInCopyMode() {
		t.Fatal("expected copy mode to exit before paged response arrives")
	}
	if got, want := terminal.CommittedLoadedDepth, 1; got != want {
		t.Fatalf("expected live loaded limit %d before stale copy-mode response, got %d", want, got)
	}
	if terminal.CommittedHistoryExhausted {
		t.Fatal("expected live exhausted flag to stay false before stale copy-mode response")
	}

	// Simulate the user leaving copy mode before its older-page response lands,
	// then a live prefetch immediately trying to take ownership of the same
	// numeric nextLimit through the normal exit path.
	if got := terminal.CommittedLoadingDepth; got != 0 {
		t.Fatalf("expected copy-mode exit to clear copy-mode loading slot before live prefetch, got %d", got)
	}
	if _, ok := model.historyLoading["term-1"]; ok {
		t.Fatalf("expected copy-mode exit to clear copy-mode loading owner, got %#v", model.historyLoading["term-1"])
	}
	_ = model.runtime.SetPaneViewportOffset("pane-1", 20)
	liveCmdBeforeStale := model.ensureActivePaneScrollbackCmd()
	if liveCmdBeforeStale == nil {
		t.Fatal("expected live pane prefetch command before stale copy-mode response")
	}
	if got, want := terminal.CommittedLoadingDepth, 501; got != want {
		t.Fatalf("expected live prefetch to mark loading limit %d before stale copy-mode response, got %d", want, got)
	}
	if state, ok := model.historyLoading["term-1"]; !ok || state.Owner != historyLoadingOwnerLive || state.Limit != 501 {
		t.Fatalf("expected live prefetch to own loading slot 501 after exit, got %#v ok=%v", state, ok)
	}

	drainCmd(t, model, staleCopyModeCmd, 20)

	terminal = model.runtime.Registry().Get("term-1")
	if terminal == nil || terminal.Snapshot == nil {
		t.Fatalf("expected runtime terminal snapshot after stale copy-mode response, got %#v", terminal)
	}
	if got, want := terminal.CommittedLoadedDepth, 1; got != want {
		t.Fatalf("expected stale copy-mode response not to raise live loaded limit to 2, got %d want %d", got, want)
	}
	if terminal.CommittedHistoryExhausted {
		t.Fatal("expected stale copy-mode response not to mark live exhausted")
	}
	if got, want := snapshotScrollbackLoadedDepth(terminal.Snapshot), 1; got != want {
		t.Fatalf("expected runtime committed depth to stay %d after copy-mode response lost its owner, got %d", want, got)
	}
	if got, want := len(terminal.Snapshot.Scrollback), 1; got != want {
		t.Fatalf("expected runtime snapshot not to prepend copy-mode page after exit, got %d rows want %d", got, want)
	}
	if got := rowTextFromCompactRow(terminal.Snapshot.Scrollback[0]); got != "canon001" {
		t.Fatalf("expected runtime snapshot to keep canon001 after exited copy-mode page arrives, got %q", got)
	}
	if got, want := terminal.Snapshot.ScrollbackFirstRowID, uint64(1); got != want {
		t.Fatalf("expected runtime first row id unchanged after exited copy-mode page, got %d want %d", got, want)
	}
	if got, want := terminal.Snapshot.ScrollbackLastRowID, uint64(1); got != want {
		t.Fatalf("expected runtime last row id unchanged after exited copy-mode page, got %d want %d", got, want)
	}
	if got, want := terminal.CommittedLoadingDepth, 501; got != want {
		t.Fatalf("expected stale copy-mode response not to clear live loading limit %d, got %d", want, got)
	}

	client.snapshotByTerminal["term-1"] = &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   terminal.Snapshot.Size,
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromText("canon000", 8)}),
		ScrollbackOffset:       1,
		ScrollbackTotal:        2,
		ScrollbackLogicalTotal: 2,
		ScrollbackLoadedRows:   2,
		HistoryGeneration:      10,
		ScrollbackFirstRowID:   0,
		ScrollbackLastRowID:    0,
		ScrollbackHasMore:      false,
		Screen:                 protocol.ScreenData{Cells: cloneProtocolRows(terminal.Snapshot.Screen.Cells)},
		Cursor:                 terminal.Snapshot.Cursor,
		Modes:                  terminal.Snapshot.Modes,
	}
	beforeViewportCalls := len(client.viewportRequests)
	msg := liveCmdBeforeStale()
	typed, ok := msg.(orchestrator.SnapshotLoadedMsg)
	if !ok {
		t.Fatalf("expected live prefetch to return SnapshotLoadedMsg, got %#v", msg)
	}
	_, followCmd := model.Update(typed)
	drainCmd(t, model, followCmd, 20)

	if got := len(client.viewportRequests); got != beforeViewportCalls+1 {
		t.Fatalf("expected one live history viewport request after stale copy-mode response, before=%d after=%d calls=%#v", beforeViewportCalls, got, client.viewportRequests)
	}
	request := client.viewportRequests[len(client.viewportRequests)-1]
	if request.offset != 1 {
		t.Fatalf("expected live history request to continue from committed depth 1, got %#v", request)
	}
	if got := terminal.CommittedLoadingDepth; got != 0 {
		t.Fatalf("expected live response to clear its own loading marker, got %d", got)
	}
}

func TestCopyModeExitDoesNotClearLiveOwnedHistoryLoadingSlot(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"hist0"}, []string{"live0"})
	terminal := model.runtime.Registry().Get("term-1")

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	if model.copyMode.Snapshot == nil {
		t.Fatal("expected frozen copy-mode snapshot before exit")
	}

	model.historyLoading["term-1"] = historyLoadingState{
		Limit: 501,
		Owner: historyLoadingOwnerLive,
	}
	terminal.CommittedLoadingDepth = 501

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCancelMode})
	if model.activePaneInCopyMode() {
		t.Fatal("expected copy mode to exit")
	}
	if got, want := terminal.CommittedLoadingDepth, 501; got != want {
		t.Fatalf("expected copy-mode exit not to clear live-owned loading limit %d, got %d", want, got)
	}
	if state, ok := model.historyLoading["term-1"]; !ok || state.Owner != historyLoadingOwnerLive || state.Limit != 501 {
		t.Fatalf("expected copy-mode exit to preserve live-owned loading slot, got %#v ok=%v", state, ok)
	}
}

func TestCopyModeScrollbackCmdMarksSnapshotLoadedMsgAsCopyModeRequest(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"hist0"}, []string{"live0"})
	client := model.runtime.Client().(*recordingBridgeClient)
	client.snapshotByTerminal["term-1"] = copyModeTestSnapshot([]string{"old0", "hist0"}, []string{"live0"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	buffer, ok := model.activeCopyModeBuffer()
	if !ok {
		t.Fatal("expected active copy-mode buffer")
	}
	model.copyMode.Cursor = copyModePoint{Row: 0, Col: 0}
	model.copyMode.ViewTopRow = 0

	cmd := model.ensureCopyModeScrollbackCmd(buffer)
	if cmd == nil {
		t.Fatal("expected copy-mode history request command")
	}
	msg := cmd()
	typed, ok := msg.(orchestrator.SnapshotLoadedMsg)
	if !ok {
		t.Fatalf("expected SnapshotLoadedMsg, got %#v", msg)
	}
	if !typed.CopyModeRequest {
		t.Fatalf("expected copy-mode history request to mark CopyModeRequest, got %#v", typed)
	}
}

func TestCopyModeScrollbackCmdUsesFrozenSnapshotCols(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"hist0"}, []string{"live0"})
	client := model.runtime.Client().(*recordingBridgeClient)
	client.snapshotByTerminal["term-1"] = copyModeTestSnapshot([]string{"old0", "hist0"}, []string{"live0"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	buffer, ok := model.activeCopyModeBuffer()
	if !ok {
		t.Fatal("expected active copy-mode buffer")
	}
	if buffer.snapshot == nil {
		t.Fatal("expected frozen copy-mode snapshot")
	}
	buffer.snapshot.Size.Cols = 40
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot = cloneSnapshot(terminal.Snapshot)
	terminal.Snapshot.Size.Cols = 120
	model.copyMode.Snapshot.Size.Cols = 40
	model.copyMode.Cursor = copyModePoint{Row: 0, Col: 0}
	model.copyMode.ViewTopRow = 0

	before := len(client.viewportRequests)
	cmd := model.ensureCopyModeScrollbackCmd(buffer)
	if cmd == nil {
		t.Fatal("expected copy-mode history request command")
	}
	_ = cmd()
	if got := len(client.viewportRequests); got != before+1 {
		t.Fatalf("expected one viewport request, before=%d after=%d calls=%#v", before, got, client.viewportRequests)
	}
	if got := client.viewportRequests[len(client.viewportRequests)-1].cols; got != 40 {
		t.Fatalf("expected copy-mode request to use frozen cols 40, got %d calls=%#v", got, client.viewportRequests)
	}
}

func TestCopyModeKeyboardSelectionCopiesOSC52(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"alpha", "bravo"}, []string{"charl", "delta", "echoo"})
	writer := &recordingControlWriter{}
	model.SetCursorWriter(writer)

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	if got := model.input.Mode().Kind; got != input.ModeDisplay {
		t.Fatalf("expected display mode, got %q", got)
	}
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeBeginSelection})
	if model.copyMode.Mark == nil {
		t.Fatalf("expected mark after begin selection, copyMode=%#v", model.copyMode)
	}
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeCursorRight})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeCursorRight})
	if model.copyMode.Mark == nil {
		t.Fatalf("expected mark after cursor moves, copyMode=%#v", model.copyMode)
	}
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeCopySelectionExit})

	if got := len(writer.controls); got != 1 {
		t.Fatalf("expected one clipboard write, got %d (%#v), err=%v copyMode=%#v", got, writer.controls, model.err, model.copyMode)
	}
	if want := osc52ClipboardSequence("alp"); writer.controls[0] != want {
		t.Fatalf("unexpected clipboard payload %q want %q", writer.controls[0], want)
	}
	if got := model.input.Mode().Kind; got != input.ModeNormal {
		t.Fatalf("expected copy+exit to return to normal mode, got %q", got)
	}
	if got := model.runtime.PaneViewportOffset("pane-1"); got != 0 {
		t.Fatalf("expected copy+exit to reset pane viewport, got %d", got)
	}
}

func TestCopyModeMouseSwitchPanePreservesHistoryPaneAndUpdatesActiveShortcuts(t *testing.T) {
	model := setupSplitCopyModeModel(t)
	seedCopyModeSnapshotForTerminal(t, model, "term-1", []string{"hist-left"}, []string{"live-left"})
	seedCopyModeSnapshotForTerminal(t, model, "term-2", []string{"hist-right"}, []string{"live-right"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeBeginSelection})
	if got := model.copyMode.PaneID; got != "pane-1" {
		t.Fatalf("expected copy mode bound to pane-1, got %q", got)
	}
	if model.copyMode.Mark == nil {
		t.Fatal("expected copy-mode mark before pane switch")
	}

	visible := model.workbench.VisibleWithSize(model.bodyRect())
	if visible == nil || visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		t.Fatal("expected visible workbench")
	}
	var pane2 *workbench.VisiblePane
	for i := range visible.Tabs[visible.ActiveTab].Panes {
		if visible.Tabs[visible.ActiveTab].Panes[i].ID == "pane-2" {
			pane2 = &visible.Tabs[visible.ActiveTab].Panes[i]
			break
		}
	}
	if pane2 == nil {
		t.Fatal("expected visible pane-2")
	}
	contentRect, ok := paneContentRectForVisible(*pane2)
	if !ok {
		t.Fatal("expected pane-2 content rect")
	}
	x := contentRect.X
	y := model.contentOriginY() + contentRect.Y
	_, cmd := model.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drainCmd(t, model, cmd, 20)
	_ = model.View()

	if pane := model.workbench.ActivePane(); pane == nil || pane.ID != "pane-2" {
		t.Fatalf("expected pane-2 focused after click, got %#v", pane)
	}
	if got := model.effectiveInputMode(); got != input.ModeNormal {
		t.Fatalf("expected active pane shortcuts to return to normal mode, got %q", got)
	}
	vm := model.renderVM()
	if got := vm.Status.InputMode; got != string(input.ModeNormal) {
		t.Fatalf("expected rendered status mode to follow active pane, got %q", got)
	}
	hints := strings.Join(vm.Status.Hints, " ")
	if !strings.Contains(hints, "P PANE") {
		t.Fatalf("expected normal-mode shortcuts for active pane, got %#v", vm.Status.Hints)
	}
	if strings.Contains(hints, "MOVE CURSOR") {
		t.Fatalf("expected inactive history pane not to drive copy-mode shortcuts, got %#v", vm.Status.Hints)
	}
	if got := model.copyMode.PaneID; got != "pane-1" {
		t.Fatalf("expected inactive pane to keep copy mode binding, got %q", got)
	}
	if model.copyMode.Mark == nil {
		t.Fatal("expected inactive pane to keep copy-mode selection")
	}
	view := xansi.Strip(model.View())
	if !strings.Contains(view, "hist-left") {
		t.Fatalf("expected inactive history pane to stay rendered, got:\n%s", view)
	}

	dispatchKey(t, model, ctrlKey(tea.KeyCtrlP))
	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	if got := model.copyMode.PaneID; got != "pane-1" {
		t.Fatalf("expected transient active-pane modes to preserve inactive copy mode, got %q", got)
	}
	if got := model.effectiveInputMode(); got != input.ModeNormal {
		t.Fatalf("expected inactive history pane not to affect shortcuts after mode exit, got %q", got)
	}

	client := model.runtime.Client().(*recordingBridgeClient)
	dispatchKey(t, model, runeKeyMsg('x'))
	if len(client.inputCalls) != 1 {
		t.Fatalf("expected active pane key input to be forwarded, got %#v", client.inputCalls)
	}
	if client.inputCalls[0].channel != 2 || string(client.inputCalls[0].data) != "x" {
		t.Fatalf("expected key input on pane-2 channel, got %#v", client.inputCalls[0])
	}

	if err := model.workbench.FocusPane("tab-1", "pane-1"); err != nil {
		t.Fatalf("refocus pane-1: %v", err)
	}
	if got := model.effectiveInputMode(); got != input.ModeDisplay {
		t.Fatalf("expected refocused history pane to restore copy shortcuts, got %q", got)
	}
	vm = model.renderVM()
	if got := vm.Status.InputMode; got != string(input.ModeDisplay) {
		t.Fatalf("expected status mode to restore display for history pane, got %q", got)
	}
	hints = strings.Join(vm.Status.Hints, " ")
	if !strings.Contains(hints, "MOVE CURSOR") {
		t.Fatalf("expected copy-mode shortcuts after refocus, got %#v", vm.Status.Hints)
	}
}

func TestCopyModeSupportsTwoPanesAndScrollsActivePaneWithoutBlankingEither(t *testing.T) {
	model := setupSplitCopyModeModel(t)
	seedCopyModeSnapshotForTerminal(t, model, "term-1", []string{"hist-left-0", "hist-left-1", "hist-left-2"}, []string{"live-left"})
	seedCopyModeSnapshotForTerminal(t, model, "term-2", []string{"hist-right-0", "hist-right-1", "hist-right-2"}, []string{"live-right"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})
	if _, ok := model.copyModeStateForPane("pane-1"); !ok {
		t.Fatal("expected pane-1 copy mode")
	}

	if err := model.workbench.FocusPane("tab-1", "pane-2"); err != nil {
		t.Fatalf("focus pane-2: %v", err)
	}
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})
	if _, ok := model.copyModeStateForPane("pane-2"); !ok {
		t.Fatal("expected pane-2 copy mode")
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeCursorDown})

	pane1, ok := model.copyModeStateForPane("pane-1")
	if !ok || pane1.Snapshot == nil {
		t.Fatalf("expected pane-1 copy mode to keep frozen snapshot, got %#v ok=%v", pane1, ok)
	}
	pane2, ok := model.copyModeStateForPane("pane-2")
	if !ok || pane2.Snapshot == nil {
		t.Fatalf("expected pane-2 copy mode to keep frozen snapshot, got %#v ok=%v", pane2, ok)
	}
	if pane2.Cursor.Row <= 0 {
		t.Fatalf("expected pane-2 scroll action to move its copy cursor, got %#v", pane2.Cursor)
	}
	vm := model.renderVM()
	if got := len(vm.Body.CopyModes); got != 2 {
		t.Fatalf("expected render vm to carry both copy modes, got %d (%#v)", got, vm.Body.CopyModes)
	}
	view := xansi.Strip(model.View())
	for _, want := range []string{"hist-left", "hist-right"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to keep %q visible after active copy scroll:\n%s", want, view)
		}
	}
	if got := model.effectiveInputMode(); got != input.ModeDisplay {
		t.Fatalf("expected active copy pane shortcuts, got %q", got)
	}
}

func TestCopyModeSpaceCopiesAndClearsSelection(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"alpha", "bravo"}, []string{"charl", "delta", "echoo"})
	writer := &recordingControlWriter{}
	model.SetCursorWriter(writer)

	dispatchKey(t, model, ctrlKey(tea.KeyCtrlV))
	dispatchKey(t, model, runeKeyMsg('g'))
	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeySpace})
	if model.copyMode.Mark == nil {
		t.Fatal("expected first space to begin selection")
	}
	dispatchKey(t, model, runeKeyMsg('l'))
	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeySpace})

	if got := len(writer.controls); got != 1 {
		t.Fatalf("expected second space to copy once, got %#v", writer.controls)
	}
	if want := osc52ClipboardSequence("al"); writer.controls[0] != want {
		t.Fatalf("unexpected clipboard payload %q want %q", writer.controls[0], want)
	}
	if got := model.input.Mode().Kind; got != input.ModeDisplay {
		t.Fatalf("expected space copy to keep display mode, got %q", got)
	}
	if model.copyMode.Mark != nil {
		t.Fatalf("expected copied selection to clear mark, got %#v", model.copyMode.Mark)
	}
	if model.copyMode.MouseSelecting {
		t.Fatal("expected copied selection to stop mouse-select state")
	}

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyDown})
	if model.copyMode.Mark != nil {
		t.Fatalf("expected navigation after copy to stay out of selection mode, got %#v", model.copyMode.Mark)
	}

	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeySpace})
	if model.copyMode.Mark == nil {
		t.Fatal("expected third space to start a fresh selection")
	}
}

func TestCopyModeMouseAutoScrollExtendsSelection(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"s0", "s1", "s2", "s3", "s4", "s5"}, []string{"n0", "n1", "n2", "n3"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	x, y := activePaneContentScreenOrigin(t, model)

	_, cmd := model.Update(tea.MouseMsg{X: x, Y: y + 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drainCmd(t, model, cmd, 20)
	if !model.copyMode.MouseSelecting {
		t.Fatal("expected mouse copy selection to start")
	}

	_, cmd = model.Update(tea.MouseMsg{X: x, Y: y - 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	drainCmd(t, model, cmd, 20)
	seq := model.copyMode.AutoScrollSeq
	if model.copyMode.AutoScrollDir != -1 {
		t.Fatalf("expected auto-scroll dir -1, got %d", model.copyMode.AutoScrollDir)
	}

	beforeOffset := model.runtime.PaneViewportOffset(model.copyMode.PaneID)
	beforeRow := model.copyMode.Cursor.Row

	_, cmd = model.Update(copyModeAutoScrollMsg{seq: seq})
	drainCmd(t, model, cmd, 20)

	if got := model.runtime.PaneViewportOffset(model.copyMode.PaneID); got <= beforeOffset {
		t.Fatalf("expected pane viewport to increase after auto-scroll, before=%d after=%d", beforeOffset, got)
	}
	if model.copyMode.Cursor.Row >= beforeRow {
		t.Fatalf("expected copy cursor to move upward during auto-scroll, before=%d after=%d", beforeRow, model.copyMode.Cursor.Row)
	}

	_, cmd = model.Update(tea.MouseMsg{X: x, Y: y - 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	drainCmd(t, model, cmd, 20)
	if model.copyMode.MouseSelecting {
		t.Fatal("expected mouse copy selection to stop on release")
	}
	afterReleaseRow := model.copyMode.Cursor.Row
	_, cmd = model.Update(copyModeAutoScrollMsg{seq: seq})
	drainCmd(t, model, cmd, 20)
	if model.copyMode.Cursor.Row != afterReleaseRow {
		t.Fatalf("expected stale auto-scroll tick to stop after release, before=%d after=%d", afterReleaseRow, model.copyMode.Cursor.Row)
	}
}

func TestCopyModeAutoScrollStopsAfterMouseActivitySeqChanges(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"s0", "s1", "s2", "s3", "s4", "s5"}, []string{"n0", "n1", "n2", "n3"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	x, y := activePaneContentScreenOrigin(t, model)

	_, cmd := model.Update(tea.MouseMsg{X: x, Y: y + 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drainCmd(t, model, cmd, 20)
	_, cmd = model.Update(tea.MouseMsg{X: x, Y: y - 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	drainCmd(t, model, cmd, 20)

	seq := model.copyMode.AutoScrollSeq
	before := model.copyMode.Cursor.Row
	model.noteCopyModeMouseActivity()
	_, cmd = model.Update(copyModeAutoScrollMsg{seq: seq})
	drainCmd(t, model, cmd, 20)
	if model.copyMode.Cursor.Row != before {
		t.Fatalf("expected stale auto-scroll tick canceled after activity seq change, before=%d after=%d", before, model.copyMode.Cursor.Row)
	}
}

func TestCopyModeAutoScrollStopsAfterBoundaryCancelKey(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"s0", "s1", "s2", "s3", "s4", "s5"}, []string{"n0", "n1", "n2", "n3"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	x, y := activePaneContentScreenOrigin(t, model)

	_, cmd := model.Update(tea.MouseMsg{X: x, Y: y + 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	drainCmd(t, model, cmd, 20)
	_, cmd = model.Update(tea.MouseMsg{X: x, Y: y - 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	drainCmd(t, model, cmd, 20)

	seq := model.copyMode.AutoScrollSeq
	dispatchKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlG})
	if model.copyMode.PaneID != "" {
		t.Fatalf("expected cancel key to leave copy mode, got %#v", model.copyMode)
	}

	if cmd := model.handleCopyModeAutoScroll(seq); cmd != nil {
		t.Fatalf("expected stale auto-scroll tick canceled after boundary cancel key, got %#v", cmd)
	}
}

func TestCopyModeExtendsFrozenScrollbackWhenSnapshotLoads(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"old2", "old3"}, []string{"line0", "line1", "line2", "line3"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeBeginSelection})

	beforeMark := *model.copyMode.Mark
	beforeCursorRef := model.copyMode.CursorRowRef
	beforeMarkRef := model.copyMode.MarkRowRef
	beforeSnapshot := model.copyMode.Snapshot
	beforeScreen := copyModeSnapshotScreenText(beforeSnapshot)

	seedCopyModeSnapshot(t, model, []string{"old0", "old1", "old2", "old3"}, []string{"live0", "live1", "live2", "live3"})
	loaded, err := model.runtime.LoadSnapshot(context.Background(), "term-1", 0, 0)
	if err != nil {
		t.Fatalf("load updated snapshot: %v", err)
	}
	_, cmd := model.Update(orchestrator.SnapshotLoadedMsg{TerminalID: "term-1", Snapshot: loaded})
	drainCmd(t, model, cmd, 20)

	if model.copyMode.Snapshot == beforeSnapshot {
		t.Fatal("expected copy mode to extend frozen snapshot with loaded scrollback")
	}
	if got := copyModeSnapshotScreenText(model.copyMode.Snapshot); got != beforeScreen {
		t.Fatalf("expected frozen screen to stay unchanged, before=%q after=%q", beforeScreen, got)
	}
	if got := rowTextFromCompactRow(model.copyMode.Snapshot.Scrollback[0]); !strings.Contains(got, "old0") {
		t.Fatalf("expected loaded older scrollback to be prepended, got %q", got)
	}
	if got := model.copyMode.CursorRowRef; !got.Valid || got != beforeCursorRef {
		t.Fatalf("expected canonical cursor ref to anchor across prepend, before=%#v after=%#v", beforeCursorRef, got)
	}
	if got, ok := (copyModeBuffer{snapshot: model.copyMode.Snapshot, height: 4}).rowForRef(beforeCursorRef); ok && model.copyMode.Cursor.Row != got {
		t.Fatalf("expected copy-mode cursor row to follow canonical ref row %d, got %d", got, model.copyMode.Cursor.Row)
	}
	if model.copyMode.Mark == nil {
		t.Fatal("expected mark to remain set")
	}
	if got := model.copyMode.MarkRowRef; !got.Valid || got != beforeMarkRef {
		t.Fatalf("expected canonical mark ref to anchor across prepend, before=%#v after=%#v", beforeMarkRef, got)
	}
	if got, ok := (copyModeBuffer{snapshot: model.copyMode.Snapshot, height: 4}).rowForRef(beforeMarkRef); ok && model.copyMode.Mark.Row != got {
		t.Fatalf("expected copy-mode mark row to follow canonical ref row %d, before=%d after=%d", got, beforeMark.Row, model.copyMode.Mark.Row)
	}
}

func TestCopyModeLatestRefreshAcceptsShorterMaterializationWithMoreCommittedOwnership(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: 40, Rows: 8},
		Scrollback: protocol.CompactRowsFromCells([][]protocol.Cell{
			protocolRowFromText("canon-00002", 40),
			protocolRowFromText("canon-00003", 40),
			protocolRowFromText("tail-live ", 40),
		}),
		ScrollbackOwnership:    []string{protocol.RowOwnershipPersisted, protocol.RowOwnershipPersisted, protocol.RowOwnershipLiveTailLive},
		ScrollbackTotal:        4,
		ScrollbackLogicalTotal: 4,
		ScrollbackLoadedRows:   2,
		HistoryGeneration:      7,
		ScrollbackFirstRowID:   2,
		ScrollbackLastRowID:    3,
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			protocolRowFromText("live0", 40),
		}},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	model.copyMode.Cursor = copyModePoint{Row: 1, Col: 0}
	buffer, ok := model.activeCopyModeBuffer()
	if !ok {
		t.Fatal("expected active copy-mode buffer")
	}
	model.copyMode.CursorRowRef = buffer.pointRowRef(model.copyMode.Cursor)
	beforeCursorRef := model.copyMode.CursorRowRef
	model.saveCurrentCopyModeState()

	loaded := cloneSnapshot(terminal.Snapshot)
	loaded.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{
		protocolRowFromText("canon-00003", 40),
		protocolRowFromText("canon-00004", 40),
	})
	loaded.ScrollbackOwnership = []string{protocol.RowOwnershipPersisted, protocol.RowOwnershipPersisted}
	loaded.ScrollbackOffset = 3
	loaded.ScrollbackTotal = 5
	loaded.ScrollbackLogicalTotal = 5
	loaded.ScrollbackLoadedRows = 5
	loaded.HistoryGeneration = 7
	loaded.ScrollbackFirstRowID = 3
	loaded.ScrollbackLastRowID = 4

	_, cmd := model.Update(orchestrator.SnapshotLoadedMsg{TerminalID: "term-1", Snapshot: loaded})
	drainCmd(t, model, cmd, 20)

	if got, want := model.copyMode.CommittedLoadedRows, 5; got != want {
		t.Fatalf("expected shorter materialization with more committed ownership to advance depth, got %d want %d", got, want)
	}
	if got, want := len(model.copyMode.Snapshot.Scrollback), 2; got != want {
		t.Fatalf("expected frozen snapshot to accept shorter committed materialization, got %d want %d", got, want)
	}
	if got := model.copyMode.CursorRowRef; got != beforeCursorRef {
		t.Fatalf("expected cursor to stay anchored to previous canonical row ref, before=%#v after=%#v", beforeCursorRef, got)
	}
	if got, ok := (copyModeBuffer{snapshot: model.copyMode.Snapshot, height: 4}).rowForRef(beforeCursorRef); !ok || model.copyMode.Cursor.Row != got {
		t.Fatalf("expected cursor row to resolve through ownership row ref, resolved=%d ok=%v cursor=%d", got, ok, model.copyMode.Cursor.Row)
	}
}

func TestCopyModeTopLoadsOlderScrollbackIntoFrozenBuffer(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, nil, []string{"line0", "line1", "line2", "line3"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	beforeScreen := copyModeSnapshotScreenText(model.copyMode.Snapshot)
	client := model.runtime.Client().(*recordingBridgeClient)
	beforeSnapshotCalls := len(client.snapshotCalls)
	beforeViewportCalls := len(client.viewportRequests)

	seedCopyModeSnapshot(t, model, []string{"old0", "old1", "old2", "old3"}, []string{"live0", "live1", "live2", "live3"})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})

	if got := len(client.snapshotCalls); got != beforeSnapshotCalls {
		t.Fatalf("expected copy-mode top to avoid snapshot history requests, before=%d after=%d calls=%#v", beforeSnapshotCalls, got, client.snapshotCalls)
	}
	if got := len(client.viewportRequests); got != beforeViewportCalls+1 {
		t.Fatalf("expected copy-mode top to request one history viewport, before=%d after=%d calls=%#v", beforeViewportCalls, got, client.viewportRequests)
	}
	if model.copyMode.Snapshot == nil {
		t.Fatal("expected copy-mode snapshot")
	}
	if got := copyModeSnapshotScreenText(model.copyMode.Snapshot); got != beforeScreen {
		t.Fatalf("expected frozen screen to remain unchanged after history load, before=%q after=%q", beforeScreen, got)
	}
	if got, want := len(model.copyMode.Snapshot.Scrollback), 4; got != want {
		t.Fatalf("expected older scrollback to be loaded into frozen buffer, got %d want %d", got, want)
	}
	if got := rowTextFromCompactRow(model.copyMode.Snapshot.Scrollback[0]); got != "old0" {
		t.Fatalf("expected oldest loaded row at top, got %q", got)
	}
	if got := model.copyMode.CursorLogical.Line; got != 0 {
		t.Fatalf("expected cursor logical line to stay on the same logical content after prepending history, got line %d", got)
	}
	if got := model.copyMode.CursorLogical.Offset; got != 0 {
		t.Fatalf("expected cursor logical offset preserved after prepending history, got offset %d", got)
	}
}

func TestCopyModeEnterPrefetchesHistoryWhenFrozenBufferHasNoScrollback(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, nil, []string{"line0", "line1", "line2", "line3"})
	client := model.runtime.Client().(*recordingBridgeClient)
	client.snapshotByTerminal["term-1"] = copyModeTestSnapshot([]string{"old0", "old1", "old2"}, []string{"live0", "live1", "live2"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})

	if got := len(client.viewportRequests); got != 1 {
		t.Fatalf("expected copy-mode enter to request initial history viewport, got %#v", client.viewportRequests)
	}
	request := client.viewportRequests[0]
	if request.terminalID != "term-1" || request.offset != 0 || request.limit != terminalHistoryInitialPageLimit || request.cols <= 0 {
		t.Fatalf("expected initial copy-mode history viewport request, got %#v", request)
	}
	if model.copyMode.Snapshot == nil {
		t.Fatal("expected copy-mode snapshot")
	}
	if got, want := len(model.copyMode.Snapshot.Scrollback), 3; got != want {
		t.Fatalf("expected initial history to be loaded into frozen buffer, got %d want %d", got, want)
	}
	if got := rowTextFromCompactRow(model.copyMode.Snapshot.Scrollback[0]); got != "old0" {
		t.Fatalf("expected loaded history at top, got %q", got)
	}
}

func TestCopyModeTopLiveTailOnlyLatestDoesNotAdvanceOlderRequestOffsetByLiveTailRows(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 40, Rows: 8},
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromText("tail0", 40), protocolRowFromText("tail1", 40)}),
		ScrollbackTotal:        4,
		ScrollbackLogicalTotal: 4,
		ScrollbackOwnership:    []string{protocol.RowOwnershipLiveTailLive, protocol.RowOwnershipLiveTailLive},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			protocolRowFromText("line0", 40),
			protocolRowFromText("line1", 40),
			protocolRowFromText("line2", 40),
			protocolRowFromText("line3", 40),
		}},
		Cursor: protocol.CursorState{Row: 3, Col: 0, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	}
	client := model.runtime.Client().(*recordingBridgeClient)
	client.snapshotByTerminal["term-1"] = cloneSnapshot(terminal.Snapshot)

	if _, err := model.runtime.LoadSnapshot(context.Background(), "term-1", 0, 0); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	terminal = model.runtime.Registry().Get("term-1")
	if terminal == nil || terminal.VTerm == nil {
		t.Fatalf("expected terminal with vterm, got %#v", terminal)
	}

	client.snapshotByTerminal["term-1"] = &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   terminal.Snapshot.Size,
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromText("old0", 40), protocolRowFromText("old1", 40)}),
		ScrollbackOffset:       0,
		ScrollbackTotal:        4,
		ScrollbackLogicalTotal: 4,
		ScrollbackLoadedRows:   2,
		HistoryGeneration:      0,
		ScrollbackHasMore:      true,
		Screen:                 protocol.ScreenData{Cells: cloneProtocolRows(terminal.Snapshot.Screen.Cells)},
		Cursor:                 terminal.Snapshot.Cursor,
		Modes:                  terminal.Snapshot.Modes,
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	beforeViewportCalls := len(client.viewportRequests)
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})

	if got := len(client.viewportRequests); got != beforeViewportCalls+1 {
		t.Fatalf("expected one history viewport request, before=%d after=%d calls=%#v", beforeViewportCalls, got, client.viewportRequests)
	}
	request := client.viewportRequests[len(client.viewportRequests)-1]
	if request.offset != 0 {
		t.Fatalf("expected live-tail-only latest snapshot to keep older-request offset at 0, got %#v", request)
	}
}

func TestCopyModeTopAfterActiveLiveRefreshKeepsLiveTailOwnershipOlderOffsetAtZero(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 40, Rows: 8},
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromText("tail0", 40), protocolRowFromText("tail1", 40)}),
		ScrollbackTotal:        4,
		ScrollbackLogicalTotal: 4,
		ScrollbackOwnership:    []string{protocol.RowOwnershipLiveTailLive, protocol.RowOwnershipLiveTailLive},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			protocolRowFromText("line0", 40),
			protocolRowFromText("line1", 40),
			protocolRowFromText("line2", 40),
			protocolRowFromText("line3", 40),
		}},
		Cursor: protocol.CursorState{Row: 3, Col: 0, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	}
	client := model.runtime.Client().(*recordingBridgeClient)
	client.snapshotByTerminal["term-1"] = cloneSnapshot(terminal.Snapshot)

	if _, err := model.runtime.LoadSnapshot(context.Background(), "term-1", 0, 0); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	terminal = model.runtime.Registry().Get("term-1")
	if terminal == nil || terminal.VTerm == nil || terminal.Snapshot == nil {
		t.Fatalf("expected terminal with vterm and snapshot, got %#v", terminal)
	}

	client.snapshotByTerminal["term-1"] = &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   terminal.Snapshot.Size,
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromText("old0", 40), protocolRowFromText("old1", 40)}),
		ScrollbackOffset:       0,
		ScrollbackTotal:        4,
		ScrollbackLogicalTotal: 4,
		ScrollbackLoadedRows:   2,
		HistoryGeneration:      0,
		ScrollbackHasMore:      true,
		Screen:                 protocol.ScreenData{Cells: cloneProtocolRows(terminal.Snapshot.Screen.Cells)},
		Cursor:                 terminal.Snapshot.Cursor,
		Modes:                  terminal.Snapshot.Modes,
	}
	if _, err := terminal.VTerm.Write([]byte("\r\nfresh-tail")); err != nil {
		t.Fatalf("write fresh live tail: %v", err)
	}
	terminal.SurfaceVersion++

	buffer, ok := model.activeLiveCopyModeBuffer()
	if !ok {
		t.Fatal("expected refreshed live copy-mode buffer")
	}
	if buffer.snapshot == nil {
		t.Fatal("expected refreshed snapshot")
	}
	if !protocol.HasOnlyLiveTailLiveOwnership(buffer.snapshot.ScrollbackOwnership, len(buffer.snapshot.Scrollback)) {
		t.Fatalf("expected refreshed live buffer to preserve live-tail ownership, got %#v", buffer.snapshot)
	}
	if got := snapshotScrollbackLoadedDepth(buffer.snapshot); got != 0 {
		t.Fatalf("expected refreshed live buffer committed depth 0, got %d", got)
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	beforeViewportCalls := len(client.viewportRequests)
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})

	if got := len(client.viewportRequests); got != beforeViewportCalls+1 {
		t.Fatalf("expected one history viewport request after top, before=%d after=%d calls=%#v", beforeViewportCalls, got, client.viewportRequests)
	}
	request := client.viewportRequests[len(client.viewportRequests)-1]
	if request.offset != 0 {
		t.Fatalf("expected refreshed live-tail-only latest snapshot to keep older-request offset at 0, got %#v", request)
	}
}

func TestCopyModeHistoryRequestUsesCanonicalCols(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, nil, []string{"line0"})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot.Size = protocol.Size{Cols: 96, Rows: 24}
	client := model.runtime.Client().(*recordingBridgeClient)
	client.snapshotByTerminal["term-1"] = copyModeTestSnapshot([]string{"old0"}, []string{"live0"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})

	if got := len(client.viewportRequests); got != 1 {
		t.Fatalf("expected one copy-mode history request, got %#v", client.viewportRequests)
	}
	if got := client.viewportRequests[0].cols; got != 96 {
		t.Fatalf("expected copy-mode history request to use canonical cols 96, got %d", got)
	}
}

func TestCopyModeEnterPrefetchesWhenExhaustedFlagIsStale(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, nil, []string{"line0", "line1", "line2", "line3"})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.CommittedHistoryExhausted = true
	terminal.CommittedLoadedDepth = 3
	terminal.Snapshot.ScrollbackTotal = 1
	terminal.Snapshot.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromText("known0", 40)})
	terminal.Snapshot.ScrollbackLoadedRows = 1
	terminal.Snapshot.HistoryGeneration = 7
	terminal.Snapshot.ScrollbackFirstRowID = 0
	terminal.Snapshot.ScrollbackLastRowID = 0
	terminal.Snapshot.ScrollbackOwnership = []string{protocol.RowOwnershipPersisted}
	client := model.runtime.Client().(*recordingBridgeClient)
	client.snapshotByTerminal["term-1"] = copyModeTestSnapshot([]string{"old0", "old1", "old2"}, []string{"live0", "live1", "live2"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})

	if got := len(client.viewportRequests); got != 1 {
		t.Fatalf("expected stale exhausted flag not to suppress copy-mode history load, got %#v", client.viewportRequests)
	}
	if got := client.viewportRequests[0]; got.offset != 1 || got.limit <= 0 {
		t.Fatalf("expected stale-exhausted ownership history request to continue from committed depth 1, got %#v", got)
	}
	if terminal.CommittedHistoryExhausted {
		t.Fatal("expected ownership-aware stale exhausted flag to clear before loading older history")
	}
}

func TestCopyModeEnterDoesNotPrefetchLiveTailOwnershipRowsWhenExhausted(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 40, Rows: 8},
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromText("tail0", 40), protocolRowFromText("tail1", 40)}),
		ScrollbackTotal:        2,
		ScrollbackLogicalTotal: 2,
		ScrollbackLoadedRows:   0,
		HistoryGeneration:      0,
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			protocolRowFromText("line0", 40),
			protocolRowFromText("line1", 40),
			protocolRowFromText("line2", 40),
			protocolRowFromText("line3", 40),
		}},
		Cursor: protocol.CursorState{Row: 3, Col: 0, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	}
	terminal.CommittedHistoryExhausted = true
	client := model.runtime.Client().(*recordingBridgeClient)

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})

	if got := len(client.viewportRequests); got != 0 {
		t.Fatalf("expected live-tail ownership rows not to trigger copy-mode history prefetch, got %#v", client.viewportRequests)
	}
	if !terminal.CommittedHistoryExhausted {
		t.Fatal("expected exhausted flag to stay set when only live-tail ownership rows are known")
	}
	if model.copyMode.Snapshot == nil {
		t.Fatal("expected copy-mode snapshot")
	}
	if got, want := len(model.copyMode.Snapshot.Scrollback), 2; got != want {
		t.Fatalf("expected copy mode to keep live-tail visual rows without loading older history, got %d want %d", got, want)
	}
}

func TestCopyModeTopDoesNotLoadNormalHistoryWhileAlternateScreenActive(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, nil, []string{"alt0", "alt1", "alt2", "alt3"})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot.Modes.AlternateScreen = true
	terminal.Snapshot.Screen.IsAlternateScreen = true
	terminal.Snapshot.Scrollback = []protocol.CompactRow{
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "old-normal", Width: 1}}),
	}
	client := model.runtime.Client().(*recordingBridgeClient)
	client.snapshotByTerminal["term-1"] = copyModeTestSnapshot([]string{"old0", "old1"}, []string{"live0", "live1"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})

	if got := len(client.viewportRequests); got != 0 {
		t.Fatalf("expected alternate-screen copy mode not to request normal history, got %#v", client.viewportRequests)
	}
	if got := len(model.copyMode.Snapshot.Scrollback); got != 0 {
		t.Fatalf("expected frozen alternate screen to omit normal scrollback, got %d rows", got)
	}
	if !model.copyMode.Snapshot.Modes.AlternateScreen || !model.copyMode.Snapshot.Screen.IsAlternateScreen {
		t.Fatalf("expected frozen alternate screen to remain marked alternate, got modes=%#v screen=%v", model.copyMode.Snapshot.Modes, model.copyMode.Snapshot.Screen.IsAlternateScreen)
	}
}

func TestCopyModeUsesAlternateScreenVisualHistory(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, nil, []string{"alt1", "alt2", "alt3", "alt4"})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot.Modes.AlternateScreen = true
	terminal.Snapshot.Screen.IsAlternateScreen = true
	terminal.AlternateScrollback = []protocol.CompactRow{
		protocol.CompactRowFromCells([]protocol.Cell{{Content: "c", Width: 1}, {Content: "o", Width: 1}, {Content: "d", Width: 1}, {Content: "e", Width: 1}, {Content: "x", Width: 1}, {Content: "-", Width: 1}, {Content: "0", Width: 1}}),
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})

	if got, want := len(model.copyMode.Snapshot.Scrollback), 1; got != want {
		t.Fatalf("expected alternate visual history in frozen copy buffer, got %d want %d", got, want)
	}
	if got := rowTextFromCompactRow(model.copyMode.Snapshot.Scrollback[0]); got != "codex-0" {
		t.Fatalf("expected alternate visual history row, got %q", got)
	}
	if got := model.copyMode.Cursor.Row; got != 0 {
		t.Fatalf("expected copy-mode top to reach alternate visual history, got row %d", got)
	}
}

func TestCopyModeLoadsOlderScrollbackByOffsetPage(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	latest := make([]string, 500)
	allRows := make([]string, 1000)
	for i := range allRows {
		allRows[i] = "hist"
		if i < 500 {
			allRows[i] = "old"
		}
	}
	for i := range latest {
		latest[i] = allRows[i+500]
	}
	seedCopyModeSnapshot(t, model, latest, []string{"live0", "live1", "live2", "live3"})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot.ScrollbackTotal = len(allRows)
	terminal.Snapshot.ScrollbackLogicalTotal = len(allRows)
	terminal.Snapshot.ScrollbackLoadedRows = len(latest)
	terminal.Snapshot.HistoryGeneration = 1
	terminal.Snapshot.ScrollbackFirstRowID = 1500
	terminal.Snapshot.ScrollbackLastRowID = 1999

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	client := model.runtime.Client().(*recordingBridgeClient)
	beforeSnapshotCalls := len(client.snapshotRequests)
	beforeViewportCalls := len(client.viewportRequests)

	serverSnapshot := copyModeTestSnapshot(allRows, []string{"next0", "next1", "next2", "next3"})
	serverSnapshot.HistoryGeneration = 1
	client.snapshotByTerminal["term-1"] = serverSnapshot
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})

	if got := len(client.snapshotRequests); got != beforeSnapshotCalls {
		t.Fatalf("expected paged history to avoid snapshot requests, before=%d after=%d calls=%#v", beforeSnapshotCalls, got, client.snapshotRequests)
	}
	if got := len(client.viewportRequests); got != beforeViewportCalls+1 {
		t.Fatalf("expected one paged viewport request, before=%d after=%d calls=%#v", beforeViewportCalls, got, client.viewportRequests)
	}
	request := client.viewportRequests[len(client.viewportRequests)-1]
	if request.terminalID != "term-1" || request.offset != 500 || request.limit != 500 || request.cols <= 0 {
		t.Fatalf("expected offset viewport request for older copy-mode history, got %#v", request)
	}
	if got, want := len(model.copyMode.Snapshot.Scrollback), 1000; got != want {
		t.Fatalf("expected paged history to be prepended, got %d rows want %d", got, want)
	}
	if got := rowTextFromCompactRow(model.copyMode.Snapshot.Scrollback[0]); !strings.Contains(got, "old") {
		t.Fatalf("expected older page at top of frozen scrollback, got %q", got)
	}
	if got := rowTextFromCompactRow(model.runtime.Registry().Get("term-1").Snapshot.Scrollback[0]); !strings.Contains(got, "hist") {
		t.Fatalf("expected live runtime snapshot to stay on latest page, got %q", got)
	}
}

func TestCopyModeOlderPageOffsetUsesCommittedStoreDepthNotReflowedRows(t *testing.T) {
	model := setupModel(t, modelOpts{width: 60, height: 10})
	terminal := model.runtime.Registry().GetOrCreate("term-1")
	scrollback := make([][]protocol.Cell, 0, 150)
	ownership := make([]string, 0, 150)
	wrapped := make([]bool, 0, 150)
	for i := 0; i < 100; i++ {
		scrollback = append(
			scrollback,
			protocolRowFromText(fmt.Sprintf("hist-%03d-a", i), 60),
			protocolRowFromText(fmt.Sprintf("hist-%03d-b", i), 60),
		)
		ownership = append(ownership, protocol.RowOwnershipPersisted, protocol.RowOwnershipPersisted)
		wrapped = append(wrapped, true, false)
		if i%2 == 0 {
			continue
		}
		scrollback = append(scrollback, protocolRowFromText(fmt.Sprintf("hist-%03d-c", i), 60))
		ownership = append(ownership, protocol.RowOwnershipPersisted)
		wrapped = append(wrapped, false)
	}
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 60, Rows: 10},
		Scrollback:             protocol.CompactRowsFromCells(scrollback),
		ScrollbackOwnership:    ownership,
		ScrollbackWrapped:      wrapped,
		ScrollbackTotal:        100,
		ScrollbackLogicalTotal: 100,
		ScrollbackLoadedRows:   100,
		HistoryGeneration:      9,
		ScrollbackFirstRowID:   0,
		ScrollbackLastRowID:    99,
		ScrollbackHasMore:      true,
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			protocolRowFromText("live0", 60),
		}},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	}
	client := model.runtime.Client().(*recordingBridgeClient)
	client.snapshotByTerminal = map[string]*protocol.Snapshot{}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	if got, want := model.copyMode.CommittedLoadedRows, 100; got != want {
		t.Fatalf("expected copy-mode committed depth %d from explicit loaded rows, got %d", want, got)
	}
	buffer, ok := model.activeCopyModeBuffer()
	if !ok {
		t.Fatal("expected active copy-mode buffer")
	}
	if got, want := snapshotScrollbackLoadedDepth(buffer.snapshot), 100; got != want {
		t.Fatalf("expected buffer loaded depth %d despite %d materialized projection rows, got %d", want, len(buffer.snapshot.Scrollback), got)
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})

	if got := len(client.viewportRequests); got != 1 {
		t.Fatalf("expected one older-page request, got %#v", client.viewportRequests)
	}
	request := client.viewportRequests[0]
	if request.offset != 100 {
		t.Fatalf("expected older-page offset to use committed store depth 100, not reflowed row count; got %#v", request)
	}
	if request.limit != terminalScrollbackPageLimit {
		t.Fatalf("expected standard page limit, got %#v", request)
	}
}

func TestCopyModePagedLatestReplaceTrimsScreenOverlap(t *testing.T) {
	model := setupModel(t, modelOpts{width: 16, height: 6})
	seedCopyModeSnapshot(t, model, []string{"line-075"}, []string{"line-078", "line-079", "line-080", "prompt"})
	terminal := model.runtime.Registry().Get("term-1")

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	loaded := &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       terminal.Snapshot.Size,
		Scrollback: protocol.CompactRowsFromCells([][]protocol.Cell{
			protocolRowFromText("line-076", 16),
			protocolRowFromText("line-077", 16),
			protocolRowFromText("line-078", 16),
			protocolRowFromText("line-079", 16),
			protocolRowFromText("line-080", 16),
		}),
		ScrollbackOwnership:    repeatedOwnership(protocol.RowOwnershipPersisted, 5),
		ScrollbackOffset:       0,
		ScrollbackTotal:        81,
		ScrollbackLogicalTotal: 81,
		ScrollbackLoadedRows:   81,
		HistoryGeneration:      7,
		ScrollbackFirstRowID:   0,
		ScrollbackLastRowID:    80,
		ScrollbackHasMore:      false,
		Screen:                 protocol.ScreenData{Cells: cloneProtocolRows(terminal.Snapshot.Screen.Cells)},
		Cursor:                 terminal.Snapshot.Cursor,
		Modes:                  terminal.Snapshot.Modes,
	}

	if !model.extendFrozenCopyModeSnapshot(loaded, 0, true) {
		t.Fatal("expected latest page to replace frozen copy-mode snapshot")
	}
	if gotRows := len(model.copyMode.Snapshot.Scrollback); gotRows != 2 {
		t.Fatalf("expected duplicated screen prefix trimmed from frozen latest scrollback, got %d rows", gotRows)
	}
	if got := rowTextFromCompactRow(model.copyMode.Snapshot.Scrollback[0]); got != "line-076" {
		t.Fatalf("expected first retained row line-076, got %q", got)
	}
	if got := rowTextFromCompactRow(model.copyMode.Snapshot.Scrollback[1]); got != "line-077" {
		t.Fatalf("expected second retained row line-077, got %q", got)
	}
	if got, want := model.copyMode.CommittedLoadedRows, 81; got != want {
		t.Fatalf("expected committed loaded depth to stay %d, got %d", want, got)
	}
	if got, want := model.copyMode.Snapshot.ScrollbackLastRowID, uint64(80); got != want {
		t.Fatalf("expected canonical latest window metadata preserved, got last row id %d", got)
	}
}

func TestCopyModeLoadsOlderScrollbackBeyondFullSnapshotCap(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	latest := make([]string, terminalMaterializedScrollbackLimit)
	allRows := make([]string, terminalMaterializedScrollbackLimit+500)
	for i := range allRows {
		allRows[i] = "old"
		if i >= 500 {
			allRows[i] = "hist"
		}
	}
	copy(latest, allRows[500:])
	seedCopyModeSnapshot(t, model, latest, []string{"live0", "live1", "live2", "live3"})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot.ScrollbackTotal = len(allRows)
	terminal.Snapshot.ScrollbackLogicalTotal = len(allRows)
	terminal.Snapshot.ScrollbackLoadedRows = len(latest)
	terminal.Snapshot.HistoryGeneration = 1
	terminal.Snapshot.ScrollbackFirstRowID = 1500
	terminal.Snapshot.ScrollbackLastRowID = 13499

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	client := model.runtime.Client().(*recordingBridgeClient)
	beforeSnapshotCalls := len(client.snapshotRequests)
	beforeViewportCalls := len(client.viewportRequests)
	beforeCursorRef := model.copyMode.CursorRowRef

	serverSnapshot := copyModeTestSnapshot(allRows, []string{"next0", "next1", "next2", "next3"})
	serverSnapshot.HistoryGeneration = 1
	client.snapshotByTerminal["term-1"] = serverSnapshot
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})

	if got := len(client.snapshotRequests); got != beforeSnapshotCalls {
		t.Fatalf("expected paged history to avoid snapshot requests beyond full snapshot cap, before=%d after=%d calls=%#v", beforeSnapshotCalls, got, client.snapshotRequests)
	}
	if got := len(client.viewportRequests); got != beforeViewportCalls+1 {
		t.Fatalf("expected one paged viewport request beyond full snapshot cap, before=%d after=%d calls=%#v", beforeViewportCalls, got, client.viewportRequests)
	}
	request := client.viewportRequests[len(client.viewportRequests)-1]
	if request.terminalID != "term-1" || request.offset != terminalMaterializedScrollbackLimit || request.limit != 500 || request.cols <= 0 {
		t.Fatalf("expected offset viewport request beyond full snapshot cap, got %#v", request)
	}
	if got, want := len(model.copyMode.Snapshot.Scrollback), terminalMaterializedScrollbackLimit; got != want {
		t.Fatalf("expected paged history window to stay bounded, got %d rows want %d", got, want)
	}
	if got := rowTextFromCompactRow(model.copyMode.Snapshot.Scrollback[0]); !strings.Contains(got, "old") {
		t.Fatalf("expected older page at top of frozen scrollback, got %q", got)
	}
	if got := model.copyMode.Snapshot.ScrollbackOffset; got != 500 {
		t.Fatalf("expected newest materialized rows to be trimmed from copy buffer, offset=%d", got)
	}
	if got := model.copyMode.CommittedLoadedRows; got != len(allRows) {
		t.Fatalf("expected committed depth to keep logical pagination progress, got %d want %d", got, len(allRows))
	}
	if got, want := model.copyMode.Snapshot.ScrollbackFirstRowID, uint64(1000); got != want {
		t.Fatalf("expected bounded frozen buffer to keep loaded committed window first row id, got %d want %d", got, want)
	}
	if got, want := model.copyMode.Snapshot.ScrollbackLastRowID, uint64(13499); got != want {
		t.Fatalf("expected bounded frozen buffer to keep loaded committed window last row id, got %d want %d", got, want)
	}
	if !model.copyMode.CursorRowRef.Valid {
		t.Fatalf("expected bounded trim to keep a valid canonical cursor ref, got %#v", model.copyMode.CursorRowRef)
	}
	if beforeCursorRef.Valid && model.copyMode.CursorRowRef != beforeCursorRef {
		t.Fatalf("expected bounded trim to preserve canonical cursor ref, before=%#v after=%#v", beforeCursorRef, model.copyMode.CursorRowRef)
	}
}

func TestCopyModeBoundedWindowRequestsNextOlderPageByLoadedDepth(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	latest := make([]string, terminalMaterializedScrollbackLimit)
	allRows := make([]string, terminalMaterializedScrollbackLimit+1000)
	for i := range allRows {
		allRows[i] = "old"
		if i >= 1000 {
			allRows[i] = "hist"
		}
	}
	copy(latest, allRows[1000:])
	seedCopyModeSnapshot(t, model, latest, []string{"live0", "live1", "live2", "live3"})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot.ScrollbackTotal = len(allRows)
	terminal.Snapshot.ScrollbackLogicalTotal = len(allRows)
	terminal.Snapshot.ScrollbackLoadedRows = len(latest)
	terminal.Snapshot.HistoryGeneration = 1
	terminal.Snapshot.ScrollbackFirstRowID = 2000
	terminal.Snapshot.ScrollbackLastRowID = 13999

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	client := model.runtime.Client().(*recordingBridgeClient)
	serverSnapshot := copyModeTestSnapshot(allRows, []string{"next0", "next1", "next2", "next3"})
	serverSnapshot.HistoryGeneration = 1
	client.snapshotByTerminal["term-1"] = serverSnapshot
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})

	if got, want := model.copyMode.CommittedLoadedRows, terminalMaterializedScrollbackLimit+500; got != want {
		t.Fatalf("expected first older page to advance committed depth, got %d want %d", got, want)
	}
	if got, want := len(model.copyMode.Snapshot.Scrollback), terminalMaterializedScrollbackLimit; got != want {
		t.Fatalf("expected first page to keep bounded copy buffer, got %d want %d", got, want)
	}
	if got := model.copyMode.Snapshot.ScrollbackOffset; got != 500 {
		t.Fatalf("expected first page to trim newest rows, offset=%d", got)
	}

	beforeViewportCalls := len(client.viewportRequests)
	model.copyMode.Cursor.Row = 0
	model.copyMode.ViewTopRow = 0
	buffer, ok := model.activeCopyModeBuffer()
	if !ok {
		t.Fatal("expected active copy mode buffer")
	}
	drainCmd(t, model, model.prefetchCopyModeScrollbackCmd(buffer), 20)

	if got := len(client.viewportRequests); got != beforeViewportCalls+1 {
		t.Fatalf("expected one follow-up viewport request, before=%d after=%d calls=%#v", beforeViewportCalls, got, client.viewportRequests)
	}
	request := client.viewportRequests[len(client.viewportRequests)-1]
	if request.offset != terminalMaterializedScrollbackLimit+500 || request.limit != 500 {
		t.Fatalf("expected next page request to use committed depth, got %#v", request)
	}
	if got, want := model.copyMode.CommittedLoadedRows, len(allRows); got != want {
		t.Fatalf("expected second page to advance committed depth, got %d want %d", got, want)
	}
	if got, want := len(model.copyMode.Snapshot.Scrollback), terminalMaterializedScrollbackLimit; got != want {
		t.Fatalf("expected second page to keep bounded copy buffer, got %d want %d", got, want)
	}
}

func TestCopyModeTopMixedCanonicalLatestAndLiveTailKeepsCommittedOffsetAndPrependsOlderPage(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	scrollback := make([][]protocol.Cell, 0, terminalMaterializedScrollbackLimit+2)
	for i := 1000; i < 13000; i++ {
		scrollback = append(scrollback, protocolRowFromText(fmt.Sprintf("canon-%05d", i), 40))
	}
	scrollback = append(scrollback,
		protocolRowFromText("live-tail-open-0", 40),
		protocolRowFromText("live-tail-open-1", 40),
	)
	ownership := append(
		repeatedOwnership(protocol.RowOwnershipPersisted, terminalMaterializedScrollbackLimit),
		protocol.RowOwnershipLiveTailLive,
		protocol.RowOwnershipLiveTailLive,
	)

	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 40, Rows: 8},
		Scrollback:             protocol.CompactRowsFromCells(scrollback),
		ScrollbackOwnership:    ownership,
		ScrollbackOffset:       0,
		ScrollbackTotal:        13002,
		ScrollbackLogicalTotal: 13002,
		ScrollbackLoadedRows:   12000,
		HistoryGeneration:      10,
		ScrollbackFirstRowID:   1000,
		ScrollbackLastRowID:    12999,
		ScrollbackHasMore:      true,
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			protocolRowFromText("live0", 40),
			protocolRowFromText("live1", 40),
		}},
		Cursor: protocol.CursorState{Row: 1, Col: 0, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	}
	client := model.runtime.Client().(*recordingBridgeClient)
	client.snapshotByTerminal = map[string]*protocol.Snapshot{}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	if got, want := model.copyMode.CommittedLoadedRows, 12000; got != want {
		t.Fatalf("expected frozen copy-mode committed depth %d, got %d", want, got)
	}
	if got, want := len(model.copyMode.Snapshot.Scrollback), terminalMaterializedScrollbackLimit+2; got != want {
		t.Fatalf("expected frozen scrollback to include committed rows plus live tail, got %d want %d", got, want)
	}
	buffer, ok := model.activeCopyModeBuffer()
	if !ok {
		t.Fatal("expected active copy-mode buffer")
	}
	if got := buffer.rowRef(11999); !got.Valid || got.Generation != 10 || got.RowID != 12999 {
		t.Fatalf("expected last committed materialized row to keep canonical ref, got %#v", got)
	}
	if got := buffer.rowRef(12000); got.Valid {
		t.Fatalf("expected first live tail row not to consume committed row ids, got %#v", got)
	}

	beforeViewportCalls := len(client.viewportRequests)
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})

	if got := len(client.viewportRequests); got != beforeViewportCalls+1 {
		t.Fatalf("expected one older-page viewport request, before=%d after=%d calls=%#v", beforeViewportCalls, got, client.viewportRequests)
	}
	request := client.viewportRequests[len(client.viewportRequests)-1]
	if request.terminalID != "term-1" || request.offset != 12000 || request.limit != 500 || request.cols <= 0 {
		t.Fatalf("expected committed-depth viewport request despite live tail, got %#v", request)
	}

	olderPageRows := make([][]protocol.Cell, 0, 500)
	for i := 500; i < 1000; i++ {
		olderPageRows = append(olderPageRows, protocolRowFromText(fmt.Sprintf("canon-%05d", i), 40))
	}
	olderPage := &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   terminal.Snapshot.Size,
		Scrollback:             protocol.CompactRowsFromCells(olderPageRows),
		ScrollbackOwnership:    repeatedOwnership(protocol.RowOwnershipPersisted, len(olderPageRows)),
		ScrollbackOffset:       12000,
		ScrollbackTotal:        13002,
		ScrollbackLogicalTotal: 13002,
		ScrollbackLoadedRows:   12500,
		HistoryGeneration:      10,
		ScrollbackFirstRowID:   500,
		ScrollbackLastRowID:    999,
		ScrollbackHasMore:      true,
		Screen:                 protocol.ScreenData{Cells: cloneProtocolRows(terminal.Snapshot.Screen.Cells)},
		Cursor:                 terminal.Snapshot.Cursor,
		Modes:                  terminal.Snapshot.Modes,
	}

	_, cmd := model.Update(orchestrator.SnapshotLoadedMsg{
		TerminalID:      "term-1",
		Snapshot:        olderPage,
		Offset:          12000,
		Limit:           500,
		Paged:           true,
		CopyModeRequest: true,
	})
	drainCmd(t, model, cmd, 20)

	if got, want := model.copyMode.CommittedLoadedRows, 12500; got != want {
		t.Fatalf("expected older page to advance committed depth to %d, got %d", want, got)
	}
	if got, want := len(model.copyMode.Snapshot.Scrollback), terminalMaterializedScrollbackLimit; got != want {
		t.Fatalf("expected bounded frozen buffer after older-page prepend, got %d want %d", got, want)
	}
	if got, want := model.copyMode.Snapshot.ScrollbackOffset, 500; got != want {
		t.Fatalf("expected bounded prepend to advance committed offset by 500 without counting live-tail rows, got offset=%d want %d", got, want)
	}
	if got := rowTextFromCompactRow(model.copyMode.Snapshot.Scrollback[0]); got != "canon-00500" {
		t.Fatalf("expected prepended older page to start frozen buffer, got %q", got)
	}
	if got := rowTextFromCompactRow(model.copyMode.Snapshot.Scrollback[499]); got != "canon-00999" {
		t.Fatalf("expected prepended page end to remain contiguous at row 499, got %q", got)
	}
	if got, want := model.copyMode.Snapshot.ScrollbackFirstRowID, uint64(500); got != want {
		t.Fatalf("expected merged frozen window first row id %d, got %d", want, got)
	}
	if got, want := model.copyMode.Snapshot.ScrollbackLastRowID, uint64(12999); got != want {
		t.Fatalf("expected merged frozen window last row id %d, got %d", want, got)
	}
	if got := model.copyMode.Cursor.Row; got != 0 || model.copyMode.ViewTopRow != 0 {
		t.Fatalf("expected top-pinned copy mode to stay at row 0 after older-page prepend, cursor=%#v top=%d", model.copyMode.Cursor, model.copyMode.ViewTopRow)
	}

	buffer, ok = model.activeCopyModeBuffer()
	if !ok {
		t.Fatal("expected active copy-mode buffer after older-page merge")
	}
	if got := buffer.rowRef(0); !got.Valid || got.Generation != 10 || got.RowID != 500 {
		t.Fatalf("expected first frozen row ref to match older-page start, got %#v", got)
	}
	if got, ok := buffer.rowForRef(copyModeRowRef{Generation: 10, RowID: 500, Valid: true}); !ok || got != 0 {
		t.Fatalf("expected row ref 500 to anchor at top after prepend, got row=%d ok=%v", got, ok)
	}
	if got, ok := buffer.rowForRef(copyModeRowRef{Generation: 10, RowID: 999, Valid: true}); !ok || got != 499 {
		t.Fatalf("expected row ref 999 to anchor at page end after prepend, got row=%d ok=%v", got, ok)
	}
}

func TestCopyModeTopRepeatedlyLoadsOlderPagesWhenStillAtTop(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	latest := make([]string, terminalMaterializedScrollbackLimit)
	allRows := make([]string, terminalMaterializedScrollbackLimit+1000)
	for i := range allRows {
		allRows[i] = "old"
		if i >= 1000 {
			allRows[i] = "hist"
		}
	}
	copy(latest, allRows[1000:])
	seedCopyModeSnapshot(t, model, latest, []string{"live0", "live1", "live2", "live3"})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot.ScrollbackTotal = len(allRows)
	terminal.Snapshot.ScrollbackLogicalTotal = len(allRows)
	terminal.Snapshot.ScrollbackLoadedRows = len(latest)
	terminal.Snapshot.HistoryGeneration = 1
	terminal.Snapshot.ScrollbackFirstRowID = 2000
	terminal.Snapshot.ScrollbackLastRowID = 13999

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	client := model.runtime.Client().(*recordingBridgeClient)
	serverSnapshot := copyModeTestSnapshot(allRows, []string{"next0", "next1", "next2", "next3"})
	serverSnapshot.HistoryGeneration = 1
	client.snapshotByTerminal["term-1"] = serverSnapshot

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})
	firstCalls := len(client.viewportRequests)
	if firstCalls != 1 {
		t.Fatalf("expected initial top jump to load one older page, got %#v", client.viewportRequests)
	}
	if model.copyMode.Cursor.Row != 0 || model.copyMode.ViewTopRow != 0 {
		t.Fatalf("expected cursor to remain at top after first older page, got cursor=%#v top=%d", model.copyMode.Cursor, model.copyMode.ViewTopRow)
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})
	if got := len(client.viewportRequests); got != firstCalls+1 {
		t.Fatalf("expected repeated top jump at top to load another page, got %#v", client.viewportRequests)
	}
	request := client.viewportRequests[len(client.viewportRequests)-1]
	if request.offset != terminalMaterializedScrollbackLimit+500 || request.limit != 500 {
		t.Fatalf("expected repeated top jump to continue from committed depth, got %#v", request)
	}
	if got, want := model.copyMode.CommittedLoadedRows, len(allRows); got != want {
		t.Fatalf("expected repeated top jump to advance committed depth to full history, got %d want %d", got, want)
	}
	if got := rowTextFromCompactRow(model.copyMode.Snapshot.Scrollback[0]); !strings.Contains(got, "old") {
		t.Fatalf("expected oldest loaded page at scrollback start, got %q", got)
	}
	if got := model.copyMode.ViewTopRow; got != 0 {
		t.Fatalf("expected repeated top jump to keep viewport top at 0, got %d", got)
	}
}

func TestCopyModeRejectsNonAdjacentHistoryPage(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"new0"}, []string{"live0"})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot.ScrollbackLoadedRows = 1
	terminal.Snapshot.HistoryGeneration = 7
	terminal.Snapshot.ScrollbackFirstRowID = 100
	terminal.Snapshot.ScrollbackLastRowID = 100

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	loaded := copyModeTestSnapshot([]string{"old0"}, []string{"live0"})
	loaded.ScrollbackOffset = 1
	loaded.ScrollbackTotal = 2
	loaded.ScrollbackLogicalTotal = 1
	loaded.ScrollbackLoadedRows = 2
	loaded.HistoryGeneration = 7
	loaded.ScrollbackFirstRowID = 98
	loaded.ScrollbackLastRowID = 98

	model.extendFrozenCopyModeSnapshot(loaded, 1, false)
	if got, want := model.copyMode.CommittedLoadedRows, 1; got != want {
		t.Fatalf("expected stale non-adjacent page to be rejected, committed=%d want %d", got, want)
	}
	if got := len(model.copyMode.Snapshot.Scrollback); got != 1 {
		t.Fatalf("expected stale page not to alter frozen scrollback, got %d rows", got)
	}
}

func TestCopyModeRejectsOlderPageWhenCurrentHasNoCanonicalHistoryWindow(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"canon100"}, []string{"live0"})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot.ScrollbackLoadedRows = 1
	terminal.Snapshot.HistoryGeneration = 10
	terminal.Snapshot.ScrollbackFirstRowID = 100
	terminal.Snapshot.ScrollbackLastRowID = 100

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	model.copyMode.Snapshot.Scrollback = protocol.CompactRowsFromCells([][]protocol.Cell{
		protocolRowFromText("tail0", 5),
		protocolRowFromText("tail1", 5),
	})
	model.copyMode.Snapshot.ScrollbackOffset = 0
	model.copyMode.Snapshot.ScrollbackTotal = 2
	model.copyMode.Snapshot.ScrollbackLogicalTotal = 2
	model.copyMode.Snapshot.ScrollbackHasMore = false
	model.copyMode.Snapshot.ScrollbackLoadedRows = 0
	model.copyMode.Snapshot.HistoryGeneration = 0
	model.copyMode.Snapshot.ScrollbackFirstRowID = 0
	model.copyMode.Snapshot.ScrollbackLastRowID = 0
	model.copyMode.CommittedLoadedRows = 0

	loaded := copyModeTestSnapshot([]string{"canon099"}, []string{"live0"})
	loaded.ScrollbackOffset = 1
	loaded.ScrollbackTotal = 2
	loaded.ScrollbackLogicalTotal = 2
	loaded.ScrollbackLoadedRows = 2
	loaded.HistoryGeneration = 10
	loaded.ScrollbackFirstRowID = 99
	loaded.ScrollbackLastRowID = 99

	model.extendFrozenCopyModeSnapshot(loaded, 1, false)
	if got, want := model.copyMode.CommittedLoadedRows, 0; got != want {
		t.Fatalf("expected older page to be rejected when current frozen snapshot has no canonical window, committed=%d want %d", got, want)
	}
	if got := len(model.copyMode.Snapshot.Scrollback); got != 2 {
		t.Fatalf("expected live-tail-only frozen snapshot to remain unchanged, got %d rows", got)
	}
	if got := rowTextFromCompactRow(model.copyMode.Snapshot.Scrollback[0]); got != "tail0" {
		t.Fatalf("expected first frozen row to stay tail0, got %q", got)
	}
	if got := model.copyMode.Snapshot.HistoryGeneration; got != 0 {
		t.Fatalf("expected frozen snapshot to stay non-canonical, got generation=%d", got)
	}
}

func TestCopyModePagedLatestReplaceDropsFrozenCanonicalMetadataAndKeepsOlderOffsetAtZero(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	scrollback := make([][]protocol.Cell, 0, terminalMaterializedScrollbackLimit)
	for i := 0; i < terminalMaterializedScrollbackLimit; i++ {
		scrollback = append(scrollback, protocolRowFromText(fmt.Sprintf("canon-%05d", i), 40))
	}
	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 40, Rows: 8},
		Scrollback:             protocol.CompactRowsFromCells(scrollback),
		ScrollbackOwnership:    repeatedOwnership(protocol.RowOwnershipPersisted, len(scrollback)),
		ScrollbackOffset:       0,
		ScrollbackTotal:        12000,
		ScrollbackLogicalTotal: 12000,
		ScrollbackLoadedRows:   12000,
		HistoryGeneration:      10,
		ScrollbackFirstRowID:   0,
		ScrollbackLastRowID:    11999,
		ScrollbackHasMore:      false,
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			protocolRowFromText("live0", 40),
			protocolRowFromText("live1", 40),
		}},
		Cursor: protocol.CursorState{Row: 1, Col: 0, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	}
	client := model.runtime.Client().(*recordingBridgeClient)

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	if got, want := model.copyMode.CommittedLoadedRows, 12000; got != want {
		t.Fatalf("expected initial frozen snapshot committed depth %d, got %d", want, got)
	}

	latestLiveTailOnly := &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   terminal.Snapshot.Size,
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromText("tail0", 40), protocolRowFromText("tail1", 40)}),
		ScrollbackOffset:       0,
		ScrollbackTotal:        2,
		ScrollbackLogicalTotal: 2,
		ScrollbackOwnership:    []string{protocol.RowOwnershipLiveTailLive, protocol.RowOwnershipLiveTailLive},
		ScrollbackFirstRowID:   0,
		ScrollbackLastRowID:    0,
		ScrollbackHasMore:      false,
		Screen:                 protocol.ScreenData{Cells: cloneProtocolRows(terminal.Snapshot.Screen.Cells)},
		Cursor:                 terminal.Snapshot.Cursor,
		Modes:                  terminal.Snapshot.Modes,
	}

	_, cmd := model.Update(orchestrator.SnapshotLoadedMsg{
		TerminalID:      "term-1",
		Snapshot:        latestLiveTailOnly,
		Offset:          0,
		Limit:           terminalHistoryInitialPageLimit,
		Paged:           true,
		CopyModeRequest: true,
	})
	drainCmd(t, model, cmd, 20)

	if model.copyMode.Snapshot == nil {
		t.Fatal("expected frozen snapshot after paged latest replace")
	}
	if got, want := len(model.copyMode.Snapshot.Scrollback), 2; got != want {
		t.Fatalf("expected live-tail-only latest rows to replace frozen materialization, got %d want %d", got, want)
	}
	if got := rowTextFromCompactRow(model.copyMode.Snapshot.Scrollback[0]); got != "tail0" {
		t.Fatalf("expected replace semantics to keep incoming latest rows, got %q", got)
	}
	if got, want := model.copyMode.Snapshot.ScrollbackLoadedRows, 0; got != want {
		t.Fatalf("expected replace semantics to drop old loaded rows, got %d want %d", got, want)
	}
	if got := snapshotScrollbackLoadedDepth(model.copyMode.Snapshot); got != 0 {
		t.Fatalf("expected live-tail ownership latest to keep committed depth at 0, got %d", got)
	}
	if got := model.copyMode.CommittedLoadedRows; got != 0 {
		t.Fatalf("expected copy-mode committed depth to reset to 0, got %d", got)
	}
	if got, want := model.copyMode.Snapshot.HistoryGeneration, uint64(0); got != want {
		t.Fatalf("expected replace semantics to drop old generation, got %d want %d", got, want)
	}
	if got, want := model.copyMode.Snapshot.ScrollbackFirstRowID, uint64(0); got != want {
		t.Fatalf("expected replace semantics to drop old first row id, got %d want %d", got, want)
	}
	if got, want := model.copyMode.Snapshot.ScrollbackLastRowID, uint64(0); got != want {
		t.Fatalf("expected replace semantics to drop old last row id, got %d want %d", got, want)
	}
	beforeViewportCalls := len(client.viewportRequests)
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})
	if got := len(client.viewportRequests); got != beforeViewportCalls+1 {
		t.Fatalf("expected copy-mode top to request one older page after replace, before=%d after=%d calls=%#v", beforeViewportCalls, got, client.viewportRequests)
	}
	request := client.viewportRequests[len(client.viewportRequests)-1]
	if request.offset != 0 {
		t.Fatalf("expected next older request offset to stay at 0 after live-tail-only replace, got %#v", request)
	}
}

func TestCopyModePagedLatestReplaceKeepsTopPinnedWhenCanonicalWindowAppears(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	terminal := model.runtime.Registry().GetOrCreate("term-1")
	terminal.Snapshot = &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 40, Rows: 8},
		Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromText("tail0", 40), protocolRowFromText("tail1", 40)}),
		ScrollbackOwnership:    []string{protocol.RowOwnershipLiveTailLive, protocol.RowOwnershipLiveTailLive},
		ScrollbackOffset:       0,
		ScrollbackTotal:        2,
		ScrollbackLogicalTotal: 2,
		ScrollbackLoadedRows:   0,
		HistoryGeneration:      0,
		ScrollbackFirstRowID:   0,
		ScrollbackLastRowID:    0,
		ScrollbackHasMore:      false,
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			protocolRowFromText("live0", 40),
			protocolRowFromText("live1", 40),
		}},
		Cursor: protocol.CursorState{Row: 1, Col: 0, Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	model.copyMode.Cursor = copyModePoint{Row: 0, Col: 0}
	model.copyMode.CursorLogical = copyModeLogicalPos{Line: 0, Offset: 0}
	model.copyMode.ViewTopRow = 0

	loaded := &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       terminal.Snapshot.Size,
		Scrollback: protocol.CompactRowsFromCells([][]protocol.Cell{
			protocolRowFromText("canon000", 40),
			protocolRowFromText("canon001", 40),
			protocolRowFromText("canon002", 40),
		}),
		ScrollbackOwnership:    repeatedOwnership(protocol.RowOwnershipPersisted, 3),
		ScrollbackOffset:       0,
		ScrollbackTotal:        99,
		ScrollbackLogicalTotal: 99,
		ScrollbackLoadedRows:   99,
		HistoryGeneration:      7,
		ScrollbackFirstRowID:   0,
		ScrollbackLastRowID:    98,
		ScrollbackHasMore:      false,
		Screen:                 protocol.ScreenData{Cells: cloneProtocolRows(terminal.Snapshot.Screen.Cells)},
		Cursor:                 terminal.Snapshot.Cursor,
		Modes:                  terminal.Snapshot.Modes,
	}

	if !model.extendFrozenCopyModeSnapshot(loaded, 0, true) {
		t.Fatal("expected latest replace to materialize canonical window")
	}
	if got := model.copyMode.ViewTopRow; got != 0 {
		t.Fatalf("expected top-pinned latest replace to keep view top at 0, got %d", got)
	}
	if got := model.copyMode.Cursor.Row; got != 0 {
		t.Fatalf("expected top-pinned latest replace to keep cursor row at 0, got %d", got)
	}
	if got := rowTextFromCompactRow(model.copyMode.Snapshot.Scrollback[0]); got != "canon000" {
		t.Fatalf("expected latest replace to start at canonical top row, got %q", got)
	}
	if got, want := model.copyMode.CommittedLoadedRows, 99; got != want {
		t.Fatalf("expected committed depth %d after canonical latest replace, got %d", want, got)
	}
}

func TestCopyModeRejectsOlderPageWithoutCanonicalHistoryWindow(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"canon001"}, []string{"live0"})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot.ScrollbackLoadedRows = 1
	terminal.Snapshot.HistoryGeneration = 10
	terminal.Snapshot.ScrollbackFirstRowID = 1
	terminal.Snapshot.ScrollbackLastRowID = 1

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	loaded := copyModeTestSnapshot([]string{"ghost0"}, []string{"live0"})
	loaded.ScrollbackOffset = 1
	loaded.ScrollbackTotal = 2
	loaded.ScrollbackLogicalTotal = 2
	loaded.ScrollbackLoadedRows = 0
	loaded.HistoryGeneration = 0
	loaded.ScrollbackFirstRowID = 0
	loaded.ScrollbackLastRowID = 0

	model.extendFrozenCopyModeSnapshot(loaded, 1, false)
	if got, want := model.copyMode.CommittedLoadedRows, 1; got != want {
		t.Fatalf("expected older page without canonical window to be rejected, committed=%d want %d", got, want)
	}
	if got := len(model.copyMode.Snapshot.Scrollback); got != 1 {
		t.Fatalf("expected canonical frozen snapshot to remain unchanged, got %d rows", got)
	}
	if got := rowTextFromCompactRow(model.copyMode.Snapshot.Scrollback[0]); got != "canon001" {
		t.Fatalf("expected canonical frozen row to stay canon001, got %q", got)
	}
	if got, want := model.copyMode.Snapshot.HistoryGeneration, uint64(10); got != want {
		t.Fatalf("expected canonical generation preserved, got %d want %d", got, want)
	}
}

func TestCopyModeAcceptsCanonicalOlderPageWithRowIDZero(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"canon001"}, []string{"live0"})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot.ScrollbackLoadedRows = 1
	terminal.Snapshot.HistoryGeneration = 10
	terminal.Snapshot.ScrollbackFirstRowID = 1
	terminal.Snapshot.ScrollbackLastRowID = 1

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	loaded := copyModeTestSnapshot([]string{"canon000"}, []string{"live0"})
	loaded.ScrollbackOffset = 1
	loaded.ScrollbackTotal = 2
	loaded.ScrollbackLogicalTotal = 2
	loaded.ScrollbackLoadedRows = 2
	loaded.HistoryGeneration = 10
	loaded.ScrollbackFirstRowID = 0
	loaded.ScrollbackLastRowID = 0

	model.extendFrozenCopyModeSnapshot(loaded, 1, false)
	if got, want := model.copyMode.CommittedLoadedRows, 2; got != want {
		t.Fatalf("expected canonical older page to extend frozen snapshot, committed=%d want %d", got, want)
	}
	if got := len(model.copyMode.Snapshot.Scrollback); got != 2 {
		t.Fatalf("expected merged frozen snapshot to contain 2 rows, got %d", got)
	}
	if got := rowTextFromCompactRow(model.copyMode.Snapshot.Scrollback[0]); got != "canon000" {
		t.Fatalf("expected row id 0 page to prepend first, got %q", got)
	}
	if got := rowTextFromCompactRow(model.copyMode.Snapshot.Scrollback[1]); got != "canon001" {
		t.Fatalf("expected existing canonical row to remain second, got %q", got)
	}
	if got, want := model.copyMode.Snapshot.HistoryGeneration, uint64(10); got != want {
		t.Fatalf("expected canonical generation preserved, got %d want %d", got, want)
	}
	if got, want := model.copyMode.Snapshot.ScrollbackFirstRowID, uint64(0); got != want {
		t.Fatalf("expected merged first row id 0, got %d want %d", got, want)
	}
	if got, want := model.copyMode.Snapshot.ScrollbackLastRowID, uint64(1); got != want {
		t.Fatalf("expected merged last row id 1, got %d want %d", got, want)
	}
}

func TestCopyModeExtendsFrozenSnapshotPreservesLogicalTotals(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"new0"}, []string{"live0"})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot.ScrollbackLoadedRows = 1
	terminal.Snapshot.ScrollbackTotal = 1
	terminal.Snapshot.ScrollbackLogicalTotal = 1
	terminal.Snapshot.HistoryGeneration = 7
	terminal.Snapshot.ScrollbackFirstRowID = 100
	terminal.Snapshot.ScrollbackLastRowID = 100

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	loaded := copyModeTestSnapshot([]string{"old0"}, []string{"live0"})
	loaded.ScrollbackOffset = 1
	loaded.ScrollbackTotal = 2
	loaded.ScrollbackLogicalTotal = 1
	loaded.ScrollbackLoadedRows = 2
	loaded.HistoryGeneration = 7
	loaded.ScrollbackFirstRowID = 99
	loaded.ScrollbackLastRowID = 99

	model.extendFrozenCopyModeSnapshot(loaded, 1, false)
	if model.copyMode.Snapshot == nil {
		t.Fatal("expected frozen snapshot")
	}
	if got := model.copyMode.Snapshot.ScrollbackLogicalTotal; got != 1 {
		t.Fatalf("expected logical total preserved after extending frozen snapshot, got %d", got)
	}
}

func TestCopyModeBufferDoesNotAssignCanonicalRefBeyondLoadedCommittedRows(t *testing.T) {
	buffer := copyModeBuffer{
		snapshot: &protocol.Snapshot{
			TerminalID:             "term-1",
			Size:                   protocol.Size{Cols: 5, Rows: 1},
			Scrollback:             protocol.CompactRowsFromCells([][]protocol.Cell{protocolRowFromText("committed0", 5), protocolRowFromText("tail0 ", 5)}),
			ScrollbackOwnership:    []string{protocol.RowOwnershipPersisted, protocol.RowOwnershipLiveTailLive},
			ScrollbackTotal:        2,
			ScrollbackLogicalTotal: 1,
			ScrollbackLoadedRows:   1,
			HistoryGeneration:      7,
			ScrollbackFirstRowID:   100,
			ScrollbackLastRowID:    100,
			Screen:                 protocol.ScreenData{Cells: [][]protocol.Cell{protocolRowFromText("live0", 5)}},
		},
		height: 4,
	}

	if got := buffer.rowRef(0); !got.Valid || got.Generation != 7 || got.RowID != 100 {
		t.Fatalf("expected first loaded committed row to keep canonical ref, got %#v", got)
	}
	if got := buffer.rowRef(1); got.Valid {
		t.Fatalf("expected materialized live-tail row beyond loaded committed depth to have no canonical ref, got %#v", got)
	}
}

func TestCopyModeBufferCanonicalRefsSkipLiveTailOwnershipRows(t *testing.T) {
	buffer := copyModeBuffer{
		snapshot: &protocol.Snapshot{
			TerminalID: "term-1",
			Size:       protocol.Size{Cols: 5, Rows: 1},
			Scrollback: protocol.CompactRowsFromCells([][]protocol.Cell{
				protocolRowFromText("c100 ", 5),
				protocolRowFromText("tail ", 5),
				protocolRowFromText("c101 ", 5),
			}),
			ScrollbackOwnership:    []string{protocol.RowOwnershipPersisted, protocol.RowOwnershipLiveTailLive, protocol.RowOwnershipPersisted},
			ScrollbackTotal:        3,
			ScrollbackLogicalTotal: 2,
			ScrollbackLoadedRows:   2,
			HistoryGeneration:      7,
			ScrollbackFirstRowID:   100,
			ScrollbackLastRowID:    101,
			Screen:                 protocol.ScreenData{Cells: [][]protocol.Cell{protocolRowFromText("live0", 5)}},
		},
		height: 4,
	}

	if got := buffer.rowRef(0); !got.Valid || got.Generation != 7 || got.RowID != 100 {
		t.Fatalf("expected first committed row to keep row id 100, got %#v", got)
	}
	if got := buffer.rowRef(1); got.Valid {
		t.Fatalf("expected live-tail ownership row not to consume canonical row id, got %#v", got)
	}
	if got := buffer.rowRef(2); !got.Valid || got.Generation != 7 || got.RowID != 101 {
		t.Fatalf("expected committed row after live-tail row to keep row id 101, got %#v", got)
	}
	if got, ok := buffer.rowForRef(copyModeRowRef{Generation: 7, RowID: 101, Valid: true}); !ok || got != 2 {
		t.Fatalf("expected row id 101 to resolve to committed row after live-tail row, got row=%d ok=%v", got, ok)
	}
}

func TestCopyModeBufferCanonicalRefsUseMaterializedWindowCommittedOffset(t *testing.T) {
	scrollback := make([][]protocol.Cell, 0, 12000)
	for i := 500; i < 12500; i++ {
		scrollback = append(scrollback, protocolRowFromText(fmt.Sprintf("hist-%05d", i), 12))
	}
	buffer := copyModeBuffer{
		snapshot: &protocol.Snapshot{
			TerminalID:             "term-1",
			Size:                   protocol.Size{Cols: 12, Rows: 1},
			Scrollback:             protocol.CompactRowsFromCells(scrollback),
			ScrollbackOwnership:    repeatedOwnership(protocol.RowOwnershipPersisted, len(scrollback)),
			ScrollbackOffset:       0,
			ScrollbackTotal:        12500,
			ScrollbackLogicalTotal: 12500,
			ScrollbackLoadedRows:   12500,
			HistoryGeneration:      7,
			ScrollbackFirstRowID:   0,
			ScrollbackLastRowID:    12499,
			Screen:                 protocol.ScreenData{Cells: [][]protocol.Cell{protocolRowFromText("live0", 12)}},
		},
		height: 4,
	}

	if got := buffer.rowRef(0); !got.Valid || got.Generation != 7 || got.RowID != 500 {
		t.Fatalf("expected first materialized row to map to committed row id 500, got %#v", got)
	}
	if got := buffer.rowRef(1); !got.Valid || got.RowID != 501 {
		t.Fatalf("expected second materialized row to map to committed row id 501, got %#v", got)
	}
	if got, ok := buffer.rowForRef(copyModeRowRef{Generation: 7, RowID: 100, Valid: true}); ok || got != 0 {
		t.Fatalf("expected row id 100 outside materialized slice to stay unresolved, got row=%d ok=%v", got, ok)
	}
	if got, ok := buffer.rowForRef(copyModeRowRef{Generation: 7, RowID: 500, Valid: true}); !ok || got != 0 {
		t.Fatalf("expected row id 500 to map to materialized row 0, got row=%d ok=%v", got, ok)
	}
	if got, ok := buffer.rowForRef(copyModeRowRef{Generation: 7, RowID: 600, Valid: true}); !ok || got != 100 {
		t.Fatalf("expected row id 600 to map to materialized row 100, got row=%d ok=%v", got, ok)
	}
}

func TestCopyModeTopDoesNotReloadWhenCommittedHistoryExhausted(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, nil, []string{"line0", "line1", "line2", "line3"})
	model.runtime.Registry().Get("term-1").CommittedHistoryExhausted = true

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})

	client := model.runtime.Client().(*recordingBridgeClient)
	if len(client.snapshotCalls) != 0 {
		t.Fatalf("expected exhausted copy-mode terminal not to request history, got %#v", client.snapshotCalls)
	}
}

func TestCopyModeExitAfterHistoryLoadForwardsInput(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, nil, []string{"line0", "line1", "line2", "line3"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	seedCopyModeSnapshot(t, model, []string{"old0", "old1", "old2", "old3"}, []string{"live0", "live1", "live2", "live3"})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCancelMode})

	client := model.runtime.Client().(*recordingBridgeClient)
	dispatchKey(t, model, runeKeyMsg('x'))

	if got := model.mode().Kind; got != input.ModeNormal {
		t.Fatalf("expected copy-mode cancel to return to normal mode, got %q", got)
	}
	if got := model.copyMode.PaneID; got != "" {
		t.Fatalf("expected copy mode to be cleared after cancel, got %#v", model.copyMode)
	}
	if len(client.inputCalls) != 1 {
		t.Fatalf("expected normal key input after leaving copy mode, got %#v", client.inputCalls)
	}
	if got := string(client.inputCalls[0].data); got != "x" {
		t.Fatalf("expected key input to be forwarded after leaving copy mode, got %q", got)
	}
}

func TestCopyModeFrozenViewResumesLiveSnapshotOnExit(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"hist-a", "hist-b"}, []string{"live-a", "live-b"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	frozenView := xansi.Strip(model.View())
	if !strings.Contains(frozenView, "hist-a") || !strings.Contains(frozenView, "live-a") {
		t.Fatalf("expected copy mode to show the initial snapshot, got:\n%s", frozenView)
	}

	seedCopyModeSnapshot(t, model, []string{"next-a", "next-b"}, []string{"tail-a", "tail-b"})
	loaded, err := model.runtime.LoadSnapshot(context.Background(), "term-1", 0, 0)
	if err != nil {
		t.Fatalf("load live snapshot while frozen: %v", err)
	}
	_, cmd := model.Update(orchestrator.SnapshotLoadedMsg{TerminalID: "term-1", Snapshot: loaded})
	drainCmd(t, model, cmd, 20)

	stillFrozen := xansi.Strip(model.View())
	if strings.Contains(stillFrozen, "next-a") || strings.Contains(stillFrozen, "tail-b") {
		t.Fatalf("expected copy mode view to stay frozen while active, got:\n%s", stillFrozen)
	}
	if !strings.Contains(stillFrozen, "hist-a") || !strings.Contains(stillFrozen, "live-a") {
		t.Fatalf("expected copy mode view to preserve frozen rows while active, got:\n%s", stillFrozen)
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCancelMode})

	liveView := xansi.Strip(model.View())
	if !strings.Contains(liveView, "tail-a") || !strings.Contains(liveView, "tail-b") {
		t.Fatalf("expected live snapshot to appear again after leaving copy mode, got:\n%s", liveView)
	}
}

func TestCopyModeExitRefreshesLatestLocalVTermSnapshot(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"hist-a"}, []string{"old-live"})

	if _, err := model.runtime.LoadSnapshot(context.Background(), "term-1", 0, 0); err != nil {
		t.Fatalf("load snapshot into vterm: %v", err)
	}
	terminal := model.runtime.Registry().Get("term-1")
	if terminal == nil || terminal.VTerm == nil {
		t.Fatalf("expected live vterm after snapshot load, got %#v", terminal)
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})

	if _, err := terminal.VTerm.Write([]byte("\x1b[2J\x1b[Hnew-live")); err != nil {
		t.Fatalf("write live vterm update: %v", err)
	}
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCancelMode})

	view := xansi.Strip(model.View())
	if !strings.Contains(view, "new-live") {
		t.Fatalf("expected exit from copy mode to refresh the latest local vterm snapshot, got:\n%s", view)
	}
	if strings.Contains(view, "old-live") {
		t.Fatalf("expected stale pre-copy snapshot to be replaced on exit, got:\n%s", view)
	}
}

func TestCopyModeExitReturnsToLiveSurfaceImmediatelyWhenLocalVTermIsCurrent(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"hist-a"}, []string{"queued-text"})
	client := model.runtime.Client().(*recordingBridgeClient)

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})

	terminal := model.runtime.Registry().Get("term-1")
	if terminal == nil {
		t.Fatal("expected terminal runtime")
	}
	terminal.Stream.Active = true
	client.snapshotByTerminal["term-1"] = copyModeTestSnapshot([]string{"hist-a"}, []string{"transient-live"})
	if _, err := model.runtime.LoadSnapshot(context.Background(), "term-1", 0, 0); err != nil {
		t.Fatalf("load transient live snapshot: %v", err)
	}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCancelMode})

	liveView := xansi.Strip(model.View())
	if !strings.Contains(liveView, "transient-live") {
		t.Fatalf("expected copy-mode exit to return to the current live surface immediately, got:\n%s", liveView)
	}
	if strings.Contains(liveView, "queued-text") {
		t.Fatalf("expected frozen copy-mode snapshot to clear on exit, got:\n%s", liveView)
	}
}

func TestCopyModeEnteringScrollbackForcesViewportScroll(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 12})
	seedCopyModeSnapshot(t, model, []string{"hist0", "hist1", "hist2"}, []string{"live0", "live1"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	before := model.runtime.PaneViewportOffset("pane-1")

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})

	if got := model.runtime.PaneViewportOffset("pane-1"); got < before || got <= 0 {
		t.Fatalf("expected pane viewport to enter or remain in scrollback when cursor moves above screen, before=%d after=%d", before, got)
	}
}

func TestCopyModeBufferNormalizeColSkipsContinuationCells(t *testing.T) {
	buffer := copyModeBuffer{
		snapshot: &protocol.Snapshot{
			Size: protocol.Size{Cols: 2, Rows: 1},
			Screen: protocol.ScreenData{Cells: [][]protocol.Cell{{
				{Content: "界", Width: 2},
				{Content: "", Width: 0},
			}}},
		},
		height: 1,
	}

	if got := buffer.normalizeCol(0, 1); got != 0 {
		t.Fatalf("expected continuation column to normalize back to cell start, got %d", got)
	}
	point := buffer.clampPoint(copyModePoint{Row: 0, Col: 1})
	if point.Col != 0 {
		t.Fatalf("expected clamped point to avoid continuation column, got %#v", point)
	}
}

func TestCopyModeBufferViewportRangeUsesScrollbackBoundary(t *testing.T) {
	buffer := copyModeBuffer{
		snapshot: copyModeTestSnapshot([]string{"hist0", "hist1", "hist2"}, []string{"live0", "live1"}),
		height:   2,
	}

	if got := buffer.viewportStart(0); got != 3 {
		t.Fatalf("expected live viewport to start at scrollback boundary, got %d", got)
	}
	if got := buffer.viewportEnd(0); got != 5 {
		t.Fatalf("expected live viewport to end at total rows, got %d", got)
	}
	if got := buffer.viewportStart(2); got != 1 {
		t.Fatalf("expected scrolled viewport start to move into scrollback, got %d", got)
	}
	if got := buffer.viewportEnd(2); got != 3 {
		t.Fatalf("expected scrolled viewport end to stop before live tail, got %d", got)
	}
}

func TestCopyModeSelectedTextNormalizesReverseMultiRowSelection(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"alpha", "bravo"}, []string{"charl", "delta"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	model.copyMode.Mark = &copyModePoint{Row: 2, Col: 2}
	model.copyMode.Cursor = copyModePoint{Row: 0, Col: 1}

	text, ok := model.copyModeSelectedText()
	if !ok {
		t.Fatal("expected selection text")
	}
	if text != "lpha\nbravo\ncha" {
		t.Fatalf("unexpected normalized selection text %q", text)
	}
}

func TestCopyModeSelectedTextPreservesSoftWrappedLines(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"alpha", "bravo"}, []string{"charlie"})
	terminal := model.runtime.Registry().Get("term-1")
	terminal.Snapshot.ScrollbackWrapped = []bool{true, false}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	model.copyMode.Mark = &copyModePoint{Row: 0, Col: 0}
	model.copyMode.Cursor = copyModePoint{Row: 2, Col: 6}

	text, ok := model.copyModeSelectedText()
	if !ok {
		t.Fatal("expected selection text")
	}
	if text != "alphabravo\ncharlie" {
		t.Fatalf("expected soft-wrapped scrollback rows to copy as one line, got %q", text)
	}
}

func TestCopyModeLogicalLinesUseWrappedBoundaries(t *testing.T) {
	buffer := copyModeBuffer{
		snapshot: copyModeTestSnapshot([]string{"hist0", "hist1", "hist2", "hist3"}, nil),
		height:   2,
	}
	buffer.snapshot.ScrollbackWrapped = []bool{true, true, false, false}

	logicalLines := newCopyModeLogicalLines(buffer)
	if got := logicalLines.lineStart(2); got != 0 {
		t.Fatalf("expected logical line start to walk through wrapped predecessors, got %d", got)
	}
	if got := logicalLines.lineEnd(0); got != 2 {
		t.Fatalf("expected logical line end to walk wrapped successors, got %d", got)
	}
	if logicalLines.rowContinues(2) {
		t.Fatal("expected hard line boundary after unwrapped row")
	}
}

func TestCopyModePointAtMouseMapsScreenPositionToBufferedRow(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"hist0", "hist1", "hist2"}, []string{"live0", "live1", "live2"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	model.copyMode.ViewTopRow = 1
	model.copyMode.Cursor = copyModePoint{Row: 1, Col: 0}
	x, y := activePaneContentScreenOrigin(t, model)

	point, ok := model.copyModePointAtMouse(x+2, y+1)
	if !ok {
		t.Fatal("expected mouse point inside active copy-mode pane")
	}
	if point != (copyModePoint{Row: 2, Col: 2}) {
		t.Fatalf("unexpected mapped copy-mode point %#v", point)
	}
}

func TestHandleCopyModeAutoScrollSkipsWhenSeqOrDirectionDoesNotMatch(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"hist0", "hist1", "hist2"}, []string{"live0", "live1", "live2"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	before := model.copyMode.Cursor
	model.copyMode.MouseSelecting = true
	model.copyMode.AutoScrollDir = 1
	model.copyMode.AutoScrollSeq = 7

	if cmd := model.handleCopyModeAutoScroll(6); cmd != nil {
		t.Fatalf("expected mismatched sequence to skip auto-scroll, got %#v", cmd)
	}
	if model.copyMode.Cursor != before {
		t.Fatalf("expected mismatched sequence to keep cursor unchanged, got %#v want %#v", model.copyMode.Cursor, before)
	}

	model.copyMode.AutoScrollDir = 0
	if cmd := model.handleCopyModeAutoScroll(7); cmd != nil {
		t.Fatalf("expected zero auto-scroll direction to skip tick, got %#v", cmd)
	}
	if model.copyMode.Cursor != before {
		t.Fatalf("expected zero auto-scroll direction to keep cursor unchanged, got %#v want %#v", model.copyMode.Cursor, before)
	}
}

func TestActiveLiveCopyModeBufferRefreshesStaleVTermSnapshot(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"hist-a"}, []string{"old-live"})

	if _, err := model.runtime.LoadSnapshot(context.Background(), "term-1", 0, 0); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	terminal := model.runtime.Registry().Get("term-1")
	if terminal == nil || terminal.VTerm == nil || terminal.Snapshot == nil {
		t.Fatalf("expected live terminal snapshot cache, got %#v", terminal)
	}
	if _, err := terminal.VTerm.Write([]byte("\x1b[2J\x1b[Hfresh-live")); err != nil {
		t.Fatalf("write live vterm update: %v", err)
	}
	terminal.SurfaceVersion++

	buffer, ok := model.activeLiveCopyModeBuffer()
	if !ok {
		t.Fatal("expected live copy-mode buffer")
	}
	if buffer.snapshot == nil {
		t.Fatal("expected refreshed snapshot")
	}
	var snapshotText strings.Builder
	for _, row := range buffer.snapshot.Scrollback {
		for _, cell := range row.DecodeCells() {
			snapshotText.WriteString(cell.Content)
		}
		snapshotText.WriteByte('\n')
	}
	for _, row := range buffer.snapshot.Screen.Cells {
		for _, cell := range row {
			snapshotText.WriteString(cell.Content)
		}
		snapshotText.WriteByte('\n')
	}
	if !strings.Contains(snapshotText.String(), "fresh") {
		t.Fatalf("expected refreshed buffer snapshot to include live vterm content, got %#v", buffer.snapshot)
	}
	if terminal.SnapshotVersion != terminal.SurfaceVersion {
		t.Fatalf("expected activeLiveCopyModeBuffer to refresh snapshot version, snapshot=%d surface=%d", terminal.SnapshotVersion, terminal.SurfaceVersion)
	}
	contentRect, ok := model.activePaneContentRect()
	if !ok {
		t.Fatal("expected active pane content rect")
	}
	if buffer.height != maxInt(1, contentRect.H) {
		t.Fatalf("expected buffer height to follow content rect, got %d want %d", buffer.height, maxInt(1, contentRect.H))
	}
}

func TestSyncCopyModeViewportClampsAndUpdatesPaneViewport(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"hist0", "hist1", "hist2"}, []string{"live0", "live1"})
	model.copyMode.PaneID = "pane-1"

	buffer, ok := model.activeLiveCopyModeBuffer()
	if !ok {
		t.Fatal("expected live copy-mode buffer")
	}
	model.copyMode.ViewTopRow = 999
	model.syncCopyModeViewport(buffer, copyModePoint{Row: buffer.totalRows() - 1, Col: 0})
	if got, want := model.copyMode.ViewTopRow, buffer.maxViewTopRow(); got != want {
		t.Fatalf("expected viewport to clamp to max top row, got %d want %d", got, want)
	}

	model.copyMode.ViewTopRow = 999
	model.syncCopyModeViewport(buffer, copyModePoint{Row: 1, Col: 0})
	if got := model.copyMode.ViewTopRow; got != 1 {
		t.Fatalf("expected viewport to shift upward for selected row, got %d", got)
	}
	if got, want := model.runtime.PaneViewportOffset("pane-1"), model.copyModeRenderOffset(buffer); got != want {
		t.Fatalf("expected syncCopyModeViewport to keep pane viewport aligned, got %d want %d", got, want)
	}
	if got := model.runtime.PaneViewportOffset("pane-1"); got < 0 {
		t.Fatalf("expected syncCopyModeViewport to keep non-negative pane viewport offset, got %d", got)
	}
}

func TestPasteBufferActionSendsPasteToActiveTerminal(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 12})
	seedCopyModeSnapshot(t, model, []string{"hist0"}, []string{"live0"})
	model.yankBuffer = "hello\nworld"

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionPasteBuffer})

	client := model.runtime.Client().(*recordingBridgeClient)
	if len(client.inputCalls) != 1 {
		t.Fatalf("expected one paste input call, got %#v", client.inputCalls)
	}
	if got := string(client.inputCalls[0].data); got != "hello\nworld" {
		t.Fatalf("unexpected pasted payload %q", got)
	}
	if got := model.input.Mode().Kind; got != input.ModeNormal {
		t.Fatalf("expected paste to return to normal mode, got %q", got)
	}
}

func TestPasteClipboardActionReadsSystemClipboard(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 12})
	seedCopyModeSnapshot(t, model, []string{"hist0"}, []string{"live0"})
	prevReader := systemClipboardReader
	systemClipboardReader = func() (string, error) { return "clip-text", nil }
	defer func() { systemClipboardReader = prevReader }()

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionPasteClipboard})

	client := model.runtime.Client().(*recordingBridgeClient)
	if len(client.inputCalls) != 1 {
		t.Fatalf("expected one clipboard paste input call, got %#v", client.inputCalls)
	}
	if got := string(client.inputCalls[0].data); got != "clip-text" {
		t.Fatalf("unexpected clipboard pasted payload %q", got)
	}
}

func TestClipboardHistoryPickerPastesSelectedEntry(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 12})
	seedCopyModeSnapshot(t, model, []string{"hist0"}, []string{"live0"})
	model.pushClipboardHistory("first entry", "pane-1")
	model.pushClipboardHistory("second entry", "pane-1")

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenClipboardHistory})

	if got := model.input.Mode().Kind; got != input.ModePicker {
		t.Fatalf("expected clipboard history to open picker mode, got %q", got)
	}
	if model.modalHost == nil || model.modalHost.Picker == nil {
		t.Fatal("expected clipboard history picker")
	}
	model.modalHost.Picker.Selected = 2

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSubmitPrompt})

	client := model.runtime.Client().(*recordingBridgeClient)
	if len(client.inputCalls) != 1 {
		t.Fatalf("expected one clipboard-history paste input call, got %#v", client.inputCalls)
	}
	if got := string(client.inputCalls[0].data); got != "first entry" {
		t.Fatalf("unexpected clipboard-history pasted payload %q", got)
	}
	if got := model.input.Mode().Kind; got != input.ModeNormal {
		t.Fatalf("expected history paste to return to normal mode, got %q", got)
	}
}

func TestClipboardHistoryCopyPersistsToDaemonPublicStorage(t *testing.T) {
	client := &recordingBridgeClient{snapshotByTerminal: map[string]*protocol.Snapshot{}}
	model := setupModel(t, modelOpts{client: client, width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"alpha"}, []string{"live0"})
	writer := &recordingControlWriter{}
	model.SetCursorWriter(writer)

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeBeginSelection})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeCursorRight})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeCopySelectionExit})

	if len(client.storagePutCalls) != 1 {
		t.Fatalf("expected one clipboard storage put, got %#v", client.storagePutCalls)
	}
	put := client.storagePutCalls[0]
	if put.AppID != clipboardHistoryStorageAppID || put.Scope != protocol.StorageScopePublic || put.OwnerID != "" {
		t.Fatalf("expected public clipboard storage put, got %#v", put)
	}
	if !strings.HasPrefix(put.Key, clipboardHistoryStoragePrefix) {
		t.Fatalf("expected clipboard history key prefix, got %q", put.Key)
	}
	var record clipboardHistoryRecord
	if err := json.Unmarshal(put.Value, &record); err != nil {
		t.Fatalf("decode stored clipboard record: %v", err)
	}
	if record.SchemaVersion != clipboardHistoryRecordVersion || record.Text != "al" || record.PaneID != "pane-1" || record.SourceApp != "tuiv2" {
		t.Fatalf("unexpected stored clipboard record: %#v", record)
	}
	if len(client.storageListCalls) == 0 || client.storageListCalls[0].AppID != clipboardHistoryStorageAppID || client.storageListCalls[0].Prefix != clipboardHistoryStoragePrefix {
		t.Fatalf("expected prune to list clipboard history, got %#v", client.storageListCalls)
	}
}

func TestClipboardHistoryPickerLoadsDaemonPublicStorage(t *testing.T) {
	createdAt := time.Date(2026, 5, 16, 10, 30, 0, 0, time.UTC)
	value, err := json.Marshal(clipboardHistoryRecord{
		SchemaVersion: clipboardHistoryRecordVersion,
		ID:            "shared-1",
		Text:          "from daemon",
		PaneID:        "remote-app",
		SourceApp:     "remote-ui",
		CreatedAt:     createdAt,
	})
	if err != nil {
		t.Fatalf("encode clipboard record: %v", err)
	}
	key := clipboardHistoryStorageKey("shared-1")
	client := &recordingBridgeClient{
		storageEntries: map[string]protocol.StorageEntry{
			key: {
				AppID:     clipboardHistoryStorageAppID,
				Scope:     protocol.StorageScopePublic,
				Key:       key,
				Value:     value,
				UpdatedAt: createdAt,
			},
		},
	}
	model := setupModel(t, modelOpts{client: client, width: 80, height: 12})
	seedCopyModeSnapshot(t, model, []string{"hist0"}, []string{"live0"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenClipboardHistory})

	if got := model.input.Mode().Kind; got != input.ModePicker {
		t.Fatalf("expected clipboard history picker mode, got %q", got)
	}
	if got := model.yankBuffer; got != "from daemon" {
		t.Fatalf("expected loaded storage entry to become paste buffer, got %q", got)
	}
	view := xansi.Strip(model.View())
	for _, want := range []string{"history", "preview", "from daemon", "time: 2026-05-16", "source: remote-ui", "from: remote-app"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected storage-backed history to contain %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "shared-1") {
		t.Fatalf("clipboard history picker should not show internal storage id:\n%s", view)
	}
	if len(client.storageListCalls) == 0 || client.storageListCalls[0].AppID != clipboardHistoryStorageAppID || client.storageListCalls[0].Scope != protocol.StorageScopePublic {
		t.Fatalf("expected public storage list, got %#v", client.storageListCalls)
	}
}

func TestClipboardHistoryStoreSupportsCRUDOnDaemonPublicStorage(t *testing.T) {
	client := &recordingBridgeClient{}
	store := newClipboardHistoryStoreFromClient(client)
	if store == nil {
		t.Fatal("expected storage-backed clipboard history store")
	}
	entry := clipboardHistoryEntry{
		ID:        "crud-1",
		Text:      "shared value",
		PaneID:    "pane-a",
		CreatedAt: time.Date(2026, 5, 16, 11, 0, 0, 0, time.UTC),
	}
	if err := store.Put(context.Background(), entry); err != nil {
		t.Fatalf("put clipboard entry: %v", err)
	}
	got, err := store.Get(context.Background(), entry.ID)
	if err != nil {
		t.Fatalf("get clipboard entry: %v", err)
	}
	if got.ID != entry.ID || got.Text != entry.Text || got.PaneID != entry.PaneID {
		t.Fatalf("unexpected get result: %#v", got)
	}
	listed, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list clipboard entries: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != entry.ID {
		t.Fatalf("unexpected list result: %#v", listed)
	}
	if err := store.Delete(context.Background(), entry.ID); err != nil {
		t.Fatalf("delete clipboard entry: %v", err)
	}
	listed, err = store.List(context.Background())
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("expected deleted entry to disappear, got %#v", listed)
	}
	if len(client.storageGetCalls) != 1 || client.storageGetCalls[0].Scope != protocol.StorageScopePublic {
		t.Fatalf("expected public storage get, got %#v", client.storageGetCalls)
	}
	if len(client.storageDeleteCalls) != 1 || client.storageDeleteCalls[0].Key != clipboardHistoryStorageKey(entry.ID) {
		t.Fatalf("expected storage delete by clipboard key, got %#v", client.storageDeleteCalls)
	}
}

func TestClipboardHistoryPickerCreatesEntryInPublicStorage(t *testing.T) {
	client := &recordingBridgeClient{snapshotByTerminal: map[string]*protocol.Snapshot{}}
	model := setupModel(t, modelOpts{client: client, width: 80, height: 12})
	seedCopyModeSnapshot(t, model, []string{"hist0"}, []string{"live0"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenClipboardHistory})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSubmitPrompt})

	if model.modalHost == nil || model.modalHost.Session == nil || model.modalHost.Session.Kind != input.ModePrompt {
		t.Fatalf("expected create clipboard prompt, got %#v", model.modalHost)
	}
	if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "create-clipboard-entry" {
		t.Fatalf("expected create-clipboard-entry prompt, got %#v", model.modalHost.Prompt)
	}
	model.modalHost.Prompt.Value = "manual clip"
	model.modalHost.Prompt.Cursor = len([]rune(model.modalHost.Prompt.Value))

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSubmitPrompt})

	if got := model.input.Mode().Kind; got != input.ModePicker {
		t.Fatalf("expected create submit to return to clipboard picker, got %q", got)
	}
	if len(client.storagePutCalls) != 1 {
		t.Fatalf("expected created clipboard entry to be stored, got %#v", client.storagePutCalls)
	}
	var record clipboardHistoryRecord
	if err := json.Unmarshal(client.storagePutCalls[0].Value, &record); err != nil {
		t.Fatalf("decode stored clipboard record: %v", err)
	}
	if record.Text != "manual clip" || record.SourceApp != "tuiv2" || client.storagePutCalls[0].Scope != protocol.StorageScopePublic {
		t.Fatalf("unexpected stored clipboard entry: put=%#v record=%#v", client.storagePutCalls[0], record)
	}
	view := xansi.Strip(model.View())
	for _, want := range []string{"Clipboard History", "manual clip", "New clipboard entry", "source: tuiv2", "from: pane-1"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected clipboard picker to contain %q:\n%s", want, view)
		}
	}
}

func TestClipboardHistoryPickerEditsEntryInPublicStorage(t *testing.T) {
	client := &recordingBridgeClient{snapshotByTerminal: map[string]*protocol.Snapshot{}}
	model := setupModel(t, modelOpts{client: client, width: 80, height: 12})
	seedCopyModeSnapshot(t, model, []string{"hist0"}, []string{"live0"})
	model.clipboardHistory = []clipboardHistoryEntry{normalizeClipboardHistoryEntry(clipboardHistoryEntry{
		ID:        "clip-edit",
		Text:      "old value",
		PaneID:    "pane-1",
		CreatedAt: time.Date(2026, 5, 16, 11, 5, 0, 0, time.UTC),
	})}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenClipboardHistory})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEditTerminal})

	if model.modalHost == nil || model.modalHost.Session == nil || model.modalHost.Session.Kind != input.ModePrompt {
		t.Fatalf("expected edit clipboard prompt, got %#v", model.modalHost)
	}
	if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "edit-clipboard-entry" || model.modalHost.Prompt.TerminalID != "clip-edit" {
		t.Fatalf("unexpected edit prompt: %#v", model.modalHost.Prompt)
	}
	model.modalHost.Prompt.Value = "updated value"
	model.modalHost.Prompt.Cursor = len([]rune(model.modalHost.Prompt.Value))

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSubmitPrompt})

	if got := model.input.Mode().Kind; got != input.ModePicker {
		t.Fatalf("expected edit submit to return to clipboard picker, got %q", got)
	}
	if len(client.storagePutCalls) != 1 || client.storagePutCalls[0].Key != clipboardHistoryStorageKey("clip-edit") {
		t.Fatalf("expected edited clipboard entry to be stored at same key, got %#v", client.storagePutCalls)
	}
	entry := model.clipboardHistoryEntryByID("clip-edit")
	if entry == nil || entry.Text != "updated value" {
		t.Fatalf("expected in-memory clipboard entry to update, got %#v", entry)
	}
	view := xansi.Strip(model.View())
	if !strings.Contains(view, "updated value") || strings.Contains(view, "old value") {
		t.Fatalf("expected updated clipboard picker view:\n%s", view)
	}
}

func TestClipboardHistoryPickerDeletesEntryFromPublicStorage(t *testing.T) {
	client := &recordingBridgeClient{snapshotByTerminal: map[string]*protocol.Snapshot{}}
	model := setupModel(t, modelOpts{client: client, width: 80, height: 12})
	seedCopyModeSnapshot(t, model, []string{"hist0"}, []string{"live0"})
	model.clipboardHistory = []clipboardHistoryEntry{normalizeClipboardHistoryEntry(clipboardHistoryEntry{
		ID:        "clip-delete",
		Text:      "delete me",
		PaneID:    "pane-1",
		CreatedAt: time.Date(2026, 5, 16, 11, 10, 0, 0, time.UTC),
	})}

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenClipboardHistory})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionRemoveTerminal})

	if got := model.input.Mode().Kind; got != input.ModePicker {
		t.Fatalf("expected delete to keep clipboard picker open, got %q", got)
	}
	if len(client.storageDeleteCalls) != 1 || client.storageDeleteCalls[0].Key != clipboardHistoryStorageKey("clip-delete") {
		t.Fatalf("expected clipboard storage delete, got %#v", client.storageDeleteCalls)
	}
	if entry := model.clipboardHistoryEntryByID("clip-delete"); entry != nil {
		t.Fatalf("expected deleted entry to leave memory, got %#v", entry)
	}
	view := xansi.Strip(model.View())
	for _, want := range []string{"Clipboard", "copy text first", "New clipboard entry"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected clipboard picker to contain %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "delete me") {
		t.Fatalf("expected deleted text to disappear:\n%s", view)
	}
}

func TestClipboardHistoryPickerIgnoresEmptyStateSubmit(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 12})
	seedCopyModeSnapshot(t, model, []string{"hist0"}, []string{"live0"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenClipboardHistory})
	model.modalHost.Picker.Selected = 1
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSubmitPrompt})

	if got := model.input.Mode().Kind; got != input.ModePicker {
		t.Fatalf("expected empty state submit to keep picker open, got %q", got)
	}
	if model.err != nil {
		t.Fatalf("expected empty state submit not to set error, got %v", model.err)
	}
}

func TestClipboardHistoryPromptCancelReturnsToPicker(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 12})
	seedCopyModeSnapshot(t, model, []string{"hist0"}, []string{"live0"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenClipboardHistory})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionSubmitPrompt})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCancelMode})

	if got := model.input.Mode().Kind; got != input.ModePicker {
		t.Fatalf("expected cancel to return to clipboard picker, got %q", got)
	}
	if model.modalHost == nil || model.modalHost.Session == nil || model.modalHost.Session.RequestID != clipboardHistoryRequestID() || model.modalHost.Picker == nil {
		t.Fatalf("expected clipboard picker restored, got %#v", model.modalHost)
	}
}

func TestClipboardHistoryPickerStatusHintsUseClipboardActions(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 12})
	seedCopyModeSnapshot(t, model, []string{"hist0"}, []string{"live0"})
	model.pushClipboardHistory("first entry", "pane-1")

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionOpenClipboardHistory})

	hints := strings.Join(model.renderVM().Status.Hints, " ")
	for _, want := range []string{"Enter PASTE/NEW", "Ctrl-E EDIT", "Ctrl-X DELETE"} {
		if !strings.Contains(hints, want) {
			t.Fatalf("expected clipboard status hint %q in %#v", want, model.renderVM().Status.Hints)
		}
	}
	for _, forbidden := range []string{"Tab SPLIT", "Ctrl-K KILL", "Enter HERE"} {
		if strings.Contains(hints, forbidden) {
			t.Fatalf("clipboard picker should not show terminal hint %q in %#v", forbidden, model.renderVM().Status.Hints)
		}
	}
}

func TestClipboardHistoryPickerOpensFromKeysAndRendersOverlay(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 14})
	seedCopyModeSnapshot(t, model, []string{"hist0"}, []string{"live0"})
	model.pushClipboardHistory("first entry", "pane-1")

	dispatchKey(t, model, ctrlKey(tea.KeyCtrlV))
	dispatchKey(t, model, runeKeyMsg('H'))

	if got := model.input.Mode().Kind; got != input.ModePicker {
		t.Fatalf("expected clipboard history picker mode, got %q", got)
	}
	view := xansi.Strip(model.View())
	for _, want := range []string{"Clipboard History", "history", "preview", "first entry", "source: tuiv2", "from: pane-1"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected clipboard history overlay to contain %q:\n%s", want, view)
		}
	}
}

func TestClipboardHistoryPickerShowsEmptyState(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 14})
	seedCopyModeSnapshot(t, model, []string{"hist0"}, []string{"live0"})

	dispatchKey(t, model, ctrlKey(tea.KeyCtrlV))
	dispatchKey(t, model, runeKeyMsg('H'))

	if got := model.input.Mode().Kind; got != input.ModePicker {
		t.Fatalf("expected clipboard history picker mode, got %q", got)
	}
	view := xansi.Strip(model.View())
	for _, want := range []string{"Clipboard History", "history", "preview", "New clipboard entry", "Clipboard", "copy text first"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected empty clipboard history overlay to contain %q:\n%s", want, view)
		}
	}
	for _, forbidden := range []string{"Space to mark", "Space or y"} {
		if strings.Contains(view, forbidden) {
			t.Fatalf("clipboard history empty state should not leak shortcut text %q:\n%s", forbidden, view)
		}
	}
}
