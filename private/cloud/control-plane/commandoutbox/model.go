// Package commandoutbox 实现 Controller 持久管理命令的 parent/child 状态机。
//
// 本包只消费 generated Cloud Proto。authority、delivery、execution 与 observed effect
// 分开推进；topology 观察不能冒充 Hub/daemon 的独立执行 receipt。
package commandoutbox

import (
	"errors"
	"fmt"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

var (
	// ErrCommandConflict 表示 command ID、idempotency key、版本或结果 fencing 冲突。
	ErrCommandConflict = errors.New("management command conflicts with durable state")
	// ErrCommandNotFound 表示账号隔离后的 command 不存在。
	ErrCommandNotFound = errors.New("management command not found")
)

// Record 是 Store 使用的持久 command projection 与内部 CAS 版本。
// Version 不暴露给 Web；外部状态只能读取 generated ManagementCommandProjection。
type Record struct {
	Projection     *cloudpb.ManagementCommandProjection
	IdempotencyKey string
	Version        uint64
}

// ValidateCreate 校验一个尚未投递的 parent/child command，并返回深拷贝。
// caller 必须已经完成账号授权与 topology target 解析；本函数不接受手写业务 DTO。
func ValidateCreate(projection *cloudpb.ManagementCommandProjection, idempotencyKey string, now time.Time) (*cloudpb.ManagementCommandProjection, error) {
	if projection == nil || projection.GetCommandId() == "" || projection.GetAccountId() == "" || projection.GetActor() == nil || projection.GetActor().GetActorId() == "" || projection.GetCommandKind() == cloudpb.ManagementCommandKind_MANAGEMENT_COMMAND_KIND_UNSPECIFIED || projection.GetTarget() == nil || idempotencyKey == "" || now.IsZero() || projection.GetCreatedAtUnixMillis() > now.UnixMilli() || projection.GetExpiresAtUnixMillis() <= now.UnixMilli() || projection.GetUpdatedAtUnixMillis() != projection.GetCreatedAtUnixMillis() {
		return nil, ErrCommandConflict
	}
	if projection.GetObservedEffect() != cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN {
		return nil, ErrCommandConflict
	}
	if len(projection.GetChildren()) == 0 {
		if projection.GetAuthorityResult() != cloudpb.CommandAuthorityResult_COMMAND_AUTHORITY_RESULT_COMMITTED || projection.GetDeliveryState() != cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_RUNTIME_RECEIVED || projection.GetExecutionState() != cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_ALREADY_SATISFIED {
			return nil, ErrCommandConflict
		}
		return proto.Clone(projection).(*cloudpb.ManagementCommandProjection), nil
	}
	if projection.GetDeliveryState() != cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING || projection.GetExecutionState() != cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING {
		return nil, ErrCommandConflict
	}
	seen := make(map[string]struct{}, len(projection.GetChildren()))
	for _, child := range projection.GetChildren() {
		if child == nil || child.GetChildCommandId() == "" || child.GetTargetHubId() == "" || child.GetTarget() == nil || child.GetDeliveryState() != cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING || child.GetExecutionState() != cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING || child.GetObservedEffect() != cloudpb.CommandObservedEffect_COMMAND_OBSERVED_EFFECT_UNKNOWN || child.GetUpdatedAtUnixMillis() != projection.GetCreatedAtUnixMillis() {
			return nil, ErrCommandConflict
		}
		if _, duplicate := seen[child.GetChildCommandId()]; duplicate {
			return nil, ErrCommandConflict
		}
		seen[child.GetChildCommandId()] = struct{}{}
	}
	return proto.Clone(projection).(*cloudpb.ManagementCommandProjection), nil
}

// ApplyHubResult 应用 Hub 本地执行或 daemon-forward receipt。
// KickPresence 的 Hub result 是 execution receipt；daemon command 的 Hub result 只证明 runtime 已接收。
func ApplyHubResult(projection *cloudpb.ManagementCommandProjection, result *cloudpb.HubCommandResult, now time.Time) (*cloudpb.ManagementCommandProjection, error) {
	if projection == nil || result == nil || result.GetCommandId() == "" || result.GetHubId() == "" || result.GetControlGeneration() == 0 || result.GetCompletedAtUnixMillis() <= 0 || now.IsZero() {
		return nil, ErrCommandConflict
	}
	next := proto.Clone(projection).(*cloudpb.ManagementCommandProjection)
	child := commandChild(next, result.GetCommandId())
	if child == nil || child.GetTargetHubId() != result.GetHubId() {
		return nil, ErrCommandConflict
	}
	child.DeliveryState = cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_HUB_RECEIVED
	if child.GetTarget().GetPresence() != nil {
		child.ExecutionState = executionState(result.GetResultCode())
	} else if result.GetResultCode() == cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED {
		child.DeliveryState = cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_RUNTIME_RECEIVED
	} else {
		child.ExecutionState = executionState(result.GetResultCode())
	}
	if result.GetErrorCode() != "" {
		child.LastError = &cloudpb.ManagementErrorDetail{Code: cloudpb.ManagementErrorCode_MANAGEMENT_ERROR_CODE_RUNTIME_UNAVAILABLE, Message: result.GetErrorCode(), Retryable: false}
	}
	child.UpdatedAtUnixMillis = result.GetCompletedAtUnixMillis()
	aggregate(next, now)
	return next, nil
}

// ApplyDaemonResult 应用 daemon 完整关闭 protocol/resource/DataChannel 后的独立 receipt。
// 结果必须匹配 child 的精确 ManagedPeerSessionTarget，不能由 topology CLOSED 替代。
func ApplyDaemonResult(projection *cloudpb.ManagementCommandProjection, result *cloudpb.DaemonCommandResult, now time.Time) (*cloudpb.ManagementCommandProjection, error) {
	if projection == nil || result == nil || result.GetCommandId() == "" || result.GetDaemonDeviceId() == "" || result.GetManagedSessionId() == "" || result.GetSessionIncarnation() == 0 || result.GetAssignmentEpoch() == 0 || result.GetPresenceSessionId() == "" || result.GetDaemonRuntimeGeneration() == "" || result.GetCompletedAtUnixMillis() <= 0 || now.IsZero() {
		return nil, ErrCommandConflict
	}
	next := proto.Clone(projection).(*cloudpb.ManagementCommandProjection)
	child := commandChild(next, result.GetCommandId())
	if child == nil || child.GetTarget().GetPeerSession() == nil {
		return nil, ErrCommandConflict
	}
	target := child.GetTarget().GetPeerSession()
	if target.GetDaemonDeviceId() != result.GetDaemonDeviceId() || target.GetManagedSessionId() != result.GetManagedSessionId() || target.GetSessionIncarnation() != result.GetSessionIncarnation() || target.GetAssignmentEpoch() != result.GetAssignmentEpoch() || target.GetControlPresenceSessionId() != result.GetPresenceSessionId() || target.GetDaemonRuntimeGeneration() != result.GetDaemonRuntimeGeneration() {
		return nil, ErrCommandConflict
	}
	child.DeliveryState = cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_RUNTIME_RECEIVED
	child.ExecutionState = executionState(result.GetResultCode())
	if result.GetErrorCode() != "" {
		child.LastError = &cloudpb.ManagementErrorDetail{Code: cloudpb.ManagementErrorCode_MANAGEMENT_ERROR_CODE_RUNTIME_UNAVAILABLE, Message: result.GetErrorCode(), Retryable: false}
	}
	child.UpdatedAtUnixMillis = result.GetCompletedAtUnixMillis()
	aggregate(next, now)
	return next, nil
}

// Expire 把尚未取得 execution receipt 的 child 标记为 EXPIRED/UNKNOWN。
// authority_result 保持不变；已经 APPLIED 的 sibling 不回滚。
func Expire(projection *cloudpb.ManagementCommandProjection, now time.Time) (*cloudpb.ManagementCommandProjection, error) {
	if projection == nil || now.IsZero() || now.UnixMilli() < projection.GetExpiresAtUnixMillis() {
		return nil, ErrCommandConflict
	}
	next := proto.Clone(projection).(*cloudpb.ManagementCommandProjection)
	for _, child := range next.GetChildren() {
		if child.GetExecutionState() == cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING {
			child.DeliveryState = cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_EXPIRED
			child.ExecutionState = cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_UNKNOWN
			child.LastError = &cloudpb.ManagementErrorDetail{Code: cloudpb.ManagementErrorCode_MANAGEMENT_ERROR_CODE_COMMAND_EXPIRED, Message: "management command expired", Retryable: false}
			child.UpdatedAtUnixMillis = now.UnixMilli()
		}
	}
	aggregate(next, now)
	return next, nil
}

func executionState(result cloudpb.RuntimeCommandResultCode) cloudpb.CommandExecutionState {
	switch result {
	case cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_APPLIED:
		return cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_APPLIED
	case cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_ALREADY_SATISFIED:
		return cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_ALREADY_SATISFIED
	case cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_PARTIAL:
		return cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PARTIAL
	case cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_REJECTED, cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_STALE_TARGET, cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_EXPIRED:
		return cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_REJECTED
	default:
		return cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_UNKNOWN
	}
}

func commandChild(projection *cloudpb.ManagementCommandProjection, commandID string) *cloudpb.ManagementCommandChildProjection {
	for _, child := range projection.GetChildren() {
		if child.GetChildCommandId() == commandID {
			return child
		}
	}
	return nil
}

func aggregate(projection *cloudpb.ManagementCommandProjection, now time.Time) {
	if len(projection.GetChildren()) == 0 {
		return
	}
	allDelivered, allApplied, allAlready := true, true, true
	anySuccess, anyFailure, anyPending := false, false, false
	delivery := cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_RUNTIME_RECEIVED
	for _, child := range projection.GetChildren() {
		if child.GetDeliveryState() == cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING {
			allDelivered = false
			delivery = cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING
		} else if child.GetDeliveryState() == cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_EXPIRED {
			delivery = cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_EXPIRED
		} else if child.GetDeliveryState() == cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_HUB_RECEIVED && delivery == cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_RUNTIME_RECEIVED {
			delivery = cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_HUB_RECEIVED
		}
		switch child.GetExecutionState() {
		case cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING:
			allApplied, allAlready, anyPending = false, false, true
		case cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_APPLIED:
			allAlready, anySuccess = false, true
		case cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_ALREADY_SATISFIED:
			allApplied, anySuccess = false, true
		default:
			allApplied, allAlready, anyFailure = false, false, true
		}
	}
	if allDelivered {
		projection.DeliveryState = delivery
	} else {
		projection.DeliveryState = cloudpb.CommandDeliveryState_COMMAND_DELIVERY_STATE_PENDING
	}
	switch {
	case anyPending:
		projection.ExecutionState = cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PENDING
	case allApplied:
		projection.ExecutionState = cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_APPLIED
	case allAlready:
		projection.ExecutionState = cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_ALREADY_SATISFIED
	case anySuccess && anyFailure:
		projection.ExecutionState = cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_PARTIAL
	case anyFailure:
		projection.ExecutionState = cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_REJECTED
	default:
		projection.ExecutionState = cloudpb.CommandExecutionState_COMMAND_EXECUTION_STATE_UNKNOWN
	}
	projection.UpdatedAtUnixMillis = now.UnixMilli()
}

func validateRecord(record Record) error {
	if record.Projection == nil || record.Version == 0 || record.IdempotencyKey == "" {
		return fmt.Errorf("invalid command record: %w", ErrCommandConflict)
	}
	return nil
}
