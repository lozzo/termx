package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx"
	"github.com/lozzow/termx/protocol"
	unixtransport "github.com/lozzow/termx/transport/unix"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/persist"
	"github.com/lozzow/termx/tuiv2/render"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
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
	createIndex := pickerCreateRowIndex(items)
	if createIndex < 0 {
		t.Fatalf("expected create-new entry in picker items, got %#v", items)
	}

	// ── 5. Navigate to create-new and submit ─────────────────────────────────
	pane := model.workbench.ActivePane()
	if pane == nil {
		t.Fatal("expected active pane after bootstrap")
	}
	// Move selection to the create-new item.
	for i := 0; i < createIndex; i++ {
		_, _ = model.Update(input.SemanticAction{Kind: input.ActionPickerDown})
	}
	// Submit → openCreateTerminalPrompt is called, mode switches to ModePrompt.
	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: pane.ID})
	e2eDrain(t, model, cmd)

	if model.modalHost.Session == nil || model.modalHost.Session.Kind != input.ModePrompt {
		t.Fatalf("expected name prompt after create-new, got session=%#v", model.modalHost.Session)
	}

	// ── 6. Create form: set name and command, submit ─────────────────────────
	// Override the command to produce deterministic output for the test.
	// "echo e2e_ready; exec sh" prints a marker then hands off to an interactive sh.
	paneID := model.modalHost.Prompt.PaneID
	setCreateTerminalFormField(model.modalHost.Prompt, "name", "e2e-shell")
	// Use a command that keeps its output on-screen: print the marker, then
	// wait (cat holds stdin open so the process doesn't exit immediately).
	setCreateTerminalFormField(model.modalHost.Prompt, "command", "sh -c \"printf 'e2e_ready\\n'; cat\"")
	_, cmd = model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: paneID})
	e2eDrain(t, model, cmd)

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

	view := xansi.Strip(model.View())
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

func TestE2EInitialAttachResizesAfterFirstWindowSize(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

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

	tr, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	pc := protocol.NewClient(tr)
	if err := pc.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	created, err := pc.Create(ctx, protocol.CreateParams{
		Command: []string{"sh", "-c", "printf 'ready\\n'; while IFS= read -r line; do if [ \"$line\" = size ]; then stty size; printf 'window_size_ok\\n'; fi; done"},
		Name:    "delayed-window-shell",
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("create interactive shell: %v", err)
	}

	model := New(shared.Config{AttachID: created.TerminalID}, nil, runtime.New(bridge.NewProtocolClient(pc)))

	invalidated := make(chan struct{}, 128)
	model.SetSendFunc(func(msg tea.Msg) {
		if _, ok := msg.(InvalidateMsg); ok {
			select {
			case invalidated <- struct{}{}:
			default:
			}
		}
		_, cmd := model.Update(msg)
		e2eDrain(t, model, cmd)
	})

	e2eDrain(t, model, model.Init())
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(e2eActiveSnapshotExcerpt(model), "ready") {
			break
		}
		select {
		case <-invalidated:
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("context expired waiting for initial snapshot text")
		}
	}
	if !strings.Contains(e2eActiveSnapshotExcerpt(model), "ready") {
		t.Fatalf("timeout waiting for initial snapshot text, got:\n%s", e2eActiveSnapshotExcerpt(model))
	}

	if terminal := model.runtime.Registry().Get(created.TerminalID); terminal == nil || terminal.Snapshot == nil || terminal.Snapshot.Size.Cols != 80 || terminal.Snapshot.Size.Rows != 24 {
		t.Fatalf("expected pre-window attach to keep default 80x24 snapshot, got %#v", terminal)
	}

	_, cmd := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	e2eDrain(t, model, cmd)

	pane := model.workbench.ActivePane()
	if pane == nil {
		t.Fatal("expected active pane after attach")
	}
	visible := model.workbench.VisibleWithSize(model.bodyRect())
	if visible == nil || visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) || len(visible.Tabs[visible.ActiveTab].Panes) == 0 {
		t.Fatalf("expected visible pane after resize, got %#v", visible)
	}
	wantRect, ok := paneContentRectForVisible(visible.Tabs[visible.ActiveTab].Panes[0])
	if !ok {
		t.Fatal("expected visible pane content rect after resize")
	}
	_, cmd = model.Update(input.TerminalInput{
		PaneID: pane.ID,
		Data:   []byte("size\n"),
	})
	e2eDrain(t, model, cmd)

	e2eWaitForText(t, ctx, model, invalidated, fmt.Sprintf("%d %d", wantRect.H, wantRect.W))
	e2eWaitForText(t, ctx, model, invalidated, "window_size_ok")

	terminal := model.runtime.Registry().Get(created.TerminalID)
	if terminal == nil || terminal.Snapshot == nil {
		t.Fatalf("expected terminal snapshot after resize, got %#v", terminal)
	}
	if terminal.Snapshot.Size.Cols != uint16(wantRect.W) || terminal.Snapshot.Size.Rows != uint16(wantRect.H) {
		t.Fatalf("expected resized snapshot %dx%d, got %#v", wantRect.W, wantRect.H, terminal.Snapshot.Size)
	}
}

func TestE2EExitedPaneRestartReusesSameTerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

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

	tr, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	pc := protocol.NewClient(tr)
	if err := pc.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	flagPath := filepath.Join(t.TempDir(), "restart-flag")
	command := fmt.Sprintf("if [ -f %q ]; then printf 'restart_pass_2\\n'; cat; else touch %q; printf 'restart_pass_1\\n'; sleep 1; exit 0; fi", flagPath, flagPath)
	created, err := pc.Create(ctx, protocol.CreateParams{
		Command: []string{"bash", "-lc", command},
		Name:    "restart-e2e",
		Size:    protocol.Size{Cols: 120, Rows: 40},
	})
	if err != nil {
		t.Fatalf("create restart test terminal: %v", err)
	}

	model := New(shared.Config{AttachID: created.TerminalID}, nil, runtime.New(bridge.NewProtocolClient(pc)))
	model.width = 120
	model.height = 40

	invalidated := make(chan struct{}, 128)
	model.SetSendFunc(func(msg tea.Msg) {
		if _, ok := msg.(InvalidateMsg); ok {
			select {
			case invalidated <- struct{}{}:
			default:
			}
		}
	})

	e2eDrain(t, model, model.Init())
	e2eWaitForRuntimeTerminalState(t, ctx, model, created.TerminalID, "exited")

	e2eDispatchKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})

	e2eWaitForRuntimeTerminalState(t, ctx, model, created.TerminalID, "running")
	e2eWaitForText(t, ctx, model, invalidated, "restart_pass_2")
	snapshot, err := pc.Snapshot(ctx, created.TerminalID, 0, 100)
	if err != nil {
		t.Fatalf("snapshot after restart: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected restart snapshot")
	}
	if !e2eSnapshotContains(snapshot, "restart_pass_1") {
		t.Fatalf("expected snapshot scrollback to preserve first pass output, got %#v", snapshot)
	}

	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != created.TerminalID {
		t.Fatalf("expected restarted pane to stay bound to %q, got %#v", created.TerminalID, pane)
	}

	_, cmd := model.Update(input.TerminalInput{PaneID: pane.ID, Data: []byte("restart_e2e_ok\n")})
	e2eDrainSkippingPrefixTimeout(t, model, cmd)
	e2eWaitForText(t, ctx, model, invalidated, "restart_e2e_ok")
}

func e2eSnapshotContains(snapshot *protocol.Snapshot, needle string) bool {
	if snapshot == nil {
		return false
	}
	for _, row := range snapshot.Scrollback {
		if strings.Contains(e2eSnapshotRowString(row), needle) {
			return true
		}
	}
	for _, row := range snapshot.Screen.Cells {
		if strings.Contains(e2eSnapshotRowString(row), needle) {
			return true
		}
	}
	return false
}

func e2eSnapshotRowString(row []protocol.Cell) string {
	var b strings.Builder
	for _, cell := range row {
		b.WriteString(cell.Content)
	}
	return strings.TrimRight(b.String(), " ")
}

func TestE2ECreateAfterExitedPaneDoesNotDropBufferedInput(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

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

	tr, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	pc := protocol.NewClient(tr)
	if err := pc.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	first, err := pc.Create(ctx, protocol.CreateParams{
		Command: []string{"bash", "-lc", "sleep 1; exit 0"},
		Name:    "first-exit",
		Size:    protocol.Size{Cols: 120, Rows: 40},
	})
	if err != nil {
		t.Fatalf("create first terminal: %v", err)
	}

	delayedClient := &delayedAttachBridgeClient{Client: bridge.NewProtocolClient(pc)}
	model := New(shared.Config{AttachID: first.TerminalID}, nil, runtime.New(delayedClient))
	model.width = 120
	model.height = 40

	invalidated := make(chan struct{}, 128)
	model.SetSendFunc(func(msg tea.Msg) {
		if _, ok := msg.(InvalidateMsg); ok {
			select {
			case invalidated <- struct{}{}:
			default:
			}
		}
	})

	e2eDrain(t, model, model.Init())
	e2eWaitForRuntimeTerminalState(t, ctx, model, first.TerminalID, "exited")

	e2eDispatchKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlF})
	if model.modalHost == nil || model.modalHost.Session == nil || model.modalHost.Session.Kind != input.ModePicker {
		t.Fatalf("expected picker after Ctrl-F on exited pane, got %#v", model.modalHost)
	}

	pane := model.workbench.ActivePane()
	if pane == nil {
		t.Fatal("expected active pane before create")
	}
	createIndex := pickerCreateRowIndex(model.modalHost.Picker.VisibleItems())
	for i := 0; i < createIndex; i++ {
		e2eDispatchKey(t, model, tea.KeyMsg{Type: tea.KeyDown})
	}
	e2eDispatchKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.modalHost == nil || model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "create-terminal-form" {
		t.Fatalf("expected create terminal prompt, got %#v", model.modalHost)
	}

	setCreateTerminalFormField(model.modalHost.Prompt, "name", "buffered-create")
	setCreateTerminalFormField(model.modalHost.Prompt, "command", "sh -c \"printf 'buffered_ready\\n'; IFS= read -r line; printf '%s\\n' \\\"$line\\\"; cat\"")

	release := make(chan struct{})
	attachStarted := make(chan struct{}, 1)
	delayedClient.DelayNextAttach(release, attachStarted)
	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: pane.ID})
	if cmd == nil {
		t.Fatal("expected create-terminal submit command")
	}
	done := make(chan tea.Msg, 1)
	go func() {
		done <- cmd()
	}()

	select {
	case <-attachStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for delayed attach to start")
	}

	for _, ch := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'A'}},
		{Type: tea.KeyRunes, Runes: []rune{'B'}},
		{Type: tea.KeyRunes, Runes: []rune{'C'}},
		{Type: tea.KeyRunes, Runes: []rune{'D'}},
		{Type: tea.KeyEnter},
	} {
		_, keyCmd := model.Update(ch)
		if keyCmd != nil {
			e2eDrainSkippingPrefixTimeout(t, model, keyCmd)
		}
	}

	close(release)
	select {
	case msg := <-done:
		e2eDrainMsg(t, model, msg, true)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for delayed create+attach to complete")
	}

	e2eWaitForText(t, ctx, model, invalidated, "buffered_ready")
	e2eWaitForText(t, ctx, model, invalidated, "ABCD")
}

