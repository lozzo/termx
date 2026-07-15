package connection

import (
	"crypto/ed25519"
	"fmt"
	"strings"
	"time"

	"github.com/lozzow/termx/proto/remoteauthpb"
	"github.com/lozzow/termx/shared/remoteauth"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const (
	// EndpointBootstrapBundleVersion 是 daemon-signed bootstrap bundle 的唯一当前版本。
	EndpointBootstrapBundleVersion uint32 = 2
	// ClientEndpointShareBundleVersion 是客户端到客户端 share bundle 的唯一当前版本。
	ClientEndpointShareBundleVersion uint32 = 1
	// ShareSessionOfferVersion 是静态二维码中一次性 TLS share offer 的唯一当前版本。
	ShareSessionOfferVersion uint32 = 1
	// MaxPortableContractBytes 限制二维码、近端文件和实时 share contract，防止不受信输入耗尽内存。
	MaxPortableContractBytes = 256 << 10
	// EndpointBootstrapSignatureProtocol 是 bootstrap 签名输入的跨语言 domain separator。
	EndpointBootstrapSignatureProtocol = "termx.endpoint-bootstrap.signature"
	// PairingTicketSignatureProtocol 是 pairing ticket 签名输入的跨语言 domain separator。
	PairingTicketSignatureProtocol = "termx.pairing-ticket.signature"
	// PortableSignatureVersion 是 bootstrap 与 pairing ticket canonical 签名输入的当前版本。
	PortableSignatureVersion uint32 = 1
	portableClockSkew               = 5 * time.Minute
)

// PairingTicketDescriptor 是 generated protobuf 中 daemon-local 一次性授权票据的公开部分。
// ticket 只能打开受限 pairing handshake，不能直接访问 terminal/history/file。
type PairingTicketDescriptor = remoteauthpb.PairingTicketDescriptor

// EndpointBootstrapBundleV2 是 generated deterministic protobuf bootstrap contract。
// daemon DeviceIdentity 对 canonical bytes 签名；Cloud、Hub、Relay 和 signaling 永远不得接收该消息中的授权材料。
type EndpointBootstrapBundleV2 = remoteauthpb.EndpointBootstrapBundleV2

// ClientEndpointShareBundleV1 是 generated deterministic protobuf share contract。
// 它只允许在用户确认后的实时 TLS share channel 内传输，不携带源 EndpointID、源 credential ref 或源 CapabilityGrant。
type ClientEndpointShareBundleV1 = remoteauthpb.ClientEndpointShareBundleV1

// ShareSessionOffer 是静态二维码唯一允许携带的 generated protobuf 消息。
// 二维码只能定位一次性 TLS listener，不包含 Endpoint 配置、SSH credential、Cloud token 或 CapabilityGrant。
type ShareSessionOffer = remoteauthpb.ShareSessionOffer

// PairingTicketSigningBytes 返回 DeviceIdentity 签名一次性 ticket 的 canonical protobuf bytes。
// ticket.Signature 在输入中必须为空；issuer identity 会进入签名输入，防止 ticket 被移到另一 daemon 的 bootstrap。
func PairingTicketSigningBytes(identity *remoteauthpb.EndpointDaemonIdentity, ticket *PairingTicketDescriptor) ([]byte, error) {
	if err := validateWireIdentity(identity, true); err != nil {
		return nil, fmt.Errorf("pairing ticket issuer identity: %w", err)
	}
	if err := validatePairingTicketFields(ticket, false, time.Now()); err != nil {
		return nil, err
	}
	if len(ticket.GetSignature()) != 0 {
		return nil, connectionError(ErrorConfig, "pairing ticket signature must be empty while computing signing bytes")
	}
	return pairingTicketSigningBytesUnchecked(identity, ticket)
}

// EndpointBootstrapSigningBytes 返回 DeviceIdentity 签名完整 bootstrap 的 canonical protobuf bytes。
// bundle.BundleSignature 在输入中必须为空；内部 PairingTicket 必须已由同一 DeviceIdentity 签名。
func EndpointBootstrapSigningBytes(bundle *EndpointBootstrapBundleV2) ([]byte, error) {
	if err := validateEndpointBootstrapPayload(bundle, false, time.Now()); err != nil {
		return nil, err
	}
	if len(bundle.GetBundleSignature()) != 0 {
		return nil, connectionError(ErrorConfig, "endpoint bootstrap signature must be empty while computing signing bytes")
	}
	return endpointBootstrapSigningBytesUnchecked(bundle)
}

// MarshalEndpointBootstrapBundle 校验并以 deterministic protobuf 编码 daemon bootstrap。
// 签名方应先调用 EndpointBootstrapSigningBytes 获取 canonical bytes，签名后再调用本函数输出最终 bundle。
func MarshalEndpointBootstrapBundle(bundle *EndpointBootstrapBundleV2) ([]byte, error) {
	if err := validateEndpointBootstrapBundle(bundle); err != nil {
		return nil, err
	}
	return marshalPortableContract(bundle)
}

// ParseEndpointBootstrapBundle 严格解析 daemon bootstrap。
// unknown field、超限、身份 public key/fingerprint 不一致、local route 或客户端 policy/credential 字段全部 fail closed。
func ParseEndpointBootstrapBundle(payload []byte) (*EndpointBootstrapBundleV2, error) {
	bundle := &remoteauthpb.EndpointBootstrapBundleV2{}
	if err := unmarshalPortableContract(payload, bundle); err != nil {
		return nil, err
	}
	if err := validateEndpointBootstrapBundle(bundle); err != nil {
		return nil, err
	}
	return bundle, nil
}

// MarshalClientEndpointShareBundle 校验并确定性编码客户端 share bundle。
// credential descriptor 只描述目标端需要的类别，任何源平台 ref 或 secret body 都没有 wire 字段。
func MarshalClientEndpointShareBundle(bundle *ClientEndpointShareBundleV1) ([]byte, error) {
	if err := validateClientEndpointShareBundle(bundle); err != nil {
		return nil, err
	}
	return marshalPortableContract(bundle)
}

// ParseClientEndpointShareBundle 严格解析实时 TLS channel 中的 share bundle。
// local-unix、未知字段、超限、identity 不完整和不允许导出的 credential descriptor 均被拒绝。
func ParseClientEndpointShareBundle(payload []byte) (*ClientEndpointShareBundleV1, error) {
	bundle := &remoteauthpb.ClientEndpointShareBundleV1{}
	if err := unmarshalPortableContract(payload, bundle); err != nil {
		return nil, err
	}
	if err := validateClientEndpointShareBundle(bundle); err != nil {
		return nil, err
	}
	return bundle, nil
}

// MarshalShareSessionOffer 校验并确定性编码静态二维码 offer。
// one-time secret 只用于本次 TLS listener admission，消费或过期后必须销毁。
func MarshalShareSessionOffer(offer *ShareSessionOffer) ([]byte, error) {
	if err := validateShareSessionOffer(offer); err != nil {
		return nil, err
	}
	return marshalPortableContract(offer)
}

// ParseShareSessionOffer 严格解析静态二维码 offer；unknown field、超限、空地址、空 pin 或弱 session secret 直接失败。
func ParseShareSessionOffer(payload []byte) (*ShareSessionOffer, error) {
	offer := &remoteauthpb.ShareSessionOffer{}
	if err := unmarshalPortableContract(payload, offer); err != nil {
		return nil, err
	}
	if err := validateShareSessionOffer(offer); err != nil {
		return nil, err
	}
	return offer, nil
}

func marshalPortableContract(message proto.Message) ([]byte, error) {
	if message == nil || hasUnknownFields(message.ProtoReflect()) {
		return nil, connectionError(ErrorConfig, "portable endpoint contract contains unknown fields")
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(message)
	if err != nil {
		return nil, connectionError(ErrorConfig, "encode portable endpoint contract: %v", err)
	}
	if len(payload) > MaxPortableContractBytes {
		return nil, connectionError(ErrorSizeLimit, "portable endpoint contract exceeds %d bytes", MaxPortableContractBytes)
	}
	return payload, nil
}

func unmarshalPortableContract(payload []byte, message proto.Message) error {
	if len(payload) > MaxPortableContractBytes {
		return connectionError(ErrorSizeLimit, "portable endpoint contract exceeds %d bytes", MaxPortableContractBytes)
	}
	if len(payload) == 0 {
		return connectionError(ErrorConfig, "portable endpoint contract is empty")
	}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, message); err != nil {
		return connectionError(ErrorConfig, "decode portable endpoint contract: %v", err)
	}
	if hasUnknownFields(message.ProtoReflect()) {
		return connectionError(ErrorConfig, "portable endpoint contract contains unknown fields")
	}
	return nil
}

func validateEndpointBootstrapBundle(bundle *remoteauthpb.EndpointBootstrapBundleV2) error {
	if err := validateEndpointBootstrapPayload(bundle, true, time.Now()); err != nil {
		return err
	}
	signingBytes, err := endpointBootstrapSigningBytesUnchecked(bundle)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(bundle.GetIdentity().GetDevicePublicKey()), signingBytes, bundle.GetBundleSignature()) {
		return connectionError(ErrorIdentityConflict, "endpoint bootstrap signature does not match daemon identity")
	}
	return nil
}

