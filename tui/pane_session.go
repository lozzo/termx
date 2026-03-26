package tui

import localvterm "github.com/lozzow/termx/vterm"

type PaneSession struct {
	TerminalID     string
	Channel        uint16
	AttachMode     string
	VTerm          *localvterm.VTerm
	DefaultFG      string
	DefaultBG      string
	ResizeAcquired bool
	stopStream     func()

	cellCache       [][]drawCell
	cellVersion     uint64
	viewportCache   [][]drawCell
	viewportOffset  Point
	viewportWidth   int
	viewportHeight  int
	viewportVersion uint64
	renderDirty     bool
	live            bool
	syncLost        bool
	droppedBytes    uint64
	recovering      bool
	catchingUp      bool
	dirtyTicks      int
	cleanTicks      int
	skipTick        bool
	dirtyRowsKnown  bool
	dirtyRowStart   int
	dirtyRowEnd     int
	dirtyColsKnown  bool
	dirtyColStart   int
	dirtyColEnd     int
}

func (s *PaneSession) IsRenderDirty() bool {
	return s != nil && s.renderDirty
}

func (s *PaneSession) MarkRenderDirty() {
	if s == nil {
		return
	}
	s.renderDirty = true
}

func (s *PaneSession) ClearRenderDirty() {
	if s == nil {
		return
	}
	s.renderDirty = false
}

func (s *PaneSession) IsSyncLost() bool {
	return s != nil && s.syncLost
}

func (s *PaneSession) SetSyncLost(value bool) {
	if s == nil {
		return
	}
	s.syncLost = value
}

func (s *PaneSession) DroppedBytes() uint64 {
	if s == nil {
		return 0
	}
	return s.droppedBytes
}

func (s *PaneSession) AddDroppedBytes(value uint64) {
	if s == nil {
		return
	}
	s.droppedBytes += value
}

func (s *PaneSession) SetDroppedBytes(value uint64) {
	if s == nil {
		return
	}
	s.droppedBytes = value
}

func (s *PaneSession) IsRecovering() bool {
	return s != nil && s.recovering
}

func (s *PaneSession) SetRecovering(value bool) {
	if s == nil {
		return
	}
	s.recovering = value
}

func (s *PaneSession) IsResizeAcquired() bool {
	return s != nil && s.ResizeAcquired
}

func (s *PaneSession) SetResizeAcquired(value bool) {
	if s == nil {
		return
	}
	s.ResizeAcquired = value
}

func (s *PaneSession) HasStopStream() bool {
	return s != nil && s.stopStream != nil
}

func (s *PaneSession) DirtyRows() (start int, end int, known bool) {
	if s == nil {
		return 0, 0, false
	}
	return s.dirtyRowStart, s.dirtyRowEnd, s.dirtyRowsKnown
}

func (s *PaneSession) SetDirtyRows(start int, end int, known bool) {
	if s == nil {
		return
	}
	s.dirtyRowStart = start
	s.dirtyRowEnd = end
	s.dirtyRowsKnown = known
}

func (s *PaneSession) DirtyCols() (start int, end int, known bool) {
	if s == nil {
		return 0, 0, false
	}
	return s.dirtyColStart, s.dirtyColEnd, s.dirtyColsKnown
}

func (s *PaneSession) SetDirtyCols(start int, end int, known bool) {
	if s == nil {
		return
	}
	s.dirtyColStart = start
	s.dirtyColEnd = end
	s.dirtyColsKnown = known
}