func TestE2EAttachAfterExitedPaneKeepsFastEchoWhenChannelIsReused(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

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

	tr, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	pc := protocol.NewClient(tr)
	if err := pc.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	first, err := pc.Create(ctx, protocol.CreateParams{
		Command: []string{"bash", "-lc", "printf 'first_exit\\n'; exit 0"},
		Name:    "first-exit",
		Size:    protocol.Size{Cols: 120, Rows: 40},
	})
	if err != nil {
		t.Fatalf("create first terminal: %v", err)
	}

	model := New(shared.Config{AttachID: first.TerminalID}, nil, runtime.New(bridge.NewProtocolClient(pc)))
	model.width = 120
	model.height = 40

	invalidated := make(chan struct{}, 128)
	model.SetSendFunc(func(msg tea.Msg) {
		if _, ok := msg.(InvalidateMsg); ok {
			select {
			case invalidated <- struct{}{}:
			default:
			}
		}
	})

	e2eDrain(t, model, model.Init())
	e2eWaitForRuntimeTerminalState(t, ctx, model, first.TerminalID, "exited")

	firstRuntime := model.runtime.Registry().Get(first.TerminalID)
	if firstRuntime == nil || firstRuntime.Channel == 0 {
		t.Fatalf("expected exited runtime with channel, got %#v", firstRuntime)
	}

	second, err := pc.Create(ctx, protocol.CreateParams{
		Command: []string{"bash", "-lc", "cat"},
		Name:    "second-shell",
		Size:    protocol.Size{Cols: 120, Rows: 40},
	})
	if err != nil {
		t.Fatalf("create second terminal: %v", err)
	}

	pane := model.workbench.ActivePane()
	if pane == nil {
		t.Fatal("expected active pane before reattach")
	}
	e2eDrainSkippingPrefixTimeout(t, model, model.attachPaneTerminalCmd("", pane.ID, second.TerminalID))
	e2eWaitForRuntimeTerminalState(t, ctx, model, second.TerminalID, "running")

	secondRuntime := model.runtime.Registry().Get(second.TerminalID)
	if secondRuntime == nil {
		t.Fatalf("expected second runtime after attach")
	}
	if secondRuntime.Channel != firstRuntime.Channel {
		t.Fatalf("expected channel reuse for regression test, got first=%d second=%d", firstRuntime.Channel, secondRuntime.Channel)
	}

	for _, chunk := range []string{"A", "B", "C", "D", "\n"} {
		_, cmd := model.Update(input.TerminalInput{PaneID: pane.ID, Data: []byte(chunk)})
		e2eDrainSkippingPrefixTimeout(t, model, cmd)
	}

	e2eWaitForText(t, ctx, model, invalidated, "ABCD")
}

func TestE2EAttachAfterExitedPaneRestoresCursorHighlight(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

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

	tr, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	pc := protocol.NewClient(tr)
	if err := pc.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	first, err := pc.Create(ctx, protocol.CreateParams{
		Command: []string{"bash", "-lc", "exit 0"},
		Name:    "first-exit",
		Size:    protocol.Size{Cols: 120, Rows: 40},
	})
	if err != nil {
		t.Fatalf("create first terminal: %v", err)
	}

	model := New(shared.Config{AttachID: first.TerminalID}, nil, runtime.New(bridge.NewProtocolClient(pc)))
	model.width = 120
	model.height = 40

	invalidated := make(chan struct{}, 128)
	model.SetSendFunc(func(msg tea.Msg) {
		if _, ok := msg.(InvalidateMsg); ok {
			select {
			case invalidated <- struct{}{}:
			default:
			}
		}
	})

	e2eDrain(t, model, model.Init())
	e2eWaitForRuntimeTerminalState(t, ctx, model, first.TerminalID, "exited")

	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	tab.ScrollOffset = 4

	second, err := pc.Create(ctx, protocol.CreateParams{
		Command: []string{"sh", "-c", "printf '\\033[8;20H@\\033[8;20H'; cat"},
		Name:    "cursor-restore",
		Size:    protocol.Size{Cols: 120, Rows: 40},
	})
	if err != nil {
		t.Fatalf("create second terminal: %v", err)
	}

	pane := model.workbench.ActivePane()
	if pane == nil {
		t.Fatal("expected active pane")
	}
	e2eDrainSkippingPrefixTimeout(t, model, model.attachPaneTerminalCmd("", pane.ID, second.TerminalID))
	e2eWaitForRuntimeTerminalState(t, ctx, model, second.TerminalID, "running")
	if got := model.workbench.CurrentTab().ScrollOffset; got != 0 {
		t.Fatalf("expected attach to reset scroll offset before cursor projection, got %d", got)
	}
	e2eWaitForCursorHighlight(t, ctx, model, invalidated)
}

func TestE2EInteractiveShellPromptTitleSurvivesRepeatedLS(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

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

	tr, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	pc := protocol.NewClient(tr)
	if err := pc.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	created, err := pc.Create(ctx, protocol.CreateParams{
		Command: []string{"bash", "--noprofile", "--norc", "-i"},
		Name:    "prompt-title-shell",
		Env: []string{
			"PS1=termx$ ",
			`PROMPT_COMMAND=printf '\033]2;termx-prompt\007'`,
		},
		Size: protocol.Size{Cols: 120, Rows: 40},
	})
	if err != nil {
		t.Fatalf("create interactive shell: %v", err)
	}

	model := New(shared.Config{AttachID: created.TerminalID}, nil, runtime.New(bridge.NewProtocolClient(pc)))
	model.width = 120
	model.height = 40

	invalidated := make(chan struct{}, 128)
	model.SetSendFunc(func(msg tea.Msg) {
		if _, ok := msg.(InvalidateMsg); ok {
			select {
			case invalidated <- struct{}{}:
			default:
			}
		}
		_, cmd := model.Update(msg)
		e2eDrain(t, model, cmd)
	})

	e2eDrain(t, model, model.Init())

	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != created.TerminalID {
		t.Fatalf("expected active pane attached to %q, got %#v", created.TerminalID, pane)
	}

	e2eWaitForText(t, ctx, model, invalidated, "termx$")
	e2eWaitForTitle(t, ctx, model, invalidated, created.TerminalID, "termx-prompt")

	_, cmd := model.Update(input.TerminalInput{
		PaneID: pane.ID,
		Data:   []byte("ls >/dev/null\nls >/dev/null\nprintf 'ls_prompt_ok\\n'\n"),
	})
	e2eDrain(t, model, cmd)

	e2eWaitForText(t, ctx, model, invalidated, "ls_prompt_ok")
	e2eWaitForTitle(t, ctx, model, invalidated, created.TerminalID, "termx-prompt")
}

func TestE2EKeyEnterExecutesCommandInAttachedShell(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

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

	tr, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	pc := protocol.NewClient(tr)
	if err := pc.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	created, err := pc.Create(ctx, protocol.CreateParams{
		Command: []string{"bash", "--noprofile", "--norc", "-i"},
		Name:    "enter-shell",
		Env: []string{
			"PS1=termx$ ",
			// Reuse the same prompt/title contract as the repeated-LS shell E2E so
			// this test isolates Enter-key behavior rather than shell init variance.
			`PROMPT_COMMAND=printf '\033]2;termx-prompt\007'`,
		},
		Size: protocol.Size{Cols: 120, Rows: 40},
	})
	if err != nil {
		t.Fatalf("create interactive shell: %v", err)
	}

	model := New(shared.Config{AttachID: created.TerminalID}, nil, runtime.New(bridge.NewProtocolClient(pc)))
	model.width = 120
	model.height = 40

	invalidated := make(chan struct{}, 128)
	model.SetSendFunc(func(msg tea.Msg) {
		if _, ok := msg.(InvalidateMsg); ok {
			select {
			case invalidated <- struct{}{}:
			default:
			}
		}
		_, cmd := model.Update(msg)
		e2eDrain(t, model, cmd)
	})

	e2eDrain(t, model, model.Init())

	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != created.TerminalID {
		t.Fatalf("expected active pane attached to %q, got %#v", created.TerminalID, pane)
	}

	e2eTypeText(t, model, "ls >/dev/null; printf 'key_enter_ls_ok\\n'")
	e2eDispatchKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	e2eWaitForText(t, ctx, model, invalidated, "key_enter_ls_ok")
	e2eWaitForTitle(t, ctx, model, invalidated, created.TerminalID, "termx-prompt")
}

