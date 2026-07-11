package daemon

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
)

func TestAgentAnswersOfferWithoutPublishingTerminalInventory(t *testing.T) {
	stream := cloudcompanion.NewFakePresenceStream(3)
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Ready{Ready: &cloudpb.PresenceReady{
		ManagedSessionId: "managed-1",
		IceServers:       []*cloudpb.IceServer{{Urls: []string{"stun:example.test"}}},
	}}})
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Offer{Offer: &cloudpb.SignalingOffer{
		SignalingSessionId: "signal-1", ManagedSessionId: "managed-1", Sdp: "offer-sdp",
	}}})
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Closed{Closed: &cloudpb.PresenceClosed{Reason: "device revoked"}}})

	answerer := &fakeOfferAnswerer{answer: &cloudpb.SignalingAnswer{Sdp: "answer-sdp"}}
	companion := &cloudcompanion.FakeClient{
		OpenPresenceFunc: func(context.Context, *cloudpb.OpenPresenceRequest) (cloudcompanion.PresenceStream, error) {
			return stream, nil
		},
		CompleteSignalingOfferFunc: func(context.Context, *cloudpb.CompleteSignalingOfferRequest) (*cloudpb.CompleteSignalingOfferResponse, error) {
			return &cloudpb.CompleteSignalingOfferResponse{}, nil
		},
	}
	agent := Agent{Companion: companion, Presence: &cloudpb.OpenPresenceRequest{}, Answerer: answerer}
	if err := agent.Run(context.Background()); !errors.Is(err, ErrPresenceClosed) {
		t.Fatalf("Run error = %v, want ErrPresenceClosed", err)
	}
	if answerer.calls != 1 || len(answerer.iceServers) != 1 || answerer.iceServers[0].GetUrls()[0] != "stun:example.test" {
		t.Fatalf("offer answerer calls=%d ice=%v", answerer.calls, answerer.iceServers)
	}
	recorded := companion.Requests()
	if len(recorded.OpenPresence) != 1 || len(recorded.CompleteSignalingOffer) != 1 {
		t.Fatalf("recorded requests = %+v", recorded)
	}
	completed := recorded.CompleteSignalingOffer[0]
	if completed.GetSignalingSessionId() != "signal-1" || completed.GetAnswer().GetSdp() != "answer-sdp" {
		t.Fatalf("completed offer = %+v", completed)
	}
}

func TestAgentReturnsOfferFailureWithoutEndingPresence(t *testing.T) {
	stream := cloudcompanion.NewFakePresenceStream(3)
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Ready{Ready: &cloudpb.PresenceReady{
		ManagedSessionId: "managed-1",
	}}})
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Offer{Offer: &cloudpb.SignalingOffer{
		SignalingSessionId: "signal-1", ManagedSessionId: "managed-1", Sdp: "offer-sdp",
	}}})
	if err := stream.Fail(io.EOF); err != nil {
		t.Fatalf("queue stream EOF: %v", err)
	}
	companion := &cloudcompanion.FakeClient{
		OpenPresenceFunc: func(context.Context, *cloudpb.OpenPresenceRequest) (cloudcompanion.PresenceStream, error) {
			return stream, nil
		},
		CompleteSignalingOfferFunc: func(context.Context, *cloudpb.CompleteSignalingOfferRequest) (*cloudpb.CompleteSignalingOfferResponse, error) {
			return &cloudpb.CompleteSignalingOfferResponse{}, nil
		},
	}
	agent := Agent{
		Companion: companion,
		Presence:  &cloudpb.OpenPresenceRequest{},
		Answerer:  &fakeOfferAnswerer{err: errors.New("webrtc negotiation failed")},
	}
	if err := agent.Run(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("Run error = %v, want stream EOF after offer failure", err)
	}
	completed := companion.Requests().CompleteSignalingOffer
	if len(completed) != 1 || completed[0].GetError().GetCode() != cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL {
		t.Fatalf("completed offer error = %+v", completed)
	}
}

func TestAgentRequiresCompanionAndAnswerer(t *testing.T) {
	agent := Agent{Presence: &cloudpb.OpenPresenceRequest{}, Answerer: &fakeOfferAnswerer{}}
	if err := agent.Run(context.Background()); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING) {
		t.Fatalf("Run error = %v, want COMPANION_MISSING", err)
	}
	companion := &cloudcompanion.FakeClient{}
	agent = Agent{Companion: companion, Presence: &cloudpb.OpenPresenceRequest{}}
	if err := agent.Run(context.Background()); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
		t.Fatalf("Run error = %v, want PROTOCOL", err)
	}
}

func TestAgentRejectsOfferFromDifferentManagedSession(t *testing.T) {
	stream := cloudcompanion.NewFakePresenceStream(2)
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Ready{Ready: &cloudpb.PresenceReady{
		ManagedSessionId: "managed-1",
	}}})
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Offer{Offer: &cloudpb.SignalingOffer{
		SignalingSessionId: "signal-1", ManagedSessionId: "managed-2", Sdp: "offer-sdp",
	}}})
	answerer := &fakeOfferAnswerer{}
	companion := &cloudcompanion.FakeClient{
		OpenPresenceFunc: func(context.Context, *cloudpb.OpenPresenceRequest) (cloudcompanion.PresenceStream, error) {
			return stream, nil
		},
	}
	err := (Agent{Companion: companion, Presence: &cloudpb.OpenPresenceRequest{}, Answerer: answerer}).Run(context.Background())
	if !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
		t.Fatalf("Run error = %v, want PROTOCOL", err)
	}
	if answerer.calls != 0 || len(companion.Requests().CompleteSignalingOffer) != 0 {
		t.Fatalf("cross-session offer reached answer path: calls=%d requests=%+v", answerer.calls, companion.Requests())
	}
}

func mustPushPresence(t *testing.T, stream *cloudcompanion.FakePresenceStream, event *cloudpb.PresenceEvent) {
	t.Helper()
	if err := stream.Push(event); err != nil {
		t.Fatalf("Push presence event: %v", err)
	}
}

type fakeOfferAnswerer struct {
	calls      int
	iceServers []*cloudpb.IceServer
	answer     *cloudpb.SignalingAnswer
	err        error
}

func (answerer *fakeOfferAnswerer) Answer(_ context.Context, _ *cloudpb.SignalingOffer, iceServers []*cloudpb.IceServer) (*cloudpb.SignalingAnswer, error) {
	answerer.calls++
	answerer.iceServers = iceServers
	return answerer.answer, answerer.err
}
