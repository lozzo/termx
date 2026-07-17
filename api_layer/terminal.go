package apilayer

import (
	"context"
	"errors"
	"time"

	"github.com/lozzow/termx/api_mapping"
	"github.com/lozzow/termx/proto/apipb"
	"google.golang.org/protobuf/proto"
)

const attachmentCleanupTimeout = 5 * time.Second

// TerminalAttachTransaction 持有尚未发布的 attachment 资源。
// API Layer 校验 Result 后调用 Commit；任何校验或提交失败都调用 Rollback，防止客户端不可见资源泄漏。
type TerminalAttachTransaction interface {
	// Result 返回 pending attachment 的不可变 Proto projection；不得在返回后继续修改。
	Result() *apipb.TerminalAttachResult
	// Commit 原子发布 attachment，使后续 handle 操作可见。
	Commit(context.Context) error
	// Rollback 无条件释放 pending attachment；实现必须幂等并接受独立 cleanup context。
	Rollback(context.Context) error
}

// TerminalController 是 API Layer 到 core terminal/path adapter 的 typed Proto API 边界。
// 每个实现必须返回当前 command 对应的 result，不得返回 wirepb、protocol DTO 或 UI model。
type TerminalController interface {
	TerminalDefaults(context.Context, *apipb.EndpointSessionStamp, *apipb.TerminalDefaultsCommand) (*apipb.TerminalDefaultsResult, error)
	TerminalCreate(context.Context, *apipb.EndpointSessionStamp, *apipb.TerminalCreateCommand) (*apipb.TerminalCreateResult, error)
	TerminalList(context.Context, *apipb.EndpointSessionStamp, *apipb.TerminalListCommand) (*apipb.TerminalListResult, error)
	TerminalGet(context.Context, *apipb.EndpointSessionStamp, *apipb.TerminalGetCommand) (*apipb.TerminalGetResult, error)
	TerminalRestart(context.Context, *apipb.EndpointSessionStamp, *apipb.TerminalRestartCommand) error
	TerminalKill(context.Context, *apipb.EndpointSessionStamp, *apipb.TerminalKillCommand) error
	TerminalRemove(context.Context, *apipb.EndpointSessionStamp, *apipb.TerminalRemoveCommand) error
	TerminalSetMetadata(context.Context, *apipb.EndpointSessionStamp, *apipb.TerminalSetMetadataCommand) error
	TerminalSetTags(context.Context, *apipb.EndpointSessionStamp, *apipb.TerminalSetTagsCommand) error
	TerminalAttach(context.Context, *apipb.EndpointSessionStamp, *apipb.TerminalAttachCommand) (TerminalAttachTransaction, error)
	TerminalDetach(context.Context, *apipb.EndpointSessionStamp, *apipb.TerminalDetachCommand) error
	TerminalInput(context.Context, *apipb.EndpointSessionStamp, *apipb.TerminalInputCommand) error
	TerminalResize(context.Context, *apipb.EndpointSessionStamp, *apipb.TerminalResizeCommand) (*apipb.TerminalResizeResult, error)
	TerminalResizeLock(context.Context, *apipb.EndpointSessionStamp, *apipb.TerminalResizeLockCommand) (*apipb.TerminalResizeResult, error)
	PathListDirectories(context.Context, *apipb.EndpointSessionStamp, *apipb.PathListDirectoriesCommand) (*apipb.PathListDirectoriesResult, error)
}