func TestE2EZshPromptWithEmojiVariationKeepsSingleRightBorder(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skipf("e2e: zsh not available: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

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

	tr, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	pc := protocol.NewClient(tr)
	if err := pc.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	created, err := pc.Create(ctx, protocol.CreateParams{
		Command: []string{"zsh", "-f", "-i"},
		Name:    "emoji-zsh",
		Env: []string{
			"TERM=xterm-256color",
			"PROMPT_EOL_MARK=",
			"RPROMPT=",
		},
		Size: protocol.Size{Cols: 96, Rows: 28},
	})
	if err != nil {
		t.Fatalf("create interactive zsh: %v", err)
	}

	model := New(shared.Config{AttachID: created.TerminalID}, nil, runtime.New(bridge.NewProtocolClient(pc)))
	model.width = 96
	model.height = 30

	invalidated := make(chan struct{}, 256)
	model.SetSendFunc(func(msg tea.Msg) {
		if _, ok := msg.(InvalidateMsg); ok {
			select {
			case invalidated <- struct{}{}:
			default:
			}
		}
		_, cmd := model.Update(msg)
		e2eDrain(t, model, cmd)
	})

	e2eDrain(t, model, model.Init())

	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != created.TerminalID {
		t.Fatalf("expected active pane attached to %q, got %#v", created.TerminalID, pane)
	}

	_, cmd := model.Update(input.TerminalInput{
		PaneID: pane.ID,
		Data: []byte(
			"PROMPT=$'# lozzow@RedmiBook♻️: ~/Documents/workdir/termx <> '\n" +
				"RPROMPT='[1d11220]'\n" +
				"PROMPT_EOL_MARK=''\n" +
				"clear\n",
		),
	})
	e2eDrain(t, model, cmd)

	model.hostEmojiProbePending = true
	_, cmd = model.Update(hostCursorPositionMsg{X: 1, Y: 0})
	e2eDrain(t, model, cmd)

	e2eWaitForText(t, ctx, model, invalidated, "RedmiBook♻ ")
	e2eWaitForText(t, ctx, model, invalidated, "[1d11220]")

	payload := "ζ ♻️:♻️:♻️:♻️:♻️:♻️:♻️:♻️:♻️:♻️:♻️:♻️:♻️:♻️:♻️:"
	_, cmd = model.Update(input.TerminalInput{PaneID: pane.ID, Data: []byte(payload)})
	e2eDrain(t, model, cmd)

	e2eWaitForText(t, ctx, model, invalidated, "ζ")

	view := xansi.Strip(model.View())
	if regexp.MustCompile(`♻️\x1b\[[0-9]+G`).MatchString(model.View()) {
		t.Fatalf("expected rendered frame to avoid mid-line CHA after ambiguous emoji, got:\n%q", model.View())
	}
	if !strings.Contains(view, "RedmiBook♻ ") {
		t.Fatalf("expected rendered frame to use the stable fallback for ambiguous emoji, got:\n%q", model.View())
	}
	lines := strings.Split(view, "\n")
	if len(lines) != model.height {
		t.Fatalf("expected %d frame rows, got %d:\n%s", model.height, len(lines), view)
	}

	bodyLines := lines[1 : len(lines)-1]
	foundPromptOrInput := false
	for i, line := range bodyLines {
		if got := xansi.StringWidth(line); got != model.width {
			t.Fatalf("expected body row %d to keep width %d, got %d: %q", i, model.width, got, line)
		}
		if !strings.Contains(line, "RedmiBook") && !strings.Contains(line, "ζ") && !strings.Contains(line, "[1d11220]") {
			continue
		}
		foundPromptOrInput = true
		if count := strings.Count(line, "│"); count != 2 {
			t.Fatalf("expected prompt/input row %d to contain a single left/right border pair, got %d in %q\nsnapshot excerpt:\n%s", i, count, line, e2eActiveSnapshotExcerpt(model))
		}
	}
	if !foundPromptOrInput {
		t.Fatalf("expected prompt or typed input in rendered body:\n%s\nsnapshot excerpt:\n%s", view, e2eActiveSnapshotExcerpt(model))
	}
}

func TestE2EDetachAndReattachThroughPaneMode(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

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

	model := New(shared.Config{}, nil, runtime.New(adapted))
	model.width = 120
	model.height = 40

	invalidated := make(chan struct{}, 64)
	model.SetSendFunc(func(msg tea.Msg) {
		if _, ok := msg.(InvalidateMsg); ok {
			select {
			case invalidated <- struct{}{}:
			default:
			}
		}
	})

	e2eDrain(t, model, model.Init())
	items := model.modalHost.Picker.VisibleItems()
	for i := 0; i < pickerCreateRowIndex(items); i++ {
		_, _ = model.Update(input.SemanticAction{Kind: input.ActionPickerDown})
	}
	pane := model.workbench.ActivePane()
	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: pane.ID})
	e2eDrain(t, model, cmd)
	paneID := model.modalHost.Prompt.PaneID
	setCreateTerminalFormField(model.modalHost.Prompt, "name", "e2e-shell")
	setCreateTerminalFormField(model.modalHost.Prompt, "command", "sh -c \"printf 'e2e_ready\\n'; tail -f /dev/null\"")
	_, cmd = model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: paneID})
	e2eDrain(t, model, cmd)
	_, cmd = model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: paneID})
	e2eDrain(t, model, cmd)

	pane = model.workbench.ActivePane()
	if pane == nil || pane.TerminalID == "" {
		t.Fatalf("expected pane bound after create+attach, got %#v", pane)
	}
	terminalID := pane.TerminalID
	e2eWaitForText(t, ctx, model, invalidated, "e2e_ready")

	e2eDispatchKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlP})
	e2eDispatchKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})

	pane = model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != "" {
		t.Fatalf("expected pane detached after pane-mode detach, got %#v", pane)
	}

	e2eDispatchKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlF})
	if model.modalHost.Picker == nil || len(model.modalHost.Picker.VisibleItems()) == 0 {
		t.Fatalf("expected picker with attachable terminals after detach, got %#v", model.modalHost.Picker)
	}
	items = model.modalHost.Picker.VisibleItems()
	targetIndex := -1
	for index, item := range items {
		if item.TerminalID == terminalID {
			targetIndex = index
			break
		}
	}
	if targetIndex < 0 {
		listed, listErr := model.runtime.ListTerminals(context.Background())
		t.Fatalf(
			"expected detached terminal %q to remain attachable, got %#v (session=%#v mode=%q registry=%s listErr=%v listed=%s)",
			terminalID,
			items,
			model.modalHost.Session,
			model.input.Mode().Kind,
			e2eRegistrySummary(model.runtime),
			listErr,
			e2eTerminalInfoSummary(listed),
		)
	}
	for model.modalHost.Picker.Selected != targetIndex {
		e2eDispatchKey(t, model, tea.KeyMsg{Type: tea.KeyDown})
	}
	e2eDispatchKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	pane = model.workbench.ActivePane()
	if pane == nil || pane.TerminalID != terminalID {
		t.Fatalf("expected pane reattached to %q, got %#v", terminalID, pane)
	}
	e2eWaitForText(t, ctx, model, invalidated, "e2e_ready")
}

func TestE2EClosePaneLeavesTerminalAttachable(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

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

	model := New(shared.Config{}, nil, runtime.New(adapted))
	model.width = 120
	model.height = 40

	invalidated := make(chan struct{}, 64)
	model.SetSendFunc(func(msg tea.Msg) {
		if _, ok := msg.(InvalidateMsg); ok {
			select {
			case invalidated <- struct{}{}:
			default:
			}
		}
	})

	e2eDrain(t, model, model.Init())
	items := model.modalHost.Picker.VisibleItems()
	for i := 0; i < pickerCreateRowIndex(items); i++ {
		_, _ = model.Update(input.SemanticAction{Kind: input.ActionPickerDown})
	}
	pane := model.workbench.ActivePane()
	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: pane.ID})
	e2eDrain(t, model, cmd)
	paneID := model.modalHost.Prompt.PaneID
	setCreateTerminalFormField(model.modalHost.Prompt, "name", "close-pane-shell")
	setCreateTerminalFormField(model.modalHost.Prompt, "command", "sh -c \"printf 'close_pane_ready\\n'; tail -f /dev/null\"")
	_, cmd = model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: paneID})
	e2eDrain(t, model, cmd)

	pane = model.workbench.ActivePane()
	if pane == nil || pane.TerminalID == "" {
		t.Fatalf("expected pane bound after create+attach, got %#v", pane)
	}
	terminalID := pane.TerminalID
	e2eWaitForText(t, ctx, model, invalidated, "close_pane_ready")

	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := model.workbench.SplitPane(tab.ID, pane.ID, "pane-2", workbench.SplitVertical); err != nil {
		t.Fatalf("split pane: %v", err)
	}
	if err := model.workbench.FocusPane(tab.ID, pane.ID); err != nil {
		t.Fatalf("focus pane: %v", err)
	}

	e2eDispatchKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlP})
	e2eDispatchKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})

	tab = model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab after close")
	}
	if _, exists := tab.Panes[pane.ID]; exists {
		t.Fatalf("expected pane %q to be removed after close, panes=%#v", pane.ID, tab.Panes)
	}
	active := model.workbench.ActivePane()
	if active == nil || active.ID != "pane-2" {
		t.Fatalf("expected pane-2 active after close, got %#v", active)
	}

	e2eDispatchKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlF})
	if model.modalHost.Session == nil || model.modalHost.Session.Kind != input.ModePicker {
		t.Fatalf("expected picker after close-pane flow, got %#v", model.modalHost.Session)
	}
	items = model.modalHost.Picker.VisibleItems()
	targetIndex := -1
	for index, item := range items {
		if item.TerminalID == terminalID {
			targetIndex = index
			break
		}
	}
	if targetIndex < 0 {
		listed, listErr := model.runtime.ListTerminals(context.Background())
		t.Fatalf(
			"expected terminal %q from closed pane to remain attachable, got %#v (registry=%s listErr=%v listed=%s)",
			terminalID,
			items,
			e2eRegistrySummary(model.runtime),
			listErr,
			e2eTerminalInfoSummary(listed),
		)
	}
}

func TestE2ERestoreEmptyWorkspaceRecoversViaPicker(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	env := newRealRestoreE2EEnv(t, persist.WorkspaceStateFileV2{
		Version: 2,
		Data: []persist.WorkspaceEntryV2{{
			Name:      "main",
			ActiveTab: -1,
			Tabs:      []persist.TabEntryV2{},
		}},
	})
	model := env.model

	view := xansi.Strip(model.View())
	for _, want := range []string{"No tabs in this workspace", "Ctrl-F open terminal picker"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected empty workspace recovery state %q:\n%s", want, view)
		}
	}
	if ws := model.workbench.CurrentWorkspace(); ws == nil || len(ws.Tabs) != 0 {
		t.Fatalf("expected restored workspace with 0 tabs, got %#v", ws)
	}

	e2eDispatchKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlF})

	if model.modalHost == nil || model.modalHost.Session == nil || model.modalHost.Session.Kind != input.ModePicker {
		t.Fatalf("expected picker after Ctrl-F from empty workspace, got %#v", model.modalHost)
	}
	ws := model.workbench.CurrentWorkspace()
	if ws == nil || len(ws.Tabs) != 1 {
		t.Fatalf("expected Ctrl-F recovery to seed one tab, got %#v", ws)
	}

	items := model.modalHost.Picker.VisibleItems()
	for i := 0; i < pickerCreateRowIndex(items); i++ {
		_, _ = model.Update(input.SemanticAction{Kind: input.ActionPickerDown})
	}
	pane := model.workbench.ActivePane()
	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: pane.ID})
	e2eDrain(t, model, cmd)
	paneID := model.modalHost.Prompt.PaneID
	setCreateTerminalFormField(model.modalHost.Prompt, "name", "restore-empty-workspace")
	setCreateTerminalFormField(model.modalHost.Prompt, "command", "sh -c \"printf 'restore_empty_workspace_ready\\n'; cat\"")
	_, cmd = model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: paneID})
	e2eDrain(t, model, cmd)

	pane = model.workbench.ActivePane()
	if pane == nil || pane.TerminalID == "" {
		t.Fatalf("expected attached pane after empty-workspace recovery, got %#v", pane)
	}
	e2eWaitForText(t, env.ctx, model, env.invalidated, "restore_empty_workspace_ready")
}

