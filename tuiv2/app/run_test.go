package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
	xansi "github.com/charmbracelet/x/ansi"
	creackpty "github.com/creack/pty"
	"github.com/lozzow/termx"
	"github.com/lozzow/termx/protocol"
	unixtransport "github.com/lozzow/termx/transport/unix"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/muesli/cancelreader"
)

func TestUVMouseEventToTeaMouseMsg(t *testing.T) {
	msg, ok := uvMouseEventToTeaMouseMsg(uv.MouseClickEvent(uv.Mouse{
		X:      12,
		Y:      7,
		Button: uv.MouseLeft,
		Mod:    uv.ModAlt | uv.ModCtrl,
	}), tea.MouseActionPress)
	if !ok {
		t.Fatal("expected mouse conversion to succeed")
	}
	if msg.X != 12 || msg.Y != 7 {
		t.Fatalf("expected coordinates 12,7 got %d,%d", msg.X, msg.Y)
	}
	if msg.Button != tea.MouseButtonLeft || msg.Action != tea.MouseActionPress {
		t.Fatalf("unexpected mouse mapping %#v", msg)
	}
	if !msg.Alt || !msg.Ctrl || msg.Shift {
		t.Fatalf("unexpected modifiers %#v", msg)
	}
}

func TestUVMouseEventToTeaMouseMsgRejectsUnknownButton(t *testing.T) {
	if _, ok := uvMouseEventToTeaMouseMsg(uv.MouseMotionEvent(uv.Mouse{
		X:      1,
		Y:      2,
		Button: uv.MouseButton(99),
	}), tea.MouseActionMotion); ok {
		t.Fatal("expected unsupported mouse button to be rejected")
	}
}

func TestUVKeyToTeaKeyMsgMapsShiftTab(t *testing.T) {
	msg, ok := uvKeyToTeaKeyMsg(uv.KeyPressEvent(uv.Key{Code: uv.KeyTab, Mod: uv.ModShift}))
	if !ok {
		t.Fatal("expected shift-tab conversion")
	}
	if msg.Type != tea.KeyShiftTab {
		t.Fatalf("expected KeyShiftTab, got %v", msg.Type)
	}
}

func TestUVKeyToTeaKeyMsgMapsCtrlLeft(t *testing.T) {
	msg, ok := uvKeyToTeaKeyMsg(uv.KeyPressEvent(uv.Key{Code: uv.KeyLeft, Mod: uv.ModCtrl}))
	if !ok {
		t.Fatal("expected ctrl-left conversion")
	}
	if msg.Type != tea.KeyCtrlLeft {
		t.Fatalf("expected KeyCtrlLeft, got %v", msg.Type)
	}
}

func TestUVKeyToTeaKeyMsgMapsFunctionKey(t *testing.T) {
	msg, ok := uvKeyToTeaKeyMsg(uv.KeyPressEvent(uv.Key{Code: uv.KeyF5}))
	if !ok {
		t.Fatal("expected function-key conversion")
	}
	if msg.Type != tea.KeyF5 {
		t.Fatalf("expected KeyF5, got %v", msg.Type)
	}
}

func TestTerminalReaderPreservesPrintableInputOrder(t *testing.T) {
	raw := []byte("echo cmd-1")
	reader, err := cancelreader.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("new cancel reader: %v", err)
	}
	defer reader.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventc := make(chan uv.Event, 32)
	terminalReader := uv.NewTerminalReader(reader, "xterm-256color")
	go func() {
		_ = terminalReader.StreamEvents(ctx, eventc)
		close(eventc)
	}()

	var out strings.Builder
	for event := range eventc {
		key, ok := event.(uv.KeyPressEvent)
		if !ok {
			continue
		}
		k := key.Key()
		switch {
		case k.Text != "":
			out.WriteString(k.Text)
		case k.Code < 256 && k.Code > 0:
			out.WriteRune(rune(k.Code))
		}
	}

	if got := out.String(); got != string(raw) {
		t.Fatalf("expected terminal reader order %q, got %q", string(raw), got)
	}
}

