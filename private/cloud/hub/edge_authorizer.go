package hub

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/entitlement"
	"github.com/muxvia/muxvia/private/cloud/control-plane/servicecredential"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

var (
	// ErrPolicySnapshot 表示授权投影缺失、回滚、断档或超过最大陈旧窗口。
	ErrPolicySnapshot = errors.New("Hub authorization snapshot unavailable")
	// ErrEdgeAuthorization 表示 edge token、账号 epoch、target ownership 或 revoke 状态拒绝连接。
	ErrEdgeAuthorization = errors.New("Hub edge authorization rejected")
	// ErrTargetUnavailable 表示已认证 client 请求的 daemon 已移除、类型错误或不属于当前账号。
	// 所有 target 失败统一使用该错误，避免跨账号探测设备是否存在。
	ErrTargetUnavailable = errors.New("Hub target device unavailable")
	// ErrP2PConcurrency 表示账号当前 managed P2P reservation 已达到签名套餐上限。
	ErrP2PConcurrency = errors.New("Hub managed P2P concurrency exhausted")
	// ErrP2PNotEntitled 表示账号身份有效，但当前 Entitlement 不允许新的 managed P2P。
	ErrP2PNotEntitled = errors.New("Hub managed P2P entitlement denied")
)

// DeviceAuthorization 是 Control Plane 同步给 Hub 的最小 daemon 授权投影。
// PublicKey 供后续 fresh presence proof 使用；Revoked 优先于账号 ownership 和订阅 allow。
type DeviceAuthorization struct {
	DeviceID    string
	AccountID   string
	Kind        string
	DisplayName string
	Platform    string
	PublicKey   []byte
	Revoked     bool
	AuthEpoch   uint64
}

// AccountAuthorization 是 Hub 判断账号 edge token epoch 和 managed service 能力的本地投影。
// AuthEpoch 变化会使旧 token 立即失效；Capability 直接复用 PlanCatalog/Entitlement 的 generated schema。
type AccountAuthorization struct {
	AccountID                     string
	AuthEpoch                     uint64
	Revoked                       bool
	EntitlementStatus             cloudpb.EntitlementStatus
	EntitlementEffectiveUntilUnix int64
	Capability                    *cloudpb.PlanCapability
}

// AuthorizationSnapshot 是 Hub 原子应用的、带严格单调 revision 的授权快照。
// GeneratedAt 用于 max staleness；Devices/Accounts 在应用时深拷贝，caller 后续修改不影响运行时真值。
type AuthorizationSnapshot struct {
	Revision    uint64
	GeneratedAt time.Time
	Accounts    []AccountAuthorization
	Devices     []DeviceAuthorization
}

// EdgeAuthorizerConfig 固定 Hub identity、edge token issuer、公钥和授权快照最大陈旧窗口。
// 请求路径没有 Control Plane callback；任何 cache miss 都直接 fail closed。
type EdgeAuthorizerConfig struct {
	HubID        string
	Issuer       string
	KeyRing      *servicecredential.KeyRing
	Clock        Clock
	MaxStaleness time.Duration
}

// EdgeAuthorizer 是 Hub managed direct 授权决策和 P2P reservation 的纯内存 owner。
// 它只接受当前 HubControl generation 验签后的 projection；Edge 重启必须重新 full sync，不得恢复磁盘快照。
type EdgeAuthorizer struct {
	mu           sync.RWMutex
	hubID        string
	issuer       string
	keyRing      *servicecredential.KeyRing
	clock        Clock
	maxStaleness time.Duration
	revision     uint64
	generatedAt  time.Time
	accounts     map[string]AccountAuthorization
	devices      map[string]DeviceAuthorization
	reservations map[string]managedP2PReservation
}

type managedP2PReservation struct {
	accountID        string
	clientDeviceID   string
	targetDeviceID   string
	managedSessionID string
	assignmentEpoch  uint64
	pendingUntil     time.Time
	runtimeKey       string
}

// Revision 返回当前已验签并发布的 policy revision。
// 该值只用于 Control Plane 重启后继续生成严格递增快照，不暴露账号或设备内容。
func (authorizer *EdgeAuthorizer) Revision() uint64 {
	if authorizer == nil {
		return 0
	}
	authorizer.mu.RLock()
	defer authorizer.mu.RUnlock()
	return authorizer.revision
}

