package rtc

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-remote/fileapi"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
	"github.com/lozzow/termx/termx-remote/protocol/runtimepb"
	"github.com/lozzow/termx/termx-shared/transport"
	"github.com/pion/webrtc/v4"
	"google.golang.org/protobuf/proto"
)

func TestRuntimeEventEnvelopeCarriesTerminalInventoryData(t *testing.T) {
	payload := mustMarshalRuntimeProto(t, &runtimepb.EventEnvelope{
		Type:         "terminal_created",
		ProtocolType: protocolEventTerminalCreated,
		TerminalId:   "terminal-2",
		Terminal: &runtimepb.TerminalInventoryItem{
			TerminalId: "terminal-2",
			Name:       "shell",
			State:      "running",
		},
	})
	var got runtimepb.EventEnvelope
	if err := proto.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal runtime event envelope: %v", err)
	}
	if got.GetType() != "terminal_created" ||
		got.GetTerminalId() != "terminal-2" ||
		got.GetProtocolType() != protocolEventTerminalCreated ||
		got.GetTerminal().GetName() != "shell" {
		t.Fatalf("unexpected runtime event envelope: %#v", &got)
	}
}

func TestRuntimeAPIChannelRouterHandlesTerminalManagement(t *testing.T) {
	manager := &terminalAPIRouterStub{}
	statusCode, statusBody, statusErr := routeRuntimeAPIRequest(nil, nil, &runtimepb.APIRequest{
		Id:     "req_status",
		Method: "GET",
		Path:   "/status",
		Body:   mustMarshalRuntimeProto(t, &runtimepb.Empty{}),
	})
	if statusErr != "" {
		t.Fatalf("expected status request to succeed, got %q", statusErr)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", statusCode)
	}
	var statusPayload runtimepb.StatusResponse
	if err := proto.Unmarshal(statusBody, &statusPayload); err != nil {
		t.Fatalf("unmarshal status body: %v", err)
	}
	if !statusPayload.GetOk() {
		t.Fatalf("expected status ok payload, got %#v", statusPayload)
	}

	listStatus, listBody, listErr := routeRuntimeAPIRequest(fileapi.NewManager(), manager, &runtimepb.APIRequest{
		Id:     "req_list",
		Method: "list",
		Path:   "list",
		Body:   mustMarshalRuntimeProto(t, &runtimepb.Empty{}),
	})
	if listErr != "" {
		t.Fatalf("expected terminal list request to succeed, got %q", listErr)
	}
	if listStatus != http.StatusOK {
		t.Fatalf("expected terminal list status 200, got %d", listStatus)
	}
	var listPayload runtimepb.TerminalListResponse
	if err := proto.Unmarshal(listBody, &listPayload); err != nil {
		t.Fatalf("unmarshal list body: %v", err)
	}
	if len(listPayload.GetTerminals()) != 1 {
		t.Fatalf("expected terminal list payload, got %#v", listPayload)
	}

	status, body, errMsg := routeRuntimeAPIRequest(fileapi.NewManager(), manager, &runtimepb.APIRequest{
		Id:     "req_1",
		Method: "create",
		Path:   "create",
		Body:   mustMarshalRuntimeProto(t, &runtimepb.TerminalCreateRequest{Command: []string{"/bin/zsh", "-l"}, Name: "ops shell"}),
	})
	if errMsg != "" {
		t.Fatalf("expected terminal management request to succeed, got %q", errMsg)
	}
	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	var payload runtimepb.TerminalInventoryItem
	if err := proto.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if payload.GetTerminalId() != "terminal-3" {
		t.Fatalf("expected created terminal id, got %#v", payload)
	}
	if manager.createName != "ops shell" {
		t.Fatalf("expected create routed to terminal manager, got %#v", manager)
	}

	status, body, errMsg = routeRuntimeAPIRequest(fileapi.NewManager(), manager, &runtimepb.APIRequest{
		Id:     "req_directory",
		Method: "get_directory",
		Path:   "get_directory",
		Body:   mustMarshalRuntimeProto(t, &runtimepb.TerminalDirectoryRequest{TerminalId: "terminal-1"}),
	})
	if errMsg != "" {
		t.Fatalf("expected terminal directory request to succeed, got %q", errMsg)
	}
	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	var directory runtimepb.TerminalDirectoryResponse
	if err := proto.Unmarshal(body, &directory); err != nil {
		t.Fatalf("unmarshal directory body: %v", err)
	}
	if directory.GetPath() != "/srv/app" {
		t.Fatalf("expected terminal directory payload, got %#v", &directory)
	}

	fileStatus, _, fileErr := routeRuntimeAPIRequest(fileapi.NewManager(), manager, &runtimepb.APIRequest{
		Id:     "req_2",
		Method: "POST",
		Path:   "/files/stat",
		Body:   mustMarshalRuntimeProto(t, &runtimepb.FilePathRequest{Path: "/definitely-missing-termx-file"}),
	})
	if fileStatus == http.StatusNotFound && fileErr == "unknown file route: POST /files/stat" {
		t.Fatalf("file routes must still be routed through file manager")
	}
}

