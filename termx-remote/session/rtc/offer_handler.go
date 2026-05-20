package rtc

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lozzow/termx/termx-remote/bridge"
	"github.com/lozzow/termx/termx-remote/fileapi"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
	"github.com/lozzow/termx/termx-remote/protocol/runtimepb"
	"github.com/pion/webrtc/v4"
	"google.golang.org/protobuf/proto"
)

func AnswerOffer(
	ctx context.Context,
	offer hubv1.SignalingOffer,
	iceServers []hubv1.RTCIceServerConfig,
	sink bridge.TransportSink,
	fileManager *fileapi.Manager,
) (hubv1.SignalingAnswer, error) {
	return AnswerOfferWithOptions(ctx, offer, iceServers, sink, fileManager, AnswerOptions{})
}

type SettingEngineApplier interface {
	Apply(setting *webrtc.SettingEngine)
}

type AnswerOptions struct {
	SettingEngine      SettingEngineApplier
	ChannelPolicy      ChannelPolicy
	TerminalManagement TerminalManagementRouter
	Storage            StorageRouter
	Events             EventRouter
	SessionContext     context.Context
	OnSessionClose     func()
	DisconnectedGrace  time.Duration
}

type ChannelPolicy struct {
	TerminalID              string
	AllowTerminal           bool
	AllowAPI                bool
	AllowFileManager        bool
	AllowTerminalInventory  bool
	AllowTerminalManagement bool
	AllowEvents             bool
	AllowRelayTransfer      bool
}

type TerminalManagementRouter interface {
	RouteTerminalManagementRequest(ctx context.Context, req TerminalManagementRequest) (int32, []byte, string)
}

type StorageRouter interface {
	RouteStorageRequest(ctx context.Context, req StorageRequest) (int32, []byte, string)
}

type EventRouter interface {
	SubscribeRemoteEvents(ctx context.Context, filters EventFilters) (<-chan []byte, func(), error)
}

type TerminalManagementRequest struct {
	Method string
	Path   string
	Body   []byte
}

type StorageRequest struct {
	Method string
	Path   string
	Body   []byte
}

type EventFilters struct {
	TerminalID string
	Types      []int
}

const defaultDisconnectedGrace = 12 * time.Second

