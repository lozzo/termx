package app

import (
	"bytes"
	"context"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/workbench"
	localvterm "github.com/lozzow/termx/vterm"
)

func TestDebugRealNeovimSharedPaneModelParity(t *testing.T) {
	if os.Getenv("TERMX_RUN_NVIM_TRACE") != "1" {
		t.Skip("set TERMX_RUN_NVIM_TRACE=1 to run the shared neovim model trace")
	}
	if testing.Short() {
		t.Skip("debug trace")
	}
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}

	env := newRealMouseE2EEnv(t)
	model := env.model
	model.width = 365
	model.height = 110
	_, cmd := model.Update(tea.WindowSizeMsg{Width: model.width, Height: model.height})
	e2eDrainSkippingPrefixTimeout(t, model, cmd)

	repoRoot := debugRealRepoRoot(t)
	stateHome := filepath.Join(os.TempDir(), "termx-model-nvim-state-"+strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339Nano), ":", "-"))
	if err := os.MkdirAll(stateHome, 0o755); err != nil {
		t.Fatalf("mkdir state home: %v", err)
	}

	client := model.runtime.Client()
	if client == nil {
		t.Fatal("expected runtime client")
	}
	created, err := client.Create(env.ctx, protocol.CreateParams{
		Command: []string{"nvim", filepath.Join(repoRoot, "termx.go")},
		Name:    "debug-model-nvim",
		Dir:     repoRoot,
		Env: []string{
			"TERM=xterm-256color",
			"COLORTERM=truecolor",
			"XDG_STATE_HOME=" + stateHome,
		},
		Size: protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("create terminal: %v", err)
	}

	e2eDrainSkippingPrefixTimeout(t, model, model.attachInitialTerminalCmd(created.TerminalID))
	waitForModelRuntimeText(t, env.ctx, model, created.TerminalID, "termx.go")
	waitForModelRuntimeQuiet(t, env.ctx, model, created.TerminalID, 400*time.Millisecond)
	assertModelRuntimeVTermMatchesRender(t, model, created.TerminalID, "startup")

	pane1 := model.workbench.ActivePane()
	if pane1 == nil {
		t.Fatal("expected active pane")
	}
	sendAndWait := func(label, paneID string, seq []byte, waitFor string, quiet time.Duration) {
		t.Helper()
		cmd := model.handleTerminalInput(inputTerminal(paneID, seq))
		e2eDrainSkippingPrefixTimeout(t, model, cmd)
		if waitFor != "" {
			waitForModelRuntimeText(t, env.ctx, model, created.TerminalID, waitFor)
		}
		waitForModelRuntimeQuiet(t, env.ctx, model, created.TerminalID, quiet)
		assertModelRuntimeVTermMatchesRender(t, model, created.TerminalID, label)
	}

	sendAndWait("open-neotree", pane1.ID, []byte(":Neotree filesystem left reveal\r"), "File Explorer", 1200*time.Millisecond)
	sendAndWait("focus-code", pane1.ID, []byte(":wincmd l\r"), "", 500*time.Millisecond)
	sendAndWait("jump-middle", pane1.ID, []byte("500Gzz"), "", 500*time.Millisecond)

	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := model.workbench.SplitPane(tab.ID, pane1.ID, "pane-2", workbench.SplitVertical); err != nil {
		t.Fatalf("split pane: %v", err)
	}
	e2eDrainSkippingPrefixTimeout(t, model, model.attachPaneTerminalCmd(tab.ID, "pane-2", created.TerminalID))
	waitForModelRuntimeQuiet(t, env.ctx, model, created.TerminalID, 800*time.Millisecond)
	assertModelRuntimeVTermMatchesRender(t, model, created.TerminalID, "after-share-attach")

	bursts := [][]byte{
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x19}, 10),
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x19}, 10),
		bytes.Repeat([]byte{0x05}, 24),
	}
	for _, seq := range bursts {
		cmd := model.handleTerminalInput(inputTerminal("pane-2", seq))
		e2eDrainSkippingPrefixTimeout(t, model, cmd)
		time.Sleep(18 * time.Millisecond)
	}
	waitForModelRuntimeQuiet(t, env.ctx, model, created.TerminalID, 800*time.Millisecond)
	assertModelRuntimeVTermMatchesRender(t, model, created.TerminalID, "shared-follower-burst")
}

