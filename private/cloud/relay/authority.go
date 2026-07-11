// Package relay 实现私有 TURN/Relay lease enforcement 与 usage metering。
//
// Relay 只验证 Control Plane 签名的短期 RelayLease、派生 principal-specific TURN credential、
// 转发 opaque WebRTC packets 并生成签名 usage metadata。它不终止 DTLS/DataChannel，
// 不接收 CapabilityGrant，也不解释 terminal protocol。
package relay

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	"github.com/lozzow/termx/private/cloud/control-plane/usage"
	pionturn "github.com/pion/turn/v4"
)

var (
	// ErrLeaseRejected 表示 RelayLease 签名、audience、region、route 或 binding 不匹配。
	ErrLeaseRejected = errors.New("Relay lease rejected")
	// ErrCredentialRejected 表示 TURN username 未由 active lease 派生、已过期或 principal 不匹配。
	ErrCredentialRejected = errors.New("Relay credential rejected")
	// ErrConcurrency 表示 lease 的 active/pending allocation 已达到并发上限。
	ErrConcurrency = errors.New("Relay lease concurrency exhausted")
	// ErrQuota 表示 lease 的总字节或当前速率窗口已耗尽。
	ErrQuota = errors.New("Relay lease quota exhausted")
	// ErrAllocationNotFound 表示 traffic meter 无法把 packet 绑定到已确认 allocation。
	ErrAllocationNotFound = errors.New("Relay allocation not found")
)

// Clock 是 Relay credential、lease、pending auth 和 usage interval 的时间来源。
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

// BindingAuthorizer 验证 signed credential_binding_id 是否允许当前 Relay 节点服务该 lease。
// Control Plane/infra 可以实现数据库或 deterministic binding；Relay 不查询 entitlement/billing model。
type BindingAuthorizer interface {
	AllowsBinding(bindingID, relayID string, claims servicecredential.RelayLeaseClaims) bool
}

// StaticBindings 是测试和静态部署使用的 binding allowlist。
// key 是 credential_binding_id，value 是允许消费该签名 lease 的 RelayID 集合。
type StaticBindings map[string]map[string]struct{}

// AllowsBinding 根据静态 allowlist 判断 lease binding。
func (bindings StaticBindings) AllowsBinding(bindingID, relayID string, _ servicecredential.RelayLeaseClaims) bool {
	_, ok := bindings[bindingID][relayID]
	return ok
}

// Config 固定 Relay deployment identity、验签公钥、派生密钥和 usage signing key。
// CredentialSecret 与 UsageSigner 是不同用途密钥；任何一个都不得下发 endpoint。
type Config struct {
	RelayID          string
	RelayPool        string
	Region           string
	LeaseIssuer      string
	Realm            string
	KeyRing          *servicecredential.KeyRing
	Bindings         BindingAuthorizer
	CredentialSecret []byte
	UsageSigner      servicecredential.Signer
	Clock            Clock
	CredentialTTL    time.Duration
	PendingAuthTTL   time.Duration
	NonceReader      io.Reader
}

// Authority 是 RelayLease、TURN credential、allocation limit 与 usage sequence 的并发安全 owner。
type Authority struct {
	mu sync.Mutex

	relayID        string
	relayPool      string
	region         string
	leaseIssuer    string
	realm          string
	keyRing        *servicecredential.KeyRing
	bindings       BindingAuthorizer
	secret         []byte
	usageSigner    servicecredential.Signer
	clock          Clock
	credentialTTL  time.Duration
	pendingAuthTTL time.Duration
	nonceReader    io.Reader

	leases      map[string]*leaseState
	credentials map[string]credentialState
	pending     map[string]pendingAuth
	allocations map[string]allocationState
}

type leaseState struct {
	claims            servicecredential.RelayLeaseClaims
	activatedAt       time.Time
	lastUsageAt       time.Time
	sequence          uint64
	totalBytes        uint64
	pendingBytesUp    uint64
	pendingBytesDown  uint64
	windowStartedAt   time.Time
	windowBytesUp     uint64
	windowBytesDown   uint64
	activeAllocations int
}

type credentialState struct {
	leaseID   string
	principal Principal
	deviceID  string
	password  string
	expiresAt time.Time
}

type pendingAuth struct {
	username  string
	leaseID   string
	expiresAt time.Time
}

