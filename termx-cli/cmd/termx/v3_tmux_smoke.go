package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-tui-v3/render"
)

type v3TmuxSmokeResult struct {
	Session     string
	ArtifactDir string
	ANSIPath    string
	PlainPath   string
	Captured    string
	SentInput   string
}

type v3TmuxTerminalSmokeResult struct {
	Session      string
	TerminalID   string
	ArtifactDir  string
	ANSIPath     string
	PlainPath    string
	DaemonLog    string
	SocketPath   string
	TimelinePath string
	Captured     string
	SentInput    string
}

type v3TmuxResizeSmokeResult struct {
	Session      string
	TerminalID   string
	ArtifactDir  string
	ANSIPath     string
	PlainPath    string
	DaemonLog    string
	SocketPath   string
	TimelinePath string
	Captured     string
	BeforeSize   string
	AfterSize    string
	WindowSize   string
}

type v3TmuxANSISmokeResult struct {
	Session      string
	TerminalID   string
	ArtifactDir  string
	ANSIPath     string
	PlainPath    string
	DaemonLog    string
	SocketPath   string
	TimelinePath string
	Captured     string
	ANSICaptured string
}

type v3TmuxVisualCompareResult struct {
	Session          string
	ArtifactDir      string
	CurrentANSIPath  string
	CurrentPlainPath string
	TargetPath       string
	DiffPath         string
	SummaryPath      string
	Mismatches       int
}

type v3TmuxStabilitySmokeResult struct {
	ArtifactDir  string
	TimelinePath string
	Rounds       int
	Artifacts    []string
}

func runV3TmuxSmoke(ctx context.Context) (v3TmuxSmokeResult, error) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return v3TmuxSmokeResult{}, fmt.Errorf("tmux smoke requires tmux in PATH: %w", err)
	}
	artifactDir, err := os.MkdirTemp("", "termx-v3-tmux-smoke-*")
	if err != nil {
		return v3TmuxSmokeResult{}, err
	}
	session := fmt.Sprintf("termx-v3-smoke-%d", time.Now().UnixNano())
	target := session + ":0.0"
	cleanup := func() {
		_ = runTmuxCommand(context.Background(), "kill-session", "-t", session)
	}
	defer cleanup()

	script := "printf 'termx tmux harness ready\\n'; " +
		"IFS= read -r line; " +
		"printf 'termx tmux input:%s\\n' \"$line\"; " +
		"sleep 30"
	if err := runTmuxCommand(ctx, "new-session", "-d", "-x", "100", "-y", "30", "-s", session, "/bin/sh", "-lc", script); err != nil {
		_ = os.RemoveAll(artifactDir)
		return v3TmuxSmokeResult{}, err
	}
	if err := waitForTmuxCapture(ctx, target, "termx tmux harness ready", 2*time.Second); err != nil {
		_ = os.RemoveAll(artifactDir)
		return v3TmuxSmokeResult{}, err
	}
	sent := "hello-from-tmux"
	if err := runTmuxCommand(ctx, "send-keys", "-t", target, sent, "Enter"); err != nil {
		_ = os.RemoveAll(artifactDir)
		return v3TmuxSmokeResult{}, err
	}
	if err := waitForTmuxCapture(ctx, target, "termx tmux input:"+sent, 2*time.Second); err != nil {
		_ = os.RemoveAll(artifactDir)
		return v3TmuxSmokeResult{}, err
	}
	ansi, err := captureTmuxPane(ctx, target, true)
	if err != nil {
		_ = os.RemoveAll(artifactDir)
		return v3TmuxSmokeResult{}, err
	}
	plain, err := captureTmuxPane(ctx, target, false)
	if err != nil {
		_ = os.RemoveAll(artifactDir)
		return v3TmuxSmokeResult{}, err
	}
	ansiPath := filepath.Join(artifactDir, "capture.ansi")
	plainPath := filepath.Join(artifactDir, "capture.txt")
	if err := os.WriteFile(ansiPath, []byte(ansi), 0o600); err != nil {
		_ = os.RemoveAll(artifactDir)
		return v3TmuxSmokeResult{}, err
	}
	if err := os.WriteFile(plainPath, []byte(plain), 0o600); err != nil {
		_ = os.RemoveAll(artifactDir)
		return v3TmuxSmokeResult{}, err
	}
	return v3TmuxSmokeResult{
		Session:     session,
		ArtifactDir: artifactDir,
		ANSIPath:    ansiPath,
		PlainPath:   plainPath,
		Captured:    plain,
		SentInput:   sent,
	}, nil
}

