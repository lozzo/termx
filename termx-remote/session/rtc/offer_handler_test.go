package rtc

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core/transport"
	"github.com/lozzow/termx/termx-remote/fileapi"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
	"github.com/pion/webrtc/v4"
)

func TestRuntimeEventPayloadForClientMapsTerminalInventoryEvents(t *testing.T) {
	payload := runtimeEventPayloadForClient([]byte(`{"type":1,"terminal_id":"terminal-2","timestamp":"2026-05-08T10:00:00Z"}`))
	var got struct {
		Type    string `json:"type"`
		Payload struct {
			TerminalID   string `json:"terminalId"`
			EventType    string `json:"eventType"`
			ProtocolType int    `json:"protocolType"`
			Timestamp    string `json:"timestamp"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal mapped payload: %v; body=%s", err, string(payload))
	}
	if got.Type != "inventory_changed" ||
		got.Payload.TerminalID != "terminal-2" ||
		got.Payload.EventType != "terminal_created" ||
		got.Payload.ProtocolType != protocolEventTerminalCreated ||
		got.Payload.Timestamp != "2026-05-08T10:00:00Z" {
		t.Fatalf("unexpected mapped event payload: %+v", got)
	}
}

func TestRuntimeEventPayloadForClientLeavesStringEventsUntouched(t *testing.T) {
	raw := []byte(`{"type":"heartbeat","payload":{"ok":true}}`)
	if got := runtimeEventPayloadForClient(raw); string(got) != string(raw) {
		t.Fatalf("expected string event to pass through, got %s", string(got))
	}
}

func TestRuntimeAPIChannelRouterHandlesTerminalManagement(t *testing.T) {
	manager := &terminalAPIRouterStub{}
	statusCode, statusBody, statusErr := routeRuntimeAPIRequest(nil, nil, apiRequest{
		ID:     "req_status",
		Method: "GET",
		Path:   "/status",
		Body:   json.RawMessage(`{}`),
	})
	if statusErr != "" {
		t.Fatalf("expected status request to succeed, got %q", statusErr)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", statusCode)
	}
	var statusPayload map[string]any
	if err := json.Unmarshal(statusBody, &statusPayload); err != nil {
		t.Fatalf("unmarshal status body: %v", err)
	}
	if statusPayload["ok"] != true {
		t.Fatalf("expected status ok payload, got %#v", statusPayload)
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

	status, body, errMsg = routeRuntimeAPIRequest(fileapi.NewManager(), manager, apiRequest{
		ID:     "req_directory",
		Method: "get_directory",
		Path:   "get_directory",
		Body:   json.RawMessage(`{"terminal_id":"terminal-1"}`),
	})
	if errMsg != "" {
		t.Fatalf("expected terminal directory request to succeed, got %q", errMsg)
	}
	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	if string(body) != `{"path":"/srv/app"}` {
		t.Fatalf("expected terminal directory payload, got %s", string(body))
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

func TestRuntimeAPIChannelIgnoresManagementMutationContext(t *testing.T) {
	manager := &terminalAPIRouterStub{}
	ctx := context.Background()
	listStatus, listBody, listErr := routeRuntimeAPIRequestWithContext(ctx, fileapi.NewManager(), manager, apiRequest{
		ID:     "req_list",
		Method: "list",
		Path:   "list",
		Body:   json.RawMessage(`{}`),
	})
	if listErr != "" {
		t.Fatalf("expected terminal inventory request to succeed, got %q", listErr)
	}
	if listStatus != http.StatusOK {
		t.Fatalf("expected terminal inventory status 200, got %d", listStatus)
	}
	var listPayload map[string]any
	if err := json.Unmarshal(listBody, &listPayload); err != nil {
		t.Fatalf("unmarshal list body: %v", err)
	}
	if terminals, ok := listPayload["terminals"].([]any); !ok || len(terminals) != 1 {
		t.Fatalf("expected terminal list payload, got %#v", listPayload)
	}

	createStatus, _, createErr := routeRuntimeAPIRequestWithContext(ctx, fileapi.NewManager(), manager, apiRequest{
		ID:     "req_create",
		Method: "create",
		Path:   "create",
		Body:   json.RawMessage(`{"name":"allowed"}`),
	})
	if createErr != "" {
		t.Fatalf("expected terminal create to be routed, got %q", createErr)
	}
	if createStatus != http.StatusOK {
		t.Fatalf("expected terminal create status 200, got %d", createStatus)
	}
	if manager.createName != "allowed" {
		t.Fatalf("expected terminal create to reach manager, got %#v", manager)
	}
}

func TestAnswerOfferChannelPolicyDoesNotRejectTerminalChannel(t *testing.T) {
	offerPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection returned error: %v", err)
	}
	defer offerPC.Close()

	dc, err := offerPC.CreateDataChannel("terminal:other-terminal", nil)
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
		SessionID:  "channel-policy-session",
		MachineID:  "machine-1",
		TerminalID: "signed-terminal",
		SDP:        offerPC.LocalDescription().SDP,
	}, nil, transportSinkFunc(func(context.Context, transport.Transport, string) error {
		<-time.After(10 * time.Millisecond)
		return nil
	}), fileapi.NewManager(), AnswerOptions{
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
	case <-open:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for terminal data channel to open")
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
		MachineID: "machine-1",
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
		MachineID:  "machine-1",
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
		MachineID:  "machine-1",
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
		MachineID:  "machine-1",
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

func TestDisconnectedGraceMonitorAllowsRecovery(t *testing.T) {
	pc := &fakePeerConnectionState{state: webrtc.PeerConnectionStateDisconnected}
	canceled := make(chan struct{})
	monitor := newDisconnectedGraceMonitor(pc, 25*time.Millisecond, func() {
		close(canceled)
	})

	monitor.handle(webrtc.PeerConnectionStateDisconnected)
	time.Sleep(10 * time.Millisecond)
	pc.set(webrtc.PeerConnectionStateConnected)
	monitor.handle(webrtc.PeerConnectionStateConnected)

	select {
	case <-canceled:
		t.Fatal("connection was canceled after recovering before disconnected grace expired")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestDisconnectedGraceMonitorCancelsAfterGrace(t *testing.T) {
	pc := &fakePeerConnectionState{state: webrtc.PeerConnectionStateDisconnected}
	canceled := make(chan struct{})
	monitor := newDisconnectedGraceMonitor(pc, 10*time.Millisecond, func() {
		close(canceled)
	})

	monitor.handle(webrtc.PeerConnectionStateDisconnected)

	select {
	case <-canceled:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("connection was not canceled after disconnected grace expired")
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

type transportSinkFunc func(context.Context, transport.Transport, string) error

func (f transportSinkFunc) ServeRemoteTransport(ctx context.Context, t transport.Transport, remote string) error {
	return f(ctx, t, remote)
}

type fakePeerConnectionState struct {
	mu    sync.Mutex
	state webrtc.PeerConnectionState
}

func (f *fakePeerConnectionState) ConnectionState() webrtc.PeerConnectionState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *fakePeerConnectionState) set(state webrtc.PeerConnectionState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = state
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
	case "get_directory":
		return http.StatusOK, []byte(`{"path":"/srv/app"}`), ""
	default:
		return http.StatusNotFound, nil, "unknown terminal management route"
	}
}
