// Package usage 验证 Relay 上报并按 managed session/route 幂等结算。
//
// Relay Mesh 的每个 hop 可以独立上报，但账单窗口按各 hop 最大值归并一次，
// 不能把同一批传输字节按 hop 数重复计费。
package usage

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
)

const usageEventPrefix = "TXUE1"

var (
	// ErrInvalidEvent 表示 usage event 字段、签名或 lease binding 非法。
	ErrInvalidEvent = errors.New("invalid relay usage event")
	// ErrDuplicateConflict 表示相同幂等 key 被不同 event body 重用。
	ErrDuplicateConflict = errors.New("relay usage idempotency conflict")
	// ErrSequenceRollback 表示 relay 对同一 lease 的 sequence 回退或乱序。
	ErrSequenceRollback = errors.New("relay usage sequence rollback")
	// ErrUsageOutOfRange 表示 event 超出 lease 时间、带宽或总字节配额。
	ErrUsageOutOfRange = errors.New("relay usage outside lease bounds")
)

// Event 是 Relay 对单个 lease、hop 和时间窗口的签名计量记录。
// Event 只含流量 metadata，不含 grant、terminal、DataChannel payload 或屏幕内容。
type Event struct {
	EventID           string                          `json:"event_id"`
	LeaseID           string                          `json:"lease_id"`
	ManagedSessionID  string                          `json:"managed_session_id"`
	RelayID           string                          `json:"relay_id"`
	RouteID           string                          `json:"route_id,omitempty"`
	PathKind          servicecredential.RelayPathKind `json:"path_kind"`
	HopID             string                          `json:"hop_id"`
	Sequence          uint64                          `json:"sequence"`
	IntervalStartUnix int64                           `json:"interval_start_unix"`
	IntervalEndUnix   int64                           `json:"interval_end_unix"`
	BytesUp           uint64                          `json:"bytes_up"`
	BytesDown         uint64                          `json:"bytes_down"`
	ActiveSeconds     uint64                          `json:"active_seconds"`
	TerminationReason string                          `json:"termination_reason,omitempty"`
	KeyID             string                          `json:"key_id"`
	Signature         []byte                          `json:"signature,omitempty"`
}

// SignEvent 使用 Relay 自身 Ed25519 key 对固定 TXUE1 canonical JSON 签名。
// KeyID 会由 signer 覆盖，调用方不能选择算法或伪造其他 Relay 的 key binding。
func SignEvent(event Event, signer servicecredential.Signer, signedAt time.Time) (Event, error) {
	event.KeyID = signer.KeyID()
	event.Signature = nil
	canonical, err := canonicalEvent(event)
	if err != nil {
		return Event{}, err
	}
	signature, err := signer.Sign(canonical, signedAt)
	if err != nil {
		return Event{}, err
	}
	event.Signature = signature
	return event, nil
}

// SessionUsage 是按 managed session 和 route 聚合一次的可计费 usage。
// Relay Mesh 多 hop event 只会更新窗口最大值，不会把同一流量机械相加。
type SessionUsage struct {
	ManagedSessionID string
	RouteID          string
	BytesUp          uint64
	BytesDown        uint64
	ActiveSeconds    uint64
}

// ApplyResult 描述一次 usage ingest 的结算结果。
// Duplicate 为 true 表示 event 已处理且 body 完全一致，Aggregate 不会重复增长。
type ApplyResult struct {
	Duplicate bool
	Aggregate SessionUsage
}

// Ledger 是 usage event 的并发安全幂等结算器。
// KeyRing 验证 Relay 签名，RelayKeyIDs 把 RelayID 固定到部署公钥，MaxReportDelay 允许租约结束后补报。
type Ledger struct {
	mu             sync.Mutex
	keyRing        *servicecredential.KeyRing
	relayKeyIDs    map[string]string
	maxReportDelay time.Duration
	seen           map[idempotencyKey][32]byte
	lastSequence   map[sequenceKey]uint64
	windows        map[windowKey]map[hopKey]windowUsage
	aggregates     map[aggregateKey]SessionUsage
	leaseBytes     map[string]uint64
}

type idempotencyKey struct {
	relayID  string
	leaseID  string
	sequence uint64
}

type sequenceKey struct {
	relayID string
	leaseID string
}