// NewEdgeAuthorizer 创建没有授权快照的 Hub authorizer。
// 在首次成功 ApplySnapshot 前所有连接均 fail closed。
func NewEdgeAuthorizer(config EdgeAuthorizerConfig) (*EdgeAuthorizer, error) {
	if config.HubID == "" || config.Issuer == "" || config.KeyRing == nil || config.MaxStaleness <= 0 {
		return nil, fmt.Errorf("invalid Hub edge authorizer configuration")
	}
	if config.Clock == nil {
		config.Clock = realClock{}
	}
	return &EdgeAuthorizer{hubID: config.HubID, issuer: config.Issuer, keyRing: config.KeyRing, clock: config.Clock, maxStaleness: config.MaxStaleness, reservations: make(map[string]managedP2PReservation)}, nil
}

// RelayBudget 是 Hub 从签名账号投影读取的 single Relay 区域预算。
// 它只限制付费 Relay 服务，不扩大 daemon terminal capability。
type RelayBudget struct {
	MaxLeaseDuration time.Duration
	MaxBytes         uint64
	MaxBitrateKbps   uint32
	MaxConcurrency   uint32
}

// RelayBudget 返回账号当前签名投影中的 single Relay 预算。
// 快照陈旧、账号撤销、未订阅或字段不完整时 fail closed。
func (authorizer *EdgeAuthorizer) RelayBudget(accountID string) (RelayBudget, error) {
	now := authorizer.clock.Now().UTC()
	authorizer.mu.RLock()
	defer authorizer.mu.RUnlock()
	account, ok := authorizer.accounts[accountID]
	if authorizer.revision == 0 || now.Sub(authorizer.generatedAt) > authorizer.maxStaleness || !ok || !account.activeAt(now) || account.Capability == nil || !account.Capability.GetStandardRelayEnabled() {
		return RelayBudget{}, ErrEdgeAuthorization
	}
	relay := account.Capability.GetRelay()
	if relay == nil || relay.GetMaxLeaseSeconds() == 0 || relay.GetMaxBytesPerLease() == 0 || relay.GetMaxBitrateKbps() == 0 || relay.GetMaxConcurrency() == 0 {
		return RelayBudget{}, ErrEdgeAuthorization
	}
	return RelayBudget{MaxLeaseDuration: time.Duration(relay.GetMaxLeaseSeconds()) * time.Second, MaxBytes: relay.GetMaxBytesPerLease(), MaxBitrateKbps: relay.GetMaxBitrateKbps(), MaxConcurrency: relay.GetMaxConcurrency()}, nil
}

// ApplySnapshot 原子替换完整授权投影。
// revision 必须严格递增；rollback、重复 revision、未来时间和重复主体都会拒绝且保留旧快照。
func (authorizer *EdgeAuthorizer) ApplySnapshot(snapshot AuthorizationSnapshot) error {
	now := authorizer.clock.Now().UTC()
	if snapshot.Revision == 0 || snapshot.GeneratedAt.IsZero() || snapshot.GeneratedAt.After(now) {
		return ErrPolicySnapshot
	}
	accounts := make(map[string]AccountAuthorization, len(snapshot.Accounts))
	for _, account := range snapshot.Accounts {
		if account.AccountID == "" || account.AuthEpoch == 0 || account.Capability == nil || entitlement.ValidatePlanCapability(account.Capability) != nil {
			return ErrPolicySnapshot
		}
		if _, exists := accounts[account.AccountID]; exists {
			return ErrPolicySnapshot
		}
		account.Capability = cloneHubPlanCapability(account.Capability)
		accounts[account.AccountID] = account
	}
	devices := make(map[string]DeviceAuthorization, len(snapshot.Devices))
	for _, device := range snapshot.Devices {
		if device.DeviceID == "" || device.AccountID == "" || device.DisplayName == "" || device.Kind != "client" && device.Kind != "daemon" {
			return ErrPolicySnapshot
		}
		if _, exists := devices[device.DeviceID]; exists {
			return ErrPolicySnapshot
		}
		device.PublicKey = append([]byte(nil), device.PublicKey...)
		devices[device.DeviceID] = device
	}
	authorizer.mu.Lock()
	defer authorizer.mu.Unlock()
	if snapshot.Revision <= authorizer.revision {
		return ErrPolicySnapshot
	}
	authorizer.revision, authorizer.generatedAt = snapshot.Revision, snapshot.GeneratedAt.UTC()
	authorizer.accounts, authorizer.devices = accounts, devices
	return nil
}

