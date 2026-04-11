package app

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDebugNvimScrollStressTmuxParity(t *testing.T) {
	if os.Getenv("TERMX_RUN_NVIM_TRACE") != "1" {
		t.Skip("set TERMX_RUN_NVIM_TRACE=1 to run the tmux-backed nvim stress trace")
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
		{label: "down_burst_12", seq: bytesRepeat(0x05, 12)},
		{label: "down_burst_12", seq: bytesRepeat(0x05, 12)},
		{label: "down_burst_12", seq: bytesRepeat(0x05, 12)},
		{label: "down_burst_12", seq: bytesRepeat(0x05, 12)},
		{label: "down_burst_12", seq: bytesRepeat(0x05, 12)},
		{label: "down_burst_12", seq: bytesRepeat(0x05, 12)},
		{label: "up_burst_6", seq: bytesRepeat(0x19, 6)},
		{label: "up_burst_6", seq: bytesRepeat(0x19, 6)},
		{label: "down_burst_12", seq: bytesRepeat(0x05, 12)},
		{label: "down_burst_12", seq: bytesRepeat(0x05, 12)},
		{label: "alternating_16", seq: alternatingBytes(0x05, 0x19, 8)},
		{label: "down_burst_12", seq: bytesRepeat(0x05, 12)},
		{label: "down_burst_12", seq: bytesRepeat(0x05, 12)},
		{label: "insert", seq: []byte("120Gzz0iHELLO\x1b")},
		{label: "down_burst_12", seq: bytesRepeat(0x05, 12)},
		{label: "down_burst_12", seq: bytesRepeat(0x05, 12)},
	}

	enabledStream := captureNvimStressStream(t, "tmux-parity", false, 0, actions)
	disabledStream := captureNvimStressStream(t, "tmux-parity", true, 0, actions)

	enabledLines := renderStreamThroughTmux(t, enabledStream, 120, 40)
	disabledLines := renderStreamThroughTmux(t, disabledStream, 120, 40)

	if len(enabledLines) != len(disabledLines) {
		t.Fatalf("tmux capture height mismatch: enabled=%d disabled=%d", len(enabledLines), len(disabledLines))
	}
	for i := range enabledLines {
		if enabledLines[i] == disabledLines[i] {
			continue
		}
		t.Fatalf("tmux render diverged on row %d\nenabled=%q\ndisabled=%q", i+1, enabledLines[i], disabledLines[i])
	}
}

func TestDebugNvimScrollStressTmuxFaultInjectionDiverges(t *testing.T) {
	if os.Getenv("TERMX_RUN_NVIM_TRACE") != "1" {
		t.Skip("set TERMX_RUN_NVIM_TRACE=1 to run the tmux-backed nvim stress trace")
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
		{label: "down_burst_12", seq: bytesRepeat(0x05, 12)},
		{label: "down_burst_12", seq: bytesRepeat(0x05, 12)},
		{label: "down_burst_12", seq: bytesRepeat(0x05, 12)},
		{label: "down_burst_12", seq: bytesRepeat(0x05, 12)},
		{label: "insert", seq: []byte("120Gzz0iHELLO\x1b")},
		{label: "down_burst_12", seq: bytesRepeat(0x05, 12)},
		{label: "down_burst_12", seq: bytesRepeat(0x05, 12)},
	}

	baselineStream := captureNvimStressStream(t, "tmux-fault", false, 0, actions)
	faultStream := captureNvimStressStream(t, "tmux-fault", false, 2, actions)

	baselineLines := renderStreamThroughTmux(t, baselineStream, 120, 40)
	faultLines := renderStreamThroughTmux(t, faultStream, 120, 40)

	if len(baselineLines) != len(faultLines) {
		t.Fatalf("tmux capture height mismatch: baseline=%d fault=%d", len(baselineLines), len(faultLines))
	}
	for i := range baselineLines {
		if baselineLines[i] == faultLines[i] {
			continue
		}
		return
	}
	t.Fatal("expected fault injection to diverge from baseline tmux render, but final screens matched")
}

func captureNvimStressStream(t *testing.T, name string, disableVerticalScroll bool, faultEvery int, actions []struct {
	label string
	seq   []byte
}) string {
	t.Helper()

	restoreDisableEnv := setTempEnv("TERMX_DISABLE_VERTICAL_SCROLL", disableVerticalScroll)
	defer restoreDisableEnv()
	restoreFaultEnv := setTempIntEnv("TERMX_DEBUG_FAULT_SCROLL_DROP_REMAINDER_EVERY", faultEvery)
	defer restoreFaultEnv()

	harness := startNvimPerfHarness(t, name)
	defer harness.Close(t)

	waitForPTYOutputLength(t, harness.ctx, harness.recorder, 3000)
	waitForPTYQuiet(t, harness.ctx, harness.recorder, 300*time.Millisecond)
	harness.moveToMiddle(t)

	for _, action := range actions {
		before := len(harness.recorder.Text())
		if _, err := harness.ptmx.Write(action.seq); err != nil {
			t.Fatalf("write %s: %v", action.label, err)
		}
		waitForPTYGrowthIfAny(t, harness.ctx, harness.recorder, before, 2*time.Second)
		waitForPTYQuiet(t, harness.ctx, harness.recorder, 180*time.Millisecond)
	}

	return harness.recorder.Text()
}

func renderStreamThroughTmux(t *testing.T, stream string, width, height int) []string {
	t.Helper()

	sessionName := "termx-debug-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	cmd := exec.Command(
		"tmux", "new-session",
		"-d",
		"-s", sessionName,
		"-x", strconv.Itoa(width),
		"-y", strconv.Itoa(height),
		"sh", "-lc", "stty raw -echo; cat",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("start tmux session: %v: %s", err, output)
	}
	defer func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	}()

	ttyPathBytes, err := exec.Command("tmux", "display-message", "-p", "-t", sessionName, "#{pane_tty}").CombinedOutput()
	if err != nil {
		t.Fatalf("resolve tmux pane tty: %v: %s", err, ttyPathBytes)
	}
	ttyPath := strings.TrimSpace(string(ttyPathBytes))
	if ttyPath == "" {
		t.Fatal("tmux pane tty was empty")
	}

	tty, err := os.OpenFile(ttyPath, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open tmux pane tty %q: %v", ttyPath, err)
	}
	if _, err := tty.WriteString(stream); err != nil {
		_ = tty.Close()
		t.Fatalf("write stream into tmux pane: %v", err)
	}
	_ = tty.Close()

	time.Sleep(300 * time.Millisecond)

	captured, err := exec.Command("tmux", "capture-pane", "-p", "-t", sessionName).CombinedOutput()
	if err != nil {
		t.Fatalf("capture tmux pane: %v: %s", err, captured)
	}
	text := strings.TrimSuffix(string(captured), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func setTempEnv(key string, enabled bool) func() {
	original, had := os.LookupEnv(key)
	if enabled {
		_ = os.Setenv(key, "1")
	} else {
		_ = os.Unsetenv(key)
	}
	return func() {
		if had {
			_ = os.Setenv(key, original)
			return
		}
		_ = os.Unsetenv(key)
	}
}

func setTempIntEnv(key string, value int) func() {
	original, had := os.LookupEnv(key)
	if value > 0 {
		_ = os.Setenv(key, strconv.Itoa(value))
	} else {
		_ = os.Unsetenv(key)
	}
	return func() {
		if had {
			_ = os.Setenv(key, original)
			return
		}
		_ = os.Unsetenv(key)
	}
}
