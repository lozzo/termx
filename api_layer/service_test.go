package apilayer

import (
	"context"
	"errors"
	"testing"

	"github.com/lozzow/termx/proto/apipb"
)

type fakeOperationController struct {
	operations []*apipb.OperationStamp
	err        error
}

func (controller *fakeOperationController) CancelOperation(_ context.Context, operation *apipb.OperationStamp) error {
	controller.operations = append(controller.operations, operation)
	return controller.err
}

type fakeResourceController struct {
	resources []*apipb.ResourceHandle
	err       error
}

func (controller *fakeResourceController) ReleaseResource(_ context.Context, resource *apipb.ResourceHandle) error {
	controller.resources = append(controller.resources, resource)
	return controller.err
}

func TestServiceExecutesTypedCancelWithSessionFence(t *testing.T) {
	operations := &fakeOperationController{}
	service := NewService(operations, nil)
	result := service.Execute(context.Background(), cancelCommand("request-1", 1, true))
	if result.GetAcknowledge() == nil || result.GetError() != nil {
		t.Fatalf("cancel result=%#v", result)
	}
	if len(operations.operations) != 1 || operations.operations[0].GetSession().GetEndpointId() != "studio" ||
		operations.operations[0].GetSession().GetRouteId() != "ssh" || operations.operations[0].GetSession().GetGeneration() != 7 ||
		operations.operations[0].GetOperationId() != "operation-1" {
		t.Fatalf("cancel operations=%#v", operations.operations)
	}
}

func TestServiceRejectsStaleOperationStampBeforeController(t *testing.T) {
	operations := &fakeOperationController{}
	command := cancelCommand("request-2", 1, true)
	command.GetCancelOperation().GetOperation().GetSession().Generation = 6
	result := NewService(operations, nil).Execute(context.Background(), command)
	if result.GetError().GetCode() != apipb.ApiErrorCode_API_ERROR_CODE_INVALID_REQUEST || result.GetError().GetAttempted() {
		t.Fatalf("stale result=%#v", result)
	}
	if len(operations.operations) != 0 {
		t.Fatalf("stale command reached controller: %#v", operations.operations)
	}
}

func TestServiceReportsControllerFailureAsAttempted(t *testing.T) {
	operations := &fakeOperationController{err: errors.New("write failed")}
	result := NewService(operations, nil).Execute(context.Background(), cancelCommand("request-3", 1, true))
	if result.GetError().GetCode() != apipb.ApiErrorCode_API_ERROR_CODE_INTERNAL || !result.GetError().GetAttempted() {
		t.Fatalf("controller failure=%#v", result)
	}
}

func TestServiceRequiresVersionCapabilityAndResourceController(t *testing.T) {
	resources := &fakeResourceController{}
	unsupportedVersion := NewService(nil, resources).Execute(context.Background(), releaseCommand("request-4", 2, true))
	if unsupportedVersion.GetError().GetCode() != apipb.ApiErrorCode_API_ERROR_CODE_UNSUPPORTED_VERSION {
		t.Fatalf("version result=%#v", unsupportedVersion)
	}
	missingCapability := NewService(nil, resources).Execute(context.Background(), releaseCommand("request-5", 1, false))
	if missingCapability.GetError().GetCode() != apipb.ApiErrorCode_API_ERROR_CODE_UNSUPPORTED_CAPABILITY {
		t.Fatalf("capability result=%#v", missingCapability)
	}
	unavailable := NewService(nil, nil).Execute(context.Background(), releaseCommand("request-6", 1, true))
	if unavailable.GetError().GetCode() != apipb.ApiErrorCode_API_ERROR_CODE_UNAVAILABLE || unavailable.GetError().GetAttempted() {
		t.Fatalf("unavailable result=%#v", unavailable)
	}
}

func TestServiceReleasesClonedProtoResource(t *testing.T) {
	resources := &fakeResourceController{}
	command := releaseCommand("request-resource", 1, true)
	result := NewService(nil, resources).Execute(context.Background(), command)
	if result.GetAcknowledge() == nil || len(resources.resources) != 1 {
		t.Fatalf("release result=%#v resources=%#v", result, resources.resources)
	}
	resources.resources[0].Id = "controller-mutated"
	if command.GetReleaseResource().GetResource().GetId() != "resource-1" {
		t.Fatalf("controller mutated client command: %#v", command.GetReleaseResource().GetResource())
	}
}

func TestServiceHonorsContextCancellationBeforeController(t *testing.T) {
	operations := &fakeOperationController{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := NewService(operations, nil).Execute(ctx, cancelCommand("request-7", 1, true))
	if result.GetError().GetCode() != apipb.ApiErrorCode_API_ERROR_CODE_CANCELLED || result.GetError().GetAttempted() {
		t.Fatalf("cancelled result=%#v", result)
	}
	if len(operations.operations) != 0 {
		t.Fatalf("cancelled context reached controller: %#v", operations.operations)
	}
}

func cancelCommand(requestID string, major uint32, includeCapability bool) *apipb.CommandEnvelope {
	contextMessage := requestContext(requestID, major)
	if includeCapability {
		contextMessage.Capabilities = []apipb.ApiCapability{apipb.ApiCapability_API_CAPABILITY_OPERATION_CANCELLATION}
	}
	return &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_CancelOperation{CancelOperation: &apipb.CancelOperationCommand{
		Context: contextMessage,
		Operation: &apipb.OperationStamp{
			Session: &apipb.EndpointSessionStamp{EndpointId: "studio", RouteId: "ssh", Generation: 7}, OperationId: "operation-1",
		},
	}}}
}

func releaseCommand(requestID string, major uint32, includeCapability bool) *apipb.CommandEnvelope {
	contextMessage := requestContext(requestID, major)
	if includeCapability {
		contextMessage.Capabilities = []apipb.ApiCapability{apipb.ApiCapability_API_CAPABILITY_RESOURCE_LIFECYCLE}
	}
	return &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_ReleaseResource{ReleaseResource: &apipb.ReleaseResourceCommand{
		Context:  contextMessage,
		Resource: &apipb.ResourceHandle{Id: "resource-1", Kind: "subscription", Generation: 3},
	}}}
}

func requestContext(requestID string, major uint32) *apipb.RequestContext {
	return &apipb.RequestContext{
		RequestId:  requestID,
		ApiVersion: &apipb.ApiVersion{Major: major},
		Session:    &apipb.EndpointSessionStamp{EndpointId: "studio", RouteId: "ssh", Generation: 7},
	}
}
