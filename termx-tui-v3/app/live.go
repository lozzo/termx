package app

import (
	"context"

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

const liveStreamToken = CancelToken("terminal.live.stream")

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
	return NewAppRuntime(initial, ComposeReducers(NewShellReducer(), NewUIInputReducer(), NewTerminalPoolReducer(live), NewWorkbenchStorageReducer(workbench), NewCopyModeReducer(copyMode), NewCopyModeResizeRebindReducer(copyMode), NewLiveReducer(live), NewTerminalLayoutResizeReducer()), func(root state.Root) render.Frame {
		return renderer.Render(builder.Build(root))
	}, host, runner)
}

type LiveAttachMsg struct {
	Config LiveConfig
}

func (LiveAttachMsg) isMsg() {}

type LiveAttachResultMsg struct {
	Result services.TerminalAttachResult
	Err    error
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
	Cols int
	Rows int
	Seq  uint64
}

func (LiveResizeMsg) isMsg() {}

type LiveResizeResultMsg struct {
	Cols int
	Rows int
	Seq  uint64
	Err  error
}

func (LiveResizeResultMsg) isMsg() {}

type LiveInputResultMsg struct {
	Event input.InputEvent
	Err   error
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
				root.Session = root.Session.SetError(msg.Err.Error())
				root.Surface = root.Surface.SetError(msg.Err.Error())
				return root.Advance(), nil
			}
			return root, nil
		case LiveResizeMsg:
			return reduceLiveResize(root, msg, deps)
		case LiveResizeResultMsg:
			if root.Session.IsStaleResizeResult(msg.Seq) {
				return root, nil
			}
			if msg.Err != nil {
				root.Session = root.Session.SetError(msg.Err.Error())
				root.Surface = root.Surface.SetError(msg.Err.Error())
				return root.Advance(), nil
			}
			nextSession, applied := root.Session.ApplyResizeResult(msg.Seq, msg.Cols, msg.Rows)
			if !applied {
				return root, nil
			}
			root.Session = nextSession
			root.Surface = root.Surface.Resize(msg.Cols, msg.Rows)
			if root.CopyMode.Active && root.CopyMode.BoundCols != msg.Cols {
				root.CopyMode = root.CopyMode.Resize(msg.Cols, msg.Rows)
			} else if root.CopyMode.Active {
				root.CopyMode = root.CopyMode.SetViewRows(msg.Rows)
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
			return LiveAttachResultMsg{Result: result, Err: err}
		},
	}}
}

func reduceLiveAttachResult(root state.Root, msg LiveAttachResultMsg, deps LiveDeps) (state.Root, []Effect) {
	if msg.Err != nil {
		return setLiveError(root, msg.Err.Error()), nil
	}
	root.Session = root.Session.AttachWithResizeOwner(msg.Result.TerminalID, msg.Result.Channel, msg.Result.Cols, msg.Result.Rows, msg.Result.ResizePolicy, msg.Result.SurfaceID, msg.Result.ViewID)
	root.Surface = root.Surface.Attach(msg.Result.TerminalID, msg.Result.Cols, msg.Result.Rows)
	root.Shell = root.Shell.BindPaneTerminal(state.PaneCommandTarget{PaneID: root.Shell.EnsureDefaults().ActivePaneID}, msg.Result.TerminalID)
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
				return LiveSurfaceMsg{Err: err}
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
	return []Effect{
		CancelEffect{Token: liveStreamToken},
		StreamEffect{
			Token: liveStreamToken,
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

func reduceLiveEvent(root state.Root, msg LiveEventMsg) (state.Root, []Effect) {
	event := msg.Event
	if event.TerminalID == "" {
		event.TerminalID = root.Surface.TerminalID
	}
	if event.Err != nil {
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
		root.Surface = root.Surface.ApplySnapshot(event.Snapshot)
	}
	return root.Advance(), nil
}

func reduceLiveInput(root state.Root, msg InputMsg, deps LiveDeps) (state.Root, []Effect) {
	if deps.Terminal == nil {
		return setLiveError(root, "terminal service missing"), nil
	}
	if !root.Session.Attached {
		return setLiveError(root, "terminal is not attached"), nil
	}
	if root.CopyMode.Active {
		return root, []Effect{handledEffect{}}
	}
	intent := input.RouteWithOptions(msg.Event, input.RouteOptions{
		CopyModeActive:           root.CopyMode.Active,
		TerminalMousePassthrough: liveMousePassthroughEnabled(root, msg.Event),
	})
	if intent.Kind != input.IntentTerminalInput || len(intent.Bytes) == 0 {
		return root, nil
	}
	session := root.Session
	return root, []Effect{FuncEffect{
		Run: func(ctx context.Context) Msg {
			err := deps.Terminal.SendInput(ctx, services.TerminalInputRequest{
				TerminalID: session.TerminalID,
				Channel:    session.Channel,
				Event:      msg.Event,
				Bytes:      intent.Bytes,
			})
			return LiveInputResultMsg{Event: msg.Event, Err: err}
		},
	}}
}

func liveMousePassthroughEnabled(root state.Root, event input.InputEvent) bool {
	if event.Kind != input.EventKindMouse || event.RawSeq == "" {
		return false
	}
	if root.Shell.EnsureDefaults().Overlay.Open || root.CopyMode.Active {
		return false
	}
	return root.Surface.Modes.MousePassthroughEnabled()
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
	return root, []Effect{FuncEffect{
		Run: func(ctx context.Context) Msg {
			err := deps.Terminal.Resize(ctx, services.TerminalResizeRequest{
				TerminalID:   session.TerminalID,
				Channel:      session.Channel,
				Cols:         msg.Cols,
				Rows:         msg.Rows,
				ResizePolicy: session.ResizePolicy,
				SurfaceID:    session.SurfaceID,
				ViewID:       session.ViewID,
			})
			return LiveResizeResultMsg{Cols: msg.Cols, Rows: msg.Rows, Seq: msg.Seq, Err: err}
		},
	}}
}

func setLiveError(root state.Root, message string) state.Root {
	if message == "" {
		message = "unknown terminal error"
	}
	root.Session = root.Session.SetError(message)
	root.Surface = root.Surface.SetError(message)
	return root.Advance()
}