type allocationState struct {
	leaseID   string
	username  string
	principal Principal
	sourceID  string
}

// Principal 区分 Relay lease 的 client 与 daemon endpoint credential。
type Principal string

const (
	// PrincipalClient 是 client edge allocation 的 credential principal。
	PrincipalClient Principal = "client"
	// PrincipalDaemon 是 daemon edge allocation 的 credential principal。
	PrincipalDaemon Principal = "daemon"
)

// ActivationRequest 提供离线验签所需的完整 expected context。
// 所有字段来自 Control Plane 内部调用与 Relay deployment identity，不能由匿名 TURN caller 提供。
type ActivationRequest struct {
	SignedLease      []byte
	AccountID        string
	ManagedSessionID string
	ClientDeviceID   string
	TargetDeviceID   string
	PathKind         servicecredential.RelayPathKind
	RouteID          string
}

// Credential 是返回给一个 endpoint 的 caller-specific 短期 TURN 凭据。
// String 始终脱敏；Username 与 Password 只能进入 ICE server config。
type Credential struct {
	Username  string
	Password  string
	ExpiresAt time.Time
}

// String 返回脱敏 credential 描述，不泄漏 username/password。
func (credential Credential) String() string {
	return fmt.Sprintf("RelayCredential{expires_at=%s username=[REDACTED] password=[REDACTED]}", credential.ExpiresAt.Format(time.RFC3339))
}

// Activation 是 Relay 成功接受 signed lease 后返回的 principal-specific credential pair。
// Claims 仅用于内部 service metadata；公开 endpoint 只获得对应自己的 Credential。
type Activation struct {
	Claims           servicecredential.RelayLeaseClaims
	ClientCredential Credential
	DaemonCredential Credential
}

// NewAuthority 创建 lease enforcement owner。
// 所有 identity、key、binding 和短 TTL 配置缺一不可，不存在共享 TURN credential fallback。
func NewAuthority(config Config) (*Authority, error) {
	if config.RelayID == "" || config.RelayPool == "" || config.Region == "" || config.LeaseIssuer == "" || config.Realm == "" || config.KeyRing == nil || config.Bindings == nil || len(config.CredentialSecret) < 32 || config.UsageSigner.KeyID() == "" {
		return nil, fmt.Errorf("invalid Relay authority identity or key configuration")
	}
	if config.Clock == nil {
		config.Clock = realClock{}
	}
	if config.NonceReader == nil {
		config.NonceReader = rand.Reader
	}
	if config.CredentialTTL <= 0 || config.CredentialTTL > 10*time.Minute || config.PendingAuthTTL <= 0 || config.PendingAuthTTL > 30*time.Second {
		return nil, fmt.Errorf("invalid Relay credential TTL configuration")
	}
	return &Authority{
		relayID: config.RelayID, relayPool: config.RelayPool, region: config.Region, leaseIssuer: config.LeaseIssuer,
		realm: config.Realm, keyRing: config.KeyRing, bindings: config.Bindings,
		secret: append([]byte(nil), config.CredentialSecret...), usageSigner: config.UsageSigner,
		clock: config.Clock, credentialTTL: config.CredentialTTL, pendingAuthTTL: config.PendingAuthTTL,
		nonceReader: config.NonceReader,
		leases:      make(map[string]*leaseState), credentials: make(map[string]credentialState), pending: make(map[string]pendingAuth), allocations: make(map[string]allocationState),
	}, nil
}

