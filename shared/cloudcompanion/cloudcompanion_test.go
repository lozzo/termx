package cloudcompanion

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
)

func TestFakeClientNegotiatesAndRecordsHello(t *testing.T) {
	fake := &FakeClient{
		HelloFunc: func(_ context.Context, request *cloudpb.CompanionHelloRequest) (*cloudpb.CompanionHelloResponse, error) {
			if request.GetProtocolMin() != ProtocolVersionMin || request.GetProtocolMax() != ProtocolVersionMax {
				t.Fatalf("unexpected protocol range: %d..%d", request.GetProtocolMin(), request.GetProtocolMax())
			}
			return &cloudpb.CompanionHelloResponse{SelectedProtocol: ProtocolVersionMax}, nil
		},
	}
	request := &cloudpb.CompanionHelloRequest{
		ProtocolMin: ProtocolVersionMin,
		ProtocolMax: ProtocolVersionMax,
		CallerRole:  cloudpb.CallerRole_CALLER_ROLE_TUI,
	}
	response, err := fake.Hello(context.Background(), request)
	if err != nil {
		t.Fatalf("Hello: %v", err)
	}
	if response.GetSelectedProtocol() != ProtocolVersionMax {
		t.Fatalf("selected protocol = %d, want %d", response.GetSelectedProtocol(), ProtocolVersionMax)
	}

	request.CallerRole = cloudpb.CallerRole_CALLER_ROLE_DAEMON
	recorded := fake.Requests()
	if len(recorded.Hello) != 1 || recorded.Hello[0].GetCallerRole() != cloudpb.CallerRole_CALLER_ROLE_TUI {
		t.Fatalf("recorded hello was not isolated from caller mutation: %+v", recorded.Hello)
	}
}

func TestFakeSignalingStreamEnforcesBackpressureAndClose(t *testing.T) {
	stream := NewFakeSignalingStream(1)
	if err := stream.Push(&cloudpb.SignalingEvent{Payload: &cloudpb.SignalingEvent_Closed{Closed: &cloudpb.SignalingClosed{Reason: "done"}}}); err != nil {
		t.Fatalf("first Push: %v", err)
	}
	if err := stream.Push(&cloudpb.SignalingEvent{}); !IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_BACKPRESSURE) {
		t.Fatalf("second Push error = %v, want BACKPRESSURE", err)
	}
	event, err := stream.Receive()
	if err != nil || event.GetClosed().GetReason() != "done" {
		t.Fatalf("Receive = (%v, %v), want closed event", event, err)
	}

	received := make(chan error, 1)
	go func() {
		_, receiveErr := stream.Receive()
		received <- receiveErr
	}()
	if err := stream.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-received:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("Receive after Close error = %v, want io.EOF", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Receive remained blocked after Close")
	}
}

func TestCloudErrorWireRoundTrip(t *testing.T) {
	original := &Error{
		Code:          cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_LOGIN_REQUIRED,
		Message:       "login required",
		Retryable:     true,
		RetryAfter:    2 * time.Second,
		CorrelationID: "trace-1",
	}
	roundTrip := ErrorFromWire(ErrorToWire(original))
	if roundTrip.Code != original.Code || roundTrip.Message != original.Message || roundTrip.Retryable != original.Retryable || roundTrip.RetryAfter != original.RetryAfter || roundTrip.CorrelationID != original.CorrelationID {
		t.Fatalf("round trip = %+v, want %+v", roundTrip, original)
	}
	if !IsCode(roundTrip, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_LOGIN_REQUIRED) {
		t.Fatalf("CodeOf(%v) = %s", roundTrip, CodeOf(roundTrip))
	}
}

func TestUnclassifiedErrorIsSanitizedAtCompanionBoundary(t *testing.T) {
	wire := ErrorToWire(errors.New("grant secret must not cross boundary"))
	if wire.GetCode() != cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL {
		t.Fatalf("wire code = %s, want PROTOCOL", wire.GetCode())
	}
	if wire.GetMessage() == "grant secret must not cross boundary" {
		t.Fatalf("wire message leaked original error: %q", wire.GetMessage())
	}
}
