package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/transport/memory"
	"github.com/lozzow/termx/tui/app"
	stateterminal "github.com/lozzow/termx/tui/state/terminal"
	"github.com/lozzow/termx/tui/state/types"
	"github.com/lozzow/termx/tui/state/workspace"
)

func TestTerminalServiceDelegatesCreateAndAttach(t *testing.T) {
	client := &stubClient{}
	service := NewTerminalService(client)

	created, err := service.Create(context.Background(), []string{"/bin/sh"}, "shell", protocol.Size{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if created.TerminalID != "term-1" {
		t.Fatalf("expected term-1, got %#v", created)
	}

	attached, err := service.Attach(context.Background(), "term-1", "rw")
	if err != nil {
		t.Fatalf("Attach returned error: %v", err)
	}
	if attached.Channel != 7 {
		t.Fatalf("expected channel 7, got %#v", attached)
	}
}

func TestTerminalServiceCreateUsesProtocolCreateContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runCreateContractServer(serverTransport)
	}()

	client := protocol.NewClient(clientTransport)
	defer client.Close()

	service := NewTerminalService(protocolRuntimeClient{inner: client})
	created, err := service.Create(ctx, []string{"/bin/sh"}, "shell-2", protocol.Size{Cols: 90, Rows: 30})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if created.TerminalID != "term-2" {
		t.Fatalf("expected term-2, got %#v", created)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server failed: %v", err)
	}
}

func TestTerminalServiceKillUsesProtocolKillContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runKillContractServer(serverTransport)
	}()

	client := protocol.NewClient(clientTransport)
	defer client.Close()

	service := NewTerminalService(protocolRuntimeClient{inner: client})
	if err := service.Kill(ctx, "term-9"); err != nil {
		t.Fatalf("Kill returned error: %v", err)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server failed: %v", err)
	}
}

