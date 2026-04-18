package render

import (
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type bodyRenderCache struct {
	width             int
	height            int
	order             []string
	keepSprites       map[string]struct{}
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
	key         paneContentSpriteKey
	canvas      *composedCanvas
	window      terminalSourceWindowState
	contentRect workbench.Rect
	extentHash  uint64
	delta       paneContentSpriteDelta
}

type paneContentSpriteDelta struct {
	full        bool
	scrollPlan  terminalWindowScrollPlan
	changedRows []int
}

type terminalWindowScrollDirection uint8

const (
	terminalWindowScrollNone terminalWindowScrollDirection = iota
	terminalWindowScrollUp
	terminalWindowScrollDown
)

type terminalWindowScrollPlan struct {
	direction terminalWindowScrollDirection
	start     int
	end       int
	shift     int
	reused    int
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

	// Fast path: check basic key match BEFORE computing expensive hashes.
	if cached := c.contentSprites[entry.PaneID]; cached != nil && cached.key == key && cached.canvas != nil {
		cached.delta = paneContentSpriteDelta{}
		perftrace.Count("render.pane_content_sprite.hit", interior.W*interior.H)
		return cached.canvas
	}

	cached := c.contentSprites[entry.PaneID]
	if cached != nil && cached.canvas != nil && cached.key.Width == key.Width && cached.key.Height == key.Height && cached.key.Theme == key.Theme {
		resolveFinish := perftrace.Measure("render.pane_content_sprite.resolve")
		resolved := resolvePaneContent(entry, runtimeState, true)
		resolveFinish(maxInt(1, interior.W*interior.H))
		window := buildTerminalSourceWindowState(resolved.source, resolved.contentRect.H, resolved.renderOffset)
		extentHash := terminalSourceExtentHash(resolved.source, resolved.contentRect, entry.Theme)

		if canIncrementalPaneSpriteUpdate(cached, key, resolved, window, extentHash) {
			// SurfaceVersion can change without touching the visible rows. When
			// the exact window content hash still matches, preserve both sprite
			// and body canvas without even entering the row-diff path.
			if cached.window.contentHash == window.contentHash {
				cached.key = key
				cached.window = window
				cached.contentRect = resolved.contentRect
				cached.extentHash = extentHash
				cached.delta = paneContentSpriteDelta{}
				perftrace.Count("render.pane_content_sprite.incremental.noop_hit", maxInt(1, interior.W*interior.H))
				return cached.canvas
			}

			incrementalFinish := perftrace.Measure("render.pane_content_sprite.incremental_apply")
			delta := applyIncrementalPaneSpriteRows(cached.canvas, resolved, entry.Theme, cached.window, window)
			incrementalFinish(maxInt(1, delta.changedRowCount()) * maxInt(1, resolved.contentRect.W))
			cached.key = key
			cached.window = window
			cached.contentRect = resolved.contentRect
			cached.extentHash = extentHash
			cached.delta = delta
			perftrace.Count("render.pane_content_sprite.incremental", maxInt(1, delta.changedRowCount())*maxInt(1, resolved.contentRect.W))
			perftrace.Count("render.pane_content_sprite.miss", interior.W*interior.H)
			return cached.canvas
		}

		// Full miss with same dimensions: reuse canvas allocation.
		sprite := cached.canvas
		fullFinish := perftrace.Measure("render.pane_content_sprite.full_redraw")
		sprite.resetToBlank()
		drawResolvedPaneContentSprite(sprite, entry, resolved)
		fullFinish(maxInt(1, interior.W*interior.H))
		c.contentSprites[entry.PaneID] = &paneContentSpriteCacheEntry{
			key:         key,
			canvas:      sprite,
			window:      window,
			contentRect: resolved.contentRect,
			extentHash:  extentHash,
			delta:       paneContentSpriteDelta{full: true},
		}
		perftrace.Count("render.pane_content_sprite.miss", interior.W*interior.H)
		return sprite
	}

	var sprite *composedCanvas
	if cached != nil && cached.canvas != nil && cached.key.Width == key.Width && cached.key.Height == key.Height {
		sprite = cached.canvas
		fullFinish := perftrace.Measure("render.pane_content_sprite.full_redraw")
		sprite.resetToBlank()
		resolveFinish := perftrace.Measure("render.pane_content_sprite.resolve")
		resolved := resolvePaneContent(entry, runtimeState, true)
		resolveFinish(maxInt(1, interior.W*interior.H))
		drawResolvedPaneContentSprite(sprite, entry, resolved)
		fullFinish(maxInt(1, interior.W*interior.H))
		window := buildTerminalSourceWindowState(resolved.source, resolved.contentRect.H, resolved.renderOffset)
		extentHash := terminalSourceExtentHash(resolved.source, resolved.contentRect, entry.Theme)
		c.contentSprites[entry.PaneID] = &paneContentSpriteCacheEntry{
			key:         key,
			canvas:      sprite,
			window:      window,
			contentRect: resolved.contentRect,
			extentHash:  extentHash,
			delta:       paneContentSpriteDelta{full: true},
		}
		perftrace.Count("render.pane_content_sprite.miss", interior.W*interior.H)
		return sprite
	}

	sprite = newComposedCanvas(interior.W, interior.H)
	resolveFinish := perftrace.Measure("render.pane_content_sprite.resolve")
	resolved := resolvePaneContent(entry, runtimeState, true)
	resolveFinish(maxInt(1, interior.W*interior.H))
	fullFinish := perftrace.Measure("render.pane_content_sprite.full_redraw")
	drawResolvedPaneContentSprite(sprite, entry, resolved)
	fullFinish(maxInt(1, interior.W*interior.H))

	window := buildTerminalSourceWindowState(resolved.source, resolved.contentRect.H, resolved.renderOffset)
	extentHash := terminalSourceExtentHash(resolved.source, resolved.contentRect, entry.Theme)
	c.contentSprites[entry.PaneID] = &paneContentSpriteCacheEntry{
		key:         key,
		canvas:      sprite,
		window:      window,
		contentRect: resolved.contentRect,
		extentHash:  extentHash,
		delta:       paneContentSpriteDelta{full: true},
	}
	perftrace.Count("render.pane_content_sprite.miss", interior.W*interior.H)
	return sprite
}

func canIncrementalPaneSpriteUpdate(cached *paneContentSpriteCacheEntry, nextKey paneContentSpriteKey, resolved resolvedPaneContent, nextWindow terminalSourceWindowState, nextExtentHash uint64) bool {
	if cached == nil || cached.canvas == nil || resolved.source == nil {
		return false
	}
	if cached.key.Width != nextKey.Width || cached.key.Height != nextKey.Height || cached.key.Theme != nextKey.Theme {
		return false
	}
	if cached.contentRect != resolved.contentRect || cached.extentHash != nextExtentHash {
		return false
	}
	previousStatic := cached.key.ContentKey
	nextStatic := nextKey.ContentKey
	previousStatic.SurfaceVersion = 0
	nextStatic.SurfaceVersion = 0
	previousStatic.ScrollOffset = 0
	nextStatic.ScrollOffset = 0
	if previousStatic != nextStatic {
		return false
	}
	return compatibleTerminalWindowStates(cached.window, nextWindow)
}

func applyIncrementalPaneSpriteRows(canvas *composedCanvas, resolved resolvedPaneContent, theme uiTheme, previous, next terminalSourceWindowState) paneContentSpriteDelta {
	if canvas == nil || resolved.source == nil || resolved.contentRect.W <= 0 || resolved.contentRect.H <= 0 {
		return paneContentSpriteDelta{}
	}
	rowScratch := newComposedCanvas(resolved.contentRect.W, 1)
	rowScratch.hostEmojiVS16Mode = canvas.hostEmojiVS16Mode

	if plan, ok := detectTerminalWindowScroll(previous, next); ok {
		return applyIncrementalPaneSpriteScrollPlan(canvas, rowScratch, resolved, theme, previous, next, plan)
	}

	baseChangedRows := terminalWindowChangedRows(previous, next, terminalWindowScrollPlan{})
	if len(baseChangedRows) == 0 {
		return paneContentSpriteDelta{}
	}
	if plan, ok := detectTerminalWindowPartialScroll(previous, next, len(baseChangedRows)); ok {
		return applyIncrementalPaneSpriteScrollPlan(canvas, rowScratch, resolved, theme, previous, next, plan)
	}

	delta := paneContentSpriteDelta{changedRows: append([]int(nil), baseChangedRows...)}
	for _, line := range baseChangedRows {
		targetY := resolved.contentRect.Y + line
		rowIndex := -1
		if line < len(next.rowIndices) {
			rowIndex = next.rowIndices[line]
		}
		drawPaneContentSpriteRowDiff(canvas, rowScratch, resolved.contentRect, resolved.source, rowIndex, targetY, theme)
	}
	perftrace.Count("render.pane_content_sprite.incremental.row_redraw_rows", len(delta.changedRows))
	return delta
}

func applyIncrementalPaneSpriteScrollPlan(canvas, rowScratch *composedCanvas, resolved resolvedPaneContent, theme uiTheme, previous, next terminalSourceWindowState, plan terminalWindowScrollPlan) paneContentSpriteDelta {
	if canvas == nil || rowScratch == nil || !plan.valid(len(next.exactRowHashes)) {
		return paneContentSpriteDelta{}
	}
	if !canvas.shiftRowBand(plan.start, plan.end, plan.shift, plan.direction) {
		return paneContentSpriteDelta{}
	}

	changedRows := terminalWindowChangedRows(previous, next, plan)
	for _, line := range changedRows {
		targetY := resolved.contentRect.Y + line
		rowIndex := -1
		if line < len(next.rowIndices) {
			rowIndex = next.rowIndices[line]
		}
		drawPaneContentSpriteRowDiff(canvas, rowScratch, resolved.contentRect, resolved.source, rowIndex, targetY, theme)
	}

	if plan.wholeWindow(len(next.exactRowHashes)) {
		perftrace.Count("render.pane_content_sprite.incremental.scroll_hit", 1)
		perftrace.Count("render.pane_content_sprite.incremental.scroll_shift", plan.shift)
	} else {
		perftrace.Count("render.pane_content_sprite.incremental.partial_scroll_hit", 1)
		perftrace.Count("render.pane_content_sprite.incremental.partial_scroll_shift", plan.shift)
		perftrace.Count("render.pane_content_sprite.incremental.partial_scroll_reused_rows", plan.reused)
		perftrace.Count("render.pane_content_sprite.incremental.partial_scroll_residual_redraw_rows", maxInt(0, len(changedRows)-plan.shift))
	}

	return paneContentSpriteDelta{
		scrollPlan:  plan,
		changedRows: changedRows,
	}
}

func detectTerminalWindowScroll(previous, next terminalSourceWindowState) (terminalWindowScrollPlan, bool) {
	if !compatibleTerminalWindowStates(previous, next) {
		return terminalWindowScrollPlan{}, false
	}
	height := len(next.exactRowHashes)
	for shift := 1; shift < height; shift++ {
		if terminalWindowMatchesScrollUp(previous, next, shift) {
			return terminalWindowScrollPlan{
				direction: terminalWindowScrollUp,
				start:     0,
				end:       height - 1,
				shift:     shift,
				reused:    height - shift,
			}, true
		}
		if terminalWindowMatchesScrollDown(previous, next, shift) {
			return terminalWindowScrollPlan{
				direction: terminalWindowScrollDown,
				start:     0,
				end:       height - 1,
				shift:     shift,
				reused:    height - shift,
			}, true
		}
	}
	return terminalWindowScrollPlan{}, false
}

func detectTerminalWindowPartialScroll(previous, next terminalSourceWindowState, totalChangedRows int) (terminalWindowScrollPlan, bool) {
	if !compatibleTerminalWindowStates(previous, next) || !previous.screenWindow || !next.screenWindow {
		return terminalWindowScrollPlan{}, false
	}
	best := terminalWindowScrollPlan{}
	height := len(next.exactRowHashes)
	for shift := 1; shift < height; shift++ {
		scanTerminalWindowScrollRuns(previous, next, shift, terminalWindowScrollUp, &best)
		scanTerminalWindowScrollRuns(previous, next, shift, terminalWindowScrollDown, &best)
	}
	if !partialTerminalWindowScrollWorthIt(best, totalChangedRows) {
		return terminalWindowScrollPlan{}, false
	}
	return best, true
}

func compatibleTerminalWindowStates(previous, next terminalSourceWindowState) bool {
	return len(previous.exactRowHashes) > 0 &&
		len(previous.exactRowHashes) == len(next.exactRowHashes) &&
		len(previous.rowIndices) == len(next.rowIndices) &&
		len(previous.rowIdentityHashes) == len(next.rowIdentityHashes)
}

func terminalWindowMatchesScrollUp(previous, next terminalSourceWindowState, shift int) bool {
	height := len(next.exactRowHashes)
	scrollUp := true
	for line := 0; line+shift < height; line++ {
		if previous.rowIndices[line+shift] != next.rowIndices[line] || previous.exactRowHashes[line+shift] != next.exactRowHashes[line] {
			scrollUp = false
			break
		}
	}
	if scrollUp {
		return true
	}
	if !previous.screenWindow || !next.screenWindow {
		return false
	}
	for line := 0; line+shift < height; line++ {
		if previous.rowIndices[line+shift] < 0 || next.rowIndices[line] < 0 || previous.rowIdentityHashes[line+shift] != next.rowIdentityHashes[line] {
			return false
		}
	}
	return true
}

func terminalWindowMatchesScrollDown(previous, next terminalSourceWindowState, shift int) bool {
	height := len(next.exactRowHashes)
	scrollDown := true
	for line := 0; line+shift < height; line++ {
		if previous.rowIndices[line] != next.rowIndices[line+shift] || previous.exactRowHashes[line] != next.exactRowHashes[line+shift] {
			scrollDown = false
			break
		}
	}
	if scrollDown {
		return true
	}
	if !previous.screenWindow || !next.screenWindow {
		return false
	}
	for line := 0; line+shift < height; line++ {
		if previous.rowIndices[line] < 0 || next.rowIndices[line+shift] < 0 || previous.rowIdentityHashes[line] != next.rowIdentityHashes[line+shift] {
			return false
		}
	}
	return true
}

func scanTerminalWindowScrollRuns(previous, next terminalSourceWindowState, shift int, direction terminalWindowScrollDirection, best *terminalWindowScrollPlan) {
	if best == nil {
		return
	}
	runStart := -1
	runLength := 0
	flush := func(endExclusive int) {
		if runLength == 0 {
			return
		}
		candidate := terminalWindowScrollPlan{
			direction: direction,
			start:     runStart,
			end:       endExclusive + shift - 1,
			shift:     shift,
			reused:    runLength,
		}
		if betterTerminalWindowScrollPlan(candidate, *best) {
			*best = candidate
		}
		runStart = -1
		runLength = 0
	}

	limit := len(previous.rowIdentityHashes) - shift
	for i := 0; i < limit; i++ {
		var previousLine int
		var nextLine int
		switch direction {
		case terminalWindowScrollUp:
			previousLine = i + shift
			nextLine = i
		case terminalWindowScrollDown:
			previousLine = i
			nextLine = i + shift
		default:
			return
		}
		matches := previous.rowIndices[previousLine] >= 0 &&
			next.rowIndices[nextLine] >= 0 &&
			previous.rowIdentityHashes[previousLine] == next.rowIdentityHashes[nextLine]
		if matches {
			if runLength == 0 {
				runStart = i
			}
			runLength++
			continue
		}
		flush(i)
	}
	flush(limit)
}

func betterTerminalWindowScrollPlan(candidate, current terminalWindowScrollPlan) bool {
	if candidate.reused == 0 {
		return false
	}
	if current.reused == 0 {
		return true
	}
	if candidate.reused != current.reused {
		return candidate.reused > current.reused
	}
	if candidate.shift != current.shift {
		return candidate.shift < current.shift
	}
	if candidate.start != current.start {
		return candidate.start < current.start
	}
	return candidate.direction < current.direction
}

func partialTerminalWindowScrollWorthIt(plan terminalWindowScrollPlan, totalChangedRows int) bool {
	if plan.direction == terminalWindowScrollNone || plan.reused < 4 || totalChangedRows <= 0 {
		return false
	}
	residualRedrawRows := totalChangedRows - plan.reused
	if residualRedrawRows <= 0 || residualRedrawRows >= totalChangedRows {
		return false
	}
	return residualRedrawRows < plan.reused
}

func terminalWindowChangedRows(previous, next terminalSourceWindowState, plan terminalWindowScrollPlan) []int {
	if !compatibleTerminalWindowStates(previous, next) {
		return nil
	}
	changedRows := make([]int, 0, len(next.exactRowHashes))
	for line := range next.exactRowHashes {
		if previous.exactRowHashes[line] == next.exactRowHashes[line] {
			continue
		}
		if plan.reusesLine(line) {
			continue
		}
		changedRows = append(changedRows, line)
	}
	return changedRows
}

func (d paneContentSpriteDelta) changedRowCount() int {
	return len(d.changedRows)
}

func (p terminalWindowScrollPlan) valid(height int) bool {
	if p.direction == terminalWindowScrollNone || p.shift <= 0 || p.reused <= 0 || height <= 0 {
		return false
	}
	if p.start < 0 || p.end < p.start || p.end >= height {
		return false
	}
	return p.end-p.start+1 > p.shift
}

func (p terminalWindowScrollPlan) wholeWindow(height int) bool {
	return p.valid(height) && p.start == 0 && p.end == height-1
}

func (p terminalWindowScrollPlan) reusesLine(line int) bool {
	if p.direction == terminalWindowScrollNone || line < 0 {
		return false
	}
	switch p.direction {
	case terminalWindowScrollUp:
		return line >= p.start && line <= p.end-p.shift
	case terminalWindowScrollDown:
		return line >= p.start+p.shift && line <= p.end
	default:
		return false
	}
}

func (c *bodyRenderCache) applySpriteDeltaToCanvas(canvas *composedCanvas, entry paneRenderEntry) bool {
	if c == nil || canvas == nil || entry.CopyModeActive || entry.TerminalID == "" || entry.ContentKey.State == "exited" {
		return false
	}
	cached := c.contentSprites[entry.PaneID]
	if cached == nil || cached.canvas == nil || cached.delta.full {
		return false
	}
	interior := interiorRectForEntry(entry)
	if interior.W <= 0 || interior.H <= 0 {
		return false
	}
	if !cached.delta.scrollPlan.valid(interior.H) {
		if len(cached.delta.changedRows) == 0 {
			return true
		}
		applied := false
		for _, line := range cached.delta.changedRows {
			if line < 0 || line >= interior.H {
				continue
			}
			canvas.blitRowFrom(cached.canvas, line, interior.X, interior.Y+line, interior.W)
			applied = true
		}
		return applied
	}
	if !canvas.shiftRectRowBand(interior, cached.delta.scrollPlan.start, cached.delta.scrollPlan.end, cached.delta.scrollPlan.shift, cached.delta.scrollPlan.direction) {
		return false
	}
	applied := false
	for _, line := range cached.delta.changedRows {
		if line < 0 || line >= interior.H {
			continue
		}
		canvas.blitRowFrom(cached.canvas, line, interior.X, interior.Y+line, interior.W)
		applied = true
	}
	return applied || len(cached.delta.changedRows) == 0
}

func (c *bodyRenderCache) applySpriteDeltaToCanvasRows(canvas *composedCanvas, entry paneRenderEntry, startRow, endRow int) bool {
	if c == nil || canvas == nil || entry.CopyModeActive || entry.TerminalID == "" || entry.ContentKey.State == "exited" {
		return false
	}
	cached := c.contentSprites[entry.PaneID]
	if cached == nil || cached.canvas == nil || cached.delta.full {
		return false
	}
	interior := interiorRectForEntry(entry)
	if interior.W <= 0 || interior.H <= 0 {
		return false
	}
	if startRow < 0 {
		startRow = 0
	}
	if endRow >= interior.H {
		endRow = interior.H - 1
	}
	if startRow > endRow {
		return false
	}
	if cached.delta.scrollPlan.valid(interior.H) {
		if startRow != 0 || endRow != interior.H-1 {
			return false
		}
		return c.applySpriteDeltaToCanvas(canvas, entry)
	}
	if len(cached.delta.changedRows) == 0 {
		return true
	}
	applied := false
	for _, line := range cached.delta.changedRows {
		if line < startRow || line > endRow {
			continue
		}
		canvas.blitRowFrom(cached.canvas, line, interior.X, interior.Y+line, interior.W)
		applied = true
	}
	return applied
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
	if c.keepSprites == nil {
		c.keepSprites = make(map[string]struct{}, len(entries))
	} else {
		for key := range c.keepSprites {
			delete(c.keepSprites, key)
		}
	}
	for _, entry := range entries {
		c.order = append(c.order, entry.PaneID)
		c.rects[entry.PaneID] = entry.Rect
		c.frameKeys[entry.PaneID] = entry.FrameKey
		c.contentKeys[entry.PaneID] = entry.ContentKey
		c.keepSprites[entry.PaneID] = struct{}{}
	}
	if c.contentSprites != nil {
		for paneID := range c.contentSprites {
			if _, ok := c.keepSprites[paneID]; !ok {
				delete(c.contentSprites, paneID)
			}
		}
	}
}

func (c *bodyRenderCache) matches(entries []paneRenderEntry, width, height int, hostEmojiMode shared.AmbiguousEmojiVariationSelectorMode) bool {
	if !c.compatible(entries, width, height, hostEmojiMode) {
		return false
	}
	for i, entry := range entries {
		if c.order[i] != entry.PaneID || c.rects[entry.PaneID] != entry.Rect {
			return false
		}
	}
	return true
}

func (c *bodyRenderCache) compatible(entries []paneRenderEntry, width, height int, hostEmojiMode shared.AmbiguousEmojiVariationSelectorMode) bool {
	if c == nil || c.canvas == nil || c.width != width || c.height != height || c.hostEmojiVS16Mode != hostEmojiMode || len(c.order) != len(entries) {
		return false
	}
	for i, entry := range entries {
		if c.order[i] != entry.PaneID {
			return false
		}
	}
	return true
}
