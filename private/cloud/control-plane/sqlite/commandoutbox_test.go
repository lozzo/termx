package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/commandoutbox"
	cloudsqlite "github.com/lozzow/termx/private/cloud/control-plane/sqlite"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestCommandOutboxPersistsIdempotencyAndExactResultReplay(t *testing.T) {
	now := time.Date(2026, 7, 21, 11, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "controller.db")
	store, err := cloudsqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service, _ := commandoutbox.New(store)
	command := sqliteCloseSessionCommand(now)
	created, inserted, err := service.Create(context.Background(), command, "idem-1", now)
	if err != nil || !inserted {
		t.Fatalf("create command = (%v, %v, %v)", created, inserted, err)
	}
	replayedCreate := sqliteCloseSessionCommand(now)
	replayedCreate.CommandId = "different-command"
	stored, inserted, err := service.Create(context.Background(), replayedCreate, "idem-1", now)
	if err != nil || inserted || stored.GetCommandId() != command.GetCommandId() {
		t.Fatalf("idempotent create = (%v, %v, %v)", stored, inserted, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = cloudsqlite.Open(path)
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

func sqliteCloseSessionCommand(now time.Time) *cloudpb.ManagementCommandProjection {
	target := &cloudpb.ManagedPeerSessionTarget{DaemonDeviceId: "daemon-1", ManagedSessionId: "managed-1", SessionIncarnation: 2, AssignmentEpoch: 7, ControlPresenceSessionId: "presence-1", DaemonRuntimeGeneration: "runtime-1"}
	commandTarget := &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_PeerSession{PeerSession: target}}
	return &cloudpb.ManagementCommandProjection{CommandId: "parent-1", AccountId: "account-1", Actor: &cloudpb.ManagementActorProjection{ActorKind: cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_ACCOUNT_OWNER, ActorId: "user-1"}, CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_CLOSE_MANAGED_PEER_SESSION, Target: commandTarget, AuthorityResult: cloudpb.CommandAuthorityResult_COMMAND_AUTHORITY_RESULT_NOT_APPLICABLE, DeliveryState: cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING, ExecutionState: cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING, ObservedEffect: cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN, Children: []*cloudpb.ManagementCommandChildProjection{{ChildCommandId: "child-1", TargetHubId: "hub-1", Target: commandTarget, DeliveryState: cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING, ExecutionState: cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING, ObservedEffect: cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN, UpdatedAtUnixMillis: now.UnixMilli()}}, CreatedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(5 * time.Minute).UnixMilli(), UpdatedAtUnixMillis: now.UnixMilli()}
}
