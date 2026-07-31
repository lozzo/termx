package app

import (
	"github.com/anytty/anytty/tui/render"
	"github.com/anytty/anytty/tui/state"
)

type liveSurfacePatchState struct {
	Pending    liveSurfacePatchPending
	Regions    []render.LiveRenderRegion
	Metadata   render.RenderMetadata
	Theme      render.Theme
	Cursor     render.Cursor
	CursorRect render.Rect
}

type liveSurfacePatchPending struct {
	Valid        bool
	Ref          state.TerminalRef
	BaseRevision uint64
	Revision     uint64
	ChangedRows  []int
}

func (runtime *AppRuntime) noteRenderPending(msg Msg) {
	candidate, candidateOK := liveSurfacePatchPendingFromMessage(msg)
	if runtime.renderPending {
		if !runtime.liveSurfacePatch.Pending.Valid || !candidateOK || !runtime.liveSurfacePatch.Pending.Ref.Equal(candidate.Ref) ||
			runtime.liveSurfacePatch.Pending.BaseRevision != candidate.BaseRevision || runtime.liveSurfacePatch.Pending.Revision != candidate.Revision {
			runtime.liveSurfacePatch.Pending = liveSurfacePatchPending{}
		}
	} else if candidateOK {
		runtime.liveSurfacePatch.Pending = candidate
	} else {
		runtime.liveSurfacePatch.Pending = liveSurfacePatchPending{}
	}
	runtime.renderPending = true
}

func liveSurfacePatchPendingFromMessage(msg Msg) (liveSurfacePatchPending, bool) {
	liveEvent, ok := msg.(LiveEventMsg)
	if !ok || !liveEvent.Event.Ready {
		return liveSurfacePatchPending{}, false
	}
	snapshot := liveEvent.Event.Snapshot
	if snapshot.BaseRevision == 0 || snapshot.FullReplace || snapshot.Revision <= snapshot.BaseRevision {
		return liveSurfacePatchPending{}, false
	}
	endpointID := snapshot.EndpointID
	if endpointID == "" {
		endpointID = liveEvent.Event.EndpointID
	}
	terminalID := snapshot.TerminalID
	if terminalID == "" {
		terminalID = liveEvent.Event.TerminalID
	}
	ref := state.NewTerminalRef(endpointID, terminalID)
	if ref.Empty() {
		return liveSurfacePatchPending{}, false
	}
	return liveSurfacePatchPending{
		Valid:        true,
		Ref:          ref,
		BaseRevision: snapshot.BaseRevision,
		Revision:     snapshot.Revision,
		ChangedRows:  append([]int(nil), snapshot.ChangedRows...),
	}, true
}

func (runtime *AppRuntime) rememberLiveSurfacePatchFrame(frame render.Frame) {
	runtime.liveSurfacePatch.Regions = append(runtime.liveSurfacePatch.Regions[:0], frame.LiveRegions...)
	runtime.liveSurfacePatch.Metadata = frame.Metadata
	runtime.liveSurfacePatch.Theme = frame.Theme
	runtime.liveSurfacePatch.Cursor = frame.Cursor
	runtime.liveSurfacePatch.CursorRect = frame.CursorRect
}

