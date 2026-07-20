package commandoutbox

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/hubregistry"
	cloudtopology "github.com/lozzow/termx/private/cloud/control-plane/topology"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

const defaultCommandTTL = 5 * time.Minute

// TargetSource 是 command planner 读取持久 authority 与最后可信 topology 的窄端口。
// topology 只用于固定 child target，不能在之后冒充 runtime execution receipt。
type TargetSource interface {
	Device(context.Context, string) (cloudtopology.DeviceOwnership, error)
	Presence(context.Context, string) (string, *cloudpb.PresenceProjection, error)
	PeerSession(context.Context, *cloudpb.ManagedPeerSessionTarget) (cloudtopology.StoredPeerSession, error)
	PeerSessionsForClient(context.Context, string) ([]cloudtopology.StoredPeerSession, error)
	TerminalAccess(context.Context, string, string) (cloudtopology.StoredTerminalAccess, error)
}

// RelayTargetSource 从持久 reservation 解析账号、Hub 与 Relay binding。
type RelayTargetSource interface {
	RelayReservation(context.Context, string) (*cloudpb.RelayLeaseReservation, error)
	RelayReservationsForSession(context.Context, string) ([]*cloudpb.RelayLeaseReservation, error)
}

// MigrationSource 提供 assignment 与当前 Hub control generation 的持久真值。
type MigrationSource interface {
	Assignment(context.Context, string) (hubregistry.Assignment, error)
	Deployment(context.Context, string) (hubregistry.Deployment, error)
}

// Planner 把 generated management request 解析为持久 parent/child CommandOutbox。
// 它不执行网络投递，也不根据后续 topology 变化改写已经固定的 child fencing。
type Planner struct {
	outbox             *Service
	targets            TargetSource
	relayTargets       RelayTargetSource
	migrations         MigrationSource
	random             io.Reader
	ttl                time.Duration
	notifyPolicyChange func(string)
}

// NewPlanner 创建 CommandOutbox target planner。
func NewPlanner(outbox *Service, targets TargetSource, relayTargets RelayTargetSource, random io.Reader, notifyPolicyChange func(string), migrationSources ...MigrationSource) (*Planner, error) {
	if outbox == nil || targets == nil {
		return nil, fmt.Errorf("CommandOutbox planner dependencies are required")
	}
	if random == nil {
		random = rand.Reader
	}
	planner := &Planner{outbox: outbox, targets: targets, relayTargets: relayTargets, random: random, ttl: defaultCommandTTL, notifyPolicyChange: notifyPolicyChange}
	if len(migrationSources) > 0 {
		planner.migrations = migrationSources[0]
	}
	return planner, nil
}