func AnswerOfferWithOptions(
	ctx context.Context,
	offer hubv1.SignalingOffer,
	iceServers []hubv1.RTCIceServerConfig,
	sink bridge.TransportSink,
	fileManager *fileapi.Manager,
	opts AnswerOptions,
) (hubv1.SignalingAnswer, error) {
	if fileManager == nil {
		fileManager = fileapi.NewManager()
	}
	config := webrtc.Configuration{
		ICEServers: make([]webrtc.ICEServer, 0, len(iceServers)),
	}
	for _, server := range iceServers {
		config.ICEServers = append(config.ICEServers, webrtc.ICEServer{
			URLs:       append([]string(nil), server.URLs...),
			Username:   server.Username,
			Credential: server.Credential,
		})
	}

	var pc *webrtc.PeerConnection
	var err error
	if opts.SettingEngine != nil {
		setting := webrtc.SettingEngine{}
		opts.SettingEngine.Apply(&setting)
		pc, err = webrtc.NewAPI(webrtc.WithSettingEngine(setting)).NewPeerConnection(config)
	} else {
		pc, err = webrtc.NewPeerConnection(config)
	}
	if err != nil {
		return hubv1.SignalingAnswer{}, fmt.Errorf("create peer connection: %w", err)
	}

	sessionParent := opts.SessionContext
	if sessionParent == nil {
		sessionParent = ctx
	}
	sessionCtx, cancel := context.WithCancel(sessionParent)
	sessionStarted := false
	defer func() {
		if !sessionStarted {
			cancel()
			_ = pc.Close()
		}
	}()
	disconnectMonitor := newDisconnectedGraceMonitor(pc, answerDisconnectedGrace(opts), cancel)
	pc.OnConnectionStateChange(disconnectMonitor.handle)
	go func() {
		<-sessionCtx.Done()
		disconnectMonitor.stop()
		_ = pc.Close()
		if opts.OnSessionClose != nil {
			opts.OnSessionClose()
		}
	}()

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		label := dc.Label()
		switch {
		case bridge.IsTerminalChannelLabel(label):
			if sink == nil {
				dc.OnOpen(func() {
					_ = dc.Close()
				})
				return
			}
			transport := bridge.NewDataChannelTransport(dc)
			dc.OnOpen(func() {
				go func() {
					defer transport.Close()
					_ = sink.ServeRemoteTransport(sessionCtx, transport, "webrtc:"+dc.Label())
				}()
			})
		case label == "api":
			handleAPIChannel(sessionCtx, dc, fileManager, opts.TerminalManagement, opts.Storage, func(ctx context.Context) context.Context {
				return withRelayConnection(ctx, IsRelayConnection(pc))
			})
		case strings.HasPrefix(label, "file:"):
			fileManager.HandleFileChannelWithOpenGuard(dc, strings.TrimPrefix(label, "file:"), func() bool {
				return true
			})
		case label == "events":
			if opts.Events == nil {
				dc.OnOpen(func() {
					_ = sendRuntimeEvent(dc, &runtimepb.EventEnvelope{Type: "runtime_ready"})
				})
				return
			}
			go serveEventChannel(sessionCtx, dc, opts.Events)
		default:
			dc.OnOpen(func() {
				_ = dc.Close()
			})
		}
	})

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offer.SDP,
	}); err != nil {
		return hubv1.SignalingAnswer{}, fmt.Errorf("set remote description: %w", err)
	}
	for _, candidate := range offer.Candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if err := pc.AddICECandidate(webrtc.ICECandidateInit{Candidate: candidate}); err != nil {
			return hubv1.SignalingAnswer{}, fmt.Errorf("add remote candidate: %w", err)
		}
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return hubv1.SignalingAnswer{}, fmt.Errorf("create answer: %w", err)
	}
	waitForLocalCandidates := newICEGatheringWaiterForServers(pc, config.ICEServers)
	if err := pc.SetLocalDescription(answer); err != nil {
		return hubv1.SignalingAnswer{}, fmt.Errorf("set local description: %w", err)
	}
	waitForLocalCandidates()
	local := pc.LocalDescription()
	if local == nil {
		return hubv1.SignalingAnswer{}, fmt.Errorf("missing local description")
	}

	sessionStarted = true
	return hubv1.SignalingAnswer{
		SessionID:     offer.SessionID,
		SDP:           local.SDP,
		ICECandidates: nil,
	}, nil
}

func answerDisconnectedGrace(opts AnswerOptions) time.Duration {
	if opts.DisconnectedGrace > 0 {
		return opts.DisconnectedGrace
	}
	return defaultDisconnectedGrace
}

type peerConnectionStateProvider interface {
	ConnectionState() webrtc.PeerConnectionState
}

type disconnectedGraceMonitor struct {
	pc     peerConnectionStateProvider
	grace  time.Duration
	cancel context.CancelFunc

	mu    sync.Mutex
	timer *time.Timer
}

func newDisconnectedGraceMonitor(
	pc peerConnectionStateProvider,
	grace time.Duration,
	cancel context.CancelFunc,
) *disconnectedGraceMonitor {
	return &disconnectedGraceMonitor{pc: pc, grace: grace, cancel: cancel}
}

func (m *disconnectedGraceMonitor) handle(state webrtc.PeerConnectionState) {
	switch state {
	case webrtc.PeerConnectionStateConnected:
		m.stop()
	case webrtc.PeerConnectionStateDisconnected:
		m.start()
	case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
		m.stop()
		m.cancel()
	}
}

func (m *disconnectedGraceMonitor) start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.timer != nil {
		return
	}
	m.timer = time.AfterFunc(m.grace, func() {
		m.mu.Lock()
		m.timer = nil
		m.mu.Unlock()
		if m.pc.ConnectionState() == webrtc.PeerConnectionStateDisconnected {
			m.cancel()
		}
	})
}

func (m *disconnectedGraceMonitor) stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.timer == nil {
		return
	}
	m.timer.Stop()
	m.timer = nil
}

