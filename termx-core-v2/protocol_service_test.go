package termxcorev2

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-core-v2/history"
	"github.com/lozzow/termx/termx-proto/wire"
	"github.com/lozzow/termx/termx-shared/transport/memory"
)

func TestProtocolServiceCreateListMetadataRestartRemove(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	created, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Name:    "demo",
		Tags:    map[string]string{"role": "test"},
		Command: []string{"shell"},
		Size:    protocol.Size{Cols: 12, Rows: 4},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.TerminalID != "term-1" || created.State != string(TerminalStateRunning) {
		t.Fatalf("unexpected create result %#v", created)
	}

	list, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Terminals) != 1 || list.Terminals[0].Name != "demo" || list.Terminals[0].Tags["role"] != "test" {
		t.Fatalf("unexpected list result %#v", list)
	}

	if err := client.SetMetadata(context.Background(), "term-1", "renamed", map[string]string{"role": "updated"}); err != nil {
		t.Fatalf("set metadata: %v", err)
	}
	var info protocol.TerminalInfo
	if err := client.Call(context.Background(), "get", protocol.GetParams{TerminalID: "term-1"}, &info); err != nil {
		t.Fatalf("get: %v", err)
	}
	if info.Name != "renamed" || info.Tags["role"] != "updated" {
		t.Fatalf("unexpected terminal metadata %#v", info)
	}

	if err := client.Restart(context.Background(), "term-1"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if process := serverProcess(t, server, "term-1"); process == nil {
		t.Fatal("expected restarted process")
	}

	if err := client.Remove(context.Background(), "term-1"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := server.GetTerminal("term-1"); !errors.Is(err, ErrTerminalNotFound) {
		t.Fatalf("expected removed terminal, got %v", err)
	}
}

func TestProtocolServiceHistoryWindowUsesCoreTruth(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 3}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour\nfive"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       10,
		Limit:      2,
	})
	if err != nil {
		t.Fatalf("latest history.window: %v", err)
	}
	if latest.Op != protocol.HistoryWindowReplace || latest.Size.Cols != 10 || latest.LogicalTotal != 1 || len(latest.Rows) != 2 {
		t.Fatalf("unexpected latest window %#v", latest)
	}
	if rowText(latest.Rows[0]) != "four" || rowText(latest.Rows[1]) != "five" {
		t.Fatalf("unexpected latest rows %#v", latest.Rows)
	}
	if latest.RowLineIDs[0] == 0 || latest.RowInLine[0] != 0 || latest.Generation == 0 || latest.Token == "" {
		t.Fatalf("expected line mapping, generation and token, got %#v", latest)
	}
	if latest.RowOwnership[0] != protocol.RowOwnershipLiveTailLive || latest.RowOwnership[1] != protocol.RowOwnershipLiveTailLive {
		t.Fatalf("unexpected row ownership %#v", latest.RowOwnership)
	}
	if !latest.HasMore || !latest.CursorValid {
		t.Fatalf("latest tail should still expose older committed rows from the same frozen snapshot, got %#v", latest)
	}

	older, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:      "term-1",
		Cols:            10,
		Limit:           1,
		CursorValid:     latest.CursorValid,
		BeforeLineID:    latest.CursorLineID,
		BeforeRowInLine: latest.CursorRow,
		Token:           latest.Token,
		Generation:      latest.Generation,
	})
	if err != nil {
		t.Fatalf("older history.window: %v", err)
	}
	if older.Op != protocol.HistoryWindowPrepend || older.Token != latest.Token || len(older.Rows) != 1 || rowText(older.Rows[0]) != "one" {
		t.Fatalf("unexpected older window %#v", older)
	}
	if len(older.RowOwnership) != 1 || older.RowOwnership[0] != protocol.RowOwnershipPersisted {
		t.Fatalf("older committed row should stay persisted, got %#v", older.RowOwnership)
	}

	_, err = client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:          "term-1",
		Cols:                10,
		Limit:               1,
		CursorValid:         true,
		BeforeLineID:        latest.CursorLineID,
		BeforeRowInLine:     latest.CursorRow,
		Token:               "stale-token",
		Generation:          latest.Generation,
		BoundaryFirstLineID: latest.FirstLineID,
		BoundaryLastLineID:  latest.LastLineID,
	})
	if err == nil || !strings.Contains(err.Error(), ErrStaleHistoryWindow.Error()) {
		t.Fatalf("expected stale history window error, got %v", err)
	}
	olderAtReflowCols, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:          "term-1",
		Cols:                11,
		Limit:               1,
		CursorValid:         true,
		BeforeLineID:        latest.CursorLineID,
		BeforeRowInLine:     latest.CursorRow,
		Token:               latest.Token,
		Generation:          latest.Generation,
		BoundaryFirstLineID: latest.FirstLineID,
		BoundaryLastLineID:  latest.LastLineID,
	})
	if err != nil {
		t.Fatalf("expected frozen older to accept local reflow cols when cursor/boundary still valid, got %v", err)
	}
	if olderAtReflowCols.Op != protocol.HistoryWindowPrepend || len(olderAtReflowCols.Rows) != 1 || rowText(olderAtReflowCols.Rows[0]) != "one" {
		t.Fatalf("unexpected older window at reflow cols %#v", olderAtReflowCols)
	}
	_, err = client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:          "term-1",
		Cols:                10,
		Limit:               1,
		CursorValid:         true,
		BeforeLineID:        latest.CursorLineID + 99,
		BeforeRowInLine:     latest.CursorRow,
		Token:               latest.Token,
		Generation:          latest.Generation,
		BoundaryFirstLineID: latest.FirstLineID,
		BoundaryLastLineID:  latest.LastLineID,
	})
	if err == nil || !strings.Contains(err.Error(), ErrStaleHistoryWindow.Error()) {
		t.Fatalf("expected cursor stale history window error, got %v", err)
	}
	_, err = client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:          "term-1",
		Cols:                10,
		Limit:               1,
		CursorValid:         true,
		BeforeLineID:        latest.CursorLineID,
		BeforeRowInLine:     latest.CursorRow,
		Token:               latest.Token,
		Generation:          latest.Generation,
		BoundaryFirstLineID: latest.FirstLineID + 99,
		BoundaryLastLineID:  latest.LastLineID,
	})
	if err == nil || !strings.Contains(err.Error(), ErrStaleHistoryWindow.Error()) {
		t.Fatalf("expected first-boundary stale history window error, got %v", err)
	}
}

