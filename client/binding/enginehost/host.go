// Package enginehost 提供 Android JNI 与浏览器 WASM 共用的 Go Client Engine composition root。
// 平台只能注入 credential/Cloud primitive；Endpoint Route、remote-auth、Hello、Proto API 与 generation 真值留在 Go。
package enginehost

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/client/adapter/direct"
	"github.com/lozzow/termx/client/adapter/managed"
	peeradapter "github.com/lozzow/termx/client/adapter/peer"
	sshadapter "github.com/lozzow/termx/client/adapter/ssh"
	"github.com/lozzow/termx/client/binding"
	"github.com/lozzow/termx/client/endpoint"
	"github.com/lozzow/termx/client/port"
	clientruntime "github.com/lozzow/termx/client/runtime"
	"github.com/lozzow/termx/proto/bindingpb"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/proto/remoteauthpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/remoteauth"
	"google.golang.org/protobuf/proto"
)

const bootstrapPrefix = "termx://bootstrap?payload="

// Options 定义单个平台 generation 的 primitive 依赖。
// Broker 与 peer factory 必须只属于当前 engine；关闭后不得复用到下一代。
type Options struct {
	Broker           *binding.PlatformBroker
	DirectPeers      direct.PeerFactory
	ManagedPeers     port.ManagedPeerFactory
	SSHCredentials   port.SSHCredentialSource
	ClientName       string
	CredentialPrefix string
	Now              func() time.Time
	SessionAuthority *clientruntime.SessionGenerationAuthority
}

// Host 是跨 Android/Web 共用的 binding.Host、PairingHost 与 CredentialHost。
type Host struct {
	options        Options
	owner          *clientruntime.SessionOwner
	registryMu     sync.Mutex
	registry       endpoint.Registry
	registryLoaded bool
	closeOnce      sync.Once
}