const (
	apiChunkMagic      = 0xC0
	apiChunkFirst      = 0x01
	apiChunkLast       = 0x02
	apiChunkMaxPayload = 64 * 1024
	apiSendBufferHigh  = 128 * 1024
	apiSendBufferLow   = 32 * 1024
)

func handleAPIChannel(ctx context.Context, dc *webrtc.DataChannel, manager *fileapi.Manager, terminalManagement TerminalManagementRouter, storage StorageRouter, contextHook ...func(context.Context) context.Context) {
	drainCh := make(chan struct{}, 1)
	dc.SetBufferedAmountLowThreshold(apiSendBufferLow)
	dc.OnBufferedAmountLow(func() {
		select {
		case drainCh <- struct{}{}:
		default:
		}
	})
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		var req runtimepb.APIRequest
		if err := proto.Unmarshal(msg.Data, &req); err != nil {
			log.Printf("termx remote api invalid request bytes=%d err=%v", len(msg.Data), err)
			return
		}
		go func() {
			started := time.Now()
			reqCtx := ctx
			if len(contextHook) > 0 && contextHook[0] != nil {
				reqCtx = contextHook[0](reqCtx)
			}
			log.Printf("termx remote api request id=%s method=%s path=%s body_bytes=%d buffered=%d", req.GetId(), req.GetMethod(), req.GetPath(), len(req.GetBody()), dc.BufferedAmount())
			statusCode, respBody, errMsg := routeRuntimeAPIRequestWithContext(reqCtx, manager, terminalManagement, storage, &req)
			payload, err := proto.Marshal(&runtimepb.APIResponse{
				Id:     req.GetId(),
				Status: statusCode,
				Body:   respBody,
				Error:  errMsg,
			})
			if err != nil {
				log.Printf("termx remote api response encode failed id=%s err=%v", req.GetId(), err)
				return
			}
			log.Printf("termx remote api response id=%s method=%s path=%s status=%d body_bytes=%d payload_bytes=%d elapsed_ms=%d err=%q", req.GetId(), req.GetMethod(), req.GetPath(), statusCode, len(respBody), len(payload), time.Since(started).Milliseconds(), errMsg)
			sendAPIResponse(reqCtx, dc, req.GetId(), payload, drainCh)
		}()
	})
}

func routeRuntimeAPIRequest(manager *fileapi.Manager, terminalManagement TerminalManagementRouter, req *runtimepb.APIRequest) (int32, []byte, string) {
	return routeRuntimeAPIRequestWithContext(context.Background(), manager, terminalManagement, nil, req)
}

func routeRuntimeAPIRequestWithContext(ctx context.Context, manager *fileapi.Manager, terminalManagement TerminalManagementRouter, storage StorageRouter, req *runtimepb.APIRequest) (int32, []byte, string) {
	if req == nil {
		return http.StatusBadRequest, nil, "request is nil"
	}
	if strings.HasPrefix(req.GetPath(), "/files/") {
		if manager == nil {
			return http.StatusServiceUnavailable, nil, "file api is not available"
		}
		return manager.RouteRequest(req.GetMethod(), req.GetPath(), req.GetBody())
	}
	if strings.HasPrefix(req.GetPath(), "/storage/") {
		if storage == nil {
			return http.StatusServiceUnavailable, nil, "storage api is not available"
		}
		return storage.RouteStorageRequest(ctx, StorageRequest{
			Method: req.GetMethod(),
			Path:   req.GetPath(),
			Body:   req.GetBody(),
		})
	}
	switch req.GetPath() {
	case "status", "/status":
		return protoResponseBody(&runtimepb.StatusResponse{Ok: true})
	case "list":
		if terminalManagement == nil {
			return http.StatusServiceUnavailable, nil, "terminal inventory is not available"
		}
		return terminalManagement.RouteTerminalManagementRequest(ctx, TerminalManagementRequest{
			Method: req.GetMethod(),
			Path:   req.GetPath(),
			Body:   req.GetBody(),
		})
	case "create", "set_metadata", "restart", "remove", "get_directory":
		if terminalManagement == nil {
			return http.StatusServiceUnavailable, nil, "terminal management is not available"
		}
		return terminalManagement.RouteTerminalManagementRequest(ctx, TerminalManagementRequest{
			Method: req.GetMethod(),
			Path:   req.GetPath(),
			Body:   req.GetBody(),
		})
	default:
		return http.StatusNotFound, nil, fmt.Sprintf("unknown api route: %s %s", req.GetMethod(), req.GetPath())
	}
}

