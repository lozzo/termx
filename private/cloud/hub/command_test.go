package hub_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/hub"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestHubCommandKicksOnlyExactPresenceAndReplaysResult(t *testing.T) {
	fixture := newFixture(t, 8, 8)
	presence, _ := fixture.openEdgePresence(t)
	presenceProjection := fixture.service.TopologySnapshot(1, fixture.clock.Now()).GetPresences()[0]
	now := fixture.clock.Now()
	command := &cloudpb.HubCommand{CommandId: "kick-1", CommandKind: cloudpb.HubCommandKind_HUB_COMMAND_KIND_KICK_PRESENCE, IssuedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Minute).UnixMilli(), Target: &cloudpb.HubCommand_KickPresence{KickPresence: &cloudpb.KickPresenceTarget{DaemonDeviceId: "daemon-1", AssignmentEpoch: 1, PresenceSessionId: presenceProjection.GetPresenceSessionId()}}}
	result := fixture.service.ExecuteHubCommand(command, 3, now)
	if result.GetResultCode() != cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED || fixture.service.HasPresence("daemon-1") {
		t.Fatalf("kick result = %v", result)
	}
	replay := fixture.service.ExecuteHubCommand(proto.Clone(command).(*cloudpb.HubCommand), 3, now.Add(time.Second))
	if !proto.Equal(result, replay) {
		t.Fatalf("kick replay = %v, want %v", replay, result)
	}
	_ = presence.Close()
	newPresence, _ := fixture.openEdgePresence(t)
	defer newPresence.Close()
	conflict := proto.Clone(command).(*cloudpb.HubCommand)
	conflict.ExpiresAtUnixMillis++
	if got := fixture.service.ExecuteHubCommand(conflict, 3, now.Add(time.Second)); got.GetErrorCode() != "command_replay_conflict" || !fixture.service.HasPresence("daemon-1") {
		t.Fatalf("conflicting replay affected new Presence: %v", got)
	}
}

func TestHubCommandFencesOnlyExactAssignmentGeneration(t *testing.T) {
	fixture := newFixture(t, 8, 8)
	presence, _ := fixture.openEdgePresence(t)
	now := fixture.clock.Now()
	command := &cloudpb.HubCommand{CommandId: "fence-1", CommandKind: cloudpb.HubCommandKind_HUB_COMMAND_KIND_FENCE_ASSIGNMENT, IssuedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Minute).UnixMilli(), Target: &cloudpb.HubCommand_FenceAssignment{FenceAssignment: &cloudpb.FenceAssignment{MigrationId: "migration-1", FenceCommandId: "fence-1", DaemonDeviceId: "daemon-1", SourceHubId: "hub-eu", SourceAssignmentEpoch: 1, SourceControlGeneration: 5, ExpiresAtUnixMillis: now.Add(time.Minute).UnixMilli()}}}
	if result := fixture.service.ExecuteHubCommand(command, 4, now); result.GetErrorCode() != "invalid_assignment_fence" || !fixture.service.HasPresence("daemon-1") {
		t.Fatalf("stale fence result = %v", result)
	}
	exact := proto.Clone(command).(*cloudpb.HubCommand)
	exact.CommandId = "fence-2"
	exact.GetFenceAssignment().FenceCommandId = "fence-2"
	result := fixture.service.ExecuteHubCommand(exact, 5, now)
	if result.GetResultCode() != cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED || fixture.service.HasPresence("daemon-1") {
		t.Fatalf("exact fence result = %v", result)
	}
	if replay := fixture.service.ExecuteHubCommand(proto.Clone(exact).(*cloudpb.HubCommand), 6, now.Add(time.Second)); replay.GetResultCode() != cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED || replay.GetControlGeneration() != 6 || replay.GetExecutionControlGeneration() != 5 {
		t.Fatalf("fence replay = %v", replay)
	}
	restartedCommand := proto.Clone(command).(*cloudpb.HubCommand)
	restartedCommand.CommandId = "fence-after-restart"
	restartedCommand.GetFenceAssignment().FenceCommandId = "fence-after-restart"
	if result := fixture.service.ExecuteHubCommand(restartedCommand, 6, now.Add(2*time.Second)); result.GetErrorCode() != "invalid_assignment_fence" {
		t.Fatalf("new process generation must not attest old fence = %v", result)
	}
	_ = presence.Close()
}

