package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/protocol"
	localvterm "github.com/lozzow/termx/vterm"
)

func TestResizePreviewNonAltShrinkReflowsHardColumns(t *testing.T) {
	source := snapshotWithLines("term-1", 64, 20, []string{"COL_A                 COL_B                 COL_C"})

	preview := provisionalSnapshotForResizePreview(source, 20, 6)

	if preview == nil {
		t.Fatal("expected preview snapshot")
	}
	for _, want := range []string{"COL_A", "COL_B", "COL_C"} {
		if !snapshotContainsAnyRow(preview, want) {
			t.Fatalf("expected shrink preview to contain %q, got rows %q", want, snapshotRowsText(preview))
		}
	}
	if preview.Size.Cols != 20 || preview.Size.Rows != 6 {
		t.Fatalf("expected preview size 20x6, got %dx%d", preview.Size.Cols, preview.Size.Rows)
	}
}

func TestResizePreviewNonAltShrinkSplitsByCellWidthNotWhitespace(t *testing.T) {
	source := snapshotWithLines("term-1", 32, 8, []string{"terminalmeta"})

	preview := provisionalSnapshotForResizePreview(source, 3, 8)

	if preview == nil {
		t.Fatal("expected preview snapshot")
	}
	if got := rowText(preview.Screen.Cells[0]); got != "ter" {
		t.Fatalf("expected first split segment to be %q, got %q in rows %q", "ter", got, snapshotRowsText(preview))
	}
	if got := rowText(preview.Screen.Cells[1]); got != "min" {
		t.Fatalf("expected second split segment to continue by cells, got %q in rows %q", got, snapshotRowsText(preview))
	}
	if got := rowText(preview.Screen.Cells[2]); got != "alm" {
		t.Fatalf("expected third split segment to continue by cells, got %q in rows %q", got, snapshotRowsText(preview))
	}
}

func TestResizePreviewNonAltShrinkPreservesSplitWhitespaceCells(t *testing.T) {
	source := snapshotWithLines("term-1", 8, 4, []string{"ab   cd"})

	preview := provisionalSnapshotForResizePreview(source, 4, 4)

	if preview == nil {
		t.Fatal("expected preview snapshot")
	}
	if got := preview.Screen.Cells[0][2].Content; got != " " {
		t.Fatalf("expected split to preserve first source space cell at row 0 col 2, got %#v in rows %q", preview.Screen.Cells[0][2], snapshotRowsText(preview))
	}
	if got := preview.Screen.Cells[0][3].Content; got != " " {
		t.Fatalf("expected split to preserve second source space cell at row 0 col 3, got %#v in rows %q", preview.Screen.Cells[0][3], snapshotRowsText(preview))
	}
	if got := rowText(preview.Screen.Cells[1]); got != " cd" {
		t.Fatalf("expected continuation to preserve leading source space, got %q in rows %q", got, snapshotRowsText(preview))
	}
}

func TestResizePreviewNonAltHardLinesDoNotJoinOnExpand(t *testing.T) {
	source := snapshotWithLines("term-1", 6, 4, []string{"abc", "def"})

	preview := provisionalSnapshotForResizePreview(source, 12, 4)

	if preview == nil {
		t.Fatal("expected preview snapshot")
	}
	if got := rowText(preview.Screen.Cells[0]); got != "abc" {
		t.Fatalf("expected first hard line to stay separate, got %q in rows %q", got, snapshotRowsText(preview))
	}
	if got := rowText(preview.Screen.Cells[1]); got != "def" {
		t.Fatalf("expected second hard line to stay separate, got %q in rows %q", got, snapshotRowsText(preview))
	}
}

func TestResizePreviewNonAltWrappedLinesJoinOnExpand(t *testing.T) {
	source := snapshotWithLines("term-1", 5, 4, []string{"abcde", "fgh"})
	source.ScreenRowKinds = []string{"", protocol.SnapshotRowKindWrapped, "", ""}

	preview := provisionalSnapshotForResizePreview(source, 8, 4)

	if preview == nil {
		t.Fatal("expected preview snapshot")
	}
	if got := rowText(preview.Screen.Cells[0]); got != "abcdefgh" {
		t.Fatalf("expected wrapped source rows to join on expand, got %q in rows %q", got, snapshotRowsText(preview))
	}
	if got := rowText(preview.Screen.Cells[1]); got != "" {
		t.Fatalf("expected joined continuation row to clear, got %q in rows %q", got, snapshotRowsText(preview))
	}
}

func TestResizePreviewNonAltSplitMarksContinuationRowsWrapped(t *testing.T) {
	source := snapshotWithLines("term-1", 12, 4, []string{"terminalmeta"})

	preview := provisionalSnapshotForResizePreview(source, 3, 6)

	if preview == nil {
		t.Fatal("expected preview snapshot")
	}
	if got := preview.ScreenRowKinds[0]; got == protocol.SnapshotRowKindWrapped {
		t.Fatalf("expected first split segment not to be marked wrapped, got %#v", preview.ScreenRowKinds)
	}
	for row := 1; row < 4; row++ {
		if got := preview.ScreenRowKinds[row]; got != protocol.SnapshotRowKindWrapped {
			t.Fatalf("expected continuation row %d to be wrapped, got %#v", row, preview.ScreenRowKinds)
		}
	}
}

