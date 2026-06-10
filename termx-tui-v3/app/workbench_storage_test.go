package app

import (
	"context"
	"errors"
	"testing"

	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

func TestWorkbenchStorageReducerLoadsSnapshotFromOpaqueStorage(t *testing.T) {
	shell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-logs", Title: "logs", Kind: state.PaneTerminalLive, TerminalID: "term-logs"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: "pane-logs"})
	storage := &services.FakeWorkbenchStorageService{
		LoadResult: services.WorkbenchStorageLoadResult{
			Snapshot: state.SnapshotWorkbenchForStorage(shell),
			Version:  4,
			Found:    true,
		},
	}
	reducer := NewWorkbenchStorageReducer(WorkbenchDeps{Storage: storage})
	root := state.Root{Shell: state.DefaultShell()}

	root, effects := reducer(root, WorkbenchStorageLoadRequestMsg{})
	if len(effects) != 1 {
		t.Fatalf("expected load effect, got %#v", effects)
	}
	msg := effects[0].(FuncEffect).Run(context.Background())
	root, _ = reducer(root, msg)

	if len(storage.Loads) != 1 || storage.Loads[0].AppID != state.WorkbenchStorageAppID || storage.Loads[0].Key != state.WorkbenchStorageKeyRoot {
		t.Fatalf("unexpected load ref %#v", storage.Loads)
	}
	if root.Shell.ActivePaneID != "pane-logs" || root.Shell.Workspace.Tabs[0].Panes[1].TerminalID != "term-logs" {
		t.Fatalf("expected loaded workbench shell, got %#v", root.Shell)
	}
}

func TestWorkbenchStorageReducerLoadsAndPersistsTerminalViews(t *testing.T) {
	shell := state.DefaultShell().SplitActivePane(state.PaneState{ID: "pane-logs", Title: "logs", Kind: state.PaneTerminalLive, TerminalID: "term-1"}, state.SplitDirectionVertical)
	views := state.TerminalViewStore{}
	views = views.BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-1", 7, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true))
	views = views.BindPane(state.NewPaneTerminalView("pane-logs", "term-1", 8, 40, 12, state.TerminalResizeRoleFollower, "surface", state.TerminalPaneViewID("pane-logs"), false))
	storage := &services.FakeWorkbenchStorageService{LoadResult: services.WorkbenchStorageLoadResult{Snapshot: state.SnapshotRootWorkbenchForStorage(state.Root{Shell: shell, TerminalViews: views}), Version: 4, Found: true}}
	reducer := NewWorkbenchStorageReducer(WorkbenchDeps{Storage: storage})
	root := state.Root{Shell: state.DefaultShell()}

	root, effects := reducer(root, WorkbenchStorageLoadRequestMsg{})
	loadMsg := effects[0].(FuncEffect).Run(context.Background())
	root, _ = reducer(root, loadMsg)
	if bindings := root.TerminalViews.BindingsForTerminal("term-1"); len(bindings) != 2 {
		t.Fatalf("expected loaded terminal view bindings, got %#v", bindings)
	}

	root, effects = reducer(root, WorkbenchStoragePersistRequestMsg{Reason: "test"})
	persistMsg := effects[0].(FuncEffect).Run(context.Background())
	root, _ = reducer(root, persistMsg)
	if len(storage.Saves) != 1 || len(storage.Saves[0].Snapshot.TerminalViews) != 2 || storage.Saves[0].Snapshot.SchemaVersion != state.WorkbenchStorageSchemaV2 {
		t.Fatalf("expected persisted v2 terminal view snapshot, saves=%#v", storage.Saves)
	}
}

func TestWorkbenchCommandPersistsSnapshotThroughStorageReducer(t *testing.T) {
	storage := &services.FakeWorkbenchStorageService{}
	reducer := ComposeReducers(NewShellReducer(), NewWorkbenchStorageReducer(WorkbenchDeps{Storage: storage}))
	root := state.Root{Shell: state.DefaultShell()}

	root, effects := reducer(root, ShellWorkbenchCommandMsg{Command: state.WorkbenchCommand{Action: state.WorkbenchCommandTabCreate, Name: "logs"}})
	if len(effects) != 1 {
		t.Fatalf("expected shell command to emit persist request effect, got %#v", effects)
	}
	persistMsg := effects[0].(FuncEffect).Run(context.Background())
	root, effects = reducer(root, persistMsg)
	if len(effects) != 1 {
		t.Fatalf("expected persist request to emit storage save effect, got %#v", effects)
	}
	resultMsg := effects[0].(FuncEffect).Run(context.Background())
	root, _ = reducer(root, resultMsg)

	if len(storage.Saves) != 1 {
		t.Fatalf("expected one storage save, got %#v", storage.Saves)
	}
	save := storage.Saves[0]
	if save.Ref.AppID != state.WorkbenchStorageAppID || save.Ref.Key != state.WorkbenchStorageKeyRoot || save.Snapshot.Schema != state.WorkbenchStorageSchema {
		t.Fatalf("unexpected save request %#v", save)
	}
	if !save.CheckVersion || save.ExpectedVersion != 0 {
		t.Fatalf("initial persist must use storage CAS at version 0, got %#v", save)
	}
	if len(save.Snapshot.Workspace.Tabs) != 2 || save.Snapshot.Workspace.Tabs[1].Title != "logs" {
		t.Fatalf("storage snapshot must contain updated tab tree, got %#v", save.Snapshot.Workspace.Tabs)
	}
	if len(root.Shell.Toasts) == 0 || root.Shell.Toasts[len(root.Shell.Toasts)-1].Title != "workbench.storage" {
		t.Fatalf("persist result should show feedback, got %#v", root.Shell.Toasts)
	}
	if root.WorkbenchSync.LastSavedVersion != 1 {
		t.Fatalf("persist result should track local saved version, got %#v", root.WorkbenchSync)
	}
}

func TestWorkbenchPaneCRUDPersistsClosedPaneSnapshotWithCAS(t *testing.T) {
	storage := &services.FakeWorkbenchStorageService{CurrentVersion: 3}
	reducer := ComposeReducers(NewShellReducer(), NewWorkbenchStorageReducer(WorkbenchDeps{Storage: storage}))
	root := state.Root{
		Shell: state.DefaultShell().
			SplitActivePane(state.PaneState{ID: "pane-logs", Title: "logs", Kind: state.PaneTerminalLive, TerminalID: "term-logs"}, state.SplitDirectionVertical),
		WorkbenchSync: (state.WorkbenchSyncStore{}).MarkApplied(3),
	}

	root, effects := reducer(root, ShellWorkbenchCommandMsg{Command: state.WorkbenchCommand{
		Action: state.WorkbenchCommandPaneClose,
		Target: state.PaneCommandTarget{PaneID: "pane-logs"},
		Source: state.PaneCommandSourceTest,
	}})
	if len(effects) != 1 {
		t.Fatalf("pane close workbench command should request persist, got %#v", effects)
	}
	persistMsg := effects[0].(FuncEffect).Run(context.Background())
	root, effects = reducer(root, persistMsg)
	if len(effects) != 1 {
		t.Fatalf("persist request should emit storage save, got %#v", effects)
	}
	resultMsg := effects[0].(FuncEffect).Run(context.Background())
	root, _ = reducer(root, resultMsg)

	if len(storage.Saves) != 1 || !storage.Saves[0].CheckVersion || storage.Saves[0].ExpectedVersion != 3 {
		t.Fatalf("pane CRUD persist must use storage CAS, saves=%#v", storage.Saves)
	}
	for _, pane := range storage.Saves[0].Snapshot.Workspace.Tabs[0].Panes {
		if pane.ID == "pane-logs" {
			t.Fatalf("closed pane must not be present in persisted snapshot, snapshot=%#v", storage.Saves[0].Snapshot)
		}
	}
	if root.WorkbenchSync.SaveVersion() != 4 {
		t.Fatalf("successful pane CRUD persist should advance version, sync=%#v", root.WorkbenchSync)
	}
}

func TestWorkbenchCommandPersistsAgainstLoadedStorageVersion(t *testing.T) {
	storage := &services.FakeWorkbenchStorageService{CurrentVersion: 7}
	reducer := ComposeReducers(NewShellReducer(), NewWorkbenchStorageReducer(WorkbenchDeps{Storage: storage}))
	root := state.Root{
		Shell:         state.DefaultShell(),
		WorkbenchSync: (state.WorkbenchSyncStore{}).MarkApplied(7),
	}

	root, effects := reducer(root, ShellWorkbenchCommandMsg{Command: state.WorkbenchCommand{Action: state.WorkbenchCommandTabCreate, Name: "logs"}})
	if len(effects) != 1 {
		t.Fatalf("expected workbench command persist request, got %#v", effects)
	}
	persistMsg := effects[0].(FuncEffect).Run(context.Background())
	root, effects = reducer(root, persistMsg)
	if len(effects) != 1 {
		t.Fatalf("expected storage save effect, got %#v", effects)
	}
	resultMsg := effects[0].(FuncEffect).Run(context.Background())
	root, _ = reducer(root, resultMsg)

	if len(storage.Saves) != 1 || !storage.Saves[0].CheckVersion || storage.Saves[0].ExpectedVersion != 7 {
		t.Fatalf("persist must use loaded base version, saves=%#v", storage.Saves)
	}
	if root.WorkbenchSync.LastSavedVersion != 8 || root.WorkbenchSync.SaveVersion() != 8 {
		t.Fatalf("successful save should advance base version, got %#v", root.WorkbenchSync)
	}
}

func TestWorkbenchStorageChangedReloadsExternalSnapshot(t *testing.T) {
	externalShell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-external", Title: "external", Kind: state.PaneTerminalLive, TerminalID: "term-external"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: "pane-external"})
	storage := &services.FakeWorkbenchStorageService{
		LoadResult: services.WorkbenchStorageLoadResult{
			Snapshot: state.SnapshotWorkbenchForStorage(externalShell),
			Version:  8,
			Found:    true,
		},
	}
	reducer := NewWorkbenchStorageReducer(WorkbenchDeps{Storage: storage})
	root := state.Root{Shell: state.DefaultShell()}

	root, effects := reducer(root, WorkbenchStorageChangedMsg{Event: services.WorkbenchStorageEvent{
		Ref:     state.DefaultWorkbenchStorageRef("").WithVersion(8),
		Version: 8,
		Op:      "put",
	}})
	if len(effects) != 1 || root.WorkbenchSync.LastEventVersion != 8 {
		t.Fatalf("storage change should request reload, root=%#v effects=%#v", root, effects)
	}
	loadRequest := effects[0].(FuncEffect).Run(context.Background())
	root, effects = reducer(root, loadRequest)
	if len(effects) != 1 {
		t.Fatalf("load request should emit storage load effect, got %#v", effects)
	}
	loadResult := effects[0].(FuncEffect).Run(context.Background())
	root, _ = reducer(root, loadResult)

	if root.Shell.ActivePaneID != "pane-external" || root.WorkbenchSync.LastAppliedVersion != 8 {
		t.Fatalf("external snapshot should refresh shell, root=%#v", root)
	}
}

func TestWorkbenchStorageConflictReloadsLatestSnapshot(t *testing.T) {
	remoteShell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-remote", Title: "remote", Kind: state.PaneTerminalLive, TerminalID: "term-remote"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: "pane-remote"})
	storage := &services.FakeWorkbenchStorageService{
		CurrentVersion: 9,
		LoadResult: services.WorkbenchStorageLoadResult{
			Snapshot: state.SnapshotWorkbenchForStorage(remoteShell),
			Version:  9,
			Found:    true,
		},
	}
	reducer := NewWorkbenchStorageReducer(WorkbenchDeps{Storage: storage})
	root := state.Root{
		Shell:         state.DefaultShell(),
		WorkbenchSync: (state.WorkbenchSyncStore{}).MarkApplied(8),
	}

	root, effects := reducer(root, WorkbenchStoragePersistRequestMsg{Reason: "tab.create"})
	if len(effects) != 1 {
		t.Fatalf("persist request should emit save effect, got %#v", effects)
	}
	persistResult := effects[0].(FuncEffect).Run(context.Background())
	root, effects = reducer(root, persistResult)
	if len(effects) != 1 || !root.WorkbenchSync.Conflict || root.WorkbenchSync.ConflictVersion != 8 {
		t.Fatalf("conflict should mark state and request reload, root=%#v effects=%#v", root, effects)
	}
	if len(root.Shell.Toasts) == 0 || root.Shell.Toasts[len(root.Shell.Toasts)-1].Body != "conflict: reloading" {
		t.Fatalf("conflict should show reload feedback, toasts=%#v", root.Shell.Toasts)
	}
	loadRequest := effects[0].(FuncEffect).Run(context.Background())
	root, effects = reducer(root, loadRequest)
	if len(effects) != 1 {
		t.Fatalf("reload request should emit load effect, got %#v", effects)
	}
	loadResult := effects[0].(FuncEffect).Run(context.Background())
	root, _ = reducer(root, loadResult)

	if root.Shell.ActivePaneID != "pane-remote" || root.WorkbenchSync.Conflict || root.WorkbenchSync.SaveVersion() != 9 {
		t.Fatalf("conflict reload should apply latest remote snapshot, root=%#v", root)
	}
}

func TestWorkbenchStorageChangedIgnoresSelfPersistVersion(t *testing.T) {
	reducer := NewWorkbenchStorageReducer(WorkbenchDeps{Storage: &services.FakeWorkbenchStorageService{}})
	root := state.Root{
		Shell:         state.DefaultShell(),
		WorkbenchSync: (state.WorkbenchSyncStore{}).MarkSaved(state.DefaultWorkbenchStorageRef("").WithVersion(5), 5),
	}

	root, effects := reducer(root, WorkbenchStorageChangedMsg{Event: services.WorkbenchStorageEvent{
		Ref:     state.DefaultWorkbenchStorageRef("").WithVersion(5),
		Version: 5,
		Op:      "put",
	}})
	if len(effects) != 0 || root.WorkbenchSync.LastEventVersion != 5 || root.WorkbenchSync.LastAppliedVersion != 0 {
		t.Fatalf("self storage event should not reload, root=%#v effects=%#v", root, effects)
	}
}

func TestWorkbenchStorageReducerReportsMissingServiceAndSaveErrors(t *testing.T) {
	reducer := NewWorkbenchStorageReducer(WorkbenchDeps{})
	root, effects := reducer(state.Root{Shell: state.DefaultShell()}, WorkbenchStoragePersistRequestMsg{Reason: "tab.create"})
	if len(effects) != 0 || len(root.Shell.Toasts) == 0 || root.Shell.Toasts[len(root.Shell.Toasts)-1].Body != "storage service missing" {
		t.Fatalf("missing storage should be visible feedback, root=%#v effects=%#v", root, effects)
	}

	storage := &services.FakeWorkbenchStorageService{SaveErr: errors.New("version conflict")}
	reducer = NewWorkbenchStorageReducer(WorkbenchDeps{Storage: storage})
	root = state.Root{Shell: state.DefaultShell()}
	root, effects = reducer(root, WorkbenchStoragePersistRequestMsg{Reason: "tab.create"})
	msg := effects[0].(FuncEffect).Run(context.Background())
	root, _ = reducer(root, msg)
	if len(root.Shell.Toasts) == 0 || root.Shell.Toasts[len(root.Shell.Toasts)-1].Body != "version conflict" {
		t.Fatalf("save error should be visible feedback, got %#v", root.Shell.Toasts)
	}
}

func TestInteractiveRuntimeWithWorkbenchPersistsWorkbenchCommand(t *testing.T) {
	host := NewFakeTerminalHost(8)
	watchCh := make(chan services.WorkbenchStorageEvent)
	close(watchCh)
	storage := &services.FakeWorkbenchStorageService{WatchCh: watchCh}
	runtime := NewInteractiveRuntimeWithWorkbench(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: &services.FakeTerminalService{}},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
		WorkbenchDeps{Storage: storage},
	)

	if err := runtime.Post(ShellWorkbenchCommandMsg{Command: state.WorkbenchCommand{Action: state.WorkbenchCommandTabCreate, Name: "logs"}}); err != nil {
		t.Fatalf("post workbench command: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if len(storage.Saves) != 1 || len(storage.Saves[0].Snapshot.Workspace.Tabs) != 2 {
		t.Fatalf("interactive runtime should persist workbench command, saves=%#v", storage.Saves)
	}
	if len(storage.Watches) != 1 || storage.Watches[0].Key != state.WorkbenchStorageKeyRoot {
		t.Fatalf("interactive runtime should subscribe to storage.changed, watches=%#v", storage.Watches)
	}
}
