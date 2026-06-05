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
