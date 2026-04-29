package render

import (
	"sort"

	"github.com/lozzow/termx/termx-core/perftrace"
	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/tuiv2/shared"
)

type bodyCanvasUpdateMode uint8

const (
	bodyCanvasUpdateNone bodyCanvasUpdateMode = iota
	bodyCanvasUpdateRows
	bodyCanvasUpdateFullPane
)

type bodyCanvasIncrementalUpdate struct {
	entry      paneRenderEntry
	captured   capturedBodyRenderCacheEntry
	mode       bodyCanvasUpdateMode
	scrollPlan terminalWindowScrollPlan
	rows       []int
}

func canIncrementallyUpdateBodyCanvas(cache *bodyRenderCache, entries []paneRenderEntry, width, height int) bool {
	if cache == nil || cache.canvas == nil || cache.width != width || cache.height != height || cache.overlap || entriesOverlap(entries) {
		return false
	}
	if len(cache.entries) != len(entries) {
		return false
	}
	for i, entry := range entries {
		prev := cache.entries[i]
		if prev.PaneID != entry.PaneID || prev.OwnerID != entry.OwnerID || prev.FrameKey != entry.FrameKey || prev.Theme != entry.Theme {
			return false
		}
	}
	return true
}

func updateBodyCanvasIncrementally(cache *bodyRenderCache, entries []paneRenderEntry, captured []capturedBodyRenderCacheEntry, runtimeState *VisibleRuntimeStateProxy, hostEmojiMode shared.AmbiguousEmojiVariationSelectorMode, cursorOffsetY int, cursorVisibleFn func(protocol.CursorState) bool) (*composedCanvas, bool) {
	if !canIncrementallyUpdateBodyCanvas(cache, entries, cache.width, cache.height) || len(captured) != len(entries) {
		return nil, false
	}
	updates, ok := buildBodyCanvasIncrementalUpdates(cache, entries, captured, runtimeState)
	if !ok {
		return nil, false
	}
	canvas := cache.canvas
	canvas.hostEmojiVS16Mode = hostEmojiMode
	canvas.cursorOffsetY = cursorOffsetY
	canvas.syntheticCursorVisibleFn = cursorVisibleFn
	applyBodyCanvasIncrementalUpdates(canvas, updates, runtimeState)
	projectActiveEntryCursor(canvas, entries, runtimeState)
	return canvas, true
}

func buildBodyCanvasIncrementalUpdates(cache *bodyRenderCache, entries []paneRenderEntry, captured []capturedBodyRenderCacheEntry, runtimeState *VisibleRuntimeStateProxy) ([]bodyCanvasIncrementalUpdate, bool) {
	if cache == nil || len(cache.entries) != len(entries) || len(captured) != len(entries) {
		return nil, false
	}
	updates := make([]bodyCanvasIncrementalUpdate, len(entries))
	for i, entry := range entries {
		prev := cache.entries[i]
		next := captured[i]
		update := bodyCanvasIncrementalUpdate{
			entry:    entry,
			captured: next,
		}
		if prev.ContentKey != next.cache.ContentKey {
			switch {
			case !prev.HasWindow || !next.cache.HasWindow:
				update.mode = bodyCanvasUpdateFullPane
			case prev.ContentRect != next.cache.ContentRect || prev.Metrics != next.cache.Metrics:
				update.mode = bodyCanvasUpdateFullPane
			case next.resolved.source == nil:
				update.mode = bodyCanvasUpdateFullPane
			default:
				if next.cache.ScreenUpdate.ScreenScroll == 0 {
					update.mode = bodyCanvasUpdateFullPane
					break
				}
				plan := planTerminalWindowDelta(prev.Window, next.cache.Window, next.cache.ScreenUpdate)
				update.scrollPlan = plan.scrollPlan
				update.rows = append(update.rows, plan.changedRows...)
				if update.scrollPlan.valid(next.cache.ContentRect.H) {
					update.mode = bodyCanvasUpdateRows
				} else {
					update.mode = bodyCanvasUpdateFullPane
				}
			}
		}
		updates[i] = update
	}

	if cache.hasActiveCursor {
		if !addIncrementalCursorRow(updates, cache.activeCursor) {
			return nil, false
		}
	}
	if currentCursor, ok := captureBodyRenderCacheActiveCursor(entries, runtimeState); ok {
		if !addIncrementalCursorRow(updates, currentCursor) {
			return nil, false
		}
	}

	changed := false
	for i := range updates {
		if updates[i].mode == bodyCanvasUpdateRows {
			updates[i].rows = compactSortedRows(updates[i].rows, updates[i].captured.cache.ContentRect.H)
			if len(updates[i].rows) == 0 && !updates[i].scrollPlan.valid(updates[i].captured.cache.ContentRect.H) {
				updates[i].mode = bodyCanvasUpdateNone
			}
		}
		if updates[i].mode != bodyCanvasUpdateNone {
			changed = true
		}
	}
	return updates, changed
}

