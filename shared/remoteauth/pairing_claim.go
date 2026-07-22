package remoteauth

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	endpointdomain "github.com/muxvia/muxvia/client/endpoint"
	"github.com/muxvia/muxvia/proto/remoteauthpb"
	"google.golang.org/protobuf/proto"
)

const (
	// PairingClaimOfferVersion 是二维码和手工输入使用的紧凑 claim schema 版本。
	PairingClaimOfferVersion  uint32 = 1
	pairingClaimBytes                = 16
	maxPairingClaimOfferBytes        = 512
	// PairingClaimCodePrefix 标识可以直接粘贴到无摄像头客户端的 portable claim code。
	PairingClaimCodePrefix = "MXP1-"
)

var (
	// ErrPairingClaimMalformed 表示 claim offer 不是 canonical protobuf，或缺少身份、路由和 128-bit claim。
	ErrPairingClaimMalformed = errors.New("remote pairing claim malformed")
	// ErrPairingClaimUnavailable 表示 claim 不属于当前 daemon 进程，常见于错误 daemon 或 daemon 重启后。
	ErrPairingClaimUnavailable = errors.New("remote pairing claim unavailable")
)

// PairingClaimIssueResult 是 daemon 签发短码时返回给 API Layer 的完整本地结果。
// OfferPayload 可以进入二维码；BundlePayload 只留在 daemon 内存，直到端到端 PairingAccepted 返回给目标客户端。
type PairingClaimIssueResult struct {
	Offer         *remoteauthpb.PairingClaimOfferV1
	OfferPayload  []byte
	ClaimCode     string
	BundlePayload []byte
	Claims        PairingTicketClaims
}

type storedPairingClaim struct {
	BundlePayload         []byte
	ExpiresAt             time.Time
	SubjectKeyFingerprint string
}

// IssuePairingClaim 原子签发持久 PairingTicket，并仅在当前 daemon 内存登记对应 128-bit claim。
// daemon 重启只使尚未兑换的 claim 失效，不改变已经持久化的 grant、撤销或 ticket 消费真值。
func (store *AccessStore) IssuePairingClaim(options PairingIssueOptions) (PairingClaimIssueResult, error) {
	if store == nil {
		return PairingClaimIssueResult{}, fmt.Errorf("client access store is nil")
	}
	if pairingClaimRouteSeed(options.Routes) == nil {
		return PairingClaimIssueResult{}, fmt.Errorf("%w: claim requires one Direct or Cloud pairing Route", ErrPairingClaimMalformed)
	}
	randomSource := options.Random
	if randomSource == nil {
		randomSource = store.random
	}
	claim, digest, err := store.reservePairingClaim(randomSource)
	if err != nil {
		return PairingClaimIssueResult{}, err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		store.pairingClaimsMu.Lock()
		delete(store.pairingClaims, digest)
		store.pairingClaimsMu.Unlock()
	}()

	bundle, claims, err := store.IssuePairingBundle(options)
	if err != nil {
		return PairingClaimIssueResult{}, err
	}
	bundlePayload, err := EncodePairingBundle(bundle)
	if err != nil {
		return PairingClaimIssueResult{}, err
	}
	offer := &remoteauthpb.PairingClaimOfferV1{
		SchemaVersion:     PairingClaimOfferVersion,
		Claim:             append([]byte(nil), claim...),
		DeviceId:          store.identity.DeviceID,
		DevicePublicKey:   append([]byte(nil), store.identity.PublicKey...),
		ExpiresAtUnixNano: claims.ExpiresAt.UnixNano(),
		Route:             pairingClaimRouteSeed(bundle.GetRoutes()),
	}
	offerPayload, err := EncodePairingClaimOffer(offer)
	if err != nil {
		return PairingClaimIssueResult{}, err
	}
	store.pairingClaimsMu.Lock()
	store.pairingClaims[digest] = storedPairingClaim{BundlePayload: append([]byte(nil), bundlePayload...), ExpiresAt: claims.ExpiresAt}
	store.pairingClaimsMu.Unlock()
	committed = true
	return PairingClaimIssueResult{
		Offer: offer, OfferPayload: offerPayload, ClaimCode: EncodePairingClaimCode(offerPayload),
		BundlePayload: bundlePayload, Claims: claims,
	}, nil
}

