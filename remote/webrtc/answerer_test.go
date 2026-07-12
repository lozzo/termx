package webrtc

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/transport"
	pion "github.com/pion/webrtc/v4"
)

func TestAnswererHandsReliableChannelToAuthorizedHandler(t *testing.T) {
	handler := &recordingAuthorizedHandler{called: make(chan struct{})}
	answerer := Answerer{Handler: handler}
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
	offer := createGatheredOffer(t, clientPeer)
	answer, err := answerer.Answer(context.Background(), &cloudpb.SignalingOffer{
		SignalingSessionId: "signal-1", Sdp: offer.SDP,
	}, nil)
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if answer.GetSignalingSessionId() != "signal-1" || answer.GetSdp() == "" {
		t.Fatalf("answer = %+v", answer)
	}
	if len(answer.GetCandidates()) == 0 {
		t.Fatal("answer must explicitly publish gathered daemon candidates")
	}
	if err := clientPeer.SetRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeAnswer, SDP: answer.GetSdp()}); err != nil {
		t.Fatalf("set remote answer: %v", err)
	}
	for _, candidate := range answer.GetCandidates() {
		if err := clientPeer.AddICECandidate(pion.ICECandidateInit{
			Candidate: candidate.GetCandidate(), SDPMid: stringPointer(candidate.GetSdpMid()),
			SDPMLineIndex:    uint16Pointer(uint16(candidate.GetSdpMlineIndex())),
			UsernameFragment: stringPointer(candidate.GetUsernameFragment()),
		}); err != nil {
			t.Fatalf("add remote candidate: %v", err)
		}
	}
	select {
	case <-opened:
	case <-time.After(10 * time.Second):
		t.Fatal("termx protocol data channel did not open")
	}
	select {
	case <-handler.called:
	case <-time.After(10 * time.Second):
		t.Fatal("authorized channel handler was not invoked")
	}
	clientFingerprint, err := RemoteCertificateFingerprint(clientPeer)
	if err != nil {
		t.Fatalf("RemoteCertificateFingerprint: %v", err)
	}
	if handler.daemonDTLSFingerprint == "" || handler.daemonDTLSFingerprint != clientFingerprint {
		t.Fatalf("daemon fingerprint = %q, client observed %q", handler.daemonDTLSFingerprint, clientFingerprint)
	}
}

func stringPointer(value string) *string { return &value }

func uint16Pointer(value uint16) *uint16 { return &value }

func TestAnswererFailsClosedWithoutAuthorizedHandler(t *testing.T) {
	if _, err := (Answerer{}).Answer(context.Background(), &cloudpb.SignalingOffer{Sdp: "not-used"}, nil); err == nil {
		t.Fatal("missing authorized handler must fail before WebRTC session creation")
	}
}

func TestRelayOnlyOfferRejectsNonRelayCandidate(t *testing.T) {
	answerer := Answerer{Handler: &recordingAuthorizedHandler{called: make(chan struct{})}}
	_, err := answerer.Answer(context.Background(), &cloudpb.SignalingOffer{
		Sdp:       "v=0\r\na=candidate:1 1 udp 1 192.0.2.1 1234 typ host\r\n",
		RelayOnly: true, RoutePreference: cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY,
	}, []*cloudpb.IceServer{{Urls: []string{"turn:127.0.0.1:3478"}}})
	if err == nil || !strings.Contains(err.Error(), "non-relay") {
		t.Fatalf("relay-only host candidate error = %v", err)
	}
}

func createGatheredOffer(t *testing.T, peer *pion.PeerConnection) pion.SessionDescription {
	t.Helper()
	offer, err := peer.CreateOffer(nil)
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}
	gatherComplete := pion.GatheringCompletePromise(peer)
	if err := peer.SetLocalDescription(offer); err != nil {
		t.Fatalf("set local offer: %v", err)
	}
	select {
	case <-gatherComplete:
	case <-time.After(5 * time.Second):
		t.Fatal("client ICE gathering timed out")
	}
	description := peer.LocalDescription()
	if description == nil || description.SDP == "" {
		t.Fatal("client offer has no local SDP")
	}
	return *description
}

type recordingAuthorizedHandler struct {
	called                chan struct{}
	daemonDTLSFingerprint string
}

func (handler *recordingAuthorizedHandler) ServeDataChannel(ctx context.Context, _ transport.Transport, daemonDTLSFingerprint string) error {
	handler.daemonDTLSFingerprint = daemonDTLSFingerprint
	close(handler.called)
	<-ctx.Done()
	return ctx.Err()
}