// ActivateLease 离线验签、验证 relay binding，并派生 client/daemon 两套短期 TURN credential。
// 相同 lease ID 不能用不同 claims 重新激活；无 signed lease 或错误 audience/route 一律 fail closed。
func (authority *Authority) ActivateLease(request ActivationRequest) (Activation, error) {
	now := authority.clock.Now().UTC()
	claims, err := servicecredential.VerifyRelayLease(authority.keyRing, request.SignedLease, servicecredential.RelayLeaseExpectation{
		Issuer: authority.leaseIssuer, AudienceRelayPool: authority.relayPool, AccountID: request.AccountID,
		ManagedSessionID: request.ManagedSessionID, ClientDeviceID: request.ClientDeviceID, TargetDeviceID: request.TargetDeviceID,
		Region: authority.region, PathKind: request.PathKind, RouteID: request.RouteID,
	}, now)
	if err != nil {
		return Activation{}, fmt.Errorf("%w: %v", ErrLeaseRejected, err)
	}
	if !authority.bindings.AllowsBinding(claims.CredentialBindingID, authority.relayID, claims) {
		return Activation{}, ErrLeaseRejected
	}
	if claims.PathKind == servicecredential.RelayPathMesh && claims.ClientEdgeRelayID != authority.relayID && claims.DaemonEdgeRelayID != authority.relayID {
		return Activation{}, ErrLeaseRejected
	}

	authority.mu.Lock()
	defer authority.mu.Unlock()
	authority.cleanupLocked(now)
	if current := authority.leases[claims.LeaseID]; current != nil {
		if current.claims != claims {
			return Activation{}, ErrLeaseRejected
		}
		var client Credential
		if authority.principalAllowed(claims, PrincipalClient) {
			client, err = authority.credentialForLeaseLocked(current, PrincipalClient, now)
			if err != nil {
				return Activation{}, err
			}
		}
		var daemon Credential
		if authority.principalAllowed(claims, PrincipalDaemon) {
			daemon, err = authority.credentialForLeaseLocked(current, PrincipalDaemon, now)
			if err != nil {
				return Activation{}, err
			}
		}
		return Activation{Claims: claims, ClientCredential: client, DaemonCredential: daemon}, nil
	}
	state := &leaseState{claims: claims, activatedAt: now, lastUsageAt: now, windowStartedAt: now.Truncate(time.Second)}
	authority.leases[claims.LeaseID] = state
	var client Credential
	if authority.principalAllowed(claims, PrincipalClient) {
		client, err = authority.newCredentialLocked(state, PrincipalClient, claims.ClientDeviceID, now)
		if err != nil {
			delete(authority.leases, claims.LeaseID)
			return Activation{}, err
		}
	}
	var daemon Credential
	if authority.principalAllowed(claims, PrincipalDaemon) {
		daemon, err = authority.newCredentialLocked(state, PrincipalDaemon, claims.TargetDeviceID, now)
		if err != nil {
			delete(authority.credentials, client.Username)
			delete(authority.leases, claims.LeaseID)
			return Activation{}, err
		}
	}
	return Activation{Claims: claims, ClientCredential: client, DaemonCredential: daemon}, nil
}

// AuthenticateTURN 返回 Pion TURN message-integrity key，并创建短 TTL pending allocation reservation。
// username 未由 active lease 派生、realm 错误、lease 过期或并发耗尽时返回 false。
func (authority *Authority) AuthenticateTURN(username, realm, sourceID string) ([]byte, bool) {
	now := authority.clock.Now().UTC()
	authority.mu.Lock()
	defer authority.mu.Unlock()
	authority.cleanupLocked(now)
	credential, ok := authority.credentials[username]
	if !ok || realm != authority.realm || sourceID == "" || !now.Before(credential.expiresAt) {
		return nil, false
	}
	lease := authority.leases[credential.leaseID]
	if lease == nil || !now.Before(time.Unix(lease.claims.ExpiresAtUnix, 0)) {
		return nil, false
	}
	// TURN permission/channel 请求会对已确认 allocation 再次鉴权；它们复用同一 source，不应重复占用并发配额。
	if authority.activeSourceAuthorizedLocked(sourceID, username) {
		return pionturn.GenerateAuthKey(username, authority.realm, credential.password), true
	}
	if pending, exists := authority.pending[sourceID]; exists {
		if pending.username != username {
			return nil, false
		}
	} else {
		if lease.activeAllocations+authority.pendingCountLocked(credential.leaseID) >= int(lease.claims.MaxConcurrency) {
			return nil, false
		}
		authority.pending[sourceID] = pendingAuth{username: username, leaseID: credential.leaseID, expiresAt: now.Add(authority.pendingAuthTTL)}
	}
	return pionturn.GenerateAuthKey(username, authority.realm, credential.password), true
}

