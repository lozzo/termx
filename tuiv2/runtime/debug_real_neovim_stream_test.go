package runtime

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

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	localvterm "github.com/lozzow/termx/vterm"
)

func TestDebugRealNeovimNeotreeRuntimeStreamParity(t *testing.T) {
	if os.Getenv("TERMX_RUN_NVIM_TRACE") != "1" {
		t.Skip("set TERMX_RUN_NVIM_TRACE=1 to run the real neovim runtime trace")
	}
	if testing.Short() {
		t.Skip("debug trace")
	}
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}

	rt, ctx := newTestRuntime(t)
	repoRoot := debugRepoRoot(t)
	stateHome := filepath.Join(os.TempDir(), "termx-nvim-state-"+strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339Nano), ":", "-"))
	if err := os.MkdirAll(stateHome, 0o755); err != nil {
		t.Fatalf("mkdir state home: %v", err)
	}
	created, err := rt.client.Create(ctx, protocol.CreateParams{
		Command: []string{"nvim", filepath.Join(repoRoot, "termx.go")},
		Name:    "debug-real-nvim",
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

	if _, err := rt.AttachTerminal(ctx, "pane-1", created.TerminalID, "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if err := rt.StartStream(ctx, created.TerminalID); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	waitForRuntimeText(t, ctx, rt, created.TerminalID, "termx.go")
	waitForRuntimeQuiet(t, ctx, rt, created.TerminalID, 300*time.Millisecond)
	assertRuntimeVTermMatchesRender(t, rt, created.TerminalID, "startup")

	if err := rt.ResizePane(ctx, "pane-1", created.TerminalID, 362, 108); err != nil {
		t.Fatalf("resize pane: %v", err)
	}
	waitForRuntimeQuiet(t, ctx, rt, created.TerminalID, 500*time.Millisecond)
	assertRuntimeVTermMatchesRender(t, rt, created.TerminalID, "after-resize")

	sendAndWait := func(label string, seq []byte, waitFor string, quiet time.Duration) {
		t.Helper()
		if err := rt.SendInput(ctx, "pane-1", seq); err != nil {
			t.Fatalf("send %s: %v", label, err)
		}
		if waitFor != "" {
			waitForRuntimeText(t, ctx, rt, created.TerminalID, waitFor)
		}
		waitForRuntimeQuiet(t, ctx, rt, created.TerminalID, quiet)
		assertRuntimeVTermMatchesRender(t, rt, created.TerminalID, label)
	}

	sendAndWait("open-neotree", []byte(":Neotree filesystem left reveal\r"), "File Explorer", 1200*time.Millisecond)
	sendAndWait("focus-code", []byte(":wincmd l\r"), "", 500*time.Millisecond)
	sendAndWait("jump-middle", []byte("500Gzz"), "", 500*time.Millisecond)

	bursts := [][]byte{
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x19}, 10),
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x19}, 10),
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x05}, 24),
	}
	for i, seq := range bursts {
		if err := rt.SendInput(ctx, "pane-1", seq); err != nil {
			t.Fatalf("send scroll-%d: %v", i+1, err)
		}
		time.Sleep(18 * time.Millisecond)
	}
	waitForRuntimeQuiet(t, ctx, rt, created.TerminalID, 800*time.Millisecond)
	assertRuntimeVTermMatchesRender(t, rt, created.TerminalID, "rapid-burst")
}

func TestDebugRealNeovimNeotreeSharedFollowerRuntimeParity(t *testing.T) {
	if os.Getenv("TERMX_RUN_NVIM_TRACE") != "1" {
		t.Skip("set TERMX_RUN_NVIM_TRACE=1 to run the shared neovim runtime trace")
	}
	if testing.Short() {
		t.Skip("debug trace")
	}
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}

	rt, ctx, terminalID := startRealNeovimRuntimeTrace(t)

	sendAndWait := func(label string, seq []byte, waitFor string, quiet time.Duration) {
		t.Helper()
		if err := rt.SendInput(ctx, "pane-1", seq); err != nil {
			t.Fatalf("send %s: %v", label, err)
		}
		if waitFor != "" {
			waitForRuntimeText(t, ctx, rt, terminalID, waitFor)
		}
		waitForRuntimeQuiet(t, ctx, rt, terminalID, quiet)
		assertRuntimeVTermMatchesRender(t, rt, terminalID, label)
	}

	sendAndWait("open-neotree", []byte(":Neotree filesystem left reveal\r"), "File Explorer", 1200*time.Millisecond)
	sendAndWait("focus-code", []byte(":wincmd l\r"), "", 500*time.Millisecond)
	sendAndWait("jump-middle", []byte("500Gzz"), "", 500*time.Millisecond)

	if _, err := rt.AttachTerminal(ctx, "pane-2", terminalID, "collaborator"); err != nil {
		t.Fatalf("attach follower: %v", err)
	}
	if err := rt.StartStream(ctx, terminalID); err != nil {
		t.Fatalf("restart shared stream: %v", err)
	}
	waitForRuntimeQuiet(t, ctx, rt, terminalID, 800*time.Millisecond)
	assertRuntimeVTermMatchesRender(t, rt, terminalID, "after-follower-attach")

	bursts := [][]byte{
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x19}, 10),
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x05}, 24),
		bytes.Repeat([]byte{0x19}, 10),
		bytes.Repeat([]byte{0x05}, 24),
	}
	for i, seq := range bursts {
		if err := rt.ResizePane(ctx, "pane-2", terminalID, 165, 74); err != nil {
			t.Fatalf("follower resize %d: %v", i+1, err)
		}
		if err := rt.SendInput(ctx, "pane-2", seq); err != nil {
			t.Fatalf("follower send %d: %v", i+1, err)
		}
		time.Sleep(18 * time.Millisecond)
	}
	waitForRuntimeQuiet(t, ctx, rt, terminalID, 800*time.Millisecond)
	assertRuntimeVTermMatchesRender(t, rt, terminalID, "shared-follower-burst")
}

