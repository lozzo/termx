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

func TestAnswerOfferSurvivesCanceledRequestContext(t *testing.T) {
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	sessionCtx, cancelSession := context.WithCancel(context.Background())
	defer cancelSession()
	offerPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection returned error: %v", err)
	}
	defer offerPC.Close()

	apiDC, err := offerPC.CreateDataChannel("api", nil)
	if err != nil {
		t.Fatalf("CreateDataChannel(api) returned error: %v", err)
	}
	apiOpen := make(chan struct{})
	apiDC.OnOpen(func() {
		select {
		case <-apiOpen:
		default:
			close(apiOpen)
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

	answer, err := AnswerOfferWithOptions(requestCtx, hubv1.SignalingOffer{
		SessionID:  "request-context-session",
		DeviceID:   "machine-1",
		TerminalID: "terminal-1",
		SDP:        offerPC.LocalDescription().SDP,
	}, nil, nil, fileapi.NewManager(), AnswerOptions{SessionContext: sessionCtx})
	if err != nil {
		t.Fatalf("AnswerOfferWithOptions returned error: %v", err)
	}
	cancelRequest()
	if err := offerPC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answer.SDP,
	}); err != nil {
		t.Fatalf("SetRemoteDescription returned error: %v", err)
	}

	select {
	case <-apiOpen:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for api data channel to open after request context cancellation")
	}
}

func TestAnswerOfferDefaultSessionContextFollowsCallerContext(t *testing.T) {
	callerCtx, cancelCaller := context.WithCancel(context.Background())
	offerPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection returned error: %v", err)
	}
	defer offerPC.Close()

	apiDC, err := offerPC.CreateDataChannel("api", nil)
	if err != nil {
		t.Fatalf("CreateDataChannel(api) returned error: %v", err)
	}
	apiOpen := make(chan struct{})
	apiClosed := make(chan struct{})
	apiDC.OnOpen(func() {
		select {
		case <-apiOpen:
		default:
			close(apiOpen)
		}
	})
	apiDC.OnClose(func() {
		select {
		case <-apiClosed:
		default:
			close(apiClosed)
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

	answer, err := AnswerOfferWithOptions(callerCtx, hubv1.SignalingOffer{
		SessionID:  "default-context-session",
		DeviceID:   "machine-1",
		TerminalID: "terminal-1",
		SDP:        offerPC.LocalDescription().SDP,
	}, nil, nil, fileapi.NewManager(), AnswerOptions{})
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
	case <-apiOpen:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for api data channel to open before caller cancellation")
	}
	cancelCaller()
	select {
	case <-apiClosed:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for api data channel to close after caller context cancellation")
	}
}

func TestAnswerOfferSessionContextClosesDataChannel(t *testing.T) {
	sessionCtx, cancelSession := context.WithCancel(context.Background())
	offerPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection returned error: %v", err)
	}
	defer offerPC.Close()

	apiDC, err := offerPC.CreateDataChannel("api", nil)
	if err != nil {
		t.Fatalf("CreateDataChannel(api) returned error: %v", err)
	}
	apiOpen := make(chan struct{})
	apiClosed := make(chan struct{})
	apiDC.OnOpen(func() {
		select {
		case <-apiOpen:
		default:
			close(apiOpen)
		}
	})
	apiDC.OnClose(func() {
		select {
		case <-apiClosed:
		default:
			close(apiClosed)
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
		SessionID:  "session-context-session",
		DeviceID:   "machine-1",
		TerminalID: "terminal-1",
		SDP:        offerPC.LocalDescription().SDP,
	}, nil, nil, fileapi.NewManager(), AnswerOptions{SessionContext: sessionCtx})
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
	case <-apiOpen:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for api data channel to open before session cancellation")
	}
	cancelSession()
	select {
	case <-apiClosed:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for api data channel to close after session context cancellation")
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
