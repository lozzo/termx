package daemon

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/remoteauth"
)

func TestAgentAnswersOfferWithoutPublishingTerminalInventory(t *testing.T) {
	stream := cloudcompanion.NewFakePresenceStream(3)
	ready := managedReady("presence-1")
	ready.IceServers = []*cloudpb.IceServer{{Urls: []string{"stun:example.test"}}}
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Ready{Ready: ready}})
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Offer{Offer: managedOffer("device-1", "presence-1", "signal-1", "managed-1", 1)}})
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Closed{Closed: &cloudpb.PresenceClosed{Reason: "device revoked"}}})

	answerer := &fakeOfferAnswerer{answer: &cloudpb.SignalingAnswer{Sdp: "answer-sdp"}}
	identity := testAgentIdentity(t, "device-1")
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	companion := agentCompanion(t, identity, now, stream)
	companion.CompleteSignalingOfferFunc = func(context.Context, *cloudpb.CompleteSignalingOfferRequest) (*cloudpb.CompleteSignalingOfferResponse, error) {
		return &cloudpb.CompleteSignalingOfferResponse{}, nil
	}
	agent := Agent{Companion: companion, Identity: identity, Metadata: &cloudpb.DeviceMetadata{DisplayName: "Lab"}, Answerer: answerer, Runtime: newTestManagedRuntime(t, identity.DeviceID), Now: func() time.Time { return now }}
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

func TestAgentExecutesSignedCloseAndReportsIndependentReceipt(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := cloudpb.NewDaemonControlVerifier(map[string]ed25519.PublicKey{"control-1": publicKey})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	identity := testAgentIdentity(t, "device-1")
	runtime := newTestManagedRuntime(t, identity.DeviceID)
	if err := runtime.BindPresence("hub-1", 7, "presence-1", now); err != nil {
		t.Fatal(err)
	}
	target := &cloudpb.ManagedPeerSessionTarget{DaemonDeviceId: identity.DeviceID, ManagedSessionId: "managed-1", SessionIncarnation: 2, AssignmentEpoch: 7, ControlPresenceSessionId: "presence-1", DaemonRuntimeGeneration: runtime.RuntimeGeneration()}
	owner := &agentCloseOwner{done: make(chan struct{})}
	handle, _, err := runtime.Registry().Begin(&cloudpb.ManagedPeerSessionProjection{Target: target, ClientDeviceId: "client-1", EstablishedPresenceSessionId: "presence-1", ControlOwnerHubId: "hub-1", ObservedDataPath: cloudpb.ObservedPath_OBSERVED_PATH_DIRECT, State: cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_AUTHENTICATED}, owner, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.MarkReady(now); err != nil {
		t.Fatal(err)
	}
	owner.request = func() {
		_, _ = handle.MarkClosed("remote_command", now)
		close(owner.done)
	}
	unsigned := &cloudpb.DaemonControlCommand{CommandId: "command-1", CommandKind: cloudpb.DaemonControlCommandKind_DAEMON_CONTROL_COMMAND_KIND_CLOSE_MANAGED_PEER_SESSION, AccountId: "account-1", TargetDeviceId: identity.DeviceID, HubId: "hub-1", AssignmentEpoch: 7, AuthEpoch: 3, PresenceSessionId: "presence-1", DaemonRuntimeGeneration: runtime.RuntimeGeneration(), IssuedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Minute).UnixMilli(), Target: &cloudpb.DaemonControlCommand_ManagedPeerSession{ManagedPeerSession: target}}
	signed, err := cloudpb.SignDaemonControlCommand(unsigned, "control-1", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	stream := cloudcompanion.NewFakePresenceStream(3)
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Ready{Ready: managedReady("presence-1")}})
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_DaemonCommand{DaemonCommand: signed}})
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Closed{Closed: &cloudpb.PresenceClosed{Reason: "test complete"}}})
	companion := agentCompanion(t, identity, now, stream)
	companion.ReportDaemonCommandResultFunc = func(_ context.Context, request *cloudpb.ReportDaemonCommandResultRequest) (*cloudpb.ReportDaemonCommandResultResponse, error) {
		return &cloudpb.ReportDaemonCommandResultResponse{AcceptedCommandId: request.GetResult().GetCommandId()}, nil
	}
	agent := Agent{Companion: companion, Identity: identity, Answerer: &fakeOfferAnswerer{}, Runtime: runtime, CommandVerifier: verifier, Now: func() time.Time { return now }}
	if err := agent.Run(context.Background()); !errors.Is(err, ErrPresenceClosed) {
		t.Fatalf("Run() error = %v", err)
	}
	recorded := companion.Requests().ReportDaemonCommandResult
	if len(recorded) != 1 {
		t.Fatalf("reported daemon command results = %d, want 1", len(recorded))
	}
	result := recorded[0].GetResult()
	if result.GetResultCode() != cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED || result.GetClosedRegistryRevision() == 0 || !owner.requested {
		t.Fatalf("daemon result = %+v, owner requested = %v", result, owner.requested)
	}
}

