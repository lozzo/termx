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

func TestWorkbenchStorageLoadInvalidatesFrozenHistoryAndCopyMode(t *testing.T) {
	shell := state.DefaultShell()
	shell.Workspace.Tabs[0].Panes[0].Kind = state.PaneTerminalLive
	shell.Workspace.Tabs[0].Panes[0].TerminalID = "term-new"
	views := state.TerminalViewStore{}.
		BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-new", 11, 80, 24, state.TerminalResizeRoleFollower, "surface-new", state.TerminalPaneViewID(state.DefaultPaneID), false))
	reducer := NewWorkbenchStorageReducer(WorkbenchDeps{Storage: &services.FakeWorkbenchStorageService{}})
	root := state.Root{
		Shell: state.DefaultShell().BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-old"),
		TerminalViews: state.TerminalViewStore{}.
			BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-old", 7, 80, 24, state.TerminalResizeRoleOwner, "surface-old", state.TerminalPaneViewID(state.DefaultPaneID), true)),
		History: state.HistoryStore{
			PaneID:     state.DefaultPaneID,
			ViewID:     state.TerminalPaneViewID(state.DefaultPaneID),
			TerminalID: "term-old",
			Token:      "tok-old",
			Cols:       80,
			Rows:       []state.HistoryRow{{Text: "old-history", LineID: 1}},
		},
		CopyMode: state.CopyModeStore{
			Active:     true,
			PaneID:     state.DefaultPaneID,
			ViewID:     state.TerminalPaneViewID(state.DefaultPaneID),
			TerminalID: "term-old",
			BoundToken: "tok-old",
			BoundCols:  80,
			ViewRows:   20,
		},
	}

	root, effects := reducer(root, WorkbenchStorageLoadResultMsg{Result: services.WorkbenchStorageLoadResult{
		Snapshot: state.SnapshotRootWorkbenchForStorage(state.Root{Shell: shell, TerminalViews: views}),
		Version:  9,
		Found:    true,
	}})

	if root.CopyMode.Active || root.CopyMode.TerminalID != "" || root.CopyMode.BoundToken != "" {
		t.Fatalf("workbench load must clear stale copy mode binding, got %#v", root.CopyMode)
	}
	if root.History.TerminalID != "term-old" || root.History.Token != "" || len(root.History.Rows) != 0 || root.History.Pending != nil {
		t.Fatalf("workbench load must invalidate stale frozen history window, got %#v", root.History)
	}
	if root.Shell.Workspace.Tabs[0].Panes[0].TerminalID != "term-new" {
		t.Fatalf("workbench load should still apply external shell snapshot, got %#v", root.Shell)
	}
	if len(effects) != 2 {
		t.Fatalf("restored terminal view should still request list+attach effects, got %#v", effects)
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

func TestInteractiveRuntimeWithWorkbenchPersistsFloatingCommand(t *testing.T) {
	host := NewFakeTerminalHost(8)
	watchCh := make(chan services.WorkbenchStorageEvent)
	close(watchCh)
	storage := &services.FakeWorkbenchStorageService{WatchCh: watchCh}
	root := state.Root{Shell: state.DefaultShell()}
	root.Shell, _ = root.Shell.ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "floating-1",
		Pane:     state.PaneState{ID: "floating-pane-1", Title: "floating", Kind: state.PaneEmpty},
		Rect:     state.FloatingRect{X: 2, Y: 2, W: 30, H: 8},
		BoundsW:  80,
		BoundsH:  24,
	})

	runtime := NewInteractiveRuntimeWithWorkbench(
		root,
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: &services.FakeTerminalService{}},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
		WorkbenchDeps{Storage: storage},
	)

	if err := runtime.Post(ShellFloatingCommandMsg{Command: state.FloatingCommand{Action: state.FloatingCommandCollapseAll, Source: state.PaneCommandSourceTest}}); err != nil {
		t.Fatalf("post floating command: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if len(storage.Saves) != 1 {
		t.Fatalf("floating command should persist workbench snapshot, saves=%#v", storage.Saves)
	}
	if len(storage.Saves[0].Snapshot.Floatings) != 1 || !storage.Saves[0].Snapshot.Floatings[0].Collapsed {
		t.Fatalf("persisted floating snapshot should include collapsed state, snapshot=%#v", storage.Saves[0].Snapshot.Floatings)
	}
}

