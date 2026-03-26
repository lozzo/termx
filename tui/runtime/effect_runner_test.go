package runtime

import (
	"context"
	"testing"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui/app"
	coreterminal "github.com/lozzow/termx/tui/core/terminal"
	"github.com/lozzow/termx/tui/core/types"
)

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