func runV3TmuxTerminalSmoke(ctx context.Context, termxBin string) (v3TmuxTerminalSmokeResult, error) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return v3TmuxTerminalSmokeResult{}, fmt.Errorf("tmux terminal smoke requires tmux in PATH: %w", err)
	}
	if termxBin == "" {
		return v3TmuxTerminalSmokeResult{}, fmt.Errorf("tmux terminal smoke requires a termx binary path")
	}
	artifactDir, err := os.MkdirTemp("", "termx-v3-tmux-terminal-*")
	if err != nil {
		return v3TmuxTerminalSmokeResult{}, err
	}
	removeArtifacts := true
	defer func() {
		if removeArtifacts {
			_ = os.RemoveAll(artifactDir)
		}
	}()

	socketPath := filepath.Join(artifactDir, "termx-core-v2.sock")
	daemonLog := filepath.Join(artifactDir, "daemon.log")
	timelinePath := filepath.Join(artifactDir, "timeline.txt")
	session := fmt.Sprintf("termx-v3-terminal-%d", time.Now().UnixNano())
	target := session + ":0.0"
	appendTimeline := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...) + "\n"
		_ = appendFile(timelinePath, line)
	}

	daemonStdout, err := os.Create(filepath.Join(artifactDir, "daemon.stdout"))
	if err != nil {
		return v3TmuxTerminalSmokeResult{}, err
	}
	defer daemonStdout.Close()
	daemonStderr, err := os.Create(filepath.Join(artifactDir, "daemon.stderr"))
	if err != nil {
		return v3TmuxTerminalSmokeResult{}, err
	}
	defer daemonStderr.Close()
	daemonCtx, stopDaemonCtx := context.WithCancel(ctx)
	defer stopDaemonCtx()
	daemonCmd := exec.CommandContext(daemonCtx, termxBin, "--socket", socketPath, "--log-file", daemonLog, "daemon")
	daemonCmd.Stdout = daemonStdout
	daemonCmd.Stderr = daemonStderr
	if err := daemonCmd.Start(); err != nil {
		return v3TmuxTerminalSmokeResult{}, fmt.Errorf("start core-v2 daemon: %w", err)
	}
	defer func() {
		stopDaemonCtx()
		if daemonCmd.Process != nil {
			_ = daemonCmd.Process.Kill()
		}
		_ = daemonCmd.Wait()
	}()
	appendTimeline("daemon start socket=%s log=%s", socketPath, daemonLog)
	if err := waitForSocket(socketPath, 5*time.Second, func() error {
		client, dialErr := dialV3Client(socketPath)
		if dialErr != nil {
			return dialErr
		}
		return client.Close()
	}); err != nil {
		return v3TmuxTerminalSmokeResult{}, err
	}
	appendTimeline("daemon ready")

	terminalIDPath := filepath.Join(artifactDir, "terminal.id")
	scriptPath := filepath.Join(artifactDir, "tmux-terminal-smoke.sh")
	terminalScript := "printf 'termx-pty-ready\\n'; while IFS= read -r line; do printf 'termx-pty-echo:%s\\n' \"$line\"; done"
	script := strings.Join([]string{
		"#!/bin/sh",
		"set -eu",
		"export TERM=xterm-256color",
		"export TERMX=0",
		"termx_bin=" + shellQuote(termxBin),
		"socket=" + shellQuote(socketPath),
		"log_file=" + shellQuote(daemonLog),
		"terminal_id_path=" + shellQuote(terminalIDPath),
		"ls_before_path=" + shellQuote(filepath.Join(artifactDir, "ls.before.txt")),
		"printf 'termx tmux terminal harness ready\\n'",
		"id=\"$($termx_bin --socket \"$socket\" --log-file \"$log_file\" v3 new --name tmux-e2e -- /bin/sh -c " + shellQuote(terminalScript) + ")\"",
		"printf '%s\\n' \"$id\" > \"$terminal_id_path\"",
		"printf 'termx terminal id:%s\\n' \"$id\"",
		"$termx_bin --socket \"$socket\" --log-file \"$log_file\" v3 ls > \"$ls_before_path\"",
		"exec $termx_bin --socket \"$socket\" --log-file \"$log_file\" v3 attach \"$id\"",
		"",
	}, "\n")
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return v3TmuxTerminalSmokeResult{}, err
	}
	appendTimeline("tmux script=%s", scriptPath)

	cleanupTmux := func() {
		_ = runTmuxCommand(context.Background(), "kill-session", "-t", session)
	}
	defer cleanupTmux()
	if err := runTmuxCommand(ctx, "new-session", "-d", "-x", "100", "-y", "30", "-s", session, "/bin/sh", scriptPath); err != nil {
		return v3TmuxTerminalSmokeResult{}, err
	}
	appendTimeline("tmux session=%s", session)
	terminalIDBytes, err := waitForFileContent(ctx, terminalIDPath, 5*time.Second)
	if err != nil {
		return v3TmuxTerminalSmokeResult{}, fmt.Errorf("read terminal id artifact: %w", err)
	}
	terminalID := strings.TrimSpace(string(terminalIDBytes))
	appendTimeline("terminal created id=%s", terminalID)
	if err := waitForTmuxCapture(ctx, target, "termx-pty-ready", 5*time.Second); err != nil {
		return v3TmuxTerminalSmokeResult{}, err
	}
	appendTimeline("attach rendered initial live surface")

	sent := "tmux-live-input"
	if err := runTmuxCommand(ctx, "send-keys", "-t", target, sent, "Enter"); err != nil {
		return v3TmuxTerminalSmokeResult{}, err
	}
	appendTimeline("tmux send-keys input=%s", sent)
	if err := waitForTmuxCapture(ctx, target, "termx-pty-echo:"+sent, 5*time.Second); err != nil {
		return v3TmuxTerminalSmokeResult{}, err
	}
	appendTimeline("live surface echoed input")

	ansi, err := captureTmuxPane(ctx, target, true)
	if err != nil {
		return v3TmuxTerminalSmokeResult{}, err
	}
	plain, err := captureTmuxPane(ctx, target, false)
	if err != nil {
		return v3TmuxTerminalSmokeResult{}, err
	}
	ansiPath := filepath.Join(artifactDir, "capture.ansi")
	plainPath := filepath.Join(artifactDir, "capture.txt")
	if err := os.WriteFile(ansiPath, []byte(ansi), 0o600); err != nil {
		return v3TmuxTerminalSmokeResult{}, err
	}
	if err := os.WriteFile(plainPath, []byte(plain), 0o600); err != nil {
		return v3TmuxTerminalSmokeResult{}, err
	}
	appendTimeline("captured ansi=%s plain=%s", ansiPath, plainPath)

	_ = runTermxCommand(context.Background(), termxBin, "--socket", socketPath, "--log-file", daemonLog, "v3", "kill", terminalID)
	_ = runTermxCommand(context.Background(), termxBin, "--socket", socketPath, "--log-file", daemonLog, "v3", "rm", terminalID)
	appendTimeline("terminal cleanup id=%s", terminalID)
	removeArtifacts = false
	return v3TmuxTerminalSmokeResult{
		Session:      session,
		TerminalID:   terminalID,
		ArtifactDir:  artifactDir,
		ANSIPath:     ansiPath,
		PlainPath:    plainPath,
		DaemonLog:    daemonLog,
		SocketPath:   socketPath,
		TimelinePath: timelinePath,
		Captured:     plain,
		SentInput:    sent,
	}, nil
}

