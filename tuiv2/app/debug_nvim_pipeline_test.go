package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	localvterm "github.com/lozzow/termx/vterm"
)

func TestDebugSinglePaneNvimScrollPipelineLocatesFirstDivergence(t *testing.T) {
	if os.Getenv("TERMX_RUN_NVIM_TRACE") != "1" {
		t.Skip("set TERMX_RUN_NVIM_TRACE=1 to run the single-pane nvim pipeline trace")
	}
	if testing.Short() {
		t.Skip("debug trace")
	}
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	harness := startNvimPipelineHarness(t, "single-pane-scroll")
	defer harness.Close(t)

	harness.waitForInitialScreen(t)
	harness.drainAsync(t, 200*time.Millisecond)

	// Move away from the top so Ctrl-E produces multiple viewport scroll frames.
	harness.sendInputAndWait(t, []byte("150Gzz"), 300*time.Millisecond)

	actions := [][]byte{
		bytesRepeat(0x05, 12),
		bytesRepeat(0x05, 12),
		bytesRepeat(0x05, 12),
		bytesRepeat(0x19, 6),
		bytesRepeat(0x05, 12),
		bytesRepeat(0x05, 12),
		bytesRepeat(0x19, 6),
		bytesRepeat(0x05, 12),
	}
	for _, action := range actions {
		pane := harness.model.workbench.ActivePane()
		if pane == nil {
			t.Fatal("expected active pane during burst input")
		}
		_, cmd := harness.model.Update(input.TerminalInput{PaneID: pane.ID, Data: action})
		e2eDrain(t, harness.model, cmd)
		time.Sleep(18 * time.Millisecond)
	}
	captured := harness.drainAsync(t, 320*time.Millisecond)
	if len(captured) == 0 {
		t.Fatal("expected rapid nvim burst to produce invalidated frames")
	}
	if len(captured) < 4 {
		t.Fatalf("expected multiple captured frames, got %d", len(captured))
	}

	enabled := replayPipelineFramesThroughWriterAndTmux(t, captured, true)
	disabled := replayPipelineFramesThroughWriterAndTmux(t, captured, false)

	for i := range captured {
		serverScreen := normalizeComparableHeight(captured[i].ServerLines, 40)
		localSnapshotScreen := normalizeComparableHeight(captured[i].LocalSnapshotLines, 40)
		localSurfaceScreen := normalizeComparableHeight(captured[i].LocalSurfaceLines, 40)
		if hasVisibleText(localSnapshotScreen) && !reflect.DeepEqual(localSnapshotScreen, localSurfaceScreen) {
			t.Fatalf(
				"first divergence before render: local snapshot and local surface differ on frame %d\nsnapshot=%s\nsurface=%s",
				i+1,
				formatComparableLines(localSnapshotScreen[:minInt(8, len(localSnapshotScreen))]),
				formatComparableLines(localSurfaceScreen[:minInt(8, len(localSurfaceScreen))]),
			)
		}
		if !reflect.DeepEqual(serverScreen, localSurfaceScreen) {
			t.Fatalf(
				"first divergence at runtime surface on frame %d\nserver=%s\nlocal_surface=%s\nlocal_snapshot=%s",
				i+1,
				formatComparableLines(serverScreen[:minInt(8, len(serverScreen))]),
				formatComparableLines(localSurfaceScreen[:minInt(8, len(localSurfaceScreen))]),
				formatComparableLines(localSnapshotScreen[:minInt(8, len(localSnapshotScreen))]),
			)
		}
		expected := normalizeComparableHeight(comparableScreenLines(captured[i].RenderLines), 40)
		disabledScreen := normalizeComparableHeight(disabled.TmuxScreens[i], 40)
		if !reflect.DeepEqual(disabledScreen, expected) {
			t.Fatalf(
				"disabled writer baseline diverged on frame %d\nexpected=%s\nbaseline=%s\nraw=%q",
				i+1,
				formatComparableLines(expected[:minInt(8, len(expected))]),
				formatComparableLines(disabledScreen[:minInt(8, len(disabledScreen))]),
				debugEscape(disabled.RawANSI[i], 240),
			)
		}
		enabledTmuxScreen := normalizeComparableHeight(enabled.TmuxScreens[i], 40)
		enabledVTermScreen := normalizeComparableHeight(enabled.VTermScreens[i], 40)
		if reflect.DeepEqual(enabledTmuxScreen, expected) {
			continue
		}
		vtermDiverged := !reflect.DeepEqual(enabledVTermScreen, expected)
		t.Fatalf(
			"first divergence at writer layer on frame %d vterm_diverged=%v\nserver=%s\nlocal_snapshot=%s\nlocal_surface=%s\nrender=%s\nenabled_tmux=%s\nenabled_vterm=%s\nenabled_raw=%q\ndisabled_raw=%q",
			i+1,
			vtermDiverged,
			formatComparableLines(captured[i].ServerLines[:minInt(8, len(captured[i].ServerLines))]),
			formatComparableLines(captured[i].LocalSnapshotLines[:minInt(8, len(captured[i].LocalSnapshotLines))]),
			formatComparableLines(captured[i].LocalSurfaceLines[:minInt(8, len(captured[i].LocalSurfaceLines))]),
			formatComparableLines(expected[:minInt(8, len(expected))]),
			formatComparableLines(enabledTmuxScreen[:minInt(8, len(enabledTmuxScreen))]),
			formatComparableLines(enabledVTermScreen[:minInt(8, len(enabledVTermScreen))]),
			debugEscape(enabled.RawANSI[i], 320),
			debugEscape(disabled.RawANSI[i], 320),
		)
	}
	t.Fatalf("did not reproduce an intermediate divergence after %d frames", len(captured))
}