func TestExecuteWorkbenchActionDelegatesCreateAndKill(t *testing.T) {
	client := &stubClient{}
	service := NewTerminalService(client)

	createResult, err := ExecuteWorkbenchAction(context.Background(), service, PendingWorkbenchAction{
		Kind:    PendingWorkbenchActionCreateTerminal,
		Command: []string{"/bin/sh"},
		Name:    "shell-2",
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("create action returned error: %v", err)
	}
	if createResult.TerminalID != "term-1" {
		t.Fatalf("expected created terminal id, got %#v", createResult)
	}

	if _, err := ExecuteWorkbenchAction(context.Background(), service, PendingWorkbenchAction{
		Kind:       PendingWorkbenchActionKillTerminal,
		TerminalID: "term-1",
	}); err != nil {
		t.Fatalf("kill action returned error: %v", err)
	}
}

func TestApplyIntentExecutesCreateEffectFromAppReducerAndBindsTerminal(t *testing.T) {
	service := NewTerminalService(&stubClient{
		createResult: &protocol.CreateResult{TerminalID: "term-created", State: "running"},
		attachResult: &protocol.AttachResult{Channel: 11, Mode: "rw"},
		snapshot:     &protocol.Snapshot{TerminalID: "term-created", Size: protocol.Size{Cols: 80, Rows: 24}},
	})
	model := splitUnconnectedPaneModelForRuntimeTest()

	next, err := ApplyIntent(context.Background(), model, service, app.ConfirmCreateTerminalIntent{
		Command: []string{"/bin/sh"},
		Name:    "shell-2",
	})
	if err != nil {
		t.Fatalf("ApplyIntent returned error: %v", err)
	}
	pane, _ := next.Workspace.ActiveTab().ActivePane()
	if pane.TerminalID != types.TerminalID("term-created") || pane.SlotState != types.PaneSlotLive {
		t.Fatalf("expected runtime create to bind live terminal, got %+v", pane)
	}
	if next.Sessions[types.TerminalID("term-created")].Channel != 11 {
		t.Fatalf("expected attached session after runtime create, got %#v", next.Sessions[types.TerminalID("term-created")])
	}
}

func TestApplyIntentExecutesKillEffectFromAppReducerAndMarksExited(t *testing.T) {
	service := NewTerminalService(&stubClient{})
	model := livePaneModelForRuntimeTest()

	next, err := ApplyIntent(context.Background(), model, service, app.IntentClosePaneAndKillTerminal)
	if err != nil {
		t.Fatalf("ApplyIntent returned error: %v", err)
	}
	if next.Terminals[types.TerminalID("term-1")].State != stateterminal.StateExited {
		t.Fatalf("expected runtime kill to mark exited, got %#v", next.Terminals[types.TerminalID("term-1")])
	}
	pane, _ := next.Workspace.ActiveTab().ActivePane()
	if pane.SlotState != types.PaneSlotExited {
		t.Fatalf("expected exited pane after runtime kill, got %+v", pane)
	}
}

func TestApplyIntentKillsCreatedTerminalWhenAttachFails(t *testing.T) {
	service := NewTerminalService(&stubClient{
		createResult: &protocol.CreateResult{TerminalID: "term-created", State: "running"},
		attachErr:    errors.New("attach failed"),
	})
	model := splitUnconnectedPaneModelForRuntimeTest()

	next, err := ApplyIntent(context.Background(), model, service, app.ConfirmCreateTerminalIntent{
		Command: []string{"/bin/sh"},
		Name:    "shell-2",
	})
	if err == nil {
		t.Fatal("expected attach failure to surface")
	}
	if service.client.(*stubClient).lastKilledID != "term-created" {
		t.Fatalf("expected cleanup kill for created terminal, got %q", service.client.(*stubClient).lastKilledID)
	}
	pane, _ := next.Workspace.ActiveTab().ActivePane()
	if pane.TerminalID != "" || pane.SlotState != types.PaneSlotUnconnected {
		t.Fatalf("expected pane to remain unbound, got %+v", pane)
	}
}

func splitUnconnectedPaneModelForRuntimeTest() app.Model {
	model := livePaneModelForRuntimeTest()
	model = model.Apply(app.IntentSplitVertical)
	return model
}

func livePaneModelForRuntimeTest() app.Model {
	model := app.NewModel()
	ws := workspace.NewTemporary("main")
	tab := ws.ActiveTab()
	pane, _ := tab.ActivePane()
	pane.SlotState = types.PaneSlotLive
	pane.TerminalID = types.TerminalID("term-1")
	tab.TrackPane(pane)
	model.Workspace = ws
	model.Terminals[types.TerminalID("term-1")] = stateterminal.Metadata{
		ID:              types.TerminalID("term-1"),
		Name:            "api-dev",
		Command:         []string{"/bin/sh"},
		State:           stateterminal.StateRunning,
		OwnerPaneID:     pane.ID,
		AttachedPaneIDs: []types.PaneID{pane.ID},
	}
	model.Sessions[types.TerminalID("term-1")] = app.TerminalSession{
		TerminalID: types.TerminalID("term-1"),
		Channel:    7,
		Attached:   true,
	}
	return model
}

type protocolRuntimeClient struct {
	inner *protocol.Client
}

func (c protocolRuntimeClient) Close() error { return c.inner.Close() }

func (c protocolRuntimeClient) Create(ctx context.Context, command []string, name string, size protocol.Size) (*protocol.CreateResult, error) {
	return c.inner.Create(ctx, protocol.CreateParams{
		Command: command,
		Name:    name,
		Size:    size,
	})
}

func (c protocolRuntimeClient) SetTags(ctx context.Context, terminalID string, tags map[string]string) error {
	return c.inner.SetTags(ctx, terminalID, tags)
}

func (c protocolRuntimeClient) SetMetadata(ctx context.Context, terminalID string, name string, tags map[string]string) error {
	return c.inner.SetMetadata(ctx, terminalID, name, tags)
}

func (c protocolRuntimeClient) List(ctx context.Context) (*protocol.ListResult, error) {
	return c.inner.List(ctx)
}

func (c protocolRuntimeClient) Events(ctx context.Context, params protocol.EventsParams) (<-chan protocol.Event, error) {
	return c.inner.Events(ctx, params)
}

func (c protocolRuntimeClient) Attach(ctx context.Context, terminalID string, mode string) (*protocol.AttachResult, error) {
	return c.inner.Attach(ctx, terminalID, mode)
}

func (c protocolRuntimeClient) Snapshot(ctx context.Context, terminalID string, offset, limit int) (*protocol.Snapshot, error) {
	return c.inner.Snapshot(ctx, terminalID, offset, limit)
}

func (c protocolRuntimeClient) Input(ctx context.Context, channel uint16, data []byte) error {
	return c.inner.Input(ctx, channel, data)
}

func (c protocolRuntimeClient) Resize(ctx context.Context, channel uint16, cols, rows uint16) error {
	return c.inner.Resize(ctx, channel, cols, rows)
}

func (c protocolRuntimeClient) Stream(channel uint16) (<-chan protocol.StreamFrame, func()) {
	return c.inner.Stream(channel)
}

func (c protocolRuntimeClient) Kill(ctx context.Context, terminalID string) error {
	return c.inner.Kill(ctx, terminalID)
}

func runCreateContractServer(tr *memory.Transport) error {
	req, err := readRequest(tr)
	if err != nil {
		return err
	}
	if req.Method != "create" {
		return nil
	}

	var params protocol.CreateParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return err
	}
	if len(params.Command) != 1 || params.Command[0] != "/bin/sh" || params.Name != "shell-2" {
		return tcontractError("unexpected create params")
	}
	return writeResponse(tr, req.ID, protocol.CreateResult{TerminalID: "term-2", State: "running"})
}

func runKillContractServer(tr *memory.Transport) error {
	req, err := readRequest(tr)
	if err != nil {
		return err
	}
	if req.Method != "kill" {
		return tcontractError("unexpected method")
	}
	var params protocol.GetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return err
	}
	if params.TerminalID != "term-9" {
		return tcontractError("unexpected terminal id")
	}
	return writeResponse(tr, req.ID, map[string]any{})
}

func readRequest(tr *memory.Transport) (protocol.Request, error) {
	frame, err := tr.Recv()
	if err != nil {
		return protocol.Request{}, err
	}
	channel, kind, payload, err := protocol.DecodeFrame(frame)
	if err != nil {
		return protocol.Request{}, err
	}
	if channel != 0 || kind != protocol.TypeRequest {
		return protocol.Request{}, tcontractError("unexpected frame")
	}
	var req protocol.Request
	return req, json.Unmarshal(payload, &req)
}

func writeResponse(tr *memory.Transport, id uint64, result any) error {
	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}
	framePayload, err := json.Marshal(protocol.Response{ID: id, Result: payload})
	if err != nil {
		return err
	}
	frame, err := protocol.EncodeFrame(0, protocol.TypeResponse, framePayload)
	if err != nil {
		return err
	}
	return tr.Send(frame)
}

type tcontractError string

func (e tcontractError) Error() string { return string(e) }
