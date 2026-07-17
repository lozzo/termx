package apimapping

import (
	"math"
	"testing"
	"time"

	"github.com/lozzow/termx/proto/apipb"
	"google.golang.org/protobuf/proto"
)

func TestValidateTerminalInputRejectsMismatchedOperationSession(t *testing.T) {
	contextMessage := terminalRequestContext("request-1")
	command := &apipb.CommandEnvelope{Context: contextMessage, Command: &apipb.CommandEnvelope_TerminalInput{TerminalInput: &apipb.TerminalInputCommand{
		Attachment: &apipb.ResourceHandle{OpaqueToken: []byte("attachment-1"), Kind: apipb.ResourceKind_RESOURCE_KIND_TERMINAL_ATTACHMENT, Session: contextMessage.GetSession(), Generation: 1},
		Operation:  &apipb.OperationStamp{Session: &apipb.EndpointSessionStamp{EndpointId: "studio", RouteId: "ssh", Generation: 6}, OperationId: "input-1"},
		Data:       []byte("x"),
	}}}
	if err := ValidateTerminalCommand(command); err == nil {
		t.Fatal("mismatched operation session must fail")
	}
}

func TestValidateTerminalCreateRejectsNegativeAndOverflowingLimits(t *testing.T) {
	base := &apipb.TerminalCreateSpec{TerminalId: "term-1", Command: []string{"sh"}, Size: &apipb.TerminalSize{Cols: 80, Rows: 24}}
	negative := proto.Clone(base).(*apipb.TerminalCreateSpec)
	negative.ScrollbackRows = -1
	if err := ValidateTerminalCreateSpec(negative); err == nil {
		t.Fatal("negative scrollback rows must fail")
	}
	overflow := proto.Clone(base).(*apipb.TerminalCreateSpec)
	overflow.ScrollbackMaxAgeSeconds = math.MaxInt64/int64(time.Second) + 1
	if err := ValidateTerminalCreateSpec(overflow); err == nil {
		t.Fatal("overflowing scrollback duration must fail")
	}
}

func TestValidateTerminalAttachRejectsUnknownEnums(t *testing.T) {
	contextMessage := terminalRequestContext("request-enum")
	command := &apipb.CommandEnvelope{Context: contextMessage, Command: &apipb.CommandEnvelope_TerminalAttach{TerminalAttach: &apipb.TerminalAttachCommand{
		Terminal: &apipb.TerminalRef{EndpointId: "studio", TerminalId: "term-1"}, Mode: apipb.AttachmentMode(99),
		ResizePolicy: apipb.ResizePolicy_RESIZE_POLICY_OWNER, SurfaceId: "surface", ViewId: "view",
		Operation: &apipb.OperationStamp{Session: contextMessage.GetSession(), OperationId: "attach-1"},
	}}}
	if err := ValidateTerminalCommand(command); err == nil {
		t.Fatal("unknown attachment mode must fail")
	}
}

func TestValidateTerminalCommandRejectsNilTypedPayload(t *testing.T) {
	command := &apipb.CommandEnvelope{
		Context: terminalRequestContext("request-nil"),
		Command: &apipb.CommandEnvelope_TerminalDefaults{},
	}
	if err := ValidateTerminalCommand(command); err == nil {
		t.Fatal("nil typed payload must fail before API Layer cloning")
	}
}

func TestValidateTerminalCommandRejectsOversizedInputAndPathLimit(t *testing.T) {
	contextMessage := terminalRequestContext("request-limits")
	input := &apipb.CommandEnvelope{Context: contextMessage, Command: &apipb.CommandEnvelope_TerminalInput{TerminalInput: &apipb.TerminalInputCommand{
		Attachment: &apipb.ResourceHandle{OpaqueToken: []byte("attachment"), Kind: apipb.ResourceKind_RESOURCE_KIND_TERMINAL_ATTACHMENT, Session: contextMessage.GetSession(), Generation: 1},
		Operation:  &apipb.OperationStamp{Session: contextMessage.GetSession(), OperationId: "input"},
		Data:       make([]byte, maxTerminalInputBytes+1),
	}}}
	if err := ValidateTerminalCommand(input); err == nil {
		t.Fatal("oversized input must fail")
	}
	path := &apipb.CommandEnvelope{Context: contextMessage, Command: &apipb.CommandEnvelope_PathListDirectories{PathListDirectories: &apipb.PathListDirectoriesCommand{Limit: maxPathEntries + 1}}}
	if err := ValidateTerminalCommand(path); err == nil {
		t.Fatal("oversized path limit must fail")
	}
}

func TestValidateTerminalAttachResultRejectsCrossSessionResource(t *testing.T) {
	session := terminalRequestContext("request-result").GetSession()
	other := &apipb.EndpointSessionStamp{EndpointId: "studio", RouteId: "ssh", Generation: 6}
	command := &apipb.TerminalAttachCommand{
		Terminal: &apipb.TerminalRef{EndpointId: "studio", TerminalId: "term-1"},
		Mode:     apipb.AttachmentMode_ATTACHMENT_MODE_COLLABORATOR, ResizePolicy: apipb.ResizePolicy_RESIZE_POLICY_OWNER,
		SurfaceId: "surface", ViewId: "view", Operation: &apipb.OperationStamp{Session: session, OperationId: "attach"},
	}
	result := &apipb.TerminalAttachResult{
		Attachment: &apipb.AttachmentHandle{
			Resource:  &apipb.ResourceHandle{OpaqueToken: []byte("attachment"), Kind: apipb.ResourceKind_RESOURCE_KIND_TERMINAL_ATTACHMENT, Session: other, Generation: 1},
			Terminal:  &apipb.TerminalRef{EndpointId: "studio", TerminalId: "term-1"},
			Operation: &apipb.OperationStamp{Session: session, OperationId: "attach"},
		},
		Mode: apipb.AttachmentMode_ATTACHMENT_MODE_COLLABORATOR, ResizePolicy: apipb.ResizePolicy_RESIZE_POLICY_OWNER,
		Size: &apipb.TerminalSize{Cols: 80, Rows: 24},
	}
	if err := ValidateTerminalAttachResult(command, result, session); err == nil {
		t.Fatal("cross-session attachment result must fail")
	}
}

func terminalRequestContext(requestID string) *apipb.RequestContext {
	return &apipb.RequestContext{
		RequestId: requestID, ApiVersion: &apipb.ApiVersion{Major: 1},
		Session: &apipb.EndpointSessionStamp{EndpointId: "studio", RouteId: "ssh", Generation: 7},
	}
}
