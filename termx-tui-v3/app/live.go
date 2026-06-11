package app

import (
	"context"
	"strings"

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
}

const liveStreamTokenPrefix = "terminal.live.stream:"

// NewLiveRuntime 组合 live app 主路径：TerminalHost 输入 -> reducer/effect ->
// terminal service -> render VM -> FrameSink。
func NewLiveRuntime(initial state.Root, host TerminalHost, runner EffectRunner, deps LiveDeps) *AppRuntime {
	initial.Shell = initial.Shell.EnsureDefaults()
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())
	return NewAppRuntime(initial, ComposeReducers(NewShellReducer(), NewUIInputReducer(), NewTerminalPoolReducer(deps), NewLiveReducer(deps), NewTerminalLayoutResizeReducer()), func(root state.Root) render.Frame {
		return renderer.Render(builder.Build(root))
	}, host, runner)
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
	return NewInteractiveRuntimeWithWorkbench(initial, host, runner, live, copyMode, WorkbenchDeps{})
}

func NewInteractiveRuntimeWithWorkbench(
	initial state.Root,
	host TerminalHost,
	runner EffectRunner,
	live LiveDeps,
	copyMode CopyModeDeps,
	workbench WorkbenchDeps,
) *AppRuntime {
	initial.Shell = initial.Shell.EnsureDefaults()
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())
	runtime := NewAppRuntime(initial, ComposeReducers(NewShellReducer(), NewUIInputReducer(), NewTerminalPoolReducer(live), NewWorkbenchStorageReducer(workbench), NewCopyModeReducer(copyMode), NewCopyModeResizeRebindReducer(copyMode), NewLiveReducer(live), NewTerminalLayoutResizeReducer()), func(root state.Root) render.Frame {
		return renderer.Render(builder.Build(root))
	}, host, runner)
	if workbench.Storage != nil {
		// 启动时先恢复 core-v2 opaque storage 中的 workbench truth，再订阅后续变化。
		runtime.enqueue(WorkbenchStorageLoadRequestMsg{})
		runtime.enqueue(WorkbenchStorageWatchRequestMsg{})
	}
	return runtime
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
	Snapshot state.LiveSurfaceSnapshot
	Err      error
}

func (LiveSurfaceMsg) isMsg() {}

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
	TerminalID string
	Event      input.InputEvent
	Err        error
}

func (LiveInputResultMsg) isMsg() {}

