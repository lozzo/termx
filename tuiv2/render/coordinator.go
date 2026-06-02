package render

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-shared/perftrace"
	"github.com/lozzow/termx/tuiv2/workbench"
)

// Coordinator 负责 render invalidation / schedule / flush / ticker。
// 它通过 RenderVMFn 拉取当前稳定的 render view-model。
type Coordinator struct {
	vmFn           RenderVMFn
	mu             sync.Mutex
	dirty          bool
	lastFrame      string
	lastResult     RenderResult
	hasLastResult  bool
	lastKey        renderVMKey
	bodyCache      *bodyRenderCache
	altScreenCache *altScreenRowCache
	tabBarValue    string
	statusValue    string
	tabBarKey      tabBarCacheKey
	statusKey      statusBarCacheKey

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
	meta    *PresentMetadata
}

type tabBarCacheKey struct {
	Theme         uiTheme
	Width         int
	ChromeSig     string
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
	ChromeSig      string
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
	Theme     UIThemeConfig
	ChromeSig string
	Status    renderStatusKey
	Body      renderBodyKey
}

type renderStatusKey struct {
	ChromeSig      string
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
	FloatingPreview    renderFloatingDragPreviewKey
	CopyMode           renderCopyModeKey
	CopyModesSig       string
}

type renderFloatingDragPreviewKey struct {
	PaneID   string
	Rect     workbench.Rect
	Snapshot *protocol.Snapshot
}

type renderCopyModeKey struct {
	PaneID            string
	CursorRow         int
	CursorCol         int
	CursorLogicalLine int
	CursorLogicalCol  int
	ViewTopRow        int
	MarkSet           bool
	MarkRow           int
	MarkCol           int
	MarkLogicalLine   int
	MarkLogicalCol    int
	ProjectionToken   string
	ProjectionSig     uint64
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
	ContentOffsetX       int
	ContentOffsetY       int
	EmptyActionSelected  int
	ExitedActionSelected int
	ExitedActionPulse    bool
	CopyModeActive       bool
	CopyMode             renderCopyModeKey
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
	c.altScreenCache = nil
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
	result, cached := c.renderResultRef()
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
	result, _ := c.renderResultRef()
	return append([]string(nil), result.Lines...), result.CursorSequence()
}

func (c *Coordinator) RenderFrameLinesRef() ([]string, string) {
	if c == nil || c.vmFn == nil {
		return nil, hideCursorANSI()
	}
	result, _ := c.renderResultRef()
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
	result, _ := c.renderResultRef()
	return cloneRenderResult(result)
}

func (c *Coordinator) RenderRef() RenderResult {
	result, _ := c.renderResultRef()
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
	chromeSig := normalizeUIChromeConfig(vm.Chrome).signature()
	copyMode := vm.Body.CopyMode
	if copyMode.PaneID == "" && len(vm.Body.CopyModes) > 0 {
		copyMode = vm.Body.CopyModes[0]
	}
	return renderVMKey{
		Workbench: vm.Workbench,
		Runtime:   vm.Runtime,
		Surface:   vm.Surface,
		Overlay:   vm.Overlay,
		TermSize:  vm.TermSize,
		Theme:     vm.Theme,
		ChromeSig: chromeSig,
		Status: renderStatusKey{
			ChromeSig:      chromeSig,
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
			FloatingPreview: renderFloatingDragPreviewKey{
				PaneID:   vm.Body.FloatingDragPreview.PaneID,
				Rect:     vm.Body.FloatingDragPreview.Rect,
				Snapshot: vm.Body.FloatingDragPreview.Snapshot,
			},
			CopyMode:     renderCopyModeKeyForVM(copyMode),
			CopyModesSig: renderCopyModesSignature(vm.Body.CopyModes),
		},
	}
}

func renderCopyModesSignature(copyModes []RenderCopyModeVM) string {
	if len(copyModes) == 0 {
		return ""
	}
	parts := make([]string, 0, len(copyModes))
	for _, copyMode := range copyModes {
		if copyMode.PaneID == "" {
			continue
		}
		parts = append(parts, strings.Join([]string{
			copyMode.PaneID,
			strconv.Itoa(copyMode.CursorRow),
			strconv.Itoa(copyMode.CursorCol),
			strconv.Itoa(copyMode.CursorLogicalLine),
			strconv.Itoa(copyMode.CursorLogicalCol),
			strconv.Itoa(copyMode.ViewTopRow),
			strconv.FormatBool(copyMode.MarkSet),
			strconv.Itoa(copyMode.MarkRow),
			strconv.Itoa(copyMode.MarkCol),
			strconv.Itoa(copyMode.MarkLogicalLine),
			strconv.Itoa(copyMode.MarkLogicalCol),
			copyModeProjectionToken(copyMode.Projection),
			strconv.FormatUint(copyModeProjectionSignature(copyMode.Projection), 16),
		}, "\x1e"))
	}
	sort.Strings(parts)
	return strings.Join(parts, "\x1f")
}

