package app

import (
	"context"
	"errors"
	"log/slog"
	"sort"

	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type WorkbenchDeps struct {
	Storage services.WorkbenchStorageService
	Ref     state.WorkbenchStorageRef
	Logger  *slog.Logger
}

const (
	workbenchStorageLoadToken = CancelToken("workbench.storage.load")
	workbenchStorageSaveToken = CancelToken("workbench.storage.save")
)

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
	Result          services.WorkbenchStorageSaveResult
	Err             error
	Reason          string
	ExpectedVersion uint64
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
			return reduceWorkbenchStorageLoadResult(root, msg, deps)
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
			logEffectError(deps.Logger, "workbench.storage.watch", err, "key", ref.Key, "owner_id", ref.OwnerID)
			if isContextLifecycleError(err) {
				return
			}
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
		if isContextLifecycleError(msg.Err) {
			return root, nil
		}
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
	return root, []Effect{FuncEffect{Token: workbenchStorageLoadToken, Async: true, ForceSyncInTests: true, Run: func(ctx context.Context) Msg {
		result, err := deps.Storage.LoadWorkbench(ctx, ref)
		logEffectError(deps.Logger, "workbench.storage.load", err, "key", ref.Key, "owner_id", ref.OwnerID)
		if isContextLifecycleError(err) {
			return nil
		}
		return WorkbenchStorageLoadResultMsg{Result: result, Err: err}
	}}}
}

func reduceWorkbenchStorageLoadResult(root state.Root, msg WorkbenchStorageLoadResultMsg, deps WorkbenchDeps) (state.Root, []Effect) {
	if msg.Err != nil {
		if isContextLifecycleError(msg.Err) {
			return root, nil
		}
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.storage", Body: errorString(msg.Err)})
		return root.Advance(), nil
	}
	if !msg.Result.Found {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "workbench.storage", Body: "empty"})
		return root.Advance(), nil
	}
	if msg.Result.Version != 0 && msg.Result.Version == root.WorkbenchSync.LastAppliedVersion {
		return root, nil
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
	previousViews := root.TerminalViews
	terminalViews = preserveWorkbenchRuntimeTerminalViews(previousViews, terminalViews)
	// 外部 workbench snapshot 会整体替换 pane/view 结构；旧 frozen history、
	// pending request 和 copy 绑定都不能跨这次替换继续复用。
	root.History = root.History.InvalidateWindow()
	root.CopyMode = state.CopyModeStore{}
	root.Shell = shell
	root.TerminalViews = terminalViews
	root.WorkbenchSync = root.WorkbenchSync.MarkApplied(msg.Result.Version)
	restoredAttachBindings := workbenchRestoredTerminalAttachBindings(previousViews, root.TerminalViews.Bindings())
	logLifecycleTrace(deps.Logger, "workbench.restore",
		"version", msg.Result.Version,
		"active_pane", root.Shell.ActivePaneID,
		"active_floating", root.Shell.ActiveFloatingID,
		"active_pane_kind", lifecycleActivePaneKind(root),
		"active_terminal", lifecycleActiveTerminalID(root),
		"terminal_views", len(root.TerminalViews.Views),
		"bindings", lifecycleTerminalViewsSummary(root.TerminalViews),
		"previous_bindings", lifecycleTerminalViewsSummary(previousViews),
		"reattach_bindings", lifecycleTerminalViewBindingsSummary(restoredAttachBindings),
		"surface_terminal", root.Surface.TerminalID,
		"surface_state", string(root.Surface.State),
		"session_terminal", root.Session.TerminalID,
		"session_state", string(root.Session.State),
	)
	root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastInfo, Title: "workbench.storage", Body: "loaded"})
	if len(root.TerminalViews.Views) > 0 {
		effects := []Effect{FuncEffect{Run: func(context.Context) Msg { return TerminalPoolListRequestMsg{} }}}
		effects = append(effects, workbenchRestoredTerminalLifecycleQueryEffects(previousViews, root.TerminalViews.Bindings())...)
		effects = append(effects, workbenchRestoredTerminalAttachEffectsForBindings(restoredAttachBindings)...)
		return root.Advance(), effects
	}
	return root.Advance(), nil
}