func TestAgentAcquiresDaemonCredentialForRelayOnlyOffer(t *testing.T) {
	stream := cloudcompanion.NewFakePresenceStream(3)
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Ready{Ready: managedReady("presence-1")}})
	relayOffer := managedOffer("device-relay", "presence-1", "signal-relay", "managed-relay", 1)
	relayOffer.RoutePreference, relayOffer.RelayOnly = cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY, true
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Offer{Offer: relayOffer}})
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
	if err := (Agent{Companion: companion, Identity: identity, Answerer: answerer, Runtime: newTestManagedRuntime(t, identity.DeviceID), Now: func() time.Time { return now }}).Run(context.Background()); !errors.Is(err, ErrPresenceClosed) {
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
	agent := Agent{Companion: companion, Identity: identity, Metadata: &cloudpb.DeviceMetadata{DisplayName: "Proof daemon"}, Answerer: &fakeOfferAnswerer{}, Runtime: newTestManagedRuntime(t, identity.DeviceID), Now: func() time.Time { return now }}
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

func TestAgentRunContinuouslyRenewsFreshPresenceAfterStreamEOF(t *testing.T) {
	first := cloudcompanion.NewFakePresenceStream(1)
	if err := first.Fail(io.EOF); err != nil {
		t.Fatal(err)
	}
	second := cloudcompanion.NewFakePresenceStream(1)
	if err := second.Fail(cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "stop renewal harness")); err != nil {
		t.Fatal(err)
	}

	identity := testAgentIdentity(t, "device-renew")
	now := time.Date(2026, 7, 12, 7, 0, 0, 0, time.UTC)
	presenceAttempt := 0
	companion := &cloudcompanion.FakeClient{
		BeginPresenceFunc: func(_ context.Context, request *cloudpb.BeginPresenceRequest) (*cloudpb.PresenceChallenge, error) {
			if request.GetDeviceId() != identity.DeviceID {
				t.Fatalf("BeginPresence device = %q", request.GetDeviceId())
			}
			presenceAttempt++
			return &cloudpb.PresenceChallenge{
				PresenceSessionId: fmt.Sprintf("presence-%d", presenceAttempt),
				ChallengeId:       fmt.Sprintf("challenge-%d", presenceAttempt),
				Challenge:         bytes.Repeat([]byte{byte(presenceAttempt)}, 32),
				ExpiresAtUnix:     uint64(now.Add(time.Minute).Unix()),
			}, nil
		},
		OpenPresenceFunc: func(context.Context, *cloudpb.OpenPresenceRequest) (cloudcompanion.PresenceStream, error) {
			if presenceAttempt == 1 {
				return first, nil
			}
			return second, nil
		},
	}
	agent := Agent{Companion: companion, Identity: identity, Answerer: &fakeOfferAnswerer{}, Runtime: newTestManagedRuntime(t, identity.DeviceID), Now: func() time.Time { return now }}
	err := agent.RunContinuously(context.Background(), time.Millisecond)
	if !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
		t.Fatalf("RunContinuously error = %v, want terminal PROTOCOL error", err)
	}
	recorded := companion.Requests()
	if len(recorded.BeginPresence) != 2 || len(recorded.OpenPresence) != 2 {
		t.Fatalf("renewed presence requests = %#v", recorded)
	}
	if recorded.OpenPresence[0].GetPresenceSessionId() == recorded.OpenPresence[1].GetPresenceSessionId() ||
		recorded.OpenPresence[0].GetProof().GetChallengeId() == recorded.OpenPresence[1].GetProof().GetChallengeId() {
		t.Fatalf("presence renewal reused challenge: %#v", recorded.OpenPresence)
	}
}

