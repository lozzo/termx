package termx

import (
	"context"
	"strings"
	"testing"
	"time"

	localvterm "github.com/lozzow/termx/vterm"
)

func TestTerminalLifecycleAndSnapshot(t *testing.T) {
	ctx := context.Background()
	bus := NewEventBus(nil)

	term, err := newTerminal(ctx, bus, terminalConfig{
		ID:             "abc12345",
		Name:           "shell",
		Command:        []string{"bash", "--noprofile", "--norc"},
		Size:           Size{Cols: 80, Rows: 24},
		ScrollbackSize: 128,
		KeepAfterExit:  time.Second,
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("new terminal failed: %v", err)
	}
	defer term.Close()

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream := term.Subscribe(streamCtx)

	if err := term.WriteInput([]byte("echo hello-termx\n")); err != nil {
		t.Fatalf("write input failed: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		msg := <-stream
		if msg.Type == StreamOutput && strings.Contains(string(msg.Output), "hello-termx") {
			break
		}
	}

	snap := term.Snapshot(0, 50)
	if !snapshotContains(snap, "hello-termx") {
		t.Fatalf("snapshot missing output: %#v", snap)
	}

	if err := term.Resize(100, 40); err != nil {
		t.Fatalf("resize failed: %v", err)
	}

	if got := term.Info(); got.Size != (Size{Cols: 100, Rows: 40}) {
		t.Fatalf("unexpected size: %#v", got.Size)
	}

	if err := term.Kill(); err != nil {
		t.Fatalf("kill failed: %v", err)
	}

	select {
	case <-term.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for terminal exit")
	}

	if got := term.Info(); got.State != StateExited {
		t.Fatalf("unexpected state: %s", got.State)
	}
}

func TestSubscribeAfterExitReplaysSnapshotAndClosed(t *testing.T) {
	ctx := context.Background()
	bus := NewEventBus(nil)

	term, err := newTerminal(ctx, bus, terminalConfig{
		ID:             "exit1234",
		Name:           "env",
		Command:        []string{"sh", "-c", "echo replay-me; sleep 0.1; exit 0"},
		Size:           Size{Cols: 80, Rows: 24},
		ScrollbackSize: 128,
		KeepAfterExit:  time.Second,
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("new terminal failed: %v", err)
	}
	defer term.Close()

	select {
	case <-term.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for terminal exit")
	}

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream := term.Subscribe(streamCtx)

	select {
	case msg, ok := <-stream:
		if !ok {
			t.Fatal("expected replay output frame")
		}
		if msg.Type != StreamOutput || !strings.Contains(string(msg.Output), "replay-me") {
			t.Fatalf("expected replay output, got %#v", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for replay output")
	}

	select {
	case msg, ok := <-stream:
		if !ok {
			t.Fatal("expected closed frame")
		}
		if msg.Type != StreamClosed {
			t.Fatalf("expected closed frame, got %#v", msg)
		}
		if msg.ExitCode == nil || *msg.ExitCode != 0 {
			t.Fatalf("expected exit code 0, got %#v", msg.ExitCode)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for closed frame")
	}
}

func TestTerminalSnapshotReturnsNewestScrollbackWindow(t *testing.T) {
	vt := localvterm.New(4, 2, 16, nil)
	if _, err := vt.Write([]byte("1\n2\n3\n4\n5\n")); err != nil {
		t.Fatalf("write scrollback seed failed: %v", err)
	}

	term := &Terminal{
		id:    "snap-1",
		size:  Size{Cols: 4, Rows: 2},
		vterm: vt,
	}

	latest := term.Snapshot(0, 2)
	if len(latest.Scrollback) != 2 {
		t.Fatalf("expected 2 latest scrollback rows, got %d", len(latest.Scrollback))
	}
	if got := snapshotRowString(latest.Scrollback[0]); !strings.Contains(got, "3") {
		t.Fatalf("expected latest window to start near newest history, got %q", got)
	}
	if got := snapshotRowString(latest.Scrollback[1]); !strings.Contains(got, "4") {
		t.Fatalf("expected latest window to end at newest history, got %q", got)
	}

	older := term.Snapshot(2, 2)
	if len(older.Scrollback) != 2 {
		t.Fatalf("expected 2 older scrollback rows, got %d", len(older.Scrollback))
	}
	if got := snapshotRowString(older.Scrollback[0]); !strings.Contains(got, "1") {
		t.Fatalf("expected older window to include oldest history, got %q", got)
	}
	if got := snapshotRowString(older.Scrollback[1]); !strings.Contains(got, "2") {
		t.Fatalf("expected older window to include next history row, got %q", got)
	}
}

func TestTerminalDeliversTrailingOutputBeforeClosedFrame(t *testing.T) {
	ctx := context.Background()
	bus := NewEventBus(nil)

	term, err := newTerminal(ctx, bus, terminalConfig{
		ID:             "trail123",
		Name:           "cat",
		Command:        []string{"cat", "-vet"},
		Size:           Size{Cols: 80, Rows: 24},
		ScrollbackSize: 128,
		KeepAfterExit:  time.Second,
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("new terminal failed: %v", err)
	}
	defer term.Close()

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream := term.Subscribe(streamCtx)

	if err := term.WriteInput([]byte("A\t\x1bB\n\x04")); err != nil {
		t.Fatalf("write input failed: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	sawOutput := false
	for time.Now().Before(deadline) {
		msg, ok := <-stream
		if !ok {
			break
		}
		switch msg.Type {
		case StreamOutput:
			if strings.Contains(string(msg.Output), "A^I^[B$") {
				sawOutput = true
			}
		case StreamClosed:
			if !sawOutput {
				t.Fatalf("stream closed before trailing output arrived")
			}
			return
		}
	}
	if !sawOutput {
		t.Fatal("expected trailing output before close")
	}
	t.Fatal("timed out waiting for closed frame")
}

func snapshotContains(s *Snapshot, needle string) bool {
	for _, row := range s.Scrollback {
		if rowToString(row) == needle {
			return true
		}
	}
	for _, row := range s.Screen.Cells {
		if strings.Contains(rowToString(row), needle) {
			return true
		}
	}
	return false
}

func rowToString(row []Cell) string {
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(cell.Content)
	}
	return strings.TrimRight(b.String(), " ")
}
