package daemon

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/remoteauth"
)

func TestAgentAnswersOfferWithoutPublishingTerminalInventory(t *testing.T) {
	stream := cloudcompanion.NewFakePresenceStream(3)
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Ready{Ready: &cloudpb.PresenceReady{
		PresenceSessionId: "presence-1",
		IceServers:        []*cloudpb.IceServer{{Urls: []string{"stun:example.test"}}},
	}}})
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Offer{Offer: &cloudpb.SignalingOffer{
		SignalingSessionId: "signal-1", ManagedSessionId: "managed-1", Sdp: "offer-sdp",
	}}})
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Closed{Closed: &cloudpb.PresenceClosed{Reason: "device revoked"}}})

	answerer := &fakeOfferAnswerer{answer: &cloudpb.SignalingAnswer{Sdp: "answer-sdp"}}
	identity := testAgentIdentity(t, "device-1")
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	companion := agentCompanion(t, identity, now, stream)
	companion.CompleteSignalingOfferFunc = func(context.Context, *cloudpb.CompleteSignalingOfferRequest) (*cloudpb.CompleteSignalingOfferResponse, error) {
		return &cloudpb.CompleteSignalingOfferResponse{}, nil
	}
	agent := Agent{Companion: companion, Identity: identity, Metadata: &cloudpb.DeviceMetadata{DisplayName: "Lab"}, Answerer: answerer, Now: func() time.Time { return now }}
	if err := agent.Run(context.Background()); !errors.Is(err, ErrPresenceClosed) {
		t.Fatalf("Run error = %v, want ErrPresenceClosed", err)
	}
	if answerer.calls != 1 || len(answerer.iceServers) != 1 || answerer.iceServers[0].GetUrls()[0] != "stun:example.test" {
		t.Fatalf("offer answerer calls=%d ice=%v", answerer.calls, answerer.iceServers)
	}
	recorded := companion.Requests()
	if len(recorded.BeginPresence) != 1 || len(recorded.OpenPresence) != 1 || len(recorded.CompleteSignalingOffer) != 1 {
		t.Fatalf("recorded requests = %+v", recorded)
	}
	completed := recorded.CompleteSignalingOffer[0]
	if completed.GetSignalingSessionId() != "signal-1" || completed.GetAnswer().GetSdp() != "answer-sdp" {
		t.Fatalf("completed offer = %+v", completed)
	}
}

func TestAgentAcquiresDaemonCredentialForRelayOnlyOffer(t *testing.T) {
	stream := cloudcompanion.NewFakePresenceStream(3)
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Ready{Ready: &cloudpb.PresenceReady{PresenceSessionId: "presence-1"}}})
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Offer{Offer: &cloudpb.SignalingOffer{
		SignalingSessionId: "signal-relay", ManagedSessionId: "managed-relay", TargetDeviceId: "device-relay", Sdp: "offer-sdp",
		RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY, RelayOnly: true,
	}}})
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Closed{Closed: &cloudpb.PresenceClosed{Reason: "done"}}})

	identity := testAgentIdentity(t, "device-relay")
	now := time.Date(2026, 7, 12, 1, 0, 0, 0, time.UTC)
	companion := agentCompanion(t, identity, now, stream)
	companion.AcquireRelayLeaseFunc = func(context.Context, *cloudpb.AcquireRelayLeaseRequest) (*cloudpb.RelayLease, error) {
		return &cloudpb.RelayLease{
			LeaseId: "lease-relay", SignedLease: []byte("signed-lease"), ExpiresAtUnix: uint64(now.Add(time.Minute).Unix()),
			PathKind:   cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY,
			IceServers: []*cloudpb.IceServer{{Urls: []string{"turn:127.0.0.1:3478?transport=udp"}, Username: "daemon-short", Credential: "daemon-secret"}},
		}, nil
	}
	companion.CompleteSignalingOfferFunc = func(context.Context, *cloudpb.CompleteSignalingOfferRequest) (*cloudpb.CompleteSignalingOfferResponse, error) {
		return &cloudpb.CompleteSignalingOfferResponse{}, nil
	}
	answerer := &fakeOfferAnswerer{answer: &cloudpb.SignalingAnswer{Sdp: "answer-sdp"}}
	if err := (Agent{Companion: companion, Identity: identity, Answerer: answerer, Now: func() time.Time { return now }}).Run(context.Background()); !errors.Is(err, ErrPresenceClosed) {
		t.Fatalf("Run error = %v", err)
	}
	requests := companion.Requests().AcquireRelayLease
	if len(requests) != 1 || requests[0].GetManagedSessionId() != "managed-relay" || requests[0].GetTargetDeviceId() != identity.DeviceID {
		t.Fatalf("daemon Relay lease requests = %#v", requests)
	}
	if len(answerer.iceServers) != 1 || answerer.iceServers[0].GetUsername() != "daemon-short" {
		t.Fatalf("daemon answer ICE = %#v", answerer.iceServers)
	}
}

