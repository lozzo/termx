package apilayer

import (
	"context"
	"errors"
	"testing"

	"github.com/lozzow/termx/proto/apipb"
)

type fakeTerminalController struct {
	creates  []*apipb.TerminalCreateCommand
	inputs   []*apipb.TerminalInputCommand
	inputErr error
}

func (*fakeTerminalController) TerminalDefaults(context.Context, *apipb.TerminalDefaultsCommand) (*apipb.TerminalDefaultsResult, error) {
	return &apipb.TerminalDefaultsResult{Defaults: &apipb.TerminalDefaults{DefaultCommand: []string{"sh"}, DefaultCwd: "/tmp"}}, nil
}

func (controller *fakeTerminalController) TerminalCreate(_ context.Context, command *apipb.TerminalCreateCommand) (*apipb.TerminalCreateResult, error) {
	controller.creates = append(controller.creates, command)
	return &apipb.TerminalCreateResult{Terminal: &apipb.TerminalInfo{Ref: &apipb.TerminalRef{EndpointId: "studio", TerminalId: command.GetTerminal().GetTerminalId()}}}, nil
}

func (*fakeTerminalController) TerminalList(context.Context, *apipb.TerminalListCommand) (*apipb.TerminalListResult, error) {
	return &apipb.TerminalListResult{}, nil
}

func (*fakeTerminalController) TerminalGet(context.Context, *apipb.TerminalGetCommand) (*apipb.TerminalGetResult, error) {
	return &apipb.TerminalGetResult{}, nil
}

func (*fakeTerminalController) TerminalRestart(context.Context, *apipb.TerminalRestartCommand) error {
	return nil
}

func (*fakeTerminalController) TerminalKill(context.Context, *apipb.TerminalKillCommand) error {
	return nil
}

func (*fakeTerminalController) TerminalRemove(context.Context, *apipb.TerminalRemoveCommand) error {
	return nil
}

func (*fakeTerminalController) TerminalSetMetadata(context.Context, *apipb.TerminalSetMetadataCommand) error {
	return nil
}

func (*fakeTerminalController) TerminalSetTags(context.Context, *apipb.TerminalSetTagsCommand) error {
	return nil
}

func (*fakeTerminalController) TerminalAttach(context.Context, *apipb.TerminalAttachCommand) (*apipb.TerminalAttachResult, error) {
	return &apipb.TerminalAttachResult{}, nil
}

func (*fakeTerminalController) TerminalDetach(context.Context, *apipb.TerminalDetachCommand) error {
	return nil
}

func (controller *fakeTerminalController) TerminalInput(_ context.Context, command *apipb.TerminalInputCommand) error {
	controller.inputs = append(controller.inputs, command)
	return controller.inputErr
}

func (*fakeTerminalController) TerminalResize(context.Context, *apipb.TerminalResizeCommand) (*apipb.TerminalResizeResult, error) {
	return &apipb.TerminalResizeResult{}, nil
}

func (*fakeTerminalController) TerminalResizeLock(context.Context, *apipb.TerminalResizeLockCommand) (*apipb.TerminalResizeResult, error) {
	return &apipb.TerminalResizeResult{}, nil
}

func (*fakeTerminalController) PathListDirectories(context.Context, *apipb.PathListDirectoriesCommand) (*apipb.PathListDirectoriesResult, error) {
	return &apipb.PathListDirectoriesResult{}, nil
}

func TestServiceDispatchesValidatedTerminalCreateWithClonedProto(t *testing.T) {
	controller := &fakeTerminalController{}
	command := terminalCreateCommand()
	result := NewService(nil, nil, controller).Execute(context.Background(), command)
	if result.GetTerminalCreate().GetTerminal().GetRef().GetTerminalId() != "term-1" || len(controller.creates) != 1 {
		t.Fatalf("create result=%#v calls=%#v", result, controller.creates)
	}
	controller.creates[0].Terminal.Name = "controller-mutated"
	if command.GetTerminalCreate().GetTerminal().GetName() != "demo" {
		t.Fatal("terminal controller mutated client command")
	}
}

func TestServiceRejectsTerminalRefForDifferentEndpointBeforeController(t *testing.T) {
	controller := &fakeTerminalController{}
	command := &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalGet{TerminalGet: &apipb.TerminalGetCommand{
		Context:  terminalAPIContext("request-get", apipb.ApiCapability_API_CAPABILITY_TERMINAL_LIFECYCLE),
		Terminal: &apipb.TerminalRef{EndpointId: "other", TerminalId: "term-1"},
	}}}
	result := NewService(nil, nil, controller).Execute(context.Background(), command)
	if result.GetError().GetCode() != apipb.ApiErrorCode_API_ERROR_CODE_INVALID_REQUEST || result.GetError().GetAttempted() {
		t.Fatalf("endpoint mismatch result=%#v", result)
	}
}

func TestServiceMarksTerminalInputControllerFailureAttempted(t *testing.T) {
	controller := &fakeTerminalController{inputErr: errors.New("input write failed")}
	command := terminalInputCommand()
	result := NewService(nil, nil, controller).Execute(context.Background(), command)
	if result.GetError().GetCode() != apipb.ApiErrorCode_API_ERROR_CODE_INTERNAL || !result.GetError().GetAttempted() || len(controller.inputs) != 1 {
		t.Fatalf("input result=%#v calls=%d", result, len(controller.inputs))
	}
}

func terminalCreateCommand() *apipb.CommandEnvelope {
	return &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalCreate{TerminalCreate: &apipb.TerminalCreateCommand{
		Context:  terminalAPIContext("request-create", apipb.ApiCapability_API_CAPABILITY_TERMINAL_LIFECYCLE),
		Terminal: &apipb.TerminalCreateSpec{TerminalId: "term-1", Name: "demo", Command: []string{"sh"}, Size: &apipb.TerminalSize{Cols: 80, Rows: 24}},
	}}}
}

func terminalInputCommand() *apipb.CommandEnvelope {
	contextMessage := terminalAPIContext("request-input", apipb.ApiCapability_API_CAPABILITY_TERMINAL_ATTACHMENT)
	return &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalInput{TerminalInput: &apipb.TerminalInputCommand{
		Context:    contextMessage,
		Attachment: &apipb.ResourceHandle{Id: "attachment-1", Kind: "terminal_attachment", Generation: 2},
		Operation:  &apipb.OperationStamp{Session: contextMessage.GetSession(), OperationId: "input-1"},
		Data:       []byte("x"),
	}}}
}

func terminalAPIContext(requestID string, capability apipb.ApiCapability) *apipb.RequestContext {
	return &apipb.RequestContext{
		RequestId: requestID, ApiVersion: &apipb.ApiVersion{Major: 1}, Capabilities: []apipb.ApiCapability{capability},
		Session: &apipb.EndpointSessionStamp{EndpointId: "studio", RouteId: "ssh", Generation: 7},
	}
}
