package runtime

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/anytty/anytty/cloud/edge/policy"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type relayLease struct {
	claims   *cloudv1.RelayLeaseClaims
	material *cloudv1.RelayICEConfig
	limiter  *policy.LeaseLimiter
}

type relayReservation struct {
	username string
	claims   *cloudv1.RelayLeaseClaims
	created  time.Time
}

type relayAllocation struct {
	summary *cloudv1.RelayAllocationSummary
	claims  *cloudv1.RelayLeaseClaims
}

// RegisterRelayLease 把已经由 Edge 验签的租约和派生 credential 写入纯内存 Runtime。
func (state *State) RegisterRelayLease(ctx context.Context, claims *cloudv1.RelayLeaseClaims, material *cloudv1.RelayICEConfig) error {
	if claims == nil || material == nil || strings.TrimSpace(material.GetUsername()) == "" || strings.TrimSpace(material.GetCredential()) == "" ||
		claims.GetLeaseId() != material.GetLeaseId() || claims.GetExpiresAt() == nil || material.GetExpiresAt() == nil || !claims.GetExpiresAt().AsTime().Equal(material.GetExpiresAt().AsTime()) {
		return errors.New("RelayLease claims and credential material do not match")
	}
	claimsClone := proto.Clone(claims).(*cloudv1.RelayLeaseClaims)
	materialClone := proto.Clone(material).(*cloudv1.RelayICEConfig)
	return state.mutate(ctx, func(data *stateData) error {
		data.expireRelayLeases(time.Now().UTC())
		if current, exists := data.relayLeases[materialClone.GetUsername()]; exists && current.claims.GetLeaseId() == claimsClone.GetLeaseId() {
			current.claims, current.material = claimsClone, materialClone
			data.relayLeases[materialClone.GetUsername()] = current
			return nil
		}
		limiter, err := policy.NewLeaseLimiter(claimsClone.GetExpiresAt().AsTime(), claimsClone.GetMaxBytes(), claimsClone.GetMaxRateBytesPerSecond(), time.Now().UTC())
		if err != nil {
			return err
		}
		if _, err := data.restrictRelayRate(data.accountRates, claimsClone.GetAccountId(), claimsClone); err != nil {
			return err
		}
		if _, err := data.restrictRelayRate(data.sessionRates, claimsClone.GetSessionId(), claimsClone); err != nil {
			return err
		}
		data.relayLeases[materialClone.GetUsername()] = relayLease{claims: claimsClone, material: materialClone, limiter: limiter}
		return nil
	})
}

// RenewRelayLease atomically extends the policy attached to an existing TURN
// credential. The credential and cumulative byte usage remain unchanged.
func (state *State) RenewRelayLease(ctx context.Context, username string, claims *cloudv1.RelayLeaseClaims) (*cloudv1.RelayICEConfig, error) {
	username = strings.TrimSpace(username)
	if username == "" || claims == nil || claims.GetIssuedAt() == nil || claims.GetExpiresAt() == nil ||
		claims.GetIssuedAt().CheckValid() != nil || claims.GetExpiresAt().CheckValid() != nil || claims.GetMaxBytes() == 0 ||
		claims.GetMaxRateBytesPerSecond() == 0 || claims.GetMaxConcurrentAllocations() == 0 {
		return nil, errors.New("RelayLease renewal claims are incomplete")
	}
	claimsClone := proto.Clone(claims).(*cloudv1.RelayLeaseClaims)
	var renewed *cloudv1.RelayICEConfig
	err := state.mutate(ctx, func(data *stateData) error {
		now := time.Now().UTC()
		current, exists := data.relayLeases[username]
		if !exists || current.claims.GetExpiresAt() == nil || !current.claims.GetExpiresAt().AsTime().After(now) {
			return errors.New("RelayLease renewal targets an unavailable or expired credential")
		}
		if !sameRelayLeaseIdentity(current.claims, claimsClone) {
			return errors.New("RelayLease renewal changed its bound identity")
		}
		if !claimsClone.GetExpiresAt().AsTime().After(current.claims.GetExpiresAt().AsTime()) {
			return errors.New("RelayLease renewal did not extend expiry")
		}
		if data.leaseAllocations[current.claims.GetLeaseId()] > claimsClone.GetMaxConcurrentAllocations() {
			return errors.New("RelayLease renewal concurrency is below active allocations")
		}
		if _, err := data.restrictRelayRate(data.accountRates, claimsClone.GetAccountId(), claimsClone); err != nil {
			return err
		}
		if _, err := data.restrictRelayRate(data.sessionRates, claimsClone.GetSessionId(), claimsClone); err != nil {
			return err
		}
		if err := current.limiter.Renew(claimsClone.GetExpiresAt().AsTime(), claimsClone.GetMaxBytes(), claimsClone.GetMaxRateBytesPerSecond(), now); err != nil {
			return err
		}
		material := proto.Clone(current.material).(*cloudv1.RelayICEConfig)
		material.LeaseId = claimsClone.GetLeaseId()
		material.ExpiresAt = timestamppb.New(claimsClone.GetExpiresAt().AsTime())
		current.claims, current.material = claimsClone, material
		data.relayLeases[username] = current
		for reservationID, reservation := range data.relayReservations {
			if reservation.username == username {
				reservation.claims = claimsClone
				data.relayReservations[reservationID] = reservation
			}
		}
		for allocationID, allocation := range data.allocations {
			if allocation.claims.GetLeaseId() == claimsClone.GetLeaseId() && allocation.claims.GetSessionId() == claimsClone.GetSessionId() {
				allocation.claims = claimsClone
				data.allocations[allocationID] = allocation
			}
		}
		renewed = proto.Clone(material).(*cloudv1.RelayICEConfig)
		return nil
	})
	return renewed, err
}

