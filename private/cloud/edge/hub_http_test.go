package edge

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	"github.com/lozzow/termx/private/cloud/companion/session"
	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	cloudhub "github.com/lozzow/termx/private/cloud/hub"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
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
	authorizer, err := cloudhub.NewEdgeAuthorizer(cloudhub.EdgeAuthorizerConfig{HubID: "hub-1", Issuer: "termx-cloud-controller", KeyRing: ring, MaxStaleness: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := authorizer.ApplySnapshot(cloudhub.AuthorizationSnapshot{Revision: 1, GeneratedAt: now, Accounts: []cloudhub.AccountAuthorization{{AccountID: "account-1", AuthEpoch: 1, EntitlementStatus: cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_ACTIVE, EntitlementEffectiveUntilUnix: now.Add(time.Hour).Unix(), Capability: &cloudpb.PlanCapability{ManagedP2PEnabled: true}}}, Devices: []cloudhub.DeviceAuthorization{{DeviceID: "client-1", AccountID: "account-1", Kind: "client", DisplayName: "Client"}, {DeviceID: "daemon-1", AccountID: "account-1", Kind: "daemon", DisplayName: "Daemon", PublicKey: daemonPublic}}}); err != nil {
		t.Fatal(err)
	}
	hubService, err := cloudhub.New(cloudhub.Config{HubID: "hub-1", MaxPresenceTTL: time.Minute, MaxSignalingTTL: time.Minute, PresenceChallengeTTL: time.Minute, MaxPresenceChallenges: 8, PresenceQueueSize: 8, ClientQueueSize: 8, MaxSDPBytes: 4096, MaxCandidates: 8, MaxPresences: 8, MaxSessions: 8, MaxSessionsPerClient: 4, EdgeAuthorizer: authorizer, AssignmentSource: staticAssignmentSource{deviceID: "daemon-1", epoch: 1}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(newHubHTTPHandler(hubHTTPConfig{Hub: hubService, Authorizer: authorizer, HubID: "hub-1", HubURL: "http://127.0.0.1:1"}))
	defer server.Close()
	adapter, err := httpapi.New(httpapi.Config{ControlPlaneURL: server.URL, HubURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := servicecredential.NewEdgeAccessIssuer("termx-cloud-controller", signer)
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
	clientSession, err := session.New(session.Metadata{Kind: session.KindAccount, AccountID: "account-1", DeviceID: "client-1", ExpiresAt: now.Add(time.Hour)}, clientToken, now)
	if err != nil {
		t.Fatal(err)
	}
	daemonSession, err := session.New(session.Metadata{Kind: session.KindDevice, AccountID: "account-1", DeviceID: "daemon-1", ExpiresAt: now.Add(time.Hour)}, daemonToken, now)
	if err != nil {
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
	presence, err := adapter.OpenPresence(presenceContext, daemonSession.Authorization(), &cloudpb.OpenPresenceRequest{PresenceSessionId: challenge.GetPresenceSessionId(), Proof: &cloudpb.DeviceProof{DeviceId: "daemon-1", DevicePublicKey: daemonPublic, ChallengeId: challenge.GetChallengeId(), Signature: ed25519.Sign(daemonPrivate, signingBytes), SignedAtUnixNano: signedAt.UnixNano()}, Metadata: &cloudpb.DeviceMetadata{Platform: "test", TermxVersion: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	defer presence.Close()
	ready, err := presence.Receive(context.Background())
	if err != nil || ready.GetReady().GetPresenceSessionId() != challenge.GetPresenceSessionId() {
		t.Fatalf("presence ready = (%#v, %v)", ready, err)
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
	if err != nil || offer.GetOffer().GetSdp() != "offer" {
		t.Fatalf("presence offer = (%#v, %v)", offer, err)
	}
	if _, err := adapter.CompleteSignalingOffer(context.Background(), daemonSession.Authorization(), &cloudpb.CompleteSignalingOfferRequest{SignalingSessionId: offer.GetOffer().GetSignalingSessionId(), Result: &cloudpb.CompleteSignalingOfferRequest_Answer{Answer: &cloudpb.SignalingAnswer{SignalingSessionId: offer.GetOffer().GetSignalingSessionId(), Sdp: "answer"}}}); err != nil {
		t.Fatal(err)
	}
	answer, err := signaling.Receive(context.Background())
	if err != nil || answer.GetAnswer().GetSdp() != "answer" {
		t.Fatalf("signaling answer = (%#v, %v)", answer, err)
	}
}

type staticAssignmentSource struct {
	deviceID string
	epoch    uint64
}

func (source staticAssignmentSource) ActiveAssignment(deviceID string) (uint64, bool) {
	return source.epoch, source.epoch != 0 && deviceID == source.deviceID
}
