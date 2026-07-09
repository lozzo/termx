package protocol

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-proto/wirepb"
	"github.com/lozzow/termx/termx-shared/plugin"
	"google.golang.org/protobuf/proto"
)

func TestClientSessionControlRegisterAndListCodecRoundTrip(t *testing.T) {
	action := ClientControlActionSpec{
		ID:                   "termx.client.panel.close_and_kill_terminal",
		OwnerPluginID:        "termx.builtin.tui",
		Scope:                plugin.ActionScopeClient,
		SupportedClientKinds: []plugin.ClientKind{plugin.ClientKindTUI, plugin.ClientKindGUI},
		RequiredCaps:         []plugin.Capability{"client.panel.close", "terminal.kill"},
		ClientRequiredCaps:   []plugin.Capability{"client.panel.close"},
		DaemonRequiredCaps:   []plugin.Capability{"terminal.kill"},
		Danger:               plugin.DangerDestructive,
		ParamsSchema:         `{"type":"object"}`,
		BroadcastAllowed:     true,
	}
	register := ClientSessionRegisterParams{
		SessionID:    "tui-1",
		ClientKind:   plugin.ClientKindTUI,
		WorkspaceID:  "workspace-main",
		InstanceID:   "pid-123",
		PID:          123,
		Capabilities: []plugin.Capability{"client.panel.close", "terminal.kill"},
		Actions:      []ClientControlActionSpec{action},
		Metadata:     map[string]string{"tty": "/dev/ttys001"},
	}
	payload, err := EncodeMethodParams(MethodClientSessionRegister, register)
	if err != nil {
		t.Fatalf("encode register: %v", err)
	}
	decoded, err := DecodeMethodParams(MethodClientSessionRegister, payload)
	if err != nil {
		t.Fatalf("decode register: %v", err)
	}
	if got := decoded.(ClientSessionRegisterParams); !reflect.DeepEqual(got, register) {
		t.Fatalf("register roundtrip mismatch:\n got: %#v\nwant: %#v", got, register)
	}

	connectedAt := time.Date(2026, 7, 9, 9, 0, 0, 0, time.UTC)
	session := ClientSessionInfo{
		SessionID:    register.SessionID,
		ClientKind:   register.ClientKind,
		WorkspaceID:  register.WorkspaceID,
		InstanceID:   register.InstanceID,
		PID:          register.PID,
		Capabilities: register.Capabilities,
		Actions:      register.Actions,
		ConnectedAt:  connectedAt,
		LastSeenAt:   connectedAt.Add(5 * time.Second),
		Metadata:     register.Metadata,
	}
	registerResultPayload, err := EncodeMethodResult(MethodClientSessionRegister, &ClientSessionRegisterResult{Session: session})
	if err != nil {
		t.Fatalf("encode register result: %v", err)
	}
	var registerResult ClientSessionRegisterResult
	if err := DecodeMethodResult(MethodClientSessionRegister, registerResultPayload, &registerResult); err != nil {
		t.Fatalf("decode register result: %v", err)
	}
	if !reflect.DeepEqual(registerResult.Session, session) {
		t.Fatalf("register result roundtrip mismatch:\n got: %#v\nwant: %#v", registerResult.Session, session)
	}

	webSession := session
	webSession.SessionID = "web-1"
	webSession.ClientKind = plugin.ClientKindWeb
	webSession.Actions = nil
	webSession.Metadata = map[string]string{"ua": "termx-web"}
	listPayload, err := EncodeMethodResult(MethodClientSessionList, ClientSessionListResult{Sessions: []ClientSessionInfo{session, webSession}})
	if err != nil {
		t.Fatalf("encode list result: %v", err)
	}
	var listResult ClientSessionListResult
	if err := DecodeMethodResult(MethodClientSessionList, listPayload, &listResult); err != nil {
		t.Fatalf("decode list result: %v", err)
	}
	if !reflect.DeepEqual(listResult.Sessions, []ClientSessionInfo{session, webSession}) {
		t.Fatalf("list result roundtrip mismatch:\n got: %#v\nwant: %#v", listResult.Sessions, []ClientSessionInfo{session, webSession})
	}

	paramsPayload, err := EncodeMethodParams(MethodClientSessionList, &ClientSessionListParams{ClientKind: plugin.ClientKindTUI, WorkspaceID: "workspace-main", IncludeActions: true})
	if err != nil {
		t.Fatalf("encode list params: %v", err)
	}
	paramsDecoded, err := DecodeMethodParams(MethodClientSessionList, paramsPayload)
	if err != nil {
		t.Fatalf("decode list params: %v", err)
	}
	gotParams := paramsDecoded.(ClientSessionListParams)
	if gotParams.ClientKind != plugin.ClientKindTUI || gotParams.WorkspaceID != "workspace-main" || !gotParams.IncludeActions {
		t.Fatalf("unexpected list params: %#v", gotParams)
	}

	watchPayload, err := EncodeMethodParams(MethodClientControlWatch, ClientControlWatchParams{SessionID: "tui-1"})
	if err != nil {
		t.Fatalf("encode watch params: %v", err)
	}
	watchDecoded, err := DecodeMethodParams(MethodClientControlWatch, watchPayload)
	if err != nil {
		t.Fatalf("decode watch params: %v", err)
	}
	if watchDecoded.(ClientControlWatchParams).SessionID != "tui-1" {
		t.Fatalf("unexpected watch params: %#v", watchDecoded)
	}
	watchResultPayload, err := EncodeMethodResult(MethodClientControlWatch, ClientControlWatchResult{SessionID: "tui-1", Channel: 17})
	if err != nil {
		t.Fatalf("encode watch result: %v", err)
	}
	var watchResult ClientControlWatchResult
	if err := DecodeMethodResult(MethodClientControlWatch, watchResultPayload, &watchResult); err != nil {
		t.Fatalf("decode watch result: %v", err)
	}
	if watchResult.SessionID != "tui-1" || watchResult.Channel != 17 {
		t.Fatalf("unexpected watch result: %#v", watchResult)
	}

	unwatchPayload, err := EncodeMethodParams(MethodClientControlUnwatch, ClientControlUnwatchParams{SessionID: "tui-1", Channel: 17})
	if err != nil {
		t.Fatalf("encode unwatch params: %v", err)
	}
	unwatchDecoded, err := DecodeMethodParams(MethodClientControlUnwatch, unwatchPayload)
	if err != nil {
		t.Fatalf("decode unwatch params: %v", err)
	}
	if unwatchDecoded.(ClientControlUnwatchParams).SessionID != "tui-1" || unwatchDecoded.(ClientControlUnwatchParams).Channel != 17 {
		t.Fatalf("unexpected unwatch params: %#v", unwatchDecoded)
	}
	unwatchResultPayload, err := EncodeMethodResult(MethodClientControlUnwatch, ClientControlUnwatchResult{SessionID: "tui-1", Channel: 17, Stopped: true})
	if err != nil {
		t.Fatalf("encode unwatch result: %v", err)
	}
	var unwatchResult ClientControlUnwatchResult
	if err := DecodeMethodResult(MethodClientControlUnwatch, unwatchResultPayload, &unwatchResult); err != nil {
		t.Fatalf("decode unwatch result: %v", err)
	}
	if unwatchResult.SessionID != "tui-1" || unwatchResult.Channel != 17 || !unwatchResult.Stopped {
		t.Fatalf("unexpected unwatch result: %#v", unwatchResult)
	}
	oversizeWatch, err := proto.Marshal(&wirepb.ClientControlWatchResult{SessionId: "tui-1", Channel: 70000})
	if err != nil {
		t.Fatalf("marshal oversized watch result: %v", err)
	}
	if err := DecodeMethodResult(MethodClientControlWatch, oversizeWatch, &watchResult); err == nil || !strings.Contains(err.Error(), "exceeds uint16") {
		t.Fatalf("oversized watch channel should fail, got %v", err)
	}
	oversizeUnwatch, err := proto.Marshal(&wirepb.ClientControlUnwatchParams{SessionId: "tui-1", Channel: 70000})
	if err != nil {
		t.Fatalf("marshal oversized unwatch params: %v", err)
	}
	if _, err := DecodeMethodParams(MethodClientControlUnwatch, oversizeUnwatch); err == nil || !strings.Contains(err.Error(), "exceeds uint16") {
		t.Fatalf("oversized unwatch channel should fail, got %v", err)
	}
}

