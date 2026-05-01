package rtc

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	hubv1 "github.com/lozzow/termx/termx-core/remote/hubv1"
	"github.com/pion/webrtc/v4"
)

func TestLocalICETCPMuxStartsReportsEndpointAndAppliesSettingEngine(t *testing.T) {
	mux, err := StartLocalICETCPMux(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatalf("StartLocalICETCPMux returned error: %v", err)
	}
	defer mux.Close()

	endpoint := mux.Endpoint()
	if !endpoint.Enabled {
		t.Fatalf("expected mux endpoint to be enabled: %#v", endpoint)
	}
	if endpoint.Host != "127.0.0.1" || endpoint.Port == 0 {
		t.Fatalf("unexpected endpoint: %#v", endpoint)
	}

	conn, err := net.Dial("tcp", net.JoinHostPort(endpoint.Host, endpoint.PortString()))
	if err != nil {
		t.Fatalf("expected ICE TCP listener to accept TCP connections: %v", err)
	}
	_ = conn.Close()

	var setting webrtc.SettingEngine
	mux.Apply(&setting)
}

func TestLocalICETCPMuxRejectsEmptyAddress(t *testing.T) {
	if _, err := StartLocalICETCPMux(context.Background(), ""); err == nil {
		t.Fatal("expected empty ICE TCP address to be rejected")
	}
}

func TestLocalICETCPMuxAnswerIncludesLoopbackTCPCandidate(t *testing.T) {
	mux, err := StartLocalICETCPMux(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatalf("StartLocalICETCPMux returned error: %v", err)
	}
	defer mux.Close()

	offerPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection returned error: %v", err)
	}
	defer offerPC.Close()
	if _, err := offerPC.CreateDataChannel("api", nil); err != nil {
		t.Fatalf("CreateDataChannel returned error: %v", err)
	}
	offer, err := offerPC.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer returned error: %v", err)
	}
	if err := offerPC.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription returned error: %v", err)
	}
	waitTCPMuxTestPeerICE(t, offerPC, 5*time.Second)

	answer, err := AnswerOfferWithOptions(context.Background(), hubv1.SignalingOffer{
		SessionID:  "tcp-candidate-session",
		DeviceID:   "machine-1",
		TerminalID: "terminal-1",
		SDP:        offerPC.LocalDescription().SDP,
	}, nil, nil, nil, AnswerOptions{SettingEngine: mux})
	if err != nil {
		t.Fatalf("AnswerOfferWithOptions returned error: %v", err)
	}
	if !strings.Contains(answer.SDP, " typ host tcptype passive") {
		t.Fatalf("expected answer SDP to include a passive TCP host candidate, got:\n%s", answer.SDP)
	}
	if !strings.Contains(answer.SDP, " 127.0.0.1 ") {
		t.Fatalf("expected answer SDP to include loopback TCP host candidate, got:\n%s", answer.SDP)
	}
}

func waitTCPMuxTestPeerICE(t *testing.T, pc *webrtc.PeerConnection, timeout time.Duration) {
	t.Helper()
	if pc.ICEGatheringState() == webrtc.ICEGatheringStateComplete {
		return
	}
	done := make(chan struct{})
	pc.OnICEGatheringStateChange(func(state webrtc.ICEGatheringState) {
		if state == webrtc.ICEGatheringStateComplete {
			select {
			case <-done:
			default:
				close(done)
			}
		}
	})
	select {
	case <-done:
	case <-time.After(timeout):
	}
}