func (service *Service) executeTerminal(ctx context.Context, currentSession *apipb.EndpointSessionStamp, command *apipb.CommandEnvelope, requestContext *apipb.RequestContext) *apipb.ResultEnvelope {
	requestID := requestContext.GetRequestId()
	if err := apimapping.ValidateTerminalCommand(command); err != nil {
		return errorResult(requestID, currentSession, apimapping.ErrorToProto(err, false))
	}
	if service == nil || service.terminals == nil {
		return unavailable(requestID, currentSession, "terminal controller is unavailable")
	}

	switch value := command.GetCommand().(type) {
	case *apipb.CommandEnvelope_TerminalDefaults:
		result, err := service.terminals.TerminalDefaults(ctx, cloneSession(currentSession), cloneMessage(value.TerminalDefaults))
		return terminalDefaultsResult(requestID, currentSession, result, err)
	case *apipb.CommandEnvelope_TerminalCreate:
		result, err := service.terminals.TerminalCreate(ctx, cloneSession(currentSession), cloneMessage(value.TerminalCreate))
		return terminalCreateResult(requestID, currentSession, result, err)
	case *apipb.CommandEnvelope_TerminalList:
		result, err := service.terminals.TerminalList(ctx, cloneSession(currentSession), cloneMessage(value.TerminalList))
		return terminalListResult(requestID, currentSession, result, err)
	case *apipb.CommandEnvelope_TerminalGet:
		result, err := service.terminals.TerminalGet(ctx, cloneSession(currentSession), cloneMessage(value.TerminalGet))
		return terminalGetResult(requestID, currentSession, result, err)
	case *apipb.CommandEnvelope_TerminalRestart:
		return terminalAck(requestID, currentSession, service.terminals.TerminalRestart(ctx, cloneSession(currentSession), cloneMessage(value.TerminalRestart)))
	case *apipb.CommandEnvelope_TerminalKill:
		return terminalAck(requestID, currentSession, service.terminals.TerminalKill(ctx, cloneSession(currentSession), cloneMessage(value.TerminalKill)))
	case *apipb.CommandEnvelope_TerminalRemove:
		return terminalAck(requestID, currentSession, service.terminals.TerminalRemove(ctx, cloneSession(currentSession), cloneMessage(value.TerminalRemove)))
	case *apipb.CommandEnvelope_TerminalSetMetadata:
		return terminalAck(requestID, currentSession, service.terminals.TerminalSetMetadata(ctx, cloneSession(currentSession), cloneMessage(value.TerminalSetMetadata)))
	case *apipb.CommandEnvelope_TerminalSetTags:
		return terminalAck(requestID, currentSession, service.terminals.TerminalSetTags(ctx, cloneSession(currentSession), cloneMessage(value.TerminalSetTags)))
	case *apipb.CommandEnvelope_TerminalAttach:
		transaction, err := service.terminals.TerminalAttach(ctx, cloneSession(currentSession), cloneMessage(value.TerminalAttach))
		return service.terminalAttachResult(ctx, requestID, currentSession, value.TerminalAttach, transaction, err)
	case *apipb.CommandEnvelope_TerminalDetach:
		return terminalAck(requestID, currentSession, service.terminals.TerminalDetach(ctx, cloneSession(currentSession), cloneMessage(value.TerminalDetach)))
	case *apipb.CommandEnvelope_TerminalInput:
		return terminalAck(requestID, currentSession, service.terminals.TerminalInput(ctx, cloneSession(currentSession), cloneMessage(value.TerminalInput)))
	case *apipb.CommandEnvelope_TerminalResize:
		result, err := service.terminals.TerminalResize(ctx, cloneSession(currentSession), cloneMessage(value.TerminalResize))
		return terminalResizeResult(requestID, currentSession, result, err)
	case *apipb.CommandEnvelope_TerminalResizeLock:
		result, err := service.terminals.TerminalResizeLock(ctx, cloneSession(currentSession), cloneMessage(value.TerminalResizeLock))
		return terminalResizeResult(requestID, currentSession, result, err)
	case *apipb.CommandEnvelope_PathListDirectories:
		result, err := service.terminals.PathListDirectories(ctx, cloneSession(currentSession), cloneMessage(value.PathListDirectories))
		return pathListDirectoriesResult(requestID, currentSession, result, err)
	default:
		return errorResult(requestID, currentSession, apimapping.ErrorToProto(&apimapping.ValidationError{Field: "command", Reason: "unsupported terminal command"}, false))
	}
}

func cloneMessage[T proto.Message](message T) T {
	return proto.Clone(message).(T)
}

func terminalAck(requestID string, session *apipb.EndpointSessionStamp, err error) *apipb.ResultEnvelope {
	if err != nil {
		return errorResult(requestID, session, apimapping.ErrorToProto(err, true))
	}
	return acknowledge(requestID, session)
}

func terminalDefaultsResult(requestID string, session *apipb.EndpointSessionStamp, result *apipb.TerminalDefaultsResult, err error) *apipb.ResultEnvelope {
	if err != nil || result == nil {
		return terminalResultError(requestID, session, err)
	}
	return &apipb.ResultEnvelope{RequestId: requestID, OriginSession: cloneSession(session), Result: &apipb.ResultEnvelope_TerminalDefaults{TerminalDefaults: cloneMessage(result)}}
}

func terminalCreateResult(requestID string, session *apipb.EndpointSessionStamp, result *apipb.TerminalCreateResult, err error) *apipb.ResultEnvelope {
	if err != nil || result == nil {
		return terminalResultError(requestID, session, err)
	}
	return &apipb.ResultEnvelope{RequestId: requestID, OriginSession: cloneSession(session), Result: &apipb.ResultEnvelope_TerminalCreate{TerminalCreate: cloneMessage(result)}}
}

