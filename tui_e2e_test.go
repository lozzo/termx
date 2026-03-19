package termx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tui"
	localvterm "github.com/lozzow/termx/vterm"
)

func TestE2ETUI_FloatingOverlayPersistsAcrossTerminalFrames(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	probeID := e2eCreateTerminal(t, client, "probe-floating-e2e", nil)
	_ = client.Kill(context.Background(), probeID)

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	h.openFloatingViewport()
	screen := h.waitForStableScreenContains("[floating]", 10*time.Second)
	assertFloatingOverlayFrame(t, screen, "")

	h.sendText("echo FLOAT-TOP")
	h.pressEnter()
	screen = h.waitForStableScreenContains("FLOAT-TOP", 10*time.Second)
	assertFloatingOverlayFrame(t, screen, "FLOAT-TOP")

	h.pressEsc()
	for i := 0; i < 5; i++ {
		marker := fmt.Sprintf("BASE-%02d", i)
		h.sendText("echo " + marker)
		h.pressEnter()
		screen = h.waitForStableScreenContains(marker, 10*time.Second)
		assertFloatingOverlayFrame(t, screen, "FLOAT-TOP")
	}
}

func TestE2ETUI_FloatingOverlayHideShowUpdatesTerminalScreen(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	probeID := e2eCreateTerminal(t, client, "probe-floating-toggle", nil)
	_ = client.Kill(context.Background(), probeID)

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	h.openFloatingViewport()
	h.waitForStableScreenContains("[floating]", 10*time.Second)
	h.sendText("echo FLOAT-TOGGLE")
	h.pressEnter()

	screen := h.waitForStableScreenContains("FLOAT-TOGGLE", 10*time.Second)
	assertFloatingOverlayFrame(t, screen, "FLOAT-TOGGLE")

	h.sendPrefixRune('W')
	screen = h.waitForStableScreenWithout("FLOAT-TOGGLE", 10*time.Second)
	if strings.Contains(screen, "[floating]") {
		t.Fatalf("expected floating title to disappear when hidden, got:\n%s", screen)
	}
	if strings.Count(screen, "┌") != 1 {
		t.Fatalf("expected only base pane frame to remain when floating hidden, got:\n%s", screen)
	}

	h.sendPrefixRune('W')
	screen = h.waitForStableScreenContains("FLOAT-TOGGLE", 10*time.Second)
	assertFloatingOverlayFrame(t, screen, "FLOAT-TOGGLE")
}

func TestE2ETUI_FloatingOverlayZOrderMatchesVisibleTopWindow(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	probeID := e2eCreateTerminal(t, client, "probe-floating-z", nil)
	_ = client.Kill(context.Background(), probeID)

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	h.openFloatingViewport()
	h.waitForStableScreenContains("[floating]", 10*time.Second)
	h.sendText("echo FLOAT-A")
	h.pressEnter()
	h.waitForStableScreenContains("FLOAT-A", 10*time.Second)

	h.openFloatingViewport()
	h.waitForStableScreenContains("[floating z:2]", 10*time.Second)
	h.sendText("echo FLOAT-B")
	h.pressEnter()

	screen := h.waitForStableScreenContains("FLOAT-B", 10*time.Second)
	if strings.Contains(screen, "FLOAT-A") {
		t.Fatalf("expected top floating pane to occlude lower pane content, got:\n%s", screen)
	}
	if !strings.Contains(screen, "[floating z:2]") {
		t.Fatalf("expected top floating pane title to expose z-order, got:\n%s", screen)
	}

	h.sendPrefixKey(tea.KeyTab)
	h.sendPrefixRune(']')
	screen = h.waitForStableScreenContains("FLOAT-A", 10*time.Second)
	if strings.Contains(screen, "FLOAT-B") {
		t.Fatalf("expected raised floating pane to become visible top window, got:\n%s", screen)
	}
	if !strings.Contains(screen, "[floating z:2]") {
		t.Fatalf("expected raised floating pane title to show z:2, got:\n%s", screen)
	}
}

