package hub

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

var (
	// ErrProjectionUnavailable 表示 Hub 尚未取得可用 full projection 或 projection 已过期。
	ErrProjectionUnavailable = errors.New("Hub control projection unavailable")
	// ErrProjectionConflict 表示 revision、digest、签名或字段关系与当前完整投影冲突。
	ErrProjectionConflict = errors.New("Hub control projection conflict")
)

const projectionSignatureDomain = "termx-hub-projection-v1\x00"

// AssignmentFence 是 assignment expiry timer 对 Hub runtime 的唯一关闭边界。
// 实现必须只关闭精确 daemon+epoch，不得按裸 device ID 误伤后续 assignment。
type AssignmentFence interface {
	FenceAssignment(daemonDeviceID string, assignmentEpoch uint64)
}

// ProjectionConfig 固定 Hub identity、Controller projection key、最大陈旧窗口和 policy sink。
type ProjectionConfig struct {
	HubID               string
	ControllerKeyID     string
	ControllerPublicKey ed25519.PublicKey
	Clock               Clock
	MaxStaleness        time.Duration
	PolicySink          interface {
		ApplySnapshot(AuthorizationSnapshot) error
	}
	AssignmentFence AssignmentFence
}

// ProjectionSnapshot 是调试、health 与 control reconciliation 可读取的纯内存投影摘要。
// 返回消息均为深拷贝，caller 不能修改运行时真值。
type ProjectionSnapshot struct {
	Revision    uint64
	Digest      []byte
	GeneratedAt time.Time
	ExpiresAt   time.Time
	Accounts    []*cloudpb.HubAccountPolicy
	Devices     []*cloudpb.CloudDevicePolicy
	Assignments []*cloudpb.HubAssignment
}

// Projection 是 Hub policy/assignment 的唯一纯内存 owner。
// 它不提供文件 store；Edge 重启后必须等待 Controller 重新发送 full projection。
type Projection struct {
	mu sync.Mutex

	hubID           string
	controllerKeyID string
	controllerKey   ed25519.PublicKey
	clock           Clock
	maxStaleness    time.Duration
	policySink      interface {
		ApplySnapshot(AuthorizationSnapshot) error
	}
	fence AssignmentFence

	revision    uint64
	digest      []byte
	generatedAt time.Time
	expiresAt   time.Time
	accounts    map[string]*cloudpb.HubAccountPolicy
	devices     map[string]*cloudpb.CloudDevicePolicy
	assignments map[string]*cloudpb.HubAssignment
	timer       *time.Timer
	closed      bool
}

// NewProjection 创建空 Hub projection；首次 full snapshot 前 readiness 必须为 false。
func NewProjection(config ProjectionConfig) (*Projection, error) {
	if config.HubID == "" || config.ControllerKeyID == "" || len(config.ControllerPublicKey) != ed25519.PublicKeySize || config.MaxStaleness <= 0 || config.PolicySink == nil {
		return nil, fmt.Errorf("invalid Hub projection configuration")
	}
	if config.Clock == nil {
		config.Clock = realClock{}
	}
	return &Projection{
		hubID: config.HubID, controllerKeyID: config.ControllerKeyID,
		controllerKey: append(ed25519.PublicKey(nil), config.ControllerPublicKey...),
		clock:         config.Clock, maxStaleness: config.MaxStaleness, policySink: config.PolicySink, fence: config.AssignmentFence,
		accounts: make(map[string]*cloudpb.HubAccountPolicy), devices: make(map[string]*cloudpb.CloudDevicePolicy), assignments: make(map[string]*cloudpb.HubAssignment),
	}, nil
}