// ConfirmAllocation 把通过 TURN message-integrity 的 pending source 转成 active allocation。
// Event handler 必须使用稳定 allocationID；重复确认保持幂等。
func (authority *Authority) ConfirmAllocation(sourceID, allocationID, username string) error {
	now := authority.clock.Now().UTC()
	authority.mu.Lock()
	defer authority.mu.Unlock()
	authority.cleanupLocked(now)
	if current, ok := authority.allocations[allocationID]; ok {
		if current.username == username && current.sourceID == sourceID {
			return nil
		}
		return ErrCredentialRejected
	}
	pending, ok := authority.pending[sourceID]
	if !ok || pending.username != username || !now.Before(pending.expiresAt) {
		return ErrCredentialRejected
	}
	credential := authority.credentials[username]
	lease := authority.leases[pending.leaseID]
	if lease == nil || credential.leaseID != pending.leaseID || lease.activeAllocations >= int(lease.claims.MaxConcurrency) {
		return ErrConcurrency
	}
	delete(authority.pending, sourceID)
	lease.activeAllocations++
	authority.allocations[allocationID] = allocationState{leaseID: pending.leaseID, username: username, principal: credential.principal, sourceID: sourceID}
	return nil
}

// ReleaseAllocation 删除 active allocation 并释放 lease concurrency。
func (authority *Authority) ReleaseAllocation(allocationID string) {
	authority.mu.Lock()
	defer authority.mu.Unlock()
	allocation, ok := authority.allocations[allocationID]
	if !ok {
		return
	}
	delete(authority.allocations, allocationID)
	if lease := authority.leases[allocation.leaseID]; lease != nil && lease.activeAllocations > 0 {
		lease.activeAllocations--
	}
}

// RecordTraffic 把 opaque packet bytes 计入 allocation 对应 lease，并执行总字节和每秒 bitrate 上限。
// bytesUp/bytesDown 只表示方向计数，Relay 不读取 payload 内容。
func (authority *Authority) RecordTraffic(allocationID string, bytesUp, bytesDown uint64) error {
	now := authority.clock.Now().UTC()
	authority.mu.Lock()
	defer authority.mu.Unlock()
	authority.cleanupLocked(now)
	allocation, ok := authority.allocations[allocationID]
	if !ok {
		return ErrAllocationNotFound
	}
	lease := authority.leases[allocation.leaseID]
	if lease == nil || !now.Before(time.Unix(lease.claims.ExpiresAtUnix, 0)) {
		return ErrLeaseRejected
	}
	if lease.claims.PathKind == servicecredential.RelayPathSingle && allocation.principal == PrincipalDaemon {
		return nil
	}
	if bytesUp > math.MaxUint64-bytesDown {
		return ErrQuota
	}
	delta := bytesUp + bytesDown
	if lease.totalBytes > lease.claims.MaxBytes || delta > lease.claims.MaxBytes-lease.totalBytes {
		return ErrQuota
	}
	window := now.Truncate(time.Second)
	if !window.Equal(lease.windowStartedAt) {
		lease.windowStartedAt = window
		lease.windowBytesUp = 0
		lease.windowBytesDown = 0
	}
	maxDirectionBytes := uint64(lease.claims.MaxBitrateKbps) * 125
	if bytesUp > maxDirectionBytes-lease.windowBytesUp || bytesDown > maxDirectionBytes-lease.windowBytesDown {
		return ErrQuota
	}
	lease.totalBytes += delta
	lease.pendingBytesUp += bytesUp
	lease.pendingBytesDown += bytesDown
	lease.windowBytesUp += bytesUp
	lease.windowBytesDown += bytesDown
	return nil
}

// DrainUsage 签发每个有新增流量 lease 的幂等递增 UsageEvent。
// Relay Mesh 每个 hop 独立上报；Control Plane ledger 负责按 route/session 聚合一次。
func (authority *Authority) DrainUsage(terminationReason string) ([]usage.Event, error) {
	now := authority.clock.Now().UTC()
	authority.mu.Lock()
	defer authority.mu.Unlock()
	authority.cleanupPendingLocked(now)
	events := make([]usage.Event, 0)
	for _, lease := range authority.leases {
		if lease.pendingBytesUp == 0 && lease.pendingBytesDown == 0 {
			continue
		}
		if now.Unix() <= lease.lastUsageAt.Unix() {
			continue
		}
		lease.sequence++
		activeSeconds := uint64(now.Sub(lease.lastUsageAt) / time.Second)
		event := usage.Event{
			EventID: fmt.Sprintf("%s-%s-%d", authority.relayID, lease.claims.LeaseID, lease.sequence),
			LeaseID: lease.claims.LeaseID, ManagedSessionID: lease.claims.ManagedSessionID, RelayID: authority.relayID,
			RouteID: lease.claims.RouteID, PathKind: lease.claims.PathKind, HopID: authority.relayID, Sequence: lease.sequence,
			IntervalStartUnix: lease.lastUsageAt.Unix(), IntervalEndUnix: now.Unix(), BytesUp: lease.pendingBytesUp,
			BytesDown: lease.pendingBytesDown, ActiveSeconds: activeSeconds, TerminationReason: terminationReason,
		}
		signed, err := usage.SignEvent(event, authority.usageSigner, now)
		if err != nil {
			return nil, err
		}
		events = append(events, signed)
		lease.pendingBytesUp = 0
		lease.pendingBytesDown = 0
		lease.lastUsageAt = now
	}
	return events, nil
}

