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
	if overlap && !cache.matches(entries, width, height, hostEmojiMode) {
		if dirty, ok := overlapDamagedRect(cache, entries, width, height); ok {
			redrawDamagedRect(cache.canvas, cache, entries, runtimeState, dirty)
			cache.reset(entries, width, height)
			perftrace.Count("render.body.canvas.damaged_rect", dirty.W*dirty.H)
			return cache.canvas
		}
		canvas := rebuildBodyCanvas(cache, entries, width, height, hostEmojiMode, cursorOffsetY, coordinator.syntheticCursorVisible, runtimeState)
		cache.canvas = canvas
		cache.reset(entries, width, height)
		return canvas
	}
	if !cache.matches(entries, width, height, hostEmojiMode) {
		canvas := rebuildBodyCanvas(cache, entries, width, height, hostEmojiMode, cursorOffsetY, coordinator.syntheticCursorVisible, runtimeState)
		cache.canvas = canvas
		cache.reset(entries, width, height)
		return canvas
	}

	// Overlapping panes need a full rebuild. The cached active-pane refresh path
	// redraws the active pane content to clear the old cursor, which is correct
	// for tiled layouts but will paint over floating panes layered above it.
	// TODO(perf): If floating-window drag is still not smooth enough under heavy
	// styled content, prototype a damaged-rect path for non-overlapping floating
	// moves before doing more ANSI micro-optimizations. Re-profile first.
	if overlap {
		changed := false
		for _, entry := range entries {
			if cache.frameKeys[entry.PaneID] != entry.FrameKey || cache.contentKeys[entry.PaneID] != entry.ContentKey {
				changed = true
				break
			}
		}
		cache.canvas.clearCursor()
		if !changed {
			projectActiveEntryCursor(cache.canvas, entries, runtimeState)
			return cache.canvas
		}
		canvas := rebuildBodyCanvas(cache, entries, width, height, hostEmojiMode, cursorOffsetY, coordinator.syntheticCursorVisible, runtimeState)
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
				drawPaneContentFromCache(cache.canvas, cache, entry, runtimeState, true)
				changed = true
			}
		}
		restoreActiveEntryContent(cache.canvas, cache, entries, runtimeState)
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
		drawPaneContentFromCache(canvas, cache, entry, runtimeState, false)
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
			return workbench.Rect{}, false
		}
		if rectChanged {
			dirty = unionRects(dirty, oldRect)
			dirty = unionRects(dirty, entry.Rect)
		} else {
			dirty = unionRects(dirty, entry.Rect)
		}
		if !entry.Floating && dirty.W*dirty.H >= canvasArea*7/10 {
			return workbench.Rect{}, false
		}
	}
	dirty, ok := clipRectToViewport(dirty, width, height)
	if !ok {
		return workbench.Rect{}, false
	}
	if dirty.W*dirty.H >= canvasArea*9/10 {
		return workbench.Rect{}, false
	}
	return dirty, changed > 0
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