func TestInteractiveRuntimeFloatingAutoFitRefreshDoesNotPersist(t *testing.T) {
	host := NewFakeTerminalHost(8)
	watchCh := make(chan services.WorkbenchStorageEvent)
	close(watchCh)
	storage := &services.FakeWorkbenchStorageService{WatchCh: watchCh}
	root := state.Root{
		Shell:    state.DefaultShell(),
		Viewport: state.ViewportStore{Valid: true, Cols: 100, Rows: 30},
		Surface:  state.TerminalSurfaceStore{Surfaces: map[string]state.LiveSurfaceSnapshot{"term-1": {TerminalID: "term-1", Cols: 40, Rows: 12, State: state.TerminalLiveAttached}}},
	}
	root.Shell, _ = root.Shell.ApplyFloatingCommand(state.FloatingCommand{
		Action:   state.FloatingCommandCreate,
		TargetID: "floating-1",
		Pane:     state.PaneState{ID: "floating-pane-1", Title: "floating", Kind: state.PaneTerminalLive, TerminalID: "term-1"},
		Rect:     state.FloatingRect{X: 2, Y: 2, W: 20, H: 8},
		BoundsW:  100,
		BoundsH:  30,
	})
	root.TerminalViews = root.TerminalViews.BindFloating(state.NewFloatingTerminalView("floating-1", "floating-pane-1", "term-1", 7, 40, 12, state.TerminalResizeRoleOwner, "surface", state.TerminalFloatingViewID("floating-1"), true))

	runtime := NewInteractiveRuntimeWithWorkbench(
		root,
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: &services.FakeTerminalService{}},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
		WorkbenchDeps{Storage: storage},
	)

	if err := runtime.Post(ShellFloatingCommandMsg{Command: state.FloatingCommand{Action: state.FloatingCommandToggleAutoFit, TargetID: "floating-1", Source: state.PaneCommandSourceTest}}); err != nil {
		t.Fatalf("enable auto-fit: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain enable auto-fit: %v", err)
	}
	storage.Saves = nil

	if err := runtime.Post(LiveSurfaceMsg{Snapshot: state.LiveSurfaceSnapshot{TerminalID: "term-1", Cols: 60, Rows: 20, State: state.TerminalLiveAttached}}); err != nil {
		t.Fatalf("post live surface update: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain live surface update: %v", err)
	}

	floating := runtime.State().Shell.EnsureDefaults().Floatings[0]
	if floating.AutoFit.Cols != 60 || floating.AutoFit.Rows != 20 || floating.Rect.W != 62 || floating.Rect.H != 22 {
		t.Fatalf("auto-fit refresh should update floating geometry from live size, got %#v", floating)
	}
	if len(storage.Saves) != 0 {
		t.Fatalf("auto-fit refresh should not persist workbench snapshot, saves=%#v", storage.Saves)
	}
}

func TestInteractiveRuntimeWithWorkbenchLoadsSnapshotBeforeWatch(t *testing.T) {
	host := NewFakeTerminalHost(8)
	watchCh := make(chan services.WorkbenchStorageEvent)
	close(watchCh)
	shell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-restored", Title: "restored", Kind: state.PaneTerminalLive, TerminalID: "term-restored"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: "pane-restored"})
	views := state.TerminalViewStore{}.
		BindPane(state.NewPaneTerminalView("pane-restored", "term-restored", 11, 80, 24, state.TerminalResizeRoleOwner, "surface-restored", state.TerminalPaneViewID("pane-restored"), true))
	storage := &services.FakeWorkbenchStorageService{
		LoadResult: services.WorkbenchStorageLoadResult{
			Snapshot: state.SnapshotRootWorkbenchForStorage(state.Root{Shell: shell, TerminalViews: views}),
			Version:  7,
			Found:    true,
		},
		WatchCh: watchCh,
	}
	runtime := NewInteractiveRuntimeWithWorkbench(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: &services.FakeTerminalService{}},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
		WorkbenchDeps{Storage: storage},
	)

	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	root := runtime.State()
	if len(storage.Loads) != 1 || len(storage.Watches) != 1 {
		t.Fatalf("interactive runtime should load once and watch once, loads=%#v watches=%#v", storage.Loads, storage.Watches)
	}
	if root.Shell.ActivePaneID != "pane-restored" || root.WorkbenchSync.SaveVersion() != 7 {
		t.Fatalf("expected restored workbench snapshot, root=%#v", root)
	}
	if binding, ok := root.TerminalViews.PaneBinding("pane-restored"); !ok || binding.TerminalID != "term-restored" || binding.ViewID == "" {
		t.Fatalf("expected restored terminal view binding, binding=%#v ok=%v", binding, ok)
	}
}