// New 校验平台依赖并创建共享 managed host。
func New(options Options) (*Host, error) {
	if options.Broker == nil || options.DirectPeers == nil && options.ManagedPeers == nil {
		return nil, fmt.Errorf("binding engine host requires broker and at least one peer factory")
	}
	options.ClientName = strings.TrimSpace(options.ClientName)
	if options.ClientName == "" {
		return nil, fmt.Errorf("managed binding client name is required")
	}
	options.CredentialPrefix = strings.TrimSpace(options.CredentialPrefix)
	if options.CredentialPrefix == "" {
		return nil, fmt.Errorf("managed binding credential prefix is required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Host{options: options, owner: clientruntime.NewSessionOwnerWithAuthority(options.SessionAuthority)}, nil
}

// OpenSession 从 generated EndpointConfigV1 建立 Go-owned generation；平台 UI 状态不能参与 endpoint、Route、auth 或协议判断。
func (host *Host) OpenSession(ctx context.Context, request *bindingpb.OpenSessionRequest) (clientruntime.ApplicationReadyPeerSession, error) {
	if request == nil {
		return nil, fmt.Errorf("open session request is required")
	}
	intent, err := connectIntent(request.GetIntent())
	if err != nil {
		return nil, err
	}
	endpointID := strings.TrimSpace(request.GetEndpointId())
	if endpointID == "" {
		return nil, fmt.Errorf("open session endpoint_id is required")
	}
	target, err := host.registryEndpoint(ctx, endpoint.EndpointID(endpointID))
	if err != nil {
		return nil, err
	}
	routeID := endpoint.RouteID(strings.TrimSpace(request.GetRouteOverride()))
	if routeID == "" {
		routes := target.RouteList()
		if len(routes) != 1 {
			return nil, fmt.Errorf("open session route_override is required for multi-route endpoint")
		}
		routeID = routes[0].ID
	}
	route, ok := target.Route(routeID)
	if !ok {
		return nil, fmt.Errorf("endpoint %q has no route %q", target.ID, routeID)
	}
	credentials := platformCredentials{broker: host.options.Broker}
	authorizer := peeradapter.CapabilityAuthorizer{Credentials: credentials, Signers: credentials, Now: host.options.Now}
	wireTarget, err := endpoint.EndpointToProto(target)
	if err != nil {
		return nil, err
	}
	config := sessionConfig(wireTarget, routeID, request.GetIntent())
	switch route.Kind {
	case endpoint.RouteDirectWebRTCTCP:
		if host.options.DirectPeers == nil {
			return nil, fmt.Errorf("Direct WebRTC peer factory is unavailable")
		}
		return host.owner.AcquireRoute(ctx, target, routeID, intent, config, &direct.Dialer{
			Peers: host.options.DirectPeers, Authorization: authorizer, ClientName: host.options.ClientName, Now: host.options.Now,
		})
	case endpoint.RouteSSHWebRTCTCP:
		if host.options.DirectPeers == nil || host.options.SSHCredentials == nil {
			return nil, fmt.Errorf("SSH WebRTC peer factory or credential source is unavailable")
		}
		return host.owner.AcquireRoute(ctx, target, routeID, intent, config, sshadapter.NewDialer(sshadapter.Options{
			Peers: host.options.DirectPeers, Authorization: authorizer, Credentials: host.options.SSHCredentials,
			ClientName: host.options.ClientName,
		}))
	case endpoint.RouteManagedWebRTC:
		if host.options.ManagedPeers == nil {
			return nil, fmt.Errorf("managed WebRTC peer factory is unavailable")
		}
		return host.owner.AcquireRoute(ctx, target, routeID, intent, config, &managed.Dialer{
			Cloud: platformCloud{broker: host.options.Broker}, Peers: host.options.ManagedPeers, ClientName: host.options.ClientName,
			Authorization: authorizer, Now: host.options.Now,
		})
	default:
		return nil, fmt.Errorf("binding engine host does not support route kind %q", route.Kind)
	}
}

// ImportPairing 验证 bootstrap、使用平台不可导出 signer 完成 PairingExchange，并原子绑定 grant。
func (host *Host) ImportPairing(ctx context.Context, request *bindingpb.ImportPairingRequest) (*bindingpb.ImportPairingResult, error) {
	payload, err := decodeBootstrap(request.GetPortablePayload())
	if err != nil {
		return nil, err
	}
	now := host.options.Now().UTC()
	bundle, claims, err := remoteauth.ParsePairingBundle(payload, now)
	if err != nil {
		return nil, err
	}
	endpointID := strings.TrimSpace(request.GetExpectedEndpointId())
	if endpointID == "" {
		endpointID = bundle.GetIdentity().GetDeviceId()
	}
	credentialRef := credentialRef(host.options.CredentialPrefix, bundle.GetIdentity().GetDeviceId(), bundle.GetIdentity().GetDeviceFingerprint())
	response, err := host.options.Broker.Exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_CredentialPrepare{
		CredentialPrepare: &bindingpb.CredentialPrepareRequest{EndpointId: endpointID, CredentialRef: credentialRef},
	}})
	if err != nil {
		return nil, err
	}
	record, err := platformCredential(response)
	if err != nil {
		return nil, err
	}
	candidate, err := endpoint.EndpointCandidateFromBootstrapBundle(bundle)
	if err != nil {
		return nil, err
	}
	var directRoute endpoint.AccessRoute
	var managedRoute endpoint.AccessRoute
	for _, route := range candidate.Routes {
		if !route.Enabled {
			continue
		}
		if route.Kind == endpoint.RouteDirectWebRTCTCP && directRoute.ID == "" {
			directRoute = route
		}
		if route.Kind == endpoint.RouteManagedWebRTC && managedRoute.ID == "" {
			managedRoute = route
		}
	}
	pairingRoute := directRoute
	if pairingRoute.ID == "" {
		pairingRoute = managedRoute
	}
	if pairingRoute.ID == "" {
		return nil, fmt.Errorf("pairing bundle has no supported WebRTC route")
	}
	target := pairingTarget(endpointID, candidate.Identity, pairingRoute, credentialRef)
	attempt, err := host.owner.BeginRouteAttempt(target, pairingRoute.ID, clientruntime.ConnectIntentInteractive)
	if err != nil {
		return nil, err
	}
	identity := remoteauth.ClientAccessIdentity{
		EndpointID: endpointID, PublicKey: append(ed25519.PublicKey(nil), record.GetPublicKey()...), Fingerprint: record.GetKeyFingerprint(),
	}
	if err := identity.ValidatePublic(); err != nil {
		return nil, fmt.Errorf("pairing identity is invalid: %w", err)
	}
	signer := platformSigner{broker: host.options.Broker, credentialRef: credentialRef, identity: identity}
	pairingRequest := remoteauth.ClientPairingRequest{
		ExpectedDeviceID: bundle.GetIdentity().GetDeviceId(), ExpectedDeviceFingerprint: bundle.GetIdentity().GetDeviceFingerprint(),
		PairingBundle: payload, Identity: identity, Signer: signer, ClientLabel: host.options.ClientName,
	}
	var paired remoteauth.PairingExchangeResult
	switch pairingRoute.Kind {
	case endpoint.RouteDirectWebRTCTCP:
		if host.options.DirectPeers == nil {
			return nil, fmt.Errorf("Direct pairing peer factory is unavailable")
		}
		paired, err = (&direct.PairingConnector{Peers: host.options.DirectPeers, Now: host.options.Now}).Redeem(ctx, attempt, pairingRequest)
	case endpoint.RouteManagedWebRTC:
		if host.options.ManagedPeers == nil {
			return nil, fmt.Errorf("managed pairing peer factory is unavailable")
		}
		paired, err = (&managed.PairingConnector{Cloud: platformCloud{broker: host.options.Broker}, Peers: host.options.ManagedPeers, Now: host.options.Now}).Redeem(ctx, attempt, pairingRequest)
	}
	if err != nil {
		return nil, err
	}
	boundResponse, err := host.options.Broker.Exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_CredentialBind{
		CredentialBind: &bindingpb.CredentialBindRequest{EndpointId: endpointID, CredentialRef: credentialRef, CapabilityGrant: paired.Grant},
	}})
	if err != nil {
		return nil, err
	}
	bound, err := platformCredential(boundResponse)
	if err != nil {
		return nil, err
	}
	if bound.GetKeyFingerprint() != identity.Fingerprint || bound.GetCapabilityGrant() != paired.Grant {
		return nil, fmt.Errorf("platform secure store bound a different pairing credential")
	}
	committed, registry, err := host.commitPairingEndpoint(ctx, endpoint.EndpointID(endpointID), candidate, credentialRef)
	if err != nil {
		rollbackContext, cancelRollback := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelRollback()
		rollbackErr := host.rollbackPreparedCredential(rollbackContext, record, paired.Grant)
		if rollbackErr != nil {
			return nil, fmt.Errorf("commit paired endpoint: %v; rollback credential: %w", err, rollbackErr)
		}
		return nil, err
	}
	return &bindingpb.ImportPairingResult{
		Endpoint: committed, Registry: registry, TicketId: claims.TicketID, ClientKeyFingerprint: record.GetKeyFingerprint(),
		ExpiresAtUnixNano: paired.ExpiresAt.UnixNano(), AuthorizationRequired: false,
	}, nil
}

