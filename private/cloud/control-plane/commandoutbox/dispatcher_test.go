package commandoutbox_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/commandoutbox"
	postgrestest "github.com/muxvia/muxvia/private/cloud/control-plane/postgrestest"
	cloudtopology "github.com/muxvia/muxvia/private/cloud/control-plane/topology"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestDispatcherRetriesIdenticalSignedDaemonCommandUntilExecutionReceipt(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	store, err := postgrestest.Open(t, filepath.Join(t.TempDir(), "controller-postgres"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	outbox, _ := commandoutbox.New(store)
	now := time.Date(2026, 7, 20, 14, 0, 0, 0, time.UTC)
	projection := closeSessionCommand(now, "child-1")
	if _, _, err := outbox.Create(context.Background(), projection, "idem-dispatch", now); err != nil {
		t.Fatal(err)
	}
	publisher := &recordingCommandPublisher{}
	source := &plannerSource{device: cloudtopology.DeviceOwnership{DeviceID: "daemon-1", AccountID: "account-1", Kind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: 9}}
	dispatcher, err := commandoutbox.NewDispatcher(outbox, publisher, nil, source, "control-1", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.DispatchOnce(context.Background(), now.Add(time.Second), 32); err != nil {
		t.Fatal(err)
	}
	if _, _, err := outbox.ApplyHubResult(context.Background(), &cloudpb.HubCommandResult{CommandId: "child-1", HubId: "hub-1", ControlGeneration: 2, ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED, CompletedAtUnixMillis: now.Add(2 * time.Second).UnixMilli()}, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.DispatchOnce(context.Background(), now.Add(3*time.Second), 32); err != nil {
		t.Fatal(err)
	}
	if len(publisher.commands) != 2 || !proto.Equal(publisher.commands[0], publisher.commands[1]) {
		t.Fatalf("dispatcher retries = %v", publisher.commands)
	}
	verifier, _ := cloudpb.NewDaemonControlVerifier(map[string]ed25519.PublicKey{"control-1": publicKey})
	if err := verifier.Verify(publisher.commands[0].GetDaemonCommand(), now.Add(time.Second)); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestDispatcherSignsTerminalRevokeAndPersistsDaemonResult(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	store, err := postgrestest.Open(t, filepath.Join(t.TempDir(), "controller-postgres"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	outbox, _ := commandoutbox.New(store)
	now := time.Date(2026, 7, 20, 14, 30, 0, 0, time.UTC)
	target := &cloudpb.RevokeTerminalAccessTarget{DaemonDeviceId: "daemon-1", OpaqueAccessReference: "opaque-1", AssignmentEpoch: 7, PresenceSessionId: "presence-1", DaemonRuntimeGeneration: "runtime-1", AccessProjectionRevision: 11}
	commandTarget := &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_TerminalAccess{TerminalAccess: target}}
	projection := &cloudpb.ManagementCommandProjection{CommandId: "parent-access", AccountId: "account-1", Actor: &cloudpb.ManagementActorProjection{ActorKind: cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_ACCOUNT_OWNER, ActorId: "user-1"}, CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_REVOKE_TERMINAL_ACCESS, Target: &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_TerminalAccess{TerminalAccess: &cloudpb.RevokeTerminalAccessTarget{DaemonDeviceId: "daemon-1", OpaqueAccessReference: "opaque-1"}}}, AuthorityResult: cloudpb.CommandAuthorityResult_COMMAND_AUTHORITY_RESULT_NOT_APPLICABLE, DeliveryState: cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING, ExecutionState: cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING, ObservedEffect: cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN, Children: []*cloudpb.ManagementCommandChildProjection{{ChildCommandId: "child-access", TargetHubId: "hub-1", Target: commandTarget, DeliveryState: cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING, ExecutionState: cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING, ObservedEffect: cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN, UpdatedAtUnixMillis: now.UnixMilli()}}, CreatedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(5 * time.Minute).UnixMilli(), UpdatedAtUnixMillis: now.UnixMilli()}
	if _, _, err := outbox.Create(context.Background(), projection, "idem-access", now); err != nil {
		t.Fatal(err)
	}
	publisher := &recordingCommandPublisher{}
	source := &plannerSource{device: cloudtopology.DeviceOwnership{DeviceID: "daemon-1", AccountID: "account-1", Kind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: 9}}
	dispatcher, _ := commandoutbox.NewDispatcher(outbox, publisher, nil, source, "control-1", privateKey)
	if err := dispatcher.DispatchOnce(context.Background(), now.Add(time.Second), 32); err != nil {
		t.Fatal(err)
	}
	if len(publisher.commands) != 1 {
		t.Fatalf("published commands = %v", publisher.commands)
	}
	signed := publisher.commands[0].GetDaemonCommand()
	verifier, _ := cloudpb.NewDaemonControlVerifier(map[string]ed25519.PublicKey{"control-1": publicKey})
	if err := verifier.Verify(signed, now.Add(time.Second)); err != nil || !proto.Equal(signed.GetTerminalAccess(), target) {
		t.Fatalf("signed terminal command = (%v, %v)", signed, err)
	}
	if _, _, err := outbox.ApplyHubResult(context.Background(), &cloudpb.HubCommandResult{CommandId: "child-access", HubId: "hub-1", ControlGeneration: 2, ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED, CompletedAtUnixMillis: now.Add(2 * time.Second).UnixMilli()}, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	completed, _, err := outbox.ApplyDaemonResult(context.Background(), &cloudpb.DaemonCommandResult{CommandId: "child-access", DaemonDeviceId: "daemon-1", OpaqueAccessReference: "opaque-1", AssignmentEpoch: 7, PresenceSessionId: "presence-1", DaemonRuntimeGeneration: "runtime-1", AccessProjectionRevision: 12, ClosedSessionCount: 2, ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED, CompletedAtUnixMillis: now.Add(3 * time.Second).UnixMilli()}, now.Add(3*time.Second))
	if err != nil || completed.GetExecutionState() != cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_APPLIED {
		t.Fatalf("terminal daemon result = (%v, %v)", completed, err)
	}
}

func TestDispatcherUsesIndependentRelayPublisher(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	store, err := postgrestest.Open(t, filepath.Join(t.TempDir(), "controller-postgres"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	outbox, _ := commandoutbox.New(store)
	now := time.Date(2026, 7, 20, 15, 0, 0, 0, time.UTC)
	target := &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_RelayAllocations{RelayAllocations: &cloudpb.RelayControlTarget{RelayId: "relay-1", LeaseId: "lease-1"}}}
	projection := &cloudpb.ManagementCommandProjection{CommandId: "parent-relay", AccountId: "account-1", Actor: &cloudpb.ManagementActorProjection{ActorKind: cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_ACCOUNT_OWNER, ActorId: "user-1"}, CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_CLOSE_RELAY_ALLOCATIONS, Target: target, AuthorityResult: cloudpb.CommandAuthorityResult_COMMAND_AUTHORITY_RESULT_NOT_APPLICABLE, DeliveryState: cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING, ExecutionState: cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING, ObservedEffect: cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN, Children: []*cloudpb.ManagementCommandChildProjection{{ChildCommandId: "child-relay", TargetHubId: "hub-1", Target: target, DeliveryState: cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING, ExecutionState: cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING, ObservedEffect: cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN, UpdatedAtUnixMillis: now.UnixMilli()}}, CreatedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(5 * time.Minute).UnixMilli(), UpdatedAtUnixMillis: now.UnixMilli()}
	if _, _, err := outbox.Create(context.Background(), projection, "idem-relay", now); err != nil {
		t.Fatal(err)
	}
	hubPublisher := &recordingCommandPublisher{}
	relayPublisher := &recordingRelayPublisher{}
	dispatcher, _ := commandoutbox.NewDispatcher(outbox, hubPublisher, relayPublisher, &plannerSource{}, "control-1", privateKey)
	if err := dispatcher.DispatchOnce(context.Background(), now.Add(time.Second), 32); err != nil {
		t.Fatal(err)
	}
	if len(hubPublisher.commands) != 0 || len(relayPublisher.commands) != 1 || relayPublisher.relayIDs[0] != "relay-1" || relayPublisher.commands[0].GetRelayControlGeneration() != 0 || relayPublisher.commands[0].GetCommandKind() != cloudpb.RelayControlCommandKind_RELAY_CONTROL_COMMAND_KIND_CLOSE_LEASE_ALLOCATIONS {
		t.Fatalf("Relay dispatch = hub:%v relay:%v", hubPublisher.commands, relayPublisher.commands)
	}
}

func TestDispatcherPublishesExactAssignmentFenceToSourceHub(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	store, err := postgrestest.Open(t, filepath.Join(t.TempDir(), "controller-postgres"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	outbox, _ := commandoutbox.New(store)
	now := time.Date(2026, 7, 21, 17, 30, 0, 0, time.UTC)
	migration := &cloudpb.AssignmentMigrationTarget{MigrationId: "migration-1", DaemonDeviceId: "daemon-1", SourceHubId: "hub-a", SourceAssignmentEpoch: 4, SourceControlGeneration: 7, TargetHubId: "hub-b", TargetAssignmentEpoch: 5, TargetNotBeforeUnixMillis: now.UnixMilli(), TargetExpiresAtUnixMillis: now.Add(time.Hour).UnixMilli()}
	target := &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_AssignmentMigration{AssignmentMigration: migration}}
	projection := &cloudpb.ManagementCommandProjection{CommandId: "parent-migration", AccountId: "account-1", Actor: &cloudpb.ManagementActorProjection{ActorKind: cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_OPERATOR_ADMIN, ActorId: "operator-1"}, CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_MIGRATE_ASSIGNMENT, Target: target, AuthorityResult: cloudpb.CommandAuthorityResult_COMMAND_AUTHORITY_RESULT_NOT_APPLICABLE, DeliveryState: cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING, ExecutionState: cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING, ObservedEffect: cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN, Children: []*cloudpb.ManagementCommandChildProjection{{ChildCommandId: "child-migration", TargetHubId: "hub-a", Target: target, DeliveryState: cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING, ExecutionState: cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING, ObservedEffect: cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN, UpdatedAtUnixMillis: now.UnixMilli()}}, CreatedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(5 * time.Minute).UnixMilli(), UpdatedAtUnixMillis: now.UnixMilli()}
	if _, _, err := outbox.Create(context.Background(), projection, "migration-idem", now); err != nil {
		t.Fatal(err)
	}
	publisher := &recordingCommandPublisher{}
	dispatcher, _ := commandoutbox.NewDispatcher(outbox, publisher, nil, &plannerSource{}, "control-1", privateKey)
	if err := dispatcher.DispatchOnce(context.Background(), now.Add(time.Second), 32); err != nil {
		t.Fatal(err)
	}
	if len(publisher.commands) != 1 || publisher.hubIDs[0] != "hub-a" {
		t.Fatalf("migration publish = %v %v", publisher.hubIDs, publisher.commands)
	}
	fence := publisher.commands[0].GetFenceAssignment()
	if fence.GetMigrationId() != "migration-1" || fence.GetFenceCommandId() != "child-migration" || fence.GetDaemonDeviceId() != "daemon-1" || fence.GetSourceHubId() != "hub-a" || fence.GetSourceAssignmentEpoch() != 4 || fence.GetSourceControlGeneration() != 7 {
		t.Fatalf("assignment fence = %v", fence)
	}
}

type recordingCommandPublisher struct {
	hubIDs   []string
	commands []*cloudpb.HubCommand
}

func (publisher *recordingCommandPublisher) PublishCommand(hubID string, command *cloudpb.HubCommand) error {
	publisher.hubIDs = append(publisher.hubIDs, hubID)
	publisher.commands = append(publisher.commands, proto.Clone(command).(*cloudpb.HubCommand))
	return nil
}

type recordingRelayPublisher struct {
	relayIDs []string
	commands []*cloudpb.RelayControlCommand
}

func (publisher *recordingRelayPublisher) PublishCommand(relayID string, command *cloudpb.RelayControlCommand) error {
	publisher.relayIDs = append(publisher.relayIDs, relayID)
	publisher.commands = append(publisher.commands, proto.Clone(command).(*cloudpb.RelayControlCommand))
	return nil
}
