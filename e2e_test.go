package termx

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/transport/memory"
)

// newE2EClient creates a Server, a memory transport pair, and a protocol Client
// ready for end-to-end testing. It returns the client and a cleanup function.
func newE2EClient(t *testing.T, opts ...ServerOption) (*Server, *protocol.Client, func()) {
	t.Helper()

	defaults := []ServerOption{
		WithDefaultScrollback(128),
		WithDefaultKeepAfterExit(2 * time.Second),
	}
	srv := NewServer(append(defaults, opts...)...)

	ctx, cancel := context.WithCancel(context.Background())
	clientTransport, serverTransport := memory.NewPair()

	go func() {
		_ = srv.handleTransport(ctx, serverTransport, "memory")
	}()

	client := protocol.NewClient(clientTransport)
	if err := client.Hello(ctx, protocol.Hello{Version: protocol.Version, Client: "e2e-test"}); err != nil {
		cancel()
		clientTransport.Close()
		serverTransport.Close()
		t.Fatalf("hello failed: %v", err)
	}

	cleanup := func() {
		client.Close()
		cancel()
		clientTransport.Close()
		serverTransport.Close()
	}
	return srv, client, cleanup
}

func newE2EProtocolClient(t *testing.T, srv *Server) (*protocol.Client, func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	clientTransport, serverTransport := memory.NewPair()

	go func() {
		_ = srv.handleTransport(ctx, serverTransport, "memory")
	}()

	client := protocol.NewClient(clientTransport)
	if err := client.Hello(ctx, protocol.Hello{Version: protocol.Version, Client: "e2e-test"}); err != nil {
		cancel()
		clientTransport.Close()
		serverTransport.Close()
		t.Fatalf("hello failed: %v", err)
	}

	cleanup := func() {
		client.Close()
		cancel()
		clientTransport.Close()
		serverTransport.Close()
	}
	return client, cleanup
}

// e2eCreateTerminal is a helper that creates a bash terminal via protocol.
func e2eCreateTerminal(t *testing.T, client *protocol.Client, name string, tags map[string]string) string {
	t.Helper()
	ctx := context.Background()
	created, err := client.Create(ctx, protocol.CreateParams{
		Command: []string{"bash", "--noprofile", "--norc"},
		Name:    name,
		Tags:    tags,
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted: %v", err)
		}
		t.Fatalf("create %q failed: %v", name, err)
	}
	return created.TerminalID
}

// e2eAttach attaches to a terminal and returns the stream channel and stop function.
func e2eAttach(t *testing.T, client *protocol.Client, terminalID, mode string) (uint16, <-chan protocol.StreamFrame, func()) {
	t.Helper()
	ctx := context.Background()
	attach, err := client.Attach(ctx, terminalID, mode)
	if err != nil {
		t.Fatalf("attach %q failed: %v", terminalID, err)
	}
	stream, stop := client.Stream(attach.Channel)
	return attach.Channel, stream, stop
}

// waitStreamContains waits for the stream to contain needle within timeout.
func waitStreamContains(t *testing.T, stream <-chan protocol.StreamFrame, needle string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for stream to contain %q", needle)
		case msg, ok := <-stream:
			if !ok {
				t.Fatalf("stream closed while waiting for %q", needle)
			}
			if streamFrameContainsText(msg, needle) {
				return
			}
		}
	}
}

// waitStreamClosed waits for a TypeClosed frame within timeout.
func waitStreamClosed(t *testing.T, stream <-chan protocol.StreamFrame, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for stream close")
		case msg, ok := <-stream:
			if !ok {
				return // channel closed
			}
			if msg.Type == protocol.TypeClosed {
				return
			}
		}
	}
}