func TestDebugRealNeovimSharedFloatingModelParity(t *testing.T) {
	if os.Getenv("TERMX_RUN_NVIM_TRACE") != "1" {
		t.Skip("set TERMX_RUN_NVIM_TRACE=1 to run the shared floating neovim model trace")
	}
	if testing.Short() {
		t.Skip("debug trace")
	}
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}

	env := newRealMouseE2EEnv(t)
	model := env.model
	model.width = 365
	model.height = 110
	_, cmd := model.Update(tea.WindowSizeMsg{Width: model.width, Height: model.height})
	e2eDrainSkippingPrefixTimeout(t, model, cmd)

	repoRoot := debugRealRepoRoot(t)
	stateHome := filepath.Join(os.TempDir(), "termx-model-float-nvim-state-"+strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339Nano), ":", "-"))
	if err := os.MkdirAll(stateHome, 0o755); err != nil {
		t.Fatalf("mkdir state home: %v", err)
	}

	client := model.runtime.Client()
	if client == nil {
		t.Fatal("expected runtime client")
	}
	created, err := client.Create(env.ctx, protocol.CreateParams{
		Command: []string{"nvim", filepath.Join(repoRoot, "termx.go")},
		Name:    "debug-model-nvim-float",
		Dir:     repoRoot,
		Env: []string{
			"TERM=xterm-256color",
			"COLORTERM=truecolor",
			"XDG_STATE_HOME=" + stateHome,
		},
		Size: protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("create terminal: %v", err)
	}

	e2eDrainSkippingPrefixTimeout(t, model, model.attachInitialTerminalCmd(created.TerminalID))
	waitForModelRuntimeText(t, env.ctx, model, created.TerminalID, "termx.go")
	waitForModelRuntimeQuiet(t, env.ctx, model, created.TerminalID, 400*time.Millisecond)
	assertModelRuntimeVTermMatchesRender(t, model, created.TerminalID, "startup")

	pane1 := model.workbench.ActivePane()
	if pane1 == nil {
		t.Fatal("expected active pane")
	}
	sendAndWait := func(label, paneID string, seq []byte, waitFor string, quiet time.Duration) {
		t.Helper()
		cmd := model.handleTerminalInput(inputTerminal(paneID, seq))
		e2eDrainSkippingPrefixTimeout(t, model, cmd)
		if waitFor != "" {
			waitForModelRuntimeText(t, env.ctx, model, created.TerminalID, waitFor)
		}
		waitForModelRuntimeQuiet(t, env.ctx, model, created.TerminalID, quiet)
		assertModelRuntimeVTermMatchesRender(t, model, created.TerminalID, label)
	}

	sendAndWait("open-neotree", pane1.ID, []byte(":Neotree filesystem left reveal\r"), "File Explorer", 1200*time.Millisecond)
	sendAndWait("focus-code", pane1.ID, []byte(":wincmd l\r"), "", 500*time.Millisecond)
	sendAndWait("jump-middle", pane1.ID, []byte("500Gzz"), "", 500*time.Millisecond)

	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 96, Y: 10, W: 168, H: 76}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}
	e2eDrainSkippingPrefixTimeout(t, model, model.attachPaneTerminalCmd(tab.ID, "float-1", created.TerminalID))
	if err := model.workbench.FocusPane(tab.ID, "float-1"); err != nil {
		t.Fatalf("focus floating pane: %v", err)
	}
	model.workbench.ReorderFloatingPane(tab.ID, "float-1", true)
	model.render.Invalidate()

	waitForModelRuntimeQuiet(t, env.ctx, model, created.TerminalID, 800*time.Millisecond)
	assertModelRuntimeVTermMatchesRender(t, model, created.TerminalID, "after-floating-share-attach")

	bursts := [][]byte{
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x19}, 10),
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x19}, 10),
		bytes.Repeat([]byte{0x05}, 24),
	}
	for _, seq := range bursts {
		cmd := model.handleTerminalInput(inputTerminal("float-1", seq))
		e2eDrainSkippingPrefixTimeout(t, model, cmd)
		time.Sleep(18 * time.Millisecond)
	}
	waitForModelRuntimeQuiet(t, env.ctx, model, created.TerminalID, 800*time.Millisecond)
	assertModelRuntimeVTermMatchesRender(t, model, created.TerminalID, "shared-floating-burst")
}