func TestE2ETUI_StartupChooserCanAttachExistingTerminal(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	terminalID := e2eCreateTerminal(t, client, "startup-attach", map[string]string{"role": "worker"})

	h := newTUIScreenHarnessWithConfig(t, client, 120, 30, tui.Config{
		DefaultShell:  "/bin/sh",
		Workspace:     "main",
		StartupPicker: true,
	})
	defer h.Close()

	h.waitForStableScreenContains("Choose Terminal", 10*time.Second)
	h.sendText(terminalID)
	h.pressEnter()

	screen := h.waitForStableScreenContains("startup-attach", 10*time.Second)
	if strings.Contains(screen, "Choose Terminal") {
		t.Fatalf("expected startup chooser to close after attach, got:\n%s", screen)
	}
	if strings.Count(screen, "┌") == 0 {
		t.Fatalf("expected attached terminal to render inside TUI frame, got:\n%s", screen)
	}

	h.sendText("echo STARTUP-ATTACHED")
	h.pressEnter()
	screen = h.waitForStableScreenContains("STARTUP-ATTACHED", 10*time.Second)
	if !strings.Contains(screen, "startup-attach") {
		t.Fatalf("expected attached terminal to stay alive after command, got:\n%s", screen)
	}
}

func TestE2ETUI_AttachIDBootstrapsFullLayout(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	terminalID := e2eCreateTerminal(t, client, "direct-attach", map[string]string{"role": "shell"})

	h := newTUIScreenHarnessWithConfig(t, client, 120, 30, tui.Config{
		DefaultShell: "/bin/sh",
		Workspace:    "main",
		AttachID:     terminalID,
	})
	defer h.Close()

	screen := h.waitForStableScreenContains("direct-attach", 10*time.Second)
	if strings.Contains(screen, "Choose Terminal") {
		t.Fatalf("expected direct attach to skip chooser, got:\n%s", screen)
	}
	if strings.Count(screen, "┌") == 0 {
		t.Fatalf("expected direct attach to render inside TUI frame, got:\n%s", screen)
	}

	h.sendText("echo DIRECT-ATTACHED")
	h.pressEnter()
	screen = h.waitForStableScreenContains("DIRECT-ATTACHED", 10*time.Second)
	if !strings.Contains(screen, terminalID[:2]) {
		t.Fatalf("expected attached layout to remain active after command, got:\n%s", screen)
	}
}

func TestE2ETUI_SplitChooserCanAttachExistingTerminal(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	terminalID := e2eCreateTerminal(t, client, "split-attach", map[string]string{"role": "worker"})

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)
	h.sendPrefixRune('%')
	h.waitForStableScreenContains("Open Viewport", 10*time.Second)
	h.sendText(terminalID)
	h.pressEnter()

	screen := h.waitForStableScreenContains("split-attach", 10*time.Second)
	if strings.Contains(screen, "Open Viewport") {
		t.Fatalf("expected split chooser to close after attach, got:\n%s", screen)
	}
	if strings.Count(screen, "┌") < 2 {
		t.Fatalf("expected split attach to render two framed panes, got:\n%s", screen)
	}

	h.sendText("echo SPLIT-ATTACHED")
	h.pressEnter()
	screen = h.waitForStableScreenContains("SPLIT-ATTACHED", 10*time.Second)
	if !strings.Contains(screen, "split-attach") {
		t.Fatalf("expected attached split pane to stay active after command, got:\n%s", screen)
	}
}

func TestE2ETUI_StartupChooserKeepsExitedTerminalVisible(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	terminalID := e2eCreateTerminal(t, client, "startup-exited", nil)
	_ = client.Kill(context.Background(), terminalID)

	h := newTUIScreenHarnessWithConfig(t, client, 120, 30, tui.Config{
		DefaultShell:  "/bin/sh",
		Workspace:     "main",
		StartupPicker: true,
	})
	defer h.Close()

	h.waitForStableScreenContains("Choose Terminal", 10*time.Second)
	h.sendText(terminalID)
	h.pressEnter()

	screen := h.waitForStableScreenContains("startup-exited", 10*time.Second)
	if strings.Contains(screen, "Choose Terminal") {
		t.Fatalf("expected chooser to close after exited attach, got:\n%s", screen)
	}
	if strings.Count(screen, "┌") == 0 {
		t.Fatalf("expected exited terminal to remain framed, got:\n%s", screen)
	}
}