func TestClientControlCallCodecPreservesClientOwnedSelectorsAndTrace(t *testing.T) {
	call := validClientControlCallForTest()
	call.ActionID = "termx.client.panel.close_and_kill_terminal"
	call.Params = []byte(`{"confirm":false}`)
	call.Target.ClientKind = plugin.ClientKindTUI
	call.Target.WorkspaceID = "workspace-main"
	call.Target.ActivePanel = true
	call.Deadline = time.Date(2026, 7, 9, 9, 1, 0, 0, time.UTC)
	call.IdempotencyKey = "close-kill-codex"

	if _, ok := reflect.TypeOf(call).FieldByName("Source"); ok {
		t.Fatalf("client.control.call request must not carry Source; host derives it into ClientControlInvocation")
	}
	payload, err := EncodeMethodParams(MethodClientControlCall, call)
	if err != nil {
		t.Fatalf("encode call: %v", err)
	}
	call.Params[0] = '['
	call.Target.TerminalRef.EndpointID = "mutated"

	decoded, err := DecodeMethodParams(MethodClientControlCall, payload)
	if err != nil {
		t.Fatalf("decode call: %v", err)
	}
	got := decoded.(ClientControlCallParams)
	if got.Target.SessionID != "tui-1" || !got.Target.ActivePanel {
		t.Fatalf("client-owned target selector was not preserved: %#v", got.Target)
	}
	if got.Target.TerminalRef == nil || got.Target.TerminalRef.EndpointID != "remote-a" || got.Target.TerminalRef.TerminalID != "codex" {
		t.Fatalf("terminal ref must preserve endpoint and terminal identity, got %#v", got.Target.TerminalRef)
	}
	if !bytes.Equal(got.Params, []byte(`{"confirm":false}`)) {
		t.Fatalf("params were not cloned through codec: %q", string(got.Params))
	}
	if got.TraceParent.TraceID != "trace-1" || got.TraceParent.Token != "opaque-token" {
		t.Fatalf("trace parent mismatch: %#v", got.TraceParent)
	}
}