func renderCopyModeKeyForVM(copyMode RenderCopyModeVM) renderCopyModeKey {
	return renderCopyModeKey{
		PaneID:            copyMode.PaneID,
		CursorRow:         copyMode.CursorRow,
		CursorCol:         copyMode.CursorCol,
		CursorLogicalLine: copyMode.CursorLogicalLine,
		CursorLogicalCol:  copyMode.CursorLogicalCol,
		ViewTopRow:        copyMode.ViewTopRow,
		MarkSet:           copyMode.MarkSet,
		MarkRow:           copyMode.MarkRow,
		MarkCol:           copyMode.MarkCol,
		MarkLogicalLine:   copyMode.MarkLogicalLine,
		MarkLogicalCol:    copyMode.MarkLogicalCol,
		ProjectionToken:   copyModeProjectionToken(copyMode.Projection),
		ProjectionSig:     copyModeProjectionSignature(copyMode.Projection),
	}
}

func copyModeProjectionToken(projection *RenderCopyModeProjectionVM) string {
	if projection == nil {
		return ""
	}
	return projection.Token
}

func copyModeProjectionSignature(projection *RenderCopyModeProjectionVM) uint64 {
	if projection == nil {
		return 0
	}
	hash := fnvOffset64
	hash = fnvMixString(hash, projection.TerminalID)
	hash = fnvMixString(hash, projection.Token)
	hash = fnvMixUint64(hash, projection.Generation)
	hash = fnvMixUint64(hash, uint64(projection.Size.Cols))
	hash = fnvMixUint64(hash, uint64(projection.Size.Rows))
	hash = fnvMixUint64(hash, uint64(len(projection.Rows)))
	hash = fnvMixUint64(hash, uint64(len(projection.Lines)))
	hash = fnvMixUint64(hash, uint64(projection.TotalRows))
	hash = fnvMixUint64(hash, uint64(projection.TotalLines))
	hash = fnvMixBool(hash, projection.HasMore)
	hash = fnvMixUint64(hash, projection.FirstBoundaryID)
	hash = fnvMixUint64(hash, projection.LastBoundaryID)
	for index := range projection.Rows {
		hash = fnvMixUint64(hash, copyModeProjectionRowSignature(projection, index))
	}
	for _, line := range projection.Lines {
		hash = fnvMixInt64(hash, int64(line.StartRow))
		hash = fnvMixInt64(hash, int64(line.EndRow))
		hash = fnvMixUint64(hash, line.LogicalLineID)
		hash = fnvMixBool(hash, line.ClippedBefore)
		hash = fnvMixBool(hash, line.ClippedAfter)
	}
	return hash
}

func copyModeProjectionRowSignature(projection *RenderCopyModeProjectionVM, rowIndex int) uint64 {
	if projection == nil || rowIndex < 0 || rowIndex >= len(projection.Rows) {
		return fnvMixUint64(fnvOffset64, 0)
	}
	row := projection.Rows[rowIndex]
	hash := fnvOffset64
	hash = fnvMixUint64(hash, uint64(rowIndex+1))
	hash = fnvMixString(hash, projection.Token)
	hash = fnvMixUint64(hash, projection.Generation)
	hash = fnvMixString(hash, row.Kind)
	hash = fnvMixBool(hash, row.Wrapped)
	hash = fnvMixInt64(hash, row.Timestamp.UnixNano())
	return hashProtocolRow(hash, row.Cells)
}

func (c *Coordinator) renderResultRef() (RenderResult, bool) {
	if c == nil || c.vmFn == nil {
		return RenderResult{}, false
	}
	vm := c.vmFn()
	key := renderVMKeyForVM(vm)
	c.mu.Lock()
	if !c.dirty && c.hasLastResult && c.lastKey == key {
		result := c.lastResult
		c.mu.Unlock()
		return result, true
	}
	c.mu.Unlock()
	result := renderResultWithCoordinator(c, vm)
	c.mu.Lock()
	c.lastResult = result
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
	theme := uiThemeForVM(vm)
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
	theme := uiThemeForVM(vm)
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
	chromeSig := normalizeUIChromeConfig(vm.Chrome).signature()
	if k.Theme != theme || k.Width != vm.TermSize.Width || k.ChromeSig != chromeSig || k.Error != vm.Status.Error || k.Notice != vm.Status.Notice {
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
	k.ChromeSig = normalizeUIChromeConfig(vm.Chrome).signature()
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
		ChromeSig:      normalizeUIChromeConfig(vm.Chrome).signature(),
		InputMode:      strings.TrimSpace(vm.Status.InputMode),
		StatusHintsSig: strings.Join(vm.Status.Hints, "\x1f"),
		RightTokensSig: statusBarRightTokenSignature(vm.Status.RightTokens),
	}
}

func statusBarCacheKeyForState(state VisibleRenderState, theme uiTheme) statusBarCacheKey {
	return statusBarCacheKey{
		Theme:          theme,
		Width:          state.TermSize.Width,
		ChromeSig:      normalizeUIChromeConfig(state.Chrome).signature(),
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

func rectsOverlap(a, b workbench.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}
