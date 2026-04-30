package termx

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core/internal/remote/bridge"
	"github.com/lozzow/termx/termx-core/internal/remote/fileapi"
	remotertc "github.com/lozzow/termx/termx-core/internal/remote/rtc"
	"github.com/lozzow/termx/termx-core/protocol"
	hubv1 "github.com/lozzow/termx/termx-core/remote/hubv1"
	"github.com/pion/webrtc/v4"
)

func TestE2E_WebRTCTerminalAttachOverNativeProtocol(t *testing.T) {
	srv := NewServer()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	info, err := srv.Create(ctx, CreateOptions{
		Command: []string{"bash", "--noprofile", "--norc"},
		Name:    "webrtc-e2e",
		Size:    Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted: %v", err)
		}
		t.Fatalf("Create returned error: %v", err)
	}

	offerPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection returned error: %v", err)
	}
	defer offerPC.Close()

	dc, err := offerPC.CreateDataChannel("terminal:"+info.ID, nil)
	if err != nil {
		t.Fatalf("CreateDataChannel returned error: %v", err)
	}

	openCh := make(chan struct{})
	dc.OnOpen(func() {
		select {
		case <-openCh:
		default:
			close(openCh)
		}
	})

	offer, err := offerPC.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer returned error: %v", err)
	}
	if err := offerPC.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription returned error: %v", err)
	}
	waitPeerICE(t, offerPC, 5*time.Second)

	answer, err := remotertc.AnswerOffer(ctx, hubv1.SignalingOffer{
		SessionID:     "session-1",
		DeviceID:      "device-1",
		TerminalID:    info.ID,
		SDP:           offerPC.LocalDescription().SDP,
		ICECandidates: nil,
	}, nil, remoteInventoryProvider{server: srv}, fileapi.NewManager())
	if err != nil {
		t.Fatalf("AnswerOffer returned error: %v", err)
	}
	if err := offerPC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answer.SDP,
	}); err != nil {
		t.Fatalf("SetRemoteDescription returned error: %v", err)
	}

	select {
	case <-openCh:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for data channel open")
	}

	clientTransport := bridge.NewDataChannelTransport(dc)
	defer clientTransport.Close()
	client := protocol.NewClient(clientTransport)
	defer client.Close()

	if err := client.Hello(ctx, protocol.Hello{Version: protocol.Version, Client: "webrtc-e2e"}); err != nil {
		t.Fatalf("Hello returned error: %v", err)
	}

	snapshot, err := client.Snapshot(ctx, info.ID, 0, 0)
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}
	if snapshot.TerminalID != info.ID {
		t.Fatalf("expected snapshot terminal %q, got %q", info.ID, snapshot.TerminalID)
	}

	attach, err := client.Attach(ctx, info.ID, string(ModeCollaborator))
	if err != nil {
		t.Fatalf("Attach returned error: %v", err)
	}
	stream, stop := client.Stream(attach.Channel)
	defer stop()

	if err := client.Input(ctx, attach.Channel, []byte("echo webrtc-attach-ok\n")); err != nil {
		t.Fatalf("Input returned error: %v", err)
	}
	waitStreamContains(t, stream, "webrtc-attach-ok", 8*time.Second)
}

func waitPeerICE(t *testing.T, pc *webrtc.PeerConnection, timeout time.Duration) {
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
