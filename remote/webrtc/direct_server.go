package webrtc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/anytty/anytty/internal/protocol/directsignal"
	"github.com/anytty/anytty/proto/remoteauthpb"
	"github.com/anytty/anytty/shared/remoteauth"
	ice "github.com/pion/ice/v4"
	pion "github.com/pion/webrtc/v4"
)

const (
	directSignalingClockSkew         = 5 * time.Second
	directSignalingFirstRequestLimit = 5 * time.Second
	directSignalingErrorWriteLimit   = 250 * time.Millisecond
	directSignalingPreAuthLimit      = 64
	directSignalingPreAuthPerIPLimit = 8
	directSignalingPeerLimit         = 32
)

type DirectServerOption func(*directServerOptions)

type directServerOptions struct {
	logger *slog.Logger
}

type directAnswerFunc func(context.Context, Answerer, *SignalingOffer) (*SignalingAnswer, error)

var errDirectConnectionClosePanic = errors.New("direct signaling connection close failed")

type directConnection struct {
	net.Conn
	closeOnce sync.Once
	closeErr  error
}

func (connection *directConnection) Close() error {
	connection.closeOnce.Do(func() {
		func() {
			defer func() {
				if recover() != nil {
					connection.closeErr = errDirectConnectionClosePanic
				}
			}()
			connection.closeErr = connection.Conn.Close()
		}()
	})
	return connection.closeErr
}

// WithPionLogger routes embedded Pion diagnostics through the daemon logger.
func WithPionLogger(logger *slog.Logger) DirectServerOption {
	return func(options *directServerOptions) {
		options.logger = logger
	}
}

// DirectServer 是 daemon embedded signaling 与共享 ICE-TCP mux 的生命周期 owner。
// signaling connection 只交换一次短期 Proto offer/answer；terminal capability 仍必须在建立后的 DataChannel 内由 Handler 验证。
type DirectServer struct {
	identity          remoteauth.Identity
	handler           DataChannelSessionHandler
	admission         DirectSignalingAdmission
	signalingListener net.Listener
	iceMux            ice.TCPMux
	peerConnections   PeerConnectionFactory
	now               func() time.Time
	firstRequestLimit time.Duration

	// Test hooks are package-private so production admission limits stay fixed.
	afterPreAuthAcquire       func()
	beforeConnectionWorkerRun func()
	answerForTest             directAnswerFunc

	mu           sync.Mutex
	consumed     map[string]time.Time
	conns        map[*directConnection]struct{}
	preAuthTotal int
	preAuthByIP  map[string]int
	peerSlots    chan struct{}
	closed       bool
	closeOnce    sync.Once
	closeErr     error
	wg           sync.WaitGroup
}

// DirectSignalingAdmission 只在公开信令分配 peer 前检查持久 grant 或一次性 pairing claim；它不能替代 DataChannel proof。
type DirectSignalingAdmission interface {
	GrantActive(grantID string, expiresAt time.Time) bool
	PairingClaimActive(digest, clientPublicKey []byte, expiresAt time.Time) bool
}