func runV3TmuxResizeSmoke(ctx context.Context, termxBin string) (v3TmuxResizeSmokeResult, error) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return v3TmuxResizeSmokeResult{}, fmt.Errorf("tmux resize smoke requires tmux in PATH: %w", err)
	}
	if termxBin == "" {
		return v3TmuxResizeSmokeResult{}, fmt.Errorf("tmux resize smoke requires a termx binary path")
	}
	artifactDir, err := os.MkdirTemp("", "termx-v3-tmux-resize-*")
	if err != nil {
		return v3TmuxResizeSmokeResult{}, err
	}
	removeArtifacts := true
	defer func() {
		if removeArtifacts {
			_ = os.RemoveAll(artifactDir)
		}
	}()

	socketPath := filepath.Join(artifactDir, "termx-core-v2.sock")
	daemonLog := filepath.Join(artifactDir, "daemon.log")
	timelinePath := filepath.Join(artifactDir, "timeline.txt")
	session := fmt.Sprintf("termx-v3-resize-%d", time.Now().UnixNano())
	target := session + ":0.0"
	appendTimeline := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...) + "\n"
		_ = appendFile(timelinePath, line)
	}

	daemonCtx, stopDaemonCtx := context.WithCancel(ctx)
	defer stopDaemonCtx()
	daemonCmd := exec.CommandContext(daemonCtx, termxBin, "--socket", socketPath, "--log-file", daemonLog, "daemon")
	daemonCmd.Stdout = io.Discard
	daemonCmd.Stderr = io.Discard
	if err := daemonCmd.Start(); err != nil {
		return v3TmuxResizeSmokeResult{}, fmt.Errorf("start core-v2 daemon: %w", err)
	}
	defer func() {
		stopDaemonCtx()
		if daemonCmd.Process != nil {
			_ = daemonCmd.Process.Kill()
		}
		_ = daemonCmd.Wait()
	}()
	appendTimeline("daemon start socket=%s log=%s", socketPath, daemonLog)
	if err := waitForSocket(socketPath, 5*time.Second, func() error {
		client, dialErr := dialV3Client(socketPath)
		if dialErr != nil {
			return dialErr
		}
		return client.Close()
	}); err != nil {
		return v3TmuxResizeSmokeResult{}, err
	}
	appendTimeline("daemon ready")

	terminalIDPath := filepath.Join(artifactDir, "terminal.id")
	scriptPath := filepath.Join(artifactDir, "tmux-resize-smoke.sh")
	terminalScript := "printf 'termx-pty-ready\\n'\n" +
		"while IFS= read -r line; do\n" +
		"  case \"$line\" in\n" +
		"    size-*) set -- $(stty size); printf 'termx-pty-size:%s:%sx%s\\n' \"$line\" \"$2\" \"$1\" ;;\n" +
		"    *) printf 'termx-pty-echo:%s\\n' \"$line\" ;;\n" +
		"  esac\n" +
		"done\n"
	script := strings.Join([]string{
		"#!/bin/sh",
		"set -eu",
		"export TERM=xterm-256color",
		"export TERMX=0",
		"termx_bin=" + shellQuote(termxBin),
		"socket=" + shellQuote(socketPath),
		"log_file=" + shellQuote(daemonLog),
		"terminal_id_path=" + shellQuote(terminalIDPath),
		"printf 'termx tmux resize harness ready\\n'",
		"id=\"$($termx_bin --socket \"$socket\" --log-file \"$log_file\" v3 new --name tmux-resize -- /bin/sh -c " + shellQuote(terminalScript) + ")\"",
		"printf '%s\\n' \"$id\" > \"$terminal_id_path\"",
		"printf 'termx resize terminal id:%s\\n' \"$id\"",
		"exec $termx_bin --socket \"$socket\" --log-file \"$log_file\" v3 attach \"$id\"",
		"",
	}, "\n")
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return v3TmuxResizeSmokeResult{}, err
	}
	appendTimeline("tmux script=%s", scriptPath)

	cleanupTmux := func() {
		_ = runTmuxCommand(context.Background(), "kill-session", "-t", session)
	}
	defer cleanupTmux()
	if err := runTmuxCommand(ctx, "new-session", "-d", "-x", "100", "-y", "30", "-s", session, "/bin/sh", scriptPath); err != nil {
		return v3TmuxResizeSmokeResult{}, err
	}
	appendTimeline("tmux session=%s initial=100x30", session)
	terminalIDBytes, err := waitForFileContent(ctx, terminalIDPath, 5*time.Second)
	if err != nil {
		return v3TmuxResizeSmokeResult{}, fmt.Errorf("read terminal id artifact: %w", err)
	}
	terminalID := strings.TrimSpace(string(terminalIDBytes))
	appendTimeline("terminal created id=%s", terminalID)
	if err := waitForTmuxCapture(ctx, target, "termx-pty-ready", 5*time.Second); err != nil {
		return v3TmuxResizeSmokeResult{}, err
	}

	if err := runTmuxCommand(ctx, "send-keys", "-t", target, "size-before", "Enter"); err != nil {
		return v3TmuxResizeSmokeResult{}, err
	}
	beforeMarker := "termx-pty-size:size-before:"
	if err := waitForTmuxCapture(ctx, target, beforeMarker, 5*time.Second); err != nil {
		return v3TmuxResizeSmokeResult{}, err
	}
	beforeCapture, _ := captureTmuxPane(ctx, target, false)
	beforeSize := lastMarkerSuffix(beforeCapture, beforeMarker)
	appendTimeline("before size=%s", beforeSize)

	windowSize := "120x40"
	if err := runTmuxCommand(ctx, "resize-window", "-t", session, "-x", "120", "-y", "40"); err != nil {
		return v3TmuxResizeSmokeResult{}, err
	}
	appendTimeline("tmux resize-window=%s", windowSize)
	time.Sleep(250 * time.Millisecond)
	if err := runTmuxCommand(ctx, "send-keys", "-t", target, "size-after", "Enter"); err != nil {
		return v3TmuxResizeSmokeResult{}, err
	}
	afterMarker := "termx-pty-size:size-after:"
	if err := waitForTmuxCapture(ctx, target, afterMarker, 5*time.Second); err != nil {
		return v3TmuxResizeSmokeResult{}, err
	}
	afterCapture, _ := captureTmuxPane(ctx, target, false)
	afterSize := lastMarkerSuffix(afterCapture, afterMarker)
	appendTimeline("after size=%s", afterSize)

	ansi, err := captureTmuxPane(ctx, target, true)
	if err != nil {
		return v3TmuxResizeSmokeResult{}, err
	}
	plain, err := captureTmuxPane(ctx, target, false)
	if err != nil {
		return v3TmuxResizeSmokeResult{}, err
	}
	ansiPath := filepath.Join(artifactDir, "capture.ansi")
	plainPath := filepath.Join(artifactDir, "capture.txt")
	if err := os.WriteFile(ansiPath, []byte(ansi), 0o600); err != nil {
		return v3TmuxResizeSmokeResult{}, err
	}
	if err := os.WriteFile(plainPath, []byte(plain), 0o600); err != nil {
		return v3TmuxResizeSmokeResult{}, err
	}
	appendTimeline("captured ansi=%s plain=%s", ansiPath, plainPath)

	_ = runTermxCommand(context.Background(), termxBin, "--socket", socketPath, "--log-file", daemonLog, "v3", "kill", terminalID)
	_ = runTermxCommand(context.Background(), termxBin, "--socket", socketPath, "--log-file", daemonLog, "v3", "rm", terminalID)
	appendTimeline("terminal cleanup id=%s", terminalID)
	removeArtifacts = false
	return v3TmuxResizeSmokeResult{
		Session:      session,
		TerminalID:   terminalID,
		ArtifactDir:  artifactDir,
		ANSIPath:     ansiPath,
		PlainPath:    plainPath,
		DaemonLog:    daemonLog,
		SocketPath:   socketPath,
		TimelinePath: timelinePath,
		Captured:     plain,
		BeforeSize:   beforeSize,
		AfterSize:    afterSize,
		WindowSize:   windowSize,
	}, nil
}

