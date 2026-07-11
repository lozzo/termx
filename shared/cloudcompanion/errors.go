package cloudcompanion

import (
	"errors"
	"fmt"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
)

// Error 是 public runtime 可以稳定分类的 Cloud Companion 错误。
// Code 是程序分支真值；Message 只能用于脱敏展示，CorrelationID 只能用于服务端诊断，不能包含凭据或 terminal 数据。
type Error struct {
	Code          cloudpb.CloudErrorCode
	Message       string
	Retryable     bool
	RetryAfter    time.Duration
	CorrelationID string
}

// Error 返回稳定错误码和可展示消息。
// 即使 Message 为空也保留错误码，避免调用方依赖服务端英文文案做控制流判断。
func (err *Error) Error() string {
	if err == nil {
		return "<nil>"
	}
	if err.Message == "" {
		return err.Code.String()
	}
	return fmt.Sprintf("%s: %s", err.Code.String(), err.Message)
}

// NewError 创建一个本地稳定 cloud 错误。
// 该函数用于 companion 缺失、IPC 不兼容等 public-side 失败；code 不得使用 UNSPECIFIED。
func NewError(code cloudpb.CloudErrorCode, message string) *Error {
	if code == cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNSPECIFIED {
		code = cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL
	}
	return &Error{Code: code, Message: message}
}

// ErrorFromWire 把 companion 返回的 protobuf 错误转换为本地稳定错误。
// nil wire error 表示协议违规并映射为 PROTOCOL，不能被当作成功或临时网络故障。
func ErrorFromWire(wire *cloudpb.CloudError) *Error {
	if wire == nil {
		return NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "companion returned an empty error")
	}
	code := wire.GetCode()
	if code == cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNSPECIFIED {
		code = cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL
	}
	return &Error{
		Code:          code,
		Message:       wire.GetMessage(),
		Retryable:     wire.GetRetryable(),
		RetryAfter:    time.Duration(wire.GetRetryAfterMillis()) * time.Millisecond,
		CorrelationID: wire.GetCorrelationId(),
	}
}

// ErrorToWire 把稳定本地错误投影成 protobuf 错误。
// 未分类错误统一映射为 PROTOCOL；该转换不得把原始凭据、请求体或 terminal payload 拼进 Message。
func ErrorToWire(err error) *cloudpb.CloudError {
	if err == nil {
		return nil
	}
	var cloudErr *Error
	if !errors.As(err, &cloudErr) {
		return &cloudpb.CloudError{Code: cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, Message: "public runtime reported an unclassified protocol error"}
	}
	retryAfterMillis := uint64(0)
	if cloudErr.RetryAfter > 0 {
		retryAfterMillis = uint64(cloudErr.RetryAfter / time.Millisecond)
	}
	return &cloudpb.CloudError{
		Code:             cloudErr.Code,
		Message:          cloudErr.Message,
		Retryable:        cloudErr.Retryable,
		RetryAfterMillis: retryAfterMillis,
		CorrelationId:    cloudErr.CorrelationID,
	}
}

// CodeOf 返回错误链中的稳定 cloud 错误码。
// 非 cloud 错误返回 UNSPECIFIED，调用方必须保留原始失败，不能据此尝试旧 Hub 或其他 transport。
func CodeOf(err error) cloudpb.CloudErrorCode {
	var cloudErr *Error
	if errors.As(err, &cloudErr) {
		return cloudErr.Code
	}
	return cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNSPECIFIED
}

// IsCode 判断错误链是否包含指定稳定 cloud 错误码。
// 它只用于 endpoint 局部状态投影，不改变 retry、fallback 或授权语义。
func IsCode(err error, code cloudpb.CloudErrorCode) bool {
	return CodeOf(err) == code
}
