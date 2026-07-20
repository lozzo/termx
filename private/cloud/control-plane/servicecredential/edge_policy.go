package servicecredential

import (
	"fmt"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

const (
	edgePolicyPrefix  = "TXEP1"
	edgePolicyVersion = 1
	maxEdgePolicyTTL  = 24 * time.Hour
)

// EdgePolicyAccount 是签名 Hub 授权快照中的最小账号投影。
// 它只包含 edge admission 所需状态，不包含账单明细、refresh token 或 terminal capability。
type EdgePolicyAccount struct {
	AccountID                     string                    `json:"account_id"`
	AuthEpoch                     uint64                    `json:"auth_epoch"`
	Revoked                       bool                      `json:"revoked"`
	EntitlementStatus             cloudpb.EntitlementStatus `json:"entitlement_status"`
	EntitlementEffectiveUntilUnix int64                     `json:"entitlement_effective_until_unix"`
	Capability                    *cloudpb.PlanCapability   `json:"capability"`
}

// EdgePolicyDevice 是签名 Hub 授权快照中的最小设备 ownership/public-key 投影。
// PublicKey 只用于 DeviceIdentity proof；私钥和 CapabilityGrant 永不进入快照。
type EdgePolicyDevice struct {
	DeviceID    string `json:"device_id"`
	AccountID   string `json:"account_id"`
	Kind        string `json:"kind"`
	DisplayName string `json:"display_name"`
	Platform    string `json:"platform,omitempty"`
	PublicKey   []byte `json:"public_key,omitempty"`
	Revoked     bool   `json:"revoked"`
}

// EdgePolicyClaims 是 Control Plane 签发、Hub 可持久化并在重启时重新验签的完整授权快照。
// Revision 严格单调；ExpiresAt 同时限制离线恢复和 Control Plane 中断的最大陈旧窗口。
type EdgePolicyClaims struct {
	Version       uint32              `json:"version"`
	KeyID         string              `json:"key_id"`
	Issuer        string              `json:"issuer"`
	AudienceHubID string              `json:"audience_hub_id"`
	Revision      uint64              `json:"revision"`
	Accounts      []EdgePolicyAccount `json:"accounts"`
	Devices       []EdgePolicyDevice  `json:"devices"`
	GeneratedUnix int64               `json:"generated_unix"`
	ExpiresAtUnix int64               `json:"expires_at_unix"`
}

// EdgePolicyIssuer 使用 Control Plane 的 edge policy 专用签名身份签发完整 Hub 快照。
// 生产部署应与 Relay 委派和其他 credential 使用不同 key；Hub 只持有 verification key。
type EdgePolicyIssuer struct {
	issuer string
	signer Signer
}

// NewEdgePolicyIssuer 创建固定 issuer 和 Ed25519 signer 的快照签发器。
func NewEdgePolicyIssuer(issuer string, signer Signer) (EdgePolicyIssuer, error) {
	if issuer == "" || signer.KeyID() == "" {
		return EdgePolicyIssuer{}, fmt.Errorf("invalid edge policy issuer")
	}
	return EdgePolicyIssuer{issuer: issuer, signer: signer}, nil
}

// Issue 签发目标 Hub、revision 和完整账号/设备投影，TTL 最长 24 小时。
// caller 必须从同一数据库 revision 构造全部记录；delta 不得调用此方法伪装完整快照。
func (issuer EdgePolicyIssuer) Issue(hubID string, revision uint64, accounts []EdgePolicyAccount, devices []EdgePolicyDevice, ttl time.Duration, now time.Time) ([]byte, error) {
	if hubID == "" || revision == 0 || len(accounts) == 0 || ttl <= 0 || ttl > maxEdgePolicyTTL {
		return nil, ErrMalformedCredential
	}
	now = now.UTC().Truncate(time.Second)
	claims := EdgePolicyClaims{Version: edgePolicyVersion, KeyID: issuer.signer.KeyID(), Issuer: issuer.issuer, AudienceHubID: hubID, Revision: revision, Accounts: cloneEdgePolicyAccounts(accounts), Devices: cloneEdgePolicyDevices(devices), GeneratedUnix: now.Unix(), ExpiresAtUnix: now.Add(ttl).Unix()}
	if err := validateEdgePolicyClaims(claims, issuer.issuer, hubID, now); err != nil {
		return nil, err
	}
	raw, err := signToken(edgePolicyPrefix, claims, issuer.signer, now)
	return []byte(raw), err
}

// VerifyEdgePolicy 离线验证签名、issuer、Hub audience、时效和完整记录约束。
// 成功返回深拷贝 claims；Hub 仍负责 revision 单调与原子发布。
func VerifyEdgePolicy(ring *KeyRing, encoded []byte, issuer, hubID string, now time.Time) (EdgePolicyClaims, error) {
	var claims EdgePolicyClaims
	if err := verifyToken(edgePolicyPrefix, string(encoded), tokenKeyID, &claims, ring, now); err != nil {
		return EdgePolicyClaims{}, err
	}
	if err := validateEdgePolicyClaims(claims, issuer, hubID, now.UTC()); err != nil {
		return EdgePolicyClaims{}, err
	}
	claims.Accounts = cloneEdgePolicyAccounts(claims.Accounts)
	claims.Devices = cloneEdgePolicyDevices(claims.Devices)
	return claims, nil
}

func validateEdgePolicyClaims(claims EdgePolicyClaims, issuer, hubID string, now time.Time) error {
	generated := time.Unix(claims.GeneratedUnix, 0).UTC()
	expires := time.Unix(claims.ExpiresAtUnix, 0).UTC()
	if claims.Version != edgePolicyVersion || claims.KeyID == "" || claims.Issuer != issuer || claims.AudienceHubID != hubID || claims.Revision == 0 || len(claims.Accounts) == 0 || generated.After(now) || !expires.After(generated) || expires.Sub(generated) > maxEdgePolicyTTL || !now.Before(expires) {
		return ErrCredentialExpired
	}
	accounts := make(map[string]struct{}, len(claims.Accounts))
	for _, account := range claims.Accounts {
		if account.AccountID == "" || account.AuthEpoch == 0 || !validEdgePlanCapability(account.Capability) {
			return ErrMalformedCredential
		}
		switch account.EntitlementStatus {
		case cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_ACTIVE:
			if account.EntitlementEffectiveUntilUnix <= now.Unix() {
				return ErrCredentialExpired
			}
		case cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_SUSPENDED, cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_EXPIRED:
		default:
			return ErrMalformedCredential
		}
		if _, exists := accounts[account.AccountID]; exists {
			return ErrMalformedCredential
		}
		accounts[account.AccountID] = struct{}{}
	}
	devices := make(map[string]struct{}, len(claims.Devices))
	for _, device := range claims.Devices {
		if device.DeviceID == "" || device.AccountID == "" || device.DisplayName == "" || device.Kind != "client" && device.Kind != "daemon" {
			return ErrMalformedCredential
		}
		if _, exists := accounts[device.AccountID]; !exists {
			return ErrCredentialBinding
		}
		if _, exists := devices[device.DeviceID]; exists {
			return ErrMalformedCredential
		}
		devices[device.DeviceID] = struct{}{}
	}
	return nil
}

func cloneEdgePolicyAccounts(source []EdgePolicyAccount) []EdgePolicyAccount {
	result := append([]EdgePolicyAccount(nil), source...)
	for index := range result {
		if result[index].Capability != nil {
			result[index].Capability = proto.Clone(result[index].Capability).(*cloudpb.PlanCapability)
		}
	}
	return result
}

func cloneEdgePolicyDevices(source []EdgePolicyDevice) []EdgePolicyDevice {
	result := append([]EdgePolicyDevice(nil), source...)
	for index := range result {
		result[index].PublicKey = append([]byte(nil), result[index].PublicKey...)
	}
	return result
}

func validEdgePlanCapability(capability *cloudpb.PlanCapability) bool {
	if capability == nil || capability.GetCloudDeviceLimit() == 0 || !capability.GetManagedP2PEnabled() && !capability.GetStandardRelayEnabled() {
		return false
	}
	if capability.GetManagedP2PEnabled() != (capability.GetManagedP2PMaxConcurrency() > 0) {
		return false
	}
	relay := capability.GetRelay()
	if !capability.GetStandardRelayEnabled() {
		return relay == nil || len(relay.GetAllowedRegions()) == 0 && !relay.GetAllowRelayMesh() && relay.GetMaxLeaseSeconds() == 0 && relay.GetMaxBytesPerLease() == 0 && relay.GetMaxBitrateKbps() == 0 && relay.GetMaxConcurrency() == 0 && relay.GetMaxBytesPerPeriod() == 0
	}
	return relay != nil && !relay.GetAllowRelayMesh() && len(relay.GetAllowedRegions()) > 0 && relay.GetMaxLeaseSeconds() > 0 && relay.GetMaxBytesPerLease() > 0 && relay.GetMaxBitrateKbps() > 0 && relay.GetMaxConcurrency() > 0 && relay.GetMaxBytesPerPeriod() >= relay.GetMaxBytesPerLease()
}
