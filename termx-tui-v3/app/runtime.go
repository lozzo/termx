package app

import (
	"context"
	"errors"

	"github.com/lozzow/termx/termx-tui-v3/input"
	"github.com/lozzow/termx/termx-tui-v3/render"
	"github.com/lozzow/termx/termx-tui-v3/state"
)

// Msg 是 TUI-v3 runtime 的根消息契约，不绑定 Bubble Tea。
type Msg interface {
	isMsg()
}

// Effect 是 TUI-v3 runtime 的根副作用契约，不等同于 Bubble Tea Cmd。
type Effect interface {
	isEffect()
}

// NoopMsg 是 smoke-test 消息。
type NoopMsg struct{}

func (NoopMsg) isMsg() {}

// QuitMsg 请求 runtime 退出。
type QuitMsg struct{}

func (QuitMsg) isMsg() {}

// InputMsg 是 TerminalHost 输入事件进入 message path 的边界消息。
type InputMsg struct {
	Event input.InputEvent
}

func (InputMsg) isMsg() {}

// HostResizeMsg 是外部 terminal emulator 尺寸进入 reducer-owned state 的入口。
type HostResizeMsg struct {
	Cols int
	Rows int
}

func (HostResizeMsg) isMsg() {}

// TickMsg 是 timer/interval effect 回投后的普通消息。
type TickMsg struct {
	Token CancelToken
}

func (TickMsg) isMsg() {}

// NoopEffect 是无需执行的副作用。
type NoopEffect struct{}

func (NoopEffect) isEffect() {}

// FuncEffect 是 harness 和 service adapter 使用的最小副作用包装。
type FuncEffect struct {
	Token CancelToken
	Run   func(context.Context) Msg
}

func (FuncEffect) isEffect() {}

// BatchEffect 表达多个 effect 的调度组合。
type BatchEffect struct {
	Effects []Effect
}

func (BatchEffect) isEffect() {}

// CancelEffect 请求取消某个 token 绑定的 pending effect。
type CancelEffect struct {
	Token CancelToken
}

func (CancelEffect) isEffect() {}

// CancelToken 用于取消 pending history request、terminal operation 和 timer。
type CancelToken string

// Reducer 保持同步纯边界：输入 state/message，返回新 state/effects。
type Reducer func(state.Root, Msg) (state.Root, []Effect)

// RenderFunc 把 reducer-owned state 投影成 frame。
type RenderFunc func(state.Root) render.Frame

type handledEffect struct{}

func (handledEffect) isEffect() {}

// ComposeReducers 按顺序执行多个 reducer，并合并它们产生的 effects。
func ComposeReducers(reducers ...Reducer) Reducer {
	return func(root state.Root, msg Msg) (state.Root, []Effect) {
		var effects []Effect
		for _, reducer := range reducers {
			if reducer == nil {
				continue
			}
			next, nextEffects := reducer(root, msg)
			root = next
			handled := false
			for _, effect := range nextEffects {
				if _, ok := effect.(handledEffect); ok {
					handled = true
					continue
				}
				effects = append(effects, effect)
			}
			if handled {
				break
			}
		}
		return root, effects
	}
}

// TerminalHost 是宿主 TTY 边界。真实实现负责 raw mode、输入和 FrameSink；
// fake 实现只用于 harness。
type TerminalHost interface {
	Size() (cols int, rows int, err error)
	InputEvents() <-chan input.InputEvent
	FrameSink() render.FrameSink
}

// EffectRunner 执行 effect，并只通过 message path 回投结果。
type EffectRunner interface {
	Run(context.Context, Effect, func(Msg))
	Cancel(CancelToken)
}

var (
	ErrRuntimeStopped = errors.New("app runtime stopped")
	ErrInputQueueFull = errors.New("terminal host input queue full")
)

// AppRuntime 是 TUI-v3 自有单线程消息循环。
type AppRuntime struct {
	state               state.Root
	reduce              Reducer
	render              RenderFunc
	host                TerminalHost
	runner              EffectRunner
	queue               []Msg
	lastHitRegions      []render.HitRegion
	hostSizeInitialized bool
	running             bool
	quit                bool
}

