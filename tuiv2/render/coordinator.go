package render

import (
	"strconv"
	"strings"
	"sync"
	"time"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/modal"
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
	tabBarKey   tabBarCacheKey
	tabBarValue string
	statusKey   statusBarCacheKey
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
	Workbench                *workbench.VisibleWorkbench
	Runtime                  *VisibleRuntimeStateProxy
	SurfaceKind              VisibleSurfaceKind
	SurfaceTerminalPool      *modal.TerminalManagerState
	OverlayKind              VisibleOverlayKind
	OverlayPrompt            *modal.PromptState
	OverlayPicker            *modal.PickerState
	OverlayWorkspacePicker   *modal.WorkspacePickerState
	OverlayTerminalManager   *modal.TerminalManagerState
	OverlayHelp              *modal.HelpState
	OverlayFloatingOverview  *modal.FloatingOverviewState
	TermSize                 TermSize
	Notice                   string
	Error                    string
	InputMode                string
	OwnerConfirmPaneID       string
	EmptyPaneSelectionPaneID string
	EmptyPaneSelectionIndex  int
}

type tabBarCacheKey struct {
	Workbench *workbench.VisibleWorkbench
	Width     int
	Error     string
	Notice    string
}

type statusBarCacheKey struct {
	Workbench *workbench.VisibleWorkbench
	Runtime   *VisibleRuntimeStateProxy
	Width     int
	InputMode string
}

type paneRenderEntry struct {
	PaneID              string
	Rect                workbench.Rect
	Title               string
	Border              paneBorderInfo
	Theme               uiTheme
	Overflow            paneOverflowHints
	ContentKey          paneContentKey
	FrameKey            paneFrameKey
	TerminalID          string
	ScrollOffset        int
	Active              bool
	Floating            bool
	EmptyActionSelected int
}

