package app

import (
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

func seedCopyModeSnapshot(t *testing.T, m *Model, scrollback, screen []string) {
	t.Helper()
	terminal := m.runtime.Registry().GetOrCreate("term-1")
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
	snapshot := &protocol.Snapshot{
		TerminalID: "term-1",
		Size:       protocol.Size{Cols: uint16(maxCols), Rows: uint16(len(screenRows))},
		Scrollback: sbRows,
		Screen:     protocol.ScreenData{Cells: screenRows},
		Cursor:     protocol.CursorState{Row: maxInt(0, len(screenRows)-1), Col: 0, Visible: true},
		Modes:      protocol.TerminalModes{AutoWrap: true},
	}
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

func TestCopyModePreservesCursorWhenScrollbackExpands(t *testing.T) {
	model := setupModel(t, modelOpts{width: 40, height: 8})
	seedCopyModeSnapshot(t, model, []string{"old2", "old3"}, []string{"line0", "line1", "line2", "line3"})

	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionEnterDisplayMode})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeTop})
	dispatchAction(t, model, input.SemanticAction{Kind: input.ActionCopyModeBeginSelection})

	beforeRow := model.copyMode.Cursor.Row
	beforeMark := *model.copyMode.Mark

	seedCopyModeSnapshot(t, model, []string{"old0", "old1", "old2", "old3"}, []string{"line0", "line1", "line2", "line3"})
	_, cmd := model.Update(orchestrator.SnapshotLoadedMsg{TerminalID: "term-1", Snapshot: model.runtime.Registry().Get("term-1").Snapshot})
	drainCmd(t, model, cmd, 20)

	if got := model.copyMode.Cursor.Row; got != beforeRow+2 {
		t.Fatalf("expected cursor row to shift with prepended scrollback, before=%d after=%d", beforeRow, got)
	}
	if model.copyMode.Mark == nil {
		t.Fatal("expected mark to remain set")
	}
	if got := model.copyMode.Mark.Row; got != beforeMark.Row+2 {
		t.Fatalf("expected mark row to shift with prepended scrollback, before=%d after=%d", beforeMark.Row, got)
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
