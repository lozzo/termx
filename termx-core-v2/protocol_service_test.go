package termxcorev2

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
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

func TestProtocolServiceExitMetadataRoundTrips(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()
	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Name:    "job",
		Command: []string{"bash", "-lc", "make test"},
		Size:    protocol.Size{Cols: 12, Rows: 4},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	events, err := client.Events(context.Background(), protocol.EventsParams{
		TerminalID: "term-1",
		Types:      []protocol.EventType{protocol.EventTerminalStateChanged},
	})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	serverProcess(t, server, "term-1").exit(23)
	event := requireProtocolEvent(t, events)
	if event.StateChanged == nil || event.StateChanged.ExitCode == nil || *event.StateChanged.ExitCode != 23 || event.StateChanged.ExitedAt.IsZero() {
		t.Fatalf("expected exited event metadata, got %#v", event)
	}
	var info protocol.TerminalInfo
	if err := client.Call(context.Background(), "get", protocol.GetParams{TerminalID: "term-1"}, &info); err != nil {
		t.Fatalf("get: %v", err)
	}
	if info.State != string(TerminalStateExited) || info.ExitCode == nil || *info.ExitCode != 23 || !info.ExitedAt.Equal(event.StateChanged.ExitedAt) {
		t.Fatalf("expected get to carry exit metadata, got %#v event=%#v", info, event)
	}
	if got := strings.Join(info.Command, " "); !strings.Contains(got, "make test") {
		t.Fatalf("expected command metadata, got %#v", info.Command)
	}
	list, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Terminals) != 1 || !list.Terminals[0].ExitedAt.Equal(info.ExitedAt) {
		t.Fatalf("expected list to carry exited_at, got %#v want=%s", list, info.ExitedAt)
	}
}

func TestProtocolServiceRestartPublishesRunningLifecycleEvent(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()
	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Name:    "job",
		Command: []string{"shell"},
		Size:    protocol.Size{Cols: 12, Rows: 4},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	events, err := client.Events(context.Background(), protocol.EventsParams{
		TerminalID: "term-1",
		Types:      []protocol.EventType{protocol.EventTerminalStateChanged},
	})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	serverProcess(t, server, "term-1").exit(23)
	exited := requireProtocolEvent(t, events)
	if exited.StateChanged == nil || exited.StateChanged.NewState != string(TerminalStateExited) {
		t.Fatalf("expected exited lifecycle before restart, got %#v", exited)
	}
	if err := client.Restart(context.Background(), "term-1"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	event := requireProtocolEvent(t, events)
	if event.StateChanged == nil || event.StateChanged.NewState != string(TerminalStateRunning) {
		t.Fatalf("restart should publish authoritative running lifecycle, got %#v", event)
	}
	if event.StateChanged.ExitCode != nil || !event.StateChanged.ExitedAt.IsZero() {
		t.Fatalf("running lifecycle should clear exit metadata, got %#v", event.StateChanged)
	}
}

func TestProtocolServiceRestartedProcessSurvivesClientSessionClose(t *testing.T) {
	factory := newSessionBoundRecordingProcessFactory()
	server, client, closeClient := newProtocolClientWithProcessFactory(t, factory)
	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Name:    "job",
		Command: []string{"shell"},
		Size:    protocol.Size{Cols: 12, Rows: 4},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := client.Restart(context.Background(), "term-1"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	restarted := factory.process("term-1")
	if restarted == nil {
		t.Fatal("expected restarted process")
	}

	closeClient()

	select {
	case exit, ok := <-restarted.Wait():
		if ok {
			t.Fatalf("restarted process must not be tied to closed protocol session, exit=%#v", exit)
		}
	case <-time.After(100 * time.Millisecond):
	}
	info, err := server.GetTerminal("term-1")
	if err != nil {
		t.Fatalf("get terminal after client close: %v", err)
	}
	if info.State != TerminalStateRunning {
		t.Fatalf("closing client session must not mark restarted terminal exited, got %#v", info)
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
	if latest.Op != protocol.HistoryWindowReplace || latest.Size.Cols != 10 || latest.LogicalTotal != 2 || len(latest.Rows) != 2 {
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
		t.Fatalf("latest tail should still expose older rows from the same frozen snapshot, got %#v", latest)
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
	if older.Op != protocol.HistoryWindowPrepend || older.Token != latest.Token || len(older.Rows) != 1 || rowText(older.Rows[0]) != "three" {
		t.Fatalf("unexpected older window %#v", older)
	}
	if len(older.RowOwnership) != 1 || older.RowOwnership[0] != protocol.RowOwnershipLiveTailLive {
		t.Fatalf("older frozen live-tail row should stay live-tail-owned, got %#v", older.RowOwnership)
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
	if olderAtReflowCols.Op != protocol.HistoryWindowPrepend || len(olderAtReflowCols.Rows) != 1 || rowText(olderAtReflowCols.Rows[0]) != "three" {
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

func TestProtocolServiceHistoryCopyUsesFrozenLogicalRange(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 8, Rows: 3}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "alpha  \nbeta\n好wide\ngamma"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       8,
		Limit:      2,
	})
	if err != nil {
		t.Fatalf("latest history.window: %v", err)
	}
	if latest.Token == "" || latest.Generation == 0 || len(latest.RowLineIDs) == 0 {
		t.Fatalf("expected frozen latest metadata, got %#v", latest)
	}
	older, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:          "term-1",
		Cols:                8,
		Limit:               2,
		Token:               latest.Token,
		Generation:          latest.Generation,
		CursorValid:         latest.CursorValid,
		BeforeLineID:        latest.CursorLineID,
		BeforeRowInLine:     latest.CursorRow,
		BoundaryFirstLineID: latest.FirstLineID,
		BoundaryLastLineID:  latest.LastLineID,
	})
	if err != nil {
		t.Fatalf("older history.window: %v", err)
	}
	if len(older.RowLineIDs) < 2 {
		t.Fatalf("expected older rows, got %#v", older)
	}

	text, err := client.HistoryCopy(context.Background(), protocol.HistoryWindowParams{
		TerminalID:          "term-1",
		Cols:                8,
		Token:               latest.Token,
		Generation:          latest.Generation,
		BoundaryFirstLineID: older.RowLineIDs[0],
		BoundaryLastLineID:  latest.LastLineID,
		RangeValid:          true,
		RangeStartLineID:    older.RowLineIDs[0],
		RangeStartCol:       2,
		RangeEndLineID:      latest.RowLineIDs[len(latest.RowLineIDs)-1],
		RangeEndCol:         3,
	})
	if err != nil {
		t.Fatalf("history.copy: %v", err)
	}
	if text != "pha  \nbeta\n好wide\ngam" {
		t.Fatalf("unexpected copied text %q", text)
	}
}

func TestProtocolServiceFrozenSnapshotSurvivesClearScrollbackForOldObserver(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 2}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour"); err != nil {
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
	if rowText(latest.Rows[0]) != "four" || !latest.CursorValid {
		t.Fatalf("expected latest tail with older cursor, got %#v", latest)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "\x1b[3J"); err != nil {
		t.Fatalf("clear scrollback: %v", err)
	}

	older, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:          "term-1",
		Cols:                10,
		Limit:               1,
		Token:               latest.Token,
		Generation:          latest.Generation,
		CursorValid:         latest.CursorValid,
		BeforeLineID:        latest.CursorLineID,
		BeforeRowInLine:     latest.CursorRow,
		BoundaryFirstLineID: latest.FirstLineID,
		BoundaryLastLineID:  latest.LastLineID,
	})
	if err != nil {
		t.Fatalf("older after clear scrollback: %v", err)
	}
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "three" {
		t.Fatalf("old observer should still page deleted committed history, got %#v", older)
	}

	reloaded, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       10,
		Limit:      4,
	})
	if err != nil {
		t.Fatalf("latest after clear scrollback: %v", err)
	}
	if reloaded.LogicalTotal != 0 {
		t.Fatalf("new observer must not count cleared committed history, got %#v", reloaded)
	}
	for _, row := range reloaded.Rows {
		if got := rowText(row); got == "one" || got == "two" {
			t.Fatalf("new latest should not expose cleared committed row %q in %#v", got, reloaded.Rows)
		}
	}
}

