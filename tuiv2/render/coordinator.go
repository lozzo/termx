package render

import (
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
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
	bodyCache   *bodyRenderCache
	tabBarKey   tabBarCacheKey
	tabBarValue string
	statusKey   statusBarCacheKey
	statusValue string
}

type VisibleStateFn func() VisibleRenderState

type renderedBody struct {
	content string
	cursor  string
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
	PaneID       string
	Rect         workbench.Rect
	Title        string
	Meta         string
	ContentKey   paneContentKey
	FrameKey     paneFrameKey
	TerminalID   string
	ScrollOffset int
	Active       bool
}

type paneFrameKey struct {
	Rect   workbench.Rect
	Title  string
	Meta   string
	Active bool
}

type paneContentKey struct {
	TerminalID    string
	Snapshot      *protocol.Snapshot
	Name          string
	State         string
	TerminalKnown bool
	ScrollOffset  int
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
	return &Coordinator{visibleFn: fn, dirty: true}
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
	c.mu.Lock()
	if !c.dirty && c.lastFrame != "" {
		frame := c.lastFrame
		c.mu.Unlock()
		return frame
	}
	c.mu.Unlock()
	state := c.visibleFn()
	if state.Workbench == nil {
		c.mu.Lock()
		c.lastFrame = "tuiv2"
		c.dirty = false
		frame := c.lastFrame
		c.mu.Unlock()
		return frame
	}

	tabBar := c.renderTabBarCached(state)
	statusBar := c.renderStatusBarCached(state)
	bodyHeight := maxInt(1, state.TermSize.Height-2)
	currentCoordinator = c
	rendered := renderBodyFrame(state, state.TermSize.Width, bodyHeight)
	currentCoordinator = nil
	body := rendered.content
	cursor := rendered.cursor

	if overlay := renderPromptOverlay(state.Prompt, TermSize{Width: state.TermSize.Width, Height: bodyHeight}); overlay != "" {
		body = compositeOverlay(body, overlay, TermSize{Width: state.TermSize.Width, Height: bodyHeight})
		cursor = ""
	}
	if overlay := renderPickerOverlay(state.Picker, TermSize{Width: state.TermSize.Width, Height: bodyHeight}); overlay != "" {
		body = compositeOverlay(body, overlay, TermSize{Width: state.TermSize.Width, Height: bodyHeight})
		cursor = ""
	}
	if overlay := renderWorkspacePickerOverlay(state.WorkspacePicker, TermSize{Width: state.TermSize.Width, Height: bodyHeight}); overlay != "" {
		body = compositeOverlay(body, overlay, TermSize{Width: state.TermSize.Width, Height: bodyHeight})
		cursor = ""
	}
	if overlay := renderTerminalManagerOverlay(state.TerminalManager, TermSize{Width: state.TermSize.Width, Height: bodyHeight}); overlay != "" {
		body = compositeOverlay(body, overlay, TermSize{Width: state.TermSize.Width, Height: bodyHeight})
		cursor = ""
	}
	if overlay := renderHelpOverlay(state.Help, TermSize{Width: state.TermSize.Width, Height: bodyHeight}); overlay != "" {
		body = compositeOverlay(body, overlay, TermSize{Width: state.TermSize.Width, Height: bodyHeight})
		cursor = ""
	}
	frame := strings.Join([]string{tabBar, body, statusBar}, "\n") + cursor
	c.mu.Lock()
	c.lastFrame = frame
	c.dirty = false
	c.mu.Unlock()
	return frame
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
	return renderBodyFrame(state, width, height).content
}

func renderBodyFrame(state VisibleRenderState, width, height int) renderedBody {
	if width <= 0 || height <= 0 {
		return renderedBody{}
	}
	if state.TerminalPool != nil {
		return renderedBody{
			content: renderTerminalPoolPage(state.TerminalPool, state.Runtime, TermSize{Width: width, Height: height}),
		}
	}
	if state.Workbench == nil {
		return renderedBody{content: strings.Repeat("\n", maxInt(0, height-1))}
	}

	activeTabIdx := state.Workbench.ActiveTab
	if activeTabIdx < 0 || activeTabIdx >= len(state.Workbench.Tabs) {
		return renderedBody{content: strings.Repeat("\n", maxInt(0, height-1))}
	}
	tab := state.Workbench.Tabs[activeTabIdx]
	lookup := newRuntimeLookup(state.Runtime)
	entries := paneEntriesForTab(tab, state.Workbench.FloatingPanes, width, height, lookup)

	canvas := renderBodyCanvas(state, entries, width, height)
	return renderedBody{
		content: canvas.contentString(),
		cursor:  canvas.cursorANSI(),
	}
}

