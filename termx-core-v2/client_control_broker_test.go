package termxcorev2

import (
	"context"
	"strings"
	"testing"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-shared/plugin"
)

func TestClientControlBrokerQueuesUnicastAndAcceptsResponse(t *testing.T) {
	broker := newClientControlBroker(2)
	action := clientControlBrokerTestAction(plugin.DangerNone)
	if _, err := broker.register(1, clientControlBrokerRegisterParams("tui-1", plugin.ClientKindTUI, "ws-a", action)); err != nil {
		t.Fatalf("register: %v", err)
	}
	inbox, err := broker.watch(1, 11, protocol.ClientControlWatchParams{SessionID: "tui-1"})
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	trace := plugin.TraceParent{TraceID: "trace-1", Token: "token-1"}
	call := protocol.ClientControlCallParams{
		RequestID:   "req-1",
		ActionID:    action.ID,
		Params:      []byte(`{"panel":"active"}`),
		Target:      protocol.ClientControlTarget{SessionID: "tui-1", ActivePanel: true},
		TraceParent: trace,
	}
	result, err := broker.call(context.Background(), protocol.ClientControlSource{PluginID: "acme.runner", Kind: "one_shot"}, call)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if result.RequestID != "req-1" || result.Broadcast || len(result.Deliveries) != 1 || result.Deliveries[0].Status != protocol.ClientControlStatusQueued {
		t.Fatalf("unexpected call result %#v", result)
	}

	var invocation protocol.ClientControlInvocation
	select {
	case invocation = <-inbox:
	default:
		t.Fatal("expected queued invocation")
	}
	if invocation.Source.PluginID != "acme.runner" || invocation.Source.Kind != "one_shot" {
		t.Fatalf("source must be host-derived into invocation, got %#v", invocation.Source)
	}
	if invocation.Target.SessionID != "tui-1" || string(invocation.Params) != `{"panel":"active"}` || invocation.TraceParent != trace {
		t.Fatalf("unexpected invocation %#v", invocation)
	}

	ack, err := broker.respond(1, protocol.ClientControlResponseParams{
		RequestID:   invocation.RequestID,
		SessionID:   invocation.Target.SessionID,
		Status:      protocol.ClientControlStatusOK,
		Result:      []byte(`{"closed":true}`),
		TraceParent: invocation.TraceParent,
	})
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if !ack.Accepted || ack.RequestID != "req-1" {
		t.Fatalf("unexpected ack %#v", ack)
	}
	if _, err := broker.respond(1, protocol.ClientControlResponseParams{
		RequestID:   invocation.RequestID,
		SessionID:   invocation.Target.SessionID,
		Status:      protocol.ClientControlStatusOK,
		TraceParent: invocation.TraceParent,
	}); err == nil || !strings.Contains(err.Error(), "no pending") {
		t.Fatalf("second response should be rejected, got %v", err)
	}
}

func TestClientControlBrokerBroadcastFiltersAndRejectsDestructiveBroadcast(t *testing.T) {
	broker := newClientControlBroker(4)
	normal := clientControlBrokerTestAction(plugin.DangerNone)
	normal.BroadcastAllowed = true
	destructive := clientControlBrokerTestAction(plugin.DangerDestructive)
	destructive.ID = "acme.deploy.panel.close_and_kill"
	destructive.BroadcastAllowed = true
	if _, err := broker.register(1, clientControlBrokerRegisterParams("tui-1", plugin.ClientKindTUI, "ws-a", normal, destructive)); err != nil {
		t.Fatalf("register tui-1: %v", err)
	}
	if _, err := broker.register(2, clientControlBrokerRegisterParams("tui-2", plugin.ClientKindTUI, "ws-b", normal)); err != nil {
		t.Fatalf("register tui-2: %v", err)
	}
	if _, err := broker.register(3, clientControlBrokerRegisterParams("web-1", plugin.ClientKindWeb, "ws-a", normal, destructive)); err != nil {
		t.Fatalf("register web-1: %v", err)
	}

	trace := plugin.TraceParent{TraceID: "trace-b", Token: "token-b"}
	result, err := broker.call(context.Background(), protocol.ClientControlSource{PluginID: "acme.runner", Kind: "one_shot"}, protocol.ClientControlCallParams{
		RequestID:   "req-b",
		ActionID:    normal.ID,
		Target:      protocol.ClientControlTarget{Broadcast: true, ClientKind: plugin.ClientKindTUI},
		TraceParent: trace,
	})
	if err != nil {
		t.Fatalf("broadcast call: %v", err)
	}
	if !result.Broadcast || len(result.Deliveries) != 2 {
		t.Fatalf("expected broadcast to two tui sessions, got %#v", result)
	}

	rejected, err := broker.call(context.Background(), protocol.ClientControlSource{PluginID: "acme.runner", Kind: "one_shot"}, protocol.ClientControlCallParams{
		RequestID:   "req-danger",
		ActionID:    destructive.ID,
		Target:      protocol.ClientControlTarget{Broadcast: true, WorkspaceID: "ws-a"},
		TraceParent: plugin.TraceParent{TraceID: "trace-danger", Token: "token-danger"},
	})
	if err != nil {
		t.Fatalf("destructive broadcast call: %v", err)
	}
	if len(rejected.Deliveries) != 2 {
		t.Fatalf("expected ws-a deliveries, got %#v", rejected)
	}
	for _, delivery := range rejected.Deliveries {
		if delivery.Status != protocol.ClientControlStatusRejected || delivery.Error == nil || delivery.Error.Code != "destructive_broadcast_denied" {
			t.Fatalf("expected destructive broadcast rejection for every delivery, got %#v in %#v", delivery, rejected)
		}
	}
}