// Create 校验账号隔离与精确 target，随后持久创建 command。
// device revoke 的 authority mutation 与 parent/child insert 由同一 Store 事务提交。
func (planner *Planner) Create(ctx context.Context, request *cloudpb.CreateManagementCommandRequest, actor *cloudpb.ManagementActorProjection, now time.Time) (*cloudpb.ManagementCommandProjection, bool, error) {
	if planner == nil || request == nil || request.GetAccountId() == "" || request.GetIdempotencyKey() == "" || request.GetTarget() == nil || actor == nil || actor.GetActorId() == "" || actor.GetActorKind() == cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_UNSPECIFIED || now.IsZero() {
		return nil, false, ErrCommandConflict
	}
	if existing, err := planner.outbox.ByIdempotency(ctx, request.GetAccountId(), request.GetIdempotencyKey()); err == nil {
		if existing.GetCommandKind() != request.GetCommandKind() || !sameRequestedTarget(existing.GetTarget(), request.GetTarget(), request.GetCommandKind()) {
			return nil, false, ErrCommandConflict
		}
		return existing, false, nil
	} else if !errors.Is(err, ErrCommandNotFound) {
		return nil, false, err
	}
	commandID, err := planner.nextID("command")
	if err != nil {
		return nil, false, err
	}
	projection := &cloudpb.ManagementCommandProjection{CommandId: commandID, AccountId: request.GetAccountId(), Actor: proto.Clone(actor).(*cloudpb.ManagementActorProjection), CommandKind: request.GetCommandKind(), Target: proto.Clone(request.GetTarget()).(*cloudpb.ManagementCommandTarget), AuthorityResult: cloudpb.CommandAuthorityResult_COMMAND_AUTHORITY_RESULT_NOT_APPLICABLE, DeliveryState: cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING, ExecutionState: cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING, ObservedEffect: cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN, CreatedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(planner.ttl).UnixMilli(), UpdatedAtUnixMillis: now.UnixMilli()}

	switch request.GetCommandKind() {
	case cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_KICK_PRESENCE:
		target := request.GetTarget().GetPresence()
		accountID, presence, err := planner.targets.Presence(ctx, target.GetDaemonDeviceId())
		if err != nil || accountID != request.GetAccountId() || !presenceMatchesTarget(presence, target) {
			return nil, false, ErrCommandNotFound
		}
		if err := planner.appendChild(projection, presence.GetControlOwnerHubId(), request.GetTarget(), now); err != nil {
			return nil, false, err
		}
	case cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_CLOSE_MANAGED_PEER_SESSION:
		target := request.GetTarget().GetPeerSession()
		stored, err := planner.targets.PeerSession(ctx, target)
		if err != nil || stored.AccountID != request.GetAccountId() || stored.Value.GetState() == cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_CLOSED {
			return nil, false, ErrCommandNotFound
		}
		if err := planner.appendChild(projection, stored.HubID, request.GetTarget(), now); err != nil {
			return nil, false, err
		}
	case cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_REVOKE_CLOUD_DEVICE:
		target := request.GetTarget().GetCloudDevice()
		ownership, err := planner.targets.Device(ctx, target.GetDeviceId())
		if err != nil || ownership.AccountID != request.GetAccountId() || ownership.AuthEpoch != target.GetExpectedAuthEpoch() || ownership.Revoked {
			return nil, false, ErrCommandNotFound
		}
		projection.AuthorityResult = cloudpb.CommandAuthorityResult_COMMAND_AUTHORITY_RESULT_COMMITTED
		switch ownership.Kind {
		case cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON:
			accountID, presence, presenceErr := planner.targets.Presence(ctx, ownership.DeviceID)
			if presenceErr == nil && accountID == request.GetAccountId() && presence.GetAvailability() == cloudpb.Availability_AVAILABILITY_ONLINE {
				childTarget := &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_Presence{Presence: &cloudpb.KickPresenceTarget{DaemonDeviceId: ownership.DeviceID, AssignmentEpoch: presence.GetAssignmentEpoch(), PresenceSessionId: presence.GetPresenceSessionId()}}}
				if err := planner.appendChild(projection, presence.GetControlOwnerHubId(), childTarget, now); err != nil {
					return nil, false, err
				}
			} else if presenceErr != nil && !errors.Is(presenceErr, cloudtopology.ErrTopologyRejected) {
				return nil, false, presenceErr
			}
		case cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_CLIENT:
			sessions, err := planner.targets.PeerSessionsForClient(ctx, ownership.DeviceID)
			if err != nil {
				return nil, false, err
			}
			for _, session := range sessions {
				if session.AccountID != request.GetAccountId() {
					return nil, false, ErrCommandConflict
				}
				childTarget := &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_PeerSession{PeerSession: proto.Clone(session.Value.GetTarget()).(*cloudpb.ManagedPeerSessionTarget)}}
				if err := planner.appendChild(projection, session.HubID, childTarget, now); err != nil {
					return nil, false, err
				}
			}
		default:
			return nil, false, ErrCommandConflict
		}
		if len(projection.GetChildren()) == 0 {
			projection.DeliveryState = cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_RUNTIME_RECEIVED
			projection.ExecutionState = cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_ALREADY_SATISFIED
		}
		stored, created, err := planner.outbox.CreateDeviceRevoke(ctx, projection, request.GetIdempotencyKey(), ownership.DeviceID, ownership.AuthEpoch, now)
		if err == nil && created && planner.notifyPolicyChange != nil {
			planner.notifyPolicyChange(request.GetAccountId())
		}
		return stored, created, err
	case cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_REVOKE_TERMINAL_ACCESS:
		requested := request.GetTarget().GetTerminalAccess()
		stored, err := planner.targets.TerminalAccess(ctx, requested.GetDaemonDeviceId(), requested.GetOpaqueAccessReference())
		if err != nil || stored.AccountID != request.GetAccountId() || stored.Value.GetState() != cloudpb.TerminalAccessState_TERMINAL_ACCESS_STATE_ACTIVE || stored.Inventory == nil || stored.Inventory.GetAccessProjectionRevision() == 0 {
			return nil, false, ErrCommandNotFound
		}
		childTarget := &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_TerminalAccess{TerminalAccess: &cloudpb.RevokeTerminalAccessTarget{DaemonDeviceId: requested.GetDaemonDeviceId(), OpaqueAccessReference: requested.GetOpaqueAccessReference(), AssignmentEpoch: stored.Inventory.GetAssignmentEpoch(), PresenceSessionId: stored.Inventory.GetControlPresenceSessionId(), DaemonRuntimeGeneration: stored.Inventory.GetDaemonRuntimeGeneration(), AccessProjectionRevision: stored.Inventory.GetAccessProjectionRevision()}}}
		if err := planner.appendChild(projection, stored.HubID, childTarget, now); err != nil {
			return nil, false, err
		}
	case cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_CLOSE_RELAY_ALLOCATIONS:
		if planner.relayTargets == nil {
			return nil, false, ErrCommandConflict
		}
		target := request.GetTarget().GetRelayAllocations()
		if target == nil || target.GetRelayId() == "" || (target.GetLeaseId() == "") == (target.GetManagedSessionId() == "") {
			return nil, false, ErrCommandConflict
		}
		var reservations []*cloudpb.RelayLeaseReservation
		if target.GetLeaseId() != "" {
			reservation, err := planner.relayTargets.RelayReservation(ctx, target.GetLeaseId())
			if err != nil {
				return nil, false, ErrCommandNotFound
			}
			reservations = []*cloudpb.RelayLeaseReservation{reservation}
		} else {
			reservations, err = planner.relayTargets.RelayReservationsForSession(ctx, target.GetManagedSessionId())
			if err != nil {
				return nil, false, ErrCommandNotFound
			}
		}
		seen := map[string]bool{}
		for _, reservation := range reservations {
			if reservation.GetAccountId() != request.GetAccountId() || reservation.GetRelayId() != target.GetRelayId() || reservation.GetState() != cloudpb.RelayReservationState_RELAY_RESERVATION_STATE_ACTIVE || reservation.GetHubId() == "" {
				continue
			}
			key := reservation.GetHubId() + "\x00" + reservation.GetRelayId()
			if !seen[key] {
				seen[key] = true
				if err := planner.appendChild(projection, reservation.GetHubId(), request.GetTarget(), now); err != nil {
					return nil, false, err
				}
			}
		}
		if len(projection.GetChildren()) == 0 {
			return nil, false, ErrCommandNotFound
		}
	case cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_MIGRATE_ASSIGNMENT:
		requested := request.GetTarget().GetAssignmentMigration()
		if planner.migrations == nil || requested == nil || requested.GetDaemonDeviceId() == "" || requested.GetTargetHubId() == "" {
			return nil, false, ErrCommandConflict
		}
		current, err := planner.migrations.Assignment(ctx, requested.GetDaemonDeviceId())
		if err != nil || current.Value.GetAccountId() != request.GetAccountId() || current.Value.GetHubId() == requested.GetTargetHubId() || current.Value.GetExpiresAtUnixMillis() <= now.UnixMilli() {
			return nil, false, ErrCommandNotFound
		}
		sourceDeployment, err := planner.migrations.Deployment(ctx, current.Value.GetHubId())
		if err != nil || !sourceDeployment.Enabled || sourceDeployment.ControlGeneration == 0 {
			return nil, false, ErrCommandConflict
		}
		targetDeployment, err := planner.migrations.Deployment(ctx, requested.GetTargetHubId())
		if err != nil || !targetDeployment.Enabled {
			return nil, false, ErrCommandNotFound
		}
		migrationID, err := planner.nextID("migration")
		if err != nil {
			return nil, false, err
		}
		canonical := &cloudpb.AssignmentMigrationTarget{MigrationId: migrationID, DaemonDeviceId: requested.GetDaemonDeviceId(), SourceHubId: current.Value.GetHubId(), SourceAssignmentEpoch: current.Value.GetAssignmentEpoch(), SourceControlGeneration: sourceDeployment.ControlGeneration, TargetHubId: requested.GetTargetHubId(), TargetAssignmentEpoch: current.Value.GetAssignmentEpoch() + 1, TargetNotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), TargetExpiresAtUnixMillis: now.Add(time.Hour).UnixMilli()}
		projection.Target = &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_AssignmentMigration{AssignmentMigration: canonical}}
		if err := planner.appendChild(projection, current.Value.GetHubId(), projection.GetTarget(), now); err != nil {
			return nil, false, err
		}
	default:
		return nil, false, ErrCommandConflict
	}
	return planner.outbox.Create(ctx, projection, request.GetIdempotencyKey(), now)
}

