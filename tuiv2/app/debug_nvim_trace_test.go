package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	creackpty "github.com/creack/pty"
	"github.com/lozzow/termx"
	"github.com/lozzow/termx/protocol"
	unixtransport "github.com/lozzow/termx/transport/unix"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/shared"
	localvterm "github.com/lozzow/termx/vterm"
)

func TestDebugNvimScrollTrace(t *testing.T) {
	if os.Getenv("TERMX_RUN_NVIM_TRACE") != "1" {
		t.Skip("set TERMX_RUN_NVIM_TRACE=1 to run the interactive nvim trace")
	}
	if testing.Short() {
		t.Skip("debug trace")
	}
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	socketPath := filepath.Join(t.TempDir(), "termx-debug-nvim.sock")
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
	if err := waitTestSocket(socketPath, 5*time.Second); err != nil {
		t.Fatalf("server socket never appeared: %v", err)
	}

	ctrlTransport, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial control client: %v", err)
	}
	ctrlClient := protocol.NewClient(ctrlTransport)
	if err := ctrlClient.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello control client: %v", err)
	}
	t.Cleanup(func() { _ = ctrlClient.Close() })

	tmpFile := filepath.Join(t.TempDir(), "nvim-scroll.txt")
	var lines []string
	for i := 1; i <= 300; i++ {
		lines = append(lines, fmt.Sprintf("line %03d %s", i, strings.Repeat("x", 40)))
	}
	if err := os.WriteFile(tmpFile, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	created, err := ctrlClient.Create(ctx, protocol.CreateParams{
		Command: []string{
			"nvim",
			"-u", "NONE",
			"-n",
			"-c", "set nomore nonumber norelativenumber laststatus=0 cmdheight=0 noshowmode nowrap",
			tmpFile,
		},
		Name: "debug-nvim",
		Size: protocol.Size{Cols: 120, Rows: 40},
	})
	if err != nil {
		t.Fatalf("create terminal: %v", err)
	}

	appTransport, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial app client: %v", err)
	}
	appProtocolClient := protocol.NewClient(appTransport)
	if err := appProtocolClient.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello app client: %v", err)
	}
	t.Cleanup(func() { _ = appProtocolClient.Close() })

	ptmx, tty, err := creackpty.Open()
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("open pty: %v", err)
	}
	defer ptmx.Close()
	defer tty.Close()

	if err := creackpty.Setsize(ptmx, &creackpty.Winsize{Cols: 120, Rows: 40}); err != nil {
		t.Fatalf("set pty size: %v", err)
	}

	errc := make(chan error, 1)
	go func() {
		errc <- runWithClientOptions(
			shared.Config{AttachID: created.TerminalID},
			bridge.NewProtocolClient(appProtocolClient),
			tty,
			tty,
			tea.WithContext(ctx),
		)
	}()

	recorder := &ptyOutputRecorder{eventc: make(chan struct{}, 1024)}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				recorder.Append(string(buf[:n]))
			}
			if err != nil {
				return
			}
		}
	}()

	waitForPTYOutputLength(t, ctx, recorder, 3000)
	waitForPTYQuiet(t, ctx, recorder, 300*time.Millisecond)
	base := len(recorder.Text())
	t.Logf("initial_output_bytes=%d sync_frames=%d", base, strings.Count(recorder.Text(), synchronizedOutputBegin))

	scrollAndLog := func(label string, seq []byte) {
		t.Helper()
		before := len(recorder.Text())
		if _, err := ptmx.Write(seq); err != nil {
			t.Fatalf("write %s: %v", label, err)
		}
		waitForPTYGrowth(t, ctx, recorder, before+200)
		waitForPTYQuiet(t, ctx, recorder, 250*time.Millisecond)
		delta := recorder.Text()[before:]
		t.Logf(
			"%s bytes=%d sync_frames=%d origin=%d clear=%d crlf=%d sample=%q",
			label,
			len(delta),
			strings.Count(delta, synchronizedOutputBegin),
			strings.Count(delta, xansi.MoveCursorOrigin),
			strings.Count(delta, xansi.EraseEntireDisplay),
			strings.Count(delta, "\r\n"),
			debugEscape(delta, 220),
		)
	}

	for i := 0; i < 5; i++ {
		scrollAndLog("ctrl_e_"+strconv.Itoa(i+1), []byte{0x05})
	}

	cancel()
	select {
	case err := <-errc:
		if err != nil && !errors.Is(err, tea.ErrProgramKilled) {
			t.Fatalf("runWithClientOptions returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for TUI shutdown")
	}
}