func TestProtocolServiceOlderAcceptsBoundaryFromMultiRowLatestSnapshot(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 3}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour\nfive"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       10,
		Limit:      2,
	})
	if err != nil {
		t.Fatalf("latest history.window: %v", err)
	}
	if len(latest.Rows) != 2 || rowText(latest.Rows[0]) != "four" || rowText(latest.Rows[1]) != "five" || !latest.CursorValid {
		t.Fatalf("expected two-row frozen latest snapshot, got %#v", latest)
	}

	older, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:          "term-1",
		Cols:                10,
		Limit:               1,
		CursorValid:         latest.CursorValid,
		BeforeLineID:        latest.CursorLineID,
		BeforeRowInLine:     latest.CursorRow,
		Token:               latest.Token,
		Generation:          latest.Generation,
		BoundaryFirstLineID: latest.FirstLineID,
		BoundaryLastLineID:  latest.LastLineID,
	})
	if err != nil {
		t.Fatalf("older history.window from multi-row latest boundary: %v", err)
	}
	if older.Op != protocol.HistoryWindowPrepend || len(older.Rows) != 1 || rowText(older.Rows[0]) != "one" {
		t.Fatalf("unexpected older window from multi-row latest boundary %#v", older)
	}
}

func TestProtocolServiceOlderAcceptsExpandedBoundaryAfterPrepend(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 1}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour\nfive"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       10,
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("latest history.window: %v", err)
	}
	if len(latest.Rows) != 1 || rowText(latest.Rows[0]) != "five" || !latest.CursorValid {
		t.Fatalf("expected one-row frozen latest snapshot, got %#v", latest)
	}
	firstOlder, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:          "term-1",
		Cols:                10,
		Limit:               1,
		CursorValid:         latest.CursorValid,
		BeforeLineID:        latest.CursorLineID,
		BeforeRowInLine:     latest.CursorRow,
		Token:               latest.Token,
		Generation:          latest.Generation,
		BoundaryFirstLineID: latest.FirstLineID,
		BoundaryLastLineID:  latest.LastLineID,
	})
	if err != nil {
		t.Fatalf("first older history.window: %v", err)
	}
	if firstOlder.Op != protocol.HistoryWindowPrepend || len(firstOlder.Rows) != 1 || rowText(firstOlder.Rows[0]) != "three" || !firstOlder.CursorValid {
		t.Fatalf("expected first prepend page over frozen snapshot, got %#v", firstOlder)
	}

	secondOlder, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:          "term-1",
		Cols:                10,
		Limit:               1,
		CursorValid:         firstOlder.CursorValid,
		BeforeLineID:        firstOlder.CursorLineID,
		BeforeRowInLine:     firstOlder.CursorRow,
		Token:               firstOlder.Token,
		Generation:          firstOlder.Generation,
		// TUI prepend 后会把 first boundary 替换成 older response 的 first，
		// 但继续保留原先 latest 的 tail boundary；这里按真实 merge 后的
		// frozen store 边界发起下一次 older。
		BoundaryFirstLineID: firstOlder.FirstLineID,
		BoundaryLastLineID:  latest.LastLineID,
	})
	if err != nil {
		t.Fatalf("second older history.window from expanded boundary: %v", err)
	}
	if secondOlder.Op != protocol.HistoryWindowPrepend || len(secondOlder.Rows) != 1 || rowText(secondOlder.Rows[0]) != "two" {
		t.Fatalf("unexpected second older window from expanded boundary %#v", secondOlder)
	}
}

func TestProtocolServiceFrozenSnapshotIgnoresLaterCarriageReturnMutation(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 2}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour\nfive"); err != nil {
		t.Fatalf("ingest initial output: %v", err)
	}

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       10,
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("latest frozen snapshot: %v", err)
	}
	if rowText(latest.Rows[0]) != "five" || !latest.CursorValid {
		t.Fatalf("expected frozen snapshot cursor over mutable tail, got %#v", latest)
	}

	if err := server.IngestOutput(context.Background(), "term-1", "\rTH"); err != nil {
		t.Fatalf("ingest later CR mutation: %v", err)
	}

	current, err := server.LatestWindow("term-1", 10, 10)
	if err != nil {
		t.Fatalf("latest after CR mutation: %v", err)
	}
	if len(current.Rows) == 0 || current.Rows[len(current.Rows)-1].Text != "THve" {
		t.Fatalf("live history tail should reflect post-snapshot CR mutation, got %#v", current)
	}

	older, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:      "term-1",
		Cols:            10,
		Limit:           1,
		CursorValid:     latest.CursorValid,
		BeforeLineID:    latest.CursorLineID,
		BeforeRowInLine: latest.CursorRow,
		Token:           latest.Token,
		Generation:      latest.Generation,
	})
	if err != nil {
		t.Fatalf("older from frozen snapshot after CR mutation: %v", err)
	}
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "two" || !older.HasMore || !older.CursorValid {
		t.Fatalf("older page should still come from frozen snapshot, got %#v", older)
	}
	oldest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:      "term-1",
		Cols:            10,
		Limit:           1,
		CursorValid:     older.CursorValid,
		BeforeLineID:    older.CursorLineID,
		BeforeRowInLine: older.CursorRow,
		Token:           older.Token,
		Generation:      older.Generation,
	})
	if err != nil {
		t.Fatalf("oldest page from frozen snapshot after CR mutation: %v", err)
	}
	if len(oldest.Rows) != 1 || rowText(oldest.Rows[0]) != "one" {
		t.Fatalf("oldest page should still come from frozen snapshot, got %#v", oldest)
	}

	reloaded, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       10,
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("latest after CR mutation via new snapshot: %v", err)
	}
	if len(reloaded.Rows) != 1 || rowText(reloaded.Rows[0]) != "THve" {
		t.Fatalf("new snapshot should see mutated live tail, got %#v", reloaded)
	}
}

func TestProtocolServiceKeepsOlderWorkingForPreviousFrozenTokenAfterNewLatest(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 2}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour"); err != nil {
		t.Fatalf("ingest initial output: %v", err)
	}

	first, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       10,
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("first latest frozen snapshot: %v", err)
	}
	if first.Token == "" || !first.CursorValid || rowText(first.Rows[0]) != "four" {
		t.Fatalf("expected first frozen snapshot over mutable tail, got %#v", first)
	}

	if err := server.IngestOutput(context.Background(), "term-1", "\rfo"); err != nil {
		t.Fatalf("ingest later live mutation: %v", err)
	}

	second, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       10,
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("second latest frozen snapshot: %v", err)
	}
	if second.Token == "" || second.Token == first.Token || rowText(second.Rows[0]) != "four" {
		t.Fatalf("expected second latest to pin a newer token and payload, got first=%#v second=%#v", first, second)
	}

	older, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:      "term-1",
		Cols:            10,
		Limit:           1,
		CursorValid:     first.CursorValid,
		BeforeLineID:    first.CursorLineID,
		BeforeRowInLine: first.CursorRow,
		Token:           first.Token,
		Generation:      first.Generation,
	})
	if err != nil {
		t.Fatalf("older from previous frozen token after new latest: %v", err)
	}
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "one" {
		t.Fatalf("older page should still come from previous frozen token, got %#v", older)
	}
}