func TestAgentRunContinuouslyDoesNotRenewExplicitPresenceClose(t *testing.T) {
	stream := cloudcompanion.NewFakePresenceStream(1)
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Closed{Closed: &cloudpb.PresenceClosed{Reason: "device revoked"}}})
	identity := testAgentIdentity(t, "device-revoked")
	companion := agentCompanion(t, identity, time.Now().UTC(), stream)
	err := (Agent{Companion: companion, Identity: identity, Answerer: &fakeOfferAnswerer{}, Runtime: newTestManagedRuntime(t, identity.DeviceID)}).RunContinuously(context.Background(), time.Millisecond)
	if !errors.Is(err, ErrPresenceClosed) {
		t.Fatalf("RunContinuously error = %v, want explicit close", err)
	}
	if requests := companion.Requests(); len(requests.BeginPresence) != 1 || len(requests.OpenPresence) != 1 {
		t.Fatalf("explicit close renewed presence = %#v", requests)
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
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Ready{Ready: managedReady("presence-1")}})
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Offer{Offer: managedOffer("device-1", "presence-1", "signal-1", "managed-1", 1)}})
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
		Runtime:   newTestManagedRuntime(t, identity.DeviceID),
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
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Ready{Ready: managedReady("presence-1")}})
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Offer{Offer: managedOffer("device-1", "presence-1", "signal-1", "managed-1", 1)}})
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Offer{Offer: managedOffer("device-1", "presence-1", "signal-2", "managed-2", 2)}})
	mustPushPresence(t, stream, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Closed{Closed: &cloudpb.PresenceClosed{Reason: "done"}}})
	answerer := &fakeOfferAnswerer{answer: &cloudpb.SignalingAnswer{Sdp: "answer-sdp"}}
	identity := testAgentIdentity(t, "device-1")
	companion := agentCompanion(t, identity, time.Now().UTC(), stream)
	companion.CompleteSignalingOfferFunc = func(context.Context, *cloudpb.CompleteSignalingOfferRequest) (*cloudpb.CompleteSignalingOfferResponse, error) {
		return &cloudpb.CompleteSignalingOfferResponse{}, nil
	}
	err := (Agent{Companion: companion, Identity: identity, Answerer: answerer, Runtime: newTestManagedRuntime(t, identity.DeviceID)}).Run(context.Background())
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

func managedReady(presenceSessionID string) *cloudpb.PresenceReady {
	return &cloudpb.PresenceReady{PresenceSessionId: presenceSessionID, HubId: "hub-1", AssignmentEpoch: 7}
}

func managedOffer(deviceID, presenceSessionID, signalingSessionID, managedSessionID string, incarnation uint64) *cloudpb.SignalingOffer {
	return &cloudpb.SignalingOffer{SignalingSessionId: signalingSessionID, ManagedSessionId: managedSessionID, TargetDeviceId: deviceID, Sdp: "offer-sdp", SessionIncarnation: incarnation, PresenceSessionId: presenceSessionID, AssignmentEpoch: 7}
}

func newTestManagedRuntime(t *testing.T, deviceID string) *ManagedRuntime {
	t.Helper()
	runtime, err := NewManagedRuntime(deviceID, bytes.NewReader(bytes.Repeat([]byte{0x33}, 18)))
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

type agentCloseOwner struct {
	done      chan struct{}
	request   func()
	requested bool
}

func (owner *agentCloseOwner) RequestClose() {
	owner.requested = true
	if owner.request != nil {
		owner.request()
	}
}

func (owner *agentCloseOwner) Done() <-chan struct{} { return owner.done }

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