type paneFrameKey struct {
	Rect            workbench.Rect
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

type paneContentKey struct {
	TerminalID          string
	Snapshot            *protocol.Snapshot
	Name                string
	State               string
	ThemeBG             string
	TerminalKnown       bool
	ScrollOffset        int
	EmptyActionSelected int
}

type bodyRenderCache struct {
	width       int
	height      int
	order       []string
	rects       map[string]workbench.Rect
	frameKeys   map[string]paneFrameKey
	contentKeys map[string]paneContentKey
	canvas      *composedCanvas
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

func (c *Coordinator) Schedule()     {}
func (c *Coordinator) FlushPending() {}
func (c *Coordinator) StartTicker()  {}

func (c *Coordinator) RenderFrame() string {
	if c == nil || c.visibleFn == nil {
		return ""
	}
	state := c.visibleFn()
	key := stateKey(state)
	c.mu.Lock()
	if !c.dirty && c.lastFrame != "" && c.lastState == key {
		frame := c.lastFrame
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
		frame := c.lastFrame
		c.mu.Unlock()
		return frame
	}

	tabBar := c.renderTabBarCached(state)
	statusBar := c.renderStatusBarCached(state)
	bodyHeight := FrameBodyHeight(state.TermSize.Height)
	rendered := renderBodyFrameWithCoordinator(c, state, state.TermSize.Width, bodyHeight)
	body := rendered.content
	cursor := rendered.cursor

	overlaySize := TermSize{Width: state.TermSize.Width, Height: bodyHeight}
	if overlay := renderActiveOverlay(state, overlaySize); overlay != "" {
		body = compositeOverlay(body, overlay, TermSize{Width: state.TermSize.Width, Height: bodyHeight})
		cursor = hideCursorANSI()
		rendered.blink = false
	}
	frame := strings.Join([]string{tabBar, body, statusBar}, "\n")
	c.mu.Lock()
	if !rendered.blink {
		c.cursorBlinkVisible = true
	}
	c.lastFrame = frame + cursor
	c.lastCursor = cursor
	c.lastState = key
	c.dirty = false
	frame = c.lastFrame
	c.mu.Unlock()
	return frame
}

func stateKey(state VisibleRenderState) renderStateKey {
	return renderStateKey{
		Workbench:                state.Workbench,
		Runtime:                  state.Runtime,
		SurfaceKind:              state.Surface.Kind,
		SurfaceTerminalPool:      state.Surface.TerminalPool,
		OverlayKind:              state.Overlay.Kind,
		OverlayPrompt:            state.Overlay.Prompt,
		OverlayPicker:            state.Overlay.Picker,
		OverlayWorkspacePicker:   state.Overlay.WorkspacePicker,
		OverlayTerminalManager:   state.Overlay.TerminalManager,
		OverlayHelp:              state.Overlay.Help,
		OverlayFloatingOverview:  state.Overlay.FloatingOverview,
		TermSize:                 state.TermSize,
		Notice:                   state.Notice,
		Error:                    state.Error,
		InputMode:                state.InputMode,
		OwnerConfirmPaneID:       state.OwnerConfirmPaneID,
		EmptyPaneSelectionPaneID: state.EmptyPaneSelectionPaneID,
		EmptyPaneSelectionIndex:  state.EmptyPaneSelectionIndex,
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

func (c *Coordinator) syntheticCursorVisible(cursor protocol.CursorState) bool {
	if c == nil {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cursorBlinkVisible
}

func (c *Coordinator) renderTabBarCached(state VisibleRenderState) string {
	key := tabBarCacheKey{
		Workbench: state.Workbench,
		Width:     state.TermSize.Width,
		Error:     state.Error,
		Notice:    state.Notice,
	}
	c.mu.Lock()
	if c.tabBarKey == key && c.tabBarValue != "" {
		value := c.tabBarValue
		c.mu.Unlock()
		return value
	}
	c.mu.Unlock()
	value := renderTabBar(state)
	c.mu.Lock()
	c.tabBarKey = key
	c.tabBarValue = value
	c.mu.Unlock()
	return value
}

func (c *Coordinator) renderStatusBarCached(state VisibleRenderState) string {
	key := statusBarCacheKey{
		Workbench: state.Workbench,
		Runtime:   state.Runtime,
		Width:     state.TermSize.Width,
		InputMode: state.InputMode,
	}
	c.mu.Lock()
	if c.statusKey == key && c.statusValue != "" {
		value := c.statusValue
		c.mu.Unlock()
		return value
	}
	c.mu.Unlock()
	value := renderStatusBar(state)
	c.mu.Lock()
	c.statusKey = key
	c.statusValue = value
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
	if state.Surface.Kind == VisibleSurfaceTerminalPool && state.Surface.TerminalPool != nil {
		return renderedBody{
			content: renderTerminalPoolPage(state.Surface.TerminalPool, state.Runtime, TermSize{Width: width, Height: height}),
		}
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
	entries := paneEntriesForTab(tab, state.Workbench.FloatingPanes, width, height, lookup, state.OwnerConfirmPaneID, state.EmptyPaneSelectionPaneID, state.EmptyPaneSelectionIndex, uiThemeForRuntime(state.Runtime))

	canvas := renderBodyCanvas(coordinator, state, entries, width, height)
	return renderedBody{
		content: canvas.contentString(),
		cursor:  canvas.cursorANSI(),
		blink:   canvas.syntheticCursorBlink,
	}
}

func visibleStateNeedsCursorBlink(state VisibleRenderState) bool {
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
	if width <= 0 || height <= 0 {
		return false
	}
	entries := paneEntriesForTab(tab, state.Workbench.FloatingPanes, width, height, newRuntimeLookup(state.Runtime), state.OwnerConfirmPaneID, state.EmptyPaneSelectionPaneID, state.EmptyPaneSelectionIndex, uiThemeForRuntime(state.Runtime))
	_, _, ok := activeEntryCursorTarget(entries, state.Runtime)
	return ok
}

type emptyWorkbenchKind uint8

const (
	emptyWorkbenchNoTabs emptyWorkbenchKind = iota
	emptyWorkbenchNoPanes
)

func renderEmptyWorkbenchBody(state VisibleRenderState, width, height int, kind emptyWorkbenchKind) renderedBody {
	canvas := newComposedCanvas(width, height)
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

func renderActiveOverlay(state VisibleRenderState, termSize TermSize) string {
	theme := uiThemeForState(state)
	switch state.Overlay.Kind {
	case VisibleOverlayPrompt:
		return renderPromptOverlayWithTheme(state.Overlay.Prompt, termSize, theme)
	case VisibleOverlayPicker:
		return renderPickerOverlayWithTheme(state.Overlay.Picker, termSize, theme)
	case VisibleOverlayWorkspacePicker:
		return renderWorkspacePickerOverlayWithTheme(state.Overlay.WorkspacePicker, termSize, theme)
	case VisibleOverlayTerminalManager:
		return renderTerminalManagerOverlayWithTheme(state.Overlay.TerminalManager, termSize, theme)
	case VisibleOverlayHelp:
		return renderHelpOverlayWithTheme(state.Overlay.Help, termSize, theme)
	case VisibleOverlayFloatingOverview:
		return renderFloatingOverviewOverlayWithTheme(state.Overlay.FloatingOverview, termSize, theme)
	default:
		return ""
	}
}

func renderBodyCanvas(coordinator *Coordinator, state VisibleRenderState, entries []paneRenderEntry, width, height int) *composedCanvas {
	if coordinator == nil {
		canvas := newComposedCanvas(width, height)
		canvas.cursorOffsetY = TopChromeRows
		for _, entry := range entries {
			drawPaneFrame(canvas, entry.Rect, entry.Title, entry.Border, entry.Theme, entry.Overflow, entry.Active, entry.Floating)
			drawPaneContentWithKey(canvas, entry.Rect, entry, state.Runtime)
		}
		return canvas
	}
	cache := coordinator.bodyCache
	if cache == nil || !cache.matches(entries, width, height) {
		canvas := newComposedCanvas(width, height)
		canvas.cursorOffsetY = TopChromeRows
		canvas.syntheticCursorVisibleFn = coordinator.syntheticCursorVisible
		for _, entry := range entries {
			drawPaneFrame(canvas, entry.Rect, entry.Title, entry.Border, entry.Theme, entry.Overflow, entry.Active, entry.Floating)
			drawPaneContentWithKey(canvas, entry.Rect, entry, state.Runtime)
		}
		coordinator.bodyCache = newBodyRenderCache(canvas, entries, width, height)
		return canvas
	}

	// Overlapping panes need a full rebuild. The cached active-pane refresh path
	// redraws the active pane content to clear the old cursor, which is correct
	// for tiled layouts but will paint over floating panes layered above it.
		if entriesOverlap(entries) {
			canvas := newComposedCanvas(width, height)
			canvas.cursorOffsetY = TopChromeRows
			canvas.syntheticCursorVisibleFn = coordinator.syntheticCursorVisible
			for _, entry := range entries {
			drawPaneFrame(canvas, entry.Rect, entry.Title, entry.Border, entry.Theme, entry.Overflow, entry.Active, entry.Floating)
			drawPaneContentWithKey(canvas, entry.Rect, entry, state.Runtime)
		}
		projectActiveEntryCursor(canvas, entries, state.Runtime)
		cache.canvas = canvas
		cache.reset(entries, width, height)
		return canvas
	}

	if !entriesOverlap(entries) {
		changed := false
		cache.canvas.cursorVisible = false
		for _, entry := range entries {
			if cache.frameKeys[entry.PaneID] != entry.FrameKey {
				drawPaneFrame(cache.canvas, entry.Rect, entry.Title, entry.Border, entry.Theme, entry.Overflow, entry.Active, entry.Floating)
				changed = true
			}
			if cache.contentKeys[entry.PaneID] != entry.ContentKey {
				fillRect(cache.canvas, contentRectForPane(entry.Rect), blankDrawCell())
				drawPaneContentWithKey(cache.canvas, entry.Rect, entry, state.Runtime)
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

func paneEntriesForTab(tab workbench.VisibleTab, floating []workbench.VisiblePane, width, height int, lookup runtimeLookup, confirmPaneID, emptyPaneSelectionPaneID string, emptyPaneSelectionIndex int, theme uiTheme) []paneRenderEntry {
	entries := make([]paneRenderEntry, 0, len(tab.Panes)+len(floating))
	zoomedPaneID := tab.ZoomedPaneID
	for _, pane := range tab.Panes {
		rect := pane.Rect
		if zoomedPaneID != "" {
			if pane.ID != zoomedPaneID {
				continue
			}
			rect = workbench.Rect{X: 0, Y: 0, W: width, H: height}
		}
		rect, ok := clipRectToViewport(rect, width, height)
		if !ok {
			continue
		}
		entries = append(entries, buildPaneRenderEntry(pane, rect, tab.ActivePaneID, tab.ScrollOffset, lookup, confirmPaneID, emptyPaneSelectionPaneID, emptyPaneSelectionIndex, theme))
	}
	for _, pane := range floating {
		rect, ok := clipRectToViewport(pane.Rect, width, height)
		if !ok {
			continue
		}
		entries = append(entries, buildPaneRenderEntry(pane, rect, tab.ActivePaneID, tab.ScrollOffset, lookup, confirmPaneID, emptyPaneSelectionPaneID, emptyPaneSelectionIndex, theme))
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

func buildPaneRenderEntry(pane workbench.VisiblePane, rect workbench.Rect, activePaneID string, scrollOffset int, lookup runtimeLookup, confirmPaneID, emptyPaneSelectionPaneID string, emptyPaneSelectionIndex int, theme uiTheme) paneRenderEntry {
	active := pane.ID == activePaneID
	title := resolvePaneTitleWithLookup(pane, lookup)
	border := paneBorderInfoWithLookup(pane, lookup, confirmPaneID)
	terminal := lookup.terminal(pane.TerminalID)
	overflow := paneOverflowHints{}
	emptyActionSelected := -1
	if pane.TerminalID == "" && pane.ID == emptyPaneSelectionPaneID {
		emptyActionSelected = emptyPaneSelectionIndex
	}
	contentKey := paneContentKey{
		TerminalID:          pane.TerminalID,
		ThemeBG:             theme.panelBG,
		TerminalKnown:       terminal != nil,
		ScrollOffset:        scrollOffset,
		EmptyActionSelected: emptyActionSelected,
	}
	if terminal != nil {
		contentKey.Snapshot = terminal.Snapshot
		contentKey.Name = terminal.Name
		contentKey.State = terminal.State
		overflow = snapshotOverflowHints(terminal.Snapshot, contentRectForPane(rect))
	}
	return paneRenderEntry{
		PaneID:     pane.ID,
		Rect:       rect,
		Title:      title,
		Border:     border,
		Theme:      theme,
		Overflow:   overflow,
		ContentKey: contentKey,
		FrameKey: paneFrameKey{
			Rect:            rect,
			Title:           title,
			Border:          border,
			ThemeBG:         theme.panelBG,
			Overflow:        overflow,
			Active:          active,
			Floating:        pane.Floating,
			ChromeSignature: paneChromeActionSignatureForFrame(rect, title, border, pane.Floating),
		},
		TerminalID:          pane.TerminalID,
		ScrollOffset:        scrollOffset,
		Active:              active,
		Floating:            pane.Floating,
		EmptyActionSelected: emptyActionSelected,
	}
}

func snapshotOverflowHints(snapshot *protocol.Snapshot, rect workbench.Rect) paneOverflowHints {
	if snapshot == nil || rect.W <= 0 || rect.H <= 0 {
		return paneOverflowHints{}
	}
	termW := int(snapshot.Size.Cols)
	termH := int(snapshot.Size.Rows)
	if termW <= 0 || termH <= 0 {
		return paneOverflowHints{}
	}
	return paneOverflowHints{
		Right:  termW > rect.W,
		Bottom: termH > rect.H,
	}
}

func newBodyRenderCache(canvas *composedCanvas, entries []paneRenderEntry, width, height int) *bodyRenderCache {
	cache := &bodyRenderCache{canvas: canvas}
	cache.reset(entries, width, height)
	return cache
}

func (c *bodyRenderCache) reset(entries []paneRenderEntry, width, height int) {
	if c == nil {
		return
	}
	c.width = width
	c.height = height
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
	for _, entry := range entries {
		c.order = append(c.order, entry.PaneID)
		c.rects[entry.PaneID] = entry.Rect
		c.frameKeys[entry.PaneID] = entry.FrameKey
		c.contentKeys[entry.PaneID] = entry.ContentKey
	}
}

func (c *bodyRenderCache) matches(entries []paneRenderEntry, width, height int) bool {
	if c == nil || c.canvas == nil || c.width != width || c.height != height || len(c.order) != len(entries) {
		return false
	}
	for i, entry := range entries {
		if c.order[i] != entry.PaneID || c.rects[entry.PaneID] != entry.Rect {
			return false
		}
	}
	return true
}

func renderTerminalPoolPage(pool *modal.TerminalManagerState, runtimeState *VisibleRuntimeStateProxy, termSize TermSize) string {
	if pool == nil {
		return ""
	}
	theme := uiThemeForRuntime(runtimeState)
	width := maxInt(1, termSize.Width)
	height := maxInt(1, termSize.Height)
	layout := buildTerminalPoolPageLayout(pool, width, height)
	innerWidth := layout.innerWidth
	headerLines := make([]string, 0, 3)

	title := terminalPickerTitleStyle(theme).Width(width).Render(forceWidthANSIOverlay(coalesce(strings.TrimSpace(pool.Title), "Terminal Pool"), width))
	headerLines = append(headerLines, title)
	headerLines = append(headerLines, forceWidthANSIOverlay(renderOverlaySearchLine(theme, pool.Query, width), width))
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
	return renderPageWithPinnedFooter(headerLines, contentLines, footerLine, width, height)
}

func renderTerminalPoolDetails(item *modal.PickerItem, runtimeState *VisibleRuntimeStateProxy, innerWidth int) []string {
	if item == nil {
		return nil
	}
	lookup := newRuntimeLookup(runtimeState)
	lines := []string{forceWidthANSIOverlay("PREVIEW", innerWidth)}
	if terminal := lookup.terminal(item.TerminalID); terminal != nil {
		lines = append(lines, terminalPoolPreviewLines(terminal.Snapshot, innerWidth, 4)...)
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

func terminalPoolPreviewLines(snapshot *protocol.Snapshot, innerWidth int, maxLines int) []string {
	if snapshot == nil || maxLines <= 0 {
		return []string{forceWidthANSIOverlay("(no live preview)", innerWidth)}
	}
	rows := snapshot.Screen.Cells
	if len(rows) == 0 {
		return []string{forceWidthANSIOverlay("(no live preview)", innerWidth)}
	}
	lines := make([]string, 0, minInt(len(rows), maxLines))
	for _, row := range rows {
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
func drawPaneFrame(canvas *composedCanvas, rect workbench.Rect, title string, border paneBorderInfo, theme uiTheme, overflow paneOverflowHints, active bool, floating bool) {
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
	topEdge := "─"
	leftEdge := "│"
	bottomEdge := "─"
	rightEdge := "│"

	// horizontal edges
	for x := rect.X; x < rect.X+rect.W; x++ {
		canvas.set(x, rect.Y, drawCell{Content: topEdge, Width: 1, Style: borderStyle})
		canvas.set(x, rect.Y+rect.H-1, drawCell{Content: bottomEdge, Width: 1, Style: borderStyle})
	}
	// vertical edges
	for y := rect.Y; y < rect.Y+rect.H; y++ {
		canvas.set(rect.X, y, drawCell{Content: leftEdge, Width: 1, Style: borderStyle})
		canvas.set(rect.X+rect.W-1, y, drawCell{Content: rightEdge, Width: 1, Style: borderStyle})
	}
	// corners
	canvas.set(rect.X, rect.Y, drawCell{Content: "┌", Width: 1, Style: borderStyle})
	canvas.set(rect.X+rect.W-1, rect.Y, drawCell{Content: "┐", Width: 1, Style: borderStyle})
	canvas.set(rect.X, rect.Y+rect.H-1, drawCell{Content: "└", Width: 1, Style: borderStyle})
	canvas.set(rect.X+rect.W-1, rect.Y+rect.H-1, drawCell{Content: "┘", Width: 1, Style: borderStyle})

	drawPaneOverflowMarkers(canvas, rect, theme, overflow, active)
	drawPaneTopBorderLabels(canvas, rect, chromeStyles, title, border, floating)
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

// drawPaneContent fills the interior of a pane with terminal snapshot content.
func drawPaneContent(canvas *composedCanvas, rect workbench.Rect, pane workbench.VisiblePane, lookup runtimeLookup, scrollOffset int, active bool) {
	if rect.W < 3 || rect.H < 3 {
		return
	}
	contentRect := workbench.Rect{X: rect.X + 1, Y: rect.Y + 1, W: rect.W - 2, H: rect.H - 2}
	fillRect(canvas, contentRect, blankDrawCell())

	if pane.TerminalID == "" {
		drawEmptyPaneContent(canvas, contentRect, pane.ID, pane.TerminalID, defaultUITheme(), -1)
		return
	}

	terminal := lookup.terminal(pane.TerminalID)
	if terminal == nil {
		drawEmptyPaneContent(canvas, contentRect, pane.ID, pane.TerminalID, defaultUITheme(), -1)
		return
	}
	if terminal.Snapshot == nil || len(terminal.Snapshot.Screen.Cells) == 0 {
		canvas.drawText(contentRect.X, contentRect.Y, terminal.Name+" ["+terminal.State+"]", drawStyle{FG: defaultUITheme().panelMuted})
		return
	}
	drawSnapshotWithOffset(canvas, contentRect, terminal.Snapshot, scrollOffset, defaultUITheme())
	if active {
		projectPaneCursor(canvas, contentRect, terminal.Snapshot, scrollOffset)
	}
}

func drawPaneContentWithKey(canvas *composedCanvas, rect workbench.Rect, entry paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) {
	contentRect := contentRectForPane(rect)
	fillRect(canvas, contentRect, blankDrawCell())
	if entry.TerminalID == "" {
		drawEmptyPaneContent(canvas, contentRect, entry.PaneID, entry.TerminalID, entry.Theme, entry.EmptyActionSelected)
		return
	}
	terminal := findVisibleTerminal(runtimeState, entry.TerminalID)
	if terminal == nil {
		drawEmptyPaneContent(canvas, contentRect, entry.PaneID, entry.TerminalID, entry.Theme, -1)
		return
	}
	if terminal.Snapshot == nil || len(terminal.Snapshot.Screen.Cells) == 0 {
		canvas.drawText(contentRect.X, contentRect.Y, terminal.Name+" ["+terminal.State+"]", drawStyle{FG: entry.Theme.panelMuted})
		return
	}
	drawSnapshotWithOffset(canvas, contentRect, terminal.Snapshot, entry.ScrollOffset, entry.Theme)
	if entry.Active {
		projectPaneCursor(canvas, contentRect, terminal.Snapshot, entry.ScrollOffset)
	}
}

func contentRectForPane(rect workbench.Rect) workbench.Rect {
	return workbench.Rect{X: rect.X + 1, Y: rect.Y + 1, W: rect.W - 2, H: rect.H - 2}
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

func projectPaneCursor(canvas *composedCanvas, rect workbench.Rect, snapshot *protocol.Snapshot, scrollOffset int) {
	if canvas == nil || snapshot == nil || !snapshot.Cursor.Visible || scrollOffset > 0 {
		return
	}
	x := rect.X + snapshot.Cursor.Col
	y := rect.Y + snapshot.Cursor.Row
	if x < rect.X || y < rect.Y || x >= rect.X+rect.W || y >= rect.Y+rect.H {
		return
	}
	drawSyntheticCursor(canvas, x, y, snapshot.Cursor)
}

func drawSyntheticCursor(canvas *composedCanvas, x, y int, cursor protocol.CursorState) {
	if canvas == nil || y < 0 || y >= canvas.height || x < 0 || x >= canvas.width {
		return
	}
	canvas.syntheticCursorBlink = true
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
}

type paneBorderLabelsLayout struct {
	actionSlots []paneChromeActionSlot
	titleX      int
	titleLabel  string
	stateX      int
	stateLabel  string
	shareX      int
	shareLabel  string
	roleX       int
	roleLabel   string
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
	allSlots := make([]paneBorderSlot, 0, 3)
	if label := padPaneBorderSlot(border.StateLabel, paneBorderStateSlotWidth); label != "" {
		allSlots = append(allSlots, paneBorderSlot{kind: "state", label: label})
	}
	if label := padPaneBorderSlot(border.ShareLabel, paneBorderShareSlotWidth); label != "" {
		allSlots = append(allSlots, paneBorderSlot{kind: "share", label: label})
	}
	if label := paneBorderRoleSlot(border.RoleLabel); label != "" {
		allSlots = append(allSlots, paneBorderSlot{kind: "role", label: label})
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
	for _, kind := range []string{"share", "state", "role"} {
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
	if canvas == nil || snapshot == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	if offset <= 0 {
		canvas.drawSnapshotInRect(rect, snapshot)
		drawSnapshotExtentHints(canvas, rect, snapshot, theme)
		return
	}
	totalRows := len(snapshot.Scrollback) + len(snapshot.Screen.Cells)
	if totalRows == 0 {
		drawSnapshotExtentHints(canvas, rect, snapshot, theme)
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
		var row []protocol.Cell
		if rowIndex < len(snapshot.Scrollback) {
			row = snapshot.Scrollback[rowIndex]
		} else {
			row = snapshot.Screen.Cells[rowIndex-len(snapshot.Scrollback)]
		}
		canvas.drawProtocolRowInRect(rect, targetY, row)
		targetY++
	}
	drawSnapshotExtentHints(canvas, rect, snapshot, theme)
}

func drawSnapshotExtentHints(canvas *composedCanvas, rect workbench.Rect, snapshot *protocol.Snapshot, theme uiTheme) {
	if canvas == nil || snapshot == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	termW, termH := snapshotExtentSize(snapshot)
	if termW <= 0 || termH <= 0 {
		return
	}

	dotStyle := drawStyle{FG: theme.panelBorder}

	visibleCols := minInt(rect.W, termW)
	visibleRows := minInt(rect.H, termH)

	if termW < rect.W {
		startX := rect.X + visibleCols
		endX := rect.X + rect.W
		for y := rect.Y; y < rect.Y+visibleRows; y++ {
			for x := startX; x < endX; x++ {
				canvas.set(x, y, drawCell{Content: "·", Width: 1, Style: dotStyle})
			}
		}
	}
	if termH < rect.H {
		startY := rect.Y + visibleRows
		endY := rect.Y + rect.H
		for y := startY; y < endY; y++ {
			for x := rect.X; x < rect.X+rect.W; x++ {
				canvas.set(x, y, drawCell{Content: "·", Width: 1, Style: dotStyle})
			}
		}
	}
}

func snapshotExtentSize(snapshot *protocol.Snapshot) (int, int) {
	if snapshot == nil {
		return 0, 0
	}
	termW := int(snapshot.Size.Cols)
	termH := int(snapshot.Size.Rows)
	if screenH := len(snapshot.Screen.Cells); screenH > termH {
		termH = screenH
	}
	for _, row := range snapshot.Screen.Cells {
		if rowW := protocolRowDisplayWidth(row); rowW > termW {
			termW = rowW
		}
	}
	return termW, termH
}

func protocolRowDisplayWidth(row []protocol.Cell) int {
	width := 0
	for _, cell := range row {
		switch {
		case cell.Width > 0:
			width += cell.Width
		case cell.Content != "":
			width++
		default:
			width++
		}
	}
	return width
}

func projectActiveEntryCursor(canvas *composedCanvas, entries []paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) {
	if canvas == nil {
		return
	}
	canvas.cursorVisible = false
	canvas.syntheticCursorBlink = false
	rect, snapshot, ok := activeEntryCursorTarget(entries, runtimeState)
	if !ok {
		return
	}
	projectPaneCursor(canvas, rect, snapshot, 0)
}

func activeEntryCursorTarget(entries []paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) (workbench.Rect, *protocol.Snapshot, bool) {
	for i, entry := range entries {
		if !entry.Active {
			continue
		}
		terminal := findVisibleTerminal(runtimeState, entry.TerminalID)
		if terminal == nil || terminal.Snapshot == nil {
			return workbench.Rect{}, nil, false
		}
		rect := contentRectForPane(entry.Rect)
		snapshot := terminal.Snapshot
		if entry.ScrollOffset > 0 || !snapshot.Cursor.Visible || activeCursorOccluded(entries, i, rect, snapshot) {
			return rect, snapshot, false
		}
		cursorX := rect.X + snapshot.Cursor.Col
		cursorY := rect.Y + snapshot.Cursor.Row
		if cursorX < rect.X || cursorY < rect.Y || cursorX >= rect.X+rect.W || cursorY >= rect.Y+rect.H {
			return rect, snapshot, false
		}
		return rect, snapshot, true
	}
	return workbench.Rect{}, nil, false
}

func activeCursorOccluded(entries []paneRenderEntry, activeIdx int, rect workbench.Rect, snapshot *protocol.Snapshot) bool {
	if activeIdx < 0 || activeIdx >= len(entries) || snapshot == nil || !snapshot.Cursor.Visible {
		return false
	}
	cursorX := rect.X + snapshot.Cursor.Col
	cursorY := rect.Y + snapshot.Cursor.Row
	for i := activeIdx + 1; i < len(entries); i++ {
		entryRect := entries[i].Rect
		if cursorX >= entryRect.X && cursorX < entryRect.X+entryRect.W &&
			cursorY >= entryRect.Y && cursorY < entryRect.Y+entryRect.H {
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
