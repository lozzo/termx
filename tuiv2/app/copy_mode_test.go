package app

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/orchestrator"
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
		Scrollback: sbRows,
		Screen:     protocol.ScreenData{Cells: screenRows},
		Cursor:     protocol.CursorState{Row: maxInt(0, len(screenRows)-1), Col: 0, Visible: true},
		Modes:      protocol.TerminalModes{AutoWrap: true},
	}
}

func seedCopyModeSnapshot(t *testing.T, m *Model, scrollback, screen []string) {
	t.Helper()
	terminal := m.runtime.Registry().GetOrCreate("term-1")
	snapshot := copyModeTestSnapshot(scrollback, screen)
	terminal.Snapshot = snapshot
	if client, ok := m.runtime.Client().(*recordingBridgeClient); ok {
		if client.snapshotByTerminal == nil {
			client.snapshotByTerminal = make(map[string]*protocol.Snapshot)
		}
		client.snapshotByTerminal["term-1"] = snapshot
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
	if got := model.workbench.CurrentTab().ScrollOffset; got != 0 {
		t.Fatalf("expected copy+exit to reset scroll offset, got %d", got)
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

	tab := model.workbench.CurrentTab()
	beforeOffset := tab.ScrollOffset
	beforeRow := model.copyMode.Cursor.Row

	_, cmd = model.Update(copyModeAutoScrollMsg{seq: seq})
	drainCmd(t, model, cmd, 20)

	if tab.ScrollOffset <= beforeOffset {
		t.Fatalf("expected scroll offset to increase after auto-scroll, before=%d after=%d", beforeOffset, tab.ScrollOffset)
	}
	if model.copyMode.Cursor.Row >= beforeRow {
		t.Fatalf("expected copy cursor to move upward during auto-scroll, before=%d after=%d", beforeRow, model.copyMode.Cursor.Row)
	}

	_, cmd = model.Update(tea.MouseMsg{X: x, Y: y - 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	drainCmd(t, model, cmd, 20)
	if model.copyMode.MouseSelecting {
		t.Fatal("expected mouse copy selection to stop on release")
	}
}

func TestCopyModeFreezesCursorAndSelectionWhenScrollbackExpands(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"old2", "old3"}, []string{"line0", "line1", "line2", "line3"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeBeginSelection})

	beforeRow := model.copyMode.Cursor.Row
	beforeMark := *model.copyMode.Mark
	beforeSnapshot := model.copyMode.Snapshot

	seedCopyModeSnapshot(t, model, []string{"old0", "old1", "old2", "old3"}, []string{"line0", "line1", "line2", "line3"})
	loaded, err := model.runtime.LoadSnapshot(context.Background(), "term-1", 0, 0)
	if err != nil {
		t.Fatalf("load updated snapshot: %v", err)
	}
	_, cmd := model.Update(orchestrator.SnapshotLoadedMsg{TerminalID: "term-1", Snapshot: loaded})
	drainCmd(t, model, cmd, 20)

	if got := model.copyMode.Cursor.Row; got != beforeRow {
		t.Fatalf("expected frozen copy-mode cursor row to stay fixed, before=%d after=%d", beforeRow, got)
	}
	if model.copyMode.Mark == nil {
		t.Fatal("expected mark to remain set")
	}
	if got := model.copyMode.Mark.Row; got != beforeMark.Row {
		t.Fatalf("expected frozen copy-mode mark row to stay fixed, before=%d after=%d", beforeMark.Row, got)
	}
	if model.copyMode.Snapshot != beforeSnapshot {
		t.Fatal("expected copy mode to keep rendering the frozen snapshot while live scrollback changes")
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

func TestCopyModeExitKeepsFrozenSnapshotUntilLiveSnapshotAdvances(t *testing.T) {
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

	frozenView := xansi.Strip(model.View())
	if !strings.Contains(frozenView, "queued-text") {
		t.Fatalf("expected copy-mode exit to keep the frozen snapshot while the live stream is still on the same frame, got:\n%s", frozenView)
	}
	if strings.Contains(frozenView, "transient-live") {
		t.Fatalf("expected transient live frame to stay hidden until the stream advances, got:\n%s", frozenView)
	}

	client.snapshotByTerminal["term-1"] = copyModeTestSnapshot([]string{"hist-a"}, []string{"settled-live"})
	if _, err := model.runtime.LoadSnapshot(context.Background(), "term-1", 0, 0); err != nil {
		t.Fatalf("load settled live snapshot: %v", err)
	}

	liveView := xansi.Strip(model.View())
	if !strings.Contains(liveView, "settled-live") {
		t.Fatalf("expected view to switch back to the live snapshot after the stream advances, got:\n%s", liveView)
	}
	if strings.Contains(liveView, "queued-text") {
		t.Fatalf("expected frozen snapshot override to stop once the live snapshot changes, got:\n%s", liveView)
	}
}

func TestCopyModeEnteringScrollbackForcesViewportScroll(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 12})
	seedCopyModeSnapshot(t, model, []string{"hist0", "hist1", "hist2"}, []string{"live0", "live1"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	before := model.workbench.CurrentTab().ScrollOffset

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})

	if got := model.workbench.CurrentTab().ScrollOffset; got < before || got <= 0 {
		t.Fatalf("expected viewport to enter or remain in scrollback when cursor moves above screen, before=%d after=%d", before, got)
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
	model.modalHost.Picker.Selected = 1

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

func TestClipboardHistoryPickerOpensFromKeysAndRendersOverlay(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 14})
	seedCopyModeSnapshot(t, model, []string{"hist0"}, []string{"live0"})
	model.pushClipboardHistory("first entry", "pane-1")

	dispatchKey(t, model, ctrlKey(tea.KeyCtrlV))
	dispatchKey(t, model, runeKeyMsg('h'))

	if got := model.input.Mode().Kind; got != input.ModePicker {
		t.Fatalf("expected clipboard history picker mode, got %q", got)
	}
	view := xansi.Strip(model.View())
	for _, want := range []string{"Clipboard History", "first entry"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected clipboard history overlay to contain %q:\n%s", want, view)
		}
	}
}

func TestClipboardHistoryPickerShowsEmptyState(t *testing.T) {
	model := setupModel(t, modelOpts{width: 80, height: 14})
	seedCopyModeSnapshot(t, model, []string{"hist0"}, []string{"live0"})

	dispatchKey(t, model, ctrlKey(tea.KeyCtrlV))
	dispatchKey(t, model, runeKeyMsg('h'))

	if got := model.input.Mode().Kind; got != input.ModePicker {
		t.Fatalf("expected clipboard history picker mode, got %q", got)
	}
	view := xansi.Strip(model.View())
	for _, want := range []string{"Clipboard History", "Clipboard history is empty", "copy text first"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected empty clipboard history overlay to contain %q:\n%s", want, view)
		}
	}
}