func TestDebugSinglePaneNvimActualRunIntermediateTmuxParity(t *testing.T) {
	if os.Getenv("TERMX_RUN_NVIM_TRACE") != "1" {
		t.Skip("set TERMX_RUN_NVIM_TRACE=1 to run the single-pane nvim tmux parity trace")
	}
	if testing.Short() {
		t.Skip("debug trace")
	}
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	actions := []struct {
		label string
		seq   []byte
	}{
		{label: "down_12_a", seq: bytesRepeat(0x05, 12)},
		{label: "down_12_b", seq: bytesRepeat(0x05, 12)},
		{label: "down_12_c", seq: bytesRepeat(0x05, 12)},
		{label: "up_6", seq: bytesRepeat(0x19, 6)},
		{label: "down_12_d", seq: bytesRepeat(0x05, 12)},
		{label: "down_12_e", seq: bytesRepeat(0x05, 12)},
		{label: "alternating_16", seq: alternatingBytes(0x05, 0x19, 8)},
		{label: "down_12_f", seq: bytesRepeat(0x05, 12)},
	}

	enabled := captureActualRunStepScreens(t, "single-pane-actual", false, actions)
	disabled := captureActualRunStepScreens(t, "single-pane-actual", true, actions)

	if len(enabled.Screens) != len(disabled.Screens) {
		t.Fatalf("step count mismatch: enabled=%d disabled=%d", len(enabled.Screens), len(disabled.Screens))
	}
	for i := range enabled.Screens {
		left := normalizeComparableHeight(comparableScreenLines(enabled.Screens[i]), 40)
		right := normalizeComparableHeight(comparableScreenLines(disabled.Screens[i]), 40)
		if reflect.DeepEqual(left, right) {
			continue
		}
		t.Fatalf(
			"actual run diverged at step %d label=%s\nenabled=%s\ndisabled=%s\nenabled_stream_sample=%q\ndisabled_stream_sample=%q",
			i+1,
			enabled.Labels[i],
			formatComparableLines(left[:minInt(8, len(left))]),
			formatComparableLines(right[:minInt(8, len(right))]),
			debugEscape(enabled.RawSteps[i], 320),
			debugEscape(disabled.RawSteps[i], 320),
		)
	}
	t.Fatalf("did not reproduce an actual-run intermediate divergence across %d steps", len(enabled.Screens))
}

