package app

import (
	"context"
	"log/slog"
	"strings"

	"github.com/lozzow/termx/termx-shared/terminalmeta"
	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/services"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

type LiveConfig struct {
	TerminalID   string
	Cols         int
	Rows         int
	Mode         string
	ResizePolicy string
	SurfaceID    string
	ViewID       string
}

type LiveDeps struct {
	Terminal services.TerminalService
	Logger   *slog.Logger
}

const liveStreamTokenPrefix = "terminal.live.stream:"

// NewLiveRuntime 组合 live app 主路径：TerminalHost 输入 -> reducer/effect ->
// terminal service -> render VM -> FrameSink。
func NewLiveRuntime(initial state.Root, host TerminalHost, runner EffectRunner, deps LiveDeps) *AppRuntime {
	initial.Shell = initial.Shell.EnsureDefaults()
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())
	runtime := NewAppRuntime(initial, ComposeReducers(NewShellReducer(), NewUIInputReducer(), NewTerminalPoolReducer(deps), NewTerminalInputRouterReducer(deps), NewLiveReducer(deps), NewTerminalLayoutResizeReducer()), hostRenderFunc(host, builder, renderer), host, runner)
	runtime.SetLogger(deps.Logger)
	return runtime
}

// NewInteractiveRuntime 组合 live 与 copy mode 主路径。copy mode 会消费
// page up、wheel、selection/copy 等交互；普通 terminal input 仍交给 live path。
func NewInteractiveRuntime(
	initial state.Root,
	host TerminalHost,
	runner EffectRunner,
	live LiveDeps,
	copyMode CopyModeDeps,
) *AppRuntime {
	return NewInteractiveRuntimeWithStorage(initial, host, runner, live, copyMode, WorkbenchDeps{}, ClipboardDeps{})
}

func NewInteractiveRuntimeWithWorkbench(
	initial state.Root,
	host TerminalHost,
	runner EffectRunner,
	live LiveDeps,
	copyMode CopyModeDeps,
	workbench WorkbenchDeps,
) *AppRuntime {
	return NewInteractiveRuntimeWithStorage(initial, host, runner, live, copyMode, workbench, ClipboardDeps{})
}

func NewInteractiveRuntimeWithStorage(
	initial state.Root,
	host TerminalHost,
	runner EffectRunner,
	live LiveDeps,
	copyMode CopyModeDeps,
	workbench WorkbenchDeps,
	clipboard ClipboardDeps,
) *AppRuntime {
	initial.Shell = initial.Shell.EnsureDefaults()
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())
	runtime := NewAppRuntime(initial, ComposeReducers(NewShellReducer(), NewUIInputReducer(), NewTerminalPoolReducer(live), NewWorkbenchStorageReducer(workbench), NewClipboardStorageReducer(clipboard), NewCopyModeReducer(copyMode), NewCopyModeResizeRebindReducer(copyMode), NewTerminalInputRouterReducer(live), NewLiveReducer(live), NewTerminalLayoutResizeReducer()), hostRenderFunc(host, builder, renderer), host, runner)
	runtime.SetLogger(live.Logger)
	if workbench.Storage != nil {
		// 启动时先恢复 core-v2 opaque storage 中的 workbench truth，再订阅后续变化。
		if !workbench.SkipInitialLoad {
			runtime.enqueue(WorkbenchStorageLoadRequestMsg{})
		}
		runtime.enqueue(WorkbenchStorageWatchRequestMsg{})
	}
	if clipboard.Storage != nil {
		// copy list 是 TUI schema，core 只保存 opaque value；启动时拉一次并监听变化。
		runtime.enqueue(ClipboardStorageLoadRequestMsg{Reason: "startup"})
		runtime.enqueue(ClipboardStorageWatchRequestMsg{})
	}
	return runtime
}

func hostRenderFunc(host TerminalHost, builder render.RenderVMBuilder, renderer render.Renderer) RenderFunc {
	ansiOnly := false
	if host != nil {
		if sink := host.FrameSink(); sink != nil {
			if preference, ok := sink.(render.FrameSinkPreference); ok {
				ansiOnly = !preference.NeedsCompleteFrame()
			}
		}
	}
	return func(root state.Root) render.Frame {
		vm := builder.Build(root)
		if ansiOnly {
			// 中文说明：真实 TTY 只消费 ANSI 行；测试 sink 默认保留 plain/styled frame 方便断言。
			return renderer.RenderANSI(vm)
		}
		return renderer.Render(vm)
	}
}

type LiveAttachMsg struct {
	Config LiveConfig
}

func (LiveAttachMsg) isMsg() {}

type LiveAttachResultMsg struct {
	TerminalID string
	Result     services.TerminalAttachResult
	Err        error
}

func (LiveAttachResultMsg) isMsg() {}

type LiveSurfaceMsg struct {
	Snapshot       state.LiveSurfaceSnapshot
	Err            error
	LifecycleKnown bool
}

func (LiveSurfaceMsg) isMsg() {}

type LiveLifecycleQueryTarget struct {
	TerminalID string
	Cols       int
	Rows       int
}

type LiveLifecycleQueryMsg struct {
	Reason  string
	Targets []LiveLifecycleQueryTarget
}

func (LiveLifecycleQueryMsg) isMsg() {}

type LiveEventMsg struct {
	Event services.TerminalLiveEvent
}

func (LiveEventMsg) isMsg() {}

type LiveExitMsg struct {
	TerminalID string
	ExitCode   int
	Reason     string
}

func (LiveExitMsg) isMsg() {}

type LiveResizeMsg struct {
	TerminalID string
	Cols       int
	Rows       int
	Seq        uint64
	ViewID     string
}

func (LiveResizeMsg) isMsg() {}

type LiveResizeResultMsg struct {
	Result services.TerminalResizeResult
	Cols   int
	Rows   int
	Seq    uint64
	ViewID string
	Err    error
}

func (LiveResizeResultMsg) isMsg() {}

type LiveInputResultMsg struct {
	TerminalID   string
	ViewID       string
	Channel      uint16
	Event        input.InputEvent
	Bytes        []byte
	RetryOnError bool
	Err          error
}

func (LiveInputResultMsg) isMsg() {}

