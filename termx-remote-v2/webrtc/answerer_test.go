package webrtc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	termxcorev2 "github.com/lozzow/termx/termx-core-v2"
	hubclient "github.com/lozzow/termx/termx-hub/client"
	"github.com/lozzow/termx/termx-proto/wire"
	"github.com/lozzow/termx/termx-remote-v2/daemon"
	"github.com/lozzow/termx/termx-shared/remoteauth"
	"github.com/lozzow/termx/termx-shared/transport/datachannel"
	pion "github.com/pion/webrtc/v4"
)

func TestAnswererCarriesTermxProtocolToScopedCore(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate device identity: %v", err)
	}
	now := time.Now().UTC()
	grant, err := remoteauth.Issue(privateKey, remoteauth.Claims{
		GrantID: "grant-daemon", DeviceID: "device-1", Scope: remoteauth.Scope{AllowDaemon: true},
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("issue grant: %v", err)
	}
	core := termxcorev2.NewServer()
	answerer := Answerer{Acceptor: daemon.SessionAcceptor{
		Core: core, DeviceFingerprint: remoteauth.Fingerprint(publicKey), Revocations: remoteauth.NewRevocations(),
	}}

	clientPeer, err := pion.NewPeerConnection(pion.Configuration{})
	if err != nil {
		t.Fatalf("create client peer: %v", err)
	}
	defer clientPeer.Close()
	channel, err := clientPeer.CreateDataChannel(protocolChannelLabel, nil)
	if err != nil {
		t.Fatalf("create protocol data channel: %v", err)
	}
	opened := make(chan struct{})
	channel.OnOpen(func() { close(opened) })
	offer, err := clientPeer.CreateOffer(nil)
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}
	gatherComplete := pion.GatheringCompletePromise(clientPeer)
	if err := clientPeer.SetLocalDescription(offer); err != nil {
		t.Fatalf("set local offer: %v", err)
	}
	select {
	case <-gatherComplete:
	case <-time.After(5 * time.Second):
		t.Fatal("client ICE gathering timed out")
	}
	localDescription := clientPeer.LocalDescription()
	if localDescription == nil || localDescription.SDP == "" {
		localDescription = clientPeer.PendingLocalDescription()
	}
	if localDescription == nil || localDescription.SDP == "" {
		t.Fatal("client offer has no local SDP")
	}
	answer, err := answerer.Answer(context.Background(), hubclient.Offer{
		SessionID: "session-1", SDP: localDescription.SDP, CapabilityGrant: grant,
	}, hubclient.RegistrationAck{})
	if err != nil {
		t.Fatalf("answer offer: %v", err)
	}
	if err := clientPeer.SetRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeAnswer, SDP: answer.SDP}); err != nil {
		t.Fatalf("set remote answer: %v", err)
	}
	select {
	case <-opened:
	case <-time.After(10 * time.Second):
		t.Fatal("termx protocol data channel did not open")
	}

	client := protocol.NewClient(datachannel.New(NewChannel(channel)))
	defer client.Close()
	if err := client.Hello(context.Background(), protocol.Hello{Version: wire.Version, Client: "remote-v2-webrtc-test"}); err != nil {
		t.Fatalf("protocol hello over WebRTC: %v", err)
	}
	list, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("protocol list over WebRTC: %v", err)
	}
	if len(list.Terminals) != 0 {
		t.Fatalf("unexpected remote terminal list %#v", list.Terminals)
	}
}

func TestAnswererRejectsGrantBeforeCreatingSession(t *testing.T) {
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	answerer := Answerer{Acceptor: daemon.SessionAcceptor{
		Core: termxcorev2.NewServer(), DeviceFingerprint: remoteauth.Fingerprint(publicKey), Revocations: remoteauth.NewRevocations(),
	}}
	if _, err := answerer.Answer(context.Background(), hubclient.Offer{CapabilityGrant: "legacy-session-token"}, hubclient.RegistrationAck{}); err == nil {
		t.Fatal("invalid grant must be rejected before WebRTC session creation")
	}
}
