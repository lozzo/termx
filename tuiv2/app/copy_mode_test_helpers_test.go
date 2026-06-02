package app

import (
	"strings"
	"testing"

	"github.com/lozzow/termx/internal/protocol"
)

type recordingControlWriter struct {
	cursor   string
	controls []string
	queued   []string
}

func (w *recordingControlWriter) SetCursorSequence(seq string) {
	w.cursor = seq
}

func (w *recordingControlWriter) WriteControlSequence(seq string) error {
	w.controls = append(w.controls, seq)
	return nil
}

func (w *recordingControlWriter) QueueControlSequenceAfterWrite(seq string) {
	w.queued = append(w.queued, seq)
}

func terminalSnapshotFixture(scrollback, screen []string) *protocol.Snapshot {
	sbRows := make([][]protocol.Cell, 0, len(scrollback))
	for _, row := range scrollback {
		sbRows = append(sbRows, protocolRowFromText(row, 80))
	}
	screenRows := make([][]protocol.Cell, 0, len(screen))
	for _, row := range screen {
		screenRows = append(screenRows, protocolRowFromText(row, 80))
	}
	return &protocol.Snapshot{
		TerminalID:             "term-1",
		Size:                   protocol.Size{Cols: 80, Rows: uint16(maxInt(1, len(screenRows)))},
		Scrollback:             protocol.CompactRowsFromCells(sbRows),
		ScrollbackTotal:        len(sbRows),
		ScrollbackLogicalTotal: len(sbRows),
		ScrollbackLoadedRows:   len(sbRows),
		ScrollbackOwnership:    repeatedTestOwnership(protocol.RowOwnershipPersisted, len(sbRows)),
		Screen:                 protocol.ScreenData{Cells: screenRows},
		ScreenOwnership:        repeatedTestOwnership(protocol.RowOwnershipScreen, len(screenRows)),
		Cursor:                 protocol.CursorState{Row: maxInt(0, len(screenRows)-1), Col: 0, Visible: true},
		Modes:                  protocol.TerminalModes{AutoWrap: true},
	}
}

func seedTerminalSnapshotFixture(t *testing.T, m *Model, scrollback, screen []string) {
	t.Helper()
	seedTerminalSnapshotFixtureForTerminal(t, m, "term-1", scrollback, screen)
}

func seedTerminalSnapshotFixtureForTerminal(t *testing.T, m *Model, terminalID string, scrollback, screen []string) {
	t.Helper()
	if m == nil || m.runtime == nil {
		t.Fatal("model runtime is nil")
	}
	snapshot := terminalSnapshotFixture(scrollback, screen)
	snapshot.TerminalID = terminalID
	terminal := m.runtime.Registry().GetOrCreate(terminalID)
	terminal.Snapshot = snapshot
	terminal.SnapshotVersion = terminal.SurfaceVersion
	if client, ok := m.runtime.Client().(*recordingBridgeClient); ok {
		if client.snapshotByTerminal == nil {
			client.snapshotByTerminal = map[string]*protocol.Snapshot{}
		}
		client.snapshotByTerminal[terminalID] = cloneSnapshot(snapshot)
	}
}

func setupSplitCopyModeModel(t *testing.T) *Model {
	t.Helper()
	model := setupModel(t, modelOpts{width: 80, height: 16})
	if err := model.workbench.SplitPane("tab-1", "pane-1", "pane-2", "right"); err != nil {
		t.Fatalf("split pane: %v", err)
	}
	if err := model.workbench.BindPaneTerminal("tab-1", "pane-2", "term-2"); err != nil {
		t.Fatalf("bind pane terminal: %v", err)
	}
	seedTerminalSnapshotFixtureForTerminal(t, model, "term-2", []string{"hist-2"}, []string{"live-2"})
	return model
}

func protocolRowFromText(text string, cols int) []protocol.Cell {
	if cols <= 0 {
		cols = len([]rune(text))
	}
	cells := make([]protocol.Cell, 0, cols)
	for _, r := range []rune(text) {
		cells = append(cells, protocol.Cell{Content: string(r), Width: 1})
	}
	for len(cells) < cols {
		cells = append(cells, protocol.Cell{Content: " ", Width: 1})
	}
	return cells
}

func rowTextFromCompactRow(row protocol.CompactRow) string {
	var b strings.Builder
	for _, cell := range row.DecodeCells() {
		b.WriteString(cell.Content)
	}
	return strings.TrimRight(b.String(), " ")
}

func repeatedTestOwnership(value string, count int) []string {
	if count <= 0 || value == "" {
		return nil
	}
	out := make([]string, count)
	for i := range out {
		out[i] = value
	}
	return out
}
