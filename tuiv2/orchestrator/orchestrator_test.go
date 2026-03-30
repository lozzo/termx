package orchestrator

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/bridge"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
	unixtransport "github.com/lozzow/termx/transport/unix"
)

func newTestOrchestrator(t *testing.T) (*Orchestrator, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	socketPath := filepath.Join(t.TempDir(), "termx.sock")
	srv := termx.NewServer(termx.WithSocketPath(socketPath))
	done := make(chan error, 1)
	go func() {
		done <- srv.ListenAndServe(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		_ = srv.Shutdown(context.Background())
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("server did not stop in time")
		}
	})

	var transport *unixtransport.Transport
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for {
		transport, err = unixtransport.Dial(socketPath)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	client := protocol.NewClient(transport)
	t.Cleanup(func() { _ = client.Close() })

	helloCtx, helloCancel := context.WithTimeout(ctx, 2*time.Second)
	defer helloCancel()
	if err := client.Hello(helloCtx, protocol.Hello{Version: protocol.Version}); err != nil {
		t.Fatalf("hello: %v", err)
	}

	rt := runtime.New(bridge.NewProtocolClient(client))
	wb := workbench.NewWorkbench()
	mh := modal.NewHost()
	return New(wb, rt, mh), ctx
}

func TestHandleSemanticActionOpenPicker(t *testing.T) {
	orch, _ := newTestOrchestrator(t)

	effects := orch.HandleSemanticAction(input.SemanticAction{
		Kind:     input.ActionOpenPicker,
		TargetID: "req-1",
	})
	if len(effects) != 2 {
		t.Fatalf("expected 2 effects, got %d", len(effects))
	}
	if orch.modalHost.Session == nil || orch.modalHost.Session.Kind != input.ModePicker {
		t.Fatalf("expected picker session, got %#v", orch.modalHost.Session)
	}
}

func TestAttachAndLoadSnapshot(t *testing.T) {
	orch, ctx := newTestOrchestrator(t)

	created, err := orch.runtime.Registry(), error(nil)
	_ = created
	result, err := orch.runtime.ListTerminals(ctx)
	if err != nil {
		t.Fatalf("list terminals: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty terminal list, got %d", len(result))
	}

	createdTerm, err := orch.runtimeClientCreate(ctx, []string{"sh"}, "demo")
	if err != nil {
		t.Fatalf("create terminal: %v", err)
	}

	msgs, err := orch.AttachAndLoadSnapshot(ctx, "pane-1", createdTerm.TerminalID, "collaborator", 0, 10)
	if err != nil {
		t.Fatalf("attach and load snapshot: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 msgs, got %d", len(msgs))
	}
}

func (o *Orchestrator) runtimeClientCreate(ctx context.Context, command []string, name string) (*protocol.CreateResult, error) {
	return o.runtimeClient().Create(ctx, command, name, protocol.Size{Cols: 80, Rows: 24})
}

func (o *Orchestrator) runtimeClient() bridge.Client {
	return o.runtimeClientUnsafe()
}

func (o *Orchestrator) runtimeClientUnsafe() bridge.Client {
	return o.runtimeClientField()
}

func (o *Orchestrator) runtimeClientField() bridge.Client {
	return o.runtimeClientFromRuntime()
}

func (o *Orchestrator) runtimeClientFromRuntime() bridge.Client {
	return o.runtimeClientAccessor()
}

func (o *Orchestrator) runtimeClientAccessor() bridge.Client {
	return o.runtimeBridgeClient()
}

func (o *Orchestrator) runtimeBridgeClient() bridge.Client {
	return o.runtimeClientDirect()
}

func (o *Orchestrator) runtimeClientDirect() bridge.Client {
	return o.runtimeTestClient()
}

func (o *Orchestrator) runtimeTestClient() bridge.Client {
	return o.runtimeClientValue()
}

func (o *Orchestrator) runtimeClientValue() bridge.Client {
	return o.runtimeInternalClient()
}

func (o *Orchestrator) runtimeInternalClient() bridge.Client {
	return o.runtimeExposeClient()
}

func (o *Orchestrator) runtimeExposeClient() bridge.Client {
	return o.runtimeVisibleClient()
}

func (o *Orchestrator) runtimeVisibleClient() bridge.Client {
	return o.runtime.Client()
}