// NewDirectServer 使用 daemon 已绑定的 signaling 与 ICE-TCP listener 创建服务。
// ICE listener 被单个 Pion TCPMux 接管并在所有 peer 间共享；任一依赖缺失都在启动前失败，不创建 fallback listener。
func NewDirectServer(identity remoteauth.Identity, handler DataChannelSessionHandler, signalingListener, iceListener net.Listener, now func() time.Time, serverOptions ...DirectServerOption) (*DirectServer, error) {
	if err := identity.Validate(); err != nil {
		return nil, fmt.Errorf("direct server identity: %w", err)
	}
	if handler == nil || signalingListener == nil || iceListener == nil {
		return nil, fmt.Errorf("direct server handler and listeners are required")
	}
	admission, ok := handler.(DirectSignalingAdmission)
	if !ok {
		return nil, fmt.Errorf("direct server handler requires signaling admission")
	}
	options := directServerOptions{}
	for _, apply := range serverOptions {
		if apply != nil {
			apply(&options)
		}
	}
	loggerFactory := NewLoggerFactory(options.logger)
	mux := pion.NewICETCPMux(loggerFactory.NewLogger("ice-tcp-mux"), iceListener, 8)
	settings := pion.SettingEngine{}
	settings.LoggerFactory = loggerFactory
	settings.SetNetworkTypes([]pion.NetworkType{pion.NetworkTypeTCP4, pion.NetworkTypeTCP6})
	settings.SetIncludeLoopbackCandidate(true)
	settings.SetICETCPMux(mux)
	// Direct/SSH 的底层是 TCP；对端 socket 已关闭后无需沿用 media 场景的 5s/25s ICE 恢复窗口。
	// 两秒 disconnected 窗口仍允许短暂调度停顿，同时阻止快速重连把已结束 ufrag 堆积到共享 mux。
	settings.SetICETimeouts(2*time.Second, 5*time.Second, 500*time.Millisecond)
	api := pion.NewAPI(pion.WithSettingEngine(settings))
	return &DirectServer{
		identity: identity, handler: handler, admission: admission, signalingListener: signalingListener, iceMux: mux,
		peerConnections: api.NewPeerConnection, now: now, consumed: make(map[string]time.Time), conns: make(map[*directConnection]struct{}),
		firstRequestLimit: directSignalingFirstRequestLimit, preAuthByIP: make(map[string]int),
		peerSlots: make(chan struct{}, directSignalingPeerLimit),
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
		rawConnection, err := server.signalingListener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				server.wg.Wait()
				return ctx.Err()
			}
			return fmt.Errorf("accept direct signaling connection: %w", err)
		}
		connection := &directConnection{Conn: rawConnection}
		releasePreAuth, acquired := server.tryAcquirePreAuth(connection.RemoteAddr())
		if !acquired {
			server.writeError(ctx, connection, remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_OVERLOADED, "direct signaling server is overloaded")
			_ = connection.Close()
			continue
		}
		server.startConnectionWorker(ctx, connection, releasePreAuth)
	}
}

// Close 幂等关闭 daemon embedded signaling 与共享 ICE-TCP listener。
// 已建立 peer 会因 mux/ICE 关闭而结束；调用方不得在同一 server 上重新启动第二个 generation。
func (server *DirectServer) Close() error {
	if server == nil {
		return nil
	}
	server.closeOnce.Do(func() {
		server.mu.Lock()
		server.closed = true
		connections := make([]*directConnection, 0, len(server.conns))
		for connection := range server.conns {
			connections = append(connections, connection)
		}
		server.mu.Unlock()
		if server.signalingListener != nil {
			server.closeErr = server.signalingListener.Close()
		}
		for _, connection := range connections {
			if err := connection.Close(); server.closeErr == nil {
				server.closeErr = err
			}
		}
		if server.iceMux != nil {
			if err := server.iceMux.Close(); server.closeErr == nil {
				server.closeErr = err
			}
		}
	})
	return server.closeErr
}

func (server *DirectServer) startConnectionWorker(ctx context.Context, connection *directConnection, releasePreAuth func()) {
	tracked := false
	defer func() {
		if recover() == nil {
			return
		}
		releasePreAuth()
		func() {
			defer func() { _ = recover() }()
			server.writeError(ctx, connection, remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_INTERNAL, "direct signaling server internal failure")
		}()
		_ = connection.Close()
		if tracked {
			server.untrack(connection)
		}
	}()
	if server.afterPreAuthAcquire != nil {
		server.afterPreAuthAcquire()
	}
	if !server.track(connection) {
		releasePreAuth()
		server.writeError(ctx, connection, remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_OVERLOADED, "direct signaling server is overloaded")
		_ = connection.Close()
		return
	}
	tracked = true
	server.wg.Add(1)
	go server.runConnectionWorker(ctx, connection, releasePreAuth)
}

func (server *DirectServer) runConnectionWorker(ctx context.Context, connection *directConnection, releasePreAuth func()) {
	defer func() {
		panicked := recover() != nil
		releasePreAuth()
		if panicked {
			func() {
				defer func() { _ = recover() }()
				server.writeError(ctx, connection, remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_INTERNAL, "direct signaling server internal failure")
			}()
		}
		_ = connection.Close()
		server.untrack(connection)
		server.wg.Done()
	}()
	if server.beforeConnectionWorkerRun != nil {
		server.beforeConnectionWorkerRun()
	}
	server.serveConnection(ctx, connection, releasePreAuth)
}