type LiveInputAttachResultMsg struct {
	Target liveInputTargetInfo
	Event  input.InputEvent
	Bytes  []byte
	Result services.TerminalAttachResult
	Err    error
}

func (LiveInputAttachResultMsg) isMsg() {}

func NewLiveReducer(deps LiveDeps) Reducer {
	return func(root state.Root, msg Msg) (state.Root, []Effect) {
		switch msg := msg.(type) {
		case LiveAttachMsg:
			return reduceLiveAttach(root, msg, deps)
		case LiveAttachResultMsg:
			return reduceLiveAttachResult(root, msg, deps)
		case LiveLifecycleQueryMsg:
			return reduceLiveLifecycleQuery(root, msg, deps)
		case LiveSurfaceMsg:
			if msg.Err != nil {
				if next, ok := markTerminalExitedFromError(root, msg.Snapshot.TerminalID, msg.Err); ok {
					logLifecycleTrace(deps.Logger, "live.surface.error.exited",
						"terminal_id", msg.Snapshot.TerminalID,
						"error", msg.Err.Error(),
						"surface_state", string(next.Surface.SurfaceForTerminal(msg.Snapshot.TerminalID).State),
						"session_state", string(next.Session.State),
					)
					return next.Advance(), nil
				}
				root.Surface = root.Surface.SetError(msg.Err.Error())
				return root.Advance(), nil
			}
			root.Surface = root.Surface.ApplySnapshotWithLifecycle(msg.Snapshot, msg.LifecycleKnown)
			if msg.LifecycleKnown && msg.Snapshot.State == state.TerminalLiveAttached && msg.Snapshot.TerminalID == root.Session.TerminalID {
				root.Session = root.Session.MarkAttached(msg.Snapshot.TerminalID)
			}
			logLifecycleTrace(deps.Logger, "live.surface",
				"terminal_id", msg.Snapshot.TerminalID,
				"snapshot_state", string(msg.Snapshot.State),
				"lifecycle_known", msg.LifecycleKnown,
				"snapshot_exit_code", msg.Snapshot.ExitCode,
				"snapshot_exited_at", lifecycleTimeSummary(msg.Snapshot.ExitedAt),
				"snapshot_command", strings.Join(msg.Snapshot.Command, " "),
				"surface_state", string(root.Surface.State),
				"surface_terminal_state", string(root.Surface.SurfaceForTerminal(msg.Snapshot.TerminalID).State),
				"session_state", string(root.Session.State),
				"active_terminal", lifecycleActiveTerminalID(root),
				"bindings", lifecycleTerminalViewBindingsSummary(root.TerminalViews.BindingsForTerminal(msg.Snapshot.TerminalID)),
			)
			return maybeRefreshFloatingAutoFit(root, msg.Snapshot.TerminalID)
		case LiveEventMsg:
			return reduceLiveEvent(root, msg, deps)
		case LiveExitMsg:
			root.Session = root.Session.MarkExited(msg.TerminalID, msg.ExitCode, msg.Reason)
			root.Surface = root.Surface.MarkExited(msg.TerminalID, msg.ExitCode, msg.Reason)
			return root.Advance(), nil
		case LiveInputResultMsg:
			if msg.Err != nil {
				if isContextLifecycleError(msg.Err) {
					return root, nil
				}
				if next, ok := markTerminalExitedFromError(root, msg.TerminalID, msg.Err); ok {
					return next.Advance(), nil
				}
				if msg.RetryOnError && msg.ViewID != "" && len(msg.Bytes) > 0 {
					if target, ok := liveInputTargetForView(root, msg.ViewID); ok && target.TerminalID == msg.TerminalID {
						// 中文说明：channel 是 view attach 身份；发送失败时只重建这一个 view，
						// 不能退回全局 session 或抢用 sibling panel 的 channel。
						return root, []Effect{liveAttachForInputEffect(root, target, msg.Event, msg.Bytes, deps)}
					}
				}
				root = setLiveInputError(root, msg.TerminalID, msg.Err.Error())
				return root.Advance(), nil
			}
			return root, nil
		case LiveInputAttachResultMsg:
			return reduceLiveInputAttachResult(root, msg, deps)
		case LiveResizeMsg:
			return reduceLiveResize(root, msg, deps)
		case LiveResizeResultMsg:
			if shouldRecoverOwnerDesiredAfterResizeResult(root, msg) {
				return recoverLatestResizeAfterStaleResult(root, msg)
			}
			viewScoped := msg.ViewID != ""
			if msg.ViewID != "" {
				binding, ok := root.TerminalViews.Views[msg.ViewID]
				if !ok {
					// view 已经被关闭或解绑后，迟到的 resize 结果不能再回写共享 session/surface；
					// 否则 close pane 后旧 view 的结果会把当前 owner 的尺寸状态顶回去。
					return recoverLatestResizeAfterStaleResult(root, msg)
				}
				if binding.IsStaleResizeResult(msg.Seq) {
					return recoverLatestResizeAfterStaleResult(root, msg)
				}
			}
			if !viewScoped && root.Session.IsStaleResizeResult(msg.Seq) {
				return recoverLatestResizeAfterStaleResult(root, msg)
			}
			if msg.Err != nil {
				if next, ok := markTerminalExitedFromError(root, root.Session.TerminalID, msg.Err); ok {
					return next.Advance(), nil
				}
				root.Session = root.Session.SetError(msg.Err.Error())
				root.Surface = root.Surface.SetError(msg.Err.Error())
				return root.Advance(), nil
			}
			cols, rows := msg.Cols, msg.Rows
			if msg.Result.Cols > 0 {
				cols = msg.Result.Cols
			}
			if msg.Result.Rows > 0 {
				rows = msg.Result.Rows
			}
			if viewScoped {
				if hasResizeControlResult(msg.Result) {
					root.TerminalViews, _ = root.TerminalViews.ApplyResizeControl(msg.ViewID, resizeControlProjectionFromResult(msg.Result))
				}
				root.TerminalViews, _ = root.TerminalViews.ApplyResizeResult(msg.ViewID, msg.Seq, cols, rows, "")
				if msg.Result.Resized || !hasResizeControlResult(msg.Result) {
					root.Session = root.Session.Resize(cols, rows)
					root.Surface = root.Surface.Resize(cols, rows)
				}
			} else {
				nextSession, applied := root.Session.ApplyResizeResult(msg.Seq, cols, rows)
				if !applied {
					return root, nil
				}
				root.Session = nextSession
				root.Surface = root.Surface.Resize(cols, rows)
			}
			if root.CopyMode.Active && root.CopyMode.BoundCols != cols {
				root.CopyMode = root.CopyMode.Resize(cols, rows)
			} else if root.CopyMode.Active {
				root.CopyMode = root.CopyMode.SetViewRows(rows)
				root.CopyMode = root.CopyMode.Scroll(0, len(root.History.Rows))
			}
			return maybeRefreshFloatingAutoFit(root, liveResizeTerminalID(root, msg))
		default:
			return root, nil
		}
	}
}

