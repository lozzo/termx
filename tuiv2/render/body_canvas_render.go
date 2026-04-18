package render

import (
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func renderBodyCanvas(coordinator *Coordinator, runtimeState *VisibleRuntimeStateProxy, immersiveZoom bool, entries []paneRenderEntry, width, height int) *composedCanvas {
	finish := perftrace.Measure("render.body.canvas")
	defer finish(0)
	hostEmojiMode := emojiVariationSelectorModeForRuntime(runtimeState)
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
			drawPaneContentWithKey(canvas, entry.Rect, entry, runtimeState)
		}
		projectActiveEntryCursor(canvas, entries, runtimeState)
		return canvas
	}
	cache := coordinator.bodyCache
	if cache == nil || !cache.compatible(entries, width, height, hostEmojiMode) {
		canvas := rebuildBodyCanvas(cache, entries, width, height, hostEmojiMode, cursorOffsetY, coordinator.syntheticCursorVisible, runtimeState)
		coordinator.bodyCache = newBodyRenderCache(cache, canvas, entries, width, height)
		return canvas
	}
	overlap := entriesOverlap(entries)
	matches := cache.matches(entries, width, height, hostEmojiMode)
	if overlap {
		changed := !matches
		if !changed {
			for _, entry := range entries {
				if cache.frameKeys[entry.PaneID] != entry.FrameKey || cache.contentKeys[entry.PaneID] != entry.ContentKey {
					changed = true
					break
				}
			}
		}
		cache.canvas.clearCursor()
		if !changed {
			projectActiveEntryCursor(cache.canvas, entries, runtimeState)
			return cache.canvas
		}
		// Same-rect overlap updates still have a bounded damage region. Rebuilds
		// here throw away earlier projection/pipeline wins even though only a
		// small stacked slice actually changed, so try the damaged-rect path
		// before falling back to a full body recompose.
		if dirty, ok := overlapDamagedRect(cache, entries, width, height); ok {
			redrawDamagedRect(cache.canvas, cache, entries, runtimeState, dirty)
			cache.reset(entries, width, height)
			perftrace.Count("render.body.canvas.damaged_rect", dirty.W*dirty.H)
			perftrace.Count("render.body.canvas.path.overlap_damaged_rect", 0)
			if matches {
				perftrace.Count("render.body.canvas.path.overlap_same_rect_dirty", 0)
			}
			return cache.canvas
		}
		perftrace.Count("render.body.canvas.path.overlap_full_rebuild", 0)
		canvas := rebuildBodyCanvas(cache, entries, width, height, hostEmojiMode, cursorOffsetY, coordinator.syntheticCursorVisible, runtimeState)
		cache.canvas = canvas
		cache.reset(entries, width, height)
		return canvas
	}
	if !matches {
		canvas := rebuildBodyCanvas(cache, entries, width, height, hostEmojiMode, cursorOffsetY, coordinator.syntheticCursorVisible, runtimeState)
		cache.canvas = canvas
		cache.reset(entries, width, height)
		return canvas
	}

	if !overlap {
		changed := false
		activeContentRedrawn := false
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
				drawPaneContentFromCache(cache.canvas, cache, entry, runtimeState, true)
				if entry.Active {
					activeContentRedrawn = true
				}
				changed = true
			}
		}
		if !activeContentRedrawn {
			restoreActiveEntryContent(cache.canvas, cache, entries, runtimeState)
		}
		if changed {
			projectActiveEntryCursor(cache.canvas, entries, runtimeState)
			cache.reset(entries, width, height)
			return cache.canvas
		}
		projectActiveEntryCursor(cache.canvas, entries, runtimeState)
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
	interior := interiorRectForEntry(entry)
	if interior.W <= 0 || interior.H <= 0 {
		return
	}
	drawPaneContentFromCacheRows(canvas, cache, entry, runtimeState, 0, interior.H-1, clearInterior)
}

