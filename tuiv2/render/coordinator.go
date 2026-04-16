package render

import (
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/workbench"
)

// Coordinator 负责 render invalidation / schedule / flush / ticker。
// 它通过 RenderVMFn 拉取当前稳定的 render view-model。
type Coordinator struct {
	vmFn          RenderVMFn
	mu            sync.Mutex
	dirty         bool
	lastFrame     string
	lastResult    RenderResult
	hasLastResult bool
	lastKey       renderVMKey
	bodyCache     *bodyRenderCache
	tabBarValue   string
	statusValue   string
	tabBarKey     tabBarCacheKey
	statusKey     statusBarCacheKey

	cursorBlinkVisible bool
}

const CursorBlinkInterval = 600 * time.Millisecond

type VisibleStateFn func() VisibleRenderState
type RenderVMFn func() RenderVM

type renderedBody struct {
	content string
	lines   []string
	cursor  string
	blink   bool
}

type tabBarCacheKey struct {
	Theme         uiTheme
	Width         int
	WorkspaceName string
	ActiveTab     int
	Error         string
	Notice        string
	Tabs          []tabBarCacheTab
}

type tabBarCacheTab struct {
	ID   string
	Name string
}

type statusBarCacheKey struct {
	Theme          uiTheme
	Width          int
	InputMode      string
	StatusHintsSig string
	RightTokensSig string
}

type renderVMKey struct {
	Workbench *workbench.VisibleWorkbench
	Runtime   *VisibleRuntimeStateProxy
	Surface   RenderSurfaceVM
	Overlay   RenderOverlayVM
	TermSize  TermSize
	Status    renderStatusKey
	Body      renderBodyKey
}

type renderStatusKey struct {
	Notice         string
	Error          string
	InputMode      string
	StatusHintSig  string
	RightTokensSig string
}

type renderBodyKey struct {
	OwnerConfirmPaneID string
	EmptySelection     RenderPaneSelectionVM
	ExitedSelection    RenderPaneSelectionVM
	SnapshotOverride   RenderSnapshotOverrideVM
	CopyMode           renderCopyModeKey
}

type renderCopyModeKey struct {
	PaneID     string
	CursorRow  int
	CursorCol  int
	ViewTopRow int
	MarkSet    bool
	MarkRow    int
	MarkCol    int
	Snapshot   *protocol.Snapshot
}

type paneContentKey struct {
	TerminalID           string
	Snapshot             *protocol.Snapshot
	SurfaceVersion       uint64
	Name                 string
	State                string
	ThemeBG              string
	TerminalKnown        bool
	SharedLeft           bool
	SharedTop            bool
	ScrollOffset         int
	EmptyActionSelected  int
	ExitedActionSelected int
	ExitedActionPulse    bool
	CopyModeActive       bool
	CopyModeCursorRow    int
	CopyModeCursorCol    int
	CopyModeViewTopRow   int
	CopyModeMarkSet      bool
	CopyModeMarkRow      int
	CopyModeMarkCol      int
}

func NewCoordinator(fn VisibleStateFn) *Coordinator {
	if fn == nil {
		return NewCoordinatorWithVM(nil)
	}
	return NewCoordinatorWithVM(func() RenderVM {
		return RenderVMFromVisibleState(fn())
	})
}

func NewCoordinatorWithVM(fn RenderVMFn) *Coordinator {
	return &Coordinator{
		vmFn:               fn,
		dirty:              true,
		cursorBlinkVisible: true,
	}
}

func (c *Coordinator) Invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.dirty = true
	c.mu.Unlock()
}

func (c *Coordinator) ResetCaches() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.dirty = true
	c.lastFrame = ""
	c.lastResult = RenderResult{}
	c.hasLastResult = false
	c.lastKey = renderVMKey{}
	c.bodyCache = nil
	c.tabBarValue = ""
	c.statusValue = ""
	c.tabBarKey = tabBarCacheKey{}
	c.statusKey = statusBarCacheKey{}
	c.mu.Unlock()
}

func (c *Coordinator) RevealCursorBlink() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.cursorBlinkVisible = true
	c.dirty = true
	c.mu.Unlock()
}