func TestDebugSinglePaneNvimActualRunMatchesObserverRenderPerStep(t *testing.T) {
	if os.Getenv("TERMX_RUN_NVIM_TRACE") != "1" {
		t.Skip("set TERMX_RUN_NVIM_TRACE=1 to run the single-pane nvim observer parity trace")
	}
	if testing.Short() {
		t.Skip("debug trace")
	}
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	actions := []struct {
		label string
		seq   []byte
	}{
		{label: "down_12_a", seq: bytesRepeat(0x05, 12)},
		{label: "down_12_b", seq: bytesRepeat(0x05, 12)},
		{label: "down_12_c", seq: bytesRepeat(0x05, 12)},
		{label: "up_6", seq: bytesRepeat(0x19, 6)},
		{label: "down_12_d", seq: bytesRepeat(0x05, 12)},
		{label: "down_12_e", seq: bytesRepeat(0x05, 12)},
		{label: "alternating_16", seq: alternatingBytes(0x05, 0x19, 8)},
		{label: "down_12_f", seq: bytesRepeat(0x05, 12)},
	}

	actual := captureActualRunStepScreens(t, "single-pane-observer", false, actions)
	observer := captureObserverRenderStepScreens(t, "single-pane-observer", actions)

	if len(actual.Screens) != len(observer.Screens) {
		t.Fatalf("step count mismatch: actual=%d observer=%d", len(actual.Screens), len(observer.Screens))
	}
	for i := range actual.Screens {
		actualScreen := normalizeComparableHeight(comparableScreenLines(actual.Screens[i]), 40)
		observerScreen := normalizeComparableHeight(comparableScreenLines(observer.Screens[i]), 40)
		if reflect.DeepEqual(actualScreen, observerScreen) {
			continue
		}
		t.Fatalf(
			"actual run diverged from observer render at step %d label=%s\nactual=%s\nobserver=%s\nactual_stream_sample=%q",
			i+1,
			actual.Labels[i],
			formatComparableLines(actualScreen[:minInt(8, len(actualScreen))]),
			formatComparableLines(observerScreen[:minInt(8, len(observerScreen))]),
			debugEscape(actual.RawSteps[i], 320),
		)
	}
	t.Skipf("actual-run vs observer stayed aligned across %d steps", len(actual.Screens))
}

func TestDebugSinglePaneNvimActualFramesMatchObserverFrames(t *testing.T) {
	if os.Getenv("TERMX_RUN_NVIM_TRACE") != "1" {
		t.Skip("set TERMX_RUN_NVIM_TRACE=1 to run the single-pane nvim frame parity trace")
	}
	if testing.Short() {
		t.Skip("debug trace")
	}
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	actions := [][]byte{
		bytesRepeat(0x05, 12),
		bytesRepeat(0x05, 12),
		bytesRepeat(0x05, 12),
		bytesRepeat(0x19, 6),
		bytesRepeat(0x05, 12),
		bytesRepeat(0x05, 12),
		bytesRepeat(0x19, 6),
		bytesRepeat(0x05, 12),
	}

	observer := captureObserverRapidBurstFrames(t, "single-pane-frame-parity", actions)
	actual := captureActualRunFrameScreens(t, "single-pane-frame-parity", actions)

	if len(actual.Screens) == 0 {
		t.Fatal("expected actual run to emit synchronized-output frames")
	}
	if len(observer) == 0 {
		t.Fatal("expected observer to capture invalidated render frames")
	}

	observerIndex := 0
	for actualIndex := range actual.Screens {
		actualScreen := normalizeComparableHeight(comparableScreenLines(actual.Screens[actualIndex]), 40)
		matched := false
		for observerIndex < len(observer) {
			observerScreen := normalizeComparableHeight(comparableScreenLines(observer[observerIndex].RenderLines), 40)
			if reflect.DeepEqual(actualScreen, observerScreen) {
				observerIndex++
				matched = true
				break
			}
			observerIndex++
		}
		if matched {
			continue
		}
		t.Fatalf(
			"actual run emitted a frame not present in observer render sequence at actual_frame=%d\nactual=%s\nraw=%q",
			actualIndex+1,
			formatComparableLines(actualScreen[:minInt(8, len(actualScreen))]),
			debugEscape(actual.RawFrames[actualIndex], 320),
		)
	}
	t.Skipf("actual run frames matched observer render subsequence actual=%d observer=%d", len(actual.Screens), len(observer))
}

