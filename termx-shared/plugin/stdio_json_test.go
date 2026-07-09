package plugin

import (
	"encoding/json"
	"testing"
	"time"
)

func TestStdioJSONRequestValidationRequiresHostDerivedEnvelope(t *testing.T) {
	deadline := time.Now().Add(time.Minute).UnixNano()
	request := NormalizeStdioJSONRequest(StdioJSONRequest{
		RequestID:      "req-1",
		PluginID:       "acme.deploy",
		Host:           HostOneShot,
		Handler:        "close_and_kill",
		Kind:           StdioJSONInvocationAction,
		TraceParent:    TraceParent{TraceID: "trace-1", Token: "token-1"},
		DeadlineUnixNS: deadline,
		Action: &StdioJSONActionInvocation{
			ActionID: "acme.deploy.close_and_kill",
			Params:   json.RawMessage(`{"panel_id":"pane-1"}`),
			Target:   ActionTarget{SessionID: "tui-1", ActivePanel: true},
		},
		Context: StdioJSONContext{
			ClientKind:      ClientKindTUI,
			ClientSessionID: "tui-1",
			WorkspaceID:     "default",
			EndpointID:      "remote-a",
			TerminalRef:     &TerminalRef{EndpointID: "remote-a", TerminalID: "codex"},
			GrantRef:        "grant:acme",
		},
	})
	if err := ValidateStdioJSONRequest(request); err != nil {
		t.Fatalf("valid action request rejected: %v", err)
	}

	clone := request.Clone()
	request.Action.Params[0] = '{'
	request.Context.TerminalRef.TerminalID = "mutated"
	if string(clone.Action.Params) != `{"panel_id":"pane-1"}` {
		t.Fatalf("request clone should preserve params, got %s", clone.Action.Params)
	}
	if clone.Context.TerminalRef == nil || clone.Context.TerminalRef.TerminalID != "codex" {
		t.Fatalf("request clone should preserve terminal ref, got %#v", clone.Context.TerminalRef)
	}
}

func TestStdioJSONHookRequestRequiresTrace(t *testing.T) {
	deadline := time.Now().Add(time.Minute).UnixNano()
	request := NormalizeStdioJSONRequest(StdioJSONRequest{
		RequestID:      "req-hook",
		PluginID:       "acme.deploy",
		Host:           HostClient,
		Handler:        "on_panel",
		Kind:           StdioJSONInvocationHook,
		TraceParent:    TraceParent{TraceID: "trace-1", Token: "token-1"},
		DeadlineUnixNS: deadline,
		Hook: &HookEvent{
			EventID:    "event-1",
			Type:       SystemEventClientPanelCreated,
			SourceHost: HostClient,
			ObjectKind: ObjectKindPanel,
			ObjectID:   "pane-1",
			Time:       time.Now(),
			Trace:      MessageTrace{TraceID: "trace-1"},
			Payload:    json.RawMessage(`{"panel_id":"pane-1"}`),
		},
	})
	if err := ValidateStdioJSONRequest(request); err != nil {
		t.Fatalf("valid hook request rejected: %v", err)
	}

	request.Hook.Trace = MessageTrace{}
	if err := ValidateStdioJSONRequest(request); err == nil {
		t.Fatalf("hook request without trace should fail")
	}

	request.Hook.Trace = MessageTrace{TraceID: "trace-2"}
	if err := ValidateStdioJSONRequest(request); err == nil {
		t.Fatalf("hook request with mismatched trace parent should fail")
	}

	request.Hook.Trace = MessageTrace{TraceID: "trace-1"}
	request.Hook.Payload = json.RawMessage(`{"bad"`)
	if err := ValidateStdioJSONRequest(request); err == nil {
		t.Fatalf("hook request with invalid payload json should fail")
	}
}

func TestStdioJSONRequestRejectsMissingDeadlineAndInvalidJSON(t *testing.T) {
	request := NormalizeStdioJSONRequest(StdioJSONRequest{
		RequestID: "req-1",
		PluginID:  "acme.deploy",
		Host:      HostOneShot,
		Handler:   "run",
		Kind:      StdioJSONInvocationAction,
		TraceParent: TraceParent{
			TraceID: "trace-1",
			Token:   "token-1",
		},
		Action: &StdioJSONActionInvocation{
			ActionID: "acme.deploy.run",
			Params:   json.RawMessage(`{"bad"`),
		},
	})
	if err := ValidateStdioJSONRequest(request); err == nil {
		t.Fatalf("request without deadline should fail")
	}
	request.DeadlineUnixNS = time.Now().Add(time.Minute).UnixNano()
	if err := ValidateStdioJSONRequest(request); err == nil {
		t.Fatalf("request with invalid params json should fail")
	}

	request.Action.Params = json.RawMessage(`{}`)
	request.TraceParent = TraceParent{}
	if err := ValidateStdioJSONRequest(request); err == nil {
		t.Fatalf("request without trace parent should fail")
	}
}

