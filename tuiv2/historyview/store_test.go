package historyview

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
)

func TestMemoryStoreLatestReplaceInitializesAuthoritativeWindow(t *testing.T) {
	store := NewMemoryStore()
	window := fakeWindow("term-1", WindowOpReplace, "g1:10-12:c80", 10, 12, []string{"one", "two"})
	window.HasMore = true
	window.Lines[0].ClippedBefore = true
	window.Lines[1].ClippedAfter = true

	if accepted := store.ApplyHistoryWindow(window); !accepted {
		t.Fatal("expected latest replace to be accepted")
	}

	got, ok := store.HistoryWindow("term-1")
	if !ok {
		t.Fatal("expected stored history window")
	}
	if got.Token != window.Token || got.Op != WindowOpReplace || !got.HasMore {
		t.Fatalf("unexpected stored header: %#v", got)
	}
	if len(got.Rows) != 2 || rowText(got.Rows[0]) != "one" || rowText(got.Rows[1]) != "two" {
		t.Fatalf("unexpected stored rows: %#v", got.Rows)
	}
	if len(got.Lines) != 2 || !got.Lines[0].ClippedBefore || !got.Lines[1].ClippedAfter {
		t.Fatalf("expected clipped line spans to be preserved: %#v", got.Lines)
	}
}

func TestMemoryStoreOlderPrependRequiresCurrentTokenAndGeneration(t *testing.T) {
	store := NewMemoryStore()
	latest := fakeWindow("term-1", WindowOpReplace, "g2:20-22:c80", 20, 22, []string{"new-a", "new-b"})
	latest.Generation = 2
	if !store.ApplyHistoryWindow(latest) {
		t.Fatal("expected latest replace")
	}
	store.SetViewportTop("term-1", 1)

	stale := fakeWindow("term-1", WindowOpPrepend, "g1:18-19:c80", 18, 19, []string{"stale"})
	stale.Generation = 1
	if store.ApplyHistoryWindow(stale) {
		t.Fatal("expected stale older window to be rejected")
	}

	older := fakeWindow("term-1", WindowOpPrepend, latest.Token, 18, 19, []string{"old-a", "old-b"})
	older.Generation = 2
	older.LastBoundaryID = 19
	if !store.ApplyHistoryWindow(older) {
		t.Fatal("expected matching older window to be accepted")
	}

	got, ok := store.HistoryWindow("term-1")
	if !ok {
		t.Fatal("expected merged history window")
	}
	if len(got.Rows) != 4 {
		t.Fatalf("expected older rows to be prepended, got %d rows", len(got.Rows))
	}
	if texts := []string{rowText(got.Rows[0]), rowText(got.Rows[1]), rowText(got.Rows[2]), rowText(got.Rows[3])}; texts[0] != "old-a" || texts[1] != "old-b" || texts[2] != "new-a" || texts[3] != "new-b" {
		t.Fatalf("unexpected row order after prepend: %#v", texts)
	}
	if got.Lines[2].StartRow != 2 || got.Lines[3].StartRow != 3 {
		t.Fatalf("expected existing line spans to shift after prepend: %#v", got.Lines)
	}
	if top := store.ViewportTop("term-1"); top != 3 {
		t.Fatalf("expected viewport top to keep visual position after prepend, got %d", top)
	}
}

func TestMemoryStoreOlderPrependRejectsOverlappingBoundary(t *testing.T) {
	store := NewMemoryStore()
	latest := fakeWindow("term-1", WindowOpReplace, "g3:30-32:c80", 30, 32, []string{"new"})
	latest.Generation = 3
	latest.FirstBoundaryID = 30
	if !store.ApplyHistoryWindow(latest) {
		t.Fatal("expected latest replace")
	}

	overlap := fakeWindow("term-1", WindowOpPrepend, latest.Token, 29, 30, []string{"overlap"})
	overlap.Generation = 3
	overlap.LastBoundaryID = 30
	if store.ApplyHistoryWindow(overlap) {
		t.Fatal("expected overlapping older boundary to be rejected")
	}
}

func TestMemoryStoreReplaceResetsStaleWindowAndPendingToken(t *testing.T) {
	store := NewMemoryStore()
	store.SetPendingRequest("term-1", "stale-token")
	if pending := store.PendingRequest("term-1"); pending != "stale-token" {
		t.Fatalf("expected pending token, got %q", pending)
	}

	first := fakeWindow("term-1", WindowOpReplace, "g4:40-41:c80", 40, 41, []string{"first"})
	if !store.ApplyHistoryWindow(first) {
		t.Fatal("expected first replace")
	}
	store.SetViewportTop("term-1", 10)

	next := fakeWindow("term-1", WindowOpReplace, "g5:50-51:c80", 50, 51, []string{"next"})
	store.SetPendingRequest("term-1", next.Token)
	if !store.ApplyHistoryWindow(next) {
		t.Fatal("expected next replace")
	}

	got, ok := store.HistoryWindow("term-1")
	if !ok || got.Token != next.Token || rowText(got.Rows[0]) != "next" {
		t.Fatalf("expected replace to reset stored window, got ok=%v window=%#v", ok, got)
	}
	if pending := store.PendingRequest("term-1"); pending != "" {
		t.Fatalf("expected matching pending token to be cleared, got %q", pending)
	}
	if top := store.ViewportTop("term-1"); top != 0 {
		t.Fatalf("expected viewport top to clamp after replace, got %d", top)
	}
}