func TestInteractiveRuntimeWorkbenchRestoreReattachesTerminalViewsFromCore(t *testing.T) {
	host := NewFakeTerminalHost(8)
	host.SetSize(120, 40)
	watchCh := make(chan services.WorkbenchStorageEvent)
	close(watchCh)
	shell := state.DefaultShell().
		SplitActivePane(state.PaneState{ID: "pane-restored", Title: "restored", Kind: state.PaneTerminalLive, TerminalID: "term-restored"}, state.SplitDirectionVertical).
		FocusPane(state.PaneCommandTarget{PaneID: "pane-restored"})
	views := state.TerminalViewStore{}.
		BindPane(state.NewPaneTerminalView("pane-restored", "term-restored", 11, 80, 24, state.TerminalResizeRoleOwner, "surface-restored", state.TerminalPaneViewID("pane-restored"), true))
	storage := &services.FakeWorkbenchStorageService{LoadResult: services.WorkbenchStorageLoadResult{Snapshot: state.SnapshotRootWorkbenchForStorage(state.Root{Shell: shell, TerminalViews: views}), Version: 7, Found: true}, WatchCh: watchCh}
	terminal := &services.FakeTerminalService{
		AttachResult: services.TerminalAttachResult{Channel: 42, Cols: 100, Rows: 30, ResizePolicy: state.TerminalResizeRoleFollower, CanResize: false, OwnerViewID: "other-view"},
		SurfaceResult: services.TerminalSurfaceResult{Ready: true, Snapshot: state.LiveSurfaceSnapshot{TerminalID: "term-restored", Cols: 100, Rows: 30, Lines: []string{"changed by another tui"}, State: state.TerminalLiveAttached}},
	}
	runtime := NewInteractiveRuntimeWithWorkbench(state.Root{}, host, NewSyncEffectRunner(), LiveDeps{Terminal: terminal}, CopyModeDeps{Core: &services.FakeCoreClient{}}, WorkbenchDeps{Storage: storage})

	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if len(terminal.Attaches) != 1 {
		t.Fatalf("restored terminal view should reattach through core, attaches=%#v", terminal.Attaches)
	}
	attach := terminal.Attaches[0]
	if attach.TerminalID != "term-restored" || attach.ViewID != state.TerminalPaneViewID("pane-restored") || attach.ResizePolicy != state.TerminalResizeRoleFollower {
		t.Fatalf("restore attach must use restored view as follower, attach=%#v", attach)
	}
	root := runtime.State()
	binding, ok := root.TerminalViews.PaneBinding("pane-restored")
	if !ok || binding.Channel != 42 || binding.ResizeRole != state.TerminalResizeRoleFollower || binding.OwnerViewID != "other-view" || !binding.Attached {
		t.Fatalf("restored binding should reflect core attach result, binding=%#v ok=%v", binding, ok)
	}
	if channel, ok := root.Session.InputChannelFor("term-restored"); !ok || channel != 42 {
		t.Fatalf("restored attach should refresh input channel, channel=%d ok=%v", channel, ok)
	}
	if len(terminal.Surfaces) != 1 || root.Surface.TerminalID != "term-restored" || root.Surface.Lines[0] != "changed by another tui" {
		t.Fatalf("restore should load authoritative live surface, surfaces=%#v surface=%#v", terminal.Surfaces, root.Surface)
	}
}

