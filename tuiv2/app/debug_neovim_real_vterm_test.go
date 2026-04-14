package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	xansi "github.com/charmbracelet/x/ansi"
	creackpty "github.com/creack/pty"
	localvterm "github.com/lozzow/termx/vterm"
)

func TestDebugRealNeovimNeotreeVTermParity(t *testing.T) {
	if os.Getenv("TERMX_RUN_NVIM_TRACE") != "1" {
		t.Skip("set TERMX_RUN_NVIM_TRACE=1 to run the real neovim/neotree vterm trace")
	}
	if testing.Short() {
		t.Skip("debug trace")
	}
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}

	h := startRealNeovimHarness(t)
	defer h.Close(t)

	if err := h.pumpUntilQuiet("startup", 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	waitForPTYText(t, h.ctx, h.recorder, "termx.go")

	h.sendAndPump(t, "open-neotree", []byte(":Neotree filesystem left reveal\r"), 1200*time.Millisecond)
	waitForPTYText(t, h.ctx, h.recorder, "File Explorer")
	h.sendAndPump(t, "focus-code", []byte(":wincmd l\r"), 500*time.Millisecond)
	h.sendAndPump(t, "jump-middle", []byte("500Gzz"), 500*time.Millisecond)

	for i, seq := range [][]byte{
		bytes.Repeat([]byte{0x05}, 18),
		bytes.Repeat([]byte{0x05}, 18),
		bytes.Repeat([]byte{0x19}, 8),
		bytes.Repeat([]byte{0x05}, 18),
		bytes.Repeat([]byte{0x05}, 18),
	} {
		h.sendAndPump(t, fmt.Sprintf("scroll-%d", i+1), seq, 350*time.Millisecond)
	}

	t.Fatalf("did not reproduce a vterm parity mismatch after scripted neovim/neotree actions")
}

func TestDebugRealNeovimNeotreeSnapshotRestoreParity(t *testing.T) {
	if os.Getenv("TERMX_RUN_NVIM_TRACE") != "1" {
		t.Skip("set TERMX_RUN_NVIM_TRACE=1 to run the real neovim/neotree snapshot trace")
	}
	if testing.Short() {
		t.Skip("debug trace")
	}
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}

	h := startRealNeovimHarness(t)
	defer h.Close(t)

	if err := h.pumpUntilQuiet("startup", 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	waitForPTYText(t, h.ctx, h.recorder, "termx.go")
	h.sendAndPump(t, "open-neotree", []byte(":Neotree filesystem left reveal\r"), 1200*time.Millisecond)
	waitForPTYText(t, h.ctx, h.recorder, "File Explorer")
	h.sendAndPump(t, "focus-code", []byte(":wincmd l\r"), 500*time.Millisecond)
	h.sendAndPump(t, "jump-middle", []byte("500Gzz"), 500*time.Millisecond)

	state := h.vt.SnapshotRenderState()
	restored := localvterm.New(state.Cols, state.Rows, 0, nil)
	restored.LoadSnapshotWithMetadata(
		state.Scrollback,
		state.ScrollbackTimestamps,
		state.ScrollbackRowKinds,
		state.Screen,
		state.ScreenTimestamps,
		state.ScreenRowKinds,
		state.Cursor,
		state.Modes,
	)
	if err := assertVTermsEqual(h.vt, restored, "after-restore"); err != nil {
		t.Fatal(err)
	}

	sendAndPumpBoth := func(label string, seq []byte, quiet time.Duration) {
		t.Helper()
		if _, err := h.ptmx.Write(seq); err != nil {
			t.Fatalf("write %s: %v", label, err)
		}
		if err := pumpPTYIntoVTermsUntilQuiet(h.ctx, h.vt, restored, h.chunkc, quiet, label); err != nil {
			t.Fatal(err)
		}
	}

	for i, seq := range [][]byte{
		bytes.Repeat([]byte{0x05}, 18),
		bytes.Repeat([]byte{0x05}, 18),
		bytes.Repeat([]byte{0x19}, 8),
		bytes.Repeat([]byte{0x05}, 18),
		bytes.Repeat([]byte{0x05}, 18),
	} {
		sendAndPumpBoth(fmt.Sprintf("scroll-%d", i+1), seq, 350*time.Millisecond)
	}

	t.Fatalf("did not reproduce a snapshot-restore parity mismatch after scripted neovim/neotree actions")
}