func preserveWorkbenchRuntimeTerminalViews(previous state.TerminalViewStore, restored state.TerminalViewStore) state.TerminalViewStore {
	if len(previous.Views) == 0 || len(restored.Views) == 0 {
		return restored
	}
	for viewID, binding := range restored.Views {
		previousBinding, ok := previous.Views[viewID]
		if !ok || !workbenchBindingAlreadyLive(previous, binding) {
			continue
		}
		// storage snapshot 是布局/绑定意图；当前 runtime 已经 live 的同一 view 不能因为 reload
		// 丢掉 channel 和 resize-control truth，否则会触发无意义 reattach/live-surface 风暴。
		binding.Channel = previousBinding.Channel
		binding.SurfaceID = previousBinding.SurfaceID
		binding.ResizeRole = previousBinding.ResizeRole
		binding.Attached = previousBinding.Attached
		binding.CanResize = previousBinding.CanResize
		binding.SizeLocked = previousBinding.SizeLocked
		binding.ControlReason = previousBinding.ControlReason
		binding.OwnerSurfaceID = previousBinding.OwnerSurfaceID
		binding.OwnerViewID = previousBinding.OwnerViewID
		binding.ResizeEpoch = previousBinding.ResizeEpoch
		binding.LastError = previousBinding.LastError
		restored.Views[viewID] = binding
	}
	return restored
}

func lifecycleActivePaneKind(root state.Root) string {
	shell := root.Shell.EnsureDefaults()
	if shell.ActiveFloatingID != "" {
		for _, floating := range shell.Floatings {
			if floating.ID == shell.ActiveFloatingID {
				return string(floating.Pane.Kind)
			}
		}
	}
	pane, ok := shell.Pane(state.PaneCommandTarget{PaneID: shell.ActivePaneID})
	if !ok {
		return ""
	}
	return string(pane.Kind)
}

func lifecycleActiveTerminalID(root state.Root) string {
	if binding, ok := activeTerminalBinding(root); ok {
		return binding.TerminalID
	}
	return root.Surface.TerminalID
}

func activeTerminalBinding(root state.Root) (state.TerminalViewBinding, bool) {
	shell := root.Shell.EnsureDefaults()
	if shell.ActiveFloatingID != "" {
		return root.TerminalViews.FloatingBinding(shell.ActiveFloatingID)
	}
	return root.TerminalViews.PaneBinding(shell.ActivePaneID)
}

func workbenchRestoredTerminalAttachBindings(previous state.TerminalViewStore, bindings []state.TerminalViewBinding) []state.TerminalViewBinding {
	if len(bindings) == 0 {
		return nil
	}
	ordered := orderedRestoredTerminalBindings(bindings)
	out := make([]state.TerminalViewBinding, 0, len(ordered))
	for _, binding := range ordered {
		if binding.TerminalID == "" || binding.ViewID == "" {
			continue
		}
		if workbenchBindingAlreadyLive(previous, binding) {
			continue
		}
		out = append(out, binding)
	}
	return out
}

func workbenchRestoredTerminalAttachEffects(previous state.TerminalViewStore, bindings []state.TerminalViewBinding) []Effect {
	return workbenchRestoredTerminalAttachEffectsForBindings(workbenchRestoredTerminalAttachBindings(previous, bindings))
}

func workbenchRestoredTerminalAttachEffectsForBindings(bindings []state.TerminalViewBinding) []Effect {
	if len(bindings) == 0 {
		return nil
	}
	effects := make([]Effect, 0, len(bindings))
	for _, binding := range bindings {
		binding := binding
		cols, rows := binding.DesiredCols, binding.DesiredRows
		if cols <= 0 {
			cols = 80
		}
		if rows <= 0 {
			rows = 24
		}
		// storage 里记录的是上次退出时的 view 意图；重进时必须原样向 core 申请，
		// 最终 owner/follower truth 仍以 core-v2 attach 返回的 ResizeControl 为准。
		resizePolicy := binding.ResizeRole
		if resizePolicy == "" {
			resizePolicy = state.TerminalResizeRoleFollower
		}
		cfg := LiveConfig{
			TerminalID:   binding.TerminalID,
			Cols:         cols,
			Rows:         rows,
			Mode:         "collaborator",
			ResizePolicy: resizePolicy,
			SurfaceID:    binding.SurfaceID,
			ViewID:       binding.ViewID,
		}
		cfgCopy := cfg
		effects = append(effects, FuncEffect{Run: func(context.Context) Msg {
			return LiveAttachMsg{Config: cfgCopy}
		}})
	}
	return effects
}

func workbenchRestoredTerminalLifecycleQueryEffects(previous state.TerminalViewStore, bindings []state.TerminalViewBinding) []Effect {
	targets := workbenchRestoredTerminalLifecycleQueryTargets(previous, bindings)
	if len(targets) == 0 {
		return nil
	}
	return []Effect{FuncEffect{Run: func(context.Context) Msg {
		return LiveLifecycleQueryMsg{Reason: "workbench.restore", Targets: targets}
	}}}
}

