// Package direct 实现不依赖 TermX Cloud 的 daemon embedded signaling + ICE-TCP connector。
// Endpoint/Route 选择与 generation 属于 client/runtime；本包只执行当前 Direct attempt 的 signaling、DTLS auth、Hello 和资源清理。
package direct

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	peeradapter "github.com/lozzow/termx/client/adapter/peer"
	protocoladapter "github.com/lozzow/termx/client/adapter/protocol"
	"github.com/lozzow/termx/client/endpoint"
	"github.com/lozzow/termx/client/port"
	clientruntime "github.com/lozzow/termx/client/runtime"
	internalprotocol "github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/internal/protocol/directsignal"
	"github.com/lozzow/termx/proto/apipb"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/proto/remoteauthpb"
	"github.com/lozzow/termx/proto/wire"
	"github.com/lozzow/termx/shared/remoteauth"
	"github.com/lozzow/termx/shared/transport/datachannel"
)

const defaultClientName = "termx-go-direct"

// PeerFactory 创建只启用 ICE-TCP 的 WebRTC peer primitive。
// factory 不解析 Endpoint、不访问 credential，也不执行 signaling 或 remote auth。
type PeerFactory interface {
	// OpenDirectPeer 为当前 attempt 创建一个可靠有序 protocol DataChannel。
	OpenDirectPeer(context.Context) (port.ManagedPeer, error)
}

// SignalingClient 是 Direct connector 对 daemon embedded signaling 的单次 exchange 边界。
// 实现只能传输 generated Proto request/response，不能返回预授权 session 或修改 Endpoint pin。
type SignalingClient interface {
	// Exchange 在给定 locator 中建立一条 signaling TCP connection，并返回 daemon-signed answer。
	Exchange(context.Context, []string, *remoteauthpb.DirectSignalingRequestV1) (*remoteauthpb.DirectSignalingAnswerV1, error)
}

// Dialer 是 direct-webrtc-tcp Route 的 Go-owned connector。
// 成功结果已经完成 daemon-signed signaling、实际 DTLS-bound capability auth、protocol Hello 与 ReadyPeerSession 装配。
type Dialer struct {
	Peers         PeerFactory
	Signaling     SignalingClient
	Authorization peeradapter.Authorizer
	Random        io.Reader
	Now           func() time.Time
	ClientName    string
	Phase         func(clientruntime.EndpointPhase)
}

