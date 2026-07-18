//go:build termx_android_spike

package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	apilayer "github.com/lozzow/termx/api_layer"
	"github.com/lozzow/termx/client/adapter/managed"
	pionadapter "github.com/lozzow/termx/client/adapter/managed/pion"
	"github.com/lozzow/termx/client/endpoint"
	clientruntime "github.com/lozzow/termx/client/runtime"
	core "github.com/lozzow/termx/core"
	"github.com/lozzow/termx/proto/bindingpb"
	"github.com/lozzow/termx/proto/cloudpb"
	remotev2daemon "github.com/lozzow/termx/remote/daemon"
	remotev2webrtc "github.com/lozzow/termx/remote/webrtc"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/remoteauth"
)

// androidSpikeHost 只在显式 debug/instrumentation build tag 下提供 PA005N1 纵向 harness。
type androidSpikeHost struct {
	ctx        context.Context
	cancel     context.CancelFunc
	server     *core.Server
	answerer   remotev2webrtc.Answerer
	identity   remoteauth.Identity
	credential remoteauth.ClientAccessCredential
	store      *remoteauth.AccessStore
	owner      *clientruntime.SessionOwner
	now        time.Time
	closeOnce  sync.Once
}

func newAndroidSpikeHost(runtimeDir string) (androidHost, error) {
	if runtimeDir == "" {
		return nil, fmt.Errorf("android runtime directory is required")
	}
	stateDir := filepath.Join(runtimeDir, fmt.Sprintf("termx-go-client-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("create android client state directory: %w", err)
	}
	_, daemonPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	identity, err := remoteauth.NewIdentity("android-spike-daemon", daemonPrivateKey)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	clientIdentity, err := remoteauth.GenerateClientAccessIdentity("android-spike", rand.Reader)
	if err != nil {
		return nil, err
	}
	store, err := remoteauth.LoadAccessStore(stateDir, identity, remoteauth.AccessStoreOptions{Now: func() time.Time { return now }})
	if err != nil {
		return nil, err
	}
	bundle, _, err := store.IssuePairingBundle(remoteauth.PairingIssueOptions{Scope: remoteauth.Scope{AllowDaemon: true}, TicketTTL: time.Hour, GrantLifetime: time.Hour, Now: now})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	payload, err := remoteauth.EncodePairingBundle(bundle)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	exchanged, err := store.RedeemPairingBundle(payload, clientIdentity.PublicKey, "android-go-client", now)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	server := core.NewServer(core.WithApplicationExecutorFactory(apilayer.CoreApplicationExecutorFactory))
	daemonCtx, daemonCancel := context.WithCancel(context.Background())
	host := &androidSpikeHost{ctx: daemonCtx, cancel: daemonCancel, server: server, identity: identity, store: store, now: now,
		owner:      clientruntime.NewSessionOwnerWithAuthority(androidSessionAuthority),
		credential: remoteauth.ClientAccessCredential{Version: 1, EndpointID: "android-spike", Identity: clientIdentity, CapabilityGrant: exchanged.Grant, UpdatedAt: now}}
	host.answerer = remotev2webrtc.Answerer{Handler: remotev2daemon.SessionAcceptor{Core: server, Identity: identity, AccessStore: store, Now: func() time.Time { return now }}}
	return host, nil
}

func (host *androidSpikeHost) OpenSession(ctx context.Context, request *bindingpb.OpenSessionRequest) (clientruntime.ApplicationReadyPeerSession, error) {
	if request.GetEndpointId() == "cancel" {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if request.GetEndpointId() != "android-spike" {
		return nil, fmt.Errorf("unsupported Android spike endpoint %q", request.GetEndpointId())
	}
	target := endpoint.Endpoint{ID: "android-spike", DaemonIdentity: endpoint.DaemonIdentity{DeviceID: host.identity.DeviceID, DeviceFingerprint: host.identity.Fingerprint}, Routes: map[endpoint.RouteID]endpoint.AccessRoute{
		"webrtc": {ID: "webrtc", Kind: endpoint.RouteManagedWebRTC, Enabled: true, Source: endpoint.SourceCloud, PolicySource: endpoint.SourceUser,
			CredentialRef: "credential:android-spike", TargetDeviceID: host.identity.DeviceID, AccountProfileRef: "default", RelayMode: endpoint.RelayDirect},
	}}
	dialer := &managed.Dialer{Cloud: androidSpikeCompanion(host.ctx, host.answerer), Peers: pionadapter.Factory{}, ClientName: "android-go-client",
		Authorization: managed.CapabilityAuthorizer{Credentials: androidSpikeCredentialSource{credential: host.credential}, Now: func() time.Time { return host.now }}, Now: func() time.Time { return host.now }}
	lease, err := host.owner.ConnectRoute(ctx, target, "webrtc", clientruntime.ConnectIntentInteractive, dialer)
	if err != nil {
		return nil, err
	}
	return host.owner.ApplicationSession(lease)
}

func (host *androidSpikeHost) close() error {
	var err error
	host.closeOnce.Do(func() {
		host.cancel()
		_ = host.owner.Close()
		err = host.store.Close()
	})
	return err
}

type androidSpikeCredentialSource struct {
	credential remoteauth.ClientAccessCredential
}

func (source androidSpikeCredentialSource) ResolveClientCredential(_ context.Context, endpointID, credentialRef string) (remoteauth.ClientAccessCredential, error) {
	if endpointID != "android-spike" || credentialRef != "credential:android-spike" {
		return remoteauth.ClientAccessCredential{}, fmt.Errorf("Android spike credential reference is invalid")
	}
	return source.credential, nil
}

func androidSpikeCompanion(daemonCtx context.Context, answerer remotev2webrtc.Answerer) *cloudcompanion.FakeClient {
	return &cloudcompanion.FakeClient{
		ResolveEndpointFunc: func(_ context.Context, request *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error) {
			return &cloudpb.ResolvedEndpoint{EndpointId: request.GetEndpointId(), TargetDeviceId: request.GetTargetDeviceId(), ManagedSessionId: "android-managed-1"}, nil
		},
		CreateSignalingSessionFunc: func(_ context.Context, request *cloudpb.CreateSignalingSessionRequest) (cloudcompanion.SignalingStream, error) {
			answer, err := answerer.Answer(daemonCtx, &cloudpb.SignalingOffer{SignalingSessionId: "android-signal-1", ManagedSessionId: request.GetManagedSessionId(), Sdp: request.GetOfferSdp()}, nil)
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
