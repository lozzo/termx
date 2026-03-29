package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	localvterm "github.com/lozzow/termx/vterm"
	"golang.org/x/text/unicode/norm"
)

func activatePrefixForTest(model *Model) tea.Cmd {
	if model == nil {
		return nil
	}
	return model.activatePrefix()
}

func TestTerminalLocationsUsesWorkbenchBackedWorkspaceState(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.workspace = Workspace{
		Name: "main",
		Tabs: []*Tab{{
			Name: "1",
			Panes: map[string]*Pane{
				"p1": {ID: "p1", Terminal: &Terminal{ID: "term-1"}, Viewport: &Viewport{TerminalID: "term-1"}},
			},
		}},
		ActiveTab: 0,
	}
	model.snapshotCurrentWorkspace()

	locations := model.terminalLocations()
	if len(locations["term-1"]) == 0 {
		t.Fatal("expected workbench-backed terminal location")
	}
}

func TestOpenTerminalPickerCmdUsesAppWorkspaceSync(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.workspace = Workspace{
		Name:      "main",
		Tabs:      []*Tab{{Name: "1"}, {Name: "2", Panes: map[string]*Pane{"p2": {ID: "p2", Viewport: &Viewport{}}}, ActivePaneID: "p2"}},
		ActiveTab: 1,
	}
	if model.workbench == nil {
		t.Fatal("expected workbench")
	}

	_ = model.openTerminalPickerCmd()

	if model.workbench.CurrentWorkspace().ActiveTab != 1 {
		t.Fatalf("expected picker open to sync active tab 1, got %d", model.workbench.CurrentWorkspace().ActiveTab)
	}
}

func TestPaneCanReferenceSharedTerminalObject(t *testing.T) {
	store := NewTerminalStore()
	terminal := store.GetOrCreate("term-1")
	terminal.SetMetadata("shared", []string{"bash"}, map[string]string{"role": "dev"})

	first := &Pane{ID: "pane-1", Terminal: terminal, Viewport: &Viewport{}}
	second := &Pane{ID: "pane-2", Terminal: terminal, Viewport: &Viewport{}}

	terminal.Name = "renamed"

	if first.Terminal.Name != "renamed" || second.Terminal.Name != "renamed" {
		t.Fatal("expected panes to share terminal object reference")
	}
}

func TestPaneTitleCanReadFromTerminalObject(t *testing.T) {
	terminal := &Terminal{ID: "term-1", Name: "worker", Command: []string{"tail", "-f", "worker.log"}}
	pane := &Pane{ID: "pane-1", Terminal: terminal, Viewport: &Viewport{TerminalID: "term-1"}}

	if got := paneTitle(pane); got == "" {
		t.Fatal("expected pane title from terminal-backed pane")
	}
}

func TestTerminalPickerLocationsCanUseSharedTerminalMetadata(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	terminal := model.terminalStore.GetOrCreate("term-1")
	terminal.SetMetadata("worker", []string{"bash"}, map[string]string{"role": "dev"})
	pane := &Pane{ID: "pane-1", Title: "worker", Terminal: terminal, Viewport: &Viewport{TerminalID: "term-1"}}
	tab := &Tab{Name: "1", Panes: map[string]*Pane{pane.ID: pane}, ActivePaneID: pane.ID}
	model.workspace = Workspace{Name: "main", Tabs: []*Tab{tab}, ActiveTab: 0}
	model.workspaceStore = map[string]Workspace{"main": model.workspace}
	model.workspaceOrder = []string{"main"}
	model.activeWorkspace = 0

	locations := model.terminalLocations()
	if len(locations["term-1"]) == 0 {
		t.Fatal("expected location for terminal-backed pane")
	}
}

func TestModelNewWorkbenchStartsFromOwnedWorkspaceCopy(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh", Workspace: "main"})

	if model.workbench == nil {
		t.Fatal("expected model to initialize workbench")
	}
	owned := model.workbench.Current()
	if owned == nil {
		t.Fatal("expected workbench current workspace")
	}
	if &model.workspace == owned {
		t.Fatal("expected model workspace to avoid aliasing workbench root")
	}
	if len(model.workspace.Tabs) != 1 || len(owned.Tabs) != 1 {
		t.Fatalf("expected both views to start with one tab, got model=%d workbench=%d", len(model.workspace.Tabs), len(owned.Tabs))
	}
	if model.workspace.Tabs[0] == owned.Tabs[0] {
		t.Fatal("expected phase-1 model workspace to keep its own tab pointer, not workbench-owned tab state")
	}
}

func TestModelPrefixActions(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	if cmd := model.Init(); cmd == nil {
		t.Fatal("expected init command")
	}
	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	if len(model.workspace.Tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(model.workspace.Tabs))
	}
	tab := model.currentTab()
	if len(tab.Panes) != 1 {
		t.Fatalf("expected 1 pane, got %d", len(tab.Panes))
	}

	createSplitPaneViaPicker(t, model, SplitVertical)

	if len(model.currentTab().Panes) != 2 {
		t.Fatalf("expected split to create pane, got %d", len(model.currentTab().Panes))
	}

	createNewTabViaPicker(t, model)

	if len(model.workspace.Tabs) != 2 {
		t.Fatalf("expected new tab, got %d", len(model.workspace.Tabs))
	}
}

