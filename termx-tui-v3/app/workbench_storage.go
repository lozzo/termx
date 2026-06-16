package app

import (
	"context"
	"errors"

	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type WorkbenchDeps struct {
	Storage services.WorkbenchStorageService
	Ref     state.WorkbenchStorageRef
}

type WorkbenchStorageLoadRequestMsg struct{}

func (WorkbenchStorageLoadRequestMsg) isMsg() {}

type WorkbenchStorageWatchRequestMsg struct{}

func (WorkbenchStorageWatchRequestMsg) isMsg() {}

type WorkbenchStorageChangedMsg struct {
	Event services.WorkbenchStorageEvent
	Err   error
}

func (WorkbenchStorageChangedMsg) isMsg() {}

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

// workbench storage 是 pane/floating 到 terminal 连接关系的持久来源；
// attach、kill、layout 这类本地 view 变更都通过完整 snapshot 委托给 core opaque storage。
func workbenchPersistEffects(reason string) []Effect {
	return []Effect{FuncEffect{Run: func(context.Context) Msg {
		return WorkbenchStoragePersistRequestMsg{Reason: reason}
	}}}
}

func NewWorkbenchStorageReducer(deps WorkbenchDeps) Reducer {
	return func(root state.Root, msg Msg) (state.Root, []Effect) {
		switch msg := msg.(type) {
		case WorkbenchStorageWatchRequestMsg:
			return reduceWorkbenchStorageWatchRequest(root, deps)
		case WorkbenchStorageChangedMsg:
			return reduceWorkbenchStorageChanged(root, msg)
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

func reduceWorkbenchStorageWatchRequest(root state.Root, deps WorkbenchDeps) (state.Root, []Effect) {
	if deps.Storage == nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.storage", Body: "storage service missing"})
		return root.Advance(), nil
	}
	ref := workbenchStorageRef(root, deps)
	return root, []Effect{StreamEffect{Token: CancelToken("workbench.storage.watch"), Run: func(ctx context.Context, post func(Msg)) {
		events, err := deps.Storage.WatchWorkbench(ctx, ref)
		if err != nil {
			post(WorkbenchStorageChangedMsg{Err: err})
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				post(WorkbenchStorageChangedMsg{Event: event})
			}
		}
	}}}
}

func reduceWorkbenchStorageChanged(root state.Root, msg WorkbenchStorageChangedMsg) (state.Root, []Effect) {
	if msg.Err != nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.storage", Body: errorString(msg.Err)})
		return root.Advance(), nil
	}
	if root.WorkbenchSync.ShouldIgnoreEvent(msg.Event.Version) {
		root.WorkbenchSync = root.WorkbenchSync.MarkEvent(msg.Event.Version)
		return root.Advance(), nil
	}
	root.WorkbenchSync = root.WorkbenchSync.MarkEvent(msg.Event.Version)
	return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg {
		return WorkbenchStorageLoadRequestMsg{}
	}}}
}

func reduceWorkbenchStorageLoadRequest(root state.Root, deps WorkbenchDeps) (state.Root, []Effect) {
	if deps.Storage == nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.storage", Body: "storage service missing"})
		return root.Advance(), nil
	}
	ref := workbenchStorageRef(root, deps)
	return root, []Effect{FuncEffect{Async: true, ForceSyncInTests: true, Run: func(ctx context.Context) Msg {
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
	terminalViews, err := msg.Result.Snapshot.ToTerminalViewStore()
	if err != nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.storage", Body: errorString(err)})
		return root.Advance(), nil
	}
	// 外部 workbench snapshot 会整体替换 pane/view 结构；旧 frozen history、
	// pending request 和 copy 绑定都不能跨这次替换继续复用。
	root.History = root.History.InvalidateWindow()
	root.CopyMode = state.CopyModeStore{}
	root.Shell = shell
	root.TerminalViews = terminalViews
	root.WorkbenchSync = root.WorkbenchSync.MarkApplied(msg.Result.Version)
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "workbench.storage", Body: "loaded"})
	if len(root.TerminalViews.Views) > 0 {
		effects := []Effect{FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }}}
		effects = append(effects, workbenchRestoredTerminalAttachEffects(root.TerminalViews.Bindings())...)
		return root.Advance(), effects
	}
	return root.Advance(), nil
}

