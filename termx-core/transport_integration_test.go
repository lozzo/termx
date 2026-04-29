package termx

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/termx-core/transport/memory"
)

func TestProtocolClientOverMemoryTransport(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := NewServer(WithDefaultScrollback(128))
	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	go func() {
		_ = srv.handleTransport(ctx, serverTransport, "memory")
	}()

	client := protocol.NewClient(clientTransport)
	if err := client.Hello(ctx, protocol.Hello{Version: protocol.Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	created, err := client.Create(ctx, protocol.CreateParams{
		Command: []string{"bash", "--noprofile", "--norc"},
		Name:    "proto",
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}

	attach, err := client.Attach(ctx, created.TerminalID, string(ModeCollaborator))
	if err != nil {
		t.Fatalf("attach failed: %v", err)
	}

	output, stopOutput := client.Stream(attach.Channel)
	defer stopOutput()

	if err := client.Input(ctx, attach.Channel, []byte("echo proto-test\n")); err != nil {
		t.Fatalf("input failed: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		msg := <-output
		if msg.Type == protocol.TypeOutput && strings.Contains(string(msg.Payload), "proto-test") {
			break
		}
	}

	snap, err := client.Snapshot(ctx, created.TerminalID, 0, 50)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if !protocolSnapshotContains(snap, "proto-test") {
		t.Fatalf("snapshot missing output: %#v", snap)
	}
}

func protocolSnapshotContains(s *protocol.Snapshot, needle string) bool {
	for _, row := range s.Scrollback {
		if protocolRowString(row) == needle {
			return true
		}
	}
	for _, row := range s.Screen.Cells {
		if strings.Contains(protocolRowString(row), needle) {
			return true
		}
	}
	return false
}

func protocolRowString(row []protocol.Cell) string {
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(cell.Content)
	}
	return strings.TrimRight(b.String(), " ")
}
