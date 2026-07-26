package webrtc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/muxvia/muxvia/shared/transport"
	pion "github.com/pion/webrtc/v4"
)

func TestAnswererHandsReliableChannelToAuthorizedHandler(t *testing.T) {
	handler := &recordingAuthorizedHandler{called: make(chan struct{}), result: make(chan error)}
	sessionErrors := make(chan error, 1)
	answerer := Answerer{Handler: handler, OnSessionError: func(err error) { sessionErrors <- err }}
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
	answer, err := answerer.Answer(context.Background(), &SignalingOffer{
		SessionID: "signal-1", SDP: offer.SDP,
	}, nil)
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if answer.SessionID != "signal-1" || answer.SDP == "" {
		t.Fatalf("answer = %+v", answer)
	}
	if len(answer.Candidates) == 0 {
		t.Fatal("answer must explicitly publish gathered daemon candidates")
	}
	if err := clientPeer.SetRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeAnswer, SDP: answer.SDP}); err != nil {
		t.Fatalf("set remote answer: %v", err)
	}
	for _, candidate := range answer.Candidates {
		if err := clientPeer.AddICECandidate(pion.ICECandidateInit{
			Candidate: candidate.Candidate, SDPMid: stringPointer(candidate.SDPMid),
			SDPMLineIndex:    uint16Pointer(uint16(candidate.SDPMLineIndex)),
			UsernameFragment: stringPointer(candidate.UsernameFragment),
		}); err != nil {
			t.Fatalf("add remote candidate: %v", err)
		}
	}
	select {
	case <-opened:
	case <-time.After(10 * time.Second):
		t.Fatal("muxvia protocol data channel did not open")
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
	sessionErr := errors.New("authorized session failed")
	handler.result <- sessionErr
	select {
	case got := <-sessionErrors:
		if !errors.Is(got, sessionErr) {
			t.Fatalf("session error = %v, want %v", got, sessionErr)
		}
	case <-time.After(time.Second):
		t.Fatal("authorized handler failure was not reported")
	}
}

func stringPointer(value string) *string { return &value }

func uint16Pointer(value uint16) *uint16 { return &value }

func TestAnswererFailsClosedWithoutAuthorizedHandler(t *testing.T) {
	if _, err := (Answerer{}).Answer(context.Background(), &SignalingOffer{SDP: "not-used"}, nil); err == nil {
		t.Fatal("missing authorized handler must fail before WebRTC session creation")
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
	result                chan error
	daemonDTLSFingerprint string
}

func (handler *recordingAuthorizedHandler) ServeDataChannel(ctx context.Context, _ transport.Transport, daemonDTLSFingerprint string) error {
	handler.daemonDTLSFingerprint = daemonDTLSFingerprint
	close(handler.called)
	if handler.result == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	select {
	case err := <-handler.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