func TestAgentSignsFreshPresenceChallengeInsideDaemon(t *testing.T) {
	stream := cloudcompanion.NewFakePresenceStream(1)
	if err := stream.Fail(io.EOF); err != nil {
		t.Fatal(err)
	}
	identity := testAgentIdentity(t, "device-proof")
	now := time.Date(2026, 7, 11, 12, 30, 0, 0, time.UTC)
	companion := agentCompanion(t, identity, now, stream)
	agent := Agent{Companion: companion, Identity: identity, Metadata: &cloudpb.DeviceMetadata{DisplayName: "Proof daemon"}, Answerer: &fakeOfferAnswerer{}, Now: func() time.Time { return now }}
	if err := agent.Run(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("Run error = %v, want stream EOF", err)
	}
	request := companion.Requests().OpenPresence[0]
	proof := request.GetProof()
	signingBytes, err := cloudcompanion.PresenceProofSigningBytes(&cloudpb.PresenceProofInput{
		PresenceSessionId: request.GetPresenceSessionId(), ChallengeId: proof.GetChallengeId(), Challenge: bytes.Repeat([]byte{0x42}, 32),
		DeviceId: proof.GetDeviceId(), DevicePublicKey: proof.GetDevicePublicKey(), SignedAtUnixNano: proof.GetSignedAtUnixNano(),
	})
	if err != nil || !ed25519.Verify(identity.PublicKey, signingBytes, proof.GetSignature()) {
		t.Fatalf("presence proof signature invalid: %v", err)
	}
}

func agentCompanion(t *testing.T, identity remoteauth.Identity, now time.Time, stream cloudcompanion.PresenceStream) *cloudcompanion.FakeClient {
	t.Helper()
	return &cloudcompanion.FakeClient{
		BeginPresenceFunc: func(_ context.Context, request *cloudpb.BeginPresenceRequest) (*cloudpb.PresenceChallenge, error) {
			if request.GetDeviceId() != identity.DeviceID {
				t.Fatalf("BeginPresence device = %q", request.GetDeviceId())
			}
			return &cloudpb.PresenceChallenge{PresenceSessionId: "presence-1", ChallengeId: "challenge-1", Challenge: bytes.Repeat([]byte{0x42}, 32), ExpiresAtUnix: uint64(now.Add(time.Minute).Unix())}, nil
		},
		OpenPresenceFunc: func(context.Context, *cloudpb.OpenPresenceRequest) (cloudcompanion.PresenceStream, error) {
			return stream, nil
		},
	}
}

func TestAgentReturnsOfferFailureWithoutEndingPresence(t *testing.T) {
	stream := cloudcompanion.NewFakePresenceStream(3)
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Ready{Ready: &cloudpb.PresenceReady{
		PresenceSessionId: "presence-1",
	}}})
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Offer{Offer: &cloudpb.SignalingOffer{
		SignalingSessionId: "signal-1", ManagedSessionId: "managed-1", Sdp: "offer-sdp",
	}}})
	if err := stream.Fail(io.EOF); err != nil {
		t.Fatalf("queue stream EOF: %v", err)
	}
	identity := testAgentIdentity(t, "device-1")
	companion := agentCompanion(t, identity, time.Now().UTC(), stream)
	companion.CompleteSignalingOfferFunc = func(context.Context, *cloudpb.CompleteSignalingOfferRequest) (*cloudpb.CompleteSignalingOfferResponse, error) {
		return &cloudpb.CompleteSignalingOfferResponse{}, nil
	}
	agent := Agent{
		Companion: companion,
		Identity:  identity,
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
	identity := testAgentIdentity(t, "device-1")
	agent := Agent{Identity: identity, Answerer: &fakeOfferAnswerer{}}
	if err := agent.Run(context.Background()); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING) {
		t.Fatalf("Run error = %v, want COMPANION_MISSING", err)
	}
	companion := &cloudcompanion.FakeClient{}
	agent = Agent{Companion: companion, Identity: identity}
	if err := agent.Run(context.Background()); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
		t.Fatalf("Run error = %v, want PROTOCOL", err)
	}
}

func TestAgentAcceptsIndependentManagedSessionsOnOnePresence(t *testing.T) {
	stream := cloudcompanion.NewFakePresenceStream(4)
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Ready{Ready: &cloudpb.PresenceReady{
		PresenceSessionId: "presence-1",
	}}})
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Offer{Offer: &cloudpb.SignalingOffer{
		SignalingSessionId: "signal-1", ManagedSessionId: "managed-1", Sdp: "offer-sdp",
	}}})
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Offer{Offer: &cloudpb.SignalingOffer{
		SignalingSessionId: "signal-2", ManagedSessionId: "managed-2", Sdp: "offer-sdp",
	}}})
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Closed{Closed: &cloudpb.PresenceClosed{Reason: "done"}}})
	answerer := &fakeOfferAnswerer{answer: &cloudpb.SignalingAnswer{Sdp: "answer-sdp"}}
	identity := testAgentIdentity(t, "device-1")
	companion := agentCompanion(t, identity, time.Now().UTC(), stream)
	companion.CompleteSignalingOfferFunc = func(context.Context, *cloudpb.CompleteSignalingOfferRequest) (*cloudpb.CompleteSignalingOfferResponse, error) {
		return &cloudpb.CompleteSignalingOfferResponse{}, nil
	}
	err := (Agent{Companion: companion, Identity: identity, Answerer: answerer}).Run(context.Background())
	if !errors.Is(err, ErrPresenceClosed) {
		t.Fatalf("Run error = %v, want presence close", err)
	}
	if answerer.calls != 2 || len(companion.Requests().CompleteSignalingOffer) != 2 {
		t.Fatalf("managed sessions did not share presence: calls=%d requests=%+v", answerer.calls, companion.Requests())
	}
}

func testAgentIdentity(t *testing.T, deviceID string) remoteauth.Identity {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := remoteauth.NewIdentity(deviceID, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return identity
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
