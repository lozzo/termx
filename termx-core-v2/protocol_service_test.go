package termxcorev2

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

func TestProtocolServiceCreateUsesNameAsDefaultTerminalID(t *testing.T) {
	_, client, closeClient := newProtocolClient(t)
	defer closeClient()

	created, err := client.Create(context.Background(), protocol.CreateParams{
		Name:    "named-shell",
		Command: []string{"shell"},
		Size:    protocol.Size{Cols: 12, Rows: 4},
	})
	if err != nil {
		t.Fatalf("create with name-only identity: %v", err)
	}
	if created.TerminalID != "named-shell" {
		t.Fatalf("name-only create should return name as terminal id, got %#v", created)
	}
	if _, err := client.Create(context.Background(), protocol.CreateParams{
		Name:    "named-shell",
		Command: []string{"shell"},
		Size:    protocol.Size{Cols: 12, Rows: 4},
	}); err == nil || !strings.Contains(err.Error(), "duplicate terminal") {
		t.Fatalf("duplicate name create should fail with duplicate terminal, got %v", err)
	}
}

func TestProtocolServiceListDirectoriesUsesDaemonPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "project"), 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "profile"), 0o755); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}

	_, client, closeClient := newProtocolClient(t)
	defer closeClient()

	result, err := client.ListDirectories(context.Background(), protocol.PathListDirsParams{Prefix: "pro", Limit: 10})
	if err != nil {
		t.Fatalf("list directories: %v", err)
	}
	if result.BasePath != resolvedDir || result.Missing || len(result.Entries) != 2 || result.Entries[0].Path != "profile"+string(os.PathSeparator) || result.Entries[1].Path != "project"+string(os.PathSeparator) {
		t.Fatalf("unexpected path list result %#v", result)
	}
}

