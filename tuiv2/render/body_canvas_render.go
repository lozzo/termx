package render

import (
	"github.com/lozzow/termx/perftrace"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func renderBodyCanvas(coordinator *Coordinator, state VisibleRenderState, entries []paneRenderEntry, width, height int) *composedCanvas {
	finish := perftrace.Measure("render.body.canvas")
	defer finish(0)
	immersiveZoom := immersiveZoomActive(state)
	hostEmojiMode := emojiVariationSelectorModeForRuntime(state.Runtime)
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
			drawPaneContentWithKey(canvas, entry.Rect, entry, state.Runtime)
		}
		projectActiveEntryCursor(canvas, entries, state.Runtime)
		return canvas
	}
	cache := coordinator.bodyCache
	overlap := entriesOverlap(entries)
	if cache == nil || !cache.matches(entries, width, height, hostEmojiMode) {
		canvas := rebuildBodyCanvas(cache, entries, width, height, hostEmojiMode, cursorOffsetY, coordinator.syntheticCursorVisible, state.Runtime)
		coordinator.bodyCache = newBodyRenderCache(cache, canvas, entries, width, height)
		return canvas
	}

	// Overlapping panes need a full rebuild. The cached active-pane refresh path
	// redraws the active pane content to clear the old cursor, which is correct
	// for tiled layouts but will paint over floating panes layered above it.
	// TODO(perf): If floating-window drag is still not smooth enough under heavy
	// styled content, prototype a damaged-rect path for non-overlapping floating
	// moves before doing more ANSI micro-optimizations. Re-profile first.
	if overlap {
		canvas := rebuildBodyCanvas(cache, entries, width, height, hostEmojiMode, cursorOffsetY, coordinator.syntheticCursorVisible, state.Runtime)
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
				drawPaneContentFromCache(cache.canvas, cache, entry, state.Runtime, true)
				changed = true
			}
		}
		restoreActiveEntryContent(cache.canvas, entries, state.Runtime)
		if changed {
			projectActiveEntryCursor(cache.canvas, entries, state.Runtime)
			cache.reset(entries, width, height)
			return cache.canvas
		}
		projectActiveEntryCursor(cache.canvas, entries, state.Runtime)
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
	if canvas == nil {
		return
	}
	fillRect(canvas, workbench.Rect{W: canvas.width, H: canvas.height}, blankDrawCell())
	contentRect := localContentRectForEntry(entry)
	if contentRect.W <= 0 || contentRect.H <= 0 {
		return
	}
	if entry.TerminalID == "" {
		drawEmptyPaneContent(canvas, contentRect, entry.PaneID, entry.TerminalID, entry.Theme, entry.EmptyActionSelected)
		return
	}
	terminal := findVisibleTerminal(runtimeState, entry.TerminalID)
	if terminal == nil {
		drawEmptyPaneContent(canvas, contentRect, entry.PaneID, entry.TerminalID, entry.Theme, -1)
		return
	}
	snapshot := entry.Snapshot
	surface := entry.Surface
	if snapshot == nil && surface == nil {
		surface = terminal.Surface
	}
	if snapshot == nil && surface == nil {
		snapshot = terminal.Snapshot
	}
	source := renderSource(snapshot, surface)
	if source == nil || source.ScreenRows() == 0 {
		canvas.drawText(contentRect.X, contentRect.Y, terminal.Name+" ["+terminal.State+"]", drawStyle{FG: entry.Theme.panelMuted})
		if terminal.State == "exited" {
			drawExitedPaneRecoveryHints(canvas, contentRect, entry.Theme, entry.ExitedActionSelected, entry.ExitedActionPulse)
		}
		return
	}
	renderOffset := entry.ScrollOffset
	if entry.CopyModeActive {
		renderOffset = scrollOffsetForViewportTop(snapshot, contentRect.H, entry.CopyModeViewTopRow)
	}
	drawTerminalSourceWithOffset(canvas, contentRect, source, renderOffset, entry.Theme)
	if entry.CopyModeActive {
		drawCopyModeOverlay(canvas, contentRect, snapshot, entry.Theme, entry.CopyModeCursorRow, entry.CopyModeCursorCol, entry.CopyModeViewTopRow, entry.CopyModeMarkSet, entry.CopyModeMarkRow, entry.CopyModeMarkCol)
	}
	if terminal.State == "exited" {
		drawExitedPaneRecoveryHints(canvas, contentRect, entry.Theme, entry.ExitedActionSelected, entry.ExitedActionPulse)
	}
}

func restoreActiveEntryContent(canvas *composedCanvas, entries []paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) {
	if canvas == nil {
		return
	}
	for _, entry := range entries {
		if !entry.Active {
			continue
		}
		base := entry
		base.Active = false
		drawPaneContentWithKey(canvas, entry.Rect, base, runtimeState)
		return
	}
}