func validateEndpointBootstrapPayload(bundle *remoteauthpb.EndpointBootstrapBundleV2, requireBundleSignature bool, now time.Time) error {
	if bundle == nil || bundle.GetSchemaVersion() != EndpointBootstrapBundleVersion {
		return connectionError(ErrorUnsupportedVersion, "endpoint bootstrap version or bundle_id is invalid")
	}
	if err := validateIdentifier("endpoint bootstrap bundle", bundle.GetBundleId()); err != nil {
		return connectionError(ErrorConfig, "endpoint bootstrap bundle_id is invalid")
	}
	if err := validateWireIdentity(bundle.GetIdentity(), true); err != nil {
		return fmt.Errorf("endpoint bootstrap identity: %w", err)
	}
	if bundle.GetIssuedAtUnixNano() <= 0 || bundle.GetExpiresAtUnixNano() <= bundle.GetIssuedAtUnixNano() ||
		bundle.GetExpiresAtUnixNano() <= now.UnixNano() || bundle.GetIssuedAtUnixNano() > now.Add(portableClockSkew).UnixNano() {
		return connectionError(ErrorConfig, "endpoint bootstrap timestamps or signature are invalid")
	}
	if requireBundleSignature && len(bundle.GetBundleSignature()) != ed25519.SignatureSize {
		return connectionError(ErrorConfig, "endpoint bootstrap signature is invalid")
	}
	if !requireBundleSignature && len(bundle.GetBundleSignature()) != 0 {
		return connectionError(ErrorConfig, "endpoint bootstrap signature must be empty while signing")
	}
	seenRoutes := make(map[string]struct{}, len(bundle.GetRoutes()))
	for _, route := range bundle.GetRoutes() {
		if err := validatePortableRoute(route, bundle.GetIdentity(), false); err != nil {
			return fmt.Errorf("endpoint bootstrap route: %w", err)
		}
		if _, duplicate := seenRoutes[route.GetRouteId()]; duplicate {
			return connectionError(ErrorRouteConflict, "endpoint bootstrap repeats route_id %q", route.GetRouteId())
		}
		seenRoutes[route.GetRouteId()] = struct{}{}
	}
	authorization := bundle.GetAuthorization()
	if authorization == nil || authorization.GetPayload() == nil {
		return connectionError(ErrorConfig, "endpoint bootstrap requires an authorization bootstrap")
	}
	if ticket := authorization.GetPairingTicket(); ticket != nil {
		if err := validatePairingTicketFields(ticket, true, now); err != nil || ticket.GetExpiresAtUnixNano() <= bundle.GetIssuedAtUnixNano() || ticket.GetExpiresAtUnixNano() > bundle.GetExpiresAtUnixNano() {
			return connectionError(ErrorConfig, "pairing ticket is invalid")
		}
		signingBytes, err := pairingTicketSigningBytesUnchecked(bundle.GetIdentity(), ticket)
		if err != nil {
			return err
		}
		if !ed25519.Verify(ed25519.PublicKey(bundle.GetIdentity().GetDevicePublicKey()), signingBytes, ticket.GetSignature()) {
			return connectionError(ErrorIdentityConflict, "pairing ticket signature does not match daemon identity")
		}
	} else if len(authorization.GetBoundGrant()) == 0 {
		return connectionError(ErrorConfig, "authorization bootstrap is empty")
	} else {
		// CONN002 验证 grant subject 与接收方 private-key possession 前，禁止把 opaque bytes 降级成 bearer grant。
		return connectionError(ErrorAuthorizationRequired, "bound capability grant verification is not available")
	}
	return nil
}

