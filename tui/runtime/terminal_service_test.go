package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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

func TestTerminalPoolActionsReachRuntimeService(t *testing.T) {
	service := NewTerminalService(&stubClient{
		attachResult: &protocol.AttachResult{Channel: 19, Mode: "observer"},
		snapshot:     &protocol.Snapshot{TerminalID: "term-2", Size: protocol.Size{Cols: 80, Rows: 24}},
	})
	model := terminalPoolModelForRuntimeTest()

	next, err := ApplyIntent(context.Background(), model, service, app.SelectTerminalPoolIntent{TerminalID: types.TerminalID("term-2")})
	if err != nil {
		t.Fatalf("ApplyIntent returned error: %v", err)
	}
	client := service.client.(*stubClient)
	if client.lastAttachID != "term-2" || client.lastAttachMode != "observer" {
		t.Fatalf("expected readonly preview attach, got id=%q mode=%q", client.lastAttachID, client.lastAttachMode)
	}
	if next.Pool.PreviewTerminalID != types.TerminalID("term-2") {
		t.Fatalf("expected preview terminal to switch, got %q", next.Pool.PreviewTerminalID)
	}
	session := next.Sessions[types.TerminalID("term-2")]
	if !session.ReadOnly || !session.Preview {
		t.Fatalf("expected readonly preview session, got %#v", session)
	}
}

func TestTerminalPoolPreviewConsumesStreamFramesContinuously(t *testing.T) {
	client := &stubClient{
		attachResult: &protocol.AttachResult{Channel: 19, Mode: "observer"},
		snapshot: &protocol.Snapshot{
			TerminalID: "term-2",
			Screen: protocol.ScreenData{
				Cells: [][]protocol.Cell{{{Content: "s", Width: 1}}},
			},
		},
		streams: map[uint16]chan protocol.StreamFrame{
			19: make(chan protocol.StreamFrame, 4),
		},
	}
	model := terminalPoolModelForRuntimeTest()
	model.IntentExecutor = NewModelIntentExecutor(NewTerminalService(client))

	teaModel, cmd := model.Update(app.IntentMessage{Intent: app.SelectTerminalPoolIntent{TerminalID: types.TerminalID("term-2")}})
	if cmd == nil {
		t.Fatal("expected preview subscription cmd")
	}
	stream := client.streams[19]
	stream <- protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("one")}
	msg := cmd()

	teaModel, nextCmd := teaModel.Update(msg)
	updated := teaModel.(app.Model)
	if got := flattenedPreviewText(updated, types.TerminalID("term-2")); !strings.Contains(got, "one") {
		t.Fatalf("expected first stream frame to update preview, got %q", got)
	}
	if nextCmd == nil {
		t.Fatal("expected next preview stream cmd")
	}

	stream <- protocol.StreamFrame{Type: protocol.TypeOutput, Payload: []byte("two")}
	msg = nextCmd()
	teaModel, _ = teaModel.Update(msg)
	updated = teaModel.(app.Model)
	if got := flattenedPreviewText(updated, types.TerminalID("term-2")); !strings.Contains(got, "two") {
		t.Fatalf("expected continuous preview refresh, got %q", got)
	}
}

func TestTerminalPoolOpenActionsAttachRuntimeSession(t *testing.T) {
	tests := []struct {
		name   string
		intent app.Intent
		check  func(t *testing.T, model app.Model)
	}{
		{
			name:   "open here",
			intent: app.OpenSelectedTerminalHereIntent{},
			check: func(t *testing.T, model app.Model) {
				pane, _ := model.Workspace.ActiveTab().ActivePane()
				if pane.TerminalID != types.TerminalID("term-1") {
					t.Fatalf("expected active pane to bind term-1, got %+v", pane)
				}
			},
		},
		{
			name:   "open new tab",
			intent: app.OpenSelectedTerminalInNewTabIntent{},
			check: func(t *testing.T, model app.Model) {
				pane, _ := model.Workspace.ActiveTab().ActivePane()
				if pane.TerminalID != types.TerminalID("term-1") {
					t.Fatalf("expected new tab pane to bind term-1, got %+v", pane)
				}
			},
		},
		{
			name:   "open floating",
			intent: app.OpenSelectedTerminalInFloatingIntent{},
			check: func(t *testing.T, model app.Model) {
				pane, _ := model.Workspace.ActiveTab().ActivePane()
				if pane.Kind != types.PaneKindFloating || pane.TerminalID != types.TerminalID("term-1") {
					t.Fatalf("expected floating pane to bind term-1, got %+v", pane)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &stubClient{
				attachResult: &protocol.AttachResult{Channel: 33, Mode: "collaborator"},
				snapshot:     &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}},
			}
			service := NewTerminalService(client)
			model := terminalPoolModelForRuntimeTest()

			next, err := ApplyIntent(context.Background(), model, service, tt.intent)
			if err != nil {
				t.Fatalf("ApplyIntent returned error: %v", err)
			}
			if client.lastAttachID != "term-1" || client.lastAttachMode != "collaborator" {
				t.Fatalf("expected workbench attach, got id=%q mode=%q", client.lastAttachID, client.lastAttachMode)
			}
			session := next.Sessions[types.TerminalID("term-1")]
			if session.Channel != 33 || session.ReadOnly || session.Preview {
				t.Fatalf("expected writable workbench session, got %#v", session)
			}
			tt.check(t, next)
		})
	}
}