func TestDebugRealNeovimFloatingReattachModelParity(t *testing.T) {
	if os.Getenv("TERMX_RUN_NVIM_TRACE") != "1" {
		t.Skip("set TERMX_RUN_NVIM_TRACE=1 to run the floating reattach neovim model trace")
	}
	if testing.Short() {
		t.Skip("debug trace")
	}
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}

	env := newRealMouseE2EEnv(t)
	model := env.model
	model.width = 365
	model.height = 110
	_, cmd := model.Update(tea.WindowSizeMsg{Width: model.width, Height: model.height})
	e2eDrainSkippingPrefixTimeout(t, model, cmd)

	repoRoot := debugRealRepoRoot(t)
	stateHomeA := filepath.Join(os.TempDir(), "termx-model-reattach-a-"+strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339Nano), ":", "-"))
	stateHomeB := filepath.Join(os.TempDir(), "termx-model-reattach-b-"+strings.ReplaceAll(time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano), ":", "-"))
	for _, dir := range []string{stateHomeA, stateHomeB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir state home %s: %v", dir, err)
		}
	}

	client := model.runtime.Client()
	if client == nil {
		t.Fatal("expected runtime client")
	}
	createNvim := func(name, stateHome string) string {
		t.Helper()
		created, err := client.Create(env.ctx, protocol.CreateParams{
			Command: []string{"nvim", filepath.Join(repoRoot, "termx.go")},
			Name:    name,
			Dir:     repoRoot,
			Env: []string{
				"TERM=xterm-256color",
				"COLORTERM=truecolor",
				"XDG_STATE_HOME=" + stateHome,
			},
			Size: protocol.Size{Cols: 80, Rows: 24},
		})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return created.TerminalID
	}

	term1 := createNvim("debug-model-reattach-a", stateHomeA)
	term2 := createNvim("debug-model-reattach-b", stateHomeB)

	e2eDrainSkippingPrefixTimeout(t, model, model.attachInitialTerminalCmd(term1))
	waitForModelRuntimeText(t, env.ctx, model, term1, "termx.go")
	waitForModelRuntimeQuiet(t, env.ctx, model, term1, 400*time.Millisecond)
	assertModelRuntimeVTermMatchesRender(t, model, term1, "term1-startup")

	pane1 := model.workbench.ActivePane()
	if pane1 == nil {
		t.Fatal("expected active pane")
	}
	sendAndWaitTerm := func(label, paneID, terminalID string, seq []byte, waitFor string, quiet time.Duration) {
		t.Helper()
		cmd := model.handleTerminalInput(inputTerminal(paneID, seq))
		e2eDrainSkippingPrefixTimeout(t, model, cmd)
		if waitFor != "" {
			waitForModelRuntimeText(t, env.ctx, model, terminalID, waitFor)
		}
		waitForModelRuntimeQuiet(t, env.ctx, model, terminalID, quiet)
		assertModelRuntimeVTermMatchesRender(t, model, terminalID, label)
	}

	sendAndWaitTerm("term1-open-neotree", pane1.ID, term1, []byte(":Neotree filesystem left reveal\r"), "File Explorer", 1200*time.Millisecond)
	sendAndWaitTerm("term1-focus-code", pane1.ID, term1, []byte(":wincmd l\r"), "", 500*time.Millisecond)
	sendAndWaitTerm("term1-jump-middle", pane1.ID, term1, []byte("500Gzz"), "", 500*time.Millisecond)

	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 96, Y: 10, W: 168, H: 76}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}
	e2eDrainSkippingPrefixTimeout(t, model, model.attachPaneTerminalCmd(tab.ID, "float-1", term2))
	if err := model.workbench.FocusPane(tab.ID, "float-1"); err != nil {
		t.Fatalf("focus floating pane: %v", err)
	}
	model.workbench.ReorderFloatingPane(tab.ID, "float-1", true)
	model.render.Invalidate()

	waitForModelRuntimeText(t, env.ctx, model, term2, "termx.go")
	waitForModelRuntimeQuiet(t, env.ctx, model, term2, 800*time.Millisecond)
	assertModelRuntimeVTermMatchesRender(t, model, term2, "term2-startup")
	sendAndWaitTerm("term2-open-neotree", "float-1", term2, []byte(":Neotree filesystem left reveal\r"), "File Explorer", 1200*time.Millisecond)
	sendAndWaitTerm("term2-focus-code", "float-1", term2, []byte(":wincmd l\r"), "", 500*time.Millisecond)
	sendAndWaitTerm("term2-jump-middle", "float-1", term2, []byte("500Gzz"), "", 500*time.Millisecond)

	e2eDrainSkippingPrefixTimeout(t, model, model.attachPaneTerminalCmd(tab.ID, "float-1", term1))
	if err := model.workbench.FocusPane(tab.ID, "float-1"); err != nil {
		t.Fatalf("refocus floating pane: %v", err)
	}
	model.workbench.ReorderFloatingPane(tab.ID, "float-1", true)
	model.render.Invalidate()
	waitForModelRuntimeQuiet(t, env.ctx, model, term1, 800*time.Millisecond)
	assertModelRuntimeVTermMatchesRender(t, model, term1, "term1-after-reattach")

	bursts := [][]byte{
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x19}, 10),
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x19}, 10),
		bytes.Repeat([]byte{0x05}, 24),
	}
	for _, seq := range bursts {
		cmd := model.handleTerminalInput(inputTerminal("float-1", seq))
		e2eDrainSkippingPrefixTimeout(t, model, cmd)
		time.Sleep(18 * time.Millisecond)
	}
	waitForModelRuntimeQuiet(t, env.ctx, model, term1, 800*time.Millisecond)
	assertModelRuntimeVTermMatchesRender(t, model, term1, "term1-reattach-burst")
}

