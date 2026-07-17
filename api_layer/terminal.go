package apilayer

import (
	"context"
	"errors"

	"github.com/lozzow/termx/proto/apipb"
	"github.com/lozzow/termx/transformer"
	"google.golang.org/protobuf/proto"
)

// TerminalController 是 API Layer 到 core terminal/path adapter 的 typed Proto API 边界。
// 每个实现必须返回当前 command 对应的 result，不得返回 wirepb、protocol DTO 或 UI model。
type TerminalController interface {
	TerminalDefaults(context.Context, *apipb.TerminalDefaultsCommand) (*apipb.TerminalDefaultsResult, error)
	TerminalCreate(context.Context, *apipb.TerminalCreateCommand) (*apipb.TerminalCreateResult, error)
	TerminalList(context.Context, *apipb.TerminalListCommand) (*apipb.TerminalListResult, error)
	TerminalGet(context.Context, *apipb.TerminalGetCommand) (*apipb.TerminalGetResult, error)
	TerminalRestart(context.Context, *apipb.TerminalRestartCommand) error
	TerminalKill(context.Context, *apipb.TerminalKillCommand) error
	TerminalRemove(context.Context, *apipb.TerminalRemoveCommand) error
	TerminalSetMetadata(context.Context, *apipb.TerminalSetMetadataCommand) error
	TerminalSetTags(context.Context, *apipb.TerminalSetTagsCommand) error
	TerminalAttach(context.Context, *apipb.TerminalAttachCommand) (*apipb.TerminalAttachResult, error)
	TerminalDetach(context.Context, *apipb.TerminalDetachCommand) error
	TerminalInput(context.Context, *apipb.TerminalInputCommand) error
	TerminalResize(context.Context, *apipb.TerminalResizeCommand) (*apipb.TerminalResizeResult, error)
	TerminalResizeLock(context.Context, *apipb.TerminalResizeLockCommand) (*apipb.TerminalResizeResult, error)
	PathListDirectories(context.Context, *apipb.PathListDirectoriesCommand) (*apipb.PathListDirectoriesResult, error)
}

func (service *Service) executeTerminal(ctx context.Context, command *apipb.CommandEnvelope, requestContext *apipb.RequestContext) *apipb.ResultEnvelope {
	requestID := requestContext.GetRequestId()
	required := transformer.RequiredCapabilityForCommand(command)
	if !transformer.HasCapability(requestContext, required) {
		return unsupportedCapability(requestID, required)
	}
	if err := transformer.ValidateTerminalCommand(command); err != nil {
		return errorResult(requestID, transformer.ErrorToProto(err, false))
	}
	if service == nil || service.terminals == nil {
		return unavailable(requestID, "terminal controller is unavailable")
	}

	switch value := command.GetCommand().(type) {
	case *apipb.CommandEnvelope_TerminalDefaults:
		result, err := service.terminals.TerminalDefaults(ctx, cloneMessage(value.TerminalDefaults))
		return terminalDefaultsResult(requestID, result, err)
	case *apipb.CommandEnvelope_TerminalCreate:
		result, err := service.terminals.TerminalCreate(ctx, cloneMessage(value.TerminalCreate))
		return terminalCreateResult(requestID, result, err)
	case *apipb.CommandEnvelope_TerminalList:
		result, err := service.terminals.TerminalList(ctx, cloneMessage(value.TerminalList))
		return terminalListResult(requestID, result, err)
	case *apipb.CommandEnvelope_TerminalGet:
		result, err := service.terminals.TerminalGet(ctx, cloneMessage(value.TerminalGet))
		return terminalGetResult(requestID, result, err)
	case *apipb.CommandEnvelope_TerminalRestart:
		return terminalAck(requestID, service.terminals.TerminalRestart(ctx, cloneMessage(value.TerminalRestart)))
	case *apipb.CommandEnvelope_TerminalKill:
		return terminalAck(requestID, service.terminals.TerminalKill(ctx, cloneMessage(value.TerminalKill)))
	case *apipb.CommandEnvelope_TerminalRemove:
		return terminalAck(requestID, service.terminals.TerminalRemove(ctx, cloneMessage(value.TerminalRemove)))
	case *apipb.CommandEnvelope_TerminalSetMetadata:
		return terminalAck(requestID, service.terminals.TerminalSetMetadata(ctx, cloneMessage(value.TerminalSetMetadata)))
	case *apipb.CommandEnvelope_TerminalSetTags:
		return terminalAck(requestID, service.terminals.TerminalSetTags(ctx, cloneMessage(value.TerminalSetTags)))
	case *apipb.CommandEnvelope_TerminalAttach:
		result, err := service.terminals.TerminalAttach(ctx, cloneMessage(value.TerminalAttach))
		return terminalAttachResult(requestID, result, err)
	case *apipb.CommandEnvelope_TerminalDetach:
		return terminalAck(requestID, service.terminals.TerminalDetach(ctx, cloneMessage(value.TerminalDetach)))
	case *apipb.CommandEnvelope_TerminalInput:
		return terminalAck(requestID, service.terminals.TerminalInput(ctx, cloneMessage(value.TerminalInput)))
	case *apipb.CommandEnvelope_TerminalResize:
		result, err := service.terminals.TerminalResize(ctx, cloneMessage(value.TerminalResize))
		return terminalResizeResult(requestID, result, err)
	case *apipb.CommandEnvelope_TerminalResizeLock:
		result, err := service.terminals.TerminalResizeLock(ctx, cloneMessage(value.TerminalResizeLock))
		return terminalResizeResult(requestID, result, err)
	case *apipb.CommandEnvelope_PathListDirectories:
		result, err := service.terminals.PathListDirectories(ctx, cloneMessage(value.PathListDirectories))
		return pathListDirectoriesResult(requestID, result, err)
	default:
		return errorResult(requestID, transformer.ErrorToProto(&transformer.ValidationError{Field: "command", Reason: "unsupported terminal command"}, false))
	}
}