func TestProtocolServiceKeepsOlderWorkingAcrossTerminalsWithSameFrozenGeneration(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	for _, terminalID := range []string{"term-a", "term-b"} {
		if _, err := client.Create(context.Background(), protocol.CreateParams{ID: terminalID, Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 2}}); err != nil {
			t.Fatalf("create %s: %v", terminalID, err)
		}
		if err := server.IngestOutput(context.Background(), terminalID, "one\ntwo\nthree\nfour"); err != nil {
			t.Fatalf("ingest initial output for %s: %v", terminalID, err)
		}
	}

	first, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-a",
		Cols:       10,
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("first terminal latest frozen snapshot: %v", err)
	}
	second, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-b",
		Cols:       10,
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("second terminal latest frozen snapshot: %v", err)
	}
	if first.Generation != second.Generation {
		t.Fatalf("expected same frozen generation for identical terminal history, got first=%d second=%d", first.Generation, second.Generation)
	}
	if first.Token == "" || second.Token == "" || first.Token == second.Token {
		t.Fatalf("frozen tokens must stay unique across terminals even when generation matches, got first=%q second=%q", first.Token, second.Token)
	}

	older, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:      "term-a",
		Cols:            10,
		Limit:           1,
		CursorValid:     first.CursorValid,
		BeforeLineID:    first.CursorLineID,
		BeforeRowInLine: first.CursorRow,
		Token:           first.Token,
		Generation:      first.Generation,
	})
	if err != nil {
		t.Fatalf("older from first terminal after second terminal latest: %v", err)
	}
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "one" {
		t.Fatalf("older page should still come from first terminal frozen token, got %#v", older)
	}
}

func TestProtocolServiceFrozenSnapshotIgnoresLaterEraseInLineMutation(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 2}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour"); err != nil {
		t.Fatalf("ingest initial output: %v", err)
	}

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       10,
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("latest frozen snapshot: %v", err)
	}
	if rowText(latest.Rows[0]) != "four" || !latest.CursorValid {
		t.Fatalf("expected frozen snapshot cursor over mutable tail, got %#v", latest)
	}

	if err := server.IngestOutput(context.Background(), "term-1", "\rfo\x1b[K"); err != nil {
		t.Fatalf("ingest later EL mutation: %v", err)
	}

	current, err := server.LatestWindow("term-1", 10, 10)
	if err != nil {
		t.Fatalf("latest after EL mutation: %v", err)
	}
	if len(current.Rows) == 0 || strings.TrimRight(current.Rows[len(current.Rows)-1].Text, " ") != "fo" {
		t.Fatalf("live history tail should reflect post-snapshot EL mutation, got %#v", current)
	}

	older, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:      "term-1",
		Cols:            10,
		Limit:           1,
		CursorValid:     latest.CursorValid,
		BeforeLineID:    latest.CursorLineID,
		BeforeRowInLine: latest.CursorRow,
		Token:           latest.Token,
		Generation:      latest.Generation,
	})
	if err != nil {
		t.Fatalf("older from frozen snapshot after EL mutation: %v", err)
	}
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "one" {
		t.Fatalf("older page should still come from frozen snapshot, got %#v", older)
	}
}

func TestProtocolServiceFrozenSnapshotIgnoresLaterEraseInLineModeOneMutation(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 2}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour"); err != nil {
		t.Fatalf("ingest initial output: %v", err)
	}

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       10,
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("latest frozen snapshot: %v", err)
	}
	if rowText(latest.Rows[0]) != "four" || !latest.CursorValid {
		t.Fatalf("expected frozen snapshot cursor over mutable tail, got %#v", latest)
	}

	if err := server.IngestOutput(context.Background(), "term-1", "\rfo\x1b[1K"); err != nil {
		t.Fatalf("ingest later EL 1 mutation: %v", err)
	}

	current, err := server.LatestWindow("term-1", 10, 10)
	if err != nil {
		t.Fatalf("latest after EL 1 mutation: %v", err)
	}
	if len(current.Rows) == 0 || current.Rows[len(current.Rows)-1].Text != "   r" {
		t.Fatalf("live history tail should reflect post-snapshot EL 1 mutation, got %#v", current)
	}

	older, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:      "term-1",
		Cols:            10,
		Limit:           1,
		CursorValid:     latest.CursorValid,
		BeforeLineID:    latest.CursorLineID,
		BeforeRowInLine: latest.CursorRow,
		Token:           latest.Token,
		Generation:      latest.Generation,
	})
	if err != nil {
		t.Fatalf("older from frozen snapshot after EL 1 mutation: %v", err)
	}
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "one" {
		t.Fatalf("older page should still come from frozen snapshot after EL 1 mutation, got %#v", older)
	}
}

func TestProtocolServiceFrozenSnapshotIgnoresLaterEraseInLineModeTwoMutation(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 2}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour"); err != nil {
		t.Fatalf("ingest initial output: %v", err)
	}

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       10,
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("latest frozen snapshot: %v", err)
	}
	if rowText(latest.Rows[0]) != "four" || !latest.CursorValid {
		t.Fatalf("expected frozen snapshot cursor over mutable tail, got %#v", latest)
	}

	if err := server.IngestOutput(context.Background(), "term-1", "\rfo\x1b[2K"); err != nil {
		t.Fatalf("ingest later EL 2 mutation: %v", err)
	}

	current, err := server.LatestWindow("term-1", 10, 10)
	if err != nil {
		t.Fatalf("latest after EL 2 mutation: %v", err)
	}
	if len(current.Rows) == 0 || current.Rows[len(current.Rows)-1].Text != "    " {
		t.Fatalf("live history tail should reflect post-snapshot EL 2 mutation, got %#v", current)
	}

	older, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:      "term-1",
		Cols:            10,
		Limit:           1,
		CursorValid:     latest.CursorValid,
		BeforeLineID:    latest.CursorLineID,
		BeforeRowInLine: latest.CursorRow,
		Token:           latest.Token,
		Generation:      latest.Generation,
	})
	if err != nil {
		t.Fatalf("older from frozen snapshot after EL 2 mutation: %v", err)
	}
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "one" {
		t.Fatalf("older page should still come from frozen snapshot after EL 2 mutation, got %#v", older)
	}
}

func TestProtocolServiceFrozenSnapshotIgnoresLaterClearScrollback(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 2}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour"); err != nil {
		t.Fatalf("ingest initial output: %v", err)
	}

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       10,
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("latest frozen snapshot: %v", err)
	}
	if rowText(latest.Rows[0]) != "four" || !latest.CursorValid {
		t.Fatalf("expected frozen snapshot cursor over mutable tail, got %#v", latest)
	}

	if err := server.IngestOutput(context.Background(), "term-1", "\x1b[3J"); err != nil {
		t.Fatalf("ingest clear scrollback: %v", err)
	}

	older, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:      "term-1",
		Cols:            10,
		Limit:           1,
		CursorValid:     latest.CursorValid,
		BeforeLineID:    latest.CursorLineID,
		BeforeRowInLine: latest.CursorRow,
		Token:           latest.Token,
		Generation:      latest.Generation,
	})
	if err != nil {
		t.Fatalf("older from frozen snapshot after clear scrollback: %v", err)
	}
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "one" {
		t.Fatalf("older page should still come from frozen snapshot after clear scrollback, got %#v", older)
	}

	reloaded, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       10,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("latest after clear scrollback via new snapshot: %v", err)
	}
	if len(reloaded.Rows) != 3 || rowText(reloaded.Rows[0]) != "two" || rowText(reloaded.Rows[1]) != "three" || rowText(reloaded.Rows[2]) != "four" || reloaded.LogicalTotal != 0 {
		t.Fatalf("new snapshot should see empty committed history after clear scrollback, got %#v", reloaded)
	}
}