func (server *DirectServer) serveConnection(ctx context.Context, connection net.Conn, releasePreAuth func()) {
	firstRequestLimit := server.firstRequestLimit
	if firstRequestLimit <= 0 {
		firstRequestLimit = directSignalingFirstRequestLimit
	}
	requestStarted := time.Now()
	_ = connection.SetReadDeadline(earlierDeadline(requestStarted.Add(firstRequestLimit), ctx))
	_ = connection.SetWriteDeadline(earlierDeadline(requestStarted.Add(remoteauth.DirectSignalingMaxTTL), ctx))
	request := &remoteauthpb.DirectSignalingRequestV2{}
	if err := directsignal.ReadMessage(connection, request); err != nil {
		server.writeError(ctx, connection, remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_PROTOCOL, "invalid direct signaling request")
		return
	}
	code := server.admit(request)
	releasePreAuth()
	if code != remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_UNSPECIFIED {
		message := "direct signaling request rejected"
		if code == remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_EXPIRED {
			message = "direct signaling request expired"
		} else if code == remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_REPLAYED {
			message = "direct signaling request already consumed"
		} else if code == remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_IDENTITY_MISMATCH {
			message = "direct signaling endpoint identity mismatch"
		} else if code == remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_AUTHORIZATION {
			message = "direct signaling authorization is unavailable"
		}
		server.writeError(ctx, connection, code, message)
		return
	}
	releasePeer, acquired := server.tryAcquirePeer()
	if !acquired {
		server.writeError(ctx, connection, remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_OVERLOADED, "direct signaling server is overloaded")
		return
	}
	peerHandedOff := false
	defer func() {
		if !peerHandedOff {
			releasePeer()
		}
	}()
	if ctx.Err() != nil {
		releasePeer()
		server.writeError(ctx, connection, remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_INTERNAL, "direct signaling server is stopping")
		return
	}
	answerer := Answerer{
		Handler: server.handler, PeerConnections: server.peerConnections, CloseOnDisconnected: true, OnPeerClosed: releasePeer,
	}
	offer := &SignalingOffer{
		SessionID: request.GetRequestId(), SDP: request.GetOfferSdp(),
	}
	var answer *SignalingAnswer
	var err error
	if server.answerForTest != nil {
		answer, err = server.answerForTest(ctx, answerer, offer)
	} else {
		answer, err = answerer.Answer(ctx, offer, nil)
	}
	if err != nil {
		releasePeer()
		server.writeError(ctx, connection, remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_INTERNAL, "create direct signaling answer failed")
		return
	}
	peerHandedOff = true
	now := server.currentTime()
	wireAnswer := &remoteauthpb.DirectSignalingAnswerV2{
		SchemaVersion: remoteauth.DirectSignalingSchemaVersion, RequestId: request.GetRequestId(),
		Identity: &remoteauthpb.EndpointDaemonIdentity{
			DeviceId: server.identity.DeviceID, DevicePublicKey: append([]byte(nil), server.identity.PublicKey...), DeviceFingerprint: server.identity.Fingerprint,
		},
		AnswerSdp: answer.SDP, IssuedAtUnixNano: now.UnixNano(), ExpiresAtUnixNano: now.Add(remoteauth.DirectSignalingMaxTTL).UnixNano(),
	}
	for _, candidate := range answer.Candidates {
		wireAnswer.Candidates = append(wireAnswer.Candidates, &remoteauthpb.DirectIceCandidate{
			Candidate: candidate.Candidate, SdpMid: candidate.SDPMid, SdpMlineIndex: candidate.SDPMLineIndex, UsernameFragment: candidate.UsernameFragment,
		})
	}
	if err := remoteauth.SignDirectSignalingAnswer(server.identity, wireAnswer); err != nil {
		server.writeError(ctx, connection, remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_INTERNAL, "sign direct signaling answer failed")
		return
	}
	_ = directsignal.WriteMessage(connection, &remoteauthpb.DirectSignalingResponseV2{
		Payload: &remoteauthpb.DirectSignalingResponseV2_Answer{Answer: wireAnswer},
	})
}

