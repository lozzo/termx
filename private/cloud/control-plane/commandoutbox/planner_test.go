package commandoutbox_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/commandoutbox"
	"github.com/lozzow/termx/private/cloud/control-plane/hubregistry"
	"github.com/lozzow/termx/private/cloud/control-plane/relayquota"
	cloudsqlite "github.com/lozzow/termx/private/cloud/control-plane/sqlite"
	cloudtopology "github.com/lozzow/termx/private/cloud/control-plane/topology"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestPlannerClientRevokeCommitsAuthorityAndFansOutAcrossHubs(t *testing.T) {
	store, err := cloudsqlite.Open(filepath.Join(t.TempDir(), "controller.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.PutDeviceOwnership(context.Background(), cloudtopology.DeviceOwnership{DeviceID: "client-1", AccountID: "account-1", Kind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_CLIENT, AuthEpoch: 4}); err != nil {
		t.Fatal(err)
	}
	outbox, _ := commandoutbox.New(store)
	source := &plannerSource{device: cloudtopology.DeviceOwnership{DeviceID: "client-1", AccountID: "account-1", Kind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_CLIENT, AuthEpoch: 4}, sessions: []cloudtopology.StoredPeerSession{
		{AccountID: "account-1", HubID: "hub-a", Value: plannerSession("daemon-a", "managed-a", 1, "presence-a", "runtime-a", "client-1")},
		{AccountID: "account-1", HubID: "hub-b", Value: plannerSession("daemon-b", "managed-b", 2, "presence-b", "runtime-b", "client-1")},
	}}
	randomBytes := make([]byte, 108)
	for index := range randomBytes {
		randomBytes[index] = byte(index + 1)
	}
	planner, _ := commandoutbox.NewPlanner(outbox, source, nil, bytes.NewReader(randomBytes), nil)
	now := time.Date(2026, 7, 20, 13, 0, 0, 0, time.UTC)
	target := &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_CloudDevice{CloudDevice: &cloudpb.RevokeCloudDeviceTarget{DeviceId: "client-1", ExpectedAuthEpoch: 4}}}
	created, inserted, err := planner.Create(context.Background(), &cloudpb.CreateManagementCommandRequest{AccountId: "account-1", CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_REVOKE_CLOUD_DEVICE, Target: target, IdempotencyKey: "idem-1"}, &cloudpb.ManagementActorProjection{ActorKind: cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_ACCOUNT_OWNER, ActorId: "user-1"}, now)
	if err != nil || !inserted || created.GetAuthorityResult() != cloudpb.CommandAuthorityResult_COMMAND_AUTHORITY_RESULT_COMMITTED || len(created.GetChildren()) != 2 || created.GetChildren()[0].GetTargetHubId() == created.GetChildren()[1].GetTargetHubId() {
		t.Fatalf("Create() = (%v, %v, %v)", created, inserted, err)
	}
	ownership, err := store.DeviceOwnership(context.Background(), "client-1")
	if err != nil || !ownership.Revoked || ownership.AuthEpoch != 5 {
		t.Fatalf("device authority = (%+v, %v)", ownership, err)
	}
	source.device = ownership
	replayed, inserted, err := planner.Create(context.Background(), &cloudpb.CreateManagementCommandRequest{AccountId: "account-1", CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_REVOKE_CLOUD_DEVICE, Target: target, IdempotencyKey: "idem-1"}, &cloudpb.ManagementActorProjection{ActorKind: cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_ACCOUNT_OWNER, ActorId: "user-1"}, now)
	if err != nil || inserted || replayed.GetCommandId() != created.GetCommandId() {
		t.Fatalf("replay Create() = (%v, %v, %v)", replayed, inserted, err)
	}
}

