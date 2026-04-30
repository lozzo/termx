package rtc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-core/internal/remote/bridge"
	"github.com/lozzow/termx/termx-core/internal/remote/fileapi"
	hubv1 "github.com/lozzow/termx/termx-core/remote/hubv1"
	"github.com/pion/webrtc/v4"
)

func AnswerOffer(
	ctx context.Context,
	offer hubv1.SignalingOffer,
	iceServers []hubv1.RTCIceServerConfig,
	sink bridge.TransportSink,
	fileManager *fileapi.Manager,
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

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return hubv1.SignalingAnswer{}, fmt.Errorf("create peer connection: %w", err)
	}

	sessionCtx, cancel := context.WithCancel(ctx)
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
	}()

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		label := dc.Label()
		switch {
		case bridge.IsTerminalChannelLabel(label):
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
			handleAPIChannel(dc, fileManager)
		case strings.HasPrefix(label, "file:"):
			fileManager.HandleFileChannel(dc, strings.TrimPrefix(label, "file:"))
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
	if err := pc.SetLocalDescription(answer); err != nil {
		return hubv1.SignalingAnswer{}, fmt.Errorf("set local description: %w", err)
	}
	if err := waitForICEGathering(pc, 8*time.Second); err != nil {
		return hubv1.SignalingAnswer{}, fmt.Errorf("wait for local candidates: %w", err)
	}
	local := pc.LocalDescription()
	if local == nil {
		return hubv1.SignalingAnswer{}, fmt.Errorf("missing local description")
	}

	return hubv1.SignalingAnswer{
		SessionID:     offer.SessionID,
		SDP:           local.SDP,
		ICECandidates: nil,
	}, nil
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

func handleAPIChannel(dc *webrtc.DataChannel, manager *fileapi.Manager) {
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		var req apiRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			return
		}
		go func() {
			statusCode, respBody, errMsg := manager.RouteRequest(req.Method, req.Path, req.Body)
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

func waitForICEGathering(pc *webrtc.PeerConnection, timeout time.Duration) error {
	if pc.ICEGatheringState() == webrtc.ICEGatheringStateComplete {
		return nil
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
		return nil
	case <-time.After(timeout):
		return nil
	}
}