func renderBodyCanvas(state VisibleRenderState, entries []paneRenderEntry, width, height int) *composedCanvas {
	coordinator := stateCoordinator(state)
	if coordinator == nil {
		canvas := newComposedCanvas(width, height)
		canvas.cursorOffsetY = 1
		for _, entry := range entries {
			drawPaneFrame(canvas, entry.Rect, workbench.VisiblePane{ID: entry.PaneID, TerminalID: entry.TerminalID}, runtimeLookup{}, entry.Title, entry.Meta, entry.Active)
			drawPaneContentWithKey(canvas, entry.Rect, entry, state.Runtime)
		}
		return canvas
	}
	cache := coordinator.bodyCache
	if cache == nil || !cache.matches(entries, width, height) {
		canvas := newComposedCanvas(width, height)
		canvas.cursorOffsetY = 1
		for _, entry := range entries {
			drawPaneFrame(canvas, entry.Rect, workbench.VisiblePane{ID: entry.PaneID, TerminalID: entry.TerminalID}, runtimeLookup{}, entry.Title, entry.Meta, entry.Active)
			drawPaneContentWithKey(canvas, entry.Rect, entry, state.Runtime)
		}
		coordinator.bodyCache = newBodyRenderCache(canvas, entries, width, height)
		return canvas
	}

	if !entriesOverlap(entries) {
		changed := false
		cache.canvas.cursorVisible = false
		for _, entry := range entries {
			if cache.frameKeys[entry.PaneID] != entry.FrameKey {
				drawPaneFrame(cache.canvas, entry.Rect, workbench.VisiblePane{ID: entry.PaneID, TerminalID: entry.TerminalID}, runtimeLookup{}, entry.Title, entry.Meta, entry.Active)
				changed = true
			}
			if cache.contentKeys[entry.PaneID] != entry.ContentKey {
				fillRect(cache.canvas, contentRectForPane(entry.Rect), blankDrawCell())
				drawPaneContentWithKey(cache.canvas, entry.Rect, entry, state.Runtime)
				changed = true
			}
		}
		if changed {
			projectActiveEntryCursor(cache.canvas, entries, state.Runtime)
			cache.reset(entries, width, height)
			return cache.canvas
		}
		projectActiveEntryCursor(cache.canvas, entries, state.Runtime)
		return cache.canvas
	}

	damage := make([]workbench.Rect, 0, len(entries))
	for _, entry := range entries {
		if cache.frameKeys[entry.PaneID] != entry.FrameKey || cache.contentKeys[entry.PaneID] != entry.ContentKey {
			damage = append(damage, entry.Rect)
		}
	}
	if len(damage) == 0 {
		projectActiveEntryCursor(cache.canvas, entries, state.Runtime)
		return cache.canvas
	}

	cache.canvas.cursorVisible = false
	for _, rect := range damage {
		fillRect(cache.canvas, rect, blankDrawCell())
	}
	for _, entry := range entries {
		if !overlapsAnyRect(entry.Rect, damage) {
			continue
		}
		drawPaneFrame(cache.canvas, entry.Rect, workbench.VisiblePane{ID: entry.PaneID, TerminalID: entry.TerminalID}, runtimeLookup{}, entry.Title, entry.Meta, entry.Active)
		drawPaneContentWithKey(cache.canvas, entry.Rect, entry, state.Runtime)
	}
	projectActiveEntryCursor(cache.canvas, entries, state.Runtime)
	cache.reset(entries, width, height)
	return cache.canvas
}

func stateCoordinator(state VisibleRenderState) *Coordinator {
	// renderBodyFrame is only called from Coordinator.RenderFrame on the same goroutine.
	// We stash the active coordinator in the state via a private field substitute:
	// since we don't have that field, use the singleton current coordinator path.
	// This helper exists only to keep cache plumbing local.
	return currentCoordinator
}

var currentCoordinator *Coordinator

