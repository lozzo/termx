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
	var (
		cache           *bodyRenderCache
		cursorVisibleFn func(protocol.CursorState) bool
	)
	if coordinator != nil {
		cache = coordinator.bodyCache
		cursorVisibleFn = coordinator.syntheticCursorVisible
	}
	// Render fully composes the final framebuffer every time. The only
	// production incremental output engine now lives at the presenter boundary.
	canvas := rebuildBodyCanvas(cache, entries, width, height, hostEmojiMode, cursorOffsetY, cursorVisibleFn, runtimeState)
	if coordinator != nil {
		coordinator.bodyCache = newBodyRenderCache(cache, canvas, entries, width, height)
	}
	perftrace.Count("render.body.canvas.path.full_compose", maxInt(1, width*height))
	return canvas
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
			canvas.withOwner(entry.OwnerID, func() {
				drawPaneFrame(canvas, entry.Rect, entry.SharedLeft, entry.SharedTop, entry.Title, entry.Border, entry.Theme, entry.Overflow, entry.Active, entry.Floating)
			})
		}
		canvas.withOwner(entry.OwnerID, func() {
			drawPaneContentWithKey(canvas, entry.Rect, entry, runtimeState)
		})
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
	drawPaneContentFromCacheRows(canvas, cache, entry, runtimeState, 0, interior.H-1, clearInterior, true)
}

func drawPaneContentFromCacheRows(canvas *composedCanvas, cache *bodyRenderCache, entry paneRenderEntry, runtimeState *VisibleRuntimeStateProxy, startRow, endRow int, clearRows bool, allowDelta bool) {
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
	if allowDelta && clearRows && startRow == 0 && endRow == interior.H-1 {
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

func drawResolvedPaneContentSprite(canvas *composedCanvas, entry paneRenderEntry, resolved resolvedPaneContent) {
	if canvas == nil {
		return
	}
	canvas.withOwner(entry.OwnerID, func() {
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
		drawTerminalSourceWithOffsetAndMetrics(canvas, contentRect, resolved.source, resolved.renderOffset, entry.Theme, resolved.metrics)
		if entry.CopyModeActive {
			drawCopyModeOverlay(canvas, contentRect, resolved.snapshot, entry.Theme, entry.CopyModeCursorRow, entry.CopyModeCursorCol, entry.CopyModeViewTopRow, entry.CopyModeMarkSet, entry.CopyModeMarkRow, entry.CopyModeMarkCol)
		}
		if resolved.terminalState == "exited" {
			drawExitedPaneRecoveryHints(canvas, contentRect, entry.Theme, entry.ExitedActionSelected, entry.ExitedActionPulse)
		}
	})
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
		canvas.withOwner(entry.OwnerID, func() {
			drawPaneContentFromCache(canvas, cache, entry, runtimeState, true)
		})
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
			canvas.withOwner(entry.OwnerID, func() {
				drawPaneFrame(canvas, entry.Rect, entry.SharedLeft, entry.SharedTop, entry.Title, entry.Border, entry.Theme, entry.Overflow, entry.Active, entry.Floating)
			})
		}
		startRow, endRow, ok := dirtyInteriorRows(entry, dirty)
		if !ok {
			continue
		}
		// Row-band redraw keeps the damaged-rect path cheap without arbitrary
		// X clipping. Cutting through wide cells, continuation footprints, or
		// FE0F compensation columns at arbitrary horizontal offsets is much
		// riskier than repainting the affected interior rows end-to-end.
		canvas.withOwner(entry.OwnerID, func() {
			drawPaneContentFromCacheRows(canvas, cache, entry, runtimeState, startRow, endRow, false, false)
		})
	}
	projectActiveEntryCursor(canvas, entries, runtimeState)
}

func applyOverlapIncrementalComposite(canvas *composedCanvas, cache *bodyRenderCache, entries []paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) bool {
	if canvas == nil || cache == nil {
		return false
	}
	changed := make([]int, 0, len(entries))
	for i, entry := range entries {
		if cache.frameKeys[entry.PaneID] == entry.FrameKey && cache.contentKeys[entry.PaneID] == entry.ContentKey {
			continue
		}
		changed = append(changed, i)
	}
	if len(changed) == 0 || len(changed) > 4 {
		return false
	}

	canvas.clearCursor()
	for _, idx := range changed {
		entry := entries[idx]
		frameChanged := cache.frameKeys[entry.PaneID] != entry.FrameKey
		contentChanged := cache.contentKeys[entry.PaneID] != entry.ContentKey
		if frameChanged {
			if entry.Frameless {
				canvas.withOwner(entry.OwnerID, func() {
					fillRect(canvas, entry.Rect, blankDrawCell())
				})
			} else {
				canvas.withOwner(entry.OwnerID, func() {
					drawPaneFrame(canvas, entry.Rect, entry.SharedLeft, entry.SharedTop, entry.Title, entry.Border, entry.Theme, entry.Overflow, entry.Active, entry.Floating)
				})
			}
		}
		if frameChanged || contentChanged {
			// When overlap dirty union grows to the full viewport, we still don't
			// need a full rebuild if the changed pane can update itself
			// incrementally. Repaint the changed pane first, then redraw only the
			// later overlapping panes to restore correct z-order.
			canvas.withOwner(entry.OwnerID, func() {
				drawPaneContentFromCacheRows(canvas, cache, entry, runtimeState, 0, interiorRectForEntry(entry).H-1, true, !frameChanged)
			})
		}
		for upper := idx + 1; upper < len(entries); upper++ {
			overlay := entries[upper]
			if !rectsOverlap(entry.Rect, overlay.Rect) {
				continue
			}
			if !overlay.Frameless {
				canvas.withOwner(overlay.OwnerID, func() {
					drawPaneFrame(canvas, overlay.Rect, overlay.SharedLeft, overlay.SharedTop, overlay.Title, overlay.Border, overlay.Theme, overlay.Overflow, overlay.Active, overlay.Floating)
				})
			}
			canvas.withOwner(overlay.OwnerID, func() {
				drawPaneContentFromCacheRows(canvas, cache, overlay, runtimeState, 0, interiorRectForEntry(overlay).H-1, false, false)
			})
		}
	}
	projectActiveEntryCursor(canvas, entries, runtimeState)
	return true
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