func TestProtocolServiceSessionCloseReleasesFrozenObserver(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 2}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree"); err != nil {
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
	if latest.Token == "" {
		t.Fatalf("expected frozen token, got %#v", latest)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "\x1b[3J"); err != nil {
		t.Fatalf("clear scrollback: %v", err)
	}
	terminal, err := server.Terminal("term-1")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	if got := terminal.RetainedHistoryLineCount(); got == 0 {
		t.Fatal("expected clear to retain deleted payload for active frozen observer")
	}

	closeClient()
	if got := terminal.RetainedHistoryLineCount(); got != 0 {
		t.Fatalf("session close should release frozen observer and cleanup payloads, got %d", got)
	}
}

func TestProtocolServiceHistoryReleaseDropsFrozenObserver(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 2}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree"); err != nil {
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
	if err := server.IngestOutput(context.Background(), "term-1", "\x1b[3J"); err != nil {
		t.Fatalf("clear scrollback: %v", err)
	}
	terminal, err := server.Terminal("term-1")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	if got := terminal.RetainedHistoryLineCount(); got == 0 {
		t.Fatal("expected clear to retain deleted payload for active frozen observer")
	}
	if err := client.ReleaseHistory(context.Background(), protocol.HistoryWindowParams{TerminalID: "term-1", Token: latest.Token}); err != nil {
		t.Fatalf("history release: %v", err)
	}
	if got := terminal.RetainedHistoryLineCount(); got != 0 {
		t.Fatalf("history release should cleanup retained payloads, got %d", got)
	}
	_, err = client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:      "term-1",
		Cols:            10,
		Limit:           1,
		Token:           latest.Token,
		Generation:      latest.Generation,
		CursorValid:     latest.CursorValid,
		BeforeLineID:    latest.CursorLineID,
		BeforeRowInLine: latest.CursorRow,
	})
	if err == nil || !strings.Contains(err.Error(), ErrStaleHistoryWindow.Error()) {
		t.Fatalf("released token should be stale, got %v", err)
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
	if older.Op != protocol.HistoryWindowPrepend || len(older.Rows) != 1 || rowText(older.Rows[0]) != "three" {
		t.Fatalf("unexpected older window from multi-row latest boundary %#v", older)
	}
}

func TestProtocolServiceHistoryWindowTokenWithoutCursorReturnsFrozenOldestPage(t *testing.T) {
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
	if len(latest.Rows) != 2 || rowText(latest.Rows[0]) != "four" || rowText(latest.Rows[1]) != "five" || latest.Token == "" {
		t.Fatalf("expected frozen latest tail, got %#v", latest)
	}

	oldest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:          "term-1",
		Cols:                10,
		Limit:               2,
		Token:               latest.Token,
		Generation:          latest.Generation,
		BoundaryFirstLineID: latest.FirstLineID,
		BoundaryLastLineID:  latest.LastLineID,
	})
	if err != nil {
		t.Fatalf("oldest history.window from frozen token: %v", err)
	}
	if oldest.Op != protocol.HistoryWindowReplace || oldest.Token != latest.Token || oldest.CursorValid || !oldest.HasMore {
		t.Fatalf("oldest jump should replace current loaded page without an older cursor, got %#v", oldest)
	}
	if len(oldest.Rows) != 2 || rowText(oldest.Rows[0]) != "one" || rowText(oldest.Rows[1]) != "two" {
		t.Fatalf("unexpected oldest rows %#v", oldest.Rows)
	}

	_, err = client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:          "term-1",
		Cols:                10,
		Limit:               2,
		Token:               latest.Token,
		Generation:          latest.Generation,
		BoundaryFirstLineID: latest.FirstLineID + 99,
		BoundaryLastLineID:  latest.LastLineID,
	})
	if err == nil || !strings.Contains(err.Error(), ErrStaleHistoryWindow.Error()) {
		t.Fatalf("expected stale boundary error for frozen oldest request, got %v", err)
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
	if firstOlder.Op != protocol.HistoryWindowPrepend || len(firstOlder.Rows) != 1 || rowText(firstOlder.Rows[0]) != "four" || !firstOlder.CursorValid {
		t.Fatalf("expected first prepend page over frozen snapshot, got %#v", firstOlder)
	}

	secondOlder, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:      "term-1",
		Cols:            10,
		Limit:           1,
		CursorValid:     firstOlder.CursorValid,
		BeforeLineID:    firstOlder.CursorLineID,
		BeforeRowInLine: firstOlder.CursorRow,
		Token:           firstOlder.Token,
		Generation:      firstOlder.Generation,
		// TUI prepend 后会把 first boundary 替换成 older response 的 first，
		// 但继续保留原先 latest 的 tail boundary；这里按真实 merge 后的
		// frozen store 边界发起下一次 older。
		BoundaryFirstLineID: firstOlder.FirstLineID,
		BoundaryLastLineID:  latest.LastLineID,
	})
	if err != nil {
		t.Fatalf("second older history.window from expanded boundary: %v", err)
	}
	if secondOlder.Op != protocol.HistoryWindowPrepend || len(secondOlder.Rows) != 1 || rowText(secondOlder.Rows[0]) != "three" {
		t.Fatalf("unexpected second older window from expanded boundary %#v", secondOlder)
	}
}

func TestFrozenSnapshotOlderWindowUsesLargeSnapshotCursor(t *testing.T) {
	snapshot := frozenSnapshotForBenchmark(100000)
	latest := frozenSnapshotLatestWindow(snapshot, 80, 24)
	if !latest.Cursor.Valid {
		t.Fatalf("expected latest cursor from large snapshot, got %#v", latest)
	}

	older := frozenSnapshotOlderWindow(snapshot, 80, 24, latest.Cursor)
	if len(older.Rows) != 24 {
		t.Fatalf("expected one older page, got %d rows", len(older.Rows))
	}
	if older.FirstLineID == 0 || older.LastLineID == 0 || !older.Cursor.Valid {
		t.Fatalf("older window should keep logical boundary and cursor, got %#v", older)
	}
	if got := older.Rows[len(older.Rows)-1].LineID; got >= latest.FirstLineID {
		t.Fatalf("older page must be before latest boundary, got last=%d latestFirst=%d", got, latest.FirstLineID)
	}

	oldest := frozenSnapshotOldestWindow(snapshot, 80, 24)
	if len(oldest.Rows) != 24 || oldest.Op != history.HistoryWindowReplace || oldest.Cursor.Valid {
		t.Fatalf("expected one replace oldest page without cursor, got %#v", oldest)
	}
	if oldest.FirstLineID != 1 || oldest.Rows[0].LineID != 1 || !oldest.HasMore {
		t.Fatalf("oldest page should start at first logical line and report skipped newer rows, got %#v", oldest)
	}

	newer := frozenSnapshotNewerWindow(snapshot, 80, 24, history.HistoryCursor{Valid: true, BeforeLineID: older.LastLineID, BeforeRowInLine: older.Rows[len(older.Rows)-1].RowInLine})
	if newer.Op != history.HistoryWindowAppend || len(newer.Rows) != 24 {
		t.Fatalf("expected append newer page, got %#v", newer)
	}
	if newer.Rows[0].LineID <= older.LastLineID {
		t.Fatalf("newer page must move forward after local tail, olderLast=%d firstNewer=%d", older.LastLineID, newer.Rows[0].LineID)
	}
}

