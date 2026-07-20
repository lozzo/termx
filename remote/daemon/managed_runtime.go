package daemon

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

// ManagedRuntime 是 daemon process 对 Cloud managed session registry 与当前控制 Presence 的 owner。
// runtime generation 在进程内固定；Presence 续约只能替换 control Presence，不能复制 session map。
type ManagedRuntime struct {
	mu                sync.RWMutex
	daemonDeviceID    string
	runtimeGeneration string
	registry          *ManagedSessionRegistry
	commandMu         sync.Mutex
	commandReceipts   map[string]managedCommandReceipt
}

type managedCommandReceipt struct {
	digest    [sha256.Size]byte
	result    *cloudpb.DaemonCommandResult
	expiresAt time.Time
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
	return &ManagedRuntime{daemonDeviceID: daemonDeviceID, runtimeGeneration: "runtime-" + base64.RawURLEncoding.EncodeToString(value), commandReceipts: make(map[string]managedCommandReceipt)}, nil
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
// HUB004 的 receipt 只在进程内保留到 command expiry；HUB005 再由 enrollment-owned 持久 store 替换。
func (runtime *ManagedRuntime) ExecuteControlCommand(ctx context.Context, command *cloudpb.DaemonControlCommand, verifier *cloudpb.DaemonControlVerifier, now time.Time) (*cloudpb.DaemonCommandResult, error) {
	if runtime == nil || command == nil || verifier == nil || now.IsZero() {
		return nil, cloudpb.ErrInvalidDaemonControlCommand
	}
	if err := verifier.Verify(command, now); err != nil {
		return nil, err
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(command)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(payload)
	runtime.commandMu.Lock()
	defer runtime.commandMu.Unlock()
	for commandID, receipt := range runtime.commandReceipts {
		if !now.Before(receipt.expiresAt) {
			delete(runtime.commandReceipts, commandID)
		}
	}
	if receipt, ok := runtime.commandReceipts[command.GetCommandId()]; ok {
		if receipt.digest != digest {
			return nil, fmt.Errorf("daemon command replay conflict: %w", cloudpb.ErrInvalidDaemonControlCommand)
		}
		return proto.Clone(receipt.result).(*cloudpb.DaemonCommandResult), nil
	}

	runtime.mu.RLock()
	registry := runtime.registry
	runtimeGeneration := runtime.runtimeGeneration
	daemonDeviceID := runtime.daemonDeviceID
	runtime.mu.RUnlock()
	if registry == nil || command.GetCommandKind() != cloudpb.DaemonControlCommandKind_DAEMON_CONTROL_COMMAND_KIND_CLOSE_MANAGED_PEER_SESSION || command.GetTargetDeviceId() != daemonDeviceID || command.GetDaemonRuntimeGeneration() != runtimeGeneration || command.GetManagedPeerSession() == nil {
		return nil, fmt.Errorf("daemon command runtime target mismatch: %w", ErrManagedSessionRegistryTarget)
	}
	if command.GetHubId() != registry.controlOwnerHubID || command.GetAssignmentEpoch() != registry.assignmentEpoch || command.GetPresenceSessionId() != registry.controlPresenceSessionID {
		return nil, fmt.Errorf("daemon command Presence target mismatch: %w", ErrManagedSessionRegistryTarget)
	}
	closed, err := registry.CloseExact(ctx, command.GetManagedPeerSession(), now)
	if err != nil {
		return nil, err
	}
	result := &cloudpb.DaemonCommandResult{CommandId: command.GetCommandId(), DaemonDeviceId: daemonDeviceID, ManagedSessionId: command.GetManagedPeerSession().GetManagedSessionId(), SessionIncarnation: command.GetManagedPeerSession().GetSessionIncarnation(), AssignmentEpoch: command.GetAssignmentEpoch(), PresenceSessionId: command.GetPresenceSessionId(), DaemonRuntimeGeneration: runtimeGeneration, ClosedRegistryRevision: closed.GetRegistryRevision(), CompletedAtUnixMillis: now.UnixMilli()}
	switch closed.GetDisposition() {
	case cloudpb.ExactSessionCloseDisposition_EXACT_SESSION_CLOSE_DISPOSITION_REQUESTED:
		result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED
	case cloudpb.ExactSessionCloseDisposition_EXACT_SESSION_CLOSE_DISPOSITION_ALREADY_CLOSED, cloudpb.ExactSessionCloseDisposition_EXACT_SESSION_CLOSE_DISPOSITION_NOT_FOUND:
		result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_ALREADY_SATISFIED
	case cloudpb.ExactSessionCloseDisposition_EXACT_SESSION_CLOSE_DISPOSITION_STALE_TARGET:
		result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_STALE_TARGET
		result.ErrorCode = "stale_target"
	default:
		result.ResultCode = cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_REJECTED
		result.ErrorCode = "close_rejected"
	}
	runtime.commandReceipts[command.GetCommandId()] = managedCommandReceipt{digest: digest, result: proto.Clone(result).(*cloudpb.DaemonCommandResult), expiresAt: time.UnixMilli(command.GetExpiresAtUnixMillis()).UTC()}
	return result, nil
}