func cloneMessage[T proto.Message](message T) T {
	return proto.Clone(message).(T)
}

func terminalAck(requestID string, err error) *apipb.ResultEnvelope {
	if err != nil {
		return errorResult(requestID, transformer.ErrorToProto(err, true))
	}
	return acknowledge(requestID)
}

func terminalDefaultsResult(requestID string, result *apipb.TerminalDefaultsResult, err error) *apipb.ResultEnvelope {
	if err != nil || result == nil {
		return terminalResultError(requestID, result, err)
	}
	return &apipb.ResultEnvelope{RequestId: requestID, Result: &apipb.ResultEnvelope_TerminalDefaults{TerminalDefaults: result}}
}

func terminalCreateResult(requestID string, result *apipb.TerminalCreateResult, err error) *apipb.ResultEnvelope {
	if err != nil || result == nil {
		return terminalResultError(requestID, result, err)
	}
	return &apipb.ResultEnvelope{RequestId: requestID, Result: &apipb.ResultEnvelope_TerminalCreate{TerminalCreate: result}}
}

func terminalListResult(requestID string, result *apipb.TerminalListResult, err error) *apipb.ResultEnvelope {
	if err != nil || result == nil {
		return terminalResultError(requestID, result, err)
	}
	return &apipb.ResultEnvelope{RequestId: requestID, Result: &apipb.ResultEnvelope_TerminalList{TerminalList: result}}
}

func terminalGetResult(requestID string, result *apipb.TerminalGetResult, err error) *apipb.ResultEnvelope {
	if err != nil || result == nil {
		return terminalResultError(requestID, result, err)
	}
	return &apipb.ResultEnvelope{RequestId: requestID, Result: &apipb.ResultEnvelope_TerminalGet{TerminalGet: result}}
}

func terminalAttachResult(requestID string, result *apipb.TerminalAttachResult, err error) *apipb.ResultEnvelope {
	if err != nil || result == nil {
		return terminalResultError(requestID, result, err)
	}
	return &apipb.ResultEnvelope{RequestId: requestID, Result: &apipb.ResultEnvelope_TerminalAttach{TerminalAttach: result}}
}

func terminalResizeResult(requestID string, result *apipb.TerminalResizeResult, err error) *apipb.ResultEnvelope {
	if err != nil || result == nil {
		return terminalResultError(requestID, result, err)
	}
	return &apipb.ResultEnvelope{RequestId: requestID, Result: &apipb.ResultEnvelope_TerminalResize{TerminalResize: result}}
}

func pathListDirectoriesResult(requestID string, result *apipb.PathListDirectoriesResult, err error) *apipb.ResultEnvelope {
	if err != nil || result == nil {
		return terminalResultError(requestID, result, err)
	}
	return &apipb.ResultEnvelope{RequestId: requestID, Result: &apipb.ResultEnvelope_PathListDirectories{PathListDirectories: result}}
}

func terminalResultError(requestID string, result any, err error) *apipb.ResultEnvelope {
	if err == nil && result == nil {
		err = errors.New("terminal controller returned nil result")
	}
	return errorResult(requestID, transformer.ErrorToProto(err, true))
}