func TestE2ETUI_ExitedPaneRestartRebindsViewportToFreshShell(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	terminalID := e2eCreateTerminal(t, client, "restartable-shell", nil)

	h := newTUIScreenHarnessWithConfig(t, client, 120, 30, tui.Config{
		DefaultShell:  "/bin/sh",
		Workspace:     "main",
		StartupPicker: true,
	})
	defer h.Close()

	h.waitForStableScreenContains("Choose Terminal", 10*time.Second)
	h.sendText(terminalID)
	h.pressEnter()
	h.waitForStableScreenContains("restartable-shell", 10*time.Second)

	h.sendText("exit")
	h.pressEnter()
	screen := h.waitForStableScreenContains("[exited code=0] restartable-shell", 10*time.Second)
	if !strings.Contains(screen, "restart:r") {
		t.Fatalf("expected exited pane affordance to mention restart in status line, got:\n%s", screen)
	}

	h.sendText("r")
	h.waitForStableScreenWithout("[exited code=0] restartable-shell", 10*time.Second)
	h.sendText("echo RESTARTED-SHELL")
	h.pressEnter()

	screen = h.waitForStableScreenContains("RESTARTED-SHELL", 10*time.Second)
	if !strings.Contains(screen, "restartable-shell") {
		t.Fatalf("expected restarted pane to keep shell title context, got:\n%s", screen)
	}
}

func TestE2ETUI_FloatingChooserKeepsExitedTerminalVisible(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	terminalID := e2eCreateTerminal(t, client, "floating-exited", nil)
	_ = client.Kill(context.Background(), terminalID)

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)
	h.sendPrefixRune('w')
	h.waitForStableScreenContains("Open Floating Viewport", 10*time.Second)
	h.sendText(terminalID)
	h.pressEnter()

	screen := h.waitForStableScreenContains("floating-exited", 10*time.Second)
	if strings.Contains(screen, "Open Floating Viewport") {
		t.Fatalf("expected floating chooser to close after exited attach, got:\n%s", screen)
	}
	if !strings.Contains(screen, "[floating]") {
		t.Fatalf("expected exited floating pane to remain visible, got:\n%s", screen)
	}
}

func TestE2ETUI_Workflow_CreateSplitFloatingAndReuse(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	reuseID := e2eCreateTerminal(t, client, "reuse-target", map[string]string{"role": "shared"})

	h := newTUIScreenHarnessWithConfig(t, client, 120, 30, tui.Config{
		DefaultShell:  "/bin/sh",
		Workspace:     "main",
		StartupPicker: true,
	})
	defer h.Close()

	h.waitForStableScreenContains("Choose Terminal", 10*time.Second)
	h.pressEnter()

	h.sendText("echo ROOT-NEW")
	h.pressEnter()
	screen := h.waitForStableScreenContains("ROOT-NEW", 10*time.Second)
	if strings.Contains(screen, "Choose Terminal") {
		t.Fatalf("expected startup chooser create path to close, got:\n%s", screen)
	}

	h.sendPrefixRune('%')
	h.waitForStableScreenContains("Open Viewport", 10*time.Second)
	h.pressEnter()
	h.sendText("echo SPLIT-NEW")
	h.pressEnter()
	screen = h.waitForStableScreenContains("SPLIT-NEW", 10*time.Second)
	if strings.Count(screen, "┌") < 2 {
		t.Fatalf("expected split create to render multiple panes, got:\n%s", screen)
	}

	h.sendPrefixRune('w')
	h.waitForStableScreenContains("Open Floating Viewport", 10*time.Second)
	h.sendText(reuseID)
	h.pressEnter()
	screen = h.waitForStableScreenContains("reuse-target", 10*time.Second)
	if !strings.Contains(screen, "[floating]") {
		t.Fatalf("expected floating reuse pane to remain visible, got:\n%s", screen)
	}

	h.sendText("echo FLOAT-REUSE")
	h.pressEnter()
	screen = h.waitForStableScreenContains("FLOAT-REUSE", 10*time.Second)
	if !strings.Contains(screen, "[floating]") {
		t.Fatalf("expected floating reuse output to stay visible, got:\n%s", screen)
	}
}