func (store *AccessStore) reservePairingClaim(randomSource io.Reader) ([]byte, [sha256.Size]byte, error) {
	if randomSource == nil {
		return nil, [sha256.Size]byte{}, fmt.Errorf("generate pairing claim: random source is nil")
	}
	for attempt := 0; attempt < 8; attempt++ {
		claim := make([]byte, pairingClaimBytes)
		if _, err := io.ReadFull(randomSource, claim); err != nil {
			return nil, [sha256.Size]byte{}, fmt.Errorf("generate pairing claim: %w", err)
		}
		digest := sha256.Sum256(claim)
		store.pairingClaimsMu.Lock()
		if store.pairingClaims == nil {
			store.pairingClaims = make(map[[sha256.Size]byte]storedPairingClaim)
		}
		if _, exists := store.pairingClaims[digest]; !exists {
			store.pairingClaims[digest] = storedPairingClaim{}
			store.pairingClaimsMu.Unlock()
			return claim, digest, nil
		}
		store.pairingClaimsMu.Unlock()
	}
	return nil, [sha256.Size]byte{}, fmt.Errorf("generate pairing claim: collision limit reached")
}

// ResolvePairingClaimForExchange 校验 offer 归属和客户端绑定，并返回 daemon 内存中的完整签名 bundle。
// 过期后仅允许已经成功绑定的同一客户端在 delivery grace 内恢复丢失响应。
func (store *AccessStore) ResolvePairingClaimForExchange(payload []byte, clientPublicKey ed25519.PublicKey, now time.Time) ([]byte, error) {
	if store == nil || len(clientPublicKey) != ed25519.PublicKeySize {
		return nil, ErrPairingClaimMalformed
	}
	offer, err := ParsePairingClaimOfferForExchange(payload)
	if err != nil {
		return nil, err
	}
	if offer.GetDeviceId() != store.identity.DeviceID || !bytes.Equal(offer.GetDevicePublicKey(), store.identity.PublicKey) {
		return nil, ErrPairingClaimUnavailable
	}
	if now.IsZero() {
		now = store.now().UTC()
	}
	digest := sha256.Sum256(offer.GetClaim())
	subject := Fingerprint(clientPublicKey)
	store.pairingClaimsMu.Lock()
	defer store.pairingClaimsMu.Unlock()
	store.compactPairingClaimsLocked(now)
	record, exists := store.pairingClaims[digest]
	if !exists || len(record.BundlePayload) == 0 {
		return nil, ErrPairingClaimUnavailable
	}
	if record.SubjectKeyFingerprint != "" && record.SubjectKeyFingerprint != subject {
		return nil, ErrPairingTicketConsumed
	}
	if now.After(record.ExpiresAt) && record.SubjectKeyFingerprint == "" {
		return nil, ErrPairingTicketExpired
	}
	return append([]byte(nil), record.BundlePayload...), nil
}

// RedeemPairingClaim 复用 AccessStore 的 PairingTicket 原子消费事务，并在成功后把内存 claim 固定到同一客户端 key。
func (store *AccessStore) RedeemPairingClaim(offerPayload []byte, clientPublicKey ed25519.PublicKey, clientLabel string, now time.Time) (PairingExchangeResult, []byte, error) {
	bundlePayload, err := store.ResolvePairingClaimForExchange(offerPayload, clientPublicKey, now)
	if err != nil {
		return PairingExchangeResult{}, nil, err
	}
	result, err := store.RedeemPairingBundle(bundlePayload, clientPublicKey, clientLabel, now)
	if err != nil {
		return PairingExchangeResult{}, nil, err
	}
	offer, err := ParsePairingClaimOfferForExchange(offerPayload)
	if err != nil {
		return PairingExchangeResult{}, nil, err
	}
	digest := sha256.Sum256(offer.GetClaim())
	store.pairingClaimsMu.Lock()
	record := store.pairingClaims[digest]
	record.SubjectKeyFingerprint = result.SubjectKeyFingerprint
	store.pairingClaims[digest] = record
	store.pairingClaimsMu.Unlock()
	return result, bundlePayload, nil
}

