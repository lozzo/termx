package managed

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/muxvia/muxvia/client/endpoint"
	"github.com/muxvia/muxvia/client/port"
	clientruntime "github.com/muxvia/muxvia/client/runtime"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/proto/remoteauthpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
	"github.com/muxvia/muxvia/shared/remoteauth"
	"github.com/muxvia/muxvia/shared/transport/datachannel"
)

func TestManagedPairingKeepsClaimAndBundleInsideDataChannel(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	daemon, err := remoteauth.NewIdentity("device-1", ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x41}, ed25519.SeedSize)))
	if err != nil {
		t.Fatal(err)
	}
	store, err := remoteauth.LoadAccessStore(t.TempDir(), daemon, remoteauth.AccessStoreOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	issued, err := store.IssuePairingClaim(remoteauth.PairingIssueOptions{
		Scope: remoteauth.Scope{AllowDaemon: true}, TicketTTL: 10 * time.Minute, GrantLifetime: time.Hour, Now: now,
		Routes: []*remoteauthpb.EndpointRouteConfigV1{{SchemaVersion: 1, RouteId: "cloud", Enabled: true, Route: &remoteauthpb.EndpointRouteConfigV1_ManagedWebrtc{ManagedWebrtc: &remoteauthpb.ManagedWebRTCRouteConfig{TargetDeviceId: daemon.DeviceID}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	clientChannel, serverChannel := newManagedMessageChannelPair()
	fingerprint := managedPairingFingerprint(0x37)
	peer := &fakeManagedPeer{channel: clientChannel, observedPath: endpoint.PathDirect, fingerprint: fingerprint}
	stream := cloudcompanion.NewFakeSignalingStream(1)
	if err := stream.Push(&cloudpb.SignalingEvent{Payload: &cloudpb.SignalingEvent_Answer{Answer: &cloudpb.SignalingAnswer{Sdp: "answer-sdp"}}}); err != nil {
		t.Fatal(err)
	}
	cloud := &cloudcompanion.FakeClient{
		ResolveEndpointFunc: func(_ context.Context, request *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error) {
			return &cloudpb.ResolvedEndpoint{EndpointId: request.GetEndpointId(), TargetDeviceId: daemon.DeviceID, ManagedSessionId: "managed-pairing"}, nil
		},
		CreateSignalingSessionFunc: func(_ context.Context, request *cloudpb.CreateSignalingSessionRequest) (cloudcompanion.SignalingStream, error) {
			if bytes.Contains([]byte(request.GetOfferSdp()), issued.OfferPayload) || bytes.Contains([]byte(request.GetOfferSdp()), issued.BundlePayload) {
				t.Fatal("Cloud signaling observed pairing credential")
			}
			return stream, nil
		},
	}
	identity := endpoint.DaemonIdentity{DeviceID: daemon.DeviceID, DeviceFingerprint: daemon.Fingerprint}
	target := endpoint.Endpoint{ID: "device-1", DaemonIdentity: identity, Routes: map[endpoint.RouteID]endpoint.AccessRoute{
		"cloud": {ID: "cloud", Kind: endpoint.RouteManagedWebRTC, Enabled: true, Source: endpoint.SourceCloud, PolicySource: endpoint.SourceUser, CredentialRef: "credential:device-1", TargetDeviceID: daemon.DeviceID, AccountProfileRef: "default", RelayMode: endpoint.RelayDirect},
	}}
	attempt, err := clientruntime.NewAttemptRequest(target, "cloud", 1, clientruntime.ConnectIntentInteractive)
	if err != nil {
		t.Fatal(err)
	}
	clientIdentity, err := remoteauth.GenerateClientAccessIdentity("device-1", bytes.NewReader(bytes.Repeat([]byte{0x72}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	signer, err := remoteauth.NewPrivateClientAccessSigner(clientIdentity)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := remoteauth.DTLSChannelBinding(fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	serverDone := make(chan error, 1)
	go func() {
		connection := datachannel.New(serverChannel)
		defer connection.Close()
		_, acceptErr := (remoteauth.ServerHandshake{Identity: daemon, AccessStore: store, Now: func() time.Time { return now }}).Accept(context.Background(), connection, binding)
		serverDone <- acceptErr
	}()
	result, err := (&PairingConnector{Cloud: cloud, Peers: fakePeerFactory{peer: peer}, Now: func() time.Time { return now }}).Redeem(context.Background(), attempt, remoteauth.ClientPairingRequest{
		ExpectedDeviceID: daemon.DeviceID, ExpectedDeviceFingerprint: daemon.Fingerprint,
		PairingClaimOffer: issued.OfferPayload, Identity: clientIdentity, Signer: signer, ClientLabel: "Android",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	if result.TicketID != issued.Claims.TicketID || !bytes.Equal(result.Bundle, issued.BundlePayload) || !peer.closed {
		t.Fatalf("managed pairing result=%#v peer.closed=%v", result, peer.closed)
	}
}

type pairedManagedMessageChannel struct {
	mu             sync.RWMutex
	peer           *pairedManagedMessageChannel
	messageHandler func([]byte)
	closeHandler   func()
	closed         bool
}

func newManagedMessageChannelPair() (*pairedManagedMessageChannel, *pairedManagedMessageChannel) {
	left, right := &pairedManagedMessageChannel{}, &pairedManagedMessageChannel{}
	left.peer, right.peer = right, left
	return left, right
}

func (channel *pairedManagedMessageChannel) Send(payload []byte) error {
	channel.peer.mu.RLock()
	handler, closed := channel.peer.messageHandler, channel.peer.closed
	channel.peer.mu.RUnlock()
	if closed || handler == nil {
		return fmt.Errorf("paired managed channel is unavailable")
	}
	handler(append([]byte(nil), payload...))
	return nil
}
func (channel *pairedManagedMessageChannel) SetMessageHandler(handler func([]byte)) {
	channel.mu.Lock()
	channel.messageHandler = handler
	channel.mu.Unlock()
}
func (channel *pairedManagedMessageChannel) SetCloseHandler(handler func()) {
	channel.mu.Lock()
	channel.closeHandler = handler
	channel.mu.Unlock()
}
func (channel *pairedManagedMessageChannel) BufferedAmount() uint64               { return 0 }
func (channel *pairedManagedMessageChannel) SetBufferedAmountLowThreshold(uint64) {}
func (channel *pairedManagedMessageChannel) SetBufferedAmountLowHandler(func())   {}
func (channel *pairedManagedMessageChannel) Close() error {
	channel.mu.Lock()
	if channel.closed {
		channel.mu.Unlock()
		return nil
	}
	channel.closed = true
	handler := channel.closeHandler
	channel.mu.Unlock()
	if handler != nil {
		handler()
	}
	return nil
}

func managedPairingFingerprint(value byte) string {
	result := "sha-256:"
	for index := 0; index < 32; index++ {
		if index > 0 {
			result += ":"
		}
		result += hex.EncodeToString([]byte{value})
	}
	return result
}

var _ port.ManagedMessageChannel = (*pairedManagedMessageChannel)(nil)
