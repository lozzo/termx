package runtime

import (
	"context"
	"testing"

	"github.com/lozzow/termx/tui/app"
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