// RevokeRelaySession removes every credential for a closed ClientGateway
// session before physical allocations are settled, preventing recreation races.
func (state *State) RevokeRelaySession(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("Relay session identity is required")
	}
	return state.mutate(ctx, func(data *stateData) error {
		for username, lease := range data.relayLeases {
			if lease.claims.GetSessionId() == sessionID {
				delete(data.relayLeases, username)
			}
		}
		for reservationID, reservation := range data.relayReservations {
			if reservation.claims.GetSessionId() == sessionID {
				delete(data.relayReservations, reservationID)
				data.decrementRelayCounters(reservation.claims)
			}
		}
		return nil
	})
}

func sameRelayLeaseIdentity(current, renewed *cloudv1.RelayLeaseClaims) bool {
	return current != nil && renewed != nil &&
		current.GetLeaseId() == renewed.GetLeaseId() && current.GetAccountId() == renewed.GetAccountId() &&
		current.GetEdgeId() == renewed.GetEdgeId() && current.GetDaemonId() == renewed.GetDaemonId() &&
		current.GetClientId() == renewed.GetClientId() && current.GetSessionId() == renewed.GetSessionId()
}

func (data *stateData) expireRelayLeases(now time.Time) {
	for username, lease := range data.relayLeases {
		if lease.claims.GetExpiresAt() == nil || !lease.claims.GetExpiresAt().AsTime().After(now) {
			delete(data.relayLeases, username)
		}
	}
	for key, limiter := range data.accountRates {
		if limiter.Expired(now) {
			delete(data.accountRates, key)
		}
	}
	for key, limiter := range data.sessionRates {
		if limiter.Expired(now) {
			delete(data.sessionRates, key)
		}
	}
}

// RelayAuth 查询当前 username 对应的活跃租约和密码；过期租约不会被 TURN AuthHandler 接受。
func (state *State) RelayAuth(ctx context.Context, username string, now time.Time) (*cloudv1.RelayLeaseClaims, string, bool, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, "", false, nil
	}
	type result struct {
		claims   *cloudv1.RelayLeaseClaims
		password string
		ok       bool
	}
	reply := make(chan result, 1)
	if err := state.submit(ctx, func(data *stateData) {
		lease, exists := data.relayLeases[username]
		if !exists || lease.claims.GetExpiresAt() == nil || !lease.claims.GetExpiresAt().AsTime().After(now.UTC()) {
			delete(data.relayLeases, username)
			reply <- result{}
			return
		}
		reply <- result{claims: proto.Clone(lease.claims).(*cloudv1.RelayLeaseClaims), password: lease.material.GetCredential(), ok: true}
	}); err != nil {
		return nil, "", false, err
	}
	select {
	case <-ctx.Done():
		return nil, "", false, ctx.Err()
	case <-state.done:
		return nil, "", false, ErrStateClosed
	case value := <-reply:
		return value.claims, value.password, value.ok, nil
	}
}