func TestDebugNvimInsertTrace(t *testing.T) {
	if os.Getenv("TERMX_RUN_NVIM_TRACE") != "1" {
		t.Skip("set TERMX_RUN_NVIM_TRACE=1 to run the interactive nvim trace")
	}
	if testing.Short() {
		t.Skip("debug trace")
	}
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	socketPath := filepath.Join(t.TempDir(), "termx-debug-nvim-insert.sock")
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
	if err := waitTestSocket(socketPath, 5*time.Second); err != nil {
		t.Fatalf("server socket never appeared: %v", err)
	}

	ctrlTransport, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial control client: %v", err)
	}
	ctrlClient := protocol.NewClient(ctrlTransport)
	if err := ctrlClient.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello control client: %v", err)
	}
	t.Cleanup(func() { _ = ctrlClient.Close() })

	tmpFile := filepath.Join(t.TempDir(), "nvim-insert.txt")
	var lines []string
	for i := 1; i <= 300; i++ {
		lines = append(lines, fmt.Sprintf("line %03d %s", i, strings.Repeat("x", 40)))
	}
	if err := os.WriteFile(tmpFile, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	created, err := ctrlClient.Create(ctx, protocol.CreateParams{
		Command: []string{
			"nvim",
			"-u", "NONE",
			"-n",
			"-c", "set nomore nonumber norelativenumber laststatus=0 cmdheight=0 noshowmode nowrap",
			tmpFile,
		},
		Name: "debug-nvim-insert",
		Size: protocol.Size{Cols: 120, Rows: 40},
	})
	if err != nil {
		t.Fatalf("create terminal: %v", err)
	}

	appTransport, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial app client: %v", err)
	}
	appProtocolClient := protocol.NewClient(appTransport)
	if err := appProtocolClient.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello app client: %v", err)
	}
	t.Cleanup(func() { _ = appProtocolClient.Close() })

	ptmx, tty, err := creackpty.Open()
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("open pty: %v", err)
	}
	defer ptmx.Close()
	defer tty.Close()

	if err := creackpty.Setsize(ptmx, &creackpty.Winsize{Cols: 120, Rows: 40}); err != nil {
		t.Fatalf("set pty size: %v", err)
	}

	errc := make(chan error, 1)
	go func() {
		errc <- runWithClientOptions(
			shared.Config{AttachID: created.TerminalID},
			bridge.NewProtocolClient(appProtocolClient),
			tty,
			tty,
			tea.WithContext(ctx),
		)
	}()

	recorder := &ptyOutputRecorder{eventc: make(chan struct{}, 1024)}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				recorder.Append(string(buf[:n]))
			}
			if err != nil {
				return
			}
		}
	}()

	waitForPTYOutputLength(t, ctx, recorder, 3000)
	waitForPTYQuiet(t, ctx, recorder, 300*time.Millisecond)
	before := len(recorder.Text())

	if _, err := ptmx.Write([]byte("120G0iHELLO\x1b")); err != nil {
		t.Fatalf("write insert keys: %v", err)
	}
	waitForPTYGrowth(t, ctx, recorder, before+200)
	waitForPTYQuiet(t, ctx, recorder, 250*time.Millisecond)

	delta := recorder.Text()[before:]
	t.Logf(
		"insert bytes=%d sync_frames=%d origin=%d clear=%d scroll_up=%d scroll_down=%d row4=%d row120=%d sample=%q",
		len(delta),
		strings.Count(delta, synchronizedOutputBegin),
		strings.Count(delta, xansi.MoveCursorOrigin),
		strings.Count(delta, xansi.EraseEntireDisplay),
		strings.Count(delta, xansi.SU(1)),
		strings.Count(delta, xansi.SD(1)),
		strings.Count(delta, "\x1b[4;1H"),
		strings.Count(delta, "\x1b[23;1H"),
		debugEscape(delta, 280),
	)

	cancel()
	select {
	case err := <-errc:
		if err != nil && !errors.Is(err, tea.ErrProgramKilled) {
			t.Fatalf("runWithClientOptions returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for TUI shutdown")
	}
}

