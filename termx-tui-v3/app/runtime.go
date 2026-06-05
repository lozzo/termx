package app

import (
	"context"
	"errors"
	"sync"
	"time"

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
	Ticks uint64
}

func (TickMsg) isMsg() {}

// NoopEffect 是无需执行的副作用。
type NoopEffect struct{}

func (NoopEffect) isEffect() {}

// FuncEffect 是 harness 和 service adapter 使用的最小副作用包装。
type FuncEffect struct {
	Token CancelToken
	Async bool
	Run   func(context.Context) Msg
}

func (FuncEffect) isEffect() {}

// StreamEffect 表达会多次回投消息的长生命周期 service 订阅。
type StreamEffect struct {
	Token CancelToken
	Run   func(context.Context, func(Msg))
}

func (StreamEffect) isEffect() {}

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

const (
	defaultToastTickInterval = time.Second
	toastTickToken           = CancelToken("toast.tick")
)

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
	mu                  sync.Mutex
	state               state.Root
	reduce              Reducer
	render              RenderFunc
	host                TerminalHost
	runner              EffectRunner
	queue               []Msg
	lastHitRegions      []render.HitRegion
	mouseDrag           mouseDragState
	hostSizeInitialized bool
	now                 func() time.Time
	toastTickInterval   time.Duration
	lastToastTick       time.Time
	running             bool
	quit                bool
}

type mouseDragState struct {
	Active             bool
	Kind               mouseDragKind
	PaneID             string
	FloatingID         string
	Direction          state.PaneResizeDirection
	SplitPath          string
	ResizeBeforePaneID string
	ResizeAfterPaneID  string
	ResizeBeforeCells  int
	ResizeAfterCells   int
	ResizeGroup        []state.PaneResizeGroupItem
	StartCol           int
	StartRow           int
	LastDelta          int
	LastCol            int
	LastRow            int
}

type mouseDragKind string

const (
	mouseDragPaneResize     mouseDragKind = "pane-resize"
	mouseDragFloatingMove   mouseDragKind = "floating-move"
	mouseDragFloatingResize mouseDragKind = "floating-resize"
)

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
		state:             initial,
		reduce:            reducer,
		render:            renderer,
		host:              host,
		runner:            runner,
		now:               time.Now,
		toastTickInterval: defaultToastTickInterval,
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
	runtime.enqueue(msg)
	return nil
}

func (runtime *AppRuntime) Drain(ctx context.Context) error {
	runtime.running = true
	defer func() {
		runtime.running = false
	}()
	runtime.ingestHostInitialSize()
	runtime.ingestHostInput()
	runtime.enqueueDueToastTick()
	for {
		runtime.enqueueDueToastTick()
		msg, ok := runtime.dequeue()
		if !ok {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
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
		runtime.enqueueDueToastTick()
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
				runtime.enqueue(msg)
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
	runtime.prepend(msg)
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
				runtime.enqueue(HostResizeMsg{Cols: event.Cols, Rows: event.Rows})
			} else {
				runtime.enqueue(runtime.dispatchMouseHitRegion(InputMsg{Event: event}))
			}
		default:
			return
		}
	}
}

func (runtime *AppRuntime) enqueueDueToastTick() {
	if len(runtime.state.Shell.Toasts) == 0 {
		runtime.lastToastTick = time.Time{}
		return
	}
	interval := runtime.toastTickInterval
	if interval <= 0 {
		interval = defaultToastTickInterval
	}
	now := runtime.currentTime()
	if runtime.lastToastTick.IsZero() {
		runtime.lastToastTick = now
		return
	}
	elapsed := now.Sub(runtime.lastToastTick)
	if elapsed < interval {
		return
	}
	ticks := uint64(elapsed / interval)
	if ticks == 0 {
		return
	}
	runtime.lastToastTick = runtime.lastToastTick.Add(time.Duration(ticks) * interval)
	runtime.enqueue(TickMsg{Token: toastTickToken, Ticks: ticks})
}