func TestE2ERestoreEmptyTabRecoversViaPicker(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	env := newRealRestoreE2EEnv(t, persist.WorkspaceStateFileV2{
		Version: 2,
		Data: []persist.WorkspaceEntryV2{{
			Name:      "main",
			ActiveTab: 0,
			Tabs: []persist.TabEntryV2{{
				Name:  "blank",
				Panes: []persist.PaneEntryV2{},
			}},
		}},
	})
	model := env.model

	view := xansi.Strip(model.View())
	for _, want := range []string{"blank", "No panes in this tab", "Ctrl-F create the first pane via terminal picker"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected empty tab recovery state %q:\n%s", want, view)
		}
	}
	tab := model.workbench.CurrentTab()
	if tab == nil || len(tab.Panes) != 0 {
		t.Fatalf("expected restored empty tab before recovery, got %#v", tab)
	}

	e2eDispatchKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlF})

	if model.modalHost == nil || model.modalHost.Session == nil || model.modalHost.Session.Kind != input.ModePicker {
		t.Fatalf("expected picker after Ctrl-F from empty tab, got %#v", model.modalHost)
	}
	tab = model.workbench.CurrentTab()
	if tab == nil || len(tab.Panes) != 1 {
		t.Fatalf("expected Ctrl-F recovery to seed first pane, got %#v", tab)
	}
	if tab.Name != "blank" {
		t.Fatalf("expected recovery to preserve tab name, got %#v", tab)
	}

	items := model.modalHost.Picker.VisibleItems()
	for i := 0; i < pickerCreateRowIndex(items); i++ {
		_, _ = model.Update(input.SemanticAction{Kind: input.ActionPickerDown})
	}
	pane := model.workbench.ActivePane()
	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: pane.ID})
	e2eDrain(t, model, cmd)
	paneID := model.modalHost.Prompt.PaneID
	setCreateTerminalFormField(model.modalHost.Prompt, "name", "restore-empty-tab")
	setCreateTerminalFormField(model.modalHost.Prompt, "command", "sh -c \"printf 'restore_empty_tab_ready\\n'; cat\"")
	_, cmd = model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: paneID})
	e2eDrain(t, model, cmd)

	pane = model.workbench.ActivePane()
	if pane == nil || pane.TerminalID == "" {
		t.Fatalf("expected attached pane after empty-tab recovery, got %#v", pane)
	}
	e2eWaitForText(t, env.ctx, model, env.invalidated, "restore_empty_tab_ready")
}

func TestE2EMouseTopChromeOmitsManagementActions(t *testing.T) {
	model := setupModel(t, modelOpts{width: 1000})
	for _, kind := range []render.HitRegionKind{
		render.HitRegionTabRename,
		render.HitRegionTabKill,
		render.HitRegionWorkspacePrev,
		render.HitRegionWorkspaceNext,
		render.HitRegionWorkspaceCreate,
		render.HitRegionWorkspaceRename,
		render.HitRegionWorkspaceDelete,
	} {
		for _, region := range render.TabBarHitRegions(model.visibleRenderState()) {
			if region.Kind == kind {
				t.Fatalf("expected management region %q to be omitted, got %#v", kind, region)
			}
		}
	}
}

func TestE2EMouseWorkspacePickerFooterNextSwitchesWorkspace(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	env := newRealMouseE2EEnv(t)
	model := env.model
	model.width = 320
	e2eDismissActiveOverlayIfAny(t, model)

	model.workbench.AddWorkspace("dev", &workbench.WorkspaceState{
		Name:      "dev",
		ActiveTab: 0,
		Tabs: []*workbench.TabState{{
			ID:           "tab-dev",
			Name:         "dev tab",
			ActivePaneID: "pane-dev",
			Panes: map[string]*workbench.PaneState{
				"pane-dev": {ID: "pane-dev"},
			},
			Root: workbench.NewLeaf("pane-dev"),
		}},
	})
	if !model.workbench.SwitchWorkspace("main") {
		t.Fatal("switch workspace to main")
	}

	label := e2eTabBarRegionByKind(t, model, render.HitRegionWorkspaceLabel)
	e2eMouseClickAt(t, model, label.Rect.X, label.Rect.Y)

	if model.modalHost == nil || model.modalHost.Session == nil || model.modalHost.Session.Kind != input.ModeWorkspacePicker {
		t.Fatalf("expected workspace picker after workspace label click, got %#v", model.modalHost)
	}

	next := e2eOverlayWorkspaceItemRegion(t, model, 1)
	e2eMouseClickAt(t, model, next.Rect.X, e2eScreenYForBodyY(model, next.Rect.Y))

	if model.input.Mode().Kind != input.ModeNormal {
		t.Fatalf("expected workspace picker footer click to close picker, got mode %q", model.input.Mode().Kind)
	}
	if ws := model.workbench.CurrentWorkspace(); ws == nil || ws.Name != "dev" {
		t.Fatalf("expected workspace footer next to switch to dev, got %#v", ws)
	}
}

func TestE2EMouseCreateTerminalFromPickerAndPromptFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	env := newRealMouseE2EEnv(t)
	model := env.model

	if model.modalHost == nil || model.modalHost.Picker == nil {
		t.Fatalf("expected picker after init, got %#v", model.modalHost)
	}
	items := model.modalHost.Picker.VisibleItems()
	targetIndex := pickerCreateRowIndex(items)
	if targetIndex < 0 {
		t.Fatalf("expected create-new picker row, got %#v", items)
	}

	target := overlayPickerItemRegion(t, model, targetIndex)
	e2eMouseClickAt(t, model, target.Rect.X, e2eScreenYForBodyY(model, target.Rect.Y))

	if model.modalHost == nil || model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "create-terminal-form" {
		t.Fatalf("expected create-terminal-form prompt after mouse picker click, got %#v", model.modalHost)
	}

	inputRegion := e2eOverlayRegionByKind(t, model, render.HitRegionPromptInput)
	e2eMouseClickAt(t, model, inputRegion.Rect.X, e2eScreenYForBodyY(model, inputRegion.Rect.Y))
	e2eTypeText(t, model, "mouse-e2e-shell")
	if movePromptFormField(model.modalHost.Prompt, 1) {
		model.render.Invalidate()
	}
	e2eTypeText(t, model, "sh -c \"printf 'mouse_e2e_ready\\n'; cat\"")

	submit := e2eOverlayRegionByKind(t, model, render.HitRegionPromptSubmit)
	e2eMouseClickAt(t, model, submit.Rect.X, e2eScreenYForBodyY(model, submit.Rect.Y))

	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID == "" {
		t.Fatalf("expected pane attached after mouse create flow, got %#v", pane)
	}
	if model.modalHost != nil && model.modalHost.Session != nil {
		t.Fatalf("expected modal closed after mouse create flow, got %#v", model.modalHost.Session)
	}

	e2eWaitForText(t, env.ctx, model, env.invalidated, "mouse_e2e_ready")
}

func TestE2EMousePaneChromeOmitsReconnectAction(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	env := newRealMouseE2EEnv(t)
	model := env.model
	paneID, _ := e2eCreateTerminalViaMouse(t, env, "mouse-reconnect-shell", "mouse_reconnect_ready")

	if paneChromeRegionPresent(model, paneID, render.HitRegionPaneReconnect) {
		t.Fatalf("expected pane chrome to omit reconnect action for pane %q", paneID)
	}
}

func TestE2EMouseFollowClickShowsConfirmAcrossTiledLayouts(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	cases := []struct {
		name  string
		build func(t *testing.T, env realMouseE2EEnv, paneID, terminalID string) string
	}{
		{
			name: "vertical-split-active-follower",
			build: func(t *testing.T, env realMouseE2EEnv, paneID, terminalID string) string {
				return e2eShareTerminalInSplitPane(t, env, paneID, "pane-2", workbench.SplitVertical, terminalID)
			},
		},
		{
			name: "horizontal-split-active-follower",
			build: func(t *testing.T, env realMouseE2EEnv, paneID, terminalID string) string {
				return e2eShareTerminalInSplitPane(t, env, paneID, "pane-2", workbench.SplitHorizontal, terminalID)
			},
		},
		{
			name: "nested-right-bottom-follower",
			build: func(t *testing.T, env realMouseE2EEnv, paneID, terminalID string) string {
				right := e2eShareTerminalInSplitPane(t, env, paneID, "pane-2", workbench.SplitVertical, terminalID)
				return e2eShareTerminalInSplitPane(t, env, right, "pane-3", workbench.SplitHorizontal, terminalID)
			},
		},
		{
			name: "nested-left-bottom-follower",
			build: func(t *testing.T, env realMouseE2EEnv, paneID, terminalID string) string {
				_ = e2eShareTerminalInSplitPane(t, env, paneID, "pane-2", workbench.SplitVertical, terminalID)
				if err := env.model.workbench.FocusPane(env.model.workbench.CurrentTab().ID, paneID); err != nil {
					t.Fatalf("refocus left pane: %v", err)
				}
				env.model.render.Invalidate()
				return e2eShareTerminalInSplitPane(t, env, paneID, "pane-3", workbench.SplitHorizontal, terminalID)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newRealMouseE2EEnv(t)
			model := env.model
			ownerPaneID, terminalID := e2eCreateTerminalViaMouse(t, env, tc.name, tc.name+"_ready")
			targetPaneID := tc.build(t, env, ownerPaneID, terminalID)

			terminal := model.runtime.Registry().Get(terminalID)
			if terminal == nil {
				t.Fatalf("expected terminal %q in runtime registry", terminalID)
			}
			if terminal.OwnerPaneID != ownerPaneID {
				t.Fatalf("expected %q to stay owner before mouse click, got %q", ownerPaneID, terminal.OwnerPaneID)
			}

			_ = model.View()
			button := visiblePaneChromeRegion(t, model, targetPaneID, render.HitRegionPaneOwner)
			e2eMouseClickAtImmediate(t, model, button.Rect.X, e2eScreenYForBodyY(model, button.Rect.Y))

			if model.ownerConfirmPaneID != targetPaneID {
				t.Fatalf("expected owner confirm armed for %q, got %q", targetPaneID, model.ownerConfirmPaneID)
			}
			if terminal.OwnerPaneID != ownerPaneID {
				t.Fatalf("expected owner unchanged after first click, got %q", terminal.OwnerPaneID)
			}
			if !strings.Contains(model.View(), "◆ owner?") {
				t.Fatalf("expected owner confirm rendered after first click:\n%s", model.View())
			}

			button = visiblePaneChromeRegion(t, model, targetPaneID, render.HitRegionPaneOwner)
			e2eMouseClickAtImmediate(t, model, button.Rect.X, e2eScreenYForBodyY(model, button.Rect.Y))
			e2eWaitForOwner(t, env.ctx, model, targetPaneID)

			if terminal.OwnerPaneID != targetPaneID {
				t.Fatalf("expected %q to become owner, got %q", targetPaneID, terminal.OwnerPaneID)
			}
		})
	}
}