func TestDebugNvimScrollThenInsertScreenPosition(t *testing.T) {
	if os.Getenv("TERMX_RUN_NVIM_TRACE") != "1" {
		t.Skip("set TERMX_RUN_NVIM_TRACE=1 to run the interactive nvim trace")
	}
	if testing.Short() {
		t.Skip("debug trace")
	}
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	socketPath := filepath.Join(t.TempDir(), "termx-debug-nvim-screen.sock")
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
	if err := waitTestSocket(socketPath, 5*time.Second); err != nil {
		t.Fatalf("server socket never appeared: %v", err)
	}

	ctrlTransport, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial control client: %v", err)
	}
	ctrlClient := protocol.NewClient(ctrlTransport)
	if err := ctrlClient.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello control client: %v", err)
	}
	t.Cleanup(func() { _ = ctrlClient.Close() })

	tmpFile := filepath.Join(t.TempDir(), "nvim-screen.txt")
	var lines []string
	for i := 1; i <= 300; i++ {
		lines = append(lines, fmt.Sprintf("line %03d %s", i, strings.Repeat("x", 40)))
	}
	if err := os.WriteFile(tmpFile, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	created, err := ctrlClient.Create(ctx, protocol.CreateParams{
		Command: []string{
			"nvim",
			"-u", "NONE",
			"-n",
			"-c", "set nomore nonumber norelativenumber laststatus=0 cmdheight=0 noshowmode nowrap",
			tmpFile,
		},
		Name: "debug-nvim-screen",
		Size: protocol.Size{Cols: 120, Rows: 40},
	})
	if err != nil {
		t.Fatalf("create terminal: %v", err)
	}

	appTransport, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial app client: %v", err)
	}
	appProtocolClient := protocol.NewClient(appTransport)
	if err := appProtocolClient.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello app client: %v", err)
	}
	t.Cleanup(func() { _ = appProtocolClient.Close() })

	ptmx, tty, err := creackpty.Open()
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("open pty: %v", err)
	}
	defer ptmx.Close()
	defer tty.Close()

	if err := creackpty.Setsize(ptmx, &creackpty.Winsize{Cols: 120, Rows: 40}); err != nil {
		t.Fatalf("set pty size: %v", err)
	}

	errc := make(chan error, 1)
	go func() {
		errc <- runWithClientOptions(
			shared.Config{AttachID: created.TerminalID},
			bridge.NewProtocolClient(appProtocolClient),
			tty,
			tty,
			tea.WithContext(ctx),
		)
	}()

	recorder := &ptyOutputRecorder{eventc: make(chan struct{}, 1024)}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				recorder.Append(string(buf[:n]))
			}
			if err != nil {
				return
			}
		}
	}()

	waitForPTYOutputLength(t, ctx, recorder, 3000)
	waitForPTYQuiet(t, ctx, recorder, 300*time.Millisecond)

	if _, err := ptmx.Write([]byte{0x05, 0x05, 0x05}); err != nil {
		t.Fatalf("scroll before insert: %v", err)
	}
	waitForPTYQuiet(t, ctx, recorder, 250*time.Millisecond)

	if _, err := ptmx.Write([]byte("120Gzz0iHELLO\x1b")); err != nil {
		t.Fatalf("write insert keys: %v", err)
	}
	waitForPTYQuiet(t, ctx, recorder, 300*time.Millisecond)

	hostVT := localvterm.New(120, 40, 0, nil)
	if _, err := hostVT.Write([]byte(recorder.Text())); err != nil {
		t.Fatalf("replay full PTY stream into host vterm: %v", err)
	}
	screen := hostVT.ScreenContent()
	row := -1
	screenLines := vtermScreenLines(screen)
	for i, line := range screenLines {
		if strings.Contains(line, "HELLO") {
			row = i
			break
		}
	}
	t.Logf("hello_row=%d top=%q middle=%q", row, strings.TrimRight(screenLines[0], " "), strings.TrimRight(screenLines[20], " "))
	if row == 0 {
		t.Fatalf("expected HELLO not to land on the top row after scroll+insert, got screen=%#v", screenLines)
	}

	cancel()
	select {
	case err := <-errc:
		if err != nil && !errors.Is(err, tea.ErrProgramKilled) {
			t.Fatalf("runWithClientOptions returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for TUI shutdown")
	}
}

func waitForPTYGrowth(t *testing.T, ctx context.Context, recorder *ptyOutputRecorder, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(recorder.Text()) >= want {
			return
		}
		select {
		case <-recorder.eventc:
		case <-ctx.Done():
			t.Fatalf("context expired waiting for PTY length >= %d", want)
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatalf("timeout waiting for PTY length >= %d, got %d", want, len(recorder.Text()))
}

func waitForPTYQuiet(t *testing.T, ctx context.Context, recorder *ptyOutputRecorder, quiet time.Duration) {
	t.Helper()
	timer := time.NewTimer(quiet)
	defer timer.Stop()
	for {
		select {
		case <-recorder.eventc:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(quiet)
		case <-timer.C:
			return
		case <-ctx.Done():
			t.Fatal("context expired waiting for PTY quiet period")
		}
	}
}

func debugEscape(s string, limit int) string {
	if len(s) > limit {
		s = s[:limit]
	}
	s = strings.ReplaceAll(s, "\x1b", "\\x1b")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}