func (store *AccessStore) compactPairingClaimsLocked(now time.Time) {
	for digest, record := range store.pairingClaims {
		if !record.ExpiresAt.IsZero() && now.After(record.ExpiresAt.Add(defaultDeliveryGrace)) {
			delete(store.pairingClaims, digest)
		}
	}
}

// EncodePairingClaimOffer 严格校验并确定性编码二维码 claim；输出不得包含 bundle、grant、scope 或 credential。
func EncodePairingClaimOffer(offer *remoteauthpb.PairingClaimOfferV1) ([]byte, error) {
	if err := validatePairingClaimOffer(offer, time.Time{}, false); err != nil {
		return nil, err
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(offer)
	if err != nil || len(payload) > maxPairingClaimOfferBytes {
		return nil, fmt.Errorf("%w: encoded offer is invalid", ErrPairingClaimMalformed)
	}
	return payload, nil
}

// ParsePairingClaimOffer 校验 canonical offer 和当前有效期，供二维码导入前建立临时 Endpoint pin 与 pairing Route。
func ParsePairingClaimOffer(payload []byte, now time.Time) (*remoteauthpb.PairingClaimOfferV1, error) {
	offer, err := parsePairingClaimOffer(payload)
	if err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := validatePairingClaimOffer(offer, now, true); err != nil {
		return nil, err
	}
	return offer, nil
}

// ParsePairingClaimOfferForExchange 校验 canonical offer，但把过期后的同 key 响应恢复交给 owning AccessStore 判断。
func ParsePairingClaimOfferForExchange(payload []byte) (*remoteauthpb.PairingClaimOfferV1, error) {
	offer, err := parsePairingClaimOffer(payload)
	if err != nil {
		return nil, err
	}
	if err := validatePairingClaimOffer(offer, time.Time{}, false); err != nil {
		return nil, err
	}
	return offer, nil
}

func parsePairingClaimOffer(payload []byte) (*remoteauthpb.PairingClaimOfferV1, error) {
	if len(payload) == 0 || len(payload) > maxPairingClaimOfferBytes {
		return nil, ErrPairingClaimMalformed
	}
	offer := &remoteauthpb.PairingClaimOfferV1{}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, offer); err != nil || len(offer.ProtoReflect().GetUnknown()) != 0 {
		return nil, ErrPairingClaimMalformed
	}
	canonical, err := (proto.MarshalOptions{Deterministic: true}).Marshal(offer)
	if err != nil || !bytes.Equal(payload, canonical) {
		return nil, ErrPairingClaimMalformed
	}
	return offer, nil
}

func validatePairingClaimOffer(offer *remoteauthpb.PairingClaimOfferV1, now time.Time, requireFresh bool) error {
	if offer == nil || offer.GetSchemaVersion() != PairingClaimOfferVersion || len(offer.GetClaim()) != pairingClaimBytes || strings.TrimSpace(offer.GetDeviceId()) == "" || len(offer.GetDevicePublicKey()) != ed25519.PublicKeySize || offer.GetExpiresAtUnixNano() <= 0 {
		return ErrPairingClaimMalformed
	}
	if offer.GetRoute() == nil {
		return ErrPairingClaimMalformed
	}
	switch route := offer.GetRoute().GetRoute().(type) {
	case *remoteauthpb.PairingRouteSeed_DirectWebrtcTcp:
		if route.DirectWebrtcTcp == nil || strings.TrimSpace(route.DirectWebrtcTcp.GetSignalingAddress()) == "" || strings.TrimSpace(route.DirectWebrtcTcp.GetIceTcpAddress()) == "" {
			return ErrPairingClaimMalformed
		}
	case *remoteauthpb.PairingRouteSeed_ManagedWebrtc:
		if route.ManagedWebrtc == nil || strings.TrimSpace(route.ManagedWebrtc.GetTargetDeviceId()) != strings.TrimSpace(offer.GetDeviceId()) {
			return ErrPairingClaimMalformed
		}
	default:
		return ErrPairingClaimMalformed
	}
	if requireFresh && now.After(time.Unix(0, offer.GetExpiresAtUnixNano()).UTC()) {
		return ErrPairingTicketExpired
	}
	return nil
}