func TestResizePreviewNonAltAnchorsToCapturedVisibleTopAfterHistory(t *testing.T) {
	source := snapshotWithLines("term-1", 20, 4, []string{"visible-one", "visible-two", "visible-three", "visible-four"})
	source.Scrollback = [][]protocol.Cell{
		testProtocolCellsFromString("history-one", 20),
		testProtocolCellsFromString("history-two", 20),
	}
	source.ScrollbackTimestamps = make([]time.Time, len(source.Scrollback))
	source.ScrollbackRowKinds = make([]string, len(source.Scrollback))

	preview := provisionalSnapshotForResizePreview(source, 20, 4)

	if preview == nil {
		t.Fatal("expected preview snapshot")
	}
	if got := rowText(preview.Screen.Cells[0]); got != "visible-one" {
		t.Fatalf("expected viewport to start at captured visible top, got %q in rows %q", got, snapshotRowsText(preview))
	}
	if got := rowText(preview.Screen.Cells[3]); got != "visible-four" {
		t.Fatalf("expected viewport to preserve captured visible rows, got %q in rows %q", got, snapshotRowsText(preview))
	}
}

func TestResizePreviewNonAltMapsCursorThroughReflow(t *testing.T) {
	source := snapshotWithLines("term-1", 12, 4, []string{"terminalmeta"})
	source.Cursor = protocol.CursorState{Row: 0, Col: 8, Visible: true}

	preview := provisionalSnapshotForResizePreview(source, 3, 6)

	if preview == nil {
		t.Fatal("expected preview snapshot")
	}
	if !preview.Cursor.Visible {
		t.Fatalf("expected cursor to remain visible, got %#v", preview.Cursor)
	}
	if preview.Cursor.Row != 2 || preview.Cursor.Col != 2 {
		t.Fatalf("expected cursor at reflowed row=2 col=2, got %#v in rows %q", preview.Cursor, snapshotRowsText(preview))
	}
}

func TestResizePreviewNonAltShrinkExpandRestoresFromOriginalSource(t *testing.T) {
	source := snapshotWithLines("term-1", 64, 3, []string{"COL_A                 COL_B                 COL_C"})
	shrink := provisionalSnapshotForResizePreview(source, 20, 6)
	if shrink == nil || !snapshotContainsAnyRow(shrink, "COL_C") {
		t.Fatalf("expected shrink preview to retain COL_C, got rows %q", snapshotRowsText(shrink))
	}

	expand := provisionalSnapshotForResizePreview(source, 64, 3)

	if expand == nil {
		t.Fatal("expected expanded preview snapshot")
	}
	if got := rowText(expand.Screen.Cells[0]); !strings.Contains(got, "COL_A                 COL_B                 COL_C") {
		t.Fatalf("expected expanded preview to restore original row, got %q", got)
	}
}

func TestResizePreviewAltScreenCropAndRestoreGrid(t *testing.T) {
	source := snapshotWithLines("term-1", 6, 2, []string{"ABCDEF", "UVWXYZ"})
	source.Screen.IsAlternateScreen = true
	source.Modes.AlternateScreen = true

	shrink := provisionalSnapshotForResizePreview(source, 3, 2)
	expand := provisionalSnapshotForResizePreview(source, 6, 2)

	if got := rowText(shrink.Screen.Cells[0]); got != "ABC" {
		t.Fatalf("expected alt shrink to crop first row, got %q", got)
	}
	if strings.Contains(snapshotRowsText(shrink), "DEF") {
		t.Fatalf("expected alt shrink not to text-reflow cropped cells into later rows, got %q", snapshotRowsText(shrink))
	}
	if got := rowText(expand.Screen.Cells[0]); got != "ABCDEF" {
		t.Fatalf("expected alt expand to restore source grid, got %q", got)
	}
	if got := rowText(expand.Screen.Cells[1]); got != "UVWXYZ" {
		t.Fatalf("expected alt expand to restore second source row, got %q", got)
	}
}

func TestResizePreviewOutputExitsPreviewButKeepsSourceForResizeBurst(t *testing.T) {
	terminal := &TerminalRuntime{
		TerminalID:            "term-1",
		Snapshot:              snapshotWithLines("term-1", 20, 3, []string{"before"}),
		ResizePreviewSource:   snapshotWithLines("term-1", 40, 3, []string{"before source"}),
		PreferSnapshot:        true,
		VTerm:                 localvterm.New(20, 3, 100, nil),
		BootstrapPending:      true,
		SurfaceVersion:        1,
		SnapshotVersion:       1,
		ScrollbackExhausted:   true,
		ScrollbackLoadedLimit: 1,
	}
	rt := New(newFakeBridgeClient())

	rt.handleOutputFrame(terminal, "term-1", protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("AFTER_REAL_OUTPUT")})

	if terminal.ResizePreviewSource == nil {
		t.Fatal("expected real output to keep resize source for shrink-expand burst")
	}
	if !terminal.PreferSnapshot {
		t.Fatal("expected resize burst output to keep snapshot preference")
	}
	if terminal.BootstrapPending {
		t.Fatal("expected real output to clear bootstrap pending")
	}
}

