package controller

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/commandoutbox"
	postgrestest "github.com/muxvia/muxvia/private/cloud/control-plane/postgrestest"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestMigrationResultReplayRetriesProjectionRefresh(t *testing.T) {
	now := time.Date(2026, 7, 21, 19, 0, 0, 0, time.UTC)
	store, err := postgrestest.Open(t, filepath.Join(t.TempDir(), "controller-postgres"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.MoveAssignment(context.Background(), &cloudpb.HubAssignment{DaemonDeviceId: "daemon-1", AccountId: "account-1", HubId: "hub-a", AssignmentEpoch: 1, NotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Hour).UnixMilli()}, now); err != nil {
		t.Fatal(err)
	}
	outbox, _ := commandoutbox.New(store)
	migration := &cloudpb.AssignmentMigrationTarget{MigrationId: "migration-1", DaemonDeviceId: "daemon-1", SourceHubId: "hub-a", SourceAssignmentEpoch: 1, SourceControlGeneration: 3, TargetHubId: "hub-b", TargetAssignmentEpoch: 2, TargetNotBeforeUnixMillis: now.UnixMilli(), TargetExpiresAtUnixMillis: now.Add(2 * time.Hour).UnixMilli()}
	target := &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_AssignmentMigration{AssignmentMigration: migration}}
	projection := &cloudpb.ManagementCommandProjection{CommandId: "parent-1", AccountId: "account-1", Actor: &cloudpb.ManagementActorProjection{ActorKind: cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_OPERATOR_ADMIN, ActorId: "operator-1"}, CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_MIGRATE_ASSIGNMENT, Target: target, AuthorityResult: cloudpb.CommandAuthorityResult_COMMAND_AUTHORITY_RESULT_NOT_APPLICABLE, DeliveryState: cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING, ExecutionState: cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING, ObservedEffect: cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN, Children: []*cloudpb.ManagementCommandChildProjection{{ChildCommandId: "child-1", TargetHubId: "hub-a", Target: target, DeliveryState: cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING, ExecutionState: cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING, ObservedEffect: cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN, UpdatedAtUnixMillis: now.UnixMilli()}}, CreatedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(5 * time.Minute).UnixMilli(), UpdatedAtUnixMillis: now.UnixMilli()}
	if _, _, err := outbox.Create(context.Background(), projection, "idem-1", now); err != nil {
		t.Fatal(err)
	}
	refreshes := 0
	sink := &migrationResultSink{outbox: outbox, refresh: func(*cloudpb.ManagementCommandProjection, time.Time) error {
		refreshes++
		if refreshes == 1 {
			return errors.New("projection publisher unavailable")
		}
		return nil
	}}
	result := &cloudpb.HubCommandResult{CommandId: "child-1", HubId: "hub-a", ControlGeneration: 3, ExecutionControlGeneration: 3, ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED, CompletedAtUnixMillis: now.Add(time.Second).UnixMilli()}
	if err := sink.IngestHubResult(context.Background(), result, now.Add(time.Second)); err == nil {
		t.Fatal("first projection refresh unexpectedly succeeded")
	}
	reconnected := proto.Clone(result).(*cloudpb.HubCommandResult)
	reconnected.ControlGeneration = 4
	reconnected.CompletedAtUnixMillis = now.Add(2 * time.Second).UnixMilli()
	if err := sink.IngestHubResult(context.Background(), reconnected, now.Add(2*time.Second)); err != nil || refreshes != 2 {
		t.Fatalf("replayed migration refresh = (%d, %v)", refreshes, err)
	}
	assignment, err := store.Assignment(context.Background(), "daemon-1")
	if err != nil || assignment.Value.GetHubId() != "hub-b" || assignment.Value.GetAssignmentEpoch() != 2 {
		t.Fatalf("migration assignment = (%+v, %v)", assignment, err)
	}
}