func TestProtocolServiceHistoryWindowIgnoresAltScreenOutput(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 20, Rows: 2}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\n\x1b[?1049halt-tail\n\x1b[?1049lafter"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       20,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("latest history.window after alt-screen: %v", err)
	}
	if len(latest.Rows) != 2 || rowText(latest.Rows[0]) != "one" || rowText(latest.Rows[1]) != "after" || latest.LogicalTotal != 0 {
		t.Fatalf("alt-screen output must stay out of primary history window, got %#v", latest)
	}
}

func TestProtocolServiceFrozenSnapshotSurvivesRestartWhileNewLatestResetsHistory(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 2}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour"); err != nil {
		t.Fatalf("ingest initial output: %v", err)
	}

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       10,
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("latest frozen snapshot: %v", err)
	}
	if rowText(latest.Rows[0]) != "four" || !latest.CursorValid {
		t.Fatalf("expected frozen snapshot cursor over mutable tail, got %#v", latest)
	}

	if err := client.Restart(context.Background(), "term-1"); err != nil {
		t.Fatalf("restart: %v", err)
	}

	older, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:      "term-1",
		Cols:            10,
		Limit:           1,
		CursorValid:     latest.CursorValid,
		BeforeLineID:    latest.CursorLineID,
		BeforeRowInLine: latest.CursorRow,
		Token:           latest.Token,
		Generation:      latest.Generation,
	})
	if err != nil {
		t.Fatalf("older from frozen snapshot after restart: %v", err)
	}
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "one" {
		t.Fatalf("older page should still come from pre-restart frozen snapshot, got %#v", older)
	}

	reloaded, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       10,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("latest after restart via new snapshot: %v", err)
	}
	if reloaded.Token == latest.Token {
		t.Fatalf("restart should pin a new frozen token for subsequent latest, got old=%q new=%q", latest.Token, reloaded.Token)
	}
	if len(reloaded.Rows) != 0 || reloaded.LogicalTotal != 0 || reloaded.CursorValid || reloaded.HasMore {
		t.Fatalf("new latest after restart should see reset empty history, got %#v", reloaded)
	}
}

func TestProtocolServiceFrozenSnapshotIgnoresLaterProcessExitForceCommit(t *testing.T) {
	factory := newRecordingProcessFactory()
	server, client, closeClient := newProtocolClientWithProcessFactory(t, factory)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 20, Rows: 1}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	process := serverProcess(t, server, "term-1")
	process.emitOutput("one\ntwo\nopen-tail")

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       20,
		Limit:      2,
	})
	if err != nil {
		t.Fatalf("latest frozen snapshot before exit: %v", err)
	}
	if len(latest.Rows) != 2 || rowText(latest.Rows[0]) != "two" || rowText(latest.Rows[1]) != "open-tail" || !latest.CursorValid {
		t.Fatalf("expected frozen snapshot over mutable tail before exit, got %#v", latest)
	}

	process.exit(7)

	older, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:      "term-1",
		Cols:            20,
		Limit:           1,
		CursorValid:     latest.CursorValid,
		BeforeLineID:    latest.CursorLineID,
		BeforeRowInLine: latest.CursorRow,
		Token:           latest.Token,
		Generation:      latest.Generation,
	})
	if err != nil {
		t.Fatalf("older from frozen snapshot after exit force commit: %v", err)
	}
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "one" {
		t.Fatalf("older page should still come from pre-exit frozen snapshot, got %#v", older)
	}

	reloaded, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       20,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("latest after exit via new snapshot: %v", err)
	}
	if len(reloaded.Rows) != 3 || rowText(reloaded.Rows[0]) != "one" || rowText(reloaded.Rows[1]) != "two" || rowText(reloaded.Rows[2]) != "open-tail" || reloaded.LogicalTotal != 3 {
		t.Fatalf("new latest after exit should see force-committed primary history, got %#v", reloaded)
	}
	if len(reloaded.RowOwnership) != 3 || reloaded.RowOwnership[0] != protocol.RowOwnershipPersisted || reloaded.RowOwnership[1] != protocol.RowOwnershipPersisted || reloaded.RowOwnership[2] != protocol.RowOwnershipPersisted {
		t.Fatalf("force-committed exit tail should now be persisted in new snapshot, got %#v", reloaded.RowOwnership)
	}
}

func TestProtocolServiceFrozenSnapshotIgnoresLaterEraseDisplayFromCursorMutation(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 2}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour"); err != nil {
		t.Fatalf("ingest initial output: %v", err)
	}

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       10,
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("latest frozen snapshot: %v", err)
	}
	if rowText(latest.Rows[0]) != "four" || !latest.CursorValid {
		t.Fatalf("expected frozen snapshot cursor over mutable tail, got %#v", latest)
	}

	if err := server.IngestOutput(context.Background(), "term-1", "\rfo\x1b[0J"); err != nil {
		t.Fatalf("ingest later ED 0 mutation: %v", err)
	}

	current, err := server.LatestWindow("term-1", 10, 10)
	if err != nil {
		t.Fatalf("latest after ED 0 mutation: %v", err)
	}
	if len(current.Rows) == 0 || strings.TrimRight(current.Rows[len(current.Rows)-1].Text, " ") != "fo" {
		t.Fatalf("live history tail should reflect post-snapshot ED 0 mutation, got %#v", current)
	}

	older, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:      "term-1",
		Cols:            10,
		Limit:           1,
		CursorValid:     latest.CursorValid,
		BeforeLineID:    latest.CursorLineID,
		BeforeRowInLine: latest.CursorRow,
		Token:           latest.Token,
		Generation:      latest.Generation,
	})
	if err != nil {
		t.Fatalf("older from frozen snapshot after ED 0 mutation: %v", err)
	}
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "one" {
		t.Fatalf("older page should still come from frozen snapshot after ED 0 mutation, got %#v", older)
	}
}