func serveEventChannel(ctx context.Context, dc *webrtc.DataChannel, router EventRouter) {
	closed := make(chan struct{})
	var cancel func()
	defer func() {
		if cancel != nil {
			cancel()
		}
	}()
	dc.OnClose(func() {
		if cancel != nil {
			cancel()
			cancel = nil
		}
		select {
		case <-closed:
		default:
			close(closed)
		}
	})
	go func() {
		<-ctx.Done()
		if cancel != nil {
			cancel()
			cancel = nil
		}
	}()
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		var req runtimepb.EventSubscribeRequest
		if err := proto.Unmarshal(msg.Data, &req); err != nil {
			_ = sendRuntimeEvent(dc, &runtimepb.EventEnvelope{Type: "error", Error: "invalid event subscription"})
			return
		}
		if strings.TrimSpace(req.GetType()) != "subscribe" {
			_ = sendRuntimeEvent(dc, &runtimepb.EventEnvelope{Type: "error", Error: "unknown event request"})
			return
		}
		if cancel != nil {
			cancel()
			cancel = nil
		}
		subCtx, subCancel := context.WithCancel(ctx)
		events, unsubscribe, err := router.SubscribeRemoteEvents(subCtx, EventFilters{
			TerminalID: strings.TrimSpace(req.GetTerminalId()),
			Types:      int32sToInts(req.GetTypes()),
		})
		if err != nil {
			subCancel()
			_ = sendRuntimeEvent(dc, &runtimepb.EventEnvelope{Type: "error", Error: "event subscription failed"})
			return
		}
		cancel = func() {
			subCancel()
			unsubscribe()
		}
		go func() {
			for {
				select {
				case <-subCtx.Done():
					return
				case payload, ok := <-events:
					if !ok {
						return
					}
					if err := dc.Send(payload); err != nil {
						subCancel()
						return
					}
				}
			}
		}()
	})
	select {
	case <-ctx.Done():
	case <-closed:
	}
}

const (
	protocolEventTerminalCreated         = 1
	protocolEventTerminalStateChanged    = 2
	protocolEventTerminalResized         = 3
	protocolEventTerminalRemoved         = 4
	protocolEventTerminalMetadataChanged = 10
)

var terminalInventoryProtocolEvents = map[int]string{
	protocolEventTerminalCreated:         "terminal_created",
	protocolEventTerminalStateChanged:    "terminal_state_changed",
	protocolEventTerminalResized:         "terminal_resized",
	protocolEventTerminalRemoved:         "terminal_removed",
	protocolEventTerminalMetadataChanged: "terminal_metadata_changed",
}

func protoResponseBody(value proto.Message) (int32, []byte, string) {
	body, err := proto.Marshal(value)
	if err != nil {
		return http.StatusInternalServerError, nil, err.Error()
	}
	return http.StatusOK, body, ""
}

func sendRuntimeEvent(dc *webrtc.DataChannel, event *runtimepb.EventEnvelope) error {
	payload, err := proto.Marshal(event)
	if err != nil {
		return err
	}
	return dc.Send(payload)
}

func int32sToInts(values []int32) []int {
	if len(values) == 0 {
		return nil
	}
	out := make([]int, 0, len(values))
	for _, value := range values {
		out = append(out, int(value))
	}
	return out
}