func (runtime *AppRuntime) tryRenderLiveSurfacePatch() bool {
	pending := runtime.liveSurfacePatch.Pending
	if !pending.Valid || !runtime.canUseIncompleteFrameSink() || runtime.liveSurfacePatch.Metadata.Width != runtime.state.Viewport.Cols || runtime.liveSurfacePatch.Metadata.Height != runtime.state.Viewport.Rows {
		return false
	}
	region, ok := runtime.liveSurfacePatchRegion(pending)
	if !ok {
		return false
	}
	surface := runtime.state.Surface.SurfaceForTerminalRef(pending.Ref)
	if surface.Revision != pending.Revision || surface.Cols != region.Rect.W || surface.Rows != region.Rect.H || len(surface.Screen) < region.Rect.H {
		return false
	}

	frame := render.Frame{
		Cursor:     runtime.liveSurfacePatch.Cursor,
		CursorRect: runtime.liveSurfacePatch.CursorRect,
		Metadata:   runtime.liveSurfacePatch.Metadata,
		Theme:      runtime.liveSurfacePatch.Theme,
	}
	frame.Cursor, frame.CursorRect = liveSurfacePatchCursor(region, surface, frame.Cursor, frame.CursorRect)
	if len(pending.ChangedRows) == 0 {
		frame.Patch = &render.FramePatch{CursorOnly: true}
	} else {
		firstRow, lastRow, ok := liveSurfacePatchRowRange(pending.ChangedRows, region.Rect.H)
		if !ok {
			return false
		}
		lines := make([]string, lastRow-firstRow+1)
		for row := firstRow; row <= lastRow; row++ {
			lines[row-firstRow] = render.TerminalLiveRowANSI(surface.Screen[row], region.Rect.W, frame.Theme)
		}
		frame.Patch = &render.FramePatch{
			Rect:      region.Rect,
			Rewrite:   true,
			LineY:     region.Rect.Y + firstRow,
			LineX:     region.Rect.X,
			LineWidth: region.Rect.W,
			LinesANSI: lines,
		}
	}

	done := runtime.writeFrame(frame)
	runtime.firstFrameWritten = true
	runtime.observeRuntimePatchFrame(frame)
	runtime.liveSurfacePatch.Cursor = frame.Cursor
	runtime.liveSurfacePatch.CursorRect = frame.CursorRect
	for index := range runtime.liveSurfacePatch.Regions {
		if liveSurfacePatchRegionMatches(runtime.liveSurfacePatch.Regions[index], pending.Ref) {
			runtime.liveSurfacePatch.Regions[index].Revision = pending.Revision
		}
	}
	runtime.trackFrameCompletion([]liveFrameReadyTarget{{
		EndpointID:       pending.Ref.EndpointID,
		TerminalID:       pending.Ref.TerminalID,
		ObservedRevision: pending.Revision,
	}}, done)
	return true
}

func (runtime *AppRuntime) liveSurfacePatchRegion(pending liveSurfacePatchPending) (render.LiveRenderRegion, bool) {
	var matched render.LiveRenderRegion
	count := 0
	for _, region := range runtime.liveSurfacePatch.Regions {
		if !liveSurfacePatchRegionMatches(region, pending.Ref) || region.Revision != pending.BaseRevision {
			continue
		}
		matched = region
		count++
	}
	return matched, count == 1
}

func liveSurfacePatchRegionMatches(region render.LiveRenderRegion, ref state.TerminalRef) bool {
	return state.NewTerminalRef(state.EndpointID(region.EndpointID), region.TerminalID).Equal(ref)
}

func liveSurfacePatchRowRange(rows []int, height int) (int, int, bool) {
	first := height
	last := -1
	for _, row := range rows {
		if row < 0 || row >= height {
			return 0, 0, false
		}
		if row < first {
			first = row
		}
		if row > last {
			last = row
		}
	}
	return first, last, last >= first
}

func liveSurfacePatchCursor(region render.LiveRenderRegion, surface state.TerminalSurfaceStore, fallback render.Cursor, fallbackRect render.Rect) (render.Cursor, render.Rect) {
	if !region.Active {
		return fallback, fallbackRect
	}
	shape := render.CursorShapeBlock
	if surface.Cursor.Shape == string(render.CursorShapeBar) {
		shape = render.CursorShapeBar
	}
	if surface.Cursor.Visible && surface.Cursor.Row >= 0 && surface.Cursor.Row < region.Rect.H && surface.Cursor.Col >= 0 && surface.Cursor.Col < region.Rect.W {
		return render.Cursor{Visible: true, Row: surface.Cursor.Row, Col: surface.Cursor.Col, Shape: shape}, render.Rect{
			X: region.Rect.X + surface.Cursor.Col,
			Y: region.Rect.Y + surface.Cursor.Row,
			W: 1,
			H: 1,
		}
	}
	return render.Cursor{Anchor: true, Shape: render.CursorShapeBar}, render.Rect{X: region.Rect.X, Y: region.Rect.Y, W: 1, H: 1}
}
