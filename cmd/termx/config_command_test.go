package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigCommandsAtomicallyUseRuntimeParser(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	path := filepath.Join(configHome, "termx", "tui-v3.yaml")

	runConfigCommand(t, nil, "config", "set", "tui.theme.mode", "light")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %#o", info.Mode().Perm())
	}

	getOutput := runConfigCommand(t, nil, "config", "get", "tui.theme.mode")
	if strings.TrimSpace(getOutput) != "light" {
		t.Fatalf("config get = %q", getOutput)
	}
	effective := runConfigCommand(t, nil, "config", "show", "--effective")
	if !strings.Contains(effective, `"Mode": "light"`) {
		t.Fatalf("effective config did not use updated runtime value: %s", effective)
	}
	runConfigCommand(t, nil, "config", "validate")

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	invalid := newRootCmd()
	invalid.SetOut(io.Discard)
	invalid.SetErr(io.Discard)
	invalid.SetArgs([]string{"config", "set", "tui.theme.mode", "invalid-mode"})
	if err := invalid.Execute(); cliExitCode(err) != 2 {
		t.Fatalf("invalid config error = %v, exit=%d", err, cliExitCode(err))
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("invalid config mutation changed the source file")
	}

	runConfigCommand(t, nil, "config", "unset", "tui.theme.mode")
	missing := newRootCmd()
	missing.SetOut(io.Discard)
	missing.SetErr(io.Discard)
	missing.SetArgs([]string{"config", "get", "tui.theme.mode"})
	if err := missing.Execute(); cliExitCode(err) != 3 {
		t.Fatalf("unset config get error = %v, exit=%d", err, cliExitCode(err))
	}
}

func TestConfigPathsUseActualTUIConfigPath(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	output := runConfigCommand(t, nil, "config", "paths", "--json")
	if !strings.Contains(output, filepath.Join(configHome, "termx", "tui-v3.yaml")) || strings.Contains(output, "termx.yaml") {
		t.Fatalf("config paths = %s", output)
	}
}

func runConfigCommand(t *testing.T, input io.Reader, args ...string) string {
	t.Helper()
	command := newRootCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(io.Discard)
	if input != nil {
		command.SetIn(input)
	}
	command.SetArgs(args)
	if err := command.Execute(); err != nil {
		t.Fatalf("termx %s: %v", strings.Join(args, " "), err)
	}
	return output.String()
}