// ReserveRelayAllocation 在 Pion 创建 socket 前按 account/lease/session 三层原子占用并发配额。
func (state *State) ReserveRelayAllocation(ctx context.Context, reservationID, username string, now time.Time) (policy.RelayAdmission, error) {
	reservationID, username = strings.TrimSpace(reservationID), strings.TrimSpace(username)
	if reservationID == "" || username == "" {
		return policy.RelayAdmission{}, errors.New("Relay allocation reservation and username are required")
	}
	type result struct {
		admission policy.RelayAdmission
		err       error
	}
	reply := make(chan result, 1)
	if err := state.submit(ctx, func(data *stateData) {
		data.expireRelayReservations(now.UTC().Add(-10 * time.Second))
		if _, exists := data.relayReservations[reservationID]; exists {
			reply <- result{err: errors.New("Relay allocation reservation already exists")}
			return
		}
		lease, exists := data.relayLeases[username]
		if !exists || lease.claims.GetExpiresAt() == nil || !lease.claims.GetExpiresAt().AsTime().After(now.UTC()) {
			reply <- result{err: errors.New("RelayLease is unavailable or expired")}
			return
		}
		claims := lease.claims
		limit := claims.GetMaxConcurrentAllocations()
		if data.accountAllocations[claims.GetAccountId()] >= limit || data.leaseAllocations[claims.GetLeaseId()] >= limit || data.sessionAllocations[claims.GetSessionId()] >= limit {
			reply <- result{err: errors.New("Relay allocation concurrency limit reached")}
			return
		}
		data.relayReservations[reservationID] = relayReservation{username: username, claims: proto.Clone(claims).(*cloudv1.RelayLeaseClaims), created: now.UTC()}
		data.incrementRelayCounters(claims)
		limiter, err := policy.NewAdmissionLimiter(data.accountRates[claims.GetAccountId()], data.sessionRates[claims.GetSessionId()], lease.limiter)
		if err != nil {
			data.decrementRelayCounters(claims)
			delete(data.relayReservations, reservationID)
			reply <- result{err: err}
			return
		}
		reply <- result{admission: policy.RelayAdmission{LeaseID: claims.GetLeaseId(), SessionID: claims.GetSessionId(), MaxBytes: claims.GetMaxBytes(), MaxRateBytesPerSecond: claims.GetMaxRateBytesPerSecond(), MaxConcurrentAllocations: limit, Limiter: limiter}}
	}); err != nil {
		return policy.RelayAdmission{}, err
	}
	select {
	case <-ctx.Done():
		return policy.RelayAdmission{}, ctx.Err()
	case <-state.done:
		return policy.RelayAdmission{}, ErrStateClosed
	case value := <-reply:
		return value.admission, value.err
	}
}

func (data *stateData) restrictRelayRate(limiters map[string]*policy.RateLimiter, key string, claims *cloudv1.RelayLeaseClaims) (*policy.RateLimiter, error) {
	now := time.Now().UTC()
	if current := limiters[key]; current != nil {
		if err := current.Restrict(claims.GetExpiresAt().AsTime(), claims.GetMaxRateBytesPerSecond(), now); err != nil {
			return nil, err
		}
		return current, nil
	}
	limiter, err := policy.NewRateLimiter(claims.GetExpiresAt().AsTime(), claims.GetMaxRateBytesPerSecond(), now)
	if err != nil {
		return nil, err
	}
	limiters[key] = limiter
	return limiter, nil
}

// ActivateRelayAllocation 把成功创建的 TURN allocation 发布成 Runtime projection。
func (state *State) ActivateRelayAllocation(ctx context.Context, reservationID, allocationID string, transport cloudv1.RelayTransport, started time.Time) error {
	reservationID, allocationID = strings.TrimSpace(reservationID), strings.TrimSpace(allocationID)
	if reservationID == "" || allocationID == "" || transport == cloudv1.RelayTransport_RELAY_TRANSPORT_UNSPECIFIED {
		return errors.New("Relay allocation identity and transport are required")
	}
	return state.mutate(ctx, func(data *stateData) error {
		reservation, exists := data.relayReservations[reservationID]
		if !exists {
			return errors.New("Relay allocation reservation is missing")
		}
		if _, exists := data.allocations[allocationID]; exists {
			return errors.New("Relay allocation already exists")
		}
		delete(data.relayReservations, reservationID)
		data.allocationNextGen[allocationID]++
		summary := &cloudv1.RelayAllocationSummary{
			AllocationId: allocationID, SessionId: reservation.claims.GetSessionId(), LeaseId: reservation.claims.GetLeaseId(), AccountId: reservation.claims.GetAccountId(),
			Transport: transport, Generation: data.allocationNextGen[allocationID], StartedAt: timestamppb.New(started.UTC()),
		}
		data.allocations[allocationID] = relayAllocation{summary: summary, claims: reservation.claims}
		data.revision++
		data.publish(&cloudv1.RuntimeDelta{Revision: data.revision, Change: &cloudv1.RuntimeDelta_AllocationUpserted{AllocationUpserted: proto.Clone(summary).(*cloudv1.RelayAllocationSummary)}})
		return nil
	})
}