func validateClientEndpointShareBundle(bundle *remoteauthpb.ClientEndpointShareBundleV1) error {
	if bundle == nil || bundle.GetSchemaVersion() != ClientEndpointShareBundleVersion {
		return connectionError(ErrorUnsupportedVersion, "client endpoint share version or transfer_id is invalid")
	}
	if err := validateIdentifier("client endpoint share transfer", bundle.GetTransferId()); err != nil {
		return connectionError(ErrorConfig, "client endpoint share transfer_id is invalid")
	}
	if err := validateWireIdentity(bundle.GetIdentity(), false); err != nil {
		return fmt.Errorf("client endpoint share identity: %w", err)
	}
	now := time.Now()
	if bundle.GetIssuedAtUnixNano() <= 0 || bundle.GetExpiresAtUnixNano() <= bundle.GetIssuedAtUnixNano() ||
		bundle.GetExpiresAtUnixNano() <= now.UnixNano() || bundle.GetIssuedAtUnixNano() > now.Add(portableClockSkew).UnixNano() {
		return connectionError(ErrorConfig, "client endpoint share timestamps are invalid")
	}
	switch bundle.GetConnectMode() {
	case remoteauthpb.EndpointConnectMode_ENDPOINT_CONNECT_MODE_AUTO,
		remoteauthpb.EndpointConnectMode_ENDPOINT_CONNECT_MODE_ON_DEMAND,
		remoteauthpb.EndpointConnectMode_ENDPOINT_CONNECT_MODE_MANUAL:
	default:
		return connectionError(ErrorConfig, "client endpoint share connect_mode is invalid")
	}
	if policy := bundle.GetSelectionPolicy(); policy != nil {
		if (policy.GetHedgeDelayConfigured() && policy.GetHedgeDelayMillis() > 30_000) || (!policy.GetHedgeDelayConfigured() && policy.GetHedgeDelayMillis() != 0) {
			return connectionError(ErrorConfig, "client endpoint share hedge delay is invalid")
		}
	}
	seenRoutes := make(map[string]struct{}, len(bundle.GetRoutes()))
	for _, route := range bundle.GetRoutes() {
		if err := validatePortableRoute(route, bundle.GetIdentity(), true); err != nil {
			return fmt.Errorf("client endpoint share route: %w", err)
		}
		if _, duplicate := seenRoutes[route.GetRouteId()]; duplicate {
			return connectionError(ErrorRouteConflict, "client endpoint share repeats route_id %q", route.GetRouteId())
		}
		seenRoutes[route.GetRouteId()] = struct{}{}
	}
	seenDescriptors := make(map[string]*remoteauthpb.EndpointCredentialDescriptor, len(bundle.GetCredentialDescriptors()))
	for _, descriptor := range bundle.GetCredentialDescriptors() {
		if descriptor == nil || validateIdentifier("credential descriptor", descriptor.GetDescriptorId()) != nil {
			return connectionError(ErrorConfig, "credential descriptor id is required")
		}
		switch descriptor.GetKind() {
		case remoteauthpb.EndpointCredentialKind_ENDPOINT_CREDENTIAL_KIND_SSH_PRIVATE_KEY,
			remoteauthpb.EndpointCredentialKind_ENDPOINT_CREDENTIAL_KIND_SSH_PASSWORD:
		case remoteauthpb.EndpointCredentialKind_ENDPOINT_CREDENTIAL_KIND_SSH_AGENT,
			remoteauthpb.EndpointCredentialKind_ENDPOINT_CREDENTIAL_KIND_CAPABILITY_GRANT,
			remoteauthpb.EndpointCredentialKind_ENDPOINT_CREDENTIAL_KIND_CLOUD_PROFILE:
			if descriptor.GetExportable() {
				return connectionError(ErrorConfig, "credential descriptor %q cannot be exportable", descriptor.GetDescriptorId())
			}
		default:
			return connectionError(ErrorConfig, "credential descriptor %q has unknown kind", descriptor.GetDescriptorId())
		}
		if existing, duplicate := seenDescriptors[descriptor.GetDescriptorId()]; duplicate {
			if existing.GetKind() != descriptor.GetKind() || existing.GetExportable() != descriptor.GetExportable() {
				return connectionError(ErrorConfig, "credential descriptor %q is defined inconsistently", descriptor.GetDescriptorId())
			}
			return connectionError(ErrorConfig, "credential descriptor %q is repeated", descriptor.GetDescriptorId())
		}
		seenDescriptors[descriptor.GetDescriptorId()] = descriptor
	}
	if len(bundle.GetBoundGrant()) != 0 {
		// share listener 在 CONN002 前只能迁移配置；目标 client-bound grant 必须由后续 verifier 验证 subject。
		return connectionError(ErrorAuthorizationRequired, "shared bound capability grant verification is not available")
	}
	return nil
}