func TestInteractiveRuntimeWorkbenchRestoreShowsExitedTerminalFromCore(t *testing.T) {
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	watchCh := make(chan services.WorkbenchStorageEvent)
	close(watchCh)
	shell := state.DefaultShell()
	shell.Workspace.Tabs[0].Panes[0].Kind = state.PaneTerminalLive
	shell.Workspace.Tabs[0].Panes[0].TerminalID = "term-exited"
	views := state.TerminalViewStore{}.
		BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-exited", 9, 80, 24, state.TerminalResizeRoleOwner, "surface", state.TerminalPaneViewID(state.DefaultPaneID), true))
	storage := &services.FakeWorkbenchStorageService{LoadResult: services.WorkbenchStorageLoadResult{Snapshot: state.SnapshotRootWorkbenchForStorage(state.Root{Shell: shell, TerminalViews: views}), Version: 7, Found: true}, WatchCh: watchCh}
	terminal := &services.FakeTerminalService{
		AttachResult:  services.TerminalAttachResult{Channel: 12, Cols: 80, Rows: 24, ResizePolicy: state.TerminalResizeRoleFollower},
		SurfaceResult: services.TerminalSurfaceResult{Ready: true, Snapshot: state.LiveSurfaceSnapshot{TerminalID: "term-exited", Cols: 80, Rows: 24, State: state.TerminalLiveExited, ExitCode: 130, ExitReason: "process exited"}},
	}
	runtime := NewInteractiveRuntimeWithWorkbench(state.Root{}, host, NewSyncEffectRunner(), LiveDeps{Terminal: terminal}, CopyModeDeps{Core: &services.FakeCoreClient{}}, WorkbenchDeps{Storage: storage})

	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	root := runtime.State()
	if root.Surface.TerminalID != "term-exited" || root.Surface.State != state.TerminalLiveExited || root.Surface.ExitCode != 130 {
		t.Fatalf("restored exited terminal should remain visible, surface=%#v", root.Surface)
	}
	if pane, ok := root.Shell.Pane(state.PaneCommandTarget{PaneID: state.DefaultPaneID}); !ok || pane.TerminalID != "term-exited" {
		t.Fatalf("exited terminal pane must stay bound, pane=%#v ok=%v", pane, ok)
	}
}

func TestInteractiveRuntimeStartupLoadsTerminalPoolTitleAfterWorkbenchRestore(t *testing.T) {
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	watchCh := make(chan services.WorkbenchStorageEvent)
	close(watchCh)
	shell := state.DefaultShell()
	shell.Workspace.Tabs[0].Panes[0].Title = "shell"
	shell.Workspace.Tabs[0].Panes[0].Kind = state.PaneTerminalLive
	shell.Workspace.Tabs[0].Panes[0].TerminalID = "term-main"
	views := state.TerminalViewStore{}.
		BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-main", 11, 80, 24, state.TerminalResizeRoleOwner, "surface-main", state.TerminalPaneViewID(state.DefaultPaneID), true))
	storage := &services.FakeWorkbenchStorageService{
		LoadResult: services.WorkbenchStorageLoadResult{
			Snapshot: state.SnapshotRootWorkbenchForStorage(state.Root{Shell: shell, TerminalViews: views}),
			Version:  7,
			Found:    true,
		},
		WatchCh: watchCh,
	}
	terminal := &services.FakeTerminalService{ListResult: services.TerminalListResult{Items: []services.TerminalPoolItem{{TerminalID: "term-main", Title: "main", State: "running"}}}}
	runtime := NewInteractiveRuntimeWithWorkbench(
		state.Root{},
		host,
		NewSyncEffectRunner(),
		LiveDeps{Terminal: terminal},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
		WorkbenchDeps{Storage: storage},
	)

	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if len(terminal.Lists) != 1 {
		t.Fatalf("startup should load terminal pool once, lists=%#v", terminal.Lists)
	}
	frame := lastFrame(t, host.Frames())
	if frameContains(frame, "[󰍀] shell") || !frameContains(frame, "[󰍀] main") {
		t.Fatalf("restored terminal chrome should use terminal title after startup list, frame=%#v", frame.Lines)
	}
}

