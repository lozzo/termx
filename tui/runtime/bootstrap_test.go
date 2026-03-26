package runtime

import (
	"context"
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/core/types"
)

func TestBootstrapCreatesLiveShellPane(t *testing.T) {
	client := &stubClient{
		createResult: &protocol.CreateResult{TerminalID: "term-1", State: "running"},
	}

	model, err := Bootstrap(context.Background(), client, BootstrapConfig{
		Workspace:    "main",
		DefaultShell: "/bin/sh",
	})
	if err != nil {
		t.Fatalf("Bootstrap returned error: %v", err)
	}
	if client.createCalls != 1 {
		t.Fatalf("expected create call, got %d", client.createCalls)
	}

	pane := model.Workbench.ActivePane()
	if pane.SlotState != types.PaneSlotLive {
		t.Fatalf("expected live pane, got %q", pane.SlotState)
	}
	if pane.TerminalID != types.TerminalID("term-1") {
		t.Fatalf("expected pane bound to term-1, got %q", pane.TerminalID)
	}
	meta, ok := model.Workbench.Terminals[types.TerminalID("term-1")]
	if !ok {
		t.Fatal("expected terminal metadata to be recorded")
	}
	if meta.OwnerPaneID != pane.ID {
		t.Fatalf("expected owner pane %q, got %q", pane.ID, meta.OwnerPaneID)
	}
}