func TestProtocolServicePathDefaultsUsesDaemonEnvironment(t *testing.T) {
	dir := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	t.Setenv("SHELL", "/bin/sh")
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}

	_, client, closeClient := newProtocolClient(t)
	defer closeClient()

	result, err := client.PathDefaults(context.Background())
	if err != nil {
		t.Fatalf("path defaults: %v", err)
	}
	if strings.Join(result.DefaultCommand, " ") != "/bin/sh" || result.DefaultCWD != resolvedDir {
		t.Fatalf("unexpected path defaults %#v", result)
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

func TestProtocolServiceHistoryWindowReturnsAuthoritativeRowsAfterR324(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()
	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-history-r324", Command: []string{"shell"}, Size: protocol.Size{Cols: 24, Rows: 4}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-history-r324", "alpha\r\nbeta\r\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}

	window, err := client.HistoryWindow(context.Background(), protocol.HistoryWindowParams{TerminalID: "term-history-r324", Cols: 24, Limit: 10})
	if err != nil {
		t.Fatalf("history.window should return authoritative rows: %v", err)
	}
	got := protocolRowTexts(window.Rows)
	joined := strings.Join(got, "|")
	if !strings.Contains(joined, "terminal started: term-history-r324") || !strings.Contains(joined, "started at: ") {
		t.Fatalf("history.window should include core lifecycle start marker, got %v window=%#v", got, window)
	}
	alphaIdx, betaIdx := -1, -1
	for i, text := range got {
		switch text {
		case "alpha":
			alphaIdx = i
		case "beta":
			betaIdx = i
		}
	}
	if alphaIdx < 0 || betaIdx < 0 || betaIdx != alphaIdx+1 {
		t.Fatalf("history.window should preserve payload rows after lifecycle marker, got %v window=%#v", got, window)
	}
	if window.Token == "" || len(window.RowLineIDs) != len(window.Rows) {
		t.Fatalf("history.window should carry frozen token and row ids, got %#v", window)
	}
	if window.Generation == 0 {
		t.Fatalf("history.window should carry generation, got %#v", window)
	}
	if window.HasMore && (!window.CursorValid || window.CursorLineID == 0) {
		t.Fatalf("history.window with older rows should carry segment cursor, got %#v", window)
	}
	if got := strings.Join(window.RowSegments, "|"); got == "" {
		t.Fatalf("history.window should preserve row segments, got %q window=%#v", got, window)
	}
	if len(window.RowKinds) <= betaIdx || window.RowKinds[alphaIdx] != "ordinary" || window.RowKinds[betaIdx] != "ordinary" {
		t.Fatalf("history.window should preserve payload row kinds, got %#v window=%#v", window.RowKinds, window)
	}
	if window.FirstLineID != window.RowLineIDs[0] || window.LastLineID != window.RowLineIDs[len(window.RowLineIDs)-1] || len(window.RowInLine) != len(window.Rows) {
		t.Fatalf("history.window should preserve logical line boundary and row mapping, got %#v", window)
	}
	text, err := client.HistoryCopy(context.Background(), protocol.HistoryWindowParams{
		TerminalID:       "term-history-r324",
		Token:            window.Token,
		RangeValid:       true,
		RangeStartLineID: window.RowLineIDs[alphaIdx],
		RangeEndLineID:   window.RowLineIDs[betaIdx],
	})
	if err != nil {
		t.Fatalf("history.copy should use authoritative token: %v", err)
	}
	if text != "alpha\nbeta" {
		t.Fatalf("history.copy mismatch: %q", text)
	}
	if err := client.ReleaseHistory(context.Background(), protocol.HistoryWindowParams{TerminalID: "term-history-r324", Token: window.Token}); err != nil {
		t.Fatalf("history.release should succeed: %v", err)
	}
}

func TestR448ProtocolServiceHistoryBacklogStatus(t *testing.T) {
	server := NewServer(
		WithProcessFactory(newRecordingProcessFactory()),
		WithHistoryBackpressureConfig(HistoryBackpressureConfig{
			Mode:        HistoryBackpressureBounded,
			BufferBytes: 4096,
		}),
	)
	_, client, closeClient := newProtocolClientWithServer(t, server)
	defer closeClient()
	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-r448-backlog", Command: []string{"shell"}, Size: protocol.Size{Cols: 24, Rows: 4}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r448-backlog", "alpha\r\nbeta\r\n"); err != nil {
		t.Fatalf("ingest output: %v", err)
	}
	status, err := client.HistoryBacklogStatus(context.Background(), "term-r448-backlog")
	if err != nil {
		t.Fatalf("history backlog status: %v", err)
	}
	if status.TerminalID != "term-r448-backlog" || !status.HistoryEnabled {
		t.Fatalf("status lost terminal identity/enabled flag: %#v", status)
	}
	if status.BackpressureMode != string(HistoryBackpressureBounded) || status.BufferLimitBytes != 4096 {
		t.Fatalf("status lost backpressure config: %#v", status)
	}
	if status.AppliedSeq == 0 || status.TargetSeq == 0 || status.AppliedSeq != status.TargetSeq {
		t.Fatalf("status should expose applied target seq after ingest: %#v", status)
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

func TestProtocolServiceAttachReplacesSameClientViewAttachment(t *testing.T) {
	_, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-reattach", Command: []string{"shell"}, Size: protocol.Size{Cols: 80, Rows: 24}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	first, err := client.AttachWithOptions(context.Background(), protocol.AttachParams{
		TerminalID:   "term-reattach",
		ResizePolicy: protocol.ResizePolicyOwner,
		SurfaceID:    "surface-1",
		ViewID:       "view-main",
	})
	if err != nil {
		t.Fatalf("first attach: %v", err)
	}
	second, err := client.AttachWithOptions(context.Background(), protocol.AttachParams{
		TerminalID:   "term-reattach",
		ResizePolicy: protocol.ResizePolicyFollower,
		SurfaceID:    "surface-1",
		ViewID:       "view-main",
	})
	if err != nil {
		t.Fatalf("second attach: %v", err)
	}
	if second.Channel == first.Channel {
		t.Fatalf("reattach should allocate a fresh channel, first=%#v second=%#v", first, second)
	}
	if err := client.InputWithOptions(context.Background(), protocol.InputParams{
		TerminalID: "term-reattach",
		Channel:    first.Channel,
		SurfaceID:  "surface-1",
		ViewID:     "view-main",
		Data:       []byte("old"),
	}); err == nil {
		t.Fatal("old reattached channel should be released")
	}
	if err := client.InputWithOptions(context.Background(), protocol.InputParams{
		TerminalID: "term-reattach",
		Channel:    second.Channel,
		SurfaceID:  "surface-1",
		ViewID:     "view-main",
		Data:       []byte("new"),
	}); err != nil {
		t.Fatalf("fresh channel should accept input: %v", err)
	}
	list, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("list after reattach: %v", err)
	}
	if len(list.Terminals) != 1 || list.Terminals[0].ResizeOwnerAttachmentCount != 1 {
		t.Fatalf("same client view reattach should count as one view attachment, list=%#v", list)
	}
	if _, err := client.AttachWithOptions(context.Background(), protocol.AttachParams{
		TerminalID:   "term-reattach",
		ResizePolicy: protocol.ResizePolicyFollower,
		SurfaceID:    "surface-1",
		ViewID:       "view-side",
	}); err != nil {
		t.Fatalf("third attach: %v", err)
	}
	list, err = client.List(context.Background())
	if err != nil {
		t.Fatalf("list after second view: %v", err)
	}
	if len(list.Terminals) != 1 || list.Terminals[0].ResizeOwnerAttachmentCount != 2 {
		t.Fatalf("distinct client views should count separately, list=%#v", list)
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

func TestProtocolServiceLiveScreenReturnsNativeRows(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-1", Command: []string{"shell"}, Size: protocol.Size{Cols: 40, Rows: 6}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-1", "alpha\r\n\x1b[32mOK\x1b[0m"); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	liveScreen, err := client.LiveScreen(context.Background(), "term-1")
	if err != nil {
		t.Fatalf("live screen: %v", err)
	}
	if liveScreen.TerminalID != "term-1" || liveScreen.Size != (protocol.Size{Cols: 40, Rows: 6}) || liveScreen.Revision == 0 {
		t.Fatalf("unexpected live screen metadata %#v", liveScreen)
	}
	rows := liveScreen.Rows
	if len(rows) != 6 {
		t.Fatalf("expected size-bound live screen rows, got %#v", rows)
	}
	var rowText strings.Builder
	for _, row := range rows {
		if rowText.Len() > 0 {
			rowText.WriteString("\n")
		}
		rowText.WriteString(row.Text)
	}
	joined := rowText.String()
	if !strings.Contains(joined, "terminal started: term-1") || !strings.Contains(joined, "started at: ") {
		t.Fatalf("live screen should expose terminal start marker, rows=%#v", rows)
	}
	if !strings.Contains(joined, "alpha") {
		t.Fatalf("live screen should expose native row text, got %#v", rows)
	}
	var okCells []protocol.Cell
	for _, row := range rows {
		cells := row.DecodeCells()
		if nativeCellsText(cells) == "OK" {
			okCells = cells
			break
		}
	}
	if len(okCells) == 0 || okCells[0].Style.FG != "ansi:2" {
		t.Fatalf("live screen must preserve styled native cells, rows=%#v okCells=%#v", rows, okCells)
	}
}