// DeleteCredential 删除当前平台 secure store 中的 credential record。
func (host *Host) DeleteCredential(ctx context.Context, request *bindingpb.DeleteCredentialRequest) error {
	response, err := host.options.Broker.Exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_CredentialDelete{
		CredentialDelete: &bindingpb.CredentialDeleteRequest{CredentialRef: request.GetCredentialRef()},
	}})
	if err != nil {
		return err
	}
	return platformResponseError(response)
}

// Close 关闭当前 generation 的 peer factory 与 broker，解除冻结中的平台等待。
func (host *Host) Close() error {
	if host == nil {
		return nil
	}
	host.closeOnce.Do(func() {
		_ = host.owner.Close()
		if closer, ok := host.options.DirectPeers.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		if closer, ok := host.options.ManagedPeers.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		_ = host.options.Broker.Close()
	})
	return nil
}

// Broker 返回当前 engine 独占的平台 broker，供 JNI/WASM wrapper 驱动。
func (host *Host) Broker() *binding.PlatformBroker { return host.options.Broker }

func sessionConfig(config *remoteauthpb.EndpointConfigV1, routeID endpoint.RouteID, intent bindingpb.ConnectIntent) string {
	payload, _ := (proto.MarshalOptions{Deterministic: true}).Marshal(config)
	return fmt.Sprintf("%s\x00%d\x00%x", routeID, intent, sha256.Sum256(payload))
}

func pairingTarget(endpointID string, identity endpoint.DaemonIdentity, route endpoint.AccessRoute, credentialRef string) endpoint.Endpoint {
	route.CredentialRef = credentialRef
	if route.Kind == endpoint.RouteManagedWebRTC && route.TargetDeviceID == "" {
		route.TargetDeviceID = identity.DeviceID
	}
	return endpoint.Endpoint{
		ID: endpoint.EndpointID(endpointID), DaemonIdentity: identity,
		Routes: map[endpoint.RouteID]endpoint.AccessRoute{route.ID: route},
	}
}

type platformCredentials struct{ broker *binding.PlatformBroker }

