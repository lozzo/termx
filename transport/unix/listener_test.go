package unix

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/transport"
)

func TestListenerDialRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termx.sock")
	listener, err := NewListener(path)
	if err != nil {
		t.Fatalf("new listener failed: %v", err)
	}
	defer listener.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	accepted := make(chan transport.Transport, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			return
		}
		accepted <- conn
	}()

	client, err := Dial(path)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer client.Close()

	var server transport.Transport
	select {
	case server = <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for accept")
	}
	defer server.Close()

	if err := client.Send([]byte("hello")); err != nil {
		t.Fatalf("client send failed: %v", err)
	}
	got, err := server.Recv()
	if err != nil {
		t.Fatalf("server recv failed: %v", err)
	}
	if !bytes.Equal(got, []byte("hello")) {
		t.Fatalf("unexpected server payload: %q", string(got))
	}

	if err := server.Send([]byte("world")); err != nil {
		t.Fatalf("server send failed: %v", err)
	}
	got, err = client.Recv()
	if err != nil {
		t.Fatalf("client recv failed: %v", err)
	}
	if !bytes.Equal(got, []byte("world")) {
		t.Fatalf("unexpected client payload: %q", string(got))
	}

	if err := client.Close(); err != nil {
		t.Fatalf("client close failed: %v", err)
	}
	select {
	case <-client.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for client done channel")
	}
}

func TestListenerAcceptContextCancel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termx.sock")
	listener, err := NewListener(path)
	if err != nil {
		t.Fatalf("new listener failed: %v", err)
	}
	defer listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := listener.Accept(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestListenerSupportsLongSocketPath(t *testing.T) {
	base := filepath.Join(t.TempDir(), strings.Repeat("socket-dir-", 8))
	path := filepath.Join(base, "termx.sock")
	if len(path) <= maxSocketPathBytes() {
		t.Fatalf("expected long socket path, got len=%d limit=%d", len(path), maxSocketPathBytes())
	}

	listener, err := NewListener(path)
	if err != nil {
		t.Fatalf("new listener failed: %v", err)
	}
	defer listener.Close()

	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("expected visible alias at original path: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	accepted := make(chan transport.Transport, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			return
		}
		accepted <- conn
	}()

	client, err := Dial(path)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer client.Close()

	select {
	case server := <-accepted:
		defer server.Close()
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for accept on long socket path")
	}
}

func TestListenerDialRoundTripLargeAndEmptyFrames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termx.sock")
	listener, err := NewListener(path)
	if err != nil {
		t.Fatalf("new listener failed: %v", err)
	}
	defer listener.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	accepted := make(chan transport.Transport, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			return
		}
		accepted <- conn
	}()

	client, err := Dial(path)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer client.Close()

	var server transport.Transport
	select {
	case server = <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for accept")
	}
	defer server.Close()

	large := bytes.Repeat([]byte("termx-compressible-frame-"), 40*1024)
	if err := client.Send(large); err != nil {
		t.Fatalf("client send large failed: %v", err)
	}
	got, err := server.Recv()
	if err != nil {
		t.Fatalf("server recv large failed: %v", err)
	}
	if !bytes.Equal(got, large) {
		t.Fatalf("unexpected large payload mismatch: got=%d want=%d", len(got), len(large))
	}

	if err := server.Send(nil); err != nil {
		t.Fatalf("server send empty failed: %v", err)
	}
	got, err = client.Recv()
	if err != nil {
		t.Fatalf("client recv empty failed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty frame payload, got %d bytes", len(got))
	}
}

func TestListenerDialRoundTripManyFrames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termx.sock")
	listener, err := NewListener(path)
	if err != nil {
		t.Fatalf("new listener failed: %v", err)
	}
	defer listener.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	accepted := make(chan transport.Transport, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			return
		}
		accepted <- conn
	}()

	client, err := Dial(path)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer client.Close()

	var server transport.Transport
	select {
	case server = <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for accept")
	}
	defer server.Close()

	for i := 0; i < 128; i++ {
		payload := []byte(fmt.Sprintf("frame-%03d-%s", i, strings.Repeat("x", i%17)))
		if err := client.Send(payload); err != nil {
			t.Fatalf("client send %d failed: %v", i, err)
		}
		got, err := server.Recv()
		if err != nil {
			t.Fatalf("server recv %d failed: %v", i, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("frame %d mismatch: got %q want %q", i, string(got), string(payload))
		}
	}
}

func TestListenerDialRoundTripFragmentedFrameBoundaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termx.sock")
	listener, err := NewListener(path)
	if err != nil {
		t.Fatalf("new listener failed: %v", err)
	}
	defer listener.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	accepted := make(chan transport.Transport, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			return
		}
		accepted <- conn
	}()

	client, err := Dial(path)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer client.Close()

	var server transport.Transport
	select {
	case server = <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for accept")
	}
	defer server.Close()

	for _, size := range []int{maxPacketPayloadSize + 1, maxPacketPayloadSize*3 + 17} {
		payload := bytes.Repeat([]byte("z"), size)
		if err := client.Send(payload); err != nil {
			t.Fatalf("client send size=%d failed: %v", size, err)
		}
		got, err := server.Recv()
		if err != nil {
			t.Fatalf("server recv size=%d failed: %v", size, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("fragmented payload mismatch for size=%d: got=%d want=%d", size, len(got), len(payload))
		}
	}
}

func TestListenerDialRoundTripConcurrentSmallFrames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termx.sock")
	listener, err := NewListener(path)
	if err != nil {
		t.Fatalf("new listener failed: %v", err)
	}
	defer listener.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	accepted := make(chan transport.Transport, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			return
		}
		accepted <- conn
	}()

	client, err := Dial(path)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer client.Close()

	var server transport.Transport
	select {
	case server = <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for accept")
	}
	defer server.Close()

	const frameCount = 32
	payloads := make([][]byte, frameCount)
	for i := 0; i < frameCount; i++ {
		payloads[i] = []byte(fmt.Sprintf("small-%02d-%s", i, strings.Repeat("y", i%5)))
	}

	var wg sync.WaitGroup
	for _, payload := range payloads {
		payload := payload
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := client.Send(payload); err != nil {
				t.Errorf("client send failed: %v", err)
			}
		}()
	}
	wg.Wait()

	received := make(map[string]int, frameCount)
	for i := 0; i < frameCount; i++ {
		got, err := server.Recv()
		if err != nil {
			t.Fatalf("server recv %d failed: %v", i, err)
		}
		received[string(got)]++
	}
	for _, payload := range payloads {
		if received[string(payload)] != 1 {
			t.Fatalf("expected payload %q exactly once, got count=%d", string(payload), received[string(payload)])
		}
	}
}
