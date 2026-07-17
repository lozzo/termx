package apimapping

import (
	"errors"
	"strings"
	"testing"

	corev2 "github.com/lozzow/termx/core"
	"github.com/lozzow/termx/proto/apipb"
	"google.golang.org/protobuf/encoding/protowire"
)

func TestRequestMetadataValidatesEnvelopeSession(t *testing.T) {
	err := ValidateRequestContext(&apipb.RequestContext{
		RequestId:  "request-1",
		ApiVersion: &apipb.ApiVersion{Major: 1},
		Session:    &apipb.EndpointSessionStamp{EndpointId: "studio", RouteId: "ssh"},
	})
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) || validationErr.Field != "session.generation" {
		t.Fatalf("session validation error=%#v", err)
	}
}

func TestRequestAndFenceIdentityLimits(t *testing.T) {
	contextMessage := &apipb.RequestContext{
		RequestId: strings.Repeat("r", maxAPIIdentityBytes+1), ApiVersion: &apipb.ApiVersion{Major: 1},
		Session: &apipb.EndpointSessionStamp{EndpointId: "studio", RouteId: "ssh", Generation: 1},
	}
	if err := ValidateRequestContext(contextMessage); err == nil {
		t.Fatal("oversized request ID must fail")
	}
	operation := &apipb.OperationStamp{
		Session:     &apipb.EndpointSessionStamp{EndpointId: "studio", RouteId: "ssh", Generation: 1},
		OperationId: strings.Repeat("o", maxAPIIdentityBytes+1),
	}
	if err := ValidateOperationStamp(operation, operation.GetSession()); err == nil {
		t.Fatal("oversized operation ID must fail")
	}
}

func TestSafeRequestCorrelationDropsInvalidFields(t *testing.T) {
	contextMessage := &apipb.RequestContext{
		RequestId: strings.Repeat("r", maxAPIIdentityBytes+1), ApiVersion: &apipb.ApiVersion{Major: 1},
		Session: &apipb.EndpointSessionStamp{EndpointId: strings.Repeat("e", maxAPIIdentityBytes+1), RouteId: "ssh", Generation: 1},
	}
	requestID, session := SafeRequestCorrelation(contextMessage)
	if requestID != "" || session != nil {
		t.Fatalf("unsafe correlation leaked: request_id=%q session=%#v", requestID, session)
	}
}

func TestSessionStampsEqualIgnoresUnknownFields(t *testing.T) {
	left := &apipb.EndpointSessionStamp{EndpointId: "studio", RouteId: "ssh", Generation: 7}
	right := &apipb.EndpointSessionStamp{EndpointId: "studio", RouteId: "ssh", Generation: 7}
	unknown := protowire.AppendTag(nil, 99, protowire.VarintType)
	unknown = protowire.AppendVarint(unknown, 1)
	left.ProtoReflect().SetUnknown(unknown)
	if !SessionStampsEqual(left, right) {
		t.Fatal("unknown fields must not change known session fence semantics")
	}
}

func TestCoreErrorsMapToStableCodesAndRetryability(t *testing.T) {
	tests := []struct {
		err       error
		code      apipb.ApiErrorCode
		retryable bool
	}{
		{err: corev2.ErrTerminalNotFound, code: apipb.ApiErrorCode_API_ERROR_CODE_NOT_FOUND},
		{err: corev2.ErrDuplicateTerminal, code: apipb.ApiErrorCode_API_ERROR_CODE_CONFLICT},
		{err: corev2.ErrInvalidCommand, code: apipb.ApiErrorCode_API_ERROR_CODE_INVALID_REQUEST},
		{err: corev2.ErrServerClosed, code: apipb.ApiErrorCode_API_ERROR_CODE_UNAVAILABLE, retryable: true},
	}
	for _, test := range tests {
		mapped := ErrorToProto(test.err, true)
		if mapped.GetCode() != test.code || mapped.GetRetryable() != test.retryable || !mapped.GetAttempted() {
			t.Fatalf("error %v mapped to %#v", test.err, mapped)
		}
	}
}

func TestValidationErrorMapsToTypedProtoDetail(t *testing.T) {
	apiError := ErrorToProto(&ValidationError{Field: "resource.id", Reason: "is required"}, false)
	if apiError.GetCode() != apipb.ApiErrorCode_API_ERROR_CODE_INVALID_REQUEST || apiError.GetAttempted() {
		t.Fatalf("api error=%#v", apiError)
	}
	if apiError.GetValidation().GetField() != "resource.id" || apiError.GetValidation().GetReason() != "is required" {
		t.Fatalf("validation detail=%#v", apiError.GetValidation())
	}
}