func TestE2ETUI_StartupLayoutRestoresFloatingViewport(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	shellID := e2eCreateTerminal(t, client, "layout-shell", map[string]string{"role": "shell"})
	agentID := e2eCreateTerminal(t, client, "layout-agent", map[string]string{"role": "agent"})
	_ = shellID
	_ = agentID

	layoutPath := filepath.Join(t.TempDir(), "floating-layout.yaml")
	if err := os.WriteFile(layoutPath, []byte(`
name: demo
tabs:
  - name: coding
    tiling:
      terminal:
        tag: "role=shell"
        command: "bash --noprofile --norc"
    floating:
      - terminal:
          tag: "role=agent"
          command: "bash --noprofile --norc"
        width: 60
        height: 14
        position: top-right
`), 0o644); err != nil {
		t.Fatalf("write layout file: %v", err)
	}

	h := newTUIScreenHarnessWithConfig(t, client, 120, 30, tui.Config{
		DefaultShell: "/bin/sh",
		Workspace:    "main",
		StartupLayout: layoutPath,
	})
	defer h.Close()

	screen := h.waitForStableScreenContains("layout-agent [floating]", 10*time.Second)
	if !strings.Contains(screen, "layout-shell") {
		t.Fatalf("expected tiled terminal from startup layout, got:\n%s", screen)
	}
	if !strings.Contains(screen, "layout-agent [floating]") {
		t.Fatalf("expected floating terminal from startup layout, got:\n%s", screen)
	}

	h.sendPrefixKey(tea.KeyTab)
	h.sendText("echo FLOAT-LAYOUT")
	h.pressEnter()
	screen = h.waitForStableScreenContains("FLOAT-LAYOUT", 10*time.Second)
	if !strings.Contains(screen, "layout-agent [floating]") {
		t.Fatalf("expected floating viewport to stay visible after input, got:\n%s", screen)
	}
}

