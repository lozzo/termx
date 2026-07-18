package pion_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	apilayer "github.com/lozzow/termx/api_layer"
	"github.com/lozzow/termx/client/adapter/managed"
	pionadapter "github.com/lozzow/termx/client/adapter/managed/pion"
	"github.com/lozzow/termx/client/endpoint"
	clientruntime "github.com/lozzow/termx/client/runtime"
	core "github.com/lozzow/termx/core"
	"github.com/lozzow/termx/proto/apipb"
	"github.com/lozzow/termx/proto/cloudpb"
	remotev2daemon "github.com/lozzow/termx/remote/daemon"
	remotev2webrtc "github.com/lozzow/termx/remote/webrtc"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/remoteauth"
)

func TestPionAdapterCompletesAuthHelloAndProtoAPI(t *testing.T) {
	identity, credential, store, now := identityFixture(t)
	server := core.NewServer(core.WithApplicationExecutorFactory(apilayer.CoreApplicationExecutorFactory))
	answerer := remotev2webrtc.Answerer{Handler: remotev2daemon.SessionAcceptor{
		Core: server, Identity: identity, AccessStore: store, Now: func() time.Time { return now },
	}}
	companion := signalingCompanion(answerer)
	attempt := attemptFixture(t, identity)
	dialer := &managed.Dialer{
		Cloud: companion, Peers: pionadapter.Factory{}, ClientName: "pion-engine-e2e",
		Authorization: managed.CapabilityAuthorizer{
			Credentials: staticCredentialSource{credential: credential}, Now: func() time.Time { return now },
		},
		Now: func() time.Time { return now },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ready, err := dialer.Dial(ctx, attempt)
	if err != nil {
		t.Fatal(err)
	}
	defer ready.Close()
	application := ready.(clientruntime.ApplicationReadySession)
	result, err := application.ExecuteApplication(ctx, &apipb.CommandEnvelope{
		Command: &apipb.CommandEnvelope_TerminalList{TerminalList: &apipb.TerminalListCommand{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.GetTerminalList() == nil || ready.Stamp() != attempt.Stamp() || ready.ObservedPath() != string(endpoint.PathDirect) {
		t.Fatalf("ready result=%#v stamp=%#v path=%q", result, ready.Stamp(), ready.ObservedPath())
	}
	recorded := companion.Requests()
	if len(recorded.ResolveEndpoint) != 1 || len(recorded.CreateSignalingSession) != 1 || recorded.CreateSignalingSession[0].GetOfferSdp() == "" {
		t.Fatalf("Companion requests = %+v", recorded)
	}
}

func TestPionAdapterCompletesPairingExchangeAndClosesPairingChannel(t *testing.T) {
	_, daemonPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := remoteauth.NewIdentity("device-pairing", daemonPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	store, err := remoteauth.LoadAccessStore(t.TempDir(), identity, remoteauth.AccessStoreOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	bundle, _, err := store.IssuePairingBundle(remoteauth.PairingIssueOptions{
		Scope: remoteauth.Scope{AllowDaemon: true}, TicketTTL: time.Hour, GrantLifetime: time.Hour, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := remoteauth.EncodePairingBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	clientIdentity, err := remoteauth.GenerateClientAccessIdentity("lab", rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := remoteauth.NewPrivateClientAccessSigner(clientIdentity)
	if err != nil {
		t.Fatal(err)
	}
	server := core.NewServer(core.WithApplicationExecutorFactory(apilayer.CoreApplicationExecutorFactory))
	answerer := remotev2webrtc.Answerer{Handler: remotev2daemon.SessionAcceptor{
		Core: server, Identity: identity, AccessStore: store, Now: func() time.Time { return now },
	}}
	attempt := attemptFixture(t, identity)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	paired, err := (&managed.PairingDialer{
		Cloud: signalingCompanion(answerer), Peers: pionadapter.Factory{}, Now: func() time.Time { return now },
	}).Redeem(ctx, attempt, remoteauth.ClientPairingRequest{
		ExpectedDeviceID: identity.DeviceID, ExpectedDeviceFingerprint: identity.Fingerprint,
		PairingBundle: payload, Identity: clientIdentity, Signer: signer, ClientLabel: "android-pairing-e2e",
	})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := remoteauth.Verify(paired.Grant, identity.Fingerprint, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if paired.TicketID == "" || paired.DeliveryReceipt == "" || claims.SubjectKeyFingerprint != clientIdentity.Fingerprint || !claims.Scope.AllowDaemon {
		t.Fatalf("pairing result=%+v claims=%+v", paired, claims)
	}
}

type staticCredentialSource struct {
	credential remoteauth.ClientAccessCredential
}

func (source staticCredentialSource) ResolveClientCredential(_ context.Context, endpointID, credentialRef string) (remoteauth.ClientAccessCredential, error) {
	if endpointID != "lab" || credentialRef != "credential:lab" {
		return remoteauth.ClientAccessCredential{}, context.Canceled
	}
	return source.credential, nil
}

func attemptFixture(t *testing.T, identity remoteauth.Identity) clientruntime.AttemptRequest {
	t.Helper()
	target := endpoint.Endpoint{
		ID: "lab", DaemonIdentity: endpoint.DaemonIdentity{DeviceID: identity.DeviceID, DeviceFingerprint: identity.Fingerprint},
		Routes: map[endpoint.RouteID]endpoint.AccessRoute{
			"webrtc": {
				ID: "webrtc", Kind: endpoint.RouteManagedWebRTC, Enabled: true,
				Source: endpoint.SourceCloud, PolicySource: endpoint.SourceUser, CredentialRef: "credential:lab",
				TargetDeviceID: identity.DeviceID, AccountProfile: "default", RelayMode: endpoint.RelayDirect,
			},
		},
	}
	attempt, err := clientruntime.NewAttemptRequest(target, "webrtc", 11, clientruntime.ConnectIntentInteractive)
	if err != nil {
		t.Fatal(err)
	}
	return attempt
}

func signalingCompanion(answerer remotev2webrtc.Answerer) *cloudcompanion.FakeClient {
	return &cloudcompanion.FakeClient{
		ResolveEndpointFunc: func(_ context.Context, request *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error) {
			return &cloudpb.ResolvedEndpoint{
				EndpointId: request.GetEndpointId(), TargetDeviceId: request.GetTargetDeviceId(), ManagedSessionId: "managed-1",
			}, nil
		},
		CreateSignalingSessionFunc: func(ctx context.Context, request *cloudpb.CreateSignalingSessionRequest) (cloudcompanion.SignalingStream, error) {
			answer, err := answerer.Answer(ctx, &cloudpb.SignalingOffer{
				SignalingSessionId: "signal-1", ManagedSessionId: request.GetManagedSessionId(), Sdp: request.GetOfferSdp(),
			}, nil)
			if err != nil {
				return nil, err
			}
			stream := cloudcompanion.NewFakeSignalingStream(1)
			if err := stream.Push(&cloudpb.SignalingEvent{Payload: &cloudpb.SignalingEvent_Answer{Answer: answer}}); err != nil {
				return nil, err
			}
			return stream, nil
		},
	}
}

func identityFixture(t *testing.T) (remoteauth.Identity, remoteauth.ClientAccessCredential, *remoteauth.AccessStore, time.Time) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := remoteauth.NewIdentity("device-1", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	clientIdentity, err := remoteauth.GenerateClientAccessIdentity("lab", rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	store, err := remoteauth.LoadAccessStore(t.TempDir(), identity, remoteauth.AccessStoreOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	bundle, _, err := store.IssuePairingBundle(remoteauth.PairingIssueOptions{
		Scope: remoteauth.Scope{AllowDaemon: true}, TicketTTL: time.Hour, GrantLifetime: time.Hour, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := remoteauth.EncodePairingBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	exchanged, err := store.RedeemPairingBundle(payload, clientIdentity.PublicKey, "pion-engine-e2e", now)
	if err != nil {
		t.Fatal(err)
	}
	return identity, remoteauth.ClientAccessCredential{
		Version: 1, EndpointID: "lab", Identity: clientIdentity, CapabilityGrant: exchanged.Grant, UpdatedAt: now,
	}, store, now
}