func nativeCellsText(cells []protocol.Cell) string {
	var builder strings.Builder
	for _, cell := range cells {
		builder.WriteString(cell.Content)
	}
	return strings.TrimRight(builder.String(), " ")
}

func TestProtocolServiceLiveScreenRevisionAdvancesWithOutput(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-r347-live-revision", Command: []string{"shell"}, Size: protocol.Size{Cols: 12, Rows: 4}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	first, err := client.LiveScreen(context.Background(), "term-r347-live-revision")
	if err != nil {
		t.Fatalf("initial live screen: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r347-live-revision", "alpha\r\n"); err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	second, err := client.LiveScreen(context.Background(), "term-r347-live-revision")
	if err != nil {
		t.Fatalf("second live screen: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-r347-live-revision", "beta\r\n"); err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	third, err := client.LiveScreen(context.Background(), "term-r347-live-revision")
	if err != nil {
		t.Fatalf("third live screen: %v", err)
	}
	if !(first.Revision < second.Revision && second.Revision < third.Revision) {
		t.Fatalf("live projection revision must advance monotonically, got %d -> %d -> %d", first.Revision, second.Revision, third.Revision)
	}
}

func TestProtocolServiceNextLiveInvalidationDecodesOneShotParams(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-live-next", Command: []string{"shell"}, Size: protocol.Size{Cols: 12, Rows: 4}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	initial, err := client.LiveScreen(context.Background(), "term-live-next")
	if err != nil {
		t.Fatalf("initial live screen: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	events := make(chan *protocol.Event, 1)
	errs := make(chan error, 1)
	go func() {
		event, err := client.NextLiveInvalidation(ctx, "term-live-next", initial.Revision)
		if err != nil {
			errs <- err
			return
		}
		events <- event
	}()

	select {
	case err := <-errs:
		t.Fatalf("live invalidation request should decode terminal-scoped params, got %v", err)
	case event := <-events:
		t.Fatalf("live invalidation arm returned before next wake: %#v", event)
	case <-time.After(20 * time.Millisecond):
	}
	if err := server.IngestOutput(context.Background(), "term-live-next", "wake\r\n"); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	select {
	case err := <-errs:
		t.Fatalf("live invalidation request failed after wake: %v", err)
	case event := <-events:
		if event == nil || event.Type != protocol.EventTerminalLiveInvalidated || event.TerminalID != "term-live-next" {
			t.Fatalf("unexpected live invalidation event %#v", event)
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for live invalidation event: %v", ctx.Err())
	}
}

func TestProtocolServiceNextLiveInvalidationUsesObservedRevision(t *testing.T) {
	server, client, closeClient := newProtocolClient(t)
	defer closeClient()

	if _, err := client.Create(context.Background(), protocol.CreateParams{ID: "term-live-observed", Command: []string{"shell"}, Size: protocol.Size{Cols: 12, Rows: 4}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-live-observed", "first\r\n"); err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	first, err := client.LiveScreen(context.Background(), "term-live-observed")
	if err != nil {
		t.Fatalf("first live screen: %v", err)
	}
	if err := server.IngestOutput(context.Background(), "term-live-observed", "second\r\n"); err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := client.NextLiveInvalidation(ctx, "term-live-observed", first.Revision)
	if err != nil {
		t.Fatalf("missed live invalidation should return immediately: %v", err)
	}
	if event == nil || event.LiveInvalidated == nil || event.LiveInvalidated.Revision <= first.Revision {
		t.Fatalf("expected wake newer than observed revision %d, got %#v", first.Revision, event)
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

func protocolRowTexts(rows []protocol.CompactRow) []string {
	texts := make([]string, 0, len(rows))
	for _, row := range rows {
		texts = append(texts, cellsText(row.DecodeCells()))
	}
	return texts
}