func TestClientControlCallResultAndResponseCodecRoundTrip(t *testing.T) {
	callResult := ClientControlCallResult{
		RequestID: "req-1",
		Broadcast: true,
		Deliveries: []ClientControlDelivery{
			{SessionID: "tui-1", Status: ClientControlStatusQueued},
			{SessionID: "web-1", Status: ClientControlStatusNotFound, Error: &ClientControlError{Code: "session_not_found", Message: "missing"}},
		},
	}
	payload, err := EncodeMethodResult(MethodClientControlCall, callResult)
	if err != nil {
		t.Fatalf("encode call result: %v", err)
	}
	var gotCallResult ClientControlCallResult
	if err := DecodeMethodResult(MethodClientControlCall, payload, &gotCallResult); err != nil {
		t.Fatalf("decode call result: %v", err)
	}
	if !reflect.DeepEqual(gotCallResult, callResult) {
		t.Fatalf("call result roundtrip mismatch:\n got: %#v\nwant: %#v", gotCallResult, callResult)
	}

	response := validClientControlResponseForTest()
	response.Result = []byte(`{"terminal_ref":{"endpoint_id":"remote-a","terminal_id":"codex"}}`)
	responsePayload, err := EncodeMethodParams(MethodClientControlRespond, &response)
	if err != nil {
		t.Fatalf("encode response params: %v", err)
	}
	decodedResponse, err := DecodeMethodParams(MethodClientControlRespond, responsePayload)
	if err != nil {
		t.Fatalf("decode response params: %v", err)
	}
	if !reflect.DeepEqual(decodedResponse.(ClientControlResponseParams), response) {
		t.Fatalf("response params roundtrip mismatch:\n got: %#v\nwant: %#v", decodedResponse, response)
	}

	ackPayload, err := EncodeMethodResult(MethodClientControlRespond, ClientControlResponseResult{RequestID: "req-1", Accepted: true})
	if err != nil {
		t.Fatalf("encode response result: %v", err)
	}
	var ack ClientControlResponseResult
	if err := DecodeMethodResult(MethodClientControlRespond, ackPayload, &ack); err != nil {
		t.Fatalf("decode response result: %v", err)
	}
	if ack.RequestID != "req-1" || !ack.Accepted {
		t.Fatalf("unexpected ack: %#v", ack)
	}
}