func TestE2ERunWithClientRendersInitialFrameOnPTY(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

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

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	errc := make(chan error, 1)
	go func() {
		errc <- runWithClientOptions(shared.Config{}, nil, tty, tty, tea.WithContext(ctx))
	}()

	recorder := &ptyOutputRecorder{eventc: make(chan struct{}, 1)}
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

	waitForPTYText(t, ctx, recorder, "main")
	output := recorder.Text()

	if !strings.Contains(output, "main") {
		t.Fatalf("expected initial frame to include workspace chrome, got %q", output)
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

func TestE2ERunWithClientWrapsTTYFramesWithSynchronizedOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

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

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	errc := make(chan error, 1)
	go func() {
		errc <- runWithClientOptions(shared.Config{}, nil, tty, tty, tea.WithContext(ctx))
	}()

	recorder := &ptyOutputRecorder{eventc: make(chan struct{}, 1)}
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

	waitForPTYSubstringCount(t, ctx, recorder, synchronizedOutputBegin, 1)
	waitForPTYSubstringCount(t, ctx, recorder, synchronizedOutputEnd, 1)

	output := recorder.Text()
	beginAt := strings.Index(output, synchronizedOutputBegin)
	endAt := strings.Index(output, synchronizedOutputEnd)
	if beginAt < 0 || endAt < 0 || beginAt >= endAt {
		t.Fatalf("expected synchronized output markers in order, got output %q", output)
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

func TestRunWithClientNonTTYOutputDoesNotEmitEmojiProbe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	var out bytes.Buffer
	err := runWithClientOptions(shared.Config{}, nil, nil, &out, tea.WithContext(ctx))
	if err != nil && !errors.Is(err, tea.ErrProgramKilled) {
		t.Fatalf("runWithClientOptions returned unexpected error: %v", err)
	}
	if strings.Contains(out.String(), xansi.RequestExtendedCursorPositionReport) {
		t.Fatalf("expected non-tty output not to emit decxcpr probe, got output %q", out.String())
	}
}

func TestConfigureProgramOutputKeepsNonTTYCursorInline(t *testing.T) {
	model := New(shared.Config{}, nil, nil)
	var out bytes.Buffer

	output, probeSupported := configureProgramOutput(model, &out)

	if output != &out {
		t.Fatalf("expected non-tty output to remain unchanged, got %#v", output)
	}
	if probeSupported {
		t.Fatal("expected non-tty output to disable host emoji probe")
	}
	if model.cursorOut != nil {
		t.Fatalf("expected non-tty output to keep inline cursor projection, got writer %#v", model.cursorOut)
	}
}

func TestConfigureProgramOutputUsesCursorWriterForTTY(t *testing.T) {
	model := New(shared.Config{}, nil, nil)
	tty := &cursorWriterProbeTTY{}

	output, probeSupported := configureProgramOutput(model, tty)

	writer, ok := output.(*outputCursorWriter)
	if !ok || writer == nil {
		t.Fatalf("expected tty output to use outputCursorWriter, got %#v", output)
	}
	if !probeSupported {
		t.Fatal("expected tty output to enable host emoji probe")
	}
	if model.cursorOut == nil {
		t.Fatal("expected tty output to configure model cursor writer")
	}
	if model.cursorOut != writer {
		t.Fatalf("expected model cursor writer to match configured output writer, got %#v want %#v", model.cursorOut, writer)
	}
}

func TestE2ERunWithClientRetriesHostEmojiProbeWhenFirstCPRIsDropped(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	originalRetryDelay := hostEmojiProbeRetryDelay
	originalMaxAttempts := hostEmojiProbeMaxAttempts
	t.Cleanup(func() {
		hostEmojiProbeRetryDelay = originalRetryDelay
		hostEmojiProbeMaxAttempts = originalMaxAttempts
	})
	hostEmojiProbeRetryDelay = 15 * time.Millisecond
	hostEmojiProbeMaxAttempts = 3

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

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	errc := make(chan error, 1)
	go func() {
		errc <- runWithClientOptions(shared.Config{}, nil, tty, tty, tea.WithContext(ctx))
	}()

	recorder := &ptyOutputRecorder{eventc: make(chan struct{}, 1)}
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

	waitForPTYSubstringCount(t, ctx, recorder, xansi.RequestExtendedCursorPositionReport, 2)

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

func TestE2ERunWithClientAcceptsNonOriginExtendedCursorReportForEmojiProbe(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	originalRetryDelay := hostEmojiProbeRetryDelay
	originalMaxAttempts := hostEmojiProbeMaxAttempts
	t.Cleanup(func() {
		hostEmojiProbeRetryDelay = originalRetryDelay
		hostEmojiProbeMaxAttempts = originalMaxAttempts
	})
	hostEmojiProbeRetryDelay = 20 * time.Millisecond
	hostEmojiProbeMaxAttempts = 3

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

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	errc := make(chan error, 1)
	go func() {
		errc <- runWithClientOptions(shared.Config{}, nil, tty, tty, tea.WithContext(ctx))
	}()

	recorder := &ptyOutputRecorder{eventc: make(chan struct{}, 1)}
	responded := make(chan struct{}, 1)
	go func() {
		buf := make([]byte, 4096)
		sent := false
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				recorder.Append(chunk)
				if !sent && strings.Contains(recorder.Text(), xansi.RequestExtendedCursorPositionReport) {
					sent = true
					_, _ = ptmx.Write([]byte(xansi.ExtendedCursorPositionReport(8, 3, 0)))
					responded <- struct{}{}
				}
			}
			if err != nil {
				return
			}
		}
	}()

	select {
	case <-responded:
	case <-ctx.Done():
		t.Fatal("timed out waiting to respond to startup decxcpr probe")
	}

	time.Sleep(3 * hostEmojiProbeRetryDelay)
	if got := strings.Count(recorder.Text(), xansi.RequestExtendedCursorPositionReport); got != 1 {
		t.Fatalf("expected non-origin decxcpr response to stop retries after first probe, got %d probes in output:\n%s", got, recorder.Text())
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

func TestE2ERunWithClientIgnoresUnexpectedProbeColumnsUntilValidResponse(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	originalRetryDelay := hostEmojiProbeRetryDelay
	originalMaxAttempts := hostEmojiProbeMaxAttempts
	t.Cleanup(func() {
		hostEmojiProbeRetryDelay = originalRetryDelay
		hostEmojiProbeMaxAttempts = originalMaxAttempts
	})
	hostEmojiProbeRetryDelay = 20 * time.Millisecond
	hostEmojiProbeMaxAttempts = 4

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

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	errc := make(chan error, 1)
	go func() {
		errc <- runWithClientOptions(shared.Config{}, nil, tty, tty, tea.WithContext(ctx))
	}()

	recorder := &ptyOutputRecorder{eventc: make(chan struct{}, 1)}
	respondedInvalid := make(chan struct{}, 1)
	respondedValid := make(chan struct{}, 1)
	go func() {
		buf := make([]byte, 4096)
		invalidSent := false
		validSent := false
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				recorder.Append(string(buf[:n]))
				probes := strings.Count(recorder.Text(), xansi.RequestExtendedCursorPositionReport)
				if !invalidSent && probes >= 1 {
					invalidSent = true
					_, _ = ptmx.Write([]byte(xansi.ExtendedCursorPositionReport(8, 18, 0)))
					respondedInvalid <- struct{}{}
				}
				if invalidSent && !validSent && probes >= 2 {
					validSent = true
					_, _ = ptmx.Write([]byte(xansi.ExtendedCursorPositionReport(8, 2, 0)))
					respondedValid <- struct{}{}
				}
			}
			if err != nil {
				return
			}
		}
	}()

	select {
	case <-respondedInvalid:
	case <-ctx.Done():
		t.Fatal("timed out waiting to respond to initial decxcpr probe")
	}
	select {
	case <-respondedValid:
	case <-ctx.Done():
		t.Fatal("timed out waiting to respond to retried decxcpr probe")
	}

	time.Sleep(3 * hostEmojiProbeRetryDelay)
	if got := strings.Count(recorder.Text(), xansi.RequestExtendedCursorPositionReport); got != 2 {
		t.Fatalf("expected invalid decxcpr response to be ignored until the next exact-width probe succeeds, got %d probes in output:\n%s", got, recorder.Text())
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

func TestE2ERunWithClientAttachShellAcceptsRepeatedCommandsOnPTY(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	socketPath := filepath.Join(t.TempDir(), "termx-run-pty.sock")
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

	created, err := ctrlClient.Create(ctx, protocol.CreateParams{
		Command: []string{"sh", "-c", "printf 'pty-ready\\n'; exec sh"},
		Name:    "pty-shell",
		Size:    protocol.Size{Cols: 120, Rows: 40},
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

	recorder := &ptyOutputRecorder{eventc: make(chan struct{}, 1)}
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

	waitForPTYText(t, ctx, recorder, "pty-ready")
	for i := 1; i <= 8; i++ {
		marker := "cmd-" + strconv.Itoa(i)
		if _, err := ptmx.Write([]byte("echo " + marker + "\n")); err != nil {
			t.Fatalf("write command %q: %v", marker, err)
		}
		waitForPTYText(t, ctx, recorder, marker)
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

func TestE2ERunWithClientAttachHtopCanQuitOnPTY(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}
	if _, err := exec.LookPath("htop"); err != nil {
		t.Skip("htop not installed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	socketPath := filepath.Join(t.TempDir(), "termx-run-htop.sock")
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

	created, err := ctrlClient.Create(ctx, protocol.CreateParams{
		Command: []string{"htop"},
		Name:    "pty-htop",
		Size:    protocol.Size{Cols: 120, Rows: 40},
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

	recorder := &ptyOutputRecorder{eventc: make(chan struct{}, 1)}
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

	waitForPTYOutputLength(t, ctx, recorder, 2000)
	if _, err := ptmx.Write([]byte("q")); err != nil {
		t.Fatalf("write htop quit key: %v", err)
	}
	waitForTerminalState(t, ctx, srv, created.TerminalID, termx.StateExited)

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

type ptyOutputRecorder struct {
	mu     sync.RWMutex
	text   string
	eventc chan struct{}
}

func (r *ptyOutputRecorder) Append(chunk string) {
	if r == nil || chunk == "" {
		return
	}
	r.mu.Lock()
	r.text += chunk
	r.mu.Unlock()
	select {
	case r.eventc <- struct{}{}:
	default:
	}
}

func (r *ptyOutputRecorder) Text() string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.text
}

func waitForPTYText(t *testing.T, ctx context.Context, recorder *ptyOutputRecorder, target string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(recorder.Text(), target) {
			return
		}
		select {
		case <-recorder.eventc:
		case <-ctx.Done():
			t.Fatalf("context expired waiting for %q", target)
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatalf("timeout waiting for %q in PTY output\nlatest output:\n%s", target, recorder.Text())
}

func waitForPTYOutputLength(t *testing.T, ctx context.Context, recorder *ptyOutputRecorder, min int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if len(recorder.Text()) >= min {
			return
		}
		select {
		case <-recorder.eventc:
		case <-ctx.Done():
			t.Fatalf("context expired waiting for PTY output length >= %d", min)
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatalf("timeout waiting for PTY output length >= %d\nlatest output length=%d", min, len(recorder.Text()))
}

func waitForPTYSubstringCount(t *testing.T, ctx context.Context, recorder *ptyOutputRecorder, target string, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Count(recorder.Text(), target) >= want {
			return
		}
		select {
		case <-recorder.eventc:
		case <-ctx.Done():
			t.Fatalf("context expired waiting for %d occurrences of %q", want, target)
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatalf("timeout waiting for %d occurrences of %q\nlatest output:\n%s", want, target, recorder.Text())
}

func waitForTerminalState(t *testing.T, ctx context.Context, srv *termx.Server, terminalID string, want termx.TerminalState) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		info, err := srv.Get(ctx, terminalID)
		if err == nil && info != nil && info.State == want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("context expired waiting for terminal %s state %s", terminalID, want)
		case <-time.After(100 * time.Millisecond):
		}
	}
	info, err := srv.Get(context.Background(), terminalID)
	t.Fatalf("timeout waiting for terminal %s state %s; latest info=%#v err=%v", terminalID, want, info, err)
}

func waitTestSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := unixtransport.Dial(path)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("socket did not appear in time")
}

type keyCaptureModel struct {
	keys        strings.Builder
	targetCount int
	count       int
}

func (m keyCaptureModel) Init() tea.Cmd { return nil }

func (m keyCaptureModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		switch typed.Type {
		case tea.KeyRunes:
			m.keys.WriteString(string(typed.Runes))
			m.count += len(typed.Runes)
		case tea.KeySpace:
			m.keys.WriteByte(' ')
			m.count++
		case tea.KeyEnter:
			m.keys.WriteByte('\n')
			m.count++
		case tea.KeyCtrlJ:
			m.keys.WriteByte('\n')
			m.count++
		}
		if m.targetCount > 0 && m.count >= m.targetCount {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m keyCaptureModel) View() string { return "" }

func TestStartInputForwarderPreservesKeyOrderIntoBubbleTea(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	ptmx, tty, err := creackpty.Open()
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("open pty: %v", err)
	}
	defer ptmx.Close()
	defer tty.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	model := keyCaptureModel{targetCount: len("echo cmd-1\n")}
	program := tea.NewProgram(model, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithContext(ctx))
	stopInput, restoreInput, err := startInputForwarder(program, tty)
	if err != nil {
		t.Fatalf("start input forwarder: %v", err)
	}
	defer func() { _ = restoreInput() }()
	defer stopInput()

	done := make(chan tea.Model, 1)
	go func() {
		finalModel, _ := program.Run()
		done <- finalModel
	}()

	if _, err := ptmx.Write([]byte("echo cmd-1\n")); err != nil {
		t.Fatalf("write PTY input: %v", err)
	}

	select {
	case rawModel := <-done:
		finalModel, ok := rawModel.(keyCaptureModel)
		if !ok {
			t.Fatalf("unexpected final model type %T", rawModel)
		}
		if got := finalModel.keys.String(); got != "echo cmd-1\n" {
			t.Fatalf("expected key order %q, got %q", "echo cmd-1\n", got)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for Bubble Tea program to exit")
	}
}

type hostCursorPositionCaptureModel struct {
	got   bool
	x, y  int
	extra []tea.KeyMsg
}

func (m hostCursorPositionCaptureModel) Init() tea.Cmd { return nil }

func (m hostCursorPositionCaptureModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case hostCursorPositionMsg:
		m.got = true
		m.x = typed.X
		m.y = typed.Y
		return m, tea.Quit
	case tea.KeyMsg:
		m.extra = append(m.extra, typed)
	}
	return m, nil
}

func (m hostCursorPositionCaptureModel) View() string { return "" }

func TestStartInputForwarderParsesExtendedCursorPositionReport(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: requires a real PTY, skipped with -short")
	}

	ptmx, tty, err := creackpty.Open()
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("open pty: %v", err)
	}
	defer ptmx.Close()
	defer tty.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	model := hostCursorPositionCaptureModel{}
	program := tea.NewProgram(model, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithContext(ctx))
	stopInput, restoreInput, err := startInputForwarder(program, tty)
	if err != nil {
		t.Fatalf("start input forwarder: %v", err)
	}
	defer func() { _ = restoreInput() }()
	defer stopInput()

	done := make(chan tea.Model, 1)
	go func() {
		finalModel, _ := program.Run()
		done <- finalModel
	}()

	if _, err := ptmx.Write([]byte(xansi.ExtendedCursorPositionReport(1, 3, 0))); err != nil {
		t.Fatalf("write decxcpr: %v", err)
	}

	select {
	case rawModel := <-done:
		finalModel, ok := rawModel.(hostCursorPositionCaptureModel)
		if !ok {
			t.Fatalf("unexpected final model type %T", rawModel)
		}
		if !finalModel.got {
			t.Fatal("expected host cursor position report to reach bubbletea model")
		}
		if finalModel.x != 2 || finalModel.y != 0 {
			t.Fatalf("expected zero-based decxcpr coordinates 2,0 got %d,%d", finalModel.x, finalModel.y)
		}
		if len(finalModel.extra) != 0 {
			t.Fatalf("expected no spurious key events alongside decxcpr, got %#v", finalModel.extra)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for extended cursor position report to reach Bubble Tea")
	}
}

type fakeSessionEventsClient struct {
	events   chan protocol.Event
	session  *protocol.SessionSnapshot
	getCalls []string
}

func (f *fakeSessionEventsClient) Close() error { return nil }

func (f *fakeSessionEventsClient) Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error) {
	return f.events, nil
}

func (f *fakeSessionEventsClient) GetSession(_ context.Context, sessionID string) (*protocol.SessionSnapshot, error) {
	f.getCalls = append(f.getCalls, sessionID)
	if f.session == nil {
		return &protocol.SessionSnapshot{}, nil
	}
	return f.session, nil
}

func TestStartSessionEventsForwarderReconnectsAndRequestsResync(t *testing.T) {
	originalFactory := newSessionEventsClient
	originalDelay := sessionEventsReconnectDelay
	defer func() {
		newSessionEventsClient = originalFactory
		sessionEventsReconnectDelay = originalDelay
	}()

	first := &fakeSessionEventsClient{events: make(chan protocol.Event, 4)}
	second := &fakeSessionEventsClient{
		events: make(chan protocol.Event, 4),
		session: &protocol.SessionSnapshot{
			Session: protocol.SessionInfo{ID: "main", Revision: 7},
		},
	}
	clients := []*fakeSessionEventsClient{first, second}
	var mu sync.Mutex
	dials := 0
	newSessionEventsClient = func(context.Context, string) (sessionEventsClient, error) {
		mu.Lock()
		defer mu.Unlock()
		if dials >= len(clients) {
			return nil, errors.New("unexpected extra dial")
		}
		client := clients[dials]
		dials++
		return client, nil
	}
	sessionEventsReconnectDelay = 10 * time.Millisecond

	msgs := make(chan tea.Msg, 8)
	stop := startSessionEventsForwarder(func(msg tea.Msg) {
		msgs <- msg
	}, shared.Config{SessionID: "main", SocketPath: "/tmp/termx.sock"}, nil)
	defer stop()

	first.events <- protocol.Event{Type: protocol.EventSessionUpdated, SessionID: "main"}
	select {
	case msg := <-msgs:
		typed, ok := msg.(sessionEventMsg)
		if !ok {
			t.Fatalf("expected sessionEventMsg, got %T", msg)
		}
		if typed.Event.Type != protocol.EventSessionUpdated || typed.Event.SessionID != "main" {
			t.Fatalf("unexpected event %#v", typed.Event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for forwarded session event")
	}

	close(first.events)

	select {
	case msg := <-msgs:
		typed, ok := msg.(sessionSnapshotMsg)
		if !ok {
			t.Fatalf("expected sessionSnapshotMsg after reconnect, got %T", msg)
		}
		if typed.Err != nil {
			t.Fatalf("unexpected resync error: %v", typed.Err)
		}
		if typed.Snapshot == nil || typed.Snapshot.Session.ID != "main" || typed.Snapshot.Session.Revision != 7 {
			t.Fatalf("unexpected resync snapshot %#v", typed.Snapshot)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for session resync snapshot")
	}

	second.events <- protocol.Event{Type: protocol.EventSessionDeleted, SessionID: "main"}
	select {
	case msg := <-msgs:
		typed, ok := msg.(sessionEventMsg)
		if !ok {
			t.Fatalf("expected sessionEventMsg after reconnect, got %T", msg)
		}
		if typed.Event.Type != protocol.EventSessionDeleted || typed.Event.SessionID != "main" {
			t.Fatalf("unexpected reconnected event %#v", typed.Event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for forwarded event after reconnect")
	}
}
