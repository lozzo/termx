package app

import (
	"context"
	"errors"
	"log/slog"
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

type noRenderMsg interface {
	SkipRender() bool
}

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
	// ForceSyncInTests 只给 deterministic harness 使用：真实 runtime 仍按 Async 异步执行，
	// 但 SyncEffectRunner 需要同步跑完该 effect，避免测试必须引入额外 goroutine 等待。
	ForceSyncInTests bool
	Run              func(context.Context) Msg
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
	defaultToastTickInterval   = time.Second
	toastTickToken             = CancelToken("toast.tick")
	defaultMaxMessagesPerBatch = 128
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
	EventsReady() <-chan struct{}
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
	wake                chan struct{}
	state               state.Root
	reduce              Reducer
	render              RenderFunc
	host                TerminalHost
	runner              EffectRunner
	queue               []Msg
	lastHitRegions      []render.HitRegion
	mouseDrag           mouseDragState
	lastMouseAction     mouseActionClickState
	hostSizeInitialized bool
	now                 func() time.Time
	toastTickInterval   time.Duration
	lastToastTick       time.Time
	running             bool
	quit                bool
	firstFrameWritten   bool
	startupFrameReady   bool
	maxMessagesPerBatch int
	copyHistoryPatch    copyHistoryPatchCache
	diagnostics         *runtimeDiagnostics
	stopDiagnostics     func()
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

type mouseActionClickState struct {
	Kind     render.HitRegionKind
	ActionID string
	PaneID   string
	Floating bool
	Row      int
	Col      int
}

type mouseHitResolution struct {
	Foreground    render.HitRegion
	HasForeground bool
	HistoryRow    render.HitRegion
	HasHistoryRow bool
	FocusOwner    render.HitRegion
	HasFocusOwner bool
}

type mouseDragKind string

const (
	mouseDragPaneResize       mouseDragKind = "pane-resize"
	mouseDragFloatingMove     mouseDragKind = "floating-move"
	mouseDragFloatingResize   mouseDragKind = "floating-resize"
	mouseDragClipboardDivider mouseDragKind = "clipboard-divider"
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
		wake:              make(chan struct{}, 1),
		reduce:            reducer,
		render:            renderer,
		host:              host,
		runner:            runner,
		now:               time.Now,
		toastTickInterval: defaultToastTickInterval,
	}
}

func (runtime *AppRuntime) SetLogger(logger *slog.Logger) {
	if runtime.stopDiagnostics != nil {
		runtime.stopDiagnostics()
		runtime.stopDiagnostics = nil
	}
	applyRuntimeTuning(logger)
	runtime.diagnostics = newRuntimeDiagnostics(logger)
	runtime.stopDiagnostics = startRuntimeHeapSignalProfiler(runtime, logger)
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
		runtime.stopRuntimeDiagnostics()
	}()
	return runtime.drainBatch(ctx)
}

