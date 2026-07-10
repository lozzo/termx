package servicecredential

import (
	"fmt"
	"time"
)

const (
	relayLeaseVersion = 1
	maxRelayLeaseTTL  = 10 * time.Minute
)

// RelayPathKind 是 Relay lease 允许的数据路径形态。
// v1 只允许单 Relay 或双 Edge Relay Mesh，不能表达任意 N 跳路径。
type RelayPathKind string

const (
	// RelayPathSingle 允许一个 region-specific managed Relay。
	RelayPathSingle RelayPathKind = "single_relay"
	// RelayPathMesh 允许 lease 指定的两个 Edge Relay 和受限 backbone route。
	RelayPathMesh RelayPathKind = "relay_mesh"
)

// RelayLeaseClaims 是 Control Plane entitlement service 签名的短期 Relay 准入声明。
// Claims 绑定 managed session、双方设备、region、route 和配额，不包含 terminal scope 或 CapabilityGrant。
type RelayLeaseClaims struct {
	Version             uint32        `json:"version"`
	KeyID               string        `json:"key_id"`
	LeaseID             string        `json:"lease_id"`
	Issuer              string        `json:"issuer"`
	AudienceRelayPool   string        `json:"audience_relay_pool"`
	AccountID           string        `json:"account_id"`
	ManagedSessionID    string        `json:"managed_session_id"`
	ClientDeviceID      string        `json:"client_device_id"`
	TargetDeviceID      string        `json:"target_device_id"`
	Region              string        `json:"region"`
	PathKind            RelayPathKind `json:"path_kind"`
	RouteID             string        `json:"route_id,omitempty"`
	RouteVersion        uint32        `json:"route_version,omitempty"`
	ClientEdgeRelayID   string        `json:"client_edge_relay_id,omitempty"`
	DaemonEdgeRelayID   string        `json:"daemon_edge_relay_id,omitempty"`
	MaxInternalTransit  uint32        `json:"max_internal_transit"`
	NotBeforeUnix       int64         `json:"not_before_unix"`
	ExpiresAtUnix       int64         `json:"expires_at_unix"`
	MaxBytes            uint64        `json:"max_bytes"`
	MaxBitrateKbps      uint32        `json:"max_bitrate_kbps"`
	MaxConcurrency      uint32        `json:"max_concurrency"`
	CredentialBindingID string        `json:"credential_binding_id"`
}

// RelayLeaseRequest 是 entitlement 通过后交给 credential issuer 的完整租约输入。
// Quota 字段必须来自服务端 policy clamp，不能直接信任客户端请求值。
type RelayLeaseRequest struct {
	LeaseID             string
	AudienceRelayPool   string
	AccountID           string
	ManagedSessionID    string
	ClientDeviceID      string
	TargetDeviceID      string
	Region              string
	PathKind            RelayPathKind
	RouteID             string
	RouteVersion        uint32
	ClientEdgeRelayID   string
	DaemonEdgeRelayID   string
	MaxInternalTransit  uint32
	TTL                 time.Duration
	MaxBytes            uint64
	MaxBitrateKbps      uint32
	MaxConcurrency      uint32
	CredentialBindingID string
}

// RelayLease 包装不可记录的签名 lease bytes。
// String 始终脱敏；Relay credential material 只能通过显式 Bytes 调用传输。
type RelayLease struct {
	raw string
}

// Bytes 返回交给指定 Relay pool 的签名 lease 副本。
// 该值是短期服务凭据，不得作为 terminal authorization 或长期 TURN secret 使用。
func (lease RelayLease) Bytes() []byte {
	return []byte(lease.raw)
}

// String 返回脱敏文本，防止 fmt 或结构化日志泄漏 credential material。
func (lease RelayLease) String() string {
	return "RelayLease{[REDACTED]}"
}

// RelayLeaseIssuer 使用 Control Plane active key 签发 Relay lease。
// 该 issuer 只接受已经由 entitlement domain clamp 的 quota，不接触 terminal scope。
type RelayLeaseIssuer struct {
	issuer string
	signer Signer
}

// NewRelayLeaseIssuer 创建固定 Ed25519 算法的 Relay lease issuer。
func NewRelayLeaseIssuer(issuer string, signer Signer) (RelayLeaseIssuer, error) {
	if issuer == "" || signer.KeyID() == "" {
		return RelayLeaseIssuer{}, fmt.Errorf("invalid Relay lease issuer")
	}
	return RelayLeaseIssuer{issuer: issuer, signer: signer}, nil
}

// Issue 验证 route shape、quota 和短 TTL 后签发 session-specific Relay lease。
// single relay 不接受 mesh 字段；mesh 最多允许一个 internal transit。
func (issuer RelayLeaseIssuer) Issue(request RelayLeaseRequest, now time.Time) (RelayLease, RelayLeaseClaims, error) {
	if err := validateRelayLeaseRequest(request); err != nil {
		return RelayLease{}, RelayLeaseClaims{}, err
	}
	now = now.UTC().Truncate(time.Second)
	claims := RelayLeaseClaims{
		Version:             relayLeaseVersion,
		KeyID:               issuer.signer.KeyID(),
		LeaseID:             request.LeaseID,
		Issuer:              issuer.issuer,
		AudienceRelayPool:   request.AudienceRelayPool,
		AccountID:           request.AccountID,
		ManagedSessionID:    request.ManagedSessionID,
		ClientDeviceID:      request.ClientDeviceID,
		TargetDeviceID:      request.TargetDeviceID,
		Region:              request.Region,
		PathKind:            request.PathKind,
		RouteID:             request.RouteID,
		RouteVersion:        request.RouteVersion,
		ClientEdgeRelayID:   request.ClientEdgeRelayID,
		DaemonEdgeRelayID:   request.DaemonEdgeRelayID,
		MaxInternalTransit:  request.MaxInternalTransit,
		NotBeforeUnix:       now.Unix(),
		ExpiresAtUnix:       now.Add(request.TTL).Unix(),
		MaxBytes:            request.MaxBytes,
		MaxBitrateKbps:      request.MaxBitrateKbps,
		MaxConcurrency:      request.MaxConcurrency,
		CredentialBindingID: request.CredentialBindingID,
	}
	raw, err := signToken(relayLeasePrefix, claims, issuer.signer, now)
	if err != nil {
		return RelayLease{}, RelayLeaseClaims{}, err
	}
	return RelayLease{raw: raw}, claims, nil
}