func NewAppRuntime(
	initial state.Root,
	reducer Reducer,
	renderer RenderFunc,
	host TerminalHost,
	runner EffectRunner,
) *AppRuntime {
	if reducer == nil {
		reducer = func(root state.Root, _ Msg) (state.Root, []Effect) {
			return root, nil
		}
	}
	if renderer == nil {
		renderer = func(state.Root) render.Frame { return render.Frame{} }
	}
	if runner == nil {
		runner = NewSyncEffectRunner()
	}
	return &AppRuntime{
		state:  initial,
		reduce: reducer,
		render: renderer,
		host:   host,
		runner: runner,
	}
}

func (runtime *AppRuntime) State() state.Root {
	return runtime.state
}

func (runtime *AppRuntime) Post(msg Msg) error {
	if runtime.quit {
		return ErrRuntimeStopped
	}
	msg = runtime.dispatchMouseHitRegion(msg)
	runtime.queue = append(runtime.queue, msg)
	return nil
}

func (runtime *AppRuntime) Drain(ctx context.Context) error {
	runtime.running = true
	defer func() {
		runtime.running = false
	}()
	runtime.ingestHostInitialSize()
	runtime.ingestHostInput()
	for {
		if len(runtime.queue) == 0 {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		msg := runtime.queue[0]
		copy(runtime.queue, runtime.queue[1:])
		runtime.queue = runtime.queue[:len(runtime.queue)-1]
		if _, ok := msg.(QuitMsg); ok {
			runtime.quit = true
			return nil
		}
		if !runtime.prepareRuntimeMessage(msg) {
			runtime.ingestHostInput()
			if runtime.quit {
				return nil
			}
			continue
		}
		next, effects := runtime.reduce(runtime.state, msg)
		runtime.state = next
		runtime.renderFrame()
		for _, effect := range effects {
			runtime.scheduleEffect(ctx, effect)
		}
		runtime.ingestHostInput()
		if runtime.quit {
			return nil
		}
	}
	return nil
}

func (runtime *AppRuntime) scheduleEffect(ctx context.Context, effect Effect) {
	switch effect := effect.(type) {
	case nil:
		return
	case NoopEffect:
		return
	case BatchEffect:
		for _, child := range effect.Effects {
			runtime.scheduleEffect(ctx, child)
		}
	case CancelEffect:
		runtime.runner.Cancel(effect.Token)
	default:
		runtime.runner.Run(ctx, effect, func(msg Msg) {
			if msg != nil && !runtime.quit {
				runtime.queue = append(runtime.queue, msg)
			}
		})
	}
}

func (runtime *AppRuntime) prepareRuntimeMessage(msg Msg) bool {
	switch msg := msg.(type) {
	case HostResizeMsg:
		next, changed := runtime.state.Viewport.Resize(msg.Cols, msg.Rows)
		if !changed {
			return false
		}
		runtime.state.Viewport = next
		runtime.state = runtime.state.Advance()
		return true
	default:
		return true
	}
}

func (runtime *AppRuntime) ingestHostInitialSize() {
	if runtime.host == nil || runtime.hostSizeInitialized {
		return
	}
	runtime.hostSizeInitialized = true
	cols, rows, err := runtime.host.Size()
	if err != nil || cols <= 0 || rows <= 0 {
		return
	}
	msg := HostResizeMsg{Cols: cols, Rows: rows}
	runtime.queue = append([]Msg{msg}, runtime.queue...)
}

func (runtime *AppRuntime) ingestHostInput() {
	if runtime.host == nil {
		return
	}
	events := runtime.host.InputEvents()
	for {
		select {
		case event := <-events:
			if event.Kind == input.EventKindResize {
				runtime.queue = append(runtime.queue, HostResizeMsg{Cols: event.Cols, Rows: event.Rows})
			} else {
				runtime.queue = append(runtime.queue, runtime.dispatchMouseHitRegion(InputMsg{Event: event}))
			}
		default:
			return
		}
	}
}

func (runtime *AppRuntime) renderFrame() {
	if runtime.host == nil {
		return
	}
	frame := runtime.render(runtime.state)
	runtime.lastHitRegions = cloneRenderHitRegions(frame.HitRegions)
	_ = runtime.host.FrameSink().WriteFrame(frame)
}

func (runtime *AppRuntime) dispatchMouseHitRegion(msg Msg) Msg {
	inputMsg, ok := msg.(InputMsg)
	if !ok || inputMsg.Event.Kind != input.EventKindMouse || inputMsg.Event.Mouse != input.MouseLeft {
		return msg
	}
	region, ok := hitRegionAt(runtime.lastHitRegions, inputMsg.Event)
	if !ok {
		return msg
	}
	if command, ok := PaneCommandFromHitRegion(region); ok {
		return ShellPaneCommandMsg{Command: command}
	}
	switch region.Kind {
	case render.HitRegionToastClose:
		return ShellCloseCurrentToastMsg{}
	case render.HitRegionOverlay:
		return ShellCloseOverlayMsg{}
	case render.HitRegionContentAction:
		return ShellContentActionMsg{ActionID: region.ActionID, PaneID: region.PaneID}
	default:
		return msg
	}
}

func hitRegionAt(regions []render.HitRegion, event input.InputEvent) (render.HitRegion, bool) {
	col := event.Col - 1
	row := event.Row - 1
	if event.Col <= 0 {
		col = event.Col
	}
	if event.Row <= 0 {
		row = event.Row
	}
	for _, region := range regions {
		if pointInRect(col, row, region.Rect) {
			return region, true
		}
	}
	return render.HitRegion{}, false
}

func pointInRect(col int, row int, rect render.Rect) bool {
	return col >= rect.X && col < rect.X+rect.W && row >= rect.Y && row < rect.Y+rect.H
}

func cloneRenderHitRegions(regions []render.HitRegion) []render.HitRegion {
	if len(regions) == 0 {
		return nil
	}
	cloned := make([]render.HitRegion, len(regions))
	copy(cloned, regions)
	return cloned
}

func (runtime *AppRuntime) Running() bool {
	return runtime.running
}

func (runtime *AppRuntime) Quit() bool {
	return runtime.quit
}

// SyncEffectRunner 立即执行 effect，适合 deterministic harness。
type SyncEffectRunner struct {
	canceled map[CancelToken]struct{}
}

func NewSyncEffectRunner() *SyncEffectRunner {
	return &SyncEffectRunner{canceled: make(map[CancelToken]struct{})}
}

func (runner *SyncEffectRunner) Run(ctx context.Context, effect Effect, post func(Msg)) {
	funcEffect, ok := effect.(FuncEffect)
	if !ok || funcEffect.Run == nil {
		return
	}
	if _, canceled := runner.canceled[funcEffect.Token]; canceled && funcEffect.Token != "" {
		return
	}
	msg := funcEffect.Run(ctx)
	if msg != nil {
		post(msg)
	}
}

func (runner *SyncEffectRunner) Cancel(token CancelToken) {
	if token == "" {
		return
	}
	runner.canceled[token] = struct{}{}
}

// FakeTerminalHost 是 runtime harness 使用的 TerminalHost fake。
type FakeTerminalHost struct {
	events chan input.InputEvent
	sink   *FakeFrameSink
	cols   int
	rows   int
}

func NewFakeTerminalHost(buffer int) *FakeTerminalHost {
	if buffer <= 0 {
		buffer = 1
	}
	return &FakeTerminalHost{
		events: make(chan input.InputEvent, buffer),
		sink:   &FakeFrameSink{},
	}
}

func (host *FakeTerminalHost) SendInput(event input.InputEvent) error {
	select {
	case host.events <- event:
		return nil
	default:
		return ErrInputQueueFull
	}
}

func (host *FakeTerminalHost) SetSize(cols int, rows int) {
	host.cols = cols
	host.rows = rows
}

func (host *FakeTerminalHost) Size() (int, int, error) {
	return host.cols, host.rows, nil
}

func (host *FakeTerminalHost) SendResize(cols int, rows int) error {
	host.SetSize(cols, rows)
	select {
	case host.events <- input.InputEvent{Kind: input.EventKindResize, Cols: cols, Rows: rows}:
		return nil
	default:
		return ErrInputQueueFull
	}
}

func (host *FakeTerminalHost) InputEvents() <-chan input.InputEvent {
	return host.events
}

func (host *FakeTerminalHost) FrameSink() render.FrameSink {
	return host.sink
}

func (host *FakeTerminalHost) Frames() []render.Frame {
	return host.sink.Frames()
}

// FakeFrameSink 记录 renderer 输出，供 harness 断言。
type FakeFrameSink struct {
	frames []render.Frame
}

func (sink *FakeFrameSink) WriteFrame(frame render.Frame) error {
	sink.frames = append(sink.frames, frame.Clone())
	return nil
}

func (sink *FakeFrameSink) Frames() []render.Frame {
	frames := make([]render.Frame, len(sink.frames))
	for i, frame := range sink.frames {
		frames[i] = frame.Clone()
	}
	return frames
}
