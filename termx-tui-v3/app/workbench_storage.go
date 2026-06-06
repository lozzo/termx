package app

import (
	"context"

	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type WorkbenchDeps struct {
	Storage services.WorkbenchStorageService
	Ref     state.WorkbenchStorageRef
}

type WorkbenchStorageLoadRequestMsg struct{}

func (WorkbenchStorageLoadRequestMsg) isMsg() {}

type WorkbenchStorageLoadResultMsg struct {
	Result services.WorkbenchStorageLoadResult
	Err    error
}

func (WorkbenchStorageLoadResultMsg) isMsg() {}

type WorkbenchStoragePersistRequestMsg struct {
	Reason string
}

func (WorkbenchStoragePersistRequestMsg) isMsg() {}

type WorkbenchStoragePersistResultMsg struct {
	Result services.WorkbenchStorageSaveResult
	Err    error
}

func (WorkbenchStoragePersistResultMsg) isMsg() {}

func NewWorkbenchStorageReducer(deps WorkbenchDeps) Reducer {
	return func(root state.Root, msg Msg) (state.Root, []Effect) {
		switch msg := msg.(type) {
		case WorkbenchStorageLoadRequestMsg:
			return reduceWorkbenchStorageLoadRequest(root, deps)
		case WorkbenchStorageLoadResultMsg:
			return reduceWorkbenchStorageLoadResult(root, msg)
		case WorkbenchStoragePersistRequestMsg:
			return reduceWorkbenchStoragePersistRequest(root, msg, deps)
		case WorkbenchStoragePersistResultMsg:
			return reduceWorkbenchStoragePersistResult(root, msg)
		default:
			return root, nil
		}
	}
}

func reduceWorkbenchStorageLoadRequest(root state.Root, deps WorkbenchDeps) (state.Root, []Effect) {
	if deps.Storage == nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.storage", Body: "storage service missing"})
		return root.Advance(), nil
	}
	ref := workbenchStorageRef(root, deps)
	return root, []Effect{FuncEffect{Run: func(ctx context.Context) Msg {
		result, err := deps.Storage.LoadWorkbench(ctx, ref)
		return WorkbenchStorageLoadResultMsg{Result: result, Err: err}
	}}}
}

func reduceWorkbenchStorageLoadResult(root state.Root, msg WorkbenchStorageLoadResultMsg) (state.Root, []Effect) {
	if msg.Err != nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.storage", Body: errorString(msg.Err)})
		return root.Advance(), nil
	}
	if !msg.Result.Found {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "workbench.storage", Body: "empty"})
		return root.Advance(), nil
	}
	shell, err := msg.Result.Snapshot.ToShellStore()
	if err != nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.storage", Body: errorString(err)})
		return root.Advance(), nil
	}
	root.Shell = shell
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "workbench.storage", Body: "loaded"})
	return root.Advance(), nil
}

func reduceWorkbenchStoragePersistRequest(root state.Root, _ WorkbenchStoragePersistRequestMsg, deps WorkbenchDeps) (state.Root, []Effect) {
	if deps.Storage == nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.storage", Body: "storage service missing"})
		return root.Advance(), nil
	}
	ref := workbenchStorageRef(root, deps)
	snapshot := state.SnapshotWorkbenchForStorage(root.Shell)
	return root, []Effect{FuncEffect{Run: func(ctx context.Context) Msg {
		result, err := deps.Storage.SaveWorkbench(ctx, services.WorkbenchStorageSaveRequest{
			Ref:      ref,
			Snapshot: snapshot,
		})
		return WorkbenchStoragePersistResultMsg{Result: result, Err: err}
	}}}
}

func reduceWorkbenchStoragePersistResult(root state.Root, msg WorkbenchStoragePersistResultMsg) (state.Root, []Effect) {
	if msg.Err != nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.storage", Body: errorString(msg.Err)})
		return root.Advance(), nil
	}
	body := msg.Result.Ref.Key
	if body == "" {
		body = state.WorkbenchStorageKeyRoot
	}
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "workbench.storage", Body: body})
	return root.Advance(), nil
}

func workbenchStorageRef(root state.Root, deps WorkbenchDeps) state.WorkbenchStorageRef {
	if deps.Ref.AppID != "" || deps.Ref.Key != "" {
		return deps.Ref
	}
	return state.DefaultWorkbenchStorageRef(root.Shell.EnsureDefaults().Workspace.ID)
}