func reduceLiveAttach(root state.Root, msg LiveAttachMsg, deps LiveDeps) (state.Root, []Effect) {
	if deps.Terminal == nil {
		return setLiveError(root, "terminal service missing"), nil
	}
	cfg := msg.Config
	cfg.Cols, cfg.Rows = liveAttachContentSize(root, cfg)
	if cfg.SurfaceID == "" {
		cfg.SurfaceID = "termx-tui-v3"
	}
	if cfg.ViewID == "" {
		cfg.ViewID = state.TerminalPaneViewID(root.Shell.EnsureDefaults().ActivePaneID)
	}
	if cfg.ResizePolicy == "" {
		cfg.ResizePolicy = state.TerminalResizeRoleOwner
	}
	logLifecycleTrace(deps.Logger, "live.attach.request",
		"terminal_id", cfg.TerminalID,
		"view_id", cfg.ViewID,
		"surface_id", cfg.SurfaceID,
		"cols", cfg.Cols,
		"rows", cfg.Rows,
		"mode", cfg.Mode,
		"resize_policy", cfg.ResizePolicy,
		"existing_bindings", lifecycleTerminalViewBindingsSummary(root.TerminalViews.BindingsForTerminal(cfg.TerminalID)),
		"surface_state", string(root.Surface.SurfaceForTerminal(cfg.TerminalID).State),
		"session_terminal", root.Session.TerminalID,
		"session_state", string(root.Session.State),
	)
	return root, []Effect{FuncEffect{
		Async:            true,
		ForceSyncInTests: true,
		Run: func(ctx context.Context) Msg {
			result, err := deps.Terminal.Attach(ctx, services.TerminalAttachRequest{
				TerminalID:   cfg.TerminalID,
				Cols:         cfg.Cols,
				Rows:         cfg.Rows,
				Mode:         cfg.Mode,
				ResizePolicy: cfg.ResizePolicy,
				SurfaceID:    cfg.SurfaceID,
				ViewID:       cfg.ViewID,
			})
			return LiveAttachResultMsg{TerminalID: cfg.TerminalID, Result: result, Err: err}
		},
	}}
}