func TestProtocolServiceHistoryWindowNewerUsesFrozenSnapshot(t *testing.T) {
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
	older, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:          "term-1",
		Cols:                10,
		Limit:               2,
		CursorValid:         latest.CursorValid,
		BeforeLineID:        latest.CursorLineID,
		BeforeRowInLine:     latest.CursorRow,
		Token:               latest.Token,
		Generation:          latest.Generation,
		BoundaryFirstLineID: latest.FirstLineID,
		BoundaryLastLineID:  latest.LastLineID,
	})
	if err != nil {
		t.Fatalf("older history.window: %v", err)
	}
	if len(older.Rows) != 2 || rowText(older.Rows[len(older.Rows)-1]) != "four" {
		t.Fatalf("expected older page ending at four, got %#v", older)
	}
	newer, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:          "term-1",
		Cols:                10,
		Limit:               1,
		Mode:                "newer",
		Token:               latest.Token,
		Generation:          latest.Generation,
		AfterCursorValid:    true,
		AfterLineID:         older.RowLineIDs[len(older.RowLineIDs)-1],
		AfterRowInLine:      0,
		BoundaryFirstLineID: older.FirstLineID,
		BoundaryLastLineID:  latest.LastLineID,
	})
	if err != nil {
		t.Fatalf("newer history.window: %v", err)
	}
	if newer.Op != protocol.HistoryWindowAppend || len(newer.Rows) != 1 || rowText(newer.Rows[0]) != "five" {
		t.Fatalf("expected append newer page with five, got %#v", newer)
	}
	if newer.FirstLineID != older.FirstLineID || newer.LastLineID != latest.LastLineID {
		t.Fatalf("newer response must keep frozen merged boundary, got first=%d last=%d", newer.FirstLineID, newer.LastLineID)
	}
}

func TestProtocolServiceFrozenSnapshotContinuousOlderMovesBackward(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 120, Rows: 24}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	var output strings.Builder
	for line := 1; line <= 1000; line++ {
		fmt.Fprintf(&output, "%06d stress line\n", line)
	}
	if err := server.IngestOutput(context.Background(), "term-1", output.String()); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       120,
		Limit:      48,
	})
	if err != nil {
		t.Fatalf("latest history.window: %v", err)
	}
	if !latest.CursorValid || latest.FirstLineID == 0 {
		t.Fatalf("expected latest cursor over 1000-line output, got %#v", latest)
	}
	previousFirst := latest.FirstLineID
	cursorLine := latest.CursorLineID
	cursorRow := latest.CursorRow
	boundaryFirst := latest.FirstLineID
	boundaryLast := latest.LastLineID

	for page := 1; page <= 4; page++ {
		older, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
			TerminalID:          "term-1",
			Cols:                120,
			Limit:               48,
			CursorValid:         true,
			BeforeLineID:        cursorLine,
			BeforeRowInLine:     cursorRow,
			Token:               latest.Token,
			Generation:          latest.Generation,
			BoundaryFirstLineID: boundaryFirst,
			BoundaryLastLineID:  boundaryLast,
		})
		if err != nil {
			t.Fatalf("older page %d: %v", page, err)
		}
		if len(older.Rows) == 0 || older.FirstLineID == 0 {
			t.Fatalf("older page %d should return rows, got %#v", page, older)
		}
		if older.FirstLineID >= previousFirst {
			t.Fatalf("older page %d did not move backward: previous first=%d current first=%d window=%#v", page, previousFirst, older.FirstLineID, older)
		}
		previousFirst = older.FirstLineID
		cursorLine = older.CursorLineID
		cursorRow = older.CursorRow
		boundaryFirst = older.FirstLineID
		if older.LastLineID != boundaryLast {
			t.Fatalf("older page %d should keep latest tail boundary last=%d, got %d", page, boundaryLast, older.LastLineID)
		}
	}
}