func TestClientControlBrokerRejectsDuplicateWatchAndReleasesOnUnwatch(t *testing.T) {
	broker := newClientControlBroker(2)
	action := clientControlBrokerTestAction(plugin.DangerNone)
	if _, err := broker.register(1, clientControlBrokerRegisterParams("tui-1", plugin.ClientKindTUI, "ws-a", action)); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := broker.watch(1, 11, protocol.ClientControlWatchParams{SessionID: "tui-1"}); err != nil {
		t.Fatalf("watch: %v", err)
	}
	if _, err := broker.watch(1, 12, protocol.ClientControlWatchParams{SessionID: "tui-1"}); err == nil || !strings.Contains(err.Error(), "active control watcher") {
		t.Fatalf("duplicate watch should fail, got %v", err)
	}
	if !broker.unwatch(1, "tui-1", 11) {
		t.Fatal("expected unwatch to release active watcher")
	}
	if _, err := broker.watch(1, 12, protocol.ClientControlWatchParams{SessionID: "tui-1"}); err != nil {
		t.Fatalf("rewatch after unwatch: %v", err)
	}
}

func TestClientControlBrokerResponseRequiresOwnerAndTrace(t *testing.T) {
	broker := newClientControlBroker(2)
	action := clientControlBrokerTestAction(plugin.DangerNone)
	if _, err := broker.register(1, clientControlBrokerRegisterParams("tui-1", plugin.ClientKindTUI, "ws-a", action)); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := broker.call(context.Background(), protocol.ClientControlSource{PluginID: "acme.runner", Kind: "one_shot"}, protocol.ClientControlCallParams{
		RequestID:   "req-1",
		ActionID:    action.ID,
		Target:      protocol.ClientControlTarget{SessionID: "tui-1"},
		TraceParent: plugin.TraceParent{TraceID: "trace-1", Token: "token-1"},
	}); err != nil {
		t.Fatalf("call: %v", err)
	}

	if _, err := broker.respond(2, protocol.ClientControlResponseParams{
		RequestID:   "req-1",
		SessionID:   "tui-1",
		Status:      protocol.ClientControlStatusOK,
		TraceParent: plugin.TraceParent{TraceID: "trace-1", Token: "token-1"},
	}); err == nil || !strings.Contains(err.Error(), "owned by another protocol session") {
		t.Fatalf("wrong owner response should be rejected, got %v", err)
	}
	if _, err := broker.respond(1, protocol.ClientControlResponseParams{
		RequestID:   "req-1",
		SessionID:   "tui-1",
		Status:      protocol.ClientControlStatusOK,
		TraceParent: plugin.TraceParent{TraceID: "trace-1", Token: "wrong"},
	}); err == nil || !strings.Contains(err.Error(), "trace parent does not match") {
		t.Fatalf("wrong trace response should be rejected, got %v", err)
	}
}

func TestClientControlBrokerRejectsDuplicatePendingRequest(t *testing.T) {
	broker := newClientControlBroker(4)
	action := clientControlBrokerTestAction(plugin.DangerNone)
	if _, err := broker.register(1, clientControlBrokerRegisterParams("tui-1", plugin.ClientKindTUI, "ws-a", action)); err != nil {
		t.Fatalf("register: %v", err)
	}
	call := protocol.ClientControlCallParams{
		RequestID:   "req-1",
		ActionID:    action.ID,
		Target:      protocol.ClientControlTarget{SessionID: "tui-1"},
		TraceParent: plugin.TraceParent{TraceID: "trace-1", Token: "token-1"},
	}
	if result, err := broker.call(context.Background(), protocol.ClientControlSource{PluginID: "acme.runner", Kind: "one_shot"}, call); err != nil || len(result.Deliveries) != 1 || result.Deliveries[0].Status != protocol.ClientControlStatusQueued {
		t.Fatalf("first call result=%#v err=%v", result, err)
	}
	duplicate, err := broker.call(context.Background(), protocol.ClientControlSource{PluginID: "acme.runner", Kind: "one_shot"}, call)
	if err != nil {
		t.Fatalf("duplicate call: %v", err)
	}
	if len(duplicate.Deliveries) != 1 || duplicate.Deliveries[0].Status != protocol.ClientControlStatusRejected || duplicate.Deliveries[0].Error == nil || duplicate.Deliveries[0].Error.Code != "duplicate_request" {
		t.Fatalf("expected duplicate request rejection, got %#v", duplicate)
	}
}

func clientControlBrokerTestAction(danger plugin.DangerLevel) protocol.ClientControlActionSpec {
	return protocol.ClientControlActionSpec{
		ID:                 "acme.deploy.panel.close",
		OwnerPluginID:      "acme.deploy",
		Scope:              plugin.ActionScopeClient,
		RequiredCaps:       []plugin.Capability{"client.panel.close"},
		ClientRequiredCaps: []plugin.Capability{"client.panel.close"},
		Danger:             danger,
	}
}

func clientControlBrokerRegisterParams(sessionID string, kind plugin.ClientKind, workspaceID string, actions ...protocol.ClientControlActionSpec) protocol.ClientSessionRegisterParams {
	return protocol.ClientSessionRegisterParams{
		SessionID:    sessionID,
		ClientKind:   kind,
		WorkspaceID:  workspaceID,
		InstanceID:   sessionID + "-instance",
		Capabilities: []plugin.Capability{"client.panel.close"},
		Actions:      actions,
	}
}