func TestResizePreviewNoopScreenUpdateDoesNotClearPreviewSource(t *testing.T) {
	terminal := &TerminalRuntime{
		TerminalID:          "term-1",
		Snapshot:            snapshotWithLines("term-1", 20, 3, []string{"before"}),
		ResizePreviewSource: snapshotWithLines("term-1", 40, 3, []string{"before source"}),
		PreferSnapshot:      true,
		VTerm:               localvterm.New(20, 3, 100, nil),
	}
	rt := New(newFakeBridgeClient())
	contract := NewScreenUpdateContract(protocol.ScreenUpdate{
		Size:   protocol.Size{Cols: 20, Rows: 3},
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})

	rt.applyDecodedScreenUpdateContract(terminal, "term-1", contract)

	if terminal.ResizePreviewSource == nil {
		t.Fatal("expected noop screen update to keep resize preview source")
	}
}

func TestResizePreviewContentScreenUpdateExitsPreviewButKeepsSourceForResizeBurst(t *testing.T) {
	terminal := &TerminalRuntime{
		TerminalID:          "term-1",
		Snapshot:            snapshotWithLines("term-1", 20, 3, []string{"before"}),
		ResizePreviewSource: snapshotWithLines("term-1", 40, 3, []string{"before source"}),
		PreferSnapshot:      true,
		VTerm:               localvterm.New(20, 3, 100, nil),
	}
	rt := New(newFakeBridgeClient())
	contract := NewScreenUpdateContract(protocol.ScreenUpdate{
		Size: protocol.Size{Cols: 20, Rows: 3},
		ChangedSpans: []protocol.ScreenSpanUpdate{{
			Row:      0,
			ColStart: 0,
			Cells:    []protocol.Cell{{Content: "A", Width: 1}},
		}},
		Cursor: protocol.CursorState{Visible: true},
		Modes:  protocol.TerminalModes{AutoWrap: true},
	})

	rt.applyDecodedScreenUpdateContract(terminal, "term-1", contract)

	if terminal.ResizePreviewSource == nil {
		t.Fatal("expected content screen update to keep resize source for shrink-expand burst")
	}
}

func TestResizePreviewNextUserInputClearsPreviewSource(t *testing.T) {
	client := newFakeBridgeClient()
	client.attachResult = &protocol.AttachResult{Channel: 7, Mode: "collaborator"}
	rt := New(client)
	terminal := rt.Registry().GetOrCreate("term-1")
	terminal.Channel = 7
	terminal.BoundPaneIDs = []string{"pane-1"}
	terminal.ResizePreviewSource = snapshotWithLines("term-1", 40, 3, []string{"before source"})
	binding := rt.BindPane("pane-1")
	binding.Channel = 7
	binding.Connected = true

	if err := rt.SendInput(context.Background(), "pane-1", []byte("x")); err != nil {
		t.Fatalf("send input: %v", err)
	}
	if terminal.ResizePreviewSource != nil {
		t.Fatal("expected user input to clear resize preview source")
	}
}

func TestCaptureResizePreviewSourceCarriesVTermWrappedRows(t *testing.T) {
	vt := localvterm.New(5, 3, 100, nil)
	if _, err := vt.Write([]byte("abcdef")); err != nil {
		t.Fatalf("write wrapped output: %v", err)
	}
	terminal := &TerminalRuntime{TerminalID: "term-1", VTerm: vt}

	source := captureResizePreviewSource("term-1", terminal, nil, vt)

	if source == nil {
		t.Fatal("expected resize preview source")
	}
	if len(source.ScreenRowKinds) < 2 {
		t.Fatalf("expected screen row kinds, got %#v", source.ScreenRowKinds)
	}
	if source.ScreenRowKinds[1] != protocol.SnapshotRowKindWrapped {
		t.Fatalf("expected captured continuation row to be wrapped, got %#v", source.ScreenRowKinds)
	}
}

func snapshotContainsAnyRow(snapshot *protocol.Snapshot, want string) bool {
	return strings.Contains(snapshotRowsText(snapshot), want)
}

func snapshotRowsText(snapshot *protocol.Snapshot) string {
	if snapshot == nil {
		return ""
	}
	var builder strings.Builder
	for rowIndex, row := range snapshot.Screen.Cells {
		if rowIndex > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(rowText(row))
	}
	return builder.String()
}

func rowText(row []protocol.Cell) string {
	var builder strings.Builder
	for _, cell := range row {
		builder.WriteString(cell.Content)
	}
	return strings.TrimRight(builder.String(), " ")
}

func testProtocolCellsFromString(value string, cols int) []protocol.Cell {
	row := make([]protocol.Cell, cols)
	for col := range row {
		row[col] = protocol.Cell{Content: " ", Width: 1}
	}
	for col := 0; col < len(value) && col < cols; col++ {
		row[col] = protocol.Cell{Content: string(value[col]), Width: 1}
	}
	return row
}