func TestInteractiveRuntimeWorkbenchReloadDoesNotKeepOldFrozenHistory(t *testing.T) {
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	runtime := NewInteractiveRuntimeWithWorkbench(
		state.Root{
			Shell: state.DefaultShell().BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-old"),
			TerminalViews: state.TerminalViewStore{}.
				BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-old", 7, 80, 24, state.TerminalResizeRoleOwner, "surface-old", state.TerminalPaneViewID(state.DefaultPaneID), true)),
			History: state.HistoryStore{
				PaneID:     state.DefaultPaneID,
				ViewID:     state.TerminalPaneViewID(state.DefaultPaneID),
				TerminalID: "term-old",
				Token:      "tok-old",
				Cols:       80,
				Rows:       []state.HistoryRow{{Text: "old-history", LineID: 1}},
			},
			CopyMode: state.CopyModeStore{
				Active:     true,
				PaneID:     state.DefaultPaneID,
				ViewID:     state.TerminalPaneViewID(state.DefaultPaneID),
				TerminalID: "term-old",
				BoundToken: "tok-old",
				BoundCols:  80,
				ViewRows:   20,
			},
		},
		host,
		NewSyncEffectRunner(),
		LiveDeps{},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
		WorkbenchDeps{},
	)

	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain initial frozen history: %v", err)
	}
	if !frameContains(lastFrame(t, host.Frames()), "old-history") {
		t.Fatalf("expected initial frame to render old frozen history, frames=%#v", host.Frames())
	}

	shell := state.DefaultShell()
	shell.Workspace.Tabs[0].Panes[0].Kind = state.PaneTerminalLive
	shell.Workspace.Tabs[0].Panes[0].TerminalID = "term-new"
	views := state.TerminalViewStore{}.
		BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-new", 11, 80, 24, state.TerminalResizeRoleFollower, "surface-new", state.TerminalPaneViewID(state.DefaultPaneID), false))
	if err := runtime.Post(WorkbenchStorageLoadResultMsg{Result: services.WorkbenchStorageLoadResult{
		Snapshot: state.SnapshotRootWorkbenchForStorage(state.Root{Shell: shell, TerminalViews: views}),
		Version:  9,
		Found:    true,
	}}); err != nil {
		t.Fatalf("post workbench load result: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain workbench load result: %v", err)
	}

	if runtime.State().CopyMode.Active {
		t.Fatalf("workbench load must exit stale copy mode, got %#v", runtime.State().CopyMode)
	}
	if runtime.State().History.TerminalID != "term-old" || runtime.State().History.Token != "" {
		t.Fatalf("workbench load must invalidate stale frozen history window, got %#v", runtime.State().History)
	}
	last := lastFrame(t, host.Frames())
	if frameContains(last, "old-history") {
		t.Fatalf("workbench load must not keep rendering old frozen history, got %#v", last.Lines)
	}
}

