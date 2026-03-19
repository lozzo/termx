package tui

import (
	"context"
	"errors"
	"fmt"
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
	model.height = 28

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	view := model.View()
	if !containsAll(view, "termx", "Ctrl-a", "split", "new tab") {
		t.Fatalf("welcome view missing expected hints:\n%s", view)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	helpView := model.View()
	if !containsAll(helpView, "Help", "Ctrl-a %", "Ctrl-a c", "Ctrl-a d") {
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

	tab := model.currentTab()
	if len(tab.Panes) != 1 {
		t.Fatalf("expected startup create selection to create first pane, got %d panes", len(tab.Panes))
	}
	if client.createCalls != 1 {
		t.Fatalf("expected startup create selection to create terminal, got %d calls", client.createCalls)
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'%'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if model.terminalPicker == nil {
		t.Fatal("expected split command to open chooser")
	}
	if model.terminalPicker.Title != "Open Viewport" {
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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
	basePane.renderDirty = true
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
	floatPane.renderDirty = true
	_, _ = floatPane.VTerm.Write([]byte("FLOAT-ONLY-XYZ"))

	visibleView := xansi.Strip(model.View())
	if !strings.Contains(visibleView, "FLOAT-ONLY-XYZ") {
		t.Fatalf("expected floating content in visible render, got:\n%s", visibleView)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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
	firstFloat.renderDirty = true
	_, _ = secondFloat.VTerm.Write([]byte("TOP-LAYER"))
	secondFloat.live = true
	secondFloat.renderDirty = true

	view := xansi.Strip(model.View())
	if !strings.Contains(view, "TOP-LAYER") || strings.Contains(view, "BOTTOM-LAYER") {
		t.Fatalf("expected top floating viewport to occlude bottom one, got:\n%s", view)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'_'}})
	if cmd != nil {
		t.Fatalf("expected z-order change to be synchronous")
	}

	firstFloat.renderDirty = true
	secondFloat.renderDirty = true
	view = xansi.Strip(model.View())
	if !strings.Contains(view, "BOTTOM-LAYER") || strings.Contains(view, "TOP-LAYER") {
		t.Fatalf("expected lowered floating viewport to move behind bottom one, got:\n%s", view)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	firstFloat.renderDirty = true
	secondFloat.renderDirty = true
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
	if cmd != nil {
		t.Fatalf("expected alt move to be synchronous")
	}
	if floating.Rect.X != 6 {
		t.Fatalf("expected alt-h to move floating pane left by 4, got %+v", floating.Rect)
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
	if floating.Rect.X != 0 {
		t.Fatalf("expected floating pane to clamp at left edge, got %+v", floating.Rect)
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
	basePane.renderDirty = true
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
	floatPane.renderDirty = true
	_, _ = floatPane.VTerm.Write([]byte("FLOAT-TOP"))

	view := xansi.Strip(model.View())
	if !containsAll(view, "float-monitor [floating]", "FLOAT-TOP") {
		t.Fatalf("expected floating frame and body in initial render, got:\n%s", view)
	}

	for i := 0; i < 64; i++ {
		_, _ = basePane.VTerm.Write([]byte(fmt.Sprintf("\rBASE-%02d", i)))
		basePane.renderDirty = true

		view = xansi.Strip(model.View())
		if !containsAll(view, "float-monitor [floating]", "FLOAT-TOP") {
			t.Fatalf("expected floating frame to remain visible after tiled redraw %d, got:\n%s", i, view)
		}
	}
}

func TestFloatingViewportTitlesIncludeZOrder(t *testing.T) {
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
		t.Fatalf("expected 2 floating viewports, got %d", len(tab.Floating))
	}
	tab.Floating[0].Rect = Rect{X: 4, Y: 2, W: 28, H: 8}
	tab.Floating[1].Rect = Rect{X: 34, Y: 3, W: 28, H: 8}
	tab.Panes[tab.Floating[0].PaneID].Title = "float-a"
	tab.Panes[tab.Floating[1].PaneID].Title = "float-b"
	tab.Panes[tab.Floating[0].PaneID].renderDirty = true
	tab.Panes[tab.Floating[1].PaneID].renderDirty = true

	view := xansi.Strip(model.View())
	if !containsAll(view, "float-a [floating z:1]", "float-b [floating z:2]") {
		t.Fatalf("expected floating titles to include z-order, got:\n%s", view)
	}
}

func TestHelpAndStatusShowViewportControlsAndState(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
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
	pane.renderDirty = true

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	pane.Offset = Point{X: 4, Y: 0}
	pane.renderDirty = true

	model.width = 240
	status := xansi.Strip(model.renderStatus())
	if !containsAll(status, "mode:fixed", "pinned", "readonly", "offset:4,0") {
		t.Fatalf("expected status to expose viewport state, got:\n%s", status)
	}

	model.height = 32
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	helpView := xansi.Strip(model.View())
	if !containsAll(helpView, "Ctrl-a M", "Ctrl-a P", "Ctrl-a R", "Ctrl-a Ctrl-h/j/k/l") {
		t.Fatalf("expected help to include viewport controls, got:\n%s", helpView)
	}
}

func TestFloatingViewportStatusAndHelpExposeFloatingControls(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})
	model.width = 160
	model.height = 32

	msg := mustRunCmd(t, model.Init())
	_, cmd := model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	createFloatingPaneViaPicker(t, model)

	status := xansi.Strip(model.renderStatus())
	if !containsAll(status, "layer:floating") {
		t.Fatalf("expected status to expose floating layer state, got:\n%s", status)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	helpView := xansi.Strip(model.View())
	if !containsAll(helpView, "Ctrl-a w", "Ctrl-a W", "Ctrl-a Tab", "Ctrl-a Alt-h/j/k/l", "Ctrl-a Alt-H/J/K/L") {
		t.Fatalf("expected help to include floating viewport controls, got:\n%s", helpView)
	}
}

func TestPrefixColonBeginsCommandPrompt(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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
	pane.renderDirty = true

	view := model.View()
	if !strings.Contains(view, "\x1b[") {
		t.Fatalf("expected ANSI sequences in rendered view:\n%s", view)
	}
	if !strings.Contains(xansi.Strip(view), "RED") {
		t.Fatalf("expected rendered text in view:\n%s", view)
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
	pane.renderDirty = true

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
	pane.renderDirty = true

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
	_, _ = pane.VTerm.Write([]byte("cursor"))
	pane.live = true
	pane.renderDirty = true

	view := model.View()
	if !strings.Contains(view, "\x1b[0;7m") {
		t.Fatalf("expected reverse-video cursor in rendered view:\n%s", view)
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})

	pane := model.currentTab().Panes[model.currentTab().ActivePaneID]
	if pane == nil || !pane.Readonly {
		t.Fatal("expected active pane to enter readonly mode")
	}

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd != nil {
		_ = mustRunCmd(t, cmd)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	if cmd != nil {
		_ = mustRunCmd(t, cmd)
	}

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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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
	if !model.prefixActive {
		t.Fatal("expected prefix mode to be active")
	}

	_, cmd = model.Update(rawInputMsg{data: []byte{0x01}})
	_ = mustRunCmd(t, cmd)

	if model.prefixActive {
		t.Fatal("expected literal ctrl-a to clear prefix mode")
	}
	if len(client.inputs) != 1 || string(client.inputs[0]) != "\x01" {
		t.Fatalf("expected forwarded ctrl-a, got %#v", client.inputs)
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

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	if !model.prefixActive {
		t.Fatal("expected prefix mode to be active")
	}
	msg := mustRunCmd(t, cmd)
	_, _ = model.Update(msg)
	if model.prefixActive {
		t.Fatal("expected prefix timeout to clear prefix mode")
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if model.currentTab().ActivePaneID != original {
		t.Fatalf("expected left arrow to move focus back to %q, got %q", original, model.currentTab().ActivePaneID)
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if model.terminalPicker == nil {
		t.Fatal("expected floating command to open chooser")
	}
	if model.terminalPicker.Title != "Open Floating Viewport" {
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
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	view := xansi.Strip(model.View())
	if !strings.Contains(view, "○ "+orphanID) {
		t.Fatalf("expected detached terminal %q to appear as orphan in picker:\n%s", orphanID, view)
	}
}

func TestClosingLastViewportQuitsTUIWithoutKillingTerminal(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client, Config{DefaultShell: "/bin/sh"})

	msg := mustRunCmd(t, model.Init())
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

func TestKillingTerminalMarksAllViewportsKilled(t *testing.T) {
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shared")})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)

	if client.kills != 1 || len(client.killedIDs) != 1 || client.killedIDs[0] != "shared-001" {
		t.Fatalf("expected shared terminal kill request, got kills=%d ids=%v", client.kills, client.killedIDs)
	}
	if model.quitting {
		t.Fatal("expected killing the shared terminal to keep killed viewports visible")
	}
	if cmd != nil {
		t.Fatal("expected kill to stay in the TUI")
	}
	if len(model.currentTab().Panes) != 2 {
		t.Fatalf("expected killed terminal viewports to remain visible, got %d panes", len(model.currentTab().Panes))
	}
	view := xansi.Strip(model.View())
	if !containsAll(view, "[killed]", "terminal was killed") {
		t.Fatalf("expected killed marker in viewport render, got:\n%s", view)
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("list-layouts")})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	status := xansi.Strip(model.renderStatus())
	if !strings.Contains(status, "layouts: demo, ops, user-only") {
		t.Fatalf("expected deduped layout list in status, got %q", status)
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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
	if got := xansi.Strip(model.renderStatus()); !strings.Contains(got, "deleted layout: demo") {
		t.Fatalf("expected delete notice in status, got %q", got)
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("delete-layout missing")})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}

	if got := xansi.Strip(model.renderStatus()); !strings.Contains(got, `err:layout "missing" not found`) {
		t.Fatalf("expected missing-layout error in status, got %q", got)
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	msg = mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	_, cmd = model.Update(rawInputMsg{data: []byte("orphan-001")})
	if msg := mustRunCmd(t, cmd); msg != nil {
		_, _ = model.Update(msg)
	}
	_, cmd = model.Update(rawInputMsg{data: []byte{0x7f, '2', 0x0b}})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)

	if model.terminalPicker != nil {
		t.Fatal("expected picker to close after ctrl-k")
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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
	if !strings.Contains(view, "fish") {
		t.Fatalf("expected pane title to include command name, got:\n%s", view)
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
	pane.renderDirty = true
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
	left.renderDirty = true
	_, _ = right.VTerm.Write([]byte("right side"))
	right.live = true
	right.renderDirty = true

	_ = model.View()
	if left.renderDirty || right.renderDirty {
		t.Fatal("expected initial render to clear dirty flags")
	}
	leftCached := firstCellPtr(left.cellCache)
	rightCached := firstCellPtr(right.cellCache)

	_, _ = left.VTerm.Write([]byte("\r\nchanged"))
	left.live = true
	left.renderDirty = true

	_ = model.View()

	if left.renderDirty {
		t.Fatal("expected dirty pane render to be flushed")
	}
	if got := firstCellPtr(right.cellCache); got != rightCached {
		t.Fatal("expected clean sibling pane cache to be reused")
	}
	if leftCached == nil && firstCellPtr(left.cellCache) != nil {
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

	_, _ = model.Update(renderTickMsg{})
	if got := xansi.Strip(model.View()); !strings.Contains(got, "batched output") {
		t.Fatalf("expected render tick to flush pending output, got:\n%s", got)
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
	pane.renderDirty = true

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
		_, _ = model.Update(renderTickMsg{})
	}

	if !pane.catchingUp {
		t.Fatal("expected pane to enter catching-up mode after sustained dirty ticks")
	}
	if got := xansi.Strip(model.renderStatus()); !strings.Contains(got, "catching-up") {
		t.Fatalf("expected status to mention catching-up, got %q", got)
	}

	for i := 0; i < 5; i++ {
		pane.renderDirty = false
		_, _ = model.Update(renderTickMsg{})
	}

	if pane.catchingUp {
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
	pane.VTerm.Resize(20, 6)
	_, _ = pane.VTerm.Write([]byte("0123456789ABCDEFGHIJ"))
	pane.live = true
	pane.renderDirty = true

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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
	pane.renderDirty = true

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})

	pane.Offset = Point{X: 0, Y: 0}
	pane.renderDirty = true
	before := xansi.Strip(model.View())

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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
	pane.renderDirty = true

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})

	for i := 0; i < 8; i++ {
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	}
	if pane.Offset.X != 2 {
		t.Fatalf("expected horizontal offset clamp at 2, got %d", pane.Offset.X)
	}

	for i := 0; i < 4; i++ {
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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
	pane.renderDirty = true

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	pane.Offset = Point{X: 0, Y: 0}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	pane.Offset = Point{X: 3, Y: 2}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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
	pane.renderDirty = true
	pane.Mode = ViewportModeFixed
	pane.Pin = true
	pane.Offset = Point{X: 0, Y: 0}

	model.prefixActive = true
	model.rawPending = []byte{0x0c}
	consumed, cmd, ok := model.consumePrefixInput()
	if !ok || consumed != 1 || cmd != nil {
		t.Fatalf("expected ctrl+l prefix input to be consumed inline, got consumed=%d ok=%v cmd=%v", consumed, ok, cmd != nil)
	}
	if pane.Offset.X != 4 {
		t.Fatalf("expected ctrl+l pan to move offset to 4, got %d", pane.Offset.X)
	}

	model.prefixActive = true
	model.rawPending = []byte("\x1b[1;5D")
	consumed, cmd, ok = model.consumePrefixInput()
	if !ok || consumed != len("\x1b[1;5D") || cmd != nil {
		t.Fatalf("expected ctrl+left prefix sequence to be consumed, got consumed=%d ok=%v cmd=%v", consumed, ok, cmd != nil)
	}
	if pane.Offset.X != 0 {
		t.Fatalf("expected ctrl+left pan to clamp back to 0, got %d", pane.Offset.X)
	}
}

func TestFixedViewportDoesNotSendResizeToTerminal(t *testing.T) {
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
	resizesBefore := client.resizeCalls

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
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
	pane.catchingUp = true
	pane.renderDirty = true
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
			VTerm:       localvterm.New(16, 4, 100, nil),
			live:        true,
			renderDirty: true,
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
	pane.renderDirty = true

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

	canvas := newComposedCanvas(14, 6)
	rect := Rect{X: 0, Y: 0, W: 14, H: 6}
	canvas.drawPane(rect, pane, false)
	_ = canvas.String()
	for i := range canvas.rowDirty {
		canvas.rowDirty[i] = false
	}
	canvas.fullDirty = false

	pane.renderDirty = true
	pane.dirtyRowsKnown = true
	pane.dirtyRowStart = 1
	pane.dirtyRowEnd = 1
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
	if pane.renderDirty || pane.dirtyRowsKnown {
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

	canvas := newComposedCanvas(18, 5)
	rect := Rect{X: 0, Y: 0, W: 18, H: 5}
	canvas.drawPaneFrame(rect, pane, false)
	canvas.drawText(Rect{X: 1, Y: 1, W: 16, H: 1}, []string{"abcdefghijklmnop"}, drawStyle{})

	pane.Snapshot.Screen.Cells[0] = makeRow("abcXYZghijklmnop")
	pane.renderDirty = true
	pane.dirtyRowsKnown = true
	pane.dirtyRowStart = 0
	pane.dirtyRowEnd = 0
	pane.dirtyColsKnown = true
	pane.dirtyColStart = 3
	pane.dirtyColEnd = 5
	canvas.drawPaneBody(rect, pane, false)

	body := xansi.Strip(rowToANSI(canvas.cells[1]))
	if !strings.Contains(body, "abcXYZgh") {
		t.Fatalf("expected updated mid-row segment, got %q", body)
	}
	if pane.dirtyColsKnown {
		t.Fatal("expected dirty column state to clear after redraw")
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

	canvas := newComposedCanvas(18, 5)
	rect := Rect{X: 0, Y: 0, W: 18, H: 5}
	canvas.drawPaneFrame(rect, pane, true)
	canvas.drawText(Rect{X: 1, Y: 1, W: 16, H: 1}, []string{"abcdefghijklmnop"}, drawStyle{})

	pane.Snapshot.Screen.Cells[0] = makeRow("abcXYZghijklmnop")
	pane.renderDirty = true
	pane.dirtyRowsKnown = true
	pane.dirtyRowStart = 0
	pane.dirtyRowEnd = 0
	pane.dirtyColsKnown = true
	pane.dirtyColStart = 3
	pane.dirtyColEnd = 8
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
			VTerm:       localvterm.New(16, 4, 100, nil),
			live:        true,
			renderDirty: true,
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
			cellCache:   [][]drawCell{stringToDrawCells("012345", drawStyle{}), stringToDrawCells("abcdef", drawStyle{})},
			cellVersion: 1,
		},
	}

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
			cellCache:   [][]drawCell{stringToDrawCells("012345", drawStyle{})},
			cellVersion: 1,
		},
	}

	first := paneCellsForViewport(pane, 3, 1)
	pane.cellCache = [][]drawCell{stringToDrawCells("xyz789", drawStyle{})}
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
			Mode:        ViewportModeFixed,
			Offset:      Point{X: 2, Y: 0},
			VTerm:       localvterm.New(12, 4, 100, nil),
			live:        true,
			renderDirty: true,
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
	if pane.cellCache != nil {
		t.Fatal("expected fixed live viewport render to avoid populating full grid cache")
	}
	if pane.renderDirty {
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
	pane.renderDirty = true

	view := xansi.Strip(model.View())
	if !strings.Contains(view, "fit-direct-path") {
		t.Fatalf("expected rendered output, got:\n%s", view)
	}
	if pane.cellCache != nil {
		t.Fatal("expected live fit view render to avoid full grid cache")
	}
	if pane.renderDirty {
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
	pane.renderDirty = true
	pane.cellCache = nil

	view := xansi.Strip(model.View())
	if !strings.Contains(view, "snapshot-direct") {
		t.Fatalf("expected rendered snapshot output, got:\n%s", view)
	}
	if pane.cellCache != nil {
		t.Fatal("expected snapshot fit view render to avoid full grid cache")
	}
	if pane.renderDirty {
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

func createFloatingPaneViaPicker(t *testing.T, model *Model) {
	t.Helper()

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	msg := mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if model.terminalPicker == nil {
		t.Fatal("expected floating command to open chooser")
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}
}

func createSplitPaneViaPicker(t *testing.T, model *Model, dir SplitDirection) {
	t.Helper()

	key := '%'
	if dir == SplitHorizontal {
		key = '"'
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
	msg := mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if model.terminalPicker == nil {
		t.Fatal("expected split command to open chooser")
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	msg := mustRunCmd(t, cmd)
	_, _ = model.Update(msg)

	if model.terminalPicker == nil {
		t.Fatal("expected new tab command to open chooser")
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = mustRunCmd(t, cmd)
	_, cmd = model.Update(msg)
	if cmd != nil {
		if next := mustRunCmd(t, cmd); next != nil {
			_, _ = model.Update(next)
		}
	}
}

type fakeClient struct {
	next          int
	createCalls   int
	nextChannel   uint16
	inputs        [][]byte
	resizeCalls   int
	resizeCols    uint16
	resizeRows    uint16
	resizeChannel uint16
	kills         int
	killedIDs     []string
	attachedIDs   []string
	listResult    []protocol.TerminalInfo
	snapshotByID  map[string]*protocol.Snapshot
	snapshotCalls int
	snapshotErr   error
	terminalByID  map[string]protocol.TerminalInfo
	terminalOrder []string
	createDelay   time.Duration
	listDelay     time.Duration
	attachDelay   time.Duration
	snapshotDelay time.Duration
	inputDelay    time.Duration
	resizeDelay   time.Duration
	killDelay     time.Duration
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
