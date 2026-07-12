package servicecredential

import (
	"fmt"
	"time"
)

const (
	edgeAccessPrefix  = "TXEA1"
	edgeAccessVersion = 1
	maxEdgeAccessTTL  = 24 * time.Hour
)

// EdgePrincipalKind 区分使用 Hub 边缘热路径的 client 与 daemon 身份。
// 角色写入签名 claims，防止同一账号下的 daemon token 被拿来发起 client offer。
type EdgePrincipalKind string

const (
	// EdgePrincipalClient 表示发起 managed signaling 的客户端。
	EdgePrincipalClient EdgePrincipalKind = "client"
	// EdgePrincipalDaemon 表示已注册 presence 并完成 answer 的 daemon。
	EdgePrincipalDaemon EdgePrincipalKind = "daemon"
)

// EdgeAccessClaims 是客户端启动阶段取得、由 Hub 离线验证的账号边缘会话声明。
// 它只证明固定账号与 client device；target ownership 和订阅能力必须由 Hub 本地授权投影另行判断。
type EdgeAccessClaims struct {
	Version          uint32            `json:"version"`
	KeyID            string            `json:"key_id"`
	TokenID          string            `json:"token_id"`
	Issuer           string            `json:"issuer"`
	AudienceHubID    string            `json:"audience_hub_id"`
	AccountID        string            `json:"account_id"`
	ClientDeviceID   string            `json:"client_device_id"`
	PrincipalKind    EdgePrincipalKind `json:"principal_kind"`
	AuthEpoch        uint64            `json:"auth_epoch"`
	IssuedAtUnix     int64             `json:"issued_at_unix"`
	NotBeforeUnix    int64             `json:"not_before_unix"`
	ExpiresAtUnix    int64             `json:"expires_at_unix"`
	HubURL           string            `json:"hub_url,omitempty"`
	Region           string            `json:"region,omitempty"`
	DirectoryVersion uint64            `json:"directory_version,omitempty"`
}

// EdgeAccessIssuer 持有 Control Plane 的 edge access 专用签名器。
// Hub 只接收其公钥；该 issuer 不签发 Relay lease 或 terminal capability。
type EdgeAccessIssuer struct {
	issuer string
	signer Signer
}

// NewEdgeAccessIssuer 创建 edge access issuer；issuer 与 key id 为空时 fail closed。
func NewEdgeAccessIssuer(issuer string, signer Signer) (EdgeAccessIssuer, error) {
	if issuer == "" || signer.KeyID() == "" {
		return EdgeAccessIssuer{}, fmt.Errorf("invalid edge access issuer")
	}
	return EdgeAccessIssuer{issuer: issuer, signer: signer}, nil
}

// IssueEdgeAccess 为固定 Hub、账号和 client device 签发最长 24 小时的离线凭据。
// AuthEpoch 来自 Control Plane 持久账号状态，Hub 必须与本地投影中的 epoch 精确匹配。
func (issuer EdgeAccessIssuer) IssueEdgeAccess(tokenID, hubID, accountID, clientDeviceID string, authEpoch uint64, ttl time.Duration, now time.Time) ([]byte, error) {
	return issuer.IssueEdgeAccessForPrincipal(tokenID, hubID, accountID, clientDeviceID, EdgePrincipalClient, authEpoch, ttl, now)
}

// IssueEdgeAccessForPrincipal 为固定 client/daemon 角色签发 edge credential。
// 调用方必须从已认证的登录或设备 enrollment owner 构造 principal，不能接受 caller 自选角色。
func (issuer EdgeAccessIssuer) IssueEdgeAccessForPrincipal(tokenID, hubID, accountID, deviceID string, principal EdgePrincipalKind, authEpoch uint64, ttl time.Duration, now time.Time) ([]byte, error) {
	return issuer.IssueEdgeAccessWithDirectory(tokenID, hubID, "", "", 0, accountID, deviceID, principal, authEpoch, ttl, now)
}

// IssueEdgeAccessWithDirectory 把启动阶段选择的 Hub URL、region 和防回滚 version 绑定进签名 edge credential。
func (issuer EdgeAccessIssuer) IssueEdgeAccessWithDirectory(tokenID, hubID, hubURL, region string, directoryVersion uint64, accountID, deviceID string, principal EdgePrincipalKind, authEpoch uint64, ttl time.Duration, now time.Time) ([]byte, error) {
	if tokenID == "" || hubID == "" || accountID == "" || deviceID == "" || authEpoch == 0 || principal != EdgePrincipalClient && principal != EdgePrincipalDaemon {
		return nil, ErrMalformedCredential
	}
	if ttl <= 0 || ttl > maxEdgeAccessTTL {
		return nil, ErrCredentialExpired
	}
	now = now.UTC().Truncate(time.Second)
	claims := EdgeAccessClaims{
		Version: edgeAccessVersion, KeyID: issuer.signer.KeyID(), TokenID: tokenID, Issuer: issuer.issuer,
		AudienceHubID: hubID, AccountID: accountID, ClientDeviceID: deviceID, PrincipalKind: principal, AuthEpoch: authEpoch,
		IssuedAtUnix: now.Unix(), NotBeforeUnix: now.Unix(), ExpiresAtUnix: now.Add(ttl).Unix(),
		HubURL: hubURL, Region: region, DirectoryVersion: directoryVersion,
	}
	raw, err := signToken(edgeAccessPrefix, claims, issuer.signer, now)
	return []byte(raw), err
}

// EdgeAccessExpectation 是 Hub 从自身 identity 和请求连接构造的验签上下文。
// target device 不在这里，因为 target authorization 属于 Hub 授权投影而非 bearer token。
type EdgeAccessExpectation struct {
	Issuer         string
	AudienceHubID  string
	AccountID      string
	ClientDeviceID string
	PrincipalKind  EdgePrincipalKind
}

// VerifyEdgeAccess 离线验证签名、时效、Hub audience、账号和 client device 绑定。
// 成功不表示 target 已授权；调用方仍必须执行本地 policy projection 决策。
func VerifyEdgeAccess(ring *KeyRing, encoded []byte, expected EdgeAccessExpectation, now time.Time) (EdgeAccessClaims, error) {
	var claims EdgeAccessClaims
	if err := verifyToken(edgeAccessPrefix, string(encoded), tokenKeyID, &claims, ring, now); err != nil {
		return EdgeAccessClaims{}, err
	}
	issuedAt := time.Unix(claims.IssuedAtUnix, 0).UTC()
	notBefore := time.Unix(claims.NotBeforeUnix, 0).UTC()
	expiresAt := time.Unix(claims.ExpiresAtUnix, 0).UTC()
	if claims.Version != edgeAccessVersion || claims.KeyID == "" || claims.TokenID == "" || claims.AuthEpoch == 0 || claims.PrincipalKind != EdgePrincipalClient && claims.PrincipalKind != EdgePrincipalDaemon ||
		!expiresAt.After(issuedAt) || expiresAt.Sub(issuedAt) > maxEdgeAccessTTL || notBefore.Before(issuedAt) || notBefore.After(expiresAt) || now.Before(notBefore) || !now.Before(expiresAt) {
		return EdgeAccessClaims{}, ErrCredentialExpired
	}
	if claims.Issuer != expected.Issuer || claims.AudienceHubID != expected.AudienceHubID || claims.AccountID != expected.AccountID || claims.ClientDeviceID != expected.ClientDeviceID || claims.PrincipalKind != expected.PrincipalKind {
		return EdgeAccessClaims{}, ErrCredentialBinding
	}
	return claims, nil
}
