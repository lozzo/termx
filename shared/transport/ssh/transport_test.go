package ssh

import (
	"bytes"
	"testing"
	"time"
)

func TestBuildCommandUsesRemoteStdioProxyAndAuthAlias(t *testing.T) {
	binaryName, args, err := BuildCommand(DialOptions{
		Address:        "root@114.66.58.243",
		AuthRef:        "ssh:cn-fast",
		RemoteSocket:   "auto",
		RemoteCommand:  "termx-dev",
		SSHBinary:      "ssh-test",
		ConnectTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if binaryName != "ssh-test" {
		t.Fatalf("unexpected binary %q", binaryName)
	}
	want := []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=3", "cn-fast", "termx-dev", "--socket", "auto", "daemon", "stdio-proxy"}
	if len(args) != len(want) {
		t.Fatalf("unexpected args len got=%v want=%v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("unexpected args got=%v want=%v", args, want)
		}
	}
	for _, arg := range args {
		if arg == "sh" || arg == "bash" || arg == "-tt" {
			t.Fatalf("ssh transport must not request shell/tty fallback, args=%v", args)
		}
	}
}

func TestBuildCommandRejectsUnsupportedAuthRef(t *testing.T) {
	if _, _, err := BuildCommand(DialOptions{Address: "root@example.com", AuthRef: "file:/tmp/key"}); err == nil {
		t.Fatal("expected unsupported auth_ref error")
	}
}

func TestFrameRoundTripPreservesBoundaries(t *testing.T) {
	var buf bytes.Buffer
	if err := writeFrame(&buf, []byte("one")); err != nil {
		t.Fatalf("write one: %v", err)
	}
	if err := writeFrame(&buf, []byte("two")); err != nil {
		t.Fatalf("write two: %v", err)
	}
	first, err := readFrame(&buf)
	if err != nil {
		t.Fatalf("read first: %v", err)
	}
	second, err := readFrame(&buf)
	if err != nil {
		t.Fatalf("read second: %v", err)
	}
	if string(first) != "one" || string(second) != "two" {
		t.Fatalf("unexpected frames %q %q", first, second)
	}
}