func pairingClaimRouteSeed(routes []*remoteauthpb.EndpointRouteConfigV1) *remoteauthpb.PairingRouteSeed {
	for _, route := range routes {
		if route == nil || !route.GetEnabled() {
			continue
		}
		if direct := route.GetDirectWebrtcTcp(); direct != nil && len(direct.GetSignalingAddresses()) > 0 && len(direct.GetIceTcpAddresses()) > 0 {
			return &remoteauthpb.PairingRouteSeed{Route: &remoteauthpb.PairingRouteSeed_DirectWebrtcTcp{DirectWebrtcTcp: &remoteauthpb.PairingDirectRouteSeed{
				SignalingAddress: direct.GetSignalingAddresses()[0], IceTcpAddress: direct.GetIceTcpAddresses()[0], ServerName: direct.GetServerName(),
			}}}
		}
		if managed := route.GetManagedWebrtc(); managed != nil && strings.TrimSpace(managed.GetTargetDeviceId()) != "" {
			return &remoteauthpb.PairingRouteSeed{Route: &remoteauthpb.PairingRouteSeed_ManagedWebrtc{ManagedWebrtc: &remoteauthpb.PairingManagedRouteSeed{TargetDeviceId: managed.GetTargetDeviceId()}}}
		}
	}
	return nil
}

// PairingClaimEndpointCandidate 把紧凑 offer 投影为仅用于建立 pairing peer 的临时 Endpoint candidate。
// 完整 label、Route 列表和授权配置必须以 PairingAccepted 返回的签名 bundle 为准。
func PairingClaimEndpointCandidate(offer *remoteauthpb.PairingClaimOfferV1) (endpointdomain.EndpointCandidate, error) {
	if err := validatePairingClaimOffer(offer, time.Time{}, false); err != nil {
		return endpointdomain.EndpointCandidate{}, err
	}
	identity := endpointdomain.DaemonIdentity{DeviceID: offer.GetDeviceId(), DeviceFingerprint: Fingerprint(ed25519.PublicKey(offer.GetDevicePublicKey()))}
	var route endpointdomain.AccessRoute
	switch seed := offer.GetRoute().GetRoute().(type) {
	case *remoteauthpb.PairingRouteSeed_DirectWebrtcTcp:
		route = endpointdomain.AccessRoute{ID: "pairing-direct", Kind: endpointdomain.RouteDirectWebRTCTCP, Enabled: true, Source: endpointdomain.SourceBootstrap, PolicySource: endpointdomain.SourceUser, SignalingAddresses: []string{seed.DirectWebrtcTcp.GetSignalingAddress()}, ICETCPAddresses: []string{seed.DirectWebrtcTcp.GetIceTcpAddress()}, ServerName: seed.DirectWebrtcTcp.GetServerName()}
	case *remoteauthpb.PairingRouteSeed_ManagedWebrtc:
		route = endpointdomain.AccessRoute{ID: "pairing-cloud", Kind: endpointdomain.RouteManagedWebRTC, Enabled: true, Source: endpointdomain.SourceCloud, PolicySource: endpointdomain.SourceUser, TargetDeviceID: seed.ManagedWebrtc.GetTargetDeviceId(), AccountProfileRef: "default", RelayMode: endpointdomain.RelayAuto}
	default:
		return endpointdomain.EndpointCandidate{}, ErrPairingClaimMalformed
	}
	return endpointdomain.EndpointCandidate{Source: endpointdomain.SourceBootstrap, Identity: identity, SuggestedLabel: offer.GetDeviceId(), Routes: []endpointdomain.AccessRoute{route}}, nil
}

// EncodePairingClaimCode 把 canonical offer 编码为相机扫码和手工输入共用的紧凑文本。
func EncodePairingClaimCode(payload []byte) string {
	return PairingClaimCodePrefix + base64.RawURLEncoding.EncodeToString(payload)
}

// DecodePairingClaimCode 解码 portable claim code；canonical protobuf 和有效期仍由 ParsePairingClaimOffer 校验。
func DecodePairingClaimCode(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, PairingClaimCodePrefix) {
		return nil, ErrPairingClaimMalformed
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, PairingClaimCodePrefix))
	if err != nil || len(payload) == 0 {
		return nil, ErrPairingClaimMalformed
	}
	return payload, nil
}
