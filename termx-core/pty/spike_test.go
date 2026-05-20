package pty

import (
	"bytes"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSpawnReadWriteResizeAndKill(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	p, err := Spawn(SpawnOptions{
		Command:    []string{"bash", "--noprofile", "--norc"},
		Size:       Size{Cols: 80, Rows: 24},
		TerminalID: "spike01",
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("spawn failed: %v", err)
	}
	defer p.Close()

	if _, err := p.Write([]byte("echo hello\n")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	var out bytes.Buffer
	deadline := time.Now().Add(5 * time.Second)
	buf := make([]byte, 4096)
	for time.Now().Before(deadline) {
		n, err := p.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
			if strings.Contains(out.String(), "hello") {
				break
			}
		}
		if err != nil && err != io.EOF {
			t.Fatalf("read failed: %v", err)
		}
	}

	if !strings.Contains(out.String(), "hello") {
		t.Fatalf("output %q does not contain hello", out.String())
	}

	if err := p.Resize(100, 40); err != nil {
		t.Fatalf("resize failed: %v", err)
	}

	if err := p.Kill(); err != nil {
		t.Fatalf("kill failed: %v", err)
	}

	select {
	case <-p.Wait():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process exit")
	}
}
