package app

import (
	"context"
	"encoding/json"
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
	return &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: uint16(maxCols), Rows: uint16(len(screenRows))},
		Scrollback: protocol.CompactRowsFromCells(sbRows),
		Screen:     protocol.ScreenData{Cells: screenRows},
		Cursor:     protocol.CursorState{Row: maxInt(0, len(screenRows)-1), Col: 0, Visible: true},
		Modes:      protocol.TerminalModes{AutoWrap: true},
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
	cloned.ScrollbackOffset = offset
	cloned.ScrollbackTotal = total
	cloned.ScrollbackHasMore = start > 0
	return cloned
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

	beforeRow := model.copyMode.Cursor.Row
	beforeMark := *model.copyMode.Mark
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
	if got := model.copyMode.Cursor.Row; got != beforeRow+2 {
		t.Fatalf("expected copy-mode cursor row to shift with prepended history, before=%d after=%d", beforeRow, got)
	}
	if model.copyMode.Mark == nil {
		t.Fatal("expected mark to remain set")
	}
	if got := model.copyMode.Mark.Row; got != beforeMark.Row+2 {
		t.Fatalf("expected copy-mode mark row to shift with prepended history, before=%d after=%d", beforeMark.Row, got)
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
	if got := model.copyMode.Cursor.Row; got != 4 {
		t.Fatalf("expected cursor to stay on the same logical content after prepending history, got row %d", got)
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
	terminal.ScrollbackExhausted = true
	terminal.Snapshot.ScrollbackTotal = 3
	client := model.runtime.Client().(*recordingBridgeClient)
	client.snapshotByTerminal["term-1"] = copyModeTestSnapshot([]string{"old0", "old1", "old2"}, []string{"live0", "live1", "live2"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})

	if got := len(client.viewportRequests); got != 1 {
		t.Fatalf("expected stale exhausted flag not to suppress copy-mode history load, got %#v", client.viewportRequests)
	}
	if got, want := len(model.copyMode.Snapshot.Scrollback), 3; got != want {
		t.Fatalf("expected stale-exhausted history to load into frozen buffer, got %d want %d", got, want)
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

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	client := model.runtime.Client().(*recordingBridgeClient)
	beforeSnapshotCalls := len(client.snapshotRequests)
	beforeViewportCalls := len(client.viewportRequests)

	serverSnapshot := copyModeTestSnapshot(allRows, []string{"next0", "next1", "next2", "next3"})
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

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	client := model.runtime.Client().(*recordingBridgeClient)
	beforeSnapshotCalls := len(client.snapshotRequests)
	beforeViewportCalls := len(client.viewportRequests)

	client.snapshotByTerminal["term-1"] = copyModeTestSnapshot(allRows, []string{"next0", "next1", "next2", "next3"})
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
	if got := model.copyMode.LoadedRows; got != len(allRows) {
		t.Fatalf("expected loaded depth to keep logical pagination progress, got %d want %d", got, len(allRows))
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

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	client := model.runtime.Client().(*recordingBridgeClient)
	client.snapshotByTerminal["term-1"] = copyModeTestSnapshot(allRows, []string{"next0", "next1", "next2", "next3"})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})

	if got, want := model.copyMode.LoadedRows, terminalMaterializedScrollbackLimit+500; got != want {
		t.Fatalf("expected first older page to advance loaded depth, got %d want %d", got, want)
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
		t.Fatalf("expected next page request to use loaded depth, got %#v", request)
	}
	if got, want := model.copyMode.LoadedRows, len(allRows); got != want {
		t.Fatalf("expected second page to advance loaded depth, got %d want %d", got, want)
	}
	if got, want := len(model.copyMode.Snapshot.Scrollback), terminalMaterializedScrollbackLimit; got != want {
		t.Fatalf("expected second page to keep bounded copy buffer, got %d want %d", got, want)
	}
}

func TestCopyModeTopDoesNotReloadWhenScrollbackExhausted(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, nil, []string{"line0", "line1", "line2", "line3"})
	model.runtime.Registry().Get("term-1").ScrollbackExhausted = true

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
	if got := model.runtime.PaneViewportOffset("pane-1"); got <= 0 {
		t.Fatalf("expected syncCopyModeViewport to move viewport into scrollback, got %d", got)
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