func TestTerminalPoolKeyBindingsReachRuntimeActions(t *testing.T) {
	tests := []struct {
		name   string
		key    tea.KeyMsg
		assert func(t *testing.T, client *stubClient, model app.Model)
	}{
		{
			name: "enter opens here",
			key:  tea.KeyMsg{Type: tea.KeyEnter},
			assert: func(t *testing.T, client *stubClient, model app.Model) {
				if client.lastAttachID != "term-1" || client.lastAttachMode != "collaborator" {
					t.Fatalf("expected enter to attach selected terminal, got id=%q mode=%q", client.lastAttachID, client.lastAttachMode)
				}
				pane, _ := model.Workspace.ActiveTab().ActivePane()
				if pane.TerminalID != "term-1" {
					t.Fatalf("expected enter to keep active pane bound, got %+v", pane)
				}
			},
		},
		{
			name: "t opens new tab",
			key:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}},
			assert: func(t *testing.T, client *stubClient, model app.Model) {
				if client.lastAttachID != "term-1" || client.lastAttachMode != "collaborator" {
					t.Fatalf("expected t to attach selected terminal, got id=%q mode=%q", client.lastAttachID, client.lastAttachMode)
				}
				pane, _ := model.Workspace.ActiveTab().ActivePane()
				if pane.TerminalID != "term-1" {
					t.Fatalf("expected t to bind new tab pane, got %+v", pane)
				}
			},
		},
		{
			name: "k kills selected terminal",
			key:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}},
			assert: func(t *testing.T, client *stubClient, model app.Model) {
				if client.lastKilledID != "term-1" {
					t.Fatalf("expected k to kill selected terminal, got %q", client.lastKilledID)
				}
				if model.Terminals[types.TerminalID("term-1")].State != stateterminal.StateExited {
					t.Fatalf("expected k to mark terminal exited, got %#v", model.Terminals[types.TerminalID("term-1")])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &stubClient{
				attachResult: &protocol.AttachResult{Channel: 41, Mode: "collaborator"},
				snapshot:     &protocol.Snapshot{TerminalID: "term-1", Size: protocol.Size{Cols: 80, Rows: 24}},
			}
			model := terminalPoolModelForRuntimeTest()
			model.IntentExecutor = NewModelIntentExecutor(NewTerminalService(client))

			teaModel, _ := model.Update(tt.key)
			updated := teaModel.(app.Model)
			tt.assert(t, client, updated)
		})
	}
}

func TestTerminalPoolRemoveActionReachesRuntimeService(t *testing.T) {
	service := NewTerminalService(&stubClient{})
	model := terminalPoolModelForRuntimeTest()

	next, err := ApplyIntent(context.Background(), model, service, app.RemoveSelectedTerminalIntent{})
	if err != nil {
		t.Fatalf("ApplyIntent returned error: %v", err)
	}
	client := service.client.(*stubClient)
	if client.lastRemovedID != "term-1" {
		t.Fatalf("expected remove to reach runtime service, got %q", client.lastRemovedID)
	}
	if _, ok := next.Terminals[types.TerminalID("term-1")]; ok {
		t.Fatalf("expected removed terminal to leave model, got %#v", next.Terminals[types.TerminalID("term-1")])
	}
}

func TestTerminalPoolMetadataSaveReachesRuntimeService(t *testing.T) {
	service := NewTerminalService(&stubClient{})
	model := terminalPoolModelForRuntimeTest()

	opened := model.Apply(app.OpenTerminalPoolIntent{})
	edited := opened.Apply(app.OpenTerminalMetadataEditorIntent{}).Apply(app.UpdateTerminalMetadataDraftIntent{
		Name:     "api-renamed",
		TagsText: "backend,prod",
	})
	next, err := ApplyIntent(context.Background(), edited, service, app.SaveTerminalMetadataIntent{})
	if err != nil {
		t.Fatalf("ApplyIntent returned error: %v", err)
	}
	client := service.client.(*stubClient)
	if client.lastMetadataID != "term-2" || client.lastMetadataName != "api-renamed" {
		t.Fatalf("expected metadata call to reach runtime, got id=%q name=%q", client.lastMetadataID, client.lastMetadataName)
	}
	if next.Terminals[types.TerminalID("term-2")].Name != "api-renamed" {
		t.Fatalf("expected model metadata update after runtime success, got %#v", next.Terminals[types.TerminalID("term-2")])
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

func terminalPoolModelForRuntimeTest() app.Model {
	model := livePaneModelForRuntimeTest()
	model.Terminals[types.TerminalID("term-2")] = stateterminal.Metadata{
		ID:              types.TerminalID("term-2"),
		Name:            "worker-tail",
		Command:         []string{"bash", "-lc", "tail -f worker.log"},
		Tags:            map[string]string{"team": "ops"},
		State:           stateterminal.StateRunning,
		LastInteraction: time.Unix(20, 0),
	}
	meta := model.Terminals[types.TerminalID("term-1")]
	meta.Tags = map[string]string{"team": "backend"}
	model.Terminals[types.TerminalID("term-1")] = meta
	model.Screen = app.ScreenTerminalPool
	model.FocusTarget = app.FocusTerminalPool
	model.Pool = app.TerminalPoolState{
		SelectedTerminalID: types.TerminalID("term-1"),
		PreviewTerminalID:  types.TerminalID("term-1"),
		PreviewReadonly:    true,
	}
	return model
}

func flattenedPreviewText(model app.Model, terminalID types.TerminalID) string {
	session := model.Sessions[terminalID]
	if session.Snapshot == nil {
		return ""
	}
	var lines []string
	for _, row := range session.Snapshot.Screen.Cells {
		var b strings.Builder
		for _, cell := range row {
			b.WriteString(cell.Content)
		}
		lines = append(lines, b.String())
	}
	return strings.Join(lines, "\n")
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

func (c protocolRuntimeClient) Remove(ctx context.Context, terminalID string) error {
	return c.inner.Remove(ctx, terminalID)
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