func reduceLiveAttachResult(root state.Root, msg LiveAttachResultMsg, deps LiveDeps) (state.Root, []Effect) {
	if msg.Err != nil {
		if next, ok := markTerminalExitedFromError(root, msg.TerminalID, msg.Err); ok {
			logLifecycleTrace(deps.Logger, "live.attach.result.exited",
				"terminal_id", msg.TerminalID,
				"error", msg.Err.Error(),
				"surface_state", string(next.Surface.SurfaceForTerminal(msg.TerminalID).State),
				"session_state", string(next.Session.State),
			)
			return next.Advance(), nil
		}
		logLifecycleTrace(deps.Logger, "live.attach.result",
			"terminal_id", msg.TerminalID,
			"error", msg.Err.Error(),
			"bindings", lifecycleTerminalViewBindingsSummary(root.TerminalViews.BindingsForTerminal(msg.TerminalID)),
		)
		return setLiveError(root, msg.Err.Error()), nil
	}
	result := msg.Result
	if result.TerminalID == "" {
		result.TerminalID = msg.TerminalID
	}
	result = normalizeTerminalAttachResultForLock(root, result)
	viewID := result.ViewID
	activePaneID := root.Shell.EnsureDefaults().ActivePaneID
	if viewID == "" {
		viewID = state.TerminalPaneViewID(activePaneID)
	} else if !liveAttachViewStillPresent(root, viewID) {
		// 外部 reload/restore 替换 pane/view 结构后，旧 view 的迟到 attach result
		// 不能回退绑定到当前 pane/floating；但首次 attach 时 view binding 可能还没建，
		// 只要 shell 结构里仍存在这个目标 view，就必须允许它继续落到当前 truth。
		logLifecycleTrace(deps.Logger, "live.attach.result.stale_view",
			"terminal_id", result.TerminalID,
			"view_id", viewID,
			"channel", result.Channel,
			"bindings", lifecycleTerminalViewsSummary(root.TerminalViews),
		)
		return root, nil
	}
	target, hasTarget := liveAttachTargetForViewID(root, viewID)
	if hasTarget && target.PaneID != "" {
		activePaneID = target.PaneID
	}
	root.Session = root.Session.AttachWithResizeOwner(result.TerminalID, result.Channel, result.Cols, result.Rows, result.ResizePolicy, result.SurfaceID, viewID)
	root.Surface = root.Surface.Attach(result.TerminalID, result.Cols, result.Rows)
	if existing, ok := root.TerminalViews.Views[viewID]; ok {
		if existing.PaneID != "" {
			activePaneID = existing.PaneID
		}
		if existing.FloatingID != "" {
			root = invalidateCopyModeForTerminalRebind(root, existing.PaneID, viewID, result.TerminalID)
			binding := state.NewFloatingTerminalView(existing.FloatingID, existing.PaneID, result.TerminalID, result.Channel, result.Cols, result.Rows, result.ResizePolicy, result.SurfaceID, viewID, result.CanResize)
			binding.Layout = existing.Layout
			root.TerminalViews = root.TerminalViews.BindFloating(binding)
			root.TerminalViews, _ = root.TerminalViews.ApplyResizeControl(viewID, state.TerminalResizeControlProjection{
				CanResize:      result.CanResize,
				SizeLocked:     result.SizeLocked,
				ControlReason:  result.ControlReason,
				OwnerSurfaceID: result.OwnerSurfaceID,
				OwnerViewID:    result.OwnerViewID,
				ResizeEpoch:    result.ResizeEpoch,
				ResizeRole:     result.ResizePolicy,
				SurfaceID:      result.SurfaceID,
				ViewID:         viewID,
			})
			root.TerminalViews = projectTerminalAttachResultLock(root.TerminalViews, result)
			logLiveAttachApplied(deps, root, result, "floating-existing")
			effects := workbenchPersistEffects("terminal.attach")
			effects = append(effects, liveEffects(result.TerminalID, result.Cols, result.Rows, deps)...)
			return root.Advance(), effects
		}
	}
	if hasTarget && target.FloatingID != "" {
		root = invalidateCopyModeForTerminalRebind(root, target.PaneID, viewID, result.TerminalID)
		binding := state.NewFloatingTerminalView(target.FloatingID, target.PaneID, result.TerminalID, result.Channel, result.Cols, result.Rows, result.ResizePolicy, result.SurfaceID, viewID, result.CanResize)
		root.TerminalViews = root.TerminalViews.BindFloating(binding)
		root.TerminalViews, _ = root.TerminalViews.ApplyResizeControl(viewID, state.TerminalResizeControlProjection{
			CanResize:      result.CanResize,
			SizeLocked:     result.SizeLocked,
			ControlReason:  result.ControlReason,
			OwnerSurfaceID: result.OwnerSurfaceID,
			OwnerViewID:    result.OwnerViewID,
			ResizeEpoch:    result.ResizeEpoch,
			ResizeRole:     result.ResizePolicy,
			SurfaceID:      result.SurfaceID,
			ViewID:         viewID,
		})
		root.TerminalViews = projectTerminalAttachResultLock(root.TerminalViews, result)
		root.Shell = root.Shell.BindFloatingTerminal(target.FloatingID, result.TerminalID)
		logLiveAttachApplied(deps, root, result, "floating-target")
		effects := workbenchPersistEffects("terminal.attach")
		effects = append(effects, liveEffects(result.TerminalID, result.Cols, result.Rows, deps)...)
		return root.Advance(), effects
	}
	root.Shell = root.Shell.EnsureActiveTabForAttach()
	root.Shell = root.Shell.BindPaneTerminal(state.PaneCommandTarget{PaneID: activePaneID}, result.TerminalID)
	root = invalidateCopyModeForTerminalRebind(root, activePaneID, viewID, result.TerminalID)
	binding := state.NewPaneTerminalView(activePaneID, result.TerminalID, result.Channel, result.Cols, result.Rows, result.ResizePolicy, result.SurfaceID, viewID, result.CanResize)
	if existing, ok := root.TerminalViews.Views[viewID]; ok {
		binding.Layout = existing.Layout
	}
	root.TerminalViews = root.TerminalViews.BindPane(binding)
	root.TerminalViews, _ = root.TerminalViews.ApplyResizeControl(viewID, state.TerminalResizeControlProjection{
		CanResize:      result.CanResize,
		SizeLocked:     result.SizeLocked,
		ControlReason:  result.ControlReason,
		OwnerSurfaceID: result.OwnerSurfaceID,
		OwnerViewID:    result.OwnerViewID,
		ResizeEpoch:    result.ResizeEpoch,
		ResizeRole:     result.ResizePolicy,
		SurfaceID:      result.SurfaceID,
		ViewID:         viewID,
	})
	root.TerminalViews = projectTerminalAttachResultLock(root.TerminalViews, result)
	logLiveAttachApplied(deps, root, result, "pane")
	effects := workbenchPersistEffects("terminal.attach")
	effects = append(effects, liveEffects(result.TerminalID, result.Cols, result.Rows, deps)...)
	return root.Advance(), effects
}

func logLiveAttachApplied(deps LiveDeps, root state.Root, result services.TerminalAttachResult, targetKind string) {
	logLifecycleTrace(deps.Logger, "live.attach.result",
		"target_kind", targetKind,
		"terminal_id", result.TerminalID,
		"view_id", result.ViewID,
		"surface_id", result.SurfaceID,
		"channel", result.Channel,
		"cols", result.Cols,
		"rows", result.Rows,
		"resize_policy", result.ResizePolicy,
		"can_resize", result.CanResize,
		"size_locked", result.SizeLocked,
		"control_reason", result.ControlReason,
		"owner_view_id", result.OwnerViewID,
		"surface_state", string(root.Surface.SurfaceForTerminal(result.TerminalID).State),
		"session_terminal", root.Session.TerminalID,
		"session_state", string(root.Session.State),
		"bindings", lifecycleTerminalViewBindingsSummary(root.TerminalViews.BindingsForTerminal(result.TerminalID)),
	)
}

func liveAttachViewStillPresent(root state.Root, viewID string) bool {
	if viewID == "" {
		return false
	}
	if !root.Session.Attached && len(root.TerminalViews.Views) == 0 {
		// 初次 attach 时还没有任何 reducer-owned view binding，显式 ViewID 不能被误判成 stale。
		return true
	}
	if _, ok := root.TerminalViews.Views[viewID]; ok {
		return true
	}
	shell := root.Shell.EnsureDefaults()
	for _, tab := range shell.Workspace.Tabs {
		for _, pane := range tab.Panes {
			if state.TerminalPaneViewID(pane.ID) == viewID {
				return true
			}
		}
	}
	for _, tab := range shell.Workspace.Tabs {
		for _, floating := range tab.Floatings {
			if state.TerminalFloatingViewID(floating.ID) == viewID {
				return true
			}
		}
	}
	return false
}

type liveAttachViewTarget struct {
	PaneID     string
	FloatingID string
}

