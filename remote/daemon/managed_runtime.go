package daemon

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/remoteauth"
)

// ManagedRuntime 是 daemon process 对 Cloud managed session registry 与当前控制 Presence 的 owner。
// runtime generation 在进程内固定；Presence 续约只能替换 control Presence，不能复制 session map。
type ManagedRuntime struct {
	mu                sync.RWMutex
	daemonDeviceID    string
	runtimeGeneration string
	registry          *ManagedSessionRegistry
	commandMu         sync.Mutex
}

// NewManagedRuntime 创建进程级 runtime generation；random 为空时使用 crypto/rand。
// 生成值只用于 correlation/fencing，不进入 credential、terminal payload 或持久配置。
func NewManagedRuntime(daemonDeviceID string, random io.Reader) (*ManagedRuntime, error) {
	if daemonDeviceID == "" {
		return nil, fmt.Errorf("create managed runtime: %w", ErrManagedSessionRegistryTarget)
	}
	if random == nil {
		random = rand.Reader
	}
	value := make([]byte, 18)
	if _, err := io.ReadFull(random, value); err != nil {
		return nil, fmt.Errorf("generate daemon runtime generation: %w", err)
	}
	return &ManagedRuntime{daemonDeviceID: daemonDeviceID, runtimeGeneration: "runtime-" + base64.RawURLEncoding.EncodeToString(value)}, nil
}

// RuntimeGeneration 返回当前 daemon 进程固定的 generation。
func (runtime *ManagedRuntime) RuntimeGeneration() string {
	if runtime == nil {
		return ""
	}
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return runtime.runtimeGeneration
}

