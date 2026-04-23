package render

import (
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func renderBodyCanvas(coordinator *Coordinator, runtimeState *VisibleRuntimeStateProxy, immersiveZoom bool, entries []paneRenderEntry, preview *paneRenderEntry, width, height int) *composedCanvas {
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
	var (
		canvas   *composedCanvas
		captured []capturedBodyRenderCacheEntry
	)
	if canIncrementallyUpdateBodyCanvas(cache, entries, width, height) {
		captured = captureBodyRenderCacheEntries(entries, runtimeState)
		if next, ok := updateBodyCanvasIncrementally(cache, entries, captured, runtimeState, hostEmojiMode, cursorOffsetY, cursorVisibleFn); ok {
			canvas = next
			perftrace.Count("render.body.canvas.path.incremental", maxInt(1, width*height))
		}
	}
	if canvas == nil {
		canvas = rebuildBodyCanvas(cache, entries, width, height, hostEmojiMode, cursorOffsetY, cursorVisibleFn, runtimeState)
		perftrace.Count("render.body.canvas.path.full_compose", maxInt(1, width*height))
	}
	outputCanvas := canvas
	if preview != nil {
		if coordinator != nil {
			outputCanvas = previewCanvasFromCache(cache, canvas, preview.Rect)
		} else {
			outputCanvas = cloneComposedCanvasForRect(canvas, preview.Rect)
		}
	}
	if coordinator != nil {
		if captured == nil {
			captured = captureBodyRenderCacheEntries(entries, runtimeState)
		}
		nextCache := newBodyRenderCache(canvas, captured, entries, runtimeState, width, height)
		if cache != nil {
			nextCache.preview = cache.preview
		}
		if preview != nil {
			applyBodyCanvasPreviewOverlay(outputCanvas, nextCache, *preview, runtimeState)
			perftrace.Count("render.body.canvas.path.preview_overlay", maxInt(1, preview.Rect.W*preview.Rect.H))
			nextCache.previewCanvas = outputCanvas
		} else {
			nextCache.previewCanvas = nil
		}
		coordinator.bodyCache = nextCache
		return outputCanvas
	}
	if preview != nil {
		applyBodyCanvasPreviewOverlay(outputCanvas, cache, *preview, runtimeState)
		perftrace.Count("render.body.canvas.path.preview_overlay", maxInt(1, preview.Rect.W*preview.Rect.H))
	}
	return outputCanvas
}

func previewCanvasFromCache(cache *bodyRenderCache, base *composedCanvas, currentRect workbench.Rect) *composedCanvas {
	if base == nil {
		return nil
	}
	if cache == nil || cache.previewCanvas == nil || cache.previewCanvas.width != base.width || cache.previewCanvas.height != base.height {
		return cloneComposedCanvas(base)
	}
	scratch := cache.previewCanvas
	union := currentRect
	if cache.preview.valid {
		union = unionWorkbenchRects(union, cache.preview.entry.FrameKey.Rect)
	}
	syncPreviewCanvasRows(scratch, base, union)
	return scratch
}

func syncPreviewCanvasRows(dst, src *composedCanvas, refreshRect workbench.Rect) {
	if dst == nil || src == nil {
		return
	}
	refreshRect, ok := clipRectToViewport(refreshRect, src.width, src.height)
	if !ok {
		refreshRect = workbench.Rect{}
	}
	dst.hostEmojiVS16Mode = src.hostEmojiVS16Mode
	dst.cursorPlaced = src.cursorPlaced
	dst.cursorVisible = src.cursorVisible
	dst.cursorX = src.cursorX
	dst.cursorY = src.cursorY
	dst.cursorOffsetX = src.cursorOffsetX
	dst.cursorOffsetY = src.cursorOffsetY
	dst.cursorShape = src.cursorShape
	dst.cursorBlink = src.cursorBlink
	dst.syntheticCursorBlink = src.syntheticCursorBlink
	dst.syntheticCursorVisibleFn = src.syntheticCursorVisibleFn
	dst.currentOwner = src.currentOwner
	dst.fullCache = ""
	dst.fullDirty = true
	for y := 0; y < src.height; y++ {
		if !src.rowDirty[y] && (y < refreshRect.Y || y >= refreshRect.Y+refreshRect.H) {
			continue
		}
		if len(dst.cells[y]) != len(src.cells[y]) {
			dst.cells[y] = make([]drawCell, len(src.cells[y]))
		}
		copy(dst.cells[y], src.cells[y])
		dst.rowCache[y] = src.rowCache[y]
		dst.rowDirty[y] = src.rowDirty[y]
		dst.rowDirtyMin[y] = src.rowDirtyMin[y]
		dst.rowDirtyMax[y] = src.rowDirtyMax[y]
		if len(src.rowChunks[y]) == 0 {
			dst.rowChunks[y] = nil
			continue
		}
		if cap(dst.rowChunks[y]) < len(src.rowChunks[y]) {
			dst.rowChunks[y] = make([]string, len(src.rowChunks[y]))
		} else {
			dst.rowChunks[y] = dst.rowChunks[y][:len(src.rowChunks[y])]
		}
		copy(dst.rowChunks[y], src.rowChunks[y])
	}
}

func applyBodyCanvasPreviewOverlay(canvas *composedCanvas, cache *bodyRenderCache, entry paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) {
	if canvas == nil {
		return
	}
	if sprite := previewSpriteFromCache(cache, entry); sprite != nil {
		canvas.blitRectFrom(sprite, workbenchRectForSprite(sprite), entry.Rect.X, entry.Rect.Y)
		return
	}
	sprite := buildPreviewSprite(entry, runtimeState)
	if sprite == nil {
		return
	}
	canvas.blitRectFrom(sprite, workbenchRectForSprite(sprite), entry.Rect.X, entry.Rect.Y)
	if cache != nil {
		cache.preview = bodyRenderCachePreview{entry: captureBodyRenderCacheEntry(entry, runtimeState).cache, sprite: sprite, valid: true}
	}
}

func cloneComposedCanvasForRect(base *composedCanvas, rect workbench.Rect) *composedCanvas {
	if base == nil {
		return nil
	}
	rect, ok := clipRectToViewport(rect, base.width, base.height)
	if !ok {
		rect = workbench.Rect{}
	}
	clone := &composedCanvas{
		width:                    base.width,
		height:                   base.height,
		cells:                    append([][]drawCell(nil), base.cells...),
		rowCache:                 append([]string(nil), base.rowCache...),
		rowDirty:                 append([]bool(nil), base.rowDirty...),
		rowDirtyMin:              append([]int(nil), base.rowDirtyMin...),
		rowDirtyMax:              append([]int(nil), base.rowDirtyMax...),
		rowChunks:                append([][]string(nil), base.rowChunks...),
		fullCache:                "",
		fullDirty:                true,
		hostEmojiVS16Mode:        base.hostEmojiVS16Mode,
		cursorPlaced:             base.cursorPlaced,
		cursorVisible:            base.cursorVisible,
		cursorX:                  base.cursorX,
		cursorY:                  base.cursorY,
		cursorOffsetX:            base.cursorOffsetX,
		cursorOffsetY:            base.cursorOffsetY,
		cursorShape:              base.cursorShape,
		cursorBlink:              base.cursorBlink,
		syntheticCursorBlink:     base.syntheticCursorBlink,
		syntheticCursorVisibleFn: base.syntheticCursorVisibleFn,
		currentOwner:             base.currentOwner,
	}
	for y := rect.Y; y < rect.Y+rect.H; y++ {
		clone.cells[y] = append([]drawCell(nil), base.cells[y]...)
		if len(base.rowChunks[y]) > 0 {
			clone.rowChunks[y] = append([]string(nil), base.rowChunks[y]...)
		}
	}
	return clone
}

func cloneComposedCanvas(base *composedCanvas) *composedCanvas {
	if base == nil {
		return nil
	}
	clone := cloneComposedCanvasForRect(base, workbench.Rect{X: 0, Y: 0, W: base.width, H: base.height})
	if clone != nil {
		clone.fullCache = base.fullCache
		clone.fullDirty = base.fullDirty
	}
	return clone
}

func unionWorkbenchRects(a, b workbench.Rect) workbench.Rect {
	switch {
	case a.W <= 0 || a.H <= 0:
		return b
	case b.W <= 0 || b.H <= 0:
		return a
	}
	left := minInt(a.X, b.X)
	top := minInt(a.Y, b.Y)
	right := maxInt(a.X+a.W, b.X+b.W)
	bottom := maxInt(a.Y+a.H, b.Y+b.H)
	return workbench.Rect{X: left, Y: top, W: right - left, H: bottom - top}
}

func previewSpriteFromCache(cache *bodyRenderCache, entry paneRenderEntry) *composedCanvas {
	if cache == nil || !cache.preview.valid || cache.preview.sprite == nil {
		return nil
	}
	captured := captureBodyRenderCacheEntry(entry, nil).cache
	if cache.preview.entry.FrameKey != captured.FrameKey || cache.preview.entry.ContentKey != captured.ContentKey || cache.preview.entry.Metrics != captured.Metrics {
		return nil
	}
	return cache.preview.sprite
}

func buildPreviewSprite(entry paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) *composedCanvas {
	if entry.Rect.W <= 0 || entry.Rect.H <= 0 {
		return nil
	}
	sprite := newComposedCanvas(entry.Rect.W, entry.Rect.H)
	localEntry := entry
	localEntry.Rect = workbench.Rect{X: 0, Y: 0, W: entry.Rect.W, H: entry.Rect.H}
	localEntry.FrameKey.Rect = localEntry.Rect
	sprite.withOwner(localEntry.OwnerID, func() {
		drawPaneFrame(sprite, localEntry.Rect, localEntry.SharedLeft, localEntry.SharedTop, localEntry.Title, localEntry.Border, localEntry.Theme, localEntry.Overflow, localEntry.Active, localEntry.Floating, localEntry.Chrome)
	})
	sprite.withOwner(localEntry.OwnerID, func() {
		drawPaneContentWithKey(sprite, localEntry.Rect, localEntry, runtimeState)
	})
	return sprite
}

func workbenchRectForSprite(sprite *composedCanvas) workbench.Rect {
	if sprite == nil {
		return workbench.Rect{}
	}
	return workbench.Rect{X: 0, Y: 0, W: sprite.width, H: sprite.height}
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
				drawPaneFrame(canvas, entry.Rect, entry.SharedLeft, entry.SharedTop, entry.Title, entry.Border, entry.Theme, entry.Overflow, entry.Active, entry.Floating, entry.Chrome)
			})
		}
		canvas.withOwner(entry.OwnerID, func() {
			drawPaneContentWithKey(canvas, entry.Rect, entry, runtimeState)
		})
	}
	projectActiveEntryCursor(canvas, entries, runtimeState)
	return canvas
}