func (authority *Authority) newCredentialLocked(lease *leaseState, principal Principal, deviceID string, now time.Time) (Credential, error) {
	expiresAt := minTime(time.Unix(lease.claims.ExpiresAtUnix, 0).UTC(), now.Add(authority.credentialTTL))
	nonceBytes := make([]byte, 12)
	if _, err := io.ReadFull(authority.nonceReader, nonceBytes); err != nil {
		return Credential{}, fmt.Errorf("generate Relay credential nonce: %w", err)
	}
	username := fmt.Sprintf("tx1.%s.%s.%d.%s", lease.claims.LeaseID, principal, expiresAt.Unix(), base64.RawURLEncoding.EncodeToString(nonceBytes))
	password := authority.password(username, lease.claims.CredentialBindingID)
	authority.credentials[username] = credentialState{leaseID: lease.claims.LeaseID, principal: principal, deviceID: deviceID, password: password, expiresAt: expiresAt}
	return Credential{Username: username, Password: password, ExpiresAt: expiresAt}, nil
}

func (authority *Authority) credentialForLeaseLocked(lease *leaseState, principal Principal, now time.Time) (Credential, error) {
	for username, credential := range authority.credentials {
		if credential.leaseID == lease.claims.LeaseID && credential.principal == principal && now.Before(credential.expiresAt) {
			return Credential{Username: username, Password: credential.password, ExpiresAt: credential.expiresAt}, nil
		}
	}
	deviceID := lease.claims.ClientDeviceID
	if principal == PrincipalDaemon {
		deviceID = lease.claims.TargetDeviceID
	}
	return authority.newCredentialLocked(lease, principal, deviceID, now)
}

func (authority *Authority) password(username, bindingID string) string {
	mac := hmac.New(sha256.New, authority.secret)
	_, _ = mac.Write([]byte(authority.realm + "\n" + bindingID + "\n" + username))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (authority *Authority) pendingCountLocked(leaseID string) int {
	count := 0
	for _, pending := range authority.pending {
		if pending.leaseID == leaseID {
			count++
		}
	}
	return count
}

func (authority *Authority) activeSourceAuthorizedLocked(sourceID, username string) bool {
	for _, allocation := range authority.allocations {
		if allocation.sourceID == sourceID && allocation.username == username {
			return true
		}
	}
	return false
}

func (authority *Authority) principalAllowed(claims servicecredential.RelayLeaseClaims, principal Principal) bool {
	if claims.PathKind == servicecredential.RelayPathSingle {
		return true
	}
	if principal == PrincipalClient {
		return claims.ClientEdgeRelayID == authority.relayID
	}
	return claims.DaemonEdgeRelayID == authority.relayID
}

func (authority *Authority) cleanupLocked(now time.Time) {
	authority.cleanupPendingLocked(now)
	for username, credential := range authority.credentials {
		if !now.Before(credential.expiresAt) {
			delete(authority.credentials, username)
		}
	}
	for leaseID, lease := range authority.leases {
		if !now.Before(time.Unix(lease.claims.ExpiresAtUnix, 0)) && lease.activeAllocations == 0 && lease.pendingBytesUp == 0 && lease.pendingBytesDown == 0 {
			delete(authority.leases, leaseID)
		}
	}
}

func (authority *Authority) cleanupPendingLocked(now time.Time) {
	for sourceID, pending := range authority.pending {
		if !now.Before(pending.expiresAt) {
			delete(authority.pending, sourceID)
		}
	}
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}
