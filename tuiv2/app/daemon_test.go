package app

import (
	"context"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	termxtestkit "github.com/lozzow/termx/termx-testkit"
)

const (
	appTestModeCollaborator = termxtestkit.ModeCollaborator
	appTestStateExited      = termxtestkit.StateExited
)

type appTestDaemon = termxtestkit.Daemon

func startAppTestDaemon(t testing.TB, ctx context.Context, socketName string) *appTestDaemon {
	t.Helper()
	return termxtestkit.StartDaemon(t, ctx, socketName)
}

func dialAppTestProtocolClient(t testing.TB, ctx context.Context, socketPath string) *protocol.Client {
	t.Helper()
	client, err := termxtestkit.DialClient(ctx, socketPath)
	if err != nil {
		t.Fatalf("dial protocol client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func waitForTerminalState(t *testing.T, ctx context.Context, daemon *appTestDaemon, terminalID string, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		state, err := daemon.TerminalState(ctx, terminalID)
		if err == nil && state == want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("context expired waiting for terminal %s state %s", terminalID, want)
		case <-time.After(100 * time.Millisecond):
		}
	}
	state, err := daemon.TerminalState(context.Background(), terminalID)
	t.Fatalf("timeout waiting for terminal %s state %s; latest state=%q err=%v", terminalID, want, state, err)
}