func TestDebugRealNeovimNeotreeReplayBootstrapParity(t *testing.T) {
	if os.Getenv("TERMX_RUN_NVIM_TRACE") != "1" {
		t.Skip("set TERMX_RUN_NVIM_TRACE=1 to run the real neovim/neotree replay trace")
	}
	if testing.Short() {
		t.Skip("debug trace")
	}
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}

	h := startRealNeovimHarness(t)
	defer h.Close(t)

	if err := h.pumpUntilQuiet("startup", 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	waitForPTYText(t, h.ctx, h.recorder, "termx.go")
	h.sendAndPump(t, "open-neotree", []byte(":Neotree filesystem left reveal\r"), 1200*time.Millisecond)
	waitForPTYText(t, h.ctx, h.recorder, "File Explorer")
	h.sendAndPump(t, "focus-code", []byte(":wincmd l\r"), 500*time.Millisecond)
	h.sendAndPump(t, "jump-middle", []byte("500Gzz"), 500*time.Millisecond)

	width := screenWidth(h.vt)
	_, height := h.vt.Size()
	replayed := localvterm.New(width, height, 0, nil)
	replay := h.vt.EncodeReplay(500)
	if _, err := replayed.Write(replay); err != nil {
		t.Fatalf("bootstrap replay write failed: %v", err)
	}
	if err := assertVTermsEqual(h.vt, replayed, "after-replay-bootstrap"); err != nil {
		t.Fatal(err)
	}

	sendAndPumpBoth := func(label string, seq []byte, quiet time.Duration) {
		t.Helper()
		if _, err := h.ptmx.Write(seq); err != nil {
			t.Fatalf("write %s: %v", label, err)
		}
		if err := pumpPTYIntoVTermsUntilQuiet(h.ctx, h.vt, replayed, h.chunkc, quiet, label); err != nil {
			t.Fatal(err)
		}
	}

	for i, seq := range [][]byte{
		bytes.Repeat([]byte{0x05}, 18),
		bytes.Repeat([]byte{0x05}, 18),
		bytes.Repeat([]byte{0x19}, 8),
		bytes.Repeat([]byte{0x05}, 18),
		bytes.Repeat([]byte{0x05}, 18),
	} {
		sendAndPumpBoth(fmt.Sprintf("scroll-%d", i+1), seq, 350*time.Millisecond)
	}

	t.Fatalf("did not reproduce a replay-bootstrap parity mismatch after scripted neovim/neotree actions")
}

type realNeovimHarness struct {
	ctx      context.Context
	cancel   context.CancelFunc
	cmd      *exec.Cmd
	ptmx     *os.File
	recorder *ptyOutputRecorder
	chunkc   chan []byte
	vt       *localvterm.VTerm
}

func startRealNeovimHarness(t *testing.T) *realNeovimHarness {
	t.Helper()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("pwd: %v", err)
	}
	repoRoot := cwd
	for {
		if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(repoRoot)
		if parent == repoRoot {
			t.Fatalf("could not locate repo root from %s", cwd)
		}
		repoRoot = parent
	}
	targetFile := filepath.Join(repoRoot, "termx.go")
	if _, err := os.Stat(targetFile); err != nil {
		t.Fatalf("stat target file: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	cmd := exec.CommandContext(ctx, "nvim", targetFile)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"XDG_STATE_HOME="+t.TempDir(),
	)

	ptmx, err := creackpty.StartWithSize(cmd, &creackpty.Winsize{Cols: 365, Rows: 110})
	if err != nil {
		cancel()
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("start nvim pty: %v", err)
	}

	recorder := &ptyOutputRecorder{eventc: make(chan struct{}, 4096)}
	chunkc := make(chan []byte, 4096)
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				chunk := append([]byte(nil), buf[:n]...)
				recorder.Append(string(chunk))
				select {
				case chunkc <- chunk:
				default:
					panic("real neovim chunk queue full")
				}
			}
			if err != nil {
				close(chunkc)
				return
			}
		}
	}()

	return &realNeovimHarness{
		ctx:      ctx,
		cancel:   cancel,
		cmd:      cmd,
		ptmx:     ptmx,
		recorder: recorder,
		chunkc:   chunkc,
		vt:       localvterm.New(365, 110, 0, nil),
	}
}

func (h *realNeovimHarness) Close(t *testing.T) {
	t.Helper()
	if h == nil {
		return
	}
	h.cancel()
	if h.ptmx != nil {
		_ = h.ptmx.Close()
	}
	if h.cmd != nil && h.cmd.Process != nil {
		_ = h.cmd.Process.Kill()
	}
	if h.cmd != nil {
		_ = h.cmd.Wait()
	}
}

func (h *realNeovimHarness) pumpUntilQuiet(phase string, quiet time.Duration) error {
	if h == nil {
		return fmt.Errorf("phase %s: harness is nil", phase)
	}
	return pumpPTYIntoVTermUntilQuiet(h.ctx, h.vt, h.chunkc, quiet, phase)
}