func NewLiveReducer(deps LiveDeps) Reducer {
	return func(root state.Root, msg Msg) (state.Root, []Effect) {
		switch msg := msg.(type) {
		case LiveAttachMsg:
			return reduceLiveAttach(root, msg, deps)
		case LiveAttachResultMsg:
			return reduceLiveAttachResult(root, msg, deps)
		case LiveSurfaceMsg:
			if msg.Err != nil {
				if next, ok := markTerminalExitedFromError(root, msg.Snapshot.TerminalID, msg.Err); ok {
					return next.Advance(), nil
				}
				root.Surface = root.Surface.SetError(msg.Err.Error())
				return root.Advance(), nil
			}
			root.Surface = root.Surface.ApplySnapshot(msg.Snapshot)
			return root.Advance(), nil
		case LiveEventMsg:
			return reduceLiveEvent(root, msg)
		case LiveExitMsg:
			root.Session = root.Session.MarkExited(msg.TerminalID, msg.ExitCode, msg.Reason)
			root.Surface = root.Surface.MarkExited(msg.TerminalID, msg.ExitCode, msg.Reason)
			return root.Advance(), nil
		case InputMsg:
			return reduceLiveInput(root, msg, deps)
		case LiveInputResultMsg:
			if msg.Err != nil {
				if next, ok := markTerminalExitedFromError(root, msg.TerminalID, msg.Err); ok {
					return next.Advance(), nil
				}
				root = setLiveInputError(root, msg.TerminalID, msg.Err.Error())
				return root.Advance(), nil
			}
			return root, nil
		case LiveResizeMsg:
			return reduceLiveResize(root, msg, deps)
		case LiveResizeResultMsg:
			viewScoped := msg.ViewID != ""
			if msg.ViewID != "" {
				if binding, ok := root.TerminalViews.Views[msg.ViewID]; ok && binding.IsStaleResizeResult(msg.Seq) {
					return root, nil
				}
			}
			if !viewScoped && root.Session.IsStaleResizeResult(msg.Seq) {
				return root, nil
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
			return root.Advance(), nil
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
	return root, []Effect{FuncEffect{
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
			return next.Advance(), nil
		}
		return setLiveError(root, msg.Err.Error()), nil
	}
	root.Session = root.Session.AttachWithResizeOwner(msg.Result.TerminalID, msg.Result.Channel, msg.Result.Cols, msg.Result.Rows, msg.Result.ResizePolicy, msg.Result.SurfaceID, msg.Result.ViewID)
	root.Surface = root.Surface.Attach(msg.Result.TerminalID, msg.Result.Cols, msg.Result.Rows)
	viewID := msg.Result.ViewID
	activePaneID := root.Shell.EnsureDefaults().ActivePaneID
	if viewID == "" {
		viewID = state.TerminalPaneViewID(activePaneID)
	}
	if existing, ok := root.TerminalViews.Views[viewID]; ok {
		if existing.PaneID != "" {
			activePaneID = existing.PaneID
		}
		if existing.FloatingID != "" {
			binding := state.NewFloatingTerminalView(existing.FloatingID, existing.PaneID, msg.Result.TerminalID, msg.Result.Channel, msg.Result.Cols, msg.Result.Rows, msg.Result.ResizePolicy, msg.Result.SurfaceID, viewID, msg.Result.CanResize)
			binding.Layout = existing.Layout
			root.TerminalViews = root.TerminalViews.BindFloating(binding)
			root.TerminalViews, _ = root.TerminalViews.ApplyResizeControl(viewID, state.TerminalResizeControlProjection{
				CanResize:      msg.Result.CanResize,
				SizeLocked:     msg.Result.SizeLocked,
				ControlReason:  msg.Result.ControlReason,
				OwnerSurfaceID: msg.Result.OwnerSurfaceID,
				OwnerViewID:    msg.Result.OwnerViewID,
				ResizeEpoch:    msg.Result.ResizeEpoch,
				ResizeRole:     msg.Result.ResizePolicy,
				SurfaceID:      msg.Result.SurfaceID,
				ViewID:         viewID,
			})
			return root.Advance(), liveEffects(msg.Result.TerminalID, msg.Result.Cols, msg.Result.Rows, deps)
		}
	}
	root.Shell = root.Shell.EnsureActiveTabForAttach()
	root.Shell = root.Shell.BindPaneTerminal(state.PaneCommandTarget{PaneID: activePaneID}, msg.Result.TerminalID)
	binding := state.NewPaneTerminalView(activePaneID, msg.Result.TerminalID, msg.Result.Channel, msg.Result.Cols, msg.Result.Rows, msg.Result.ResizePolicy, msg.Result.SurfaceID, viewID, msg.Result.CanResize)
	if existing, ok := root.TerminalViews.Views[viewID]; ok {
		binding.Layout = existing.Layout
	}
	root.TerminalViews = root.TerminalViews.BindPane(binding)
	root.TerminalViews, _ = root.TerminalViews.ApplyResizeControl(viewID, state.TerminalResizeControlProjection{
		CanResize:      msg.Result.CanResize,
		SizeLocked:     msg.Result.SizeLocked,
		ControlReason:  msg.Result.ControlReason,
		OwnerSurfaceID: msg.Result.OwnerSurfaceID,
		OwnerViewID:    msg.Result.OwnerViewID,
		ResizeEpoch:    msg.Result.ResizeEpoch,
		ResizeRole:     msg.Result.ResizePolicy,
		SurfaceID:      msg.Result.SurfaceID,
		ViewID:         viewID,
	})
	return root.Advance(), liveEffects(msg.Result.TerminalID, msg.Result.Cols, msg.Result.Rows, deps)
}

func liveEffects(terminalID string, cols int, rows int, deps LiveDeps) []Effect {
	effects := liveSurfaceEffect(terminalID, cols, rows, deps)
	effects = append(effects, liveStreamEffect(terminalID, cols, rows, deps)...)
	return effects
}

func liveSurfaceEffect(terminalID string, cols int, rows int, deps LiveDeps) []Effect {
	source, ok := deps.Terminal.(services.TerminalSurfaceService)
	if !ok || terminalID == "" {
		return nil
	}
	return []Effect{FuncEffect{
		Run: func(ctx context.Context) Msg {
			result, err := source.LiveSurface(ctx, services.TerminalSurfaceRequest{
				TerminalID: terminalID,
				Cols:       cols,
				Rows:       rows,
			})
			if err != nil {
				return LiveSurfaceMsg{Snapshot: state.LiveSurfaceSnapshot{TerminalID: terminalID}, Err: err}
			}
			if !result.Ready {
				return nil
			}
			return LiveSurfaceMsg{Snapshot: result.Snapshot}
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

func reduceLiveEvent(root state.Root, msg LiveEventMsg) (state.Root, []Effect) {
	event := msg.Event
	if event.TerminalID == "" {
		event.TerminalID = root.Surface.TerminalID
	}
	if event.Err != nil {
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
	if event.Exited {
		root.Surface = root.Surface.MarkExited(event.TerminalID, event.ExitCode, event.Reason)
		if event.TerminalID == root.Session.TerminalID {
			root.Session = root.Session.MarkExited(event.TerminalID, event.ExitCode, event.Reason)
		}
	}
	if event.Ready {
		if event.Snapshot.TerminalID == "" {
			event.Snapshot.TerminalID = event.TerminalID
		}
		root.Surface = root.Surface.ApplySnapshot(event.Snapshot)
	}
	return root.Advance(), nil
}

func reduceLiveInput(root state.Root, msg InputMsg, deps LiveDeps) (state.Root, []Effect) {
	if deps.Terminal == nil {
		return setLiveError(root, "terminal service missing"), nil
	}
	if root.CopyMode.Active {
		return root, []Effect{handledEffect{}}
	}
	target, ok := liveInputTarget(root)
	if !ok {
		root.Shell = root.Shell.AddToast(state.ToastSpec{Severity: state.ToastWarning, Title: "terminal.input", Body: "no terminal bound"})
		return root.Advance(), []Effect{handledEffect{}}
	}
	intent := input.RouteWithOptions(msg.Event, input.RouteOptions{
		CopyModeActive:           root.CopyMode.Active,
		TerminalMousePassthrough: liveMousePassthroughEnabled(root, msg.Event, target),
	})
	if intent.Kind != input.IntentTerminalInput || len(intent.Bytes) == 0 {
		return root, nil
	}
	return root, []Effect{FuncEffect{
		Run: func(ctx context.Context) Msg {
			err := deps.Terminal.SendInput(ctx, services.TerminalInputRequest{
				TerminalID: target.TerminalID,
				Channel:    target.Channel,
				Event:      msg.Event,
				Bytes:      intent.Bytes,
			})
			return LiveInputResultMsg{TerminalID: target.TerminalID, Event: msg.Event, Err: err}
		},
	}}
}

type liveInputTargetInfo struct {
	PaneID     string
	TerminalID string
	Channel    uint16
	Floating   bool
}

func liveInputTarget(root state.Root) (liveInputTargetInfo, bool) {
	shell := root.Shell.EnsureDefaults()
	for _, floating := range shell.Floatings {
		if floating.ID != shell.ActiveFloatingID || floating.Pane.TerminalID == "" {
			continue
		}
		target := liveInputTargetInfo{PaneID: floating.Pane.ID, TerminalID: floating.Pane.TerminalID, Floating: true}
		if binding, ok := root.TerminalViews.FloatingBinding(floating.ID); ok && binding.TerminalID == floating.Pane.TerminalID {
			target.Channel = binding.Channel
		} else if channel, ok := root.Session.InputChannelFor(floating.Pane.TerminalID); ok {
			target.Channel = channel
		}
		return target, true
	}
	pane, ok := shell.Pane(state.PaneCommandTarget{PaneID: shell.ActivePaneID})
	if !ok {
		return liveInputTargetInfo{}, false
	}
	terminalID := pane.TerminalID
	if terminalID == "" {
		return liveInputTargetInfo{}, false
	}
	target := liveInputTargetInfo{PaneID: pane.ID, TerminalID: terminalID}
	if binding, ok := root.TerminalViews.PaneBinding(pane.ID); ok && binding.TerminalID == terminalID {
		target.Channel = binding.Channel
	} else if channel, ok := root.Session.InputChannelFor(terminalID); ok {
		target.Channel = channel
	}
	return target, true
}

func liveMousePassthroughEnabled(root state.Root, event input.InputEvent, target liveInputTargetInfo) bool {
	if event.Kind != input.EventKindMouse || event.RawSeq == "" {
		return false
	}
	if root.Shell.EnsureDefaults().Overlay.Open || root.CopyMode.Active {
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
	if msg.Seq == 0 {
		root.Session = root.Session.RequestResize(msg.Cols, msg.Rows)
		msg.Seq = root.Session.ResizeRequestSeq
	}
	session := root.Session
	if msg.ViewID != "" {
		if binding, ok := root.TerminalViews.Views[msg.ViewID]; ok {
			session.TerminalID = binding.TerminalID
			session.Channel = binding.Channel
			session.SurfaceID = binding.SurfaceID
			session.ViewID = binding.ViewID
		}
	}
	return root, []Effect{FuncEffect{
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