func startRealNeovimRuntimeTrace(t *testing.T) (*Runtime, context.Context, string) {
	t.Helper()

	rt, ctx := newTestRuntime(t)
	repoRoot := debugRepoRoot(t)
	stateHome := filepath.Join(os.TempDir(), "termx-nvim-state-"+strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339Nano), ":", "-"))
	if err := os.MkdirAll(stateHome, 0o755); err != nil {
		t.Fatalf("mkdir state home: %v", err)
	}
	created, err := rt.client.Create(ctx, protocol.CreateParams{
		Command: []string{"nvim", filepath.Join(repoRoot, "termx.go")},
		Name:    "debug-real-nvim",
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

	if _, err := rt.AttachTerminal(ctx, "pane-1", created.TerminalID, "collaborator"); err != nil {
		t.Fatalf("attach terminal: %v", err)
	}
	if err := rt.StartStream(ctx, created.TerminalID); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	waitForRuntimeText(t, ctx, rt, created.TerminalID, "termx.go")
	waitForRuntimeQuiet(t, ctx, rt, created.TerminalID, 300*time.Millisecond)
	assertRuntimeVTermMatchesRender(t, rt, created.TerminalID, "startup")

	if err := rt.ResizePane(ctx, "pane-1", created.TerminalID, 362, 108); err != nil {
		t.Fatalf("resize pane: %v", err)
	}
	waitForRuntimeQuiet(t, ctx, rt, created.TerminalID, 500*time.Millisecond)
	assertRuntimeVTermMatchesRender(t, rt, created.TerminalID, "after-resize")

	return rt, ctx, created.TerminalID
}

func debugRepoRoot(t *testing.T) string {
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

func runtimeVTermForDebug(t *testing.T, rt *Runtime, terminalID string) *localvterm.VTerm {
	t.Helper()
	if rt == nil || rt.Registry() == nil {
		t.Fatal("runtime unavailable")
	}
	terminal := rt.Registry().Get(terminalID)
	if terminal == nil || terminal.VTerm == nil {
		t.Fatalf("terminal %q has no vterm", terminalID)
	}
	vt, ok := terminal.VTerm.(*localvterm.VTerm)
	if !ok {
		t.Fatalf("terminal %q vterm has unexpected type %T", terminalID, terminal.VTerm)
	}
	return vt
}

func waitForRuntimeText(t *testing.T, ctx context.Context, rt *Runtime, terminalID, want string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if vtermContains(runtimeVTermForDebug(t, rt, terminalID), want) {
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

func waitForRuntimeQuiet(t *testing.T, ctx context.Context, rt *Runtime, terminalID string, quiet time.Duration) {
	t.Helper()
	timer := time.NewTimer(quiet)
	defer timer.Stop()
	last := runtimeScreenDigest(runtimeVTermForDebug(t, rt, terminalID))
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("context expired waiting for terminal %q quiet", terminalID)
		case <-time.After(40 * time.Millisecond):
			current := runtimeScreenDigest(runtimeVTermForDebug(t, rt, terminalID))
			if current != last {
				last = current
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(quiet)
				continue
			}
		case <-timer.C:
			return
		}
	}
}

func assertRuntimeVTermMatchesRender(t *testing.T, rt *Runtime, terminalID, phase string) {
	t.Helper()
	vt := runtimeVTermForDebug(t, rt, terminalID)
	screen := vt.ScreenContent()
	rendered := vt.RenderLines()
	if len(rendered) == 0 {
		return
	}
	width, _ := vt.Size()
	plain := runtimeComparableScreenLines(screen, width)
	limit := len(plain)
	if len(rendered) < limit {
		limit = len(rendered)
	}
	for i := 0; i < limit; i++ {
		row := strings.TrimRight(plain[i], " ")
		line := strings.TrimRight(xansi.Strip(rendered[i]), " ")
		if row == line {
			continue
		}
		t.Fatalf("phase %s terminal %s mismatch row %d\nscreen=%q\nrender=%q", phase, terminalID, i, row, line)
	}
}

func runtimeComparableScreenLines(screen localvterm.ScreenData, width int) []string {
	out := make([]string, len(screen.Cells))
	for i, row := range screen.Cells {
		out[i] = runtimeComparableRow(row, width)
	}
	return out
}

func runtimeComparableRow(row []localvterm.Cell, width int) string {
	if width <= 0 {
		width = len(row)
	}
	var b strings.Builder
	col := 0
	for i := 0; i < len(row) && col < width; i++ {
		cell := row[i]
		if cell.Content == "" && cell.Width == 0 {
			continue
		}
		content := cell.Content
		if content == "" {
			content = " "
		}
		w := cell.Width
		if w <= 0 {
			w = xansi.StringWidth(content)
			if w <= 0 {
				w = 1
			}
		}
		if col+w > width {
			break
		}
		b.WriteString(content)
		col += w
	}
	return b.String()
}

func runtimeScreenDigest(vt *localvterm.VTerm) uint64 {
	if vt == nil {
		return 0
	}
	screen := vt.ScreenContent()
	width, _ := vt.Size()
	h := fnv.New64a()
	for _, line := range runtimeComparableScreenLines(screen, width) {
		_, _ = h.Write([]byte(line))
		_, _ = h.Write([]byte{'\n'})
	}
	return h.Sum64()
}
