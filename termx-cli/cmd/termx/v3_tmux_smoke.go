package main

import (
	"context"
	"fmt"
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

func runTmuxCommand(ctx context.Context, args ...string) error {
	output, err := exec.CommandContext(ctx, "tmux", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
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
