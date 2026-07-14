//go:build darwin || linux

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDaemonServiceLifecycleUsesExactRuntimeRecord(t *testing.T) {
	binary := buildTermxBinaryForTest(t)
	runtimeDir := t.TempDir()
	socketPath := filepath.Join(runtimeDir, "termx.sock")
	logPath := filepath.Join(runtimeDir, "termx.log")
	t.Cleanup(func() {
		stop := exec.Command(binary, "--socket", socketPath, "--log-file", logPath, "daemon", "stop")
		_, _ = stop.CombinedOutput()
	})

	stopped := executeTermxBinary(t, binary, "--socket", socketPath, "--log-file", logPath, "daemon", "status", "--json")
	if stopped.State != "stopped" || stopped.PID != 0 {
		t.Fatalf("initial daemon status = %#v", stopped)
	}
	started := executeTermxBinary(t, binary, "--socket", socketPath, "--log-file", logPath, "daemon", "start", "--json")
	if started.State != "running" || started.PID <= 0 {
		t.Fatalf("started daemon status = %#v", started)
	}
	recordInfo, err := os.Stat(daemonRecordPath(socketPath))
	if err != nil {
		t.Fatal(err)
	}
	if recordInfo.Mode().Perm() != 0o600 {
		t.Fatalf("daemon record mode = %#o", recordInfo.Mode().Perm())
	}

	restart := exec.Command(binary, "--socket", socketPath, "--log-file", logPath, "daemon", "restart")
	if output, err := restart.CombinedOutput(); err != nil {
		t.Fatalf("daemon restart: %v\n%s", err, output)
	}
	restarted := executeTermxBinary(t, binary, "--socket", socketPath, "--log-file", logPath, "daemon", "status", "--json")
	if restarted.State != "running" || restarted.PID <= 0 || restarted.PID == started.PID {
		t.Fatalf("restarted daemon status = %#v, old pid=%d", restarted, started.PID)
	}

	logCommand := exec.Command(binary, "--log-file", logPath, "daemon", "logs", "--lines", "5")
	if output, err := logCommand.CombinedOutput(); err != nil || !bytes.Contains(output, []byte("core-v2 daemon")) {
		t.Fatalf("daemon logs = %v\n%s", err, output)
	}
	stopped = executeTermxBinary(t, binary, "--socket", socketPath, "--log-file", logPath, "daemon", "stop", "--json")
	if stopped.State != "stopped" {
		t.Fatalf("stopped daemon status = %#v", stopped)
	}
	if _, err := os.Stat(daemonRecordPath(socketPath)); !os.IsNotExist(err) {
		t.Fatalf("daemon record remains after stop: %v", err)
	}
}

func TestDaemonRuntimeRecordRejectsUnsafePermissions(t *testing.T) {
	path := daemonRecordPath(filepath.Join(t.TempDir(), "termx.sock"))
	if err := os.WriteFile(path, []byte(`{"schema_version":1}`), 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := readDaemonRuntimeRecord(path); cliExitCode(err) != 5 {
		t.Fatalf("unsafe daemon record error = %v, exit=%d", err, cliExitCode(err))
	}
}

func executeTermxBinary(t *testing.T, binary string, args ...string) daemonStatusView {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Stderr = io.Discard
	output, err := command.Output()
	if err != nil {
		t.Fatalf("termx %s: %v", strings.Join(args, " "), err)
	}
	var view daemonStatusView
	if err := json.Unmarshal(output, &view); err != nil {
		t.Fatalf("decode daemon status %q: %v", output, err)
	}
	return view
}
