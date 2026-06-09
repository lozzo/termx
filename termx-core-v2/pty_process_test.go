package termxcorev2

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPTYProcessFactoryFeedsLiveSurfaceAndHistory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pty integration requires a Unix-like PTY")
	}
	server := NewServer()
	events := server.Events(context.Background(), EventFilter{Types: []EventType{EventTerminalChanged, EventTerminalResized, EventTerminalExited}})
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
	window := waitForHistoryRow(t, server, "term-pty", "echo:beta")
	if window.TotalLines == 0 || window.Generation == 0 || window.Token == "" {
		t.Fatalf("expected authoritative history metadata, got %#v", window)
	}
	assertEventuallyEvent(t, events, EventTerminalChanged, "term-pty")
	assertEventuallyEvent(t, events, EventTerminalResized, "term-pty")
	assertEventuallyEvent(t, events, EventTerminalExited, "term-pty")
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

func waitForHistoryRow(t *testing.T, server *Server, terminalID string, want string) historyWindowSnapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		window, err := server.LatestWindow(terminalID, 80, 20)
		if err == nil {
			for _, row := range window.Rows {
				if strings.Contains(row.Text, want) {
					return historyWindowSnapshot{TotalLines: window.TotalLines, Generation: uint64(window.Generation), Token: string(window.Token)}
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	window, _ := server.LatestWindow(terminalID, 80, 20)
	t.Fatalf("timed out waiting for history row %q, got %#v", want, window)
	return historyWindowSnapshot{}
}

type historyWindowSnapshot struct {
	TotalLines int
	Generation uint64
	Token      string
}
