package commandoutbox_test

import (
	"errors"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/commandoutbox"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestCommandOutboxSeparatesHubDeliveryAndDaemonExecution(t *testing.T) {
	now := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)
	command := closeSessionCommand(now, "child-1")
	validated, err := commandoutbox.ValidateCreate(command, "idem-1", now)
	if err != nil {
		t.Fatal(err)
	}
	hubReceived, err := commandoutbox.ApplyHubResult(validated, &cloudpb.HubCommandResult{CommandId: "child-1", HubId: "hub-1", ControlGeneration: 4, ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED, CompletedAtUnixMillis: now.Add(time.Second).UnixMilli()}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if hubReceived.GetChildren()[0].GetDeliveryState() != cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_RUNTIME_RECEIVED || hubReceived.GetExecutionState() != cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING {
		t.Fatalf("Hub receipt changed execution truth: %v", hubReceived)
	}
	result := &cloudpb.DaemonCommandResult{CommandId: "child-1", DaemonDeviceId: "daemon-1", ManagedSessionId: "managed-1", SessionIncarnation: 2, AssignmentEpoch: 7, PresenceSessionId: "presence-1", DaemonRuntimeGeneration: "runtime-1", ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED, ClosedRegistryRevision: 9, CompletedAtUnixMillis: now.Add(2 * time.Second).UnixMilli()}
	applied, err := commandoutbox.ApplyDaemonResult(hubReceived, result, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if applied.GetExecutionState() != cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_APPLIED || applied.GetAuthorityResult() != cloudpb.CommandAuthorityResult_COMMAND_AUTHORITY_RESULT_NOT_APPLICABLE {
		t.Fatalf("daemon execution state = %v", applied)
	}
	result.SessionIncarnation = 3
	if _, err := commandoutbox.ApplyDaemonResult(hubReceived, result, now.Add(2*time.Second)); !errors.Is(err, commandoutbox.ErrCommandConflict) {
		t.Fatalf("stale daemon result error = %v", err)
	}
}

func TestCommandOutboxExpiryKeepsCommittedAuthorityAndAggregatesPartial(t *testing.T) {
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	command := closeSessionCommand(now, "child-1")
	command.AuthorityResult = cloudpb.CommandAuthorityResult_COMMAND_AUTHORITY_RESULT_COMMITTED
	second := proto.Clone(command.Children[0]).(*cloudpb.ManagementCommandChildProjection)
	second.ChildCommandId = "child-2"
	second.TargetHubId = "hub-2"
	command.Children = append(command.Children, second)
	validated, _ := commandoutbox.ValidateCreate(command, "idem-2", now)
	applied, err := commandoutbox.ApplyDaemonResult(validated, &cloudpb.DaemonCommandResult{CommandId: "child-1", DaemonDeviceId: "daemon-1", ManagedSessionId: "managed-1", SessionIncarnation: 2, AssignmentEpoch: 7, PresenceSessionId: "presence-1", DaemonRuntimeGeneration: "runtime-1", ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED, CompletedAtUnixMillis: now.Add(time.Second).UnixMilli()}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	expired, err := commandoutbox.Expire(applied, now.Add(6*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if expired.GetAuthorityResult() != cloudpb.CommandAuthorityResult_COMMAND_AUTHORITY_RESULT_COMMITTED || expired.GetExecutionState() != cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PARTIAL || expired.GetChildren()[1].GetDeliveryState() != cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_EXPIRED {
		t.Fatalf("expired parent = %v", expired)
	}
}

func TestRelayResultRequiresSettlementAndPreservesPartial(t *testing.T) {
	now := time.Date(2026, 7, 21, 11, 0, 0, 0, time.UTC)
	target := &cloudpb.RelayControlTarget{RelayId: "relay-1", LeaseId: "lease-1"}
	commandTarget := &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_RelayAllocations{RelayAllocations: target}}
	projection := &cloudpb.ManagementCommandProjection{CommandId: "parent-relay", AccountId: "account-1", Actor: &cloudpb.ManagementActorProjection{ActorKind: cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_ACCOUNT_OWNER, ActorId: "user-1"}, CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_CLOSE_RELAY_ALLOCATIONS, Target: commandTarget, AuthorityResult: cloudpb.CommandAuthorityResult_COMMAND_AUTHORITY_RESULT_NOT_APPLICABLE, DeliveryState: cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING, ExecutionState: cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING, ObservedEffect: cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN, Children: []*cloudpb.ManagementCommandChildProjection{{ChildCommandId: "child-relay", TargetHubId: "hub-1", Target: commandTarget, DeliveryState: cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING, ExecutionState: cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING, ObservedEffect: cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN, UpdatedAtUnixMillis: now.UnixMilli()}}, CreatedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(5 * time.Minute).UnixMilli(), UpdatedAtUnixMillis: now.UnixMilli()}
	validated, err := commandoutbox.ValidateCreate(projection, "idem-relay", now)
	if err != nil {
		t.Fatal(err)
	}
	partial, err := commandoutbox.ApplyRelayResult(validated, &cloudpb.RelayCommandResult{CommandId: "child-relay", RelayId: "relay-1", RelayControlGeneration: 3, ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_PARTIAL, Allocations: []*cloudpb.RelayAllocationCloseResult{{AllocationId: "allocation-1", ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED}}, LeaseId: "lease-1", UsageDrainComplete: true, ErrorCode: "usage_settlement_incomplete", CompletedAtUnixMillis: now.Add(time.Second).UnixMilli()}, now.Add(time.Second))
	if err != nil || partial.GetExecutionState() != cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PARTIAL {
		t.Fatalf("partial Relay result = (%v, %v)", partial, err)
	}
	if _, err := commandoutbox.ApplyRelayResult(validated, &cloudpb.RelayCommandResult{CommandId: "child-relay", RelayId: "relay-1", RelayControlGeneration: 3, ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED, LeaseId: "lease-1", UsageDrainComplete: true, CompletedAtUnixMillis: now.Add(time.Second).UnixMilli()}, now.Add(time.Second)); !errors.Is(err, commandoutbox.ErrCommandConflict) {
		t.Fatalf("unsettled APPLIED Relay result = %v", err)
	}
	applied, err := commandoutbox.ApplyRelayResult(validated, &cloudpb.RelayCommandResult{CommandId: "child-relay", RelayId: "relay-1", RelayControlGeneration: 3, ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED, Allocations: []*cloudpb.RelayAllocationCloseResult{{AllocationId: "allocation-1", ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED}}, LeaseId: "lease-1", FinalUsageSequence: 2, UsageDrainComplete: true, UsageSettlementComplete: true, SettledUsage: []*cloudpb.RelayUsageAck{{EventId: "usage-2", Sequence: 2}}, CompletedAtUnixMillis: now.Add(2 * time.Second).UnixMilli()}, now.Add(2*time.Second))
	if err != nil || applied.GetExecutionState() != cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_APPLIED {
		t.Fatalf("applied Relay result = (%v, %v)", applied, err)
	}
}

func closeSessionCommand(now time.Time, childID string) *cloudpb.ManagementCommandProjection {
	target := &cloudpb.ManagedPeerSessionTarget{DaemonDeviceId: "daemon-1", ManagedSessionId: "managed-1", SessionIncarnation: 2, AssignmentEpoch: 7, ControlPresenceSessionId: "presence-1", DaemonRuntimeGeneration: "runtime-1"}
	commandTarget := &cloudpb.ManagementCommandTarget{Target: &cloudpb.ManagementCommandTarget_PeerSession{PeerSession: target}}
	return &cloudpb.ManagementCommandProjection{CommandId: "parent-1", AccountId: "account-1", Actor: &cloudpb.ManagementActorProjection{ActorKind: cloudpb.ManagementActorKind_MANAGEMENT_ACTOR_KIND_ACCOUNT_OWNER, ActorId: "user-1"}, CommandKind: cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_CLOSE_MANAGED_PEER_SESSION, Target: commandTarget, AuthorityResult: cloudpb.CommandAuthorityResult_COMMAND_AUTHORITY_RESULT_NOT_APPLICABLE, DeliveryState: cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING, ExecutionState: cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING, ObservedEffect: cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN, Children: []*cloudpb.ManagementCommandChildProjection{{ChildCommandId: childID, TargetHubId: "hub-1", Target: commandTarget, DeliveryState: cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING, ExecutionState: cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING, ObservedEffect: cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN, UpdatedAtUnixMillis: now.UnixMilli()}}, CreatedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(5 * time.Minute).UnixMilli(), UpdatedAtUnixMillis: now.UnixMilli()}
}
