package termxcorev2

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-proto/wire"
	"github.com/lozzow/termx/termx-shared/transport/memory"
)

func TestProtocolServiceCreateListMetadataRestartRemove(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	created, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Name:    "demo",
		Tags:    map[string]string{"role": "test"},
		Command: []string{"shell"},
		Size:    protocol.Size{Cols: 12, Rows: 4},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.TerminalID != "term-1" || created.State != string(TerminalStateRunning) {
		t.Fatalf("unexpected create result %#v", created)
	}

	list, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Terminals) != 1 || list.Terminals[0].Name != "demo" || list.Terminals[0].Tags["role"] != "test" {
		t.Fatalf("unexpected list result %#v", list)
	}

	if err := client.SetMetadata(context.Background(), "term-1", "renamed", map[string]string{"role": "updated"}); err != nil {
		t.Fatalf("set metadata: %v", err)
	}
	var info protocol.TerminalInfo
	if err := client.Call(context.Background(), "get", protocol.GetParams{TerminalID: "term-1"}, &info); err != nil {
		t.Fatalf("get: %v", err)
	}
	if info.Name != "renamed" || info.Tags["role"] != "updated" {
		t.Fatalf("unexpected terminal metadata %#v", info)
	}

	if err := client.Restart(context.Background(), "term-1"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if process := serverProcess(t, server, "term-1"); process == nil {
		t.Fatal("expected restarted process")
	}

	if err := client.Remove(context.Background(), "term-1"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := server.GetTerminal("term-1"); !errors.Is(err, ErrTerminalNotFound) {
		t.Fatalf("expected removed terminal, got %v", err)
	}
}

func TestProtocolServiceCreateCarriesRemoteProcessContract(t *testing.T) {
	factory := newRecordingProcessFactory()
	_, client, closeClient := newProtocolClientWithProcessFactory(t, factory)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:                 "term-remote",
		Name:               "remote",
		Command:            []string{"shell"},
		Size:               protocol.Size{Cols: 100, Rows: 40},
		Dir:                "/tmp/termx-remote",
		Env:                []string{"TERMUX_REMOTE=1", "TERMUX_REGION=local"},
		ScrollbackSize:     123,
		ScrollbackMaxBytes: 4567,
		ScrollbackMaxAge:   2 * time.Hour,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	specs := factory.spawnedSpecs("term-remote")
	if len(specs) != 1 {
		t.Fatalf("expected one process spawn, got %#v", specs)
	}
	assertRemoteProcessSpec(t, specs[0])

	list, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Terminals) != 1 || list.Terminals[0].CWD != "/tmp/termx-remote" || list.Terminals[0].LiveCWD != "/tmp/termx-remote" {
		t.Fatalf("list should expose create cwd, got %#v", list)
	}
	var info protocol.TerminalInfo
	if err := client.Call(context.Background(), "get", protocol.GetParams{TerminalID: "term-remote"}, &info); err != nil {
		t.Fatalf("get: %v", err)
	}
	if info.CWD != "/tmp/termx-remote" || info.LiveCWD != "/tmp/termx-remote" {
		t.Fatalf("get should expose create cwd, got %#v", info)
	}

	if err := client.Restart(context.Background(), "term-remote"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	specs = factory.spawnedSpecs("term-remote")
	if len(specs) != 2 {
		t.Fatalf("expected restart to spawn second process, got %#v", specs)
	}
	assertRemoteProcessSpec(t, specs[1])
}

func assertRemoteProcessSpec(t *testing.T, spec ProcessSpec) {
	t.Helper()
	if spec.TerminalID != "term-remote" || spec.Dir != "/tmp/termx-remote" || spec.Size != (Size{Cols: 100, Rows: 40}) {
		t.Fatalf("process spec lost terminal/dir/size: %#v", spec)
	}
	if got := strings.Join(spec.Env, "\x00"); !strings.Contains(got, "TERMUX_REMOTE=1") || !strings.Contains(got, "TERMUX_REGION=local") {
		t.Fatalf("process spec lost env: %#v", spec.Env)
	}
	if spec.ScrollbackSize != 123 || spec.ScrollbackMaxBytes != 4567 || spec.ScrollbackMaxAge != 2*time.Hour {
		t.Fatalf("process spec lost scrollback contract: %#v", spec)
	}
}

func TestProtocolServiceExitMetadataRoundTrips(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()
	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Name:    "job",
		Command: []string{"bash", "-lc", "make test"},
		Size:    protocol.Size{Cols: 12, Rows: 4},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	events, err := client.Events(context.Background(), protocol.EventsParams{
		TerminalID: "term-1",
		Types:      []protocol.EventType{protocol.EventTerminalStateChanged},
	})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	serverProcess(t, server, "term-1").exit(23)
	event := requireProtocolEvent(t, events)
	if event.StateChanged == nil || event.StateChanged.ExitCode == nil || *event.StateChanged.ExitCode != 23 || event.StateChanged.ExitedAt.IsZero() {
		t.Fatalf("expected exited event metadata, got %#v", event)
	}
	var info protocol.TerminalInfo
	if err := client.Call(context.Background(), "get", protocol.GetParams{TerminalID: "term-1"}, &info); err != nil {
		t.Fatalf("get: %v", err)
	}
	if info.State != string(TerminalStateExited) || info.ExitCode == nil || *info.ExitCode != 23 || !info.ExitedAt.Equal(event.StateChanged.ExitedAt) {
		t.Fatalf("expected get to carry exit metadata, got %#v event=%#v", info, event)
	}
}

func TestProtocolServiceRestartedProcessSurvivesClientSessionClose(t *testing.T) {
	factory := newSessionBoundRecordingProcessFactory()
	server, client, closeClient := newProtocolClientWithProcessFactory(t, factory)
	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Name:    "job",
		Command: []string{"shell"},
		Size:    protocol.Size{Cols: 12, Rows: 4},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := client.Restart(context.Background(), "term-1"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	restarted := factory.process("term-1")
	if restarted == nil {
		t.Fatal("expected restarted process")
	}
	closeClient()

	select {
	case exit, ok := <-restarted.Wait():
		if ok {
			t.Fatalf("restarted process must not be tied to closed protocol session, exit=%#v", exit)
		}
	case <-time.After(100 * time.Millisecond):
	}
	info, err := server.GetTerminal("term-1")
	if err != nil {
		t.Fatalf("get terminal after client close: %v", err)
	}
	if info.State != TerminalStateRunning {
		t.Fatalf("closing client session must not mark restarted terminal exited, got %#v", info)
	}
}

func TestProtocolServiceHistoryWindowReportsNotRebuilt(t *testing.T) {
	_, client, closeClient := newProtocolClient(t)
	defer closeClient()
	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 3}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{TerminalID: "term-1", Cols: 10, Limit: 2})
	if err == nil || !strings.Contains(err.Error(), ErrHistoryNotRebuilt.Error()) {
		t.Fatalf("history.window should report clean-slate state, got %v", err)
	}
	_, err = client.HistoryCopy(context.Background(), protocol.HistoryWindowParams{TerminalID: "term-1", Token: "missing"})
	if err == nil || !strings.Contains(err.Error(), ErrHistoryNotRebuilt.Error()) {
		t.Fatalf("history.copy should report clean-slate state, got %v", err)
	}
}