func runV3TmuxANSISmoke(ctx context.Context, termxBin string) (v3TmuxANSISmokeResult, error) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return v3TmuxANSISmokeResult{}, fmt.Errorf("tmux ansi smoke requires tmux in PATH: %w", err)
	}
	if termxBin == "" {
		return v3TmuxANSISmokeResult{}, fmt.Errorf("tmux ansi smoke requires a termx binary path")
	}
	artifactDir, err := os.MkdirTemp("", "termx-v3-tmux-ansi-*")
	if err != nil {
		return v3TmuxANSISmokeResult{}, err
	}
	removeArtifacts := true
	defer func() {
		if removeArtifacts {
			_ = os.RemoveAll(artifactDir)
		}
	}()

	socketPath := filepath.Join(artifactDir, "termx-core-v2.sock")
	daemonLog := filepath.Join(artifactDir, "daemon.log")
	timelinePath := filepath.Join(artifactDir, "timeline.txt")
	session := fmt.Sprintf("termx-v3-ansi-%d", time.Now().UnixNano())
	target := session + ":0.0"
	appendTimeline := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...) + "\n"
		_ = appendFile(timelinePath, line)
	}

	daemonCtx, stopDaemonCtx := context.WithCancel(ctx)
	defer stopDaemonCtx()
	daemonCmd := exec.CommandContext(daemonCtx, termxBin, "--socket", socketPath, "--log-file", daemonLog, "daemon")
	daemonCmd.Stdout = io.Discard
	daemonCmd.Stderr = io.Discard
	if err := daemonCmd.Start(); err != nil {
		return v3TmuxANSISmokeResult{}, fmt.Errorf("start core-v2 daemon: %w", err)
	}
	defer func() {
		stopDaemonCtx()
		if daemonCmd.Process != nil {
			_ = daemonCmd.Process.Kill()
		}
		_ = daemonCmd.Wait()
	}()
	appendTimeline("daemon start socket=%s log=%s", socketPath, daemonLog)
	if err := waitForSocket(socketPath, 5*time.Second, func() error {
		client, dialErr := dialV3Client(socketPath)
		if dialErr != nil {
			return dialErr
		}
		return client.Close()
	}); err != nil {
		return v3TmuxANSISmokeResult{}, err
	}
	appendTimeline("daemon ready")

	terminalIDPath := filepath.Join(artifactDir, "terminal.id")
	scriptPath := filepath.Join(artifactDir, "tmux-ansi-smoke.sh")
	terminalScript := "printf 'termx-ansi-ready\\n'\n" +
		"printf '\\033[31mANSI16_RED\\033[0m \\033[1;34mANSI16_BLUE_BOLD\\033[0m\\n'\n" +
		"printf '\\033[38;5;202mANSI256_ORANGE\\033[0m \\033[48;5;24mANSI256_BG\\033[0m\\n'\n" +
		"printf '\\033[38;2;12;200;155mTRUECOLOR_MINT\\033[0m\\n'\n" +
		"printf 'CR_START\\rCR_REPLACED\\n'\n" +
		"printf '\\033[?1049hALT_SCREEN_MARK\\033[?1049lPRIMARY_AFTER_ALT\\n'\n" +
		"while IFS= read -r line; do printf 'termx-ansi-echo:%s\\n' \"$line\"; done\n"
	script := strings.Join([]string{
		"#!/bin/sh",
		"set -eu",
		"export TERM=xterm-256color",
		"export TERMX=0",
		"termx_bin=" + shellQuote(termxBin),
		"socket=" + shellQuote(socketPath),
		"log_file=" + shellQuote(daemonLog),
		"terminal_id_path=" + shellQuote(terminalIDPath),
		"printf 'termx tmux ansi harness ready\\n'",
		"id=\"$($termx_bin --socket \"$socket\" --log-file \"$log_file\" v3 new --name tmux-ansi -- /bin/sh -c " + shellQuote(terminalScript) + ")\"",
		"printf '%s\\n' \"$id\" > \"$terminal_id_path\"",
		"printf 'termx ansi terminal id:%s\\n' \"$id\"",
		"exec $termx_bin --socket \"$socket\" --log-file \"$log_file\" v3 attach \"$id\"",
		"",
	}, "\n")
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return v3TmuxANSISmokeResult{}, err
	}
	appendTimeline("tmux script=%s", scriptPath)

	cleanupTmux := func() {
		_ = runTmuxCommand(context.Background(), "kill-session", "-t", session)
	}
	defer cleanupTmux()
	if err := runTmuxCommand(ctx, "new-session", "-d", "-x", "100", "-y", "30", "-s", session, "/bin/sh", scriptPath); err != nil {
		return v3TmuxANSISmokeResult{}, err
	}
	appendTimeline("tmux session=%s", session)
	terminalIDBytes, err := waitForFileContent(ctx, terminalIDPath, 5*time.Second)
	if err != nil {
		return v3TmuxANSISmokeResult{}, fmt.Errorf("read terminal id artifact: %w", err)
	}
	terminalID := strings.TrimSpace(string(terminalIDBytes))
	appendTimeline("terminal created id=%s", terminalID)
	for _, marker := range []string{"termx-ansi-ready", "ANSI16_RED", "ANSI256_ORANGE", "TRUECOLOR_MINT", "CR_REPLACED", "PRIMARY_AFTER_ALT"} {
		if err := waitForTmuxCapture(ctx, target, marker, 5*time.Second); err != nil {
			return v3TmuxANSISmokeResult{}, err
		}
		appendTimeline("marker rendered=%s", marker)
	}

	ansi, err := captureTmuxPane(ctx, target, true)
	if err != nil {
		return v3TmuxANSISmokeResult{}, err
	}
	plain, err := captureTmuxPane(ctx, target, false)
	if err != nil {
		return v3TmuxANSISmokeResult{}, err
	}
	ansiPath := filepath.Join(artifactDir, "capture.ansi")
	plainPath := filepath.Join(artifactDir, "capture.txt")
	if err := os.WriteFile(ansiPath, []byte(ansi), 0o600); err != nil {
		return v3TmuxANSISmokeResult{}, err
	}
	if err := os.WriteFile(plainPath, []byte(plain), 0o600); err != nil {
		return v3TmuxANSISmokeResult{}, err
	}
	appendTimeline("captured ansi=%s plain=%s", ansiPath, plainPath)

	_ = runTermxCommand(context.Background(), termxBin, "--socket", socketPath, "--log-file", daemonLog, "v3", "kill", terminalID)
	_ = runTermxCommand(context.Background(), termxBin, "--socket", socketPath, "--log-file", daemonLog, "v3", "rm", terminalID)
	appendTimeline("terminal cleanup id=%s", terminalID)
	removeArtifacts = false
	return v3TmuxANSISmokeResult{
		Session:      session,
		TerminalID:   terminalID,
		ArtifactDir:  artifactDir,
		ANSIPath:     ansiPath,
		PlainPath:    plainPath,
		DaemonLog:    daemonLog,
		SocketPath:   socketPath,
		TimelinePath: timelinePath,
		Captured:     plain,
		ANSICaptured: ansi,
	}, nil
}

