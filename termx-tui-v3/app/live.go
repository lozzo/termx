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
	initial.Shell = initial.Shell.EnsureDefaults()
	builder := render.NewRenderVMBuilder()
	renderer := render.NewRenderer(render.DefaultTheme())
	return NewAppRuntime(initial, ComposeReducers(NewShellReducer(), NewUIInputReducer(), NewTerminalPoolReducer(live), NewCopyModeReducer(copyMode), NewCopyModeResizeRebindReducer(copyMode), NewLiveReducer(live), NewTerminalLayoutResizeReducer()), func(root state.Root) render.Frame {
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
}

func (LiveSurfaceMsg) isMsg() {}

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
			return reduceLiveAttachResult(root, msg)
		case LiveSurfaceMsg:
			root.Surface = root.Surface.ApplySnapshot(msg.Snapshot)
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
				root.CopyMode = root.CopyMode.Resize(msg.Cols)
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

func reduceLiveAttachResult(root state.Root, msg LiveAttachResultMsg) (state.Root, []Effect) {
	if msg.Err != nil {
		return setLiveError(root, msg.Err.Error()), nil
	}
	root.Session = root.Session.Attach(msg.Result.TerminalID, msg.Result.Channel, msg.Result.Cols, msg.Result.Rows)
	root.Surface.TerminalID = msg.Result.TerminalID
	root.Surface = root.Surface.Resize(msg.Result.Cols, msg.Result.Rows)
	return root.Advance(), nil
}

func reduceLiveInput(root state.Root, msg InputMsg, deps LiveDeps) (state.Root, []Effect) {
	if deps.Terminal == nil {
		return setLiveError(root, "terminal service missing"), nil
	}
	if !root.Session.Attached {
		return setLiveError(root, "terminal is not attached"), nil
	}
	intent := input.Route(msg.Event, root.CopyMode.Active)
	if root.CopyMode.Active && intent.Kind != input.IntentTerminalInput {
		return root, nil
	}
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
				TerminalID: session.TerminalID,
				Channel:    session.Channel,
				Cols:       msg.Cols,
				Rows:       msg.Rows,
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