type nvimPipelineHarness struct {
	ctx        context.Context
	cancel     context.CancelFunc
	model      *Model
	asyncMsgs  chan tea.Msg
	control    *protocol.Client
	server     *termx.Server
	serverDone chan error
	terminalID string
	width      int
	height     int
}

func startNvimPipelineHarness(t *testing.T, name string) *nvimPipelineHarness {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	socketPath := filepath.Join(t.TempDir(), name+".sock")
	srv := termx.NewServer(termx.WithSocketPath(socketPath))
	serverDone := make(chan error, 1)
	go func() { serverDone <- srv.ListenAndServe(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = srv.Shutdown(context.Background())
		select {
		case <-serverDone:
		case <-time.After(3 * time.Second):
		}
	})
	if err := waitTestSocket(socketPath, 5*time.Second); err != nil {
		t.Fatalf("server socket never appeared: %v", err)
	}

	controlTransport, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial control client: %v", err)
	}
	controlClient := protocol.NewClient(controlTransport)
	if err := controlClient.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello control client: %v", err)
	}
	t.Cleanup(func() { _ = controlClient.Close() })

	tmpFile := filepath.Join(t.TempDir(), name+".txt")
	var lines []string
	for i := 1; i <= 300; i++ {
		lines = append(lines, fmt.Sprintf("line %03d %s", i, strings.Repeat("x", 40)))
	}
	if err := os.WriteFile(tmpFile, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	created, err := controlClient.Create(ctx, protocol.CreateParams{
		Command: []string{
			"nvim",
			"-u", "NONE",
			"-n",
			"-c", "set nomore nonumber norelativenumber laststatus=0 cmdheight=0 noshowmode nowrap",
			tmpFile,
		},
		Name: name,
		Size: protocol.Size{Cols: 120, Rows: 40},
	})
	if err != nil {
		t.Fatalf("create terminal: %v", err)
	}

	appTransport, err := unixtransport.Dial(socketPath)
	if err != nil {
		t.Fatalf("dial app client: %v", err)
	}
	appClient := protocol.NewClient(appTransport)
	if err := appClient.Hello(ctx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello app client: %v", err)
	}
	t.Cleanup(func() { _ = appClient.Close() })

	model := New(shared.Config{AttachID: created.TerminalID}, nil, runtime.New(bridge.NewProtocolClient(appClient)))
	model.width = 120
	model.height = 40

	asyncMsgs := make(chan tea.Msg, 512)
	model.SetSendFunc(func(msg tea.Msg) {
		select {
		case asyncMsgs <- msg:
		default:
			panic(fmt.Sprintf("nvim pipeline async queue full for %T", msg))
		}
	})

	e2eDrain(t, model, model.Init())
	_, cmd := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	e2eDrain(t, model, cmd)

	return &nvimPipelineHarness{
		ctx:        ctx,
		cancel:     cancel,
		model:      model,
		asyncMsgs:  asyncMsgs,
		control:    controlClient,
		server:     srv,
		serverDone: serverDone,
		terminalID: created.TerminalID,
		width:      120,
		height:     40,
	}
}

func (h *nvimPipelineHarness) Close(t *testing.T) {
	t.Helper()
	if h == nil {
		return
	}
	h.cancel()
	_ = h.server.Shutdown(context.Background())
	select {
	case err := <-h.serverDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("server shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for pipeline server shutdown")
	}
}