func TestDebugRealNeovimSharedFloatingWheelModelParity(t *testing.T) {
	if os.Getenv("TERMX_RUN_NVIM_TRACE") != "1" {
		t.Skip("set TERMX_RUN_NVIM_TRACE=1 to run the shared floating wheel neovim trace")
	}
	if testing.Short() {
		t.Skip("debug trace")
	}
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}

	env := newRealMouseE2EEnv(t)
	model := env.model
	model.width = 365
	model.height = 110
	_, cmd := model.Update(tea.WindowSizeMsg{Width: model.width, Height: model.height})
	e2eDrainSkippingPrefixTimeout(t, model, cmd)

	repoRoot := debugRealRepoRoot(t)
	stateHome := filepath.Join(os.TempDir(), "termx-model-wheel-nvim-state-"+strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339Nano), ":", "-"))
	if err := os.MkdirAll(stateHome, 0o755); err != nil {
		t.Fatalf("mkdir state home: %v", err)
	}

	client := model.runtime.Client()
	if client == nil {
		t.Fatal("expected runtime client")
	}
	created, err := client.Create(env.ctx, protocol.CreateParams{
		Command: []string{"nvim", filepath.Join(repoRoot, "termx.go")},
		Name:    "debug-model-wheel-nvim",
		Dir:     repoRoot,
		Env: []string{
			"TERM=xterm-256color",
			"COLORTERM=truecolor",
			"XDG_STATE_HOME=" + stateHome,
		},
		Size: protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("create terminal: %v", err)
	}

	e2eDrainSkippingPrefixTimeout(t, model, model.attachInitialTerminalCmd(created.TerminalID))
	waitForModelRuntimeText(t, env.ctx, model, created.TerminalID, "termx.go")
	waitForModelRuntimeQuiet(t, env.ctx, model, created.TerminalID, 400*time.Millisecond)
	assertModelRuntimeVTermMatchesRender(t, model, created.TerminalID, "startup")

	pane1 := model.workbench.ActivePane()
	if pane1 == nil {
		t.Fatal("expected active pane")
	}
	sendAndWait := func(label, paneID string, seq []byte, waitFor string, quiet time.Duration) {
		t.Helper()
		cmd := model.handleTerminalInput(inputTerminal(paneID, seq))
		e2eDrainSkippingPrefixTimeout(t, model, cmd)
		if waitFor != "" {
			waitForModelRuntimeText(t, env.ctx, model, created.TerminalID, waitFor)
		}
		waitForModelRuntimeQuiet(t, env.ctx, model, created.TerminalID, quiet)
		assertModelRuntimeVTermMatchesRender(t, model, created.TerminalID, label)
	}

	sendAndWait("open-neotree", pane1.ID, []byte(":Neotree filesystem left reveal\r"), "File Explorer", 1200*time.Millisecond)
	sendAndWait("focus-code", pane1.ID, []byte(":wincmd l\r"), "", 500*time.Millisecond)
	sendAndWait("jump-middle", pane1.ID, []byte("500Gzz"), "", 500*time.Millisecond)

	tab := model.workbench.CurrentTab()
	if tab == nil {
		t.Fatal("expected current tab")
	}
	if err := model.workbench.CreateFloatingPane(tab.ID, "float-1", workbench.Rect{X: 96, Y: 10, W: 168, H: 76}); err != nil {
		t.Fatalf("create floating pane: %v", err)
	}
	e2eDrainSkippingPrefixTimeout(t, model, model.attachPaneTerminalCmd(tab.ID, "float-1", created.TerminalID))
	if err := model.workbench.FocusPane(tab.ID, "float-1"); err != nil {
		t.Fatalf("focus floating pane: %v", err)
	}
	model.workbench.ReorderFloatingPane(tab.ID, "float-1", true)
	model.render.Invalidate()

	waitForModelRuntimeQuiet(t, env.ctx, model, created.TerminalID, 800*time.Millisecond)
	assertModelRuntimeVTermMatchesRender(t, model, created.TerminalID, "after-floating-share-attach")

	x, y := floatingPaneContentScreenPoint(t, model, "float-1")
	msg := tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress}
	for i := 0; i < 10; i++ {
		cmd := model.handleMouseWheelRepeated(msg, 6)
		e2eDrainSkippingPrefixTimeout(t, model, cmd)
		time.Sleep(18 * time.Millisecond)
	}
	waitForModelRuntimeQuiet(t, env.ctx, model, created.TerminalID, 800*time.Millisecond)
	assertModelRuntimeVTermMatchesRender(t, model, created.TerminalID, "shared-floating-wheel")
}

