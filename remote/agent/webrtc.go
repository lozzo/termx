package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lozzow/termx/transport"
	rtctransport "github.com/lozzow/termx/transport/webrtc"
	pion "github.com/pion/webrtc/v4"
)

const termxDataChannelLabel = "termx"

type LocalDialer func(context.Context) (transport.Transport, error)

type LocalOfferRequest struct {
	SDP        string   `json:"sdp"`
	Candidates []string `json:"candidates,omitempty"`
}

type LocalOfferResponse struct {
	SDP        string   `json:"sdp"`
	Candidates []string `json:"candidates,omitempty"`
}

type WebRTCHandler struct {
	dialLocal LocalDialer

	mu    sync.Mutex
	peers map[string]*peerSession
}

type peerSession struct {
	pc     *pion.PeerConnection
	cancel context.CancelFunc
	once   sync.Once
}

func NewWebRTCHandler(dialLocal LocalDialer) *WebRTCHandler {
	return &WebRTCHandler{
		dialLocal: dialLocal,
		peers:     make(map[string]*peerSession),
	}
}

func (h *WebRTCHandler) Close() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	peers := make([]*peerSession, 0, len(h.peers))
	for id, peer := range h.peers {
		delete(h.peers, id)
		peers = append(peers, peer)
	}
	h.mu.Unlock()
	for _, peer := range peers {
		peer.close()
	}
	return nil
}

func (h *WebRTCHandler) HandleLocalOffer(_ context.Context, req LocalOfferRequest) (*LocalOfferResponse, error) {
	if h == nil || h.dialLocal == nil {
		return nil, fmt.Errorf("local dialer is not configured")
	}
	if req.SDP == "" {
		return nil, fmt.Errorf("sdp is required")
	}

	pc, err := pion.NewPeerConnection(pion.Configuration{})
	if err != nil {
		return nil, fmt.Errorf("create peer connection: %w", err)
	}

	sessionID := fmt.Sprintf("local-%d", time.Now().UnixNano())
	peerCtx, cancel := context.WithCancel(context.Background())
	peer := &peerSession{pc: pc, cancel: cancel}
	h.mu.Lock()
	h.peers[sessionID] = peer
	h.mu.Unlock()

	pc.OnConnectionStateChange(func(state pion.PeerConnectionState) {
		switch state {
		case pion.PeerConnectionStateFailed, pion.PeerConnectionStateClosed:
			h.cleanupPeer(sessionID)
		}
	})
	pc.OnDataChannel(func(dc *pion.DataChannel) {
		if dc.Label() != termxDataChannelLabel {
			return
		}
		dc.OnOpen(func() {
			localTransport, err := h.dialLocal(peerCtx)
			if err != nil {
				h.cleanupPeer(sessionID)
				return
			}
			remoteTransport := rtctransport.NewTransport(rtctransport.NewPionChannel(dc))
			go func() {
				_ = Proxy(peerCtx, localTransport, remoteTransport)
				h.cleanupPeer(sessionID)
			}()
		})
	})

	if err := pc.SetRemoteDescription(pion.SessionDescription{
		Type: pion.SDPTypeOffer,
		SDP:  req.SDP,
	}); err != nil {
		h.cleanupPeer(sessionID)
		return nil, fmt.Errorf("set remote description: %w", err)
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		h.cleanupPeer(sessionID)
		return nil, fmt.Errorf("create answer: %w", err)
	}
	gatherComplete := pion.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		h.cleanupPeer(sessionID)
		return nil, fmt.Errorf("set local description: %w", err)
	}
	select {
	case <-gatherComplete:
	case <-time.After(5 * time.Second):
	}

	localDesc := pc.LocalDescription()
	if localDesc == nil {
		h.cleanupPeer(sessionID)
		return nil, fmt.Errorf("missing local description")
	}
	return &LocalOfferResponse{
		SDP:        localDesc.SDP,
		Candidates: nil,
	}, nil
}

func (h *WebRTCHandler) cleanupPeer(sessionID string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	peer, ok := h.peers[sessionID]
	if ok {
		delete(h.peers, sessionID)
	}
	h.mu.Unlock()
	if ok {
		peer.close()
	}
}

func (p *peerSession) close() {
	if p == nil {
		return
	}
	p.once.Do(func() {
		if p.cancel != nil {
			p.cancel()
		}
		if p.pc != nil && p.pc.ConnectionState() != pion.PeerConnectionStateClosed {
			_ = p.pc.Close()
		}
	})
}
