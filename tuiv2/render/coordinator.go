package render

import (
	"strconv"
	"strings"
	"sync"
	"time"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/modal"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

// Coordinator 负责 render invalidation / schedule / flush / ticker。
// 它通过 VisibleStateFn 拉取 workbench + runtime 的当前可见状态。
type Coordinator struct {
	visibleFn   VisibleStateFn
	mu          sync.Mutex
	dirty       bool
	lastFrame   string
	lastCursor  string
	lastState   renderStateKey
	bodyCache   *bodyRenderCache
	tabBarValue string
	statusValue string

	cursorBlinkVisible bool
}

const CursorBlinkInterval = 600 * time.Millisecond

type VisibleStateFn func() VisibleRenderState

type renderedBody struct {
	content string
	cursor  string
	blink   bool
}

type renderStateKey struct {
	Workbench                  *workbench.VisibleWorkbench
	Runtime                    *VisibleRuntimeStateProxy
	SurfaceKind                VisibleSurfaceKind
	SurfaceTerminalPool        *modal.TerminalManagerState
	OverlayKind                VisibleOverlayKind
	OverlayPrompt              *modal.PromptState
	OverlayPicker              *modal.PickerState
	OverlayWorkspacePicker     *modal.WorkspacePickerState
	OverlayTerminalManager     *modal.TerminalManagerState
	OverlayHelp                *modal.HelpState
	OverlayFloatingOverview    *modal.FloatingOverviewState
	TermSize                   TermSize
	Notice                     string
	Error                      string
	InputMode                  string
	OwnerConfirmPaneID         string
	EmptyPaneSelectionPaneID   string
	EmptyPaneSelectionIndex    int
	ExitedPaneSelectionPaneID  string
	ExitedPaneSelectionIndex   int
	PaneSnapshotOverridePaneID string
	PaneSnapshotOverride       *protocol.Snapshot
	CopyModePaneID             string
	CopyModeCursorRow          int
	CopyModeCursorCol          int
	CopyModeViewTopRow         int
	CopyModeMarkSet            bool
	CopyModeMarkRow            int
	CopyModeMarkCol            int
	CopyModeSnapshot           *protocol.Snapshot
}

type paneRenderEntry struct {
	PaneID               string
	Rect                 workbench.Rect
	Frameless            bool
	SharedLeft           bool
	SharedTop            bool
	Title                string
	Border               paneBorderInfo
	Theme                uiTheme
	Overflow             paneOverflowHints
	ContentKey           paneContentKey
	FrameKey             paneFrameKey
	TerminalID           string
	Snapshot             *protocol.Snapshot
	Surface              runtime.TerminalSurface
	SurfaceVersion       uint64
	ScrollOffset         int
	Active               bool
	Floating             bool
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

type paneFrameKey struct {
	Rect            workbench.Rect
	Frameless       bool
	SharedLeft      bool
	SharedTop       bool
	Title           string
	Border          paneBorderInfo
	ThemeBG         string
	Overflow        paneOverflowHints
	Active          bool
	Floating        bool
	ChromeSignature string
}

type paneOverflowHints struct {
	Right  bool
	Bottom bool
}

type renderTerminalMetrics struct {
	Cols int
	Rows int
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

type bodyRenderCache struct {
	width             int
	height            int
	order             []string
	rects             map[string]workbench.Rect
	frameKeys         map[string]paneFrameKey
	contentKeys       map[string]paneContentKey
	contentSprites    map[string]*paneContentSpriteCacheEntry
	canvas            *composedCanvas
	hostEmojiVS16Mode shared.AmbiguousEmojiVariationSelectorMode
}

type paneContentSpriteKey struct {
	ContentKey paneContentKey
	Theme      uiTheme
	Width      int
	Height     int
}

type paneContentSpriteCacheEntry struct {
	key    paneContentSpriteKey
	canvas *composedCanvas
}

func NewCoordinator(fn VisibleStateFn) *Coordinator {
	return &Coordinator{
		visibleFn:          fn,
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
	if c == nil || c.visibleFn == nil {
		return ""
	}
	state := c.visibleFn()
	key := stateKey(state)
	c.mu.Lock()
	if !c.dirty && c.lastFrame != "" && c.lastState == key {
		frame = c.lastFrame
		cacheMetric = "render.frame.cache_hit"
		c.mu.Unlock()
		return frame
	}
	c.mu.Unlock()
	if state.Workbench == nil {
		c.mu.Lock()
		c.lastFrame = "tuiv2"
		c.lastCursor = hideCursorANSI()
		c.lastState = key
		c.dirty = false
		frame = c.lastFrame
		c.mu.Unlock()
		return frame
	}

	immersiveZoom := immersiveZoomActive(state)
	bodyCursorOffsetY := TopChromeRows
	if immersiveZoom {
		bodyCursorOffsetY = 0
	}
	tabBar := ""
	statusBar := ""
	bodyHeight := FrameBodyHeight(state.TermSize.Height)
	if immersiveZoom {
		bodyHeight = maxInt(1, state.TermSize.Height)
	} else {
		tabBar = c.renderTabBarCached(state)
		statusBar = c.renderStatusBarCached(state)
	}
	rendered := renderBodyFrameWithCoordinator(c, state, state.TermSize.Width, bodyHeight)
	body := rendered.content
	cursor := rendered.cursor

	overlaySize := TermSize{Width: state.TermSize.Width, Height: bodyHeight}
	overlayCursorVisible := true
	c.mu.Lock()
	overlayCursorVisible = c.cursorBlinkVisible
	c.mu.Unlock()
	if overlay := renderActiveOverlayWithCursor(state, overlaySize, bodyCursorOffsetY, overlayCursorVisible); overlay.content != "" {
		body = compositeOverlay(body, overlay.content, TermSize{Width: state.TermSize.Width, Height: bodyHeight})
		cursor = overlay.cursor
		rendered.blink = overlay.blink
	}
	frameParts := []string{body}
	if !immersiveZoom {
		frameParts = []string{tabBar, body, statusBar}
	}
	frame = strings.Join(frameParts, "\n")
	c.mu.Lock()
	if !rendered.blink {
		c.cursorBlinkVisible = true
	}
	c.lastFrame = frame
	c.lastCursor = cursor
	c.lastState = key
	c.dirty = false
	frame = c.lastFrame
	c.mu.Unlock()
	return frame
}

func stateKey(state VisibleRenderState) renderStateKey {
	return renderStateKey{
		Workbench:                  state.Workbench,
		Runtime:                    state.Runtime,
		SurfaceKind:                state.Surface.Kind,
		SurfaceTerminalPool:        state.Surface.TerminalPool,
		OverlayKind:                state.Overlay.Kind,
		OverlayPrompt:              state.Overlay.Prompt,
		OverlayPicker:              state.Overlay.Picker,
		OverlayWorkspacePicker:     state.Overlay.WorkspacePicker,
		OverlayTerminalManager:     state.Overlay.TerminalManager,
		OverlayHelp:                state.Overlay.Help,
		OverlayFloatingOverview:    state.Overlay.FloatingOverview,
		TermSize:                   state.TermSize,
		Notice:                     state.Notice,
		Error:                      state.Error,
		InputMode:                  state.InputMode,
		OwnerConfirmPaneID:         state.OwnerConfirmPaneID,
		EmptyPaneSelectionPaneID:   state.EmptyPaneSelectionPaneID,
		EmptyPaneSelectionIndex:    state.EmptyPaneSelectionIndex,
		ExitedPaneSelectionPaneID:  state.ExitedPaneSelectionPaneID,
		ExitedPaneSelectionIndex:   state.ExitedPaneSelectionIndex,
		PaneSnapshotOverridePaneID: state.PaneSnapshotOverridePaneID,
		PaneSnapshotOverride:       state.PaneSnapshotOverride,
		CopyModePaneID:             state.CopyModePaneID,
		CopyModeCursorRow:          state.CopyModeCursorRow,
		CopyModeCursorCol:          state.CopyModeCursorCol,
		CopyModeViewTopRow:         state.CopyModeViewTopRow,
		CopyModeMarkSet:            state.CopyModeMarkSet,
		CopyModeMarkRow:            state.CopyModeMarkRow,
		CopyModeMarkCol:            state.CopyModeMarkCol,
		CopyModeSnapshot:           state.CopyModeSnapshot,
	}
}

func (c *Coordinator) CursorSequence() string {
	if c == nil {
		return hideCursorANSI()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastCursor == "" {
		return hideCursorANSI()
	}
	return c.lastCursor
}

func (c *Coordinator) CachedFrameAndCursor() (string, string, bool) {
	if c == nil || c.visibleFn == nil {
		return "", hideCursorANSI(), false
	}
	state := c.visibleFn()
	key := stateKey(state)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dirty || c.lastFrame == "" || c.lastState != key {
		return "", "", false
	}
	cursor := c.lastCursor
	if cursor == "" {
		cursor = hideCursorANSI()
	}
	return c.lastFrame, cursor, true
}

func (c *Coordinator) NeedsCursorTicks() bool {
	if c == nil || c.visibleFn == nil {
		return false
	}
	return visibleStateNeedsCursorBlink(c.visibleFn())
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

func (c *Coordinator) renderTabBarCached(state VisibleRenderState) string {
	value := renderTabBar(state)
	c.mu.Lock()
	// Reuse the previous string allocation if the content is identical
	// to reduce GC pressure and help bubbletea detect unchanged lines.
	if c.tabBarValue == value {
		value = c.tabBarValue
	} else {
		c.tabBarValue = value
	}
	c.mu.Unlock()
	return value
}

func (c *Coordinator) renderStatusBarCached(state VisibleRenderState) string {
	value := renderStatusBar(state)
	c.mu.Lock()
	if c.statusValue == value {
		value = c.statusValue
	} else {
		c.statusValue = value
	}
	c.mu.Unlock()
	return value
}

func renderBody(state VisibleRenderState, width, height int) string {
	return renderBodyFrameWithCoordinator(nil, state, width, height).content
}

func renderBodyFrame(state VisibleRenderState, width, height int) renderedBody {
	return renderBodyFrameWithCoordinator(nil, state, width, height)
}

func renderBodyFrameWithCoordinator(coordinator *Coordinator, state VisibleRenderState, width, height int) renderedBody {
	if width <= 0 || height <= 0 {
		return renderedBody{}
	}
	cursorOffsetY := TopChromeRows
	if immersiveZoomActive(state) {
		cursorOffsetY = 0
	}
	if state.Surface.Kind == VisibleSurfaceTerminalPool && state.Surface.TerminalPool != nil {
		cursorVisible := true
		if coordinator != nil {
			coordinator.mu.Lock()
			cursorVisible = coordinator.cursorBlinkVisible
			coordinator.mu.Unlock()
		}
		return renderTerminalPoolPageWithCursor(state.Surface.TerminalPool, state.Runtime, TermSize{Width: width, Height: height}, cursorOffsetY, cursorVisible)
	}
	if state.Workbench == nil {
		return renderedBody{content: strings.Repeat("\n", maxInt(0, height-1))}
	}

	activeTabIdx := state.Workbench.ActiveTab
	if activeTabIdx < 0 || activeTabIdx >= len(state.Workbench.Tabs) {
		return renderEmptyWorkbenchBody(state, width, height, emptyWorkbenchNoTabs)
	}
	tab := state.Workbench.Tabs[activeTabIdx]
	if len(tab.Panes) == 0 {
		return renderEmptyWorkbenchBody(state, width, height, emptyWorkbenchNoPanes)
	}
	lookup := newRuntimeLookup(state.Runtime)
	exitedSelectionPulse := true
	if coordinator != nil {
		coordinator.mu.Lock()
		exitedSelectionPulse = coordinator.cursorBlinkVisible
		coordinator.mu.Unlock()
	}
	entries := paneEntriesForTab(tab, state.Workbench.FloatingPanes, width, height, lookup, state.OwnerConfirmPaneID, state.EmptyPaneSelectionPaneID, state.EmptyPaneSelectionIndex, state.ExitedPaneSelectionPaneID, state.ExitedPaneSelectionIndex, exitedSelectionPulse, state, uiThemeForRuntime(state.Runtime))

	canvas := renderBodyCanvas(coordinator, state, entries, width, height)
	return renderedBody{
		content: canvas.contentString(),
		cursor:  canvas.cursorANSI(),
		blink:   canvas.syntheticCursorBlink,
	}
}

func visibleStateNeedsCursorBlink(state VisibleRenderState) bool {
	if overlayNeedsCursorBlink(state.Overlay) || terminalPoolNeedsCursorBlink(state.Surface) {
		return true
	}
	if state.Overlay.Kind != VisibleOverlayNone || state.Surface.Kind != VisibleSurfaceWorkbench {
		return false
	}
	if state.Workbench == nil || state.Runtime == nil {
		return false
	}
	activeTabIdx := state.Workbench.ActiveTab
	if activeTabIdx < 0 || activeTabIdx >= len(state.Workbench.Tabs) {
		return false
	}
	tab := state.Workbench.Tabs[activeTabIdx]
	if len(tab.Panes) == 0 {
		return false
	}
	width := state.TermSize.Width
	height := FrameBodyHeight(state.TermSize.Height)
	if immersiveZoomActive(state) {
		height = maxInt(1, state.TermSize.Height)
	}
	if width <= 0 || height <= 0 {
		return false
	}
	if activeExitedPaneHasRecoverySelection(state) {
		return true
	}
	// Keep the in-pane synthetic cursor steady. Blinking it forces full-frame
	// redraws every tick, which causes visible footer/status-bar shimmer in
	// some host terminals even though only the pane cursor changes.
	return false
}

func overlayNeedsCursorBlink(overlay VisibleOverlay) bool {
	switch overlay.Kind {
	case VisibleOverlayPrompt, VisibleOverlayPicker, VisibleOverlayWorkspacePicker, VisibleOverlayTerminalManager:
		return true
	default:
		return false
	}
}

func terminalPoolNeedsCursorBlink(surface VisibleSurface) bool {
	return surface.Kind == VisibleSurfaceTerminalPool && surface.TerminalPool != nil
}

func activeExitedPaneHasRecoverySelection(state VisibleRenderState) bool {
	if state.Workbench == nil || state.Runtime == nil || state.ExitedPaneSelectionPaneID == "" {
		return false
	}
	activeTabIdx := state.Workbench.ActiveTab
	if activeTabIdx < 0 || activeTabIdx >= len(state.Workbench.Tabs) {
		return false
	}
	tab := state.Workbench.Tabs[activeTabIdx]
	if tab.ActivePaneID == "" || tab.ActivePaneID != state.ExitedPaneSelectionPaneID {
		return false
	}
	for i := range tab.Panes {
		pane := &tab.Panes[i]
		if pane.ID != tab.ActivePaneID || pane.TerminalID == "" {
			continue
		}
		terminal := findVisibleTerminal(state.Runtime, pane.TerminalID)
		return terminal != nil && terminal.State == "exited"
	}
	return false
}

type emptyWorkbenchKind uint8

const (
	emptyWorkbenchNoTabs emptyWorkbenchKind = iota
	emptyWorkbenchNoPanes
)

func renderEmptyWorkbenchBody(state VisibleRenderState, width, height int, kind emptyWorkbenchKind) renderedBody {
	canvas := newComposedCanvas(width, height)
	canvas.hostEmojiVS16Mode = emojiVariationSelectorModeForRuntime(state.Runtime)
	theme := uiThemeForState(state)

	headline := "No tabs in this workspace"
	details := []string{
		"Ctrl-F open terminal picker",
		"Ctrl-T then c create a new tab",
	}
	if kind == emptyWorkbenchNoPanes {
		headline = "No panes in this tab"
		details = []string{
			"Ctrl-F create the first pane via terminal picker",
			"Ctrl-T then c create a fresh tab",
		}
	}

	lines := append([]string{headline}, details...)
	startY := maxInt(0, (height-len(lines))/2)
	for i, line := range lines {
		y := startY + i
		if y >= height {
			break
		}
		text := centerText(xansi.Truncate(line, width, ""), width)
		style := drawStyle{FG: theme.panelMuted}
		if i == 0 {
			style = drawStyle{FG: theme.panelText, Bold: true}
		}
		canvas.drawText(0, y, text, style)
	}

	return renderedBody{
		content: canvas.contentString(),
		cursor:  hideCursorANSI(),
	}
}

func emojiVariationSelectorModeForRuntime(runtimeState *VisibleRuntimeStateProxy) shared.AmbiguousEmojiVariationSelectorMode {
	if runtimeState == nil {
		return shared.AmbiguousEmojiVariationSelectorStrip
	}
	switch runtimeState.HostEmojiVS16Mode {
	case shared.AmbiguousEmojiVariationSelectorRaw:
		return shared.AmbiguousEmojiVariationSelectorRaw
	case shared.AmbiguousEmojiVariationSelectorAdvance, shared.AmbiguousEmojiVariationSelectorStrip:
		// 中文说明：即便探测分类里有 advance，真正进入 Bubble Tea 行渲染时
		// 也要合并到 strip，因为这里不能安全依赖行内光标移动来补齐宽度。
		return shared.AmbiguousEmojiVariationSelectorStrip
	default:
		return shared.AmbiguousEmojiVariationSelectorStrip
	}
}

func renderActiveOverlay(state VisibleRenderState, termSize TermSize) string {
	return renderActiveOverlayWithCursor(state, termSize, 0, true).content
}

func renderActiveOverlayWithCursor(state VisibleRenderState, termSize TermSize, cursorOffsetY int, cursorVisible bool) renderedBody {
	theme := uiThemeForState(state)
	result := renderedBody{cursor: hideCursorANSI()}
	switch state.Overlay.Kind {
	case VisibleOverlayPrompt:
		result.content = renderPromptOverlayWithThemeAndCursor(state.Overlay.Prompt, termSize, theme, cursorVisible)
		result.blink = true
		if cursorVisible {
			if x, y, ok := promptOverlayCursorTarget(state.Overlay.Prompt, termSize); ok {
				result.cursor = hostCursorANSI(x, y+cursorOffsetY, "bar", false)
			}
		}
	case VisibleOverlayPicker:
		result.content = renderPickerOverlayWithThemeAndCursor(state.Overlay.Picker, termSize, theme, cursorVisible)
		result.blink = true
		if cursorVisible {
			if x, y, ok := pickerOverlayCursorTarget(state.Overlay.Picker, termSize); ok {
				result.cursor = hostCursorANSI(x, y+cursorOffsetY, "bar", false)
			}
		}
	case VisibleOverlayWorkspacePicker:
		result.content = renderWorkspacePickerOverlayWithThemeAndCursor(state.Overlay.WorkspacePicker, termSize, theme, cursorVisible)
		result.blink = true
		if cursorVisible {
			if x, y, ok := workspacePickerOverlayCursorTarget(state.Overlay.WorkspacePicker, termSize); ok {
				result.cursor = hostCursorANSI(x, y+cursorOffsetY, "bar", false)
			}
		}
	case VisibleOverlayTerminalManager:
		result.content = renderTerminalManagerOverlayWithThemeAndCursor(state.Overlay.TerminalManager, termSize, theme, cursorVisible)
		result.blink = true
		if cursorVisible {
			if x, y, ok := terminalManagerOverlayCursorTarget(state.Overlay.TerminalManager, termSize); ok {
				result.cursor = hostCursorANSI(x, y+cursorOffsetY, "bar", false)
			}
		}
	case VisibleOverlayHelp:
		result.content = renderHelpOverlayWithTheme(state.Overlay.Help, termSize, theme)
	case VisibleOverlayFloatingOverview:
		result.content = renderFloatingOverviewOverlayWithTheme(state.Overlay.FloatingOverview, termSize, theme)
	default:
		return renderedBody{}
	}
	return result
}

func renderBodyCanvas(coordinator *Coordinator, state VisibleRenderState, entries []paneRenderEntry, width, height int) *composedCanvas {
	immersiveZoom := immersiveZoomActive(state)
	hostEmojiMode := emojiVariationSelectorModeForRuntime(state.Runtime)
	cursorOffsetY := TopChromeRows
	if immersiveZoom {
		cursorOffsetY = 0
	}
	if coordinator == nil {
		canvas := newComposedCanvas(width, height)
		canvas.hostEmojiVS16Mode = hostEmojiMode
		canvas.cursorOffsetY = cursorOffsetY
		for _, entry := range entries {
			if !entry.Frameless {
				drawPaneFrame(canvas, entry.Rect, entry.SharedLeft, entry.SharedTop, entry.Title, entry.Border, entry.Theme, entry.Overflow, entry.Active, entry.Floating)
			}
			drawPaneContentWithKey(canvas, entry.Rect, entry, state.Runtime)
		}
		projectActiveEntryCursor(canvas, entries, state.Runtime)
		return canvas
	}
	cache := coordinator.bodyCache
	overlap := entriesOverlap(entries)
	if cache == nil || !cache.matches(entries, width, height, hostEmojiMode) {
		canvas := rebuildBodyCanvas(cache, entries, width, height, hostEmojiMode, cursorOffsetY, coordinator.syntheticCursorVisible, state.Runtime)
		coordinator.bodyCache = newBodyRenderCache(cache, canvas, entries, width, height)
		return canvas
	}

	// Overlapping panes need a full rebuild. The cached active-pane refresh path
	// redraws the active pane content to clear the old cursor, which is correct
	// for tiled layouts but will paint over floating panes layered above it.
	if overlap {
		canvas := rebuildBodyCanvas(cache, entries, width, height, hostEmojiMode, cursorOffsetY, coordinator.syntheticCursorVisible, state.Runtime)
		cache.canvas = canvas
		cache.reset(entries, width, height)
		return canvas
	}

	if !overlap {
		changed := false
		cache.canvas.clearCursor()
		for _, entry := range entries {
			frameChanged := false
			if cache.frameKeys[entry.PaneID] != entry.FrameKey {
				if entry.Frameless {
					fillRect(cache.canvas, entry.Rect, blankDrawCell())
				} else {
					drawPaneFrame(cache.canvas, entry.Rect, entry.SharedLeft, entry.SharedTop, entry.Title, entry.Border, entry.Theme, entry.Overflow, entry.Active, entry.Floating)
				}
				frameChanged = true
				changed = true
			}
			if frameChanged || cache.contentKeys[entry.PaneID] != entry.ContentKey {
				drawPaneContentFromCache(cache.canvas, cache, entry, state.Runtime, true)
				changed = true
			}
		}
		restoreActiveEntryContent(cache.canvas, entries, state.Runtime)
		if changed {
			projectActiveEntryCursor(cache.canvas, entries, state.Runtime)
			cache.reset(entries, width, height)
			return cache.canvas
		}
		projectActiveEntryCursor(cache.canvas, entries, state.Runtime)
		return cache.canvas
	}

	return cache.canvas
}

func rebuildBodyCanvas(cache *bodyRenderCache, entries []paneRenderEntry, width, height int, hostEmojiMode shared.AmbiguousEmojiVariationSelectorMode, cursorOffsetY int, cursorVisibleFn func(protocol.CursorState) bool, runtimeState *VisibleRuntimeStateProxy) *composedCanvas {
	var canvas *composedCanvas
	if cache != nil && cache.canvas != nil && cache.width == width && cache.height == height {
		canvas = cache.canvas
		canvas.hostEmojiVS16Mode = hostEmojiMode
		canvas.cursorOffsetY = cursorOffsetY
		canvas.syntheticCursorVisibleFn = cursorVisibleFn
		canvas.resetToBlank()
	} else {
		canvas = newComposedCanvas(width, height)
		canvas.hostEmojiVS16Mode = hostEmojiMode
		canvas.cursorOffsetY = cursorOffsetY
		canvas.syntheticCursorVisibleFn = cursorVisibleFn
	}
	for _, entry := range entries {
		if !entry.Frameless {
			drawPaneFrame(canvas, entry.Rect, entry.SharedLeft, entry.SharedTop, entry.Title, entry.Border, entry.Theme, entry.Overflow, entry.Active, entry.Floating)
		}
		drawPaneContentFromCache(canvas, cache, entry, runtimeState, false)
	}
	projectActiveEntryCursor(canvas, entries, runtimeState)
	return canvas
}

func drawPaneContentFromCache(canvas *composedCanvas, cache *bodyRenderCache, entry paneRenderEntry, runtimeState *VisibleRuntimeStateProxy, clearInterior bool) {
	if canvas == nil {
		return
	}
	if cache == nil {
		drawPaneContentWithKey(canvas, entry.Rect, entry, runtimeState)
		return
	}
	interior := interiorRectForEntry(entry)
	if interior.W <= 0 || interior.H <= 0 {
		return
	}
	sprite := cache.contentSprite(entry, runtimeState)
	if sprite == nil {
		drawPaneContentWithKey(canvas, entry.Rect, entry, runtimeState)
		return
	}
	if clearInterior {
		fillRect(canvas, interior, blankDrawCell())
	}
	canvas.blit(sprite, interior.X, interior.Y)
}

func (c *bodyRenderCache) contentSprite(entry paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) *composedCanvas {
	if c == nil {
		return nil
	}
	interior := interiorRectForEntry(entry)
	if interior.W <= 0 || interior.H <= 0 {
		return nil
	}
	key := paneContentSpriteKey{
		ContentKey: entry.ContentKey,
		Theme:      entry.Theme,
		Width:      interior.W,
		Height:     interior.H,
	}
	if c.contentSprites == nil {
		c.contentSprites = make(map[string]*paneContentSpriteCacheEntry)
	}
	if cached := c.contentSprites[entry.PaneID]; cached != nil && cached.key == key && cached.canvas != nil {
		perftrace.Count("render.pane_content_sprite.hit", interior.W*interior.H)
		return cached.canvas
	}
	var sprite *composedCanvas
	if cached := c.contentSprites[entry.PaneID]; cached != nil && cached.canvas != nil && cached.key.Width == key.Width && cached.key.Height == key.Height {
		sprite = cached.canvas
		sprite.resetToBlank()
	} else {
		sprite = newComposedCanvas(interior.W, interior.H)
	}
	drawPaneContentSprite(sprite, entry, runtimeState)
	c.contentSprites[entry.PaneID] = &paneContentSpriteCacheEntry{key: key, canvas: sprite}
	perftrace.Count("render.pane_content_sprite.miss", interior.W*interior.H)
	return sprite
}

func drawPaneContentSprite(canvas *composedCanvas, entry paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) {
	if canvas == nil {
		return
	}
	fillRect(canvas, workbench.Rect{W: canvas.width, H: canvas.height}, blankDrawCell())
	contentRect := localContentRectForEntry(entry)
	if contentRect.W <= 0 || contentRect.H <= 0 {
		return
	}
	if entry.TerminalID == "" {
		drawEmptyPaneContent(canvas, contentRect, entry.PaneID, entry.TerminalID, entry.Theme, entry.EmptyActionSelected)
		return
	}
	terminal := findVisibleTerminal(runtimeState, entry.TerminalID)
	if terminal == nil {
		drawEmptyPaneContent(canvas, contentRect, entry.PaneID, entry.TerminalID, entry.Theme, -1)
		return
	}
	snapshot := entry.Snapshot
	surface := entry.Surface
	if snapshot == nil && surface == nil {
		surface = terminal.Surface
	}
	if snapshot == nil && surface == nil {
		snapshot = terminal.Snapshot
	}
	source := renderSource(snapshot, surface)
	if source == nil || source.ScreenRows() == 0 {
		canvas.drawText(contentRect.X, contentRect.Y, terminal.Name+" ["+terminal.State+"]", drawStyle{FG: entry.Theme.panelMuted})
		if terminal.State == "exited" {
			drawExitedPaneRecoveryHints(canvas, contentRect, entry.Theme, entry.ExitedActionSelected, entry.ExitedActionPulse)
		}
		return
	}
	renderOffset := entry.ScrollOffset
	if entry.CopyModeActive {
		renderOffset = scrollOffsetForViewportTop(snapshot, contentRect.H, entry.CopyModeViewTopRow)
	}
	drawTerminalSourceWithOffset(canvas, contentRect, source, renderOffset, entry.Theme)
	if entry.CopyModeActive {
		drawCopyModeOverlay(canvas, contentRect, snapshot, entry.Theme, entry.CopyModeCursorRow, entry.CopyModeCursorCol, entry.CopyModeViewTopRow, entry.CopyModeMarkSet, entry.CopyModeMarkRow, entry.CopyModeMarkCol)
	}
	if terminal.State == "exited" {
		drawExitedPaneRecoveryHints(canvas, contentRect, entry.Theme, entry.ExitedActionSelected, entry.ExitedActionPulse)
	}
}

func restoreActiveEntryContent(canvas *composedCanvas, entries []paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) {
	if canvas == nil {
		return
	}
	for _, entry := range entries {
		if !entry.Active {
			continue
		}
		base := entry
		base.Active = false
		drawPaneContentWithKey(canvas, entry.Rect, base, runtimeState)
		return
	}
}

func paneEntriesForTab(tab workbench.VisibleTab, floating []workbench.VisiblePane, width, height int, lookup runtimeLookup, confirmPaneID, emptyPaneSelectionPaneID string, emptyPaneSelectionIndex int, exitedPaneSelectionPaneID string, exitedPaneSelectionIndex int, exitedPaneSelectionPulse bool, state VisibleRenderState, theme uiTheme) []paneRenderEntry {
	entries := make([]paneRenderEntry, 0, len(tab.Panes)+len(floating))
	zoomedPaneID := tab.ZoomedPaneID
	immersiveZoom := immersiveZoomActive(state)
	for _, pane := range tab.Panes {
		originalRect := pane.Rect
		rect := originalRect
		frameless := false
		if zoomedPaneID != "" {
			if pane.ID != zoomedPaneID {
				continue
			}
			originalRect = workbench.Rect{X: 0, Y: 0, W: width, H: height}
			rect = workbench.Rect{X: 0, Y: 0, W: width, H: height}
			frameless = immersiveZoom
		}
		rect, ok := clipRectToViewport(rect, width, height)
		if !ok {
			continue
		}
		entries = append(entries, buildPaneRenderEntry(pane, originalRect, rect, frameless, tab.ActivePaneID, tab.ScrollOffset, lookup, confirmPaneID, emptyPaneSelectionPaneID, emptyPaneSelectionIndex, exitedPaneSelectionPaneID, exitedPaneSelectionIndex, exitedPaneSelectionPulse, state.PaneSnapshotOverridePaneID, state.PaneSnapshotOverride, state.CopyModePaneID, state.CopyModeCursorRow, state.CopyModeCursorCol, state.CopyModeViewTopRow, state.CopyModeMarkSet, state.CopyModeMarkRow, state.CopyModeMarkCol, state.CopyModeSnapshot, theme))
	}
	for _, pane := range floating {
		originalRect := pane.Rect
		rect, ok := clipRectToViewport(originalRect, width, height)
		if !ok {
			continue
		}
		entries = append(entries, buildPaneRenderEntry(pane, originalRect, rect, false, tab.ActivePaneID, tab.ScrollOffset, lookup, confirmPaneID, emptyPaneSelectionPaneID, emptyPaneSelectionIndex, exitedPaneSelectionPaneID, exitedPaneSelectionIndex, exitedPaneSelectionPulse, state.PaneSnapshotOverridePaneID, state.PaneSnapshotOverride, state.CopyModePaneID, state.CopyModeCursorRow, state.CopyModeCursorCol, state.CopyModeViewTopRow, state.CopyModeMarkSet, state.CopyModeMarkRow, state.CopyModeMarkCol, state.CopyModeSnapshot, theme))
	}
	return entries
}

func clipRectToViewport(rect workbench.Rect, width, height int) (workbench.Rect, bool) {
	if rect.W <= 0 || rect.H <= 0 || width <= 0 || height <= 0 {
		return workbench.Rect{}, false
	}
	x1 := maxInt(rect.X, 0)
	y1 := maxInt(rect.Y, 0)
	x2 := minInt(rect.X+rect.W, width)
	y2 := minInt(rect.Y+rect.H, height)
	if x1 >= x2 || y1 >= y2 {
		return workbench.Rect{}, false
	}
	return workbench.Rect{X: x1, Y: y1, W: x2 - x1, H: y2 - y1}, true
}

func buildPaneRenderEntry(pane workbench.VisiblePane, originalRect, rect workbench.Rect, frameless bool, activePaneID string, scrollOffset int, lookup runtimeLookup, confirmPaneID, emptyPaneSelectionPaneID string, emptyPaneSelectionIndex int, exitedPaneSelectionPaneID string, exitedPaneSelectionIndex int, exitedPaneSelectionPulse bool, paneSnapshotOverridePaneID string, paneSnapshotOverride *protocol.Snapshot, copyModePaneID string, copyModeCursorRow, copyModeCursorCol, copyModeViewTopRow int, copyModeMarkSet bool, copyModeMarkRow, copyModeMarkCol int, copyModeSnapshot *protocol.Snapshot, theme uiTheme) paneRenderEntry {
	active := pane.ID == activePaneID
	title := resolvePaneTitleWithLookup(pane, lookup)
	border := paneBorderInfoWithLookup(pane, lookup, confirmPaneID)
	terminal := lookup.terminal(pane.TerminalID)
	overflow := paneOverflowHintsForRender(originalRect, rect, nil, nil)
	copyModeActive := pane.ID == copyModePaneID
	snapshot := (*protocol.Snapshot)(nil)
	surface := runtime.TerminalSurface(nil)
	surfaceVersion := uint64(0)
	if terminal != nil {
		snapshot = terminal.Snapshot
		surface = terminal.Surface
		surfaceVersion = terminal.SurfaceVersion
	}
	if pane.ID == paneSnapshotOverridePaneID && paneSnapshotOverride != nil {
		snapshot = paneSnapshotOverride
		surface = nil
		surfaceVersion = 0
	}
	if copyModeActive && copyModeSnapshot != nil {
		snapshot = copyModeSnapshot
		surface = nil
		surfaceVersion = 0
	}
	if copyModeActive {
		border.CopyTimeLabel = copyModeTimestampLabel(snapshot, copyModeCursorRow)
		border.CopyRowLabel = copyModeRowPositionLabel(snapshot, copyModeCursorRow)
	}
	emptyActionSelected := -1
	if pane.TerminalID == "" && pane.ID == emptyPaneSelectionPaneID {
		emptyActionSelected = emptyPaneSelectionIndex
	}
	exitedActionSelected := -1
	if pane.TerminalID != "" && pane.ID == exitedPaneSelectionPaneID {
		if terminal := lookup.terminal(pane.TerminalID); terminal != nil && terminal.State == "exited" {
			exitedActionSelected = exitedPaneSelectionIndex
		}
	}
	contentKey := paneContentKey{
		TerminalID:           pane.TerminalID,
		ThemeBG:              theme.panelBG,
		TerminalKnown:        terminal != nil,
		SharedLeft:           pane.SharedLeft,
		SharedTop:            pane.SharedTop,
		ScrollOffset:         scrollOffset,
		EmptyActionSelected:  emptyActionSelected,
		ExitedActionSelected: exitedActionSelected,
		ExitedActionPulse:    exitedPaneSelectionPulse,
		CopyModeActive:       copyModeActive,
		CopyModeCursorRow:    copyModeCursorRow,
		CopyModeCursorCol:    copyModeCursorCol,
		CopyModeViewTopRow:   copyModeViewTopRow,
		CopyModeMarkSet:      copyModeMarkSet,
		CopyModeMarkRow:      copyModeMarkRow,
		CopyModeMarkCol:      copyModeMarkCol,
	}
	if terminal != nil {
		if snapshot != nil && surface == nil {
			contentKey.Snapshot = snapshot
		}
		contentKey.SurfaceVersion = surfaceVersion
		contentKey.Name = terminal.Name
		contentKey.State = terminal.State
		overflow = paneOverflowHintsForRender(originalRect, rect, snapshot, surface)
	}
	return paneRenderEntry{
		PaneID:     pane.ID,
		Rect:       rect,
		Frameless:  frameless,
		SharedLeft: pane.SharedLeft,
		SharedTop:  pane.SharedTop,
		Title:      title,
		Border:     border,
		Theme:      theme,
		Overflow:   overflow,
		ContentKey: contentKey,
		FrameKey: paneFrameKey{
			Rect:            rect,
			Frameless:       frameless,
			SharedLeft:      pane.SharedLeft,
			SharedTop:       pane.SharedTop,
			Title:           title,
			Border:          border,
			ThemeBG:         theme.panelBG,
			Overflow:        overflow,
			Active:          active,
			Floating:        pane.Floating,
			ChromeSignature: paneChromeActionSignatureForFrame(rect, title, border, pane.Floating),
		},
		TerminalID:           pane.TerminalID,
		Snapshot:             snapshot,
		Surface:              surface,
		SurfaceVersion:       surfaceVersion,
		ScrollOffset:         scrollOffset,
		Active:               active,
		Floating:             pane.Floating,
		EmptyActionSelected:  emptyActionSelected,
		ExitedActionSelected: exitedActionSelected,
		ExitedActionPulse:    exitedPaneSelectionPulse,
		CopyModeActive:       copyModeActive,
		CopyModeCursorRow:    copyModeCursorRow,
		CopyModeCursorCol:    copyModeCursorCol,
		CopyModeViewTopRow:   copyModeViewTopRow,
		CopyModeMarkSet:      copyModeMarkSet,
		CopyModeMarkRow:      copyModeMarkRow,
		CopyModeMarkCol:      copyModeMarkCol,
	}
}

func paneOverflowHintsForRender(originalRect, clippedRect workbench.Rect, snapshot *protocol.Snapshot, surface runtime.TerminalSurface) paneOverflowHints {
	if originalRect.W <= 0 || originalRect.H <= 0 || clippedRect.W <= 0 || clippedRect.H <= 0 {
		return paneOverflowHints{}
	}
	overflow := paneOverflowHints{
		Right:  originalRect.X+originalRect.W > clippedRect.X+clippedRect.W,
		Bottom: originalRect.Y+originalRect.H > clippedRect.Y+clippedRect.H,
	}
	metrics := terminalMetricsForSource(renderSource(snapshot, surface))
	contentRect := contentRectForPane(clippedRect)
	if metrics.Cols > 0 && contentRect.W > 0 && metrics.Cols > contentRect.W {
		overflow.Right = true
	}
	if metrics.Rows > 0 && contentRect.H > 0 && metrics.Rows > contentRect.H {
		overflow.Bottom = true
	}
	return overflow
}

func newBodyRenderCache(previous *bodyRenderCache, canvas *composedCanvas, entries []paneRenderEntry, width, height int) *bodyRenderCache {
	cache := &bodyRenderCache{canvas: canvas}
	if previous != nil && previous.contentSprites != nil {
		cache.contentSprites = previous.contentSprites
	}
	cache.reset(entries, width, height)
	return cache
}

func (c *bodyRenderCache) reset(entries []paneRenderEntry, width, height int) {
	if c == nil {
		return
	}
	c.width = width
	c.height = height
	if c.canvas != nil {
		c.hostEmojiVS16Mode = c.canvas.hostEmojiVS16Mode
	}
	c.order = c.order[:0]
	if c.rects == nil {
		c.rects = make(map[string]workbench.Rect, len(entries))
	} else {
		for key := range c.rects {
			delete(c.rects, key)
		}
	}
	if c.frameKeys == nil {
		c.frameKeys = make(map[string]paneFrameKey, len(entries))
	} else {
		for key := range c.frameKeys {
			delete(c.frameKeys, key)
		}
	}
	if c.contentKeys == nil {
		c.contentKeys = make(map[string]paneContentKey, len(entries))
	} else {
		for key := range c.contentKeys {
			delete(c.contentKeys, key)
		}
	}
	keepSprites := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		c.order = append(c.order, entry.PaneID)
		c.rects[entry.PaneID] = entry.Rect
		c.frameKeys[entry.PaneID] = entry.FrameKey
		c.contentKeys[entry.PaneID] = entry.ContentKey
		keepSprites[entry.PaneID] = struct{}{}
	}
	if c.contentSprites != nil {
		for paneID := range c.contentSprites {
			if _, ok := keepSprites[paneID]; !ok {
				delete(c.contentSprites, paneID)
			}
		}
	}
}

func (c *bodyRenderCache) matches(entries []paneRenderEntry, width, height int, hostEmojiMode shared.AmbiguousEmojiVariationSelectorMode) bool {
	if c == nil || c.canvas == nil || c.width != width || c.height != height || c.hostEmojiVS16Mode != hostEmojiMode || len(c.order) != len(entries) {
		return false
	}
	for i, entry := range entries {
		if c.order[i] != entry.PaneID || c.rects[entry.PaneID] != entry.Rect {
			return false
		}
	}
	return true
}

func renderTerminalPoolPageWithCursor(pool *modal.TerminalManagerState, runtimeState *VisibleRuntimeStateProxy, termSize TermSize, cursorOffsetY int, cursorVisible bool) renderedBody {
	if pool == nil {
		return renderedBody{}
	}
	theme := uiThemeForRuntime(runtimeState)
	width := maxInt(1, termSize.Width)
	height := maxInt(1, termSize.Height)
	layout := buildTerminalPoolPageLayout(pool, width, height)
	innerWidth := layout.innerWidth
	headerLines := make([]string, 0, 3)

	title := terminalPickerTitleStyle(theme).Width(width).Render(forceWidthANSIOverlay(coalesce(strings.TrimSpace(pool.Title), "Terminal Pool"), width))
	headerLines = append(headerLines, title)
	headerLines = append(headerLines, forceWidthANSIOverlay(renderOverlaySearchLineWithCursor(theme, pool.Query, pool.Cursor, pool.CursorSet, width, cursorVisible), width))
	headerLines = append(headerLines, overlayCardFillStyle(theme).Width(width).Render(""))

	contentLines := make([]string, 0, height)

	items := pool.VisibleItems()
	for _, row := range terminalPoolListRows(items) {
		if row.itemIndex < 0 {
			contentLines = append(contentLines, renderOverlaySpan(overlayCardFillStyle(theme), "  "+overlaySectionTitleStyle(theme).Render(row.groupText), width))
			continue
		}
		line := items[row.itemIndex].RenderLine(innerWidth, row.itemIndex == pool.Selected, pickerLineStyle(theme), pickerSelectedLineStyle(theme), pickerCreateRowStyle(theme))
		contentLines = append(contentLines, renderOverlaySpan(overlayCardFillStyle(theme), "  "+line, width))
	}
	if detailLines := renderTerminalPoolDetails(pool.SelectedItem(), runtimeState, innerWidth); len(detailLines) > 0 {
		contentLines = append(contentLines, overlayCardFillStyle(theme).Width(width).Render(""))
		for _, line := range detailLines {
			contentLines = append(contentLines, renderOverlaySpan(overlayCardFillStyle(theme), "  "+line, width))
		}
	}

	footerLine, _ := layoutTerminalPoolFooterActionsWithTheme(theme, width, height)
	result := renderedBody{
		content: renderPageWithPinnedFooter(headerLines, contentLines, footerLine, width, height),
		cursor:  hideCursorANSI(),
	}
	if cursorVisible {
		cursorX := layout.queryRect.X + valueCursorCellOffset(pool.Query, queryCursorIndex(pool.Query, pool.Cursor, pool.CursorSet), layout.queryRect.W)
		result.cursor = hostCursorANSI(cursorX, layout.queryRect.Y+cursorOffsetY, "bar", false)
	}
	return result
}

func renderTerminalPoolDetails(item *modal.PickerItem, runtimeState *VisibleRuntimeStateProxy, innerWidth int) []string {
	if item == nil {
		return nil
	}
	lookup := newRuntimeLookup(runtimeState)
	lines := []string{forceWidthANSIOverlay("PREVIEW", innerWidth)}
	if terminal := lookup.terminal(item.TerminalID); terminal != nil {
		lines = append(lines, terminalPoolPreviewLines(terminal.Snapshot, terminal.Surface, innerWidth, 4)...)
		if strings.TrimSpace(terminal.OwnerPaneID) != "" {
			lines = append(lines, forceWidthANSIOverlay("owner pane: "+terminal.OwnerPaneID, innerWidth))
		}
		lines = append(lines, forceWidthANSIOverlay("bound panes: "+strconv.Itoa(len(terminal.BoundPaneIDs)), innerWidth))
	} else {
		lines = append(lines, forceWidthANSIOverlay("(no live preview)", innerWidth))
	}
	if strings.TrimSpace(item.Command) != "" {
		if len(lines) > 0 && !strings.Contains(lines[len(lines)-1], "DETAIL") {
			lines = append(lines, forceWidthANSIOverlay("DETAIL", innerWidth))
		}
		lines = append(lines, forceWidthANSIOverlay("command: "+item.Command, innerWidth))
	}
	if strings.TrimSpace(item.Location) != "" {
		lines = append(lines, forceWidthANSIOverlay("location: "+item.Location, innerWidth))
	}
	if strings.TrimSpace(item.Description) != "" {
		lines = append(lines, forceWidthANSIOverlay("status: "+item.Description, innerWidth))
	}
	return lines
}

func terminalPoolPreviewLines(snapshot *protocol.Snapshot, surface runtime.TerminalSurface, innerWidth int, maxLines int) []string {
	source := renderSource(snapshot, surface)
	if source == nil || maxLines <= 0 {
		return []string{forceWidthANSIOverlay("(no live preview)", innerWidth)}
	}
	if source.ScreenRows() == 0 {
		return []string{forceWidthANSIOverlay("(no live preview)", innerWidth)}
	}
	lines := make([]string, 0, minInt(source.ScreenRows(), maxLines))
	base := source.ScrollbackRows()
	for rowIndex := 0; rowIndex < source.ScreenRows() && len(lines) < maxLines; rowIndex++ {
		row := source.Row(base + rowIndex)
		if len(lines) >= maxLines {
			break
		}
		var builder strings.Builder
		for _, cell := range row {
			builder.WriteString(cell.Content)
		}
		text := strings.TrimRight(builder.String(), " ")
		if text == "" {
			text = " "
		}
		lines = append(lines, forceWidthANSIOverlay(text, innerWidth))
	}
	if len(lines) == 0 {
		return []string{forceWidthANSIOverlay("(no live preview)", innerWidth)}
	}
	return lines
}

func renderPageWithPinnedFooter(headerLines, contentLines []string, footerLine string, width, height int) string {
	if height <= 0 {
		return ""
	}
	if height == 1 {
		return forceWidthANSIOverlay(footerLine, width)
	}

	lines := make([]string, 0, height)
	lines = append(lines, headerLines...)
	lines = append(lines, contentLines...)
	if len(lines) > height-1 {
		lines = lines[:height-1]
	}
	for len(lines) < height-1 {
		lines = append(lines, forceWidthANSIOverlay("", width))
	}
	lines = append(lines, forceWidthANSIOverlay(footerLine, width))
	return strings.Join(lines, "\n")
}

// resolvePaneTitle returns the stable title for a pane, preferring terminal
// metadata name, then the persisted pane title. OSC/program titles are not used
// for pane chrome because the chrome should stay anchored to the terminal name.
func resolvePaneTitle(pane workbench.VisiblePane, runtimeState *VisibleRuntimeStateProxy) string {
	return resolvePaneTitleWithLookup(pane, newRuntimeLookup(runtimeState))
}

func resolvePaneTitleWithLookup(pane workbench.VisiblePane, lookup runtimeLookup) string {
	if strings.TrimSpace(pane.TerminalID) == "" {
		return "unconnected"
	}
	if terminal := lookup.terminal(pane.TerminalID); terminal != nil {
		if terminal.Name != "" {
			return terminal.Name
		}
	}
	return pane.Title
}

// drawPaneFrame draws the border box with a title on the left and stable chrome slots on the right.
func drawPaneFrame(canvas *composedCanvas, rect workbench.Rect, sharedLeft, sharedTop bool, title string, border paneBorderInfo, theme uiTheme, overflow paneOverflowHints, active bool, floating bool) {
	_ = sharedLeft
	_ = sharedTop
	if rect.W < 2 || rect.H < 2 {
		return
	}
	borderFG := theme.panelBorder2
	titleFG := theme.panelMuted
	metaFG := theme.panelMuted
	actionFG := theme.panelMuted
	stateFG := theme.panelMuted
	if active {
		borderFG = theme.chromeAccent
		titleFG = theme.panelText
		metaFG = theme.panelMuted
		actionFG = theme.panelText
		switch border.StateTone {
		case "success":
			stateFG = theme.success
		case "warning":
			stateFG = theme.warning
		case "danger":
			stateFG = theme.danger
		default:
			stateFG = metaFG
		}
	}
	borderStyle := drawStyle{FG: borderFG}
	chromeStyles := paneChromeDrawStyles{
		Title:         drawStyle{FG: titleFG, Bold: true},
		Meta:          drawStyle{FG: metaFG},
		State:         drawStyle{FG: stateFG},
		Action:        drawStyle{FG: actionFG, Bold: active},
		EmphasizeRole: active,
	}
	if floating {
		// Floating panes are true overlays. If we merge their border connections
		// with whatever tiled border glyph is already underneath, the corner cell
		// turns into ├/┼ and the single resulting glyph inherits only one style.
		// That visually "activates" the underlying pane border at the junction.
		// Overwrite the floating frame directly so its corners stay real corners.
		drawDirectPaneBorder(canvas, rect, borderStyle)
		drawPaneOverflowMarkers(canvas, rect, theme, overflow, active)
		drawPaneTopBorderLabels(canvas, rect, chromeStyles, title, border, floating)
		return
	}
	// Framed split panes intentionally keep their own left/top borders instead
	// of merging into a single shared divider. Collapsing neighboring pane
	// frames saves a column, but it also changes the visual contract of split
	// layouts and makes the center separator disappear into one line.
	drawHorizontalBorder(canvas, rect.X, rect.X+rect.W-1, rect.Y, borderStyle, false, true, false)
	drawHorizontalBorder(canvas, rect.X, rect.X+rect.W-1, rect.Y+rect.H-1, borderStyle, false, false, true)
	drawVerticalBorder(canvas, rect.X, verticalBorderStart(rect.Y, false), rect.Y+rect.H-2, borderStyle, false)
	drawVerticalBorder(canvas, rect.X+rect.W-1, verticalBorderStart(rect.Y, false), rect.Y+rect.H-2, borderStyle, false)

	drawPaneOverflowMarkers(canvas, rect, theme, overflow, active)
	drawPaneTopBorderLabels(canvas, rect, chromeStyles, title, border, floating)
}

func drawDirectPaneBorder(canvas *composedCanvas, rect workbench.Rect, style drawStyle) {
	if canvas == nil || rect.W < 2 || rect.H < 2 {
		return
	}
	for x := rect.X; x < rect.X+rect.W; x++ {
		canvas.set(x, rect.Y, drawCell{Content: "─", Width: 1, Style: style})
		canvas.set(x, rect.Y+rect.H-1, drawCell{Content: "─", Width: 1, Style: style})
	}
	for y := rect.Y; y < rect.Y+rect.H; y++ {
		canvas.set(rect.X, y, drawCell{Content: "│", Width: 1, Style: style})
		canvas.set(rect.X+rect.W-1, y, drawCell{Content: "│", Width: 1, Style: style})
	}
	canvas.set(rect.X, rect.Y, drawCell{Content: "┌", Width: 1, Style: style})
	canvas.set(rect.X+rect.W-1, rect.Y, drawCell{Content: "┐", Width: 1, Style: style})
	canvas.set(rect.X, rect.Y+rect.H-1, drawCell{Content: "└", Width: 1, Style: style})
	canvas.set(rect.X+rect.W-1, rect.Y+rect.H-1, drawCell{Content: "┘", Width: 1, Style: style})
}

func drawPaneOverflowMarkers(canvas *composedCanvas, rect workbench.Rect, theme uiTheme, overflow paneOverflowHints, active bool) {
	if canvas == nil || rect.W < 3 || rect.H < 3 {
		return
	}
	if overflow.Right {
		markerFG := theme.panelMuted
		if active {
			markerFG = ensureContrast(mixHex(theme.chromeAccent, theme.panelText, 0.35), theme.hostBG, 4.2)
		}
		markerStyle := drawStyle{FG: markerFG, Bold: active}
		canvas.set(rect.X+rect.W-1, rect.Y+rect.H-2, drawCell{Content: ">", Width: 1, Style: markerStyle})
	}
	if overflow.Bottom {
		markerFG := theme.panelMuted
		if active {
			markerFG = ensureContrast(theme.warning, theme.hostBG, 4.2)
		}
		markerStyle := drawStyle{FG: markerFG, Bold: active}
		canvas.set(rect.X+rect.W-2, rect.Y+rect.H-1, drawCell{Content: "v", Width: 1, Style: markerStyle})
	}
}

const (
	borderConnUp = 1 << iota
	borderConnDown
	borderConnLeft
	borderConnRight
)

var borderGlyphConnections = map[string]uint8{
	"│": borderConnUp | borderConnDown,
	"─": borderConnLeft | borderConnRight,
	"┌": borderConnDown | borderConnRight,
	"┐": borderConnDown | borderConnLeft,
	"└": borderConnUp | borderConnRight,
	"┘": borderConnUp | borderConnLeft,
	"├": borderConnUp | borderConnDown | borderConnRight,
	"┤": borderConnUp | borderConnDown | borderConnLeft,
	"┬": borderConnDown | borderConnLeft | borderConnRight,
	"┴": borderConnUp | borderConnLeft | borderConnRight,
	"┼": borderConnUp | borderConnDown | borderConnLeft | borderConnRight,
}

var borderConnectionGlyph = map[uint8]string{
	borderConnUp | borderConnDown:                                    "│",
	borderConnLeft | borderConnRight:                                 "─",
	borderConnDown | borderConnRight:                                 "┌",
	borderConnDown | borderConnLeft:                                  "┐",
	borderConnUp | borderConnRight:                                   "└",
	borderConnUp | borderConnLeft:                                    "┘",
	borderConnUp | borderConnDown | borderConnRight:                  "├",
	borderConnUp | borderConnDown | borderConnLeft:                   "┤",
	borderConnDown | borderConnLeft | borderConnRight:                "┬",
	borderConnUp | borderConnLeft | borderConnRight:                  "┴",
	borderConnUp | borderConnDown | borderConnLeft | borderConnRight: "┼",
}

func drawHorizontalBorder(canvas *composedCanvas, startX, endX, y int, style drawStyle, sharedStart bool, downAtEnd bool, upAtEnd bool) {
	if canvas == nil || startX > endX {
		return
	}
	for x := startX; x <= endX; x++ {
		connections := uint8(0)
		if x == startX {
			if sharedStart {
				connections |= borderConnRight
			} else if upAtEnd {
				connections |= borderConnRight | borderConnUp
			} else {
				connections |= borderConnRight | borderConnDown
			}
		} else if x == endX {
			if upAtEnd {
				connections |= borderConnLeft | borderConnUp
			} else if downAtEnd {
				connections |= borderConnLeft | borderConnDown
			} else {
				connections |= borderConnLeft
			}
		} else {
			connections |= borderConnLeft | borderConnRight
		}
		mergeBorderCell(canvas, x, y, connections, style)
	}
}

func drawVerticalBorder(canvas *composedCanvas, x, startY, endY int, style drawStyle, sharedStart bool) {
	if canvas == nil || startY > endY {
		return
	}
	for y := startY; y <= endY; y++ {
		connections := uint8(0)
		if y == startY {
			if sharedStart {
				connections |= borderConnDown
			} else {
				connections |= borderConnUp | borderConnDown
			}
		} else {
			connections |= borderConnUp | borderConnDown
		}
		mergeBorderCell(canvas, x, y, connections, style)
	}
}

func verticalBorderStart(y int, sharedTop bool) int {
	if sharedTop {
		return y - 1
	}
	return y + 1
}

func mergeBorderCell(canvas *composedCanvas, x, y int, connections uint8, style drawStyle) {
	if canvas == nil || x < 0 || y < 0 || x >= canvas.width || y >= canvas.height {
		return
	}
	if existing, ok := borderGlyphConnections[canvas.cells[y][x].Content]; ok {
		connections |= existing
	}
	glyph, ok := borderConnectionGlyph[connections]
	if !ok {
		return
	}
	canvas.set(x, y, drawCell{Content: glyph, Width: 1, Style: style})
}

// drawPaneContent fills the interior of a pane with terminal snapshot content.
func drawPaneContent(canvas *composedCanvas, rect workbench.Rect, pane workbench.VisiblePane, lookup runtimeLookup, scrollOffset int, active bool) {
	if rect.W < 3 || rect.H < 3 {
		return
	}
	contentRect := contentRectForPaneEdges(rect, pane.SharedLeft, pane.SharedTop)
	// Clear the full framed interior, not just the terminal content rect. The
	// reserved right gutter intentionally sits outside contentRect so that pane
	// borders stay visually stable; if we only clear contentRect, stale border
	// glyphs can survive in that gutter and reappear as duplicate right edges.
	fillRect(canvas, interiorRectForPaneEdges(rect, pane.SharedLeft, pane.SharedTop), blankDrawCell())

	if pane.TerminalID == "" {
		drawEmptyPaneContent(canvas, contentRect, pane.ID, pane.TerminalID, defaultUITheme(), -1)
		return
	}

	terminal := lookup.terminal(pane.TerminalID)
	if terminal == nil {
		drawEmptyPaneContent(canvas, contentRect, pane.ID, pane.TerminalID, defaultUITheme(), -1)
		return
	}
	source := renderSource(terminal.Snapshot, terminal.Surface)
	if source == nil || source.ScreenRows() == 0 {
		canvas.drawText(contentRect.X, contentRect.Y, terminal.Name+" ["+terminal.State+"]", drawStyle{FG: defaultUITheme().panelMuted})
		if terminal.State == "exited" {
			drawExitedPaneRecoveryHints(canvas, contentRect, defaultUITheme(), -1, true)
		}
		return
	}
	drawTerminalSourceWithOffset(canvas, contentRect, source, scrollOffset, defaultUITheme())
	if active {
		projectPaneCursorSource(canvas, contentRect, source, scrollOffset)
	}
	if terminal.State == "exited" {
		drawExitedPaneRecoveryHints(canvas, contentRect, defaultUITheme(), -1, true)
	}
}

func drawPaneContentWithKey(canvas *composedCanvas, rect workbench.Rect, entry paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) {
	contentRect := contentRectForEntry(entry)
	// Keep the cached redraw path on the same invariant as drawPaneContent():
	// every content repaint owns the whole framed interior, including the
	// reserved gutter column.
	fillRect(canvas, interiorRectForEntry(entry), blankDrawCell())
	if entry.TerminalID == "" {
		drawEmptyPaneContent(canvas, contentRect, entry.PaneID, entry.TerminalID, entry.Theme, entry.EmptyActionSelected)
		return
	}
	terminal := findVisibleTerminal(runtimeState, entry.TerminalID)
	if terminal == nil {
		drawEmptyPaneContent(canvas, contentRect, entry.PaneID, entry.TerminalID, entry.Theme, -1)
		return
	}
	snapshot := entry.Snapshot
	surface := entry.Surface
	if snapshot == nil && surface == nil {
		surface = terminal.Surface
	}
	if snapshot == nil && surface == nil {
		snapshot = terminal.Snapshot
	}
	source := renderSource(snapshot, surface)
	if source == nil || source.ScreenRows() == 0 {
		canvas.drawText(contentRect.X, contentRect.Y, terminal.Name+" ["+terminal.State+"]", drawStyle{FG: entry.Theme.panelMuted})
		if terminal.State == "exited" {
			drawExitedPaneRecoveryHints(canvas, contentRect, entry.Theme, entry.ExitedActionSelected, entry.ExitedActionPulse)
		}
		return
	}
	renderOffset := entry.ScrollOffset
	if entry.CopyModeActive {
		renderOffset = scrollOffsetForViewportTop(snapshot, contentRect.H, entry.CopyModeViewTopRow)
	}
	drawTerminalSourceWithOffset(canvas, contentRect, source, renderOffset, entry.Theme)
	if entry.CopyModeActive {
		drawCopyModeOverlay(canvas, contentRect, snapshot, entry.Theme, entry.CopyModeCursorRow, entry.CopyModeCursorCol, entry.CopyModeViewTopRow, entry.CopyModeMarkSet, entry.CopyModeMarkRow, entry.CopyModeMarkCol)
	}
	if terminal.State == "exited" {
		drawExitedPaneRecoveryHints(canvas, contentRect, entry.Theme, entry.ExitedActionSelected, entry.ExitedActionPulse)
	}
}

func contentRectForPane(rect workbench.Rect) workbench.Rect {
	content, _ := workbench.FramedPaneContentRect(rect, false, false)
	return content
}

func interiorRectForPane(rect workbench.Rect) workbench.Rect {
	return interiorRectForPaneEdges(rect, false, false)
}

func interiorRectForPaneEdges(rect workbench.Rect, sharedLeft, sharedTop bool) workbench.Rect {
	_ = sharedLeft
	_ = sharedTop
	interior := workbench.Rect{X: rect.X + 1, Y: rect.Y + 1, W: rect.W - 2, H: rect.H - 2}
	return interior
}

func contentRectForPaneEdges(rect workbench.Rect, sharedLeft, sharedTop bool) workbench.Rect {
	content, _ := workbench.FramedPaneContentRect(rect, sharedLeft, sharedTop)
	return content
}

func interiorRectForEntry(entry paneRenderEntry) workbench.Rect {
	if entry.Frameless {
		return entry.Rect
	}
	return interiorRectForPaneEdges(entry.Rect, entry.SharedLeft, entry.SharedTop)
}

func contentRectForEntry(entry paneRenderEntry) workbench.Rect {
	if entry.Frameless {
		return entry.Rect
	}
	return contentRectForPaneEdges(entry.Rect, entry.SharedLeft, entry.SharedTop)
}

func localContentRectForEntry(entry paneRenderEntry) workbench.Rect {
	interior := interiorRectForEntry(entry)
	content := contentRectForEntry(entry)
	return workbench.Rect{
		X: content.X - interior.X,
		Y: content.Y - interior.Y,
		W: content.W,
		H: content.H,
	}
}

func immersiveZoomActive(state VisibleRenderState) bool {
	if state.Surface.Kind != VisibleSurfaceWorkbench || state.Workbench == nil {
		return false
	}
	activeTab := state.Workbench.ActiveTab
	if activeTab < 0 || activeTab >= len(state.Workbench.Tabs) {
		return false
	}
	return strings.TrimSpace(state.Workbench.Tabs[activeTab].ZoomedPaneID) != ""
}

func fillRect(canvas *composedCanvas, rect workbench.Rect, cell drawCell) {
	if canvas == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	rect, ok := clipRectToViewport(rect, canvas.width, canvas.height)
	if !ok {
		return
	}
	if cell == blankDrawCell() {
		blankRow := cachedBlankFillRow(rect.W)
		for y := rect.Y; y < rect.Y+rect.H; y++ {
			clearBlankFillBoundaryFootprint(canvas, rect.X, y)
			if rect.W > 1 {
				clearBlankFillBoundaryFootprint(canvas, rect.X+rect.W-1, y)
			}
			copy(canvas.cells[y][rect.X:rect.X+rect.W], blankRow)
			canvas.rowDirty[y] = true
			canvas.fullDirty = true
		}
		return
	}
	for y := rect.Y; y < rect.Y+rect.H; y++ {
		for x := rect.X; x < rect.X+rect.W; x++ {
			canvas.set(x, y, cell)
		}
	}
}

func clearBlankFillBoundaryFootprint(canvas *composedCanvas, x, y int) {
	if canvas == nil || x < 0 || y < 0 || x >= canvas.width || y >= canvas.height {
		return
	}
	cell := canvas.cells[y][x]
	// When a blank fill starts or ends in the middle of a wide-cell footprint,
	// clear the entire footprint first so we do not leave a stale lead or
	// continuation cell straddling the fill boundary.
	if !cell.Continuation && canvas.cellFootprintWidth(x, y) <= 1 {
		return
	}
	canvas.clearOverlappingCellFootprints(x, y, 1)
}

func projectPaneCursor(canvas *composedCanvas, rect workbench.Rect, snapshot *protocol.Snapshot, scrollOffset int) {
	projectPaneCursorSource(canvas, rect, renderSource(snapshot, nil), scrollOffset)
}

func projectPaneCursorSource(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, scrollOffset int) {
	if canvas == nil || source == nil || !source.Cursor().Visible || scrollOffset > 0 {
		return
	}
	cursor := source.Cursor()
	x := rect.X + cursor.Col
	y := rect.Y + cursor.Row
	if x < rect.X || y < rect.Y || x >= rect.X+rect.W || y >= rect.Y+rect.H {
		return
	}
	drawSyntheticCursor(canvas, x, y, cursor)
}

func drawSyntheticCursor(canvas *composedCanvas, x, y int, cursor protocol.CursorState) {
	if canvas == nil || y < 0 || y >= canvas.height || x < 0 || x >= canvas.width {
		return
	}
	if canvas.syntheticCursorVisibleFn != nil && !canvas.syntheticCursorVisibleFn(cursor) {
		return
	}
	leadX := x
	for leadX > 0 && canvas.cells[y][leadX].Continuation {
		leadX--
	}
	cell := canvas.cells[y][leadX]
	if cell.Continuation {
		cell = blankDrawCell()
	}
	if cell.Content == "" {
		cell = blankDrawCell()
	}
	style := cell.Style
	style.Reverse = false
	style.FG = "#000000"
	style.BG = "#ffffff"
	switch cursor.Shape {
	case "underline":
		style.Underline = true
	case "bar":
		style.Bold = true
	}
	cell.Style = style
	canvas.set(leadX, y, cell)
}

func drawEmptyPaneContent(canvas *composedCanvas, rect workbench.Rect, paneID, terminalID string, theme uiTheme, selectedIndex int) {
	if canvas == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	actions := layoutEmptyPaneActions(rect, paneID)
	if len(actions) == 0 {
		return
	}

	headline := "No terminal attached"
	if strings.TrimSpace(terminalID) != "" {
		headline = "Terminal unavailable"
	}
	firstActionY := actions[0].rowRect.Y
	headlineY := firstActionY - 1
	if headlineY >= rect.Y {
		headlineStyle := drawStyle{FG: theme.panelText}
		canvas.drawText(rect.X, headlineY, centerText(xansi.Truncate(headline, rect.W, ""), rect.W), headlineStyle)
	}

	for index, item := range actions {
		style := emptyPaneActionDrawStyle(theme, item.spec.Kind, index == selectedIndex)
		lineText := centerText(xansi.Truncate(wrapEmptyPaneActionLabel(item.spec, index == selectedIndex), rect.W, ""), rect.W)
		canvas.drawText(item.rowRect.X, item.rowRect.Y, lineText, style)
	}

	if strings.TrimSpace(terminalID) != "" {
		lastActionY := actions[len(actions)-1].rowRect.Y
		terminalLineY := lastActionY + 1
		if terminalLineY < rect.Y+rect.H {
			line := centerText(xansi.Truncate("terminal="+terminalID, rect.W, ""), rect.W)
			canvas.drawText(rect.X, terminalLineY, line, drawStyle{FG: theme.panelMuted})
		}
	}
}

func drawExitedPaneRecoveryHints(canvas *composedCanvas, rect workbench.Rect, theme uiTheme, selectedIndex int, pulse bool) {
	if canvas == nil || rect.W <= 0 || rect.H < 2 {
		return
	}
	actions := layoutExitedPaneRecoveryActions(rect, "pane")
	if len(actions) == 0 {
		return
	}
	if rect.H >= len(actions)+1 {
		headlineY := actions[0].rowRect.Y - 1
		headline := centerText(xansi.Truncate("last output", rect.W, ""), rect.W)
		canvas.drawText(rect.X, headlineY, headline, drawStyle{FG: theme.panelText, Bold: true})
	}
	for index, item := range actions {
		style := exitedPaneActionDrawStyle(theme, item.spec.Kind, index == selectedIndex)
		text := centerText(xansi.Truncate(wrapExitedPaneActionLabel(item.spec, index == selectedIndex, pulse), rect.W, ""), rect.W)
		canvas.drawText(item.rowRect.X, item.rowRect.Y, text, style)
	}
}

func exitedPaneActionDrawStyle(theme uiTheme, kind HitRegionKind, selected bool) drawStyle {
	accent := theme.panelText
	switch kind {
	case HitRegionExitedPaneRestart:
		accent = theme.success
	case HitRegionExitedPaneChoose:
		accent = theme.chromeAccent
	}
	if selected {
		return drawStyle{FG: ensureContrast(mixHex(accent, theme.panelText, 0.15), theme.hostBG, 4.0), Bold: true}
	}
	return drawStyle{FG: ensureContrast(accent, theme.hostBG, 3.8), Bold: true}
}

func emptyPaneActionDrawStyle(theme uiTheme, kind HitRegionKind, selected bool) drawStyle {
	accent := theme.panelText
	switch kind {
	case HitRegionEmptyPaneAttach:
		accent = theme.chromeAccent
	case HitRegionEmptyPaneCreate:
		accent = theme.success
	case HitRegionEmptyPaneManager:
		accent = theme.panelText
	case HitRegionEmptyPaneClose:
		accent = theme.danger
	}
	if selected {
		return drawStyle{FG: ensureContrast(mixHex(accent, theme.panelText, 0.2), theme.hostBG, 4.0), Bold: true}
	}
	return drawStyle{FG: ensureContrast(accent, theme.hostBG, 3.8), Bold: kind != HitRegionEmptyPaneManager}
}

type paneChromeDrawStyles struct {
	Title         drawStyle
	Meta          drawStyle
	State         drawStyle
	Action        drawStyle
	EmphasizeRole bool
}

func drawPaneTopBorderLabels(canvas *composedCanvas, rect workbench.Rect, styles paneChromeDrawStyles, title string, border paneBorderInfo, floating bool) {
	layout, ok := paneTopBorderLabelsLayout(rect, title, border, paneChromeActionTokensForFrame(rect, title, border, floating))
	if canvas == nil || !ok {
		return
	}
	for _, slot := range layout.actionSlots {
		drawBorderLabel(canvas, slot.X, rect.Y, slot.Label, styles.Action)
	}
	if layout.titleLabel != "" {
		drawBorderLabel(canvas, layout.titleX, rect.Y, layout.titleLabel, styles.Title)
	}
	if layout.stateLabel != "" {
		drawBorderLabel(canvas, layout.stateX, rect.Y, layout.stateLabel, styles.State)
	}
	if layout.shareLabel != "" {
		drawBorderLabel(canvas, layout.shareX, rect.Y, layout.shareLabel, styles.Meta)
	}
	if layout.roleLabel != "" {
		roleStyle := styles.Meta
		if styles.EmphasizeRole {
			roleStyle = styles.Action
		}
		drawBorderLabel(canvas, layout.roleX, rect.Y, layout.roleLabel, roleStyle)
	}
	if layout.copyTimeLabel != "" {
		drawBorderLabel(canvas, layout.copyTimeX, rect.Y, layout.copyTimeLabel, styles.Meta)
	}
	if layout.copyRowLabel != "" {
		drawBorderLabel(canvas, layout.copyRowX, rect.Y, layout.copyRowLabel, styles.Meta)
	}
}

type paneBorderLabelsLayout struct {
	actionSlots   []paneChromeActionSlot
	titleX        int
	titleLabel    string
	stateX        int
	stateLabel    string
	shareX        int
	shareLabel    string
	roleX         int
	roleLabel     string
	copyTimeX     int
	copyTimeLabel string
	copyRowX      int
	copyRowLabel  string
}

type paneBorderSlot struct {
	label string
	kind  string
}

func paneTopBorderLabelsLayout(rect workbench.Rect, title string, border paneBorderInfo, actionTokens []paneChromeActionToken) (paneBorderLabelsLayout, bool) {
	if rect.W <= 4 {
		return paneBorderLabelsLayout{}, false
	}
	innerX := rect.X + 2
	innerW := rect.W - 4
	if innerW <= 0 {
		return paneBorderLabelsLayout{}, false
	}

	fullTitleLabel := normalizePaneBorderLabel(title)
	allSlots := make([]paneBorderSlot, 0, 5)
	if label := padPaneBorderSlot(border.StateLabel, paneBorderStateSlotWidth); label != "" {
		allSlots = append(allSlots, paneBorderSlot{kind: "state", label: label})
	}
	if label := padPaneBorderSlot(border.ShareLabel, paneBorderShareSlotWidth); label != "" {
		allSlots = append(allSlots, paneBorderSlot{kind: "share", label: label})
	}
	if label := paneBorderRoleSlot(border.RoleLabel); label != "" {
		allSlots = append(allSlots, paneBorderSlot{kind: "role", label: label})
	}
	if label := normalizePaneBorderLabel(border.CopyTimeLabel); label != "" {
		allSlots = append(allSlots, paneBorderSlot{kind: "copy-time", label: label})
	}
	if label := normalizePaneBorderLabel(border.CopyRowLabel); label != "" {
		allSlots = append(allSlots, paneBorderSlot{kind: "copy-row", label: label})
	}
	titleFullWidth := xansi.StringWidth(fullTitleLabel)
	active := paneBorderSlotsForWidth(allSlots, maxInt(0, innerW-titleFullWidth))
	if fullTitleLabel == "" && len(active) == 0 {
		return paneBorderLabelsLayout{}, false
	}

	actionCount := len(actionTokens)
	reservedStatuses := paneBorderSlotsWidth(active)
	preferActionCluster := len(actionTokens) > 0 && actionTokens[0].Kind == HitRegionPaneCenterFloating
	for {
		reservedRight := reservedStatuses + visiblePaneChromeActionClusterWidth(actionTokens, actionCount, preferActionCluster)
		titleBudget := innerW - reservedRight
		titleFits := titleFullWidth <= titleBudget
		if preferActionCluster {
			titleFits = titleBudget >= 1
		}
		if titleFits || (fullTitleLabel == "" && reservedRight <= innerW) {
			break
		}
		if actionCount > 0 {
			actionCount--
			continue
		}
		removeIdx := paneBorderSlotRemovalIndex(active)
		if removeIdx >= 0 {
			active = append(active[:removeIdx], active[removeIdx+1:]...)
			reservedStatuses = paneBorderSlotsWidth(active)
			continue
		}
		break
	}
	titleLabel := xansi.Truncate(fullTitleLabel, maxInt(0, innerW-reservedStatuses-visiblePaneChromeActionClusterWidth(actionTokens, actionCount, preferActionCluster)), "")
	if titleLabel == "" && len(active) == 0 && actionCount == 0 {
		return paneBorderLabelsLayout{}, false
	}

	layout := paneBorderLabelsLayout{
		actionSlots: make([]paneChromeActionSlot, actionCount),
		titleX:      innerX,
		titleLabel:  titleLabel,
	}
	visibleActionTokens := visiblePaneChromeActionTokens(actionTokens, actionCount, preferActionCluster)
	right := innerX + innerW
	actionXs := make([]int, actionCount)
	for i := actionCount - 1; i >= 0; i-- {
		labelW := xansi.StringWidth(visibleActionTokens[i].Label)
		right -= labelW
		actionXs[i] = right
		if i > 0 {
			right -= paneChromeActionGap
		}
	}
	if len(active) > 0 && actionCount > 0 {
		right--
	}
	for i := len(active) - 1; i >= 0; i-- {
		slot := active[i]
		slotW := xansi.StringWidth(slot.label)
		x := right - slotW
		switch slot.kind {
		case "state":
			layout.stateX = x
			layout.stateLabel = slot.label
		case "share":
			layout.shareX = x
			layout.shareLabel = slot.label
		case "role":
			layout.roleX = x
			layout.roleLabel = slot.label
		case "copy-time":
			layout.copyTimeX = x
			layout.copyTimeLabel = slot.label
		case "copy-row":
			layout.copyRowX = x
			layout.copyRowLabel = slot.label
		}
		right = x - 1
	}
	for i := 0; i < actionCount; i++ {
		token := visibleActionTokens[i]
		layout.actionSlots[i] = paneChromeActionSlot{
			Kind:  token.Kind,
			Label: token.Label,
			X:     actionXs[i],
		}
	}
	return layout, true
}

func visiblePaneChromeActionTokens(tokens []paneChromeActionToken, count int, preferSuffix bool) []paneChromeActionToken {
	if count <= 0 || len(tokens) == 0 {
		return nil
	}
	if count >= len(tokens) {
		return tokens
	}
	if preferSuffix {
		return tokens[len(tokens)-count:]
	}
	return tokens[:count]
}

func visiblePaneChromeActionClusterWidth(tokens []paneChromeActionToken, count int, preferSuffix bool) int {
	return paneChromeActionClusterWidth(visiblePaneChromeActionTokens(tokens, count, preferSuffix), count)
}

func paneBorderSlotsForWidth(slots []paneBorderSlot, width int) []paneBorderSlot {
	if len(slots) == 0 || width <= 0 {
		return nil
	}
	active := append([]paneBorderSlot(nil), slots...)
	for paneBorderSlotsWidth(active) > width {
		removeIdx := paneBorderSlotRemovalIndex(active)
		if removeIdx < 0 {
			break
		}
		active = append(active[:removeIdx], active[removeIdx+1:]...)
	}
	if paneBorderSlotsWidth(active) > width {
		return nil
	}
	return active
}

func paneBorderSlotRemovalIndex(slots []paneBorderSlot) int {
	for _, kind := range []string{"share", "state", "role", "copy-time", "copy-row"} {
		for i := len(slots) - 1; i >= 0; i-- {
			if slots[i].kind == kind {
				return i
			}
		}
	}
	return -1
}

func PaneOwnerButtonRect(pane workbench.VisiblePane, runtimeState *VisibleRuntimeStateProxy, confirmPaneID string) (workbench.Rect, bool) {
	lookup := newRuntimeLookup(runtimeState)
	title := resolvePaneTitleWithLookup(pane, lookup)
	border := paneBorderInfoWithLookup(pane, lookup, confirmPaneID)
	layout, ok := paneTopBorderLabelsLayout(
		pane.Rect,
		title,
		border,
		paneChromeActionTokensForPane(pane, title, border),
	)
	if !ok || layout.roleLabel == "" {
		return workbench.Rect{}, false
	}
	actionLabel := paneOwnerActionLabel(pane, lookup, confirmPaneID)
	if actionLabel == "" {
		return workbench.Rect{}, false
	}
	return workbench.Rect{
		X: layout.roleX,
		Y: pane.Rect.Y,
		W: xansi.StringWidth(layout.roleLabel),
		H: 1,
	}, true
}

func paneOwnerActionLabel(pane workbench.VisiblePane, lookup runtimeLookup, confirmPaneID string) string {
	if pane.TerminalID == "" || lookup.paneRole(pane.ID) != "follower" {
		return ""
	}
	if confirmPaneID == pane.ID {
		return ownerConfirmLabel
	}
	return "follow"
}

func normalizePaneBorderLabel(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return " " + text + " "
}

func padPaneBorderSlot(text string, width int) string {
	if strings.TrimSpace(text) == "" || width <= 0 {
		return ""
	}
	text = xansi.Truncate(strings.TrimSpace(text), width, "")
	pad := maxInt(0, width-xansi.StringWidth(text))
	left := pad / 2
	right := pad - left
	return strings.Repeat(" ", left) + text + strings.Repeat(" ", right)
}

func paneBorderRoleSlot(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return padPaneBorderSlot(text, paneBorderRoleSlotWidth)
}

func paneBorderSlotsWidth(slots []paneBorderSlot) int {
	total := 0
	for i, slot := range slots {
		total += xansi.StringWidth(slot.label)
		if i > 0 {
			total++
		}
	}
	return total
}

func drawBorderLabel(canvas *composedCanvas, x, y int, text string, style drawStyle) {
	if canvas == nil || strings.TrimSpace(text) == "" {
		return
	}
	canvas.drawText(x, y, text, style)
}

func copyModeTimestampLabel(snapshot *protocol.Snapshot, row int) string {
	ts := snapshotRowTimestamp(snapshot, row)
	if ts.IsZero() {
		return ""
	}
	return formatSnapshotRowTimestamp(ts)
}

func copyModeRowPositionLabel(snapshot *protocol.Snapshot, row int) string {
	totalRows := snapshotTotalRows(snapshot)
	if totalRows <= 0 || row < 0 || row >= totalRows {
		return ""
	}
	return strconv.Itoa(row+1) + "/" + strconv.Itoa(totalRows)
}

func formatSnapshotRowTimestamp(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.Local().Format("2006-01-02 15:04:05")
}

func snapshotRowTimestamp(snapshot *protocol.Snapshot, row int) time.Time {
	if snapshot == nil || row < 0 {
		return time.Time{}
	}
	if row < len(snapshot.Scrollback) {
		if row < len(snapshot.ScrollbackTimestamps) {
			return snapshot.ScrollbackTimestamps[row]
		}
		return time.Time{}
	}
	row -= len(snapshot.Scrollback)
	if row < 0 || row >= len(snapshot.Screen.Cells) {
		return time.Time{}
	}
	if row < len(snapshot.ScreenTimestamps) {
		return snapshot.ScreenTimestamps[row]
	}
	return time.Time{}
}

func snapshotRowKind(snapshot *protocol.Snapshot, row int) string {
	if snapshot == nil || row < 0 {
		return ""
	}
	if row < len(snapshot.Scrollback) {
		if row < len(snapshot.ScrollbackRowKinds) {
			return snapshot.ScrollbackRowKinds[row]
		}
		return ""
	}
	row -= len(snapshot.Scrollback)
	if row < 0 || row >= len(snapshot.Screen.Cells) {
		return ""
	}
	if row < len(snapshot.ScreenRowKinds) {
		return snapshot.ScreenRowKinds[row]
	}
	return ""
}

func snapshotTotalRows(snapshot *protocol.Snapshot) int {
	if snapshot == nil {
		return 0
	}
	return len(snapshot.Scrollback) + len(snapshot.Screen.Cells)
}

func applyScrollbackOffset(snapshot *protocol.Snapshot, offset int, height int) *protocol.Snapshot {
	if snapshot == nil || offset <= 0 || height <= 0 {
		return snapshot
	}
	rows := make([][]protocol.Cell, 0, len(snapshot.Scrollback)+len(snapshot.Screen.Cells))
	rows = append(rows, snapshot.Scrollback...)
	rows = append(rows, snapshot.Screen.Cells...)
	if len(rows) == 0 {
		return snapshot
	}
	end := len(rows) - offset
	if end < 0 {
		end = 0
	}
	start := end - height
	if start < 0 {
		start = 0
	}
	window := rows[start:end]
	cells := make([][]protocol.Cell, 0, len(window))
	for _, row := range window {
		cells = append(cells, append([]protocol.Cell(nil), row...))
	}
	cloned := *snapshot
	cloned.Screen = protocol.ScreenData{
		Cells:             cells,
		IsAlternateScreen: snapshot.Screen.IsAlternateScreen,
	}
	return &cloned
}

func drawSnapshotWithOffset(canvas *composedCanvas, rect workbench.Rect, snapshot *protocol.Snapshot, offset int, theme uiTheme) {
	drawTerminalSourceWithOffset(canvas, rect, renderSource(snapshot, nil), offset, theme)
}

func drawTerminalSourceWithOffset(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, offset int, theme uiTheme) {
	if canvas == nil || source == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	if offset <= 0 {
		drawTerminalSourceInRect(canvas, rect, source)
		drawTerminalExtentHints(canvas, rect, source, theme)
		return
	}
	totalRows := source.TotalRows()
	if totalRows == 0 {
		drawTerminalExtentHints(canvas, rect, source, theme)
		return
	}
	end := totalRows - offset
	if end < 0 {
		end = 0
	}
	start := end - rect.H
	if start < 0 {
		start = 0
	}
	targetY := rect.Y
	for rowIndex := start; rowIndex < end && targetY < rect.Y+rect.H; rowIndex++ {
		drawTerminalSourceRowInRect(canvas, rect, source, rowIndex, targetY, theme)
		targetY++
	}
	drawTerminalExtentHints(canvas, rect, terminalExtentHintsView(source, totalRows), theme)
}

func drawSnapshotRowInRect(canvas *composedCanvas, rect workbench.Rect, snapshot *protocol.Snapshot, rowIndex int, targetY int, theme uiTheme) {
	drawTerminalSourceRowInRect(canvas, rect, renderSource(snapshot, nil), rowIndex, targetY, theme)
}

func drawTerminalSourceInRect(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource) {
	if canvas == nil || source == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	base := source.ScrollbackRows()
	for y := 0; y < rect.H && y < source.ScreenRows(); y++ {
		canvas.drawProtocolRowInRect(rect, rect.Y+y, source.Row(base+y))
	}
}

func drawTerminalSourceRowInRect(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, rowIndex int, targetY int, theme uiTheme) {
	if source == nil {
		return
	}
	if kind := source.RowKind(rowIndex); kind != "" {
		if drawSnapshotMarkerRow(canvas, rect, targetY, kind, source.RowTimestamp(rowIndex), theme) {
			return
		}
	}
	canvas.drawProtocolRowInRect(rect, targetY, source.Row(rowIndex))
}

func drawSnapshotMarkerRow(canvas *composedCanvas, rect workbench.Rect, targetY int, kind string, ts time.Time, theme uiTheme) bool {
	if canvas == nil || rect.W <= 0 {
		return false
	}
	label := snapshotMarkerLabel(kind, ts)
	if strings.TrimSpace(label) == "" {
		return false
	}
	canvas.drawText(rect.X, targetY, centerText(label, rect.W), drawStyle{FG: theme.panelMuted})
	return true
}

func snapshotMarkerLabel(kind string, ts time.Time) string {
	switch kind {
	case protocol.SnapshotRowKindRestart:
		label := "[ restarted ]"
		if formatted := formatSnapshotRowTimestamp(ts); formatted != "" {
			label = "[ restarted " + formatted + " ]"
		}
		return label
	default:
		return ""
	}
}

func snapshotExtentHintsView(snapshot *protocol.Snapshot, rows int) *protocol.Snapshot {
	if snapshot == nil || rows <= 0 {
		return snapshot
	}
	if int(snapshot.Size.Rows) >= rows {
		return snapshot
	}
	cloned := *snapshot
	if rows > int(^uint16(0)) {
		rows = int(^uint16(0))
	}
	cloned.Size.Rows = uint16(rows)
	return &cloned
}

func terminalExtentHintsView(source terminalRenderSource, rows int) terminalRenderSource {
	if source == nil || rows <= 0 {
		return source
	}
	if size := source.Size(); int(size.Rows) >= rows {
		return source
	}
	switch typed := source.(type) {
	case snapshotRenderSource:
		return renderSource(snapshotExtentHintsView(typed.snapshot, rows), nil)
	default:
		return source
	}
}

func drawSnapshotExtentHints(canvas *composedCanvas, rect workbench.Rect, snapshot *protocol.Snapshot, theme uiTheme) {
	drawTerminalExtentHints(canvas, rect, renderSource(snapshot, nil), theme)
}

func drawTerminalExtentHints(canvas *composedCanvas, rect workbench.Rect, source terminalRenderSource, theme uiTheme) {
	if canvas == nil || source == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	metrics := terminalMetricsForSource(source)
	if metrics.Cols <= 0 || metrics.Rows <= 0 {
		return
	}

	dotStyle := drawStyle{FG: theme.panelBorder}

	visibleCols := minInt(rect.W, metrics.Cols)
	visibleRows := minInt(rect.H, metrics.Rows)

	if metrics.Cols < rect.W {
		startX := rect.X + visibleCols
		endX := rect.X + rect.W
		for y := rect.Y; y < rect.Y+visibleRows; y++ {
			for x := startX; x < endX; x++ {
				canvas.set(x, y, drawCell{Content: "·", Width: 1, Style: dotStyle})
			}
		}
	}
	if metrics.Rows < rect.H {
		startY := rect.Y + visibleRows
		endY := rect.Y + rect.H
		for y := startY; y < endY; y++ {
			for x := rect.X; x < rect.X+rect.W; x++ {
				canvas.set(x, y, drawCell{Content: "·", Width: 1, Style: dotStyle})
			}
		}
	}
}

func renderTerminalMetricsForSnapshot(snapshot *protocol.Snapshot) renderTerminalMetrics {
	return terminalMetricsForSource(renderSource(snapshot, nil))
}

func drawCopyModeOverlay(canvas *composedCanvas, rect workbench.Rect, snapshot *protocol.Snapshot, theme uiTheme, cursorRow, cursorCol, viewTopRow int, markSet bool, markRow, markCol int) {
	if canvas == nil || snapshot == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	totalRows := len(snapshot.Scrollback) + len(snapshot.Screen.Cells)
	if totalRows <= 0 {
		return
	}
	cursorRow, cursorCol = clampCopyPoint(snapshot, cursorRow, cursorCol)
	selectionStartRow, selectionStartCol := markRow, markCol
	selectionEndRow, selectionEndCol := cursorRow, cursorCol
	if markSet {
		selectionStartRow, selectionStartCol = clampCopyPoint(snapshot, selectionStartRow, selectionStartCol)
		selectionEndRow, selectionEndCol = clampCopyPoint(snapshot, selectionEndRow, selectionEndCol)
		if selectionStartRow > selectionEndRow || (selectionStartRow == selectionEndRow && selectionStartCol > selectionEndCol) {
			selectionStartRow, selectionEndRow = selectionEndRow, selectionStartRow
			selectionStartCol, selectionEndCol = selectionEndCol, selectionStartCol
		}
	}
	start := clampCopyViewportTop(snapshot, rect.H, viewTopRow)
	selectionBG := ensureContrast(mixHex(theme.info, theme.chromeAccent, 0.35), theme.hostBG, 1.2)
	cursorBG := ensureContrast(theme.warning, theme.hostBG, 1.2)
	for visibleRow := 0; visibleRow < rect.H; visibleRow++ {
		rowIndex := start + visibleRow
		if rowIndex < 0 || rowIndex >= totalRows {
			continue
		}
		if markSet && rowIndex >= selectionStartRow && rowIndex <= selectionEndRow {
			firstCol := 0
			lastCol := rowMaxCol(snapshot, rowIndex)
			if rowIndex == selectionStartRow {
				firstCol = selectionStartCol
			}
			if rowIndex == selectionEndRow {
				lastCol = selectionEndCol
			}
			for col := firstCol; col <= lastCol; col++ {
				drawCopyModeCellHighlight(canvas, rect.X+col, rect.Y+visibleRow, selectionBG)
			}
		}
	}
	screenRow := cursorRow - start
	if screenRow >= 0 && screenRow < rect.H {
		drawCopyModeCellHighlight(canvas, rect.X+cursorCol, rect.Y+screenRow, cursorBG)
	}
}

func drawCopyModeCellHighlight(canvas *composedCanvas, x, y int, bg string) {
	if canvas == nil || x < 0 || y < 0 || x >= canvas.width || y >= canvas.height {
		return
	}
	leadX := x
	for leadX > 0 && canvas.cells[y][leadX].Continuation {
		leadX--
	}
	cell := canvas.cells[y][leadX]
	if cell.Continuation {
		cell = blankDrawCell()
	}
	if cell.Content == "" {
		cell = blankDrawCell()
	}
	style := cell.Style
	style.BG = bg
	style.FG = contrastTextColor(bg)
	cell.Style = style
	canvas.set(leadX, y, cell)
}

func clampCopyViewportTop(snapshot *protocol.Snapshot, height, viewTopRow int) int {
	totalRows := len(snapshot.Scrollback) + len(snapshot.Screen.Cells)
	if totalRows <= 0 {
		return 0
	}
	maxTop := maxInt(0, totalRows-maxInt(1, height))
	if viewTopRow < 0 {
		viewTopRow = 0
	}
	if viewTopRow > maxTop {
		viewTopRow = maxTop
	}
	return viewTopRow
}

func scrollOffsetForViewportTop(snapshot *protocol.Snapshot, height, viewTopRow int) int {
	if snapshot == nil {
		return 0
	}
	totalRows := len(snapshot.Scrollback) + len(snapshot.Screen.Cells)
	viewTopRow = clampCopyViewportTop(snapshot, height, viewTopRow)
	offset := totalRows - (viewTopRow + maxInt(1, height))
	if offset < 0 {
		offset = 0
	}
	if viewTopRow < len(snapshot.Scrollback) && offset == 0 && len(snapshot.Scrollback) > 0 {
		offset = 1
	}
	return offset
}

func snapshotRow(snapshot *protocol.Snapshot, rowIndex int) []protocol.Cell {
	if snapshot == nil || rowIndex < 0 {
		return nil
	}
	if rowIndex < len(snapshot.Scrollback) {
		return snapshot.Scrollback[rowIndex]
	}
	rowIndex -= len(snapshot.Scrollback)
	if rowIndex < 0 || rowIndex >= len(snapshot.Screen.Cells) {
		return nil
	}
	return snapshot.Screen.Cells[rowIndex]
}

func rowMaxCol(snapshot *protocol.Snapshot, rowIndex int) int {
	row := snapshotRow(snapshot, rowIndex)
	if len(row) > 0 {
		return len(row) - 1
	}
	if snapshot == nil || snapshot.Size.Cols == 0 {
		return 0
	}
	return int(snapshot.Size.Cols) - 1
}

func clampCopyPoint(snapshot *protocol.Snapshot, row, col int) (int, int) {
	totalRows := len(snapshot.Scrollback) + len(snapshot.Screen.Cells)
	if totalRows <= 0 {
		return 0, 0
	}
	if row < 0 {
		row = 0
	}
	if row >= totalRows {
		row = totalRows - 1
	}
	maxCol := rowMaxCol(snapshot, row)
	if col < 0 {
		col = 0
	}
	if col > maxCol {
		col = maxCol
	}
	rowCells := snapshotRow(snapshot, row)
	for col > 0 && col < len(rowCells) && rowCells[col].Content == "" && rowCells[col].Width == 0 {
		col--
	}
	return row, col
}

func projectActiveEntryCursor(canvas *composedCanvas, entries []paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) {
	if canvas == nil {
		return
	}
	canvas.clearCursor()
	canvas.syntheticCursorBlink = false
	target, ok := activeEntryCursorRenderTarget(entries, runtimeState)
	if !ok {
		return
	}
	// 中文说明：活动 pane 采用“双光标”策略：
	// 1. 宿主光标始终 hidden + positioned，用来给 IME/preedit 提供正确锚点；
	// 2. pane 内真正可见的光标由合成画布里的 synthetic cursor 承担，避免宿主侧
	//    的整行高亮/预编辑背景越过 pane 边界。
	canvas.setHiddenCursor(target.X, target.Y, target.Shape, target.Blink)
	if !target.Visible {
		return
	}
	drawSyntheticCursor(canvas, target.X, target.Y, protocol.CursorState{
		Visible: true,
		Shape:   target.Shape,
		Blink:   target.Blink,
	})
}

type cursorProjectionTarget struct {
	X     int
	Y     int
	Shape string
	Blink bool
}

type cursorRenderTarget struct {
	cursorProjectionTarget
	Visible bool
}

func activeEntryCursorRenderTarget(entries []paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) (cursorRenderTarget, bool) {
	for i, entry := range entries {
		if !entry.Active {
			continue
		}
		terminal := findVisibleTerminal(runtimeState, entry.TerminalID)
		snapshot := entry.Snapshot
		surface := entry.Surface
		if snapshot == nil && surface == nil && terminal != nil {
			surface = terminal.Surface
		}
		if snapshot == nil && surface == nil && terminal != nil {
			snapshot = terminal.Snapshot
		}
		source := renderSource(snapshot, surface)
		if source == nil {
			return cursorRenderTarget{}, false
		}
		rect := contentRectForEntry(entry)
		if entry.CopyModeActive || entry.ScrollOffset > 0 {
			return cursorRenderTarget{}, false
		}
		target, ok := entryCursorRenderTarget(rect, source)
		if !ok {
			return cursorRenderTarget{}, false
		}
		if activeCursorOccluded(entries, i, target.cursorProjectionTarget) {
			return cursorRenderTarget{}, false
		}
		return target, true
	}
	return cursorRenderTarget{}, false
}

func activeEntryCursorTarget(entries []paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) (cursorProjectionTarget, bool) {
	target, ok := activeEntryCursorRenderTarget(entries, runtimeState)
	if !ok {
		return cursorProjectionTarget{}, false
	}
	return target.cursorProjectionTarget, true
}

func entryCursorRenderTarget(rect workbench.Rect, source terminalRenderSource) (cursorRenderTarget, bool) {
	snapshotTarget, snapshotOK := renderSourceCursorProjectionTarget(rect, source)
	fallbackTarget, fallbackOK := visualCursorProjectionTargetForSource(rect, source)
	cursor := protocol.CursorState{}
	if source != nil {
		cursor = source.Cursor()
	}
	switch {
	case snapshotOK && shouldPreferVisualCursorTargetForSource(source, snapshotTarget, fallbackTarget, fallbackOK):
		return cursorRenderTarget{
			cursorProjectionTarget: fallbackTarget,
			Visible:                cursor.Visible,
		}, true
	case snapshotOK:
		return cursorRenderTarget{
			cursorProjectionTarget: snapshotTarget,
			Visible:                cursor.Visible,
		}, true
	case fallbackOK:
		return cursorRenderTarget{
			cursorProjectionTarget: fallbackTarget,
			Visible:                cursor.Visible,
		}, true
	default:
		return cursorRenderTarget{}, false
	}
}

func snapshotCursorProjectionTarget(rect workbench.Rect, snapshot *protocol.Snapshot) (cursorProjectionTarget, bool) {
	if snapshot == nil {
		return cursorProjectionTarget{}, false
	}
	cursorX := rect.X + snapshot.Cursor.Col
	cursorY := rect.Y + snapshot.Cursor.Row
	if cursorX < rect.X || cursorY < rect.Y || cursorX >= rect.X+rect.W || cursorY >= rect.Y+rect.H {
		return cursorProjectionTarget{}, false
	}
	return cursorProjectionTarget{
		X:     cursorX,
		Y:     cursorY,
		Shape: snapshot.Cursor.Shape,
		Blink: snapshot.Cursor.Blink,
	}, true
}

func shouldPreferVisualCursorTarget(snapshot *protocol.Snapshot, snapshotTarget, visualTarget cursorProjectionTarget, visualOK bool) bool {
	if snapshot == nil || !visualOK {
		return false
	}
	if !snapshot.Cursor.Visible {
		return true
	}
	if !snapshotLikelyOwnsVisualCursor(snapshot) {
		return false
	}
	// 中文说明：Claude/Cloud Code 这类全屏 TUI 可能把真实终端 cursor 留在顶部，
	// 再在底部输入区自己画一个块光标。这里仅在“真实 cursor 还停在顶部，而视觉
	// 光标明确出现在更下方”时切换，避免误伤普通终端程序。
	return snapshot.Cursor.Row <= 1 && visualTarget.Y >= snapshotTarget.Y+2
}

func visualCursorProjectionTarget(rect workbench.Rect, snapshot *protocol.Snapshot) (cursorProjectionTarget, bool) {
	if snapshot == nil || !snapshotLikelyOwnsVisualCursor(snapshot) {
		return cursorProjectionTarget{}, false
	}
	rows := snapshot.Screen.Cells
	if len(rows) == 0 {
		return cursorProjectionTarget{}, false
	}
	startRow := maxInt(0, len(rows)/2)
	for row := len(rows) - 1; row >= startRow; row-- {
		cells := rows[row]
		for col := 0; col < len(cells) && col < rect.W; col++ {
			if !cellLooksLikeVisualCursor(cells, col) {
				continue
			}
			return cursorProjectionTarget{
				X:     rect.X + col,
				Y:     rect.Y + row,
				Shape: "block",
				Blink: false,
			}, true
		}
	}
	return cursorProjectionTarget{}, false
}

func snapshotLikelyOwnsVisualCursor(snapshot *protocol.Snapshot) bool {
	if snapshot == nil {
		return false
	}
	return snapshot.Screen.IsAlternateScreen ||
		snapshot.Modes.AlternateScreen ||
		snapshot.Modes.MouseTracking ||
		snapshot.Modes.BracketedPaste
}

func cellLooksLikeVisualCursor(row []protocol.Cell, col int) bool {
	if col < 0 || col >= len(row) {
		return false
	}
	cell := row[col]
	if cell.Content == "" && cell.Width == 0 {
		return false
	}
	if !styleLooksLikeVisualCursor(cell.Style) {
		return false
	}
	run := styledCellRunLength(row, col)
	return run >= 1 && run <= 2
}

func styleLooksLikeVisualCursor(style protocol.CellStyle) bool {
	if style.Reverse {
		return true
	}
	return (style.FG == "#000000" && style.BG == "#ffffff") ||
		(style.FG == "#ffffff" && style.BG == "#000000")
}

func styledCellRunLength(row []protocol.Cell, col int) int {
	if col < 0 || col >= len(row) {
		return 0
	}
	style := row[col].Style
	run := 1
	for i := col - 1; i >= 0 && sameCellStyle(row[i].Style, style); i-- {
		run++
	}
	for i := col + 1; i < len(row) && sameCellStyle(row[i].Style, style); i++ {
		run++
	}
	return run
}

func sameCellStyle(a, b protocol.CellStyle) bool {
	return a.FG == b.FG &&
		a.BG == b.BG &&
		a.Bold == b.Bold &&
		a.Italic == b.Italic &&
		a.Underline == b.Underline &&
		a.Blink == b.Blink &&
		a.Reverse == b.Reverse &&
		a.Strikethrough == b.Strikethrough
}

func activeCursorOccluded(entries []paneRenderEntry, activeIdx int, target cursorProjectionTarget) bool {
	if activeIdx < 0 || activeIdx >= len(entries) {
		return false
	}
	for i := activeIdx + 1; i < len(entries); i++ {
		entryRect := entries[i].Rect
		if target.X >= entryRect.X && target.X < entryRect.X+entryRect.W &&
			target.Y >= entryRect.Y && target.Y < entryRect.Y+entryRect.H {
			return true
		}
	}
	return false
}

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