func workbenchRestoredTerminalAttachEffects(bindings []state.TerminalViewBinding) []Effect {
	if len(bindings) == 0 {
		return nil
	}
	effects := make([]Effect, 0, len(bindings))
	for _, binding := range bindings {
		binding := binding
		if binding.TerminalID == "" || binding.ViewID == "" {
			continue
		}
		cols, rows := binding.DesiredCols, binding.DesiredRows
		if cols <= 0 {
			cols = 80
		}
		if rows <= 0 {
			rows = 24
		}
		// 启动恢复只重建 view 到 core 的连接；owner truth 必须等 core-v2 返回。
		resizePolicy := state.TerminalResizeRoleFollower
		if binding.ResizeRole == state.TerminalResizeRoleObserver {
			resizePolicy = state.TerminalResizeRoleObserver
		}
		effects = append(effects, FuncEffect{Run: func(context.Context) Msg {
			return LiveAttachMsg{Config: LiveConfig{
				TerminalID:   binding.TerminalID,
				Cols:         cols,
				Rows:         rows,
				Mode:         "collaborator",
				ResizePolicy: resizePolicy,
				SurfaceID:    binding.SurfaceID,
				ViewID:       binding.ViewID,
			}}
		}})
	}
	return effects
}

func reduceWorkbenchStoragePersistRequest(root state.Root, _ WorkbenchStoragePersistRequestMsg, deps WorkbenchDeps) (state.Root, []Effect) {
	if deps.Storage == nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.storage", Body: "storage service missing"})
		return root.Advance(), nil
	}
	ref := workbenchStorageRef(root, deps)
	snapshot := state.SnapshotRootWorkbenchForStorage(root)
	expectedVersion := root.WorkbenchSync.SaveVersion()
	return root, []Effect{FuncEffect{Async: true, ForceSyncInTests: true, Run: func(ctx context.Context) Msg {
		result, err := deps.Storage.SaveWorkbench(ctx, services.WorkbenchStorageSaveRequest{
			Ref:             ref.WithVersion(expectedVersion),
			Snapshot:        snapshot,
			CheckVersion:    true,
			ExpectedVersion: expectedVersion,
		})
		return WorkbenchStoragePersistResultMsg{Result: result, Err: err}
	}}}
}

func reduceWorkbenchStoragePersistResult(root state.Root, msg WorkbenchStoragePersistResultMsg) (state.Root, []Effect) {
	if msg.Err != nil {
		if errors.Is(msg.Err, services.ErrWorkbenchStorageConflict) {
			root.WorkbenchSync = root.WorkbenchSync.MarkConflict(root.WorkbenchSync.SaveVersion())
			root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.storage", Body: "conflict: reloading"})
			return root.Advance(), []Effect{FuncEffect{Run: func(context.Context) Msg {
				return WorkbenchStorageLoadRequestMsg{}
			}}}
		}
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.storage", Body: errorString(msg.Err)})
		return root.Advance(), nil
	}
	body := msg.Result.Ref.Key
	if body == "" {
		body = state.WorkbenchStorageKeyRoot
	}
	root.WorkbenchSync = root.WorkbenchSync.MarkSaved(msg.Result.Ref, msg.Result.Version)
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "workbench.storage", Body: body})
	return root.Advance(), nil
}

func workbenchStorageRef(root state.Root, deps WorkbenchDeps) state.WorkbenchStorageRef {
	if deps.Ref.AppID != "" || deps.Ref.Key != "" {
		return deps.Ref
	}
	return state.DefaultWorkbenchStorageRef(root.Shell.EnsureDefaults().Workspace.ID)
}