func streamFrameContainsText(frame protocol.StreamFrame, needle string) bool {
	switch frame.Type {
	case protocol.TypeOutput:
		return strings.Contains(string(frame.Payload), needle)
	case protocol.TypeScreenUpdate:
		update, err := protocol.DecodeScreenUpdatePayload(frame.Payload)
		if err != nil {
			return false
		}
		for _, row := range update.Screen.Cells {
			if strings.Contains(protocolCellsText(row), needle) {
				return true
			}
		}
		for _, row := range update.ChangedRows {
			if strings.Contains(protocolCellsText(row.Cells), needle) {
				return true
			}
		}
		for _, row := range update.ScrollbackAppend {
			if strings.Contains(protocolCellsText(row.Cells), needle) {
				return true
			}
		}
	}
	return false
}

func protocolCellsText(row []protocol.Cell) string {
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(cell.Content)
	}
	return strings.TrimRight(b.String(), " ")
}

// doRawRequest sends a raw JSON-RPC request to the server via the protocol client.
// This is used for methods not yet exposed as convenience methods on protocol.Client
// (e.g. get, set_tags, detach, resize-by-id).
func doRawRequest(t *testing.T, ct *protocol.Client, method string, params any) json.RawMessage {
	t.Helper()
	// We don't have access to doRequest, so we use the existing client methods
	// or construct the request manually. Since the protocol.Client.doRequest is
	// unexported, we work around by calling the available methods.
	// For methods that need a workaround, we'll use the server directly.
	return nil
}

// =============================================================================
// Test: Full TUI workflow — create, attach, I/O, snapshot, resize, tags, kill
// =============================================================================