func inputTerminal(paneID string, data []byte) input.TerminalInput {
	return input.TerminalInput{PaneID: paneID, Data: data}
}

func debugRealRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("pwd: %v", err)
	}
	root := cwd
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatalf("could not locate repo root from %s", cwd)
		}
		root = parent
	}
}

func modelRuntimeVTerm(t *testing.T, model *Model, terminalID string) *localvterm.VTerm {
	t.Helper()
	if model == nil || model.runtime == nil || model.runtime.Registry() == nil {
		t.Fatal("runtime unavailable")
	}
	terminal := model.runtime.Registry().Get(terminalID)
	if terminal == nil || terminal.VTerm == nil {
		t.Fatalf("terminal %q has no vterm", terminalID)
	}
	vt, ok := terminal.VTerm.(*localvterm.VTerm)
	if !ok {
		t.Fatalf("terminal %q vterm has unexpected type %T", terminalID, terminal.VTerm)
	}
	return vt
}

func floatingPaneContentScreenPoint(t *testing.T, model *Model, paneID string) (int, int) {
	t.Helper()
	if model == nil || model.workbench == nil {
		t.Fatal("model unavailable")
	}
	visible := model.workbench.VisibleWithSize(model.bodyRect())
	if visible == nil {
		t.Fatal("expected visible state")
	}
	for _, pane := range visible.FloatingPanes {
		if pane.ID != paneID {
			continue
		}
		contentRect, ok := paneContentRectForVisible(pane)
		if !ok {
			t.Fatalf("floating pane %q has no content rect", paneID)
		}
		return contentRect.X + 2, e2eScreenYForBodyY(model, contentRect.Y+2)
	}
	t.Fatalf("floating pane %q not visible", paneID)
	return 0, 0
}

func waitForModelRuntimeText(t *testing.T, ctx context.Context, model *Model, terminalID, want string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if vtermContainsText(modelRuntimeVTerm(t, model, terminalID), want) {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("context expired waiting for %q in terminal %q", want, terminalID)
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatalf("timeout waiting for %q in terminal %q", want, terminalID)
}

func waitForModelRuntimeQuiet(t *testing.T, ctx context.Context, model *Model, terminalID string, quiet time.Duration) {
	t.Helper()
	timer := time.NewTimer(quiet)
	defer timer.Stop()
	last := modelRuntimeDigest(modelRuntimeVTerm(t, model, terminalID))
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("context expired waiting for terminal %q quiet", terminalID)
		case <-time.After(40 * time.Millisecond):
			current := modelRuntimeDigest(modelRuntimeVTerm(t, model, terminalID))
			if current != last {
				last = current
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(quiet)
			}
		case <-timer.C:
			return
		}
	}
}

func assertModelRuntimeVTermMatchesRender(t *testing.T, model *Model, terminalID, phase string) {
	t.Helper()
	assertVTermMatchesRender(modelRuntimeVTerm(t, model, terminalID), phase)
}

func vtermContainsText(vt *localvterm.VTerm, want string) bool {
	if vt == nil || want == "" {
		return false
	}
	screen := vt.ScreenContent()
	width, _ := vt.Size()
	for _, line := range comparableVTermScreenLines(screen, width) {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}

func modelRuntimeDigest(vt *localvterm.VTerm) uint64 {
	if vt == nil {
		return 0
	}
	screen := vt.ScreenContent()
	width, _ := vt.Size()
	h := fnv.New64a()
	for _, line := range comparableVTermScreenLines(screen, width) {
		_, _ = h.Write([]byte(line))
		_, _ = h.Write([]byte{'\n'})
	}
	return h.Sum64()
}