func TestClientControlInvocationPayloadRoundTrip(t *testing.T) {
	invocation, err := DeriveClientControlInvocation(validClientControlCallForTest(), ClientControlSource{PluginID: "acme.deploy", Kind: "one_shot"})
	if err != nil {
		t.Fatalf("derive invocation: %v", err)
	}
	payload, err := EncodeClientControlInvocationPayload(invocation)
	if err != nil {
		t.Fatalf("encode invocation: %v", err)
	}
	invocation.Params[0] = '['
	invocation.Target.TerminalRef.EndpointID = "mutated"
	got, err := DecodeClientControlInvocationPayload(payload)
	if err != nil {
		t.Fatalf("decode invocation: %v", err)
	}
	if got.Source.PluginID != "acme.deploy" || got.Source.Kind != "one_shot" {
		t.Fatalf("source must come from derived invocation payload, got %#v", got.Source)
	}
	if got.Target.TerminalRef == nil || got.Target.TerminalRef.EndpointID != "remote-a" || got.Target.TerminalRef.TerminalID != "codex" {
		t.Fatalf("terminal ref must roundtrip endpoint+terminal only, got %#v", got.Target.TerminalRef)
	}
	if !bytes.Equal(got.Params, []byte(`{"ok":true}`)) {
		t.Fatalf("invocation params were not cloned through payload: %q", string(got.Params))
	}
}

func TestClientControlValidation(t *testing.T) {
	if err := ValidateClientSessionRegister(ClientSessionRegisterParams{ClientKind: plugin.ClientKindTUI}); err == nil || !strings.Contains(err.Error(), "session id") {
		t.Fatalf("missing session id should fail, got %v", err)
	}
	if err := ValidateClientSessionRegister(ClientSessionRegisterParams{SessionID: "tui-1", ClientKind: plugin.ClientKindTUI, Actions: []ClientControlActionSpec{{}}}); err == nil || !strings.Contains(err.Error(), "action id") {
		t.Fatalf("empty action id should fail, got %v", err)
	}
	if err := ValidateClientSessionRegister(ClientSessionRegisterParams{SessionID: "tui-1", ClientKind: plugin.ClientKindTUI, Actions: []ClientControlActionSpec{{ID: "termx.client.panel.close", OwnerPluginID: "acme.fake"}}}); err == nil || !strings.Contains(err.Error(), "termx namespace") {
		t.Fatalf("termx action without builtin owner should fail, got %v", err)
	}
	if err := ValidateClientSessionRegister(ClientSessionRegisterParams{SessionID: "tui-1", ClientKind: plugin.ClientKindTUI, Capabilities: []plugin.Capability{"client.panel.close"}, Actions: []ClientControlActionSpec{{ID: "acme.deploy.panel.close", OwnerPluginID: "acme.deploy", RequiredCaps: []plugin.Capability{"terminal.kill"}}}}); err == nil || !strings.Contains(err.Error(), "requires capabilities") {
		t.Fatalf("action requiring unregistered capability should fail, got %v", err)
	}
	if err := ValidateClientSessionRegister(ClientSessionRegisterParams{SessionID: "tui-1", ClientKind: plugin.ClientKindTUI, Actions: []ClientControlActionSpec{{ID: "acme.deploy.danger", OwnerPluginID: "acme.deploy", Danger: plugin.DangerDestructive}}}); err == nil || !strings.Contains(err.Error(), "explicit capability") {
		t.Fatalf("destructive action without caps should fail, got %v", err)
	}

	missingTarget := validClientControlCallForTest()
	missingTarget.Target = ClientControlTarget{}
	if err := ValidateClientControlCall(missingTarget); err == nil || !strings.Contains(err.Error(), "session id or broadcast") {
		t.Fatalf("missing target should fail, got %v", err)
	}
	missingTrace := validClientControlCallForTest()
	missingTrace.TraceParent = plugin.TraceParent{}
	if err := ValidateClientControlCall(missingTrace); err == nil || !strings.Contains(err.Error(), "trace parent") {
		t.Fatalf("missing trace should fail, got %v", err)
	}
	sessionAndBroadcast := validClientControlCallForTest()
	sessionAndBroadcast.Target.Broadcast = true
	if err := ValidateClientControlCall(sessionAndBroadcast); err == nil || !strings.Contains(err.Error(), "both session id and broadcast") {
		t.Fatalf("session+broadcast should fail, got %v", err)
	}
	partialRef := validClientControlCallForTest()
	partialRef.Target.TerminalRef = &ClientTerminalRef{TerminalID: "term-1"}
	if err := ValidateClientControlCall(partialRef); err == nil || !strings.Contains(err.Error(), "terminal ref") {
		t.Fatalf("partial terminal ref should fail, got %v", err)
	}
	unscopedBroadcast := validClientControlCallForTest()
	unscopedBroadcast.Target = ClientControlTarget{Broadcast: true}
	if err := ValidateClientControlCall(unscopedBroadcast); err == nil || !strings.Contains(err.Error(), "broadcast requires explicit") {
		t.Fatalf("unscoped broadcast should fail, got %v", err)
	}
	terminalRefOnlyBroadcast := validClientControlCallForTest()
	terminalRefOnlyBroadcast.Target = ClientControlTarget{Broadcast: true, TerminalRef: &ClientTerminalRef{EndpointID: "remote-a", TerminalID: "codex"}}
	if err := ValidateClientControlCall(terminalRefOnlyBroadcast); err == nil || !strings.Contains(err.Error(), "client kind or workspace") {
		t.Fatalf("terminal-ref-only broadcast should fail until broker owns terminal visibility, got %v", err)
	}
	scopedBroadcast := validClientControlCallForTest()
	scopedBroadcast.Target = ClientControlTarget{Broadcast: true, ClientKind: plugin.ClientKindTUI}
	if err := ValidateClientControlCall(scopedBroadcast); err != nil {
		t.Fatalf("scoped broadcast should pass: %v", err)
	}
	activePanel := validClientControlCallForTest()
	activePanel.Target = ClientControlTarget{SessionID: "tui-1", ActivePanel: true}
	if err := ValidateClientControlCall(activePanel); err != nil {
		t.Fatalf("valid active-panel selector should pass without broker interpretation: %v", err)
	}

	okWithError := validClientControlResponseForTest()
	okWithError.Error = &ClientControlError{Code: "unexpected"}
	if err := ValidateClientControlResponse(okWithError); err == nil || !strings.Contains(err.Error(), "cannot carry error") {
		t.Fatalf("ok response with error should fail, got %v", err)
	}
	errorWithoutBody := validClientControlResponseForTest()
	errorWithoutBody.Status = ClientControlStatusError
	if err := ValidateClientControlResponse(errorWithoutBody); err == nil || !strings.Contains(err.Error(), "requires error") {
		t.Fatalf("error response without error should fail, got %v", err)
	}
	wrongSession := validClientControlResponseForTest()
	wrongSession.SessionID = "web-1"
	context := ClientControlResponseValidationContext{RequestID: "req-1", SessionID: "tui-1", TraceParent: plugin.TraceParent{TraceID: "trace-1", Token: "next-token"}}
	if err := ValidateClientControlResponseFor(wrongSession, context); err == nil || !strings.Contains(err.Error(), "session id does not match") {
		t.Fatalf("wrong response session should fail, got %v", err)
	}
	wrongTrace := validClientControlResponseForTest()
	wrongTrace.TraceParent = plugin.TraceParent{TraceID: "trace-2", Token: "forged"}
	if err := ValidateClientControlResponseFor(wrongTrace, context); err == nil || !strings.Contains(err.Error(), "trace parent does not match") {
		t.Fatalf("wrong response trace should fail, got %v", err)
	}
}

