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
	if len(save.Snapshot.Workspace.Tabs) != 2 || save.Snapshot.Workspace.Tabs[1].Title != "logs" {
		t.Fatalf("storage snapshot must contain updated tab tree, got %#v", save.Snapshot.Workspace.Tabs)
	}
	if len(root.Shell.Toasts) == 0 || root.Shell.Toasts[len(root.Shell.Toasts)-1].Title != "workbench.storage" {
		t.Fatalf("persist result should show feedback, got %#v", root.Shell.Toasts)
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
	storage := &services.FakeWorkbenchStorageService{}
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
}