func TestHubCommandForwardsExactDaemonSessionAndReportsIndependentResult(t *testing.T) {
	fixture := newFixture(t, 8, 8)
	presence, _ := fixture.openEdgePresence(t)
	defer presence.Close()
	presenceID := fixture.service.TopologySnapshot(1, fixture.clock.Now()).GetPresences()[0].GetPresenceSessionId()
	target := &cloudpb.ManagedPeerSessionTarget{DaemonDeviceId: "daemon-1", ManagedSessionId: "managed-1", SessionIncarnation: 4, AssignmentEpoch: 1, ControlPresenceSessionId: presenceID, DaemonRuntimeGeneration: "runtime-1"}
	session := &cloudpb.ManagedPeerSessionProjection{Target: target, ClientDeviceId: "client-1", EstablishedPresenceSessionId: presenceID, ControlOwnerHubId: "hub-eu", ObservedDataPath: cloudpb.ObservedPath_OBSERVED_PATH_DIRECT, State: cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_READY, Freshness: cloudpb.Freshness_FRESHNESS_FRESH}
	if _, err := fixture.service.ReportDaemonRuntime("daemon-1", runtimeRequest("runtime-1", 1, presenceID, []*cloudpb.ManagedPeerSessionProjection{session}, fixture.clock.Now().UnixMilli())); err != nil {
		t.Fatal(err)
	}
	now := fixture.clock.Now()
	daemonCommand := &cloudpb.DaemonControlCommand{CommandId: "close-1", CommandKind: cloudpb.DaemonControlCommandKind_DAEMON_CONTROL_COMMAND_KIND_CLOSE_MANAGED_PEER_SESSION, AccountId: "account-1", TargetDeviceId: "daemon-1", HubId: "hub-eu", AssignmentEpoch: 1, AuthEpoch: 1, PresenceSessionId: presenceID, DaemonRuntimeGeneration: "runtime-1", IssuedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Minute).UnixMilli(), ControlKeyId: "controller-control", Target: &cloudpb.DaemonControlCommand_ManagedPeerSession{ManagedPeerSession: target}, Signature: []byte("signature")}
	command := &cloudpb.HubCommand{CommandId: "close-1", CommandKind: cloudpb.HubCommandKind_HUB_COMMAND_KIND_FORWARD_DAEMON_COMMAND, IssuedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Minute).UnixMilli(), Target: &cloudpb.HubCommand_DaemonCommand{DaemonCommand: daemonCommand}}
	staleAuthority := proto.Clone(command).(*cloudpb.HubCommand)
	staleAuthority.CommandId = "close-stale-auth"
	staleAuthority.GetDaemonCommand().CommandId = "close-stale-auth"
	staleAuthority.GetDaemonCommand().AuthEpoch = 2
	if result := fixture.service.ExecuteHubCommand(staleAuthority, 5, now); result.GetErrorCode() != "stale_daemon_authority" {
		t.Fatalf("stale auth result = %v", result)
	}
	if result := fixture.service.ExecuteHubCommand(command, 5, now); result.GetResultCode() != cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED {
		t.Fatalf("forward result = %v", result)
	}
	event, err := presence.Receive(context.Background())
	if err != nil || event.DaemonCommand.GetCommandId() != "close-1" {
		t.Fatalf("daemon command event = (%v, %v)", event, err)
	}
	if replay := fixture.service.ExecuteHubCommand(proto.Clone(command).(*cloudpb.HubCommand), 6, now.Add(time.Second)); replay.GetControlGeneration() != 6 {
		t.Fatalf("reconnected command replay = %v", replay)
	}
	daemonResult := &cloudpb.DaemonCommandResult{CommandId: "close-1", DaemonDeviceId: "daemon-1", ManagedSessionId: "managed-1", SessionIncarnation: 4, AssignmentEpoch: 1, PresenceSessionId: presenceID, DaemonRuntimeGeneration: "runtime-1", ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED, ClosedRegistryRevision: 9, CompletedAtUnixMillis: now.Add(time.Second).UnixMilli()}
	response, err := fixture.service.ReportDaemonCommandResult("daemon-1", daemonResult)
	if err != nil || response.GetAcceptedCommandId() != "close-1" {
		t.Fatalf("daemon command result = (%v, %v)", response, err)
	}
	select {
	case runtimeEvent := <-fixture.service.RuntimeEvents():
		if runtimeEvent.GetDaemonCommandResult().GetClosedRegistryRevision() != 9 {
			t.Fatalf("runtime command event = %v", runtimeEvent)
		}
	case <-time.After(time.Second):
		t.Fatal("daemon result was not queued for Controller")
	}
	conflict := proto.Clone(daemonResult).(*cloudpb.DaemonCommandResult)
	conflict.ClosedRegistryRevision = 10
	if _, err := fixture.service.ReportDaemonCommandResult("daemon-1", conflict); !errors.Is(err, hub.ErrRuntimeReport) {
		t.Fatalf("conflicting daemon result = %v", err)
	}
}