func TestProtocolServiceFrozenSnapshotIgnoresLaterEraseDisplayToCursorMutation(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 2}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour"); err != nil {
		t.Fatalf("ingest initial output: %v", err)
	}

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       10,
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("latest frozen snapshot: %v", err)
	}
	if rowText(latest.Rows[0]) != "four" || !latest.CursorValid {
		t.Fatalf("expected frozen snapshot cursor over mutable tail, got %#v", latest)
	}

	if err := server.IngestOutput(context.Background(), "term-1", "\rfo\x1b[1J"); err != nil {
		t.Fatalf("ingest later ED 1 mutation: %v", err)
	}

	current, err := server.LatestWindow("term-1", 10, 10)
	if err != nil {
		t.Fatalf("latest after ED 1 mutation: %v", err)
	}
	if len(current.Rows) == 0 || current.Rows[len(current.Rows)-1].Text != "   r" {
		t.Fatalf("live history tail should reflect post-snapshot ED 1 mutation, got %#v", current)
	}

	older, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:      "term-1",
		Cols:            10,
		Limit:           1,
		CursorValid:     latest.CursorValid,
		BeforeLineID:    latest.CursorLineID,
		BeforeRowInLine: latest.CursorRow,
		Token:           latest.Token,
		Generation:      latest.Generation,
	})
	if err != nil {
		t.Fatalf("older from frozen snapshot after ED 1 mutation: %v", err)
	}
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "one" {
		t.Fatalf("older page should still come from frozen snapshot after ED 1 mutation, got %#v", older)
	}
}

func TestProtocolHistoryWindowPreservesStyledCells(t *testing.T) {
	track := history.NewHistoryTrack()
	if err := track.Apply(history.HistoryEvent{Kind: history.EventWritePrimaryCells, Cells: []history.Cell{
		{Text: "ERR", Width: 3, Style: history.CellStyle{FG: "ansi:1", Bold: true}},
		{Text: " ", Width: 1},
		{Text: "好", Width: 2, Style: history.CellStyle{FG: "#ffcc00", Underline: true}, LinkURL: "file://build.log", LinkParams: "line=7"},
	}}); err != nil {
		t.Fatalf("write styled cells: %v", err)
	}
	if err := track.Apply(history.HistoryEvent{Kind: history.EventForceCommitFrontier}); err != nil {
		t.Fatalf("commit styled cells: %v", err)
	}
	window, err := track.LatestWindow(history.HistoryWindowRequest{Cols: 10, Rows: 4})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}

	out := protocolHistoryWindowFromCore("term-1", Size{Cols: 10, Rows: 4}, window)
	cells := out.Rows[0].DecodeCells()
	if got := rowText(out.Rows[0]); got != "ERR 好" {
		t.Fatalf("plain text should be decoded from compact cells, got %q row=%#v", got, out.Rows[0])
	}
	if len(cells) != 3 {
		t.Fatalf("expected compact row to decode styled cells, got %#v", cells)
	}
	if cells[0].Content != "ERR" || cells[0].Width != 3 || cells[0].Style.FG != "ansi:1" || !cells[0].Style.Bold {
		t.Fatalf("lost first styled cell through protocol conversion %#v", cells[0])
	}
	if cells[2].Content != "好" || cells[2].Width != 2 || cells[2].Style.FG != "#ffcc00" || !cells[2].Style.Underline || cells[2].LinkURL == "" || cells[2].LinkParams == "" {
		t.Fatalf("lost wide linked cell through protocol conversion %#v", cells[2])
	}
	if out.Rows[0].Text != "" {
		t.Fatalf("styled history rows should not be downgraded to Text-only CompactRow, got %#v", out.Rows[0])
	}
}

func TestProtocolHistoryWindowPreservesTrailingBlankCells(t *testing.T) {
	track := history.NewHistoryTrack()
	if err := track.Apply(history.HistoryEvent{Kind: history.EventWritePrimaryCells, Cells: []history.Cell{
		{Text: "cmd", Width: 3},
		{Text: " ", Width: 1},
		{Text: " ", Width: 1},
	}}); err != nil {
		t.Fatalf("write cells: %v", err)
	}
	if err := track.Apply(history.HistoryEvent{Kind: history.EventForceCommitFrontier}); err != nil {
		t.Fatalf("commit cells: %v", err)
	}
	window, err := track.LatestWindow(history.HistoryWindowRequest{Cols: 10, Rows: 4})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}
	if window.Rows[0].Text != "cmd  " {
		t.Fatalf("core visual row should keep trailing blanks, got %q", window.Rows[0].Text)
	}

	out := protocolHistoryWindowFromCore("term-1", Size{Cols: 10, Rows: 4}, window)
	if got := rowText(out.Rows[0]); got != "cmd  " {
		t.Fatalf("protocol history row should preserve trailing blanks, got %q row=%#v", got, out.Rows[0])
	}
	cells := out.Rows[0].DecodeCells()
	if len(cells) != 3 || cells[1].Content != " " || cells[2].Content != " " {
		t.Fatalf("expected trailing blank cells to survive protocol conversion, got %#v", cells)
	}
}

func TestProtocolServiceHistoryWindowPreservesIngestedANSIStyles(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 20, Rows: 4}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	output := "\x1b[1;31mERR\x1b[0m \x1b[4;38;2;255;204;0m好\x1b[0m "
	output += "\x1b]8;id=termx;https://example.test\aLINK\x1b]8;;\a\n"
	if err := server.IngestOutput(context.Background(), "term-1", output); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       20,
		Limit:      4,
	})
	if err != nil {
		t.Fatalf("history.window: %v", err)
	}
	if len(latest.Rows) != 1 || rowText(latest.Rows[0]) != "ERR 好 LINK" {
		t.Fatalf("unexpected latest rows %#v", latest.Rows)
	}
	cells := latest.Rows[0].DecodeCells()
	if len(cells) != 5 {
		t.Fatalf("expected compact row cells from ingested ANSI output, got %#v", cells)
	}
	if cells[0].Content != "ERR" || cells[0].Width != 3 || cells[0].Style.FG != "ansi:1" || !cells[0].Style.Bold {
		t.Fatalf("expected red bold ERR cell through protocol path, got %#v", cells[0])
	}
	if cells[2].Content != "好" || cells[2].Width != 2 || cells[2].Style.FG != "#ffcc00" || !cells[2].Style.Underline {
		t.Fatalf("expected truecolor underlined wide cell through protocol path, got %#v", cells[2])
	}
	if cells[4].Content != "LINK" || cells[4].LinkURL != "https://example.test" || cells[4].LinkParams != "id=termx" {
		t.Fatalf("expected OSC 8 link metadata through protocol path, got %#v", cells[4])
	}
	if latest.Rows[0].Text != "" {
		t.Fatalf("styled ingest must not downgrade protocol row to plain Text, got %#v", latest.Rows[0])
	}
}

func TestProtocolServiceHistoryWindowPreservesProcessOutputANSIStyles(t *testing.T) {
	factory := newRecordingProcessFactory()
	server, client, closeClient := newProtocolClientWithProcessFactory(t, factory)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 20, Rows: 4}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	process := serverProcess(t, server, "term-1")
	process.emitOutput("\x1b[")
	process.emitOutput("1;31mERR\x1b[0m ")
	process.emitOutput("\x1b]8;id=termx;https://example.test\aLINK\x1b]8;;\a\n")

	latest := waitForProtocolHistoryRow(t, client, "term-1", "ERR LINK")
	cells := latest.Rows[0].DecodeCells()
	if len(cells) != 3 {
		t.Fatalf("expected styled cells from process output reader, got %#v", cells)
	}
	if cells[0].Content != "ERR" || cells[0].Style.FG != "ansi:1" || !cells[0].Style.Bold {
		t.Fatalf("expected process output SGR through protocol path, got %#v", cells[0])
	}
	if cells[2].Content != "LINK" || cells[2].LinkURL != "https://example.test" || cells[2].LinkParams != "id=termx" {
		t.Fatalf("expected process output OSC 8 link through protocol path, got %#v", cells[2])
	}
}