func BenchmarkFrozenSnapshotOlderWindowLargeHistory(b *testing.B) {
	snapshot := frozenSnapshotForBenchmark(100000)
	latest := frozenSnapshotLatestWindow(snapshot, 80, 24)
	cursor := latest.Cursor
	if !cursor.Valid {
		b.Fatal("missing benchmark cursor")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		window := frozenSnapshotOlderWindow(snapshot, 80, 24, cursor)
		if len(window.Rows) != 24 {
			b.Fatalf("expected 24 rows, got %d", len(window.Rows))
		}
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
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "four" || !older.HasMore || !older.CursorValid {
		t.Fatalf("older page should still come from frozen snapshot, got %#v", older)
	}
	oldest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID:          "term-1",
		Cols:                10,
		Limit:               1,
		Token:               latest.Token,
		Generation:          latest.Generation,
		BoundaryFirstLineID: latest.FirstLineID,
		BoundaryLastLineID:  latest.LastLineID,
	})
	if err != nil {
		t.Fatalf("direct oldest page from frozen snapshot after CR mutation: %v", err)
	}
	if oldest.Op != protocol.HistoryWindowReplace || len(oldest.Rows) != 1 || rowText(oldest.Rows[0]) != "one" {
		t.Fatalf("direct oldest page should still come from frozen snapshot, got %#v", oldest)
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
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "three" {
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
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "three" {
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
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "three" {
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
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "three" {
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
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "three" {
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
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "three" {
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
	if len(reloaded.Rows) != 2 || rowText(reloaded.Rows[0]) != "three" || rowText(reloaded.Rows[1]) != "four" || reloaded.LogicalTotal != 0 {
		t.Fatalf("new snapshot should see empty committed history after clear scrollback, got %#v", reloaded)
	}
}

func TestProtocolServiceHistoryWindowAppendsAltScreenFinalFrameOnExit(t *testing.T) {
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
	if len(latest.Rows) != 3 || rowText(latest.Rows[0]) != "one" || rowText(latest.Rows[1]) != "alt-tail" || rowText(latest.Rows[2]) != "after" || latest.LogicalTotal != 2 {
		t.Fatalf("alt-screen final frame should enter history before following primary output, got %#v", latest)
	}
}

func TestProtocolServiceEnterAltScreenCommitsPrimaryPageFirst(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 20, Rows: 2}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour\x1b[?1049h\x1b[2Jhalt-tail\x1b[?1049lafter"); err != nil {
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
	if len(latest.Rows) != 6 || rowText(latest.Rows[0]) != "one" || rowText(latest.Rows[1]) != "two" || rowText(latest.Rows[2]) != "three" || rowText(latest.Rows[3]) != "four" || rowText(latest.Rows[4]) != "halt-tail" || rowText(latest.Rows[5]) != "after" {
		t.Fatalf("enter alt-screen should preserve primary tail, append alt final frame, then continue primary, got %#v", latest)
	}
	if latest.LogicalTotal != 5 {
		t.Fatalf("enter alt-screen should commit primary page and alt final frame, got total=%d", latest.LogicalTotal)
	}
}

func TestProtocolServiceHistoryWindowFlushesStressTailBeforeAltScreenFreeze(t *testing.T) {
	factory := newRecordingProcessFactory()
	server, client, closeClient := newProtocolClientWithProcessFactory(t, factory)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    protocol.Size{Cols: 120, Rows: 20},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	process := serverProcess(t, server, "term-1")
	var output strings.Builder
	for i := 1; i <= 100; i++ {
		output.WriteString(stressHistoryLine(i))
		output.WriteByte('\n')
	}
	output.WriteString("\x1b[?1049h\x1b[2JALT_SCREEN_MARK\x1b[?1049l")
	process.emitOutput(output.String())

	var latest *protocol.HistoryWindow
	assertEventually(t, time.Second, func() bool {
		var err error
		latest, err = client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
			TerminalID: "term-1",
			Cols:       120,
			Limit:      30,
		})
		return err == nil && latest != nil && protocolHistoryWindowContainsText(latest, "000100") && protocolHistoryWindowContainsText(latest, "ALT_SCREEN_MARK")
	}, "history.window should observe stress tail and alt-screen final frame after async output reaches history queue")
	if !protocolHistoryWindowContainsText(latest, "000100") {
		t.Fatalf("history.window latest must preserve primary stress tail before alt-screen, got %#v", latest.Rows)
	}
	if !protocolHistoryWindowContainsText(latest, "ALT_SCREEN_MARK") {
		t.Fatalf("alt-screen final frame should enter frozen primary history, got %#v", latest.Rows)
	}
}

func TestProtocolServiceHistoryWindowPreservesStressTailAcrossFullscreenHomeClear(t *testing.T) {
	factory := newRecordingProcessFactory()
	server, client, closeClient := newProtocolClientWithProcessFactory(t, factory)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    protocol.Size{Cols: 120, Rows: 42},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	process := serverProcess(t, server, "term-1")
	var output strings.Builder
	for i := 1; i <= 100; i++ {
		output.WriteString(stressHistoryLine(i))
		output.WriteByte('\n')
	}
	output.WriteString("\x1b[?25l\x1b[H\x1b[JCODEX_FULLSCREEN_MARK")
	process.emitOutput(output.String())

	var latest *protocol.HistoryWindow
	assertEventually(t, time.Second, func() bool {
		var err error
		latest, err = client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
			TerminalID: "term-1",
			Cols:       120,
			Limit:      120,
		})
		return err == nil && latest != nil && protocolHistoryWindowContainsText(latest, "CODEX_FULLSCREEN_MARK")
	}, "fullscreen output should reach history before assertion")
	for i := 59; i <= 100; i++ {
		marker := fmt.Sprintf("%06d", i)
		if !protocolHistoryWindowContainsText(latest, marker) {
			t.Fatalf("history.window latest must preserve primary screen line %s before fullscreen clear, got %#v", marker, latest.Rows)
		}
	}
	if latest.LogicalTotal < 100 {
		t.Fatalf("fullscreen clear should commit stress primary page, got total=%d window=%#v", latest.LogicalTotal, latest)
	}
	if !protocolHistoryWindowContainsText(latest, "CODEX_FULLSCREEN_MARK") {
		t.Fatalf("fullscreen payload after page-break should be visible as fresh tail, got %#v", latest.Rows)
	}
}

func TestProtocolServiceFrozenSnapshotSurvivesRestartWhileNewLatestPreservesHistory(t *testing.T) {
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
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "three" {
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
	if !protocolHistoryWindowContainsText(reloaded, "three") || !protocolHistoryWindowContainsText(reloaded, "four") {
		t.Fatalf("new latest after restart should preserve terminal history, got %#v", reloaded)
	}
	if reloaded.LogicalTotal == 0 || reloaded.Token == "" {
		t.Fatalf("new latest after restart should expose preserved history metadata, got %#v", reloaded)
	}
}

func TestProtocolServiceFrozenSnapshotIgnoresLaterResizeReprojection(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    protocol.Size{Cols: 10, Rows: 3},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	attach, err := client.Attach(context.Background(), "term-1", "")
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "one\ntwo\nthree\nfour\nfive"); err != nil {
		t.Fatalf("ingest initial output: %v", err)
	}

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       10,
		Limit:      2,
	})
	if err != nil {
		t.Fatalf("latest frozen snapshot before resize: %v", err)
	}
	if len(latest.Rows) != 2 || rowText(latest.Rows[0]) != "four" || rowText(latest.Rows[1]) != "five" || !latest.CursorValid {
		t.Fatalf("expected frozen latest over pre-resize live tail, got %#v", latest)
	}

	if err := client.Resize(context.Background(), attach.Channel, 10, 4); err != nil {
		t.Fatalf("resize terminal: %v", err)
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
		t.Fatalf("older from frozen snapshot after resize: %v", err)
	}
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "three" {
		t.Fatalf("older page should still come from pre-resize frozen snapshot, got %#v", older)
	}

	reloaded, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       10,
		Limit:      4,
	})
	if err != nil {
		t.Fatalf("latest after resize via new snapshot: %v", err)
	}
	if reloaded.Token == latest.Token {
		t.Fatalf("resize should pin a new frozen token for subsequent latest, got old=%q new=%q", latest.Token, reloaded.Token)
	}
	if len(reloaded.Rows) != 4 || rowText(reloaded.Rows[0]) != "two" || rowText(reloaded.Rows[1]) != "three" || rowText(reloaded.Rows[2]) != "four" || rowText(reloaded.Rows[3]) != "five" {
		t.Fatalf("new latest after resize should see resized frontier projection, got %#v", reloaded)
	}
	if len(reloaded.RowOwnership) != 4 || reloaded.RowOwnership[0] != protocol.RowOwnershipLiveTailLive || reloaded.RowOwnership[1] != protocol.RowOwnershipLiveTailLive || reloaded.RowOwnership[2] != protocol.RowOwnershipLiveTailLive || reloaded.RowOwnership[3] != protocol.RowOwnershipLiveTailLive {
		t.Fatalf("grow resize should expose a larger live tail in the new snapshot, got %#v", reloaded.RowOwnership)
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
	// 先等输出真正进入 authoritative history，再冻结 snapshot；否则这里会和
	// ingest goroutine 竞争，测到空窗口而不是想验证的 pre-exit frozen tail。
	waitForProtocolHistoryRow(t, client, "term-1", "open-tail")

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
	waitForTerminalState(t, server, "term-1", TerminalStateExited)

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
	reloadedText := historyWindowRowsText(reloaded.Rows)
	if !strings.Contains(reloadedText, "one") ||
		!strings.Contains(reloadedText, "two") ||
		!strings.Contains(reloadedText, "open-tail") ||
		!strings.Contains(reloadedText, "terminal exited:") ||
		!strings.Contains(reloadedText, "m-1 code:7 exited") ||
		!strings.Contains(reloadedText, "command: shell") ||
		reloaded.LogicalTotal != 6 {
		t.Fatalf("new latest after exit should see force-committed primary history, got %#v", reloaded)
	}
	if len(reloaded.RowOwnership) != len(reloaded.Rows) {
		t.Fatalf("expected ownership for every reloaded row, got rows=%d ownership=%#v", len(reloaded.Rows), reloaded.RowOwnership)
	}
	for index, ownership := range reloaded.RowOwnership {
		if ownership != protocol.RowOwnershipPersisted {
			t.Fatalf("force-committed exit tail should now be persisted in new snapshot row=%d ownership=%#v", index, reloaded.RowOwnership)
		}
	}
	if len(older.Rows) == 1 && strings.Contains(historyWindowRowsText(older.Rows), "terminal exited") {
		t.Fatalf("older page should still come from pre-exit frozen snapshot and exclude exit marker, got %#v", older)
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
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "three" {
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
	if len(older.Rows) != 1 || rowText(older.Rows[0]) != "three" {
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

func TestProtocolHistoryWindowPreservesStyledTrailingBlankCells(t *testing.T) {
	track := history.NewHistoryTrack()
	style := history.CellStyle{BG: "idx:24"}
	if err := track.Apply(history.HistoryEvent{Kind: history.EventWritePrimaryCells, Cells: []history.Cell{
		{Text: "B", Width: 1, Style: style},
		{Text: "G", Width: 1, Style: style},
		{Text: " ", Width: 1, Style: style},
		{Text: " ", Width: 1, Style: style},
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

	out := protocolHistoryWindowFromCore("term-1", Size{Cols: 10, Rows: 4}, window)
	cells := out.Rows[0].DecodeCells()
	if got := rowText(out.Rows[0]); got != "BG  " || len(cells) != 4 {
		t.Fatalf("protocol history row should preserve styled trailing blanks, got text=%q cells=%#v", got, cells)
	}
	for i, cell := range cells {
		if cell.Width != 1 || cell.Style.BG != "idx:24" {
			t.Fatalf("styled trailing blank cell %d should survive protocol conversion, got %#v", i, cell)
		}
	}
}

func TestProtocolHistoryWindowPreservesTailFillWithoutBlankCells(t *testing.T) {
	track := history.NewHistoryTrack()
	style := history.CellStyle{BG: "idx:24"}
	if err := track.Apply(history.HistoryEvent{Kind: history.EventWritePrimaryCells, Cells: []history.Cell{
		{Text: "abcdefghij", Width: 10, Style: style},
	}}); err != nil {
		t.Fatalf("write cells: %v", err)
	}
	if err := track.Apply(history.HistoryEvent{Kind: history.EventSetActiveLineTailFill, Style: style}); err != nil {
		t.Fatalf("tail fill: %v", err)
	}
	if err := track.Apply(history.HistoryEvent{Kind: history.EventForceCommitFrontier}); err != nil {
		t.Fatalf("commit cells: %v", err)
	}
	window, err := track.LatestWindow(history.HistoryWindowRequest{Cols: 8, Rows: 4})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}

	out := protocolHistoryWindowFromCore("term-1", Size{Cols: 8, Rows: 4}, window)
	if got := rowText(out.Rows[1]); got != "ij" {
		t.Fatalf("tail fill must not become protocol row text, got %q row=%#v", got, out.Rows[1])
	}
	if out.Rows[0].TailFill != nil {
		t.Fatalf("tail fill should only attach to final visual row, got %#v", out.Rows[0])
	}
	if out.Rows[1].TailFill == nil || out.Rows[1].TailFill.BG != "idx:24" {
		t.Fatalf("expected protocol tail fill metadata, got %#v", out.Rows[1])
	}
}

func TestProtocolHistoryWindowPreservesStyledPaddedCellFootprintAcrossWrap(t *testing.T) {
	track := history.NewHistoryTrack()
	style := history.CellStyle{BG: "idx:24"}
	if err := track.Apply(history.HistoryEvent{Kind: history.EventWritePrimaryCells, Cells: []history.Cell{
		{Text: "BG", Width: 6, Style: style},
	}}); err != nil {
		t.Fatalf("write cells: %v", err)
	}
	if err := track.Apply(history.HistoryEvent{Kind: history.EventForceCommitFrontier}); err != nil {
		t.Fatalf("commit cells: %v", err)
	}
	window, err := track.LatestWindow(history.HistoryWindowRequest{Cols: 4, Rows: 4})
	if err != nil {
		t.Fatalf("latest window: %v", err)
	}

	out := protocolHistoryWindowFromCore("term-1", Size{Cols: 4, Rows: 4}, window)
	if len(out.Rows) != 2 {
		t.Fatalf("expected padded cell to wrap into two protocol rows, got %#v", out.Rows)
	}
	if got := rowText(out.Rows[0]); got != "BG  " {
		t.Fatalf("first protocol row should keep styled padding, got %q row=%#v", got, out.Rows[0])
	}
	if got := rowText(out.Rows[1]); got != "  " {
		t.Fatalf("second protocol row should keep styled padding, got %q row=%#v", got, out.Rows[1])
	}
	for rowIndex, row := range out.Rows {
		for cellIndex, cell := range row.DecodeCells() {
			if cell.Width != 1 || cell.Style.BG != "idx:24" {
				t.Fatalf("row=%d cell=%d should keep bg padded footprint, got %#v", rowIndex, cellIndex, cell)
			}
		}
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

func TestProtocolServiceHistoryWindowLatestFreezesCurrentHistoryWithoutWaitingForQueue(t *testing.T) {
	factory := newRecordingProcessFactory()
	server, client, closeClient := newProtocolClientWithProcessFactory(t, factory)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    protocol.Size{Cols: 40, Rows: 4},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "000001 committed\n"); err != nil {
		t.Fatalf("seed history: %v", err)
	}
	process := factory.process("term-1")
	enteredHistory := make(chan struct{})
	releaseHistory := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseHistory) }) }
	terminalHistoryPipelineBeforeIngestHook = func() {
		select {
		case <-enteredHistory:
		default:
			close(enteredHistory)
		}
		<-releaseHistory
	}
	defer func() {
		terminalHistoryPipelineBeforeIngestHook = nil
	}()
	defer release()

	process.emitOutput("100000 future\n")
	select {
	case <-enteredHistory:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for async history worker")
	}
	assertEventually(t, time.Second, func() bool {
		rows, err := server.LiveRows("term-1")
		return err == nil && strings.Contains(strings.Join(rows, "\n"), "100000 future")
	}, "live output should reach surface while history queue is blocked")

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       40,
		Limit:      4,
	})
	if err != nil {
		t.Fatalf("latest history.window: %v", err)
	}
	text := historyWindowRowsText(latest.Rows)
	if !strings.Contains(text, "000001 committed") {
		t.Fatalf("latest must include already recorded history, got %#v", latest.Rows)
	}
	if strings.Contains(text, "100000 future") {
		t.Fatalf("latest must not wait for queued future output, got %#v", latest.Rows)
	}
	release()
}

func TestProtocolServiceSnapshotDoesNotWaitForHistoryGenerationLock(t *testing.T) {
	factory := newRecordingProcessFactory()
	server, client, closeClient := newProtocolClientWithProcessFactory(t, factory)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    protocol.Size{Cols: 40, Rows: 4},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "000001 committed\n"); err != nil {
		t.Fatalf("seed history: %v", err)
	}
	boundary, err := client.Snapshot(context.Background(), "term-1", 0, 4)
	if err != nil {
		t.Fatalf("initial snapshot: %v", err)
	}
	if boundary.HistoryGeneration == 0 {
		t.Fatal("initial snapshot should expose completed history generation")
	}

	process := factory.process("term-1")
	enteredHistory := make(chan struct{})
	releaseHistory := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseHistory) }) }
	terminalHistoryPipelineBeforeIngestHook = func() {
		select {
		case <-enteredHistory:
		default:
			close(enteredHistory)
		}
		<-releaseHistory
	}
	defer func() {
		terminalHistoryPipelineBeforeIngestHook = nil
	}()
	defer release()

	process.emitOutput("100000 live-only-for-now\n")
	select {
	case <-enteredHistory:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for async history worker")
	}
	assertEventually(t, time.Second, func() bool {
		rows, err := server.LiveRows("term-1")
		return err == nil && strings.Contains(strings.Join(rows, "\n"), "100000 live-only-for-now")
	}, "live output should reach surface while history queue is blocked")

	snapshotCh := make(chan struct {
		snapshot *protocol.Snapshot
		err      error
	}, 1)
	go func() {
		snapshot, err := client.Snapshot(context.Background(), "term-1", 0, 4)
		snapshotCh <- struct {
			snapshot *protocol.Snapshot
			err      error
		}{snapshot: snapshot, err: err}
	}()
	select {
	case result := <-snapshotCh:
		if result.err != nil {
			t.Fatalf("snapshot should not wait for history lock: %v", result.err)
		}
		if result.snapshot.HistoryGeneration != boundary.HistoryGeneration {
			t.Fatalf("snapshot should report last completed generation while history is blocked, got %d want %d", result.snapshot.HistoryGeneration, boundary.HistoryGeneration)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("snapshot waited for history generation lock")
	}
	release()
}