func workbenchRestoredTerminalLifecycleQueryTargets(previous state.TerminalViewStore, bindings []state.TerminalViewBinding) []LiveLifecycleQueryTarget {
	if len(bindings) == 0 {
		return nil
	}
	ordered := orderedRestoredTerminalBindings(bindings)
	targets := make([]LiveLifecycleQueryTarget, 0, len(ordered))
	seen := map[string]struct{}{}
	for _, binding := range ordered {
		if binding.TerminalID == "" {
			continue
		}
		if !workbenchBindingAlreadyLive(previous, binding) {
			continue
		}
		if _, ok := seen[binding.TerminalID]; ok {
			continue
		}
		seen[binding.TerminalID] = struct{}{}
		cols, rows := binding.DesiredCols, binding.DesiredRows
		if cols <= 0 {
			cols = 80
		}
		if rows <= 0 {
			rows = 24
		}
		targets = append(targets, LiveLifecycleQueryTarget{TerminalID: binding.TerminalID, Cols: cols, Rows: rows})
	}
	return targets
}

func workbenchBindingAlreadyLive(previous state.TerminalViewStore, binding state.TerminalViewBinding) bool {
	existing, ok := previous.Views[binding.ViewID]
	if !ok {
		return false
	}
	if !existing.Attached || existing.TerminalID == "" || existing.TerminalID != binding.TerminalID {
		return false
	}
	if existing.Channel == 0 {
		return false
	}
	if existing.ResizeRole != binding.ResizeRole {
		return false
	}
	if existing.DesiredCols != binding.DesiredCols || existing.DesiredRows != binding.DesiredRows {
		return false
	}
	return true
}

func orderedRestoredTerminalBindings(bindings []state.TerminalViewBinding) []state.TerminalViewBinding {
	ordered := append([]state.TerminalViewBinding(nil), bindings...)
	sort.SliceStable(ordered, func(i, j int) bool {
		left := ordered[i]
		right := ordered[j]
		if left.TerminalID != right.TerminalID {
			return left.TerminalID < right.TerminalID
		}
		leftPriority := restoredResizeRolePriority(left.ResizeRole)
		rightPriority := restoredResizeRolePriority(right.ResizeRole)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return left.ViewID < right.ViewID
	})
	return ordered
}

func restoredResizeRolePriority(role string) int {
	switch role {
	case state.TerminalResizeRoleOwner:
		return 0
	case state.TerminalResizeRoleFollower, "":
		return 1
	case state.TerminalResizeRoleObserver:
		return 2
	default:
		return 1
	}
}

func reduceWorkbenchStoragePersistRequest(root state.Root, msg WorkbenchStoragePersistRequestMsg, deps WorkbenchDeps) (state.Root, []Effect) {
	if deps.Storage == nil {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "workbench.storage", Body: "storage service missing"})
		return root.Advance(), nil
	}
	ref := workbenchStorageRef(root, deps)
	snapshot := state.SnapshotRootWorkbenchForStorage(root)
	expectedVersion := root.WorkbenchSync.SaveVersion()
	return root, []Effect{FuncEffect{Token: workbenchStorageSaveToken, Async: true, ForceSyncInTests: true, Run: func(ctx context.Context) Msg {
		result, err := deps.Storage.SaveWorkbench(ctx, services.WorkbenchStorageSaveRequest{
			Ref:             ref.WithVersion(expectedVersion),
			Snapshot:        snapshot,
			CheckVersion:    true,
			ExpectedVersion: expectedVersion,
		})
		logEffectError(deps.Logger, "workbench.storage.save", err, "key", ref.Key, "owner_id", ref.OwnerID, "expected_version", expectedVersion)
		if isContextLifecycleError(err) {
			return nil
		}
		return WorkbenchStoragePersistResultMsg{Result: result, Err: err, Reason: msg.Reason, ExpectedVersion: expectedVersion}
	}}}
}

func reduceWorkbenchStoragePersistResult(root state.Root, msg WorkbenchStoragePersistResultMsg) (state.Root, []Effect) {
	if msg.ExpectedVersion != root.WorkbenchSync.SaveVersion() {
		return root, nil
	}
	if msg.Err != nil {
		if isContextLifecycleError(msg.Err) {
			return root, nil
		}
		if errors.Is(msg.Err, services.ErrWorkbenchStorageConflict) {
			if root.WorkbenchSync.Conflict && root.WorkbenchSync.ConflictVersion == msg.ExpectedVersion {
				return root, nil
			}
			root.WorkbenchSync = root.WorkbenchSync.MarkConflict(msg.ExpectedVersion)
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
