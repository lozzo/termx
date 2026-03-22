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
	screen := h.waitForStableScreenContains("[floating", 10*time.Second)
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
	h.waitForStableScreenContains("[floating", 10*time.Second)
	h.sendText("echo FLOAT-TOGGLE")
	h.pressEnter()

	screen := h.waitForStableScreenContains("FLOAT-TOGGLE", 10*time.Second)
	assertFloatingOverlayFrame(t, screen, "FLOAT-TOGGLE")

	h.sendPrefixRune('W')
	screen = h.waitForStableScreenWithout("FLOAT-TOGGLE", 10*time.Second)
	if strings.Contains(screen, "[floating") {
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
	h.waitForStableScreenContains("[floating", 10*time.Second)
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

	h.sendCtrlKey(tea.KeyCtrlO)
	h.waitForMode("floating")
	h.pressKey(tea.KeyTab)
	h.sendText("]")
	screen = h.waitForStableScreenContains("FLOAT-A", 10*time.Second)
	if strings.Contains(screen, "FLOAT-B") {
		t.Fatalf("expected raised floating pane to become visible top window, got:\n%s", screen)
	}
	if !strings.Contains(screen, "[floating z:2]") {
		t.Fatalf("expected raised floating pane title to show z:2, got:\n%s", screen)
	}
}

func TestE2ETUI_FloatingWindowsAreStaggeredAndStatusHintsStayVisible(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	h.openFloatingViewport()
	h.waitForStableScreenContains("[floating", 10*time.Second)

	h.openFloatingViewport()
	screen := h.waitForStableScreenContains("[floating z:2]", 10*time.Second)
	if !strings.Contains(screen, "[floating z:1]") || !strings.Contains(screen, "[floating z:2]") {
		t.Fatalf("expected staggered floating windows to keep both title bars visible, got:\n%s", screen)
	}
	if !strings.Contains(screen, "◫ 2") || !strings.Contains(screen, "◫ float") {
		t.Fatalf("expected chrome to expose compact floating status, got:\n%s", screen)
	}

	h.pressEsc()
	screen = h.waitForStableScreenMatching("tiled focus restored after escaping floating layer", 10*time.Second, func(screen string) bool {
		return strings.Contains(screen, "◫ 2") && strings.Contains(screen, "▣ tiled")
	})
	if !strings.Contains(screen, "▣ tiled") {
		t.Fatalf("expected tiled focus summary after escaping floating layer, got:\n%s", screen)
	}
}

func TestE2ETUI_FloatingCenterShortcutRecentersPartiallyHiddenWindow(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	h.sendCtrlKey(tea.KeyCtrlO)
	h.waitForMode("floating")
	h.sendText("n")
	h.waitForStableScreenContains("Open Floating Pane", 10*time.Second)
	h.pressEnter()
	h.completeDefaultTerminalCreate()
	h.waitForStableScreenContains("[floating", 10*time.Second)

	tab := h.model.CurrentTabForTest()
	if tab == nil || len(tab.Floating) == 0 {
		t.Fatalf("expected floating pane after open, got %#v", tab)
	}
	entry := tab.Floating[len(tab.Floating)-1]
	h.sendCtrlKey(tea.KeyCtrlO)
	h.waitForMode("floating")
	for i := 0; i < 18; i++ {
		h.sendText("h")
	}
	for i := 0; i < 8; i++ {
		h.sendText("k")
	}
	h.waitForStableScreenMatching("floating pane moved partially out of viewport", 10*time.Second, func(string) bool {
		return entry.Rect.X < 0 || entry.Rect.Y < 0
	})

	expected := tui.Rect{
		X: max(0, (120-entry.Rect.W)/2),
		Y: max(0, (28-entry.Rect.H)/2),
		W: entry.Rect.W,
		H: entry.Rect.H,
	}

	h.sendText("c")
	screen := h.waitForStableScreenMatching("floating center shortcut recenters active pane", 10*time.Second, func(screen string) bool {
		return entry.Rect == expected && strings.Contains(screen, "[floating")
	})
	if entry.Rect != expected {
		t.Fatalf("expected center shortcut to recenter floating pane to %+v, got %+v", expected, entry.Rect)
	}
	if !strings.Contains(screen, "[floating") {
		t.Fatalf("expected centered floating pane to remain visible, got:\n%s", screen)
	}
}

func TestE2ETUI_FloatingDragRestoresPreviouslyOccludedFloatingBody(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	bottomID := e2eCreateTerminal(t, client, "float-bottom", nil)
	topID := e2eCreateTerminal(t, client, "float-top", nil)

	bottomCh, _, stopBottom := e2eAttach(t, client, bottomID, "collaborator")
	defer stopBottom()
	topCh, _, stopTop := e2eAttach(t, client, topID, "collaborator")
	defer stopTop()

	if err := client.Input(context.Background(), bottomCh, []byte("printf 'BOTTOM-ROW-0\\nBOTTOM-ROW-1\\nBOTTOM-ROW-2\\n'\n")); err != nil {
		t.Fatalf("seed bottom terminal: %v", err)
	}
	if err := client.Input(context.Background(), topCh, []byte("printf 'TOP-LAYER\\n'\n")); err != nil {
		t.Fatalf("seed top terminal: %v", err)
	}

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	for _, terminalID := range []string{bottomID, topID} {
		h.openFloatingChooser()
		h.sendText(terminalID)
		h.pressEnter()
		h.waitForStableScreenMatching("floating attach completes", 10*time.Second, func(screen string) bool {
			return strings.Contains(screen, "[floating")
		})
	}

	tab := h.model.CurrentTabForTest()
	if tab == nil || len(tab.Floating) != 2 {
		t.Fatalf("expected 2 floating panes, got %#v", tab)
	}

	titles := []string{"bottom", "top"}
	rects := []tui.Rect{
		{X: 4, Y: 2, W: 32, H: 8},
		{X: 12, Y: 4, W: 32, H: 8},
	}
	for i, floating := range tab.Floating {
		if floating == nil {
			t.Fatalf("expected floating entry %d", i)
		}
		pane := tab.Panes[floating.PaneID]
		if pane == nil {
			t.Fatalf("expected pane for floating entry %d", i)
		}
		pane.Name = titles[i]
		pane.Title = titles[i]
		floating.Rect = rects[i]
	}

	h.program.Send(tea.WindowSizeMsg{Width: 121, Height: 30})
	h.program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	screen := h.waitForStableScreenMatching("arranged overlapping floating panes", 10*time.Second, func(screen string) bool {
		return containsAll(screen, "bottom [floating z:1]", "top [floating z:2]", "TOP-LAYER")
	})
	if !strings.Contains(screen, "bottom [floating z:1]") {
		t.Fatalf("expected bottom floating title before drag, got:\n%s", screen)
	}
	if strings.Contains(screen, "BOTTOM-ROW-2") {
		t.Fatalf("expected top floating pane to occlude part of bottom pane before drag, got:\n%s", screen)
	}

	dragged := tab.Floating[1]
	startWrites := h.rec.WriteCount()
	h.mouseDragLeftPath(
		imagePoint{X: dragged.Rect.X + 4, Y: dragged.Rect.Y + 1},
		imagePoint{X: dragged.Rect.X + 16, Y: dragged.Rect.Y + 1},
		imagePoint{X: dragged.Rect.X + 28, Y: dragged.Rect.Y + 1},
		imagePoint{X: dragged.Rect.X + 40, Y: dragged.Rect.Y + 1},
	)
	deadline := time.Now().Add(10 * time.Second)
	modelView := ""
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		screen, _, _ = h.rec.Snapshot()
		modelView = h.model.View()
		if containsAll(screen, "BOTTOM-ROW-0", "BOTTOM-ROW-1", "BOTTOM-ROW-2") {
			break
		}
	}
	if !containsAll(screen, "BOTTOM-ROW-0", "BOTTOM-ROW-1", "BOTTOM-ROW-2") {
		tab = h.model.CurrentTabForTest()
		rects := make([]string, 0, len(tab.Floating))
		for _, floating := range tab.Floating {
			if floating == nil {
				rects = append(rects, "<nil>")
				continue
			}
			rects = append(rects, fmt.Sprintf("%s=%+v z=%d", floating.PaneID, floating.Rect, floating.Z))
		}
		t.Fatalf("expected drag to reveal bottom floating body, rects=%v recorder:\n%s\n\nmodel view:\n%s", rects, screen, modelView)
	}
	if !containsAll(screen, "bottom", "top", "BOTTOM-ROW-0", "BOTTOM-ROW-1", "BOTTOM-ROW-2") {
		t.Fatalf("expected drag result to keep overlapping floating panes rendered, got:\n%s", screen)
	}

	frames := h.rec.FramesSince(startWrites)
	if len(frames) == 0 {
		t.Fatal("expected screen recorder to capture drag frames")
	}
	for _, frame := range frames {
		if !containsAll(frame.Screen, "bottom", "top") {
			t.Fatalf("expected both floating panes to stay rendered during drag in frame %d (%s):\n%s", frame.Index, frame.At, frame.Screen)
		}
	}
}