// AuthorizeDirect 离线验证 client edge token，并与本地账号和 target device 投影取交集。
// 返回 claims 只供 Hub 创建短期 EdgeManagedSession；任何缺失、撤销、epoch 不匹配或陈旧快照都 fail closed。
func (authorizer *EdgeAuthorizer) AuthorizeDirect(token []byte, accountID, clientDeviceID, targetDeviceID string) (servicecredential.EdgeAccessClaims, error) {
	now := authorizer.clock.Now().UTC()
	claims, err := authorizer.verifyClientClaims(token, accountID, clientDeviceID, now)
	if err != nil {
		return servicecredential.EdgeAccessClaims{}, err
	}
	authorizer.mu.RLock()
	defer authorizer.mu.RUnlock()
	if _, err := authorizer.authorizeManagedP2PLocked(claims, accountID, clientDeviceID, targetDeviceID, now); err != nil {
		return servicecredential.EdgeAccessClaims{}, err
	}
	return claims, nil
}

// ReserveManagedP2P 在当前签名 policy、ownership、revoke 和 auth epoch 下原子占用账号并发名额。
// reservationID 先绑定 signaling；answer 后由 daemon 完整 runtime inventory 转交为存活 session reservation。
func (authorizer *EdgeAuthorizer) ReserveManagedP2P(token []byte, accountID, clientDeviceID, targetDeviceID, reservationID, managedSessionID string, assignmentEpoch uint64, pendingUntil time.Time) (servicecredential.EdgeAccessClaims, error) {
	if reservationID == "" || managedSessionID == "" || assignmentEpoch == 0 || !pendingUntil.After(authorizer.clock.Now().UTC()) {
		return servicecredential.EdgeAccessClaims{}, ErrEdgeAuthorization
	}
	now := authorizer.clock.Now().UTC()
	claims, err := authorizer.verifyClientClaims(token, accountID, clientDeviceID, now)
	if err != nil {
		return servicecredential.EdgeAccessClaims{}, err
	}
	authorizer.mu.Lock()
	defer authorizer.mu.Unlock()
	authorizer.cleanupReservationsLocked(now)
	account, err := authorizer.authorizeManagedP2PLocked(claims, accountID, clientDeviceID, targetDeviceID, now)
	if err != nil {
		return servicecredential.EdgeAccessClaims{}, err
	}
	if _, exists := authorizer.reservations[reservationID]; exists {
		return servicecredential.EdgeAccessClaims{}, ErrP2PConcurrency
	}
	active := uint32(0)
	for _, reservation := range authorizer.reservations {
		if reservation.accountID == accountID {
			active++
		}
	}
	if limit := account.Capability.GetManagedP2PMaxConcurrency(); limit == 0 || active >= limit {
		return servicecredential.EdgeAccessClaims{}, ErrP2PConcurrency
	}
	authorizer.reservations[reservationID] = managedP2PReservation{accountID: accountID, clientDeviceID: clientDeviceID, targetDeviceID: targetDeviceID, managedSessionID: managedSessionID, assignmentEpoch: assignmentEpoch, pendingUntil: pendingUntil.UTC()}
	return claims, nil
}

func (authorizer *EdgeAuthorizer) verifyClientClaims(token []byte, accountID, clientDeviceID string, now time.Time) (servicecredential.EdgeAccessClaims, error) {
	claims, err := servicecredential.VerifyEdgeAccess(authorizer.keyRing, token, servicecredential.EdgeAccessExpectation{Issuer: authorizer.issuer, AudienceHubID: authorizer.hubID, AccountID: accountID, ClientDeviceID: clientDeviceID, PrincipalKind: servicecredential.EdgePrincipalClient}, now)
	if err != nil {
		return servicecredential.EdgeAccessClaims{}, fmt.Errorf("%w: %v", ErrEdgeAuthorization, err)
	}
	return claims, nil
}

