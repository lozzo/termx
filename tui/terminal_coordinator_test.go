package tui

import (
	"context"
	"testing"

	"github.com/lozzow/termx/protocol"
)

func TestNewTerminalCoordinatorHoldsStoreAndClient(t *testing.T) {
	store := NewTerminalStore()
	client := &fakeClient{}
	coordinator := NewTerminalCoordinator(client, store)

	if coordinator == nil {
		t.Fatal("expected coordinator")
	}
	if coordinator.Store() != store {
		t.Fatal("expected coordinator to hold store reference")
	}
	if coordinator.Client() != client {
		t.Fatal("expected coordinator to hold client reference")
	}
}

func TestTerminalCoordinatorAttachLoadsSnapshotAndUpdatesStore(t *testing.T) {
	store := NewTerminalStore()
	client := &fakeClient{
		snapshotByID: map[string]*protocol.Snapshot{
			"term-1": {TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}},
		},
	}
	coordinator := NewTerminalCoordinator(client, store)
	info := protocol.TerminalInfo{ID: "term-1", Name: "worker", Command: []string{"bash"}, State: "running"}

	view, err := coordinator.AttachTerminal(context.Background(), info)
	if err != nil {
		t.Fatalf("expected attach to succeed, got %v", err)
	}
	if view == nil {
		t.Fatal("expected viewport result")
	}
	terminal := store.Get("term-1")
	if terminal == nil || terminal.Name != "worker" {
		t.Fatalf("expected store updated with terminal metadata, got %#v", terminal)
	}
}

func TestTerminalCoordinatorMarksTerminalExitedInStore(t *testing.T) {
	store := NewTerminalStore()
	terminal := store.GetOrCreate("term-1")
	terminal.State = "running"
	coordinator := NewTerminalCoordinator(&fakeClient{}, store)

	coordinator.MarkExited("term-1", 42)

	if terminal.State != "exited" {
		t.Fatalf("expected exited state, got %q", terminal.State)
	}
	if terminal.ExitCode == nil || *terminal.ExitCode != 42 {
		t.Fatalf("expected exit code 42, got %#v", terminal.ExitCode)
	}
}