// Connect 只尝试 request 指定的 Direct Route；任何失败都会关闭 peer、DataChannel 和 protocol client。
// signaling locator 变化不改变 Endpoint identity，answer 必须由 pin 对应的 daemon DeviceIdentity 签名。
func (dialer *Dialer) Connect(ctx context.Context, request clientruntime.AttemptRequest) (clientruntime.ReadyPeerSession, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	route := request.Route()
	if route.Kind != endpoint.RouteDirectWebRTCTCP {
		return nil, fmt.Errorf("route %q kind %q is not direct WebRTC TCP", route.ID, route.Kind)
	}
	if dialer == nil || dialer.Peers == nil || dialer.Authorization == nil {
		return nil, fmt.Errorf("direct WebRTC connector dependencies are incomplete")
	}
	prepared, err := dialer.Authorization.Prepare(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("prepare direct endpoint authorization: %w", err)
	}
	if prepared == nil {
		return nil, fmt.Errorf("direct endpoint authorizer returned no transaction")
	}
	dialer.reportPhase(clientruntime.EndpointPhaseConnecting)
	peer, err := dialer.Peers.OpenDirectPeer(ctx)
	if err != nil {
		return nil, fmt.Errorf("create direct endpoint peer: %w", err)
	}
	if peer == nil || peer.Channel() == nil {
		if peer != nil {
			_ = peer.Close()
		}
		return nil, fmt.Errorf("direct endpoint peer has no protocol DataChannel")
	}
	connection := datachannel.New(peer.Channel())
	closeAttempt := func() {
		_ = connection.Close()
		_ = peer.Close()
	}
	offer, err := peer.CreateOffer(ctx)
	if err != nil {
		closeAttempt()
		return nil, fmt.Errorf("create direct endpoint offer: %w", err)
	}
	requestID, err := dialer.requestID()
	if err != nil {
		closeAttempt()
		return nil, err
	}
	now := dialer.currentTime()
	signalingRequest := &remoteauthpb.DirectSignalingRequestV1{
		SchemaVersion: remoteauth.DirectSignalingSchemaVersion, RequestId: requestID,
		ExpectedDeviceId: request.DaemonIdentity().DeviceID, ExpectedDeviceFingerprint: request.DaemonIdentity().DeviceFingerprint,
		OfferSdp: offer, IssuedAtUnixNano: now.UnixNano(), ExpiresAtUnixNano: now.Add(remoteauth.DirectSignalingMaxTTL).UnixNano(),
	}
	dialer.reportPhase(clientruntime.EndpointPhaseSignaling)
	signaling := dialer.Signaling
	if signaling == nil {
		signaling = TCPSignalingClient{}
	}
	answer, err := signaling.Exchange(ctx, route.SignalingAddresses, signalingRequest)
	if err != nil {
		closeAttempt()
		return nil, err
	}
	if err := remoteauth.VerifyDirectSignalingAnswer(answer, requestID, request.DaemonIdentity().DeviceID, request.DaemonIdentity().DeviceFingerprint, dialer.currentTime()); err != nil {
		closeAttempt()
		return nil, fmt.Errorf("verify direct signaling answer: %w", err)
	}
	candidates := make([]*cloudpb.IceCandidate, 0, len(answer.GetCandidates()))
	for _, candidate := range answer.GetCandidates() {
		if candidate == nil {
			continue
		}
		candidates = append(candidates, &cloudpb.IceCandidate{
			Candidate: candidate.GetCandidate(), SdpMid: candidate.GetSdpMid(), SdpMlineIndex: candidate.GetSdpMlineIndex(), UsernameFragment: candidate.GetUsernameFragment(),
		})
	}
	if err := peer.ApplyAnswer(ctx, answer.GetAnswerSdp(), candidates); err != nil {
		closeAttempt()
		return nil, fmt.Errorf("apply direct endpoint answer: %w", err)
	}
	if err := peer.WaitReady(ctx); err != nil {
		closeAttempt()
		return nil, fmt.Errorf("wait direct endpoint DataChannel: %w", err)
	}
	if peer.ObservedPath() != endpoint.PathDirect {
		closeAttempt()
		return nil, fmt.Errorf("direct endpoint established unexpected path %q", peer.ObservedPath())
	}
	fingerprint, err := peer.RemoteCertificateFingerprint()
	if err != nil {
		closeAttempt()
		return nil, fmt.Errorf("read direct endpoint DTLS certificate: %w", err)
	}
	dialer.reportPhase(clientruntime.EndpointPhaseAuthorizing)
	if _, err := prepared.Authenticate(ctx, connection, fingerprint); err != nil {
		closeAttempt()
		return nil, fmt.Errorf("authenticate direct endpoint DataChannel: %w", err)
	}
	protocolClient := internalprotocol.NewClient(connection)
	clientName := strings.TrimSpace(dialer.ClientName)
	if clientName == "" {
		clientName = defaultClientName
	}
	if err := protocolClient.Hello(ctx, internalprotocol.Hello{Version: wire.Version, Client: clientName}); err != nil {
		_ = protocolClient.Close()
		_ = peer.Close()
		return nil, fmt.Errorf("direct endpoint protocol Hello: %w", err)
	}
	application, err := protocoladapter.NewApplicationClientWithObservedPath(protocolClient, request.Stamp(), string(endpoint.PathDirect))
	if err != nil {
		_ = protocolClient.Close()
		_ = peer.Close()
		return nil, err
	}
	if err := application.MarkReady(clientruntime.ReadyPeerSessionEvidence{
		Identity: request.DaemonIdentity(), IdentityVerified: true, AuthorizationVerified: true, ProtocolVersion: wire.Version,
	}); err != nil {
		_ = application.Close()
		_ = peer.Close()
		return nil, err
	}
	session := newSession(application, peer)
	dialer.reportPhase(clientruntime.EndpointPhaseReady)
	return session, nil
}