func (authorizer *EdgeAuthorizer) authorizeManagedP2PLocked(claims servicecredential.EdgeAccessClaims, accountID, clientDeviceID, targetDeviceID string, now time.Time) (AccountAuthorization, error) {
	if authorizer.revision == 0 || now.Sub(authorizer.generatedAt) > authorizer.maxStaleness {
		return AccountAuthorization{}, ErrPolicySnapshot
	}
	account, accountOK := authorizer.accounts[accountID]
	client, clientOK := authorizer.devices[clientDeviceID]
	target, targetOK := authorizer.devices[targetDeviceID]
	if !accountOK || !clientOK || account.Revoked || account.AuthEpoch != claims.AuthEpoch || client.Revoked || client.AccountID != accountID || client.Kind != "client" {
		return AccountAuthorization{}, ErrEdgeAuthorization
	}
	if account.EntitlementStatus != cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_ACTIVE || now.Unix() >= account.EntitlementEffectiveUntilUnix || account.Capability == nil || !account.Capability.GetManagedP2PEnabled() {
		return AccountAuthorization{}, ErrP2PNotEntitled
	}
	if !targetOK || target.Revoked || target.AccountID != accountID || target.Kind != "daemon" {
		return AccountAuthorization{}, ErrTargetUnavailable
	}
	return account, nil
}

// ReleaseManagedP2P 幂等释放精确 signaling reservation；它不按账号计数猜测要释放的 session。
func (authorizer *EdgeAuthorizer) ReleaseManagedP2P(reservationID string) {
	if authorizer == nil || reservationID == "" {
		return
	}
	authorizer.mu.Lock()
	delete(authorizer.reservations, reservationID)
	authorizer.mu.Unlock()
}

// CloseManagedP2PSignaling 结束 signaling 对 reservation 的所有权。
// 已收到 answer 的 reservation 必须保留到 daemon runtime inventory 接管或 pending TTL 到期；未 answer 的请求立即释放。
func (authorizer *EdgeAuthorizer) CloseManagedP2PSignaling(reservationID string, answered bool) {
	if authorizer == nil || reservationID == "" {
		return
	}
	authorizer.mu.Lock()
	reservation, ok := authorizer.reservations[reservationID]
	if ok && !answered && reservation.runtimeKey == "" {
		delete(authorizer.reservations, reservationID)
	}
	authorizer.mu.Unlock()
}

// ReconcileManagedP2P 使用 daemon-owned 完整 PeerSession inventory 替换指定 daemon 的存活 P2P reservation。
// 它只消费 DIRECT 且处于 AUTHENTICATED/READY/CLOSING 的 session；Relay 和 CLOSED 不占 managed P2P 名额。
func (authorizer *EdgeAuthorizer) ReconcileManagedP2P(daemonDeviceID string, sessions []*cloudpb.ManagedPeerSessionProjection) error {
	if authorizer == nil || daemonDeviceID == "" {
		return ErrPolicySnapshot
	}
	now := authorizer.clock.Now().UTC()
	authorizer.mu.Lock()
	defer authorizer.mu.Unlock()
	candidate := make(map[string]managedP2PReservation, len(authorizer.reservations)+len(sessions))
	for reservationID, reservation := range authorizer.reservations {
		candidate[reservationID] = reservation
	}
	cleanupManagedP2PReservations(candidate, now)
	target, targetOK := authorizer.devices[daemonDeviceID]
	if !targetOK || target.Kind != "daemon" || target.AccountID == "" {
		return ErrTargetUnavailable
	}
	active := make(map[string]struct{}, len(sessions))
	for _, session := range sessions {
		if !managedP2PSessionActive(session) {
			continue
		}
		targetRef := session.GetTarget()
		client := authorizer.devices[session.GetClientDeviceId()]
		if targetRef.GetDaemonDeviceId() != daemonDeviceID || targetRef.GetManagedSessionId() == "" || targetRef.GetSessionIncarnation() == 0 || targetRef.GetAssignmentEpoch() == 0 || client.DeviceID == "" || client.Kind != "client" || client.AccountID != target.AccountID {
			return ErrEdgeAuthorization
		}
		runtimeKey := managedP2PRuntimeKey(targetRef)
		active[runtimeKey] = struct{}{}
		matched := ""
		for reservationID, reservation := range candidate {
			if reservation.runtimeKey == runtimeKey || reservation.runtimeKey == "" && reservation.managedSessionID == targetRef.GetManagedSessionId() && reservation.clientDeviceID == session.GetClientDeviceId() && reservation.targetDeviceID == daemonDeviceID && reservation.assignmentEpoch == targetRef.GetAssignmentEpoch() {
				matched = reservationID
				reservation.runtimeKey = runtimeKey
				reservation.pendingUntil = time.Time{}
				candidate[reservationID] = reservation
				break
			}
		}
		if matched == "" {
			candidate["runtime\x00"+runtimeKey] = managedP2PReservation{accountID: target.AccountID, clientDeviceID: session.GetClientDeviceId(), targetDeviceID: daemonDeviceID, managedSessionID: targetRef.GetManagedSessionId(), assignmentEpoch: targetRef.GetAssignmentEpoch(), runtimeKey: runtimeKey}
		}
	}
	for reservationID, reservation := range candidate {
		if reservation.targetDeviceID == daemonDeviceID && reservation.runtimeKey != "" {
			if _, exists := active[reservation.runtimeKey]; !exists {
				delete(candidate, reservationID)
			}
		}
	}
	authorizer.reservations = candidate
	return nil
}

