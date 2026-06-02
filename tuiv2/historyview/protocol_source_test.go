package historyview

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
)

func TestProtocolSourceLiveSurfaceUsesSnapshotProjection(t *testing.T) {
	client := &fakeProtocolClient{
		snapshot: &protocol.Snapshot{
			Size: protocol.Size{Cols: 80, Rows: 24},
			Screen: protocol.ScreenData{
				Cells: [][]protocol.Cell{
					{{Content: "l", Width: 1}, {Content: "i", Width: 1}, {Content: "v", Width: 1}, {Content: "e", Width: 1}},
				},
				IsAlternateScreen: true,
			},
			Cursor:    protocol.CursorState{Row: 1, Col: 2, Visible: true, Shape: "bar", Blink: true},
			Modes:     protocol.TerminalModes{AlternateScreen: true, AutoWrap: true},
			Timestamp: time.Date(2026, 6, 2, 3, 0, 0, 0, time.UTC),
		},
	}
	source := NewProtocolSource(client)

	surface, err := source.LiveSurface(context.Background(), "term-live")
	if err != nil {
		t.Fatalf("live surface: %v", err)
	}

	if client.snapshotTerminalID != "term-live" || client.snapshotOffset != 0 || client.snapshotLimit != 0 {
		t.Fatalf("unexpected snapshot request terminal=%q offset=%d limit=%d", client.snapshotTerminalID, client.snapshotOffset, client.snapshotLimit)
	}
	if surface.TerminalID != "term-live" || surface.Size != (protocol.Size{Cols: 80, Rows: 24}) || !surface.Screen.IsAlternateScreen {
		t.Fatalf("unexpected live surface header: %#v", surface)
	}
	if got := surfaceRowText(surface.Screen.Cells[0]); got != "live" {
		t.Fatalf("unexpected live surface row %q", got)
	}
	if !surface.Cursor.Visible || surface.Cursor.Shape != "bar" || !surface.Modes.AutoWrap || surface.Timestamp.IsZero() {
		t.Fatalf("unexpected live surface state: %#v", surface)
	}

	client.snapshot.Screen.Cells[0][0].Content = "x"
	if got := surfaceRowText(surface.Screen.Cells[0]); got != "live" {
		t.Fatalf("expected live surface screen rows to be cloned, got %q", got)
	}
}

func TestProtocolSourceLatestHistoryWindowMapsProtocolMetadata(t *testing.T) {
	client := &fakeProtocolClient{
		window: fakeProtocolHistoryWindow(protocol.HistoryWindowReplace, 0),
	}
	source := NewProtocolSource(client)

	window, err := source.LatestHistoryWindow(context.Background(), WindowRequest{TerminalID: "term-1", Token: "ignored-local", Limit: 20, Cols: 80})
	if err != nil {
		t.Fatalf("latest history window: %v", err)
	}

	if client.historyParams != (protocol.HistoryWindowParams{TerminalID: "term-1", BeforeOffset: 0, Limit: 20, Cols: 80}) {
		t.Fatalf("unexpected latest params: %#v", client.historyParams)
	}
	if window.TerminalID != "term-1" || window.Token != "token-1" || window.Op != WindowOpReplace {
		t.Fatalf("unexpected history window header: %#v", window)
	}
	if window.BeforeCursor != 11 || window.LoadedRows != 2 || window.TotalRows != 40 || window.LoadedLines != 2 || window.TotalLines != 25 || !window.HasMore {
		t.Fatalf("unexpected history window totals: %#v", window)
	}
	if window.Generation != 7 || window.FirstBoundaryID != 100 || window.LastBoundaryID != 104 || window.FirstLineID != 9001 || window.LastLineID != 9002 {
		t.Fatalf("unexpected history window boundaries: %#v", window)
	}
	if len(window.Rows) != 2 || historyRowText(window.Rows[0]) != "old" || historyRowText(window.Rows[1]) != "tail" {
		t.Fatalf("unexpected history rows: %#v", window.Rows)
	}
	if window.Rows[0].Kind != RowKindPersisted || window.Rows[1].Kind != RowKindLiveTailReclaimed || !window.Rows[1].Wrapped || window.Rows[0].Timestamp.IsZero() {
		t.Fatalf("unexpected history row metadata: %#v", window.Rows)
	}
	if len(window.Lines) != 2 {
		t.Fatalf("expected two line spans, got %#v", window.Lines)
	}
	if window.Lines[0] != (LineSpan{StartRow: 0, EndRow: 0, Kind: RowKindPersisted, LogicalLineID: 9001, ClippedBefore: true}) {
		t.Fatalf("unexpected first line span: %#v", window.Lines[0])
	}
	if window.Lines[1] != (LineSpan{StartRow: 1, EndRow: 1, Kind: RowKindLiveTailReclaimed, LogicalLineID: 9002, ClippedAfter: true}) {
		t.Fatalf("unexpected second line span: %#v", window.Lines[1])
	}

	client.window.Rows[0].Text = "mutated"
	if got := historyRowText(window.Rows[0]); got != "old" {
		t.Fatalf("expected history rows to be cloned, got %q", got)
	}
}

