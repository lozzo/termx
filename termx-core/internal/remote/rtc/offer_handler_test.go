package rtc

import (
	"context"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core/internal/remote/fileapi"
	hubv1 "github.com/lozzow/termx/termx-core/remote/hubv1"
	"github.com/pion/webrtc/v4"
)

func TestAnswerOfferChannelPolicyRejectsWrongTerminalChannel(t *testing.T) {
	offerPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection returned error: %v", err)
	}
	defer offerPC.Close()

	dc, err := offerPC.CreateDataChannel("terminal:other-terminal", nil)
	if err != nil {
		t.Fatalf("CreateDataChannel returned error: %v", err)
	}
	closed := make(chan struct{})
	dc.OnClose(func() {
		select {
		case <-closed:
		default:
			close(closed)
		}
	})

	offer, err := offerPC.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer returned error: %v", err)
	}
	if err := offerPC.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription returned error: %v", err)
	}
	waitTestPeerICE(t, offerPC, 5*time.Second)

	answer, err := AnswerOfferWithOptions(context.Background(), hubv1.SignalingOffer{
		SessionID:  "channel-policy-session",
		DeviceID:   "machine-1",
		TerminalID: "signed-terminal",
		SDP:        offerPC.LocalDescription().SDP,
	}, nil, nil, fileapi.NewManager(), AnswerOptions{
		ChannelPolicy: ChannelPolicy{
			TerminalID:       "signed-terminal",
			AllowTerminal:    true,
			AllowFileManager: true,
		},
	})
	if err != nil {
		t.Fatalf("AnswerOfferWithOptions returned error: %v", err)
	}
	if err := offerPC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answer.SDP,
	}); err != nil {
		t.Fatalf("SetRemoteDescription returned error: %v", err)
	}

	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for unauthorized terminal data channel to close")
	}
}

func waitTestPeerICE(t *testing.T, pc *webrtc.PeerConnection, timeout time.Duration) {
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
