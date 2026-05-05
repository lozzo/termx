package rtc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lozzow/termx/termx-remote/bridge"
	"github.com/lozzow/termx/termx-remote/fileapi"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
	"github.com/pion/webrtc/v4"
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
	Events             EventRouter
	SessionContext     context.Context
	OnSessionClose     func()
}

type ChannelPolicy struct {
	TerminalID              string
	AllowTerminal           bool
	AllowAPI                bool
	AllowFileManager        bool
	AllowTerminalManagement bool
	AllowEvents             bool
	AllowRelayTransfer      bool
}

type TerminalManagementRouter interface {
	RouteTerminalManagementRequest(ctx context.Context, req TerminalManagementRequest) (int32, []byte, string)
}

type EventRouter interface {
	SubscribeRemoteEvents(ctx context.Context, filters EventFilters) (<-chan []byte, func(), error)
}

type TerminalManagementRequest struct {
	Method string
	Path   string
	Body   json.RawMessage
}

type EventFilters struct {
	TerminalID string
	SessionID  string
	Types      []int
}

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
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateClosed, webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateDisconnected:
			cancel()
			_ = pc.Close()
		}
	})
	go func() {
		<-sessionCtx.Done()
		_ = pc.Close()
		if opts.OnSessionClose != nil {
			opts.OnSessionClose()
		}
	}()

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		label := dc.Label()
		switch {
		case bridge.IsTerminalChannelLabel(label):
			if !opts.ChannelPolicy.allowTerminal(label) {
				dc.OnOpen(func() {
					_ = dc.Close()
				})
				return
			}
			dc.OnOpen(func() {
				if sink == nil {
					_ = dc.Close()
					return
				}
				transport := bridge.NewDataChannelTransport(dc)
				go func() {
					defer transport.Close()
					_ = sink.ServeRemoteTransport(sessionCtx, transport, "webrtc:"+dc.Label())
				}()
			})
		case label == "api":
			var apiFiles *fileapi.Manager
			if opts.ChannelPolicy.allowFileManager() {
				apiFiles = fileManager
			}
			var apiTerminalManagement TerminalManagementRouter
			if opts.ChannelPolicy.allowTerminalManagement() {
				apiTerminalManagement = opts.TerminalManagement
			}
			if !opts.ChannelPolicy.allowAPI() && apiFiles == nil && apiTerminalManagement == nil {
				dc.OnOpen(func() {
					_ = dc.Close()
				})
				return
			}
			handleAPIChannel(sessionCtx, dc, apiFiles, apiTerminalManagement, func(ctx context.Context) context.Context {
				ctx = withRelayTransferAllowed(ctx, opts.ChannelPolicy.allowRelayTransfer())
				return withRelayConnection(ctx, IsRelayConnection(pc))
			})
		case strings.HasPrefix(label, "file:"):
			if !opts.ChannelPolicy.allowFileManager() {
				dc.OnOpen(func() {
					_ = dc.Close()
				})
				return
			}
			fileManager.HandleFileChannelWithOpenGuard(dc, strings.TrimPrefix(label, "file:"), func() bool {
				return !IsRelayConnection(pc) || opts.ChannelPolicy.allowRelayTransfer()
			})
		case label == "events":
			if !opts.ChannelPolicy.allowEvents() {
				dc.OnOpen(func() {
					_ = dc.Close()
				})
				return
			}
			if opts.Events == nil {
				dc.OnOpen(func() {
					_ = dc.SendText(`{"type":"runtime_ready","payload":{"channel":"events"}}`)
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
	for _, candidate := range offer.ICECandidates {
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
	waitForLocalCandidates := newICEGatheringWaiter(pc)
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

func (p ChannelPolicy) allowTerminal(label string) bool {
	if !p.active() {
		return true
	}
	if !p.AllowTerminal {
		return false
	}
	terminalID := strings.TrimSpace(strings.TrimPrefix(label, "terminal:"))
	if terminalID == "" {
		return false
	}
	policyTerminalID := strings.TrimSpace(p.TerminalID)
	return policyTerminalID == "" || terminalID == policyTerminalID
}

func (p ChannelPolicy) allowFileManager() bool {
	if !p.active() {
		return true
	}
	return p.AllowFileManager
}

func (p ChannelPolicy) allowAPI() bool {
	if !p.active() {
		return true
	}
	return p.AllowAPI || p.AllowFileManager || p.AllowTerminalManagement
}

func (p ChannelPolicy) allowEvents() bool {
	if !p.active() {
		return true
	}
	return p.AllowEvents
}

func (p ChannelPolicy) allowTerminalManagement() bool {
	if !p.active() {
		return true
	}
	return p.AllowTerminalManagement
}

func (p ChannelPolicy) allowRelayTransfer() bool {
	if !p.active() {
		return true
	}
	return p.AllowRelayTransfer
}

func (p ChannelPolicy) active() bool {
	return strings.TrimSpace(p.TerminalID) != "" || p.AllowTerminal || p.AllowAPI || p.AllowFileManager || p.AllowTerminalManagement || p.AllowEvents || p.AllowRelayTransfer
}

type apiRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body"`
}

type apiResponse struct {
	ID     string      `json:"id"`
	Status int         `json:"status"`
	Body   interface{} `json:"body"`
}

const (
	apiChunkMagic      = 0xC0
	apiChunkFirst      = 0x01
	apiChunkLast       = 0x02
	apiChunkMaxPayload = 200 * 1024
)

func handleAPIChannel(ctx context.Context, dc *webrtc.DataChannel, manager *fileapi.Manager, terminalManagement TerminalManagementRouter, contextHook ...func(context.Context) context.Context) {
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		var req apiRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			return
		}
		go func() {
			reqCtx := ctx
			if len(contextHook) > 0 && contextHook[0] != nil {
				reqCtx = contextHook[0](reqCtx)
			}
			statusCode, respBody, errMsg := routeRuntimeAPIRequestWithContext(reqCtx, manager, terminalManagement, req)
			var body interface{}
			if errMsg != "" {
				body = map[string]string{"error": errMsg}
			} else if len(respBody) > 0 {
				if json.Unmarshal(respBody, &body) != nil {
					body = string(respBody)
				}
			}
			payload, _ := json.Marshal(apiResponse{
				ID:     req.ID,
				Status: int(statusCode),
				Body:   body,
			})
			sendAPIResponse(dc, req.ID, payload)
		}()
	})
}

func routeRuntimeAPIRequest(manager *fileapi.Manager, terminalManagement TerminalManagementRouter, req apiRequest) (int32, []byte, string) {
	return routeRuntimeAPIRequestWithContext(context.Background(), manager, terminalManagement, req)
}

func routeRuntimeAPIRequestWithContext(ctx context.Context, manager *fileapi.Manager, terminalManagement TerminalManagementRouter, req apiRequest) (int32, []byte, string) {
	if strings.HasPrefix(req.Path, "/files/") {
		if manager == nil {
			return http.StatusForbidden, nil, "file api is not allowed by connection policy"
		}
		if relayConnectionFromContext(ctx) && !relayTransferAllowedFromContext(ctx) {
			return http.StatusForbidden, nil, "file transfer is not allowed over relay connection"
		}
		return manager.RouteRequest(req.Method, req.Path, req.Body)
	}
	switch req.Path {
	case "ping", "status", "/status":
		return jsonResponseBody(map[string]any{"ok": true})
	case "list", "create", "set_metadata", "remove":
		if terminalManagement == nil {
			return http.StatusForbidden, nil, "terminal management is not allowed by connection policy"
		}
		return terminalManagement.RouteTerminalManagementRequest(ctx, TerminalManagementRequest{
			Method: req.Method,
			Path:   req.Path,
			Body:   req.Body,
		})
	default:
		return http.StatusNotFound, nil, fmt.Sprintf("unknown api route: %s %s", req.Method, req.Path)
	}
}

func serveEventChannel(ctx context.Context, dc *webrtc.DataChannel, router EventRouter) {
	type subscribeRequest struct {
		Type       string `json:"type"`
		TerminalID string `json:"terminal_id"`
		SessionID  string `json:"session_id"`
		Types      []int  `json:"types"`
	}
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
		var req subscribeRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			_ = dc.SendText(`{"type":"error","payload":{"message":"invalid event subscription"}}`)
			return
		}
		if strings.TrimSpace(req.Type) != "subscribe" {
			_ = dc.SendText(`{"type":"error","payload":{"message":"unknown event request"}}`)
			return
		}
		if cancel != nil {
			cancel()
			cancel = nil
		}
		subCtx, subCancel := context.WithCancel(ctx)
		events, unsubscribe, err := router.SubscribeRemoteEvents(subCtx, EventFilters{
			TerminalID: strings.TrimSpace(req.TerminalID),
			SessionID:  strings.TrimSpace(req.SessionID),
			Types:      append([]int(nil), req.Types...),
		})
		if err != nil {
			subCancel()
			_ = dc.SendText(`{"type":"error","payload":{"message":"event subscription failed"}}`)
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
					if err := dc.SendText(string(payload)); err != nil {
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

func jsonResponseBody(value any) (int32, []byte, string) {
	body, err := json.Marshal(value)
	if err != nil {
		return http.StatusInternalServerError, nil, err.Error()
	}
	return http.StatusOK, body, ""
}

func sendAPIResponse(dc *webrtc.DataChannel, id string, data []byte) {
	idBytes := []byte(id)
	headerSize := 3 + len(idBytes)
	chunkDataSize := apiChunkMaxPayload - headerSize
	offset := 0
	first := true

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
		_ = dc.Send(buf)

		offset = end
		first = false
	}
}

func newICEGatheringWaiter(pc *webrtc.PeerConnection) func() {
	return newICEGatheringWaiterEvents(
		pc.OnICECandidate,
		webrtc.GatheringCompletePromise(pc),
		500*time.Millisecond,
		5*time.Second,
	)
}

func newICEGatheringWaiterEvents(
	onICECandidate func(func(*webrtc.ICECandidate)),
	gatherComplete <-chan struct{},
	earlyStopDelay time.Duration,
	hardTimeout time.Duration,
) func() {
	earlyStop := make(chan struct{}, 1)
	var hasCandidate int32

	onICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		if c.Typ != webrtc.ICECandidateTypeHost && c.Typ != webrtc.ICECandidateTypeSrflx {
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