func drawPaneContentFromCacheRows(canvas *composedCanvas, cache *bodyRenderCache, entry paneRenderEntry, runtimeState *VisibleRuntimeStateProxy, startRow, endRow int, clearRows bool) {
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
	if startRow < 0 {
		startRow = 0
	}
	if endRow >= interior.H {
		endRow = interior.H - 1
	}
	if startRow > endRow {
		return
	}
	rowCount := endRow - startRow + 1
	contentSpriteFinish := perftrace.Measure("render.body.canvas.content_sprite")
	sprite := cache.contentSprite(entry, runtimeState)
	contentSpriteFinish(maxInt(1, interior.W*rowCount))
	if sprite == nil {
		drawPaneContentWithKey(canvas, entry.Rect, entry, runtimeState)
		return
	}
	if clearRows && startRow == 0 && endRow == interior.H-1 {
		deltaFinish := perftrace.Measure("render.body.canvas.apply_sprite_delta")
		applied := cache.applySpriteDeltaToCanvasRows(canvas, entry, startRow, endRow)
		deltaFinish(maxInt(1, interior.W*rowCount))
		if applied {
			return
		}
	}
	fullBlitFinish := perftrace.Measure("render.body.canvas.full_sprite_blit")
	if clearRows {
		fillRect(canvas, workbench.Rect{
			X: interior.X,
			Y: interior.Y + startRow,
			W: interior.W,
			H: rowCount,
		}, blankDrawCell())
	}
	canvas.blitRowsFrom(sprite, startRow, interior.X, interior.Y+startRow, interior.W, rowCount)
	fullBlitFinish(maxInt(1, interior.W*rowCount))
}

func drawPaneContentSprite(canvas *composedCanvas, entry paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) {
	drawResolvedPaneContentSprite(canvas, entry, resolvePaneContent(entry, runtimeState, true))
}

func drawResolvedPaneContentSprite(canvas *composedCanvas, entry paneRenderEntry, resolved resolvedPaneContent) {
	if canvas == nil {
		return
	}
	fillRect(canvas, workbench.Rect{W: canvas.width, H: canvas.height}, blankDrawCell())
	contentRect := resolved.contentRect
	if contentRect.W <= 0 || contentRect.H <= 0 {
		return
	}
	if entry.TerminalID == "" {
		drawEmptyPaneContent(canvas, contentRect, entry.PaneID, entry.TerminalID, entry.Theme, entry.EmptyActionSelected)
		return
	}
	if !resolved.terminalKnown {
		drawEmptyPaneContent(canvas, contentRect, entry.PaneID, entry.TerminalID, entry.Theme, -1)
		return
	}
	if resolved.source == nil || resolved.source.ScreenRows() == 0 {
		canvas.drawText(contentRect.X, contentRect.Y, resolved.terminalName+" ["+resolved.terminalState+"]", drawStyle{FG: entry.Theme.panelMuted})
		if resolved.terminalState == "exited" {
			drawExitedPaneRecoveryHints(canvas, contentRect, entry.Theme, entry.ExitedActionSelected, entry.ExitedActionPulse)
		}
		return
	}
	drawTerminalSourceWithOffset(canvas, contentRect, resolved.source, resolved.renderOffset, entry.Theme)
	if entry.CopyModeActive {
		drawCopyModeOverlay(canvas, contentRect, resolved.snapshot, entry.Theme, entry.CopyModeCursorRow, entry.CopyModeCursorCol, entry.CopyModeViewTopRow, entry.CopyModeMarkSet, entry.CopyModeMarkRow, entry.CopyModeMarkCol)
	}
	if resolved.terminalState == "exited" {
		drawExitedPaneRecoveryHints(canvas, contentRect, entry.Theme, entry.ExitedActionSelected, entry.ExitedActionPulse)
	}
}

func restoreActiveEntryContent(canvas *composedCanvas, cache *bodyRenderCache, entries []paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) {
	if canvas == nil {
		return
	}
	for _, entry := range entries {
		if !entry.Active {
			continue
		}
		// Use sprite cache to restore content cheaply instead of full redraw
		drawPaneContentFromCache(canvas, cache, entry, runtimeState, true)
		return
	}
}

func redrawDamagedRect(canvas *composedCanvas, cache *bodyRenderCache, entries []paneRenderEntry, runtimeState *VisibleRuntimeStateProxy, dirty workbench.Rect) {
	if canvas == nil || dirty.W <= 0 || dirty.H <= 0 {
		return
	}
	canvas.clearCursor()
	fillRect(canvas, dirty, blankDrawCell())
	for _, entry := range entries {
		if !rectsOverlap(entry.Rect, dirty) {
			continue
		}
		if !entry.Frameless {
			drawPaneFrame(canvas, entry.Rect, entry.SharedLeft, entry.SharedTop, entry.Title, entry.Border, entry.Theme, entry.Overflow, entry.Active, entry.Floating)
		}
		startRow, endRow, ok := dirtyInteriorRows(entry, dirty)
		if !ok {
			continue
		}
		// Row-band redraw keeps the damaged-rect path cheap without arbitrary
		// X clipping. Cutting through wide cells, continuation footprints, or
		// FE0F compensation columns at arbitrary horizontal offsets is much
		// riskier than repainting the affected interior rows end-to-end.
		drawPaneContentFromCacheRows(canvas, cache, entry, runtimeState, startRow, endRow, false)
	}
	projectActiveEntryCursor(canvas, entries, runtimeState)
}

