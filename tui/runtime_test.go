package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunReturnsResetError(t *testing.T) {
	err := Run(nil, Config{}, nil, nil)
	if err == nil {
		t.Fatal("expected reset error, got nil")
	}
	if !strings.Contains(err.Error(), "已重置") {
		t.Fatalf("expected reset message, got %v", err)
	}
}

func TestWaitForSocketRejectsNilProbe(t *testing.T) {
	err := WaitForSocket("", time.Millisecond, nil)
	if err == nil || err.Error() != "probe is nil" {
		t.Fatalf("expected nil probe error, got %v", err)
	}
}

func TestWaitForSocketReturnsWhenSocketAndProbeReady(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termx.sock")
	if err := osWriteFile(path, []byte("ready")); err != nil {
		t.Fatalf("write socket marker: %v", err)
	}
	probeCalled := false
	err := WaitForSocket(path, time.Second, func() error {
		probeCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("expected socket to become ready, got %v", err)
	}
	if !probeCalled {
		t.Fatal("expected probe to be called")
	}
}

func TestWaitForSocketTimesOutWhenProbeNeverSucceeds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termx.sock")
	if err := osWriteFile(path, []byte("ready")); err != nil {
		t.Fatalf("write socket marker: %v", err)
	}
	err := WaitForSocket(path, 120*time.Millisecond, func() error {
		return errors.New("not ready")
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}
