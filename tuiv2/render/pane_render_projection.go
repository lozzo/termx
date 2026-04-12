package render

import (
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/runtime"
	"github.com/lozzow/termx/tuiv2/workbench"
)

type paneRenderEntry struct {
	PaneID               string
	Rect                 workbench.Rect
	Frameless            bool
	SharedLeft           bool
	SharedTop            bool
	Title                string
	Border               paneBorderInfo
	Theme                uiTheme
	Overflow             paneOverflowHints
	ContentKey           paneContentKey
	FrameKey             paneFrameKey
	TerminalID           string
	Snapshot             *protocol.Snapshot
	Surface              runtime.TerminalSurface
	SurfaceVersion       uint64
	ScrollOffset         int
	Active               bool
	Floating             bool
	EmptyActionSelected  int
	ExitedActionSelected int
	ExitedActionPulse    bool
	CopyModeActive       bool
	CopyModeCursorRow    int
	CopyModeCursorCol    int
	CopyModeViewTopRow   int
	CopyModeMarkSet      bool
	CopyModeMarkRow      int
	CopyModeMarkCol      int
}

type paneFrameKey struct {
	Rect            workbench.Rect
	Frameless       bool
	SharedLeft      bool
	SharedTop       bool
	Title           string
	Border          paneBorderInfo
	ThemeBG         string
	Overflow        paneOverflowHints
	Active          bool
	Floating        bool
	ChromeSignature string
}

type paneOverflowHints struct {
	Right  bool
	Bottom bool
}

type renderTerminalMetrics struct {
	Cols int
	Rows int
}

func paneEntriesForTab(tab workbench.VisibleTab, floating []workbench.VisiblePane, width, height int, lookup runtimeLookup, confirmPaneID, emptyPaneSelectionPaneID string, emptyPaneSelectionIndex int, exitedPaneSelectionPaneID string, exitedPaneSelectionIndex int, exitedPaneSelectionPulse bool, state VisibleRenderState, theme uiTheme) []paneRenderEntry {
	entries := make([]paneRenderEntry, 0, len(tab.Panes)+len(floating))
	zoomedPaneID := tab.ZoomedPaneID
	immersiveZoom := immersiveZoomActive(state)
	for _, pane := range tab.Panes {
		originalRect := pane.Rect
		rect := originalRect
		frameless := false
		if zoomedPaneID != "" {
			if pane.ID != zoomedPaneID {
				continue
			}
			originalRect = workbench.Rect{X: 0, Y: 0, W: width, H: height}
			rect = workbench.Rect{X: 0, Y: 0, W: width, H: height}
			frameless = immersiveZoom
		}
		rect, ok := clipRectToViewport(rect, width, height)
		if !ok {
			continue
		}
		entries = append(entries, buildPaneRenderEntry(pane, originalRect, rect, frameless, tab.ActivePaneID, tab.ScrollOffset, lookup, confirmPaneID, emptyPaneSelectionPaneID, emptyPaneSelectionIndex, exitedPaneSelectionPaneID, exitedPaneSelectionIndex, exitedPaneSelectionPulse, state.PaneSnapshotOverridePaneID, state.PaneSnapshotOverride, state.CopyModePaneID, state.CopyModeCursorRow, state.CopyModeCursorCol, state.CopyModeViewTopRow, state.CopyModeMarkSet, state.CopyModeMarkRow, state.CopyModeMarkCol, state.CopyModeSnapshot, theme))
	}
	for _, pane := range floating {
		originalRect := pane.Rect
		rect, ok := clipRectToViewport(originalRect, width, height)
		if !ok {
			continue
		}
		entries = append(entries, buildPaneRenderEntry(pane, originalRect, rect, false, tab.ActivePaneID, tab.ScrollOffset, lookup, confirmPaneID, emptyPaneSelectionPaneID, emptyPaneSelectionIndex, exitedPaneSelectionPaneID, exitedPaneSelectionIndex, exitedPaneSelectionPulse, state.PaneSnapshotOverridePaneID, state.PaneSnapshotOverride, state.CopyModePaneID, state.CopyModeCursorRow, state.CopyModeCursorCol, state.CopyModeViewTopRow, state.CopyModeMarkSet, state.CopyModeMarkRow, state.CopyModeMarkCol, state.CopyModeSnapshot, theme))
	}
	return entries
}

func clipRectToViewport(rect workbench.Rect, width, height int) (workbench.Rect, bool) {
	if rect.W <= 0 || rect.H <= 0 || width <= 0 || height <= 0 {
		return workbench.Rect{}, false
	}
	x1 := maxInt(rect.X, 0)
	y1 := maxInt(rect.Y, 0)
	x2 := minInt(rect.X+rect.W, width)
	y2 := minInt(rect.Y+rect.H, height)
	if x1 >= x2 || y1 >= y2 {
		return workbench.Rect{}, false
	}
	return workbench.Rect{X: x1, Y: y1, W: x2 - x1, H: y2 - y1}, true
}