func TestClientControlPolicyAndDerivedInvocation(t *testing.T) {
	call := validClientControlCallForTest()
	source := ClientControlSource{PluginID: "acme.deploy", Kind: "one_shot"}
	invocation, err := DeriveClientControlInvocation(call, source)
	if err != nil {
		t.Fatalf("derive invocation: %v", err)
	}
	call.Params = []byte("mutated")
	call.Target.TerminalRef.EndpointID = "mutated"
	if invocation.Source != source || invocation.Target.TerminalRef.EndpointID != "remote-a" || bytes.Equal(invocation.Params, call.Params) {
		t.Fatalf("derived invocation must carry host source and clone request data: %#v", invocation)
	}
	if _, err := DeriveClientControlInvocation(validClientControlCallForTest(), ClientControlSource{PluginID: "acme.deploy"}); err == nil || !strings.Contains(err.Error(), "source kind") {
		t.Fatalf("missing host-derived source kind should fail, got %v", err)
	}
	if err := ValidateClientControlCallWithPolicy(validClientControlCallForTest(), ClientControlCallValidationPolicy{
		TraceValidator: func(parent plugin.TraceParent) error {
			if parent.Token != "opaque-token" {
				return errInvalidTraceForTest
			}
			return nil
		},
		ActionSpec: &ClientControlActionSpec{ID: "termx.client.panel.close", OwnerPluginID: "termx.builtin.tui", Danger: plugin.DangerNone},
	}); err != nil {
		t.Fatalf("valid trace and action policy should pass: %v", err)
	}
	if err := ValidateClientControlCallWithPolicy(validClientControlCallForTest(), ClientControlCallValidationPolicy{TraceValidator: func(plugin.TraceParent) error { return errInvalidTraceForTest }}); err == nil || !strings.Contains(err.Error(), "trace parent rejected") {
		t.Fatalf("forged trace should fail, got %v", err)
	}
	destructiveBroadcast := validClientControlCallForTest()
	destructiveBroadcast.Target = ClientControlTarget{Broadcast: true, ClientKind: plugin.ClientKindTUI}
	if err := ValidateClientControlCallWithPolicy(destructiveBroadcast, ClientControlCallValidationPolicy{ActionSpec: &ClientControlActionSpec{ID: destructiveBroadcast.ActionID, OwnerPluginID: "termx.builtin.tui", Danger: plugin.DangerDestructive, BroadcastAllowed: true}}); err == nil || !strings.Contains(err.Error(), "destructive broadcast") {
		t.Fatalf("destructive broadcast without explicit allow should fail, got %v", err)
	}
	if err := ValidateClientControlCallWithPolicy(destructiveBroadcast, ClientControlCallValidationPolicy{ActionSpec: &ClientControlActionSpec{ID: destructiveBroadcast.ActionID, OwnerPluginID: "termx.builtin.tui", Danger: plugin.DangerDestructive, BroadcastAllowed: true}, AllowDestructiveBroadcast: true}); err != nil {
		t.Fatalf("destructive broadcast with explicit allow should pass: %v", err)
	}
	ordinaryBroadcast := validClientControlCallForTest()
	ordinaryBroadcast.Target = ClientControlTarget{Broadcast: true, ClientKind: plugin.ClientKindTUI}
	if err := ValidateClientControlCallWithPolicy(ordinaryBroadcast, ClientControlCallValidationPolicy{ActionSpec: &ClientControlActionSpec{ID: ordinaryBroadcast.ActionID, OwnerPluginID: "termx.builtin.tui"}}); err == nil || !strings.Contains(err.Error(), "broadcast requires action policy") {
		t.Fatalf("broadcast without action policy should fail, got %v", err)
	}
}