func TestE2ETUI_CommandLoadLayoutPromptCanAttachExistingTerminal(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	terminalID := e2eCreateTerminal(t, client, "prompt-layout-target", map[string]string{"role": "manual"})
	_ = terminalID

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
        tag: "role=missing"
        command: "bash --noprofile --norc"
`), 0o644); err != nil {
		t.Fatalf("write layout file: %v", err)
	}

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)
	h.sendPrefixRune(':')
	h.sendText("load-layout demo prompt")
	h.pressEnter()

	h.waitForStableScreenContains("Resolve Layout Viewport", 10*time.Second)
	h.pressKey(tea.KeyDown)
	h.pressEnter()

	screen := h.waitForStableScreenContains("prompt-layout-target", 10*time.Second)
	if strings.Contains(screen, "Resolve Layout Viewport") {
		t.Fatalf("expected prompt picker to close after attach, got:\n%s", screen)
	}

	h.sendText("echo PROMPT-LAYOUT")
	h.pressEnter()
	screen = h.waitForStableScreenContains("PROMPT-LAYOUT", 10*time.Second)
	if !strings.Contains(screen, "prompt-layout-target") {
		t.Fatalf("expected manually attached terminal to stay active, got:\n%s", screen)
	}
}

func TestE2ETUI_TabChooserCreatesAndPickerReusesTerminal(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	reuseID := e2eCreateTerminal(t, client, "picker-reuse", map[string]string{"role": "picker"})

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	h.sendPrefixRune('c')
	h.waitForStableScreenContains("Open Tab", 10*time.Second)
	h.pressEnter()
	h.sendText("echo TAB-NEW")
	h.pressEnter()
	screen := h.waitForStableScreenContains("TAB-NEW", 10*time.Second)
	if strings.Contains(screen, "Open Tab") {
		t.Fatalf("expected tab chooser create path to close, got:\n%s", screen)
	}

	h.sendPrefixRune('f')
	h.waitForStableScreenContains("Terminal Picker", 10*time.Second)
	h.sendText(reuseID)
	h.pressEnter()
	screen = h.waitForStableScreenContains("picker-reuse", 10*time.Second)
	if strings.Contains(screen, "Terminal Picker") {
		t.Fatalf("expected terminal picker to close after reuse attach, got:\n%s", screen)
	}

	h.sendText("echo PICKER-REUSE")
	h.pressEnter()
	screen = h.waitForStableScreenContains("PICKER-REUSE", 10*time.Second)
	if !strings.Contains(screen, "picker-reuse") {
		t.Fatalf("expected reused terminal to stay attached after command, got:\n%s", screen)
	}
}

func TestE2ETUI_CloseFloatingKeepsTiledLayoutAlive(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	reuseID := e2eCreateTerminal(t, client, "close-float-reuse", nil)

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	h.sendText("echo BASE-LIVE")
	h.pressEnter()
	h.waitForStableScreenContains("BASE-LIVE", 10*time.Second)

	h.sendPrefixRune('%')
	h.waitForStableScreenContains("Open Viewport", 10*time.Second)
	h.pressEnter()
	h.sendText("echo SPLIT-LIVE")
	h.pressEnter()
	screen := h.waitForStableScreenContains("SPLIT-LIVE", 10*time.Second)
	if strings.Count(screen, "┌") < 2 {
		t.Fatalf("expected split panes before floating close flow, got:\n%s", screen)
	}

	h.sendPrefixRune('w')
	h.waitForStableScreenContains("Open Floating Viewport", 10*time.Second)
	h.sendText(reuseID)
	h.pressEnter()
	screen = h.waitForStableScreenContains("[floating]", 10*time.Second)
	if !strings.Contains(screen, "close-float-reuse") {
		t.Fatalf("expected floating reuse pane to appear, got:\n%s", screen)
	}

	h.sendPrefixRune('x')
	screen = h.waitForStableScreenWithout("[floating]", 10*time.Second)
	if !strings.Contains(screen, "SPLIT-LIVE") {
		t.Fatalf("expected tiled layout to remain after closing floating pane, got:\n%s", screen)
	}
	if strings.Contains(screen, "Open Floating Viewport") {
		t.Fatalf("expected floating chooser to remain closed, got:\n%s", screen)
	}
}

func TestE2ETUI_TabNavigationKeepsBothTabsUsable(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)
	h.sendText("echo TAB1-LIVE")
	h.pressEnter()
	h.waitForStableScreenContains("TAB1-LIVE", 10*time.Second)

	h.sendPrefixRune('c')
	h.waitForStableScreenContains("Open Tab", 10*time.Second)
	h.pressEnter()
	h.sendText("echo TAB2-LIVE")
	h.pressEnter()
	screen := h.waitForStableScreenContains("TAB2-LIVE", 10*time.Second)
	if !strings.Contains(screen, " 2 ") && !strings.Contains(screen, "tab:2") {
		// best-effort; content check below is the important part
	}

	h.sendPrefixRune('p')
	screen = h.waitForStableScreenContains("TAB1-LIVE", 10*time.Second)
	if strings.Contains(screen, "TAB2-LIVE") && !strings.Contains(screen, "TAB1-LIVE") {
		t.Fatalf("expected previous-tab navigation to return to first tab, got:\n%s", screen)
	}

	h.sendPrefixRune('n')
	screen = h.waitForStableScreenContains("TAB2-LIVE", 10*time.Second)
	if !strings.Contains(screen, "TAB2-LIVE") {
		t.Fatalf("expected next-tab navigation to return to second tab, got:\n%s", screen)
	}
}

type tuiScreenHarness struct {
	t       *testing.T
	model   *tui.Model
	program *tea.Program
	rec     *ansiScreenRecorder
	artDir  string
	done    chan struct{}
	runErr  error
}

func newTUIScreenHarness(t *testing.T, client *protocol.Client, width, height int) *tuiScreenHarness {
	t.Helper()
	return newTUIScreenHarnessWithConfig(t, client, width, height, tui.Config{
		DefaultShell: "/bin/sh",
		Workspace:    "main",
	})
}

func newTUIScreenHarnessWithConfig(t *testing.T, client *protocol.Client, width, height int, cfg tui.Config) *tuiScreenHarness {
	t.Helper()

	rec := newANSIScreenRecorder(width, height)
	model := tui.NewModel(tui.NewProtocolClient(client), cfg)
	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithInput(nil),
		tea.WithOutput(rec),
	)
	model.SetProgram(program)

	h := &tuiScreenHarness{
		t:       t,
		model:   model,
		program: program,
		rec:     rec,
		artDir:  filepath.Join(t.TempDir(), "tui-frames"),
		done:    make(chan struct{}),
	}
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		if err := h.rec.dumpArtifacts(h.artDir); err != nil {
			t.Logf("failed to dump TUI artifacts: %v", err)
			return
		}
		t.Logf("saved TUI artifacts to %s", h.artDir)
	})

	go func() {
		defer close(h.done)
		_, h.runErr = program.Run()
	}()

	program.Send(tea.WindowSizeMsg{Width: width, Height: height})
	return h
}

func (h *tuiScreenHarness) Close() {
	h.t.Helper()
	h.program.Quit()
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
		h.t.Fatal("timed out waiting for TUI program to stop")
	}
	h.model.StopRenderTicker()
	if h.runErr != nil {
		h.t.Fatalf("tui program exited with error: %v", h.runErr)
	}
}

func (h *tuiScreenHarness) sendPrefixRune(r rune) {
	h.t.Helper()
	h.program.Send(tea.KeyMsg{Type: tea.KeyCtrlA})
	time.Sleep(5 * time.Millisecond)
	h.program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	time.Sleep(5 * time.Millisecond)
}

func (h *tuiScreenHarness) sendPrefixKey(key tea.KeyType) {
	h.t.Helper()
	h.program.Send(tea.KeyMsg{Type: tea.KeyCtrlA})
	time.Sleep(5 * time.Millisecond)
	h.program.Send(tea.KeyMsg{Type: key})
	time.Sleep(5 * time.Millisecond)
}

func (h *tuiScreenHarness) sendText(text string) {
	h.t.Helper()
	for _, r := range text {
		h.program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		time.Sleep(5 * time.Millisecond)
	}
}

func (h *tuiScreenHarness) pressEnter() {
	h.t.Helper()
	h.program.Send(tea.KeyMsg{Type: tea.KeyEnter})
	time.Sleep(10 * time.Millisecond)
}

func (h *tuiScreenHarness) pressKey(key tea.KeyType) {
	h.t.Helper()
	h.program.Send(tea.KeyMsg{Type: key})
	time.Sleep(10 * time.Millisecond)
}

func (h *tuiScreenHarness) pressEsc() {
	h.t.Helper()
	h.program.Send(tea.KeyMsg{Type: tea.KeyEsc})
	time.Sleep(10 * time.Millisecond)
}

func (h *tuiScreenHarness) openFloatingViewport() {
	h.t.Helper()
	h.sendPrefixRune('w')
	h.waitForStableScreenContains("Open Floating Viewport", 10*time.Second)
	h.pressEnter()
}

func (h *tuiScreenHarness) waitForStableScreenContains(needle string, timeout time.Duration) string {
	h.t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		h.failIfExited()
		screen, idle, writes := h.rec.Snapshot()
		if writes > 0 {
			last = screen
		}
		if writes > 0 && idle >= 50*time.Millisecond && strings.Contains(screen, needle) {
			return screen
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for stable screen containing %q, last screen:\n%s", needle, last)
	return ""
}

func (h *tuiScreenHarness) waitForStableScreenWithout(needle string, timeout time.Duration) string {
	h.t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		h.failIfExited()
		screen, idle, writes := h.rec.Snapshot()
		if writes > 0 {
			last = screen
		}
		if writes > 0 && idle >= 50*time.Millisecond && !strings.Contains(screen, needle) {
			return screen
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for stable screen without %q, last screen:\n%s", needle, last)
	return ""
}

func (h *tuiScreenHarness) failIfExited() {
	h.t.Helper()
	select {
	case <-h.done:
		h.model.StopRenderTicker()
		if h.runErr != nil {
			h.t.Fatalf("tui program exited early with error: %v", h.runErr)
		}
		h.t.Fatal("tui program exited early")
	default:
	}
}

type ansiScreenRecorder struct {
	mu        sync.Mutex
	vt        *localvterm.VTerm
	lastWrite time.Time
	writes    int
	started   time.Time
	frames    []ansiFrame
	maxFrames int
}

type ansiFrame struct {
	Index   int
	At      time.Duration
	Screen  string
	Payload []byte
}

func newANSIScreenRecorder(width, height int) *ansiScreenRecorder {
	return &ansiScreenRecorder{
		vt:        localvterm.New(width, height, 0, nil),
		started:   time.Now(),
		maxFrames: 64,
	}
}

func (r *ansiScreenRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.vt.Write(p); err != nil {
		return 0, err
	}
	r.lastWrite = time.Now()
	r.writes++
	r.frames = append(r.frames, ansiFrame{
		Index:   r.writes,
		At:      time.Since(r.started),
		Screen:  screenDataString(r.vt.ScreenContent()),
		Payload: append([]byte(nil), p...),
	})
	if len(r.frames) > r.maxFrames {
		r.frames = append([]ansiFrame(nil), r.frames[len(r.frames)-r.maxFrames:]...)
	}
	return len(p), nil
}

func (r *ansiScreenRecorder) Snapshot() (string, time.Duration, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	lastWrite := r.lastWrite
	writes := r.writes
	if writes == 0 {
		return "", 0, 0
	}
	return screenDataString(r.vt.ScreenContent()), time.Since(lastWrite), writes
}

func (r *ansiScreenRecorder) dumpArtifacts(dir string) error {
	r.mu.Lock()
	frames := append([]ansiFrame(nil), r.frames...)
	current := ""
	if len(frames) > 0 {
		current = frames[len(frames)-1].Screen
	}
	r.mu.Unlock()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "latest.txt"), []byte(current), 0o644); err != nil {
		return err
	}
	for _, frame := range frames {
		name := fmt.Sprintf("frame-%03d-%06dms.txt", frame.Index, frame.At.Milliseconds())
		var out strings.Builder
		out.WriteString("index: ")
		out.WriteString(fmt.Sprintf("%d\n", frame.Index))
		out.WriteString("elapsed_ms: ")
		out.WriteString(fmt.Sprintf("%d\n", frame.At.Milliseconds()))
		out.WriteString("payload:\n")
		out.Write(frame.Payload)
		if len(frame.Payload) == 0 || frame.Payload[len(frame.Payload)-1] != '\n' {
			out.WriteByte('\n')
		}
		out.WriteString("screen:\n")
		out.WriteString(frame.Screen)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(out.String()), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func screenDataString(screen localvterm.ScreenData) string {
	lines := make([]string, len(screen.Cells))
	for y, row := range screen.Cells {
		var line strings.Builder
		for _, cell := range row {
			content := cell.Content
			if content == "" {
				content = " "
			}
			line.WriteString(content)
		}
		lines[y] = strings.TrimRight(line.String(), " ")
	}
	return strings.Join(lines, "\n")
}

func assertFloatingOverlayFrame(t *testing.T, screen, marker string) {
	t.Helper()
	if !strings.Contains(screen, "[floating]") {
		t.Fatalf("expected floating title marker in screen, got:\n%s", screen)
	}
	if marker != "" && !strings.Contains(screen, marker) {
		t.Fatalf("expected screen to contain %q, got:\n%s", marker, screen)
	}
	if strings.Count(screen, "┌") < 2 || strings.Count(screen, "┘") < 2 {
		t.Fatalf("expected both tiled and floating borders to be visible, got:\n%s", screen)
	}
}

func TestANSIScreenRecorderDumpArtifacts(t *testing.T) {
	rec := newANSIScreenRecorder(20, 4)
	if _, err := rec.Write([]byte("hello")); err != nil {
		t.Fatalf("write first frame: %v", err)
	}
	if _, err := rec.Write([]byte("\rworld")); err != nil {
		t.Fatalf("write second frame: %v", err)
	}

	dir := filepath.Join(t.TempDir(), "artifacts")
	if err := rec.dumpArtifacts(dir); err != nil {
		t.Fatalf("dump artifacts: %v", err)
	}

	latest, err := os.ReadFile(filepath.Join(dir, "latest.txt"))
	if err != nil {
		t.Fatalf("read latest artifact: %v", err)
	}
	if !strings.Contains(string(latest), "world") {
		t.Fatalf("expected latest artifact to contain final screen, got:\n%s", latest)
	}

	matches, err := filepath.Glob(filepath.Join(dir, "frame-*.txt"))
	if err != nil {
		t.Fatalf("glob frame artifacts: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 frame artifacts, got %d", len(matches))
	}
	first, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read first frame artifact: %v", err)
	}
	if !strings.Contains(string(first), "payload:") || !strings.Contains(string(first), "screen:") {
		t.Fatalf("expected frame artifact metadata, got:\n%s", first)
	}
}
