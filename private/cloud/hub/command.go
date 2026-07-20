package hub

import (
	"crypto/sha256"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

type hubCommandState struct {
	digest       [sha256.Size]byte
	command      *cloudpb.HubCommand
	hubResult    *cloudpb.HubCommandResult
	daemonResult *cloudpb.DaemonCommandResult
	expiresAt    time.Time
}

// ExecuteHubCommand 验证当前 Hub command envelope 后执行本地 Kick 或转发 daemon deny-only command。
// exact replay 返回首次 Hub result；相同 command ID 的不同 digest 被拒绝且不影响新 Presence/session。
func (service *Service) ExecuteHubCommand(command *cloudpb.HubCommand, controlGeneration uint64, now time.Time) *cloudpb.HubCommandResult {
	result := &cloudpb.HubCommandResult{ControlGeneration: controlGeneration, ExecutionControlGeneration: controlGeneration, ResultCode: cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_REJECTED, CompletedAtUnixMillis: now.UTC().UnixMilli()}
	if service == nil || command == nil || command.GetCommandId() == "" || controlGeneration == 0 || now.IsZero() {
		result.ErrorCode = "invalid_command"
		return result
	}
	result.CommandId, result.HubId = command.GetCommandId(), service.hubID
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(command)
	if err != nil {
		result.ErrorCode = "invalid_command"
		return result
	}
	digest := sha256.Sum256(payload)
	service.mu.Lock()
	defer service.mu.Unlock()
	service.cleanupLocked(now.UTC())
	if current := service.commands[command.GetCommandId()]; current != nil {
		if current.digest != digest {
			result.ErrorCode = "command_replay_conflict"
			return result
		}
		// Controller 会在 durable execution 仍为 PENDING 时重发同一 child。
		// Hub 内存 dedupe 不能吞掉 daemon command/result，否则一次断流会永久丢失独立 receipt。
		if current.daemonResult != nil {
			select {
			case service.runtimeEvents <- &cloudpb.HubRuntimeEnvelope{Payload: &cloudpb.HubRuntimeEnvelope_DaemonCommandResult{DaemonCommandResult: proto.Clone(current.daemonResult).(*cloudpb.DaemonCommandResult)}}:
			default:
			}
		} else if daemonCommand := current.command.GetDaemonCommand(); daemonCommand != nil {
			presence := service.presences[daemonCommand.GetTargetDeviceId()]
			if presence != nil && !presence.closed {
				select {
				case presence.events <- PresenceEvent{DaemonCommand: proto.Clone(daemonCommand).(*cloudpb.DaemonControlCommand)}:
				default:
				}
			}
		}
		replayed := proto.Clone(current.hubResult).(*cloudpb.HubCommandResult)
		if replayed.GetControlGeneration() != controlGeneration {
			replayed.ControlGeneration = controlGeneration
			replayed.CompletedAtUnixMillis = now.UTC().UnixMilli()
		}
		return replayed
	}
	if command.GetIssuedAtUnixMillis() > now.UnixMilli() || command.GetExpiresAtUnixMillis() <= now.UnixMilli() || command.GetExpiresAtUnixMillis() <= command.GetIssuedAtUnixMillis() {
		result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_EXPIRED
		result.ErrorCode = "command_expired"
		service.rememberCommandLocked(command, digest, result)
		return result
	}
	if len(service.commands) >= service.maxSessions {
		result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_UNKNOWN
		result.ErrorCode = "command_capacity"
		service.rememberCommandLocked(command, digest, result)
		return result
	}
	switch command.GetCommandKind() {
	case cloudpb.HubCommandKind_HUB_COMMAND_KIND_FENCE_ASSIGNMENT:
		service.executeFenceAssignmentLocked(command.GetFenceAssignment(), controlGeneration, result)
	case cloudpb.HubCommandKind_HUB_COMMAND_KIND_KICK_PRESENCE:
		service.executeKickPresenceLocked(command.GetKickPresence(), result)
	case cloudpb.HubCommandKind_HUB_COMMAND_KIND_FORWARD_DAEMON_COMMAND:
		service.forwardDaemonCommandLocked(command.GetDaemonCommand(), result)
	default:
		result.ErrorCode = "unsupported_command"
	}
	service.rememberCommandLocked(command, digest, result)
	return result
}

func (service *Service) executeFenceAssignmentLocked(target *cloudpb.FenceAssignment, controlGeneration uint64, result *cloudpb.HubCommandResult) {
	if target == nil || target.GetMigrationId() == "" || target.GetFenceCommandId() != result.GetCommandId() || target.GetDaemonDeviceId() == "" || target.GetSourceHubId() != service.hubID || target.GetSourceAssignmentEpoch() == 0 || target.GetSourceControlGeneration() != controlGeneration || target.GetExpiresAtUnixMillis() <= result.GetCompletedAtUnixMillis() {
		result.ErrorCode = "invalid_assignment_fence"
		return
	}
	if service.fenceAssignmentLocked(target.GetDaemonDeviceId(), target.GetSourceAssignmentEpoch()) {
		result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED
	} else {
		result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_ALREADY_SATISFIED
	}
}

func (service *Service) rememberCommandLocked(command *cloudpb.HubCommand, digest [sha256.Size]byte, result *cloudpb.HubCommandResult) {
	service.commands[command.GetCommandId()] = &hubCommandState{digest: digest, command: proto.Clone(command).(*cloudpb.HubCommand), hubResult: proto.Clone(result).(*cloudpb.HubCommandResult), expiresAt: time.UnixMilli(command.GetExpiresAtUnixMillis()).UTC()}
}

func (service *Service) executeKickPresenceLocked(target *cloudpb.KickPresenceTarget, result *cloudpb.HubCommandResult) {
	if target == nil || target.GetDaemonDeviceId() == "" || target.GetAssignmentEpoch() == 0 || target.GetPresenceSessionId() == "" {
		result.ErrorCode = "invalid_presence_target"
		return
	}
	presence := service.presences[target.GetDaemonDeviceId()]
	if presence == nil || presence.closed {
		observed := service.presenceTopology[target.GetDaemonDeviceId()]
		if observed != nil && observed.GetAssignmentEpoch() == target.GetAssignmentEpoch() && observed.GetPresenceSessionId() == target.GetPresenceSessionId() && observed.GetAvailability() == cloudpb.Availability_AVAILABILITY_OFFLINE {
			result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_ALREADY_SATISFIED
			return
		}
		result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_STALE_TARGET
		result.ErrorCode = "stale_presence"
		return
	}
	if presence.assignmentEpoch != target.GetAssignmentEpoch() || presence.sessionID != target.GetPresenceSessionId() {
		result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_STALE_TARGET
		result.ErrorCode = "stale_presence"
		return
	}
	service.closePresenceLocked(presence)
	delete(service.presences, presence.deviceID)
	result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED
}

func (service *Service) forwardDaemonCommandLocked(command *cloudpb.DaemonControlCommand, result *cloudpb.HubCommandResult) {
	if command == nil || command.GetCommandId() != result.GetCommandId() || command.GetHubId() != service.hubID || command.GetAccountId() == "" || command.GetTargetDeviceId() == "" || command.GetAssignmentEpoch() == 0 || command.GetPresenceSessionId() == "" || command.GetDaemonRuntimeGeneration() == "" || command.GetExpiresAtUnixMillis() <= command.GetIssuedAtUnixMillis() {
		result.ErrorCode = "invalid_daemon_command"
		return
	}
	if service.edgeAuthorizer == nil || service.edgeAuthorizer.AuthorizeDaemonControl(command.GetAccountId(), command.GetTargetDeviceId(), command.GetAuthEpoch()) != nil {
		result.ErrorCode = "stale_daemon_authority"
		return
	}
	presence := service.presences[command.GetTargetDeviceId()]
	runtime := service.runtimeTopology[command.GetTargetDeviceId()]
	if presence == nil || presence.closed || presence.accountID != command.GetAccountId() || presence.assignmentEpoch != command.GetAssignmentEpoch() || presence.sessionID != command.GetPresenceSessionId() || runtime == nil || runtime.presenceSessionID != presence.sessionID || runtime.assignmentEpoch != presence.assignmentEpoch || runtime.runtimeGeneration != command.GetDaemonRuntimeGeneration() {
		result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_STALE_TARGET
		result.ErrorCode = "stale_daemon_runtime"
		return
	}
	if target := command.GetManagedPeerSession(); target != nil && !runtimeHasSession(runtime, target) {
		result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_STALE_TARGET
		result.ErrorCode = "stale_managed_session"
		return
	}
	if target := command.GetTerminalAccess(); target != nil && !runtimeHasTerminalAccess(runtime, target) {
		result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_STALE_TARGET
		result.ErrorCode = "stale_terminal_access"
		return
	}
	select {
	case presence.events <- PresenceEvent{DaemonCommand: proto.Clone(command).(*cloudpb.DaemonControlCommand)}:
		result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED
	default:
		result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_UNKNOWN
		result.ErrorCode = "presence_backpressure"
	}
}

// ReportDaemonCommandResult 验证 daemon result 与此前转发 command 的全部 fencing，并加入 Hub runtime 上行队列。
// exact replay 可以再次上行供 Controller journal 幂等确认；冲突 replay 被拒绝。
func (service *Service) ReportDaemonCommandResult(daemonDeviceID string, result *cloudpb.DaemonCommandResult) (*cloudpb.ReportDaemonCommandResultResponse, error) {
	if service == nil || daemonDeviceID == "" || result == nil || result.GetCommandId() == "" || result.GetDaemonDeviceId() != daemonDeviceID {
		return nil, ErrRuntimeReport
	}
	now := service.clock.Now().UTC()
	service.mu.Lock()
	defer service.mu.Unlock()
	service.cleanupLocked(now)
	state := service.commands[result.GetCommandId()]
	if state == nil || state.command.GetDaemonCommand() == nil || !daemonResultMatchesCommand(state.command.GetDaemonCommand(), result) {
		return nil, ErrRuntimeReport
	}
	presence := service.presences[daemonDeviceID]
	if presence == nil || presence.closed || presence.assignmentEpoch != result.GetAssignmentEpoch() || presence.sessionID != result.GetPresenceSessionId() {
		return nil, ErrRuntimeReport
	}
	if state.daemonResult != nil && !proto.Equal(state.daemonResult, result) {
		return nil, ErrRuntimeReport
	}
	envelope := &cloudpb.HubRuntimeEnvelope{Payload: &cloudpb.HubRuntimeEnvelope_DaemonCommandResult{DaemonCommandResult: proto.Clone(result).(*cloudpb.DaemonCommandResult)}}
	select {
	case service.runtimeEvents <- envelope:
		state.daemonResult = proto.Clone(result).(*cloudpb.DaemonCommandResult)
		return &cloudpb.ReportDaemonCommandResultResponse{AcceptedCommandId: result.GetCommandId()}, nil
	default:
		return nil, ErrBackpressure
	}
}

// RuntimeEvents 返回独立 Hub/daemon command result 的有界上行事件源。
func (service *Service) RuntimeEvents() <-chan *cloudpb.HubRuntimeEnvelope {
	if service == nil {
		closed := make(chan *cloudpb.HubRuntimeEnvelope)
		close(closed)
		return closed
	}
	return service.runtimeEvents
}

func runtimeHasSession(runtime *daemonRuntimeTopology, target *cloudpb.ManagedPeerSessionTarget) bool {
	for _, session := range runtime.peerSessions {
		candidate := session.GetTarget()
		if candidate.GetDaemonDeviceId() == target.GetDaemonDeviceId() && candidate.GetManagedSessionId() == target.GetManagedSessionId() && candidate.GetSessionIncarnation() == target.GetSessionIncarnation() && candidate.GetAssignmentEpoch() == target.GetAssignmentEpoch() && candidate.GetControlPresenceSessionId() == target.GetControlPresenceSessionId() && candidate.GetDaemonRuntimeGeneration() == target.GetDaemonRuntimeGeneration() {
			return true
		}
	}
	return false
}

func runtimeHasTerminalAccess(runtime *daemonRuntimeTopology, target *cloudpb.RevokeTerminalAccessTarget) bool {
	inventory := runtime.terminalAccesses
	if inventory == nil || target == nil || inventory.GetDaemonDeviceId() != target.GetDaemonDeviceId() || inventory.GetAssignmentEpoch() != target.GetAssignmentEpoch() || inventory.GetControlPresenceSessionId() != target.GetPresenceSessionId() || inventory.GetDaemonRuntimeGeneration() != target.GetDaemonRuntimeGeneration() {
		return false
	}
	for _, access := range inventory.GetAccesses() {
		// Hub 只证明 opaque reference 属于当前 runtime。状态和 revision 由 daemon AccessStore
		// 验证；这样执行后 receipt 丢失时，REVOKED 新投影仍允许相同 signed command 重放。
		if access.GetOpaqueAccessReference() == target.GetOpaqueAccessReference() {
			return true
		}
	}
	return false
}

func daemonResultMatchesCommand(command *cloudpb.DaemonControlCommand, result *cloudpb.DaemonCommandResult) bool {
	if command.GetCommandId() != result.GetCommandId() || command.GetTargetDeviceId() != result.GetDaemonDeviceId() || command.GetAssignmentEpoch() != result.GetAssignmentEpoch() || command.GetPresenceSessionId() != result.GetPresenceSessionId() || command.GetDaemonRuntimeGeneration() != result.GetDaemonRuntimeGeneration() {
		return false
	}
	target := command.GetManagedPeerSession()
	if target != nil {
		return target.GetManagedSessionId() == result.GetManagedSessionId() && target.GetSessionIncarnation() == result.GetSessionIncarnation() && result.GetOpaqueAccessReference() == ""
	}
	access := command.GetTerminalAccess()
	return access != nil && access.GetOpaqueAccessReference() == result.GetOpaqueAccessReference() && result.GetManagedSessionId() == "" && result.GetSessionIncarnation() == 0 && result.GetAccessProjectionRevision() >= access.GetAccessProjectionRevision()
}