func TestE2E_FullTUIWorkflow(t *testing.T) {
	srv, client, cleanup := newE2EClient(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Create terminal
	termID := e2eCreateTerminal(t, client, "e2e-main", map[string]string{"group": "dev"})

	// 2. Attach as collaborator
	ch, stream, stopStream := e2eAttach(t, client, termID, "collaborator")
	defer stopStream()

	// 3. Send input and verify output through stream
	if err := client.Input(ctx, ch, []byte("echo e2e-hello\n")); err != nil {
		t.Fatalf("input failed: %v", err)
	}
	waitStreamContains(t, stream, "e2e-hello", 5*time.Second)

	// 4. Verify snapshot captures the output
	snap, err := client.Snapshot(ctx, termID, 0, 50)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if !protocolSnapshotContains(snap, "e2e-hello") {
		t.Fatalf("snapshot missing 'e2e-hello'")
	}

	// 5. Resize via channel and verify server-side update
	if err := client.Resize(ctx, ch, 120, 40); err != nil {
		t.Fatalf("resize failed: %v", err)
	}
	// Give server time to process resize
	time.Sleep(100 * time.Millisecond)
	info, err := srv.Get(ctx, termID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if info.Size.Cols != 120 || info.Size.Rows != 40 {
		t.Fatalf("resize not applied: got %dx%d, want 120x40", info.Size.Cols, info.Size.Rows)
	}

	// 6. Set tags and verify
	if err := srv.SetTags(ctx, termID, map[string]string{"status": "active"}); err != nil {
		t.Fatalf("set tags failed: %v", err)
	}
	info, err = srv.Get(ctx, termID)
	if err != nil {
		t.Fatalf("get after set_tags failed: %v", err)
	}
	if info.Tags["group"] != "dev" || info.Tags["status"] != "active" {
		t.Fatalf("unexpected tags: %v", info.Tags)
	}

	// 7. Kill and verify stream closure
	if err := client.Kill(ctx, termID); err != nil {
		t.Fatalf("kill failed: %v", err)
	}
	waitStreamClosed(t, stream, 5*time.Second)
}

// =============================================================================
// Test: Multiple terminals — list, filter by tags, independent I/O
// =============================================================================

func TestE2E_MultipleTerminals(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	ctx := context.Background()

	// Create two terminals with different tags
	id1 := e2eCreateTerminal(t, client, "term-a", map[string]string{"group": "build"})
	id2 := e2eCreateTerminal(t, client, "term-b", map[string]string{"group": "logs"})

	// List and verify both exist
	listResult, err := client.List(ctx)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(listResult.Terminals) != 2 {
		t.Fatalf("expected 2 terminals, got %d", len(listResult.Terminals))
	}

	// Attach to both, send different commands
	ch1, stream1, stop1 := e2eAttach(t, client, id1, "collaborator")
	defer stop1()
	ch2, stream2, stop2 := e2eAttach(t, client, id2, "collaborator")
	defer stop2()

	if err := client.Input(ctx, ch1, []byte("echo from-term-a\n")); err != nil {
		t.Fatalf("input to term-a failed: %v", err)
	}
	if err := client.Input(ctx, ch2, []byte("echo from-term-b\n")); err != nil {
		t.Fatalf("input to term-b failed: %v", err)
	}

	waitStreamContains(t, stream1, "from-term-a", 5*time.Second)
	waitStreamContains(t, stream2, "from-term-b", 5*time.Second)

	// Kill first, second should still work
	if err := client.Kill(ctx, id1); err != nil {
		t.Fatalf("kill term-a failed: %v", err)
	}
	waitStreamClosed(t, stream1, 5*time.Second)

	// Verify term-b still responds
	if err := client.Input(ctx, ch2, []byte("echo still-alive\n")); err != nil {
		t.Fatalf("input to term-b after killing term-a failed: %v", err)
	}
	waitStreamContains(t, stream2, "still-alive", 5*time.Second)

	// Cleanup
	if err := client.Kill(ctx, id2); err != nil {
		t.Fatalf("kill term-b failed: %v", err)
	}
}

// =============================================================================
// Test: Events subscription — create/state-change/remove events over protocol
// =============================================================================

func TestE2E_EventsSubscription(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	ctx := context.Background()

	// Subscribe to events before creating terminals
	events, err := client.Events(ctx, protocol.EventsParams{})
	if err != nil {
		t.Fatalf("events subscription failed: %v", err)
	}

	// Create terminal — should trigger EventTerminalCreated
	termID := e2eCreateTerminal(t, client, "evt-test", nil)

	deadline := time.After(5 * time.Second)
	gotCreated := false
	for !gotCreated {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for create event")
		case evt := <-events:
			if evt.Type == protocol.EventTerminalCreated && evt.TerminalID == termID {
				gotCreated = true
			}
		}
	}

	// Kill terminal — should trigger state change and removal events
	if err := client.Kill(ctx, termID); err != nil {
		t.Fatalf("kill failed: %v", err)
	}

	deadline = time.After(5 * time.Second)
	gotStateChange := false
	gotRemoved := false
	for !gotStateChange || !gotRemoved {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for events: stateChange=%v removed=%v", gotStateChange, gotRemoved)
		case evt := <-events:
			if evt.TerminalID != termID {
				continue
			}
			switch evt.Type {
			case protocol.EventTerminalStateChanged:
				if evt.StateChanged != nil && evt.StateChanged.NewState == "exited" {
					gotStateChange = true
				}
			case protocol.EventTerminalRemoved:
				gotRemoved = true
			}
		}
	}
}

// =============================================================================
// Test: Observer mode — cannot write input
// =============================================================================

func TestE2E_ObserverCannotWrite(t *testing.T) {
	srv, client, cleanup := newE2EClient(t)
	defer cleanup()

	ctx := context.Background()

	termID := e2eCreateTerminal(t, client, "observer-test", nil)

	ch, _, stop := e2eAttach(t, client, termID, "observer")
	defer stop()

	// Observer input should be silently ignored (no error from protocol, but
	// input not delivered). We verify by sending input as observer, then
	// checking snapshot doesn't contain it.
	_ = client.Input(ctx, ch, []byte("echo observer-input\n"))
	time.Sleep(500 * time.Millisecond)

	snap, err := client.Snapshot(ctx, termID, 0, 50)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if protocolSnapshotContains(snap, "observer-input") {
		t.Fatal("observer was able to write input — should be rejected")
	}

	if err := srv.Kill(ctx, termID); err != nil {
		t.Fatalf("cleanup kill failed: %v", err)
	}
}

