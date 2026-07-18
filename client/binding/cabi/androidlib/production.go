package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lozzow/termx/client/adapter/managed"
	pionadapter "github.com/lozzow/termx/client/adapter/managed/pion"
	"github.com/lozzow/termx/client/binding"
	"github.com/lozzow/termx/client/endpoint"
	clientruntime "github.com/lozzow/termx/client/runtime"
	"github.com/lozzow/termx/proto/bindingpb"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/remoteauth"
	"google.golang.org/protobuf/proto"
)

const androidBootstrapPrefix = "termx://bootstrap?payload="

// androidProductionHost 是 Android 正式 Client Engine composition root。
// endpoint/session/auth/protocol 真值留在 Go；Kotlin 仅通过 PlatformBroker 提供 Cloud、credential 和 signer primitive。
type androidProductionHost struct {
	broker    *binding.PlatformBroker
	closeOnce sync.Once
}

var androidProcessGeneration atomic.Uint64

func nextAndroidSessionGeneration() clientruntime.SessionGeneration {
	return clientruntime.SessionGeneration(androidProcessGeneration.Add(1))
}

func newAndroidProductionHost() *androidProductionHost {
	return &androidProductionHost{broker: binding.NewPlatformBroker()}
}

// OpenSession 按 bindingpb managed endpoint 配置建立新的 Go-owned generation。
// 缺失 endpoint pin、credential ref 或 platform response 时 fail closed，不读取旧 Kotlin connection state。
func (host *androidProductionHost) OpenSession(ctx context.Context, request *bindingpb.OpenSessionRequest) (clientruntime.ApplicationReadySession, error) {
	managedConfig := request.GetManaged()
	if managedConfig == nil {
		return nil, fmt.Errorf("Android managed endpoint configuration is required")
	}
	relayMode, err := androidRelayMode(managedConfig.GetRelayMode())
	if err != nil {
		return nil, err
	}
	intent, err := androidConnectIntent(request.GetIntent())
	if err != nil {
		return nil, err
	}
	endpointID := strings.TrimSpace(request.GetEndpointId())
	routeID := endpoint.RouteID(strings.TrimSpace(request.GetRouteOverride()))
	if routeID == "" {
		routeID = "managed-webrtc"
	}
	target := endpoint.Endpoint{
		ID: endpoint.EndpointID(endpointID),
		DaemonIdentity: endpoint.DaemonIdentity{
			DeviceID:          strings.TrimSpace(managedConfig.GetTargetDeviceId()),
			DeviceFingerprint: strings.TrimSpace(managedConfig.GetDeviceFingerprint()),
		},
		Routes: map[endpoint.RouteID]endpoint.AccessRoute{
			routeID: {
				ID: routeID, Kind: endpoint.RouteManagedWebRTC, Enabled: true,
				Source: endpoint.SourceCloud, PolicySource: endpoint.SourceUser,
				CredentialRef:  strings.TrimSpace(managedConfig.GetCredentialRef()),
				TargetDeviceID: strings.TrimSpace(managedConfig.GetTargetDeviceId()),
				AccountProfile: strings.TrimSpace(managedConfig.GetAccountProfile()), RelayMode: relayMode,
			},
		},
	}
	// generation 在 Android 进程内全局单调，Activity/bridge 重建不能让迟到 callback 命中新 Engine 的同代 session。
	generation := nextAndroidSessionGeneration()
	attempt, err := clientruntime.NewAttemptRequest(target, routeID, generation, intent)
	if err != nil {
		return nil, err
	}
	credentials := androidPlatformCredentials{broker: host.broker}
	dialer := &managed.Dialer{
		Cloud: androidPlatformCloud{broker: host.broker}, Peers: pionadapter.Factory{}, ClientName: "termx-android",
		Authorization: managed.CapabilityAuthorizer{Credentials: credentials, Signers: credentials},
	}
	ready, err := dialer.Dial(ctx, attempt)
	if err != nil {
		return nil, err
	}
	application, ok := ready.(clientruntime.ApplicationReadySession)
	if !ok {
		_ = ready.Close()
		return nil, fmt.Errorf("Android managed route returned no Proto application session")
	}
	return application, nil
}