// ApplyFull 验签并原子替换完整 projection。
// 同 revision+digest 幂等；rollback、同 revision 不同 digest 和非法字段保留旧状态。
func (projection *Projection) ApplyFull(snapshot *cloudpb.FullProjectionSnapshot) error {
	if snapshot == nil {
		return ErrProjectionConflict
	}
	now := projection.clock.Now().UTC()
	candidate, err := validateFullProjection(snapshot, projection.hubID, projection.controllerKeyID, projection.controllerKey, now, projection.maxStaleness)
	if err != nil {
		return err
	}
	projection.mu.Lock()
	if projection.closed {
		projection.mu.Unlock()
		return ErrProjectionUnavailable
	}
	if candidate.revision == projection.revision {
		if bytes.Equal(candidate.digest, projection.digest) {
			projection.mu.Unlock()
			return nil
		}
		projection.mu.Unlock()
		return ErrProjectionConflict
	}
	if candidate.revision < projection.revision {
		projection.mu.Unlock()
		return ErrProjectionConflict
	}
	if err := projection.policySink.ApplySnapshot(candidate.authorizationSnapshot()); err != nil {
		projection.mu.Unlock()
		return fmt.Errorf("apply Hub account policy: %w", err)
	}
	fenced := projection.installLocked(candidate)
	projection.mu.Unlock()
	projection.fenceAssignments(fenced)
	return nil
}

// ApplyDelta 在当前完整 candidate 的副本上执行操作并校验 resulting digest 后原子发布。
func (projection *Projection) ApplyDelta(delta *cloudpb.PolicyDelta) error {
	if delta == nil {
		return ErrProjectionConflict
	}
	now := projection.clock.Now().UTC()
	if err := verifyDeltaEnvelope(delta, projection.hubID, projection.controllerKeyID, projection.controllerKey, now, projection.maxStaleness); err != nil {
		return err
	}
	projection.mu.Lock()
	if projection.closed || projection.revision == 0 || delta.GetPreviousProjectionRevision() != projection.revision || delta.GetProjectionRevision() != projection.revision+1 {
		projection.mu.Unlock()
		return ErrProjectionConflict
	}
	candidate := projection.candidateLocked(delta.GetProjectionRevision(), delta.GetGeneratedAtUnixMillis(), delta.GetExpiresAtUnixMillis())
	if err := applyDeltaOperations(candidate, delta); err != nil {
		projection.mu.Unlock()
		return err
	}
	if err := validateProjectionMaps(candidate, projection.hubID); err != nil {
		projection.mu.Unlock()
		return err
	}
	digest, err := digestCandidate(candidate)
	if err != nil || !bytes.Equal(digest, delta.GetResultingDigest()) {
		projection.mu.Unlock()
		return ErrProjectionConflict
	}
	candidate.digest = digest
	if err := projection.policySink.ApplySnapshot(candidate.authorizationSnapshot()); err != nil {
		projection.mu.Unlock()
		return fmt.Errorf("apply Hub account policy delta: %w", err)
	}
	fenced := projection.installLocked(candidate)
	projection.mu.Unlock()
	projection.fenceAssignments(fenced)
	return nil
}

// Ready 表示 projection 已完整应用且未超过有效期或 max staleness。
func (projection *Projection) Ready() bool {
	projection.mu.Lock()
	defer projection.mu.Unlock()
	if projection.closed || projection.revision == 0 {
		return false
	}
	now := projection.clock.Now().UTC()
	return now.Before(projection.expiresAt) && now.Sub(projection.generatedAt) <= projection.maxStaleness
}

// OwnsAssignment 判断精确 daemon+epoch 是否仍属于本 Hub 且 lease 未过期。
func (projection *Projection) OwnsAssignment(daemonDeviceID string, assignmentEpoch uint64) bool {
	projection.mu.Lock()
	defer projection.mu.Unlock()
	assignment := projection.assignments[daemonDeviceID]
	now := projection.clock.Now().UTC()
	return !projection.closed && assignment != nil && assignment.GetAssignmentEpoch() == assignmentEpoch && now.UnixMilli() >= assignment.GetNotBeforeUnixMillis() && now.UnixMilli() < assignment.GetExpiresAtUnixMillis()
}