func TestProtocolServiceAttachRoutesInputResizeAndEvents(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 3}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	events, err := client.Events(context.Background(), protocol.EventsParams{
		TerminalID: "term-1",
		Types:      []protocol.EventType{protocol.EventTerminalResized},
	})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	attach, err := client.AttachWithOptions(context.Background(), protocol.AttachParams{
		TerminalID:   "term-1",
		Mode:         "collaborator",
		ResizePolicy: protocol.ResizePolicyOwner,
		SurfaceID:    "surface-1",
		ViewID:       "view-1",
	})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if attach.Channel == 0 || attach.ResizeControl == nil || !attach.ResizeControl.CanResize {
		t.Fatalf("unexpected attach result %#v", attach)
	}

	if err := client.Input(context.Background(), attach.Channel, []byte("echo hi\n")); err != nil {
		t.Fatalf("input: %v", err)
	}
	if err := client.Resize(context.Background(), attach.Channel, 20, 5); err != nil {
		t.Fatalf("resize frame: %v", err)
	}
	if _, err := client.EnsureResize(context.Background(), protocol.EnsureResizeParams{
		TerminalID: "missing",
		Channel:    attach.Channel,
		Cols:       30,
		Rows:       10,
	}); err == nil {
		t.Fatal("expected ensure_resize to reject mismatched terminal/channel")
	}
	process := waitForProcessTraffic(t, server, "term-1", 1, 1)
	inputs, resizes, _, _ := process.snapshot()
	if string(inputs[0]) != "echo hi\n" {
		t.Fatalf("input did not reach process %#v", inputs)
	}
	if resizes[0] != (Size{Cols: 20, Rows: 5}) {
		t.Fatalf("resize did not reach process %#v", resizes)
	}

	evt := requireProtocolEvent(t, events)
	if evt.Type != protocol.EventTerminalResized || evt.Resized == nil || evt.Resized.NewSize != (protocol.Size{Cols: 20, Rows: 5}) {
		t.Fatalf("unexpected resize event %#v", evt)
	}
}

func TestProtocolServiceMultipleAttachmentsResizeOwnership(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 3}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	owner, err := client.AttachWithOptions(context.Background(), protocol.AttachParams{TerminalID: "term-1", ResizePolicy: protocol.ResizePolicyOwner, SurfaceID: "surface-owner", ViewID: "view-owner"})
	if err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	follower, err := client.AttachWithOptions(context.Background(), protocol.AttachParams{TerminalID: "term-1", ResizePolicy: protocol.ResizePolicyFollower, SurfaceID: "surface-follower", ViewID: "view-follower"})
	if err != nil {
		t.Fatalf("attach follower: %v", err)
	}
	if owner.Channel == follower.Channel || owner.ResizeControl == nil || follower.ResizeControl == nil {
		t.Fatalf("expected distinct attachment controls owner=%#v follower=%#v", owner, follower)
	}
	if !owner.ResizeControl.CanResize || owner.ResizeControl.Reason != protocol.ResizeControlReasonOwner {
		t.Fatalf("owner should be resize owner, got %#v", owner.ResizeControl)
	}
	if follower.ResizeControl.CanResize || follower.ResizeControl.Reason != protocol.ResizeControlReasonFollower || follower.ResizeControl.OwnerViewID != "view-owner" {
		t.Fatalf("follower should not own resize, got %#v", follower.ResizeControl)
	}

	result, err := client.EnsureResize(context.Background(), protocol.EnsureResizeParams{TerminalID: "term-1", Channel: follower.Channel, Cols: 40, Rows: 10, ResizePolicy: protocol.ResizePolicyFollower, SurfaceID: "surface-follower", ViewID: "view-follower"})
	if err != nil {
		t.Fatalf("follower ensure_resize: %v", err)
	}
	if result.Resized || result.ResizeControl == nil || result.ResizeControl.CanResize || result.ResizeControl.Reason != protocol.ResizeControlReasonFollower {
		t.Fatalf("follower ensure_resize should not resize, got %#v", result)
	}
	if _, err := client.EnsureResize(context.Background(), protocol.EnsureResizeParams{TerminalID: "term-1", Channel: owner.Channel, Cols: 30, Rows: 8, ResizePolicy: protocol.ResizePolicyOwner, SurfaceID: "surface-owner", ViewID: "view-owner"}); err != nil {
		t.Fatalf("owner ensure_resize: %v", err)
	}
	process := waitForProcessTraffic(t, server, "term-1", 0, 1)
	_, resizes, _, _ := process.snapshot()
	if len(resizes) != 1 || resizes[0] != (Size{Cols: 30, Rows: 8}) {
		t.Fatalf("expected only owner resize to reach process, got %#v", resizes)
	}
	var info protocol.TerminalInfo
	if err := client.Call(context.Background(), "get", protocol.GetParams{TerminalID: "term-1"}, &info); err != nil {
		t.Fatalf("get: %v", err)
	}
	if info.ResizeOwnership == nil || info.ResizeOwnership.OwnerViewID != "view-owner" || info.ResizeOwnerAttachmentCount != 2 {
		t.Fatalf("expected ownership summary in terminal info, got %#v", info)
	}
}