func TestProtocolServiceHistoryWindowLatestHonorsGenerationBoundary(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    protocol.Size{Cols: 40, Rows: 4},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "000001 visible\n"); err != nil {
		t.Fatalf("ingest visible: %v", err)
	}
	snapshot, err := client.Snapshot(context.Background(), "term-1", 0, 4)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.HistoryGeneration == 0 {
		t.Fatal("snapshot must expose history generation boundary")
	}
	if err := server.IngestOutput(context.Background(), "term-1", "100000 future\n"); err != nil {
		t.Fatalf("ingest future: %v", err)
	}

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       40,
		Limit:      4,
		Generation: snapshot.HistoryGeneration,
	})
	if err != nil {
		t.Fatalf("latest history.window: %v", err)
	}
	text := historyWindowRowsText(latest.Rows)
	if !strings.Contains(text, "000001 visible") {
		t.Fatalf("latest should keep line visible at boundary, got %#v", latest.Rows)
	}
	if strings.Contains(text, "100000 future") {
		t.Fatalf("latest must not include line after boundary generation, got %#v", latest.Rows)
	}
}

func TestProtocolServiceHistoryWindowLatestDoesNotWaitForOwnBarrierOrBlockSameSessionInput(t *testing.T) {
	factory := newRecordingProcessFactory()
	_, client, closeClient := newProtocolClientWithProcessFactory(t, factory)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    protocol.Size{Cols: 40, Rows: 4},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	attachOne, err := client.AttachWithOptions(context.Background(), protocol.AttachParams{
		TerminalID: "term-1",
		SurfaceID:  "surface-1",
		ViewID:     "pane:one",
	})
	if err != nil {
		t.Fatalf("attach one: %v", err)
	}
	attachTwo, err := client.AttachWithOptions(context.Background(), protocol.AttachParams{
		TerminalID: "term-1",
		SurfaceID:  "surface-2",
		ViewID:     "pane:two",
	})
	if err != nil {
		t.Fatalf("attach two: %v", err)
	}

	process := factory.process("term-1")
	enteredHistory := make(chan struct{})
	releaseHistory := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseHistory) }) }
	terminalHistoryPipelineBeforeIngestHook = func() {
		select {
		case <-enteredHistory:
		default:
			close(enteredHistory)
		}
		<-releaseHistory
	}
	defer func() {
		terminalHistoryPipelineBeforeIngestHook = nil
	}()
	defer release()

	process.emitOutput("000001 first\n100000 final\n")
	select {
	case <-enteredHistory:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for async history worker")
	}

	latestCh := make(chan error, 1)
	go func() {
		_, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
			TerminalID: "term-1",
			Cols:       40,
			Limit:      4,
		})
		latestCh <- err
	}()
	inputCh := make(chan error, 1)
	go func() {
		inputCh <- client.InputWithOptions(context.Background(), protocol.InputParams{
			TerminalID: "term-1",
			Channel:    attachTwo.Channel,
			SurfaceID:  "surface-2",
			ViewID:     "pane:two",
			Data:       []byte("ls\n"),
		})
	}()
	select {
	case err := <-inputCh:
		if err != nil {
			t.Fatalf("sibling view input should not wait for slow history latest: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("sibling view input was blocked behind slow history latest")
	}
	inputs, _, _, _ := process.snapshot()
	if len(inputs) != 1 || string(inputs[0]) != "ls\n" {
		t.Fatalf("expected input to reach process immediately, got %#v", inputs)
	}

	select {
	case err := <-latestCh:
		if err != nil {
			t.Fatalf("latest should not wait for its own history barrier: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("latest waited for its own history barrier")
	}
	release()
	if attachOne.Channel == 0 {
		t.Fatalf("expected first attachment to be valid: %#v", attachOne)
	}
}

func TestProtocolServiceFrozenSnapshotReflowsCombiningAndWideStyledCells(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    protocol.Size{Cols: 20, Rows: 4},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	output := "\x1b[35;1me\u0301好\x1b[0m\n"
	if err := server.IngestOutput(context.Background(), "term-1", output); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       2,
		Limit:      4,
	})
	if err != nil {
		t.Fatalf("history.window: %v", err)
	}
	if got := len(latest.Rows); got != 2 {
		t.Fatalf("expected frozen snapshot projection to wrap into 2 rows, got %d rows %#v", got, latest.Rows)
	}
	if got := rowText(latest.Rows[0]); got != "e\u0301" {
		t.Fatalf("expected first row to keep combining grapheme intact, got %q", got)
	}
	if got := rowText(latest.Rows[1]); got != "好" {
		t.Fatalf("expected second row to keep wide grapheme intact, got %q", got)
	}
	firstCells := latest.Rows[0].DecodeCells()
	if len(firstCells) != 1 || firstCells[0].Content != "e\u0301" || firstCells[0].Width != 0 || firstCells[0].Style.FG != "ansi:5" || !firstCells[0].Style.Bold {
		t.Fatalf("expected combining grapheme to stay one styled display cell, got %#v", firstCells)
	}
	secondCells := latest.Rows[1].DecodeCells()
	if len(secondCells) != 1 || secondCells[0].Content != "好" || secondCells[0].Width != 2 || secondCells[0].Style.FG != "ansi:5" || !secondCells[0].Style.Bold {
		t.Fatalf("expected wide grapheme to stay one styled display cell, got %#v", secondCells)
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

	if err := client.InputWithOptions(context.Background(), protocol.InputParams{
		TerminalID: "term-1",
		Channel:    attach.Channel,
		SurfaceID:  "surface-1",
		ViewID:     "view-1",
		Data:       []byte("echo hi\n"),
	}); err != nil {
		t.Fatalf("input: %v", err)
	}
	if err := client.InputWithOptions(context.Background(), protocol.InputParams{
		TerminalID: "missing",
		Channel:    attach.Channel,
		SurfaceID:  "surface-1",
		ViewID:     "view-1",
		Data:       []byte("bad\n"),
	}); err == nil {
		t.Fatal("expected input to reject mismatched terminal/channel")
	}
	if err := client.InputWithOptions(context.Background(), protocol.InputParams{
		TerminalID: "term-1",
		Channel:    attach.Channel,
		SurfaceID:  "surface-2",
		ViewID:     "view-1",
		Data:       []byte("bad\n"),
	}); err == nil {
		t.Fatal("expected input to reject mismatched surface")
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

func TestProtocolServiceEventsSubscriptionsDoNotCancelEachOther(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 3}}); err != nil {
		t.Fatalf("create term-1: %v", err)
	}
	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-2", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 3}}); err != nil {
		t.Fatalf("create term-2: %v", err)
	}
	termOneEvents, err := client.Events(context.Background(), protocol.EventsParams{
		TerminalID: "term-1",
		Types:      []protocol.EventType{protocol.EventTerminalStateChanged},
	})
	if err != nil {
		t.Fatalf("events term-1: %v", err)
	}
	termTwoEvents, err := client.Events(context.Background(), protocol.EventsParams{
		TerminalID: "term-2",
		Types:      []protocol.EventType{protocol.EventTerminalStateChanged},
	})
	if err != nil {
		t.Fatalf("events term-2: %v", err)
	}

	serverProcess(t, server, "term-1").emitOutput("one\n")
	if event := requireProtocolEvent(t, termOneEvents); event.TerminalID != "term-1" || event.Type != protocol.EventTerminalStateChanged {
		t.Fatalf("expected term-1 changed event, got %#v", event)
	}
	requireNoProtocolEvent(t, termTwoEvents)

	serverProcess(t, server, "term-2").emitOutput("two\n")
	if event := requireProtocolEvent(t, termTwoEvents); event.TerminalID != "term-2" || event.Type != protocol.EventTerminalStateChanged {
		t.Fatalf("expected term-2 changed event, got %#v", event)
	}
	requireNoProtocolEvent(t, termOneEvents)
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
	nextOwner, err := client.AttachWithOptions(context.Background(), protocol.AttachParams{TerminalID: "term-1", ResizePolicy: protocol.ResizePolicyOwner, SurfaceID: "surface-next", ViewID: "view-next"})
	if err != nil {
		t.Fatalf("attach next owner: %v", err)
	}
	if nextOwner.ResizeControl == nil || !nextOwner.ResizeControl.CanResize || nextOwner.ResizeControl.OwnerViewID != "view-next" {
		t.Fatalf("explicit owner attach should take resize owner, got %#v", nextOwner.ResizeControl)
	}
	owner = nextOwner

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
	sameSize, err := client.EnsureResize(context.Background(), protocol.EnsureResizeParams{TerminalID: "term-1", Channel: owner.Channel, Cols: 30, Rows: 8, ResizePolicy: protocol.ResizePolicyOwner, SurfaceID: "surface-owner", ViewID: "view-owner"})
	if err != nil {
		t.Fatalf("same-size owner ensure_resize: %v", err)
	}
	if sameSize.Resized || sameSize.Size != (protocol.Size{Cols: 30, Rows: 8}) || sameSize.ResizeControl == nil || !sameSize.ResizeControl.CanResize {
		t.Fatalf("same-size owner ensure_resize should refresh control without PTY resize, got %#v", sameSize)
	}
	_, resizes, _, _ = process.snapshot()
	if len(resizes) != 1 {
		t.Fatalf("same-size ensure_resize must not reach process again, got %#v", resizes)
	}
	transfer, err := client.EnsureResize(context.Background(), protocol.EnsureResizeParams{TerminalID: "term-1", Channel: follower.Channel, Cols: 30, Rows: 8, ResizePolicy: protocol.ResizePolicyOwner, SurfaceID: "surface-follower", ViewID: "view-follower"})
	if err != nil {
		t.Fatalf("same-size owner transfer ensure_resize: %v", err)
	}
	if transfer.Resized || transfer.ResizeControl == nil || !transfer.ResizeControl.CanResize || transfer.ResizeControl.OwnerViewID != "view-follower" {
		t.Fatalf("same-size owner transfer should refresh ownership without PTY resize, got %#v", transfer)
	}
	_, resizes, _, _ = process.snapshot()
	if len(resizes) != 1 {
		t.Fatalf("same-size owner transfer must not resize process again, got %#v", resizes)
	}
	var info protocol.TerminalInfo
	if err := client.Call(context.Background(), "get", protocol.GetParams{TerminalID: "term-1"}, &info); err != nil {
		t.Fatalf("get: %v", err)
	}
	if info.ResizeOwnership == nil || info.ResizeOwnership.OwnerViewID != "view-follower" || info.ResizeOwnerAttachmentCount != 3 {
		t.Fatalf("expected ownership summary in terminal info, got %#v", info)
	}
}