func runV3TmuxVisualCompare(ctx context.Context, termxBin string) (v3TmuxVisualCompareResult, error) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return v3TmuxVisualCompareResult{}, fmt.Errorf("tmux visual compare requires tmux in PATH: %w", err)
	}
	if termxBin == "" {
		return v3TmuxVisualCompareResult{}, fmt.Errorf("tmux visual compare requires a termx binary path")
	}
	artifactDir, err := os.MkdirTemp("", "termx-v3-tmux-visual-*")
	if err != nil {
		return v3TmuxVisualCompareResult{}, err
	}
	removeArtifacts := true
	defer func() {
		if removeArtifacts {
			_ = os.RemoveAll(artifactDir)
		}
	}()

	session := fmt.Sprintf("termx-v3-visual-%d", time.Now().UnixNano())
	target := session + ":0.0"
	scriptPath := filepath.Join(artifactDir, "tmux-visual-compare.sh")
	script := strings.Join([]string{
		"#!/bin/sh",
		"set -eu",
		"export TERM=xterm-256color",
		"termx_bin=" + shellQuote(termxBin),
		// 用 ANSI repaint 输出单帧，tmux 抓到的是屏幕状态，不是逐行日志。
		"$termx_bin v3 visual-snapshot --ansi",
		"sleep 30",
		"",
	}, "\n")
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return v3TmuxVisualCompareResult{}, err
	}
	cleanupTmux := func() {
		_ = runTmuxCommand(context.Background(), "kill-session", "-t", session)
	}
	defer cleanupTmux()
	if err := runTmuxCommand(ctx, "new-session", "-d", "-x", "120", "-y", "40", "-s", session, "/bin/sh", scriptPath); err != nil {
		return v3TmuxVisualCompareResult{}, err
	}
	if err := waitForTmuxCapture(ctx, target, "quick actio", 5*time.Second); err != nil {
		return v3TmuxVisualCompareResult{}, err
	}
	currentANSI, err := captureTmuxPane(ctx, target, true)
	if err != nil {
		return v3TmuxVisualCompareResult{}, err
	}
	currentPlain, err := captureTmuxPane(ctx, target, false)
	if err != nil {
		return v3TmuxVisualCompareResult{}, err
	}
	currentANSIPath := filepath.Join(artifactDir, "current.ansi")
	currentPlainPath := filepath.Join(artifactDir, "current.txt")
	targetPath := filepath.Join(artifactDir, "target.txt")
	diffPath := filepath.Join(artifactDir, "diff.txt")
	summaryPath := filepath.Join(artifactDir, "summary.txt")
	targetPlain := v3VisualTargetPlain()
	diffText, mismatches := diffVisualPlain(targetPlain, currentPlain, 120, 40)
	summary := strings.Join([]string{
		"termx v3 tmux visual compare",
		"source: termx-tui-v3/docs/unicode-ui-wireframes.md + tuiv2 chrome slot contract",
		fmt.Sprintf("viewport: %dx%d", 120, 40),
		fmt.Sprintf("mismatches: %d", mismatches),
		"current_plain: " + currentPlainPath,
		"current_ansi: " + currentANSIPath,
		"target: " + targetPath,
		"diff: " + diffPath,
		"",
	}, "\n")
	for _, file := range []struct {
		path string
		body string
	}{
		{currentANSIPath, currentANSI},
		{currentPlainPath, currentPlain},
		{targetPath, targetPlain},
		{diffPath, diffText},
		{summaryPath, summary},
	} {
		if err := os.WriteFile(file.path, []byte(file.body), 0o600); err != nil {
			return v3TmuxVisualCompareResult{}, err
		}
	}
	removeArtifacts = false
	return v3TmuxVisualCompareResult{
		Session:          session,
		ArtifactDir:      artifactDir,
		CurrentANSIPath:  currentANSIPath,
		CurrentPlainPath: currentPlainPath,
		TargetPath:       targetPath,
		DiffPath:         diffPath,
		SummaryPath:      summaryPath,
		Mismatches:       mismatches,
	}, nil
}