// ImportPairing 验证 daemon-signed bootstrap、准备 Android signer、完成受限 PairingExchange 并原子绑定 grant。
// 配对 transport 在 grant 返回后关闭；普通 application session 必须重新拨号执行 capability handshake。
func (host *androidProductionHost) ImportPairing(ctx context.Context, request *bindingpb.ImportPairingRequest) (*bindingpb.ImportPairingResult, error) {
	payload, err := decodeAndroidBootstrap(request.GetPortablePayload())
	if err != nil {
		return nil, err
	}
	bundle, claims, err := remoteauth.ParsePairingBundle(payload, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	endpointID := strings.TrimSpace(request.GetExpectedEndpointId())
	if endpointID == "" {
		endpointID = bundle.GetIdentity().GetDeviceId()
	}
	credentialRef := androidCredentialRef(bundle.GetIdentity().GetDeviceId(), bundle.GetIdentity().GetDeviceFingerprint())
	response, err := host.broker.Exchange(ctx, &bindingpb.PlatformRequest{
		Request: &bindingpb.PlatformRequest_CredentialPrepare{CredentialPrepare: &bindingpb.CredentialPrepareRequest{
			EndpointId: endpointID, CredentialRef: credentialRef,
		}},
	})
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
	var pairingRoute endpoint.AccessRoute
	for _, route := range candidate.Routes {
		if route.Enabled && route.Kind == endpoint.RouteManagedWebRTC {
			pairingRoute = route
			break
		}
	}
	if pairingRoute.ID == "" {
		return nil, fmt.Errorf("Android pairing bundle has no managed WebRTC route")
	}
	pairingRoute.CredentialRef = credentialRef
	if pairingRoute.TargetDeviceID == "" {
		pairingRoute.TargetDeviceID = bundle.GetIdentity().GetDeviceId()
	}
	target := endpoint.Endpoint{
		ID: endpoint.EndpointID(endpointID), DaemonIdentity: candidate.Identity,
		Routes: map[endpoint.RouteID]endpoint.AccessRoute{pairingRoute.ID: pairingRoute},
	}
	generation := nextAndroidSessionGeneration()
	attempt, err := clientruntime.NewAttemptRequest(target, pairingRoute.ID, generation, clientruntime.ConnectIntentInteractive)
	if err != nil {
		return nil, err
	}
	identity := remoteauth.ClientAccessIdentity{
		EndpointID: endpointID, PublicKey: append(ed25519.PublicKey(nil), record.GetPublicKey()...),
		Fingerprint: record.GetKeyFingerprint(),
	}
	if err := identity.ValidatePublic(); err != nil {
		return nil, fmt.Errorf("Android pairing identity is invalid: %w", err)
	}
	signer := androidPlatformSigner{broker: host.broker, credentialRef: credentialRef, identity: identity}
	paired, err := (&managed.PairingDialer{
		Cloud: androidPlatformCloud{broker: host.broker}, Peers: pionadapter.Factory{},
	}).Redeem(ctx, attempt, remoteauth.ClientPairingRequest{
		ExpectedDeviceID:          bundle.GetIdentity().GetDeviceId(),
		ExpectedDeviceFingerprint: bundle.GetIdentity().GetDeviceFingerprint(),
		PairingBundle:             payload, Identity: identity, Signer: signer, ClientLabel: "termx-android",
	})
	if err != nil {
		return nil, err
	}
	boundResponse, err := host.broker.Exchange(ctx, &bindingpb.PlatformRequest{
		Request: &bindingpb.PlatformRequest_CredentialBind{CredentialBind: &bindingpb.CredentialBindRequest{
			EndpointId: endpointID, CredentialRef: credentialRef, CapabilityGrant: paired.Grant,
		}},
	})
	if err != nil {
		return nil, err
	}
	bound, err := platformCredential(boundResponse)
	if err != nil {
		return nil, err
	}
	if bound.GetKeyFingerprint() != identity.Fingerprint || bound.GetCapabilityGrant() != paired.Grant {
		return nil, fmt.Errorf("Android secure store bound a different pairing credential")
	}
	label := strings.TrimSpace(bundle.GetSuggestedLabel())
	if label == "" {
		label = bundle.GetIdentity().GetDeviceId()
	}
	return &bindingpb.ImportPairingResult{
		EndpointId: endpointID, Label: label,
		TargetDeviceId: bundle.GetIdentity().GetDeviceId(), DeviceFingerprint: bundle.GetIdentity().GetDeviceFingerprint(),
		CredentialRef: credentialRef, TicketId: claims.TicketID, ClientKeyFingerprint: record.GetKeyFingerprint(),
		ExpiresAtUnixNano: paired.ExpiresAt.UnixNano(), AuthorizationRequired: false,
	}, nil
}

// DeleteCredential 请求 Android secure store 删除本地 signer/grant record。
func (host *androidProductionHost) DeleteCredential(ctx context.Context, request *bindingpb.DeleteCredentialRequest) error {
	response, err := host.broker.Exchange(ctx, &bindingpb.PlatformRequest{
		Request: &bindingpb.PlatformRequest_CredentialDelete{CredentialDelete: &bindingpb.CredentialDeleteRequest{CredentialRef: request.GetCredentialRef()}},
	})
	if err != nil {
		return err
	}
	return platformResponseError(response)
}

func (host *androidProductionHost) close() error {
	host.closeOnce.Do(func() { _ = host.broker.Close() })
	return nil
}

type androidPlatformCredentials struct{ broker *binding.PlatformBroker }

func (source androidPlatformCredentials) ResolveClientCredential(ctx context.Context, endpointID, credentialRef string) (remoteauth.ClientAccessCredential, error) {
	response, err := source.broker.Exchange(ctx, &bindingpb.PlatformRequest{
		Request: &bindingpb.PlatformRequest_CredentialResolve{CredentialResolve: &bindingpb.CredentialResolveRequest{
			EndpointId: endpointID, CredentialRef: credentialRef,
		}},
	})
	if err != nil {
		return remoteauth.ClientAccessCredential{}, err
	}
	record, err := platformCredential(response)
	if err != nil {
		return remoteauth.ClientAccessCredential{}, err
	}
	identity := remoteauth.ClientAccessIdentity{
		EndpointID: record.GetEndpointId(), Fingerprint: record.GetKeyFingerprint(),
		PublicKey: append(ed25519.PublicKey(nil), record.GetPublicKey()...),
	}
	if err := identity.ValidatePublic(); err != nil {
		return remoteauth.ClientAccessCredential{}, err
	}
	return remoteauth.ClientAccessCredential{
		Version: 1, EndpointID: record.GetEndpointId(), Identity: identity,
		CapabilityGrant: record.GetCapabilityGrant(), UpdatedAt: time.Now().UTC(),
	}, nil
}

func (source androidPlatformCredentials) ResolveClientSigner(_ context.Context, endpointID, credentialRef string, identity remoteauth.ClientAccessIdentity) (remoteauth.ClientAccessSigner, error) {
	if identity.EndpointID != endpointID {
		return nil, fmt.Errorf("Android signer endpoint mismatch")
	}
	return androidPlatformSigner{broker: source.broker, credentialRef: credentialRef, identity: identity}, nil
}

type androidPlatformSigner struct {
	broker        *binding.PlatformBroker
	credentialRef string
	identity      remoteauth.ClientAccessIdentity
}

func (signer androidPlatformSigner) Sign(ctx context.Context, payload []byte) ([]byte, error) {
	response, err := signer.broker.Exchange(ctx, &bindingpb.PlatformRequest{
		Request: &bindingpb.PlatformRequest_CredentialSign{CredentialSign: &bindingpb.CredentialSignRequest{
			CredentialRef: signer.credentialRef, Payload: append([]byte(nil), payload...),
		}},
	})
	if err != nil {
		return nil, err
	}
	if err := platformResponseError(response); err != nil {
		return nil, err
	}
	signature := append([]byte(nil), response.GetCredentialSign().GetSignature()...)
	if !ed25519.Verify(signer.identity.PublicKey, payload, signature) {
		return nil, fmt.Errorf("Android platform signer returned an invalid signature")
	}
	return signature, nil
}

type androidPlatformCloud struct{ broker *binding.PlatformBroker }

func (cloud androidPlatformCloud) ResolveEndpoint(ctx context.Context, request *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error) {
	response, err := cloud.exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_CloudResolveEndpoint{CloudResolveEndpoint: proto.Clone(request).(*cloudpb.ResolveEndpointRequest)}})
	if err != nil {
		return nil, err
	}
	return proto.Clone(response.GetCloudResolvedEndpoint()).(*cloudpb.ResolvedEndpoint), nil
}

