package runtime

import (
	"context"
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	coreterminal "github.com/lozzow/termx/tui/core/terminal"
	"github.com/lozzow/termx/tui/core/types"
)

func TestEffectRunnerMapsConnectToConnectedMessage(t *testing.T) {
	client := &stubClient{
		listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{{ID: "term-1", Name: "shell-1", State: "running"}}},
		snapshotByID: map[string]*protocol.Snapshot{"term-1": {TerminalID: "term-1"}},
	}
	runner := NewEffectRunner(client)

	msg := runner.Run(context.Background(), app.EffectConnectTerminal{TerminalID: types.TerminalID("term-1")})
	connected, ok := msg.(app.MessageTerminalConnected)
	if !ok {
		t.Fatalf("expected MessageTerminalConnected, got %T", msg)
	}
	if connected.Terminal.ID != types.TerminalID("term-1") || connected.Terminal.Name != "shell-1" {
		t.Fatalf("expected connected terminal metadata, got %#v", connected.Terminal)
	}
	if connected.Snapshot == nil || connected.Snapshot.TerminalID != "term-1" {
		t.Fatalf("expected connected snapshot, got %#v", connected.Snapshot)
	}
}

func TestEffectRunnerMapsDisconnectToPaneMessage(t *testing.T) {
	client := &stubClient{}
	runner := NewEffectRunner(client)

	msg := runner.Run(context.Background(), app.EffectDisconnectPane{PaneID: types.PaneID("pane-1")})
	disconnected, ok := msg.(app.MessageTerminalDisconnected)
	if !ok {
		t.Fatalf("expected MessageTerminalDisconnected, got %T", msg)
	}
	if disconnected.PaneID != types.PaneID("pane-1") {
		t.Fatalf("expected disconnected pane-1, got %q", disconnected.PaneID)
	}
}

func TestEffectRunnerMapsReconnectToConnectedMessage(t *testing.T) {
	client := &stubClient{
		listResult: &protocol.ListResult{Terminals: []protocol.TerminalInfo{{ID: "term-1", Name: "shell-1", State: "running"}}},
		snapshotByID: map[string]*protocol.Snapshot{"term-1": {TerminalID: "term-1"}},
	}
	runner := NewEffectRunner(client)

	msg := runner.Run(context.Background(), app.EffectReconnectTerminal{TerminalID: types.TerminalID("term-1")})
	connected, ok := msg.(app.MessageTerminalConnected)
	if !ok {
		t.Fatalf("expected MessageTerminalConnected, got %T", msg)
	}
	if connected.Terminal.ID != types.TerminalID("term-1") {
		t.Fatalf("expected reconnected terminal term-1, got %#v", connected.Terminal)
	}
}

func TestEffectRunnerMapsRemoveToUnconnectedPaneMessage(t *testing.T) {
	client := &stubClient{}
	runner := NewEffectRunner(client)

	msg := runner.Run(context.Background(), app.EffectRemoveTerminal{TerminalID: types.TerminalID("term-1")})
	removed, ok := msg.(app.MessageTerminalRemoved)
	if !ok {
		t.Fatalf("expected MessageTerminalRemoved, got %T", msg)
	}
	if removed.TerminalID != types.TerminalID("term-1") {
		t.Fatalf("expected removed terminal term-1, got %q", removed.TerminalID)
	}
	if len(client.removeCalls) != 1 || client.removeCalls[0] != "term-1" {
		t.Fatalf("expected remove call for term-1, got %#v", client.removeCalls)
	}
}

func TestEffectRunnerMapsKillToExitedMessage(t *testing.T) {
	client := &stubClient{}
	runner := NewEffectRunner(client)

	msg := runner.Run(context.Background(), app.EffectKillTerminal{TerminalID: types.TerminalID("term-1")})
	exited, ok := msg.(app.MessageTerminalExited)
	if !ok {
		t.Fatalf("expected MessageTerminalExited, got %T", msg)
	}
	if exited.TerminalID != types.TerminalID("term-1") {
		t.Fatalf("expected exited terminal term-1, got %q", exited.TerminalID)
	}
	if len(client.killCalls) != 1 || client.killCalls[0] != "term-1" {
		t.Fatalf("expected kill call for term-1, got %#v", client.killCalls)
	}
}

func TestEffectRunnerMapsLoadTerminalPoolToLoadedMessage(t *testing.T) {
	client := &stubClient{
		listResult: &protocol.ListResult{
			Terminals: []protocol.TerminalInfo{
				{ID: "term-visible", Name: "visible-shell", State: "running"},
				{ID: "term-exited", Name: "exited-shell", State: "exited"},
			},
		},
	}
	runner := NewEffectRunner(client)

	msg := runner.Run(context.Background(), app.EffectLoadTerminalPool{})
	loaded, ok := msg.(app.MessageTerminalPoolLoaded)
	if !ok {
		t.Fatalf("expected MessageTerminalPoolLoaded, got %T", msg)
	}
	if len(loaded.Terminals) != 2 {
		t.Fatalf("expected 2 terminals, got %d", len(loaded.Terminals))
	}
	if loaded.Terminals[0].State != coreterminal.StateRunning || loaded.Terminals[1].State != coreterminal.StateExited {
		t.Fatalf("expected state conversion, got %#v", loaded.Terminals)
	}
}