func liveAttachTargetForViewID(root state.Root, viewID string) (liveAttachViewTarget, bool) {
	if viewID == "" {
		return liveAttachViewTarget{}, false
	}
	if existing, ok := root.TerminalViews.Views[viewID]; ok {
		return liveAttachViewTarget{PaneID: existing.PaneID, FloatingID: existing.FloatingID}, true
	}
	shell := root.Shell.EnsureDefaults()
	for _, tab := range shell.Workspace.Tabs {
		for _, pane := range tab.Panes {
			if state.TerminalPaneViewID(pane.ID) == viewID {
				return liveAttachViewTarget{PaneID: pane.ID}, true
			}
		}
	}
	for _, tab := range shell.Workspace.Tabs {
		for _, floating := range tab.Floatings {
			if state.TerminalFloatingViewID(floating.ID) == viewID {
				return liveAttachViewTarget{PaneID: floating.Pane.ID, FloatingID: floating.ID}, true
			}
		}
	}
	return liveAttachViewTarget{}, false
}

func invalidateCopyModeForTerminalRebind(root state.Root, paneID string, viewID string, terminalID string) state.Root {
	if !copyModeInputContext(root.CopyMode) || terminalID == "" || root.CopyMode.TerminalID == terminalID {
		return root
	}
	sameView := viewID != "" && root.CopyMode.ViewID == viewID
	samePane := paneID != "" && root.CopyMode.PaneID == paneID
	if !sameView && !samePane {
		return root
	}
	if root.CopyMode.Entering {
		root.History = root.History.InvalidateWindow()
		root.History.TerminalID = terminalID
		root.CopyMode = state.CopyModeStore{}
		return root
	}
	// 当前 pane/view 已经重绑到新的 terminal，旧 frozen history 不能继续留在屏幕上。
	root.History = root.History.InvalidateWindow()
	root.History.TerminalID = terminalID
	root.CopyMode.PaneID = paneID
	root.CopyMode.ViewID = viewID
	root.CopyMode.TerminalID = terminalID
	root.CopyMode.BoundToken = ""
	root.CopyMode.BoundCols = 0
	root.CopyMode.ViewportTop = 0
	root.CopyMode.Cursor = state.CopyPosition{}
	root.CopyMode.Mark = nil
	root.CopyMode.Selection = nil
	root.CopyMode.Query = ""
	root.CopyMode.Matches = nil
	root.CopyMode.ActiveMatch = 0
	root.CopyMode.Empty = true
	return root
}

func liveEffects(terminalID string, cols int, rows int, deps LiveDeps) []Effect {
	effects := liveSurfaceEffect(terminalID, cols, rows, deps)
	effects = append(effects, liveStreamEffect(terminalID, cols, rows, deps)...)
	return effects
}

func reduceLiveLifecycleQuery(root state.Root, msg LiveLifecycleQueryMsg, deps LiveDeps) (state.Root, []Effect) {
	if len(msg.Targets) == 0 {
		return root, nil
	}
	seen := map[string]struct{}{}
	effects := make([]Effect, 0, len(msg.Targets))
	for _, target := range msg.Targets {
		terminalID := strings.TrimSpace(target.TerminalID)
		if terminalID == "" {
			continue
		}
		if _, ok := seen[terminalID]; ok {
			continue
		}
		seen[terminalID] = struct{}{}
		cols, rows := target.Cols, target.Rows
		if cols <= 0 {
			cols = 80
		}
		if rows <= 0 {
			rows = 24
		}
		// 中文说明：这是按需向 core 查询 terminal lifecycle，不把 running/exited 权威性缓存进 TUI。
		logLifecycleTrace(deps.Logger, "live.lifecycle.query",
			"reason", msg.Reason,
			"terminal_id", terminalID,
			"cols", cols,
			"rows", rows,
			"bindings", lifecycleTerminalViewBindingsSummary(root.TerminalViews.BindingsForTerminal(terminalID)),
		)
		effects = append(effects, liveSurfaceEffect(terminalID, cols, rows, deps)...)
	}
	return root, effects
}

func liveSurfaceEffect(terminalID string, cols int, rows int, deps LiveDeps) []Effect {
	source, ok := deps.Terminal.(services.TerminalSurfaceService)
	if !ok || terminalID == "" {
		return nil
	}
	return []Effect{FuncEffect{
		Async:            true,
		ForceSyncInTests: true,
		Run: func(ctx context.Context) Msg {
			result, err := source.LiveSurface(ctx, services.TerminalSurfaceRequest{
				TerminalID: terminalID,
				Cols:       cols,
				Rows:       rows,
			})
			if err != nil {
				logEffectError(deps.Logger, "live.surface", err, "terminal_id", terminalID)
				if isContextLifecycleError(err) {
					return nil
				}
				return LiveSurfaceMsg{Snapshot: state.LiveSurfaceSnapshot{TerminalID: terminalID}, Err: err}
			}
			if result.Snapshot.TerminalID == "" {
				result.Snapshot.TerminalID = terminalID
			}
			if result.Snapshot.Cols == 0 {
				result.Snapshot.Cols = cols
			}
			if result.Snapshot.Rows == 0 {
				result.Snapshot.Rows = rows
			}
			return LiveSurfaceMsg{Snapshot: result.Snapshot, LifecycleKnown: result.LifecycleKnown}
		},
	}}
}

func liveStreamEffect(terminalID string, cols int, rows int, deps LiveDeps) []Effect {
	source, ok := deps.Terminal.(services.TerminalLiveEventService)
	if !ok || terminalID == "" {
		return nil
	}
	token := liveStreamTokenForTerminal(terminalID)
	return []Effect{
		CancelEffect{Token: token},
		StreamEffect{
			Token: token,
			Run: func(ctx context.Context, post func(Msg)) {
				events, err := source.LiveEvents(ctx, services.TerminalLiveEventRequest{
					TerminalID: terminalID,
					Cols:       cols,
					Rows:       rows,
				})
				if err != nil {
					logEffectError(deps.Logger, "live.events", err, "terminal_id", terminalID)
					if isContextLifecycleError(err) {
						return
					}
					post(LiveEventMsg{Event: services.TerminalLiveEvent{TerminalID: terminalID, Err: err}})
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
						if event.TerminalID == "" {
							event.TerminalID = terminalID
						}
						post(LiveEventMsg{Event: event})
					}
				}
			},
		},
	}
}

func liveStreamTokenForTerminal(terminalID string) CancelToken {
	return CancelToken(liveStreamTokenPrefix + terminalID)
}