func (cloud androidPlatformCloud) CreateSignalingSession(ctx context.Context, request *cloudpb.CreateSignalingSessionRequest) (cloudcompanion.SignalingStream, error) {
	response, err := cloud.exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_CloudCreateSignaling{CloudCreateSignaling: proto.Clone(request).(*cloudpb.CreateSignalingSessionRequest)}})
	if err != nil {
		return nil, err
	}
	return &androidSignalingStream{events: cloneSignalingEvents(response.GetCloudSignaling().GetEvents())}, nil
}

func (cloud androidPlatformCloud) AcquireRelayLease(ctx context.Context, request *cloudpb.AcquireRelayLeaseRequest) (*cloudpb.RelayLease, error) {
	response, err := cloud.exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_CloudAcquireRelay{CloudAcquireRelay: proto.Clone(request).(*cloudpb.AcquireRelayLeaseRequest)}})
	if err != nil {
		return nil, err
	}
	return proto.Clone(response.GetCloudRelayLease()).(*cloudpb.RelayLease), nil
}

func (cloud androidPlatformCloud) PlanManagedRoute(ctx context.Context, request *cloudpb.PlanManagedRouteRequest) (*cloudpb.ManagedRoutePlan, error) {
	response, err := cloud.exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_CloudPlanRoute{CloudPlanRoute: proto.Clone(request).(*cloudpb.PlanManagedRouteRequest)}})
	if err != nil {
		return nil, err
	}
	return proto.Clone(response.GetCloudRoutePlan()).(*cloudpb.ManagedRoutePlan), nil
}