func sendAPIResponse(ctx context.Context, dc *webrtc.DataChannel, id string, data []byte, drainCh <-chan struct{}) {
	idBytes := []byte(id)
	headerSize := 3 + len(idBytes)
	chunkDataSize := apiChunkMaxPayload - headerSize
	offset := 0
	first := true
	chunks := 0
	started := time.Now()

	for offset < len(data) {
		end := offset + chunkDataSize
		if end > len(data) {
			end = len(data)
		}
		last := end >= len(data)

		var flags byte
		if first {
			flags |= apiChunkFirst
		}
		if last {
			flags |= apiChunkLast
		}

		buf := make([]byte, headerSize+(end-offset))
		buf[0] = apiChunkMagic
		buf[1] = flags
		buf[2] = byte(len(idBytes))
		copy(buf[3:3+len(idBytes)], idBytes)
		copy(buf[headerSize:], data[offset:end])
		if err := waitAPIWritable(ctx, dc, drainCh); err != nil {
			log.Printf("termx remote api send wait failed id=%s chunks=%d sent_bytes=%d total_bytes=%d buffered=%d err=%v", id, chunks, offset, len(data), dc.BufferedAmount(), err)
			return
		}
		if err := dc.Send(buf); err != nil {
			log.Printf("termx remote api send failed id=%s chunk=%d chunk_bytes=%d sent_bytes=%d total_bytes=%d buffered=%d err=%v", id, chunks+1, len(buf), offset, len(data), dc.BufferedAmount(), err)
			return
		}
		chunks += 1

		offset = end
		first = false
	}
	log.Printf("termx remote api send complete id=%s chunks=%d total_bytes=%d elapsed_ms=%d buffered=%d", id, chunks, len(data), time.Since(started).Milliseconds(), dc.BufferedAmount())
}

func waitAPIWritable(ctx context.Context, dc *webrtc.DataChannel, drainCh <-chan struct{}) error {
	for dc.BufferedAmount() >= apiSendBufferHigh {
		log.Printf("termx remote api waiting writable buffered=%d high=%d", dc.BufferedAmount(), apiSendBufferHigh)
		select {
		case <-drainCh:
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(30 * time.Second):
			return context.DeadlineExceeded
		}
	}
	return nil
}

func newICEGatheringWaiterForServers(pc *webrtc.PeerConnection, iceServers []webrtc.ICEServer) func() {
	return newICEGatheringWaiterEvents(
		pc.OnICECandidate,
		webrtc.GatheringCompletePromise(pc),
		500*time.Millisecond,
		5*time.Second,
		hasTURNServer(iceServers),
	)
}

func newICEGatheringWaiterEvents(
	onICECandidate func(func(*webrtc.ICECandidate)),
	gatherComplete <-chan struct{},
	earlyStopDelay time.Duration,
	hardTimeout time.Duration,
	requireRelayCandidate ...bool,
) func() {
	requireRelay := len(requireRelayCandidate) > 0 && requireRelayCandidate[0]
	return newICEGatheringWaiterEventsWithPolicy(onICECandidate, gatherComplete, earlyStopDelay, hardTimeout, requireRelay)
}

func newICEGatheringWaiterEventsWithPolicy(
	onICECandidate func(func(*webrtc.ICECandidate)),
	gatherComplete <-chan struct{},
	earlyStopDelay time.Duration,
	hardTimeout time.Duration,
	requireRelayCandidate bool,
) func() {
	earlyStop := make(chan struct{}, 1)
	var hasCandidate int32

	onICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		if !candidateTypeSatisfiesGatheringPolicy(c.Typ, requireRelayCandidate) {
			return
		}
		if atomic.CompareAndSwapInt32(&hasCandidate, 0, 1) {
			time.AfterFunc(earlyStopDelay, func() {
				select {
				case earlyStop <- struct{}{}:
				default:
				}
			})
		}
	})

	return func() {
		select {
		case <-gatherComplete:
		case <-earlyStop:
		case <-time.After(hardTimeout):
		}
	}
}

func candidateTypeSatisfiesGatheringPolicy(candidateType webrtc.ICECandidateType, requireRelayCandidate bool) bool {
	if requireRelayCandidate {
		return candidateType == webrtc.ICECandidateTypeRelay
	}
	return candidateType == webrtc.ICECandidateTypeHost ||
		candidateType == webrtc.ICECandidateTypeSrflx ||
		candidateType == webrtc.ICECandidateTypeRelay
}

func hasTURNServer(iceServers []webrtc.ICEServer) bool {
	for _, server := range iceServers {
		for _, rawURL := range server.URLs {
			url := strings.TrimSpace(strings.ToLower(rawURL))
			if strings.HasPrefix(url, "turn:") || strings.HasPrefix(url, "turns:") {
				return true
			}
		}
	}
	return false
}