func TestPlannerDaemonRevokeUsesExactPresenceAndHubReceiptCompletesEnforcement(t *testing.T) {
	store, err := cloudsqlite.Open(filepath.Join(t.TempDir(), "controller.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	device := cloudtopology.DeviceOwnership{DeviceID: "daemon-1", AccountID: "account-1", Kind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: 2}
	if err := store.PutDeviceOwnership(context.Background(), device); err != nil {
		t.Fatal(err)
	}
	outbox, _ := commandoutbox.New(store)
	source := &plannerSource{device: device, presence: &cloudpb.PresenceProjection{DaemonDeviceId: "daemon-1", ControlOwnerHubId: "hub-a", AssignmentEpoch: 8, PresenceSessionId: "presence-8", Availability: cloudpb.Availability_AVAILABILITY_ONLINE}}
	randomBytes := make([]byte, 36)
	for index := range randomBytes {
		randomBytes[index] = byte(index + 11)
	}
	planner, _ := commandoutbox.NewPlanner(outbox, source, nil, bytes.NewReader(randomBytes), nil)
	now := time.Date(2026, 7, 20, 13, 30, 0, 0, time.UTC)
	target := &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_CloudDevice{CloudDevice: &cloudpb.RevokeCloudDeviceTarget{DeviceId: "daemon-1", ExpectedAuthEpoch: 2}}}
	created, _, err := planner.Create(context.Background(), &cloudpb.CreateManagementCommandRequest{AccountId: "account-1", CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_REVOKE_CLOUD_DEVICE, Target: target, IdempotencyKey: "daemon-revoke"}, &cloudpb.ManagementActorProjection{ActorKind: cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_ACCOUNT_OWNER, ActorId: "user-1"}, now)
	if err != nil || len(created.GetChildren()) != 1 || created.GetChildren()[0].GetTarget().GetPresence().GetPresenceSessionId() != "presence-8" {
		t.Fatalf("daemon revoke = (%v, %v)", created, err)
	}
	completed, _, err := outbox.ApplyHubResult(context.Background(), &cloudpb.HubCommandResult{CommandId: created.GetChildren()[0].GetChildCommandId(), HubId: "hub-a", ControlGeneration: 3, ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED, CompletedAtUnixMillis: now.Add(time.Second).UnixMilli()}, now.Add(time.Second))
	if err != nil || completed.GetAuthorityResult() != cloudpb.CommandAuthorityResult_COMMAND_AUTHORITY_RESULT_COMMITTED || completed.GetExecutionState() != cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_APPLIED {
		t.Fatalf("daemon revoke result = (%v, %v)", completed, err)
	}
}

func TestPlannerTerminalRevokeUsesOpaqueProjectionAndExactInventoryFence(t *testing.T) {
	store, err := cloudsqlite.Open(filepath.Join(t.TempDir(), "controller.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	outbox, _ := commandoutbox.New(store)
	source := &plannerSource{terminal: cloudtopology.StoredTerminalAccess{
		AccountID: "account-1", HubID: "hub-a",
		Value:     &cloudpb.TerminalAccessProjection{DaemonDeviceId: "daemon-1", OpaqueAccessReference: "opaque-1", State: cloudpb.TerminalAccessState_TERMINAL_ACCESS_STATE_ACTIVE, AccessProjectionRevision: 12},
		Inventory: &cloudpb.TerminalAccessInventorySnapshot{DaemonDeviceId: "daemon-1", ControlOwnerHubId: "hub-a", AssignmentEpoch: 8, ControlPresenceSessionId: "presence-8", DaemonRuntimeGeneration: "runtime-8", AccessProjectionRevision: 12},
	}}
	randomBytes := make([]byte, 36)
	for index := range randomBytes {
		randomBytes[index] = byte(index + 21)
	}
	planner, _ := commandoutbox.NewPlanner(outbox, source, nil, bytes.NewReader(randomBytes), nil)
	now := time.Date(2026, 7, 20, 14, 0, 0, 0, time.UTC)
	requestTarget := &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_TerminalAccess{TerminalAccess: &cloudpb.RevokeTerminalAccessTarget{DaemonDeviceId: "daemon-1", OpaqueAccessReference: "opaque-1"}}}
	created, inserted, err := planner.Create(context.Background(), &cloudpb.CreateManagementCommandRequest{AccountId: "account-1", CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_REVOKE_TERMINAL_ACCESS, Target: requestTarget, IdempotencyKey: "revoke-access"}, &cloudpb.ManagementActorProjection{ActorKind: cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_ACCOUNT_OWNER, ActorId: "user-1"}, now)
	if err != nil || !inserted || len(created.GetChildren()) != 1 {
		t.Fatalf("terminal revoke create = (%v, %v, %v)", created, inserted, err)
	}
	child := created.GetChildren()[0]
	target := child.GetTarget().GetTerminalAccess()
	if child.GetTargetHubId() != "hub-a" || target.GetAssignmentEpoch() != 8 || target.GetPresenceSessionId() != "presence-8" || target.GetDaemonRuntimeGeneration() != "runtime-8" || target.GetAccessProjectionRevision() != 12 {
		t.Fatalf("terminal revoke = (%v, %v, %v)", created, inserted, err)
	}
	if created.GetTarget().GetTerminalAccess().GetAssignmentEpoch() != 0 {
		t.Fatalf("user target was rewritten instead of preserving idempotency input: %v", created.GetTarget())
	}
}

func TestPlannerRelayCloseUsesPersistentReservationBinding(t *testing.T) {
	store, err := cloudsqlite.Open(filepath.Join(t.TempDir(), "controller.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 20, 15, 0, 0, 0, time.UTC)
	request := relayquota.ReserveRequest{LeaseID: "lease-1", AccountID: "account-1", ManagedSessionID: "managed-1", ClientDeviceID: "client-1", TargetDeviceID: "daemon-1", Region: "local-1", HubID: "hub-1", RelayID: "relay-1", PeriodStart: now.Add(-time.Hour), PeriodEnd: now.Add(time.Hour), PeriodLimitBytes: 1024, MaxBytesPerLease: 512, MaxConcurrency: 2, ExpiresAt: now.Add(10 * time.Minute), ReleaseAfter: now.Add(12 * time.Minute)}
	if _, _, _, err := store.Reserve(context.Background(), request, now); err != nil {
		t.Fatal(err)
	}
	outbox, _ := commandoutbox.New(store)
	planner, _ := commandoutbox.NewPlanner(outbox, &plannerSource{}, store, bytes.NewReader(make([]byte, 72)), nil)
	target := &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_RelayAllocations{RelayAllocations: &cloudpb.RelayControlTarget{RelayId: "relay-1", LeaseId: "lease-1"}}}
	created, inserted, err := planner.Create(context.Background(), &cloudpb.CreateManagementCommandRequest{AccountId: "account-1", CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_CLOSE_RELAY_ALLOCATIONS, Target: target, IdempotencyKey: "close-relay"}, &cloudpb.ManagementActorProjection{ActorKind: cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_ACCOUNT_OWNER, ActorId: "user-1"}, now)
	if err != nil || !inserted || len(created.GetChildren()) != 1 || created.GetChildren()[0].GetTargetHubId() != "hub-1" {
		t.Fatalf("Relay close create = (%v, %v, %v)", created, inserted, err)
	}
	wrong := proto.Clone(target).(*cloudpb.ManagementCommandTarget)
	wrong.GetRelayAllocations().RelayId = "relay-2"
	if _, _, err := planner.Create(context.Background(), &cloudpb.CreateManagementCommandRequest{AccountId: "account-1", CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_CLOSE_RELAY_ALLOCATIONS, Target: wrong, IdempotencyKey: "wrong-relay"}, &cloudpb.ManagementActorProjection{ActorKind: cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_ACCOUNT_OWNER, ActorId: "user-1"}, now); !errors.Is(err, commandoutbox.ErrCommandNotFound) {
		t.Fatalf("wrong Relay binding = %v", err)
	}
}

func TestPlannerCanonicalizesAssignmentMigrationAndReplaysUserTarget(t *testing.T) {
	store, err := cloudsqlite.Open(filepath.Join(t.TempDir(), "controller.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	outbox, _ := commandoutbox.New(store)
	source := &migrationSource{
		assignment: hubregistry.Assignment{Value: &cloudpb.HubAssignment{DaemonDeviceId: "daemon-1", AccountId: "account-1", HubId: "hub-a", AssignmentEpoch: 4, NotBeforeUnixMillis: 1, ExpiresAtUnixMillis: time.Date(2026, 7, 21, 18, 0, 0, 0, time.UTC).UnixMilli()}},
		deployments: map[string]hubregistry.Deployment{
			"hub-a": {Enabled: true, ControlGeneration: 7},
			"hub-b": {Enabled: true, ControlGeneration: 3},
		},
	}
	planner, _ := commandoutbox.NewPlanner(outbox, &plannerSource{}, nil, bytes.NewReader(make([]byte, 72)), nil, source)
	now := time.Date(2026, 7, 21, 17, 0, 0, 0, time.UTC)
	requestTarget := &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_AssignmentMigration{AssignmentMigration: &cloudpb.AssignmentMigrationTarget{DaemonDeviceId: "daemon-1", TargetHubId: "hub-b"}}}
	request := &cloudpb.CreateManagementCommandRequest{AccountId: "account-1", CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_MIGRATE_ASSIGNMENT, Target: requestTarget, IdempotencyKey: "move-daemon-1"}
	actor := &cloudpb.ManagementActorProjection{ActorKind: cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_OPERATOR_ADMIN, ActorId: "operator-1"}
	created, inserted, err := planner.Create(context.Background(), request, actor, now)
	if err != nil || !inserted || len(created.GetChildren()) != 1 {
		t.Fatalf("migration create = (%v, %v, %v)", created, inserted, err)
	}
	target := created.GetTarget().GetAssignmentMigration()
	if target.GetMigrationId() == "" || target.GetSourceHubId() != "hub-a" || target.GetSourceAssignmentEpoch() != 4 || target.GetSourceControlGeneration() != 7 || target.GetTargetHubId() != "hub-b" || target.GetTargetAssignmentEpoch() != 5 {
		t.Fatalf("canonical migration target = %v", target)
	}
	if child := created.GetChildren()[0]; child.GetTargetHubId() != "hub-a" || !proto.Equal(child.GetTarget(), created.GetTarget()) {
		t.Fatalf("migration child = %v", child)
	}
	replayed, inserted, err := planner.Create(context.Background(), request, actor, now.Add(time.Second))
	if err != nil || inserted || replayed.GetCommandId() != created.GetCommandId() {
		t.Fatalf("migration replay = (%v, %v, %v)", replayed, inserted, err)
	}
}

type plannerSource struct {
	device   cloudtopology.DeviceOwnership
	presence *cloudpb.PresenceProjection
	session  cloudtopology.StoredPeerSession
	sessions []cloudtopology.StoredPeerSession
	terminal cloudtopology.StoredTerminalAccess
}

type migrationSource struct {
	assignment  hubregistry.Assignment
	deployments map[string]hubregistry.Deployment
}

func (source *migrationSource) Assignment(context.Context, string) (hubregistry.Assignment, error) {
	return source.assignment, nil
}

func (source *migrationSource) Deployment(_ context.Context, hubID string) (hubregistry.Deployment, error) {
	deployment, ok := source.deployments[hubID]
	if !ok {
		return hubregistry.Deployment{}, hubregistry.ErrDeploymentNotFound
	}
	return deployment, nil
}

func (source *plannerSource) Device(context.Context, string) (cloudtopology.DeviceOwnership, error) {
	return source.device, nil
}
func (source *plannerSource) Presence(context.Context, string) (string, *cloudpb.PresenceProjection, error) {
	if source.presence == nil {
		return source.device.AccountID, nil, cloudtopology.ErrTopologyRejected
	}
	return source.device.AccountID, source.presence, nil
}
func (source *plannerSource) PeerSession(context.Context, *cloudpb.ManagedPeerSessionTarget) (cloudtopology.StoredPeerSession, error) {
	return source.session, nil
}
func (source *plannerSource) PeerSessionsForClient(context.Context, string) ([]cloudtopology.StoredPeerSession, error) {
	return source.sessions, nil
}
func (source *plannerSource) TerminalAccess(context.Context, string, string) (cloudtopology.StoredTerminalAccess, error) {
	if source.terminal.Value == nil {
		return cloudtopology.StoredTerminalAccess{}, cloudtopology.ErrTopologyRejected
	}
	return source.terminal, nil
}

func plannerSession(daemonID, managedID string, incarnation uint64, presenceID, runtimeID, clientID string) *cloudpb.ManagedPeerSessionProjection {
	return &cloudpb.ManagedPeerSessionProjection{Target: &cloudpb.ManagedPeerSessionTarget{DaemonDeviceId: daemonID, ManagedSessionId: managedID, SessionIncarnation: incarnation, AssignmentEpoch: 7, ControlPresenceSessionId: presenceID, DaemonRuntimeGeneration: runtimeID}, ClientDeviceId: clientID, ControlOwnerHubId: "ignored", ObservedDataPath: cloudpb.ObservedPath_OBSERVED_PATH_DIRECT, State: cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_READY, Freshness: cloudpb.Freshness_FRESHNESS_FRESH}
}