func (cloud androidPlatformCloud) ReportPathQuality(ctx context.Context, request *cloudpb.ReportPathQualityRequest) (*cloudpb.ReportPathQualityResponse, error) {
	response, err := cloud.exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_CloudReportQuality{CloudReportQuality: proto.Clone(request).(*cloudpb.ReportPathQualityRequest)}})
	if err != nil {
		return nil, err
	}
	return proto.Clone(response.GetCloudQualityReported()).(*cloudpb.ReportPathQualityResponse), nil
}

func (cloud androidPlatformCloud) ReportConnectionOutcome(ctx context.Context, request *cloudpb.ReportConnectionOutcomeRequest) (*cloudpb.ReportConnectionOutcomeResponse, error) {
	response, err := cloud.exchange(ctx, &bindingpb.PlatformRequest{Request: &bindingpb.PlatformRequest_CloudReportOutcome{CloudReportOutcome: proto.Clone(request).(*cloudpb.ReportConnectionOutcomeRequest)}})
	if err != nil {
		return nil, err
	}
	return proto.Clone(response.GetCloudOutcomeReported()).(*cloudpb.ReportConnectionOutcomeResponse), nil
}

func (cloud androidPlatformCloud) exchange(ctx context.Context, request *bindingpb.PlatformRequest) (*bindingpb.PlatformResponse, error) {
	response, err := cloud.broker.Exchange(ctx, request)
	if err != nil {
		return nil, err
	}
	if err := platformResponseError(response); err != nil {
		return nil, err
	}
	if response.GetResponse() == nil {
		return nil, fmt.Errorf("Android platform Cloud response is empty")
	}
	return response, nil
}