func validateShareSessionOffer(offer *remoteauthpb.ShareSessionOffer) error {
	if offer == nil || offer.GetSchemaVersion() != ShareSessionOfferVersion {
		return connectionError(ErrorUnsupportedVersion, "share session offer version or transfer_id is invalid")
	}
	if err := validateIdentifier("share session transfer", offer.GetTransferId()); err != nil {
		return connectionError(ErrorConfig, "share session offer transfer_id is invalid")
	}
	if len(offer.GetListenerAddresses()) == 0 || strings.TrimSpace(offer.GetEphemeralCertificateSha256()) == "" || len(offer.GetOneTimeSessionSecret()) < 32 || offer.GetExpiresAtUnixNano() <= time.Now().UnixNano() {
		return connectionError(ErrorConfig, "share session offer is incomplete")
	}
	seenAddresses := make(map[string]struct{}, len(offer.GetListenerAddresses()))
	for _, address := range offer.GetListenerAddresses() {
		if address != strings.TrimSpace(address) || address == "" || strings.ContainsAny(address, "\r\n") {
			return connectionError(ErrorConfig, "share session offer address is invalid")
		}
		if _, duplicate := seenAddresses[address]; duplicate {
			return connectionError(ErrorConfig, "share session offer repeats listener address")
		}
		seenAddresses[address] = struct{}{}
	}
	return nil
}

