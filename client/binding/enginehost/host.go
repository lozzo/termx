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

	"github.com/muxvia/muxvia/client/adapter/direct"
	"github.com/muxvia/muxvia/client/adapter/managed"
	peeradapter "github.com/muxvia/muxvia/client/adapter/peer"
	shareadapter "github.com/muxvia/muxvia/client/adapter/share"
	sshadapter "github.com/muxvia/muxvia/client/adapter/ssh"
	systemadapter "github.com/muxvia/muxvia/client/adapter/system"
	"github.com/muxvia/muxvia/client/binding"
	"github.com/muxvia/muxvia/client/endpoint"
	"github.com/muxvia/muxvia/client/port"
	clientruntime "github.com/muxvia/muxvia/client/runtime"
	"github.com/muxvia/muxvia/proto/bindingpb"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/proto/remoteauthpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
	"github.com/muxvia/muxvia/shared/remoteauth"
	"google.golang.org/protobuf/proto"
)

const bootstrapPrefix = "muxvia://bootstrap?payload="

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
	ShareReceive     func(context.Context, *remoteauthpb.ShareSessionOffer) (*remoteauthpb.ClientEndpointShareBundleV1, error)
}

// Host 是跨 Android/Web 共用的 binding.Host、PairingHost 与 CredentialHost。
type Host struct {
	options        Options
	owner          *clientruntime.SessionOwner
	registryMu     sync.Mutex
	registry       endpoint.Registry
	registryLoaded bool
	pendingShares  map[string]*remoteauthpb.ClientEndpointShareBundleV1
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
	if options.SSHCredentials == nil && options.DirectPeers != nil {
		// Android/native binding 默认复用同一 platform broker 的不可导出 SSH signer；浏览器没有 Direct primitive，不会启用该 Route。
		options.SSHCredentials = platformSSHCredential{broker: options.Broker}
	}
	if options.ShareReceive == nil {
		options.ShareReceive = shareadapter.Receive
	}
	return &Host{options: options, owner: clientruntime.NewSessionOwnerWithAuthority(options.SessionAuthority), pendingShares: make(map[string]*remoteauthpb.ClientEndpointShareBundleV1)}, nil
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
	credentials := platformCredentials{broker: host.options.Broker}
	authorizer := peeradapter.CapabilityAuthorizer{Credentials: credentials, Signers: credentials, Now: host.options.Now}
	planningTarget, environment, err := routePlanEnvironment(ctx, target, host.options, credentials, platformManagedEligibility{broker: host.options.Broker})
	if err != nil {
		return nil, err
	}
	dialers, err := clientruntime.NewPeerConnectorMap(host.routeConnectors(authorizer))
	if err != nil {
		return nil, err
	}
	wireTarget, err := endpoint.EndpointToProto(planningTarget)
	if err != nil {
		return nil, err
	}
	config := sessionConfig(wireTarget, routeID, request.GetIntent(), environment)
	return host.owner.AcquirePlanned(ctx, planningTarget, routeID, intent, config, environment, systemadapter.Clock{}, dialers)
}