func TestMemoryStoreTracksCopyModeInteractionState(t *testing.T) {
	store := NewMemoryStore()
	if _, ok := store.Cursor("term-1"); ok {
		t.Fatal("expected no cursor before interaction state is set")
	}
	cursor := Cursor{Row: 2, Col: 4, LogicalLineID: 42, LogicalOffset: 7}
	store.SetCursor("term-1", cursor)
	if got, ok := store.Cursor("term-1"); !ok || got != cursor {
		t.Fatalf("unexpected cursor state ok=%v cursor=%#v", ok, got)
	}

	selection := Selection{
		Active: true,
		Anchor: Cursor{Row: 1, Col: 0, LogicalLineID: 41},
		Focus:  Cursor{Row: 3, Col: 8, LogicalLineID: 43, LogicalOffset: 9},
	}
	store.SetSelection("term-1", selection)
	if got, ok := store.Selection("term-1"); !ok || got != selection {
		t.Fatalf("unexpected selection state ok=%v selection=%#v", ok, got)
	}
	store.ClearSelection("term-1")
	if _, ok := store.Selection("term-1"); ok {
		t.Fatal("expected selection to be cleared")
	}
}

func TestFakeSourceDrivesLatestAndOlderRequests(t *testing.T) {
	source := &fakeSource{
		latest: fakeWindow("term-1", WindowOpReplace, "g6:60-61:c80", 60, 61, []string{"latest"}),
		older:  fakeWindow("term-1", WindowOpPrepend, "g6:60-61:c80", 58, 59, []string{"older"}),
	}
	store := NewMemoryStore()
	ctx := context.Background()

	latest, err := source.LatestHistoryWindow(ctx, WindowRequest{TerminalID: "term-1", Limit: 20, Cols: 80})
	if err != nil {
		t.Fatalf("latest history window: %v", err)
	}
	if !store.ApplyHistoryWindow(latest) {
		t.Fatal("expected latest window to be accepted")
	}
	older, err := source.OlderHistoryWindow(ctx, WindowRequest{TerminalID: "term-1", Token: latest.Token, BeforeCursor: latest.BeforeCursor, Limit: 20, Cols: 80})
	if err != nil {
		t.Fatalf("older history window: %v", err)
	}
	if !store.ApplyHistoryWindow(older) {
		t.Fatal("expected older window to be accepted")
	}
	if source.latestRequests != 1 || source.olderRequests != 1 {
		t.Fatalf("unexpected request counts latest=%d older=%d", source.latestRequests, source.olderRequests)
	}
}

func fakeWindow(terminalID string, op WindowOp, token WindowToken, firstID, lastID uint64, texts []string) HistoryWindow {
	rows := make([]HistoryRow, len(texts))
	lines := make([]LineSpan, len(texts))
	for i, text := range texts {
		id := firstID + uint64(i)
		rows[i] = HistoryRow{
			Cells:     protocol.CompactRowFromCells([]protocol.Cell{{Content: text, Width: len(text)}}),
			Kind:      RowKindPersisted,
			Timestamp: time.Date(2026, 6, 2, 1, i, 0, 0, time.UTC),
		}
		lines[i] = LineSpan{
			StartRow:      i,
			EndRow:        i,
			Kind:          RowKindPersisted,
			LogicalLineID: id,
		}
	}
	return HistoryWindow{
		TerminalID:      terminalID,
		Token:           token,
		Op:              op,
		Size:            protocol.Size{Cols: 80, Rows: 24},
		Rows:            rows,
		Lines:           lines,
		LoadedRows:      len(rows),
		TotalRows:       len(rows),
		LoadedLines:     len(lines),
		TotalLines:      len(lines),
		BeforeCursor:    int(firstID),
		Generation:      1,
		FirstLineID:     firstID,
		LastLineID:      lastID,
		FirstBoundaryID: firstID,
		LastBoundaryID:  lastID,
		Timestamp:       time.Date(2026, 6, 2, 2, 0, 0, 0, time.UTC),
	}
}

func rowText(row HistoryRow) string {
	var out string
	for _, cell := range row.Cells.DecodeCells() {
		out += cell.Content
	}
	return out
}

type fakeSource struct {
	latest         HistoryWindow
	older          HistoryWindow
	err            error
	latestRequests int
	olderRequests  int
}

func (s *fakeSource) LiveSurface(context.Context, string) (LiveSurface, error) {
	if s.err != nil {
		return LiveSurface{}, s.err
	}
	return LiveSurface{TerminalID: "term-1"}, nil
}

func (s *fakeSource) LatestHistoryWindow(context.Context, WindowRequest) (HistoryWindow, error) {
	s.latestRequests++
	if s.err != nil {
		return HistoryWindow{}, s.err
	}
	if s.latest.TerminalID == "" {
		return HistoryWindow{}, errors.New("missing latest fake window")
	}
	return s.latest, nil
}

func (s *fakeSource) OlderHistoryWindow(_ context.Context, request WindowRequest) (HistoryWindow, error) {
	s.olderRequests++
	if s.err != nil {
		return HistoryWindow{}, s.err
	}
	if s.older.TerminalID == "" {
		return HistoryWindow{}, errors.New("missing older fake window")
	}
	if request.BeforeCursor <= 0 {
		return HistoryWindow{}, errors.New("missing older before cursor")
	}
	s.older.Token = request.Token
	return s.older, nil
}
