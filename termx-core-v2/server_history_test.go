package termxcorev2

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lozzow/termx/termx-core-v2/history"
)

func TestServerHistoryWindowReadsAuthoritativeStore(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()))
	info, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-history",
		Command: []string{"sh"},
		Size:    Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("register terminal: %v", err)
	}

	if err := server.IngestOutput(context.Background(), info.ID, "hello history\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	window, err := server.TerminalHistoryWindow(context.Background(), info.ID, history.HistoryWindowRequest{
		Cols:  80,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("history window: %v", err)
	}
	if len(window.Rows) == 0 {
		t.Fatalf("history window should expose rows")
	}
	if got := historyRowsText(window.Rows); !strings.Contains(got, "hello history") {
		t.Fatalf("history window should read authoritative payload, got %q rows=%#v", got, window.Rows)
	}
}

func TestServerHistoryDisabledReturnsExplicitError(t *testing.T) {
	server := NewServer(WithProcessFactory(newRecordingProcessFactory()), WithHistoryDisabled())
	info, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-no-history",
		Command: []string{"sh"},
		Size:    Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("register terminal: %v", err)
	}

	_, err = server.TerminalHistoryWindow(context.Background(), info.ID, history.HistoryWindowRequest{Cols: 80, Limit: 10})
	if !errors.Is(err, ErrHistoryDisabled) {
		t.Fatalf("disabled history should return ErrHistoryDisabled, got %v", err)
	}
}

func historyRowsText(rows []history.HistoryRow) string {
	var builder strings.Builder
	for _, row := range rows {
		for _, cell := range row.Cells {
			builder.WriteString(cell.Text)
		}
		builder.WriteByte('\n')
	}
	return builder.String()
}