// ActiveAssignment 返回当前 Hub 对 daemon 的有效 assignment epoch。
// Hub admission 只能消费该方法，不得从 device ownership 或 Presence 反推 assignment。
func (projection *Projection) ActiveAssignment(daemonDeviceID string) (uint64, bool) {
	projection.mu.Lock()
	defer projection.mu.Unlock()
	assignment := projection.assignments[daemonDeviceID]
	now := projection.clock.Now().UTC().UnixMilli()
	if projection.closed || assignment == nil || now < assignment.GetNotBeforeUnixMillis() || now >= assignment.GetExpiresAtUnixMillis() {
		return 0, false
	}
	return assignment.GetAssignmentEpoch(), true
}

// Snapshot 返回当前内存 projection 的深拷贝。
func (projection *Projection) Snapshot() ProjectionSnapshot {
	projection.mu.Lock()
	defer projection.mu.Unlock()
	result := ProjectionSnapshot{Revision: projection.revision, Digest: append([]byte(nil), projection.digest...), GeneratedAt: projection.generatedAt, ExpiresAt: projection.expiresAt}
	for _, value := range projection.accounts {
		result.Accounts = append(result.Accounts, proto.Clone(value).(*cloudpb.HubAccountPolicy))
	}
	for _, value := range projection.devices {
		result.Devices = append(result.Devices, proto.Clone(value).(*cloudpb.CloudDevicePolicy))
	}
	for _, value := range projection.assignments {
		result.Assignments = append(result.Assignments, proto.Clone(value).(*cloudpb.HubAssignment))
	}
	sortProjection(result.Accounts, result.Devices, result.Assignments)
	return result
}

// Close 停止 assignment timer 并使 projection 永久不可用。
func (projection *Projection) Close() {
	projection.mu.Lock()
	projection.closed = true
	if projection.timer != nil {
		projection.timer.Stop()
		projection.timer = nil
	}
	projection.mu.Unlock()
}

type projectionCandidate struct {
	revision    uint64
	digest      []byte
	generatedAt time.Time
	expiresAt   time.Time
	accounts    map[string]*cloudpb.HubAccountPolicy
	devices     map[string]*cloudpb.CloudDevicePolicy
	assignments map[string]*cloudpb.HubAssignment
}

func validateFullProjection(snapshot *cloudpb.FullProjectionSnapshot, hubID, keyID string, publicKey ed25519.PublicKey, now time.Time, maxStaleness time.Duration) (*projectionCandidate, error) {
	if snapshot.GetHubId() != hubID || snapshot.GetProjectionRevision() == 0 || snapshot.GetSigningKeyId() != keyID || len(snapshot.GetSignature()) != ed25519.SignatureSize {
		return nil, ErrProjectionConflict
	}
	generatedAt, expiresAt := time.UnixMilli(snapshot.GetGeneratedAtUnixMillis()).UTC(), time.UnixMilli(snapshot.GetExpiresAtUnixMillis()).UTC()
	if generatedAt.After(now) || !expiresAt.After(generatedAt) || expiresAt.Sub(generatedAt) > maxStaleness || !now.Before(expiresAt) {
		return nil, ErrProjectionUnavailable
	}
	signing, err := fullSigningBytes(snapshot)
	if err != nil || !ed25519.Verify(publicKey, signing, snapshot.GetSignature()) {
		return nil, ErrProjectionConflict
	}
	candidate := &projectionCandidate{revision: snapshot.GetProjectionRevision(), generatedAt: generatedAt, expiresAt: expiresAt, accounts: map[string]*cloudpb.HubAccountPolicy{}, devices: map[string]*cloudpb.CloudDevicePolicy{}, assignments: map[string]*cloudpb.HubAssignment{}}
	for _, value := range snapshot.GetAccounts() {
		if value == nil || candidate.accounts[value.GetAccountId()] != nil {
			return nil, ErrProjectionConflict
		}
		candidate.accounts[value.GetAccountId()] = proto.Clone(value).(*cloudpb.HubAccountPolicy)
	}
	for _, value := range snapshot.GetDevices() {
		if value == nil || candidate.devices[value.GetDeviceId()] != nil {
			return nil, ErrProjectionConflict
		}
		candidate.devices[value.GetDeviceId()] = proto.Clone(value).(*cloudpb.CloudDevicePolicy)
	}
	for _, value := range snapshot.GetAssignments() {
		if value == nil || candidate.assignments[value.GetDaemonDeviceId()] != nil {
			return nil, ErrProjectionConflict
		}
		candidate.assignments[value.GetDaemonDeviceId()] = proto.Clone(value).(*cloudpb.HubAssignment)
	}
	if err := validateProjectionMaps(candidate, hubID); err != nil {
		return nil, err
	}
	digest, err := digestCandidate(candidate)
	if err != nil || !bytes.Equal(digest, snapshot.GetSnapshotDigest()) {
		return nil, ErrProjectionConflict
	}
	candidate.digest = digest
	return candidate, nil
}