func TestE2EMultiClientSharedSessionSyncsPaneLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	env := newSharedSessionE2EHarness(t)
	term1 := env.createTerminal(t, "shared-1", "shared_one_ready")
	clientA := env.newClient(t, 120, 40, term1)
	e2eWaitForText(t, env.ctx, clientA.model, clientA.invalidated, "shared_one_ready")

	clientB := env.newClient(t, 90, 30, "")
	e2eWaitForText(t, env.ctx, clientB.model, clientB.invalidated, "shared_one_ready")

	term2 := env.createTerminal(t, "shared-2", "shared_two_ready")
	e2eDrainSkippingPrefixTimeout(t, clientA.model, clientA.model.splitPaneAndAttachTerminalCmd("1", term2))
	newPaneID := clientA.model.workbench.CurrentTab().ActivePaneID
	if newPaneID == "" || newPaneID == "1" {
		t.Fatalf("expected split pane to create a new active pane, got %q", newPaneID)
	}
	snapshot := e2eWaitForSessionRevision(t, env.ctx, env.control, 2)
	if tab := snapshot.Workbench.Workspaces["main"].Tabs[0]; tab == nil || tab.Panes[newPaneID] == nil {
		t.Fatalf("expected daemon session to contain pane %q, got %#v", newPaneID, snapshot.Workbench)
	}
	_ = e2eWaitForSessionEvent(t, env.ctx, clientB, 2)

	e2eWaitForPaneTerminal(t, env.ctx, clientB, newPaneID, term2)
	e2eWaitForText(t, env.ctx, clientB.model, clientB.invalidated, "shared_two_ready")

	_, cmd := clientA.model.Update(input.SemanticAction{Kind: input.ActionClosePane, PaneID: newPaneID})
	e2eDrainSkippingPrefixTimeout(t, clientA.model, cmd)
	snapshot = e2eWaitForSessionRevision(t, env.ctx, env.control, snapshot.Session.Revision+1)
	if tab := snapshot.Workbench.Workspaces["main"].Tabs[0]; tab != nil && tab.Panes[newPaneID] != nil {
		t.Fatalf("expected daemon session to remove pane %q, got %#v", newPaneID, snapshot.Workbench)
	}
	_ = e2eWaitForSessionEvent(t, env.ctx, clientB, snapshot.Session.Revision)
	e2eWaitForPaneAbsent(t, env.ctx, clientB, newPaneID)
}

func TestE2EMultiClientSessionKeepsLocalTabSelectionIndependent(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	env := newSharedSessionE2EHarness(t)
	term1 := env.createTerminal(t, "tab-shared-1", "tab_one_ready")
	clientA := env.newClient(t, 120, 40, term1)
	e2eWaitForText(t, env.ctx, clientA.model, clientA.invalidated, "tab_one_ready")

	clientB := env.newClient(t, 80, 20, "")
	e2eWaitForText(t, env.ctx, clientB.model, clientB.invalidated, "tab_one_ready")

	term2 := env.createTerminal(t, "tab-shared-2", "tab_two_ready")
	e2eDrainSkippingPrefixTimeout(t, clientA.model, clientA.model.createTabAndAttachTerminalCmd(term2))
	_ = e2eWaitForSessionRevision(t, env.ctx, env.control, 2)
	e2eWaitForTabCount(t, env.ctx, clientB, 2)

	wsA := clientA.model.workbench.CurrentWorkspace()
	if wsA == nil || len(wsA.Tabs) != 2 {
		t.Fatalf("expected clientA to have 2 tabs, got %#v", wsA)
	}
	firstTabID := wsA.Tabs[0].ID
	secondTabID := wsA.Tabs[1].ID

	if err := clientA.model.workbench.SwitchTab(wsA.Name, 0); err != nil {
		t.Fatalf("switch clientA back to first tab: %v", err)
	}
	e2eDrainSkippingPrefixTimeout(t, clientA.model, clientA.model.saveStateCmd())
	e2eWaitForCurrentTabID(t, env.ctx, clientA, firstTabID)

	if err := clientB.model.workbench.SwitchTab(wsA.Name, 1); err != nil {
		t.Fatalf("switch clientB to second tab: %v", err)
	}
	e2eDrainSkippingPrefixTimeout(t, clientB.model, clientB.model.saveStateCmd())
	e2eWaitForCurrentTabID(t, env.ctx, clientB, secondTabID)
	e2eWaitForCurrentTabID(t, env.ctx, clientA, firstTabID)
}

func TestE2EMultiClientSharedSessionSyncsTabClose(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	env := newSharedSessionE2EHarness(t)
	term1 := env.createTerminal(t, "tab-close-1", "tab_close_one_ready")
	clientA := env.newClient(t, 120, 40, term1)
	e2eWaitForText(t, env.ctx, clientA.model, clientA.invalidated, "tab_close_one_ready")

	clientB := env.newClient(t, 80, 20, "")
	e2eWaitForText(t, env.ctx, clientB.model, clientB.invalidated, "tab_close_one_ready")

	term2 := env.createTerminal(t, "tab-close-2", "tab_close_two_ready")
	e2eDrainSkippingPrefixTimeout(t, clientA.model, clientA.model.createTabAndAttachTerminalCmd(term2))
	snapshot := e2eWaitForSessionRevision(t, env.ctx, env.control, 2)
	e2eWaitForTabCount(t, env.ctx, clientB, 2)

	wsA := clientA.model.workbench.CurrentWorkspace()
	if wsA == nil || len(wsA.Tabs) != 2 {
		t.Fatalf("expected clientA to have 2 tabs, got %#v", wsA)
	}
	secondTabID := wsA.Tabs[1].ID

	_, cmd := clientA.model.Update(input.SemanticAction{Kind: input.ActionCloseTab, TabID: secondTabID})
	e2eDrainSkippingPrefixTimeout(t, clientA.model, cmd)

	snapshot = e2eWaitForSessionRevision(t, env.ctx, env.control, snapshot.Session.Revision+1)
	ws := snapshot.Workbench.Workspaces["main"]
	if ws == nil {
		t.Fatalf("expected daemon session main workspace, got %#v", snapshot.Workbench)
	}
	if len(ws.Tabs) != 1 {
		t.Fatalf("expected daemon session to contain 1 tab after close, got %#v", ws.Tabs)
	}
	for _, tab := range ws.Tabs {
		if tab != nil && tab.ID == secondTabID {
			t.Fatalf("expected daemon session to remove closed tab %q, got %#v", secondTabID, ws.Tabs)
		}
	}

	_ = e2eWaitForSessionEvent(t, env.ctx, clientB, snapshot.Session.Revision)
	e2eWaitForTabCount(t, env.ctx, clientB, 1)
}

func TestE2EMultiClientReconnectRestoresSessionAndReceivesLaterUpdates(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	env := newSharedSessionE2EHarness(t)
	term1 := env.createTerminal(t, "reconnect-1", "reconnect_one_ready")
	clientA := env.newClient(t, 120, 40, term1)
	e2eWaitForText(t, env.ctx, clientA.model, clientA.invalidated, "reconnect_one_ready")

	clientB := env.newClient(t, 100, 30, "")
	e2eWaitForText(t, env.ctx, clientB.model, clientB.invalidated, "reconnect_one_ready")

	term2 := env.createTerminal(t, "reconnect-2", "reconnect_two_ready")
	e2eDrainSkippingPrefixTimeout(t, clientA.model, clientA.model.splitPaneAndAttachTerminalCmd("1", term2))
	secondPaneID := clientA.model.workbench.CurrentTab().ActivePaneID
	_ = e2eWaitForSessionRevision(t, env.ctx, env.control, 2)
	e2eWaitForPaneTerminal(t, env.ctx, clientB, secondPaneID, term2)

	_ = clientB.raw.Close()

	clientC := env.newClient(t, 100, 30, "")
	e2eWaitForPaneTerminal(t, env.ctx, clientC, secondPaneID, term2)
	e2eWaitForText(t, env.ctx, clientC.model, clientC.invalidated, "reconnect_two_ready")

	_, cmd := clientA.model.Update(input.SemanticAction{Kind: input.ActionClosePane, PaneID: secondPaneID})
	e2eDrainSkippingPrefixTimeout(t, clientA.model, cmd)
	e2eWaitForPaneAbsent(t, env.ctx, clientC, secondPaneID)
}

func TestE2EMultiClientSharedPaneInputSyncsOwnerBadgeAndLastActiveSize(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	env := newSharedSessionE2EHarness(t)
	terminalID := env.createTerminal(t, "shared-same-pane", "shared_same_pane_ready")

	clientA := env.newClient(t, 120, 40, terminalID)
	e2eWaitForText(t, env.ctx, clientA.model, clientA.invalidated, "shared_same_pane_ready")

	clientB := env.newClient(t, 90, 30, "")
	e2eWaitForText(t, env.ctx, clientB.model, clientB.invalidated, "shared_same_pane_ready")

	paneA := clientA.model.workbench.ActivePane()
	paneB := clientB.model.workbench.ActivePane()
	if paneA == nil || paneB == nil {
		t.Fatalf("expected active panes in both clients, got A=%#v B=%#v", paneA, paneB)
	}
	if paneA.ID != paneB.ID {
		t.Fatalf("expected both clients to show the same pane, got A=%q B=%q", paneA.ID, paneB.ID)
	}
	if paneA.TerminalID != terminalID || paneB.TerminalID != terminalID {
		t.Fatalf("expected both clients bound to %q, got A=%q B=%q", terminalID, paneA.TerminalID, paneB.TerminalID)
	}

	rectA := e2eVisiblePaneRect(t, clientA.model, paneA.ID)
	contentA, ok := paneContentRect(rectA)
	if !ok {
		t.Fatal("expected client A content rect")
	}
	wantACols := uint16(maxInt(2, contentA.W))
	wantARows := uint16(maxInt(2, contentA.H))

	e2eDrainSkippingPrefixTimeout(t, clientA.model, clientA.model.acquireSessionLeaseAndResizeCmd(paneA.ID, terminalID))
	e2eWaitForTerminalOwnerPane(t, env.ctx, clientA.model, terminalID, paneA.ID)
	e2eWaitForTerminalOwnerPane(t, env.ctx, clientB.model, terminalID, paneA.ID)
	e2eWaitForTerminalSize(t, env.ctx, clientA.model, terminalID, wantACols, wantARows)
	e2eWaitForServerTerminalSize(t, env.ctx, env.control, terminalID, wantACols, wantARows)
	e2eWaitForSharedPaneOwnerBadge(t, env.ctx, clientA, paneA.ID)
	e2eWaitForSharedPaneOwnerBadge(t, env.ctx, clientB, paneA.ID)

	_, cmd := clientB.model.Update(input.TerminalInput{PaneID: paneB.ID, Data: []byte("client_b_active\n")})
	e2eDrainSkippingPrefixTimeout(t, clientB.model, cmd)
	e2eWaitForText(t, env.ctx, clientB.model, clientB.invalidated, "client_b_active")
	e2eWaitForText(t, env.ctx, clientA.model, clientA.invalidated, "client_b_active")

	rectB := e2eVisiblePaneRect(t, clientB.model, paneB.ID)
	contentB, ok := paneContentRect(rectB)
	if !ok {
		t.Fatal("expected client B content rect")
	}
	wantBCols := uint16(maxInt(2, contentB.W))
	wantBRows := uint16(maxInt(2, contentB.H))
	e2eWaitForTerminalOwnerPane(t, env.ctx, clientA.model, terminalID, paneA.ID)
	e2eWaitForTerminalOwnerPane(t, env.ctx, clientB.model, terminalID, paneA.ID)
	e2eWaitForTerminalSize(t, env.ctx, clientB.model, terminalID, wantBCols, wantBRows)
	e2eWaitForServerTerminalSize(t, env.ctx, env.control, terminalID, wantBCols, wantBRows)
	e2eWaitForSharedPaneOwnerBadge(t, env.ctx, clientA, paneA.ID)
	e2eWaitForSharedPaneOwnerBadge(t, env.ctx, clientB, paneA.ID)

	_, cmd = clientA.model.Update(input.TerminalInput{PaneID: paneA.ID, Data: []byte("client_a_active\n")})
	e2eDrainSkippingPrefixTimeout(t, clientA.model, cmd)
	e2eWaitForText(t, env.ctx, clientA.model, clientA.invalidated, "client_a_active")
	e2eWaitForText(t, env.ctx, clientB.model, clientB.invalidated, "client_a_active")

	e2eWaitForTerminalOwnerPane(t, env.ctx, clientA.model, terminalID, paneA.ID)
	e2eWaitForTerminalOwnerPane(t, env.ctx, clientB.model, terminalID, paneA.ID)
	e2eWaitForTerminalSize(t, env.ctx, clientA.model, terminalID, wantACols, wantARows)
	e2eWaitForServerTerminalSize(t, env.ctx, env.control, terminalID, wantACols, wantARows)
	e2eWaitForSharedPaneOwnerBadge(t, env.ctx, clientA, paneA.ID)
	e2eWaitForSharedPaneOwnerBadge(t, env.ctx, clientB, paneA.ID)
}