type androidSignalingStream struct {
	mu     sync.Mutex
	events []*cloudpb.SignalingEvent
	closed bool
}

func (stream *androidSignalingStream) Receive() (*cloudpb.SignalingEvent, error) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.closed || len(stream.events) == 0 {
		return nil, io.EOF
	}
	event := stream.events[0]
	stream.events = stream.events[1:]
	return proto.Clone(event).(*cloudpb.SignalingEvent), nil
}

func (stream *androidSignalingStream) Close() error {
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
		return nil, fmt.Errorf("Android platform credential response is incomplete")
	}
	return proto.Clone(record).(*bindingpb.CredentialRecord), nil
}

func platformResponseError(response *bindingpb.PlatformResponse) error {
	if response == nil {
		return fmt.Errorf("Android platform response is empty")
	}
	if value := response.GetError(); value != nil {
		return fmt.Errorf("Android platform request failed: %s", value.GetMessage())
	}
	return nil
}

func androidRelayMode(value bindingpb.ManagedRelayMode) (endpoint.RelayMode, error) {
	switch value {
	case bindingpb.ManagedRelayMode_MANAGED_RELAY_MODE_AUTO:
		return endpoint.RelayAuto, nil
	case bindingpb.ManagedRelayMode_MANAGED_RELAY_MODE_DIRECT:
		return endpoint.RelayDirect, nil
	case bindingpb.ManagedRelayMode_MANAGED_RELAY_MODE_RELAY_ONLY:
		return endpoint.RelayOnly, nil
	case bindingpb.ManagedRelayMode_MANAGED_RELAY_MODE_SMART_ROUTE:
		return endpoint.RelaySmart, nil
	default:
		return "", fmt.Errorf("Android managed relay mode is unsupported")
	}
}

func androidConnectIntent(value bindingpb.ConnectIntent) (clientruntime.ConnectIntent, error) {
	switch value {
	case bindingpb.ConnectIntent_CONNECT_INTENT_INTERACTIVE:
		return clientruntime.ConnectIntentInteractive, nil
	case bindingpb.ConnectIntent_CONNECT_INTENT_BACKGROUND:
		return clientruntime.ConnectIntentBackground, nil
	case bindingpb.ConnectIntent_CONNECT_INTENT_PROBE:
		return clientruntime.ConnectIntentProbe, nil
	default:
		return "", fmt.Errorf("Android connect intent is unsupported")
	}
}

func decodeAndroidBootstrap(value string) ([]byte, error) {
	encoded := strings.TrimSpace(value)
	if strings.HasPrefix(encoded, androidBootstrapPrefix) {
		encoded = strings.TrimPrefix(encoded, androidBootstrapPrefix)
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(payload) == 0 {
		return nil, fmt.Errorf("Android pairing bootstrap payload is invalid")
	}
	return payload, nil
}

func androidCredentialRef(deviceID, fingerprint string) string {
	digest := sha256.Sum256([]byte(deviceID + "\n" + fingerprint))
	return "android-access-" + base64.RawURLEncoding.EncodeToString(digest[:])
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

var _ binding.Host = (*androidProductionHost)(nil)
var _ binding.PairingHost = (*androidProductionHost)(nil)
var _ binding.CredentialHost = (*androidProductionHost)(nil)
var _ managed.CredentialSource = androidPlatformCredentials{}
var _ managed.SignerSource = androidPlatformCredentials{}
var _ remoteauth.ClientAccessSigner = androidPlatformSigner{}
var _ managed.CloudClient = androidPlatformCloud{}
var _ cloudcompanion.SignalingStream = (*androidSignalingStream)(nil)