func addIncrementalCursorRow(updates []bodyCanvasIncrementalUpdate, cursor bodyRenderCacheCursor) bool {
	if cursor.PaneID == "" || cursor.Line < 0 {
		return true
	}
	for i := range updates {
		if updates[i].entry.PaneID != cursor.PaneID {
			continue
		}
		if updates[i].mode == bodyCanvasUpdateFullPane {
			return true
		}
		if !updates[i].captured.cache.HasWindow || updates[i].captured.resolved.source == nil || cursor.Line >= updates[i].captured.cache.ContentRect.H {
			updates[i].mode = bodyCanvasUpdateFullPane
			updates[i].rows = nil
			updates[i].scrollPlan = terminalWindowScrollPlan{}
			return true
		}
		updates[i].mode = bodyCanvasUpdateRows
		updates[i].rows = append(updates[i].rows, cursor.Line)
		return true
	}
	return false
}

func compactSortedRows(rows []int, height int) []int {
	if len(rows) == 0 || height <= 0 {
		return nil
	}
	filtered := rows[:0]
	for _, row := range rows {
		if row < 0 || row >= height {
			continue
		}
		filtered = append(filtered, row)
	}
	if len(filtered) == 0 {
		return nil
	}
	sort.Ints(filtered)
	out := filtered[:1]
	for _, row := range filtered[1:] {
		if row == out[len(out)-1] {
			continue
		}
		out = append(out, row)
	}
	return out
}

func applyBodyCanvasIncrementalUpdates(canvas *composedCanvas, updates []bodyCanvasIncrementalUpdate, runtimeState *VisibleRuntimeStateProxy) {
	if canvas == nil {
		return
	}
	for _, update := range updates {
		switch update.mode {
		case bodyCanvasUpdateNone:
			continue
		case bodyCanvasUpdateFullPane:
			perftrace.Count("render.body.canvas.incremental.full_pane", maxInt(1, update.entry.Rect.W*update.entry.Rect.H))
			canvas.withOwner(update.entry.OwnerID, func() {
				fillRect(canvas, interiorRectForEntry(update.entry), blankDrawCell())
				drawPaneContentWithKey(canvas, update.entry.Rect, update.entry, runtimeState)
			})
		case bodyCanvasUpdateRows:
			if update.captured.resolved.source == nil {
				perftrace.Count("render.body.canvas.incremental.full_pane", maxInt(1, update.entry.Rect.W*update.entry.Rect.H))
				canvas.withOwner(update.entry.OwnerID, func() {
					fillRect(canvas, interiorRectForEntry(update.entry), blankDrawCell())
					drawPaneContentWithKey(canvas, update.entry.Rect, update.entry, runtimeState)
				})
				continue
			}
			contentRect := update.captured.cache.ContentRect
			damage := len(update.rows) * maxInt(1, contentRect.W)
			if update.scrollPlan.valid(contentRect.H) {
				damage += maxInt(1, contentRect.W*(update.scrollPlan.end-update.scrollPlan.start+1))
			}
			perftrace.Count("render.body.canvas.incremental.rows", maxInt(1, damage))
			canvas.withOwner(update.entry.OwnerID, func() {
				if update.scrollPlan.valid(contentRect.H) {
					canvas.shiftRectRowBand(contentRect, update.scrollPlan.start, update.scrollPlan.end, update.scrollPlan.shift, update.scrollPlan.direction)
				}
				drawTerminalSourceWindowRowsWithMetrics(canvas, contentRect, update.captured.resolved.source, update.captured.resolved.renderOffset, update.entry.Theme, update.captured.cache.Metrics, update.rows)
			})
		}
	}
}