func TestProtocolServiceAttachmentStreamsLiveScreenUpdates(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{
		ID:      "term-1",
		Command: []string{"shell"},
		Size:    protocol.Size{Cols: 20, Rows: 4},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	attach, err := client.Attach(context.Background(), "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	stream, stop := client.Stream(attach.Channel)
	defer stop()

	if err := server.IngestOutput(context.Background(), "term-1", "typed-live"); err != nil {
		t.Fatalf("ingest live output: %v", err)
	}

	update := waitForScreenUpdateContaining(t, stream, "typed-live")
	if !update.FullReplace {
		t.Fatalf("attachment live stream should send full-replace screen update, got %#v", update)
	}
	if update.Size != (protocol.Size{Cols: 20, Rows: 4}) {
		t.Fatalf("screen update should carry terminal size, got %#v", update.Size)
	}
}

func TestProtocolServiceAttachRoutesInputResizeAndEvents(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 10, Rows: 3}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	events, err := client.Events(context.Background(), protocol.EventsParams{
		TerminalID: "term-1",
		Types:      []protocol.EventType{protocol.EventTerminalResized},
	})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	attach, err := client.AttachWithOptions(context.Background(), protocol.AttachParams{
		TerminalID:   "term-1",
		Mode:         "collaborator",
		ResizePolicy: protocol.ResizePolicyOwner,
		SurfaceID:    "surface-1",
		ViewID:       "view-1",
	})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if attach.Channel == 0 || attach.ResizeControl == nil || !attach.ResizeControl.CanResize {
		t.Fatalf("unexpected attach result %#v", attach)
	}

	if err := client.InputWithOptions(context.Background(), protocol.InputParams{
		TerminalID: "term-1",
		Channel:    attach.Channel,
		SurfaceID:  "surface-1",
		ViewID:     "view-1",
		Data:       []byte("echo hi\n"),
	}); err != nil {
		t.Fatalf("input: %v", err)
	}
	if err := client.Resize(context.Background(), attach.Channel, 20, 5); err != nil {
		t.Fatalf("resize frame: %v", err)
	}
	process := waitForProcessTraffic(t, server, "term-1", 1, 1)
	inputs, resizes, _, _ := process.snapshot()
	if string(inputs[0]) != "echo hi\n" {
		t.Fatalf("input did not reach process %#v", inputs)
	}
	if resizes[0] != (Size{Cols: 20, Rows: 5}) {
		t.Fatalf("resize did not reach process %#v", resizes)
	}

	evt := requireProtocolEvent(t, events)
	if evt.Type != protocol.EventTerminalResized || evt.Resized == nil || evt.Resized.NewSize != (protocol.Size{Cols: 20, Rows: 5}) {
		t.Fatalf("unexpected resize event %#v", evt)
	}
}

func TestProtocolServiceStorageMethodsAndEvents(t *testing.T) {
	_, client, closeClient := newProtocolClient(t)
	defer closeClient()

	events, err := client.Events(context.Background(), protocol.EventsParams{
		Types:            []protocol.EventType{protocol.EventStorageChanged},
		StorageAppID:     "termx-tui-v3",
		StorageScope:     protocol.StorageScopePublic,
		StorageOwnerID:   "workspace-main",
		StorageKeyPrefix: "workbench/",
	})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	entry, err := client.StoragePut(context.Background(), protocol.StoragePutParams{
		AppID:   "termx-tui-v3",
		Scope:   protocol.StorageScopePublic,
		OwnerID: "workspace-main",
		Key:     "workbench/root",
		Value:   []byte("v1"),
	})
	if err != nil {
		t.Fatalf("storage put: %v", err)
	}
	if entry.Version != 1 || string(entry.Value) != "v1" {
		t.Fatalf("unexpected put entry %#v", entry)
	}
	event := requireProtocolEvent(t, events)
	if event.Type != protocol.EventStorageChanged || event.Storage == nil || event.Storage.Key != "workbench/root" || event.Storage.Version != 1 || event.Storage.Op != "put" {
		t.Fatalf("unexpected storage event %#v", event)
	}
	got, err := client.StorageGet(context.Background(), protocol.StorageGetParams{
		AppID:   "termx-tui-v3",
		Scope:   protocol.StorageScopePublic,
		OwnerID: "workspace-main",
		Key:     "workbench/root",
	})
	if err != nil {
		t.Fatalf("storage get: %v", err)
	}
	if got.Version != 1 || string(got.Value) != "v1" {
		t.Fatalf("unexpected get entry %#v", got)
	}
}

func TestProtocolServiceWorkbenchMethodsAndEvents(t *testing.T) {
	_, client, closeClient := newProtocolClient(t)
	defer closeClient()

	events, err := client.Events(context.Background(), protocol.EventsParams{
		Types:       []protocol.EventType{protocol.EventWorkbenchChanged},
		WorkbenchID: "workspace-main",
	})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	snapshot, err := client.WorkbenchGet(context.Background(), protocol.WorkbenchGetParams{})
	if err != nil {
		t.Fatalf("workbench get: %v", err)
	}
	result, err := client.WorkbenchApply(context.Background(), protocol.WorkbenchMutateParams{
		Action:          protocol.WorkbenchMutationPaneSplit,
		WorkspaceID:     "workspace-main",
		TabID:           "tab-main",
		PaneID:          "pane-main",
		TargetID:        "pane-two",
		Name:            "logs",
		SplitDirection:  protocol.WorkbenchSplitHorizontal,
		CheckVersion:    true,
		ExpectedVersion: snapshot.Version,
	})
	if err != nil {
		t.Fatalf("workbench apply: %v", err)
	}
	if result.Snapshot.Version != 2 || result.ResourceID != "pane-two" {
		t.Fatalf("unexpected apply result %#v", result)
	}
	event := requireProtocolEvent(t, events)
	if event.Type != protocol.EventWorkbenchChanged || event.Workbench == nil || event.Workbench.ResourceID != "pane-two" || event.Workbench.Version != 2 {
		t.Fatalf("unexpected workbench event %#v", event)
	}
}

func TestProtocolServiceSnapshotReturnsLiveSurfaceRows(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 12, Rows: 4}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "alpha\n\x1b[31mERR\x1b[0m ok\r\x1b[32mOK\x1b[0m"); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	snapshot, err := client.Snapshot(context.Background(), "term-1", 0, 2)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.TerminalID != "term-1" || snapshot.Size != (protocol.Size{Cols: 12, Rows: 4}) {
		t.Fatalf("unexpected snapshot metadata %#v", snapshot)
	}
	if len(snapshot.Screen.Cells) != 4 {
		t.Fatalf("expected size-bound live screen rows, got %#v", snapshot.Screen.Cells)
	}
	got := []string{cellsText(snapshot.Screen.Cells[0]), cellsText(snapshot.Screen.Cells[1])}
	if got[0] != "alpha" || !strings.HasPrefix(got[1], "OK   ERR ok") || len(snapshot.Scrollback) != 0 {
		t.Fatalf("snapshot must expose live screen cell matrix without scrollback truth, got rows=%#v scrollback=%#v", got, snapshot.Scrollback)
	}
	if snapshot.Screen.Cells[1][0].Style.FG != "ansi:2" {
		t.Fatalf("snapshot must preserve live cell style, got %#v", snapshot.Screen.Cells[1][0])
	}

	compact, err := client.SnapshotCompact(context.Background(), "term-1", 0, 2)
	if err != nil {
		t.Fatalf("compact snapshot: %v", err)
	}
	if compact.TerminalID != "term-1" || compact.Size != (protocol.Size{Cols: 12, Rows: 4}) {
		t.Fatalf("unexpected compact snapshot metadata %#v", compact)
	}
	if got := compact.ScreenRows[0].Text; got != "alpha" {
		t.Fatalf("compact snapshot should keep row compact text, got %#v", compact.ScreenRows[0])
	}
}