func TestE2E_ObserverCannotKill(t *testing.T) {
	srv, client, cleanup := newE2EClient(t)
	defer cleanup()

	ctx := context.Background()

	termID := e2eCreateTerminal(t, client, "observer-kill-test", nil)
	_, _, stop := e2eAttach(t, client, termID, "observer")
	defer stop()

	err := client.Kill(ctx, termID)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected observer kill to be denied, got %v", err)
	}

	if _, err := srv.Get(ctx, termID); err != nil {
		t.Fatalf("expected terminal to remain after denied observer kill, got %v", err)
	}

	if err := srv.Kill(ctx, termID); err != nil {
		t.Fatalf("cleanup kill failed: %v", err)
	}
}

// =============================================================================
// Test: Snapshot reconnection — subscribe, snapshot, verify consistency
// =============================================================================

func TestE2E_SnapshotReconnection(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	ctx := context.Background()

	termID := e2eCreateTerminal(t, client, "snap-test", nil)

	// Attach and produce some output
	ch, stream, stop := e2eAttach(t, client, termID, "collaborator")

	if err := client.Input(ctx, ch, []byte("echo line-one\n")); err != nil {
		t.Fatalf("input failed: %v", err)
	}
	waitStreamContains(t, stream, "line-one", 5*time.Second)

	if err := client.Input(ctx, ch, []byte("echo line-two\n")); err != nil {
		t.Fatalf("input failed: %v", err)
	}
	waitStreamContains(t, stream, "line-two", 5*time.Second)

	// Stop first subscription (simulate disconnect)
	stop()

	// Simulate reconnection: new attach + snapshot
	ch2, stream2, stop2 := e2eAttach(t, client, termID, "collaborator")
	defer stop2()

	snap, err := client.Snapshot(ctx, termID, 0, 100)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}

	// Snapshot should contain both lines
	if !protocolSnapshotContains(snap, "line-one") {
		t.Fatal("snapshot missing 'line-one' after reconnection")
	}
	if !protocolSnapshotContains(snap, "line-two") {
		t.Fatal("snapshot missing 'line-two' after reconnection")
	}

	// New stream should still work
	if err := client.Input(ctx, ch2, []byte("echo line-three\n")); err != nil {
		t.Fatalf("input on new stream failed: %v", err)
	}
	waitStreamContains(t, stream2, "line-three", 5*time.Second)

	if err := client.Kill(ctx, termID); err != nil {
		t.Fatalf("kill failed: %v", err)
	}
}

// =============================================================================
// Test: Resize via protocol — channel-level resize
// =============================================================================

func TestE2E_ResizeViaProtocol(t *testing.T) {
	srv, client, cleanup := newE2EClient(t)
	defer cleanup()

	ctx := context.Background()

	termID := e2eCreateTerminal(t, client, "resize-test", nil)

	ch, _, stop := e2eAttach(t, client, termID, "collaborator")
	defer stop()

	// Resize to several sizes
	sizes := [][2]uint16{{100, 30}, {200, 50}, {80, 24}}
	for _, sz := range sizes {
		if err := client.Resize(ctx, ch, sz[0], sz[1]); err != nil {
			t.Fatalf("resize to %dx%d failed: %v", sz[0], sz[1], err)
		}
		time.Sleep(100 * time.Millisecond)

		info, err := srv.Get(ctx, termID)
		if err != nil {
			t.Fatalf("get failed: %v", err)
		}
		if info.Size.Cols != sz[0] || info.Size.Rows != sz[1] {
			t.Fatalf("resize to %dx%d not applied: got %dx%d", sz[0], sz[1], info.Size.Cols, info.Size.Rows)
		}
	}

	if err := client.Kill(ctx, termID); err != nil {
		t.Fatalf("kill failed: %v", err)
	}
}

