package runtime

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/anytty/anytty/cloud/edge/policy"
	"github.com/anytty/anytty/cloud/relayquota"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	MaxPhysicalAllocationsPerReservation = 4
	MaxLiveRelayObjects                  = 4096
)

type relayGroup struct {
	grant                        *cloudv1.RelayGrant
	material                     *cloudv1.RelayICEConfig
	limiter                      *policy.GroupLimiter
	username                     string
	closing                      bool
	pending, active, closeActive uint32
	ingress, egress              uint64
}

type relayPendingAllocation struct {
	groupID string
	created time.Time
}

type relayAllocation struct {
	groupID string
	closing bool
}

func (state *State) RegisterRelayGrant(ctx context.Context, grant *cloudv1.RelayGrant, material *cloudv1.RelayICEConfig) error {
	if err := validateRelayGrantMaterial(grant, material); err != nil {
		return err
	}
	grantClone := proto.Clone(grant).(*cloudv1.RelayGrant)
	materialClone := proto.Clone(material).(*cloudv1.RelayICEConfig)
	return state.mutate(ctx, func(data *stateData) error {
		if _, err := data.requireActiveDaemon(grantClone.GetPolicy().GetDaemonId()); err != nil {
			return err
		}
		now := state.now().UTC()
		if !grantClone.GetAuthorizedUntil().AsTime().After(now) {
			return errors.New("Relay grant is already expired")
		}
		if current := data.relayGroups[grantClone.GetReservationId()]; current != nil {
			if !proto.Equal(current.grant, grantClone) || current.username != materialClone.GetUsername() {
				return errors.New("Relay grant conflicts with an active reservation group")
			}
			return nil
		}
		if data.liveRelayObjects() >= MaxLiveRelayObjects {
			return errors.New("Edge live Relay object limit reached")
		}
		limiter, err := policy.NewGroupLimiter(grantClone.GetAuthorizedUntil().AsTime(), grantClone.GetReservedBytes(), grantClone.GetMaxRateBytesPerSecond(), now)
		if err != nil {
			return err
		}
		group := &relayGroup{grant: grantClone, material: materialClone, limiter: limiter, username: materialClone.GetUsername()}
		data.relayGroups[grantClone.GetReservationId()] = group
		data.relayAuth[materialClone.GetUsername()] = grantClone.GetReservationId()
		return nil
	})
}

func (state *State) RenewRelayGrant(ctx context.Context, username string, grant *cloudv1.RelayGrant) (*cloudv1.RelayICEConfig, error) {
	username = strings.TrimSpace(username)
	if username == "" || grant == nil || grant.GetAuthorizedUntil() == nil {
		return nil, errors.New("Relay renewal grant and credential are required")
	}
	grantClone := proto.Clone(grant).(*cloudv1.RelayGrant)
	var renewed *cloudv1.RelayICEConfig
	err := state.mutate(ctx, func(data *stateData) error {
		group := data.relayGroups[grantClone.GetReservationId()]
		if group == nil || group.username != username || group.closing || group.grant.GetSessionId() != grantClone.GetSessionId() ||
			group.grant.GetReservedBytes() != grantClone.GetReservedBytes() || group.grant.GetMaxRateBytesPerSecond() != grantClone.GetMaxRateBytesPerSecond() ||
			!relayquota.EqualDigest(group.grant.GetPolicyDigest(), grantClone.GetPolicyDigest()) || grantClone.GetRenewSequence() != group.grant.GetRenewSequence()+1 {
			return errors.New("Relay renewal changed reservation authority")
		}
		if err := group.limiter.Renew(grantClone.GetAuthorizedUntil().AsTime(), state.now().UTC()); err != nil {
			return err
		}
		group.grant = grantClone
		group.material.ExpiresAt = timestamppb.New(grantClone.GetAuthorizedUntil().AsTime())
		renewed = proto.Clone(group.material).(*cloudv1.RelayICEConfig)
		return nil
	})
	return renewed, err
}