func newProtocolClient(t *testing.T) (*Server, *protocol.Client, func()) {
	return newProtocolClientWithProcessFactory(t, newRecordingProcessFactory())
}

func newProtocolClientWithProcessFactory(t *testing.T, factory ProcessFactory) (*Server, *protocol.Client, func()) {
	t.Helper()
	return newProtocolClientWithServer(t, NewServer(WithProcessFactory(factory)))
}

func newProtocolClientWithServer(t *testing.T, server *Server) (*Server, *protocol.Client, func()) {
	t.Helper()
	clientTransport, serverTransport := memory.NewPair()
	errCh := make(chan error, 1)
	go func() {
		errCh <- newProtocolSession(server, serverTransport, TransportScope{}).run(context.Background())
	}()
	client := protocol.NewClient(clientTransport)
	if err := client.Hello(context.Background(), protocol.Hello{Version: wire.Version, Client: "test"}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	closeClient := func() {
		_ = client.Close()
		select {
		case err := <-errCh:
			if err != nil && !strings.Contains(err.Error(), "EOF") {
				t.Fatalf("server session returned error: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("server session did not stop")
		}
	}
	return server, client, closeClient
}

func waitForScreenUpdateContaining(t *testing.T, stream <-chan protocol.StreamFrame, want string) protocol.ScreenUpdate {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case frame, ok := <-stream:
			if !ok {
				t.Fatalf("stream closed before screen update containing %q", want)
			}
			if frame.Type != wire.TypeScreenUpdate {
				continue
			}
			update, err := protocol.DecodeScreenUpdatePayload(frame.Payload)
			if err != nil {
				t.Fatalf("decode screen update: %v", err)
			}
			if screenUpdateContainsText(update, want) {
				return update
			}
		case <-deadline:
			t.Fatalf("timed out waiting for screen update containing %q", want)
		}
	}
}

func screenUpdateContainsText(update protocol.ScreenUpdate, want string) bool {
	for _, row := range update.Screen.Cells {
		var builder strings.Builder
		for _, cell := range row {
			builder.WriteString(cell.Content)
		}
		if strings.Contains(builder.String(), want) {
			return true
		}
	}
	return false
}

func waitForTerminalState(t *testing.T, server *Server, terminalID string, want TerminalState) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		info, err := server.GetTerminal(terminalID)
		if err == nil && info.State == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	info, err := server.GetTerminal(terminalID)
	if err != nil {
		t.Fatalf("timed out waiting for terminal %q state %q: %v", terminalID, want, err)
	}
	t.Fatalf("timed out waiting for terminal %q state %q, got %#v", terminalID, want, info)
}