type windowKey struct {
	leaseID string
	routeID string
	start   int64
	end     int64
}

type hopKey struct {
	relayID string
	hopID   string
}

type windowUsage struct {
	bytesUp       uint64
	bytesDown     uint64
	activeSeconds uint64
}

type aggregateKey struct {
	managedSessionID string
	routeID          string
}

// NewLedger 创建 usage ledger 并复制 RelayID 到 KeyID 的部署绑定。
// report delay 必须非负；缺少显式 Relay key binding 的 event 会被拒绝。
func NewLedger(keyRing *servicecredential.KeyRing, relayKeyIDs map[string]string, maxReportDelay time.Duration) (*Ledger, error) {
	if keyRing == nil || maxReportDelay < 0 {
		return nil, fmt.Errorf("invalid usage ledger configuration")
	}
	bindings := make(map[string]string, len(relayKeyIDs))
	for relayID, keyID := range relayKeyIDs {
		if relayID == "" || keyID == "" {
			return nil, fmt.Errorf("invalid Relay signing key binding")
		}
		bindings[relayID] = keyID
	}
	return &Ledger{
		keyRing:        keyRing,
		relayKeyIDs:    bindings,
		maxReportDelay: maxReportDelay,
		seen:           make(map[idempotencyKey][32]byte),
		lastSequence:   make(map[sequenceKey]uint64),
		windows:        make(map[windowKey]map[hopKey]windowUsage),
		aggregates:     make(map[aggregateKey]SessionUsage),
		leaseBytes:     make(map[string]uint64),
	}, nil
}

// Apply 验签、检查 lease bounds，并以 `(relay_id, lease_id, sequence)` 幂等写入账本。
// 延迟补报只要求 event 窗口位于 lease 内且上报时间不超过 MaxReportDelay；sequence 仍必须单调。
func (ledger *Ledger) Apply(lease servicecredential.RelayLeaseClaims, event Event, now time.Time) (ApplyResult, error) {
	canonical, err := canonicalEvent(event)
	if err != nil {
		return ApplyResult{}, err
	}
	expectedKeyID, ok := ledger.relayKeyIDs[event.RelayID]
	if !ok || expectedKeyID != event.KeyID {
		return ApplyResult{}, ErrInvalidEvent
	}
	if err := ledger.keyRing.Verify(event.KeyID, canonical, event.Signature, now); err != nil {
		return ApplyResult{}, fmt.Errorf("verify usage signature: %w", err)
	}
	if err := validateEventAgainstLease(lease, event, now, ledger.maxReportDelay); err != nil {
		return ApplyResult{}, err
	}
	digest := sha256.Sum256(canonical)
	idempotency := idempotencyKey{relayID: event.RelayID, leaseID: event.LeaseID, sequence: event.Sequence}
	sequence := sequenceKey{relayID: event.RelayID, leaseID: event.LeaseID}
	window := windowKey{leaseID: event.LeaseID, routeID: event.RouteID, start: event.IntervalStartUnix, end: event.IntervalEndUnix}
	hop := hopKey{relayID: event.RelayID, hopID: event.HopID}
	aggregateID := aggregateKey{managedSessionID: event.ManagedSessionID, routeID: event.RouteID}

	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if existing, exists := ledger.seen[idempotency]; exists {
		if existing != digest {
			return ApplyResult{}, ErrDuplicateConflict
		}
		return ApplyResult{Duplicate: true, Aggregate: ledger.aggregates[aggregateID]}, nil
	}
	if last := ledger.lastSequence[sequence]; event.Sequence <= last {
		return ApplyResult{}, ErrSequenceRollback
	}
	hops := ledger.windows[window]
	if hops == nil {
		hops = make(map[hopKey]windowUsage)
	}
	if _, exists := hops[hop]; exists {
		return ApplyResult{}, ErrDuplicateConflict
	}
	before := maximumWindowUsage(hops)
	hops[hop] = windowUsage{bytesUp: event.BytesUp, bytesDown: event.BytesDown, activeSeconds: event.ActiveSeconds}
	after := maximumWindowUsage(hops)
	deltaBytes := after.bytesUp - before.bytesUp + after.bytesDown - before.bytesDown
	if ledger.leaseBytes[event.LeaseID]+deltaBytes > lease.MaxBytes {
		return ApplyResult{}, ErrUsageOutOfRange
	}

	// 只有全部校验通过后才提交 maps，避免失败事件污染 sequence 或幂等真值。
	ledger.windows[window] = hops
	ledger.seen[idempotency] = digest
	ledger.lastSequence[sequence] = event.Sequence
	ledger.leaseBytes[event.LeaseID] += deltaBytes
	aggregate := ledger.aggregates[aggregateID]
	aggregate.ManagedSessionID = event.ManagedSessionID
	aggregate.RouteID = event.RouteID
	aggregate.BytesUp += after.bytesUp - before.bytesUp
	aggregate.BytesDown += after.bytesDown - before.bytesDown
	aggregate.ActiveSeconds += after.activeSeconds - before.activeSeconds
	ledger.aggregates[aggregateID] = aggregate
	return ApplyResult{Aggregate: aggregate}, nil
}

