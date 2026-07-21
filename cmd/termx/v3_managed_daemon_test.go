package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	corev2 "github.com/muxvia/muxvia/core"
	"github.com/muxvia/muxvia/proto/cloudpb"
	remotev2daemon "github.com/muxvia/muxvia/remote/daemon"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
	"github.com/muxvia/muxvia/shared/transport"
)

func TestStartV3ManagedDaemonBuildsPresenceWithoutStoppingCore(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	first := cloudcompanion.NewFakePresenceStream(1)
	if err := first.Fail(io.EOF); err != nil {
		t.Fatal(err)
	}
	second := cloudcompanion.NewFakePresenceStream(1)
	if err := second.Push(&cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Closed{Closed: &cloudpb.PresenceClosed{Reason: "test complete"}}}); err != nil {
		t.Fatal(err)
	}
	presenceAttempt := 0
	fake := &managedDaemonCloudFake{FakeClient: &cloudcompanion.FakeClient{
		BeginPresenceFunc: func(context.Context, *cloudpb.BeginPresenceRequest) (*cloudpb.PresenceChallenge, error) {
			presenceAttempt++
			return &cloudpb.PresenceChallenge{
				PresenceSessionId: fmt.Sprintf("presence-cli-%d", presenceAttempt), ChallengeId: fmt.Sprintf("challenge-cli-%d", presenceAttempt),
				Challenge: bytes.Repeat([]byte{0x61}, 32), ExpiresAtUnix: uint64(time.Now().Add(time.Minute).Unix()),
			}, nil
		},
		OpenPresenceFunc: func(context.Context, *cloudpb.OpenPresenceRequest) (cloudcompanion.PresenceStream, error) {
			if presenceAttempt == 1 {
				return first, nil
			}
			return second, nil
		},
	}, closed: make(chan struct{})}
	previousOpen := openV3CloudDaemonCompanion
	openV3CloudDaemonCompanion = func(context.Context) (v3CloudClient, error) { return fake, nil }
	defer func() { openV3CloudDaemonCompanion = previousOpen }()
	previousDelay := v3ManagedPresenceRetryDelay
	v3ManagedPresenceRetryDelay = time.Millisecond
	defer func() { v3ManagedPresenceRetryDelay = previousDelay }()
	core := &managedDaemonCoreFake{}
	clientAccess, err := loadV3ClientAccessRuntime(resolveV3Socket(""))
	if err != nil {
		t.Fatal(err)
	}
	controlReceipts, err := remotev2daemon.LoadControlReceiptStore(v3RemoteControlDir(), clientAccess.Identity)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := controlReceipts.InstallEnrollment(&cloudpb.DaemonControlEnrollment{AccountId: "account-1", DaemonDeviceId: clientAccess.Identity.DeviceID, AuthEpoch: 1, EnrolledAtUnixMillis: now.UnixMilli(), VerificationKeys: []*cloudpb.DaemonControlVerificationKey{{KeyId: "control-1", PublicKey: bytes.Repeat([]byte{0x41}, 32), NotBeforeUnixMillis: now.Add(-time.Hour).UnixMilli(), NotAfterUnixMillis: now.Add(time.Hour).UnixMilli()}}}); err != nil {
		t.Fatal(err)
	}
	if err := controlReceipts.Close(); err != nil {
		t.Fatal(err)
	}
	if err := startV3ManagedDaemon(context.Background(), core, clientAccess, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fake.closed:
	case <-time.After(time.Second):
		t.Fatal("managed daemon did not close its Companion connection after presence ended")
	}
	requests := fake.Requests()
	if len(requests.BeginPresence) != 2 || len(requests.OpenPresence) != 2 || requests.OpenPresence[0].GetProof().GetDeviceId() == "" {
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
