package relay_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/relay"
	"github.com/pion/webrtc/v4"
)

func TestDirectAndLeaseBoundTURNCarryOpaqueDataChannel(t *testing.T) {
	directPair := exchangeDataChannel(t, webrtc.Configuration{}, webrtc.Configuration{}, "direct-e2e-marker")
	defer directPair.close()
	if directPair.candidateType != webrtc.ICECandidateTypeHost {
		t.Fatalf("direct candidate type = %s, want host", directPair.candidateType)
	}

	fixture := newRelayFixture(t, 8, 10_000_000, 1_000_000)
	activation, err := fixture.authority.ActivateLease(fixture.activationRequest)
	if err != nil {
		t.Fatal(err)
	}
	server, err := relay.NewServer(relay.ServerConfig{Authority: fixture.authority, ListenAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	clientConfig := relayWebRTCConfig(server.URL(), activation.ClientCredential)
	daemonConfig := relayWebRTCConfig(server.URL(), activation.DaemonCredential)
	relayPair := exchangeDataChannel(t, clientConfig, daemonConfig, "relay-e2e-secret-marker")
	defer relayPair.close()
	if relayPair.candidateType != webrtc.ICECandidateTypeRelay {
		t.Fatalf("managed candidate type = %s, want relay", relayPair.candidateType)
	}

	fixture.clock.Advance(3 * time.Second)
	events, err := fixture.authority.DrainUsage("")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].BytesUp+events[0].BytesDown == 0 {
		t.Fatalf("TURN usage events = %#v", events)
	}
}

type peerPair struct {
	left          *webrtc.PeerConnection
	right         *webrtc.PeerConnection
	candidateType webrtc.ICECandidateType
}

func (pair peerPair) close() {
	_ = pair.left.Close()
	_ = pair.right.Close()
}

func relayWebRTCConfig(url string, credential relay.Credential) webrtc.Configuration {
	return webrtc.Configuration{
		ICEServers:         []webrtc.ICEServer{{URLs: []string{url}, Username: credential.Username, Credential: credential.Password}},
		ICETransportPolicy: webrtc.ICETransportPolicyRelay,
	}
}

func exchangeDataChannel(t *testing.T, leftConfig, rightConfig webrtc.Configuration, marker string) peerPair {
	t.Helper()
	settingEngine := webrtc.SettingEngine{}
	settingEngine.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	settingEngine.SetIncludeLoopbackCandidate(true)
	api := webrtc.NewAPI(webrtc.WithSettingEngine(settingEngine))
	left, err := api.NewPeerConnection(leftConfig)
	if err != nil {
		t.Fatal(err)
	}
	right, err := api.NewPeerConnection(rightConfig)
	if err != nil {
		_ = left.Close()
		t.Fatal(err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = left.Close()
			_ = right.Close()
		}
	}()

	received := make(chan string, 1)
	right.OnDataChannel(func(channel *webrtc.DataChannel) {
		channel.OnMessage(func(message webrtc.DataChannelMessage) {
			received <- string(message.Data)
		})
	})
	leftChannel, err := left.CreateDataChannel("protocol", nil)
	if err != nil {
		t.Fatal(err)
	}
	opened := make(chan struct{})
	leftChannel.OnOpen(func() { close(opened) })

	offer, err := left.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	leftGathered := webrtc.GatheringCompletePromise(left)
	if err := left.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, leftGathered, "left ICE gathering")
	if err := right.SetRemoteDescription(*left.LocalDescription()); err != nil {
		t.Fatal(err)
	}
	answer, err := right.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	rightGathered := webrtc.GatheringCompletePromise(right)
	if err := right.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, rightGathered, "right ICE gathering")
	if err := left.SetRemoteDescription(*right.LocalDescription()); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, opened, "DataChannel open")
	if err := leftChannel.SendText(marker); err != nil {
		t.Fatal(err)
	}
	select {
	case value := <-received:
		if value != marker {
			t.Fatalf("DataChannel payload = %q, want %q", value, marker)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for DataChannel payload")
	}
	pair, err := left.SCTP().Transport().ICETransport().GetSelectedCandidatePair()
	if err != nil || pair == nil || pair.Local == nil {
		t.Fatalf("selected ICE candidate pair = %#v, %v", pair, err)
	}
	cleanup = false
	return peerPair{left: left, right: right, candidateType: pair.Local.Typ}
}

func waitSignal(t *testing.T, signal <-chan struct{}, operation string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatal(fmt.Errorf("%s: %w", operation, ctx.Err()))
	}
}
