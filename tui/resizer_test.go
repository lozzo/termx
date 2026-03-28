package tui

import "testing"

func TestResizerSyncsTerminalResizeThroughCoordinator(t *testing.T) {
	client := &fakeClient{}
	store := NewTerminalStore()
	coordinator := NewTerminalCoordinator(client, store)
	resizer := NewResizer(coordinator)
	pane := &Pane{Viewport: &Viewport{TerminalID: "term-1", Channel: 7, ResizeAcquired: true}}

	resizer.SyncPaneResize(pane, 120, 40)

	if client.resizeCalls != 1 {
		t.Fatalf("expected one resize call, got %d", client.resizeCalls)
	}
}