func verifyDeltaEnvelope(delta *cloudpb.PolicyDelta, hubID, keyID string, publicKey ed25519.PublicKey, now time.Time, maxStaleness time.Duration) error {
	if delta.GetHubId() != hubID || delta.GetProjectionRevision() == 0 || delta.GetSigningKeyId() != keyID || len(delta.GetSignature()) != ed25519.SignatureSize {
		return ErrProjectionConflict
	}
	generatedAt, expiresAt := time.UnixMilli(delta.GetGeneratedAtUnixMillis()).UTC(), time.UnixMilli(delta.GetExpiresAtUnixMillis()).UTC()
	if generatedAt.After(now) || !expiresAt.After(generatedAt) || expiresAt.Sub(generatedAt) > maxStaleness || !now.Before(expiresAt) {
		return ErrProjectionUnavailable
	}
	clone := proto.Clone(delta).(*cloudpb.PolicyDelta)
	clone.Signature = nil
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(clone)
	if err != nil || !ed25519.Verify(publicKey, append([]byte(projectionSignatureDomain+"delta\x00"), payload...), delta.GetSignature()) {
		return ErrProjectionConflict
	}
	return nil
}

func fullSigningBytes(snapshot *cloudpb.FullProjectionSnapshot) ([]byte, error) {
	clone := proto.Clone(snapshot).(*cloudpb.FullProjectionSnapshot)
	clone.Signature = nil
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(clone)
	if err != nil {
		return nil, err
	}
	return append([]byte(projectionSignatureDomain+"full\x00"), payload...), nil
}

func digestCandidate(candidate *projectionCandidate) ([]byte, error) {
	full := &cloudpb.FullProjectionSnapshot{HubId: "digest", ProjectionRevision: candidate.revision, GeneratedAtUnixMillis: candidate.generatedAt.UnixMilli(), ExpiresAtUnixMillis: candidate.expiresAt.UnixMilli()}
	for _, value := range candidate.accounts {
		full.Accounts = append(full.Accounts, proto.Clone(value).(*cloudpb.HubAccountPolicy))
	}
	for _, value := range candidate.devices {
		full.Devices = append(full.Devices, proto.Clone(value).(*cloudpb.CloudDevicePolicy))
	}
	for _, value := range candidate.assignments {
		full.Assignments = append(full.Assignments, proto.Clone(value).(*cloudpb.HubAssignment))
	}
	sortProjection(full.Accounts, full.Devices, full.Assignments)
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(full)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(append([]byte(projectionSignatureDomain+"digest\x00"), payload...))
	return digest[:], nil
}

func sortProjection(accounts []*cloudpb.HubAccountPolicy, devices []*cloudpb.CloudDevicePolicy, assignments []*cloudpb.HubAssignment) {
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].GetAccountId() < accounts[j].GetAccountId() })
	sort.Slice(devices, func(i, j int) bool { return devices[i].GetDeviceId() < devices[j].GetDeviceId() })
	sort.Slice(assignments, func(i, j int) bool { return assignments[i].GetDaemonDeviceId() < assignments[j].GetDaemonDeviceId() })
}