func TestE2ETabSwitchSharedTerminalPromotesOwnerResizesAndShowsCursor(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	env := newRealMouseE2EEnv(t)
	model := env.model
	client := model.runtime.Client()
	if client == nil {
		t.Fatal("expected runtime client")
	}

	created, err := client.Create(env.ctx, protocol.CreateParams{
		Command: []string{"sh", "-c", "printf '\\033[30;90H@\\033[30;90H'; cat"},
		Name:    "shared-tab-cursor",
		Size:    protocol.Size{Cols: 120, Rows: 40},
	})
	if err != nil {
		t.Fatalf("create shared terminal: %v", err)
	}

	e2eDrainSkippingPrefixTimeout(t, model, model.attachInitialTerminalCmd(created.TerminalID))
	firstPane := model.workbench.ActivePane()
	if firstPane == nil || firstPane.TerminalID != created.TerminalID {
		t.Fatalf("expected first pane attached to %q, got %#v", created.TerminalID, firstPane)
	}
	firstPaneID := firstPane.ID

	e2eDrainSkippingPrefixTimeout(t, model, model.createTabAndAttachTerminalCmd(created.TerminalID))
	secondPane := model.workbench.ActivePane()
	if secondPane == nil || secondPane.TerminalID != created.TerminalID {
		t.Fatalf("expected second tab pane attached to %q, got %#v", created.TerminalID, secondPane)
	}
	secondPaneID := secondPane.ID

	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected second tab after createTabAndAttachTerminalCmd")
	}
	if err := model.workbench.SplitPane(tab.ID, secondPaneID, "pane-side", workbench.SplitVertical); err != nil {
		t.Fatalf("split second tab pane: %v", err)
	}
	if err := model.workbench.FocusPane(tab.ID, secondPaneID); err != nil {
		t.Fatalf("refocus shared pane on second tab: %v", err)
	}
	model.render.Invalidate()

	e2eDrainSkippingPrefixTimeout(t, model, model.switchTabByIndexMouse(0))
	e2eWaitForTerminalOwnerPane(t, env.ctx, model, created.TerminalID, firstPaneID)

	e2eDrainSkippingPrefixTimeout(t, model, model.switchTabByIndexMouse(1))
	e2eWaitForTerminalOwnerPane(t, env.ctx, model, created.TerminalID, secondPaneID)

	visible := model.workbench.VisibleWithSize(model.bodyRect())
	if visible == nil || visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		t.Fatalf("expected visible state after second tab switch, got %#v", visible)
	}
	var target *workbench.VisiblePane
	for i := range visible.Tabs[visible.ActiveTab].Panes {
		pane := &visible.Tabs[visible.ActiveTab].Panes[i]
		if pane.ID == secondPaneID {
			target = pane
			break
		}
	}
	if target == nil {
		t.Fatalf("expected visible pane %q after second tab switch, got %#v", secondPaneID, visible.Tabs[visible.ActiveTab].Panes)
	}

	targetContent, ok := paneContentRectForVisible(*target)
	if !ok {
		t.Fatal("expected target content rect")
	}
	wantCols := uint16(maxInt(2, targetContent.W))
	wantRows := uint16(maxInt(2, targetContent.H))
	e2eWaitForTerminalSize(t, env.ctx, model, created.TerminalID, wantCols, wantRows)
	e2eWaitForCursorHighlight(t, env.ctx, model, env.invalidated)
}

type realMouseE2EEnv struct {
	ctx         context.Context
	model       *Model
	invalidated chan struct{}
}

type sharedSessionE2EHarness struct {
	ctx        context.Context
	socketPath string
	control    *protocol.Client
}

type sharedSessionClient struct {
	model       *Model
	invalidated chan struct{}
	raw         *protocol.Client
	events      chan protocol.Event
}

func newRealMouseE2EEnv(t *testing.T) realMouseE2EEnv {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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

	tr, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	pc := protocol.NewClient(tr)
	if err := pc.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	model := New(shared.Config{}, nil, runtime.New(bridge.NewProtocolClient(pc)))
	model.width = 120
	model.height = 40

	invalidated := make(chan struct{}, 64)
	model.SetSendFunc(func(msg tea.Msg) {
		if _, ok := msg.(InvalidateMsg); ok {
			select {
			case invalidated <- struct{}{}:
			default:
			}
		}
	})

	e2eDrain(t, model, model.Init())
	return realMouseE2EEnv{
		ctx:         ctx,
		model:       model,
		invalidated: invalidated,
	}
}

func newRealRestoreE2EEnv(t *testing.T, file persist.WorkspaceStateFileV2) realMouseE2EEnv {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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

	tr, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	pc := protocol.NewClient(tr)
	if err := pc.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	statePath := filepath.Join(t.TempDir(), "workspace-state.json")
	data, err := json.Marshal(file)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	model := New(shared.Config{WorkspaceStatePath: statePath}, workbench.NewWorkbench(), runtime.New(bridge.NewProtocolClient(pc)))
	model.width = 120
	model.height = 40

	invalidated := make(chan struct{}, 64)
	model.SetSendFunc(func(msg tea.Msg) {
		if _, ok := msg.(InvalidateMsg); ok {
			select {
			case invalidated <- struct{}{}:
			default:
			}
		}
	})

	e2eDrain(t, model, model.Init())
	return realMouseE2EEnv{
		ctx:         ctx,
		model:       model,
		invalidated: invalidated,
	}
}

func newSharedSessionE2EHarness(t *testing.T) sharedSessionE2EHarness {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	socketPath := filepath.Join(t.TempDir(), "termx-shared-e2e.sock")
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
	control := e2eDialProtocolClient(t, ctx, socketPath)
	return sharedSessionE2EHarness{
		ctx:        ctx,
		socketPath: socketPath,
		control:    control,
	}
}

func e2eDialProtocolClient(t *testing.T, ctx context.Context, socketPath string) *protocol.Client {
	t.Helper()
	tr, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	pc := protocol.NewClient(tr)
	if err := pc.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	return pc
}

func (h sharedSessionE2EHarness) newClient(t *testing.T, width, height int, attachID string) sharedSessionClient {
	t.Helper()
	pc := e2eDialProtocolClient(t, h.ctx, h.socketPath)
	model := New(shared.Config{SessionID: "main", AttachID: attachID}, nil, runtime.New(bridge.NewProtocolClient(pc)))
	model.width = width
	model.height = height

	invalidated := make(chan struct{}, 128)
	eventLog := make(chan protocol.Event, 32)
	model.SetSendFunc(func(msg tea.Msg) {
		if _, ok := msg.(InvalidateMsg); ok {
			select {
			case invalidated <- struct{}{}:
			default:
			}
		}
		_, cmd := model.Update(msg)
		e2eDrainSkippingPrefixTimeout(t, model, cmd)
	})

	if err := model.bootstrapStartup(); err != nil {
		t.Fatalf("bootstrap startup: %v", err)
	}
	if attachID != "" {
		e2eDrainSkippingPrefixTimeout(t, model, model.attachInitialTerminalCmd(attachID))
	}

	events, err := pc.Events(h.ctx, protocol.EventsParams{
		SessionID: "main",
		Types: []protocol.EventType{
			protocol.EventSessionCreated,
			protocol.EventSessionUpdated,
			protocol.EventSessionDeleted,
		},
	})
	if err != nil {
		t.Fatalf("subscribe session events: %v", err)
	}
	terminalEventsClient := e2eDialProtocolClient(t, h.ctx, h.socketPath)
	terminalEvents, err := terminalEventsClient.Events(h.ctx, protocol.EventsParams{
		Types: []protocol.EventType{
			protocol.EventTerminalResized,
		},
	})
	if err != nil {
		t.Fatalf("subscribe terminal events: %v", err)
	}
	go func() {
		for evt := range events {
			select {
			case invalidated <- struct{}{}:
			default:
			}
			select {
			case eventLog <- evt:
			default:
			}
			_, cmd := model.Update(sessionEventMsg{Event: evt})
			e2eDrainSkippingPrefixTimeout(t, model, cmd)
		}
	}()
	go func() {
		for evt := range terminalEvents {
			select {
			case invalidated <- struct{}{}:
			default:
			}
			_, cmd := model.Update(terminalEventMsg{Event: evt})
			e2eDrainSkippingPrefixTimeout(t, model, cmd)
		}
	}()

	return sharedSessionClient{
		model:       model,
		invalidated: invalidated,
		raw:         pc,
		events:      eventLog,
	}
}