func TestE2ETUI_V3ShortcutModesAndFloatingResize(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForNormalMode("Ctrl", "▸", "<p> PANE")

	h.sendCtrlKey(tea.KeyCtrlG)
	screen := h.waitForMode("global", "<?> HELP", "<t> TERMINALS")
	if !containsAll(screen, "<?>", "HELP", "<t>", "TERMINALS") {
		t.Fatalf("expected global mode hints, got:\n%s", screen)
	}
	h.sendText("?")
	screen = h.waitForStableScreenContains("Help / Shortcut Map", 10*time.Second)
	if !containsAll(screen, "Most used", "Ctrl-p   pane actions", "Shared terminal", "Esc close") {
		t.Fatalf("expected v3 help content, got:\n%s", screen)
	}
	h.pressEsc()
	h.waitForStableScreenWithout("Help / Shortcut Map", 10*time.Second)

	h.sendCtrlKey(tea.KeyCtrlP)
	h.waitForMode("pane", "<%> SPLIT", "<hjkl> FOCUS")
	h.sendText("%")
	h.waitForStableScreenContains("Open Pane", 10*time.Second)
	h.pressEnter()
	h.completeDefaultTerminalCreate()
	h.pressEsc()
	h.sendText("echo V3-SPLIT")
	h.pressEnter()
	h.waitForStableScreenContains("V3-SPLIT", 10*time.Second)

	h.sendCtrlKey(tea.KeyCtrlT)
	h.waitForStableScreenMatching("tab mode", 10*time.Second, func(screen string) bool {
		return h.model.ActiveModeForTest() == "tab"
	})
	h.sendText("c")
	h.waitForStableScreenContains("Open Tab", 10*time.Second)
	h.pressEnter()
	h.completeDefaultTerminalCreate()
	h.pressEsc()
	h.sendText("echo V3-TAB")
	h.pressEnter()
	h.waitForStableScreenContains("V3-TAB", 10*time.Second)

	h.sendCtrlKey(tea.KeyCtrlV)
	h.waitForMode("connection", "CONNECTION", "<a> TAKE OWNER")
	h.sendText("a")
	h.waitForStableScreenContains("acquired resize control", 10*time.Second)

	h.sendCtrlKey(tea.KeyCtrlO)
	h.waitForMode("floating")
	h.sendText("n")
	h.waitForStableScreenContains("Open Floating Pane", 10*time.Second)
	h.pressEnter()
	h.completeDefaultTerminalCreate()
	h.waitForStableScreenContains("[floating", 10*time.Second)

	tab := h.model.CurrentTabForTest()
	if tab == nil || len(tab.Floating) == 0 {
		t.Fatalf("expected floating pane after v3 shortcut flow, got %#v", tab)
	}
	entry := tab.Floating[len(tab.Floating)-1]
	before := entry.Rect
	h.sendCtrlKey(tea.KeyCtrlO)
	h.waitForMode("floating")
	h.sendText("L")
	h.waitForStableScreenMatching("floating rect resize", 10*time.Second, func(string) bool {
		tab := h.model.CurrentTabForTest()
		if tab == nil || len(tab.Floating) == 0 {
			return false
		}
		after := tab.Floating[len(tab.Floating)-1].Rect
		return after.W > before.W
	})
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

func TestE2ETUI_ScenarioStartupRestoresWorkspaceStateWithSharedTerminalBinding(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	sharedID := e2eCreateTerminal(t, client, "restore-shared", nil)
	statePath := filepath.Join(t.TempDir(), "workspace-state.json")
	if err := os.WriteFile(statePath, []byte(fmt.Sprintf(`{
  "version": 1,
  "workspace": {
    "name": "restore-ws",
    "active_tab": 1,
    "tabs": [
      {
        "name": "left",
        "active_pane_id": "pane-1",
        "floating_visible": false,
        "root": {"pane_id": "pane-1"},
        "panes": [
          {
            "id": "pane-1",
            "title": "left",
            "terminal_id": %q,
            "command": ["bash"],
            "terminal_state": "running",
            "mode": "fit"
          }
        ]
      },
      {
        "name": "right",
        "active_pane_id": "pane-2",
        "floating_visible": false,
        "auto_acquire_resize": true,
        "root": {"pane_id": "pane-2"},
        "panes": [
          {
            "id": "pane-2",
            "title": "right",
            "terminal_id": %q,
            "command": ["bash"],
            "terminal_state": "running",
            "mode": "fit"
          }
        ]
      }
    ]
  }
}`, sharedID, sharedID)), 0o644); err != nil {
		t.Fatalf("write workspace state: %v", err)
	}

	h := newTUIScreenHarnessWithConfig(t, client, 120, 30, tui.Config{
		DefaultShell:       "/bin/sh",
		WorkspaceStatePath: statePath,
	})
	defer h.Close()

	h.waitForStableScreenContains("restore-shared", 10*time.Second)

	tab := h.model.CurrentTabForTest()
	if tab == nil || tab.Name != "right" || !tab.AutoAcquireResize {
		t.Fatalf("expected restored active tab with auto-acquire, got %#v", tab)
	}
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil || pane.TerminalID != sharedID || !pane.ResizeAcquired {
		t.Fatalf("expected restored active pane to bind shared terminal with resize acquire, got %#v", pane)
	}

	h.model.ActivateTabForTest(0)
	tab = h.model.CurrentTabForTest()
	if tab == nil || tab.Name != "left" {
		t.Fatalf("expected to switch to restored left tab, got %#v", tab)
	}
	if pane := tab.Panes[tab.ActivePaneID]; pane == nil || pane.TerminalID != sharedID {
		t.Fatalf("expected restored left tab to keep shared terminal binding, got %#v", pane)
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
	if !strings.Contains(screen, "direct-attach") {
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
	h.waitForStableScreenContains("Open Pane", 10*time.Second)
	h.sendText(terminalID)
	h.pressEnter()

	screen := h.waitForStableScreenContains("split-attach", 10*time.Second)
	if strings.Contains(screen, "Open Pane") {
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

func TestE2ETUI_ScenarioLaunchIntoWorkingWorkspace(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	screen := h.waitForStableScreenContains("ws:main", 10*time.Second)
	tab := h.model.CurrentTabForTest()
	if tab == nil || len(tab.Panes) == 0 {
		t.Fatalf("expected startup to open a working tab with panes, got %#v", tab)
	}
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil || strings.TrimSpace(pane.TerminalID) == "" {
		t.Fatalf("expected startup to focus a live pane, got %#v", pane)
	}
	if h.model.PromptKindForTest() != "" || strings.Contains(screen, "Start New Terminal") {
		t.Fatalf("expected direct launch to land in a usable workspace, got:\n%s", screen)
	}

	h.sendText("echo SCENARIO-LAUNCH")
	h.pressEnter()
	screen = h.waitForStableScreenContains("SCENARIO-LAUNCH", 10*time.Second)
	if !strings.Contains(screen, "ws:main") {
		t.Fatalf("expected workspace chrome to remain visible while working, got:\n%s", screen)
	}
}

func TestE2ETUI_ScenarioSplitAndContinueWorking(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)
	before := h.model.CurrentTabForTest()
	if before == nil {
		t.Fatal("expected startup tab before split")
	}
	beforeCount := len(before.Panes)

	h.sendCtrlKey(tea.KeyCtrlP)
	h.waitForMode("pane", "<%> SPLIT", "<hjkl> FOCUS")
	h.sendText("%")
	h.waitForStableScreenContains("Open Pane", 10*time.Second)
	h.pressEnter()
	h.completeDefaultTerminalCreate()
	h.pressEsc()

	screen := h.waitForStableScreenMatching("split pane created", 10*time.Second, func(screen string) bool {
		tab := h.model.CurrentTabForTest()
		return tab != nil && len(tab.Panes) == beforeCount+1 && strings.Count(screen, "┌") >= 2
	})
	if strings.Contains(screen, "Open Pane") {
		t.Fatalf("expected split chooser to close after creating pane, got:\n%s", screen)
	}

	h.sendText("echo SCENARIO-SPLIT")
	h.pressEnter()
	h.waitForStableScreenContains("SCENARIO-SPLIT", 10*time.Second)
}

func TestE2ETUI_ScenarioReuseTerminalInNewTab(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	reuseID := e2eCreateTerminal(t, client, "scenario-reuse-tab", map[string]string{"role": "scenario"})

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	h.sendPrefixRune('c')
	h.waitForStableScreenContains("Open Tab", 10*time.Second)
	h.sendText(reuseID)
	h.pressEnter()

	screen := h.waitForStableScreenContains("scenario-reuse-tab", 10*time.Second)
	if strings.Contains(screen, "Open Tab") {
		t.Fatalf("expected new-tab chooser to close after attach, got:\n%s", screen)
	}

	tab := h.model.CurrentTabForTest()
	if tab == nil {
		t.Fatal("expected active tab after reuse attach")
	}
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil || pane.TerminalID != reuseID {
		t.Fatalf("expected new tab to attach reused terminal %q, got %#v", reuseID, pane)
	}

	h.sendText("echo SCENARIO-TAB-REUSE")
	h.pressEnter()
	screen = h.waitForStableScreenContains("SCENARIO-TAB-REUSE", 10*time.Second)
	if !strings.Contains(screen, "scenario-reuse-tab") {
		t.Fatalf("expected reused terminal title to remain visible, got:\n%s", screen)
	}
}

func TestE2ETUI_ScenarioReuseTerminalInFloatingPane(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	reuseID := e2eCreateTerminal(t, client, "scenario-float-reuse", map[string]string{"role": "float"})

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)
	h.sendText("echo SCENARIO-TILED-BASE")
	h.pressEnter()
	h.waitForStableScreenContains("SCENARIO-TILED-BASE", 10*time.Second)

	h.openFloatingChooser()
	h.sendText(reuseID)
	h.pressEnter()

	screen := h.waitForStableScreenContains("scenario-float-reuse", 10*time.Second)
	if !strings.Contains(screen, "[floating") {
		t.Fatalf("expected reused terminal to open in floating pane, got:\n%s", screen)
	}

	tab := h.model.CurrentTabForTest()
	if tab == nil || len(tab.Floating) == 0 {
		t.Fatalf("expected floating pane after reuse attach, got %#v", tab)
	}
	floating := tab.Panes[tab.Floating[len(tab.Floating)-1].PaneID]
	if floating == nil || floating.TerminalID != reuseID {
		t.Fatalf("expected floating pane to attach reused terminal %q, got %#v", reuseID, floating)
	}

	h.sendText("echo SCENARIO-FLOAT-REUSE")
	h.pressEnter()
	screen = h.waitForStableScreenContains("SCENARIO-FLOAT-REUSE", 10*time.Second)
	if strings.Count(screen, "┌") < 2 || !strings.Contains(screen, "shell-1") {
		t.Fatalf("expected tiled pane to remain visible under floating reuse, got:\n%s", screen)
	}
}

func TestE2ETUI_SharedFloatingAttachKeepsOriginalOwner(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	tab := h.model.CurrentTabForTest()
	if tab == nil {
		t.Fatal("expected startup tab")
	}
	base := tab.Panes[tab.ActivePaneID]
	if base == nil || strings.TrimSpace(base.TerminalID) == "" {
		t.Fatalf("expected startup pane with terminal, got %#v", base)
	}
	if !base.ResizeAcquired {
		t.Fatalf("expected startup pane to carry explicit resize ownership, got %#v", base)
	}
	sharedID := base.TerminalID
	basePaneID := base.ID

	h.openFloatingChooser()
	h.sendText(sharedID)
	h.pressEnter()

	screen := h.waitForStableScreenMatching("shared floating attach keeps original owner", 10*time.Second, func(screen string) bool {
		tab := h.model.CurrentTabForTest()
		if tab == nil || len(tab.Floating) == 0 {
			return false
		}
		tiled := tab.Panes[basePaneID]
		floatPane := tab.Panes[tab.Floating[len(tab.Floating)-1].PaneID]
		return tiled != nil &&
			floatPane != nil &&
			tiled.TerminalID == sharedID &&
			floatPane.TerminalID == sharedID &&
			tiled.ResizeAcquired &&
			!floatPane.ResizeAcquired &&
			strings.Contains(screen, "owner") &&
			strings.Contains(screen, "follower")
	})

	tab = h.model.CurrentTabForTest()
	tiled := tab.Panes[basePaneID]
	floatPane := tab.Panes[tab.Floating[len(tab.Floating)-1].PaneID]
	if tiled == nil || !tiled.ResizeAcquired {
		t.Fatalf("expected original tiled pane to keep owner after floating attach, got %#v", tiled)
	}
	if floatPane == nil || floatPane.ResizeAcquired {
		t.Fatalf("expected floating pane to stay follower after attach, got %#v", floatPane)
	}
	if !containsAll(screen, "owner", "follower") {
		t.Fatalf("expected screen to show owner and follower badges, got:\n%s", screen)
	}
}

func TestE2ETUI_ScenarioEditTerminalMetadata(t *testing.T) {
	srv, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)
	tab := h.model.CurrentTabForTest()
	if tab == nil {
		t.Fatal("expected startup tab before metadata edit")
	}
	pane := tab.Panes[tab.ActivePaneID]
	if pane == nil || strings.TrimSpace(pane.TerminalID) == "" {
		t.Fatalf("expected active pane with terminal before edit, got %#v", pane)
	}

	newName := "scenario-shell"
	h.sendCtrlKey(tea.KeyCtrlG)
	h.waitForMode("global", "<?> HELP", "<t> TERMINALS")
	h.sendText(":")
	h.waitForStableScreenMatching("command prompt open", 10*time.Second, func(screen string) bool {
		return h.model.PromptKindForTest() == "command" && strings.Contains(screen, "command:")
	})
	h.sendText("edit-terminal")
	h.pressEnter()

	h.waitForStableScreenContains("Edit Terminal", 10*time.Second)
	for i := 0; i < 48; i++ {
		h.pressKey(tea.KeyBackspace)
	}
	h.sendText(newName)
	h.pressEnter()

	h.waitForStableScreenContains("Edit Terminal", 10*time.Second)
	for i := 0; i < 64; i++ {
		h.pressKey(tea.KeyBackspace)
	}
	h.sendText("role=api team=infra")
	h.pressEnter()

	info := waitForServerTerminalInfo(t, srv, pane.TerminalID, 10*time.Second, func(info *TerminalInfo) bool {
		return info.Name == newName && info.Tags["role"] == "api" && info.Tags["team"] == "infra"
	})
	if info.Tags["role"] != "api" || info.Tags["team"] != "infra" {
		t.Fatalf("expected updated metadata to round-trip through protocol, got %#v", info)
	}

	screen := h.waitForStableScreenContains(newName, 10*time.Second)
	if !strings.Contains(screen, "updated terminal metadata") {
		t.Fatalf("expected metadata update notice after edit, got:\n%s", screen)
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
	screen := h.waitForStableScreenMatching("pane exits and keeps restartable shell visible", 10*time.Second, func(screen string) bool {
		tab := h.model.CurrentTabForTest()
		if tab == nil {
			return false
		}
		pane := tab.Panes[tab.ActivePaneID]
		return pane != nil && tui.PaneTerminalStateForTest(pane) == "exited" && strings.Contains(screen, "restartable-shell")
	})

	h.sendText("r")
	h.waitForStableScreenMatching("pane restart clears exited state", 10*time.Second, func(screen string) bool {
		tab := h.model.CurrentTabForTest()
		if tab == nil {
			return false
		}
		pane := tab.Panes[tab.ActivePaneID]
		return pane != nil && tui.PaneTerminalStateForTest(pane) == "running"
	})
	h.sendText("echo RESTARTED-SHELL")
	h.pressEnter()

	screen = h.waitForStableScreenContains("RESTARTED-SHELL", 10*time.Second)
	if !strings.Contains(screen, "restartable-shell") {
		t.Fatalf("expected restarted pane to keep shell title context, got:\n%s", screen)
	}
}

func TestE2ETUI_ExitedPaneHistoryUsesNeutralForeground(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)
	h.sendText("printf '\\033[31mRED\\033[0m\\n'; exit")
	h.pressEnter()

	screen := h.waitForStableScreenMatching("pane exits after colored output", 10*time.Second, func(screen string) bool {
		tab := h.model.CurrentTabForTest()
		if tab == nil {
			return false
		}
		pane := tab.Panes[tab.ActivePaneID]
		return pane != nil && tui.PaneTerminalStateForTest(pane) == "exited"
	})
	if !strings.Contains(screen, "RED") {
		t.Fatalf("expected exited pane history to stay visible, got:\n%s", screen)
	}

	rawView := h.model.View()
	if strings.Contains(rawView, "38;2;255;0;0") {
		t.Fatalf("expected exited pane history to drop original red styling, got:\n%s", rawView)
	}
	if !strings.Contains(rawView, "38;2;226;232;240") {
		t.Fatalf("expected exited pane history to render with neutral foreground, got:\n%s", rawView)
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
	h.openFloatingChooser()
	h.sendText(terminalID)
	h.pressEnter()

	screen := h.waitForStableScreenContains("floating-exited", 10*time.Second)
	if strings.Contains(screen, "Open Floating Pane") {
		t.Fatalf("expected floating chooser to close after exited attach, got:\n%s", screen)
	}
	if !strings.Contains(screen, "[floating") {
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
	h.completeDefaultTerminalCreate()

	h.sendText("echo ROOT-NEW")
	h.pressEnter()
	screen := h.waitForStableScreenContains("ROOT-NEW", 10*time.Second)
	if strings.Contains(screen, "Choose Terminal") {
		t.Fatalf("expected startup chooser create path to close, got:\n%s", screen)
	}

	h.sendPrefixRune('%')
	h.waitForStableScreenContains("Open Pane", 10*time.Second)
	h.pressEnter()
	h.completeDefaultTerminalCreate()
	h.waitForStableScreenMatching("split pane shell prompt", 10*time.Second, func(screen string) bool {
		return strings.Count(screen, "┌") >= 2 && strings.Count(screen, "$") >= 2
	})
	h.sendText("echo SPLIT-NEW")
	h.pressEnter()
	screen = h.waitForStableScreenContains("SPLIT-NEW", 10*time.Second)
	if strings.Count(screen, "┌") < 2 {
		t.Fatalf("expected split create to render multiple panes, got:\n%s", screen)
	}

	h.openFloatingChooser()
	h.sendText(reuseID)
	h.pressEnter()
	screen = h.waitForStableScreenContains("reuse-target", 10*time.Second)
	if !strings.Contains(screen, "[floating") {
		t.Fatalf("expected floating reuse pane to remain visible, got:\n%s", screen)
	}

	h.sendText("echo FLOAT-REUSE")
	h.pressEnter()
	screen = h.waitForStableScreenContains("FLOAT-REUSE", 10*time.Second)
	if !strings.Contains(screen, "[floating") {
		t.Fatalf("expected floating reuse output to stay visible, got:\n%s", screen)
	}
}

func TestE2ETUI_SharedTerminalAcrossTiledAndMultipleFloatingViewsSurvivesResize(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	tab := h.model.CurrentTabForTest()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	base := tab.Panes[tab.ActivePaneID]
	if base == nil {
		t.Fatal("expected base pane")
	}
	sharedID := base.TerminalID

	for i := 0; i < 2; i++ {
		h.openFloatingChooser()
		h.sendText(sharedID)
		h.pressEnter()
		h.waitForStableScreenContains("floating", 10*time.Second)
	}

	h.program.Send(tea.WindowSizeMsg{Width: 96, Height: 24})
	h.waitForStableScreenContains("[floating z:2]", 10*time.Second)

	h.pressEsc()
	h.sendText("i=0; while [ $i -lt 40 ]; do echo SHARED-$i; i=$((i+1)); done")
	h.pressEnter()

	screen := h.waitForStableScreenContains("SHARED-39", 10*time.Second)
	if !strings.Contains(screen, "[floating z:2]") {
		t.Fatalf("expected top floating overlay after shared terminal resize, got:\n%s", screen)
	}

	h.program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	screen = h.waitForStableScreenContains("SHARED-39", 10*time.Second)
	if !strings.Contains(screen, "floating") {
		t.Fatalf("expected floating overlays to remain visible after restoring size, got:\n%s", screen)
	}
}

func TestE2ETUI_ScenarioCloseSharedPaneKeepsTerminalAlive(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	tab := h.model.CurrentTabForTest()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	base := tab.Panes[tab.ActivePaneID]
	if base == nil {
		t.Fatal("expected base pane")
	}
	sharedID := base.TerminalID

	h.openFloatingChooser()
	h.sendText(sharedID)
	h.pressEnter()
	screen := h.waitForStableScreenContains("[floating", 10*time.Second)
	if !strings.Contains(screen, "[floating") {
		t.Fatalf("expected floating shared pane before close, got:\n%s", screen)
	}

	h.sendPrefixRune('x')
	screen = h.waitForStableScreenWithout("[floating", 10*time.Second)
	if strings.Contains(screen, "Choose Terminal") {
		t.Fatalf("expected close shared pane to return to layout, got:\n%s", screen)
	}

	tab = h.model.CurrentTabForTest()
	if tab == nil || len(tab.Floating) != 0 {
		t.Fatalf("expected shared floating pane to close, got %#v", tab)
	}
	base = tab.Panes[tab.ActivePaneID]
	if base == nil || base.TerminalID != sharedID {
		t.Fatalf("expected base shared terminal to remain active, got %#v", base)
	}

	h.sendText("echo SHARED-STILL-LIVE")
	h.pressEnter()
	screen = h.waitForStableScreenContains("SHARED-STILL-LIVE", 10*time.Second)
	if strings.Contains(screen, "[floating") {
		t.Fatalf("expected floating pane to stay closed after shared terminal command, got:\n%s", screen)
	}
}

func TestE2ETUI_ScenarioKillSharedTerminalKeepsSavedSlots(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	tab := h.model.CurrentTabForTest()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	base := tab.Panes[tab.ActivePaneID]
	if base == nil {
		t.Fatal("expected base pane")
	}
	sharedID := base.TerminalID

	h.sendCtrlKey(tea.KeyCtrlP)
	h.waitForMode("pane", "<%> SPLIT", "<hjkl> FOCUS")
	h.sendText("%")
	h.waitForStableScreenContains("Open Pane", 10*time.Second)
	h.pressEnter()
	h.completeDefaultTerminalCreate()
	h.pressEsc()
	h.waitForStableScreenMatching("split pane created", 10*time.Second, func(screen string) bool {
		tab := h.model.CurrentTabForTest()
		return tab != nil && len(tab.Panes) == 2 && strings.Count(screen, "┌") >= 2
	})
	h.openFloatingChooser()
	h.sendText(sharedID)
	h.pressEnter()
	screen := h.waitForStableScreenContains("[floating", 10*time.Second)
	if !strings.Contains(screen, "[floating") {
		t.Fatalf("expected shared floating pane before kill, got:\n%s", screen)
	}

	h.sendPrefixRune('X')
	h.waitForStableScreenContains("Stop Terminal", 10*time.Second)
	h.pressEnter()
	screen = h.waitForStableScreenMatching("shared terminal removed", 10*time.Second, func(screen string) bool {
		tab := h.model.CurrentTabForTest()
		if tab == nil || len(tab.Panes) != 3 || !strings.Contains(screen, "left 2 saved panes") {
			return false
		}
		saved := 0
		for _, pane := range tab.Panes {
			if pane != nil && pane.TerminalState == "unbound" {
				saved++
			}
		}
		return saved == 2
	})

	tab = h.model.CurrentTabForTest()
	if tab == nil || len(tab.Panes) != 3 {
		t.Fatalf("expected all pane slots to remain after kill, got %#v", tab)
	}
	unbound := 0
	for _, pane := range tab.Panes {
		if pane == nil {
			t.Fatalf("expected concrete pane slots after kill, got %#v", tab.Panes)
		}
		if pane.TerminalID == sharedID {
			t.Fatalf("expected killed terminal to unbind from all panes, got %#v", pane)
		}
		if pane.TerminalState == "unbound" {
			unbound++
		}
	}
	if unbound != 2 {
		t.Fatalf("expected 2 saved unbound panes after kill, got %d", unbound)
	}
	if !strings.Contains(screen, "left 2 saved panes") {
		t.Fatalf("expected saved-slot notice after removing shared terminal, got:\n%s", screen)
	}
	alive := 0
	for _, pane := range tab.Panes {
		if pane != nil && pane.TerminalID != "" {
			alive++
		}
	}
	if alive != 1 {
		t.Fatalf("expected exactly one surviving bound pane after kill, got %#v", tab.Panes)
	}
}

func TestE2ETUI_ScenarioRemoteKillShowsNoticeAndKeepsSavedSlots(t *testing.T) {
	srv, killerClient, cleanup := newE2EClient(t)
	defer cleanup()

	observerClient, observerCleanup := newE2EProtocolClient(t, srv)
	defer observerCleanup()

	sharedID := e2eCreateTerminal(t, killerClient, "shared-remote-kill", nil)

	killer := newTUIScreenHarnessWithConfig(t, killerClient, 120, 30, tui.Config{
		DefaultShell: "/bin/sh",
		Workspace:    "killer",
		AttachID:     sharedID,
	})
	defer killer.Close()
	killer.waitForStableScreenContains("shared-remote-kill", 10*time.Second)

	observer := newTUIScreenHarnessWithConfig(t, observerClient, 120, 30, tui.Config{
		DefaultShell: "/bin/sh",
		Workspace:    "observer",
		AttachID:     sharedID,
	})
	defer observer.Close()
	observer.waitForStableScreenContains("shared-remote-kill", 10*time.Second)

	observer.sendCtrlKey(tea.KeyCtrlP)
	observer.waitForMode("pane", "<%> SPLIT", "<hjkl> FOCUS")
	observer.sendText("%")
	observer.waitForStableScreenContains("Open Pane", 10*time.Second)
	observer.pressEnter()
	observer.completeDefaultTerminalCreate()
	observer.pressEsc()
	observer.waitForStableScreenMatching("observer split ready", 10*time.Second, func(screen string) bool {
		tab := observer.model.CurrentTabForTest()
		return tab != nil && len(tab.Panes) == 2 && strings.Contains(screen, "shared-remote-kill")
	})

	killer.sendPrefixRune('X')
	killer.waitForStableScreenContains("Stop Terminal", 10*time.Second)
	killer.pressEnter()
	observer.waitForStableScreenMatching("remote remove notice", 10*time.Second, func(screen string) bool {
		tab := observer.model.CurrentTabForTest()
		if tab == nil || len(tab.Panes) != 2 {
			return false
		}
		saved := 0
		for _, pane := range tab.Panes {
			if pane == nil {
				return false
			}
			if pane.TerminalID == sharedID {
				return false
			}
			if pane.TerminalState == "unbound" {
				saved++
			}
		}
		return saved == 1 && strings.Contains(screen, "removed by another client") && strings.Contains(screen, "left 1 saved pane")
	})
}

func TestE2ETUI_TerminalManagerOpensFromGlobalModeAndAttachesSelectedTerminal(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	targetID := e2eCreateTerminal(t, client, "manager-target", nil)

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	h.openTerminalManager()
	h.waitForStableScreenContains("Running Terminals", 10*time.Second)
	h.sendText("manager-target")
	h.pressEnter()
	screen := h.waitForStableScreenMatching("terminal manager attach", 10*time.Second, func(screen string) bool {
		tab := h.model.CurrentTabForTest()
		if tab == nil {
			return false
		}
		pane := tab.Panes[tab.ActivePaneID]
		return pane != nil && pane.TerminalID == targetID && !strings.Contains(screen, "Running Terminals")
	})
	if !containsAll(screen, "manager-target", "ws:main") {
		t.Fatalf("expected attached terminal to become active pane, got:\n%s", screen)
	}
}

func TestE2ETUI_CommandLineOpensTerminalManager(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	_ = e2eCreateTerminal(t, client, "manager-command", nil)

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	h.runCommand("terminals")
	screen := h.waitForStableScreenContains("Running Terminals", 10*time.Second)
	if !containsAll(screen, "manager-command", "Terminal Details", "TERMINALS", "<Enter> BRING HERE") {
		t.Fatalf("expected command line to open terminal manager, got:\n%s", screen)
	}
}

func TestE2ETUI_TerminalManagerCanEditParkedTerminalMetadata(t *testing.T) {
	srv, client, cleanup := newE2EClient(t)
	defer cleanup()

	parkedID := e2eCreateTerminal(t, client, "parked-old", map[string]string{"role": "ops"})

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	h.openTerminalManager()
	h.waitForStableScreenContains("Running Terminals", 10*time.Second)
	h.sendText("parked-old")
	h.waitForStableScreenContains("parked-old", 10*time.Second)
	h.sendCtrlKey(tea.KeyCtrlE)

	h.waitForStableScreenContains("Edit Terminal", 10*time.Second)
	h.waitForStableScreenContains("step 1/2", 10*time.Second)
	for i := 0; i < 48; i++ {
		h.pressKey(tea.KeyBackspace)
	}
	h.sendText("parked-new")
	h.pressEnter()

	h.waitForStableScreenContains("step 2/2", 10*time.Second)
	for i := 0; i < 64; i++ {
		h.pressKey(tea.KeyBackspace)
	}
	h.sendText("role=ops team=infra")
	h.pressEnter()

	info := waitForServerTerminalInfo(t, srv, parkedID, 10*time.Second, func(info *TerminalInfo) bool {
		return info.Name == "parked-new" && info.Tags["role"] == "ops" && info.Tags["team"] == "infra"
	})
	if info.Name != "parked-new" || info.Tags["team"] != "infra" {
		t.Fatalf("expected parked terminal metadata update to persist, got %#v", info)
	}

	screen := h.waitForStableScreenContains("updated terminal metadata", 10*time.Second)
	if !strings.Contains(screen, "updated terminal metadata") {
		t.Fatalf("expected parked terminal edit notice, got:\n%s", screen)
	}

	h.openTerminalManager()
	screen = h.waitForStableScreenContains("Running Terminals", 10*time.Second)
	if !containsAll(screen, "parked-new", "PARKED") {
		t.Fatalf("expected reopened terminal manager to show updated parked terminal metadata, got:\n%s", screen)
	}
}

func TestE2ETUI_ScenarioClosePaneDoesNotNotifyOtherClients(t *testing.T) {
	srv, primaryClient, cleanup := newE2EClient(t)
	defer cleanup()

	secondaryClient, secondaryCleanup := newE2EProtocolClient(t, srv)
	defer secondaryCleanup()

	sharedID := e2eCreateTerminal(t, primaryClient, "shared-close-pane", nil)

	primary := newTUIScreenHarnessWithConfig(t, primaryClient, 120, 30, tui.Config{
		DefaultShell: "/bin/sh",
		Workspace:    "primary",
		AttachID:     sharedID,
	})
	defer primary.Close()
	primary.waitForStableScreenContains("shared-close-pane", 10*time.Second)

	secondary := newTUIScreenHarnessWithConfig(t, secondaryClient, 120, 30, tui.Config{
		DefaultShell: "/bin/sh",
		Workspace:    "secondary",
		AttachID:     sharedID,
	})
	defer secondary.Close()
	secondary.waitForStableScreenContains("shared-close-pane", 10*time.Second)

	secondary.sendPrefixRune('x')
	select {
	case <-secondary.done:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for secondary client to close its last pane")
	}

	primary.sendText("echo PRIMARY-STILL-ATTACHED")
	primary.pressEnter()
	screen := primary.waitForStableScreenContains("PRIMARY-STILL-ATTACHED", 10*time.Second)
	if strings.Contains(screen, "removed by another client") {
		t.Fatalf("expected local pane close on another client to stay silent, got:\n%s", screen)
	}
}

func TestE2ETUI_ScenarioDetachDoesNotInterruptOtherClients(t *testing.T) {
	srv, primaryClient, cleanup := newE2EClient(t)
	defer cleanup()

	secondaryClient, secondaryCleanup := newE2EProtocolClient(t, srv)
	defer secondaryCleanup()

	sharedID := e2eCreateTerminal(t, primaryClient, "shared-detach", nil)

	primary := newTUIScreenHarnessWithConfig(t, primaryClient, 120, 30, tui.Config{
		DefaultShell: "/bin/sh",
		Workspace:    "primary",
		AttachID:     sharedID,
	})
	defer primary.Close()
	primary.waitForStableScreenContains("shared-detach", 10*time.Second)

	secondary := newTUIScreenHarnessWithConfig(t, secondaryClient, 120, 30, tui.Config{
		DefaultShell: "/bin/sh",
		Workspace:    "secondary",
		AttachID:     sharedID,
	})
	defer secondary.Close()
	secondary.waitForStableScreenContains("shared-detach", 10*time.Second)

	secondary.sendPrefixRune('d')
	select {
	case <-secondary.done:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for secondary client to detach")
	}

	primary.sendText("echo PRIMARY-SURVIVES-DETACH")
	primary.pressEnter()
	screen := primary.waitForStableScreenContains("PRIMARY-SURVIVES-DETACH", 10*time.Second)
	if strings.Contains(screen, "removed by another client") {
		t.Fatalf("expected detach on another client to stay silent, got:\n%s", screen)
	}
}

func TestE2ETUI_ScenarioSharedExitedTerminalPropagatesToAllPanes(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	tab := h.model.CurrentTabForTest()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	base := tab.Panes[tab.ActivePaneID]
	if base == nil {
		t.Fatal("expected base pane")
	}
	sharedID := base.TerminalID

	h.openFloatingChooser()
	h.sendText(sharedID)
	h.pressEnter()
	screen := h.waitForStableScreenContains("[floating", 10*time.Second)
	if !strings.Contains(screen, "[floating") {
		t.Fatalf("expected floating shared pane before exit, got:\n%s", screen)
	}

	h.sendText("exit")
	h.pressEnter()

	screen = h.waitForStableScreenMatching("shared exited terminal propagates to all panes", 10*time.Second, func(screen string) bool {
		tab := h.model.CurrentTabForTest()
		if tab == nil || len(tab.Panes) < 2 {
			return false
		}
		exitedCount := 0
		for _, pane := range tab.Panes {
			if pane != nil && pane.TerminalID == sharedID && tui.PaneTerminalStateForTest(pane) == "exited" {
				exitedCount++
			}
		}
		return exitedCount >= 2 && strings.Contains(screen, "[floating")
	})
	tab = h.model.CurrentTabForTest()
	if tab == nil || len(tab.Panes) < 2 {
		t.Fatalf("expected multiple panes after shared exit, got %#v", tab)
	}
	exitedCount := 0
	for _, pane := range tab.Panes {
		if pane != nil && pane.TerminalID == sharedID && pane.TerminalState == "exited" && pane.ExitCode != nil && *pane.ExitCode == 0 {
			exitedCount++
		}
	}
	if exitedCount < 2 {
		t.Fatalf("expected all shared panes to enter exited state, got %d shared exited panes in %#v", exitedCount, tab.Panes)
	}
}

func TestE2ETUI_ScenarioSharedTerminalPreservesOwnerUntilExplicitAcquire(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	tab := h.model.CurrentTabForTest()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	base := tab.Panes[tab.ActivePaneID]
	if base == nil {
		t.Fatal("expected base pane")
	}
	sharedID := base.TerminalID
	initialCols, initialRows := base.VTerm.Size()

	h.sendPrefixRune('%')
	h.waitForStableScreenContains("Open Pane", 10*time.Second)
	h.sendText(sharedID)
	h.pressEnter()
	screen := h.waitForStableScreenMatching("shared terminal keeps original owner", 10*time.Second, func(screen string) bool {
		tab := h.model.CurrentTabForTest()
		if tab == nil {
			return false
		}
		owners := 0
		followers := 0
		for _, pane := range tab.Panes {
			if pane == nil || pane.TerminalID != sharedID || pane.VTerm == nil {
				continue
			}
			if pane.ResizeAcquired {
				owners++
			} else {
				followers++
			}
		}
		return owners == 1 && followers >= 1 && strings.Count(screen, "owner") >= 1 && strings.Count(screen, "follower") >= 1
	})
	if strings.Count(screen, "┌") < 2 {
		t.Fatalf("expected split attach to create a shared pane before acquire test, got:\n%s", screen)
	}

	h.acquireResize()
	h.waitForStableScreenContains("acquired resize control", 10*time.Second)
	h.waitForStableScreenMatching("shared terminal resized after acquire", 10*time.Second, func(string) bool {
		tab := h.model.CurrentTabForTest()
		if tab == nil {
			return false
		}
		shared := 0
		for _, pane := range tab.Panes {
			if pane == nil || pane.TerminalID != sharedID || pane.VTerm == nil {
				continue
			}
			cols, rows := pane.VTerm.Size()
			if cols == initialCols && rows == initialRows {
				return false
			}
			shared++
		}
		return shared >= 1
	})
}

func TestE2ETUI_ScenarioTabAutoAcquireResizeOnEnter(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	tab := h.model.CurrentTabForTest()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	base := tab.Panes[tab.ActivePaneID]
	if base == nil {
		t.Fatal("expected base pane")
	}
	sharedID := base.TerminalID
	initialCols, initialRows := base.VTerm.Size()

	h.sendPrefixRune('c')
	h.waitForStableScreenContains("Open Tab", 10*time.Second)
	h.sendText(sharedID)
	h.pressEnter()
	h.waitForStableScreenContains("shell-1", 10*time.Second)

	h.sendCtrlKey(tea.KeyCtrlP)
	h.waitForMode("pane", "<%> SPLIT", "<hjkl> FOCUS")
	h.sendText("%")
	h.waitForStableScreenContains("Open Pane", 10*time.Second)
	h.pressEnter()
	h.completeDefaultTerminalCreate()
	h.pressEsc()
	h.waitForStableScreenMatching("tab2 split created", 10*time.Second, func(screen string) bool {
		tab := h.model.CurrentTabForTest()
		return tab != nil && len(tab.Panes) == 2 && strings.Count(screen, "┌") >= 2
	})
	h.focusTerminalPane(sharedID)

	h.model.SetCurrentTabAutoAcquireForTest(true)

	h.model.ActivateTabForTest(0)
	h.waitForStableScreenContains("ws:main", 10*time.Second)

	tab = h.model.CurrentTabForTest()
	if tab == nil || tab.ActivePaneID == "" {
		t.Fatal("expected tab1 after switching back")
	}
	for _, pane := range tab.Panes {
		if pane == nil || pane.TerminalID != sharedID || pane.VTerm == nil {
			continue
		}
		cols, rows := pane.VTerm.Size()
		if cols != initialCols || rows != initialRows {
			t.Fatalf("expected tab1 to keep initial shared size before auto-acquire, got %dx%d want %dx%d", cols, rows, initialCols, initialRows)
		}
	}

	h.model.ActivateTabForTest(1)
	h.waitForStableScreenContains("acquired resize control", 10*time.Second)
	h.waitForStableScreenMatching("tab auto acquire resize", 10*time.Second, func(string) bool {
		tab := h.model.CurrentTabForTest()
		if tab == nil || !tab.AutoAcquireResize || len(tab.Panes) < 2 {
			return false
		}
		shared := 0
		for _, pane := range tab.Panes {
			if pane == nil || pane.TerminalID != sharedID || pane.VTerm == nil {
				continue
			}
			cols, rows := pane.VTerm.Size()
			if cols == initialCols && rows == initialRows {
				return false
			}
			shared++
		}
		return shared >= 1
	})
}

func TestE2ETUI_ScenarioSharedTerminalWarnsBeforeOwnershipTransferWhenSizeLockEnabled(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	tab := h.model.CurrentTabForTest()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	base := tab.Panes[tab.ActivePaneID]
	if base == nil {
		t.Fatal("expected base pane")
	}
	sharedID := base.TerminalID
	initialCols, initialRows := base.VTerm.Size()

	h.sendPrefixRune('%')
	h.waitForStableScreenContains("Open Pane", 10*time.Second)
	h.sendText(sharedID)
	h.pressEnter()
	h.waitForStableScreenContains("shell-1", 10*time.Second)

	h.setTerminalTag(sharedID, "termx.size_lock", "warn")

	h.waitForStableScreenMatching("shared terminal keeps one owner before warn acquire", 10*time.Second, func(string) bool {
		tab := h.model.CurrentTabForTest()
		if tab == nil {
			return false
		}
		owners := 0
		for _, pane := range tab.Panes {
			if pane == nil || pane.TerminalID != sharedID || pane.VTerm == nil {
				continue
			}
			if pane.ResizeAcquired {
				owners++
			}
		}
		return owners == 1
	})

	h.acquireResize()
	screen := h.waitForStableScreenContains("Size Change Warning", 10*time.Second)
	if !strings.Contains(screen, "lock mode: warn") {
		t.Fatalf("expected size lock warning modal, got:\n%s", screen)
	}

	h.pressEnter()
	h.waitForStableScreenContains("acquired resize control", 10*time.Second)
	h.waitForStableScreenMatching("shared terminal resized after warn confirmation", 10*time.Second, func(string) bool {
		tab := h.model.CurrentTabForTest()
		if tab == nil {
			return false
		}
		shared := 0
		for _, pane := range tab.Panes {
			if pane == nil || pane.TerminalID != sharedID || pane.VTerm == nil {
				continue
			}
			cols, rows := pane.VTerm.Size()
			if cols == initialCols && rows == initialRows {
				return false
			}
			shared++
		}
		return shared >= 2
	})
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
		DefaultShell:  "/bin/sh",
		Workspace:     "main",
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

func TestE2ETUI_StartupLayoutCanReuseExplicitHintAcrossTiledAndFloatingPanes(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	sharedID := e2eCreateTerminal(t, client, "layout-shared", map[string]string{"role": "shell"})

	layoutPath := filepath.Join(t.TempDir(), "shared-layout.yaml")
	if err := os.WriteFile(layoutPath, []byte(fmt.Sprintf(`
name: shared
tabs:
  - name: dev
    tiling:
      terminal:
        _hint_id: %q
        tag: "role=shell"
        command: "bash --noprofile --norc"
    floating:
      - terminal:
          _hint_id: %q
          tag: "role=shell"
          command: "bash --noprofile --norc"
        width: 60
        height: 14
        position: top-right
`, sharedID, sharedID)), 0o644); err != nil {
		t.Fatalf("write layout file: %v", err)
	}

	h := newTUIScreenHarnessWithConfig(t, client, 120, 30, tui.Config{
		DefaultShell:  "/bin/sh",
		Workspace:     "main",
		StartupLayout: layoutPath,
	})
	defer h.Close()

	h.waitForStableScreenContains("layout-shared", 10*time.Second)
	tab := h.model.CurrentTabForTest()
	if tab == nil || len(tab.Panes) != 2 || len(tab.Floating) != 1 {
		t.Fatalf("expected startup layout to build tiled+floating panes, got %#v", tab)
	}
	shared := 0
	for _, pane := range tab.Panes {
		if pane != nil && pane.TerminalID == sharedID {
			shared++
		}
	}
	if shared != 2 {
		t.Fatalf("expected both layout panes to reuse shared terminal %q, got %#v", sharedID, tab.Panes)
	}
}

func TestE2ETUI_CommandLoadLayoutPromptReusesExplicitHintAcrossPanes(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	sharedID := e2eCreateTerminal(t, client, "prompt-shared", map[string]string{"role": "shared"})

	home := t.TempDir()
	t.Setenv("HOME", home)
	layoutDir := filepath.Join(home, ".config", "termx", "layouts")
	if err := os.MkdirAll(layoutDir, 0o755); err != nil {
		t.Fatalf("mkdir layout dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(layoutDir, "prompt-shared.yaml"), []byte(`
name: prompt-shared
tabs:
  - name: shared
    tiling:
      terminal:
        _hint_id: "missing-shared"
        tag: "role=missing"
        command: "bash --noprofile --norc"
    floating:
      - terminal:
          _hint_id: "missing-shared"
          tag: "role=missing"
          command: "bash --noprofile --norc"
        width: 60
        height: 14
        position: top-right
`), 0o644); err != nil {
		t.Fatalf("write layout file: %v", err)
	}

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)
	h.sendPrefixRune(':')
	h.sendText("load-layout prompt-shared prompt")
	h.pressEnter()

	h.waitForStableScreenContains("Resolve Layout Pane", 10*time.Second)
	h.sendText(sharedID)
	h.pressEnter()

	screen := h.waitForStableScreenContains("prompt-shared [floating]", 10*time.Second)
	if strings.Contains(screen, "Resolve Layout Pane") {
		t.Fatalf("expected repeated explicit hint prompt to resolve in one pass, got:\n%s", screen)
	}
	if !strings.Contains(screen, "prompt-shared") || !strings.Contains(screen, "[floating") {
		t.Fatalf("expected tiled and floating panes after shared prompt attach, got:\n%s", screen)
	}

	tab := h.model.CurrentTabForTest()
	if tab == nil || len(tab.Panes) != 2 || len(tab.Floating) != 1 {
		t.Fatalf("expected shared prompt layout to build tiled+floating panes, got %#v", tab)
	}
	ids := make(map[string]struct{})
	for _, pane := range tab.Panes {
		if pane == nil {
			t.Fatalf("expected concrete panes, got %#v", tab.Panes)
		}
		ids[pane.TerminalID] = struct{}{}
	}
	if len(ids) != 1 {
		t.Fatalf("expected repeated explicit hint prompt to bind one terminal, got %#v", ids)
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

	h.waitForStableScreenContains("Resolve Layout Pane", 10*time.Second)
	h.sendText("prompt-layout-target")
	h.pressEnter()

	screen := h.waitForStableScreenContains("prompt-layout-target", 10*time.Second)
	if strings.Contains(screen, "Resolve Layout Pane") {
		t.Fatalf("expected prompt picker to close after attach, got:\n%s", screen)
	}

	h.sendText("echo PROMPT-LAYOUT")
	h.pressEnter()
	screen = h.waitForStableScreenContains("PROMPT-LAYOUT", 10*time.Second)
	if !strings.Contains(screen, "prompt-layout-target") {
		t.Fatalf("expected manually attached terminal to stay active, got:\n%s", screen)
	}
}

func TestE2ETUI_CommandLoadLayoutSkipLeavesWaitingPaneAndKeepsLayoutUsable(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	home := t.TempDir()
	t.Setenv("HOME", home)
	layoutDir := filepath.Join(home, ".config", "termx", "layouts")
	if err := os.MkdirAll(layoutDir, 0o755); err != nil {
		t.Fatalf("mkdir layout dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(layoutDir, "skip-demo.yaml"), []byte(`
name: skip-demo
tabs:
  - name: logs
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
	h.sendText("load-layout skip-demo skip")
	h.pressEnter()

	screen := h.waitForStableScreenContains("waiting for terminal", 10*time.Second)
	if strings.Contains(screen, "Resolve Layout Pane") {
		t.Fatalf("expected skip policy to avoid picker prompt, got:\n%s", screen)
	}
	h.sendPrefixRune('c')
	h.waitForStableScreenContains("Open Tab", 10*time.Second)
}

func TestE2ETUI_StartupLayoutArrangeGridReusesAllMatchedTerminals(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	_ = e2eCreateTerminal(t, client, "api.log", map[string]string{"type": "log"})
	_ = e2eCreateTerminal(t, client, "worker.log", map[string]string{"type": "log"})
	_ = e2eCreateTerminal(t, client, "redis.log", map[string]string{"type": "log"})

	layoutPath := filepath.Join(t.TempDir(), "arrange-layout.yaml")
	if err := os.WriteFile(layoutPath, []byte(`
name: monitoring
tabs:
  - name: logs
    tiling:
      arrange: grid
      match:
        tag: "type=log"
      min_size: [20, 6]
`), 0o644); err != nil {
		t.Fatalf("write layout file: %v", err)
	}

	h := newTUIScreenHarnessWithConfig(t, client, 120, 30, tui.Config{
		DefaultShell:  "/bin/sh",
		Workspace:     "main",
		StartupLayout: layoutPath,
	})
	defer h.Close()

	screen := h.waitForStableScreenContains("api.log", 10*time.Second)
	if !strings.Contains(screen, "worker.log") || !strings.Contains(screen, "redis.log") {
		t.Fatalf("expected arranged grid to show all matched terminals, got:\n%s", screen)
	}
	if strings.Count(screen, "┌") < 3 {
		t.Fatalf("expected arrange grid to render multiple framed panes, got:\n%s", screen)
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
	h.completeDefaultTerminalCreate()
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

func TestE2ETUI_NewTabReusePreservesAltScreenSnapshotBeforeIncrementalUpdates(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	h.sendText("printf '\\033[?1049h\\033[2J\\033[HCPU 99%%\\r\\nMem 1.2G\\r\\nTasks 42\\r\\nLoad 1.0\\033[H'")
	h.pressEnter()
	screen := h.waitForStableScreenContains("Mem 1.2G", 10*time.Second)
	if !strings.Contains(screen, "Tasks 42") {
		t.Fatalf("expected alt-screen baseline before reuse, got:\n%s", screen)
	}

	tab := h.model.CurrentTabForTest()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	base := tab.Panes[tab.ActivePaneID]
	if base == nil {
		t.Fatal("expected active pane")
	}

	h.sendPrefixRune('c')
	h.waitForStableScreenContains("Open Tab", 10*time.Second)
	h.sendText(base.TerminalID)
	h.pressEnter()
	screen = h.waitForStableScreenContains("Mem 1.2G", 10*time.Second)
	if !strings.Contains(screen, "Tasks 42") || strings.Contains(screen, "Open Tab") {
		t.Fatalf("expected reused tab to restore full alt-screen snapshot, got:\n%s", screen)
	}

	h.sendText("printf '\\033[2;1H!'")
	h.pressEnter()
	screen = h.waitForStableScreenMatching("alt-screen incremental reuse", 10*time.Second, func(screen string) bool {
		return strings.Contains(screen, "!") && strings.Contains(screen, "1.2G") && strings.Contains(screen, "Tasks 42")
	})
	if !strings.Contains(screen, "Load 1.0") {
		t.Fatalf("expected incremental update to preserve existing alt-screen body, got:\n%s", screen)
	}
}

func TestE2ETUI_SplitReusePreservesAltScreenSnapshotBeforeIncrementalUpdates(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)
	seedAltScreenForReuseTest(h)

	tab := h.model.CurrentTabForTest()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	base := tab.Panes[tab.ActivePaneID]
	if base == nil {
		t.Fatal("expected active pane")
	}

	h.sendPrefixRune('%')
	h.waitForStableScreenContains("Open Pane", 10*time.Second)
	h.sendText(base.TerminalID)
	h.pressEnter()
	screen := h.waitForStableScreenMatching("split alt-screen reuse attached", 10*time.Second, func(screen string) bool {
		return !strings.Contains(screen, "Open Pane") &&
			strings.Count(screen, "┌") >= 2 &&
			strings.Contains(screen, "Mem 1.2G") &&
			strings.Contains(screen, "Tasks 42")
	})
	assertAltScreenReuseBodyVisible(t, screen)

	h.sendText("printf '\\033[2;1H!'")
	h.pressEnter()
	screen = h.waitForStableScreenMatching("alt-screen split incremental reuse", 10*time.Second, func(screen string) bool {
		return strings.Contains(screen, "!") && strings.Contains(screen, "1.2G") && strings.Contains(screen, "Tasks 42")
	})
	if !strings.Contains(screen, "Load 1.0") || !strings.Contains(screen, "1.2G") {
		t.Fatalf("expected split incremental update to preserve alt-screen body, got:\n%s", screen)
	}
}

func TestE2ETUI_FloatingReusePreservesAltScreenSnapshotBeforeIncrementalUpdates(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)
	seedAltScreenForReuseTest(h)

	tab := h.model.CurrentTabForTest()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	base := tab.Panes[tab.ActivePaneID]
	if base == nil {
		t.Fatal("expected active pane")
	}

	h.openFloatingChooser()
	h.sendText(base.TerminalID)
	h.pressEnter()
	screen := h.waitForStableScreenMatching("floating alt-screen reuse attached", 10*time.Second, func(screen string) bool {
		return !strings.Contains(screen, "Open Floating Pane") &&
			strings.Contains(screen, "[floating") &&
			strings.Contains(screen, "Mem 1.2G") &&
			strings.Contains(screen, "Tasks 42")
	})
	assertAltScreenReuseBodyVisible(t, screen)

	h.sendText("printf '\\033[2;1H!'")
	h.pressEnter()
	screen = h.waitForStableScreenMatching("alt-screen floating incremental reuse", 10*time.Second, func(screen string) bool {
		return strings.Contains(screen, "!") && strings.Contains(screen, "1.2G") && strings.Contains(screen, "Tasks 42")
	})
	if !strings.Contains(screen, "Load 1.0") || !strings.Contains(screen, "1.2G") || !strings.Contains(screen, "[floating") {
		t.Fatalf("expected floating incremental update to preserve alt-screen body, got:\n%s", screen)
	}
}

func TestE2ETUI_ScenarioSharedAltScreenFloatingResizeRequiresAcquire(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)
	seedAltScreenForReuseTest(h)

	tab := h.model.CurrentTabForTest()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	base := tab.Panes[tab.ActivePaneID]
	if base == nil {
		t.Fatal("expected active pane")
	}
	sharedID := base.TerminalID
	initialCols, initialRows := base.VTerm.Size()

	h.openFloatingChooser()
	h.sendText(sharedID)
	h.pressEnter()
	screen := h.waitForStableScreenMatching("shared alt-screen floating attached", 10*time.Second, func(screen string) bool {
		return !strings.Contains(screen, "Open Floating Pane") &&
			strings.Contains(screen, "[floating") &&
			strings.Contains(screen, "Mem 1.2G") &&
			strings.Contains(screen, "Tasks 42")
	})
	assertAltScreenReuseBodyVisible(t, screen)

	h.program.Send(tea.WindowSizeMsg{Width: 96, Height: 24})
	h.waitForStableScreenMatching("shared alt-screen unchanged without acquire", 10*time.Second, func(string) bool {
		tab := h.model.CurrentTabForTest()
		if tab == nil {
			return false
		}
		shared := 0
		for _, pane := range tab.Panes {
			if pane == nil || pane.TerminalID != sharedID || pane.VTerm == nil {
				continue
			}
			cols, rows := pane.VTerm.Size()
			if cols != initialCols || rows != initialRows {
				return false
			}
			shared++
		}
		return shared >= 2
	})

	h.acquireResize()
	h.waitForStableScreenContains("acquired resize control", 10*time.Second)
	h.sendCtrlKey(tea.KeyCtrlO)
	h.waitForMode("floating")
	h.sendText("L")
	screen = h.waitForStableScreenMatching("shared alt-screen resized after acquire", 10*time.Second, func(screen string) bool {
		tab := h.model.CurrentTabForTest()
		if tab == nil {
			return false
		}
		shared := 0
		resized := 0
		for _, pane := range tab.Panes {
			if pane == nil || pane.TerminalID != sharedID || pane.VTerm == nil {
				continue
			}
			cols, rows := pane.VTerm.Size()
			if cols != initialCols || rows != initialRows {
				resized++
			}
			shared++
		}
		return shared >= 2 && resized >= 1 && strings.Contains(screen, "[floating")
	})
	assertAltScreenReuseBodyVisible(t, screen)
	h.pressEsc()
	h.waitForStableScreenMatching("returned to normal after floating resize", 10*time.Second, func(screen string) bool {
		return h.model.ActiveModeForTest() == "" && containsAll(screen, "Ctrl", "▸", "<p> PANE")
	})

	h.sendText("printf '\\033[2;1H!'")
	h.pressEnter()
	screen = h.waitForStableScreenMatching("shared alt-screen incremental update after acquire", 10*time.Second, func(screen string) bool {
		return strings.Contains(screen, "!") &&
			strings.Contains(screen, "1.2G") &&
			strings.Contains(screen, "Tasks 42") &&
			strings.Contains(screen, "Load 1.0") &&
			strings.Contains(screen, "[floating")
	})
	if !strings.Contains(screen, "1.2G") {
		t.Fatalf("expected shared floating alt-screen body to survive resize and incremental updates, got:\n%s", screen)
	}
}

func TestE2ETUI_SharedSplitAndFloatingAltScreenFramesStayConsistent(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)
	seedAltScreenForReuseTest(h)

	tab := h.model.CurrentTabForTest()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	base := tab.Panes[tab.ActivePaneID]
	if base == nil {
		t.Fatal("expected active pane")
	}
	sharedID := base.TerminalID

	h.sendPrefixRune('%')
	h.waitForStableScreenContains("Open Pane", 10*time.Second)
	h.sendText(sharedID)
	h.pressEnter()
	screen := h.waitForStableScreenMatching("shared split alt-screen attached", 10*time.Second, func(screen string) bool {
		return !strings.Contains(screen, "Open Pane") &&
			strings.Count(screen, "┌") >= 2 &&
			strings.Contains(screen, "Mem 1.2G") &&
			strings.Contains(screen, "Tasks 42")
	})
	assertAltScreenReuseBodyVisible(t, screen)

	h.openFloatingChooser()
	h.sendText(sharedID)
	h.pressEnter()
	screen = h.waitForStableScreenMatching("shared split floating alt-screen attached", 10*time.Second, func(screen string) bool {
		return !strings.Contains(screen, "Open Floating Pane") &&
			strings.Contains(screen, "[floating") &&
			strings.Contains(screen, "Mem 1.2G") &&
			strings.Contains(screen, "Tasks 42")
	})
	assertAltScreenReuseBodyVisible(t, screen)

	startWrites := h.rec.WriteCount()
	h.sendText("printf '\\033[2;1H!\\033[3;1HMem 1.2G\\033[4;1HTasks 42\\033[5;1HLoad 1.0'")
	h.pressEnter()
	screen = h.waitForStableScreenMatching("shared split floating alt-screen incremental", 10*time.Second, func(screen string) bool {
		return strings.Contains(screen, "!") &&
			strings.Contains(screen, "Mem 1.2G") &&
			strings.Contains(screen, "Tasks 42") &&
			strings.Contains(screen, "Load 1.0") &&
			strings.Contains(screen, "[floating")
	})
	if !containsAll(screen, "!", "Mem 1.2G", "Tasks 42", "Load 1.0") {
		t.Fatalf("expected shared split+floating incremental screen to keep updated alt-screen lines, got:\n%s", screen)
	}

	frames := h.rec.FramesSince(startWrites)
	if len(frames) == 0 {
		t.Fatal("expected recorder to capture shared split+floating frames")
	}
	for _, frame := range frames {
		if !strings.Contains(frame.Screen, "[floating") {
			continue
		}
		if strings.Contains(frame.Screen, "!") && !containsAll(frame.Screen, "Mem 1.2G", "Tasks 42", "Load 1.0") {
			t.Fatalf("expected shared split+floating frame %d to keep alt-screen body intact, got:\n%s", frame.Index, frame.Screen)
		}
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
	h.waitForStableScreenContains("Open Pane", 10*time.Second)
	h.pressEnter()
	h.completeDefaultTerminalCreate()
	h.sendText("echo SPLIT-LIVE")
	h.pressEnter()
	screen := h.waitForStableScreenContains("SPLIT-LIVE", 10*time.Second)
	if strings.Count(screen, "┌") < 2 {
		t.Fatalf("expected split panes before floating close flow, got:\n%s", screen)
	}

	h.openFloatingChooser()
	h.sendText(reuseID)
	h.pressEnter()
	screen = h.waitForStableScreenContains("[floating", 10*time.Second)
	if !strings.Contains(screen, "close-float-reuse") {
		t.Fatalf("expected floating reuse pane to appear, got:\n%s", screen)
	}

	h.sendPrefixRune('x')
	screen = h.waitForStableScreenWithout("[floating", 10*time.Second)
	if !strings.Contains(screen, "SPLIT-LIVE") {
		t.Fatalf("expected tiled layout to remain after closing floating pane, got:\n%s", screen)
	}
	if strings.Contains(screen, "Open Floating Pane") {
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
	h.completeDefaultTerminalCreate()
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

func TestE2ETUI_PrefixNavigationCanRepeatWithoutRePrefix(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)
	h.sendText("echo LEFT-PANE")
	h.pressEnter()
	h.waitForStableScreenContains("LEFT-PANE", 10*time.Second)

	h.sendPrefixRune('%')
	h.waitForStableScreenContains("Open Pane", 10*time.Second)
	h.pressEnter()
	h.completeDefaultTerminalCreate()
	h.sendText("echo RIGHT-PANE")
	h.pressEnter()
	screen := h.waitForStableScreenContains("RIGHT-PANE", 10*time.Second)
	if strings.Count(screen, "┌") < 2 {
		t.Fatalf("expected split panes before repeated prefix navigation, got:\n%s", screen)
	}

	h.sendPrefixKey(tea.KeyLeft)
	screen = h.waitForStableScreenContains("LEFT-PANE", 10*time.Second)
	if strings.Contains(screen, "RIGHT-PANE") && !strings.Contains(screen, "LEFT-PANE") {
		t.Fatalf("expected first prefix-left to move focus to left pane, got:\n%s", screen)
	}

	h.pressKey(tea.KeyRight)
	screen = h.waitForStableScreenContains("RIGHT-PANE", 10*time.Second)
	if !strings.Contains(screen, "RIGHT-PANE") {
		t.Fatalf("expected repeated right arrow without re-prefix to move focus back, got:\n%s", screen)
	}
}

func TestE2ETUI_InvalidDirectModeKeyDoesNotFreezeShellInput(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	h.sendCtrlKey(tea.KeyCtrlP)
	screen := h.waitForStableScreenMatching("pane mode entered", 10*time.Second, func(screen string) bool {
		return h.model.ActiveModeForTest() == "pane" && containsAll(screen, "<%> SPLIT", "<hjkl> FOCUS")
	})
	if !containsAll(screen, "SPLIT", "FOCUS") {
		t.Fatalf("expected pane mode hints before invalid key, got:\n%s", screen)
	}

	h.sendText("q")
	screen = h.waitForStableScreenMatching("invalid pane mode key kept mode active", 10*time.Second, func(screen string) bool {
		return h.model.ActiveModeForTest() == "pane" && containsAll(screen, "<%> SPLIT", "<hjkl> FOCUS")
	})
	if strings.Contains(screen, "err:") {
		t.Fatalf("expected invalid pane mode key to be harmless, got:\n%s", screen)
	}

	h.pressEsc()
	h.waitForStableScreenMatching("pane mode exited after esc", 10*time.Second, func(screen string) bool {
		return h.model.ActiveModeForTest() == "" && containsAll(screen, "Ctrl", "▸", "<p> PANE")
	})

	h.sendText("echo INVALID-MODE-RECOVERED")
	h.pressEnter()
	screen = h.waitForStableScreenContains("INVALID-MODE-RECOVERED", 10*time.Second)
	if !strings.Contains(screen, "INVALID-MODE-RECOVERED") {
		t.Fatalf("expected shell input to recover after invalid mode key, got:\n%s", screen)
	}
}

func TestE2ETUI_DirectModeShortcutCanOverrideCurrentModeWithoutSticking(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	h.sendCtrlKey(tea.KeyCtrlP)
	h.waitForStableScreenMatching("pane mode entered", 10*time.Second, func(screen string) bool {
		return h.model.ActiveModeForTest() == "pane" && containsAll(screen, "<%> SPLIT", "<hjkl> FOCUS")
	})

	h.sendCtrlKey(tea.KeyCtrlO)
	screen := h.waitForStableScreenMatching("float mode overrides pane mode", 10*time.Second, func(screen string) bool {
		return h.model.ActiveModeForTest() == "floating" && containsAll(screen, "<[]> Z-ORDER", "<hjkl> MOVE")
	})
	if !containsAll(screen, "Z-ORDER", "MOVE") {
		t.Fatalf("expected floating mode hints after override, got:\n%s", screen)
	}

	h.pressEsc()
	h.waitForStableScreenMatching("back to normal after overridden mode esc", 10*time.Second, func(screen string) bool {
		return h.model.ActiveModeForTest() == "" && containsAll(screen, "Ctrl", "▸", "<p> PANE", "<o> FLOAT")
	})

	h.sendText("echo MODE-OVERRIDE-OK")
	h.pressEnter()
	screen = h.waitForStableScreenContains("MODE-OVERRIDE-OK", 10*time.Second)
	if !strings.Contains(screen, "MODE-OVERRIDE-OK") {
		t.Fatalf("expected shell input after direct mode override flow, got:\n%s", screen)
	}
}

func TestE2ETUI_UnknownCommandPromptDoesNotBlockFollowupInput(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)

	h.sendCtrlKey(tea.KeyCtrlG)
	h.waitForMode("global", "<?> HELP", "<t> TERMINALS")
	h.sendText(":")
	h.waitForStableScreenMatching("command prompt open", 10*time.Second, func(screen string) bool {
		return h.model.PromptKindForTest() == "command" && strings.Contains(screen, "command:")
	})
	h.sendText("definitely-not-a-command")
	h.pressEnter()

	screen := h.waitForStableScreenMatching("unknown command rendered as non-blocking error", 10*time.Second, func(screen string) bool {
		return strings.Contains(screen, "unknown command: definitely-not-a-command") && h.model.PromptKindForTest() == ""
	})
	if h.model.InputBlockedForTest() {
		t.Fatalf("expected unknown command path to release input blocking, got:\n%s", screen)
	}

	h.sendText("echo UNKNOWN-COMMAND-RECOVERED")
	h.pressEnter()
	screen = h.waitForStableScreenContains("UNKNOWN-COMMAND-RECOVERED", 10*time.Second)
	if !strings.Contains(screen, "UNKNOWN-COMMAND-RECOVERED") {
		t.Fatalf("expected shell input after unknown command, got:\n%s", screen)
	}
}

func TestE2ETUI_WorkspaceSwitchRestoresFloatingAndTiledState(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	reuseID := e2eCreateTerminal(t, client, "ws-floating-reuse", nil)

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)
	h.sendText("echo MAIN-TILED")
	h.pressEnter()
	h.waitForStableScreenContains("MAIN-TILED", 10*time.Second)

	h.openFloatingChooser()
	h.sendText(reuseID)
	h.pressEnter()
	screen := h.waitForStableScreenContains("ws-floating-reuse", 10*time.Second)
	if !strings.Contains(screen, "[floating") {
		t.Fatalf("expected floating pane before workspace switch, got:\n%s", screen)
	}

	h.sendPrefixRune('s')
	h.waitForStableScreenContains("Choose Workspace", 10*time.Second)
	h.pressEnter()
	screen = h.waitForStableScreenContains("Choose Terminal", 10*time.Second)
	assertNoChooseWorkspaceResidue(t, screen)

	h.pressEnter()
	h.completeDefaultTerminalCreate()
	h.sendText("echo WS2-TILED")
	h.pressEnter()
	screen = h.waitForStableScreenContains("WS2-TILED", 10*time.Second)

	h.sendPrefixRune('s')
	h.waitForStableScreenContains("Choose Workspace", 10*time.Second)
	h.sendText("main")
	h.pressEnter()
	screen = h.waitForStableScreenContains("MAIN-TILED", 10*time.Second)
	if !strings.Contains(screen, "ws-floating-reuse") || !strings.Contains(screen, "[floating") {
		t.Fatalf("expected switching back to restore main floating+tiled layout, got:\n%s", screen)
	}
}

func TestE2ETUI_WorkspacePickerEscapeClearsOverlayResidue(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)
	h.sendText("echo WS-BASELINE")
	h.pressEnter()
	h.waitForStableScreenContains("WS-BASELINE", 10*time.Second)

	h.sendPrefixRune('s')
	h.waitForStableScreenContains("Choose Workspace", 10*time.Second)
	h.sendText("main")
	h.pressKey(tea.KeyEsc)

	screen := h.waitForStableScreenContains("WS-BASELINE", 10*time.Second)
	assertNoWorkspacePickerResidue(t, screen)

	h.sendText("echo WS-AFTER-ESC")
	h.pressEnter()
	screen = h.waitForStableScreenContains("WS-AFTER-ESC", 10*time.Second)
	assertNoWorkspacePickerResidue(t, screen)
}

func TestE2ETUI_NewWorkspaceViewClearsWorkspacePickerOverlay(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)
	h.sendPrefixRune('s')
	h.waitForStableScreenContains("Choose Workspace", 10*time.Second)
	h.pressEnter()

	screen := h.waitForStableScreenContains("Choose Terminal", 10*time.Second)
	assertNoChooseWorkspaceResidue(t, screen)
}

func TestE2ETUI_WorkspaceDeleteShowsErrorWhenOnlyOneWorkspaceExists(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)
	h.sendCtrlKey(tea.KeyCtrlW)
	h.waitForMode("workspace", "WORKSPACE", "<x> DELETE")
	h.sendText("x")

	screen := h.waitForStableScreenMatching("workspace delete error", 10*time.Second, func(screen string) bool {
		return h.model.ActiveModeForTest() == "" && strings.Contains(screen, "cannot delete the last workspace")
	})
	if !strings.Contains(screen, "ws:main") {
		t.Fatalf("expected current workspace to remain active after delete rejection, got:\n%s", screen)
	}
}

func TestE2ETUI_PickerFilterBackspaceAndEscapeLeaveLayoutUsable(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	_ = e2eCreateTerminal(t, client, "picker-alpha", nil)
	_ = e2eCreateTerminal(t, client, "picker-beta", nil)

	h := newTUIScreenHarness(t, client, 120, 30)
	defer h.Close()

	h.waitForStableScreenContains("ws:main", 10*time.Second)
	h.sendText("echo BASE-STABLE")
	h.pressEnter()
	h.waitForStableScreenContains("BASE-STABLE", 10*time.Second)

	h.sendPrefixRune('f')
	h.waitForStableScreenContains("Terminal Picker", 10*time.Second)
	h.sendText("picker-bet")
	h.pressKey(tea.KeyBackspace)
	h.pressKey(tea.KeyEsc)

	screen := h.waitForStableScreenContains("BASE-STABLE", 10*time.Second)
	if strings.Contains(screen, "Terminal Picker") {
		t.Fatalf("expected picker escape to return to layout, got:\n%s", screen)
	}

	h.sendText("echo AFTER-PICKER")
	h.pressEnter()
	screen = h.waitForStableScreenContains("AFTER-PICKER", 10*time.Second)
	if !strings.Contains(screen, "AFTER-PICKER") {
		t.Fatalf("expected layout to remain usable after picker cancel, got:\n%s", screen)
	}
}

func TestE2ETUI_AgentAPIInputAppearsAndHumanCanContinue(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	terminalID := e2eCreateTerminal(t, client, "ai-agent-live", map[string]string{"role": "ai-agent"})
	agentAttach, err := client.Attach(context.Background(), terminalID, "collaborator")
	if err != nil {
		t.Fatalf("attach agent writer: %v", err)
	}

	h := newTUIScreenHarnessWithConfig(t, client, 120, 30, tui.Config{
		DefaultShell: "/bin/sh",
		Workspace:    "main",
		AttachID:     terminalID,
	})
	defer h.Close()

	h.waitForStableScreenContains("ai-agent-live", 10*time.Second)
	if err := client.Input(context.Background(), agentAttach.Channel, []byte("echo AGENT-WROTE\n")); err != nil {
		t.Fatalf("agent input: %v", err)
	}
	screen := h.waitForStableScreenContains("AGENT-WROTE", 10*time.Second)
	if !strings.Contains(screen, "ai-agent-live") {
		t.Fatalf("expected TUI to keep attached agent viewport visible, got:\n%s", screen)
	}

	h.sendText("echo HUMAN-WROTE")
	h.pressEnter()
	screen = h.waitForStableScreenContains("HUMAN-WROTE", 10*time.Second)
	if !strings.Contains(screen, "AGENT-WROTE") {
		t.Fatalf("expected human and agent writes to share terminal state, got:\n%s", screen)
	}
}

func TestE2ETUI_ReadonlyAgentViewportStillAllowsCtrlC(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	terminalID := e2eCreateTerminal(t, client, "ai-agent-readonly", map[string]string{"role": "ai-agent"})
	agentAttach, err := client.Attach(context.Background(), terminalID, "collaborator")
	if err != nil {
		t.Fatalf("attach agent writer: %v", err)
	}

	h := newTUIScreenHarnessWithConfig(t, client, 120, 30, tui.Config{
		DefaultShell: "/bin/sh",
		Workspace:    "main",
		AttachID:     terminalID,
	})
	defer h.Close()

	h.waitForStableScreenContains("ai-agent-readonly", 10*time.Second)
	if err := client.Input(context.Background(), agentAttach.Channel, []byte("sleep 30\n")); err != nil {
		t.Fatalf("agent input sleep: %v", err)
	}
	h.waitForStableScreenContains("sleep 30", 10*time.Second)

	h.sendPrefixRune('v')
	h.sendText("r")

	h.pressKey(tea.KeyCtrlC)
	screen := h.waitForStableScreenContains("^C", 10*time.Second)
	if !strings.Contains(screen, "ai-agent-readonly") {
		t.Fatalf("expected readonly agent viewport to stay attached after ctrl-c, got:\n%s", screen)
	}

	h.sendText("echo BLOCKED")
	h.pressEnter()
	time.Sleep(300 * time.Millisecond)
	screen, _, _ = h.rec.Snapshot()
	if strings.Contains(screen, "BLOCKED") {
		t.Fatalf("expected readonly mode to block normal input after ctrl-c, got:\n%s", screen)
	}
}

func TestE2ETUI_PaneChromeShowsReadonlyAndAccessStatus(t *testing.T) {
	_, client, cleanup := newE2EClient(t)
	defer cleanup()

	terminalID := e2eCreateTerminal(t, client, "chrome-agent", map[string]string{"role": "ai-agent"})

	h := newTUIScreenHarnessWithConfig(t, client, 120, 30, tui.Config{
		DefaultShell: "/bin/sh",
		Workspace:    "main",
		AttachID:     terminalID,
	})
	defer h.Close()

	h.waitForStableScreenContains("chrome-agent", 10*time.Second)
	screen := h.waitForStableScreenContains("chrome-agent", 10*time.Second)

	h.sendPrefixRune('v')
	h.sendText("r")
	screen = h.waitForStableScreenMatching("readonly chrome badge appears", 10*time.Second, func(screen string) bool {
		return strings.Contains(screen, "ro") && (strings.Contains(screen, "🔒") || strings.Contains(screen, "[ro]"))
	})
	if !strings.Contains(screen, "ro") {
		t.Fatalf("expected readonly chrome badges after toggle, got:\n%s", screen)
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
	switch r {
	case '%', 'x', 'X':
		h.sendCtrlKey(tea.KeyCtrlP)
		h.program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	case 'c', 'n', 'p':
		h.sendCtrlKey(tea.KeyCtrlT)
		h.program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	case 'h', 'j', 'k', 'l':
		h.sendCtrlKey(tea.KeyCtrlP)
		h.program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		time.Sleep(5 * time.Millisecond)
		h.pressEsc()
	case 's':
		h.sendCtrlKey(tea.KeyCtrlW)
		h.program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	case 'o':
		h.sendCtrlKey(tea.KeyCtrlO)
	case 'w':
		h.sendCtrlKey(tea.KeyCtrlO)
		h.program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	case 'W':
		h.sendCtrlKey(tea.KeyCtrlO)
		h.program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
		time.Sleep(5 * time.Millisecond)
		h.pressEsc()
	case ']':
		h.sendCtrlKey(tea.KeyCtrlO)
		h.program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		time.Sleep(5 * time.Millisecond)
		h.pressEsc()
	case 'v':
		h.sendCtrlKey(tea.KeyCtrlV)
	case 'f':
		h.sendCtrlKey(tea.KeyCtrlF)
	case ':', 'd':
		h.sendCtrlKey(tea.KeyCtrlG)
		h.program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	default:
		h.t.Fatalf("unsupported legacy prefix rune mapping: %q", r)
	}
	time.Sleep(5 * time.Millisecond)
}

func (h *tuiScreenHarness) sendCtrlKey(key tea.KeyType) {
	h.t.Helper()
	h.program.Send(tea.KeyMsg{Type: key})
	time.Sleep(10 * time.Millisecond)
}

func (h *tuiScreenHarness) sendPrefixKey(key tea.KeyType) {
	h.t.Helper()
	switch key {
	case tea.KeyTab:
		h.sendCtrlKey(tea.KeyCtrlO)
		h.program.Send(tea.KeyMsg{Type: key})
		time.Sleep(5 * time.Millisecond)
		h.pressEsc()
		return
	case tea.KeyLeft, tea.KeyRight, tea.KeyUp, tea.KeyDown:
		h.sendCtrlKey(tea.KeyCtrlP)
	default:
		h.t.Fatalf("unsupported legacy prefix key mapping: %v", key)
	}
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

func (h *tuiScreenHarness) completeDefaultTerminalCreate() {
	h.t.Helper()
	screen := h.waitForStableScreenMatching("terminal create prompt", 10*time.Second, func(screen string) bool {
		kind := h.model.PromptKindForTest()
		return kind == "create-terminal-name" || kind == "create-terminal-tags" ||
			strings.Contains(screen, "new terminal name:") || strings.Contains(screen, "new terminal tags:") ||
			strings.Contains(screen, "New Terminal")
	})
	if h.model.PromptKindForTest() == "create-terminal-name" || strings.Contains(screen, "new terminal name:") || strings.Contains(screen, "name:  [") {
		h.pressEnter()
		screen = h.waitForStableScreenMatching("terminal tag prompt", 10*time.Second, func(screen string) bool {
			return h.model.PromptKindForTest() == "create-terminal-tags" ||
				strings.Contains(screen, "new terminal tags:") ||
				strings.Contains(screen, "tags:  [")
		})
	}
	if h.model.PromptKindForTest() == "create-terminal-tags" || strings.Contains(screen, "new terminal tags:") || strings.Contains(screen, "tags:  [") {
		h.pressEnter()
	}
	h.waitForStableScreenMatching("terminal create completion", 10*time.Second, func(screen string) bool {
		return h.model.PromptKindForTest() == "" && !h.model.InputBlockedForTest() &&
			!strings.Contains(screen, "new terminal name:") &&
			!strings.Contains(screen, "new terminal tags:")
	})
}

func (h *tuiScreenHarness) mouseDragLeft(fromX, fromY, toX, toY int) {
	h.t.Helper()
	h.program.Send(tea.MouseMsg{X: fromX, Y: fromY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	time.Sleep(10 * time.Millisecond)
	h.program.Send(tea.MouseMsg{X: toX, Y: toY, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	time.Sleep(10 * time.Millisecond)
	h.program.Send(tea.MouseMsg{X: toX, Y: toY, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})
	time.Sleep(10 * time.Millisecond)
}

func (h *tuiScreenHarness) mouseDragLeftPath(points ...imagePoint) {
	h.t.Helper()
	if len(points) < 2 {
		h.t.Fatal("mouseDragLeftPath requires at least two points")
	}
	start := points[0]
	h.program.Send(tea.MouseMsg{X: start.X, Y: start.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	time.Sleep(10 * time.Millisecond)
	for _, pt := range points[1:] {
		h.program.Send(tea.MouseMsg{X: pt.X, Y: pt.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
		time.Sleep(16 * time.Millisecond)
	}
	end := points[len(points)-1]
	h.program.Send(tea.MouseMsg{X: end.X, Y: end.Y, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})
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

func (h *tuiScreenHarness) runCommand(command string) {
	h.t.Helper()
	h.sendCtrlKey(tea.KeyCtrlG)
	time.Sleep(20 * time.Millisecond)
	h.sendText(":")
	h.waitForStableScreenMatching("command prompt open", 10*time.Second, func(screen string) bool {
		return h.model.PromptKindForTest() == "command" && strings.Contains(screen, "command:")
	})
	h.sendText(command)
	h.pressEnter()
}

func (h *tuiScreenHarness) acquireResize() {
	h.t.Helper()
	h.sendCtrlKey(tea.KeyCtrlR)
	h.waitForStableScreenMatching("resize mode", 10*time.Second, func(screen string) bool {
		return h.model.ActiveModeForTest() == "resize" || strings.Contains(screen, "[RESIZE]")
	})
	h.sendText("a")
}

func (h *tuiScreenHarness) setTerminalTag(terminalID, key, value string) {
	h.t.Helper()
	h.model.SetTerminalTagForTest(terminalID, key, value)
}

func (h *tuiScreenHarness) focusTerminalPane(terminalID string) {
	h.t.Helper()
	check := func() bool {
		tab := h.model.CurrentTabForTest()
		if tab != nil {
			if pane := tab.Panes[tab.ActivePaneID]; pane != nil && pane.TerminalID == terminalID {
				return true
			}
		}
		return false
	}
	if check() {
		return
	}
	h.pressEsc()
	time.Sleep(20 * time.Millisecond)
	if check() {
		return
	}
	for i := 0; i < 6; i++ {
		for _, key := range []rune{'h', 'j', 'k', 'l'} {
			h.sendPrefixRune(key)
			time.Sleep(20 * time.Millisecond)
			if check() {
				return
			}
		}
		h.sendPrefixKey(tea.KeyTab)
		time.Sleep(20 * time.Millisecond)
		if check() {
			return
		}
		h.pressEsc()
		time.Sleep(20 * time.Millisecond)
		if check() {
			return
		}
	}
	h.t.Fatalf("failed to focus pane for terminal %q", terminalID)
}

func (h *tuiScreenHarness) openFloatingViewport() {
	h.t.Helper()
	h.openFloatingChooser()
	h.pressEnter()
	h.completeDefaultTerminalCreate()
}

func (h *tuiScreenHarness) waitForMode(mode string, tokens ...string) string {
	h.t.Helper()
	return h.waitForStableScreenMatching("mode "+mode, 10*time.Second, func(screen string) bool {
		if h.model.ActiveModeForTest() != mode {
			return false
		}
		return containsAll(screen, tokens...)
	})
}

func (h *tuiScreenHarness) waitForNormalMode(tokens ...string) string {
	h.t.Helper()
	return h.waitForStableScreenMatching("normal mode", 10*time.Second, func(screen string) bool {
		if h.model.ActiveModeForTest() != "" {
			return false
		}
		return containsAll(screen, tokens...)
	})
}

func (h *tuiScreenHarness) openTerminalManager() {
	h.t.Helper()
	h.sendCtrlKey(tea.KeyCtrlG)
	h.waitForMode("global", "<?> HELP", "<t> TERMINALS")
	h.sendText("t")
}

func (h *tuiScreenHarness) openFloatingChooser() {
	h.t.Helper()
	h.sendPrefixRune('o')
	h.sendText("n")
	h.waitForStableScreenContains("Open Floating Pane", 10*time.Second)
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

func (h *tuiScreenHarness) waitForStableScreenMatching(desc string, timeout time.Duration, match func(string) bool) string {
	h.t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		h.failIfExited()
		screen, idle, writes := h.rec.Snapshot()
		if writes > 0 {
			last = screen
		}
		if idle >= 50*time.Millisecond && match(screen) {
			return screen
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for stable screen matching %q, last screen:\n%s", desc, last)
	return ""
}

func assertNoWorkspacePickerResidue(t *testing.T, screen string) {
	t.Helper()
	for _, needle := range []string{
		"Choose Workspace",
		"Create a new workspace",
		"[Enter] switch or create",
		"query: ",
	} {
		if strings.Contains(screen, needle) {
			t.Fatalf("expected workspace picker overlay to be fully cleared; found %q in screen:\n%s", needle, screen)
		}
	}
}

func assertNoChooseWorkspaceResidue(t *testing.T, screen string) {
	t.Helper()
	for _, needle := range []string{
		"Choose Workspace",
		"Create a new workspace",
		"[Enter] switch or create",
	} {
		if strings.Contains(screen, needle) {
			t.Fatalf("expected workspace picker overlay to be fully cleared; found %q in screen:\n%s", needle, screen)
		}
	}
}

func seedAltScreenForReuseTest(h *tuiScreenHarness) {
	h.t.Helper()
	h.sendText("printf '\\033[?1049h\\033[2J\\033[HCPU 99%%\\r\\nMem 1.2G\\r\\nTasks 42\\r\\nLoad 1.0\\033[H'")
	h.pressEnter()
	screen := h.waitForStableScreenContains("Mem 1.2G", 10*time.Second)
	assertAltScreenReuseBodyVisible(h.t, screen)
}

func assertAltScreenReuseBodyVisible(t *testing.T, screen string) {
	t.Helper()
	for _, needle := range []string{"99%", "Mem 1.2G", "Tasks 42", "Load 1.0"} {
		if !strings.Contains(screen, needle) {
			t.Fatalf("expected alt-screen body to contain %q, got:\n%s", needle, screen)
		}
	}
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

func waitForServerTerminalInfo(t *testing.T, srv *Server, terminalID string, timeout time.Duration, match func(*TerminalInfo) bool) *TerminalInfo {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last *TerminalInfo
	for time.Now().Before(deadline) {
		info, err := srv.Get(context.Background(), terminalID)
		if err == nil {
			last = info
			if match(info) {
				return info
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for terminal %q info match, last info: %#v", terminalID, last)
	return nil
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

type imagePoint struct {
	X int
	Y int
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

func (r *ansiScreenRecorder) WriteCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.writes
}

func (r *ansiScreenRecorder) FramesSince(index int) []ansiFrame {
	r.mu.Lock()
	defer r.mu.Unlock()
	frames := make([]ansiFrame, 0, len(r.frames))
	for _, frame := range r.frames {
		if frame.Index > index {
			frames = append(frames, ansiFrame{
				Index:   frame.Index,
				At:      frame.At,
				Screen:  frame.Screen,
				Payload: append([]byte(nil), frame.Payload...),
			})
		}
	}
	return frames
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

func containsAll(s string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(s, part) {
			return false
		}
	}
	return true
}

func assertFloatingOverlayFrame(t *testing.T, screen, marker string) {
	t.Helper()
	if !strings.Contains(screen, "[floating") {
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