func validateProjectionMaps(candidate *projectionCandidate, hubID string) error {
	for accountID, account := range candidate.accounts {
		if accountID == "" || account.GetAuthEpoch() == 0 || account.GetCapability() == nil {
			return ErrProjectionConflict
		}
	}
	for deviceID, device := range candidate.devices {
		if deviceID == "" || device.GetAccountId() == "" || candidate.accounts[device.GetAccountId()] == nil || device.GetAuthEpoch() == 0 || device.GetDeviceKind() == cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_UNSPECIFIED {
			return ErrProjectionConflict
		}
	}
	for daemonID, assignment := range candidate.assignments {
		device := candidate.devices[daemonID]
		if assignment.GetHubId() != hubID || assignment.GetAccountId() == "" || assignment.GetAssignmentEpoch() == 0 || assignment.GetExpiresAtUnixMillis() <= assignment.GetNotBeforeUnixMillis() || device == nil || device.GetDeviceKind() != cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON || device.GetAccountId() != assignment.GetAccountId() {
			return ErrProjectionConflict
		}
	}
	return nil
}

func applyDeltaOperations(candidate *projectionCandidate, delta *cloudpb.PolicyDelta) error {
	for _, operation := range delta.GetAccountOperations() {
		if operation.GetAccountId() == "" {
			return ErrProjectionConflict
		}
		switch operation.GetOperation() {
		case cloudpb.ProjectionDeltaOperation_PROJECTION_DELTA_OPERATION_UPSERT:
			if operation.GetPolicy() == nil || operation.GetPolicy().GetAccountId() != operation.GetAccountId() {
				return ErrProjectionConflict
			}
			candidate.accounts[operation.GetAccountId()] = proto.Clone(operation.GetPolicy()).(*cloudpb.HubAccountPolicy)
		case cloudpb.ProjectionDeltaOperation_PROJECTION_DELTA_OPERATION_REMOVE:
			delete(candidate.accounts, operation.GetAccountId())
		default:
			return ErrProjectionConflict
		}
	}
	for _, operation := range delta.GetDeviceOperations() {
		if operation.GetDeviceId() == "" {
			return ErrProjectionConflict
		}
		switch operation.GetOperation() {
		case cloudpb.ProjectionDeltaOperation_PROJECTION_DELTA_OPERATION_UPSERT:
			if operation.GetPolicy() == nil || operation.GetPolicy().GetDeviceId() != operation.GetDeviceId() {
				return ErrProjectionConflict
			}
			candidate.devices[operation.GetDeviceId()] = proto.Clone(operation.GetPolicy()).(*cloudpb.CloudDevicePolicy)
		case cloudpb.ProjectionDeltaOperation_PROJECTION_DELTA_OPERATION_REMOVE:
			delete(candidate.devices, operation.GetDeviceId())
		default:
			return ErrProjectionConflict
		}
	}
	for _, operation := range delta.GetAssignmentOperations() {
		if operation.GetDaemonDeviceId() == "" {
			return ErrProjectionConflict
		}
		switch operation.GetOperation() {
		case cloudpb.ProjectionDeltaOperation_PROJECTION_DELTA_OPERATION_UPSERT:
			if operation.GetAssignment() == nil || operation.GetAssignment().GetDaemonDeviceId() != operation.GetDaemonDeviceId() {
				return ErrProjectionConflict
			}
			candidate.assignments[operation.GetDaemonDeviceId()] = proto.Clone(operation.GetAssignment()).(*cloudpb.HubAssignment)
		case cloudpb.ProjectionDeltaOperation_PROJECTION_DELTA_OPERATION_REMOVE:
			delete(candidate.assignments, operation.GetDaemonDeviceId())
		default:
			return ErrProjectionConflict
		}
	}
	return nil
}