func (h sharedSessionE2EHarness) createTerminal(t *testing.T, name, marker string) string {
	t.Helper()
	created, err := h.control.Create(h.ctx, protocol.CreateParams{
		Command: []string{"sh", "-c", fmt.Sprintf("printf '%s\\n'; cat", marker)},
		Name:    name,
		Size:    protocol.Size{Cols: 120, Rows: 40},
	})
	if err != nil {
		t.Fatalf("create terminal %q: %v", name, err)
	}
	return created.TerminalID
}

func e2eWaitForPaneTerminal(t *testing.T, ctx context.Context, client sharedSessionClient, paneID, terminalID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		tab := client.model.workbench.CurrentTab()
		if tab != nil {
			if pane := tab.Panes[paneID]; pane != nil && pane.TerminalID == terminalID {
				return
			}
		}
		select {
		case <-client.invalidated:
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("context expired waiting for pane %q terminal %q", paneID, terminalID)
		}
	}
	t.Fatalf("timeout waiting for pane %q terminal %q", paneID, terminalID)
}

func e2eWaitForTabCount(t *testing.T, ctx context.Context, client sharedSessionClient, count int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ws := client.model.workbench.CurrentWorkspace()
		if ws != nil && len(ws.Tabs) == count {
			return
		}
		select {
		case <-client.invalidated:
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("context expired waiting for tab count %d", count)
		}
	}
	ws := client.model.workbench.CurrentWorkspace()
	if ws == nil {
		t.Fatalf("timeout waiting for tab count %d: no workspace", count)
	}
	t.Fatalf("timeout waiting for tab count %d: got %d", count, len(ws.Tabs))
}

func e2eWaitForPaneAbsent(t *testing.T, ctx context.Context, client sharedSessionClient, paneID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		tab := client.model.workbench.CurrentTab()
		if tab == nil || tab.Panes[paneID] == nil {
			return
		}
		select {
		case <-client.invalidated:
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("context expired waiting for pane %q to disappear", paneID)
		}
	}
	t.Fatalf("timeout waiting for pane %q to disappear", paneID)
}

func e2eWaitForCurrentTabID(t *testing.T, ctx context.Context, client sharedSessionClient, tabID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		tab := client.model.workbench.CurrentTab()
		if tab != nil && tab.ID == tabID {
			return
		}
		select {
		case <-client.invalidated:
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("context expired waiting for current tab %q", tabID)
		}
	}
	tab := client.model.workbench.CurrentTab()
	if tab == nil {
		t.Fatalf("timeout waiting for current tab %q: no current tab", tabID)
	}
	t.Fatalf("timeout waiting for current tab %q: got %q", tabID, tab.ID)
}

func e2eWaitForSessionRevision(t *testing.T, ctx context.Context, client *protocol.Client, revision uint64) *protocol.SessionSnapshot {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := client.GetSession(ctx, "main")
		if err == nil && snapshot != nil && snapshot.Session.Revision >= revision {
			return snapshot
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("context expired waiting for session revision %d", revision)
		}
	}
	t.Fatalf("timeout waiting for session revision %d", revision)
	return nil
}

func e2eWaitForSessionEvent(t *testing.T, ctx context.Context, client sharedSessionClient, minRevision uint64) protocol.Event {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case evt := <-client.events:
			if evt.Session != nil && evt.Session.Revision >= minRevision {
				return evt
			}
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("context expired waiting for session event revision %d", minRevision)
		}
	}
	t.Fatalf("timeout waiting for session event revision %d", minRevision)
	return protocol.Event{}
}

func e2eCreateTerminalViaMouse(t *testing.T, env realMouseE2EEnv, name, readyMarker string) (string, string) {
	t.Helper()
	model := env.model
	items := model.modalHost.Picker.VisibleItems()
	targetIndex := pickerCreateRowIndex(items)
	if targetIndex < 0 {
		t.Fatalf("expected create-new picker row, got %#v", items)
	}

	target := overlayPickerItemRegion(t, model, targetIndex)
	e2eMouseClickAt(t, model, target.Rect.X, e2eScreenYForBodyY(model, target.Rect.Y))
	if model.modalHost == nil || model.modalHost.Prompt == nil {
		t.Fatalf("expected prompt after mouse create-row click, got %#v", model.modalHost)
	}

	inputRegion := e2eOverlayRegionByKind(t, model, render.HitRegionPromptInput)
	e2eMouseClickAt(t, model, inputRegion.Rect.X, e2eScreenYForBodyY(model, inputRegion.Rect.Y))
	e2eTypeText(t, model, name)
	if movePromptFormField(model.modalHost.Prompt, 1) {
		model.render.Invalidate()
	}
	e2eTypeText(t, model, fmt.Sprintf("sh -c \"printf '%s\\n'; cat\"", readyMarker))

	submit := e2eOverlayRegionByKind(t, model, render.HitRegionPromptSubmit)
	e2eMouseClickAt(t, model, submit.Rect.X, e2eScreenYForBodyY(model, submit.Rect.Y))

	pane := model.workbench.ActivePane()
	if pane == nil || pane.TerminalID == "" {
		t.Fatalf("expected pane attached after mouse create flow, got %#v", pane)
	}
	e2eWaitForText(t, env.ctx, model, env.invalidated, readyMarker)
	return pane.ID, pane.TerminalID
}