func TestClientControlMethodCodecTypeErrors(t *testing.T) {
	for method, wrong := range map[string]any{
		MethodClientSessionRegister: ClientControlCallParams{},
		MethodClientSessionList:     ClientControlCallParams{},
		MethodClientControlCall:     ClientSessionRegisterParams{},
		MethodClientControlWatch:    ClientControlCallParams{},
		MethodClientControlUnwatch:  ClientControlCallParams{},
		MethodClientControlRespond:  ClientControlCallParams{},
	} {
		if _, err := EncodeMethodParams(method, wrong); err == nil {
			t.Fatalf("%s wrong params type should fail", method)
		}
	}
	for method, result := range map[string]any{
		MethodClientSessionRegister: ClientSessionRegisterResult{},
		MethodClientSessionList:     ClientSessionListResult{},
		MethodClientControlCall:     ClientControlCallResult{},
		MethodClientControlWatch:    ClientControlWatchResult{},
		MethodClientControlUnwatch:  ClientControlUnwatchResult{},
		MethodClientControlRespond:  ClientControlResponseResult{},
	} {
		if _, err := EncodeMethodResult(method, ClientControlResponseParams{}); err == nil {
			t.Fatalf("%s wrong result type should fail", method)
		}
		payload, err := EncodeMethodResult(method, result)
		if err != nil {
			t.Fatalf("encode %s result: %v", method, err)
		}
		var wrong ClientSessionListParams
		if err := DecodeMethodResult(method, payload, &wrong); err == nil {
			t.Fatalf("%s wrong decode target should fail", method)
		}
	}
}

var errInvalidTraceForTest = errors.New("invalid trace")

func validClientControlCallForTest() ClientControlCallParams {
	return ClientControlCallParams{
		RequestID: "req-1",
		ActionID:  "termx.client.panel.close",
		Params:    []byte(`{"ok":true}`),
		Target: ClientControlTarget{
			SessionID: "tui-1",
			TerminalRef: &ClientTerminalRef{
				EndpointID: "remote-a",
				TerminalID: "codex",
			},
		},
		TraceParent: plugin.TraceParent{TraceID: "trace-1", Token: "opaque-token"},
	}
}

func validClientControlResponseForTest() ClientControlResponseParams {
	return ClientControlResponseParams{
		RequestID:   "req-1",
		SessionID:   "tui-1",
		Status:      ClientControlStatusOK,
		TraceParent: plugin.TraceParent{TraceID: "trace-1", Token: "next-token"},
	}
}