// Aggregate 返回某个 managed session/route 已结算的 usage 快照。
// 查询不需要 terminal identity；空 route ID 表示 single-relay session。
func (ledger *Ledger) Aggregate(managedSessionID, routeID string) SessionUsage {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	return ledger.aggregates[aggregateKey{managedSessionID: managedSessionID, routeID: routeID}]
}

func canonicalEvent(event Event) ([]byte, error) {
	if event.KeyID == "" || len(event.Signature) != 0 && len(event.Signature) != 64 {
		return nil, ErrInvalidEvent
	}
	event.Signature = nil
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, ErrInvalidEvent
	}
	return []byte(usageEventPrefix + "." + base64.RawURLEncoding.EncodeToString(payload)), nil
}

func validateEventAgainstLease(lease servicecredential.RelayLeaseClaims, event Event, now time.Time, maxReportDelay time.Duration) error {
	if event.EventID == "" || event.LeaseID == "" || event.ManagedSessionID == "" || event.RelayID == "" || event.HopID == "" || event.Sequence == 0 || event.IntervalEndUnix <= event.IntervalStartUnix {
		return ErrInvalidEvent
	}
	if event.LeaseID != lease.LeaseID || event.ManagedSessionID != lease.ManagedSessionID || event.RouteID != lease.RouteID || event.PathKind != lease.PathKind {
		return ErrInvalidEvent
	}
	if lease.PathKind == servicecredential.RelayPathMesh && event.RouteID == "" || lease.PathKind == servicecredential.RelayPathSingle && event.RouteID != "" {
		return ErrInvalidEvent
	}
	if event.IntervalStartUnix < lease.NotBeforeUnix || event.IntervalEndUnix > lease.ExpiresAtUnix || time.Unix(event.IntervalEndUnix, 0).After(now) || now.After(time.Unix(lease.ExpiresAtUnix, 0).Add(maxReportDelay)) {
		return ErrUsageOutOfRange
	}
	durationSeconds := uint64(event.IntervalEndUnix - event.IntervalStartUnix)
	if event.ActiveSeconds > durationSeconds {
		return ErrUsageOutOfRange
	}
	bytesPerSecond := uint64(lease.MaxBitrateKbps) * 125
	if bytesPerSecond == 0 || durationSeconds > math.MaxUint64/bytesPerSecond {
		return ErrUsageOutOfRange
	}
	maxBytesPerDirection := bytesPerSecond * durationSeconds
	if event.BytesUp > maxBytesPerDirection || event.BytesDown > maxBytesPerDirection || event.BytesUp > lease.MaxBytes || event.BytesDown > lease.MaxBytes-event.BytesUp {
		return ErrUsageOutOfRange
	}
	return nil
}

func maximumWindowUsage(hops map[hopKey]windowUsage) windowUsage {
	keys := make([]hopKey, 0, len(hops))
	for key := range hops {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].relayID == keys[right].relayID {
			return keys[left].hopID < keys[right].hopID
		}
		return keys[left].relayID < keys[right].relayID
	})
	var maximum windowUsage
	for _, key := range keys {
		value := hops[key]
		if value.bytesUp > maximum.bytesUp {
			maximum.bytesUp = value.bytesUp
		}
		if value.bytesDown > maximum.bytesDown {
			maximum.bytesDown = value.bytesDown
		}
		if value.activeSeconds > maximum.activeSeconds {
			maximum.activeSeconds = value.activeSeconds
		}
	}
	return maximum
}