func (source platformCredentials) ResolveClientCredential(ctx context.Context, endpointID, reference string) (remoteauth.ClientAccessCredential, error) {
	response, err := source.broker.Exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_CredentialResolve{
		CredentialResolve: &bindingpb.CredentialResolveRequest{EndpointId: endpointID, CredentialRef: reference},
	}})
	if err != nil {
		return remoteauth.ClientAccessCredential{}, err
	}
	record, err := platformCredential(response)
	if err != nil {
		return remoteauth.ClientAccessCredential{}, err
	}
	identity := remoteauth.ClientAccessIdentity{EndpointID: record.GetEndpointId(), Fingerprint: record.GetKeyFingerprint(), PublicKey: append(ed25519.PublicKey(nil), record.GetPublicKey()...)}
	if err := identity.ValidatePublic(); err != nil {
		return remoteauth.ClientAccessCredential{}, err
	}
	return remoteauth.ClientAccessCredential{Version: 1, EndpointID: record.GetEndpointId(), Identity: identity, CapabilityGrant: record.GetCapabilityGrant(), UpdatedAt: time.Now().UTC()}, nil
}

func (source platformCredentials) ResolveClientSigner(_ context.Context, endpointID, reference string, identity remoteauth.ClientAccessIdentity) (remoteauth.ClientAccessSigner, error) {
	if identity.EndpointID != endpointID {
		return nil, fmt.Errorf("platform signer endpoint mismatch")
	}
	return platformSigner{broker: source.broker, credentialRef: reference, identity: identity}, nil
}

type platformSigner struct {
	broker        *binding.PlatformBroker
	credentialRef string
	identity      remoteauth.ClientAccessIdentity
}

func (signer platformSigner) Sign(ctx context.Context, payload []byte) ([]byte, error) {
	response, err := signer.broker.Exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_CredentialSign{
		CredentialSign: &bindingpb.CredentialSignRequest{CredentialRef: signer.credentialRef, Payload: append([]byte(nil), payload...)},
	}})
	if err != nil {
		return nil, err
	}
	if err := platformResponseError(response); err != nil {
		return nil, err
	}
	signature := append([]byte(nil), response.GetCredentialSign().GetSignature()...)
	if !ed25519.Verify(signer.identity.PublicKey, payload, signature) {
		return nil, fmt.Errorf("platform signer returned an invalid signature")
	}
	return signature, nil
}

type platformCloud struct{ broker *binding.PlatformBroker }

func (cloud platformCloud) ResolveEndpoint(ctx context.Context, request *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error) {
	response, err := cloud.exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_CloudResolveEndpoint{CloudResolveEndpoint: proto.Clone(request).(*cloudpb.ResolveEndpointRequest)}})
	if err != nil {
		return nil, err
	}
	return proto.Clone(response.GetCloudResolvedEndpoint()).(*cloudpb.ResolvedEndpoint), nil
}

func (cloud platformCloud) CreateSignalingSession(ctx context.Context, request *cloudpb.CreateSignalingSessionRequest) (cloudcompanion.SignalingStream, error) {
	response, err := cloud.exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_CloudCreateSignaling{CloudCreateSignaling: proto.Clone(request).(*cloudpb.CreateSignalingSessionRequest)}})
	if err != nil {
		return nil, err
	}
	return &signalingStream{events: cloneSignalingEvents(response.GetCloudSignaling().GetEvents())}, nil
}

func (cloud platformCloud) AcquireRelayLease(ctx context.Context, request *cloudpb.AcquireRelayLeaseRequest) (*cloudpb.RelayLease, error) {
	response, err := cloud.exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_CloudAcquireRelay{CloudAcquireRelay: proto.Clone(request).(*cloudpb.AcquireRelayLeaseRequest)}})
	if err != nil {
		return nil, err
	}
	return proto.Clone(response.GetCloudRelayLease()).(*cloudpb.RelayLease), nil
}

func (cloud platformCloud) PlanManagedRoute(ctx context.Context, request *cloudpb.PlanManagedRouteRequest) (*cloudpb.ManagedRoutePlan, error) {
	response, err := cloud.exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_CloudPlanRoute{CloudPlanRoute: proto.Clone(request).(*cloudpb.PlanManagedRouteRequest)}})
	if err != nil {
		return nil, err
	}
	return proto.Clone(response.GetCloudRoutePlan()).(*cloudpb.ManagedRoutePlan), nil
}

func (cloud platformCloud) ReportPathQuality(ctx context.Context, request *cloudpb.ReportPathQualityRequest) (*cloudpb.ReportPathQualityResponse, error) {
	response, err := cloud.exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_CloudReportQuality{CloudReportQuality: proto.Clone(request).(*cloudpb.ReportPathQualityRequest)}})
	if err != nil {
		return nil, err
	}
	return proto.Clone(response.GetCloudQualityReported()).(*cloudpb.ReportPathQualityResponse), nil
}

