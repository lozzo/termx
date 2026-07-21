package ipc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
)

func TestIPCConnectionPreservesHelloUnaryAndStreamOwnership(t *testing.T) {
	presence := cloudcompanion.NewFakePresenceStream(1)
	fake := &cloudcompanion.FakeClient{
		HelloFunc: func(_ context.Context, request *cloudpb.CompanionHelloRequest) (*cloudpb.CompanionHelloResponse, error) {
			return &cloudpb.CompanionHelloResponse{SelectedProtocol: cloudcompanion.ProtocolVersionMax, CompanionVersion: "1.2.3", BuildChannel: "test", ResponseNonce: append([]byte(nil), request.GetRequestNonce()...)}, nil
		},
		StatusFunc: func(context.Context, *cloudpb.StatusRequest) (*cloudpb.StatusResponse, error) {
			return &cloudpb.StatusResponse{State: cloudpb.CompanionState_COMPANION_STATE_READY, AccountId: "account-1"}, nil
		},
		PlanManagedRouteFunc: func(context.Context, *cloudpb.PlanManagedRouteRequest) (*cloudpb.ManagedRoutePlan, error) {
			return &cloudpb.ManagedRoutePlan{PlanId: "plan-1", ManagedSessionId: "managed-1", TargetDeviceId: "daemon-1"}, nil
		},
		ReportDaemonRuntimeFunc: func(_ context.Context, request *cloudpb.ReportDaemonRuntimeRequest) (*cloudpb.ReportDaemonRuntimeResponse, error) {
			return &cloudpb.ReportDaemonRuntimeResponse{ReportId: request.GetReportId(), DaemonRuntimeGeneration: request.GetDaemonRuntimeGeneration(), AcceptedRegistryRevision: request.GetRegistryRevision()}, nil
		},
		OpenPresenceFunc: func(context.Context, *cloudpb.OpenPresenceRequest) (cloudcompanion.PresenceStream, error) {
			return presence, nil
		},
	}
	client, cleanup := newPipeHarness(t, fake)
	defer cleanup()

	hello, err := client.Hello(context.Background(), testHello())
	if err != nil || hello.GetCompanionVersion() != "1.2.3" {
		t.Fatalf("Hello = (%v, %v)", hello, err)
	}
	status, err := client.Status(context.Background(), &cloudpb.StatusRequest{})
	if err != nil || status.GetAccountId() != "account-1" {
		t.Fatalf("Status = (%v, %v)", status, err)
	}
	plan, err := client.PlanManagedRoute(context.Background(), &cloudpb.PlanManagedRouteRequest{ManagedSessionId: "managed-1"})
	if err != nil || plan.GetPlanId() != "plan-1" {
		t.Fatalf("PlanManagedRoute = (%v, %v)", plan, err)
	}
	runtimeAck, err := client.ReportDaemonRuntime(context.Background(), &cloudpb.ReportDaemonRuntimeRequest{ReportId: "runtime-1:0", DaemonRuntimeGeneration: "runtime-1"})
	if err != nil || runtimeAck.GetReportId() != "runtime-1:0" {
		t.Fatalf("ReportDaemonRuntime = (%v, %v)", runtimeAck, err)
	}

	stream, err := client.OpenPresence(context.Background(), &cloudpb.OpenPresenceRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if err := presence.Push(&cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Ready{Ready: &cloudpb.PresenceReady{PresenceSessionId: "presence-1"}}}); err != nil {
		t.Fatal(err)
	}
	event, err := stream.Receive()
	if err != nil || event.GetReady().GetPresenceSessionId() != "presence-1" {
		t.Fatalf("presence Receive = (%v, %v)", event, err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestIPCRejectsOperationBeforeHello(t *testing.T) {
	fake := &cloudcompanion.FakeClient{}
	client, cleanup := newPipeHarness(t, fake)
	defer cleanup()

	_, err := client.Status(context.Background(), &cloudpb.StatusRequest{})
	if !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
		t.Fatalf("Status before Hello error = %v", err)
	}
}

func TestIPCCancelPropagatesOnlyToOwningRequest(t *testing.T) {
	requestCanceled := make(chan struct{})
	fake := &cloudcompanion.FakeClient{
		HelloFunc: func(context.Context, *cloudpb.CompanionHelloRequest) (*cloudpb.CompanionHelloResponse, error) {
			return &cloudpb.CompanionHelloResponse{SelectedProtocol: cloudcompanion.ProtocolVersionMax, CompanionVersion: "test", BuildChannel: "test", ResponseNonce: bytes.Repeat([]byte{2}, 32)}, nil
		},
		StatusFunc: func(ctx context.Context, _ *cloudpb.StatusRequest) (*cloudpb.StatusResponse, error) {
			<-ctx.Done()
			close(requestCanceled)
			return nil, ctx.Err()
		},
	}
	client, cleanup := newPipeHarness(t, fake)
	defer cleanup()
	if _, err := client.Hello(context.Background(), testHello()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Status(ctx, &cloudpb.StatusRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Status error = %v", err)
	}
	select {
	case <-requestCanceled:
	case <-time.After(time.Second):
		t.Fatal("server request context was not canceled")
	}
	status, err := client.Hello(context.Background(), testHello())
	if status != nil || !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
		t.Fatalf("second Hello = (%v, %v)", status, err)
	}
}

func TestIPCCanceledStreamOpenClosesLateOwnedStream(t *testing.T) {
	openStarted := make(chan struct{})
	requestCanceled := make(chan struct{})
	releaseOpen := make(chan struct{})
	stream := newCloseTrackingPresenceStream()
	fake := &cloudcompanion.FakeClient{
		HelloFunc: func(context.Context, *cloudpb.CompanionHelloRequest) (*cloudpb.CompanionHelloResponse, error) {
			return &cloudpb.CompanionHelloResponse{SelectedProtocol: cloudcompanion.ProtocolVersionMax, CompanionVersion: "test", BuildChannel: "test", ResponseNonce: bytes.Repeat([]byte{2}, 32)}, nil
		},
		OpenPresenceFunc: func(ctx context.Context, _ *cloudpb.OpenPresenceRequest) (cloudcompanion.PresenceStream, error) {
			close(openStarted)
			<-ctx.Done()
			close(requestCanceled)
			<-releaseOpen
			return stream, nil
		},
	}
	client, cleanup := newPipeHarness(t, fake)
	defer cleanup()
	if _, err := client.Hello(context.Background(), testHello()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := client.OpenPresence(ctx, &cloudpb.OpenPresenceRequest{})
		result <- err
	}()
	<-openStarted
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenPresence error = %v", err)
	}
	<-requestCanceled
	close(releaseOpen)
	select {
	case <-stream.closed:
	case <-time.After(time.Second):
		t.Fatal("late stream returned after cancellation was not closed")
	}
}

func TestFrameRejectsOversizeBeforeAllocation(t *testing.T) {
	var header [4]byte
	header[0] = 0xff
	if err := readFrame(bytes.NewReader(header[:]), new(cloudpb.IPCRequest)); err == nil {
		t.Fatal("oversized frame must be rejected")
	}
}

func newPipeHarness(t *testing.T, fake cloudcompanion.FullClient) (*Client, func()) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	server := &Server{NewClient: func() (cloudcompanion.FullClient, error) { return fake, nil }}
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.ServeConn(context.Background(), serverConn) }()
	client, err := NewClient(clientConn)
	if err != nil {
		t.Fatal(err)
	}
	return client, func() {
		_ = client.Close()
		select {
		case err := <-serverDone:
			if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, net.ErrClosed) {
				t.Errorf("ServeConn error = %v", err)
			}
		case <-time.After(time.Second):
			t.Error("ServeConn did not exit")
		}
	}
}

func testHello() *cloudpb.CompanionHelloRequest {
	return &cloudpb.CompanionHelloRequest{
		ProtocolMin:   cloudcompanion.ProtocolVersionMin,
		ProtocolMax:   cloudcompanion.ProtocolVersionMax,
		MuxviaVersion: "test",
		CallerRole:    cloudpb.CallerRole_CALLER_ROLE_CLI,
		RequestNonce:  bytes.Repeat([]byte{1}, 32),
	}
}

type closeTrackingPresenceStream struct {
	closed chan struct{}
	once   chan struct{}
}

func newCloseTrackingPresenceStream() *closeTrackingPresenceStream {
	return &closeTrackingPresenceStream{closed: make(chan struct{}), once: make(chan struct{}, 1)}
}

func (stream *closeTrackingPresenceStream) Receive() (*cloudpb.PresenceEvent, error) {
	<-stream.closed
	return nil, io.EOF
}

func (stream *closeTrackingPresenceStream) Close() error {
	select {
	case stream.once <- struct{}{}:
		close(stream.closed)
	default:
	}
	return nil
}