// Run 进入真正的事件驱动主循环：处理一批消息后等待下一次唤醒，
// 不依赖外层固定 sleep 轮询。
func (runtime *AppRuntime) Run(ctx context.Context) error {
	runtime.running = true
	defer func() {
		runtime.running = false
		runtime.stopRuntimeDiagnostics()
	}()
	for {
		if err := runtime.drainBatch(ctx); err != nil {
			return err
		}
		if runtime.quit {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !runtime.waitForWake(ctx) {
			return ctx.Err()
		}
	}
}

func (runtime *AppRuntime) stopRuntimeDiagnostics() {
	if runtime.stopDiagnostics == nil {
		return
	}
	runtime.stopDiagnostics()
	runtime.stopDiagnostics = nil
}

func (runtime *AppRuntime) RequestHeapProfile(reason string) {
	if runtime == nil || runtime.diagnostics == nil {
		return
	}
	runtime.diagnostics.RequestHeapProfile(runtime.State(), reason)
}

func (runtime *AppRuntime) RequestMemstats(reason string) {
	if runtime == nil || runtime.diagnostics == nil {
		return
	}
	runtime.diagnostics.RequestMemstats(runtime.State(), reason)
}

func (runtime *AppRuntime) drainBatch(ctx context.Context) error {
	needsRender := false
	processed := 0
	for {
		runtime.ingestHostInitialSize()
		runtime.ingestHostInput()
		runtime.enqueueDueToastTick()
		runtime.enqueueDueToastTick()
		msg, ok := runtime.dequeue()
		if !ok {
			runtime.ingestHostCurrentSize()
			msg, ok = runtime.dequeue()
			if !ok {
				if needsRender {
					runtime.renderFrame()
					needsRender = false
					runtime.ingestHostInput()
					runtime.enqueueDueToastTick()
					continue
				}
				return nil
			}
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
		msg = runtime.prepareRenderPressureMessage(msg)
		next, effects := runtime.reduce(runtime.state, msg)
		runtime.state = next
		runtime.observeRuntimeMessage(msg, effects)
		processed++
		if !messageSkipsRender(msg) {
			needsRender = true
		}
		if _, ok := msg.(HostResizeMsg); ok {
			runtime.renderFrame()
			needsRender = false
		}
		if runtime.shouldWriteFirstFrame() {
			runtime.renderFrame()
			needsRender = false
		}
		for _, effect := range effects {
			runtime.scheduleEffect(ctx, effect)
		}
		runtime.ingestHostInput()
		runtime.enqueueDueToastTick()
		if runtime.quit {
			return nil
		}
		if needsRender && processed >= runtime.messageBatchLimit() {
			runtime.renderFrame()
			return nil
		}
	}
	return nil
}

func (runtime *AppRuntime) messageBatchLimit() int {
	if runtime.maxMessagesPerBatch > 0 {
		return runtime.maxMessagesPerBatch
	}
	return defaultMaxMessagesPerBatch
}

func messageSkipsRender(msg Msg) bool {
	noRender, ok := msg.(noRenderMsg)
	return ok && noRender.SkipRender()
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

func (runtime *AppRuntime) prepareRenderPressureMessage(msg Msg) Msg {
	surface, ok := msg.(LiveSurfaceMsg)
	if !ok || surface.Superseded || surface.Err != nil || surface.LifecycleKnown || !ordinaryLiveSnapshot(surface.Snapshot) {
		return msg
	}
	if !runtime.liveSurfaceHasQueuedOrDirtySuccessor(surface.Snapshot.TerminalID) {
		return msg
	}
	// 中文说明：压力输出时由事件积压和 refresh 背压决定是否丢中间帧；
	// 这里只跳过已被后续 ordinary surface 取代的渲染，不按固定帧率限速。
	surface.Superseded = true
	return surface
}

func (runtime *AppRuntime) liveSurfaceHasQueuedOrDirtySuccessor(terminalID string) bool {
	if terminalID == "" {
		return false
	}
	if refresh, ok := runtime.state.Surface.Refreshes[terminalID]; ok && refresh.Dirty {
		return true
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	for _, queued := range runtime.queue {
		if liveQueueBoundary(queued) {
			return false
		}
		update, ok := queuedOrdinaryLiveUpdate(queued)
		if ok && update.terminalID == terminalID {
			return true
		}
	}
	return false
}

func (runtime *AppRuntime) ingestHostInitialSize() {
	if runtime.host == nil || runtime.hostSizeInitialized {
		return
	}
	runtime.hostSizeInitialized = true
	if runtime.ingestHostCurrentSize() {
		runtime.startupFrameReady = true
	}
}

func (runtime *AppRuntime) ingestHostCurrentSize() bool {
	if runtime.host == nil {
		return false
	}
	cols, rows, err := runtime.host.Size()
	if err != nil || cols <= 0 || rows <= 0 {
		return false
	}
	if runtime.state.Viewport.Valid && runtime.state.Viewport.Cols == cols && runtime.state.Viewport.Rows == rows {
		return false
	}
	msg := HostResizeMsg{Cols: cols, Rows: rows}
	runtime.prepend(msg)
	return true
}

func (runtime *AppRuntime) ingestHostInput() {
	if runtime.host == nil {
		return
	}
	events := runtime.host.InputEvents()
	for {
		select {
		case event := <-events:
			switch event.Kind {
			case input.EventKindResize:
				runtime.enqueue(HostResizeMsg{Cols: event.Cols, Rows: event.Rows})
			case input.EventKindHostTheme:
				runtime.enqueue(HostThemeMsg{Update: state.HostThemeUpdate{
					DefaultFG:    event.Theme.DefaultFG,
					DefaultBG:    event.Theme.DefaultBG,
					PaletteIndex: event.Theme.PaletteIndex,
					PaletteColor: event.Theme.PaletteColor,
				}})
			default:
				msg := runtime.dispatchMouseHitRegion(InputMsg{Event: event})
				runtime.logHostInputEvent(event, msg)
				runtime.enqueue(msg)
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
	if runtime.coalesceQueuedHostResize(msg) {
		runtime.signalWakeLocked()
		return
	}
	if runtime.coalesceQueuedLiveUpdate(msg) {
		runtime.signalWakeLocked()
		return
	}
	if runtime.coalesceQueuedWorkbenchStorage(msg) {
		runtime.signalWakeLocked()
		return
	}
	if runtime.prioritizeQueuedInputLocked(msg) {
		runtime.signalWakeLocked()
		return
	}
	runtime.queue = append(runtime.queue, msg)
	runtime.signalWakeLocked()
}

func (runtime *AppRuntime) prioritizeQueuedInputLocked(msg Msg) bool {
	if _, ok := msg.(InputMsg); !ok {
		return false
	}
	index := firstOrdinaryLiveUpdateIndex(runtime.queue)
	if index < 0 {
		return false
	}
	runtime.insertAtLocked(index, msg)
	return true
}

func (runtime *AppRuntime) coalesceQueuedHostResize(msg Msg) bool {
	incoming, ok := msg.(HostResizeMsg)
	if !ok {
		return false
	}
	first := -1
	original := runtime.queue
	filtered := original[:0]
	for _, queued := range original {
		if _, ok := queued.(HostResizeMsg); ok {
			if first < 0 {
				first = len(filtered)
			}
			continue
		}
		filtered = append(filtered, queued)
	}
	runtime.setQueueFilteredLocked(original, filtered)
	if first < 0 {
		return false
	}
	runtime.insertAtLocked(first, incoming)
	return true
}

func (runtime *AppRuntime) dropQueuedHostResizeLocked() {
	original := runtime.queue
	filtered := original[:0]
	for _, queued := range original {
		if _, ok := queued.(HostResizeMsg); ok {
			continue
		}
		filtered = append(filtered, queued)
	}
	runtime.setQueueFilteredLocked(original, filtered)
}

func (runtime *AppRuntime) insertAtLocked(index int, msg Msg) {
	if index < 0 {
		index = 0
	}
	if index >= len(runtime.queue) {
		runtime.queue = append(runtime.queue, msg)
		return
	}
	runtime.queue = append(runtime.queue, nil)
	copy(runtime.queue[index+1:], runtime.queue[index:])
	runtime.queue[index] = msg
}

func (runtime *AppRuntime) coalesceQueuedLiveUpdate(msg Msg) bool {
	incoming, ok := queuedOrdinaryLiveUpdate(msg)
	if !ok {
		return false
	}
	for i := len(runtime.queue) - 1; i >= 0; i-- {
		queued := runtime.queue[i]
		// 普通 live 帧只能在同一语义基线内合并，不能跨过 resize/exit/attach 等边界。
		if liveQueueBoundary(queued) {
			return false
		}
		existing, ok := queuedOrdinaryLiveUpdate(queued)
		if !ok || existing.terminalID != incoming.terminalID {
			continue
		}
		if liveRevisionNewer(existing.revision, incoming.revision) {
			return true
		}
		runtime.removeAtLocked(i)
		runtime.queue = append(runtime.queue, msg)
		return true
	}
	return false
}

func (runtime *AppRuntime) coalesceQueuedWorkbenchStorage(msg Msg) bool {
	switch msg := msg.(type) {
	case WorkbenchStorageLoadRequestMsg:
		return runtime.replaceQueuedWorkbenchStorageLoadLocked(msg)
	case WorkbenchStoragePersistRequestMsg:
		return runtime.replaceQueuedWorkbenchStoragePersistLocked(msg)
	default:
		return false
	}
}

func (runtime *AppRuntime) replaceQueuedWorkbenchStorageLoadLocked(msg WorkbenchStorageLoadRequestMsg) bool {
	first := -1
	original := runtime.queue
	filtered := original[:0]
	for _, queued := range original {
		if _, ok := queued.(WorkbenchStorageLoadRequestMsg); ok {
			if first < 0 {
				first = len(filtered)
			}
			continue
		}
		filtered = append(filtered, queued)
	}
	if first < 0 {
		return false
	}
	runtime.setQueueFilteredLocked(original, filtered)
	runtime.queue = append(runtime.queue, nil)
	copy(runtime.queue[first+1:], runtime.queue[first:])
	runtime.queue[first] = msg
	return true
}

func (runtime *AppRuntime) replaceQueuedWorkbenchStoragePersistLocked(msg WorkbenchStoragePersistRequestMsg) bool {
	first := -1
	original := runtime.queue
	filtered := original[:0]
	for _, queued := range original {
		if _, ok := queued.(WorkbenchStoragePersistRequestMsg); ok {
			if first < 0 {
				first = len(filtered)
			}
			continue
		}
		filtered = append(filtered, queued)
	}
	if first < 0 {
		return false
	}
	// persist request 不携带 snapshot；真正保存时读取当时 root，因此保留最后一个请求即可。
	runtime.setQueueFilteredLocked(original, filtered)
	runtime.queue = append(runtime.queue, nil)
	copy(runtime.queue[first+1:], runtime.queue[first:])
	runtime.queue[first] = msg
	return true
}

type queuedLiveUpdate struct {
	terminalID string
	revision   uint64
}

func queuedOrdinaryLiveUpdate(msg Msg) (queuedLiveUpdate, bool) {
	switch msg := msg.(type) {
	case LiveSurfaceMsg:
		// 中文说明：core lifecycle 查询结果必须按边界消息处理，不能被普通 live 帧合并丢掉。
		if msg.Err != nil || msg.LifecycleKnown || !ordinaryLiveSnapshot(msg.Snapshot) {
			return queuedLiveUpdate{}, false
		}
		if msg.Snapshot.TerminalID == "" {
			return queuedLiveUpdate{}, false
		}
		return queuedLiveUpdate{terminalID: msg.Snapshot.TerminalID, revision: msg.Snapshot.Revision}, true
	case LiveEventMsg:
		// 中文说明：event stream 里的 lifecycle 变化同样是边界，不进入 latest-only 合并。
		if ordinaryLiveRefreshEvent(msg.Event) {
			if msg.Event.TerminalID == "" {
				return queuedLiveUpdate{}, false
			}
			return queuedLiveUpdate{terminalID: msg.Event.TerminalID}, true
		}
		if msg.Event.Err != nil || msg.Event.Exited || msg.Event.LifecycleKnown || !msg.Event.Ready || !ordinaryLiveSnapshot(msg.Event.Snapshot) {
			return queuedLiveUpdate{}, false
		}
		terminalID := msg.Event.Snapshot.TerminalID
		if terminalID == "" {
			terminalID = msg.Event.TerminalID
		}
		if terminalID == "" {
			return queuedLiveUpdate{}, false
		}
		return queuedLiveUpdate{terminalID: terminalID, revision: msg.Event.Snapshot.Revision}, true
	default:
		return queuedLiveUpdate{}, false
	}
}

func liveQueueBoundary(msg Msg) bool {
	switch msg := msg.(type) {
	case LiveAttachMsg, LiveAttachResultMsg, LiveInputAttachResultMsg, TerminalPoolAttachResultMsg, LiveExitMsg, LiveResizeMsg, LiveResizeResultMsg, HostResizeMsg, QuitMsg:
		return true
	case LiveSurfaceMsg:
		_, ok := queuedOrdinaryLiveUpdate(msg)
		return !ok
	case LiveEventMsg:
		_, ok := queuedOrdinaryLiveUpdate(msg)
		return !ok
	default:
		return false
	}
}

func ordinaryLiveSnapshot(snapshot state.LiveSurfaceSnapshot) bool {
	if snapshot.Err != "" || snapshot.ExitCode != 0 || snapshot.ExitReason != "" {
		return false
	}
	return snapshot.State == "" || snapshot.State == state.TerminalLiveAttached
}

func liveRevisionNewer(existing uint64, incoming uint64) bool {
	return existing != 0 && incoming != 0 && existing > incoming
}

func firstOrdinaryLiveUpdateIndex(queue []Msg) int {
	for i, queued := range queue {
		if _, ok := queuedOrdinaryLiveUpdate(queued); ok {
			return i
		}
		if liveQueueBoundary(queued) {
			return -1
		}
	}
	return -1
}

func (runtime *AppRuntime) removeAtLocked(index int) {
	if index < 0 || index >= len(runtime.queue) {
		return
	}
	copy(runtime.queue[index:], runtime.queue[index+1:])
	last := len(runtime.queue) - 1
	runtime.queue[last] = nil
	if last == 0 {
		runtime.queue = nil
		return
	}
	runtime.queue = runtime.queue[:last]
}

func (runtime *AppRuntime) setQueueFilteredLocked(original []Msg, filtered []Msg) {
	for i := len(filtered); i < len(original); i++ {
		original[i] = nil
	}
	if len(filtered) == 0 {
		runtime.queue = nil
		return
	}
	runtime.queue = filtered
}

func (runtime *AppRuntime) prepend(msg Msg) {
	if msg == nil {
		return
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if _, ok := msg.(HostResizeMsg); ok {
		runtime.dropQueuedHostResizeLocked()
	}
	runtime.queue = append([]Msg{msg}, runtime.queue...)
	runtime.signalWakeLocked()
}

func (runtime *AppRuntime) dequeue() (Msg, bool) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if len(runtime.queue) == 0 {
		return nil, false
	}
	msg := runtime.queue[0]
	copy(runtime.queue, runtime.queue[1:])
	last := len(runtime.queue) - 1
	runtime.queue[last] = nil
	if last == 0 {
		runtime.queue = nil
		return msg, true
	}
	runtime.queue = runtime.queue[:last]
	return msg, true
}

func (runtime *AppRuntime) currentTime() time.Time {
	if runtime.now != nil {
		return runtime.now()
	}
	return time.Now()
}

func (runtime *AppRuntime) waitForWake(ctx context.Context) bool {
	runtime.enqueueDueToastTick()
	if runtime.ingestHostCurrentSize() {
		return true
	}
	runtime.mu.Lock()
	hasQueued := len(runtime.queue) > 0
	wake := runtime.wake
	runtime.mu.Unlock()
	if hasQueued {
		return true
	}
	var hostReady <-chan struct{}
	if runtime.host != nil {
		hostReady = runtime.host.EventsReady()
	}
	var toastC <-chan time.Time
	if delay := runtime.nextToastWakeDelay(); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		toastC = timer.C
	} else if delay == 0 {
		runtime.enqueueDueToastTick()
		return true
	}
	select {
	case <-ctx.Done():
		return false
	case <-wake:
		return true
	case <-hostReady:
		return true
	case <-toastC:
		runtime.enqueueDueToastTick()
		return true
	}
}

func (runtime *AppRuntime) signalWakeLocked() {
	if runtime.wake == nil {
		return
	}
	select {
	case runtime.wake <- struct{}{}:
	default:
	}
}

func (runtime *AppRuntime) nextToastWakeDelay() time.Duration {
	if len(runtime.state.Shell.Toasts) == 0 {
		return -1
	}
	interval := runtime.toastTickInterval
	if interval <= 0 {
		interval = defaultToastTickInterval
	}
	now := runtime.currentTime()
	if runtime.lastToastTick.IsZero() {
		return 0
	}
	elapsed := now.Sub(runtime.lastToastTick)
	if elapsed >= interval {
		return 0
	}
	return interval - elapsed
}

func (runtime *AppRuntime) renderFrame() {
	if runtime.host == nil {
		return
	}
	if runtime.tryRenderCopyHistoryPatch() {
		return
	}
	frame := runtime.render(runtime.state)
	runtime.lastHitRegions = cloneRenderHitRegions(frame.HitRegions)
	_ = runtime.host.FrameSink().WriteFrame(frame)
	runtime.firstFrameWritten = true
	runtime.rememberCopyHistoryPatchFrame(frame)
	runtime.observeRuntimeFrame(frame)
}

func (runtime *AppRuntime) shouldWriteFirstFrame() bool {
	return runtime.startupFrameReady && !runtime.firstFrameWritten && runtime.state.Viewport.Valid
}

func (runtime *AppRuntime) dispatchMouseHitRegion(msg Msg) Msg {
	inputMsg, ok := msg.(InputMsg)
	if !ok {
		return msg
	}
	runtime.clearStaleMouseDrag(inputMsg.Event)
	if inputMsg.Event.Kind != input.EventKindMouse {
		return msg
	}
	if dragMsg, handled := runtime.dispatchMouseDrag(inputMsg.Event); handled {
		return dragMsg
	}
	resolution := resolveMouseHitRegions(runtime.lastHitRegions, inputMsg.Event)
	if inputMsg.Event.Mouse != input.MouseLeft {
		if msg, ok := runtime.overlayMouseSelectMsg(inputMsg.Event, resolution); ok {
			return msg
		}
		if msg, ok := runtime.copyModeMouseWheelEnterMsg(inputMsg.Event, resolution); ok {
			return msg
		}
		if inputMsg.Event.RawSeq != "" {
			if runtime.mouseEventCanPassthrough(inputMsg.Event, resolution) {
				return msg
			}
			if runtime.mouseWheelCanRouteToCopyMode(inputMsg.Event, resolution) {
				return msg
			}
			return NoopMsg{}
		}
		if runtime.mouseEventHitsUI(inputMsg.Event, resolution) {
			return NoopMsg{}
		}
		return msg
	}
	if !resolution.HasForeground {
		return msg
	}
	region := resolution.Foreground
	if region.Kind == render.HitRegionPaneResize {
		if drag, ok := paneResizeDragState(region, inputMsg.Event); ok {
			runtime.mouseDrag = drag
			return NoopMsg{}
		}
	}
	if drag, ok := runtime.floatingDragState(region, inputMsg.Event); ok {
		runtime.mouseDrag = drag
		return ShellFloatingCommandMsg{Command: state.FloatingCommand{
			Action:   state.FloatingCommandFocusRaise,
			TargetID: drag.FloatingID,
			Source:   state.PaneCommandSourceMouse,
		}}
	}
	if drag, ok := clipboardHistoryDividerDragState(region, inputMsg.Event); ok {
		runtime.mouseDrag = drag
		return NoopMsg{}
	}
	if region.Kind == render.HitRegionPaneAction && region.ActionID == render.ActionPaneClose.String() {
		return ShellWorkbenchCommandMsg{Command: state.WorkbenchCommand{
			Action: state.WorkbenchCommandPaneClose,
			Target: state.PaneCommandTarget{PaneID: region.PaneID},
			Source: state.PaneCommandSourceMouse,
		}}
	}
	if region.Kind == render.HitRegionPaneAction && region.ActionID == render.ActionTerminalTakeResizeOwner.String() {
		if !runtime.consumeTakeResizeOwnerDoubleClick(region, inputMsg.Event) {
			return ShellArmOwnerConfirmMsg{ViewID: terminalViewIDForOwnerRegion(runtime.state, region)}
		}
		return ShellContentActionMsg{ActionID: region.ActionID, PaneID: region.PaneID, Floating: region.Floating, Row: region.Row}
	}
	if region.Kind == render.HitRegionPaneAction && region.ActionID == render.ActionResizeLayoutLock.String() {
		return ShellContentActionMsg{ActionID: region.ActionID, PaneID: region.PaneID, Floating: region.Floating, Row: region.Row}
	}
	switch region.Kind {
	case render.HitRegionToastClose:
		return ShellCloseCurrentToastMsg{}
	case render.HitRegionToast:
		return NoopMsg{}
	case render.HitRegionOverlay:
		return ShellCloseOverlayMsg{}
	}
	if command, ok := PaneCommandFromHitRegion(region); ok && command.Action != state.PaneCommandFocus {
		runtime.fillMousePaneCommandDefaults(&command)
		if command.Action == state.PaneCommandSplit {
			return ShellWorkbenchCommandMsg{Command: state.WorkbenchCommand{
				Action: state.WorkbenchCommandPaneSplit,
				Pane:   command,
				Source: state.PaneCommandSourceMouse,
			}}
		}
		return ShellPaneCommandMsg{Command: command}
	}
	if activationMsg, ok := runtime.terminalInputActivationMsg(region); ok {
		return activationMsg
	}
	if runtime.mouseEventCanPassthrough(inputMsg.Event, resolution) {
		return msg
	}
	// 中文说明：history row 是内容前景命中区；点击非 active pane 的历史文本时必须先切焦点，
	// 不能直接进入 copy selection，否则文本区域会吞掉 panel focus。
	if resolution.HasHistoryRow {
		if focusMsg, shouldFocus := runtime.historyRowFocusMsg(resolution); shouldFocus {
			return focusMsg
		}
		if !runtime.copyModeMouseSelectAllowed(resolution.HistoryRow) {
			return NoopMsg{}
		}
		col := historyHitRegionDisplayColumn(inputMsg.Event, resolution.HistoryRow)
		return CopyModeMouseSelectMsg{Position: state.CopyPosition{Row: resolution.HistoryRow.Row, Col: col}, PaneID: resolution.HistoryRow.PaneID}
	}
	if command, ok := PaneCommandFromHitRegion(region); ok {
		runtime.fillMousePaneCommandDefaults(&command)
		return ShellPaneCommandMsg{Command: command}
	}
	switch region.Kind {
	case render.HitRegionContentAction:
		return ShellContentActionMsg{ActionID: region.ActionID, PaneID: region.PaneID, Floating: region.Floating, Row: region.Row}
	default:
		return msg
	}
}

func (runtime *AppRuntime) terminalInputActivationMsg(region render.HitRegion) (Msg, bool) {
	shell := runtime.state.Shell.EnsureDefaults()
	switch region.Kind {
	case render.HitRegionPaneContent:
		if region.PaneID == "" {
			return nil, false
		}
		if shell.ActivePaneID == region.PaneID && shell.ActiveFloatingID() == "" && shell.InteractionMode == state.InteractionModeNormal {
			return nil, false
		}
		return ShellActivateTerminalInputMsg{PaneID: region.PaneID}, true
	case render.HitRegionContentAction:
		if region.ActionID != render.ActionFloatingRaise.String() || !region.Floating {
			return nil, false
		}
		floatingID, ok := runtime.floatingIDForPaneID(region.PaneID)
		if !ok {
			return nil, false
		}
		if shell.ActiveFloatingID() == floatingID && shell.InteractionMode == state.InteractionModeNormal {
			return nil, false
		}
		return ShellActivateTerminalInputMsg{PaneID: region.PaneID, FloatingID: floatingID}, true
	default:
		return nil, false
	}
}

func (runtime *AppRuntime) mouseWheelCanRouteToCopyMode(event input.InputEvent, resolution mouseHitResolution) bool {
	if event.Kind != input.EventKindMouse {
		return false
	}
	switch event.Mouse {
	case input.MouseWheelUp:
	case input.MouseWheelDown:
		if !copyModeInputContext(runtime.state.CopyMode) {
			return false
		}
	default:
		return false
	}
	// 中文说明：带 RawSeq 的普通鼠标事件默认会被 terminal mouse tracking 吞掉；上滑需要保留
	// 进入 copy/history 的入口，已进入 copy/history 后下滑也必须继续交给 copy reducer。
	if !resolution.HasForeground {
		return true
	}
	switch resolution.Foreground.Kind {
	case render.HitRegionPaneContent, render.HitRegionHistoryRow:
		return true
	default:
		return false
	}
}

func (runtime *AppRuntime) overlayMouseSelectMsg(event input.InputEvent, resolution mouseHitResolution) (Msg, bool) {
	if !runtime.state.Shell.EnsureDefaults().Overlay.Open || !resolution.HasForeground {
		return nil, false
	}
	if resolution.Foreground.Kind != render.HitRegionOverlay && resolution.Foreground.Kind != render.HitRegionContentAction {
		return nil, false
	}
	switch event.Mouse {
	case input.MouseWheelUp:
		return ShellOverlayMouseSelectMsg{Delta: -1}, true
	case input.MouseWheelDown:
		return ShellOverlayMouseSelectMsg{Delta: 1}, true
	default:
		return nil, false
	}
}

func (runtime *AppRuntime) clearStaleMouseDrag(event input.InputEvent) {
	if !runtime.mouseDrag.Active {
		return
	}
	if event.Kind != input.EventKindMouse || event.Mouse == input.MouseLeft {
		// macOS 终端的 Option 原生选择可能吞掉 release，新输入必须能恢复 UI 鼠标状态。
		runtime.mouseDrag = mouseDragState{}
	}
}

func terminalViewIDForOwnerRegion(root state.Root, region render.HitRegion) string {
	if region.Floating {
		if floatingID, ok := root.Shell.EnsureDefaults().FloatingIDForPaneID(region.PaneID); ok {
			if binding, ok := root.TerminalViews.FloatingBinding(floatingID); ok {
				return binding.ViewID
			}
		}
		return ""
	}
	if binding, ok := root.TerminalViews.PaneBinding(region.PaneID); ok {
		return binding.ViewID
	}
	if binding, ok := root.TerminalViews.FloatingBinding(region.PaneID); ok {
		return binding.ViewID
	}
	return ""
}

func (runtime *AppRuntime) consumeTakeResizeOwnerDoubleClick(region render.HitRegion, event input.InputEvent) bool {
	current := mouseActionClickState{Kind: region.Kind, ActionID: region.ActionID, PaneID: region.PaneID, Floating: region.Floating, Row: event.Row, Col: event.Col}
	if runtime.lastMouseAction == current {
		runtime.lastMouseAction = mouseActionClickState{}
		return true
	}
	runtime.lastMouseAction = current
	return false
}

func historyHitRegionDisplayColumn(event input.InputEvent, region render.HitRegion) int {
	col := event.Col - 1
	if event.Col <= 0 {
		col = event.Col
	}
	col -= region.Rect.X
	if col < 0 {
		return 0
	}
	return col
}

func (runtime *AppRuntime) historyRowFocusMsg(resolution mouseHitResolution) (Msg, bool) {
	if !resolution.HasHistoryRow {
		return nil, false
	}
	region := resolution.HistoryRow
	if msg, ok := runtime.focusMsgForOwnerRegion(region); ok {
		return msg, true
	}
	if region.PaneID != "" {
		return nil, false
	}
	if !resolution.HasFocusOwner {
		return nil, false
	}
	return runtime.focusMsgForOwnerRegion(resolution.FocusOwner)
}

func (runtime *AppRuntime) focusMsgForOwnerRegion(region render.HitRegion) (Msg, bool) {
	if region.Floating {
		floatingID, ok := runtime.floatingIDForPaneID(region.PaneID)
		if !ok {
			return nil, false
		}
		shell := runtime.state.Shell.EnsureDefaults()
		if shell.ActiveFloatingID() == floatingID && shell.InteractionMode == state.InteractionModeNormal {
			return nil, false
		}
		return ShellActivateTerminalInputMsg{PaneID: region.PaneID, FloatingID: floatingID}, true
	}
	return runtime.focusMsgForOwner(region.PaneID)
}

func (runtime *AppRuntime) focusMsgForOwner(ownerID string) (Msg, bool) {
	if ownerID == "" {
		return nil, false
	}
	shell := runtime.state.Shell.EnsureDefaults()
	if _, ok := shell.PaneByID(ownerID); ok {
		if shell.ActivePaneID == ownerID && shell.ActiveFloatingID() == "" && shell.InteractionMode == state.InteractionModeNormal {
			return nil, false
		}
		return ShellActivateTerminalInputMsg{PaneID: ownerID}, true
	}
	for _, floating := range shell.ActiveFloatings() {
		if floating.ID != ownerID {
			continue
		}
		if shell.ActiveFloatingID() == ownerID && shell.InteractionMode == state.InteractionModeNormal {
			return nil, false
		}
		return ShellActivateTerminalInputMsg{PaneID: floating.Pane.ID, FloatingID: ownerID}, true
	}
	return nil, false
}

func (runtime *AppRuntime) copyModeMouseSelectAllowed(region render.HitRegion) bool {
	copyMode := runtime.state.CopyMode
	if !copyMode.Active {
		return false
	}
	if region.PaneID == "" {
		return true
	}
	if copyMode.PaneID == region.PaneID {
		return true
	}
	if region.Floating {
		if floatingID, ok := runtime.floatingIDForPaneID(region.PaneID); ok && copyMode.ViewID == state.TerminalFloatingViewID(floatingID) {
			return true
		}
	}
	if copyMode.ViewID == state.TerminalFloatingViewID(region.PaneID) {
		return true
	}
	shell := runtime.state.Shell.EnsureDefaults()
	if shell.ActivePaneID == region.PaneID && shell.ActiveFloatingID() == "" && copyMode.PaneID == "" && copyMode.ViewID == "" {
		return true
	}
	return false
}

func (runtime *AppRuntime) copyModeMouseWheelEnterMsg(event input.InputEvent, resolution mouseHitResolution) (Msg, bool) {
	if event.Kind != input.EventKindMouse || event.Mouse != input.MouseWheelUp || copyModeInputContext(runtime.state.CopyMode) {
		return nil, false
	}
	region, ok := copyModeWheelTargetRegion(resolution)
	if !ok {
		return nil, false
	}
	binding, ok := runtime.terminalViewBindingForMouseRegion(region)
	if !ok || binding.TerminalID == "" {
		return nil, false
	}
	rect := region.Rect
	if rect.W <= 0 || rect.H <= 0 {
		if fallback, ok := terminalViewContentRect(runtime.state, runtimeViewportRect(runtime.state.Viewport), binding); ok {
			rect = fallback
		}
	}
	if rect.W <= 0 {
		return nil, false
	}
	// 中文说明：滚轮上滑是 copy/history 入口，必须绑定鼠标命中的 TerminalView；
	// 不能先丢给 active pane，否则非 active sibling 要等下一次事件才进入 copy。
	return CopyModeEnterViewMsg{Binding: binding, Cols: rect.W, Rows: rect.H}, true
}

func copyModeWheelTargetRegion(resolution mouseHitResolution) (render.HitRegion, bool) {
	if resolution.HasHistoryRow {
		return resolution.HistoryRow, true
	}
	if !resolution.HasForeground {
		return render.HitRegion{}, false
	}
	switch resolution.Foreground.Kind {
	case render.HitRegionPaneContent, render.HitRegionHistoryRow, render.HitRegionContentAction:
		return resolution.Foreground, true
	default:
		return render.HitRegion{}, false
	}
}

func (runtime *AppRuntime) terminalViewBindingForMouseRegion(region render.HitRegion) (state.TerminalViewBinding, bool) {
	if region.Floating {
		if floatingID, ok := runtime.floatingIDForPaneID(region.PaneID); ok {
			return runtime.state.TerminalViews.FloatingBinding(floatingID)
		}
	}
	if binding, ok := runtime.state.TerminalViews.PaneBinding(region.PaneID); ok {
		return binding, true
	}
	if binding, ok := runtime.state.TerminalViews.FloatingBinding(region.PaneID); ok {
		return binding, true
	}
	return state.TerminalViewBinding{}, false
}

func runtimeViewportRect(viewport state.ViewportStore) render.Rect {
	if !viewport.Valid {
		return render.Rect{}
	}
	return render.Rect{W: viewport.Cols, H: viewport.Rows}
}

func (runtime *AppRuntime) mouseEventCanPassthrough(event input.InputEvent, resolution mouseHitResolution) bool {
	if event.RawSeq == "" || !resolution.HasFocusOwner || !resolution.HasForeground {
		return false
	}
	shell := runtime.state.Shell.EnsureDefaults()
	if shell.Overlay.Open || copyModeInputContext(runtime.state.CopyMode) {
		return false
	}
	if shell.InteractionMode != state.InteractionModeNormal {
		return false
	}
	if !mouseForegroundAllowsTerminalPassthrough(resolution.Foreground) {
		return false
	}
	return runtime.focusOwnerMouseTrackingEnabled(resolution.FocusOwner)
}

func (runtime *AppRuntime) mouseEventHitsUI(event input.InputEvent, resolution mouseHitResolution) bool {
	if !resolution.HasForeground {
		return false
	}
	if event.Kind == input.EventKindMouse && mouseEventIsWheel(event) {
		return false
	}
	region := resolution.Foreground
	switch region.Kind {
	case render.HitRegionPaneContent, render.HitRegionHistoryRow:
		return false
	case render.HitRegionContentAction:
		return region.ActionID != render.ActionFloatingRaise.String()
	default:
		return true
	}
}

func mouseEventIsWheel(event input.InputEvent) bool {
	return event.Mouse == input.MouseWheelUp || event.Mouse == input.MouseWheelDown
}

func mouseForegroundAllowsTerminalPassthrough(region render.HitRegion) bool {
	switch region.Kind {
	case render.HitRegionPaneContent:
		return true
	case render.HitRegionContentAction:
		return region.ActionID == render.ActionFloatingRaise.String()
	default:
		return false
	}
}

func (runtime *AppRuntime) focusOwnerMouseTrackingEnabled(region render.HitRegion) bool {
	shell := runtime.state.Shell.EnsureDefaults()
	switch region.Kind {
	case render.HitRegionPaneContent:
		if region.PaneID != shell.ActivePaneID || shell.ActiveFloatingID() != "" {
			return false
		}
		return runtime.paneMouseTrackingEnabled(region.PaneID)
	case render.HitRegionContentAction:
		if region.ActionID != render.ActionFloatingRaise.String() || !region.Floating {
			return false
		}
		floatingID, ok := runtime.floatingIDForPaneID(region.PaneID)
		if !ok || floatingID != shell.ActiveFloatingID() {
			return false
		}
		return runtime.floatingMouseTrackingEnabled(floatingID)
	default:
		return false
	}
}

func (runtime *AppRuntime) paneMouseTrackingEnabled(paneID string) bool {
	if paneID == "" {
		return false
	}
	binding, ok := runtime.state.TerminalViews.PaneBinding(paneID)
	if !ok || binding.TerminalID == "" {
		return false
	}
	surface := runtime.state.Surface.SurfaceForTerminal(binding.TerminalID)
	return surface.Modes.MousePassthroughEnabled()
}

func (runtime *AppRuntime) floatingMouseTrackingEnabled(floatingID string) bool {
	if floatingID == "" {
		return false
	}
	binding, ok := runtime.state.TerminalViews.FloatingBinding(floatingID)
	if !ok || binding.TerminalID == "" {
		return false
	}
	surface := runtime.state.Surface.SurfaceForTerminal(binding.TerminalID)
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
		case mouseDragClipboardDivider:
			delta := event.Col - runtime.mouseDrag.LastCol
			if delta == 0 {
				return NoopMsg{}, true
			}
			runtime.mouseDrag.LastCol = event.Col
			runtime.mouseDrag.LastRow = event.Row
			return ShellMoveClipboardHistoryDividerMsg{Delta: delta}, true
		default:
			return NoopMsg{}, true
		}
	default:
		return nil, false
	}
}

func clipboardHistoryDividerDragState(region render.HitRegion, event input.InputEvent) (mouseDragState, bool) {
	if region.Kind != render.HitRegionContentAction || region.ActionID != render.ActionClipboardHistoryDividerDrag.String() {
		return mouseDragState{}, false
	}
	return mouseDragState{
		Active:  true,
		Kind:    mouseDragClipboardDivider,
		LastCol: event.Col,
		LastRow: event.Row,
	}, true
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

func (runtime *AppRuntime) floatingDragState(region render.HitRegion, event input.InputEvent) (mouseDragState, bool) {
	if region.Kind != render.HitRegionContentAction || region.PaneID == "" || !region.Floating {
		return mouseDragState{}, false
	}
	var kind mouseDragKind
	switch region.ActionID {
	case render.ActionFloatingMoveDrag.String():
		kind = mouseDragFloatingMove
	case render.ActionFloatingResizeDrag.String():
		kind = mouseDragFloatingResize
	default:
		return mouseDragState{}, false
	}
	floatingID, ok := runtime.floatingIDForPaneID(region.PaneID)
	if !ok {
		return mouseDragState{}, false
	}
	return mouseDragState{
		Active:     true,
		Kind:       kind,
		PaneID:     region.PaneID,
		FloatingID: floatingID,
		LastCol:    event.Col,
		LastRow:    event.Row,
	}, true
}

func (runtime *AppRuntime) floatingIDForPaneID(paneID string) (string, bool) {
	if paneID == "" {
		return "", false
	}
	return runtime.state.Shell.EnsureDefaults().FloatingIDForPaneID(paneID)
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

func resolveMouseHitRegions(regions []render.HitRegion, event input.InputEvent) mouseHitResolution {
	col, row := mouseEventPoint(event)
	resolution := mouseHitResolution{}
	for _, region := range regions {
		if !pointInRect(col, row, region.Rect) {
			continue
		}
		if !resolution.HasForeground {
			resolution.Foreground = region
			resolution.HasForeground = true
		}
		if !resolution.HasHistoryRow && region.Kind == render.HitRegionHistoryRow {
			resolution.HistoryRow = region
			resolution.HasHistoryRow = true
		}
		if !resolution.HasFocusOwner && mouseFocusOwnerRegion(region) {
			resolution.FocusOwner = region
			resolution.HasFocusOwner = true
		}
	}
	return resolution
}

func mouseFocusOwnerRegion(region render.HitRegion) bool {
	switch region.Kind {
	case render.HitRegionPaneContent:
		return region.PaneID != ""
	case render.HitRegionContentAction:
		return region.PaneID != "" && region.ActionID == render.ActionFloatingRaise.String()
	default:
		return false
	}
}

func mouseEventPoint(event input.InputEvent) (int, int) {
	col := event.Col - 1
	row := event.Row - 1
	if event.Col <= 0 {
		col = event.Col
	}
	if event.Row <= 0 {
		row = event.Row
	}
	return col, row
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
		if effect.Async && !effect.ForceSyncInTests {
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
	ready  chan struct{}
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
		ready:  make(chan struct{}, 1),
		sink:   &FakeFrameSink{},
	}
}

func (host *FakeTerminalHost) SendInput(event input.InputEvent) error {
	select {
	case host.events <- event:
		host.signalReady()
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
		host.signalReady()
		return nil
	default:
		return ErrInputQueueFull
	}
}

func (host *FakeTerminalHost) InputEvents() <-chan input.InputEvent {
	return host.events
}

func (host *FakeTerminalHost) EventsReady() <-chan struct{} {
	return host.ready
}

func (host *FakeTerminalHost) FrameSink() render.FrameSink {
	return host.sink
}

func (host *FakeTerminalHost) Frames() []render.Frame {
	return host.sink.Frames()
}

func (host *FakeTerminalHost) signalReady() {
	select {
	case host.ready <- struct{}{}:
	default:
	}
}

// FakeFrameSink 记录 renderer 输出，供 harness 断言。
type FakeFrameSink struct {
	frames  []render.Frame
	onWrite func()
}

func (sink *FakeFrameSink) WriteFrame(frame render.Frame) error {
	if sink.onWrite != nil {
		sink.onWrite()
	}
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