func validateWireIdentity(identity *remoteauthpb.EndpointDaemonIdentity, requirePublicKey bool) error {
	if identity == nil {
		return connectionError(ErrorConfig, "daemon identity is required")
	}
	model := DaemonIdentity{DeviceID: identity.GetDeviceId(), DeviceFingerprint: identity.GetDeviceFingerprint()}
	if err := model.Validate(true); err != nil {
		return err
	}
	if len(identity.GetDevicePublicKey()) == 0 && !requirePublicKey {
		return nil
	}
	if len(identity.GetDevicePublicKey()) != ed25519.PublicKeySize || remoteauth.Fingerprint(ed25519.PublicKey(identity.GetDevicePublicKey())) != model.DeviceFingerprint {
		return connectionError(ErrorIdentityConflict, "daemon public key does not match device_fingerprint")
	}
	return nil
}

func validatePortableRoute(route *remoteauthpb.EndpointAccessRoute, identity *remoteauthpb.EndpointDaemonIdentity, allowPolicy bool) error {
	if route == nil || validateIdentifier("portable route", route.GetRouteId()) != nil {
		return connectionError(ErrorConfig, "portable route_id is required")
	}
	if route.GetKind() == remoteauthpb.EndpointRouteKind_ENDPOINT_ROUTE_KIND_LOCAL_UNIX {
		return connectionError(ErrorConfig, "local-unix route is not portable")
	}
	if route.GetCredentialRef() != "" {
		return connectionError(ErrorConfig, "portable route cannot contain a source credential_ref")
	}
	if route.GetSource() != remoteauthpb.EndpointSource_ENDPOINT_SOURCE_UNSPECIFIED || route.GetPolicySource() != remoteauthpb.EndpointSource_ENDPOINT_SOURCE_UNSPECIFIED {
		return connectionError(ErrorConfig, "portable route cannot claim source provenance")
	}
	if !allowPolicy && (route.GetManualOnly() || route.Priority != nil) {
		return connectionError(ErrorConfig, "daemon bootstrap cannot contain client route policy")
	}
	model, err := accessRouteFromWire(route, !allowPolicy || route.GetEnabled())
	if err != nil {
		return err
	}
	return model.Validate(DaemonIdentity{DeviceID: identity.GetDeviceId(), DeviceFingerprint: identity.GetDeviceFingerprint()})
}