func (h *realNeovimHarness) sendAndPump(t *testing.T, label string, seq []byte, quiet time.Duration) {
	t.Helper()
	if h == nil || h.ptmx == nil {
		t.Fatalf("write %s: harness is not ready", label)
	}
	if _, err := h.ptmx.Write(seq); err != nil {
		t.Fatalf("write %s: %v", label, err)
	}
	if err := h.pumpUntilQuiet(label, quiet); err != nil {
		t.Fatal(err)
	}
}

func pumpPTYIntoVTermUntilQuiet(ctx context.Context, vt *localvterm.VTerm, chunkc <-chan []byte, quiet time.Duration, phase string) error {
	if vt == nil {
		return fmt.Errorf("phase %s: vterm is nil", phase)
	}
	timer := time.NewTimer(quiet)
	defer timer.Stop()
	for {
		select {
		case chunk, ok := <-chunkc:
			if !ok {
				if err := assertVTermMatchesRender(vt, phase); err != nil {
					return err
				}
				return nil
			}
			if _, err := vt.Write(chunk); err != nil {
				return fmt.Errorf("phase %s: vterm write failed: %w", phase, err)
			}
			if err := assertVTermMatchesRender(vt, phase); err != nil {
				return err
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(quiet)
		case <-timer.C:
			if err := assertVTermMatchesRender(vt, phase); err != nil {
				return err
			}
			return nil
		case <-ctx.Done():
			return fmt.Errorf("phase %s: context expired waiting for PTY quiet", phase)
		}
	}
}

func pumpPTYIntoVTermsUntilQuiet(ctx context.Context, direct, restored *localvterm.VTerm, chunkc <-chan []byte, quiet time.Duration, phase string) error {
	if direct == nil || restored == nil {
		return fmt.Errorf("phase %s: vterm is nil", phase)
	}
	timer := time.NewTimer(quiet)
	defer timer.Stop()
	for {
		select {
		case chunk, ok := <-chunkc:
			if !ok {
				if err := assertVTermsEqual(direct, restored, phase); err != nil {
					return err
				}
				return nil
			}
			if _, err := direct.Write(chunk); err != nil {
				return fmt.Errorf("phase %s: direct vterm write failed: %w", phase, err)
			}
			if _, err := restored.Write(chunk); err != nil {
				return fmt.Errorf("phase %s: restored vterm write failed: %w", phase, err)
			}
			if err := assertVTermsEqual(direct, restored, phase); err != nil {
				return err
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(quiet)
		case <-timer.C:
			if err := assertVTermsEqual(direct, restored, phase); err != nil {
				return err
			}
			return nil
		case <-ctx.Done():
			return fmt.Errorf("phase %s: context expired waiting for PTY quiet", phase)
		}
	}
}

func assertVTermMatchesRender(vt *localvterm.VTerm, phase string) error {
	if vt == nil {
		return fmt.Errorf("phase %s: vterm is nil", phase)
	}
	screen := vt.ScreenContent()
	rendered := vt.RenderLines()
	if len(rendered) == 0 {
		return nil
	}
	width := screenWidth(vt)
	plain := comparableVTermScreenLines(screen, width)
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
		return fmt.Errorf(
			"phase %s: vterm screen/render mismatch at row %d\nscreen=%q\nrender=%q",
			phase,
			i,
			row,
			line,
		)
	}
	return nil
}

func assertVTermsEqual(direct, restored *localvterm.VTerm, phase string) error {
	if err := assertVTermMatchesRender(direct, phase+"-direct"); err != nil {
		return err
	}
	if err := assertVTermMatchesRender(restored, phase+"-restored"); err != nil {
		return err
	}
	directScreen := comparableVTermScreenLines(direct.ScreenContent(), screenWidth(direct))
	restoredScreen := comparableVTermScreenLines(restored.ScreenContent(), screenWidth(restored))
	limit := len(directScreen)
	if len(restoredScreen) < limit {
		limit = len(restoredScreen)
	}
	for i := 0; i < limit; i++ {
		left := strings.TrimRight(directScreen[i], " ")
		right := strings.TrimRight(restoredScreen[i], " ")
		if left == right {
			continue
		}
		return fmt.Errorf(
			"phase %s: restored snapshot diverged from direct stream at row %d\ndirect=%q\nrestored=%q",
			phase,
			i,
			left,
			right,
		)
	}
	return nil
}

func screenWidth(vt *localvterm.VTerm) int {
	if vt == nil {
		return 0
	}
	width, _ := vt.Size()
	return width
}

func comparableVTermScreenLines(screen localvterm.ScreenData, width int) []string {
	out := make([]string, len(screen.Cells))
	for i, row := range screen.Cells {
		out[i] = comparableVTermRow(row, width)
	}
	return out
}

func comparableVTermRow(row []localvterm.Cell, width int) string {
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