func (host *Host) routeConnectors(authorizer peeradapter.CapabilityAuthorizer) map[endpoint.RouteKind]clientruntime.PeerConnector {
	connectors := make(map[endpoint.RouteKind]clientruntime.PeerConnector, 3)
	if host.options.DirectPeers != nil {
		connectors[endpoint.RouteDirectWebRTCTCP] = &direct.Dialer{
			Peers: host.options.DirectPeers, Authorization: authorizer, ClientName: host.options.ClientName, Now: host.options.Now,
		}
		if host.options.SSHCredentials != nil {
			connectors[endpoint.RouteSSHWebRTCTCP] = sshadapter.NewDialer(sshadapter.Options{
				Peers: host.options.DirectPeers, Authorization: authorizer, Credentials: host.options.SSHCredentials,
				ClientName: host.options.ClientName,
			})
		}
	}
	if host.options.ManagedPeers != nil {
		connectors[endpoint.RouteManagedWebRTC] = &managed.Dialer{
			Cloud: platformCloud{broker: host.options.Broker}, Peers: host.options.ManagedPeers, ClientName: host.options.ClientName,
			Authorization: authorizer, Now: host.options.Now,
		}
	}
	return connectors
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
	pairingRoute, err := directPairingRoute(candidate)
	if err != nil {
		return nil, err
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
	if host.options.DirectPeers == nil {
		return nil, fmt.Errorf("Direct pairing peer factory is unavailable")
	}
	paired, err := (&direct.PairingConnector{Peers: host.options.DirectPeers, Now: host.options.Now}).Redeem(ctx, attempt, pairingRequest)
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

// directPairingRoute 固定使用 daemon embedded signaling 兑换一次性 grant。
// Cloud Route 只作为同一 Endpoint 的后续连接方式，不能建立第二套 managed-only 配对协议。
func directPairingRoute(candidate endpoint.EndpointCandidate) (endpoint.AccessRoute, error) {
	for _, route := range candidate.Routes {
		if route.Enabled && route.Kind == endpoint.RouteDirectWebRTCTCP {
			return route, nil
		}
	}
	return endpoint.AccessRoute{}, fmt.Errorf("pairing bundle requires an enabled Direct WebRTC TCP route")
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
		host.registryMu.Lock()
		host.pendingShares = make(map[string]*remoteauthpb.ClientEndpointShareBundleV1)
		host.registryMu.Unlock()
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

func sessionConfig(config *remoteauthpb.EndpointConfigV1, routeID endpoint.RouteID, intent bindingpb.ConnectIntent, environment clientruntime.RoutePlanEnvironment) string {
	payload, _ := (proto.MarshalOptions{Deterministic: true}).Marshal(config)
	return fmt.Sprintf("%s\x00%d\x00%x\x00%s\x00%s", routeID, intent, sha256.Sum256(payload), joinRouteKinds(environment.SupportedRouteKinds), strings.Join(environment.AvailableCredentialRefs, "\x00"))
}

type clientCredentialAvailability interface {
	Available(context.Context, string, string) bool
}

type managedRouteEligibility interface {
	Available(context.Context, endpoint.AccessRoute) bool
}

type sshCredentialAvailability interface {
	Available(string) bool
}

type contextSSHCredentialAvailability interface {
	AvailableContext(context.Context, string) bool
}

// routePlanEnvironment 生成当前调用的能力快照，并在副本中禁用当前账号不可用的 managed Route。
// Cloud 查询或登出只改变 managed eligibility，不能删除持久 Endpoint，也不能阻断 Direct/SSH。
func routePlanEnvironment(ctx context.Context, target endpoint.Endpoint, options Options, credentials clientCredentialAvailability, cloud managedRouteEligibility) (endpoint.Endpoint, clientruntime.RoutePlanEnvironment, error) {
	wireTarget, err := endpoint.EndpointToProto(target)
	if err != nil {
		return endpoint.Endpoint{}, clientruntime.RoutePlanEnvironment{}, err
	}
	planningTarget, err := endpoint.EndpointFromProto(wireTarget)
	if err != nil {
		return endpoint.Endpoint{}, clientruntime.RoutePlanEnvironment{}, err
	}
	environment := clientruntime.RoutePlanEnvironment{}
	if options.DirectPeers != nil {
		environment.SupportedRouteKinds = append(environment.SupportedRouteKinds, endpoint.RouteDirectWebRTCTCP)
	}
	_, sshContextSupported := options.SSHCredentials.(contextSSHCredentialAvailability)
	_, sshLegacySupported := options.SSHCredentials.(sshCredentialAvailability)
	sshSupported := options.DirectPeers != nil && options.SSHCredentials != nil && (sshContextSupported || sshLegacySupported)
	if sshSupported {
		environment.SupportedRouteKinds = append(environment.SupportedRouteKinds, endpoint.RouteSSHWebRTCTCP)
	}
	managedSupported := false
	available := make(map[string]struct{})
	credentialChecked := make(map[string]bool)
	for _, route := range planningTarget.RouteList() {
		if route.CredentialRef != "" && credentials != nil {
			credentialAvailable, checked := credentialChecked[route.CredentialRef]
			if !checked {
				credentialAvailable = credentials.Available(ctx, string(planningTarget.ID), route.CredentialRef)
				credentialChecked[route.CredentialRef] = credentialAvailable
			}
			if credentialAvailable {
				available[route.CredentialRef] = struct{}{}
			}
		}
		switch route.Kind {
		case endpoint.RouteSSHWebRTCTCP:
			if sshSupported && sshCredentialAvailable(ctx, options.SSHCredentials, route.SSHCredentialRef) {
				available[route.SSHCredentialRef] = struct{}{}
			}
		case endpoint.RouteManagedWebRTC:
			if options.ManagedPeers != nil && cloud != nil && cloud.Available(ctx, route) {
				managedSupported = true
				continue
			}
			route.Enabled = false
			planningTarget.Routes[route.ID] = route
		}
	}
	if managedSupported {
		environment.SupportedRouteKinds = append(environment.SupportedRouteKinds, endpoint.RouteManagedWebRTC)
	}
	for _, route := range planningTarget.RouteList() {
		for _, reference := range []string{route.CredentialRef, route.SSHCredentialRef} {
			if _, ok := available[reference]; ok {
				environment.AvailableCredentialRefs = append(environment.AvailableCredentialRefs, reference)
				delete(available, reference)
			}
		}
	}
	return planningTarget, environment, nil
}

func sshCredentialAvailable(ctx context.Context, source port.SSHCredentialSource, reference string) bool {
	if availability, ok := source.(contextSSHCredentialAvailability); ok {
		return availability.AvailableContext(ctx, reference)
	}
	if availability, ok := source.(sshCredentialAvailability); ok {
		return availability.Available(reference)
	}
	return false
}

func joinRouteKinds(kinds []endpoint.RouteKind) string {
	values := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		values = append(values, string(kind))
	}
	return strings.Join(values, "\x00")
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

func (source platformCredentials) Available(ctx context.Context, endpointID, reference string) bool {
	_, err := source.ResolveClientCredential(ctx, endpointID, reference)
	return err == nil
}

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

type platformManagedEligibility struct{ broker *binding.PlatformBroker }

func (eligibility platformManagedEligibility) Available(ctx context.Context, route endpoint.AccessRoute) bool {
	response, err := eligibility.broker.Exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_CloudRouteEligibility{
		CloudRouteEligibility: &bindingpb.CloudRouteEligibilityRequest{
			AccountProfileRef: route.AccountProfileRef,
			RelayMode:         managedRelayMode(route.RelayMode),
		},
	}})
	if err != nil || platformResponseError(response) != nil {
		return false
	}
	value := response.GetCloudRouteEligibility()
	if value == nil || !value.GetAccountSessionAvailable() {
		return false
	}
	switch route.RelayMode {
	case endpoint.RelayDirect:
		return value.GetManagedDirectAvailable()
	case endpoint.RelayOnly:
		return value.GetRelayAvailable()
	default:
		return value.GetManagedDirectAvailable() || value.GetRelayAvailable()
	}
}

func managedRelayMode(mode endpoint.RelayMode) remoteauthpb.ManagedWebRTCRelayMode {
	switch mode {
	case endpoint.RelayDirect:
		return remoteauthpb.ManagedWebRTCRelayMode_MANAGED_WEBRTC_RELAY_MODE_DIRECT
	case endpoint.RelayOnly:
		return remoteauthpb.ManagedWebRTCRelayMode_MANAGED_WEBRTC_RELAY_MODE_RELAY_ONLY
	case endpoint.RelaySmart:
		return remoteauthpb.ManagedWebRTCRelayMode_MANAGED_WEBRTC_RELAY_MODE_SMART_ROUTE
	default:
		return remoteauthpb.ManagedWebRTCRelayMode_MANAGED_WEBRTC_RELAY_MODE_AUTO
	}
}

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
var _ binding.EndpointShareHost = (*Host)(nil)
var _ peeradapter.CredentialSource = platformCredentials{}
var _ peeradapter.SignerSource = platformCredentials{}
var _ remoteauth.ClientAccessSigner = platformSigner{}
var _ managed.CloudClient = platformCloud{}
var _ cloudcompanion.SignalingStream = (*signalingStream)(nil)