func (state *State) RelayAuth(ctx context.Context, username string, now time.Time) (*cloudv1.RelayGrant, string, bool, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, "", false, nil
	}
	var grant *cloudv1.RelayGrant
	var password string
	var found bool
	err := state.call(ctx, func(data *stateData) error {
		group := data.relayGroups[data.relayAuth[username]]
		if group == nil || group.closing || !group.grant.GetAuthorizedUntil().AsTime().After(now.UTC()) {
			delete(data.relayAuth, username)
			return nil
		}
		grant = proto.Clone(group.grant).(*cloudv1.RelayGrant)
		password, found = group.material.GetCredential(), true
		return nil
	})
	return grant, password, found, err
}

func (state *State) ReserveRelayAllocation(ctx context.Context, physicalID, username string, now time.Time) (policy.RelayAdmission, error) {
	physicalID, username = strings.TrimSpace(physicalID), strings.TrimSpace(username)
	if physicalID == "" || username == "" {
		return policy.RelayAdmission{}, errors.New("physical allocation reservation and username are required")
	}
	var admission policy.RelayAdmission
	err := state.mutate(ctx, func(data *stateData) error {
		if _, exists := data.relayPending[physicalID]; exists {
			return errors.New("physical allocation reservation already exists")
		}
		groupID := data.relayAuth[username]
		group := data.relayGroups[groupID]
		if group == nil || group.closing || !group.grant.GetAuthorizedUntil().AsTime().After(now.UTC()) {
			return errors.New("Relay grant is unavailable or expired")
		}
		if group.pending+group.active+group.closeActive >= MaxPhysicalAllocationsPerReservation {
			return errors.New("Relay reservation physical allocation limit reached")
		}
		if data.liveRelayObjects() >= MaxLiveRelayObjects {
			return errors.New("Edge live Relay object limit reached")
		}
		data.relayPending[physicalID] = relayPendingAllocation{groupID: groupID, created: now.UTC()}
		group.pending++
		admission = policy.RelayAdmission{ReservationID: groupID, SessionID: group.grant.GetSessionId(), Limiter: group.limiter}
		return nil
	})
	return admission, err
}

func (state *State) ActivateRelayAllocation(ctx context.Context, physicalID, allocationID string, transport cloudv1.RelayTransport, started time.Time) error {
	physicalID, allocationID = strings.TrimSpace(physicalID), strings.TrimSpace(allocationID)
	if physicalID == "" || allocationID == "" || transport == cloudv1.RelayTransport_RELAY_TRANSPORT_UNSPECIFIED || started.IsZero() {
		return errors.New("physical allocation identity, transport, and start time are required")
	}
	return state.mutate(ctx, func(data *stateData) error {
		pending, exists := data.relayPending[physicalID]
		group := data.relayGroups[pending.groupID]
		if !exists || group == nil || group.pending == 0 || group.closing {
			return errors.New("physical allocation reservation is missing or closing")
		}
		if _, exists := data.allocations[allocationID]; exists {
			return errors.New("Relay allocation already exists")
		}
		delete(data.relayPending, physicalID)
		group.pending--
		group.active++
		data.allocations[allocationID] = relayAllocation{groupID: pending.groupID}
		return nil
	})
}

func (state *State) CancelRelayAllocationReservation(ctx context.Context, physicalID string) error {
	physicalID = strings.TrimSpace(physicalID)
	return state.mutate(ctx, func(data *stateData) error {
		pending, exists := data.relayPending[physicalID]
		if !exists {
			return nil
		}
		delete(data.relayPending, physicalID)
		if group := data.relayGroups[pending.groupID]; group != nil && group.pending > 0 {
			group.pending--
		}
		return nil
	})
}

// BeginRelaySessionClose stops admission before TURN pending/active sockets drain.
func (state *State) BeginRelaySessionClose(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("Relay session identity is required")
	}
	return state.mutate(ctx, func(data *stateData) error {
		for _, group := range data.relayGroups {
			if group.grant.GetSessionId() == sessionID {
				group.closing = true
				delete(data.relayAuth, group.username)
			}
		}
		return nil
	})
}

func (state *State) BeginRelayAllocationClose(ctx context.Context, allocationID string) error {
	return state.mutate(ctx, func(data *stateData) error {
		allocationID = strings.TrimSpace(allocationID)
		allocation, exists := data.allocations[allocationID]
		if !exists || allocation.closing {
			return nil
		}
		group := data.relayGroups[allocation.groupID]
		if group == nil || group.active == 0 {
			return errors.New("Relay allocation close invariant failed")
		}
		group.active--
		group.closeActive++
		allocation.closing = true
		data.allocations[allocationID] = allocation
		return nil
	})
}