// RelayLeaseExpectation 是 Relay 在接纳 TURN/WebRTC 流量前必须匹配的服务上下文。
// Relay 从部署身份和 connection allocation 构造这些值，不能让 endpoint 改写 lease audience 或 route。
type RelayLeaseExpectation struct {
	Issuer            string
	AudienceRelayPool string
	AccountID         string
	ManagedSessionID  string
	ClientDeviceID    string
	TargetDeviceID    string
	Region            string
	PathKind          RelayPathKind
	RouteID           string
}

// VerifyRelayLease 离线验签并验证 Relay pool、session、双方设备、region 和 route 绑定。
// 成功只授权受限 Relay 数据面，不授权 Hub signaling 或 daemon terminal operation。
func VerifyRelayLease(ring *KeyRing, encoded []byte, expected RelayLeaseExpectation, now time.Time) (RelayLeaseClaims, error) {
	var claims RelayLeaseClaims
	if err := verifyToken(relayLeasePrefix, string(encoded), tokenKeyID, &claims, ring, now); err != nil {
		return RelayLeaseClaims{}, err
	}
	if err := validateRelayLeaseClaims(claims, expected, now); err != nil {
		return RelayLeaseClaims{}, err
	}
	return claims, nil
}

func validateRelayLeaseRequest(request RelayLeaseRequest) error {
	if request.LeaseID == "" || request.AudienceRelayPool == "" || request.AccountID == "" || request.ManagedSessionID == "" || request.ClientDeviceID == "" || request.TargetDeviceID == "" || request.Region == "" || request.CredentialBindingID == "" {
		return ErrMalformedCredential
	}
	if request.TTL <= 0 || request.TTL > maxRelayLeaseTTL || request.MaxBytes == 0 || request.MaxBitrateKbps == 0 || request.MaxConcurrency == 0 {
		return ErrCredentialExpired
	}
	switch request.PathKind {
	case RelayPathSingle:
		if request.RouteID != "" || request.RouteVersion != 0 || request.ClientEdgeRelayID != "" || request.DaemonEdgeRelayID != "" || request.MaxInternalTransit != 0 {
			return ErrCredentialBinding
		}
	case RelayPathMesh:
		if request.RouteID == "" || request.RouteVersion == 0 || request.ClientEdgeRelayID == "" || request.DaemonEdgeRelayID == "" || request.ClientEdgeRelayID == request.DaemonEdgeRelayID || request.MaxInternalTransit > 1 {
			return ErrCredentialBinding
		}
	default:
		return ErrCredentialBinding
	}
	return nil
}

func validateRelayLeaseClaims(claims RelayLeaseClaims, expected RelayLeaseExpectation, now time.Time) error {
	request := RelayLeaseRequest{
		LeaseID:             claims.LeaseID,
		AudienceRelayPool:   claims.AudienceRelayPool,
		AccountID:           claims.AccountID,
		ManagedSessionID:    claims.ManagedSessionID,
		ClientDeviceID:      claims.ClientDeviceID,
		TargetDeviceID:      claims.TargetDeviceID,
		Region:              claims.Region,
		PathKind:            claims.PathKind,
		RouteID:             claims.RouteID,
		RouteVersion:        claims.RouteVersion,
		ClientEdgeRelayID:   claims.ClientEdgeRelayID,
		DaemonEdgeRelayID:   claims.DaemonEdgeRelayID,
		MaxInternalTransit:  claims.MaxInternalTransit,
		TTL:                 time.Unix(claims.ExpiresAtUnix, 0).Sub(time.Unix(claims.NotBeforeUnix, 0)),
		MaxBytes:            claims.MaxBytes,
		MaxBitrateKbps:      claims.MaxBitrateKbps,
		MaxConcurrency:      claims.MaxConcurrency,
		CredentialBindingID: claims.CredentialBindingID,
	}
	if claims.Version != relayLeaseVersion || claims.KeyID == "" || claims.Issuer == "" {
		return ErrMalformedCredential
	}
	if err := validateRelayLeaseRequest(request); err != nil {
		return err
	}
	notBefore := time.Unix(claims.NotBeforeUnix, 0).UTC()
	expiresAt := time.Unix(claims.ExpiresAtUnix, 0).UTC()
	if now.Before(notBefore) || !now.Before(expiresAt) {
		return ErrCredentialExpired
	}
	if claims.Issuer != expected.Issuer || claims.AudienceRelayPool != expected.AudienceRelayPool || claims.AccountID != expected.AccountID || claims.ManagedSessionID != expected.ManagedSessionID || claims.ClientDeviceID != expected.ClientDeviceID || claims.TargetDeviceID != expected.TargetDeviceID || claims.Region != expected.Region || claims.PathKind != expected.PathKind || claims.RouteID != expected.RouteID {
		return ErrCredentialBinding
	}
	return nil
}