// BindPresence 绑定经过 Hub 验证的当前 control Presence。
// 同 Hub/assignment 的续约保留 active session；跨 assignment 只有空 inventory 时才能建立新 registry。
func (runtime *ManagedRuntime) BindPresence(hubID string, assignmentEpoch uint64, presenceSessionID string, observedAt time.Time) error {
	if runtime == nil || hubID == "" || assignmentEpoch == 0 || presenceSessionID == "" || observedAt.IsZero() {
		return fmt.Errorf("bind managed runtime presence: %w", ErrManagedSessionRegistryTarget)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.registry == nil {
		registry, err := NewManagedSessionRegistry(runtime.daemonDeviceID, runtime.runtimeGeneration, hubID, assignmentEpoch, presenceSessionID)
		if err != nil {
			return err
		}
		runtime.registry = registry
		return nil
	}
	if runtime.registry.controlOwnerHubID == hubID && runtime.registry.assignmentEpoch == assignmentEpoch {
		_, err := runtime.registry.ReplaceControlPresence("presence-bind", hubID, assignmentEpoch, presenceSessionID, observedAt)
		return err
	}
	inventory, err := runtime.registry.Inventory("assignment-rebind", observedAt)
	if err != nil || len(inventory.GetSessions()) != 0 {
		return fmt.Errorf("bind managed runtime across active assignment: %w", ErrManagedSessionRegistryTransition)
	}
	registry, err := NewManagedSessionRegistry(runtime.daemonDeviceID, runtime.runtimeGeneration, hubID, assignmentEpoch, presenceSessionID)
	if err != nil {
		return err
	}
	runtime.registry = registry
	return nil
}

// Registry 返回当前 Presence 已绑定的 registry；未完成 PresenceReady 时返回 nil。
func (runtime *ManagedRuntime) Registry() *ManagedSessionRegistry {
	if runtime == nil {
		return nil
	}
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return runtime.registry
}

// ExecuteControlCommand 验签并执行当前 daemon process generation 的精确 deny-only 命令。
// result 必须先写入 enrollment-owned ControlReceiptStore，caller 才能向 Companion 回报。
func (runtime *ManagedRuntime) ExecuteControlCommand(ctx context.Context, command *cloudpb.DaemonControlCommand, receipts *ControlReceiptStore, accessStore *remoteauth.AccessStore, now time.Time) (*cloudpb.DaemonCommandResult, error) {
	if runtime == nil || command == nil || receipts == nil || now.IsZero() {
		return nil, cloudpb.ErrInvalidDaemonControlCommand
	}
	runtime.commandMu.Lock()
	defer runtime.commandMu.Unlock()
	replayed, digest, err := receipts.VerifyOrReplay(command, now)
	if err != nil {
		return nil, err
	}
	if replayed != nil {
		return replayed, nil
	}

	runtime.mu.RLock()
	registry := runtime.registry
	runtimeGeneration := runtime.runtimeGeneration
	daemonDeviceID := runtime.daemonDeviceID
	runtime.mu.RUnlock()
	if registry == nil || command.GetTargetDeviceId() != daemonDeviceID || command.GetDaemonRuntimeGeneration() != runtimeGeneration {
		return nil, fmt.Errorf("daemon command runtime target mismatch: %w", ErrManagedSessionRegistryTarget)
	}
	if command.GetHubId() != registry.controlOwnerHubID || command.GetAssignmentEpoch() != registry.assignmentEpoch || command.GetPresenceSessionId() != registry.controlPresenceSessionID {
		return nil, fmt.Errorf("daemon command Presence target mismatch: %w", ErrManagedSessionRegistryTarget)
	}
	result := &cloudpb.DaemonCommandResult{CommandId: command.GetCommandId(), DaemonDeviceId: daemonDeviceID, AssignmentEpoch: command.GetAssignmentEpoch(), PresenceSessionId: command.GetPresenceSessionId(), DaemonRuntimeGeneration: runtimeGeneration, CompletedAtUnixMillis: now.UnixMilli()}
	switch command.GetCommandKind() {
	case cloudpb.DaemonControlCommandKind_DAEMON_CONTROL_COMMAND_KIND_CLOSE_MANAGED_PEER_SESSION:
		target := command.GetManagedPeerSession()
		if target == nil {
			return nil, cloudpb.ErrInvalidDaemonControlCommand
		}
		closed, err := registry.CloseExact(ctx, target, now)
		if err != nil {
			return nil, err
		}
		result.ManagedSessionId, result.SessionIncarnation = target.GetManagedSessionId(), target.GetSessionIncarnation()
		result.ClosedRegistryRevision = closed.GetRegistryRevision()
		switch closed.GetDisposition() {
		case cloudpb.ExactSessionCloseDisposition_EXACT_SESSION_CLOSE_DISPOSITION_REQUESTED:
			result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED
		case cloudpb.ExactSessionCloseDisposition_EXACT_SESSION_CLOSE_DISPOSITION_ALREADY_CLOSED, cloudpb.ExactSessionCloseDisposition_EXACT_SESSION_CLOSE_DISPOSITION_NOT_FOUND:
			result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_ALREADY_SATISFIED
		case cloudpb.ExactSessionCloseDisposition_EXACT_SESSION_CLOSE_DISPOSITION_STALE_TARGET:
			result.ResultCode, result.ErrorCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_STALE_TARGET, "stale_target"
		default:
			result.ResultCode, result.ErrorCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_REJECTED, "close_rejected"
		}
	case cloudpb.DaemonControlCommandKind_DAEMON_CONTROL_COMMAND_KIND_REVOKE_TERMINAL_ACCESS:
		if accessStore == nil || command.GetTerminalAccess() == nil {
			return nil, cloudpb.ErrInvalidDaemonControlCommand
		}
		accessTarget := command.GetTerminalAccess()
		reference := accessTarget.GetOpaqueAccessReference()
		if accessTarget.GetAccessProjectionRevision() != accessStore.AccessProjectionRevision() {
			result.ResultCode, result.ErrorCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_STALE_TARGET, "stale_access_projection"
			result.OpaqueAccessReference = reference
			result.AccessProjectionRevision = accessStore.AccessProjectionRevision()
			break
		}
		grantID, alreadyRevoked, err := resolveOpaqueGrant(accessStore, daemonDeviceID, reference)
		if err != nil {
			result.ResultCode, result.ErrorCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_STALE_TARGET, "stale_access_reference"
			break
		}
		if _, err := accessStore.RevokeGrant(grantID); err != nil {
			return nil, err
		}
		closedCount, revision, err := registry.CloseAccess(ctx, reference, now)
		if err != nil {
			return nil, err
		}
		result.OpaqueAccessReference = reference
		result.AccessProjectionRevision = accessStore.AccessProjectionRevision()
		result.ClosedSessionCount = closedCount
		result.ClosedRegistryRevision = revision
		if alreadyRevoked {
			result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_ALREADY_SATISFIED
		} else {
			result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED
		}
	default:
		return nil, cloudpb.ErrInvalidDaemonControlCommand
	}
	if err := receipts.CommitReceipt(command, digest, result); err != nil {
		return nil, err
	}
	return result, nil
}

func resolveOpaqueGrant(store *remoteauth.AccessStore, daemonDeviceID, reference string) (string, bool, error) {
	for _, record := range store.ListClientAccess() {
		if OpaqueAccessReference(daemonDeviceID, record.GrantID) == reference {
			return record.GrantID, !record.RevokedAt.IsZero(), nil
		}
	}
	return "", false, ErrManagedSessionRegistryTarget
}