func v3VisualTargetPlain() string {
	lines := []string{
		"┌ main ─┬─ 1:main × ─┬─ 2:logs × ─┬─ ＋ ─────────────────────────────────────────────────────────────── termx ┐",
		"├───────┴─────────────┴────────────┴──────────────────────────────────────────────────────────────────────────────┤",
		"│ shell ──────────────────────────────────────────────────────────────── ↕  ↔  × │ logs ───────────────────── × │",
		"│ termx git:termx-core-v2-tui-v3-migration  go v1.26.0                            │ visual review baseline       │",
		"│ > make test                                                                      │ target visual mismatch       │",
		"│ ok   termx-tui-v3/render                                                         │ emoji 🚀 and 中文            │",
		"│ >                                                                                │                            │",
		"│                                                                                  │ ┌ quick actions ───────── × ┐ │",
		"│                                                                                  │ │ No terminal attached      │ │",
		"│                                                                                  │ │                          │ │",
		"│                                                                                  │ │ Attach existing           │ │",
		"│                                                                                  │ │ New terminal              │ │",
		"│                                                                                  │ │ Terminal Pool             │ │",
		"│                                                                                  │ │ Close                    │ │",
		"│                                                                                  │ └──────────────────────────┘ │",
		"│                                                                                  │                            │",
		"│                                                                                  │                            │",
		"│                                                                                  │                            │",
		"│                                                                                  │                            │",
		"│                                                                                  │                            │",
		"│                                                                                  │                            │",
		"│                                                                                  │                            │",
		"│                                                                                  │                            │",
		"│                                                                                  │                            │",
		"│                                                                                  │                            │",
		"│                                                                                  │                            │",
		"│                                                                                  │                            │",
		"│                                                                                  │                            │",
		"│                                                                                  │                            │",
		"│                                                                                  │                            │",
		"│                                                                                  │                            │",
		"│                                                                                  │                            │",
		"│                                                                                  │                            │",
		"│                                                                                  │                            │",
		"│                                                                                  │                            │",
		"│                                                                                  │                            │",
		"│                                                                                  │                            │",
		"└──────────────────────────────────────────────────────────────────────────────────┴────────────────────────────┘",
		"┌──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┐",
		"└ [Ctrl+P] pane  [Ctrl+R] resize  [Ctrl+F] picker  [Ctrl+G] global                         ws:main tabs:2 panes:2 ┘",
	}
	return normalizeVisualText(strings.Join(lines, "\n"), 120, 40)
}

