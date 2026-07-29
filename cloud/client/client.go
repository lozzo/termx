// Package client 实现 Go Client Engine 复用的 Controller 解析与 Edge ClientGateway 协议。
// 它只运输 generated Cloud Proto，不选择 Endpoint Route，也不拥有 PeerSession generation。
package client

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/anytty/anytty/cloud/edge/clientgateway"
	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/shared/remoteauth"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Signer 是 Android Keystore 或 desktop secure credential 对 canonical Cloud proof 的异步签名边界。
type Signer interface {
	Sign(context.Context, []byte) ([]byte, error)
}

// Config 是当前 Cloud account/profile 解析出的 Controller endpoint；不属于 Endpoint/Route 真值。
type Config struct {
	ControllerAddress    string
	ControllerServerName string
	ControllerCAPEM      []byte
	Now                  func() time.Time
}

// Client 是无状态 Cloud 网络协议客户端；每次 Resolve/Exchange 都使用当前调用的 grant 和 generation。
type Client struct{ config Config }

// RouteResolution 是一次已认证的目录结果，或者由本机缓存 Edge locator 与原始 daemon grant 重建。
type RouteResolution struct {
	locator    *cloudv1.EdgeLocator
	routeGrant *cloudv1.SignedEnvelope
	accessMode cloudv1.CloudClientAccessMode
}

func NewCachedRoute(locator *cloudv1.EdgeLocator, routeGrant *cloudv1.SignedEnvelope, accessMode cloudv1.CloudClientAccessMode) (*RouteResolution, error) {
	if err := validateEdgeLocator(locator); err != nil || routeGrant == nil || (accessMode != cloudv1.CloudClientAccessMode_CLOUD_CLIENT_ACCESS_MODE_CAPABILITY && accessMode != cloudv1.CloudClientAccessMode_CLOUD_CLIENT_ACCESS_MODE_PAIRING) {
		return nil, errors.New("cached Cloud Route is incomplete")
	}
	return &RouteResolution{locator: proto.Clone(locator).(*cloudv1.EdgeLocator), routeGrant: proto.Clone(routeGrant).(*cloudv1.SignedEnvelope), accessMode: accessMode}, nil
}

func (resolution *RouteResolution) Locator() *cloudv1.EdgeLocator {
	if resolution == nil || resolution.locator == nil {
		return nil
	}
	return proto.Clone(resolution.locator).(*cloudv1.EdgeLocator)
}

// SignalSession 持有一个已经完成 offer/answer 的 ClientGateway 流。
// 该流跟随 ReadyPeerSession 存活，使 Edge 的纯内存客户端投影与真实 P2P 生命周期一致；terminal 数据仍只走 DataChannel。
type SignalSession struct {
	answer     *cloudv1.EdgeAnswer
	connection *grpc.ClientConn
	stream     cloudv1.ClientGateway_ConnectClient
	cancel     context.CancelFunc
	closeOnce  sync.Once
	closeErr   error
}

// Answer 返回 daemon 生成的不可变 SDP answer 投影。
func (session *SignalSession) Answer() *cloudv1.EdgeAnswer {
	if session == nil || session.answer == nil {
		return nil
	}
	return proto.Clone(session.answer).(*cloudv1.EdgeAnswer)
}

// Close 结束 ClientGateway 观察流并释放 Edge gRPC 连接；可以重复调用。
func (session *SignalSession) Close() error {
	if session == nil {
		return nil
	}
	session.closeOnce.Do(func() {
		if session.stream != nil {
			session.closeErr = session.stream.CloseSend()
		}
		if session.cancel != nil {
			session.cancel()
		}
		if session.connection != nil {
			if err := session.connection.Close(); session.closeErr == nil {
				session.closeErr = err
			}
		}
	})
	return session.closeErr
}