func TestHubReplaysTerminalCommandAfterAccessProjectionBecomesRevoked(t *testing.T) {
	fixture := newFixture(t, 8, 8)
	presence, _ := fixture.openEdgePresence(t)
	defer presence.Close()
	presenceID := fixture.service.TopologySnapshot(1, fixture.clock.Now()).GetPresences()[0].GetPresenceSessionId()
	request := runtimeRequest("runtime-1", 0, presenceID, nil, fixture.clock.Now().UnixMilli())
	request.TerminalAccesses = &cloudpb.TerminalAccessInventorySnapshot{ReportId: request.GetReportId(), DaemonDeviceId: "daemon-1", ControlOwnerHubId: "hub-eu", AssignmentEpoch: 1, ControlPresenceSessionId: presenceID, DaemonRuntimeGeneration: "runtime-1", AccessProjectionRevision: 4, ObservedAtUnixMillis: fixture.clock.Now().UnixMilli(), Accesses: []*cloudpb.TerminalAccessProjection{{DaemonDeviceId: "daemon-1", OpaqueAccessReference: "opaque-1", SubjectFingerprintSummary: "subject-1234", State: cloudpb.TerminalAccessState_TERMINAL_ACCESS_STATE_ACTIVE, IssuedAtUnixMillis: fixture.clock.Now().Add(-time.Hour).UnixMilli(), ExpiresAtUnixMillis: fixture.clock.Now().Add(time.Hour).UnixMilli(), AccessProjectionRevision: 4}}}
	if _, err := fixture.service.ReportDaemonRuntime("daemon-1", request); err != nil {
		t.Fatal(err)
	}
	now := fixture.clock.Now()
	target := &cloudpb.RevokeTerminalAccessTarget{DaemonDeviceId: "daemon-1", OpaqueAccessReference: "opaque-1", AssignmentEpoch: 1, PresenceSessionId: presenceID, DaemonRuntimeGeneration: "runtime-1", AccessProjectionRevision: 4}
	daemonCommand := &cloudpb.DaemonControlCommand{CommandId: "access-1", CommandKind: cloudpb.DaemonControlCommandKind_DAEMON_CONTROL_COMMAND_KIND_REVOKE_TERMINAL_ACCESS, AccountId: "account-1", TargetDeviceId: "daemon-1", HubId: "hub-eu", AssignmentEpoch: 1, AuthEpoch: 1, PresenceSessionId: presenceID, DaemonRuntimeGeneration: "runtime-1", IssuedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Minute).UnixMilli(), ControlKeyId: "controller-control", Target: &cloudpb.DaemonControlCommand_TerminalAccess{TerminalAccess: target}, Signature: []byte("signature")}
	command := &cloudpb.HubCommand{CommandId: "access-1", CommandKind: cloudpb.HubCommandKind_HUB_COMMAND_KIND_FORWARD_DAEMON_COMMAND, IssuedAtUnixMillis: now.UnixMilli(), ExpiresAtUnixMillis: now.Add(time.Minute).UnixMilli(), Target: &cloudpb.HubCommand_DaemonCommand{DaemonCommand: daemonCommand}}
	if result := fixture.service.ExecuteHubCommand(command, 5, now); result.GetResultCode() != cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED {
		t.Fatalf("terminal forward result = %v", result)
	}
	if event, err := presence.Receive(context.Background()); err != nil || event.DaemonCommand.GetCommandId() != "access-1" {
		t.Fatalf("terminal command event = (%v, %v)", event, err)
	}
	revoked := proto.Clone(request).(*cloudpb.ReportDaemonRuntimeRequest)
	revoked.TerminalAccesses.AccessProjectionRevision = 5
	revoked.TerminalAccesses.Accesses[0].AccessProjectionRevision = 5
	revoked.TerminalAccesses.Accesses[0].State = cloudpb.TerminalAccessState_TERMINAL_ACCESS_STATE_REVOKED
	if _, err := fixture.service.ReportDaemonRuntime("daemon-1", revoked); err != nil {
		t.Fatal(err)
	}
	if replay := fixture.service.ExecuteHubCommand(proto.Clone(command).(*cloudpb.HubCommand), 6, now.Add(time.Second)); replay.GetResultCode() != cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED {
		t.Fatalf("terminal replay result = %v", replay)
	}
	if event, err := presence.Receive(context.Background()); err != nil || event.DaemonCommand.GetCommandId() != "access-1" {
		t.Fatalf("terminal replay event = (%v, %v)", event, err)
	}
}