func TestStdioJSONResponseValidationAndClone(t *testing.T) {
	deadline := time.Now().Add(time.Minute).UnixNano()
	response := StdioJSONResponse{
		Protocol:  StdioJSONProtocol,
		RequestID: "req-1",
		Status:    StdioJSONStatusOK,
		Result:    json.RawMessage(`{"ok":true}`),
		ActionCalls: []StdioJSONActionCall{
			{
				ActionID:       "termx.client.panel.close",
				Params:         json.RawMessage(`{"force":true}`),
				Target:         ActionTarget{TerminalRef: &TerminalRef{EndpointID: "remote-a", TerminalID: "codex"}},
				DeadlineUnixNS: deadline,
			},
		},
	}
	if err := ValidateStdioJSONResponse(response, "req-1", deadline); err != nil {
		t.Fatalf("valid response rejected: %v", err)
	}

	clone := response.Clone()
	response.Result[0] = '['
	response.ActionCalls[0].Params[0] = '['
	response.ActionCalls[0].Target.TerminalRef.TerminalID = "mutated"
	if string(clone.Result) != `{"ok":true}` {
		t.Fatalf("response clone should preserve result, got %s", clone.Result)
	}
	if string(clone.ActionCalls[0].Params) != `{"force":true}` {
		t.Fatalf("response clone should preserve action params, got %s", clone.ActionCalls[0].Params)
	}
	if clone.ActionCalls[0].Target.TerminalRef == nil || clone.ActionCalls[0].Target.TerminalRef.TerminalID != "codex" {
		t.Fatalf("response clone should preserve terminal ref, got %#v", clone.ActionCalls[0].Target.TerminalRef)
	}
}

func TestStdioJSONResponseRejectsForgedOrIncompleteActionCalls(t *testing.T) {
	response := StdioJSONResponse{
		Protocol:  StdioJSONProtocol,
		RequestID: "req-1",
		Status:    StdioJSONStatusOK,
		ActionCalls: []StdioJSONActionCall{
			{ActionID: "termx.client.panel.close"},
		},
	}
	if err := ValidateStdioJSONResponse(response, "req-1"); err == nil {
		t.Fatalf("action call without deadline should fail")
	}

	failed := StdioJSONResponse{Protocol: StdioJSONProtocol, RequestID: "req-1", Status: StdioJSONStatusFailed}
	if err := ValidateStdioJSONResponse(failed, "req-1"); err == nil {
		t.Fatalf("failed response without error should fail")
	}
	failed.Error = "failed"
	failed.ActionCalls = []StdioJSONActionCall{{ActionID: "termx.client.panel.close", DeadlineUnixNS: time.Now().Add(time.Second).UnixNano()}}
	if err := ValidateStdioJSONResponse(failed, "req-1"); err == nil {
		t.Fatalf("non-ok response with action calls should fail")
	}

	wrongRequest := StdioJSONResponse{Protocol: StdioJSONProtocol, RequestID: "old", Status: StdioJSONStatusOK}
	if err := ValidateStdioJSONResponse(wrongRequest, "req-1"); err == nil {
		t.Fatalf("response with mismatched request id should fail")
	}

	deadline := time.Now().Add(time.Second).UnixNano()
	futureDeadline := time.Now().Add(time.Hour).UnixNano()
	lateCall := StdioJSONResponse{
		Protocol:  StdioJSONProtocol,
		RequestID: "req-1",
		Status:    StdioJSONStatusOK,
		ActionCalls: []StdioJSONActionCall{
			{ActionID: "termx.client.panel.close", DeadlineUnixNS: futureDeadline},
		},
	}
	if err := ValidateStdioJSONResponse(lateCall, "req-1", deadline); err == nil {
		t.Fatalf("action call deadline beyond invocation deadline should fail")
	}
}
