package agent

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/transport"
	"github.com/lozzow/termx/transport/memory"
	rtctransport "github.com/lozzow/termx/transport/webrtc"
	"github.com/pion/webrtc/v4"
)

func TestWebRTCHandlerBridgesDataChannelToLocalTransport(t *testing.T) {
	localClient, bridgeLocal := memory.NewPair()
	defer localClient.Close()

	handler := NewWebRTCHandler(func(context.Context) (transport.Transport, error) {
		return bridgeLocal, nil
	})
	defer handler.Close()

	clientPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("create client peer connection: %v", err)
	}
	defer clientPC.Close()

	dc, err := clientPC.CreateDataChannel(termxDataChannelLabel, nil)
	if err != nil {
		t.Fatalf("create data channel: %v", err)
	}

	openCh := make(chan struct{}, 1)
	dc.OnOpen(func() {
		select {
		case openCh <- struct{}{}:
		default:
		}
	})

	offer, err := clientPC.CreateOffer(nil)
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}
	gatherComplete := webrtc.GatheringCompletePromise(clientPC)
	if err := clientPC.SetLocalDescription(offer); err != nil {
		t.Fatalf("set local description: %v", err)
	}
	select {
	case <-gatherComplete:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out gathering local ICE")
	}

	answer, err := handler.HandleLocalOffer(context.Background(), LocalOfferRequest{
		SDP: clientPC.LocalDescription().SDP,
	})
	if err != nil {
		t.Fatalf("HandleLocalOffer returned error: %v", err)
	}
	if err := clientPC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answer.SDP,
	}); err != nil {
		t.Fatalf("set remote description: %v", err)
	}

	select {
	case <-openCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for data channel open")
	}

	remoteTransport := rtctransport.NewTransport(rtctransport.NewPionChannel(dc))
	defer remoteTransport.Close()

	outbound, err := protocol.EncodeFrame(7, protocol.TypeInput, []byte("echo hi\n"))
	if err != nil {
		t.Fatalf("encode outbound frame: %v", err)
	}
	if err := remoteTransport.Send(outbound); err != nil {
		t.Fatalf("remote send failed: %v", err)
	}
	gotLocal, err := localClient.Recv()
	if err != nil {
		t.Fatalf("local recv failed: %v", err)
	}
	if !bytes.Equal(gotLocal, outbound) {
		t.Fatalf("unexpected bridged outbound frame")
	}

	inbound, err := protocol.EncodeFrame(7, protocol.TypeOutput, []byte("hello"))
	if err != nil {
		t.Fatalf("encode inbound frame: %v", err)
	}
	if err := localClient.Send(inbound); err != nil {
		t.Fatalf("local send failed: %v", err)
	}
	gotRemote, err := remoteTransport.Recv()
	if err != nil {
		t.Fatalf("remote recv failed: %v", err)
	}
	if !bytes.Equal(gotRemote, inbound) {
		t.Fatalf("unexpected bridged inbound frame")
	}
}
