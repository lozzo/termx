package postgres_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/commandoutbox"
	"github.com/muxvia/muxvia/private/cloud/control-plane/hubregistry"
	cloudpostgres "github.com/muxvia/muxvia/private/cloud/control-plane/postgres"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestCommandOutboxPersistsIdempotencyAndExactResultReplay(t *testing.T) {
	now := time.Date(2026, 7, 21, 11, 0, 0, 0, time.UTC)
	dsn := testPostgresDSN(t)
	store, err := cloudpostgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	service, _ := commandoutbox.New(store)
	command := postgresCloseSessionCommand(now)
	created, inserted, err := service.Create(context.Background(), command, "idem-1", now)
	if err != nil || !inserted {
		t.Fatalf("create command = (%v, %v, %v)", created, inserted, err)
	}
	replayedCreate := postgresCloseSessionCommand(now)
	replayedCreate.CommandId = "different-command"
	stored, inserted, err := service.Create(context.Background(), replayedCreate, "idem-1", now)
	if err != nil || inserted || stored.GetCommandId() != command.GetCommandId() {
		t.Fatalf("idempotent create = (%v, %v, %v)", stored, inserted, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = cloudpostgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, _ = commandoutbox.New(store)
	loaded, err := service.Get(context.Background(), "account-1", "parent-1")
	if err != nil || loaded.GetChildren()[0].GetChildCommandId() != "child-1" {
		t.Fatalf("restarted command = (%v, %v)", loaded, err)
	}
	hubResult := &cloudpb.HubCommandResult{CommandId: "child-1", HubId: "hub-1", ControlGeneration: 3, ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED, CompletedAtUnixMillis: now.Add(time.Second).UnixMilli()}
	forwarded, replay, err := service.ApplyHubResult(context.Background(), hubResult, now.Add(time.Second))
	if err != nil || replay || forwarded.GetExecutionState() != cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING {
		t.Fatalf("Hub result = (%v, %v, %v)", forwarded, replay, err)
	}
	reconnected := proto.Clone(hubResult).(*cloudpb.HubCommandResult)
	reconnected.ControlGeneration = 4
	reconnected.CompletedAtUnixMillis = now.Add(2 * time.Second).UnixMilli()
	exact, replay, err := service.ApplyHubResult(context.Background(), reconnected, now.Add(2*time.Second))
	if err != nil || !replay || exact.GetUpdatedAtUnixMillis() != forwarded.GetUpdatedAtUnixMillis() {
		t.Fatalf("exact Hub replay = (%v, %v, %v)", exact, replay, err)
	}
	conflict := proto.Clone(hubResult).(*cloudpb.HubCommandResult)
	conflict.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_REJECTED
	if _, _, err := service.ApplyHubResult(context.Background(), conflict, now.Add(2*time.Second)); !errors.Is(err, commandoutbox.ErrCommandConflict) {
		t.Fatalf("conflicting Hub replay error = %v", err)
	}
	daemonResult := &cloudpb.DaemonCommandResult{CommandId: "child-1", DaemonDeviceId: "daemon-1", ManagedSessionId: "managed-1", SessionIncarnation: 2, AssignmentEpoch: 7, PresenceSessionId: "presence-1", DaemonRuntimeGeneration: "runtime-1", ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED, ClosedRegistryRevision: 10, CompletedAtUnixMillis: now.Add(3 * time.Second).UnixMilli()}
	applied, replay, err := service.ApplyDaemonResult(context.Background(), daemonResult, now.Add(3*time.Second))
	if err != nil || replay || applied.GetExecutionState() != cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_APPLIED {
		t.Fatalf("daemon result = (%v, %v, %v)", applied, replay, err)
	}
}

func TestAssignmentMigrationResultMovesAssignmentAtomically(t *testing.T) {
	now := time.Date(2026, 7, 21, 18, 0, 0, 0, time.UTC)
	store, err := cloudpostgres.Open(context.Background(), testPostgresDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, hubID := range []string{"hub-a", "hub-b"} {
		hubKey, _, _ := ed25519.GenerateKey(rand.Reader)
		relayKey, _, _ := ed25519.GenerateKey(rand.Reader)
		metadata := &cloudpb.EdgeDeploymentMetadata{HubId: hubID, EdgeDeploymentId: "edge-" + hubID, Region: "local", HubControlIdentityFingerprint: hubregistry.IdentityFingerprint(hubKey), RelayId: "relay-" + hubID, RelayControlIdentityFingerprint: hubregistry.IdentityFingerprint(relayKey)}
		if err := store.PutDeployment(context.Background(), hubregistry.Deployment{Metadata: metadata, ControlPublicKey: hubKey, RelayControlPublicKey: relayKey, MaxAssignments: 100, IdentityApproved: true, Enabled: true, DirectoryRevision: 1, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.MoveAssignment(context.Background(), &cloudpb.HubAssignment{DaemonDeviceId: "daemon-1", AccountId: "account-1", HubId: "hub-a", AssignmentEpoch: 1, NotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Hour).UnixMilli()}, now); err != nil {
		t.Fatal(err)
	}
	service, _ := commandoutbox.New(store)
	migration := &cloudpb.AssignmentMigrationTarget{MigrationId: "migration-1", DaemonDeviceId: "daemon-1", SourceHubId: "hub-a", SourceAssignmentEpoch: 1, SourceControlGeneration: 4, TargetHubId: "hub-b", TargetAssignmentEpoch: 2, TargetNotBeforeUnixMillis: now.UnixMilli(), TargetExpiresAtUnixMillis: now.Add(2 * time.Hour).UnixMilli()}
	target := &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_AssignmentMigration{AssignmentMigration: migration}}
	projection := &cloudpb.ManagementCommandProjection{CommandId: "parent-migration", AccountId: "account-1", Actor: &cloudpb.ManagementActorProjection{ActorKind: cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_OPERATOR_ADMIN, ActorId: "operator-1"}, CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_MIGRATE_ASSIGNMENT, Target: target, AuthorityResult: cloudpb.CommandAuthorityResult_COMMAND_AUTHORITY_RESULT_NOT_APPLICABLE, DeliveryState: cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING, ExecutionState: cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING, ObservedEffect: cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN, Children: []*cloudpb.ManagementCommandChildProjection{{ChildCommandId: "child-migration", TargetHubId: "hub-a", Target: target, DeliveryState: cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING, ExecutionState: cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING, ObservedEffect: cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN, UpdatedAtUnixMillis: now.UnixMilli()}}, CreatedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(5 * time.Minute).UnixMilli(), UpdatedAtUnixMillis: now.UnixMilli()}
	if _, _, err := service.Create(context.Background(), projection, "migration-idem", now); err != nil {
		t.Fatal(err)
	}
	wrongGeneration := &cloudpb.HubCommandResult{CommandId: "child-migration", HubId: "hub-a", ControlGeneration: 3, ExecutionControlGeneration: 3, ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED, CompletedAtUnixMillis: now.Add(time.Second).UnixMilli()}
	if _, _, err := service.ApplyHubResult(context.Background(), wrongGeneration, now.Add(time.Second)); !errors.Is(err, commandoutbox.ErrCommandConflict) {
		t.Fatalf("wrong generation error = %v", err)
	}
	assignment, _ := store.Assignment(context.Background(), "daemon-1")
	command, _ := service.Get(context.Background(), "account-1", "parent-migration")
	if assignment.Value.GetHubId() != "hub-a" || assignment.Value.GetAssignmentEpoch() != 1 || command.GetExecutionState() != cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING {
		t.Fatalf("failed transaction leaked state: assignment=%v command=%v", assignment, command)
	}
	result := proto.Clone(wrongGeneration).(*cloudpb.HubCommandResult)
	result.ControlGeneration = 5
	result.ExecutionControlGeneration = 4
	completed, replay, err := service.ApplyHubResult(context.Background(), result, now.Add(2*time.Second))
	if err != nil || replay || completed.GetExecutionState() != cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_APPLIED {
		t.Fatalf("migration result = (%v, %v, %v)", completed, replay, err)
	}
	assignment, _ = store.Assignment(context.Background(), "daemon-1")
	if assignment.Value.GetHubId() != "hub-b" || assignment.Value.GetAssignmentEpoch() != 2 || assignment.PreviousHubID != "hub-a" || assignment.PreviousEpoch != 1 {
		t.Fatalf("moved assignment = %+v", assignment)
	}
}

func postgresCloseSessionCommand(now time.Time) *cloudpb.ManagementCommandProjection {
	target := &cloudpb.ManagedPeerSessionTarget{DaemonDeviceId: "daemon-1", ManagedSessionId: "managed-1", SessionIncarnation: 2, AssignmentEpoch: 7, ControlPresenceSessionId: "presence-1", DaemonRuntimeGeneration: "runtime-1"}
	commandTarget := &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_PeerSession{PeerSession: target}}
	return &cloudpb.ManagementCommandProjection{CommandId: "parent-1", AccountId: "account-1", Actor: &cloudpb.ManagementActorProjection{ActorKind: cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_ACCOUNT_OWNER, ActorId: "user-1"}, CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_CLOSE_MANAGED_PEER_SESSION, Target: commandTarget, AuthorityResult: cloudpb.CommandAuthorityResult_COMMAND_AUTHORITY_RESULT_NOT_APPLICABLE, DeliveryState: cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING, ExecutionState: cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING, ObservedEffect: cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN, Children: []*cloudpb.ManagementCommandChildProjection{{ChildCommandId: "child-1", TargetHubId: "hub-1", Target: commandTarget, DeliveryState: cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING, ExecutionState: cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING, ObservedEffect: cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN, UpdatedAtUnixMillis: now.UnixMilli()}}, CreatedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(5 * time.Minute).UnixMilli(), UpdatedAtUnixMillis: now.UnixMilli()}
}