func (h *nvimPipelineHarness) waitForInitialScreen(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_ = h.model.View()
		if strings.Contains(xansi.Strip(h.model.lastViewFrame), "line 001") {
			return
		}
		h.drainAsync(t, 200*time.Millisecond)
	}
	t.Fatalf("timeout waiting for initial nvim screen\nview=%s", xansi.Strip(h.model.lastViewFrame))
}

func (h *nvimPipelineHarness) sendInputAndWait(t *testing.T, data []byte, quiet time.Duration) []nvimPipelineFrame {
	t.Helper()
	pane := h.model.workbench.ActivePane()
	if pane == nil {
		t.Fatal("expected active pane")
	}
	_, cmd := h.model.Update(input.TerminalInput{PaneID: pane.ID, Data: data})
	e2eDrain(t, h.model, cmd)
	return h.drainAsync(t, quiet)
}

func (h *nvimPipelineHarness) drainAsync(t *testing.T, quiet time.Duration) []nvimPipelineFrame {
	t.Helper()
	timer := time.NewTimer(quiet)
	defer timer.Stop()

	var frames []nvimPipelineFrame
	for {
		select {
		case msg := <-h.asyncMsgs:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(quiet)
			e2eDrainMsg(t, h.model, msg, false)
			if _, ok := msg.(InvalidateMsg); ok {
				frames = append(frames, h.captureFrame(t))
			}
		case <-timer.C:
			return frames
		case <-h.ctx.Done():
			t.Fatalf("context expired while draining async messages")
		}
	}
}

type nvimPipelineFrame struct {
	RenderLines        []string
	Cursor             string
	ServerLines        []string
	LocalSnapshotLines []string
	LocalSurfaceLines  []string
}

func (h *nvimPipelineHarness) captureFrame(t *testing.T) nvimPipelineFrame {
	t.Helper()
	_ = h.model.View()

	serverSnapshot, err := h.control.Snapshot(h.ctx, h.terminalID, 0, 0)
	if err != nil {
		t.Fatalf("load authoritative snapshot: %v", err)
	}

	terminal := h.model.runtime.Registry().Get(h.terminalID)
	if terminal == nil {
		t.Fatal("expected terminal in runtime registry")
	}
	localSnapshot := (*protocol.Snapshot)(nil)
	if terminal.Snapshot != nil {
		localSnapshot = terminal.Snapshot
	}
	localSurfaceLines := []string(nil)
	if terminal.VTerm != nil {
		localSurfaceLines = comparableScreenLines(vtermScreenLines(terminal.VTerm.ScreenContent()))
	}

	return nvimPipelineFrame{
		RenderLines:        append([]string(nil), strings.Split(h.model.lastViewFrame, "\n")...),
		Cursor:             h.model.lastViewCursor,
		ServerLines:        comparableScreenLines(snapshotScreenLines(serverSnapshot)),
		LocalSnapshotLines: comparableScreenLines(snapshotScreenLines(localSnapshot)),
		LocalSurfaceLines:  localSurfaceLines,
	}
}

func snapshotScreenLines(snapshot *protocol.Snapshot) []string {
	if snapshot == nil {
		return nil
	}
	lines := make([]string, 0, len(snapshot.Screen.Cells))
	for _, row := range snapshot.Screen.Cells {
		var b strings.Builder
		for _, cell := range row {
			if cell.Width == 0 && cell.Content == "" {
				continue
			}
			b.WriteString(cell.Content)
		}
		lines = append(lines, b.String())
	}
	return lines
}

type writerReplayResult struct {
	RawANSI      []string
	TmuxScreens  [][]string
	VTermScreens [][]string
}