func validatePairingTicketFields(ticket *remoteauthpb.PairingTicketDescriptor, requireSignature bool, now time.Time) error {
	if ticket == nil || validateIdentifier("pairing ticket", ticket.GetTicketId()) != nil ||
		len(ticket.GetScopeCeiling()) == 0 || ticket.GetExpiresAtUnixNano() <= now.UnixNano() || len(ticket.GetNonce()) < 16 || ticket.GetMaxRedemptions() != 1 {
		return connectionError(ErrorConfig, "pairing ticket is invalid")
	}
	seenScopes := make(map[string]struct{}, len(ticket.GetScopeCeiling()))
	for _, scope := range ticket.GetScopeCeiling() {
		if scope != strings.TrimSpace(scope) || scope == "" || strings.ContainsAny(scope, "\r\n") {
			return connectionError(ErrorConfig, "pairing ticket scope is invalid")
		}
		if _, duplicate := seenScopes[scope]; duplicate {
			return connectionError(ErrorConfig, "pairing ticket scope is repeated")
		}
		seenScopes[scope] = struct{}{}
	}
	if requireSignature && len(ticket.GetSignature()) != ed25519.SignatureSize {
		return connectionError(ErrorConfig, "pairing ticket signature is invalid")
	}
	if !requireSignature && len(ticket.GetSignature()) != 0 {
		return connectionError(ErrorConfig, "pairing ticket signature must be empty while signing")
	}
	return nil
}

func pairingTicketSigningBytesUnchecked(identity *remoteauthpb.EndpointDaemonIdentity, ticket *remoteauthpb.PairingTicketDescriptor) ([]byte, error) {
	cloned := proto.Clone(ticket).(*remoteauthpb.PairingTicketDescriptor)
	cloned.Signature = nil
	return marshalPortableContract(&remoteauthpb.PairingTicketSignatureInput{
		Protocol: PairingTicketSignatureProtocol, Version: PortableSignatureVersion,
		IssuerDeviceId: identity.GetDeviceId(), IssuerDeviceFingerprint: identity.GetDeviceFingerprint(), Ticket: cloned,
	})
}

