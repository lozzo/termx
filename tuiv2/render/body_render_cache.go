package render

import "github.com/lozzow/termx/tuiv2/workbench"

type bodyRenderCacheCursor struct {
	PaneID string
	Line   int
}

type bodyRenderCacheEntry struct {
	PaneID       string
	OwnerID      uint32
	Theme        uiTheme
	FrameKey     paneFrameKey
	ContentKey   paneContentKey
	ContentRect  workbench.Rect
	Metrics      renderTerminalMetrics
	ScreenUpdate terminalScreenUpdateHint
	Window       terminalSourceWindowState
	HasWindow    bool
}

type capturedBodyRenderCacheEntry struct {
	cache    bodyRenderCacheEntry
	resolved resolvedPaneContent
}

type bodyRenderCachePreview struct {
	entry  bodyRenderCacheEntry
	sprite *composedCanvas
	valid  bool
}

type bodyRenderCache struct {
	width           int
	height          int
	canvas          *composedCanvas
	entries         []bodyRenderCacheEntry
	activeCursor    bodyRenderCacheCursor
	hasActiveCursor bool
	overlap         bool
	preview         bodyRenderCachePreview
}

func captureBodyRenderCacheEntries(entries []paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) []capturedBodyRenderCacheEntry {
	captured := make([]capturedBodyRenderCacheEntry, len(entries))
	for i, entry := range entries {
		captured[i] = captureBodyRenderCacheEntry(entry, runtimeState)
	}
	return captured
}

func captureBodyRenderCacheEntry(entry paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) capturedBodyRenderCacheEntry {
	resolved := resolvePaneContent(entry, runtimeState, false)
	captured := capturedBodyRenderCacheEntry{
		cache: bodyRenderCacheEntry{
			PaneID:       entry.PaneID,
			OwnerID:      entry.OwnerID,
			Theme:        entry.Theme,
			FrameKey:     entry.FrameKey,
			ContentKey:   entry.ContentKey,
			ContentRect:  resolved.contentRect,
			Metrics:      resolved.metrics,
			ScreenUpdate: effectiveTerminalScreenUpdateHint(resolved.screenUpdate, resolved.surface != nil, entry.SurfaceVersion),
		},
		resolved: resolved,
	}
	if resolved.source != nil && resolved.contentRect.H > 0 {
		captured.cache.Window = buildTerminalSourceWindowState(resolved.source, resolved.contentRect.H, resolved.renderOffset)
		captured.cache.HasWindow = true
	}
	return captured
}

func captureBodyRenderCacheActiveCursor(entries []paneRenderEntry, runtimeState *VisibleRuntimeStateProxy) (bodyRenderCacheCursor, bool) {
	target, ok := activeEntryCursorRenderTarget(entries, runtimeState)
	if !ok {
		return bodyRenderCacheCursor{}, false
	}
	for _, entry := range entries {
		if !entry.Active {
			continue
		}
		contentRect := contentRectForEntry(entry)
		line := target.Y - contentRect.Y
		if line < 0 || line >= contentRect.H {
			return bodyRenderCacheCursor{}, false
		}
		return bodyRenderCacheCursor{
			PaneID: entry.PaneID,
			Line:   line,
		}, true
	}
	return bodyRenderCacheCursor{}, false
}

func newBodyRenderCache(canvas *composedCanvas, captured []capturedBodyRenderCacheEntry, entries []paneRenderEntry, runtimeState *VisibleRuntimeStateProxy, width, height int) *bodyRenderCache {
	cacheEntries := make([]bodyRenderCacheEntry, len(captured))
	for i, entry := range captured {
		cacheEntries[i] = entry.cache
	}
	activeCursor, hasActiveCursor := captureBodyRenderCacheActiveCursor(entries, runtimeState)
	return &bodyRenderCache{
		width:           width,
		height:          height,
		canvas:          canvas,
		entries:         cacheEntries,
		activeCursor:    activeCursor,
		hasActiveCursor: hasActiveCursor,
		overlap:         entriesOverlap(entries),
		preview:         bodyRenderCachePreview{},
	}
}
