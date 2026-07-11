package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	corev2 "github.com/lozzow/termx/core"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/transport"
)

func TestStartV3ManagedDaemonBuildsPresenceWithoutStoppingCore(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	stream := cloudcompanion.NewFakePresenceStream(1)
	if err := stream.Fail(io.EOF); err != nil {
		t.Fatal(err)
	}
	fake := &managedDaemonCloudFake{FakeClient: &cloudcompanion.FakeClient{
		BeginPresenceFunc: func(context.Context, *cloudpb.BeginPresenceRequest) (*cloudpb.PresenceChallenge, error) {
			return &cloudpb.PresenceChallenge{
				PresenceSessionId: "presence-cli", ChallengeId: "challenge-cli",
				Challenge: bytes.Repeat([]byte{0x61}, 32), ExpiresAtUnix: uint64(time.Now().Add(time.Minute).Unix()),
			}, nil
		},
		OpenPresenceFunc: func(context.Context, *cloudpb.OpenPresenceRequest) (cloudcompanion.PresenceStream, error) {
			return stream, nil
		},
	}, closed: make(chan struct{})}
	previousOpen := openV3CloudDaemonCompanion
	openV3CloudDaemonCompanion = func(context.Context) (v3CloudClient, error) { return fake, nil }
	defer func() { openV3CloudDaemonCompanion = previousOpen }()
	core := &managedDaemonCoreFake{}
	if err := startV3ManagedDaemon(context.Background(), core, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fake.closed:
	case <-time.After(time.Second):
		t.Fatal("managed daemon did not close its Companion connection after presence ended")
	}
	requests := fake.Requests()
	if len(requests.BeginPresence) != 1 || len(requests.OpenPresence) != 1 || requests.OpenPresence[0].GetProof().GetDeviceId() == "" {
		t.Fatalf("managed daemon presence requests = %#v", requests)
	}
	if core.calls != 0 {
		t.Fatalf("presence alone reached core scoped transport %d times", core.calls)
	}
}

type managedDaemonCoreFake struct{ calls int }

func (core *managedDaemonCoreFake) ServeScopedTransport(context.Context, transport.Transport, corev2.TransportScope) error {
	core.calls++
	return nil
}

type managedDaemonCloudFake struct {
	*cloudcompanion.FakeClient
	once   sync.Once
	closed chan struct{}
}

func (fake *managedDaemonCloudFake) Close() error {
	fake.once.Do(func() { close(fake.closed) })
	return nil
}