func (runtime *AppRuntime) enqueue(msg Msg) {
	if msg == nil {
		return
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.queue = append(runtime.queue, msg)
}

func (runtime *AppRuntime) prepend(msg Msg) {
	if msg == nil {
		return
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.queue = append([]Msg{msg}, runtime.queue...)
}

func (runtime *AppRuntime) dequeue() (Msg, bool) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if len(runtime.queue) == 0 {
		return nil, false
	}
	msg := runtime.queue[0]
	copy(runtime.queue, runtime.queue[1:])
	runtime.queue = runtime.queue[:len(runtime.queue)-1]
	return msg, true
}

func (runtime *AppRuntime) currentTime() time.Time {
	if runtime.now != nil {
		return runtime.now()
	}
	return time.Now()
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
	if !ok || inputMsg.Event.Kind != input.EventKindMouse {
		return msg
	}
	if dragMsg, handled := runtime.dispatchMouseDrag(inputMsg.Event); handled {
		return dragMsg
	}
	if inputMsg.Event.Mouse != input.MouseLeft {
		if runtime.mouseEventCanPassthrough(inputMsg.Event) {
			return msg
		}
		if runtime.mouseEventHitsUI(inputMsg.Event) {
			return NoopMsg{}
		}
		return msg
	}
	region, ok := hitRegionAt(runtime.lastHitRegions, inputMsg.Event)
	if !ok {
		return msg
	}
	if region.Kind == render.HitRegionPaneResize {
		if drag, ok := paneResizeDragState(region, inputMsg.Event); ok {
			runtime.mouseDrag = drag
			return NoopMsg{}
		}
	}
	if drag, ok := floatingDragState(region, inputMsg.Event); ok {
		runtime.mouseDrag = drag
		return ShellFloatingCommandMsg{Command: state.FloatingCommand{
			Action:   state.FloatingCommandFocusRaise,
			TargetID: drag.FloatingID,
			Source:   state.PaneCommandSourceMouse,
		}}
	}
	if command, ok := PaneCommandFromHitRegion(region); ok {
		runtime.fillMousePaneCommandDefaults(&command)
		return ShellPaneCommandMsg{Command: command}
	}
	switch region.Kind {
	case render.HitRegionToastClose:
		return ShellCloseCurrentToastMsg{}
	case render.HitRegionToast:
		return NoopMsg{}
	case render.HitRegionOverlay:
		return ShellCloseOverlayMsg{}
	case render.HitRegionHistoryRow:
		col := inputMsg.Event.Col - region.Rect.X - 1
		if inputMsg.Event.Col <= 0 {
			col = 0
		}
		col -= 2
		if col < 0 {
			col = 0
		}
		return CopyModeMouseSelectMsg{Position: state.CopyPosition{Row: region.Row, Col: col}}
	case render.HitRegionContentAction:
		return ShellContentActionMsg{ActionID: region.ActionID, PaneID: region.PaneID, Row: region.Row}
	default:
		return msg
	}
}

func (runtime *AppRuntime) mouseEventCanPassthrough(event input.InputEvent) bool {
	if event.RawSeq == "" {
		return false
	}
	region, ok := hitRegionAt(runtime.lastHitRegions, event)
	if !ok || region.Kind != render.HitRegionPaneContent {
		return false
	}
	if region.PaneID != runtime.state.Shell.EnsureDefaults().ActivePaneID {
		return false
	}
	return runtime.paneMouseTrackingEnabled(region.PaneID)
}

func (runtime *AppRuntime) mouseEventHitsUI(event input.InputEvent) bool {
	region, ok := hitRegionAt(runtime.lastHitRegions, event)
	if !ok {
		return false
	}
	switch region.Kind {
	case render.HitRegionPaneContent, render.HitRegionHistoryRow:
		return false
	default:
		return true
	}
}

func (runtime *AppRuntime) paneMouseTrackingEnabled(paneID string) bool {
	if paneID == "" {
		return false
	}
	pane, ok := runtime.state.Shell.PaneByID(paneID)
	if !ok {
		return false
	}
	terminalID := pane.TerminalID
	if terminalID == "" && pane.Active {
		terminalID = runtime.state.Session.TerminalID
	}
	surface := runtime.state.Surface.SurfaceForTerminal(terminalID)
	return surface.Modes.MousePassthroughEnabled()
}

func (runtime *AppRuntime) fillMousePaneCommandDefaults(command *state.PaneCommand) {
	if command == nil || command.Action != state.PaneCommandSplit || command.NewPane.ID != "" {
		return
	}
	command.NewPane = state.PaneState{ID: nextKeyboardPaneID(runtime.state.Shell), Title: "pane", Kind: state.PaneEmpty}
}

func (runtime *AppRuntime) dispatchMouseDrag(event input.InputEvent) (Msg, bool) {
	switch event.Mouse {
	case input.MouseLeftUp:
		if runtime.mouseDrag.Active {
			runtime.mouseDrag = mouseDragState{}
			return NoopMsg{}, true
		}
		return nil, false
	case input.MouseLeftDrag:
		if !runtime.mouseDrag.Active {
			return nil, false
		}
		switch runtime.mouseDrag.Kind {
		case mouseDragPaneResize:
			delta := mouseDragResizeDelta(runtime.mouseDrag, event)
			step := delta - runtime.mouseDrag.LastDelta
			if step == 0 {
				return NoopMsg{}, true
			}
			runtime.mouseDrag.LastDelta = delta
			runtime.mouseDrag.LastCol = event.Col
			runtime.mouseDrag.LastRow = event.Row
			return ShellPaneCommandMsg{Command: state.PaneCommand{
				Action:           state.PaneCommandResize,
				Target:           state.PaneCommandTarget{PaneID: runtime.mouseDrag.PaneID},
				ResizeDirection:  runtime.mouseDrag.Direction,
				ResizeSplitPath:  runtime.mouseDrag.SplitPath,
				ResizeGroupCells: mouseDragResizeGroupCells(runtime.mouseDrag, delta),
				Delta:            step,
				Source:           state.PaneCommandSourceMouse,
			}}, true
		case mouseDragFloatingMove:
			deltaX := event.Col - runtime.mouseDrag.LastCol
			deltaY := event.Row - runtime.mouseDrag.LastRow
			if deltaX == 0 && deltaY == 0 {
				return NoopMsg{}, true
			}
			runtime.mouseDrag.LastCol = event.Col
			runtime.mouseDrag.LastRow = event.Row
			return ShellFloatingCommandMsg{Command: state.FloatingCommand{
				Action:   state.FloatingCommandMove,
				TargetID: runtime.mouseDrag.FloatingID,
				DeltaX:   deltaX,
				DeltaY:   deltaY,
				Source:   state.PaneCommandSourceMouse,
			}}, true
		case mouseDragFloatingResize:
			deltaW := event.Col - runtime.mouseDrag.LastCol
			deltaH := event.Row - runtime.mouseDrag.LastRow
			if deltaW == 0 && deltaH == 0 {
				return NoopMsg{}, true
			}
			runtime.mouseDrag.LastCol = event.Col
			runtime.mouseDrag.LastRow = event.Row
			return ShellFloatingCommandMsg{Command: state.FloatingCommand{
				Action:   state.FloatingCommandResize,
				TargetID: runtime.mouseDrag.FloatingID,
				DeltaW:   deltaW,
				DeltaH:   deltaH,
				Source:   state.PaneCommandSourceMouse,
			}}, true
		default:
			return NoopMsg{}, true
		}
	default:
		return nil, false
	}
}

func paneResizeDragState(region render.HitRegion, event input.InputEvent) (mouseDragState, bool) {
	direction, ok := paneResizeDirectionFromHitRegion(region)
	if !ok || region.PaneID == "" {
		return mouseDragState{}, false
	}
	return mouseDragState{
		Active:             true,
		Kind:               mouseDragPaneResize,
		PaneID:             region.PaneID,
		Direction:          direction,
		SplitPath:          region.SplitPath,
		ResizeBeforePaneID: region.ResizeBeforePaneID,
		ResizeAfterPaneID:  region.ResizeAfterPaneID,
		ResizeBeforeCells:  region.ResizeBeforeCells,
		ResizeAfterCells:   region.ResizeAfterCells,
		ResizeGroup:        paneResizeGroupFromHitRegion(region),
		StartCol:           event.Col,
		StartRow:           event.Row,
		LastCol:            event.Col,
		LastRow:            event.Row,
	}, true
}

func paneResizeGroupFromHitRegion(region render.HitRegion) []state.PaneResizeGroupItem {
	if len(region.ResizeGroup) < 3 {
		return nil
	}
	out := make([]state.PaneResizeGroupItem, 0, len(region.ResizeGroup))
	for _, item := range region.ResizeGroup {
		out = append(out, state.PaneResizeGroupItem{PaneID: item.PaneID, Cells: item.Cells, DeltaSign: item.DeltaSign})
	}
	return out
}

func mouseDragResizeGroupCells(drag mouseDragState, delta int) []state.PaneResizeGroupItem {
	if len(drag.ResizeGroup) == 0 || drag.ResizeBeforePaneID == "" || drag.ResizeAfterPaneID == "" {
		return nil
	}
	beforeCells := drag.ResizeBeforeCells + delta
	afterCells := drag.ResizeAfterCells - delta
	if beforeCells <= 0 || afterCells <= 0 {
		return nil
	}
	out := make([]state.PaneResizeGroupItem, len(drag.ResizeGroup))
	for i, item := range drag.ResizeGroup {
		out[i] = item
		if item.DeltaSign != 0 {
			out[i].Cells = item.Cells + delta*item.DeltaSign
			if out[i].Cells <= 0 {
				return nil
			}
			continue
		}
		switch item.PaneID {
		case drag.ResizeBeforePaneID:
			out[i].Cells = beforeCells
		case drag.ResizeAfterPaneID:
			out[i].Cells = afterCells
		}
	}
	return out
}

func floatingDragState(region render.HitRegion, event input.InputEvent) (mouseDragState, bool) {
	if region.Kind != render.HitRegionContentAction || region.PaneID == "" {
		return mouseDragState{}, false
	}
	var kind mouseDragKind
	switch region.ActionID {
	case "floating.move-drag":
		kind = mouseDragFloatingMove
	case "floating.resize-drag":
		kind = mouseDragFloatingResize
	default:
		return mouseDragState{}, false
	}
	return mouseDragState{
		Active:     true,
		Kind:       kind,
		FloatingID: region.PaneID,
		LastCol:    event.Col,
		LastRow:    event.Row,
	}, true
}

func paneResizeDirectionFromHitRegion(region render.HitRegion) (state.PaneResizeDirection, bool) {
	switch region.Direction {
	case string(state.PaneResizeLeft):
		return state.PaneResizeLeft, true
	case string(state.PaneResizeRight), "":
		return state.PaneResizeRight, true
	case string(state.PaneResizeUp):
		return state.PaneResizeUp, true
	case string(state.PaneResizeDown):
		return state.PaneResizeDown, true
	default:
		return "", false
	}
}

func mouseDragResizeDelta(drag mouseDragState, event input.InputEvent) int {
	switch drag.Direction {
	case state.PaneResizeLeft, state.PaneResizeRight:
		return event.Col - drag.StartCol
	case state.PaneResizeUp, state.PaneResizeDown:
		return event.Row - drag.StartRow
	default:
		return 0
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
	for i := range cloned {
		if len(cloned[i].ResizeGroup) > 0 {
			cloned[i].ResizeGroup = append([]render.ResizeGroupItem(nil), cloned[i].ResizeGroup...)
		}
	}
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
	switch effect := effect.(type) {
	case FuncEffect:
		if effect.Run == nil {
			return
		}
		if _, canceled := runner.canceled[effect.Token]; canceled && effect.Token != "" {
			return
		}
		msg := effect.Run(ctx)
		if msg != nil {
			post(msg)
		}
	case StreamEffect:
		if effect.Run == nil {
			return
		}
		if _, canceled := runner.canceled[effect.Token]; canceled && effect.Token != "" {
			return
		}
		effect.Run(ctx, post)
	default:
		return
	}
}

func (runner *SyncEffectRunner) Cancel(token CancelToken) {
	if token == "" {
		return
	}
	runner.canceled[token] = struct{}{}
}

// AsyncEffectRunner 同步执行普通 effect，异步执行标记为 Async 的 FuncEffect 和 StreamEffect。
type AsyncEffectRunner struct {
	mu      sync.Mutex
	nextID  uint64
	cancels map[CancelToken]asyncEffectHandle
}

type asyncEffectHandle struct {
	ID     uint64
	Cancel context.CancelFunc
}

func NewAsyncEffectRunner() *AsyncEffectRunner {
	return &AsyncEffectRunner{cancels: make(map[CancelToken]asyncEffectHandle)}
}

func (runner *AsyncEffectRunner) Run(ctx context.Context, effect Effect, post func(Msg)) {
	switch effect := effect.(type) {
	case FuncEffect:
		if effect.Run == nil {
			return
		}
		if effect.Async {
			runner.runAsyncFunc(ctx, effect, post)
			return
		}
		msg := effect.Run(ctx)
		if msg != nil {
			post(msg)
		}
	case StreamEffect:
		if effect.Run == nil {
			return
		}
		runner.runStream(ctx, effect, post)
	}
}

func (runner *AsyncEffectRunner) runAsyncFunc(ctx context.Context, effect FuncEffect, post func(Msg)) {
	effectCtx, done := runner.start(effect.Token, ctx)
	go func() {
		defer done()
		msg := effect.Run(effectCtx)
		if msg != nil {
			post(msg)
		}
	}()
}

func (runner *AsyncEffectRunner) runStream(ctx context.Context, effect StreamEffect, post func(Msg)) {
	effectCtx, done := runner.start(effect.Token, ctx)
	go func() {
		defer done()
		effect.Run(effectCtx, post)
	}()
}

func (runner *AsyncEffectRunner) start(token CancelToken, parent context.Context) (context.Context, func()) {
	if token == "" {
		ctx, cancel := context.WithCancel(parent)
		return ctx, cancel
	}
	runner.Cancel(token)
	ctx, cancel := context.WithCancel(parent)
	runner.mu.Lock()
	runner.nextID++
	id := runner.nextID
	runner.cancels[token] = asyncEffectHandle{ID: id, Cancel: cancel}
	runner.mu.Unlock()
	return ctx, func() {
		runner.mu.Lock()
		if current := runner.cancels[token]; current.ID == id {
			delete(runner.cancels, token)
		}
		runner.mu.Unlock()
		cancel()
	}
}

func (runner *AsyncEffectRunner) Cancel(token CancelToken) {
	if token == "" {
		return
	}
	runner.mu.Lock()
	handle := runner.cancels[token]
	if handle.Cancel != nil {
		delete(runner.cancels, token)
	}
	runner.mu.Unlock()
	if handle.Cancel != nil {
		handle.Cancel()
	}
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