func replayPipelineFramesThroughWriterAndTmux(t *testing.T, frames []nvimPipelineFrame, enableVerticalScroll bool) writerReplayResult {
	t.Helper()

	sink := &cursorWriterProbeTTY{}
	writer := newOutputCursorWriter(sink)
	writer.SetVerticalScrollEnabled(enableVerticalScroll)
	if err := writer.enterDirectTerminal(); err != nil {
		t.Fatalf("enter direct terminal: %v", err)
	}

	tmuxHarness := startTmuxReplayHarness(t, 120, 40)
	defer tmuxHarness.Close()

	vt := localvterm.New(120, 40, 0, nil)
	result := writerReplayResult{
		RawANSI:      make([]string, 0, len(frames)),
		TmuxScreens:  make([][]string, 0, len(frames)),
		VTermScreens: make([][]string, 0, len(frames)),
	}

	writeCount := 0
	appendNewWrites := func() string {
		sink.mu.Lock()
		defer sink.mu.Unlock()
		if writeCount >= len(sink.writes) {
			return ""
		}
		payload := strings.Join(sink.writes[writeCount:], "")
		writeCount = len(sink.writes)
		return payload
	}

	initial := appendNewWrites()
	if initial != "" {
		tmuxHarness.Append(t, initial)
		if _, err := vt.Write([]byte(initial)); err != nil {
			t.Fatalf("replay initial terminal setup into vterm: %v", err)
		}
	}

	for _, frame := range frames {
		if err := writer.WriteFrameLines(frame.RenderLines, frame.Cursor); err != nil {
			t.Fatalf("write frame lines: %v", err)
		}
		raw := appendNewWrites()
		result.RawANSI = append(result.RawANSI, raw)
		tmuxHarness.Append(t, raw)
		result.TmuxScreens = append(result.TmuxScreens, comparableScreenLines(tmuxHarness.Capture(t)))
		if _, err := vt.Write([]byte(raw)); err != nil {
			t.Fatalf("replay frame into vterm: %v", err)
		}
		result.VTermScreens = append(result.VTermScreens, comparableScreenLines(vtermScreenLines(vt.ScreenContent())))
	}

	return result
}

type tmuxReplayHarness struct {
	sessionName string
	ttyPath     string
}

func startTmuxReplayHarness(t *testing.T, width, height int) *tmuxReplayHarness {
	t.Helper()

	sessionName := "termx-pipeline-" + strings.ReplaceAll(strconvQuoteInt(time.Now().UnixNano()), "-", "n")
	cmd := exec.Command(
		"tmux", "new-session",
		"-d",
		"-s", sessionName,
		"-x", fmt.Sprintf("%d", width),
		"-y", fmt.Sprintf("%d", height),
		"sh", "-lc", "stty raw -echo; cat",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("start tmux session: %v: %s", err, output)
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	})

	ttyPathBytes, err := exec.Command("tmux", "display-message", "-p", "-t", sessionName, "#{pane_tty}").CombinedOutput()
	if err != nil {
		t.Fatalf("resolve tmux pane tty: %v: %s", err, ttyPathBytes)
	}
	ttyPath := strings.TrimSpace(string(ttyPathBytes))
	if ttyPath == "" {
		t.Fatal("tmux pane tty was empty")
	}

	return &tmuxReplayHarness{sessionName: sessionName, ttyPath: ttyPath}
}

func (h *tmuxReplayHarness) Append(t *testing.T, stream string) {
	t.Helper()
	if h == nil || stream == "" {
		return
	}
	tty, err := os.OpenFile(h.ttyPath, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open tmux pane tty %q: %v", h.ttyPath, err)
	}
	if _, err := tty.WriteString(stream); err != nil {
		_ = tty.Close()
		t.Fatalf("write stream into tmux pane: %v", err)
	}
	_ = tty.Close()
	time.Sleep(80 * time.Millisecond)
}

