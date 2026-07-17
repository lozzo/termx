package apimapping

import (
	"errors"
	"testing"

	"github.com/lozzow/termx/proto/apipb"
)

func TestRequestMetadataRejectsDuplicateCapabilities(t *testing.T) {
	err := ValidateRequestContext(&apipb.RequestContext{
		RequestId:  "request-1",
		ApiVersion: &apipb.ApiVersion{Major: 1},
		Capabilities: []apipb.ApiCapability{
			apipb.ApiCapability_API_CAPABILITY_TYPED_ERRORS,
			apipb.ApiCapability_API_CAPABILITY_TYPED_ERRORS,
		},
	})
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) || validationErr.Field != "context.capabilities[1]" {
		t.Fatalf("duplicate capability error=%#v", err)
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