// CancelRelayAllocationReservation 释放 Pion allocation 创建失败或超时留下的精确配额占用。
func (state *State) CancelRelayAllocationReservation(ctx context.Context, reservationID string) error {
	return state.mutate(ctx, func(data *stateData) error {
		reservation, exists := data.relayReservations[strings.TrimSpace(reservationID)]
		if !exists {
			return nil
		}
		delete(data.relayReservations, strings.TrimSpace(reservationID))
		data.decrementRelayCounters(reservation.claims)
		return nil
	})
}

// CloseRelayAllocation 原子删除 Runtime allocation、冻结字节计数并生成唯一 UsageEvent。
// 调用方必须先把返回事件提交 durable outbox，之后才能经 EdgeControl 发送。
func (state *State) CloseRelayAllocation(ctx context.Context, allocationID string, ingressBytes, egressBytes uint64, ended time.Time) (*cloudv1.UsageEvent, error) {
	allocationID = strings.TrimSpace(allocationID)
	type result struct {
		event *cloudv1.UsageEvent
		err   error
	}
	reply := make(chan result, 1)
	if err := state.submit(ctx, func(data *stateData) {
		allocation, exists := data.allocations[allocationID]
		if !exists {
			reply <- result{err: errors.New("Relay allocation is missing")}
			return
		}
		delete(data.allocations, allocationID)
		data.decrementRelayCounters(allocation.claims)
		data.revision++
		data.publish(&cloudv1.RuntimeDelta{Revision: data.revision, Change: &cloudv1.RuntimeDelta_AllocationRemoved{AllocationRemoved: &cloudv1.RelayAllocationRemoved{AllocationId: allocationID, Generation: allocation.summary.GetGeneration()}}})
		reply <- result{event: &cloudv1.UsageEvent{
			SchemaVersion: 1, EventId: uuid.NewString(), EdgeId: allocation.claims.GetEdgeId(), LeaseId: allocation.claims.GetLeaseId(), AccountId: allocation.claims.GetAccountId(),
			DaemonId: allocation.claims.GetDaemonId(), ClientId: allocation.claims.GetClientId(), SessionId: allocation.claims.GetSessionId(), AllocationId: allocationID,
			Transport: allocation.summary.GetTransport(), IngressBytes: ingressBytes, EgressBytes: egressBytes, StartedAt: allocation.summary.GetStartedAt(), EndedAt: timestamppb.New(ended.UTC()),
		}}
	}); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-state.done:
		return nil, ErrStateClosed
	case value := <-reply:
		return value.event, value.err
	}
}

func (data *stateData) incrementRelayCounters(claims *cloudv1.RelayLeaseClaims) {
	data.accountAllocations[claims.GetAccountId()]++
	data.leaseAllocations[claims.GetLeaseId()]++
	data.sessionAllocations[claims.GetSessionId()]++
}

func (data *stateData) decrementRelayCounters(claims *cloudv1.RelayLeaseClaims) {
	decrementCounter(data.accountAllocations, claims.GetAccountId())
	decrementCounter(data.leaseAllocations, claims.GetLeaseId())
	decrementCounter(data.sessionAllocations, claims.GetSessionId())
}

func (data *stateData) expireRelayReservations(before time.Time) {
	for reservationID, reservation := range data.relayReservations {
		if reservation.created.Before(before) {
			delete(data.relayReservations, reservationID)
			data.decrementRelayCounters(reservation.claims)
		}
	}
}

func decrementCounter(counters map[string]uint32, key string) {
	if counters[key] <= 1 {
		delete(counters, key)
		return
	}
	counters[key]--
}