func buildPaneRenderEntry(pane workbench.VisiblePane, originalRect, rect workbench.Rect, frameless bool, activePaneID string, scrollOffset int, lookup runtimeLookup, confirmPaneID, emptyPaneSelectionPaneID string, emptyPaneSelectionIndex int, exitedPaneSelectionPaneID string, exitedPaneSelectionIndex int, exitedPaneSelectionPulse bool, paneSnapshotOverridePaneID string, paneSnapshotOverride *protocol.Snapshot, copyModePaneID string, copyModeCursorRow, copyModeCursorCol, copyModeViewTopRow int, copyModeMarkSet bool, copyModeMarkRow, copyModeMarkCol int, copyModeSnapshot *protocol.Snapshot, theme uiTheme) paneRenderEntry {
	active := pane.ID == activePaneID
	title := displayPaneTitleWithLookup(pane, lookup)
	border := paneBorderInfoWithLookup(pane, lookup, confirmPaneID)
	terminal := lookup.terminal(pane.TerminalID)
	overflow := paneOverflowHintsForRender(originalRect, rect, nil, nil)
	copyModeActive := pane.ID == copyModePaneID
	snapshot := (*protocol.Snapshot)(nil)
	surface := runtime.TerminalSurface(nil)
	surfaceVersion := uint64(0)
	if terminal != nil {
		snapshot = terminal.Snapshot
		surface = terminal.Surface
		surfaceVersion = terminal.SurfaceVersion
	}
	if pane.ID == paneSnapshotOverridePaneID && paneSnapshotOverride != nil {
		snapshot = paneSnapshotOverride
		surface = nil
		surfaceVersion = 0
	}
	if copyModeActive && copyModeSnapshot != nil {
		snapshot = copyModeSnapshot
		surface = nil
		surfaceVersion = 0
	}
	if copyModeActive {
		border.CopyTimeLabel = copyModeTimestampLabel(snapshot, copyModeCursorRow)
		border.CopyRowLabel = copyModeRowPositionLabel(snapshot, copyModeCursorRow)
	}
	emptyActionSelected := -1
	if pane.TerminalID == "" && pane.ID == emptyPaneSelectionPaneID {
		emptyActionSelected = emptyPaneSelectionIndex
	}
	exitedActionSelected := -1
	if pane.TerminalID != "" && pane.ID == exitedPaneSelectionPaneID {
		if terminal := lookup.terminal(pane.TerminalID); terminal != nil && terminal.State == "exited" {
			exitedActionSelected = exitedPaneSelectionIndex
		}
	}
	contentKey := paneContentKey{
		TerminalID:           pane.TerminalID,
		ThemeBG:              theme.panelBG,
		TerminalKnown:        terminal != nil,
		SharedLeft:           pane.SharedLeft,
		SharedTop:            pane.SharedTop,
		ScrollOffset:         scrollOffset,
		EmptyActionSelected:  emptyActionSelected,
		ExitedActionSelected: exitedActionSelected,
		ExitedActionPulse:    exitedPaneSelectionPulse,
		CopyModeActive:       copyModeActive,
		CopyModeCursorRow:    copyModeCursorRow,
		CopyModeCursorCol:    copyModeCursorCol,
		CopyModeViewTopRow:   copyModeViewTopRow,
		CopyModeMarkSet:      copyModeMarkSet,
		CopyModeMarkRow:      copyModeMarkRow,
		CopyModeMarkCol:      copyModeMarkCol,
	}
	if terminal != nil {
		if snapshot != nil && surface == nil {
			contentKey.Snapshot = snapshot
		}
		contentKey.SurfaceVersion = surfaceVersion
		contentKey.Name = terminal.Name
		contentKey.State = terminal.State
		overflow = paneOverflowHintsForRender(originalRect, rect, snapshot, surface)
	}
	return paneRenderEntry{
		PaneID:     pane.ID,
		Rect:       rect,
		Frameless:  frameless,
		SharedLeft: pane.SharedLeft,
		SharedTop:  pane.SharedTop,
		Title:      title,
		Border:     border,
		Theme:      theme,
		Overflow:   overflow,
		ContentKey: contentKey,
		FrameKey: paneFrameKey{
			Rect:            rect,
			Frameless:       frameless,
			SharedLeft:      pane.SharedLeft,
			SharedTop:       pane.SharedTop,
			Title:           title,
			Border:          border,
			ThemeBG:         theme.panelBG,
			Overflow:        overflow,
			Active:          active,
			Floating:        pane.Floating,
			ChromeSignature: paneChromeActionSignatureForFrame(rect, title, border, pane.Floating),
		},
		TerminalID:           pane.TerminalID,
		Snapshot:             snapshot,
		Surface:              surface,
		SurfaceVersion:       surfaceVersion,
		ScrollOffset:         scrollOffset,
		Active:               active,
		Floating:             pane.Floating,
		EmptyActionSelected:  emptyActionSelected,
		ExitedActionSelected: exitedActionSelected,
		ExitedActionPulse:    exitedPaneSelectionPulse,
		CopyModeActive:       copyModeActive,
		CopyModeCursorRow:    copyModeCursorRow,
		CopyModeCursorCol:    copyModeCursorCol,
		CopyModeViewTopRow:   copyModeViewTopRow,
		CopyModeMarkSet:      copyModeMarkSet,
		CopyModeMarkRow:      copyModeMarkRow,
		CopyModeMarkCol:      copyModeMarkCol,
	}
}

func paneOverflowHintsForRender(originalRect, clippedRect workbench.Rect, snapshot *protocol.Snapshot, surface runtime.TerminalSurface) paneOverflowHints {
	if originalRect.W <= 0 || originalRect.H <= 0 || clippedRect.W <= 0 || clippedRect.H <= 0 {
		return paneOverflowHints{}
	}
	overflow := paneOverflowHints{
		Right:  originalRect.X+originalRect.W > clippedRect.X+clippedRect.W,
		Bottom: originalRect.Y+originalRect.H > clippedRect.Y+clippedRect.H,
	}
	metrics := terminalMetricsForSource(renderSource(snapshot, surface))
	contentRect := contentRectForPane(clippedRect)
	if metrics.Cols > 0 && contentRect.W > 0 && metrics.Cols > contentRect.W {
		overflow.Right = true
	}
	if metrics.Rows > 0 && contentRect.H > 0 && metrics.Rows > contentRect.H {
		overflow.Bottom = true
	}
	return overflow
}