func maybeRefreshFloatingAutoFit(root state.Root, terminalID string) (state.Root, []Effect) {
	if terminalID == "" {
		return root.Advance(), nil
	}
	shell := root.Shell.EnsureDefaults()
	for _, floating := range shell.ActiveFloatings() {
		if floating.Pane.TerminalID != terminalID || floating.FitMode != state.FloatingFitAuto {
			continue
		}
		next, effects := reduceFloatingCommand(root, state.FloatingCommand{
			Action:   state.FloatingCommandRefreshAutoFit,
			TargetID: floating.ID,
			Source:   state.PaneCommandSourceTest,
		})
		if len(effects) == 0 {
			return next, nil
		}
		return next, effects
	}
	return root.Advance(), nil
}

func liveResizeTerminalID(root state.Root, msg LiveResizeResultMsg) string {
	if msg.ViewID != "" {
		if binding, ok := root.TerminalViews.Views[msg.ViewID]; ok {
			return binding.TerminalID
		}
	}
	return root.Session.TerminalID
}

func reduceLiveEvent(root state.Root, msg LiveEventMsg, deps LiveDeps) (state.Root, []Effect) {
	event := msg.Event
	if event.TerminalID == "" {
		event.TerminalID = root.Surface.TerminalID
	}
	if event.Err != nil {
		if isContextLifecycleError(event.Err) {
			return root, nil
		}
		if next, ok := markTerminalExitedFromError(root, event.TerminalID, event.Err); ok {
			return next.Advance(), nil
		}
		if event.TerminalID == "" || event.TerminalID == root.Surface.TerminalID {
			root.Surface = root.Surface.SetError(event.Err.Error())
			root.Session = root.Session.SetError(event.Err.Error())
			return root.Advance(), nil
		}
		root.Surface = root.Surface.ApplySnapshot(state.LiveSurfaceSnapshot{TerminalID: event.TerminalID, Err: event.Err.Error(), State: state.TerminalLiveError})
		return root.Advance(), nil
	}
	if event.Metadata {
		root.TerminalPool = root.TerminalPool.ApplyTagsEdited(event.TerminalID, event.Tags, "")
		root.TerminalViews = root.TerminalViews.ApplyTerminalSizeLock(event.TerminalID, terminalmeta.SizeLocked(event.Tags))
		return root.Advance(), nil
	}
	if event.Exited {
		event.Snapshot.State = state.TerminalLiveExited
		event.Snapshot.ExitCode = event.ExitCode
		event.Snapshot.ExitReason = event.Reason
		event.Snapshot.ExitedAt = event.ExitedAt
		event.Snapshot.Command = append([]string(nil), event.Command...)
		root.Surface = root.Surface.MarkExitedWithMetadata(event.TerminalID, event.ExitCode, event.Reason, event.ExitedAt, event.Command)
		if event.TerminalID == root.Session.TerminalID {
			root.Session = root.Session.MarkExitedWithMetadata(event.TerminalID, event.ExitCode, event.Reason, event.ExitedAt, event.Command)
		}
	}
	if event.Ready {
		if event.Snapshot.TerminalID == "" {
			event.Snapshot.TerminalID = event.TerminalID
		}
		root.Surface = root.Surface.ApplySnapshotWithLifecycle(event.Snapshot, event.LifecycleKnown)
		if event.LifecycleKnown && event.Snapshot.State == state.TerminalLiveAttached && event.Snapshot.TerminalID == root.Session.TerminalID {
			root.Session = root.Session.MarkAttached(event.Snapshot.TerminalID)
		}
		logLifecycleTrace(deps.Logger, "live.event",
			"terminal_id", event.Snapshot.TerminalID,
			"snapshot_state", string(event.Snapshot.State),
			"lifecycle_known", event.LifecycleKnown,
			"surface_state", string(root.Surface.State),
			"session_state", string(root.Session.State),
			"active_terminal", lifecycleActiveTerminalID(root),
		)
		return maybeRefreshFloatingAutoFit(root, event.Snapshot.TerminalID)
	}
	return root.Advance(), nil
}

type liveInputTargetInfo struct {
	PaneID      string
	FloatingID  string
	ViewID      string
	TerminalID  string
	Channel     uint16
	ResizeRole  string
	SurfaceID   string
	DesiredCols int
	DesiredRows int
	Floating    bool
}

func liveInputTarget(root state.Root) (liveInputTargetInfo, bool) {
	shell := root.Shell.EnsureDefaults()
	if activeFloatingID := shell.ActiveFloatingID(); activeFloatingID != "" {
		binding, ok := root.TerminalViews.FloatingBinding(activeFloatingID)
		if !ok || binding.TerminalID == "" {
			return liveInputTargetInfo{}, false
		}
		return liveInputTargetFromBinding(binding), true
	}
	pane, ok := shell.Pane(state.PaneCommandTarget{PaneID: shell.ActivePaneID})
	if !ok {
		return liveInputTargetInfo{}, false
	}
	binding, ok := root.TerminalViews.PaneBinding(pane.ID)
	if !ok || binding.TerminalID == "" {
		return liveInputTargetInfo{}, false
	}
	return liveInputTargetFromBinding(binding), true
}

func liveInputTargetForView(root state.Root, viewID string) (liveInputTargetInfo, bool) {
	if viewID == "" {
		return liveInputTargetInfo{}, false
	}
	binding, ok := root.TerminalViews.Views[viewID]
	if !ok || binding.TerminalID == "" {
		return liveInputTargetInfo{}, false
	}
	return liveInputTargetFromBinding(binding), true
}

func liveInputTargetFromBinding(binding state.TerminalViewBinding) liveInputTargetInfo {
	info := liveInputTargetInfo{
		PaneID:      binding.PaneID,
		FloatingID:  binding.FloatingID,
		ViewID:      binding.ViewID,
		TerminalID:  binding.TerminalID,
		Channel:     binding.Channel,
		ResizeRole:  binding.ResizeRole,
		SurfaceID:   binding.SurfaceID,
		DesiredCols: binding.DesiredCols,
		DesiredRows: binding.DesiredRows,
		Floating:    binding.FloatingID != "",
	}
	if info.SurfaceID == "" {
		info.SurfaceID = "termx-tui-v3"
	}
	return info
}

