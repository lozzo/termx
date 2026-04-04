package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx"
	"github.com/lozzow/termx/protocol"
	unixtransport "github.com/lozzow/termx/transport/unix"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/input"
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
			`PROMPT_COMMAND=printf '\033]2;enter-shell-ready\007'`,
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
	e2eWaitForTitle(t, ctx, model, invalidated, created.TerminalID, "enter-shell-ready")

	e2eTypeText(t, model, "ls >/dev/null; printf 'key_enter_ls_ok\\n'")
	e2eDispatchKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	e2eWaitForText(t, ctx, model, invalidated, "key_enter_ls_ok")
	e2eWaitForTitle(t, ctx, model, invalidated, created.TerminalID, "enter-shell-ready")
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
	for i := 0; i < len(items)-1; i++ {
		_, _ = model.Update(input.SemanticAction{Kind: input.ActionPickerDown})
	}
	pane := model.workbench.ActivePane()
	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: pane.ID})
	e2eDrain(t, model, cmd)
	paneID := model.modalHost.Prompt.PaneID
	model.modalHost.Prompt.Value = "e2e-shell"
	model.modalHost.Prompt.Command = []string{"sh", "-c", "printf 'e2e_ready\\n'; tail -f /dev/null"}
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
	for i := 0; i < len(items)-1; i++ {
		_, _ = model.Update(input.SemanticAction{Kind: input.ActionPickerDown})
	}
	pane := model.workbench.ActivePane()
	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: pane.ID})
	e2eDrain(t, model, cmd)
	paneID := model.modalHost.Prompt.PaneID
	model.modalHost.Prompt.Value = "close-pane-shell"
	model.modalHost.Prompt.Command = []string{"sh", "-c", "printf 'close_pane_ready\\n'; tail -f /dev/null"}
	_, cmd = model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: paneID})
	e2eDrain(t, model, cmd)
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
	for i := 0; i < len(items)-1; i++ {
		_, _ = model.Update(input.SemanticAction{Kind: input.ActionPickerDown})
	}
	pane := model.workbench.ActivePane()
	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: pane.ID})
	e2eDrain(t, model, cmd)
	paneID := model.modalHost.Prompt.PaneID
	model.modalHost.Prompt.Value = "restore-empty-workspace"
	model.modalHost.Prompt.Command = []string{"sh", "-c", "printf 'restore_empty_workspace_ready\\n'; cat"}
	_, cmd = model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: paneID})
	e2eDrain(t, model, cmd)
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
	for i := 0; i < len(items)-1; i++ {
		_, _ = model.Update(input.SemanticAction{Kind: input.ActionPickerDown})
	}
	pane := model.workbench.ActivePane()
	_, cmd := model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: pane.ID})
	e2eDrain(t, model, cmd)
	paneID := model.modalHost.Prompt.PaneID
	model.modalHost.Prompt.Value = "restore-empty-tab"
	model.modalHost.Prompt.Command = []string{"sh", "-c", "printf 'restore_empty_tab_ready\\n'; cat"}
	_, cmd = model.Update(input.SemanticAction{Kind: input.ActionSubmitPrompt, PaneID: paneID})
	e2eDrain(t, model, cmd)
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

	next := e2eOverlayFooterActionRegion(t, model, input.ActionNextWorkspace)
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
	targetIndex := len(items) - 1
	if targetIndex < 0 || !items[targetIndex].CreateNew {
		t.Fatalf("expected create-new picker row, got %#v", items)
	}

	target := overlayPickerItemRegion(t, model, targetIndex)
	e2eMouseClickAt(t, model, target.Rect.X, e2eScreenYForBodyY(model, target.Rect.Y))

	if model.modalHost == nil || model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "create-terminal-name" {
		t.Fatalf("expected create-terminal-name prompt after mouse picker click, got %#v", model.modalHost)
	}

	inputRegion := e2eOverlayRegionByKind(t, model, render.HitRegionPromptInput)
	e2eMouseClickAt(t, model, inputRegion.Rect.X, e2eScreenYForBodyY(model, inputRegion.Rect.Y))
	e2eTypeText(t, model, "mouse-e2e-shell")
	model.modalHost.Prompt.Command = []string{"sh", "-c", "printf 'mouse_e2e_ready\\n'; cat"}

	submit := e2eOverlayRegionByKind(t, model, render.HitRegionPromptSubmit)
	e2eMouseClickAt(t, model, submit.Rect.X, e2eScreenYForBodyY(model, submit.Rect.Y))

	if model.modalHost.Prompt == nil || model.modalHost.Prompt.Kind != "create-terminal-tags" {
		t.Fatalf("expected tags prompt after mouse submit, got %#v", model.modalHost.Prompt)
	}

	submit = e2eOverlayRegionByKind(t, model, render.HitRegionPromptSubmit)
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

type realMouseE2EEnv struct {
	ctx         context.Context
	model       *Model
	invalidated chan struct{}
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

func e2eCreateTerminalViaMouse(t *testing.T, env realMouseE2EEnv, name, readyMarker string) (string, string) {
	t.Helper()
	model := env.model
	items := model.modalHost.Picker.VisibleItems()
	targetIndex := len(items) - 1
	if targetIndex < 0 || !items[targetIndex].CreateNew {
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
	model.modalHost.Prompt.Command = []string{"sh", "-c", fmt.Sprintf("printf '%s\\n'; cat", readyMarker)}

	submit := e2eOverlayRegionByKind(t, model, render.HitRegionPromptSubmit)
	e2eMouseClickAt(t, model, submit.Rect.X, e2eScreenYForBodyY(model, submit.Rect.Y))
	submit = e2eOverlayRegionByKind(t, model, render.HitRegionPromptSubmit)
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
		conn, err := net.Dial("unix", path)
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
