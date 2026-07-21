package webrtc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/muxvia/muxvia/internal/protocol/directsignal"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/proto/remoteauthpb"
	"github.com/muxvia/muxvia/shared/remoteauth"
	ice "github.com/pion/ice/v4"
	pion "github.com/pion/webrtc/v4"
)

const directSignalingClockSkew = 5 * time.Second

// DirectServer 是 daemon embedded signaling 与共享 ICE-TCP mux 的生命周期 owner。
// signaling connection 只交换一次短期 Proto offer/answer；terminal capability 仍必须在建立后的 DataChannel 内由 Handler 验证。
type DirectServer struct {
	identity          remoteauth.Identity
	handler           DataChannelSessionHandler
	signalingListener net.Listener
	iceMux            ice.TCPMux
	peerConnections   PeerConnectionFactory
	now               func() time.Time

	mu        sync.Mutex
	consumed  map[string]time.Time
	conns     map[net.Conn]struct{}
	peerSlots chan struct{}
	closeOnce sync.Once
	closeErr  error
	wg        sync.WaitGroup
}

// NewDirectServer 使用 daemon 已绑定的 signaling 与 ICE-TCP listener 创建服务。
// ICE listener 被单个 Pion TCPMux 接管并在所有 peer 间共享；任一依赖缺失都在启动前失败，不创建 fallback listener。
func NewDirectServer(identity remoteauth.Identity, handler DataChannelSessionHandler, signalingListener, iceListener net.Listener, now func() time.Time) (*DirectServer, error) {
	if err := identity.Validate(); err != nil {
		return nil, fmt.Errorf("direct server identity: %w", err)
	}
	if handler == nil || signalingListener == nil || iceListener == nil {
		return nil, fmt.Errorf("direct server handler and listeners are required")
	}
	mux := pion.NewICETCPMux(nil, iceListener, 8)
	settings := pion.SettingEngine{}
	settings.SetNetworkTypes([]pion.NetworkType{pion.NetworkTypeTCP4, pion.NetworkTypeTCP6})
	settings.SetIncludeLoopbackCandidate(true)
	settings.SetICETCPMux(mux)
	// Direct/SSH 的底层是 TCP；对端 socket 已关闭后无需沿用 media 场景的 5s/25s ICE 恢复窗口。
	// 两秒 disconnected 窗口仍允许短暂调度停顿，同时阻止快速重连把已结束 ufrag 堆积到共享 mux。
	settings.SetICETimeouts(2*time.Second, 5*time.Second, 500*time.Millisecond)
	api := pion.NewAPI(pion.WithSettingEngine(settings))
	return &DirectServer{
		identity: identity, handler: handler, signalingListener: signalingListener, iceMux: mux,
		peerConnections: api.NewPeerConnection, now: now, consumed: make(map[string]time.Time), conns: make(map[net.Conn]struct{}),
		peerSlots: make(chan struct{}, 32),
	}, nil
}

// Serve 接受一问一答的 Direct signaling connection，直到 context 取消或 listener 关闭。
// 返回前会关闭 signaling listener、共享 TCPMux 和仍在读写的 signaling connection，并等待 handler 退出。
func (server *DirectServer) Serve(ctx context.Context) error {
	if server == nil || server.signalingListener == nil || server.iceMux == nil {
		return fmt.Errorf("direct server is not initialized")
	}
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = server.Close()
		case <-stop:
		}
	}()
	defer close(stop)
	for {
		connection, err := server.signalingListener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				server.wg.Wait()
				return ctx.Err()
			}
			return fmt.Errorf("accept direct signaling connection: %w", err)
		}
		server.track(connection, true)
		server.wg.Add(1)
		go func() {
			defer server.wg.Done()
			defer server.track(connection, false)
			defer connection.Close()
			server.serveConnection(ctx, connection)
		}()
	}
}

// Close 幂等关闭 daemon embedded signaling 与共享 ICE-TCP listener。
// 已建立 peer 会因 mux/ICE 关闭而结束；调用方不得在同一 server 上重新启动第二个 generation。
func (server *DirectServer) Close() error {
	if server == nil {
		return nil
	}
	server.closeOnce.Do(func() {
		if server.signalingListener != nil {
			server.closeErr = server.signalingListener.Close()
		}
		server.mu.Lock()
		connections := make([]net.Conn, 0, len(server.conns))
		for connection := range server.conns {
			connections = append(connections, connection)
		}
		server.mu.Unlock()
		for _, connection := range connections {
			_ = connection.Close()
		}
		if server.iceMux != nil {
			if err := server.iceMux.Close(); server.closeErr == nil {
				server.closeErr = err
			}
		}
	})
	return server.closeErr
}