func diffVisualPlain(target string, current string, width int, height int) (string, int) {
	targetLines := normalizeVisualLines(target, width, height)
	currentLines := normalizeVisualLines(current, width, height)
	var builder strings.Builder
	builder.WriteString("tmux visual diff: target vs current\n")
	builder.WriteString("source target: Unicode wireframe + tuiv2 slot contract\n\n")
	mismatches := 0
	for i := 0; i < height; i++ {
		if targetLines[i] == currentLines[i] {
			continue
		}
		mismatches++
		fmt.Fprintf(&builder, "@@ row %02d @@\n", i+1)
		fmt.Fprintf(&builder, "- %s\n", targetLines[i])
		fmt.Fprintf(&builder, "+ %s\n", currentLines[i])
	}
	if mismatches == 0 {
		builder.WriteString("no row mismatches\n")
	}
	return builder.String(), mismatches
}

func normalizeVisualText(text string, width int, height int) string {
	return strings.Join(normalizeVisualLines(text, width, height), "\n") + "\n"
}

func normalizeVisualLines(text string, width int, height int) []string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	out := make([]string, height)
	for i := 0; i < height; i++ {
		if i < len(lines) {
			out[i] = fitVisualLine(lines[i], width)
			continue
		}
		out[i] = strings.Repeat(" ", width)
	}
	return out
}

