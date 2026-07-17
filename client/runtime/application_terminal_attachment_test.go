package runtime

import (
	"context"
	"errors"
	"reflect"
	"testing"

	coreapi "github.com/lozzow/termx/core/api"
)

func TestAttachmentStampRequiresOriginalSessionAndViewIdentity(t *testing.T) {
	valid := AttachmentStamp{
		EndpointSessionStamp: EndpointSessionStamp{EndpointID: "studio", RouteID: "ssh", Generation: 2},
		TerminalID:           "term-1", Channel: 7, SurfaceID: "surface-1", ViewID: "view-1", OperationID: "attach-1",
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.OperationID = ""
	if err := invalid.Validate(); CodeOf(err) != ErrorInvalidRequest {
		t.Fatalf("invalid stamp error=%v code=%q", err, CodeOf(err))
	}
}

func TestWasAttemptedDefaultsToNoReplayForUnknownErrors(t *testing.T) {
	if WasAttempted(nil) {
		t.Fatal("nil error must not be attempted")
	}
	if !WasAttempted(errors.New("adapter failed")) {
		t.Fatal("unknown error must conservatively prevent replay")
	}
	if WasAttempted(&Error{Code: ErrorStaleSession, Message: "stale", Attempted: false}) {
		t.Fatal("generation guard failure must remain unattempted")
	}
	if !WasAttempted(&Error{Code: ErrorUnavailable, Message: "write failed", Attempted: true}) {
		t.Fatal("adapter write failure must remain attempted")
	}
}

type contractTerminalAttachmentApplication struct{}

func (contractTerminalAttachmentApplication) AttachTerminal(context.Context, TerminalAttachRequest) (TerminalAttachResult, error) {
	return TerminalAttachResult{}, nil
}

func (contractTerminalAttachmentApplication) DetachTerminal(context.Context, TerminalDetachRequest) error {
	return nil
}

func (contractTerminalAttachmentApplication) SendTerminalInput(context.Context, TerminalInputRequest) error {
	return nil
}

func (contractTerminalAttachmentApplication) ResizeTerminal(context.Context, TerminalResizeRequest) (TerminalResizeResult, error) {
	return TerminalResizeResult{}, nil
}

var _ TerminalAttachmentApplication = contractTerminalAttachmentApplication{}

func TestAttachmentApplicationContractWrapsCoreProjection(t *testing.T) {
	assertFieldType(t, TerminalAttachRequest{}, "Spec", reflect.TypeOf(coreapi.TerminalAttachSpec{}))
	assertFieldType(t, TerminalAttachResult{}, "Attachment", reflect.TypeOf(coreapi.TerminalAttachResult{}))
	assertFieldType(t, TerminalResizeRequest{}, "Size", reflect.TypeOf(coreapi.TerminalSize{}))
	assertFieldType(t, TerminalResizeResult{}, "Resize", reflect.TypeOf(coreapi.TerminalResizeResult{}))
}