func (server *DirectServer) serveConnection(ctx context.Context, connection net.Conn) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	} else {
		_ = connection.SetDeadline(time.Now().Add(remoteauth.DirectSignalingMaxTTL))
	}
	request := &remoteauthpb.DirectSignalingRequestV1{}
	if err := directsignal.ReadMessage(connection, request); err != nil {
		server.writeError(connection, remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_PROTOCOL, "invalid direct signaling request")
		return
	}
	if code := server.admit(request); code != remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_UNSPECIFIED {
		message := "direct signaling request rejected"
		if code == remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_EXPIRED {
			message = "direct signaling request expired"
		} else if code == remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_REPLAYED {
			message = "direct signaling request already consumed"
		} else if code == remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_IDENTITY_MISMATCH {
			message = "direct signaling endpoint identity mismatch"
		}
		server.writeError(connection, code, message)
		return
	}
	select {
	case server.peerSlots <- struct{}{}:
	case <-ctx.Done():
		server.writeError(connection, remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_INTERNAL, "direct signaling server is stopping")
		return
	}
	var releaseOnce sync.Once
	releasePeer := func() {
		releaseOnce.Do(func() { <-server.peerSlots })
	}
	answer, err := (Answerer{
		Handler: server.handler, PeerConnections: server.peerConnections, CloseOnDisconnected: true, OnPeerClosed: releasePeer,
	}).Answer(ctx, &cloudpb.SignalingOffer{
		SignalingSessionId: request.GetRequestId(), Sdp: request.GetOfferSdp(),
	}, nil)
	if err != nil {
		releasePeer()
		server.writeError(connection, remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_INTERNAL, "create direct signaling answer failed")
		return
	}
	now := server.currentTime()
	wireAnswer := &remoteauthpb.DirectSignalingAnswerV1{
		SchemaVersion: remoteauth.DirectSignalingSchemaVersion, RequestId: request.GetRequestId(),
		Identity: &remoteauthpb.EndpointDaemonIdentity{
			DeviceId: server.identity.DeviceID, DevicePublicKey: append([]byte(nil), server.identity.PublicKey...), DeviceFingerprint: server.identity.Fingerprint,
		},
		AnswerSdp: answer.GetSdp(), IssuedAtUnixNano: now.UnixNano(), ExpiresAtUnixNano: now.Add(remoteauth.DirectSignalingMaxTTL).UnixNano(),
	}
	for _, candidate := range answer.GetCandidates() {
		if candidate == nil {
			continue
		}
		wireAnswer.Candidates = append(wireAnswer.Candidates, &remoteauthpb.DirectIceCandidate{
			Candidate: candidate.GetCandidate(), SdpMid: candidate.GetSdpMid(), SdpMlineIndex: candidate.GetSdpMlineIndex(), UsernameFragment: candidate.GetUsernameFragment(),
		})
	}
	if err := remoteauth.SignDirectSignalingAnswer(server.identity, wireAnswer); err != nil {
		server.writeError(connection, remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_INTERNAL, "sign direct signaling answer failed")
		return
	}
	_ = directsignal.WriteMessage(connection, &remoteauthpb.DirectSignalingResponseV1{
		Payload: &remoteauthpb.DirectSignalingResponseV1_Answer{Answer: wireAnswer},
	})
}

func (server *DirectServer) admit(request *remoteauthpb.DirectSignalingRequestV1) remoteauthpb.DirectSignalingErrorCode {
	if request == nil || request.GetSchemaVersion() != remoteauth.DirectSignalingSchemaVersion || strings.TrimSpace(request.GetRequestId()) == "" ||
		strings.TrimSpace(request.GetOfferSdp()) == "" || request.GetIssuedAtUnixNano() <= 0 || request.GetExpiresAtUnixNano() <= 0 {
		return remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_PROTOCOL
	}
	if request.GetExpectedDeviceId() != server.identity.DeviceID || request.GetExpectedDeviceFingerprint() != server.identity.Fingerprint {
		return remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_IDENTITY_MISMATCH
	}
	now := server.currentTime()
	issuedAt := time.Unix(0, request.GetIssuedAtUnixNano()).UTC()
	expiresAt := time.Unix(0, request.GetExpiresAtUnixNano()).UTC()
	if issuedAt.After(now.Add(directSignalingClockSkew)) || !expiresAt.After(now) || !expiresAt.After(issuedAt) || expiresAt.Sub(issuedAt) > remoteauth.DirectSignalingMaxTTL {
		return remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_EXPIRED
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	for requestID, expiry := range server.consumed {
		if !expiry.After(now) {
			delete(server.consumed, requestID)
		}
	}
	if _, exists := server.consumed[request.GetRequestId()]; exists {
		return remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_REPLAYED
	}
	server.consumed[request.GetRequestId()] = expiresAt
	return remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_UNSPECIFIED
}

func (server *DirectServer) writeError(connection net.Conn, code remoteauthpb.DirectSignalingErrorCode, message string) {
	_ = directsignal.WriteMessage(connection, &remoteauthpb.DirectSignalingResponseV1{
		Payload: &remoteauthpb.DirectSignalingResponseV1_Error{Error: &remoteauthpb.DirectSignalingErrorV1{Code: code, Message: message}},
	})
}

func (server *DirectServer) currentTime() time.Time {
	if server.now != nil {
		return server.now().UTC()
	}
	return time.Now().UTC()
}

func (server *DirectServer) track(connection net.Conn, add bool) {
	server.mu.Lock()
	defer server.mu.Unlock()
	if add {
		server.conns[connection] = struct{}{}
	} else {
		delete(server.conns, connection)
	}
}
