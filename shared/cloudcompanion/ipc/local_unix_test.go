//go:build !windows

package ipc

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestListenPreservesActiveCompanionEndpoint(t *testing.T) {
	endpoint := privateTestEndpoint(t)
	first, err := Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	if second, err := Listen(endpoint); err == nil {
		_ = second.Close()
		t.Fatal("second listener replaced an active Cloud Companion endpoint")
	}
	probe, err := net.Dial("unix", endpoint)
	if err != nil {
		t.Fatalf("active endpoint was not preserved: %v", err)
	}
	_ = probe.Close()
}

func TestListenReclaimsStaleCompanionEndpoint(t *testing.T) {
	endpoint := privateTestEndpoint(t)
	stale, err := net.Listen("unix", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	stale.(*net.UnixListener).SetUnlinkOnClose(false)
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}

	listener, err := Listen(endpoint)
	if err != nil {
		t.Fatalf("Listen did not reclaim stale endpoint: %v", err)
	}
	defer listener.Close()
}

func TestRemovingListenerPreservesReplacementSocket(t *testing.T) {
	endpoint := privateTestEndpoint(t)
	first, err := Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(endpoint); err != nil {
		t.Fatal(err)
	}
	replacement, err := net.Listen("unix", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(endpoint); err != nil {
		t.Fatalf("closing old listener removed replacement endpoint: %v", err)
	}
}

func privateTestEndpoint(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "muxvia-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(directory, "cloud-companion.sock")
}
