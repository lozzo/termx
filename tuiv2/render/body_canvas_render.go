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
	if preview != nil {
		applyBodyCanvasPreviewOverlay(canvas, cache, *preview, runtimeState)
		perftrace.Count("render.body.canvas.path.preview_overlay", maxInt(1, preview.Rect.W*preview.Rect.H))
	}
	if coordinator != nil {
		if captured == nil {
			captured = captureBodyRenderCacheEntries(entries, runtimeState)
		}
		nextCache := newBodyRenderCache(canvas, captured, entries, runtimeState, width, height)
		if cache != nil {
			nextCache.preview = cache.preview
		}
		coordinator.bodyCache = nextCache
	}
	return canvas
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