func (h *tmuxReplayHarness) Capture(t *testing.T) []string {
	t.Helper()
	if h == nil {
		return nil
	}
	captured, err := exec.Command("tmux", "capture-pane", "-p", "-t", h.sessionName).CombinedOutput()
	if err != nil {
		t.Fatalf("capture tmux pane: %v: %s", err, captured)
	}
	text := strings.TrimSuffix(string(captured), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func (h *tmuxReplayHarness) Close() {
	if h == nil || h.sessionName == "" {
		return
	}
	_ = exec.Command("tmux", "kill-session", "-t", h.sessionName).Run()
}

func comparableScreenLines(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, len(lines))
	for i := range lines {
		out[i] = strings.TrimRight(xansi.Strip(lines[i]), " ")
	}
	return out
}

func hasVisibleText(lines []string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			return true
		}
	}
	return false
}

func normalizeComparableHeight(lines []string, height int) []string {
	if height <= 0 {
		return append([]string(nil), lines...)
	}
	out := make([]string, height)
	for i := 0; i < height && i < len(lines); i++ {
		out[i] = lines[i]
	}
	return out
}

type actualRunStepCapture struct {
	Labels   []string
	RawSteps []string
	Screens  [][]string
}

func captureActualRunStepScreens(t *testing.T, name string, disableVerticalScroll bool, actions []struct {
	label string
	seq   []byte
}) actualRunStepCapture {
	t.Helper()

	restoreDisableEnv := setTempEnv("TERMX_DISABLE_VERTICAL_SCROLL", disableVerticalScroll)
	defer restoreDisableEnv()

	harness := startNvimPerfHarness(t, name)
	defer harness.Close(t)

	waitForPTYOutputLength(t, harness.ctx, harness.recorder, 3000)
	waitForPTYQuiet(t, harness.ctx, harness.recorder, 300*time.Millisecond)
	harness.moveToMiddle(t)

	out := actualRunStepCapture{
		Labels:   make([]string, 0, len(actions)),
		RawSteps: make([]string, 0, len(actions)),
		Screens:  make([][]string, 0, len(actions)),
	}
	for _, action := range actions {
		before := len(harness.recorder.Text())
		if _, err := harness.ptmx.Write(action.seq); err != nil {
			t.Fatalf("write %s: %v", action.label, err)
		}
		waitForPTYGrowthIfAny(t, harness.ctx, harness.recorder, before, 2*time.Second)
		waitForPTYQuiet(t, harness.ctx, harness.recorder, 180*time.Millisecond)

		rawStream := harness.recorder.Text()
		stream := sanitizeReplayStream(rawStream)
		out.Labels = append(out.Labels, action.label)
		out.RawSteps = append(out.RawSteps, rawStream[before:])
		out.Screens = append(out.Screens, renderStreamThroughTmux(t, stream, 120, 40))
	}
	return out
}

type actualRunFrameCapture struct {
	RawFrames []string
	Screens   [][]string
}

func captureObserverRapidBurstFrames(t *testing.T, name string, actions [][]byte) []nvimPipelineFrame {
	t.Helper()

	harness := startNvimPipelineHarness(t, name)
	defer harness.Close(t)

	harness.waitForInitialScreen(t)
	harness.drainAsync(t, 200*time.Millisecond)
	harness.sendInputAndWait(t, []byte("50G"), 300*time.Millisecond)

	for _, action := range actions {
		pane := harness.model.workbench.ActivePane()
		if pane == nil {
			t.Fatal("expected active pane during observer burst")
		}
		_, cmd := harness.model.Update(input.TerminalInput{PaneID: pane.ID, Data: action})
		e2eDrain(t, harness.model, cmd)
		time.Sleep(18 * time.Millisecond)
	}
	return harness.drainAsync(t, 320*time.Millisecond)
}