func TestProtocolServiceTerminalSizeLockRequiresManualUnlockBeforeResize(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 3}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	owner, err := client.AttachWithOptions(context.Background(), protocol.AttachParams{TerminalID: "term-1", ResizePolicy: protocol.ResizePolicyOwner, SurfaceID: "surface-owner", ViewID: "view-owner"})
	if err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	follower, err := client.AttachWithOptions(context.Background(), protocol.AttachParams{TerminalID: "term-1", ResizePolicy: protocol.ResizePolicyFollower, SurfaceID: "surface-follower", ViewID: "view-follower"})
	if err != nil {
		t.Fatalf("attach follower: %v", err)
	}

	locked, err := client.LockResize(context.Background(), protocol.ResizeControlParams{TerminalID: "term-1", Channel: owner.Channel, ResizePolicy: protocol.ResizePolicyOwner, SurfaceID: "surface-owner", ViewID: "view-owner"})
	if err != nil {
		t.Fatalf("lock resize: %v", err)
	}
	if locked.ResizeControl == nil || !locked.ResizeControl.SizeLocked || locked.ResizeControl.CanResize || locked.ResizeControl.Reason != protocol.ResizeControlReasonSizeLocked {
		t.Fatalf("expected locked owner control, got %#v", locked)
	}
	followerUnlock, err := client.UnlockResize(context.Background(), protocol.ResizeControlParams{TerminalID: "term-1", Channel: follower.Channel, ResizePolicy: protocol.ResizePolicyFollower, SurfaceID: "surface-follower", ViewID: "view-follower"})
	if err != nil {
		t.Fatalf("follower unlock: %v", err)
	}
	if followerUnlock.ResizeControl == nil || !followerUnlock.ResizeControl.SizeLocked {
		t.Fatalf("follower must not unlock terminal size, got %#v", followerUnlock)
	}

	blocked, err := client.EnsureResize(context.Background(), protocol.EnsureResizeParams{TerminalID: "term-1", Channel: owner.Channel, Cols: 50, Rows: 12, ResizePolicy: protocol.ResizePolicyOwner, SurfaceID: "surface-owner", ViewID: "view-owner"})
	if err != nil {
		t.Fatalf("locked ensure resize: %v", err)
	}
	if blocked.Resized || blocked.ResizeControl == nil || blocked.ResizeControl.Reason != protocol.ResizeControlReasonSizeLocked || blocked.Size != (protocol.Size{Cols: 10, Rows: 3}) {
		t.Fatalf("locked ensure_resize should not change PTY size, got %#v", blocked)
	}
	process := waitForProcessTraffic(t, server, "term-1", 0, 0)
	_, resizes, _, _ := process.snapshot()
	if len(resizes) != 0 {
		t.Fatalf("locked resize reached process %#v", resizes)
	}

	unlocked, err := client.UnlockResize(context.Background(), protocol.ResizeControlParams{TerminalID: "term-1", Channel: owner.Channel, ResizePolicy: protocol.ResizePolicyOwner, SurfaceID: "surface-owner", ViewID: "view-owner"})
	if err != nil {
		t.Fatalf("unlock resize: %v", err)
	}
	if unlocked.ResizeControl == nil || unlocked.ResizeControl.SizeLocked || !unlocked.ResizeControl.CanResize {
		t.Fatalf("expected unlocked owner control, got %#v", unlocked)
	}
	resized, err := client.EnsureResize(context.Background(), protocol.EnsureResizeParams{TerminalID: "term-1", Channel: owner.Channel, Cols: 50, Rows: 12, ResizePolicy: protocol.ResizePolicyOwner, SurfaceID: "surface-owner", ViewID: "view-owner"})
	if err != nil {
		t.Fatalf("unlocked ensure resize: %v", err)
	}
	if !resized.Resized || resized.Size != (protocol.Size{Cols: 50, Rows: 12}) {
		t.Fatalf("manual resize after unlock should resize, got %#v", resized)
	}
}

func TestProtocolServiceDetachByChannelKeepsSiblingAttachment(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 3}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	first, err := client.AttachWithOptions(context.Background(), protocol.AttachParams{TerminalID: "term-1", ResizePolicy: protocol.ResizePolicyOwner, SurfaceID: "surface-1", ViewID: "view-1"})
	if err != nil {
		t.Fatalf("attach first: %v", err)
	}
	second, err := client.AttachWithOptions(context.Background(), protocol.AttachParams{TerminalID: "term-1", ResizePolicy: protocol.ResizePolicyFollower, SurfaceID: "surface-2", ViewID: "view-2"})
	if err != nil {
		t.Fatalf("attach second: %v", err)
	}
	if err := client.Call(context.Background(), "detach", protocol.DetachParams{TerminalID: "term-1", Channel: first.Channel}, nil); err != nil {
		t.Fatalf("detach first: %v", err)
	}
	if err := client.Input(context.Background(), first.Channel, []byte("old\n")); err != nil {
		t.Fatalf("detached first input frame send: %v", err)
	}
	if err := client.Input(context.Background(), second.Channel, []byte("new\n")); err != nil {
		t.Fatalf("second input: %v", err)
	}
	process := waitForProcessTraffic(t, server, "term-1", 1, 0)
	inputs, _, _, _ := process.snapshot()
	if len(inputs) != 1 || string(inputs[0]) != "new\n" {
		t.Fatalf("expected only sibling attachment input, got %#v", inputs)
	}
}

func TestProtocolServiceStorageMethodsAndEvents(t *testing.T) {
	_, client, closeClient := newProtocolClient(t)
	defer closeClient()

	events, err := client.Events(context.Background(), protocol.EventsParams{
		Types:            []protocol.EventType{protocol.EventStorageChanged},
		StorageAppID:     "termx-tui-v3",
		StorageScope:     protocol.StorageScopePublic,
		StorageOwnerID:   "workspace-main",
		StorageKeyPrefix: "workbench/",
	})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	entry, err := client.StoragePut(context.Background(), protocol.StoragePutParams{
		AppID:   "termx-tui-v3",
		Scope:   protocol.StorageScopePublic,
		OwnerID: "workspace-main",
		Key:     "workbench/root",
		Value:   []byte("v1"),
	})
	if err != nil {
		t.Fatalf("storage put: %v", err)
	}
	if entry.Version != 1 || string(entry.Value) != "v1" {
		t.Fatalf("unexpected put entry %#v", entry)
	}
	event := requireProtocolEvent(t, events)
	if event.Type != protocol.EventStorageChanged || event.Storage == nil || event.Storage.Key != "workbench/root" || event.Storage.Version != 1 || event.Storage.Op != "put" {
		t.Fatalf("unexpected storage event %#v", event)
	}
	got, err := client.StorageGet(context.Background(), protocol.StorageGetParams{
		AppID:   "termx-tui-v3",
		Scope:   protocol.StorageScopePublic,
		OwnerID: "workspace-main",
		Key:     "workbench/root",
	})
	if err != nil {
		t.Fatalf("storage get: %v", err)
	}
	if got.Version != 1 || string(got.Value) != "v1" {
		t.Fatalf("unexpected get entry %#v", got)
	}
	if _, err := client.StoragePut(context.Background(), protocol.StoragePutParams{
		AppID:           "termx-tui-v3",
		Scope:           protocol.StorageScopePublic,
		OwnerID:         "workspace-main",
		Key:             "workbench/root",
		Value:           []byte("stale"),
		CheckVersion:    true,
		ExpectedVersion: 99,
	}); err == nil || !strings.Contains(err.Error(), ErrStorageVersionConflict.Error()) {
		t.Fatalf("expected storage conflict, got %v", err)
	}
	list, err := client.StorageList(context.Background(), protocol.StorageListParams{
		AppID:   "termx-tui-v3",
		Scope:   protocol.StorageScopePublic,
		OwnerID: "workspace-main",
		Prefix:  "workbench/",
	})
	if err != nil {
		t.Fatalf("storage list: %v", err)
	}
	if len(list.Entries) != 1 || list.Entries[0].Key != "workbench/root" {
		t.Fatalf("unexpected list %#v", list)
	}
	deleted, err := client.StorageDelete(context.Background(), protocol.StorageDeleteParams{
		AppID:           "termx-tui-v3",
		Scope:           protocol.StorageScopePublic,
		OwnerID:         "workspace-main",
		Key:             "workbench/root",
		CheckVersion:    true,
		ExpectedVersion: 1,
	})
	if err != nil {
		t.Fatalf("storage delete: %v", err)
	}
	if !deleted.Deleted || deleted.Version != 2 {
		t.Fatalf("unexpected delete result %#v", deleted)
	}
}