func TestProtocolSourceOlderHistoryWindowUsesBeforeCursor(t *testing.T) {
	client := &fakeProtocolClient{
		window: fakeProtocolHistoryWindow(protocol.HistoryWindowPrepend, 6),
	}
	source := NewProtocolSource(client)

	window, err := source.OlderHistoryWindow(context.Background(), WindowRequest{TerminalID: "term-1", Token: "token-1", BeforeCursor: 6, Limit: 10, Cols: 100})
	if err != nil {
		t.Fatalf("older history window: %v", err)
	}

	if client.historyParams != (protocol.HistoryWindowParams{TerminalID: "term-1", BeforeOffset: 6, Limit: 10, Cols: 100}) {
		t.Fatalf("unexpected older params: %#v", client.historyParams)
	}
	if window.Op != WindowOpPrepend || window.BeforeCursor != 6 {
		t.Fatalf("unexpected older window metadata: %#v", window)
	}
}

func TestProtocolSourceOlderHistoryWindowRequiresBeforeCursor(t *testing.T) {
	client := &fakeProtocolClient{}
	source := NewProtocolSource(client)

	if _, err := source.OlderHistoryWindow(context.Background(), WindowRequest{TerminalID: "term-1", Token: "token-1", Limit: 10, Cols: 80}); err == nil {
		t.Fatal("expected missing before cursor to be rejected")
	}
	if client.historyRequests != 0 {
		t.Fatalf("expected no protocol request, got %d", client.historyRequests)
	}
}

func fakeProtocolHistoryWindow(op protocol.HistoryWindowOp, beforeOffset int) *protocol.HistoryWindow {
	return &protocol.HistoryWindow{
		TerminalID:    "term-1",
		Token:         "token-1",
		Op:            op,
		Size:          protocol.Size{Cols: 80, Rows: 24},
		Rows:          []protocol.CompactRow{protocol.CompactRowFromCells([]protocol.Cell{{Content: "o", Width: 1}, {Content: "l", Width: 1}, {Content: "d", Width: 1}}), protocol.CompactRowFromCells([]protocol.Cell{{Content: "t", Width: 1}, {Content: "a", Width: 1}, {Content: "i", Width: 1}, {Content: "l", Width: 1}})},
		RowTimestamps: []time.Time{time.Date(2026, 6, 2, 4, 0, 0, 0, time.UTC), time.Date(2026, 6, 2, 4, 1, 0, 0, time.UTC)},
		RowKinds:      []string{"output", "output"},
		RowWrapped:    []bool{false, true},
		RowOwnership:  []string{protocol.RowOwnershipPersisted, protocol.RowOwnershipLiveTailReclaimed},
		Lines: []protocol.HistoryLineSpan{
			{StartRow: 0, EndRow: 0, LogicalLineID: 9001, ClippedBefore: true},
			{StartRow: 1, EndRow: 1, LogicalLineID: 9002, ClippedAfter: true},
		},
		BeforeOffset: beforeOffsetOrDefault(beforeOffset, 11),
		LoadedRows:   2,
		TotalRows:    40,
		LogicalTotal: 25,
		HasMore:      true,
		Generation:   7,
		FirstRowID:   100,
		LastRowID:    104,
		Timestamp:    time.Date(2026, 6, 2, 4, 2, 0, 0, time.UTC),
	}
}

func beforeOffsetOrDefault(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func surfaceRowText(row []protocol.Cell) string {
	var out string
	for _, cell := range row {
		out += cell.Content
	}
	return out
}

func historyRowText(row HistoryRow) string {
	var out string
	for _, cell := range row.Cells.DecodeCells() {
		out += cell.Content
	}
	return out
}

type fakeProtocolClient struct {
	snapshot *protocol.Snapshot
	window   *protocol.HistoryWindow
	err      error

	snapshotTerminalID string
	snapshotOffset     int
	snapshotLimit      int
	historyParams      protocol.HistoryWindowParams
	historyRequests    int
}

func (c *fakeProtocolClient) Snapshot(_ context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error) {
	c.snapshotTerminalID = terminalID
	c.snapshotOffset = offset
	c.snapshotLimit = limit
	if c.err != nil {
		return nil, c.err
	}
	if c.snapshot == nil {
		return nil, errors.New("missing snapshot")
	}
	return c.snapshot, nil
}

func (c *fakeProtocolClient) HistoryWindow(_ context.Context, params protocol.HistoryWindowParams) (*protocol.HistoryWindow, error) {
	c.historyParams = params
	c.historyRequests++
	if c.err != nil {
		return nil, c.err
	}
	if c.window == nil {
		return nil, errors.New("missing history window")
	}
	return c.window, nil
}