func TestInteractiveRuntimeWorkbenchReloadIgnoresDelayedOldHistoryWindow(t *testing.T) {
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	pendingHistory, err := (state.HistoryStore{}).BeginLatest(state.HistoryPendingRequest{
		ID:         7,
		PaneID:     state.DefaultPaneID,
		ViewID:     state.TerminalPaneViewID(state.DefaultPaneID),
		TerminalID: "term-old",
		Cols:       80,
	})
	if err != nil {
		t.Fatalf("begin latest pending: %v", err)
	}
	runtime := NewInteractiveRuntimeWithWorkbench(
		state.Root{
			Shell: state.DefaultShell().BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-old"),
			TerminalViews: state.TerminalViewStore{}.
				BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-old", 7, 80, 24, state.TerminalResizeRoleOwner, "surface-old", state.TerminalPaneViewID(state.DefaultPaneID), true)),
			History: pendingHistory,
			CopyMode: state.CopyModeStore{}.BindLatest(
				state.DefaultPaneID,
				state.TerminalPaneViewID(state.DefaultPaneID),
				"term-old",
				7,
				80,
				20,
			),
		},
		host,
		NewSyncEffectRunner(),
		LiveDeps{},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
		WorkbenchDeps{},
	)

	shell := state.DefaultShell()
	shell.Workspace.Tabs[0].Panes[0].Kind = state.PaneTerminalLive
	shell.Workspace.Tabs[0].Panes[0].TerminalID = "term-new"
	views := state.TerminalViewStore{}.
		BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-new", 11, 80, 24, state.TerminalResizeRoleFollower, "surface-new", state.TerminalPaneViewID(state.DefaultPaneID), false))
	if err := runtime.Post(WorkbenchStorageLoadResultMsg{Result: services.WorkbenchStorageLoadResult{
		Snapshot: state.SnapshotRootWorkbenchForStorage(state.Root{Shell: shell, TerminalViews: views}),
		Version:  9,
		Found:    true,
	}}); err != nil {
		t.Fatalf("post workbench load result: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain workbench load result: %v", err)
	}

	delayed := state.HistoryWindow{
		TerminalID: "term-old",
		PaneID:     state.DefaultPaneID,
		ViewID:     state.TerminalPaneViewID(state.DefaultPaneID),
		Token:      "tok-delayed",
		Cols:       80,
		Op:         state.HistoryWindowReplace,
		Rows:       []state.HistoryRow{{Text: "delayed-old-history", LineID: 8}},
	}
	if err := runtime.Post(CopyModeHistoryResultMsg{Result: services.HistoryResult{RequestID: 7, Window: delayed}}); err != nil {
		t.Fatalf("post delayed old history window: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain delayed old history window: %v", err)
	}

	if runtime.State().History.Token != "" || len(runtime.State().History.Rows) != 0 {
		t.Fatalf("workbench reload must ignore delayed old history window, got %#v", runtime.State().History)
	}
	if runtime.State().CopyMode.Active {
		t.Fatalf("workbench reload must keep copy mode inactive after delayed old history window, got %#v", runtime.State().CopyMode)
	}
	last := lastFrame(t, host.Frames())
	if frameContains(last, "delayed-old-history") {
		t.Fatalf("workbench reload must not render delayed old history window, got %#v", last.Lines)
	}
}

func TestInteractiveRuntimeWorkbenchReloadIgnoresDelayedOldHistoryError(t *testing.T) {
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	pendingHistory, err := (state.HistoryStore{}).BeginLatest(state.HistoryPendingRequest{
		ID:         7,
		PaneID:     state.DefaultPaneID,
		ViewID:     state.TerminalPaneViewID(state.DefaultPaneID),
		TerminalID: "term-old",
		Cols:       80,
	})
	if err != nil {
		t.Fatalf("begin latest pending: %v", err)
	}
	runtime := NewInteractiveRuntimeWithWorkbench(
		state.Root{
			Shell: state.DefaultShell().BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-old"),
			TerminalViews: state.TerminalViewStore{}.
				BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-old", 7, 80, 24, state.TerminalResizeRoleOwner, "surface-old", state.TerminalPaneViewID(state.DefaultPaneID), true)),
			History: pendingHistory,
			CopyMode: state.CopyModeStore{}.BindLatest(
				state.DefaultPaneID,
				state.TerminalPaneViewID(state.DefaultPaneID),
				"term-old",
				7,
				80,
				20,
			),
		},
		host,
		NewSyncEffectRunner(),
		LiveDeps{},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
		WorkbenchDeps{},
	)

	shell := state.DefaultShell()
	shell.Workspace.Tabs[0].Panes[0].Kind = state.PaneTerminalLive
	shell.Workspace.Tabs[0].Panes[0].TerminalID = "term-new"
	views := state.TerminalViewStore{}.
		BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-new", 11, 80, 24, state.TerminalResizeRoleFollower, "surface-new", state.TerminalPaneViewID(state.DefaultPaneID), false))
	if err := runtime.Post(WorkbenchStorageLoadResultMsg{Result: services.WorkbenchStorageLoadResult{
		Snapshot: state.SnapshotRootWorkbenchForStorage(state.Root{Shell: shell, TerminalViews: views}),
		Version:  9,
		Found:    true,
	}}); err != nil {
		t.Fatalf("post workbench load result: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain workbench load result: %v", err)
	}
	beforeSurfaceErr := runtime.State().Surface.Err
	beforeSessionErr := runtime.State().Session.LastError

	if err := runtime.Post(CopyModeHistoryResultMsg{
		Result: services.HistoryResult{RequestID: 7},
		Err:    errors.New("delayed old history failed"),
	}); err != nil {
		t.Fatalf("post delayed old history error: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain delayed old history error: %v", err)
	}

	if runtime.State().Surface.Err != beforeSurfaceErr || runtime.State().Session.LastError != beforeSessionErr {
		t.Fatalf("workbench reload must ignore delayed old history error without replacing current ui error state, state=%#v", runtime.State())
	}
	last := lastFrame(t, host.Frames())
	if frameContains(last, "delayed old history failed") {
		t.Fatalf("workbench reload must not render delayed old history error, got %#v", last.Lines)
	}
}