func endpointBootstrapSigningBytesUnchecked(bundle *remoteauthpb.EndpointBootstrapBundleV2) ([]byte, error) {
	cloned := proto.Clone(bundle).(*remoteauthpb.EndpointBootstrapBundleV2)
	cloned.BundleSignature = nil
	return marshalPortableContract(&remoteauthpb.EndpointBootstrapSignatureInput{
		Protocol: EndpointBootstrapSignatureProtocol, Version: PortableSignatureVersion, Bundle: cloned,
	})
}

func accessRouteFromWire(route *remoteauthpb.EndpointAccessRoute, enabled bool) (AccessRoute, error) {
	kind := mapWireRouteKind(route.GetKind())
	if kind == "" {
		return AccessRoute{}, connectionError(ErrorConfig, "portable route has unknown kind")
	}
	model := AccessRoute{
		ID: RouteID(strings.TrimSpace(route.GetRouteId())), Kind: kind, Enabled: enabled, ManualOnly: route.GetManualOnly(),
		CredentialRef: "", Source: SourceShare, PolicySource: SourceShare, Socket: route.GetSocket(), Host: route.GetHost(),
		Port: uint16(route.GetPort()), User: route.GetUser(), ProxyJump: route.GetProxyJump(), RemoteSocket: route.GetRemoteSocket(),
		HostKeyFingerprints: append([]string(nil), route.GetHostKeyFingerprints()...), Addresses: append([]string(nil), route.GetAddresses()...),
		ServerName: route.GetServerName(), TargetDeviceID: route.GetTargetDeviceId(), AccountProfile: route.GetAccountProfile(),
	}
	if kind == RouteManagedWebRTC {
		model.RelayMode = mapWireRelayMode(route.GetRelayMode())
	}
	if route.GetPort() > 65535 {
		return AccessRoute{}, connectionError(ErrorConfig, "portable route port is invalid")
	}
	if route.Priority != nil {
		priority := int(route.GetPriority())
		model.Priority = &priority
	}
	return model, nil
}

func hasUnknownFields(message protoreflect.Message) bool {
	if len(message.GetUnknown()) != 0 {
		return true
	}
	found := false
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		switch {
		case field.IsList() && field.Message() != nil:
			list := value.List()
			for index := 0; index < list.Len(); index++ {
				if hasUnknownFields(list.Get(index).Message()) {
					found = true
					return false
				}
			}
		case field.IsMap() && field.MapValue().Message() != nil:
			value.Map().Range(func(_ protoreflect.MapKey, item protoreflect.Value) bool {
				if hasUnknownFields(item.Message()) {
					found = true
					return false
				}
				return true
			})
		case field.Message() != nil:
			found = hasUnknownFields(value.Message())
		}
		return !found
	})
	return found
}

func mapWireRouteKind(kind remoteauthpb.EndpointRouteKind) RouteKind {
	switch kind {
	case remoteauthpb.EndpointRouteKind_ENDPOINT_ROUTE_KIND_SSH_STDIO:
		return RouteSSHStdio
	case remoteauthpb.EndpointRouteKind_ENDPOINT_ROUTE_KIND_DIRECT_TLS:
		return RouteDirectTLS
	case remoteauthpb.EndpointRouteKind_ENDPOINT_ROUTE_KIND_MANAGED_WEBRTC:
		return RouteManagedWebRTC
	default:
		return ""
	}
}

func mapWireRelayMode(mode remoteauthpb.EndpointRelayMode) RelayMode {
	switch mode {
	case remoteauthpb.EndpointRelayMode_ENDPOINT_RELAY_MODE_UNSPECIFIED,
		remoteauthpb.EndpointRelayMode_ENDPOINT_RELAY_MODE_AUTO:
		return RelayAuto
	case remoteauthpb.EndpointRelayMode_ENDPOINT_RELAY_MODE_DIRECT:
		return RelayDirect
	case remoteauthpb.EndpointRelayMode_ENDPOINT_RELAY_MODE_RELAY_ONLY:
		return RelayOnly
	case remoteauthpb.EndpointRelayMode_ENDPOINT_RELAY_MODE_SMART_ROUTE:
		return RelaySmart
	default:
		return ""
	}
}