func paneEntriesForTab(tab workbench.VisibleTab, floating []workbench.VisiblePane, width, height int, lookup runtimeLookup) []paneRenderEntry {
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
		if rect.W <= 0 || rect.H <= 0 {
			continue
		}
		entries = append(entries, buildPaneRenderEntry(pane, rect, tab.ActivePaneID, tab.ScrollOffset, lookup))
	}
	for _, pane := range floating {
		if pane.Rect.W <= 0 || pane.Rect.H <= 0 {
			continue
		}
		entries = append(entries, buildPaneRenderEntry(pane, pane.Rect, tab.ActivePaneID, tab.ScrollOffset, lookup))
	}
	return entries
}

func buildPaneRenderEntry(pane workbench.VisiblePane, rect workbench.Rect, activePaneID string, scrollOffset int, lookup runtimeLookup) paneRenderEntry {
	active := pane.ID == activePaneID
	title := resolvePaneTitleWithLookup(pane, lookup)
	meta := paneMetaWithLookup(pane, lookup)
	terminal := lookup.terminal(pane.TerminalID)
	contentKey := paneContentKey{
		TerminalID:    pane.TerminalID,
		TerminalKnown: terminal != nil,
		ScrollOffset:  scrollOffset,
	}
	if terminal != nil {
		contentKey.Snapshot = terminal.Snapshot
		contentKey.Name = terminal.Name
		contentKey.State = terminal.State
	}
	return paneRenderEntry{
		PaneID:       pane.ID,
		Rect:         rect,
		Title:        title,
		Meta:         meta,
		ContentKey:   contentKey,
		FrameKey:     paneFrameKey{Rect: rect, Title: title, Meta: meta, Active: active},
		TerminalID:   pane.TerminalID,
		ScrollOffset: scrollOffset,
		Active:       active,
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
	width := maxInt(1, termSize.Width)
	height := maxInt(1, termSize.Height)
	innerWidth := maxInt(24, width-4)
	headerLines := make([]string, 0, 3)

	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#f8fafc")).Render(coalesce(strings.TrimSpace(pool.Title), "Terminal Pool"))
	headerLines = append(headerLines, forceWidthANSIOverlay(title, width))
	headerLines = append(headerLines, forceWidthANSIOverlay(terminalPickerQueryStyle.Render("search: "+pool.Query+"_"), width))
	headerLines = append(headerLines, forceWidthANSIOverlay("", width))

	contentLines := make([]string, 0, height)

	items := pool.VisibleItems()
	lastGroup := ""
	for index := range items {
		if group := strings.ToUpper(strings.TrimSpace(items[index].State)); group != "" && group != lastGroup {
			contentLines = append(contentLines, "  "+forceWidthANSIOverlay(group, innerWidth))
			lastGroup = group
		}
		line := items[index].RenderLine(innerWidth, index == pool.Selected, pickerLineStyle, pickerSelectedLineStyle, pickerCreateRowStyle)
		contentLines = append(contentLines, "  "+forceWidthANSIOverlay(line, innerWidth))
	}
	if detailLines := renderTerminalPoolDetails(pool.SelectedItem(), runtimeState, innerWidth); len(detailLines) > 0 {
		contentLines = append(contentLines, forceWidthANSIOverlay("", width))
		for _, line := range detailLines {
			contentLines = append(contentLines, "  "+forceWidthANSIOverlay(line, innerWidth))
		}
	}

	footer := coalesce(pool.Footer, "[Enter] here  [Ctrl-T] tab  [Ctrl-O] float  [Ctrl-E] edit  [Ctrl-K] kill  [Esc] close")
	footerLine := forceWidthANSIOverlay(pickerFooterStyle.Render(footer), width)
	return renderPageWithPinnedFooter(headerLines, contentLines, footerLine, width, height)
}

func renderTerminalPoolDetails(item *modal.PickerItem, runtimeState *VisibleRuntimeStateProxy, innerWidth int) []string {
	if item == nil {
		return nil
	}
	lookup := newRuntimeLookup(runtimeState)
	lines := []string{
		forceWidthANSIOverlay("PREVIEW", innerWidth),
		forceWidthANSIOverlay("live preview", innerWidth),
	}
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

// resolvePaneTitle returns the title for a pane, preferring terminal OSC 2 title,
// then persisted terminal metadata name, then the pane title.
func resolvePaneTitle(pane workbench.VisiblePane, runtimeState *VisibleRuntimeStateProxy) string {
	return resolvePaneTitleWithLookup(pane, newRuntimeLookup(runtimeState))
}

func resolvePaneTitleWithLookup(pane workbench.VisiblePane, lookup runtimeLookup) string {
	if strings.TrimSpace(pane.TerminalID) == "" {
		return "unconnected"
	}
	if terminal := lookup.terminal(pane.TerminalID); terminal != nil {
		if terminal.Title != "" {
			return terminal.Title
		}
		if terminal.Name != "" {
			return terminal.Name
		}
	}
	return pane.Title
}

// drawPaneFrame draws the border box with a title on the left and compact pane meta on the right.
func drawPaneFrame(canvas *composedCanvas, rect workbench.Rect, pane workbench.VisiblePane, lookup runtimeLookup, title string, meta string, active bool) {
	if rect.W < 2 || rect.H < 2 {
		return
	}
	borderFG := "#d1d5db"
	titleFG := "#e5e7eb"
	if active {
		borderFG = "#4ade80"
		titleFG = "#f0fdf4"
	}
	borderStyle := drawStyle{FG: borderFG}
	titleStyle := drawStyle{FG: titleFG, Bold: true}

	// horizontal edges
	for x := rect.X; x < rect.X+rect.W; x++ {
		canvas.set(x, rect.Y, drawCell{Content: "─", Width: 1, Style: borderStyle})
		canvas.set(x, rect.Y+rect.H-1, drawCell{Content: "─", Width: 1, Style: borderStyle})
	}
	// vertical edges
	for y := rect.Y; y < rect.Y+rect.H; y++ {
		canvas.set(rect.X, y, drawCell{Content: "│", Width: 1, Style: borderStyle})
		canvas.set(rect.X+rect.W-1, y, drawCell{Content: "│", Width: 1, Style: borderStyle})
	}
	// corners
	canvas.set(rect.X, rect.Y, drawCell{Content: "┌", Width: 1, Style: borderStyle})
	canvas.set(rect.X+rect.W-1, rect.Y, drawCell{Content: "┐", Width: 1, Style: borderStyle})
	canvas.set(rect.X, rect.Y+rect.H-1, drawCell{Content: "└", Width: 1, Style: borderStyle})
	canvas.set(rect.X+rect.W-1, rect.Y+rect.H-1, drawCell{Content: "┘", Width: 1, Style: borderStyle})

	if meta == "" {
		meta = paneMetaWithLookup(pane, lookup)
	}
	drawPaneTopBorderLabels(canvas, rect, titleStyle, title, meta)
}

// drawPaneContent fills the interior of a pane with terminal snapshot content.
func drawPaneContent(canvas *composedCanvas, rect workbench.Rect, pane workbench.VisiblePane, lookup runtimeLookup, scrollOffset int, active bool) {
	if rect.W < 3 || rect.H < 3 {
		return
	}
	contentRect := workbench.Rect{X: rect.X + 1, Y: rect.Y + 1, W: rect.W - 2, H: rect.H - 2}
	fillRect(canvas, contentRect, blankDrawCell())

	if pane.TerminalID == "" {
		drawEmptyPaneContent(canvas, contentRect, pane.TerminalID)
		return
	}

	terminal := lookup.terminal(pane.TerminalID)
	if terminal == nil {
		drawEmptyPaneContent(canvas, contentRect, pane.TerminalID)
		return
	}
	if terminal.Snapshot == nil || len(terminal.Snapshot.Screen.Cells) == 0 {
		canvas.drawText(contentRect.X, contentRect.Y, terminal.Name+" ["+terminal.State+"]", drawStyle{FG: "#94a3b8"})
		return
	}
	drawSnapshotWithOffset(canvas, contentRect, terminal.Snapshot, scrollOffset)
	if active {
		projectPaneCursor(canvas, contentRect, terminal.Snapshot)
	}
}

func drawPaneContentWithKey(canvas *composedCanvas, rect workbench.Rect, entry paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) {
	contentRect := contentRectForPane(rect)
	fillRect(canvas, contentRect, blankDrawCell())
	if entry.TerminalID == "" {
		drawEmptyPaneContent(canvas, contentRect, entry.TerminalID)
		return
	}
	terminal := findVisibleTerminal(runtimeState, entry.TerminalID)
	if terminal == nil {
		drawEmptyPaneContent(canvas, contentRect, entry.TerminalID)
		return
	}
	if terminal.Snapshot == nil || len(terminal.Snapshot.Screen.Cells) == 0 {
		canvas.drawText(contentRect.X, contentRect.Y, terminal.Name+" ["+terminal.State+"]", drawStyle{FG: "#94a3b8"})
		return
	}
	drawSnapshotWithOffset(canvas, contentRect, terminal.Snapshot, entry.ScrollOffset)
	if entry.Active {
		projectPaneCursor(canvas, contentRect, terminal.Snapshot)
	}
}

func contentRectForPane(rect workbench.Rect) workbench.Rect {
	return workbench.Rect{X: rect.X + 1, Y: rect.Y + 1, W: rect.W - 2, H: rect.H - 2}
}

func fillRect(canvas *composedCanvas, rect workbench.Rect, cell drawCell) {
	if canvas == nil || rect.W <= 0 || rect.H <= 0 {
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

func projectPaneCursor(canvas *composedCanvas, rect workbench.Rect, snapshot *protocol.Snapshot) {
	if canvas == nil || snapshot == nil || !snapshot.Cursor.Visible {
		return
	}
	x := rect.X + snapshot.Cursor.Col
	y := rect.Y + snapshot.Cursor.Row
	if x < rect.X || y < rect.Y || x >= rect.X+rect.W || y >= rect.Y+rect.H {
		return
	}
	canvas.setCursor(x, y)
}

func drawEmptyPaneContent(canvas *composedCanvas, rect workbench.Rect, terminalID string) {
	lines := []string{
		"Attach existing terminal",
		"Create new terminal",
		"Open terminal manager",
	}
	if terminalID != "" {
		lines = []string{
			"Attach existing terminal",
			"Create new terminal",
			"Open terminal manager",
			"terminal=" + terminalID,
		}
	}
	for i, line := range lines {
		if i >= rect.H {
			return
		}
		canvas.drawText(rect.X, rect.Y+i, line, drawStyle{FG: "#64748b"})
	}
}

func drawPaneTopBorderLabels(canvas *composedCanvas, rect workbench.Rect, style drawStyle, title, meta string) {
	if canvas == nil || rect.W <= 4 {
		return
	}
	innerX := rect.X + 2
	innerW := rect.W - 4
	if innerW <= 0 {
		return
	}

	titleLabel := normalizePaneBorderLabel(title)
	metaLabel := normalizePaneBorderLabel(meta)
	titleLabel = xansi.Truncate(titleLabel, innerW, "")
	titleW := xansi.StringWidth(titleLabel)
	metaW := 0
	if metaLabel != "" && titleW < innerW {
		metaLabel = xansi.Truncate(metaLabel, innerW-titleW-1, "")
		metaW = xansi.StringWidth(metaLabel)
		if metaW == 0 {
			metaLabel = ""
		}
	} else {
		metaLabel = ""
	}

	if titleLabel != "" {
		drawBorderLabel(canvas, innerX, rect.Y, titleLabel, style)
	}
	if metaLabel != "" {
		metaX := innerX + innerW - metaW
		if metaX < innerX+titleW {
			return
		}
		drawBorderLabel(canvas, metaX, rect.Y, metaLabel, style)
	}
}

func normalizePaneBorderLabel(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return " " + text + " "
}

func drawBorderLabel(canvas *composedCanvas, x, y int, text string, style drawStyle) {
	for idx, ch := range []rune(text) {
		canvas.set(x+idx, y, drawCell{Content: string(ch), Width: 1, Style: style})
	}
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

func drawSnapshotWithOffset(canvas *composedCanvas, rect workbench.Rect, snapshot *protocol.Snapshot, offset int) {
	if canvas == nil || snapshot == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	if offset <= 0 {
		canvas.drawSnapshotInRect(rect, snapshot)
		return
	}
	totalRows := len(snapshot.Scrollback) + len(snapshot.Screen.Cells)
	if totalRows == 0 {
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
		for x := 0; x < rect.W && x < len(row); x++ {
			cell := row[x]
			content := cell.Content
			if content == "" {
				content = " "
			}
			canvas.set(rect.X+x, targetY, drawCell{
				Content: content,
				Width:   maxCellWidth(cell.Width),
				Style:   cellStyleFromSnapshot(cell),
			})
		}
		targetY++
	}
}

func projectActiveEntryCursor(canvas *composedCanvas, entries []paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) {
	if canvas == nil {
		return
	}
	canvas.cursorVisible = false
	for _, entry := range entries {
		if !entry.Active {
			continue
		}
		terminal := findVisibleTerminal(runtimeState, entry.TerminalID)
		if terminal != nil && terminal.Snapshot != nil {
			projectPaneCursor(canvas, contentRectForPane(entry.Rect), terminal.Snapshot)
		}
		return
	}
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
