package render

import (
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/shared"
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
	if coordinator != nil {
		if captured == nil {
			captured = captureBodyRenderCacheEntries(entries, runtimeState)
		}
		coordinator.bodyCache = newBodyRenderCache(canvas, captured, entries, runtimeState, width, height)
	}
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