// =============================================================================
// Test: Terminal natural exit — process exits, stream gets TypeClosed
// =============================================================================

func TestE2E_NaturalExit(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	ctx := context.Background()

	termID := e2eCreateTerminal(t, client, "exit-test", nil)

	ch, stream, stop := e2eAttach(t, client, termID, "collaborator")
	defer stop()

	// Tell bash to exit
	if err := client.Input(ctx, ch, []byte("exit 42\n")); err != nil {
		t.Fatalf("input failed: %v", err)
	}

	// Should receive TypeClosed
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for natural exit close")
		case msg, ok := <-stream:
			if !ok {
				return // closed
			}
			if msg.Type == protocol.TypeClosed {
				code, err := protocol.DecodeClosedPayload(msg.Payload)
				if err != nil {
					t.Fatalf("decode closed payload failed: %v", err)
				}
				if code != 42 {
					t.Fatalf("expected exit code 42, got %d", code)
				}
				return
			}
		}
	}
}

// =============================================================================
// Test: Two clients attaching to the same terminal
// =============================================================================

func TestE2E_MultiClientAttach(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := NewServer(
		WithDefaultScrollback(128),
		WithDefaultKeepAfterExit(200*time.Millisecond),
	)

	// Client 1
	ct1, st1 := memory.NewPair()
	go func() { _ = srv.handleTransport(ctx, st1, "client-1") }()
	client1 := protocol.NewClient(ct1)
	if err := client1.Hello(ctx, protocol.Hello{Version: protocol.Version, Client: "client-1"}); err != nil {
		t.Fatalf("hello client-1 failed: %v", err)
	}
	defer client1.Close()
	defer ct1.Close()
	defer st1.Close()

	// Client 2
	ct2, st2 := memory.NewPair()
	go func() { _ = srv.handleTransport(ctx, st2, "client-2") }()
	client2 := protocol.NewClient(ct2)
	if err := client2.Hello(ctx, protocol.Hello{Version: protocol.Version, Client: "client-2"}); err != nil {
		t.Fatalf("hello client-2 failed: %v", err)
	}
	defer client2.Close()
	defer ct2.Close()
	defer st2.Close()

	// Client 1 creates a terminal
	created, err := client1.Create(ctx, protocol.CreateParams{
		Command: []string{"bash", "--noprofile", "--norc"},
		Name:    "shared",
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted: %v", err)
		}
		t.Fatalf("create failed: %v", err)
	}
	termID := created.TerminalID

	// Both clients attach
	ch1, stream1, stop1 := e2eAttach(t, client1, termID, "collaborator")
	defer stop1()
	_, stream2, stop2 := e2eAttach(t, client2, termID, "collaborator")
	defer stop2()

	// Client 1 types, both should see output
	if err := client1.Input(ctx, ch1, []byte("echo multi-client\n")); err != nil {
		t.Fatalf("input failed: %v", err)
	}

	waitStreamContains(t, stream1, "multi-client", 5*time.Second)
	waitStreamContains(t, stream2, "multi-client", 5*time.Second)

	// Client 2 can also see snapshot
	snap, err := client2.Snapshot(ctx, termID, 0, 50)
	if err != nil {
		t.Fatalf("snapshot from client-2 failed: %v", err)
	}
	if !protocolSnapshotContains(snap, "multi-client") {
		t.Fatal("client-2 snapshot missing 'multi-client'")
	}

	if err := client1.Kill(ctx, termID); err != nil {
		t.Fatalf("kill failed: %v", err)
	}
}

// =============================================================================
// Test: Error cases — kill nonexistent, attach nonexistent, create invalid
// =============================================================================

