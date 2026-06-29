package termxcorev2

import (
	"context"
	"strings"
	"testing"

	"github.com/lozzow/termx/internal/protocol"
)

func TestProtocolServiceHistoryWindowCopyRelease(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()
	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-protocol-history",
		Command: []string{"shell"},
		Size:    protocol.Size{Cols: 24, Rows: 4},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-protocol-history", "alpha\r\nbeta\r\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-protocol-history",
		Cols:       24,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("history.window: %v", err)
	}
	if got := strings.Join(protocolHistoryRowTexts(window.Rows), "|"); got != "alpha|beta" {
		t.Fatalf("unexpected history rows %q window=%#v", got, window)
	}
	if window.Token == "" || window.Generation == 0 || len(window.RowLineIDs) < 2 {
		t.Fatalf("history.window should carry frozen token/generation/line ids, got %#v", window)
	}
	if len(window.RowOwnership) < 2 || window.RowOwnership[0] == "" || len(window.RowSegments) < 2 || window.RowSegments[0] == "" {
		t.Fatalf("history.window should carry row ownership and segment truth, got %#v", window)
	}

	text, err := client.HistoryCopy(context.Background(), protocol.HistoryWindowParams{
		TerminalID:       "term-protocol-history",
		Token:            window.Token,
		Generation:       window.Generation,
		RangeValid:       true,
		RangeStartLineID: window.RowLineIDs[0],
		RangeEndLineID:   window.RowLineIDs[1],
		RangeEndCol:      len("beta"),
		CursorSegment:    window.RowSegments[0],
	})
	if err != nil {
		t.Fatalf("history.copy: %v", err)
	}
	if !strings.Contains(text, "alpha") || !strings.Contains(text, "beta") {
		t.Fatalf("history.copy should read frozen authoritative text, got %q", text)
	}
	if err := client.ReleaseHistory(context.Background(), protocol.HistoryWindowParams{
		TerminalID: "term-protocol-history",
		Token:      window.Token,
	}); err != nil {
		t.Fatalf("history.release: %v", err)
	}
}

func protocolHistoryRowTexts(rows []protocol.CompactRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		var builder strings.Builder
		for _, cell := range row.DecodeCells() {
			builder.WriteString(cell.Content)
		}
		out = append(out, builder.String())
	}
	return out
}