func TestProtocolServiceWorkbenchMethodsAndEvents(t *testing.T) {
	_, client, closeClient := newProtocolClient(t)
	defer closeClient()

	events, err := client.Events(context.Background(), protocol.EventsParams{
		Types:       []protocol.EventType{protocol.EventWorkbenchChanged},
		WorkbenchID: "workspace-main",
	})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	snapshot, err := client.WorkbenchGet(context.Background(), protocol.WorkbenchGetParams{})
	if err != nil {
		t.Fatalf("workbench get: %v", err)
	}
	if snapshot.Version != 1 || snapshot.ActiveWorkspaceID != "workspace-main" {
		t.Fatalf("unexpected initial snapshot %#v", snapshot)
	}
	result, err := client.WorkbenchApply(context.Background(), protocol.WorkbenchMutateParams{
		Action:          protocol.WorkbenchMutationPaneSplit,
		WorkspaceID:     "workspace-main",
		TabID:           "tab-main",
		PaneID:          "pane-main",
		TargetID:        "pane-two",
		Name:            "logs",
		SplitDirection:  protocol.WorkbenchSplitHorizontal,
		CheckVersion:    true,
		ExpectedVersion: snapshot.Version,
	})
	if err != nil {
		t.Fatalf("workbench apply: %v", err)
	}
	if result.Snapshot.Version != 2 || result.ResourceID != "pane-two" {
		t.Fatalf("unexpected apply result %#v", result)
	}
	event := requireProtocolEvent(t, events)
	if event.Type != protocol.EventWorkbenchChanged || event.Workbench == nil || event.Workbench.ResourceID != "pane-two" || event.Workbench.Version != 2 {
		t.Fatalf("unexpected workbench event %#v", event)
	}
	if _, err := client.WorkbenchApply(context.Background(), protocol.WorkbenchMutateParams{
		Action:          protocol.WorkbenchMutationPaneRename,
		WorkspaceID:     "workspace-main",
		TabID:           "tab-main",
		PaneID:          "pane-two",
		Name:            "stale",
		CheckVersion:    true,
		ExpectedVersion: 1,
	}); err == nil || !strings.Contains(err.Error(), ErrWorkbenchVersionConflict.Error()) {
		t.Fatalf("expected workbench conflict, got %v", err)
	}
}

func TestProtocolServiceSnapshotReturnsLiveSurfaceRows(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 12, Rows: 4}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "alpha\n\x1b[31mERR\x1b[0m ok\r\x1b[32mOK\x1b[0m"); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	snapshot, err := client.Snapshot(context.Background(), "term-1", 0, 2)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.TerminalID != "term-1" || snapshot.Size != (protocol.Size{Cols: 12, Rows: 4}) {
		t.Fatalf("unexpected snapshot metadata %#v", snapshot)
	}
	if len(snapshot.Screen.Cells) != 4 {
		t.Fatalf("expected size-bound live screen rows, got %#v", snapshot.Screen.Cells)
	}
	got := []string{cellsText(snapshot.Screen.Cells[0]), cellsText(snapshot.Screen.Cells[1])}
	if got[0] != "alpha       " || !strings.HasPrefix(got[1], "OK   ERR ok ") || len(snapshot.Scrollback) != 0 {
		t.Fatalf("snapshot must expose live screen cell matrix without scrollback truth, got rows=%#v scrollback=%#v", got, snapshot.Scrollback)
	}
	if snapshot.Screen.Cells[1][0].Style.FG != "ansi:2" {
		t.Fatalf("snapshot must preserve live cell style, got %#v", snapshot.Screen.Cells[1][0])
	}
	if !snapshot.Cursor.Visible || snapshot.Cursor.Row != 1 || snapshot.Cursor.Col == 0 {
		t.Fatalf("snapshot must preserve live cursor, got %#v", snapshot.Cursor)
	}
}

func newProtocolClient(t *testing.T) (*Server, *protocol.Client, func()) {
	return newProtocolClientWithProcessFactory(t, newRecordingProcessFactory())
}

func newProtocolClientWithProcessFactory(t *testing.T, factory ProcessFactory) (*Server, *protocol.Client, func()) {
	t.Helper()
	clientTransport, serverTransport := memory.NewPair()
	server := NewServer(WithProcessFactory(factory))
	errCh := make(chan error, 1)
	go func() {
		errCh <- newProtocolSession(server, serverTransport).run(context.Background())
	}()
	client := protocol.NewClient(clientTransport)
	if err := client.Hello(context.Background(), protocol.Hello{Version: wire.Version, Client: "test"}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	closeClient := func() {
		_ = client.Close()
		select {
		case err := <-errCh:
			if err != nil && !strings.Contains(err.Error(), "EOF") {
				t.Fatalf("server session returned error: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("server session did not stop")
		}
	}
	return server, client, closeClient
}

func waitForProtocolHistoryRow(t *testing.T, client *protocol.Client, terminalID string, want string) *protocol.HistoryWindow {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		window, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
			TerminalID: terminalID,
			Cols:       20,
			Limit:      4,
		})
		if err == nil {
			for _, row := range window.Rows {
				if rowText(row) == want {
					return window
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	window, _ := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: terminalID,
		Cols:       20,
		Limit:      4,
	})
	t.Fatalf("timed out waiting for protocol history row %q, got %#v", want, window)
	return nil
}

func serverProcess(t *testing.T, server *Server, terminalID string) *recordingProcess {
	t.Helper()
	terminal, err := server.Terminal(terminalID)
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	process, ok := terminal.process.(*recordingProcess)
	if !ok {
		t.Fatalf("expected recording process, got %T", terminal.process)
	}
	return process
}

func waitForProcessTraffic(t *testing.T, server *Server, terminalID string, inputs int, resizes int) *recordingProcess {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		process := serverProcess(t, server, terminalID)
		processInputs, processResizes, _, _ := process.snapshot()
		if len(processInputs) >= inputs && len(processResizes) >= resizes {
			return process
		}
		time.Sleep(time.Millisecond)
	}
	process := serverProcess(t, server, terminalID)
	processInputs, processResizes, _, _ := process.snapshot()
	t.Fatalf("timed out waiting for process traffic inputs=%d/%d resizes=%d/%d", len(processInputs), inputs, len(processResizes), resizes)
	return process
}

func requireProtocolEvent(t *testing.T, events <-chan protocol.Event) protocol.Event {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("protocol events channel closed")
		}
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for protocol event")
	}
	return protocol.Event{}
}

func rowText(row protocol.CompactRow) string {
	var builder strings.Builder
	for _, cell := range row.DecodeCells() {
		builder.WriteString(cell.Content)
	}
	return builder.String()
}

func cellsText(row []protocol.Cell) string {
	var builder strings.Builder
	for _, cell := range row {
		builder.WriteString(cell.Content)
	}
	return builder.String()
}