func (dialer *Dialer) requestID() (string, error) {
	randomSource := dialer.Random
	if randomSource == nil {
		randomSource = rand.Reader
	}
	payload := make([]byte, 24)
	if _, err := io.ReadFull(randomSource, payload); err != nil {
		return "", fmt.Errorf("generate direct signaling request id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func (dialer *Dialer) currentTime() time.Time {
	if dialer != nil && dialer.Now != nil {
		return dialer.Now().UTC()
	}
	return time.Now().UTC()
}

func (dialer *Dialer) reportPhase(phase clientruntime.EndpointPhase) {
	if dialer != nil && dialer.Phase != nil {
		dialer.Phase(phase)
	}
}

// TCPSignalingClient 使用首个可建立 TCP connection 的 locator 完成一次 Proto exchange。
// 地址选择只发生在写入 request 前；一旦请求可能已被 daemon 消费，就不重放到其他地址。
type TCPSignalingClient struct {
	Dialer net.Dialer
}

// Exchange 建连后写入一个 request 并读取一个 response；context 取消会立即打断当前 socket。
func (client TCPSignalingClient) Exchange(ctx context.Context, addresses []string, request *remoteauthpb.DirectSignalingRequestV1) (*remoteauthpb.DirectSignalingAnswerV1, error) {
	if request == nil || len(addresses) == 0 {
		return nil, fmt.Errorf("direct signaling request and addresses are required")
	}
	var connection net.Conn
	var dialErrors []error
	for _, address := range addresses {
		address = strings.TrimSpace(address)
		if address == "" {
			continue
		}
		candidate, err := client.Dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			dialErrors = append(dialErrors, fmt.Errorf("%s: %w", address, err))
			continue
		}
		connection = candidate
		break
	}
	if connection == nil {
		return nil, fmt.Errorf("connect direct signaling: %w", errors.Join(dialErrors...))
	}
	defer connection.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	}
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.SetDeadline(time.Now())
		case <-stop:
		}
	}()
	defer close(stop)
	if err := directsignal.WriteMessage(connection, request); err != nil {
		return nil, err
	}
	response := &remoteauthpb.DirectSignalingResponseV1{}
	if err := directsignal.ReadMessage(connection, response); err != nil {
		return nil, err
	}
	switch payload := response.GetPayload().(type) {
	case *remoteauthpb.DirectSignalingResponseV1_Answer:
		if payload.Answer == nil {
			return nil, fmt.Errorf("direct signaling returned an empty answer")
		}
		return payload.Answer, nil
	case *remoteauthpb.DirectSignalingResponseV1_Error:
		if payload.Error == nil {
			return nil, fmt.Errorf("direct signaling returned an empty error")
		}
		return nil, &SignalingError{Code: payload.Error.GetCode(), Message: payload.Error.GetMessage()}
	default:
		return nil, fmt.Errorf("direct signaling returned an unknown response")
	}
}

// SignalingError 是 daemon 返回的稳定 Direct signaling admission 失败。
type SignalingError struct {
	Code    remoteauthpb.DirectSignalingErrorCode
	Message string
}

// Error 返回脱敏错误文本；调用方应使用 Code 分类，不解析字符串。
func (failure *SignalingError) Error() string {
	if failure == nil {
		return ""
	}
	return failure.Message
}

// Session 把 authenticated protocol client 与当前 Pion peer 绑定为 exact-close ReadyPeerSession。
// ApplicationClient 拥有 Proto command/event/resource；本类型只补齐 peer 生命周期，不能创建新 generation。
type Session struct {
	*protocoladapter.ApplicationClient
	peer      port.ManagedPeer
	closeOnce sync.Once
	closeErr  error
}

func newSession(application *protocoladapter.ApplicationClient, peer port.ManagedPeer) *Session {
	session := &Session{ApplicationClient: application, peer: peer}
	go func() {
		<-application.Done()
		_ = session.Close()
	}()
	return session
}

// ExecuteApplication 通过当前 generation 的 ApplicationSession 写入 correlation stamp 后执行 generated Proto command。
func (session *Session) ExecuteApplication(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	return session.ApplicationSession.Execute(ctx, command)
}

// ExecuteApplicationTerminal 为 resource-producing command 保留有界 terminal response，并使用同一 generation fence。
func (session *Session) ExecuteApplicationTerminal(ctx context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	return session.ApplicationSession.ExecuteTerminal(ctx, command)
}

// Close 幂等关闭 protocol/DataChannel 与 Pion peer，并等待两侧资源释放请求完成。
func (session *Session) Close() error {
	if session == nil {
		return nil
	}
	session.closeOnce.Do(func() {
		if session.ApplicationClient != nil {
			session.closeErr = session.ApplicationClient.Close()
		}
		if session.peer != nil {
			if err := session.peer.Close(); session.closeErr == nil {
				session.closeErr = err
			}
		}
	})
	return session.closeErr
}

var _ clientruntime.PeerConnector = (*Dialer)(nil)
var _ clientruntime.ApplicationReadyPeerSession = (*Session)(nil)
