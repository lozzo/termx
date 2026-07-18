package runtime

import (
	"context"
	"testing"

	"github.com/lozzow/termx/client/endpoint"
	"github.com/lozzow/termx/proto/apipb"
	"google.golang.org/protobuf/proto"
)

type recordingProtoExecutor struct {
	command *apipb.CommandEnvelope
	result  *apipb.ResultEnvelope
}

func (executor *recordingProtoExecutor) ExecuteApplication(_ context.Context, command *apipb.CommandEnvelope) (*apipb.ResultEnvelope, error) {
	executor.command = command
	if executor.result != nil {
		result := proto.Clone(executor.result).(*apipb.ResultEnvelope)
		if result.RequestId == "" {
			result.RequestId = command.GetContext().GetRequestId()
		}
		if result.OriginSession == nil {
			result.OriginSession = proto.Clone(command.GetContext().GetSession()).(*apipb.EndpointSessionStamp)
		}
		return result, nil
	}
	return &apipb.ResultEnvelope{RequestId: command.GetContext().GetRequestId(), OriginSession: proto.Clone(command.GetContext().GetSession()).(*apipb.EndpointSessionStamp), Result: &apipb.ResultEnvelope_Acknowledge{Acknowledge: &apipb.AcknowledgeResult{}}}, nil
}

func TestApplicationSessionOwnsContextAndOperationStamp(t *testing.T) {
	executor := &recordingProtoExecutor{}
	session, err := NewApplicationSession(EndpointSessionStamp{EndpointID: endpoint.EndpointID("studio"), RouteID: endpoint.RouteID("ssh"), Generation: 7}, executor)
	if err != nil {
		t.Fatal(err)
	}
	command := &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalAttach{TerminalAttach: &apipb.TerminalAttachCommand{Terminal: &apipb.TerminalRef{EndpointId: "studio", TerminalId: "term-1"}}}}
	if _, err := session.Execute(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	contextMessage := executor.command.GetContext()
	operation := executor.command.GetTerminalAttach().GetOperation()
	if contextMessage.GetSession().GetRouteId() != "ssh" || contextMessage.GetSession().GetGeneration() != 7 || contextMessage.GetRequestId() == "" {
		t.Fatalf("unexpected request context %#v", contextMessage)
	}
	if operation.GetOperationId() != contextMessage.GetRequestId() || operation.GetSession().GetGeneration() != 7 {
		t.Fatalf("unexpected operation stamp %#v", operation)
	}
	if command.GetContext() != nil || command.GetTerminalAttach().GetOperation() != nil {
		t.Fatal("application session mutated caller command")
	}
}

func TestApplicationSessionConvertsTypedAPIError(t *testing.T) {
	executor := &recordingProtoExecutor{result: &apipb.ResultEnvelope{Result: &apipb.ResultEnvelope_Error{Error: &apipb.ApiError{Code: apipb.ApiErrorCode_API_ERROR_CODE_STALE_SESSION, Message: "stale", Attempted: false}}}}
	session, err := NewApplicationSession(EndpointSessionStamp{EndpointID: "local", RouteID: "unix", Generation: 2}, executor)
	if err != nil {
		t.Fatal(err)
	}
	_, err = session.Execute(context.Background(), &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalList{TerminalList: &apipb.TerminalListCommand{}}})
	if CodeOf(err) != ErrorStaleSession || WasAttempted(err) {
		t.Fatalf("unexpected runtime error %#v", err)
	}
}

func TestApplicationSessionConvertsNotFoundAPIError(t *testing.T) {
	executor := &recordingProtoExecutor{result: &apipb.ResultEnvelope{Result: &apipb.ResultEnvelope_Error{Error: &apipb.ApiError{
		Code: apipb.ApiErrorCode_API_ERROR_CODE_NOT_FOUND, Message: "missing", Attempted: true,
	}}}}
	session, err := NewApplicationSession(EndpointSessionStamp{EndpointID: "local", RouteID: "unix", Generation: 2}, executor)
	if err != nil {
		t.Fatal(err)
	}
	_, err = session.Execute(context.Background(), &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalGet{
		TerminalGet: &apipb.TerminalGetCommand{Terminal: &apipb.TerminalRef{EndpointId: "local", TerminalId: "missing"}},
	}})
	if CodeOf(err) != ErrorNotFound || !WasAttempted(err) {
		t.Fatalf("unexpected runtime error %#v", err)
	}
}

func TestApplicationSessionRejectsMismatchedResultCorrelation(t *testing.T) {
	tests := []struct {
		name   string
		result *apipb.ResultEnvelope
		code   ErrorCode
	}{
		{name: "request", result: &apipb.ResultEnvelope{RequestId: "wrong", Result: &apipb.ResultEnvelope_Acknowledge{Acknowledge: &apipb.AcknowledgeResult{}}}, code: ErrorUnavailable},
		{name: "session", result: &apipb.ResultEnvelope{OriginSession: &apipb.EndpointSessionStamp{EndpointId: "local", RouteId: "unix", Generation: 8}, Result: &apipb.ResultEnvelope_Acknowledge{Acknowledge: &apipb.AcknowledgeResult{}}}, code: ErrorStaleSession},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &recordingProtoExecutor{result: test.result}
			session, err := NewApplicationSession(EndpointSessionStamp{EndpointID: "local", RouteID: "unix", Generation: 7}, executor)
			if err != nil {
				t.Fatal(err)
			}
			_, err = session.Execute(context.Background(), &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalList{TerminalList: &apipb.TerminalListCommand{}}})
			if CodeOf(err) != test.code {
				t.Fatalf("error=%v code=%s, want %s", err, CodeOf(err), test.code)
			}
		})
	}
}