func (candidate *projectionCandidate) authorizationSnapshot() AuthorizationSnapshot {
	result := AuthorizationSnapshot{Revision: candidate.revision, GeneratedAt: candidate.generatedAt}
	for _, account := range candidate.accounts {
		result.Accounts = append(result.Accounts, AccountAuthorization{AccountID: account.GetAccountId(), AuthEpoch: account.GetAuthEpoch(), Revoked: account.GetRevoked(), EntitlementStatus: account.GetEntitlementStatus(), EntitlementEffectiveUntilUnix: time.UnixMilli(account.GetEntitlementEffectiveUntilUnixMillis()).Unix(), Capability: cloneHubPlanCapability(account.GetCapability())})
	}
	for _, device := range candidate.devices {
		kind := "client"
		if device.GetDeviceKind() == cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON {
			kind = "daemon"
		}
		result.Devices = append(result.Devices, DeviceAuthorization{DeviceID: device.GetDeviceId(), AccountID: device.GetAccountId(), Kind: kind, DisplayName: device.GetDeviceId(), PublicKey: append([]byte(nil), device.GetPublicKey()...), Revoked: device.GetRevoked(), AuthEpoch: device.GetAuthEpoch()})
	}
	return result
}

func (projection *Projection) candidateLocked(revision uint64, generatedMillis, expiresMillis int64) *projectionCandidate {
	candidate := &projectionCandidate{revision: revision, generatedAt: time.UnixMilli(generatedMillis).UTC(), expiresAt: time.UnixMilli(expiresMillis).UTC(), accounts: map[string]*cloudpb.HubAccountPolicy{}, devices: map[string]*cloudpb.CloudDevicePolicy{}, assignments: map[string]*cloudpb.HubAssignment{}}
	for key, value := range projection.accounts {
		candidate.accounts[key] = proto.Clone(value).(*cloudpb.HubAccountPolicy)
	}
	for key, value := range projection.devices {
		candidate.devices[key] = proto.Clone(value).(*cloudpb.CloudDevicePolicy)
	}
	for key, value := range projection.assignments {
		candidate.assignments[key] = proto.Clone(value).(*cloudpb.HubAssignment)
	}
	return candidate
}

func (projection *Projection) installLocked(candidate *projectionCandidate) []expiredAssignment {
	var fenced []expiredAssignment
	for deviceID, current := range projection.assignments {
		next := candidate.assignments[deviceID]
		if next == nil || next.GetAssignmentEpoch() != current.GetAssignmentEpoch() {
			fenced = append(fenced, expiredAssignment{deviceID: deviceID, epoch: current.GetAssignmentEpoch()})
		}
	}
	projection.revision, projection.digest = candidate.revision, append([]byte(nil), candidate.digest...)
	projection.generatedAt, projection.expiresAt = candidate.generatedAt, candidate.expiresAt
	projection.accounts, projection.devices, projection.assignments = candidate.accounts, candidate.devices, candidate.assignments
	projection.scheduleExpiryLocked()
	return fenced
}

func (projection *Projection) scheduleExpiryLocked() {
	if projection.timer != nil {
		projection.timer.Stop()
		projection.timer = nil
	}
	if projection.closed || len(projection.assignments) == 0 {
		return
	}
	now := projection.clock.Now().UTC()
	var next time.Time
	for _, assignment := range projection.assignments {
		expires := time.UnixMilli(assignment.GetExpiresAtUnixMillis()).UTC()
		if next.IsZero() || expires.Before(next) {
			next = expires
		}
	}
	delay := next.Sub(now)
	if delay < 0 {
		delay = 0
	}
	projection.timer = time.AfterFunc(delay, projection.expireAssignments)
}

func (projection *Projection) expireAssignments() {
	projection.mu.Lock()
	if projection.closed {
		projection.mu.Unlock()
		return
	}
	nowMillis := projection.clock.Now().UTC().UnixMilli()
	var expired []expiredAssignment
	for deviceID, assignment := range projection.assignments {
		if assignment.GetExpiresAtUnixMillis() <= nowMillis {
			expired = append(expired, expiredAssignment{deviceID: deviceID, epoch: assignment.GetAssignmentEpoch()})
			delete(projection.assignments, deviceID)
		}
	}
	projection.scheduleExpiryLocked()
	projection.mu.Unlock()
	projection.fenceAssignments(expired)
}

type expiredAssignment struct {
	deviceID string
	epoch    uint64
}

func (projection *Projection) fenceAssignments(values []expiredAssignment) {
	if projection.fence == nil {
		return
	}
	for _, value := range values {
		projection.fence.FenceAssignment(value.deviceID, value.epoch)
	}
}