// ReleaseManagedP2PForAssignment 在 assignment fence 时释放精确 daemon+epoch 的本 Hub reservation。
// 新 epoch 的 reservation 不得被迟到 fence 误删；新 owning Hub 会从 daemon 完整 inventory 重建仍存活的 session。
func (authorizer *EdgeAuthorizer) ReleaseManagedP2PForAssignment(daemonDeviceID string, assignmentEpoch uint64) {
	if authorizer == nil || daemonDeviceID == "" || assignmentEpoch == 0 {
		return
	}
	authorizer.mu.Lock()
	for reservationID, reservation := range authorizer.reservations {
		if reservation.targetDeviceID == daemonDeviceID && reservation.assignmentEpoch == assignmentEpoch {
			delete(authorizer.reservations, reservationID)
		}
	}
	authorizer.mu.Unlock()
}

func (authorizer *EdgeAuthorizer) cleanupReservationsLocked(now time.Time) {
	cleanupManagedP2PReservations(authorizer.reservations, now)
}

func cleanupManagedP2PReservations(reservations map[string]managedP2PReservation, now time.Time) {
	for reservationID, reservation := range reservations {
		if reservation.runtimeKey == "" && !reservation.pendingUntil.IsZero() && !now.Before(reservation.pendingUntil) {
			delete(reservations, reservationID)
		}
	}
}

func managedP2PSessionActive(session *cloudpb.ManagedPeerSessionProjection) bool {
	if session == nil || session.GetObservedDataPath() != cloudpb.ObservedPath_OBSERVED_PATH_DIRECT {
		return false
	}
	switch session.GetState() {
	case cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_AUTHENTICATED, cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_READY, cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_CLOSING:
		return true
	default:
		return false
	}
}

func managedP2PRuntimeKey(target *cloudpb.ManagedPeerSessionTarget) string {
	return fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%s", target.GetDaemonDeviceId(), target.GetManagedSessionId(), target.GetSessionIncarnation(), target.GetAssignmentEpoch(), target.GetDaemonRuntimeGeneration())
}

func (account AccountAuthorization) activeAt(now time.Time) bool {
	return !account.Revoked && account.EntitlementStatus == cloudpb.EntitlementStatus_ENTITLEMENT_STATUS_ACTIVE && now.Unix() < account.EntitlementEffectiveUntilUnix
}

func cloneHubPlanCapability(capability *cloudpb.PlanCapability) *cloudpb.PlanCapability {
	if capability == nil {
		return nil
	}
	return proto.Clone(capability).(*cloudpb.PlanCapability)
}

// AuthorizeClient 验证当前连接发起端自身仍存在于 Hub 签名内存投影且未撤销。
// 目录、resolve、signaling 和 Relay 都应先经过该边界，不能只验证账号或目标 daemon。
func (authorizer *EdgeAuthorizer) AuthorizeClient(token []byte, accountID, clientDeviceID string) (servicecredential.EdgeAccessClaims, error) {
	now := authorizer.clock.Now().UTC()
	claims, err := servicecredential.VerifyEdgeAccess(authorizer.keyRing, token, servicecredential.EdgeAccessExpectation{Issuer: authorizer.issuer, AudienceHubID: authorizer.hubID, AccountID: accountID, ClientDeviceID: clientDeviceID, PrincipalKind: servicecredential.EdgePrincipalClient}, now)
	if err != nil {
		return servicecredential.EdgeAccessClaims{}, fmt.Errorf("%w: %v", ErrEdgeAuthorization, err)
	}
	authorizer.mu.RLock()
	defer authorizer.mu.RUnlock()
	account, accountOK := authorizer.accounts[accountID]
	client, clientOK := authorizer.devices[clientDeviceID]
	if authorizer.revision == 0 || now.Sub(authorizer.generatedAt) > authorizer.maxStaleness || !accountOK || !clientOK || account.Revoked || account.AuthEpoch != claims.AuthEpoch || client.Revoked || client.AccountID != accountID || client.Kind != "client" {
		return servicecredential.EdgeAccessClaims{}, ErrEdgeAuthorization
	}
	return claims, nil
}