func sameRequestedTarget(existing, requested *cloudpb.ManagementCommandTarget, kind cloudpb.ManagementCommandKind) bool {
	if kind != cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_MIGRATE_ASSIGNMENT {
		return proto.Equal(existing, requested)
	}
	left, right := existing.GetAssignmentMigration(), requested.GetAssignmentMigration()
	return left != nil && right != nil && left.GetDaemonDeviceId() == right.GetDaemonDeviceId() && left.GetTargetHubId() == right.GetTargetHubId()
}

func (planner *Planner) appendChild(parent *cloudpb.ManagementCommandProjection, hubID string, target *cloudpb.ManagementCommandTarget, now time.Time) error {
	if hubID == "" || target == nil {
		return ErrCommandConflict
	}
	childID, err := planner.nextID("child")
	if err != nil {
		return err
	}
	parent.Children = append(parent.Children, &cloudpb.ManagementCommandChildProjection{ChildCommandId: childID, TargetHubId: hubID, Target: proto.Clone(target).(*cloudpb.ManagementCommandTarget), DeliveryState: cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING, ExecutionState: cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING, ObservedEffect: cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN, UpdatedAtUnixMillis: now.UnixMilli()})
	return nil
}

func (planner *Planner) nextID(prefix string) (string, error) {
	value := make([]byte, 18)
	if _, err := io.ReadFull(planner.random, value); err != nil {
		return "", err
	}
	return prefix + "-" + base64.RawURLEncoding.EncodeToString(value), nil
}

func presenceMatchesTarget(presence *cloudpb.PresenceProjection, target *cloudpb.KickPresenceTarget) bool {
	return presence != nil && target != nil && presence.GetDaemonDeviceId() == target.GetDaemonDeviceId() && presence.GetAssignmentEpoch() == target.GetAssignmentEpoch() && presence.GetPresenceSessionId() == target.GetPresenceSessionId() && presence.GetAvailability() == cloudpb.Availability_AVAILABILITY_ONLINE
}
