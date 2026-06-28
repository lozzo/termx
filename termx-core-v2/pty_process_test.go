package termxcorev2

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPTYProcessFactoryFeedsLiveSurface(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pty integration requires a Unix-like PTY")
	}
	server := NewServer()
	events := server.Events(context.Background(), EventFilter{Types: []EventType{EventTerminalLiveInvalidated, EventTerminalResized, EventTerminalExited}})
	info, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-pty",
		Command: []string{"/bin/sh", "-c", "printf 'alpha\\n'; read line; printf \"echo:%s\\n\" \"$line\""},
		Size:    Size{Cols: 40, Rows: 8},
	})
	if err != nil {
		t.Fatalf("register pty terminal: %v", err)
	}
	if info.State != TerminalStateRunning {
		t.Fatalf("unexpected terminal state %#v", info)
	}
	waitForLiveRow(t, server, "term-pty", "alpha")
	if err := server.ResizeTerminal(context.Background(), "term-pty", 50, 10); err != nil {
		t.Fatalf("resize pty terminal: %v", err)
	}
	if err := server.WriteInput(context.Background(), "term-pty", []byte("beta\n")); err != nil {
		t.Fatalf("write pty input: %v", err)
	}
	waitForLiveRow(t, server, "term-pty", "echo:beta")
	assertEventuallyEvent(t, events, EventTerminalLiveInvalidated, "term-pty")
	assertEventuallyEvent(t, events, EventTerminalResized, "term-pty")
	assertEventuallyEvent(t, events, EventTerminalExited, "term-pty")
}

func TestPTYProcessFactoryUsesCreateDirAndEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pty integration requires a Unix-like PTY")
	}
	dir, err := os.MkdirTemp("/tmp", "termx-pty-env-")
	if err != nil {
		t.Fatalf("create pty cwd: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	server := NewServer()
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-pty-env",
		Command: []string{"/bin/sh", "-c", "printf 'cwd:%s env:%s\\n' \"$PWD\" \"$TERMX_REMOTE_TEST\""},
		Size:    Size{Cols: 120, Rows: 4},
		Options: TerminalCreateOptions{
			Dir: dir,
			Env: []string{"TERMX_REMOTE_TEST=ok"},
		},
	}); err != nil {
		t.Fatalf("register pty terminal: %v", err)
	}
	waitForTerminalState(t, server, "term-pty-env", TerminalStateExited)
	info, err := server.GetTerminal("term-pty-env")
	if err != nil {
		t.Fatalf("get pty env terminal: %v", err)
	}
	if info.CWD != dir || info.LiveCWD != dir {
		t.Fatalf("terminal info should keep requested cwd, got %#v want %q", info, dir)
	}
}

func assertEventuallyEvent(t *testing.T, events <-chan Event, typ EventType, terminalID string) Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("event channel closed")
			}
			if event.Type == typ && event.TerminalID == terminalID {
				return event
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s event", typ)
		}
	}
}

func TestPTYProcessFactoryKillExitsProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pty integration requires a Unix-like PTY")
	}
	server := NewServer()
	events := server.Events(context.Background(), EventFilter{Types: []EventType{EventTerminalExited}})
	if _, err := server.RegisterTerminal(TerminalRecord{
		ID:      "term-kill",
		Command: []string{"/bin/sh", "-c", "while true; do sleep 1; done"},
		Size:    Size{Cols: 20, Rows: 4},
	}); err != nil {
		t.Fatalf("register pty terminal: %v", err)
	}
	if err := server.KillTerminal(context.Background(), "term-kill"); err != nil {
		t.Fatalf("kill pty terminal: %v", err)
	}
	event := assertEventValue(t, events, EventTerminalExited, "term-kill")
	if event.Terminal == nil || event.Terminal.State != TerminalStateExited {
		t.Fatalf("expected exited terminal event, got %#v", event)
	}
}

func waitForLiveRow(t *testing.T, server *Server, terminalID string, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := server.LiveRows(terminalID)
		if err == nil {
			for _, row := range rows {
				if strings.Contains(row, want) {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	rows, _ := server.LiveRows(terminalID)
	t.Fatalf("timed out waiting for live row %q, got %#v", want, rows)
}