func TestProtocolServiceResizeOwnershipIsGlobalAcrossClientSessions(t *testing.T) {
	factory := newRecordingProcessFactory()
	server := NewServer(WithProcessFactory(factory))
	clientOneTransport, serverOneTransport := memory.NewPair()
	clientTwoTransport, serverTwoTransport := memory.NewPair()
	errOne := make(chan error, 1)
	errTwo := make(chan error, 1)
	go func() { errOne <- newProtocolSession(server, serverOneTransport).run(context.Background()) }()
	go func() { errTwo <- newProtocolSession(server, serverTwoTransport).run(context.Background()) }()
	clientOne := protocol.NewClient(clientOneTransport)
	clientTwo := protocol.NewClient(clientTwoTransport)
	defer func() {
		_ = clientOne.Close()
		_ = clientTwo.Close()
		for _, errCh := range []chan error{errOne, errTwo} {
			select {
			case err := <-errCh:
				if err != nil && !strings.Contains(err.Error(), "EOF") {
					t.Fatalf("server session returned error: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("server session did not stop")
			}
		}
	}()
	if err := clientOne.Hello(context.Background(), protocol.Hello{Version: wire.Version, Client: "test-1"}); err != nil {
		t.Fatalf("hello one: %v", err)
	}
	if err := clientTwo.Hello(context.Background(), protocol.Hello{Version: wire.Version, Client: "test-2"}); err != nil {
		t.Fatalf("hello two: %v", err)
	}
	if _, err := clientOne.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 3}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	owner, err := clientOne.AttachWithOptions(context.Background(), protocol.AttachParams{TerminalID: "term-1", ResizePolicy: protocol.ResizePolicyOwner, SurfaceID: "surface-one", ViewID: "pane:main"})
	if err != nil {
		t.Fatalf("attach owner: %v", err)
	}
	follower, err := clientTwo.AttachWithOptions(context.Background(), protocol.AttachParams{TerminalID: "term-1", ResizePolicy: protocol.ResizePolicyFollower, SurfaceID: "surface-two", ViewID: "pane:main"})
	if err != nil {
		t.Fatalf("attach follower: %v", err)
	}
	if owner.ResizeControl == nil || !owner.ResizeControl.CanResize || owner.ResizeControl.OwnerSurfaceID != "surface-one" || owner.ResizeControl.OwnerViewID != "pane:main" {
		t.Fatalf("first client should own resize globally, got %#v", owner.ResizeControl)
	}
	if follower.ResizeControl == nil || follower.ResizeControl.CanResize || follower.ResizeControl.OwnerSurfaceID != "surface-one" || follower.ResizeControl.OwnerViewID != "pane:main" {
		t.Fatalf("same panel in another surface must be follower, got %#v", follower.ResizeControl)
	}
	result, err := clientTwo.EnsureResize(context.Background(), protocol.EnsureResizeParams{TerminalID: "term-1", Channel: follower.Channel, Cols: 10, Rows: 3, ResizePolicy: protocol.ResizePolicyOwner, SurfaceID: "surface-two", ViewID: "pane:main"})
	if err != nil {
		t.Fatalf("take owner same size: %v", err)
	}
	if result.ResizeControl == nil || !result.ResizeControl.CanResize || result.ResizeControl.OwnerSurfaceID != "surface-two" || result.ResizeControl.OwnerViewID != "pane:main" {
		t.Fatalf("second client should take global owner by surface+panel, got %#v", result.ResizeControl)
	}
	refresh, err := clientOne.EnsureResize(context.Background(), protocol.EnsureResizeParams{TerminalID: "term-1", Channel: owner.Channel, Cols: 10, Rows: 3, ResizePolicy: protocol.ResizePolicyFollower, SurfaceID: "surface-one", ViewID: "pane:main"})
	if err != nil {
		t.Fatalf("refresh first client: %v", err)
	}
	if refresh.ResizeControl == nil || refresh.ResizeControl.CanResize || refresh.ResizeControl.OwnerSurfaceID != "surface-two" || refresh.ResizeControl.OwnerViewID != "pane:main" {
		t.Fatalf("first client should now see other surface owner, got %#v", refresh.ResizeControl)
	}
	var info protocol.TerminalInfo
	if err := clientOne.Call(context.Background(), "get", protocol.GetParams{TerminalID: "term-1"}, &info); err != nil {
		t.Fatalf("get: %v", err)
	}
	if info.ResizeOwnerAttachmentCount != 2 || info.ResizeOwnership == nil || info.ResizeOwnership.OwnerSurfaceID != "surface-two" || info.ResizeOwnership.OwnerViewID != "pane:main" {
		t.Fatalf("terminal info should expose global attachment count and owner, got %#v", info)
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
	if err := client.InputWithOptions(context.Background(), protocol.InputParams{TerminalID: "term-1", Channel: first.Channel, SurfaceID: "surface-1", ViewID: "view-1", Data: []byte("old-acked\n")}); err == nil {
		t.Fatal("acked input must reject detached channel")
	}
	if err := client.Input(context.Background(), first.Channel, []byte("old\n")); err != nil {
		t.Fatalf("detached first input frame send: %v", err)
	}
	if err := client.InputWithOptions(context.Background(), protocol.InputParams{TerminalID: "term-1", Channel: second.Channel, SurfaceID: "surface-2", ViewID: "view-2", Data: []byte("new\n")}); err != nil {
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
	if got[0] != "alpha" || !strings.HasPrefix(got[1], "OK   ERR ok") || len(snapshot.Scrollback) != 0 {
		t.Fatalf("snapshot must expose live screen cell matrix without scrollback truth, got rows=%#v scrollback=%#v", got, snapshot.Scrollback)
	}
	if snapshot.Screen.Cells[1][0].Style.FG != "ansi:2" {
		t.Fatalf("snapshot must preserve live cell style, got %#v", snapshot.Screen.Cells[1][0])
	}
	if !snapshot.Cursor.Visible || snapshot.Cursor.Row != 1 || snapshot.Cursor.Col == 0 {
		t.Fatalf("snapshot must preserve live cursor, got %#v", snapshot.Cursor)
	}

	compact, err := client.SnapshotCompact(context.Background(), "term-1", 0, 2)
	if err != nil {
		t.Fatalf("compact snapshot: %v", err)
	}
	if compact.TerminalID != "term-1" || compact.Size != (protocol.Size{Cols: 12, Rows: 4}) {
		t.Fatalf("unexpected compact snapshot metadata %#v", compact)
	}
	if len(compact.ScreenRows) != 4 {
		t.Fatalf("expected compact snapshot to preserve screen row count, got %#v", compact.ScreenRows)
	}
	if got := compact.ScreenRows[0].Text; got != "alpha" {
		t.Fatalf("compact snapshot should keep row compact text, got %#v", compact.ScreenRows[0])
	}
	if got := compact.ScreenRows[1].DecodeCells(); len(got) == 0 || got[0].Style.FG != "ansi:2" {
		t.Fatalf("compact snapshot must preserve live style on demand, got %#v", got)
	}
}

func TestProtocolServiceSnapshotTrimsPlainBlankTailOnly(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 8, Rows: 3}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "abc"); err != nil {
		t.Fatalf("ingest plain: %v", err)
	}

	snapshot, err := client.Snapshot(context.Background(), "term-1", 0, 3)
	if err != nil {
		t.Fatalf("snapshot plain: %v", err)
	}
	if got := len(snapshot.Screen.Cells[0]); got != 3 {
		t.Fatalf("plain live row should trim default blank tail before protocol allocation, got len=%d row=%#v", got, snapshot.Screen.Cells[0])
	}
	if got := cellsText(snapshot.Screen.Cells[0]); got != "abc" {
		t.Fatalf("unexpected trimmed plain row text %q", got)
	}

	if err := server.IngestOutput(context.Background(), "term-1", "\r\n\x1b[44mxy  "); err != nil {
		t.Fatalf("ingest styled blanks: %v", err)
	}
	snapshot, err = client.Snapshot(context.Background(), "term-1", 0, 3)
	if err != nil {
		t.Fatalf("snapshot styled: %v", err)
	}
	if got := len(snapshot.Screen.Cells[1]); got < 4 {
		t.Fatalf("styled trailing blanks must survive live snapshot protocol trim, got len=%d row=%#v", got, snapshot.Screen.Cells[1])
	}
	for _, cell := range snapshot.Screen.Cells[1][:4] {
		if cell.Style.BG != "ansi:4" {
			t.Fatalf("styled live row cell should keep background through protocol trim, got %#v", snapshot.Screen.Cells[1][:4])
		}
	}
}

func TestProtocolServiceSnapshotKeepsAltScreenFinalFrameOnExit(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 20, Rows: 3}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "primary\n\x1b[?1049h\x1b[2Jalt-final\x1b[?1049l"); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	snapshot, err := client.Snapshot(context.Background(), "term-1", 0, 3)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.Screen.IsAlternateScreen || snapshot.Modes.AlternateScreen {
		t.Fatalf("alt exit should expose final frame as primary live screen, got %#v", snapshot)
	}
	gotRows := []string{
		cellsText(snapshot.Screen.Cells[0]),
		cellsText(snapshot.Screen.Cells[1]),
		cellsText(snapshot.Screen.Cells[2]),
	}
	if got := strings.Join(gotRows, "\n"); !strings.Contains(got, "primary") || !strings.Contains(got, "alt-final") {
		t.Fatalf("snapshot should keep primary tail and append alt final frame, got %#v", gotRows)
	}

	latest, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-1",
		Cols:       20,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if len(latest.Rows) != 2 || rowText(latest.Rows[0]) != "primary" || rowText(latest.Rows[1]) != "alt-final" || latest.LogicalTotal != 2 {
		t.Fatalf("alt final frame should enter primary history, got %#v", latest)
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

func protocolHistoryWindowContainsText(window *protocol.HistoryWindow, want string) bool {
	if window == nil {
		return false
	}
	for _, row := range window.Rows {
		if strings.Contains(rowText(row), want) {
			return true
		}
	}
	return false
}

func waitForTerminalState(t *testing.T, server *Server, terminalID string, want TerminalState) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		info, err := server.GetTerminal(terminalID)
		if err == nil && info.State == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	info, err := server.GetTerminal(terminalID)
	if err != nil {
		t.Fatalf("timed out waiting for terminal %q state %q: %v", terminalID, want, err)
	}
	t.Fatalf("timed out waiting for terminal %q state %q, got %#v", terminalID, want, info)
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

func requireNoProtocolEvent(t *testing.T, events <-chan protocol.Event) {
	t.Helper()
	select {
	case event, ok := <-events:
		if ok {
			t.Fatalf("unexpected protocol event %#v", event)
		}
		t.Fatal("protocol events channel closed")
	case <-time.After(50 * time.Millisecond):
	}
}

func frozenSnapshotForBenchmark(lines int) history.FrozenSnapshot {
	snapshotLines := make([]history.SnapshotLine, 0, lines)
	for i := 1; i <= lines; i++ {
		text := "line-" + strconv.Itoa(i)
		snapshotLines = append(snapshotLines, history.SnapshotLine{
			Line: history.LogicalLine{
				ID:         history.LogicalLineID(i),
				Generation: 1,
				Seal:       history.SealStateSealed,
				Cells:      []history.Cell{{Text: text, Width: len(text)}},
				Residency:  history.ResidencyMemory,
			},
			Committed: true,
		})
	}
	return history.NewDetachedFrozenSnapshot("bench-snapshot", 1, snapshotLines)
}

func rowText(row protocol.CompactRow) string {
	var builder strings.Builder
	for _, cell := range row.DecodeCells() {
		builder.WriteString(cell.Content)
	}
	return builder.String()
}

func historyWindowRowsText(rows []protocol.CompactRow) string {
	parts := make([]string, len(rows))
	for index, row := range rows {
		if row.Text != "" {
			parts[index] = row.Text
		} else {
			parts[index] = rowText(row)
		}
	}
	return strings.Join(parts, "\n")
}

func cellsText(row []protocol.Cell) string {
	var builder strings.Builder
	for _, cell := range row {
		builder.WriteString(cell.Content)
	}
	return builder.String()
}