func (cloud platformCloud) ReportConnectionOutcome(ctx context.Context, request *cloudpb.ReportConnectionOutcomeRequest) (*cloudpb.ReportConnectionOutcomeResponse, error) {
	response, err := cloud.exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_CloudReportOutcome{CloudReportOutcome: proto.Clone(request).(*cloudpb.ReportConnectionOutcomeRequest)}})
	if err != nil {
		return nil, err
	}
	return proto.Clone(response.GetCloudOutcomeReported()).(*cloudpb.ReportConnectionOutcomeResponse), nil
}

func (cloud platformCloud) exchange(ctx context.Context, request *bindingpb.PlatformRequest) (*bindingpb.PlatformResponse, error) {
	response, err := cloud.broker.Exchange(ctx, request)
	if err != nil {
		return nil, err
	}
	if err := platformResponseError(response); err != nil {
		return nil, err
	}
	if response.GetResponse() == nil {
		return nil, fmt.Errorf("platform Cloud response is empty")
	}
	return response, nil
}

type signalingStream struct {
	mu     sync.Mutex
	events []*cloudpb.SignalingEvent
	closed bool
}

func (stream *signalingStream) Receive() (*cloudpb.SignalingEvent, error) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.closed || len(stream.events) == 0 {
		return nil, io.EOF
	}
	event := stream.events[0]
	stream.events = stream.events[1:]
	return proto.Clone(event).(*cloudpb.SignalingEvent), nil
}

func (stream *signalingStream) Close() error {
	stream.mu.Lock()
	stream.closed = true
	stream.events = nil
	stream.mu.Unlock()
	return nil
}

func platformCredential(response *bindingpb.PlatformResponse) (*bindingpb.CredentialRecord, error) {
	if err := platformResponseError(response); err != nil {
		return nil, err
	}
	record := response.GetCredential()
	if record == nil || record.GetEndpointId() == "" || record.GetCredentialRef() == "" {
		return nil, fmt.Errorf("platform credential response is incomplete")
	}
	return proto.Clone(record).(*bindingpb.CredentialRecord), nil
}

func platformResponseError(response *bindingpb.PlatformResponse) error {
	if response == nil {
		return fmt.Errorf("platform response is empty")
	}
	if value := response.GetError(); value != nil {
		return fmt.Errorf("platform request failed: %s", value.GetMessage())
	}
	return nil
}

func connectIntent(value bindingpb.ConnectIntent) (clientruntime.ConnectIntent, error) {
	switch value {
	case bindingpb.ConnectIntent_CONNECT_INTENT_INTERACTIVE:
		return clientruntime.ConnectIntentInteractive, nil
	case bindingpb.ConnectIntent_CONNECT_INTENT_BACKGROUND:
		return clientruntime.ConnectIntentBackground, nil
	case bindingpb.ConnectIntent_CONNECT_INTENT_PROBE:
		return clientruntime.ConnectIntentProbe, nil
	default:
		return "", fmt.Errorf("connect intent is unsupported")
	}
}

func decodeBootstrap(value string) ([]byte, error) {
	encoded := strings.TrimSpace(value)
	if strings.HasPrefix(encoded, bootstrapPrefix) {
		encoded = strings.TrimPrefix(encoded, bootstrapPrefix)
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(payload) == 0 {
		return nil, fmt.Errorf("pairing bootstrap payload is invalid")
	}
	return payload, nil
}

func credentialRef(prefix, deviceID, fingerprint string) string {
	digest := sha256.Sum256([]byte(deviceID + "\n" + fingerprint))
	return prefix + base64.RawURLEncoding.EncodeToString(digest[:])
}

func cloneSignalingEvents(values []*cloudpb.SignalingEvent) []*cloudpb.SignalingEvent {
	result := make([]*cloudpb.SignalingEvent, 0, len(values))
	for _, value := range values {
		if value != nil {
			result = append(result, proto.Clone(value).(*cloudpb.SignalingEvent))
		}
	}
	return result
}

var _ binding.Host = (*Host)(nil)
var _ binding.PairingHost = (*Host)(nil)
var _ binding.CredentialHost = (*Host)(nil)
var _ binding.EndpointRegistryHost = (*Host)(nil)
var _ peeradapter.CredentialSource = platformCredentials{}
var _ peeradapter.SignerSource = platformCredentials{}
var _ remoteauth.ClientAccessSigner = platformSigner{}
var _ managed.CloudClient = platformCloud{}
var _ cloudcompanion.SignalingStream = (*signalingStream)(nil)