func (server *DirectServer) admit(request *remoteauthpb.DirectSignalingRequestV2) remoteauthpb.DirectSignalingErrorCode {
	if request == nil || request.GetSchemaVersion() != remoteauth.DirectSignalingSchemaVersion || strings.TrimSpace(request.GetRequestId()) == "" ||
		strings.TrimSpace(request.GetOfferSdp()) == "" || request.GetIssuedAtUnixNano() <= 0 || request.GetExpiresAtUnixNano() <= 0 {
		return remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_PROTOCOL
	}
	grantMode := strings.TrimSpace(request.GetGrantId()) != "" && request.GetGrantExpiresAtUnixNano() > 0 &&
		len(request.GetPairingClaimDigest()) == 0 && len(request.GetPairingClientPublicKey()) == 0 && request.GetPairingExpiresAtUnixNano() == 0
	pairingMode := strings.TrimSpace(request.GetGrantId()) == "" && request.GetGrantExpiresAtUnixNano() == 0 &&
		len(request.GetPairingClaimDigest()) > 0 && len(request.GetPairingClientPublicKey()) > 0 && request.GetPairingExpiresAtUnixNano() > 0
	if grantMode == pairingMode {
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
	authorized := false
	if grantMode {
		grantExpiresAt := time.Unix(0, request.GetGrantExpiresAtUnixNano()).UTC()
		authorized = grantExpiresAt.After(now) && server.admission != nil && server.admission.GrantActive(request.GetGrantId(), grantExpiresAt)
	} else {
		pairingExpiresAt := time.Unix(0, request.GetPairingExpiresAtUnixNano()).UTC()
		authorized = server.admission != nil && server.admission.PairingClaimActive(request.GetPairingClaimDigest(), request.GetPairingClientPublicKey(), pairingExpiresAt)
	}
	if !authorized {
		return remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_AUTHORIZATION
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

func (server *DirectServer) writeError(ctx context.Context, connection net.Conn, code remoteauthpb.DirectSignalingErrorCode, message string) {
	_ = connection.SetWriteDeadline(earlierDeadline(time.Now().Add(directSignalingErrorWriteLimit), ctx))
	_ = directsignal.WriteMessage(connection, &remoteauthpb.DirectSignalingResponseV2{
		Payload: &remoteauthpb.DirectSignalingResponseV2_Error{Error: &remoteauthpb.DirectSignalingErrorV2{Code: code, Message: message}},
	})
}

func earlierDeadline(deadline time.Time, ctx context.Context) time.Time {
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		return contextDeadline
	}
	return deadline
}

func (server *DirectServer) currentTime() time.Time {
	if server.now != nil {
		return server.now().UTC()
	}
	return time.Now().UTC()
}

func (server *DirectServer) tryAcquirePreAuth(remoteAddress net.Addr) (func(), bool) {
	sourceIP := normalizedDirectSourceIP(remoteAddress)
	server.mu.Lock()
	if server.closed || server.preAuthTotal >= directSignalingPreAuthLimit || server.preAuthByIP[sourceIP] >= directSignalingPreAuthPerIPLimit {
		server.mu.Unlock()
		return nil, false
	}
	server.preAuthTotal++
	server.preAuthByIP[sourceIP]++
	server.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			server.mu.Lock()
			server.preAuthTotal--
			if server.preAuthByIP[sourceIP] == 1 {
				delete(server.preAuthByIP, sourceIP)
			} else {
				server.preAuthByIP[sourceIP]--
			}
			server.mu.Unlock()
		})
	}, true
}

func normalizedDirectSourceIP(remoteAddress net.Addr) string {
	if tcpAddress, ok := remoteAddress.(*net.TCPAddr); ok {
		if address, valid := netip.AddrFromSlice(tcpAddress.IP); valid {
			return address.Unmap().WithZone("").String()
		}
	}
	if remoteAddress != nil {
		host, _, err := net.SplitHostPort(remoteAddress.String())
		if err == nil {
			if address, parseErr := netip.ParseAddr(host); parseErr == nil {
				return address.Unmap().WithZone("").String()
			}
		}
	}
	return "unknown"
}

func (server *DirectServer) tryAcquirePeer() (func(), bool) {
	select {
	case server.peerSlots <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() { <-server.peerSlots })
		}, true
	default:
		return nil, false
	}
}

func (server *DirectServer) track(connection *directConnection) bool {
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.closed {
		return false
	}
	server.conns[connection] = struct{}{}
	return true
}

func (server *DirectServer) untrack(connection *directConnection) {
	server.mu.Lock()
	delete(server.conns, connection)
	server.mu.Unlock()
}
