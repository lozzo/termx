package edge

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/companion/cloudservice/httpapi"
	"github.com/muxvia/muxvia/private/cloud/companion/session"
	"github.com/muxvia/muxvia/private/cloud/control-plane/servicecredential"
	cloudhub "github.com/muxvia/muxvia/private/cloud/hub"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
	"google.golang.org/protobuf/proto"
)

func TestHubPublicAdapterRunsPresenceResolveAndSignalingOverHTTP(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	controllerPublic, controllerPrivate, _ := ed25519.GenerateKey(rand.Reader)
	daemonPublic, daemonPrivate, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := servicecredential.NewSigner("controller-key", controllerPrivate, now.Add(-time.Minute), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	ring, err := servicecredential.NewKeyRing(servicecredential.VerificationKey{ID: "controller-key", PublicKey: controllerPublic, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := cloudhub.NewEdgeAuthorizer(cloudhub.EdgeAuthorizerConfig{HubID: "hub-1", Issuer: "muxvia-cloud-controller", KeyRing: ring, MaxStaleness: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	accountPolicy := cloudhub.AccountAuthorization{AccountID: "account-1", AuthEpoch: 1, EntitlementStatus: cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_ACTIVE, EntitlementEffectiveUntilUnix: now.Add(time.Hour).Unix(), Capability: &cloudpb.PlanCapability{ManagedP2PEnabled: true, ManagedP2PMaxConcurrency: 1, CloudDeviceLimit: 2}}
	daemonPolicy := cloudhub.DeviceAuthorization{DeviceID: "daemon-1", AccountID: "account-1", Kind: "daemon", DisplayName: "Daemon", PublicKey: daemonPublic, AuthEpoch: 1}
	if err := authorizer.ApplySnapshot(cloudhub.AuthorizationSnapshot{Revision: 1, GeneratedAt: now, Accounts: []cloudhub.AccountAuthorization{accountPolicy}, Devices: []cloudhub.DeviceAuthorization{daemonPolicy}}); err != nil {
		t.Fatal(err)
	}
	hubService, err := cloudhub.New(cloudhub.Config{HubID: "hub-1", MaxPresenceTTL: time.Minute, MaxSignalingTTL: time.Minute, PresenceChallengeTTL: time.Minute, MaxPresenceChallenges: 8, PresenceQueueSize: 8, ClientQueueSize: 8, MaxSDPBytes: 4096, MaxCandidates: 8, MaxPresences: 8, MaxSessions: 8, MaxSessionsPerClient: 4, EdgeAuthorizer: authorizer, AssignmentSource: staticAssignmentSource{deviceID: "daemon-1", epoch: 1}})
	if err != nil {
		t.Fatal(err)
	}
	projection := &staticProjection{staticAssignmentSource: staticAssignmentSource{deviceID: "daemon-1", epoch: 1}}
	server := httptest.NewServer(newHubHTTPHandler(hubHTTPConfig{Hub: hubService, Authorizer: authorizer, Projection: projection, HubID: "hub-1", HubURL: "http://127.0.0.1:1"}))
	defer server.Close()
	adapter, err := httpapi.New(httpapi.Config{ControlPlaneURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := servicecredential.NewEdgeAccessIssuer("muxvia-cloud-controller", signer)
	if err != nil {
		t.Fatal(err)
	}
	clientToken, err := issuer.IssueEdgeAccessForPrincipal("client-token", "hub-1", "account-1", "client-1", servicecredential.EdgePrincipalClient, 1, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	daemonToken, err := issuer.IssueEdgeAccessForPrincipal("daemon-token", "hub-1", "account-1", "daemon-1", servicecredential.EdgePrincipalDaemon, 1, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	clientSession, err := session.New(session.Metadata{Kind: session.KindAccount, AccountID: "account-1", DeviceID: "client-1", ExpiresAt: now.Add(time.Hour), HubID: "hub-1", HubURL: server.URL, HubRegion: "local-1", HubDirectoryVersion: 1}, clientToken, now)
	if err != nil {
		t.Fatal(err)
	}
	daemonSession, err := session.New(session.Metadata{Kind: session.KindDevice, AccountID: "account-1", DeviceID: "daemon-1", ExpiresAt: now.Add(time.Hour), HubID: "hub-1", HubURL: server.URL, HubRegion: "local-1", HubDirectoryVersion: 1}, daemonToken, now)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := adapter.ListManagedDevices(context.Background(), clientSession.Authorization(), &cloudpb.ListManagedDevicesRequest{SchemaVersion: 1})
	if err != nil || len(directory.GetDevices()) != 1 || directory.GetDevices()[0].GetDeviceId() != "daemon-1" {
		t.Fatalf("new client directory before projection sync = (%#v, %v)", directory, err)
	}
	clientPolicy := cloudhub.DeviceAuthorization{DeviceID: "client-1", AccountID: "account-1", Kind: "client", DisplayName: "Client", AuthEpoch: 1}
	if err := authorizer.ApplySnapshot(cloudhub.AuthorizationSnapshot{Revision: 2, GeneratedAt: now, Accounts: []cloudhub.AccountAuthorization{accountPolicy}, Devices: []cloudhub.DeviceAuthorization{clientPolicy, daemonPolicy}}); err != nil {
		t.Fatal(err)
	}

	challenge, err := adapter.BeginPresence(context.Background(), daemonSession.Authorization(), &cloudpb.BeginPresenceRequest{DeviceId: "daemon-1"})
	if err != nil {
		t.Fatal(err)
	}
	signedAt := now.Add(time.Second)
	signingBytes, err := cloudcompanion.PresenceProofSigningBytes(&cloudpb.PresenceProofInput{PresenceSessionId: challenge.GetPresenceSessionId(), ChallengeId: challenge.GetChallengeId(), Challenge: challenge.GetChallenge(), DeviceId: "daemon-1", DevicePublicKey: daemonPublic, SignedAtUnixNano: signedAt.UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	presenceContext, cancelPresence := context.WithCancel(context.Background())
	defer cancelPresence()
	presence, err := adapter.OpenPresence(presenceContext, daemonSession.Authorization(), &cloudpb.OpenPresenceRequest{PresenceSessionId: challenge.GetPresenceSessionId(), Proof: &cloudpb.DeviceProof{DeviceId: "daemon-1", DevicePublicKey: daemonPublic, ChallengeId: challenge.GetChallengeId(), Signature: ed25519.Sign(daemonPrivate, signingBytes), SignedAtUnixNano: signedAt.UnixNano()}, Metadata: &cloudpb.DeviceMetadata{Platform: "test", MuxviaVersion: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	defer presence.Close()
	ready, err := presence.Receive(context.Background())
	if err != nil || ready.GetReady().GetPresenceSessionId() != challenge.GetPresenceSessionId() || ready.GetReady().GetHubId() != "hub-1" || ready.GetReady().GetAssignmentEpoch() != 1 {
		t.Fatalf("presence ready = (%#v, %v)", ready, err)
	}
	reportID := "runtime-1:0"
	runtimeReport := &cloudpb.ReportDaemonRuntimeRequest{ReportId: reportID, HubId: "hub-1", AssignmentEpoch: 1, PresenceSessionId: challenge.GetPresenceSessionId(), DaemonRuntimeGeneration: "runtime-1", PeerSessions: &cloudpb.PeerSessionInventorySnapshot{ReportId: reportID, DaemonDeviceId: "daemon-1", ControlOwnerHubId: "hub-1", AssignmentEpoch: 1, ControlPresenceSessionId: challenge.GetPresenceSessionId(), DaemonRuntimeGeneration: "runtime-1", ObservedAtUnixMillis: now.UnixMilli()}}
	runtimeAck, err := adapter.ReportDaemonRuntime(context.Background(), daemonSession.Authorization(), runtimeReport)
	if err != nil || runtimeAck.GetReportId() != reportID || runtimeAck.GetAcceptedRegistryRevision() != 0 {
		t.Fatalf("daemon runtime report = (%#v, %v)", runtimeAck, err)
	}
	topology := hubService.TopologySnapshot(1, now)
	if len(topology.GetPresences()) != 1 || topology.GetPresences()[0].GetDaemonRuntimeGeneration() != "runtime-1" {
		t.Fatalf("Hub runtime topology = %#v", topology)
	}
	resolved, err := adapter.ResolveEndpoint(context.Background(), clientSession.Authorization(), &cloudpb.ResolveEndpointRequest{EndpointId: "endpoint-1", TargetDeviceId: "daemon-1"})
	if err != nil || resolved.GetHubId() != "hub-1" || resolved.GetPresence() != cloudpb.PresenceState_PRESENCE_STATE_ONLINE {
		t.Fatalf("resolved endpoint = (%#v, %v)", resolved, err)
	}

	signaling, err := adapter.CreateSignalingSession(context.Background(), clientSession.Authorization(), &cloudpb.CreateSignalingSessionRequest{EndpointId: "endpoint-1", ManagedSessionId: resolved.GetManagedSessionId(), TargetDeviceId: "daemon-1", OfferSdp: "offer", RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY})
	if err != nil {
		t.Fatal(err)
	}
	defer signaling.Close()
	offer, err := presence.Receive(context.Background())
	if err != nil || offer.GetOffer().GetSdp() != "offer" || offer.GetOffer().GetSessionIncarnation() == 0 || offer.GetOffer().GetPresenceSessionId() != challenge.GetPresenceSessionId() || offer.GetOffer().GetAssignmentEpoch() != 1 {
		t.Fatalf("presence offer = (%#v, %v)", offer, err)
	}
	if _, err := adapter.CompleteSignalingOffer(context.Background(), daemonSession.Authorization(), &cloudpb.CompleteSignalingOfferRequest{SignalingSessionId: offer.GetOffer().GetSignalingSessionId(), Result: &cloudpb.CompleteSignalingOfferRequest_Answer{Answer: &cloudpb.SignalingAnswer{SignalingSessionId: offer.GetOffer().GetSignalingSessionId(), Sdp: "answer"}}}); err != nil {
		t.Fatal(err)
	}
	answer, err := signaling.Receive(context.Background())
	if err != nil || answer.GetAnswer().GetSdp() != "answer" {
		t.Fatalf("signaling answer = (%#v, %v)", answer, err)
	}
	target := &cloudpb.ManagedPeerSessionTarget{DaemonDeviceId: "daemon-1", ManagedSessionId: resolved.GetManagedSessionId(), SessionIncarnation: offer.GetOffer().GetSessionIncarnation(), AssignmentEpoch: 1, ControlPresenceSessionId: challenge.GetPresenceSessionId(), DaemonRuntimeGeneration: "runtime-1"}
	runtimeReport.ReportId = "runtime-1:1"
	runtimeReport.RegistryRevision = 1
	runtimeReport.PeerSessions.ReportId = runtimeReport.GetReportId()
	runtimeReport.PeerSessions.RegistryRevision = 1
	runtimeReport.PeerSessions.Sessions = []*cloudpb.ManagedPeerSessionProjection{{Target: target, ClientDeviceId: "client-1", EstablishedPresenceSessionId: challenge.GetPresenceSessionId(), ControlOwnerHubId: "hub-1", ObservedDataPath: cloudpb.ObservedPath_OBSERVED_PATH_DIRECT, State: cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_READY, Freshness: cloudpb.Freshness_FRESHNESS_FRESH}}
	if _, err := adapter.ReportDaemonRuntime(context.Background(), daemonSession.Authorization(), runtimeReport); err != nil {
		t.Fatal(err)
	}
	daemonCommand := &cloudpb.DaemonControlCommand{CommandId: "command-1", CommandKind: cloudpb.DaemonControlCommandKind_DAEMON_CONTROL_COMMAND_KIND_CLOSE_MANAGED_PEER_SESSION, AccountId: "account-1", TargetDeviceId: "daemon-1", HubId: "hub-1", AssignmentEpoch: 1, AuthEpoch: 1, PresenceSessionId: challenge.GetPresenceSessionId(), DaemonRuntimeGeneration: "runtime-1", IssuedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Minute).UnixMilli(), ControlKeyId: "control-1", Target: &cloudpb.DaemonControlCommand_ManagedPeerSession{ManagedPeerSession: target}, Signature: bytes.Repeat([]byte{0x42}, ed25519.SignatureSize)}
	hubResult := hubService.ExecuteHubCommand(&cloudpb.HubCommand{CommandId: "command-1", CommandKind: cloudpb.HubCommandKind_HUB_COMMAND_KIND_FORWARD_DAEMON_COMMAND, IssuedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Minute).UnixMilli(), Target: &cloudpb.HubCommand_DaemonCommand{DaemonCommand: daemonCommand}}, 3, now)
	if hubResult.GetResultCode() != cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED {
		t.Fatalf("Hub command result = %v", hubResult)
	}
	commandEvent, err := presence.Receive(context.Background())
	if err != nil || commandEvent.GetDaemonCommand().GetCommandId() != "command-1" {
		t.Fatalf("daemon command event = (%v, %v)", commandEvent, err)
	}
	daemonResult := &cloudpb.DaemonCommandResult{CommandId: "command-1", DaemonDeviceId: "daemon-1", ManagedSessionId: target.GetManagedSessionId(), SessionIncarnation: target.GetSessionIncarnation(), AssignmentEpoch: 1, PresenceSessionId: challenge.GetPresenceSessionId(), DaemonRuntimeGeneration: "runtime-1", ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED, ClosedRegistryRevision: 2, CompletedAtUnixMillis: now.Add(time.Second).UnixMilli()}
	accepted, err := adapter.ReportDaemonCommandResult(context.Background(), daemonSession.Authorization(), &cloudpb.ReportDaemonCommandResultRequest{Result: daemonResult})
	if err != nil || accepted.GetAcceptedCommandId() != "command-1" {
		t.Fatalf("daemon command result HTTP = (%v, %v)", accepted, err)
	}
	select {
	case runtimeEvent := <-hubService.RuntimeEvents():
		if !proto.Equal(runtimeEvent.GetDaemonCommandResult(), daemonResult) {
			t.Fatalf("runtime daemon result = %v", runtimeEvent)
		}
	case <-time.After(time.Second):
		t.Fatal("daemon command result was not queued for HubControl")
	}
}

func TestHubP2PAdmissionErrorsRemainDistinct(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		code   cloudpb.CloudErrorCode
	}{
		{err: cloudhub.ErrEdgeAuthentication, status: http.StatusUnauthorized, code: cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED},
		{err: cloudhub.ErrPolicySnapshot, status: http.StatusServiceUnavailable, code: cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY},
		{err: cloudhub.ErrPrincipalRevoked, status: http.StatusForbidden, code: cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_AUTHORIZATION_REVOKED},
		{err: cloudhub.ErrP2PNotEntitled, status: http.StatusForbidden, code: cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ENTITLEMENT_DENIED},
		{err: cloudhub.ErrP2PConcurrency, status: http.StatusTooManyRequests, code: cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_QUOTA_EXHAUSTED},
	} {
		response := httptest.NewRecorder()
		mapHubError(response, test.err)
		value := &cloudpb.CloudError{}
		if err := proto.Unmarshal(response.Body.Bytes(), value); err != nil {
			t.Fatal(err)
		}
		if response.Code != test.status || value.GetCode() != test.code {
			t.Fatalf("error %v = status %d contract %v", test.err, response.Code, value)
		}
	}
}

type staticAssignmentSource struct {
	deviceID string
	epoch    uint64
}

type staticProjection struct{ staticAssignmentSource }

func (*staticProjection) Ready() bool { return true }

func (source staticAssignmentSource) ActiveAssignment(deviceID string) (uint64, bool) {
	return source.epoch, source.epoch != 0 && deviceID == source.deviceID
}
