// Package apilayer 实现 generated Proto API 到 core-facing controller 的 application 边界。
package apilayer

import (
	"context"
	"fmt"

	"github.com/lozzow/termx/proto/apipb"
	"github.com/lozzow/termx/transformer"
	"google.golang.org/protobuf/proto"
)

const supportedAPIMajor uint32 = 1

// OperationController 是 API Layer 取消 core-owned operation 的窄内部边界。
// 跨层参数仍使用 generated proto，controller adapter 不得另建 Go request DTO。
type OperationController interface {
	CancelOperation(context.Context, *apipb.OperationStamp) error
}

// ResourceController 是 API Layer 释放 core-owned 长期资源的窄内部边界。
// 跨层参数仍使用 generated proto，resource handle 对 controller 保持 opaque。
type ResourceController interface {
	ReleaseResource(context.Context, *apipb.ResourceHandle) error
}

// Service 执行公共 Proto command，并保证所有失败返回 typed ResultEnvelope。
// Service 不拥有 transport framing；调用方负责 request correlation 和 payload 传输。
type Service struct {
	operations OperationController
	resources  ResourceController
}

// NewService 创建 application API service；缺失 controller 的对应 command 会 fail closed。
func NewService(operations OperationController, resources ResourceController) *Service {
	return &Service{operations: operations, resources: resources}
}

// Execute 校验并执行单个 typed command。返回值始终非 nil，领域失败不会泄露成 Go error。
func (service *Service) Execute(ctx context.Context, command *apipb.CommandEnvelope) *apipb.ResultEnvelope {
	if ctx == nil {
		ctx = context.Background()
	}
	requestID, requestContext := commandRequestContext(command)
	if err := transformer.ValidateRequestContext(requestContext); err != nil {
		return errorResult(requestID, transformer.ErrorToProto(err, false))
	}
	requestID = requestContext.GetRequestId()
	if requestContext.GetApiVersion().GetMajor() != supportedAPIMajor {
		return errorResult(requestID, &apipb.ApiError{
			Code:    apipb.ApiErrorCode_API_ERROR_CODE_UNSUPPORTED_VERSION,
			Message: fmt.Sprintf("unsupported API major version %d", requestContext.GetApiVersion().GetMajor()),
		})
	}
	if err := ctx.Err(); err != nil {
		return errorResult(requestID, transformer.ErrorToProto(err, false))
	}

	switch value := command.GetCommand().(type) {
	case *apipb.CommandEnvelope_CancelOperation:
		return service.cancelOperation(ctx, requestContext, value.CancelOperation)
	case *apipb.CommandEnvelope_ReleaseResource:
		return service.releaseResource(ctx, requestContext, value.ReleaseResource)
	default:
		return errorResult(requestID, &apipb.ApiError{
			Code:    apipb.ApiErrorCode_API_ERROR_CODE_INVALID_REQUEST,
			Message: "command is required",
			Detail:  &apipb.ApiError_Validation{Validation: &apipb.ValidationErrorDetail{Field: "command", Reason: "is required"}},
		})
	}
}

func (service *Service) cancelOperation(ctx context.Context, requestContext *apipb.RequestContext, command *apipb.CancelOperationCommand) *apipb.ResultEnvelope {
	requestID := requestContext.GetRequestId()
	if !transformer.HasCapability(requestContext, apipb.ApiCapability_API_CAPABILITY_OPERATION_CANCELLATION) {
		return unsupportedCapability(requestID, apipb.ApiCapability_API_CAPABILITY_OPERATION_CANCELLATION)
	}
	if err := transformer.ValidateOperationStamp(command.GetOperation(), requestContext.GetSession()); err != nil {
		return errorResult(requestID, transformer.ErrorToProto(err, false))
	}
	if service == nil || service.operations == nil {
		return unavailable(requestID, "operation controller is unavailable")
	}
	operation := proto.Clone(command.GetOperation()).(*apipb.OperationStamp)
	if err := service.operations.CancelOperation(ctx, operation); err != nil {
		return errorResult(requestID, transformer.ErrorToProto(err, true))
	}
	return acknowledge(requestID)
}

func (service *Service) releaseResource(ctx context.Context, requestContext *apipb.RequestContext, command *apipb.ReleaseResourceCommand) *apipb.ResultEnvelope {
	requestID := requestContext.GetRequestId()
	if !transformer.HasCapability(requestContext, apipb.ApiCapability_API_CAPABILITY_RESOURCE_LIFECYCLE) {
		return unsupportedCapability(requestID, apipb.ApiCapability_API_CAPABILITY_RESOURCE_LIFECYCLE)
	}
	if err := transformer.ValidateResourceHandle(command.GetResource()); err != nil {
		return errorResult(requestID, transformer.ErrorToProto(err, false))
	}
	if service == nil || service.resources == nil {
		return unavailable(requestID, "resource controller is unavailable")
	}
	resource := proto.Clone(command.GetResource()).(*apipb.ResourceHandle)
	if err := service.resources.ReleaseResource(ctx, resource); err != nil {
		return errorResult(requestID, transformer.ErrorToProto(err, true))
	}
	return acknowledge(requestID)
}

func commandRequestContext(command *apipb.CommandEnvelope) (string, *apipb.RequestContext) {
	if command == nil {
		return "", nil
	}
	switch value := command.GetCommand().(type) {
	case *apipb.CommandEnvelope_CancelOperation:
		if value.CancelOperation != nil {
			return value.CancelOperation.GetContext().GetRequestId(), value.CancelOperation.GetContext()
		}
	case *apipb.CommandEnvelope_ReleaseResource:
		if value.ReleaseResource != nil {
			return value.ReleaseResource.GetContext().GetRequestId(), value.ReleaseResource.GetContext()
		}
	}
	return "", nil
}

func acknowledge(requestID string) *apipb.ResultEnvelope {
	return &apipb.ResultEnvelope{RequestId: requestID, Result: &apipb.ResultEnvelope_Acknowledge{Acknowledge: &apipb.AcknowledgeResult{}}}
}

func errorResult(requestID string, apiError *apipb.ApiError) *apipb.ResultEnvelope {
	return &apipb.ResultEnvelope{RequestId: requestID, Result: &apipb.ResultEnvelope_Error{Error: apiError}}
}

func unsupportedCapability(requestID string, capability apipb.ApiCapability) *apipb.ResultEnvelope {
	return errorResult(requestID, &apipb.ApiError{
		Code:    apipb.ApiErrorCode_API_ERROR_CODE_UNSUPPORTED_CAPABILITY,
		Message: fmt.Sprintf("required API capability %s was not negotiated", capability.String()),
	})
}

func unavailable(requestID string, message string) *apipb.ResultEnvelope {
	return errorResult(requestID, &apipb.ApiError{Code: apipb.ApiErrorCode_API_ERROR_CODE_UNAVAILABLE, Message: message})
}