func overlapDamagedRect(cache *bodyRenderCache, entries []paneRenderEntry, width, height int) (workbench.Rect, bool) {
	if cache == nil {
		return workbench.Rect{}, false
	}
	dirty := workbench.Rect{}
	changed := 0
	canvasArea := maxInt(1, width*height)
	for _, entry := range entries {
		oldRect, ok := cache.rects[entry.PaneID]
		if !ok {
			perftrace.Count("render.body.canvas.overlap_dirty.miss.invalid_rect", 0)
			return workbench.Rect{}, false
		}
		frameChanged := cache.frameKeys[entry.PaneID] != entry.FrameKey
		contentChanged := cache.contentKeys[entry.PaneID] != entry.ContentKey
		rectChanged := oldRect != entry.Rect
		if !rectChanged && !frameChanged && !contentChanged {
			continue
		}
		changed++
		if changed > 4 {
			perftrace.Count("render.body.canvas.overlap.changed_entries", changed)
			perftrace.Count("render.body.canvas.overlap_dirty.miss.too_many_changed", 0)
			return workbench.Rect{}, false
		}
		if rectChanged {
			dirty = unionRects(dirty, oldRect)
			dirty = unionRects(dirty, entry.Rect)
		} else if frameChanged {
			dirty = unionRects(dirty, entry.Rect)
		} else if entry.Frameless {
			dirty = unionRects(dirty, entry.Rect)
		} else {
			dirty = unionRects(dirty, interiorRectForEntry(entry))
		}
		if !entry.Floating && dirty.W*dirty.H >= canvasArea*7/10 {
			perftrace.Count("render.body.canvas.overlap.changed_entries", changed)
			perftrace.Count("render.body.canvas.overlap.changed_area", dirty.W*dirty.H)
			perftrace.Count("render.body.canvas.overlap_dirty.miss.area_too_large", 0)
			return workbench.Rect{}, false
		}
	}
	if changed == 0 {
		return workbench.Rect{}, false
	}
	dirty, ok := clipRectToViewport(dirty, width, height)
	if !ok {
		perftrace.Count("render.body.canvas.overlap.changed_entries", changed)
		perftrace.Count("render.body.canvas.overlap_dirty.miss.invalid_rect", 0)
		return workbench.Rect{}, false
	}
	perftrace.Count("render.body.canvas.overlap.changed_entries", changed)
	perftrace.Count("render.body.canvas.overlap.changed_area", dirty.W*dirty.H)
	if dirty.W*dirty.H >= canvasArea*9/10 {
		perftrace.Count("render.body.canvas.overlap_dirty.miss.area_too_large", 0)
		return workbench.Rect{}, false
	}
	return dirty, true
}

func dirtyInteriorRows(entry paneRenderEntry, dirty workbench.Rect) (int, int, bool) {
	interior := interiorRectForEntry(entry)
	intersection, ok := intersectRects(interior, dirty)
	if !ok {
		return 0, 0, false
	}
	startRow := intersection.Y - interior.Y
	endRow := startRow + intersection.H - 1
	return startRow, endRow, true
}

func intersectRects(a, b workbench.Rect) (workbench.Rect, bool) {
	x1 := maxInt(a.X, b.X)
	y1 := maxInt(a.Y, b.Y)
	x2 := minInt(a.X+a.W, b.X+b.W)
	y2 := minInt(a.Y+a.H, b.Y+b.H)
	if x1 >= x2 || y1 >= y2 {
		return workbench.Rect{}, false
	}
	return workbench.Rect{X: x1, Y: y1, W: x2 - x1, H: y2 - y1}, true
}

func unionRects(a, b workbench.Rect) workbench.Rect {
	if a.W <= 0 || a.H <= 0 {
		return b
	}
	if b.W <= 0 || b.H <= 0 {
		return a
	}
	x1 := minInt(a.X, b.X)
	y1 := minInt(a.Y, b.Y)
	x2 := maxInt(a.X+a.W, b.X+b.W)
	y2 := maxInt(a.Y+a.H, b.Y+b.H)
	return workbench.Rect{X: x1, Y: y1, W: x2 - x1, H: y2 - y1}
}