// AccountDevices 返回账号设备投影的深拷贝；调用前必须已经通过 AuthorizeClient。
// 返回值不含 session、terminal inventory 或 CapabilityGrant，Presence 由 Hub Service 另行叠加。
func (authorizer *EdgeAuthorizer) AccountDevices(accountID string) []DeviceAuthorization {
	authorizer.mu.RLock()
	defer authorizer.mu.RUnlock()
	devices := make([]DeviceAuthorization, 0)
	for _, device := range authorizer.devices {
		if device.AccountID != accountID {
			continue
		}
		device.PublicKey = append([]byte(nil), device.PublicKey...)
		devices = append(devices, device)
	}
	return devices
}

// AuthorizeDaemon 离线验证 daemon edge token、账号 epoch 与本地 device ownership/revoke 投影。
// 它只允许完成已由该 device active presence 接收的 signaling，不授予 client offer 或 terminal capability。
func (authorizer *EdgeAuthorizer) AuthorizeDaemon(token []byte, accountID, deviceID string) (servicecredential.EdgeAccessClaims, error) {
	claims, _, err := authorizer.AuthorizeDaemonDevice(token, accountID, deviceID)
	return claims, err
}

// AuthorizeDaemonDevice 离线验证 daemon edge token，并返回与 token 同 revision 的设备公钥投影。
// Hub Presence 使用该公钥验证 fresh DeviceProof；返回值不包含 private key、terminal 或 capability。
func (authorizer *EdgeAuthorizer) AuthorizeDaemonDevice(token []byte, accountID, deviceID string) (servicecredential.EdgeAccessClaims, DeviceAuthorization, error) {
	now := authorizer.clock.Now().UTC()
	claims, err := servicecredential.VerifyEdgeAccess(authorizer.keyRing, token, servicecredential.EdgeAccessExpectation{Issuer: authorizer.issuer, AudienceHubID: authorizer.hubID, AccountID: accountID, ClientDeviceID: deviceID, PrincipalKind: servicecredential.EdgePrincipalDaemon}, now)
	if err != nil {
		return servicecredential.EdgeAccessClaims{}, DeviceAuthorization{}, fmt.Errorf("%w: %v", ErrEdgeAuthorization, err)
	}
	authorizer.mu.RLock()
	defer authorizer.mu.RUnlock()
	if authorizer.revision == 0 || now.Sub(authorizer.generatedAt) > authorizer.maxStaleness {
		return servicecredential.EdgeAccessClaims{}, DeviceAuthorization{}, ErrPolicySnapshot
	}
	account, accountOK := authorizer.accounts[accountID]
	device, deviceOK := authorizer.devices[deviceID]
	if !accountOK || !deviceOK || account.Revoked || account.AuthEpoch != claims.AuthEpoch || device.Revoked || device.AccountID != accountID || device.Kind != "daemon" {
		return servicecredential.EdgeAccessClaims{}, DeviceAuthorization{}, ErrEdgeAuthorization
	}
	device.PublicKey = append([]byte(nil), device.PublicKey...)
	return claims, device, nil
}

// AuthorizeDaemonControl 验证 Controller 签名命令引用的账号、daemon 与 device auth epoch
// 仍属于当前 fresh policy。它不验证命令签名或 session target，后两者分别属于 daemon 与 Hub runtime。
func (authorizer *EdgeAuthorizer) AuthorizeDaemonControl(accountID, deviceID string, authEpoch uint64) error {
	if authorizer == nil || accountID == "" || deviceID == "" || authEpoch == 0 {
		return ErrEdgeAuthorization
	}
	now := authorizer.clock.Now().UTC()
	authorizer.mu.RLock()
	defer authorizer.mu.RUnlock()
	account, accountOK := authorizer.accounts[accountID]
	device, deviceOK := authorizer.devices[deviceID]
	if authorizer.revision == 0 || now.Sub(authorizer.generatedAt) > authorizer.maxStaleness || !accountOK || !deviceOK || account.Revoked || device.Revoked || device.AccountID != accountID || device.Kind != "daemon" || device.AuthEpoch != authEpoch {
		return ErrEdgeAuthorization
	}
	return nil
}