func (c *Coordinator) Schedule()     {}
func (c *Coordinator) FlushPending() {}
func (c *Coordinator) StartTicker()  {}

func (c *Coordinator) RenderFrame() string {
	finish := perftrace.Measure("render.frame")
	frame := ""
	cacheMetric := "render.frame.cache_miss"
	defer func() {
		perftrace.Count(cacheMetric, len(frame))
		finish(len(frame))
	}()
	if c == nil || c.vmFn == nil {
		return ""
	}
	result, cached := c.renderResult()
	if cached {
		cacheMetric = "render.frame.cache_hit"
	}
	frame = c.frameFromResult(result)
	return frame
}

func (c *Coordinator) RenderFrameLines() ([]string, string) {
	if c == nil || c.vmFn == nil {
		return nil, hideCursorANSI()
	}
	result, _ := c.renderResult()
	return append([]string(nil), result.Lines...), result.CursorSequence()
}

func (c *Coordinator) RenderFrameLinesRef() ([]string, string) {
	if c == nil || c.vmFn == nil {
		return nil, hideCursorANSI()
	}
	result, _ := c.renderResult()
	return result.Lines, result.CursorSequence()
}

func (c *Coordinator) CursorSequence() string {
	if c == nil {
		return hideCursorANSI()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.hasLastResult {
		return hideCursorANSI()
	}
	return c.lastResult.CursorSequence()
}

func (c *Coordinator) CachedFrameAndCursor() (string, string, bool) {
	if c == nil || c.vmFn == nil {
		return "", hideCursorANSI(), false
	}
	vm := c.vmFn()
	key := renderVMKeyForVM(vm)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dirty || !c.hasLastResult || c.lastKey != key {
		return "", "", false
	}
	return c.cachedFrameLocked(), c.lastResult.CursorSequence(), true
}

func (c *Coordinator) CachedFrameLinesAndCursor() ([]string, string, bool) {
	if c == nil || c.vmFn == nil {
		return nil, hideCursorANSI(), false
	}
	vm := c.vmFn()
	key := renderVMKeyForVM(vm)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dirty || !c.hasLastResult || c.lastKey != key {
		return nil, "", false
	}
	return append([]string(nil), c.lastResult.Lines...), c.lastResult.CursorSequence(), true
}

func (c *Coordinator) CachedFrameLinesAndCursorRef() ([]string, string, bool) {
	if c == nil || c.vmFn == nil {
		return nil, hideCursorANSI(), false
	}
	vm := c.vmFn()
	key := renderVMKeyForVM(vm)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dirty || !c.hasLastResult || c.lastKey != key {
		return nil, "", false
	}
	return c.lastResult.Lines, c.lastResult.CursorSequence(), true
}

func (c *Coordinator) CachedRenderResult() (RenderResult, bool) {
	if c == nil || c.vmFn == nil {
		return RenderResult{}, false
	}
	vm := c.vmFn()
	key := renderVMKeyForVM(vm)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dirty || !c.hasLastResult || c.lastKey != key {
		return RenderResult{}, false
	}
	return cloneRenderResult(c.lastResult), true
}

func (c *Coordinator) Render() RenderResult {
	result, _ := c.renderResult()
	return result
}

func (c *Coordinator) NeedsCursorTicks() bool {
	if c == nil || c.vmFn == nil {
		return false
	}
	return renderVMNeedsCursorBlink(c.vmFn())
}

func (c *Coordinator) AdvanceCursorBlink() bool {
	if c == nil {
		return false
	}
	if !c.NeedsCursorTicks() {
		c.mu.Lock()
		c.cursorBlinkVisible = true
		c.mu.Unlock()
		return false
	}
	c.mu.Lock()
	c.cursorBlinkVisible = !c.cursorBlinkVisible
	c.dirty = true
	c.mu.Unlock()
	return true
}

func (c *Coordinator) syntheticCursorVisible(_ protocol.CursorState) bool {
	_ = c
	// Pane-local synthetic cursors stay steady. Reusing overlay blink state here
	// leaves them stranded in the hidden phase after overlays close.
	return true
}

func renderVMKeyForVM(vm RenderVM) renderVMKey {
	return renderVMKey{
		Workbench: vm.Workbench,
		Runtime:   vm.Runtime,
		Surface:   vm.Surface,
		Overlay:   vm.Overlay,
		TermSize:  vm.TermSize,
		Status: renderStatusKey{
			Notice:         vm.Status.Notice,
			Error:          vm.Status.Error,
			InputMode:      vm.Status.InputMode,
			StatusHintSig:  strings.Join(vm.Status.Hints, "\x1f"),
			RightTokensSig: statusBarRightTokenSignature(vm.Status.RightTokens),
		},
		Body: renderBodyKey{
			OwnerConfirmPaneID: vm.Body.OwnerConfirmPaneID,
			EmptySelection:     vm.Body.EmptySelection,
			ExitedSelection:    vm.Body.ExitedSelection,
			SnapshotOverride:   vm.Body.SnapshotOverride,
			CopyMode: renderCopyModeKey{
				PaneID:     vm.Body.CopyMode.PaneID,
				CursorRow:  vm.Body.CopyMode.CursorRow,
				CursorCol:  vm.Body.CopyMode.CursorCol,
				ViewTopRow: vm.Body.CopyMode.ViewTopRow,
				MarkSet:    vm.Body.CopyMode.MarkSet,
				MarkRow:    vm.Body.CopyMode.MarkRow,
				MarkCol:    vm.Body.CopyMode.MarkCol,
				Snapshot:   vm.Body.CopyMode.Snapshot,
			},
		},
	}
}

func (c *Coordinator) renderResult() (RenderResult, bool) {
	if c == nil || c.vmFn == nil {
		return RenderResult{}, false
	}
	vm := c.vmFn()
	key := renderVMKeyForVM(vm)
	c.mu.Lock()
	if !c.dirty && c.hasLastResult && c.lastKey == key {
		result := cloneRenderResult(c.lastResult)
		c.mu.Unlock()
		return result, true
	}
	c.mu.Unlock()
	result := renderResultWithCoordinator(c, vm)
	c.mu.Lock()
	c.lastResult = cloneRenderResult(result)
	c.lastFrame = ""
	c.lastKey = key
	c.hasLastResult = true
	c.dirty = false
	c.mu.Unlock()
	return result, false
}

func (c *Coordinator) cachedFrameLocked() string {
	if !c.hasLastResult {
		return ""
	}
	if c.lastFrame == "" {
		c.lastFrame = c.lastResult.Frame()
	}
	return c.lastFrame
}

func (c *Coordinator) frameFromResult(result RenderResult) string {
	if c == nil {
		return result.Frame()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if frame := c.cachedFrameLocked(); frame != "" {
		return frame
	}
	return result.Frame()
}

func (c *Coordinator) renderTabBarCached(vm RenderVM) string {
	theme := uiThemeForRuntime(vm.Runtime)
	c.mu.Lock()
	if c.tabBarKey.matchesVM(vm, theme) {
		value := c.tabBarValue
		c.mu.Unlock()
		return value
	}
	c.mu.Unlock()
	value := renderTabBarVM(vm)
	c.mu.Lock()
	if c.tabBarValue == value {
		value = c.tabBarValue
	}
	c.tabBarValue = value
	c.tabBarKey.captureVM(vm, theme)
	c.mu.Unlock()
	return value
}

func (c *Coordinator) renderStatusBarCached(vm RenderVM) string {
	theme := uiThemeForRuntime(vm.Runtime)
	key := statusBarCacheKeyForVM(vm, theme)
	c.mu.Lock()
	if c.statusKey == key {
		value := c.statusValue
		c.mu.Unlock()
		return value
	}
	c.mu.Unlock()
	value := renderStatusBarVM(vm)
	c.mu.Lock()
	if c.statusValue == value {
		value = c.statusValue
	}
	c.statusValue = value
	c.statusKey = key
	c.mu.Unlock()
	return value
}

func (k tabBarCacheKey) matchesVM(vm RenderVM, theme uiTheme) bool {
	if k.Theme != theme || k.Width != vm.TermSize.Width || k.Error != vm.Status.Error || k.Notice != vm.Status.Notice {
		return false
	}
	if vm.Workbench == nil {
		return k.WorkspaceName == "" && k.ActiveTab == -1 && len(k.Tabs) == 0
	}
	if k.WorkspaceName != vm.Workbench.WorkspaceName || k.ActiveTab != vm.Workbench.ActiveTab || len(k.Tabs) != len(vm.Workbench.Tabs) {
		return false
	}
	for i := range vm.Workbench.Tabs {
		tab := vm.Workbench.Tabs[i]
		if k.Tabs[i].ID != tab.ID || k.Tabs[i].Name != tab.Name {
			return false
		}
	}
	return true
}

func (k *tabBarCacheKey) captureVM(vm RenderVM, theme uiTheme) {
	if k == nil {
		return
	}
	k.Theme = theme
	k.Width = vm.TermSize.Width
	k.Error = vm.Status.Error
	k.Notice = vm.Status.Notice
	k.WorkspaceName = ""
	k.ActiveTab = -1
	if vm.Workbench == nil {
		k.Tabs = k.Tabs[:0]
		return
	}
	k.WorkspaceName = vm.Workbench.WorkspaceName
	k.ActiveTab = vm.Workbench.ActiveTab
	if cap(k.Tabs) < len(vm.Workbench.Tabs) {
		k.Tabs = make([]tabBarCacheTab, len(vm.Workbench.Tabs))
	} else {
		k.Tabs = k.Tabs[:len(vm.Workbench.Tabs)]
	}
	for i, tab := range vm.Workbench.Tabs {
		k.Tabs[i] = tabBarCacheTab{ID: tab.ID, Name: tab.Name}
	}
}

func statusBarCacheKeyForVM(vm RenderVM, theme uiTheme) statusBarCacheKey {
	return statusBarCacheKey{
		Theme:          theme,
		Width:          vm.TermSize.Width,
		InputMode:      strings.TrimSpace(vm.Status.InputMode),
		StatusHintsSig: strings.Join(vm.Status.Hints, "\x1f"),
		RightTokensSig: statusBarRightTokenSignature(vm.Status.RightTokens),
	}
}

func statusBarCacheKeyForState(state VisibleRenderState, theme uiTheme) statusBarCacheKey {
	return statusBarCacheKey{
		Theme:          theme,
		Width:          state.TermSize.Width,
		InputMode:      strings.TrimSpace(state.InputMode),
		StatusHintsSig: strings.Join(state.StatusHints, "\x1f"),
		RightTokensSig: statusBarRightTokenSignature(statusBarRightTokens(state)),
	}
}

func renderBody(state VisibleRenderState, width, height int) string {
	return renderBodyFrameWithCoordinator(nil, state, width, height).Content()
}

func (b renderedBody) Content() string {
	if b.content != "" || len(b.lines) == 0 {
		return b.content
	}
	return strings.Join(b.lines, "\n")
}

func splitRenderedLines(frame string, dst []string) []string {
	dst = dst[:0]
	start := 0
	for i := 0; i < len(frame); i++ {
		if frame[i] != '\n' {
			continue
		}
		dst = append(dst, frame[start:i])
		start = i + 1
	}
	return append(dst, frame[start:])
}

func renderBodyFrameWithCoordinator(coordinator *Coordinator, state VisibleRenderState, width, height int) renderedBody {
	return renderBodyFrameWithCoordinatorVM(coordinator, RenderVMFromVisibleState(state), width, height)
}

type emptyWorkbenchKind uint8

const (
	emptyWorkbenchNoTabs emptyWorkbenchKind = iota
	emptyWorkbenchNoPanes
)

func entriesOverlap(entries []paneRenderEntry) bool {
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if rectsOverlap(entries[i].Rect, entries[j].Rect) {
				return true
			}
		}
	}
	return false
}

func overlapsAnyRect(rect workbench.Rect, others []workbench.Rect) bool {
	for _, other := range others {
		if rectsOverlap(rect, other) {
			return true
		}
	}
	return false
}

func rectsOverlap(a, b workbench.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}
