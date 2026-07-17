package transformer

import (
	"reflect"
	"testing"
	"time"

	corev2 "github.com/lozzow/termx/core"
	"github.com/lozzow/termx/proto/apipb"
)

func TestTerminalRecordFromProtoCopiesCreateSpecIntoCoreDomain(t *testing.T) {
	spec := &apipb.TerminalCreateSpec{
		TerminalId: "term-1", Name: "demo", Command: []string{"sh", "-l"}, Tags: map[string]string{"role": "dev"},
		Size: &apipb.TerminalSize{Cols: 100, Rows: 30}, Cwd: "/srv/app", Env: []string{"A=1"},
		ScrollbackRows: 2000, ScrollbackMaxBytes: 4096, ScrollbackMaxAgeSeconds: 120,
	}
	record, err := TerminalRecordFromProto(spec)
	if err != nil {
		t.Fatal(err)
	}
	if record.ID != "term-1" || record.Size != (corev2.Size{Cols: 100, Rows: 30}) || record.Options.Dir != "/srv/app" || record.Options.ScrollbackMaxAge != 2*time.Minute {
		t.Fatalf("record=%#v", record)
	}
	spec.Command[0] = "mutated"
	spec.Tags["role"] = "mutated"
	if record.Command[0] != "sh" || record.Tags["role"] != "dev" {
		t.Fatalf("record aliases proto input: %#v", record)
	}
}

func TestTerminalInfoToProtoAddsEndpointWithoutMutatingCore(t *testing.T) {
	exitCode := 23
	now := time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	info := corev2.TerminalInfo{
		ID: "term-1", Name: "demo", Command: []string{"sh"}, Tags: map[string]string{"role": "dev"},
		Size: corev2.Size{Cols: 80, Rows: 24}, State: corev2.TerminalStateExited,
		CreatedAt: now.Add(-time.Minute), ExitedAt: now, ExitCode: &exitCode,
	}
	projection, err := TerminalInfoToProto("studio", info)
	if err != nil {
		t.Fatal(err)
	}
	if projection.GetRef().GetEndpointId() != "studio" || projection.GetRef().GetTerminalId() != "term-1" || projection.GetState() != apipb.TerminalState_TERMINAL_STATE_EXITED || projection.GetExitCode() != 23 {
		t.Fatalf("projection=%#v", projection)
	}
	projection.Command[0] = "mutated"
	projection.Tags["role"] = "mutated"
	if !reflect.DeepEqual(info.Command, []string{"sh"}) || info.Tags["role"] != "dev" {
		t.Fatalf("projection aliases core domain: %#v", info)
	}
}

func TestValidateTerminalInputRejectsMismatchedOperationSession(t *testing.T) {
	contextMessage := terminalRequestContext("request-1")
	command := &apipb.CommandEnvelope{Command: &apipb.CommandEnvelope_TerminalInput{TerminalInput: &apipb.TerminalInputCommand{
		Context:    contextMessage,
		Attachment: &apipb.ResourceHandle{Id: "attachment-1", Kind: "terminal_attachment", Generation: 1},
		Operation:  &apipb.OperationStamp{Session: &apipb.EndpointSessionStamp{EndpointId: "studio", RouteId: "ssh", Generation: 6}, OperationId: "input-1"},
		Data:       []byte("x"),
	}}}
	if err := ValidateTerminalCommand(command); err == nil {
		t.Fatal("mismatched operation session must fail")
	}
}

func terminalRequestContext(requestID string) *apipb.RequestContext {
	return &apipb.RequestContext{
		RequestId: requestID, ApiVersion: &apipb.ApiVersion{Major: 1},
		Capabilities: []apipb.ApiCapability{apipb.ApiCapability_API_CAPABILITY_TERMINAL_ATTACHMENT},
		Session:      &apipb.EndpointSessionStamp{EndpointId: "studio", RouteId: "ssh", Generation: 7},
	}
}