func TestModelViewShowsWelcomeAndHelp(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	view := model.View()
	if !containsAll(view, "termx", "Ctrl-p", "Ctrl-t", "Ctrl-g", "shortcut") {
		t.Fatalf("welcome view missing expected hints:\n%s", view)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	helpView := model.View()
	if !containsAll(helpView, "Help / Shortcut Map", "Most used", "Ctrl-p   pane actions", "Shared terminal", "take ownership before changing PTY size", "Exit", "close current mode/modal") {
		t.Fatalf("help overlay missing expected content:\n%s", helpView)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.showHelp {
		t.Fatal("expected esc to close help overlay")
	}
}

func TestInitWithStartupPickerShowsTerminalChooser(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "worker", Command: []string{"tail", "-f", "worker.log"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", StartupPicker: true})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	if model.terminalPicker == nil {
		t.Fatal("expected startup picker to open")
	}
	if model.terminalPicker.Title != "Choose Terminal" {
		t.Fatalf("expected startup picker title, got %q", model.terminalPicker.Title)
	}
	if len(model.currentTab().Panes) != 0 {
		t.Fatalf("expected startup picker to avoid creating pane immediately, got %d panes", len(model.currentTab().Panes))
	}

	view := model.View()
	if idx := lineIndexContaining(view, "┌ Choose Terminal"); idx < 8 {
		t.Fatalf("expected startup picker modal to be centered, got:\n%s", xansi.Strip(view))
	}
	line := lineContaining(view, "┌ Choose Terminal")
	if !strings.Contains(line, "┐") {
		t.Fatalf("expected startup picker modal top border to close cleanly, got:\n%s", xansi.Strip(view))
	}
	if strings.Contains(line, "Ctrl-a") {
		t.Fatalf("expected startup picker modal line to be isolated from welcome copy, got:\n%s", xansi.Strip(view))
	}
	if xansi.StringWidth(strings.TrimSpace(line)) > 70 {
		t.Fatalf("expected startup picker modal to stay visually compact, got line width %d:\n%s", xansi.StringWidth(strings.TrimSpace(line)), xansi.Strip(view))
	}
	lines := strings.Split(xansi.Strip(view), "\n")
	for _, bodyLine := range lines[1 : len(lines)-1] {
		if xansi.StringWidth(bodyLine) != model.width {
			t.Fatalf("expected picker body line width %d, got %d in line %q", model.width, xansi.StringWidth(bodyLine), bodyLine)
		}
	}
}

func TestRenderContentBodyEmptyWorkspaceFillsFullWidth(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh", Workspace: "main"})
	model.width = 80
	model.height = 24
	model.workspace = Workspace{
		Name:      "empty-ws",
		Tabs:      []*Tab{{Name: "1"}},
		ActiveTab: 0,
	}

	body := model.renderContentBody()
	lines := strings.Split(body, "\n")
	if len(lines) != model.height-2 {
		t.Fatalf("expected %d content lines, got %d", model.height-2, len(lines))
	}
	for _, line := range lines {
		if xansi.StringWidth(line) != model.width {
			t.Fatalf("expected empty workspace body line width %d, got %d in line %q", model.width, xansi.StringWidth(line), line)
		}
	}
}

func TestInitWithStartupLayoutLoadsWorkspaceBeforeStartupPicker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	layoutDir := filepath.Join(home, ".config", "termx", "layouts")
	if err := os.MkdirAll(layoutDir, 0o755); err != nil {
		t.Fatalf("mkdir layout dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(layoutDir, "demo.yaml"), []byte(`
name: demo
tabs:
  - name: coding
    tiling:
      terminal:
        tag: "role=editor"
        command: "vim ."
`), 0o644); err != nil {
		t.Fatalf("write layout file: %v", err)
	}

	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{
				ID:      "shared-001",
				Name:    "editor",
				Command: []string{"vim", "."},
				State:   "running",
				Tags:    map[string]string{"role": "editor"},
			},
		},
	}
	model := NewModel(client, Config{
		DefaultShell:  "/bin/sh",
		StartupPicker: true,
		StartupLayout: "demo",
	})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	if model.terminalPicker != nil {
		t.Fatal("expected startup layout to bypass startup picker")
	}
	if model.workspace.Name != "demo" {
		t.Fatalf("expected startup layout workspace demo, got %q", model.workspace.Name)
	}
	tab := model.currentTab()
	if tab == nil || tab.Name != "coding" {
		t.Fatalf("expected startup layout tab coding, got %#v", tab)
	}
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil || pane.TerminalID != "shared-001" {
		t.Fatalf("expected startup layout to attach matched terminal, got %#v", pane)
	}
}

func TestStartupPickerCreateSelectionCreatesFirstPane(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", StartupPicker: true})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		msg = mustRunCmd(t, cmd)
		_, _ = model.Update(msg)
	}

	if model.terminalPicker == nil || len(model.terminalPicker.Filtered) != 1 || !model.terminalPicker.Filtered[0].CreateNew {
		t.Fatalf("expected startup picker to offer a single create row, got %#v", model.terminalPicker)
	}
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("expected startup create selection to open prompt before creation, got %#v", cmd)
	}
	if model.prompt == nil || model.prompt.Kind != "create-terminal-name" {
		t.Fatalf("expected startup create prompt, got %#v", model.prompt)
	}
}

func TestStartupPickerCanAttachExistingTerminalIntoFirstPane(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "worker", Command: []string{"tail", "-f", "worker.log"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", StartupPicker: true})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shared")})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	tab := model.currentTab()
	if len(tab.Panes) != 1 {
		t.Fatalf("expected startup attach selection to create one pane, got %d panes", len(tab.Panes))
	}
	if got := tab.Panes[tab.ActivePaneID].TerminalID; got != "shared-001" {
		t.Fatalf("expected startup attach selection to bind shared terminal, got %q", got)
	}
}

func TestInitWithAttachIDBootstrapsTUILayout(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", AttachID: "shared-001"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	if len(tab.Panes) != 1 {
		t.Fatalf("expected attach init to create one pane, got %d panes", len(tab.Panes))
	}
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil || pane.TerminalID != "shared-001" {
		t.Fatalf("expected attach init to bind shared terminal, got %#v", pane)
	}
}

func TestInitWithAttachIDUsesTerminalMetadataWhenAvailable(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "worker", Command: []string{"tail", "-f", "worker.log"}, State: "running", Tags: map[string]string{"role": "build"}},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", AttachID: "shared-001"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected attach init to create pane")
	}
	if pane.Title != "worker" {
		t.Fatalf("expected attach init to reuse terminal name, got %q", pane.Title)
	}
	if got := pane.Command; len(got) != 3 || got[0] != "tail" {
		t.Fatalf("expected attach init to populate command metadata, got %#v", got)
	}
	if pane.Tags["role"] != "build" {
		t.Fatalf("expected attach init to populate tags, got %#v", pane.Tags)
	}
}

func TestStartupPickerListTimeoutSurfacesError(t *testing.T) {
	client := &fakeClient{listDelay: 50 * time.Millisecond}
	model := NewModel(client, Config{
		DefaultShell:   "/bin/sh",
		StartupPicker:  true,
		RequestTimeout: 5 * time.Millisecond,
	})

	start := time.Now()
	msg := mustRunCmd(t, model.Init())
	if time.Since(start) > 40*time.Millisecond {
		t.Fatalf("expected list timeout quickly, took %s", time.Since(start))
	}
	_, _ = model.Update(msg)

	if model.err == nil {
		t.Fatal("expected startup picker timeout to set error")
	}
	if !strings.Contains(model.err.Error(), "list terminals timed out") {
		t.Fatalf("expected friendly list timeout, got %v", model.err)
	}
}

func TestAttachInitTimeoutSurfacesError(t *testing.T) {
	client := &fakeClient{attachDelay: 50 * time.Millisecond}
	model := NewModel(client, Config{
		DefaultShell:   "/bin/sh",
		AttachID:       "shared-001",
		RequestTimeout: 5 * time.Millisecond,
	})

	start := time.Now()
	msg := mustRunCmd(t, model.Init())
	if time.Since(start) > 40*time.Millisecond {
		t.Fatalf("expected attach timeout quickly, took %s", time.Since(start))
	}
	_, _ = model.Update(msg)

	if model.err == nil {
		t.Fatal("expected attach timeout to set error")
	}
	if !strings.Contains(model.err.Error(), "attach terminal timed out") {
		t.Fatalf("expected friendly attach timeout, got %v", model.err)
	}
}

func TestSendToActiveTimeoutSurfacesError(t *testing.T) {
	client := &fakeClient{inputDelay: 50 * time.Millisecond}
	model := NewModel(client, Config{
		DefaultShell:   "/bin/sh",
		RequestTimeout: 5 * time.Millisecond,
	})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	start := time.Now()
	msg = mustRunCmd(t, model.sendToActive([]byte("echo slow")))
	if time.Since(start) > 40*time.Millisecond {
		t.Fatalf("expected input timeout quickly, took %s", time.Since(start))
	}
	_, _ = model.Update(msg)

	if model.err == nil {
		t.Fatal("expected input timeout to set error")
	}
	if !strings.Contains(model.err.Error(), "send input timed out") {
		t.Fatalf("expected friendly input timeout, got %v", model.err)
	}
}

func TestSplitCommandOpensChooserAndCanAttachExistingTerminal(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "worker", Command: []string{"tail", "-f", "worker.log"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'%'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if model.terminalPicker == nil {
		t.Fatal("expected split command to open chooser")
	}
	if model.terminalPicker.Title != "Open Pane" {
		t.Fatalf("expected split chooser title, got %q", model.terminalPicker.Title)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shared")})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	tab := model.currentTab()
	if len(tab.Panes) != 2 {
		t.Fatalf("expected split chooser to create new pane, got %d panes", len(tab.Panes))
	}
	if got := tab.Panes[tab.ActivePaneID].TerminalID; got != "shared-001" {
		t.Fatalf("expected split chooser to attach shared terminal, got %q", got)
	}
}

func TestPickerSelectionBlocksInputUntilPaneArrives(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'%'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if model.terminalPicker == nil {
		t.Fatal("expected split chooser to open")
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		cmd = commitDefaultTerminalCreatePrompt(t, model)
	}
	if !model.inputBlocked {
		t.Fatal("expected picker selection to block pane input until pane creation completes")
	}
	_, blockedCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if blockedCmd != nil {
		t.Fatalf("expected blocked input to be ignored, got %#v", blockedCmd)
	}
	if len(client.inputs) != 0 {
		t.Fatalf("expected blocked input to avoid sending bytes, got %#v", client.inputs)
	}

	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)
	if model.inputBlocked {
		t.Fatal("expected pane creation to unblock input")
	}
}

func TestNewTabCommandOpensChooserAndCanAttachExistingTerminal(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "worker", Command: []string{"tail", "-f", "worker.log"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if model.terminalPicker == nil {
		t.Fatal("expected new tab command to open chooser")
	}
	if model.terminalPicker.Title != "Open Tab" {
		t.Fatalf("expected new tab chooser title, got %q", model.terminalPicker.Title)
	}
	if len(model.workspace.Tabs) != 1 {
		t.Fatalf("expected new tab chooser to defer tab creation, got %d tabs", len(model.workspace.Tabs))
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shared")})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		cmd = commitDefaultTerminalCreatePrompt(t, model)
	}
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	if len(model.workspace.Tabs) != 2 {
		t.Fatalf("expected attach selection to create new tab, got %d tabs", len(model.workspace.Tabs))
	}
	tab := model.currentTab()
	if len(tab.Panes) != 1 {
		t.Fatalf("expected new tab chooser to create one pane, got %d panes", len(tab.Panes))
	}
	if got := tab.Panes[tab.ActivePaneID].TerminalID; got != "shared-001" {
		t.Fatalf("expected new tab chooser to attach shared terminal, got %q", got)
	}
}

func TestNewTabCreateSelectionOpensTerminalCreatePrompt(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)
	createCallsBefore := client.createCalls

	_ = activatePrefixForTest(model)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("expected create selection to open prompt before creating terminal, got %#v", cmd)
	}
	if model.prompt == nil || model.prompt.Kind != "create-terminal-name" {
		t.Fatalf("expected create-terminal-name prompt, got %#v", model.prompt)
	}
	if client.createCalls != createCallsBefore {
		t.Fatalf("expected create to wait for prompt completion, got %d -> %d", createCallsBefore, client.createCalls)
	}
	if len(model.workspace.Tabs) != 1 {
		t.Fatalf("expected new tab creation to wait for prompt completion, got %d tabs", len(model.workspace.Tabs))
	}
}

func TestNewTabCreatePromptUsesCustomNameAndTags(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model.prompt.Value = "api-shell"
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("expected first prompt enter to advance to tags, got %#v", cmd)
	}
	if model.prompt == nil || model.prompt.Kind != "create-terminal-tags" {
		t.Fatalf("expected create-terminal-tags prompt, got %#v", model.prompt)
	}
	model.prompt.Value = "role=api team=infra"
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	if len(model.workspace.Tabs) != 2 {
		t.Fatalf("expected completed prompt flow to create new tab, got %d tabs", len(model.workspace.Tabs))
	}
	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected created pane")
	}
	if pane.Title != "api-shell" {
		t.Fatalf("expected created pane title api-shell, got %q", pane.Title)
	}
	if pane.Name != "api-shell" {
		t.Fatalf("expected created terminal name api-shell, got %q", pane.Name)
	}
	if got := pane.Tags["role"]; got != "api" {
		t.Fatalf("expected role tag api, got %#v", pane.Tags)
	}
	if got := pane.Tags["team"]; got != "infra" {
		t.Fatalf("expected team tag infra, got %#v", pane.Tags)
	}
	info := client.terminalByID[pane.TerminalID]
	if info.Name != "api-shell" {
		t.Fatalf("expected created server terminal name api-shell, got %#v", info)
	}
	if info.Tags["role"] != "api" || info.Tags["team"] != "infra" {
		t.Fatalf("expected created server tags to persist, got %#v", info.Tags)
	}
}

func TestTerminalCreatePromptUsesFriendlyDefaultName(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/zsh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.prompt == nil || model.prompt.Value != "shell-2" {
		t.Fatalf("expected friendly default terminal name shell-2, got %#v", model.prompt)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil || pane.Name != "shell-2" {
		t.Fatalf("expected friendly default name to persist, got %#v", pane)
	}
	if strings.Contains(pane.Title, pane.TerminalID) {
		t.Fatalf("expected pane title to hide random terminal id, got %q", pane.Title)
	}
}

func TestTerminalPickerCtrlEOpensTerminalMetadataPrompt(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{
				ID:      "shared-001",
				Name:    "worker-shell",
				Command: []string{"/bin/zsh"},
				Tags:    map[string]string{"role": "worker"},
				State:   "running",
			},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	msg = mustRunCmd(t, model.openTerminalPickerCmd())
	_, _ = model.Update(msg)
	if model.terminalPicker == nil {
		t.Fatal("expected terminal picker")
	}
	model.terminalPicker.Query = "worker"
	model.terminalPicker.applyFilter()

	_, cmd := model.Update(inputEventMsg{event: uv.KeyPressEvent{Code: 'e', Text: "e", Mod: uv.ModCtrl}})
	if cmd != nil {
		t.Fatalf("expected ctrl+e to open edit prompt without command, got %#v", cmd)
	}
	if model.terminalPicker != nil {
		t.Fatal("expected picker to close when editing metadata")
	}
	if model.prompt == nil || model.prompt.Kind != "edit-terminal-name" {
		t.Fatalf("expected edit-terminal-name prompt, got %#v", model.prompt)
	}
	if model.prompt.Value != "worker-shell" {
		t.Fatalf("expected existing terminal name in prompt, got %#v", model.prompt)
	}
}

func TestTerminalMetadataPromptUpdatesAttachedPanesAcrossWorkspacesWithoutRebinding(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	currentTab := model.currentTab()
	currentPane := currentTab.Panes[currentTab.ActivePaneID]
	currentPane.Name = "shell-1"
	currentPane.Title = "shell-1"
	currentPane.Command = []string{"/bin/zsh"}
	currentPane.Tags = map[string]string{"role": "dev"}

	otherTab := newTab("1")
	otherPane := &Pane{
		ID:    "pane-shared-other",
		Title: "shell-1",
		Viewport: &Viewport{
			TerminalID: currentPane.TerminalID,
			Name:       "shell-1",
			Command:    []string{"/bin/zsh"},
			Tags:       map[string]string{"role": "dev"},
			Mode:       ViewportModeFit,
		},
	}
	otherTab.Panes[otherPane.ID] = otherPane
	otherTab.ActivePaneID = otherPane.ID
	otherTab.Root = NewLeaf(otherPane.ID)
	model.workspaceStore["ops"] = Workspace{Name: "ops", Tabs: []*Tab{otherTab}, ActiveTab: 0}
	model.workspaceOrder = []string{"main", "ops"}

	model.beginTerminalEditPrompt(protocol.TerminalInfo{
		ID:      currentPane.TerminalID,
		Name:    currentPane.Name,
		Command: append([]string(nil), currentPane.Command...),
		Tags:    cloneStringMap(currentPane.Tags),
	})
	model.prompt.Value = "api-shell"
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("expected name step to advance to tags, got %#v", cmd)
	}
	if model.prompt == nil || model.prompt.Kind != "edit-terminal-tags" {
		t.Fatalf("expected edit-terminal-tags prompt, got %#v", model.prompt)
	}
	model.prompt.Value = "role=api team=infra"
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if got := currentPane.TerminalID; got == "" {
		t.Fatal("expected current pane to remain attached")
	}
	if got := currentPane.Title; got != "api-shell" {
		t.Fatalf("expected current pane title api-shell, got %q", got)
	}
	if got := currentPane.Tags["team"]; got != "infra" {
		t.Fatalf("expected current pane tags to update, got %#v", currentPane.Tags)
	}
	stored := model.workspaceStore["ops"]
	if len(stored.Tabs) != 1 || stored.Tabs[0] == nil {
		t.Fatalf("expected inactive workspace topology to stay intact, got %#v", stored)
	}
	updated := stored.Tabs[0].Panes[otherPane.ID]
	if updated == nil {
		t.Fatal("expected inactive workspace pane to remain present")
	}
	if updated.TerminalID != currentPane.TerminalID {
		t.Fatalf("expected inactive workspace pane to stay bound to %q, got %q", currentPane.TerminalID, updated.TerminalID)
	}
	if updated.Title != "api-shell" || updated.Tags["role"] != "api" || updated.Tags["team"] != "infra" {
		t.Fatalf("expected inactive workspace pane metadata to update in place, got %#v", updated)
	}
	info := client.terminalByID[currentPane.TerminalID]
	if info.Name != "api-shell" || info.Tags["role"] != "api" || info.Tags["team"] != "infra" {
		t.Fatalf("expected backend metadata update, got %#v", info)
	}
}

func TestTerminalMetadataPromptCanClearTags(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	pane.Name = "shell-1"
	pane.Command = []string{"/bin/zsh"}
	pane.Tags = map[string]string{"role": "dev", "team": "infra"}

	model.beginTerminalEditPrompt(protocol.TerminalInfo{
		ID:      pane.TerminalID,
		Name:    pane.Name,
		Command: append([]string(nil), pane.Command...),
		Tags:    cloneStringMap(pane.Tags),
	})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("expected name step to advance to tags, got %#v", cmd)
	}
	model.prompt.Value = ""
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if pane.Tags != nil {
		t.Fatalf("expected pane tags to clear, got %#v", pane.Tags)
	}
	info := client.terminalByID[pane.TerminalID]
	if len(info.Tags) != 0 {
		t.Fatalf("expected backend tags to clear, got %#v", info.Tags)
	}
}

func TestStatusStatePartsUseFriendlyPaneLabelAndMinimalRuntimeSummary(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", IconSet: "ascii"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	pane.Name = "api-shell"
	pane.Tags = map[string]string{"role": "api", "termx.size_lock": "warn"}
	pane.Title = "api-shell"
	clone := *pane
	clone.ID = "pane-shared"
	clone.Tags = cloneStringMap(pane.Tags)
	model.currentTab().Panes[clone.ID] = &clone

	status := strings.Join(model.statusStateParts(), " ")
	if !strings.Contains(status, "pane:api-shell") {
		t.Fatalf("expected friendly pane label in status, got %q", status)
	}
	if !containsAll(status, "layer:tiled", "state:running", "connection:owner") {
		t.Fatalf("expected minimal runtime summary in status, got %q", status)
	}
	if containsAll(status, "shared:2", "size-lock:warn") || strings.Contains(status, "display:") {
		t.Fatalf("expected pane relationship badges to stay in pane chrome, got %q", status)
	}
	if strings.Contains(status, pane.TerminalID) {
		t.Fatalf("expected status to avoid raw terminal id, got %q", status)
	}
}

func TestDirectModeShortcutKeyUsesSharedMapping(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})

	cmd := model.directModeCmdForKey(tea.KeyMsg{Type: tea.KeyCtrlO})
	if cmd == nil {
		t.Fatal("expected ctrl+o to enter floating mode")
	}
	if got := model.ActiveModeForTest(); got != "floating" {
		t.Fatalf("expected floating mode from shared key mapping, got %q", got)
	}
}

func TestDirectModeShortcutEventUsesSharedMapping(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})

	cmd := model.directModeCmdForEvent(uv.KeyPressEvent{Code: 'v', Text: "v", Mod: uv.ModCtrl})
	if cmd == nil {
		t.Fatal("expected ctrl+v to enter connection mode")
	}
	if got := model.ActiveModeForTest(); got != "connection" {
		t.Fatalf("expected connection mode from shared event mapping, got %q", got)
	}
}

func TestRootPrefixShortcutUsesSharedMapping(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})

	result := model.dispatchRootPrefixKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	if !result.keep || result.cmd != nil || result.state.kind != prefixStateTransitionEnter || result.state.mode != prefixModeWorkspace {
		t.Fatalf("expected root prefix w to return workspace enter transition, got %#v", result)
	}
	if got := model.ActiveModeForTest(); got != "" {
		t.Fatalf("expected root prefix dispatch to stay pure before apply, got %q", got)
	}
	_ = model.applyActivePrefixResult(result)
	if got := model.ActiveModeForTest(); got != "workspace" {
		t.Fatalf("expected workspace mode after applying shared root prefix mapping, got %q", got)
	}
}

func TestRootPrefixEventUsesSharedMapping(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})

	result := model.dispatchPrefixEvent(uv.KeyPressEvent{Code: 'o', Text: "o"})
	if !result.keep || result.cmd != nil || result.state.kind != prefixStateTransitionEnter || result.state.mode != prefixModeFloating {
		t.Fatalf("expected root event o to return floating enter transition, got %#v", result)
	}
	if got := model.ActiveModeForTest(); got != "" {
		t.Fatalf("expected root event dispatch to stay pure before apply, got %q", got)
	}
	_ = model.applyActivePrefixResult(result)
	if got := model.ActiveModeForTest(); got != "floating" {
		t.Fatalf("expected floating mode after applying shared root event mapping, got %q", got)
	}
}

func TestPrefixInputFromKeyAndEventShareCanonicalTokens(t *testing.T) {
	tests := []struct {
		name  string
		key   tea.KeyMsg
		event uv.KeyPressEvent
		want  string
	}{
		{
			name:  "esc",
			key:   tea.KeyMsg{Type: tea.KeyEsc},
			event: uv.KeyPressEvent{Code: uv.KeyEsc},
			want:  "esc",
		},
		{
			name:  "tab",
			key:   tea.KeyMsg{Type: tea.KeyTab},
			event: uv.KeyPressEvent{Code: uv.KeyTab},
			want:  "tab",
		},
		{
			name:  "comma",
			key:   tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}},
			event: uv.KeyPressEvent{Code: ',', Text: ","},
			want:  ",",
		},
		{
			name:  "digit",
			key:   tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}},
			event: uv.KeyPressEvent{Code: '2', Text: "2"},
			want:  "2",
		},
		{
			name:  "question",
			key:   tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}},
			event: uv.KeyPressEvent{Code: '?', Text: "?"},
			want:  "?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := prefixInputFromKey(tt.key).token; got != tt.want {
				t.Fatalf("expected key token %q, got %q", tt.want, got)
			}
			if got := prefixInputFromEvent(tt.event).token; got != tt.want {
				t.Fatalf("expected event token %q, got %q", tt.want, got)
			}
		})
	}
}

func TestPrefixInputFromKeyAndEventShareDirectionalTokens(t *testing.T) {
	tests := []struct {
		name  string
		key   tea.KeyMsg
		event uv.KeyPressEvent
		want  string
	}{
		{
			name:  "left",
			key:   tea.KeyMsg{Type: tea.KeyLeft},
			event: uv.KeyPressEvent{Code: uv.KeyLeft},
			want:  "left",
		},
		{
			name:  "right",
			key:   tea.KeyMsg{Type: tea.KeyRight},
			event: uv.KeyPressEvent{Code: uv.KeyRight},
			want:  "right",
		},
		{
			name:  "space",
			key:   tea.KeyMsg{Type: tea.KeySpace},
			event: uv.KeyPressEvent{Code: ' ', Text: " "},
			want:  "space",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := prefixInputFromKey(tt.key).token; got != tt.want {
				t.Fatalf("expected key token %q, got %q", tt.want, got)
			}
			if got := prefixInputFromEvent(tt.event).token; got != tt.want {
				t.Fatalf("expected event token %q, got %q", tt.want, got)
			}
		})
	}
}

func TestTabWorkspaceGlobalActionsUseCanonicalPrefixInput(t *testing.T) {
	if got := tabModeActionForInput(prefixInput{token: ","}, false); got.kind != tabModeActionRename {
		t.Fatalf("expected canonical tab input to map rename, got %#v", got)
	}
	if got := tabModeActionForInput(prefixInput{token: "2"}, true); got.kind != tabModeActionJump || got.index != 1 {
		t.Fatalf("expected canonical direct tab digit to jump to second tab, got %#v", got)
	}
	if got := workspaceModeActionForInput(prefixInput{token: "r"}); got.kind != workspaceModeActionRename {
		t.Fatalf("expected canonical workspace input to map rename, got %#v", got)
	}
	if got := globalModeActionForInput(prefixInput{token: "?"}); got.kind != globalModeActionHelp {
		t.Fatalf("expected canonical global input to map help, got %#v", got)
	}
}

func TestResizeAndOffsetActionsUseCanonicalPrefixInput(t *testing.T) {
	if got := resizeModeActionForInput(prefixInput{token: "left"}); got.kind != resizeModeActionResize || got.direction != DirectionLeft || got.amount != 2 {
		t.Fatalf("expected canonical resize left input, got %#v", got)
	}
	if got := resizeModeActionForInput(prefixInput{token: "L"}); got.kind != resizeModeActionResize || got.direction != DirectionRight || got.amount != 4 {
		t.Fatalf("expected canonical resize L input, got %#v", got)
	}
	if got := resizeModeActionForInput(prefixInput{token: "space"}); got.kind != resizeModeActionCycleLayout {
		t.Fatalf("expected canonical resize space input, got %#v", got)
	}
	if got := offsetPanModeActionForInput(prefixInput{token: "right"}); got.kind != offsetPanModeActionPan || got.direction != DirectionRight {
		t.Fatalf("expected canonical offset right input, got %#v", got)
	}
	if got := offsetPanModeActionForInput(prefixInput{token: "G"}); got.kind != offsetPanModeActionJumpBottom {
		t.Fatalf("expected canonical offset G input, got %#v", got)
	}
}

func TestViewportAndFloatingActionsUseCanonicalPrefixInput(t *testing.T) {
	if got := viewportModeActionForInput(prefixInput{token: "left"}, false); got.kind != viewportModeActionPan || got.direction != DirectionLeft {
		t.Fatalf("expected canonical viewport left input, got %#v", got)
	}
	if got := viewportModeActionForInput(prefixInput{token: "a"}, false); got.kind != viewportModeActionAcquire {
		t.Fatalf("expected canonical viewport acquire input, got %#v", got)
	}
	if got := viewportModeActionForInput(prefixInput{token: "o"}, false); got.kind != viewportModeActionOffsetMode {
		t.Fatalf("expected canonical viewport offset-mode input, got %#v", got)
	}
	if got := floatingModeActionForInput(prefixInput{token: "tab"}); got.kind != floatingModeActionFocusNext {
		t.Fatalf("expected canonical floating tab input, got %#v", got)
	}
	if got := floatingModeActionForInput(prefixInput{token: "left"}); got.kind != floatingModeActionMove || got.direction != DirectionLeft {
		t.Fatalf("expected canonical floating left input, got %#v", got)
	}
	if got := floatingModeActionForInput(prefixInput{token: "L"}); got.kind != floatingModeActionResize || got.direction != DirectionRight || got.amount != 4 {
		t.Fatalf("expected canonical floating L input, got %#v", got)
	}
}

func TestPrefixInputFromKeyAndEventShareCtrlDirectionalTokens(t *testing.T) {
	tests := []struct {
		name  string
		key   tea.KeyMsg
		event uv.KeyPressEvent
		want  string
	}{
		{
			name:  "ctrl-left",
			key:   tea.KeyMsg{Type: tea.KeyCtrlLeft},
			event: uv.KeyPressEvent{Code: uv.KeyLeft, Mod: uv.ModCtrl},
			want:  "ctrl+left",
		},
		{
			name:  "ctrl-h",
			key:   tea.KeyMsg{Type: tea.KeyCtrlH},
			event: uv.KeyPressEvent{Code: 'h', Text: "h", Mod: uv.ModCtrl},
			want:  "ctrl+h",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := prefixInputFromKey(tt.key).token; got != tt.want {
				t.Fatalf("expected key token %q, got %q", tt.want, got)
			}
			if got := prefixInputFromEvent(tt.event).token; got != tt.want {
				t.Fatalf("expected event token %q, got %q", tt.want, got)
			}
		})
	}
}

func TestPrefixInputFromEventPreservesAltShiftModifiers(t *testing.T) {
	input := prefixInputFromEvent(uv.KeyPressEvent{Code: 'H', Text: "H", Mod: uv.ModAlt | uv.ModShift})
	if input.token != "H" {
		t.Fatalf("expected token H, got %q", input.token)
	}
	if !input.alt || !input.shift {
		t.Fatalf("expected alt+shift modifiers to be preserved, got %+v", input)
	}
}

func TestPaneActionsUseCanonicalPrefixInput(t *testing.T) {
	if got := panePrefixActionForInput(prefixInput{token: "left"}); got.kind != panePrefixActionFocus || got.direction != DirectionLeft {
		t.Fatalf("expected canonical pane left input, got %#v", got)
	}
	if got := panePrefixActionForInput(prefixInput{token: "ctrl+h"}); got.kind != panePrefixActionViewportPan || got.offset != -4 {
		t.Fatalf("expected canonical pane ctrl+h input, got %#v", got)
	}
	if got := panePrefixActionForInput(prefixInput{token: "space"}); got.kind != panePrefixActionCycleLayout {
		t.Fatalf("expected canonical pane space input, got %#v", got)
	}
	if got := panePrefixActionForInput(prefixInput{token: "tab"}); got.kind != panePrefixActionCycleFloatingFocus {
		t.Fatalf("expected canonical pane tab input, got %#v", got)
	}
	if got := panePrefixActionForInput(prefixInput{token: "4"}); got.kind != panePrefixActionJumpTab || got.index != 3 {
		t.Fatalf("expected canonical pane tab-jump input, got %#v", got)
	}
}

func TestAltFloatingActionsUseCanonicalPrefixInput(t *testing.T) {
	move, ok := floatingAltActionForInput(prefixInput{token: "left", alt: true})
	if !ok || move.kind != floatingModeActionMove || move.direction != DirectionLeft {
		t.Fatalf("expected alt-left to map floating move left, got ok=%v action=%#v", ok, move)
	}

	resize, ok := floatingAltActionForInput(prefixInput{token: "H", alt: true, shift: true})
	if !ok || resize.kind != floatingModeActionResize || resize.direction != DirectionLeft || resize.amount != 4 {
		t.Fatalf("expected alt-shift-H to map floating resize left, got ok=%v action=%#v", ok, resize)
	}

	if action, ok := floatingAltActionForInput(prefixInput{token: "left"}); ok || action.kind != floatingModeActionNone {
		t.Fatalf("expected plain left not to map alt floating action, got ok=%v action=%#v", ok, action)
	}
}

func TestDispatchPrefixInputHandlesRootShortcutTransition(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})

	result := model.dispatchPrefixInput(prefixInput{token: "w"})
	if !result.keep || result.cmd != nil || result.state.kind != prefixStateTransitionEnter || result.state.mode != prefixModeWorkspace {
		t.Fatalf("expected canonical root input to return workspace enter transition, got %#v", result)
	}
}

func TestDispatchPrefixInputHandlesPaneDirectionalKeep(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)
	model.prefixActive = true
	model.prefixMode = prefixModePane

	result := model.dispatchPrefixInput(prefixInput{token: "left"})
	if !result.keep || !result.rearm {
		t.Fatalf("expected canonical pane directional input to keep and rearm, got %#v", result)
	}
}

func TestDispatchRootPrefixInputUnknownClearsPrefix(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	result := model.dispatchRootPrefixInput(prefixInput{token: "q"})
	if result.keep || result.rearm || result.cmd != nil {
		t.Fatalf("expected unknown root prefix input to stay one-shot, got %#v", result)
	}
}

func TestPrefixIntentForInputCapturesModeAndDirectState(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.prefixMode = prefixModeTab
	model.directMode = true

	intent := model.prefixIntentForInput(prefixInput{token: "2"})
	if intent.mode != prefixModeTab || !intent.direct || intent.input.token != "2" {
		t.Fatalf("expected prefix intent to capture mode/direct/input, got %#v", intent)
	}
}

func TestDispatchPrefixIntentUsesCapturedDirectState(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)
	model.workspace.Tabs = append(model.workspace.Tabs, newTab("two"))

	result := model.dispatchPrefixIntent(prefixIntent{
		mode:   prefixModeTab,
		direct: true,
		input:  prefixInput{token: "2"},
	})
	if model.workspace.ActiveTab != 1 {
		t.Fatalf("expected direct tab intent to jump to second tab, got %d", model.workspace.ActiveTab)
	}
	if result.keep {
		t.Fatalf("expected direct tab intent jump to be one-shot, got %#v", result)
	}
}

func TestPrefixRuntimePlanForResultPrefersExplicitTransition(t *testing.T) {
	plan := prefixRuntimePlanForResult(prefixDispatchResult{
		cmd:   tea.Quit,
		keep:  true,
		rearm: true,
		state: prefixStateTransition{kind: prefixStateTransitionEnter, mode: prefixModeWorkspace},
	})
	if plan.clear {
		t.Fatalf("expected explicit transition to win over clear, got %#v", plan)
	}
	if plan.rearm {
		t.Fatalf("expected explicit transition not to request standalone rearm, got %#v", plan)
	}
	if plan.transition.kind != prefixStateTransitionEnter || plan.transition.mode != prefixModeWorkspace {
		t.Fatalf("expected enter transition plan, got %#v", plan)
	}
	if plan.cmd == nil {
		t.Fatal("expected runtime plan to keep original command")
	}
}

func TestPrefixRuntimePlanForResultClearsWhenOneShotWithoutTransition(t *testing.T) {
	plan := prefixRuntimePlanForResult(prefixDispatchResult{keep: false})
	if !plan.clear {
		t.Fatalf("expected one-shot result to clear prefix state, got %#v", plan)
	}
	if plan.transition.kind != prefixStateTransitionNone {
		t.Fatalf("expected no explicit transition for one-shot clear, got %#v", plan)
	}
}

func TestPrefixRuntimePlanForResultRearmsStickyResult(t *testing.T) {
	plan := prefixRuntimePlanForResult(prefixDispatchResult{keep: true, rearm: true})
	if plan.clear {
		t.Fatalf("expected sticky result not to clear prefix state, got %#v", plan)
	}
	if !plan.rearm {
		t.Fatalf("expected sticky result to request rearm, got %#v", plan)
	}
	if plan.transition.kind != prefixStateTransitionNone {
		t.Fatalf("expected sticky result without explicit transition, got %#v", plan)
	}
}

func TestPaneModeKeyAndEventShareKeepBehaviorForDirectionalInput(t *testing.T) {
	newModel := func() *Model {
		model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
		msg := mustRunCmd(t, model.Init())
		_, _ = model.Update(msg)
		model.prefixActive = true
		model.prefixMode = prefixModePane
		return model
	}

	keyModel := newModel()
	keyResult := keyModel.dispatchPaneModeKey(tea.KeyMsg{Type: tea.KeyLeft})
	if !keyResult.keep || !keyResult.rearm {
		t.Fatalf("expected key pane directional input to keep and rearm, got %#v", keyResult)
	}

	eventModel := newModel()
	eventResult := eventModel.dispatchPaneModeEvent(uv.KeyPressEvent{Code: uv.KeyLeft})
	if !eventResult.keep || !eventResult.rearm {
		t.Fatalf("expected event pane directional input to keep and rearm, got %#v", eventResult)
	}
}

func TestDispatchPaneModeInputUnknownKeepsOnlyInDirectMode(t *testing.T) {
	newModel := func() *Model {
		model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
		msg := mustRunCmd(t, model.Init())
		_, _ = model.Update(msg)
		model.prefixActive = true
		model.prefixMode = prefixModePane
		return model
	}

	normalModel := newModel()
	normalResult := normalModel.dispatchPaneModeInput(prefixInput{token: "q"})
	if normalResult.keep || normalResult.rearm || normalResult.cmd != nil {
		t.Fatalf("expected unknown normal pane input to clear mode, got %#v", normalResult)
	}

	directModel := newModel()
	directModel.directMode = true
	directResult := directModel.dispatchPaneModeInput(prefixInput{token: "q"})
	if !directResult.keep || directResult.rearm || directResult.cmd != nil {
		t.Fatalf("expected unknown direct pane input to keep mode without rearm, got %#v", directResult)
	}
}

func TestExitedPaneShortcutKeyUsesSharedHandler(t *testing.T) {
	exitCode := 0
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	pane := &Pane{
		ID:    "pane-1",
		Title: "shell",
		Viewport: &Viewport{
			TerminalID:    "term-1",
			Channel:       1,
			TerminalState: "exited",
			ExitCode:      &exitCode,
		},
	}
	model.workspace.Tabs = []*Tab{{Name: "1", Panes: map[string]*Pane{pane.ID: pane}, ActivePaneID: pane.ID}}

	cmd := model.handleExitedPaneKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil {
		t.Fatal("expected exited key shortcut to restart pane")
	}
}

func TestExitedPaneShortcutEventUsesSharedHandler(t *testing.T) {
	exitCode := 0
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	pane := &Pane{
		ID:    "pane-1",
		Title: "shell",
		Viewport: &Viewport{
			TerminalID:    "term-1",
			Channel:       1,
			TerminalState: "exited",
			ExitCode:      &exitCode,
		},
	}
	model.workspace.Tabs = []*Tab{{Name: "1", Panes: map[string]*Pane{pane.ID: pane}, ActivePaneID: pane.ID}}

	cmd := model.handleExitedPaneEvent(uv.KeyPressEvent{Code: 'r', Text: "r"})
	if cmd == nil {
		t.Fatal("expected exited event shortcut to restart pane")
	}
}

func TestTerminalPickerLineBodyPrefersFriendlyNameOverID(t *testing.T) {
	item := terminalPickerItem{
		Info: protocol.TerminalInfo{
			ID:        "ab12cd34",
			Name:      "api-shell",
			Command:   []string{"/bin/zsh"},
			State:     "running",
			CreatedAt: time.Now().Add(-2 * time.Minute),
			Tags:      map[string]string{"role": "api"},
		},
	}

	line := item.lineBodyValue(time.Now())
	if !strings.Contains(line, "api-shell") {
		t.Fatalf("expected picker line to use friendly name, got %q", line)
	}
	if strings.Contains(line, "ab12cd34") {
		t.Fatalf("expected picker line to hide raw terminal id, got %q", line)
	}
	if !strings.Contains(line, "role=api") {
		t.Fatalf("expected picker line to keep tags visible, got %q", line)
	}
}

func TestNewTabAttachSeedsVTermFromSnapshotBeforeIncrementalOutput(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "htop", Command: []string{"htop"}, State: "running"},
		},
		snapshotByID: map[string]*protocol.Snapshot{"shared-001": altScreenSnapshotForTest("shared-001")},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("htop")})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected attached pane")
	}
	if pane.VTerm == nil {
		t.Fatal("expected attached pane VTerm")
	}

	assertAltScreenPaneRetainsSnapshotAfterIncrementalOutput(t, model, pane)
}

func TestSplitAttachSeedsVTermFromSnapshotBeforeIncrementalOutput(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "htop", Command: []string{"htop"}, State: "running"},
		},
		snapshotByID: map[string]*protocol.Snapshot{"shared-001": altScreenSnapshotForTest("shared-001")},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'%'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("htop")})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := activePane(model.currentTab())
	if pane == nil || pane.VTerm == nil {
		t.Fatalf("expected split-attached pane runtime, got %#v", pane)
	}
	assertAltScreenPaneRetainsSnapshotAfterIncrementalOutput(t, model, pane)
}

func TestFloatingAttachSeedsVTermFromSnapshotBeforeIncrementalOutput(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "htop", Command: []string{"htop"}, State: "running"},
		},
		snapshotByID: map[string]*protocol.Snapshot{"shared-001": altScreenSnapshotForTest("shared-001")},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cmd != nil {
		msg = mustRunCmd(t, cmd)
		_, _ = model.Update(msg)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("htop")})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	pane := activePane(model.currentTab())
	if pane == nil || pane.VTerm == nil {
		t.Fatalf("expected floating-attached pane runtime, got %#v", pane)
	}
	assertAltScreenPaneRetainsSnapshotAfterIncrementalOutput(t, model, pane)
}

func altScreenSnapshotForTest(terminalID string) *protocol.Snapshot {
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: 10, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{
				stringToProtocolRow("CPU 99%"),
				stringToProtocolRow("Mem 1.2G"),
				stringToProtocolRow("Tasks 42"),
				stringToProtocolRow("Load 1.0"),
			},
		},
		Cursor: protocol.CursorState{Row: 0, Col: 0, Visible: true},
		Modes:  protocol.TerminalModes{AlternateScreen: true, AutoWrap: true},
	}
}

func assertAltScreenPaneRetainsSnapshotAfterIncrementalOutput(t *testing.T, model *Model, pane *Pane) {
	t.Helper()
	cmd := model.handlePaneOutput(paneOutputMsg{
		paneID: pane.ID,
		frame: protocol.StreamFrame{
			Type:    protocol.TypeOutput,
			Payload: []byte("\x1b[H!"),
		},
	})
	if cmd != nil {
		t.Fatalf("expected incremental output write to stay in fast path, got recovery cmd %#v", cmd)
	}

	screen := pane.VTerm.ScreenContent()
	if got := vtermRowString(screen.Cells[0]); !strings.Contains(got, "!PU 99%") {
		t.Fatalf("expected first row to retain snapshot baseline after incremental output, got %q", got)
	}
	if got := vtermRowString(screen.Cells[1]); !strings.Contains(got, "Mem 1.2G") {
		t.Fatalf("expected second row to retain snapshot baseline after incremental output, got %q", got)
	}
	if !pane.VTerm.IsAltScreen() {
		t.Fatal("expected attached VTerm to preserve alternate screen mode from snapshot")
	}
}

func TestNewTabCommandCancelKeepsCurrentTab(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	originalTab := model.workspace.ActiveTab

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if len(model.workspace.Tabs) != 1 {
		t.Fatalf("expected chooser open to avoid creating tab eagerly, got %d tabs", len(model.workspace.Tabs))
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatal("expected cancel to be synchronous")
	}
	if model.terminalPicker != nil {
		t.Fatal("expected esc to close new tab chooser")
	}
	if len(model.workspace.Tabs) != 1 {
		t.Fatalf("expected cancel to keep tab count unchanged, got %d tabs", len(model.workspace.Tabs))
	}
	if model.workspace.ActiveTab != originalTab {
		t.Fatalf("expected cancel to keep current tab %d, got %d", originalTab, model.workspace.ActiveTab)
	}
}

func TestSplitCommandCancelKeepsCurrentLayout(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	originalPaneID := model.currentTab().ActivePaneID

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'%'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatal("expected cancel to be synchronous")
	}
	if model.terminalPicker != nil {
		t.Fatal("expected esc to close split chooser")
	}
	tab := model.currentTab()
	if len(tab.Panes) != 1 {
		t.Fatalf("expected cancel to keep pane count unchanged, got %d panes", len(tab.Panes))
	}
	if tab.ActivePaneID != originalPaneID {
		t.Fatalf("expected cancel to keep active pane %q, got %q", originalPaneID, tab.ActivePaneID)
	}
}

func TestPrefixWCreatesFloatingViewportInFixedMode(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	tab := model.currentTab()
	originalActive := tab.ActivePaneID

	createFloatingPaneViaPicker(t, model)

	if len(tab.Floating) != 1 {
		t.Fatalf("expected 1 floating viewport, got %d", len(tab.Floating))
	}
	if tab.ActivePaneID == originalActive {
		t.Fatal("expected floating viewport to become active")
	}
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active floating pane")
	}
	if pane.Mode != ViewportModeFixed {
		t.Fatalf("expected floating viewport to default to fixed mode, got %q", pane.Mode)
	}
	if !tab.FloatingVisible {
		t.Fatal("expected floating layer to remain visible after create")
	}
}

func TestFloatingViewportCanBeHiddenWithPrefixWUpper(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	tab := model.currentTab()
	basePane := tab.Panes[tab.ActivePaneID]
	if basePane == nil {
		t.Fatal("expected base pane")
	}
	basePane.live = true
	basePane.MarkRenderDirty()
	_, _ = basePane.VTerm.Write([]byte("TILED-BASE"))

	createFloatingPaneViaPicker(t, model)

	if len(tab.Floating) != 1 {
		t.Fatalf("expected 1 floating viewport, got %d", len(tab.Floating))
	}
	floating := tab.Floating[0]
	floatPane := tab.Panes[floating.PaneID]
	if floatPane == nil {
		t.Fatal("expected floating pane")
	}
	floating.Rect = Rect{X: 4, Y: 2, W: 28, H: 8}
	floatPane.live = true
	floatPane.MarkRenderDirty()
	_, _ = floatPane.VTerm.Write([]byte("FLOAT-ONLY-XYZ"))

	visibleView := xansi.Strip(model.View())
	if !strings.Contains(visibleView, "FLOAT-ONLY-XYZ") {
		t.Fatalf("expected floating content in visible render, got:\n%s", visibleView)
	}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'W'}})
	if cmd != nil {
		t.Fatalf("expected toggle floating visibility to be synchronous")
	}
	if tab.FloatingVisible {
		t.Fatal("expected floating layer to be hidden")
	}

	hiddenView := xansi.Strip(model.View())
	if strings.Contains(hiddenView, "FLOAT-ONLY-XYZ") {
		t.Fatalf("expected floating content to disappear when layer hidden, got:\n%s", hiddenView)
	}
}

func TestPrefixTabCyclesFloatingFocusByZOrder(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	tab := model.currentTab()
	basePaneID := tab.ActivePaneID

	for i := 0; i < 2; i++ {
		createFloatingPaneViaPicker(t, model)
	}

	if len(tab.Floating) != 2 {
		t.Fatalf("expected 2 floating viewports, got %d", len(tab.Floating))
	}
	topID := tab.ActivePaneID
	if topID != tab.Floating[1].PaneID {
		t.Fatalf("expected last created floating pane active, got %q", topID)
	}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if cmd != nil {
		t.Fatalf("expected floating focus cycle to be synchronous")
	}
	if tab.ActivePaneID != tab.Floating[0].PaneID {
		t.Fatalf("expected Tab to cycle to lower z floating pane, got %q", tab.ActivePaneID)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if tab.ActivePaneID != basePaneID {
		t.Fatalf("expected esc to return focus to tiled pane, got %q", tab.ActivePaneID)
	}

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if tab.ActivePaneID != tab.Floating[0].PaneID {
		t.Fatalf("expected Tab from tiled focus to enter floating layer at bottom z, got %q", tab.ActivePaneID)
	}
}

func TestFloatingViewportZOrderCommandsAffectRender(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	tab := model.currentTab()
	for i := 0; i < 2; i++ {
		createFloatingPaneViaPicker(t, model)
	}

	if len(tab.Floating) != 2 {
		t.Fatalf("expected 2 floating panes, got %d", len(tab.Floating))
	}

	firstFloat := tab.Panes[tab.Floating[0].PaneID]
	secondFloat := tab.Panes[tab.Floating[1].PaneID]
	if firstFloat == nil || secondFloat == nil {
		t.Fatal("expected floating panes to exist")
	}
	tab.Floating[0].Rect = Rect{X: 6, Y: 2, W: 32, H: 8}
	tab.Floating[1].Rect = Rect{X: 6, Y: 2, W: 32, H: 8}

	_, _ = firstFloat.VTerm.Write([]byte("BOTTOM-LAYER"))
	firstFloat.live = true
	firstFloat.MarkRenderDirty()
	_, _ = secondFloat.VTerm.Write([]byte("TOP-LAYER"))
	secondFloat.live = true
	secondFloat.MarkRenderDirty()

	view := xansi.Strip(model.View())
	if !strings.Contains(view, "TOP-LAYER") || strings.Contains(view, "BOTTOM-LAYER") {
		t.Fatalf("expected top floating viewport to occlude bottom one, got:\n%s", view)
	}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'_'}})
	if cmd != nil {
		t.Fatalf("expected z-order change to be synchronous")
	}

	firstFloat.MarkRenderDirty()
	secondFloat.MarkRenderDirty()
	view = xansi.Strip(model.View())
	if !strings.Contains(view, "BOTTOM-LAYER") || strings.Contains(view, "TOP-LAYER") {
		t.Fatalf("expected lowered floating viewport to move behind bottom one, got:\n%s", view)
	}

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	firstFloat.MarkRenderDirty()
	secondFloat.MarkRenderDirty()
	view = xansi.Strip(model.View())
	if !strings.Contains(view, "TOP-LAYER") || strings.Contains(view, "BOTTOM-LAYER") {
		t.Fatalf("expected raise command to bring floating viewport back to top, got:\n%s", view)
	}
}

func TestFloatingViewportAltMoveUpdatesRectAndClamps(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 80
	model.height = 24

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	tab := model.currentTab()
	createFloatingPaneViaPicker(t, model)

	if len(tab.Floating) != 1 {
		t.Fatalf("expected floating pane, got %d", len(tab.Floating))
	}
	floating := tab.Floating[0]
	floating.Rect = Rect{X: 10, Y: 6, W: 24, H: 8}

	model.prefixActive = true
	_, cmd = model.Update(inputEventMsg{event: uv.KeyPressEvent{Code: 'h', Text: "h", Mod: uv.ModAlt}})
	if floating.Rect.X != 6 {
		t.Fatalf("expected alt-h to move floating pane left by 4, got %+v", floating.Rect)
	}
	if !model.prefixActive {
		t.Fatal("expected repeated floating move to keep prefix active")
	}

	model.prefixActive = true
	_, _ = model.Update(inputEventMsg{event: uv.KeyPressEvent{Code: 'k', Text: "k", Mod: uv.ModAlt}})
	if floating.Rect.Y != 4 {
		t.Fatalf("expected alt-k to move floating pane up by 2, got %+v", floating.Rect)
	}

	for i := 0; i < 10; i++ {
		model.prefixActive = true
		_, _ = model.Update(inputEventMsg{event: uv.KeyPressEvent{Code: 'h', Text: "h", Mod: uv.ModAlt}})
	}
	if floating.Rect.X >= 0 {
		t.Fatalf("expected floating pane to be allowed outside the left edge, got %+v", floating.Rect)
	}
}

func TestFloatingViewportCenterShortcutRecentersActiveRect(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 80
	model.height = 24

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	tab := model.currentTab()
	createFloatingPaneViaPicker(t, model)

	if len(tab.Floating) != 1 {
		t.Fatalf("expected floating pane, got %#v", tab)
	}
	entry := tab.Floating[0]
	entry.Rect = Rect{X: -18, Y: 15, W: 24, H: 8}
	expected := centeredFloatingRect(Rect{X: 0, Y: 0, W: 80, H: 22}, entry.Rect.W, entry.Rect.H)

	model.prefixActive = true
	model.prefixMode = prefixModeFloating
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})

	if entry.Rect != expected {
		t.Fatalf("expected center shortcut to recenter floating rect to %+v, got %+v", expected, entry.Rect)
	}
	if !model.prefixActive || model.prefixMode != prefixModeFloating {
		t.Fatalf("expected floating sticky mode to remain active after center, active=%v mode=%v", model.prefixActive, model.prefixMode)
	}
}

func TestFloatingViewportAltResizeUpdatesRectAndClamps(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 80
	model.height = 24

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	tab := model.currentTab()
	createFloatingPaneViaPicker(t, model)

	floating := tab.Floating[0]
	floating.Rect = Rect{X: 10, Y: 5, W: 24, H: 8}

	model.prefixActive = true
	_, _ = model.Update(inputEventMsg{event: uv.KeyPressEvent{Code: 'H', Text: "H", Mod: uv.ModAlt | uv.ModShift}})
	if floating.Rect.W != 20 {
		t.Fatalf("expected alt-H to shrink width by 4, got %+v", floating.Rect)
	}

	model.prefixActive = true
	_, _ = model.Update(inputEventMsg{event: uv.KeyPressEvent{Code: 'J', Text: "J", Mod: uv.ModAlt | uv.ModShift}})
	if floating.Rect.H != 10 {
		t.Fatalf("expected alt-J to grow height by 2, got %+v", floating.Rect)
	}

	for i := 0; i < 20; i++ {
		model.prefixActive = true
		_, _ = model.Update(inputEventMsg{event: uv.KeyPressEvent{Code: 'H', Text: "H", Mod: uv.ModAlt | uv.ModShift}})
	}
	if floating.Rect.W < 8 {
		t.Fatalf("expected floating width to clamp to minimum, got %+v", floating.Rect)
	}
}

func TestWindowResizeClampsFloatingViewportRect(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	tab := model.currentTab()
	createFloatingPaneViaPicker(t, model)

	floating := tab.Floating[0]
	floating.Rect = Rect{X: 90, Y: 25, W: 40, H: 20}

	_, cmd = model.Update(tea.WindowSizeMsg{Width: 60, Height: 18})
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	if floating.Rect.X+floating.Rect.W > 60 || floating.Rect.Y+floating.Rect.H > 16 {
		t.Fatalf("expected floating rect to clamp inside resized viewport, got %+v", floating.Rect)
	}
}

func TestFloatingViewportRenderKeepsFrameVisibleAcrossUnderlyingRedraws(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	tab := model.currentTab()
	basePane := tab.Panes[tab.ActivePaneID]
	if basePane == nil {
		t.Fatal("expected base pane")
	}
	basePane.live = true
	basePane.MarkRenderDirty()
	_, _ = basePane.VTerm.Write([]byte("BASE-LAYER"))

	createFloatingPaneViaPicker(t, model)

	if len(tab.Floating) != 1 {
		t.Fatalf("expected 1 floating viewport, got %d", len(tab.Floating))
	}
	floating := tab.Floating[0]
	floatPane := tab.Panes[floating.PaneID]
	if floatPane == nil {
		t.Fatal("expected floating pane")
	}
	floating.Rect = Rect{X: 8, Y: 4, W: 34, H: 10}
	floatPane.Title = "float-monitor"
	floatPane.live = true
	floatPane.MarkRenderDirty()
	_, _ = floatPane.VTerm.Write([]byte("FLOAT-TOP"))

	view := xansi.Strip(model.View())
	if !containsAll(view, "float-monitor", "FLOAT-TOP") {
		t.Fatalf("expected floating frame and body in initial render, got:\n%s", view)
	}

	for i := 0; i < 64; i++ {
		_, _ = basePane.VTerm.Write([]byte(fmt.Sprintf("\rBASE-%02d", i)))
		basePane.MarkRenderDirty()

		view = xansi.Strip(model.View())
		if !containsAll(view, "float-monitor", "FLOAT-TOP") {
			t.Fatalf("expected floating frame to remain visible after tiled redraw %d, got:\n%s", i, view)
		}
	}
}

func TestFloatingViewportTitlesIncludeZOrder(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", IconSet: "ascii"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	tab := model.currentTab()
	for i := 0; i < 2; i++ {
		createFloatingPaneViaPicker(t, model)
	}

	if len(tab.Floating) != 2 {
		t.Fatalf("expected 2 floating viewports, got %d", len(tab.Floating))
	}
	tab.Floating[0].Rect = Rect{X: 4, Y: 2, W: 36, H: 8}
	tab.Floating[1].Rect = Rect{X: 44, Y: 3, W: 36, H: 8}
	tab.Panes[tab.Floating[0].PaneID].Title = "float-a"
	tab.Panes[tab.Floating[1].PaneID].Title = "float-b"
	tab.Panes[tab.Floating[0].PaneID].MarkRenderDirty()
	tab.Panes[tab.Floating[1].PaneID].MarkRenderDirty()

	view := xansi.Strip(model.View())
	if !containsAll(view, "shell-2 [floating z:1]", "shell-3 [floating z:2]") {
		t.Fatalf("expected floating titles to include z-order, got:\n%s", view)
	}
}

func TestHelpAndStatusShowViewportControlsAndState(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", IconSet: "ascii"})
	model.width = 120
	model.height = 16

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	pane.VTerm.Resize(20, 6)
	_, _ = pane.VTerm.Write([]byte("0123456789ABCDEFGHIJ"))
	pane.live = true
	pane.MarkRenderDirty()

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	pane.Offset = Point{X: 4, Y: 0}
	pane.MarkRenderDirty()

	model.width = 240
	status := xansi.Strip(model.renderStatus())
	if !containsAll(status, "shell-1", "live") {
		t.Fatalf("expected status to expose minimal pane runtime state, got:\n%s", status)
	}
	if strings.Contains(status, "fixed") {
		t.Fatalf("expected pane display state to stay in pane chrome instead of bottom summary, got:\n%s", status)
	}

	model.height = 40
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	helpView := xansi.Strip(model.View())
	if !containsAll(helpView, "Ctrl-v   connection actions", "take ownership before changing PTY size", "size lock warn", "Ctrl-g   global actions") {
		t.Fatalf("expected help to include connection controls, got:\n%s", helpView)
	}
}

func TestFloatingViewportStatusAndHelpExposeFloatingControls(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", IconSet: "ascii"})
	model.width = 240
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	createFloatingPaneViaPicker(t, model)

	status := xansi.Strip(model.renderStatus())
	if !containsAll(status, "float", "live") {
		t.Fatalf("expected status to expose floating layer state, got:\n%s", status)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	helpView := xansi.Strip(model.View())
	if !containsAll(helpView, "Ctrl-o   floating actions", "Ctrl-f   terminal picker", "drag body to move", "drag bottom-right corner to resize") {
		t.Fatalf("expected help to include floating pane controls, got:\n%s", helpView)
	}
}

func TestFloatingViewportStatusShowsSwitchHintFromTiledFocus(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", IconSet: "ascii"})
	model.width = 240
	model.height = 32

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	createFloatingPaneViaPicker(t, model)
	tab := model.currentTab()
	if tab == nil || len(tab.Floating) == 0 {
		t.Fatalf("expected floating pane, got %#v", tab)
	}
	_ = model.focusTiledPane()

	bar := xansi.Strip(model.renderTabBar())
	if !containsAll(bar, "pane:2", "term:2", "float:1") {
		t.Fatalf("expected top bar to expose compact workspace counts, got:\n%s", bar)
	}
}

func TestWelcomePaneLinesUsePaneTerminology(t *testing.T) {
	pane := &Pane{Title: "api-shell", Viewport: &Viewport{TerminalState: "exited"}}
	lines := welcomePaneLines(pane)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "viewport") {
		t.Fatalf("expected welcome pane copy to avoid viewport terminology, got:\n%s", joined)
	}
	if !containsAll(joined, "pane:", "api-shell", "restart in this pane") {
		t.Fatalf("expected welcome pane copy to use pane terminology, got:\n%s", joined)
	}
}

func TestNewFloatingViewportRectsAreStaggered(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 32

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	createFloatingPaneViaPicker(t, model)
	createFloatingPaneViaPicker(t, model)

	tab := model.currentTab()
	if tab == nil || len(tab.Floating) != 2 {
		t.Fatalf("expected 2 floating panes, got %#v", tab)
	}
	first := tab.Floating[0].Rect
	second := tab.Floating[1].Rect
	if first == second {
		t.Fatalf("expected staggered floating rects, got %+v and %+v", first, second)
	}
	if second.X <= first.X && second.Y <= first.Y {
		t.Fatalf("expected second floating rect to be offset from first, got %+v and %+v", first, second)
	}
}

func TestMouseClickFloatingPaneFocusesAndRaisesIt(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 32

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	createFloatingPaneViaPicker(t, model)
	createFloatingPaneViaPicker(t, model)

	tab := model.currentTab()
	if tab == nil || len(tab.Floating) != 2 {
		t.Fatalf("expected 2 floating panes, got %#v", tab)
	}
	tab.Floating[0].Rect = Rect{X: 8, Y: 4, W: 28, H: 8}
	tab.Floating[1].Rect = Rect{X: 16, Y: 6, W: 28, H: 8}
	target := tab.Floating[0]

	_, cmd := model.Update(inputEventMsg{event: uv.MouseClickEvent{X: target.Rect.X + 2, Y: target.Rect.Y + 1, Button: uv.MouseLeft}})
	if cmd != nil {
		t.Fatalf("expected mouse focus/raise to be synchronous")
	}
	if tab.ActivePaneID != target.PaneID {
		t.Fatalf("expected click to focus lower floating pane, got %q", tab.ActivePaneID)
	}
	if _, top, ok := floatingPaneOrder(tab, target.PaneID); !ok || top != 2 {
		t.Fatalf("expected clicked floating pane to be raised, got order=%d ok=%v", top, ok)
	}
}

func TestMouseDragFloatingPaneMovesRect(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 32

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	createFloatingPaneViaPicker(t, model)

	tab := model.currentTab()
	if tab == nil || len(tab.Floating) != 1 {
		t.Fatalf("expected floating pane, got %#v", tab)
	}
	entry := tab.Floating[0]
	entry.Rect = Rect{X: 10, Y: 5, W: 28, H: 8}

	_, _ = model.Update(inputEventMsg{event: uv.MouseClickEvent{X: entry.Rect.X + 3, Y: entry.Rect.Y + 1, Button: uv.MouseLeft}})
	_, _ = model.Update(inputEventMsg{event: uv.MouseMotionEvent{X: 30, Y: 12, Button: uv.MouseLeft}})
	_, _ = model.Update(inputEventMsg{event: uv.MouseReleaseEvent{X: 30, Y: 12, Button: uv.MouseLeft}})

	if entry.Rect.X != 27 || entry.Rect.Y != 11 {
		t.Fatalf("expected drag to move floating rect to 27,11 got %+v", entry.Rect)
	}
}

func TestMouseDragFloatingPaneClampsToViewportBounds(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 80
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	createFloatingPaneViaPicker(t, model)

	tab := model.currentTab()
	if tab == nil || len(tab.Floating) != 1 {
		t.Fatalf("expected floating pane, got %#v", tab)
	}
	entry := tab.Floating[0]
	entry.Rect = Rect{X: 10, Y: 4, W: 28, H: 8}

	_, _ = model.Update(inputEventMsg{event: uv.MouseClickEvent{X: entry.Rect.X + 2, Y: entry.Rect.Y + 1, Button: uv.MouseLeft}})
	_, _ = model.Update(inputEventMsg{event: uv.MouseMotionEvent{X: 0, Y: 1, Button: uv.MouseLeft}})
	if entry.Rect.X != -2 || entry.Rect.Y != 0 {
		t.Fatalf("expected drag to allow partial top-left out-of-bounds visibility, got %+v", entry.Rect)
	}

	_, _ = model.Update(inputEventMsg{event: uv.MouseMotionEvent{X: 79, Y: 18, Button: uv.MouseLeft}})
	_, _ = model.Update(inputEventMsg{event: uv.MouseReleaseEvent{X: 79, Y: 18, Button: uv.MouseLeft}})

	if entry.Rect.X != 76 || entry.Rect.Y != 16 {
		t.Fatalf("expected drag to keep only a visible floating edge in bounds, got %+v", entry.Rect)
	}
}

func TestMouseResizeFloatingPaneFromBottomRightHandle(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 32

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	createFloatingPaneViaPicker(t, model)

	tab := model.currentTab()
	if tab == nil || len(tab.Floating) != 1 {
		t.Fatalf("expected floating pane, got %#v", tab)
	}
	entry := tab.Floating[0]
	entry.Rect = Rect{X: 10, Y: 5, W: 28, H: 8}

	handleX := entry.Rect.X + entry.Rect.W - 1
	handleY := entry.Rect.Y + entry.Rect.H - 1
	_, _ = model.Update(inputEventMsg{event: uv.MouseClickEvent{X: handleX, Y: handleY + 1, Button: uv.MouseLeft}})
	_, _ = model.Update(inputEventMsg{event: uv.MouseMotionEvent{X: handleX + 8, Y: handleY + 4 + 1, Button: uv.MouseLeft}})
	_, _ = model.Update(inputEventMsg{event: uv.MouseReleaseEvent{X: handleX + 8, Y: handleY + 4 + 1, Button: uv.MouseLeft}})

	if entry.Rect.W <= 28 || entry.Rect.H <= 8 {
		t.Fatalf("expected mouse resize to grow floating rect, got %+v", entry.Rect)
	}
}

func TestMouseDragFloatingPaneInvalidatesRenderImmediately(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 32
	model.renderBatching = true
	model.program = &tea.Program{}
	model.renderInterval = 16 * time.Millisecond
	model.renderFastInterval = 8 * time.Millisecond

	now := time.Unix(0, 0)
	model.timeNow = func() time.Time { return now }

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)
	createFloatingPaneViaPicker(t, model)

	tab := model.currentTab()
	if tab == nil || len(tab.Floating) != 1 {
		t.Fatalf("expected floating pane, got %#v", tab)
	}
	entry := tab.Floating[0]
	entry.Rect = Rect{X: 10, Y: 5, W: 28, H: 8}

	_ = model.View()
	model.renderDirty = false

	_, _ = model.Update(inputEventMsg{event: uv.MouseClickEvent{X: entry.Rect.X + 3, Y: entry.Rect.Y + 1, Button: uv.MouseLeft}})
	model.renderDirty = false

	now = now.Add(2 * time.Millisecond)
	_, _ = model.Update(inputEventMsg{event: uv.MouseMotionEvent{X: 30, Y: 12, Button: uv.MouseLeft}})

	if !model.renderDirty {
		t.Fatal("expected drag motion to invalidate render immediately to avoid visible trails")
	}
	if entry.Rect.X != 27 || entry.Rect.Y != 11 {
		t.Fatalf("expected drag to still update geometry immediately, got %+v", entry.Rect)
	}
}

func TestRenderTabCompositeFloatingRectChangeRebuildsCanvasCache(t *testing.T) {
	model := benchmarkModelWithFloatingOverlay(t, 120, 32)
	tab := model.currentTab()
	if tab == nil {
		t.Fatal("expected active tab")
	}
	if tab.renderCache == nil || tab.renderCache.canvas == nil {
		t.Fatal("expected populated render cache")
	}
	if len(tab.Floating) != 1 {
		t.Fatalf("expected one floating pane, got %d", len(tab.Floating))
	}

	cache := tab.renderCache
	entry := tab.Floating[0]
	entry.Rect = Rect{X: entry.Rect.X + 5, Y: entry.Rect.Y + 2, W: entry.Rect.W, H: entry.Rect.H}

	out := model.renderTabComposite(tab, model.width, model.height-2)
	if out == "" {
		t.Fatal("expected rendered output")
	}
	if tab.renderCache == cache {
		t.Fatal("expected floating rect change to rebuild tab render cache for correctness")
	}
	if got := tab.renderCache.rects[entry.PaneID]; got != entry.Rect {
		t.Fatalf("expected cached rect %+v, got %+v", entry.Rect, got)
	}
}

func TestRenderTabCompositeFloatingRectChangeRebuildsOverlappedPaneFully(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 32

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	if tab == nil {
		t.Fatal("expected active tab")
	}
	createFloatingPaneViaPicker(t, model)
	createFloatingPaneViaPicker(t, model)
	if len(tab.Floating) != 2 {
		t.Fatalf("expected 2 floating panes, got %d", len(tab.Floating))
	}

	bottom := tab.Panes[tab.Floating[0].PaneID]
	top := tab.Panes[tab.Floating[1].PaneID]
	if bottom == nil || top == nil {
		t.Fatal("expected floating panes")
	}

	tab.Floating[0].Rect = Rect{X: 8, Y: 3, W: 32, H: 8}
	tab.Floating[1].Rect = Rect{X: 8, Y: 3, W: 32, H: 8}

	bottom.Title = "bottom"
	bottom.Snapshot = &protocol.Snapshot{
		TerminalID: bottom.TerminalID,
		Size:       protocol.Size{Cols: 80, Rows: 24},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			stringToProtocolRow("BOTTOM-ROW-0"),
			stringToProtocolRow("BOTTOM-ROW-1"),
			stringToProtocolRow("BOTTOM-ROW-2"),
		}},
	}
	bottom.live = false
	bottom.MarkRenderDirty()
	bottom.clearDirtyRegion()

	top.Title = "top"
	top.Snapshot = &protocol.Snapshot{
		TerminalID: top.TerminalID,
		Size:       protocol.Size{Cols: 80, Rows: 24},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			stringToProtocolRow("TOP-LAYER"),
		}},
	}
	top.live = false
	top.MarkRenderDirty()
	top.clearDirtyRegion()

	initial := xansi.Strip(model.renderTabComposite(tab, model.width, model.height-2))
	if !strings.Contains(initial, "TOP-LAYER") {
		t.Fatalf("expected top floating pane to be visible, got:\n%s", initial)
	}
	if strings.Contains(initial, "BOTTOM-ROW-1") {
		t.Fatalf("expected top floating pane to occlude bottom pane before move, got:\n%s", initial)
	}

	bottom.MarkRenderDirty()
	bottom.SetDirtyRows(0, 0, true)

	tab.Floating[1].Rect = Rect{X: 48, Y: 3, W: 32, H: 8}

	out := xansi.Strip(model.renderTabComposite(tab, model.width, model.height-2))
	if !containsAll(out, "BOTTOM-ROW-0", "BOTTOM-ROW-1", "BOTTOM-ROW-2") {
		t.Fatalf("expected floating rect rebuild to fully restore previously occluded pane, got:\n%s", out)
	}
}

func TestRenderTabCompositeOverlapRedrawRestoresTopFloatingPaneFully(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 32

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	if tab == nil {
		t.Fatal("expected active tab")
	}
	createFloatingPaneViaPicker(t, model)
	createFloatingPaneViaPicker(t, model)
	if len(tab.Floating) != 2 {
		t.Fatalf("expected 2 floating panes, got %d", len(tab.Floating))
	}

	bottom := tab.Panes[tab.Floating[0].PaneID]
	top := tab.Panes[tab.Floating[1].PaneID]
	if bottom == nil || top == nil {
		t.Fatal("expected floating panes")
	}

	tab.Floating[0].Rect = Rect{X: 8, Y: 3, W: 32, H: 8}
	tab.Floating[1].Rect = Rect{X: 12, Y: 5, W: 32, H: 8}

	bottom.Title = "bottom"
	bottom.Snapshot = &protocol.Snapshot{
		TerminalID: bottom.TerminalID,
		Size:       protocol.Size{Cols: 80, Rows: 24},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			stringToProtocolRow("BOTTOM-LIVE-0"),
		}},
	}
	bottom.live = false
	bottom.MarkRenderDirty()
	bottom.clearDirtyRegion()

	top.Title = "top"
	top.Snapshot = &protocol.Snapshot{
		TerminalID: top.TerminalID,
		Size:       protocol.Size{Cols: 80, Rows: 24},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			stringToProtocolRow("TOP-ROW-0"),
			stringToProtocolRow("TOP-ROW-1"),
			stringToProtocolRow("TOP-ROW-2"),
		}},
	}
	top.live = false
	top.MarkRenderDirty()
	top.clearDirtyRegion()

	initial := xansi.Strip(model.renderTabComposite(tab, model.width, model.height-2))
	if !containsAll(initial, "TOP-ROW-0", "TOP-ROW-1", "TOP-ROW-2") {
		t.Fatalf("expected top floating pane body before overlap redraw, got:\n%s", initial)
	}

	bottom.Snapshot = &protocol.Snapshot{
		TerminalID: bottom.TerminalID,
		Size:       protocol.Size{Cols: 80, Rows: 24},
		Screen: protocol.ScreenData{Cells: [][]protocol.Cell{
			stringToProtocolRow("BOTTOM-LIVE-1"),
		}},
	}
	bottom.MarkRenderDirty()
	bottom.SetDirtyRows(0, 0, true)

	top.MarkRenderDirty()
	top.SetDirtyRows(0, 0, true)

	out := xansi.Strip(model.renderTabComposite(tab, model.width, model.height-2))
	if !containsAll(out, "TOP-ROW-0", "TOP-ROW-1", "TOP-ROW-2") {
		t.Fatalf("expected overlap redraw to fully restore top floating pane, got:\n%s", out)
	}
}

func TestRenderTabCompositeFloatingRectDamageRedrawsLivePaneUnderMovedOverlay(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 32

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	if tab == nil {
		t.Fatal("expected active tab")
	}
	createFloatingPaneViaPicker(t, model)
	createFloatingPaneViaPicker(t, model)
	if len(tab.Floating) != 2 {
		t.Fatalf("expected 2 floating panes, got %d", len(tab.Floating))
	}

	bottom := tab.Panes[tab.Floating[0].PaneID]
	top := tab.Panes[tab.Floating[1].PaneID]
	if bottom == nil || top == nil {
		t.Fatal("expected floating panes")
	}

	tab.Floating[0].Rect = Rect{X: 4, Y: 2, W: 32, H: 8}
	tab.Floating[1].Rect = Rect{X: 12, Y: 4, W: 32, H: 8}

	bottom.Title = "bottom"
	bottom.live = true
	bottom.MarkRenderDirty()
	_, _ = bottom.VTerm.Write([]byte("BOTTOM-ROW-0\r\nBOTTOM-ROW-1\r\nBOTTOM-ROW-2"))

	top.Title = "top"
	top.live = true
	top.MarkRenderDirty()
	_, _ = top.VTerm.Write([]byte("TOP-LAYER"))

	initial := xansi.Strip(model.renderTabComposite(tab, model.width, model.height-2))
	if !strings.Contains(initial, "TOP-LAYER") {
		t.Fatalf("expected top live floating pane before drag, got:\n%s", initial)
	}

	tab.Floating[1].Rect = Rect{X: 48, Y: 4, W: 32, H: 8}
	out := xansi.Strip(model.renderTabComposite(tab, model.width, model.height-2))
	if !containsAll(out, "BOTTOM-ROW-0", "BOTTOM-ROW-1", "BOTTOM-ROW-2") {
		t.Fatalf("expected moved overlay to reveal full live pane body, got:\n%s", out)
	}
}

func TestMouseDragFloatingPaneKeepsPreviouslyOccludedLivePaneVisible(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 32

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	if tab == nil {
		t.Fatal("expected active tab")
	}
	createFloatingPaneViaPicker(t, model)
	createFloatingPaneViaPicker(t, model)
	if len(tab.Floating) != 2 {
		t.Fatalf("expected 2 floating panes, got %d", len(tab.Floating))
	}

	bottom := tab.Panes[tab.Floating[0].PaneID]
	top := tab.Panes[tab.Floating[1].PaneID]
	if bottom == nil || top == nil {
		t.Fatal("expected floating panes")
	}

	tab.Floating[0].Rect = Rect{X: 4, Y: 2, W: 32, H: 8}
	tab.Floating[1].Rect = Rect{X: 12, Y: 4, W: 32, H: 8}

	bottom.Title = "bottom"
	bottom.live = true
	bottom.MarkRenderDirty()
	_, _ = bottom.VTerm.Write([]byte("BOTTOM-ROW-0\r\nBOTTOM-ROW-1\r\nBOTTOM-ROW-2"))

	top.Title = "top"
	top.live = true
	top.MarkRenderDirty()
	_, _ = top.VTerm.Write([]byte("TOP-LAYER"))

	initial := xansi.Strip(model.View())
	if !containsAll(initial, "BOTTOM-ROW-0", "TOP-LAYER") {
		t.Fatalf("expected both floating panes before drag, got:\n%s", initial)
	}

	_, _ = model.Update(inputEventMsg{event: uv.MouseClickEvent{X: 16, Y: 4, Button: uv.MouseLeft}})
	_, _ = model.Update(inputEventMsg{event: uv.MouseMotionEvent{X: 52, Y: 4, Button: uv.MouseLeft}})
	_, _ = model.Update(inputEventMsg{event: uv.MouseReleaseEvent{X: 52, Y: 4, Button: uv.MouseLeft}})

	out := xansi.Strip(model.View())
	if !containsAll(out, "TOP-LAYER", "BOTTOM-ROW-0", "BOTTOM-ROW-1", "BOTTOM-ROW-2") {
		t.Fatalf("expected mouse drag to keep bottom floating pane visible, got:\n%s", out)
	}
}

func TestDrawPaneBodyAltScreenBypassesDirtyRowOptimization(t *testing.T) {
	pane := &Pane{
		Title: "alt",
		Viewport: &Viewport{
			VTerm: localvterm.New(24, 6, 0, nil),
		},
	}
	pane.live = true
	pane.MarkRenderDirty()
	pane.SetDirtyRows(0, 0, true)

	_, _ = pane.VTerm.Write([]byte("SHELL-A\r\n"))
	_, _ = pane.VTerm.Write([]byte("\x1b[?1049h\x1b[2J\x1b[HALT-ROW-0\r\nALT-ROW-1"))
	if !pane.VTerm.IsAltScreen() {
		t.Fatal("expected pane vterm to be in alt-screen")
	}

	canvas := newComposedCanvas(24, 6)
	staleRect := Rect{X: 1, Y: 3, W: 10, H: 1}
	canvas.drawText(staleRect, []string{"STALE-LINE"}, drawStyle{})

	canvas.drawPaneBody(Rect{X: 0, Y: 0, W: 24, H: 6}, pane, false)
	view := xansi.Strip(canvas.String())
	if strings.Contains(view, "STALE-LINE") {
		t.Fatalf("expected alt-screen pane redraw to clear stale rows outside dirty range, got:\n%s", view)
	}
	if !strings.Contains(view, "ALT-ROW-0") || !strings.Contains(view, "ALT-ROW-1") {
		t.Fatalf("expected alt-screen content after full redraw, got:\n%s", view)
	}
}

func TestPrefixColonBeginsCommandPrompt(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})

	if model.prompt == nil || model.prompt.Title != "command" {
		t.Fatalf("expected command prompt, got %#v", model.prompt)
	}
}

func TestParseLayoutResolvePolicySupportsPromptAndFlags(t *testing.T) {
	cases := []struct {
		args []string
		want LayoutResolvePolicy
	}{
		{args: nil, want: LayoutResolveCreate},
		{args: []string{"prompt"}, want: LayoutResolvePrompt},
		{args: []string{"--prompt"}, want: LayoutResolvePrompt},
		{args: []string{"skip"}, want: LayoutResolveSkip},
		{args: []string{"create"}, want: LayoutResolveCreate},
	}
	for _, tc := range cases {
		got, err := parseLayoutResolvePolicy(tc.args)
		if err != nil {
			t.Fatalf("parseLayoutResolvePolicy(%q) returned error: %v", tc.args, err)
		}
		if got != tc.want {
			t.Fatalf("parseLayoutResolvePolicy(%q) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestTerminalPickerViewUsesRenderCacheUntilDirty(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "worker", Command: []string{"tail", "-f", "worker.log"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 24

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	first := model.View()
	model.terminalPicker.Query = "mutated-without-invalidate"
	model.terminalPicker.applyFilter()
	second := model.View()
	if first != second {
		t.Fatal("expected cached picker view when render is clean")
	}

	model.invalidateRender()
	third := model.View()
	if third == second {
		t.Fatal("expected dirty picker render to refresh cache")
	}
}

func TestModelViewPreservesAnsiColors(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	_, _ = pane.VTerm.Write([]byte("\x1b[31mRED\x1b[0m"))
	pane.live = true
	pane.MarkRenderDirty()

	view := model.View()
	if !strings.Contains(view, "\x1b[") {
		t.Fatalf("expected ANSI sequences in rendered view:\n%s", view)
	}
	if !strings.Contains(xansi.Strip(view), "RED") {
		t.Fatalf("expected rendered text in view:\n%s", view)
	}
}

func TestExitedPaneHistoryUsesNeutralForeground(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	_, _ = pane.VTerm.Write([]byte("\x1b[31mRED\x1b[0m"))
	pane.live = true
	pane.MarkRenderDirty()
	model.markTerminalExited(pane.TerminalID, 0)

	view := model.View()
	if !strings.Contains(xansi.Strip(view), "RED") {
		t.Fatalf("expected exited pane history text in view:\n%s", view)
	}
	if strings.Contains(view, "38;2;255;0;0") {
		t.Fatalf("expected exited pane history to drop original red styling:\n%s", view)
	}
	if !strings.Contains(view, "38;2;226;232;240") {
		t.Fatalf("expected exited pane history to use neutral foreground:\n%s", view)
	}
}

func TestExitedSnapshotHistoryUsesNeutralForeground(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20
	model.workspace = Workspace{Name: "main", Tabs: []*Tab{newTab("1")}, ActiveTab: 0}
	tab := model.currentTab()
	pane := &Pane{
		ID:    "pane-001",
		Title: "snapshot-exited",
		Viewport: &Viewport{
			TerminalID:    "term-001",
			TerminalState: "exited",
			Snapshot: &protocol.Snapshot{
				TerminalID: "term-001",
				Size:       protocol.Size{Cols: 3, Rows: 1},
				Screen: protocol.ScreenData{
					Cells: [][]protocol.Cell{{
						{Content: "R", Width: 1, Style: protocol.CellStyle{FG: "#ff0000"}},
						{Content: "E", Width: 1, Style: protocol.CellStyle{FG: "#ff0000"}},
						{Content: "D", Width: 1, Style: protocol.CellStyle{FG: "#ff0000"}},
					}},
				},
			},
		},
	}
	tab.Panes[pane.ID] = pane
	tab.ActivePaneID = pane.ID
	tab.Root = NewLeaf(pane.ID)

	view := model.View()
	if !strings.Contains(xansi.Strip(view), "RED") {
		t.Fatalf("expected exited snapshot text in view:\n%s", view)
	}
	if strings.Contains(view, "38;2;255;0;0") {
		t.Fatalf("expected exited snapshot history to drop original red styling:\n%s", view)
	}
	if !strings.Contains(view, "38;2;226;232;240") {
		t.Fatalf("expected exited snapshot history to use neutral foreground:\n%s", view)
	}
}

func TestLivePaneRenderPreservesDefaultTerminalColors(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	if _, err := pane.VTerm.Write([]byte("HOST")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	pane.live = true
	pane.MarkRenderDirty()

	model.handleInputEvent(uv.ForegroundColorEvent{Color: color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff}})
	model.handleInputEvent(uv.BackgroundColorEvent{Color: color.RGBA{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff}})

	view := model.View()
	if strings.Contains(view, "38;2;17;34;51") || strings.Contains(view, "48;2;170;187;204") {
		t.Fatalf("expected live pane content to preserve default terminal colors without baking RGB values:\n%s", view)
	}
}

func TestHostTerminalColorsApplyToNewViewportDefaults(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.applyHostTerminalColors(
		color.RGBA{R: 0x22, G: 0x44, B: 0x66, A: 0xff},
		color.RGBA{R: 0x11, G: 0x33, B: 0x55, A: 0xff},
	)

	view := model.newViewport("term-001", 7, nil)
	if view == nil || view.VTerm == nil {
		t.Fatal("expected viewport runtime")
	}
	if view.DefaultFG != "#224466" {
		t.Fatalf("expected viewport foreground default, got %q", view.DefaultFG)
	}
	if view.DefaultBG != "#113355" {
		t.Fatalf("expected viewport background default, got %q", view.DefaultBG)
	}
	if fg, bg := view.VTerm.DefaultColors(); fg != "#224466" || bg != "#113355" {
		t.Fatalf("expected vterm defaults to match host colors, got fg=%q bg=%q", fg, bg)
	}
}

func TestLivePaneRenderPreservesAnsiPaletteIndices(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	if _, err := pane.VTerm.Write([]byte("\x1b[31mRED\x1b[0m")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	pane.live = true
	pane.MarkRenderDirty()

	model.handleInputEvent(uv.UnknownOscEvent("\x1b]4;1;rgb:12/34/56\x07"))

	view := model.View()
	if !strings.Contains(view, "[31m") && !strings.Contains(view, "[0;31m") {
		t.Fatalf("expected live pane to preserve ANSI palette index instead of baking RGB:\n%s", view)
	}
}

func TestHostPaletteColorAppliesToNewViewport(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.handleInputEvent(uv.UnknownOscEvent("\x1b]4;1;rgb:ab/cd/ef\x07"))

	view := model.newViewport("term-001", 7, nil)
	if view == nil || view.VTerm == nil {
		t.Fatal("expected viewport runtime")
	}
	if color := view.VTerm.IndexedColor(1); color != "#abcdef" {
		t.Fatalf("expected inherited palette color, got %q", color)
	}
	if _, err := view.VTerm.Write([]byte("\x1b[31mR\x1b[0m")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if cell := view.VTerm.CellAt(0, 0); cell.Style.FG != "ansi:1" {
		t.Fatalf("expected new viewport to preserve ANSI color semantic, got %#v", cell.Style)
	}
}

func TestHostTerminalColorChangeDoesNotDirtyRenderedPane(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	if _, err := pane.VTerm.Write([]byte("HOST")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	pane.live = true
	pane.MarkRenderDirty()

	beforeView := model.View()
	beforeVersion := pane.cellVersion
	if pane.IsRenderDirty() {
		t.Fatal("expected pane to be clean after render")
	}
	model.renderDirty = false

	model.handleInputEvent(uv.ForegroundColorEvent{Color: color.RGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}})
	model.handleInputEvent(uv.BackgroundColorEvent{Color: color.RGBA{R: 0x65, G: 0x43, B: 0x21, A: 0xff}})

	if pane.IsRenderDirty() {
		t.Fatal("expected host default color change not to dirty rendered pane")
	}
	if pane.cellVersion != beforeVersion {
		t.Fatalf("expected host default color change not to invalidate pane cell cache version, got %d want %d", pane.cellVersion, beforeVersion)
	}
	if model.renderDirty {
		t.Fatal("expected host default color change not to force immediate full rerender")
	}
	if afterView := model.View(); afterView != beforeView {
		t.Fatalf("expected cached view to remain stable after host default color change")
	}
}

func TestHostPaletteColorChangeDoesNotDirtyRenderedPane(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	if _, err := pane.VTerm.Write([]byte("\x1b[31mRED\x1b[0m")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	pane.live = true
	pane.MarkRenderDirty()

	beforeView := model.View()
	beforeVersion := pane.cellVersion
	if pane.IsRenderDirty() {
		t.Fatal("expected pane to be clean after render")
	}
	model.renderDirty = false

	model.handleInputEvent(uv.UnknownOscEvent("\x1b]4;1;rgb:12/34/56\x07"))

	if pane.IsRenderDirty() {
		t.Fatal("expected host palette color change not to dirty rendered pane")
	}
	if pane.cellVersion != beforeVersion {
		t.Fatalf("expected host palette color change not to invalidate pane cell cache version, got %d want %d", pane.cellVersion, beforeVersion)
	}
	if model.renderDirty {
		t.Fatal("expected host palette color change not to force immediate full rerender")
	}
	if afterView := model.View(); afterView != beforeView {
		t.Fatalf("expected cached view to remain stable after host palette color change")
	}
}

func TestModelViewRendersCJKWithoutInsertedSpaces(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	_, _ = pane.VTerm.Write([]byte("你a好"))
	pane.live = true
	pane.MarkRenderDirty()

	view := xansi.Strip(model.View())
	if !strings.Contains(view, "你a好") {
		t.Fatalf("expected contiguous CJK text in view, got:\n%s", view)
	}
	if strings.Contains(view, "你 a 好") || strings.Contains(view, "你 a好") || strings.Contains(view, "你a 好") {
		t.Fatalf("expected no inserted spaces in CJK rendering, got:\n%s", view)
	}
}

func TestModelViewRendersUnicodeClustersWithoutInsertedSpaces(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	text := "e\u0301🙂한글"
	_, _ = pane.VTerm.Write([]byte(text))
	pane.live = true
	pane.MarkRenderDirty()

	view := xansi.Strip(model.View())
	if !strings.Contains(view, norm.NFC.String(text)) {
		t.Fatalf("expected contiguous unicode text in view, got:\n%s", view)
	}
	if strings.Contains(view, "e ́") || strings.Contains(view, "🙂 한") || strings.Contains(view, "한 글") {
		t.Fatalf("expected no inserted spaces around unicode clusters, got:\n%s", view)
	}
}

func TestActivePaneRendersVisibleCursor(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	_, _ = pane.VTerm.Write([]byte("\x1b[?25hcursor"))
	pane.live = true
	pane.MarkRenderDirty()

	cursor := paneCursorForViewport(pane, model.width-2, model.height-3)
	if !cursor.Visible || cursor.Row != 0 || cursor.Col != 6 {
		t.Fatalf("expected visible cursor at 0,6, got %+v", cursor)
	}
	view := model.View()
	if !strings.Contains(view, "cursor") {
		t.Fatalf("expected pane content in rendered view:\n%s", view)
	}
}

func TestCtrlCAndCtrlDPassThroughToActivePane(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	_ = mustRunCmd(t, cmd)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	_ = mustRunCmd(t, cmd)

	if model.quitting {
		t.Fatal("expected ctrl-c/ctrl-d to be sent to pane, not quit TUI")
	}
	if len(client.inputs) != 2 {
		t.Fatalf("expected 2 input writes, got %d", len(client.inputs))
	}
	if got := string(client.inputs[0]); got != "\x03" {
		t.Fatalf("expected ctrl-c byte, got %q", got)
	}
	if got := string(client.inputs[1]); got != "\x04" {
		t.Fatalf("expected ctrl-d byte, got %q", got)
	}
}

func TestReadonlyViewportBlocksInputExceptCtrlC(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil || !pane.Readonly {
		t.Fatal("expected active pane to enter readonly mode")
	}

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd != nil {
		_ = mustRunCmd(t, cmd)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected ctrl-c to remain writable in readonly mode")
	}
	_ = mustRunCmd(t, cmd)

	if got := len(client.inputs); got != 1 {
		t.Fatalf("expected only ctrl-c to pass through readonly mode, got %d writes", got)
	}
	if got := string(client.inputs[0]); got != "\x03" {
		t.Fatalf("expected ctrl-c payload, got %q", got)
	}
}

func TestReadonlyViewportBlocksRawInputButAllowsCtrlC(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})

	_, cmd := model.Update(rawInputMsg{data: []byte("ab\x03cd")})
	if cmd != nil {
		_ = mustRunCmd(t, cmd)
	}

	if got := len(client.inputs); got != 1 {
		t.Fatalf("expected raw readonly path to only forward ctrl-c, got %d writes", got)
	}
	if got := string(client.inputs[0]); got != "\x03" {
		t.Fatalf("expected raw readonly payload to contain only ctrl-c, got %q", got)
	}
}

func TestReadonlyViewportBlocksKillTerminal(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})

	_ = activatePrefixForTest(model)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	if cmd != nil {
		t.Fatalf("expected readonly kill to be blocked locally, got cmd %#v", cmd)
	}
	if client.kills != 0 {
		t.Fatalf("expected readonly kill to avoid client kill request, got %d", client.kills)
	}
	if model.err == nil || !strings.Contains(model.err.Error(), "readonly pane cannot kill/remove terminal") {
		t.Fatalf("expected readonly kill error, got %v", model.err)
	}
}

func TestKillActiveTerminalOpensStopConfirmationPrompt(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 24

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	cmd := activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	if cmd != nil {
		t.Fatalf("expected stop-terminal request to open confirm prompt before killing, got %#v", cmd)
	}
	if client.kills != 0 {
		t.Fatalf("expected stop-terminal confirmation to defer kill, got %d", client.kills)
	}
	if model.prompt == nil || model.prompt.Kind != "confirm-stop-terminal" {
		t.Fatalf("expected confirm-stop-terminal prompt, got %#v", model.prompt)
	}
	view := xansi.Strip(model.View())
	if !containsAll(view, "Stop Terminal", "Stopping this terminal will affect every pane", "Enter stop") {
		t.Fatalf("expected stop confirmation prompt in view, got:\n%s", view)
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if client.kills != 1 {
		t.Fatalf("expected confirmed stop to issue kill request, got %d", client.kills)
	}
}

func TestCloseActivePaneShowsTerminalKeepsRunningNotice(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	base := tab.Panes[tab.ActivePaneID]
	if base == nil {
		t.Fatal("expected base pane")
	}
	other := &Pane{
		ID:    "pane-2",
		Title: "other",
		Viewport: &Viewport{
			TerminalID:    "term-002",
			Channel:       base.Channel + 1,
			VTerm:         localvterm.New(80, 24, 100, nil),
			Snapshot:      &protocol.Snapshot{TerminalID: "term-002", Size: protocol.Size{Cols: 80, Rows: 24}},
			TerminalState: "running",
			Mode:          ViewportModeFit,
		},
	}
	tab.Panes[other.ID] = other
	_ = tab.Root.Split(base.ID, SplitVertical, other.ID)
	tab.ActivePaneID = base.ID

	var cmd tea.Cmd
	cmd = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if len(tab.Panes) != 1 {
		t.Fatalf("expected one pane to remain after closing active pane, got %d", len(tab.Panes))
	}
	if model.notice != "pane closed; terminal keeps running" {
		t.Fatalf("unexpected close-pane notice: %q", model.notice)
	}
}

func TestEnterPassThroughUsesCarriageReturn(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = mustRunCmd(t, cmd)

	if len(client.inputs) != 1 {
		t.Fatalf("expected 1 input write, got %d", len(client.inputs))
	}
	if got := string(client.inputs[0]); got != "\r" {
		t.Fatalf("expected carriage return for enter, got %q", got)
	}
}

func TestRawInputPassesBytesThroughUnchanged(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	raw := []byte{'a', '\r', '\t', 0x7f}
	_, cmd := model.Update(rawInputMsg{data: raw})
	_ = mustRunCmd(t, cmd)

	if len(client.inputs) != 1 {
		t.Fatalf("expected 1 input write, got %d", len(client.inputs))
	}
	if got := string(client.inputs[0]); got != string(raw) {
		t.Fatalf("expected raw bytes %q, got %q", string(raw), got)
	}
}

func TestRawInputUnicodePassesBytesThroughUnchanged(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	raw := []byte("🩷🙂你好")
	_, cmd := model.Update(rawInputMsg{data: raw})
	_ = mustRunCmd(t, cmd)

	if len(client.inputs) != 1 {
		t.Fatalf("expected 1 input write, got %d", len(client.inputs))
	}
	if got := string(client.inputs[0]); got != string(raw) {
		t.Fatalf("expected raw unicode bytes %q, got %q", string(raw), got)
	}
}

func TestRawPrefixCtrlAForwardsLiteralCtrlA(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, cmd := model.Update(rawInputMsg{data: []byte{0x01}})
	_ = mustRunCmd(t, cmd)
	if model.prefixActive {
		t.Fatal("expected literal ctrl-a to bypass prefix mode")
	}
	if len(client.inputs) != 1 || string(client.inputs[0]) != "\x01" {
		t.Fatalf("expected forwarded ctrl-a, got %#v", client.inputs)
	}
}

func TestCtrlADoesNotActivatePrefixMode(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_ = mustRunCmd(t, cmd)

	if model.prefixActive {
		t.Fatal("expected ctrl-a to stop activating prefix mode")
	}
	if len(client.inputs) != 1 || string(client.inputs[0]) != "\x01" {
		t.Fatalf("expected ctrl-a to forward literal input, got %#v", client.inputs)
	}
}

func TestHelpCloseUsesSameControlFlowForKeyAndEvent(t *testing.T) {
	newModel := func() *Model {
		model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
		msg := mustRunCmd(t, model.Init())
		_, _ = model.Update(msg)
		model.showHelp = true
		return model
	}

	keyModel := newModel()
	_, cmd := keyModel.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("expected key help close to stay local, got %#v", cmd)
	}
	if keyModel.showHelp {
		t.Fatal("expected key path to close help")
	}

	eventModel := newModel()
	cmd = eventModel.handleKeyPressEvent(uv.KeyPressEvent{Code: uv.KeyEsc})
	if cmd != nil {
		t.Fatalf("expected event help close to stay local, got %#v", cmd)
	}
	if eventModel.showHelp {
		t.Fatal("expected event path to close help")
	}
}

func TestCtrlAUsesSameControlFlowForKeyAndEvent(t *testing.T) {
	newModel := func() (*Model, *fakeClient) {
		client := &fakeClient{}
		model := NewModel(client, Config{DefaultShell: "/bin/sh"})
		msg := mustRunCmd(t, model.Init())
		_, _ = model.Update(msg)
		return model, client
	}

	keyModel, keyClient := newModel()
	_, cmd := keyModel.handleKey(tea.KeyMsg{Type: tea.KeyCtrlA})
	if cmd == nil {
		t.Fatal("expected key ctrl-a to produce send command")
	}
	_ = mustRunCmd(t, cmd)
	if len(keyClient.inputs) == 0 || string(keyClient.inputs[len(keyClient.inputs)-1]) != "\x01" {
		t.Fatalf("expected key ctrl-a to forward literal input, got %#v", keyClient.inputs)
	}

	eventModel, eventClient := newModel()
	cmd = eventModel.handleKeyPressEvent(uv.KeyPressEvent{Code: 'a', Text: "a", Mod: uv.ModCtrl})
	if cmd == nil {
		t.Fatal("expected event ctrl-a to produce send command")
	}
	_ = mustRunCmd(t, cmd)
	if len(eventClient.inputs) == 0 || string(eventClient.inputs[len(eventClient.inputs)-1]) != "\x01" {
		t.Fatalf("expected event ctrl-a to forward literal input, got %#v", eventClient.inputs)
	}
}

func TestRawArrowKeysUseApplicationCursorModeWhenRequested(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	_, _ = pane.VTerm.Write([]byte("\x1b[?1h"))
	pane.live = true

	_, cmd := model.Update(rawInputMsg{data: []byte("\x1b[A")})
	_ = mustRunCmd(t, cmd)

	if len(client.inputs) != 1 {
		t.Fatalf("expected 1 input write, got %d", len(client.inputs))
	}
	if got := string(client.inputs[0]); got != "\x1bOA" {
		t.Fatalf("expected application cursor up sequence, got %q", got)
	}
}

func TestRawArrowKeysStayRawWhenApplicationCursorModeIsOff(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, cmd := model.Update(rawInputMsg{data: []byte("\x1b[A")})
	_ = mustRunCmd(t, cmd)

	if len(client.inputs) != 1 {
		t.Fatalf("expected 1 input write, got %d", len(client.inputs))
	}
	if got := string(client.inputs[0]); got != "\x1b[A" {
		t.Fatalf("expected raw cursor up sequence, got %q", got)
	}
}

func TestInputEventArrowUsesApplicationCursorModeWhenRequested(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	_, _ = pane.VTerm.Write([]byte("\x1b[?1h"))
	pane.live = true

	_, cmd := model.Update(inputEventMsg{event: uv.KeyPressEvent{Code: uv.KeyUp}})
	_ = mustRunCmd(t, cmd)

	if len(client.inputs) != 1 {
		t.Fatalf("expected 1 input write, got %d", len(client.inputs))
	}
	if got := string(client.inputs[0]); got != "\x1bOA" {
		t.Fatalf("expected application cursor up sequence, got %q", got)
	}
}

func TestInputEventTextPassesThroughViaVTerm(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, cmd := model.Update(inputEventMsg{event: uv.KeyPressEvent{Code: 'a', Text: "a"}})
	_ = mustRunCmd(t, cmd)

	if len(client.inputs) != 1 {
		t.Fatalf("expected 1 input write, got %d", len(client.inputs))
	}
	if got := string(client.inputs[0]); got != "a" {
		t.Fatalf("expected text input to be forwarded, got %q", got)
	}
}

func TestInputEventExtendedTextUsesTextPayload(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	text := "🩷"
	_, cmd := model.Update(inputEventMsg{event: uv.KeyPressEvent{Code: uv.KeyExtended, Text: text}})
	_ = mustRunCmd(t, cmd)

	if len(client.inputs) != 1 {
		t.Fatalf("expected 1 input write, got %d", len(client.inputs))
	}
	if got := string(client.inputs[0]); got != text {
		t.Fatalf("expected extended text input to be forwarded, got %q", got)
	}
}

func TestInputEventAltExtendedTextUsesEscapePrefix(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	text := "🩷"
	_, cmd := model.Update(inputEventMsg{event: uv.KeyPressEvent{Code: uv.KeyExtended, Text: text, Mod: uv.ModAlt}})
	_ = mustRunCmd(t, cmd)

	if len(client.inputs) != 1 {
		t.Fatalf("expected 1 input write, got %d", len(client.inputs))
	}
	if got := string(client.inputs[0]); got != "\x1b"+text {
		t.Fatalf("expected alt+extended text input to be forwarded with escape prefix, got %q", got)
	}
}

func TestInputEventSpecialKeysEncodeForPane(t *testing.T) {
	tests := []struct {
		name  string
		event uv.KeyPressEvent
		want  string
	}{
		{name: "shift-tab", event: uv.KeyPressEvent{Code: uv.KeyTab, Mod: uv.ModShift}, want: "\x1b[Z"},
		{name: "delete", event: uv.KeyPressEvent{Code: uv.KeyDelete}, want: "\x1b[3~"},
		{name: "home", event: uv.KeyPressEvent{Code: uv.KeyHome}, want: "\x1b[H"},
		{name: "end", event: uv.KeyPressEvent{Code: uv.KeyEnd}, want: "\x1b[F"},
		{name: "pgup", event: uv.KeyPressEvent{Code: uv.KeyPgUp}, want: "\x1b[5~"},
		{name: "pgdown", event: uv.KeyPressEvent{Code: uv.KeyPgDown}, want: "\x1b[6~"},
		{name: "f1", event: uv.KeyPressEvent{Code: uv.KeyF1}, want: "\x1bOP"},
		{name: "f5", event: uv.KeyPressEvent{Code: uv.KeyF5}, want: "\x1b[15~"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeClient{}
			model := NewModel(client, Config{DefaultShell: "/bin/sh"})

			msg := mustRunCmd(t, model.Init())
			_, _ = model.Update(msg)

			_, cmd := model.Update(inputEventMsg{event: tc.event})
			_ = mustRunCmd(t, cmd)

			if len(client.inputs) != 1 {
				t.Fatalf("expected 1 input write, got %d", len(client.inputs))
			}
			if got := string(client.inputs[0]); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestInputEventPasteUsesBracketedPasteWhenPaneRequestsIt(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	_, _ = pane.VTerm.Write([]byte("\x1b[?2004h"))
	pane.live = true

	_, cmd := model.Update(inputEventMsg{event: uv.PasteEvent{Content: "hello\nworld"}})
	_ = mustRunCmd(t, cmd)

	if len(client.inputs) != 1 {
		t.Fatalf("expected 1 input write, got %d", len(client.inputs))
	}
	if got, want := string(client.inputs[0]), "\x1b[200~hello\nworld\x1b[201~"; got != want {
		t.Fatalf("expected bracketed paste %q, got %q", want, got)
	}
}

func TestPrefixTimeoutClearsPrefixMode(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.prefixTimeout = time.Millisecond

	cmd := activatePrefixForTest(model)
	if !model.prefixActive {
		t.Fatal("expected prefix mode to be active")
	}
	msg := mustRunCmd(t, cmd)
	_, _ = model.Update(msg)
	if model.prefixActive {
		t.Fatal("expected prefix timeout to clear prefix mode")
	}
}

func TestApplyPrefixStateTransitionClearResetsModeFlags(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.prefixActive = true
	model.directMode = true
	model.prefixMode = prefixModeFloating
	model.prefixFallback = prefixFallbackFloatingCreate

	cmd := model.applyPrefixStateTransition(prefixStateTransition{kind: prefixStateTransitionClear})
	if cmd != nil {
		t.Fatalf("expected clear transition to stay synchronous, got %v", cmd != nil)
	}
	if model.prefixActive || model.directMode {
		t.Fatalf("expected clear transition to reset active/direct flags, active=%v direct=%v", model.prefixActive, model.directMode)
	}
	if model.prefixMode != prefixModeRoot {
		t.Fatalf("expected clear transition to reset prefix mode, got %v", model.prefixMode)
	}
	if model.prefixFallback != prefixFallbackNone {
		t.Fatalf("expected clear transition to reset fallback, got %v", model.prefixFallback)
	}
}

func TestApplyPrefixStateTransitionEnterFloatingFocusesTopFloatingPane(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.workspace = Workspace{
		Tabs: []*Tab{
			{
				Panes: map[string]*Pane{
					"pane-1": {ID: "pane-1", Viewport: &Viewport{}},
					"pane-2": {ID: "pane-2", Viewport: &Viewport{}},
					"pane-3": {ID: "pane-3", Viewport: &Viewport{}},
				},
				FloatingVisible: true,
				Floating: []*FloatingPane{
					{PaneID: "pane-2", Z: 1},
					{PaneID: "pane-3", Z: 2},
				},
				ActivePaneID: "pane-1",
			},
		},
	}

	cmd := model.applyPrefixStateTransition(prefixStateTransition{
		kind:     prefixStateTransitionEnter,
		mode:     prefixModeFloating,
		fallback: prefixFallbackFloatingCreate,
	})
	if cmd == nil {
		t.Fatal("expected entering floating mode to arm prefix timeout")
	}
	if !model.prefixActive || model.directMode {
		t.Fatalf("expected floating enter to enable prefix mode without direct mode, active=%v direct=%v", model.prefixActive, model.directMode)
	}
	if model.prefixMode != prefixModeFloating {
		t.Fatalf("expected floating mode after transition, got %v", model.prefixMode)
	}
	if model.prefixFallback != prefixFallbackFloatingCreate {
		t.Fatalf("expected floating enter to preserve fallback, got %v", model.prefixFallback)
	}
	if got := model.currentTab().ActivePaneID; got != "pane-3" {
		t.Fatalf("expected floating transition to focus topmost floating pane, got %q", got)
	}
}

func TestApplyPrefixStateTransitionEnterDirectModeArmsFreshTimeoutSeq(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.prefixTimeout = 5 * time.Millisecond
	initialSeq := model.prefixSeq

	cmd := model.applyPrefixStateTransition(prefixStateTransition{
		kind:   prefixStateTransitionEnter,
		mode:   prefixModeResize,
		direct: true,
	})
	if cmd == nil {
		t.Fatal("expected direct enter to arm prefix timeout")
	}
	if !model.prefixActive || !model.directMode {
		t.Fatalf("expected direct enter to enable prefix and direct mode, active=%v direct=%v", model.prefixActive, model.directMode)
	}
	if model.prefixMode != prefixModeResize {
		t.Fatalf("expected resize mode after direct enter, got %v", model.prefixMode)
	}
	if model.prefixSeq <= initialSeq {
		t.Fatalf("expected direct enter to bump prefix sequence, before=%d after=%d", initialSeq, model.prefixSeq)
	}
}

func TestDirectModeTimeoutClearsMode(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.prefixTimeout = time.Millisecond

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if !model.prefixActive || model.prefixMode != prefixModePane {
		t.Fatalf("expected pane mode to activate, active=%v mode=%v", model.prefixActive, model.prefixMode)
	}
	msg := mustRunCmd(t, cmd)
	_, _ = model.Update(msg)
	if model.prefixActive {
		t.Fatal("expected direct pane mode to clear after timeout")
	}
}

func TestResizeModeActionRearmsTimeout(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.prefixTimeout = 5 * time.Millisecond

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if !model.prefixActive || model.prefixMode != prefixModeResize {
		t.Fatalf("expected resize mode to activate, active=%v mode=%v", model.prefixActive, model.prefixMode)
	}
	initialSeq := model.prefixSeq
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if !model.prefixActive {
		t.Fatal("expected resize action to keep mode active")
	}
	if model.prefixSeq <= initialSeq {
		t.Fatalf("expected resize action to rearm timeout, seq before=%d after=%d", initialSeq, model.prefixSeq)
	}
	rearmedSeq := model.prefixSeq
	_, _ = model.Update(prefixTimeoutMsg{seq: initialSeq})
	if !model.prefixActive {
		t.Fatal("expected original timer to be ignored after resize rearm")
	}
	msg := mustRunCmd(t, cmd)
	_ = msg
	_, _ = model.Update(prefixTimeoutMsg{seq: rearmedSeq})
	if model.prefixActive {
		t.Fatal("expected resize mode to clear after rearmed timeout")
	}
}

func TestResizeModeAcquireKeepsModeActiveForKeyAndEvent(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	result := model.dispatchResizeModeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if !result.keep || !result.rearm || result.cmd == nil {
		t.Fatalf("expected key acquire to keep and rearm resize mode, got %#v", result)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	eventResult := model.dispatchResizeModeEvent(uv.KeyPressEvent{Code: 'a', Text: "a"})
	if !eventResult.keep || !eventResult.rearm || eventResult.cmd == nil {
		t.Fatalf("expected event acquire to keep and rearm resize mode, got %#v", eventResult)
	}
}

func TestResizeModeRuntimePlanForResizeAndAcquire(t *testing.T) {
	resizePlan := resizeModeRuntimePlanForAction(resizeModeAction{kind: resizeModeActionResize, direction: DirectionRight, amount: 4})
	if resizePlan.resizeDirection != DirectionRight || resizePlan.resizeAmount != 4 || !resizePlan.keep || !resizePlan.rearm {
		t.Fatalf("expected resize action plan to capture direction/amount and keep/rearm, got %#v", resizePlan)
	}
	acquirePlan := resizeModeRuntimePlanForAction(resizeModeAction{kind: resizeModeActionAcquire})
	if !acquirePlan.acquire || !acquirePlan.keep || !acquirePlan.rearm {
		t.Fatalf("expected acquire action plan to keep/rearm and request acquire, got %#v", acquirePlan)
	}
}

func TestResizeModeRuntimePlanForBalanceAndCycleLayout(t *testing.T) {
	balancePlan := resizeModeRuntimePlanForAction(resizeModeAction{kind: resizeModeActionBalance})
	if !balancePlan.balance || !balancePlan.keep || !balancePlan.rearm {
		t.Fatalf("expected balance action plan to keep/rearm and request rebalance, got %#v", balancePlan)
	}
	cyclePlan := resizeModeRuntimePlanForAction(resizeModeAction{kind: resizeModeActionCycleLayout})
	if !cyclePlan.cycleLayout || !cyclePlan.keep || !cyclePlan.rearm {
		t.Fatalf("expected cycle-layout action plan to keep/rearm and request cycle, got %#v", cyclePlan)
	}
}

func TestResizeModeRuntimePlanForUnknownKeepsMode(t *testing.T) {
	plan := resizeModeRuntimePlanForAction(resizeModeAction{})
	if !plan.keep || !plan.rearm {
		t.Fatalf("expected unknown resize action to keep and rearm mode, got %#v", plan)
	}
}

func TestActivePrefixKeyAndEventClearStateOnOneShotAction(t *testing.T) {
	newModel := func() *Model {
		model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
		msg := mustRunCmd(t, model.Init())
		_, _ = model.Update(msg)
		model.prefixActive = true
		model.prefixMode = prefixModeGlobal
		return model
	}

	keyModel := newModel()
	cmd := keyModel.handleActivePrefixKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if cmd != nil {
		t.Fatalf("expected key help action to stay local, got %#v", cmd)
	}
	if keyModel.prefixActive {
		t.Fatal("expected key one-shot action to clear prefix state")
	}

	eventModel := newModel()
	cmd = eventModel.handleActivePrefixEvent(uv.KeyPressEvent{Code: '?', Text: "?"})
	if cmd != nil {
		t.Fatalf("expected event help action to stay local, got %#v", cmd)
	}
	if eventModel.prefixActive {
		t.Fatal("expected event one-shot action to clear prefix state")
	}
}

func TestActivePrefixKeyAndEventRearmStickyAction(t *testing.T) {
	newModel := func() *Model {
		model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
		msg := mustRunCmd(t, model.Init())
		_, _ = model.Update(msg)
		model.prefixActive = true
		model.prefixMode = prefixModeOffsetPan
		model.prefixTimeout = 5 * time.Millisecond
		return model
	}

	keyModel := newModel()
	initialSeq := keyModel.prefixSeq
	cmd := keyModel.handleActivePrefixKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if !keyModel.prefixActive {
		t.Fatal("expected key sticky action to keep prefix active")
	}
	if keyModel.prefixSeq <= initialSeq {
		t.Fatalf("expected key sticky action to rearm prefix timeout, seq before=%d after=%d", initialSeq, keyModel.prefixSeq)
	}
	if cmd == nil {
		t.Fatal("expected key sticky action to arm timeout")
	}

	eventModel := newModel()
	initialSeq = eventModel.prefixSeq
	cmd = eventModel.handleActivePrefixEvent(uv.KeyPressEvent{Code: 'l', Text: "l"})
	if !eventModel.prefixActive {
		t.Fatal("expected event sticky action to keep prefix active")
	}
	if eventModel.prefixSeq <= initialSeq {
		t.Fatalf("expected event sticky action to rearm prefix timeout, seq before=%d after=%d", initialSeq, eventModel.prefixSeq)
	}
	if cmd == nil {
		t.Fatal("expected event sticky action to arm timeout")
	}
}

func TestResizeModeKeyAndEventShareDirectionalBehavior(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	keyResult := model.dispatchResizeModeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	if !keyResult.keep || !keyResult.rearm || keyResult.cmd == nil {
		t.Fatalf("expected key resize action to keep and rearm, got %#v", keyResult)
	}

	eventResult := model.dispatchResizeModeEvent(uv.KeyPressEvent{Code: 'L', Text: "L", Mod: uv.ModShift})
	if !eventResult.keep || !eventResult.rearm || eventResult.cmd == nil {
		t.Fatalf("expected event resize action to keep and rearm, got %#v", eventResult)
	}
}

func TestTabModeKeyAndEventShareRenameBehavior(t *testing.T) {
	newModel := func() *Model {
		model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
		msg := mustRunCmd(t, model.Init())
		_, _ = model.Update(msg)
		return model
	}

	keyModel := newModel()
	keyResult := keyModel.dispatchTabSubPrefixKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}})
	if keyResult.keep {
		t.Fatalf("expected key rename to be one-shot, got %#v", keyResult)
	}
	if keyModel.prompt == nil || keyModel.prompt.Title != "rename tab" {
		t.Fatalf("expected key rename to open tab prompt, got %#v", keyModel.prompt)
	}

	eventModel := newModel()
	eventResult := eventModel.dispatchTabSubPrefixEvent(uv.KeyPressEvent{Code: ',', Text: ","})
	if eventResult.keep {
		t.Fatalf("expected event rename to be one-shot, got %#v", eventResult)
	}
	if eventModel.prompt == nil || eventModel.prompt.Title != "rename tab" {
		t.Fatalf("expected event rename to open tab prompt, got %#v", eventModel.prompt)
	}
}

func TestTabModeKeyAndEventShareDirectDigitJump(t *testing.T) {
	newModel := func() *Model {
		model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
		msg := mustRunCmd(t, model.Init())
		_, _ = model.Update(msg)
		model.workspace.Tabs = append(model.workspace.Tabs, newTab("two"))
		model.directMode = true
		model.prefixActive = true
		model.prefixMode = prefixModeTab
		return model
	}

	keyModel := newModel()
	keyResult := keyModel.dispatchTabSubPrefixKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if keyModel.workspace.ActiveTab != 1 {
		t.Fatalf("expected key direct digit jump to select tab 2, got %d", keyModel.workspace.ActiveTab)
	}
	if keyResult.keep {
		t.Fatalf("expected key direct digit jump to be one-shot, got %#v", keyResult)
	}

	eventModel := newModel()
	eventResult := eventModel.dispatchTabSubPrefixEvent(uv.KeyPressEvent{Code: '2', Text: "2"})
	if eventModel.workspace.ActiveTab != 1 {
		t.Fatalf("expected event direct digit jump to select tab 2, got %d", eventModel.workspace.ActiveTab)
	}
	if eventResult.keep {
		t.Fatalf("expected event direct digit jump to be one-shot, got %#v", eventResult)
	}
}

func TestTabModeRuntimePlanForNew(t *testing.T) {
	plan := tabModeRuntimePlanForAction(tabModeAction{kind: tabModeActionNew}, 2, 0)
	if plan.keep {
		t.Fatalf("expected new action to remain one-shot, got %#v", plan)
	}
	if !plan.openNew {
		t.Fatalf("expected new action to request new-tab picker, got %#v", plan)
	}
}

func TestTabModeRuntimePlanForRename(t *testing.T) {
	plan := tabModeRuntimePlanForAction(tabModeAction{kind: tabModeActionRename}, 2, 0)
	if plan.keep {
		t.Fatalf("expected rename action to remain one-shot, got %#v", plan)
	}
	if !plan.beginRename {
		t.Fatalf("expected rename action to request rename prompt, got %#v", plan)
	}
}

func TestTabModeRuntimePlanForNextAndJump(t *testing.T) {
	nextPlan := tabModeRuntimePlanForAction(tabModeAction{kind: tabModeActionNext}, 3, 1)
	if nextPlan.activateIndex != 2 {
		t.Fatalf("expected next action to target index 2, got %#v", nextPlan)
	}
	jumpPlan := tabModeRuntimePlanForAction(tabModeAction{kind: tabModeActionJump, index: 2}, 3, 0)
	if jumpPlan.activateIndex != 2 {
		t.Fatalf("expected jump action to target index 2, got %#v", jumpPlan)
	}
}

func TestTabModeRuntimePlanForUnknownKeepsDirectModeOnly(t *testing.T) {
	plan := tabModeRuntimePlanForAction(tabModeAction{}, 2, 0)
	if !plan.keep {
		t.Fatalf("expected unknown tab action plan to request keep handling, got %#v", plan)
	}
}

func TestApplyTabModeActionUnknownKeepsModeInBothDirectAndNormal(t *testing.T) {
	newModel := func() *Model {
		model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
		msg := mustRunCmd(t, model.Init())
		_, _ = model.Update(msg)
		model.prefixActive = true
		model.prefixMode = prefixModeTab
		return model
	}

	normalModel := newModel()
	normalResult := normalModel.applyTabModeAction(tabModeAction{})
	if !normalResult.keep || normalResult.rearm {
		t.Fatalf("expected normal unknown tab action to keep mode without rearm, got %#v", normalResult)
	}

	directModel := newModel()
	directModel.directMode = true
	directResult := directModel.applyTabModeAction(tabModeAction{})
	if !directResult.keep || directResult.rearm {
		t.Fatalf("expected direct unknown tab action to keep mode without rearm, got %#v", directResult)
	}
}

func TestWorkspaceModeKeyAndEventShareRenameBehavior(t *testing.T) {
	newModel := func() *Model {
		model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh", Workspace: "main"})
		msg := mustRunCmd(t, model.Init())
		_, _ = model.Update(msg)
		return model
	}

	keyModel := newModel()
	keyResult := keyModel.dispatchWorkspaceSubPrefixKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if keyResult.keep {
		t.Fatalf("expected key workspace rename to be one-shot, got %#v", keyResult)
	}
	if keyModel.prompt == nil || keyModel.prompt.Title != "rename workspace" {
		t.Fatalf("expected key workspace rename prompt, got %#v", keyModel.prompt)
	}

	eventModel := newModel()
	eventResult := eventModel.dispatchWorkspaceSubPrefixEvent(uv.KeyPressEvent{Code: 'r', Text: "r"})
	if eventResult.keep {
		t.Fatalf("expected event workspace rename to be one-shot, got %#v", eventResult)
	}
	if eventModel.prompt == nil || eventModel.prompt.Title != "rename workspace" {
		t.Fatalf("expected event workspace rename prompt, got %#v", eventModel.prompt)
	}
}

func TestWorkspaceModeKeyAndEventShareDirectCreateBehavior(t *testing.T) {
	newModel := func() *Model {
		model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh", Workspace: "main"})
		msg := mustRunCmd(t, model.Init())
		_, _ = model.Update(msg)
		model.directMode = true
		model.prefixActive = true
		model.prefixMode = prefixModeWorkspace
		return model
	}

	keyModel := newModel()
	keyResult := keyModel.dispatchWorkspaceSubPrefixKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if keyResult.keep || keyResult.cmd == nil {
		t.Fatalf("expected key workspace create to be one-shot with command, got %#v", keyResult)
	}

	eventModel := newModel()
	eventResult := eventModel.dispatchWorkspaceSubPrefixEvent(uv.KeyPressEvent{Code: 'c', Text: "c"})
	if eventResult.keep || eventResult.cmd == nil {
		t.Fatalf("expected event workspace create to be one-shot with command, got %#v", eventResult)
	}
}

func TestWorkspaceModeRuntimePlanForPicker(t *testing.T) {
	plan := workspaceModeRuntimePlanForAction(workspaceModeAction{kind: workspaceModeActionPicker}, "workspace-2")
	if plan.keep {
		t.Fatalf("expected picker action to remain one-shot, got %#v", plan)
	}
	if !plan.openPicker {
		t.Fatalf("expected picker action to request workspace picker, got %#v", plan)
	}
}

func TestWorkspaceModeRuntimePlanForCreateUsesSuggestedName(t *testing.T) {
	plan := workspaceModeRuntimePlanForAction(workspaceModeAction{kind: workspaceModeActionCreate}, "workspace-9")
	if plan.keep {
		t.Fatalf("expected create action to remain one-shot, got %#v", plan)
	}
	if plan.createName != "workspace-9" {
		t.Fatalf("expected create action to capture suggested workspace name, got %#v", plan)
	}
}

func TestWorkspaceModeRuntimePlanForRenameIsLocal(t *testing.T) {
	plan := workspaceModeRuntimePlanForAction(workspaceModeAction{kind: workspaceModeActionRename}, "workspace-2")
	if plan.keep {
		t.Fatalf("expected rename action to remain one-shot, got %#v", plan)
	}
	if !plan.beginRename {
		t.Fatalf("expected rename action to request rename prompt, got %#v", plan)
	}
}

func TestWorkspaceModeRuntimePlanForUnknownActionKeepsMode(t *testing.T) {
	plan := workspaceModeRuntimePlanForAction(workspaceModeAction{}, "workspace-2")
	if !plan.keep {
		t.Fatalf("expected unknown workspace action to keep mode active, got %#v", plan)
	}
	if plan.openPicker || plan.beginRename || plan.createName != "" {
		t.Fatalf("expected unknown workspace action to stay inert, got %#v", plan)
	}
}

func TestApplyWorkspaceModeActionUnknownKeepsModeInBothDirectAndNormal(t *testing.T) {
	newModel := func() *Model {
		model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh", Workspace: "main"})
		msg := mustRunCmd(t, model.Init())
		_, _ = model.Update(msg)
		model.prefixActive = true
		model.prefixMode = prefixModeWorkspace
		return model
	}

	normalModel := newModel()
	normalResult := normalModel.applyWorkspaceModeAction(workspaceModeAction{})
	if !normalResult.keep || normalResult.rearm {
		t.Fatalf("expected normal unknown workspace action to keep mode without rearm, got %#v", normalResult)
	}

	directModel := newModel()
	directModel.directMode = true
	directResult := directModel.applyWorkspaceModeAction(workspaceModeAction{})
	if !directResult.keep || directResult.rearm {
		t.Fatalf("expected direct unknown workspace action to keep mode without rearm, got %#v", directResult)
	}
}

func TestViewportModeKeyAndEventShareAcquireBehavior(t *testing.T) {
	newModel := func() *Model {
		model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
		msg := mustRunCmd(t, model.Init())
		_, _ = model.Update(msg)
		model.directMode = true
		model.prefixActive = true
		model.prefixMode = prefixModeViewport
		return model
	}

	keyModel := newModel()
	keyResult := keyModel.dispatchViewportSubPrefixKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if keyResult.keep || keyResult.cmd == nil {
		t.Fatalf("expected key viewport acquire to be one-shot with command, got %#v", keyResult)
	}

	eventModel := newModel()
	eventResult := eventModel.dispatchViewportSubPrefixEvent(uv.KeyPressEvent{Code: 'a', Text: "a"})
	if eventResult.keep || eventResult.cmd == nil {
		t.Fatalf("expected event viewport acquire to be one-shot with command, got %#v", eventResult)
	}
}

func TestViewportModeRuntimePlanForAcquireAndToggle(t *testing.T) {
	acquirePlan := viewportModeRuntimePlanForAction(viewportModeAction{kind: viewportModeActionAcquire}, false)
	if !acquirePlan.acquire || acquirePlan.keep {
		t.Fatalf("expected acquire action to stay one-shot and request acquire, got %#v", acquirePlan)
	}
	togglePlan := viewportModeRuntimePlanForAction(viewportModeAction{kind: viewportModeActionToggleMode}, false)
	if !togglePlan.toggleMode || togglePlan.keep {
		t.Fatalf("expected toggle-mode action to stay one-shot and request toggle, got %#v", togglePlan)
	}
}

func TestViewportModeRuntimePlanForPanAndFollow(t *testing.T) {
	panPlan := viewportModeRuntimePlanForAction(viewportModeAction{kind: viewportModeActionPan, direction: DirectionRight}, false)
	if panPlan.panDirection != DirectionRight || !panPlan.keep {
		t.Fatalf("expected pan action to keep mode and capture direction, got %#v", panPlan)
	}
	followPlan := viewportModeRuntimePlanForAction(viewportModeAction{kind: viewportModeActionFollow}, false)
	if !followPlan.follow || followPlan.keep {
		t.Fatalf("expected follow action to be one-shot and request follow reset, got %#v", followPlan)
	}
}

func TestViewportModeRuntimePlanForOffsetModeAndUnknown(t *testing.T) {
	offsetPlan := viewportModeRuntimePlanForAction(viewportModeAction{kind: viewportModeActionOffsetMode}, false)
	if !offsetPlan.enterOffsetMode || !offsetPlan.keep {
		t.Fatalf("expected offset-mode action to keep mode and request offset-mode transition, got %#v", offsetPlan)
	}
	directUnknown := viewportModeRuntimePlanForAction(viewportModeAction{}, true)
	if !directUnknown.keep {
		t.Fatalf("expected unknown direct viewport action to keep mode active, got %#v", directUnknown)
	}
}

func TestFloatingModeKeyAndEventShareDirectPickerBehavior(t *testing.T) {
	newModel := func() *Model {
		model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
		msg := mustRunCmd(t, model.Init())
		_, _ = model.Update(msg)
		model.directMode = true
		model.prefixActive = true
		model.prefixMode = prefixModeFloating
		return model
	}

	keyModel := newModel()
	keyResult := keyModel.dispatchFloatingModeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if keyResult.keep || keyResult.cmd == nil {
		t.Fatalf("expected key floating picker to be one-shot with command, got %#v", keyResult)
	}

	eventModel := newModel()
	eventResult := eventModel.dispatchFloatingModeEvent(uv.KeyPressEvent{Code: 'f', Text: "f"})
	if eventResult.keep || eventResult.cmd == nil {
		t.Fatalf("expected event floating picker to be one-shot with command, got %#v", eventResult)
	}
}

func TestFloatingModeRuntimePlanForFocusNext(t *testing.T) {
	plan := floatingModeRuntimePlanForAction(floatingModeAction{kind: floatingModeActionFocusNext}, false)
	if !plan.focusNext || !plan.keep {
		t.Fatalf("expected focus-next to keep mode and request focus cycle, got %#v", plan)
	}
}

func TestFloatingModeRuntimePlanForNewAndPicker(t *testing.T) {
	newPlan := floatingModeRuntimePlanForAction(floatingModeAction{kind: floatingModeActionNew}, false)
	if !newPlan.openNew || newPlan.keep {
		t.Fatalf("expected new action to open floating picker one-shot, got %#v", newPlan)
	}
	pickerPlan := floatingModeRuntimePlanForAction(floatingModeAction{kind: floatingModeActionPicker}, false)
	if !pickerPlan.openPicker || pickerPlan.keep {
		t.Fatalf("expected picker action to open terminal picker one-shot, got %#v", pickerPlan)
	}
}

func TestFloatingModeRuntimePlanForMoveAndResize(t *testing.T) {
	movePlan := floatingModeRuntimePlanForAction(floatingModeAction{kind: floatingModeActionMove, direction: DirectionLeft}, false)
	if movePlan.moveDirection != DirectionLeft || !movePlan.keep {
		t.Fatalf("expected move action to keep mode and capture direction, got %#v", movePlan)
	}
	resizePlan := floatingModeRuntimePlanForAction(floatingModeAction{kind: floatingModeActionResize, direction: DirectionRight, amount: 4}, false)
	if resizePlan.resizeDirection != DirectionRight || resizePlan.resizeAmount != 4 || !resizePlan.keep {
		t.Fatalf("expected resize action to keep mode and capture resize request, got %#v", resizePlan)
	}
}

func TestFloatingModeRuntimePlanForUnknownStaysInert(t *testing.T) {
	plan := floatingModeRuntimePlanForAction(floatingModeAction{}, false)
	if plan.keep {
		t.Fatalf("expected unknown floating action plan to stay inert before direct-mode policy, got %#v", plan)
	}
}

func TestFloatingModeRuntimePlanForUnknownKeepsModeInDirectMode(t *testing.T) {
	plan := floatingModeRuntimePlanForAction(floatingModeAction{}, true)
	if !plan.keep {
		t.Fatalf("expected unknown direct floating action to keep mode active, got %#v", plan)
	}
}

func TestApplyFloatingModeActionUnknownKeepsModeOnlyInDirectMode(t *testing.T) {
	newModel := func() *Model {
		model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
		msg := mustRunCmd(t, model.Init())
		_, _ = model.Update(msg)
		model.prefixActive = true
		model.prefixMode = prefixModeFloating
		return model
	}

	normalModel := newModel()
	normalResult := normalModel.applyFloatingModeAction(floatingModeAction{})
	if normalResult.keep {
		t.Fatalf("expected normal unknown floating action to exit mode, got %#v", normalResult)
	}

	directModel := newModel()
	directModel.directMode = true
	directResult := directModel.applyFloatingModeAction(floatingModeAction{})
	if !directResult.keep || directResult.rearm {
		t.Fatalf("expected direct unknown floating action to keep mode without rearm, got %#v", directResult)
	}
}

func TestOffsetPanModeKeyAndEventShareRearmBehavior(t *testing.T) {
	newModel := func() *Model {
		model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
		msg := mustRunCmd(t, model.Init())
		_, _ = model.Update(msg)
		model.prefixActive = true
		model.prefixMode = prefixModeOffsetPan
		return model
	}

	keyModel := newModel()
	keyResult := keyModel.dispatchOffsetPanModeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if !keyResult.keep || !keyResult.rearm {
		t.Fatalf("expected key offset-pan move to keep and rearm, got %#v", keyResult)
	}

	eventModel := newModel()
	eventResult := eventModel.dispatchOffsetPanModeEvent(uv.KeyPressEvent{Code: 'l', Text: "l"})
	if !eventResult.keep || !eventResult.rearm {
		t.Fatalf("expected event offset-pan move to keep and rearm, got %#v", eventResult)
	}
}

func TestOffsetPanModeRuntimePlanForPanAndJump(t *testing.T) {
	panPlan := offsetPanModeRuntimePlanForAction(offsetPanModeAction{kind: offsetPanModeActionPan, direction: DirectionLeft})
	if panPlan.panDirection != DirectionLeft || !panPlan.keep || !panPlan.rearm {
		t.Fatalf("expected offset-pan plan to capture pan direction and keep/rearm, got %#v", panPlan)
	}
	jumpPlan := offsetPanModeRuntimePlanForAction(offsetPanModeAction{kind: offsetPanModeActionJumpBottom})
	if !jumpPlan.jumpBottom || !jumpPlan.keep || !jumpPlan.rearm {
		t.Fatalf("expected offset-pan jump-bottom plan to keep/rearm, got %#v", jumpPlan)
	}
}

func TestOffsetPanModeRuntimePlanForUnknownStaysInert(t *testing.T) {
	plan := offsetPanModeRuntimePlanForAction(offsetPanModeAction{})
	if plan.keep || plan.rearm {
		t.Fatalf("expected unknown offset-pan action plan to stay inert, got %#v", plan)
	}
}

func TestApplyViewportNavigationRuntimePlanPanUpdatesOffset(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.width = 80
	model.height = 24
	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := activePane(model.currentTab())
	if pane == nil {
		t.Fatal("expected active pane")
	}
	tab := model.currentTab()
	viewW, viewH, ok := model.paneViewportSizeInTab(tab, pane.ID)
	if !ok {
		t.Fatal("expected viewport size")
	}
	contentW, _ := paneContentSize(pane)
	expectedX := min(4, max(0, contentW-viewW))

	result := model.applyViewportNavigationRuntimePlan(viewportNavigationRuntimePlan{
		panDirection: DirectionRight,
		keep:         true,
		rearm:        true,
	})
	if pane.Offset != (Point{X: expectedX, Y: 0}) {
		t.Fatalf("expected pan plan to move offset right, got %+v", pane.Offset)
	}
	if !result.keep || !result.rearm {
		t.Fatalf("expected pan plan result to preserve keep/rearm, got %#v", result)
	}
	_ = viewH
}

func TestApplyViewportNavigationRuntimePlanJumpBottomUsesSharedLogic(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.width = 80
	model.height = 24
	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := activePane(model.currentTab())
	if pane == nil {
		t.Fatal("expected active pane")
	}
	tab := model.currentTab()
	_, viewH, ok := model.paneViewportSizeInTab(tab, pane.ID)
	if !ok {
		t.Fatal("expected viewport size")
	}
	_, contentH := paneContentSize(pane)
	expectedY := max(0, contentH-viewH)

	result := model.applyViewportNavigationRuntimePlan(viewportNavigationRuntimePlan{
		jumpBottom: true,
		keep:       true,
		rearm:      true,
	})
	if pane.Offset.Y != expectedY {
		t.Fatalf("expected jump-bottom plan to use shared max offset, got %+v", pane.Offset)
	}
	if !result.keep || !result.rearm {
		t.Fatalf("expected jump-bottom plan result to preserve keep/rearm, got %#v", result)
	}
}

func TestViewportOffsetModeKeyAndEventShareTransitionBehavior(t *testing.T) {
	newModel := func() *Model {
		model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
		msg := mustRunCmd(t, model.Init())
		_, _ = model.Update(msg)
		model.prefixActive = true
		model.prefixMode = prefixModeViewport
		return model
	}

	keyModel := newModel()
	keyResult := keyModel.dispatchViewportSubPrefixKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if !keyResult.keep || keyResult.cmd != nil || keyResult.state.kind != prefixStateTransitionEnter || keyResult.state.mode != prefixModeOffsetPan {
		t.Fatalf("expected key viewport offset command to return offset-pan transition, got %#v", keyResult)
	}
	if got := keyModel.ActiveModeForTest(); got != "connection" {
		t.Fatalf("expected key dispatch to remain in current mode before apply, got %q", got)
	}
	_ = keyModel.applyActivePrefixResult(keyResult)
	if got := keyModel.ActiveModeForTest(); got != "offset-pan" {
		t.Fatalf("expected key apply to enter offset-pan mode, got %q", got)
	}

	eventModel := newModel()
	eventResult := eventModel.dispatchViewportSubPrefixEvent(uv.KeyPressEvent{Code: 'o', Text: "o"})
	if !eventResult.keep || eventResult.cmd != nil || eventResult.state.kind != prefixStateTransitionEnter || eventResult.state.mode != prefixModeOffsetPan {
		t.Fatalf("expected event viewport offset command to return offset-pan transition, got %#v", eventResult)
	}
	if got := eventModel.ActiveModeForTest(); got != "connection" {
		t.Fatalf("expected event dispatch to remain in current mode before apply, got %q", got)
	}
	_ = eventModel.applyActivePrefixResult(eventResult)
	if got := eventModel.ActiveModeForTest(); got != "offset-pan" {
		t.Fatalf("expected event apply to enter offset-pan mode, got %q", got)
	}
}

func TestGlobalModeKeyAndEventShareManagerBehavior(t *testing.T) {
	newModel := func() *Model {
		model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
		msg := mustRunCmd(t, model.Init())
		_, _ = model.Update(msg)
		model.prefixActive = true
		model.prefixMode = prefixModeGlobal
		return model
	}

	keyModel := newModel()
	keyResult := keyModel.dispatchGlobalModeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if keyResult.keep || keyResult.cmd == nil {
		t.Fatalf("expected key global manager action to be one-shot with command, got %#v", keyResult)
	}

	eventModel := newModel()
	eventResult := eventModel.dispatchGlobalModeEvent(uv.KeyPressEvent{Code: 't', Text: "t"})
	if eventResult.keep || eventResult.cmd == nil {
		t.Fatalf("expected event global manager action to be one-shot with command, got %#v", eventResult)
	}
}

func TestGlobalModeRuntimePlanForActionHelp(t *testing.T) {
	plan := globalModeRuntimePlanForAction(globalModeAction{kind: globalModeActionHelp})
	if !plan.showHelp {
		t.Fatalf("expected help action to request help, got %#v", plan)
	}
	if plan.keep {
		t.Fatalf("expected help action to remain one-shot, got %#v", plan)
	}
	if plan.cmd != nil {
		t.Fatalf("expected help action to stay local, got %#v", plan)
	}
}

func TestGlobalModeRuntimePlanForActionCommand(t *testing.T) {
	plan := globalModeRuntimePlanForAction(globalModeAction{kind: globalModeActionCommand})
	if !plan.beginCommand {
		t.Fatalf("expected command action to request prompt, got %#v", plan)
	}
	if plan.keep {
		t.Fatalf("expected command action to remain one-shot, got %#v", plan)
	}
}

func TestGlobalModeRuntimePlanForActionQuit(t *testing.T) {
	plan := globalModeRuntimePlanForAction(globalModeAction{kind: globalModeActionQuit})
	if !plan.quit {
		t.Fatalf("expected quit action to request quitting, got %#v", plan)
	}
	if plan.cmd == nil {
		t.Fatal("expected quit action to carry quit command")
	}
	if plan.keep {
		t.Fatalf("expected quit action to remain one-shot, got %#v", plan)
	}
}

func TestGlobalModeRuntimePlanForUnknownActionKeepsMode(t *testing.T) {
	plan := globalModeRuntimePlanForAction(globalModeAction{})
	if !plan.keep {
		t.Fatalf("expected unknown global action to keep mode active, got %#v", plan)
	}
	if plan.showHelp || plan.beginCommand || plan.quit || plan.cmd != nil {
		t.Fatalf("expected unknown global action to stay inert, got %#v", plan)
	}
}

func TestPaneModeKeyAndEventShareRenameBehavior(t *testing.T) {
	newModel := func() *Model {
		model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
		msg := mustRunCmd(t, model.Init())
		_, _ = model.Update(msg)
		model.prefixActive = true
		model.prefixMode = prefixModePane
		return model
	}

	keyModel := newModel()
	keyResult := keyModel.dispatchPaneModeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}})
	if keyResult.keep {
		t.Fatalf("expected key pane rename to be one-shot, got %#v", keyResult)
	}
	if keyModel.prompt == nil || keyModel.prompt.Title != "rename tab" {
		t.Fatalf("expected key pane rename prompt, got %#v", keyModel.prompt)
	}

	eventModel := newModel()
	eventResult := eventModel.dispatchPaneModeEvent(uv.KeyPressEvent{Code: ',', Text: ","})
	if eventResult.keep {
		t.Fatalf("expected event pane rename to be one-shot, got %#v", eventResult)
	}
	if eventModel.prompt == nil || eventModel.prompt.Title != "rename tab" {
		t.Fatalf("expected event pane rename prompt, got %#v", eventModel.prompt)
	}
}

func TestPaneModeKeyAndEventShareCloseBehavior(t *testing.T) {
	newModel := func() *Model {
		model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
		msg := mustRunCmd(t, model.Init())
		_, _ = model.Update(msg)
		createSplitPaneViaPicker(t, model, SplitVertical)
		model.prefixActive = true
		model.prefixMode = prefixModePane
		return model
	}

	keyModel := newModel()
	keyResult := keyModel.dispatchPaneModeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if keyResult.keep || keyResult.cmd == nil {
		t.Fatalf("expected key pane close to be one-shot with command, got %#v", keyResult)
	}

	eventModel := newModel()
	eventResult := eventModel.dispatchPaneModeEvent(uv.KeyPressEvent{Code: 'x', Text: "x"})
	if eventResult.keep || eventResult.cmd == nil {
		t.Fatalf("expected event pane close to be one-shot with command, got %#v", eventResult)
	}
}

func TestPrefixTabSubPrefixRenameOpensPrompt(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if !model.prefixActive || model.prefixMode != prefixModeTab {
		t.Fatalf("expected tab sub-prefix mode, active=%v mode=%v", model.prefixActive, model.prefixMode)
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}})
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}
	if model.prompt == nil || model.prompt.Title != "rename tab" {
		t.Fatalf("expected tab sub-prefix rename to open prompt, got %#v", model.prompt)
	}
	if model.prefixActive {
		t.Fatal("expected tab sub-prefix to clear after one-shot action")
	}
}

func TestPrefixWorkspaceSubPrefixCreateActivatesNewWorkspace(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", Workspace: "main"})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	if !model.prefixActive || model.prefixMode != prefixModeWorkspace {
		t.Fatalf("expected workspace sub-prefix mode, active=%v mode=%v", model.prefixActive, model.prefixMode)
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	if model.workspace.Name != "workspace-2" {
		t.Fatalf("expected workspace-2 after workspace sub-prefix create, got %q", model.workspace.Name)
	}
	if model.activeWorkspace != 1 {
		t.Fatalf("expected active workspace index 1, got %d", model.activeWorkspace)
	}
}

func TestPrefixWorkspaceSubPrefixTimeoutFallsBackToFloatingChooser(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40
	model.prefixTimeout = time.Millisecond

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	timeoutMsg := mustRunCmd(t, cmd)
	_, cmd = model.Update(timeoutMsg)
	if cmd == nil {
		t.Fatal("expected workspace sub-prefix timeout to run legacy fallback")
	}
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if model.terminalPicker == nil || model.terminalPicker.Title != "Open Floating Pane" {
		t.Fatalf("expected legacy C-a w fallback to floating chooser, got %#v", model.terminalPicker)
	}
}

func TestWorkspaceDeleteShowsNoticeWhenOnlyOneWorkspaceExists(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", Workspace: "main"})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	if !model.prefixActive || model.prefixMode != prefixModeWorkspace {
		t.Fatalf("expected workspace mode, active=%v mode=%v", model.prefixActive, model.prefixMode)
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd != nil {
		t.Fatalf("expected last-workspace delete to stay local, got %#v", cmd)
	}
	if model.err == nil || !strings.Contains(model.err.Error(), "cannot delete the last workspace") {
		t.Fatalf("expected last-workspace delete notice, got err=%v notice=%q", model.err, model.notice)
	}
	if model.workspace.Name != "main" || len(model.workspaceOrder) != 1 {
		t.Fatalf("expected workspace to remain unchanged, got name=%q order=%v", model.workspace.Name, model.workspaceOrder)
	}
}

func TestPrefixWorkspaceSubPrefixRenameAndDelete(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", Workspace: "main"})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	_, cmd = model.Update(workspaceActivatedMsg{
		workspace: Workspace{Name: "workspace-2", Tabs: []*Tab{newTab("1")}, ActiveTab: 0},
		index:     1,
		notice:    "workspace: workspace-2",
	})
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}
	model.workspaceStore["workspace-2"] = model.workspace
	model.workspaceOrder = []string{"main", "workspace-2"}
	model.activeWorkspace = 1

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}
	if model.prompt == nil || model.prompt.Title != "rename workspace" {
		t.Fatalf("expected workspace rename prompt, got %#v", model.prompt)
	}
	model.prompt.Value = "dev"
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}
	if model.workspace.Name != "dev" || model.workspaceOrder[1] != "dev" {
		t.Fatalf("expected workspace rename to update active workspace, got name=%q order=%v", model.workspace.Name, model.workspaceOrder)
	}
	if model.activeWorkspace != 1 {
		t.Fatalf("expected renamed workspace to remain active at index 1, got %d", model.activeWorkspace)
	}
	if main := model.workspaceStore["main"]; main.Name != "main" {
		t.Fatalf("expected rename to preserve stored main workspace, got %#v", main)
	}
	if renamed := model.workspaceStore["dev"]; renamed.Name != "dev" {
		t.Fatalf("expected rename to store renamed workspace under new name, got %#v", renamed)
	}
	if _, exists := model.workspaceStore["workspace-2"]; exists {
		t.Fatalf("expected rename to prune stale workspace-2 entry, store=%v", model.workspaceStore)
	}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}
	if model.workspace.Name != "main" {
		t.Fatalf("expected workspace delete to switch back to main, got %q", model.workspace.Name)
	}
	if len(model.workspaceOrder) != 1 || model.workspaceOrder[0] != "main" {
		t.Fatalf("expected deleted workspace to be removed, got %v", model.workspaceOrder)
	}
	if _, exists := model.workspaceStore["dev"]; exists {
		t.Fatalf("expected delete to prune stale dev entry, store=%v", model.workspaceStore)
	}
}

func TestPrefixViewportSubPrefixToggleMode(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	pane := activePane(model.currentTab())
	if pane == nil || pane.Mode != ViewportModeFit {
		t.Fatalf("expected active fit pane before viewport sub-prefix toggle, got %#v", pane)
	}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane = activePane(model.currentTab())
	if pane == nil || pane.Mode != ViewportModeFixed {
		t.Fatalf("expected viewport sub-prefix to toggle fixed mode, got %#v", pane)
	}
	if model.prefixActive {
		t.Fatal("expected viewport sub-prefix to clear after one-shot action")
	}
}

func TestDirectModeTriggersAndStatus(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", IconSet: "ascii"})
	model.width = 200
	model.height = 32

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if !model.prefixActive || !model.directMode || model.prefixMode != prefixModePane {
		t.Fatalf("expected ctrl-p to enter pane mode, active=%v direct=%v mode=%v", model.prefixActive, model.directMode, model.prefixMode)
	}
	status := xansi.Strip(model.renderStatus())
	if !containsAll(status, "PANE", "<%> SPLIT", "<hjkl> FOCUS", "<Esc> EXIT") {
		t.Fatalf("expected pane mode status hints, got:\n%s", status)
	}
	if strings.Contains(status, "pane:") && strings.Index(status, "PANE") > strings.Index(status, "pane:") {
		t.Fatalf("expected shortcut hints to stay on the left of runtime state, got:\n%s", status)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.prefixActive {
		t.Fatal("expected esc to leave direct mode")
	}
	status = xansi.Strip(model.renderStatus())
	if !containsAll(status, "Ctrl", "▸", "<p> PANE", "<r> RESIZE", "<t> TAB", "<o> FLOAT", "<f> PICKER") {
		t.Fatalf("expected normal status mode bar, got:\n%s", status)
	}
	if strings.Contains(status, "pane:") && strings.Index(status, "Ctrl") > strings.Index(status, "pane:") {
		t.Fatalf("expected normal shortcut section to stay left of runtime state, got:\n%s", status)
	}
}

func TestRenderStatusHintsUseDirectionalShortcutBadges(t *testing.T) {
	got := xansi.Strip(renderStatusHints([]string{"[NORMAL]", "Ctrl-p pane", "Esc:exit", "prefix"}))
	if !containsAll(got, "Ctrl", "▸", "<p> PANE", "<Esc> EXIT", "PREFIX") {
		t.Fatalf("expected guided shortcut segments, got %q", got)
	}
	if strings.Contains(got, "") {
		t.Fatalf("expected status bar to avoid nerd-font arrows in safe mode, got %q", got)
	}
}

func TestStatusShortcutPartsDoNotShowExitedActionsInNormalBar(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 160
	model.height = 32

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	pane.TerminalState = "exited"
	status := xansi.Strip(model.renderStatus())
	if containsAny(status, "<r> RESTART", "<f> ATTACH", "<x> CLOSE") {
		t.Fatalf("expected exited quick actions to stay out of normal status bar, got:\n%s", status)
	}
	if !containsAll(status, "Ctrl", "▸", "<p> PANE", "<o> FLOAT") {
		t.Fatalf("expected normal ctrl shortcuts to remain visible, got:\n%s", status)
	}
}

func TestDrawPaneFrameUsesGreenActiveAndGrayInactiveBorders(t *testing.T) {
	activeCanvas := newComposedCanvas(32, 6)
	activeCanvas.drawPaneFrameWithTitle(Rect{X: 0, Y: 0, W: 32, H: 6}, "active", "run", true, false)
	active := activeCanvas.String()
	if !strings.Contains(active, "38;2;74;222;128") {
		t.Fatalf("expected active pane border to use green accent, got %q", active)
	}

	inactiveCanvas := newComposedCanvas(32, 6)
	inactiveCanvas.drawPaneFrameWithTitle(Rect{X: 0, Y: 0, W: 32, H: 6}, "inactive", "run", false, false)
	inactive := inactiveCanvas.String()
	if !strings.Contains(inactive, "38;2;209;213;219") {
		t.Fatalf("expected inactive pane border to use bright gray, got %q", inactive)
	}
}

func TestDrawPaneFrameKeepsTopRuleVisibleAroundTitleAndMeta(t *testing.T) {
	canvas := newComposedCanvas(40, 6)
	canvas.drawPaneFrameWithTitle(Rect{X: 0, Y: 0, W: 40, H: 6}, "shell-1", "live fit", true, false)
	firstLine := strings.Split(xansi.Strip(canvas.String()), "\n")[0]
	if !containsAll(firstLine, "shell-1", "live fit", "┌─", "─┐") {
		t.Fatalf("expected top border to keep visible rule around title and meta, got %q", firstLine)
	}
}

func TestRenderStatusKeepsAccessFlagsOutOfRuntimeSummary(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", IconSet: "ascii"})
	model.width = 160
	model.height = 32

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	pane.AttachMode = "observer"
	pane.Readonly = true

	status := xansi.Strip(model.renderStatus())
	if !containsAll(status, "shell-1", "tiled", "live") {
		t.Fatalf("expected runtime status to keep only compact focus summary, got:\n%s", status)
	}
	if containsAll(status, "obs", "ro") {
		t.Fatalf("expected observer/readonly to stay in pane title badges, got:\n%s", status)
	}
}

func TestPaneTitleAddsObserverAndReadonlyBadges(t *testing.T) {
	pane := &Pane{
		ID:    "pane-1",
		Title: "agent-shell",
		Viewport: &Viewport{
			TerminalID:    "term-001",
			TerminalState: "running",
			AttachMode:    "observer",
			Readonly:      true,
		},
	}

	title := paneTitle(pane)
	if !containsAll(title, "agent-shell", "[obs]", "[ro]") {
		t.Fatalf("expected pane title badges, got %q", title)
	}
}

func TestPaneTitlePrefersTerminalNameOverPaneTitle(t *testing.T) {
	pane := &Pane{
		ID:    "pane-1",
		Title: "pane-observer-slot",
		Viewport: &Viewport{
			TerminalID:    "term-001",
			Name:          "real-terminal-name",
			TerminalState: "running",
		},
	}

	if got := paneTitle(pane); !strings.Contains(got, "real-terminal-name") || strings.Contains(got, "pane-observer-slot") {
		t.Fatalf("expected pane title to prefer terminal name, got %q", got)
	}
}

func TestPaneTitleUsesSavedPaneLabelWhenUnbound(t *testing.T) {
	pane := &Pane{
		ID:    "pane-1",
		Title: "stale-title",
		Viewport: &Viewport{
			TerminalState: "unbound",
		},
	}

	if got := paneTitle(pane); got != "saved pane" {
		t.Fatalf("expected unbound pane title to collapse to saved pane, got %q", got)
	}
}

func TestRenderTabBarUsesWorkspaceBadgeAndBracketedActiveTab(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh", Workspace: "project-x", IconSet: "ascii"})
	model.width = 120
	model.workspace.Tabs = []*Tab{newTab("dev"), newTab("logs"), newTab("build")}
	model.workspace.ActiveTab = 1

	bar := xansi.Strip(model.renderTabBar())
	if !containsAll(bar, "[project-x]", "1:dev", "[2:logs]", "3:build", "pane:0") {
		t.Fatalf("expected lightweight workspace/tab bar, got:\n%s", bar)
	}
}

func TestEmptyStateViewUsesLauncherCard(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 24
	model.workspace.Tabs = []*Tab{newTab("1")}
	model.workspace.ActiveTab = 0

	view := xansi.Strip(model.View())
	if !containsAll(view, "termx workspace", "Start New Terminal", "Attach Existing Terminal", "Open Workspace Picker") {
		t.Fatalf("expected launcher-style empty state card, got:\n%s", view)
	}
}

func TestHelpScreenRendersCenteredModal(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 24

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)
	model.showHelp = true

	view := model.View()
	if idx := lineIndexContaining(view, "┌ Help / Shortcut Map"); idx < 4 {
		t.Fatalf("expected centered help modal, got:\n%s", xansi.Strip(view))
	}
	line := lineContaining(view, "┌ Help / Shortcut Map")
	if !strings.Contains(line, "┐") {
		t.Fatalf("expected help modal top border to close cleanly, got:\n%s", xansi.Strip(view))
	}
	if strings.Contains(line, "workspace = one whole session") {
		t.Fatalf("expected help title row to stay isolated inside modal chrome, got:\n%s", xansi.Strip(view))
	}
}

func TestEditTerminalPromptRendersCenteredModal(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 24

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	model.beginTerminalEditPrompt(protocol.TerminalInfo{
		ID:      pane.TerminalID,
		Name:    "api-shell",
		Command: []string{"/bin/zsh"},
		Tags:    map[string]string{"role": "api"},
	})

	view := xansi.Strip(model.View())
	if !containsAll(view, "Edit Terminal", "step 1/2", "name:", "api-shell", "terminal id:", pane.TerminalID, "command: /bin/zsh", "updates terminal metadata", "Enter next", "Esc cancel") {
		t.Fatalf("expected centered edit-terminal modal, got:\n%s", view)
	}
}

func TestNewTerminalPromptShowsTargetAndCommandContext(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/zsh"})
	model.width = 100
	model.height = 24

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	model.beginTerminalCreatePrompt(terminalPickerAction{Kind: terminalPickerActionFloating, TabIndex: 0}, []string{"/bin/zsh", "-l"})

	view := xansi.Strip(model.View())
	if !containsAll(view, "New Terminal", "step 1/2", "opens in: floating pane", "command: /bin/zsh -l", "name:", "Enter next", "Esc cancel") {
		t.Fatalf("expected create-terminal prompt to describe target and command, got:\n%s", view)
	}
}

func TestApplyTerminalMetadataUpdateShowsNoticeWithoutAttachedPanes(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})

	model.applyTerminalMetadataUpdate("term-parked", "parked-shell", map[string]string{"role": "ops"})

	if model.notice != "updated terminal metadata" {
		t.Fatalf("expected metadata update notice even for parked terminal, got %q", model.notice)
	}
}

func TestDirectViewModePansWithoutSubmode(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 24
	model.height = 12

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	pane.VTerm.Resize(80, 12)
	_, _ = pane.VTerm.Write([]byte(strings.Repeat("0123456789", 8)))
	pane.live = true
	pane.MarkRenderDirty()
	pane.Mode = ViewportModeFixed
	pane.Pin = true
	pane.Offset = Point{}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if pane.Offset.X != 4 {
		t.Fatalf("expected direct view mode to pan immediately, got offset %+v", pane.Offset)
	}
	if !model.prefixActive || model.prefixMode != prefixModeViewport || !model.directMode {
		t.Fatalf("expected direct view mode to remain active, active=%v direct=%v mode=%v", model.prefixActive, model.directMode, model.prefixMode)
	}
}

func TestDirectModeIgnoresUnknownKeyUntilEsc(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'!'}})
	if !model.prefixActive || !model.directMode || model.prefixMode != prefixModeTab {
		t.Fatalf("expected unknown key to leave direct tab mode active, active=%v direct=%v mode=%v", model.prefixActive, model.directMode, model.prefixMode)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.prefixActive {
		t.Fatal("expected esc to leave direct mode after ignored key")
	}
}

func TestPrefixFloatingStickyModeMovesPaneAndEscapes(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	createFloatingPaneViaPicker(t, model)
	tab := model.currentTab()
	if tab == nil || len(tab.Floating) == 0 {
		t.Fatalf("expected floating pane, got %#v", tab)
	}
	before := tab.Floating[0].Rect

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if !model.prefixActive || model.prefixMode != prefixModeFloating {
		t.Fatalf("expected floating sticky prefix mode, active=%v mode=%v", model.prefixActive, model.prefixMode)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	after := tab.Floating[0].Rect
	if after.X != before.X-4 {
		t.Fatalf("expected floating sticky move left by 4, before=%+v after=%+v", before, after)
	}
	if !model.prefixActive || model.prefixMode != prefixModeFloating {
		t.Fatalf("expected floating sticky mode to remain active, active=%v mode=%v", model.prefixActive, model.prefixMode)
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}
	if model.prefixActive {
		t.Fatal("expected Esc to exit floating sticky mode")
	}
	if isFloatingPane(tab, tab.ActivePaneID) {
		t.Fatalf("expected Esc to return focus to tiled pane, got %q", tab.ActivePaneID)
	}
}

func TestPrefixFloatingStickyModeInvalidKeyClearsMode(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	createFloatingPaneViaPicker(t, model)

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if !model.prefixActive || model.prefixMode != prefixModeFloating {
		t.Fatalf("expected floating sticky mode, active=%v mode=%v", model.prefixActive, model.prefixMode)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if model.prefixActive {
		t.Fatal("expected unsupported floating sticky key to clear prefix mode")
	}
}

func TestPrefixArrowNavigationMovesFocus(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	original := model.currentTab().ActivePaneID

	createSplitPaneViaPicker(t, model, SplitVertical)

	if model.currentTab().ActivePaneID == original {
		t.Fatal("expected split to focus the new pane")
	}

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if model.currentTab().ActivePaneID != original {
		t.Fatalf("expected left arrow to move focus back to %q, got %q", original, model.currentTab().ActivePaneID)
	}
	if !model.prefixActive {
		t.Fatal("expected directional prefix action to keep prefix mode active for repeated navigation")
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	if model.currentTab().ActivePaneID == original {
		t.Fatalf("expected repeated arrow without re-prefix to move focus forward, got %q", model.currentTab().ActivePaneID)
	}
}

func TestPrefixResizeStaysActiveForRepeatedResize(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	createSplitPaneViaPicker(t, model, SplitVertical)
	tab := model.currentTab()
	if tab == nil || tab.Root == nil {
		t.Fatal("expected split layout")
	}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}})
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}
	if !model.prefixActive {
		t.Fatal("expected resize prefix action to stay active")
	}
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}})
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}
	if !model.prefixActive {
		t.Fatal("expected repeated resize to keep prefix active")
	}
}

func TestPrefixSwapMovesActivePanePosition(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	createSplitPaneViaPicker(t, model, SplitVertical)
	createSplitPaneViaPicker(t, model, SplitHorizontal)

	tab := model.currentTab()
	activeID := tab.ActivePaneID
	before := tab.Root.Rects(Rect{X: 0, Y: 0, W: model.width, H: model.height - 2})[activeID]

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'{'}})
	_, resizeCmd := model.Update(mustRunCmd(t, cmd))
	if resizeCmd != nil {
		_ = mustRunCmd(t, resizeCmd)
	}

	after := tab.Root.Rects(Rect{X: 0, Y: 0, W: model.width, H: model.height - 2})[activeID]
	if after == before {
		t.Fatalf("expected active pane %q to move after swap, rect stayed %#v", activeID, after)
	}
	if tab.ActivePaneID != activeID {
		t.Fatalf("expected swap to keep focus on %q, got %q", activeID, tab.ActivePaneID)
	}
}

func TestClosingCurrentTabKillsItsPanesAndReturnsToPreviousTab(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	firstTab := model.currentTab()
	firstTabPaneCount := len(firstTab.Panes)

	createNewTabViaPicker(t, model)

	createSplitPaneViaPicker(t, model, SplitVertical)

	secondTab := model.currentTab()
	killedIDs := make(map[string]bool, len(secondTab.Panes))
	for _, pane := range secondTab.Panes {
		killedIDs[pane.TerminalID] = true
	}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'&'}})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)

	if model.quitting {
		t.Fatal("expected closing one of multiple tabs to keep TUI running")
	}
	if cmd != nil {
		t.Fatal("expected no quit command when another tab remains")
	}
	if len(model.workspace.Tabs) != 1 {
		t.Fatalf("expected 1 tab after close, got %d", len(model.workspace.Tabs))
	}
	if model.workspace.ActiveTab != 0 {
		t.Fatalf("expected focus to return to first tab, got %d", model.workspace.ActiveTab)
	}
	if got := len(model.currentTab().Panes); got != firstTabPaneCount {
		t.Fatalf("expected original first tab pane count %d, got %d", firstTabPaneCount, got)
	}
	if len(client.killedIDs) != len(killedIDs) {
		t.Fatalf("expected %d killed terminals, got %d", len(killedIDs), len(client.killedIDs))
	}
	for _, terminalID := range client.killedIDs {
		if !killedIDs[terminalID] {
			t.Fatalf("unexpected killed terminal %q", terminalID)
		}
	}
}

func TestClosingLastTabQuitsTUI(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'&'}})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)

	if !model.quitting {
		t.Fatal("expected closing the last tab to quit the TUI")
	}
	if _, ok := mustRunCmd(t, cmd).(tea.QuitMsg); !ok {
		t.Fatal("expected quit command after closing the last tab")
	}
	if len(client.killedIDs) != 1 {
		t.Fatalf("expected 1 killed terminal, got %d", len(client.killedIDs))
	}
}

func TestTerminalPickerEnterReplacesCurrentViewport(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "worker", Command: []string{"tail", "-f", "worker.log"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	original := model.currentTab().Panes[model.currentTab().ActivePaneID].TerminalID

	_ = activatePrefixForTest(model)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shared")})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane.TerminalID != "shared-001" {
		t.Fatalf("expected active pane to attach shared terminal, got %q", pane.TerminalID)
	}
	if len(tab.Panes) != 1 {
		t.Fatalf("expected picker attach to replace current viewport, got %d panes", len(tab.Panes))
	}
	if pane.TerminalID == original {
		t.Fatalf("expected terminal to change from %q", original)
	}
	if model.terminalPicker != nil {
		t.Fatal("expected picker to close after attach")
	}
}

func TestTerminalPickerTabSplitsAndAttachesExistingTerminal(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "worker", Command: []string{"tail", "-f", "worker.log"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shared")})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		_ = mustRunCmd(t, cmd)
	}

	tab := model.currentTab()
	if len(tab.Panes) != 2 {
		t.Fatalf("expected picker tab action to split current viewport, got %d panes", len(tab.Panes))
	}
	active := tab.Panes[tab.ActivePaneID]
	if active == nil || active.TerminalID != "shared-001" {
		t.Fatalf("expected new active pane to attach shared terminal, got %#v", active)
	}
}

func TestFloatingCommandOpensChooserAndCanAttachExistingTerminal(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "worker", Command: []string{"tail", "-f", "worker.log"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if model.terminalPicker == nil {
		t.Fatal("expected floating command to open chooser")
	}
	if model.terminalPicker.Title != "Open Floating Pane" {
		t.Fatalf("expected floating chooser title, got %q", model.terminalPicker.Title)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shared")})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	tab := model.currentTab()
	if len(tab.Floating) != 1 {
		t.Fatalf("expected chooser attach to create floating pane, got %d floating panes", len(tab.Floating))
	}
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil || pane.TerminalID != "shared-001" {
		t.Fatalf("expected floating chooser to attach shared terminal, got %#v", pane)
	}
	if pane.Mode != ViewportModeFixed {
		t.Fatalf("expected floating chooser to keep fixed mode, got %q", pane.Mode)
	}
}

func TestFloatingChooserKeepsExitedTerminalVisible(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "worker", Command: []string{"tail", "-f", "worker.log"}, State: "exited"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shared")})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	tab := model.currentTab()
	if tab == nil || len(tab.Floating) != 1 {
		t.Fatalf("expected exited terminal attach to keep floating pane visible, got %#v", tab)
	}
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected floating pane")
	}

	_, _ = model.Update(paneOutputMsg{
		paneID: pane.ID,
		frame:  protocol.StreamFrame{Type: protocol.TypeClosed, Payload: protocol.EncodeClosedPayload(0)},
	})

	if len(tab.Floating) != 1 {
		t.Fatalf("expected closed terminal to keep floating viewport, got %d floating panes", len(tab.Floating))
	}
	if pane.TerminalState != "exited" {
		t.Fatalf("expected floating pane to show exited state, got %q", pane.TerminalState)
	}
	if model.quitting {
		t.Fatal("expected exited floating terminal to keep TUI alive")
	}
}

func TestStartupChooserKeepsExitedTerminalVisible(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "worker", Command: []string{"tail", "-f", "worker.log"}, State: "exited"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", StartupPicker: true})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shared")})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected startup attach to create pane")
	}
	_, _ = model.Update(paneOutputMsg{
		paneID: pane.ID,
		frame:  protocol.StreamFrame{Type: protocol.TypeClosed, Payload: protocol.EncodeClosedPayload(0)},
	})
	if model.currentTab().Panes[model.currentTab().ActivePaneID] == nil {
		t.Fatal("expected exited terminal pane to remain visible")
	}
	if model.quitting {
		t.Fatal("expected exited startup terminal to keep TUI alive")
	}
}

func TestExitedPaneRestartShortcutCreatesReplacementTerminalAndPreservesViewportFlags(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	pane.TerminalID = "term-old"
	pane.Name = "worker"
	pane.Command = []string{"bash", "--noprofile", "--norc"}
	pane.Tags = map[string]string{"role": "worker"}
	pane.Mode = ViewportModeFixed
	pane.Offset = Point{X: 4, Y: 2}
	pane.Pin = true
	pane.Readonly = true
	model.markTerminalExited("term-old", 7)

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if client.createCalls != 2 {
		t.Fatalf("expected restart to create one additional terminal, got %d create calls", client.createCalls)
	}
	restarted := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if restarted == nil {
		t.Fatal("expected restarted pane")
	}
	if restarted.TerminalID == "term-old" || restarted.TerminalID == "" {
		t.Fatalf("expected restarted pane to bind new terminal, got %q", restarted.TerminalID)
	}
	if restarted.TerminalState != "running" {
		t.Fatalf("expected restarted pane to be running, got %q", restarted.TerminalState)
	}
	if restarted.Mode != ViewportModeFixed || restarted.Offset != (Point{X: 4, Y: 2}) || !restarted.Pin || !restarted.Readonly {
		t.Fatalf("expected viewport flags to survive restart, got %#v", restarted.Viewport)
	}
	if got := client.terminalByID[restarted.TerminalID].Tags["role"]; got != "worker" {
		t.Fatalf("expected restart to copy tags, got %#v", client.terminalByID[restarted.TerminalID].Tags)
	}
	if got := client.terminalByID[restarted.TerminalID].Command; len(got) != 3 || got[0] != "bash" {
		t.Fatalf("expected restart to reuse command, got %#v", got)
	}
}

func TestExitedPaneRestartOnlyRebindsActiveViewport(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	createSplitPaneViaPicker(t, model, SplitVertical)
	tab := model.currentTab()
	ids := tab.Root.LeafIDs()
	if len(ids) != 2 {
		t.Fatalf("expected two panes, got %v", ids)
	}
	left := tab.Panes[ids[0]]
	right := tab.Panes[ids[1]]
	left.TerminalID = "term-shared"
	left.Command = []string{"bash", "--noprofile", "--norc"}
	left.Tags = map[string]string{"role": "shared"}
	right.TerminalID = "term-shared"
	right.Command = []string{"bash", "--noprofile", "--norc"}
	right.Tags = map[string]string{"role": "shared"}
	tab.ActivePaneID = right.ID
	model.markTerminalExited("term-shared", 9)

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if right.TerminalID == "term-shared" || right.TerminalState != "running" {
		t.Fatalf("expected active pane to restart onto new terminal, got %#v", right)
	}
	if left.TerminalID != "term-shared" || left.TerminalState != "exited" {
		t.Fatalf("expected sibling pane to stay bound to old exited terminal, got %#v", left)
	}
}

func TestExitedPaneRestartInputEventShortcutCreatesReplacementTerminal(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	pane.TerminalID = "term-old"
	pane.Command = []string{"bash", "--noprofile", "--norc"}
	model.markTerminalExited("term-old", 3)

	_, cmd = model.Update(inputEventMsg{event: uv.KeyPressEvent{Code: 'r', Text: "r"}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	restarted := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if restarted == nil || restarted.TerminalID == "" || restarted.TerminalID == "term-old" {
		t.Fatalf("expected input-event restart to bind a new terminal, got %#v", restarted)
	}
	if len(client.inputs) != 0 {
		t.Fatalf("expected restart shortcut not to forward raw input, got %#v", client.inputs)
	}
}

func TestExitedPaneRestartRawInputShortcutCreatesReplacementTerminal(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	pane.TerminalID = "term-old"
	pane.Command = []string{"bash", "--noprofile", "--norc"}
	model.markTerminalExited("term-old", 5)

	_, cmd = model.Update(rawInputMsg{data: []byte("r")})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	restarted := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if restarted == nil || restarted.TerminalID == "" || restarted.TerminalID == "term-old" {
		t.Fatalf("expected raw-input restart to bind a new terminal, got %#v", restarted)
	}
	if len(client.inputs) != 0 {
		t.Fatalf("expected raw restart shortcut not to forward input, got %#v", client.inputs)
	}
}

func TestFloatingRectDefaultsToPaneContentSize(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	pane := &Pane{
		ID: "pane-001",
		Viewport: &Viewport{
			Mode:  ViewportModeFixed,
			VTerm: localvterm.New(50, 12, 100, nil),
			live:  true,
		},
	}
	rect := model.defaultFloatingRectForPane(pane)
	if rect.W != 52 || rect.H != 14 {
		t.Fatalf("expected floating rect to wrap content size, got %+v", rect)
	}
}

func TestClosingViewportDetachesWithoutKillingTerminal(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	createSplitPaneViaPicker(t, model, SplitVertical)

	tab := model.currentTab()
	orphanID := tab.Panes[tab.ActivePaneID].TerminalID

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		t.Fatal("expected detach to stay in the TUI when another viewport remains")
	}

	if model.quitting {
		t.Fatal("expected detach to keep TUI running")
	}
	if client.kills != 0 {
		t.Fatalf("expected detach to avoid killing terminal, got %d kill requests", client.kills)
	}
	if len(model.currentTab().Panes) != 1 {
		t.Fatalf("expected one viewport to remain, got %d", len(model.currentTab().Panes))
	}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	view := xansi.Strip(model.View())
	orphanName := client.terminalByID[orphanID].Name
	if orphanName == "" {
		orphanName = "terminal"
	}
	if !strings.Contains(view, "○ "+orphanName) {
		t.Fatalf("expected detached terminal %q (%q) to appear as orphan in picker:\n%s", orphanID, orphanName, view)
	}
}

func TestClosingLastViewportQuitsTUIWithoutKillingTerminal(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)

	if !model.quitting {
		t.Fatal("expected closing the last viewport to quit the TUI")
	}
	if _, ok := mustRunCmd(t, cmd).(tea.QuitMsg); !ok {
		t.Fatal("expected quit command after closing the last viewport")
	}
	if client.kills != 0 {
		t.Fatalf("expected closing the last viewport to keep terminal alive, got %d kill requests", client.kills)
	}
}

func TestKillingSharedTerminalKeepsBoundPanesAsSavedSlots(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "worker", Command: []string{"tail", "-f", "worker.log"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shared")})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shared")})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		_ = mustRunCmd(t, cmd)
	}

	if len(model.currentTab().Panes) != 2 {
		t.Fatalf("expected duplicated terminal to be visible in two viewports, got %d panes", len(model.currentTab().Panes))
	}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	if cmd != nil {
		t.Fatalf("expected shared terminal stop to open confirm prompt first, got %#v", cmd)
	}
	if model.prompt == nil || model.prompt.Kind != "confirm-stop-terminal" {
		t.Fatalf("expected confirm-stop-terminal prompt, got %#v", model.prompt)
	}
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)

	if client.kills != 1 || len(client.killedIDs) != 1 || client.killedIDs[0] != "shared-001" {
		t.Fatalf("expected shared terminal kill request, got kills=%d ids=%v", client.kills, client.killedIDs)
	}
	if model.quitting {
		t.Fatal("expected killing a shared terminal to keep pane slots instead of quitting")
	}
	if cmd != nil {
		t.Fatalf("expected no quit command after killing shared terminal, got %#v", cmd)
	}
	tab := model.currentTab()
	if tab == nil || len(tab.Panes) != 2 {
		t.Fatalf("expected both pane slots to remain after kill, got %#v", tab)
	}
	for _, pane := range tab.Panes {
		if pane == nil {
			t.Fatalf("expected concrete pane slot, got %#v", tab.Panes)
		}
		if pane.TerminalID != "" {
			t.Fatalf("expected killed terminal to unbind from pane, got %#v", pane)
		}
		if pane.TerminalState != "unbound" {
			t.Fatalf("expected pane to become unbound slot, got state %q", pane.TerminalState)
		}
	}
	if !strings.Contains(model.notice, "left 2 saved panes") {
		t.Fatalf("expected saved slot notice after kill, got %q", model.notice)
	}
}

func TestRemoteTerminalRemovedEventKeepsSavedSlotsAndShowsNotice(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	shared := tab.Panes[tab.ActivePaneID]
	if shared == nil {
		t.Fatal("expected initial pane")
	}
	sharedID := shared.TerminalID

	localPane := &Pane{
		ID:    "pane-local",
		Title: "local",
		Viewport: &Viewport{
			TerminalID: "local-001",
			Channel:    shared.Channel + 1,
			VTerm:      localvterm.New(80, 24, 100, nil),
			Snapshot: &protocol.Snapshot{
				TerminalID: "local-001",
				Size:       protocol.Size{Cols: 80, Rows: 24},
			},
			Mode: ViewportModeFit,
		},
	}
	tab.Panes[localPane.ID] = localPane
	_ = tab.Root.Split(shared.ID, SplitVertical, localPane.ID)
	tab.ActivePaneID = localPane.ID

	_, _ = model.Update(terminalEventMsg{event: protocol.Event{
		Type:       protocol.EventTerminalRemoved,
		TerminalID: shared.TerminalID,
		Removed:    &protocol.TerminalRemovedData{Reason: "killed"},
	}})

	if len(tab.Panes) != 2 {
		t.Fatalf("expected remote removal to keep pane slot, got %d panes", len(tab.Panes))
	}
	if slot := tab.Panes[shared.ID]; slot == nil || slot.TerminalState != "unbound" || slot.TerminalID != "" {
		t.Fatalf("expected shared pane to become an unbound slot after remote terminal removal, got %#v", slot)
	}
	if model.notice != fmt.Sprintf("terminal %q was removed by another client; left 1 saved pane", sharedID) {
		t.Fatalf("unexpected remote removal notice: %q", model.notice)
	}
	if model.quitting {
		t.Fatal("expected surviving local pane to keep TUI running")
	}
}

func TestCommandTerminalsOpensTerminalManager(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "term-001", Name: "api", Command: []string{"bash"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	cmd := model.executeCommandPrompt("terminals")
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if model.terminalManager == nil {
		t.Fatal("expected terminal manager to open from command prompt")
	}
	view := xansi.Strip(model.View())
	if !containsAll(view, "Running Terminals", "api", "TERMINALS", "<Enter> BRING HERE") {
		t.Fatalf("expected terminal manager chrome, got:\n%s", view)
	}
}

func TestTerminalManagerEnterAttachesSelectionToActivePane(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "term-001", Name: "api", Command: []string{"bash"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	cmd := model.loadTerminalManagerCmd("", "term-001")
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)
	if model.terminalManager == nil {
		t.Fatal("expected terminal manager to open")
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil || pane.TerminalID != "term-001" {
		t.Fatalf("expected terminal manager enter to attach selected terminal, got %#v", pane)
	}
	if model.terminalManager != nil {
		t.Fatal("expected terminal manager to close after attach")
	}
}

func TestTerminalManagerDefaultsToActiveTerminalWhenPaneAlreadyBound(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	active := activePane(model.currentTab())
	if active == nil || active.TerminalID == "" {
		t.Fatal("expected init to bind an active pane terminal")
	}
	client.listResult = []protocol.TerminalInfo{
		{ID: active.TerminalID, Name: "current", Command: []string{"bash"}, State: "running"},
		{ID: "term-002", Name: "other", Command: []string{"bash"}, State: "running"},
	}

	cmd := model.openTerminalManagerCmd()
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)
	if model.terminalManager == nil {
		t.Fatal("expected terminal manager to open")
	}

	item := model.selectedTerminalManagerItem()
	if item == nil {
		t.Fatal("expected terminal manager selection")
	}
	if item.CreateNew {
		t.Fatal("expected terminal manager to default to a real terminal")
	}
	if item.Info.ID != active.TerminalID {
		t.Fatalf("expected terminal manager to default to current active terminal %q, got %q", active.TerminalID, item.Info.ID)
	}
}

func TestPaneRenderEntriesZoomedTiledPaneHidesFloatingLayer(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})

	tab := newTab("main")
	tab.Root = NewLeaf("pane-a")
	tab.Root.Split("pane-a", SplitVertical, "pane-b")
	tab.Panes = map[string]*Pane{
		"pane-a": {ID: "pane-a", Viewport: &Viewport{}},
		"pane-b": {ID: "pane-b", Viewport: &Viewport{}},
		"pane-f": {ID: "pane-f", Viewport: &Viewport{Mode: ViewportModeFixed}},
	}
	tab.FloatingVisible = true
	tab.Floating = []*FloatingPane{{PaneID: "pane-f", Rect: Rect{X: 10, Y: 4, W: 30, H: 10}, Z: 1}}
	tab.ZoomedPaneID = "pane-a"

	entries := model.paneRenderEntries(tab, 120, 38)
	if len(entries) != 1 {
		t.Fatalf("expected only zoomed tiled pane to render, got %#v", entries)
	}
	if entries[0].PaneID != "pane-a" {
		t.Fatalf("expected zoomed tiled pane to render, got %q", entries[0].PaneID)
	}
	if entries[0].Rect != (Rect{X: 0, Y: 0, W: 120, H: 38}) {
		t.Fatalf("expected zoomed tiled pane to fill root rect, got %+v", entries[0].Rect)
	}
	if entries[0].Floating {
		t.Fatal("expected zoomed tiled pane to render as full-screen content, not floating overlay")
	}
}

func TestPaneRenderEntriesZoomedFloatingPaneFillsRootRect(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})

	tab := newTab("main")
	tab.Root = NewLeaf("pane-a")
	tab.Panes = map[string]*Pane{
		"pane-a": {ID: "pane-a", Viewport: &Viewport{}},
		"pane-f": {ID: "pane-f", Viewport: &Viewport{Mode: ViewportModeFixed}},
	}
	tab.FloatingVisible = true
	tab.Floating = []*FloatingPane{{PaneID: "pane-f", Rect: Rect{X: 8, Y: 3, W: 40, H: 12}, Z: 1}}
	tab.ZoomedPaneID = "pane-f"

	entries := model.paneRenderEntries(tab, 100, 26)
	if len(entries) != 1 {
		t.Fatalf("expected only zoomed floating pane to render, got %#v", entries)
	}
	if entries[0].PaneID != "pane-f" {
		t.Fatalf("expected zoomed floating pane to render, got %q", entries[0].PaneID)
	}
	if entries[0].Rect != (Rect{X: 0, Y: 0, W: 100, H: 26}) {
		t.Fatalf("expected zoomed floating pane to fill root rect, got %+v", entries[0].Rect)
	}
}

func TestVisiblePaneRectsZoomedFloatingPaneReturnsOnlyFullScreenPane(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	tab := newTab("main")
	tab.Root = NewLeaf("pane-a")
	tab.Panes = map[string]*Pane{
		"pane-a": {ID: "pane-a", Viewport: &Viewport{}},
		"pane-f": {ID: "pane-f", Viewport: &Viewport{Mode: ViewportModeFixed}},
	}
	tab.FloatingVisible = true
	tab.Floating = []*FloatingPane{{PaneID: "pane-f", Rect: Rect{X: 8, Y: 3, W: 40, H: 12}, Z: 1}}
	tab.ZoomedPaneID = "pane-f"

	rects := model.visiblePaneRects(tab)
	if len(rects) != 1 {
		t.Fatalf("expected only zoomed floating pane rect, got %#v", rects)
	}
	if rects["pane-f"] != (Rect{X: 0, Y: 0, W: 120, H: 28}) {
		t.Fatalf("expected zoomed floating pane rect to fill content area, got %+v", rects["pane-f"])
	}
}

func TestClampFloatingRectKeepsWindowSmallerThanViewport(t *testing.T) {
	bounds := Rect{X: 0, Y: 0, W: 80, H: 24}

	rect := clampFloatingRect(Rect{X: 0, Y: 0, W: 80, H: 24}, bounds)
	if rect.W >= bounds.W {
		t.Fatalf("expected floating width to stay smaller than viewport width, got %+v within %+v", rect, bounds)
	}
	if rect.H >= bounds.H {
		t.Fatalf("expected floating height to stay smaller than viewport height, got %+v within %+v", rect, bounds)
	}

	loose := clampFloatingRectLoose(Rect{X: 0, Y: 0, W: 80, H: 24}, bounds)
	if loose.W >= bounds.W {
		t.Fatalf("expected loose floating width to stay smaller than viewport width, got %+v within %+v", loose, bounds)
	}
	if loose.H >= bounds.H {
		t.Fatalf("expected loose floating height to stay smaller than viewport height, got %+v within %+v", loose, bounds)
	}
}

func TestDefaultFloatingRectForPaneStaysSmallerThanViewport(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.width = 80
	model.height = 24

	pane := &Pane{
		ID: "pane-001",
		Viewport: &Viewport{
			Mode:  ViewportModeFixed,
			VTerm: localvterm.New(200, 80, 100, nil),
			live:  true,
		},
	}

	rect := model.defaultFloatingRectForPane(pane)
	bounds := Rect{X: 0, Y: 0, W: model.width, H: model.height - 2}
	if rect.W >= bounds.W {
		t.Fatalf("expected default floating width to stay smaller than viewport width, got %+v within %+v", rect, bounds)
	}
	if rect.H >= bounds.H {
		t.Fatalf("expected default floating height to stay smaller than viewport height, got %+v within %+v", rect, bounds)
	}
}

func TestTerminalManagerDetailShowsVisibilityAndLocations(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "term-001", Name: "api", Command: []string{"bash"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	base := activePane(tab)
	base.TerminalID = "term-001"
	base.Name = "api"
	base.Title = "api"
	base.Command = []string{"bash"}
	base.TerminalState = "running"

	floatPane := &Pane{
		ID:    "pane-float",
		Title: "api-float",
		Viewport: &Viewport{
			TerminalID:    "term-001",
			Channel:       base.Channel + 1,
			VTerm:         localvterm.New(80, 24, 100, nil),
			Snapshot:      &protocol.Snapshot{TerminalID: "term-001", Size: protocol.Size{Cols: 80, Rows: 24}},
			TerminalState: "running",
			Mode:          ViewportModeFit,
		},
	}
	tab.Panes[floatPane.ID] = floatPane
	tab.Floating = append(tab.Floating, &FloatingPane{PaneID: floatPane.ID, Rect: Rect{X: 4, Y: 3, W: 40, H: 10}, Z: 0})

	cmd := model.loadTerminalManagerCmd("", "term-001")
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)
	model.terminalManager.selectID("term-001")

	lines := model.renderTerminalManagerDetail(60)
	detail := strings.Join(lines, "\n")
	if !containsAll(detail, "visibility: visible", "shown in:", "ws:main / tab:tab 1 / pane:api", "ws:main / tab:tab 1 / float:api-float") {
		t.Fatalf("expected terminal manager detail to show visibility and locations, got:\n%s", detail)
	}
}

func TestTerminalManagerViewGroupsVisibleParkedAndExitedTerminals(t *testing.T) {
	exitCode := 0
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "term-visible", Name: "api", Command: []string{"bash"}, State: "running"},
			{ID: "term-parked", Name: "logs", Command: []string{"tail", "-f", "app.log"}, State: "running"},
			{ID: "term-exited", Name: "old-build", Command: []string{"make", "test"}, State: "exited", ExitCode: &exitCode},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 140
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	base := activePane(tab)
	base.TerminalID = "term-visible"
	base.Name = "api"
	base.Title = "api"
	base.Command = []string{"bash"}
	base.TerminalState = "running"

	cmd := model.loadTerminalManagerCmd("", "term-visible")
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	view := xansi.Strip(model.View())
	if !containsAll(view, "VISIBLE", "PARKED", "EXITED", "api", "logs", "old-build") {
		t.Fatalf("expected terminal manager to group visible/parked/exited terminals, got:\n%s", view)
	}
}

func TestTerminalManagerStatusBarUsesManagerActionsAndSummary(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "term-visible", Name: "api", Command: []string{"bash"}, State: "running"},
			{ID: "term-parked", Name: "logs", Command: []string{"tail", "-f", "app.log"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", IconSet: "ascii"})
	model.width = 160
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	base := activePane(tab)
	base.TerminalID = "term-visible"
	base.Name = "api"
	base.Title = "api"
	base.Command = []string{"bash"}
	base.TerminalState = "running"

	cmd := model.loadTerminalManagerCmd("", "term-visible")
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	status := xansi.Strip(model.renderStatus())
	if !containsAll(status, "TERMINALS", "<Enter> BRING HERE", "<t> NEW TAB", "<k> STOP TERMINAL", "api", "visible", "shown:1") {
		t.Fatalf("expected terminal manager status bar to expose manager actions and summary, got:\n%s", status)
	}
	if strings.Contains(status, "Ctrl +") {
		t.Fatalf("expected terminal manager status to replace normal shortcut bar, got:\n%s", status)
	}
}

func TestTerminalManagerCtrlEOpensEditPromptForSelectedTerminal(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "term-parked", Name: "parked-shell", Command: []string{"bash", "--noprofile"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 140
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	cmd := model.loadTerminalManagerCmd("", "term-parked")
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	if cmd != nil {
		t.Fatalf("expected terminal manager edit to be synchronous, got %#v", cmd)
	}
	if model.terminalManager != nil {
		t.Fatal("expected terminal manager to close when editing terminal metadata")
	}
	if model.prompt == nil || model.prompt.Kind != "edit-terminal-name" {
		t.Fatalf("expected edit-terminal-name prompt, got %#v", model.prompt)
	}
	view := xansi.Strip(model.View())
	if !containsAll(view, "Edit Terminal", "step 1/2", "terminal id: term-parked", "command: bash --noprofile") {
		t.Fatalf("expected metadata prompt context after terminal manager edit, got:\n%s", view)
	}
}

func TestWelcomePaneLinesDescribeSavedPaneActions(t *testing.T) {
	pane := &Pane{
		ID:    "pane-1",
		Title: "saved pane",
		Viewport: &Viewport{
			TerminalState: "unbound",
		},
	}
	lines := welcomePaneLines(pane)
	body := strings.Join(lines, "\n")
	if !containsAll(body, "No terminal in this pane", "Enter start new terminal", "Ctrl-f bring running terminal here", "Ctrl-g then t open terminal manager") {
		t.Fatalf("expected saved-pane guidance, got:\n%s", body)
	}
}

func TestPaneFrameMetaUsesRelationshipBadges(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 24

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	base := activePane(tab)
	base.Mode = ViewportModeFixed
	base.Readonly = true
	base.Tags = map[string]string{"termx.size_lock": "warn"}
	shared := &Pane{
		ID:    "pane-2",
		Title: "shared",
		Viewport: &Viewport{
			TerminalID:    base.TerminalID,
			Channel:       base.Channel + 1,
			VTerm:         localvterm.New(80, 24, 100, nil),
			Snapshot:      &protocol.Snapshot{TerminalID: base.TerminalID, Size: protocol.Size{Cols: 80, Rows: 24}},
			TerminalState: "running",
			Mode:          ViewportModeFit,
		},
	}
	tab.Panes[shared.ID] = shared
	meta := xansi.Strip(model.paneFrameMeta(tab, base.ID, base, false))
	if !containsAll(meta, "live", "owner", "2", "ro", "lock") {
		t.Fatalf("expected layered relationship badges, got %q", meta)
	}
}

func TestTerminalPickerShowsStateRuntimeAndTags(t *testing.T) {
	exitCode := 1
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{
				ID:        "run-001",
				Name:      "agent",
				Command:   []string{"claude", "--print"},
				State:     "running",
				CreatedAt: time.Now().Add(-5*time.Minute - 21*time.Second),
				Tags: map[string]string{
					"role": "ai-agent",
					"team": "infra",
				},
			},
			{
				ID:        "exit-001",
				Name:      "logs",
				Command:   []string{"tail", "-f", "app.log"},
				State:     "exited",
				CreatedAt: time.Now().Add(-2*time.Hour - 7*time.Minute),
				ExitCode:  &exitCode,
				Tags: map[string]string{
					"role": "log",
				},
			},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 180
	model.height = 24

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	view := xansi.Strip(model.View())
	if !containsAll(view, "running", "5m", "role=ai-agent", "team=infra", "exited", "2h", "code=1") {
		t.Fatalf("expected picker to render state/runtime/tags, got:\n%s", view)
	}
}

func TestTerminalPickerSearchMatchesTagsAndWorkspaceLocation(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{
				ID:      "shared-001",
				Name:    "agent",
				Command: []string{"claude"},
				State:   "running",
				Tags: map[string]string{
					"role": "ai-agent",
				},
			},
			{
				ID:      "orphan-001",
				Name:    "logs",
				Command: []string{"tail", "-f", "app.log"},
				State:   "running",
			},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", Workspace: "ops"})
	model.width = 160
	model.height = 24

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("role=ai-agent")})
	if got := len(model.terminalPicker.Filtered); got != 1 || model.terminalPicker.Filtered[0].Info.ID != "shared-001" {
		t.Fatalf("expected tag search to match shared terminal, got %#v", model.terminalPicker.Filtered)
	}

	model.terminalPicker.Query = ""
	model.terminalPicker.applyFilter()
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ws:ops / tab:1")})
	if got := len(model.terminalPicker.Filtered); got == 0 || model.terminalPicker.Filtered[0].Info.ID != "pane-001" {
		t.Fatalf("expected location search to match current workspace viewport, got %#v", model.terminalPicker.Filtered)
	}
}

func TestLoadLayoutSpecCmdReplacesWorkspaceAndAttachesMatchedTerminal(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{
				ID:      "shared-001",
				Name:    "editor",
				Command: []string{"vim", "."},
				State:   "running",
				Tags: map[string]string{
					"role":    "editor",
					"project": "api",
				},
			},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	layout, err := ParseLayoutYAML([]byte(`
name: dev
tabs:
  - name: coding
    tiling:
      terminal:
        tag: "role=editor,project=api"
        command: "vim ."
        mode: fixed
`))
	if err != nil {
		t.Fatalf("ParseLayoutYAML returned error: %v", err)
	}

	msg := mustRunCmd(t, model.loadLayoutSpecCmd(layout, "loaded-dev", LayoutResolveCreate))
	_, _ = model.Update(msg)

	if model.workspace.Name != "loaded-dev" {
		t.Fatalf("expected workspace name loaded-dev, got %q", model.workspace.Name)
	}
	if len(model.workspace.Tabs) != 1 {
		t.Fatalf("expected 1 loaded tab, got %d", len(model.workspace.Tabs))
	}
	tab := model.currentTab()
	if tab == nil || tab.Name != "coding" {
		t.Fatalf("expected coding tab, got %#v", tab)
	}
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	if pane.TerminalID != "shared-001" {
		t.Fatalf("expected matched terminal id shared-001, got %q", pane.TerminalID)
	}
	if pane.Mode != ViewportModeFixed {
		t.Fatalf("expected fixed mode from layout, got %q", pane.Mode)
	}
	if len(client.attachedIDs) != 1 || client.attachedIDs[0] != "shared-001" {
		t.Fatalf("expected matched terminal attach call, got %v", client.attachedIDs)
	}
}

func TestLoadLayoutSpecCmdLeavesWaitingViewportForUnmatchedTerminal(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 24

	layout, err := ParseLayoutYAML([]byte(`
name: dev
tabs:
  - name: logs
    tiling:
      terminal:
        tag: "role=log"
        command: "tail -f app.log"
`))
	if err != nil {
		t.Fatalf("ParseLayoutYAML returned error: %v", err)
	}

	msg := mustRunCmd(t, model.loadLayoutSpecCmd(layout, "", LayoutResolveSkip))
	_, _ = model.Update(msg)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	if pane.TerminalState != "waiting" {
		t.Fatalf("expected waiting pane state, got %q", pane.TerminalState)
	}
	if got := xansi.Strip(model.View()); !strings.Contains(got, "waiting for terminal") {
		t.Fatalf("expected waiting placeholder in view, got:\n%s", got)
	}
}

func TestLoadLayoutSpecCmdCreatePolicyCreatesMissingTerminal(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 24

	layout, err := ParseLayoutYAML([]byte(`
name: dev
tabs:
  - name: shell
    tiling:
      terminal:
        tag: "role=shell"
        command: "zsh"
`))
	if err != nil {
		t.Fatalf("ParseLayoutYAML returned error: %v", err)
	}

	msg := mustRunCmd(t, model.loadLayoutSpecCmd(layout, "", LayoutResolveCreate))
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	if pane.TerminalState != "running" {
		t.Fatalf("expected created pane to be running, got %q", pane.TerminalState)
	}
	if pane.TerminalID == "" {
		t.Fatal("expected created pane to have terminal id")
	}
	if client.createCalls != 1 {
		t.Fatalf("expected one create call, got %d", client.createCalls)
	}
	if len(client.attachedIDs) != 1 || client.attachedIDs[0] != pane.TerminalID {
		t.Fatalf("expected created terminal attach, got %v", client.attachedIDs)
	}
}

func TestLoadLayoutSpecCmdPromptPolicyOpensPickerForUnmatchedPane(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "log", Command: []string{"tail", "-f", "app.log"}, State: "running", Tags: map[string]string{"role": "log"}},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 24

	layout, err := ParseLayoutYAML([]byte(`
name: dev
tabs:
  - name: logs
    tiling:
      terminal:
        tag: "role=missing"
        command: "tail -f app.log"
`))
	if err != nil {
		t.Fatalf("ParseLayoutYAML returned error: %v", err)
	}

	msg := mustRunCmd(t, model.loadLayoutSpecCmd(layout, "", LayoutResolvePrompt))
	_, cmd := model.Update(msg)
	if cmd == nil {
		t.Fatal("expected prompt policy to open chooser")
	}
	next := mustRunCmd(t, cmd)
	_, _ = model.Update(next)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil || pane.TerminalState != "waiting" {
		t.Fatalf("expected active waiting pane before prompt resolution, got %#v", pane)
	}
	if model.terminalPicker == nil {
		t.Fatal("expected prompt policy to open terminal picker")
	}
	if model.terminalPicker.Action.Kind != terminalPickerActionLayoutResolve {
		t.Fatalf("expected layout resolve action, got %#v", model.terminalPicker.Action)
	}
	if model.terminalPicker.Action.PaneID != pane.ID {
		t.Fatalf("expected prompt to target waiting pane %q, got %#v", pane.ID, model.terminalPicker.Action)
	}
}

func TestLoadLayoutSpecCmdPromptPolicyAttachResolvesWaitingPane(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "term-log", Name: "log", Command: []string{"tail", "-f", "app.log"}, State: "running", Tags: map[string]string{"role": "log"}},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 24

	layout, err := ParseLayoutYAML([]byte(`
name: dev
tabs:
  - name: logs
    tiling:
      terminal:
        tag: "role=missing"
        command: "tail -f app.log"
`))
	if err != nil {
		t.Fatalf("ParseLayoutYAML returned error: %v", err)
	}

	msg := mustRunCmd(t, model.loadLayoutSpecCmd(layout, "", LayoutResolvePrompt))
	_, cmd := model.Update(msg)
	next := mustRunCmd(t, cmd)
	_, _ = model.Update(next)

	if model.terminalPicker == nil || len(model.terminalPicker.Filtered) < 2 {
		t.Fatalf("expected picker items for create + attach, got %#v", model.terminalPicker)
	}
	model.terminalPicker.Selected = 1
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		if follow := mustRunCmd(t, cmd); follow != nil {
			_, _ = model.Update(follow)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil || pane.TerminalID != "term-log" || pane.TerminalState != "running" {
		t.Fatalf("expected prompt attach to resolve waiting pane, got %#v", pane)
	}
	if model.terminalPicker != nil {
		t.Fatal("expected prompt picker to close after resolution")
	}
}

func TestLoadLayoutSpecCmdPromptPolicyDoesNotAutoCreateWhenNoTerminalsExist(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 24

	layout, err := ParseLayoutYAML([]byte(`
name: dev
tabs:
  - name: logs
    tiling:
      terminal:
        tag: "role=missing"
        command: "tail -f app.log"
`))
	if err != nil {
		t.Fatalf("ParseLayoutYAML returned error: %v", err)
	}

	msg := mustRunCmd(t, model.loadLayoutSpecCmd(layout, "", LayoutResolvePrompt))
	_, cmd := model.Update(msg)
	next := mustRunCmd(t, cmd)
	_, _ = model.Update(next)

	if client.createCalls != 0 {
		t.Fatalf("expected prompt policy not to auto-create when no terminals exist, got %d create calls", client.createCalls)
	}
	if model.terminalPicker == nil {
		t.Fatal("expected prompt policy to open picker even when only create option exists")
	}
	if len(model.terminalPicker.Filtered) != 1 || !model.terminalPicker.Filtered[0].CreateNew {
		t.Fatalf("expected create-only picker, got %#v", model.terminalPicker.Filtered)
	}
}

func TestLoadLayoutSpecCmdPromptPolicyEscSkipsCurrentPaneAndContinuesToNext(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "term-log", Name: "log", Command: []string{"tail", "-f", "app.log"}, State: "running", Tags: map[string]string{"role": "log"}},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 24

	layout, err := ParseLayoutYAML([]byte(`
name: dev
tabs:
  - name: logs
    tiling:
      split: vertical
      children:
        - terminal:
            tag: "role=missing-a"
            command: "tail -f a.log"
        - terminal:
            tag: "role=missing-b"
            command: "tail -f b.log"
`))
	if err != nil {
		t.Fatalf("ParseLayoutYAML returned error: %v", err)
	}

	msg := mustRunCmd(t, model.loadLayoutSpecCmd(layout, "", LayoutResolvePrompt))
	_, cmd := model.Update(msg)
	next := mustRunCmd(t, cmd)
	_, _ = model.Update(next)

	firstPaneID := model.terminalPicker.Action.PaneID
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected esc to advance to next unresolved pane")
	}
	next = mustRunCmd(t, cmd)
	_, _ = model.Update(next)

	if model.terminalPicker == nil {
		t.Fatal("expected next prompt picker to open")
	}
	if model.terminalPicker.Action.PaneID == firstPaneID {
		t.Fatalf("expected esc to advance beyond first pane %q", firstPaneID)
	}
	firstPane := findPane(model.workspace.Tabs, firstPaneID)
	if firstPane == nil || firstPane.TerminalState != "waiting" {
		t.Fatalf("expected skipped pane to remain waiting, got %#v", firstPane)
	}
}

func TestLoadLayoutSpecCmdPromptPolicyAttachReusesExplicitHintAcrossPanes(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "term-shared", Name: "shared-log", Command: []string{"tail", "-f", "shared.log"}, State: "running", Tags: map[string]string{"role": "shared"}},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 24

	layout, err := ParseLayoutYAML([]byte(`
name: dev
tabs:
  - name: logs
    tiling:
      split: vertical
      children:
        - terminal:
            _hint_id: "shared-missing"
            tag: "role=missing"
            command: "tail -f shared.log"
        - terminal:
            _hint_id: "shared-missing"
            tag: "role=missing"
            command: "tail -f shared.log"
`))
	if err != nil {
		t.Fatalf("ParseLayoutYAML returned error: %v", err)
	}

	msg := mustRunCmd(t, model.loadLayoutSpecCmd(layout, "", LayoutResolvePrompt))
	_, cmd := model.Update(msg)
	next := mustRunCmd(t, cmd)
	_, _ = model.Update(next)

	if model.terminalPicker == nil {
		t.Fatal("expected prompt policy to open terminal picker")
	}
	if len(model.terminalPicker.Filtered) < 2 {
		t.Fatalf("expected picker items for create + attach, got %#v", model.terminalPicker)
	}
	model.terminalPicker.Selected = 1
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		if follow := mustRunCmd(t, cmd); follow != nil {
			_, _ = model.Update(follow)
		}
	}

	tab := model.currentTab()
	if tab == nil || len(tab.Panes) != 2 {
		t.Fatalf("expected prompt attach to keep both panes, got %#v", tab)
	}
	for _, pane := range tab.Panes {
		if pane == nil || pane.TerminalID != "term-shared" || pane.TerminalState != "running" {
			t.Fatalf("expected shared terminal binding for all panes, got %#v", pane)
		}
	}
	if model.terminalPicker != nil {
		t.Fatal("expected repeated hint attach to finish without a second prompt")
	}
	if len(model.layoutPromptQueue) != 0 || model.layoutPromptCurrent != nil {
		t.Fatalf("expected repeated hint prompt queue to be drained, current=%#v queue=%#v", model.layoutPromptCurrent, model.layoutPromptQueue)
	}
	if got := len(client.attachedIDs); got != 2 {
		t.Fatalf("expected one attach per pane for shared terminal, got %d attaches", got)
	}
}

func TestLoadLayoutSpecCmdCreatePolicyReusesExplicitHintAcrossPanes(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 24

	layout, err := ParseLayoutYAML([]byte(`
name: dev
tabs:
  - name: logs
    tiling:
      terminal:
        _hint_id: "shared-missing"
        tag: "role=missing"
        command: "tail -f shared.log"
    floating:
      - terminal:
          _hint_id: "shared-missing"
          tag: "role=missing"
          command: "tail -f shared.log"
        width: 40
        height: 12
        position: top-right
`))
	if err != nil {
		t.Fatalf("ParseLayoutYAML returned error: %v", err)
	}

	msg := mustRunCmd(t, model.loadLayoutSpecCmd(layout, "", LayoutResolveCreate))
	_, _ = model.Update(msg)

	tab := model.currentTab()
	if tab == nil || len(tab.Panes) != 2 || len(tab.Floating) != 1 {
		t.Fatalf("expected create policy to build tiled+floating panes, got %#v", tab)
	}
	terminalIDs := make(map[string]struct{})
	for _, pane := range tab.Panes {
		if pane == nil || strings.TrimSpace(pane.TerminalID) == "" || pane.TerminalState != "running" {
			t.Fatalf("expected created shared terminal for every pane, got %#v", pane)
		}
		terminalIDs[pane.TerminalID] = struct{}{}
	}
	if len(terminalIDs) != 1 {
		t.Fatalf("expected explicit hint create policy to reuse one terminal, got %#v", terminalIDs)
	}
	if client.createCalls != 1 {
		t.Fatalf("expected explicit hint create policy to create once, got %d", client.createCalls)
	}
	if got := len(client.attachedIDs); got != 2 {
		t.Fatalf("expected one attach per pane after single create, got %d", got)
	}
}

func TestLoadLayoutSpecCmdRestoresFloatingTerminalFromLayout(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{
				ID:      "term-shell",
				Name:    "shell",
				Command: []string{"zsh"},
				State:   "running",
				Tags:    map[string]string{"role": "shell"},
			},
			{
				ID:      "term-agent",
				Name:    "agent",
				Command: []string{"claude-code"},
				State:   "running",
				Tags:    map[string]string{"role": "agent"},
			},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	layout, err := ParseLayoutYAML([]byte(`
name: dev
tabs:
  - name: coding
    tiling:
      terminal:
        tag: "role=shell"
        command: "zsh"
    floating:
      - terminal:
          tag: "role=agent"
          command: "claude-code"
        width: 72
        height: 18
        position: top-right
`))
	if err != nil {
		t.Fatalf("ParseLayoutYAML returned error: %v", err)
	}

	msg := mustRunCmd(t, model.loadLayoutSpecCmd(layout, "", LayoutResolveCreate))
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	tab := model.currentTab()
	if tab == nil || len(tab.Floating) != 1 {
		t.Fatalf("expected one floating pane, got %#v", tab)
	}
	entry := tab.Floating[0]
	floatPane := tab.Panes[entry.PaneID]
	if floatPane == nil {
		t.Fatal("expected floating pane")
	}
	if floatPane.TerminalID != "term-agent" {
		t.Fatalf("expected floating pane to attach matched terminal, got %#v", floatPane)
	}
	if floatPane.Mode != ViewportModeFixed {
		t.Fatalf("expected floating pane to use fixed mode, got %q", floatPane.Mode)
	}
	if !tab.FloatingVisible {
		t.Fatal("expected floating layer to be visible after layout load")
	}
	if entry.Rect.W != 72 || entry.Rect.H != 18 {
		t.Fatalf("expected floating rect size from layout, got %+v", entry.Rect)
	}
	if entry.Rect.X+entry.Rect.W != model.width || entry.Rect.Y != 0 {
		t.Fatalf("expected top-right positioned floating rect, got %+v", entry.Rect)
	}
}

func TestLoadLayoutSpecCmdArrangeGridAttachesAllMatchedTerminals(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "term-a", Name: "api.log", Command: []string{"tail", "-f", "api.log"}, State: "running", Tags: map[string]string{"type": "log"}},
			{ID: "term-b", Name: "worker.log", Command: []string{"tail", "-f", "worker.log"}, State: "running", Tags: map[string]string{"type": "log"}},
			{ID: "term-c", Name: "redis.log", Command: []string{"tail", "-f", "redis.log"}, State: "running", Tags: map[string]string{"type": "log"}},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	layout, err := ParseLayoutYAML([]byte(`
name: monitoring
tabs:
  - name: logs
    tiling:
      arrange: grid
      match:
        tag: "type=log"
`))
	if err != nil {
		t.Fatalf("ParseLayoutYAML returned error: %v", err)
	}

	msg := mustRunCmd(t, model.loadLayoutSpecCmd(layout, "", LayoutResolveCreate))
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	tab := model.currentTab()
	if tab == nil || tab.Root == nil {
		t.Fatalf("expected arranged workspace root, got %#v", tab)
	}
	if got := len(tab.Root.LeafIDs()); got != 3 {
		t.Fatalf("expected 3 arranged panes, got %d", got)
	}
	if client.createCalls != 0 {
		t.Fatalf("expected arrange attach path to reuse existing terminals, got %d create calls", client.createCalls)
	}
	if len(client.attachedIDs) != 3 {
		t.Fatalf("expected all matched terminals to be attached, got %v", client.attachedIDs)
	}
}

func TestLoadLayoutSpecCmdCreatePolicyCreatesMissingFloatingTerminal(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	layout, err := ParseLayoutYAML([]byte(`
name: dev
tabs:
  - name: coding
    tiling:
      terminal:
        command: "zsh"
    floating:
      - terminal:
          tag: "role=agent"
          command: "claude-code"
        width: 66
        height: 16
`))
	if err != nil {
		t.Fatalf("ParseLayoutYAML returned error: %v", err)
	}

	msg := mustRunCmd(t, model.loadLayoutSpecCmd(layout, "", LayoutResolveCreate))
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	tab := model.currentTab()
	if tab == nil || len(tab.Floating) != 1 {
		t.Fatalf("expected one floating pane, got %#v", tab)
	}
	floatPane := tab.Panes[tab.Floating[0].PaneID]
	if floatPane == nil {
		t.Fatal("expected floating pane")
	}
	if floatPane.TerminalID == "" || floatPane.TerminalState != "running" {
		t.Fatalf("expected floating create path to attach running terminal, got %#v", floatPane)
	}
	if floatPane.Mode != ViewportModeFixed {
		t.Fatalf("expected floating create path to keep fixed mode, got %q", floatPane.Mode)
	}
	if client.createCalls != 2 {
		t.Fatalf("expected tiled + floating terminals to be created, got %d create calls", client.createCalls)
	}
	if len(client.attachedIDs) != 2 {
		t.Fatalf("expected both created terminals to be attached, got %v", client.attachedIDs)
	}
	if tab.Floating[0].Rect.W != 66 || tab.Floating[0].Rect.H != 16 {
		t.Fatalf("expected floating rect size from layout, got %+v", tab.Floating[0].Rect)
	}
}

func TestCommandSaveLayoutWritesYAMLFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", Workspace: "dev"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("save-layout demo")})
	if cmd != nil {
		_ = mustRunCmd(t, cmd)
	}
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	path := filepath.Join(home, ".config", "termx", "layouts", "demo.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected saved layout file: %v", err)
	}
	if !strings.Contains(string(data), "name: demo") {
		t.Fatalf("expected saved layout YAML, got:\n%s", string(data))
	}
}

func TestCommandSaveLayoutWritesFloatingPositionAnchor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh", Workspace: "dev"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	createFloatingPaneViaPicker(t, model)
	tab := model.currentTab()
	if tab == nil || len(tab.Floating) != 1 {
		t.Fatalf("expected one floating pane, got %#v", tab)
	}
	floatPane := tab.Panes[tab.Floating[0].PaneID]
	if floatPane == nil {
		t.Fatal("expected floating pane")
	}
	floatPane.Tags = map[string]string{"role": "agent"}
	floatPane.Command = []string{"claude-code"}
	tab.Floating[0].Rect = Rect{X: 74, Y: 0, W: 46, H: 14}

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("save-layout demo")})
	if cmd != nil {
		_ = mustRunCmd(t, cmd)
	}
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	path := filepath.Join(home, ".config", "termx", "layouts", "demo.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected saved layout file: %v", err)
	}
	if !strings.Contains(string(data), "position: top-right") {
		t.Fatalf("expected saved floating anchor, got:\n%s", string(data))
	}
}

func TestDetectFloatingPositionAnchor(t *testing.T) {
	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	cases := []struct {
		name string
		rect Rect
		want string
	}{
		{name: "center", rect: Rect{X: 35, Y: 7, W: 50, H: 14}, want: "center"},
		{name: "top-left", rect: Rect{X: 0, Y: 0, W: 40, H: 10}, want: "top-left"},
		{name: "top-right", rect: Rect{X: 80, Y: 0, W: 40, H: 10}, want: "top-right"},
		{name: "bottom-left", rect: Rect{X: 0, Y: 18, W: 40, H: 10}, want: "bottom-left"},
		{name: "bottom-right", rect: Rect{X: 80, Y: 18, W: 40, H: 10}, want: "bottom-right"},
	}
	for _, tc := range cases {
		if got := model.floatingRectPositionAnchor(tc.rect); got != tc.want {
			t.Fatalf("%s: expected %q, got %q", tc.name, tc.want, got)
		}
	}
}

func TestCommandLoadLayoutReadsYAMLAndReplacesWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	layoutDir := filepath.Join(home, ".config", "termx", "layouts")
	if err := os.MkdirAll(layoutDir, 0o755); err != nil {
		t.Fatalf("mkdir layout dir: %v", err)
	}
	layoutPath := filepath.Join(layoutDir, "demo.yaml")
	if err := os.WriteFile(layoutPath, []byte(`
name: demo
tabs:
  - name: coding
    tiling:
      terminal:
        tag: "role=editor"
        command: "vim ."
`), 0o644); err != nil {
		t.Fatalf("write layout file: %v", err)
	}

	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{
				ID:      "shared-001",
				Name:    "editor",
				Command: []string{"vim", "."},
				State:   "running",
				Tags: map[string]string{
					"role": "editor",
				},
			},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("load-layout demo")})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, nextCmd := model.Update(next)
			if nextCmd != nil {
				if follow := mustRunCmd(t, nextCmd); follow != nil {
					_, _ = model.Update(follow)
				}
			}
		}
	}

	if model.workspace.Name != "demo" {
		t.Fatalf("expected workspace demo, got %q", model.workspace.Name)
	}
	tab := model.currentTab()
	if tab == nil || tab.Name != "coding" {
		t.Fatalf("expected coding tab, got %#v", tab)
	}
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil || pane.TerminalID != "shared-001" {
		t.Fatalf("expected attached matched terminal, got %#v", pane)
	}
}

func TestCommandLoadLayoutPromptPolicyOpensPicker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	layoutDir := filepath.Join(home, ".config", "termx", "layouts")
	if err := os.MkdirAll(layoutDir, 0o755); err != nil {
		t.Fatalf("mkdir layout dir: %v", err)
	}
	layoutPath := filepath.Join(layoutDir, "demo.yaml")
	if err := os.WriteFile(layoutPath, []byte(`
name: demo
tabs:
  - name: coding
    tiling:
      terminal:
        tag: "role=missing"
        command: "vim ."
`), 0o644); err != nil {
		t.Fatalf("write layout file: %v", err)
	}

	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("load-layout demo prompt")})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, nextCmd := model.Update(next)
			if nextCmd != nil {
				if follow := mustRunCmd(t, nextCmd); follow != nil {
					_, _ = model.Update(follow)
				}
			}
		}
	}

	if model.workspace.Name != "demo" {
		t.Fatalf("expected workspace demo, got %q", model.workspace.Name)
	}
	if model.terminalPicker == nil {
		t.Fatal("expected prompt policy command to open terminal picker")
	}
	if model.terminalPicker.Action.Kind != terminalPickerActionLayoutResolve {
		t.Fatalf("expected layout resolve picker action, got %#v", model.terminalPicker.Action)
	}
}

func TestCommandLoadLayoutSecondRunReusesPreviouslyCreatedTerminal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	layoutDir := filepath.Join(home, ".config", "termx", "layouts")
	if err := os.MkdirAll(layoutDir, 0o755); err != nil {
		t.Fatalf("mkdir layout dir: %v", err)
	}
	layoutPath := filepath.Join(layoutDir, "demo.yaml")
	if err := os.WriteFile(layoutPath, []byte(`
name: demo
tabs:
  - name: shell
    tiling:
      terminal:
        tag: "role=shell"
        command: "zsh"
`), 0o644); err != nil {
		t.Fatalf("write layout file: %v", err)
	}

	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	first := mustRunCmd(t, model.loadLayoutCmd("demo", LayoutResolveCreate))
	_, cmd := model.Update(first)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	firstPane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if firstPane == nil || firstPane.TerminalID == "" {
		t.Fatalf("expected first loaded pane terminal id, got %#v", firstPane)
	}
	firstID := firstPane.TerminalID
	if client.createCalls != 1 {
		t.Fatalf("expected first load to create one terminal, got %d", client.createCalls)
	}

	second := mustRunCmd(t, model.loadLayoutCmd("demo", LayoutResolveCreate))
	_, cmd = model.Update(second)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	secondPane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if secondPane == nil {
		t.Fatal("expected pane after second load")
	}
	if secondPane.TerminalID != firstID {
		t.Fatalf("expected second load to reuse %q, got %q", firstID, secondPane.TerminalID)
	}
	if client.createCalls != 1 {
		t.Fatalf("expected second load to reuse terminal without creating a duplicate, got %d create calls", client.createCalls)
	}
}

func TestLoadLayoutCommandPrefersProjectLayoutOverUserLayout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	projectDir := t.TempDir()
	t.Chdir(projectDir)

	projectLayoutDir := filepath.Join(projectDir, ".termx", "layouts")
	if err := os.MkdirAll(projectLayoutDir, 0o755); err != nil {
		t.Fatalf("mkdir project layout dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectLayoutDir, "demo.yaml"), []byte(`
name: demo
tabs:
  - name: project-tab
    tiling:
      terminal:
        command: "echo project"
`), 0o644); err != nil {
		t.Fatalf("write project layout file: %v", err)
	}

	userLayoutDir := filepath.Join(home, ".config", "termx", "layouts")
	if err := os.MkdirAll(userLayoutDir, 0o755); err != nil {
		t.Fatalf("mkdir user layout dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userLayoutDir, "demo.yaml"), []byte(`
name: demo
tabs:
  - name: user-tab
    tiling:
      terminal:
        command: "echo user"
`), 0o644); err != nil {
		t.Fatalf("write user layout file: %v", err)
	}

	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("load-layout demo")})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, nextCmd := model.Update(next)
			if nextCmd != nil {
				if follow := mustRunCmd(t, nextCmd); follow != nil {
					_, _ = model.Update(follow)
				}
			}
		}
	}

	tab := model.currentTab()
	if tab == nil || tab.Name != "project-tab" {
		t.Fatalf("expected project layout to win, got %#v", tab)
	}
}

func TestCommandListLayoutsShowsAvailableLayoutNames(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	projectDir := t.TempDir()
	t.Chdir(projectDir)

	projectLayoutDir := filepath.Join(projectDir, ".termx", "layouts")
	if err := os.MkdirAll(projectLayoutDir, 0o755); err != nil {
		t.Fatalf("mkdir project layout dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectLayoutDir, "demo.yaml"), []byte("name: demo\ntabs:\n  - name: p\n    tiling:\n      terminal:\n        command: zsh\n"), 0o644); err != nil {
		t.Fatalf("write project demo layout: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectLayoutDir, "ops.yaml"), []byte("name: ops\ntabs:\n  - name: p\n    tiling:\n      terminal:\n        command: zsh\n"), 0o644); err != nil {
		t.Fatalf("write project ops layout: %v", err)
	}

	userLayoutDir := filepath.Join(home, ".config", "termx", "layouts")
	if err := os.MkdirAll(userLayoutDir, 0o755); err != nil {
		t.Fatalf("mkdir user layout dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userLayoutDir, "demo.yaml"), []byte("name: demo\ntabs:\n  - name: u\n    tiling:\n      terminal:\n        command: zsh\n"), 0o644); err != nil {
		t.Fatalf("write user demo layout: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userLayoutDir, "user-only.yaml"), []byte("name: user-only\ntabs:\n  - name: u\n    tiling:\n      terminal:\n        command: zsh\n"), 0o644); err != nil {
		t.Fatalf("write user-only layout: %v", err)
	}

	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("list-layouts")})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	bar := xansi.Strip(model.renderTabBar())
	if !strings.Contains(bar, "layouts: demo, ops, user-only") {
		t.Fatalf("expected deduped layout list in top bar notice, got %q", bar)
	}
}

func TestCommandDeleteLayoutRemovesResolvedProjectLayoutFirst(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	projectDir := t.TempDir()
	t.Chdir(projectDir)

	projectLayoutDir := filepath.Join(projectDir, ".termx", "layouts")
	if err := os.MkdirAll(projectLayoutDir, 0o755); err != nil {
		t.Fatalf("mkdir project layout dir: %v", err)
	}
	projectPath := filepath.Join(projectLayoutDir, "demo.yaml")
	if err := os.WriteFile(projectPath, []byte("name: demo\ntabs:\n  - name: p\n    tiling:\n      terminal:\n        command: zsh\n"), 0o644); err != nil {
		t.Fatalf("write project demo layout: %v", err)
	}

	userLayoutDir := filepath.Join(home, ".config", "termx", "layouts")
	if err := os.MkdirAll(userLayoutDir, 0o755); err != nil {
		t.Fatalf("mkdir user layout dir: %v", err)
	}
	userPath := filepath.Join(userLayoutDir, "demo.yaml")
	if err := os.WriteFile(userPath, []byte("name: demo\ntabs:\n  - name: u\n    tiling:\n      terminal:\n        command: zsh\n"), 0o644); err != nil {
		t.Fatalf("write user demo layout: %v", err)
	}

	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("delete-layout demo")})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	if _, err := os.Stat(projectPath); !os.IsNotExist(err) {
		t.Fatalf("expected project layout to be deleted first, stat err=%v", err)
	}
	if _, err := os.Stat(userPath); err != nil {
		t.Fatalf("expected user layout to remain, stat err=%v", err)
	}
	if got := xansi.Strip(model.renderTabBar()); !strings.Contains(got, "deleted layout: demo") {
		t.Fatalf("expected delete notice in top bar, got %q", got)
	}
}

func TestCommandDeleteLayoutShowsErrorWhenLayoutMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())

	model := NewModel(&fakeClient{}, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("delete-layout missing")})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	if got := xansi.Strip(model.renderTabBar()); !strings.Contains(got, `layout "missing" not found`) {
		t.Fatalf("expected missing-layout error in top bar, got %q", got)
	}
}

func TestRawPickerInputNavigatesAndKillsSelection(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "orphan-001", Name: "one", Command: []string{"sleep", "1"}, State: "running"},
			{ID: "orphan-002", Name: "two", Command: []string{"sleep", "2"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	_, cmd = model.Update(rawInputMsg{data: []byte("orphan-001")})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}
	_, cmd = model.Update(rawInputMsg{data: []byte{0x7f, '2', 0x0b}})
	if cmd != nil {
		t.Fatalf("expected raw ctrl-k to open stop confirmation first, got %#v", cmd)
	}
	if model.prompt == nil || model.prompt.Kind != "confirm-stop-terminal" {
		t.Fatalf("expected confirm-stop-terminal prompt, got %#v", model.prompt)
	}
	_, cmd = model.Update(rawInputMsg{data: []byte{'\r'}})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)

	if model.prompt != nil {
		t.Fatal("expected stop confirmation prompt to close after enter")
	}
	if len(client.killedIDs) != 1 || client.killedIDs[0] != "orphan-002" {
		t.Fatalf("expected ctrl-k to kill the selected terminal, got %v", client.killedIDs)
	}
	if cmd != nil {
		t.Fatal("expected killing an orphan from picker to keep TUI open")
	}
}

func TestInputEventPickerPasteAndEnterAttach(t *testing.T) {
	client := &fakeClient{
		listResult: []protocol.TerminalInfo{
			{ID: "shared-001", Name: "worker", Command: []string{"tail", "-f", "worker.log"}, State: "running"},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	_, cmd = model.Update(inputEventMsg{event: uv.PasteEvent{Content: "shared"}})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}
	_, cmd = model.Update(inputEventMsg{event: uv.KeyPressEvent{Code: uv.KeyEnter}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if got := model.currentTab().Panes[model.currentTab().ActivePaneID].TerminalID; got != "shared-001" {
		t.Fatalf("expected picker enter to attach shared terminal, got %q", got)
	}
}

func TestInputEventPrefixXDetachesViewport(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	cmd := model.handlePrefixEvent(uv.KeyPressEvent{Code: '%', Text: "%"})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if model.terminalPicker == nil {
		t.Fatal("expected input-event split to open chooser")
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		cmd = commitDefaultTerminalCreatePrompt(t, model)
	}
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	if len(model.currentTab().Panes) != 2 {
		t.Fatalf("expected split via input events, got %d panes", len(model.currentTab().Panes))
	}

	cmd = model.handlePrefixEvent(uv.KeyPressEvent{Code: 'x', Text: "x"})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)

	if cmd != nil {
		t.Fatal("expected detach via input events to keep TUI open")
	}
	if got := len(model.currentTab().Panes); got != 1 {
		t.Fatalf("expected detach via input events to remove one viewport, got %d panes", got)
	}
}

func TestPrefixResizeAdjustsVerticalBoundary(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	createSplitPaneViaPicker(t, model, SplitVertical)

	tab := model.currentTab()
	activeID := tab.ActivePaneID
	before := tab.Root.Rects(Rect{X: 0, Y: 0, W: model.width, H: model.height - 2})[activeID]

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}

	after := tab.Root.Rects(Rect{X: 0, Y: 0, W: model.width, H: model.height - 2})[activeID]
	if after.W <= before.W {
		t.Fatalf("expected active pane width to grow, before=%#v after=%#v", before, after)
	}
	if after.X >= before.X {
		t.Fatalf("expected active pane left edge to move left, before=%#v after=%#v", before, after)
	}
}

func TestPrefixResizeAdjustsHorizontalBoundary(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	createSplitPaneViaPicker(t, model, SplitHorizontal)

	tab := model.currentTab()
	activeID := tab.ActivePaneID
	before := tab.Root.Rects(Rect{X: 0, Y: 0, W: model.width, H: model.height - 2})[activeID]

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}

	after := tab.Root.Rects(Rect{X: 0, Y: 0, W: model.width, H: model.height - 2})[activeID]
	if after.H <= before.H {
		t.Fatalf("expected active pane height to grow, before=%#v after=%#v", before, after)
	}
	if after.Y >= before.Y {
		t.Fatalf("expected active pane top edge to move up, before=%#v after=%#v", before, after)
	}
}

func TestPrefixSpaceCyclesPredefinedLayouts(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	for _, dir := range []SplitDirection{SplitVertical, SplitHorizontal, SplitVertical} {
		createSplitPaneViaPicker(t, model, dir)
	}

	tab := model.currentTab()
	rootRect := Rect{X: 0, Y: 0, W: model.width, H: model.height - 2}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}
	rects := tab.Root.Rects(rootRect)
	for paneID, rect := range rects {
		if rect.W != rootRect.W {
			t.Fatalf("expected even-horizontal full width for %s, got %#v", paneID, rect)
		}
	}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}
	rects = tab.Root.Rects(rootRect)
	for paneID, rect := range rects {
		if rect.H != rootRect.H {
			t.Fatalf("expected even-vertical full height for %s, got %#v", paneID, rect)
		}
	}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}
	ids := tab.Root.LeafIDs()
	rects = tab.Root.Rects(rootRect)
	main := rects[ids[0]]
	if main.W <= rootRect.W/2 {
		t.Fatalf("expected main-horizontal first pane to be wide, got %#v", main)
	}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}
	ids = tab.Root.LeafIDs()
	rects = tab.Root.Rects(rootRect)
	main = rects[ids[0]]
	if main.H <= rootRect.H/2 {
		t.Fatalf("expected main-vertical first pane to be tall, got %#v", main)
	}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}
	rects = tab.Root.Rects(rootRect)
	for paneID, rect := range rects {
		if rect.W == rootRect.W || rect.H == rootRect.H {
			t.Fatalf("expected tiled layout to constrain both dimensions for %s, got %#v", paneID, rect)
		}
	}
}

func TestPrefixRenameTabCommitsNewName(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}})

	for _, r := range []rune("editor") {
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if got := model.currentTab().Name; got != "editor" {
		t.Fatalf("expected renamed tab to be %q, got %q", "editor", got)
	}
}

func TestRawRenameTabCanBeCanceled(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	original := model.currentTab().Name

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}})

	_, cmd := model.Update(rawInputMsg{data: []byte("logs")})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}
	_, cmd = model.Update(rawInputMsg{data: []byte{0x1b}})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}

	if got := model.currentTab().Name; got != original {
		t.Fatalf("expected tab rename cancel to keep %q, got %q", original, got)
	}
}

func TestPaneTitleUsesCommandName(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/usr/bin/fish"})
	model.width = 100
	model.height = 24

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	view := xansi.Strip(model.View())
	if !strings.Contains(view, "fish-1") {
		t.Fatalf("expected pane title to include friendly command-based name, got:\n%s", view)
	}
}

func TestModelResizeMessageResizesLocalVTerm(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}

	_, _ = model.Update(paneResizeMsg{channel: pane.Channel, cols: 90, rows: 33})

	screen := pane.VTerm.ScreenContent()
	if len(screen.Cells) != 33 {
		t.Fatalf("expected 33 rows, got %d", len(screen.Cells))
	}
	if len(screen.Cells) == 0 || len(screen.Cells[0]) != 90 {
		t.Fatalf("expected 90 cols, got %d", len(screen.Cells[0]))
	}
}

func TestModelResizeMessageResizesAllSharedTerminalVTerms(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	base := tab.Panes[tab.ActivePaneID]
	if base == nil {
		t.Fatal("expected active pane")
	}

	sharedSnap := &protocol.Snapshot{
		TerminalID: base.TerminalID,
		Size:       protocol.Size{Cols: 72, Rows: 18},
	}
	floatPane := &Pane{
		ID:    "pane-shared-float",
		Title: "shared-float",
		Viewport: &Viewport{
			TerminalID: base.TerminalID,
			Channel:    base.Channel + 100,
			VTerm:      localvterm.New(72, 18, 100, nil),
			Snapshot:   sharedSnap,
			Mode:       ViewportModeFixed,
		},
	}
	tab.Panes[floatPane.ID] = floatPane
	tab.Floating = append(tab.Floating, &FloatingPane{
		PaneID: floatPane.ID,
		Rect:   Rect{X: 6, Y: 3, W: 40, H: 12},
		Z:      1,
	})

	secondTab := newTab("2")
	secondPane := &Pane{
		ID:    "pane-shared-other-tab",
		Title: "shared-other-tab",
		Viewport: &Viewport{
			TerminalID: base.TerminalID,
			Channel:    base.Channel + 200,
			VTerm:      localvterm.New(64, 16, 100, nil),
			Snapshot: &protocol.Snapshot{
				TerminalID: base.TerminalID,
				Size:       protocol.Size{Cols: 64, Rows: 16},
			},
			Mode: ViewportModeFit,
		},
	}
	secondTab.Panes[secondPane.ID] = secondPane
	secondTab.ActivePaneID = secondPane.ID
	secondTab.Root = NewLeaf(secondPane.ID)
	model.workspace.Tabs = append(model.workspace.Tabs, secondTab)

	_, _ = model.Update(paneResizeMsg{channel: base.Channel, cols: 90, rows: 33})

	for _, pane := range []*Pane{base, floatPane, secondPane} {
		if pane == nil || pane.VTerm == nil {
			t.Fatalf("expected runtime for pane %#v", pane)
		}
		cols, rows := pane.VTerm.Size()
		if cols != 90 || rows != 33 {
			t.Fatalf("expected pane %q to resize to 90x33, got %dx%d", pane.ID, cols, rows)
		}
		if pane.Snapshot == nil {
			t.Fatalf("expected pane %q snapshot", pane.ID)
		}
		if pane.Snapshot.Size.Cols != 90 || pane.Snapshot.Size.Rows != 33 {
			t.Fatalf("expected pane %q snapshot size 90x33, got %+v", pane.ID, pane.Snapshot.Size)
		}
	}
	if floatPane.Mode != ViewportModeFixed {
		t.Fatalf("expected floating shared pane to remain fixed, got %q", floatPane.Mode)
	}
}

func TestResizeVisiblePanesUsesPreservedSharedOwner(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	base := tab.Panes[tab.ActivePaneID]
	if base == nil {
		t.Fatal("expected active pane")
	}

	sharedPane := &Pane{
		ID:    "pane-shared-other",
		Title: "shared-other",
		Viewport: &Viewport{
			TerminalID: base.TerminalID,
			Channel:    base.Channel + 100,
			VTerm:      localvterm.New(64, 16, 100, nil),
			Snapshot: &protocol.Snapshot{
				TerminalID: base.TerminalID,
				Size:       protocol.Size{Cols: 64, Rows: 16},
			},
			Mode: ViewportModeFit,
		},
	}
	tab.Panes[sharedPane.ID] = sharedPane
	tab.Floating = append(tab.Floating, &FloatingPane{
		PaneID: sharedPane.ID,
		Rect:   Rect{X: 6, Y: 3, W: 40, H: 12},
		Z:      1,
	})

	runAllCmds(t, model.resizeVisiblePanesCmd())

	if client.resizeCalls != 1 {
		t.Fatalf("expected shared terminal resize to keep using preserved owner, got %d resize calls", client.resizeCalls)
	}
	if client.resizeChannel != base.Channel {
		t.Fatalf("expected preserved owner channel %d to drive resize, got %d", base.Channel, client.resizeChannel)
	}
}

func TestResizeVisiblePanesUsesAcquiredSharedPane(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	base := tab.Panes[tab.ActivePaneID]
	if base == nil {
		t.Fatal("expected active pane")
	}
	base.ResizeAcquired = true

	sharedPane := &Pane{
		ID:    "pane-shared-other",
		Title: "shared-other",
		Viewport: &Viewport{
			TerminalID: base.TerminalID,
			Channel:    base.Channel + 100,
			VTerm:      localvterm.New(64, 16, 100, nil),
			Snapshot: &protocol.Snapshot{
				TerminalID: base.TerminalID,
				Size:       protocol.Size{Cols: 64, Rows: 16},
			},
			Mode: ViewportModeFit,
		},
	}
	tab.Panes[sharedPane.ID] = sharedPane
	tab.Floating = append(tab.Floating, &FloatingPane{
		PaneID: sharedPane.ID,
		Rect:   Rect{X: 6, Y: 3, W: 40, H: 12},
		Z:      1,
	})

	runAllCmds(t, model.resizeVisiblePanesCmd())

	if client.resizeCalls != 1 {
		t.Fatalf("expected one resize call for acquired shared pane, got %d", client.resizeCalls)
	}
	if client.resizeChannel != base.Channel {
		t.Fatalf("expected acquired pane channel %d to drive resize, got %d", base.Channel, client.resizeChannel)
	}
}

func TestResizeVisiblePanesUsesResizerForOwnerPane(t *testing.T) {
	client := &fakeClient{}
	resizerClient := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	pane.ResizeAcquired = true
	model.app = NewApp(model.workbench, model.app.TerminalCoordinator(), NewResizer(NewTerminalCoordinator(resizerClient, model.terminalStore)), model.app.RenderLoop())

	runAllCmds(t, model.resizeVisiblePanesCmd())

	if client.resizeCalls != 0 {
		t.Fatalf("expected model client resize path to be bypassed, got %d calls", client.resizeCalls)
	}
	if resizerClient.resizeCalls != 1 {
		t.Fatalf("expected resizer client to receive one resize call, got %d", resizerClient.resizeCalls)
	}
	if resizerClient.resizeChannel != pane.Channel {
		t.Fatalf("expected resizer to drive channel %d, got %d", pane.Channel, resizerClient.resizeChannel)
	}
}

func TestFloatingModeResizeUsesAcquiredSharedPane(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	base := tab.Panes[tab.ActivePaneID]
	if base == nil {
		t.Fatal("expected active pane")
	}
	clearTerminalResizeAcquire(model.workspace.Tabs, base.TerminalID)

	floatPane := &Pane{
		ID:    "pane-shared-float",
		Title: "shared-float",
		Viewport: &Viewport{
			TerminalID: base.TerminalID,
			Channel:    base.Channel + 100,
			VTerm:      localvterm.New(64, 16, 100, nil),
			Snapshot: &protocol.Snapshot{
				TerminalID: base.TerminalID,
				Size:       protocol.Size{Cols: 64, Rows: 16},
			},
			Mode:           ViewportModeFit,
			ResizeAcquired: true,
			renderDirty:    true,
		},
	}
	tab.Panes[floatPane.ID] = floatPane
	tab.Floating = append(tab.Floating, &FloatingPane{
		PaneID: floatPane.ID,
		Rect:   Rect{X: 6, Y: 3, W: 40, H: 12},
		Z:      1,
	})
	tab.ActivePaneID = floatPane.ID

	result := model.dispatchFloatingModeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	runAllCmds(t, result.cmd)

	if client.resizeCalls != 1 {
		t.Fatalf("expected floating resize to trigger one resize call after acquire, got %d", client.resizeCalls)
	}
	if client.resizeChannel != floatPane.Channel {
		t.Fatalf("expected floating pane channel %d to drive resize, got %d", floatPane.Channel, client.resizeChannel)
	}
	if tab.Floating[0].Rect.W <= 40 {
		t.Fatalf("expected floating rect width to grow, got %+v", tab.Floating[0].Rect)
	}
}

func TestAttachFloatingSharedPaneKeepsExistingResizeOwner(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	base := tab.Panes[tab.ActivePaneID]
	if base == nil {
		t.Fatal("expected active pane")
	}
	if connection := paneConnectionStatus(model.workspace.Tabs, base); connection != "owner" {
		t.Fatalf("expected initial pane to start as owner, got %q", connection)
	}

	floatPane := &Pane{
		ID:    "pane-shared-float",
		Title: "shared-float",
		Viewport: &Viewport{
			TerminalID: base.TerminalID,
			Channel:    base.Channel + 100,
			VTerm:      localvterm.New(64, 16, 100, nil),
			Snapshot: &protocol.Snapshot{
				TerminalID: base.TerminalID,
				Size:       protocol.Size{Cols: 64, Rows: 16},
			},
			Mode: ViewportModeFit,
		},
	}

	model.attachPane(paneCreatedMsg{
		tabIndex: model.workspace.ActiveTab,
		floating: true,
		pane:     floatPane,
	})

	if !base.ResizeAcquired {
		t.Fatal("expected existing tiled pane to keep resize ownership after floating attach")
	}
	if floatPane.ResizeAcquired {
		t.Fatal("expected newly attached floating pane to stay follower by default")
	}
	if connection := paneConnectionStatus(model.workspace.Tabs, base); connection != "owner" {
		t.Fatalf("expected tiled pane to remain owner, got %q", connection)
	}
	if connection := paneConnectionStatus(model.workspace.Tabs, floatPane); connection != "follower" {
		t.Fatalf("expected floating pane to start as follower, got %q", connection)
	}
}

func TestViewRefreshesOwnerBadgesAndFocusBordersAfterSharedFloatingAttach(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	initial := model.View()
	if !strings.Contains(initial, "38;2;74;222;128") {
		t.Fatalf("expected initial active pane to render green border, got:\n%s", initial)
	}

	tab := model.currentTab()
	base := tab.Panes[tab.ActivePaneID]
	if base == nil {
		t.Fatal("expected active pane")
	}
	floatPane := &Pane{
		ID:    "pane-shared-float",
		Title: "shared-float",
		Viewport: &Viewport{
			TerminalID: base.TerminalID,
			Channel:    base.Channel + 100,
			VTerm:      localvterm.New(64, 16, 100, nil),
			Snapshot: &protocol.Snapshot{
				TerminalID: base.TerminalID,
				Size:       protocol.Size{Cols: 64, Rows: 16},
			},
			Mode: ViewportModeFit,
		},
	}
	model.attachPane(paneCreatedMsg{
		tabIndex: model.workspace.ActiveTab,
		floating: true,
		pane:     floatPane,
	})

	screen := model.View()
	stripped := xansi.Strip(screen)
	if strings.Count(stripped, "owner") != 1 {
		t.Fatalf("expected exactly one owner badge after floating attach, got:\n%s", stripped)
	}
	if strings.Count(stripped, "owner") > 1 {
		t.Fatalf("expected floating attach not to duplicate owner badges, got:\n%s", stripped)
	}
	if !strings.Contains(stripped, "follower") {
		t.Fatalf("expected floating attach to show a follower badge, got:\n%s", stripped)
	}
	if !strings.Contains(screen, "38;2;74;222;128") {
		t.Fatalf("expected focused pane to keep green border, got:\n%s", screen)
	}
	if !strings.Contains(screen, "38;2;209;213;219") {
		t.Fatalf("expected unfocused pane to redraw with gray border, got:\n%s", screen)
	}
}

func TestMouseResizeFloatingPaneUsesAcquiredSharedPane(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	base := tab.Panes[tab.ActivePaneID]
	if base == nil {
		t.Fatal("expected active pane")
	}
	clearTerminalResizeAcquire(model.workspace.Tabs, base.TerminalID)

	floatPane := &Pane{
		ID:    "pane-shared-float",
		Title: "shared-float",
		Viewport: &Viewport{
			TerminalID: base.TerminalID,
			Channel:    base.Channel + 100,
			VTerm:      localvterm.New(64, 16, 100, nil),
			Snapshot: &protocol.Snapshot{
				TerminalID: base.TerminalID,
				Size:       protocol.Size{Cols: 64, Rows: 16},
			},
			Mode:           ViewportModeFit,
			ResizeAcquired: true,
			renderDirty:    true,
		},
	}
	tab.Panes[floatPane.ID] = floatPane
	tab.Floating = append(tab.Floating, &FloatingPane{
		PaneID: floatPane.ID,
		Rect:   Rect{X: 6, Y: 3, W: 40, H: 12},
		Z:      1,
	})
	tab.ActivePaneID = floatPane.ID
	model.mouseDragPaneID = floatPane.ID
	model.mouseDragMode = mouseDragResize

	cmd := model.handleMouseMotionEvent(uv.MouseMotionEvent{X: 52, Y: 18, Button: uv.MouseLeft})
	runAllCmds(t, cmd)

	if client.resizeCalls != 1 {
		t.Fatalf("expected mouse resize to trigger one resize call after acquire, got %d", client.resizeCalls)
	}
	if client.resizeChannel != floatPane.Channel {
		t.Fatalf("expected floating pane channel %d to drive resize, got %d", floatPane.Channel, client.resizeChannel)
	}
	if tab.Floating[0].Rect.W <= 40 || tab.Floating[0].Rect.H <= 12 {
		t.Fatalf("expected mouse resize to grow floating rect, got %+v", tab.Floating[0].Rect)
	}
}

func TestStartPaneStreamSkipsDuplicateSharedTerminalStreams(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	first := &Pane{
		ID:    "pane-1",
		Title: "one",
		Viewport: &Viewport{
			TerminalID: "term-shared",
			Channel:    1,
		},
	}
	second := &Pane{
		ID:    "pane-2",
		Title: "two",
		Viewport: &Viewport{
			TerminalID: "term-shared",
			Channel:    2,
		},
	}
	model.workspace.Tabs = []*Tab{{
		Name:         "1",
		Panes:        map[string]*Pane{first.ID: first, second.ID: second},
		ActivePaneID: first.ID,
	}}

	model.startPaneStream(first)
	model.startPaneStream(second)

	if client.streamCalls != 1 {
		t.Fatalf("expected one stream source for shared terminal, got %d calls on channels %v", client.streamCalls, client.streamChannels)
	}
	if !first.HasStopStream() {
		t.Fatal("expected first pane to own shared terminal stream")
	}
	if second.HasStopStream() {
		t.Fatal("expected second pane to mirror shared terminal without starting another stream")
	}
}

func TestRemovePanePromotesSharedTerminalStreamOwner(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	first := &Pane{
		ID:    "pane-1",
		Title: "one",
		Viewport: &Viewport{
			TerminalID: "term-shared",
			Channel:    1,
		},
	}
	second := &Pane{
		ID:    "pane-2",
		Title: "two",
		Viewport: &Viewport{
			TerminalID: "term-shared",
			Channel:    2,
		},
	}
	tab := &Tab{
		Name:         "1",
		Panes:        map[string]*Pane{first.ID: first, second.ID: second},
		ActivePaneID: first.ID,
		Root:         NewLeaf(first.ID),
	}
	tab.Root.Split(first.ID, SplitVertical, second.ID)
	model.workspace.Tabs = []*Tab{tab}

	model.startPaneStream(first)
	model.startPaneStream(second)
	if client.streamCalls != 1 {
		t.Fatalf("expected one initial stream source, got %d", client.streamCalls)
	}

	if removed := model.removePane(first.ID); removed {
		t.Fatal("expected tab to remain after removing first shared pane")
	}

	if client.streamCalls != 2 {
		t.Fatalf("expected second shared pane to become stream owner after removal, got %d calls on channels %v", client.streamCalls, client.streamChannels)
	}
	if !second.HasStopStream() {
		t.Fatal("expected second pane to own stream after first pane removal")
	}
}

func TestRemovePaneTransfersSharedTerminalResizeOwner(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	first := &Pane{
		ID:    "pane-1",
		Title: "one",
		Viewport: &Viewport{
			TerminalID:     "term-shared",
			Channel:        1,
			ResizeAcquired: true,
		},
	}
	second := &Pane{
		ID:    "pane-2",
		Title: "two",
		Viewport: &Viewport{
			TerminalID: "term-shared",
			Channel:    2,
		},
	}
	tab := &Tab{
		Name:         "1",
		Panes:        map[string]*Pane{first.ID: first, second.ID: second},
		ActivePaneID: first.ID,
		Root:         NewLeaf(first.ID),
	}
	tab.Root.Split(first.ID, SplitVertical, second.ID)
	model.workspace.Tabs = []*Tab{tab}

	if removed := model.removePane(first.ID); removed {
		t.Fatal("expected tab to remain after removing first shared pane")
	}
	if got := paneConnectionStatus(model.workspace.Tabs, second); got != "owner" {
		t.Fatalf("expected remaining pane to become owner, got %q", got)
	}
	if !second.ResizeAcquired {
		t.Fatal("expected remaining pane owner flag set")
	}
}

func TestUnbindPaneTerminalTransfersSharedTerminalResizeOwner(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	first := &Pane{
		ID:    "pane-1",
		Title: "one",
		Viewport: &Viewport{
			TerminalID:     "term-shared",
			Channel:        1,
			ResizeAcquired: true,
		},
	}
	second := &Pane{
		ID:    "pane-2",
		Title: "two",
		Viewport: &Viewport{
			TerminalID: "term-shared",
			Channel:    2,
		},
	}
	tab := &Tab{
		Name:         "1",
		Panes:        map[string]*Pane{first.ID: first, second.ID: second},
		ActivePaneID: first.ID,
		Root:         NewLeaf(first.ID),
	}
	tab.Root.Split(first.ID, SplitVertical, second.ID)
	model.workspace.Tabs = []*Tab{tab}

	model.unbindPaneTerminal(first)

	if first.TerminalID != "" {
		t.Fatalf("expected first pane unbound, got terminal %q", first.TerminalID)
	}
	if got := paneConnectionStatus(model.workspace.Tabs, second); got != "owner" {
		t.Fatalf("expected second pane to become owner after unbind, got %q", got)
	}
	if !second.ResizeAcquired {
		t.Fatal("expected second pane owner flag set after unbind")
	}
}

func TestHandlePaneOutputMirrorsSharedTerminalToAllPanes(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 30

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	base := tab.Panes[tab.ActivePaneID]
	if base == nil {
		t.Fatal("expected active pane")
	}
	floatPane := &Pane{
		ID:    "pane-shared-float",
		Title: "shared-float",
		Viewport: &Viewport{
			TerminalID: base.TerminalID,
			Channel:    base.Channel + 100,
			VTerm:      localvterm.New(80, 24, 100, nil),
			Snapshot: &protocol.Snapshot{
				TerminalID: base.TerminalID,
				Size:       protocol.Size{Cols: 80, Rows: 24},
			},
			Mode: ViewportModeFixed,
		},
	}
	tab.Panes[floatPane.ID] = floatPane
	tab.Floating = append(tab.Floating, &FloatingPane{
		PaneID: floatPane.ID,
		Rect:   Rect{X: 8, Y: 3, W: 40, H: 12},
		Z:      1,
	})

	payload := []byte("\x1b[?1049h\x1b[2J\x1b[HSL-ROW-0\r\nSL-ROW-1\r\nSL-ROW-2")
	if cmd := model.handlePaneOutput(paneOutputMsg{
		paneID: base.ID,
		frame: protocol.StreamFrame{
			Type:    protocol.TypeOutput,
			Payload: payload,
		},
	}); cmd != nil {
		t.Fatalf("expected mirror output path not to require recovery, got cmd")
	}

	for _, pane := range []*Pane{base, floatPane} {
		if pane == nil || pane.VTerm == nil {
			t.Fatalf("expected runtime for pane %#v", pane)
		}
		if !pane.VTerm.IsAltScreen() {
			t.Fatalf("expected pane %q to mirror alternate screen state", pane.ID)
		}
		body := xansi.Strip(strings.Join(model.paneLines(pane), "\n"))
		if !containsAll(body, "SL-ROW-0", "SL-ROW-1", "SL-ROW-2") {
			t.Fatalf("expected pane %q to mirror shared output, got:\n%s", pane.ID, body)
		}
	}
}

func TestModelHandlePaneOutputWriteErrorTriggersRecovery(t *testing.T) {
	client := &fakeClient{
		snapshotByID: map[string]*protocol.Snapshot{
			"term-001": {
				TerminalID: "term-001",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen: protocol.ScreenData{
					Cells: [][]protocol.Cell{
						{{Content: "R", Width: 1}, {Content: "E", Width: 1}, {Content: "C", Width: 1}},
					},
				},
				Cursor: protocol.CursorState{Row: 0, Col: 3, Visible: true},
			},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}

	model.paneWriter = func(*Pane, []byte) (int, error) {
		return 0, errors.New("write failed")
	}

	cmd := model.handlePaneOutput(paneOutputMsg{
		paneID: pane.ID,
		frame:  protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("hello")},
	})
	if cmd == nil {
		t.Fatal("expected recovery command after pane write failure")
	}
	if !pane.syncLost {
		t.Fatal("expected write failure to mark pane syncLost")
	}
	if !pane.recovering {
		t.Fatal("expected write failure to mark pane recovering")
	}

	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if pane.syncLost {
		t.Fatal("expected recovery to clear syncLost")
	}
	if pane.recovering {
		t.Fatal("expected recovery to clear recovering flag")
	}
	if pane.Snapshot == nil || pane.Snapshot.Size.Cols != 80 || pane.Snapshot.Size.Rows != 24 {
		t.Fatalf("expected recovered snapshot size 80x24, got %#v", pane.Snapshot)
	}
}

func TestPaneCellsUsesCacheUntilDirty(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	first := paneCells(pane)
	second := paneCells(pane)
	if len(first) == 0 || len(second) == 0 {
		t.Fatal("expected cached pane cells")
	}
	if &first[0] != &second[0] {
		t.Fatal("expected second call to reuse cached lines")
	}

	_, _ = pane.VTerm.Write([]byte("changed"))
	pane.live = true
	pane.MarkRenderDirty()
	third := paneCells(pane)
	if &third[0] == &second[0] {
		t.Fatal("expected dirty pane to rebuild cache")
	}
}

func TestOneDirtyPaneDoesNotInvalidateSiblingCaches(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 120
	model.height = 40

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		_, _ = model.Update(mustRunCmd(t, cmd))
	}

	createSplitPaneViaPicker(t, model, SplitVertical)

	tab := model.currentTab()
	ids := tab.Root.LeafIDs()
	if len(ids) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(ids))
	}

	left := tab.Panes[ids[0]]
	right := tab.Panes[ids[1]]
	if left == nil || right == nil {
		t.Fatal("expected both panes")
	}

	_, _ = left.VTerm.Write([]byte("left side"))
	left.live = true
	left.MarkRenderDirty()
	_, _ = right.VTerm.Write([]byte("right side"))
	right.live = true
	right.MarkRenderDirty()

	_ = model.View()
	if left.IsRenderDirty() || right.IsRenderDirty() {
		t.Fatal("expected initial render to clear dirty flags")
	}
	leftCached := firstCellPtr(left.CellCache())
	rightCached := firstCellPtr(right.CellCache())

	_, _ = left.VTerm.Write([]byte("\r\nchanged"))
	left.live = true
	left.MarkRenderDirty()

	_ = model.View()

	if left.IsRenderDirty() {
		t.Fatal("expected dirty pane render to be flushed")
	}
	if got := firstCellPtr(right.CellCache()); got != rightCached {
		t.Fatal("expected clean sibling pane cache to be reused")
	}
	if leftCached == nil && firstCellPtr(left.CellCache()) != nil {
		t.Fatal("expected live pane direct render to avoid populating full grid cache")
	}
}

func TestPaneOutputWaitsForRenderTickWhenBatchingEnabled(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 24
	model.renderBatching = true
	model.program = &tea.Program{}
	now := time.Unix(0, 0)
	model.timeNow = func() time.Time { return now }

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	tab := model.currentTab()
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}

	initial := model.View()

	_, _ = model.Update(paneOutputMsg{
		paneID: pane.ID,
		frame: protocol.StreamFrame{
			Type:    protocol.TypeOutput,
			Payload: []byte("batched output"),
		},
	})

	if got := model.View(); got != initial {
		t.Fatal("expected view to stay cached before render tick")
	}

	now = now.Add(model.renderInterval)
	_, _ = model.Update(renderTickMsg{})
	if got := xansi.Strip(model.View()); !strings.Contains(got, "batched output") {
		t.Fatalf("expected render tick to flush pending output, got:\n%s", got)
	}
}

func TestFlushPendingRenderUsesInteractiveFramePacing(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.renderBatching = true
	model.program = &tea.Program{}
	model.renderInterval = 16 * time.Millisecond
	model.renderFastInterval = 8 * time.Millisecond

	now := time.Unix(0, 0)
	model.timeNow = func() time.Time { return now }
	model.renderDirty = false
	model.renderLastFlush = now
	model.renderPending.Store(true)

	now = now.Add(8 * time.Millisecond)
	model.flushPendingRender()
	if model.renderDirty {
		t.Fatal("expected idle 8ms tick to stay below flush interval")
	}
	if !model.renderPending.Load() {
		t.Fatal("expected pending render to stay queued before idle interval elapses")
	}

	model.noteInteraction()
	model.flushPendingRender()
	if !model.renderDirty {
		t.Fatal("expected interactive render window to allow fast flush")
	}
	if model.renderPending.Load() {
		t.Fatal("expected pending render to clear after interactive flush")
	}
}

func TestHandleInputEventExtendsInteractiveRenderWindow(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	now := time.Unix(0, 0)
	model.timeNow = func() time.Time { return now }
	model.renderInteractiveWindow = 125 * time.Millisecond

	_ = model.handleInputEvent(uv.MouseMotionEvent{X: 12, Y: 7, Button: uv.MouseLeft})

	want := now.Add(125 * time.Millisecond)
	if !model.renderInteractiveUntil.Equal(want) {
		t.Fatalf("expected interactive deadline %s, got %s", want, model.renderInteractiveUntil)
	}
}

func TestLogRenderStatsWritesCountersToLogger(t *testing.T) {
	var buf bytes.Buffer
	model := NewModel(&fakeClient{}, Config{
		DefaultShell: "/bin/sh",
		Logger:       slog.New(slog.NewTextHandler(&buf, nil)),
	})
	model.renderStatsInterval = 10 * time.Second
	model.renderViewCalls.Add(12)
	model.renderFrames.Add(5)
	model.renderCacheHits.Add(7)

	model.logRenderStats()

	out := buf.String()
	if !strings.Contains(out, "tui render stats") || !strings.Contains(out, "view_calls=12") || !strings.Contains(out, "frames=5") || !strings.Contains(out, "cache_hits=7") {
		t.Fatalf("expected render stats log line, got %q", out)
	}
}

func TestSyncLostRecoversPaneFromSnapshotAndKeepsStreaming(t *testing.T) {
	client := &fakeClient{
		snapshotByID: map[string]*protocol.Snapshot{
			"pane-001": {
				TerminalID: "pane-001",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen: protocol.ScreenData{
					Cells: [][]protocol.Cell{
						{
							{Content: "r", Width: 1},
							{Content: "e", Width: 1},
							{Content: "s", Width: 1},
							{Content: "y", Width: 1},
							{Content: "n", Width: 1},
							{Content: "c", Width: 1},
						},
					},
				},
				Cursor: protocol.CursorState{Row: 0, Col: 6, Visible: true},
				Modes:  protocol.TerminalModes{AutoWrap: true},
			},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 24

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	_, _ = pane.VTerm.Write([]byte("stale"))
	pane.live = true
	pane.MarkRenderDirty()

	_, cmd := model.Update(paneOutputMsg{
		paneID: pane.ID,
		frame:  protocol.StreamFrame{Type: protocol.TypeSyncLost, Payload: protocol.EncodeSyncLostPayload(128)},
	})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	view := xansi.Strip(model.View())
	if !strings.Contains(view, "resync") {
		t.Fatalf("expected snapshot content after sync lost, got:\n%s", view)
	}
	if pane.syncLost {
		t.Fatal("expected syncLost flag to clear after recovery")
	}

	_, cmd = model.Update(paneOutputMsg{
		paneID: pane.ID,
		frame:  protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("!")},
	})
	if cmd != nil {
		if msg := mustRunCmd(t, cmd); msg != nil {
			_, _ = model.Update(msg)
		}
	}
	_, _ = model.Update(renderTickMsg{})

	view = xansi.Strip(model.View())
	if !strings.Contains(view, "resync!") {
		t.Fatalf("expected streaming to continue from recovered snapshot, got:\n%s", view)
	}
}

func TestPaneOutputRecreatesMissingRuntimeWithoutPanic(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 100
	model.height = 24

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	pane.Snapshot = &protocol.Snapshot{
		TerminalID: pane.TerminalID,
		Size:       protocol.Size{Cols: 80, Rows: 24},
	}
	pane.VTerm = nil
	pane.live = false

	_, cmd := model.Update(paneOutputMsg{
		paneID: pane.ID,
		frame:  protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("ok")},
	})
	if cmd != nil {
		t.Fatalf("expected output handling to stay synchronous, got %#v", cmd)
	}
	if pane.VTerm == nil {
		t.Fatal("expected missing runtime VTerm to be recreated")
	}
	if !pane.live {
		t.Fatal("expected pane to return to live mode after runtime recreation")
	}
}

func TestSyncLostWhileRecoveryInFlightDoesNotQueueDuplicateSnapshots(t *testing.T) {
	client := &fakeClient{
		snapshotByID: map[string]*protocol.Snapshot{
			"pane-001": {
				TerminalID: "pane-001",
				Size:       protocol.Size{Cols: 80, Rows: 24},
				Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "o", Width: 1}, {Content: "k", Width: 1}}}},
				Cursor:     protocol.CursorState{Row: 0, Col: 2, Visible: true},
				Modes:      protocol.TerminalModes{AutoWrap: true},
			},
		},
	}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	baselineCalls := client.snapshotCalls

	_, firstCmd := model.Update(paneOutputMsg{
		paneID: pane.ID,
		frame:  protocol.StreamFrame{Type: protocol.TypeSyncLost, Payload: protocol.EncodeSyncLostPayload(64)},
	})
	if firstCmd == nil {
		t.Fatal("expected first sync lost to request snapshot recovery")
	}

	_, secondCmd := model.Update(paneOutputMsg{
		paneID: pane.ID,
		frame:  protocol.StreamFrame{Type: protocol.TypeSyncLost, Payload: protocol.EncodeSyncLostPayload(64)},
	})
	if secondCmd != nil {
		t.Fatal("expected duplicate sync lost to avoid a second snapshot request while recovering")
	}
	if client.snapshotCalls != baselineCalls {
		t.Fatalf("expected snapshot command to be deferred until run, got %d calls", client.snapshotCalls-baselineCalls)
	}

	msg = mustRunCmd(t, firstCmd)
	_, _ = model.Update(msg)
	if client.snapshotCalls != baselineCalls+1 {
		t.Fatalf("expected exactly one snapshot request, got %d", client.snapshotCalls-baselineCalls)
	}
}

func TestSyncLostRecoveryFailureAllowsRetry(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}

	client.snapshotErr = errors.New("boom")
	_, cmd = model.Update(paneOutputMsg{
		paneID: pane.ID,
		frame:  protocol.StreamFrame{Type: protocol.TypeSyncLost, Payload: protocol.EncodeSyncLostPayload(64)},
	})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if pane.recovering {
		t.Fatal("expected failed recovery to clear recovering flag")
	}

	client.snapshotErr = nil
	client.snapshotByID = map[string]*protocol.Snapshot{
		"pane-001": {
			TerminalID: "pane-001",
			Size:       protocol.Size{Cols: 80, Rows: 24},
			Screen:     protocol.ScreenData{Cells: [][]protocol.Cell{{{Content: "o", Width: 1}, {Content: "k", Width: 1}}}},
			Cursor:     protocol.CursorState{Row: 0, Col: 2, Visible: true},
			Modes:      protocol.TerminalModes{AutoWrap: true},
		},
	}
	_, cmd = model.Update(paneOutputMsg{
		paneID: pane.ID,
		frame:  protocol.StreamFrame{Type: protocol.TypeSyncLost, Payload: protocol.EncodeSyncLostPayload(64)},
	})
	if cmd == nil {
		t.Fatal("expected retry sync lost to request snapshot again")
	}
}

func TestContinuousDirtyPaneEntersCatchingUpAndEventuallyRecovers(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.renderBatching = true
	model.program = &tea.Program{}
	model.width = 220
	now := time.Unix(0, 0)
	model.timeNow = func() time.Time { return now }

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}

	for i := 0; i < 30; i++ {
		_, cmd := model.Update(paneOutputMsg{
			paneID: pane.ID,
			frame:  protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("x")},
		})
		if cmd != nil {
			if next := mustRunCmd(t, cmd); next != nil {
				_, _ = model.Update(next)
			}
		}
		now = now.Add(model.renderInterval)
		_, _ = model.Update(renderTickMsg{})
	}

	if !pane.IsCatchingUp() {
		t.Fatal("expected pane to enter catching-up mode after sustained dirty ticks")
	}
	if got := xansi.Strip(model.renderStatus()); !strings.Contains(got, "0B") {
		t.Fatalf("expected status to mention catching-up, got %q", got)
	}

	for i := 0; i < 5; i++ {
		pane.ClearRenderDirty()
		now = now.Add(model.renderInterval)
		_, _ = model.Update(renderTickMsg{})
	}

	if pane.IsCatchingUp() {
		t.Fatal("expected pane to leave catching-up mode after clean ticks")
	}
}

func TestFixedModeViewportRendersCroppedContentAroundCursor(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 11
	model.height = 8

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	follower := &Pane{
		ID: "pane-follower",
		Viewport: &Viewport{
			TerminalID: pane.TerminalID,
			Channel:    pane.Channel + 1,
		},
	}
	model.currentTab().Panes[follower.ID] = follower
	ensureTerminalResizeOwner(model.workspace.Tabs, pane.TerminalID, follower.ID)
	pane.VTerm.Resize(20, 6)
	_, _ = pane.VTerm.Write([]byte("0123456789ABCDEFGHIJ"))
	pane.live = true
	pane.MarkRenderDirty()

	_ = activatePrefixForTest(model)
	_, resizeCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	if resizeCmd != nil {
		if next := mustRunCmd(t, resizeCmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	view := xansi.Strip(model.View())
	if !strings.Contains(view, "BCDEFGHI") {
		t.Fatalf("expected fixed viewport to crop around cursor, got:\n%s", view)
	}
	if strings.Contains(view, "0123456789ABCDEFGHIJ") {
		t.Fatalf("expected fixed viewport to crop instead of showing the full line, got:\n%s", view)
	}
	if connection := paneConnectionStatus(model.workspace.Tabs, pane); connection != "follower" {
		t.Fatalf("expected fixed viewport under shared terminal to remain follower, got %q", connection)
	}
}

func TestPinnedFixedViewportAllowsManualOffsetPan(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 16
	model.height = 10

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	pane.VTerm.Resize(20, 6)
	_, _ = pane.VTerm.Write([]byte("0123456789ABCDEFGHIJ"))
	pane.live = true
	pane.MarkRenderDirty()

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})

	pane.Offset = Point{X: 0, Y: 0}
	pane.MarkRenderDirty()
	before := xansi.Strip(model.View())

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	after := xansi.Strip(model.View())

	if before == after {
		t.Fatal("expected manual pan to change rendered content")
	}
	if pane.Offset.X != 4 {
		t.Fatalf("expected offset X to move by 4, got %d", pane.Offset.X)
	}
}

func TestPinnedFixedViewportPanClampsToContentBounds(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 20
	model.height = 10

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	pane.VTerm.Resize(20, 6)
	_, _ = pane.VTerm.Write([]byte("0123456789ABCDEFGHIJ"))
	pane.live = true
	pane.MarkRenderDirty()

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})

	for i := 0; i < 8; i++ {
		_ = activatePrefixForTest(model)
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	}
	if pane.Offset.X != 2 {
		t.Fatalf("expected horizontal offset clamp at 2, got %d", pane.Offset.X)
	}

	for i := 0; i < 4; i++ {
		_ = activatePrefixForTest(model)
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	}
	if pane.Offset.Y != 0 {
		t.Fatalf("expected vertical offset clamp at 0, got %d", pane.Offset.Y)
	}
}

func TestUnpinnedFixedViewportResumesCursorFollow(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 20
	model.height = 10

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	pane.VTerm.Resize(20, 6)
	_, _ = pane.VTerm.Write([]byte("0123456789ABCDEFGHIJ"))
	pane.live = true
	pane.MarkRenderDirty()

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	pane.Offset = Point{X: 0, Y: 0}

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})

	if pane.Pin {
		t.Fatal("expected pin to be disabled")
	}
	if pane.Offset.X != 2 {
		t.Fatalf("expected offset to snap back to cursor-followed view, got %d", pane.Offset.X)
	}
}

func TestViewportModeToggleBackToFitClearsFixedStateAndResizes(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 20
	model.height = 10

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	pane.Offset = Point{X: 3, Y: 2}

	_ = activatePrefixForTest(model)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	if pane.Mode != ViewportModeFit {
		t.Fatalf("expected fit mode, got %q", pane.Mode)
	}
	if pane.Pin {
		t.Fatal("expected pin cleared when returning to fit mode")
	}
	if pane.Offset != (Point{}) {
		t.Fatalf("expected offset reset, got %+v", pane.Offset)
	}
	if client.resizeCalls == 0 {
		t.Fatal("expected fit mode toggle to resize the PTY")
	}
}

func TestConsumePrefixInputHandlesCtrlPanKeys(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 16
	model.height = 10

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	pane.VTerm.Resize(20, 6)
	_, _ = pane.VTerm.Write([]byte("0123456789ABCDEFGHIJ"))
	pane.live = true
	pane.MarkRenderDirty()
	pane.Mode = ViewportModeFixed
	pane.Pin = true
	pane.Offset = Point{X: 0, Y: 0}

	model.prefixActive = true
	model.rawPending = []byte{0x0c}
	consumed, cmd, ok := model.consumePrefixInput()
	if !ok || consumed != 1 || cmd == nil {
		t.Fatalf("expected ctrl+l prefix input to be consumed and rearm prefix, got consumed=%d ok=%v cmd=%v", consumed, ok, cmd != nil)
	}
	if pane.Offset.X != 4 {
		t.Fatalf("expected ctrl+l pan to move offset to 4, got %d", pane.Offset.X)
	}

	model.prefixActive = true
	model.rawPending = []byte("\x1b[1;5D")
	consumed, cmd, ok = model.consumePrefixInput()
	if !ok || consumed != len("\x1b[1;5D") || cmd == nil {
		t.Fatalf("expected ctrl+left prefix sequence to be consumed and rearm prefix, got consumed=%d ok=%v cmd=%v", consumed, ok, cmd != nil)
	}
	if pane.Offset.X != 0 {
		t.Fatalf("expected ctrl+left pan to clamp back to 0, got %d", pane.Offset.X)
	}
}

func TestConsumePrefixInputInvalidByteClearsStickyMode(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.prefixActive = true
	model.prefixMode = prefixModeFloating
	model.rawPending = []byte("q")

	consumed, cmd, ok := model.consumePrefixInput()
	if !ok || consumed != 1 {
		t.Fatalf("expected invalid sticky prefix byte to be consumed, consumed=%d ok=%v", consumed, ok)
	}
	if cmd != nil {
		t.Fatalf("expected invalid sticky prefix byte to be ignored synchronously, got cmd=%v", cmd != nil)
	}
	if model.prefixActive {
		t.Fatal("expected invalid sticky prefix byte to clear prefix mode")
	}
}

func TestFollowerFixedViewportDoesNotSendResizeToTerminal(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 80
	model.height = 24

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	follower := &Pane{
		ID: "pane-follower",
		Viewport: &Viewport{
			TerminalID: pane.TerminalID,
			Channel:    pane.Channel + 1,
		},
	}
	model.currentTab().Panes[follower.ID] = follower
	ensureTerminalResizeOwner(model.workspace.Tabs, pane.TerminalID, follower.ID)
	resizesBefore := client.resizeCalls

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	_, cmd = model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if cmd != nil {
		_ = mustRunCmd(t, cmd)
	}

	if got := client.resizeCalls; got != resizesBefore {
		t.Fatalf("expected fixed viewport resize to avoid PTY resize, got %d -> %d", resizesBefore, got)
	}
	if pane.Mode != ViewportModeFixed {
		t.Fatalf("expected viewport mode fixed, got %q", pane.Mode)
	}
	if connection := paneConnectionStatus(model.workspace.Tabs, pane); connection != "follower" {
		t.Fatalf("expected fixed pane to remain follower without ownership, got %q", connection)
	}
}

func TestCatchingUpSkipsAlternateRenderTicks(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.renderBatching = true
	model.program = &tea.Program{}

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	pane.SetCatchingUp(true)
	pane.MarkRenderDirty()
	model.renderCache = "cached"
	model.renderDirty = false
	model.renderPending.Store(true)

	_, _ = model.Update(renderTickMsg{})
	if model.renderDirty {
		t.Fatal("expected first catching-up tick to skip rendering")
	}

	model.renderPending.Store(true)
	_, _ = model.Update(renderTickMsg{})
	if !model.renderDirty {
		t.Fatal("expected second catching-up tick to allow rendering")
	}
}

func TestComposedCanvasMarksOnlyChangedRowsDirty(t *testing.T) {
	canvas := newComposedCanvas(4, 2)
	initial := canvas.String()
	if initial == "" {
		t.Fatal("expected initial canvas output")
	}
	if canvas.rowDirty[0] || canvas.rowDirty[1] {
		t.Fatal("expected initial String call to clear dirty rows")
	}

	canvas.set(0, 0, blankDrawCell())
	if canvas.rowDirty[0] || canvas.rowDirty[1] {
		t.Fatal("expected writing the same cell to keep rows clean")
	}

	canvas.set(1, 1, drawCell{Content: "x", Width: 1})
	if canvas.rowDirty[0] {
		t.Fatal("expected untouched first row to stay clean")
	}
	if !canvas.rowDirty[1] {
		t.Fatal("expected changed second row to become dirty")
	}

	updated := canvas.String()
	if updated == initial {
		t.Fatal("expected canvas output to change after writing a new cell")
	}
	if canvas.rowDirty[0] || canvas.rowDirty[1] {
		t.Fatal("expected String call to clear dirty rows again")
	}
}

func TestComposedCanvasCachesJoinedStringUntilCanvasChanges(t *testing.T) {
	canvas := newComposedCanvas(3, 1)
	first := canvas.String()
	if canvas.fullDirty {
		t.Fatal("expected first String call to clear full cache dirty flag")
	}

	second := canvas.String()
	if first != second {
		t.Fatal("expected stable String result when canvas stays clean")
	}
	if canvas.fullDirty {
		t.Fatal("expected clean String call to keep full cache clean")
	}

	canvas.set(1, 0, drawCell{Content: "x", Width: 1})
	if !canvas.fullDirty {
		t.Fatal("expected cell mutation to invalidate full canvas cache")
	}

	third := canvas.String()
	if third == first {
		t.Fatal("expected dirty canvas to rebuild joined string")
	}
	if canvas.fullDirty {
		t.Fatal("expected rebuilt String call to clear full cache dirty flag")
	}
}

func TestComposedCanvasDrawPaneBodyKeepsBorderRowsClean(t *testing.T) {
	pane := &Pane{
		ID:    "pane-001",
		Title: "demo",
		Viewport: &Viewport{
			VTerm: localvterm.New(16, 4, 100, nil),
			live:  true,
		},
	}
	if _, err := pane.VTerm.Write([]byte("before")); err != nil {
		t.Fatalf("write initial content: %v", err)
	}

	canvas := newComposedCanvas(14, 6)
	rect := Rect{X: 0, Y: 0, W: 14, H: 6}
	canvas.drawPane(rect, pane, true)
	_ = canvas.String()

	if _, err := pane.VTerm.Write([]byte("\rafter")); err != nil {
		t.Fatalf("write updated content: %v", err)
	}
	pane.MarkRenderDirty()

	canvas.drawPaneBody(rect, pane, true)

	if canvas.rowDirty[0] {
		t.Fatal("expected top border row to stay clean on content-only redraw")
	}
	if !canvas.rowDirty[1] {
		t.Fatal("expected content row to become dirty on content-only redraw")
	}
}

func TestDirtyRowsForOutputTracksSimpleSingleLineWrite(t *testing.T) {
	start, end, ok := dirtyRowsForOutput(
		[]byte("hello"),
		localvterm.CursorState{Row: 2, Col: 0},
		localvterm.CursorState{Row: 2, Col: 5},
		false,
		false,
		24,
	)
	if !ok {
		t.Fatal("expected simple write to produce dirty row range")
	}
	if start != 2 || end != 2 {
		t.Fatalf("expected row 2 only, got %d..%d", start, end)
	}
}

func TestDirtyRegionForOutputTracksSingleLineColumnSpan(t *testing.T) {
	rowStart, rowEnd, colStart, colEnd, ok := dirtyRegionForOutput(
		[]byte("hello"),
		localvterm.CursorState{Row: 2, Col: 3},
		localvterm.CursorState{Row: 2, Col: 8},
		false,
		false,
		80,
		24,
	)
	if !ok {
		t.Fatal("expected simple write to produce dirty region")
	}
	if rowStart != 2 || rowEnd != 2 {
		t.Fatalf("expected row 2 only, got %d..%d", rowStart, rowEnd)
	}
	if colStart != 3 || colEnd != 8 {
		t.Fatalf("expected cols 3..7, got %d..%d", colStart, colEnd)
	}
}

func TestDirtyRowsForOutputFallsBackToFullRedrawOnEscapeSequence(t *testing.T) {
	if _, _, ok := dirtyRowsForOutput(
		[]byte("\x1b[2J"),
		localvterm.CursorState{Row: 0, Col: 0},
		localvterm.CursorState{Row: 0, Col: 0},
		false,
		false,
		24,
	); ok {
		t.Fatal("expected escape sequence to disable incremental dirty rows")
	}
}

func TestDirtyRegionForOutputDropsColumnSpanAcrossLines(t *testing.T) {
	rowStart, rowEnd, _, _, ok := dirtyRegionForOutput(
		[]byte("hello\n"),
		localvterm.CursorState{Row: 2, Col: 3},
		localvterm.CursorState{Row: 3, Col: 0},
		false,
		false,
		80,
		24,
	)
	if !ok {
		t.Fatal("expected newline write to produce dirty rows")
	}
	if rowStart != 2 || rowEnd != 3 {
		t.Fatalf("expected rows 2..3, got %d..%d", rowStart, rowEnd)
	}
}

func TestComposedCanvasDrawPaneBodyDirtyRowsKeepsOtherContentRowsClean(t *testing.T) {
	pane := &Pane{
		ID:    "pane-001",
		Title: "demo",
		Viewport: &Viewport{
			VTerm:          localvterm.New(16, 4, 100, nil),
			live:           true,
			renderDirty:    true,
			dirtyRowsKnown: true,
			dirtyRowStart:  1,
			dirtyRowEnd:    1,
		},
	}
	if _, err := pane.VTerm.Write([]byte("row0\nrow1\nrow2")); err != nil {
		t.Fatalf("write initial content: %v", err)
	}
	pane.SetDirtyRows(1, 1, true)

	canvas := newComposedCanvas(14, 6)
	rect := Rect{X: 0, Y: 0, W: 14, H: 6}
	canvas.drawPane(rect, pane, false)
	_ = canvas.String()
	for i := range canvas.rowDirty {
		canvas.rowDirty[i] = false
	}
	canvas.fullDirty = false

	pane.MarkRenderDirty()
	pane.SetDirtyRows(1, 1, true)
	canvas.drawPaneBody(rect, pane, false)

	if canvas.rowDirty[0] {
		t.Fatal("expected top border row to stay clean on dirty-row redraw")
	}
	if canvas.rowDirty[1] {
		t.Fatal("expected unaffected content row to stay clean")
	}
	if !canvas.rowDirty[2] {
		t.Fatal("expected only targeted dirty content row to redraw")
	}
	if canvas.rowDirty[3] {
		t.Fatal("expected lower untouched content row to stay clean")
	}
	if pane.IsRenderDirty() {
		t.Fatal("expected dirty row state to clear after redraw")
	}
}

func TestComposedCanvasDrawPaneBodyDirtyRegionKeepsOtherColumnsClean(t *testing.T) {
	makeRow := func(text string) []protocol.Cell {
		row := make([]protocol.Cell, 0, len(text))
		for _, r := range text {
			row = append(row, protocol.Cell{Content: string(r), Width: 1})
		}
		return row
	}
	pane := &Pane{
		ID:    "pane-001",
		Title: "demo",
		Viewport: &Viewport{
			Snapshot: &protocol.Snapshot{
				Size: protocol.Size{Cols: 16, Rows: 4},
				Screen: protocol.ScreenData{
					Cells: [][]protocol.Cell{
						makeRow("abcdefghijklmnop"),
					},
				},
			},
			renderDirty:    true,
			dirtyRowsKnown: true,
			dirtyRowStart:  0,
			dirtyRowEnd:    0,
			dirtyColsKnown: true,
			dirtyColStart:  3,
			dirtyColEnd:    5,
		},
	}
	pane.SetDirtyRows(0, 0, true)
	pane.SetDirtyCols(3, 5, true)

	canvas := newComposedCanvas(18, 5)
	rect := Rect{X: 0, Y: 0, W: 18, H: 5}
	canvas.drawPaneFrame(rect, pane, false)
	canvas.drawText(Rect{X: 1, Y: 1, W: 16, H: 1}, []string{"abcdefghijklmnop"}, drawStyle{})

	pane.Snapshot.Screen.Cells[0] = makeRow("abcXYZghijklmnop")
	pane.MarkRenderDirty()
	pane.SetDirtyRows(0, 0, true)
	pane.SetDirtyCols(3, 5, true)
	canvas.drawPaneBody(rect, pane, false)

	body := xansi.Strip(rowToANSI(canvas.cells[1]))
	if !strings.Contains(body, "abcXYZgh") {
		t.Fatalf("expected updated mid-row segment, got %q", body)
	}

}

func TestComposedCanvasDrawPaneBodyDirtyRegionSupportsActivePaneCursor(t *testing.T) {
	makeRow := func(text string) []protocol.Cell {
		row := make([]protocol.Cell, 0, len(text))
		for _, r := range text {
			row = append(row, protocol.Cell{Content: string(r), Width: 1})
		}
		return row
	}
	pane := &Pane{
		ID:    "pane-001",
		Title: "demo",
		Viewport: &Viewport{
			Snapshot: &protocol.Snapshot{
				Size: protocol.Size{Cols: 16, Rows: 4},
				Screen: protocol.ScreenData{
					Cells: [][]protocol.Cell{
						makeRow("abcdefghijklmnop"),
					},
				},
				Cursor: protocol.CursorState{Row: 0, Col: 8, Visible: true, Shape: "block"},
			},
			renderDirty:    true,
			dirtyRowsKnown: true,
			dirtyRowStart:  0,
			dirtyRowEnd:    0,
			dirtyColsKnown: true,
			dirtyColStart:  3,
			dirtyColEnd:    8,
		},
	}
	pane.SetDirtyRows(0, 0, true)
	pane.SetDirtyCols(3, 8, true)

	canvas := newComposedCanvas(18, 5)
	rect := Rect{X: 0, Y: 0, W: 18, H: 5}
	canvas.drawPaneFrame(rect, pane, true)
	canvas.drawText(Rect{X: 1, Y: 1, W: 16, H: 1}, []string{"abcdefghijklmnop"}, drawStyle{})

	pane.Snapshot.Screen.Cells[0] = makeRow("abcXYZghijklmnop")
	pane.MarkRenderDirty()
	pane.SetDirtyRows(0, 0, true)
	pane.SetDirtyCols(3, 8, true)
	pane.Snapshot.Cursor.Col = 8
	canvas.drawPaneBody(rect, pane, true)

	body := rowToANSI(canvas.cells[1])
	stripped := xansi.Strip(body)
	if !strings.Contains(stripped, "abcXYZgh") {
		t.Fatalf("expected updated body under active cursor, got %q", stripped)
	}
	cursorCell := canvas.cells[1][9]
	if !cursorCell.Style.Reverse {
		t.Fatal("expected active cursor cell to be reverse-styled")
	}
}

func TestBorderTitleCellsReuseCachedSliceForSameInputs(t *testing.T) {
	style := drawStyle{FG: "#ecfccb", BG: "#111827", Bold: true}
	first := borderTitleCells("demo", 10, style)
	second := borderTitleCells("demo", 10, style)
	if len(first) == 0 || len(second) == 0 {
		t.Fatal("expected cached title cells")
	}
	if &first[0] != &second[0] {
		t.Fatal("expected title cell cache reuse for same inputs")
	}
}

func TestCachedBlankFillRowReuseCachedSliceForSameInputs(t *testing.T) {
	first := cachedBlankFillRow(12)
	second := cachedBlankFillRow(12)
	if len(first) != 12 || len(second) != 12 {
		t.Fatal("expected cached fill rows")
	}
	if &first[0] != &second[0] {
		t.Fatal("expected fill row cache reuse for same inputs")
	}
}

func TestComposedCanvasDrawPaneCursorOnlyKeepsNonCursorRowsClean(t *testing.T) {
	pane := &Pane{
		ID:    "pane-001",
		Title: "demo",
		Viewport: &Viewport{
			VTerm: localvterm.New(16, 4, 100, nil),
			live:  true,
		},
	}
	if _, err := pane.VTerm.Write([]byte("cursor")); err != nil {
		t.Fatalf("write initial content: %v", err)
	}

	canvas := newComposedCanvas(14, 6)
	rect := Rect{X: 0, Y: 0, W: 14, H: 6}
	canvas.drawPane(rect, pane, true)
	_ = canvas.String()

	canvas.drawPaneCursorOnly(rect, pane, false)

	if canvas.rowDirty[0] || canvas.rowDirty[2] || canvas.rowDirty[3] || canvas.rowDirty[4] || canvas.rowDirty[5] {
		t.Fatal("expected only cursor row to change during cursor-only redraw")
	}
	if !canvas.rowDirty[1] {
		t.Fatal("expected cursor row to become dirty during cursor-only redraw")
	}
}

func TestTextLinesToGridHandlesUnicodeGraphemes(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{name: "combining", text: "e\u0301"},
		{name: "emoji", text: "🙂"},
		{name: "emoji-zwj", text: "👩‍💻"},
		{name: "cjk", text: "你好"},
		{name: "hangul", text: "한글"},
		{name: "fullwidth-punct", text: "！"},
		{name: "mixed", text: "Ae\u0301🙂界"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			row := textLinesToGrid([]string{tc.text})[0]
			if got, want := rowToANSI(row), tc.text; got != want {
				t.Fatalf("expected row text %q, got %q", want, got)
			}
			if got, want := len(row), xansi.StringWidth(tc.text); got != want {
				t.Fatalf("expected row width %d, got %d", want, got)
			}
		})
	}
}

func TestTrimToWidthIsGraphemeAware(t *testing.T) {
	if got := trimToWidth("A👩‍💻B", 3); got != "A👩‍💻" {
		t.Fatalf("expected emoji cluster to stay intact, got %q", got)
	}
	if got := trimToWidth("e\u0301x", 1); got != "e\u0301" {
		t.Fatalf("expected combining cluster to stay intact, got %q", got)
	}
}

func TestCropDrawGridClipsWideCellsAtViewportEdge(t *testing.T) {
	row := stringToDrawCells("A好B", drawStyle{})
	grid := [][]drawCell{row}

	cropped := cropDrawGrid(grid, Point{X: 1, Y: 0}, 2, 1)
	if len(cropped) != 1 || len(cropped[0]) != 2 {
		t.Fatalf("expected 1x2 cropped grid, got %dx%d", len(cropped), len(cropped[0]))
	}
	if got := rowToANSI(cropped[0]); got != "好" {
		t.Fatalf("expected cropped row to keep only the full wide cell, got %q", got)
	}

	clipped := cropDrawGrid(grid, Point{X: 2, Y: 0}, 1, 1)
	if got := rowToANSI(clipped[0]); got != " " {
		t.Fatalf("expected clipped continuation cell to render blank, got %q", got)
	}
}

func TestPaneCellsForViewportCachesFixedCropForStableViewport(t *testing.T) {
	pane := &Pane{
		ID: "pane-001",
		Viewport: &Viewport{
			Mode:        ViewportModeFixed,
			Offset:      Point{X: 1, Y: 0},
			cellVersion: 1,
		},
	}

	pane.SetCellCache([][]drawCell{stringToDrawCells("012345", drawStyle{}), stringToDrawCells("abcdef", drawStyle{})})
	first := paneCellsForViewport(pane, 3, 2)
	second := paneCellsForViewport(pane, 3, 2)
	if len(first) == 0 || len(first[0]) == 0 {
		t.Fatal("expected cropped viewport cells")
	}
	if &first[0][0] != &second[0][0] {
		t.Fatal("expected stable fixed viewport crop to reuse cached grid")
	}
	if got := rowToANSI(first[0]); got != "123" {
		t.Fatalf("expected first cropped row 123, got %q", got)
	}

	pane.Offset = Point{X: 2, Y: 0}
	third := paneCellsForViewport(pane, 3, 2)
	if &first[0][0] == &third[0][0] {
		t.Fatal("expected offset change to invalidate cached viewport crop")
	}
	if got := rowToANSI(third[0]); got != "234" {
		t.Fatalf("expected offset change to recrop row, got %q", got)
	}

	fourth := paneCellsForViewport(pane, 3, 2)
	if &third[0][0] != &fourth[0][0] {
		t.Fatal("expected viewport cache reuse after offset stabilizes again")
	}
}

func TestPaneCellsForViewportInvalidatesFixedCropWhenBaseGridChanges(t *testing.T) {
	pane := &Pane{
		ID: "pane-001",
		Viewport: &Viewport{
			Mode:        ViewportModeFixed,
			Offset:      Point{X: 1, Y: 0},
			cellVersion: 1,
		},
	}

	pane.SetCellCache([][]drawCell{stringToDrawCells("012345", drawStyle{})})
	first := paneCellsForViewport(pane, 3, 1)
	pane.SetCellCache([][]drawCell{stringToDrawCells("xyz789", drawStyle{})})
	pane.cellVersion++

	second := paneCellsForViewport(pane, 3, 1)
	if &first[0][0] == &second[0][0] {
		t.Fatal("expected cell cache version change to invalidate viewport crop cache")
	}
	if got := rowToANSI(second[0]); got != "yz7" {
		t.Fatalf("expected recropped row after base grid change, got %q", got)
	}
}

func TestPaneCellsForViewportFixedLivePathAvoidsMaterializingFullGrid(t *testing.T) {
	pane := &Pane{
		ID: "pane-001",
		Viewport: &Viewport{
			Mode:   ViewportModeFixed,
			Offset: Point{X: 2, Y: 0},
			VTerm:  localvterm.New(12, 4, 100, nil),
			live:   true,

			cellVersion: 7,
		},
	}
	if _, err := pane.VTerm.Write([]byte("0123456789AB")); err != nil {
		t.Fatalf("write vterm: %v", err)
	}

	first := paneCellsForViewport(pane, 4, 1)
	if got := rowToANSI(first[0]); got != "2345" {
		t.Fatalf("expected cropped live viewport row 2345, got %q", got)
	}
	if pane.CellCache() != nil {
		t.Fatal("expected fixed live viewport render to avoid populating full grid cache")
	}
	if pane.IsRenderDirty() {
		t.Fatal("expected viewport render to clear dirty flag")
	}

	second := paneCellsForViewport(pane, 4, 1)
	if &first[0][0] != &second[0][0] {
		t.Fatal("expected live viewport cache reuse after first render")
	}
}

func TestViewLiveFitPaneAvoidsMaterializingFullGrid(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	if _, err := pane.VTerm.Write([]byte("fit-direct-path")); err != nil {
		t.Fatalf("write vterm: %v", err)
	}
	pane.live = true
	pane.MarkRenderDirty()

	view := xansi.Strip(model.View())
	if !strings.Contains(view, "fit-direct-path") {
		t.Fatalf("expected rendered output, got:\n%s", view)
	}
	if pane.CellCache() != nil {
		t.Fatal("expected live fit view render to avoid full grid cache")
	}
	if pane.IsRenderDirty() {
		t.Fatal("expected live fit render to clear dirty flag")
	}
}

func TestViewSnapshotFitPaneAvoidsMaterializingFullGrid(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 90
	model.height = 20

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil {
		t.Fatal("expected active pane")
	}
	pane.live = false
	pane.Snapshot = &protocol.Snapshot{
		TerminalID: pane.TerminalID,
		Size:       protocol.Size{Cols: 20, Rows: 4},
		Screen: protocol.ScreenData{
			Cells: [][]protocol.Cell{stringToProtocolRow("snapshot-direct")},
		},
		Cursor: protocol.CursorState{Visible: true},
	}
	pane.MarkRenderDirty()
	pane.ClearCellCache()

	view := xansi.Strip(model.View())
	if !strings.Contains(view, "snapshot-direct") {
		t.Fatalf("expected rendered snapshot output, got:\n%s", view)
	}
	if pane.CellCache() != nil {
		t.Fatal("expected snapshot fit view render to avoid full grid cache")
	}
	if pane.IsRenderDirty() {
		t.Fatal("expected snapshot fit render to clear dirty flag")
	}
}

func mustRunCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	return cmd()
}

func runAllCmds(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	switch batch := msg.(type) {
	case tea.BatchMsg:
		for _, sub := range batch {
			runAllCmds(t, sub)
		}
	default:
	}
}

func lineIndexContaining(view, needle string) int {
	for _, line := range strings.Split(xansi.Strip(view), "\n") {
		if idx := strings.Index(line, needle); idx >= 0 {
			return idx
		}
	}
	return -1
}

func lineContaining(view, needle string) string {
	for _, line := range strings.Split(xansi.Strip(view), "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

func createFloatingPaneViaPicker(t *testing.T, model *Model) {
	t.Helper()

	_ = activatePrefixForTest(model)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})

	var msg tea.Msg
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cmd != nil {
		msg = mustRunCmd(t, cmd)
		_, _ = model.Update(msg)
	}

	if model.terminalPicker == nil {
		t.Fatal("expected floating command to open chooser")
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		cmd = commitDefaultTerminalCreatePrompt(t, model)
	}
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}
	model.clearPrefixState()
}

func createSplitPaneViaPicker(t *testing.T, model *Model, dir SplitDirection) {
	t.Helper()

	key := '%'
	if dir == SplitHorizontal {
		key = '"'
	}

	_ = activatePrefixForTest(model)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
	msg := mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if model.terminalPicker == nil {
		t.Fatal("expected split command to open chooser")
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		cmd = commitDefaultTerminalCreatePrompt(t, model)
	}
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}
}

func createNewTabViaPicker(t *testing.T, model *Model) {
	t.Helper()

	_ = activatePrefixForTest(model)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	msg := mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if model.terminalPicker == nil {
		t.Fatal("expected new tab command to open chooser")
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		cmd = commitDefaultTerminalCreatePrompt(t, model)
	}
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}
}

func commitDefaultTerminalCreatePrompt(t testing.TB, model *Model) tea.Cmd {
	t.Helper()
	if model.prompt == nil || model.prompt.Kind != "create-terminal-name" {
		t.Fatalf("expected create-terminal-name prompt, got %#v", model.prompt)
	}
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("expected name prompt enter to advance to tags, got %#v", cmd)
	}
	if model.prompt == nil || model.prompt.Kind != "create-terminal-tags" {
		t.Fatalf("expected create-terminal-tags prompt, got %#v", model.prompt)
	}
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected tags prompt enter to start creation")
	}
	return cmd
}

type fakeClient struct {
	next           int
	createCalls    int
	nextChannel    uint16
	inputs         [][]byte
	resizeCalls    int
	resizeCols     uint16
	resizeRows     uint16
	resizeChannel  uint16
	kills          int
	killedIDs      []string
	attachedIDs    []string
	listResult     []protocol.TerminalInfo
	snapshotByID   map[string]*protocol.Snapshot
	snapshotCalls  int
	snapshotErr    error
	terminalByID   map[string]protocol.TerminalInfo
	terminalOrder  []string
	createDelay    time.Duration
	listDelay      time.Duration
	attachDelay    time.Duration
	snapshotDelay  time.Duration
	inputDelay     time.Duration
	resizeDelay    time.Duration
	killDelay      time.Duration
	metadataCalls  int
	events         chan protocol.Event
	eventsCalls    int
	streamCalls    int
	streamChannels []uint16
}

func (f *fakeClient) Close() error { return nil }

func (f *fakeClient) Create(ctx context.Context, command []string, name string, size protocol.Size) (*protocol.CreateResult, error) {
	if err := f.wait(ctx, f.createDelay); err != nil {
		return nil, err
	}
	f.createCalls++
	f.next++
	id := terminalID(f.next)
	info := protocol.TerminalInfo{ID: id, Name: name, Command: append([]string(nil), command...), Size: size, State: "running"}
	f.storeTerminal(info)
	return &protocol.CreateResult{TerminalID: id, State: "running"}, nil
}

func (f *fakeClient) SetTags(ctx context.Context, terminalID string, tags map[string]string) error {
	if err := f.wait(ctx, 0); err != nil {
		return err
	}
	info, ok := f.terminalByID[terminalID]
	if !ok {
		return fmt.Errorf("terminal %q not found", terminalID)
	}
	info.Tags = cloneStringMap(tags)
	f.terminalByID[terminalID] = info
	return nil
}

func (f *fakeClient) SetMetadata(ctx context.Context, terminalID string, name string, tags map[string]string) error {
	if err := f.wait(ctx, 0); err != nil {
		return err
	}
	info, ok := f.terminalByID[terminalID]
	if !ok {
		return fmt.Errorf("terminal %q not found", terminalID)
	}
	f.metadataCalls++
	info.Name = strings.TrimSpace(name)
	info.Tags = cloneStringMap(tags)
	f.terminalByID[terminalID] = info
	return nil
}

func (f *fakeClient) Attach(ctx context.Context, terminalID string, mode string) (*protocol.AttachResult, error) {
	if err := f.wait(ctx, f.attachDelay); err != nil {
		return nil, err
	}
	f.nextChannel++
	f.attachedIDs = append(f.attachedIDs, terminalID)
	return &protocol.AttachResult{Mode: mode, Channel: f.nextChannel}, nil
}

func (f *fakeClient) Snapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error) {
	if err := f.wait(ctx, f.snapshotDelay); err != nil {
		return nil, err
	}
	f.snapshotCalls++
	if f.snapshotErr != nil {
		return nil, f.snapshotErr
	}
	if f.snapshotByID != nil {
		if snap, ok := f.snapshotByID[terminalID]; ok {
			cp := *snap
			return &cp, nil
		}
	}
	return &protocol.Snapshot{
		TerminalID: terminalID,
		Size:       protocol.Size{Cols: 80, Rows: 24},
	}, nil
}

func (f *fakeClient) List(ctx context.Context) (*protocol.ListResult, error) {
	if err := f.wait(ctx, f.listDelay); err != nil {
		return nil, err
	}
	items := make([]protocol.TerminalInfo, 0, len(f.terminalOrder))
	for _, info := range f.listResult {
		f.storeTerminal(info)
	}
	for _, id := range f.terminalOrder {
		info, ok := f.terminalByID[id]
		if ok {
			items = append(items, info)
		}
	}
	return &protocol.ListResult{Terminals: items}, nil
}

func (f *fakeClient) Events(ctx context.Context, params protocol.EventsParams) (<-chan protocol.Event, error) {
	f.eventsCalls++
	if f.events == nil {
		ch := make(chan protocol.Event)
		close(ch)
		return ch, nil
	}
	return f.events, nil
}

func (f *fakeClient) Input(ctx context.Context, channel uint16, data []byte) error {
	if err := f.wait(ctx, f.inputDelay); err != nil {
		return err
	}
	f.inputs = append(f.inputs, append([]byte(nil), data...))
	return nil
}
func (f *fakeClient) Resize(ctx context.Context, channel uint16, cols, rows uint16) error {
	if err := f.wait(ctx, f.resizeDelay); err != nil {
		return err
	}
	f.resizeCalls++
	f.resizeChannel = channel
	f.resizeCols = cols
	f.resizeRows = rows
	return nil
}
func (f *fakeClient) Stream(channel uint16) (<-chan protocol.StreamFrame, func()) {
	f.streamCalls++
	f.streamChannels = append(f.streamChannels, channel)
	ch := make(chan protocol.StreamFrame)
	close(ch)
	return ch, func() {}
}
func (f *fakeClient) Kill(ctx context.Context, terminalID string) error {
	if err := f.wait(ctx, f.killDelay); err != nil {
		return err
	}
	f.kills++
	f.killedIDs = append(f.killedIDs, terminalID)
	delete(f.terminalByID, terminalID)
	for i, id := range f.terminalOrder {
		if id == terminalID {
			f.terminalOrder = append(f.terminalOrder[:i], f.terminalOrder[i+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeClient) storeTerminal(info protocol.TerminalInfo) {
	if f.terminalByID == nil {
		f.terminalByID = make(map[string]protocol.TerminalInfo)
	}
	if _, ok := f.terminalByID[info.ID]; !ok {
		f.terminalOrder = append(f.terminalOrder, info.ID)
	}
	if info.Size.Cols == 0 {
		info.Size.Cols = 80
	}
	if info.Size.Rows == 0 {
		info.Size.Rows = 24
	}
	if info.Command == nil {
		info.Command = []string{}
	}
	f.terminalByID[info.ID] = info
}

func (f *fakeClient) wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func terminalID(i int) string {
	return fmt.Sprintf("pane-%03d", i)
}

func containsAll(s string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(s, part) {
			return false
		}
	}
	return true
}

func containsAny(s string, parts ...string) bool {
	for _, part := range parts {
		if strings.Contains(s, part) {
			return true
		}
	}
	return false
}

func firstCellPtr(grid [][]drawCell) *drawCell {
	for _, row := range grid {
		if len(row) > 0 {
			return &row[0]
		}
	}
	return nil
}

func stringToProtocolRow(s string) []protocol.Cell {
	cells := stringToDrawCells(s, drawStyle{})
	row := make([]protocol.Cell, 0, len(cells))
	for _, cell := range cells {
		if cell.Continuation {
			row = append(row, protocol.Cell{})
			continue
		}
		row = append(row, protocol.Cell{
			Content: cell.Content,
			Width:   cell.Width,
		})
	}
	return row
}

func vtermRowString(row []localvterm.Cell) string {
	var b strings.Builder
	for _, cell := range row {
		if cell.Content == "" {
			b.WriteByte(' ')
			continue
		}
		b.WriteString(cell.Content)
	}
	return strings.TrimRight(b.String(), " ")
}