func e2eMouseClickAt(t *testing.T, m *Model, x, y int) {
	t.Helper()
	_, cmd := m.Update(tea.MouseMsg{
		X:      x,
		Y:      y,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	e2eDrain(t, m, cmd)
}

func e2eMouseClickAtImmediate(t *testing.T, m *Model, x, y int) {
	t.Helper()
	_, cmd := m.Update(tea.MouseMsg{
		X:      x,
		Y:      y,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	drainCmd(t, m, cmd, 20)
}

func e2eDismissActiveOverlayIfAny(t *testing.T, m *Model) {
	t.Helper()
	state := m.visibleRenderState()
	if state.Overlay.Kind == render.VisibleOverlayNone {
		return
	}
	dismiss := e2eOverlayRegionByKind(t, m, render.HitRegionOverlayDismiss)
	e2eMouseClickAt(t, m, dismiss.Rect.X, e2eScreenYForBodyY(m, dismiss.Rect.Y))
}

func e2eScreenYForBodyY(m *Model, bodyY int) int {
	return bodyY + m.contentOriginY()
}

func e2eTypeText(t *testing.T, m *Model, text string) {
	t.Helper()
	for _, r := range text {
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		e2eDrain(t, m, cmd)
	}
}

func e2eTabBarRegionByKind(t *testing.T, m *Model, kind render.HitRegionKind) render.HitRegion {
	t.Helper()
	state := m.visibleRenderState()
	regions := render.TabBarHitRegions(state)
	for _, region := range regions {
		if region.Kind == kind {
			return region
		}
	}
	t.Fatalf("expected tab bar region %q, got %#v", kind, regions)
	return render.HitRegion{}
}

func e2eOverlayRegionByKind(t *testing.T, m *Model, kind render.HitRegionKind) render.HitRegion {
	t.Helper()
	state := m.visibleRenderState()
	regions := render.OverlayHitRegions(state)
	for _, region := range regions {
		if region.Kind == kind {
			return region
		}
	}
	t.Fatalf("expected overlay region %q, got %#v", kind, regions)
	return render.HitRegion{}
}

func e2eOverlayFooterActionRegion(t *testing.T, m *Model, kind input.ActionKind) render.HitRegion {
	t.Helper()
	state := m.visibleRenderState()
	regions := render.OverlayHitRegions(state)
	for _, region := range regions {
		if region.Kind == render.HitRegionOverlayFooterAction && region.Action.Kind == kind {
			return region
		}
	}
	t.Fatalf("expected overlay footer action region %q, got %#v", kind, regions)
	return render.HitRegion{}
}

func e2eOverlayWorkspaceItemRegion(t *testing.T, m *Model, index int) render.HitRegion {
	t.Helper()
	state := m.visibleRenderState()
	regions := render.OverlayHitRegions(state)
	for _, region := range regions {
		if region.Kind == render.HitRegionWorkspaceItem && region.ItemIndex == index {
			return region
		}
	}
	t.Fatalf("expected workspace item region %d, got %#v", index, regions)
	return render.HitRegion{}
}

func pickerCreateRowIndex(items []modal.PickerItem) int {
	for i, item := range items {
		if item.CreateNew {
			return i
		}
	}
	return -1
}

func setCreateTerminalFormField(prompt *modal.PromptState, key, value string) {
	if prompt == nil {
		return
	}
	field := prompt.Field(key)
	if field == nil {
		return
	}
	field.Value = value
	field.Cursor = len([]rune(value))
}

func overlayPickerItemRegion(t *testing.T, m *Model, index int) render.HitRegion {
	t.Helper()
	state := m.visibleRenderState()
	regions := render.OverlayHitRegions(state)
	for _, region := range regions {
		if region.Kind == render.HitRegionPickerItem && region.ItemIndex == index {
			return region
		}
	}
	t.Fatalf("expected picker item region %d, got %#v", index, regions)
	return render.HitRegion{}
}

func e2eDispatchKey(t *testing.T, m *Model, msg tea.KeyMsg) {
	t.Helper()
	_, cmd := m.Update(msg)
	if cmd == nil {
		return
	}
	e2eDrainSkippingPrefixTimeout(t, m, cmd)
}

// e2eDrain recursively executes cmd and all downstream commands, updating the
// model with each returned message.  It handles tea.BatchMsg by processing
// each item individually.
func e2eDrain(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	e2eDrainMsg(t, m, cmd(), false)
}

func e2eDrainSkippingPrefixTimeout(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	e2eDrainMsg(t, m, cmd(), true)
}

func e2eDrainMsg(t *testing.T, m *Model, msg tea.Msg, skipPrefixTimeout bool) {
	t.Helper()
	if msg == nil {
		return
	}
	if skipPrefixTimeout {
		if _, ok := msg.(prefixTimeoutMsg); ok {
			return
		}
	}
	switch typed := msg.(type) {
	case tea.BatchMsg:
		for _, item := range typed {
			if item == nil {
				continue
			}
			e2eDrainMsg(t, m, item(), skipPrefixTimeout)
		}
	default:
		_, nextCmd := m.Update(typed)
		if nextCmd != nil {
			e2eDrainMsg(t, m, nextCmd(), skipPrefixTimeout)
		}
	}
}

// e2eWaitForText polls model.View() until it contains the target string or
// the test context expires.  Between polls it waits for an InvalidateMsg to
// avoid busy-looping.
func e2eWaitForText(t *testing.T, ctx context.Context, m *Model, invalidated <-chan struct{}, target string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		view := xansi.Strip(m.View())
		if strings.Contains(view, target) {
			return
		}
		select {
		case <-invalidated:
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("context expired waiting for %q in view", target)
		}
	}
	t.Fatalf("timeout: %q never appeared in view\nfinal view:\n%s\nsnapshot excerpt:\n%s", target, xansi.Strip(m.View()), e2eActiveSnapshotExcerpt(m))
}

func e2eActiveSnapshotExcerpt(m *Model) string {
	if m == nil || m.workbench == nil || m.runtime == nil {
		return "<unavailable>"
	}
	pane := m.workbench.ActivePane()
	if pane == nil || pane.TerminalID == "" {
		return "<no active terminal>"
	}
	terminal := m.runtime.Registry().Get(pane.TerminalID)
	if terminal == nil || terminal.Snapshot == nil {
		return "<no snapshot>"
	}
	lines := make([]string, 0, 8)
	for _, row := range terminal.Snapshot.Screen.Cells {
		var b strings.Builder
		for _, cell := range row {
			b.WriteString(cell.Content)
		}
		lines = append(lines, b.String())
		if len(lines) >= 8 {
			break
		}
	}
	return strings.Join(lines, "\n")
}

func e2eWaitForTitle(t *testing.T, ctx context.Context, m *Model, invalidated <-chan struct{}, terminalID string, target string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		terminal := m.runtime.Registry().Get(terminalID)
		if terminal != nil && terminal.Title == target {
			return
		}
		select {
		case <-invalidated:
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("context expired waiting for terminal %q title %q", terminalID, target)
		}
	}
	terminal := m.runtime.Registry().Get(terminalID)
	if terminal == nil {
		t.Fatalf("timeout waiting for terminal %q title %q: terminal missing", terminalID, target)
	}
	t.Fatalf("timeout waiting for terminal %q title %q: got %q", terminalID, target, terminal.Title)
}

func e2eWaitForOwner(t *testing.T, ctx context.Context, m *Model, targetPaneID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pane := m.workbench.ActivePane()
		if pane != nil && pane.TerminalID != "" {
			terminal := m.runtime.Registry().Get(pane.TerminalID)
			if terminal != nil && terminal.OwnerPaneID == targetPaneID {
				return
			}
		}
		select {
		case <-time.After(20 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("context expired waiting for owner %q", targetPaneID)
		}
	}
	pane := m.workbench.ActivePane()
	if pane == nil {
		t.Fatalf("timeout waiting for owner %q: no active pane", targetPaneID)
	}
	terminal := m.runtime.Registry().Get(pane.TerminalID)
	if terminal == nil {
		t.Fatalf("timeout waiting for owner %q: terminal %q missing", targetPaneID, pane.TerminalID)
	}
	t.Fatalf("timeout waiting for owner %q: got %q", targetPaneID, terminal.OwnerPaneID)
}

func e2eWaitForTerminalOwnerPane(t *testing.T, ctx context.Context, m *Model, terminalID, paneID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		terminal := m.runtime.Registry().Get(terminalID)
		if terminal != nil && terminal.OwnerPaneID == paneID {
			return
		}
		select {
		case <-time.After(20 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("context expired waiting for terminal %q owner %q", terminalID, paneID)
		}
	}
	terminal := m.runtime.Registry().Get(terminalID)
	if terminal == nil {
		t.Fatalf("timeout waiting for terminal %q owner %q: terminal missing", terminalID, paneID)
	}
	t.Fatalf("timeout waiting for terminal %q owner %q: got %q", terminalID, paneID, terminal.OwnerPaneID)
}

func e2eWaitForTerminalSize(t *testing.T, ctx context.Context, m *Model, terminalID string, cols, rows uint16) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		terminal := m.runtime.Registry().Get(terminalID)
		if terminal != nil && terminal.Snapshot != nil &&
			terminal.Snapshot.Size.Cols == cols && terminal.Snapshot.Size.Rows == rows {
			return
		}
		select {
		case <-time.After(20 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("context expired waiting for terminal %q size %dx%d", terminalID, cols, rows)
		}
	}
	terminal := m.runtime.Registry().Get(terminalID)
	if terminal == nil || terminal.Snapshot == nil {
		t.Fatalf("timeout waiting for terminal %q size %dx%d: snapshot missing", terminalID, cols, rows)
	}
	t.Fatalf(
		"timeout waiting for terminal %q size %dx%d: got %dx%d",
		terminalID,
		cols,
		rows,
		terminal.Snapshot.Size.Cols,
		terminal.Snapshot.Size.Rows,
	)
}

func e2eWaitForRuntimeTerminalState(t *testing.T, ctx context.Context, m *Model, terminalID, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		terminal := m.runtime.Registry().Get(terminalID)
		if terminal != nil && terminal.State == want {
			return
		}
		select {
		case <-time.After(20 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("context expired waiting for terminal %q state %q", terminalID, want)
		}
	}
	terminal := m.runtime.Registry().Get(terminalID)
	if terminal == nil {
		t.Fatalf("timeout waiting for terminal %q state %q: terminal missing", terminalID, want)
	}
	t.Fatalf("timeout waiting for terminal %q state %q: got %q", terminalID, want, terminal.State)
}

func e2eWaitForServerTerminalSize(t *testing.T, ctx context.Context, client *protocol.Client, terminalID string, cols, rows uint16) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := client.Snapshot(ctx, terminalID, 0, 0)
		if err == nil && snapshot != nil && snapshot.Size.Cols == cols && snapshot.Size.Rows == rows {
			return
		}
		select {
		case <-time.After(20 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("context expired waiting for server terminal %q size %dx%d", terminalID, cols, rows)
		}
	}
	snapshot, err := client.Snapshot(ctx, terminalID, 0, 0)
	if err != nil || snapshot == nil {
		t.Fatalf("timeout waiting for server terminal %q size %dx%d: snapshot err=%v snapshot=%#v", terminalID, cols, rows, err, snapshot)
	}
	t.Fatalf(
		"timeout waiting for server terminal %q size %dx%d: got %dx%d",
		terminalID,
		cols,
		rows,
		snapshot.Size.Cols,
		snapshot.Size.Rows,
	)
}

type delayedAttachBridgeClient struct {
	bridge.Client

	mu            sync.Mutex
	release       <-chan struct{}
	attachStarted chan<- struct{}
}

func (c *delayedAttachBridgeClient) DelayNextAttach(release <-chan struct{}, attachStarted chan<- struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.release = release
	c.attachStarted = attachStarted
}

func (c *delayedAttachBridgeClient) Attach(ctx context.Context, terminalID, mode string) (*protocol.AttachResult, error) {
	c.mu.Lock()
	release := c.release
	attachStarted := c.attachStarted
	c.release = nil
	c.attachStarted = nil
	c.mu.Unlock()

	if release != nil {
		if attachStarted != nil {
			select {
			case attachStarted <- struct{}{}:
			default:
			}
		}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return c.Client.Attach(ctx, terminalID, mode)
}

func e2eVisiblePaneRect(t *testing.T, m *Model, paneID string) workbench.Rect {
	t.Helper()
	visible := m.workbench.VisibleWithSize(m.bodyRect())
	if visible == nil || visible.ActiveTab < 0 || visible.ActiveTab >= len(visible.Tabs) {
		t.Fatalf("expected visible state for pane %q, got %#v", paneID, visible)
	}
	for i := range visible.Tabs[visible.ActiveTab].Panes {
		pane := visible.Tabs[visible.ActiveTab].Panes[i]
		if pane.ID == paneID {
			return pane.Rect
		}
	}
	for i := range visible.FloatingPanes {
		pane := visible.FloatingPanes[i]
		if pane.ID == paneID {
			return pane.Rect
		}
	}
	t.Fatalf("expected visible pane %q, got tiled=%#v floating=%#v", paneID, visible.Tabs[visible.ActiveTab].Panes, visible.FloatingPanes)
	return workbench.Rect{}
}

func e2eWaitForSharedPaneOwnerBadge(t *testing.T, ctx context.Context, client sharedSessionClient, paneID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		view := xansi.Strip(client.model.View())
		if strings.Contains(view, "◆ owner") && !strings.Contains(view, "◇ follow") {
			return
		}
		select {
		case <-client.invalidated:
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("context expired waiting for shared pane %q owner badge", paneID)
		}
	}
	t.Fatalf("timeout waiting for shared pane %q owner badge:\n%s", paneID, xansi.Strip(client.model.View()))
}

func e2eWaitForCursorHighlight(t *testing.T, ctx context.Context, m *Model, invalidated <-chan struct{}) {
	t.Helper()
	hostCursorANSI := regexp.MustCompile(`(?:\x1b\[[0-9]+;[0-9]+H\x1b\[[0-9]+ q\x1b\[\?25h)|(?:\x1b\[\?25l\x1b\[[0-9]+;[0-9]+H)`)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if hostCursorANSI.MatchString(m.View()) {
			return
		}
		select {
		case <-invalidated:
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal("context expired waiting for projected host cursor")
		}
	}
	t.Fatalf("timeout waiting for projected host cursor:\n%s", m.View())
}

func e2eShareTerminalInSplitPane(t *testing.T, env realMouseE2EEnv, sourcePaneID, newPaneID string, dir workbench.SplitDirection, terminalID string) string {
	t.Helper()

	model := env.model
	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := model.workbench.SplitPane(tab.ID, sourcePaneID, newPaneID, dir); err != nil {
		t.Fatalf("split pane %q -> %q: %v", sourcePaneID, newPaneID, err)
	}
	pane := tab.Panes[newPaneID]
	if pane == nil {
		t.Fatalf("expected new pane %q after split", newPaneID)
	}
	pane.TerminalID = terminalID
	pane.Title = newPaneID
	if _, err := model.runtime.AttachTerminal(env.ctx, newPaneID, terminalID, "collaborator"); err != nil {
		t.Fatalf("attach shared terminal %q to pane %q: %v", terminalID, newPaneID, err)
	}
	model.render.Invalidate()
	return newPaneID
}

// e2eWaitSocket dials the unix socket in a loop until it succeeds or the
// deadline passes.
func e2eWaitSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := unixtransport.Dial(path)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("socket %s did not appear within %s", path, timeout)
}

func e2eRegistrySummary(rt *runtime.Runtime) string {
	if rt == nil || rt.Registry() == nil {
		return "<nil>"
	}
	ids := rt.Registry().IDs()
	if len(ids) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		terminal := rt.Registry().Get(id)
		if terminal == nil {
			parts = append(parts, id+":<nil>")
			continue
		}
		parts = append(parts, fmt.Sprintf("%s(state=%q,name=%q,owner=%q,bound=%v)", id, terminal.State, terminal.Name, terminal.OwnerPaneID, terminal.BoundPaneIDs))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func e2eTerminalInfoSummary(terminals []protocol.TerminalInfo) string {
	if len(terminals) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(terminals))
	for _, terminal := range terminals {
		parts = append(parts, fmt.Sprintf("%s(state=%q,name=%q)", terminal.ID, terminal.State, terminal.Name))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
