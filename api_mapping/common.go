// Package apimapping 提供 core domain 与公共 Proto API 之间的无状态确定性映射。
// 它不建立连接、不处理 framing，也不拥有授权、session 或应用状态。
package apimapping

import (
	"context"
	"errors"
	"fmt"

	"github.com/lozzow/termx/proto/apipb"
)

// ValidationError 描述 proto 字段不满足 API contract；Field 使用 proto field path。
type ValidationError struct {
	Field  string
	Reason string
}

// Error 返回适合日志诊断的 validation 文本；客户端应读取转换后的 typed ApiError。
func (err *ValidationError) Error() string {
	if err == nil {
		return "invalid API request"
	}
	return fmt.Sprintf("%s: %s", err.Field, err.Reason)
}

// ValidateRequestContext 校验公共 request context 的版本、capability 与可选 session 结构。
func ValidateRequestContext(contextMessage *apipb.RequestContext) error {
	if contextMessage == nil {
		return validation("context", "is required")
	}
	if contextMessage.GetRequestId() == "" {
		return validation("context.request_id", "is required")
	}
	version := contextMessage.GetApiVersion()
	if version == nil || version.GetMajor() == 0 {
		return validation("context.api_version.major", "must be greater than zero")
	}
	seen := make(map[apipb.ApiCapability]struct{}, len(contextMessage.GetCapabilities()))
	for index, capability := range contextMessage.GetCapabilities() {
		if capability == apipb.ApiCapability_API_CAPABILITY_UNSPECIFIED {
			return validation(fmt.Sprintf("context.capabilities[%d]", index), "must be specified")
		}
		if _, exists := seen[capability]; exists {
			return validation(fmt.Sprintf("context.capabilities[%d]", index), "must not be duplicated")
		}
		seen[capability] = struct{}{}
	}
	if contextMessage.GetSession() != nil {
		if err := ValidateSessionStamp(contextMessage.GetSession()); err != nil {
			return err
		}
	}
	return nil
}

// HasCapability 返回 request context 是否显式协商指定 capability。
func HasCapability(contextMessage *apipb.RequestContext, capability apipb.ApiCapability) bool {
	for _, current := range contextMessage.GetCapabilities() {
		if current == capability {
			return true
		}
	}
	return false
}

// ValidateSessionStamp 校验 endpoint、route 和 generation fence。
func ValidateSessionStamp(stamp *apipb.EndpointSessionStamp) error {
	if stamp == nil {
		return validation("session", "is required")
	}
	if stamp.GetEndpointId() == "" {
		return validation("session.endpoint_id", "is required")
	}
	if stamp.GetRouteId() == "" {
		return validation("session.route_id", "is required")
	}
	if stamp.GetGeneration() == 0 {
		return validation("session.generation", "must be greater than zero")
	}
	return nil
}

// ValidateOperationStamp 校验 operation stamp，并要求它与 request session fence 一致。
func ValidateOperationStamp(stamp *apipb.OperationStamp, requestSession *apipb.EndpointSessionStamp) error {
	if stamp == nil {
		return validation("operation", "is required")
	}
	if stamp.GetOperationId() == "" {
		return validation("operation.operation_id", "is required")
	}
	if err := ValidateSessionStamp(stamp.GetSession()); err != nil {
		return err
	}
	if requestSession == nil || !sessionStampsEqual(stamp.GetSession(), requestSession) {
		return validation("operation.session", "must match context.session")
	}
	return nil
}

// ValidateResourceHandle 校验 opaque resource handle。
func ValidateResourceHandle(handle *apipb.ResourceHandle) error {
	if handle == nil {
		return validation("resource", "is required")
	}
	if handle.GetId() == "" {
		return validation("resource.id", "is required")
	}
	if handle.GetKind() == "" {
		return validation("resource.kind", "is required")
	}
	if handle.GetGeneration() == 0 {
		return validation("resource.generation", "must be greater than zero")
	}
	return nil
}

// ErrorToProto 把 API mapping/core adapter 失败映射为公共 typed error。
// attempted 必须由 API Layer 在 adapter 调用边界确定，API mapping 不推断是否已执行副作用。
func ErrorToProto(err error, attempted bool) *apipb.ApiError {
	if err == nil {
		return nil
	}
	var validationErr *ValidationError
	if errors.As(err, &validationErr) {
		return &apipb.ApiError{
			Code:      apipb.ApiErrorCode_API_ERROR_CODE_INVALID_REQUEST,
			Message:   validationErr.Error(),
			Attempted: attempted,
			Detail: &apipb.ApiError_Validation{Validation: &apipb.ValidationErrorDetail{
				Field: validationErr.Field, Reason: validationErr.Reason,
			}},
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &apipb.ApiError{Code: apipb.ApiErrorCode_API_ERROR_CODE_CANCELLED, Message: err.Error(), Attempted: attempted}
	}
	return &apipb.ApiError{Code: apipb.ApiErrorCode_API_ERROR_CODE_INTERNAL, Message: "application operation failed", Attempted: attempted}
}

func validation(field string, reason string) error {
	return &ValidationError{Field: field, Reason: reason}
}

func sessionStampsEqual(left *apipb.EndpointSessionStamp, right *apipb.EndpointSessionStamp) bool {
	return left.GetEndpointId() == right.GetEndpointId() && left.GetRouteId() == right.GetRouteId() && left.GetGeneration() == right.GetGeneration()
}
