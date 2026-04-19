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
		extentHash := terminalSourceExtentHashWithMetrics(resolved.source, resolved.contentRect, entry.Theme, resolved.metrics)

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

			hint := effectiveTerminalScreenUpdateHint(resolved.screenUpdate, resolved.surface != nil, entry.SurfaceVersion)
			incrementalFinish := perftrace.Measure("render.pane_content_sprite.incremental_apply")
			delta := applyIncrementalPaneSpriteRows(cached.canvas, resolved, entry.Theme, cached.window, window, hint)
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
		extentHash := terminalSourceExtentHashWithMetrics(resolved.source, resolved.contentRect, entry.Theme, resolved.metrics)
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
	extentHash := terminalSourceExtentHashWithMetrics(resolved.source, resolved.contentRect, entry.Theme, resolved.metrics)
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

func applyIncrementalPaneSpriteRows(canvas *composedCanvas, resolved resolvedPaneContent, theme uiTheme, previous, next terminalSourceWindowState, hint terminalScreenUpdateHint) paneContentSpriteDelta {
	if canvas == nil || resolved.source == nil || resolved.contentRect.W <= 0 || resolved.contentRect.H <= 0 {
		return paneContentSpriteDelta{}
	}
	rowScratch := newComposedCanvas(resolved.contentRect.W, 1)
	rowScratch.hostEmojiVS16Mode = canvas.hostEmojiVS16Mode

	deltaPlan := planTerminalWindowDelta(previous, next, hint)
	if len(deltaPlan.changedRows) == 0 && !deltaPlan.usesScroll() {
		return paneContentSpriteDelta{}
	}
	if deltaPlan.usesScroll() {
		return applyIncrementalPaneSpriteScrollPlan(canvas, rowScratch, resolved, theme, next, deltaPlan)
	}

	delta := paneContentSpriteDelta{changedRows: append([]int(nil), deltaPlan.changedRows...)}
	for _, line := range delta.changedRows {
		targetY := resolved.contentRect.Y + line
		rowIndex := -1
		if line < len(next.rowIndices) {
			rowIndex = next.rowIndices[line]
		}
		drawPaneContentSpriteRowDiffWithMetrics(canvas, rowScratch, resolved.contentRect, resolved.source, rowIndex, targetY, theme, resolved.metrics)
	}
	if deltaPlan.explicitHint {
		perftrace.Count("render.pane_content_sprite.incremental.explicit_changed_rows_hit", 1)
		perftrace.Count("render.pane_content_sprite.incremental.explicit_changed_rows", len(delta.changedRows))
	}
	perftrace.Count("render.pane_content_sprite.incremental.row_redraw_rows", len(delta.changedRows))
	return delta
}

func (p terminalWindowDeltaPlan) usesScroll() bool {
	return p.scrollPlan.direction != terminalWindowScrollNone && p.scrollPlan.shift > 0
}

func applyIncrementalPaneSpriteScrollPlan(canvas, rowScratch *composedCanvas, resolved resolvedPaneContent, theme uiTheme, next terminalSourceWindowState, plan terminalWindowDeltaPlan) paneContentSpriteDelta {
	if canvas == nil || rowScratch == nil || !plan.scrollPlan.valid(len(next.exactRowHashes)) {
		return paneContentSpriteDelta{}
	}
	if !canvas.shiftRowBand(plan.scrollPlan.start, plan.scrollPlan.end, plan.scrollPlan.shift, plan.scrollPlan.direction) {
		return paneContentSpriteDelta{}
	}

	changedRows := append([]int(nil), plan.changedRows...)
	for _, line := range changedRows {
		targetY := resolved.contentRect.Y + line
		rowIndex := -1
		if line < len(next.rowIndices) {
			rowIndex = next.rowIndices[line]
		}
		drawPaneContentSpriteRowDiffWithMetrics(canvas, rowScratch, resolved.contentRect, resolved.source, rowIndex, targetY, theme, resolved.metrics)
	}

	if plan.scrollPlan.wholeWindow(len(next.exactRowHashes)) {
		perftrace.Count("render.pane_content_sprite.incremental.scroll_hit", 1)
		perftrace.Count("render.pane_content_sprite.incremental.scroll_shift", plan.scrollPlan.shift)
	} else {
		perftrace.Count("render.pane_content_sprite.incremental.partial_scroll_hit", 1)
		perftrace.Count("render.pane_content_sprite.incremental.partial_scroll_shift", plan.scrollPlan.shift)
		perftrace.Count("render.pane_content_sprite.incremental.partial_scroll_reused_rows", plan.scrollPlan.reused)
		perftrace.Count("render.pane_content_sprite.incremental.partial_scroll_residual_redraw_rows", len(changedRows))
	}

	return paneContentSpriteDelta{
		scrollPlan:  plan.scrollPlan,
		changedRows: changedRows,
	}
}

func (d paneContentSpriteDelta) changedRowCount() int {
	return len(d.changedRows)
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
