package webrtc

import (
	"context"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-proto/cloudpb"
	"github.com/lozzow/termx/termx-shared/transport/datachannel"
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
	if err := clientPeer.SetRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeAnswer, SDP: answer.GetSdp()}); err != nil {
		t.Fatalf("set remote answer: %v", err)
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
}

func TestAnswererFailsClosedWithoutAuthorizedHandler(t *testing.T) {
	if _, err := (Answerer{}).Answer(context.Background(), &cloudpb.SignalingOffer{Sdp: "not-used"}, nil); err == nil {
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
	called chan struct{}
}

func (handler *recordingAuthorizedHandler) ServeDataChannel(ctx context.Context, _ datachannel.Channel) error {
	close(handler.called)
	<-ctx.Done()
	return ctx.Err()
}
