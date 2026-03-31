package app

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx"
	"github.com/lozzow/termx/protocol"
	unixtransport "github.com/lozzow/termx/transport/unix"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
)

// TestE2ECreateShellAndInteract is a full end-to-end test of the MVP flow:
//
//  1. Start a real termx server
//  2. Connect via a real bridge client
//  3. Build the tuiv2 app model, init → picker opens with items loaded
//  4. Navigate picker to "+ new terminal" and submit
//  5. Fill name prompt, submit; fill tags prompt (empty), submit
//  6. terminal is created and attached; stream goroutine starts
//  7. Wait for "e2e_ready" from the shell to appear in View()
//  8. Send "echo e2e_interaction_ok\n", wait for it to appear in View()
func TestE2ECreateShellAndInteract(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// ── 1. Start server ──────────────────────────────────────────────────────
	socketPath := filepath.Join(t.TempDir(), "termx-e2e.sock")
	srv := termx.NewServer(termx.WithSocketPath(socketPath))
	srvDone := make(chan error, 1)
	go func() { srvDone <- srv.ListenAndServe(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = srv.Shutdown(context.Background())
		select {
		case <-srvDone:
		case <-time.After(3 * time.Second):
		}
	})
	if err := e2eWaitSocket(socketPath, 5*time.Second); err != nil {
		t.Fatalf("server socket never appeared: %v", err)
	}

	// ── 2. Connect bridge client ─────────────────────────────────────────────
	tr, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	pc := protocol.NewClient(tr)
	if err := pc.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	adapted := bridge.NewProtocolClient(pc)

	// ── 3. Build model ───────────────────────────────────────────────────────
	model := New(shared.Config{}, nil, runtime.New(adapted))
	model.width = 120
	model.height = 40

	// Capture InvalidateMsg fired by the stream goroutine.
	invalidated := make(chan struct{}, 64)
	model.SetSendFunc(func(msg tea.Msg) {
		if _, ok := msg.(InvalidateMsg); ok {
			select {
			case invalidated <- struct{}{}:
			default:
			}
		}
	})

	// ── 4. Init: bootstrap workspace, picker opens, terminal list loads ──────
	e2eDrain(t, model, model.Init())

	if model.modalHost.Session == nil || model.modalHost.Session.Kind != input.ModePicker {
		t.Fatalf("expected picker open after init, got session=%#v", model.modalHost.Session)
	}
	items := model.modalHost.Picker.VisibleItems()
	if len(items) == 0 || !items[len(items)-1].CreateNew {
		t.Fatalf("expected create-new entry in picker items, got %#v", items)
	}

	// ── 5. Navigate to create-new and submit ─────────────────────────────────
	pane := model.workbench.ActivePane()
	if pane == nil {
		t.Fatal("expected active pane after bootstrap")
	}
	// Move selection to the last (create-new) item.
	for i := 0; i < len(items)-1; i++ {
		_, _ = model.Update(input.SemanticAction{Kind: input.ActionPickerDown})
	}
	// Submit → openCreateTerminalPrompt is called, mode switches to ModePrompt.
	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: pane.ID})
	e2eDrain(t, model, cmd)

	if model.modalHost.Session == nil || model.modalHost.Session.Kind != input.ModePrompt {
		t.Fatalf("expected name prompt after create-new, got session=%#v", model.modalHost.Session)
	}

	// ── 6. Name prompt: set name and command, submit ─────────────────────────
	// Override the command to produce deterministic output for the test.
	// "echo e2e_ready; exec sh" prints a marker then hands off to an interactive sh.
	paneID := model.modalHost.Prompt.PaneID
	model.modalHost.Prompt.Value = "e2e-shell"
	// Use a command that keeps its output on-screen: print the marker, then
	// wait (cat holds stdin open so the process doesn't exit immediately).
	model.modalHost.Prompt.Command = []string{"sh", "-c", "printf 'e2e_ready\\n'; cat"}
	_, cmd = model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: paneID})
	e2eDrain(t, model, cmd)

	if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "create-terminal-tags" {
		t.Fatalf("expected tags prompt after name submit, got prompt=%#v", model.modalHost.Prompt)
	}

	// ── 7. Tags prompt: empty submit → client.Create + AttachTerminal ────────
	_, cmd = model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: paneID})
	e2eDrain(t, model, cmd) // blocks until Create+Attach+Snapshot complete

	// Pane should now be bound and modal closed.
	pane = model.workbench.ActivePane()
	if pane == nil || pane.TerminalID == "" {
		t.Fatalf("expected pane bound to terminal after create+attach, pane=%#v", pane)
	}
	if model.modalHost.Session != nil {
		t.Fatalf("expected modal closed after attach, got session=%#v", model.modalHost.Session)
	}

	// ── 8. Wait for "e2e_ready" to arrive via stream ─────────────────────────
	e2eWaitForText(t, ctx, model, invalidated, "e2e_ready")

	view := model.View()
	if strings.Contains(view, "unbound pane") {
		t.Fatalf("pane still rendered as unbound:\n%s", view)
	}
	if !strings.Contains(view, "e2e_ready") {
		t.Logf("view after attach:\n%s", view)
		t.Fatal("expected e2e_ready marker in rendered view")
	}

	// ── 9. Send input, verify echo appears in view ───────────────────────────
	_, cmd = model.Update(input.TerminalInput{PaneID: pane.ID, Data: []byte("echo e2e_interaction_ok\n")})
	e2eDrain(t, model, cmd)

	e2eWaitForText(t, ctx, model, invalidated, "e2e_interaction_ok")

	if !strings.Contains(model.View(), "e2e_interaction_ok") {
		t.Logf("final view:\n%s", model.View())
		t.Fatal("expected e2e_interaction_ok in view after sending echo command")
	}
}

// e2eDrain recursively executes cmd and all downstream commands, updating the
// model with each returned message.  It handles tea.BatchMsg by processing
// each item individually.
func e2eDrain(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	switch typed := msg.(type) {
	case tea.BatchMsg:
		for _, item := range typed {
			if next := item(); next != nil {
				_, nextCmd := m.Update(next)
				e2eDrain(t, m, nextCmd)
			}
		}
	default:
		_, nextCmd := m.Update(typed)
		e2eDrain(t, m, nextCmd)
	}
}

// e2eWaitForText polls model.View() until it contains the target string or
// the test context expires.  Between polls it waits for an InvalidateMsg to
// avoid busy-looping.
func e2eWaitForText(t *testing.T, ctx context.Context, m *Model, invalidated <-chan struct{}, target string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(m.View(), target) {
			return
		}
		select {
		case <-invalidated:
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("context expired waiting for %q in view", target)
		}
	}
	t.Fatalf("timeout: %q never appeared in view\nfinal view:\n%s", target, m.View())
}

// e2eWaitSocket dials the unix socket in a loop until it succeeds or the
// deadline passes.
func e2eWaitSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", path)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("socket %s did not appear within %s", path, timeout)
}