func liveAttachForInputEffect(root state.Root, target liveInputTargetInfo, event input.InputEvent, bytes []byte, deps LiveDeps) Effect {
	payload := append([]byte(nil), bytes...)
	req := liveInputAttachRequest(root, target)
	return FuncEffect{
		Async:            true,
		ForceSyncInTests: true,
		Run: func(ctx context.Context) Msg {
			result, err := deps.Terminal.Attach(ctx, req)
			return LiveInputAttachResultMsg{Target: target, Event: event, Bytes: payload, Result: result, Err: err}
		},
	}
}

func liveInputAttachRequest(root state.Root, target liveInputTargetInfo) services.TerminalAttachRequest {
	cols, rows := liveInputAttachSize(root, target)
	resizePolicy := target.ResizeRole
	if resizePolicy == "" {
		resizePolicy = state.TerminalResizeRoleFollower
	}
	surfaceID := target.SurfaceID
	if surfaceID == "" {
		surfaceID = "termx-tui-v3"
	}
	return services.TerminalAttachRequest{
		TerminalID:   target.TerminalID,
		Cols:         cols,
		Rows:         rows,
		Mode:         "collaborator",
		ResizePolicy: resizePolicy,
		SurfaceID:    surfaceID,
		ViewID:       target.ViewID,
	}
}

func liveInputAttachSize(root state.Root, target liveInputTargetInfo) (int, int) {
	if rect, ok := terminalPoolTargetContentRect(root, terminalPoolTarget{PaneID: target.PaneID, FloatingID: target.FloatingID, ViewID: target.ViewID}, render.Rect{}); ok {
		return rect.W, rect.H
	}
	if target.DesiredCols > 0 && target.DesiredRows > 0 {
		return target.DesiredCols, target.DesiredRows
	}
	cols, rows := liveAttachContentSize(root, LiveConfig{Cols: root.Session.Cols, Rows: root.Session.Rows})
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	return cols, rows
}

func reduceLiveInputAttachResult(root state.Root, msg LiveInputAttachResultMsg, deps LiveDeps) (state.Root, []Effect) {
	if msg.Err != nil {
		if isContextLifecycleError(msg.Err) {
			return root, nil
		}
		if next, ok := markTerminalExitedFromError(root, msg.Target.TerminalID, msg.Err); ok {
			return next.Advance(), nil
		}
		root = setLiveInputError(root, msg.Target.TerminalID, msg.Err.Error())
		return root.Advance(), nil
	}
	result := msg.Result
	if result.TerminalID == "" {
		result.TerminalID = msg.Target.TerminalID
	}
	if result.ViewID == "" {
		result.ViewID = msg.Target.ViewID
	}
	if result.SurfaceID == "" {
		result.SurfaceID = msg.Target.SurfaceID
	}
	if result.ResizePolicy == "" {
		result.ResizePolicy = msg.Target.ResizeRole
	}
	if result.ResizePolicy == "" {
		result.ResizePolicy = state.TerminalResizeRoleFollower
	}
	if result.Cols <= 0 || result.Rows <= 0 {
		cols, rows := liveInputAttachSize(root, msg.Target)
		if result.Cols <= 0 {
			result.Cols = cols
		}
		if result.Rows <= 0 {
			result.Rows = rows
		}
	}
	next, effects := reduceLiveAttachResult(root, LiveAttachResultMsg{TerminalID: msg.Target.TerminalID, Result: result}, deps)
	target, ok := liveInputTargetForView(next, result.ViewID)
	if !ok || target.Channel == 0 {
		return next, effects
	}
	effects = append(effects, terminalSendInputEffect(target, msg.Event, msg.Bytes, false, deps))
	return next, effects
}

func liveMousePassthroughEnabled(root state.Root, event input.InputEvent, target liveInputTargetInfo) bool {
	if event.Kind != input.EventKindMouse || event.RawSeq == "" {
		return false
	}
	if root.Shell.EnsureDefaults().Overlay.Open || copyModeOwnsActiveInput(root) {
		return false
	}
	if target.TerminalID == "" {
		return false
	}
	return root.Surface.SurfaceForTerminal(target.TerminalID).Modes.MousePassthroughEnabled()
}

func reduceLiveResize(root state.Root, msg LiveResizeMsg, deps LiveDeps) (state.Root, []Effect) {
	if deps.Terminal == nil {
		return setLiveError(root, "terminal service missing"), nil
	}
	if !root.Session.Attached {
		return setLiveError(root, "terminal is not attached"), nil
	}
	if msg.ViewID == "" {
		root, msg = adoptActiveOwnerResizeBinding(root, msg)
	}
	if msg.Seq == 0 {
		root.Session = root.Session.RequestResize(msg.Cols, msg.Rows)
		msg.Seq = root.Session.ResizeRequestSeq
	}
	session := root.Session
	if msg.ViewID != "" {
		binding, ok := root.TerminalViews.Views[msg.ViewID]
		if !ok {
			// view 已经被关闭后，排队里的旧 resize 请求不能借当前 session 的 channel 再发一遍；
			// 否则 close pane 后会把新 owner 已恢复的 PTY size 又改回旧尺寸。
			return root, nil
		}
		session.TerminalID = binding.TerminalID
		session.Channel = binding.Channel
		session.SurfaceID = binding.SurfaceID
		session.ViewID = binding.ViewID
		// 中文说明：view-scoped resize 的权限身份必须来自目标 binding；
		// restore 多个 view 后，全局 session 可能已被最后一个 follower attach 覆盖。
		session.ResizePolicy = binding.ResizeRole
	}
	return root, []Effect{FuncEffect{
		Async:            true,
		ForceSyncInTests: true,
		Run: func(ctx context.Context) Msg {
			result, err := deps.Terminal.Resize(ctx, services.TerminalResizeRequest{
				TerminalID:   session.TerminalID,
				Channel:      session.Channel,
				Cols:         msg.Cols,
				Rows:         msg.Rows,
				ResizePolicy: session.ResizePolicy,
				SurfaceID:    session.SurfaceID,
				ViewID:       session.ViewID,
			})
			return LiveResizeResultMsg{Result: result, Cols: msg.Cols, Rows: msg.Rows, Seq: msg.Seq, ViewID: msg.ViewID, Err: err}
		},
	}}
}