// CloseRelayAllocation records counters only after the tracked socket is static.
func (state *State) CloseRelayAllocation(ctx context.Context, allocationID string, ingress, egress uint64) error {
	allocationID = strings.TrimSpace(allocationID)
	return state.mutate(ctx, func(data *stateData) error {
		allocation, exists := data.allocations[allocationID]
		if !exists {
			return nil
		}
		group := data.relayGroups[allocation.groupID]
		if group == nil {
			return errors.New("Relay allocation group is missing")
		}
		if ingress > math.MaxUint64-group.ingress || egress > math.MaxUint64-group.egress {
			return errors.New("Relay allocation counters overflow group aggregate")
		}
		group.ingress += ingress
		group.egress += egress
		if allocation.closing {
			if group.closeActive == 0 {
				return errors.New("Relay allocation closing count underflow")
			}
			group.closeActive--
		} else {
			if group.active == 0 {
				return errors.New("Relay allocation active count underflow")
			}
			group.active--
		}
		delete(data.allocations, allocationID)
		return nil
	})
}

func (state *State) RelaySessionSettlement(ctx context.Context, sessionID string, observedAt time.Time) (*cloudv1.RelaySettlement, error) {
	var settlement *cloudv1.RelaySettlement
	err := state.call(ctx, func(data *stateData) error {
		for _, group := range data.relayGroups {
			if group.grant.GetSessionId() != sessionID {
				continue
			}
			if !group.closing || group.pending != 0 || group.active != 0 || group.closeActive != 0 {
				return errors.New("Relay group is not static for settlement")
			}
			if group.ingress > group.grant.GetReservedBytes() || group.egress > group.grant.GetReservedBytes()-group.ingress {
				return errors.New("Relay aggregate exceeds reserved bytes")
			}
			settlement = &cloudv1.RelaySettlement{ReservationId: group.grant.GetReservationId(), Kind: cloudv1.RelaySettlementKind_RELAY_SETTLEMENT_KIND_EXACT, IngressBytes: group.ingress, EgressBytes: group.egress, PolicyDigest: append([]byte(nil), group.grant.GetPolicyDigest()...), ObservedAt: timestamppb.New(observedAt.UTC())}
			return nil
		}
		return nil
	})
	return settlement, err
}

func (state *State) ForgetRelayGroup(ctx context.Context, reservationID string) error {
	reservationID = strings.TrimSpace(reservationID)
	return state.mutate(ctx, func(data *stateData) error {
		group := data.relayGroups[reservationID]
		if group == nil {
			return nil
		}
		if !group.closing || group.pending != 0 || group.active != 0 || group.closeActive != 0 {
			return errors.New("live Relay group cannot be forgotten")
		}
		delete(data.relayAuth, group.username)
		delete(data.relayGroups, reservationID)
		return nil
	})
}

func (state *State) RelayReservationLive(ctx context.Context, reservationID string) (bool, error) {
	reservationID = strings.TrimSpace(reservationID)
	live := false
	err := state.call(ctx, func(data *stateData) error {
		live = data.relayGroups[reservationID] != nil
		return nil
	})
	return live, err
}

func validateRelayGrantMaterial(grant *cloudv1.RelayGrant, material *cloudv1.RelayICEConfig) error {
	if grant == nil || grant.GetPolicy() == nil || material == nil || strings.TrimSpace(material.GetUsername()) == "" || strings.TrimSpace(material.GetCredential()) == "" || grant.GetReservationId() != material.GetReservationId() || grant.GetAuthorizedUntil() == nil || material.GetExpiresAt() == nil || !grant.GetAuthorizedUntil().AsTime().Equal(material.GetExpiresAt().AsTime()) {
		return errors.New("Relay grant and credential material do not match")
	}
	digest, err := relayquota.PolicyDigest(grant.GetPolicy())
	if err != nil || !relayquota.EqualDigest(digest, grant.GetPolicyDigest()) {
		return errors.New("Relay grant policy digest is invalid")
	}
	return nil
}

func (data *stateData) liveRelayObjects() int {
	return len(data.relayGroups) + len(data.relayPending) + len(data.allocations)
}