func fitVisualLine(line string, width int) string {
	line = strings.TrimRight(line, "\r")
	displayWidth := render.DisplayWidth(line)
	if displayWidth > width {
		return render.TruncateCells(line, width)
	}
	if displayWidth < width {
		return line + strings.Repeat(" ", width-displayWidth)
	}
	return line
}

func runV3TmuxStabilitySmoke(ctx context.Context, termxBin string, rounds int) (v3TmuxStabilitySmokeResult, error) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return v3TmuxStabilitySmokeResult{}, fmt.Errorf("tmux stability smoke requires tmux in PATH: %w", err)
	}
	if termxBin == "" {
		return v3TmuxStabilitySmokeResult{}, fmt.Errorf("tmux stability smoke requires a termx binary path")
	}
	if rounds <= 0 {
		rounds = 1
	}
	artifactDir, err := os.MkdirTemp("", "termx-v3-tmux-stability-*")
	if err != nil {
		return v3TmuxStabilitySmokeResult{}, err
	}
	removeArtifacts := true
	defer func() {
		if removeArtifacts {
			_ = os.RemoveAll(artifactDir)
		}
	}()
	timelinePath := filepath.Join(artifactDir, "timeline.txt")
	appendTimeline := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...) + "\n"
		_ = appendFile(timelinePath, line)
	}
	var artifacts []string
	for round := 1; round <= rounds; round++ {
		appendTimeline("round %d terminal smoke start", round)
		terminal, err := runV3TmuxTerminalSmoke(ctx, termxBin)
		if err != nil {
			return v3TmuxStabilitySmokeResult{}, fmt.Errorf("round %d terminal smoke: %w", round, err)
		}
		artifacts = append(artifacts, terminal.ArtifactDir)
		appendTimeline("round %d terminal smoke ok artifact=%s", round, terminal.ArtifactDir)

		appendTimeline("round %d resize smoke start", round)
		resize, err := runV3TmuxResizeSmoke(ctx, termxBin)
		if err != nil {
			return v3TmuxStabilitySmokeResult{}, fmt.Errorf("round %d resize smoke: %w", round, err)
		}
		artifacts = append(artifacts, resize.ArtifactDir)
		appendTimeline("round %d resize smoke ok artifact=%s before=%s after=%s", round, resize.ArtifactDir, resize.BeforeSize, resize.AfterSize)

		appendTimeline("round %d ansi smoke start", round)
		ansi, err := runV3TmuxANSISmoke(ctx, termxBin)
		if err != nil {
			return v3TmuxStabilitySmokeResult{}, fmt.Errorf("round %d ansi smoke: %w", round, err)
		}
		artifacts = append(artifacts, ansi.ArtifactDir)
		appendTimeline("round %d ansi smoke ok artifact=%s", round, ansi.ArtifactDir)
	}
	manifest := strings.Join(append([]string{"tmux stability artifacts:"}, artifacts...), "\n") + "\n"
	if err := os.WriteFile(filepath.Join(artifactDir, "artifacts.txt"), []byte(manifest), 0o600); err != nil {
		return v3TmuxStabilitySmokeResult{}, err
	}
	removeArtifacts = false
	return v3TmuxStabilitySmokeResult{
		ArtifactDir:  artifactDir,
		TimelinePath: timelinePath,
		Rounds:       rounds,
		Artifacts:    artifacts,
	}, nil
}

func runTmuxCommand(ctx context.Context, args ...string) error {
	output, err := exec.CommandContext(ctx, "tmux", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func runTermxCommand(ctx context.Context, termxBin string, args ...string) error {
	output, err := exec.CommandContext(ctx, termxBin, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("termx %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func captureTmuxPane(ctx context.Context, target string, ansi bool) (string, error) {
	args := []string{"capture-pane", "-t", target, "-p"}
	if ansi {
		args = []string{"capture-pane", "-t", target, "-e", "-p"}
	}
	output, err := exec.CommandContext(ctx, "tmux", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func waitForTmuxCapture(ctx context.Context, target string, marker string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		captured, err := captureTmuxPane(ctx, target, false)
		if err == nil {
			last = captured
			if strings.Contains(captured, marker) {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return fmt.Errorf("timed out waiting for tmux marker %q; last capture:\n%s", marker, last)
}

func waitForFileContent(ctx context.Context, path string, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && len(strings.TrimSpace(string(data))) > 0 {
			return data, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("timed out waiting for file %s: %w", path, lastErr)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func appendFile(path string, text string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.WriteString(file, text)
	return err
}

func lastMarkerSuffix(text string, marker string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if index := strings.Index(lines[i], marker); index >= 0 {
			fields := strings.Fields(lines[i][index+len(marker):])
			if len(fields) > 0 {
				return fields[0]
			}
			return ""
		}
	}
	return ""
}
