package rtc

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-remote/fileapi"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
	"github.com/pion/webrtc/v4"
)

func TestRuntimeAPIChannelRouterHandlesTerminalManagement(t *testing.T) {
	manager := &terminalAPIRouterStub{}
	pingStatus, pingBody, pingErr := routeRuntimeAPIRequest(nil, nil, apiRequest{
		ID:     "req_ping",
		Method: "ping",
		Path:   "ping",
		Body:   json.RawMessage(`{}`),
	})
	if pingErr != "" {
		t.Fatalf("expected ping request to succeed, got %q", pingErr)
	}
	if pingStatus != http.StatusOK {
		t.Fatalf("expected ping status 200, got %d", pingStatus)
	}
	var pingPayload map[string]any
	if err := json.Unmarshal(pingBody, &pingPayload); err != nil {
		t.Fatalf("unmarshal ping body: %v", err)
	}
	if pingPayload["ok"] != true {
		t.Fatalf("expected ping ok payload, got %#v", pingPayload)
	}

	listStatus, listBody, listErr := routeRuntimeAPIRequest(fileapi.NewManager(), manager, apiRequest{
		ID:     "req_list",
		Method: "list",
		Path:   "list",
		Body:   json.RawMessage(`{}`),
	})
	if listErr != "" {
		t.Fatalf("expected terminal list request to succeed, got %q", listErr)
	}
	if listStatus != http.StatusOK {
		t.Fatalf("expected terminal list status 200, got %d", listStatus)
	}
	var listPayload map[string]any
	if err := json.Unmarshal(listBody, &listPayload); err != nil {
		t.Fatalf("unmarshal list body: %v", err)
	}
	if terminals, ok := listPayload["terminals"].([]any); !ok || len(terminals) != 1 {
		t.Fatalf("expected terminal list payload, got %#v", listPayload)
	}

	status, body, errMsg := routeRuntimeAPIRequest(fileapi.NewManager(), manager, apiRequest{
		ID:     "req_1",
		Method: "create",
		Path:   "create",
		Body:   json.RawMessage(`{"command":["/bin/zsh","-l"],"name":"ops shell"}`),
	})
	if errMsg != "" {
		t.Fatalf("expected terminal management request to succeed, got %q", errMsg)
	}
	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if payload["terminal_id"] != "terminal-3" {
		t.Fatalf("expected created terminal id, got %#v", payload)
	}
	if manager.createName != "ops shell" {
		t.Fatalf("expected create routed to terminal manager, got %#v", manager)
	}

	fileStatus, _, fileErr := routeRuntimeAPIRequest(fileapi.NewManager(), manager, apiRequest{
		ID:     "req_2",
		Method: "POST",
		Path:   "/files/stat",
		Body:   json.RawMessage(`{"path":"/definitely-missing-termx-file"}`),
	})
	if fileStatus == http.StatusNotFound && fileErr == "unknown file route: POST /files/stat" {
		t.Fatalf("file routes must still be routed through file manager")
	}
}

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

func TestAnswerOfferMachineScopedPolicyAllowsAnyTerminalChannel(t *testing.T) {
	sessionCtx, cancelSession := context.WithCancel(context.Background())
	defer cancelSession()
	offerPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection returned error: %v", err)
	}
	defer offerPC.Close()

	dc, err := offerPC.CreateDataChannel("terminal:term-2", nil)
	if err != nil {
		t.Fatalf("CreateDataChannel returned error: %v", err)
	}
	open := make(chan struct{})
	dc.OnOpen(func() {
		select {
		case <-open:
		default:
			close(open)
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
		SessionID: "machine-scoped-channel-policy-session",
		DeviceID:  "machine-1",
		SDP:       offerPC.LocalDescription().SDP,
	}, nil, nil, fileapi.NewManager(), AnswerOptions{
		ChannelPolicy: ChannelPolicy{
			AllowTerminal: true,
			AllowAPI:      true,
		},
		SessionContext: sessionCtx,
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
	case <-open:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for machine-scoped terminal data channel to open")
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

type terminalAPIRouterStub struct {
	createName string
}

func (s *terminalAPIRouterStub) RouteTerminalManagementRequest(_ context.Context, req TerminalManagementRequest) (int32, []byte, string) {
	switch req.Method {
	case "list":
		return http.StatusOK, []byte(`{"terminals":[{"terminal_id":"terminal-1","machine_id":"machine-1","name":"zsh","state":"running"}]}`), ""
	case "create":
		var body struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(req.Body, &body)
		s.createName = body.Name
		return http.StatusOK, []byte(`{"terminal_id":"terminal-3"}`), ""
	default:
		return http.StatusNotFound, nil, "unknown terminal management route"
	}
}
