package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTUIStressMemoryBinaryCommandCheck(t *testing.T) {
	script, err := filepath.Abs("anytty_tui_stress_memory.sh")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("script build includes development commands", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "dev-check")
		command := exec.Command("bash", script, "--root", root, "--cleanup-root", "--check-binary")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("development binary check failed: %v\n%s", err, output)
		}
		if !strings.Contains(string(output), "binary command check ok") {
			t.Fatalf("binary check did not execute: %s", output)
		}
	})

	t.Run("ordinary binary fails fast", func(t *testing.T) {
		binary := filepath.Join(t.TempDir(), "anytty-without-dev-commands")
		build := exec.Command("go", "build", "-o", binary, "./cmd/anytty")
		build.Dir = ".."
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build ordinary binary: %v\n%s", err, output)
		}
		root := filepath.Join(t.TempDir(), "ordinary-check")
		command := exec.Command("bash", script, "--bin", binary, "--root", root, "--cleanup-root", "--check-binary")
		output, err := command.CombinedOutput()
		if err == nil {
			t.Fatalf("ordinary binary unexpectedly passed command check: %s", output)
		}
		if !strings.Contains(string(output), "build with -tags anytty_dev_commands") {
			t.Fatalf("ordinary binary failure was not actionable: %s", output)
		}
	})
}
