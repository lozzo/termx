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