func TestRuntimeAPIChannelIgnoresManagementMutationContext(t *testing.T) {
	manager := &terminalAPIRouterStub{}
	ctx := context.Background()
	listStatus, listBody, listErr := routeRuntimeAPIRequestWithContext(ctx, fileapi.NewManager(), manager, nil, &runtimepb.APIRequest{
		Id:     "req_list",
		Method: "list",
		Path:   "list",
		Body:   mustMarshalRuntimeProto(t, &runtimepb.Empty{}),
	})
	if listErr != "" {
		t.Fatalf("expected terminal inventory request to succeed, got %q", listErr)
	}
	if listStatus != http.StatusOK {
		t.Fatalf("expected terminal inventory status 200, got %d", listStatus)
	}
	var listPayload runtimepb.TerminalListResponse
	if err := proto.Unmarshal(listBody, &listPayload); err != nil {
		t.Fatalf("unmarshal list body: %v", err)
	}
	if len(listPayload.GetTerminals()) != 1 {
		t.Fatalf("expected terminal list payload, got %#v", listPayload)
	}

	createStatus, _, createErr := routeRuntimeAPIRequestWithContext(ctx, fileapi.NewManager(), manager, nil, &runtimepb.APIRequest{
		Id:     "req_create",
		Method: "create",
		Path:   "create",
		Body:   mustMarshalRuntimeProto(t, &runtimepb.TerminalCreateRequest{Name: "allowed"}),
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

func TestAnswerOfferTerminalChannelBuffersFrameSentOnClientOpen(t *testing.T) {
	sessionCtx, cancelSession := context.WithCancel(context.Background())
	defer cancelSession()
	offerPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection returned error: %v", err)
	}
	defer offerPC.Close()

	dc, err := offerPC.CreateDataChannel("terminal:term-early", nil)
	if err != nil {
		t.Fatalf("CreateDataChannel returned error: %v", err)
	}
	firstFrame := []byte("first-frame")
	clientOpen := make(chan struct{})
	sendErr := make(chan error, 1)
	dc.OnOpen(func() {
		select {
		case <-clientOpen:
		default:
			close(clientOpen)
		}
		sendErr <- dc.Send(firstFrame)
	})

	offer, err := offerPC.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer returned error: %v", err)
	}
	if err := offerPC.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription returned error: %v", err)
	}
	waitTestPeerICE(t, offerPC, 5*time.Second)

	received := make(chan []byte, 1)
	recvErr := make(chan error, 1)
	answer, err := AnswerOfferWithOptions(context.Background(), hubv1.SignalingOffer{
		SessionID: "early-terminal-frame-session",
		MachineID: "machine-1",
		SDP:       offerPC.LocalDescription().SDP,
	}, nil, transportSinkFunc(func(_ context.Context, t transport.Transport, remote string) error {
		if remote != "webrtc:terminal:term-early" {
			t.Close()
			return nil
		}
		frame, err := t.Recv()
		if err != nil {
			recvErr <- err
			return err
		}
		received <- frame
		return nil
	}), fileapi.NewManager(), AnswerOptions{
		ChannelPolicy: ChannelPolicy{
			AllowTerminal: true,
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
	case <-clientOpen:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for terminal data channel to open")
	}
	select {
	case err := <-sendErr:
		if err != nil {
			t.Fatalf("Send on client open returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for client open send result")
	}
	select {
	case frame := <-received:
		if string(frame) != string(firstFrame) {
			t.Fatalf("expected first frame %q, got %q", string(firstFrame), string(frame))
		}
	case err := <-recvErr:
		t.Fatalf("server transport Recv returned error: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for server to receive first terminal frame")
	}
}

func TestTerminalDataChannelTransportIsRegisteredBeforeOpen(t *testing.T) {
	source, err := os.ReadFile("offer_handler.go")
	if err != nil {
		t.Fatalf("read offer_handler.go: %v", err)
	}
	text := string(source)
	terminalCase := strings.Index(text, "case bridge.IsTerminalChannelLabel(label):")
	if terminalCase < 0 {
		t.Fatal("terminal data channel case not found")
	}
	nextCase := strings.Index(text[terminalCase+1:], "\n\t\tcase ")
	if nextCase < 0 {
		t.Fatal("next data channel case not found")
	}
	block := text[terminalCase : terminalCase+1+nextCase]
	transportIndex := strings.Index(block, "transport := bridge.NewDataChannelTransport(dc)")
	onOpenIndex := strings.Index(block, "dc.OnOpen(func()")
	if transportIndex < 0 {
		t.Fatal("terminal data channel must create DataChannelTransport")
	}
	serveIndex := strings.Index(block, "sink.ServeRemoteTransport")
	if serveIndex < 0 {
		t.Fatal("terminal data channel must serve the transport after open")
	}
	serveBlock := block[:serveIndex]
	onOpenIndex = strings.LastIndex(serveBlock, "dc.OnOpen(func()")
	if onOpenIndex < 0 {
		t.Fatal("terminal data channel must handle OnOpen before serving transport")
	}
	if transportIndex > onOpenIndex {
		t.Fatal("terminal DataChannelTransport must be created before OnOpen so first client frames are buffered")
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
		return http.StatusOK, marshalRuntimeProtoForStub(&runtimepb.TerminalListResponse{
			Terminals: []*runtimepb.TerminalInventoryItem{{TerminalId: "terminal-1", Name: "zsh", State: "running"}},
		}), ""
	case "create":
		var body runtimepb.TerminalCreateRequest
		_ = proto.Unmarshal(req.Body, &body)
		s.createName = body.GetName()
		return http.StatusOK, marshalRuntimeProtoForStub(&runtimepb.TerminalInventoryItem{TerminalId: "terminal-3"}), ""
	case "get_directory":
		return http.StatusOK, marshalRuntimeProtoForStub(&runtimepb.TerminalDirectoryResponse{Path: "/srv/app"}), ""
	default:
		return http.StatusNotFound, nil, "unknown terminal management route"
	}
}

func mustMarshalRuntimeProto(t *testing.T, msg proto.Message) []byte {
	t.Helper()
	data, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal runtime proto: %v", err)
	}
	return data
}

func marshalRuntimeProtoForStub(msg proto.Message) []byte {
	data, err := proto.Marshal(msg)
	if err != nil {
		panic(err)
	}
	return data
}
