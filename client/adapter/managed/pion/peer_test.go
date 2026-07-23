package pion

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	pionwebrtc "github.com/pion/webrtc/v4"
)

func TestPeerConfigurationEnforcesCandidatePolicy(t *testing.T) {
	servers := []*cloudpb.IceServer{{
		Urls: []string{"stun:stun.example.com", "turn:turn.example.com"}, Username: "client", Credential: "secret",
	}}
	relay, err := peerConfiguration(servers, cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY, true)
	if err != nil {
		t.Fatal(err)
	}
	if relay.ICETransportPolicy != pionwebrtc.ICETransportPolicyRelay || len(relay.ICEServers) != 1 || len(relay.ICEServers[0].URLs) != 2 {
		t.Fatalf("relay configuration = %#v", relay)
	}
	direct, err := peerConfiguration(servers, cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(direct.ICEServers) != 1 || len(direct.ICEServers[0].URLs) != 1 || direct.ICEServers[0].URLs[0] != "stun:stun.example.com" {
		t.Fatalf("direct configuration retained TURN material: %#v", direct)
	}
	if _, err := peerConfiguration(servers, cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY, true); err == nil {
		t.Fatal("direct-only route accepted relay-only policy")
	}
}

func TestWaitReadyReturnsWhenChannelClosesBeforeOpen(t *testing.T) {
	peer := &managedPeer{ready: make(chan struct{}), channelClosed: make(chan struct{}), readyTimeout: time.Second}
	close(peer.channelClosed)
	if err := peer.WaitReady(context.Background()); err == nil {
		t.Fatal("closed DataChannel was reported ready")
	}
}

func TestWaitReadyReturnsPeerFailureAndTimeout(t *testing.T) {
	connectionFailed := make(chan error, 1)
	connectionFailed <- errors.New("ICE failed")
	peer := &managedPeer{ready: make(chan struct{}), channelClosed: make(chan struct{}), connectionFailed: connectionFailed, readyTimeout: time.Second}
	if err := peer.WaitReady(context.Background()); err == nil || err.Error() != "ICE failed" {
		t.Fatalf("peer failure = %v", err)
	}

	peer = &managedPeer{ready: make(chan struct{}), channelClosed: make(chan struct{}), connectionFailed: make(chan error), readyTimeout: time.Millisecond}
	if err := peer.WaitReady(context.Background()); err == nil {
		t.Fatal("WebRTC ready timeout unexpectedly succeeded")
	}
}