func TestE2E_ErrorCases(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	ctx := context.Background()

	// Kill nonexistent terminal
	err := client.Kill(ctx, "nonexistent-id")
	if err == nil {
		t.Fatal("expected error killing nonexistent terminal")
	}

	// Create with empty command
	_, err = client.Create(ctx, protocol.CreateParams{
		Command: []string{},
		Name:    "bad",
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err == nil {
		t.Fatal("expected error creating terminal with empty command")
	}

	// Snapshot nonexistent terminal
	_, err = client.Snapshot(ctx, "nonexistent-id", 0, 50)
	if err == nil {
		t.Fatal("expected error snapshotting nonexistent terminal")
	}

	// Attach nonexistent terminal
	_, err = client.Attach(ctx, "nonexistent-id", "collaborator")
	if err == nil {
		t.Fatal("expected error attaching nonexistent terminal")
	}
}

// =============================================================================
// Test: Resize validation — zero size rejected
// =============================================================================

func TestE2E_ResizeZeroRejected(t *testing.T) {
	srv, client, cleanup := newE2EClient(t)
	defer cleanup()

	ctx := context.Background()
	termID := e2eCreateTerminal(t, client, "resize-zero", nil)

	// Resize to 0x0 via server API (protocol resize is channel-level fire-and-forget)
	err := srv.Resize(ctx, termID, 0, 0)
	if err == nil {
		t.Fatal("expected error resizing to 0x0")
	}

	// Verify terminal still has valid size
	info, err := srv.Get(ctx, termID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if info.Size.Cols == 0 || info.Size.Rows == 0 {
		t.Fatalf("terminal has zero size after rejected resize: %dx%d", info.Size.Cols, info.Size.Rows)
	}

	if err := client.Kill(ctx, termID); err != nil {
		t.Fatalf("kill failed: %v", err)
	}
}

// =============================================================================
// Test: Events with terminal filter
// =============================================================================

func TestE2E_EventsFilteredByTerminal(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	ctx := context.Background()

	// Create first terminal
	id1 := e2eCreateTerminal(t, client, "filtered-a", nil)

	// Subscribe to events filtered to id1 only
	events, err := client.Events(ctx, protocol.EventsParams{TerminalID: id1})
	if err != nil {
		t.Fatalf("events subscription failed: %v", err)
	}

	// Create second terminal — should NOT produce an event on our filtered channel
	_ = e2eCreateTerminal(t, client, "filtered-b", nil)

	// Kill first terminal — should produce events
	if err := client.Kill(ctx, id1); err != nil {
		t.Fatalf("kill failed: %v", err)
	}

	deadline := time.After(5 * time.Second)
	gotEvent := false
	for !gotEvent {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for filtered event")
		case evt := <-events:
			// All events should be for id1
			if evt.TerminalID != id1 {
				t.Fatalf("received event for wrong terminal %q, expected %q", evt.TerminalID, id1)
			}
			if evt.Type == protocol.EventTerminalStateChanged {
				gotEvent = true
			}
		}
	}
}

// =============================================================================
// Test: Concurrent I/O — multiple goroutines writing to same terminal
// =============================================================================

func TestE2E_ConcurrentIO(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	ctx := context.Background()
	termID := e2eCreateTerminal(t, client, "concurrent-io", nil)

	ch, stream, stop := e2eAttach(t, client, termID, "collaborator")
	defer stop()

	// Send multiple echo commands concurrently
	markers := []string{"marker-a", "marker-b", "marker-c"}
	done := make(chan struct{})
	for _, m := range markers {
		go func(marker string) {
			_ = client.Input(ctx, ch, []byte("echo "+marker+"\n"))
		}(m)
	}

	// Wait for all markers to appear in stream
	seen := make(map[string]bool)
	timeout := time.After(10 * time.Second)
	for len(seen) < len(markers) {
		select {
		case <-timeout:
			t.Fatalf("timed out: only saw %v of %v", seen, markers)
		case msg := <-stream:
			for _, m := range markers {
				if streamFrameContainsText(msg, m) {
					seen[m] = true
				}
			}
		}
	}
	close(done)

	if err := client.Kill(ctx, termID); err != nil {
		t.Fatalf("kill failed: %v", err)
	}
}