func terminalListResult(requestID string, session *apipb.EndpointSessionStamp, result *apipb.TerminalListResult, err error) *apipb.ResultEnvelope {
	if err != nil || result == nil {
		return terminalResultError(requestID, session, err)
	}
	return &apipb.ResultEnvelope{RequestId: requestID, OriginSession: cloneSession(session), Result: &apipb.ResultEnvelope_TerminalList{TerminalList: cloneMessage(result)}}
}

func terminalGetResult(requestID string, session *apipb.EndpointSessionStamp, result *apipb.TerminalGetResult, err error) *apipb.ResultEnvelope {
	if err != nil || result == nil {
		return terminalResultError(requestID, session, err)
	}
	return &apipb.ResultEnvelope{RequestId: requestID, OriginSession: cloneSession(session), Result: &apipb.ResultEnvelope_TerminalGet{TerminalGet: cloneMessage(result)}}
}

func (service *Service) terminalAttachResult(ctx context.Context, requestID string, session *apipb.EndpointSessionStamp, command *apipb.TerminalAttachCommand, transaction TerminalAttachTransaction, err error) *apipb.ResultEnvelope {
	if err != nil {
		if transaction != nil {
			if rollbackErr := rollbackAttachment(ctx, transaction); rollbackErr != nil {
				return errorResult(requestID, session, &apipb.ApiError{Code: apipb.ApiErrorCode_API_ERROR_CODE_INTERNAL, Message: "terminal attachment failed and rollback failed", Attempted: true})
			}
		}
		return errorResult(requestID, session, apimapping.ErrorToProto(err, true))
	}
	if transaction == nil {
		return terminalResultError(requestID, session, err)
	}
	result := transaction.Result()
	if result != nil {
		result = cloneMessage(result)
	}
	if err := apimapping.ValidateTerminalAttachResult(command, result, session); err != nil {
		if rollbackErr := rollbackAttachment(ctx, transaction); rollbackErr != nil {
			return errorResult(requestID, session, &apipb.ApiError{Code: apipb.ApiErrorCode_API_ERROR_CODE_INTERNAL, Message: "terminal attachment publication failed and rollback failed", Attempted: true})
		}
		return errorResult(requestID, session, &apipb.ApiError{Code: apipb.ApiErrorCode_API_ERROR_CODE_INTERNAL, Message: "terminal controller returned an invalid attachment result", Attempted: true})
	}
	if err := transaction.Commit(ctx); err != nil {
		if rollbackErr := rollbackAttachment(ctx, transaction); rollbackErr != nil {
			return errorResult(requestID, session, &apipb.ApiError{Code: apipb.ApiErrorCode_API_ERROR_CODE_INTERNAL, Message: "terminal attachment commit and rollback failed", Attempted: true})
		}
		return errorResult(requestID, session, apimapping.ErrorToProto(err, true))
	}
	return &apipb.ResultEnvelope{RequestId: requestID, OriginSession: cloneSession(session), Result: &apipb.ResultEnvelope_TerminalAttach{TerminalAttach: cloneMessage(result)}}
}

func rollbackAttachment(ctx context.Context, transaction TerminalAttachTransaction) error {
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), attachmentCleanupTimeout)
	defer cancel()
	return transaction.Rollback(cleanupContext)
}

func terminalResizeResult(requestID string, session *apipb.EndpointSessionStamp, result *apipb.TerminalResizeResult, err error) *apipb.ResultEnvelope {
	if err != nil || result == nil {
		return terminalResultError(requestID, session, err)
	}
	return &apipb.ResultEnvelope{RequestId: requestID, OriginSession: cloneSession(session), Result: &apipb.ResultEnvelope_TerminalResize{TerminalResize: cloneMessage(result)}}
}

func pathListDirectoriesResult(requestID string, session *apipb.EndpointSessionStamp, result *apipb.PathListDirectoriesResult, err error) *apipb.ResultEnvelope {
	if err != nil || result == nil {
		return terminalResultError(requestID, session, err)
	}
	return &apipb.ResultEnvelope{RequestId: requestID, OriginSession: cloneSession(session), Result: &apipb.ResultEnvelope_PathListDirectories{PathListDirectories: cloneMessage(result)}}
}

func terminalResultError(requestID string, session *apipb.EndpointSessionStamp, err error) *apipb.ResultEnvelope {
	if err == nil {
		err = errors.New("terminal controller returned nil result")
	}
	return errorResult(requestID, session, apimapping.ErrorToProto(err, true))
}
