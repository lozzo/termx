package tui

import "testing"

func TestResizerSyncsTerminalResizeForOwnerPane(t *testing.T) {
	client := &fakeClient{}
	store := NewTerminalStore()
	terminal := store.GetOrCreate("term-1")
	coordinator := NewTerminalCoordinator(client, store)
	resizer := NewResizer(coordinator)
	pane := &Pane{ID: "pane-1", Terminal: terminal, Viewport: &Viewport{TerminalID: "term-1", Channel: 7, ResizeAcquired: true}}

	resizer.SyncPaneResize(pane, 120, 40)

	if client.resizeCalls != 1 {
		t.Fatalf("expected one resize call, got %d", client.resizeCalls)
	}
}