func TestInteractiveRuntimeWorkbenchReloadIgnoresDelayedOldAttachResult(t *testing.T) {
	host := NewFakeTerminalHost(8)
	host.SetSize(80, 24)
	runtime := NewInteractiveRuntimeWithWorkbench(
		state.Root{
			Shell: state.DefaultShell().BindPaneTerminal(state.PaneCommandTarget{PaneID: state.DefaultPaneID}, "term-old"),
			TerminalViews: state.TerminalViewStore{}.
				BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-old", 7, 80, 24, state.TerminalResizeRoleOwner, "surface-old", state.TerminalPaneViewID(state.DefaultPaneID), true)),
		},
		host,
		NewSyncEffectRunner(),
		LiveDeps{},
		CopyModeDeps{Core: &services.FakeCoreClient{}},
		WorkbenchDeps{},
	)

	shell := state.DefaultShell()
	shell.Workspace.Tabs[0].Panes[0].Kind = state.PaneTerminalLive
	shell.Workspace.Tabs[0].Panes[0].TerminalID = "term-new"
	views := state.TerminalViewStore{}.
		BindPane(state.NewPaneTerminalView(state.DefaultPaneID, "term-new", 11, 80, 24, state.TerminalResizeRoleFollower, "surface-new", state.TerminalPaneViewID(state.DefaultPaneID), false))
	if err := runtime.Post(WorkbenchStorageLoadResultMsg{Result: services.WorkbenchStorageLoadResult{
		Snapshot: state.SnapshotRootWorkbenchForStorage(state.Root{Shell: shell, TerminalViews: views}),
		Version:  9,
		Found:    true,
	}}); err != nil {
		t.Fatalf("post workbench load result: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain workbench load result: %v", err)
	}

	if err := runtime.Post(LiveAttachResultMsg{Result: services.TerminalAttachResult{
		TerminalID:   "term-old",
		Channel:      99,
		Cols:         100,
		Rows:         30,
		ResizePolicy: state.TerminalResizeRoleOwner,
		SurfaceID:    "surface-old",
		ViewID:       "pane:stale-old",
		CanResize:    true,
	}}); err != nil {
		t.Fatalf("post delayed old attach result: %v", err)
	}
	if err := runtime.Drain(context.Background()); err != nil {
		t.Fatalf("drain delayed old attach result: %v", err)
	}

	binding, ok := runtime.State().TerminalViews.PaneBinding(state.DefaultPaneID)
	if !ok || binding.TerminalID != "term-new" || binding.Channel != 11 || binding.ViewID != state.TerminalPaneViewID(state.DefaultPaneID) {
		t.Fatalf("workbench reload must ignore delayed old attach result without rebinding active pane, binding=%#v ok=%v", binding, ok)
	}
	if runtime.State().Shell.Workspace.Tabs[0].Panes[0].TerminalID != "term-new" {
		t.Fatalf("workbench reload must keep reloaded pane terminal, shell=%#v", runtime.State().Shell)
	}
	if channel, ok := runtime.State().Session.InputChannelFor("term-new"); ok && channel == 99 {
		t.Fatalf("workbench reload must not move delayed old attach channel onto reloaded session, session=%#v", runtime.State().Session)
	}
}
