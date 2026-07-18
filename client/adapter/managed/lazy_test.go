package managed

import (
	"context"
	"io"
	"sync/atomic"
	"testing"

	clientruntime "github.com/lozzow/termx/client/runtime"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/transport"
)

func TestLazyDialerOwnsCloudLifecycle(t *testing.T) {
	attempt := managedAttempt(t)
	channel := newScriptedProtocolChannel(t)
	peer := &fakeManagedPeer{channel: channel, fingerprint: "sha-256:aa:bb"}
	stream := cloudcompanion.NewFakeSignalingStream(1)
	if err := stream.Push(&cloudpb.SignalingEvent{Payload: &cloudpb.SignalingEvent_Answer{Answer: &cloudpb.SignalingAnswer{Sdp: "answer-sdp"}}}); err != nil {
		t.Fatal(err)
	}
	cloud := &cloudcompanion.FakeClient{
		ResolveEndpointFunc: func(context.Context, *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error) {
			return &cloudpb.ResolvedEndpoint{EndpointId: "studio", TargetDeviceId: "device-1", ManagedSessionId: "managed-1"}, nil
		},
		CreateSignalingSessionFunc: func(context.Context, *cloudpb.CreateSignalingSessionRequest) (cloudcompanion.SignalingStream, error) {
			return stream, nil
		},
	}
	var openCalls atomic.Int32
	closer := &countingCloser{}
	dialer := LazyDialer{
		OpenCloud: func(context.Context) (CloudClient, io.Closer, error) {
			openCalls.Add(1)
			return cloud, closer, nil
		},
		Peers: fakePeerFactory{peer: peer},
		Authorization: &fakeAuthorizer{prepare: func(clientruntime.AttemptRequest) (PreparedAuthorization, error) {
			return &fakePreparedAuthorization{authenticate: func(transport.Transport, string) error { return nil }}, nil
		}},
		ClientName: "lazy-managed-test",
	}

	if openCalls.Load() != 0 {
		t.Fatal("Cloud opener must remain lazy before Dial")
	}
	ready, err := dialer.Dial(context.Background(), attempt)
	if err != nil {
		t.Fatal(err)
	}
	if openCalls.Load() != 1 || closer.calls.Load() != 0 {
		t.Fatalf("open=%d close=%d", openCalls.Load(), closer.calls.Load())
	}
	if err := ready.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ready.Close(); err != nil {
		t.Fatal(err)
	}
	if closer.calls.Load() != 1 {
		t.Fatalf("Cloud owner close calls = %d, want 1", closer.calls.Load())
	}
}

func TestLazyDialerRejectsIncompleteCloudLifecycle(t *testing.T) {
	closer := &countingCloser{}
	dialer := LazyDialer{OpenCloud: func(context.Context) (CloudClient, io.Closer, error) {
		return nil, closer, nil
	}}
	if _, err := dialer.Dial(context.Background(), managedAttempt(t)); err == nil {
		t.Fatal("incomplete Cloud lifecycle must fail")
	}
	if closer.calls.Load() != 1 {
		t.Fatalf("incomplete Cloud owner close calls = %d, want 1", closer.calls.Load())
	}
}

type countingCloser struct {
	calls atomic.Int32
}

func (closer *countingCloser) Close() error {
	closer.calls.Add(1)
	return nil
}