func serverProcess(t *testing.T, server *Server, terminalID string) *recordingProcess {
	t.Helper()
	terminal, err := server.Terminal(terminalID)
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	process, ok := terminal.process.(*recordingProcess)
	if !ok {
		t.Fatalf("expected recording process, got %T", terminal.process)
	}
	return process
}

func waitForProcessTraffic(t *testing.T, server *Server, terminalID string, inputs int, resizes int) *recordingProcess {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		process := serverProcess(t, server, terminalID)
		processInputs, processResizes, _, _ := process.snapshot()
		if len(processInputs) >= inputs && len(processResizes) >= resizes {
			return process
		}
		time.Sleep(time.Millisecond)
	}
	process := serverProcess(t, server, terminalID)
	processInputs, processResizes, _, _ := process.snapshot()
	t.Fatalf("timed out waiting for process traffic inputs=%d/%d resizes=%d/%d", len(processInputs), inputs, len(processResizes), resizes)
	return process
}

func requireProtocolEvent(t *testing.T, events <-chan protocol.Event) protocol.Event {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("protocol events channel closed")
		}
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for protocol event")
	}
	return protocol.Event{}
}

func cellsText(row []protocol.Cell) string {
	var builder strings.Builder
	for _, cell := range row {
		builder.WriteString(cell.Content)
	}
	return builder.String()
}