func adoptActiveOwnerResizeBinding(root state.Root, msg LiveResizeMsg) (state.Root, LiveResizeMsg) {
	viewID := root.Session.ViewID
	if viewID == "" {
		return root, msg
	}
	binding, ok := root.TerminalViews.Views[viewID]
	if !ok || binding.TerminalID == "" || binding.TerminalID != root.Session.TerminalID {
		return root, msg
	}
	nextViews, decision := root.TerminalViews.RequestViewResize(viewID, msg.Cols, msg.Rows)
	if !decision.Allowed {
		return root, msg
	}
	root.TerminalViews = nextViews
	msg.ViewID = viewID
	// 裸 LiveResizeMsg 如果其实命中当前 owner view，就必须同步 view-local desired size。
	// 否则 resize 结果回来后，stale recovery 还会拿旧 binding 尺寸把 PTY 拉回去。
	if decision.Changed {
		msg.Seq = decision.Seq
	}
	return root, msg
}

func hasResizeControlResult(result services.TerminalResizeResult) bool {
	return result.TerminalID != "" || result.ControlReason != "" || result.SizeLocked || result.ResizeEpoch != 0 || result.OwnerSurfaceID != "" || result.OwnerViewID != "" || result.ResizePolicy != "" || result.SurfaceID != "" || result.ViewID != "" || result.CanResize || result.Resized
}

func resizeControlProjectionFromResult(result services.TerminalResizeResult) state.TerminalResizeControlProjection {
	return state.TerminalResizeControlProjection{
		CanResize:      result.CanResize,
		SizeLocked:     result.SizeLocked,
		ControlReason:  result.ControlReason,
		OwnerSurfaceID: result.OwnerSurfaceID,
		OwnerViewID:    result.OwnerViewID,
		ResizeEpoch:    result.ResizeEpoch,
		ResizeRole:     result.ResizePolicy,
		SurfaceID:      result.SurfaceID,
		ViewID:         result.ViewID,
	}
}

func shouldRecoverOwnerDesiredAfterResizeResult(root state.Root, msg LiveResizeResultMsg) bool {
	if !msg.Result.Resized {
		return false
	}
	terminalID := staleResizeResultTerminalID(root, msg)
	if terminalID == "" {
		return false
	}
	desiredCols, desiredRows, _, ok := desiredResizeForTerminal(root, terminalID)
	if !ok {
		return false
	}
	cols, rows := resolvedResizeResultSize(msg)
	return cols != desiredCols || rows != desiredRows
}

func recoverLatestResizeAfterStaleResult(root state.Root, msg LiveResizeResultMsg) (state.Root, []Effect) {
	terminalID := staleResizeResultTerminalID(root, msg)
	if terminalID == "" {
		return root, nil
	}
	desiredCols, desiredRows, viewID, ok := desiredResizeForTerminal(root, terminalID)
	if !ok {
		return root, nil
	}
	actualCols, actualRows := resolvedResizeResultSize(msg)
	if desiredCols == actualCols && desiredRows == actualRows {
		return root, nil
	}
	// 旧 resize 请求可能已经真正把 PTY 改回过期尺寸；
	// stale guard 丢掉结果后，还要立即重申当前 owner 的最新期望尺寸，避免卡在旧 geometry。
	root.Session = root.Session.RequestResize(desiredCols, desiredRows)
	seq := root.Session.ResizeRequestSeq
	return root, []Effect{FuncEffect{
		Run: func(context.Context) Msg {
			return LiveResizeMsg{TerminalID: terminalID, Cols: desiredCols, Rows: desiredRows, Seq: seq, ViewID: viewID}
		},
	}}
}

func staleResizeResultTerminalID(root state.Root, msg LiveResizeResultMsg) string {
	if msg.Result.TerminalID != "" {
		return msg.Result.TerminalID
	}
	if msg.ViewID != "" {
		if binding, ok := root.TerminalViews.Views[msg.ViewID]; ok {
			return binding.TerminalID
		}
	}
	return root.Session.TerminalID
}

func desiredResizeForTerminal(root state.Root, terminalID string) (int, int, string, bool) {
	if binding, ok := root.TerminalViews.OwnerBinding(terminalID); ok {
		if binding.DesiredCols > 0 && binding.DesiredRows > 0 {
			return binding.DesiredCols, binding.DesiredRows, binding.ViewID, true
		}
	}
	if root.Session.TerminalID != terminalID {
		return 0, 0, "", false
	}
	cols, rows := root.Session.DesiredSize()
	if cols <= 0 || rows <= 0 {
		return 0, 0, "", false
	}
	return cols, rows, root.Session.ViewID, true
}

func resolvedResizeResultSize(msg LiveResizeResultMsg) (int, int) {
	cols, rows := msg.Cols, msg.Rows
	if msg.Result.Cols > 0 {
		cols = msg.Result.Cols
	}
	if msg.Result.Rows > 0 {
		rows = msg.Result.Rows
	}
	return cols, rows
}

func setLiveError(root state.Root, message string) state.Root {
	if message == "" {
		message = "unknown terminal error"
	}
	root.Session = root.Session.SetError(message)
	root.Surface = root.Surface.SetError(message)
	return root.Advance()
}

func markTerminalExitedFromError(root state.Root, terminalID string, err error) (state.Root, bool) {
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "terminal exited") {
		return root, false
	}
	if terminalID == "" {
		terminalID = root.Session.TerminalID
	}
	if terminalID == "" {
		terminalID = root.Surface.TerminalID
	}
	root.Session = root.Session.MarkExited(terminalID, 0, "")
	root.Surface = root.Surface.MarkExited(terminalID, 0, "")
	return root, true
}

func setLiveInputError(root state.Root, terminalID string, message string) state.Root {
	if message == "" {
		message = "unknown terminal input error"
	}
	if terminalID == "" || terminalID == root.Session.TerminalID {
		root.Session = root.Session.SetError(message)
		root.Surface = root.Surface.SetError(message)
		return root
	}
	root.Surface = root.Surface.ApplySnapshot(state.LiveSurfaceSnapshot{
		TerminalID: terminalID,
		State:      state.TerminalLiveError,
		Err:        message,
	})
	return root
}