// NewClient 验证 Controller TLS locator；账号 session 在 R7 接入，但 R5 不允许使用明文或跳过证书校验。
func NewClient(config Config) (*Client, error) {
	config.ControllerAddress = strings.TrimSpace(config.ControllerAddress)
	config.ControllerServerName = strings.TrimSpace(config.ControllerServerName)
	if config.ControllerAddress == "" || config.ControllerServerName == "" {
		return nil, errors.New("Cloud Controller address and TLS server name are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Client{config: config}, nil
}

// Resolve 只在本机没有 Edge locator 或旧 Edge 失效时查询实时 Presence；返回结果仍使用原始 daemon grant 准入。
func (client *Client) Resolve(ctx context.Context, cloudRouteGrant []byte, signer Signer) (*RouteResolution, error) {
	if client == nil || signer == nil || len(cloudRouteGrant) == 0 {
		return nil, errors.New("Cloud route grant and signer are required")
	}
	grant := &cloudv1.SignedEnvelope{}
	if err := proto.Unmarshal(cloudRouteGrant, grant); err != nil {
		return nil, fmt.Errorf("decode CloudRouteGrant: %w", err)
	}
	connection, err := client.dial(client.config.ControllerAddress, client.config.ControllerServerName, client.config.ControllerCAPEM)
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	directory := cloudv1.NewDirectoryServiceClient(connection)
	challenge, err := directory.BeginClientRoute(ctx, &cloudv1.BeginClientRouteRequest{CloudRouteGrant: grant})
	if err != nil {
		return nil, fmt.Errorf("begin Cloud route resolution: %w", err)
	}
	requestID := uuid.NewString()
	canonical, err := ticket.ClientRouteProofBytes(challenge.GetChallengeId(), challenge.GetChallenge(), grant, requestID)
	if err != nil {
		return nil, err
	}
	proof, err := signer.Sign(ctx, canonical)
	if err != nil {
		return nil, fmt.Errorf("sign Cloud route challenge: %w", err)
	}
	resolved, err := directory.ResolveClientRoute(ctx, &cloudv1.ResolveClientRouteRequest{ChallengeId: challenge.GetChallengeId(), RequestId: requestID, ClientProof: proof})
	if err != nil {
		return nil, fmt.Errorf("resolve Cloud route: %w", err)
	}
	if resolved.GetEdgeLocator() == nil {
		return nil, errors.New("Cloud route response is incomplete")
	}
	return NewCachedRoute(resolved.GetEdgeLocator(), grant, cloudv1.CloudClientAccessMode_CLOUD_CLIENT_ACCESS_MODE_CAPABILITY)
}

// PairingRoute 只使用 claim offer 内由 daemon 签名绑定的 locator，不访问 Controller。
func (client *Client) PairingRoute(pairingClaimOffer []byte) (*RouteResolution, error) {
	if client == nil || len(pairingClaimOffer) == 0 {
		return nil, errors.New("Cloud pairing route input is incomplete")
	}
	offer, err := remoteauth.ParsePairingClaimOfferForExchange(pairingClaimOffer)
	if err != nil {
		return nil, err
	}
	var grant *cloudv1.SignedEnvelope
	var locator *cloudv1.EdgeLocator
	var locatorPayload []byte
	for _, route := range offer.GetRoutes() {
		managed := route.GetManagedWebrtc()
		if managed == nil || len(managed.GetBootstrapGrant()) == 0 || len(managed.GetEdgeLocator()) == 0 {
			continue
		}
		candidate := &cloudv1.SignedEnvelope{}
		if err := proto.Unmarshal(managed.GetBootstrapGrant(), candidate); err != nil {
			return nil, errors.New("Cloud pairing bootstrap grant is invalid")
		}
		grant = candidate
		locator, err = DecodeEdgeLocator(managed.GetEdgeLocator())
		if err != nil {
			return nil, err
		}
		locatorPayload = append([]byte(nil), managed.GetEdgeLocator()...)
		break
	}
	if grant == nil {
		return nil, errors.New("pairing claim offer has no Cloud bootstrap grant")
	}
	unverified := &cloudv1.PairingRouteGrantClaims{}
	if err := proto.Unmarshal(grant.GetPayload(), unverified); err != nil {
		return nil, errors.New("Cloud pairing bootstrap grant payload is invalid")
	}
	verifiedGrant, err := ticket.VerifyPairingRouteGrant(grant, ed25519.PublicKey(offer.GetDevicePublicKey()), unverified.GetDaemonId(), client.config.Now().UTC())
	if err != nil || verifiedGrant.GetDeviceId() != offer.GetDeviceId() {
		return nil, errors.New("Cloud pairing bootstrap grant does not match the invited device")
	}
	digest := sha256.Sum256(offer.GetClaim())
	if !bytes.Equal(verifiedGrant.GetPairingClaimSha256(), digest[:]) {
		return nil, errors.New("Cloud pairing bootstrap grant does not match the invited claim")
	}
	locatorDigest := sha256.Sum256(locatorPayload)
	if !bytes.Equal(verifiedGrant.GetEdgeLocatorSha256(), locatorDigest[:]) {
		return nil, errors.New("Cloud pairing bootstrap grant does not match its Edge locator")
	}
	return NewCachedRoute(locator, grant, cloudv1.CloudClientAccessMode_CLOUD_CLIENT_ACCESS_MODE_PAIRING)
}

// Exchange 连接目标 Edge，并用 daemon RouteGrant 与本次 client proof 完成一次 offer/answer。
func (client *Client) Exchange(ctx context.Context, resolution *RouteResolution, identity remoteauth.ClientAccessIdentity, signer Signer, product cloudv1.ClientProduct, attemptGeneration uint64, relayPreference cloudv1.RelayPreference, createOffer func(context.Context, *cloudv1.ClientReady) (string, error)) (*SignalSession, error) {
	if client == nil || resolution == nil || resolution.locator == nil || resolution.routeGrant == nil || signer == nil || identity.ValidatePublic() != nil || product == cloudv1.ClientProduct_CLIENT_PRODUCT_UNSPECIFIED || attemptGeneration == 0 || createOffer == nil {
		return nil, errors.New("Cloud signaling input is incomplete")
	}
	locator, routeGrant := resolution.locator, resolution.routeGrant
	connection, err := client.dial(locator.GetPublicEndpoint(), locator.GetServerName(), locator.GetCaCertificatePem())
	if err != nil {
		return nil, markEdgeLocatorUnavailable(err)
	}
	if err := waitForEdgeTransport(ctx, connection); err != nil {
		_ = connection.Close()
		return nil, markEdgeLocatorUnavailable(err)
	}
	closeConnection := true
	defer func() {
		if closeConnection {
			_ = connection.Close()
		}
	}()
	// route racer 会在 winner 发布后取消 attempt context；ClientGateway 需要在 answer 前响应该取消，
	// 成功后则改由 ReadyPeerSession lifecycle 持有，不能被 winner 自己的 attempt cancel 误关。
	streamContext, cancelStream := context.WithCancel(context.WithoutCancel(ctx))
	stopParentCancellation := context.AfterFunc(ctx, cancelStream)
	keepStream := false
	defer func() {
		if !keepStream {
			_ = stopParentCancellation()
			cancelStream()
		}
	}()
	stream, err := cloudv1.NewClientGatewayClient(connection).Connect(streamContext)
	if err != nil {
		return nil, err
	}
	sessionID, bootID := uuid.NewString(), uuid.NewString()
	canonical, err := ticket.CloudRouteHelloProofBytes(routeGrant, locator.GetEdgeId(), sessionID, attemptGeneration)
	if err != nil {
		return nil, err
	}
	proof, err := signer.Sign(ctx, canonical)
	if err != nil {
		return nil, err
	}
	hello := &cloudv1.ClientSignal{ProtocolVersion: clientgateway.ProtocolVersion, MessageId: uuid.NewString(), SenderId: identity.Fingerprint, BootId: bootID, ConnectionId: sessionID, StreamSeq: 1, SentAt: timestamppb.New(client.config.Now().UTC()), Payload: &cloudv1.ClientSignal_Hello{Hello: &cloudv1.ClientHello{RouteGrant: routeGrant, ClientPublicKey: append([]byte(nil), identity.PublicKey...), ClientProof: proof, Product: product, SoftwareVersion: "development", AttemptGeneration: attemptGeneration, RelayPreference: relayPreference, AccessMode: resolution.accessMode}}}
	if err := stream.Send(hello); err != nil {
		return nil, err
	}
	ready, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	if ready.GetReady() == nil || ready.GetProtocolVersion() != clientgateway.ProtocolVersion || ready.GetConnectionId() != sessionID || ready.GetStreamSeq() != 1 || ready.GetReady().GetGeneration() != attemptGeneration {
		return nil, errors.New("ClientReady is invalid")
	}
	offerSDP, err := createOffer(ctx, ready.GetReady())
	if err != nil {
		return nil, fmt.Errorf("create Cloud P2P offer: %w", err)
	}
	if strings.TrimSpace(offerSDP) == "" {
		return nil, errors.New("create Cloud P2P offer returned an empty SDP")
	}
	offer := &cloudv1.ClientSignal{ProtocolVersion: clientgateway.ProtocolVersion, MessageId: uuid.NewString(), SenderId: identity.Fingerprint, BootId: bootID, ConnectionId: sessionID, StreamSeq: 2, SentAt: timestamppb.New(client.config.Now().UTC()), Payload: &cloudv1.ClientSignal_Offer{Offer: &cloudv1.ClientOffer{SessionId: sessionID, OfferSdp: offerSDP}}}
	if err := stream.Send(offer); err != nil {
		return nil, err
	}
	response, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	if response.GetConnectionId() != sessionID || response.GetStreamSeq() != 2 {
		return nil, errors.New("Edge signaling response is invalid")
	}
	if rejected := response.GetRejected(); rejected != nil {
		return nil, fmt.Errorf("Cloud signaling rejected (%s): %s", rejected.GetCode(), rejected.GetMessage())
	}
	if response.GetAnswer() == nil || response.GetAnswer().GetSessionId() != sessionID || strings.TrimSpace(response.GetAnswer().GetAnswerSdp()) == "" {
		return nil, errors.New("Edge signaling answer is invalid")
	}
	if !stopParentCancellation() || ctx.Err() != nil {
		cancelStream()
		return nil, ctx.Err()
	}
	closeConnection = false
	keepStream = true
	return &SignalSession{answer: proto.Clone(response.GetAnswer()).(*cloudv1.EdgeAnswer), connection: connection, stream: stream, cancel: cancelStream}, nil
}

func waitForEdgeTransport(ctx context.Context, connection *grpc.ClientConn) error {
	if connection == nil {
		return errors.New("Edge connection is nil")
	}
	dialContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	connection.Connect()
	for {
		state := connection.GetState()
		if state == connectivity.Ready {
			return nil
		}
		if state == connectivity.Shutdown {
			return errors.New("Edge transport shut down during connect")
		}
		if !connection.WaitForStateChange(dialContext, state) {
			return context.Cause(dialContext)
		}
	}
}

func (client *Client) dial(address, serverName string, caPEM []byte) (*grpc.ClientConn, error) {
	var roots *x509.CertPool
	if len(caPEM) != 0 {
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(caPEM) {
			return nil, errors.New("Cloud TLS CA certificate is invalid")
		}
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13, ServerName: strings.TrimSpace(serverName), RootCAs: roots}
	return grpc.NewClient(strings.TrimSpace(address), grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
}
