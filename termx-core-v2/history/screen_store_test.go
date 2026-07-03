package history

import (
	"errors"
	"strings"
	"testing"
)

func TestR425ScreenBackedStoreLatestOlderFreezeCopy(t *testing.T) {
	buffer := NewScreenHistoryBuffer(12, 1)
	if err := buffer.ApplyTransaction(TerminalSemanticTransaction{
		Seq: 1,
		Ops: []TerminalSemanticOp{
			screenBufferWriteOp(0, 0, "sealed"),
			screenBufferControlOp("lf"),
			screenBufferWriteOp(0, 0, "current"),
		},
	}); err != nil {
		t.Fatalf("apply screen history rows: %v", err)
	}
	store := NewScreenBackedHistoryStore("term-r425", buffer)

	latest, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-r425", Limit: 1, Cols: 12})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if got := r425HistoryTexts(latest.Rows); got != "current" {
		t.Fatalf("latest should be projected from current screen row, got %q window=%#v", got, latest)
	}
	if latest.LogicalTotal != 2 || !latest.HasMore || !latest.Boundary.Cursor.Valid || latest.Boundary.Cursor.BeforeRowIndex != 1 {
		t.Fatalf("latest must expose total rows and older cursor, window=%#v", latest)
	}

	older, err := store.OlderWindow(HistoryWindowRequest{TerminalID: "term-r425", Limit: 1, Cols: 12, Cursor: latest.Boundary.Cursor})
	if err != nil {
		t.Fatalf("older window: %v", err)
	}
	if got := r425HistoryTexts(older.Rows); got != "sealed" {
		t.Fatalf("older should page into sealed physical rows, got %q window=%#v", got, older)
	}
	if older.LogicalTotal != 2 || older.HasMore {
		t.Fatalf("older must preserve projection total and hit oldest boundary, window=%#v", older)
	}

	snapshot, err := store.Freeze(FreezeHistoryRequest{TerminalID: "term-r425", Limit: 1, Cols: 12})
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	if snapshot.Token == "" || !snapshot.Boundary.Cursor.Valid {
		t.Fatalf("freeze should create token and latest boundary, snapshot=%#v", snapshot)
	}
	if err := buffer.ApplyTransaction(TerminalSemanticTransaction{
		Seq: 2,
		Ops: []TerminalSemanticOp{screenBufferWriteOp(0, 0, "mutated")},
	}); err != nil {
		t.Fatalf("mutate screen after freeze: %v", err)
	}

	liveAfterFreeze, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-r425", Limit: 1, Cols: 12})
	if err != nil {
		t.Fatalf("latest after freeze: %v", err)
	}
	if got := r425HistoryTexts(liveAfterFreeze.Rows); got != "mutated" {
		t.Fatalf("live latest should follow screen mutation, got %q window=%#v", got, liveAfterFreeze)
	}
	frozenLatest, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-r425", Token: snapshot.Token, Limit: 1, Cols: 12})
	if err != nil {
		t.Fatalf("frozen latest: %v", err)
	}
	if got := r425HistoryTexts(frozenLatest.Rows); got != "current" {
		t.Fatalf("frozen latest must not be rewritten by later repaint, got %q window=%#v", got, frozenLatest)
	}
	if frozenLatest.Generation != snapshot.Generation || frozenLatest.LogicalTotal != 2 {
		t.Fatalf("frozen latest should retain snapshot generation and total, snapshot=%#v window=%#v", snapshot, frozenLatest)
	}

	copied, err := store.Copy(HistoryCopyRequest{TerminalID: "term-r425", Token: snapshot.Token, Cols: 12})
	if err != nil {
		t.Fatalf("copy frozen projection: %v", err)
	}
	if copied != "sealed\ncurrent" {
		t.Fatalf("copy should read frozen screen-backed rows, got %q", copied)
	}
	if err := store.Release(snapshot.Token); err != nil {
		t.Fatalf("release token: %v", err)
	}
	if _, err := store.LatestWindow(HistoryWindowRequest{TerminalID: "term-r425", Token: snapshot.Token, Limit: 1, Cols: 12}); !errors.Is(err, ErrHistoryInvalidMutation) {
		t.Fatalf("released token should be invalid, got %v", err)
	}
}

func TestR425ScreenBackedStoreOldestNewerAndReadState(t *testing.T) {
	buffer := NewScreenHistoryBuffer(12, 1)
	if err := buffer.ApplyTransaction(TerminalSemanticTransaction{
		Seq: 1,
		Ops: []TerminalSemanticOp{
			screenBufferWriteOp(0, 0, "one"),
			screenBufferControlOp("lf"),
			screenBufferWriteOp(0, 0, "two"),
			screenBufferControlOp("lf"),
			screenBufferWriteOp(0, 0, "three"),
		},
	}); err != nil {
		t.Fatalf("apply three screen rows: %v", err)
	}
	store := NewScreenBackedHistoryStore("term-r425", buffer)

	state := store.ReadState()
	if !state.HasTimeline || state.HasPrimaryCurrent || state.HasAltCurrent || state.Generation != 1 {
		t.Fatalf("read state should reflect sealed rows without treating ordinary current rows as primary frame, state=%#v", state)
	}
	oldest, err := store.OldestWindow(HistoryWindowRequest{TerminalID: "term-r425", Limit: 1, Cols: 12})
	if err != nil {
		t.Fatalf("oldest window: %v", err)
	}
	if got := r425HistoryTexts(oldest.Rows); got != "one" {
		t.Fatalf("oldest should return projection head, got %q window=%#v", got, oldest)
	}
	if oldest.Op != HistoryWindowReplace || oldest.LogicalTotal != 3 || !oldest.HasMore || !oldest.Boundary.Cursor.Valid || oldest.Boundary.Cursor.BeforeRowIndex != 1 {
		t.Fatalf("oldest should expose replace page and newer cursor, window=%#v", oldest)
	}
	newer, err := store.NewerWindow(HistoryWindowRequest{TerminalID: "term-r425", Limit: 1, Cols: 12, Cursor: oldest.Boundary.Cursor})
	if err != nil {
		t.Fatalf("newer window: %v", err)
	}
	if got := r425HistoryTexts(newer.Rows); got != "two" {
		t.Fatalf("newer should page toward current rows, got %q window=%#v", got, newer)
	}
	if newer.Op != HistoryWindowAppend || newer.LogicalTotal != 3 || !newer.HasMore || !newer.Boundary.Cursor.Valid || newer.Boundary.Cursor.BeforeRowIndex != 2 {
		t.Fatalf("newer should preserve total and next cursor, window=%#v", newer)
	}
}

func r425HistoryTexts(rows []HistoryRow) string {
	texts := make([]string, 0, len(rows))
	for _, row := range rows {
		texts = append(texts, rowText(row.Cells))
	}
	return strings.Join(texts, "|")
}
