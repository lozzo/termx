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
	resolved := resolvePaneContent(entry, runtimeState, true)
	window := buildTerminalSourceWindowState(resolved.source, resolved.contentRect.H, resolved.renderOffset)
	extentHash := terminalSourceExtentHash(resolved.source, resolved.contentRect, entry.Theme)
	if c.contentSprites == nil {
		c.contentSprites = make(map[string]*paneContentSpriteCacheEntry)
	}
	if cached := c.contentSprites[entry.PaneID]; cached != nil && cached.key == key && cached.canvas != nil {
		perftrace.Count("render.pane_content_sprite.hit", interior.W*interior.H)
		return cached.canvas
	}
	if cached := c.contentSprites[entry.PaneID]; cached != nil && cached.canvas != nil && canIncrementalPaneSpriteUpdate(cached, key, resolved, window, extentHash) {
		changedRows := applyIncrementalPaneSpriteRows(cached.canvas, resolved, entry.Theme, cached.window, window)
		cached.key = key
		cached.window = window
		cached.contentRect = resolved.contentRect
		cached.extentHash = extentHash
		perftrace.Count("render.pane_content_sprite.incremental", changedRows*maxInt(1, resolved.contentRect.W))
		perftrace.Count("render.pane_content_sprite.miss", interior.W*interior.H)
		return cached.canvas
	}
	var sprite *composedCanvas
	if cached := c.contentSprites[entry.PaneID]; cached != nil && cached.canvas != nil && cached.key.Width == key.Width && cached.key.Height == key.Height {
		sprite = cached.canvas
		sprite.resetToBlank()
	} else {
		sprite = newComposedCanvas(interior.W, interior.H)
	}
	drawPaneContentSprite(sprite, entry, runtimeState)
	c.contentSprites[entry.PaneID] = &paneContentSpriteCacheEntry{
		key:         key,
		canvas:      sprite,
		window:      window,
		contentRect: resolved.contentRect,
		extentHash:  extentHash,
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
	if previousStatic != nextStatic {
		return false
	}
	if len(nextWindow.rowHashes) == 0 || len(nextWindow.rowHashes) != len(cached.window.rowHashes) || len(nextWindow.rowIndices) != len(cached.window.rowIndices) {
		return false
	}
	return true
}

func applyIncrementalPaneSpriteRows(canvas *composedCanvas, resolved resolvedPaneContent, theme uiTheme, previous, next terminalSourceWindowState) int {
	if canvas == nil || resolved.source == nil || resolved.contentRect.W <= 0 || resolved.contentRect.H <= 0 {
		return 0
	}
	changedRows := 0
	for line := range next.rowHashes {
		if line < len(previous.rowHashes) && previous.rowHashes[line] == next.rowHashes[line] {
			continue
		}
		targetY := resolved.contentRect.Y + line
		rowIndex := -1
		if line < len(next.rowIndices) {
			rowIndex = next.rowIndices[line]
		}
		drawPaneContentSpriteRow(canvas, resolved.contentRect, resolved.source, rowIndex, targetY, theme)
		changedRows++
	}
	return changedRows
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
	keepSprites := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		c.order = append(c.order, entry.PaneID)
		c.rects[entry.PaneID] = entry.Rect
		c.frameKeys[entry.PaneID] = entry.FrameKey
		c.contentKeys[entry.PaneID] = entry.ContentKey
		keepSprites[entry.PaneID] = struct{}{}
	}
	if c.contentSprites != nil {
		for paneID := range c.contentSprites {
			if _, ok := keepSprites[paneID]; !ok {
				delete(c.contentSprites, paneID)
			}
		}
	}
}

func (c *bodyRenderCache) matches(entries []paneRenderEntry, width, height int, hostEmojiMode shared.AmbiguousEmojiVariationSelectorMode) bool {
	if c == nil || c.canvas == nil || c.width != width || c.height != height || c.hostEmojiVS16Mode != hostEmojiMode || len(c.order) != len(entries) {
		return false
	}
	for i, entry := range entries {
		if c.order[i] != entry.PaneID || c.rects[entry.PaneID] != entry.Rect {
			return false
		}
	}
	return true
}