func captureActualRunFrameScreens(t *testing.T, name string, actions [][]byte) actualRunFrameCapture {
	t.Helper()

	harness := startNvimPerfHarness(t, name)
	defer harness.Close(t)

	waitForPTYOutputLength(t, harness.ctx, harness.recorder, 3000)
	waitForPTYQuiet(t, harness.ctx, harness.recorder, 300*time.Millisecond)
	harness.moveToMiddle(t)
	waitForPTYQuiet(t, harness.ctx, harness.recorder, 250*time.Millisecond)

	baseStream := sanitizeReplayStream(harness.recorder.Text())
	before := len(harness.recorder.Text())
	for _, action := range actions {
		if _, err := harness.ptmx.Write(action); err != nil {
			t.Fatalf("write burst action: %v", err)
		}
		time.Sleep(18 * time.Millisecond)
	}
	waitForPTYGrowthIfAny(t, harness.ctx, harness.recorder, before, 2*time.Second)
	waitForPTYQuiet(t, harness.ctx, harness.recorder, 320*time.Millisecond)

	rawDelta := sanitizeReplayStream(harness.recorder.Text()[before:])
	frameChunks := extractSynchronizedOutputFrames(rawDelta)
	tmuxHarness := startTmuxReplayHarness(t, 120, 40)
	defer tmuxHarness.Close()
	tmuxHarness.Append(t, baseStream)

	out := actualRunFrameCapture{
		RawFrames: make([]string, 0, len(frameChunks)),
		Screens:   make([][]string, 0, len(frameChunks)),
	}
	for _, chunk := range frameChunks {
		tmuxHarness.Append(t, chunk)
		out.RawFrames = append(out.RawFrames, chunk)
		out.Screens = append(out.Screens, tmuxHarness.Capture(t))
	}
	return out
}

func captureObserverRenderStepScreens(t *testing.T, name string, actions []struct {
	label string
	seq   []byte
}) actualRunStepCapture {
	t.Helper()

	harness := startNvimPipelineHarness(t, name)
	defer harness.Close(t)

	harness.waitForInitialScreen(t)
	harness.drainAsync(t, 200*time.Millisecond)
	harness.sendInputAndWait(t, []byte("50G"), 300*time.Millisecond)

	out := actualRunStepCapture{
		Labels:   make([]string, 0, len(actions)),
		RawSteps: make([]string, 0, len(actions)),
		Screens:  make([][]string, 0, len(actions)),
	}
	for _, action := range actions {
		frames := harness.sendInputAndWait(t, action.seq, 220*time.Millisecond)
		_ = harness.model.View()
		screen := comparableScreenLines(strings.Split(harness.model.lastViewFrame, "\n"))
		if len(frames) > 0 {
			screen = comparableScreenLines(frames[len(frames)-1].RenderLines)
		}
		out.Labels = append(out.Labels, action.label)
		out.RawSteps = append(out.RawSteps, debugEscape(string(action.seq), 64))
		out.Screens = append(out.Screens, screen)
	}
	return out
}

func formatComparableLines(lines []string) string {
	if len(lines) == 0 {
		return "<empty>"
	}
	quoted := make([]string, 0, len(lines))
	for _, line := range lines {
		quoted = append(quoted, fmt.Sprintf("%q", line))
	}
	return strings.Join(quoted, "\n")
}

func strconvQuoteInt(value int64) string {
	return fmt.Sprintf("%d", value)
}

func sanitizeReplayStream(stream string) string {
	if stream == "" {
		return ""
	}
	stream = strings.ReplaceAll(stream, hostEmojiVariationProbeSequence, "")
	stream = strings.ReplaceAll(stream, xansi.RequestForegroundColor, "")
	stream = strings.ReplaceAll(stream, xansi.RequestBackgroundColor, "")
	stream = strings.ReplaceAll(stream, requestTerminalPaletteQueries(), "")
	return stream
}

func extractSynchronizedOutputFrames(stream string) []string {
	if stream == "" {
		return nil
	}
	frames := make([]string, 0, 16)
	for pos := 0; pos < len(stream); {
		start := strings.Index(stream[pos:], synchronizedOutputBegin)
		if start < 0 {
			break
		}
		start += pos
		end := strings.Index(stream[start+len(synchronizedOutputBegin):], synchronizedOutputEnd)
		if end < 0 {
			break
		}
		end += start + len(synchronizedOutputBegin)
		frames = append(frames, stream[start:end+len(synchronizedOutputEnd)])
		pos = end + len(synchronizedOutputEnd)
	}
	return frames
}
